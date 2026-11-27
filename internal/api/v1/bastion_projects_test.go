package v1

import (
	"net/http"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_BAS_134_WhoChangesWhatABastionMayDistribute: the administrator's list is read with endpoints:read and changed with
// endpoints:write (a caller with endpoints:read alone is refused before the service), the Bastions a project allows are read by its
// Owner or a holder of repositories:read-config, and a repository's member bastions is written by whoever writes its configuration:
// the project's Owner or a holder of repositories:write-config, never a Maintainer (Q-REPO-491). What the service stores, cascades,
// records and journals is test/integration/bastionreg's (TestR_BAS_134_TheDistributionIsStoredAndNarrowsInOneTransaction).
func TestR_BAS_134_WhoChangesWhatABastionMayDistribute(t *testing.T) {
	regs := bastionmain.NewRegistrations(bastionmain.RegistrationOptions{Max: 4}) // no database: every answer below comes before it
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run("R-BAS-134 "+method+" of an allowed project needs endpoints:write", func(t *testing.T) {
			s := newSetup(t, func(o *Options) { o.Access, o.Bastions = readsEndpoints{}, regs }, authed)
			problem(t, s.do(method, "/api/v1/bastions/partners/projects/platform", "", nil), http.StatusForbidden, errs.CodeForbidden)
		})
	}
	for id, want := range map[string]Requirement{
		"listBastionProjects":  {Permission: "endpoints:read"},
		"allowBastionProject":  {Permission: "endpoints:write"},
		"removeBastionProject": {Permission: "endpoints:write"},
		"listProjectBastions":  {Permission: "repositories:read-config", Project: "path.projectKey", ProjectRole: "owner"},
		"updateRepository":     {Permission: "repositories:write-config", Project: "path.projectKey", ProjectRole: "owner"},
		"replaceRepository":    {Permission: "repositories:write-config", Project: "path.projectKey", ProjectRole: "owner"},
	} {
		t.Run("R-BAS-134 "+id+" is declared "+want.Permission+" "+want.ProjectRole, func(t *testing.T) {
			got := requirementOf(t, id)
			if got.Permission != want.Permission || got.ProjectRole != want.ProjectRole || got.Project != want.Project {
				t.Fatalf("%+v", got)
			}
		})
	}
}

// TestR_BAS_134_TheAddressesAreTheListedOnes: a repository's Bastion and a Bastion a project allows show the base URLs their deployments
// reported only where server.additional_base_urls lists them, in the listed spelling (as R-BAS-007 holds a client setup's), and an
// empty list, never null, for none.
func TestR_BAS_134_TheAddressesAreTheListedOnes(t *testing.T) {
	listed := []string{"https://pkgs.example.com/", "https://pkgs-b.example.com"}
	got := listedAddresses([]string{"https://PKGS.example.com", "https://other.example.com", "https://pkgs-b.example.com/"}, listed)
	if !slices.Equal(got, []string{"https://pkgs.example.com", "https://pkgs-b.example.com"}) {
		t.Fatalf("%q", got)
	}
	if got := listedAddresses(nil, listed); got == nil || len(got) != 0 {
		t.Fatalf("none: %#v", got)
	}
	s := &Server{o: Options{Packages: PackageServices{AdditionalBaseURLs: listed}}}
	st := s.listedBastions(repository.Stored{Document: repository.Document{Bastions: []repository.BastionRef{
		{Name: "artifacts", Addresses: []string{"https://pkgs.example.com", "https://unlisted.example.com"}},
	}}})
	if b := st.Document.Bastions; len(b) != 1 || b[0].Name != "artifacts" || !slices.Equal(b[0].Addresses, []string{"https://pkgs.example.com"}) {
		t.Fatalf("%+v", b)
	}
}
