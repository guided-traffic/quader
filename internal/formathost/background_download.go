package formathost

import (
	"context"
	"errors"
	"net/url"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/repository"
)

// The background download a probe started (repositories-and-proxy.md R-REPO-065): a job of the worker, run through the same gate
// and engine as a mirror pass. Its requests take a background slot within the repository's background share and yield to client
// fetches (R-REPO-055); they read the upstream's circuit without moving it and wait out a rate limit (R-REPO-054); every file goes
// through the format's gate, the repository's include and exclude patterns, the vulnerability scanning's rule for a miss and the
// populate record of system:background-download. Its upstream exchanges and uncached bytes are charged to the probe's budget key
// (usage-and-limits.md R-LIM-037).

// BackgroundDownloadKind is the job kind of a probe's background download (queue default, exclusive per version, BackgroundDownload.Key).
const BackgroundDownloadKind = "remote-background-download"

// BackgroundDownloadResult is the result the job records: what the version's content was and what became of it.
type BackgroundDownloadResult struct {
	// Outcome is completed, bounded or skipped (the repository is gone, no longer remote, or its switch was turned off).
	Outcome string `json:"outcome"`
	// Files are the files of the content; Stored those this download stored, Present those stored before (and the manifests it read
	// to find the content), Refused those the repository's patterns, the format's gate, the upstream or the scanning refused. Bytes
	// are the bytes this download stored.
	Files   int   `json:"files"`
	Stored  int   `json:"stored"`
	Present int   `json:"present"`
	Refused int   `json:"refused"`
	Bytes   int64 `json:"bytes"`
}

// The outcome of a background download that found nothing to do.
const downloadSkipped = "skipped"

// BackgroundDownloadOptions are what a run needs besides the job: the limits engine that charges it and the byte bound of one download.
type BackgroundDownloadOptions struct {
	// Limits resumes the probe's charge (limits.Engine.Resume); nil charges nobody.
	Limits *limits.Engine
	// MaxBytes is proxy.max_artifact_bytes, the system value of a repository's limits.maxArtifactBytes: one background download
	// stores at most the repository's value of it in sum (R-REPO-065).
	MaxBytes int64
	// Metrics counts the end of the download; nil counts nothing.
	Metrics *ProbeMetrics
}

// RunBackgroundDownload runs one attempt of a job remote-background-download (R-REPO-065). An error that a later attempt can overcome
// (the circuit open, the upstream's rate limit, a pre-emption by a client fetch, a failure of the upstream) is returned for the job's
// retry: what this attempt stored is present for the next. A refusal of a single file is counted and the run goes on.
func (m *MirrorHost) RunBackgroundDownload(ctx context.Context, a BackgroundDownload, jobID string, o BackgroundDownloadOptions) (BackgroundDownloadResult, error) {
	res := BackgroundDownloadResult{Outcome: downloadSkipped}
	repo, err := m.h.o.Repos.ByID(ctx, a.RepoID)
	if errs.KindOf(err) == errs.NotFound {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	st, ok := repo.Remote()
	if !ok || repo.Disabled || !st.DownloadOnProbe {
		return res, nil
	}
	f, ok := m.h.o.Formats(repo.Format)
	if !ok || f.Remote() == nil {
		return res, nil
	}
	charge := o.Limits.Resume(a.Ticket, a.RepoID)
	defer charge.Done()
	u, err := m.downloadUpstream(repo, jobID, charge)
	if err != nil {
		return res, err
	}
	// A manifest the format reads to find the content is stored as it is read (an OCI index's platforms): counted as stored here,
	// and as present when the loop below reaches it.
	read := mirror.WithStoredTally(ctx, func(fr mirror.FillResult) { res.Bytes += fr.Bytes })
	items, err := f.Remote().ProbeContent(read, u, a.probe())
	if err != nil {
		o.Metrics.count(repo.Format, DownloadFailed)
		return res, mapBackground(err)
	}
	bound := o.MaxBytes
	if st.Limits.MaxArtifactBytes != nil {
		bound = *st.Limits.MaxArtifactBytes
	}
	res.Outcome = DownloadCompleted
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.Path] {
			continue
		}
		seen[it.Path] = true
		if res.Files == formats.MaxProbeFiles || (bound > 0 && res.Bytes+it.Size > bound) {
			res.Outcome = DownloadBounded
			break
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
			continue
		case err != nil:
			o.Metrics.count(repo.Format, DownloadFailed)
			return res, err
		case fr.res.Present:
			res.Present++
		default:
			res.Stored++
			res.Bytes += fr.res.Bytes
		}
	}
	if res.Outcome == DownloadCompleted && res.Refused == 0 && a.Document != "" {
		p := a.probe()
		if _, err := m.e.MarkBackgroundDownloaded(ctx, a.RepoID, p.Document.Path, p.Digest); err != nil {
			return res, err
		}
	}
	o.Metrics.count(repo.Format, res.Outcome)
	return res, nil
}

// downloadUpstream is the upstream of one run: a pass's, with the window of a background run (Host.BackgroundUpstream), whose
// requests carry the probe's charge and record their fills for system:background-download.
func (m *MirrorHost) downloadUpstream(repo repository.Repository, jobID string, charge *limits.Charge) (*backgroundUpstream, error) {
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
	pu.charge, pu.fill = charge, fillBackgroundDownload
	return &backgroundUpstream{passUpstream: pu, win: mirror.NewWindow(nil, nil, share)}, nil
}
