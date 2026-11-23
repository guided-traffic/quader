package app

import (
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/secrets"
)

// TestR_ARCH_021_TheReencryptionWalksEverySealedColumn pins the sealed columns that secrets-reencrypt walks, which are the columns
// the packages of a replica register with internal/secrets (storage.md R-STOR-083 for the storage keys). A column a change seals
// fails here until the list says so, in the unit job; the integration area repository asserts the task's result against
// secrets.Columns and no longer counts them itself (Q-ARCH-701). Milliseconds.
func TestR_ARCH_021_TheReencryptionWalksEverySealedColumn(t *testing.T) {
	want := []string{
		"deb_signing_keys.private_key", "repository_secrets.sealed", "signing_identities.sealed_key", "storage_keys.sealed_key",
		"upload_sessions.sealed_data_key",
	}
	var got []string
	for _, c := range secrets.Columns() {
		got = append(got, c.String())
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("the sealed columns are %v, want %v: name a new one here and in R-ARCH-021's list", got, want)
	}
}
