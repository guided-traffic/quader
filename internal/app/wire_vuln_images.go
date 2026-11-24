package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/buildinfo"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	vmetrics "github.com/guided-traffic/quader/internal/vuln/metrics"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// The wiring of the scanning of container images (R-VULN-033 to R-VULN-037, R-VULN-048, docs/developer/vulnerabilities.md): the service
// behind the kinds vuln-image-scan and vuln-sync-trivy-db, and the wake-up in the commit path. A replica that runs no jobs builds only the
// wake-up. The worker runs no tool (ADR-229): it calls the unit vuln-scanner-trivy for the analysis and the detection.

// imageOutputBytes bounds a file the analyser leaves behind in the scanner (an analysis, a report).
const imageOutputBytes = 512 << 20

// noImageScanner is the result of a run of the image scanning's job in a process that names no scanner: the role all, which builds
// none (Q-ARCH-563). The run touches no row.
const noImageScanner = "no image scanner is configured"

// wireVulnImages builds the area's image scanning.
func (a *App) wireVulnImages(ctx context.Context, in infra, m *vmetrics.Metrics, out *vulnArea) error {
	cfg := a.cfg.Vulnerabilities
	if !cfg.Enabled || !cfg.Images.Enabled {
		return nil
	}
	if in.queue != nil {
		queue := in.queue
		out.wakeImages = func(ctx context.Context, tx meta.Tx) error {
			_, err := queue.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: vuln.KindImageScan, DedupKey: vuln.DedupImageScan})
			return err
		}
	}
	if !a.sub.JobLoops {
		return nil
	}
	evaluation, ok := out.evaluation()
	if !ok {
		return nil
	}
	images := cfg.Images
	if images.Scanner.URL == "" {
		// The role all builds no image scanner (Q-ARCH-563): the job ends at once and touches no row.
		out.noImageScanner = true
		return nil
	}
	log := a.log.With("component", "vuln-images")
	rows := store.NewImages(in.db)
	// The database in use is the source state's; the worker only checks that its repository holds the layer (R-VULN-044, R-VULN-048).
	var database imagescan.Database = databaseInUse{a: a, rows: rows, repo: images.Database.Repository, image: images.Database.Image}
	if override := a.o.Vuln; override != nil && override.ImageDatabase != nil {
		database = override.ImageDatabase
	}
	// >>> the role scanner (R-VULN-041, ADR-212): the call of the scanner and the scan token of each call (wire_vuln_scanner_call.go)
	call, tokens, err := a.scannerCall(ctx, in, log)
	if err != nil {
		return fmt.Errorf("the image scanning: %w", err)
	}
	out.scannerClient = call
	// <<< the role scanner
	var wakeDatabase func(ctx context.Context, tx meta.Tx) error
	var enqueue func(ctx context.Context, tx meta.Tx, delay time.Duration) error
	if in.queue != nil {
		queue := in.queue
		wakeDatabase = func(ctx context.Context, tx meta.Tx) error {
			_, err := queue.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: vuln.KindSyncTrivyDB, DedupKey: vuln.TaskSyncTrivyDB})
			return err
		}
		enqueue = func(ctx context.Context, tx meta.Tx, delay time.Duration) error {
			_, err := queue.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: vuln.KindImageScan, DedupKey: vuln.DedupImageScan, Delay: delay})
			return err
		}
	}
	svc, err := imagescan.New(imagescan.Options{
		DB: in.db, Images: rows, Evaluate: evaluation, Scanner: call, Tokens: tokens, Database: database,
		States: out.states, Enqueue: enqueue, WakeDatabase: wakeDatabase,
		Concurrency: images.Concurrency, RunSlice: images.RunSlice, MaxAttempts: images.MaxAttempts, Delay: images.ScanDelay, Metrics: m, Log: log,
		// What a terminal failure is judged under (Q-VULN-270): the scanner's bounds, which the worker reads from the same section, and the
		// release, whose scanner reads the images and whose analyser it pins.
		Judges: imagescan.Judges{Limits: imagescan.LimitsJudge(imageContentLimits(images)), Release: buildinfo.Get().Version},
	})
	if err != nil {
		return fmt.Errorf("the image scanning: %w", err)
	}
	out.images = svc
	return nil
}

// imageContentLimits are the bounds of R-VULN-036 the scanner applies to an image, from vulnerabilities.images.
func imageContentLimits(images config.VulnerabilityImages) imagescan.ContentLimits {
	return imagescan.ContentLimits{
		MaxLayers: images.MaxLayers, MaxImageBytes: int64(images.MaxImageSize), MaxUncompressedBytes: int64(images.MaxUncompressedSize), MaxEntries: images.MaxEntries,
	}
}

// spoolSpace is the scanner's spool as the image scanning sees it: the room of every analysis and detection is taken from its budget
// (Q-VULN-145).
type spoolSpace struct{ spool *blob.Spool }

// Hold implements [imagescan.Space].
func (s spoolSpace) Hold(n int64) (func(), error) {
	h, err := s.spool.Hold(n)
	if errors.Is(err, blob.ErrSpoolFull) {
		return nil, fmt.Errorf("%w: %w", imagescan.ErrNoSpace, err)
	}
	if err != nil {
		return nil, err
	}
	return h.Release, nil
}

// Adopt implements [imagescan.Space].
func (s spoolSpace) Adopt(n int64) func() { return s.spool.Adopt(n).Release }

// Budget implements [imagescan.Space].
func (s spoolSpace) Budget() int64 { return s.spool.Budget() }

// registerVulnImageJobs registers the kinds and tasks of the image scanning. The scan coalesces: its work is in a table, so one run after
// another loses nothing, and the queue is the advisory sync's, whose slot a long run gives back through its time slice.
func (a *App) registerVulnImageJobs(r *jobs.Registry) {
	scan := func(ctx context.Context, run *jobs.Run) error {
		area := part[vulnArea](a.parts)
		switch {
		case area.noImageScanner:
			return run.SetResult(map[string]string{"skipped": noImageScanner})
		case area.images == nil:
			return run.SetResult(map[string]string{"skipped": "the image scanning is not available on this replica"})
		}
		return area.images.RunScan(ctx, run)
	}
	// The database's task reads the repository the setting names (R-VULN-044); it is on with the image scanning and by nothing else.
	db := func(ctx context.Context, run *jobs.Run) error {
		area := part[vulnArea](a.parts)
		if area.sync == nil {
			return run.SetResult(map[string]string{"skipped": "the vulnerability database is not synced on this replica"})
		}
		return area.sync.RunTrivyDB(ctx, run, run.ID)
	}
	r.RegisterKind(vuln.KindImageScan, jobs.KindSpec{Queue: vuln.QueueVuln, DedupMode: jobs.Coalesce, MaxAttempts: 3, Handler: scan})
	r.RegisterKind(vuln.KindSyncTrivyDB, jobs.KindSpec{Queue: vuln.QueueVuln, DedupMode: jobs.Exclusive, MaxAttempts: 1, Handler: db})
	for _, td := range vuln.TaskDefaults() {
		if td.Name == vuln.TaskImageScan || td.Name == vuln.TaskSyncTrivyDB {
			r.RegisterTask(td.Name, a.vulnTaskSpec(td))
		}
	}
}
