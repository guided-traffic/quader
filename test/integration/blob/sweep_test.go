//go:build integration

package blob

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
)

// The orphan sweep (storage.md R-STOR-026) against PostgreSQL with two in-memory connections and a clock of the test (R-STOR-063,
// objects of bytes to 1 KiB). Every subtest has connections of its own over one database; their digests differ.

// orphanAt is a digest whose name lies below the top-level prefix hh, made from seed.
func orphanAt(hh byte, seed byte) blob.Digest {
	d := blob.Digest(sha256.Sum256([]byte{seed, hh, 0x5e}))
	d[0] = hh
	return d
}

// sweeper is a collector whose clock runs ahead by ahead, so that what the test planted now is older than min_age.
func (m *memConns) sweeper(ahead time.Duration) *blob.Collector {
	return m.collector(blob.WithCollectorClock(func() time.Time { return time.Now().Add(ahead) }))
}

func (m *memConns) pastMinAge() time.Duration { return m.cfg.Orphans.MinAge + time.Hour }

// sweepRun is the job framework's handle of one orphan-sweep job.
type sweepRun struct {
	checkpoint []byte
	saved      []blob.SweepCursor
	result     blob.SweepResult
}

func (f *sweepRun) Draining() <-chan struct{} { return nil }

func (f *sweepRun) LoadCheckpoint(v any) (bool, error) {
	if f.checkpoint == nil {
		return false, nil
	}
	return true, json.Unmarshal(f.checkpoint, v)
}

func (f *sweepRun) SaveCheckpoint(_ context.Context, v any) error {
	f.saved = append(f.saved, v.(blob.SweepCursor))
	return nil
}

func (f *sweepRun) SetResult(v any) error { f.result = v.(blob.SweepResult); return nil }

func connResult(t *testing.T, res blob.SweepResult, id string) blob.SweepConnectionResult {
	t.Helper()
	for _, c := range res.Connections {
		if c.Connection == id {
			return c
		}
	}
	t.Fatalf("the result names no connection %s: %+v", id, res)
	return blob.SweepConnectionResult{}
}

