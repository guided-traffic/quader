package debfile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func contentsOf(t *testing.T, deb []byte, pkg string) (*Contents, error) {
	t.Helper()
	return ReadContents(context.Background(), bytes.NewReader(deb), int64(len(deb)), pkg)
}

func gz(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// dataDeb is a package file whose data archive holds the entries, compressed by how.
func dataDeb(t *testing.T, how string, data []byte) []byte {
	t.Helper()
	return arOf(
		arMember{"debian-binary", "2.0\n"},
		arMember{"control.tar.gz", string(compress(t, ".gz", tarOf(t, tarEntry{name: "./control", data: goodControl})))},
		arMember{"data.tar" + how, string(compress(t, how, data))},
	)
}

func wantRefusal(t *testing.T, err error, reason string) {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) || r.Reason != reason {
		t.Fatalf("err = %v, want the refusal %s", err, reason)
	}
}

// lower sets a bound for one test and puts it back after it.
func lower(t *testing.T, set func()) {
	t.Helper()
	saved := bounds
	set()
	t.Cleanup(func() { bounds = saved })
}

// R-DEB-039: the package files dpkg-deb built with xz, Zstandard and gzip are read alike: the changelog, the copyright with its
// licences from the machine-readable fields, and the file list with the link as a name and its target, never followed.
func TestR_DEB_039_ThePackageFilesOfDpkgDebAreRead(t *testing.T) {
	for _, how := range []string{"xz", "zstd", "gzip"} {
		c, err := contentsOf(t, readFixture(t, "quader-docs_1.2.0-1_all."+how+".deb"), "quader-docs")
		if err != nil {
			t.Fatalf("%s: %v", how, err)
		}
		if !strings.HasPrefix(c.Changelog, "quader-docs (1.2.0-1) unstable; urgency=medium") || c.ChangelogFile != "changelog.Debian.gz" ||
			c.ChangelogTruncated || strings.Count(c.Changelog, "\n -- ") != 3 {
			t.Errorf("%s: changelog %q (%s)", how, c.Changelog, c.ChangelogFile)
		}
		if !strings.HasPrefix(c.Copyright, "Format: https://www.debian.org/") || strings.Join(c.Licenses, ",") != "Apache-2.0,GPL-2+" {
			t.Errorf("%s: copyright %q, licences %v", how, c.Copyright, c.Licenses)
		}
		var paths []string
		for _, f := range c.Files {
			p := f.Path
			if f.Kind == "link" {
				p += " -> " + f.Target
			}
			paths = append(paths, p)
		}
		// The archive's order: dpkg-deb writes the symbolic links after every other entry.
		want := "/usr/bin/quader-docs,/usr/share/doc/quader-docs/changelog.Debian.gz,/usr/share/doc/quader-docs/copyright," +
			"/usr/share/man/man1/quader-docs.1.gz,/usr/bin/qd -> quader-docs"
		if strings.Join(paths, ",") != want || c.FileCount != 5 {
			t.Errorf("%s: files %v (%d)", how, paths, c.FileCount)
		}
	}
}

// R-DEB-039: a data archive that inflates past the cap is refused without being inflated further (a bomb is a small file), and so is
// one with more entries than the walk reads, one compressed with bzip2, one that is no tar and one whose reading takes too long.
func TestR_DEB_039_TheReaderStopsAtItsBounds(t *testing.T) {
	lower(t, func() { bounds.inflated, bounds.entries = 1<<20, 100 })
	bomb := tarOf(t, tarEntry{name: "./usr/share/zeros", data: string(make([]byte, 4<<20))})
	for _, how := range []string{".gz", ".xz", ".zst"} {
		deb := dataDeb(t, how, bomb)
		if len(deb) > 64<<10 {
			t.Fatalf("%s: the bomb is %d bytes", how, len(deb))
		}
		_, err := contentsOf(t, deb, "a")
		wantRefusal(t, err, ReasonTooLarge)
	}
	var many []tarEntry
	for i := range 101 {
		many = append(many, tarEntry{name: fmt.Sprintf("./f%d", i)})
	}
	_, err := contentsOf(t, dataDeb(t, ".gz", tarOf(t, many...)), "a")
	wantRefusal(t, err, ReasonTooManyEntries)

	_, err = contentsOf(t, dataDeb(t, ".bz2", []byte("BZh9")), "a")
	wantRefusal(t, err, ReasonUnsupportedCompression)
	_, err = contentsOf(t, dataDeb(t, ".gz", []byte(strings.Repeat("not a tar archive", 100))), "a")
	wantRefusal(t, err, ReasonInvalidArchive)
	_, err = contentsOf(t, []byte("!<arch>\nnot an archive"), "a")
	wantRefusal(t, err, ReasonInvalidArchive)

	lower(t, func() { bounds.timeout = time.Nanosecond })
	_, err = contentsOf(t, dataDeb(t, ".gz", tarOf(t, many[:50]...)), "a")
	wantRefusal(t, err, ReasonTimeout)
}

