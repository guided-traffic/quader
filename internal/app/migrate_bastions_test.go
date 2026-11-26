package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_BAS_127_TheDeclaredTokenIsReadFromItsFileAndChecked: migrate reads each entry's token file as the Bastion reads its own -
// one trailing line break dropped - and checks the networks by the registration's rules; a missing, empty, oversized or malformed
// file, a token of another type and a network /0 stop the run with the entry named, every problem in one error, and no error holds
// the value. Milliseconds, files of bytes.
func TestR_BAS_127_TheDeclaredTokenIsReadFromItsFileAndChecked(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	bst, _ := token.Generate(token.TypeBST)
	pat, _ := token.Generate(token.TypePAT)
	good := write("good", bst+"\n")
	got, err := declaredBastions(map[string]config.DeclaredBastion{
		"partners": {Description: "Partners", Egress: []string{"10.252.0.0/16", "198.51.100.20"}, TokenFile: good},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "partners" || got[0].Token.Hash != token.Hash(bst) || got[0].Token.Hint != token.Hint(bst) ||
		len(got[0].Egress) != 2 || got[0].Egress[1].String() != "198.51.100.20/32" {
		t.Errorf("declared %+v", got)
	}

	_, err = declaredBastions(map[string]config.DeclaredBastion{
		"a-missing":   {TokenFile: filepath.Join(dir, "missing")},
		"b-empty":     {TokenFile: write("empty", "\n")},
		"c-large":     {TokenFile: write("large", strings.Repeat("x", 2000))},
		"d-malformed": {TokenFile: write("malformed", bst[:56]+"!\n")},
		"e-pat":       {TokenFile: write("pat", pat)},
		"f-space":     {TokenFile: write("space", " "+bst)},
		"g-every":     {TokenFile: good, Egress: []string{"0.0.0.0/0"}},
	})
	if err == nil {
		t.Fatal("no error")
	}
	msg := err.Error()
	for _, want := range []string{
		"bastions.declared.a-missing.token_file: cannot read", "bastions.declared.b-empty.token_file", "is empty",
		"bastions.declared.c-large.token_file", "larger than 1024 bytes", "bastions.declared.d-malformed.token_file",
		"not a Bastion token", "bastions.declared.e-pat.token_file", "bastions.declared.f-space.token_file",
		"bastions.declared.g-every.egress", "quader admin bastion token generate",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not say %q:\n%s", want, msg)
		}
	}
	for _, secret := range []string{bst, bst[8:], pat} {
		if strings.Contains(msg, secret) {
			t.Error("the error holds a token's value")
		}
	}
}
