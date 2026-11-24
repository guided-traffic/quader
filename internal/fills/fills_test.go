package fills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time             { return c.t }
func (c *clock) add(d time.Duration)        { c.t = c.t.Add(d) }
func newClock() *clock                      { return &clock{t: time.Date(2026, 11, 24, 8, 40, 0, 0, time.UTC)} }
func start(name string) proxy.DownloadStart { return startOf(name, 1000) }

func startOf(name string, total int64) proxy.DownloadStart {
	return proxy.DownloadStart{RepoID: 1, Repo: "web/npm-remote", Format: "npm", Name: name, Version: "1.0.0", Path: name + "-1.0.0.tgz",
		Total: total, Cause: proxy.DownloadCause{Origin: proxy.DownloadOriginClient, Principal: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20"}}
}

// value reads the one series of a collector.
func value(c prometheus.Collector) float64 {
	ch := make(chan prometheus.Metric, 1)
	c.Collect(ch)
	m := <-ch
	var d dto.Metric
	if err := m.Write(&d); err != nil {
		return -1
	}
	if d.Counter != nil {
		return d.Counter.GetValue()
	}
	return d.Gauge.GetValue()
}

// read reads n bytes through the download's counter.
func read(t *testing.T, d proxy.Download, n int) {
	t.Helper()
	if _, err := io.Copy(io.Discard, d.Reader(strings.NewReader(strings.Repeat("x", n)))); err != nil {
		t.Fatal(err)
	}
}

// R-OPS-050, Q-OPS-421: the speed of a running download is the bytes of the last 10 s over those 10 s, sampled at each publication; in
// the first 10 s the bytes over the time since the begin; none in the first second.
func TestR_OPS_050_TheSpeedOfARunningDownload(t *testing.T) {
	c := newClock()
	tr := NewTracker(TrackerOptions{Replica: "quader-packages-1", Now: c.now})
	d := tr.Begin(start("a"))
	speedAt := func() *int64 { return tr.snapshot().Running[0].Speed }

	c.add(500 * time.Millisecond)
	read(t, d, 400)
	if s := speedAt(); s != nil {
		t.Fatalf("in the first second: %d", *s)
	}
	// 2 s in, 4000 bytes: the first window divides by the time since the begin.
	c.add(1500 * time.Millisecond)
	read(t, d, 3600)
	if s := speedAt(); s == nil || *s != 2000 {
		t.Fatalf("at 2 s: %v", s)
	}
	// Every 2 s, 2000 bytes more until 12 s (24,000 bytes); then the download stalls.
	for range 5 {
		c.add(2 * time.Second)
		read(t, d, 4000)
		tr.snapshot()
	}
	// At 14 s the base is the sample at 4 s (8,000 bytes): 16,000 bytes over 10 s.
	c.add(2 * time.Second)
	if s := speedAt(); s == nil || *s != 1600 {
		t.Fatalf("at 14 s after a stall of 2 s: %v", s)
	}
	// A stall of 10 s and more reads 0.
	c.add(10 * time.Second)
	if s := speedAt(); s == nil || *s != 0 {
		t.Fatalf("after a stall of 12 s: %v", s)
	}
	if got := tr.snapshot().Running[0]; got.Bytes != 24000 || got.ElapsedMs != 24000 || got.Total == nil || *got.Total != 1000 {
		t.Errorf("entry %+v", got)
	}
	if w := len(tr.running[tr.snapshot().Running[0].ID].win.samples); w > 8 {
		t.Errorf("the window keeps %d samples", w)
	}
}

// fakeSink records the publications and the sweeps.
type fakeSink struct {
	pubs   []Publication
	sweeps []time.Duration
	fail   error
}

func (f *fakeSink) Publish(_ context.Context, p Publication) error {
	if f.fail != nil {
		return f.fail
	}
	f.pubs = append(f.pubs, p)
	return nil
}

func (f *fakeSink) Sweep(_ context.Context, older time.Duration) (int, error) {
	f.sweeps = append(f.sweeps, older)
	return 0, f.fail
}

// R-OPS-050, R-OPS-051: a publication writes nothing while idle, upserts the live row while something runs (at most 200 entries, the
// longest running first, and the count of the others), deletes it once after the last end, inserts the ended downloads, keeps them for
// the next publication when the write fails, and sweeps at most every 30 s with ten intervals as the bound.
func TestR_OPS_050_APublicationWritesWhatRunsAndWhatEnded(t *testing.T) {
	c := newClock()
	reg := prometheus.NewRegistry()
	tr := NewTracker(TrackerOptions{Replica: "quader-packages-1", Now: c.now, Metrics: reg})
	sink := &fakeSink{}
	p := NewPublisher(PublisherOptions{Tracker: tr, Sink: sink, InstanceUID: "uid-1", Replica: "quader-packages-1", Interval: 2 * time.Second, Now: c.now})
	ctx := context.Background()

	p.Tick(ctx)
	if len(sink.pubs) != 0 || len(sink.sweeps) != 1 || sink.sweeps[0] != 30*time.Second {
		t.Fatalf("idle: %+v, sweeps %v", sink.pubs, sink.sweeps)
	}
	var ds []proxy.Download
	for i := range MaxPublished + 3 {
		ds = append(ds, tr.Begin(start(fmt.Sprintf("p%03d", i))))
		c.add(time.Millisecond)
	}
	c.add(2 * time.Second)
	p.Tick(ctx)
	if len(sink.pubs) != 1 || len(sink.pubs[0].Running) != MaxPublished || sink.pubs[0].Omitted != 3 || sink.pubs[0].DeleteLive {
		t.Fatalf("running: %d, omitted %d", len(sink.pubs[0].Running), sink.pubs[0].Omitted)
	}
	if first := sink.pubs[0].Running[0]; first.Name != "p000" || first.By == "" || first.Origin != "client" {
		t.Errorf("the longest running first: %+v", first)
	}
	if len(sink.sweeps) != 1 {
		t.Errorf("a sweep within 30 s of the last: %v", sink.sweeps)
	}
	if v := value(tr.m.running); v != MaxPublished+3 {
		t.Errorf("the gauge: %v", v)
	}

	// Every download ends: the next publication inserts them and removes the live row, the one after writes nothing.
	for i, d := range ds {
		read(t, d, 10)
		outcome := OutcomeCompleted
		var f *remotecuration.Failure
		if i == 0 {
			outcome, f = OutcomeFailed, &remotecuration.Failure{Class: remotecuration.ClassIntegrity, Code: "upstream_integrity_mismatch"}
		}
		d.End(outcome, f)
		d.End(OutcomeCancelled, nil) // a second end does nothing
	}
	c.add(2 * time.Second)
	p.Tick(ctx)
	last := sink.pubs[len(sink.pubs)-1]
	if len(sink.pubs) != 2 || !last.DeleteLive || len(last.Running) != 0 || len(last.Ended) != MaxFinished {
		t.Fatalf("after the last end: delete %v, running %d, ended %d", last.DeleteLive, len(last.Running), len(last.Ended))
	}
	if e := last.Ended[len(last.Ended)-1]; e.Name != fmt.Sprintf("p%03d", MaxPublished+2) || e.Outcome != OutcomeCompleted || e.Bytes != 10 {
		t.Errorf("the buffer keeps the newest 100: %+v", e)
	}
	c.add(2 * time.Second)
	p.Tick(ctx)
	if len(sink.pubs) != 2 {
		t.Fatalf("idle again: %d publications", len(sink.pubs))
	}
	if got := value(tr.m.total.WithLabelValues("web/npm-remote", OutcomeFailed)); got != 1 {
		t.Errorf("failed %v", got)
	}
	if got := value(tr.m.bytes.WithLabelValues("web/npm-remote")); got != float64(10*len(ds)) {
		t.Errorf("bytes %v", got)
	}

	// A failed publication keeps its ended downloads for the next one and counts the failure.
	d := tr.Begin(start("late"))
	c.add(time.Second)
	d.End(OutcomeCancelled, nil)
	sink.fail = errors.New("database unavailable")
	c.add(30 * time.Second)
	p.Tick(ctx)
	sink.fail = nil
	c.add(2 * time.Second)
	p.Tick(ctx)
	last = sink.pubs[len(sink.pubs)-1]
	if len(last.Ended) != 1 || last.Ended[0].Name != "late" || last.Ended[0].Outcome != OutcomeCancelled || last.Ended[0].DurationMs != 1000 {
		t.Fatalf("after a failure: %+v", last)
	}
	if got := value(tr.m.failures); got < 1 {
		t.Errorf("failures %v", got)
	}

	// Flush removes the live row whatever runs, and writes what ended.
	tr.Begin(start("running at the shutdown"))
	if err := p.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if last = sink.pubs[len(sink.pubs)-1]; !last.DeleteLive || len(last.Running) != 0 {
		t.Errorf("flush: %+v", last)
	}
}

// R-OPS-051: the downloads of a swept live row end interrupted at the row's last write, with the bytes and the elapsed time of that
// write and every fact of the entry.
func TestR_OPS_051_AStaleLiveRowEndsInterrupted(t *testing.T) {
	total := int64(65536)
	written := time.Date(2026, 11, 24, 8, 40, 0, 0, time.UTC)
	got := Interrupted("quader-worker-1", written, []Entry{{
		ID: "01937d4e-0000-7000-8000-000000000001", RepoID: 7, Name: "library/ubuntu", Version: "26.10", Path: "v2/x", Bytes: 16384, Total: &total,
		StartedAt: written.Add(-9 * time.Second), ElapsedMs: 9000, Origin: "queue", QueueOrigin: "person",
		QueueRow: "01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e0f", By: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20",
	}})
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	f := got[0]
	if f.Outcome != OutcomeInterrupted || !f.FinishedAt.Equal(written) || f.DurationMs != 9000 || f.Bytes != 16384 || *f.Total != total ||
		f.Replica != "quader-worker-1" || f.QueueRow == nil || f.By == nil || f.QueueOrigin != "person" || f.RepoID != 7 {
		t.Errorf("%+v", f)
	}
}

// R-OPS-049: a cause whose principal or queue row is not a UUID records none, so that the insert never fails its cast; a download
// without a total records none.
func TestR_OPS_049_AFinishedDownloadKeepsOnlyWhatItCanStore(t *testing.T) {
	c := newClock()
	tr := NewTracker(TrackerOptions{Replica: "r", Now: c.now})
	s := startOf("x", -1)
	s.Cause = proxy.DownloadCause{Origin: proxy.DownloadOriginQueue, QueueOrigin: remotecuration.OriginBulk, QueueRow: "not-a-row", Principal: "system"}
	tr.Begin(s).End(OutcomeCompleted, &remotecuration.Failure{Class: remotecuration.ClassNetwork})
	e := tr.snapshot().Ended[0]
	if e.By != nil || e.QueueRow != nil || e.QueueOrigin != "bulk" || e.Total != nil || e.FailureClass != "" {
		t.Errorf("%+v", e)
	}
}
