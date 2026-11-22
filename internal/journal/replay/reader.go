// Package replay reads the revocation journal and applies the handlers the domains registered (R-HA-054 "Replay run",
// R-BAK-024).
package replay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/journal"
)

// Source is the journal on one connection: the connection's id, its store and the prefix below which the journal lies.
type Source struct {
	Connection string
	Store      journal.Store
	Prefix     string
}

// Reader is the [journal.Reader] over the journal of one or more connections (storage.md R-STOR-059).
//
// Whoever holds the runtime credential of a connection can write objects below its journal prefix, so every object is
// untrusted: one that does not decode, whose ID differs from its key, or whose key names another day than its time is reported
// unreadable and never replayed. An ID that appears under a second key, and a key whose contents diverge (between versions or
// between connections), are not dropped either: every content is replayed and flagged, because a handler only removes or narrows
// and checks the entry against the database, whereas dropping a copy would let the writer of the planted one hide the genuine one
// (audit finding H2). The same bytes under one key on two connections are one copy.
type Reader struct {
	sources []Source
	now     func() time.Time
}

var _ journal.Reader = (*Reader)(nil)

// NewReader builds a reader of the journal below blobPrefix of one store.
func NewReader(store journal.Store, blobPrefix string) *Reader {
	return ReaderOf(Source{Connection: "default", Store: store, Prefix: blobPrefix})
}

// ReaderOf builds a reader of the journal of every source, merged by key (storage.md R-STOR-059).
func ReaderOf(sources ...Source) *Reader {
	return &Reader{sources: sources, now: time.Now}
}

// content is one distinct content found under a key, with the connections that hold it.
type content struct {
	body  []byte
	obj   journal.Object
	conns []string
}

// keyed is what the sources hold under one journal key relative to their prefixes.
type keyed struct {
	contents []*content
}

// Entries lists the days from since to now on every source that can be listed, reads every object of them and returns the
// readable contents with at > since, sorted by at and id. An object that cannot be read or decoded, and a source that cannot be
// listed, are returned in the second result and stop nothing as long as one source was listed. A journal of which no source can
// be listed is a journal that is not known at all, and the caller refuses the restore (R-BAK-020 check 8) or reports the failure.
func (r *Reader) Entries(ctx context.Context, since time.Time) ([]journal.Object, []journal.Unreadable, error) {
	var (
		unreadable []journal.Unreadable
		byKey      = map[string]*keyed{}
		listed     int
		lastErr    error
	)
	for _, src := range r.sources {
		keys, err := r.list(ctx, src, since)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			lastErr = err
			unreadable = append(unreadable, journal.Unreadable{Key: src.Connection + ": " + journal.ListPrefix(src.Prefix), Reason: "the connection cannot be listed: " + err.Error()})
			continue
		}
		listed++
		for _, key := range keys {
			rel, objs, bad, err := r.read(ctx, src, key)
			if err != nil {
				return nil, nil, err
			}
			unreadable = append(unreadable, bad...)
			if rel == "" {
				continue
			}
			k := byKey[rel]
			if k == nil {
				k = &keyed{}
				byKey[rel] = k
			}
			k.add(src.Connection, objs)
		}
	}
	if listed == 0 && len(r.sources) > 0 {
		return nil, nil, lastErr
	}
	return merge(byKey, since), unreadable, nil
}

