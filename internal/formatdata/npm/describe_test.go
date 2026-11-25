package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/npm/manifest"
	"github.com/guided-traffic/quader/internal/formats/npm/tarball"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

type docBlob []byte

func (b docBlob) Open(context.Context, [32]byte, int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b)), nil
}

// TestR_NPM_050_ACachedVersionIsDescribedByItsEntryOfTheStoredDocument: the job reads the entries of the versions the remote holds and
// their times out of the stored document and nothing else, and the row of an entry is its manifest without the README and the
// upstream's tarball address, with its deprecation (cut to the column's bound), its install script and the facts its dist announces.
// Unit, a document of a few hundred bytes, microseconds.
func TestR_NPM_050_ACachedVersionIsDescribedByItsEntryOfTheStoredDocument(t *testing.T) {
	long := strings.Repeat("d", 2000)
	doc := `{"name":"acme","readme":"# top","versions":{` +
		`"1.0.0":{"name":"acme","version":"1.0.0","description":"Old","dist":{"tarball":"https://up/acme-1.0.0.tgz"}},` +
		`"2.0.0":{"name":"acme","version":"2.0.0","description":"Acme UI","license":"MIT","readme":"# per version","readmeFilename":"README.md",` +
		`"scripts":{"postinstall":"node setup.js"},"deprecated":"` + long + `",` +
		`"dist":{"tarball":"https://up/acme-2.0.0.tgz","integrity":"sha512-x","fileCount":12,"unpackedSize":3456}}},` +
		`"time":{"modified":"2026-11-01T00:00:00.000Z","1.0.0":"2026-01-01T00:00:00.000Z","2.0.0":"2026-02-03T04:05:06.789Z"}}`
	s := &Store{o: Options{Blobs: docBlob(doc)}}
	entries, times, err := s.readEntries(context.Background(), make([]byte, 32), int64(len(doc)), map[string]bool{"2.0.0": true, "3.0.0": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries["2.0.0"] == nil {
		t.Fatalf("entries %v, want the one of 2.0.0", entries)
	}
	if !reflect.DeepEqual(times, map[string]string{"2.0.0": "2026-02-03T04:05:06.789Z"}) {
		t.Errorf("times %v", times)
	}

	row, err := cachedRow(entries["2.0.0"], times["2.0.0"])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(row.Manifest), &m); err != nil {
		t.Fatal(err)
	}
	dist, _ := m["dist"].(map[string]any)
	if m["description"] != "Acme UI" || m["license"] != "MIT" || m["readme"] != nil || m["readmeFilename"] != nil || dist["tarball"] != nil || dist["integrity"] != "sha512-x" {
		t.Errorf("manifest %s", row.Manifest)
	}
	if len(row.Deprecated) != maxDeprecated || !row.HasInstallScript || row.FileCount != 12 || row.UnpackedSize != 3456 || row.Scanned {
		t.Errorf("row: deprecated %d bytes, install script %v, files %d, unpacked %d, scanned %v",
			len(row.Deprecated), row.HasInstallScript, row.FileCount, row.UnpackedSize, row.Scanned)
	}
	if row.PublishedAt != "2026-02-03T04:05:06.789Z" {
		t.Errorf("published %q", row.PublishedAt)
	}
	if _, err := cachedRow(json.RawMessage(`"not an object"`), ""); err == nil {
		t.Error("an entry that is no object made a row")
	}
}

// TestR_NPM_050_AVersionIsDescribedOnceWhateverIsReadable: a cached version gets a row in one run whatever its store holds: the entry
// of the document, else the package.json of the tarball the reader accepted, else its name and number; the tarball's facts when it was
// read. A blob store that fails is the one error that is tried again; a refusal or a document that is no JSON is not. Unit, microseconds.
func TestR_NPM_050_AVersionIsDescribedOnceWhateverIsReadable(t *testing.T) {
	res := &tarball.Result{
		Manifest:  manifest.Manifest{"name": "acme", "version": "2.0.0", "description": "From the tarball", "scripts": map[string]any{"install": "node-gyp rebuild"}},
		FileCount: 7, UnpackedSize: 900, Readme: "# acme\n", ReadmeFilename: "README.md",
	}
	entry := json.RawMessage(`{"name":"acme","version":"2.0.0","description":"From the document","deprecated":"Use 3"}`)
	manifestOf := func(row q.NPMPutCachedVersionParams) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(row.Manifest), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	for _, c := range []struct {
		name     string
		entry    json.RawMessage
		res      *tarball.Result
		want     string
		scanned  bool
		readme   string
		deprecat string
	}{
		{"the entry and the tarball", entry, res, "From the document", true, "README.md", "Use 3"},
		{"the entry, the tarball refused", entry, nil, "From the document", false, "", "Use 3"},
		{"no entry: the tarball's package.json", nil, res, "From the tarball", true, "README.md", ""},
		{"an entry that is no object: the tarball's package.json", json.RawMessage(`"x"`), res, "From the tarball", true, "README.md", ""},
		{"neither: name and number", nil, nil, "", false, "", ""},
	} {
		row, err := describedRow("acme", "2.0.0", c.entry, "", c.res)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		m := manifestOf(row)
		if m["name"] != "acme" || m["version"] != "2.0.0" || (c.want != "" && m["description"] != c.want) || (c.want == "" && m["description"] != nil) {
			t.Errorf("%s: manifest %s", c.name, row.Manifest)
		}
		if row.Scanned != c.scanned || row.ReadmeFilename != c.readme || row.Deprecated != c.deprecat || (c.scanned && (row.FileCount != 7 || !row.HasInstallScript)) {
			t.Errorf("%s: row %+v", c.name, row)
		}
	}
	for _, c := range []struct {
		err  error
		want bool
	}{
		{errs.E(errs.Invalid, errs.CodeNPMBadPublish, "refused"), true},
		{undecodable(&json.SyntaxError{}), true},
		{errs.E(errs.Unavailable, errs.CodeObjectStoreUnavail, "down"), false},
		{undecodable(io.ErrClosedPipe), false},
	} {
		if got := permanent(c.err); got != c.want {
			t.Errorf("permanent(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// TestR_NPM_050_ARemotesVersionShowsTheTagsOfItsStoredDocument: the dist-tags are read up to the member versions, which the registry
// writes after them; a document that holds them later shows none rather than reading every version. Unit, microseconds.
func TestR_NPM_050_ARemotesVersionShowsTheTagsOfItsStoredDocument(t *testing.T) {
	got := tagsOf(json.NewDecoder(strings.NewReader(`{"_id":"acme","name":"acme","dist-tags":{"latest":"2.0.0","next":3},"versions":{}}`)))
	if !reflect.DeepEqual(got, map[string]string{"latest": "2.0.0"}) {
		t.Errorf("tags %v", got)
	}
	if got := tagsOf(json.NewDecoder(strings.NewReader(`{"name":"acme","versions":{},"dist-tags":{"latest":"2.0.0"}}`))); got != nil {
		t.Errorf("tags after the versions %v, want none", got)
	}
}
