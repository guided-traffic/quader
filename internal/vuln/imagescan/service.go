// Package imagescan scans the container images quader stores for known vulnerabilities (docs/spec/vulnerability-scanning.md R-VULN-033 to
// R-VULN-037, R-VULN-041 to R-VULN-043, R-VULN-048). A scan has two steps, kept apart on purpose, and both run in the unit
// vuln-scanner-trivy (ADR-212, ADR-229); the worker runs no tool:
//
//	analysis   the layers of a manifest are read by the analyser and reduced to the list of what the image contains. It needs no database,
//	           runs at most once per manifest digest, in the role scanner (internal/vuln/scanner), which pulls the image through the scan
//	           listener of packages with a scan token; the worker calls it ([Client.Analyse]), checks its answer ([CheckAnalysis]) and
//	           keeps it as answered in vuln_image_sbom.
//	detection  the stored list is sent to the scanner ([Client.Detect]), which compares it with the copy of the vulnerability database its
//	           pod holds, again whenever the database changes. It reads no layer; the worker reads the report in Go under limits.
//
// The result enters the lifecycle of findings through the evaluation (evaluate.Service.ApplyImage), so an image's finding has the
// statuses, triage, report and blocking of a package's. Work lives in the table vuln_image_scan, not in a job's arguments, so a coalesced
// enqueue, a lost worker and a drain lose nothing.
package imagescan

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/advisory"
	"github.com/guided-traffic/quader/internal/vuln/evaluate"
	vmetrics "github.com/guided-traffic/quader/internal/vuln/metrics"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// The working bounds of the job.
const (
	discoverBatch = 1000
	orphanBatch   = 1000
	// retryBackoff is the wait before the first retry of a scan that failed for a reason that may pass; it doubles per attempt. A terminal
	// failure has no back-off: it is due again when what it was judged under changes (Judges, Q-VULN-270).
	retryBackoff = time.Minute
	maxBackoff   = 6 * time.Hour
	// versionsDifferWait is the wait of a row whose detection met two scanner versions in one cycle (R-VULN-048).
	versionsDifferWait = time.Minute
)

// Run is the part of a job's run handle the scan uses. *jobs.Run implements it.
type Run interface {
	SaveProgress(ctx context.Context, done, total int64, unit string) error
	SetResult(v any) error
}

var _ Run = (*jobs.Run)(nil)

// ScanTokens issues the scan token of one manifest (formats/oci.md R-OCI-037): registrytoken.Signer.IssueScan.
type ScanTokens interface {
	IssueScan(repo qname.Repo, name, digest string, now time.Time) (registrytoken.ScanToken, string, error)
}

// Rows are the statements of the job over vuln_image_scan and vuln_image_sbom: *store.Images, or a stand-in of a unit test.
type Rows interface {
	Discover(ctx context.Context, delay time.Duration, limit int) (int64, error)
	Orphans(ctx context.Context, limit int) ([]string, error)
	DueCount(ctx context.Context, dbDigest string) (int64, error)
	Due(ctx context.Context, dbDigest string, retry store.Retry, limit int) ([]store.ImageDue, error)
	NextNew(ctx context.Context) (time.Duration, bool, error)
	Unanalysed(ctx context.Context, retry store.Retry, limit int) ([]store.ImageDue, error)
	SBOM(ctx context.Context, digest [32]byte) (store.StoredAnalysis, bool, error)
	PutSBOM(ctx context.Context, digest [32]byte, a store.StoredAnalysis) error
	DeleteSBOM(ctx context.Context, digest [32]byte) error
	PruneSBOMs(ctx context.Context) (int64, error)
	Failed(ctx context.Context, repoID int64, name string, digest [32]byte, f store.ImageFailure) error
	Deferred(ctx context.Context, repoID int64, name string, digest [32]byte, code, detail string, wait time.Duration) error
	Rearm(ctx context.Context, repoID int64) (int64, error)
}

