package keys_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta"
)

const destroyInstallation = "0b7c3f5e-1111-4000-8000-000000000001"

// destroyEnv is the database of the tests of the destruction: connection archive with keyed names and two generations, a holder that
// holds its keys, and the database's answer for the holders (keystest.DB.Held) of a retired connection without any.
type destroyEnv struct {
	*env
	h        *keys.Holder
	ids      []string
	retired  time.Time
	announce []keys.Destruction
}

func newDestroyEnv(t *testing.T) *destroyEnv {
	t.Helper()
	e := &destroyEnv{env: newEnv(t), retired: time.Date(2027, 3, 2, 8, 0, 0, 0, time.UTC)}
	e.h = e.holder(t)
	if err := e.h.Ensure(ctx, e.archive()); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.NewGeneration(ctx, e.db, e.db, e.ring, nil, "archive", nil); err != nil {
		t.Fatal(err)
	}
	e.h.FlushArea(cluster.AreaStorageKeys, "test")
	if _, err := e.h.Active(ctx, "archive"); err != nil {
		t.Fatal(err)
	}
	for _, r := range e.db.Rows() {
		e.ids = append(e.ids, r.KeyID)
	}
	e.db.Held = keys.Holders{Recorded: true, DrainedAt: e.retired.Add(-24 * time.Hour), RetiredAt: e.retired}
	return e
}

// destroy runs the destruction of archive with confirm and the options' changes.
func (e *destroyEnv) destroy(confirm string, change func(*keys.DestroyOptions)) (keys.Destruction, error) {
	o := keys.DestroyOptions{Connection: "archive", Installation: destroyInstallation, Confirm: confirm, StaleAfter: 30 * time.Second}
	if change != nil {
		change(&o)
	}
	return keys.Destroy(ctx, e.db, e.db, o, func(_ context.Context, _ meta.Tx, d keys.Destruction) error {
		e.announce = append(e.announce, d)
		return nil
	})
}

func (e *destroyEnv) confirmation() string {
	return keys.Confirmation(destroyInstallation, "archive", e.ids)
}

func refusedBy(d keys.Destruction, holder, says string) bool {
	return slices.ContainsFunc(d.Refusals, func(r keys.HolderRefusal) bool { return r.Holder == holder && strings.Contains(r.Say, says) })
}

func TestR_STOR_098_EachHolderRefusesAndNothingIsWritten(t *testing.T) {
	for _, c := range []struct {
		name, holder, says string
		held               keys.Holders
	}{
		{
			"a live replica that configures the connection", keys.HolderReplicas, "1 live replicas have connection archive in their storage.connections",
			keys.Holders{Recorded: true, LiveReplicas: 1},
		},
		{
			"the connection draining on a live replica", keys.HolderState, "connection archive is in use (filling, active or draining)",
			keys.Holders{Recorded: true, LiveReplicas: 1},
		},
		{"the connection detached", keys.HolderState, "connection archive is detached", keys.Holders{Recorded: true}},
		{
			"one copy row", keys.HolderCopies, "1 copy rows lie on connection archive or under one of its keys",
			keys.Holders{Recorded: true, RetiredAt: time.Now(), Copies: 1},
		},
		{
			"one queue row", keys.HolderQueue, "1 queue rows of the reconciliation name connection archive",
			keys.Holders{Recorded: true, RetiredAt: time.Now(), Queued: 1},
		},
		{"one upload session", keys.HolderSessions, "storage.uploads.session_max_age", keys.Holders{Recorded: true, RetiredAt: time.Now(), Sessions: 1}},
	} {
		t.Run("R-STOR-098 "+c.name+" refuses the dry run and the destruction with its sentence", func(t *testing.T) {
			e := newDestroyEnv(t)
			e.db.Held = c.held
			before, writes := e.db.Rows(), e.db.Writes
			for _, confirm := range []string{"", e.confirmation()} {
				d, err := e.destroy(confirm, nil)
				if !errors.Is(err, keys.ErrHeld) || !refusedBy(d, c.holder, c.says) || d.Confirm != "" {
					t.Fatalf("confirm %q: %v, refusals %+v", confirm, err, d.Refusals)
				}
			}
			if e.db.Writes != writes || !slices.Equal(e.db.Rows(), before) || len(e.announce) != 0 {
				t.Fatal("a refused destruction wrote or announced something")
			}
		})
	}
}

