package formathost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	vt "github.com/guided-traffic/quader/internal/virtual/virtualtest"
)

// tiersFake answers servingMember's questions per member key.
type tiersFake struct {
	plan     *formats.VirtualPlan
	local    map[string]bool
	cached   map[string]bool
	gate     map[string]error
	announce map[string]bool
	gated    []string
}

func (f *tiersFake) planFor(context.Context, *formats.Request, formats.Path) (*formats.VirtualPlan, error) {
	return f.plan, nil
}

func (f *tiersFake) heldLocally(_ context.Context, m formats.RepoRef, _ formats.Path) (bool, error) {
	return f.local[m.Name.Key], nil
}

func (f *tiersFake) cachedAt(_ context.Context, mrq *formats.Request, _ formats.Path) (bool, error) {
	return f.cached[mrq.Repo.Name.Key], nil
}

func (f *tiersFake) gateAt(_ context.Context, mrq *formats.Request, _ formats.Path) (formats.Digests, error) {
	k := mrq.Repo.Name.Key
	f.gated = append(f.gated, k)
	if err := f.gate[k]; err != nil {
		return formats.Digests{}, err
	}
	if f.announce[k] {
		return formats.Digests{SHA512: []byte{1}}, nil
	}
	return formats.Digests{}, nil
}

// TestQ_REPO_367_AProbeThroughAVirtualRepositoryIsTheProbeOfTheMemberThatServesIt: the member is found by the tiers of R-REPO-019
// step 6 for the probe's file, without an upstream: a local member that holds it starts nothing; the first remote member whose cache
// holds it is the one; then the first remote member the caller may fill whose gate announces the file, else the first whose gate does
// not refuse it; a member the caller may not fill, and one whose gate says the version is not there, are passed over.
func TestQ_REPO_367_AProbeThroughAVirtualRepositoryIsTheProbeOfTheMemberThatServesIt(t *testing.T) {
	v := vt.VirtualRepo(9, "all", 0, nil, vt.LocalRepo(1, "team"), vt.RemoteRepo(2, "vendor"), vt.RemoteRepo(3, "public"), vt.RemoteRepo(4, "mirror"))
	rq := &formats.Request{Request: httptest.NewRequest(http.MethodHead, "/x", nil), Repo: &v}
	member := func(id int64, key string, tier formats.Tier, fill bool) formats.PlannedMember {
		return formats.PlannedMember{Member: formats.RepoRef{ID: id, Name: qr(key)}, Tier: tier, MayFill: fill}
	}
	plan := &formats.VirtualPlan{Admitted: true, Members: []formats.PlannedMember{
		member(1, "team", formats.TierLocal, false),
		member(2, "vendor", formats.TierRemote, false),
		member(3, "public", formats.TierRemote, true),
		member(4, "mirror", formats.TierRemote, true),
	}}
	probe := formats.Probe{Name: "left-pad", Version: "1.0.0", Files: []formats.Path{{Path: "left-pad/-/left-pad-1.0.0.tgz"}}}
	missing := errs.E(errs.NotFound, errs.CodeNotFound, "no")
	for _, tc := range []struct {
		name string
		f    tiersFake
		want string // the member's key, or the outcome
	}{
		{"a local member holds it", tiersFake{local: map[string]bool{"team": true}, cached: map[string]bool{"public": true}}, ProbeLocal},
		{"a remote member's cache holds it, also one the caller may not fill", tiersFake{cached: map[string]bool{"vendor": true}}, "vendor"},
		{"the first fillable member whose gate announces it", tiersFake{announce: map[string]bool{"mirror": true}}, "mirror"},
		{"the first fillable member when no gate can tell", tiersFake{}, "public"},
		{"a member whose gate says the version is not there is passed over", tiersFake{gate: map[string]error{"public": missing}}, "mirror"},
		{"no member serves it", tiersFake{gate: map[string]error{"public": missing, "mirror": missing}}, ProbeNotAdmitted},
		{"the virtual repository's patterns refuse it", tiersFake{plan: &formats.VirtualPlan{}}, ProbeNotAdmitted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.f
			if f.plan == nil {
				f.plan = plan
			}
			mrq, out, err := servingMember(t.Context(), &f, rq, probe)
			if err != nil {
				t.Fatal(err)
			}
			got := out
			if mrq != nil {
				got = mrq.Repo.Name.Key
				if mrq.Addressed() != v.Name || mrq.Repo.Class != "remote" {
					t.Errorf("the member's request is addressed to %s, class %s; want the virtual repository's address", mrq.Addressed(), mrq.Repo.Class)
				}
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			for _, k := range f.gated {
				if k == "vendor" {
					t.Error("the gate was asked for a member the caller may not fill")
				}
			}
		})
	}
}
