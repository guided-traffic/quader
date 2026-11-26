package catalogue

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cataloguebounds"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Doc is a document ready to be kept: its path, the SHA-256 and the size of its raw bytes, and the bytes gzip-compressed (Q-BAS-346).
type Doc struct {
	Path   string
	SHA256 [32]byte
	Size   int
	Gzip   []byte
}

// NewDoc compresses raw and names it.
func NewDoc(path string, raw []byte) (Doc, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return Doc{}, err
	}
	if err := zw.Close(); err != nil {
		return Doc{}, err
	}
	return Doc{Path: path, SHA256: sha256.Sum256(raw), Size: len(raw), Gzip: buf.Bytes()}, nil
}

// Unit is what the builder keeps of a unit between runs: the fingerprint it was rendered at and the signature objects it names.
type Unit struct {
	Fingerprint []byte
	Refs        []string
}

// State is the builder's state of a registration.
type State struct {
	// Floor is the first number of the change list; Seq the last one handed out.
	Floor, Seq int64
	Salt       []byte
	BaseURL    string
	// Stopped is why the builder stopped for the registration (CodeBaseURLUnknown), empty when it did not.
	Stopped            string
	Public             iface.CataloguePublic
	BuiltAt, CheckedAt time.Time
}

// Registered is a registration as the builder lists them.
type Registered struct {
	ID, Name string
	Enabled  bool
}

// PackageKey names a package by its format and its normalized name.
type PackageKey struct{ Format, Name string }

// Stored is a kept document: the SHA-256 and the size of its raw bytes and its compressed bytes.
type Stored struct {
	SHA256 string
	Size   int64
	Gzip   []byte
}

// Store is the builder's door to the tables of 00146; [DB] implements it over PostgreSQL.
type Store interface {
	Reader
	Registrations(ctx context.Context) ([]Registered, error)
	// Held lists the registrations that hold a state.
	Held(ctx context.Context) ([]string, error)
	State(ctx context.Context, id string) (State, bool, error)
	// Begin creates the state of a registration's first build with the change list starting at floor.
	Begin(ctx context.Context, id string, floor int64) error
	Stop(ctx context.Context, id, reason string, floor int64) error
	// Drop deletes the registration's catalogue whole.
	Drop(ctx context.Context, id string) error
	Units(ctx context.Context, id string) (map[string]Unit, error)
	// LiveUnits lists the units that hold a live row.
	LiveUnits(ctx context.Context, id string) ([]string, error)
	// UnitRows answers the paths of a unit with the hex SHA-256 of their documents, "" for a deleted one.
	UnitRows(ctx context.Context, id, unit string) (map[string]string, error)
	// Write keeps the documents put and marks the paths del deleted, all under numbers of one block of the change list, and sets
	// the unit's fingerprint (nil u deletes the unit), in one transaction.
	Write(ctx context.Context, id, unit string, put []Doc, del []string, u *Unit) error
	// Finish records what a run leaves.
	Finish(ctx context.Context, id string, salt []byte, base string, public iface.CataloguePublic, changed bool) error
	// Sweep deletes the documents no row names.
	Sweep(ctx context.Context) (int64, error)
	PackageFacts(ctx context.Context) (map[PackageKey]string, error)
	// RepositoryFacts answers the fingerprint of every repository of the formats by its qualified name.
	RepositoryFacts(ctx context.Context, formats []string) (map[string]string, error)
}

// Reader is what the catalogue routes of the Bastion interface read (R-BAS-101).
type Reader interface {
	// Changes answers the change list after the cursor, at most limit entries (clamped by the caller).
	Changes(ctx context.Context, id string, after int64, limit int) (iface.CatalogueChanges, error)
	// Documents answers the live documents of the paths that the catalogue holds.
	Documents(ctx context.Context, id string, paths []string) (map[string]Stored, error)
	// Public answers the public set of the registration's catalogue, empty before its first build.
	Public(ctx context.Context, id string) (iface.CataloguePublic, error)
}

// DB is the [Store] over PostgreSQL.
type DB struct{ db *postgres.DB }

// NewDB returns the store over db.
func NewDB(db *postgres.DB) *DB { return &DB{db: db} }

var _ Store = (*DB)(nil)

func (s *DB) read(ctx context.Context, fn func(*q.Queries) error) error {
	return s.db.Read(ctx, func(d q.DBTX) error { return fn(q.New(d)) })
}

func (s *DB) tx(ctx context.Context, fn func(*q.Queries) error) error {
	return s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("catalogue: not a PostgreSQL transaction")
		}
		return fn(q.New(ptx))
	})
}

