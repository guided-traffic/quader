package report

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/advisory"
	"github.com/guided-traffic/quader/internal/vuln/evaluate"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// R-VULN-039, Q-VULN-274: a version that is not checked says why, from the repository alone, in the order a reader acts on it: what the
// version is (a format without the adapter, a virtual repository), the installation's switch (for an image also the image scanning's),
// the repository's, the scope of R-VULN-010 (internal, never matched, an upstream that is no public registry), and the data of its
// ecosystem (an image the database for images, a package its advisory data). Unit, microseconds.
func TestR_VULN_039_ARepositorySaysWhyItsVersionsAreNotChecked(t *testing.T) {
	on := coverFacts{Ecosystem: "npm", Installation: true, Images: true, Repository: true, Complete: true}
	image := on
	image.Ecosystem = advisory.EcosystemOCI
	with := func(f coverFacts, change func(*coverFacts)) coverFacts { change(&f); return f }
	for _, tc := range []struct {
		name  string
		f     coverFacts
		eval  vuln.Evaluation
		state vuln.CheckState
	}{
		{"a checked package", on, vuln.EvaluationEvaluated, vuln.CheckChecked},
		{"an image whose database is there", image, vuln.EvaluationEvaluated, vuln.CheckChecked},
		{"a format without the adapter", with(on, func(f *coverFacts) { f.Ecosystem = "" }), vuln.EvaluationNotCovered, vuln.CheckFormatNotScanned},
		{"a virtual repository", with(on, func(f *coverFacts) { f.Virtual = true }), vuln.EvaluationNotCovered, vuln.CheckFormatNotScanned},
		{"the installation's scanning off", with(on, func(f *coverFacts) { f.Installation = false }), vuln.EvaluationNotCovered, vuln.CheckOffInstallation},
		{"the image scanning off", with(image, func(f *coverFacts) { f.Images = false }), vuln.EvaluationNotCovered, vuln.CheckOffInstallation},
		{"the image scanning off leaves packages", with(on, func(f *coverFacts) { f.Images = false }), vuln.EvaluationEvaluated, vuln.CheckChecked},
		{"the repository's scanning off", with(on, func(f *coverFacts) { f.Repository = false }), vuln.EvaluationNotCovered, vuln.CheckOffRepository},
		{"an internal name", with(on, func(f *coverFacts) { f.Scope = evaluate.ScopeInternal }), vuln.EvaluationInternal, vuln.CheckInternal},
		{"never matched", with(on, func(f *coverFacts) { f.Scope, f.MatchNever = evaluate.ScopeNotCovered, true }), vuln.EvaluationNotCovered, vuln.CheckNotMatched},
		{"an upstream that is no public registry", with(on, func(f *coverFacts) { f.Scope = evaluate.ScopeNotCovered }), vuln.EvaluationNotCovered, vuln.CheckUnknownUpstream},
		{"a package without its advisory data", with(on, func(f *coverFacts) { f.Complete = false }), vuln.EvaluationPendingData, vuln.CheckNoAdvisoryData},
		{"an image without the database", with(image, func(f *coverFacts) { f.Complete = false }), vuln.EvaluationPendingData, vuln.CheckNoDatabase},
		{"off before missing data", with(on, func(f *coverFacts) { f.Repository, f.Complete = false, false }), vuln.EvaluationNotCovered, vuln.CheckOffRepository},
	} {
		if eval, state := coverOf(tc.f); eval != tc.eval || state != tc.state {
			t.Errorf("%s: %s %s, want %s %s", tc.name, eval, state, tc.eval, tc.state)
		}
	}
}

