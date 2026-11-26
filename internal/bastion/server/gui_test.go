package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
)

// TestR_BAS_116_TheWebInterfaceIsMountedBeforeTheEngineAndNeverReachesTheMainInstallation: the route table of the client listener
// over the whole replica, linked to the main installation's double with the switch gui on and a catalogue of one npm package. Every
// address of the web interface answers 405 to every unsafe method; a page, the search, the lookup, the versions, a version and a
// missing document are answered from the catalogue; and the double records no forward and no check for any of it, while the package
// API beside it still forwards (W-35's B.7). About 0.3 s.
func TestR_BAS_116_TheWebInterfaceIsMountedBeforeTheEngineAndNeverReachesTheMainInstallation(t *testing.T) {
	main := maintest.New(t)
	main.SetAnonymous(true)
	main.SetConfiguration(func(c *iface.Configuration) { c.Switches.GUI, c.Switches.Docs = true, true })
	main.SetPublic(iface.CataloguePublic{
		Projects: []string{"acme"}, Repositories: []iface.CatalogueRepository{{Name: "acme/npm-local", Format: "npm", Class: "local"}},
	})
	const id = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10"
	summary := `{"id":"` + id + `","format":"npm","name":"left-pad","key":"left-pad","latestVersion":"1.0.0","repositories":["acme/npm-local"]}`
	main.PutDocument("index/0", []byte(`{"items":[`+summary+`]}`))
	main.PutDocument("packages/"+id, []byte(summary))
	main.PutDocument("packages/"+id+"/versions", []byte(`{"items":[{"version":"1.0.0"}]}`))
	main.PutDocument("packages/"+id+"/versions/1.0.0", []byte(`{"name":"left-pad","version":"1.0.0"}`))
	dir := t.TempDir()
	srv, logs := newLinked(t, linkedConfig(main, dir, tokenFileFor(t, dir, main.NewToken()), "  gui:\n    poll_interval: 1s\n"), nil)
	run(t, srv, logs)
	eventually(t, 10*time.Second, "the catalogue serves", func() bool { return srv.Catalogue().State().Ready })
	base := "http://" + srv.ClientAddr().String()
	forwards, checks := len(main.Forwards()), main.Calls("check")

	paths := []string{
		"/", "/ui", "/ui/", "/ui/en/projects/acme/packages/npm/left-pad", "/ui/data/v1/state", "/ui/data/v1/legal?locale=en",
		"/ui/data/v1/packages", "/ui/data/v1/packages/" + id + "/versions/1.0.0",
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, p := range paths {
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			req, err := http.NewRequest(m, base+p, strings.NewReader(`{"name":"evil"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = "packages.example.com"
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
				t.Errorf("%s %s = %d Allow %q", m, p, resp.StatusCode, resp.Header.Get("Allow"))
			}
		}
	}
	for p, want := range map[string]int{
		"/ui/en/projects/acme/packages/npm/left-pad":           200,
		"/ui/data/v1/packages?q=left":                          200,
		"/ui/data/v1/packages/lookup?format=npm&name=left-pad": 200,
		"/ui/data/v1/packages/" + id + "/versions":             200,
		"/ui/data/v1/packages/" + id + "/versions/1.0.0":       200,
		"/ui/data/v1/packages/" + id + "/versions/9.9.9":       404,
		"/ui/data/v1/packages/lookup?format=npm&name=other":    404,
	} {
		code, hd, body := get(t, base+p)
		if code != want || hd.Get("Quader-Bastion-Replica") != "" || hd.Get("X-Quader-Replica") != "" {
			t.Errorf("GET %s = %d %v %s", p, code, hd, body)
		}
	}
	_, _, body := get(t, base+"/ui/data/v1/packages")
	var list struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Items) != 1 || list.Items[0].Name != "left-pad" || list.NextCursor != nil {
		t.Errorf("the search = %s", body)
	}
	if got := len(main.Forwards()); got != forwards {
		t.Errorf("the web interface forwarded %d requests to the main installation", got-forwards)
	}
	if got := main.Calls("check"); got != checks {
		t.Errorf("the web interface asked the main installation %d checks", got-checks)
	}
	// The package API beside it still forwards: the engine is behind the web interface, not replaced by it.
	if code, _, _ := get(t, base+"/api/npm/acme/npm-local/left-pad"); code == http.StatusMethodNotAllowed || len(main.Forwards()) == forwards {
		t.Errorf("the package API = %d, forwards %d", code, len(main.Forwards())-forwards)
	}
}
