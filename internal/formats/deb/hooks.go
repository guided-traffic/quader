package deb

import (
	"context"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
)

// OnCommit runs in the transaction that wrote an item of a Debian repository (R-DEB-006, R-DEB-008).
//
// The package file committed by the upload route creates the package row if absent, takes the row lock, claims the name, links the
// file to the version (a file of another architecture joins the version a sibling created, R-STOR-039), raises the revision and
// records the format's rows: the file, its coordinates and the raise of change_seq of its suites. Anything else is refused: a Debian
// repository holds the package files that the upload route created, on their canonical pool paths, and a file that reached it any
// other way would be one no index lists (R-DEB-001, raw deploys answer 409). A remote repository's cache holds what its upstream serves
// and owns no row here.
func (f *Format) OnCommit(ctx context.Context, tx formats.Tx, c formats.CommitEvent) error {
	// A file a remote repository fetched is its upstream's, stored as it is: its parsed index rows are the format's rows for it
	// (R-DEB-023), and no row of the local class belongs to a cache. A package file it fetched is listed as a version of its package
	// (commitFill).
	if c.Repo.Class == formats.Remote {
		if c.Origin == formats.OriginFill {
			if analyzeRemote(c.Path.Path).kind == rkByHash {
				return byHashFilled(ctx, tx, c)
			}
			return f.commitFill(ctx, tx, c)
		}
		return nil
	}
	if p, ok := c.Payload.(*uploadCommit); ok {
		return f.commitFile(ctx, tx, c, p)
	}
	return errs.E(errs.Conflict, errs.CodeConflict,
		"A package file reaches a Debian repository through PUT upload/<file> with its distribution; a raw deploy is not accepted.")
}

// byHashFilled enqueues the parse of the Packages index of the directory a by-hash file belongs to (R-DEB-023): APT fetches every index
// by its hash when the release says Acquire-By-Hash (Debian's and Ubuntu's archives do), so a remote that parsed only the indexes fetched
// at their plain paths never had rows for an APT client: no digest for the pool files (R-DEB-022), and nothing for the upstream
// catalogue and a download ahead of a version to name (R-CUR-007, R-CUR-010; found by the leg deb's case of the curation, Q-CUR-162).
// The SHA-512 of an index is not stored, so the file is not matched by its digest: the directory's Packages is enqueued, and the job
// reads the index the current release names for it, by its SHA-256, and does nothing for one it parsed already or one the release does
// not list (Q-DEB-16, Q-DEB-36). A by-hash file of another directory (i18n, source, a Contents file's) has no Packages index.
func byHashFilled(ctx context.Context, tx formats.Tx, c formats.CommitEvent) error {
	st, err := storeOf(c.Data)
	if err != nil {
		return err
	}
	dir, rel, ok, err := releaseDirOf(ctx, st, c.Repo.ID, c.Path.Path)
	if err != nil || !ok {
		return err
	}
	indexDir, _, found := strings.Cut(rel, "/"+DirByHash+"/")
	if !found || !strings.HasPrefix(path.Base(indexDir), "binary-") {
		return nil
	}
	return st.EnqueueParse(ctx, tx, c.Repo.ID, dir, indexDir+"/Packages")
}

// commitFill records the package file a remote repository cached as a version of its package, so that the repository lists what was
// fetched through it as every other format does (R-API-081, Q-REPO-330): the package and version rows, with the file linked to the
// version, the first file of a version as its lead and the file of another architecture beside it. Which package and version a file
// is comes from the remote's parsed index rows (R-DEB-022), which the fill was checked against; a file no parsed index lists, a source
// package's file and every file under dists/ get no row. The row of deb_binaries the screens read, with the file's control paragraph,
// is written by the job the commit queues, which reads the stored file's control archive (R-DEB-037): the commit reads no blob. A
// cached file gets no row of deb_coordinates and no raise of a suite: nothing builds an index of a remote.
func (f *Format) commitFill(ctx context.Context, tx formats.Tx, c formats.CommitEvent) error {
	if analyzeRemote(c.Path.Path).kind != rkContent {
		return nil
	}
	st, err := storeOf(c.Data)
	if err != nil {
		return err
	}
	e, found, err := st.EntryDigest(ctx, c.Repo.ID, c.Path.Path)
	if err != nil || !found || e.Architecture == "source" || e.Package == "" || e.Version == "" {
		return err
	}
	pkg, _, err := c.Packages.EnsureLocked(ctx, c.Repo, e.Package, e.Package)
	if err != nil {
		return err
	}
	live, err := c.Packages.Versions(ctx, pkg)
	if err != nil {
		return err
	}
	linked := false
	for _, v := range live {
		if v.Version == e.Version {
			if err := c.Packages.LinkItem(ctx, c.Item, v.ID, v.LeadItem == 0); err != nil {
				return err
			}
			linked = true
			break
		}
	}
	if !linked {
		if _, err := c.Packages.InsertVersion(ctx, pkg, formats.NewVersion{Version: e.Version, DisplayVersion: e.Version, LeadItem: &c.Item}); err != nil {
			return err
		}
	}
	if _, _, err := c.Packages.BumpRevision(ctx, pkg); err != nil {
		return err
	}
	return st.QueueDescribe(ctx, tx, c.Repo.ID, e.Package)
}

