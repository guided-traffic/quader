//go:build integration

package blob

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/listq"
	"github.com/guided-traffic/quader/internal/secrets"
)

// destroyedKeysInPostgreSQL runs what only PostgreSQL shows of the destruction of a retired connection's keys (R-STOR-097 to
// R-STOR-103), in the database of the keys' case: the CHECKs of a destroyed row, two refusals that write nothing, a destruction with its
// audit event, its outbox row and its change, the walk and the sealed count after it, the start's refusal of the id, and the
// connections' list. Eight rows of storage_keys at most; no object of a blob.
func destroyedKeysInPostgreSQL(t *testing.T, p *pgEnv, ring *secrets.KeyRing) {
	ctx := context.Background()
	t.Run("R-STOR-097 a destroyed row with a sealed value, one without destroyed_at, and an active row with NULL are each refused by a CHECK", func(t *testing.T) {
		now := time.Now()
		insert := func(state string, sealed, retired, destroyed any) error {
			_, err := p.admin.Exec(ctx, `INSERT INTO storage_keys (key_id, connection_id, kind, generation, sealed_key, state, retired_at, destroyed_at)
				VALUES ('sk-checks-1-0000aaaa', 'checks', 'storage', 1, $1, $2, $3, $4)`, sealed, state, retired, destroyed)
			return err
		}
		for _, c := range []struct {
			name, state                string
			sealed, retired, destroyed any
		}{
			{"a destroyed row with a sealed value", "destroyed", "v1:k1:AAAA", now, now},
			{"a destroyed row without destroyed_at", "destroyed", nil, now, nil},
			{"an active row with NULL", "active", nil, nil, nil},
			{"a destroyed row that is not retired", "destroyed", nil, nil, now},
		} {
			if got := sqlStateOf(insert(c.state, c.sealed, c.retired, c.destroyed)); got != "23514" {
				t.Errorf("%s: SQLSTATE %q, want 23514", c.name, got)
			}
		}
		if err := insert("destroyed", nil, now, now); err != nil {
			t.Fatalf("a destroyed row as R-STOR-097 says: %v", err)
		}
		p.exec(t, `DELETE FROM storage_keys WHERE connection_id = 'checks'`)
	})

	h := newHolder(t, p, ring)
	if err := h.Ensure(ctx, storagekeys.Connection{ID: "gone", KeyedNames: true, Prefix: "gone", Objects: blobtest.New()}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	rows, err := p.admin.Query(ctx, `SELECT key_id FROM storage_keys WHERE connection_id = 'gone' ORDER BY key_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	p.exec(t, `INSERT INTO storage_connections (id, bucket, prefix, encrypted, keyed_names, drained_at, retired_at)
		VALUES ('gone', 'gone', 'gone', true, true, now() - interval '1 day', now())`)
	if _, err := storagekeys.Export(ctx, p.db, nil, "0b7c3f5e-1111-4000-8000-000000000001", "it-blob-pod", "gone", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var sk string
	for _, id := range ids {
		if strings.HasPrefix(id, "sk-") {
			sk = id
		}
	}
	listing := func(pool string) {
		p.exec(t, `DELETE FROM backup_run`)
		p.exec(t, `INSERT INTO backup_run (id, operation, backup_id, state, trigger, target, pod, quader_version, lease_expires_at, finished_at, result)
			VALUES (gen_random_uuid(), 'create', 'b1', 'completed', 'manual', 's3://bk/quader (aws)', 'p', '0.1.0', now(), now(),
			        jsonb_build_object('retention', jsonb_build_object('keepLast', 1, 'keepWithinSeconds', 0),
			                           'retained', jsonb_build_array(jsonb_build_object('backupId', 'b1', 'snapshotAt', '2027-03-01T02:41:17Z',
			                                                                            'poolKeyIds', $1::jsonb, 'sealedKeyIds', $2::jsonb))))`,
			pool, `["`+sk+`"]`)
	}
	opts := storagekeys.DestroyOptions{
		Connection: "gone", Installation: "0b7c3f5e-1111-4000-8000-000000000001", StaleAfter: time.Minute,
		Backup: storagekeys.BackupTarget{Bucket: "bk", Prefix: "quader"},
	}
	untouched := func(t *testing.T) {
		t.Helper()
		if n := p.count(t, "storage_keys WHERE connection_id = 'gone' AND state = 'destroyed'"); n != 0 {
			t.Fatalf("%d rows destroyed by a refused destruction", n)
		}
	}

	t.Run("R-STOR-098 R-STOR-100 a copy row and a retained backup whose pool names a key each refuse, and nothing is written", func(t *testing.T) {
		listing(`[]`)
		p.exec(t, `INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES (sha256('x'::bytea), 'gone', 'gone/keyed/x', 1120, 'present')`)
		d, err := storagekeys.Destroy(ctx, p.db, nil, opts, nil)
		if !errors.Is(err, storagekeys.ErrHeld) || len(d.Refusals) != 1 || d.Refusals[0].Holder != storagekeys.HolderCopies {
			t.Fatalf("a copy row: %v %+v", err, d.Refusals)
		}
		untouched(t)
		p.exec(t, `DELETE FROM blob_copies WHERE connection_id = 'gone'`)
		listing(`["` + sk + `"]`)
		d, err = storagekeys.Destroy(ctx, p.db, nil, opts, nil)
		if !errors.Is(err, storagekeys.ErrHeld) || len(d.Refusals) != 1 || !strings.Contains(d.Refusals[0].Say, "backup b1 (snapshot 2027-03-01T02:41:17Z) holds pool objects under "+sk) {
			t.Fatalf("a retained backup: %v %+v", err, d.Refusals)
		}
		untouched(t)
	})

	t.Run("R-STOR-099 a destruction in PostgreSQL with its audit event, its outbox row and its change", func(t *testing.T) {
		listing(`[]`)
		rec, err := audit.New(audit.Options{DB: p.db, Replica: "it-blob-destroy", Config: config.Audit{AsyncBuffer: 1000}, Log: quiet, AppLog: quiet})
		if err != nil {
			t.Fatal(err)
		}
		clu := config.Cluster{
			LeaseRetryInterval: 500 * time.Millisecond, EpochPollInterval: time.Second, EpochMaxStaleness: 20 * time.Second,
			ChangeRetention: time.Hour, SignalCoalesceWindow: 20 * time.Millisecond,
		}
		changes, err := cluster.NewChanges(cluster.ChangesOptions{DB: p.db.ControlPool(), Config: clu, WatchdogTimeout: 30 * time.Second, Log: quiet})
		if err != nil {
			t.Fatal(err)
		}
		// The traces of data keys handed to a Bastion as the handover writes them (R-STOR-096, the connection and the storage key in
		// their details): the report counts the files and the handovers of its connection, not another's (R-STOR-098's holder, Q-STOR-475).
		// File ab is handed twice (by two replicas of the Bastion), file cd once, and file ab once more on connection other: two files and
		// three handovers.
		for _, h := range []struct{ replica, sha, conn, key string }{
			{"bastion-1", "ab", "gone", sk},
			{"bastion-2", "ab", "gone", sk},
			{"bastion-1", "cd", "gone", sk},
			{"bastion-1", "ab", "other", "sk-other-1-0000bbbb"},
		} {
			handed, _ := json.Marshal(map[string]string{
				"bastion": "partners", "replica": h.replica, "sha256": strings.Repeat(h.sha, 32), "connection": h.conn, "storageKeyId": h.key,
				"linkExpiresAt": time.Now().UTC().Format(time.RFC3339),
			})
			if err := p.db.InTx(ctx, func(tx meta.Tx) error {
				return rec.RecordTx(ctx, tx, audit.Event{
					Type: audit.EventBastionDataKeyHanded, Outcome: audit.Success, Actor: audit.Actor{Kind: "anonymous"},
					Subject: audit.Subject{Kind: "bastion", Name: "partners"}, Repository: "it/generic", Project: "it", Path: "dist/a.bin", Details: handed,
				})
			}); err != nil {
				t.Fatal(err)
			}
		}
		dry, err := storagekeys.Destroy(ctx, p.db, nil, opts, nil)
		if err != nil || dry.Confirm != storagekeys.Confirmation(opts.Installation, "gone", ids) {
			t.Fatalf("dry run: %v %+v", err, dry)
		}
		if got := dry.Remaining.Handed; got != (storagekeys.HandedHeld{Files: 2, Handovers: 3}) {
			t.Errorf("the dry run counts %+v, want the 2 files and 3 handovers of connection gone", got)
		}
		o := opts
		o.Confirm = dry.Confirm
		d, err := storagekeys.Destroy(ctx, p.db, nil, o, func(ctx context.Context, tx meta.Tx, d storagekeys.Destruction) error {
			if err := changes.Notify(tx, cluster.AreaStorageKeys, storagekeys.DestroyedChangeVersion, storagekeys.DestroyedChange(d.Connection, d.Destroyed)); err != nil {
				return err
			}
			details, _ := json.Marshal(map[string]any{
				"connection": d.Connection, "keyIds": d.Destroyed, "exports": d.Remaining.Exports,
				"backupListing":         map[string]any{"runId": d.Remaining.Backups.RunID, "listedAt": d.Remaining.Backups.ListedAt, "target": d.Remaining.Backups.Target},
				"retainedHoldingSealed": d.Remaining.Backups.Count,
			})
			entry, _ := json.Marshal(storagekeys.DestroyedEntry{Connection: d.Connection, KeyIDs: d.Destroyed, DestroyedAt: *d.DestroyedAt})
			return rec.RecordTx(ctx, tx, audit.Event{
				Type: audit.EventStorageKeysDestroyed, Outcome: audit.Success,
				Actor: audit.Actor{Kind: "system", Name: "admin"}, Details: details, Journal: entry,
			})
		})
		if err != nil || len(d.Destroyed) != len(ids) || d.Remaining.Exports.Count != 1 || d.Remaining.Backups.Count == nil || *d.Remaining.Backups.Count != 1 {
			t.Fatalf("%v %+v", err, d)
		}
		if n := p.count(t, "storage_keys WHERE connection_id = 'gone' AND state = 'destroyed' AND sealed_key IS NULL AND destroyed_at IS NOT NULL AND retired_at IS NOT NULL"); n != int64(len(ids)) {
			t.Errorf("%d rows destroyed, want %d", n, len(ids))
		}
		if n := p.count(t, "audit_event WHERE action = 'system.storage.keys_destroyed'"); n != 1 {
			t.Errorf("%d audit events", n)
		}
		if n := p.count(t, `revocation_outbox WHERE event = 'system.storage.keys_destroyed' AND entry ->> 'connection' = 'gone'`); n != 1 {
			t.Errorf("%d outbox rows", n)
		}
		if n := p.count(t, `cluster_change WHERE area = 'storage_keys' AND payload::text LIKE '%"destroyed"%' AND payload::text LIKE '%"gone"%'`); n != 1 {
			t.Errorf("%d changes of the area name the destruction", n)
		}
		if again, err := storagekeys.Destroy(ctx, p.db, nil, o, nil); err != nil || again.State != storagekeys.StateDestroyed {
			t.Errorf("a second run: %v %+v", err, again)
		}
	})

	t.Run("R-STOR-097 the walk of secrets-reencrypt skips the destroyed rows with failed 0, and CountSealed does not count them", func(t *testing.T) {
		rotated := rootRing(t, "k2", map[string]byte{"k1": 1, "k2": 2})
		res, err := storagekeys.Reseal(ctx, p.db, nil, rotated, 0)
		if err != nil || res.Failed != 0 {
			t.Fatalf("walk %+v %v", res, err)
		}
		counts, err := listq.SealedCounter{DB: p.db.Main()}.CountSealed(ctx, storagekeys.Column())
		if err != nil {
			t.Fatal(err)
		}
		var sealed int64
		for _, n := range counts {
			sealed += n
		}
		if want := p.count(t, "storage_keys WHERE state <> 'destroyed'"); sealed != want {
			t.Errorf("CountSealed counts %d values, want the %d rows that are not destroyed", sealed, want)
		}
	})

	t.Run("R-STOR-102 a start whose configuration names the id is refused, and so is Ensure", func(t *testing.T) {
		cfg := blobtest.Storage(t, t.TempDir())
		prefix := "gone"
		cfg.Connections = []config.StorageConnection{config.StorageConnection{ID: "gone", Bucket: "gone", Prefix: &prefix}.WithDefaults()}
		conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], blobtest.New()))
		if err != nil {
			t.Fatal(err)
		}
		s, err := blob.New(cfg, conns, p.db, blob.WithLogger(quiet))
		if err != nil {
			t.Fatal(err)
		}
		day := time.Now().UTC().Format(time.DateOnly)
		want := "storage.connections[0] (gone): the storage keys of connection gone were destroyed on " + day + "; the id is retired for good; choose another id (storage.md R-STOR-102)"
		var r *blob.Refusal
		if err := s.RecordConnections(ctx); !errors.As(err, &r) || err.Error() != want {
			t.Fatalf("the start: %v, want %q", err, want)
		}
		if err := newHolder(t, p, ring).Ensure(ctx, storagekeys.Connection{ID: "gone", Prefix: "gone", Objects: blobtest.New()}); !storagekeys.IsRefusal(err) {
			t.Fatalf("Ensure: %v", err)
		}
	})

	t.Run("R-STOR-088 R-STOR-097 the connections' list shows the destroyed keys with the time of their destruction", func(t *testing.T) {
		views, err := summary.List(ctx, p.db, 5*time.Minute, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range views {
			if v.ID != "gone" {
				continue
			}
			if len(v.Keys) != 1 || v.Keys[0].State != storagekeys.StateDestroyed || v.Keys[0].DestroyedAt == nil {
				t.Fatalf("the storage keys of gone: %+v", v.Keys)
			}
			return
		}
		t.Fatal("gone is not in the list")
	})
}
