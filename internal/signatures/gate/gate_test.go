package gate

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

// fakeStore is the store the gate reads: rows and fetch states the test sets, and counts of the reads and the priority work.
type fakeStore struct {
	mu        sync.Mutex
	rows      []Row
	fetches   []Fetch
	read      map[[32]byte]bool
	refusals  []Reason
	reads     atomic.Int32
	asked     atomic.Int32
	raised    []Fetch
	rowRepos  [][]int64
	rowScopes []string
}

func (f *fakeStore) GateRows(_ context.Context, projectID string, repoIDs []int64, subjects [][32]byte) ([]Row, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rowRepos = append(f.rowRepos, repoIDs)
	f.rowScopes = append(f.rowScopes, projectID)
	return append([]Row(nil), f.rows...), nil
}

func (f *fakeStore) GateFetches(context.Context, []int64, [][32]byte) ([]Fetch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Fetch(nil), f.fetches...), nil
}

func (f *fakeStore) GateObjectsRead(_ context.Context, ds [][32]byte, _ int64) (map[[32]byte]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read, nil
}

func (f *fakeStore) GateSnapshot(context.Context) (*Snapshot, error) { return snapshotOf(), nil }

func (f *fakeStore) GatePrioritize(_ context.Context, _ [][32]byte, due []Fetch) error {
	f.asked.Add(1)
	f.mu.Lock()
	f.raised = append(f.raised, due...)
	f.mu.Unlock()
	return nil
}

func (f *fakeStore) RecordRefusal(_ context.Context, _ int64, _ [32]byte, _ string, r Reason) error {
	f.mu.Lock()
	f.refusals = append(f.refusals, r)
	f.mu.Unlock()
	return nil
}

func (f *fakeStore) set(rows ...Row) {
	f.mu.Lock()
	f.rows = rows
	f.mu.Unlock()
}

