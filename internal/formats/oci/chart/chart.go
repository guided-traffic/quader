// Package chart reads the documentation a Helm chart's content layer carries (formats/oci.md R-OCI-042): the README and values.yaml at
// the chart's root, out of the gzip-compressed tar `helm package` writes, through the bounded reader of untrusted archives
// (internal/formats/untrusted, R-ARCH-028). Nothing is unpacked to disk, a link entry is never followed (it is skipped, and one that
// points outside the archive refuses the whole read), each file is cut at its bound and the walk stops once both are found. It is a leaf
// the format's view calls with a stored blob; it reads no rows and writes nothing.
package chart

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The bounds of the walk.
const (
	// MaxFileBytes bounds the README and values.yaml each: the rest is cut, as an npm or NuGet README is (64 KiB).
	MaxFileBytes = 64 << 10
	// maxEntries and maxDeclared bound the archive the walk passes: a chart is a few hundred files of a few MiB.
	maxEntries  = 10_000
	maxDeclared = 256 << 20
	maxRead     = 4 * MaxFileBytes
)

// readmeRank orders the names of a README at the chart's root as Helm's `helm show readme` finds them: README.md first, then the other
// spellings; 0 is no README.
func readmeRank(base string) int {
	switch strings.ToLower(base) {
	case "readme.md":
		return 3
	case "readme.markdown", "readme.txt":
		return 2
	case "readme":
		return 1
	}
	return 0
}

// ReadFiles reads the README and values.yaml at the root of the chart archive in r. A file below the root (a subchart's
// charts/<dep>/README.md, a templates/README.md) is not the chart's. An archive the reader refuses or that crosses a bound is reported
// as Unreadable with what was found before it.
func ReadFiles(ctx context.Context, r io.Reader) rows.ChartFiles {
	var out rows.ChartFiles
	lim := untrusted.DefaultLimits().Tighten(untrusted.Limits{Entries: maxEntries, StreamBytes: maxDeclared, UncompressedBytes: maxRead})
	tr := untrusted.NewTarGz(ctx, r, lim, untrusted.ContainedSymlinks())
	rank := 0
	root := ""
	for {
		e, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			out.Unreadable = unreadable(err)
			return out
		}
		dir, base, ok := strings.Cut(strings.TrimPrefix(e.Name, "./"), "/")
		if !ok || e.Dir || e.Link || base == "" || strings.Contains(base, "/") {
			continue
		}
		// Every file of a chart is below one directory, the chart's name; a second top directory is no root of it.
		if root == "" {
			root = dir
		} else if dir != root {
			continue
		}
		switch {
		case base == "values.yaml" && !out.HasValues:
			text, cut, err := readCapped(tr)
			if err != nil {
				out.Unreadable = unreadable(err)
				return out
			}
			out.Values, out.ValuesCut, out.HasValues = text, cut, true
		case readmeRank(base) > rank:
			text, cut, err := readCapped(tr)
			if err != nil {
				out.Unreadable = unreadable(err)
				return out
			}
			out.Readme, out.ReadmeName, out.ReadmeCut, rank = text, base, cut, readmeRank(base)
		}
		if out.HasValues && rank == 3 {
			return out
		}
	}
}

// readCapped reads the current entry up to MaxFileBytes; cut says there was more. A NUL, which no text holds, ends the text there.
func readCapped(tr *untrusted.TarReader) (string, bool, error) {
	data, err := io.ReadAll(io.LimitReader(tr, MaxFileBytes+1))
	if err != nil {
		return "", false, err
	}
	cut := len(data) > MaxFileBytes
	if cut {
		data = data[:MaxFileBytes]
	}
	text := strings.ToValidUTF8(string(data), "�")
	if i := strings.IndexByte(text, 0); i >= 0 {
		text, cut = text[:i], true
	}
	return text, cut, nil
}

func unreadable(err error) string {
	if untrusted.IsTooLarge(err) {
		return rows.UnreadableTooLarge
	}
	return rows.UnreadableInvalid
}
