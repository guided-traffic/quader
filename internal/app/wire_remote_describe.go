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
	"github.com/guided-traffic/quader/internal/packages/remotedescribe"
)

// The jobs that describe what a remote repository cached (npm R-NPM-050, Debian R-DEB-037): their kinds are registered with the format
// jobs (registerFormatJobs), with handlers that find the data port's handler built here on a replica that claims jobs.

// startRemoteDescribe builds the handlers of npm-remote-describe and deb-remote-describe over the data ports, and the task
// remote-describe, which it queues when a remote holds a cached version or file without its row (R-NPM-050, R-DEB-037): what was cached
// before the jobs existed is described after the upgrade without a change at the upstream.
func (a *App) startRemoteDescribe(ctx context.Context, in infra, ports map[formats.ID]any) error {
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
	task, err := remotedescribe.New(remotedescribe.Options{
		DB: in.db, Jobs: in.queue,
		Formats: []remotedescribe.Format{{Kind: npmdata.DescribeJobKind, Port: npm}, {Kind: debdata.DescribeJobKind, Port: deb}},
	})
	if err != nil {
		return err
	}
	a.remoteDescribe.set(task)
	queued, err := task.EnqueueIfNeeded(ctx)
	if err != nil {
		return fmt.Errorf("formats: queueing %s: %w", remotedescribe.TaskName, err)
	}
	if queued {
		a.log.Info("queued the description of what remotes cached without it", "task", remotedescribe.TaskName)
	}
	return nil
}

// registerRemoteDescribeTask registers the kind and the task remote-describe: once a day by default, maintenance.tasks.remote-describe
// overrides it, and an operator runs it through the tasks API (R-OPS-019).
func (a *App) registerRemoteDescribeTask(r *jobs.Registry) {
	r.RegisterKind(remotedescribe.TaskName, remotedescribe.KindSpec(a.remoteDescribe.handler("the task remote-describe",
		func(ctx context.Context, t *remotedescribe.Task, run *jobs.Run) error { return t.Run(ctx, run) })))
	spec := jobs.TaskSpec{Schedule: remotedescribe.Schedule}
	if t, ok := a.cfg.Maintenance.Tasks[remotedescribe.TaskName]; ok && t.Schedule != "" {
		spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
	}
	r.RegisterTask(remotedescribe.TaskName, spec)
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
