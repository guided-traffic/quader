package untrusted_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

func symlink(t *testing.T, tw *tar.Writer, name, target string, typ byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Linkname: target, Mode: 0o777, Typeflag: typ}); err != nil {
		t.Fatal(err)
	}
}

// readAll walks an archive with the option and returns the entries and what Read handed out for each.
func readAll(t *testing.T, archive []byte, l untrusted.Limits) ([]untrusted.Entry, map[string]string, error) {
	t.Helper()
	tr := untrusted.NewTarGz(context.Background(), bytes.NewReader(archive), l, untrusted.ContainedSymlinks())
	var out []untrusted.Entry
	content := map[string]string{}
	for {
		e, err := tr.Next()
		if errors.Is(err, io.EOF) && !untrusted.IsRefused(err) {
			return out, content, nil
		}
		if err != nil {
			return out, content, err
		}
		out = append(out, e)
		b, _ := io.ReadAll(tr)
		content[e.Name] = string(b)
	}
}

// Q-ANS-91, R-ARCH-028: with ContainedSymlinks a symbolic link whose target stays inside the archive is handed out with its resolved
// target and counted against the entries, and nothing is read through it; without the option, and for a hard link always, the link is
// refused.
func TestQ_ANS_91_AContainedSymlinkIsAdmittedCountedAndNeverFollowed(t *testing.T) {
	archive := tarGz(t, func(tw *tar.Writer) {
		file(t, tw, "tests/utils/shippable/remote.sh", "#!/bin/sh\necho remote\n")
		symlink(t, tw, "tests/utils/shippable/aix.sh", "remote.sh", tar.TypeSymlink)
		symlink(t, tw, "docs/up.md", "../tests/utils/shippable/remote.sh", tar.TypeSymlink)
	})
	entries, content, err := readAll(t, archive, untrusted.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d entries", len(entries))
	}
	aix, up := entries[1], entries[2]
	if !aix.Link || aix.Target != "tests/utils/shippable/remote.sh" || aix.Size != 0 || aix.Dir {
		t.Errorf("the link beside its target: %+v", aix)
	}
	if !up.Link || up.Target != "tests/utils/shippable/remote.sh" {
		t.Errorf("the link one directory up: %+v", up)
	}
	if content["tests/utils/shippable/aix.sh"] != "" || content["docs/up.md"] != "" {
		t.Errorf("a read of a link handed out content: %q", content)
	}
	if got, ok := untrusted.ResolveLink(map[string]string{aix.Name: aix.Target}, aix.Name); !ok || got != "tests/utils/shippable/remote.sh" {
		t.Errorf("ResolveLink = %q %v", got, ok)
	}
	if _, ok := untrusted.ResolveLink(map[string]string{"a": "b", "b": "a"}, "a"); ok {
		t.Error("a loop of links resolved")
	}

	// A link counts against the entries like any entry.
	l := untrusted.DefaultLimits()
	l.Entries = 2
	if _, _, err := readAll(t, archive, l); !untrusted.IsTooLarge(err) {
		t.Errorf("three entries, one of them a link, under a bound of two: %v", err)
	}

	// Without the option the same archive is refused at its first link.
	tr := untrusted.NewTarGz(context.Background(), bytes.NewReader(archive), untrusted.DefaultLimits())
	_, _ = tr.Next()
	if _, err := tr.Next(); !untrusted.IsRefused(err) || !strings.Contains(err.Error(), "aix.sh") {
		t.Errorf("without the option: %v", err)
	}
}

// Q-ANS-91: a link with an absolute target, one that leaves the archive, one without a target and a hard link are refused with a
// sentence that names the entry.
func TestQ_ANS_91_ALinkThatLeavesTheArchiveIsRefusedByName(t *testing.T) {
	for name, c := range map[string]struct {
		entry, target string
		typ           byte
		says          string
	}{
		"an absolute target":       {"plugins/x.py", "/etc/passwd", tar.TypeSymlink, "absolute path"},
		"a target above the root":  {"plugins/x.py", "../../etc/passwd", tar.TypeSymlink, "leaves the archive"},
		"one level above the root": {"x.py", "../x.py", tar.TypeSymlink, "leaves the archive"},
		"an empty target":          {"plugins/x.py", "", tar.TypeSymlink, "without a usable target"},
		"a hard link":              {"plugins/x.py", "README.md", tar.TypeLink, "is a hard link"},
	} {
		t.Run(name, func(t *testing.T) {
			archive := tarGz(t, func(tw *tar.Writer) {
				file(t, tw, "README.md", "# r\n")
				symlink(t, tw, c.entry, c.target, c.typ)
			})
			_, _, err := readAll(t, archive, untrusted.DefaultLimits())
			if !untrusted.IsRefused(err) || !strings.Contains(err.Error(), c.entry) || !strings.Contains(err.Error(), c.says) {
				t.Errorf("error = %v, want a refusal naming %q that says %q", err, c.entry, c.says)
			}
		})
	}
}
