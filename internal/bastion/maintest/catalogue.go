package maintest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cataloguebounds"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The registration's public catalogue as the main installation's builder keeps it (R-BAS-101 to R-BAS-108; the store of
// internal/bastionmain/catalogue and its in-memory twin cataloguetest): a row per path with the document's SHA-256 and size of its
// raw bytes or deleted, numbered by a sequence that starts at the clock in microseconds, so that a cursor below the catalogue's
// first number or above its last answers reset; a change list read from 0 leaves the deleted paths out; every document is kept
// gzip-compressed once per content. The routes answer 404 not_found while the switch gui of the configuration the handshake sends
// is off, and a documentation path is missing while docs is off (R-BAS-102). The test sets what the catalogue holds.

// catalogueRow is one path of the change list.
type catalogueRow struct {
	sha  string // "" for deleted
	size int64
	seq  int64
}

// catalogue is the fake's public catalogue.
type catalogue struct {
	floor, seq int64
	rows       map[string]catalogueRow
	docs       map[string][]byte // gzip by the SHA-256 of the raw bytes
	public     iface.CataloguePublic
	built      bool
	// corrupt are the paths whose next frame carries bytes that do not verify.
	corrupt map[string]bool
	// batches are the paths of every batch read, in order.
	batches [][]string
}

func newCatalogue() *catalogue {
	return &catalogue{
		rows: map[string]catalogueRow{}, docs: map[string][]byte{}, corrupt: map[string]bool{},
		public: iface.CataloguePublic{Projects: []string{}, Repositories: []iface.CatalogueRepository{}},
	}
}

// next is the number of the next change.
func (c *catalogue) next(now time.Time) int64 {
	if !c.built {
		c.built = true
		c.floor = max(now.UnixMicro(), c.seq+1)
		c.seq = c.floor - 1
	}
	c.seq++
	return c.seq
}

// PutDocument keeps a document of the catalogue under its path, as a run of the builder does: a new number in the change list.
func (m *Main) PutDocument(path string, raw []byte) {
	if _, err := iface.ParseCataloguePath(path); err != nil {
		m.t.Fatalf("maintest: %v", err)
	}
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write(raw)
	_ = w.Close()
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.cat
	c.docs[sha] = b.Bytes()
	c.rows[path] = catalogueRow{sha: sha, size: int64(len(raw)), seq: c.next(m.now())}
}

// DeleteDocument marks a path of the catalogue deleted.
func (m *Main) DeleteDocument(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.cat
	if _, ok := c.rows[path]; ok {
		c.rows[path] = catalogueRow{seq: c.next(m.now())}
	}
}

// SetPublic sets the public set of the catalogue.
func (m *Main) SetPublic(pub iface.CataloguePublic) { m.lock(func() { m.cat.public = pub }) }

// RebuildCatalogue drops the catalogue as a restore or the switch gui off and on does (R-BAS-108): the next document starts a
// numbering above every number before, so a Bastion's cursor answers reset.
func (m *Main) RebuildCatalogue() {
	m.lock(func() {
		old := m.cat
		m.cat = newCatalogue()
		m.cat.seq, m.cat.public = old.seq, old.public
	})
}

// CorruptNext makes the next frame of the path carry bytes that do not verify against the SHA-256 its header names.
func (m *Main) CorruptNext(path string) { m.lock(func() { m.cat.corrupt[path] = true }) }

// CatalogueBatches returns the paths of every batch read of documents, in order.
func (m *Main) CatalogueBatches() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]string(nil), m.cat.batches...)
}

