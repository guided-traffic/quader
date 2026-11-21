package clamav

import (
	"context"
	"strconv"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/clamav/rows"
)

// OnCommit writes the package row of the database, the version row and the side row of a committed definition file, in the commit's
// transaction (R-CLAMAV-002). Only the database task commits into the repository, as system work, with the container's metadata as the
// payload (rows.Container) or, for a detached signature, its facts (rows.Signature); a commit at any other path, or without the
// payload of its kind, is refused, so nothing reaches the repository that the screens could not describe.
func (Format) OnCommit(ctx context.Context, tx formats.Tx, c formats.CommitEvent) error {
	s, ok := ParsePath(c.Path.Path)
	if !ok {
		return errs.E(errs.Invalid, errs.CodeInvalidPath, "The path is no path of ClamAV's definitions: <db>/<version>/<db>.cvd or its .cvd.sign.")
	}
	port, err := portOf(c.Data)
	if err != nil {
		return err
	}
	pkg, _, err := c.Packages.EnsureLocked(ctx, c.Repo, s.DB, s.DB)
	if err != nil {
		return err
	}
	vid, existed, err := ensureVersion(ctx, c, pkg, s)
	if err != nil {
		return err
	}
	switch s.Kind {
	case KindContainer:
		meta, ok := c.Payload.(rows.Container)
		if !ok || meta.DB != s.DB || meta.Version != s.Version {
			return errs.E(errs.Invalid, errs.CodeInvalidPath, "A container is committed with its metadata, for the database and version of its path.")
		}
		if existed {
			if err := c.Packages.LinkItem(ctx, c.Item, vid, true); err != nil {
				return err
			}
		}
		if err := port.PutContainer(ctx, tx, int64(vid), s.File, meta); err != nil {
			return err
		}
	case KindSignature:
		sig, ok := c.Payload.(rows.Signature)
		if !ok || sig.DB != s.DB || sig.Version != s.Version {
			return errs.E(errs.Invalid, errs.CodeInvalidPath, "A detached signature is committed with its facts, for the database and version of its path.")
		}
		if err := c.Packages.LinkItem(ctx, c.Item, vid, false); err != nil {
			return err
		}
		if err := port.PutSignature(ctx, tx, int64(vid), s.File, c.Size, sig); err != nil {
			return err
		}
	}
	_, _, err = c.Packages.BumpRevision(ctx, pkg)
	return err
}

// ensureVersion finds the version row of a stored path or inserts it; a container is the version's lead item.
func ensureVersion(ctx context.Context, c formats.CommitEvent, pkg formats.PackageRef, s Stored) (formats.VersionID, bool, error) {
	version := strconv.Itoa(s.Version)
	vs, err := c.Packages.Versions(ctx, pkg)
	if err != nil {
		return 0, false, err
	}
	for _, v := range vs {
		if v.Version == version {
			return v.ID, true, nil
		}
	}
	nv := formats.NewVersion{Version: version, DisplayVersion: version, CreatedBy: c.Actor}
	if s.Kind == KindContainer {
		item := c.Item
		nv.LeadItem = &item
	}
	id, err := c.Packages.InsertVersion(ctx, pkg, nv)
	return id, false, err
}

// OnItemsRemoved marks the versions whose container the cleanup removed (R-MAL-034): the side row keeps the metadata with
// files_removed_at as history, and the version row stays, so the screens list it as Files removed. A removed detached signature alone
// changes no row but the package's revision.
func (Format) OnItemsRemoved(ctx context.Context, tx formats.Tx, e formats.ItemsEvent) error {
	port, err := portOf(e.Data)
	if err != nil {
		return err
	}
	var removed []int64
	raised := map[string]bool{}
	for _, it := range e.Items {
		s, ok := ParsePath(it.Item.Path)
		if !ok {
			continue
		}
		if s.Kind == KindContainer && it.Version != 0 {
			removed = append(removed, int64(it.Version))
		}
		if raised[s.DB] {
			continue
		}
		raised[s.DB] = true
		pkg, _, err := e.Packages.EnsureLocked(ctx, e.Repo, s.DB, s.DB)
		if err != nil {
			return err
		}
		if _, _, err := e.Packages.BumpRevision(ctx, pkg); err != nil {
			return err
		}
	}
	if len(removed) == 0 {
		return nil
	}
	return port.FilesRemoved(ctx, tx, removed)
}
