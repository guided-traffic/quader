package scrub

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
)

// destroyedKeys are a replica's keys whose lookup answers destroyed_key for one identifier, as keys.Holder does for a row the
// destruction set destroyed (R-STOR-101); every other answer is the test keys'.
type destroyedKeys struct {
	*blobtest.Keys
	destroyed string
}

func (k destroyedKeys) Lookup(ctx context.Context, connection string) blobcrypt.KeyLookup {
	held := k.Keys.Lookup(ctx, connection)
	return func(id string) (*blobcrypt.StorageKey, error) {
		if id == k.destroyed {
			return nil, fmt.Errorf("%w: the storage key %s of connection %s was destroyed", blobcrypt.ErrDestroyedKey, id, connection)
		}
		return held(id)
	}
}

func TestR_STOR_101_TheScrubMarksACopyWhoseRecordNamesADestroyedKey(t *testing.T) {
	t.Run("R-STOR-101 R-STOR-087 a copy whose record names a destroyed key is marked corrupt and repaired from the plain connection", func(t *testing.T) {
		l, store, plain, ks := newLedger(), blobtest.New(), blobtest.New(), blobtest.NewKeys()
		old := ks.Draw("archive", 1)
		archive := sealedConnection("archive", store, ks)
		good := data(1024, 51)
		d, cw := putSealed(t, l, archive, good)
		putPlain(l, "plain", plain, good)
		before, _ := store.Data(cw.Key)
		newKey := ks.Draw("archive", 2)
		archive.UseKeys(destroyedKeys{Keys: ks, destroyed: old})
		res, err := newScrubber(t, l, newClock(), archive, connection("plain", plain, 50<<20)).Run(context.Background(), RunOptions{Connection: "archive"})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "archive"); r.Corrupt != 1 || r.Repaired != 1 {
			t.Fatalf("archive = %+v", r)
		}
		_, damaged, whole := l.recorded()
		if !slices.Equal(damaged, []string{"archive/" + d.Hex()[:6] + "/corrupt"}) || !slices.Equal(whole, []string{"archive/" + d.Hex()[:6]}) {
			t.Fatalf("damaged = %v, whole = %v", damaged, whole)
		}
		after, _ := store.Data(cw.Key)
		if row := l.copies[d][0]; bytes.Equal(after, before) || row.StorageKeyID != newKey || row.State != blob.CopyPresent {
			t.Fatalf("the repair kept the old bytes or the row names %q in state %s", row.StorageKeyID, row.State)
		}
	})
}
