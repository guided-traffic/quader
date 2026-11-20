// Package imagescan scans the container images quader stores for known vulnerabilities (docs/spec/vulnerability-scanning.md R-VULN-033 to
// R-VULN-037, R-VULN-041 to R-VULN-043). A scan has two steps, kept apart on purpose, and the cut between them is the cut between two
// roles (ADR-212):
//
//	analysis   the layers of a manifest are read by the analyser and reduced to the list of what the image contains. It needs no database,
//	           runs at most once per manifest digest and analyser version, in the role scanner (internal/vuln/scanner), which pulls the
//	           image through the scan listener of packages with a scan token; the worker calls it ([Client]) and keeps its answer,
//	           written anew ([Rewrite]), in vuln_image_sbom.
//	detection  the list is compared with the vulnerability database through the [Database] seam, in the worker, again whenever the
//	           database changes. It reads no layer.
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
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	discoverBatch  = 1000
	orphanBatch    = 1000
	maxSBOMBytes   = MaxAnalysisBytes
	maxSBOMStored  = 64 << 20
	maxReportBytes = int64(512) << 20
	// retryBackoff is the wait before the first retry of a scan that failed; it doubles per attempt.
	retryBackoff = time.Minute
	maxBackoff   = 6 * time.Hour
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

// Options configure [New].
type Options struct {
	DB       meta.Runner
	Images   *store.Images
	Evaluate *evaluate.Service
	// Scanner is the call of the role scanner, which analyses an image (R-VULN-041); Tokens signs the scan token of each call.
	Scanner Scanner
	Tokens  ScanTokens
	// Analyser runs the detection and names the analyser version whose analyses the worker keeps; Database is the seam of
	// Q-VULN-137.
	Analyser Analyser
	Database Database
	// States answers whether the database is complete, from the replica's cache.
	States vuln.States
	// Enqueue wakes the job; it runs in the caller's transaction.
	Enqueue func(ctx context.Context, tx meta.Tx) error
	// WakeDatabase enqueues vuln-sync-trivy-db at once when the database's repository no longer holds the layer in use (R-VULN-044);
	// it runs in the caller's transaction. nil wakes nothing: the task's schedule brings the layer back.
	WakeDatabase func(ctx context.Context, tx meta.Tx) error
	// ScratchDir is the root of the working directories of the detections, on the worker's local disk.
	ScratchDir string
	// Space is the worker's spool; the room of every detection is taken from it before the detection writes a file (Q-VULN-145).
	Space Space
	// SpaceWait is how long a detection waits for room before it is put off (2 minutes by default), and SpacePoll how often it asks
	// again meanwhile (half a second by default).
	SpaceWait, SpacePoll time.Duration
	// Concurrency is the number of calls to the scanner one job has in flight (R-VULN-041).
	Concurrency int
	// RunSlice is how long one job keeps its slot before it hands the rest to the next run, so that an advisory sync waiting on the same
	// queue gets its turn.
	RunSlice time.Duration
	// MaxAttempts bounds the retries of a scan that failed for a reason that may pass.
	MaxAttempts int
	// Delay postpones a new manifest, so that a pull of a remote image has brought its layers.
	Delay time.Duration
	// Run bounds the analyser's detection.
	Run     Limits
	Metrics *vmetrics.Metrics
	Log     *slog.Logger
	Now     func() time.Time
}

// Service runs the scans.
type Service struct {
	o   Options
	log *slog.Logger
}

