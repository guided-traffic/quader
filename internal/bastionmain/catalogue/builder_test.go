package catalogue_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue/cataloguetest"
)

// The builder of the public catalogue against a fake management API (R-BAS-104 to R-BAS-108): what it renders, what it strips,
// how it rewrites links and when it renders again. No database; the documents are bytes.

const (
	mainOrigin = "https://quader.example.com"
	otherBase  = "https://other.example.com"
	bastionURL = "https://packages.example.com"
	regID      = "0192f1c4-0000-7000-8000-0000000000b1"
	sigObject  = "5d41402abc4b2a76b9719d911017c592ae2c7f7d6b2f3e1a0e2b1c3d4e5f6a7b"
	pkgID      = "pkg_npmleftpad"
)

// fakeAPI answers the management API's reads by path; the query is ignored, every list is one page. now stands for the time of
// a check, which a rescan moves.
type fakeAPI struct {
	mu    sync.Mutex
	now   string
	bases map[string]bool
	reads map[string]int
	// gone removes the package from the index.
	gone bool
	// latest is the package's latest version, which a publish moves.
	latest string
}

func newFake() *fakeAPI {
	return &fakeAPI{now: "2026-11-26T10:00:00Z", bases: map[string]bool{}, reads: map[string]int{}, latest: "1.0.0"}
}

func (f *fakeAPI) Get(_ context.Context, base, target string) (catalogue.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bases[base] = true
	u, err := url.Parse(target)
	if err != nil {
		return catalogue.Answer{}, err
	}
	f.reads[u.Path]++
	body, ok := f.body(base, u)
	switch {
	case !ok:
		return catalogue.Answer{Status: http.StatusNotFound, ContentType: "application/problem+json", Body: []byte(`{"status":404}`)}, nil
	case strings.HasPrefix(u.Path, "/api/v1/signatures/objects/"):
		return catalogue.Answer{Status: http.StatusOK, ContentType: "application/octet-stream", Body: []byte(body)}, nil
	}
	return catalogue.Answer{Status: http.StatusOK, ContentType: "application/json; charset=utf-8", Body: []byte(body)}, nil
}

