package npm

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/name"
	"github.com/guided-traffic/quader/internal/formats/npm/rows"
	"github.com/guided-traffic/quader/internal/formats/npm/semver"
)

// npm deprecates one version at a time with a message (R-NPM-018, R-API-085): `versions[v].deprecated` of the package document, which
// npm install prints as a warning and npm's version picker passes over while a version that is not deprecated satisfies the range.

var (
	_ formats.DeprecationFinder = (*Format)(nil)
	_ formats.Deprecator        = (*Format)(nil)
)

// DeprecationSubject is the version.
func (*Format) DeprecationSubject() formats.DeprecationSubject { return formats.DeprecateVersion }

// DeprecatedVersions returns the stored versions of the repositories that carry a deprecation message, a local publisher's or a
// remote's upstream's (R-API-085).
func (*Format) DeprecatedVersions(ctx context.Context, ports formats.DataPorts, repos []string, limit int) ([]formats.VersionID, error) {
	p, err := portOf(ports)
	if err != nil {
		return nil, err
	}
	ids, err := p.ViewDeprecated(ctx, repos, limit+1)
	out := make([]formats.VersionID, len(ids))
	for i, id := range ids {
		out[i] = formats.VersionID(id)
	}
	return out, err
}

// deprecationMessage checks what npm can carry of a deprecation: a message of at most 1,024 characters and nothing else. A message that
// is empty after trimming is no deprecation, which npm writes as the empty string and Quader refuses as a set: the clear is its own act.
func deprecationMessage(d *formats.Deprecation) (string, error) {
	if d == nil {
		return "", nil
	}
	if len(d.Reasons) > 0 || d.Alternate != nil {
		return "", errs.E(errs.Unprocessable, errs.CodeValidation,
			"npm carries a deprecation message only; reasons and an alternate package are NuGet's and an npm client reads neither.")
	}
	msg := strings.TrimSpace(d.Message)
	switch {
	case msg == "":
		return "", errs.E(errs.Unprocessable, errs.CodeValidation, "An npm deprecation needs a message: npm deprecate shows nothing else, and an empty one clears it.")
	case utf8.RuneCountInString(msg) > formats.MaxDeprecationMessage:
		return "", errs.E(errs.Unprocessable, errs.CodeValidation, "The message is longer than 1,024 characters, the limit of an npm deprecation.")
	}
	return msg, nil
}

// Deprecate sets or clears the deprecation of one live version of a local repository's package (R-NPM-018, R-API-085), as the
// metadata write of npm deprecate does: deploy on the package and its ownership (Q-NPM-9), the package's lock, the message, the
// package's revision and modified_at, and npm.deprecate, in one transaction. A version the caller may not read is one that is not
// published.
func (f *Format) Deprecate(ctx context.Context, rq *formats.Request, c formats.DeprecationChange) (*formats.Deprecation, bool, error) {
	if c.Version == "" {
		return nil, false, errs.E(errs.Unprocessable, errs.CodeValidation, "npm deprecates single versions; name the version.")
	}
	msg, err := deprecationMessage(c.Deprecation)
	if err != nil {
		return nil, false, err
	}
	n, err := name.Parse(c.Package)
	if err != nil {
		return nil, false, notFound()
	}
	if v, perr := semver.Parse(c.Version); perr != nil || v.String() != c.Version {
		return nil, false, notFound()
	}
	if err := rq.Host.Authorize(ctx, rq, documentTarget(n, c.Version, formats.ActionDeploy)); err != nil {
		return nil, false, err
	}
	st, repo, err := f.writeTarget(ctx, rq, n)
	if err != nil {
		return nil, false, err
	}
	audit := auditOf(rq)
	held, changed := "", false
	err = rq.Host.Write(ctx, rq, func(tx formats.Tx, pw formats.PackageWriter) error {
		held, changed = "", false
		pkg, _, err := pw.EnsureLocked(ctx, repo, n.String(), n.String())
		if err != nil {
			return err
		}
		if err := f.claim(ctx, pw, opDeprecate, n.String(), c.Version, false); err != nil {
			return err
		}
		cv, err := formats.CallerVersions(ctx, rq, n.String())
		if err != nil {
			return err
		}
		live, err := st.LiveVersions(ctx, tx, int64(pkg.ID))
		if err != nil {
			return err
		}
		i := slices.IndexFunc(live, func(v rows.LiveVersion) bool { return v.Version == c.Version })
		if i < 0 || !cv.Version(c.Version) {
			return notFound()
		}
		prev, err := st.Deprecated(ctx, tx, live[i].ID)
		if err != nil {
			return err
		}
		held = prev
		if prev == msg {
			return nil // the version holds this deprecation already: the request changes nothing
		}
		_, at, err := pw.BumpRevision(ctx, pkg)
		if err != nil {
			return err
		}
		held, changed = msg, true
		return st.SetDeprecated(ctx, tx, rows.DeprecateInput{
			RepoID: repo.ID, PackageID: int64(pkg.ID), VersionID: live[i].ID, Name: n.String(), Version: c.Version, Message: msg, At: at, Audit: audit,
		})
	})
	if err != nil || held == "" {
		return nil, changed, err
	}
	return &formats.Deprecation{Message: held}, changed, nil
}
