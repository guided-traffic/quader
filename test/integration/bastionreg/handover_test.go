//go:build integration

package bastionreg

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
)

// TestR_BAS_071_TheSwitchIsPerRegistration: a new registration has the handover off; PATCH {"handover": true} with
// endpoints:write answers 200, raises the revision and records bastion.handover.enabled without a journal entry, and a caller
// with endpoints:read is refused; a null switches nothing (nor does a null enabled); the replica's snapshot follows the change area
// (R-ARCH-018); {"handover": false} records bastion.handover.disabled with the journal entry {principalId, name}, and the replay
// of that entry switches the member off again after a restore to a point before it (R-HA-054), already in effect the second time.
func TestR_BAS_071_TheSwitchIsPerRegistration(t *testing.T) {
	e := newEnv(t)
	r, c := e.newCast("web-1", replicaOpts{})
	created := r.call("POST", bastions, c.admin, map[string]any{"name": "partners"})
	want(t, created, http.StatusCreated, "")
	if created.json(t)["handover"] != false {
		t.Fatalf("a new registration: %s", created.Body)
	}
	bid := e.text(`SELECT id::text FROM principal WHERE kind = 'bastion' AND name = 'partners'`)
	switches := bastionmain.NewRegistrationSnapshot(r.db, r.caches, quiet)
	if err := switches.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = switches.Stop(context.Background()) })
	if switches.On(bid) {
		t.Fatal("the snapshot says on for a new registration")
	}

	want(t, r.call("PATCH", bastions+"/partners", c.auditor, map[string]any{"handover": true}, "If-Match", "*"), http.StatusForbidden, "")
	on := r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"handover": true}, "If-Match", created.Header.Get("ETag"))
	want(t, on, http.StatusOK, "")
	if on.json(t)["handover"] != true || on.Header.Get("ETag") == created.Header.Get("ETag") {
		t.Fatalf("after the switch-on: %s, ETag %q", on.Body, on.Header.Get("ETag"))
	}
	if got := e.events(audit.EventBastionHandoverEnabled); len(got) != 1 || got[0].ResourceType != "bastion" || got[0].ResourceID != bid {
		t.Fatalf("bastion.handover.enabled = %+v", got)
	}
	waitFor(t, "the snapshot of the switch-on", 5*time.Second, func() bool { return switches.On(bid) })

	t.Run("a null switches nothing, nor does a null enabled", func(t *testing.T) {
		want(t, r.call("PATCH", bastions+"/partners", c.admin, `{"handover": null, "enabled": null}`, "If-Match", "*"), http.StatusOK, "")
		if got := r.call("GET", bastions+"/partners", c.admin, nil).json(t); got["handover"] != true || got["enabled"] != true {
			t.Fatalf("after the nulls: %v", got)
		}
	})

	off := r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"handover": false}, "If-Match", "*")
	want(t, off, http.StatusOK, "")
	if got := e.events(audit.EventBastionHandoverDisabled); len(got) != 1 || got[0].ResourceID != bid {
		t.Fatalf("bastion.handover.disabled = %+v", got)
	}
	if n := e.number(`SELECT count(*) FROM revocation_outbox WHERE event = 'bastion.handover.disabled' AND entry ->> 'principalId' = $1 AND entry ->> 'name' = 'partners'`, bid); n != 1 {
		t.Fatalf("journal entries of the switch-off = %d, want 1", n)
	}
	if n := e.number(`SELECT count(*) FROM revocation_outbox WHERE event = 'bastion.handover.enabled'`); n != 0 {
		t.Fatalf("%d journal entries of the switch-on, want none", n)
	}
	waitFor(t, "the snapshot of the switch-off", 5*time.Second, func() bool { return !switches.On(bid) })

	t.Run("the replay switches the member off after a restore to a point before the switch-off", func(t *testing.T) {
		e.exec(`UPDATE bastion SET handover = true WHERE principal_id = $1::uuid`, bid) // the restored row
		h, ok := journal.HandlerFor(audit.EventBastionHandoverDisabled)
		if !ok {
			t.Fatal("no replay handler")
		}
		obj := journal.Object{Event: audit.EventBastionHandoverDisabled, Entry: []byte(`{"principalId":"` + bid + `","name":"partners"}`)}
		replay := func() journal.Outcome {
			var res journal.Result
			if err := r.db.InTx(context.Background(), func(tx meta.Tx) error {
				var err error
				res, err = h.Apply(context.Background(), tx, journal.Env{Notifier: r.changes}, obj)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return res.Outcome
		}
		if got := replay(); got != journal.Applied {
			t.Fatalf("first replay: %s", got)
		}
		if e.number(`SELECT count(*) FROM bastion WHERE principal_id = $1::uuid AND handover`, bid) != 0 {
			t.Fatal("the replay left the handover on")
		}
		if got := replay(); got != journal.AlreadyInEffect {
			t.Fatalf("second replay: %s", got)
		}
		unknown := journal.Object{Event: audit.EventBastionHandoverDisabled, Entry: []byte(`{"principalId":"0192f1c4-0000-7000-8000-00000000dead","name":"gone"}`)}
		var res journal.Result
		if err := r.db.InTx(context.Background(), func(tx meta.Tx) error {
			var err error
			res, err = h.Apply(context.Background(), tx, journal.Env{}, unknown)
			return err
		}); err != nil || res.Outcome != journal.SubjectUnknown {
			t.Fatalf("an unknown registration: %+v %v", res, err)
		}
	})
}
