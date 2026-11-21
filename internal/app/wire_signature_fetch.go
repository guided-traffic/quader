package app

import (
	"context"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	sigfetch "github.com/guided-traffic/quader/internal/signatures/fetch"
)

// The background fetch of a remote's signatures (signatures.md R-SIG-026): the data ports of OCI and npm queue a target through
// signatureFetchQueue in the transaction that stored what asks for it; the job signature-fetch runs it on a replica that claims jobs,
// with the upstream of the repository for one run (Host.BackgroundUpstream) and the format's fetch, found through the capabilities
// Register read.

// signatureFetchQueue is the queue the data ports take. A queue that cannot enqueue (the restore's) queues nothing: the fetch is no
// condition of a commit.
// refreshAfter is signatures.remote.refresh_after (0 the default).
func signatureFetchQueue(db meta.Runner, queue any, refreshAfter time.Duration) *sigfetch.Queue {
	jq, ok := queue.(sigfetch.Enqueuer)
	if !ok {
		return nil
	}
	return sigfetch.NewQueue(sigfetch.Options{DB: db, Jobs: jq, RefreshAfter: refreshAfter})
}

// signatureFetchHandler is the handler registered for the kind signature-fetch; the replica that claims jobs sets the runtime
// (startSignatureFetch).
func (a *App) signatureFetchHandler() jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		h, ok := a.signatureFetch.get()
		if !ok {
			return fmt.Errorf("signature-fetch: this replica has not wired it yet")
		}
		return h(ctx, run)
	}
}

// startSignatureFetch builds the runtime of the job signature-fetch on a replica that claims jobs.
func (a *App) startSignatureFetch(in infra) {
	cfg := a.cfg.Signatures
	o := sigfetch.HandlerOptions{
		Queue:      signatureFetchQueue(in.db, in.queue, cfg.Remote.RefreshAfter),
		Background: signatureBackground{a},
		Fetcher: func(format string) (sigfetch.Fetcher, bool) {
			f := formats.RemoteCapabilities(formats.ID(format)).Signatures
			return f, f != nil
		},
		// The bounds of R-SIG-040: the requests per remote and hour, and what one run fetches of an image (R-SIG-026).
		RequestsPerHour: cfg.Remote.RequestsPerHour,
		RunContext: func(ctx context.Context) context.Context {
			return formats.WithSignatureFetchLimits(ctx, formats.SignatureFetchLimits{
				MaxManifests: cfg.Remote.MaxManifestsPerSubject, MaxObjectSize: int64(cfg.MaxObjectSize),
			})
		},
		// Every request of a run counts in quader_signatures_remote_requests_total (R-SIG-039); the metric is registered by the jobs of
		// the signatures on this replica (wire_signatures.go), found when a run reports.
		Observe: func(format, result string) {
			if m := part[signaturesArea](a.parts).metrics; m != nil {
				m.RemoteRequest(format, result)
			}
		},
	}
	a.signatureFetch.set(sigfetch.Handler(o))
}

// signatureBackground gives the job the upstream of a remote repository for one run, from the mirror host's builder, as the Debian
// remote class's jobs take it (debBackground).
type signatureBackground struct{ a *App }

var _ sigfetch.Background = signatureBackground{}

// Upstream implements [sigfetch.Background].
func (b signatureBackground) Upstream(ctx context.Context, repoID int64, jobID string) (discover.Upstream, error) {
	area := part[mirrorEngineArea](b.a.parts)
	if area.b == nil {
		return nil, fmt.Errorf("formats: this process has no remote engine for the background fetch of signatures")
	}
	mh, err := area.b.get()
	if err != nil {
		return nil, err
	}
	return mh.Background(formathost.WithJobID(ctx, jobID), repoID)
}
