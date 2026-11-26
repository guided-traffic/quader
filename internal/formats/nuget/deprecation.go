package nuget

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/id"
	"github.com/guided-traffic/quader/internal/formats/nuget/remote"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
	"github.com/guided-traffic/quader/internal/formats/nuget/version"
)

// A NuGet version is deprecated with reasons, an optional message and an optional alternate package (R-NUGET-036): the member
// deprecation of its catalog entry, which `dotnet list package --deprecated` and Visual Studio read from the registration. nuget.org
// sets it on its web site only; Quader sets it through the management operation. It is not the unlisting (R-NUGET-006): an unlisted
// version is hidden from search and still restored when pinned, a deprecated one is found, installed and marked.

var (
	_ formats.DeprecationFinder = (*Format)(nil)
	_ formats.Deprecator        = (*Format)(nil)
)

// deprecationReasons are the reasons of the catalog entry, in the spelling nuget.org writes; a client compares them without case.
func deprecationReasons() []string { return []string{"Legacy", "CriticalBugs", "Other"} }

// DeprecationSubject is the version.
func (*Format) DeprecationSubject() formats.DeprecationSubject { return formats.DeprecateVersion }

// DeprecatedVersions returns the stored versions of the repositories that carry a deprecation (R-NUGET-036).
func (*Format) DeprecatedVersions(ctx context.Context, ports formats.DataPorts, repos []string, limit int) ([]formats.VersionID, error) {
	p, err := portOf(ports)
	if err != nil {
		return nil, err
	}
	ids, err := p.ViewDeprecated(ctx, repos, limit+1)
	out := make([]formats.VersionID, len(ids))
	for i, v := range ids {
		out[i] = formats.VersionID(v)
	}
	return out, err
}

// deprecationRow checks a deprecation against what the catalog entry carries and returns the member to store: at least one known
// reason, each once, in nuget.org's spelling; a message of at most 1,024 characters; an alternate package with a valid ID and a valid
// range, "*" when none is given, since the clients format the range and fail without one.
func deprecationRow(d *formats.Deprecation) (*rows.Deprecation, error) {
	if d == nil {
		return nil, nil
	}
	if len(d.Reasons) == 0 {
		return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "A NuGet deprecation needs at least one reason: Legacy, CriticalBugs or Other.")
	}
	out := &rows.Deprecation{Message: strings.TrimSpace(d.Message)}
	for _, r := range d.Reasons {
		i := slices.IndexFunc(deprecationReasons(), func(k string) bool { return strings.EqualFold(k, strings.TrimSpace(r)) })
		switch {
		case i < 0:
			return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "NuGet knows the reasons Legacy, CriticalBugs and Other; "+strings.TrimSpace(r)+" is none of them.")
		case slices.Contains(out.Reasons, deprecationReasons()[i]):
			return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "The reason "+deprecationReasons()[i]+" is named twice.")
		}
		out.Reasons = append(out.Reasons, deprecationReasons()[i])
	}
	if utf8.RuneCountInString(out.Message) > formats.MaxDeprecationMessage {
		return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "The message is longer than 1,024 characters.")
	}
	if a := d.Alternate; a != nil {
		alt := &rows.AlternatePackage{ID: strings.TrimSpace(a.Package), Range: strings.TrimSpace(a.Range)}
		if id.ValidateUpstream(alt.ID) != nil {
			return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "The alternate package "+alt.ID+" is no NuGet package ID.")
		}
		if alt.Range == "" {
			alt.Range = "*"
		}
		if alt.Range != "*" {
			if _, perr := version.ParseRange(alt.Range); perr != nil {
				return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "The range "+alt.Range+" of the alternate package is no NuGet version range.")
			}
		}
		out.AlternatePackage = alt
	}
	return out, nil
}

// deprecationOfRow is the stored member as the operation answers it.
func deprecationOfRow(d *rows.Deprecation) *formats.Deprecation {
	if d == nil {
		return nil
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil
	}
	return deprecationOf(raw)
}