var _ Rows = (*store.Images)(nil)

// Options configure [New].
type Options struct {
	DB       meta.Runner
	Images   Rows
	Evaluate *evaluate.Service
	// Scanner is the unit vuln-scanner-trivy: its analysis of an image (R-VULN-041) and its detection of a stored analysis (R-VULN-048).
	// Tokens signs the scan token of each analysis.
	Scanner Scanner
	Tokens  ScanTokens
	// Database names the database in use and checks that its repository holds the layer (R-VULN-044).
	Database Database
	// States answers whether the database is complete, from the replica's cache.
	States vuln.States
	// Enqueue wakes the job; it runs in the caller's transaction. Delay postpones the job it enqueues (DatabaseLoadingWait, Q-VULN-201).
	Enqueue func(ctx context.Context, tx meta.Tx, delay time.Duration) error
	// WakeDatabase enqueues vuln-sync-trivy-db at once when the database's repository no longer holds the layer in use (R-VULN-044);
	// it runs in the caller's transaction. nil wakes nothing: the task's schedule brings the layer back.
	WakeDatabase func(ctx context.Context, tx meta.Tx) error
	// Concurrency is the number of calls to the scanner one job has in flight (R-VULN-041).
	Concurrency int
	// RunSlice is how long one job keeps its slot before it hands the rest to the next run, so that an advisory sync waiting on the same
	// queue gets its turn.
	RunSlice time.Duration
	// MaxAttempts bounds the retries of a scan that failed for a reason that may pass.
	MaxAttempts int
	// Judges are what a terminal failure is judged under: the bounds and the release (Q-VULN-270). A row judged under neither any more is
	// due again. An empty Limits stands for the fingerprint of zero bounds, so that a terminal failure always names a judge.
	Judges Judges
	// Delay postpones a new manifest, so that a pull of a remote image has brought its layers.
	Delay   time.Duration
	Metrics *vmetrics.Metrics
	Log     *slog.Logger
	Now     func() time.Time
}

// Service runs the scans.
type Service struct {
	o   Options
	log *slog.Logger
	// scope answers whether a repository takes part in the scanning, and apply records a detection in one transaction; New sets them to
	// the evaluation's, a unit test to stand-ins.
	scope func(ctx context.Context, ref ImageRef) (bool, error)
	apply func(ctx context.Context, ref ImageRef, det Detection, packages int, digest string, scoped bool) error
}

// New returns the service.
func New(o Options) (*Service, error) {
	switch {
	case o.DB == nil || o.Images == nil || o.Evaluate == nil:
		return nil, errors.New("imagescan: the database and the evaluation are required")
	case o.Database == nil || o.Scanner == nil || o.Tokens == nil:
		return nil, errors.New("imagescan: the database in use, the scanner and the scan tokens are required")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 1
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 5
	}
	s := &Service{o: o, log: o.Log}
	s.scope, s.apply = s.scoped, s.recordDetection
	return s, nil
}

// errDatabaseLoading ends a run: every scanner a detection reached holds no copy of the database in use yet (Q-VULN-201).
var errDatabaseLoading = errors.New("the scanners load the database in use")

