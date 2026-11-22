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
			"QUADER_STORAGE_CONNECTIONS=[{\"bucket\":\"blobs\"}]", "QUADER_STORAGE_SPOOL_DIR=" + spoolDir,
		},
		Command: config.CommandMigrate,
	})
	if err != nil {
		tb.Fatalf("storage defaults: %v", err)
	}
	return l.Config.Storage
}

// S3 is the client configuration of the default storage connection with every default applied (config.ClientConfig of
// Storage's one connection and its shared client settings); tests set the endpoint, the bucket and the credentials.
func S3(tb testing.TB) config.StorageS3 {
	tb.Helper()
	s := Storage(tb, tb.TempDir())
	return config.ClientConfig(s.Connections[0], s.ObjectStore)
}