// Deprecate sets or clears the deprecation of one version of a local repository (R-NUGET-036), decided as a relist is: deploy on the
// version's package path and the ownership of the ID (R-AUTHZ-007), the package's lock, a version the caller may not read answered as
// a missing one, the row, the package's revision and package.metadata_changed, in one transaction. The registration and the search
// render it from the row at once.
func (f *Format) Deprecate(ctx context.Context, rq *formats.Request, c formats.DeprecationChange) (*formats.Deprecation, bool, error) {
	if c.Version == "" {
		return nil, false, errs.E(errs.Unprocessable, errs.CodeValidation, "NuGet deprecates single versions; name the version.")
	}
	dep, err := deprecationRow(c.Deprecation)
	if err != nil {
		return nil, false, err
	}
	if id.ValidateUpstream(c.Package) != nil {
		return nil, false, errNoSuchResource()
	}
	v, err := version.Parse(c.Version)
	if err != nil {
		return nil, false, errNoSuchResource()
	}
	lowerID, lowerVersion := id.Lower(c.Package), v.URLForm()
	target := formats.Target{Package: lowerID, Version: lowerVersion, Path: NupkgPath(lowerID, lowerVersion), Actions: []formats.Action{formats.ActionDeploy}}
	if err := rq.Host.Authorize(ctx, rq, target); err != nil {
		return nil, false, err
	}
	st, err := storeOf(rq.Host.FormatData(ID))
	if err != nil {
		return nil, false, err
	}
	repo := rq.RepoRef()
	if _, found, err := rq.Host.Packages().Package(ctx, repo, lowerID); err != nil {
		return nil, false, err
	} else if !found {
		return nil, false, errNoSuchResource()
	}
	audit := auditOf(rq)
	changed := false
	err = rq.Host.Write(ctx, rq, func(tx formats.Tx, pw formats.PackageWriter) error {
		changed = false
		pkg, _, err := pw.EnsureLocked(ctx, repo, lowerID, lowerID)
		if err != nil {
			return err
		}
		if err := pw.Claim(ctx, target, formats.OwnershipWrite{}); err != nil {
			return err
		}
		if err := hiddenFromCaller(ctx, rq, lowerID, lowerVersion); err != nil {
			return err
		}
		ref, found, err := st.FindVersion(ctx, tx, repo.ID, lowerID, lowerVersion)
		if err != nil {
			return err
		}
		if !found {
			return errNoSuchResource()
		}
		if changed, err = st.SetDeprecation(ctx, tx, rows.DeprecationInput{
			VersionID: ref.VersionID, LowerID: lowerID, LowerVersion: lowerVersion, Deprecation: dep, Audit: audit,
		}); err != nil || !changed {
			return err
		}
		_, _, err = pw.BumpRevision(ctx, pkg)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return deprecationOfRow(dep), changed, nil
}

// upstreamDeprecations reads, from the leaves a registration document of an upstream inlines, the deprecation each version carries,
// nil for one that carries none (R-NUGET-036): the member cleaned of the upstream's addresses (Q-NUGET-85), in the shape the rows hold.
// A leaf that is no version of the ID is skipped.
func upstreamDeprecations(lowerID string, leaves []remote.Leaf) map[string]*rows.Deprecation {
	out := map[string]*rows.Deprecation{}
	for _, l := range leaves {
		v, ok := remote.Convert(l, lowerID)
		if !ok {
			continue
		}
		var d *rows.Deprecation
		if raw := v.Extras.Deprecation; raw != nil {
			var x rows.Deprecation
			if json.Unmarshal(raw, &x) == nil && (len(x.Reasons) > 0 || x.Message != "" || x.AlternatePackage != nil) {
				if len(x.Reasons) == 0 {
					x.Reasons = []string{"Other"} // a client reads a deprecation without a known reason as Other
				}
				if x.AlternatePackage != nil && x.AlternatePackage.Range == "" {
					x.AlternatePackage.Range = "*"
				}
				d = &x
			}
		}
		out[v.Row.LowerVersion] = d
	}
	return out
}
