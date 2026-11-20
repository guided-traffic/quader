package formathost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/repository"
)

// A probe starts the download (repositories-and-proxy.md R-REPO-065, walk item W-25): a request of a client that names exactly one
// version or one file of a remote repository without fetching its content - the manifest of an OCI tag, an npm version document, a
// HEAD of a file - may start the download of that version's content in the background. The format's handler tells the host of a
// probe it answered (formats.RemoteFetcher.Probed); the host decides on the probe's request and, when everything admits it, queues
// the job remote-background-download, which a worker runs through the same gate and engine as a mirror pass (background_download.go).
//
// The decision never fails, delays or changes the probe's answer: it runs after Probed returned, at most probeDecisions at once per
// replica, each within probeDecisionTimeout.
//
// A probe a virtual repository answered (Q-REPO-367) is the probe of the member that serves its content: the decision first finds that
// member by the tiers of R-REPO-019 step 6 (servingMember), then decides on a request derived for it, so that the member's switch,
// patterns, platforms and bounds apply, while the charge stays the probe's: the budget key resolved at the addressed repository, the
// member's ceiling, the usage recorded at the member (usage-and-limits.md Q-LIM-2, R-LIM-037).

// The outcomes of a probe's decision and of a background download (the label outcome of quader_remote_background_downloads_total).
const (
	// ProbeStarted: the job was queued.
	ProbeStarted = "started"
	// ProbeOff: the repository's switch remote.downloadOnProbe is off, or the repository cannot fetch now (offline, disabled, the
	// cache view, only-if-cached).
	ProbeOff = "off"
	// ProbeStored: every file the probe named is stored already.
	ProbeStored = "stored"
	// ProbeLocal: a local member of the virtual repository the probe addressed holds the version (Q-REPO-367).
	ProbeLocal = "local"
	// ProbeNotAdmitted: the repository's patterns, a global claim or the caller's populate permission refuse the content.
	ProbeNotAdmitted = "not_admitted"
	// ProbeActive: a background download of the version is queued or running.
	ProbeActive = "active"
	// ProbeQueueFull: the repository has limits.maxConcurrentFetches background downloads queued or running.
	ProbeQueueFull = "queue_full"
	// ProbeLimited: step 9a refused the probe's caller in Enforce (usage-and-limits.md R-LIM-037).
	ProbeLimited = "limited"
	// ProbeBusy: the replica takes probeDecisions decisions at once already.
	ProbeBusy = "busy"
	// ProbeFailed: the decision could not be taken (a store or queue error).
	ProbeFailed = "failed"
	// DownloadCompleted, DownloadBounded and DownloadFailed are the outcomes of a job: everything stored, cut at a bound, failed
	// after its last attempt.
	DownloadCompleted = "completed"
	DownloadBounded   = "bounded"
	DownloadFailed    = "failed_download"
)

const (
	// probeDecisions bounds the decisions a replica takes at once; a probe beyond them starts nothing (outcome busy).
	probeDecisions = 16
	// probeDecisionTimeout bounds one decision: the lookups of what is stored, the queue, step 9a and the enqueue.
	probeDecisionTimeout = 10 * time.Second
)

// BackgroundDownloads is the queue of the jobs remote-background-download as the decision on a probe uses it; the composition root
// builds it over internal/jobs (R-REPO-065).
type BackgroundDownloads interface {
	// Busy reports whether a background download with the dedup key is queued or running, and how many of the repository are.
	Busy(ctx context.Context, key string, repoID int64) (active bool, ofRepository int, err error)
	// Enqueue queues the job; created is false when one with the same key was queued meanwhile.
	Enqueue(ctx context.Context, a BackgroundDownload) (created bool, err error)
}

// BackgroundDownload are the arguments of a job remote-background-download: the repository, the probe and the ticket of the limits
// the download is charged with (usage-and-limits.md R-LIM-037).
type BackgroundDownload struct {
	RepoID  int64    `json:"repoId"`
	Name    string   `json:"name"`
	Version string   `json:"version,omitempty"`
	Files   []string `json:"files"`
	// Document and Digest are the probe's metadata entry and the hex of what it held (formats.Probe).
	Document string        `json:"document,omitempty"`
	Digest   string        `json:"digest,omitempty"`
	Ticket   limits.Ticket `json:"ticket"`
}

// Key is the dedup key of the job: one background download of a version of a repository at a time, across replicas (R-REPO-065).
// A probe without a version (a HEAD of an OCI blob) is keyed by its file.
func (a BackgroundDownload) Key() string {
	what := a.Version
	if what == "" && len(a.Files) > 0 {
		what = a.Files[0]
	}
	sum := sha256.Sum256([]byte(a.Name + "\x00" + what))
	return BackgroundDownloadKind + ":" + strconv.FormatInt(a.RepoID, 10) + ":" + hex.EncodeToString(sum[:16])
}

