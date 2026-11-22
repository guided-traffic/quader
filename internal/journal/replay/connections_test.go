package replay

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/journal"
)

// TestR_STOR_059_TheReplayReadsEveryConnection merges the journal of two in-memory stores by key (unit, about 0.01 s, entries of
// a few bytes).
func TestR_STOR_059_TheReplayReadsEveryConnection(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	newReader := func(srcs ...Source) *Reader {
		r := ReaderOf(srcs...)
		r.now = func() time.Time { return now.Add(time.Minute) }
		return r
	}
	t.Run("identical objects of one key are one copy, an entry only one connection holds is read too", func(t *testing.T) {
		a, b := &memStore{objs: map[string][]byte{}}, &memStore{objs: map[string][]byte{}}
		o := put(t, a, 1, now.Add(time.Second), "token.revoked")
		body := a.objs[journal.Key(prefix, o)]
		b.objs[journal.Key("q2", o)] = body // the same bytes below the second connection's prefix
		put(t, a, 2, now.Add(2*time.Second), "token.revoked")
		got, bad, err := newReader(Source{"default", a, prefix}, Source{"secondary", b, "q2"}).Entries(t.Context(), now)
		if err != nil || len(bad) != 0 {
			t.Fatalf("Entries = %v, %v, %v", got, bad, err)
		}
		if len(got) != 2 || got[0].ID != id(1) || got[1].ID != id(2) || got[0].Warning != "" {
			t.Fatalf("entries = %+v, want the two entries once each, unflagged", got)
		}
	})
	t.Run("different objects under one key are each replayed and flagged", func(t *testing.T) {
		a, b := &memStore{objs: map[string][]byte{}}, &memStore{objs: map[string][]byte{}}
		o := put(t, a, 1, now.Add(time.Second), "token.revoked")
		forged := journal.Object{V: 1, ID: o.ID, At: o.At, Event: "user.disabled", Entry: []byte(`{}`)}
		fb, _ := forged.Encode()
		b.objs[journal.Key(prefix, o)] = fb
		got, _, err := newReader(Source{"default", a, prefix}, Source{"secondary", b, prefix}).Entries(t.Context(), now)
		if err != nil || len(got) != 2 {
			t.Fatalf("Entries = %+v, %v; want both contents", got, err)
		}
		for _, g := range got {
			if !strings.Contains(g.Warning, "diverge") || !strings.Contains(g.Warning, "secondary") {
				t.Errorf("entry %s %s is not flagged with the connections: %q", g.ID, g.Event, g.Warning)
			}
		}
	})
	t.Run("a connection that cannot be listed is reported and the others are read; none listed fails", func(t *testing.T) {
		a, b := &memStore{objs: map[string][]byte{}}, &memStore{objs: map[string][]byte{}, listErr: errors.New("denied")}
		put(t, a, 1, now.Add(time.Second), "token.revoked")
		got, bad, err := newReader(Source{"default", a, prefix}, Source{"secondary", b, prefix}).Entries(t.Context(), now)
		if err != nil || len(got) != 1 || len(bad) != 1 || !strings.HasPrefix(bad[0].Key, "secondary: ") {
			t.Fatalf("Entries = %v, %v, %v", got, bad, err)
		}
		a.listErr = errors.New("denied")
		if _, _, err := newReader(Source{"default", a, prefix}, Source{"secondary", b, prefix}).Entries(t.Context(), now); err == nil {
			t.Fatal("a journal of which no connection can be listed must fail the read")
		}
	})
}
