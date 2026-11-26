package summary

import (
	"context"
	"sort"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The storage overview (storage.md R-STOR-107): the figures of the state now for the top of the storage page and the admin dashboard,
// with every state derived here, once, so that the page and the dashboard cannot word one state twice (Q-STOR-505). It reads what the
// connections' list reads (the rows, the registry, the queue and the latest snapshot), the folded totals and the scrub task's runs; it
// never counts over blobs or copies (R-STOR-107, Q-STOR-504).

// The states of the overview's tiles (Q-STOR-506).
const (
	ConnectionsHealthy     = "healthy"
	ConnectionsDegraded    = "degraded"
	ConnectionsUnreachable = "unreachable"

	AvailabilityRedundant  = "redundant"
	AvailabilityCatchingUp = "catching_up"
	AvailabilityAtRisk     = "at_risk"
	AvailabilityOneStore   = "one_store"

	EncryptionEncrypted    = "encrypted"
	EncryptionPartly       = "partly"
	EncryptionProviderOnly = "provider_only"
	EncryptionOff          = "off"

	IntegrityVerified  = "verified"
	IntegrityChecking  = "checking"
	IntegrityBehind    = "behind"
	IntegrityRepairing = "repairing"
	IntegrityDamaged   = "damaged"
)

// BacklogBehind is the age of a connection's oldest queued gap past which the reconciliation is behind: the hour of the alert
// QuaderStorageReplicationBacklog (R-STOR-056).
const BacklogBehind = time.Hour

// OverviewInput is what the overview is derived from, read at the time of the request.
type OverviewInput struct {
	// Views is the connections' list (List).
	Views []View
	// Latest is the latest snapshot; nil before the first.
	Latest *Summary
	// Stored is the live blobs and their bytes now (Stored).
	Stored Blobs
	// MinCopies is storage.replication.min_copies and DownloadVerify storage.download.verify of the answering replica.
	MinCopies      int
	DownloadVerify bool
	// Scrub is the scrub task's runs.
	Scrub ScrubRuns
}

// ScrubRuns is the scrub task's last finished run, its next scheduled run and whether a run is going on (R-STOR-027).
type ScrubRuns struct {
	LastRunAt, NextRunAt *time.Time
	Running              bool
}

// Overview is the storage overview's state now (R-STOR-107).
type Overview struct {
	At time.Time
	// FiguresAt is the latest snapshot's time, nil before the first: the time of every figure that is not the registry's or the
	// folded totals' (H-2420).
	FiguresAt    *time.Time
	Connections  OverviewConnections
	Stored       Blobs
	Availability OverviewAvailability
	Encryption   OverviewEncryption
	Integrity    OverviewIntegrity
}

// OverviewConnections is the tile Connections: the connections that hold or take content (not retired, not drained).
type OverviewConnections struct {
	State string
	// Configured counts those a live replica configures; Reachable those of them whose circuit is closed on every such replica.
	Configured, Reachable int
	// Unreachable is those with the circuit open on a live replica, Detached those no live replica configures, by id.
	Unreachable []Unreachable
	Detached    []string
}

// Unreachable is a connection whose circuit is open on CircuitOpen of the Configured live replicas that configure it.
type Unreachable struct {
	ID                      string
	CircuitOpen, Configured int
}

// OverviewAvailability is the tile Availability (R-STOR-106): the target, the copies per blob and what the loss of one connection costs.
type OverviewAvailability struct {
	State string
	// Target is the snapshot's target, the live replicas' count of connections without drain before the first snapshot.
	Target, MinCopies int
	PerBlob           []CopyCount
	BelowTarget       Pending
	// Loss is the connections holding sole copies, the most bytes first: the blobs that would be unavailable without each.
	Loss []Loss
}

// Loss is what would be unavailable without one connection: its sole copies' blobs and plain bytes.
type Loss struct {
	Connection   string
	Blobs, Bytes int64
}

// OverviewEncryption is the tile Encryption: which of the connections that hold content Quader encrypts, which ask their store for
// server-side encryption in the answering replica's configuration, and the encrypted share of the stored bytes.
type OverviewEncryption struct {
	State                                string
	Encrypted, Plain, ProviderConfigured []string
	// EncryptedShare is the encrypted connections' stored bytes over all, from the snapshot; 0 to 1.
	EncryptedShare float64
}

// OverviewIntegrity is the tile Integrity: the checks that hold, the scrub's coverage and the damage of the snapshot.
type OverviewIntegrity struct {
	State          string
	DownloadVerify bool
	// AuthenticatedReads is the encrypted connections, whose every read Quader's format authenticates (R-STOR-082).
	AuthenticatedReads []string
	Scrub              OverviewScrub
	// DamagedCopies counts the missing and corrupt copies, DamagedBlobs the live blobs without a whole copy, both from the snapshot.
	DamagedCopies, DamagedBlobs int64
}

// OverviewScrub is the scrub's coverage within the period over every counted connection and its runs.
type OverviewScrub struct {
	// Coverage is the present copies read whole within their connection's period over all present copies, 1 without a copy.
	Coverage float64
	Overdue  int64
	ScrubRuns
}

// Stored reads the live blobs and their bytes now: the folded counter and its unfolded deltas (Q-STOR-504).
func Stored(ctx context.Context, db *postgres.DB) (Blobs, error) {
	var row q.StorageStoredNowRow
	err := db.Read(ctx, func(c q.DBTX) error {
		var err error
		row, err = q.New(c).StorageStoredNow(ctx)
		return err
	})
	return Blobs{Count: row.Blobs, Bytes: row.Bytes}, err
}

// DeriveOverview derives the overview from its input at now (R-STOR-107, Q-STOR-505, Q-STOR-506). A pure function.
func DeriveOverview(in OverviewInput, now time.Time) Overview {
	held := make([]View, 0, len(in.Views))
	for _, v := range in.Views {
		if v.State != StateRetired && v.State != StateDrained {
			held = append(held, v)
		}
	}
	o := Overview{At: now.UTC(), Stored: in.Stored}
	if in.Latest != nil {
		at := in.Latest.ComputedAt.UTC()
		o.FiguresAt = &at
	}
	o.Connections = overviewConnections(held)
	o.Availability = overviewAvailability(held, in, now)
	o.Encryption = overviewEncryption(held)
	o.Integrity = overviewIntegrity(held, in)
	return o
}

// overviewConnections: unreachable when no configured connection is reachable on any replica; degraded when one is unreachable on some
// replica, one is detached, or no live replica configures any; healthy otherwise.
func overviewConnections(held []View) OverviewConnections {
	out := OverviewConnections{Unreachable: []Unreachable{}, Detached: []string{}}
	everyOpen := true
	for _, v := range held {
		if v.State == StateDetached {
			out.Detached = append(out.Detached, v.ID)
			continue
		}
		if v.Configured == 0 {
			continue
		}
		out.Configured++
		if v.CircuitOpen == 0 {
			out.Reachable++
		} else {
			out.Unreachable = append(out.Unreachable, Unreachable{ID: v.ID, CircuitOpen: v.CircuitOpen, Configured: v.Configured})
		}
		if v.CircuitOpen < v.Configured {
			everyOpen = false
		}
	}
	switch {
	case out.Configured > 0 && everyOpen:
		out.State = ConnectionsUnreachable
	case out.Configured == 0 || len(out.Unreachable) > 0 || len(out.Detached) > 0:
		out.State = ConnectionsDegraded
	default:
		out.State = ConnectionsHealthy
	}
	return out
}

// overviewAvailability: one store with one target connection; at risk while sole copies exist and a target connection is unreachable
// on a replica or its reconciliation is behind (the oldest gap older than BacklogBehind, not filling); catching up while a blob is
// below the target or a sole copy exists; redundant otherwise. Before the first snapshot the queue stands in for the figures.
func overviewAvailability(held []View, in OverviewInput, now time.Time) OverviewAvailability {
	out := OverviewAvailability{MinCopies: in.MinCopies, PerBlob: []CopyCount{}, Loss: []Loss{}}
	var targets int
	var unreachable, behind, queued bool
	for _, v := range held {
		if v.State != StateFilling && v.State != StateActive {
			continue
		}
		targets++
		unreachable = unreachable || v.CircuitOpen > 0
		queued = queued || v.Queued > 0
		behind = behind || (v.State == StateActive && v.OldestQueuedAt != nil && now.Sub(*v.OldestQueuedAt) > BacklogBehind)
	}
	out.Target = targets
	var soles int64
	if in.Latest != nil {
		out.Target, out.BelowTarget = in.Latest.Copies.Target, in.Latest.Copies.BelowTarget
		out.PerBlob = append(out.PerBlob, in.Latest.Copies.PerBlob...)
		for _, v := range held {
			if f := v.Figures; f != nil && f.SoleCopies > 0 {
				out.Loss = append(out.Loss, Loss{Connection: v.ID, Blobs: f.SoleCopies, Bytes: f.SoleBytes})
				soles += f.SoleCopies
			}
		}
		sort.SliceStable(out.Loss, func(i, j int) bool {
			if out.Loss[i].Bytes != out.Loss[j].Bytes {
				return out.Loss[i].Bytes > out.Loss[j].Bytes
			}
			return out.Loss[i].Blobs > out.Loss[j].Blobs
		})
	}
	switch {
	case out.Target <= 1:
		out.State = AvailabilityOneStore
	case soles > 0 && (unreachable || behind):
		out.State = AvailabilityAtRisk
	case soles > 0 || out.BelowTarget.Blobs > 0 || (in.Latest == nil && queued):
		out.State = AvailabilityCatchingUp
	default:
		out.State = AvailabilityRedundant
	}
	return out
}

// overviewEncryption: encrypted when Quader encrypts every connection that holds content, partly when some, provider only when none
// but each asks its store for server-side encryption, off otherwise. The server-side mode is the answering replica's configuration; a
// connection it does not configure counts as not asking.
func overviewEncryption(held []View) OverviewEncryption {
	out := OverviewEncryption{Encrypted: []string{}, Plain: []string{}, ProviderConfigured: []string{}}
	var encBytes, allBytes int64
	for _, v := range held {
		if v.Encrypted {
			out.Encrypted = append(out.Encrypted, v.ID)
		} else {
			out.Plain = append(out.Plain, v.ID)
		}
		if v.Local != nil && v.Local.Settings.SSEMode != "" && v.Local.Settings.SSEMode != "none" {
			out.ProviderConfigured = append(out.ProviderConfigured, v.ID)
		}
		if f := v.Figures; f != nil {
			allBytes += f.PresentBytes
			if v.Encrypted {
				encBytes += f.PresentBytes
			}
		}
	}
	switch {
	case len(held) > 0 && len(out.Plain) == 0:
		out.State = EncryptionEncrypted
	case len(out.Encrypted) > 0:
		out.State = EncryptionPartly
	case len(held) > 0 && len(out.ProviderConfigured) == len(held):
		out.State = EncryptionProviderOnly
	default:
		out.State = EncryptionOff
	}
	zero := 0.0
	if out.State == EncryptionEncrypted {
		zero = 1
	}
	out.EncryptedShare = ratio(encBytes, allBytes, zero)
	return out
}

// overviewIntegrity: damaged while a live blob has no whole copy, repairing while a copy is missing or corrupt, behind while copies are
// overdue for the scrub, checking while the coverage is below 1 or no snapshot exists, verified otherwise.
func overviewIntegrity(held []View, in OverviewInput) OverviewIntegrity {
	out := OverviewIntegrity{DownloadVerify: in.DownloadVerify, AuthenticatedReads: []string{}, Scrub: OverviewScrub{ScrubRuns: in.Scrub}}
	var present, verified int64
	for _, v := range held {
		if v.Encrypted {
			out.AuthenticatedReads = append(out.AuthenticatedReads, v.ID)
		}
		if f := v.Figures; f != nil {
			present += f.PresentCopies
			verified += f.VerifiedWithinPeriod
			out.Scrub.Overdue += f.ScrubOverdue
			out.DamagedCopies += f.MissingCopies + f.CorruptCopies
		}
	}
	out.Scrub.Coverage = ratio(verified, present, 1)
	if in.Latest != nil {
		out.DamagedBlobs = in.Latest.Damaged.Missing + in.Latest.Damaged.Corrupt
	}
	switch {
	case out.DamagedBlobs > 0:
		out.State = IntegrityDamaged
	case out.DamagedCopies > 0:
		out.State = IntegrityRepairing
	case out.Scrub.Overdue > 0:
		out.State = IntegrityBehind
	case in.Latest == nil || out.Scrub.Coverage < 1:
		out.State = IntegrityChecking
	default:
		out.State = IntegrityVerified
	}
	return out
}
