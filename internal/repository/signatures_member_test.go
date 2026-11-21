package repository

import (
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_SIG_040_TheFetchSwitchIsAMemberOfARemoteRepository: signatures.fetch is settable on a remote repository and kept as written; on a
// local or a virtual repository, which fetch nothing, it answers 422 not_applicable at /signatures/fetch; a value that is no boolean is
// refused by the decoder (R-SIG-026, R-SIG-040, Q-SIG-108). Unit, milliseconds.
func TestR_SIG_040_TheFetchSwitchIsAMemberOfARemoteRepository(t *testing.T) {
	d, err := vulnValidate(t, `{"key":"npm-remote","class":"remote","format":"npm","remote":{"url":"https://registry.npmjs.org/"},"signatures":{"fetch":false}}`)
	if err != nil {
		t.Fatal(err)
	}
	if d.Signatures == nil || d.Signatures.Fetch == nil || *d.Signatures.Fetch {
		t.Fatalf("member = %+v, want fetch false", d.Signatures)
	}
	for class, body := range map[string]string{
		"local":   `{"key":"npm-local","class":"local","format":"npm","signatures":{"fetch":false}}`,
		"virtual": `{"key":"npm-all","class":"virtual","format":"npm","signatures":{"fetch":true}}`,
	} {
		_, err := vulnValidate(t, body)
		if got := fieldsOf(err)["/signatures/fetch"]; got != errs.CodeNotApplicable {
			t.Errorf("%s: fields = %v (%v), want not_applicable at /signatures/fetch", class, fieldsOf(err), err)
		}
	}
	if _, err := vulnValidate(t, `{"key":"npm-local","class":"local","format":"npm","signatures":{}}`); err != nil {
		t.Errorf("an empty member: %v", err)
	}
	if _, err := DecodeDocument([]byte(`{"key":"npm-remote","class":"remote","format":"npm","signatures":{"fetch":"no"}}`)); err == nil {
		t.Error("a fetch that is no boolean was decoded")
	}
}
