package engine

import (
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// TestR_BAS_099_TheKitKnowsTheHeadersABastionPasses: the headers a Bastion passes on of every answer besides the representation
// headers and the declared ones (R-BAS-099 row 5) are the ones the conformance kit allows on a refusal of a download's checks
// (formattest.PassedHeaders, R-BAS-058), so that the kit fails a format whose refusal carries a header a Bastion drops.
func TestR_BAS_099_TheKitKnowsTheHeadersABastionPasses(t *testing.T) {
	ours, kit := slices.Sorted(slices.Values(passedHeaders())), slices.Sorted(slices.Values(formattest.PassedHeaders()))
	if !slices.Equal(ours, kit) {
		t.Fatalf("the engine passes %v, the kit allows %v", ours, kit)
	}
}
