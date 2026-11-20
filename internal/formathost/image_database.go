package formathost

import (
	"context"
	"net/url"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/repository"
)

// ImageDatabaseUpstream returns the upstream of one run of the image scanning's database task on a remote repository
// (vulnerability-scanning.md R-VULN-044, Q-VULN-172): the system's own reading through the repository, in process, the way a background
// download of R-REPO-065 runs. Its Document fetches or revalidates the tag's document once per run and its Artifact makes the repository
// hold a file: every request is a background request within the repository's background share, yields to client fetches, reads the
// upstream's circuit without moving it and waits out a rate limit (R-REPO-054, R-REPO-055); a file goes through the format's gate, the
// engine's integrity check and the repository's include and exclude patterns, and its fill records the populate event of
// system:image-database (R-AUTHZ-008). It carries no charge, so the engine records every exchange and uncached byte under the subject
// system of the repository and limits it by nothing (usage-and-limits.md Q-LIM-19). A file the repository holds costs no upstream request.
//
// The repository's own settings decide the upstream: its address and stored account, the egress rules, the outbound proxy, offline. A
// system repository such as upstream/scanner-trivy answers every protocol request 404 (R-REPO-070); this path is not a protocol request
// and reaches it. jobID names the job in the upstream log record of every request (R-REPO-057).
func (m *MirrorHost) ImageDatabaseUpstream(repo repository.Repository, jobID string) (discover.Upstream, error) {
	if repo.Class != repository.Remote {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The repository is not a remote repository.")
	}
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
	pu.fill = fillImageDatabase
	return &backgroundUpstream{passUpstream: pu, win: mirror.NewWindow(nil, nil, share)}, nil
}

// AdmitsName reports whether the repository's include and exclude patterns, its claims and the format's name rule admit an image name
// (MirrorHost.Admits): the database task asks it before it asks the upstream for the tag's document, so that a name the patterns refuse
// costs no request.
func (m *MirrorHost) AdmitsName(ctx context.Context, repo repository.Repository, name string) (bool, error) {
	admit, err := m.Admits(ctx, repo)
	if err != nil {
		return false, err
	}
	return admit(name), nil
}
