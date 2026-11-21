package gate

import (
	"github.com/guided-traffic/quader/internal/signatures"
)

// Reason is why a gate refuses an artifact (R-SIG-062, R-SIG-068): the member reason of the answer, the label of the metric and the
// column of a refused download.
type Reason string

// The reasons, in the order a refusal names the first one a counting signature gives (R-SIG-062); Missing when none gives any.
const (
	ReasonBlocked    Reason = "blocked"
	ReasonInvalid    Reason = "invalid"
	ReasonNotChosen  Reason = "not_chosen"
	ReasonUntrusted  Reason = "untrusted"
	ReasonNotChecked Reason = "not_checked"
	ReasonMissing    Reason = "missing"
)

// reasonOrder is the order of R-SIG-062.
func reasonOrder() []Reason {
	return []Reason{ReasonBlocked, ReasonInvalid, ReasonNotChosen, ReasonUntrusted, ReasonNotChecked}
}

// Outcome is a gate's decision.
type Outcome uint8

const (
	// Pass serves the artifact.
	Pass Outcome = iota
	// Pending waits: nothing passes and a check or a fetch is due (R-SIG-067).
	Pending
	// Refused refuses it with a Reason.
	Refused
)

// Row is one counting signature (the roles publisher and provenance) of a subject attached in a gate's view, as the store reads it
// (R-SIG-066): the integrity state, what a block matches it by, and its trust row in the gate's scope.
type Row struct {
	// ID is the signature's id: what passed an artifact (the member gate of R-SIG-073).
	ID         int64
	Subject    [32]byte
	State      signatures.State
	KeyIDs     []string
	OIDCIssuer string
	Identity   string
	// Issued marks a signature Quader made itself (signatures.identity_id, stage 2): its npm registry signature or its receipt attestation.
	// It never passes, whatever its role or the anchor its chain ends in (R-SIG-062): Quader signs everything it receives.
	Issued bool
	// HasTrust is false while the scope has no trust row for the signature yet.
	HasTrust     bool
	TrustState   string
	TrustAnchors []int64
	TrustEpoch   int64
}

// Fetch is the state of the background fetch of a subject's signatures in a remote repository of a gate's view (R-SIG-067).
type Fetch struct {
	RepoID  int64
	Target  string
	Subject [32]byte
	Queued  bool
	Fetched bool
	Failed  bool
}

// Judgement is one gate's decision of one artifact. A pass says what passed it: a release of the gate's repository (Released), or the
// counting signature of the id Signature.
type Judgement struct {
	Outcome   Outcome
	Reason    Reason
	Released  bool
	Signature int64
}

// Judge decides one gate (the table of R-SIG-062) over the rows and fetch states of its view for the digests of one artifact: the item's
// blob and the digests that cover it (R-SIG-063). One passing signature of one digest passes the artifact, as one decides the badge;
// nothing passing and something pending waits; nothing passing and nothing pending refuses with the first reason of R-SIG-062's order a
// signature gives, else missing. A signature Quader issued itself does not count (Row.Issued). A release of the gate's repository passes
// whatever the signatures say (R-SIG-070). chosen are the IDs of the anchors the setting accepts, nil for any anchor.
func Judge(snap *Snapshot, chosen map[int64]bool, digests [][32]byte, rows []Row, fetches []Fetch, released bool) Judgement {
	if released {
		return Judgement{Outcome: Pass, Released: true}
	}
	seen := map[Reason]bool{}
	pending := false
	for _, d := range digests {
		for _, r := range rows {
			if r.Subject != d || r.Issued {
				continue
			}
			switch o, reason := judgeRow(snap, chosen, r); o {
			case Pass:
				return Judgement{Outcome: Pass, Signature: r.ID}
			case Pending:
				pending = true
			default:
				seen[reason] = true
			}
		}
		switch fetchState(fetches, d) {
		case fetchDue:
			pending = true
		case fetchFailed:
			seen[ReasonNotChecked] = true
		}
	}
	if pending {
		return Judgement{Outcome: Pending}
	}
	for _, r := range reasonOrder() {
		if seen[r] {
			return Judgement{Outcome: Refused, Reason: r}
		}
	}
	return Judgement{Outcome: Refused, Reason: ReasonMissing}
}

// judgeRow is one row of the table of R-SIG-062.
func judgeRow(snap *Snapshot, chosen map[int64]bool, r Row) (Outcome, Reason) {
	switch r.State {
	case signatures.StatePending:
		return Pending, ""
	case signatures.StateInvalid:
		return Refused, ReasonInvalid
	case signatures.StateNoKey:
		return Refused, ReasonUntrusted
	case signatures.StateValid:
	default: // unsupported, malformed, failed
		return Refused, ReasonNotChecked
	}
	if !r.HasTrust {
		return Pending, ""
	}
	state, anchors := snap.Subtract(r)
	switch state {
	case TrustBlocked:
		return Refused, ReasonBlocked
	case TrustUntrusted:
		return Refused, ReasonUntrusted
	}
	if chosen == nil {
		return Pass, ""
	}
	for _, id := range anchors {
		if chosen[id] {
			return Pass, ""
		}
	}
	return Refused, ReasonNotChosen
}

type fetchStatus uint8

const (
	fetchNone fetchStatus = iota
	fetchDone
	fetchDue
	fetchFailed
)

// fetchState reads the rows of a subject (R-SIG-067): fetched when a row has fetched_at, also while a refresh queued it again; due or
// running when none has and one is queued; failed when none has either and one failed; not fetched otherwise, and the gate then judges
// what clients brought.
func fetchState(fetches []Fetch, subject [32]byte) fetchStatus {
	queued, failed := false, false
	for _, f := range fetches {
		if f.Subject != subject {
			continue
		}
		switch {
		case f.Fetched:
			return fetchDone
		case f.Queued:
			queued = true
		case f.Failed:
			failed = true
		}
	}
	switch {
	case queued:
		return fetchDue
	case failed:
		return fetchFailed
	}
	return fetchNone
}