// list returns the keys of the days from since to a day after now on one source.
func (r *Reader) list(ctx context.Context, src Source, since time.Time) ([]string, error) {
	var keys []string
	// A day further than now is listed too: a key cannot name the future, but the clock of the writer may run ahead of this
	// one by a day boundary.
	for _, p := range journal.DayPrefixes(src.Prefix, since, r.now().Add(24*time.Hour)) {
		if err := src.Store.List(ctx, p, func(key string) error { keys = append(keys, key); return nil }); err != nil {
			return nil, fmt.Errorf("journal: listing %s on connection %s: %w", p, src.Connection, err)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// read reads one key of a source: its key relative to the source's prefix (empty when nothing of it is readable), the readable
// contents, and what is not readable. Only a cancelled context is an error.
func (r *Reader) read(ctx context.Context, src Source, key string) (string, []content, []journal.Unreadable, error) {
	where := key
	if len(r.sources) > 1 {
		where = src.Connection + ": " + key
	}
	id, ok := journal.ParseKey(src.Prefix, key)
	if !ok {
		return "", nil, []journal.Unreadable{{Key: where, Reason: "the key is not a journal key"}}, nil
	}
	bodies, err := bodiesOf(ctx, src.Store, key)
	if err != nil {
		if ctx.Err() != nil {
			return "", nil, nil, ctx.Err()
		}
		return "", nil, []journal.Unreadable{{Key: where, Reason: "read: " + err.Error()}}, nil
	}
	var (
		out []content
		bad []journal.Unreadable
		rel string
	)
	for _, body := range bodies {
		o, err := journal.Decode(body)
		if err != nil {
			reason := err.Error()
			if errors.Is(err, journal.ErrUnknownVersion) {
				reason = "unknown version: " + reason
			}
			bad = append(bad, journal.Unreadable{Key: where, Reason: reason})
			continue
		}
		if o.ID != id || journal.Key(src.Prefix, o) != key {
			bad = append(bad, journal.Unreadable{Key: where, Reason: "the object does not belong under this key"})
			continue
		}
		rel = journal.Key("", o)
		out = append(out, content{body: body, obj: o})
	}
	return rel, out, bad, nil
}

// add records the contents one connection holds under the key; the same bytes on another connection are the same copy.
func (k *keyed) add(conn string, objs []content) {
	for _, o := range objs {
		i := slices.IndexFunc(k.contents, func(c *content) bool { return bytes.Equal(c.body, o.body) })
		if i < 0 {
			c := o
			c.conns = []string{conn}
			k.contents = append(k.contents, &c)
			continue
		}
		if !slices.Contains(k.contents[i].conns, conn) {
			k.contents[i].conns = append(k.contents[i].conns, conn)
		}
	}
}

// merge flattens the contents of every key, flags a key whose contents diverge and an ID under more than one key, and keeps the
// objects newer than since, sorted by at and id.
func merge(byKey map[string]*keyed, since time.Time) []journal.Object {
	rels := make([]string, 0, len(byKey))
	for rel := range byKey {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	var out []journal.Object
	keysOf := map[string][]string{} // entry ID -> the keys it was read under
	for _, rel := range rels {
		k := byKey[rel]
		// quader writes a key only with the same bytes (R-HA-054), so a second content is somebody else's write.
		for _, c := range k.contents {
			o := c.obj
			if len(k.contents) > 1 {
				o.Warning = "the contents of " + rel + " diverge (" + strings.Join(connsOf(k.contents), "; ") + "); every content is replayed"
			}
			out = append(out, o)
		}
		if len(k.contents) > 0 {
			id := k.contents[0].obj.ID
			keysOf[id] = append(keysOf[id], rel)
		}
	}
	kept := out[:0]
	for _, o := range out {
		if ks := keysOf[o.ID]; len(ks) > 1 {
			o.Warning = strings.TrimSpace(o.Warning + " the entry " + o.ID + " is also under " +
				strings.Join(without(ks, journal.Key("", o)), ", ") + "; every copy is replayed")
		}
		if o.At.After(since) {
			kept = append(kept, o)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Less(kept[j]) })
	return kept
}

// connsOf names, for each content, the connections that hold it.
func connsOf(contents []*content) []string {
	out := make([]string, 0, len(contents))
	for _, c := range contents {
		out = append(out, strings.Join(c.conns, ", "))
	}
	return out
}

// bodiesOf returns the contents to read for a key: every version when the store was listed with versions, else the one.
func bodiesOf(ctx context.Context, store journal.Store, key string) ([][]byte, error) {
	if av, ok := store.(journal.AllVersions); ok {
		return av.GetAll(ctx, key)
	}
	b, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return [][]byte{b}, nil
}

func without(keys []string, drop string) []string {
	var out []string
	for _, k := range keys {
		if k != drop {
			out = append(out, k)
		}
	}
	return out
}
