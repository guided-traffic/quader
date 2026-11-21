// Package cvdtest builds ClamAV definition containers of a few hundred bytes for the tests (formats/clamav.md R-CLAMAV-009): ClamAV's
// header of 512 bytes with every field, the MD5 of the archive in field 6, and a gzip of a tar that holds the database's index
// (<db>.info, "name:size:sha256" per member) and the members. No test needs ClamAV's real databases, which no test may download
// (Q-MAL-90). The signature field holds what the caller passes: the containers verify against no key of ClamAV's.
package cvdtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5" //nolint:gosec // the header's field 6 is the MD5 of the archive, ClamAV's legacy check; nothing here relies on it for security
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Member is one file of a container's archive.
type Member struct {
	Name string
	Body []byte
}

// Options are the fields of a container's header beside its database and version.
type Options struct {
	BuiltAt            time.Time
	Signatures         int
	FunctionalityLevel int
	Builder            string
	// DSig is field 7, the signature over the MD5; "X" when empty.
	DSig string
	// NoIndex leaves the member <db>.info out of the archive.
	NoIndex bool
}

// Container returns a container of the database db at version: the header, then the gzip of a tar holding <db>.info and the members.
func Container(db string, version int, o Options, members ...Member) []byte {
	archive := Archive(db, version, o, members...)
	return append(Header(db, version, o, archive), archive...)
}

// Archive is the archive of a container: the bytes after its header.
func Archive(db string, version int, o Options, members ...Member) []byte {
	o = defaults(o)
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	files := members
	if !o.NoIndex {
		var idx strings.Builder
		fmt.Fprintf(&idx, "ClamAV-VDB:%s:%d:%d:%d:X:X:%s:%d\n", o.BuiltAt.UTC().Format("02 Jan 2006 15-04 -0700"), version, o.Signatures,
			o.FunctionalityLevel, o.Builder, o.BuiltAt.Unix())
		for _, m := range members {
			sum := sha256.Sum256(m.Body)
			fmt.Fprintf(&idx, "%s:%d:%s\n", m.Name, len(m.Body), hex.EncodeToString(sum[:]))
		}
		idx.WriteString("DSIG:X\n")
		files = append([]Member{{Name: db + ".info", Body: []byte(idx.String())}}, members...)
	}
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, f := range files {
		_ = tw.WriteHeader(&tar.Header{Name: f.Name, Mode: 0o644, Size: int64(len(f.Body)), Format: tar.FormatUSTAR, ModTime: o.BuiltAt})
		_, _ = tw.Write(f.Body)
	}
	_ = tw.Close()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(tarBuf.Bytes())
	_ = zw.Close()
	return gz.Bytes()
}

// Header is the header of 512 bytes of a container whose archive is archive: every field, the archive's MD5 in field 6.
func Header(db string, version int, o Options, archive []byte) []byte {
	o = defaults(o)
	sum := md5.Sum(archive) //nolint:gosec // see the import
	h := fmt.Sprintf("ClamAV-VDB:%s:%d:%d:%d:%s:%s:%s:%d", o.BuiltAt.UTC().Format("02 Jan 2006 15-04 -0700"), version, o.Signatures,
		o.FunctionalityLevel, hex.EncodeToString(sum[:]), o.DSig, o.Builder, o.BuiltAt.Unix())
	out := make([]byte, 512)
	copy(out, h)
	for i := len(h); i < len(out); i++ {
		out[i] = ' '
	}
	_ = db
	return out
}

// Sign is a detached signature file of n signatures: #clamsign-1.0 and n lines.
func Sign(n int) []byte {
	var b strings.Builder
	b.WriteString("#clamsign-1.0\n")
	for i := range n {
		fmt.Fprintf(&b, "%d:%s\n", i+1, strings.Repeat("A", 40))
	}
	return []byte(b.String())
}

func defaults(o Options) Options {
	if o.BuiltAt.IsZero() {
		o.BuiltAt = time.Date(2026, 11, 21, 8, 29, 0, 0, time.UTC)
	}
	if o.Builder == "" {
		o.Builder = "quader-test"
	}
	if o.FunctionalityLevel == 0 {
		o.FunctionalityLevel = 120
	}
	if o.DSig == "" {
		o.DSig = "X"
	}
	return o
}
