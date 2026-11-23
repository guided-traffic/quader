package oci

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_OCI_027_OnlyANamedMissLetsTheWalkGoOn (Q-OCI-245): the walk of a virtual read goes on past a member only at a miss it
// names (the member does not have the item, a 404 of any code; the caller may not read it there, 403 forbidden, R-AUTHZ-009 rules 2
// and 8; the caller may not fill its cache, 403 populate_not_permitted, R-AUTHZ-008) or at a failure it names (the upstream could
// not be used, 502 or 504; the upstream is assumed offline or rate limits Quader; every fetch slot stayed busy; the stored object is
// lost; a dependency is unavailable: R-REPO-019 step 7, R-REPO-029); the lists are errs.IsMemberMiss and errs.IsMemberFailure,
// every format's. Any other answer of a member, a refusal or a 503 that no rule names, ends the walk with that answer, and the next
// member is not asked: a refusal written after this rule cannot let another member serve what the first one refused. Unit,
// microseconds, no data.
func TestR_OCI_027_OnlyANamedMissLetsTheWalkGoOn(t *testing.T) {
	unnamed := []*errs.Error{
		errs.E(errs.Forbidden, "a_gate_not_yet_written", "refused by a gate the walk does not know"),
		errs.E(errs.Unavailable, "a_gate_pending_not_yet_written", "held by a gate the walk does not know", errs.WithRetryAfter(time.Second)),
		errs.E(errs.Forbidden, errs.CodePackageOwnedByOtherOwner, "a refusal of another kind"),
		errs.E(errs.Unavailable, errs.CodeDatabaseUnavailable, "the replica's database, no statement about the member"),
	}
	for _, refused := range unnamed {
		later := false
		steps := []step{
			{memberOf("first"), func(http.ResponseWriter) error { return refused }},
			{memberOf("second"), func(w http.ResponseWriter) error { later = true; return ok(w) }},
		}
		err := walk(context.Background(), rqWith(false), httptest.NewRecorder(), steps, errManifestUnknown)
		if e, _ := errs.As(err); e == nil || e.Code != refused.Code || later {
			t.Errorf("%s: the walk answered %v, the next member asked %v; want the member's answer and no other member", refused.Code, err, later)
		}
		if isMemberMiss(refused) || isMemberFailure(refused) {
			t.Errorf("%s: miss %v, failure %v", refused.Code, isMemberMiss(refused), isMemberFailure(refused))
		}
	}
	for _, miss := range []*errs.Error{
		errs.E(errs.NotFound, errs.CodeOCIManifestUnknown, "no such manifest"), errs.E(errs.NotFound, errs.CodeOCIBlobUnknown, "no such blob"),
		errs.E(errs.NotFound, errs.CodeOCINameUnknown, "no such name"), errs.E(errs.NotFound, errs.CodeNotFound, "hidden"),
		errs.E(errs.Forbidden, errs.CodeForbidden, "no read here"), errs.E(errs.Forbidden, errs.CodePopulateNotPermitted, "no fill"),
	} {
		if !isMemberMiss(miss) {
			t.Errorf("%s: no miss", miss.Code)
		}
	}
	for _, failure := range []*errs.Error{
		errs.E(errs.UpstreamFailed, errs.CodeUpstreamUnreachable, "unreachable"), errs.E(errs.UpstreamTimeout, errs.CodeUpstreamTimeout, "slow"),
		errs.E(errs.Unavailable, errs.CodeUpstreamAssumedOffline, "circuit open"), errs.E(errs.Unavailable, errs.CodeUpstreamRateLimited, "429"),
		errs.E(errs.Unavailable, errs.CodeContentUnavailable, "object lost"), errs.E(errs.Unavailable, errs.CodeUnavailable, "down"),
		errs.E(errs.Unavailable, errs.CodeFetchQueueTimeout, "every fetch slot busy"),
	} {
		if !isMemberFailure(failure) {
			t.Errorf("%s: no failure", failure.Code)
		}
	}
}
