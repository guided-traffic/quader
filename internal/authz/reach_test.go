package authz

import (
	"testing"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// reachFixture is the fixture of the read shapes (R-BAS-012). Repositories:
//
//	npm-a, npm-b      local
//	npm-strict        local
//	npm-pub, npm-int  local, public and internal levels; npm-vpub and npm-vmix virtual over them
//	npmjs             remote, fills on read
//	npm-frozen        remote, fills on deploy
//	npm-v             virtual over npm-a and npmjs
//	npm-v2            virtual over npm-v and npm-frozen (nested)
//	npm-vb            virtual over npm-b
//	npm-empty         virtual without members
//
// Subjects: bot reads everything and deploys into the frozen mirror; devs (alice) read npm-a, npmjs and
// npm-v, and npm-b for @a/* only; bob holds a path-only grant on npm-a; carol holds nothing; anonymous reads
// only what a public level admits; the auditor holds content:metadata-read and no grant; ops holds the admin role.
//
// The cases of the package shapes (R-BAS-080), each the one that shows a rule: the fillers read everything and
// may fill the frozen mirror for @acme/* only; ext, an external user, reads @a/* of npm-pub, whose wiki is
// internal; dave reads the package document "x" of npm-strict and no other path; the second auditor holds
// content:metadata-read with grants on npm-a and npmjs (not on npm-v) and dave's grant; the acme readers read
// @acme/* of npm-strict, and the acme path readers the same within the paths @acme/**; the v3 readers read
// npm-v3, a virtual repository over npm-v and npm-b, and every leaf below it, and not npm-v, whose gate the
// resolver asks as a member's read. The exposure rule of the endpoint customers admits @acme/* of npm-a from
// version 2.0.0 on.
func reachFixture() *fixture {
	f := newFixture().
		repo("npm-a", "local", "npm", "").
		repo("npm-b", "local", "npm", "").
		repo("npm-strict", "local", "npm", "").
		repo("npmjs", "remote", "npm", `{"remote":{"cacheFillAction":"read"}}`).
		repo("npm-frozen", "remote", "npm", `{"remote":{"cacheFillAction":"deploy"}}`).
		repo("npm-v", "virtual", "npm", `{"virtual":{"members":["npm-a","npmjs"]}}`).
		repo("npm-v2", "virtual", "npm", `{"virtual":{"members":["npm-v","npm-frozen"]}}`).
		repo("npm-vb", "virtual", "npm", `{"virtual":{"members":["npm-b"]}}`).
		repo("npm-empty", "virtual", "npm", `{"virtual":{"members":[]}}`)
	f.policy(policyDef{name: "bot-read", selectors: []string{"@any"}, entries: map[string][]string{idBot: {"read"}}})
	f.policy(policyDef{name: "bot-fill", selectors: []string{"npm-frozen"}, entries: map[string][]string{idBot: {"deploy"}}})
	f.policy(policyDef{name: "readers", selectors: []string{"@any"}, entries: map[string][]string{"group-readers": {"read"}}})
	f.policy(policyDef{name: "v2-readers", selectors: []string{"npm-v2", "npm-a", "npmjs", "npm-frozen"}, entries: map[string][]string{"group-v2": {"read"}}})
	f.policy(policyDef{name: "devs", selectors: []string{"npm-a", "npmjs", "npm-v", "npm-vb"}, entries: map[string][]string{idDevs: {"read"}}})
	f.policy(policyDef{name: "devs-b", selectors: []string{"npm-b"}, pkgInc: []string{"@a/*"}, entries: map[string][]string{idDevs: {"read"}}})
	f.policy(policyDef{name: "bob-path", selectors: []string{"npm-a"}, pathInc: []string{"x/**"}, entries: map[string][]string{idBob: {"read"}}})
	f.bind(RoleAdmin, idOps).bind(RoleAuditor, idAuditorDirect)
	// The levels (R-AUTHZ-033): npm-pub and the virtual repository over it are public, npm-int internal, and npm-vmix is public
	// over both, so an anonymous subject reads through npm-vpub and not through npm-vmix (R-AUTHZ-009 rule 6).
	f.repo("npm-pub", "local", "npm", "").repo("npm-int", "local", "npm", "").repo("npm-open", "local", "npm", "")
	f.repo("npm-vpub", "virtual", "npm", `{"virtual":{"members":["npm-pub"]}}`)
	f.repo("npm-vmix", "virtual", "npm", `{"virtual":{"members":["npm-pub","npm-int"]}}`)
	f.levels("npm-pub", "public", "internal").levels("npm-vpub", "public", "public").levels("npm-vmix", "public", "public")
	f.levels("npm-int", "internal", "internal").levels("npm-open", "public", "public")
	f.exposure(ruleDef{endpoint: "customers", name: "acme", repo: "npm-a", dims: ruleDims{
		Packages: PatternLists{Include: []string{"@acme/*"}}, Versions: VersionLists{Range: ptr(">=2.0.0")},
	}})
	f.policy(policyDef{name: "fillers-read", selectors: []string{"@any"}, entries: map[string][]string{"group-fillers": {"read"}}})
	f.policy(policyDef{name: "fillers-acme", selectors: []string{"npm-frozen"}, pkgInc: []string{"@acme/*"}, entries: map[string][]string{"group-fillers": {"deploy"}}})
	f.policy(policyDef{name: "ext-pub", selectors: []string{"npm-pub"}, pkgInc: []string{"@a/*"}, entries: map[string][]string{"group-ext": {"read"}}})
	f.policy(policyDef{name: "dave-doc", selectors: []string{"npm-strict"}, pathInc: []string{"x"}, entries: map[string][]string{"group-dave": {"read"}}})
	f.policy(policyDef{name: "auditors-members", selectors: []string{"npm-a", "npmjs"}, entries: map[string][]string{"group-auditors": {"read"}}})
	f.policy(policyDef{name: "auditors-doc", selectors: []string{"npm-strict"}, pathInc: []string{"x"}, entries: map[string][]string{"group-auditors": {"read"}}})
	f.policy(policyDef{name: "acme-readers", selectors: []string{"npm-strict"}, pkgInc: []string{"@acme/*"}, entries: map[string][]string{"group-acme": {"read"}}})
	f.policy(policyDef{
		name: "acme-path-readers", selectors: []string{"npm-strict"}, pkgInc: []string{"@acme/*"}, pathInc: []string{"@acme/**"},
		entries: map[string][]string{"group-acme-path": {"read"}},
	})
	f.bind(RoleAuditor, "group-auditors")
	f.repo("npm-v3", "virtual", "npm", `{"virtual":{"members":["npm-v","npm-b"]}}`)
	f.policy(policyDef{name: "v3-readers", selectors: []string{"npm-v3", "npm-a", "npmjs", "npm-b"}, entries: map[string][]string{"group-v3": {"read"}}})
	return f
}

// reachKeys are the repository keys of the reach fixture, and one it does not hold.
var reachKeys = []string{
	"npm-a", "npm-b", "npm-strict", "npmjs", "npm-frozen", "npm-v", "npm-v2", "npm-v3", "npm-vb", "npm-empty", "npm-pub", "npm-open", "npm-int",
	"npm-vpub", "npm-vmix", "ghost",
}

// reachEndpoints are the endpoints the subjects of the reach fixture call through: none, one that exposes everything and one with
// exposure rules.
var reachEndpoints = []string{"", "default", "customers"}

// reachSubjects are the subjects of the reach fixture.
func reachSubjects() map[string]Subject {
	byPackage := func(sub Subject, key, pkg string) Subject {
		return tokenScop(sub, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{inst(key)}, Packages: scope.Patterns{Include: []string{pkg}}, Actions: []string{"read"}}}})
	}
	return map[string]Subject{
		"bot": botSub, "alice": aliceSub, "bob": bobSub, "carol": carolSub, "admin": adminSub, "anonymous": anonSub,
		"auditor": userSub(idAuditorDirect, "auditor"), "reader": userSub("user-reader", "reader", "group-readers"),
		"v2 reader":                userSub("user-v2", "v2", "group-v2"),
		"scoped bot":               tokenScop(botSub, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{"it/npm-a"}, Actions: []string{"read"}}}}),
		"scoped bot through npm-v": tokenScop(botSub, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{"it/npm-v"}, Actions: []string{"read"}}}}),
		"inheriting bot":           tokenScop(botSub, scope.Scope{Inherit: true}),
		"session bot":              session(botSub),
		"external carol":           external(carolSub),
		"external alice":           external(aliceSub),
		"ci":                       ciSub,
		"scoped carol":             tokenScop(carolSub, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{"it/npm-b"}, Actions: []string{"read"}}}}),
		"filler":                   userSub("user-filler", "filler", "group-fillers"),
		"ext":                      external(userSub("user-ext", "ext", "group-ext")),
		"carol scoped to @a/*":     byPackage(carolSub, "npm-pub", "@a/*"),
		"bot scoped to @acme/*":    byPackage(botSub, "npm-a", "@acme/*"),
		"dave":                     userSub("user-dave", "dave", "group-dave"),
		"auditor with grants":      userSub("user-auditor-2", "auditor2", "group-auditors"),
		"acme reader":              userSub("user-acme", "acme", "group-acme"),
		"acme path reader":         userSub("user-acme-path", "acmepath", "group-acme-path"),
		"v3 reader":                userSub("user-v3", "v3", "group-v3"),
	}
}

