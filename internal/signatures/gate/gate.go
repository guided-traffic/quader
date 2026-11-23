// Package gate is the gate per repository of the signatures, stage 3 of signatures.md (R-SIG-061 to R-SIG-069, R-SIG-075, R-SIG-076):
// a repository whose member signatures.onlyVerified is on serves an artifact only when a signature of its publisher or its build is
// attached where the repository sees it, matches the content and is known to a trust anchor the repository accepts.
//
// The package holds the replica's snapshot of anchors and blocks and the subtraction of a narrowing from an older trust row
// (snapshot.go), the decision of one gate over stored rows (decide.go), the gate of the serve points with its answers, its bounded wait,
// its audit event and its metrics (gate.go, wait.go, metrics.go), and the one decision and one wait of the three checks of a serve point
// (serve.go). It runs no SQL itself: the store of the signatures implements [Store], and the format host names what the gate judges of
// an item ([Classifier]). The setting's order and replay are internal/signatures/gateset.
package gate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// Kind is what the gate judges a stored item as, as the item's format names it (R-SIG-063; formats.GateKind at the format contract).
type Kind uint8

const (
	// KindNone is no artifact of its own: passed.
	KindNone Kind = iota
	// KindArtifact is judged by its blob and the digests that cover it.
	KindArtifact
	// KindAttachment is a carrier of signatures the format recognised by its content: passed.
	KindAttachment
	// KindSignatureFile is passed while it is the object of a signature whose scheme was read, and judged as an artifact otherwise.
	KindSignatureFile
)

// Subject is what the format says of an item.
type Subject struct {
	Kind     Kind
	Covering [][32]byte
}

// Item is a stored item the gate asks the format about: its canonical path and its blob.
type Item struct {
	Path string
	Blob [32]byte
}

// Classifier asks the items' format what the gate judges of each, in one call for the items of one repository
// (formats.SignatureAdapter.Gate, through the format host): a serve asks for one item, a preview for a batch (R-SIG-072). An answer of
// another length than the items is read as every item an artifact (fail closed).
type Classifier func(ctx context.Context, repo Repo, items []Item) ([]Subject, error)

// Repo is a repository as the serve point hands it to the gate, from the replica's snapshot of the repositories.
type Repo struct {
	ID     int64
	Name   qname.Repo
	Class  string
	Format string
	// ProjectID is the uuid of the repository's project, whose trust rows the gate reads (R-SIG-065); "" for the installation's scope
	// (a repository of upstream).
	ProjectID string
	// System marks a system repository of upstream: never gated (R-SIG-064, Q-MAL-100's rule).
	System  bool
	Setting Setting
}

const classRemote = "remote"

func (r Repo) gated() bool { return r.Setting.OnlyVerified && !r.System }

// Store is what the gate reads and writes; internal/signatures/store implements it.
type Store interface {
	// GateRows reads the counting signatures of the subjects attached in the repositories, with their trust row in the scope (R-SIG-066).
	GateRows(ctx context.Context, projectID string, repoIDs []int64, subjects [][32]byte) ([]Row, error)
	// GateFetches reads the state of the background fetch of the subjects in the remote repositories (R-SIG-067).
	GateFetches(ctx context.Context, repoIDs []int64, subjects [][32]byte) ([]Fetch, error)
	// GateObjectsRead answers those of the digests that are the object of a signature whose scheme was read, within maxSize (R-SIG-063).
	GateObjectsRead(ctx context.Context, digests [][32]byte, maxSize int64) (map[[32]byte]bool, error)
	// GateSnapshot reads the anchors, the blocks and the switch (R-SIG-066).
	GateSnapshot(ctx context.Context) (*Snapshot, error)
	// GatePrioritize is the priority work of a waiting download (R-SIG-067): the subjects' pending signatures due at priority 0 now,
	// the waiting jobs signature-fetch of the targets first, and the job signature-verify woken.
	GatePrioritize(ctx context.Context, subjects [][32]byte, fetches []Fetch) error
	// RecordRefusal writes the refused download (R-SIG-073).
	RecordRefusal(ctx context.Context, repoID int64, subject [32]byte, path string, reason Reason) error
	// GateReleased answers those of the subjects a release passes in the repository (R-SIG-070).
	GateReleased(ctx context.Context, repoID int64, subjects [][32]byte) (map[[32]byte]bool, error)
	// GateItems reads one batch of the stored files of the repositories after the position, in the order of repository and path, for a
	// preview (R-SIG-072); generated items are not read.
	GateItems(ctx context.Context, repoIDs []int64, after ItemCursor, limit int) ([]StoredItem, error)
	// GateImpact reads the candidates of an impact count (R-SIG-071): the artifacts attached in the repositories whose counting
	// signatures the change can take the state from, at most limit.
	GateImpact(ctx context.Context, q ImpactQuery, repoIDs []int64, limit int) ([]Candidate, error)
}

