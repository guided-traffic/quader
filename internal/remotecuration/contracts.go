package remotecuration

import (
	"context"
	"time"
)

// Mode is a remote repository's delivery mode, the member remote.delivery.mode of its document (R-CUR-001, Q-CUR-2).
type Mode string

// The delivery modes.
const (
	// AllExceptBlocked delivers every package and version unless a rule blocks it; the default.
	AllExceptBlocked Mode = "allExceptBlocked"
	// OnlyAllowed delivers nothing unless a rule allows it.
	OnlyAllowed Mode = "onlyAllowed"
)

// Valid reports whether m is one of the two modes.
func (m Mode) Valid() bool { return m == AllExceptBlocked || m == OnlyAllowed }

// RuleKind is the subject of a delivery rule (R-CUR-001): the narrowest kind that matches decides.
type RuleKind string

// The rule kinds, from the widest to the narrowest.
const (
	// KindSection holds a name pattern of R-AUTHZ-005 that matches package names.
	KindSection RuleKind = "section"
	// KindPackage holds one normalized package name: every version, future ones included.
	KindPackage RuleKind = "package"
	// KindVersion holds a name and a version in the format's canonical form (R-CUR-006).
	KindVersion RuleKind = "version"
)

// Effect is what a rule does.
type Effect string

// The effects. At equal width a block wins (R-CUR-001 step 2).
const (
	Allow Effect = "allow"
	Block Effect = "block"
)

// Reason says why the rules do not deliver a version: the listing's unavailableReason and the catalogue's delivery state (R-CUR-009,
// R-CUR-014, R-API-081).
type Reason string

// The reasons.
const (
	// ReasonNone: delivered.
	ReasonNone Reason = ""
	// ReasonBlocked: a rule with the effect block decided.
	ReasonBlocked Reason = "blocked"
	// ReasonNotAllowed: no rule matched and the mode is onlyAllowed.
	ReasonNotAllowed Reason = "notAllowed"
)

// DecidedBy names what made a decision, as delivery-rules/check answers it (R-CUR-006): a rule (its kind, its subject and its effect),
// or the mode when no rule matched (Kind empty, Mode set).
type DecidedBy struct {
	Kind    RuleKind
	Subject string
	Effect  Effect
	Mode    Mode
}

// Decision is the answer of the rules for a package name and, where one is asked, a version (R-CUR-001).
type Decision struct {
	Delivered bool
	Reason    Reason
	By        DecidedBy
}

// Delivered is the decision of a repository without rules in the mode allExceptBlocked: what the stub answers.
func Delivered() Decision {
	return Decision{Delivered: true, By: DecidedBy{Mode: AllExceptBlocked}}
}

// Decider is the decision of R-CUR-001 on a repository's rules as the replica's snapshot holds them (R-CUR-003). It is asked before a
// fill, at the serve point, in the format's documents, by the mirror's pass and by the catalogue's rows, and reads no database: the
// rules are in the snapshot (R-CUR-006). repoID is the repository's ID as the repository model spells it; name is the package name and
// version the version, each in any form the format reads, which the decider folds by the format's rules (catalog.Names, the version
// canon) before it compares. repository.Decider implements it over the snapshot (R2); [DeliverAll] answers where none is wired.
type Decider interface {
	// Decide decides one version of a package: steps 1 to 3 of R-CUR-001.
	Decide(ctx context.Context, repoID, name, version string) Decision
	// Package decides a package as a whole, for a request that names no version (a package document, a version or tag list):
	// delivered when the package level allows it or a version rule of the package allows (R-CUR-001, "the package's decision").
	Package(ctx context.Context, repoID, name string) Decision
}

// DeliverAll is the stub [Decider]: every package and version is delivered, as a repository without rules in the default mode.
type DeliverAll struct{}

// Decide answers delivered.
func (DeliverAll) Decide(context.Context, string, string, string) Decision { return Delivered() }

// Package answers delivered.
func (DeliverAll) Package(context.Context, string, string) Decision { return Delivered() }

// State is the state of a row of a repository's download queue (R-CUR-011).
type State string

