package report

import (
	"context"
	"slices"
	"time"

	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/advisory"
	"github.com/guided-traffic/quader/internal/vuln/evaluate"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// The state of a version's check by its cause (R-VULN-039, Q-VULN-274): what a list cell, the version page's card and the security screens
// say instead of a bare "N/A". The repository decides most causes (the settings, the format, the scope of R-VULN-010, the advisory data);
// an image's scan decides the rest, per manifest (R-VULN-034), so that every tag of one image says the same.

// Presence is what a reader of the report knows of the image scanners of the installation: whether one is connected now.
type Presence int

const (
	// PresenceUnknown: the replica cannot tell (its scanners do not report themselves); a row's own code still names a scanner that
	// could not be reached.
	PresenceUnknown Presence = iota
	// PresenceNone: no scanner is configured, or none of those that report themselves is ready.
	PresenceNone
	// PresenceReady: a scanner is connected and ready.
	PresenceReady
)

// Scanners answers whether a scanner of images is connected; the report asks it at most once per answer, and only for an answer with an
// image that waits for its scan.
type Scanners interface {
	ImageScanners(ctx context.Context) (Presence, error)
}

// Check is the state of a version's check with what a screen says beside it.
type Check struct {
	State vuln.CheckState
	// Since is when the image was queued (CheckQueued) or its scan started (CheckScanning); nil otherwise and when not known.
	Since *time.Time
	// Code is the code of R-VULN-034 of a failed scan, or of a row that was put off for a reason of the installation; "" otherwise.
	Code string
}

// coverFacts are what decides a repository's coverage of a version (coverOf).
type coverFacts struct {
	// Ecosystem is the repository's OSV ecosystem, "" for a format without the adapter; Virtual a virtual repository.
	Ecosystem string
	Virtual   bool
	// Installation is vulnerabilities.enabled, Images vulnerabilities.images.enabled, Repository the repository's own switch.
	Installation, Images, Repository bool
	// Scope is the scope of R-VULN-010 for the name; MatchNever the repository's matchPublicAdvisories never.
	Scope      evaluate.Scope
	MatchNever bool
	// Complete says that the ecosystem's data has been imported completely.
	Complete bool
}

// coverOf is the evaluation state of a repository's versions (R-VULN-001) and the cause of one that is not evaluated (Q-VULN-274), in the
// order a reader acts on them: what the version is, the switches, the scope, the data. An image's checked state is refined by its scans
// (imageCheck).
func coverOf(f coverFacts) (vuln.Evaluation, vuln.CheckState) {
	image := f.Ecosystem == advisory.EcosystemOCI
	switch {
	case f.Ecosystem == "" || f.Virtual:
		return vuln.EvaluationNotCovered, vuln.CheckFormatNotScanned
	case !f.Installation || (image && !f.Images):
		return vuln.EvaluationNotCovered, vuln.CheckOffInstallation
	case !f.Repository:
		return vuln.EvaluationNotCovered, vuln.CheckOffRepository
	}
	switch f.Scope {
	case evaluate.ScopeInternal:
		return vuln.EvaluationInternal, vuln.CheckInternal
	case evaluate.ScopeNotCovered:
		if f.MatchNever {
			return vuln.EvaluationNotCovered, vuln.CheckNotMatched
		}
		return vuln.EvaluationNotCovered, vuln.CheckUnknownUpstream
	}
	switch {
	case f.Complete:
		return vuln.EvaluationEvaluated, vuln.CheckChecked
	case image:
		return vuln.EvaluationPendingData, vuln.CheckNoDatabase
	}
	return vuln.EvaluationPendingData, vuln.CheckNoAdvisoryData
}

// coverage reads the facts of coverOf for a version of the name in the repository (name "" for the repository as a whole, which a local
// repository's names count as public for).
func (s *Service) coverage(ctx context.Context, r repository.Repository, name string, data map[string]DataInfo) (vuln.Evaluation, vuln.CheckState) {
	settings := r.VulnerabilitySettings()
	f := coverFacts{
		Ecosystem: r.VulnerabilityEcosystem(), Virtual: r.Class == repository.Virtual, Installation: s.cfg.Enabled, Images: s.cfg.ImagesEnabled,
		Repository: settings.Enabled, MatchNever: settings.MatchPublicAdvisories == vuln.MatchNever,
	}
	f.Complete = data[f.Ecosystem].Complete
	if f.Ecosystem != "" && !f.Virtual && f.Installation && f.Repository {
		// An error of the rule leaves the name matched, as the evaluation does: a rule that cannot read a claim withholds less.
		f.Scope, _, _ = evaluate.ScopeOf(ctx, r, name, s.rule, s.cfg.PublicUpstreams)
	}
	return coverOf(f)
}

// failedCheck is the state of a failed scan by the class of its failure (vuln.ImageMissingCause): judged under the bounds, under the
// release, or neither, which is tried again.
func failedCheck(m ImageState) vuln.CheckState {
	switch vuln.ImageMissingCause(true, true, m.Judged) {
	case vuln.MissingImageOverLimits:
		return vuln.CheckOverLimits
	case vuln.MissingImageUnreadable:
		return vuln.CheckUnreadable
	}
	return vuln.CheckScanFailed
}

// failedRank orders the classes of failure: what needs an operator before what passes on its own.
func failedRank(c vuln.CheckState) int {
	switch c {
	case vuln.CheckOverLimits, vuln.CheckUnreadable:
		return 2
	case vuln.CheckScanFailed:
		return 1
	}
	return 0
}

// imageCheck is the state of an image version whose repository checks it (base CheckChecked), from the manifests it stands for
// (R-VULN-034, Q-VULN-274 to Q-VULN-276), each read on its own, so that every tag of one manifest says the same:
//   - a tag whose own manifest is no container image is never scanned (CheckNotAnImage);
//   - a failed manifest makes the version failed, the class that needs an operator first;
//   - a manifest whose call runs now (started within bound) makes it scanning;
//   - a manifest that waits makes it wait for a scanner when none is connected (presence, or the code a row was put off with), else
//     queued since the earliest of its manifests was queued;
//   - every manifest scanned makes it checked.
//
// While no database is in use (base CheckNoDatabase) the rows still say what does not wait for one (Q-VULN-298): a failed manifest makes
// the version failed and a call that runs makes it scanning (the scanner analyses without a database, Q-VULN-168), and another artifact
// than an image is no image; everything else is no database, the one cause it waits for. Any other base is the repository's cause and
// stands as it is.
func imageCheck(base vuln.CheckState, images []ImageState, presence Presence, now time.Time, bound time.Duration) Check {
	if base == vuln.CheckNoDatabase {
		switch c := imageCheck(vuln.CheckChecked, images, presence, now, bound); {
		case failedRank(c.State) > 0, c.State == vuln.CheckScanning, c.State == vuln.CheckNotAnImage:
			return c
		}
		return Check{State: base}
	}
	if base != vuln.CheckChecked {
		return Check{State: base}
	}
	if len(images) == 1 && images[0].Kind == store.ImageKindOther {
		return Check{State: vuln.CheckNotAnImage}
	}
	var failed, scanning, waiting Check
	var noScanner string
	for _, m := range images {
		switch m.State {
		case store.ImageScanScanned:
			continue
		case store.ImageScanFailed:
			if c := failedCheck(m); failedRank(c) > failedRank(failed.State) {
				failed = Check{State: c, Code: m.ErrorCode}
			}
			continue
		}
		// Pending, or no row yet (an index none of whose platforms is stored, or a manifest discovery has not reached).
		if m.StartedAt != nil && (bound <= 0 || now.Sub(*m.StartedAt) < bound) {
			if scanning.Since == nil || m.StartedAt.Before(*scanning.Since) {
				scanning = Check{State: vuln.CheckScanning, Since: m.StartedAt}
			}
			continue
		}
		if vuln.NoScannerCode(m.ErrorCode) && noScanner == "" {
			noScanner = m.ErrorCode
		}
		if waiting.State == "" {
			waiting = Check{State: vuln.CheckQueued, Code: m.ErrorCode}
		}
		if m.CreatedAt != nil && (waiting.Since == nil || m.CreatedAt.Before(*waiting.Since)) {
			waiting.Since = m.CreatedAt
		}
	}
	switch {
	case failed.State != "":
		return failed
	case scanning.State != "":
		return scanning
	case waiting.State == "":
		if len(images) == 0 {
			return Check{State: vuln.CheckQueued}
		}
		return Check{State: vuln.CheckChecked}
	case noScanner != "" || presence == PresenceNone:
		return Check{State: vuln.CheckNoScanner, Code: noScanner}
	}
	return waiting
}

// waitsForScan reports whether an image version's manifests include one that waits for its scan: the one case where the report asks
// whether a scanner is connected.
func waitsForScan(images []ImageState) bool {
	return slices.ContainsFunc(images, func(m ImageState) bool {
		return m.State != store.ImageScanScanned && m.State != store.ImageScanFailed && m.Kind != store.ImageKindOther
	})
}

// presenceOnce asks the scanners at most once per answer, and only when an image waits for its scan.
type presenceOnce struct {
	s     Scanners
	asked bool
	p     Presence
}

func (o *presenceOnce) get(ctx context.Context, images []ImageState) (Presence, error) {
	if o.s == nil || !waitsForScan(images) {
		return PresenceUnknown, nil
	}
	if !o.asked {
		p, err := o.s.ImageScanners(ctx)
		if err != nil {
			return PresenceUnknown, err
		}
		o.asked, o.p = true, p
	}
	return o.p, nil
}
