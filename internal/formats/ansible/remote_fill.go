package ansible

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/formats/ansible/version"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// The archives a remote repository caches (R-ANS-018, R-ANS-020). The announced digest (remote_gate.go) ties the bytes to the version
// document; the declared identity of the archive is the second check, that the upstream served the archive that was asked for: its
// MANIFEST.json must name the collection and the version of the path. The checks run before anything is stored, and no byte of an
// archive reaches a client before they ran (Q-ARCH-267): an archive that fails answers 502 to every request of the fetch and leaves no
// blob, no item and no row.

// InspectsFill declares the hook for the archive of a collection version and of a role version.
func (remoteAdapter) InspectsFill(p formats.Path) bool {
	t, err := parseTarget(p)
	return err == nil && (t.kind == kindArchive || t.kind == kindRoleArchive)
}

// InspectFill reads the spooled archive. A collection archive is read as an upload is (R-ANS-010, within the bounds of R-ANS-031)
// and its declared identity must be the path's: another namespace, name or version is 502 upstream_unavailable with the reason
// identity_mismatch, and an archive the reader refuses cannot be checked and is refused alike. A role archive has no manifest: it
// must be a gzip-compressed tar archive within the same bounds, and nothing else of it is claimed (R-ANS-020, H-840).
func (a remoteAdapter) InspectFill(ctx context.Context, rc formats.RemoteContext, p formats.Path, sp formats.Spooled) (*formats.Inspection, error) {
	t, err := parseTarget(p)
	if err != nil {
		return nil, err
	}
	body, err := sp.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	lim := untrusted.DefaultLimits().Tighten(a.f.Descriptor().Limits)
	if t.kind == kindRoleArchive {
		if err := checkRoleArchive(ctx, body, lim); err != nil {
			return nil, err
		}
		// The role's package and the version's name, which the commit records as a cached collection's (R-ANS-056); nothing of the
		// archive is claimed.
		return &formats.Inspection{
			Package: RolePackage(t.roleID), Version: t.roleRef, Properties: map[string]string{"ansible.role_id": t.roleID},
			Payload: &inspectedRole{id: t.roleID, fetchDocs: memberSettings(rc.Settings).FetchDocumentation},
		}, nil
	}
	arc, err := collection.ReadArchive(ctx, body, lim)
	if err != nil {
		var ce *collection.Error
		if errors.As(err, &ce) {
			return nil, errs.E(errs.UpstreamFailed, errs.CodeUpstreamFailed,
				"The upstream served an archive for "+t.ns+"."+t.name+" "+t.version+" that Quader refuses (invalid_archive): "+ce.Detail, errs.WithCause(err))
		}
		return nil, err
	}
	if arc.Namespace != t.ns || arc.Name != t.name || arc.Version != t.version {
		return nil, errs.E(errs.UpstreamFailed, errs.CodeUpstreamFailed,
			"The upstream served an archive for "+t.ns+"."+t.name+" "+t.version+" that declares "+arc.Namespace+"."+arc.Name+" "+arc.Version+" (identity_mismatch).")
	}
	return &formats.Inspection{
		Package: t.ns + "." + t.name, Version: t.version,
		Properties: map[string]string{
			"ansible.namespace": arc.Namespace, "ansible.name": arc.Name, "ansible.requires_ansible": arc.RequiresAnsible,
			"ansible.dependency_count": strconv.Itoa(len(arc.Dependencies)),
		},
		Payload: &inspected{archive: arc},
	}, nil
}

// CommitFill records the generic package and version rows of a collection archive a remote cached, in the transaction of the fill
// (FillCommitter, Q-ANS-76). The package screens list a remote repository's cache by these rows (R-UI-020), so a collection installed
// through a remote has a package, a version and the files of the cached archive, and the row of its manifest (putManifest) gives the page
// what a local collection's page has. A version that has a row already gets the item linked again, and a role archive has no inspection
// and is stored as it came.
func (a remoteAdapter) CommitFill(ctx context.Context, tx formats.Tx, c formats.CommitEvent) error {
	in := c.Inspection
	if in == nil || in.Package == "" || in.Version == "" {
		return nil
	}
	if r, ok := in.Payload.(*inspectedRole); ok {
		return commitRole(ctx, tx, c, r)
	}
	pkg, _, err := c.Packages.EnsureLocked(ctx, c.Repo, in.Package, in.Package)
	if err != nil {
		return err
	}
	live, err := c.Packages.Versions(ctx, pkg)
	if err != nil {
		return err
	}
	linked := false
	at := time.Now()
	for _, v := range live {
		if version.Same(v.Version, in.Version) {
			if err := c.Packages.LinkItem(ctx, c.Item, v.ID, true); err != nil {
				return err
			}
			linked = true
			break
		}
	}
	if !linked {
		if _, err := c.Packages.InsertVersion(ctx, pkg, formats.NewVersion{Version: in.Version, DisplayVersion: in.Version, LeadItem: &c.Item}); err != nil {
			return err
		}
		_, bumped, err := c.Packages.BumpRevision(ctx, pkg)
		if err != nil {
			return err
		}
		at = bumped
	}
	props := map[string]string{}
	for k, v := range in.Properties {
		if v != "" {
			props[k] = v
		}
	}
	if err := c.Packages.SetProperties(ctx, c.Item, props); err != nil {
		return err
	}
	return a.putManifest(ctx, tx, c, at)
}

