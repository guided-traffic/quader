package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
)

// reportedURLs is a BastionBaseURLs over a map from a registration to what it reported.
type reportedURLs map[string]string

func (r reportedURLs) ReportedBaseURL(_ context.Context, name string) (string, bool, error) {
	v, ok := r[name]
	return v, ok, nil
}

// Distributes: every registration of the map distributes every repository, but the one named undistributing.
func (r reportedURLs) Distributes(_ context.Context, name string, _ int64) (bool, error) {
	_, ok := r[name]
	return ok && name != undistributing, nil
}

// undistributing is a registration of a test that distributes no repository.
const undistributing = "elsewhere"

type failingURLs struct{}

func (failingURLs) ReportedBaseURL(context.Context, string) (string, bool, error) {
	return "", false, errors.New("database down")
}

func (failingURLs) Distributes(context.Context, string, int64) (bool, error) {
	return false, errors.New("database down")
}

func bastionServer(t *testing.T, reported BastionBaseURLs, additional ...string) http.Handler {
	t.Helper()
	return setupServerWith(t, "https://quader.example.com", true, authed, func(ps *PackageServices) {
		ps.Bastions = reported
		ps.AdditionalBaseURLs = additional
	})
}

func setupFrom(t *testing.T, h http.Handler, path string) gen.ClientSetup {
	t.Helper()
	w := get(h, path)
	var out gen.ClientSetup
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
	return out
}

