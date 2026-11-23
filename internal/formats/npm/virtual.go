package npm

import (
	"context"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/document"
	"github.com/guided-traffic/quader/internal/formats/npm/merge"
	"github.com/guided-traffic/quader/internal/formats/npm/name"
	"github.com/guided-traffic/quader/internal/formats/npm/rows"
	"github.com/guided-traffic/quader/internal/observability"
)

// The virtual class (R-NPM-028 to R-NPM-032). The virtual engine plans a request
// (patterns, pin rules, claims, the member each caller may read and fill) and caches
// the merged document by the members that took part; npm contributes the document of a
// local member, the merge itself, and the strategy per path.

// neutralPrefix is the tarball address of a document that is merged and cached: it names
// no repository and no request. Every response rewrites the addresses for the
// repository it serves, after the merged-metadata cache, so the cached bytes hold no
// address a request chose (R-REPO-043, Q-NPM-34).
const neutralPrefix = "urn:quader:npm-tarball:"

// Virtual returns the format's virtual adapter.
func (f *Format) Virtual() formats.VirtualAdapter { return virtualAdapter{f: f} }

type virtualAdapter struct{ f *Format }

var _ formats.VirtualAdapter = virtualAdapter{}

// Strategy: every document is merged over all members (R-NPM-030 item 7: npm has no
// request kind that stops at the first member). A path that is no package document
// has no document to merge and is resolved to the first member that has it.
func (a virtualAdapter) Strategy(p formats.Path) formats.Strategy {
	if info := a.f.Classify(p); info.Kind == formats.PathMetadata && info.Package != "" && p.Path == info.Package {
		return formats.Merge
	}
	return formats.FirstMatch
}

// ArtifactPrefix: the tarballs of a package are cached below the path of its document
// (`left-pad/-/left-pad-1.3.0.tgz`, `@acme/util/-/util-1.0.0.tgz`).
func (a virtualAdapter) ArtifactPrefix(pkg string) string {
	n, err := name.Parse(pkg)
	if err != nil {
		return ""
	}
	return n.String() + "/"
}

// LocalDocument renders the full document of a local member from its rows, with the
// neutral tarball address, and returns the revision it was rendered at (R-ARCH-015). A
// member without a live version holds no document; its revision is that of its package
// row, 0 when it has none, so that a later publish changes it.
func (a virtualAdapter) LocalDocument(ctx context.Context, rq *formats.Request, member formats.RepoRef, p formats.Path, _ string) ([]byte, formats.RevSeq, bool, error) {
	if sn, version, keys, ok := signaturePath(p.Path); ok {
		// A version's attestations from a local member: the bundle the trust step vouches for (R-SIG-028, Q-SIG-35). The keys of a
		// virtual repository are its handler's union (virtualKeys), never a member's document.
		if keys {
			return nil, 0, false, nil
		}
		doc, found, err := localAttestations(ctx, rq.Host, member, sn, version)
		return doc, 0, found, err
	}
	n, err := name.Parse(p.Path)
	if err != nil {
		return nil, 0, false, err
	}
	st, err := storeOf(rq.Host.FormatData(ID))
	if err != nil {
		return nil, 0, false, err
	}
	pkg, found, err := st.LocalDocument(ctx, member.ID, n.String())
	if err != nil {
		return nil, 0, false, err
	}
	if !found {
		revs, err := rq.Host.Packages().RevSeqs(ctx, []formats.RepoRef{member}, n.String())
		if err != nil {
			return nil, 0, false, err
		}
		return nil, revs[member.ID], false, nil
	}
	rep := a.f.replicaOf(rq.Host)
	doc, err := document.FullDocument(&pkg, document.Options{
		Name: n.String(), Basename: n.Package(), TarballPrefix: neutralPrefix,
		Rev: Rev(member.ID, formats.PackageID(pkg.PackageID), formats.RevSeq(pkg.RevSeq)), AbbreviatedTime: rep.cfg.AbbreviatedTime,
	})
	if err != nil {
		return nil, 0, false, errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(err))
	}
	return doc, formats.RevSeq(pkg.RevSeq), true, nil
}

// Merge applies the per-version source rule (R-REPO-020): the entry of a version comes
// from the member that tier resolution would pick for its tarball, a member the caller
// may not fill supplies the versions it has cached only (R-AUTHZ-008), the dist-tags
// follow the strategies of the virtual repository (R-NPM-031), and a version two
// members hold with different integrity is counted and logged (R-NPM-030).
func (a virtualAdapter) Merge(ctx context.Context, rq *formats.Request, p formats.Path, docs []formats.MemberDocument) ([]byte, error) {
	n, err := name.Parse(p.Path)
	if err != nil {
		return nil, err
	}
	set := settingsOf(rq.Repo.Config)
	parts := make([]merge.Part, 0, len(docs))
	var emptied *formats.MemberRules
	for _, d := range docs {
		doc := d.Doc
		if m := memberWithRules(rq, d.Member); m != nil {
			// Each remote member's part lists what that member's rules deliver (R-NPM-049, R-CUR-003 point 3).
			kept, ok, err := a.f.deliveredPart(ctx, n.String(), doc, m)
			if err != nil {
				return nil, err
			}
			if !ok {
				if emptied == nil {
					emptied = m
				}
				continue
			}
			doc = kept
		}
		parts = append(parts, merge.Part{
			Member: d.Member.Name.String(), Tier: merge.Tier(d.Tier), Position: d.Position, Doc: doc,
			CachedVersions: d.CachedVersions, Restricted: d.Restricted,
		})
	}
	if len(parts) == 0 && emptied != nil {
		return nil, memberRefusal(rq, emptied, n.String())
	}
	res, err := merge.Do(parts, merge.Options{Name: n.String(), LatestStrategy: set.LatestStrategy, DistTagStrategy: set.DistTagStrategy})
	if err != nil {
		return nil, err
	}
	if len(res.Conflicts) > 0 {
		a.f.replicaOf(rq.Host).met.versionConflicts(rq.Repo.Name.String(), len(res.Conflicts))
		for _, c := range res.Conflicts {
			log := rq.Log
			if log == nil {
				log = observability.LoggerFrom(ctx)
			}
			log.WarnContext(ctx, "two members of a virtual repository hold a version with different integrity",
				"repo", rq.Repo.Name, "package", n.String(), "version", c.Version, "winner", c.Winner, "other", c.Other)
		}
	}
	return res.Doc, nil
}

// shadowing converts the outcome of the shadowing check of a first publish to what the
// publish's transaction records.
func shadowing(out formats.ShadowOutcome) rows.Shadowing {
	s := rows.Shadowing{Undetermined: out.Undetermined}
	for _, x := range out.Shadowed {
		s.Shadowed = append(s.Shadowed, rows.ShadowPair{Virtual: x.Virtual, Remote: x.Remote})
	}
	return s
}
