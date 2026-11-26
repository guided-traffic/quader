// Package cataloguetest is the in-memory store of the public catalogue for the tests of the builder and of the catalogue routes
// (R-BAS-101 to R-BAS-108): the tables of 00146 as maps, with the same numbering, reset and deletion rules as the PostgreSQL store.
package cataloguetest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
)

type row struct {
	unit, sha string // sha "" for a deleted path
	size      int
	seq       int64
}

type registration struct {
	state *catalogue.State
	rows  map[string]row
	units map[string]catalogue.Unit
}

// Memory is a [catalogue.Store] in memory. The zero value is not usable; use New.
type Memory struct {
	mu   sync.Mutex
	regs []catalogue.Registered
	by   map[string]*registration
	docs map[string][]byte // sha -> gzip
	// Packages and Repositories are the facts the builder fingerprints with; a test changes them to stand for a change in the
	// database.
	Packages     map[catalogue.PackageKey]string
	Repositories map[string]string
	// Writes counts the calls of Write.
	Writes int
}

// New returns an empty store with the registrations.
func New(regs ...catalogue.Registered) *Memory {
	return &Memory{
		regs: regs, by: map[string]*registration{}, docs: map[string][]byte{},
		Packages: map[catalogue.PackageKey]string{}, Repositories: map[string]string{},
	}
}

var _ catalogue.Store = (*Memory)(nil)

func (m *Memory) reg(id string) *registration {
	r := m.by[id]
	if r == nil {
		r = &registration{rows: map[string]row{}, units: map[string]catalogue.Unit{}}
		m.by[id] = r
	}
	return r
}

// Registrations implements catalogue.Store.
func (m *Memory) Registrations(context.Context) ([]catalogue.Registered, error) { return m.regs, nil }