// The states of a row.
const (
	StateWaiting   State = "waiting"
	StateRunning   State = "running"
	StateDone      State = "done"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Active reports whether a row in the state takes the repository's one active row of its version (remote_download_one_active).
func (s State) Active() bool { return s == StateWaiting || s == StateRunning }

// Origin is what caused a row of the queue (R-CUR-011).
type Origin string

// The origins, in the order the queue runs waiting rows: a person's and a request's before a bulk's and a probe's.
const (
	OriginPerson  Origin = "person"
	OriginRequest Origin = "request"
	OriginAllow   Origin = "allow"
	OriginBulk    Origin = "bulk"
	OriginProbe   Origin = "probe"
	OriginClient  Origin = "client"
)

// Outcome is how a done row ended (R-CUR-011).
type Outcome string

// The outcomes of a done row.
const (
	OutcomeCompleted Outcome = "completed"
	OutcomeBounded   Outcome = "bounded"
	OutcomeRefused   Outcome = "refused"
	OutcomeSkipped   Outcome = "skipped"
)

// FailureClass is the closed list of the member class of a row's failure (R-CUR-011, Q-CUR-20).
type FailureClass string

// The failure classes.
const (
	ClassUpstreamNotFound    FailureClass = "upstream_not_found"
	ClassUpstreamAuth        FailureClass = "upstream_auth"
	ClassUpstreamRateLimited FailureClass = "upstream_rate_limited"
	ClassUpstreamError       FailureClass = "upstream_error"
	ClassUpstreamUnexpected  FailureClass = "upstream_unexpected"
	ClassNetwork             FailureClass = "network"
	ClassTLS                 FailureClass = "tls"
	ClassEgressDenied        FailureClass = "egress_denied"
	ClassIntegrity           FailureClass = "integrity"
	ClassContentRefused      FailureClass = "content_refused"
	ClassGateRefused         FailureClass = "gate_refused"
	ClassCircuitOpen         FailureClass = "circuit_open"
	ClassBound               FailureClass = "bound"
	ClassStorage             FailureClass = "storage"
	ClassInternal            FailureClass = "internal"
)

// FailureClasses lists the classes in the order of R-CUR-011, for the API's filter and its document.
func FailureClasses() []FailureClass {
	return []FailureClass{
		ClassUpstreamNotFound, ClassUpstreamAuth, ClassUpstreamRateLimited, ClassUpstreamError, ClassUpstreamUnexpected, ClassNetwork,
		ClassTLS, ClassEgressDenied, ClassIntegrity, ClassContentRefused, ClassGateRefused, ClassCircuitOpen, ClassBound, ClassStorage,
		ClassInternal,
	}
}

// Retryable reports whether a later attempt can overcome a failure of the class (R-CUR-011 "Retries"); a failure of any other class
// ends the row failed at once.
func (c FailureClass) Retryable() bool {
	switch c {
	case ClassUpstreamRateLimited, ClassUpstreamError, ClassNetwork, ClassCircuitOpen, ClassStorage, ClassInternal:
		return true
	}
	return false
}

// FailureStep is the closed list of the member step of a row's failure: where the attempt failed (R-CUR-011).
type FailureStep string

// The steps of an attempt, in their order.
const (
	StepPlan    FailureStep = "plan"
	StepResolve FailureStep = "resolve"
	StepConnect FailureStep = "connect"
	StepTLS     FailureStep = "tls"
	StepRequest FailureStep = "request"
	StepStatus  FailureStep = "status"
	StepBody    FailureStep = "body"
	StepVerify  FailureStep = "verify"
	StepGate    FailureStep = "gate"
	StepStore   FailureStep = "store"
)

// Failure is the failure of a row's attempt as the column remote_download.failure and the API hold it, every member a script can
// filter on (R-CUR-011, Q-CUR-20). Address has no query: a signed address of a CDN carries a credential in it; Target is the address
// the upstream named and Quader refused to ask (foreign_host_denied, insecure_redirect), without its query as well. Excerpt is at
// most 512 bytes of printable ASCII. Detail is an English sentence for a script and an administrator; a screen writes its own
// sentence from Code and the other members (Q-CUR-169).
type Failure struct {
	Class      FailureClass `json:"class"`
	Step       FailureStep  `json:"step"`
	Code       string       `json:"code"`
	File       string       `json:"file,omitempty"`
	Address    string       `json:"address,omitempty"`
	Target     string       `json:"target,omitempty"`
	Status     int          `json:"status,omitempty"`
	Excerpt    string       `json:"excerpt,omitempty"`
	RetryAfter int          `json:"retryAfter,omitempty"`
	Detail     string       `json:"detail"`
	Attempt    int          `json:"attempt"`
	At         time.Time    `json:"at"`
}

// QueuedDownload is what a row that waits or runs tells about the version it fetches: the catalogue's state queued of a version
// (R-CUR-009) and the examination of a request (R-CUR-015).
type QueuedDownload struct {
	ID          string
	Version     string
	State       State
	Origin      Origin
	Attempts    int
	MaxAttempts int
	// NextAttemptAt is the row's not_before; zero when it waits for a free place only.
	NextAttemptAt time.Time
}

// Queue answers which versions of a package have a row that waits or runs in a repository's queue (R-CUR-011). versions are canonical
// (R-CUR-006); the answer holds an entry per version that has such a row, keyed by the version ("" for a file of no version). The
// queue package implements it (R4); until then [NoQueue] answers.
type Queue interface {
	Active(ctx context.Context, repoID, name string, versions []string) (map[string]QueuedDownload, error)
}

// NoQueue is the stub [Queue]: nothing waits or runs.
type NoQueue struct{}

// Active answers nothing.
func (NoQueue) Active(context.Context, string, string, []string) (map[string]QueuedDownload, error) {
	return map[string]QueuedDownload{}, nil
}

// RequestState is the state of a request entry (R-CUR-015).
type RequestState string

// The states of a request entry.
const (
	RequestOpen      RequestState = "open"
	RequestAllowed   RequestState = "allowed"
	RequestDeclined  RequestState = "declined"
	RequestWithdrawn RequestState = "withdrawn"
)

// OpenRequest is what an open request entry tells about its subject: the catalogue's state request of a package or a version
// (R-CUR-009).
type OpenRequest struct {
	ID string
	// Version is "" for the package with its future versions.
	Version  string
	Askers   int
	Refusals int64
}

// Requests answers which subjects of a package have an open request entry in a repository (R-CUR-015): an entry per subject, keyed by
// the version, "" for the package. versions are canonical (R-CUR-006); the package's own entry is answered whatever versions holds.
// requests.Service implements it (R2b); [NoRequests] answers where none is wired.
type Requests interface {
	Open(ctx context.Context, repoID, name string, versions []string) (map[string]OpenRequest, error)
}

// NoRequests is the stub [Requests]: nothing is requested.
type NoRequests struct{}

// Open answers nothing.
func (NoRequests) Open(context.Context, string, string, []string) (map[string]OpenRequest, error) {
	return map[string]OpenRequest{}, nil
}
