package bastionmain

import (
	"context"
	"net/http"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// reachResolver implements [ShapeResolver] over the decision point's reach classification (R-BAS-012, R-BAS-080).
// The rules are authz.Engine.ReachOn's and PackageReachOn's; this type maps them to the wire's shapes and to the
// status a refused read of the repository gets.
type reachResolver struct{ r Reachers }

// Reachers is the decision point as the resolver asks it: the reach of repositories and of the packages of one
// repository. *authz.Engine implements both.
type Reachers interface {
	authz.Reacher
	authz.PackageReacher
}

// NewShapeResolver returns the [ShapeResolver] over the decision point.
func NewShapeResolver(r Reachers) ShapeResolver { return reachResolver{r: r} }

// PackageShapes implements [ShapeResolver]: full where the decision point's package reach is full, partial for every other
// name, an empty one and one the decision point did not answer included.
func (s reachResolver) PackageShapes(ctx context.Context, idx *authz.Index, subject authz.Subject, key qname.Repo, pkgs []string) (map[string]iface.Read, error) {
	got, err := s.r.PackageReachOn(ctx, idx, subject, key, pkgs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]iface.Read, len(pkgs))
	for _, p := range pkgs {
		out[p] = iface.ReadPartial
		if got[p] == authz.ReachFull {
			out[p] = iface.ReadFull
		}
	}
	return out, nil
}

// Shapes implements [ShapeResolver]: the shape on each repository.
func (s reachResolver) Shapes(ctx context.Context, idx *authz.Index, subject authz.Subject, keys []qname.Repo) (map[qname.Repo]Shape, error) {
	got, err := s.r.ReachOn(ctx, idx, subject, keys)
	if err != nil {
		return nil, err
	}
	out := make(map[qname.Repo]Shape, len(got))
	for k, res := range got {
		out[k] = shapeOf(res)
	}
	return out, nil
}

func shapeOf(res authz.ReachResult) Shape {
	switch res.Reach {
	case authz.ReachFull:
		return Shape{Read: iface.ReadFull}
	case authz.ReachPartial:
		return Shape{Read: iface.ReadPartial}
	}
	switch res.Denial {
	case authz.DenyUnauthenticated:
		return Shape{Read: iface.ReadNone, Status: http.StatusUnauthorized}
	case authz.DenyVisible:
		return Shape{Read: iface.ReadNone, Status: http.StatusForbidden}
	}
	return Shape{Read: iface.ReadNone, Status: http.StatusNotFound}
}
