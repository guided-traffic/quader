package summary

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/history"
)

// Unit tests of the storage overview's states (storage.md R-STOR-107, Q-STOR-506): every state of every tile from views and snapshots
// built in memory. The reads behind the input are the integration test's (test/integration/blob/summary_test.go), the API's shape
// internal/api/v1's.

// overviewView is a connection of the overview's input: active on two live replicas unless changed.
func overviewView(id string, change func(*View)) View {
	at := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	v := View{Row: Row{ID: id, Configured: 2, LiveReplicas: 2, Undrained: true, FilledAt: &at, HoldsCopies: true}}
	if change != nil {
		change(&v)
	}
	v.State = v.Row.State()
	return v
}

// figures gives a view the snapshot's figures of a connection with n present copies, all read whole within the period.
func figures(n int64, change func(*Connection)) func(*View) {
	return func(v *View) {
		f := &Connection{ID: v.ID, PresentCopies: n, PresentBytes: n * 100, VerifiedWithinPeriod: n}
		if change != nil {
			change(f)
		}
		v.Figures = f
	}
}

func both(fs ...func(*View)) func(*View) {
	return func(v *View) {
		for _, f := range fs {
			f(v)
		}
	}
}

func TestR_STOR_107_TheStatesOfTheOverview(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC)
	snap := func(target int, below int64) *Summary {
		return &Summary{ComputedAt: now.Add(-20 * time.Minute), Copies: Copies{Target: target, BelowTarget: Pending{Blobs: below, Bytes: below * 100}}}
	}
	twoHealthy := func() []View {
		return []View{overviewView("default", figures(4, nil)), overviewView("archive", figures(4, nil))}
	}
	oldQueue := now.Add(-2 * time.Hour)
	newQueue := now.Add(-4 * time.Minute)
	sole := figures(4, func(c *Connection) { c.SoleCopies, c.SoleBytes = 2, 200 })

	cases := []struct {
		name  string
		in    OverviewInput
		check func(Overview) string
		want  string
	}{
		{"Connections healthy: every connection reachable on every replica", OverviewInput{Views: twoHealthy(), Latest: snap(2, 0)}, conn, ConnectionsHealthy},
		{"Connections degraded: one unreachable on one replica", OverviewInput{Views: []View{
			overviewView("default", figures(4, nil)), overviewView("archive", func(v *View) { v.CircuitOpen = 1 }),
		}}, conn, ConnectionsDegraded},
		{"Connections degraded: one detached", OverviewInput{Views: []View{
			overviewView("default", nil), overviewView("archive", func(v *View) { v.Configured, v.Undrained = 0, false }),
		}}, conn, ConnectionsDegraded},
		{"Connections unreachable: none reachable on any replica", OverviewInput{Views: []View{
			overviewView("default", func(v *View) { v.CircuitOpen = 2 }), overviewView("archive", func(v *View) { v.CircuitOpen = 2 }),
		}}, conn, ConnectionsUnreachable},
		{"Availability redundant: two targets, nothing below, no sole copy", OverviewInput{Views: twoHealthy(), Latest: snap(2, 0)}, avail, AvailabilityRedundant},
		{"Availability catching up: blobs below the target, the queue young", OverviewInput{Views: []View{
			overviewView("default", sole), overviewView("archive", both(figures(2, nil), func(v *View) { v.Queued, v.OldestQueuedAt = 2, &newQueue })),
		}, Latest: snap(2, 2)}, avail, AvailabilityCatchingUp},
		{"Availability at risk: sole copies and the queue older than an hour", OverviewInput{Views: []View{
			overviewView("default", sole), overviewView("archive", both(figures(2, nil), func(v *View) { v.Queued, v.OldestQueuedAt = 2, &oldQueue })),
		}, Latest: snap(2, 2)}, avail, AvailabilityAtRisk},
		{"Availability at risk: sole copies and a target unreachable on a replica", OverviewInput{Views: []View{
			overviewView("default", sole), overviewView("archive", both(figures(2, nil), func(v *View) { v.CircuitOpen = 1 })),
		}, Latest: snap(2, 2)}, avail, AvailabilityAtRisk},
		{"Availability catching up, not at risk: an old queue on a filling connection", OverviewInput{Views: []View{
			overviewView("default", sole), overviewView("archive", both(figures(2, nil), func(v *View) { v.FilledAt, v.Queued, v.OldestQueuedAt = nil, 2, &oldQueue })),
		}, Latest: snap(2, 2)}, avail, AvailabilityCatchingUp},
		{"Availability one store", OverviewInput{Views: []View{overviewView("default", figures(4, nil))}, Latest: snap(1, 0)}, avail, AvailabilityOneStore},
		{"Encryption encrypted: Quader encrypts every connection that holds content", OverviewInput{Views: []View{
			overviewView("default", func(v *View) { v.Encrypted = true }), overviewView("archive", func(v *View) { v.Encrypted = true }),
		}}, enc, EncryptionEncrypted},
		{"Encryption partly: one plain beside one encrypted", OverviewInput{Views: []View{
			overviewView("default", figures(1, nil)), overviewView("archive", both(figures(3, nil), func(v *View) { v.Encrypted = true })),
		}}, enc, EncryptionPartly},
		{"Encryption provider only: none by Quader, each store applied server-side encryption, a bucket default under none included", OverviewInput{Views: []View{
			overviewView("default", applied("AES256", "none")),
			overviewView("archive", applied("aws:kms", "kms")),
		}}, enc, EncryptionProviderOnly},
		{"Encryption off: each store asked, one reported nothing applied", OverviewInput{Views: []View{
			overviewView("default", applied("AES256", "s3")),
			overviewView("archive", applied("", "kms")),
		}}, enc, EncryptionOff},
		{"Encryption off: neither", OverviewInput{Views: []View{
			overviewView("default", func(v *View) { v.Local = &Local{Settings: Settings{SSEMode: "none"}} }),
		}}, enc, EncryptionOff},
		{"Problems none: nothing in the period", OverviewInput{Views: twoHealthy(), Latest: snap(2, 0)}, probs, ProblemsNone},
		{"Problems resolved: an outage that ended two hours ago and repaired copies", OverviewInput{
			Views: twoHealthy(), Latest: snap(2, 0),
			Problems: []history.Problem{
				{Kind: history.ProblemUnreachable, Started: now.Add(-3 * time.Hour), Ended: now.Add(-2 * time.Hour), Tone: history.ToneWarning},
				{Kind: history.ProblemCopiesDamaged, Started: now.Add(-5 * time.Hour), Ended: now.Add(-4 * time.Hour), Count: 3, Resolved: 3},
			},
		}, probs, ProblemsResolved},
		{
			"Problems resolved: an acknowledged finding waits for no act",
			OverviewInput{
				Views: twoHealthy(), Latest: snap(2, 0),
				Problems: []history.Problem{{Kind: history.ProblemCopiesDamaged, Started: now.Add(-5 * time.Hour), Count: 1, Acknowledged: 1}},
			},
			probs, ProblemsResolved,
		},
		{"Problems open: a finding neither resolved nor acknowledged", OverviewInput{
			Views: twoHealthy(), Latest: snap(2, 0),
			Problems: []history.Problem{{Kind: history.ProblemCopiesDamaged, Started: now.Add(-5 * time.Hour), Count: 1, Open: 1}},
		}, probs, ProblemsOpen},
		{
			"Problems ongoing: an outage that ended within the hour",
			OverviewInput{
				Views: twoHealthy(), Latest: snap(2, 0),
				Problems: []history.Problem{{Kind: history.ProblemUnreachable, Started: now.Add(-2 * time.Hour), Ended: now.Add(-30 * time.Minute)}},
			},
			probs, ProblemsOngoing,
		},
		{"Problems ongoing: refused uploads in the current hour", OverviewInput{
			Views: twoHealthy(), Latest: snap(2, 0),
			Problems: []history.Problem{{Kind: history.ProblemUploadsRefused, Started: now.Add(-20 * time.Minute)}},
		}, probs, ProblemsOngoing},
		{
			"Problems ongoing: an unresolved finding no other copy held",
			OverviewInput{
				Views: twoHealthy(), Latest: snap(2, 0),
				Problems: []history.Problem{{Kind: history.ProblemCopiesDamaged, Started: now.Add(-50 * time.Hour), Open: 1, Unavailable: 1, UnavailableOpen: 1}},
			},
			probs, ProblemsOngoing,
		},
		{"Problems ongoing: a connection unreachable now", OverviewInput{Views: []View{
			overviewView("default", figures(4, nil)), overviewView("archive", func(v *View) { v.CircuitOpen = 1 }),
		}}, probs, ProblemsOngoing},
		{"Integrity verified: nothing damaged, every copy read within the period", OverviewInput{Views: twoHealthy(), Latest: snap(2, 0)}, integ, IntegrityVerified},
		{"Integrity checking: coverage below 1, nothing overdue", OverviewInput{Views: []View{
			overviewView("default", figures(4, func(c *Connection) { c.VerifiedWithinPeriod = 3 })),
		}, Latest: snap(1, 0)}, integ, IntegrityChecking},
		{"Integrity checking: no snapshot yet", OverviewInput{Views: twoHealthy()}, integ, IntegrityChecking},
		{"Integrity behind: overdue copies", OverviewInput{Views: []View{
			overviewView("default", figures(4, func(c *Connection) { c.VerifiedWithinPeriod, c.ScrubOverdue = 3, 1 })),
		}, Latest: snap(1, 0)}, integ, IntegrityBehind},
		{"Integrity repairing: a damaged copy, the blob whole elsewhere", OverviewInput{Views: []View{
			overviewView("default", figures(4, func(c *Connection) { c.CorruptCopies = 1 })),
		}, Latest: snap(1, 0)}, integ, IntegrityRepairing},
		{"Integrity damaged: a blob without a whole copy", OverviewInput{Views: twoHealthy(), Latest: func() *Summary {
			s := snap(2, 0)
			s.Damaged.Missing = 1
			return s
		}()}, integ, IntegrityDamaged},
	}
	for _, c := range cases {
		t.Run("R-STOR-107 "+c.name, func(t *testing.T) {
			if got := c.check(DeriveOverview(c.in, now)); got != c.want {
				t.Errorf("state %s, want %s", got, c.want)
			}
		})
	}

	t.Run("R-STOR-107 the figures: reachable counts, the loss per connection, the encrypted share, the coverage and the snapshot's time", func(t *testing.T) {
		in := OverviewInput{
			Views: []View{
				overviewView("default", sole),
				overviewView("archive", both(figures(2, func(c *Connection) { c.VerifiedWithinPeriod = 1 }), func(v *View) { v.Encrypted, v.CircuitOpen = true, 1 })),
				overviewView("old", func(v *View) { at := now; v.RetiredAt = &at }),
			},
			Latest: snap(2, 2), Stored: Blobs{Count: 4, Bytes: 400}, MinCopies: 1, DownloadVerify: true,
		}
		o := DeriveOverview(in, now)
		if o.Connections.Configured != 2 || o.Connections.Reachable != 1 || len(o.Connections.Unreachable) != 1 ||
			o.Connections.Unreachable[0] != (Unreachable{ID: "archive", CircuitOpen: 1, Configured: 2}) {
			t.Errorf("connections = %+v", o.Connections)
		}
		if len(o.Availability.Loss) != 1 || o.Availability.Loss[0] != (Loss{Connection: "default", Blobs: 2, Bytes: 200}) || o.Availability.MinCopies != 1 ||
			o.Availability.Target != 2 || o.Availability.BelowTarget.Blobs != 2 {
			t.Errorf("availability = %+v", o.Availability)
		}
		if o.Encryption.EncryptedShare != 0.3333 || len(o.Encryption.Encrypted) != 1 || len(o.Encryption.Plain) != 1 {
			t.Errorf("encryption = %+v (the retired connection counts nowhere)", o.Encryption)
		}
		if o.Integrity.Scrub.Coverage != 0.8333 || !o.Integrity.DownloadVerify || len(o.Integrity.AuthenticatedReads) != 1 || o.Stored.Count != 4 {
			t.Errorf("integrity = %+v, stored = %+v", o.Integrity, o.Stored)
		}
		if o.FiguresAt == nil || !o.FiguresAt.Equal(now.Add(-20*time.Minute)) || !o.At.Equal(now) {
			t.Errorf("times = %v %v", o.FiguresAt, o.At)
		}
		if none := DeriveOverview(OverviewInput{}, now); none.FiguresAt != nil || none.Integrity.Scrub.Coverage != 1 || none.Encryption.EncryptedShare != 0 {
			t.Errorf("empty = %+v", none)
		}
	})
}

