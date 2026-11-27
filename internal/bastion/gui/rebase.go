package gui

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// rebased is the catalogue as this replica serves it (R-BAS-135, Q-BAS-524): a pair of deployments under one registration shares one
// catalogue, rendered on one of their base URLs, and each deployment names its own. Wherever the catalogue was rendered on another
// base URL than the replica's own server.base_url, every document, summary, version, description and documentation hit it hands out
// carries the replica's own in place of that one: whole origins only, so a host that merely begins with it is left. The catalogue's
// kept copy is not touched; its SHA-256 stays what the main installation wrote, and a rewritten answer carries the SHA-256 of what is
// served. The sitemap's lists (Packages, DocEntries) are passed as they are: they are read for routes, and the sitemap's addresses are
// on the replica's own base URL already.
type rebased struct {
	catalogue.Reader
	// own is the replica's server.base_url without a trailing slash.
	own string
}

// rebase wraps a catalogue for a replica whose base URL is own.
func rebase(r catalogue.Reader, own string) rebased {
	return rebased{Reader: r, own: strings.TrimSuffix(own, "/")}
}

// from is the base URL to replace, "" when there is none: the catalogue names none, or the replica's own.
func (r rebased) from() string {
	from := r.RenderedOn()
	if from == "" || from == r.own {
		return ""
	}
	return from
}

// PublicSet implements catalogue.Reader: the descriptions with the replica's own base URL.
func (r rebased) PublicSet() (iface.CataloguePublic, error) {
	pub, err := r.Reader.PublicSet()
	from := r.from()
	if err != nil || from == "" {
		return pub, err
	}
	for i := range pub.Projects {
		pub.Projects[i].Description = replaceOriginText(pub.Projects[i].Description, from, r.own)
	}
	for i := range pub.Repositories {
		pub.Repositories[i].Description = replaceOriginText(pub.Repositories[i].Description, from, r.own)
	}
	return pub, nil
}

// Search implements catalogue.Reader.
func (r rebased) Search(q catalogue.Query) (catalogue.Page, error) {
	p, err := r.Reader.Search(q)
	from := r.from()
	if err != nil || from == "" {
		return p, err
	}
	items := make([]catalogue.Entry, len(p.Items))
	for i, e := range p.Items {
		items[i] = rebaseEntry(e, from, r.own)
	}
	p.Items = items
	return p, nil
}

// Package implements catalogue.Reader.
func (r rebased) Package(id string) (catalogue.Entry, bool, error) {
	e, ok, err := r.Reader.Package(id)
	if from := r.from(); ok && err == nil && from != "" {
		e = rebaseEntry(e, from, r.own)
	}
	return e, ok, err
}

// Lookup implements catalogue.Reader.
func (r rebased) Lookup(format, name string) (catalogue.Entry, bool, error) {
	e, ok, err := r.Reader.Lookup(format, name)
	if from := r.from(); ok && err == nil && from != "" {
		e = rebaseEntry(e, from, r.own)
	}
	return e, ok, err
}

// Versions implements catalogue.Reader.
func (r rebased) Versions(ctx context.Context, id, after string, limit int) (catalogue.VersionPage, error) {
	p, err := r.Reader.Versions(ctx, id, after, limit)
	from := r.from()
	if err != nil || from == "" {
		return p, err
	}
	items := make([]json.RawMessage, len(p.Items))
	for i, v := range p.Items {
		items[i], _ = replaceOrigin(v, from, r.own)
	}
	p.Items = items
	return p, nil
}

// SearchDocs implements catalogue.Reader.
func (r rebased) SearchDocs(q catalogue.DocQuery) (catalogue.DocPage, error) {
	p, err := r.Reader.SearchDocs(q)
	from := r.from()
	if err != nil || from == "" {
		return p, err
	}
	items := make([]catalogue.DocHit, len(p.Items))
	for i, h := range p.Items {
		h.ShortDescription = replaceOriginText(h.ShortDescription, from, r.own)
		items[i] = h
	}
	p.Items = items
	return p, nil
}

// Document implements catalogue.Reader: a document that names the base URL it was rendered on is answered with the replica's own,
// with the SHA-256, the size and the compressed form of the bytes served.
func (r rebased) Document(ctx context.Context, path string) (catalogue.Document, bool, error) {
	d, ok, err := r.Reader.Document(ctx, path)
	from := r.from()
	if !ok || err != nil || from == "" {
		return d, ok, err
	}
	raw, changed := replaceOrigin(d.Raw, from, r.own)
	if !changed {
		return d, ok, nil
	}
	sum := sha256.Sum256(raw)
	out := catalogue.Document{Path: d.Path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw)), Raw: raw}
	if len(d.Gzip) > 0 {
		var b bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&b, gzip.BestSpeed)
		if _, err := zw.Write(raw); err == nil && zw.Close() == nil {
			out.Gzip = b.Bytes()
		}
	}
	return out, true, nil
}

// rebaseEntry is an entry of the index with the replica's own base URL in its description and its summary.
func rebaseEntry(e catalogue.Entry, from, to string) catalogue.Entry {
	e.Description = replaceOriginText(e.Description, from, to)
	e.Summary, _ = replaceOrigin(e.Summary, from, to)
	return e
}

func replaceOriginText(s, from, to string) string {
	if !strings.Contains(s, from) {
		return s
	}
	b, _ := replaceOrigin([]byte(s), from, to)
	return string(b)
}

// replaceOrigin replaces every whole occurrence of the base URL from in b with to, and reports whether it replaced one. An occurrence
// is whole when the byte after it cannot continue a host, a port or a path segment: the end, "/", a quote, a backslash of an escape in
// JSON, "?", "#", a space, a bracket. "https://pkgs.example.com" in "https://pkgs.example.com.evil.example" or
// "https://pkgs.example.com:8443" is another origin and is left. b is not changed; a result that replaced nothing is b itself.
func replaceOrigin(b []byte, from, to string) ([]byte, bool) {
	f := []byte(from)
	i := bytes.Index(b, f)
	if i < 0 {
		return b, false
	}
	var out []byte
	last, changed := 0, false
	for i >= 0 {
		end := i + len(f)
		if end == len(b) || !continuesAuthority(b[end]) {
			if out == nil {
				out = make([]byte, 0, len(b)+len(to))
			}
			out = append(append(out, b[last:i]...), to...)
			last, changed = end, true
		}
		next := bytes.Index(b[end:], f)
		if next < 0 {
			break
		}
		i = end + next
	}
	if !changed {
		return b, false
	}
	return append(out, b[last:]...), true
}

// continuesAuthority reports whether c may follow a base URL inside a longer host, port or path segment: a letter, a digit, or one of
// "-._~%:@".
func continuesAuthority(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("-._~%:@", c) >= 0
}
