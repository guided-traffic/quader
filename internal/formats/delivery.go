package formats

import (
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// The delivery rules of a remote repository as a format meets them (remote-curation.md R-CUR-003, R-CUR-004, Q-CUR-6). The rules decide
// at three places: before a fill and at the serve point, both in the core (a format adds nothing there), and in the documents a format
// renders, which list only the delivered versions. A format reads the request's repository's compiled rule set from the snapshot
// (Request.Repo.Delivery), which is nil for a repository that delivers everything, so a document of such a repository is filtered and
// keyed as before and costs nothing (R-ARCH-027).

// Delivery is the rule set of the request's repository, nil when it delivers everything or the request is a system read: the scanners'
// and the system's reads are not refused by a rule (Q-CUR-7).
func Delivery(rq *Request) *rules.Set {
	if rq == nil || rq.Repo == nil || SystemRead(rq) {
		return nil
	}
	return rq.Repo.Delivery()
}

// SystemRead reports whether the request is a read of Quader's own: the image scanning's pull and the other system subjects, which no
// rule of a repository refuses.
func SystemRead(rq *Request) bool { return rq != nil && rq.Subject.System() }

// Undelivered is the refusal of the request's repository's rules for a package (version "": the package's decision of R-CUR-001, for
// a package document, a version list or a tag list) or one version of it (R-CUR-003, R-CUR-004); nil when the rules deliver it. kind
// says what the refusal refused, for its record. The error is Forbidden not_allowed_in_repository with Cache-Control: no-store, which the
// format's WriteError renders in its client's shape, and its text names the repository of the request: a member of a virtual
// repository names itself (Q-CUR-8).
func Undelivered(rq *Request, name, version string, kind rules.RefusalKind) error {
	set := Delivery(rq)
	if set == nil {
		return nil
	}
	var d rc.Decision
	if version == "" {
		d = set.Package(name)
	} else {
		d = set.Decide(name, version)
	}
	if d.Delivered {
		return nil
	}
	return RefusalOf(rq, set, name, version, d.Reason, kind)
}

// RefusalOf builds the refusal of a decision that did not deliver (R-CUR-004).
func RefusalOf(rq *Request, set *rules.Set, name, version string, reason rc.Reason, kind rules.RefusalKind) error {
	n, v := set.Fold(name, version)
	rf := &rules.Refusal{
		RepoID: rq.Repo.NumericID(), Repo: rq.Repo.Name, Name: n, Version: v, Reason: reason, Kind: kind, Path: rq.Path.Path,
	}
	base := ""
	if rq.Host != nil {
		base = rq.Host.Links(rq).Base()
	}
	return rf.Err(base)
}

// MemberRules is the delivery rule set of a remote member of the request's virtual repository: what a format's merge filters that
// member's part by (R-CUR-003 point 3).
type MemberRules struct {
	Ref RepoRef
	Set *rules.Set
}

// maxMemberDepth bounds the nesting MemberDelivery follows; the repository model refuses cycles, the bound only keeps a defect finite.
const maxMemberDepth = 16

// MemberDelivery returns the rules of the member of the request's virtual repository that ref names, members of nested virtual
// repositories included, from the replica's snapshot; nil for a member without rules, a local member and a system read, which no rule
// refuses (Q-CUR-7).
func MemberDelivery(rq *Request, ref RepoRef) *MemberRules {
	if SystemRead(rq) || rq == nil || rq.Repo == nil {
		return nil
	}
	return findMember(rq.Repo.Members(), ref, 0)
}

func findMember(ms []repository.Repository, ref RepoRef, depth int) *MemberRules {
	for i := range ms {
		m := &ms[i]
		if m.Name == ref.Name {
			if m.Delivery() == nil {
				return nil
			}
			return &MemberRules{Ref: RepoRef{ID: m.NumericID(), Name: m.Name, Class: m.Class, Format: ID(m.Format)}, Set: m.Delivery()}
		}
		if depth < maxMemberDepth {
			if found := findMember(m.Members(), ref, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

// Refusal is the refusal of a merge whose part the member's rules emptied, for a package whole: it names the member (R-CUR-004,
// Q-CUR-8), as the virtual walk answers the first member's refusal when nothing serves.
func (m *MemberRules) Refusal(rq *Request, name string) error {
	n, _ := m.Set.Fold(name, "")
	rf := &rules.Refusal{RepoID: m.Ref.ID, Repo: m.Ref.Name, Name: n, Reason: ModeReason(m.Set), Kind: rules.KindDocument, Path: rq.Path.Path}
	base := ""
	if rq.Host != nil {
		base = rq.Host.Links(rq).Base()
	}
	return rf.Err(base)
}

// ModeReason is the reason a package with no delivered version is refused for when its own decision delivers it (a version rule
// allows a version the upstream does not hold): the reason the mode gives a version no rule names.
func ModeReason(set *rules.Set) rc.Reason {
	if set.Mode() == rc.OnlyAllowed {
		return rc.ReasonNotAllowed
	}
	return rc.ReasonBlocked
}

// DeliveryView is the rules of the request's repository as a document's filter reads them, the shape of [ExposureView]: a package is
// listed when some version of it is delivered, a version when it is delivered. Nil when the repository delivers everything.
func DeliveryView(rq *Request) ExposureView {
	set := Delivery(rq)
	if set == nil {
		return nil
	}
	return deliveryView{set}
}

type deliveryView struct{ set *rules.Set }

func (v deliveryView) Package(name string) bool { return v.set.Package(name).Delivered }

func (v deliveryView) Version(name, version string) bool {
	return v.set.Decide(name, version).Delivered
}

// DocumentView joins the endpoint's exposure (R-EP-018) and the repository's delivery rules into the one view a document's filter reads
// (R-CUR-003): a package or a version is listed when both let it through. Nil when neither narrows.
func DocumentView(rq *Request) ExposureView {
	ev := rq.Endpoint.Exposure
	if ev == nil && rq.Endpoint.Rules {
		ev = hidesAll{} // an endpoint with rules whose view is missing exposes nothing (fail closed, R-EP-018)
	}
	return JoinViews(ev, DeliveryView(rq))
}

type hidesAll struct{}

func (hidesAll) Package(string) bool         { return false }
func (hidesAll) Version(string, string) bool { return false }

// JoinViews is the view that lets through what both views let through; a nil view lets everything through.
func JoinViews(a, b ExposureView) ExposureView {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	return joinedView{a, b}
}

type joinedView struct{ a, b ExposureView }

func (j joinedView) Package(name string) bool { return j.a.Package(name) && j.b.Package(name) }

func (j joinedView) Version(name, version string) bool {
	return j.a.Version(name, version) && j.b.Version(name, version)
}

// DocumentFingerprint names what filters a document of the request in its rendition key and validator: the endpoint's exposure
// fingerprint (R-EP-018) and the rule set's (R-CUR-003, the mode and the revision of the rule set), "" when neither narrows, so a
// rendition rendered under other rules is never served and a repository without rules is keyed as before.
func DocumentFingerprint(rq *Request) string {
	fp := rq.Endpoint.ExposureFingerprint
	if d := Delivery(rq).Fingerprint(); d != "" {
		if fp != "" {
			return fp + "+" + d
		}
		return d
	}
	return fp
}
