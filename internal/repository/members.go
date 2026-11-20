package repository

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// Members, the deploy target and the pin rules as rows (R-REPO-017, R-REPO-021,
// R-REPO-023): the part of a virtual repository's write that needs PostgreSQL.
// The document-only rules are in validate_virtual.go.

// resolveVirtual checks the references of a virtual document against the other
// repositories and returns them as rows to write. It adds the violations of
// R-REPO-008 that only the database can decide to v: a member that is missing,
// being deleted, of another format or itself virtual (nesting is R-REPO-018,
// P1), a member of the project upstream that is no preset repository of the format
// or is retired (R-REPO-071), and a deploy target that is not local. The members
// are locked FOR SHARE in id order, both projects in one statement, so a
// concurrent deletion of a member, or a switch of a member of upstream, waits for
// this transaction instead of changing the repository after it was checked
// (Q-REPO-234). The members and the pin members are written as MemberRef writes
// them: bare keys of project and upstream/<key>; the deploy target is a bare key of
// project (Q-AUTHZ-206, amended by Q-REPO-371). cur is the stored document of an
// update, nil on a create: a member of upstream it lists that has been retired
// since stays (R-REPO-071).
func (s *Service) resolveVirtual(ctx context.Context, qs *q.Queries, v *validation, project string, d *Document, cur *Document) (*virtualRel, error) {
	if d.Class != Virtual || d.Virtual == nil {
		return nil, nil
	}
	vs := d.Virtual
	byRef, err := lockMembers(ctx, qs, project, d)
	if err != nil {
		return nil, err
	}
	if len(vs.Members) > s.maxMembers {
		v.add("/virtual/members", codeOutOfRange, "at most "+strconv.Itoa(s.maxMembers)+" members (repositories.max_virtual_members)")
	}
	var listed []string
	if cur != nil && cur.Virtual != nil {
		listed = cur.Virtual.Members
	}
	rel := &virtualRel{}
	ref := func(k string) relRef { return relRef{id: byRef[k].ID, key: k} }
	for i, k := range vs.Members {
		ptr := "/virtual/members/" + strconv.Itoa(i)
		if k == "" || k == d.Key || (strings.Contains(k, "/") && !isUpstreamRef(k)) {
			continue // reported with the document rules
		}
		row, found := byRef[k]
		c := s.checkMember(memberCheck{format: d.Format, ref: k, row: row, found: found, listed: slices.Contains(listed, k)})
		if c.code != "" {
			v.add(ptr, c.code, c.detail)
			continue
		}
		if c.warning != "" {
			v.warnings = append(v.warnings, Warning{Code: WarningMemberInactive, Pointer: ptr, Detail: c.warning})
		}
		rel.members = append(rel.members, ref(k))
	}
	if vs.DeployTarget != nil {
		if row, found := byRef[*vs.DeployTarget]; found && !row.Upstream && slices.Contains(vs.Members, *vs.DeployTarget) {
			if Class(row.Class) != Local {
				v.add("/virtual/deployTarget", codeInvalidDeployTarget, "the deploy target must be a local repository")
			} else {
				t := ref(*vs.DeployTarget)
				rel.target = &t
			}
		}
	}
	for _, r := range vs.Pinning.Rules {
		pin := relPin{patterns: slices.Clone(r.Packages)}
		for _, k := range r.Members {
			if _, found := byRef[k]; found && slices.Contains(vs.Members, k) {
				pin.members = append(pin.members, ref(k))
			}
		}
		rel.pins = append(rel.pins, pin)
	}
	return rel, s.warnUnprotected(ctx, qs, v, rel, byRef)
}