func TestR_STOR_099_TheDestructionOfARetiredConnection(t *testing.T) {
	t.Run("R-STOR-099 a dry run of a retired connection reports, prints the confirmation and writes nothing", func(t *testing.T) {
		e := newDestroyEnv(t)
		writes := e.db.Writes
		d, err := e.destroy("", nil)
		if err != nil || d.Confirm != e.confirmation() || !d.DryRun || d.State != keys.StateRetired || len(d.Keys) != 3 || len(d.Refusals) != 0 {
			t.Fatalf("%v %+v", err, d)
		}
		if !strings.HasPrefix(d.Confirm, "archive-") || len(d.Confirm) != len("archive-")+8 {
			t.Errorf("confirmation %q is not <connection>-<8 hex>", d.Confirm)
		}
		if e.db.Writes != writes || len(e.announce) != 0 {
			t.Fatal("a dry run wrote")
		}
		if d, err := e.destroy(e.confirmation(), func(o *keys.DestroyOptions) { o.DryRun = true }); err != nil || d.Confirm == "" || e.db.Writes != writes {
			t.Fatalf("--dry-run with the confirmation: %v, %d writes", err, e.db.Writes-writes)
		}
	})
	t.Run("R-STOR-099 the confirmation destroys every generation and the name key in one transaction with its hook; a second run has nothing to do", func(t *testing.T) {
		e := newDestroyEnv(t)
		d, err := e.destroy(e.confirmation(), nil)
		if err != nil || len(d.Destroyed) != 3 || d.DryRun || len(e.announce) != 1 || !slices.Equal(e.announce[0].Destroyed, d.Destroyed) {
			t.Fatalf("%v %+v, announced %d", err, d, len(e.announce))
		}
		for _, r := range e.db.Rows() {
			if r.State != keys.StateDestroyed || r.Sealed != "" || r.DestroyedAt.IsZero() || r.RetiredAt.IsZero() {
				t.Errorf("row %+v", r)
			}
		}
		again, err := e.destroy(e.confirmation(), nil)
		if err != nil || again.State != keys.StateDestroyed || again.DestroyedAt == nil || len(again.Destroyed) != 0 || len(e.announce) != 1 {
			t.Fatalf("second run: %v %+v", err, again)
		}
	})
	t.Run("R-STOR-099 an unrecorded connection whose keys exist passes", func(t *testing.T) {
		e := newDestroyEnv(t)
		e.db.Held = keys.Holders{}
		if d, err := e.destroy("", nil); err != nil || d.State != "unrecorded" || d.Confirm == "" {
			t.Fatalf("%v %+v", err, d)
		}
	})
	t.Run("R-STOR-099 a confirmation of another installation, or taken before a generation was added, does not match", func(t *testing.T) {
		e := newDestroyEnv(t)
		other := keys.Confirmation("0b7c3f5e-2222-4000-8000-000000000002", "archive", e.ids)
		before := e.confirmation()
		if _, err := keys.NewGeneration(ctx, e.db, e.db, e.ring, nil, "archive", nil); err != nil {
			t.Fatal(err)
		}
		writes := e.db.Writes
		for _, token := range []string{other, before} {
			if _, err := e.destroy(token, nil); !errors.Is(err, keys.ErrConfirmMismatch) || !strings.Contains(err.Error(), "the dry run prints the current confirmation") {
				t.Errorf("%s: %v", token, err)
			}
		}
		if e.db.Writes != writes {
			t.Fatal("a mismatch wrote")
		}
	})
	t.Run("R-STOR-099 a connection of the command's own configuration, and one without rows, are wrong calls", func(t *testing.T) {
		e := newDestroyEnv(t)
		if _, err := e.destroy("", func(o *keys.DestroyOptions) { o.Configured = []string{"default", "archive"} }); !errors.Is(err, keys.ErrConfigured) {
			t.Errorf("configured: %v", err)
		}
		if _, err := e.destroy("", func(o *keys.DestroyOptions) { o.Connection = "nothing" }); !errors.Is(err, keys.ErrNoKeys) {
			t.Errorf("no rows: %v", err)
		}
	})
}

