// Package scanner is the role scanner of the kind trivy, the unit vuln-scanner-trivy (docs/spec/architecture.md R-ARCH-036,
// docs/spec/vulnerability-scanning.md R-VULN-041, R-VULN-042 and R-VULN-048, ADR-212, ADR-229). It answers the worker's two calls: the
// analysis of one manifest, for which it pulls the image through the scan listener of packages with the call's scan token, checks and
// bounds every byte (imagescan.BuildLayout, R-VULN-036), runs the pinned analyser in a process group of its own and answers the
// CycloneDX document; and the detection of a stored analysis (detect.go), for which it runs the analyser against the copy of the
// database its pod holds read-only and answers the report. It holds no secret of the database, the object store or Valkey and no key of
// the key ring, and keeps nothing of a call between two calls beside the database it detects with (R-VULN-049).
package scanner

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// The bounds of one analysis.
const (
	// analysisMargin is held on top of the compressed size of the image: the analysis, its gzip and the files the analyser keeps in its
	// working directory (R-VULN-036, Q-VULN-145).
	analysisMargin = int64(64) << 20
	// AnalysisDirPrefix names the working directory of an analysis below scan_dir; a process that starts removes what one that stopped
	// mid-analysis left.
	AnalysisDirPrefix = "analysis-"
)

// imageName is the grammar of an image name (R-OCI-002): components of lower-case letters and digits with separators, joined by '/'.
var imageName = regexp.MustCompile(`^[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*)*$`)

// Options configure [New].
type Options struct {
	// Analyser runs the analysis: imagescan.Trivy, or the fake of a test.
	Analyser imagescan.Analyser
	// Registry is the scan listener the scanner pulls from (vulnerabilities.images.registry.url): its configuration, never a member
	// of a call.
	Registry Registry
	// Space is the scanner's spool: an analysis holds the compressed size of its image plus 64 MiB before it writes a file.
	Space imagescan.Space
	// ScanDir is the root of the working directories of the analyses and the detections (vulnerabilities.images.scan_dir).
	ScanDir string
	// DatabaseDir is the directory the loader of the pod writes the database into, read-only to the scanner
	// (vulnerabilities.images.database.dir, R-VULN-049); empty answers every detection database_loading.
	DatabaseDir string
	// Slots is the number of calls at a time, analyses and detections together (vulnerabilities.images.scanner.slots, Q-VULN-204).
	Slots int
	// PullTimeout bounds the pull of one image (vulnerabilities.images.pull_timeout), the lifetime of its scan token.
	PullTimeout time.Duration
	// Content bounds what is read of an image; Run bounds the analyser.
	Content imagescan.ContentLimits
	Run     imagescan.Limits
	// Reaper ends the leftover processes of the container after an analysis, while no other runs (Q-ARCH-530); nil reaps nothing.
	Reaper  *Reaper
	Metrics *Metrics
	Log     *slog.Logger
	Now     func() time.Time
}

// Service is the scanner.
type Service struct {
	o     Options
	slots chan struct{}
	// mu is held by the reaper, which runs only while no slot is taken.
	mu       sync.Mutex
	busy     atomic.Int64
	draining atomic.Bool
	// stop ends the calls still running when the shutdown timeout comes (Stop).
	stop       context.Context
	stopCancel context.CancelFunc
}

