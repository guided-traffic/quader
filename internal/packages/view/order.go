package view

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The order of the versions (R-API-029, Q-API-429, Q-API-431) has two parts. The versions the format ranks (a key of the class
// versionkey.Version) come in the order of their keys; the strings it does not rank (a key of the class NotVersion: an image tag such as
// latest, main or sha-5ccfd1a; npm, NuGet, Debian and Ansible store none) come after them, newest first by their time (a version by its
// oldest visible row: push or fetch time). Newest first, the ranked part comes first; oldest first, the unranked part. A cursor of the
// ranked part is the version, one of the unranked part its time and the version: the length tells the part.

// ranked reports whether the format ranks a version: its key has the class versionkey.Version.
func ranked(a Adapter, version string) bool {
	k := a.SortKey(version)
	return len(k) > 0 && k[0] == versionkey.Version
}

// orderKey is the cursor values of a version of the order of the versions: the version in the ranked part, its time and the version in
// the unranked part.
func orderKey(v VersionSummary) []string {
	if v.Unranked {
		return []string{v.PublishedAt.UTC().Format(time.RFC3339Nano), v.Version}
	}
	return []string{v.Version}
}

// walk reads the versions of a package in the order of the versions, a batch at a time, across both parts and every holder the
// repository filter admits. It is the one reader of that order: the plain list cuts its pages from it, the list by content groups them.
type walk struct {
	s        *Service
	sc       Scope
	a        Adapter
	desc     bool
	admitted []keysetQuery
	// only and hasOnly narrow the unranked part of several holders to the versions held by the filter's repositories.
	only    []int64
	hasOnly bool
	// unranked says that the walk is in the unranked part; started that it has read a batch of it.
	unranked bool
	// key is the key of the last ranked version read (exclusive), at and ver the time and the name of the last unranked one.
	key     []byte
	at, ver string
	// sorted is the unranked part of several holders, read whole and sorted once (H-1901); pos is the next of it.
	sorted []pageKey
	loaded bool
	pos    int
	done   bool
	// read counts the version rows the walk's statements returned: the scan budget of the list by content (Q-API-432).
	read int
}

// newWalk starts the walk after the cursor values (nil: at the start of the order).
func (s *Service) newWalk(sc Scope, a Adapter, hs []q.PackageViewHoldersRow, name string, withheld []int64, f VersionFilter, only []int64,
	desc bool, after []string,
) (*walk, error) {
	w := &walk{s: s, sc: sc, a: a, desc: desc, only: only, hasOnly: f.HasRepositories}
	for _, h := range hs {
		if !f.HasRepositories || slices.Contains(only, h.RepoID) {
			w.admitted = append(w.admitted, keysetQueryOf(sc, h, name, withheld, VersionOrder{Sort: "version", Desc: desc}))
		}
	}
	w.done = len(w.admitted) == 0
	switch len(after) {
	case 0:
		w.unranked = !desc
		w.key = keyStart(VersionOrder{Desc: desc})
		w.at, w.ver = startOf(VersionOrder{Desc: desc}), ""
	case 1:
		w.key = a.SortKey(after[0])
		if !ranked(a, after[0]) {
			return nil, errBadCursor
		}
		w.at, w.ver = startOf(VersionOrder{Desc: desc}), ""
	case 2:
		k, err := keyCursor(after)
		if err != nil {
			return nil, err
		}
		w.unranked, w.at, w.ver = true, stamp(&k.published), k.version
		w.key = keyStart(VersionOrder{Desc: desc})
		if desc {
			w.key = []byte{versionkey.Version} // the ranked part lies behind a cursor of the unranked part
		}
	default:
		return nil, errBadCursor
	}
	if len(hs) > 1 {
		w.pos = -1 // the unranked part of several holders is read whole when the walk reaches it
	}
	return w, nil
}

// next reads at most n versions after the walk's position, in the order, and moves the position past them; an empty answer is the end
// of the order. A version that several holders hold comes once.
func (w *walk) next(ctx context.Context, hs []q.PackageViewHoldersRow, n int) ([]pageKey, error) {
	for !w.done {
		var got []pageKey
		var more bool
		var err error
		if w.unranked {
			got, more, err = w.unrankedBatch(ctx, hs, n)
		} else {
			got, more, err = w.s.keyBatch(ctx, w.a, w.admitted, w.key, n)
			w.read += len(got)
		}
		if err != nil {
			return nil, err
		}
		if len(got) > 0 {
			last := got[len(got)-1]
			if w.unranked {
				w.at, w.ver = stamp(&last.published), last.version
			} else {
				w.key = last.key
			}
		}
		if !more || len(got) == 0 {
			w.advance()
		}
		if len(got) > 0 {
			return got, nil
		}
	}
	return nil, nil
}

// advance moves the walk to the part after the one it read to its end: newest first from the ranked to the unranked part, oldest first
// from the unranked to the ranked part, then to the end.
func (w *walk) advance() {
	switch {
	case w.desc && !w.unranked:
		w.unranked = true
	case !w.desc && w.unranked:
		w.unranked, w.key = false, []byte{versionkey.Version}
	default:
		w.done = true
	}
}

// unrankedBatch reads at most n versions of the unranked part after the walk's position: of one holder by a keyset statement on the time
// and the version, of several from the part read whole (H-1901). more says that versions may follow.
func (w *walk) unrankedBatch(ctx context.Context, hs []q.PackageViewHoldersRow, n int) ([]pageKey, bool, error) {
	if w.pos < 0 || w.loaded {
		if !w.loaded {
			if err := w.loadUnranked(ctx, hs); err != nil {
				return nil, false, err
			}
		}
		from := w.pos
		to := min(from+n, len(w.sorted))
		w.pos = to
		return w.sorted[from:to], to < len(w.sorted), nil
	}
	got, more, err := w.s.unrankedRows(ctx, w.a, w.admitted[0], w.at, w.ver, n)
	w.read += len(got)
	return got, more, err
}

// loadUnranked reads the unranked versions of every holder, keeps each by its oldest row and in the filter's repositories, sorts them and
// places the walk after its position.
func (w *walk) loadUnranked(ctx context.Context, hs []q.PackageViewHoldersRow) error {
	var rows []q.PackageViewUnrankedNamesRow
	k := w.admitted[0]
	err := w.s.read(ctx, func(qs *q.Queries) (err error) {
		rows, err = qs.PackageViewUnrankedNames(ctx, q.PackageViewUnrankedNamesParams{
			PackageIds: packageIDs(hs), Withheld: k.withheld, Whole: ids(w.sc.whole), Rows: keys(w.sc.rows),
		})
		return err
	})
	if err != nil {
		return err
	}
	w.read += len(rows)
	idx := map[string]int{}
	inOnly := map[string]bool{}
	var all []pageKey
	for _, r := range rows {
		if r.SortKey == nil && ranked(w.a, r.Version) {
			continue
		}
		i, ok := idx[r.Version]
		if !ok {
			i = len(all)
			idx[r.Version] = i
			all = append(all, pageKey{version: r.Version, published: r.CreatedAt.Time, unranked: true})
		}
		if r.CreatedAt.Time.Before(all[i].published) {
			all[i].published = r.CreatedAt.Time
		}
		if slices.Contains(w.only, r.RepoID) {
			inOnly[r.Version] = true
		}
	}
	all = slices.DeleteFunc(all, func(k pageKey) bool { return w.hasOnly && !inOnly[k.version] })
	slices.SortFunc(all, w.timeOrder)
	w.sorted, w.loaded = all, true
	pos := pageKey{version: w.ver}
	if t, err := time.Parse(time.RFC3339Nano, w.at); err == nil {
		pos.published = t
	}
	w.pos = len(all)
	for i, k := range all {
		if w.at == "infinity" || w.at == "-infinity" || w.timeOrder(k, pos) > 0 {
			w.pos = i
			break
		}
	}
	return nil
}

// timeOrder is the order of the unranked part: by time, then by the version's bytes, in the walk's direction.
func (w *walk) timeOrder(x, y pageKey) int {
	c := x.published.Compare(y.published)
	if c == 0 {
		c = strings.Compare(x.version, y.version)
	}
	if w.desc {
		return -c
	}
	return c
}

// unrankedRows reads at most n unranked versions of one holder after (at, cur), in the order of the query: the package's rows and, for a
// remote repository, its tags, merged. A row without a stored key comes with every batch (Q-API-430) and is kept when its format does not
// rank it and it lies after the position.
func (s *Service) unrankedRows(ctx context.Context, a Adapter, k keysetQuery, at, cur string, n int) ([]pageKey, bool, error) {
	var out []pageKey
	keyed := 0
	pos := pageKey{version: cur}
	if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
		pos.published = t
	}
	after := func(x pageKey) bool {
		if at == "infinity" || at == "-infinity" {
			return true
		}
		c := x.published.Compare(pos.published)
		if c == 0 {
			c = strings.Compare(x.version, pos.version)
		}
		return (k.desc && c < 0) || (!k.desc && c > 0)
	}
	add := func(version string, created time.Time, key []byte) {
		p := pageKey{version: version, published: created, unranked: true}
		if key != nil {
			keyed++
			out = append(out, p)
			return
		}
		if !ranked(a, version) && after(p) {
			out = append(out, p)
		}
	}
	err := s.read(ctx, func(qs *q.Queries) error {
		out, keyed = out[:0], 0
		if k.desc {
			rs, err := qs.PackageViewVersionUnrankedDesc(ctx, q.PackageViewVersionUnrankedDescParams{
				PackageID: k.holder.PackageID, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
			})
			for _, r := range rs {
				add(r.Version, r.CreatedAt.Time, r.SortKey)
			}
			if err != nil || !k.remote {
				return err
			}
			ts, err := qs.PackageViewTagUnrankedDesc(ctx, q.PackageViewTagUnrankedDescParams{
				RepoID: k.holder.RepoID, Name: k.name, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
			})
			for _, r := range ts {
				add(r.Version, r.CreatedAt.Time, r.SortKey)
			}
			return err
		}
		rs, err := qs.PackageViewVersionUnrankedAsc(ctx, q.PackageViewVersionUnrankedAscParams{
			PackageID: k.holder.PackageID, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
		})
		for _, r := range rs {
			add(r.Version, r.CreatedAt.Time, r.SortKey)
		}
		if err != nil || !k.remote {
			return err
		}
		ts, err := qs.PackageViewTagUnrankedAsc(ctx, q.PackageViewTagUnrankedAscParams{
			RepoID: k.holder.RepoID, Name: k.name, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
		})
		for _, r := range ts {
			add(r.Version, r.CreatedAt.Time, r.SortKey)
		}
		return err
	})
	if err != nil {
		return nil, false, err
	}
	slices.SortFunc(out, func(x, y pageKey) int {
		c := x.published.Compare(y.published)
		if c == 0 {
			c = strings.Compare(x.version, y.version)
		}
		if k.desc {
			return -c
		}
		return c
	})
	more := keyed >= n || len(out) > n
	if len(out) > n {
		out = out[:n]
	}
	return out, more, nil
}

