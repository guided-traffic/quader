package app

import (
	"context"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/blob"
	debdata "github.com/guided-traffic/quader/internal/formatdata/deb"
	npmdata "github.com/guided-traffic/quader/internal/formatdata/npm"
	"github.com/guided-traffic/quader/internal/formats"
	debformat "github.com/guided-traffic/quader/internal/formats/deb"
	npmformat "github.com/guided-traffic/quader/internal/formats/npm"
	"github.com/guided-traffic/quader/internal/jobs"
)

// The jobs that describe what a remote repository cached (npm R-NPM-050, Debian R-DEB-037): their kinds are registered with the format
// jobs (registerFormatJobs), with handlers that find the data port's handler built here on a replica that claims jobs.

// startRemoteDescribe builds the handlers of npm-remote-describe and deb-remote-describe over the data ports.
func (a *App) startRemoteDescribe(ports map[formats.ID]any) error {
	npm, ok := ports[npmformat.ID].(*npmdata.Store)
	if !ok {
		return fmt.Errorf("formats: the npm data port is not the PostgreSQL store")
	}
	deb, ok := ports[debformat.ID].(*debdata.Store)
	if !ok {
		return fmt.Errorf("formats: the Debian data port is not the PostgreSQL store")
	}
	a.npmDescribe.set(npmdata.DescribeHandler(npm))
	a.debDescribe.set(debdata.DescribeHandler(deb))
	return nil
}

// runLate runs a handler that was built after the registry was frozen.
func runLate(ctx context.Context, h jobs.Handler, run *jobs.Run) error { return h(ctx, run) }

// npmBlobs is the blob reader of the npm data port: the stored document and the cached tarball a job reads, and the README a screen
// reads from a tarball, whole and verified against the digest (R-NPM-050, R-NPM-051).
func npmBlobs(s *blob.Store) npmdata.BlobReader {
	if s == nil {
		return nil
	}
	return npmBlobReader{s}
}

type npmBlobReader struct{ s *blob.Store }

// debBlobs is the blob reader of the Debian data port: the control archive of a stored package file, with ranged reads (R-DEB-037).
func debBlobs(s *blob.Store) debdata.BlobRanges {
	if s == nil {
		return nil
	}
	return blobRanges{s}
}

func (b npmBlobReader) Open(ctx context.Context, sha256 [32]byte, size int64) (io.ReadCloser, error) {
	return b.s.Open(ctx, blob.Digest(sha256), size, blob.OpenOptions{})
}