// lockMembers locks the repositories a virtual document names, its members, its deploy target and its pin members, FOR SHARE in
// one statement and in id order, and returns their rows by the member's written form: the bare key for a repository of project,
// upstream/<key> for a repository of the built-in project (MemberRef). A name the document rules refuse is not looked up.
func lockMembers(ctx context.Context, qs *q.Queries, project string, d *Document) (map[string]q.LockRepositoriesByKeysRow, error) {
	vs := d.Virtual
	var keys, upstreamKeys []string
	add := func(k string) {
		if uk, ok := upstreamMemberKey(k); ok {
			if !slices.Contains(upstreamKeys, uk) {
				upstreamKeys = append(upstreamKeys, uk)
			}
			return
		}
		if k != "" && k != d.Key && !strings.Contains(k, "/") && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for _, k := range vs.Members {
		add(k)
	}
	if vs.DeployTarget != nil {
		add(*vs.DeployTarget)
	}
	for _, r := range vs.Pinning.Rules {
		for _, k := range r.Members {
			add(k)
		}
	}
	byRef := map[string]q.LockRepositoriesByKeysRow{}
	if len(keys)+len(upstreamKeys) == 0 {
		return byRef, nil
	}
	rows, err := qs.LockRepositoriesByKeys(ctx, q.LockRepositoriesByKeysParams{
		UpstreamProject: wellknown.ProjectKey, Project: project, Keys: keys, UpstreamKeys: upstreamKeys,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ref := r.Key
		if r.Upstream && !wellknown.IsProject(project) {
			ref = MemberRef(project, qname.Repo{Project: wellknown.ProjectKey, Key: r.Key})
		}
		byRef[ref] = r
	}
	return byRef, nil
}

// isUpstreamRef reports whether a member is written upstream/<key>.
func isUpstreamRef(k string) bool {
	_, ok := upstreamMemberKey(k)
	return ok
}

// memberCheck is what checkMember decides one member on: the virtual repository's format, the member as written, its row (when
// found) and whether the stored document of an update lists it already.
type memberCheck struct {
	format string
	ref    string
	row    q.LockRepositoriesByKeysRow
	found  bool
	listed bool
}

// memberVerdict is the outcome of checkMember: a violation (code and detail), or the detail of the warning member_inactive of
// an accepted member of upstream that is not active, or neither.
type memberVerdict struct {
	code, detail string
	warning      string
}

// checkMember applies the rules of R-REPO-008 and R-REPO-071 that need the member's row. A member of the project upstream is a
// preset repository of the virtual repository's format that is not retired; a retired one the document listed before stays
// (R-REPO-071), and one that is not active is accepted with member_inactive, which names it and its state, so a team lists it
// before an administrator switches it on (Q-REPO-372).
func (s *Service) checkMember(c memberCheck) memberVerdict {
	row := c.row
	if isUpstreamRef(c.ref) {
		facts := s.memberFacts(row)
		switch {
		case !c.found:
			return memberVerdict{code: errs.CodeInvalidMember, detail: c.ref + " is no repository of the project upstream"}
		case facts.System():
			return memberVerdict{code: errs.CodeInvalidMember, detail: c.ref + " is used by Quader only and is never a member of a virtual repository"}
		case row.Format != c.format:
			return memberVerdict{code: errs.CodeInvalidMember, detail: c.ref + " is of another format: " + row.Format + ", not " + c.format}
		case facts.Retired() && !c.listed:
			return memberVerdict{code: errs.CodeInvalidMember, detail: c.ref + " is no longer offered by Quader"}
		case debMemberUnverified(row.Format, row.Class, row.Config):
			return memberVerdict{code: errs.CodeDebMemberUnverified, detail: c.ref + " carries no deb.upstreamKeys, so it cannot be a member: the virtual repository signs what it merges with its own key and would hand on content nobody checked"}
		}
		if facts.State != wellknown.StateActive {
			return memberVerdict{warning: inactiveDetail(c.ref, facts.State)}
		}
		return memberVerdict{}
	}
	switch {
	case !c.found:
		return memberVerdict{code: errs.CodeInvalidMember, detail: "the project has no repository with the key " + strconv.Quote(c.ref)}
	case Lifecycle(row.Lifecycle) == Deleting:
		return memberVerdict{code: errs.CodeInvalidMember, detail: "the repository is being deleted"}
	case row.Format != c.format:
		return memberVerdict{code: errs.CodeInvalidMember, detail: "the repository has the format " + row.Format + ", not " + c.format}
	case Class(row.Class) == Virtual:
		return memberVerdict{code: errs.CodeInvalidMember, detail: "a virtual repository takes local and remote members; nesting arrives with R-REPO-018"}
	case debMemberUnverified(row.Format, row.Class, row.Config):
		return memberVerdict{code: errs.CodeDebMemberUnverified, detail: "a remote Debian repository without deb.upstreamKeys cannot be a member: the virtual repository signs what it merges with its own key and would hand on content nobody checked"}
	}
	return memberVerdict{}
}

// memberFacts derives the kind and the state of a member of the project upstream from its locked row (Q-REPO-375), nil for any
// other row: disabled is read from the stored document as every read of the repository reads it, so a document that does not
// load counts as off.
func (s *Service) memberFacts(row q.LockRepositoriesByKeysRow) *UpstreamFacts {
	if !row.Upstream || row.UpstreamKind == "" {
		return nil
	}
	r, _ := toRepository(q.Repository{ID: row.ID, Key: row.Key, Class: row.Class, Format: row.Format, Config: row.Config}, wellknown.ProjectKey, s.cat)
	kind := wellknown.Kind(row.UpstreamKind)
	return &UpstreamFacts{Kind: kind, State: wellknown.StateOf(kind, r.Disabled, row.SwitchedOnBefore, row.Retired)}
}

// inactiveDetail is the detail of the warning member_inactive: the member, its state and what it means (R-REPO-071).
func inactiveDetail(ref string, state wellknown.State) string {
	switch state {
	case wellknown.StateSwitchedOff:
		return ref + " is " + string(state) + ": it takes no part until an administrator of Quader switches it on again"
	case wellknown.StateRetired:
		return ref + " is " + string(state) + ": Quader no longer offers it and it takes no part; remove it from the members"
	}
	return ref + " is " + string(state) + ": it takes no part until an administrator of Quader switches it on"
}

// warnUnprotected raises dependency_confusion_unprotected (R-REPO-023): the
// virtual repository has local and remote members, no pin rule, and none of its
// local members claims anything automatically or by a claim of its own. A member
// of the project upstream is a remote member, active or not (R-REPO-071).
func (s *Service) warnUnprotected(ctx context.Context, qs *q.Queries, v *validation, rel *virtualRel,
	byRef map[string]q.LockRepositoriesByKeysRow,
) error {
	if len(rel.pins) > 0 {
		return nil
	}
	var locals []int64
	remotes := false
	for _, m := range rel.members {
		row := byRef[m.key]
		switch Class(row.Class) {
		case Local:
			locals = append(locals, m.id)
			if r, err := toRepository(q.Repository{ID: row.ID, Key: row.Key, Class: row.Class, Format: row.Format, Config: row.Config}, "", s.cat); err == nil && r.autoClaim {
				return nil
			}
		case Remote:
			remotes = true
		}
	}
	if len(locals) == 0 || !remotes {
		return nil
	}
	claiming, err := qs.ListClaimingRepositories(ctx, locals)
	if err != nil {
		return err
	}
	if len(claiming) > 0 {
		return nil
	}
	v.warnings = append(v.warnings, Warning{
		Code: warningUnprotected, Pointer: "/virtual",
		Detail: "the virtual repository has local and remote members, no pin rule, and its local members claim no names (local.autoClaim is off and no claim exists): a public package can shadow an internal name that is not published yet",
	})
	return nil
}

// writeVirtual replaces the rows of a virtual repository with rel. It writes
// only the parts that differ from old (nil on a create) and reports whether the
// member list or deploy target and whether the pin rules changed.
func writeVirtual(ctx context.Context, qs *q.Queries, id int64, rel, old *virtualRel) (membersChanged, pinsChanged bool, err error) {
	if old == nil {
		old = &virtualRel{}
	}
	sameRef := func(a, b relRef) bool { return a.id == b.id }
	if !slices.EqualFunc(old.members, rel.members, sameRef) {
		membersChanged = true
		if err = qs.DeleteVirtualMembers(ctx, id); err != nil {
			return
		}
		for i, m := range rel.members {
			if err = qs.InsertVirtualMember(ctx, q.InsertVirtualMemberParams{VirtualID: id, Position: int32(i), MemberID: m.id}); err != nil { //nolint:gosec // bounded by max_virtual_members
				return
			}
		}
	}
	if (old.target == nil) != (rel.target == nil) || (rel.target != nil && old.target.id != rel.target.id) {
		membersChanged = true
		if err = qs.DeleteVirtualDeployTarget(ctx, id); err != nil {
			return
		}
		if rel.target != nil {
			if err = qs.InsertVirtualDeployTarget(ctx, q.InsertVirtualDeployTargetParams{VirtualID: id, TargetID: rel.target.id}); err != nil {
				return
			}
		}
	}
	if !pinsEqual(old.pins, rel.pins) {
		pinsChanged = true
		if err = qs.DeleteVirtualPinRules(ctx, id); err != nil {
			return
		}
		for i, p := range rel.pins {
			if err = qs.InsertVirtualPinRule(ctx, q.InsertVirtualPinRuleParams{VirtualID: id, Position: int32(i), PackagePatterns: p.patterns}); err != nil { //nolint:gosec // bounded by MaxPinRules
				return
			}
			for _, m := range p.members {
				if err = qs.InsertVirtualPinMember(ctx, q.InsertVirtualPinMemberParams{VirtualID: id, Position: int32(i), MemberID: m.id}); err != nil { //nolint:gosec // bounded by MaxPinRules
					return
				}
			}
		}
	}
	return membersChanged, pinsChanged, nil
}

// pinsEqual compares two pin lists; the members of a rule compare as sets,
// because a read returns them in the order of the member list.
func pinsEqual(a, b []relPin) bool {
	return slices.EqualFunc(a, b, func(x, y relPin) bool {
		if !slices.Equal(x.patterns, y.patterns) || len(x.members) != len(y.members) {
			return false
		}
		for _, m := range x.members {
			if !slices.ContainsFunc(y.members, func(o relRef) bool { return o.id == m.id }) {
				return false
			}
		}
		return true
	})
}

// notifyPins tells the replicas that pin rules changed. They are part of the
// repository snapshot, which the repo_config change of the update drops; the
// area claims carries them as well (R-REPO-023), so that everything that routes
// reads one area.
func (s *Service) notifyPins(tx meta.Tx, r Repository) error {
	return s.notif.Notify(tx, cluster.AreaClaims, 1, claimsPayload{ID: r.ID, Key: r.Name.String(), Op: "pins"})
}

// detachReferences handles the virtual repositories that list a repository when
// it is deleted (R-REPO-013). Without detach the delete answers 409
// repository_referenced naming them. With detach the same transaction removes
// the repository from every member list, deploy target and pin rule, moves the
// ETag of each virtual repository and records an update of it.
func (s *Service) detachReferences(ctx context.Context, tx meta.Tx, qs *q.Queries, c Caller, row q.Repository, project string, detach bool) error {
	refs, err := qs.ListReferencingVirtuals(ctx, row.ID)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	keys := make([]string, len(refs))
	for i, r := range refs {
		keys[i] = r.Key
	}
	if !detach {
		return errs.E(errs.Conflict, errs.CodeRepositoryReferenced,
			"The repository is referenced by the virtual repositories "+strings.Join(keys, ", ")+"; delete with detach=true or remove the references first.")
	}
	if err := qs.DetachMember(ctx, row.ID); err != nil {
		return err
	}
	if err := qs.DetachDeployTarget(ctx, row.ID); err != nil {
		return err
	}
	if err := qs.DetachPinMember(ctx, row.ID); err != nil {
		return err
	}
	ids := make([]int64, len(refs))
	for i, r := range refs {
		ids[i] = r.ID
	}
	if err := qs.TouchConfigVersions(ctx, ids); err != nil {
		return err
	}
	for _, ref := range refs {
		rows, err := qs.GetRepositoryByID(ctx, ref.ID)
		if err != nil || len(rows) == 0 {
			return err
		}
		v, _ := toRepository(rows[0], project, s.cat)
		if err := s.afterWrite(ctx, tx, c, EventUpdated, v, []string{"virtual"}); err != nil {
			return err
		}
	}
	return s.notif.Notify(tx, cluster.AreaClaims, 1, claimsPayload{ID: strconv.FormatInt(row.ID, 10), Key: project + "/" + row.Key, Op: "detach"})
}
