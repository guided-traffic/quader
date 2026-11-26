package v1

import (
	"net/http"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The handshake operation of the Bastion interface (R-BAS-008, R-BAS-010). The Bastion token was checked by the
// recognition stage before the handler runs; the shared authorization step admits the route on that proof (the
// declaration {bastion: true}) and the handler never sees a subject of the decision point. Every answer carries
// iface.HeaderInterface; the bodies are the types of internal/bastion/iface.

// BastionHandshake implements GET /api/v1/bastion/handshake (R-BAS-010): the interface version, the identity
// and token expiry of the Bastion, the response base URL derived for this request, whether anonymous requests read
// anything, the download switch, the feed position, the format table and the repository the Bastion's host is bound
// to (R-BAS-057), and the registration's configuration (R-BAS-093). It records the last contact of the replica with what the
// replica reports of itself in the headers of the contact (R-BAS-010).
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
	edge, kept, err := contactHeaders(r.Header)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	info := httpserver.InfoFrom(ctx)
	if b.Contacts != nil {
		rep := bastionmain.Report{
			ClientAddress: info.Client.String(), UserAgent: r.UserAgent(), ResponseBaseURL: info.BaseURL, EdgeLimit: edge, CatalogueKept: kept,
		}
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
	configuration, err := b.configuration(r, rec)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
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
		Configuration:    configuration,
	})
}

// configuration is the registration's configuration (R-BAS-093): what Configuration answers; a replica without it answers 503, never
// a configuration nobody stored.
func (b BastionServices) configuration(r *http.Request, rec bastionmain.Recognition) (iface.Configuration, error) {
	if b.Configuration == nil {
		return iface.Configuration{}, unavailable("The configuration of the Bastion registrations")
	}
	return b.Configuration(r.Context(), rec)
}

// BastionFormatTable is the handshake's format table of the formats fs (R-BAS-057, Q-BAS-170): one entry per format with
// routes, in the order of fs, with the facts each declares in catalog.Facts - the registry root, the headers a cached answer
// replays besides the core's, the status of a rate limit, the longest wait its clients are told, the bound-host prefixes, the
// error shape and the read routes that take a body (R-BAS-095). The lists are empty, never nil, so the wire shows [] (the
// members are required). The composition root builds it once from formats.List(); a format without routes (generic, which
// /files/ serves) has no family of its own at a Bastion and no entry, and /files/ takes the defaults of the table's Lookup.
func BastionFormatTable(fs []formats.Format) iface.HandshakeFormats {
	out := iface.HandshakeFormats{}
	for _, f := range fs {
		if len(f.Routes()) == 0 {
			continue
		}
		d := f.Descriptor()
		out = append(out, iface.HandshakeFormat{
			ID:                   string(d.ID),
			RegistryRoot:         d.RegistryRoot,
			ReplayHeaders:        append([]string{}, d.Bastion.ReplayHeaders...),
			RateLimitedStatus:    d.Bastion.RateLimited(),
			RetryAfterMaxSeconds: int(d.Bastion.RetryAfterMax / time.Second),
			BoundPrefixes:        append([]string{}, d.BoundPrefixes()...),
			ErrorShape:           iface.ErrorShape(d.Bastion.Shape()),
			ReadWithBody:         bodyRoutes(string(d.ID), d.RegistryRoot, d.Bastion.ReadWithBody),
		})
	}
	return out
}

// registryTokenRoute is the read route with a body of the registry root itself (R-OCI-004, R-BAS-051): the token service's
// POST, which no format declares, since it is the core's route below /v2/; it goes into the entry of the format that
// declares the registry root.
const registryTokenRoute = "/v2/_token" //nolint:gosec // G101: the path of the token service, not a credential

// bodyRoutes are a format's read routes that take a body as the handshake names them, paths from the host root: each
// declared pattern below /api/<format>/{project}/{repo}, and for the format of the registry root below
// /v2/{project}/{repo} too, with the token service's POST.
func bodyRoutes(id string, registryRoot bool, declared []catalog.BodyRoute) []iface.BodyRoute {
	out := []iface.BodyRoute{}
	for _, b := range declared {
		out = append(out, iface.BodyRoute{Method: b.Method, Path: "/api/" + id + "/{project}/{repo}" + b.Template})
		if registryRoot {
			out = append(out, iface.BodyRoute{Method: b.Method, Path: "/v2/{project}/{repo}" + b.Template})
		}
	}
	if registryRoot {
		out = append(out, iface.BodyRoute{Method: http.MethodPost, Path: registryTokenRoute})
	}
	return out
}

// contactHeaders reads what a replica reports of itself on the handshake (R-BAS-010): its edge figure and whether its
// catalogue is kept, each nil when the replica sends none. A value the interface does not read is a malformed request.
func contactHeaders(h http.Header) (*iface.EdgeLimit, *bool, error) {
	var edge *iface.EdgeLimit
	var kept *bool
	if v := h.Values(iface.HeaderEdgeLimit); len(v) > 0 {
		e, err := iface.ParseEdgeLimit(v[0])
		if err != nil || len(v) > 1 {
			return nil, nil, errs.E(errs.Invalid, errs.CodeInvalid, iface.HeaderEdgeLimit+" is \"off\" or rps=<n>; connections=<n>, once.",
				errs.WithBadRequest(), errs.WithCause(err))
		}
		edge = &e
	}
	if v := h.Values(iface.HeaderCatalogueKept); len(v) > 0 {
		k, err := iface.ParseCatalogueKept(v[0])
		if err != nil || len(v) > 1 {
			return nil, nil, errs.E(errs.Invalid, errs.CodeInvalid, iface.HeaderCatalogueKept+" is true or false, once.", errs.WithBadRequest(), errs.WithCause(err))
		}
		kept = &k
	}
	return edge, kept, nil
}
