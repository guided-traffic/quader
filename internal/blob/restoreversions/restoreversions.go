// Package restoreversions is the logic of `quader admin storage restore-versions --before <t> [--connection <id>]` (R-HA-025
// step 5, backup-and-restore.md R-BAK-030): after a point-in-time restore of the database, the objects that the old
// installation's collector deleted after t exist in a versioned bucket under a delete marker. The package finds the copies whose
// object is missing (the check of `quader admin storage verify` over every present copy, storage.md R-STOR-058) and removes the
// delete markers younger than t from exactly those keys, each on its connection, so that the object of the version before it is
// current again.
//
// It removes nothing else: a delete marker of a key that no copy row references stays (the orphan sweep or the grace period
// of the collector deals with those objects after the deletions resume), a delete marker older than t stays (the object was
// already gone at t), and no object version is ever deleted.
package restoreversions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/blob/verify"
)

// Outcomes of one copy.
const (
	// OutcomeRestored: the delete markers younger than t are removed and the object is there again.
	OutcomeRestored = "restored"
	// OutcomeWouldRestore: a dry run found a delete marker younger than t above an object version.
	OutcomeWouldRestore = "would_restore"
	// OutcomeNoDeleteMarker: the key has no delete marker younger than t, so the object is not missing because of a deletion
	// after t.
	OutcomeNoDeleteMarker = "no_delete_marker"
	// OutcomeNoVersion: the key has no object version below its delete markers, so nothing can be made current again.
	OutcomeNoVersion = "no_version"
	// OutcomeStillMissing: the markers are removed and the object is still not there.
	OutcomeStillMissing = "still_missing"
)

// Store is the object store side of a run on one connection: the existence check of the verification, the listing of the
// versions of a key and the removal of one version. *s3.Client satisfies it; the credential must be one that may list and
// delete object versions.
type Store interface {
	verify.Objects
	ListVersions(ctx context.Context, key string) ([]s3.ObjectVersion, error)
	DeleteVersion(ctx context.Context, key, versionID string) error
}

// Conn is one connection of the configuration with the client a run uses on it.
type Conn struct {
	ID    string
	Store Store
}

// Options configure a run.
type Options struct {
	DB verify.Source
	// Connections are the connections of the configuration; Connection limits the run to one of them (--connection), empty
	// runs on every one (backup-and-restore.md R-BAK-030).
	Connections []Conn
	Connection  string
	// Before is t: delete markers created after it are removed.
	Before time.Time
	// DryRun lists what a run would do and removes nothing.
	DryRun bool
	// Concurrency and PageSize are those of the verification.
	Concurrency, PageSize int
	Log                   *slog.Logger
}

// Entry is the result for one copy whose object was missing.
type Entry struct {
	Connection string `json:"connection"`
	SHA256     string `json:"sha256"`
	Key        string `json:"key"`
	Outcome    string `json:"outcome"`
	// MarkersRemoved is the number of delete markers this run removed (with DryRun: would remove) from the key.
	MarkersRemoved int `json:"markersRemoved"`
	// ItemCount is the number of items that reference the blob, when no other copy of it was there.
	ItemCount int64 `json:"itemCount"`
}

// Report is the result of a run.
type Report struct {
	Before time.Time `json:"before"`
	DryRun bool      `json:"dryRun,omitempty"`
	// Checked is the number of blobs read; Missing the number of copies without an object when the run started.
	Checked int64 `json:"checked"`
	Missing int64 `json:"missing"`
	// Restored is the number of copies whose object is current again (with DryRun: would be); MarkersRemoved the number of
	// delete markers removed.
	Restored       int64   `json:"restored"`
	MarkersRemoved int64   `json:"markersRemoved"`
	Entries        []Entry `json:"entries"`
	// Remaining is the number of copies that are still without an object after the run; SizeMismatch those of another size,
	// which this command does not repair.
	Remaining    int64 `json:"remaining"`
	SizeMismatch int64 `json:"sizeMismatch"`
	// Lost is the number of findings that keep the exit status at 5 after the run (R-STOR-058): a blob none of whose checked
	// copies has its object, and, with Connection set, a copy on that connection that is still missing or of another size.
	Lost int64 `json:"lost"`
	// Unchecked is the number of copies the store did not answer for.
	Unchecked int `json:"unchecked"`
	// ItemsAffected is the number of items that reference a blob that is lost after the run.
	ItemsAffected int64 `json:"itemsAffected"`
}

// OK reports whether nothing is lost after the run and the store answered for every copy.
func (r *Report) OK() bool { return r.Lost == 0 && r.Unchecked == 0 }

