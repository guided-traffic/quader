package cache

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// The top-level names of the layout (R-BAS-030). Artifacts are below sha256/ as the spec has them; the bytes of
// shared documents are below documents/ in the same form, so that the two areas of R-BAS-031 are two listings
// and a document never evicts an artifact (Q-BAS-87).
const (
	dirArtifacts = "sha256"
	dirDocuments = "documents"
	dirEntries   = "entries"
	dirAccess    = "access"
	dirSpool     = "audit-spool"
	dirProbe     = "probe"
	// fileConfiguration is the configuration a replica applied, kept for a restart without the main installation (R-BAS-093,
	// Q-BAS-327).
	fileConfiguration = "configuration/current.json"
)

type areaKey struct{}

// WithArea says which area the bytes of the call belong to: [EntryDocument] for the bytes of a shared document,
// anything else (the default) for an artifact. PutObject, GetObject, HeadObject, DeleteObject and Touch find the
// bytes below sha256/ or documents/ by it, so a caller that handles a document carries the area of its entry's kind
// on the context. The wrong area is a miss, never a wrong answer (Q-BAS-87).
func WithArea(ctx context.Context, kind EntryKind) context.Context {
	return context.WithValue(ctx, areaKey{}, kind)
}

func areaOf(ctx context.Context) EntryKind {
	if k, ok := ctx.Value(areaKey{}).(EntryKind); ok && k == EntryDocument {
		return EntryDocument
	}
	return EntryArtifact
}

var (
	hex64       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	replica     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	spoolFn     = regexp.MustCompile(`^[0-9]{1,19}\.jsonl$`)
	spoolSealed = regexp.MustCompile(`^[0-9]{1,19}$`)
)

func validHash(s string) bool { return hex64.MatchString(s) }

// layout names every object of the cache below the backend's root. plainLayout is R-BAS-030's, which a cache without
// a key keeps; sealedLayout is R-BAS-061's below the tree of the cache key. Every name is built from validated parts.
type layout interface {
	// object is the name of the bytes of a content address in an area, marker the name of its access marker.
	object(area EntryKind, sha string) (string, error)
	marker(area EntryKind, sha string) (string, error)
	// areaDir and markerDir are the prefixes the sweep lists; objectOf and markerOf name the object and the marker of a
	// listed base name (the last element of an object's name, 64 hex), so that the sweep deletes without the digest.
	areaDir(area EntryKind) string
	markerDir() string
	objectOf(area EntryKind, base string) string
	markerOf(base string) string
	// entry is the name of the entry of a key hash (the SHA-256 of a cache key, hex).
	entry(keyHash string) (string, error)
	// spool is the name of a file of the audit spool, spoolDir its prefix, spoolName the file name of a time in ms.
	spool(replica, name string) (string, error)
	spoolDir() string
	spoolName(ms int64) string
	// probe is the name of the probe of the first readiness check.
	probe(id string) string
	// configuration is the name of the stored configuration (R-BAS-093).
	configuration() string
}

// plainLayout is the layout of R-BAS-030:
//
//	sha256/<h0h1>/<h2h3>/<64 hex>    documents/<h0h1>/<h2h3>/<64 hex>    entries/<k0k1>/<64 hex>.json
//	access/<64 hex>                  audit-spool/<replica>/<unix ms>.jsonl    probe/<32 hex>
//	configuration/current.json
//
// An access marker is per content address, whatever the area.
type plainLayout struct{}

var _ layout = plainLayout{}

func (plainLayout) object(area EntryKind, sha string) (string, error) { return objectKey(area, sha) }

func (plainLayout) marker(_ EntryKind, sha string) (string, error) { return accessKey(sha) }

func (plainLayout) areaDir(area EntryKind) string {
	if area == EntryDocument {
		return dirDocuments + "/"
	}
	return dirArtifacts + "/"
}

func (plainLayout) markerDir() string { return dirAccess + "/" }

func (l plainLayout) objectOf(area EntryKind, base string) string {
	return l.areaDir(area) + base[0:2] + "/" + base[2:4] + "/" + base
}

func (plainLayout) markerOf(base string) string { return dirAccess + "/" + base }

func (plainLayout) entry(keyHash string) (string, error) { return entryKey(keyHash) }

func (plainLayout) spool(replicaName, name string) (string, error) {
	return spoolKey(replicaName, name)
}

func (plainLayout) spoolDir() string { return dirSpool + "/" }

func (plainLayout) spoolName(ms int64) string { return strconv.FormatInt(ms, 10) + ".jsonl" }

func (plainLayout) probe(id string) string { return dirProbe + "/" + id }

func (plainLayout) configuration() string { return fileConfiguration }

// sealedLayout is the layout of R-BAS-061 below the tree of the cache key's identifier, every name keyed (m is a keyed
// name of [sealer.name], 64 hex):
//
//	bc-<id>/a/<m0m1>/<m2m3>/<m>    bc-<id>/d/<m0m1>/<m2m3>/<m>    bc-<id>/e/<m0m1>/<m>    bc-<id>/x/<m>
//	bc-<id>/s/<replica>/<unix ms>  bc-<id>/p/<32 hex>  bc-<id>/c/current
//
// The marker of an object is named by the object's m, so the area is part of it.
type sealedLayout struct {
	s    *sealer
	root string
}

