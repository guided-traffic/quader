package keys_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
)

const usedSentence = "connection archive holds objects but the database has no storage keys for it; import the exported storage keys (quader admin storage keys import) or empty the location (storage.md R-STOR-083)"

func TestR_STOR_083_TheFirstReplicaDrawsTheKeysAndOthersReadThem(t *testing.T) {
	t.Run("R-STOR-083 the first replica inserts one storage key and one name key, sealed; a second replica reads the same rows", func(t *testing.T) {
		e := newEnv(t)
		a, b := e.holder(t), e.holder(t)
		if err := a.Ensure(ctx, e.archive()); err != nil {
			t.Fatal(err)
		}
		rows := e.db.Rows()
		if len(rows) != 2 {
			t.Fatalf("%d rows, want a storage key and a name key: %+v", len(rows), rows)
		}
		for _, r := range rows {
			id, ok := keys.ParseID(r.KeyID)
			if !ok || id.Connection != "archive" || id.Generation != 1 || id.Kind != r.Kind || r.State != keys.StateActive {
				t.Errorf("row %+v", r)
			}
			if !secrets.IsSealed(r.Sealed) || len(e.opened(t, r)) != blobcrypt.KeySize {
				t.Errorf("row %s is not 32 bytes sealed with the key ring", r.KeyID)
			}
		}
		if err := b.Ensure(ctx, e.archive()); err != nil {
			t.Fatal(err)
		}
		if n := len(e.db.Rows()); n != 2 {
			t.Fatalf("the second replica drew again: %d rows", n)
		}
		sa, aa, na := a.Held("archive")
		sb, ab, nb := b.Held("archive")
		if !slices.Equal(sa, sb) || aa != ab || na != nb || aa == "" || na == "" {
			t.Errorf("the replicas hold %v %s %s and %v %s %s", sa, aa, na, sb, ab, nb)
		}
		ka, _ := a.Active(ctx, "archive")
		kb, _ := b.Active(ctx, "archive")
		if !ka.Key.Equal(kb.Key) {
			t.Error("the two replicas hold different storage keys under one identifier")
		}
	})
	t.Run("R-STOR-083 two replicas that start at once draw one key of each kind", func(t *testing.T) {
		e := newEnv(t)
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := range errs {
			h := e.holder(t)
			wg.Go(func() { errs[i] = h.Ensure(ctx, e.archive()) })
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatal(err)
		}
		if n := len(e.db.Rows()); n != 2 {
			t.Fatalf("%d rows after four replicas started at once, want 2", n)
		}
	})
	t.Run("R-STOR-083 a replica that loses the race to insert reads the winner's rows", func(t *testing.T) {
		e := newEnv(t)
		winner := []keys.Row{e.sealedRow(t, "sk-archive-1-0000aaaa", keys.KindStorage, 1), e.sealedRow(t, "nk-archive-0000bbbb", keys.KindName, 1)}
		e.db.BeforeInsert = func(keys.Row) {
			for _, r := range winner {
				e.db.Put(r)
			}
			e.db.BeforeInsert = nil
		}
		h := e.holder(t)
		if err := h.Ensure(ctx, e.archive()); err != nil {
			t.Fatal(err)
		}
		storage, active, name := h.Held("archive")
		if len(e.db.Rows()) != 2 || active != "sk-archive-1-0000aaaa" || name != "nk-archive-0000bbbb" || len(storage) != 1 {
			t.Fatalf("held %v %s %s over rows %+v", storage, active, name, e.db.Rows())
		}
		k, _ := h.Active(ctx, "archive")
		if !bytes.Equal(must(k.Key.Bytes()), e.opened(t, winner[0])) {
			t.Error("the active key is not the winner's")
		}
	})
	t.Run("R-STOR-083 without keyed names no name key is drawn, and a draining connection draws nothing", func(t *testing.T) {
		e := newEnv(t)
		c := e.archive()
		c.KeyedNames = false
		if err := e.holder(t).Ensure(ctx, c); err != nil {
			t.Fatal(err)
		}
		if rows := e.db.Rows(); len(rows) != 1 || rows[0].Kind != keys.KindStorage {
			t.Fatalf("rows %+v", rows)
		}
		d := keys.Connection{ID: "old", KeyedNames: true, Drain: true, Prefix: "old", Objects: e.store}
		if err := e.holder(t).Ensure(ctx, d); err != nil {
			t.Fatal(err)
		}
		if n := len(e.db.Rows()); n != 1 {
			t.Fatalf("a draining connection drew keys: %d rows", n)
		}
	})
}

