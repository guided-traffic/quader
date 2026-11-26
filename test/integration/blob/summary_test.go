//go:build integration

package blob

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob/summary"
)

// The task storage-summary and the connections' list against PostgreSQL of the stack (storage.md R-STOR-028, R-STOR-061,
// operations-and-observability.md R-OPS-023): rows of blobs, copies, items, the queue and the registry written by SQL, no object at all.
// Six blobs of 50 to 500 bytes, three connections.

func TestR_STOR_028_TheSummaryAndTheConnectionsAgainstTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := newPG(t)

	if _, err := summary.Latest(ctx, p.db); !errors.Is(err, summary.ErrNone) {
		t.Fatalf("before the first run: %v", err)
	}

	p.exec(t, `INSERT INTO quader.storage_connections (id, bucket, prefix, filled_at) VALUES ('default', 'quader-blobs', 'quader', now() - interval '2 days'),
		('secondary', 'quader-copy', 'quader', NULL), ('archive', 'quader-old', 'quader', now() - interval '90 days')`)
	sha := func(n int) []byte {
		b := make([]byte, 32)
		b[31] = byte(n)
		return b
	}
	for n, size := range []int{100, 200, 300, 400, 500} {
		p.exec(t, `INSERT INTO quader.blobs (sha256, sha1, md5, sha512, size) VALUES ($1, $2, $3, $4, $5)`, sha(n+1), make([]byte, 20), make([]byte, 16), make([]byte, 64), size)
	}
	p.exec(t, `INSERT INTO quader.blobs (sha256, sha1, md5, sha512, size, state) VALUES ($1, $2, $3, $4, 50, 'deleting')`, sha(6), make([]byte, 20), make([]byte, 16), make([]byte, 64))
	p.exec(t, `UPDATE quader.blobs SET content_status = 'corrupt' WHERE sha256 = $1`, sha(5))
	copyRow := func(n int, conn, state, extra string) {
		p.exec(t, `INSERT INTO quader.blob_copies (sha256, connection_id, object_key, stored_size, state)
			SELECT sha256, $2, 'quader/sha256/' || encode(sha256, 'hex'), size, $3 FROM quader.blobs WHERE sha256 = $1`, sha(n), conn, state)
		if extra != "" {
			p.exec(t, `UPDATE quader.blob_copies SET `+extra+` WHERE sha256 = $1 AND connection_id = $2`, sha(n), conn)
		}
	}
	copyRow(1, "default", "present", "verified_at = now()")
	copyRow(2, "default", "present", "created_at = now() - interval '40 days'") // never read whole for longer than its period and a day
	copyRow(3, "default", "present", "")
	copyRow(4, "default", "present", "")
	copyRow(5, "default", "corrupt", "")
	copyRow(1, "secondary", "present", "verified_at = now()")
	copyRow(2, "secondary", "missing", "")
	copyRow(3, "archive", "present", "")
	p.exec(t, `INSERT INTO quader.blob_copy_queue (connection_id, sha256, enqueued_at) VALUES ('secondary', $1, now() - interval '4 minutes'), ('secondary', $2, now())`, sha(3), sha(4))
	p.exec(t, `INSERT INTO quader.blob_gc_queue (sha256, enqueued_at) VALUES ($1, now())`, sha(4))
	p.exec(t, `INSERT INTO quader.items (repo_id, path, parent_path, kind, blob_sha256, size) VALUES ($1, 'a.txt', '', 'file', $2, 100), ($1, 'b.txt', '', 'file', $3, 200)`,
		p.repo, sha(1), sha(2))
	p.exec(t, `INSERT INTO quader.items (repo_id, path, parent_path, kind, blob_sha256, size, trashed_at) VALUES ($1, 'c.txt', '', 'file', $2, 300, now())`, p.repo, sha(3))
	// two live replicas of the registry: both configure default and secondary, one has the circuit of secondary open; archive is in no
	// configuration
	p.exec(t, `INSERT INTO quader.replica (instance_uid, name, version, revision, roles, state, secret_key_ids, active_secret_key_id, started_at, heartbeat_at, object_store)
		VALUES (gen_random_uuid(), 'quader-worker-a', '0.1.0', 'r', '{worker}', 'ready', '{k1}', 'k1', now(), now(),
		        '{"connections": [{"id": "default", "circuit": "closed", "drain": false}, {"id": "secondary", "circuit": "open", "drain": false}]}'),
		       (gen_random_uuid(), 'quader-api-b', '0.1.0', 'r', '{api}', 'ready', '{k1}', 'k1', now(), now(),
		        '{"connections": [{"id": "default", "circuit": "closed", "drain": false}, {"id": "secondary", "circuit": "closed", "drain": false}]}'),
		       (gen_random_uuid(), 'quader-api-gone', '0.1.0', 'r', '{api}', 'ready', '{k1}', 'k1', now(), now() - interval '1 hour',
		        '{"connections": [{"id": "archive", "circuit": "closed", "drain": false}]}')`)

	run := &resultRun{}
	opts := summary.Options{Periods: map[string]time.Duration{"default": 720 * time.Hour, "secondary": 720 * time.Hour}, Targets: []string{"default", "secondary"}}
	if err := summary.RunJob(ctx, p.db, opts, run); err != nil {
		t.Fatal(err)
	}
	s, err := summary.Latest(ctx, p.db)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("R-STOR-028 the installation's figures count live blobs, the unfolded deltas, the trash, what awaits collection and the damaged blobs", func(t *testing.T) {
		if s.Blobs != (summary.Blobs{Count: 5, Bytes: 1500}) || s.Logical != (summary.Logical{Files: 2, Bytes: 300}) || s.Trash != (summary.Trash{Items: 1, Bytes: 300}) ||
			s.PendingCollection != (summary.Pending{Blobs: 2, Bytes: 450}) || s.Damaged != (summary.Damaged{Corrupt: 1}) || s.DeduplicationRatio != 5 {
			t.Errorf("summary = %+v", s)
		}
		if len(s.Repositories) != 1 || s.Repositories[0].Key != "generic-local" || s.Repositories[0].ShareOfLogicalBytes != 1 || s.Repositories[0].Files != 2 {
			t.Errorf("repositories = %+v", s.Repositories)
		}
		if r, ok := run.result.(summary.Result); !ok || r.Blobs != 5 || r.Connections != 3 || s.ComputedAt.IsZero() || time.Since(s.ComputedAt) > time.Minute {
			t.Errorf("result = %+v, computedAt %v", run.result, s.ComputedAt)
		}
	})

	t.Run("R-STOR-028 each connection's copies, backlog, fill, scrub and sole copies", func(t *testing.T) {
		got := map[string]summary.Connection{}
		for _, c := range s.Connections {
			got[c.ID] = c
		}
		want := map[string]summary.Connection{
			"default": {
				ID: "default", PresentCopies: 4, PresentBytes: 1000, CorruptCopies: 1, VerifiedWithinPeriod: 1, ScrubOverdue: 1, BacklogBlobs: 1,
				BacklogBytes: 500, FillRatio: 0.8, SoleCopies: 2, SoleBytes: 600,
			},
			"secondary": {
				ID: "secondary", PresentCopies: 1, PresentBytes: 100, MissingCopies: 1, VerifiedWithinPeriod: 1, BacklogBlobs: 4, BacklogBytes: 1400,
				FillRatio: 0.2,
			},
			"archive": {ID: "archive", PresentCopies: 1, PresentBytes: 300, BacklogBlobs: 4, BacklogBytes: 1200, FillRatio: 0.2},
		}
		for id, w := range want {
			if g := got[id]; !reflect.DeepEqual(g, w) {
				t.Errorf("%s = %+v, want %+v", id, got[id], w)
			}
		}
	})

	t.Run("R-STOR-106 the copies per blob on the connections that are not retired, below the two targets", func(t *testing.T) {
		want := summary.Copies{
			Target:      2,
			PerBlob:     []summary.CopyCount{{Copies: 2, Blobs: 2, Bytes: 400}, {Copies: 1, Blobs: 2, Bytes: 600}, {Copies: 0, Blobs: 1, Bytes: 500}},
			BelowTarget: summary.Pending{Blobs: 4, Bytes: 1400},
		}
		if !reflect.DeepEqual(s.Copies, want) {
			t.Errorf("copies = %+v, want %+v", s.Copies, want)
		}
	})

	t.Run("R-STOR-061 the list joins the states from the registry, the queue and the summary's figures", func(t *testing.T) {
		views, err := summary.List(ctx, p.db, 5*time.Minute, map[string]summary.Local{"secondary": {Position: 1}, "default": {Position: 2}})
		if err != nil {
			t.Fatal(err)
		}
		if len(views) != 3 || views[0].ID != "secondary" || views[1].ID != "default" || views[2].ID != "archive" {
			t.Fatalf("views = %+v", views)
		}
		sec, def, arc := views[0], views[1], views[2]
		if sec.State != summary.StateFilling || sec.Configured != 2 || sec.LiveReplicas != 2 || sec.CircuitOpen != 1 || sec.Queued != 2 || sec.OldestQueuedAt == nil ||
			time.Since(*sec.OldestQueuedAt) < 3*time.Minute || sec.Figures == nil || sec.Figures.FillRatio != 0.2 || !sec.ComputedAt.Equal(s.ComputedAt) {
			t.Errorf("secondary = %+v", sec)
		}
		if def.State != summary.StateActive || def.CircuitOpen != 0 || def.Queued != 0 || def.OldestQueuedAt != nil || def.FilledAt == nil {
			t.Errorf("default = %+v", def)
		}
		if arc.State != summary.StateDetached || arc.Configured != 0 || !arc.HoldsCopies || arc.Local != nil {
			t.Errorf("archive = %+v", arc)
		}
		if def.FirstSeenAt.IsZero() || time.Since(def.FirstSeenAt) > time.Hour || def.PassStartedAt != nil {
			t.Errorf("default's life = %v %v", def.FirstSeenAt, def.PassStartedAt)
		}
	})
}

