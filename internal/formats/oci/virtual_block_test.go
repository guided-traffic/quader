package oci

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_OCI_027_ABlockOfAMemberEndsTheWalk (Q-OCI-240): a Forbidden with the code of a block (the vulnerability scanning's or the
// malware gate's) and the gate's pending 503 are neither a miss nor a failure of the member, so the walk returns them; a refusal to
// read or to fill stays a miss and an unavailable member a failure, as before.
func TestR_OCI_027_ABlockOfAMemberEndsTheWalk(t *testing.T) {
	for _, code := range []string{errs.CodeVulnerabilityBlocked, errs.CodeVulnerabilityDataMissing, errs.CodeMalwareFound, errs.CodeMalwareNotChecked} {
		err := errs.E(errs.Forbidden, code, "blocked")
		if isMemberMiss(err) || isMemberFailure(err) {
			t.Errorf("%s: miss %v, failure %v", code, isMemberMiss(err), isMemberFailure(err))
		}
	}
	pending := errs.E(errs.Unavailable, errs.CodeMalwareCheckPending, "checking", errs.WithRetryAfter(30*time.Second))
	if isMemberMiss(pending) || isMemberFailure(pending) {
		t.Error("the pending answer goes on to the next member")
	}
	for _, err := range []error{
		errs.E(errs.Forbidden, errs.CodeForbidden, "no read"), errs.E(errs.Forbidden, errs.CodePopulateNotPermitted, "no fill"),
		errs.E(errs.NotFound, errs.CodeNotFound, "missing"),
	} {
		if !isMemberMiss(err) {
			t.Errorf("%v is no miss any more", err)
		}
	}
	if !isMemberFailure(errs.E(errs.Unavailable, errs.CodeUnavailable, "down")) {
		t.Error("an unavailable member is no failure any more")
	}
}
