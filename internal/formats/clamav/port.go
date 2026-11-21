package clamav

import (
	"context"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/clamav/rows"
)

// Store is the format's data port (Q-ARCH-193): the side table clamav_version of migration 00094, implemented by
// internal/formatdata/clamav and bound by the composition root. The hooks write through it in the item's transaction; the endpoint and
// the package view read through it. The malware scanning's database task reaches the same table through the methods of that package's
// Store that are not part of this port (the marks of the set in use, the history's end; docs/developer/clamav.md).
type Store interface {
	// PutContainer writes the side row of a version from the payload of its container's commit, in tx: every piece of the metadata,
	// the file's name and the time it was stored. A row the signature's commit made first keeps its signature's facts.
	PutContainer(ctx context.Context, tx formats.Tx, versionID int64, file string, c rows.Container) error
	// PutSignature records the detached signature of a version in tx: its name, size and number of signatures. A version without a
	// row yet gets one without the container's metadata, which its container's commit fills.
	PutSignature(ctx context.Context, tx formats.Tx, versionID int64, name string, size int64, s rows.Signature) error
	// FilesRemoved marks the versions whose container was removed (R-MAL-034), in tx: files_removed_at, the in-use and previous marks
	// cleared. The rows stay as history.
	FilesRemoved(ctx context.Context, tx formats.Tx, versionIDs []int64) error
	// Served is the version of a database the set in use names in a repository, and its container's file name (R-CLAMAV-004); ok is
	// false while none is marked in use.
	Served(ctx context.Context, repoID int64, db string) (s rows.Served, ok bool, err error)
	// Versions reads the side rows of the given version IDs; an ID without a row is absent.
	Versions(ctx context.Context, versionIDs []int64) ([]rows.Version, error)
	// Stored lists the versions of a database in a repository (R-CLAMAV-007): the stored ones, newest first, then the history rows
	// without files, newest first, at most limit.
	Stored(ctx context.Context, repoID int64, db string, limit int) ([]rows.Version, error)
}

// portOf asserts the data port a hook or a handler was handed.
func portOf(data any) (Store, error) {
	if p, ok := data.(Store); ok && p != nil {
		return p, nil
	}
	return nil, errs.E(errs.Internal, errs.CodeInternal, "clamav: the data port is not bound")
}

// hostPort is the data port of the host a handler runs on.
func hostPort(h formats.DataPorts) (Store, error) {
	if h == nil {
		return portOf(nil)
	}
	return portOf(h.FormatData(ID))
}