// R-DEB-039: what is kept is bounded: the newest part of a long changelog ends with a whole entry, the copyright file is cut at a line,
// the file list keeps its first entries and counts all of them; a link in place of the documentation directory is named, not followed.
func TestR_DEB_039_WhatIsKeptIsBounded(t *testing.T) {
	lower(t, func() { bounds.files, bounds.changelog = 3, 1000 })
	var log strings.Builder
	for i := 30; i > 0; i-- {
		fmt.Fprintf(&log, "a (1.%d) unstable; urgency=low\n\n  * Change %d.\n\n -- A <a@example.org>  Thu, 01 Jan 2026 00:00:00 +0000\n\n", i, i)
	}
	copyright := strings.Repeat("Copyright line of the upstream.\n", 3000)
	data := tarOf(t,
		tarEntry{name: "./usr/", typeflag: tar.TypeDir},
		tarEntry{name: "./usr/share/doc/a/changelog.Debian.gz", data: gz(t, log.String())},
		tarEntry{name: "./usr/share/doc/a/copyright", data: copyright},
		tarEntry{name: "./usr/share/doc/a-dev", typeflag: tar.TypeSymlink, link: "a"},
		tarEntry{name: "./dev/null-ish", typeflag: tar.TypeChar},
		tarEntry{name: "./etc/passwd", typeflag: tar.TypeSymlink, link: "/etc/shadow"},
	)
	c, err := contentsOf(t, dataDeb(t, ".gz", data), "a")
	if err != nil {
		t.Fatal(err)
	}
	if !c.ChangelogTruncated || len(c.Changelog) > 1000 || !strings.HasPrefix(c.Changelog, "a (1.30)") ||
		!strings.HasSuffix(c.Changelog, "+0000\n") {
		t.Errorf("changelog (%d bytes, cut %v): %q", len(c.Changelog), c.ChangelogTruncated, c.Changelog)
	}
	if !c.CopyrightTruncated || len(c.Copyright) > MaxCopyright || !strings.HasSuffix(c.Copyright, "upstream.\n") || c.Licenses != nil {
		t.Errorf("copyright (%d bytes, cut %v), licences %v", len(c.Copyright), c.CopyrightTruncated, c.Licenses)
	}
	if len(c.Files) != 3 || c.FileCount != 5 {
		t.Errorf("files %+v of %d", c.Files, c.FileCount)
	}

	dev, err := contentsOf(t, dataDeb(t, ".gz", data), "a-dev")
	if err != nil || dev.DocLink != "a" || dev.Changelog != "" || dev.Copyright != "" {
		t.Errorf("a-dev: %+v, %v", dev, err)
	}
	native, err := contentsOf(t, dataDeb(t, "", tarOf(t, tarEntry{name: "usr/share/doc/a/changelog.gz", data: gz(t, "a (2) unstable; urgency=low\n")})), "a")
	if err != nil || native.ChangelogFile != "changelog.gz" || native.Changelog != "a (2) unstable; urgency=low\n" {
		t.Errorf("native: %+v, %v", native, err)
	}
}

// R-DEB-039: a store that fails is no refusal: the error is the caller's to try again.
func TestR_DEB_039_AStoreThatFailsIsTriedAgain(t *testing.T) {
	deb := readFixture(t, "quader-docs_1.2.0-1_all.gzip.deb")
	failing := errors.New("the store is down")
	_, err := ReadContents(context.Background(), failingAt{bytes.NewReader(deb), failing}, int64(len(deb)), "quader-docs")
	if !errors.Is(err, failing) {
		t.Fatalf("err = %v", err)
	}
}

type failingAt struct {
	r   *bytes.Reader
	err error
}

func (f failingAt) ReadAt(p []byte, off int64) (int, error) {
	if off > 100 {
		return 0, f.err
	}
	return f.r.ReadAt(p, off)
}
