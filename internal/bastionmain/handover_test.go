package bastionmain

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/journal"
)

// TestR_BAS_072_TheRecognitionReadsTheOffer: the offer of a forwarded GET reaches the serve point in the Recognition and the
// header reaches no stage behind the recognition; a HEAD, an interface call, a value the interface refuses and two values are
// no offer; an ordinary request that sends the header passes on without it (R-BAS-073, storage.md R-STOR-095 step 1).
func TestR_BAS_072_TheRecognitionReadsTheOffer(t *testing.T) {
	recognized := func(method, path string, offers ...string) (Recognition, bool, *http.Request) {
		t.Helper()
		r := forwarded(path, iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version())
		r.Method = method
		for _, o := range offers {
			r.Header.Add(iface.HeaderHandover, o)
		}
		w, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, r)
		if w.Code != http.StatusOK || seen == nil {
			t.Fatalf("status %d, reached %v", w.Code, seen != nil)
		}
		rec, ok := RecognitionFrom(seen.Context())
		if len(seen.Header.Values(iface.HeaderHandover)) != 0 {
			t.Errorf("%s reached the handler", iface.HeaderHandover)
		}
		return rec, ok, seen
	}
	t.Run("R-BAS-072 a forwarded GET with the offer", func(t *testing.T) {
		rec, ok, _ := recognized(http.MethodGet, "/files/web/generic-local/a.bin", "accept; max-size=1048576")
		if !ok || !rec.Offered || rec.Offer.MaxSize != 1048576 {
			t.Fatalf("%+v %v", rec, ok)
		}
	})
	for name, tc := range map[string]struct {
		method, path string
		offers       []string
	}{
		"a HEAD":                        {http.MethodHead, "/files/web/generic-local/a.bin", []string{"accept; max-size=1048576"}},
		"an interface call":             {http.MethodGet, iface.PathHandshake, []string{"accept; max-size=1048576"}},
		"a value the interface refuses": {http.MethodGet, "/files/web/generic-local/a.bin", []string{"accept;max-size=1048576"}},
		"a size of 0":                   {http.MethodGet, "/files/web/generic-local/a.bin", []string{"accept; max-size=0"}},
		"two values":                    {http.MethodGet, "/files/web/generic-local/a.bin", []string{"accept; max-size=1", "accept; max-size=2"}},
	} {
		t.Run("R-BAS-073 "+name+" is no offer", func(t *testing.T) {
			if tc.path == iface.PathHandshake {
				r := httptest.NewRequest(tc.method, tc.path, nil)
				r.Header.Set("Authorization", "Bearer "+tokValid)
				r.Header.Set(iface.HeaderInterface, version())
				for _, o := range tc.offers {
					r.Header.Add(iface.HeaderHandover, o)
				}
				_, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}}, r)
				rec, _ := RecognitionFrom(seen.Context())
				if rec.Offered || len(seen.Header.Values(iface.HeaderHandover)) != 0 {
					t.Fatalf("%+v", rec)
				}
				return
			}
			if rec, ok, _ := recognized(tc.method, tc.path, tc.offers...); !ok || rec.Offered {
				t.Fatalf("%+v %v", rec, ok)
			}
		})
	}
	t.Run("R-BAS-073 an ordinary request that sends the offer passes on without it", func(t *testing.T) {
		_, seen := run(t, RecognizeOptions{Tokens: &fakeTokens{}},
			forwarded("/files/web/generic-local/a.bin", iface.HeaderHandover, "accept; max-size=1048576", "Authorization", "Bearer "+pat))
		if _, ok := RecognitionFrom(seen.Context()); ok || len(seen.Header.Values(iface.HeaderHandover)) != 0 || seen.Header.Get("Authorization") == "" {
			t.Fatalf("recognized %v, headers %v", ok, seen.Header)
		}
	})
}