// RunScan is the handler of vuln-image-scan (R-VULN-034): it adds the manifests that have no scan row, resolves the findings of manifests
// that are gone, and works off what is due. It returns when nothing is due, its time slice ended (the rest then in a job of its own), or
// the scanners still load the database in use (the next run then 30 s later, no row changed). A context that ends stops the run; the rows
// say how far it came, so a released or lost job loses nothing.
func (s *Service) RunScan(ctx context.Context, run Run) error {
	if err := s.discover(ctx); err != nil {
		return err
	}
	if err := s.resolveOrphans(ctx); err != nil {
		return err
	}
	digest, err := s.o.Database.InUse(ctx)
	switch {
	case errors.Is(err, ErrNoDatabase):
		// The analysis does not wait for the database (Q-VULN-168): what has no analysis is analysed, and the rows stay pending.
		s.count("no_database")
		return s.analyseOnly(ctx, run, "no vulnerability database has been imported yet; the detection waits for it")
	case errors.Is(err, ErrLayerMissing):
		// The repository lost the layer in use (R-VULN-044): the run acts as before the first database, and the task that makes the
		// repository hold it is woken at once instead of at its next slot.
		s.count("no_database")
		s.log.WarnContext(ctx, "the vulnerability database's repository does not hold the layer in use; the database task is woken", "error", err)
		if s.o.WakeDatabase != nil {
			if werr := s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.o.WakeDatabase(ctx, tx) }); werr != nil {
				return werr
			}
		}
		return s.analyseOnly(ctx, run, err.Error()+"; vuln-sync-trivy-db is woken to store it again, and the detection waits for it")
	case err != nil:
		return fmt.Errorf("the vulnerability database: %w", err)
	}
	total, err := s.o.Images.DueCount(ctx, digest)
	if err != nil {
		return err
	}
	if _, err := s.o.Images.PruneSBOMs(ctx); err != nil {
		s.log.WarnContext(ctx, "pruning the analyses failed", "error", err)
	}
	deadline := s.o.Now().Add(s.o.RunSlice)
	var done int64
	for s.o.RunSlice <= 0 || s.o.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		due, err := s.o.Images.Due(ctx, digest, s.retry(), s.o.Concurrency)
		if err != nil {
			return err
		}
		if len(due) == 0 {
			return s.ended(ctx, run, map[string]any{"done": done, "database": digest})
		}
		err = s.scanBatch(ctx, due, digest)
		if errors.Is(err, errDatabaseLoading) {
			return s.waitForDatabase(ctx, run, digest, done)
		}
		if err != nil {
			return err
		}
		done += int64(len(due))
		if err := run.SaveProgress(ctx, done, max(total, done), "manifests"); err != nil {
			return err
		}
	}
	return s.continued(ctx, run, 0, map[string]any{"done": done, "database": digest, "continued": true})
}

// waitForDatabase ends a run whose detection met scanners that load the database in use (Q-VULN-201): the condition belongs to the
// installation, not to a row, so no row changes and the next run comes after DatabaseLoadingWait.
func (s *Service) waitForDatabase(ctx context.Context, run Run, digest string, done int64) error {
	s.count(CodeDatabaseLoading)
	s.log.InfoContext(ctx, "the scanners load the vulnerability database in use; the scan runs again later", "database", digest, "after", DatabaseLoadingWait)
	return s.continued(ctx, run, DatabaseLoadingWait, map[string]any{
		"done": done, "database": digest, "waiting": CodeDatabaseLoading + ": the scanners hold no copy of the database in use yet; the scan runs again in 30 seconds",
	})
}

// ended closes a run that found nothing due (Q-VULN-268): a manifest it discovered still waits out its scan delay, so the run hands it to
// a job of its own at the time it becomes due, instead of leaving it to the task's next slot. A push during a run is discovered by the
// next run and waits there the same way.
func (s *Service) ended(ctx context.Context, run Run, result map[string]any) error {
	wait, ok, err := s.o.Images.NextNew(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return run.SetResult(result)
	}
	result["next"] = wait.Round(time.Second).String()
	return s.continued(ctx, run, wait, result)
}

// continued hands the rest of the work to a job of its own, after delay and after whatever waits on the queue.
func (s *Service) continued(ctx context.Context, run Run, delay time.Duration, result map[string]any) error {
	if s.o.Enqueue != nil {
		if err := s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.o.Enqueue(ctx, tx, delay) }); err != nil {
			return err
		}
	}
	return run.SetResult(result)
}