// Registrations implements [Store].
func (s *DB) Registrations(ctx context.Context) ([]Registered, error) {
	var out []Registered
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCatalogueRegistrations(ctx)
		out = make([]Registered, 0, len(rows))
		for _, r := range rows {
			out = append(out, Registered{ID: r.ID, Name: r.Name, Enabled: r.Enabled})
		}
		return err
	})
	return out, err
}

// Held implements [Store].
func (s *DB) Held(ctx context.Context) (out []string, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		out, err = qs.BastionCatalogueHasState(ctx)
		return err
	})
	return out, err
}

// State implements [Store].
func (s *DB) State(ctx context.Context, id string) (State, bool, error) {
	var (
		st State
		ok bool
	)
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCatalogueState(ctx, id)
		if err != nil || len(rows) == 0 {
			return err
		}
		r := rows[0]
		ok = true
		st = State{Floor: r.Floor, Seq: r.Seq, Salt: r.Salt, BaseURL: r.BaseUrl, Stopped: r.Stopped, BuiltAt: r.BuiltAt.Time, CheckedAt: r.CheckedAt.Time}
		return json.Unmarshal(r.Public, &st.Public)
	})
	return st, ok, err
}

// Begin implements [Store].
func (s *DB) Begin(ctx context.Context, id string, floor int64) error {
	return s.tx(ctx, func(qs *q.Queries) error {
		return qs.BastionCatalogueStateInit(ctx, q.BastionCatalogueStateInitParams{BastionID: id, Floor: floor})
	})
}

// Stop implements [Store].
func (s *DB) Stop(ctx context.Context, id, reason string, floor int64) error {
	return s.tx(ctx, func(qs *q.Queries) error {
		return qs.BastionCatalogueStop(ctx, q.BastionCatalogueStopParams{BastionID: id, Floor: floor, Stopped: reason})
	})
}

// Drop implements [Store].
func (s *DB) Drop(ctx context.Context, id string) error {
	return s.tx(ctx, func(qs *q.Queries) error { return qs.BastionCatalogueDrop(ctx, id) })
}

// Units implements [Store].
func (s *DB) Units(ctx context.Context, id string) (map[string]Unit, error) {
	out := map[string]Unit{}
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCatalogueUnits(ctx, id)
		for _, r := range rows {
			out[r.Unit] = Unit{Fingerprint: r.Fingerprint, Refs: r.Refs}
		}
		return err
	})
	return out, err
}

// LiveUnits implements [Store].
func (s *DB) LiveUnits(ctx context.Context, id string) (out []string, err error) {
	err = s.read(ctx, func(qs *q.Queries) error {
		out, err = qs.BastionCatalogueLiveUnits(ctx, id)
		return err
	})
	return out, err
}

// UnitRows implements [Store].
func (s *DB) UnitRows(ctx context.Context, id, unit string) (map[string]string, error) {
	out := map[string]string{}
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCatalogueUnitRows(ctx, q.BastionCatalogueUnitRowsParams{BastionID: id, Unit: unit})
		for _, r := range rows {
			out[r.Path] = hex.EncodeToString(r.Sha256)
		}
		return err
	})
	return out, err
}

// Write implements [Store]. The number block is reserved on the registration's state row in the same transaction, so the
// transactions of one registration commit in the order of their numbers and a reader of the change list never sees a number
// below one it has seen arrive later.
func (s *DB) Write(ctx context.Context, id, unit string, put []Doc, del []string, u *Unit) error {
	return s.tx(ctx, func(qs *q.Queries) error {
		if n := int64(len(put) + len(del)); n > 0 {
			last, err := qs.BastionCatalogueReserve(ctx, q.BastionCatalogueReserveParams{BastionID: id, N: n})
			if err != nil {
				return err
			}
			seq := last - n
			for _, d := range put {
				seq++
				if err := qs.BastionCatalogueDocumentPut(ctx, q.BastionCatalogueDocumentPutParams{Sha256: d.SHA256[:], Size: int32(d.Size), Gzip: d.Gzip}); err != nil { //nolint:gosec // G115: a document is at most cataloguebounds.DocumentMaxBytes
					return err
				}
				if err := qs.BastionCatalogueUpsert(ctx, q.BastionCatalogueUpsertParams{
					BastionID: id, Path: d.Path, Unit: unit, Sha256: d.SHA256[:], Size: int32(d.Size), Seq: seq, //nolint:gosec // G115: as above
				}); err != nil {
					return err
				}
			}
			for _, p := range del {
				seq++
				if err := qs.BastionCatalogueUpsert(ctx, q.BastionCatalogueUpsertParams{BastionID: id, Path: p, Unit: unit, Seq: seq}); err != nil {
					return err
				}
			}
		}
		if u == nil {
			return qs.BastionCatalogueUnitDelete(ctx, q.BastionCatalogueUnitDeleteParams{BastionID: id, Unit: unit})
		}
		refs := u.Refs
		if refs == nil {
			refs = []string{}
		}
		return qs.BastionCatalogueUnitSet(ctx, q.BastionCatalogueUnitSetParams{BastionID: id, Unit: unit, Fingerprint: u.Fingerprint, Refs: refs})
	})
}

