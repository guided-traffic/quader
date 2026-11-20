package ci

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_AUTHZ_041_TheProjectUpstreamHasNoCIMappings: a CI mapping of the tier project:upstream answers 409 managed_by_product before
// anything is read (Q-AUTHZ-300); the database refuses the row as well (AI010).
func TestR_AUTHZ_041_TheProjectUpstreamHasNoCIMappings(t *testing.T) {
	_, err := (&MappingService{}).CreateIn(context.Background(), Actor{}, "upstream", MappingInput{Issuer: "gitlab", Name: "build"})
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeManagedByProduct {
		t.Fatalf("err = %v", err)
	}
}
