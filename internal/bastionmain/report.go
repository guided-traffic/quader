package bastionmain

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/usage"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
)

// What a Bastion reports beside its download events (R-BAS-016, R-LIM-045, the owner's Q-BAS-324): the usage it counted per subject at
// its place, which goes into the usage store through the replica's usage buffer as the main installation's own consumption does, and
// the state of the replica that sent the batch, which replaces that replica's row of bastion_status. Both are the Bastion's word
// (H-2282): the main installation's counters never read them.

// statusRetention is how long a replica's state stays after its last report (Q-BAS-443): a replica reports with every flush, so an
// hour without one is a replica that is gone, and the addresses of its blocks go with its row. A block that ended is never shown.
const statusRetention = time.Hour

// statusPruneInterval is how often a replica deletes the states not reported within statusRetention.
const statusPruneInterval = 10 * time.Minute

// Bounds of a usage row the intake keeps (R-LIM-045): a row that breaks one is dropped and counted, never stored.
const (
	maxUsageSubjectBytes    = 512
	maxUsageCredentialBytes = 64
)

// UsageOptions are what the intake needs for the usage and the state of a batch; each nil member stores nothing of its part.
type UsageOptions struct {
	// Sink is the replica's usage buffer (usage.Buffer).
	Sink limits.UsageSink
	// Repository resolves a qualified repository name to its ID, false for a name that no longer exists (the row is then recorded
	// without a repository, R-LIM-045).
	Repository func(ctx context.Context, name qname.Repo) (int64, bool)
	// RuleFigures are the effective figures of a rule of the registration's place, for the utilisation of the overview; false for a
	// rule the registration's configuration does not hold.
	RuleFigures func(bastionID, rule string) (limits.Figures, bool)
	// Writer writes the replica's state.
	Writer meta.Runner
}

// usageSubject reads a row's subject key (R-BAS-096, limits_subject of R-LIM-030): user:<id>, technical_token:<id>,
// ci:<issuer>:<project>, certificate:<subject> or anonymous:<prefix>. The system's subject never comes from a Bastion.
func usageSubject(key, credential string) (limits.Subject, bool) {
	kind, ref, ok := strings.Cut(key, ":")
	sk, known := limits.ParseSubjectKind(kind)
	if !ok || !known || ref == "" || sk == limits.KindSystem || kind != string(sk) || len(key) > maxUsageSubjectBytes {
		return limits.Subject{}, false
	}
	s := limits.Subject{Kind: sk, Ref: ref}
	if sk == limits.KindUser || sk == limits.KindCertificate {
		s.Credential = credential
	}
	return s, true
}