// applied gives a view the server-side encryption its store reported for the start check's probe and the mode its element asks for.
func applied(algorithm, mode string) func(*View) {
	return func(v *View) {
		v.Local = &Local{Settings: Settings{SSEMode: mode}}
		v.Capabilities = &blob.Capabilities{SSEApplied: &blob.SSEApplied{Algorithm: algorithm}}
	}
}

func TestR_STOR_111_TheWorstProblemOfThePeriod(t *testing.T) {
	now := time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC)
	outage := history.Problem{
		Kind: history.ProblemUnreachable, Connection: "archive", Started: now.Add(-48 * time.Hour), Ended: now.Add(-47 * time.Hour),
		Seconds: 3600, Tone: history.ToneWarning,
	}
	longer := outage
	longer.Started, longer.Seconds = now.Add(-30*time.Hour), 7200
	refused := history.Problem{
		Kind: history.ProblemUploadsRefused, Connection: "default", Started: now.Add(-72 * time.Hour), Ended: now.Add(-71 * time.Hour),
		Seconds: 3600, Count: 2, Tone: history.ToneError,
	}
	o := DeriveOverview(OverviewInput{Period: "30d", Problems: []history.Problem{outage, longer}}, now)
	if o.Problems.Worst == nil || !o.Problems.Worst.Started.Equal(longer.Started) || o.Problems.Count != 2 || o.Problems.Period != "30d" {
		t.Errorf("the longer of two warnings: %+v", o.Problems)
	}
	o = DeriveOverview(OverviewInput{Problems: []history.Problem{outage, longer, refused}}, now)
	if o.Problems.Worst == nil || o.Problems.Worst.Kind != history.ProblemUploadsRefused {
		t.Errorf("an error before a longer warning: %+v", o.Problems.Worst)
	}
	same := outage
	same.Started = outage.Started.Add(time.Minute)
	if o := DeriveOverview(OverviewInput{Problems: []history.Problem{outage, same}}, now); !o.Problems.Worst.Started.Equal(same.Started) {
		t.Errorf("of two equal ones the newer: %+v", o.Problems.Worst)
	}
	if o := DeriveOverview(OverviewInput{}, now); o.Problems.Worst != nil || o.Problems.State != ProblemsNone {
		t.Errorf("empty: %+v", o.Problems)
	}
}

func conn(o Overview) string  { return o.Connections.State }
func probs(o Overview) string { return o.Problems.State }
func avail(o Overview) string { return o.Availability.State }
func enc(o Overview) string   { return o.Encryption.State }
func integ(o Overview) string { return o.Integrity.State }
