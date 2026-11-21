package files

import (
	"github.com/guided-traffic/quader/internal/repository"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// GateRepo is a repository as the signature gate reads it (signatures.md R-SIG-061, R-SIG-065): its member signatures from the
// replica's snapshot, the project whose trust rows its gate reads (the installation's scope for a repository of upstream), and whether
// it is a system repository of upstream, which is never gated. The format host builds the same for its serve point.
func GateRepo(r repository.Repository) sgate.Repo {
	out := sgate.Repo{
		ID: r.NumericID(), Name: r.Name, Class: string(r.Class), Format: r.Format, ProjectID: r.ProjectID,
		System: r.Upstream.System(), Setting: r.SignatureGate(),
	}
	if wellknown.IsProject(r.Name.Project) {
		out.ProjectID = ""
	}
	return out
}
