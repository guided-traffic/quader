package npm

import (
	"context"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The delivery rules of a remote repository in npm's documents (R-NPM-049, remote-curation.md R-CUR-003). The core refuses what the
// rules do not deliver before a fill and at the serve point; npm filters what its documents list, with the exposure filter of R-NPM-048:
// one filter serves the endpoint's view and the rules' view, so a document lists what both let through, a tag whose version is refused
// is dropped and latest moves to the highest delivered release. A repository without rules has no view and pays nothing.

// setView is a rule set as the document filter reads it: a package with a delivered version, a delivered version. The rules decide
// versions, not dist-tag names: a tag stays while the version it points to is delivered.
type setView struct{ set *rules.Set }

func (v setView) Package(name string) bool { return v.set.Package(name).Delivered }

func (v setView) Version(name, version string) bool { return v.set.Decide(name, version).Delivered }

func (setView) Tag(string, string, string) bool { return true }

// rulesViewOf is the request's repository's rules as the document filter reads them, nil when it delivers everything (a local or
// virtual repository, a remote without rules, a system read).
func rulesViewOf(rq *formats.Request) tagView {
	if set := formats.Delivery(rq); set != nil {
		return setView{set}
	}
	return nil
}

// noDeliveredVersion is the refusal of a package document none of whose versions the rules deliver (R-NPM-049, R-CUR-004), for a
// package whose own decision delivers it (a version rule allows a version the upstream does not hold): the refusal of the package
// whole, with the reason the mode gives a version no rule names.
func noDeliveredVersion(rq *formats.Request, n string) error {
	set := formats.Delivery(rq)
	return formats.RefusalOf(rq, set, n, "", modeReason(set), rules.KindDocument)
}

func modeReason(set *rules.Set) rc.Reason {
	if set.Mode() == rc.OnlyAllowed {
		return rc.ReasonNotAllowed
	}
	return rc.ReasonBlocked
}

// memberWithRules is the rules of a remote member of the request's virtual repository (formats.MemberDelivery); nil for a member
// without rules, a local member and a system read.
func memberWithRules(rq *formats.Request, ref formats.RepoRef) *formats.MemberRules {
	return formats.MemberDelivery(rq, ref)
}

// deliveredPart filters a remote member's document of a merge by that member's rules (R-NPM-049, R-CUR-003 point 3): a version the
// member refuses leaves its part, so the merge takes it from another member that delivers it, or lists it not at all. ok is false when
// the member delivers none of the package's versions; the part then takes no part in the merge.
func (f *Format) deliveredPart(ctx context.Context, n string, doc []byte, m *formats.MemberRules) (out []byte, ok bool, err error) {
	filtered, err := f.exposure.filterDocument(ctx, formats.Path{Path: n}, doc, setView{m.Set})
	switch {
	case errs.KindOf(err) == errs.NotFound:
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return filtered, true, nil
}

// memberRefusal is the refusal of a merge whose parts the members' rules emptied: the first such member's (R-CUR-004, Q-CUR-8).
func memberRefusal(rq *formats.Request, m *formats.MemberRules, name string) error {
	return m.Refusal(rq, name)
}
