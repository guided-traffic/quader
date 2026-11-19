package authz

// HoldsAdmin reports whether the subject holds the system role admin on this index: bound to the subject or one of its
// groups, or the break-glass administrator (R-AUTHZ-010). The credential's scope plays no part: the download limits decide
// an administrator by the principal, so that a person's budget does not depend on which of their tokens is used
// (usage-and-limits.md R-LIM-012, Q-LIM-18). It is a question, not a decision: nothing is audited.
func (x *Index) HoldsAdmin(sub Subject) bool {
	if x == nil {
		return false
	}
	return x.rolesOf(sub).has(RoleAdmin)
}

// UserHoldsAdmin is [Index.HoldsAdmin] for a user the management API names by its principal ID and its groups' IDs (the
// explanation and the usage of another subject, R-LIM-034, R-LIM-035): the role bound to the user or one of the groups.
func (x *Index) UserHoldsAdmin(id string, groupIDs []string) bool {
	if x == nil || id == "" {
		return false
	}
	rs := x.bindings[id]
	for _, g := range groupIDs {
		rs |= x.bindings[g]
	}
	return rs.has(RoleAdmin)
}
