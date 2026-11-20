package repository

import (
	"slices"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The rules of R-REPO-008, R-REPO-017, R-REPO-021, R-REPO-023 and R-REPO-071 that a
// document alone decides. What needs the other repositories (a member exists, is of
// the format, is not being deleted, a member of upstream is a preset repository that
// is not retired) is decided against PostgreSQL in members.go, in the same pass and
// under the same locks. A member is written as MemberRef writes it: a bare key of
// the own project, or upstream/<key>.

const (
	codeInvalidDeployTarget = "invalid_deploy_target"
	codeInvalidPinMember    = "invalid_pin_member"

	// warningNoMembers, warningUnprotected and warningUnscopedOnly are the
	// warnings of R-REPO-008 that this area raises.
	warningNoMembers    = "no_members"
	warningUnprotected  = "dependency_confusion_unprotected"
	warningUnscopedOnly = "pattern_unscoped_only"
)

// validateVirtual checks the virtual section of a document of class virtual and
// puts its names into the folded form that is stored. A document without the
// section is a virtual repository with no members.
func validateVirtual(v *validation, cat *catalog.Registry, d *Document) {
	if d.Virtual == nil {
		d.Virtual = &VirtualSettings{}
	}
	d.Virtual.normalize()
	vs := d.Virtual
	if ttl := *vs.MergedMetadataTTLSeconds; ttl < 0 || ttl > MaxMergedMetadataTTLSeconds {
		v.add("/virtual/mergedMetadataTtlSeconds", codeOutOfRange, "0 to "+strconv.Itoa(MaxMergedMetadataTTLSeconds))
	}

	isMember := map[string]bool{}
	for i, k := range vs.Members {
		ptr := "/virtual/members/" + strconv.Itoa(i)
		// upstream/<key> names a repository of the built-in project, which the lookup decides (R-REPO-071, Q-REPO-381); any other
		// qualified name, the own project's included, is no member.
		uk, upstream := upstreamMemberKey(k)
		switch {
		case k == "":
			v.add(ptr, errs.CodeInvalidMember, "a member is a repository key")
		case strings.Contains(k, "/") && !upstream:
			v.add(ptr, errs.CodeInvalidMember, "a member is a bare key of the virtual repository's own project or upstream/<key> of a repository of the project upstream; a repository of another project cannot be a member")
		case k == d.Key:
			v.add(ptr, errs.CodeInvalidMember, "a virtual repository cannot be its own member")
		case isMember[k]:
			v.add(ptr, errs.CodeInvalidMember, "the repository is listed twice")
		case upstream && isMember[uk], !upstream && isMember[wellknown.ProjectKey+"/"+k]:
			// Formats name a member by its key in what they hand to a client (Debian's members/<key>/ paths, Ansible's role ids),
			// so two members of one key would be one (Q-REPO-401).
			v.add(ptr, errs.CodeInvalidMember, "a repository of the own project and a repository of the project upstream with the same key cannot both be members")
		}
		isMember[k] = true
	}
	if len(vs.Members) == 0 {
		v.warnings = append(v.warnings, Warning{
			Code: warningNoMembers, Pointer: "/virtual/members",
			Detail: "the virtual repository has no members, so it resolves nothing",
		})
	}
	switch {
	case vs.DeployTarget != nil && strings.Contains(*vs.DeployTarget, "/"):
		v.add("/virtual/deployTarget", codeInvalidDeployTarget, "the deploy target is a bare key of the virtual repository's own project")
	case vs.DeployTarget != nil && !isMember[*vs.DeployTarget]:
		v.add("/virtual/deployTarget", codeInvalidDeployTarget, "the deploy target must be one of the members")
	}

	rules := vs.Pinning.Rules
	if len(rules) > MaxPinRules {
		v.add("/virtual/pinning/rules", codeOutOfRange, "at most "+strconv.Itoa(MaxPinRules)+" pin rules")
		return
	}
	usedBy := map[string]int{}
	for i, r := range rules {
		base := "/virtual/pinning/rules/" + strconv.Itoa(i)
		if n := len(r.Packages); n < 1 || n > MaxPinPatterns {
			v.add(base+"/packages", codeOutOfRange, "1 to "+strconv.Itoa(MaxPinPatterns)+" patterns per rule")
		}
		for j, raw := range r.Packages {
			pat, err := parseFolded(cat, d.Format, raw)
			if err != nil {
				v.add(base+"/packages/"+strconv.Itoa(j), codeInvalidPattern, err.Error())
				continue
			}
			folded := pat.String()
			r.Packages[j] = folded
			if prev, dup := usedBy[folded]; dup {
				v.add(base+"/packages/"+strconv.Itoa(j), codeInvalidPattern,
					"the pattern is already used by rule "+strconv.Itoa(prev)+"; one rule selects a name")
				continue
			}
			usedBy[folded] = i
		}
		seen := map[string]bool{}
		for j, k := range r.Members {
			ptr := base + "/members/" + strconv.Itoa(j)
			switch {
			case !isMember[k]:
				v.add(ptr, codeInvalidPinMember, "a pin rule names members of the virtual repository")
			case seen[k]:
				v.add(ptr, codeInvalidPinMember, "the member is listed twice")
			}
			seen[k] = true
		}
		// A rule's members are kept in the order of the member list, the order a
		// read returns them in, so that a document compares equal to itself.
		slices.SortStableFunc(r.Members, func(a, b string) int { return slices.Index(vs.Members, a) - slices.Index(vs.Members, b) })
	}
}

// warnUnscopedOnly raises pattern_unscoped_only (R-REPO-005): on a format whose
// names can be scoped, `*` stops at `/`, so a list with `*` and neither `**` nor
// `@*/*` leaves the scoped names, usually the internal ones, outside it. Whether
// a format has scoped names is read from its name rule: it takes `@scope/name`.
func warnUnscopedOnly(v *validation, info catalog.Entry, d *Document) {
	if info.PatternSubject != SubjectName || info.Names == nil {
		return
	}
	if _, scoped := info.Names.Normalize("@scope/name"); !scoped {
		return
	}
	for _, l := range []struct {
		ptr  string
		list []string
	}{{"/includePatterns", d.IncludePatterns}, {"/excludePatterns", d.ExcludePatterns}} {
		star, covers := false, false
		for _, p := range l.list {
			switch p {
			case "*":
				star = true
			case "**", "@*/*":
				covers = true
			}
		}
		if star && !covers {
			v.warnings = append(v.warnings, Warning{
				Code: warningUnscopedOnly, Pointer: l.ptr,
				Detail: `"*" matches unscoped names only; add "@*/*" for every scoped name or use "**" for every name`,
			})
		}
	}
}
