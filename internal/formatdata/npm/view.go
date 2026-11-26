package npm

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/npm/rows"
	"github.com/guided-traffic/quader/internal/formats/npm/tarball"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The reads of the package view (R-API-029): what the screens show of an npm version
// beyond the generic rows, and the two searches that need the manifest. The
// statements are in internal/meta/postgres/queries/npm_view.sql.

// ViewSummaries reads the description and the keywords of the manifests of the
// versions. A version without an npm row is absent.
func (s *Store) ViewSummaries(ctx context.Context, ids []int64) ([]rows.ViewSummary, error) {
	var out []rows.ViewSummary
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		out = out[:0]
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rs, err := qs.NPMViewSummaries(ctx, ids)
		if err != nil {
			return err
		}
		for _, r := range rs {
			var kw []string
			if err := json.Unmarshal([]byte(r.Keywords), &kw); err != nil {
				kw = nil
			}
			out = append(out, rows.ViewSummary{VersionID: r.VersionID, Description: r.Description, Keywords: nonEmpty(kw), Deprecated: r.Deprecated})
		}
		return nil
	})
	return out, err
}

// nonEmpty drops the empty entries of a keyword list.
func nonEmpty(in []string) []string {
	out := in[:0:0]
	for _, k := range in {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

// ViewDetails reads the manifest, the deprecation and the dist-tags of the versions,
// and their README text when asked: the stored text of a publish, else the file the
// read of the tarball named, read from the stored tarball outside the transaction
// (R-NPM-051). The dist-tags of a remote's version are those of the stored package
// document, read outside the transaction up to its member dist-tags (R-NPM-050).
func (s *Store) ViewDetails(ctx context.Context, ids []int64, withReadme bool) ([]rows.ViewDetail, error) {
	var out []rows.ViewDetail
	var blobs, docs []blobRef
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		out, blobs, docs = out[:0], blobs[:0], docs[:0]
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rs, err := qs.NPMViewDetails(ctx, q.NPMViewDetailsParams{Ids: ids, WithReadme: withReadme})
		if err != nil {
			return err
		}
		for _, r := range rs {
			tags, err := parseTags(r.DistTags)
			if err != nil {
				return err
			}
			out = append(out, rows.ViewDetail{
				VersionID: r.VersionID, Manifest: []byte(r.Manifest), Deprecated: r.Deprecated,
				HasReadme: r.HasReadme, Readme: r.Readme, ReadmeFilename: r.ReadmeFilename, DistTags: tags,
			})
			blobs = append(blobs, blobRef{sha256: r.Sha256, size: r.Size})
			docs = append(docs, blobRef{sha256: r.DocSha256, size: r.DocSize})
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	read := map[string]map[string]string{}
	for i := range out {
		if d := &out[i]; len(d.DistTags) == 0 && len(docs[i].sha256) == 32 {
			k := string(docs[i].sha256)
			if _, ok := read[k]; !ok {
				read[k] = s.storedTags(ctx, docs[i])
			}
			d.DistTags = read[k]
		}
	}
	if !withReadme {
		return out, nil
	}
	for i := range out {
		if d := &out[i]; d.HasReadme && d.Readme == "" && d.ReadmeFilename != "" {
			d.Readme = s.readme(ctx, blobs[i], d.ReadmeFilename)
		}
	}
	return out, nil
}

// storedTags reads the dist-tags of a stored package document by streaming it up to that member, which the registry writes before the
// versions; a document that holds them after the versions, cannot be read or is no JSON object gives none. A tag is shown only for a
// version the reader may read (the format's visibleTags).
func (s *Store) storedTags(ctx context.Context, b blobRef) map[string]string {
	if s.o.Blobs == nil {
		return nil
	}
	var sum [32]byte
	copy(sum[:], b.sha256)
	body, err := s.o.Blobs.Open(ctx, sum, b.size)
	if err != nil {
		return nil
	}
	defer func() { _ = body.Close() }()
	return tagsOf(json.NewDecoder(io.LimitReader(body, maxDocumentBytes)))
}

// tagsOf reads the top-level member dist-tags of a document at the decoder, and stops at the member versions.
func tagsOf(dec *json.Decoder) map[string]string {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil
		}
		switch k {
		case "dist-tags":
			var tags map[string]any
			if dec.Decode(&tags) != nil {
				return nil
			}
			out := map[string]string{}
			for t, v := range tags {
				if s, ok := v.(string); ok {
					out[t] = s
				}
			}
			return out
		case "versions":
			return nil
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return nil
		}
	}
	return nil
}

// blobRef names the stored tarball of a version.
type blobRef struct {
	sha256 []byte
	size   int64
}

// readmeTimeout bounds the inflation of a tarball up to its README.
const readmeTimeout = 20 * time.Second

// readme reads the README the read of a tarball named from the stored tarball, at most the 64 KiB a published README keeps, by
// streaming it up to that file (R-NPM-051): a tarball that cannot be read, one without the file and a replica without the blob store
// give none, which the screens show as a version without a README.
func (s *Store) readme(ctx context.Context, b blobRef, name string) string {
	if s.o.Blobs == nil || len(b.sha256) != 32 {
		return ""
	}
	var sum [32]byte
	copy(sum[:], b.sha256)
	body, err := s.o.Blobs.Open(ctx, sum, b.size)
	if err != nil {
		return ""
	}
	defer func() { _ = body.Close() }()
	lim := untrusted.DefaultLimits()
	lim.Deadline = readmeTimeout
	data, found, err := tarball.ReadFile(ctx, body, lim, name, maxReadme)
	if err != nil || !found {
		return ""
	}
	return clean(string(data), maxReadme)
}

// ViewFind returns the ids of at most limit versions of the repositories (by key)
// whose description contains text or whose keywords contain keyword.
func (s *Store) ViewFind(ctx context.Context, repos []string, text, keyword string, limit int) ([]int64, error) {
	if (text == "") == (keyword == "") {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "npm: a view search sets exactly one of text and keyword")
	}
	var out []int64
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		out, err = qs.NPMViewFind(ctx, q.NPMViewFindParams{Repos: repos, Text: text, Keyword: keyword, MaxRows: int32(min(max(limit, 0), math.MaxInt32))})
		return err
	})
	return out, err
}

