package gui

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
)

// TestR_BAS_135_ReplaceOriginReplacesWholeOriginsOnly: the catalogue's base URL is replaced where it stands as an origin of its own,
// followed by a path, a quote, an escape of JSON, a query or the end, and left inside a longer host, before a port, and in another
// scheme.
func TestR_BAS_135_ReplaceOriginReplacesWholeOriginsOnly(t *testing.T) {
	const from, to = "https://pkgs-eu.example.com", "https://pkgs-us.example.com"
	for in, want := range map[string]string{
		from:                                       to,
		from + "/api/npm/acme/npm/":                to + "/api/npm/acme/npm/",
		`"` + from + `"`:                           `"` + to + `"`,
		`href=\"` + from + `\"`:                    `href=\"` + to + `\"`,
		from + "?x=1 " + from + "#top":             to + "?x=1 " + to + "#top",
		from + ".evil.example/x":                   from + ".evil.example/x",
		from + "-mirror.example/x":                 from + "-mirror.example/x",
		from + ":8443/x":                           from + ":8443/x",
		"http://pkgs-eu.example.com/x":             "http://pkgs-eu.example.com/x",
		from + "x " + from + "/y":                  from + "x " + to + "/y",
		"see " + from + ". Or " + from + "/docs\n": "see " + from + ". Or " + to + "/docs\n",
	} {
		got, changed := replaceOrigin([]byte(in), from, to)
		if string(got) != want || changed != (in != want) {
			t.Errorf("%q: %q (changed %v), want %q", in, got, changed, want)
		}
	}
	in := []byte(from + "/a")
	if replaceOrigin(in, from, "https://x.example"); string(in) != from+"/a" {
		t.Errorf("the input changed: %q", in)
	}
}

// TestR_BAS_135_ABastionServesThePairsCatalogueOnItsOwnBaseURL: the catalogue of a pair of deployments was rendered on the other
// deployment's base URL; this replica serves every document, summary, version and documentation hit on its own, with the SHA-256 of
// what it serves as the tag and gzip of the same bytes, and leaves the kept document as the main installation wrote it. A replica
// whose own base URL is the one rendered on serves the kept bytes.
func TestR_BAS_135_ABastionServesThePairsCatalogueOnItsOwnBaseURL(t *testing.T) {
	const other, own = "https://pkgs-eu.example.com", "https://bastion.example.com"
	r := newReader()
	r.origin = other
	path := "packages/" + pkgID + "/versions/1.0.0%2Bbuild.1"
	kept := []byte(`{"tarball":"` + other + `/api/npm/acme/npm/ui.tgz","readme":"<a href=\"` + other + `\">home</a> ` + other + `.evil.example"}`)
	r.docs[path] = kept
	keptSum := sha256.Sum256(kept)
	r.pkgs[0].Summary = json.RawMessage(`{"id":"` + pkgID + `","homepage":"` + other + `/p"}`)
	r.pkgs[0].Description = "Docs at " + other + "/docs"
	r.hits[0].ShortDescription = "See " + other
	h := newGUI(t, r, nil)

	rec := do(h, http.MethodGet, "/ui/data/v1/"+path)
	want := `{"tarball":"` + own + `/api/npm/acme/npm/ui.tgz","readme":"<a href=\"` + own + `\">home</a> ` + other + `.evil.example"}`
	sum := sha256.Sum256([]byte(want))
	if rec.Code != 200 || rec.Body.String() != want || rec.Header().Get("ETag") != `"`+hex.EncodeToString(sum[:])+`"` {
		t.Fatalf("%d %s %s", rec.Code, rec.Header().Get("ETag"), rec.Body.String())
	}
	zipped := do(h, http.MethodGet, "/ui/data/v1/"+path, "Accept-Encoding", "gzip")
	zr, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != want || zipped.Header().Get("ETag") != `"`+hex.EncodeToString(sum[:])+`-gzip"` {
		t.Errorf("gzip: %s %s", zipped.Header().Get("ETag"), b)
	}
	if now := sha256.Sum256(r.docs[path]); now != keptSum {
		t.Error("the kept document changed")
	}
	for target, wantIn := range map[string]string{
		"/ui/data/v1/packages":                                 own + "/p",
		"/ui/data/v1/packages/lookup?format=npm&name=@acme/ui": own + "/p",
		"/ui/data/v1/search/documentation?q=web":               "See " + own,
	} {
		if rec := do(h, http.MethodGet, target); rec.Code != 200 || !strings.Contains(rec.Body.String(), wantIn) || strings.Contains(rec.Body.String(), other+`"`) {
			t.Errorf("%s: %d %s", target, rec.Code, rec.Body.String())
		}
	}
	if e, _, _ := h.o.Catalogue.Package(pkgID); e.Description != "Docs at "+own+"/docs" {
		t.Errorf("the description a crawler reads: %q", e.Description)
	}

	r.origin = own
	if rec := do(h, http.MethodGet, "/ui/data/v1/"+path); rec.Body.String() != string(kept) || rec.Header().Get("ETag") != `"`+hex.EncodeToString(keptSum[:])+`"` {
		t.Errorf("the replica the catalogue was rendered on: %s %s", rec.Header().Get("ETag"), rec.Body.String())
	}
	var _ catalogue.Reader = rebase(r, own)
}
