package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
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
