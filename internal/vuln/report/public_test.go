package report

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// R-VULN-019, R-VULN-039, Q-VULN-280: an anonymous caller is told whether a version is checked in public words only: checked, not
// checked yet (every state that waits, cannot be checked now or failed), not checked (a setting, an upstream that is not matched, an
// internal name), or what the version is (a format that is not scanned, an artifact that is no image). Unit, microseconds.
func TestR_VULN_039_AnAnonymousCallerIsToldThePublicStateOfACheck(t *testing.T) {
	want := map[vuln.CheckState]vuln.CheckState{
		vuln.CheckChecked:          vuln.CheckChecked,
		vuln.CheckQueued:           vuln.CheckPending,
		vuln.CheckScanning:         vuln.CheckPending,
		vuln.CheckNoScanner:        vuln.CheckPending,
		vuln.CheckNoDatabase:       vuln.CheckPending,
		vuln.CheckNoAdvisoryData:   vuln.CheckPending,
		vuln.CheckScanFailed:       vuln.CheckPending,
		vuln.CheckOverLimits:       vuln.CheckPending,
		vuln.CheckUnreadable:       vuln.CheckPending,
		vuln.CheckOffInstallation:  vuln.CheckNotChecked,
		vuln.CheckOffRepository:    vuln.CheckNotChecked,
		vuln.CheckNotMatched:       vuln.CheckNotChecked,
		vuln.CheckUnknownUpstream:  vuln.CheckNotChecked,
		vuln.CheckInternal:         vuln.CheckNotChecked,
		vuln.CheckFormatNotScanned: vuln.CheckFormatNotScanned,
		vuln.CheckNotAnImage:       vuln.CheckNotAnImage,
		vuln.CheckPending:          vuln.CheckPending,
		vuln.CheckNotChecked:       vuln.CheckNotChecked,
	}
	for in, out := range want {
		if got := in.Public(); got != out {
			t.Errorf("%s: public %s, want %s", in, got, out)
		}
	}
	for in, out := range map[vuln.Evaluation]vuln.Evaluation{
		vuln.EvaluationEvaluated: vuln.EvaluationEvaluated, vuln.EvaluationPendingData: vuln.EvaluationPendingData,
		vuln.EvaluationInternal: vuln.EvaluationNotCovered, vuln.EvaluationNotCovered: vuln.EvaluationNotCovered,
		vuln.EvaluationPendingScan: vuln.EvaluationPendingScan, vuln.EvaluationScanFailed: vuln.EvaluationPendingScan,
	} {
		if got := vuln.PublicEvaluation(in); got != out {
			t.Errorf("%s: public %s, want %s", in, got, out)
		}
	}
}

// R-VULN-020, Q-VULN-280: what an anonymous caller is told of a version in a repository keeps the grade, its counts, the fixable count,
// the OS and its end of life, the time of the check and the block, and drops the installation's cause: the missing data, the time a
// wait began, a scan's code and judgement. An image at the end of life of its OS keeps grading C with eosl, so that the screen says why.
// Unit, microseconds.
func TestR_VULN_020_AnAnonymousCallerIsToldNoCauseOfTheInstallation(t *testing.T) {
	at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	images := []ImageState{
		{Digest: "sha256:a", Platform: "linux/amd64", State: store.ImageScanScanned, ScannedAt: &at, OSFamily: "alpine", OSName: "3.12.0", EOSL: true},
		{Digest: "sha256:b", Platform: "linux/arm64", State: store.ImageScanFailed, ErrorCode: "over_limits", Judged: "limits"},
	}
	g := gradeIn("platform/images", vuln.EvaluationScanFailed, tally{open: Counts{Low: 2}, fixable: 1}, images, nil)
	g.Check = Check{State: vuln.CheckOverLimits, Code: "over_limits", Since: &at}
	rv := RepositoryVersion{
		Repository: "platform/images", Evaluation: vuln.EvaluationScanFailed, Blocked: true, BlockReason: vuln.BlockMissingData,
		Missing: vuln.MissingImageOverLimits, Images: images, Grade: g,
	}
	got := publicVersion(rv)
	if got.Evaluation != vuln.EvaluationPendingScan || got.Missing != "" {
		t.Errorf("evaluation %s, missing %q", got.Evaluation, got.Missing)
	}
	if !got.Blocked || got.BlockReason != vuln.BlockMissingData {
		t.Errorf("the block is gone: %v %s", got.Blocked, got.BlockReason)
	}
	for _, m := range got.Images {
		if m.ErrorCode != "" || m.Judged != "" || m.State == store.ImageScanFailed {
			t.Errorf("manifest %s: %+v", m.Digest, m)
		}
	}
	if images[1].ErrorCode != "over_limits" {
		t.Error("the caller's manifests were changed in place")
	}
	pg := got.Grade
	if pg.Check.State != vuln.CheckPending || pg.Check.Code != "" || pg.Check.Since != nil || pg.Evaluation != vuln.EvaluationPendingScan {
		t.Errorf("grade check %+v, evaluation %s", pg.Check, pg.Evaluation)
	}

	checked := gradeIn("platform/images", vuln.EvaluationEvaluated, tally{}, images[:1], nil)
	checked.Check = Check{State: vuln.CheckChecked}
	pub := publicGrade(checked)
	if pub.Grade != GradeC || !pub.EOSL || pub.OS != "alpine 3.12.0" || pub.CheckedAt == nil || !pub.CheckedAt.Equal(at) || pub.Check.State != vuln.CheckChecked {
		t.Errorf("an image at its end of life: %+v", pub)
	}
}

// R-VULN-019, Q-VULN-280: the anonymous caller's filter asks for the open findings only, whatever the filter asked; every other caller's
// filter is left as it is. Unit, microseconds.
func TestR_VULN_019_AnAnonymousCallerReadsTheOpenFindingsOnly(t *testing.T) {
	asked := store.Filter{Statuses: []string{"dismissed", "resolved"}, Advisory: "GHSA-x"}
	if got := (Scope{anonymous: true}).forCaller(asked); len(got.Statuses) != 3 || got.Statuses[0] != string(vuln.StatusNeedsTriage) || got.Advisory != "GHSA-x" {
		t.Errorf("anonymous: %+v", got)
	}
	if got := (Scope{}).forCaller(asked); len(got.Statuses) != 2 {
		t.Errorf("signed in: %+v", got)
	}
	sc := Scope{}
	if sc.Anonymous() {
		t.Error("a scope without the anonymous caller says it is one")
	}
}
