package formathost

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
	"github.com/guided-traffic/quader/internal/repository"
)

// The run of a row of the download queue (remote-curation.md R-CUR-011, R-CUR-013; R-REPO-065's run of a probe's background
// download, which it generalizes): one attempt of one version, run through the same gate and engine as a mirror pass. Its requests
// take a background slot within the repository's background share and yield to client fetches (R-REPO-055); they read the upstream's
// circuit without moving it and wait out a rate limit (R-REPO-054); every file goes through the format's gate, the vulnerability
// scanning's rule for a miss (R-CUR-014: a download ahead does not ask the delivery rules) and the populate record of
// system:background-download. A person's download is charged to the person's budget key with the ticket the API took, a probe's to the
// probe's (usage-and-limits.md R-LIM-037); a bulk's, a request's and an allow's to nobody: system work (Q-LIM-19).

// DownloadOptions are what a run needs besides the row: the limits engine that charges it and the byte bound of one download.
type DownloadOptions struct {
	// Limits resumes the row's charge (limits.Engine.Resume); nil charges nobody.
	Limits *limits.Engine
	// MaxBytes is proxy.max_artifact_bytes, the system value of a repository's limits.maxArtifactBytes: one download stores at most
	// the repository's value of it in sum (R-REPO-065).
	MaxBytes int64
	// Now is the clock of the failure's time; nil is the system's.
	Now func() time.Time
}

