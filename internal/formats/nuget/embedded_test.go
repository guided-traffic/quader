package nuget

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/nuget/nugettest"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
	"github.com/guided-traffic/quader/internal/formats/nuget/version"
	"github.com/guided-traffic/quader/internal/repository"
)

// The embedded files of a version (R-NUGET-024, Q-NUGET-135 to Q-NUGET-137): unit tests over the kit's Host and the twin of the data
// port, which reads the files out of the pushed package with the reader the database store uses. Payloads are bytes.

var (
	// tinyPNG is the PNG signature and a few bytes: an icon by its content.
	tinyPNG     = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR-test")
	readmeText  = []byte("# Acme.Widgets\n\nWidgets for tests.\n")
	licenseText = []byte("MIT License\n\nCopyright Acme\n")
)

// embeddedOpts is a package that embeds an icon, a license file and a README.
func embeddedOpts() []pkgOpt {
	return []pkgOpt{
		withMetadata(`<icon>images\icon.png</icon><readme>docs/README.md</readme><license type="file">LICENSE.txt</license>`),
		withEntry("images/icon.png", tinyPNG), withEntry("docs/README.md", readmeText), withEntry("LICENSE.txt", licenseText),
	}
}

// pushEmbedded pushes Acme.Widgets 1.2.3 with its embedded files and puts its bytes into the twin, where the database store reads the
// object store.
func pushEmbedded(t *testing.T, h *writeHost, opts ...pkgOpt) {
	t.Helper()
	h.st.PutPackage(h.mustPush(t, "Acme.Widgets", "1.2.3", opts...))
}

const leafOfWidgets = "v3/flatcontainer/acme.widgets/1.2.3/"

func TestR_NUGET_024_TheEmbeddedFilesAreReadOutOfTheStoredPackage(t *testing.T) {
	h := newWriteHost(t, formattest.HostOptions{})
	pushEmbedded(t, h, embeddedOpts()...)
	for _, c := range []struct {
		path, mediaType string
		body            []byte
	}{
		{leafOfWidgets + "icon", "image/png", tinyPNG},
		{leafOfWidgets + "license", "text/plain; charset=utf-8", licenseText},
		{leafOfWidgets + "readme", "text/markdown; charset=utf-8", readmeText},
		{"v3/flatcontainer/ACME.Widgets/1.2.3.0/README", "text/markdown; charset=utf-8", readmeText},
	} {
		r := h.do(t, http.MethodGet, c.path)
		if r.Code != http.StatusOK || !bytes.Equal(r.Body.Bytes(), c.body) {
			t.Fatalf("GET %s: %d %q", c.path, r.Code, r.Body.String())
		}
		hd := r.Header()
		if hd.Get("Content-Type") != c.mediaType || hd.Get("X-Content-Type-Options") != "nosniff" ||
			hd.Get("Content-Security-Policy") != "sandbox; default-src 'none'" || hd.Get("Cross-Origin-Resource-Policy") != "same-site" {
			t.Errorf("GET %s: the headers of package-derived content are missing: %v", c.path, hd)
		}
		if hd.Get("Content-Disposition") != "" {
			t.Errorf("GET %s: a document carries Content-Disposition %q", c.path, hd.Get("Content-Disposition"))
		}
	}
	head := h.do(t, http.MethodHead, leafOfWidgets+"readme")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "35" {
		t.Errorf("HEAD readme: %d, %d bytes, Content-Length %q", head.Code, head.Body.Len(), head.Header().Get("Content-Length"))
	}
	etag := h.do(t, http.MethodGet, leafOfWidgets+"icon").Header().Get("ETag")
	reads := h.st.PackageReads()
	resp, err := h.Serve(context.Background(), http.MethodGet, leafOfWidgets+"icon", nil, false, func(r *http.Request) { r.Header.Set("If-None-Match", etag) })
	if err != nil || resp.Code != http.StatusNotModified || etag == "" {
		t.Fatalf("If-None-Match %s: %v %d", etag, err, resp.Code)
	}
	if h.st.PackageReads() != reads {
		t.Error("a 304 read the stored package")
	}
}

// TestR_NUGET_024_AVersionThatEmbedsNoFileIsA404FromTheDatabase: clients probe the README of every package they show, so a version
// whose .nuspec names no such file is answered without a read of the object store; a version that is not stored is a 404 as well.
func TestR_NUGET_024_AVersionThatEmbedsNoFileIsA404FromTheDatabase(t *testing.T) {
	h := newWriteHost(t, formattest.HostOptions{})
	pushEmbedded(t, h)
	for _, p := range []string{leafOfWidgets + "icon", leafOfWidgets + "license", leafOfWidgets + "readme", "v3/flatcontainer/acme.widgets/9.9.9/readme"} {
		if r := h.do(t, http.MethodGet, p); r.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", p, r.Code)
		}
	}
	if n := h.st.PackageReads(); n != 0 {
		t.Errorf("the 404s read the stored package %d times", n)
	}
}

