package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/token"
)

// TestR_BAS_004_TheBastionCommandsRefuseWhatIsIncomplete: the three commands of R-BAS-004 print their usage and
// touch no database when the name or the token ID is missing or the command is unknown (the work of the
// commands is shown against a database in test/integration/bastionreg).
func TestR_BAS_004_TheBastionCommandsRefuseWhatIsIncomplete(t *testing.T) {
	s := streams{stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard}
	for _, args := range [][]string{
		{"admin", "bastion"},
		{"admin", "bastion", "create"},
		{"admin", "bastion", "frobnicate"},
		{"admin", "bastion", "token"},
		{"admin", "bastion", "token", "create"},
		{"admin", "bastion", "token", "revoke"},
		{"admin", "bastion", "token", "revoke", "partners"},
		{"admin", "bastion", "token", "list", "partners"},
	} {
		err := adminCmd(args[1:], s)
		var u usageError
		if !errors.As(err, &u) || !strings.Contains(u.Error(), "quader admin bastion") {
			t.Errorf("%v: err = %v, want the usage of quader admin bastion", args, err)
		}
	}
	if !strings.Contains(adminUsage, "bastion token revoke") {
		t.Error("the usage of quader admin does not list the Bastion commands")
	}
}

// TestR_BAS_127_TheGeneratedValueIsABastionToken: quader admin bastion token generate prints one value and nothing else, in the
// format the main installation issues and the declaration and the Bastion check (qdr_bst_, 57 characters, the checksum), a new one each
// time, without a configuration or a database; an argument is refused with the usage. Milliseconds, no data.
func TestR_BAS_127_TheGeneratedValueIsABastionToken(t *testing.T) {
	gen := func() string {
		t.Helper()
		var out strings.Builder
		if err := adminCmd([]string{"bastion", "token", "generate"}, streams{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	a, b := gen(), gen()
	if !strings.HasSuffix(a, "\n") || strings.Count(a, "\n") != 1 {
		t.Fatalf("output %q is not one line", a)
	}
	v := strings.TrimSuffix(a, "\n")
	if typ, err := token.Check(v); err != nil || typ != token.TypeBST {
		t.Errorf("%s: type %q, %v", token.Hint(v), typ, err)
	}
	if _, err := token.ParseDeclaredBastion(v); err != nil {
		t.Errorf("the declaration refuses the value: %v", err)
	}
	if a == b {
		t.Error("two runs printed the same value")
	}
	var u usageError
	if err := adminCmd([]string{"bastion", "token", "generate", "partners"}, streams{stdout: io.Discard, stderr: io.Discard}); !errors.As(err, &u) {
		t.Errorf("an argument: %v, want the usage", err)
	}
}
