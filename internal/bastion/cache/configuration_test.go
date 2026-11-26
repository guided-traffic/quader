package cache

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestR_BAS_093_TheStoredConfigurationIsSignedWhereEntriesAre: the configuration a replica applied is one object,
// configuration/current.json, signed with the signing key (Q-BAS-327); a record changed by a writer of the volume, or one
// signed with another key, fails its signature; nothing stored is ErrNotFound. With a cache key it is an object of the stored
// format bound to its name, holding no plaintext, and readable only under that key (R-BAS-061).
func TestR_BAS_093_TheStoredConfigurationIsSignedWhereEntriesAre(t *testing.T) {
	ctx := context.Background()
	payload := []byte(`{"revision":17}`)
	r := newRig(t)
	if _, err := r.LoadConfiguration(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nothing stored: %v, want ErrNotFound", err)
	}
	if err := r.StoreConfiguration(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if got, err := r.LoadConfiguration(ctx); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("read back: %q, %v", got, err)
	}
	file := filepath.Join(r.dir, "configuration", "current.json")
	rec, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the object is not configuration/current.json: %v", err)
	}
	forged := bytes.Replace(rec, []byte("17"), []byte("99"), 1)
	if err := os.WriteFile(file, forged, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadConfiguration(ctx); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a record changed on the volume: %v, want ErrBadSignature", err)
	}
	other, err := NewSigner(bytes.Repeat([]byte{3}, MinKeyBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, other.Sign(DomainConfiguration, fileConfiguration, payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadConfiguration(ctx); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a record signed with another key: %v, want ErrBadSignature", err)
	}

	t.Run("with a cache key", func(t *testing.T) {
		dir, clk := filepath.Join(t.TempDir(), "volume"), newClock()
		key := writeKeyFile(t, keyK())
		s := sealedRig(t, dir, key, clk)
		if err := s.StoreConfiguration(ctx, payload); err != nil {
			t.Fatal(err)
		}
		for name, data := range storeFiles(t, dir) {
			if bytes.Contains(data, []byte("revision")) {
				t.Fatalf("%s holds the configuration in plaintext", name)
			}
		}
		if got, err := sealedRig(t, dir, key, clk).LoadConfiguration(ctx); err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("read back under the key: %q, %v", got, err)
		}
		if _, err := sealedRig(t, dir, writeKeyFile(t, keyK2()), clk).LoadConfiguration(ctx); err == nil {
			t.Fatal("the configuration was read under another cache key")
		}
	})
}