// catalogueOp answers the four catalogue routes (R-BAS-101) for the registration's switches.
func (m *Main) catalogueOp(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	cfg := Configuration(m.handover)
	if m.configure != nil {
		m.configure(&cfg)
	}
	m.mu.Unlock()
	if !cfg.Switches.GUI {
		m.problem(w, http.StatusNotFound, "not_found", 0)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == iface.PathCatalogue:
		m.catalogueChanges(w, r)
	case r.Method == http.MethodGet && r.URL.Path == iface.PathCataloguePublic:
		m.mu.Lock()
		pub := m.cat.public
		m.mu.Unlock()
		m.writeJSON(w, pub)
	case r.Method == http.MethodPost && r.URL.Path == iface.PathCatalogueDocuments:
		m.catalogueBatch(w, r, cfg.Switches.Docs)
	default:
		m.problem(w, http.StatusNotFound, "not_found", 0)
	}
}

// catalogueChanges answers the change list after the cursor, as the builder's store does.
func (m *Main) catalogueChanges(w http.ResponseWriter, r *http.Request) {
	after, err := strconv.ParseInt(r.URL.Query().Get(iface.QueryAfter), 10, 64)
	if err != nil || after < 0 {
		http.Error(w, "after", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get(iface.QueryLimit))
	if limit <= 0 {
		limit = cataloguebounds.ChangesPage
	}
	limit = min(limit, cataloguebounds.ChangesPageMax)
	m.mu.Lock()
	c := m.cat
	ans := iface.CatalogueChanges{Cursor: after, Entries: []iface.CatalogueChange{}}
	from := after
	if after != 0 && (!c.built || after < c.floor-1 || after > c.seq) {
		ans.Reset, from, ans.Cursor = true, 0, 0
	}
	var list []iface.CatalogueChange
	for p, x := range c.rows {
		if x.seq <= from || (from == 0 && x.sha == "") {
			continue
		}
		e := iface.CatalogueChange{Path: p, Seq: x.seq, Deleted: x.sha == ""}
		if !e.Deleted {
			e.SHA256, e.Size = x.sha, x.size
		}
		list = append(list, e)
	}
	m.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
	if len(list) > limit {
		list, ans.More = list[:limit], true
	}
	for _, e := range list {
		ans.Entries = append(ans.Entries, e)
		ans.Cursor = e.Seq
	}
	m.writeJSON(w, ans)
}

// catalogueBatch answers the frames of a batch read: one per path in the request's order, missing for a path the catalogue does
// not hold or a documentation path while docs is off, up to the bound of compressed bytes and at least one.
func (m *Main) catalogueBatch(w http.ResponseWriter, r *http.Request, docs bool) {
	var req iface.CatalogueBatch
	if err := iface.DecodeStrict(r.Body, &req); err != nil || req.Validate(cataloguebounds.BatchMaxPaths) != nil {
		http.Error(w, "batch", http.StatusBadRequest)
		return
	}
	type frame struct {
		f  iface.CatalogueFrame
		gz []byte
	}
	var out []frame
	total := 0
	m.mu.Lock()
	c := m.cat
	c.batches = append(c.batches, append([]string(nil), req.Paths...))
	for _, p := range req.Paths {
		cp, _ := iface.ParseCataloguePath(p)
		row, ok := c.rows[p]
		if !ok || row.sha == "" || (!docs && cp.Documentation()) {
			out = append(out, frame{f: iface.CatalogueFrame{Path: p, Missing: true}})
			continue
		}
		gz := c.docs[row.sha]
		if c.corrupt[p] {
			delete(c.corrupt, p)
			gz = corrupted()
		}
		if len(out) > 0 && total+len(gz) > cataloguebounds.BatchMaxBytes {
			break
		}
		total += len(gz)
		out = append(out, frame{f: iface.CatalogueFrame{Path: p, SHA256: row.sha, Size: row.size}, gz: gz})
	}
	m.mu.Unlock()
	w.Header().Set("Content-Type", iface.CatalogueBatchMediaType)
	for _, f := range out {
		if err := iface.WriteCatalogueFrame(w, f.f, f.gz); err != nil {
			return
		}
	}
}

// corrupted is a gzip stream of other bytes than the document's.
func corrupted() []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte("not the document"))
	_ = w.Close()
	return b.Bytes()
}