// analyseOnly is a run without a database (Q-VULN-168): the scanner analyses what has no analysis yet and the rows stay pending, so the
// first database detects without a layer read and nothing reads as clean meanwhile. why is the reason the run's result names.
func (s *Service) analyseOnly(ctx context.Context, run Run, why string) error {
	deadline := s.o.Now().Add(s.o.RunSlice)
	seen := map[[32]byte]bool{}
	var done int64
	for s.o.RunSlice <= 0 || s.o.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		due, err := s.o.Images.Unanalysed(ctx, s.retry(), s.o.Concurrency+len(seen))
		if err != nil {
			return err
		}
		var batch []store.ImageDue
		for _, d := range due {
			if !seen[d.Digest] && len(batch) < s.o.Concurrency {
				seen[d.Digest] = true
				batch = append(batch, d)
			}
		}
		if len(batch) == 0 {
			return run.SetResult(map[string]any{"analysed": done, "skipped": why})
		}
		if err := s.each(batch, func(d store.ImageDue) error {
			if _, err := s.analyse(ctx, imageRefOf(d), d.Repo); err != nil {
				return s.record(ctx, d, err)
			}
			return nil
		}); err != nil {
			return err
		}
		done += int64(len(batch))
	}
	return s.continued(ctx, run, 0, map[string]any{"analysed": done, "continued": true})
}

func imageRefOf(d store.ImageDue) ImageRef {
	return ImageRef{RepoID: d.RepoID, Repo: d.Repo.String(), Name: d.Name, Digest: d.Digest}
}

func (s *Service) discover(ctx context.Context) error {
	for {
		n, err := s.o.Images.Discover(ctx, s.o.Delay, discoverBatch)
		if err != nil {
			return fmt.Errorf("the discovery of manifests: %w", err)
		}
		if n < discoverBatch {
			return nil
		}
	}
}

func (s *Service) resolveOrphans(ctx context.Context) error {
	for {
		ids, err := s.o.Images.Orphans(ctx, orphanBatch)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.o.Evaluate.ResolveImageFindings(ctx, tx, ids) }); err != nil {
			return err
		}
		if len(ids) < orphanBatch {
			return nil
		}
	}
}

// scanBatch scans the manifests in parallel. An image whose scan fails is recorded and the run goes on; only an error of the worker's own
// (the database, a released context) or errDatabaseLoading ends it.
func (s *Service) scanBatch(ctx context.Context, due []store.ImageDue, digest string) error {
	return s.each(due, func(d store.ImageDue) error { return s.scanOne(ctx, d, digest) })
}

