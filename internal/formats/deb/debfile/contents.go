package debfile

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The reading of a package's data archive (R-DEB-039, revising R-DEB-007): the documents Debian policy puts into every package, its
// changelog and its copyright file below /usr/share/doc/<package>/, and the list of the files it installs. The archive is untrusted: it
// is inflated as a stream and never extracted, no entry is written anywhere, a link or a device is a name in the list and never
// followed or opened, and the walk stops at the first bound it crosses. Only the changelog and the copyright file are read, and only
// their first bytes; every other entry's content is skipped by the tar reader.

// The bounds of the reading of a data archive (R-DEB-039).
const (
	// MaxInflated is the bytes the decompressor may hand out in all: what a pass that keeps nothing may inflate (R-ARCH-028's
	// StreamBytes).
	MaxInflated = 2 << 30
	// MaxEntries is the entries the walk reads before it stops.
	MaxEntries = 200_000
	// MaxDictionary is the dictionary or window a compressed stream may declare: the decoder holds that many bytes.
	MaxDictionary = 64 << 20
	// ReadTimeout bounds the reading of one package file.
	ReadTimeout = 2 * time.Minute
	// MaxChangelog is the newest part of the changelog that is kept.
	MaxChangelog = 64 << 10
	// MaxCopyright is the part of the copyright file that is kept.
	MaxCopyright = 64 << 10
	// MaxFiles is the number of entries of the file list that are kept, MaxFileNames the bytes of their names.
	MaxFiles     = 10_000
	MaxFileNames = 1 << 20
	// readBuffer is the size of one ranged read of the stored file.
	readBuffer = 4 << 20
)

// bounds are the bounds a reading applies: the constants above, which a test lowers to show each with bytes and not gigabytes.
var bounds = struct {
	inflated   int64
	entries    int
	files      int
	timeout    time.Duration
	changelog  int
	dictionary int64
}{MaxInflated, MaxEntries, MaxFiles, ReadTimeout, MaxChangelog, MaxDictionary}

// The reasons a data archive is not read (the column deb_contents.reason).
const (
	ReasonUnsupportedCompression = "unsupported_compression"
	ReasonInvalidArchive         = "invalid_archive"
	ReasonTooLarge               = "too_large"
	ReasonTooManyEntries         = "too_many_entries"
	ReasonTimeout                = "timeout"
	ReasonNoContent              = "no_content"
)

// Refusal is a data archive the reader does not read, with the reason and a sentence for a person who may read the repository's
// configuration.
type Refusal struct {
	Reason string
	Detail string
}

func (r *Refusal) Error() string { return "deb: the data archive is not read: " + r.Detail }

func refuse(reason, detail string) *Refusal { return &Refusal{Reason: reason, Detail: detail} }

// File is one entry of the file list.
type File struct {
	// Path is the absolute path the package installs, as the archive names it ("./usr/bin/hello" is "/usr/bin/hello").
	Path string `json:"p"`
	// Size is the declared size of a regular file; 0 for any other entry.
	Size int64 `json:"s,omitempty"`
	// Kind is "" for a regular file, "link" for a symbolic link, "hardlink" for a hard link and "special" for a device or a pipe.
	Kind string `json:"k,omitempty"`
	// Target is what a link points at, as the archive spells it: a name, never followed.
	Target string `json:"t,omitempty"`
}

// Contents is what the reading of a data archive keeps.
type Contents struct {
	// Changelog is the newest part of the changelog, at most MaxChangelog bytes ending at an entry or a line; ChangelogFile is its
	// name below /usr/share/doc/<package>/ ("changelog.Debian.gz" or, for a native package, "changelog.gz"). Empty when there is none.
	Changelog          string
	ChangelogFile      string
	ChangelogTruncated bool
	// Copyright is the copyright file, at most MaxCopyright bytes; Licenses are the short names of its License fields when it is in the
	// machine-readable format (DEP-5), in the order they first appear.
	Copyright          string
	CopyrightTruncated bool
	Licenses           []string
	// DocLink is the target of /usr/share/doc/<package> when the package installs it as a symbolic link to another package's
	// directory, as packages of one source often do; the changelog and the copyright are then that package's.
	DocLink string
	// Files are the first MaxFiles entries that are not directories, in the archive's order; FileCount is every such entry.
	Files     []File
	FileCount int
}

