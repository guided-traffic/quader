package cvd_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats/clamav/cvd"
	"github.com/guided-traffic/quader/internal/formats/clamav/cvd/cvdtest"
)

// TestR_CLAMAV_003_TheHeaderNamesEveryField: every field of a container's header is read (R-CLAMAV-003): the build time as text and
// from its seconds, the version, the signatures, the functionality level, the MD5, the signature and the builder. A header without
// the magic, with too few fields or a version of 0 is refused. Milliseconds, a header of 512 bytes.
func TestR_CLAMAV_003_TheHeaderNamesEveryField(t *testing.T) {
	built := time.Date(2026, 11, 21, 8, 29, 0, 0, time.UTC)
	c := cvdtest.Container("daily", 28190, cvdtest.Options{BuiltAt: built, Signatures: 4, FunctionalityLevel: 235, Builder: "svc.clamav-publisher", DSig: "abc"})
	h, err := cvd.ParseHeader(c[:cvd.HeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != 28190 || h.Signatures != 4 || h.FunctionalityLevel != 235 || h.Builder != "svc.clamav-publisher" || h.DSig != "abc" ||
		!h.BuiltAt.Equal(built) || h.BuildTimeText != "21 Nov 2026 08-29 +0000" || len(h.MD5) != 32 {
		t.Fatalf("header %+v", h)
	}
	for name, b := range map[string][]byte{
		"no magic":   []byte("NotClamAV:1:2:3"),
		"few fields": []byte("ClamAV-VDB:21 Nov 2026 08-29 +0000:1:2"),
		"version 0":  []byte("ClamAV-VDB:21 Nov 2026 08-29 +0000:0:2:3:md5:sig:me:1"),
	} {
		if _, err := cvd.ParseHeader(b); !errors.Is(err, cvd.ErrNoHeader) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestR_CLAMAV_003_TheLevelTableNamesTheOldestReleaseThatLoadsAContainer: a level is read against the table of ClamAV's releases
// (R-CLAMAV-003): a level the table names is the release of that level, one between two rows the next release, and one above the
// newest row reads "Newer than". Milliseconds.
func TestR_CLAMAV_003_TheLevelTableNamesTheOldestReleaseThatLoadsAContainer(t *testing.T) {
	for _, tc := range []struct {
		level int
		want  string
	}{
		{120, "ClamAV 0.103 or later"},
		{121, "ClamAV 0.103.1 or later"},
		{169, "ClamAV 1.0.9 or later"},
		{170, "ClamAV 1.1 or later"},
		{235, "ClamAV 1.5.5 or later"},
		{cvd.NewestLevel(), "ClamAV 1.6 or later"},
		{cvd.NewestLevel() + 1, "Newer than ClamAV 1.6"},
	} {
		if got := cvd.LevelOf(tc.level).Text(); got != tc.want {
			t.Errorf("level %d: %q, want %q", tc.level, got, tc.want)
		}
	}
}

// TestR_CLAMAV_003_TheMembersComeFromTheContainersIndex: the member list of a container is its index's (name, size, SHA-256), with the
// lines of each member that are neither empty nor a comment, sorted by name, the index itself among them; without an index the walk's
// own sizes and digests stand. Milliseconds, containers of hundreds of bytes.
func TestR_CLAMAV_003_TheMembersComeFromTheContainersIndex(t *testing.T) {
	ndb := []byte("Sig.One:0:*:414243\n# a comment\n\nSig.Two:0:*:444546\n")
	hdb := []byte("d41d8cd98f00b204e9800998ecf8427e:0:Empty.File")
	c := cvdtest.Container("daily", 2, cvdtest.Options{}, cvdtest.Member{Name: "daily.ndb", Body: ndb}, cvdtest.Member{Name: "daily.hdb", Body: hdb})
	members, read, err := cvd.ReadMembers(context.Background(), bytes.NewReader(c[cvd.HeaderSize:]), cvd.DefaultBounds())
	if err != nil || !read {
		t.Fatalf("read %v, %v", read, err)
	}
	sum := sha256.Sum256(ndb)
	if len(members) != 3 || members[0].Name != "daily.hdb" || members[0].Lines != 1 || members[1].Name != "daily.info" ||
		members[2].Name != "daily.ndb" || members[2].Lines != 2 || members[2].Size != int64(len(ndb)) || members[2].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("members %+v", members)
	}
	plain := cvdtest.Container("main", 1, cvdtest.Options{NoIndex: true}, cvdtest.Member{Name: "main.ndb", Body: ndb})
	members, read, err = cvd.ReadMembers(context.Background(), bytes.NewReader(plain[cvd.HeaderSize:]), cvd.DefaultBounds())
	if err != nil || !read || len(members) != 1 || members[0].Lines != 2 || members[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("without an index: %+v, %v, %v", members, read, err)
	}
}

// TestR_MAL_033_AContainerPastTheBoundsIsReadWithoutMembers: a container whose archive holds more entries than the bound, or
// decompresses to more bytes, gives no member list and no error, so the task stores it all the same (R-CLAMAV-003, R-MAL-033).
// Milliseconds, a container of 12 members of a few bytes.
func TestR_MAL_033_AContainerPastTheBoundsIsReadWithoutMembers(t *testing.T) {
	var ms []cvdtest.Member
	for i := range 12 {
		ms = append(ms, cvdtest.Member{Name: fmt.Sprintf("daily%02d.ndb", i), Body: []byte("Sig:0:*:41\n")})
	}
	c := cvdtest.Container("daily", 3, cvdtest.Options{}, ms...)
	for name, b := range map[string]cvd.Bounds{"entries": {Entries: 5, Bytes: 1 << 20}, "bytes": {Entries: 100, Bytes: 64}} {
		members, read, err := cvd.ReadMembers(context.Background(), bytes.NewReader(c[cvd.HeaderSize:]), b)
		if err != nil || read || members != nil {
			t.Errorf("%s: members %v, read %v, err %v", name, members, read, err)
		}
	}
}

// TestR_CLAMAV_003_TheDetachedSignatureIsCountedNotParsed: a detached signature file counts its lines after #clamsign-1.0; one without
// that first line, or above 64 KiB, is refused (R-CLAMAV-003, R-MAL-033). Milliseconds.
func TestR_CLAMAV_003_TheDetachedSignatureIsCountedNotParsed(t *testing.T) {
	if n, err := cvd.SignatureCount(cvdtest.Sign(3)); err != nil || n != 3 {
		t.Fatalf("%d, %v", n, err)
	}
	if _, err := cvd.SignatureCount([]byte("not a signature\n")); err == nil {
		t.Error("a file without #clamsign-1.0 was counted")
	}
	if _, err := cvd.SignatureCount(bytes.Repeat([]byte("A"), cvd.MaxSignSize+1)); err == nil {
		t.Error("a file above the bound was counted")
	}
	if db, v, ok := cvd.Sign("daily-28190.cvd.sign"); !ok || db != "daily" || v != 28190 || cvd.SignName("daily", 28190) != "daily-28190.cvd.sign" {
		t.Errorf("Sign: %s %d %v", db, v, ok)
	}
}
