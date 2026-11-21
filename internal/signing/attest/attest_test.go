package attest

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signing/statement"
)

// TestR_SIG_051_TheMalwareSnapshotOfTheBlobsAStatementNames: an image's statement names the worst verdict of its blobs (infected before
// pending before failed and unscannable before clean), with the database and the times of the verdict that decided it; a blob without a
// verdict is pending; a release counts only when it passes every blob that is not clean. Unit, microseconds.
func TestR_SIG_051_TheMalwareSnapshotOfTheBlobsAStatementNames(t *testing.T) {
	clean := q.SigningAttestVerdictsRow{State: "clean", DbVersion: "main-62", ScannedAt: "2026-11-21T10:00:00.000000Z"}
	newer := q.SigningAttestVerdictsRow{State: "clean", DbVersion: "main-63", ScannedAt: "2026-11-21T11:00:00.000000Z"}
	infected := q.SigningAttestVerdictsRow{State: "infected", DbVersion: "main-62", ScannedAt: "2026-11-21T09:00:00.000000Z"}
	released := infected
	released.Released = true
	for name, c := range map[string]struct {
		rows        []q.SigningAttestVerdictsRow
		want        int
		state, db   string
		wantRelease bool
	}{
		"clean":                    {[]q.SigningAttestVerdictsRow{clean, newer}, 2, "clean", "main-63", false},
		"one blob without verdict": {[]q.SigningAttestVerdictsRow{clean}, 2, statement.MalwarePending, "", false},
		"infected wins":            {[]q.SigningAttestVerdictsRow{newer, infected}, 2, "infected", "main-62", false},
		"released":                 {[]q.SigningAttestVerdictsRow{released}, 1, "infected", "main-62", true},
		"pending row":              {[]q.SigningAttestVerdictsRow{{State: "pending"}}, 1, statement.MalwarePending, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := malwareOf(c.rows, c.want)
			if got.State != c.state || got.Database != c.db || got.Released != c.wantRelease {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// TestR_SIG_053_ANewAttestationLooksAgainSoonThenLessOftenAndNeverAfterItsBound: the job looks again after a quarter of the item's age,
// between five seconds and five minutes, and at the bound of wait_for_checks at the latest. Unit, microseconds.
func TestR_SIG_053_ANewAttestationLooksAgainSoonThenLessOftenAndNeverAfterItsBound(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	a := &Attester{o: Options{Now: func() time.Time { return now }}}
	if got := a.poll(now.Add(-2*time.Second), now.Add(time.Hour)); !got.Equal(now.Add(minPoll)) {
		t.Errorf("fresh: %v", got)
	}
	if got := a.poll(now.Add(-40*time.Minute), now.Add(20*time.Minute)); !got.Equal(now.Add(maxPoll)) {
		t.Errorf("old: %v", got)
	}
	if got := a.poll(now.Add(-58*time.Minute), now.Add(2*time.Minute)); !got.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("at the bound: %v", got)
	}
}

// TestR_SIG_051_AnUploadNamesItsPrincipalAndAFillItsUpstream: the origin of an upload carries the principal and the CI claims the
// record kept, the one of a remote's item the URL it was fetched from and when. Unit, microseconds.
func TestR_SIG_051_AnUploadNamesItsPrincipalAndAFillItsUpstream(t *testing.T) {
	at := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	up := item{SigningAttestItemRow: q.SigningAttestItemRow{Class: "local", PrincipalKind: "ci_mapping", PrincipalName: "gitlab", Ci: `{"issuer":"gitlab","ref":"main"}`}, receivedAt: at}
	if o := originOf(up); o.Kind != statement.OriginUpload || o.PrincipalName != "gitlab" || o.CI["ref"] != "main" {
		t.Errorf("upload: %+v", o)
	}
	fill := item{SigningAttestItemRow: q.SigningAttestItemRow{Class: "remote", UpstreamUrl: "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"}, receivedAt: at}
	if o := originOf(fill); o.Kind != statement.OriginFill || o.Upstream == "" || !o.FetchedAt.Equal(at) {
		t.Errorf("fill: %+v", o)
	}
}