// New returns the scanner.
func New(o Options) (*Service, error) {
	switch {
	case o.Analyser == nil:
		return nil, errors.New("scanner: the analyser is required")
	case o.Space == nil:
		return nil, errors.New("scanner: the spool is required")
	case o.Registry.URL == "":
		return nil, errors.New("scanner: the scan listener's address is required (vulnerabilities.images.registry.url)")
	case o.ScanDir == "":
		return nil, errors.New("scanner: the working directory is required (vulnerabilities.images.scan_dir)")
	}
	if o.Slots < 1 {
		o.Slots = 1
	}
	if o.PullTimeout <= 0 {
		o.PullTimeout = 10 * time.Minute
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	s := &Service{o: o, slots: make(chan struct{}, o.Slots)}
	s.stop, s.stopCancel = context.WithCancel(context.Background())
	return s, nil
}

// Clean removes the working directories an analysis left when its process stopped (a crash, an eviction): a container that restarts
// keeps its volume. It runs at start, before the first call.
func (s *Service) Clean() error {
	if err := os.MkdirAll(s.o.ScanDir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.o.ScanDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && (strings.HasPrefix(e.Name(), AnalysisDirPrefix) || strings.HasPrefix(e.Name(), DetectionDirPrefix)) {
			if err := os.RemoveAll(filepath.Join(s.o.ScanDir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// Drain makes the scanner answer every new call 503 scanner_draining (R-HA-065): the readiness has failed, and the worker puts the
// scan off without an attempt. The analyses in flight go on until Stop.
func (s *Service) Drain() { s.draining.Store(true) }

// Draining reports whether Drain was called.
func (s *Service) Draining() bool { return s.draining.Load() }

// Stop ends the analyses still running at the shutdown timeout: their analysers are killed with their process groups and their calls
// are answered 503 scanner_draining.
func (s *Service) Stop() {
	s.draining.Store(true)
	s.stopCancel()
}

// Busy is the number of slots that hold an analysis or a detection.
func (s *Service) Busy() int { return int(s.busy.Load()) }

// Slots is the number of calls the scanner runs at a time (vulnerabilities.images.scanner.slots).
func (s *Service) Slots() int { return cap(s.slots) }

// AnalyserVersion is the version the analyser answered at the start: the version the scanner reports of itself (R-OPS-048).
func (s *Service) AnalyserVersion() string { return s.o.Analyser.Version() }

// Handler serves POST /scan/v1/analyses (R-VULN-041). The router of the scanner's listener mounts it and the detection's
// (DetectHandler) and nothing else.
func (s *Service) Handler() http.Handler { return http.HandlerFunc(s.serve) }

// callError is a call that did not end in an analysis, with its answer.
type callError struct {
	status   int
	code     string
	detail   string
	terminal bool
}

func (e *callError) Error() string { return e.code + ": " + e.detail }

func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.answer(w, r, &callError{status: http.StatusMethodNotAllowed, code: imagescan.CodeMalformedRequest, detail: "the call is POST"}, nil)
		return
	}
	call, err := decodeCall(r)
	if err != nil {
		s.answer(w, r, &callError{status: http.StatusBadRequest, code: imagescan.CodeMalformedRequest, detail: err.Error()}, nil)
		return
	}
	if s.draining.Load() {
		s.answer(w, r, &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeScannerDraining, detail: "the scanner is draining"}, &call)
		return
	}
	if !s.take() {
		s.answer(w, r, &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeScannerBusy, detail: "every slot of the scanner holds an analysis"}, &call)
		return
	}
	defer s.give()
	sbom, cleanup, err := s.analyse(r.Context(), call)
	defer cleanup()
	if err != nil {
		s.answer(w, r, s.callErrorOf(r.Context(), err), &call)
		return
	}
	s.send(w, r, call, sbom)
}

// take takes a slot without waiting; give gives it back and, as the last one out, lets the reaper end what the analysis left.
func (s *Service) take() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.slots <- struct{}{}:
		s.o.Metrics.slots(int(s.busy.Add(1)))
		return true
	default:
		return false
	}
}

func (s *Service) give() {
	s.mu.Lock()
	defer s.mu.Unlock()
	<-s.slots
	n := s.busy.Add(-1)
	s.o.Metrics.slots(int(n))
	if n == 0 {
		s.o.Reaper.Reap()
	}
}

// call is a decoded call.
type call struct {
	repo   qname.Repo
	name   string
	digest string
	token  string
	sha    [32]byte
}

// LogValue renders the call without its token.
func (c call) LogValue() slog.Value {
	return slog.GroupValue(slog.String("repository", c.repo.String()), slog.String("name", c.name), slog.String("digest", c.digest))
}

