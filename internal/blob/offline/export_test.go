package offline_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/keys/keystest"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
)

// keysExport writes the export of storage.md R-STOR-085 and R-STOR-103 as quader admin storage keys export writes it: the keys of
// connection archive (storage keys by generation, the name key) sealed with ring into the rows of the keys' simulated table, the
// identifiers of destroyed destroyed through the journal's replay (keys.ReplayDestroyed, the destruction one identifier at a time),
// and the document of keys.Export. The offline reader then reads an export's destroyed entry as package 7's code writes it, not as a
// test spells it.
func keysExport(t *testing.T, ring *secrets.KeyRing, sealed []entry, destroyed ...string) string {
	t.Helper()
	ctx := context.Background()
	db := keystest.New()
	created := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	highest := 0
	for _, e := range sealed {
		if id, _ := keys.ParseID(e.id); id.Kind == keys.KindStorage {
			highest = max(highest, id.Generation)
		}
	}
	for _, e := range sealed {
		id, ok := keys.ParseID(e.id)
		if !ok {
			t.Fatalf("no identifier: %s", e.id)
		}
		v, err := ring.Seal(keys.Column(), secrets.Row(e.id), e.raw)
		if err != nil {
			t.Fatal(err)
		}
		row := keys.Row{KeyID: e.id, Connection: id.Connection, Kind: id.Kind, Generation: id.Generation, Sealed: v, State: keys.StateActive, CreatedAt: created}
		if id.Kind == keys.KindStorage && id.Generation < highest {
			row.State, row.RetiredAt = keys.StateRetired, created.Add(time.Minute)
		}
		db.Put(row)
	}
	if len(destroyed) > 0 {
		err := db.InTx(ctx, func(tx meta.Tx) error {
			res, err := keys.ReplayDestroyed(ctx, tx, db, keys.DestroyedEntry{Connection: "archive", KeyIDs: destroyed, DestroyedAt: created.Add(time.Hour)},
				[]string{"default"}, time.Now(), nil)
			if err == nil && res.Outcome != keys.ReplayApplied {
				t.Errorf("the replay of the destruction: %+v", res)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	doc, err := keys.Export(ctx, db, db, "0b7c3f5e-0000-4000-8000-000000000001", "offline-test", "", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
