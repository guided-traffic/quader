package v1

import (
	"net/http"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The handshake operation of the Bastion interface (R-BAS-008, R-BAS-010). The Bastion token was checked by the
// recognition stage before the handler runs; the shared authorization step admits the route on that proof (the
// declaration {bastion: true}) and the handler never sees a subject of the decision point. Every answer carries
// iface.HeaderInterface; the bodies are the types of internal/bastion/iface.

// BastionHandshake implements GET /api/v1/bastion/handshake (R-BAS-010): the interface version, the identity
// and token expiry of the Bastion, the response base URL derived for this request, whether anonymous requests read
// anything, the download switch, the feed position, the format table and the repository the Bastion's host is bound
// to (R-BAS-057). It records the last contact of the replica.
func (s *Server) BastionHandshake(w http.ResponseWriter, r *http.Request, _ gen.BastionHandshakeParams) {
	ctx := r.Context()
	b := s.o.Bastion
	rec, ok := bastionmain.RecognitionFrom(ctx)
	if !ok || b.Feed == nil || b.AnonymousReads == nil {
		httpserver.Problem(w, r, unavailable("The Bastion interface"))
		return
	}
	cursor, err := b.Feed.Position(ctx)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	// The binding of the host the request names, which for a Bastion is its own (R-BAS-017): read as the match of /v2/
	// reads it, so the Bastion classifies a bound path as the repository the main installation then serves (Q-BAS-188).
	bound := ""
	if b.BoundRepository != nil {
		repo, isBound, err := b.BoundRepository(r)
		if err != nil {
			httpserver.Problem(w, r, err)
			return
		}
		if isBound {
			bound = repo.String()
		}
	}
	info := httpserver.InfoFrom(ctx)
	if b.Contacts != nil {
		rep := bastionmain.Report{ClientAddress: info.Client.String(), UserAgent: r.UserAgent(), ResponseBaseURL: info.BaseURL}
		if !info.Client.IsValid() {
			rep.ClientAddress = ""
		}
		// The contact is a record about the call, not part of its answer: a failure to write it is logged
		// and the handshake still answers.
		if err := b.Contacts.Record(ctx, rec, rep); err != nil {
			bastionLog(r).Warn("the last contact of a Bastion replica could not be recorded", "bastion", rec.Bastion, "replica", rec.Replica, "error", err)
		}
	}
	table := b.Formats
	if table == nil {
		table = iface.HandshakeFormats{} // the member is required: an empty table, never null
	}
	writeInterface(w, r, http.StatusOK, iface.Handshake{
		InterfaceVersion: iface.InterfaceVersion,
		Bastion:          iface.HandshakeBastion{Name: rec.Bastion, TokenID: rec.TokenID, TokenExpiresAt: rec.TokenExpiresAt.UTC()},
		Installation:     iface.HandshakeInstallation{BaseURL: b.BaseURL, Release: b.Release},
		ResponseBaseURL:  info.BaseURL,
		AnonymousReads:   b.AnonymousReads(ctx),
		RecordDownloads:  b.Downloads != nil && b.Downloads.Records(),
		Feed:             iface.HandshakeFeed{Cursor: cursor},
		ServerTime:       time.Now().UTC(),
		Formats:          table,
		BoundRepository:  bound,
	})
}

// BastionFormatTable is the handshake's format table of the formats fs (R-BAS-057, Q-BAS-170): one entry per format with
// routes, in the order of fs, with the facts each declares in catalog.Facts - the registry root, the headers a cached answer
// replays besides the core's, the status of a rate limit and the bound-host prefixes. The lists are empty, never nil, so the
// wire shows [] (the members are required). The composition root builds it once from formats.List(); a format without routes
// (generic, which /files/ serves) has no family of its own at a Bastion and no entry.
func BastionFormatTable(fs []formats.Format) iface.HandshakeFormats {
	out := iface.HandshakeFormats{}
	for _, f := range fs {
		if len(f.Routes()) == 0 {
			continue
		}
		d := f.Descriptor()
		out = append(out, iface.HandshakeFormat{
			ID:                string(d.ID),
			RegistryRoot:      d.RegistryRoot,
			ReplayHeaders:     append([]string{}, d.Bastion.ReplayHeaders...),
			RateLimitedStatus: d.Bastion.RateLimited(),
			BoundPrefixes:     append([]string{}, d.BoundPrefixes()...),
		})
	}
	return out
}
