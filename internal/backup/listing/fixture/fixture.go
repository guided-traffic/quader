// Package fixture writes backups into an in-memory target for the tests of the lane: a valid manifest, a blob list, table
// files and pool objects whose digests are real, so verify, retention and the sweep can be tested against the same bytes.
package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/target/targettest"
)

// Spec describes one backup.
type Spec struct {
	ID                        format.ID
	Installation, Incarnation string
	SnapshotAt                time.Time
	// Blobs are the contents of the pool objects the backup lists; an entry in Damaged is listed but not written.
	Blobs   []string
	Damaged []string
	// KeyIDs defaults to "k1".
	KeyIDs []string
	// Incomplete leaves the manifest out.
	Incomplete bool
	// Tables maps a table name to the bytes of its file; the default is one table "items".
	Tables map[string]string
	// Sealed are blobs an encrypted connection stored: each is listed under its storage key and written to its encrypted pool object
	// with its record (R-BAK-031, R-BAK-032).
	Sealed []Sealed
	// StorageKeys is the COPY text of the backup's storage_keys (key_id, connection_id, kind, generation, sealed_key, state), written
	// zstd-compressed as its table file; empty writes none.
	StorageKeys string
}

// Sealed is a blob as an encrypted connection stored it: its plaintext, the storage key its record names, its stored bytes and its
// record.
type Sealed struct {
	Content  string
	KeyID    string
	Body     []byte
	Metadata map[string]string
}

// Backup is what Put wrote.
type Backup struct {
	Manifest *format.Manifest
	Entries  []format.BlobEntry
}

// Digest returns the SHA-256 of a content.
func Digest(content string) format.Digest { return format.Digest(sha256.Sum256([]byte(content))) }

// ID builds the backup ID of the given day of November 2026, with a suffix derived from the day.
func ID(day int) format.ID { return format.ID(fmt.Sprintf("202611%02dT020000Z-%04x", day, day)) }

// Put writes the backup and its pool objects. A pool object that exists is overwritten with the same bytes.
func Put(tb testing.TB, m *targettest.Mem, l format.Layout, s Spec) Backup {
	tb.Helper()
	if s.Installation == "" {
		s.Installation = "inst-1"
	}
	if s.Incarnation == "" {
		s.Incarnation = "inc-1"
	}
	if s.SnapshotAt.IsZero() {
		s.SnapshotAt = s.ID.Time().Add(time.Second)
	}
	if len(s.KeyIDs) == 0 {
		s.KeyIDs = []string{"k1"}
	}
	if s.Tables == nil {
		s.Tables = map[string]string{"items": "1\tone\n2\ttwo\n"}
	}
	byDigest := map[format.Digest]format.BlobEntry{}
	for _, c := range s.Blobs {
		d := Digest(c)
		byDigest[d] = format.BlobEntry{Digest: d, Size: int64(len(c))}
		m.Set(l.Pool(d), []byte(c))
	}
	for _, sb := range s.Sealed {
		d := Digest(sb.Content)
		e := format.BlobEntry{Digest: d, Size: int64(len(sb.Content)), Pool: sb.KeyID}
		byDigest[d] = e
		m.Set(l.PoolObject(e), sb.Body)
		m.SetMetadata(l.PoolObject(e), sb.Metadata)
	}
	damaged := []format.DamagedBlob{}
	for _, c := range s.Damaged {
		d := Digest(c)
		byDigest[d] = format.BlobEntry{Digest: d, Size: int64(len(c))}
		damaged = append(damaged, format.DamagedBlob{SHA256: d.Hex(), Status: "missing"})
	}
	entries := make([]format.BlobEntry, 0, len(byDigest))
	var referenced int64
	for _, e := range byDigest {
		entries = append(entries, e)
		referenced += e.Size
	}
	slices.SortFunc(entries, func(a, b format.BlobEntry) int { return strings.Compare(a.Digest.Hex(), b.Digest.Hex()) })
	list, err := format.EncodeBlobList(entries, 3)
	if err != nil {
		tb.Fatal(err)
	}
	m.Set(l.BlobList(s.ID), list)

	man := &format.Manifest{
		Format: format.ManifestFormat, BackupID: s.ID, InstallationID: s.Installation, Incarnation: s.Incarnation,
		Quader:    format.QuaderInfo{Version: "0.1.0", Revision: "abc"},
		Schema:    format.SchemaInfo{Step: 46, Steps: []format.SchemaStep{{Step: 1, Name: "00001_core", SHA256: "x"}}},
		Postgres:  format.PostgresRef{ServerVersion: "17.6", Isolation: "repeatable read"},
		StartedAt: s.ID.Time(), SnapshotAt: s.SnapshotAt, CompletedAt: s.SnapshotAt.Add(time.Minute),
		Sequences: []format.Sequence{},
		Blobs: format.BlobsInfo{
			File: format.BlobListName, SHA256: sum(list), Referenced: int64(len(entries)), ReferencedBytes: referenced,
			Copied: int64(len(s.Blobs)), Damaged: damaged,
		},
		Secrets:    format.SecretsInfo{KeyIDs: s.KeyIDs},
		Encryption: format.Encryption{SSE: "AES256"},
	}
	names := make([]string, 0, len(s.Tables))
	for n := range s.Tables {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		body := []byte(s.Tables[n])
		m.Set(l.Table(s.ID, n), body)
		man.Tables = append(man.Tables, format.TableFile{
			Name: n, Class: "data", File: format.TablesDir + "/" + n + format.TableSuffix,
			Columns: [][2]string{{"id", "bigint"}}, Rows: 2, Bytes: int64(len(body)) * 3, CompressedBytes: int64(len(body)), SHA256: sum(body),
		})
	}
	if s.StorageKeys != "" {
		var buf bytes.Buffer
		zw, err := zstd.NewWriter(&buf)
		if err != nil {
			tb.Fatal(err)
		}
		_, _ = zw.Write([]byte(s.StorageKeys))
		_ = zw.Close()
		m.Set(l.Table(s.ID, "storage_keys"), buf.Bytes())
		man.Tables = append(man.Tables, format.TableFile{
			Name: "storage_keys", Class: "data", File: format.TablesDir + "/storage_keys" + format.TableSuffix,
			Columns: [][2]string{{"key_id", "text"}, {"connection_id", "text"}, {"kind", "text"}, {"generation", "integer"}, {"sealed_key", "text"}, {"state", "text"}},
			Rows:    int64(strings.Count(s.StorageKeys, "\n")), Bytes: int64(len(s.StorageKeys)), CompressedBytes: int64(buf.Len()), SHA256: sum(buf.Bytes()),
		})
	}
	if !s.Incomplete {
		raw, err := man.Encode()
		if err != nil {
			tb.Fatalf("fixture manifest %s: %v", s.ID, err)
		}
		m.Set(l.Manifest(s.ID), raw)
	}
	return Backup{Manifest: man, Entries: entries}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
