package chart

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
)

type entry struct {
	name, body, link string
	flag             byte
}

// archive writes a chart archive of a few hundred bytes, as `helm package` lays one out: every file below the chart's directory.
func archive(t *testing.T, entries ...entry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.flag != 0 {
			h.Typeflag, h.Linkname, h.Size = e.flag, e.link, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

// R-OCI-042: the README and values.yaml at the chart's root; a subchart's README and a link are not the chart's.
func TestR_OCI_042_ReadsTheReadmeAndTheValuesAtTheChartsRoot(t *testing.T) {
	got := ReadFiles(context.Background(), archive(t,
		entry{name: "web/Chart.yaml", body: "apiVersion: v2\nname: web\n"},
		entry{name: "web/charts/redis/README.md", body: "# not this one"},
		entry{name: "web/templates/README.md", body: "# nor this one"},
		entry{name: "web/README.txt", body: "plain"},
		entry{name: "web/README.md", body: "# web\n\nThe chart."},
		entry{name: "web/values.yaml", body: "replicaCount: 2\n"},
		entry{name: "web/LICENSE", flag: tar.TypeSymlink, link: "../README.md"},
	))
	if got.Unreadable != "" || got.ReadmeName != "README.md" || got.Readme != "# web\n\nThe chart." || !got.HasValues || got.Values != "replicaCount: 2\n" {
		t.Fatalf("%+v", got)
	}
}

// R-OCI-042: a link entry is never followed, even when its name is the README's; a chart without values has none; a file over the
// bound is cut and marked; a link that leaves the archive refuses the whole read.
func TestR_OCI_042_BoundsAndLinks(t *testing.T) {
	linked := ReadFiles(context.Background(), archive(t,
		entry{name: "web/README.md", flag: tar.TypeSymlink, link: "Chart.yaml"},
		entry{name: "web/Chart.yaml", body: "name: web"},
	))
	if linked.Readme != "" || linked.HasValues || linked.Unreadable != "" {
		t.Errorf("a link was read or a chart without values has some: %+v", linked)
	}
	big := ReadFiles(context.Background(), archive(t, entry{name: "web/values.yaml", body: strings.Repeat("a: b\n", MaxFileBytes/4)}))
	if !big.ValuesCut || len(big.Values) != MaxFileBytes {
		t.Errorf("an oversized file: cut %v, %d bytes", big.ValuesCut, len(big.Values))
	}
	escaping := ReadFiles(context.Background(), archive(t, entry{name: "web/README.md", flag: tar.TypeSymlink, link: "../../etc/passwd"}))
	if escaping.Unreadable != rows.UnreadableInvalid {
		t.Errorf("a link out of the archive: %+v", escaping)
	}
	if got := ReadFiles(context.Background(), bytes.NewReader([]byte("not a gzip"))); got.Unreadable == "" {
		t.Errorf("no archive: %+v", got)
	}
}