func reachCeiling() *ExposureCeiling {
	return NewExposureCeiling(map[string]string{"default": ExposureModeAll, "customers": ExposureModeRules}, testRegistry())
}

// leaf is a local or remote repository a read through a virtual repository resolves to, with the nested virtual
// repositories on the way, whose gates the resolver asks as a member's read.
type leaf struct {
	key    string
	nested []string
}

// leavesOf are the leaves a read through key resolves to. Through a Bastion (bastion) a member it does not distribute takes no part,
// as the resolver skips it (R-AUTHZ-043, Q-AUTHZ-341).
func leavesOf(idx *Index, bastion, key string, via []string, depth int) []leaf {
	r := idx.repo(qr(key))
	if r == nil || depth > maxReachDepth {
		return nil
	}
	if r.Class != repository.Virtual {
		return []leaf{{key: key, nested: via}}
	}
	var out []leaf
	for _, m := range r.members {
		if !idx.distributes(bastion, m) {
			continue
		}
		next := via
		if mr := idx.repo(m); mr != nil && mr.Class == repository.Virtual {
			next = append(append([]string(nil), via...), m.Key)
		}
		out = append(out, leavesOf(idx, bastion, m.Key, next, depth+1)...)
	}
	return out
}

// TestR_BAS_012_ShapeAgreesWithTheDecisionPoint: over every subject, endpoint, repository, package, version and
// kind of the fixture, a full reach never meets a refused read - the wiki of a package included - and a none
// reach never an allowed one. A read through a virtual repository is decided on its gate and on the leaf that
// holds the row, with the virtual repository as the addressed one (R-AUTHZ-009, Q-EP-11). A partial reach asserts
// nothing, so its reads are not asked, and each scan stops at the first read that refutes its reach.
func TestR_BAS_012_ShapeAgreesWithTheDecisionPoint(t *testing.T) {
	e := newTestEngine(t, reachFixture(), func(o *Options) { o.Ceiling = reachCeiling() })
	shapeAgrees(t, e, reachSubjects(), nil)
}

