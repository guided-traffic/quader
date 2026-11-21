package cvd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// Member is one file of a container's archive (R-CLAMAV-003): its name, size and SHA-256 as the container's own index (the member
// <db>.info) lists them, and the number of its lines that are neither empty nor a comment.
type Member struct {
	Name   string
	Size   int64
	SHA256 string
	// Lines is the number of the member's lines that are neither empty nor a comment (a line starting with #). One line is one
	// signature for most kinds of signature file, not for every one.
	Lines int
}

// Bounds are the bounds of a read of the members (R-MAL-033): the entries of the archive and the bytes it decompresses to.
type Bounds struct {
	Entries int
	Bytes   int64
	// Deadline bounds the whole read.
	Deadline time.Duration
}

// DefaultBounds are the bounds of R-MAL-033: 1,000 entries and 2 GiB decompressed.
func DefaultBounds() Bounds { return Bounds{Entries: 1000, Bytes: 2 << 30, Deadline: 5 * time.Minute} }

// ReadMembers reads the member list of a container from its archive: the bytes after the 512 of the header, a gzip-compressed tar. It
// reads every member once, for its line count and, where the index does not list it, its size and digest; the list is sorted by name.
// read is false and the list nil when the archive crosses a bound: the container is stored all the same (R-CLAMAV-003). An archive
// that is no tar.gz, or one the bounded reader refuses (a link, a name twice), is an error.
//
// The caller reads it only after the container's signature verified, or for a container of an installation's own repository
// (Q-CLAMAV-5): the bytes are then the publisher's, or the administrator's.
func ReadMembers(ctx context.Context, archive io.Reader, b Bounds) (members []Member, read bool, err error) {
	if b.Entries <= 0 || b.Bytes <= 0 {
		b = DefaultBounds()
	}
	l := untrusted.Limits{Entries: b.Entries, StreamBytes: b.Bytes, UncompressedBytes: 1 << 20, ManifestEntryBytes: 1 << 20, Deadline: b.Deadline}
	ctx, cancel := untrusted.DefaultLimits().Tighten(l).Context(ctx)
	defer cancel()
	tr := untrusted.NewTarGz(ctx, archive, l)
	walked := map[string]Member{}
	var index []Member
	for {
		e, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if untrusted.IsTooLarge(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if e.Dir {
			continue
		}
		w := newLineCounter(strings.HasSuffix(e.Name, ".info") && index == nil)
		n, err := tr.Stream(w)
		if untrusted.IsTooLarge(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		m := Member{Name: e.Name, Size: n, SHA256: hex.EncodeToString(w.h.Sum(nil)), Lines: w.done()}
		walked[e.Name] = m
		if w.index {
			index = w.info
		}
	}
	if index == nil {
		for _, m := range walked {
			members = append(members, m)
		}
	} else {
		for _, m := range index {
			if got, ok := walked[m.Name]; ok {
				m.Lines = got.Lines
			}
			members = append(members, m)
		}
		if info := infoMember(walked); info != nil {
			members = append(members, *info)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	return members, true, nil
}

// infoMember is the index itself, which the index does not list.
func infoMember(walked map[string]Member) *Member {
	for name, m := range walked {
		if strings.HasSuffix(name, ".info") {
			return &m
		}
	}
	return nil
}

// lineCounter hashes a member, counts its lines that are neither empty nor a comment, and keeps the lines of an index (a member
// <db>.info: "name:size:sha256" per file, between the header's copy and the DSIG line) while they fit 1 MiB.
type lineCounter struct {
	h       hash.Hash
	lines   int
	content bool
	comment bool
	start   bool
	cur     []byte
	kept    int
	index   bool
	info    []Member
}

func newLineCounter(index bool) *lineCounter {
	return &lineCounter{h: sha256.New(), start: true, index: index}
}

func (c *lineCounter) Write(p []byte) (int, error) {
	c.h.Write(p)
	for _, b := range p {
		if b == '\n' {
			c.endLine()
			continue
		}
		if c.start && b == '#' {
			c.comment = true
		}
		if b != ' ' && b != '\t' && b != '\r' {
			c.content = true
		}
		c.start = false
		if c.index && c.kept < 1<<20 && len(c.cur) < 4096 {
			c.cur = append(c.cur, b)
			c.kept++
		}
	}
	return len(p), nil
}

func (c *lineCounter) endLine() {
	if c.content && !c.comment {
		c.lines++
	}
	if c.index {
		if m, ok := indexLine(strings.TrimRight(string(c.cur), "\r")); ok {
			c.info = append(c.info, m)
		}
	}
	c.content, c.comment, c.start, c.cur = false, false, true, c.cur[:0]
}

func (c *lineCounter) done() int {
	if !c.start {
		c.endLine()
	}
	return c.lines
}

// indexLine reads one line of a container's index: name:size:sha256, a SHA-256 of 64 hex digits. The header's copy and the DSIG line
// do not have that shape.
func indexLine(line string) (Member, bool) {
	f := strings.Split(line, ":")
	if len(f) != 3 || f[0] == "" || len(f[0]) > 255 || len(f[2]) != 64 {
		return Member{}, false
	}
	size, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil || size < 0 {
		return Member{}, false
	}
	if _, err := hex.DecodeString(f[2]); err != nil {
		return Member{}, false
	}
	return Member{Name: f[0], Size: size, SHA256: strings.ToLower(f[2])}, true
}
