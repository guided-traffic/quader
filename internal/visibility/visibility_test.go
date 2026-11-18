package visibility

import (
	"fmt"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_AUTHZ_033_TheOrderOfTheLevels: private comes before internal before public, and a string that is no level is none of them.
func TestR_AUTHZ_033_TheOrderOfTheLevels(t *testing.T) {
	order := []string{Private, Internal, Public}
	for i, a := range order {
		if !Valid(a) || Rank(a) != i {
			t.Errorf("%s: valid %v rank %d, want %d", a, Valid(a), Rank(a), i)
		}
		for j, b := range order {
			if got := Above(a, b); got != (i > j) {
				t.Errorf("Above(%s, %s) = %v", a, b, got)
			}
			want := a
			if j < i {
				want = b
			}
			if got := Lower(a, b); got != want {
				t.Errorf("Lower(%s, %s) = %s, want %s", a, b, got, want)
			}
		}
	}
	for _, s := range []string{"", "Private", "open", "authenticated", "anonymous"} {
		if Valid(s) || Rank(s) != -1 {
			t.Errorf("%q is taken for a level", s)
		}
	}
}

// TestR_AUTHZ_033_PublicNotAllowedIsA409: the refusal of public while the default endpoint admits no request without a credential.
func TestR_AUTHZ_033_PublicNotAllowedIsA409(t *testing.T) {
	e, ok := errs.As(PublicNotAllowed())
	if !ok || e.Status() != 409 || e.Code != errs.CodePublicNotAllowed {
		t.Fatalf("%v", PublicNotAllowed())
	}
	if f := InvalidLevel("/visibility/wiki"); f.Pointer != "/visibility/wiki" || f.Code != "invalid_value" {
		t.Errorf("%+v", f)
	}
}

// TestR_AUTHZ_037_WhoMayChooseALevel: a restricted level is chosen by an administrator only, every other level by anyone, and the
// fallback of an absent level is the highest level at or below the one it would take that the caller may choose (Q-AUTHZ-283,
// Q-AUTHZ-286).
func TestR_AUTHZ_037_WhoMayChooseALevel(t *testing.T) {
	r := Rules{Default: Public, Restricted: []string{Private, Internal}}
	for _, c := range []struct {
		level string
		admin bool
		want  bool
	}{
		{Public, false, true},
		{Internal, false, false},
		{Private, false, false},
		{Internal, true, true},
		{Private, true, true},
	} {
		if got := r.Allows(c.level, c.admin); got != c.want {
			t.Errorf("Allows(%s, admin %v) = %v", c.level, c.admin, got)
		}
	}
	for _, c := range []struct {
		rules         Rules
		ceiling, want string
		admin         bool
	}{
		{Rules{Restricted: []string{Internal}}, Internal, Private, false},
		{Rules{Restricted: []string{Internal}}, Internal, Internal, true},
		{Rules{Restricted: []string{Public}}, Public, Internal, false},
		{Rules{Restricted: []string{Internal, Public}}, Public, Private, false},
		{Rules{Restricted: []string{Private, Internal}}, Internal, "", false},
		{Rules{Restricted: []string{Private}}, Private, "", false},
		{Rules{}, Internal, Internal, false},
	} {
		if got := c.rules.Highest(c.ceiling, c.admin); got != c.want {
			t.Errorf("%v: Highest(%s, admin %v) = %q, want %q", c.rules.Restricted, c.ceiling, c.admin, got, c.want)
		}
	}
	e, ok := errs.As(LevelRestricted(Public, "/visibility/artifacts"))
	if !ok || e.Status() != 403 || e.Code != errs.CodeVisibilityLevelRestricted || len(e.Fields) != 1 ||
		e.Fields[0].Pointer != "/visibility/artifacts" || e.Fields[0].Code != errs.CodeVisibilityLevelRestricted {
		t.Errorf("%+v", e)
	}
}

// TestR_API_075_APatchOfTheRestrictedLevels: the set is normalized, a value that is no level is 422 at its index, and the default
// is never restricted, at the member that makes it so (Q-AUTHZ-284, Q-API-331).
func TestR_API_075_APatchOfTheRestrictedLevels(t *testing.T) {
	set := func(levels ...string) *[]string { return &levels }
	level := func(l string) *string { return &l }
	p, err := CheckPatch(Patch{RestrictedLevels: set(Public, Private, Public)})
	if err != nil || fmt.Sprint(*p.RestrictedLevels) != "[private public]" {
		t.Fatalf("normalized %v, %v", p.RestrictedLevels, err)
	}
	_, err = CheckPatch(Patch{DefaultLevel: level("open"), RestrictedLevels: set(Public, "everyone")})
	if e, ok := errs.As(err); !ok || e.Status() != 422 || fmt.Sprint(pointers(e)) != "[/defaultLevel /restrictedLevels/1]" {
		t.Errorf("%v", err)
	}
	before := Rules{Default: Private, Restricted: []string{}}
	for _, c := range []struct {
		name    string
		patch   Patch
		pointer string
		after   string
	}{
		{"restricting the default", Patch{RestrictedLevels: set(Private)}, "/restrictedLevels", ""},
		{"an open default", Patch{DefaultLevel: level(Public)}, "", "public []"},
		{"both at once, clashing", Patch{DefaultLevel: level(Internal), RestrictedLevels: set(Internal)}, "/restrictedLevels", ""},
		{"both at once, apart", Patch{DefaultLevel: level(Internal), RestrictedLevels: set(Public)}, "", "internal [public]"},
	} {
		after, err := Apply(before, c.patch)
		if c.pointer == "" {
			if err != nil || fmt.Sprint(after.Default, " ", after.Restricted) != c.after {
				t.Errorf("%s: %v %v", c.name, after, err)
			}
			continue
		}
		e, ok := errs.As(err)
		if !ok || e.Status() != 422 || len(e.Fields) != 1 || e.Fields[0].Pointer != c.pointer || e.Fields[0].Code != CodeDefaultRestricted {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	_, err = Apply(Rules{Default: Private, Restricted: []string{Public}}, Patch{DefaultLevel: level(Public)})
	if e, ok := errs.As(err); !ok || e.Fields[0].Pointer != "/defaultLevel" {
		t.Errorf("a restricted level as the default: %v", err)
	}
}

// TestR_API_075_TheChangeIsAuditedWithBothSettings: settings.visibility.changed carries the default and the restricted levels
// before and after, an empty set as [] (Q-API-332).
func TestR_API_075_TheChangeIsAuditedWithBothSettings(t *testing.T) {
	ev := ChangedEvent(Caller{}, Rules{Default: Private}, Rules{Default: Private, Restricted: []string{Public}})
	want := `{"after":{"defaultLevel":"private","restrictedLevels":["public"]},"before":{"defaultLevel":"private","restrictedLevels":[]}}`
	if ev.Type != "settings.visibility.changed" || string(ev.Details) != want {
		t.Errorf("%s %s", ev.Type, ev.Details)
	}
}

func pointers(e *errs.Error) []string {
	var out []string
	for _, f := range e.Fields {
		out = append(out, f.Pointer)
	}
	return out
}
