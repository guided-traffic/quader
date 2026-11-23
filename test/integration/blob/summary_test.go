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
	if err := summary.RunJob(ctx, p.db, summary.Options{Periods: map[string]time.Duration{"default": 720 * time.Hour, "secondary": 720 * time.Hour}}, run); err != nil {
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
				BacklogBytes: 500, FillRatio: 0.8, SoleCopies: 2,
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
	})
}

// resultRun keeps the result a run stores.
type resultRun struct{ result any }

func (r *resultRun) SetResult(v any) error { r.result = v; return nil }