// ViewDeprecated returns the ids of at most limit versions of the repositories that carry a deprecation message (R-API-085).
func (s *Store) ViewDeprecated(ctx context.Context, repos []string, limit int) ([]int64, error) {
	var out []int64
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		out, err = qs.NPMViewDeprecated(ctx, q.NPMViewDeprecatedParams{Repos: repos, MaxRows: int32(min(limit, 1<<20))}) //nolint:gosec // bounded above
		return err
	})
	return out, err
}

// ViewWithheld returns the stored versions of the repositories that the format does
// not serve (R-NPM-047).
func (s *Store) ViewWithheld(ctx context.Context, repos []string) ([]int64, error) {
	var out []int64
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		out, err = qs.NPMViewWithheld(ctx, repos)
		return err
	})
	return out, err
}

// ViewStored lists the versions a repository serves of a package and, for a hosted
// repository, its dist-tags when withTags is set (the dry run of the exposure rules,
// R-EP-016). A remote repository lists the versions of its cached document, an
// unlisted one too when the repository serves removed versions; the tags of that
// document are not stored in a column, so it reports none.
func (s *Store) ViewStored(ctx context.Context, repoID int64, name string, remote bool, limit int, withTags bool) ([]string, map[string]string, error) {
	maxRows := int32(min(max(limit, 0), math.MaxInt32))
	var versions []string
	var tags map[string]string
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if remote {
			versions, err = qs.NPMViewStoredRemote(ctx, q.NPMViewStoredRemoteParams{RepoID: repoID, Name: name, MaxRows: maxRows})
			return err
		}
		if versions, err = qs.NPMViewStoredLocal(ctx, q.NPMViewStoredLocalParams{RepoID: repoID, Name: name, MaxRows: maxRows}); err != nil {
			return err
		}
		if !withTags {
			return nil
		}
		rs, err := qs.NPMDistTags(ctx, q.NPMDistTagsParams{RepoID: repoID, Name: name})
		if err != nil || len(rs) == 0 {
			return err
		}
		tags, err = parseTags(rs[0].DistTags)
		return err
	})
	return versions, tags, err
}