// removeCached takes the version of a cached package file out with the file that was its lead (R-API-081): a remote repository lists
// what it holds. The file of another architecture that stays loses its link with the version and is listed again when it is fetched
// again; a file that was linked beside the lead takes nothing with it. The row of each removed file's control paragraph goes with the
// file (R-DEB-037).
func (f *Format) removeCached(ctx context.Context, tx formats.Tx, e formats.ItemsEvent) error {
	if err := forgetCached(ctx, tx, e); err != nil {
		return err
	}
	locked := map[string]formats.PackageRef{}
	for _, it := range e.Items {
		if it.Version == 0 || !strings.HasPrefix(it.Item.Path, dirPool+"/") {
			continue
		}
		name := f.Classify(formats.Path{Path: it.Item.Path}).Package
		if name == "" {
			continue
		}
		pkg, ok := locked[name]
		if !ok {
			var err error
			if pkg, _, err = e.Packages.EnsureLocked(ctx, e.Repo, name, name); err != nil {
				return err
			}
			locked[name] = pkg
		}
		live, err := e.Packages.Versions(ctx, pkg)
		if err != nil {
			return err
		}
		for _, v := range live {
			// The removal of the item cleared the version's link to it (the foreign key sets lead_item_id to NULL): a version of the
			// removed file without a lead lost its lead with it.
			if v.ID == it.Version && (v.LeadItem == 0 || v.LeadItem == it.Item.ID) {
				if err := e.Packages.TrashOrDeleteVersion(ctx, v.ID, true); err != nil {
					return err
				}
			}
		}
	}
	for _, pkg := range locked {
		if _, _, err := e.Packages.BumpRevision(ctx, pkg); err != nil {
			return err
		}
	}
	return nil
}

// forgetCached deletes the rows of the removed files of a remote's cache (R-DEB-037).
func forgetCached(ctx context.Context, tx formats.Tx, e formats.ItemsEvent) error {
	st, err := storeOf(e.Data)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(e.Items))
	for _, it := range e.Items {
		if strings.HasPrefix(it.Item.Path, dirPool+"/") {
			ids = append(ids, it.Item.ID)
		}
	}
	return st.ForgetCached(ctx, tx, e.Repo.ID, ids)
}

func (f *Format) commitFile(ctx context.Context, tx formats.Tx, c formats.CommitEvent, p *uploadCommit) error {
	st, err := storeOf(c.Data)
	if err != nil {
		return err
	}
	in := p.in
	pkg, _, err := c.Packages.EnsureLocked(ctx, c.Repo, in.name(), in.name())
	if err != nil {
		return err
	}
	live, err := c.Packages.Versions(ctx, pkg)
	if err != nil {
		return err
	}
	first := len(live) == 0
	if first && p.shadowSkipped {
		// The last version went away between the shadowing check and this commit: the upload gives the repository its first version
		// of the name, and R-REPO-048 has not been asked. The client retries, and the check then runs.
		return errs.E(errs.Unavailable, errs.CodeShadowCheckUnavailable,
			in.name()+" lost its last version while this upload ran; try again.", errs.WithRetryAfter(time.Second))
	}
	if err := c.Packages.Claim(ctx, formats.Target{Package: in.name(), Version: in.version(), Path: in.path, Actions: []formats.Action{formats.ActionDeploy}},
		formats.OwnershipWrite{Publish: true, FirstVersion: first}); err != nil {
		return err
	}
	var claim string
	if first {
		res, err := c.Packages.AutoClaim(ctx, in.name())
		if err != nil {
			return err
		}
		claim = res.String()
	}
	var vid formats.VersionID
	for _, v := range live {
		if v.Version == in.version() {
			vid = v.ID
		}
	}
	if vid != 0 {
		err = c.Packages.LinkItem(ctx, c.Item, vid, false)
	} else {
		_, err = c.Packages.InsertVersion(ctx, pkg, formats.NewVersion{
			Version: in.version(), DisplayVersion: in.version(), LeadItem: &c.Item, CreatedBy: c.Actor,
		})
	}
	if err != nil {
		return err
	}
	if _, _, err := c.Packages.BumpRevision(ctx, pkg); err != nil {
		return err
	}
	res, err := st.RecordUpload(ctx, tx, rows.UploadInput{
		Binary: rows.Binary{
			ItemID: c.Item.ID, RepoID: c.Repo.ID, Path: c.Path.Path, SHA256: digestOf(c.Digests.SHA256), Size: c.Size, Package: in.name(), Version: in.version(), Architecture: in.architecture(),
			Source: in.pkg.Source, Control: controlText(in), Coordinates: p.coords,
		},
		Actor: c.Actor,
	})
	if err != nil {
		return err
	}
	p.result = res
	if res.Created {
		pub := rows.Published{
			RepoKey: c.Repo.Name.String(), Path: c.Path.Path, Package: in.name(), Version: in.version(), Size: c.Size, Actor: c.Actor, Claim: claim,
		}
		copy(pub.SHA256[:], c.Digests.SHA256)
		if first {
			pub.ShadowUndetermined = p.shadow.Undetermined
			for _, x := range p.shadow.Shadowed {
				pub.Shadowed = append(pub.Shadowed, rows.ShadowPair{Virtual: x.Virtual, Remote: x.Remote})
			}
		}
		if err := st.RecordPublished(ctx, tx, pub); err != nil {
			return err
		}
	}
	return c.Packages.SetProperties(ctx, c.Item, properties(in.pkg, coordStrings(res.Coordinates)))
}

