package ansible

import (
	"context"
	"encoding/json"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// deprecationStore is the group of the port that keeps the deprecation of a collection (R-ANS-060): the table
// ansible_collection_deprecations of migration 00137. The statements are in queries/ansible_local.sql and ansible_remote.sql.
type deprecationStore struct{ *group }

// CollectionDeprecation reads the deprecation of a collection of a repository.
func (s deprecationStore) CollectionDeprecation(ctx context.Context, tx meta.Tx, repoID int64, name string) (rows.CollectionDeprecation, bool, error) {
	var out rows.CollectionDeprecation
	found := false
	err := s.read(ctx, tx, func(qs *q.Queries) error {
		list, err := qs.AnsibleCollectionDeprecation(ctx, q.AnsibleCollectionDeprecationParams{RepoID: repoID, Name: name})
		if err != nil || len(list) == 0 {
			return err
		}
		out, found = rows.CollectionDeprecation{By: list[0].DeprecatedBy, Message: list[0].Message, At: list[0].DeprecatedAt.Time.UTC()}, true
		return nil
	})
	return out, found, err
}

// SetCollectionDeprecation sets, changes or clears a local collection's deprecation and records package.metadata_changed with the
// collection as the package and no version, and the message where it has one.
func (s deprecationStore) SetCollectionDeprecation(ctx context.Context, tx meta.Tx, repoID int64, name string, d *rows.CollectionDeprecation, a rows.Audit) (bool, error) {
	qs, err := txQueries(tx)
	if err != nil {
		return false, err
	}
	details := map[string]any{"package": name, "change": audit.ChangeUndeprecate}
	var n int64
	if d == nil {
		n, err = qs.AnsibleClearCollectionDeprecation(ctx, q.AnsibleClearCollectionDeprecationParams{RepoID: repoID, Name: name})
	} else {
		details["change"] = audit.ChangeDeprecate
		if d.Message != "" {
			details["message"] = d.Message
		}
		n, err = qs.AnsibleSetCollectionDeprecation(ctx, q.AnsibleSetCollectionDeprecationParams{
			RepoID: repoID, Name: name, DeprecatedBy: d.By, Message: d.Message,
		})
	}
	if err != nil || n == 0 {
		return false, err
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return false, err
	}
	return true, s.o.Recorder.RecordTx(ctx, tx, s.event(ctx, audit.EventPackageMetadataChanged, a, name, "", raw, nil))
}

// SetUpstreamDeprecated records the flag of a remote's collection document.
func (s deprecationStore) SetUpstreamDeprecated(ctx context.Context, tx meta.Tx, repoID int64, name string, deprecated bool) error {
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	if !deprecated {
		_, err = qs.AnsibleClearCollectionDeprecation(ctx, q.AnsibleClearCollectionDeprecationParams{RepoID: repoID, Name: name})
		return err
	}
	return qs.AnsibleSetUpstreamDeprecated(ctx, q.AnsibleSetUpstreamDeprecatedParams{RepoID: repoID, Name: name})
}

// ViewDeprecated lists the generic versions of the repositories whose collection is deprecated.
func (s viewStore) ViewDeprecated(ctx context.Context, repos []string, limit int) ([]int64, error) {
	var out []int64
	err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		out, err = qs.AnsibleViewDeprecated(ctx, q.AnsibleViewDeprecatedParams{Repos: repos, RowLimit: int32(min(limit, 1<<20))}) //nolint:gosec // bounded above
		return err
	})
	return out, err
}
