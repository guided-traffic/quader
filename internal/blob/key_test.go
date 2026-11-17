package blob

import (
	"strings"
	"testing"
)

func TestKey(t *testing.T) {
	d, ok := ParseDigest("5be0c1d2" + strings.Repeat("ab", 28))
	if !ok {
		t.Fatal("digest does not parse")
	}
	t.Run("R-STOR-001 the key is prefix/sha256/<h0h1>/<h2h3>/<hex>", func(t *testing.T) {
		want := "quader/sha256/5b/e0/5be0c1d2" + strings.Repeat("ab", 28)
		if got := Key("quader", d); got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	})
	t.Run("R-STOR-001 an empty prefix starts at sha256 without a leading slash", func(t *testing.T) {
		if got := Key("", d); !strings.HasPrefix(got, "sha256/5b/e0/") {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("R-STOR-001 the key function takes a digest and nothing else", func(t *testing.T) {
		// A path, a repository key or traversal text cannot reach the key: the
		// only input is 32 bytes, and the output is hex.
		var zero Digest
		k := Key("quader", zero)
		if strings.Contains(k, "..") || strings.Contains(k, "%") || strings.Count(k, "/") != 4 {
			t.Fatalf("unexpected key %s", k)
		}
	})
	t.Run("R-STOR-001 ParseKey inverts Key and refuses foreign keys", func(t *testing.T) {
		if got, ok := ParseKey("quader", Key("quader", d)); !ok || got != d {
			t.Fatalf("round trip: %v %v", got, ok)
		}
		for _, bad := range []string{
			"quader/sha256/5b/e0/5be0c1d2",   // short
			"quader/sha256/00/00/" + d.Hex(), // wrong fan-out
			"quader/sha256/5b/e0/" + strings.ToUpper(d.Hex()),
			"other/sha256/5b/e0/" + d.Hex(),
			"quader/_probe/replica-1",
		} {
			if _, ok := ParseKey("quader", bad); ok {
				t.Errorf("%s parsed", bad)
			}
		}
	})
}