// Finish implements [Store].
func (s *DB) Finish(ctx context.Context, id string, salt []byte, base string, public iface.CataloguePublic, changed bool) error {
	pub, err := json.Marshal(public)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(qs *q.Queries) error {
		return qs.BastionCatalogueStateSet(ctx, q.BastionCatalogueStateSetParams{
			BastionID: id, Salt: salt, BaseUrl: base, Stopped: "", Public: pub, Changed: changed,
		})
	})
}

// Sweep implements [Store].
func (s *DB) Sweep(ctx context.Context) (n int64, err error) {
	err = s.tx(ctx, func(qs *q.Queries) error {
		n, err = qs.BastionCatalogueSweep(ctx)
		return err
	})
	return n, err
}

// PackageFacts implements [Store].
func (s *DB) PackageFacts(ctx context.Context) (map[PackageKey]string, error) {
	out := map[PackageKey]string{}
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCataloguePackageFacts(ctx)
		for _, r := range rows {
			out[PackageKey{Format: r.Format, Name: r.Name}] = r.Fingerprint
		}
		return err
	})
	return out, err
}

// RepositoryFacts implements [Store].
func (s *DB) RepositoryFacts(ctx context.Context, formats []string) (map[string]string, error) {
	out := map[string]string{}
	if len(formats) == 0 {
		return out, nil
	}
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCatalogueRepositoryFacts(ctx, formats)
		for _, r := range rows {
			out[r.Repository] = r.Fingerprint
		}
		return err
	})
	return out, err
}

// Changes implements [Reader]: the entries after the cursor, or, for a cursor the catalogue does not continue (one of an earlier
// catalogue of the registration, a restored or a dropped one), the whole list with reset (R-BAS-101). A list read from the start
// leaves the deleted paths out: a Bastion that holds nothing has nothing to delete.
func (s *DB) Changes(ctx context.Context, id string, after int64, limit int) (iface.CatalogueChanges, error) {
	ans := iface.CatalogueChanges{Cursor: after}
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCatalogueState(ctx, id)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			if after > 0 {
				ans.Reset, ans.Cursor = true, 0
			}
			return nil
		}
		st := rows[0]
		from := after
		if after != 0 && (after < st.Floor-1 || after > st.Seq) {
			ans.Reset, from, ans.Cursor = true, 0, 0
		}
		list, err := qs.BastionCatalogueChanges(ctx, q.BastionCatalogueChangesParams{
			BastionID: id, After: from, LiveOnly: from == 0, MaxRows: int32(limit + 1), //nolint:gosec // G115: a limit of ClampLimit
		})
		if err != nil {
			return err
		}
		if len(list) > limit {
			list, ans.More = list[:limit], true
		}
		ans.Entries = make([]iface.CatalogueChange, 0, len(list))
		for _, r := range list {
			e := iface.CatalogueChange{Path: r.Path, Seq: r.Seq, Deleted: r.Sha256 == nil}
			if !e.Deleted {
				e.SHA256, e.Size = hex.EncodeToString(r.Sha256), int64(r.Size)
			}
			ans.Entries = append(ans.Entries, e)
			ans.Cursor = r.Seq
		}
		return nil
	})
	return ans, err
}

// Documents implements [Reader].
func (s *DB) Documents(ctx context.Context, id string, paths []string) (map[string]Stored, error) {
	out := make(map[string]Stored, len(paths))
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.BastionCatalogueDocuments(ctx, q.BastionCatalogueDocumentsParams{BastionID: id, Paths: paths})
		for _, r := range rows {
			out[r.Path] = Stored{SHA256: hex.EncodeToString(r.Sha256), Size: int64(r.Size), Gzip: r.Gzip}
		}
		return err
	})
	return out, err
}

// Public implements [Reader].
func (s *DB) Public(ctx context.Context, id string) (iface.CataloguePublic, error) {
	st, ok, err := s.State(ctx, id)
	if err != nil || !ok {
		return iface.CataloguePublic{}, err
	}
	return st.Public, nil
}

// ClampLimit is the change list's limit of a request: the page without one, at most the bound (R-BAS-101).
func ClampLimit(limit *int) int {
	switch {
	case limit == nil:
		return cataloguebounds.ChangesPage
	case *limit > cataloguebounds.ChangesPageMax:
		return cataloguebounds.ChangesPageMax
	}
	return *limit
}
