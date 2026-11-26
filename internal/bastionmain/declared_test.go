package bastionmain

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/token"
)

// declaredToken is a declared value of a fixed random part, its hash and hint.
func declaredToken(t *testing.T, b byte) token.DeclaredBastionToken {
	t.Helper()
	var r [32]byte
	r[0] = b
	d, err := token.ParseDeclaredBastion(token.FromRandom(token.TypeBST, r))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestR_BAS_127_TheRunDecidesEachDeclaredTokenFromWhatTheRegistrationHolds: a registration not declared yet, or declared without a
// token, looks the value up (to store it or take an issued token); the same value as the declared token writes nothing; another value
// is looked up, so the old one is replaced; the same value revoked since stops the run, naming the hint and never the value; a
// registration marked but no longer named is released and one still named is not. Milliseconds, no data.
func TestR_BAS_127_TheRunDecidesEachDeclaredTokenFromWhatTheRegistrationHolds(t *testing.T) {
	a, b := declaredToken(t, 1), declaredToken(t, 2)
	if step, err := planToken(nil, a); err != nil || step != tokenLookup {
		t.Errorf("created: %v %v, want a lookup", step, err)
	}
	if step, err := planToken(&declaredNow{ID: "r1", Name: "partners"}, a); err != nil || step != tokenLookup {
		t.Errorf("marked without a token: %v %v, want a lookup", step, err)
	}
	now := &declaredNow{ID: "r1", Name: "partners", TokenID: "t1", Hash: a.Hash[:]}
	if step, err := planToken(now, a); err != nil || step != tokenKeep {
		t.Errorf("unchanged: %v %v, want keep", step, err)
	}
	if step, err := planToken(now, b); err != nil || step != tokenLookup {
		t.Errorf("changed: %v %v, want a lookup", step, err)
	}
	now.Revoked = true
	_, err := planToken(now, a)
	if err == nil || !strings.Contains(err.Error(), "revoked") || !strings.Contains(err.Error(), a.Hint) {
		t.Errorf("revoked: %v", err)
	}
	if step, err := planToken(now, b); err != nil || step != tokenLookup {
		t.Errorf("a new value after a revocation: %v %v, want a lookup", step, err)
	}

	marked := []declaredNow{{ID: "r1", Name: "edge"}, {ID: "r2", Name: "partners"}}
	gone := released(marked, []Declared{{Name: "partners", Token: a}})
	if len(gone) != 1 || gone[0].Name != "edge" {
		t.Errorf("released %+v, want edge", gone)
	}
	if gone := released(marked, nil); len(gone) != 2 {
		t.Errorf("no declaration releases %d, want both", len(gone))
	}
	if (DeclaredResult{}).Changed() || !(DeclaredResult{Released: []string{"edge"}}).Changed() {
		t.Error("Changed does not tell a run that wrote from one that did not")
	}
}
