package principal

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// TestR_AUTHZ_041_TheProjectUpstreamHasNoServiceAccounts: a service account of the built-in project upstream, or of one of its
// repositories, answers 409 managed_by_product before anything is read (Q-AUTHZ-300); the database refuses the row as well (AI010,
// the integration area upstream).
func TestR_AUTHZ_041_TheProjectUpstreamHasNoServiceAccounts(t *testing.T) {
	m := &Manager{}
	_, err := m.CreateProjectServiceAccountTx(context.Background(), nil, Caller{}, "upstream", NewServiceAccount{Name: "bot"})
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeManagedByProduct {
		t.Fatalf("a project's account: %v", err)
	}
	_, err = m.CreateRepositoryServiceAccountTx(context.Background(), nil, Caller{}, qname.Repo{Project: "upstream", Key: "npmjs"},
		NewServiceAccount{Name: "bot"}, []string{"read"})
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeManagedByProduct {
		t.Fatalf("a repository's account: %v", err)
	}
}
