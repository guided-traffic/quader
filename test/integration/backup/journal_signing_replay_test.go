//go:build integration

package backup

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/journal"
	signingstore "github.com/guided-traffic/quader/internal/signing/store" // the replay handlers of the signing identity
)

// TestR_SIG_059_AWithdrawnIdentityDoesNotComeBackWithARestore: a database restored to a point before the current identity was removed
// and a previous one retired gets both again from the revocation journal: the removed one is previous with its end of signing, the
// retired one retired with its sealed key gone; a second replay changes nothing, an entry for another certificate is not applied, and the
// replay writes no audit event (R-SIG-059, R-AUTHZ-031's rule). Integration, about 1 s, rows.
func TestR_SIG_059_AWithdrawnIdentityDoesNotComeBackWithARestore(t *testing.T) {
	tw := newTwin(t)
	db := tw.open(tw.src)
	// insert writes an uploaded identity with a sealed key (the value is not opened here) and npm's keyid of 43 base64 characters.
	insert := func(state, cert string) int64 {
		return db.number(`INSERT INTO signing_identities (source, state, public_key, key_sha256, npm_keyid, sealed_key, certificate,
				cert_sha256, not_before, not_after, expires_at)
			VALUES ('upload', $1, '\x01', decode(repeat('aa', 32), 'hex'), $3, 'v1:k:sealed', '\x02', decode($2, 'hex'),
				now() - interval '1 day', now() + interval '60 days', CASE WHEN $1 = 'previous' THEN now() - interval '1 hour' END)
			RETURNING id`, state, cert, "SHA256:"+strings.Repeat("A", 43))
	}
	certCurrent, certPrevious := hexOf(0x11), hexOf(0x22)
	current := insert("current", certCurrent)
	previous := insert("previous", certPrevious)
	eventsBefore := db.number(`SELECT count(*) FROM audit_event`)

	store := newMemStore()
	ids := map[string]string{
		"removed": handWritten(t, store, audit.EventSigningRemoved, sigEntry(t, signingstore.RemovedEntry{ID: current, CertSHA256: certCurrent})),
		"retired": handWritten(t, store, audit.EventSigningRetired, sigEntry(t, signingstore.RetiredEntry{ID: previous, CertSHA256: certPrevious})),
		"other":   handWritten(t, store, audit.EventSigningRetired, sigEntry(t, signingstore.RetiredEntry{ID: current, CertSHA256: hexOf(0x33)})),
	}
	sum, printed := db.replay(store, time.Now().Add(-time.Minute), nil)
	outcome := map[string]journal.Outcome{}
	for _, r := range sum.Results {
		outcome[r.ID] = r.Outcome
	}
	if outcome[ids["removed"]] != journal.Applied || outcome[ids["retired"]] != journal.Applied {
		t.Fatalf("removed %s, retired %s\n%s", outcome[ids["removed"]], outcome[ids["retired"]], printed)
	}
	if outcome[ids["other"]] == journal.Applied {
		t.Errorf("an entry for another certificate was applied\n%s", printed)
	}
	if got := db.text(`SELECT state || ' ' || (expires_at IS NOT NULL)::text FROM signing_identities WHERE id = $1`, current); got != "previous true" {
		t.Errorf("the removed identity is %q", got)
	}
	if got := db.text(`SELECT state || ' ' || (sealed_key IS NULL)::text || ' ' || (retired_at IS NOT NULL)::text FROM signing_identities WHERE id = $1`, previous); got != "retired true true" {
		t.Errorf("the retired identity is %q", got)
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

func hexOf(b byte) string {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return hex.EncodeToString(out)
}