func controlText(in *incoming) string {
	b, err := in.pkg.Control.Bytes()
	if err != nil {
		return "" // the paragraph was written once already when the package was read
	}
	return string(b)
}

// OnItemsRemoved runs in the transaction that removed items of a Debian repository: the deletion of a package file through the
// format's route, through /files/ or the items API, and its move to the trash can (R-DEB-013). The rows of a file in the trash can
// stay (a build lists files that are not trashed) and go with its permanent removal; change_seq of its suites rises either way, so
// the next generation no longer lists it. A version that has no other file goes with its last one. The deletion of a package file is
// journaled, so a restore withholds it again (R-DEB-032, Q-DEB-22); the permanent removal of a file already in the trash can writes no
// second event.
//
// The ownership of the name was decided before, in the same transaction: by the delete route under the package row lock, and for a
// delete through /files/ or the items API by the generic layer. The writer of this hook is bound to no request and decides none.
func (f *Format) OnItemsRemoved(ctx context.Context, tx formats.Tx, e formats.ItemsEvent) error {
	if e.Repo.Class == formats.Remote && len(e.Items) > 0 {
		return f.removeCached(ctx, tx, e)
	}
	if e.Repo.Class != formats.Local || len(e.Items) == 0 {
		return nil
	}
	st, err := storeOf(e.Data)
	if err != nil {
		return err
	}
	byPackage := map[string][]formats.RemovedItem{}
	for _, it := range e.Items {
		info := f.Classify(formats.Path{Path: it.Item.Path})
		if info.Kind != formats.Content || info.Package == "" || !strings.HasPrefix(it.Item.Path, dirPool+"/") {
			continue
		}
		byPackage[info.Package] = append(byPackage[info.Package], it)
	}
	names := make([]string, 0, len(byPackage))
	for n := range byPackage {
		names = append(names, n)
	}
	sort.Strings(names)
	purge := e.Reason != "trash"
	journaled := e.Reason == "delete" || e.Reason == "trash"
	for _, name := range names {
		var pkg formats.PackageRef
		bumped := false
		for _, it := range byPackage[name] {
			gone, found, err := st.Removed(ctx, tx, e.Repo.ID, it.Item.ID, it.Item.Path, digestOf(it.SHA256), purge)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			if !bumped {
				if pkg, _, err = e.Packages.EnsureLocked(ctx, e.Repo, name, name); err != nil {
					return err
				}
				if _, _, err = e.Packages.BumpRevision(ctx, pkg); err != nil {
					return err
				}
				bumped = true
			}
			if gone.VersionFiles == 0 && it.Version != 0 {
				if err := e.Packages.TrashOrDeleteVersion(ctx, it.Version, purge); err != nil {
					return err
				}
			}
			if journaled {
				if err := st.RecordPackageDeleted(ctx, tx, rows.PackageDeleted{
					RepoID: e.Repo.ID, RepoKey: e.Repo.Name.String(), RemovedFile: gone, Actor: e.Actor,
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// OnItemsRestored runs in the transaction that brought removed files back from the trash can (R-DEB-013, R-STOR-023): change_seq of
// the suites of each file rises again, so the next generation lists it.
func (f *Format) OnItemsRestored(ctx context.Context, tx formats.Tx, e formats.ItemsEvent) error {
	if e.Repo.Class != formats.Local || len(e.Items) == 0 {
		return nil
	}
	st, err := storeOf(e.Data)
	if err != nil {
		return err
	}
	for _, it := range e.Items {
		info := f.Classify(formats.Path{Path: it.Item.Path})
		if info.Package == "" || !strings.HasPrefix(it.Item.Path, dirPool+"/") {
			continue
		}
		if _, found, err := st.Restored(ctx, tx, e.Repo.ID, it.Item.ID); err != nil {
			return err
		} else if !found {
			continue
		}
		pkg, _, err := e.Packages.EnsureLocked(ctx, e.Repo, info.Package, info.Package)
		if err != nil {
			return err
		}
		if _, _, err := e.Packages.BumpRevision(ctx, pkg); err != nil {
			return err
		}
	}
	return nil
}

// digestOf is the SHA-256 digest of the stored content as the fixed-size value the rows carry; a digest of another length is the zero
// value.
func digestOf(sum []byte) [32]byte {
	var out [32]byte
	if len(sum) == len(out) {
		copy(out[:], sum)
	}
	return out
}
