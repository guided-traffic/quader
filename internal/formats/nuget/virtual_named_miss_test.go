package nuget

import (
	"context"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// refusing answers every page the MemRemote does not hold with a refusal no rule of the merge names.
type refusing struct{ *formattest.MemRemote }

func (p refusing) Metadata(ctx context.Context, rq *formats.Request, path formats.Path, variant string) (*formats.RemoteDocument, error) {
	if _, ok := p.Docs[path.Path]; !ok {
		return nil, errs.E(errs.Forbidden, "a_gate_not_yet_written", "Refused by a gate the merge does not know.")
	}
	return p.MemRemote.Metadata(ctx, rq, path, variant)
}

// TestR_AUTHZ_009_AnUnnamedRefusalOfAMembersPageEndsTheMerge (Q-OCI-245 for NuGet's pages): the registration of a package through a
// virtual repository leaves out a page of a remote member only at the refusal it names, populate_not_permitted for a caller who may not
// fill the member (R-AUTHZ-008). Any other refusal of the page ends the read with that refusal, also one written after this rule, so
// a merge never answers as if the member had nothing to say. Unit, milliseconds, a paged index of 200 versions of a few bytes each.
func TestR_AUTHZ_009_AnUnnamedRefusalOfAMembersPageEndsTheMerge(t *testing.T) {
	index, pages := pagedUpstream(t, 200)
	delete(pages, RegistrationPagePath("acme.widgets", "1.64.0", "1.127.0"))
	member := formats.MemberDocument{
		Member: formats.RepoRef{Name: qr("remote")}, Tier: formats.TierRemote, Position: 0, Doc: index,
		CachedVersions: map[string]bool{"1.3.0": true}, Restricted: true,
	}
	res := &mergeResolver{docs: docsOf(nil, []formats.MemberDocument{member})}
	h := remoteMemberHost(t, res, refusing{&formattest.MemRemote{Docs: pages}})
	if r := get(t, h, http.MethodGet, "v3/registration-semver2/acme.widgets/index.json"); r.Code != http.StatusForbidden {
		t.Errorf("a page refused by an unnamed refusal: %d %s, want the member's 403", r.Code, r.Body.String())
	}
}