// Held implements catalogue.Store.
func (m *Memory) Held(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, r := range m.by {
		if r.state != nil {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// State implements catalogue.Store.
func (m *Memory) State(_ context.Context, id string) (catalogue.State, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.by[id]
	if r == nil || r.state == nil {
		return catalogue.State{}, false, nil
	}
	return *r.state, true, nil
}

// Begin implements catalogue.Store.
func (m *Memory) Begin(_ context.Context, id string, floor int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.reg(id)
	if r.state == nil {
		r.state = &catalogue.State{Floor: floor, Seq: floor - 1}
	}
	return nil
}

// Stop implements catalogue.Store.
func (m *Memory) Stop(ctx context.Context, id, reason string, floor int64) error {
	_ = m.Begin(ctx, id, floor)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.by[id].state.Stopped = reason
	return nil
}

// Drop implements catalogue.Store.
func (m *Memory) Drop(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.by, id)
	return nil
}

// Units implements catalogue.Store.
func (m *Memory) Units(_ context.Context, id string) (map[string]catalogue.Unit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]catalogue.Unit{}
	for k, v := range m.reg(id).units {
		out[k] = v
	}
	return out, nil
}

// LiveUnits implements catalogue.Store.
func (m *Memory) LiveUnits(_ context.Context, id string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, r := range m.reg(id).rows {
		if r.sha != "" && !seen[r.unit] {
			seen[r.unit] = true
			out = append(out, r.unit)
		}
	}
	sort.Strings(out)
	return out, nil
}

// UnitRows implements catalogue.Store.
func (m *Memory) UnitRows(_ context.Context, id, unit string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for p, r := range m.reg(id).rows {
		if r.unit == unit {
			out[p] = r.sha
		}
	}
	return out, nil
}

// Write implements catalogue.Store.
func (m *Memory) Write(_ context.Context, id, unit string, put []catalogue.Doc, del []string, u *catalogue.Unit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Writes++
	r := m.reg(id)
	for _, d := range put {
		r.state.Seq++
		sha := hex.EncodeToString(d.SHA256[:])
		m.docs[sha] = d.Gzip
		r.rows[d.Path] = row{unit: unit, sha: sha, size: d.Size, seq: r.state.Seq}
	}
	for _, p := range del {
		r.state.Seq++
		r.rows[p] = row{unit: unit, seq: r.state.Seq}
	}
	if u == nil {
		delete(r.units, unit)
	} else {
		r.units[unit] = *u
	}
	return nil
}

// Finish implements catalogue.Store.
func (m *Memory) Finish(_ context.Context, id string, salt []byte, base string, public iface.CataloguePublic, changed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.reg(id).state
	st.Salt, st.BaseURL, st.Stopped, st.Public, st.CheckedAt = salt, base, "", public, time.Now()
	if changed {
		st.BuiltAt = st.CheckedAt
	}
	return nil
}

// Sweep implements catalogue.Store.
func (m *Memory) Sweep(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	named := map[string]bool{}
	for _, r := range m.by {
		for _, x := range r.rows {
			named[x.sha] = true
		}
	}
	var n int64
	for sha := range m.docs {
		if !named[sha] {
			delete(m.docs, sha)
			n++
		}
	}
	return n, nil
}

// PackageFacts implements catalogue.Store.
func (m *Memory) PackageFacts(context.Context) (map[catalogue.PackageKey]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[catalogue.PackageKey]string{}
	for k, v := range m.Packages {
		out[k] = v
	}
	return out, nil
}

// RepositoryFacts implements catalogue.Store.
func (m *Memory) RepositoryFacts(context.Context, []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.Repositories {
		out[k] = v
	}
	return out, nil
}

// Changes implements catalogue.Reader with the rules of the PostgreSQL store.
func (m *Memory) Changes(_ context.Context, id string, after int64, limit int) (iface.CatalogueChanges, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ans := iface.CatalogueChanges{Cursor: after, Entries: []iface.CatalogueChange{}}
	r := m.by[id]
	if r == nil || r.state == nil {
		if after > 0 {
			ans.Reset, ans.Cursor = true, 0
		}
		return ans, nil
	}
	from := after
	if after != 0 && (after < r.state.Floor-1 || after > r.state.Seq) {
		ans.Reset, from, ans.Cursor = true, 0, 0
	}
	var list []iface.CatalogueChange
	for p, x := range r.rows {
		if x.seq <= from || (from == 0 && x.sha == "") {
			continue
		}
		e := iface.CatalogueChange{Path: p, Seq: x.seq, Deleted: x.sha == ""}
		if !e.Deleted {
			e.SHA256, e.Size = x.sha, int64(x.size)
		}
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
	if len(list) > limit {
		list, ans.More = list[:limit], true
	}
	for _, e := range list {
		ans.Entries = append(ans.Entries, e)
		ans.Cursor = e.Seq
	}
	return ans, nil
}

// Documents implements catalogue.Reader.
func (m *Memory) Documents(_ context.Context, id string, paths []string) (map[string]catalogue.Stored, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]catalogue.Stored{}
	r := m.by[id]
	if r == nil {
		return out, nil
	}
	for _, p := range paths {
		if x, ok := r.rows[p]; ok && x.sha != "" {
			out[p] = catalogue.Stored{SHA256: x.sha, Size: int64(x.size), Gzip: m.docs[x.sha]}
		}
	}
	return out, nil
}

// Public implements catalogue.Reader.
func (m *Memory) Public(_ context.Context, id string) (iface.CataloguePublic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.by[id]
	if r == nil || r.state == nil {
		return iface.CataloguePublic{}, nil
	}
	return r.state.Public, nil
}

// Raw answers the raw bytes of every live document of a registration by path, decompressed.
func (m *Memory) Raw(id string) map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]byte{}
	r := m.by[id]
	if r == nil {
		return out
	}
	for p, x := range r.rows {
		if x.sha == "" {
			continue
		}
		zr, err := gzip.NewReader(bytes.NewReader(m.docs[x.sha]))
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(zr)
		out[p] = b
	}
	return out
}

// Seq answers the last number of a registration's change list, 0 without a state.
func (m *Memory) Seq(id string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.by[id]; r != nil && r.state != nil {
		return r.state.Seq
	}
	return 0
}
