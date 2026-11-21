package job

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/key"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
	"github.com/guided-traffic/quader/internal/signing/store"
)

// fakeTx and fakeRunner run a transaction function with nothing behind it: the job's statements are replaced by the test.
type fakeTx struct{ meta.Tx }

type fakeRunner struct{}

func (fakeRunner) InTx(ctx context.Context, fn func(tx meta.Tx) error) error { return fn(fakeTx{}) }

// fakeStore is the store of a job test: the identities, the due rows handed out in order, and what the job deferred and reported.
type fakeStore struct {
	mu       sync.Mutex
	ids      []signing.Identity
	due      []store.Work
	deferred map[string]deferral
	reports  map[string]int
	switched bool
	next     time.Time
	nextDue  time.Time
}

type deferral struct {
	at     time.Time
	failed bool
	code   string
}

func (f *fakeStore) Identities(context.Context) ([]signing.Identity, error) { return f.ids, nil }
func (f *fakeStore) SwitchDue(context.Context) (bool, time.Time, error) {
	return f.switched, f.next, nil
}
func (f *fakeStore) RetryFailedDaily(context.Context) (int64, error)  { return 0, nil }
func (f *fakeStore) Backlog(context.Context) ([]store.Backlog, error) { return nil, nil }
func (f *fakeStore) NextDue(context.Context, []string) (time.Time, error) {
	return f.nextDue, nil
}

