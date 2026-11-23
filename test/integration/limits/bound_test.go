//go:build integration

package limits

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-LIM-005, R-EP-006, Q-LIM-41: a request through a host bound to a repository of the registry is charged to that repository. A rule
// at it/images (one request an hour for every signed-in user, written before the replica starts) decides the reader's requests that
// name no project and no key on the bound host, as it decides the same image's requests by its full name on the default host: both are
// one budget key. The same short path on the default host names another repository and is decided at the instance. About 3.5 s, no
// payload.
func TestR_LIM_005_ARequestThroughABoundHostIsChargedToTheBoundRepository(t *testing.T) {
	const host, images = "images.quader.test", "images"
	e := newEnvWith(t, envOptions{
		valkey: true, readable: []string{images},
		endpoints: `{"bound":{"listen":"127.0.0.1:0","base_url":"http://` + host + `","surface":["packages"],"exposure":"all",` +
			`"auth":{"token_types":["pat","sat"],"bound_tokens_only":false},"host_roots":{"` + host + `":"` + testenv.DefaultProject + `/` + images + `"}}}`,
		seed: func(st *testenv.Stack) {
			st.Repository(images, "local", "oci", `{}`)
			st.Exec(`INSERT INTO limit_assignment (id, place_kind, repository_id, who_kind, who_ref, profile_id, overrides, capped)
				SELECT gen_random_uuid(), 'repository', r.id, 'every', 'user', p.id, '{"requestsPerHour": 1}', false
				  FROM repositories r, limit_profile p WHERE r.key = $1 AND p.key = 'unlimited'`, images)
		},
	})
	bound := func() testenv.Answer {
		return e.st.DoAt(e.r.Addr("bound"), host, "GET", "/v2/library/app/manifests/1", e.reader, nil)
	}
	// The registry answers in its own error shape (R-ARCH-023); one request an hour is the rule at the repository and nowhere else.
	atImages := func(what string, a testenv.Answer) {
		t.Helper()
		if a.Status != 429 || !strings.Contains(string(a.Body), "TOOMANYREQUESTS") || !strings.Contains(string(a.Body), "1 requests per hour") {
			t.Fatalf("%s: %d %s, want the 429 of the rule at the repository", what, a.Status, a.Body)
		}
	}
	if a := bound(); a.Status == 429 {
		t.Fatalf("the first request through the bound host: %d %s", a.Status, a.Body)
	}
	atImages("the second request through the bound host", bound())
	atImages("the same image by its full name on the default host",
		e.st.Do(e.r, "GET", "/v2/"+testenv.DefaultProject+"/"+images+"/library/app/manifests/1", e.reader, nil))
	if a := e.st.Do(e.r, "GET", "/v2/library/app/manifests/1", e.reader, nil); a.Status == 429 {
		t.Fatalf("the short path on the default host names no bound repository and was refused: %s", a.Body)
	}
}