// orderPage reads a page in the order of the versions: limit+1 versions that pass the pre-release filter, from the walk's batches of at
// least keysetBatch versions. found is false when the package has no visible version at all.
func (s *Service) orderPage(ctx context.Context, sc Scope, a Adapter, hs []q.PackageViewHoldersRow, name string, withheld []int64, f VersionFilter,
	only []int64, o VersionOrder, after []string, limit int,
) ([]pageKey, bool, error) {
	w, err := s.newWalk(sc, a, hs, name, withheld, f, only, o.Desc, after)
	if err != nil {
		return nil, true, err
	}
	batch := max(limit+1, keysetBatch)
	var page []pageKey
	for len(page) < limit+1 {
		got, err := w.next(ctx, hs, batch)
		if err != nil {
			return nil, true, err
		}
		if len(got) == 0 {
			break
		}
		for _, k := range got {
			if f.Prerelease == nil || *f.Prerelease == a.Prerelease(k.version) {
				page = append(page, k)
			}
		}
	}
	if len(page) > limit+1 {
		page = page[:limit+1]
	}
	if len(page) > 0 || after != nil || (f.Prerelease == nil && !f.HasRepositories) {
		return page, len(page) > 0 || after != nil, nil
	}
	// An empty first page under a filter: the package is found when it has a version at all.
	any, err := s.anyVersion(ctx, sc, a, hs, name, withheld)
	return page, any, err
}

// anyVersion reports whether the package has a visible version in any holder, filters aside.
func (s *Service) anyVersion(ctx context.Context, sc Scope, a Adapter, hs []q.PackageViewHoldersRow, name string, withheld []int64) (bool, error) {
	w, err := s.newWalk(sc, a, hs, name, withheld, VersionFilter{}, nil, true, nil)
	if err != nil {
		return false, err
	}
	got, err := w.next(ctx, hs, 1)
	return len(got) > 0, err
}

// keyAfter reports whether key lies after at in the direction.
func keyAfter(key, at []byte, desc bool) bool {
	c := bytes.Compare(key, at)
	return (desc && c < 0) || (!desc && c > 0)
}