// shapeAgrees is the sweep of TestR_BAS_012_ShapeAgreesWithTheDecisionPoint over the subjects, each on every endpoint of the reach
// fixture; through, when set, turns a subject into the one the sweep decides (a forward through a Bastion, R-AUTHZ-043).
func shapeAgrees(t *testing.T, e *Engine, subjects map[string]Subject, through func(Subject) Subject) {
	t.Helper()
	keys := reachKeys
	pkgs := []string{"@a/x", "@acme/sdk", "x", "plain"}
	versions := []string{"", "1.0.0", "3.0.0-beta"}
	var counts [3]int
	for name, base := range subjects {
		for _, ep := range reachEndpoints {
			sub := onEP(base, ep)
			if through != nil {
				sub = through(sub)
			}
			idx, err := e.Begin(t.Context(), sub, qrs(keys)...)
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.ReachOn(t.Context(), idx, sub, qrs(keys))
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range keys {
				res, ok := got[qr(k)]
				if !ok {
					t.Fatalf("%s on %q: no answer for %s", name, ep, k)
				}
				counts[res.Reach]++
				where := name + " on " + ep + " " + k
				if res.Reach != ReachNone && res.Denial != Allow {
					t.Errorf("%s: reach %s carries the denial %d", where, res.Reach, res.Denial)
				}
				if res.Reach == ReachNone && res.Denial == Allow {
					t.Errorf("%s: reach none without a denial", where)
				}
				switch res.Reach {
				case ReachFull:
					for _, p := range pkgs {
						if why := refusedPackageRead(t, e, idx, sub, k, p, versions); why != "" {
							t.Errorf("%s: full, and the decision point refuses %s of %s", where, why, p)
							break
						}
					}
				case ReachNone:
					if what := allowedRead(t, e, idx, sub, k, pkgs, versions); what != "" {
						t.Errorf("%s: none, and the decision point allows %s", where, what)
					}
				}
			}
		}
	}
	for r, n := range counts {
		if n == 0 {
			t.Errorf("the fixture never produced the reach %s: the test checks nothing about it", Reach(r))
		}
	}
}

