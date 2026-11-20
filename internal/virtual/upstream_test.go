package virtual_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/virtual"
	vt "github.com/guided-traffic/quader/internal/virtual/virtualtest"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// A member of the built-in project upstream in a team's virtual repository (R-REPO-071, R-REPO-072, R-AUTHZ-041): the engine
// over the fakes and the real decision point. npm-all (project it, standing for the team's project web) lists npm-local and
// upstream/npmjs (ID 6, a preset repository of the project upstream).

const idUpstream = 6

// The subjects of the worked rows of R-AUTHZ-041: the CI token of the mapping web-build, Erin's personal token (a user flagged
// external with read on npm-all and npm-local) and the project service account's token. A request through a Bastion is decided
// on the same subject: it carries no mark of the channel (Q-AUTHZ-321), so row 5 is the service account's row.
var (
	ciToken = authz.Subject{Kind: principal.KindCI, Name: "ci", Mappings: []string{"ci:gitlab/web-build"}, TokenID: "cit-1", AuthMethod: "bearer"}
	erin    = func() authz.Subject { s := subjectFor("user-erin"); s.External = true; return s }()
	saToken = authz.Subject{Kind: principal.KindServiceAccount, ID: "sa-deployer", Name: "it/deployer", TokenID: "sat-1", AuthMethod: "bearer"}
	// scopedCI is the CI token of a job that requested the scope read on npm-all by its exact name (R-AUTHZ-013, Q-AUTHZ-163).
	scopedCI = func() authz.Subject {
		s := ciToken
		s.Scope = &scope.Scope{Repositories: []scope.Grant{{Repositories: []string{"it/npm-all"}, Actions: []string{"read"}}}}
		return s
	}()
)

// upstreamWorld is the world of the rows: npm-all over npm-local and upstream/npmjs in a state and at a level.
func upstreamWorld(t *testing.T, state wellknown.State, level string, more ...func(*worldOpts)) (*world, repository.Repository) {
	t.Helper()
	npmjs := vt.UpstreamRepo(idUpstream, "npmjs", state)
	w := newWorld(t, append([]func(*worldOpts){func(o *worldOpts) {
		o.members = []repository.Repository{local, npmjs}
		o.repos = append(specs("read"), vt.RepoSpec{Key: "npmjs", Class: "remote", Project: wellknown.ProjectKey, Level: level})
		o.repos[4].Members = []string{"npm-local", "npmjs"} // npm-all lists upstream/npmjs, as the grant index names it
		o.grants = append(grants(), vt.Grant{Repos: []string{"npm-all", "npm-local"}, Principal: "user-erin", Actions: []string{"read"}})
	}}, more...)...)
	w.remote.Register(npmjs)
	vt.Put(w.arts.UpstreamItems, "npmjs", "lodash/-/4.17.21.tgz", "lodash-bytes")
	vt.Put(w.arts.LocalItems, "npm-local", "@web/ui/-/1.0.0.tgz", "ui-bytes")
	return w, npmjs
}

// detail is the message of a resolution's error, "" for none.
func detail(err error) string {
	if e, ok := errs.As(err); ok {
		return e.Detail
	}
	return ""
}

const notSwitchedOn = "upstream/npmjs is a member of this virtual repository, but an administrator of Quader has not switched it on."