// Options configure [New].
type Options struct {
	Store Store
	// Enabled is signatures.enabled: with the signatures off every gate is off (R-SIG-064, H-1198).
	Enabled bool
	// Classify names what the gate judges of an item; nil judges every item as an artifact.
	Classify Classifier
	// Wait is signatures.gate.download_wait, counted from the request's arrival; MaxWaiting is signatures.gate.max_waiting.
	Wait       time.Duration
	MaxWaiting int
	// MaxObjectSize is signatures.max_object_size: a signature file above it is judged as an artifact.
	MaxObjectSize int64
	// Poll is how often a waiting download reads again without a signal (R-SIG-067: once a second); zero is one second.
	Poll time.Duration
	// RetryAfter is the Retry-After of the pending answer; zero is 30 seconds.
	RetryAfter time.Duration
	// BaseURL is server.base_url, for the address of the file's page in a refusal (R-SIG-068).
	BaseURL string
	// WakeQueues kicks the job workers of the queues after the priority work (the signal area jobs); nil kicks none, and the workers
	// find the work at their next poll.
	WakeQueues func(queues ...string)
	Metrics    *Metrics
	Audit      audit.Recorder
	Now        func() time.Time
	Log        *slog.Logger
}

// Gate is the gate of the serve points. It is safe for concurrent use.
type Gate struct {
	o     Options
	snap  atomic.Pointer[Snapshot]
	slots chan struct{}
	stop  chan struct{}
	once  sync.Once

	mu      sync.Mutex
	waiting map[[32]byte]map[chan struct{}]struct{}
	seen    map[string]time.Time
}

// The bounds of the audit dedup (R-SIG-075: at most once a minute per repository, path and client address on each replica).
const (
	dedupWindow = time.Minute
	maxTracked  = 10000
)

// The queues the priority work kicks (R-SIG-011, R-SIG-026).
const (
	queueSignatures = "signatures"
	queueIndex      = "index"
)

