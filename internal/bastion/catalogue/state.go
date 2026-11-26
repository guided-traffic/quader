package catalogue

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// docRef is the document a path holds: the SHA-256 and the size of its raw bytes, and its base name in the store.
type docRef struct {
	sha  string
	size int64
	name string
}

// shard is one shard of the manifest: the paths of its units and the document it is kept as ("" until it is written).
type shard struct {
	paths map[string]docRef
	doc   string
}

// snapshot is one state of the catalogue as the replica serves it. It is never changed after it is swapped in: a round builds the
// next one, cloning only the shards that changed.
type snapshot struct {
	cursor      int64
	confirmedAt time.Time
	gui, docs   bool
	public      iface.CataloguePublic
	repos       map[string]iface.CatalogueRepository
	shards      [shardCount]*shard
	index       *index
}

// count is the number of documents the snapshot names.
func (s *snapshot) count() int {
	n := 0
	for _, sh := range s.shards {
		if sh != nil {
			n += len(sh.paths)
		}
	}
	return n
}

// lookup is the document of a path.
func (s *snapshot) lookup(path string) (docRef, bool) {
	sh := s.shards[shardOf(path)]
	if sh == nil {
		return docRef{}, false
	}
	r, ok := sh.paths[path]
	return r, ok
}

// each calls fn for every path of the snapshot.
func (s *snapshot) each(fn func(path string, r docRef)) {
	for _, sh := range s.shards {
		if sh == nil {
			continue
		}
		for p, r := range sh.paths {
			fn(p, r)
		}
	}
}

// withPublic is the snapshot with another public set: what fell out of it is hidden at once (R-BAS-112).
func (s *snapshot) withPublic(pub iface.CataloguePublic) *snapshot {
	n := *s
	n.public, n.repos = pub, reposOf(pub)
	return &n
}

func reposOf(pub iface.CataloguePublic) map[string]iface.CatalogueRepository {
	m := make(map[string]iface.CatalogueRepository, len(pub.Repositories))
	for _, r := range pub.Repositories {
		m[r.Name] = r
	}
	return m
}

// unitOf is the unit a path belongs to, which its shard is chosen by: a package's documents (packages/<id>), a repository's
// set-up (setup/<project>/<repo>), a generic repository's files (files/<project>/<repo>), or the path itself.
func unitOf(path string) string {
	n := 2
	switch {
	case strings.HasPrefix(path, "packages/"):
	case strings.HasPrefix(path, "setup/"), strings.HasPrefix(path, "files/"):
		n = 3
	default:
		return path
	}
	end := 0
	for range n {
		i := strings.IndexByte(path[end:], '/')
		if i < 0 {
			return path
		}
		end += i + 1
	}
	return path[:end-1]
}

func shardOf(path string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(unitOf(path)))
	return int(h.Sum32() % shardCount)
}

// record is the state record (R-BAS-110), the payload the store signs: what a replica that starts needs to serve, and what a
// replica of a shared bucket takes from another.
type record struct {
	Version     int                   `json:"version"`
	Cursor      int64                 `json:"cursor"`
	ConfirmedAt time.Time             `json:"confirmedAt"`
	GUI         bool                  `json:"gui"`
	Docs        bool                  `json:"docs"`
	Public      iface.CataloguePublic `json:"public"`
	// Shards are the documents of the shards of the manifest by their number, "" for an empty shard.
	Shards []string `json:"shards"`
	// Replica is the replica that wrote it, for the log.
	Replica string `json:"replica"`
}

const recordVersion = 1

func (s *snapshot) record(replica string) record {
	r := record{
		Version: recordVersion, Cursor: s.cursor, ConfirmedAt: s.confirmedAt.UTC(), GUI: s.gui, Docs: s.docs, Public: s.public,
		Shards: make([]string, shardCount), Replica: replica,
	}
	for i, sh := range s.shards {
		if sh != nil {
			r.Shards[i] = sh.doc
		}
	}
	return r
}

func decodeRecord(b []byte) (record, error) {
	var r record
	if err := iface.DecodeStrict(bytes.NewReader(b), &r); err != nil {
		return record{}, err
	}
	if r.Version != recordVersion || len(r.Shards) != shardCount || r.Cursor < 0 {
		return record{}, fmt.Errorf("catalogue: a state record of version %d with %d shards", r.Version, len(r.Shards))
	}
	for _, d := range r.Shards {
		if d != "" && !isSHA256(d) {
			return record{}, errors.New("catalogue: a state record names a shard that is no content address")
		}
	}
	return r, nil
}

// shardDoc is the raw form of a shard: its paths in order, each with its document's SHA-256 and size.
type shardDoc struct {
	Paths []shardPath `json:"paths"`
}

type shardPath struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (sh *shard) encode() ([]byte, error) {
	d := shardDoc{Paths: make([]shardPath, 0, len(sh.paths))}
	for p, r := range sh.paths {
		d.Paths = append(d.Paths, shardPath{Path: p, SHA256: r.sha, Size: r.size})
	}
	sort.Slice(d.Paths, func(i, j int) bool { return d.Paths[i].Path < d.Paths[j].Path })
	return json.Marshal(d)
}

func decodeShard(raw []byte, n int, name func(string) (string, error)) (*shard, error) {
	var d shardDoc
	if err := iface.DecodeStrict(bytes.NewReader(raw), &d); err != nil {
		return nil, err
	}
	sh := &shard{paths: make(map[string]docRef, len(d.Paths))}
	for _, p := range d.Paths {
		if _, err := iface.ParseCataloguePath(p.Path); err != nil || shardOf(p.Path) != n || !isSHA256(p.SHA256) || p.Size < 0 {
			return nil, fmt.Errorf("catalogue: shard %d names %q", n, p.Path)
		}
		nm, err := name(p.SHA256)
		if err != nil {
			return nil, err
		}
		sh.paths[p.Path] = docRef{sha: p.SHA256, size: p.Size, name: nm}
	}
	return sh, nil
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// compress is a document's gzip form and the SHA-256 of its raw bytes.
func compress(raw []byte) (gz []byte, sha string, err error) {
	var b bytes.Buffer
	w, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return nil, "", err
	}
	if _, err := w.Write(raw); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return b.Bytes(), hex.EncodeToString(sum[:]), nil
}

// errIntegrity is a document whose bytes are not what its address says (R-BAS-109, R-BAS-113).
var errIntegrity = errors.New("catalogue: a document's bytes do not have the SHA-256 and size it is named by")

// open decompresses a document to at most size bytes and verifies its SHA-256 and size.
func open(gz []byte, sha string, size int64) ([]byte, error) {
	raw, err := openAny(gz, sha, size)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != size {
		return nil, errIntegrity
	}
	return raw, nil
}

// openAny decompresses a document to at most limit bytes and verifies its SHA-256.
func openAny(gz []byte, sha string, limit int64) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errIntegrity, err)
	}
	raw, err := io.ReadAll(io.LimitReader(zr, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errIntegrity, err)
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) > limit || hex.EncodeToString(sum[:]) != sha {
		return nil, errIntegrity
	}
	return raw, nil
}