// each runs fn for every manifest of a batch at once, so that the batch's calls to the scanner are in flight together
// (vulnerabilities.images.concurrency), and returns the first error; errDatabaseLoading counts last, so that an error of the worker's
// own is not hidden behind it.
func (s *Service) each(due []store.ImageDue, fn func(store.ImageDue) error) error {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for _, d := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(d); err != nil {
				mu.Lock()
				if firstErr == nil || errors.Is(firstErr, errDatabaseLoading) {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// scanOne works off one manifest. A [ScanError] and a detection that cannot be read are recorded on the row: the absence of findings of a
// manifest that could not be scanned must never read as clean. errDatabaseLoading changes no row.
func (s *Service) scanOne(ctx context.Context, d store.ImageDue, digest string) error {
	started := s.o.Now()
	err := s.detect(ctx, d, digest)
	switch {
	case err == nil:
		s.count("scanned")
		s.observe(started)
		return nil
	case errors.Is(err, errDatabaseLoading):
		return err
	}
	return s.record(ctx, d, err)
}

// record records a scan that did not finish: put off for a reason of the installation, failed for a reason of the image, or failed for a
// reason the worker's log names. An ended context ends the run instead.
func (s *Service) record(ctx context.Context, d store.ImageDue, err error) error {
	var se *ScanError
	switch {
	case ctx.Err() != nil:
		return context.Cause(ctx)
	case errors.As(err, &se) && se.Defer > 0:
		s.count(se.Code)
		return s.recordDeferred(ctx, d, se)
	case errors.As(err, &se):
		s.count(se.Code)
		return s.recordFailure(ctx, d, se.Code, se.Detail, se.Terminal)
	}
	s.count("error")
	s.log.ErrorContext(ctx, "an image could not be scanned", "repository", d.Repo.String(), "name", d.Name, "digest", imageRefOf(d).DigestString(), "error", err)
	return s.recordFailure(ctx, d, "scan_error", "the scan failed; the worker's log has the cause", false)
}

// recordFailure records a scan that did not finish. A failure that may pass is due again after a doubling back-off, up to MaxAttempts; a
// terminal one is not retried after a time: it names what it was judged under and is due again when that changes (Q-VULN-270).
func (s *Service) recordFailure(ctx context.Context, d store.ImageDue, code, detail string, terminal bool) error {
	f := store.ImageFailure{Code: code, Detail: detail, Backoff: min(retryBackoff<<min(d.Attempts, 10), maxBackoff)}
	if terminal {
		f.Backoff, f.Judged = 0, s.o.Judges.judgedUnder(code)
	}
	s.log.WarnContext(ctx, "an image could not be scanned", "repository", d.Repo.String(), "name", d.Name, "code", code, "judged", f.Judged)
	return s.o.Images.Failed(ctx, d.RepoID, d.Name, d.Digest, f)
}

// retry is what makes a failed row due again: the attempts of a failure that may pass, the judges of a terminal one.
func (s *Service) retry() store.Retry {
	return store.Retry{MaxAttempts: s.o.MaxAttempts, Judges: s.o.Judges.all()}
}

// recordDeferred records a scan that did not start or did not finish for a reason of the installation (R-VULN-034, Q-VULN-162): the row
// says why, stays pending and is due again after the wait, with its attempts untouched, because nothing about the image failed.
func (s *Service) recordDeferred(ctx context.Context, d store.ImageDue, se *ScanError) error {
	s.log.WarnContext(ctx, "an image scan was put off for a reason of the installation", "repository", d.Repo.String(), "name", d.Name, "code", se.Code, "detail", se.Detail)
	return s.o.Images.Deferred(ctx, d.RepoID, d.Name, d.Digest, se.Code, se.Detail, se.Defer)
}

func (s *Service) count(result string) {
	if s.o.Metrics != nil {
		s.o.Metrics.ImageScans.WithLabelValues(result).Inc()
	}
}

func (s *Service) observe(started time.Time) {
	if s.o.Metrics != nil {
		s.o.Metrics.ImageScanDuration.Observe(s.o.Now().Sub(started).Seconds())
	}
}

// detect is one manifest: the analysis, once per digest, and the detection in the scanner (R-VULN-048). A scanner of another analyser
// version than the stored analysis's answers analysis_outdated: the worker has the image analysed again, keeps the new analysis and
// detects once more; a second analysis_outdated of the cycle puts the row off (scanner_versions_differ). A detection the analyser refused
// deletes the stored analysis, so that the next attempt analyses the image again (Q-VULN-202).
func (s *Service) detect(ctx context.Context, d store.ImageDue, digest string) error {
	ref := imageRefOf(d)
	scoped, err := s.scope(ctx, ref)
	if err != nil {
		return err
	}
	if !scoped {
		// Off for this repository: the row says the current database, so the job does not come back; R-VULN-001 shows not_covered and a
		// change of the setting re-arms the row.
		return s.apply(ctx, ref, Detection{}, 0, digest, false)
	}
	a, found, err := s.o.Images.SBOM(ctx, ref.Digest)
	if err != nil {
		return err
	}
	if !found {
		if a, err = s.analyse(ctx, ref, d.Repo); err != nil {
			return err
		}
	}
	for outdated := 0; ; outdated++ {
		call := DetectCall{Repository: d.Repo, Name: d.Name, Digest: ref.DigestString(), Database: digest, Analyser: a.Analyser, Analysis: a.SBOM}
		det, err := s.o.Scanner.Detect(ctx, call)
		var se *ScanError
		switch {
		case err == nil:
			s.log.DebugContext(ctx, "the scanner detected an image", "call", call, "advisories", len(det.Advisories))
			return s.apply(ctx, ref, det, a.Packages, digest, true)
		case !errors.As(err, &se):
			return err
		case se.Code == CodeDatabaseLoading:
			return fmt.Errorf("%w: %s", errDatabaseLoading, se.Detail)
		case se.Code == CodeAnalysisOutdated && outdated == 0:
			s.log.InfoContext(ctx, "the scanner's analyser is of another version than the stored analysis; the image is analysed again", "call", call, "detail", se.Detail)
			if a, err = s.analyse(ctx, ref, d.Repo); err != nil {
				return err
			}
			continue
		case se.Code == CodeAnalysisOutdated:
			return &ScanError{
				Code: CodeScannerVersionsDiffer, Defer: versionsDifferWait,
				Detail: "scanners of two analyser versions answer: the analysis of one was outdated in the other's detection (" + se.Detail + ")",
			}
		case se.RefusedAnalysis:
			if derr := s.o.Images.DeleteSBOM(ctx, ref.Digest); derr != nil {
				return derr
			}
		}
		s.log.InfoContext(ctx, "the scanner did not detect an image", "call", call, "code", se.Code)
		return err
	}
}

func (s *Service) scoped(ctx context.Context, ref ImageRef) (bool, error) {
	var ok bool
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) (err error) {
		ok, err = s.o.Evaluate.ImageScope(ctx, tx, ref.RepoID)
		return err
	})
	return ok, err
}

// analyse calls the scanner for one manifest, checks its answer and keeps it as answered (R-VULN-043): one analysis per digest, which a
// new one replaces. The call is logged with the repository, the image, the digest, the code and the scan ID, never the token (R-VULN-041).
func (s *Service) analyse(ctx context.Context, ref ImageRef, repo qname.Repo) (store.StoredAnalysis, error) {
	digest := ref.DigestString()
	tok, sid, err := s.o.Tokens.IssueScan(repo, ref.Name, digest, s.o.Now())
	if err != nil {
		return store.StoredAnalysis{}, fmt.Errorf("the scan token of %s@%s: %w", ref.Name, digest, err)
	}
	call := Call{Repository: repo, Name: ref.Name, Digest: digest, Token: tok, ScanID: sid}
	a, err := s.o.Scanner.Analyse(ctx, call)
	if err != nil {
		var se *ScanError
		if errors.As(err, &se) {
			s.log.InfoContext(ctx, "the scanner did not analyse an image", "call", call, "code", se.Code)
		}
		return store.StoredAnalysis{}, err
	}
	packages, err := CheckAnalysis(a.Document)
	if err != nil {
		s.log.WarnContext(ctx, "the scanner's analysis of an image was refused", "call", call, "error", err)
		return store.StoredAnalysis{}, err
	}
	gz, err := gzipBytes(a.Document)
	if err != nil {
		return store.StoredAnalysis{}, err
	}
	if int64(len(gz)) > MaxStoredAnalysisBytes {
		return store.StoredAnalysis{}, &ScanError{
			Code: CodeReportTooLarge, Terminal: true,
			Detail: fmt.Sprintf("the analysis is %d bytes compressed, more than the %d a stored analysis may hold", len(gz), MaxStoredAnalysisBytes),
		}
	}
	stored := store.StoredAnalysis{SBOM: gz, Analyser: a.AnalyserVersion, Packages: packages}
	if err := s.o.Images.PutSBOM(ctx, ref.Digest, stored); err != nil {
		return store.StoredAnalysis{}, err
	}
	s.log.DebugContext(ctx, "the scanner analysed an image", "call", call, "analyser", a.AnalyserVersion, "bytes", len(a.Document), "packages", packages)
	return stored, nil
}

// recordDetection applies a detection in one transaction: the advisories, the findings and the scan row.
func (s *Service) recordDetection(ctx context.Context, ref ImageRef, det Detection, packages int, digest string, scoped bool) error {
	return s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		images, err := store.ImageTxOf(tx)
		if err != nil {
			return err
		}
		if scoped {
			if err := s.applyFindings(ctx, tx, images, ref, det, digest); err != nil {
				return err
			}
		}
		return images.Scanned(ctx, store.ImageApplied{
			RepoID: ref.RepoID, Name: ref.Name, Digest: ref.Digest, DBDigest: digest, OSFamily: det.OSFamily, OSName: det.OSName, EOSL: det.EOSL, Packages: packages,
		})
	})
}

