package cache

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_BAS_110_TheCatalogueTreeIsSignedSealedAndNeverSwept: the public catalogue's documents and its state record live in a tree of
// their own: the sweep of the areas neither counts nor evicts them, the state record is signed (one changed on the volume is
// refused), with a cache key every object of the tree is sealed below the key's tree and no plaintext is on the volume, and the
// catalogue's share comes off the artifacts' limit. Unit, milliseconds, documents of bytes.
func TestR_BAS_110_TheCatalogueTreeIsSignedSealedAndNeverSwept(t *testing.T) {
	ctx := context.Background()
	doc := []byte("a compressed document of the catalogue")
	sha := digest([]byte("its raw bytes"))
	for _, sealed := range []bool{false, true} {
		dir := filepath.Join(t.TempDir(), "volume")
		key := ""
		if sealed {
			key = writeKeyFile(t, keyK())
		}
		clk := newClock()
		r := sealedRig(t, dir, key, clk, func(c *config.BastionCache) { c.MaxBytes, c.DocumentsMaxBytes, c.MaxObjectBytes = 4*kib, kib, kib })
		if err := r.PutCatalogueDocument(ctx, sha, doc); err != nil {
			t.Fatal(err)
		}
		if got, err := r.GetCatalogueDocument(ctx, sha, 1<<20); err != nil || !bytes.Equal(got, doc) {
			t.Fatalf("sealed %v: read back %q %v", sealed, got, err)
		}
		if err := r.StoreCatalogueState(ctx, []byte(`{"cursor":7}`)); err != nil {
			t.Fatal(err)
		}
		if got, err := r.LoadCatalogueState(ctx); err != nil || string(got) != `{"cursor":7}` {
			t.Fatalf("sealed %v: the state %q %v", sealed, got, err)
		}
		// The areas' sweep of a cache far above its limits leaves the tree alone and does not count it.
		r.putObject(EntryArtifact, bytes.Repeat([]byte("a"), 8*kib))
		if err := r.SweepOnce(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetCatalogueDocument(ctx, sha, 1<<20); err != nil {
			t.Fatalf("sealed %v: the sweep evicted a document of the catalogue: %v", sealed, err)
		}
		if u, _ := r.Backend().Usage(ctx); u.Artifacts+u.Documents != 0 {
			t.Errorf("sealed %v: the areas count %d bytes after the sweep", sealed, u.Artifacts+u.Documents)
		}
		var names []string
		if err := r.ListCatalogue(ctx, func(o CatalogueObject) error { names = append(names, o.Name); return nil }); err != nil {
			t.Fatal(err)
		}
		want, _ := r.CatalogueName(sha)
		if len(names) != 1 || names[0] != want || (sealed == (want == sha)) {
			t.Errorf("sealed %v: the listing names %v, the name of the document is %s", sealed, names, want)
		}
		plain := filepath.Join(dir, "catalogue", "state.json")
		if _, err := os.Stat(plain); sealed == (err == nil) {
			t.Errorf("sealed %v: the plain state record exists: %v", sealed, err == nil)
		}
		if !sealed {
			if err := os.WriteFile(plain, []byte(`{"cursor":9}`+"\n"+"00"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := r.LoadCatalogueState(ctx); !errors.Is(err, ErrBadSignature) {
				t.Errorf("a state record changed on the volume: %v", err)
			}
		}
		if err := r.DeleteCatalogueObject(ctx, want); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteCatalogueState(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetCatalogueDocument(ctx, sha, 1<<20); !errors.Is(err, ErrNotFound) {
			t.Errorf("sealed %v: a deleted document: %v", sealed, err)
		}
	}

	// The catalogue's share comes off the artifacts' limit (R-BAS-110).
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(filepath.Join(t.TempDir(), "volume"))
	c, err := New(ctx, cfg, testSigner(t), Options{Metrics: m, CatalogueMaxBytes: 300})
	if err != nil {
		t.Fatal(err)
	}
	if arts, docs := c.limits(); arts != 700 || docs != 200 || c.CatalogueMaxBytes() != 300 {
		t.Fatalf("limits with a catalogue's share of 300 of 1200: artifacts %d, documents %d", arts, docs)
	}
	if v := val(t, m.CacheCapacity.WithLabelValues(metrics.AreaArtifacts)); v != 700 {
		t.Errorf("the artifacts' capacity gauge is %v", v)
	}
}