// New returns the service.
func New(o Options) (*Service, error) {
	switch {
	case o.DB == nil || o.Images == nil || o.Evaluate == nil:
		return nil, errors.New("imagescan: the database and the evaluation are required")
	case o.Analyser == nil || o.Database == nil || o.Scanner == nil || o.Tokens == nil:
		return nil, errors.New("imagescan: the analyser, the database, the scanner and the scan tokens are required")
	case o.Space == nil:
		return nil, errors.New("imagescan: the spool is required")
	}
	if o.SpaceWait <= 0 {
		o.SpaceWait = defaultSpaceWait
	}
	if o.SpacePoll <= 0 {
		o.SpacePoll = defaultSpacePoll
	}
	if o.Log == nil {
		o.Log = slog.Default()
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
	return &Service{o: o, log: o.Log}, nil
}

// RunScan is the handler of vuln-image-scan (R-VULN-034): it adds the manifests that have no scan row, resolves the findings of manifests
// that are gone, and works off what is due. It returns when nothing is due or its time slice ended, the rest then in a job of its own. A
// context that ends stops the run; the rows say how far it came, so a released or lost job loses nothing.
func (s *Service) RunScan(ctx context.Context, run Run) error {
	if err := s.discover(ctx); err != nil {
		return err
	}
	if err := s.resolveOrphans(ctx); err != nil {
		return err
	}
	access, err := s.o.Database.Acquire(ctx)
	if errors.Is(err, ErrNoDatabase) {
		// The analysis does not wait for the database (Q-VULN-168): what has no analysis is analysed, and the rows stay pending.
		s.count("no_database")
		return s.analyseOnly(ctx, run, "no vulnerability database has been imported yet; the detection waits for it")
	}
	if errors.Is(err, ErrLayerMissing) {
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
	}
	if errors.Is(err, ErrNoSpace) {
		// The images stay as they are; the task asks again at its next slot, when the other work of the spool may have ended.
		s.count(CodeSpoolBusy)
		s.log.WarnContext(ctx, "the vulnerability database cannot be unpacked: the spool has no room", "error", err)
		return run.SetResult(map[string]string{"skipped": "the spool of the worker has no room for the unpacked vulnerability database: " + err.Error()})
	}
	if err != nil {
		return fmt.Errorf("the vulnerability database: %w", err)
	}
	defer access.Release()
	total, err := s.o.Images.DueCount(ctx, access.Digest)
	if err != nil {
		return err
	}
	if _, err := s.o.Images.PruneSBOMs(ctx, s.o.Analyser.Version()); err != nil {
		s.log.WarnContext(ctx, "pruning the analyses failed", "error", err)
	}
	deadline := s.o.Now().Add(s.o.RunSlice)
	var done int64
	for s.o.RunSlice <= 0 || s.o.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		due, err := s.o.Images.Due(ctx, access.Digest, s.o.MaxAttempts, s.o.Concurrency)
		if err != nil {
			return err
		}
		if len(due) == 0 {
			return run.SetResult(map[string]any{"done": done, "database": access.Digest})
		}
		if err := s.scanBatch(ctx, due, access); err != nil {
			return err
		}
		done += int64(len(due))
		if err := run.SaveProgress(ctx, done, max(total, done), "manifests"); err != nil {
			return err
		}
	}
	return s.continued(ctx, run, map[string]any{"done": done, "database": access.Digest, "continued": true})
}

