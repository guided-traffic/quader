package gate

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/malware"
	mgate "github.com/guided-traffic/quader/internal/malware/gate"
	"github.com/guided-traffic/quader/internal/vuln/enforce"
)

// TestR_STOR_070_AWritersReadIsDecidedAndRecordedOnceLinkedOrStreamed: the decision of the serve point (RunDecided) reports a writer's
// pass of the signature gate (Q-SIG-211) as decided, so the download may be handed to a signed link of the object store (storage.md
// R-STOR-070 step 6, Q-STOR-350), and records the writer's read (signature.download_by_writer and its count, Q-SIG-240) in the decision
// itself, before the serve point streams the download or hands it to a link: a download a link serves leaves the event and the count a
// streamed one does, and a download whose link is not issued (here a blob without a malware verdict under a gate that is not strict,
// which streams) leaves them as well, once. A verified pass is decided and records none. Unit, milliseconds, no payload.
func TestR_STOR_070_AWritersReadIsDecidedAndRecordedOnceLinkedOrStreamed(t *testing.T) {
	ctx := context.Background()
	reg := prometheus.NewRegistry()
	m, err := NewMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	st, rec := &fakeStore{}, &recorder{}
	g := newGate(st, rec, Options{Metrics: m, Wait: 5 * time.Second, Poll: 10 * time.Millisecond})
	download := func(path string) Download {
		d := dl(filesInternal)
		d.Path, d.Name, d.Actor = path, path, audit.Actor{Kind: "user", ID: "u-1", Name: "pusher"}
		d.MayWrite = func(context.Context) (bool, error) { return true, nil }
		return d
	}
	serve := func(d Download, mv malware.Verdict) Serve {
		return Serve{
			Malware:   mgate.New(mgate.Options{Store: &malwareStore{}, Enabled: true, Wait: 5 * time.Second, Poll: 10 * time.Millisecond}),
			MalwareOf: mgate.Download{Addressed: filesInternal.Name, HolderID: 7, Path: d.Path, Verdict: mv},
			Enforce:   enforce.New(enforce.Options{}), VulnOf: enforce.Download{Addressed: "web/files-internal", Path: d.Path},
			Signatures: g, SignaturesOf: d,
		}
	}
	clean := malware.Verdict{Known: true, State: malware.StateClean}

	decided, err := serve(download("app/_blobs/linked"), clean).RunDecided(ctx)
	if err != nil || !decided {
		t.Fatalf("a writer's read of a clean blob: decided %v, %v; want a decided pass the serve point may hand to a link", decided, err)
	}
	if n := len(writerEvents(rec)); n != 1 || byWriter(t, reg, "missing") != 1 {
		t.Fatalf("the read a link serves: %d events, %v counted; want one of each", n, byWriter(t, reg, "missing"))
	}
	decided, err = serve(download("app/_blobs/streamed"), malware.Verdict{}).RunDecided(ctx)
	if err != nil || decided {
		t.Fatalf("a writer's read of a blob without a verdict under a lax gate: decided %v, %v; want served and not decided", decided, err)
	}
	if n := len(writerEvents(rec)); n != 2 || byWriter(t, reg, "missing") != 2 {
		t.Fatalf("the read that streams: %d events, %v counted; want one more of each", n, byWriter(t, reg, "missing"))
	}
	st.set(valid(fileBlob, TrustInternal, 3))
	if decided, err := serve(download("app/_blobs/verified"), clean).RunDecided(ctx); err != nil || !decided {
		t.Fatalf("a verified pass: decided %v, %v", decided, err)
	}
	if n := len(writerEvents(rec)); n != 2 {
		t.Fatalf("a verified pass recorded a writer's read: %d events", n)
	}
}
