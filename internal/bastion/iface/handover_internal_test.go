package iface

import (
	"testing"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/config"
)

// The interface is a leaf that imports nothing of the module (R-BAS-001, R-ARCH-002), so the two rules of other packages that
// R-BAS-073's checks need are written in handover.go. These tests hold them to their owners.

// TestR_BAS_073_TheStoredSizeIsTheFormats: storedSizeOf is blobcrypt.StoredSize, S(L) of R-STOR-076, at every boundary of
// a segment and at the ends of the range, and both refuse the sizes outside it.
func TestR_BAS_073_TheStoredSizeIsTheFormats(t *testing.T) {
	seg := int64(blobcrypt.SegmentSize)
	for _, l := range []int64{0, 1, seg - 1, seg, seg + 1, 2*seg - 1, 2 * seg, 80 * seg, 5242880, 1<<32 + 7, blobcrypt.MaxPlainSize - 1, blobcrypt.MaxPlainSize} {
		want, err := blobcrypt.StoredSize(l)
		got, ok := storedSizeOf(l)
		if err != nil || !ok || got != want {
			t.Errorf("S(%d): interface %d (%v), format %d (%v)", l, got, ok, want, err)
		}
	}
	for _, l := range []int64{-1, blobcrypt.MaxPlainSize + 1} {
		_, err := blobcrypt.StoredSize(l)
		if _, ok := storedSizeOf(l); ok || err == nil {
			t.Errorf("S(%d) is defined: interface %v, format %v", l, ok, err)
		}
	}
	if HandoverKeyBytes != blobcrypt.KeySize {
		t.Errorf("a handed data key is %d bytes, the format's %d", HandoverKeyBytes, blobcrypt.KeySize)
	}
}

// TestR_BAS_073_TheConnectionIsAnIdentifierOfTheConfiguration: validConnection is config.ValidConnectionID (R-STOR-050).
func TestR_BAS_073_TheConnectionIsAnIdentifierOfTheConfiguration(t *testing.T) {
	for _, id := range []string{
		"", "d", "de", "default", "store-2", "a-b", "a--b", "-ab", "ab-", "2ab", "Ab", "a_b", "a.b", "aé",
		"a234567890123456789012345678901b", "a2345678901234567890123456789012c",
	} {
		if got, want := validConnection(id), config.ValidConnectionID(id); got != want {
			t.Errorf("%q: interface %v, configuration %v", id, got, want)
		}
	}
}