// listingOf is a backup_run result with a retention listing of the retained entries.
func listingOf(t *testing.T, target string, retained ...map[string]any) *keys.Listing {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"retention": map[string]any{"keepLast": 2, "keepWithinSeconds": 86400 * 30}, "retained": retained})
	if err != nil {
		t.Fatal(err)
	}
	return &keys.Listing{RunID: "0193aaaa-0000-7000-8000-000000000001", Target: target, FinishedAt: time.Date(2027, 3, 10, 2, 41, 17, 0, time.UTC), Result: raw}
}

func TestR_STOR_100_TheBackupsDecideAsWell(t *testing.T) {
	const target = "s3://quader-backups/quader (aws)"
	cfg := keys.BackupTarget{Bucket: "quader-backups", Prefix: "quader"}
	snap := time.Date(2027, 3, 1, 2, 41, 17, 0, time.UTC)
	backupOf := func(id string, pool, sealed any) map[string]any {
		return map[string]any{"backupId": id, "snapshotAt": snap, "poolKeyIds": pool, "sealedKeyIds": sealed}
	}
	run := func(t *testing.T, held func(e *destroyEnv) keys.Holders, backup keys.BackupTarget) (keys.Destruction, error) {
		t.Helper()
		e := newDestroyEnv(t)
		e.db.Held = held(e)
		return e.destroy("", func(o *keys.DestroyOptions) {
			o.Backup = backup
			o.Now = func() time.Time { return snap.Add(time.Hour) }
		})
	}
	retired := func(h keys.Holders) keys.Holders { h.Recorded, h.RetiredAt = true, time.Now(); return h }
	for _, c := range []struct {
		name, holder, says string
		held               func(e *destroyEnv) keys.Holders
	}{
		{
			"a retained backup whose pool names a key of the connection", keys.HolderBackup, "backup 20270301T024117Z (snapshot 2027-03-01T02:41:17Z) holds pool objects under sk-archive-1-",
			func(e *destroyEnv) keys.Holders {
				return retired(keys.Holders{BackupRuns: 3, Listing: listingOf(t, target, backupOf("20270301T024117Z", []string{e.ids[1]}, []string{}))})
			},
		},
		{
			"an active run", keys.HolderBackups, "a backup run that writes the target is active",
			func(*destroyEnv) keys.Holders {
				return retired(keys.Holders{BackupRuns: 3, BackupActive: true, Listing: listingOf(t, target)})
			},
		},
		{
			"a newer create --no-prune", keys.HolderBackups, "recorded no retention listing",
			func(*destroyEnv) keys.Holders {
				return retired(keys.Holders{BackupRuns: 3, Listing: &keys.Listing{RunID: "r", Target: target, Result: json.RawMessage(`{"backupId":"x"}`)}})
			},
		},
		{
			"a retained backup without poolKeyIds", keys.HolderBackup, "records no pool keys (poolKeyIds; its manifest has no storageKeys)",
			func(*destroyEnv) keys.Holders {
				return retired(keys.Holders{BackupRuns: 3, Listing: listingOf(t, target, map[string]any{"backupId": "20270301T024117Z", "snapshotAt": snap})})
			},
		},
		{
			"another target", keys.HolderBackups, "the newest retention listing is of the target s3://elsewhere/quader, and this configuration names s3://quader-backups/quader",
			func(*destroyEnv) keys.Holders {
				return retired(keys.Holders{BackupRuns: 3, Listing: listingOf(t, "s3://elsewhere/quader")})
			},
		},
	} {
		t.Run("R-STOR-100 "+c.name+" refuses", func(t *testing.T) {
			d, err := run(t, c.held, cfg)
			if !errors.Is(err, keys.ErrHeld) || !refusedBy(d, c.holder, c.says) {
				t.Fatalf("%v %+v", err, d.Refusals)
			}
		})
	}
	t.Run("R-STOR-100 the refusal for a retained backup says when it leaves retention and how to delete it", func(t *testing.T) {
		d, _ := run(t, c0(t, target, backupOf), cfg)
		if !refusedBy(d, keys.HolderBackup, "it leaves retention not before 2027-03-31T02:41:17Z and once 2 newer backups exist (keep_last 2), or delete it: quader backup delete 20270301T024117Z") {
			t.Fatalf("%+v", d.Refusals)
		}
	})
	t.Run("R-STOR-100 without a backup target and without a run the check passes and the report says so", func(t *testing.T) {
		d, err := run(t, func(*destroyEnv) keys.Holders { return retired(keys.Holders{}) }, keys.BackupTarget{})
		if err != nil || d.Remaining.Backups.Recorded {
			t.Fatalf("%v %+v", err, d.Remaining.Backups)
		}
	})
	t.Run("R-STOR-100 a listing whose pools name none of the keys passes and counts the backups that hold them sealed", func(t *testing.T) {
		d, err := run(t, func(e *destroyEnv) keys.Holders {
			return retired(keys.Holders{BackupRuns: 3, Listing: listingOf(t, "s3://quader-backups/quader", backupOf("20270301T024117Z", []string{"sk-other-1-0000aaaa"}, e.ids))})
		}, cfg)
		b := d.Remaining.Backups
		if err != nil || b.Count == nil || *b.Count != 1 || b.LeaveNotBefore == nil || !b.LeaveNotBefore.Equal(snap.Add(30*24*time.Hour)) || b.Target != "s3://quader-backups/quader" {
			t.Fatalf("%v %+v", err, b)
		}
	})
}