// continued hands the rest of the work to a job of its own once the slice ended, after whatever waits on the queue.
func (s *Service) continued(ctx context.Context, run Run, result map[string]any) error {
	if s.o.Enqueue != nil {
		if err := s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.o.Enqueue(ctx, tx) }); err != nil {
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
		due, err := s.o.Images.Unanalysed(ctx, s.o.Analyser.Version(), s.o.MaxAttempts, s.o.Concurrency+len(seen))
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
			// An analysis that a scanner of another analyser version answered is stored under that version; this run does not take
			// its row again.
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
	return s.continued(ctx, run, map[string]any{"analysed": done, "continued": true})
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
// (the database, a released context) ends it.
func (s *Service) scanBatch(ctx context.Context, due []store.ImageDue, access DatabaseAccess) error {
	return s.each(due, func(d store.ImageDue) error { return s.scanOne(ctx, d, access) })
}

// each runs fn for every manifest of a batch at once, so that the batch's calls to the scanner are in flight together
// (vulnerabilities.images.concurrency), and returns the first error.
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
				if firstErr == nil {
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
// manifest that could not be scanned must never read as clean.
func (s *Service) scanOne(ctx context.Context, d store.ImageDue, access DatabaseAccess) error {
	started := s.o.Now()
	err := s.detect(ctx, imageRefOf(d), d.Repo, access)
	if err == nil {
		s.count("scanned")
		s.observe(started)
		return nil
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

// recordFailure records a scan that did not finish. A terminal failure is not retried: it spends all its attempts at once.
func (s *Service) recordFailure(ctx context.Context, d store.ImageDue, code, detail string, terminal bool) error {
	backoff := min(retryBackoff<<min(d.Attempts, 10), maxBackoff)
	if terminal {
		backoff = 100 * maxBackoff
	}
	s.log.WarnContext(ctx, "an image could not be scanned", "repository", d.Repo.String(), "name", d.Name, "code", code)
	return s.o.Images.Failed(ctx, d.RepoID, d.Name, d.Digest, code, detail, backoff)
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

// detect is one manifest: the analysis, once, and the detection.
func (s *Service) detect(ctx context.Context, ref ImageRef, repo qname.Repo, access DatabaseAccess) error {
	// The room of the detection is held from the moment its files start to exist until they are gone: the deferred calls run in the
	// opposite order, so the scratch directory is removed first and the room is given back after it, and every exit (a failure, a cancelled
	// run, a drain) passes through both. The image itself is the scanner's to hold: the worker holds the analysis and the report.
	var held lease
	defer held.Release()
	if err := os.MkdirAll(s.o.ScratchDir, 0o700); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(s.o.ScratchDir, "scan-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	scoped, err := s.scoped(ctx, ref)
	if err != nil {
		return err
	}
	if !scoped {
		// Off for this repository: the row says the current database, so the job does not come back; R-VULN-001 shows not_covered and a
		// change of the setting re-arms the row.
		return s.recordDetection(ctx, ref, Detection{}, 0, access, false)
	}
	if err := s.admit(ctx, &held, scanMargin, access.Held); err != nil {
		return err
	}
	sbomPath := filepath.Join(scratch, "sbom.json")
	if err := s.analysis(ctx, ref, repo, sbomPath); err != nil {
		return err
	}
	reportPath := filepath.Join(scratch, "report.json")
	if err := s.o.Analyser.Detect(ctx, sbomPath, reportPath, scratch, access, s.o.Run); err != nil {
		return err
	}
	f, err := os.Open(reportPath) //nolint:gosec // a path under the scan's own scratch directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	det, err := ParseReport(f, maxReportBytes)
	if err != nil {
		return &ScanError{Code: CodeAnalyserFailed, Detail: err.Error()}
	}
	return s.recordDetection(ctx, ref, det, packagesOf(sbomPath), access, true)
}

func (s *Service) scoped(ctx context.Context, ref ImageRef) (bool, error) {
	var ok bool
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) (err error) {
		ok, err = s.o.Evaluate.ImageScope(ctx, tx, ref.RepoID)
		return err
	})
	return ok, err
}

// analysis puts the list of what the image holds at sbomPath: from the stored analysis when one exists for this manifest and analyser,
// else by calling the scanner, which pulls the image and runs the analyser (R-VULN-041), writing its answer anew and storing it
// (R-VULN-043).
func (s *Service) analysis(ctx context.Context, ref ImageRef, repo qname.Repo, sbomPath string) error {
	stored, found, err := s.o.Images.SBOM(ctx, ref.Digest, s.o.Analyser.Version())
	if err != nil {
		return err
	}
	if found {
		// No layer is read: the list and the report are all the detection writes.
		return gunzipTo(stored, sbomPath)
	}
	doc, err := s.analyse(ctx, ref, repo)
	if err != nil {
		return err
	}
	return os.WriteFile(sbomPath, doc, 0o600)
}

// analyse calls the scanner for one manifest and keeps its answer written anew: the stored analysis is the document the detection reads
// (R-VULN-043). The call is logged with the repository, the image, the digest, the code and the scan ID, never the token (R-VULN-041).
func (s *Service) analyse(ctx context.Context, ref ImageRef, repo qname.Repo) ([]byte, error) {
	digest := ref.DigestString()
	tok, sid, err := s.o.Tokens.IssueScan(repo, ref.Name, digest, s.o.Now())
	if err != nil {
		return nil, fmt.Errorf("the scan token of %s@%s: %w", ref.Name, digest, err)
	}
	call := Call{Repository: repo, Name: ref.Name, Digest: digest, Token: tok, ScanID: sid}
	a, err := s.o.Scanner.Analyse(ctx, call)
	if err != nil {
		var se *ScanError
		if errors.As(err, &se) {
			s.log.InfoContext(ctx, "the scanner did not analyse an image", "call", call, "code", se.Code)
		}
		return nil, err
	}
	doc, err := Rewrite(a.Document)
	if err != nil {
		s.log.WarnContext(ctx, "the scanner's analysis of an image was refused", "call", call, "error", err)
		return nil, err
	}
	s.log.DebugContext(ctx, "the scanner analysed an image", "call", call, "analyser", a.AnalyserVersion, "bytes", len(doc))
	gz, err := gzipBytes(doc)
	if err != nil {
		return nil, err
	}
	if int64(len(gz)) > maxSBOMStored {
		s.log.WarnContext(ctx, "the analysis of an image is too large to keep; the next detection calls the scanner again", "digest", digest, "bytes", len(gz))
		return doc, nil
	}
	if err := s.o.Images.PutSBOM(ctx, ref.Digest, a.AnalyserVersion, gz); err != nil {
		return nil, err
	}
	return doc, nil
}

// recordDetection applies a detection in one transaction: the advisories, the findings and the scan row.
func (s *Service) recordDetection(ctx context.Context, ref ImageRef, det Detection, packages int, access DatabaseAccess, scoped bool) error {
	return s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		images, err := store.ImageTxOf(tx)
		if err != nil {
			return err
		}
		if scoped {
			if err := s.applyFindings(ctx, tx, images, ref, det, access); err != nil {
				return err
			}
		}
		return images.Scanned(ctx, store.ImageApplied{
			RepoID: ref.RepoID, Name: ref.Name, Digest: ref.Digest, DBDigest: access.Digest, OSFamily: det.OSFamily, OSName: det.OSName, EOSL: det.EOSL, Packages: packages,
		})
	})
}

func (s *Service) applyFindings(ctx context.Context, tx meta.Tx, images *store.ImageTx, ref ImageRef, det Detection, access DatabaseAccess) error {
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
		RepoID: ref.RepoID, Name: ref.Name, Digest: ref.DigestString(), Trigger: vuln.TriggerAdvisory, Matches: matches, DataComplete: complete && access.Digest != "",
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

func gunzipTo(gz []byte, path string) error {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // a path under the scan's own scratch directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, io.LimitReader(zr, maxSBOMBytes))
	return err
}

// packagesOf counts the packages in a CycloneDX document: how much an analysis found, which is what tells an image without a known
// distribution from one that is clean. A document that cannot be read counts as none.
func packagesOf(path string) int {
	f, err := os.Open(path) //nolint:gosec // a path under the scan's own scratch directory
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	var doc struct {
		Components []struct {
			Type string `json:"type"`
		} `json:"components"`
	}
	if err := json.NewDecoder(io.LimitReader(f, maxSBOMBytes)).Decode(&doc); err != nil {
		return 0
	}
	n := 0
	for _, c := range doc.Components {
		if c.Type == "library" || c.Type == "application" {
			n++
		}
	}
	return n
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
		return s.o.DB.InTx(ctx, func(tx meta.Tx) error { return s.o.Enqueue(ctx, tx) })
	}
	return nil
}