// ReadContents reads the data archive of the package file ra of the given size, the binary package pkg. A refusal is a *Refusal; an
// error of ra (the blob store failed) or of ctx is returned as it is, for the caller to try again. The deadline of ReadTimeout is applied
// here; a read cut by it is the refusal timeout.
func ReadContents(ctx context.Context, ra io.ReaderAt, size int64, pkg string) (*Contents, error) {
	rctx, cancel := context.WithTimeout(ctx, bounds.timeout)
	defer cancel()
	src := &source{ra: ra}
	c, err := readContents(rctx, src, size, pkg)
	if err == nil {
		return c, nil
	}
	var r *Refusal
	switch {
	case src.err != nil:
		return nil, src.err
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.As(err, &r):
		return nil, r
	case rctx.Err() != nil:
		return nil, refuse(ReasonTimeout, fmt.Sprintf("Reading the data archive took longer than %s.", bounds.timeout))
	case untrusted.IsTooLarge(err):
		return nil, refuse(ReasonTooLarge, "The data archive crosses a bound of the reading: "+message(err))
	}
	return nil, refuse(ReasonInvalidArchive, "The data archive is not a valid tar archive in its compression.")
}

// source is the stored file; it keeps the first error a read of it returned, which is the store's and not the archive's.
type source struct {
	ra  io.ReaderAt
	err error
}

func (s *source) ReadAt(p []byte, off int64) (int, error) {
	n, err := s.ra.ReadAt(p, off)
	if err != nil && !errors.Is(err, io.EOF) && s.err == nil {
		s.err = err
	}
	return n, err
}

func readContents(ctx context.Context, ra io.ReaderAt, size int64, pkg string) (*Contents, error) {
	ar, err := untrusted.NewAr(ra, size, untrusted.Limits{})
	if err != nil {
		return nil, refuse(ReasonInvalidArchive, "The package file is not an ar archive Quader reads.")
	}
	for {
		m, err := ar.Next()
		if err != nil {
			return nil, refuse(ReasonInvalidArchive, "The package file has no data archive.")
		}
		ext, ok := strings.CutPrefix(m.Name, "data.tar")
		if !ok {
			continue
		}
		body, err := ar.Open()
		if err != nil {
			return nil, err
		}
		in, err := inflate(bufio.NewReaderSize(body, readBuffer), ext)
		if err != nil {
			return nil, err
		}
		return walk(untrusted.WithContext(ctx, in), pkg)
	}
}

func message(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	return s
}

// inflate returns the decompressed stream of the data archive: none, gzip, xz or Zstandard, each capped at MaxInflated bytes and each
// with a dictionary of at most MaxDictionary. bzip2 and lzma, which dpkg-deb still reads and no longer writes by default, are not read.
func inflate(r io.Reader, ext string) (io.Reader, error) {
	bad := func(what string) error {
		return refuse(ReasonInvalidArchive, "The data archive is not valid "+what+".")
	}
	switch ext {
	case "":
		return untrusted.Limit(r, bounds.inflated), nil
	case ".gz":
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, bad("gzip")
		}
		zr.Multistream(false)
		return untrusted.Limit(zr, bounds.inflated), nil
	case ".xz":
		xr, err := untrusted.NewXZBlocksReader(r, bounds.inflated, bounds.dictionary)
		if err != nil {
			if untrusted.IsTooLarge(err) {
				return nil, refuse(ReasonTooLarge, "The data archive crosses a bound of the reading: "+message(err))
			}
			return nil, bad("xz")
		}
		return xr, nil
	case ".zst":
		zr, err := untrusted.NewZstdReaderWindow(r, bounds.inflated, bounds.dictionary)
		if err != nil {
			return nil, bad("Zstandard")
		}
		return zr, nil
	}
	return nil, refuse(ReasonUnsupportedCompression, "The data archive is compressed with "+strings.TrimPrefix(ext, ".")+
		", which Quader does not read; gzip, xz, Zstandard and none are read.")
}

