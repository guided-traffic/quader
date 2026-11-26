package ansible

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
)

// An Ansible Galaxy collection is deprecated as a whole (R-ANS-060): the member deprecated of its collection document, which Galaxy's
// own pages show as "This collection has been deprecated." and which no release of ansible-galaxy reads, so a client installs a
// deprecated collection as any other and warns of nothing. Galaxy carries no message and no version of a deprecation; Quader keeps a
// message for its own screens.

var (
	_ formats.DeprecationFinder = (*Format)(nil)
	_ formats.Deprecator        = (*Format)(nil)
)

// DeprecationSubject is the collection.
func (*Format) DeprecationSubject() formats.DeprecationSubject { return formats.DeprecatePackage }

// DeprecatedVersions returns the stored versions of the repositories whose collection is deprecated (R-ANS-060).
func (*Format) DeprecatedVersions(ctx context.Context, ports formats.DataPorts, repos []string, limit int) ([]formats.VersionID, error) {
	p, err := viewOf(ports)
	if err != nil {
		return nil, err
	}
	ids, err := p.ViewDeprecated(ctx, repos, limit+1)
	return versionIDs(ids), err
}

// collectionDeprecation checks what a deprecation of a collection can hold: no version, no reasons, no alternate package, and a message
// of at most 1,024 characters for Quader's screens.
func collectionDeprecation(c formats.DeprecationChange, by string) (*rows.CollectionDeprecation, error) {
	if c.Version != "" {
		return nil, errs.E(errs.Unprocessable, errs.CodeValidation,
			"Ansible Galaxy deprecates a whole collection, every version of it; name no version.")
	}
	d := c.Deprecation
	if d == nil {
		return nil, nil
	}
	if len(d.Reasons) > 0 || d.Alternate != nil {
		return nil, errs.E(errs.Unprocessable, errs.CodeValidation,
			"Ansible Galaxy carries whether a collection is deprecated and nothing else; reasons and an alternate package are NuGet's.")
	}
	msg := strings.TrimSpace(d.Message)
	if utf8.RuneCountInString(msg) > formats.MaxDeprecationMessage {
		return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "The message is longer than 1,024 characters.")
	}
	return &rows.CollectionDeprecation{By: by, Message: msg}, nil
}

// Deprecate sets, changes or clears the deprecation of a collection of a local repository (R-ANS-060), decided as a publish into the
// collection is: deploy on the collection and its ownership, the package's lock, a collection none of whose versions the caller may read
// answered as a missing one, the row, the package's revision (the collection document's validator) and package.metadata_changed, in one
// transaction.
func (f *Format) Deprecate(ctx context.Context, rq *formats.Request, c formats.DeprecationChange) (*formats.Deprecation, bool, error) {
	dep, err := collectionDeprecation(c, rq.Subject.ID)
	if err != nil {
		return nil, false, err
	}
	name := strings.ToLower(c.Package)
	ns, n, ok := collection.SplitName(name)
	if !ok {
		return nil, false, errNoCollection(c.Package)
	}
	target := formats.Target{Package: name, Path: CollectionDocumentPath(ns, n), Actions: []formats.Action{formats.ActionDeploy}}
	if err := rq.Host.Authorize(ctx, rq, target); err != nil {
		return nil, false, err
	}
	st, err := storeOf(rq.Host.FormatData(ID))
	if err != nil {
		return nil, false, err
	}
	repo := rq.RepoRef()
	if _, found, err := rq.Host.Packages().Package(ctx, repo, name); err != nil {
		return nil, false, err
	} else if !found {
		return nil, false, errNoCollection(name)
	}
	audit := auditOf(rq)
	changed := false
	err = rq.Host.Write(ctx, rq, func(tx formats.Tx, pw formats.PackageWriter) error {
		changed = false
		pkg, _, err := pw.EnsureLocked(ctx, repo, name, name)
		if err != nil {
			return err
		}
		if err := pw.Claim(ctx, target, formats.OwnershipWrite{}); err != nil {
			return err
		}
		live, err := pw.Versions(ctx, pkg)
		if err != nil {
			return err
		}
		cv, err := formats.CallerVersions(ctx, rq, name)
		if err != nil {
			return err
		}
		visible := false
		for _, v := range live {
			visible = visible || cv.Version(v.Version)
		}
		if !visible {
			return errNoCollection(name)
		}
		if changed, err = st.SetCollectionDeprecation(ctx, tx, repo.ID, name, dep, audit); err != nil || !changed {
			return err
		}
		_, _, err = pw.BumpRevision(ctx, pkg)
		return err
	})
	if err != nil || dep == nil {
		return nil, changed, err
	}
	return &formats.Deprecation{Package: true, Message: dep.Message}, changed, nil
}
