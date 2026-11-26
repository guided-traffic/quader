package fills

import (
	"sort"
	"strings"
	"time"
)

// RunningUnit is a running download as the API answers it: the entries of one unit's key across the fresh live rows, with the finished
// rows of the same key that belong to it (R-OPS-049 "Across replicas").
type RunningUnit struct {
	Entry
	// Replica is the pod name of the replica whose part of the unit began first among those that run.
	Replica string
	// Elapsed is the time since the unit's begin at the read: the elapsed time at the write plus the age of the write.
	Elapsed time.Duration
}

// Units is a read merged into units: the running ones in the order of their begin, the finished ones newest first, at most MaxFinished,
// and the count of the running ones, those a replica left out of its row included.
type Units struct {
	Running      []RunningUnit
	Finished     []Finished
	RunningCount int
}

// member is one record of a unit's key: a running entry of a fresh live row or a finished row.
type member struct {
	running *RunningUnit
	done    *Finished
	start   time.Time
	end     time.Time
}

// Merge turns a read into units (R-OPS-049, R-OPS-052, Q-OPS-429). The records of one key are one unit: a key of a run of work (a queue
// row's run, a mirror pass's image, the database task's run) whatever lies between them; a key of a client's unit, which repeats, when the
// next record begins at most MergeGap after the end of the ones before. A unit with a running record runs; its bytes are the sum of its
// records', its total the sum where every record has one, its speed the sum of the running records'; its outcome is failed before
// interrupted before cancelled before completed. A live row older than FreshIntervals intervals is left out (its replica stalled or
// stopped).
func Merge(v View, interval time.Duration) Units {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	byKey := map[string][]member{}
	var keys []string
	add := func(key string, m member) {
		if _, ok := byKey[key]; !ok {
			keys = append(keys, key)
		}
		byKey[key] = append(byKey[key], m)
	}
	out := Units{}
	fresh := FreshIntervals * interval
	for _, l := range v.Live {
		if l.Age > fresh {
			continue
		}
		out.RunningCount += l.Omitted
		for _, e := range l.Downloads {
			r := &RunningUnit{Entry: e, Replica: l.Replica, Elapsed: time.Duration(e.ElapsedMs)*time.Millisecond + l.Age}
			add(unitKey(e.Unit, e.ID), member{running: r, start: e.StartedAt, end: v.Now})
		}
	}
	for i := range v.Finished {
		f := &v.Finished[i]
		add(unitKey(f.Unit, f.ID), member{done: f, start: f.StartedAt, end: f.FinishedAt})
	}
	for _, key := range keys {
		for _, session := range sessions(key, byKey[key]) {
			if r, ok := mergeRunning(session, v.Now); ok {
				out.Running = append(out.Running, r)
				continue
			}
			out.Finished = append(out.Finished, mergeFinished(session))
		}
	}
	out.RunningCount += len(out.Running)
	sort.SliceStable(out.Running, func(i, j int) bool {
		if !out.Running[i].StartedAt.Equal(out.Running[j].StartedAt) {
			return out.Running[i].StartedAt.Before(out.Running[j].StartedAt)
		}
		return out.Running[i].ID < out.Running[j].ID
	})
	sort.SliceStable(out.Finished, func(i, j int) bool {
		if !out.Finished[i].FinishedAt.Equal(out.Finished[j].FinishedAt) {
			return out.Finished[i].FinishedAt.After(out.Finished[j].FinishedAt)
		}
		return out.Finished[i].ID > out.Finished[j].ID
	})
	if len(out.Finished) > MaxFinished {
		out.Finished = out.Finished[:MaxFinished]
	}
	return out
}

