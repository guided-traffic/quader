package blobtest

import (
	"io/fs"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

// Storage returns the storage configuration with every default applied, the
// spool at spoolDir and the object-store prefix "quader". Tests change the fields
// they care about.
func Storage(tb testing.TB, spoolDir string) config.Storage {
	tb.Helper()
	l, err := config.Load(config.Options{
		ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist },
		Environ: []string{
			"QUADER_DATABASE_HOST=db.invalid", "QUADER_DATABASE_NAME=quader",
			"QUADER_STORAGE_S3_BUCKET=blobs", "QUADER_STORAGE_SPOOL_DIR=" + spoolDir,
		},
		Command: config.CommandMigrate,
	})
	if err != nil {
		tb.Fatalf("storage defaults: %v", err)
	}
	return l.Config.Storage
}
