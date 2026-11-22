package gate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/malware"
	mgate "github.com/guided-traffic/quader/internal/malware/gate"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/vuln/enforce"
)

// byWriter counts the writer's reads of a reason on the registry.
func byWriter(t *testing.T, reg *prometheus.Registry, reason string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "quader_signatures_downloads_by_writer_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "reason" && l.GetValue() == reason {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// writerEvents are the recorded events signature.download_by_writer.
func writerEvents(rec *recorder) []audit.Event {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var out []audit.Event
	for _, ev := range rec.events {
		if ev.Type == audit.EventSignatureDownloadByWriter {
			out = append(out, ev)
		}
	}
	return out
}

// TestR_SIG_064_AWritersReadIsAuditedAndCounted: a download the gate passes only because the caller may write (Q-SIG-211) is served and
// leaves one signature.download_by_writer a minute per repository, path, caller and client address (Q-SIG-240): the HEAD and the GET of
// one manifest are one event, counted twice; another caller is an event of its own. The event names the repository, the artifact in its
// client's words, the caller and the reason a reader is refused for, pending included; the remote tag's Check records it as the serve
// point does. A verified pass, a reader's refusal and a writer's pass that the malware gate then refuses record none, and the verified
// pass reads no more than one statement of rows. Unit, milliseconds, no payload.
func TestR_SIG_064_AWritersReadIsAuditedAndCounted(t *testing.T) {
	ctx := context.Background()
	writer := audit.Actor{Kind: "user", ID: "u-1", Name: "pusher"}
	setup := func() (*fakeStore, *recorder, *prometheus.Registry, *Gate) {
		reg := prometheus.NewRegistry()
		m, err := NewMetrics(reg)
		if err != nil {
			t.Fatal(err)
		}
		st, rec := &fakeStore{}, &recorder{}
		return st, rec, reg, newGate(st, rec, Options{Metrics: m, Wait: 5 * time.Second, Poll: 10 * time.Millisecond})
	}
	download := func(may bool) Download {
		d := dl(filesInternal)
		d.Path, d.Name, d.Actor, d.Endpoint = "app/_tags/1.0", "app:1.0", writer, "main"
		d.MayWrite = func(context.Context) (bool, error) { return may, nil }
		return d
	}
	serve := func(g *Gate, d Download, mv malware.Verdict) Serve {
		ms := &malwareStore{}
		return Serve{
			Malware:   mgate.New(mgate.Options{Store: ms, Enabled: true, Wait: 5 * time.Second, Poll: 10 * time.Millisecond}),
			MalwareOf: mgate.Download{Addressed: filesInternal.Name, HolderID: 7, Path: d.Path, Verdict: mv, Strict: true},
			Enforce:   enforce.New(enforce.Options{}), VulnOf: enforce.Download{Addressed: "web/files-internal", Path: d.Path},
			Signatures: g, SignaturesOf: d,
		}
	}
	clean := malware.Verdict{Known: true, State: malware.StateClean}

	t.Run("a writer's HEAD and GET are one event and two counts", func(t *testing.T) {
		_, rec, reg, g := setup()
		d := download(true)
		for range 2 {
			if err := serve(g, d, clean).Run(ctx); err != nil {
				t.Fatalf("the writer's read: %v", err)
			}
		}
		evs := writerEvents(rec)
		if len(evs) != 1 || byWriter(t, reg, "missing") != 2 {
			t.Fatalf("%d events, %v counted", len(evs), byWriter(t, reg, "missing"))
		}
		ev := evs[0]
		if ev.Actor != writer || ev.Outcome != audit.Warning || ev.Reason != "missing" || ev.Repository != "web/files-internal" ||
			ev.Project != "web" || ev.Path != "app/_tags/1.0" || ev.Endpoint != "main" || ev.Source.ClientAddr != "192.0.2.1" {
			t.Errorf("event %+v", ev)
		}
		var body map[string]string
		if err := json.Unmarshal(ev.Details, &body); err != nil || body["artifact"] != "app:1.0" || body["repository"] != "web/files-internal" ||
			body["path"] != "app/_tags/1.0" || body["reason"] != "missing" || len(body["sha256"]) != 64 {
			t.Errorf("details %s %v", ev.Details, err)
		}
		other := d
		other.Actor = audit.Actor{Kind: "serviceaccount", ID: "sa-9", Name: "ci"}
		if err := serve(g, other, clean).Run(ctx); err != nil {
			t.Fatal(err)
		}
		if n := len(writerEvents(rec)); n != 2 {
			t.Errorf("another caller from the same address: %d events", n)
		}
	})

	t.Run("the remote tag's check and a pending signature", func(t *testing.T) {
		st, rec, reg, g := setup()
		st.set(Row{Subject: fileBlob, State: signatures.StatePending})
		if err := g.Check(ctx, download(true)); err != nil {
			t.Fatalf("the writer's read of a pending artifact: %v", err)
		}
		evs := writerEvents(rec)
		if len(evs) != 1 || evs[0].Reason != "pending" || byWriter(t, reg, "pending") != 1 {
			t.Fatalf("events %+v, counted %v", evs, byWriter(t, reg, "pending"))
		}
	})

	t.Run("a verified pass, a refusal and a writer's pass the malware gate refuses record none", func(t *testing.T) {
		st, rec, reg, g := setup()
		st.set(valid(fileBlob, TrustInternal, 3))
		if err := serve(g, download(true), clean).Run(ctx); err != nil {
			t.Fatalf("a verified pass: %v", err)
		}
		if rec.count() != 0 || st.reads.Load() != 1 {
			t.Errorf("a verified pass: %d events, %d reads of rows", rec.count(), st.reads.Load())
		}
		st.set()
		if err := serve(g, download(false), clean).Run(ctx); err == nil {
			t.Fatal("a reader's download passed")
		}
		unscannable := malware.Verdict{Known: true, State: malware.StateUnscannable, Reason: "encrypted"}
		if err := serve(g, download(true), unscannable).Run(ctx); err == nil {
			t.Fatal("an unscannable item was served to a writer")
		}
		if evs := writerEvents(rec); len(evs) != 0 || byWriter(t, reg, "missing") != 0 || rec.count() != 1 {
			t.Errorf("%d writer's events, %v counted, %d events", len(evs), byWriter(t, reg, "missing"), rec.count())
		}
	})
}
