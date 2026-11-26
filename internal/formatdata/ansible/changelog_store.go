package ansible

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The changelog of a collection version (R-ANS-053): the table ansible_changelogs (migration ansible_changelogs). A row records that the
// archive was read for it, with format none when it holds none, so the reindex and the start-up check read an archive once per generator.

// PutChangelog implements ansible.DocsStore: the changelog the generator or the fill's reader read, nil for none, in the transaction that
// writes the version row.
func (s docsStore) PutChangelog(ctx context.Context, tx meta.Tx, repoID int64, name, version string, c *rows.DocChangelog) error {
	p := q.AnsibleChangelogUpsertParams{RepoID: repoID, Name: name, Version: version, Generator: docgen.Version, Format: "none"}
	if c != nil {
		p.Format, p.Source, p.Body, p.Truncated = c.Format, c.Source, c.Body, c.Truncated
	}
	return s.read(ctx, tx, func(qs *q.Queries) error { return qs.AnsibleChangelogUpsert(ctx, p) })
}

// DocChangelog implements ansible.DocsStore: the changelog of a version; found is false when it has none or was not read.
func (s docsStore) DocChangelog(ctx context.Context, repoID int64, name, version string) (rows.DocChangelog, bool, error) {
	var out rows.DocChangelog
	found := false
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		r, err := qs.AnsibleChangelogGet(ctx, q.AnsibleChangelogGetParams{RepoID: repoID, Name: name, Version: version})
		if postgres.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		found, out = true, rows.DocChangelog{Source: r.Source, Format: r.Format, Body: r.Body, Truncated: r.Truncated}
		return nil
	})
	return out, found, err
}

// changelogHead is the changelog of a version without its body, nil for none.
func changelogHead(ctx context.Context, qs *q.Queries, repoID int64, name, version string) (*rows.DocChangelog, error) {
	r, err := qs.AnsibleChangelogHead(ctx, q.AnsibleChangelogHeadParams{RepoID: repoID, Name: name, Version: version})
	if postgres.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rows.DocChangelog{Source: r.Source, Format: r.Format, Truncated: r.Truncated}, nil
}

// RebuildChangelogs is the step of the reindex of a remote (R-ANS-053): it reads the changelog of every cached archive after the
// checkpoint that was not read by this generator, one transaction per version. An archive the reader cannot read is left as it is.
func (s docsStore) RebuildChangelogs(ctx context.Context, repoID int64, reader rows.ChangelogReader, checkpoint string) (string, bool, error) {
	var list []q.AnsibleChangelogStaleVersionsRow
	if err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		list, err = qs.AnsibleChangelogStaleVersions(ctx, q.AnsibleChangelogStaleVersionsParams{
			RepoID: repoID, After: checkpoint, Generator: docgen.Version, RowLimit: docsReindexBatch,
		})
		return err
	}); err != nil {
		return checkpoint, false, err
	}
	next := checkpoint
	for _, v := range list {
		next = v.Name + "/" + v.Version
		c, err := reader.ReadChangelog(ctx, rows.ArchiveRef{ItemID: v.ItemID, Path: v.Path, SHA256: digestOf(v.Sha256), Size: v.Size})
		if err != nil {
			continue // an archive the reader refuses or cannot read keeps no row and is read again by the next reindex
		}
		if err := s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.PutChangelog(ctx, tx, repoID, v.Name, v.Version, c) }); err != nil {
			return next, false, err
		}
	}
	return next, len(list) < docsReindexBatch, nil
}
