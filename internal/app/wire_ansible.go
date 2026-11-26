package app

import (
	"context"
	"fmt"
	"io"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/blob"
	ansibledata "github.com/guided-traffic/quader/internal/formatdata/ansible"
	"github.com/guided-traffic/quader/internal/formats"
	ansibleformat "github.com/guided-traffic/quader/internal/formats/ansible"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// ansibleBlobs gives the data port of the Ansible Galaxy format whole reads of a stored archive: the README of a version is a file
// inside it (R-ANS-026). A nil store gives none, and the screens then have no README text.
func ansibleBlobs(s *blob.Store) ansibledata.BlobReader {
	if s == nil {
		return nil
	}
	return ansibleBlobReader{s}
}

// ansibleBlobReader opens a stored blob whole, verified as it is read; the OCI data port reads a Helm chart's content layer with it too
// (formats/oci.md R-OCI-042).
type ansibleBlobReader struct{ s *blob.Store }

func (b ansibleBlobReader) Open(ctx context.Context, sha256 [32]byte, size int64) (io.ReadCloser, error) {
	return b.s.Open(ctx, blob.Digest(sha256), size, blob.OpenOptions{})
}

// ansibleArchives reads the manifest of a stored archive for the reindex (R-ANS-012): the archive is read as it was at publish, in one
// pass under the bounds of R-ANS-031, and the blob store verifies it against its digest.
type ansibleArchives struct{ blobs ansibledata.BlobReader }

var (
	_ rows.ArchiveReader   = ansibleArchives{}
	_ rows.DocsReader      = ansibleArchives{}
	_ rows.ChangelogReader = ansibleArchives{}
)

// ReadDocs generates the documentation of a stored archive in the one pass of the reader, as a publish does (R-ANS-034, R-ANS-044).
func (r ansibleArchives) ReadDocs(ctx context.Context, ref rows.ArchiveRef) (rows.DocSet, error) {
	body, err := r.blobs.Open(ctx, ref.SHA256, ref.Size)
	if err != nil {
		return rows.DocSet{}, err
	}
	defer func() { _ = body.Close() }()
	gen := docgen.NewGenerator(docgen.DefaultLimits())
	a, err := collection.ReadArchive(ctx, body, collection.DefaultLimits(), collection.WithTap(gen))
	if err != nil {
		return rows.DocSet{}, err
	}
	return gen.Build(a.ReadmeName, a.Namespace+"."+a.Name), nil
}

// ReadChangelog reads only the changelog of a stored archive, for the reindex of a remote (R-ANS-053).
func (r ansibleArchives) ReadChangelog(ctx context.Context, ref rows.ArchiveRef) (*rows.DocChangelog, error) {
	body, err := r.blobs.Open(ctx, ref.SHA256, ref.Size)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	cl := docgen.NewChangelogReader()
	a, err := collection.ReadArchive(ctx, body, collection.DefaultLimits(), collection.WithTap(cl))
	if err != nil {
		return nil, err
	}
	return cl.Changelog(a.Namespace + "." + a.Name), nil
}

func (r ansibleArchives) ReadArchive(ctx context.Context, ref rows.ArchiveRef) (rows.ArchiveFacts, error) {
	body, err := r.blobs.Open(ctx, ref.SHA256, ref.Size)
	if err != nil {
		return rows.ArchiveFacts{}, err
	}
	defer func() { _ = body.Close() }()
	lim := collection.DefaultLimits()
	a, err := collection.ReadArchive(ctx, body, lim)
	if err != nil {
		return rows.ArchiveFacts{}, err
	}
	return rows.ArchiveFacts{
		Namespace: a.Namespace, Name: a.Name, Version: a.Version, Info: a.Info, Dependencies: a.Dependencies, RequiresAnsible: a.RequiresAnsible,
	}, nil
}

// startAnsibleReindex builds the handler of the job ansible-reindex on a replica that claims jobs (R-ANS-012). The kind is registered
// earlier, with a handler that finds this one when a run starts. What an older generator made is queued for the reindex by the task
// remote-describe (its Ansible Galaxy entry, wire_remote_describe.go, Q-ANS-99).
func (a *App) startAnsibleReindex(in infra, ports map[formats.ID]any) error {
	st, ok := ports[ansibleformat.ID].(*ansibledata.Store)
	if !ok {
		return fmt.Errorf("formats: the Ansible Galaxy data port is not the PostgreSQL store")
	}
	a.ansibleReindex.set(ansibledata.ReindexHandler(st, ansibleArchives{blobs: ansibleBlobs(in.blobs)}))
	a.ansibleDocsFill.set(ansibledata.DocsFillHandler(st, ansibleBackground{a}))
	return nil
}

// ansibleBackground gives the job ansible-docs-fill the upstream of a remote repository for one run (Host.BackgroundUpstream, Q-REPO-272),
// from the mirror host's builder.
type ansibleBackground struct{ a *App }

var _ ansibledata.Background = ansibleBackground{}

// Upstream implements [ansibledata.Background].
func (b ansibleBackground) Upstream(ctx context.Context, repoID int64, jobID string) (discover.Upstream, error) {
	return debBackground(b).Upstream(ctx, repoID, jobID)
}

// ansibleDocsFillHandler is the handler registered for the kind ansible-docs-fill.
func (a *App) ansibleDocsFillHandler() jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		h, ok := a.ansibleDocsFill.get()
		if !ok {
			return fmt.Errorf("ansible docs fill: this replica has not wired it yet")
		}
		return h(ctx, run)
	}
}

// ansibleReindexHandler is the handler registered for the kind ansible-reindex.
func (a *App) ansibleReindexHandler() jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		h, ok := a.ansibleReindex.get()
		if !ok {
			return fmt.Errorf("ansible reindex: this replica has not wired it yet")
		}
		return h(ctx, run)
	}
}

// ansibleStore is the data port's management group behind the Ansible Galaxy operations of the API (R-ANS-009, R-ANS-012); a replica
// without the ports has none.
func (a *App) ansibleStore() v1.AnsibleStore {
	fp := part[formatPortsArea](a.parts)
	if fp.bindings == nil {
		return nil
	}
	if s, ok := fp.bindings.Get(ansibleformat.ID).(v1.AnsibleStore); ok {
		return s
	}
	return nil
}

// routeHandoff is the format dispatcher as the management API's road into a format's route (R-ANS-061, Q-ANS-140); nil where no
// dispatcher is composed (the shell profile), which answers the screens' publish with 503.
func (a *App) routeHandoff() v1.RouteHandoff {
	disp := part[formatHostArea](a.parts).disp
	if disp == nil {
		// A nil pointer in the interface would not be nil.
		return nil
	}
	return disp
}
