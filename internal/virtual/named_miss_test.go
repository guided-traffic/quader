package virtual_test

import (
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
	vt "github.com/guided-traffic/quader/internal/virtual/virtualtest"
)

// TestR_AUTHZ_009_OnlyANamedMissLetsTheWalkGoOn: the walk of the engine that npm, NuGet, Ansible Galaxy and generic files resolve
// through goes on past a member only at a miss or a failure it names (errs.IsMemberMiss, errs.IsMemberFailure, formats/oci.md Q-OCI-245).
// A read refused at a member (403 forbidden) is a miss in every tier and in the metadata fan-out: an unreadable member is skipped
// and never turns the answer into 403 (R-AUTHZ-009 rules 2 and 8, R-REPO-019 step 4). A member that holds the item back, by a gate's
// pending answer (malware_check_pending, signature_check_pending) or by a 503 or a refusal no rule names, ends the walk with that
// answer, and the next member is not asked, so it never serves what the first one holds back (R-REPO-019 step 7, R-MAL-018,
// signatures.md R-SIG-065). Unit, microseconds, items of a few bytes.
func TestR_AUTHZ_009_OnlyANamedMissLetsTheWalkGoOn(t *testing.T) {
	refusedRead := func() error {
		return errs.E(errs.Forbidden, errs.CodeForbidden, "The action read is not permitted on it/npm-remote.")
	}
	t.Run("a read refused at a remote member's fetch is a miss", func(t *testing.T) {
		w := newWorld(t)
		w.arts.FetchErr["npm-remote"] = refusedRead()
		_, err := w.artifact(alice, "left-pad", "1.0.0")
		if k, _ := kind(err); k != errs.NotFound {
			t.Fatalf("err = %v, want 404", err)
		}
	})
	t.Run("a read refused at a remote member's cache is a miss, and the next member serves", func(t *testing.T) {
		w := newWorld(t, func(o *worldOpts) { o.members = []repository.Repository{local, remote, remote2} })
		w.arts.CachedErr["npm-remote"] = refusedRead()
		vt.Put(w.arts.CachedItems, "npm-remote2", tgz, "second")
		rec, err := w.artifact(alice, "left-pad", "1.0.0")
		if err != nil || rec.Body.String() != "second" {
			t.Fatalf("body %q err %v", rec.Body.String(), err)
		}
	})
	t.Run("a read refused at a remote member's metadata leaves the member out of the merge", func(t *testing.T) {
		w := newWorld(t)
		w.rows.Publish(idLocal, "lodash", "1.0.0")
		w.remote.MetadataErr["npm-remote"] = refusedRead()
		m, err := w.metadata(alice, "lodash")
		if v := versionsOf(t, m, err); len(v) != 1 || m.Partial {
			t.Fatalf("merged %v partial %v err %v", v, m.Partial, err)
		}
	})
	for _, held := range []*errs.Error{
		errs.E(errs.Unavailable, errs.CodeMalwareCheckPending, "Quader is checking it.", errs.WithRetryAfter(30*time.Second)),
		errs.E(errs.Unavailable, errs.CodeSignatureCheckPending, "Its signatures are being checked.", errs.WithRetryAfter(30*time.Second)),
		errs.E(errs.Unavailable, "a_gate_pending_not_yet_written", "Held by a gate the walk does not know.", errs.WithRetryAfter(time.Second)),
		errs.E(errs.Forbidden, "a_gate_not_yet_written", "Refused by a gate the walk does not know."),
		errs.E(errs.Unavailable, errs.CodeDatabaseUnavailable, "The replica's database, no statement about the member."),
	} {
		t.Run(held.Code+" at a remote member's fetch ends the walk", func(t *testing.T) {
			w := newWorld(t, func(o *worldOpts) { o.members = []repository.Repository{local, remote, remote2} })
			w.arts.FetchErr["npm-remote"] = held
			vt.Put(w.arts.UpstreamItems, "npm-remote2", tgz, "second")
			rec, err := w.artifact(alice, "left-pad", "1.0.0")
			if e, _ := errs.As(err); e == nil || e.Code != held.Code || rec.Body.String() == "second" || slices.Contains(w.arts.Calls(), "fetch:npm-remote2") {
				t.Fatalf("err %v body %q calls %v: want the member's answer and no later member", err, rec.Body.String(), w.arts.Calls())
			}
		})
		t.Run(held.Code+" of a local member's stored item ends the walk", func(t *testing.T) {
			w := newWorld(t)
			vt.Put(w.arts.LocalItems, "npm-local", tgz, "local")
			w.arts.HitErr["npm-local"] = held
			vt.Put(w.arts.UpstreamItems, "npm-remote", tgz, "remote")
			rec, err := w.artifact(alice, "left-pad", "1.0.0")
			if e, _ := errs.As(err); e == nil || e.Code != held.Code || slices.Contains(w.arts.Calls(), "fetch:npm-remote") {
				t.Fatalf("err %v body %q calls %v: want the member's answer and no later member", err, rec.Body.String(), w.arts.Calls())
			}
		})
		t.Run(held.Code+" of a remote member's metadata ends the merge", func(t *testing.T) {
			w := newWorld(t)
			w.rows.Publish(idLocal, "lodash", "1.0.0")
			w.remote.MetadataErr["npm-remote"] = held
			if _, err := w.metadata(alice, "lodash"); err == nil {
				t.Fatal("the merge answered without the member that holds the document back")
			} else if e, _ := errs.As(err); e == nil || e.Code != held.Code {
				t.Fatalf("err = %v, want the member's answer", err)
			}
		})
	}
	t.Run("the named failures let the walk go on", func(t *testing.T) {
		for _, code := range []string{errs.CodeUpstreamAssumedOffline, errs.CodeUpstreamRateLimited, errs.CodeFetchQueueTimeout, errs.CodeUnavailable} {
			w := newWorld(t, func(o *worldOpts) { o.members = []repository.Repository{local, remote, remote2} })
			w.arts.FetchErr["npm-remote"] = errs.E(errs.Unavailable, code, "could not be asked")
			vt.Put(w.arts.UpstreamItems, "npm-remote2", tgz, "second")
			if rec, err := w.artifact(alice, "left-pad", "1.0.0"); err != nil || rec.Body.String() != "second" {
				t.Errorf("%s: body %q err %v, want the next member's item", code, rec.Body.String(), err)
			}
		}
	})
}
