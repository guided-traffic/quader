package clamav

import (
	"bytes"
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/clamav/cvd"
	"github.com/guided-traffic/quader/internal/formats/clamav/cvd/cvdtest"
	"github.com/guided-traffic/quader/internal/formats/clamav/rows"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// memStore is the in-memory twin of the data port for the kit and the unit tests: one repository, the side rows by version ID.
type memStore struct {
	mu   sync.Mutex
	rows map[int64]*rows.Version
}

var _ Store = (*memStore)(nil)

func newMem() *memStore { return &memStore{rows: map[int64]*rows.Version{}} }

func (m *memStore) row(id int64, db string, v int) *rows.Version {
	r, ok := m.rows[id]
	if !ok {
		r = &rows.Version{VersionID: id, DB: db, Version: v, StoredAt: time.Unix(1700000000, 0).UTC()}
		m.rows[id] = r
	}
	return r
}

func (m *memStore) PutContainer(_ context.Context, _ formats.Tx, id int64, file string, c rows.Container) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.row(id, c.DB, c.Version)
	r.File, r.Container, r.FilesRemovedAt = file, c, nil
	return nil
}

func (m *memStore) PutSignature(_ context.Context, _ formats.Tx, id int64, name string, size int64, s rows.Signature) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.row(id, s.DB, s.Version)
	r.SignName, r.SignSize, r.SignSignatures = name, size, s.Signatures
	return nil
}

func (m *memStore) FilesRemoved(_ context.Context, _ formats.Tx, ids []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	at := time.Unix(1700000100, 0).UTC()
	for _, id := range ids {
		if r, ok := m.rows[id]; ok && r.FilesRemovedAt == nil {
			r.FilesRemovedAt, r.InUse, r.Previous = &at, false, false
		}
	}
	return nil
}

func (m *memStore) Served(_ context.Context, _ int64, db string) (rows.Served, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	best := rows.Served{}
	for _, r := range m.rows {
		if r.DB == db && r.InUse && r.File != "" && r.Version > best.Version {
			best = rows.Served{Version: r.Version, File: r.File}
		}
	}
	return best, best.Version > 0, nil
}

func (m *memStore) Versions(_ context.Context, ids []int64) ([]rows.Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []rows.Version
	for _, id := range ids {
		if r, ok := m.rows[id]; ok {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *memStore) Stored(_ context.Context, _ int64, db string, limit int) ([]rows.Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []rows.Version
	for _, r := range m.rows {
		if r.DB == db {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := out[i].FilesRemovedAt != nil, out[j].FilesRemovedAt != nil
		if ri != rj {
			return !ri
		}
		return out[i].Version > out[j].Version
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// markInUse marks the stored version of a database in use, as the database task's MarkSets does.
func (m *memStore) markInUse(db string, v int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.DB == db {
			r.InUse = r.Version == v
		}
	}
}

// container is the bytes of a test container of a database's version.
func container(db string, v int) []byte {
	return cvdtest.Container(db, v, cvdtest.Options{Signatures: 2}, cvdtest.Member{Name: db + ".ndb", Body: []byte("Sig.A:0:*:41\nSig.B:0:*:42\n")})
}

// metadataOf is the payload the database task would hand a commit of the container.
func metadataOf(db string, v int, b []byte) rows.Container {
	h, _ := cvd.ParseHeader(b)
	members, read, _ := cvd.ReadMembers(context.Background(), bytes.NewReader(b[cvd.HeaderSize:]), cvd.DefaultBounds())
	out := rows.Container{
		DB: db, Version: v, BuildTimeText: h.BuildTimeText, BuiltAt: h.BuiltAt, Signatures: h.Signatures, FunctionalityLevel: h.FunctionalityLevel,
		Builder: h.Builder, HeaderMD5: h.MD5, ComputedMD5: h.MD5, Verification: rows.VerificationSignature, MembersRead: read,
	}
	for _, mm := range members {
		out.Members = append(out.Members, rows.Member{Name: mm.Name, Size: mm.Size, SHA256: mm.SHA256, Lines: mm.Lines})
	}
	return out
}

// commitVersion commits a container and its detached signature through the host's item store with their payloads, as the database
// task does in process.
func commitVersion(ctx context.Context, h *formattest.Host, db string, v int, withSign bool) error {
	rq, err := h.NewRequest(ctx, http.MethodGet, db+".cvd", nil)
	if err != nil {
		return err
	}
	b := container(db, v)
	sp, err := h.Ingest().Spool(ctx, rq, bytes.NewReader(b), formats.IngestOptions{})
	if err != nil {
		return err
	}
	c := formats.Commit{Path: formats.Path{Path: ContainerPath(db, v, db+".cvd")}, MediaType: "application/octet-stream", MayOverwrite: true, Payload: metadataOf(db, v, b)}
	if withSign {
		ss, err := h.Ingest().Spool(ctx, rq, bytes.NewReader(cvdtest.Sign(2)), formats.IngestOptions{})
		if err != nil {
			return err
		}
		c.Companions = []formats.Companion{{
			Spooled: ss, Path: formats.Path{Path: SignaturePath(db, v)}, MediaType: "text/plain",
			Payload: rows.Signature{DB: db, Version: v, Signatures: 2},
		}}
	}
	_, err = h.Ingest().Commit(ctx, rq, sp, c)
	return err
}

// seedInUse commits the versions of a database and marks the last one in use.
func seedInUse(db string, withSign bool, versions ...int) func(h *formattest.Host) error {
	return func(h *formattest.Host) error {
		for _, v := range versions {
			if err := commitVersion(context.Background(), h, db, v, withSign); err != nil {
				return err
			}
		}
		h.FormatData(ID).(*memStore).markInUse(db, versions[len(versions)-1])
		return nil
	}
}