// decodeCall reads the body strictly: at most 16 KiB, no unknown member, no trailing data, a repository with a project, an image name of
// the registry's grammar, a digest of 64 hex digits and a registry token.
func decodeCall(r *http.Request) (call, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, imagescan.MaxCallBytes+1))
	if err != nil {
		return call{}, fmt.Errorf("the body cannot be read: %w", err)
	}
	if len(body) > imagescan.MaxCallBytes {
		return call{}, fmt.Errorf("the body exceeds %d bytes", imagescan.MaxCallBytes)
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	var req imagescan.CallRequest
	if err := dec.Decode(&req); err != nil {
		return call{}, fmt.Errorf("the body is not a call: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return call{}, errors.New("the body holds data after the call")
	}
	repo, err := qname.ParseRepo(req.Repository)
	if err != nil {
		return call{}, fmt.Errorf("repository %q is no qualified name <projectKey>/<repoKey>", req.Repository)
	}
	if len(req.Name) > 255 || !imageName.MatchString(req.Name) {
		return call{}, errors.New("name is no image name")
	}
	if !registrytoken.ValidDigest(req.Digest) {
		return call{}, errors.New("digest is no sha256:<64 hex>")
	}
	if !registrytoken.Is(req.Token) {
		return call{}, errors.New("token is no scan token")
	}
	c := call{repo: repo, name: req.Name, digest: req.Digest, token: req.Token}
	raw, _ := hex.DecodeString(strings.TrimPrefix(req.Digest, "sha256:"))
	copy(c.sha[:], raw)
	return c, nil
}

// analyse pulls the image, checks it and runs the analyser, in a working directory of its own whose room is held in the spool before
// its first file is written. It returns the path of the analysis and the cleanup every exit runs: the directory first, then the room.
func (s *Service) analyse(rctx context.Context, c call) (string, func(), error) {
	ctx, cancel := context.WithCancelCause(rctx)
	stop := context.AfterFunc(s.stop, func() { cancel(errStopped) })
	var held func()
	dir := ""
	cleanup := func() {
		stop()
		cancel(nil)
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		if held != nil {
			held()
		}
	}
	if err := os.MkdirAll(s.o.ScanDir, 0o700); err != nil {
		return "", cleanup, err
	}
	d, err := os.MkdirTemp(s.o.ScanDir, AnalysisDirPrefix)
	if err != nil {
		return "", cleanup, err
	}
	dir = d
	admit := func(total int64) error {
		n := total + analysisMargin
		if budget := s.o.Space.Budget(); n > budget {
			return &callError{
				status: http.StatusUnprocessableEntity, code: imagescan.CodeSpoolTooSmall,
				detail: fmt.Sprintf("the analysis needs %d bytes of the scanner's spool and its budget is %d; raise vulnScannerTrivy.spool.sizeLimit or lower vulnerabilities.images.max_image_size", n, budget),
			}
		}
		release, err := s.o.Space.Hold(n)
		if errors.Is(err, imagescan.ErrNoSpace) {
			return &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeScannerBusy, detail: "the scanner's spool has no room for the image now"}
		}
		if err != nil {
			return err
		}
		held = release
		return nil
	}
	pull := s.o.Registry.puller(c.repo, c.name, c.token)
	defer pull.close()
	layout := filepath.Join(dir, "layout")
	pctx, pcancel := context.WithTimeoutCause(ctx, s.o.PullTimeout, errPullTimeout)
	started := s.o.Now()
	err = imagescan.BuildLayout(pctx, pull, imagescan.ImageRef{Repo: c.repo.String(), Name: c.name, Digest: c.sha}, layout, s.o.Content, admit)
	pcancel()
	s.o.Metrics.step(StepPull, s.o.Now().Sub(started).Seconds())
	if err != nil {
		if errors.Is(context.Cause(pctx), errPullTimeout) && ctx.Err() == nil {
			return "", cleanup, &imagescan.ScanError{Code: imagescan.CodeTimeout, Detail: "the pull of the image did not end within vulnerabilities.images.pull_timeout"}
		}
		return "", cleanup, err
	}
	sbom := filepath.Join(dir, "sbom.json")
	started = s.o.Now()
	err = s.o.Analyser.Analyse(ctx, layout, sbom, dir, s.o.Run)
	s.o.Metrics.step(StepAnalyse, s.o.Now().Sub(started).Seconds())
	if err != nil {
		return "", cleanup, err
	}
	_ = os.RemoveAll(layout) // the layers are not needed for the answer
	return sbom, cleanup, nil
}

var (
	errStopped     = errors.New("the scanner stopped")
	errPullTimeout = errors.New("the pull's timeout")
)

// callErrorOf maps what ended an analysis to its answer (R-VULN-041).
func (s *Service) callErrorOf(ctx context.Context, err error) *callError {
	var ce *callError
	var se *imagescan.ScanError
	switch {
	case errors.As(err, &ce):
		return ce
	case errors.Is(context.Cause(ctx), errStopped), errors.Is(err, errStopped):
		return &callError{status: http.StatusServiceUnavailable, code: imagescan.CodeScannerDraining, detail: "the scanner stopped during the analysis"}
	case ctx.Err() != nil:
		// The worker went away (its job was released or drained): the analyser was killed with the call; nobody reads an answer.
		return &callError{status: http.StatusServiceUnavailable, code: ResultCancelled, detail: "the call was cancelled"}
	case errors.As(err, &se) && se.Code == imagescan.CodeSpoolTooSmall:
		return &callError{status: http.StatusUnprocessableEntity, code: se.Code, detail: se.Detail}
	case errors.As(err, &se):
		return &callError{status: http.StatusUnprocessableEntity, code: se.Code, detail: se.Detail, terminal: se.Terminal}
	case errors.Is(err, errPullRefused):
		return &callError{status: http.StatusForbidden, code: imagescan.CodePullRefused, detail: err.Error()}
	case errors.Is(err, errRegistryUnavailable):
		return &callError{status: http.StatusBadGateway, code: imagescan.CodeRegistryUnavailable, detail: err.Error()}
	}
	s.o.Log.Error("an analysis failed", "error", err)
	return &callError{status: http.StatusUnprocessableEntity, code: imagescan.CodeAnalyserFailed, detail: "the analysis failed; the scanner's log has the cause"}
}