// walk reads the tar stream: every header counts against MaxEntries, the changelog and the copyright of pkg are read in part, the rest
// is skipped.
func walk(r io.Reader, pkg string) (*Contents, error) {
	tr := tar.NewReader(r)
	doc := "usr/share/doc/" + pkg
	c := &Contents{}
	var debianChangelog, nativeChangelog string
	var debTrunc, natTrunc bool
	names, entries := 0, 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if entries++; entries > bounds.entries {
			return nil, refuse(ReasonTooManyEntries, fmt.Sprintf("The data archive holds more than %d entries.", bounds.entries))
		}
		name := clean(h.Name)
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA: //nolint:staticcheck // TypeRegA is what old writers set for a regular file
			switch name {
			case doc + "/changelog.Debian.gz":
				debianChangelog, debTrunc = readChangelog(tr)
			case doc + "/changelog.gz":
				nativeChangelog, natTrunc = readChangelog(tr)
			case doc + "/copyright":
				c.Copyright, c.CopyrightTruncated = readText(tr, MaxCopyright)
			}
		case tar.TypeSymlink:
			if name == doc {
				c.DocLink = text(h.Linkname, 256)
			}
		}
		c.FileCount++
		if len(c.Files) < bounds.files && names+len(name) <= MaxFileNames {
			names += len(name)
			c.Files = append(c.Files, fileOf(h, name))
		}
	}
	switch {
	case debianChangelog != "":
		c.Changelog, c.ChangelogFile, c.ChangelogTruncated = debianChangelog, "changelog.Debian.gz", debTrunc
	case nativeChangelog != "":
		c.Changelog, c.ChangelogFile, c.ChangelogTruncated = nativeChangelog, "changelog.gz", natTrunc
	}
	c.Licenses = Licenses(c.Copyright)
	return c, nil
}

// clean is an entry's name as a path below the root without "./" or a leading slash; a name that is no valid UTF-8 is shown with
// replacement characters.
func clean(name string) string {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	return text(name, untrusted.MaxNameBytes)
}

func fileOf(h *tar.Header, name string) File {
	f := File{Path: "/" + name}
	switch h.Typeflag {
	case tar.TypeReg, tar.TypeRegA: //nolint:staticcheck // see walk
		f.Size = max(h.Size, 0)
	case tar.TypeSymlink:
		f.Kind, f.Target = "link", text(h.Linkname, untrusted.MaxNameBytes)
	case tar.TypeLink:
		f.Kind, f.Target = "hardlink", "/"+clean(h.Linkname)
	default:
		f.Kind = "special"
	}
	return f
}

// text is s as valid UTF-8 without control characters other than a tab and a line feed, at most max bytes: what a screen shows and what
// a text column of PostgreSQL holds (no NUL).
func text(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' {
			return r
		}
		if r < 0x20 || r == 0x7F || (r >= 0x80 && r < 0xA0) {
			return -1
		}
		return r
	}, s)
	if len(s) > maxBytes {
		cut := maxBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

// readText reads at most maxBytes of the current entry and reports whether it holds more.
func readText(r io.Reader, maxBytes int) (string, bool) {
	b, err := io.ReadAll(io.LimitReader(r, int64(maxBytes)+1))
	if err != nil {
		return "", false
	}
	truncated := len(b) > maxBytes
	if truncated {
		b = b[:maxBytes]
		if i := bytes.LastIndexByte(b, '\n'); i > 0 {
			b = b[:i+1]
		}
	}
	return text(strings.ReplaceAll(string(b), "\r\n", "\n"), maxBytes), truncated
}

// trailer is the line that ends an entry of a Debian changelog (deb-changelog(5)).
var trailer = regexp.MustCompile(`(?m)^ -- [^\n]*\n`)

// readChangelog inflates the gzip-compressed changelog of the current entry and keeps its first MaxChangelog bytes, the newest entries,
// cut after the last entry that fits whole (else at a line). A changelog that is no valid gzip is none. The inflation stops after the
// bytes it keeps, so a compressed bomb costs no more than those.
func readChangelog(r io.Reader) (string, bool) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return "", false
	}
	b, err := io.ReadAll(io.LimitReader(zr, int64(bounds.changelog)+1))
	if err != nil && len(b) <= bounds.changelog {
		return "", false
	}
	if len(b) <= bounds.changelog {
		return text(string(b), bounds.changelog), false
	}
	b = b[:bounds.changelog]
	if loc := trailer.FindAllIndex(b, -1); len(loc) > 0 {
		b = b[:loc[len(loc)-1][1]]
	} else if i := bytes.LastIndexByte(b, '\n'); i > 0 {
		b = b[:i+1]
	}
	return text(string(b), bounds.changelog), true
}

// Licenses reads the short names of the License fields of a copyright file in the machine-readable format (DEP-5): the file's first
// paragraph names the format with a Format field, and every Files or stand-alone License paragraph has a License field whose first line
// is the name. A file in any other form names no licence: its prose is shown as it is. Names are kept in the order they first appear, at
// most 32.
func Licenses(copyright string) []string {
	if !strings.HasPrefix(copyright, "Format:") && !strings.HasPrefix(copyright, "Format-Specification:") {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(copyright, "\n") {
		v, ok := strings.CutPrefix(line, "License:")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" || len(v) > 200 || seen[v] {
			continue
		}
		seen[v] = true
		if out = append(out, v); len(out) == 32 {
			break
		}
	}
	return out
}