// recorder keeps the audit events.
type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) RecordTx(context.Context, meta.Tx, audit.Event) error { return nil }
func (r *recorder) Record(_ context.Context, ev audit.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

var (
	filesInternal = Repo{
		ID: 7, Name: qname.Repo{Project: "web", Key: "files-internal"}, Class: "local", Format: "generic", ProjectID: "p1",
		Setting: Setting{OnlyVerified: true},
	}
	fileBlob = [32]byte{1, 2, 3}
)

func newGate(st *fakeStore, rec *recorder, o Options) *Gate {
	o.Store, o.Enabled, o.Audit = st, true, rec
	if o.BaseURL == "" {
		o.BaseURL = "https://quader.example"
	}
	return New(o)
}

func dl(r Repo) Download {
	return Download{Addressed: r, Holder: r, Members: []Repo{r}, Path: "tools/tool.tar.gz", Blob: fileBlob, Source: audit.Source{ClientAddr: "192.0.2.1"}}
}

func problem(t *testing.T, err error) *errs.Error {
	t.Helper()
	e, ok := errs.As(err)
	if !ok {
		t.Fatalf("not a problem: %v", err)
	}
	return e
}

// TestR_SIG_064_AGateThatDoesNotApplyReadsNoRow: with the member off on both repositories, an item of a system repository and the
// signatures off, nothing is read; a generated item passes. Unit, milliseconds.
func TestR_SIG_064_AGateThatDoesNotApplyReadsNoRow(t *testing.T) {
	st := &fakeStore{}
	g := newGate(st, &recorder{}, Options{})
	off := filesInternal
	off.Setting = Setting{}
	system := filesInternal
	system.System = true
	for name, d := range map[string]Download{"off": dl(off), "system": dl(system)} {
		v, err := g.Decide(context.Background(), d)
		if err != nil || v.Outcome != Pass {
			t.Errorf("%s: %+v %v", name, v, err)
		}
	}
	gen := dl(filesInternal)
	gen.Generated = true
	if v, err := g.Decide(context.Background(), gen); err != nil || v.Outcome != Pass {
		t.Errorf("generated: %+v %v", v, err)
	}
	disabled := New(Options{Store: st, Enabled: false})
	if disabled.Applies(filesInternal, filesInternal) {
		t.Error("the gate applies while the signatures are off")
	}
	if n := st.reads.Load(); n != 0 {
		t.Errorf("%d reads of rows", n)
	}
}

// TestR_SIG_068_TheAnswers: a refusal is 403 signature_not_verified with the reason, the digest and Cache-Control: no-store, its text
// names the addressed repository and the file's page and no signer; it records one audit event a minute per repository, path and
// client address, counts every refusal and lists it once per artifact; a signature file whose scheme was read passes, one whose scheme
// was not is judged as an artifact; an attachment and a layer pass. Unit, milliseconds.
func TestR_SIG_068_TheAnswers(t *testing.T) {
	st := &fakeStore{read: map[[32]byte]bool{}}
	rec := &recorder{}
	kind := KindArtifact
	g := newGate(st, rec, Options{Classify: func(context.Context, Repo, string, [32]byte) (Subject, error) {
		return Subject{Kind: kind}, nil
	}})
	d := dl(filesInternal)
	d.Path = "tools/tool\n.tar.gz"
	var err error
	for range 2 {
		err = g.Check(context.Background(), d)
	}
	e := problem(t, err)
	switch {
	case e.Kind != errs.Forbidden || e.Code != errs.CodeSignatureNotVerified:
		t.Fatalf("%s %s", e.Kind, e.Code)
	case e.Headers["Cache-Control"] != "no-store" || e.Extensions["reason"] != "missing" || e.Extensions["sha256"] == nil:
		t.Fatalf("headers %v extensions %v", e.Headers, e.Extensions)
	case !strings.Contains(e.Detail, "of web/files-internal: it has no signature") || strings.Contains(e.Detail, "\n") ||
		!strings.Contains(e.Detail, "Details: https://quader.example/ui/projects/web/repositories/files-internal/files/tools/tool%0A.tar.gz"):
		t.Fatalf("detail %q", e.Detail)
	}
	if rec.count() != 1 || len(st.refusals) != 2 {
		t.Errorf("%d events, %d refusals listed", rec.count(), len(st.refusals))
	}
	if ev := rec.events[0]; ev.Type != audit.EventSignatureDownloadRefused || ev.Outcome != audit.Denied || ev.Repository != "web/files-internal" {
		t.Errorf("event %+v", ev)
	}
	st.set(valid(fileBlob, TrustUntrusted))
	if e := problem(t, g.Check(context.Background(), d)); e.Extensions["reason"] != "untrusted" || !strings.Contains(e.Detail, "no trust anchor this repository accepts knows its signer") {
		t.Errorf("untrusted: %v %q", e.Extensions, e.Detail)
	}
	st.set()
	kind = KindSignatureFile
	if err := g.Check(context.Background(), d); err == nil {
		t.Error("a signature file whose scheme was not read passed")
	}
	st.read[fileBlob] = true
	if err := g.Check(context.Background(), d); err != nil {
		t.Errorf("a signature file whose scheme was read: %v", err)
	}
	for _, k := range []Kind{KindAttachment, KindNone} {
		kind = k
		if err := g.Check(context.Background(), d); err != nil {
			t.Errorf("kind %d: %v", k, err)
		}
	}
}

// TestR_SIG_065_EachGateJudgesInItsViewAndScope: through a virtual repository both gates apply; the virtual one reads the attachments of
// its members the caller may read in its own project's scope, the member's its own in its own; a refusal of either refuses, and the text
// names the addressed repository. Unit, milliseconds.
func TestR_SIG_065_EachGateJudgesInItsViewAndScope(t *testing.T) {
	st := &fakeStore{}
	g := newGate(st, &recorder{}, Options{})
	member := Repo{
		ID: 3, Name: qname.Repo{Project: "web", Key: "npm-local"}, Class: "local", Format: "npm", ProjectID: "p1",
		Setting: Setting{OnlyVerified: true},
	}
	other := Repo{ID: 4, Name: qname.Repo{Project: "web", Key: "npm-remote"}, Class: "remote", Format: "npm", ProjectID: "p1"}
	virtual := Repo{
		ID: 9, Name: qname.Repo{Project: "ops", Key: "npm-all"}, Class: "virtual", Format: "npm", ProjectID: "p2",
		Setting: Setting{OnlyVerified: true},
	}
	d := Download{Addressed: virtual, Holder: member, Members: []Repo{member, other}, Path: "lodash/-/lodash-4.17.21.tgz", Blob: fileBlob}
	st.set(valid(fileBlob, TrustInternal, 3))
	v, err := g.Decide(context.Background(), d)
	if err != nil || v.Outcome != Pass {
		t.Fatalf("%+v %v", v, err)
	}
	if len(st.rowRepos) != 2 || len(st.rowRepos[0]) != 2 || st.rowScopes[0] != "p2" || st.rowRepos[1][0] != 3 || st.rowScopes[1] != "p1" {
		t.Errorf("views %v scopes %v", st.rowRepos, st.rowScopes)
	}
	st.set()
	err = g.Check(context.Background(), d)
	if e := problem(t, err); !strings.Contains(e.Detail, "of ops/npm-all:") || strings.Contains(e.Detail, "npm-local") {
		t.Errorf("detail %q", e.Detail)
	}
	ungatedMember := member
	ungatedMember.Setting = Setting{}
	d.Holder = ungatedMember
	st.rowRepos = nil
	st.set(valid(fileBlob, TrustInternal, 3))
	if v, _ := g.Decide(context.Background(), d); v.Outcome != Pass || len(st.rowRepos) != 1 {
		t.Errorf("only the virtual gate applies: %+v %v", v, st.rowRepos)
	}
}

// TestR_SIG_067_TheWait: a waiting download asks for the priority work and the due fetch's job, and is served when its signal arrives;
// without a signal it reads again once a poll; the wait ends at download_wait from the arrival, so no test sleeps 25 s; waits beyond
// max_waiting and every wait at a drain are answered at once; a decision that refuses ends the wait with the refusal. Unit, under a
// second, a fake store and the signal called by the test.
func TestR_SIG_067_TheWait(t *testing.T) {
	pending := Row{Subject: fileBlob, State: signatures.StatePending}
	t.Run("the signal serves it", func(t *testing.T) {
		st := &fakeStore{fetches: []Fetch{{RepoID: 7, Target: "app@sha256:01", Subject: fileBlob, Queued: true}}}
		st.set(pending)
		remote := filesInternal
		remote.Class = classRemote
		g := newGate(st, &recorder{}, Options{Wait: 5 * time.Second, Poll: time.Hour})
		done := make(chan error, 1)
		go func() { done <- g.Check(context.Background(), dl(remote)) }()
		deadline := time.Now().Add(2 * time.Second)
		for g.Waiting() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		st.mu.Lock()
		st.fetches = []Fetch{{RepoID: 7, Target: "app@sha256:01", Subject: fileBlob, Fetched: true}}
		st.mu.Unlock()
		st.set(valid(fileBlob, TrustInternal, 3))
		g.Signal([]string{"0102030000000000000000000000000000000000000000000000000000000000"}, false)
		if err := <-done; err != nil {
			t.Fatalf("served: %v", err)
		}
		if st.asked.Load() != 1 || len(st.raised) != 1 || st.raised[0].Target != "app@sha256:01" {
			t.Errorf("priority work %d, raised %v", st.asked.Load(), st.raised)
		}
	})
	t.Run("reading again once a poll serves it", func(t *testing.T) {
		st := &fakeStore{}
		st.set(pending)
		g := newGate(st, &recorder{}, Options{Wait: 5 * time.Second, Poll: 20 * time.Millisecond})
		go func() {
			time.Sleep(60 * time.Millisecond)
			st.set(valid(fileBlob, TrustPublic, 1))
		}()
		if err := g.Check(context.Background(), dl(filesInternal)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the bound counts from the arrival", func(t *testing.T) {
		st := &fakeStore{}
		st.set(pending)
		rec := &recorder{}
		g := newGate(st, rec, Options{Wait: 25 * time.Second, Poll: time.Hour})
		d := dl(filesInternal)
		d.Arrived = time.Now().Add(-25*time.Second + 100*time.Millisecond)
		started := time.Now()
		e := problem(t, g.Check(context.Background(), d))
		if e.Kind != errs.Unavailable || e.Code != errs.CodeSignatureCheckPending || e.RetryAfter != 30*time.Second ||
			e.Headers["Cache-Control"] != "no-store" || time.Since(started) > 2*time.Second {
			t.Fatalf("%s %s %v %v after %s", e.Kind, e.Code, e.RetryAfter, e.Headers, time.Since(started))
		}
		if e.Status() != http.StatusServiceUnavailable || len(st.refusals) != 0 || rec.count() != 1 {
			t.Errorf("status %d, %d refusals listed, %d events", e.Status(), len(st.refusals), rec.count())
		}
	})
	t.Run("max_waiting and the drain answer at once", func(t *testing.T) {
		st := &fakeStore{}
		st.set(pending)
		g := newGate(st, &recorder{}, Options{Wait: 5 * time.Second, MaxWaiting: 1, Poll: time.Hour})
		first := make(chan error, 1)
		go func() { first <- g.Check(context.Background(), dl(filesInternal)) }()
		deadline := time.Now().Add(2 * time.Second)
		for g.Waiting() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if e := problem(t, g.Check(context.Background(), dl(filesInternal))); e.Code != errs.CodeSignatureCheckPending {
			t.Errorf("beyond max_waiting: %s", e.Code)
		}
		g.Stop()
		if e := problem(t, <-first); e.Code != errs.CodeSignatureCheckPending {
			t.Errorf("at the drain: %s", e.Code)
		}
		if g.Waiting() != 0 {
			t.Errorf("%d still wait", g.Waiting())
		}
	})
	t.Run("a refusal ends the wait", func(t *testing.T) {
		st := &fakeStore{}
		st.set(pending)
		g := newGate(st, &recorder{}, Options{Wait: 5 * time.Second, Poll: 10 * time.Millisecond})
		go func() {
			time.Sleep(30 * time.Millisecond)
			st.set(Row{Subject: fileBlob, State: signatures.StateInvalid})
		}()
		if e := problem(t, g.Check(context.Background(), dl(filesInternal))); e.Extensions["reason"] != "invalid" {
			t.Errorf("%v", e.Extensions)
		}
	})
}