func setupProblem(t *testing.T, body []byte) errs.Problem {
	t.Helper()
	var p errs.Problem
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestR_BAS_007_ClientSetupRendersOnTheBaseURLTheBastionReported: the acceptance case of
// R-BAS-007: the registry URL is the Bastion's, npm's configure snippet sets
// replace-registry-host=always, and there is no publish snippet and no publish target.
func TestR_BAS_007_ClientSetupRendersOnTheBaseURLTheBastionReported(t *testing.T) {
	h := bastionServer(t, reportedURLs{"partners": "https://packages.example.com"}, "https://packages.example.com")
	out := setupFrom(t, h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion=partners")
	if out.RegistryUrl != "https://packages.example.com/api/npm/it/npm-all/" || out.EndpointPath != "//packages.example.com/api/npm/it/npm-all/" {
		t.Fatalf("%+v", out)
	}
	if out.PublishTarget != nil {
		t.Errorf("publish target %q", *out.PublishTarget)
	}
	var conf string
	for _, sn := range out.Snippets {
		if sn.Tab == gen.ClientSnippetTabPublish {
			t.Errorf("a publish snippet through a Bastion: %s", sn.Id)
		}
		if sn.Tab == gen.ClientSnippetTabConfigure {
			conf = sn.Content
		}
	}
	for _, want := range []string{
		"registry=https://packages.example.com/api/npm/it/npm-all/\n",
		"//packages.example.com/api/npm/it/npm-all/:_authToken={{token}}\n",
		"replace-registry-host=always\n",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("configure snippet lacks %q:\n%s", want, conf)
		}
	}
	if strings.Contains(conf, "quader.example.com") {
		t.Errorf("the main installation's address is in a Bastion's snippet:\n%s", conf)
	}
}

// TestR_BAS_007_TheSwitchIsNpmsAndTheBastionsOnly: without bastion= the snippet is unchanged and
// still has its publish tab, and the clients that do not read replace-registry-host do not get it.
func TestR_BAS_007_TheSwitchIsNpmsAndTheBastionsOnly(t *testing.T) {
	h := bastionServer(t, reportedURLs{"partners": "https://packages.example.com"}, "https://packages.example.com")
	direct := setupFrom(t, h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm")
	hasPublish := false
	for _, sn := range direct.Snippets {
		hasPublish = hasPublish || sn.Tab == gen.ClientSnippetTabPublish
		if strings.Contains(sn.Content, "replace-registry-host") {
			t.Errorf("%s: %s", sn.Id, sn.Content)
		}
	}
	if !hasPublish || direct.RegistryUrl != "https://quader.example.com/api/npm/it/npm-all/" {
		t.Errorf("the direct setup changed: %+v", direct)
	}
	for _, client := range []string{"pnpm", "yarn", "yarn-classic"} {
		out := setupFrom(t, h, "/api/v1/projects/it/repositories/npm-all/client-setup?client="+client+"&bastion=partners")
		for _, sn := range out.Snippets {
			if strings.Contains(sn.Content, "replace-registry-host") {
				t.Errorf("%s: %s", client, sn.Content)
			}
		}
		if !strings.HasPrefix(out.RegistryUrl, "https://packages.example.com/") {
			t.Errorf("%s: %s", client, out.RegistryUrl)
		}
	}
}

// TestR_BAS_007_AnAddressThatIsNotListedAnswers409: the reported address must be an entry of
// server.additional_base_urls; a registration that has reported none answers the same code.
func TestR_BAS_007_AnAddressThatIsNotListedAnswers409(t *testing.T) {
	reported := reportedURLs{"partners": "https://packages.example.com", "fresh": "", "other": "https://other.example.com"}
	h := bastionServer(t, reported, "https://elsewhere.example.com", "https://other.example.com/")
	for name, want := range map[string]int{"partners": 409, "fresh": 409, "other": 200} {
		w := get(h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion="+name)
		if w.Code != want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
			continue
		}
		if want == 409 {
			if p := setupProblem(t, w.Body.Bytes()); p.Code != errs.CodeBastionBaseURLUnknown {
				t.Errorf("%s: code %q", name, p.Code)
			}
		}
	}
	// A listed entry with a trailing slash and another case of the host matches.
	h = bastionServer(t, reportedURLs{"partners": "https://Packages.Example.com"}, "https://packages.example.com/")
	out := setupFrom(t, h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion=partners")
	if out.RegistryUrl != "https://packages.example.com/api/npm/it/npm-all/" {
		t.Errorf("the registry URL is the listed entry's form: %s", out.RegistryUrl)
	}
}

// TestR_BAS_007_ARepositoryTheBastionDoesNotDistributeAnswers409: a set-up through a Bastion is offered only for a repository it
// distributes (R-BAS-134), since it refuses every read of any other (R-AUTHZ-043); the refusal comes before the Bastion's address is
// looked at.
func TestR_BAS_007_ARepositoryTheBastionDoesNotDistributeAnswers409(t *testing.T) {
	h := bastionServer(t, reportedURLs{"partners": "https://packages.example.com", undistributing: ""}, "https://packages.example.com")
	w := get(h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion="+undistributing)
	if w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if p := setupProblem(t, w.Body.Bytes()); p.Code != errs.CodeRepositoryNotDistributed {
		t.Errorf("code %q", p.Code)
	}
	setupFrom(t, h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion=partners")
}

// TestR_BAS_007_AnUnknownBastionIsNotFound: a name that is no registration, or no name at all,
// answers 404; a lookup that fails answers the failure and not a guess.
func TestR_BAS_007_AnUnknownBastionIsNotFound(t *testing.T) {
	h := bastionServer(t, reportedURLs{"partners": "https://packages.example.com"}, "https://packages.example.com")
	for _, name := range []string{"nobody", "Partners", "1partners", "a%20b", "abcdefghijklmnopqrstu"} {
		if w := get(h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion="+name); w.Code != 404 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	h = bastionServer(t, failingURLs{}, "https://packages.example.com")
	if w := get(h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion=partners"); w.Code != 500 {
		t.Errorf("a failed lookup: %d %s", w.Code, w.Body.String())
	}
}

// TestR_BAS_007_WithoutTheLookupTheParameterAnswers503: a replica that cannot read the registrations
// does not fall back to the installation's own address.
func TestR_BAS_007_WithoutTheLookupTheParameterAnswers503(t *testing.T) {
	h := bastionServer(t, nil, "https://packages.example.com")
	if w := get(h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion=partners"); w.Code != 503 {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}

// TestR_BAS_007_ABastionSetupNeedsNoBaseURLOfTheEndpoint: the snippets are on the Bastion's address,
// so an installation without a configured base URL still answers them.
func TestR_BAS_007_ABastionSetupNeedsNoBaseURLOfTheEndpoint(t *testing.T) {
	h := setupServerWith(t, "", true, authed, func(ps *PackageServices) {
		ps.Bastions = reportedURLs{"partners": "https://packages.example.com"}
		ps.AdditionalBaseURLs = []string{"https://packages.example.com"}
	})
	if w := get(h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm"); w.Code != 503 {
		t.Fatalf("the direct setup without a base URL: %d", w.Code)
	}
	out := setupFrom(t, h, "/api/v1/projects/it/repositories/npm-all/client-setup?client=npm&bastion=partners")
	if out.RegistryUrl != "https://packages.example.com/api/npm/it/npm-all/" {
		t.Errorf("%+v", out)
	}
}

// TestR_OCI_030_ABastionsSetUpOfAPublicRepositoryHasNoLogin: at a Bastion an image or a chart of a repository open to anonymous readers
// pulls without a login, and a Bastion refuses every push (R-BAS-095), so Docker, Podman and Helm have no configure snippet there, with
// or without a credential asked for (Q-OCI-277). A private repository keeps the login that sends the token, and the main installation's
// own set-up of the public repository keeps the interactive login a push needs.
func TestR_OCI_030_ABastionsSetUpOfAPublicRepositoryHasNoLogin(t *testing.T) {
	server := func(public ...string) http.Handler {
		return setupServerWith(t, "https://quader.example.com", true, authed, func(ps *PackageServices) {
			ps.Decider = packagesDecider{deploy: true, public: public}
			ps.Bastions = reportedURLs{"partners": "https://packages.example.com"}
			ps.AdditionalBaseURLs = []string{"https://packages.example.com"}
		})
	}
	const path = "/api/v1/projects/it/repositories/oci-local/client-setup?client="
	configure := func(out gen.ClientSetup) []string {
		var ids []string
		for _, sn := range out.Snippets {
			if sn.Tab == gen.ClientSnippetTabConfigure {
				ids = append(ids, sn.Id+": "+sn.Content)
			}
		}
		return ids
	}
	public, private := server("it/oci-local"), server()
	for _, client := range []string{"docker", "podman", "helm"} {
		for _, creds := range []string{"none", "env"} {
			out := setupFrom(t, public, path+client+"&credentials="+creds+"&bastion=partners")
			if got := configure(out); len(got) != 0 {
				t.Errorf("%s, %s: a public repository's set-up at a Bastion configures %q", client, creds, got)
			}
			if len(out.Snippets) == 0 {
				t.Errorf("%s, %s: no snippet at all", client, creds)
			}
		}
		withToken := configure(setupFrom(t, private, path+client+"&credentials=env&bastion=partners"))
		if len(withToken) == 0 || !strings.Contains(withToken[0], "login") || !strings.Contains(withToken[0], "${MY_TOKEN}") {
			t.Errorf("%s: a private repository's set-up at a Bastion lost its login: %q", client, withToken)
		}
		direct := configure(setupFrom(t, public, path+client+"&credentials=env"))
		if len(direct) == 0 || !strings.Contains(direct[0], "login") || strings.Contains(direct[0], "MY_TOKEN") {
			t.Errorf("%s: the main installation's own set-up of a public repository lost its interactive login: %q", client, direct)
		}
	}
}