// TestR_BAS_073_NoteHandoverOnlyOnAMarkedForwardThatOffered: the serve point's note reaches the marking only for a recognized
// forward that offered and that the marking stage marks; every other request records nothing, so the serve point streams.
func TestR_BAS_073_NoteHandoverOnlyOnAMarkedForwardThatOffered(t *testing.T) {
	offered := Recognition{Bastion: "partners", Interface: iface.InterfaceVersion, Offer: iface.HandoverOffer{MaxSize: 10}, Offered: true}
	marked := func(rec *Recognition) (context.Context, *Absence) {
		ctx := context.Background()
		if rec != nil {
			ctx = WithRecognition(ctx, *rec)
		}
		return WithAbsence(ctx)
	}
	ctx, a := marked(&offered)
	if _, ok := MarksHandover(ctx); !ok || !NoteHandover(ctx, "v") || a.Handover() != "v" {
		t.Fatal("a marked forward that offered records no handover")
	}
	probe := offered
	probe.Probe = true
	notOffered := offered
	notOffered.Offered = false
	for name, ctx := range map[string]context.Context{
		"an ordinary request":                  func() context.Context { c, _ := marked(nil); return c }(),
		"a forward that did not offer":         func() context.Context { c, _ := marked(&notOffered); return c }(),
		"a probe":                              func() context.Context { c, _ := marked(&probe); return c }(),
		"a forward the marking does not mark":  WithRecognition(context.Background(), offered),
		"a request without a context's record": context.Background(),
	} {
		if _, ok := MarksHandover(ctx); ok || NoteHandover(ctx, "v") {
			t.Errorf("%s records a handover", name)
		}
	}
	var none *Absence
	if none.Handover() != "" {
		t.Fatal("no record holds a value")
	}
}

// TestR_BAS_071_TheSnapshotOfTheSwitches: the snapshot answers from memory; a failed load keeps the last one; a registration
// turned on after the first load asks the link checks again, the first load and a switch-off do not (storage.md R-STOR-096).
func TestR_BAS_071_TheSnapshotOfTheSwitches(t *testing.T) {
	var mu sync.Mutex
	rows, fail := []string{"b1"}, false
	load := func(context.Context) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("the database is away")
		}
		return append([]string(nil), rows...), nil
	}
	s := newHandoverSwitches(load, nil, nil)
	if s.On("b1") {
		t.Fatal("on before the first load")
	}
	checks := 0
	s.OnSwitchedOn(func() { checks++ })
	s.refresh(context.Background())
	if !s.On("b1") || s.On("b2") || checks != 0 {
		t.Fatalf("first load: b1 %v, b2 %v, checks %d", s.On("b1"), s.On("b2"), checks)
	}
	mu.Lock()
	rows = []string{"b1", "b2"}
	mu.Unlock()
	s.refresh(context.Background())
	if !s.On("b2") || checks != 1 {
		t.Fatalf("b2 turned on: %v, checks %d", s.On("b2"), checks)
	}
	mu.Lock()
	rows, fail = []string{"b2"}, true
	mu.Unlock()
	s.refresh(context.Background())
	if !s.On("b1") {
		t.Fatal("a failed load lost the snapshot")
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	s.refresh(context.Background())
	if s.On("b1") || !s.On("b2") || checks != 1 {
		t.Fatalf("b1 turned off: %v, checks %d", s.On("b1"), checks)
	}
	var nilSwitches *HandoverSwitches
	if nilSwitches.On("b2") {
		t.Fatal("no snapshot hands over")
	}
}

// TestR_BAS_071_TheSwitchOffIsJournaledAndReplayed: bastion.handover.disabled is a transactional event with the journal flag,
// bastion.handover.enabled one without; the replay handler of the domain bastion replays the first and refuses an entry without
// an ID before it touches the database. The replay against PostgreSQL is test/integration/bastionreg's.
func TestR_BAS_071_TheSwitchOffIsJournaledAndReplayed(t *testing.T) {
	off, ok := audit.Lookup(audit.EventBastionHandoverDisabled)
	if !ok || off.Mode != audit.Transactional || off.Journal != audit.JournalRevocation || off.Entry != audit.EntryRequired {
		t.Fatalf("%+v", off)
	}
	on, ok := audit.Lookup(audit.EventBastionHandoverEnabled)
	if !ok || on.Mode != audit.Transactional || on.Journal == audit.JournalRevocation {
		t.Fatalf("%+v", on)
	}
	h, ok := journal.HandlerFor(audit.EventBastionHandoverDisabled)
	if !ok || h.Domain != "bastion" {
		t.Fatalf("%+v %v", h, ok)
	}
	if _, ok := journal.HandlerFor(audit.EventBastionHandoverEnabled); ok {
		t.Fatal("switching on is replayed")
	}
	res, err := h.Apply(context.Background(), nil, journal.Env{}, journal.Object{Event: audit.EventBastionHandoverDisabled, Entry: []byte(`{"name":"partners"}`)})
	if err != nil || res.Outcome != journal.Skipped {
		t.Fatalf("an entry without an ID: %+v %v", res, err)
	}
	if reason, listed := NoFeedEntry()[audit.EventBastionHandoverDisabled]; !listed || reason == "" {
		t.Fatal("bastion.handover.disabled is not classified for the feed")
	}
}
