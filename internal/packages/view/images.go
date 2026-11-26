package view

import (
	"bytes"
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The list by content (R-API-029, Q-API-432, ADR-280): for a format whose version is fetched as its document (OCI: an image's tags), one
// entry per content (an image's manifest digest) instead of one per version, led by the content's first version in the order of the
// versions, with its other versions as aliases. It walks the order of the versions as the plain list does (walk), a batch at a time; after
// each batch it asks of every holder which of the batch's (version, content) pairs a visible version earlier in the order names already
// (PackageViewImageSeen), and the others lead an image. The walk reads at most imageBudget version rows a request: a page may hold fewer
// entries than its limit, or none, with a cursor that names the walk's position (R-API-010); that bounds the end of a list whose versions
// lead no image (H-1900).

// imageBudget is the scan budget of one request of the list by content: the version rows its walk reads (Q-API-432).
const imageBudget = 1024

// maxAliases bounds the other versions an entry names; AliasesMore says that it has more.
const maxAliases = 100

// imageCandidate is a (version, content) pair of a batch of the walk.
type imageCandidate struct {
	key    pageKey
	digest []byte
}

// Images returns a page of the list by content: at most limit entries, more only to keep a version's entries together, and the cursor
// values of the next page, nil at the end; found is false when the scope holds no version of the package. A format whose version is no
// document lists each version as its own entry, the plain list in the order of the versions.
func (s *Service) Images(ctx context.Context, sc Scope, format, name string, f VersionFilter, after []string, limit int,
) ([]VersionSummary, []string, bool, error) {
	a, ok := s.adapters.For(format)
	if !ok {
		return nil, nil, false, nil
	}
	o := VersionOrder{Sort: "version", Desc: true}
	if a.Fetch() != FetchDocument {
		page, found, err := s.Versions(ctx, sc, format, name, f, o, after, limit)
		if err != nil || !found {
			return nil, nil, found, err
		}
		var next []string
		if len(page) > limit {
			page = page[:limit]
			next = o.VersionKey(page[len(page)-1])
		}
		for i := range page {
			page[i].Aliases = []string{}
		}
		return page, next, true, nil
	}
	withheld, err := s.withheld(ctx, sc, []string{format}, sc.Repos())
	if err != nil {
		return nil, nil, false, err
	}
	hs, err := s.holdersOf(ctx, sc, format, name)
	if err != nil || len(hs) == 0 {
		return nil, nil, false, err
	}
	var only []int64
	if f.HasRepositories {
		only = sc.topo.Expand(sc.topo.IDs(f.Repositories))
	}
	w, err := s.newWalk(sc, a, hs, name, withheld, f, only, true, after)
	if err != nil {
		return nil, nil, true, err
	}
	entries, next, err := s.imageWalk(ctx, w, hs, limit)
	if err != nil {
		return nil, nil, true, err
	}
	if len(entries) == 0 && after == nil && next == nil {
		any, err := s.anyVersion(ctx, sc, a, hs, name, withheld)
		return []VersionSummary{}, nil, any, err
	}
	out, err := s.imageSummaries(ctx, sc, a, format, hs, w, entries, f)
	return out, next, true, err
}

// imageWalk walks the order of the versions until the page holds limit entries and one more, the walk ends or it read the budget, and
// cuts the page at a version's last entry.
func (s *Service) imageWalk(ctx context.Context, w *walk, hs []q.PackageViewHoldersRow, limit int) ([]imageCandidate, []string, error) {
	var entries []imageCandidate
	var last pageKey
	ended := false
	for len(entries) < limit+1 {
		got, err := w.next(ctx, hs, keysetBatch)
		if err != nil {
			return nil, nil, err
		}
		if len(got) == 0 {
			ended = true
			break
		}
		cands := candidatesOf(got)
		seen, err := s.imagesSeen(ctx, w, cands)
		if err != nil {
			return nil, nil, err
		}
		for i, c := range cands {
			if !seen[i] {
				entries = append(entries, c)
			}
		}
		last = got[len(got)-1]
		if w.read >= imageBudget && len(entries) < limit+1 {
			return entries, positionOf(last), nil
		}
	}
	if len(entries) <= limit {
		if ended {
			return entries, nil, nil
		}
		return entries, positionOf(last), nil
	}
	page := cutAtVersion(entries, limit)
	return page, positionOf(page[len(page)-1].key), nil
}

// cutAtVersion is the page of more than limit entries: the whole versions that fit in limit entries, at least the first version, so a
// version's entries (a tag that names two contents in two holders) are never parted by a cursor.
func cutAtVersion(entries []imageCandidate, limit int) []imageCandidate {
	cut := 0
	for i := 1; i <= len(entries); i++ {
		if i == len(entries) || entries[i].key.version != entries[i-1].key.version {
			if i > limit && cut > 0 {
				break
			}
			cut = i
		}
	}
	return entries[:cut]
}

// positionOf is the cursor values of a version of the walk: the version in the ranked part, its time and the version in the other.
func positionOf(k pageKey) []string {
	if k.unranked {
		return []string{k.published.UTC().Format(time.RFC3339Nano), k.version}
	}
	return []string{k.version}
}

// candidatesOf is the (version, content) pairs of a batch, in its order: a version that two holders hold with two contents is two pairs, a
// version without a stored content one pair of its own.
func candidatesOf(got []pageKey) []imageCandidate {
	var out []imageCandidate
	for _, k := range got {
		if len(k.digests) == 0 {
			out = append(out, imageCandidate{key: k})
			continue
		}
		for _, d := range k.digests {
			out = append(out, imageCandidate{key: k, digest: d})
		}
	}
	return out
}

// imagesSeen reports, per imageCandidate of a batch, whether a visible version earlier in the order names its content in one of the walk's
// holders: one PackageViewImageSeen and one PackageViewImageUnkeyed per holder, none per imageCandidate. A imageCandidate without a content is never
// seen. The unranked part of several holders is read whole (H-1901), so its candidates are ordered among themselves here, by their place in
// it, and asked only for a ranked sibling.
func (s *Service) imagesSeen(ctx context.Context, w *walk, cands []imageCandidate) ([]bool, error) {
	seen := make([]bool, len(cands))
	var versions, ats []string
	var digests, keys [][]byte
	var at []int
	for i, c := range cands {
		if c.digest == nil {
			continue
		}
		if c.key.unranked && w.loaded {
			if first, ok := w.firstOf[string(c.digest)]; ok && first < c.key.pos {
				seen[i] = true
				continue
			}
		}
		key := c.key.key
		if c.key.unranked {
			key = []byte{versionkey.NotVersion}
		}
		versions, digests, keys = append(versions, c.key.version), append(digests, c.digest), append(keys, key)
		ats, at = append(ats, c.key.published.UTC().Format(time.RFC3339Nano)), append(at, i)
	}
	if len(at) == 0 {
		return seen, nil
	}
	err := s.read(ctx, func(qs *q.Queries) error {
		for _, k := range w.admitted {
			idx, err := qs.PackageViewImageSeen(ctx, q.PackageViewImageSeenParams{
				Versions: versions, Digests: digests, Keys: keys, Ats: ats, PackageID: k.holder.PackageID, RepoID: k.holder.RepoID, Name: k.name,
				Remote: k.remote, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, ByTime: !w.loaded,
			})
			if err != nil {
				return err
			}
			for _, j := range idx {
				seen[at[j-1]] = true
			}
			unkeyed, err := qs.PackageViewImageUnkeyed(ctx, q.PackageViewImageUnkeyedParams{
				PackageID: k.holder.PackageID, Digests: digests, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed,
			})
			if err != nil {
				return err
			}
			for _, u := range unkeyed {
				sib := pageKey{version: u.Version, published: u.CreatedAt.Time, key: w.a.SortKey(u.Version)}
				sib.unranked = sib.key[0] != versionkey.Version
				for _, j := range at {
					if c := cands[j]; bytes.Equal(c.digest, u.Digest) && c.key.version != u.Version && earlier(sib, c.key) {
						seen[j] = true
					}
				}
			}
		}
		return nil
	})
	return seen, err
}

// earlier reports whether x comes before y in the order of the versions, newest first: a ranked version before an unranked one, the
// ranked by key, the unranked by time and then by the version's bytes.
func earlier(x, y pageKey) bool {
	switch {
	case !x.unranked && !y.unranked:
		return bytes.Compare(x.key, y.key) > 0
	case !x.unranked:
		return true
	case !y.unranked:
		return false
	}
	if c := x.published.Compare(y.published); c != 0 {
		return c > 0
	}
	return x.version > y.version
}

// imageSummaries builds the entries of a page: the leading version's summary (its rows, its size), the content's digest, its other
// versions in the order of the versions, the repositories that hold any of them and the downloads of the content, and with
// VersionFilter.WithKind the kind of the leading version.
func (s *Service) imageSummaries(ctx context.Context, sc Scope, a Adapter, format string, hs []q.PackageViewHoldersRow, w *walk,
	entries []imageCandidate, f VersionFilter,
) ([]VersionSummary, error) {
	if len(entries) == 0 {
		// A page the budget ended before any image led: nothing to read (PackageViewVersions reads every version for no version).
		return []VersionSummary{}, nil
	}
	versions := make([]string, 0, len(entries))
	var digests [][]byte
	for _, e := range entries {
		if !slices.Contains(versions, e.key.version) {
			versions = append(versions, e.key.version)
		}
		if e.digest != nil {
			digests = append(digests, e.digest)
		}
	}
	rows, err := s.versionRows(ctx, sc, packageIDs(hs), versions, true, w.admitted[0].withheld)
	if err != nil {
		return nil, err
	}
	groupOf := map[string]versionGroup{}
	for _, g := range groupRows(rows) {
		groupOf[g.version] = g
	}
	tags, holders, more, dl, err := s.imageFacts(ctx, w, digests)
	if err != nil {
		return nil, err
	}
	out := make([]VersionSummary, 0, len(entries))
	for _, e := range entries {
		g, ok := groupOf[e.key.version]
		if !ok {
			continue // removed between the reads; the next request is right
		}
		files := g.primary().SizeBytes
		v := VersionSummary{
			Version: e.key.version, Prerelease: a.Prerelease(e.key.version), PublishedAt: g.published, SizeBytes: &files,
			Repositories: sc.topo.Names(sc.holders(g.repoIDs())), DownloadCount: g.downloads(), LastDownloadedAt: g.lastDownload(),
			Unranked: e.key.unranked, Aliases: []string{}, VersionID: g.primary().VersionID, Repo: sc.topo.Name(g.primary().RepoID),
		}
		if e.digest != nil {
			d := string(e.digest)
			v.Digest = "sha256:" + hex.EncodeToString(e.digest)
			for _, t := range tags[d] {
				if t != e.key.version {
					v.Aliases = append(v.Aliases, t)
				}
			}
			if len(v.Aliases) > maxAliases {
				v.Aliases, more[d] = v.Aliases[:maxAliases], true
			}
			v.AliasesMore = more[d]
			v.Repositories = sc.topo.Names(sc.holders(holders[d]))
			v.DownloadCount, v.LastDownloadedAt = dl[d].n, dl[d].last
		}
		out = append(out, v)
	}
	if err := s.adapterSizes(ctx, sc, a, format, groupOf, out); err != nil {
		return nil, err
	}
	if f.WithKind {
		// The kind of each entry's leading version (R-OCI-041), read as the list by version reads it; the digest is the entry's own.
		f.WithDigest, f.WithState = false, false
		return out, s.fillFromMetadata(ctx, sc, a, format, groupOf, out, f)
	}
	return out, nil
}

type imageDownloads struct {
	n    int64
	last *time.Time
}

// imageFacts reads, for the page's contents, their visible versions in the order of the versions (PackageViewImageTags, at most 101 of
// each kind per holder), the holders that hold any of them, whether a holder had more, and their downloads (PackageViewImageDownloads):
// two statements per holder for the page.
func (s *Service) imageFacts(ctx context.Context, w *walk, digests [][]byte) (map[string][]string, map[string][]int64, map[string]bool,
	map[string]imageDownloads, error,
) {
	byDigest := map[string][]pageKey{}
	holders := map[string][]int64{}
	more := map[string]bool{}
	dl := map[string]imageDownloads{}
	if len(digests) == 0 {
		return map[string][]string{}, holders, more, dl, nil
	}
	err := s.read(ctx, func(qs *q.Queries) error {
		for _, k := range w.admitted {
			rs, err := qs.PackageViewImageTags(ctx, q.PackageViewImageTagsParams{
				Digests: digests, PackageID: k.holder.PackageID, RepoID: k.holder.RepoID, Name: k.name, Remote: k.remote,
				Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed,
			})
			if err != nil {
				return err
			}
			count := map[string][2]int{}
			for _, r := range rs {
				d := string(r.Digest)
				p := pageKey{version: r.Version, published: r.CreatedAt.Time, key: r.SortKey}
				if p.key == nil {
					p.key = w.a.SortKey(r.Version)
				}
				p.unranked = p.key[0] != versionkey.Version
				c := count[d]
				if p.unranked {
					c[1]++
				} else {
					c[0]++
				}
				count[d] = c
				if c[0] > maxAliases || c[1] > maxAliases {
					more[d] = true
				}
				byDigest[d] = append(byDigest[d], p)
				if !slices.Contains(holders[d], k.holder.RepoID) {
					holders[d] = append(holders[d], k.holder.RepoID)
				}
			}
			ds, err := qs.PackageViewImageDownloads(ctx, q.PackageViewImageDownloadsParams{
				RepoID: k.holder.RepoID, Name: k.name, Digests: digests, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed,
			})
			if err != nil {
				return err
			}
			for _, r := range ds {
				d := string(r.Digest)
				x := dl[d]
				x.n += r.Downloads
				if r.LastDownloadAt.Valid && (x.last == nil || r.LastDownloadAt.Time.After(*x.last)) {
					at := r.LastDownloadAt.Time
					x.last = &at
				}
				dl[d] = x
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tags := map[string][]string{}
	for d, ps := range byDigest {
		slices.SortStableFunc(ps, func(x, y pageKey) int {
			switch {
			case earlier(x, y):
				return -1
			case earlier(y, x):
				return 1
			}
			return strings.Compare(x.version, y.version)
		})
		listed := map[string]bool{}
		for _, p := range ps {
			if !listed[p.version] {
				listed[p.version] = true
				tags[d] = append(tags[d], p.version)
			}
		}
	}
	return tags, holders, more, dl, nil
}
