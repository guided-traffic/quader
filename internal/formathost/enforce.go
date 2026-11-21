package formathost

import (
	"context"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/vuln/enforce"
)

// liveRef is the reference the item store reads an item of repo with: the repository and, when the vulnerability scanning is
// wired, the policy of that repository from the replica's snapshot (no query), so that the lookup reads the blocking finding
// of the item's version in the same statement (R-VULN-015). A virtual repository's member is looked up by its own key: the
// member that stores the version decides with its own policy.
func (h *Host) liveRef(ctx context.Context, repo formats.RepoRef) (items.RepoRef, error) {
	ref := itemsRepo(repo)
	if h.o.Enforce == nil || h.o.Repos == nil {
		return ref, nil
	}
	r, err := h.o.Repos.ByName(ctx, repo.Name)
	if err != nil {
		return items.RepoRef{}, err
	}
	ref.Vuln = h.o.Enforce.PolicyOf(r)
	return ref, nil
}

// refuseImage applies the verdict of the vulnerability scanning to a pull of an image manifest (R-VULN-015, R-VULN-016).
func (h *Host) refuseImage(ctx context.Context, rq *formats.Request, repo formats.RepoRef, name string, digest [32]byte) error {
	if h.o.Enforce == nil || h.o.Repos == nil {
		return nil
	}
	r, err := h.o.Repos.ByName(ctx, repo.Name)
	if err != nil {
		return err
	}
	return h.o.Enforce.GateImage(ctx, enforce.ImagePull{
		Repo: r, Addressed: rq.Addressed().String(), Name: name, Digest: digest,
		Actor: edge.ActorFrom(httpserver.PrincipalFrom(ctx)), Source: httpserver.SourceFrom(ctx), Endpoint: rq.Subject.Endpoint,
	})
}

// The block of the vulnerability scanning on a stored item about to be served (R-VULN-015) is decided by serveChecks
// (signature_gate.go) together with the malware gate and the signature gate (signatures.md R-SIG-069): it counts and records a block
// and returns the refusal, 403 with the code and text of R-VULN-016, for GET, HEAD, conditional and Range requests alike. The error
// ends the request and, in a virtual repository, the resolution: the next member is not asked.