var _ layout = sealedLayout{}

func newSealedLayout(s *sealer) sealedLayout { return sealedLayout{s: s, root: s.id + "/"} }

// rawHash decodes a validated 64-hex digest.
func rawHash(what, s string) ([]byte, error) {
	if !validHash(s) {
		return nil, fmt.Errorf("%w: %s %q", ErrInvalid, what, shorten(s))
	}
	return hex.DecodeString(s)
}

func nameArea(area EntryKind) byte {
	if area == EntryDocument {
		return nameDocument
	}
	return nameArtifact
}

func (l sealedLayout) keyed(area EntryKind, sha string) (string, error) {
	d, err := rawHash("content address", sha)
	if err != nil {
		return "", err
	}
	return l.s.name(nameArea(area), d), nil
}

func (l sealedLayout) object(area EntryKind, sha string) (string, error) {
	m, err := l.keyed(area, sha)
	if err != nil {
		return "", err
	}
	return l.objectOf(area, m), nil
}

func (l sealedLayout) marker(area EntryKind, sha string) (string, error) {
	m, err := l.keyed(area, sha)
	if err != nil {
		return "", err
	}
	return l.markerOf(m), nil
}

func (l sealedLayout) areaDir(area EntryKind) string {
	if area == EntryDocument {
		return l.root + "d/"
	}
	return l.root + "a/"
}

func (l sealedLayout) markerDir() string { return l.root + "x/" }

func (l sealedLayout) objectOf(area EntryKind, base string) string {
	return l.areaDir(area) + base[0:2] + "/" + base[2:4] + "/" + base
}

func (l sealedLayout) markerOf(base string) string { return l.root + "x/" + base }

func (l sealedLayout) entry(keyHash string) (string, error) {
	d, err := rawHash("key hash", keyHash)
	if err != nil {
		return "", err
	}
	m := l.s.name(nameEntry, d)
	return l.root + "e/" + m[0:2] + "/" + m, nil
}

func (l sealedLayout) spool(replicaName, name string) (string, error) {
	if !replica.MatchString(replicaName) || !spoolSealed.MatchString(name) {
		return "", fmt.Errorf("%w: spool file %q/%q", ErrInvalid, shorten(replicaName), shorten(name))
	}
	return l.root + "s/" + replicaName + "/" + name, nil
}

func (l sealedLayout) spoolDir() string { return l.root + "s/" }

func (sealedLayout) spoolName(ms int64) string { return strconv.FormatInt(ms, 10) }

func (l sealedLayout) probe(id string) string { return l.root + "p/" + id }

// configuration is bc-<id>/c/current: a name of its own family, bound to it like an entry's.
func (l sealedLayout) configuration() string { return l.root + "c/current" }

// objectKey is the path of the bytes of a content address: <dir>/<h0h1>/<h2h3>/<64 hex>.
func objectKey(area EntryKind, sha string) (string, error) {
	if !validHash(sha) {
		return "", fmt.Errorf("%w: content address %q", ErrInvalid, shorten(sha))
	}
	dir := dirArtifacts
	if area == EntryDocument {
		dir = dirDocuments
	}
	return dir + "/" + sha[0:2] + "/" + sha[2:4] + "/" + sha, nil
}

// entryKey is the path of an entry: entries/<k0k1>/<sha256 of the cache key>.json.
func entryKey(keyHash string) (string, error) {
	if !validHash(keyHash) {
		return "", fmt.Errorf("%w: key hash %q", ErrInvalid, shorten(keyHash))
	}
	return dirEntries + "/" + keyHash[0:2] + "/" + keyHash + ".json", nil
}

func accessKey(sha string) (string, error) {
	if !validHash(sha) {
		return "", fmt.Errorf("%w: content address %q", ErrInvalid, shorten(sha))
	}
	return dirAccess + "/" + sha, nil
}

func spoolKey(replicaName, name string) (string, error) {
	if !replica.MatchString(replicaName) || !spoolFn.MatchString(name) {
		return "", fmt.Errorf("%w: spool file %q/%q", ErrInvalid, shorten(replicaName), shorten(name))
	}
	return dirSpool + "/" + replicaName + "/" + name, nil
}

func shorten(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// areaKind maps the area label of the metrics to the kind of its bytes.
func areaKind(area string) (EntryKind, error) {
	switch area {
	case metrics.AreaArtifacts:
		return EntryArtifact, nil
	case metrics.AreaDocuments:
		return EntryDocument, nil
	}
	return "", fmt.Errorf("%w: area %q", ErrInvalid, area)
}

// base is the last element of a slash path.
func base(key string) string {
	if i := strings.LastIndexByte(key, '/'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// first is the first element of a slash path.
func first(key string) string {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i]
	}
	return key
}