// acceptUsage hands the rows and the overflow of a report to the usage buffer at the place of the registration (R-LIM-045): the
// subject from its key, the repository by its qualified name, the limit events per figure in the mode they were met in. A row whose
// subject, credential or repository does not read, or that is older than maxAge, is dropped and counted; a time in the future is the
// time of receipt.
func (in *Intake) acceptUsage(ctx context.Context, rec Recognition, u iface.UsageReport, now time.Time) (dropped int) {
	o := in.o.Usage
	if o.Sink == nil || (len(u.Rows) == 0 && len(u.Overflow) == 0) {
		return 0
	}
	place := limits.BastionPlace(rec.BastionID, rec.Bastion)
	repos := map[string]int64{}
	repoOf := func(name string) (int64, bool) {
		if id, seen := repos[name]; seen {
			return id, true
		}
		r, err := qname.ParseRepo(name)
		if err != nil {
			return 0, false
		}
		var id int64
		if o.Repository != nil {
			id, _ = o.Repository(ctx, r)
		}
		repos[name] = id
		return id, true
	}
	at := func(t time.Time) (time.Time, bool) {
		switch {
		case t.IsZero() || now.Sub(t) > in.o.MaxAge:
			return time.Time{}, false
		case t.After(now):
			return now, true
		}
		return t, true
	}
	for _, row := range u.Rows {
		s, ok := usageSubject(row.Subject, row.Credential)
		repo, okRepo := repoOf(row.Repository)
		t, okAt := at(row.BucketStart)
		if !ok || !okRepo || !okAt || len(row.Credential) > maxUsageCredentialBytes || (row.Rule != "" && !iface.ValidRuleID(row.Rule)) {
			dropped++
			continue
		}
		d := limits.UsageDelta{
			At: t, Subject: s, Place: place, RepoID: repo, Requests: row.Requests, Refused: row.Refused, Cached: row.CachedBytes,
			Uncached: row.UncachedBytes, Upstream: row.UpstreamRequests,
		}
		if o.RuleFigures != nil && row.Rule != "" {
			d.Limits, _ = o.RuleFigures(rec.BastionID, row.Rule)
		}
		d.Events = append(eventsOf(row.Limited, limits.ModeEnforce), eventsOf(row.WouldLimit, limits.ModeAudit)...)
		o.Sink.Add(d)
	}
	for _, ov := range u.Overflow {
		kind, known := limits.ParseSubjectKind(ov.SubjectKind)
		repo, okRepo := repoOf(ov.Repository)
		t, okAt := at(ov.BucketStart)
		if !known || kind == limits.KindSystem || !okRepo || !okAt {
			dropped++
			continue
		}
		o.Sink.Add(limits.UsageDelta{
			At: t, Subject: limits.Subject{Kind: kind, Ref: usage.OverflowRef}, Place: place, RepoID: repo, Requests: ov.Requests,
			Refused: ov.Refused, Cached: ov.CachedBytes, Uncached: ov.UncachedBytes, Upstream: ov.UpstreamRequests,
		})
	}
	return dropped
}

// eventsOf are the limit events of a row's map in a mode, by the figures' names ([iface.FigureNames]).
func eventsOf(m map[string]int64, mode limits.Mode) []limits.EventDelta {
	var out []limits.EventDelta
	for figure, n := range m {
		if n > 0 {
			out = append(out, limits.EventDelta{Figure: figure, Mode: mode, Count: n})
		}
	}
	return out
}

// acceptStatus replaces the state of the replica that sent the batch (R-LIM-045), with only the blocks that have not ended, and now
// and then deletes the states no replica reported within statusRetention (Q-BAS-443). A batch without a replica's state, one whose
// replica name differs from the recognition's, and a replica without a contact row write nothing.
func (in *Intake) acceptStatus(ctx context.Context, rec Recognition, st iface.ReplicaStatus, now time.Time) error {
	w := in.o.Usage.Writer
	if w == nil || st.Replica == "" || st.Replica != rec.Replica {
		return nil
	}
	st.Blocks = activeBlocks(st.Blocks, now)
	raw := mustJSON(st)
	prune := false
	in.mu.Lock()
	if now.Sub(in.prunedAt) >= statusPruneInterval {
		in.prunedAt, prune = now, true
	}
	in.mu.Unlock()
	return w.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errNotPostgres
		}
		qs := q.New(ptx)
		if _, err := qs.BastionStatusUpsert(ctx, q.BastionStatusUpsertParams{BastionID: rec.BastionID, Replica: st.Replica, Status: string(raw)}); err != nil {
			return err
		}
		if prune {
			_, err := qs.BastionStatusPrune(ctx, statusRetention.Seconds())
			return err
		}
		return nil
	})
}

// activeBlocks are the blocks whose end lies after now.
func activeBlocks(bs []iface.StatusBlock, now time.Time) []iface.StatusBlock {
	out := make([]iface.StatusBlock, 0, len(bs))
	for _, b := range bs {
		if b.Until.After(now) {
			out = append(out, b)
		}
	}
	return out
}

// stateOf reads a stored state; the blocks that ended since it was reported are left out (Q-BAS-443).
func stateOf(replica string, at time.Time, raw []byte) ReplicaState {
	s := ReplicaState{Replica: replica, ReportedAt: at}
	_ = json.Unmarshal(raw, &s.Status)
	s.Status.Replica = replica
	s.Status.Blocks = activeBlocks(s.Status.Blocks, time.Now())
	return s
}