// probe returns the probe the job was queued for.
func (a BackgroundDownload) probe() formats.Probe {
	p := formats.Probe{Name: a.Name, Version: a.Version, Document: formats.Path{Path: a.Document}}
	for _, f := range a.Files {
		p.Files = append(p.Files, formats.Path{Path: f})
	}
	if b, err := hex.DecodeString(a.Digest); err == nil && len(b) == len(p.Digest) {
		copy(p.Digest[:], b)
	}
	return p
}

// ProbeMetrics counts the decisions on probes and the ends of background downloads by format and outcome.
type ProbeMetrics struct {
	Outcomes *prometheus.CounterVec
}

// MetricBackgroundDownloads is the family of [ProbeMetrics].
const MetricBackgroundDownloads = "quader_remote_background_downloads_total"

// NewProbeMetrics registers the family in reg; a nil registerer keeps it unregistered.
func NewProbeMetrics(reg prometheus.Registerer) (*ProbeMetrics, error) {
	m := &ProbeMetrics{Outcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: MetricBackgroundDownloads,
		Help: "Decisions on probes of remote repositories and the ends of the background downloads they started, by format and outcome (R-REPO-065).",
	}, []string{"format", "outcome"})}
	if reg != nil {
		if err := reg.Register(m.Outcomes); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *ProbeMetrics) count(format, outcome string) {
	if m != nil && m.Outcomes != nil {
		m.Outcomes.WithLabelValues(format, outcome).Inc()
	}
}

// probes holds the replica's decisions in flight.
type probes struct {
	sem chan struct{}
	wg  sync.WaitGroup
}

func newProbes() *probes { return &probes{sem: make(chan struct{}, probeDecisions)} }

// WaitProbes waits until the decisions on the probes answered so far are taken. Tests use it; the replica's shutdown does not wait.
func (h *Host) WaitProbes() {
	if h.pr != nil {
		h.pr.wg.Wait()
	}
}

// Probed implements [formats.RemoteFetcher] (R-REPO-065): the cheap checks run on the probe's request, the decision after Probed
// returned.
func (r *remote) Probed(ctx context.Context, rq *formats.Request, p formats.Probe) {
	h := r.h
	if h.o.Downloads == nil || rq == nil || rq.Repo == nil {
		return
	}
	format := rq.Repo.Format
	virtual := rq.Repo.Class == repository.Virtual
	precheck := r.precheck
	if virtual {
		precheck = r.precheckVirtual
	}
	if out := precheck(rq, p); out != "" {
		h.o.ProbeMetrics.count(format, out)
		return
	}
	select {
	case h.pr.sem <- struct{}{}:
	default:
		h.o.ProbeMetrics.count(format, ProbeBusy)
		return
	}
	charge := limits.FromContext(ctx)
	dctx := context.WithoutCancel(ctx)
	h.pr.wg.Add(1)
	go func() {
		defer h.pr.wg.Done()
		defer func() { <-h.pr.sem }()
		c, cancel := context.WithTimeout(dctx, probeDecisionTimeout)
		defer cancel()
		out, err := r.decideFor(c, rq, p, charge, virtual)
		if err != nil {
			h.o.Log.Warn("the decision on a probe failed; no background download starts", "repo", rq.Repo.Name.String(), "error", err)
		}
		h.o.ProbeMetrics.count(format, out)
	}()
}

// decideFor is the decision on a probe of rq's repository; for a virtual repository, on the member that serves the probe's content.
func (r *remote) decideFor(ctx context.Context, rq *formats.Request, p formats.Probe, charge *limits.Charge, virtual bool) (string, error) {
	if !virtual {
		return r.decide(ctx, rq, p, charge)
	}
	mrq, out, err := servingMember(ctx, r, rq, p)
	if mrq == nil {
		return out, err
	}
	if out := r.precheck(mrq, p); out != "" {
		return out, nil
	}
	return r.decide(ctx, mrq, p, charge)
}

// precheckVirtual is precheck for a probe a virtual repository answered: the repository is enabled and has a resolver, the request may
// contact upstreams, the probe names files. The member's own checks follow once it is found.
func (r *remote) precheckVirtual(rq *formats.Request, p formats.Probe) string {
	switch {
	case rq.Repo.Disabled || r.h.o.Resolver == nil:
		return ProbeOff
	case modeOf(rq) != proxy.Normal:
		return ProbeOff
	case p.Name == "" || len(p.Files) == 0:
		return ProbeNotAdmitted
	}
	return ""
}