// answer writes a problem (R-ARCH-023) with its code and whether the same bytes can never pass, and counts and logs the call. A call
// whose client went away gets no answer.
func (s *Service) answer(w http.ResponseWriter, r *http.Request, ce *callError, c *call) {
	s.o.Metrics.result(ce.code)
	attrs := []any{"code", ce.code}
	if c != nil {
		attrs = append(attrs, "call", *c, "scan_id", scanIDOf(c.token))
	}
	if r.Context().Err() != nil {
		s.o.Log.Info("the worker ended a call", attrs...)
		return
	}
	s.o.Log.Info("a call ended without an analysis", attrs...)
	s.writeProblem(w, ce)
}

// writeProblem writes the problem (R-ARCH-023) of a call that did not end in its answer, with its code and whether the same bytes can
// never pass.
func (s *Service) writeProblem(w http.ResponseWriter, ce *callError) {
	if ce.status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	p := errs.Problem{
		Type: errs.ProblemTypePrefix + ce.code, Title: http.StatusText(ce.status), Status: ce.status, Detail: ce.detail, Code: ce.code,
		Extensions: map[string]any{"terminal": ce.terminal},
	}
	body, _ := json.Marshal(p)
	w.Header().Set("Content-Type", errs.ProblemContentType)
	w.WriteHeader(ce.status)
	_, _ = w.Write(body)
}

// send answers the analysis: the CycloneDX document, gzip on the wire, and the analyser version (R-VULN-041). A document above 256
// MiB is report_too_large.
func (s *Service) send(w http.ResponseWriter, r *http.Request, c call, sbom string) {
	st, err := os.Stat(sbom)
	if err != nil {
		s.answer(w, r, s.callErrorOf(r.Context(), err), &c)
		return
	}
	if st.Size() > imagescan.MaxAnalysisBytes {
		s.answer(w, r, &callError{
			status: http.StatusUnprocessableEntity, code: imagescan.CodeReportTooLarge, terminal: true,
			detail: fmt.Sprintf("the analysis is %d bytes, the limit of the answer is %d", st.Size(), imagescan.MaxAnalysisBytes),
		}, &c)
		return
	}
	f, err := os.Open(sbom) //nolint:gosec // a path below the analysis's own working directory
	if err != nil {
		s.answer(w, r, s.callErrorOf(r.Context(), err), &c)
		return
	}
	defer func() { _ = f.Close() }()
	h := w.Header()
	h.Set("Content-Type", imagescan.AnalysisMediaType)
	h.Set("Content-Encoding", "gzip")
	h.Set(imagescan.AnalyserVersionHeader, s.o.Analyser.Version())
	w.WriteHeader(http.StatusOK)
	zw := gzip.NewWriter(w)
	_, err = io.Copy(zw, f)
	if err == nil {
		err = zw.Close()
	}
	s.o.Metrics.result(ResultAnalysed)
	s.o.Log.Info("an image was analysed", "call", c, "scan_id", scanIDOf(c.token), "bytes", st.Size(), "error", err)
}

// scanIDOf reads the scan ID from a scan token for the log line, without verifying it: the scanner holds no key, and the ID is no
// secret (R-OCI-037). A token that does not parse gives none.
func scanIDOf(token string) string {
	raw := strings.TrimPrefix(token, registrytoken.Prefix)
	encoded, _, ok := strings.Cut(raw, ".")
	if !ok {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	_, rest, _ := strings.Cut(string(payload), ":")
	_, body, _ := strings.Cut(rest, ":")
	var claims struct {
		ScanID string `json:"sid"`
	}
	if json.Unmarshal([]byte(body), &claims) != nil || len(claims.ScanID) > 32 {
		return ""
	}
	return claims.ScanID
}