// R-VULN-039, Q-VULN-274 to Q-VULN-277: an image that its repository checks says the state of its scan, read per manifest, so that every
// tag of one image says the same: queued since its manifest was queued, being scanned while a call runs (within the call's deadline), no
// scanner when none is connected or the last call reached none, failed by its class with the code, not an image for another artifact,
// checked when every manifest is scanned; a cause of the repository stands as it is. Unit, microseconds.
func TestR_VULN_039_AnImageSaysTheStateOfItsScan(t *testing.T) {
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) *time.Time { t := now.Add(-ago); return &t }
	const bound = 26 * time.Minute
	scanned := ImageState{State: store.ImageScanScanned, Kind: store.ImageKindImage}
	queued := ImageState{State: store.ImageScanPending, Kind: store.ImageKindImage, CreatedAt: at(13 * time.Minute)}
	newer := ImageState{State: store.ImageScanPending, Kind: store.ImageKindImage, CreatedAt: at(2 * time.Minute)}
	noRow := ImageState{State: store.ImageScanPending, Kind: store.ImageKindImage}
	running := ImageState{State: store.ImageScanPending, Kind: store.ImageKindImage, CreatedAt: at(5 * time.Minute), StartedAt: at(20 * time.Second)}
	lost := ImageState{State: store.ImageScanPending, Kind: store.ImageKindImage, CreatedAt: at(time.Hour), StartedAt: at(40 * time.Minute)}
	busy := ImageState{State: store.ImageScanPending, Kind: store.ImageKindImage, CreatedAt: at(time.Minute), ErrorCode: "scanner_busy"}
	unreached := ImageState{State: store.ImageScanPending, Kind: store.ImageKindImage, CreatedAt: at(time.Minute), ErrorCode: "scanner_unavailable"}
	retried := ImageState{State: store.ImageScanFailed, Kind: store.ImageKindImage, ErrorCode: "timeout"}
	tooLarge := ImageState{State: store.ImageScanFailed, Kind: store.ImageKindImage, ErrorCode: "image_too_large", Judged: vuln.JudgedLimitsPrefix + "max_layers=128"}
	unreadable := ImageState{State: store.ImageScanFailed, Kind: store.ImageKindImage, ErrorCode: "layer_format", Judged: vuln.JudgedReleasePrefix + "0.1.0"}
	chart := ImageState{State: store.ImageScanPending, Kind: store.ImageKindOther}
	index := ImageState{State: store.ImageScanPending, Kind: store.ImageKindIndex}
	for _, tc := range []struct {
		name     string
		base     vuln.CheckState
		images   []ImageState
		presence Presence
		want     Check
	}{
		{"every manifest scanned", vuln.CheckChecked, []ImageState{scanned, scanned}, PresenceReady, Check{State: vuln.CheckChecked}},
		{"queued since the earliest manifest", vuln.CheckChecked, []ImageState{scanned, newer, queued}, PresenceReady, Check{State: vuln.CheckQueued, Since: queued.CreatedAt}},
		{"queued while the replica cannot tell", vuln.CheckChecked, []ImageState{queued}, PresenceUnknown, Check{State: vuln.CheckQueued, Since: queued.CreatedAt}},
		{"queued before discovery wrote its row", vuln.CheckChecked, []ImageState{noRow}, PresenceReady, Check{State: vuln.CheckQueued}},
		{"put off while every scanner is busy", vuln.CheckChecked, []ImageState{busy}, PresenceReady, Check{State: vuln.CheckQueued, Since: busy.CreatedAt, Code: "scanner_busy"}},
		{"an index none of whose platforms is stored", vuln.CheckChecked, []ImageState{index}, PresenceReady, Check{State: vuln.CheckQueued}},
		{"no manifest at all", vuln.CheckChecked, nil, PresenceReady, Check{State: vuln.CheckQueued}},
		{"a call runs", vuln.CheckChecked, []ImageState{queued, running}, PresenceReady, Check{State: vuln.CheckScanning, Since: running.StartedAt}},
		{"a lost call's mark is past its deadline", vuln.CheckChecked, []ImageState{lost}, PresenceReady, Check{State: vuln.CheckQueued, Since: lost.CreatedAt}},
		{"no scanner connected", vuln.CheckChecked, []ImageState{scanned, queued}, PresenceNone, Check{State: vuln.CheckNoScanner}},
		{"the last call reached no scanner", vuln.CheckChecked, []ImageState{unreached}, PresenceUnknown, Check{State: vuln.CheckNoScanner, Code: "scanner_unavailable"}},
		{"a failure that is tried again", vuln.CheckChecked, []ImageState{queued, retried}, PresenceReady, Check{State: vuln.CheckScanFailed, Code: "timeout"}},
		{"over the limits before a retry", vuln.CheckChecked, []ImageState{retried, tooLarge}, PresenceReady, Check{State: vuln.CheckOverLimits, Code: "image_too_large"}},
		{"unreadable", vuln.CheckChecked, []ImageState{unreadable, scanned}, PresenceNone, Check{State: vuln.CheckUnreadable, Code: "layer_format"}},
		{"another artifact than an image", vuln.CheckChecked, []ImageState{chart}, PresenceNone, Check{State: vuln.CheckNotAnImage}},
		{"the repository's cause stands", vuln.CheckNoDatabase, []ImageState{queued}, PresenceNone, Check{State: vuln.CheckNoDatabase}},
		{"the switch stands", vuln.CheckOffRepository, []ImageState{retried}, PresenceReady, Check{State: vuln.CheckOffRepository}},
	} {
		got := imageCheck(tc.base, tc.images, tc.presence, now, bound)
		if got.State != tc.want.State || got.Code != tc.want.Code || !sameTime(got.Since, tc.want.Since) {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func sameTime(a, b *time.Time) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Equal(*b))
}

// countingScanners counts the questions of the report.
type countingScanners struct {
	n int
	p Presence
}

func (c *countingScanners) ImageScanners(context.Context) (Presence, error) { c.n++; return c.p, nil }

// R-VULN-039, Q-VULN-277: the report asks whether a scanner is connected at most once per answer, and only for an answer with an image that
// waits for its scan: a list whose images are all scanned, failed or other artifacts costs no statement for it. Unit, microseconds.
func TestR_VULN_039_TheScannersAreAskedOncePerAnswerAndOnlyForAWaitingImage(t *testing.T) {
	c := &countingScanners{p: PresenceNone}
	o := &presenceOnce{s: c}
	for _, images := range [][]ImageState{
		{{State: store.ImageScanScanned}}, {{State: store.ImageScanFailed}}, {{State: store.ImageScanPending, Kind: store.ImageKindOther}},
	} {
		if p, _ := o.get(t.Context(), images); p != PresenceUnknown {
			t.Fatalf("asked for %+v", images)
		}
	}
	if c.n != 0 {
		t.Fatalf("%d questions without a waiting image", c.n)
	}
	for range 3 {
		if p, _ := o.get(t.Context(), []ImageState{{State: store.ImageScanPending}}); p != PresenceNone {
			t.Fatalf("presence %v", p)
		}
	}
	if c.n != 1 {
		t.Fatalf("%d questions, want 1", c.n)
	}
}
