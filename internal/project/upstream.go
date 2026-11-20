package project

import (
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// refuseUpstream answers 409 managed_by_product for a write of the built-in project upstream that the product keeps for itself
// (R-AUTHZ-041, Q-AUTHZ-300): its display name, description and level, its deletion, its members, and a move of a repository from or
// into it (R-REPO-069). The database refuses the same rows (SQLSTATE AI010, migration 00086).
func refuseUpstream(key, what string) error {
	if !wellknown.IsProject(key) {
		return nil
	}
	return errs.E(errs.Conflict, errs.CodeManagedByProduct, "The project upstream is Quader's: "+what)
}
