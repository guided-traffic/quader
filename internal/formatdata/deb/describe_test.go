package deb

import (
	"errors"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/deb/control"
	"github.com/guided-traffic/quader/internal/formats/deb/debfile"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TestR_DEB_037_ACachedFileIsDescribedOnceWhateverIsReadable: a cached file gets its row in one run: the paragraph of its control
// archive, or, when the reader refuses the file (or its object is lost), the paragraph its parsed index row gives, with the architecture
// of the file name when no index row is left; a blob store that fails is the one error that is tried again. Unit, microseconds.
func TestR_DEB_037_ACachedFileIsDescribedOnceWhateverIsReadable(t *testing.T) {
	f := q.DebDescribeFilesRow{
		Path: "pool/main/h/hello/hello_2.10-3_arm64.deb", Package: "hello", Version: "2.10-3", Architecture: "amd64",
		Description: "prints\na greeting",
	}
	para, err := control.Parse([]byte("Package: hello\nVersion: 2.10-3\nArchitecture: amd64\nMaintainer: A <a@example.invalid>\nDescription: prints\n"))
	if err != nil {
		t.Fatal(err)
	}
	row, err := cachedParagraph(f, &debfile.Package{Name: "hello", Version: "2.10-3", Architecture: "amd64", Source: "hello", Control: para}, nil)
	if err != nil || !strings.Contains(row.Control, "Maintainer: A") {
		t.Errorf("the control archive's paragraph: %+v, %v", row, err)
	}

	refused := errs.E(errs.Unprocessable, errs.CodeDebInvalidArchive, "no control archive")
	row, err = cachedParagraph(f, nil, refused)
	want := "Package: hello\nVersion: 2.10-3\nArchitecture: amd64\nDescription: prints a greeting\n"
	if err != nil || row.Control != want || row.Architecture != "amd64" || row.Source != "hello" {
		t.Errorf("a refused file: %q, %+v, %v", row.Control, row, err)
	}
	if _, err := control.Parse([]byte(row.Control)); err != nil {
		t.Errorf("the paragraph made of the index row does not parse: %v", err)
	}

	f.Architecture, f.Description = "", ""
	if row, err = cachedParagraph(f, nil, errNoBlob); err != nil || row.Architecture != "arm64" || strings.Contains(row.Control, "Description") {
		t.Errorf("no index row and no blob: %q, %v", row.Control, err)
	}

	down := errors.New("connection reset")
	if _, err := cachedParagraph(f, nil, down); !errors.Is(err, down) {
		t.Errorf("a blob store that fails: %v, want it returned", err)
	}
}