func (f *fakeAPI) body(base string, u *url.URL) (string, bool) {
	v := "/api/v1/packages/" + pkgID + "/versions/1.0.0"
	switch u.Path {
	case "/api/v1/formats":
		return `{"formats":[{"name":"npm","wiki":true,"clients":[{"id":"npm"}],"upstreamPresets":[{"url":"https://registry.npmjs.org"}]},
			{"name":"generic","patternSubject":"path","clients":[{"id":"curl"}],"upstreamPresets":[]}]}`, true
	case "/api/v1/projects":
		return `{"items":[{"key":"acme"}],"nextCursor":null}`, true
	case "/api/v1/projects/acme/repositories":
		return `{"items":[{"key":"npm-local","format":"npm","class":"local","visibility":{"artifacts":"public","wiki":"public"}},
			{"key":"files","format":"generic","class":"local","visibility":{"artifacts":"public","wiki":"private"}}]}`, true
	case "/api/v1/packages":
		if f.gone {
			return `{"items":[]}`, true
		}
		return `{"items":[{"id":"` + pkgID + `","format":"npm","name":"left-pad","latestVersion":"` + f.latest + `","downloadCount":51,
			"vulnerabilityGrade":{"grade":"B","checkedAt":"` + f.now + `"}}],"nextCursor":null}`, true
	case "/api/v1/packages/" + pkgID:
		return `{"id":"` + pkgID + `","name":"left-pad","downloadCount":51,"latestVersion":"` + f.latest + `"}`, true
	case "/api/v1/packages/" + pkgID + "/versions":
		return `{"items":[{"version":"1.0.0","downloadCount":3,"lastDownloadedAt":"` + f.now + `"}]}`, true
	case v:
		return `{"version":"1.0.0","repositories":[{"key":"acme/npm-local","class":"local"}],
			"metadata":{"publisher":"sa:release-bot"},
			"files":[{"path":"left-pad/-/left-pad-1.0.0.tgz","downloadUrl":"` + base + `/api/npm/acme/npm-local/left-pad/-/left-pad-1.0.0.tgz",
			"contentUrl":"/api/v1/projects/acme/repositories/npm-local/content/left-pad/-/left-pad-1.0.0.tgz",
			"upstreamUrl":"https://npm.internal.corp/left-pad.tgz",
			"malware":{"state":"clean","scannedAt":"` + f.now + `","database":"27001","databaseBuiltAt":"` + f.now + `","releasedBy":"user:alice",
				"scanner":"malware-scanner-clamav","engine":"1.5.5","scannerInstance":"quader-malware-scanner-clamav-7d9f8","age":"current",
				"pressure":3,"due":false,"nextCheckInDays":29,"downloadsSince":4}}],
			"malware":{"state":"clean","scannedAt":"` + f.now + `","database":"27001","databaseBuiltAt":"` + f.now + `","files":1,"currentFiles":1,
				"decidedBy":{"sha256":"aa","path":"left-pad/-/left-pad-1.0.0.tgz"},"engine":"1.5.5","age":"current","pressure":3,"due":false},
			"homepage":"` + mainOrigin + `/ui/en/projects/acme"}`, true
	case v + "/readme":
		return `{"html":"<p>See <a href=\"` + otherBase + `/api/v1/packages\">the API</a></p>"}`, true
	case v + "/vulnerabilities":
		return `{"syncedAt":"` + f.now + `","grade":{"grade":"B","open":1,"checkedAt":"` + f.now + `","stateCode":"evaluated"},
			"findings":[{"id":"GHSA-1","status":"confirmed","statusChangedBy":"user:bob","comment":"we know","history":[]}]}`, true
	case v + "/signatures":
		return `{"repositories":[{"repository":"acme/npm-local","origin":{"kind":"uploaded","principalKind":"user","principalName":"alice",
			"ci":{"project_path":"acme/web"},"at":"2026-11-01T00:00:00Z"},
			"signatures":[{"original":"` + base + `/api/v1/signatures/objects/` + sigObject + `","computedAt":"` + f.now + `",
			"trust":{"state":"internal","anchors":[{"id":3,"title":"Company release CA","scope":"installation"},{"id":1,"title":"Public Sigstore","scope":"shipped"}]}}]}]}`, true
	case "/api/v1/signatures/objects/" + sigObject:
		return "\x30\x82signature", true
	case "/api/v1/projects/acme/repositories/npm-local/client-setup", "/api/v1/projects/acme/repositories/files/client-setup":
		return `{"snippets":[{"content":"npm config set registry ` + base + `/api/npm/acme/npm-local/ # ` + u.Query().Get("client") + `"}]}`, true
	case "/api/v1/projects/acme/repositories/files/children":
		return `{"items":[{"path":"tool.bin","kind":"file"}]}`, true
	case "/api/v1/projects/acme/repositories/files/items":
		return `{"path":"tool.bin","digests":{"sha256":"aa"},"upstreamUrl":"https://files.internal.corp/tool.bin","releasedBy":"user:carol"}`, true
	}
	return "", false
}

func (f *fakeAPI) set(fn func(*fakeAPI)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func builder(store catalogue.Store, api catalogue.Renderer) *catalogue.Builder {
	return catalogue.New(catalogue.Options{
		Store: store, Renderer: api, Main: []string{mainOrigin, otherBase, bastionURL}, PolicyHash: "policy-1", Workers: 2,
		Now: func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) },
	})
}

func reg(gui, docs bool) catalogue.Registration {
	return catalogue.Registration{ID: regID, Name: "partners", GUI: gui, Docs: docs, BaseURL: bastionURL}
}