// allowedRead returns the first read of a package through the repository key that the decision point allows, "" when it refuses every
// one: a metadata or an artifact read of every package at every version and path of packageReads, through a virtual repository past its gate and the gates of
// the nested virtual repositories, with the populate check for an artifact of a remote repository.
func allowedRead(t *testing.T, e *Engine, idx *Index, sub Subject, key string, pkgs, versions []string) string {
	t.Helper()
	ctx := t.Context()
	addressed := ""
	if r := idx.repo(qr(key)); r != nil && r.Class == repository.Virtual {
		addressed = key
	}
	for _, kind := range []RequestKind{KindMetadata, KindArtifact} {
		if addressed != "" && e.DecideOn(ctx, idx, sub, Coordinate{Repository: qr(key), Kind: kind, Actions: []Action{Read}}).Outcome != Allow {
			continue
		}
		for _, lf := range leavesOf(idx, sub.Bastion, key, nil, 0) {
			nestedOK := true
			for _, n := range lf.nested {
				nc := Coordinate{Repository: qr(n), Kind: kind, Actions: []Action{Read}, Addressed: qr(addressed)}
				nestedOK = nestedOK && e.DecideOn(ctx, idx, sub, nc).Outcome == Allow
			}
			if !nestedOK {
				continue
			}
			remote := idx.repo(qr(lf.key)).Class == repository.Remote
			for _, p := range pkgs {
				for _, vp := range packageReads(p, versions) {
					c := Coordinate{Repository: qr(lf.key), Package: p, Version: vp.version, Path: vp.path, Kind: kind, Actions: []Action{Read}, Addressed: qr(addressed)}
					ok := e.DecideOn(ctx, idx, sub, c).Outcome == Allow
					if ok && kind == KindArtifact && remote {
						ok = e.MayPopulateOn(ctx, idx, sub, c).Outcome == Allow
					}
					if ok {
						return "a read of kind " + string(kind) + " of " + lf.key + " " + p + " version " + vp.version + " path " + vp.path
					}
				}
			}
		}
	}
	return ""
}

