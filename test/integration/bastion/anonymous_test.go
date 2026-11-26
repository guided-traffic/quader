//go:build integration

package bastion

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_BAS_094_AnAnonymousPullAndAnAnonymousDocumentSpareTheMainInstallation: what an anonymous request may cause at the main
// installation (bastion.md R-BAS-094). The project and the artifacts of web/docker-local and npm-local are public, npm-local's wiki is
// not, so the anonymous caller reads the image in full and npm-local in part: its package document of npm-local is filtered for it
// (private, R-BAS-023, Q-BAS-149). An anonymous `docker pull` replayed through the Bastion makes no token request at the main installation (the Bastion
// answers it, Q-BAS-335), and ten anonymous reads of the npm document within the metadata period send one request in total; the
// Bastion counts the other nine as hits of a private document. OCI and npm stand for every format: what is kept is decided by the
// marking's class, which the Bastion reads alike for every format, and the rule itself is the engine's unit test. About 3 s, an image
// of two blobs of 1 KiB and one package document.
func TestR_BAS_094_AnAnonymousPullAndAnAnonymousDocumentSpareTheMainInstallation(t *testing.T) {
	w := ociWorld(t, worldOptions{})
	img := newOCIImage("anonymous")
	w.pushImage("app", img, "1")
	w.publishNPM("plain-lib", "1.0.0")
	w.mustAPI("PUT", "/api/v1/projects/"+testenv.DefaultProject+"/visibility", 200, map[string]any{"level": "public"}, "If-Match", "*")
	for _, repo := range []string{ociLocal, npmLocal} {
		w.mustAPI("PATCH", "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+repo[len(testenv.DefaultProject)+1:], 200,
			map[string]any{"visibility": map[string]any{"artifacts": "public"}}, "If-Match", "*", "Content-Type", "application/merge-patch+json")
	}
	testenv.WaitFor(t, "anonymous reads at the main installation", 20*time.Second, func() bool {
		return w.api("GET", "/api/npm/"+npmLocal+"/plain-lib", "", nil).Status == http.StatusOK
	})
	// The set-up's change of docker-local's settings sets its package ownership as well, which the feed reads as a narrowing (a
	// grants entry): a Bastion that starts within the lookback would judge every answer of its first 15 s by it (R-BAS-020 rule 3).
	// The set-up stands for settings older than any Bastion's history, as setUpOlderThanTheFeed's claims do.
	w.st.Exec(`DELETE FROM audit_event WHERE action = 'authz.settings.changed'`)
	b := w.newBastion(t, bastionOptions{Settings: relaxed}).wait()
	r := b.reps[0]

	m := w.front.Mark()
	p := pull{bytes: map[string][]byte{}}
	p.want(t, "ping", r.do(t, "GET", "/v2/", callOpts{}), http.StatusUnauthorized)
	scope := url.QueryEscape("repository:" + ociLocal + "/app:pull")
	tok := registryToken(t, p.want(t, "anonymous token", r.do(t, "GET", "/v2/_token?scope="+scope+"&service="+baseHost, callOpts{}), http.StatusOK))
	base := "/v2/" + ociLocal + "/app"
	bearer := []string{"Authorization", "Bearer " + tok, "Accept", accept}
	p.want(t, "manifest by tag", r.do(t, "GET", base+"/manifests/1", callOpts{header: bearer}), http.StatusOK)
	p.bytes["manifest"] = p.want(t, "manifest by digest", r.do(t, "GET", base+"/manifests/"+digestOf(img.manifest), callOpts{header: bearer}), http.StatusOK).Body
	p.bytes["config"] = p.want(t, "config", r.do(t, "GET", base+"/blobs/"+digestOf(img.config), callOpts{header: bearer}), http.StatusOK).Body
	p.bytes["layer"] = p.want(t, "layer", r.do(t, "GET", base+"/blobs/"+digestOf(img.layer), callOpts{header: bearer}), http.StatusOK).Body
	p.check(t, img)
	kinds := map[string]int{}
	for _, rec := range w.front.Since(m) {
		if rec.Path == "/v2/_token" {
			t.Errorf("an anonymous pull sent a token request to the main installation: %+v", rec)
		}
		if rec.Kind == "forward" || rec.Kind == "check" {
			if rec.Header.Get("Authorization") != "" {
				t.Errorf("the main installation received the anonymous token on %s", rec.Path)
			}
			kinds[rec.Kind]++
		}
	}
	t.Logf("a cold anonymous pull cost the main installation %v", kinds)

	doc := "/api/npm/" + npmLocal + "/plain-lib"
	m = w.front.Mark()
	for i := range 10 {
		if got := r.do(t, "GET", doc, callOpts{addr: "198.51.100.94"}); got.Status != http.StatusOK {
			t.Fatalf("anonymous read %d: %d %s", i, got.Status, got.Body)
		}
	}
	if got := w.front.Since(m); w.front.Cost(m) != 1 {
		t.Errorf("ten anonymous reads within the period cost the main installation %d requests, want 1: %+v", w.front.Cost(m), got)
	}
	if hits := sumMetric(r.metrics(t), "quader_bastion_requests_total", `class="private"`, `result="hit"`); hits != 9 {
		t.Errorf("%v reads were served as hits of the anonymous caller's private document, want 9", hits)
	}
}