// Run finds the copies without an object (the check of `quader admin storage verify`, R-STOR-058) and removes the delete
// markers younger than o.Before from their keys, each on its connection. It returns the report so far with the error when it
// ends early; a permission error of a store (AccessDenied on the listing of versions or on the removal) ends it at once,
// because every further call would fail the same way.
func Run(ctx context.Context, o Options) (*Report, error) {
	if len(o.Connections) == 0 || o.DB == nil {
		return nil, errors.New("restore-versions: a database source and a connection are required")
	}
	if o.Before.IsZero() {
		return nil, errors.New("restore-versions: the time before which the versions are restored is required")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	stores := map[string]Store{}
	conns := make([]verify.Conn, 0, len(o.Connections))
	for _, c := range o.Connections {
		stores[c.ID] = c.Store
		conns = append(conns, verify.Conn{ID: c.ID, Objects: c.Store})
	}
	// One item per lost blob keeps the memory of the verification small; the report counts the items instead of listing them.
	vr, err := verify.Run(ctx, verify.Options{
		DB: o.DB, Connections: conns, Connection: o.Connection, Concurrency: o.Concurrency, PageSize: o.PageSize, MaxItems: 1, Log: o.Log,
	})
	if vr == nil {
		return nil, err
	}
	rep := &Report{Before: o.Before, DryRun: o.DryRun, Checked: vr.Checked, Entries: []Entry{}, Unchecked: len(vr.Unchecked)}
	if err != nil {
		return rep, err
	}
	lost := map[string]verify.Finding{}
	for _, f := range vr.Findings {
		lost[f.SHA256] = f
	}
	for _, cf := range vr.Copies {
		if cf.Kind == verify.KindMissing {
			rep.Missing++
		}
	}
	restored := map[string]bool{}
	var named []string // the copies that stay findings: still missing, or of another size
	for _, cf := range vr.Copies {
		if cf.Kind != verify.KindMissing {
			rep.SizeMismatch++
			named = append(named, cf.SHA256)
			continue
		}
		e, err := repair(ctx, o, stores[cf.Connection], cf)
		e.ItemCount = lost[cf.SHA256].ItemCount
		if !o.DryRun {
			rep.MarkersRemoved += int64(e.MarkersRemoved)
		}
		if err != nil {
			if e.Outcome != "" {
				rep.Entries = append(rep.Entries, e)
			}
			return rep, fmt.Errorf("copy %s of blob %s: %w", cf.Connection, cf.SHA256, err)
		}
		rep.Entries = append(rep.Entries, e)
		if e.Outcome == OutcomeRestored || e.Outcome == OutcomeWouldRestore {
			rep.Restored++
			restored[cf.SHA256] = true
			continue
		}
		rep.Remaining++
		named = append(named, cf.SHA256)
	}
	for sha, f := range lost {
		if !restored[sha] {
			rep.Lost++
			rep.ItemsAffected += f.ItemCount
		}
	}
	if o.Connection != "" {
		// a copy on the named connection that is still missing or of another size is a finding of that connection
		for _, sha := range named {
			if _, counted := lost[sha]; !counted {
				rep.Lost++
			}
		}
	}
	return rep, nil
}

// repair removes the delete markers of one copy's key that are younger than t.
func repair(ctx context.Context, o Options, store Store, f verify.CopyFinding) (Entry, error) {
	e := Entry{Connection: f.Connection, SHA256: f.SHA256, Key: f.Key}
	versions, err := store.ListVersions(ctx, f.Key)
	if err != nil {
		return e, fmt.Errorf("listing the versions of %s: %w", f.Key, err)
	}
	var markers []s3.ObjectVersion
	objects := 0
	for _, v := range versions {
		switch {
		case !v.Marker:
			objects++
		case v.Modified.After(o.Before):
			markers = append(markers, v)
		}
	}
	switch {
	case len(markers) == 0:
		e.Outcome = OutcomeNoDeleteMarker
		return e, nil
	case objects == 0:
		e.Outcome = OutcomeNoVersion
		return e, nil
	case o.DryRun:
		e.Outcome, e.MarkersRemoved = OutcomeWouldRestore, len(markers)
		return e, nil
	}
	for _, m := range markers {
		if err := store.DeleteVersion(ctx, f.Key, m.ID); err != nil {
			return e, fmt.Errorf("removing the delete marker %s of %s: %w", m.ID, f.Key, err)
		}
		e.MarkersRemoved++
		o.Log.Info("delete marker removed", "connection", f.Connection, "key", f.Key, "versionId", m.ID, "modified", m.Modified.UTC().Format(time.RFC3339))
	}
	if _, err := store.Head(ctx, f.Key); err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			e.Outcome = OutcomeStillMissing
			return e, nil
		}
		return e, fmt.Errorf("checking %s after the repair: %w", f.Key, err)
	}
	e.Outcome = OutcomeRestored
	return e, nil
}