// TestR_BAS_012_Shapes pins the cases of the acceptance criteria and of the rules by name.
func TestR_BAS_012_Shapes(t *testing.T) {
	on := func(o *Options) { o.Ceiling = reachCeiling() }
	keys := []string{"npm-a", "npm-strict", "npmjs", "npm-frozen", "npm-v", "npm-v2", "npm-empty", "npm-pub", "npm-open", "npm-int", "npm-vpub", "npm-vmix", "ghost"}
	reach := func(t *testing.T, mod func(*Options), sub Subject) map[qname.Repo]ReachResult {
		t.Helper()
		e := newTestEngine(t, reachFixture(), mod)
		idx, err := e.Begin(t.Context(), sub, qrs(keys)...)
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.ReachOn(t.Context(), idx, sub, qrs(keys))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	want := func(t *testing.T, got map[qname.Repo]ReachResult, key string, r Reach, denial Outcome) {
		t.Helper()
		if got[qr(key)].Reach != r || (r == ReachNone && got[qr(key)].Denial != denial) {
			t.Errorf("%s: %s (denial %d), want %s (denial %d)", key, got[qr(key)].Reach, got[qr(key)].Denial, r, denial)
		}
	}
	t.Run("an unrestricted reader without the populate right of a frozen mirror is partial on the virtual repository over it", func(t *testing.T) {
		got := reach(t, on, aliceSub)
		want(t, got, "npm-a", ReachFull, Allow)
		want(t, got, "npmjs", ReachFull, Allow)
		want(t, got, "npm-v", ReachFull, Allow)
		want(t, got, "npm-v2", ReachNone, DenyHidden) // no gate on npm-v2
	})
	t.Run("the service account that may deploy into the frozen mirror is full on the nested virtual repository", func(t *testing.T) {
		got := reach(t, on, botSub)
		want(t, got, "npm-frozen", ReachFull, Allow)
		want(t, got, "npm-v2", ReachFull, Allow)
	})
	t.Run("a reader without the populate right of a frozen mirror is partial on it and on the virtual repository over it", func(t *testing.T) {
		got := reach(t, on, userSub("user-reader", "reader", "group-readers"))
		want(t, got, "npmjs", ReachFull, Allow)
		want(t, got, "npm-frozen", ReachPartial, Allow)
		want(t, got, "npm-v", ReachFull, Allow)
		want(t, got, "npm-v2", ReachPartial, Allow)
	})
	t.Run("a nested virtual repository whose gate refuses is skipped, so the outer one is partial", func(t *testing.T) {
		got := reach(t, on, userSub("user-v2", "v2", "group-v2"))
		want(t, got, "npm-v", ReachNone, DenyHidden)
		want(t, got, "npm-v2", ReachPartial, Allow)
	})
	t.Run("an exposure rule on the endpoint makes the shape partial at most", func(t *testing.T) {
		got := reach(t, on, onEP(botSub, "customers"))
		want(t, got, "npm-a", ReachPartial, Allow)
		want(t, got, "npm-v", ReachNone, DenyHidden)
		got = reach(t, on, onEP(botSub, "default"))
		want(t, got, "npm-a", ReachFull, Allow)
	})
	t.Run("an anonymous subject reads what a public level opens, and is none with 401 elsewhere", func(t *testing.T) {
		got := reach(t, on, anonSub)
		want(t, got, "npm-open", ReachFull, Allow)
		// Public artifacts and an internal wiki: the artifacts are read in full, the wiki is not, so the shape is partial and a
		// shared document lacks the wiki members (R-BAS-012, Q-BAS-149).
		want(t, got, "npm-pub", ReachPartial, Allow)
		want(t, got, "npm-vpub", ReachPartial, Allow)
		for _, k := range []string{"npm-a", "npm-int", "npm-vmix", "npm-strict", "npm-frozen", "npm-v2", "npm-empty"} {
			want(t, got, k, ReachNone, DenyUnauthenticated)
		}
	})
	t.Run("an internal user, a service account and a CI token read an internal repository, an external user does not", func(t *testing.T) {
		want(t, reach(t, on, carolSub), "npm-int", ReachFull, Allow)
		want(t, reach(t, on, Subject{Kind: principal.KindServiceAccount, ID: "sa-partner", Name: "it/partner"}), "npm-int", ReachFull, Allow)
		want(t, reach(t, on, ciSub), "npm-int", ReachFull, Allow)
		want(t, reach(t, on, external(carolSub)), "npm-int", ReachNone, DenyHidden)
		want(t, reach(t, on, external(carolSub)), "npm-open", ReachFull, Allow)
		want(t, reach(t, on, external(carolSub)), "npm-pub", ReachPartial, Allow)
		want(t, reach(t, on, carolSub), "npm-pub", ReachFull, Allow) // internal wiki: an internal user reads it
	})
	t.Run("an unknown key is none with the refusal of a missing repository", func(t *testing.T) {
		want(t, reach(t, on, botSub), "ghost", ReachNone, DenyHidden)
		want(t, reach(t, on, anonSub), "ghost", ReachNone, DenyUnauthenticated)
	})
	t.Run("a virtual repository without members reads nothing", func(t *testing.T) {
		want(t, reach(t, on, botSub), "npm-empty", ReachNone, DenyHidden)
	})
	t.Run("the metadata capability alone is never full", func(t *testing.T) {
		got := reach(t, on, userSub(idAuditorDirect, "auditor"))
		want(t, got, "npm-a", ReachPartial, Allow)
		want(t, got, "npm-v", ReachPartial, Allow)
	})
	t.Run("the session cookie never populates, so a remote is partial", func(t *testing.T) {
		want(t, reach(t, on, session(botSub)), "npmjs", ReachPartial, Allow)
	})
	t.Run("a token scope that narrows is partial", func(t *testing.T) {
		sub := tokenScop(botSub, scope.Scope{Repositories: []scope.Grant{{Repositories: []string{"it/npm-a"}, Actions: []string{"read"}}}})
		want(t, reach(t, on, sub), "npm-a", ReachPartial, Allow)
	})
}

func external(s Subject) Subject { s.External = true; return s }

// versionPath is a version and a path of a read of a package.
type versionPath struct{ version, path string }

// packageReads are the versions and paths a read of the package pkg is decided on by the agreement tests of R-BAS-012 and R-BAS-080:
// the package without a version and a path (a coordinate a path pattern does not match), its document (npm's name itself), a version
// without a path, each version at its tarball, and the package at two paths of no package of the fixture. A version matters to an
// exposure rule, a path to a path pattern; the pairs reach each rule of the fixture without the cross product of the two.
func packageReads(pkg string, versions []string) []versionPath {
	out := []versionPath{{"", ""}, {"", pkg}, {"", "x/y"}, {"", "plain/-/plain-1.0.0.tgz"}}
	for _, v := range versions {
		if v == "" {
			continue
		}
		out = append(out, versionPath{v, ""})
		if p, ok := npmVersionPath(pkg, v); ok {
			out = append(out, versionPath{v, p})
		}
	}
	return out
}

// refusedPackageRead returns the first read of the package through the repository key that the decision point refuses, "" when it
// allows every one: metadata, artifacts and the wiki at every version and path of packageReads, with the populate check for an artifact of a
// remote repository; through a virtual repository its gate, the gates of the nested virtual repositories asked as a member's read,
// and the leaf, with the virtual repository as the addressed one (R-AUTHZ-009, Q-EP-11).
func refusedPackageRead(t *testing.T, e *Engine, idx *Index, sub Subject, key, pkg string, versions []string) string {
	t.Helper()
	ctx := t.Context()
	leaves := leavesOf(idx, sub.Bastion, key, nil, 0)
	if len(leaves) == 0 {
		return "no member holds the package"
	}
	addressed := ""
	if r := idx.repo(qr(key)); r != nil && r.Class == repository.Virtual {
		addressed = key
		for _, kind := range []RequestKind{KindMetadata, KindArtifact} {
			if e.DecideOn(ctx, idx, sub, Coordinate{Repository: qr(key), Kind: kind, Actions: []Action{Read}}).Outcome != Allow {
				return "the gate refuses a read of kind " + string(kind)
			}
		}
	}
	for _, lf := range leaves {
		for _, kind := range []RequestKind{KindMetadata, KindArtifact} {
			for _, n := range lf.nested {
				nc := Coordinate{Repository: qr(n), Kind: kind, Actions: []Action{Read}, Addressed: qr(addressed)}
				if e.DecideOn(ctx, idx, sub, nc).Outcome != Allow {
					return "the gate of the nested " + n + " refuses a read of kind " + string(kind)
				}
			}
		}
		remote := idx.repo(qr(lf.key)).Class == repository.Remote
		for _, vp := range packageReads(pkg, versions) {
			c := Coordinate{Repository: qr(lf.key), Package: pkg, Version: vp.version, Path: vp.path, Actions: []Action{Read}, Addressed: qr(addressed)}
			where := lf.key + " version " + vp.version + " path " + vp.path
			for _, kind := range []RequestKind{KindMetadata, KindArtifact} {
				c.Kind = kind
				if e.DecideOn(ctx, idx, sub, c).Outcome != Allow {
					return "a read of kind " + string(kind) + " of " + where
				}
				if kind == KindArtifact && remote && e.MayPopulateOn(ctx, idx, sub, c).Outcome != Allow {
					return "the fill of " + where
				}
			}
			c.Kind = KindMetadata
			if !e.WikiReadableOn(idx, sub, c) {
				return "the wiki of " + where
			}
		}
	}
	return ""
}

// TestR_BAS_080_PackageShapeAgreesWithTheDecisionPoint: over every subject, endpoint, repository and package of the reach fixture,
// a package shape is full or partial and never none, a full shape on the repository is full on every package, and a full package
// shape never meets a refused read - metadata, artifact or wiki, of any version at any path of the package, nor the fill of a remote
// repository (R-BAS-080). A package of a repository read in full is held by the agreement test of R-BAS-012, which asks the same
// reads of every package; this test asks them of each package that is full on a repository read in part.
func TestR_BAS_080_PackageShapeAgreesWithTheDecisionPoint(t *testing.T) {
	pkgs := []string{"@a/x", "@acme/sdk", "x", "plain"}
	versions := []string{"", "1.0.0", "3.0.0-beta"}
	e := newTestEngine(t, reachFixture(), func(o *Options) { o.Ceiling = reachCeiling() })
	var full, partial, narrowedFull int
	for name, base := range reachSubjects() {
		for _, ep := range reachEndpoints {
			sub := onEP(base, ep)
			idx, err := e.Begin(t.Context(), sub, qrs(reachKeys)...)
			if err != nil {
				t.Fatal(err)
			}
			whole, err := e.ReachOn(t.Context(), idx, sub, qrs(reachKeys))
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range reachKeys {
				got, err := e.PackageReachOn(t.Context(), idx, sub, qr(k), pkgs)
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range pkgs {
					where := name + " on " + ep + " " + k + " " + p
					switch got[p] {
					case ReachFull:
						full++
						if whole[qr(k)].Reach == ReachFull {
							continue // every read through the repository: TestR_BAS_012_ShapeAgreesWithTheDecisionPoint holds it
						}
						narrowedFull++
						if why := refusedPackageRead(t, e, idx, sub, k, p, versions); why != "" {
							t.Errorf("%s: full, and the decision point refuses %s", where, why)
						}
					case ReachPartial:
						partial++
						if whole[qr(k)].Reach == ReachFull {
							t.Errorf("%s: partial on a repository the subject reads in full", where)
						}
					default:
						t.Errorf("%s: %s, want full or partial", where, got[p])
					}
				}
			}
		}
	}
	if full == 0 || partial == 0 || narrowedFull == 0 {
		t.Errorf("the fixture produced %d full, %d partial and %d full package shapes on a repository read in part: the test checks nothing about one of them",
			full, partial, narrowedFull)
	}
}

// TestR_BAS_080_PackageShapes pins the cases of the acceptance criteria by name, and the repository shapes the package shapes rest on.
func TestR_BAS_080_PackageShapes(t *testing.T) {
	e := newTestEngine(t, reachFixture(), func(o *Options) { o.Ceiling = reachCeiling() })
	subs := reachSubjects()
	want := func(t *testing.T, sub Subject, key string, w map[string]Reach) {
		t.Helper()
		var pkgs []string
		for p := range w {
			pkgs = append(pkgs, p)
		}
		idx, err := e.Begin(t.Context(), sub, qr(key))
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.PackageReachOn(t.Context(), idx, sub, qr(key), pkgs)
		if err != nil {
			t.Fatal(err)
		}
		for p, r := range w {
			if got[p] != r {
				t.Errorf("%s %q: %s, want %s", key, p, got[p], r)
			}
		}
	}
	reach := func(t *testing.T, sub Subject, key string) Reach {
		t.Helper()
		idx, err := e.Begin(t.Context(), sub, qr(key))
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.ReachOn(t.Context(), idx, sub, []qname.Repo{qr(key)})
		if err != nil {
			t.Fatal(err)
		}
		return got[qr(key)].Reach
	}
	t.Run("a grant narrowed by package on a local repository is full on its packages and partial on the others", func(t *testing.T) {
		want(t, subs["acme reader"], "npm-strict", map[string]Reach{"@acme/sdk": ReachFull, "x": ReachPartial})
		want(t, aliceSub, "npm-b", map[string]Reach{"@a/x": ReachFull, "x": ReachPartial})
	})
	t.Run("a path pattern on the repository makes every package partial", func(t *testing.T) {
		want(t, subs["acme path reader"], "npm-strict", map[string]Reach{"@acme/sdk": ReachPartial, "x": ReachPartial})
		want(t, subs["dave"], "npm-strict", map[string]Reach{"x": ReachPartial})
	})
	t.Run("a token scope that lists a package pattern is full on its packages", func(t *testing.T) {
		want(t, subs["bot scoped to @acme/*"], "npm-a", map[string]Reach{"@acme/sdk": ReachFull, "x": ReachPartial})
	})
	t.Run("an endpoint with exposure rules is partial", func(t *testing.T) {
		want(t, onEP(botSub, "customers"), "npm-a", map[string]Reach{"@acme/sdk": ReachPartial})
		want(t, onEP(subs["bot scoped to @acme/*"], "default"), "npm-a", map[string]Reach{"@acme/sdk": ReachFull})
	})
	t.Run("a remote repository is full on a package only with the populate right for it", func(t *testing.T) {
		want(t, subs["reader"], "npm-frozen", map[string]Reach{"@acme/sdk": ReachPartial})
		want(t, botSub, "npm-frozen", map[string]Reach{"@acme/sdk": ReachFull, "x": ReachFull})
		want(t, subs["filler"], "npm-frozen", map[string]Reach{"@acme/sdk": ReachFull, "x": ReachPartial})
		want(t, session(botSub), "npmjs", map[string]Reach{"@acme/sdk": ReachPartial})
		// A fill narrowed by package is no fill of the whole repository (R-BAS-012).
		if got := reach(t, subs["filler"], "npm-frozen"); got != ReachPartial {
			t.Errorf("npm-frozen for the filler: %s, want partial", got)
		}
	})
	t.Run("a virtual repository is full on a package only when every member is", func(t *testing.T) {
		want(t, aliceSub, "npm-vb", map[string]Reach{"@a/x": ReachFull, "x": ReachPartial})
		want(t, subs["v2 reader"], "npm-v2", map[string]Reach{"@a/x": ReachPartial})
		// The gate of the nested npm-v refuses the v3 reader, who reads every leaf below it.
		want(t, subs["v3 reader"], "npm-v3", map[string]Reach{"@a/x": ReachPartial})
		want(t, subs["v3 reader"], "npm-b", map[string]Reach{"@a/x": ReachFull})
		want(t, subs["reader"], "npm-v2", map[string]Reach{"@a/x": ReachPartial})
		want(t, subs["filler"], "npm-v2", map[string]Reach{"@acme/sdk": ReachFull, "x": ReachPartial})
		// The metadata gate opens through content:metadata-read, the artifact gate does not.
		want(t, subs["auditor with grants"], "npm-v", map[string]Reach{"@a/x": ReachPartial})
		want(t, subs["auditor with grants"], "npm-a", map[string]Reach{"@a/x": ReachFull})
	})
	t.Run("a reader without the wiki of a package is partial on it", func(t *testing.T) {
		want(t, external(carolSub), "npm-pub", map[string]Reach{"@a/x": ReachPartial})
		want(t, anonSub, "npm-pub", map[string]Reach{"@a/x": ReachPartial})
		want(t, subs["ext"], "npm-pub", map[string]Reach{"@a/x": ReachFull, "x": ReachPartial})
		want(t, subs["carol scoped to @a/*"], "npm-pub", map[string]Reach{"@a/x": ReachFull, "x": ReachPartial})
		// A wiki read narrowed by package is no wiki read of the whole repository (R-BAS-012, Q-BAS-149).
		for _, name := range []string{"ext", "carol scoped to @a/*"} {
			if got := reach(t, subs[name], "npm-pub"); got != ReachPartial {
				t.Errorf("npm-pub for %s: %s, want partial", name, got)
			}
		}
	})
	t.Run("content:metadata-read alone is partial", func(t *testing.T) {
		want(t, subs["auditor"], "npm-a", map[string]Reach{"@a/x": ReachPartial})
		want(t, subs["auditor with grants"], "npm-strict", map[string]Reach{"x": ReachPartial})
	})
	t.Run("a full shape on the repository is full on every package, a none shape partial", func(t *testing.T) {
		want(t, botSub, "npm-a", map[string]Reach{"@a/x": ReachFull, "x": ReachFull, "": ReachFull})
		want(t, carolSub, "npm-a", map[string]Reach{"x": ReachPartial})
		want(t, botSub, "ghost", map[string]Reach{"x": ReachPartial})
		want(t, aliceSub, "npm-b", map[string]Reach{"": ReachPartial})
	})
}