// New creates the gate.
func New(o Options) *Gate {
	if o.Poll <= 0 {
		o.Poll = time.Second
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = 30 * time.Second
	}
	if o.MaxWaiting <= 0 {
		o.MaxWaiting = 512
	}
	if o.MaxObjectSize <= 0 {
		o.MaxObjectSize = 256 << 10
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Audit == nil {
		o.Audit = audit.Nop{}
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Gate{
		o: o, slots: make(chan struct{}, o.MaxWaiting), stop: make(chan struct{}),
		waiting: map[[32]byte]map[chan struct{}]struct{}{}, seen: map[string]time.Time{},
	}
}

// Enabled reports signatures.enabled; a nil gate is off.
func (g *Gate) Enabled() bool { return g != nil && g.o.Enabled && g.o.Store != nil }

// SetClassifier binds the classification after the gate was built: the format host is built after the signatures' area.
func (g *Gate) SetClassifier(c Classifier) {
	if g != nil {
		g.o.Classify = c
	}
}

// Applies reports whether a gate applies to a download (R-SIG-064, R-SIG-065): the signatures are on and the addressed repository's
// member or the member of the repository that holds the item is on. It reads the snapshot of the repositories only, no row, so an
// ungated download costs nothing.
func (g *Gate) Applies(addressed, holder Repo) bool {
	return g.Enabled() && !holder.System && !addressed.System && (addressed.gated() || holder.gated())
}

// Reload reads the snapshot of anchors, blocks and the switch (R-SIG-066); a failed read keeps the last one.
func (g *Gate) Reload(ctx context.Context) error {
	if !g.Enabled() {
		return nil
	}
	s, err := g.o.Store.GateSnapshot(ctx)
	if err != nil {
		return err
	}
	g.snap.Store(s)
	return nil
}

// snapshot is the replica's snapshot, read once when it has none (fail closed: no snapshot is an error, not a pass).
func (g *Gate) snapshot(ctx context.Context) (*Snapshot, error) {
	if s := g.snap.Load(); s != nil {
		return s, nil
	}
	if err := g.Reload(ctx); err != nil {
		return nil, err
	}
	return g.snap.Load(), nil
}

// Download is one serve of a stored item, as the serve point knows it.
type Download struct {
	// Addressed is the repository the client named; Holder the one that holds the item (Addressed itself, or a member of a virtual
	// Addressed). The answers name Addressed and no other repository (R-SIG-068).
	Addressed Repo
	Holder    Repo
	// Members are, for a virtual Addressed, its members the caller may read: the attachments its own gate reads (R-SIG-065).
	Members []Repo
	// Path is the item's canonical path, Blob its SHA-256. Name is the artifact in its client's words (`unsigned:1.0`,
	// `left-pad@1.3.0`), which the answers name (Q-SIG-213); "" names it by Path.
	Path      string
	Name      string
	Blob      [32]byte
	Generated bool
	// Arrived is when the request arrived; the zero time is now.
	Arrived  time.Time
	Actor    audit.Actor
	Source   audit.Source
	Endpoint string
	// MayWrite, where the serve point sets it, answers whether the caller may write to the repository: a writer reads an artifact of a
	// format whose signing tools read it before they push its signature (OCI) through the gate (Q-SIG-211). It is asked only where the
	// gate would refuse or hold the download, never on the pass path.
	MayWrite func(context.Context) (bool, error)
	// Head says the request is a HEAD: it reads the item's headers and digest, none of its content. The gate decides it as a GET, and a
	// writer's pass of it is served and not recorded by [Gate.WriterRead]: no content the gate holds back from a reader left the
	// repository (Q-SIG-422).
	Head bool
}

// view is one gate that applies: its repository, the repositories whose attachments it reads, and the anchors it accepts.
type view struct {
	repo    Repo
	repoIDs []int64
	remotes []int64
	chosen  []string
	any     bool
}

// Verdict is the gate's decision of a download from what the store holds now; Begin waits for a pending one. Released and Signature
// say what passed it in the addressed repository's gate: a release of that repository, or the counting signature of that id.
type Verdict struct {
	Outcome   Outcome
	Reason    Reason
	Released  bool
	Signature int64
	// Writer says the download passed because the caller may write to the repository and reads to sign (Q-SIG-211); the serve point
	// records it with [Gate.WriterRead] once the download is served (Q-SIG-240).
	Writer bool
	// withheld is what the gate would have answered a reader in place of the writer's pass: a reason of R-SIG-068, or pending.
	withheld Reason
	d        Download
	digests  [][32]byte
	views    []view
	fetches  []Fetch
}

// Decide decides a download (R-SIG-062 to R-SIG-066): nil Verdict.Outcome Pass serves it. It classifies the item by its format, passes
// what is no artifact, and judges each gate that applies (the addressed repository's in its view and scope, the holder's in its own),
// the first refusal or pending first; a release of a repository passes the artifact in that repository's gate (R-SIG-070).
func (g *Gate) Decide(ctx context.Context, d Download) (*Verdict, error) {
	v := &Verdict{d: d}
	if d.Generated || !g.Applies(d.Addressed, d.Holder) {
		return v, nil
	}
	digests, judge, err := g.subject(ctx, d)
	if err != nil || !judge {
		return v, err
	}
	v.digests = digests
	v.views = views(d)
	if err := g.judge(ctx, v); err != nil {
		return v, err
	}
	return v, g.writerRead(ctx, v)
}

// writerRead passes a download the gate would refuse or hold when the caller may write to the repository (Q-SIG-211): the signing tools
// of the format read the artifact before they push its signature, and an image pushed while the gate is on could never be signed
// otherwise. Only the signature gate is passed; the malware gate and the vulnerability block decide on their own.
func (g *Gate) writerRead(ctx context.Context, v *Verdict) error {
	if v.Outcome == Pass || v.d.MayWrite == nil {
		return nil
	}
	ok, err := v.d.MayWrite(ctx)
	if err != nil || !ok {
		return err
	}
	v.withheld = v.Reason
	if v.Outcome == Pending {
		v.withheld = reasonPending
	}
	v.Outcome, v.Reason, v.Writer = Pass, "", true
	return nil
}

// reasonPending is the reason of a pending answer in the events and the metrics (Q-SIG-137).
const reasonPending Reason = "pending"

// WriterRead records a served download that passed the gate only because the caller may write to the repository (Q-SIG-211, Q-SIG-240):
// the metric for each such download, and the event signature.download_by_writer at most once a minute per repository, path, caller and
// client address on each replica, so that a client's repeated GETs of one manifest are one event. A verdict without the writer's pass
// records nothing, and neither does a HEAD (Download.Head): a push's check whether a manifest exists and a signing tool's resolution of a
// tag to its digest read no content the gate holds back (Q-SIG-422). The event is asynchronous, as the refusal's: no statement within the
// request.
func (g *Gate) WriterRead(ctx context.Context, v *Verdict) {
	if g == nil || v == nil || !v.Writer || v.d.Head {
		return
	}
	d := v.d
	g.o.Metrics.writerRead(string(v.withheld))
	caller := d.Actor.Kind + ":" + d.Actor.ID
	if !g.first("writer\x00" + d.Addressed.Name.String() + "\x00" + d.Path + "\x00" + caller + "\x00" + d.Source.ClientAddr) {
		return
	}
	body, _ := json.Marshal(map[string]string{ // strings always marshal
		"repository": d.Addressed.Name.String(), "path": d.Path, "artifact": nameOf(d), "sha256": hex.EncodeToString(d.Blob[:]),
		"reason": string(v.withheld),
	})
	g.o.Audit.Record(ctx, audit.Event{
		Type: audit.EventSignatureDownloadByWriter, Actor: d.Actor, Outcome: audit.Warning, Reason: string(v.withheld), Source: d.Source,
		Subject:    audit.Subject{Kind: "repository", Name: d.Addressed.Name.String()},
		Repository: d.Addressed.Name.String(), Project: d.Addressed.Name.Project, Path: d.Path, Endpoint: d.Endpoint, Details: body,
	})
}

// subject classifies the item: judge is false for an item the gate passes as it is.
func (g *Gate) subject(ctx context.Context, d Download) (digests [][32]byte, judge bool, err error) {
	s := Subject{Kind: KindArtifact}
	if g.o.Classify != nil {
		subs, err := g.o.Classify(ctx, d.Holder, []Item{{Path: d.Path, Blob: d.Blob}})
		if err != nil {
			return nil, false, err
		}
		if len(subs) == 1 {
			s = subs[0]
		}
	}
	switch s.Kind {
	case KindNone, KindAttachment:
		return nil, false, nil
	case KindSignatureFile:
		read, err := g.o.Store.GateObjectsRead(ctx, [][32]byte{d.Blob}, g.o.MaxObjectSize)
		if err != nil {
			return nil, false, err
		}
		if read[d.Blob] {
			return nil, false, nil
		}
		s.Covering = nil
	}
	return append([][32]byte{d.Blob}, s.Covering...), true, nil
}

// views are the gates that apply, the addressed repository's first: a refusal names the addressed repository either way.
func views(d Download) []view {
	var out []view
	add := func(r Repo, seen []Repo) {
		v := view{repo: r, chosen: r.Setting.Anchors, any: len(r.Setting.Anchors) == 0}
		for _, m := range seen {
			v.repoIDs = append(v.repoIDs, m.ID)
			if m.Class == classRemote {
				v.remotes = append(v.remotes, m.ID)
			}
		}
		out = append(out, v)
	}
	if d.Addressed.gated() {
		seen := []Repo{d.Addressed}
		if d.Addressed.Class == "virtual" {
			seen = d.Members
		}
		add(d.Addressed, seen)
	}
	if d.Holder.gated() && d.Holder.ID != d.Addressed.ID {
		add(d.Holder, []Repo{d.Holder})
	}
	return out
}

// judge reads the rows of every view and decides; the first gate that refuses names the reason, else any pending one waits. A release
// is read only for a gate whose signatures do not pass, so a passing download reads no more than without releases (R-SIG-070).
func (g *Gate) judge(ctx context.Context, v *Verdict) error {
	snap, err := g.snapshot(ctx)
	if err != nil {
		return err
	}
	v.Outcome, v.Reason, v.Released, v.Signature, v.fetches = Pass, "", false, 0, nil
	for i, vw := range v.views {
		rows, err := g.o.Store.GateRows(ctx, vw.repo.ProjectID, vw.repoIDs, v.digests)
		if err != nil {
			return err
		}
		var fetches []Fetch
		if len(vw.remotes) > 0 {
			if fetches, err = g.o.Store.GateFetches(ctx, vw.remotes, v.digests); err != nil {
				return err
			}
			v.fetches = append(v.fetches, fetches...)
		}
		var chosen map[int64]bool
		if !vw.any {
			chosen = snap.Chosen(vw.chosen, vw.repo.ProjectID)
		}
		j := Judge(snap, chosen, v.digests, rows, fetches, false)
		if j.Outcome != Pass {
			released, err := g.o.Store.GateReleased(ctx, vw.repo.ID, v.digests)
			if err != nil {
				return err
			}
			if len(released) > 0 {
				j = Judgement{Outcome: Pass, Released: true}
			}
		}
		if i == 0 {
			v.Released, v.Signature = j.Released, j.Signature
		}
		switch j.Outcome {
		case Refused:
			v.Outcome, v.Reason = Refused, j.Reason
			return nil
		case Pending:
			v.Outcome = Pending
		}
	}
	return nil
}

// Refuse records a refusal (the metric, the audit event, the row of refused downloads) and returns its answer, 403
// signature_not_verified with the reason (R-SIG-068).
func (g *Gate) Refuse(ctx context.Context, v *Verdict) error {
	g.o.Metrics.refuse(string(v.Reason))
	g.recordEvent(ctx, v.d, v.Reason)
	if err := g.o.Store.RecordRefusal(ctx, v.d.Addressed.ID, v.d.Blob, v.d.Path, v.Reason); err != nil {
		g.o.Log.WarnContext(ctx, "a refused download was not listed", "repository", v.d.Addressed.Name.String(), "error", err)
	}
	return g.refusal(v.d, v.Reason)
}

// Check decides a download alone and waits for it when it is pending: the serve of a stored document a format serves without the item
// store (a remote OCI repository's tag, R-SIG-064), where no other check of R-SIG-069 is asked.
func (g *Gate) Check(ctx context.Context, d Download) error {
	v, err := g.Decide(ctx, d)
	if err != nil {
		return err
	}
	switch v.Outcome {
	case Pass:
		g.WriterRead(ctx, v)
		return nil
	case Refused:
		return g.Refuse(ctx, v)
	}
	w, err := g.Begin(ctx, v)
	if err != nil {
		return err
	}
	return w.Run(ctx)
}

// Signal is the handler of the signal area signature_verdict (R-SIG-067): the payloads are the hex digests of subjects whose check,
// trust row or fetch committed; a coarse signal (the payloads were dropped, or a trust recomputation) wakes every waiting download.
func (g *Gate) Signal(payloads []string, coarse bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if coarse {
		for _, set := range g.waiting {
			wake(set)
		}
		return
	}
	for _, p := range payloads {
		raw, err := hex.DecodeString(p)
		if err != nil || len(raw) != 32 {
			for _, set := range g.waiting {
				wake(set)
			}
			continue
		}
		wake(g.waiting[[32]byte(raw)])
	}
}

func wake(set map[chan struct{}]struct{}) {
	for ch := range set {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Stop answers every waiting download with the pending answer at once: a replica that drains (R-SIG-067).
func (g *Gate) Stop() {
	if g != nil {
		g.once.Do(func() { close(g.stop) })
	}
}

// Waiting reports how many downloads wait on this replica now.
func (g *Gate) Waiting() int { return len(g.slots) }

// recordEvent writes signature.download_refused, at most once a minute per repository, path and client address (R-SIG-075).
func (g *Gate) recordEvent(ctx context.Context, d Download, reason Reason) {
	key := d.Addressed.Name.String() + "\x00" + d.Path + "\x00" + d.Source.ClientAddr
	if !g.first(key) {
		return
	}
	body, _ := json.Marshal(map[string]string{ // strings always marshal
		"repository": d.Addressed.Name.String(), "path": d.Path, "sha256": hex.EncodeToString(d.Blob[:]), "reason": string(reason),
	})
	g.o.Audit.Record(ctx, audit.Event{
		Type: audit.EventSignatureDownloadRefused, Actor: d.Actor, Outcome: audit.Denied, Reason: string(reason), Source: d.Source,
		Subject:    audit.Subject{Kind: "repository", Name: d.Addressed.Name.String()},
		Repository: d.Addressed.Name.String(), Project: d.Addressed.Name.Project, Path: d.Path, Endpoint: d.Endpoint, Details: body,
	})
}

func (g *Gate) first(key string) bool {
	now := g.o.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if t, ok := g.seen[key]; ok && now.Sub(t) < dedupWindow {
		return false
	}
	if len(g.seen) >= maxTracked {
		for k, t := range g.seen {
			if now.Sub(t) >= dedupWindow {
				delete(g.seen, k)
			}
		}
		if len(g.seen) >= maxTracked {
			clear(g.seen)
		}
	}
	g.seen[key] = now
	return true
}

// The answers (R-SIG-068). Each carries Cache-Control: no-store, so no cache keeps a refusal past a signature that arrives or a change of
// anchors, and the digest; the texts name the addressed repository and no signer.

// Every text speaks of the signature of the artifact's publisher or its build, the only one that counts (R-SIG-062): a registry's
// signature beside it does not pass, so "no signature" would be wrong where one exists (Q-SIG-212).
func reasonText(r Reason) string {
	switch r {
	case ReasonUntrusted:
		return "no trust anchor this repository accepts knows the signer of its publisher's or its build's signature."
	case ReasonNotChosen:
		return "its publisher's or its build's signature is verified only by trust anchors this repository does not accept."
	case ReasonInvalid:
		return "its publisher's or its build's signature does not match its content."
	case ReasonBlocked:
		return "an administrator blocked the signer of its publisher's or its build's signature."
	case ReasonNotChecked:
		return "its publisher's or its build's signature could not be checked: Quader cannot read it, or its check failed."
	}
	return "it has no signature of its publisher or its build, and this repository serves only artifacts with a verified signature."
}

// nameOf is the artifact as the answers name it: the client's words, else the stored path.
func nameOf(d Download) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Path
}

// RefusalText is the text of a refusal of R-SIG-068 without its link; artifact is the artifact in its client's words.
func RefusalText(artifact string, addressed qname.Repo, r Reason) string {
	return oneLine(fmt.Sprintf("Quader refused %s of %s: %s", artifact, addressed, reasonText(r)))
}

func (g *Gate) refusal(d Download, r Reason) error {
	text := RefusalText(nameOf(d), d.Addressed.Name, r) + " Details: " + g.pageOf(d)
	return errs.E(errs.Forbidden, errs.CodeSignatureNotVerified, oneLine(text),
		errs.WithHeader("Cache-Control", "no-store"), errs.WithExtension("reason", string(r)),
		errs.WithExtension("sha256", hex.EncodeToString(d.Blob[:])))
}

// pageOf is the address of the file's page in the UI: the repository's files below the addressed repository (R-SIG-074).
func (g *Gate) pageOf(d Download) string {
	segs := strings.Split(d.Path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.TrimRight(g.o.BaseURL, "/") + "/ui/projects/" + url.PathEscape(d.Addressed.Name.Project) + "/repositories/" +
		url.PathEscape(d.Addressed.Name.Key) + "/files/" + strings.Join(segs, "/")
}

// PendingText is the text of the pending answer of R-SIG-068; artifact is the artifact in its client's words.
func PendingText(artifact string) string {
	return oneLine(fmt.Sprintf("Quader is checking the signatures of %s. Try again in a minute.", artifact))
}

func (g *Gate) pendingError(d Download) error {
	return errs.E(errs.Unavailable, errs.CodeSignatureCheckPending, PendingText(nameOf(d)),
		errs.WithHeader("Cache-Control", "no-store"), errs.WithExtension("sha256", hex.EncodeToString(d.Blob[:])),
		errs.WithRetryAfter(g.o.RetryAfter))
}

// oneLine keeps a text that names a path to one line of plain text.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}