// sessions splits the records of one key into units, each in the order of its begin: a client's key ("c|…") where a record begins more
// than MergeGap after the end of the records before; every other key is one unit.
func sessions(key string, ms []member) [][]member {
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].start.Before(ms[j].start) })
	if !strings.HasPrefix(key, "c|") {
		return [][]member{ms}
	}
	var out [][]member
	var cur []member
	var end time.Time
	for _, m := range ms {
		if len(cur) > 0 && m.start.Sub(end) > MergeGap {
			out = append(out, cur)
			cur = nil
		}
		if len(cur) == 0 || m.end.After(end) {
			end = m.end
		}
		cur = append(cur, m)
	}
	return append(out, cur)
}

// mergeRunning is a unit with a running record; ok is false for one whose records all ended.
func mergeRunning(ms []member, now time.Time) (RunningUnit, bool) {
	var first *RunningUnit
	for _, m := range ms {
		if m.running != nil {
			first = m.running
			break
		}
	}
	if first == nil {
		return RunningUnit{}, false
	}
	u := *first
	head := ms[0]
	u.ID, u.StartedAt = idOf(head), head.start
	if head.done != nil {
		u.Name, u.Path, u.By = head.done.Name, head.done.Path, deref(head.done.By)
	} else {
		u.Name, u.Path, u.By = head.running.Name, head.running.Path, head.running.By
	}
	u.Version, u.Bytes, u.Parts, u.Speed = "", 0, 0, nil
	var total int64
	known := true
	for _, m := range ms {
		var t *int64
		if m.running != nil {
			e := m.running
			u.Bytes += e.Bytes
			u.Parts += e.Parts
			t = e.Total
			u.Elapsed = max(u.Elapsed, e.Elapsed)
			if e.Speed != nil {
				s := *e.Speed
				if u.Speed != nil {
					s += *u.Speed
				}
				u.Speed = &s
			}
			if u.Version == "" {
				u.Version = e.Version
			}
		} else {
			f := m.done
			u.Bytes += f.Bytes
			u.Parts += f.Parts
			t = f.Total
			if u.Version == "" {
				u.Version = f.Version
			}
		}
		if t == nil {
			known = false
		} else {
			total += *t
		}
	}
	u.Total = nil
	if known {
		u.Total = &total
	}
	// A unit of several records ran since its first record's begin; one record keeps its replica's monotonic reading.
	if since := now.Sub(u.StartedAt); len(ms) > 1 && since > u.Elapsed {
		u.Elapsed = since
	}
	return u, true
}

// mergeFinished is a unit whose records all ended.
func mergeFinished(ms []member) Finished {
	u := *ms[0].done
	u.Version, u.Bytes, u.Parts = "", 0, 0
	var total int64
	known := true
	rank := map[string]int{OutcomeCompleted: 0, OutcomeCancelled: 1, OutcomeInterrupted: 2, OutcomeFailed: 3}
	u.Outcome = OutcomeCompleted
	for _, m := range ms {
		f := m.done
		u.Bytes += f.Bytes
		u.Parts += f.Parts
		if u.Version == "" {
			u.Version = f.Version
		}
		if f.Total == nil {
			known = false
		} else {
			total += *f.Total
		}
		if f.FinishedAt.After(u.FinishedAt) || f.FinishedAt.Equal(u.FinishedAt) {
			u.FinishedAt, u.Replica = f.FinishedAt, f.Replica
		}
		if rank[f.Outcome] > rank[u.Outcome] || (f.Outcome == OutcomeFailed && u.Outcome == OutcomeFailed) {
			u.Outcome, u.FailureClass, u.FailureCode, u.FailurePath = f.Outcome, f.FailureClass, f.FailureCode, f.FailurePath
		}
	}
	u.Total = nil
	if known {
		u.Total = &total
	}
	if len(ms) == 1 {
		return u // one record keeps its replica's monotonic reading
	}
	u.DurationMs = max(u.FinishedAt.Sub(u.StartedAt).Milliseconds(), 0)
	for _, m := range ms {
		u.DurationMs = max(u.DurationMs, m.done.DurationMs)
	}
	return u
}

func idOf(m member) string {
	if m.done != nil {
		return m.done.ID
	}
	return m.running.ID
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