// TestR_AUTHZ_041_ATeamsVirtualRepositoryReadsAMemberOfUpstreamByItsLevel: the seven rows of R-AUTHZ-041 through the engine. Who
// reads the member is decided on its level alone (rows 1, 2, 5): nothing of the virtual repository's grants reaches it; a user flagged
// external skips an internal one silently (rows 3, 4) and is not told about it as a placeholder (row 7).
func TestR_AUTHZ_041_ATeamsVirtualRepositoryReadsAMemberOfUpstreamByItsLevel(t *testing.T) {
	for _, c := range []struct {
		row          string
		sub          authz.Subject
		state        wellknown.State
		level        string
		pkg, version string
		body, from   string
		sentence     string
	}{
		{"1 a CI token reads an internal member by its level", ciToken, wellknown.StateActive, "internal", "lodash", "4.17.21", "lodash-bytes", "upstream/npmjs", ""},
		{"2 a CI token skips a private member", ciToken, wellknown.StateActive, "private", "lodash", "4.17.21", "", "", ""},
		{"3 an external user skips an internal member, without a sentence", erin, wellknown.StateActive, "internal", "lodash", "4.17.21", "", "", ""},
		{"4 an external user reads the local member", erin, wellknown.StateActive, "internal", "@web/ui", "1.0.0", "ui-bytes", "npm-local", ""},
		{"5 a service account's token, also through a Bastion, reads an internal member", saToken, wellknown.StateActive, "internal", "lodash", "4.17.21", "lodash-bytes", "upstream/npmjs", ""},
		{"6 a CI token meets a placeholder: 404 with the sentence", ciToken, wellknown.StateNotSwitchedOn, "internal", "lodash", "4.17.21", "", "", notSwitchedOn},
		{"7 an external user meets a placeholder: 404 without the sentence", erin, wellknown.StateNotSwitchedOn, "internal", "lodash", "4.17.21", "", "", ""},
		{"8 a CI token scoped to npm-all reads an internal member through it", scopedCI, wellknown.StateActive, "internal", "lodash", "4.17.21", "lodash-bytes", "upstream/npmjs", ""},
		{"8 a CI token scoped to npm-all meets a placeholder: 404 with the sentence", scopedCI, wellknown.StateNotSwitchedOn, "internal", "lodash", "4.17.21", "", "", notSwitchedOn},
	} {
		t.Run("R-AUTHZ-041 row "+c.row, func(t *testing.T) {
			w, _ := upstreamWorld(t, c.state, c.level)
			cl, p := w.plan(c.sub, artifactOf(c.pkg, c.version))
			rec := httptest.NewRecorder()
			err := w.r.Artifact(t.Context(), cl, p, w.arts, rec)
			if c.body != "" {
				if err != nil || rec.Body.String() != c.body || rec.Header().Get(virtual.ResolvedFromHeader) != c.from {
					t.Fatalf("body %q from %q err %v", rec.Body.String(), rec.Header().Get(virtual.ResolvedFromHeader), err)
				}
				return
			}
			if k, code := kind(err); k != errs.NotFound || code != errs.CodeNotFound {
				t.Fatalf("err = %v, want the 404 of a missing resource", err)
			}
			if c.sentence == "" && detail(err) != authz.NotFound().Detail {
				t.Fatalf("detail %q: a caller who would not read the member is told about it", detail(err))
			}
			if c.sentence != "" && detail(err) != c.sentence {
				t.Fatalf("detail %q, want %q", detail(err), c.sentence)
			}
			for _, call := range w.arts.Calls() {
				if strings.HasSuffix(call, ":npmjs") && c.state != wellknown.StateActive {
					t.Fatalf("the placeholder was consulted: %v", w.arts.Calls())
				}
			}
		})
	}
}

