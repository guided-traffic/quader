package discover

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// The paging of a catalogue answer that a format holds whole (remote-curation.md R-CUR-007): a package document lists every version, an
// index every row, a stub every hit. A format pages such a list with [PageVersions] or [PageHits]; their cursor is an offset into the
// list, which a later call over the same list continues.

// offsetCursor is the cursor of a list the format holds whole.
type offsetCursor struct {
	Offset int `json:"offset"`
}

// Offset reads a cursor written by [OffsetCursor]; a nil or unreadable cursor is the first page.
func Offset(c Cursor) int {
	var oc offsetCursor
	if len(c) == 0 || json.Unmarshal(c, &oc) != nil || oc.Offset < 0 {
		return 0
	}
	return oc.Offset
}

// OffsetCursor is the cursor of the page that starts at offset.
func OffsetCursor(offset int) Cursor {
	b, _ := json.Marshal(offsetCursor{Offset: offset}) //nolint:errchkjson // a struct of an int always encodes
	return b
}

// PageVersions is one page of a package's versions, which the caller lists newest first by the format's order: the versions whose text
// holds rq.Filter (case-insensitive; "" for all), from the cursor's offset, at most rq.Limit (at most [MaxVersionsOnPage] when the limit
// is 0). total counts the versions the filter keeps.
func PageVersions(all []VersionFacts, rq VersionsRequest) (page []VersionFacts, total int, next Cursor, done bool) {
	return PageTaggedVersions(all, nil, rq)
}

// PageTaggedVersions is [PageVersions] for a package with tags (tag to version, npm's dist-tags): the filter also keeps a version that a
// tag whose name holds it points at, so typing a tag's name finds its version on any page (Q-CUR-182).
func PageTaggedVersions(all []VersionFacts, tags map[string]string, rq VersionsRequest) (page []VersionFacts, total int, next Cursor, done bool) {
	kept := all
	if f := strings.ToLower(strings.TrimSpace(rq.Filter)); f != "" {
		named := map[string]bool{}
		for tag, version := range tags {
			if strings.Contains(strings.ToLower(tag), f) {
				named[version] = true
			}
		}
		kept = make([]VersionFacts, 0, len(all))
		for _, v := range all {
			if strings.Contains(strings.ToLower(v.Version), f) || named[v.Version] {
				kept = append(kept, v)
			}
		}
	}
	limit := rq.Limit
	if limit <= 0 {
		limit = MaxVersionsOnPage
	}
	from := min(Offset(rq.Cursor), len(kept))
	to := min(from+limit, len(kept))
	page = append([]VersionFacts(nil), kept[from:to]...)
	if to < len(kept) {
		return page, len(kept), OffsetCursor(to), false
	}
	return page, len(kept), nil, true
}

// PageHits is one page of hits the caller holds whole, in their order: from the cursor's offset, at most limit (deflt when it is 0).
func PageHits(all []Hit, cursor Cursor, limit, deflt int) (page []Hit, next Cursor, done bool) {
	if limit <= 0 {
		limit = deflt
	}
	from := min(Offset(cursor), len(all))
	to := min(from+limit, len(all))
	page = append([]Hit(nil), all[from:to]...)
	if to < len(all) {
		return page, OffsetCursor(to), false
	}
	return page, nil, true
}

// CutText cuts an upstream's text to [MaxTextRunes] runes and drops what is not valid UTF-8 or a control character other than a line
// break or a tab: a description reaches a person as plain text (R-CUR-008).
func CutText(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == MaxTextRunes {
			break
		}
		switch {
		case r == utf8.RuneError:
			continue
		case r < 0x20 && r != '\n' && r != '\t':
			continue
		case r == 0x7f:
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// FormatLevel spells a level as the API and a bulk's subject spell it: its path joined by "/", each part URL-encoded (R-CUR-009).
func FormatLevel(l LevelRef) string {
	parts := make([]string, len(l.Path))
	for i, p := range l.Path {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ErrBadLevel is the error of a level that is not spelled as [FormatLevel] spells one.
var ErrBadLevel = errors.New("discover: the level is its path joined by /, each part URL-encoded and not empty")

// ParseLevel reads a level spelled by [FormatLevel]; "" is the root.
func ParseLevel(s string) (LevelRef, error) {
	if s == "" {
		return LevelRef{}, nil
	}
	var l LevelRef
	for _, part := range strings.Split(s, "/") {
		p, err := url.PathUnescape(part)
		if err != nil || strings.TrimSpace(p) == "" {
			return LevelRef{}, ErrBadLevel
		}
		l.Path = append(l.Path, p)
	}
	return l, nil
}
