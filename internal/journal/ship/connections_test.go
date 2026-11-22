package ship

import (
	"errors"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
)

// TestR_STOR_059_TheJournalGoesToEveryConnection ships over two in-memory stores through the connection store (unit, about
// 0.01 s, entries of less than 1 KiB).
func TestR_STOR_059_TheJournalGoesToEveryConnection(t *testing.T) {
	first, second := blobtest.New(), blobtest.New()
	down := errors.New("connection down")
	second.SetFault(func(op, _ string) error {
		if op == "put" {
			return &blob.StoreError{Op: op, Kind: blob.KindTransient, Err: down}
		}
		return nil
	})
	targets := []Target{
		{Connection: "default", Store: NewConnectionStore(first, "sha256"), Prefix: "quader"},
		{Connection: "secondary", Store: NewConnectionStore(second, "md5"), Prefix: "q2"},
	}
	ob := &fakeOutbox{rows: rows(1)}
	key := "_journal/revocations/2026/10/28/0192f0c4-0000-7000-8000-000000000001.json"

	n, err := New(Options{Outbox: ob, Targets: targets}).Ship(t.Context())
	if n != 0 || !errors.Is(err, down) || !strings.Contains(err.Error(), "secondary") {
		t.Fatalf("Ship with the second store failing = %d, %v; want nothing deleted and the second connection named", n, err)
	}
	if _, ok := first.Data("quader/" + key); !ok {
		t.Fatal("the entry did not reach the first connection")
	}
	if len(ob.rows) != 1 || len(ob.rows[0].Shipped) != 1 || ob.rows[0].Shipped[0] != "default" {
		t.Fatalf("the outbox row must stay with one shipped mark: %+v", ob.rows)
	}
	if ob.pendingOn("secondary") != 1 || ob.pendingOn("default") != 0 {
		t.Fatalf("pending: default %d, secondary %d; want 0 and 1", ob.pendingOn("default"), ob.pendingOn("secondary"))
	}

	second.SetFault(nil)
	puts := first.Count("put")
	if n, err := New(Options{Outbox: ob, Targets: targets}).Ship(t.Context()); n != 1 || err != nil {
		t.Fatalf("Ship after the fault was lifted = %d, %v; want the row deleted", n, err)
	}
	a, _ := first.Data("quader/" + key)
	b, ok := second.Data("q2/" + key)
	if !ok || string(a) != string(b) || len(ob.rows) != 0 {
		t.Fatalf("the entry is not on both connections (%v) or the row stays (%d)", ok, len(ob.rows))
	}
	if first.Count("put") != puts {
		t.Error("the connection that held the object was written again")
	}
}

// TestR_STOR_059_AConnectionThatIsDownDelaysOnlyItsOwnCopies: one run walks the whole outbox past the rows a connection that is
// down cannot take, and writes every row to the connection that answers.
func TestR_STOR_059_AConnectionThatIsDownDelaysOnlyItsOwnCopies(t *testing.T) {
	up, down := &fakeStore{}, &fakeStore{failAt: 1}
	ob := &fakeOutbox{rows: rows(3)}
	_, err := New(Options{Outbox: ob, Targets: []Target{
		{Connection: "default", Store: down, Prefix: "quader"}, {Connection: "secondary", Store: up, Prefix: "quader"},
	}, Batch: 2}).Ship(t.Context())
	if err == nil {
		t.Fatal("no error for the connection that is down")
	}
	if len(up.puts) != 3 || len(down.puts) != 0 || ob.claims != 2 {
		t.Fatalf("puts on the answering connection %d, on the one down %d, claims %d; want 3, 0, 2", len(up.puts), len(down.puts), ob.claims)
	}
	if ob.pendingOn("default") != 3 || ob.pendingOn("secondary") != 0 {
		t.Fatalf("pending: default %d, secondary %d", ob.pendingOn("default"), ob.pendingOn("secondary"))
	}
}