func (f *fakeStore) Discovery(context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (f *fakeStore) SetDiscovery(context.Context, string, int64) error { return nil }

func (f *fakeStore) DueWork(_ context.Context, kinds []string, limit int, _ []string) ([]store.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Work
	for _, w := range f.due {
		if len(out) < limit && contains(kinds, w.Kind) {
			out = append(out, w)
		}
	}
	f.due = f.due[len(out):]
	return out, nil
}

func (f *fakeStore) DeferWork(_ context.Context, w store.Work, at time.Time, failed bool, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deferred[w.Key()] = deferral{at: at, failed: failed, code: code}
	return nil
}

func (f *fakeStore) ClaimFailedReport(_ context.Context, kind, code string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports[kind+"/"+code]++
	return f.reports[kind+"/"+code] == 1, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// recorder keeps the async events.
type recorder struct {
	audit.Nop
	mu  sync.Mutex
	evs []audit.Event
}

func (r *recorder) Record(_ context.Context, ev audit.Event) {
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
}

type runResult struct{ v any }

func (r *runResult) SetResult(v any) error { r.v = v; return nil }

// uploaded is a current uploaded identity whose key the holder has opened already.
func uploaded(t *testing.T, h *Holder) signing.Identity {
	t.Helper()
	k := key.Wrap(signingtest.Key(t))
	sha, err := k.KeySHA256()
	if err != nil {
		t.Fatal(err)
	}
	id := signing.Identity{ID: 7, Source: signing.SourceUpload, State: signing.StateCurrent, KeySHA256: sha, NotAfter: time.Now().Add(48 * time.Hour)}
	h.opened[id.ID] = k
	return id
}

func newTestJob(t *testing.T, st *fakeStore, kinds []Kind, hold func(context.Context, meta.Tx, int64) (time.Time, bool, error)) (*Job, *recorder) {
	t.Helper()
	rec := &recorder{}
	h := NewHolder(nil, nil)
	st.ids = []signing.Identity{uploaded(t, h)}
	j, err := New(Options{
		DB: fakeRunner{}, Store: st, Keys: h, Kinds: kinds, Enabled: true, Concurrency: 1, MaxAttempts: 3, Audit: rec,
		hold: hold, lock: func(context.Context, meta.Tx, store.Work) (bool, error) { return true, nil },
		done: func(context.Context, meta.Tx, store.Work) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return j, rec
}

func held(context.Context, meta.Tx, int64) (time.Time, bool, error) {
	return time.Now().Add(48 * time.Hour), true, nil
}

// TestR_SIG_049_TheJobRunsTheDueWorkInItsOrderWithTheCurrentKey: the rows the store hands out in its order (by priority, then time) are
// signed one after the other by their kind's Sign with the current identity and its key; a kind without a handler is not asked for.
func TestR_SIG_049_TheJobRunsTheDueWorkInItsOrderWithTheCurrentKey(t *testing.T) {
	st := &fakeStore{deferred: map[string]deferral{}, reports: map[string]int{}}
	st.due = []store.Work{
		{Kind: signing.WorkNPM, ItemID: 1, Priority: 0},
		{Kind: signing.WorkNPM, ItemID: 2, Priority: 0},
		{Kind: signing.WorkNPM, ItemID: 3, Priority: 3},
	}
	var order []int64
	var sawKey bool
	j, _ := newTestJob(t, st, []Kind{{Name: signing.WorkNPM, Sign: func(_ context.Context, _ meta.Tx, s Session, w store.Work) error {
		order = append(order, w.ItemID)
		sawKey = s.Key.Valid() && s.Identity.ID == 7
		return nil
	}}}, held)
	var res runResult
	if err := j.RunSigning(context.Background(), &res); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 || !sawKey {
		t.Fatalf("signed %v, key %v", order, sawKey)
	}
	if r := res.v.(result); r.Signed != 3 {
		t.Errorf("result %+v", r)
	}
}

// TestR_SIG_048_ASigningTransactionThatBeganBeforeASwitchIsRefused: the identity the job read was current; by the time its operation
// holds the row the switch made it previous (the shared lock finds no current row), so nothing is signed, the run stops, and the work
// stays due without spending an attempt. An expired certificate stops it the same way.
func TestR_SIG_048_ASigningTransactionThatBeganBeforeASwitchIsRefused(t *testing.T) {
	for name, hold := range map[string]func(context.Context, meta.Tx, int64) (time.Time, bool, error){
		"switched": func(context.Context, meta.Tx, int64) (time.Time, bool, error) { return time.Time{}, false, nil },
		"expired": func(context.Context, meta.Tx, int64) (time.Time, bool, error) {
			return time.Now().Add(-time.Second), true, nil
		},
	} {
		st := &fakeStore{deferred: map[string]deferral{}, reports: map[string]int{}}
		st.due = []store.Work{{Kind: signing.WorkNPM, ItemID: 1}, {Kind: signing.WorkNPM, ItemID: 2}}
		signed := 0
		j, _ := newTestJob(t, st, []Kind{{Name: signing.WorkNPM, Sign: func(context.Context, meta.Tx, Session, store.Work) error {
			signed++
			return nil
		}}}, hold)
		if err := j.RunSigning(context.Background(), &runResult{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if signed != 0 || len(st.deferred) != 0 {
			t.Errorf("%s: signed %d, deferred %v", name, signed, st.deferred)
		}
	}
}

// TestR_SIG_049_AnOperationThatCouldNotRunSpendsAnAttemptAndFailsAfterTheLast: the back-off doubles from a minute up to six hours; an
// error spends an attempt with the code of internal/errs, the last attempt makes the row failed and due again in a day, and signing.failed
// is recorded once per kind and code however many rows fail.
func TestR_SIG_049_AnOperationThatCouldNotRunSpendsAnAttemptAndFailsAfterTheLast(t *testing.T) {
	for attempt, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 10: 6 * time.Hour, 30: 6 * time.Hour} {
		if got := Backoff(attempt); got != want {
			t.Errorf("Backoff(%d) = %s, want %s", attempt, got, want)
		}
	}
	st := &fakeStore{deferred: map[string]deferral{}, reports: map[string]int{}}
	st.due = []store.Work{
		{Kind: signing.WorkAttestation, ItemID: 1, Attempts: 0},
		{Kind: signing.WorkAttestation, ItemID: 2, Attempts: 2},
		{Kind: signing.WorkAttestation, ItemID: 3, Attempts: 2},
	}
	j, rec := newTestJob(t, st, []Kind{{Name: signing.WorkAttestation, Sign: func(context.Context, meta.Tx, Session, store.Work) error {
		return errs.E(errs.Unavailable, "object_store_unavailable", "the object store is down")
	}}}, held)
	start := time.Now()
	if err := j.RunSigning(context.Background(), &runResult{}); err != nil {
		t.Fatal(err)
	}
	first := st.deferred["attestation:1"]
	if first.failed || first.code != "object_store_unavailable" || first.at.Sub(start) < time.Minute || first.at.Sub(start) > 2*time.Minute {
		t.Errorf("the first attempt: %+v", first)
	}
	for _, k := range []string{"attestation:2", "attestation:3"} {
		if d := st.deferred[k]; !d.failed || d.at.Sub(start) < 23*time.Hour {
			t.Errorf("%s after the last attempt: %+v", k, d)
		}
	}
	if len(rec.evs) != 1 || rec.evs[0].Type != audit.EventSigningFailed || !strings.Contains(string(rec.evs[0].Details), `"code":"object_store_unavailable"`) {
		t.Errorf("events %+v", rec.evs)
	}
}

// TestR_SIG_049_WithoutAKindOrWithSigningOffTheJobOnlySwitches: the frame runs with no package registered yet, and signing.enabled false
// takes no work; both still reschedule the switch at the end of an announce period.
func TestR_SIG_049_WithoutAKindOrWithSigningOffTheJobOnlySwitches(t *testing.T) {
	st := &fakeStore{deferred: map[string]deferral{}, reports: map[string]int{}, next: time.Now().Add(5 * time.Second)}
	var woke time.Duration
	h := NewHolder(nil, nil)
	j, err := New(Options{DB: fakeRunner{}, Store: st, Keys: h, Enabled: true, Wake: func(_ context.Context, d time.Duration) error {
		woke = d
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var res runResult
	if err := j.RunSigning(context.Background(), &res); err != nil {
		t.Fatal(err)
	}
	if r := res.v.(result); r.Skipped == "" || woke < 4*time.Second || woke > 5*time.Second {
		t.Errorf("result %+v, woken in %s", r, woke)
	}
	if _, err := New(Options{DB: fakeRunner{}, Store: st, Keys: h, Kinds: []Kind{{Name: "npm"}}}); err == nil {
		t.Error("a kind without Sign was taken")
	}
}

// TestR_SIG_048_AKindFollowsTheIdentitiesStatesInEveryRun: a kind's Served is called once per run with every identity before any work,
// also with signing off and with no identity current, because a removal leaves no identity to sign with and still changes what the
// documents serve (Q-SIG-160); its error fails the run. Unit, milliseconds.
func TestR_SIG_048_AKindFollowsTheIdentitiesStatesInEveryRun(t *testing.T) {
	for name, tc := range map[string]struct {
		enabled bool
		ids     []signing.Identity
	}{
		"signing on":          {true, []signing.Identity{{ID: 1, State: signing.StatePrevious}, {ID: 2, State: signing.StateCurrent}}},
		"signing off":         {false, []signing.Identity{{ID: 2, State: signing.StateCurrent}}},
		"no identity current": {true, []signing.Identity{{ID: 1, State: signing.StatePrevious}}},
	} {
		st := &fakeStore{deferred: map[string]deferral{}, reports: map[string]int{}, ids: tc.ids}
		var seen [][]signing.Identity
		kind := Kind{
			Name: signing.WorkNPM, Sign: func(context.Context, meta.Tx, Session, store.Work) error { return nil },
			Served: func(_ context.Context, ids []signing.Identity) error { seen = append(seen, ids); return nil },
		}
		j, err := New(Options{DB: fakeRunner{}, Store: st, Keys: NewHolder(nil, nil), Kinds: []Kind{kind}, Enabled: tc.enabled})
		if err != nil {
			t.Fatal(err)
		}
		if err := j.RunSigning(context.Background(), &runResult{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(seen) != 1 || len(seen[0]) != len(tc.ids) {
			t.Errorf("%s: Served saw %v", name, seen)
		}
	}
	st := &fakeStore{deferred: map[string]deferral{}, reports: map[string]int{}}
	failing := Kind{
		Name: signing.WorkNPM, Sign: func(context.Context, meta.Tx, Session, store.Work) error { return nil },
		Served: func(context.Context, []signing.Identity) error { return errs.E(errs.Unavailable, "db", "down") },
	}
	j, err := New(Options{DB: fakeRunner{}, Store: st, Keys: NewHolder(nil, nil), Kinds: []Kind{failing}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RunSigning(context.Background(), &runResult{}); err == nil {
		t.Error("a run whose Served failed passed")
	}
}

// fileStore is the store of a watcher test: what it registered and refused.
type fileStore struct {
	ids        []signing.Identity
	registered []signing.Checked
	refusals   map[[32]byte]*signing.Refusal
	gone       int
}

func (f *fileStore) Identities(context.Context) ([]signing.Identity, error) { return f.ids, nil }
func (f *fileStore) Backlog(context.Context) ([]store.Backlog, error)       { return nil, nil }
func (f *fileStore) ConfigurationGone(context.Context) (bool, error)        { f.gone++; return true, nil }

func (f *fileStore) RegisterFiles(_ context.Context, c signing.Checked) (signing.Identity, bool, error) {
	f.registered = append(f.registered, c)
	return signing.Identity{ID: int64(len(f.registered))}, true, nil
}

func (f *fileStore) RecordRefusal(_ context.Context, sha [32]byte, r *signing.Refusal, _ string) error {
	f.refusals[sha] = r
	return nil
}

// TestR_SIG_046_TheWatcherLoadsRotatesAndRefusesTheFilesWithoutARestart: a pair in the files is checked and registered with its key in
// the holder; an unchanged look registers nothing; a pair of a new key written over the files is registered while the old key stays in
// the holder until the switch; a pair with an RSA key is refused once per content and the keys held stay; without files the start lets
// the identity of the files go.
func TestR_SIG_046_TheWatcherLoadsRotatesAndRefusesTheFilesWithoutARestart(t *testing.T) {
	dir := t.TempDir()
	files := Files{Cert: filepath.Join(dir, "tls.crt"), Key: filepath.Join(dir, "tls.key"), CA: filepath.Join(dir, "ca.crt"), Interval: time.Hour}
	ca := signingtest.NewCA(t, "Example")
	write := func(chain, keyPEM []byte) {
		t.Helper()
		for path, b := range map[string][]byte{files.Cert: chain, files.Key: keyPEM, files.CA: ca.RootPEM()} {
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	st := &fileStore{refusals: map[[32]byte]*signing.Refusal{}}
	h := NewHolder(nil, nil)
	w := NewWatcher(WatcherOptions{Store: st, Keys: h, Files: files})
	ctx := context.Background()

	leafA, keyA := ca.Issue(t, signingtest.Leaf{})
	write(ca.ChainPEM(leafA), signingtest.KeyPEM(t, keyA))
	w.Look(ctx, true)
	w.Look(ctx, false)
	if len(st.registered) != 1 || !st.registered[0].Leaf.Equal(leafA) || len(st.refusals) != 0 {
		t.Fatalf("registered %d, refused %d", len(st.registered), len(st.refusals))
	}
	shaA := sha(t, key.Wrap(keyA))
	if _, ok, _ := h.For(ctx, signing.Identity{Source: signing.SourceConfiguration, KeySHA256: shaA}); !ok {
		t.Fatal("the holder lacks the key of the files")
	}

	leafB, keyB := ca.Issue(t, signingtest.Leaf{})
	write(ca.ChainPEM(leafB), signingtest.KeyPEM(t, keyB))
	w.Look(ctx, false)
	if len(st.registered) != 2 || !st.registered[1].Leaf.Equal(leafB) {
		t.Fatalf("the new pair was not registered: %d", len(st.registered))
	}
	for name, k := range map[string]key.Private{"old": key.Wrap(keyA), "new": key.Wrap(keyB)} {
		if _, ok, _ := h.For(ctx, signing.Identity{Source: signing.SourceConfiguration, KeySHA256: sha(t, k)}); !ok {
			t.Errorf("the holder lacks the %s key", name)
		}
	}

	rsaLeaf, rsaKey := rsaPair(t, ca)
	write(ca.ChainPEM(rsaLeaf), rsaKey)
	w.Look(ctx, false)
	w.Look(ctx, false)
	if len(st.registered) != 2 || len(st.refusals) != 1 {
		t.Fatalf("registered %d, refusals %d", len(st.registered), len(st.refusals))
	}
	for _, r := range st.refusals {
		if r.Check != signing.CheckKeyType {
			t.Errorf("the refusal is %s: %s", r.Check, r.Detail)
		}
	}
	if _, ok, _ := h.For(ctx, signing.Identity{Source: signing.SourceConfiguration, KeySHA256: sha(t, key.Wrap(keyB))}); !ok {
		t.Error("the refused pair dropped the key in use")
	}

	none := NewWatcher(WatcherOptions{Store: st, Keys: NewHolder(nil, nil)})
	none.Look(ctx, true)
	none.Look(ctx, false)
	if st.gone != 1 {
		t.Errorf("ConfigurationGone ran %d times", st.gone)
	}
	if err := os.Remove(files.Key); err != nil {
		t.Fatal(err)
	}
	w.Look(ctx, false)
	unreadable := false
	for _, r := range st.refusals {
		unreadable = unreadable || r.Check == CheckUnreadable
	}
	if !unreadable {
		t.Error("a missing key file is not refused as unreadable")
	}
}

func sha(t *testing.T, k key.Private) [32]byte {
	t.Helper()
	s, err := k.KeySHA256()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestR_SIG_047_AKeyRendersNothing: the private key's String, GoString, JSON, text and log value are the redaction marker.
func TestR_SIG_047_AKeyRendersNothing(t *testing.T) {
	k := key.Wrap(signingtest.Key(t))
	j, _ := k.MarshalJSON()
	txt, _ := k.MarshalText()
	for _, s := range []string{k.String(), k.GoString(), string(j), string(txt), k.LogValue().String()} {
		if !strings.Contains(s, "REDACTED") {
			t.Errorf("a key renders %q", s)
		}
	}
}

// rsaPair is a code-signing leaf of the CA for an RSA key and the key's PEM: a pair R-SIG-045 refuses.
func rsaPair(t *testing.T, ca *signingtest.CA) (*x509.Certificate, []byte) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := ca.Issue(t, signingtest.Leaf{Public: &k.PublicKey})
	return leaf, signingtest.KeyPEM(t, k)
}

// TestR_SIG_053_AnOperationThatWaitsOrHasNothingToSignSpendsNoAttempt: a row whose Sign answers signing.Later is postponed to its time
// in the operation's transaction and spends no attempt; one that answers signing.ErrNothing is done and counted as no signature (an item
// that is no subject, an attestation as it should be). Unit, milliseconds.
func TestR_SIG_053_AnOperationThatWaitsOrHasNothingToSignSpendsNoAttempt(t *testing.T) {
	st := &fakeStore{deferred: map[string]deferral{}, reports: map[string]int{}}
	st.due = []store.Work{
		{Kind: signing.WorkAttestation, ItemID: 1, Priority: 1},
		{Kind: signing.WorkAttestation, ItemID: 2, Priority: 1},
		{Kind: signing.WorkAttestation, ItemID: 3, Priority: 1},
	}
	until := time.Now().Add(time.Minute)
	j, _ := newTestJob(t, st, []Kind{{Name: signing.WorkAttestation, Sign: func(_ context.Context, _ meta.Tx, _ Session, w store.Work) error {
		switch w.ItemID {
		case 1:
			return &signing.Later{Until: until}
		case 2:
			return signing.ErrNothing
		}
		return nil
	}}}, held)
	var postponed []int64
	var done []int64
	j.o.postpone = func(_ context.Context, _ meta.Tx, w store.Work, at time.Time) error {
		if !at.Equal(until) {
			t.Errorf("postponed to %v, want %v", at, until)
		}
		postponed = append(postponed, w.ItemID)
		return nil
	}
	j.o.done = func(_ context.Context, _ meta.Tx, w store.Work) error { done = append(done, w.ItemID); return nil }
	var res runResult
	if err := j.RunSigning(context.Background(), &res); err != nil {
		t.Fatal(err)
	}
	if len(postponed) != 1 || postponed[0] != 1 || len(done) != 2 || len(st.deferred) != 0 {
		t.Fatalf("postponed %v, done %v, attempts spent %v", postponed, done, st.deferred)
	}
	if r := res.v.(result); r.Signed != 1 {
		t.Errorf("result %+v: only the third row signed", r)
	}
}