// RunDownload runs one attempt of a row of the download queue (R-CUR-011) and reports its end for queue.Service.Finish: the plan
// (the row's probe, else the format's VersionProbe through the worker's upstream; the step plan), the content the format names
// (formats.RemoteAdapter.ProbeContent) and each file's fill. A refusal of a single file is counted and the run goes on; any other
// failure ends the attempt with the failure in words (proxy.Describe). The error is the context's when the run was stopped before it
// ended (the caller tells a cancellation from a release); the result is then partial.
func (m *MirrorHost) RunDownload(ctx context.Context, row queue.Row, jobID string, o DownloadOptions) (queue.Result, error) {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	res := queue.Result{Outcome: remotecuration.OutcomeSkipped}
	fail := func(err error, file string) (queue.Result, error) {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		f := proxy.Describe(err)
		f.File, f.Attempt, f.At = file, row.Attempts, now()
		res.Failed, res.Failure = true, &f
		return res, nil
	}
	repo, st, f, ok, err := m.downloadable(ctx, row)
	if err != nil {
		return fail(err, "")
	}
	if !ok {
		return res, nil
	}
	var ticket limits.Ticket
	if row.Charged() {
		if err := json.Unmarshal(row.Ticket, &ticket); err != nil {
			return fail(errs.E(errs.Internal, errs.CodeInternal, "The charge of the download does not read.", errs.WithCause(err)), "")
		}
	}
	charge := o.Limits.Resume(ticket, row.RepoID)
	defer charge.Done()
	fill := fillDownloadAhead
	if row.Origin == remotecuration.OriginProbe {
		fill = fillBackgroundDownload
	}
	u, err := m.downloadUpstream(repo, jobID, charge, fill)
	if err != nil {
		return fail(err, "")
	}
	// A manifest the format reads to find the content is stored as it is read (an OCI index's platforms): counted as stored here,
	// and as present when the loop below reaches it.
	read := mirror.WithStoredTally(ctx, func(fr mirror.FillResult) { res.Bytes += fr.Bytes })
	p, err := m.planOf(read, row, repo, u)
	if err != nil {
		return fail(err, "")
	}
	items, err := f.Remote().ProbeContent(read, u, p)
	if err != nil {
		return fail(mapBackground(err), "")
	}
	bound := o.MaxBytes
	if st.Limits.MaxArtifactBytes != nil {
		bound = *st.Limits.MaxArtifactBytes
	}
	res.Outcome = remotecuration.OutcomeCompleted
	seen := map[string]bool{}
	notFound := 0
	for _, it := range items {
		if seen[it.Path] {
			continue
		}
		seen[it.Path] = true
		if res.Files == formats.MaxProbeFiles || (bound > 0 && res.Bytes+it.Size > bound) {
			res.Outcome = remotecuration.OutcomeBounded
			break
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.Files++
		var fr filled
		err := u.win.Do(ctx, func(c context.Context) (e error) {
			fr, e = m.fillPath(withBackground(c, u.facts()), repo, u.pass, formats.Path{Path: it.Path})
			return e
		})
		var refusal *mirror.RefusedError
		switch {
		case errors.As(err, &refusal):
			res.Refused++
			if refusal.Reason == mirror.RefusedNotFound {
				notFound++
			}
			d := proxy.Describe(err)
			d.File, d.Attempt, d.At = it.Path, row.Attempts, now()
			res.Failure = &d
			continue
		case err != nil:
			return fail(err, it.Path)
		case fr.res.Present:
			res.Present++
		default:
			res.Stored++
			res.Bytes += fr.res.Bytes
		}
	}
	switch {
	case res.Files > 0 && notFound == res.Files:
		res.Failed = true // the upstream has none of the files: no attempt changes that
	case res.Files > 0 && res.Refused == res.Files:
		res.Outcome = remotecuration.OutcomeRefused
	}
	if res.Outcome == remotecuration.OutcomeCompleted && res.Refused == 0 && p.Document.Path != "" {
		if _, err := m.e.MarkBackgroundDownloaded(ctx, row.RepoID, p.Document.Path, p.Digest); err != nil {
			return fail(err, p.Document.Path)
		}
	}
	return res, nil
}

// downloadable reads the row's repository: ok is false for a run that has nothing to do (the outcome skipped): the repository is
// gone, no longer remote, disabled or offline, its format has no remote class, or the switch remote.downloadOnProbe of a probe's row
// was turned off while it waited (R-CUR-013).
func (m *MirrorHost) downloadable(ctx context.Context, row queue.Row) (repository.Repository, repository.RemoteSettings, formats.Format, bool, error) {
	repo, err := m.h.o.Repos.ByID(ctx, row.RepoID)
	if errs.KindOf(err) == errs.NotFound {
		return repo, repository.RemoteSettings{}, nil, false, nil
	}
	if err != nil {
		return repo, repository.RemoteSettings{}, nil, false, err
	}
	st, ok := repo.Remote()
	if !ok || repo.Disabled || (row.Origin == remotecuration.OriginProbe && !st.DownloadOnProbe) {
		return repo, st, nil, false, nil
	}
	f, ok := m.h.o.Formats(repo.Format)
	if !ok || f.Remote() == nil {
		return repo, st, nil, false, nil
	}
	if off, ok := m.e.(interface{ Offline(proxy.Remote) bool }); ok {
		if rem, err := proxy.NewRemote(repo); err == nil && off.Offline(rem) {
			return repo, st, nil, false, nil
		}
	}
	return repo, st, f, true, nil
}

// planOf is what the run starts from: the probe the row holds (a probe's, R-CUR-013), else the format's VersionProbe through the
// worker's upstream (the step plan, R-CUR-011). A version the upstream does not have is a failure upstream_not_found.
func (m *MirrorHost) planOf(ctx context.Context, row queue.Row, repo repository.Repository, u *backgroundUpstream) (formats.Probe, error) {
	if row.Probe != nil && len(row.Probe.Files) > 0 {
		return probeOf(row), nil
	}
	vp := formats.RemoteCapabilities(formats.ID(repo.Format)).VersionProbe
	if vp == nil {
		return formats.Probe{}, errs.E(errs.NotImplemented, errs.CodeNotImplemented,
			"The format "+repo.Format+" cannot name the files of one version; only a client's request can fetch them.")
	}
	p, ok, err := vp.VersionProbe(ctx, u, row.Name, row.Version)
	if err != nil {
		return formats.Probe{}, mapBackground(err)
	}
	if !ok || len(p.Files) == 0 {
		return formats.Probe{}, errs.E(errs.NotFound, errs.CodeNotFound,
			fmt.Sprintf("The upstream has no version %q of %q.", row.Version, row.Name))
	}
	return p, nil
}

// probeOf is the probe a row holds.
func probeOf(row queue.Row) formats.Probe {
	p := formats.Probe{Name: row.Name, Version: row.Version, Document: formats.Path{Path: row.Probe.Document}}
	for _, f := range row.Probe.Files {
		p.Files = append(p.Files, formats.Path{Path: f})
	}
	if b, err := hex.DecodeString(row.Probe.Digest); err == nil && len(b) == len(p.Digest) {
		copy(p.Digest[:], b)
	}
	return p
}

// downloadUpstream is the upstream of one run: a pass's, with the window of a background run (Host.BackgroundUpstream), whose
// requests carry the row's charge and record their fills for system:background-download with the reason of the work.
func (m *MirrorHost) downloadUpstream(repo repository.Repository, jobID string, charge *limits.Charge, fill filler) (*backgroundUpstream, error) {
	rem, err := proxy.NewRemote(repo)
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The repository is not a usable remote repository.", errs.WithCause(err))
	}
	var hostsOf func(*url.URL) []string
	if lister, ok := m.Lister(repo.Format); ok {
		hostsOf = lister.MirrorHosts
	}
	share := proxy.BackgroundShare(rem.Settings.Limits.MaxConcurrentFetches, rem.Settings.Mirror.Concurrency)
	pu, err := m.newPassUpstream(repo, rem, mirror.Pass{JobID: jobID, Ceiling: share}, hostsOf)
	if err != nil {
		return nil, err
	}
	pu.charge, pu.fill = charge, fill
	return &backgroundUpstream{passUpstream: pu, win: mirror.NewWindow(nil, nil, share)}, nil
}