// TestR_STOR_106_TheCopiesInFigures: the copies of the installation against PostgreSQL of the stack (storage.md R-STOR-106, R-STOR-107):
// two connections, default plain and archive encrypted with a retired and an active storage key; one blob on both, one on default only,
// one with no present copy. Rows of 1 KiB blobs, no object.
func TestR_STOR_106_TheCopiesInFigures(t *testing.T) {
	ctx := context.Background()
	p := newPG(t)
	p.exec(t, `INSERT INTO quader.storage_connections (id, bucket, prefix, encrypted, filled_at, pass_started_at)
		VALUES ('default', 'quader-blobs', 'quader', false, now() - interval '2 days', NULL),
		       ('archive', 'quader-archive', 'quader', true, now() - interval '1 day', now() - interval '5 minutes')`)
	p.exec(t, `INSERT INTO quader.storage_keys (key_id, connection_id, kind, generation, sealed_key, state, created_at, retired_at)
		VALUES ('sk-archive-1-0a1b2c3d', 'archive', 'storage', 1, 'v1:sealed', 'retired', now() - interval '3 days', now() - interval '1 day'),
		       ('sk-archive-2-4e5f6a7b', 'archive', 'storage', 2, 'v1:sealed', 'active', now() - interval '1 day', NULL)`)
	sha := func(n int) []byte {
		b := make([]byte, 32)
		b[0] = byte(n)
		return b
	}
	for n := 1; n <= 3; n++ {
		p.exec(t, `INSERT INTO quader.blobs (sha256, sha1, md5, sha512, size) VALUES ($1, $2, $3, $4, 1024)`, sha(n), make([]byte, 20), make([]byte, 16), make([]byte, 64))
	}
	copyRow := func(n int, conn, state, key string) {
		p.exec(t, `INSERT INTO quader.blob_copies (sha256, connection_id, object_key, stored_size, state, storage_key_id)
			VALUES ($1, $2, 'k/' || encode($1, 'hex'), 1024, $3, NULLIF($4, ''))`, sha(n), conn, state, key)
	}
	copyRow(1, "default", "present", "")
	copyRow(1, "archive", "present", "sk-archive-2-4e5f6a7b")
	copyRow(2, "default", "present", "")
	copyRow(3, "default", "missing", "")

	opts := summary.Options{Periods: map[string]time.Duration{"default": 720 * time.Hour, "archive": 720 * time.Hour}, Targets: []string{"archive", "default"}}
	if err := summary.RunJob(ctx, p.db, opts, &resultRun{}); err != nil {
		t.Fatal(err)
	}
	views, latest, err := summary.ListWithLatest(ctx, p.db, 5*time.Minute, nil)
	if err != nil || latest == nil {
		t.Fatalf("list: %v, latest %v", err, latest)
	}

	t.Run("R-STOR-106 the copies per blob, the blobs below the target, the sole bytes and the copies in the format", func(t *testing.T) {
		want := summary.Copies{
			Target:      2,
			PerBlob:     []summary.CopyCount{{Copies: 2, Blobs: 1, Bytes: 1024}, {Copies: 1, Blobs: 1, Bytes: 1024}, {Copies: 0, Blobs: 1, Bytes: 1024}},
			BelowTarget: summary.Pending{Blobs: 2, Bytes: 2048},
		}
		if !reflect.DeepEqual(latest.Copies, want) {
			t.Errorf("copies = %+v, want %+v", latest.Copies, want)
		}
		got := map[string]summary.Connection{}
		for _, c := range latest.Connections {
			got[c.ID] = c
		}
		if d := got["default"]; d.SoleCopies != 1 || d.SoleBytes != 1024 || d.KeyedCopies != 0 || d.MissingCopies != 1 {
			t.Errorf("default = %+v", d)
		}
		if a := got["archive"]; a.SoleCopies != 0 || a.KeyedCopies != 1 || a.PresentCopies != 1 {
			t.Errorf("archive = %+v", a)
		}
	})

	t.Run("R-STOR-061 R-STOR-106 the list carries encrypted since, the current pass and a key's retirement", func(t *testing.T) {
		var arc, def summary.View
		for _, v := range views {
			switch v.ID {
			case "archive":
				arc = v
			case "default":
				def = v
			}
		}
		if since := arc.EncryptedSince(); since == nil || time.Since(*since) < 71*time.Hour || def.EncryptedSince() != nil {
			t.Errorf("encrypted since = %v, default %v", arc.EncryptedSince(), def.EncryptedSince())
		}
		if arc.PassStartedAt == nil || def.PassStartedAt != nil || len(arc.Keys) != 2 || arc.Keys[0].RetiredAt == nil || arc.Keys[1].RetiredAt != nil {
			t.Errorf("archive = %+v", arc)
		}
	})

	t.Run("R-STOR-107 the folded totals count the live blobs now", func(t *testing.T) {
		stored, err := summary.Stored(ctx, p.db)
		if err != nil || stored != (summary.Blobs{Count: 3, Bytes: 3072}) {
			t.Errorf("stored = %+v, %v", stored, err)
		}
	})
}

// resultRun keeps the result a run stores.
type resultRun struct{ result any }

func (r *resultRun) SetResult(v any) error { r.result = v; return nil }