// servingMember finds the member of the virtual repository rq addresses whose content the probe names, by the tiers of R-REPO-019
// step 6 for the probe's first file, without contacting an upstream (Q-REPO-367): the plan of the virtual repository for the caller
// (its patterns, pin rules and claims, the caller's read, nested virtual repositories expanded); a local member that holds the file
// ends the decision, the content is there (outcome local); the first remote member whose cache holds the file is the one; then, among
// the remote members the caller may fill, the first whose format's gate announces the file's digests from the member's stored
// documents (the member's upstream lists the version: npm, Ansible Galaxy, Debian), else the first the gate does not refuse (a
// format whose gate reads no document: NuGet). This is the member the virtual repository serves the version's content from, and the
// one whose entry a merged document carries for it (R-REPO-020). It returns the request derived for the member, or the outcome of a
// probe that starts nothing.
func servingMember(ctx context.Context, t probeTiers, rq *formats.Request, p formats.Probe) (*formats.Request, string, error) {
	file := p.Files[0]
	plan, err := t.planFor(ctx, rq, file)
	switch {
	case err != nil:
		return nil, ProbeFailed, err
	case plan == nil:
		return nil, ProbeOff, nil
	case !plan.Admitted:
		return nil, ProbeNotAdmitted, nil
	}
	var remotes []*formats.Request
	var fill []bool
	for _, pm := range plan.Members {
		if pm.Tier == formats.TierLocal {
			held, err := t.heldLocally(ctx, pm.Member, file)
			if err != nil {
				return nil, ProbeFailed, err
			}
			if held {
				return nil, ProbeLocal, nil
			}
			continue
		}
		mrq, err := memberRequest(rq, pm.Member)
		if err != nil {
			return nil, ProbeFailed, err
		}
		remotes, fill = append(remotes, mrq), append(fill, pm.MayFill)
	}
	for _, mrq := range remotes {
		cached, err := t.cachedAt(ctx, mrq, file)
		if err != nil {
			return nil, ProbeFailed, err
		}
		if cached {
			return mrq, "", nil
		}
	}
	var first *formats.Request
	for i, mrq := range remotes {
		if !fill[i] {
			continue
		}
		d, err := t.gateAt(ctx, mrq, file)
		if e, ok := errs.As(err); ok && e.Kind == errs.NotFound {
			continue
		}
		if err != nil {
			return nil, ProbeFailed, err
		}
		if len(d.SHA256)+len(d.SHA512)+len(d.SHA1)+len(d.MD5) > 0 {
			return mrq, "", nil
		}
		if first == nil {
			first = mrq
		}
	}
	if first != nil {
		return first, "", nil
	}
	return nil, ProbeNotAdmitted, nil
}

// probeTiers are what servingMember reads of the host; *remote implements them.
type probeTiers interface {
	// planFor is the virtual repository's plan for the file; nil without a resolver or a format with the class remote.
	planFor(ctx context.Context, rq *formats.Request, file formats.Path) (*formats.VirtualPlan, error)
	// heldLocally reports whether a local member holds the file.
	heldLocally(ctx context.Context, member formats.RepoRef, file formats.Path) (bool, error)
	// cachedAt reports whether a remote member's cache holds the file; a lost object is not held.
	cachedAt(ctx context.Context, mrq *formats.Request, file formats.Path) (bool, error)
	// gateAt runs the format's gate for the file on the member's stored documents, without contacting its upstream.
	gateAt(ctx context.Context, mrq *formats.Request, file formats.Path) (formats.Digests, error)
}

var _ probeTiers = (*remote)(nil)

func (r *remote) planFor(ctx context.Context, rq *formats.Request, file formats.Path) (*formats.VirtualPlan, error) {
	f, ok := r.h.o.Formats(rq.Repo.Format)
	if !ok || f.Remote() == nil || r.h.o.Resolver == nil {
		return nil, nil
	}
	return r.h.o.Resolver.Plan(ctx, rq, file, f.Classify(file))
}

func (r *remote) heldLocally(ctx context.Context, member formats.RepoRef, file formats.Path) (bool, error) {
	it, found, err := r.h.Items().Stat(ctx, member, file)
	return found && !it.Folder, err
}

func (r *remote) cachedAt(ctx context.Context, mrq *formats.Request, file formats.Path) (bool, error) {
	_, cached, err := r.Cached(ctx, mrq, file)
	if proxy.IsLost(err) {
		return false, nil // a lost item is a miss: the download fills it again (Q-REPO-202)
	}
	return cached, err
}

func (r *remote) gateAt(ctx context.Context, mrq *formats.Request, file formats.Path) (formats.Digests, error) {
	cv := *mrq
	cv.CacheView = true
	return r.gate(ctx, &cv, file)
}

// memberRequest is the request derived for the member ref of the virtual repository rq addresses.
func memberRequest(rq *formats.Request, ref formats.RepoRef) (*formats.Request, error) {
	members := rq.Repo.Members()
	for i := range members {
		if members[i].Name == ref.Name {
			return rq.Member(&members[i])
		}
	}
	return nil, errs.E(errs.Internal, errs.CodeInternal, "A planned member is not a member of the virtual repository.")
}

