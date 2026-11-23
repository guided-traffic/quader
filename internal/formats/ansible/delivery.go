package ansible

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/galaxydoc"
	"github.com/guided-traffic/quader/internal/formats/ansible/version"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The delivery rules of a remote repository in the Galaxy documents (remote-curation.md R-CUR-003, formats/ansible.md R-ANS-047): the
// version list lists the delivered versions, counts them and pages over them; the collection document's highest_version is the highest
// delivered version; a collection without a delivered version answers the refusal at its documents. A collection the rules refuse
// whole, a refused version's document and its archive are refused by the core before the engine reads them (step 5a) and at the serve
// point. A role is decided as a package (role/<id>), as the patterns decided it (H-856), so its documents need no filter. Nothing of
// this runs for a repository without rules: formats.Delivery is nil and the documents are read and keyed as before (R-ARCH-027).

// storedList is the whole version list of a collection as a remote stores it: the entries in the upstream's order, the digests of the
// stored pages, which the validator of a document rendered from them carries, and the engine's Cache-Status of the first page.
type storedList struct {
	entries []galaxydoc.VersionEntry
	digests []string
	status  string
}

// remoteListEntries reads every stored page of the upstream's version list of a collection. The count is bounded by parsePage
// (MaxListVersions), so the pages read are too.
func remoteListEntries(ctx context.Context, rq *formats.Request, path string) (storedList, error) {
	page := func(start int) (upstreamPage, *formats.RemoteDocument, error) {
		d, err := storedDocument(ctx, rq, path, offsetVariant(start))
		if err != nil {
			return upstreamPage{}, nil, err
		}
		raw, err := d.Bytes(ctx)
		if err != nil {
			return upstreamPage{}, nil, err
		}
		p, err := parsePage(raw)
		return p, d, err
	}
	head, first, err := page(0)
	if err != nil {
		return storedList{}, err
	}
	size := upstreamListLimit
	if head.count > len(head.entries) && len(head.entries) > 0 {
		size = len(head.entries)
	}
	l := storedList{entries: append([]galaxydoc.VersionEntry(nil), head.entries...), digests: []string{hexSum(first.Digest)}, status: first.Status.Header}
	for start := size; start < head.count; start += size {
		p, d, err := page(start)
		if err != nil {
			return storedList{}, err
		}
		l.entries = append(l.entries, p.entries...)
		l.digests = append(l.digests, hexSum(d.Digest))
	}
	return l, nil
}

// deliveredEntries keeps the entries of the versions the set delivers, in their order. A list whose versions the set refuses every one
// of answers the refusal of the collection (R-ANS-047): the package's decision delivered it only because a rule allows a version the
// upstream does not list.
func deliveredEntries(rq *formats.Request, set *rules.Set, pkg string, entries []galaxydoc.VersionEntry) ([]galaxydoc.VersionEntry, error) {
	kept := entries[:0:0]
	var refusal error
	for _, e := range entries {
		d := set.Decide(pkg, e.Version)
		if d.Delivered {
			kept = append(kept, e)
			continue
		}
		if refusal == nil {
			refusal = formats.RefusalOf(rq, set, pkg, "", d.Reason, rules.KindDocument)
		}
	}
	if len(kept) == 0 && refusal != nil {
		return nil, refusal
	}
	return kept, nil
}

// memberDelivery is the rule set of the member key of the request's virtual repository: a remote member's merge part lists only what
// that member's rules deliver (R-CUR-003 point 3), and another member may still deliver a version it refuses. Nil for a member that
// delivers everything and for a system read, which no rule refuses (Q-CUR-7).
func memberDelivery(rq *formats.Request, key qname.Repo) *rules.Set {
	if formats.SystemRead(rq) {
		return nil
	}
	for _, m := range rq.Repo.Members() {
		if m.Name == key {
			return m.Delivery()
		}
	}
	return nil
}

// highestOf is the highest version of the entries by the precedence of semantic versioning (R-ANS-002), as a local collection's
// highest_version is its newest version; "" for none.
func highestOf(entries []galaxydoc.VersionEntry) string {
	best, have := version.Semver{}, ""
	for _, e := range entries {
		v, err := version.Parse(e.Version)
		if err != nil {
			continue
		}
		if have == "" || version.Compare(v, best) > 0 {
			best, have = v, e.Version
		}
	}
	return have
}
