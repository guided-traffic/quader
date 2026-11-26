package v1

import (
	"net/http"
	"strconv"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The downloads operation of the Bastion interface (R-BAS-008, R-BAS-016). The Bastion token was checked by the
// recognition stage before the handler runs; the shared authorization step admits the route on that proof (the
// declaration {bastion: true}). Every answer carries iface.HeaderInterface.

// downloadEventBytes bounds the body of the intake per event: a path and a user agent are clipped to 4 KiB
// each by the audit table, and the other members are short.
const downloadEventBytes = 10 << 10

// usageRowBytes bounds the body per usage row (Q-BAS-442): a row of an anonymous subject with a repository of a usual name and no
// limit event is about 300 bytes, one with a repository name of 255 characters and a few limit events about 600.
const usageRowBytes = 640

// downloadsRestBytes holds the rest of a batch: the replica's state with its 100 blocks, the overflow rows and the members.
const downloadsRestBytes = 256 << 10

// downloadsBodyLimit bounds the body of a batch (R-BAS-016, Q-LIM-308, Q-BAS-442): maxBatch events, iface.MaxUsageRows usage rows
// and the rest; with the default max_batch of 1,000 about 71 MiB.
func downloadsBodyLimit(maxBatch int) int64 {
	return int64(maxBatch)*downloadEventBytes + int64(iface.MaxUsageRows)*usageRowBytes + downloadsRestBytes
}

// BastionDownloads implements POST /api/v1/bastion/downloads (R-BAS-016): the downloads the Bastion served from
// its cache become asynchronous artifact.downloaded events. With audit.record_downloads off the answer is 204 and
// nothing is recorded.
func (s *Server) BastionDownloads(w http.ResponseWriter, r *http.Request, _ gen.BastionDownloadsParams) {
	in := s.o.Bastion.Downloads
	rec, ok := bastionmain.RecognitionFrom(r.Context())
	if !ok || in == nil {
		httpserver.Problem(w, r, unavailable("The Bastion download intake"))
		return
	}
	body, err := ReadBody(w, r, MediaJSON, downloadsBodyLimit(s.o.Bastion.Config.Downloads.MaxBatch))
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	var req iface.DownloadsRequest
	if err := StrictUnmarshal(body, &req); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out, err := in.Accept(r.Context(), rec, req)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	if out.UsageDropped > 0 {
		bastionLog(r).Warn("a Bastion reported usage rows that were not stored", "bastion", rec.Bastion, "replica", rec.Replica, "rows", out.UsageDropped)
	}
	w.Header().Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
