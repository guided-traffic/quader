package ansible

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_AUTHZ_009_AnUnnamedRefusalOfAMembersPageEndsTheMerge (formats/oci.md Q-OCI-245 for Ansible Galaxy's pages): the version list
// of a collection through a virtual repository leaves out a page of a remote member only at the refusal it names,
// populate_not_permitted for a caller who may not fill the member (R-AUTHZ-008). Any other refusal of the page, one written after
// this rule included, ends the read with that refusal, so a merge never answers as if the member had nothing more to say. Unit,
// milliseconds, two pages of a hundred versions of a few bytes each.
func TestR_AUTHZ_009_AnUnnamedRefusalOfAMembersPageEndsTheMerge(t *testing.T) {
	rem := remote("rem", 2, 0)
	w := newWorld(t, rem)
	var first []string
	for i := range 100 {
		first = append(first, "1.0."+strconv.Itoa(i))
	}
	w.doc("collections/acme/tools/versions.json", "rem", listPage(101, first...))
	w.rem.fail = map[string]error{
		rkey("rem", "collections/acme/tools/versions.json", "offset=100"): errs.E(errs.Forbidden, "a_gate_not_yet_written", "Refused by a gate the merge does not know."),
	}
	if r := w.get(versionsPath); r.Code != http.StatusForbidden {
		t.Errorf("a page refused by an unnamed refusal: %d, want the member's 403", r.Code)
	}
}