// TestR_NUGET_024_AFileOutsideItsBoundsIsNotServed: an icon that is neither PNG nor JPEG, a README above 1 MiB, a license that is not
// UTF-8 and a file the .nuspec names and the package does not hold are answered as files the package does not embed.
func TestR_NUGET_024_AFileOutsideItsBoundsIsNotServed(t *testing.T) {
	h := newWriteHost(t, formattest.HostOptions{})
	pushEmbedded(t, h,
		withMetadata(`<icon>icon.gif</icon><readme>README.md</readme><license type="file">LICENSE.txt</license>`),
		withEntry("icon.gif", []byte("GIF89a-test")), withEntry("README.md", bytes.Repeat([]byte("a"), maxEmbedded+1)),
		withEntry("LICENSE.txt", []byte{0xff, 0xfe, 'M', 'I', 'T'}))
	h.st.PutPackage(h.mustPush(t, "Acme.Gone", "1.0.0", withMetadata(`<readme>README.md</readme>`)))
	for _, p := range []string{leafOfWidgets + "icon", leafOfWidgets + "license", leafOfWidgets + "readme", "v3/flatcontainer/acme.gone/1.0.0/readme"} {
		if r := h.do(t, http.MethodGet, p); r.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", p, r.Code)
		}
	}
}

// TestQ_NUGET_136_AnEmbeddedFileOfAPackageACheckHoldsBackIsNotServed: the read decides the checks of a download of the .nupkg it reads
// from (ItemStore.Check: the malware gate, the vulnerability block, the signature gate), and answers their refusal before any byte of the
// package is read.
func TestQ_NUGET_136_AnEmbeddedFileOfAPackageACheckHoldsBackIsNotServed(t *testing.T) {
	var checked []string
	refusal := errs.E(errs.Forbidden, errs.CodeMalwareFound, "Quader found malware in acme.widgets/1.2.3/acme.widgets.1.2.3.nupkg.")
	h := newWriteHost(t, formattest.HostOptions{Check: func(_ context.Context, _ *formats.Request, it formats.Item) error {
		checked = append(checked, it.Ref.Path)
		return refusal
	}})
	pushEmbedded(t, h, embeddedOpts()...)
	for _, file := range []string{"icon", "license", "readme"} {
		r := h.do(t, http.MethodGet, leafOfWidgets+file)
		if r.Code != http.StatusForbidden || code(t, r) != errs.CodeMalwareFound {
			t.Errorf("GET %s: %d %q, want the refusal of the package", file, r.Code, r.Body.String())
		}
	}
	if len(checked) != 3 || checked[0] != NupkgPath("acme.widgets", "1.2.3") {
		t.Errorf("checked %v, want the .nupkg once per request", checked)
	}
	if n := h.st.PackageReads(); n != 0 {
		t.Errorf("a refused file read the stored package %d times", n)
	}
}

// TestR_NUGET_024_TheDocumentsLinkTheEmbeddedFiles: a version that embeds an icon and a license file links them at the feed's own flat
// container in the registration and in the search, and the service index names the README template.
func TestR_NUGET_024_TheDocumentsLinkTheEmbeddedFiles(t *testing.T) {
	h := newWriteHost(t, formattest.HostOptions{})
	pushEmbedded(t, h, append(embeddedOpts(), withMetadata(`<iconUrl>https://legacy.example/icon.png</iconUrl>`))...)
	var reg struct {
		Items []struct {
			Items []struct {
				CatalogEntry struct {
					IconURL    string `json:"iconUrl"`
					LicenseURL string `json:"licenseUrl"`
				} `json:"catalogEntry"`
			} `json:"items"`
		} `json:"items"`
	}
	r := h.do(t, http.MethodGet, "v3/registration/acme.widgets/index.json")
	if err := json.Unmarshal(r.Body.Bytes(), &reg); err != nil || len(reg.Items) != 1 || len(reg.Items[0].Items) != 1 {
		t.Fatalf("registration: %d %v %s", r.Code, err, r.Body.String())
	}
	e := reg.Items[0].Items[0].CatalogEntry
	if e.IconURL != feedOfMain+"flatcontainer/acme.widgets/1.2.3/icon" || e.LicenseURL != feedOfMain+"flatcontainer/acme.widgets/1.2.3/license" {
		t.Errorf("the catalog entry links %q and %q", e.IconURL, e.LicenseURL)
	}
	var search struct {
		Data []struct {
			IconURL    string `json:"iconUrl"`
			LicenseURL string `json:"licenseUrl"`
		} `json:"data"`
	}
	r = get(t, h.Host, http.MethodGet, "v3/search", func(r *http.Request) { r.URL.RawQuery = "q=widgets" })
	if err := json.Unmarshal(r.Body.Bytes(), &search); err != nil || len(search.Data) != 1 {
		t.Fatalf("search: %d %v %s", r.Code, err, r.Body.String())
	}
	if d := search.Data[0]; d.IconURL != feedOfMain+"flatcontainer/acme.widgets/1.2.3/icon" || d.LicenseURL != feedOfMain+"flatcontainer/acme.widgets/1.2.3/license" {
		t.Errorf("the search result links %q and %q", d.IconURL, d.LicenseURL)
	}
	if body := h.do(t, http.MethodGet, "v3/index.json").Body.String(); !strings.Contains(body, `"`+feedOfMain+`flatcontainer/{lower_id}/{lower_version}/readme"`) {
		t.Errorf("the service index names no README template: %s", body)
	}
}

