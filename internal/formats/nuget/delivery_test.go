package nuget

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/nuget/merge"
	"github.com/guided-traffic/quader/internal/formats/nuget/nugettest"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// The delivery rules of a NuGet remote in its documents (R-NUGET-035, remote-curation.md R-CUR-003, R-CUR-004). Kit check 23 holds the
// version list and the registration index of every hive; these tests hold what the kit does not: the leaf, the search, the merge of a
// virtual repository, the embedded walk and the client's shape of the refusal.

func blockVersion(id, v string) rules.Rule {
	return rules.Rule{Kind: rc.KindVersion, Name: id, Version: v, Effect: rc.Block}
}

func blockPackage(id string) rules.Rule {
	return rules.Rule{Kind: rc.KindPackage, Name: id, Effect: rc.Block}
}

// withRules gives a remote repository delivery rules, compiled with NuGet's name rule and version canon as the snapshot compiles them.
func withRules(t *testing.T, r repository.Repository, mode rc.Mode, list ...rules.Rule) repository.Repository {
	t.Helper()
	d := New().Descriptor()
	out, err := repository.WithRules(r, mode, list, rules.Canon{Name: d.Names.Normalize, Version: d.VersionCanon.Canonical})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func deliveryHost(t *testing.T, mode rc.Mode, list ...rules.Rule) *formattest.Host {
	t.Helper()
	docs := registrationUpstream([]string{"1.0.0", "1.1.0", "2.0.0"}, 64, true)
	docs[PackageDocumentPath("acme.widgets")] = []byte(`{"versions":["1.0.0","1.1.0","2.0.0"]}`)
	o := formattest.HostOptions{Class: formats.Remote, RepoKey: "nuget-org", Remote: &formattest.MemRemote{Docs: docs}, Data: nugettest.New()}
	if list != nil || mode == rc.OnlyAllowed {
		o.Delivery = &formattest.DeliveryRules{Mode: mode, Rules: list}
	}
	return formattest.NewHost(New(), o)
}

// wantRefusal holds the refusal of R-CUR-004 in NuGet's shape: 403, Cache-Control: no-store, one line "not_allowed_in_repository:
// <text>" and the text in X-NuGet-Warning, which dotnet prints.
func wantRefusal(t *testing.T, r *formattest.Response, repo, what string) {
	t.Helper()
	body := r.Body.String()
	warning := r.Header().Get(headerWarning)
	if r.Code != http.StatusForbidden || !strings.Contains(r.Header().Get("Cache-Control"), "no-store") ||
		!strings.HasPrefix(body, errs.CodeNotAllowedInRepository+": ") || strings.Count(body, "\n") > 1 ||
		!strings.Contains(warning, "is not allowed in "+repo) || !strings.Contains(body, warning) {
		t.Fatalf("%s: %d %q %q %q", what, r.Code, r.Header().Get("Cache-Control"), warning, body)
	}
}

// TestR_NUGET_035_ARemotesDocumentsListTheDeliveredVersions: a rule that refuses 2.0.0 leaves it out of the version list and the
// registration, whose count is over the delivered versions; the leaf of 2.0.0 answers the refusal, that of 1.1.0 is served; the rules
// join the validator, so the same remote without rules answers another one. Unit, milliseconds, bytes.
func TestR_NUGET_035_ARemotesDocumentsListTheDeliveredVersions(t *testing.T) {
	h := deliveryHost(t, rc.AllExceptBlocked, blockVersion("acme.widgets", "2.0.0"))
	flat := rget(t, h, "v3/flatcontainer/acme.widgets/index.json")
	if flat.Code != 200 || flat.Body.String() != `{"versions":["1.0.0","1.1.0"]}` {
		t.Fatalf("version list: %d %s", flat.Code, flat.Body)
	}
	reg := rget(t, h, "v3/registration-semver2/acme.widgets/index.json")
	if reg.Code != 200 || strings.Contains(reg.Body.String(), "2.0.0") || !strings.Contains(reg.Body.String(), `"upper":"1.1.0"`) {
		t.Fatalf("registration: %d %s", reg.Code, reg.Body)
	}
	wantRefusal(t, rget(t, h, "v3/registration-semver2/acme.widgets/2.0.0.json"), "it/nuget-org", "the leaf of 2.0.0")
	if r := rget(t, h, "v3/registration-semver2/acme.widgets/1.1.0.json"); r.Code != 200 {
		t.Fatalf("the leaf of 1.1.0: %d %s", r.Code, r.Body)
	}
	plain := rget(t, deliveryHost(t, rc.AllExceptBlocked), "v3/flatcontainer/acme.widgets/index.json")
	if plain.Code != 200 || plain.Header().Get("ETag") == flat.Header().Get("ETag") {
		t.Fatalf("the list without rules has the validator of the filtered one: %d %s", plain.Code, plain.Header().Get("ETag"))
	}
}

// TestR_NUGET_035_AnIDWithoutADeliveredVersionAnswersTheRefusal: an ID the rules refuse whole, and one whose only allowing rule names
// a version the upstream does not hold, answer the refusal at the version list and the registration index, not a 404 (R-CUR-004).
// Unit, milliseconds, bytes.
func TestR_NUGET_035_AnIDWithoutADeliveredVersionAnswersTheRefusal(t *testing.T) {
	for name, h := range map[string]*formattest.Host{
		"blocked whole": deliveryHost(t, rc.AllExceptBlocked, blockPackage("acme.widgets")),
		"allowed only in a version the upstream lacks": deliveryHost(t, rc.OnlyAllowed,
			rules.Rule{Kind: rc.KindVersion, Name: "acme.widgets", Version: "9.9.9", Effect: rc.Allow}),
	} {
		t.Run(name, func(t *testing.T) {
			wantRefusal(t, rget(t, h, "v3/flatcontainer/acme.widgets/index.json"), "it/nuget-org", "the version list")
			wantRefusal(t, rget(t, h, "v3/registration/acme.widgets/index.json"), "it/nuget-org", "the registration index")
		})
	}
}

// TestR_NUGET_035_TheSearchOfARemoteDropsWhatTheRulesRefuse: a forwarded result whose ID the rules refuse is removed and totalHits
// reduced by it, and a refused version leaves its result, the latest's metadata with it; the cache-only search counts and lists the
// delivered packages and versions alone. Unit, milliseconds, bytes.
func TestR_NUGET_035_TheSearchOfARemoteDropsWhatTheRulesRefuse(t *testing.T) {
	fw := newForwarding(t, authz.Subject{}, pkg("Acme.Cached", "1.0.0", "2.0.0"), pkg("Acme.Blocked", "1.0.0"))
	*fw.h.Repo() = withRules(t, *fw.h.Repo(), rc.AllExceptBlocked,
		blockPackage("contoso.secret"), blockVersion("newtonsoft.json", "13.0.3"), blockPackage("acme.blocked"), blockVersion("acme.cached", "2.0.0"))

	d := forwardedOf(t, doSearch(t, fw.h, "q=json"))
	if !slices.Equal(namesOf(d), []string{"Newtonsoft.Json", "Semver.Two"}) || d.TotalHits != 6 {
		t.Fatalf("forwarded: results %v, totalHits %d", namesOf(d), d.TotalHits)
	}
	if n := d.Data[0]; n.Version != "13.0.1" || len(n.Versions) != 1 || n.Deprecation != nil {
		t.Fatalf("the result of newtonsoft.json: %+v", n)
	}

	fw.err = formats.ErrOffline
	d = forwardedOf(t, doSearch(t, fw.h, "q=acme"))
	if !slices.Equal(namesOf(d), []string{"Acme.Cached"}) || d.TotalHits != 1 || len(d.Data[0].Versions) != 1 || d.Data[0].Version != "1.0.0" {
		t.Fatalf("cache-only: %+v", d)
	}
}

// TestR_NUGET_035_AVirtualMergeTakesWhatEachMemberDelivers: each remote member's part lists what its rules deliver, so a version one
// member refuses comes from another that delivers it, is absent when none does, and a merge the rules left empty answers the first such
// member's refusal, which names it (R-CUR-003 point 3, Q-CUR-8). Unit, milliseconds, bytes.
func TestR_NUGET_035_AVirtualMergeTakesWhatEachMemberDelivers(t *testing.T) {
	remote := func(id, key string) repository.Repository {
		return repository.Repository{ID: id, Name: qr(key), Class: repository.Remote, Format: string(ID), Config: []byte(`{}`)}
	}
	first := withRules(t, remote("3", "first"), rc.AllExceptBlocked, blockVersion("acme.widgets", "2.0.0"))
	second := remote("4", "second")
	part := func(key string, pos int, versions ...string) formats.MemberDocument {
		return formats.MemberDocument{
			Member: formats.RepoRef{Name: qr(key)}, Tier: formats.TierRemote, Position: pos,
			Doc: []byte(`{"versions":["` + strings.Join(versions, `","`) + `"]}`),
		}
	}
	merged := func(members []repository.Repository, docs ...formats.MemberDocument) ([]merge.Entry, error) {
		h := formattest.NewHost(New(), formattest.HostOptions{Class: formats.Virtual, RepoKey: "all", Data: nugettest.New()})
		*h.Repo() = repository.Assemble(*h.Repo(), members, nil, 0)
		p := formats.Path{Path: PackageDocumentPath("acme.widgets")}
		rq, err := h.NewRequest(context.Background(), http.MethodGet, "v3/flatcontainer/acme.widgets/index.json", nil)
		if err != nil {
			t.Fatal(err)
		}
		out, err := New().Virtual().Merge(context.Background(), rq, p, docs)
		if err != nil {
			return nil, err
		}
		return merge.Decode(out)
	}
	sources := func(es []merge.Entry) map[string]string {
		out := map[string]string{}
		for _, e := range es {
			out[e.Version] = e.Source
		}
		return out
	}

	es, err := merged([]repository.Repository{first, second}, part("first", 0, "1.0.0", "2.0.0"), part("second", 1, "2.0.0"))
	if s := sources(es); err != nil || s["1.0.0"] != "it/first" || s["2.0.0"] != "it/second" {
		t.Fatalf("two members: %v %v", s, err)
	}
	es, err = merged([]repository.Repository{first}, part("first", 0, "1.0.0", "2.0.0"))
	if s := sources(es); err != nil || len(s) != 1 || s["1.0.0"] == "" {
		t.Fatalf("one member: %v %v", s, err)
	}
	_, err = merged([]repository.Repository{first}, part("first", 0, "2.0.0"))
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeNotAllowedInRepository || !strings.Contains(e.Detail, "it/first") {
		t.Fatalf("a merge the rules emptied: %v", err)
	}
}

// TestR_NUGET_035_TheEmbeddedWalkAnswersTheFirstMembersRefusal: a remote member whose rules refuse the version is a miss of the walk;
// a later member that holds the package serves the file, and with none the answer is the refusal, which names the member, not a 404
// (R-CUR-004, Q-CUR-8). Unit, milliseconds, bytes.
func TestR_NUGET_035_TheEmbeddedWalkAnswersTheFirstMembersRefusal(t *testing.T) {
	refusing := withRules(t, repository.Repository{ID: "5", Name: qr("refusing"), Class: repository.Remote, Format: string(ID), Config: []byte(`{}`)},
		rc.AllExceptBlocked, blockVersion("acme.widgets", "1.2.3"))
	holder := repository.Repository{ID: "6", Name: qr("holder"), Class: repository.Local, Format: string(ID)}
	for _, withHolder := range []bool{true, false} {
		members := []repository.Repository{refusing}
		if withHolder {
			members = append(members, holder)
		}
		plan := &formats.VirtualPlan{Admitted: true}
		for i, m := range members {
			tier := formats.TierLocal
			if m.Class == repository.Remote {
				tier = formats.TierRemote
			}
			plan.Members = append(plan.Members, formats.PlannedMember{Member: formats.RepoRef{Name: m.Name}, Tier: tier, Position: i})
		}
		st := nugettest.New()
		f := New()
		h := formattest.NewHost(f, formattest.HostOptions{
			Class: formats.Virtual, RepoKey: "all", Data: st, Resolver: &mergeResolver{f: f, plan: plan, scope: allScope()},
		})
		*h.Repo() = repository.Assemble(*h.Repo(), members, nil, 0)
		seedCached(t, h, st, 5, embeddedOpts()...)
		if withHolder {
			seedCached(t, h, st, 6, embeddedOpts()...)
		}
		r := get(t, h, http.MethodGet, leafOfWidgets+"readme")
		switch {
		case withHolder && (r.Code != http.StatusOK || !bytes.Equal(r.Body.Bytes(), readmeText)):
			t.Errorf("a later member that holds it: %d %q", r.Code, r.Body.String())
		case !withHolder:
			wantRefusal(t, r, "it/refusing", "the refusing member alone")
		}
	}
}
