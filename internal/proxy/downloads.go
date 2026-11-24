package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// The origins of a download from an upstream (operations-and-observability.md R-OPS-049): what caused the fill.
const (
	DownloadOriginClient          = "client"
	DownloadOriginQueue           = "queue"
	DownloadOriginMirror          = "mirror"
	DownloadOriginScannerDatabase = "scanner_database"
)

// The outcomes a download ends with on its replica (R-OPS-049); interrupted is the sweep's, never the engine's.
const (
	DownloadCompleted = "completed"
	DownloadFailed    = "failed"
	DownloadCancelled = "cancelled"
)

// DownloadTracker sees every download of an artifact's body from an upstream (R-OPS-049): the fetch of R-REPO-025 step 10 that got a
// success answer and passed the checks before the first byte. internal/fills implements it; nil sees none. Begin and End run on the
// fetch's goroutine and must not block; the reader's count is the only work per read of the body.
type DownloadTracker interface {
	Begin(DownloadStart) Download
}

// DownloadStart are the facts of a download at its begin.
type DownloadStart struct {
	RepoID int64
	// Repo is the repository's qualified name, the label of the metrics.
	Repo   string
	Format string
	// Name and Version are the request's package and version as the format classified the path; Version is the queue row's for a request
	// that has none (an OCI blob of a queued version).
	Name, Version, Path string
	// Total is the length the upstream announced, -1 for none.
	Total int64
	Cause DownloadCause
}

// DownloadCause is what caused a download (R-OPS-049). A request without one is a client's: origin client, its caller's principal.
type DownloadCause struct {
	// Origin is one of the DownloadOrigin constants.
	Origin string
	// QueueOrigin, QueueRow and QueueVersion are the queue row's origin, id and version; empty unless the origin is queue.
	QueueOrigin  remotecuration.Origin
	QueueRow     string
	QueueVersion string
	// Principal is the principal who caused it: the person who started the row, its bulk or its request; "" for system work.
	Principal string
}

// Download is one running download.
type Download interface {
	// Reader counts what is read through it as the download's bytes.
	Reader(io.Reader) io.Reader
	// End records the outcome once; f is the failure of a failed download and nil otherwise.
	End(outcome string, f *remotecuration.Failure)
}

// beginDownload reports the begin of a download to the tracker and wraps the body so that its bytes are counted before the spool; nil
// when the engine has no tracker.
func (f *fetcher) beginDownload(resp *http.Response) Download {
	t := f.e.o.Downloads
	if t == nil {
		return nil
	}
	rq := f.rq
	cause := DownloadCause{Origin: DownloadOriginClient, Principal: rq.Caller.Principal}
	if rq.Cause != nil {
		cause = *rq.Cause
	}
	version := rq.Version
	if version == "" && cause.Origin == DownloadOriginQueue {
		version = cause.QueueVersion
	}
	total := resp.ContentLength
	if total < 0 {
		total = -1
	}
	d := t.Begin(DownloadStart{
		RepoID: rq.Remote.id, Repo: rq.Remote.Repo.Name.String(), Format: rq.Remote.Repo.Format,
		Name: rq.Package, Version: version, Path: rq.Path, Total: total, Cause: cause,
	})
	if d != nil {
		resp.Body = countedBody{Reader: d.Reader(resp.Body), Closer: resp.Body}
	}
	return d
}

// countedBody is a response body read through a download's counter and closed as the body it wraps.
type countedBody struct {
	io.Reader
	io.Closer
}

// endDownload records the end of a download with the outcome of the fetch's result.
func endDownload(ctx context.Context, d Download, res flightResult) {
	if d == nil {
		return
	}
	outcome, failure := downloadOutcome(ctx, res)
	d.End(outcome, failure)
}

// downloadOutcome maps the result of an artifact's fetch to the outcome of its download (R-OPS-049): stored (also by a duplicate flight)
// is completed; a fetch whose context ended before its result, a pre-emption among them, is cancelled; every other result failed, with
// the failure's class and code of R-CUR-011. A body that went to its client unstored because the object store was unavailable failed
// with the class storage.
func downloadOutcome(ctx context.Context, res flightResult) (string, *remotecuration.Failure) {
	switch {
	case res.stored:
		return DownloadCompleted, nil
	case ctx.Err() != nil, errors.Is(res.own, ErrPreempted):
		return DownloadCancelled, nil
	}
	err := res.err
	if err == nil {
		err = res.own
	}
	if err == nil {
		err = errs.E(errs.Unavailable, errs.CodeObjectStoreUnavail, "The object store is unavailable; the artifact went to its client unstored.")
	}
	d := Describe(err)
	return DownloadFailed, &remotecuration.Failure{Class: d.Class, Code: d.Code}
}
