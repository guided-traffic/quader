package proxy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/repository"
)

// recordingTracker records the downloads the engine reports.
type recordingTracker struct {
	mu     sync.Mutex
	starts []DownloadStart
	ends   []string
	bytes  int64
}

func (r *recordingTracker) Begin(s DownloadStart) Download {
	r.mu.Lock()
	r.starts = append(r.starts, s)
	r.mu.Unlock()
	return &recordedDownload{r: r}
}

func (r *recordingTracker) Hold(string) DownloadHold { return nil }

type recordedDownload struct{ r *recordingTracker }

func (d *recordedDownload) Reader(src io.Reader) io.Reader { return countReader{src, d.r} }
func (d *recordedDownload) End(outcome string, _ *remotecuration.Failure) {
	d.r.mu.Lock()
	d.r.ends = append(d.r.ends, outcome)
	d.r.mu.Unlock()
}

type countReader struct {
	src io.Reader
	r   *recordingTracker
}

func (c countReader) Read(p []byte) (int, error) {
	n, err := c.src.Read(p)
	c.r.mu.Lock()
	c.r.bytes += int64(n)
	c.r.mu.Unlock()
	return n, err
}

// R-OPS-049: the engine tells its tracker the facts of a download at the begin (the repository, the package and version the format
// classified, the path, the announced length, the cause: a client's unless the request names another) and counts every byte read from
// the body; a queued request without a version takes the row's. The end maps the fetch's result to its outcome: stored is completed, a
// fetch whose context ended or that a client's fetch pre-empted is cancelled, every other result failed with the class and code of
// R-CUR-011, a body passed to its client unstored failed with the class storage. The engine's whole path (a miss, a collapsed miss, a
// 404, a HEAD, a hit and a document) is test/integration/proxy TestR_OPS_049_OneDownloadPerBodyFromTheUpstream.
func TestR_OPS_049_TheEngineReportsEachArtifactBody(t *testing.T) {
	rem := Remote{Repo: repository.Repository{Name: qname.Repo{Project: "web", Key: "npm-remote"}, Format: "npm"}, id: 7}
	body := strings.Repeat("x", 64<<10)

	t.Run("R-OPS-049 a client's download with its length", func(t *testing.T) {
		tr := &recordingTracker{}
		f := &fetcher{e: &Engine{o: Options{Downloads: tr}}, rq: Request{
			Remote: rem, Package: "left-pad", Version: "1.0.0", Path: "left-pad/-/left-pad-1.0.0.tgz", Caller: CallerFacts{Principal: "p-1"},
			Unit: UnitVersion,
		}}
		resp := &http.Response{ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body))}
		d := f.beginDownload(resp)
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		endDownload(context.Background(), d, flightResult{stored: true})
		want := DownloadStart{
			RepoID: 7, Repo: "web/npm-remote", Format: "npm", Name: "left-pad", Version: "1.0.0",
			Path: "left-pad/-/left-pad-1.0.0.tgz", Total: 64 << 10, Cause: DownloadCause{Origin: DownloadOriginClient, Principal: "p-1"},
			Unit: "c|7|left-pad|1.0.0|p-1",
		}
		if len(tr.starts) != 1 || tr.starts[0] != want || tr.bytes != 64<<10 || len(tr.ends) != 1 || tr.ends[0] != DownloadCompleted {
			t.Fatalf("starts %+v bytes %d ends %v", tr.starts, tr.bytes, tr.ends)
		}
	})

	t.Run("R-OPS-049 a queued blob without a length takes the row's version, names the row and joins the run's unit", func(t *testing.T) {
		tr := &recordingTracker{}
		cause := DownloadCause{
			Origin: DownloadOriginQueue, QueueOrigin: remotecuration.OriginPerson, QueueRow: "row-1", Version: "26.10", Unit: "q|row-1|job-1", Principal: "p-2",
		}
		f := &fetcher{e: &Engine{o: Options{Downloads: tr}}, rq: Request{Remote: rem, Package: "library/ubuntu", Path: "v2/blobs/sha256:00", Cause: &cause, Unit: UnitImage}}
		f.beginDownload(&http.Response{ContentLength: -1, Body: io.NopCloser(strings.NewReader("abc"))})
		if s := tr.starts[0]; s.Version != "26.10" || s.Total != -1 || s.Cause != cause || s.Unit != "q|row-1|job-1" || s.Open || s.Resolve {
			t.Fatalf("%+v", s)
		}
	})

	t.Run("R-OPS-049 an engine without a tracker leaves the body as it is", func(t *testing.T) {
		resp := &http.Response{ContentLength: 3, Body: io.NopCloser(strings.NewReader("abc"))}
		orig := resp.Body
		if d := (&fetcher{e: &Engine{}, rq: Request{Remote: rem}}).beginDownload(resp); d != nil || resp.Body != orig {
			t.Fatal("a download without a tracker")
		}
		endDownload(context.Background(), nil, flightResult{stored: true})
	})

	t.Run("R-OPS-049 the unit of each cause and format", func(t *testing.T) {
		client := DownloadCause{Origin: DownloadOriginClient, Principal: "p-1"}
		for _, c := range []struct {
			name          string
			unit          DownloadUnit
			start         DownloadStart
			key           string
			open, resolve bool
		}{
			{"a client's tarball: the version's files of the caller", UnitVersion, DownloadStart{Name: "left-pad", Version: "1.0.0", Cause: client}, "c|7|left-pad|1.0.0|p-1", false, false},
			{"a client's file of no version: the file alone", UnitVersion, DownloadStart{Name: "", Cause: client}, "", false, false},
			{"a client's file of a format of files", UnitFile, DownloadStart{Name: "a", Version: "1", Cause: client}, "", false, false},
			{"a client's blob: the image of the caller, open, its version resolved", UnitImage, DownloadStart{Name: "library/ubuntu", Cause: client}, "c|7|library/ubuntu|p-1", true, true},
			{"a mirror pass's blob of a tag: the pass's image, open", UnitImage, DownloadStart{Name: "library/ubuntu", Version: "26.10", Cause: DownloadCause{Origin: DownloadOriginMirror, Unit: "job-9", Version: "26.10"}}, "m|job-9|7|library/ubuntu|26.10", true, false},
			{"a mirror pass's tarball", UnitVersion, DownloadStart{Name: "left-pad", Version: "1.0.0", Cause: DownloadCause{Origin: DownloadOriginMirror, Unit: "job-9"}}, "m|job-9|7|left-pad|1.0.0", false, false},
			{"a mirror pass's file of no version", UnitVersion, DownloadStart{Name: "x", Cause: DownloadCause{Origin: DownloadOriginMirror, Unit: "job-9"}}, "", false, false},
			{"the database task's run", UnitImage, DownloadStart{Name: "aquasecurity/trivy-db", Cause: DownloadCause{Origin: DownloadOriginScannerDatabase, Unit: "s|job-3"}}, "s|job-3", false, false},
		} {
			key, open, resolve := unitOf(7, c.unit, c.start)
			if key != c.key || open != c.open || resolve != c.resolve {
				t.Errorf("%s: %q open %v resolve %v", c.name, key, open, resolve)
			}
		}
	})

	t.Run("R-OPS-049 the outcome of each result of a fetch", func(t *testing.T) {
		cancelled, cancel := context.WithCancelCause(context.Background())
		cancel(ErrPreempted)
		for _, c := range []struct {
			name    string
			ctx     context.Context
			res     flightResult
			outcome string
			class   remotecuration.FailureClass
			code    string
		}{
			{"stored", context.Background(), flightResult{stored: true}, DownloadCompleted, "", ""},
			{"stored by a duplicate flight after the cancellation", cancelled, flightResult{stored: true}, DownloadCompleted, "", ""},
			{"pre-empted", cancelled, flightResult{err: context.Canceled}, DownloadCancelled, "", ""},
			{"pre-empted, as the background fetch reports it", context.Background(), flightResult{own: ErrPreempted}, DownloadCancelled, "", ""},
			{
				"a digest mismatch", context.Background(),
				flightResult{err: errs.E(errs.UpstreamFailed, errs.CodeUpstreamIntegrityMismatch, "x")},
				DownloadFailed, remotecuration.ClassIntegrity, errs.CodeUpstreamIntegrityMismatch,
			},
			{"too large", context.Background(), flightResult{err: tooLarge(rem)}, DownloadFailed, remotecuration.ClassBound, errs.CodeUpstreamItemTooLarge},
			{
				"the spool refused it", context.Background(),
				flightResult{own: &BackgroundWait{Reason: WaitSpoolBusy}},
				DownloadFailed,
				remotecuration.ClassStorage, "spool_busy",
			},
			{"passed to its client unstored", context.Background(), flightResult{}, DownloadFailed, remotecuration.ClassStorage, errs.CodeObjectStoreUnavail},
		} {
			outcome, f := downloadOutcome(c.ctx, c.res)
			if outcome != c.outcome || (f == nil) != (c.class == "") || (f != nil && (f.Class != c.class || f.Code != c.code)) {
				t.Errorf("%s: %s %+v", c.name, outcome, f)
			}
		}
	})
}
