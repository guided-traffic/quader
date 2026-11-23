package nuget

import (
	"github.com/guided-traffic/quader/internal/formats"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The delivery rules of a remote repository in NuGet's documents (R-NUGET-035, remote-curation.md R-CUR-003). The core refuses what
// the rules do not deliver before a fill and at the serve point; NuGet filters what its documents list as R-NUGET-034 filters what an
// endpoint exposes: the version list, the registration hives (paged over what is left) and the search, whose forwarded count is reduced
// by what was removed. A repository without rules has no view and pays nothing.

// documentFilter is the filter of a remote's document over the versions of one ID: the endpoint's exposure and the remote's delivery
// rules. It remembers whether the rules removed a version the endpoint exposes, so that a document the rules left without a version
// answers their refusal, not a 404 (R-NUGET-035, R-CUR-004). One filter serves one request's rendering.
type documentFilter struct {
	rq             *formats.Request
	lowerID        string
	view, delivery formats.ExposureView
	refused        bool
}

func newDocumentFilter(rq *formats.Request, lowerID string) *documentFilter {
	return &documentFilter{rq: rq, lowerID: lowerID, view: viewOf(rq), delivery: formats.DeliveryView(rq)}
}

// filters reports whether the filter narrows the document: the endpoint has rules or the repository has delivery rules.
func (d *documentFilter) filters() bool { return d.view != nil || d.delivery != nil }

// keep is the predicate over the versions (URL form) of the ID; nil when nothing narrows.
func (d *documentFilter) keep() func(lowerVersion string) bool {
	if !d.filters() {
		return nil
	}
	return func(v string) bool {
		if d.view != nil && !d.view.Version(d.lowerID, v) {
			return false
		}
		if d.delivery != nil && !d.delivery.Version(d.lowerID, v) {
			d.refused = true
			return false
		}
		return true
	}
}

// missing is the answer to a document the filter left without a version: the rules' refusal of the package when they removed a
// version the endpoint exposes, else the 404 of a missing resource.
func (d *documentFilter) missing() error {
	if d.refused {
		return noDeliveredVersion(d.rq, d.lowerID)
	}
	return errNoSuchResource()
}

// noDeliveredVersion is the refusal of a document of an ID none of whose versions the rules deliver, for an ID whose own decision
// delivers it (a version rule allows a version the upstream does not hold): the refusal of the package whole, with the reason the mode
// gives a version no rule names (R-NUGET-035, R-CUR-004).
func noDeliveredVersion(rq *formats.Request, lowerID string) error {
	set := formats.Delivery(rq)
	return formats.RefusalOf(rq, set, lowerID, "", modeReason(set), rules.KindDocument)
}

func modeReason(set *rules.Set) rc.Reason {
	if set.Mode() == rc.OnlyAllowed {
		return rc.ReasonNotAllowed
	}
	return rc.ReasonBlocked
}

// deliveredBy is the predicate of a rule set over the versions (URL form) of one ID, nil for a nil set.
func deliveredBy(set *rules.Set, lowerID string) func(lowerVersion string) bool {
	if set == nil {
		return nil
	}
	return func(v string) bool { return set.Decide(lowerID, v).Delivered }
}

// memberWithRules is the rules of a remote member of the request's virtual repository (formats.MemberDelivery); nil for a member
// without rules, a local member and a system read.
func memberWithRules(rq *formats.Request, ref formats.RepoRef) *formats.MemberRules {
	return formats.MemberDelivery(rq, ref)
}

// memberRefusal is the refusal of a merge whose parts the members' rules emptied: the first such member's (R-CUR-004, Q-CUR-8).
func memberRefusal(rq *formats.Request, m *formats.MemberRules, name string) error {
	return m.Refusal(rq, name)
}
