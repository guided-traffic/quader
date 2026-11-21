package v1

import (
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
)

// TestR_SIG_071_TheImpactRequestNamesOneChange: exactly one of anchor (with its change), block and projectAnchors, which must be false;
// the parameter anchors of the preview is references separated by commas, empty for any anchor (R-SIG-071, R-SIG-072). Unit, no document.
func TestR_SIG_071_TheImpactRequestNamesOneChange(t *testing.T) {
	id, off, on := int64(4), false, true
	remove := gen.SignatureImpactRequestChange("remove")
	if ch, err := impactChange(gen.SignatureImpactRequest{Anchor: &id, Change: &remove}); err != nil || ch.Anchor != 4 || ch.AnchorChange != sgate.AnchorRemove {
		t.Errorf("an anchor removed: %+v %v", ch, err)
	}
	if ch, err := impactChange(gen.SignatureImpactRequest{ProjectAnchors: &off}); err != nil || !ch.ProjectAnchorsOff {
		t.Errorf("project anchors off: %+v %v", ch, err)
	}
	for name, bad := range map[string]gen.SignatureImpactRequest{
		"nothing":                      {},
		"an anchor without its change": {Anchor: &id},
		"two changes":                  {Anchor: &id, Change: &remove, ProjectAnchors: &off},
		"project anchors on":           {ProjectAnchors: &on},
		"a change without anchor":      {Change: &remove, ProjectAnchors: &off},
	} {
		if _, err := impactChange(bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if refs, err := previewAnchors(" installation/release-key , project/team-key"); err != nil || len(refs) != 2 || refs[1] != "project/team-key" {
		t.Errorf("two references: %v %v", refs, err)
	}
	if refs, err := previewAnchors(""); err != nil || refs == nil || len(refs) != 0 {
		t.Errorf("any anchor: %v %v", refs, err)
	}
	for _, bad := range []string{"release-key", "installation/release-key,installation/release-key", "elsewhere/x"} {
		if _, err := previewAnchors(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestR_SIG_073_AVersionIsRefusedWhenOneArtifactIs: the summary of a version's files: a refusal first (its reason), then a pending one,
// else a pass by a release or by the first artifact's signature; a file that is no artifact decides nothing. Unit.
func TestR_SIG_073_AVersionIsRefusedWhenOneArtifactIs(t *testing.T) {
	pass := sgate.Assessed{Artifact: true, Judgement: sgate.Judgement{Outcome: sgate.Pass, Signature: 9}}
	released := sgate.Assessed{Artifact: true, Judgement: sgate.Judgement{Outcome: sgate.Pass, Released: true}}
	pending := sgate.Assessed{Artifact: true, Judgement: sgate.Judgement{Outcome: sgate.Pending}}
	refused := sgate.Assessed{Artifact: true, Judgement: sgate.Judgement{Outcome: sgate.Refused, Reason: sgate.ReasonMissing}}
	layer := sgate.Assessed{Judgement: sgate.Judgement{Outcome: sgate.Refused, Reason: sgate.ReasonInvalid}}
	for name, c := range map[string]struct {
		in   []sgate.Assessed
		want gateSummary
	}{
		"a signature passes":       {[]sgate.Assessed{layer, pass}, gateSummary{outcome: sgate.Pass, signature: 9}},
		"a release passes":         {[]sgate.Assessed{pass, released}, gateSummary{outcome: sgate.Pass, released: true}},
		"pending before a pass":    {[]sgate.Assessed{pass, pending}, gateSummary{outcome: sgate.Pending}},
		"a refusal before pending": {[]sgate.Assessed{pending, refused, pass}, gateSummary{outcome: sgate.Refused, reason: sgate.ReasonMissing}},
		"nothing is an artifact":   {[]sgate.Assessed{layer}, gateSummary{outcome: sgate.Pass}},
	} {
		if got := summarize(c.in); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
	if refusedMember, reason := (gateSummary{outcome: sgate.Pending}).refusedMember(); *refusedMember || reason == nil || *reason != "pending" {
		t.Errorf("pending: %v %v", *refusedMember, reason)
	}
}
