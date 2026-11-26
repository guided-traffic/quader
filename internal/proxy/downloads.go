package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

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

// DownloadUnit is how a format's files group into the unit a person asked for (R-OPS-049, Q-OPS-427): the drawer of the top bar and
// the last 100 finished show units, never the files of one.
type DownloadUnit uint8

const (
	// UnitFile: every file a client fetches is a unit of its own (the zero value).
	UnitFile DownloadUnit = iota
	// UnitVersion: the files of one version that one caller fetches at once are one unit (npm's tarball, NuGet's package, an Ansible
	// archive, a Debian package's file; a source package's .dsc with its tarballs).
	UnitVersion
	// UnitImage: the manifests and blobs of an image that one caller fetches by its name are one unit (OCI). A blob names no tag; the
	// unit's version is resolved after the begin (R-OPS-049 "A client's pull of an image").
	UnitImage
)

// UnitLinger is how long a unit whose parts nobody names in advance (a client's pull of an image, a mirror pass's image) stays running
// after its last part ended: the next layer a client asks for joins it instead of starting a second row (R-OPS-049, Q-OPS-428).
const UnitLinger = 5 * time.Second

// DownloadTracker sees every download of an artifact's body from an upstream (R-OPS-049): the fetch of R-REPO-025 step 10 that got a
// success answer and passed the checks before the first byte. internal/fills implements it; nil sees none. Begin and End run on the
// fetch's goroutine and must not block; the reader's count is the only work per read of the body. A part joins the unit of its key
// (DownloadStart.Unit); Hold keeps a unit running between its parts while the work that names them runs (a queue row's run).
type DownloadTracker interface {
	Begin(DownloadStart) Download
	// Hold keeps the unit of key running until Release, between its parts too, and lets the holder name the parts it will fetch with
	// their sizes: the unit's total (R-OPS-049 "The facts of a unit").
	Hold(key string) DownloadHold
}

// DownloadHold is a hold on a unit (DownloadTracker.Hold).
type DownloadHold interface {
	// Expect names a part the holder will fetch and its size in bytes; 0 is unknown, which leaves the unit without a total until the
	// part begins with a length.
	Expect(path string, size int64)
	// Settle says the part at path needs no download (stored already, refused, not found): it leaves the unit's total.
	Settle(path string)
	// Fail says the work failed with f (its File the path it failed at): the unit's outcome is failed with it, whatever its parts did.
	// A unit without a part was no download and shows nowhere.
	Fail(f remotecuration.Failure)
	// Release ends the hold: a unit with no running part ends now. A second call does nothing.
	Release()
}

// DownloadStart are the facts of a download at its begin.
type DownloadStart struct {
	RepoID int64
	// Repo is the repository's qualified name, the label of the metrics.
	Repo   string
	Format string
	// Name and Version are the request's package and version as the format classified the path; Version is the cause's for a request
	// that has none (an OCI blob of a queued or mirrored tag).
	Name, Version, Path string
	// Total is the length the upstream announced, -1 for none.
	Total int64
	Cause DownloadCause
	// Unit is the key of the unit the part belongs to (R-OPS-049): parts of one key make one unit on a replica, and the units of one key
	// on several replicas are merged at the read. "" is a unit of this part alone.
	Unit string
	// Open marks a unit whose parts nobody names in advance (a client's pull of an image, a mirror pass's image): it lingers UnitLinger
	// after its last part and has no total unless a hold names its parts.
	Open bool
	// Resolve marks a part whose unit's version is resolved after the begin (a client's blob of an image names no tag).
	Resolve bool
}

// DownloadCause is what caused a download (R-OPS-049). A request without one is a client's: origin client, its caller's principal.
type DownloadCause struct {
	// Origin is one of the DownloadOrigin constants.
	Origin string
	// QueueOrigin and QueueRow are the queue row's origin and id; empty unless the origin is queue.
	QueueOrigin remotecuration.Origin
	QueueRow    string
	// Version is the version the work fetches for: a queue row's, a mirror pass's item; a request without a version of its own takes it.
	Version string
	// Unit is the key of the work's unit: a queue row's run, the database task's run; for a mirror pass its job, which the engine
	// completes with the part's name and version. "" for a client's.
	Unit string
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
	if version == "" {
		version = cause.Version
	}
	total := resp.ContentLength
	if total < 0 {
		total = -1
	}
	s := DownloadStart{
		RepoID: rq.Remote.id, Repo: rq.Remote.Repo.Name.String(), Format: rq.Remote.Repo.Format,
		Name: rq.Package, Version: version, Path: rq.Path, Total: total, Cause: cause,
	}
	s.Unit, s.Open, s.Resolve = unitOf(rq.Remote.id, rq.Unit, s)
	d := t.Begin(s)
	if d != nil {
		resp.Body = countedBody{Reader: d.Reader(resp.Body), Closer: resp.Body}
	}
	return d
}

// unitOf is the key of the unit a part belongs to (R-OPS-049, Q-OPS-427), whether the unit is open (it lingers and has no total unless
// a hold names its parts) and whether its version is resolved after the begin:
//   - a queue row's run and the database task's run: the work's key, one unit whatever the format;
//   - a mirror pass: the pass's job with the part's name and version, open for an image;
//   - a client's: the format's unit, keyed by the repository, the name, the version where the format's unit is one, and the caller.
//
// A key of a client's unit repeats (the same caller fetches the same thing again later); the read tells two units of one key apart by
// the time between them (fills.MergeGap). The other keys name one run of work.
func unitOf(repo int64, unit DownloadUnit, s DownloadStart) (key string, open, resolve bool) {
	c := s.Cause
	id := strconv.FormatInt(repo, 10)
	switch c.Origin {
	case DownloadOriginQueue, DownloadOriginScannerDatabase:
		return c.Unit, false, false
	case DownloadOriginMirror:
		if c.Unit == "" || (unit != UnitImage && s.Version == "") {
			return "", false, false
		}
		return "m|" + c.Unit + "|" + id + "|" + s.Name + "|" + s.Version, unit == UnitImage, false
	}
	switch {
	case unit == UnitImage && s.Name != "":
		return "c|" + id + "|" + s.Name + "|" + c.Principal, true, s.Version == ""
	case unit == UnitVersion && s.Name != "" && s.Version != "":
		return "c|" + id + "|" + s.Name + "|" + s.Version + "|" + c.Principal, false, false
	}
	return "", false, false
}

// Downloads is the engine's tracker; nil for an engine without one. A run of the download queue holds its unit through it.
func (e *Engine) Downloads() DownloadTracker { return e.o.Downloads }

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