// The expansion of a bulk (R-CUR-010): the format's mirror lister names a package's versions through the worker's upstream, bounded by
// the bulk's bounds instead of the mirror's; a sub-section's packages are the catalogue's (R3), which no format answers yet.

var _ queue.Planner = (*MirrorHost)(nil)

// Versions implements [queue.Planner] over the format's mirror plan (discover.Lister.MirrorPlan): the versions newest first, each with
// the sum of the sizes the upstream announced for its files.
func (m *MirrorHost) Versions(ctx context.Context, repoID int64, name string, includePrereleases bool, maxVersions int) ([]queue.PlannedVersion, error) {
	repo, err := m.h.o.Repos.ByID(ctx, repoID)
	if err != nil {
		return nil, err
	}
	lister, ok := m.Lister(repo.Format)
	if !ok {
		return nil, errs.E(errs.NotImplemented, errs.CodeNotImplemented, "The format "+repo.Format+" cannot list the versions of a package.")
	}
	u, err := m.downloadUpstream(repo, "", nil, fillDownloadAhead)
	if err != nil {
		return nil, err
	}
	plan, err := lister.MirrorPlan(ctx, &boundedUpstream{backgroundUpstream: u, max: maxVersions, prereleases: includePrereleases}, name)
	if err != nil {
		return nil, mapBackground(err)
	}
	var out []queue.PlannedVersion
	index := map[string]int{}
	for _, it := range plan.Items {
		if it.Prerelease && !includePrereleases && !it.Pinned {
			continue
		}
		i, seen := index[it.Version]
		if !seen {
			if maxVersions > 0 && len(out) >= maxVersions {
				continue
			}
			i = len(out)
			index[it.Version] = i
			out = append(out, queue.PlannedVersion{Version: it.Version})
		}
		out[i].Size += it.Size
	}
	return out, nil
}

// Packages implements [queue.Planner]: the packages of a sub-section are listed by the upstream catalogue's levels, which no format
// answers before the catalogue's package (R3).
func (m *MirrorHost) Packages(context.Context, int64, string, string) ([]string, string, bool, error) {
	return nil, "", true, discover.ErrNotOffered
}

// boundedUpstream is a run's upstream whose version bounds are a bulk's, not the mirror's (discover.Bounded).
type boundedUpstream struct {
	*backgroundUpstream
	max         int
	prereleases bool
}

// VersionBounds implements [discover.Bounded].
func (b *boundedUpstream) VersionBounds() (int, bool) { return b.max, b.prereleases }