// precheck is the part of the decision that reads only the request: a remote repository whose switch is on, that may fetch now, a
// request that may contact the upstream, and a probe that names files. It returns the outcome of a probe that starts nothing, "" to go on.
func (r *remote) precheck(rq *formats.Request, p formats.Probe) string {
	st, ok := rq.Repo.Remote()
	switch {
	case !ok || rq.Repo.Class != repository.Remote || rq.Repo.Disabled:
		return ProbeOff
	case !st.DownloadOnProbe:
		return ProbeOff
	case modeOf(rq) != proxy.Normal:
		return ProbeOff
	case p.Name == "" || len(p.Files) == 0:
		return ProbeNotAdmitted
	}
	if off, ok := r.e.(interface{ Offline(proxy.Remote) bool }); ok {
		rem, err := proxy.NewRemote(*rq.Repo)
		if err != nil || off.Offline(rem) {
			return ProbeOff
		}
	}
	return ""
}

// decide takes the decision on a probe (R-REPO-065), in the order the requirement lists: the patterns and claims, the populate
// permission of the probe's caller, what is stored, the queue, step 9a of the caller's limits, the enqueue.
func (r *remote) decide(ctx context.Context, rq *formats.Request, p formats.Probe, charge *limits.Charge) (string, error) {
	h := r.h
	f, ok := h.o.Formats(rq.Repo.Format)
	if !ok || f.Remote() == nil {
		return ProbeOff, nil
	}
	files := p.Files[:min(len(p.Files), formats.MaxProbeFiles)]
	admitted := make([]formats.Path, 0, len(files))
	for _, file := range files {
		if r.admits(ctx, rq, f, file) {
			admitted = append(admitted, file)
		}
	}
	if len(admitted) == 0 {
		return ProbeNotAdmitted, nil
	}
	first := admitted[0]
	info := f.Classify(first)
	if !h.o.Decider.MayPopulateOn(ctx, rq.Index, rq.Subject, populateCoordinate(rq, info, first)).Allowed() {
		return ProbeNotAdmitted, nil
	}
	stored, err := r.allStored(ctx, rq, admitted)
	if err != nil {
		return ProbeFailed, err
	}
	if stored {
		return ProbeStored, nil
	}
	st, _ := rq.Repo.Remote()
	args := BackgroundDownload{RepoID: rq.RepoRef().ID, Name: p.Name, Version: p.Version, Document: p.Document.Path}
	for _, file := range admitted {
		args.Files = append(args.Files, file.Path)
	}
	if p.Document.Path != "" {
		args.Digest = hex.EncodeToString(p.Digest[:])
	}
	active, n, err := h.o.Downloads.Busy(ctx, args.Key(), args.RepoID)
	switch {
	case err != nil:
		return ProbeFailed, err
	case active:
		return ProbeActive, nil
	case n >= max(1, st.Limits.MaxConcurrentFetches):
		return ProbeQueueFull, nil
	}
	ticket, _, err := charge.StartBackground(ctx, args.RepoID)
	if err != nil {
		var be *limits.BudgetError
		if errors.As(err, &be) {
			return ProbeLimited, nil
		}
		return ProbeFailed, err
	}
	args.Ticket = ticket
	created, err := h.o.Downloads.Enqueue(ctx, args)
	switch {
	case err != nil:
		return ProbeFailed, err
	case !created:
		return ProbeActive, nil
	}
	return ProbeStarted, nil
}

// admits reports whether the repository's include and exclude patterns admit a file of the probe and no global claim refuses its
// name at the remote (R-REPO-005, R-REPO-023): what Host.Authorize applies to a client's read of the file.
func (r *remote) admits(ctx context.Context, rq *formats.Request, f formats.Format, file formats.Path) bool {
	if !patternsAdmit(*rq.Repo, f, file) {
		return false
	}
	if pkg := f.Classify(file).Package; pkg != "" && r.h.o.Refusal != nil {
		refused, err := r.h.o.Refusal.RemoteRefused(ctx, *rq.Repo, pkg, nil)
		if err != nil || refused {
			return false
		}
	}
	return true
}

// allStored reports whether every file is stored, asking until the first that is not (a probe of a version that is not stored costs
// one lookup).
func (r *remote) allStored(ctx context.Context, rq *formats.Request, files []formats.Path) (bool, error) {
	for _, file := range files {
		preq, err := r.request(ctx, rq, file, "", proxy.Artifact)
		if err != nil {
			return false, err
		}
		_, cached, err := r.e.Cached(ctx, preq)
		switch {
		case proxy.IsLost(err):
			return false, nil // a lost item is a miss: the download fills it again (Q-REPO-202)
		case err != nil:
			return false, err
		case !cached:
			return false, nil
		}
	}
	return true, nil
}
