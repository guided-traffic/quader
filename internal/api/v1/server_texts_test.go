package v1

import (
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/ci"
	"github.com/guided-traffic/quader/internal/ci/reason"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_UI_214_EveryCodeASentenceStandsForIsInTheAPIsEnum: the screen says a repository's warning, a CI mapping's warning, a
// download preview's note, a mirror's last error and why a replica sees Valkey degraded by its code (R-UI-214), so every code the
// server sends is a value of the API's enum, which the UI's switch over the generated type covers. A code the server adds without
// the enum would reach the screen without its sentence. Microseconds.
func TestR_UI_214_EveryCodeASentenceStandsForIsInTheAPIsEnum(t *testing.T) {
	for _, c := range repository.WarningCodes() {
		if !gen.RepositoryWarningCode(c).Valid() {
			t.Errorf("the repository warning %q is not a value of RepositoryWarning.code", c)
		}
	}
	for _, c := range []reason.Reason{
		reason.WarnUnpinnedCondition, reason.WarnRepositoryTemplateUnconstraint, reason.WarnSharedOwnership, reason.WarnSubjectPattern,
		reason.WarnMutableUserClaim, reason.WarnUnguardedTemplateClaim, reason.WarnNoPipelineSource, reason.WarnUnprotectedRefsAllowed,
		reason.WarnCrossProjectReadOnly, ci.WarnScopeNotChecked,
	} {
		if !gen.CiWarningCode(c).Valid() {
			t.Errorf("the CI warning %q is not a value of CiWarning.code", c)
		}
	}
	for _, c := range []string{
		previewNoteSectionUncounted, previewNoteSizesUnannounced, previewNoteMoreVersions, previewNoteSizeOnceStored, previewNoteCountedOnExpand,
	} {
		if !gen.RemoteDownloadPreviewNote(c).Valid() {
			t.Errorf("the preview note %q is not a value of RemoteDownloadPreview.note", c)
		}
	}
	for _, c := range mirror.Codes() {
		if !gen.MirrorErrorsLastCode(c).Valid() {
			t.Errorf("the mirror's error code %q is not a value of MirrorErrors.lastCode", c)
		}
	}
	for _, c := range cluster.ReasonCodes() {
		if !gen.ValkeyViewReasonCode(c).Valid() {
			t.Errorf("the Valkey reason %q is not a value of ValkeyView.reasonCode", c)
		}
	}
}
