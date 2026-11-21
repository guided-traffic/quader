package app

import (
	"context"
	"fmt"

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
func signatureFetchQueue(db meta.Runner, queue any) *sigfetch.Queue {
	jq, ok := queue.(sigfetch.Enqueuer)
	if !ok {
		return nil
	}
	return sigfetch.NewQueue(sigfetch.Options{DB: db, Jobs: jq})
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
	a.signatureFetch.set(sigfetch.Handler(sigfetch.HandlerOptions{
		Queue:      signatureFetchQueue(in.db, in.queue),
		Background: signatureBackground{a},
		Fetcher: func(format string) (sigfetch.Fetcher, bool) {
			f := formats.RemoteCapabilities(formats.ID(format)).Signatures
			return f, f != nil
		},
	}))
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
