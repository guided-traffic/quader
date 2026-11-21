//go:build integration

package backup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/journal"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store" // the replay handlers of the signatures
)

// sigEntry is a journal entry of the signatures in the producers' shape.
func sigEntry(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestR_SIG_041_WhatNarrowedTrustIsReplayedIntoARestore: a database restored to a point before an anchor was removed, a signer blocked,
// project anchors switched off and a release of a repository's gate withdrawn gets all four again from the revocation journal, the anchor
// epoch rises so that the trust job recomputes, a second replay changes nothing, and the replay writes no audit event (R-SIG-041,
// R-SIG-076, Q-SIG-42, R-HA-054). Integration, about 2 s, rows.
func TestR_SIG_041_WhatNarrowedTrustIsReplayedIntoARestore(t *testing.T) {
	tw := newTwin(t)
	db := tw.open(tw.src)
	db.exec(`UPDATE signature_settings SET project_anchors = true`)
	id := db.number(`INSERT INTO signature_anchors (scope, key, title, kind, material)
		VALUES ('installation', 'release-key', 'Release key', 'openpgp', '{"openpgp":{"keys":"x"}}'::jsonb) RETURNING id`)
	db.exec("INSERT INTO project (id, key, display_name) VALUES (gen_random_uuid(), 'web', 'web')")
	repo := db.number(`INSERT INTO repositories (key, class, format, config, project_id)
		SELECT 'files-internal', 'local', 'generic', '{}'::jsonb, id FROM project WHERE key = 'web' RETURNING id`)
	db.exec(`INSERT INTO signature_releases (repo_id, subject_sha256, reason, refused_reason) VALUES ($1, decode(repeat('cd', 32), 'hex'), 'urgent', 'missing')`, repo)
	epoch := db.number(`SELECT anchor_epoch FROM signature_settings`)
	eventsBefore := db.number(`SELECT count(*) FROM audit_event`)

	store := newMemStore()
	ids := map[string]string{
		"removed": handWritten(t, store, audit.EventSignatureAnchorRemoved, sigEntry(t, sigstore.AnchorRemovedEntry{ID: id, Scope: "installation", Key: "release-key"})),
		"blocked": handWritten(t, store, audit.EventSignatureSignerBlocked, sigEntry(t, sigstore.BlockEntry{
			Kind: "fingerprint", Value: "C1F3B6A07D4E", Reason: "the key left the build machine",
		})),
		"off": handWritten(t, store, audit.EventSignatureSettingsChanged, sigEntry(t, sigstore.SettingsEntry{ProjectAnchors: false})),
		"withdrawn": handWritten(t, store, audit.EventSignatureReleaseWithdrawn, sigEntry(t, sigstore.ReleaseWithdrawnEntry{
			RepoID: repo, Repository: "web/files-internal", SHA256: strings.Repeat("cd", 32),
		})),
	}
	sum, printed := db.replay(store, time.Now().Add(-time.Minute), nil)
	outcome := map[string]journal.Outcome{}
	for _, r := range sum.Results {
		outcome[r.ID] = r.Outcome
	}
	for what, id := range ids {
		if outcome[id] != journal.Applied {
			t.Errorf("%s: %s, want applied\n%s", what, outcome[id], printed)
		}
	}
	if n := db.number(`SELECT count(*) FROM signature_anchors WHERE key = 'release-key'`); n != 0 {
		t.Errorf("the removed anchor is back: %d", n)
	}
	if got := db.text(`SELECT kind || ' ' || value || ' ' || reason FROM signature_blocks`); got != "fingerprint C1F3B6A07D4E the key left the build machine" {
		t.Errorf("the block reads %q", got)
	}
	if n := db.number(`SELECT count(*) FROM signature_releases`); n != 0 {
		t.Errorf("the withdrawn release is back: %d", n)
	}
	if got := db.text(`SELECT project_anchors::text FROM signature_settings`); got != "false" {
		t.Errorf("project anchors are %s, want off", got)
	}
	if after := db.number(`SELECT anchor_epoch FROM signature_settings`); after <= epoch {
		t.Errorf("the anchor epoch is %d, was %d: the trust job would not recompute", after, epoch)
	}
	if n := db.number(`SELECT count(*) FROM audit_event`) - eventsBefore; n != 0 {
		t.Errorf("%d audit events written by the replay", n)
	}
	again, _ := db.replay(store, time.Now().Add(-time.Minute), nil)
	for _, r := range again.Results {
		if r.Outcome == journal.Applied {
			t.Errorf("second pass applied %s", r.Line())
		}
	}
}