// seedCached records a version of a remote or a member's repository in the twin the way a cache fill does, with its metadata, and
// stores its package in the Host and the twin.
func seedCached(t *testing.T, h *formattest.Host, st *nugettest.Store, repoID int64, opts ...pkgOpt) {
	t.Helper()
	pkg := buildNupkg(t, "Acme.Widgets", "1.2.3", opts...)
	v := version.MustParse("1.2.3")
	st.Seed(rows.VersionInput{
		RepoID: repoID, VersionID: repoID*100 + 1, LowerID: "acme.widgets", DeclaredID: "Acme.Widgets", OriginalVersion: "1.2.3",
		LowerVersion: v.URLForm(), SortKey: v.SortKey(), FullVersion: v.FullNormalized(), Origin: rows.OriginCache,
		Metadata: []byte(`{"icon":"images\\icon.png","readme":"docs/README.md","license":{"type":"file","value":"LICENSE.txt"}}`),
	})
	h.PutItem(NupkgPath("acme.widgets", "1.2.3"), "application/octet-stream", pkg)
	st.PutPackage(pkg)
}

// TestQ_NUGET_137_ARemoteServesTheFilesOfThePackagesItsCacheHolds: a remote repository reads the embedded files out of the .nupkg its
// cache holds; a version it has not cached is a 404, and no fetch of the package is started for it.
func TestQ_NUGET_137_ARemoteServesTheFilesOfThePackagesItsCacheHolds(t *testing.T) {
	st := nugettest.New()
	h := formattest.NewHost(New(), formattest.HostOptions{Class: formats.Remote, Data: st})
	if r := get(t, h, http.MethodGet, leafOfWidgets+"readme"); r.Code != http.StatusNotFound {
		t.Fatalf("an uncached version: %d, want 404", r.Code)
	}
	seedCached(t, h, st, h.Ref().ID, embeddedOpts()...)
	if r := get(t, h, http.MethodGet, leafOfWidgets+"readme"); r.Code != http.StatusOK || !bytes.Equal(r.Body.Bytes(), readmeText) {
		t.Fatalf("a cached version's README: %d %q", r.Code, r.Body.String())
	}
}

// TestQ_NUGET_137_AVirtualServesTheFilesOfTheFirstMemberThatHoldsThePackage: the walk asks the members in resolution order; a member
// that holds no such version is a miss and the next one answers, and a member that holds the package and whose checks refuse ends the
// walk with the refusal (R-REPO-019).
func TestQ_NUGET_137_AVirtualServesTheFilesOfTheFirstMemberThatHoldsThePackage(t *testing.T) {
	members := []repository.Repository{
		{ID: "5", Name: qr("empty"), Class: repository.Local, Format: string(ID)},
		{ID: "6", Name: qr("holder"), Class: repository.Local, Format: string(ID)},
		{ID: "7", Name: qr("later"), Class: repository.Local, Format: string(ID)},
	}
	plan := &formats.VirtualPlan{Admitted: true}
	for i, m := range members {
		plan.Members = append(plan.Members, formats.PlannedMember{Member: formats.RepoRef{Name: m.Name}, Tier: formats.TierLocal, Position: i})
	}
	for _, refuse := range []bool{false, true} {
		st := nugettest.New()
		f := New()
		var checked []string
		h := formattest.NewHost(f, formattest.HostOptions{
			Class: formats.Virtual, RepoKey: "all", Data: st, Resolver: &mergeResolver{f: f, plan: plan, scope: allScope()},
			Check: func(_ context.Context, rq *formats.Request, _ formats.Item) error {
				checked = append(checked, rq.Repo.Name.Key)
				if refuse {
					return errs.E(errs.Forbidden, errs.CodeMalwareFound, "Quader found malware in the package.")
				}
				return nil
			},
		})
		*h.Repo() = repository.Assemble(*h.Repo(), members, nil, 0)
		seedCached(t, h, st, 6, embeddedOpts()...)
		seedCached(t, h, st, 7, embeddedOpts()...)
		r := get(t, h, http.MethodGet, leafOfWidgets+"icon")
		switch {
		case !refuse && (r.Code != http.StatusOK || !bytes.Equal(r.Body.Bytes(), tinyPNG)):
			t.Errorf("the icon through the virtual repository: %d %q", r.Code, r.Body.String())
		case refuse && r.Code != http.StatusForbidden:
			t.Errorf("a refused member: %d, want its 403", r.Code)
		}
		if len(checked) != 1 || checked[0] != "holder" {
			t.Errorf("refuse %v: the checks were asked of %v, want the member that holds the package alone", refuse, checked)
		}
	}
}
