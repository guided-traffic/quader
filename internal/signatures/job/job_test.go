package job

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
	"github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/internal/signatures/trust"
)

// runner runs a transaction function without a database: the fake store takes the nil transaction.
type runner struct{}

func (runner) InTx(_ context.Context, fn func(tx meta.Tx) error) error { return fn(nil) }

type deferred struct {
	id        int64
	spend     int
	notBefore time.Time
	due       bool
	state     signatures.State
	reason    string
}

// fakeStore records what the job writes; the methods a test does not reach panic through the nil interface.
type fakeStore struct {
	Store
	mu        sync.Mutex
	ordered   bool
	queue     []store.Check
	before    signatures.State
	defers    []deferred
	results   map[int64]signatures.Result
	invalids  []int64
	parts     map[int64]int
	trustRows []store.TrustRow
	deleted   []int64
	integrity map[int64]signatures.State
	sigs      map[int64]trust.Signature
	reach     map[int64][]string
	failed    []string
	stored    []shipped.Refreshed
}

func newFake() *fakeStore {
	return &fakeStore{
		results: map[int64]signatures.Result{}, parts: map[int64]int{}, integrity: map[int64]signatures.State{},
		sigs: map[int64]trust.Signature{}, reach: map[int64][]string{},
	}
}

func (f *fakeStore) DueChecks(_ context.Context, limit int, inFlight []int64) ([]store.Check, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	queue := slices.Clone(f.queue)
	if f.ordered {
		byOrder(queue) // as SignatureDueRows orders them
	}
	var out []store.Check
	for _, c := range queue {
		if !slices.Contains(inFlight, c.Signature.ID) && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) take(id int64) {
	f.queue = slices.DeleteFunc(f.queue, func(c store.Check) bool { return c.Signature.ID == id })
}

func (f *fakeStore) WriteCheck(_ context.Context, _ meta.Tx, id int64, r signatures.Result, _ time.Time) (signatures.State, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[id] = r
	f.take(id)
	return f.before, true, nil
}

func (f *fakeStore) Defer(_ context.Context, _ meta.Tx, id int64, spend int, notBefore time.Time, due bool, state signatures.State, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defers = append(f.defers, deferred{id: id, spend: spend, notBefore: notBefore, due: due, state: state, reason: reason})
	f.take(id)
	return nil
}

func (f *fakeStore) AddParts(_ context.Context, _ meta.Tx, id int64, parts int) (int64, error) {
	f.parts[id] = parts
	return int64(parts - 1), nil
}

func (f *fakeStore) RecordInvalid(_ context.Context, _ meta.Tx, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalids = append(f.invalids, id)
	return nil
}

func (f *fakeStore) UpstreamKeys(context.Context, []int64) (map[int64]scheme.Keys, error) {
	return map[int64]scheme.Keys{}, nil
}

func (f *fakeStore) TrustOf(_ context.Context, _ meta.Tx, ids []int64) ([]trust.Signature, error) {
	var out []trust.Signature
	for _, id := range ids {
		if s, ok := f.sigs[id]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) ReachingProjects(_ context.Context, _ meta.Tx, ids []int64, projects []string) (map[int64][]string, error) {
	out := map[int64][]string{}
	for _, id := range ids {
		for _, p := range f.reach[id] {
			if slices.Contains(projects, p) {
				out[id] = append(out[id], p)
			}
		}
	}
	return out, nil
}

func (f *fakeStore) WriteTrust(_ context.Context, _ meta.Tx, _ int64, _ []int64, rows []store.TrustRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trustRows = append(f.trustRows, rows...)
	return nil
}

func (f *fakeStore) DeleteTrust(_ context.Context, _ meta.Tx, ids []int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, ids...)
	return nil
}

func (f *fakeStore) SetIntegrity(_ context.Context, _ meta.Tx, id int64, state signatures.State, _ string) (bool, error) {
	f.integrity[id] = state
	return true, nil
}

func (f *fakeStore) TrustRootFailed(_ context.Context, code, _ string) error {
	f.failed = append(f.failed, code)
	return nil
}

func (f *fakeStore) StoreTrustRoot(_ context.Context, r shipped.Refreshed) (bool, error) {
	f.stored = append(f.stored, r)
	return true, nil
}

func (f *fakeStore) Stats(context.Context) (store.Stats, error) { return store.Stats{}, nil }

type nopRun struct{ result any }

func (r *nopRun) SetResult(v any) error {
	r.result = v
	return nil
}

var t0 = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func newJob(t *testing.T, f *fakeStore, o Options) *Job {
	t.Helper()
	o.DB, o.Store = runner{}, f
	o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	if o.Now == nil {
		o.Now = func() time.Time { return t0 }
	}
	j, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func due(id int64, priority int, notBefore time.Time) store.Check {
	return store.Check{
		Signature:     signatures.Signature{ID: id, Priority: priority, NotBefore: notBefore, Role: signatures.RolePublisher, State: signatures.StatePending},
		Object:        signatures.Object{Scheme: signatures.SchemeNotationJWS, Size: 10, Raw: []byte("x")},
		SubjectStored: true,
	}
}

// TestR_SIG_011_TheOrderTheBackoffAndTheAttempts: the due rows are worked by priority, then not_before, then id; a check that could not
// run spends an attempt and is due again after a doubling back-off from one minute; after the last attempt it is failed.
func TestR_SIG_011_TheOrderTheBackoffAndTheAttempts(t *testing.T) {
	t.Run("order by priority and time", func(t *testing.T) {
		cs := []store.Check{
			due(5, 3, t0), due(4, 1, t0.Add(time.Minute)), due(3, 1, t0), due(2, 0, t0.Add(time.Hour)), due(1, 1, t0),
		}
		byOrder(cs)
		var got []int64
		for _, c := range cs {
			got = append(got, c.Signature.ID)
		}
		if want := []int64{2, 1, 3, 4, 5}; !slices.Equal(got, want) {
			t.Fatalf("order %v, want %v", got, want)
		}
	})
	t.Run("the loop asks again for every free slot, so an upload that arrives meanwhile goes first", func(t *testing.T) {
		f := newFake()
		f.ordered = true
		f.queue = []store.Check{due(1, 3, t0), due(2, 1, t0), due(3, 1, t0.Add(-time.Minute))}
		var order []int64
		j := newJob(t, f, Options{Concurrency: 1})
		j.check = func(_ context.Context, in scheme.Input, _ time.Duration) (scheme.Output, error) {
			f.mu.Lock()
			order = append(order, in.ID)
			if in.ID == 3 {
				f.queue = append(f.queue, due(4, signatures.PriorityUpload, t0))
			}
			f.mu.Unlock()
			return scheme.Output{Result: signatures.Result{State: signatures.StateUnsupported, Role: in.Role}}, nil
		}
		w := &work{j: j, sc: buildScopes(store.TrustMaterial{}, j.o.Log)}
		n, more, err := w.loop(context.Background(), time.Time{})
		if err != nil || more || n != 4 {
			t.Fatalf("loop: %d, %v, %v", n, more, err)
		}
		if want := []int64{3, 4, 2, 1}; !slices.Equal(order, want) {
			t.Fatalf("checked %v, want %v", order, want)
		}
	})
	t.Run("the slice ends the loop and leaves the rest", func(t *testing.T) {
		f := newFake()
		f.queue = []store.Check{due(1, 1, t0), due(2, 1, t0)}
		now := t0
		j := newJob(t, f, Options{Concurrency: 1, Now: func() time.Time { return now }})
		j.check = func(_ context.Context, in scheme.Input, _ time.Duration) (scheme.Output, error) {
			now = now.Add(time.Hour)
			return scheme.Output{Result: signatures.Result{State: signatures.StateUnsupported, Role: in.Role}}, nil
		}
		w := &work{j: j, sc: buildScopes(store.TrustMaterial{}, j.o.Log)}
		n, more, err := w.loop(context.Background(), t0.Add(time.Minute))
		if err != nil || !more || n != 1 {
			t.Fatalf("loop: %d, %v, %v", n, more, err)
		}
	})
	t.Run("back-off", func(t *testing.T) {
		for attempt, want := range map[int]time.Duration{
			1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 4: 8 * time.Minute, 10: 6 * time.Hour, 40: 6 * time.Hour,
		} {
			if got := Backoff(attempt); got != want {
				t.Errorf("Backoff(%d) = %s, want %s", attempt, got, want)
			}
		}
	})
	t.Run("attempts", func(t *testing.T) {
		f := newFake()
		j := newJob(t, f, Options{MaxAttempts: 3})
		j.check = func(context.Context, scheme.Input, time.Duration) (scheme.Output, error) {
			return scheme.Output{}, scheme.ErrTransient
		}
		w := &work{j: j, sc: buildScopes(store.TrustMaterial{}, j.o.Log)}
		first := due(1, 1, t0)
		last := due(2, 1, t0)
		last.Signature.Attempts = 2
		for _, c := range []store.Check{first, last} {
			if err := w.one(context.Background(), c); err != nil {
				t.Fatal(err)
			}
		}
		want := []deferred{
			{id: 1, spend: 1, notBefore: t0.Add(time.Minute), due: true, state: signatures.StatePending},
			{id: 2, spend: 1, due: false, state: signatures.StateFailed},
		}
		if !slices.Equal(f.defers, want) {
			t.Fatalf("deferred %+v, want %+v", f.defers, want)
		}
	})
}

// TestR_SIG_011_TheDecisionsOfOneRow: a subject that is not stored waits (pending, subject_missing, not due) without a check, except
// npm's registry signature, whose subject is its message; an invalid verdict records signature.invalid once; a verdict whose object
// holds more parts adds their rows; a verdict without a trust step removes the signature's trust rows.
func TestR_SIG_011_TheDecisionsOfOneRow(t *testing.T) {
	checked := 0
	setup := func(state signatures.State, parts int) (*fakeStore, *work) {
		f := newFake()
		j := newJob(t, f, Options{})
		j.check = func(_ context.Context, in scheme.Input, _ time.Duration) (scheme.Output, error) {
			checked++
			return scheme.Output{Result: signatures.Result{State: state, Role: in.Role}, Parts: parts}, nil
		}
		return f, &work{j: j, sc: buildScopes(store.TrustMaterial{Epoch: 3}, j.o.Log)}
	}
	t.Run("subject missing waits", func(t *testing.T) {
		f, w := setup(signatures.StateValid, 0)
		c := due(1, 1, t0)
		c.SubjectStored = false
		if err := w.one(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		want := []deferred{{id: 1, due: false, state: signatures.StatePending, reason: signatures.ReasonSubjectMissing}}
		if !slices.Equal(f.defers, want) || checked != 0 {
			t.Fatalf("deferred %+v, checked %d", f.defers, checked)
		}
	})
	t.Run("npm's message needs no blob", func(t *testing.T) {
		c := due(1, 1, t0)
		c.SubjectStored, c.Signature.SubjectName, c.Signature.SignedDigest = false, "left-pad@1.3.0", make([]byte, 32)
		if waitsForSubject(c) {
			t.Fatal("an npm registry signature waits for a blob")
		}
	})
	t.Run("invalid records the event once", func(t *testing.T) {
		f, w := setup(signatures.StateInvalid, 0)
		if err := w.one(context.Background(), due(7, 1, t0)); err != nil {
			t.Fatal(err)
		}
		f.before = signatures.StateInvalid
		if err := w.one(context.Background(), due(7, 1, t0)); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(f.invalids, []int64{7}) {
			t.Fatalf("signature.invalid for %v, want once for 7", f.invalids)
		}
		if !slices.Equal(f.deleted, []int64{7, 7}) {
			t.Fatalf("trust rows removed for %v", f.deleted)
		}
	})
	t.Run("parts and trust rows", func(t *testing.T) {
		f, w := setup(signatures.StateValid, 3)
		f.sigs[9] = trust.Signature{ID: 9, State: signatures.StateValid, ReceivedAt: t0}
		if err := w.one(context.Background(), due(9, 1, t0)); err != nil {
			t.Fatal(err)
		}
		if f.parts[9] != 3 {
			t.Fatalf("parts %v", f.parts)
		}
		if len(f.trustRows) != 1 || f.trustRows[0].State != trust.StateUntrusted || f.trustRows[0].Project != "" {
			t.Fatalf("trust rows %+v", f.trustRows)
		}
	})
	t.Run("the receipt is the earlier of object and subject", func(t *testing.T) {
		c := due(1, 1, t0)
		c.Object.FirstSeenAt, c.SubjectCreatedAt = t0, t0.Add(-time.Hour)
		if got := receivedAt(c); !got.Equal(t0.Add(-time.Hour)) {
			t.Fatalf("received %s", got)
		}
	})
}

// ecdsaAnchor is a public-key anchor of a key made by the test, with npm's keyid.
func ecdsaAnchor(t *testing.T, id int64, scope string) (*ecdsa.PrivateKey, anchors.Anchor) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	m := anchors.Material{PublicKey: &anchors.PublicKeyMaterial{Keys: []anchors.PublicKeyEntry{{
		PEM: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), KeyID: testKeyID,
	}}}}
	p, err := anchors.Parse(anchors.KindPublicKey, m)
	if err != nil {
		t.Fatal(err)
	}
	return k, anchors.Anchor{ID: id, Scope: scope, Key: "k", Kind: anchors.KindPublicKey, Enabled: true, Material: m, Parsed: p}
}

func noKey(t *testing.T, id int64, k *ecdsa.PrivateKey, msg string) trust.Signature {
	t.Helper()
	d := sha256.Sum256([]byte(msg))
	sig, err := ecdsa.SignASN1(rand.Reader, k, d[:])
	if err != nil {
		t.Fatal(err)
	}
	other := sha256.Sum256([]byte("another message"))
	return trust.Signature{
		ID: id, Scheme: signatures.SchemeNPMRegistry, State: signatures.StateNoKey, SignedDigest: other[:], DigestAlg: "sha256",
		ReceivedAt: t0, KeyIDs: []string{testKeyID},
		Evidence: &scheme.Evidence{Key: &scheme.KeyEvidence{Kind: rawkey.KindECDSA, Signature: sig, KeyID: testKeyID, Hash: "sha256"}},
	}
}

// TestR_SIG_012_TheTrustStepPerSignature: the scopes are the installation's and one per project with anchors of its own while the
// switch is on; a signature gets a row in the installation's scope and in each project scope that reaches it; a block wins over every
// anchor; a no_key signature an anchor's key verifies becomes valid, one the key the issuer names refutes becomes invalid with no trust
// row and its event.
func TestR_SIG_012_TheTrustStepPerSignature(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	key, projectAnchor := ecdsaAnchor(t, 11, anchors.ScopeProject)
	material := func(switchOn bool) store.TrustMaterial {
		return store.TrustMaterial{
			Epoch: 4, ProjectAnchors: switchOn,
			Anchors: []store.ScopedAnchor{{Anchor: projectAnchor, Project: "p-web"}},
		}
	}
	t.Run("scopes follow the switch", func(t *testing.T) {
		if sc := buildScopes(material(true), log); !slices.Equal(sc.projectIDs, []string{"p-web"}) || len(sc.inst.Anchors) != 0 ||
			len(sc.keys.Public) != 1 {
			t.Fatalf("scopes with the switch on: %v, %d installation anchors, %d keys", sc.projectIDs, len(sc.inst.Anchors), len(sc.keys.Public))
		}
		if sc := buildScopes(material(false), log); len(sc.projectIDs) != 0 {
			t.Fatalf("scopes with the switch off: %v", sc.projectIDs)
		}
	})
	t.Run("a project's key makes a no_key signature valid and internal in the project only", func(t *testing.T) {
		sc := buildScopes(material(true), log)
		sig := noKey(t, 1, key, "m")
		d := sha256.Sum256([]byte("m"))
		sig.SignedDigest = d[:]
		got := decide(sig, sc, []string{"p-web", "p-gone"})
		if got.integrity != signatures.StateValid || len(got.rows) != 2 {
			t.Fatalf("decision %+v", got)
		}
		if got.rows[0].Project != "" || got.rows[0].State != trust.StateUntrusted {
			t.Errorf("installation row %+v", got.rows[0])
		}
		if got.rows[1].Project != "p-web" || got.rows[1].State != trust.StateInternal || !slices.Equal(got.rows[1].AnchorIDs, []int64{11}) {
			t.Errorf("project row %+v", got.rows[1])
		}
	})
	t.Run("a refuting key makes it invalid, without rows, with its event", func(t *testing.T) {
		f := newFake()
		sc := buildScopes(material(true), log)
		f.sigs[2] = noKey(t, 2, key, "signed something else")
		f.reach[2] = []string{"p-web"}
		if err := applyTrust(context.Background(), nil, f, sc, []int64{2}, nil); err != nil {
			t.Fatal(err)
		}
		if f.integrity[2] != signatures.StateInvalid || !slices.Equal(f.invalids, []int64{2}) || !slices.Equal(f.deleted, []int64{2}) ||
			len(f.trustRows) != 0 {
			t.Fatalf("integrity %v, events %v, removed %v, rows %+v", f.integrity, f.invalids, f.deleted, f.trustRows)
		}
	})
	t.Run("a block wins", func(t *testing.T) {
		sc := buildScopes(material(true), log)
		sc.blocks = []trust.Block{{ID: 5, Kind: trust.BlockKeyID, Value: testKeyID}}
		sig := noKey(t, 3, key, "m")
		d := sha256.Sum256([]byte("m"))
		sig.SignedDigest = d[:]
		got := decide(sig, sc, []string{"p-web"})
		for _, r := range got.rows {
			if r.State != trust.StateBlocked || r.BlockID != 5 {
				t.Errorf("row %+v is not blocked", r)
			}
		}
	})
}

// TestR_SIG_017_TheTrustRootTask: the task is skipped while refresh is off; a failure is kept and the run ends without an error; a
// refresh hands the store its targets.
func TestR_SIG_017_TheTrustRootTask(t *testing.T) {
	f := newFake()
	j := newJob(t, f, Options{})
	r := &nopRun{}
	if err := j.RunTrustRoot(context.Background(), r); err != nil || len(f.failed) != 0 {
		t.Fatalf("refresh off: %v, %v", err, f.failed)
	}
	j = newJob(t, f, Options{TrustRoot: TrustRootOptions{Refresh: true}})
	j.refresh = func(context.Context, shipped.RefreshOptions) (shipped.Refreshed, error) {
		return shipped.Refreshed{}, errors.New("the TUF repository answered 503")
	}
	if err := j.RunTrustRoot(context.Background(), r); err != nil || !slices.Equal(f.failed, []string{codeRefreshFailed}) {
		t.Fatalf("failure: %v, %v", err, f.failed)
	}
	j.refresh = func(_ context.Context, o shipped.RefreshOptions) (shipped.Refreshed, error) {
		if o.URL != shipped.DefaultURL {
			t.Errorf("url %q", o.URL)
		}
		return shipped.Refreshed{TUFVersion: 99}, nil
	}
	if err := j.RunTrustRoot(context.Background(), r); err != nil || len(f.stored) != 1 || f.stored[0].TUFVersion != 99 {
		t.Fatalf("refresh: %v, %+v", err, f.stored)
	}
}

// TestR_SIG_039_TheMetricsAreRegistered: the eight families of R-SIG-039 register once, a second registration hands them back, and the
// gauges and the remote counter record.
func TestR_SIG_039_TheMetricsAreRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := NewMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := NewMetrics(reg); err != nil || again.checks != m.checks {
		t.Fatalf("second registration: %v", err)
	}
	m.check(signatures.SchemeOpenPGP, string(signatures.StateValid), time.Millisecond)
	m.RemoteRequest("oci", "ok")
	m.set(gaugesOf(store.Stats{Due: [4]int64{1, 2, 3, 4}, Invalid: 5, Stale: 6, RootFetchedAt: t0.Add(-time.Hour)}, t0))
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	for _, want := range []string{
		"quader_signatures_checks_total", "quader_signatures_check_duration_seconds", "quader_signatures_backlog",
		"quader_signatures_oldest_pending_seconds", "quader_signatures_invalid", "quader_signatures_trust_rows_stale",
		"quader_signatures_remote_requests_total", "quader_signatures_trust_root_age_seconds",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("%s is not registered", want)
		}
	}
	gauge := func(name, priority string) float64 {
		for _, f := range families {
			if f.GetName() != name {
				continue
			}
			for _, s := range f.GetMetric() {
				if priority == "" || (len(s.GetLabel()) == 1 && s.GetLabel()[0].GetValue() == priority) {
					return s.GetGauge().GetValue()
				}
			}
		}
		return -1
	}
	if got := gauge("quader_signatures_trust_root_age_seconds", ""); got != 3600 {
		t.Errorf("trust root age %v, want 3600", got)
	}
	if got := gauge("quader_signatures_backlog", "3"); got != 4 {
		t.Errorf("backlog of priority 3 %v, want 4", got)
	}
}

// testKeyID is npm's keyid of the test's keys: "SHA256:" and the base64 of 32 bytes.
var testKeyID = "SHA256:" + base64.StdEncoding.EncodeToString(make([]byte, 32))