func TestR_STOR_026_OrphanSweep(t *testing.T) {
	p := newPG(t)
	ctx := context.Background()
	old := time.Now().Add(-time.Minute)

	t.Run("R-STOR-026 an object without a blob row is reported by a dry run and removed once older than min_age; a foreign name is only reported", func(t *testing.T) {
		m := newMemConns(t, p, 5)
		orphan, young := orphanAt(0, 1), orphanAt(0, 2)
		m.b.Set(key(orphan), memData(64, 1), old)
		foreign := "quader/sha256/00/zz/not-a-digest"
		m.b.Set(foreign, []byte("x"), old)
		dry, err := m.sweeper(m.pastMinAge()).Sweep(ctx, blob.SweepOptions{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if r := connResult(t, dry, "secondary"); r.OrphansFound != 1 || r.OrphansDeleted != 0 || r.ForeignKeys != 1 || r.PrefixesDone != 256 {
			t.Fatalf("dry run %+v", r)
		}
		if _, ok := m.b.Data(key(orphan)); !ok {
			t.Fatal("the dry run deleted the orphan")
		}
		m.b.Set(key(young), memData(64, 2), time.Now().Add(m.pastMinAge())) // written "now" on the sweep's clock: younger than min_age
		res, err := m.sweeper(m.pastMinAge()).Sweep(ctx, blob.SweepOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := connResult(t, res, "secondary"); r.OrphansDeleted != 1 || r.BytesReclaimed != 64 || r.ForeignKeys != 1 {
			t.Fatalf("sweep %+v", r)
		}
		if _, ok := m.b.Data(key(orphan)); ok {
			t.Fatal("the orphan older than min_age remains")
		}
		if _, ok := m.b.Data(key(young)); !ok {
			t.Fatal("an orphan younger than min_age was deleted")
		}
		if _, ok := m.b.Data(foreign); !ok {
			t.Fatal("the foreign key was deleted")
		}
		if _, _, ok := p.state(t, orphan); ok {
			t.Fatal("the claim of the orphan remains after the settle wait")
		}
	})

	t.Run("R-STOR-026 the object of a live blob without a copy row is adopted present and not verified; on a draining connection it is left to the drain", func(t *testing.T) {
		m := newMemConns(t, p, 5)
		data := memData(1024, 10)
		d := commit(t, m.store, data) // on the primary, queued for the secondary
		m.b.Set(key(d), data, old)
		res, err := m.sweeper(0).Sweep(ctx, blob.SweepOptions{Connection: "secondary"})
		if err != nil {
			t.Fatal(err)
		}
		if r := connResult(t, res, "secondary"); r.Adopted != 1 {
			t.Fatalf("sweep %+v", r)
		}
		if p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'secondary' AND state = 'present' AND verified_at IS NULL", d.Bytes()) != 1 {
			t.Fatal("the adopted copy is not present and unverified")
		}

		drained := newMemConns(t, p, 5, func(c *config.Storage) { c.Connections[1].Drain = true })
		data2 := memData(1024, 11)
		d2 := commit(t, drained.store, data2)
		drained.b.Set(key(d2), data2, old)
		orphan := orphanAt(0x40, 11)
		drained.b.Set(key(orphan), memData(32, 11), old)
		res, err = drained.sweeper(drained.pastMinAge()).Sweep(ctx, blob.SweepOptions{Connection: "secondary"})
		if err != nil {
			t.Fatal(err)
		}
		if r := connResult(t, res, "secondary"); r.Adopted != 0 || r.OrphansDeleted != 1 {
			t.Fatalf("sweep of a draining connection %+v", r)
		}
		if p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'secondary'", d2.Bytes()) != 0 {
			t.Fatal("the sweep adopted an object on a draining connection")
		}
	})

	t.Run("R-STOR-026 a copy whose object has another size, confirmed by a second look, is marked corrupt and queued for repair", func(t *testing.T) {
		m := newMemConns(t, p, 5)
		data := memData(1024, 20)
		d := commit(t, m.store, data)
		m.a.Set(key(d), data[:1000], old) // replaced behind Quader's back
		res, err := m.sweeper(0).Sweep(ctx, blob.SweepOptions{Connection: "primary"})
		if err != nil {
			t.Fatal(err)
		}
		if r := connResult(t, res, "primary"); r.CopiesMarked != 1 {
			t.Fatalf("sweep %+v", r)
		}
		if p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'primary' AND state = 'corrupt'", d.Bytes()) != 1 ||
			p.count(t, "blob_copy_queue WHERE sha256 = $1 AND connection_id = 'primary'", d.Bytes()) != 1 {
			t.Fatal("the copy is not corrupt with a queued repair")
		}
		if _, status, _ := p.state(t, d); status != blob.StatusCorrupt {
			t.Fatalf("the blob is %q; its only copy is corrupt", status)
		}
	})

	t.Run("R-STOR-026 a sweep cancelled at prefix 7f of the second connection continues there with resume", func(t *testing.T) {
		m := newMemConns(t, p, 5)
		orphan := orphanAt(0x80, 30)
		m.b.Set(key(orphan), memData(16, 30), old)
		rctx, cancel := context.WithCancel(ctx)
		defer cancel()
		m.b.SetFault(func(op, k string) error {
			if op == "list" && k == "quader/sha256/7f/" {
				cancel()
				return context.Canceled
			}
			return nil
		})
		first := &sweepRun{}
		if err := m.sweeper(m.pastMinAge()).RunSweepJob(rctx, json.RawMessage(`{}`), first); !errors.Is(err, context.Canceled) {
			t.Fatalf("the first run: %v", err)
		}
		last := first.saved[len(first.saved)-1]
		if last != (blob.SweepCursor{Connection: "secondary", Prefix: "7e"}) {
			t.Fatalf("the last checkpoint is %+v", last)
		}
		cp, _ := json.Marshal(last)
		p.exec(t, `INSERT INTO job (id, kind, queue, args, state, checkpoint, started_at, finished_at)
			VALUES (gen_random_uuid(), 'orphan-sweep', 'maintenance', '{}', 'cancelled', $1::jsonb, now(), now())`, string(cp))

		m.b.SetFault(nil)
		listedA := m.a.Count("list")
		var mu sync.Mutex
		var listedB []string
		m.b.SetFault(func(op, k string) error {
			if op == "list" && strings.HasPrefix(k, "quader/sha256/") {
				mu.Lock()
				listedB = append(listedB, k)
				mu.Unlock()
			}
			return nil
		})
		second := &sweepRun{}
		if err := m.sweeper(m.pastMinAge()).RunSweepJob(ctx, json.RawMessage(`{}`), second); err != nil {
			t.Fatal(err)
		}
		if got := m.a.Count("list"); got != listedA {
			t.Fatalf("the resumed run listed the first connection %d times", got-listedA)
		}
		if len(listedB) == 0 || listedB[0] != "quader/sha256/7f/" {
			t.Fatalf("the resumed run listed the second connection from %v", listedB[:min(3, len(listedB))])
		}
		if r := second.result.ResumedFrom; r == nil || *r != last {
			t.Fatalf("resumedFrom %+v", r)
		}
		if res := connResult(t, second.result, "secondary"); res.OrphansDeleted != 1 || res.PrefixesDone != 256 {
			t.Fatalf("the resumed run %+v", res)
		}
	})

	t.Run("R-STOR-026 with the first connection's circuit open the sweep walks the second and names the first skipped", func(t *testing.T) {
		m := newMemConns(t, p, 1)
		trip(t, m.a, m.ga)
		res, err := m.sweeper(0).Sweep(ctx, blob.SweepOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Skipped) != 1 || res.Skipped[0] != (blob.SweepSkip{Connection: "primary", Reason: "circuit_open"}) {
			t.Fatalf("skipped %+v", res.Skipped)
		}
		if r := connResult(t, res, "secondary"); r.PrefixesDone != 256 {
			t.Fatalf("the second connection %+v", r)
		}
	})

	t.Run("a connection whose marker names another installation is left alone", func(t *testing.T) {
		m := newMemConns(t, p, 5)
		orphan := orphanAt(0x42, 50)
		m.b.Set(key(orphan), memData(16, 50), old)
		mine, theirs := uuid.NewString(), uuid.NewString()
		marker, _ := json.Marshal(blob.Marker{InstallationID: theirs, ConnectionID: "secondary"})
		m.b.Set(blob.MarkerKey("quader"), marker, old)
		own, _ := json.Marshal(blob.Marker{InstallationID: mine, ConnectionID: "primary"})
		m.a.Set(blob.MarkerKey("quader"), own, old)
		res, err := m.collector(blob.WithInstallation(mine), blob.WithCollectorClock(func() time.Time { return time.Now().Add(m.pastMinAge()) })).
			Sweep(ctx, blob.SweepOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Skipped) != 1 || res.Skipped[0] != (blob.SweepSkip{Connection: "secondary", Reason: "foreign_marker"}) {
			t.Fatalf("skipped %+v", res.Skipped)
		}
		if _, ok := m.b.Data(key(orphan)); !ok {
			t.Fatal("the sweep deleted in another installation's location")
		}
		if r := connResult(t, res, "primary"); r.PrefixesDone != 256 || r.ForeignKeys != 0 {
			t.Fatalf("the own connection %+v", r)
		}
	})

	t.Run("under the deletion pause the sweep adopts and deletes nothing, ends paused and saves no checkpoint past the pause", func(t *testing.T) {
		m := newMemConns(t, p, 5)
		data := memData(1024, 40)
		d := commit(t, m.store, data)
		m.b.Set(key(d), data, old)
		orphan := orphanAt(0x41, 40)
		m.b.Set(key(orphan), memData(16, 40), old)
		p.exec(t, `INSERT INTO system_state (key, value) VALUES ('deletions_paused_at', to_jsonb(now()::text))
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`)
		defer p.exec(t, `DELETE FROM system_state WHERE key = 'deletions_paused_at'`)
		var saved []blob.SweepCursor
		res, err := m.sweeper(m.pastMinAge()).Sweep(ctx, blob.SweepOptions{Checkpoint: func(c blob.SweepCursor) { saved = append(saved, c) }})
		if err != nil {
			t.Fatal(err)
		}
		r := connResult(t, res, "secondary")
		if !res.Paused || r.Adopted != 1 || r.OrphansFound != 1 || r.OrphansDeleted != 0 || len(saved) != 0 {
			t.Fatalf("paused %v, %+v, checkpoints %v", res.Paused, r, saved)
		}
		if _, ok := m.b.Data(key(orphan)); !ok {
			t.Fatal("the sweep deleted under the deletion pause")
		}
	})

	t.Run("the staging step aborts old multipart uploads and removes what no live session of the connection stands for", func(t *testing.T) {
		m := newMemConns(t, p, 5)
		maxAge := m.cfg.Uploads.SessionMaxAge
		stale := time.Now().Add(-maxAge - time.Hour)
		m.b.Now = func() time.Time { return time.Now().Add(-m.cfg.Orphans.MultipartMaxAge - time.Hour) }
		if _, err := m.b.CreateMultipart(ctx, "quader/sha256/ab/cd/abcd-crashed-write", "", nil); err != nil {
			t.Fatal(err)
		}
		m.b.Now = time.Now
		if _, err := m.b.CreateMultipart(ctx, "quader/sha256/ab/cd/abcd-running-write", "", nil); err != nil {
			t.Fatal(err)
		}
		unknown := uuid.NewString()
		m.b.Set(blob.SessionKey("quader", unknown, "tail-x"), []byte("x"), stale)
		youngUnknown := uuid.NewString()
		m.b.Set(blob.SessionKey("quader", youngUnknown, "tail-x"), []byte("x"), time.Now())
		elsewhere := p.session(t, "primary", "open", time.Minute) // pinned to the primary: its objects on the secondary are leftovers
		m.b.Set(blob.SessionKey("quader", elsewhere, "tail-y"), []byte("y"), stale)
		live := p.session(t, "secondary", "open", time.Minute)
		m.b.Set(blob.SessionKey("quader", live, "tail-z"), []byte("z"), stale)
		m.b.Set("quader/_uploads/not-a-session/keep", []byte("k"), stale)
		m.b.Set("quader/_probe/replica.startup.0102", []byte("p"), stale)

		res, err := m.sweeper(0).Sweep(ctx, blob.SweepOptions{Connection: "secondary"})
		if err != nil {
			t.Fatal(err)
		}
		r := connResult(t, res, "secondary")
		if r.MultipartAborted != 1 || r.StagingDeleted != 3 || r.ForeignKeys != 1 {
			t.Fatalf("staging step %+v", r)
		}
		if m.b.OpenUploads() != 1 {
			t.Fatalf("%d multipart uploads left, want the running one", m.b.OpenUploads())
		}
		for _, k := range []string{blob.SessionKey("quader", unknown, "tail-x"), blob.SessionKey("quader", elsewhere, "tail-y"), "quader/_probe/replica.startup.0102"} {
			if _, ok := m.b.Data(k); ok {
				t.Errorf("%s remains", k)
			}
		}
		for _, k := range []string{blob.SessionKey("quader", youngUnknown, "tail-x"), blob.SessionKey("quader", live, "tail-z"), "quader/_uploads/not-a-session/keep"} {
			if _, ok := m.b.Data(k); !ok {
				t.Errorf("%s was deleted", k)
			}
		}
	})
}
