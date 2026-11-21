package v1

import (
	"context"
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// ScannerLister lists the kinds of scanner and the scanner replicas (R-OPS-048, Q-OPS-223): the kinds from this replica's
// configuration, the replicas from the rows packages wrote from their reports.
type ScannerLister interface {
	Scanners(ctx context.Context) (gen.ScannerList, error)
}

// ListScanners implements GET /api/v1/system/scanners (R-OPS-048); settings:read is enforced before it runs. A replica without the
// lister (a shell without infrastructure) answers 503.
func (s *Server) ListScanners(w http.ResponseWriter, r *http.Request) {
	if s.o.Scanners == nil {
		httpserver.Problem(w, r, errs.E(errs.Unavailable, errs.CodeUnavailable, "The scanner replicas cannot be read on this replica."))
		return
	}
	list, err := s.o.Scanners.Scanners(r.Context())
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listScanners"))
		return
	}
	if list.Kinds == nil {
		list.Kinds = []gen.ScannerKind{}
	}
	if list.Items == nil {
		list.Items = []gen.ScannerReplica{}
	}
	writeJSON(w, r, http.StatusOK, list)
}