func TestR_STOR_083_AUsedLocationWithoutKeysRefusesTheStart(t *testing.T) {
	t.Run("R-STOR-083 an object below sha256/ or keyed/ and no row: the start stops with the sentence and draws nothing", func(t *testing.T) {
		for _, key := range []string{
			blob.Key("quader", blob.Digest{0xaa, 0xbb}),
			"quader/keyed/99/42/9942d541941e3141ba35550ee20b7bbf8c60fb3acc8123325c1b293b0e2922a6",
		} {
			e := newEnv(t)
			e.store.Set(key, []byte("an object of an earlier database"), time.Now())
			err := e.holder(t).Ensure(ctx, e.archive())
			if !keys.IsRefusal(err) || err.Error() != usedSentence {
				t.Errorf("%s: %v, want the refusal %q", key, err, usedSentence)
			}
			if n := len(e.db.Rows()); n != 0 {
				t.Errorf("%s: %d keys drawn over a used location", key, n)
			}
		}
	})
	t.Run("R-STOR-083 the marker, the probes and the staging area do not count; a connection with its rows loads over its objects", func(t *testing.T) {
		e := newEnv(t)
		for _, k := range []string{"quader/_connection", "quader/_probe/x", "quader/_uploads/s/data", "quader/sha256x/y"} {
			e.store.Set(k, []byte("x"), time.Now())
		}
		if err := e.holder(t).Ensure(ctx, e.archive()); err != nil {
			t.Fatal(err)
		}
		e.store.Set(blob.Key("quader", blob.Digest{1}), []byte("an object of this database"), time.Now())
		if err := e.holder(t).Ensure(ctx, e.archive()); err != nil {
			t.Fatalf("a replica refused a location whose objects its rows wrapped: %v", err)
		}
	})
	t.Run("R-STOR-083 a name key missing over a used location is refused as such", func(t *testing.T) {
		e := newEnv(t)
		c := e.archive()
		c.KeyedNames = false
		if err := e.holder(t).Ensure(ctx, c); err != nil {
			t.Fatal(err)
		}
		e.store.Set(blob.Key("quader", blob.Digest{2}), []byte("x"), time.Now())
		err := e.holder(t).Ensure(ctx, e.archive())
		if !keys.IsRefusal(err) || !strings.Contains(err.Error(), "the database has no name key for it") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("R-STOR-083 an object another replica wrote under the keys it drew meanwhile is no reason to refuse", func(t *testing.T) {
		e := newEnv(t)
		other := e.holder(t)
		once := false
		e.store.SetFault(func(op, _ string) error {
			if op == "list" && !once {
				once = true
				if err := other.Ensure(ctx, e.archive()); err != nil {
					t.Error(err)
				}
				e.store.Set(blob.Key("quader", blob.Digest{3}), []byte("written under the other replica's key"), time.Now())
			}
			return nil
		})
		h := e.holder(t)
		if err := h.Ensure(ctx, e.archive()); err != nil {
			t.Fatalf("refused: %v", err)
		}
		_, a1, _ := h.Held("archive")
		_, a2, _ := other.Held("archive")
		if a1 != a2 || len(e.db.Rows()) != 2 {
			t.Fatalf("held %s and %s over %d rows", a1, a2, len(e.db.Rows()))
		}
	})
}

func TestR_STOR_083_ANewGenerationReachesEveryReplica(t *testing.T) {
	e := newEnv(t)
	a, late := e.holder(t), e.holder(t)
	for _, h := range []*keys.Holder{a, late} {
		if err := h.Ensure(ctx, e.archive()); err != nil {
			t.Fatal(err)
		}
	}
	gen1, _ := a.Active(ctx, "archive")
	meta1, want1 := record(t, gen1)
	var announced keys.Generation
	g, err := keys.NewGeneration(ctx, e.db, e.db, e.ring, nil, "archive", func(_ context.Context, _ meta.Tx, g keys.Generation) error {
		announced = g
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("R-STOR-083 generation 2 is active, generation 1 retired, in one transaction with its hook", func(t *testing.T) {
		id, _ := keys.ParseID(g.KeyID)
		if id.Generation != 2 || g.Generation != 2 || g.RetiredKeyID != gen1.ID || announced != g {
			t.Fatalf("generation %+v, announced %+v", g, announced)
		}
		states := map[string]string{}
		for _, r := range e.db.Rows() {
			states[r.KeyID] = r.State
		}
		if states[gen1.ID] != keys.StateRetired || states[g.KeyID] != keys.StateActive || len(states) != 3 {
			t.Fatalf("rows %v", states)
		}
	})
	t.Run("R-STOR-083 a replica wraps under the new generation once the change area storage_keys names the connection", func(t *testing.T) {
		if k, _ := a.Active(ctx, "archive"); k.ID != gen1.ID {
			t.Fatalf("active %s before the change, want %s", k.ID, gen1.ID)
		}
		a.ApplyChange(cluster.AreaStorageKeys, 1, json.RawMessage(`{"connection":"archive"}`))
		if k, _ := a.Active(ctx, "archive"); k.ID != g.KeyID {
			t.Fatalf("active %s after the change, want %s", k.ID, g.KeyID)
		}
		if _, err := blobcrypt.Open(meta1, a.Lookup(ctx, "archive"), want1); err != nil {
			t.Fatalf("an object of generation 1 does not open: %v", err)
		}
	})
	t.Run("R-STOR-083 a replica that missed the change reads the rows again for the new identifier, at most once per 10 s", func(t *testing.T) {
		gen2, _ := a.Active(ctx, "archive")
		meta2, want2 := record(t, gen2)
		reads := e.db.Reads
		if _, err := blobcrypt.Open(meta2, late.Lookup(ctx, "archive"), want2); reasonOf(err) != blobcrypt.ReasonUnknownKey {
			t.Fatalf("within 10 s of its last read: %v, want unknown_key", err)
		}
		if e.db.Reads != reads {
			t.Fatal("the rows were read again within 10 s")
		}
		e.clock.add(11 * time.Second)
		if _, err := blobcrypt.Open(meta2, late.Lookup(ctx, "archive"), want2); err != nil {
			t.Fatalf("after 10 s: %v", err)
		}
		if e.db.Reads != reads+1 {
			t.Fatalf("%d reads, want one", e.db.Reads-reads)
		}
		if k, _ := late.Active(ctx, "archive"); k.ID != g.KeyID {
			t.Fatalf("the late replica wraps under %s", k.ID)
		}
		other, _ := record(t, &blobcrypt.StorageKey{ID: "sk-archive-9-00000000", Key: gen2.Key})
		e.clock.add(11 * time.Second)
		if _, err := blobcrypt.Open(other, late.Lookup(ctx, "archive"), want2); reasonOf(err) != blobcrypt.ReasonUnknownKey {
			t.Fatalf("an identifier no row holds: %v, want unknown_key", err)
		}
	})
	t.Run("R-STOR-083 a connection without a storage key gets no generation", func(t *testing.T) {
		_, err := keys.NewGeneration(ctx, e.db, e.db, e.ring, nil, "plain", nil)
		if !errors.Is(err, keys.ErrNoStorageKey) {
			t.Fatalf("%v", err)
		}
	})
}

func TestR_STOR_083_NoKeyLeavesTheHolder(t *testing.T) {
	e := newEnv(t)
	h := e.holder(t)
	if err := h.Ensure(ctx, e.archive()); err != nil {
		t.Fatal(err)
	}
	first, _ := h.Active(ctx, "archive")
	if _, err := keys.NewGeneration(ctx, e.db, e.db, e.ring, nil, "archive", nil); err != nil {
		t.Fatal(err)
	}
	h.FlushArea(cluster.AreaStorageKeys, "test")
	active, _ := h.Active(ctx, "archive")
	name, _ := h.NameKey("archive")
	t.Run("R-STOR-083 the log, an error and a key's rendering name identifiers and never a byte of a key or a sealed value", func(t *testing.T) {
		text := e.logs.String() + fmt.Sprintf("%v %+v %#v %v %+v", active, active, active, name, name)
		_, err := keys.NewGeneration(ctx, e.db, e.db, ringOf(t, "k9", map[string]byte{"k9": 9}), nil, "archive", nil)
		if !keys.IsRefusal(err) || !strings.Contains(err.Error(), "root key \"k1\"") {
			t.Errorf("a new generation with a ring that does not open the connection's key: %v", err)
		}
		text += fmt.Sprint(err)
		e.store.Set(blob.Key("fresh", blob.Digest{4}), []byte("x"), time.Now())
		err = e.holder(t).Ensure(ctx, keys.Connection{ID: "fresh", Prefix: "fresh", Objects: e.store})
		if !keys.IsRefusal(err) {
			t.Errorf("over a used location: %v", err)
		}
		text += fmt.Sprint(err)
		if !strings.Contains(e.logs.String(), first.ID) {
			t.Errorf("the log does not name the drawn key's identifier:\n%s", e.logs.String())
		}
		for _, r := range e.db.Rows() {
			raw := e.opened(t, r)
			for _, form := range []string{hex.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw), r.Sealed, string(raw)} {
				if strings.Contains(text, form) {
					t.Fatalf("the text shows key %s: %s", r.KeyID, text)
				}
			}
		}
	})
	t.Run("R-STOR-083 Close destroys every key the holder holds", func(t *testing.T) {
		h.Close()
		if _, err := active.Key.Bytes(); err == nil {
			t.Error("the storage key still holds its bytes after Close")
		}
		if _, err := name.Bytes(); err == nil {
			t.Error("the name key still holds its bytes after Close")
		}
		if err := h.Load(ctx, "archive"); err == nil {
			t.Error("a closed holder loaded keys again")
		}
	})
}

func TestR_STOR_083_AKeyTheRingCannotOpenStopsTheStart(t *testing.T) {
	e := newEnv(t)
	if err := e.holder(t).Ensure(ctx, e.archive()); err != nil {
		t.Fatal(err)
	}
	t.Run("R-STOR-083 a ring without the root key that sealed the rows is a refusal naming the key and the root key", func(t *testing.T) {
		other := &env{db: e.db, store: e.store, ring: ringOf(t, "k2", map[string]byte{"k2": 2}), clock: e.clock, logs: e.logs}
		err := other.holder(t).Ensure(ctx, e.archive())
		if !keys.IsRefusal(err) || !strings.Contains(err.Error(), `sealed with the root key "k1", which this key ring lacks`) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("R-STOR-083 a sealed key moved to another row does not open", func(t *testing.T) {
		rows := e.db.Rows()
		moved := rows[0]
		moved.Sealed = rows[1].Sealed
		e.db.Put(moved)
		err := e.holder(t).Load(ctx, "archive")
		if !keys.IsRefusal(err) || !strings.Contains(err.Error(), "sealed for another identifier") {
			t.Fatalf("%v", err)
		}
	})
}

func TestR_STOR_083_TheSealedColumnsAreRegistered(t *testing.T) {
	t.Run("R-STOR-083 storage_keys.sealed_key under key_id and upload_sessions.sealed_data_key under id are in the key ring's registry", func(t *testing.T) {
		want := map[string]string{"storage_keys.sealed_key": "key_id", "upload_sessions.sealed_data_key": "id"}
		for _, c := range secrets.Columns() {
			if pk, ok := want[c.String()]; ok {
				if c.PrimaryKey() != pk {
					t.Errorf("%s has the row key %s, want %s", c, c.PrimaryKey(), pk)
				}
				delete(want, c.String())
			}
		}
		if len(want) != 0 {
			t.Fatalf("not registered: %v", want)
		}
		if keys.Column().String() != "storage_keys.sealed_key" || keys.SessionColumn().String() != "upload_sessions.sealed_data_key" {
			t.Error("the accessors name other columns")
		}
	})
	t.Run("R-STOR-081 a session's data key round-trips through its row and opens under no other session", func(t *testing.T) {
		ring := ringOf(t, "k1", map[string]byte{"k1": 1})
		dk, _ := blobcrypt.NewKey(bytes.NewReader(bytes.Repeat([]byte{3}, 32)))
		sealed, err := keys.SealSessionKey(ring, "0b7c3f5e-0000-4000-8000-000000000001", dk)
		if err != nil {
			t.Fatal(err)
		}
		got, err := keys.OpenSessionKey(ring, "0b7c3f5e-0000-4000-8000-000000000001", sealed)
		if err != nil || !got.Equal(dk) {
			t.Fatalf("round trip: %v", err)
		}
		if _, err := keys.OpenSessionKey(ring, "0b7c3f5e-0000-4000-8000-000000000002", sealed); !errors.Is(err, secrets.ErrAuthentication) {
			t.Fatalf("another session opened it: %v", err)
		}
	})
}

func TestR_STOR_084_TheWalkResealsBothColumnsAndChangesNoKey(t *testing.T) {
	e := newEnv(t)
	if err := e.holder(t).Ensure(ctx, e.archive()); err != nil {
		t.Fatal(err)
	}
	dk, _ := blobcrypt.NewKey(bytes.NewReader(bytes.Repeat([]byte{4}, 32)))
	sealed, _ := keys.SealSessionKey(e.ring, "0b7c3f5e-0000-4000-8000-000000000001", dk)
	e.db.SetSession("0b7c3f5e-0000-4000-8000-000000000001", sealed)
	before := map[string][]byte{}
	for _, r := range e.db.Rows() {
		before[r.KeyID] = e.opened(t, r)
	}
	rotated := ringOf(t, "k2", map[string]byte{"k1": 1, "k2": 2})
	res, err := keys.Reseal(ctx, e.db, e.db, rotated, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("R-STOR-084 every value lies under the new root key, the keys inside unchanged, and a second walk finds nothing to do", func(t *testing.T) {
		if res != (keys.ResealResult{Resealed: 3}) {
			t.Fatalf("result %+v, want three values resealed", res)
		}
		only := &env{db: e.db, ring: ringOf(t, "k2", map[string]byte{"k2": 2})}
		for _, r := range e.db.Rows() {
			if root, _ := secrets.KeyIDOf(r.Sealed); root != "k2" || !bytes.Equal(only.opened(t, r), before[r.KeyID]) {
				t.Errorf("row %s: root %s or its key changed", r.KeyID, root)
			}
		}
		if got, err := keys.OpenSessionKey(only.ring, "0b7c3f5e-0000-4000-8000-000000000001", e.db.Session("0b7c3f5e-0000-4000-8000-000000000001")); err != nil || !got.Equal(dk) {
			t.Errorf("the session's data key: %v", err)
		}
		again, err := keys.Reseal(ctx, e.db, e.db, rotated, 0)
		if err != nil || again != (keys.ResealResult{Current: 3}) {
			t.Errorf("second walk %+v %v", again, err)
		}
	})
	t.Run("R-STOR-084 a value the ring cannot open is counted and left as it is", func(t *testing.T) {
		e.db.SetSession("0b7c3f5e-0000-4000-8000-000000000002", "v1:k7:AAAA")
		got, err := keys.Reseal(ctx, e.db, e.db, rotated, 0)
		if err != nil || got.Failed != 1 || e.db.Session("0b7c3f5e-0000-4000-8000-000000000002") != "v1:k7:AAAA" {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestR_STOR_083_TheHolderFollowsTheRegisteredChangeArea(t *testing.T) {
	t.Run("R-STOR-083 the holder's area is the change area storage_keys that internal/cluster registers", func(t *testing.T) {
		a, ok := cluster.LookupArea(keys.ChangeArea)
		if keys.ChangeArea != cluster.AreaStorageKeys || !ok || a.Class != cluster.ChangeArea {
			t.Fatalf("keys.ChangeArea %q, cluster.AreaStorageKeys %q, registered %+v %v", keys.ChangeArea, cluster.AreaStorageKeys, a, ok)
		}
	})
}

func TestKeyIdentifiers(t *testing.T) {
	t.Run("R-STOR-083 an identifier names its kind, connection and generation; anything else is no identifier (Q-STOR-324)", func(t *testing.T) {
		for id, want := range map[string]keys.ID{
			"sk-archive-1-3f9a0c2e":     {Kind: keys.KindStorage, Connection: "archive", Generation: 1},
			"sk-archive-1-12-0000ffff":  {Kind: keys.KindStorage, Connection: "archive-1", Generation: 12},
			"sk-a1-999999-0a1b2c3d":     {Kind: keys.KindStorage, Connection: "a1", Generation: 999999},
			"nk-archive-71be04d9":       {Kind: keys.KindName, Connection: "archive", Generation: 1},
			"nk-default-store-0a1b2c3d": {Kind: keys.KindName, Connection: "default-store", Generation: 1},
		} {
			if got, ok := keys.ParseID(id); !ok || got != want {
				t.Errorf("%s: %+v %v, want %+v", id, got, ok, want)
			}
		}
		for _, bad := range []string{
			"", "sk-archive-0-3f9a0c2e", "sk-archive-1-3F9A0C2E", "sk-archive-3f9a0c2e", "nk-archive-1", "xk-archive-1-3f9a0c2e",
			"sk-Archive-1-3f9a0c2e", "sk-archive--1-3f9a0c2e", "sk-archive-1000000-3f9a0c2e", "nk-a-0a1b2c3d", "sk-archive-1-3f9a0c2e0",
		} {
			if _, ok := keys.ParseID(bad); ok {
				t.Errorf("%q parsed", bad)
			}
		}
	})
}

// reasonOf is the reason of a refusal of the format, empty for any other error.
func reasonOf(err error) blobcrypt.Reason {
	r, _ := blobcrypt.ReasonOf(err)
	return r
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
