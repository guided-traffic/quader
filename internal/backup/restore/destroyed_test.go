package restore

import (
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
)

// destroyedBackup makes the env's backup one of an installation whose connection archive had its keys destroyed before the backup
// (storage.md R-STOR-097): the backup's storage_keys holds archive's storage key and name key in the state destroyed, without a sealed
// value, and its pool names neither.
func destroyedBackup(t *testing.T, e *env) {
	t.Helper()
	at := "2026-11-01 00:00:00+00"
	rows := "sk-archive-1-0a1b2c3d\tarchive\tstorage\t1\t\\N\tdestroyed\t2026-10-01 00:00:00+00\t" + at + "\t" + at + "\n" +
		"nk-archive-71be04d9\tarchive\tname\t1\t\\N\tdestroyed\t2026-10-01 00:00:00+00\t" + at + "\t" + at + "\n"
	z := zstdBytes(t, []byte(rows))
	e.mem.Set(e.layout.Table(e.id, backup.StorageKeysTable), z)
	e.m.Tables = append(e.m.Tables, format.TableFile{
		Name: backup.StorageKeysTable, Class: "data", File: "db/storage_keys.copy.zst",
		Columns: [][2]string{
			{"key_id", "text"},
			{"connection_id", "text"},
			{"kind", "text"},
			{"generation", "integer"},
			{"sealed_key", "text"},
			{"state", "text"},
			{"created_at", "timestamp with time zone"},
			{"retired_at", "timestamp with time zone"},
			{"destroyed_at", "timestamp with time zone"},
		},
		Rows: 2, Bytes: int64(len(rows)), CompressedBytes: int64(len(z)), SHA256: sum(z),
	})
	e.m.StorageKeys = &format.StorageKeysInfo{Pool: []format.PoolKey{}, Sealed: []string{}, Destroyed: []string{"nk-archive-71be04d9", "sk-archive-1-0a1b2c3d"}}
	e.putManifest()
}

func TestR_STOR_102_TheRestoreRefusesAConnectionIDWithDestroyedKeys(t *testing.T) {
	check3 := func(t *testing.T, e *env) error {
		t.Helper()
		return e.restorer().checkStorageKeys(context.Background(), &state{id: e.id, m: e.m})
	}
	const sentence = "a connection id with destroyed keys is retired for good; give the connection another id (storage.md R-STOR-102)"
	t.Run("R-STOR-102 R-BAK-031 an encrypted connection of the restoring configuration with that id is refused at check 3", func(t *testing.T) {
		e := newEnv(t)
		destroyedBackup(t, e)
		encryptedSink(e, "archive")
		if err := check3(t, e); err == nil || !strings.Contains(err.Error(), sentence) {
			t.Fatalf("check 3: %v", err)
		}
	})
	t.Run("R-STOR-102 a plain connection with that id is refused as well: the restored installation could not start with it", func(t *testing.T) {
		e := newEnv(t)
		destroyedBackup(t, e)
		e.extra = []Connection{{ID: "archive", Bucket: "b2", Prefix: "q2", Store: &fakeBlobs{objs: map[format.Digest][]byte{}}}}
		if err := check3(t, e); err == nil || !strings.Contains(err.Error(), sentence) {
			t.Fatalf("check 3: %v", err)
		}
	})
	t.Run("R-STOR-102 connections of other ids restore", func(t *testing.T) {
		e := newEnv(t)
		destroyedBackup(t, e)
		encryptedSink(e, "vault")
		if err := check3(t, e); err != nil {
			t.Fatalf("check 3: %v", err)
		}
	})
}
