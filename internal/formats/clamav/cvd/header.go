// Package cvd reads ClamAV's definition files (formats/clamav.md R-CLAMAV-002, R-CLAMAV-003; malware-scanning.md R-MAL-010, R-MAL-033):
// the 512-byte header of a database container, the names of the containers and of their detached signatures, the table of ClamAV's
// functionality levels, the member list of a container's archive and the signature count of a detached signature file.
//
// It is a leaf (the standard library and internal/formats/untrusted): the format (edge), its data port and the malware scanning's
// database task (domain) read one parser. It verifies nothing: the database task checks the archive's MD5 and the header's signature
// before it reads a member (R-MAL-033).
package cvd

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HeaderSize is the size of a container's header: ClamAV's 512 bytes, padded with spaces.
const HeaderSize = 512

// headerMagic starts every header.
const headerMagic = "ClamAV-VDB:"

// headerTime is the layout of the header's build time ("20 Nov 2026 10-00 +0000").
const headerTime = "02 Jan 2006 15-04 -0700"

// Header is what the header of a database container says, field by field: ClamAV-VDB:<build time>:<version>:<signatures>:
// <functionality level>:<md5>:<signature>:<builder>:<build time in seconds>.
type Header struct {
	// BuildTimeText is field 2 as written ("21 Nov 2026 08-29 +0000").
	BuildTimeText string
	// BuiltAt is the build time: from field 9 (seconds) when it is there, else from field 2.
	BuiltAt time.Time
	// Version is field 3: the container's version.
	Version int
	// Signatures is field 4: the number of signatures the publisher counted.
	Signatures int
	// FunctionalityLevel is field 5: the level of the engine that loads it (Q-MAL-81).
	FunctionalityLevel int
	// MD5 is field 6: the MD5 of the archive after the header, in hex as written.
	MD5 string
	// DSig is field 7: the digital signature over the MD5 (the legacy check of libclamav); empty in an unsigned container.
	DSig string
	// Builder is field 8: the name of whoever built it.
	Builder string
}

// ErrNoHeader is the error of bytes that do not start with a container's header.
var ErrNoHeader = errors.New("no ClamAV database header")

// maxText bounds a text field of a header that is kept (the build time's text, the builder): the whole header has 512 bytes.
const maxText = 128

// ParseHeader reads the header at the start of b (at least its first line; at most HeaderSize bytes are read). It refuses bytes
// without the magic, with fewer than eight fields, with a field that is not a number where one is due, and with a version below 1.
func ParseHeader(b []byte) (Header, error) {
	if len(b) > HeaderSize {
		b = b[:HeaderSize]
	}
	if !bytes.HasPrefix(b, []byte(headerMagic)) {
		return Header{}, ErrNoHeader
	}
	line := string(b)
	if i := strings.IndexAny(line, "\n\x00"); i >= 0 {
		line = line[:i]
	}
	f := strings.Split(strings.TrimRight(line, " "), ":")
	if len(f) < 8 {
		return Header{}, fmt.Errorf("%w: %d fields, want at least 8", ErrNoHeader, len(f))
	}
	h := Header{BuildTimeText: clip(strings.TrimSpace(f[1])), MD5: strings.TrimSpace(f[5]), DSig: strings.TrimSpace(f[6]), Builder: clip(strings.TrimSpace(f[7]))}
	var err error
	if h.Version, err = number(f[2], "version"); err != nil {
		return Header{}, err
	}
	if h.Version < 1 {
		return Header{}, fmt.Errorf("%w: version %d", ErrNoHeader, h.Version)
	}
	if h.Signatures, err = number(f[3], "number of signatures"); err != nil {
		return Header{}, err
	}
	if h.FunctionalityLevel, err = number(f[4], "functionality level"); err != nil {
		return Header{}, err
	}
	if len(f) >= 9 {
		if secs, err := strconv.ParseInt(strings.TrimSpace(f[8]), 10, 64); err == nil && secs > 0 {
			h.BuiltAt = time.Unix(secs, 0).UTC()
		}
	}
	if h.BuiltAt.IsZero() {
		t, err := time.Parse(headerTime, strings.TrimSpace(f[1]))
		if err != nil {
			return Header{}, fmt.Errorf("%w: build time %q", ErrNoHeader, f[1])
		}
		h.BuiltAt = t.UTC()
	}
	return h, nil
}

func number(s, what string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: %s %q", ErrNoHeader, what, s)
	}
	return n, nil
}

func clip(s string) string {
	if len(s) > maxText {
		return s[:maxText]
	}
	return s
}

// The databases (R-CLAMAV-008): ClamAV's three standard databases, the package names of the format.
const (
	DBMain     = "main"
	DBDaily    = "daily"
	DBBytecode = "bytecode"
)

// DBs are the databases in the order a set names them.
func DBs() []string { return []string{DBMain, DBDaily, DBBytecode} }

// IsDB reports whether name is one of the three databases.
func IsDB(name string) bool { return name == DBMain || name == DBDaily || name == DBBytecode }

// The containers. A .cvd is the publisher's signed file, a .cld the file freshclam builds from it and its signed differences, a .cud
// ClamAV's unsigned container (sigtool --unsigned), which only an installation's own repository supplies (Q-MAL-82).
const (
	ExtCVD = "cvd"
	ExtCLD = "cld"
	ExtCUD = "cud"
)

var (
	containerName = regexp.MustCompile(`^(main|daily|bytecode)\.(cvd|cld|cud)$`)
	signName      = regexp.MustCompile(`^(main|daily|bytecode)-([1-9][0-9]{0,9})\.(cvd|cld|cud)\.sign$`)
)

// Container classifies a file name as a database container: its database and its extension.
func Container(name string) (db, ext string, ok bool) {
	m := containerName.FindStringSubmatch(name)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// Sign classifies a file name as a detached signature of ClamAV 1.5 (sigtool --sign: dbname-version.cvd.sign): its database and the
// container version it signs.
func Sign(name string) (db string, version int, ok bool) {
	m := signName.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	v, err := strconv.Atoi(m[2])
	return m[1], v, err == nil
}

// SignName is the name of the detached signature of a container's version: daily-28190.cvd.sign.
func SignName(db string, version int) string { return db + "-" + strconv.Itoa(version) + ".cvd.sign" }

// MaxSignSize bounds a detached signature file (R-MAL-033): a few lines of base64.
const MaxSignSize = 64 << 10

// signMagic is the first line of a detached signature file.
const signMagic = "#clamsign-1.0"

// SignatureCount is the number of signatures a detached signature file holds: its lines after #clamsign-1.0 that are not empty
// (R-CLAMAV-003). A file above MaxSignSize, or one whose first line is not #clamsign-1.0, is refused. Nothing of a signature is parsed:
// Quader passes the file on (Q-MAL-95).
func SignatureCount(b []byte) (int, error) {
	if len(b) > MaxSignSize {
		return 0, fmt.Errorf("the detached signature has %d bytes, above %d", len(b), MaxSignSize)
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != signMagic {
		return 0, fmt.Errorf("the detached signature does not start with %s", signMagic)
	}
	n := 0
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n, nil
}