func run(t *testing.T, b *catalogue.Builder, regs ...catalogue.Registration) catalogue.Report {
	t.Helper()
	rep, err := b.Run(context.Background(), regs)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// TestR_BAS_104_TheBuilderRendersEveryDocumentOnTheBastionsBase: a registration's build reads the management API on the
// Bastion's base URL alone and keeps every document of section C for an npm package and a generic file: the index, the package, its
// versions, the version with its README, open findings, signatures, set-up and state, the repository's set-up, the generic folder,
// file and set-up, the signature object the signatures name, and the catalogue's state.
func TestR_BAS_104_TheBuilderRendersEveryDocumentOnTheBastionsBase(t *testing.T) {
	store, api := cataloguetest.New(), newFake()
	store.Packages[catalogue.PackageKey{Format: "npm", Name: "left-pad"}] = "f1"
	rep := run(t, builder(store, api), reg(true, true))
	if len(rep.Built) != 1 || rep.Failed != 0 {
		t.Fatalf("report %+v", rep)
	}
	if len(api.bases) != 1 || !api.bases[bastionURL] {
		t.Fatalf("the renderer was asked on %v, want the Bastion's base URL alone", api.bases)
	}
	docs := store.Raw(regID)
	v := "packages/" + pkgID + "/versions/1.0.0"
	for _, p := range []string{
		"index/0", "packages/" + pkgID, "packages/" + pkgID + "/versions", v, v + "/readme", v + "/vulnerabilities", v + "/signatures",
		v + "/state", "setup/acme/npm-local/npm/" + pkgID + "/1.0.0", "setup/acme/npm-local/npm", "setup/acme/files/curl",
		"files/acme/files/folder", "files/acme/files/file/tool.bin", "files/acme/files/setup/curl/tool.bin",
		"signatures/objects/" + sigObject, "state",
	} {
		if _, ok := docs[p]; !ok {
			t.Errorf("no document %s; the catalogue holds %v", p, keys(docs))
		}
	}
	pub, err := store.Public(context.Background(), regID)
	if err != nil || len(pub.Repositories) != 2 || !pub.Repositories[1].Wiki || pub.Repositories[0].Wiki {
		t.Fatalf("the public set: %+v %v", pub, err)
	}
}

// TestR_BAS_106_NoDocumentNamesAnInternalIdentityOrTheMainInstallation: no document holds a principal (who uploaded, released or
// triaged), an internal anchor's title, a state code, a download count, an upstream that is no public registry, a base URL of the
// main installation or another Bastion, or the management API's path; the shipped anchor's title and the word of the state stay.
func TestR_BAS_106_NoDocumentNamesAnInternalIdentityOrTheMainInstallation(t *testing.T) {
	store, api := cataloguetest.New(), newFake()
	run(t, builder(store, api), reg(true, true))
	docs := store.Raw(regID)
	for p, b := range docs {
		s := string(b)
		for _, bad := range []string{
			"alice", "bob", "carol", "acme/web", "release-bot", "/content/", "Company release CA", "stateCode", "downloadCount", "lastDownloadedAt", "internal.corp",
			mainOrigin, otherBase, "/api/v1/", "we know",
		} {
			if strings.Contains(s, bad) {
				t.Errorf("%s holds %q: %s", p, bad, s)
			}
		}
	}
	sig := string(docs["packages/"+pkgID+"/versions/1.0.0/signatures"])
	if !strings.Contains(sig, "Public Sigstore") || !strings.Contains(sig, `"state":"internal"`) ||
		!strings.Contains(sig, bastionURL+"/ui/data/v1/signatures/objects/"+sigObject) {
		t.Errorf("the signatures lost what they show: %s", sig)
	}
	if v := string(docs["packages/"+pkgID+"/versions/1.0.0"]); !strings.Contains(v, bastionURL+"/ui/en/projects/acme") || !strings.Contains(v, `"releasedBy":null`) ||
		!strings.Contains(v, `"contentUrl":"/api/npm/acme/npm-local/left-pad/-/left-pad-1.0.0.tgz"`) || !strings.Contains(v, `"publisher":null`) {
		t.Errorf("the version: %s", v)
	}
}

// TestR_BAS_103_NoDocumentCarriesATimeOfItsCheck: the times of building and checking leave every document for the version's state,
// so a rescan or a sync that finds the same changes no document, and the state holds them by JSON pointer.
func TestR_BAS_103_NoDocumentCarriesATimeOfItsCheck(t *testing.T) {
	store, api := cataloguetest.New(), newFake()
	b := builder(store, api)
	run(t, b, reg(true, true))
	docs := store.Raw(regID)
	for p, d := range docs {
		if p == "state" || strings.HasSuffix(p, "/state") {
			continue
		}
		// Nor what a malware state says of the main installation's scanners, its demand or the set in use (B.15, R-MAL-039); the part that
		// decides a unit moves with its check.
		for _, k := range []string{
			`"scannedAt"`, `"checkedAt"`, `"syncedAt"`, `"builtAt"`, `"computedAt"`, `"databaseBuiltAt"`, `"database"`,
			`"engine"`, `"scannerInstance"`, `"age"`, `"pressure"`, `"due"`, `"nextCheckInDays"`, `"downloadsSince"`, `"currentFiles"`, `"decidedBy"`,
		} {
			if strings.Contains(string(d), k) {
				t.Errorf("%s carries %s: %s", p, k, d)
			}
		}
	}
	var st struct {
		Times map[string]map[string]any `json:"times"`
	}
	if err := json.Unmarshal(docs["packages/"+pkgID+"/versions/1.0.0/state"], &st); err != nil ||
		st.Times["version"]["/files/0/malware/scannedAt"] != "2026-11-26T10:00:00Z" || st.Times["vulnerabilities"]["/grade/checkedAt"] == nil ||
		st.Times["version"]["/malware/decidedBy"] == nil {
		t.Fatalf("the version's state: %+v %v", st, err)
	}
}

// TestR_BAS_105_TheReconciliationRendersOnlyWhatChanged: a second run renders no unit whose fingerprint is unchanged, even though a
// rescan moved every time; a changed fingerprint renders the package again, and a document whose bytes are equal is not written;
// a package gone from the index is deleted whole.
func TestR_BAS_105_TheReconciliationRendersOnlyWhatChanged(t *testing.T) {
	store, api := cataloguetest.New(), newFake()
	key := catalogue.PackageKey{Format: "npm", Name: "left-pad"}
	store.Packages[key] = "f1"
	b := builder(store, api)
	run(t, b, reg(true, true))
	seq := store.Seq(regID)

	api.set(func(f *fakeAPI) { f.now = "2026-11-26T11:00:00Z" })
	rep := run(t, b, reg(true, true))
	if rep.Rendered != 0 || rep.Documents != 0 || store.Seq(regID) != seq {
		t.Fatalf("an unchanged catalogue was written: %+v, seq %d to %d", rep, seq, store.Seq(regID))
	}

	store.Packages[key] = "f2"
	rep = run(t, b, reg(true, true))
	if rep.Rendered != 1 || rep.Documents != 1 {
		t.Fatalf("a changed fingerprint: %+v (want the package rendered and only its version's state written)", rep)
	}

	api.set(func(f *fakeAPI) { f.gone = true })
	rep = run(t, b, reg(true, true))
	docs := store.Raw(regID)
	for p := range docs {
		if strings.Contains(p, pkgID) {
			t.Errorf("%s of a package gone is still held", p)
		}
	}
	if rep.Deleted == 0 {
		t.Fatalf("nothing deleted: %+v", rep)
	}
	ch, err := store.Changes(context.Background(), regID, seq, 1000)
	if err != nil || ch.Validate(seq) != nil {
		t.Fatalf("the change list after the deletion: %+v %v", ch, err)
	}
}

// TestR_BAS_102_TheSwitchesDecideWhatIsBuilt: with docs off no documentation is built and switching it off deletes the README;
// with gui off the registration's catalogue is dropped whole; without a base URL the builder stops and records why.
func TestR_BAS_102_TheSwitchesDecideWhatIsBuilt(t *testing.T) {
	store, api := cataloguetest.New(), newFake()
	b := builder(store, api)
	run(t, b, reg(true, true))
	readme := "packages/" + pkgID + "/versions/1.0.0/readme"
	if _, ok := store.Raw(regID)[readme]; !ok {
		t.Fatal("no README with docs on")
	}
	run(t, b, reg(true, false))
	if _, ok := store.Raw(regID)[readme]; ok {
		t.Fatal("the README stays with docs off")
	}
	rep := run(t, b, reg(false, false))
	if len(rep.Dropped) != 1 || len(store.Raw(regID)) != 0 {
		t.Fatalf("gui off: %+v, %d documents", rep, len(store.Raw(regID)))
	}
	noBase := reg(true, true)
	noBase.BaseURL = ""
	rep = run(t, b, noBase)
	st, ok, _ := store.State(context.Background(), regID)
	if len(rep.Stopped) != 1 || !ok || st.Stopped != catalogue.CodeBaseURLUnknown {
		t.Fatalf("no base URL: %+v %+v", rep, st)
	}
}

// TestR_BAS_104_TheBaseURLIsAnEntryOfTheAdditionalBaseURLs: the reported base URL counts only as an entry of
// server.additional_base_urls, in its listed spelling.
func TestR_BAS_104_TheBaseURLIsAnEntryOfTheAdditionalBaseURLs(t *testing.T) {
	listed := []string{"https://Packages.example.com/", "https://other.example.com"}
	if got, ok := catalogue.BaseURL("https://packages.example.com", listed); !ok || got != "https://Packages.example.com" {
		t.Fatalf("%q %v", got, ok)
	}
	for _, r := range []string{"", "https://evil.example.com", "https://packages.example.com/x"} {
		if _, ok := catalogue.BaseURL(r, listed); ok {
			t.Errorf("%q counts", r)
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
