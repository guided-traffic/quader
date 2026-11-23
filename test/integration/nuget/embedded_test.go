//go:build integration

package nuget

import (
	"testing"
)

// TestR_NUGET_024_TheEmbeddedFilesThroughTheDataPort: a pushed package's icon, license and README are read out of the stored .nupkg
// through the PostgreSQL store and the object store, in the local repository and through a virtual repository that holds it as a
// member; a version that embeds no README is a 404 (Q-NUGET-135, Q-NUGET-137). What the route decides beyond the data port is held by
// the unit tests and the kit (internal/formats/nuget). Bytes; about a second on the stack.
func TestR_NUGET_024_TheEmbeddedFilesThroughTheDataPort(t *testing.T) {
	e := newWriteEnv(t)
	e.st.Repository("nuget-emb-all", "virtual", "nuget", `{}`)
	e.st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id)
		SELECT v.id, 0, m.id FROM repositories v, repositories m WHERE v.key = 'nuget-emb-all' AND m.key = 'nuget-local'`)
	e.st.Policy("emb-readers", []string{"read"}, []string{"nuget-emb-all"}, e.reader)
	r := e.replica("web-a")
	png := "\x89PNG\r\n\x1a\n-it"
	e.mustPush(r, "nuget-local", e.tokDev, nupkg(t, pkgSpec{
		id: "Acme.Embedded", version: "1.0.0",
		metadata: `<icon>icon.png</icon><readme>docs/README.md</readme><license type="file">LICENSE.txt</license>`,
		entries:  map[string]string{"icon.png": png, "docs/README.md": "# Acme.Embedded\n", "LICENSE.txt": "MIT\n"},
	}))
	e.mustPush(r, "nuget-local", e.tokDev, nupkg(t, pkgSpec{id: "Acme.Plain", version: "1.0.0"}))
	for _, repo := range []string{"nuget-local", "nuget-emb-all"} {
		for file, want := range map[string][2]string{
			"icon":    {"image/png", png},
			"license": {"text/plain; charset=utf-8", "MIT\n"},
			"readme":  {"text/markdown; charset=utf-8", "# Acme.Embedded\n"},
		} {
			a := e.st.Do(r, "GET", "/api/nuget/it/"+repo+"/v3/flatcontainer/acme.embedded/1.0.0/"+file, e.tokReader, nil)
			if a.Status != 200 || a.Header.Get("Content-Type") != want[0] || string(a.Body) != want[1] {
				t.Errorf("%s %s: %d %q %q", repo, file, a.Status, a.Header.Get("Content-Type"), a.Body)
			}
		}
		if a := e.st.Do(r, "GET", "/api/nuget/it/"+repo+"/v3/flatcontainer/acme.plain/1.0.0/readme", e.tokReader, nil); a.Status != 404 {
			t.Errorf("%s: the README of a package that embeds none: %d, want 404", repo, a.Status)
		}
	}
}
