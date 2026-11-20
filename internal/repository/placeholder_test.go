package repository

import (
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The members of the built-in project upstream in a virtual repository and the placeholders among them (R-REPO-071, R-REPO-072):
// table tests, no database.

func up(key string) qname.Repo { return qname.Repo{Project: wellknown.ProjectKey, Key: key} }

func TestR_REPO_071_AMemberIsWrittenByItsKeyInItsProjectAndQualifiedFromUpstream(t *testing.T) {
	for _, c := range []struct {
		project string
		member  qname.Repo
		want    string
	}{
		{"web", qname.Repo{Project: "web", Key: "npm-local"}, "npm-local"},
		{"web", up("npmjs"), "upstream/npmjs"},
	} {
		if got := MemberRef(c.project, c.member); got != c.want {
			t.Errorf("MemberRef(%s, %s) = %q, want %q", c.project, c.member, got, c.want)
		}
	}
	for in, want := range map[string]string{
		"upstream/npmjs": "npmjs", "npmjs": "", "upstream/": "", "upstream/a/b": "", "web/npm-local": "", "Upstream/npmjs": "",
	} {
		got, ok := upstreamMemberKey(in)
		if got != want || ok != (want != "") {
			t.Errorf("upstreamMemberKey(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestR_REPO_072_APlaceholdersSentenceNamesItAndItsState(t *testing.T) {
	head := "upstream/npmjs is a member of this virtual repository, but "
	for _, c := range []struct {
		state  wellknown.State
		others int
		want   string
	}{
		{wellknown.StateNotSwitchedOn, 0, head + "an administrator of Quader has not switched it on."},
		{wellknown.StateSwitchedOff, 0, head + "an administrator of Quader switched it off."},
		{wellknown.StateRetired, 0, head + "Quader no longer offers it; remove it from the members."},
		{wellknown.StateNotSwitchedOn, 1, head + "an administrator of Quader has not switched it on. One more member of the project upstream is not in use either."},
		{wellknown.StateSwitchedOff, 3, head + "an administrator of Quader switched it off. 3 more members of the project upstream are not in use either."},
		{wellknown.StateActive, 0, ""},
		{"", 0, ""},
	} {
		if got := PlaceholderSentence(up("npmjs"), c.state, c.others); got != c.want {
			t.Errorf("%s/%d: %q, want %q", c.state, c.others, got, c.want)
		}
		e := PlaceholderNotFound(up("npmjs"), c.state, c.others)
		switch {
		case c.want == "" && e != nil:
			t.Errorf("%s: an error without a sentence: %v", c.state, e)
		case c.want != "" && (e == nil || e.Kind != errs.NotFound || e.Code != errs.CodeNotFound || e.Detail != c.want):
			t.Errorf("%s: %+v, want the not-found kind and code with the sentence", c.state, e)
		}
	}
}

func TestR_REPO_071_TheDocumentRulesOfAMemberOfUpstream(t *testing.T) {
	t.Run("R-REPO-071 upstream/<key> is a member and a pin member; any other qualified name is invalid_member", func(t *testing.T) {
		_, _, err := decodeValidate(t, virtualDoc(`{"members":["npm-local","upstream/npmjs","mobile/npm-local","web/npm-local","upstream/","upstream/a/b"],
			"pinning":{"rules":[{"packages":["left-*"],"members":["upstream/npmjs"]}]}}`), true)
		f := fieldsOf(err)
		for i := 2; i <= 5; i++ {
			if p := "/virtual/members/" + string(rune('0'+i)); f[p] != errs.CodeInvalidMember {
				t.Errorf("%s = %q (%v)", p, f[p], f)
			}
		}
		for _, p := range []string{"/virtual/members/0", "/virtual/members/1", "/virtual/pinning/rules/0/members/0"} {
			if _, bad := f[p]; bad {
				t.Errorf("%s refused: %v", p, f)
			}
		}
	})
	t.Run("R-REPO-071 a member of upstream listed twice is invalid_member", func(t *testing.T) {
		_, _, err := decodeValidate(t, virtualDoc(`{"members":["upstream/npmjs","upstream/npmjs"]}`), true)
		if f := fieldsOf(err); f["/virtual/members/1"] != errs.CodeInvalidMember || len(f) != 1 {
			t.Fatalf("fields = %v", f)
		}
	})
	t.Run("Q-REPO-401 a member of the own project and one of upstream with the same key are refused together", func(t *testing.T) {
		for _, members := range []string{`["npmjs","upstream/npmjs"]`, `["upstream/npmjs","npmjs"]`} {
			_, _, err := decodeValidate(t, virtualDoc(`{"members":`+members+`}`), true)
			if f := fieldsOf(err); f["/virtual/members/1"] != errs.CodeInvalidMember || len(f) != 1 {
				t.Errorf("%s: fields = %v", members, f)
			}
		}
	})
	t.Run("R-REPO-071 R-REPO-021 a deploy target of upstream is invalid_deploy_target", func(t *testing.T) {
		_, _, err := decodeValidate(t, virtualDoc(`{"members":["upstream/npmjs"],"deployTarget":"upstream/npmjs"}`), true)
		if f := fieldsOf(err); f["/virtual/deployTarget"] != codeInvalidDeployTarget {
			t.Fatalf("fields = %v", f)
		}
	})
}

// upstreamRow is the locked row of a repository of the project upstream as LockRepositoriesByKeys reads it.
func upstreamRow(format, kind string, disabled, switchedOnBefore, retired bool) q.LockRepositoriesByKeysRow {
	cfg := `{}`
	if disabled {
		cfg = `{"disabled":true}`
	}
	return q.LockRepositoriesByKeysRow{
		ID: 7, Key: "npmjs", Upstream: true, Class: string(Remote), Format: format, Lifecycle: string(Active), Config: []byte(cfg),
		UpstreamKind: kind, SwitchedOnBefore: switchedOnBefore, Retired: retired,
	}
}

func TestR_REPO_071_AMemberOfUpstreamIsAPresetOfTheFormatThatIsNotRetired(t *testing.T) {
	s := &Service{cat: testCatalog()}
	for _, c := range []struct {
		name   string
		check  memberCheck
		code   string
		detail string
		warn   string
	}{
		{"active", memberCheck{ref: "upstream/npmjs", row: upstreamRow("pkgfmt", "preset", false, true, false), found: true}, "", "", ""},
		{
			"not switched on: accepted with member_inactive",
			memberCheck{ref: "upstream/npmjs", row: upstreamRow("pkgfmt", "preset", true, false, false), found: true},
			"", "", "upstream/npmjs is not-switched-on",
		},
		{
			"switched off: accepted with member_inactive",
			memberCheck{ref: "upstream/npmjs", row: upstreamRow("pkgfmt", "preset", true, true, false), found: true},
			"", "", "upstream/npmjs is switched-off",
		},
		{
			"retired and new: refused",
			memberCheck{ref: "upstream/npmjs", row: upstreamRow("pkgfmt", "preset", true, true, true), found: true},
			errs.CodeInvalidMember, "no longer offered by Quader", "",
		},
		{
			"retired and listed before: stays",
			memberCheck{ref: "upstream/npmjs", row: upstreamRow("pkgfmt", "preset", true, true, true), found: true, listed: true},
			"", "", "upstream/npmjs is retired",
		},
		{"no such repository", memberCheck{ref: "upstream/nope"}, errs.CodeInvalidMember, "no repository of the project upstream", ""},
		{
			"a system repository",
			memberCheck{ref: "upstream/scanner-trivy", row: upstreamRow("pkgfmt", "system", false, false, false), found: true},
			errs.CodeInvalidMember, "used by Quader only", "",
		},
		{
			"another format",
			memberCheck{ref: "upstream/docker-hub", row: upstreamRow("oci", "preset", true, false, false), found: true},
			errs.CodeInvalidMember, "another format", "",
		},
		{
			"a Debian archive without keys",
			memberCheck{format: "deb", ref: "upstream/debian", row: upstreamRow("deb", "preset", false, true, false), found: true},
			errs.CodeDebMemberUnverified, "deb.upstreamKeys", "",
		},
	} {
		t.Run("R-REPO-071 "+c.name, func(t *testing.T) {
			if c.check.format == "" {
				c.check.format = "pkgfmt"
			}
			got := s.checkMember(c.check)
			if got.code != c.code || !strings.Contains(got.detail, c.detail) || (c.warn == "") != (got.warning == "") || !strings.Contains(got.warning, c.warn) {
				t.Fatalf("verdict %+v, want code %q detail ~%q warning ~%q", got, c.code, c.detail, c.warn)
			}
		})
	}
}

// upstreamPreset makes a fixture repository a preset repository of the project upstream in a state.
func upstreamPreset(state wellknown.State) repoOpt {
	return func(r *Repository, _ *admLists) {
		r.Name.Project = wellknown.ProjectKey
		r.Disabled = state != wellknown.StateActive
		r.Upstream = &UpstreamFacts{Kind: wellknown.KindPreset, State: state}
	}
}

func placeholderKeys(r Route) []string {
	out := make([]string, len(r.Placeholders))
	for i, m := range r.Placeholders {
		out[i] = m.Name.String()
	}
	return out
}

func TestR_REPO_072_APlaceholderIsDroppedAsDisabledAndNamedWhereItWouldHaveBeenAsked(t *testing.T) {
	f := newRouteFixture(t)
	f.repo("npm-local", Local, "npmlike")
	f.repo("npmjs", Remote, "npmlike", upstreamPreset(wellknown.StateNotSwitchedOn))
	f.repo("off", Remote, "npmlike", upstreamPreset(wellknown.StateSwitchedOff), exclude("left-*"))
	f.repo("live", Remote, "npmlike", upstreamPreset(wellknown.StateActive))
	f.repo("db", Remote, "npmlike", func(r *Repository, _ *admLists) {
		r.Name.Project = wellknown.ProjectKey
		r.Upstream = &UpstreamFacts{Kind: wellknown.KindSystem}
	})
	v := f.virtual("all", "npmlike", []string{"npm-local", "npmjs", "off", "live", "db"}, nil)
	t.Run("R-REPO-072 a placeholder and a system repository take no part; the placeholder that would have been asked is named", func(t *testing.T) {
		r := f.route(v, "left-pad")
		if got := memberKeys(r); !slices.Equal(got, []string{"npm-local", "live"}) {
			t.Fatalf("members = %v", got)
		}
		want := map[qname.Repo]string{up("npmjs"): DropDisabled, up("off"): DropDisabled, up("db"): DropDisabled}
		if !maps.Equal(r.Dropped, want) {
			t.Fatalf("dropped = %v, want %v", r.Dropped, want)
		}
		// off's exclude pattern keeps left-pad away from it: it would not have been asked, so it is no placeholder of this name.
		if got := placeholderKeys(r); !slices.Equal(got, []string{"upstream/npmjs"}) {
			t.Fatalf("placeholders = %v", got)
		}
		if got := placeholderKeys(f.route(v, "lodash")); !slices.Equal(got, []string{"upstream/npmjs", "upstream/off"}) {
			t.Fatalf("placeholders of lodash = %v", got)
		}
	})
	t.Run("R-REPO-072 R-REPO-023 a pin rule or a claim that takes the name elsewhere leaves the placeholder out", func(t *testing.T) {
		g := newRouteFixture(t)
		g.repo("npm-local", Local, "npmlike")
		g.repo("npmjs", Remote, "npmlike", upstreamPreset(wellknown.StateNotSwitchedOn))
		g.repo("npmjs-own", Remote, "npmlike")
		w := g.virtual("all", "npmlike", []string{"npm-local", "npmjs", "npmjs-own"}, []PinRule{
			{Packages: []string{"pinned-*"}, Members: []string{"npm-local"}},
			{Packages: []string{"upper-*"}, Members: []string{"upstream/npmjs"}},
		})
		g.claim("npm-local", "@web/*", ScopeMembers)
		if got := placeholderKeys(g.route(w, "pinned-x")); len(got) != 0 {
			t.Errorf("a pin rule without the member: placeholders = %v", got)
		}
		if got := placeholderKeys(g.route(w, "@web/ui")); len(got) != 0 {
			t.Errorf("a claim of npm-local: placeholders = %v", got)
		}
		// A pin rule names the member of upstream by its qualified name: it selects upstream/npmjs, not a repository of the own
		// project with another key, and the placeholder is named.
		r := g.route(w, "upper-case")
		if got := placeholderKeys(r); !slices.Equal(got, []string{"upstream/npmjs"}) || r.Dropped[rn("npmjs-own")] != DropPinnedOut {
			t.Errorf("pinned to upstream/npmjs: placeholders %v, dropped %v", got, r.Dropped)
		}
	})
}

func TestR_REPO_071_APinRuleNamesAMemberOfUpstreamByItsQualifiedName(t *testing.T) {
	f := newRouteFixture(t)
	f.repo("npm-local", Local, "npmlike")
	f.repo("npmjs", Remote, "npmlike", upstreamPreset(wellknown.StateActive))
	v := f.virtual("all", "npmlike", []string{"npm-local", "npmjs"}, []PinRule{{Packages: []string{"left-*"}, Members: []string{"upstream/npmjs"}}})
	if got := memberKeys(f.route(v, "left-pad")); !slices.Equal(got, []string{"npmjs"}) {
		t.Fatalf("members of left-pad = %v", got)
	}
	f.claim("npm-local", "left-*", ScopeGlobal)
	idx := buildClaimIndex(f.rows, slog.New(slog.DiscardHandler))
	if remoteRefused(f.cat, idx, f.repos["npmjs"], "left-pad", &v) {
		t.Error("the pin rule that names upstream/npmjs does not lift the remote refusal of a global claim")
	}
	if !remoteRefused(f.cat, idx, f.repos["npmjs"], "left-pad", nil) {
		t.Error("addressed directly, the well-known repository does not refuse a globally claimed name")
	}
}

func TestR_REPO_061_AJournaledMemberOfUpstreamKeepsItsQualifiedName(t *testing.T) {
	if got := refOf("web", relRef{id: 7, key: "upstream/npmjs"}); got.Key != "upstream/npmjs" || got.RepositoryID != "7" {
		t.Fatalf("ref = %+v", got)
	}
	if got := refOf("web", relRef{id: 3, key: "npm-local"}); got.Key != "web/npm-local" {
		t.Fatalf("ref = %+v", got)
	}
}