// putManifest records the manifest the inspection read, as a local publish records it (Q-ANS-76): the screens read the description, the
// licences, the dependencies, the links and Requires Ansible of a remote's collection from the same row as a local one's. The archive was
// read under the bounds of R-ANS-031 by InspectFill; nothing is read again.
func (a remoteAdapter) putManifest(ctx context.Context, tx formats.Tx, c formats.CommitEvent, at time.Time) error {
	ins, _ := c.Inspection.Payload.(*inspected)
	if ins == nil || ins.archive == nil {
		return nil
	}
	arc := ins.archive
	sv, err := version.Parse(arc.Version)
	if err != nil {
		return nil //nolint:nilerr // the reader accepted the version; a string that does not parse here is listed without a manifest
	}
	st, err := storeOf(c.Data)
	if err != nil {
		return err
	}
	return st.PutCachedVersion(ctx, tx, rows.VersionRow{
		RepoID: c.Repo.ID, Name: c.Inspection.Package, Version: arc.Version, SortKey: version.SortKey(sv), ItemID: c.Item.ID,
		SHA256: sumOf(c.Digests.SHA256), Size: c.Size, RequiresAnsible: arc.RequiresAnsible, Dependencies: arc.Dependencies,
		CollectionInfo: arc.Info, PublishedAt: at,
	})
}

// inspectedRole is what InspectFill hands the commit of a role archive.
type inspectedRole struct {
	id        string
	fetchDocs bool
}

// commitRole records the generic package and version rows of a role archive a remote cached (R-ANS-056): the package "role/<id>"
// (Q-ANS-36) with the display name "<owner>.<name>" when the lookup's facts are there, the version named as the upstream names it, and
// the fetch of the role's README when it has none for the role's commit. A role whose facts are not there yet keeps the display name
// "role/<id>" until its next lookup.
func commitRole(ctx context.Context, tx formats.Tx, c formats.CommitEvent, r *inspectedRole) error {
	st, err := storeOf(c.Data)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(r.id, 10, 64)
	if err != nil {
		return errs.E(errs.Internal, errs.CodeInternal, "A role archive names no role id.")
	}
	facts, found, err := st.Role(ctx, tx, c.Repo.ID, id)
	if err != nil {
		return err
	}
	in := c.Inspection
	display := in.Package
	if found {
		display = facts.Owner + "." + facts.Name
	}
	pkg, _, err := c.Packages.EnsureLocked(ctx, c.Repo, in.Package, display)
	if err != nil {
		return err
	}
	if _, err := c.Packages.SetDisplayName(ctx, c.Repo, in.Package, display); err != nil {
		return err
	}
	live, err := c.Packages.Versions(ctx, pkg)
	if err != nil {
		return err
	}
	linked := false
	for _, v := range live {
		if v.Version == in.Version {
			if err := c.Packages.LinkItem(ctx, c.Item, v.ID, true); err != nil {
				return err
			}
			linked = true
			break
		}
	}
	if !linked {
		if _, err := c.Packages.InsertVersion(ctx, pkg, formats.NewVersion{Version: in.Version, DisplayVersion: in.Version, LeadItem: &c.Item}); err != nil {
			return err
		}
		if _, _, err := c.Packages.BumpRevision(ctx, pkg); err != nil {
			return err
		}
	}
	if err := c.Packages.SetProperties(ctx, c.Item, in.Properties); err != nil {
		return err
	}
	return queueRoleReadme(ctx, tx, st, c.Repo.ID, id, r.fetchDocs)
}

// checkRoleArchive reads a role archive to its end within the bounds of R-ANS-031: a gzip-compressed tar archive without a hard link, a
// duplicate name, a bomb or data after its end. A symbolic link is admitted when its target stays inside the role's directory, the first
// segment of its name, which is the check ansible-galaxy role install makes before it extracts one (Q-ANS-91); the link is not followed.
func checkRoleArchive(ctx context.Context, body io.Reader, lim untrusted.Limits) error {
	ctx, cancel := lim.Context(ctx)
	defer cancel()
	tr := untrusted.NewTarGz(ctx, body, lim, untrusted.ContainedSymlinks())
	refused := func(err error) error {
		return errs.E(errs.UpstreamFailed, errs.CodeUpstreamFailed, "The upstream served a role archive that Quader refuses (invalid_archive): "+err.Error(), errs.WithCause(err))
	}
	for {
		e, err := tr.Next()
		if errors.Is(err, io.EOF) && !untrusted.IsRefused(err) && !untrusted.IsTooLarge(err) {
			return nil
		}
		if err != nil {
			if untrusted.IsRefused(err) || untrusted.IsTooLarge(err) {
				return refused(err)
			}
			return err
		}
		if e.Link {
			if top, _, _ := strings.Cut(strings.TrimPrefix(e.Name, "./"), "/"); e.Target != top && !strings.HasPrefix(e.Target, top+"/") {
				return refused(untrusted.Refused(fmt.Sprintf("entry %q is a link that leaves the role's directory %q", e.Name, top)))
			}
			continue
		}
		if e.Dir {
			continue
		}
		if _, err := tr.Stream(io.Discard); err != nil {
			if untrusted.IsRefused(err) || untrusted.IsTooLarge(err) {
				return refused(err)
			}
			return err
		}
	}
}
