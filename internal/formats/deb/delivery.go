package deb

import (
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The delivery rules of a Debian remote (remote-curation.md R-CUR-003, formats/debian.md R-DEB-036, Q-CUR-9): the remote passes its
// upstream's signed indexes unchanged and the core refuses the .deb files the rules refuse; the filtered view is a virtual repository
// over it, whose merge leaves out the paragraphs (binary package name and version) its remote member's rules refuse. The merge of the
// job (internal/formatdata/deb, virtual_build.go) reads each member's rules from the build's member list; the contract's per-request
// merge reads them here.

// memberDelivery is the rule set of the member key of the request's virtual repository, nil for a member that delivers everything, a
// request without a repository and a system read (Q-CUR-7).
func memberDelivery(rq *formats.Request, key qname.Repo) *rules.Set {
	if rq == nil || rq.Repo == nil || formats.SystemRead(rq) {
		return nil
	}
	for _, m := range rq.Repo.Members() {
		if m.Name == key {
			return m.Delivery()
		}
	}
	return nil
}