func (s *Service) applyFindings(ctx context.Context, tx meta.Tx, images *store.ImageTx, ref ImageRef, det Detection, digest string) error {
	ids := make([]string, 0, len(det.Advisories))
	for _, a := range det.Advisories {
		ids = append(ids, a.ID)
	}
	kev, err := images.KnownExploited(ctx, ids)
	if err != nil {
		return err
	}
	matches := make([]evaluate.ImageMatch, 0, len(det.Advisories))
	for _, a := range det.Advisories {
		if err := images.UpsertAdvisory(ctx, storedAdvisory(a)); err != nil {
			return err
		}
		m, err := matchOf(a, kev[a.ID])
		if err != nil {
			return err
		}
		matches = append(matches, m)
	}
	complete := s.o.States == nil || s.o.States.State(advisory.EcosystemOCI).Complete
	return s.o.Evaluate.ApplyImage(ctx, tx, evaluate.ImageApply{
		RepoID: ref.RepoID, Name: ref.Name, Digest: ref.DigestString(), Trigger: vuln.TriggerAdvisory, Matches: matches, DataComplete: complete && digest != "",
	})
}

func matchOf(a Advisory, known bool) (evaluate.ImageMatch, error) {
	comps, err := json.Marshal(a.Components)
	if err != nil {
		return evaluate.ImageMatch{}, err
	}
	fixed := a.FixedVersions()
	return evaluate.ImageMatch{
		ID: a.ID, Severity: a.Severity, CVSS: a.CVSSScore, KnownExploited: known, ModifiedUs: microsOf(a.Modified),
		FixAvailable: len(fixed) > 0, Fixed: fixed, Components: comps,
	}, nil
}