// TestR_REPO_072_A404NamesThePlaceholderOnlyWhereItWouldHaveBeenAsked: the sentence of R-REPO-072 appears where the placeholder could have
// changed the answer, for each state, in a merge and in the artifact tiers, and nowhere else.
func TestR_REPO_072_A404NamesThePlaceholderOnlyWhereItWouldHaveBeenAsked(t *testing.T) {
	head := "upstream/npmjs is a member of this virtual repository, but "
	for state, want := range map[wellknown.State]string{
		wellknown.StateNotSwitchedOn: notSwitchedOn,
		wellknown.StateSwitchedOff:   head + "an administrator of Quader switched it off.",
		wellknown.StateRetired:       head + "Quader no longer offers it; remove it from the members.",
	} {
		t.Run("R-REPO-072 the metadata of a name only the placeholder would have had: "+string(state), func(t *testing.T) {
			w, _ := upstreamWorld(t, state, "internal")
			_, err := w.metadata(ciToken, "left-pad")
			if k, _ := kind(err); k != errs.NotFound || detail(err) != want {
				t.Fatalf("err %v, want 404 %q", err, want)
			}
			if w.remote.Count("metadata:npmjs") != 0 {
				t.Fatalf("the placeholder was asked: %v", w.remote.Calls())
			}
		})
	}
	t.Run("R-REPO-072 a name the placeholder's patterns keep away gets no sentence", func(t *testing.T) {
		w, _ := upstreamWorld(t, wellknown.StateNotSwitchedOn, "internal")
		w.router.Dropped = map[string]string{"npmjs": repository.DropNotAdmitted}
		if _, err := w.metadata(ciToken, "left-pad"); detail(err) != authz.NotFound().Detail {
			t.Fatalf("detail %q", detail(err))
		}
	})
	t.Run("R-REPO-072 a name a local member holds is served", func(t *testing.T) {
		w, _ := upstreamWorld(t, wellknown.StateNotSwitchedOn, "internal")
		vt.Put(w.arts.LocalItems, "npm-local", tgz, "local-bytes")
		if rec, err := w.artifact(alice, "left-pad", "1.0.0"); err != nil || rec.Body.String() != "local-bytes" {
			t.Fatalf("alice: %q %v", rec.Body.String(), err)
		}
	})
	t.Run("R-REPO-072 several placeholders: the first in list order and the number of the others", func(t *testing.T) {
		quay := vt.UpstreamRepo(7, "quay", wellknown.StateSwitchedOff)
		w, npmjs := upstreamWorld(t, wellknown.StateNotSwitchedOn, "internal", func(o *worldOpts) {
			o.repos = append(o.repos, vt.RepoSpec{Key: "quay", Class: "remote", Project: wellknown.ProjectKey, Level: "internal"})
		})
		w.v = vt.VirtualRepo(5, "npm-all", 7200, nil, local, npmjs, quay)
		_, err := w.metadata(ciToken, "left-pad")
		if want := notSwitchedOn + " One more member of the project upstream is not in use either."; detail(err) != want {
			t.Fatalf("detail %q, want %q", detail(err), want)
		}
	})
	t.Run("R-REPO-072 R-EP-018 an endpoint with exposure rules names no placeholder", func(t *testing.T) {
		w, _ := upstreamWorld(t, wellknown.StateNotSwitchedOn, "internal")
		c := w.caller(ciToken)
		c.Rules = true
		p, err := w.r.Plan(t.Context(), c, w.v, artifactOf("lodash", "4.17.21"))
		if err != nil {
			t.Fatal(err)
		}
		err = w.r.Artifact(t.Context(), c, p, w.arts, httptest.NewRecorder())
		if detail(err) != authz.NotFound().Detail || p.NotFoundMessage() != "" {
			t.Fatalf("detail %q, plan message %q", detail(err), p.NotFoundMessage())
		}
	})
}

// TestR_REPO_072_TheExplanationShowsAPlaceholdersState: the member of upstream is named as upstream/<key>, a placeholder is disabled with
// its state, and a member of upstream is never counted among the unreadable members (R-REPO-019).
func TestR_REPO_072_TheExplanationShowsAPlaceholdersState(t *testing.T) {
	w, _ := upstreamWorld(t, wellknown.StateNotSwitchedOn, "internal")
	ex, err := w.r.Explain(t.Context(), w.caller(ciToken), w.v, metadataOf("left-pad"))
	if err != nil {
		t.Fatal(err)
	}
	var line *virtual.MemberDecision
	for i := range ex.Members {
		if ex.Members[i].Key == "upstream/npmjs" {
			line = &ex.Members[i]
		}
	}
	if line == nil || line.Decision != virtual.DecisionDisabled || line.UpstreamState != string(wellknown.StateNotSwitchedOn) {
		t.Fatalf("members = %+v", ex.Members)
	}
	// The CI token reads no local member: npm-local counts as unreadable, upstream/npmjs, which it reads, is named.
	if ex.UnreadableMembers != 1 {
		t.Fatalf("unreadable = %d", ex.UnreadableMembers)
	}
	t.Run("R-REPO-019 a member of upstream the caller cannot read is named as no-read and not counted", func(t *testing.T) {
		w, _ := upstreamWorld(t, wellknown.StateActive, "private")
		ex, err := w.r.Explain(t.Context(), w.caller(erin), w.v, metadataOf("left-pad"))
		if err != nil {
			t.Fatal(err)
		}
		if ex.UnreadableMembers != 0 || len(ex.Members) != 2 || ex.Members[1].Key != "upstream/npmjs" || ex.Members[1].Decision != virtual.DecisionNoRead || ex.Members[1].UpstreamState != "" {
			t.Fatalf("explanation = %+v", ex)
		}
	})
}
