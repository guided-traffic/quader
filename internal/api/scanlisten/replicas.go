package scanlisten

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/scanreport"
)

// The scanner replicas' route of the scan listener (operations-and-observability.md R-OPS-048, Q-OPS-223): a scanner reports itself
// and signs off, and packages writes its row. The stage has admitted the request by the client certificate of a kind of scanner
// (internal/auth, scan.go); the handler asks the certificate for the kind again and takes nothing of it from the body.
//
//	PUT     /scan/v1/replicas/{uid}   204, 400 for a report of another shape, 409 when the row is refused
//	DELETE  /scan/v1/replicas/{uid}   204, 404 for no row of that kind

// The patterns of the route.
const (
	ReplicaReportPattern  = "PUT " + scanreport.PathPrefix + "{uid}"
	ReplicaSignOffPattern = "DELETE " + scanreport.PathPrefix + "{uid}"
)

// ReplicaStore writes the rows of the scanner replicas (cluster.ScannerReplicas).
type ReplicaStore interface {
	// Record writes the report of the replica uid of kind; false when the row was refused (the bound of its kind, or a row of
	// another kind under the same UID).
	Record(ctx context.Context, kind, uid string, r scanreport.Report) (bool, error)
	// Remove deletes the row of the replica uid of kind; false when there was none.
	Remove(ctx context.Context, kind, uid string) (bool, error)
}

// ReplicaOptions configure [RegisterReplicas].
type ReplicaOptions struct {
	Store ReplicaStore
	// Kind is the kind of scanner the request's client certificate names, "" for none (the app's scannerKindOf).
	Kind func(*http.Request) string
	Log  *slog.Logger
}

// RegisterReplicas mounts the route of the scanner replicas on the scan listener's router.
func RegisterReplicas(mux interface{ Handle(string, http.Handler) }, o ReplicaOptions) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	h := &replicas{o: o}
	mux.Handle(ReplicaReportPattern, http.HandlerFunc(h.report))
	mux.Handle(ReplicaSignOffPattern, http.HandlerFunc(h.signOff))
}

type replicas struct{ o ReplicaOptions }

// admitted is the kind of the request and its UID, or false after the answer: a request the stage admitted without a kind is a defect
// of the wiring and is answered like an unknown route.
func (h *replicas) admitted(w http.ResponseWriter, r *http.Request) (kind, uid string, ok bool) {
	kind = h.o.Kind(r)
	if kind == "" || httpserver.PrincipalFrom(r.Context()).Kind != httpserver.KindSystem {
		notFound(w, r)
		return "", "", false
	}
	uid = r.PathValue("uid")
	if !scanreport.ValidUID(uid) {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The replica's UID must be a UUID in lower case."))
		return "", "", false
	}
	return kind, uid, true
}

func (h *replicas) report(w http.ResponseWriter, r *http.Request) {
	kind, uid, ok := h.admitted(w, r)
	if !ok {
		return
	}
	rep, err := scanreport.Decode(r.Body)
	if err == nil {
		err = rep.Validate(kind)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, err.Error()))
		return
	}
	recorded, err := h.o.Store.Record(r.Context(), kind, uid, rep)
	if err != nil {
		h.o.Log.ErrorContext(r.Context(), "a scanner's report could not be recorded", "kind", kind, "replica", rep.Name, "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, errs.CodeUnavailable, "The report cannot be recorded now."))
		return
	}
	if !recorded {
		h.o.Log.WarnContext(r.Context(), "a scanner's report was refused: its kind has the most live rows a kind may have, or its UID is a replica of another kind",
			"kind", kind, "replica", rep.Name, "uid", uid, "max_rows", scanreport.MaxRowsPerKind)
		httpserver.Problem(w, r, errs.E(errs.Conflict, errs.CodeConflict, "The report was refused: the kind has the most replicas it may list, or the UID is taken."))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *replicas) signOff(w http.ResponseWriter, r *http.Request) {
	kind, uid, ok := h.admitted(w, r)
	if !ok {
		return
	}
	removed, err := h.o.Store.Remove(r.Context(), kind, uid)
	switch {
	case err != nil:
		h.o.Log.ErrorContext(r.Context(), "a scanner's sign-off could not be recorded", "kind", kind, "uid", uid, "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, errs.CodeUnavailable, "The sign-off cannot be recorded now."))
	case !removed:
		notFound(w, r)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
