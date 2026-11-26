package deb

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/deb/debfile"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TestR_DEB_039_TheFileListIsAnArrayAtEveryCount: the row the job deb-contents writes holds the file list as a JSON array, which the
// table's check demands, whatever the package installs: nothing but a directory (a metapackage), exactly the 10,000 entries that are
// kept, or one more, of which the first 10,000 are kept and every one is counted. The package files are generated here, names of a few
// bytes in an uncompressed data archive (about 5 MiB of tar headers at the largest). Unit, about 0.1 s.
func TestR_DEB_039_TheFileListIsAnArrayAtEveryCount(t *testing.T) {
	for _, c := range []struct{ entries, kept int }{{0, 0}, {debfile.MaxFiles, debfile.MaxFiles}, {debfile.MaxFiles + 1, debfile.MaxFiles}} {
		t.Run(fmt.Sprint(c.entries), func(t *testing.T) {
			deb := packageOf(t, c.entries)
			contents, err := debfile.ReadContents(context.Background(), bytes.NewReader(deb), int64(len(deb)), "p")
			if err != nil {
				t.Fatal(err)
			}
			var row q.DebPutContentsParams
			if err := fill(&row, contents); err != nil {
				t.Fatal(err)
			}
			var list []map[string]any
			if err := json.Unmarshal(row.Files, &list); err != nil || list == nil {
				t.Fatalf("files %.40s is no JSON array: %v", row.Files, err)
			}
			if len(list) != c.kept || row.FileCount != int32(c.entries) { //nolint:gosec // at most MaxFiles + 1
				t.Errorf("%d entries kept, %d counted; want %d of %d", len(list), row.FileCount, c.kept, c.entries)
			}
		})
	}
}

// packageOf is a package file "p" whose uncompressed data archive holds a directory and n empty regular files of short names.
func packageOf(t *testing.T, n int) []byte {
	t.Helper()
	var data bytes.Buffer
	tw := tar.NewWriter(&data)
	write := func(h *tar.Header) {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	write(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755})
	for i := range n {
		write(&tar.Header{Name: fmt.Sprintf("./f%05d", i), Typeflag: tar.TypeReg, Mode: 0o644})
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var ar bytes.Buffer
	ar.WriteString("!<arch>\n")
	for _, m := range []struct {
		name string
		data []byte
	}{{"debian-binary", []byte("2.0\n")}, {"data.tar", data.Bytes()}} {
		fmt.Fprintf(&ar, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", m.name, 0, 0, 0, "100644", len(m.data))
		ar.Write(m.data)
		if len(m.data)%2 == 1 {
			ar.WriteByte('\n')
		}
	}
	return ar.Bytes()
}