func storedAdvisory(a Advisory) store.ImageAdvisory {
	sum := sha256.Sum256([]byte(a.ID + "\x00" + a.Title + "\x00" + a.Description + "\x00" + string(a.Severity) + "\x00" + a.CVSSVector + "\x00" + a.Modified.Format(time.RFC3339Nano)))
	src := "advisory"
	if a.CVSSScore >= 0 && a.SeveritySource == "" {
		src = "cvss"
	}
	return store.ImageAdvisory{
		ID: a.ID, Summary: a.Title, Details: a.Description, Modified: orEpoch(a.Modified), Published: a.Published, Severity: a.Severity, SeveritySource: src,
		CVSSVersion: a.CVSSVersion, CVSSVector: a.CVSSVector, CVSSScore: a.CVSSScore, References: a.References, CWEs: a.CWEs, RecordSHA256: sum[:],
	}
}

func orEpoch(t time.Time) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t
}

func microsOf(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Rescan is the image half of vuln-rescan (R-VULN-011): with the scanning off the repository's findings resolve, and otherwise every manifest
// of it is due again, whose analysis is kept.
func (s *Service) Rescan(ctx context.Context, repoID int64, enabled bool) error {
	if !enabled {
		return s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.o.Evaluate.ResolveImageRepository(ctx, tx, repoID) })
	}
	if _, err := s.o.Images.Rearm(ctx, repoID); err != nil {
		return err
	}
	if s.o.Enqueue != nil {
		return s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.o.Enqueue(ctx, tx, 0) })
	}
	return nil
}
