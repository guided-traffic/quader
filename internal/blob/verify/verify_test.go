package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// fakeDB is a Source over a slice of rows; it records the page sizes it was asked for.
type fakeDB struct {
	rows  []Row
	items map[blob.Digest][]Item
	since []time.Time
	pages []int
}

func (f *fakeDB) Page(_ context.Context, after blob.Digest, first bool, since time.Time, limit int) ([]Row, error) {
	f.pages = append(f.pages, limit)
	f.since = append(f.since, since)
	var out []Row
	for _, r := range f.rows {
		if !first && bytes.Compare(r.Digest[:], after[:]) <= 0 {
			continue
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeDB) Items(_ context.Context, d blob.Digest, limit int) ([]Item, int64, error) {
	all := f.items[d]
	n := int64(len(all))
	if len(all) > limit {
		all = all[:limit]
	}
	return all, n, nil
}

// fixture is n blobs of 3 bytes with a present copy on each of the stores, named first, second, … in order.
func fixture(t *testing.T, n int, stores ...*blobtest.Store) *fakeDB {
	t.Helper()
	db := &fakeDB{items: map[blob.Digest][]Item{}}
	for i := range n {
		data := []byte{byte(i), byte(i >> 8), 'x'}
		d := blob.Digest(sha256.Sum256(data))
		row := Row{Digest: d, Size: int64(len(data))}
		for j, s := range stores {
			s.Set(blob.Key("quader", d), data, time.Now())
			row.Copies = append(row.Copies, Copy{Connection: connIDs[j], Key: blob.Key("quader", d), StoredSize: int64(len(data)), State: blob.CopyPresent})
		}
		db.rows = append(db.rows, row)
		db.items[d] = []Item{{Project: "p", Repository: "npm-local", Path: "pkg/" + d.Hex()[:6]}}
	}
	sort.Slice(db.rows, func(i, j int) bool { return bytes.Compare(db.rows[i].Digest[:], db.rows[j].Digest[:]) < 0 })
	return db
}

var connIDs = []string{"default", "secondary"}

func conns(stores ...*blobtest.Store) []Conn {
	out := make([]Conn, 0, len(stores))
	for i, s := range stores {
		out = append(out, Conn{ID: connIDs[i], Objects: s})
	}
	return out
}

func noSleep(context.Context, time.Duration) error { return nil }

// TestR_HA_024_VerifyReportsEveryItemWithoutItsObject: a run over a table of several pages finds exactly the copies whose
// object was removed or has another size, names the items of the blobs that are lost with them, and reads the table in pages
// of the configured size.
func TestR_HA_024_VerifyReportsEveryItemWithoutItsObject(t *testing.T) {
	store := blobtest.New()
	db := fixture(t, 25, store)
	gone := db.rows[3].Digest
	short := db.rows[17].Digest
	if err := store.Delete(context.Background(), blob.Key("quader", gone)); err != nil {
		t.Fatal(err)
	}
	store.Set(blob.Key("quader", short), []byte("z"), time.Now())
	db.items[gone] = append(db.items[gone], Item{Project: "p", Repository: "npm-local", Path: "other", Trashed: true})

	rep, err := Run(context.Background(), Options{DB: db, Connections: conns(store), PageSize: 10, Concurrency: 4, MaxItems: 1, Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Checked != 25 || rep.Lost != 2 || rep.Missing != 1 || rep.SizeMismatch != 1 || len(rep.Findings) != 2 || len(rep.Copies) != 2 || rep.OK() {
		t.Fatalf("report = %+v", rep)
	}
	if rep.ItemsAffected != 3 {
		t.Errorf("ItemsAffected = %d, want 3", rep.ItemsAffected)
	}
	f := rep.Findings[0]
	if f.Kind != KindMissing || f.SHA256 != gone.Hex() || len(f.Items) != 1 || f.ItemCount != 2 {
		t.Errorf("first finding = %+v, want the missing blob with one listed item of two", f)
	}
	if g := rep.Copies[1]; g.Kind != KindSizeMismatch || g.ObjectSize != 1 || g.StoredSize != 3 || g.Connection != "default" {
		t.Errorf("second copy finding = %+v", g)
	}
	if len(db.pages) != 4 { // 10 + 10 + 5 + the empty page that ends the run
		t.Errorf("pages = %v, want 4 queries", db.pages)
	}
	for _, p := range db.pages {
		if p != 10 {
			t.Errorf("a query asked for %d rows, want the page size 10", p)
		}
	}
	if len(rep.Connections) != 1 || rep.Connections[0].Checked != 25 || rep.Connections[0].Missing != 1 || rep.Connections[0].SizeMismatch != 1 {
		t.Errorf("connections = %+v", rep.Connections)
	}
}

// TestR_STOR_058_VerifyChecksEveryCopyOnItsConnection: with two connections a copy missing on one is a finding of that
// connection and the blob is not lost; missing on both, the blob is lost; with --connection only that connection's copies are
// asked, and its missing copy is its finding while the blob counts as not lost.
func TestR_STOR_058_VerifyChecksEveryCopyOnItsConnection(t *testing.T) {
	ctx := context.Background()
	first, second := blobtest.New(), blobtest.New()
	db := fixture(t, 3, first, second)
	one, both := db.rows[0].Digest, db.rows[1].Digest
	for _, k := range []string{blob.Key("quader", one), blob.Key("quader", both)} {
		if err := first.Delete(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := second.Delete(ctx, blob.Key("quader", both)); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(ctx, Options{DB: db, Connections: conns(first, second), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Lost != 1 || rep.Findings[0].SHA256 != both.Hex() || len(rep.Copies) != 3 {
		t.Fatalf("lost = %d, findings = %+v, copies = %+v", rep.Lost, rep.Findings, rep.Copies)
	}
	if rep.ConnectionFindings("default") != 2 || rep.ConnectionFindings("secondary") != 1 {
		t.Errorf("connections = %+v", rep.Connections)
	}

	heads := second.Count("head")
	rep, err = Run(ctx, Options{DB: db, Connections: conns(first, second), Connection: "default", Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if second.Count("head") != heads {
		t.Error("a run named for default asked the store of secondary")
	}
	if rep.Lost != 0 || rep.ConnectionFindings("default") != 2 || len(rep.Connections) != 1 {
		t.Errorf("rep = %+v", rep)
	}
	if _, err := Run(ctx, Options{DB: db, Connections: conns(first, second), Connection: "archive", Sleep: noSleep}); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("err = %v, want ErrUnknownConnection", err)
	}
}

// TestR_STOR_058_ABlobWithoutAPresentCopyIsLost: a blob whose copies are all marked damaged, or that has none, is lost
// without a question to a store; a present copy on a connection the configuration lacks keeps the blob from being lost.
func TestR_STOR_058_ABlobWithoutAPresentCopyIsLost(t *testing.T) {
	store := blobtest.New()
	db := fixture(t, 3, store)
	db.rows[0].Copies[0].State = blob.CopyMissing
	db.rows[1].Copies = nil
	db.rows[2].Copies = append(db.rows[2].Copies, Copy{Connection: "archive", Key: "elsewhere", StoredSize: 3, State: blob.CopyPresent})
	if err := store.Delete(context.Background(), db.rows[2].Copies[0].Key); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(context.Background(), Options{DB: db, Connections: conns(store), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Lost != 2 || len(rep.Copies) != 1 || store.Count("head") != 1 {
		t.Errorf("lost = %d, copies = %+v, heads = %d", rep.Lost, rep.Copies, store.Count("head"))
	}
}

// TestR_HA_024_VerifyOfAnIntactStoreIsOK: no finding, nothing unchecked.
func TestR_HA_024_VerifyOfAnIntactStoreIsOK(t *testing.T) {
	store := blobtest.New()
	db := fixture(t, 7, store)
	rep, err := Run(context.Background(), Options{DB: db, Connections: conns(store), Sleep: noSleep})
	if err != nil || !rep.OK() || rep.Checked != 7 {
		t.Fatalf("rep = %+v, err = %v", rep, err)
	}
}

// TestR_HA_025_VerifyPassesTheSinceCutOffToTheTable: the cut-off reaches the query, so that a restore drill can narrow a run.
func TestR_HA_025_VerifyPassesTheSinceCutOffToTheTable(t *testing.T) {
	store := blobtest.New()
	db := fixture(t, 2, store)
	cut := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	rep, err := Run(context.Background(), Options{DB: db, Connections: conns(store), Since: cut, Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Since.Equal(cut) || !db.since[0].Equal(cut) {
		t.Errorf("since = %v / %v, want %v", rep.Since, db.since[0], cut)
	}
}

// TestR_STOR_058_VerifyTreatsAStoreErrorAsUncheckedNotMissing: a 5xx that outlasts the attempts is not a lost object; a
// permission error ends the checks of that connection, and the run goes on with the other.
func TestR_STOR_058_VerifyTreatsAStoreErrorAsUncheckedNotMissing(t *testing.T) {
	store := blobtest.New()
	db := fixture(t, 3, store)
	flaky := db.rows[1].Digest
	var calls atomic.Int32
	store.SetFault(func(op, key string) error {
		if op == "head" && strings.HasSuffix(key, flaky.Hex()) {
			calls.Add(1)
			return &blob.StoreError{Op: "HeadObject", Kind: blob.KindTransient, Status: 503}
		}
		return nil
	})
	rep, err := Run(context.Background(), Options{DB: db, Connections: conns(store), Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unchecked) != 1 || rep.Unchecked[0].SHA256 != flaky.Hex() || rep.Lost != 0 || calls.Load() != headAttempts || rep.OK() {
		t.Errorf("rep = %+v, attempts = %d", rep, calls.Load())
	}

	denied, open := blobtest.New(), blobtest.New()
	db = fixture(t, 3, denied, open)
	denied.SetFault(func(op, _ string) error {
		if op == "head" {
			return &blob.StoreError{Op: "HeadObject", Kind: blob.KindConfig, Code: "AccessDenied", Status: 403}
		}
		return nil
	})
	rep, err = Run(context.Background(), Options{DB: db, Connections: conns(denied, open), Concurrency: 1, Sleep: noSleep})
	if !blob.IsConfig(err) {
		t.Errorf("err = %v, want a configuration error of the store", err)
	}
	if denied.Count("head") != 1 || open.Count("head") != 3 || rep.Lost != 0 || rep.Connections[0].Error == "" || rep.Connections[0].Unchecked != 3 {
		t.Errorf("heads = %d / %d, rep = %+v", denied.Count("head"), open.Count("head"), rep)
	}
}

// TestR_HA_024_VerifyRetriesATransientErrorBeforeItGivesUp: one failure and then an answer is a checked copy.
func TestR_HA_024_VerifyRetriesATransientErrorBeforeItGivesUp(t *testing.T) {
	store := blobtest.New()
	db := fixture(t, 1, store)
	var n atomic.Int32
	store.SetFault(func(op, _ string) error {
		if op == "head" && n.Add(1) == 1 {
			return errors.New("connection reset")
		}
		return nil
	})
	rep, err := Run(context.Background(), Options{DB: db, Connections: conns(store), Sleep: noSleep})
	if err != nil || !rep.OK() || rep.Checked != 1 {
		t.Fatalf("rep = %+v, err = %v", rep, err)
	}
}

// TestR_STOR_087_TheOperatorCheckOpensTheRecordOfAnEncryptedCopy: on an encrypted connection the check opens each copy's record from
// the HeadObject it sends, against the copy row and the blob row, and reads no body (storage.md R-STOR-087): a whole copy is no
// finding; a changed wrap is a record finding of reason record; a record of a key the replica lost one of reason unknown_key; the blob
// whose only copy fails is lost with kind record.
func TestR_STOR_087_TheOperatorCheckOpensTheRecordOfAnEncryptedCopy(t *testing.T) {
	ctx := context.Background()
	store, ks := blobtest.New(), blobtest.NewKeys()
	sk := ks.Draw("default", 1)
	p := "quader"
	c := blob.NewConnection(config.StorageConnection{ID: "default", Bucket: "a", Prefix: &p, Encrypted: true}, store)
	c.UseKeys(ks)
	db := &fakeDB{items: map[blob.Digest][]Item{}}
	for i := range 3 {
		data := bytes.Repeat([]byte{byte(i + 1)}, 1024)
		d := blob.Digest(sha256.Sum256(data))
		cw, err := c.WriteSealedStream(ctx, d, 1024, bytes.NewReader(data), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		db.rows = append(db.rows, Row{Digest: d, Size: 1024, Copies: []Copy{{Connection: "default", Key: cw.Key, StoredSize: cw.StoredSize, State: blob.CopyPresent}}})
	}
	sort.Slice(db.rows, func(i, j int) bool { return bytes.Compare(db.rows[i].Digest[:], db.rows[j].Digest[:]) < 0 })
	changed := db.rows[1].Copies[0].Key
	md := store.MetadataOf(changed)
	md[blobcrypt.MetaWrap] = "A" + md[blobcrypt.MetaWrap][1:]
	if md[blobcrypt.MetaWrap] == store.MetadataOf(changed)[blobcrypt.MetaWrap] {
		md[blobcrypt.MetaWrap] = "B" + md[blobcrypt.MetaWrap][1:]
	}
	store.SetMetadata(changed, md)
	gets := store.Count("get")
	run := func() *Report {
		rep, err := Run(ctx, Options{DB: db, Connections: []Conn{{ID: "default", Objects: store, Keys: func(ctx context.Context) blobcrypt.KeyLookup {
			return ks.Lookup(ctx, "default")
		}}}, Sleep: noSleep})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	rep := run()
	if len(rep.Copies) != 1 || rep.Copies[0].Kind != KindRecord || rep.Copies[0].Reason != "record" || rep.Copies[0].Key != changed ||
		rep.Lost != 1 || rep.Record != 1 || rep.Findings[0].Kind != KindRecord || rep.ConnectionFindings("default") != 1 {
		t.Fatalf("report %+v", rep)
	}
	if store.Count("get") != gets {
		t.Fatal("the check read a body")
	}
	ks.Forget("default", sk)
	rep = run()
	if len(rep.Copies) != 3 || rep.Copies[0].Reason == "" || rep.Lost != 3 {
		t.Fatalf("with the key lost: %+v", rep)
	}
	for _, cf := range rep.Copies {
		if cf.Key != changed && cf.Reason != "unknown_key" {
			t.Fatalf("copy %s: reason %s, want unknown_key", cf.Key, cf.Reason)
		}
	}
}