// c0 is the holders of a retired connection whose one retained backup pools under its first storage key.
func c0(t *testing.T, target string, backupOf func(string, any, any) map[string]any) func(e *destroyEnv) keys.Holders {
	return func(e *destroyEnv) keys.Holders {
		return keys.Holders{Recorded: true, RetiredAt: time.Now(), BackupRuns: 1, Listing: listingOf(t, target, backupOf("20270301T024117Z", []string{e.ids[1]}, nil))}
	}
}

func TestR_STOR_101_ReplicasForgetDestroyedKeysAndReadsAnswerDestroyedKey(t *testing.T) {
	e := newDestroyEnv(t)
	late := e.holder(t)
	if err := late.Load(ctx, "archive"); err != nil {
		t.Fatal(err)
	}
	gen2, _ := e.h.Active(ctx, "archive")
	name, _ := e.h.NameKey("archive")
	md, want := record(t, gen2)
	d, err := e.destroy(e.confirmation(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("R-STOR-101 at the change the holder destroys every key of the connection; Held lists none and the keys refuse use", func(t *testing.T) {
		e.h.ApplyChange(cluster.AreaStorageKeys, keys.DestroyedChangeVersion, mustJSON(t, keys.DestroyedChange("archive", d.Destroyed)))
		if storage, active, n := e.h.Held("archive"); len(storage) != 0 || active != "" || n != "" {
			t.Fatalf("held %v %q %q", storage, active, n)
		}
		if _, err := gen2.Key.Bytes(); err == nil {
			t.Error("the storage key still holds its bytes")
		}
		if _, err := name.Bytes(); err == nil {
			t.Error("the name key still holds its bytes")
		}
		if _, err := e.h.Active(ctx, "archive"); !errors.Is(err, keys.ErrNoKey) {
			t.Errorf("Active: %v", err)
		}
		_, err := blobcrypt.Open(md, e.h.Lookup(ctx, "archive"), want)
		if reasonOf(err) != blobcrypt.ReasonDestroyedKey || !errors.Is(err, blobcrypt.ErrDestroyedKey) || !reasonOf(err).MarksCorrupt() {
			t.Fatalf("a record of a destroyed key: %v", err)
		}
		if !strings.Contains(err.Error(), "the storage key "+gen2.ID+" of connection archive was destroyed on") {
			t.Errorf("message %q", err)
		}
	})
	t.Run("R-STOR-101 a replica that missed the change catches up at its next poll, and a load skips destroyed rows without a refusal", func(t *testing.T) {
		if storage, _, _ := late.Held("archive"); len(storage) != 2 {
			t.Fatalf("the late replica held %v before its poll", storage)
		}
		late.FlushArea(cluster.AreaStorageKeys, "poll")
		if _, err := blobcrypt.Open(md, late.Lookup(ctx, "archive"), want); reasonOf(err) != blobcrypt.ReasonDestroyedKey {
			t.Fatalf("after the poll: %v", err)
		}
		if storage, active, n := late.Held("archive"); len(storage) != 0 || active != "" || n != "" {
			t.Fatalf("the late replica holds %v %q %q", storage, active, n)
		}
		fresh := e.holder(t)
		if err := fresh.Load(ctx, "archive"); err != nil {
			t.Fatalf("a load over destroyed rows: %v", err)
		}
		if storage, _, _ := fresh.Held("archive"); len(storage) != 0 {
			t.Fatalf("a load opened %v", storage)
		}
	})
	t.Run("R-STOR-101 Q-STOR-462 a change whose list was cut to fit the change log forgets every key all the same", func(t *testing.T) {
		f := newDestroyEnv(t)
		f.h.ApplyChange(cluster.AreaStorageKeys, keys.DestroyedChangeVersion, json.RawMessage(`{"connection":"archive","destroyed":[]}`))
		if storage, active, n := f.h.Held("archive"); len(storage) != 0 || active != "" || n != "" {
			t.Fatalf("held %v %q %q", storage, active, n)
		}
		many := make([]string, 40)
		for i := range many {
			many[i] = fmt.Sprintf("sk-a-connection-of-thirty-two-c-%d-0000aaaa", i+1)
		}
		b := mustJSON(t, keys.DestroyedChange("a-connection-of-thirty-two-c", many))
		var p struct{ Destroyed []string }
		_ = json.Unmarshal(b, &p)
		// the change log refuses a payload above cluster.MaxValueSize less its envelope of 96 bytes (cluster/changelog_notify.go)
		if len(b) > cluster.MaxValueSize-96 || len(p.Destroyed) == 0 || len(p.Destroyed) == len(many) {
			t.Fatalf("%d bytes with %d of %d identifiers", len(b), len(p.Destroyed), len(many))
		}
	})
}

func TestR_STOR_102_AnIDWithDestroyedKeysIsRetiredForGood(t *testing.T) {
	e := newDestroyEnv(t)
	if _, err := e.destroy(e.confirmation(), nil); err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC().Format(time.DateOnly)
	want := "the storage keys of connection archive were destroyed on " + day + "; the id is retired for good; choose another id (storage.md R-STOR-102)"
	writes := e.db.Writes
	for _, drain := range []bool{false, true} {
		c := e.archive()
		c.Drain = drain
		if err := e.holder(t).Ensure(ctx, c); !keys.IsRefusal(err) || err.Error() != want {
			t.Errorf("R-STOR-102 Ensure (drain %v): %v, want %q", drain, err, want)
		}
	}
	if _, err := keys.NewGeneration(ctx, e.db, e.db, e.ring, nil, "archive", nil); !keys.IsRefusal(err) || err.Error() != want {
		t.Errorf("R-STOR-102 a new generation: %v", err)
	}
	if e.db.Writes != writes {
		t.Error("R-STOR-102 something was drawn for a destroyed id")
	}
	if got := blob.RetiredForGood("archive", time.Now()); got != want {
		t.Errorf("R-STOR-102 the start check's sentence %q", got)
	}
	t.Run("R-STOR-102 a destruction between the first read and the drawing transaction is refused there", func(t *testing.T) {
		f := newEnv(t)
		f.store.SetFault(func(op, _ string) error {
			if op == "list" {
				f.db.Put(keys.Row{
					KeyID: "sk-late-1-0000aaaa", Connection: "late", Kind: keys.KindStorage, Generation: 1, State: keys.StateDestroyed,
					CreatedAt: time.Now(), RetiredAt: time.Now(), DestroyedAt: time.Now(),
				})
			}
			return nil
		})
		err := f.holder(t).Ensure(ctx, keys.Connection{ID: "late", Prefix: "late", Objects: f.store})
		if !keys.IsRefusal(err) || len(f.db.Rows()) != 1 {
			t.Fatalf("%v, %d rows", err, len(f.db.Rows()))
		}
	})
}

func TestR_STOR_097_TheWalkSkipsDestroyedRows(t *testing.T) {
	e := newDestroyEnv(t)
	if err := e.h.Ensure(ctx, keys.Connection{ID: "other", Prefix: "other", Objects: e.store}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.destroy(e.confirmation(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := keys.Reseal(ctx, e.db, e.db, ringOf(t, "k2", map[string]byte{"k1": 1, "k2": 2}), 0)
	if err != nil || res != (keys.ResealResult{Resealed: 1}) {
		t.Fatalf("R-STOR-097 the walk after a destruction: %+v %v, want the other connection's key resealed and failed 0", res, err)
	}
}

func TestR_HA_054_TheReplayOfADestruction(t *testing.T) {
	at := time.Date(2027, 4, 2, 9, 0, 0, 0, time.UTC)
	now := at.Add(time.Hour)
	type announced struct {
		conn string
		ids  []string
	}
	replay := func(t *testing.T, e *destroyEnv, entry keys.DestroyedEntry, configured []string) (keys.ReplayResult, []announced) {
		t.Helper()
		var got []announced
		var res keys.ReplayResult
		err := e.db.InTx(ctx, func(tx meta.Tx) (err error) {
			res, err = keys.ReplayDestroyed(ctx, tx, e.db, entry, configured, now, func(_ meta.Tx, c string, ids []string) error {
				got = append(got, announced{c, ids})
				return nil
			})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return res, got
	}
	t.Run("R-HA-054 Q-HA-416 a sealed row is destroyed and the area announced: applied", func(t *testing.T) {
		e := newDestroyEnv(t)
		res, got := replay(t, e, keys.DestroyedEntry{Connection: "archive", KeyIDs: e.ids, DestroyedAt: at}, []string{"default"})
		if res.Outcome != keys.ReplayApplied || len(res.Changed) != 3 || len(got) != 1 || got[0].conn != "archive" {
			t.Fatalf("%+v %+v", res, got)
		}
		for _, r := range e.db.Rows() {
			if r.State != keys.StateDestroyed || r.Sealed != "" || !r.DestroyedAt.Equal(at) {
				t.Errorf("row %+v", r)
			}
		}
	})
	t.Run("R-HA-054 Q-HA-416 a destroyed row is already in effect", func(t *testing.T) {
		e := newDestroyEnv(t)
		if _, err := e.destroy(e.confirmation(), nil); err != nil {
			t.Fatal(err)
		}
		if res, got := replay(t, e, keys.DestroyedEntry{Connection: "archive", KeyIDs: e.ids, DestroyedAt: at}, []string{"default"}); res.Outcome != keys.ReplayInEffect || len(got) != 0 {
			t.Fatalf("%+v %+v", res, got)
		}
	})
	t.Run("R-HA-054 Q-HA-416 a missing row is inserted destroyed with created_at at the entry's time: applied", func(t *testing.T) {
		e := newDestroyEnv(t)
		res, _ := replay(t, e, keys.DestroyedEntry{Connection: "gone", KeyIDs: []string{"sk-gone-1-0000aaaa", "nk-gone-0000bbbb"}, DestroyedAt: at}, []string{"default"})
		rows := slices.DeleteFunc(e.db.Rows(), func(r keys.Row) bool { return r.Connection != "gone" })
		if res.Outcome != keys.ReplayApplied || len(rows) != 2 {
			t.Fatalf("%+v %+v", res, rows)
		}
		for _, r := range rows {
			if r.State != keys.StateDestroyed || !r.CreatedAt.Equal(at) || !r.DestroyedAt.Equal(at) || r.Sealed != "" {
				t.Errorf("row %+v", r)
			}
		}
	})
	t.Run("R-HA-054 Q-HA-417 an entry for a connection of the restoring configuration is skipped with its note, its keys still sealed", func(t *testing.T) {
		e := newDestroyEnv(t)
		res, got := replay(t, e, keys.DestroyedEntry{Connection: "archive", KeyIDs: e.ids, DestroyedAt: at}, []string{"default", "archive"})
		if res.Outcome != keys.ReplaySkipped || res.Note != "connection archive is in this installation's configuration; its keys are not destroyed" || len(got) != 0 {
			t.Fatalf("%+v %+v", res, got)
		}
		for _, r := range e.db.Rows() {
			if r.State == keys.StateDestroyed || r.Sealed == "" {
				t.Errorf("row %+v", r)
			}
		}
		if res, _ := replay(t, e, keys.DestroyedEntry{Connection: "archive", KeyIDs: e.ids, DestroyedAt: at}, nil); res.Outcome != keys.ReplaySkipped {
			t.Errorf("without the restoring configuration's connections: %+v", res)
		}
		if res, _ := replay(t, e, keys.DestroyedEntry{Connection: "archive", KeyIDs: []string{"sk-other-1-0000aaaa"}, DestroyedAt: at}, []string{"default"}); res.Outcome != keys.ReplayMalformed {
			t.Errorf("an entry naming another connection's key: %+v", res)
		}
	})
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
