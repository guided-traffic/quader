//go:build integration

package bastion

import (
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_AUTHZ_043_ABastionServesOnlyWhatItDistributes: a Bastion serves a repository only while it distributes it (R-AUTHZ-043,
// R-BAS-134), whatever the repository's level and whoever asks. npm-local is public and acme-full holds a read grant on it; taken off
// the Bastion by its Owner, it answers through the Bastion 404 to the token and 401 to the anonymous caller - the answers of a
// repository that does not exist - within the revocation bound, while the main installation's own door still serves both; chosen
// again, it is served again. One package document of a few hundred bytes; about 4 s.
func TestR_AUTHZ_043_ABastionServesOnlyWhatItDistributes(t *testing.T) {
	w := newWorld(t, worldOptions{})
	w.content()
	w.setAnonymous(true)
	b := w.newBastion(t, bastionOptions{Configure: relaxed}).wait()
	doc := "/api/npm/" + npmLocal + "/plain-lib"
	token, anonymous := callOpts{token: w.full.token, addr: "203.0.113.42"}, callOpts{addr: "198.51.100.42"}
	warm(t, b, token, doc)
	warm(t, b, anonymous, doc)

	choose := func(bastions ...string) {
		t.Helper()
		w.mustAPI("PATCH", "/api/v1/projects/"+testenv.DefaultProject+"/repositories/npm-local", http.StatusOK, map[string]any{"bastions": append([]string{}, bastions...)},
			"If-Match", "*", "Content-Type", "application/merge-patch+json")
	}
	choose()
	refused(t, b, doc, token, http.StatusNotFound)
	refused(t, b, doc, anonymous, http.StatusUnauthorized)
	for _, c := range []string{w.full.token, ""} {
		if got := w.api("GET", doc, c, nil); got.Status != http.StatusOK {
			t.Fatalf("the main installation's own door refuses npm-local (token %v): %d %s", c != "", got.Status, got.Body)
		}
	}

	choose("partners")
	for _, c := range []callOpts{token, anonymous} {
		for _, r := range b.reps {
			testenv.WaitFor(t, "npm-local through "+r.name+" again", bound, func() bool { return r.do(t, "GET", doc, c).Status == http.StatusOK })
		}
	}
}
