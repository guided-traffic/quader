package v1

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The operations of the delivery by signed links of the object store (storage.md R-STOR-014, R-STOR-074, Q-API-400): the
// installation's switch and its details, each connection's state on the replica that answers, the test link of a public address,
// and the switch's facts for a repository's settings page. The row is read and written through the store; the connections' states
// and the switch of GET …/switch are the replica's, from its snapshot.

// DownloadLinkStore is the row of the switch: *downloadlinks.Store.
type DownloadLinkStore interface {
	Settings(ctx context.Context) (downloadlinks.Settings, error)
	Update(ctx context.Context, actor audit.Actor, next downloadlinks.Settings, version int64) (downloadlinks.Settings, error)
}

// DownloadLinksServices are what the operations need; without Store or Links they answer 503.
type DownloadLinksServices struct {
	Store DownloadLinkStore
	// Links is the replica's side: the connections' states, the host rule a write is checked with, the snapshot of the switch and the
	// test link.
	Links *downloadlinks.Links
}

func (s *Server) downloadLinks() (DownloadLinksServices, error) {
	d := s.o.DownloadLinks
	if d.Store == nil || d.Links == nil {
		return d, unavailable("The delivery by signed links")
	}
	return d, nil
}

// downloadLinkRoutes are the routes whose clients check a digest themselves (R-STOR-071), by format and route, in the formats' order:
// the only downloads the switch reaches. A format that declares one later appears here without a change of a screen.
func downloadLinkRoutes() []gen.DownloadLinkRoute {
	out := []gen.DownloadLinkRoute{}
	for _, f := range formats.List() {
		for _, r := range f.Routes() {
			if r.ClientDigest.Checks {
				out = append(out, gen.DownloadLinkRoute{Format: string(f.Descriptor().ID), Route: r.Name})
			}
		}
	}
	return out
}

// GetDownloadLinkSwitch implements GET /api/v1/download-links/switch.
func (s *Server) GetDownloadLinkSwitch(w http.ResponseWriter, r *http.Request) {
	d, err := s.downloadLinks()
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.DownloadLinkSwitch{Enabled: d.Links.Settings().Enabled, Formats: []string{}}
	for _, rt := range downloadLinkRoutes() {
		if !slices.Contains(out.Formats, rt.Format) {
			out.Formats = append(out.Formats, rt.Format)
		}
	}
	slices.Sort(out.Formats)
	writeJSON(w, r, http.StatusOK, out)
}

// GetDownloadLinkSettings implements GET /api/v1/download-links/settings.
func (s *Server) GetDownloadLinkSettings(w http.ResponseWriter, r *http.Request) {
	d, err := s.downloadLinks()
	var st downloadlinks.Settings
	if err == nil {
		st, err = d.Store.Settings(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	SetETag(w, st.Version, "")
	writeJSON(w, r, http.StatusOK, downloadLinkSettingsRep(d.Links, st))
}

// downloadLinkSettingsRep is the answer: the row, the replica's state of each connection (its saved address from the row, which
// may be newer than the replica's snapshot) and the routes.
func downloadLinkSettingsRep(links *downloadlinks.Links, st downloadlinks.Settings) gen.DownloadLinkSettings {
	out := gen.DownloadLinkSettings{
		Enabled: st.Enabled, MinSize: st.MinSize, LinkLifeSeconds: st.LinkLifeSeconds, Connections: []gen.DownloadLinkConnection{},
		Routes: downloadLinkRoutes(), Version: st.Version, UpdatedAt: st.UpdatedAt, UpdatedBy: optionalText(st.UpdatedByName),
	}
	for _, c := range links.States() {
		conn := gen.DownloadLinkConnection{
			Name: c.Name, Encrypted: c.Encrypted, Endpoint: c.Endpoint, Usable: c.Usable, Reasons: downloadLinkReasons(c.Reasons),
			HandoverUsable: c.HandoverUsable, HandoverReasons: downloadLinkReasons(c.HandoverReasons),
		}
		if a, ok := st.PublicAddresses[c.Name]; ok {
			conn.PublicAddress = &a
		}
		out.Connections = append(out.Connections, conn)
	}
	return out
}

func downloadLinkReasons(rs []downloadlinks.Reason) []gen.DownloadLinkReason {
	out := make([]gen.DownloadLinkReason, 0, len(rs))
	for _, r := range rs {
		out = append(out, gen.DownloadLinkReason{Code: gen.DownloadLinkReasonCode(r.Code), Detail: optionalText(r.Detail)})
	}
	return out
}

// downloadLinkSettingsDoc is what a merge patch of the settings applies to (R-STOR-014): a null address removes the connection's
// entry, which returns it to its endpoint.
type downloadLinkSettingsDoc struct {
	Enabled         bool              `json:"enabled"`
	MinSize         int64             `json:"minSize"`
	LinkLifeSeconds int               `json:"linkLifeSeconds"`
	PublicAddresses map[string]string `json:"publicAddresses"`
}

// UpdateDownloadLinkSettings implements PATCH /api/v1/download-links/settings (merge patch, R-API-013).
func (s *Server) UpdateDownloadLinkSettings(w http.ResponseWriter, r *http.Request, _ gen.UpdateDownloadLinkSettingsParams) {
	ctx := r.Context()
	d, err := s.downloadLinks()
	var pre Precondition
	if err == nil {
		pre, err = requireIfMatch(r)
	}
	var patch []byte
	if err == nil {
		patch, err = ReadMergePatch(w, r, s.bodyLimit())
	}
	var cur downloadlinks.Settings
	if err == nil {
		cur, err = d.Store.Settings(ctx)
	}
	doc := downloadLinkSettingsDoc{Enabled: cur.Enabled, MinSize: cur.MinSize, LinkLifeSeconds: cur.LinkLifeSeconds, PublicAddresses: maps.Clone(cur.PublicAddresses)}
	if err == nil {
		doc, err = ApplyMergePatch(doc, patch)
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	next := cur
	if err == nil {
		next.Enabled, next.MinSize, next.LinkLifeSeconds, next.PublicAddresses = doc.Enabled, doc.MinSize, doc.LinkLifeSeconds, doc.PublicAddresses
		if next.PublicAddresses == nil {
			next.PublicAddresses = map[string]string{}
		}
		err = downloadLinkViolations(touched(downloadlinks.Validate(next, d.Links.ConnectionNames(), d.Links.Rule()), cur, next))
	}
	if err == nil {
		p := httpserver.PrincipalFrom(ctx)
		_, err = d.Store.Update(ctx, audit.Actor{Kind: string(p.Kind), ID: principal.IDOrEmpty(p.ID), Name: p.Name}, next, cur.Version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.GetDownloadLinkSettings(w, r)
}

// touched keeps the violations of what the write changes: a stored address the write leaves as it is is not checked again, so that
// an address of a connection the configuration no longer has (or a host rule that grew) refuses no other change; it stays in the row
// until a write names it (Q-STOR-355).
func touched(vs []downloadlinks.Violation, cur, next downloadlinks.Settings) []downloadlinks.Violation {
	out := vs[:0:0]
	for _, v := range vs {
		if conn, ok := strings.CutPrefix(v.Pointer, "/publicAddresses/"); ok {
			name := strings.ReplaceAll(strings.ReplaceAll(conn, "~1", "/"), "~0", "~")
			if was, stored := cur.PublicAddresses[name]; stored && was == next.PublicAddresses[name] {
				continue
			}
		}
		out = append(out, v)
	}
	return out
}

// downloadLinkViolations is the 422 of a write the settings may not store (R-STOR-014), each violation at its pointer.
func downloadLinkViolations(vs []downloadlinks.Violation) error {
	if len(vs) == 0 {
		return nil
	}
	fields := make([]errs.FieldError, 0, len(vs))
	for _, v := range vs {
		fields = append(fields, errs.FieldError{Pointer: v.Pointer, Code: v.Code, Detail: v.Detail})
	}
	return errs.E(errs.Invalid, errs.CodeValidation, "The download link settings were not changed: a value is not allowed.", errs.WithFields(fields...))
}

// TestDownloadLink implements POST /api/v1/download-links/test (R-STOR-074, Q-UI-952).
func (s *Server) TestDownloadLink(w http.ResponseWriter, r *http.Request) {
	d, err := s.downloadLinks()
	var in gen.DownloadLinkTestRequest
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var link downloadlinks.TestLink
	if err == nil {
		link, err = d.Links.TestLink(r.Context(), in.Connection, in.PublicAddress)
	}
	if err != nil {
		httpserver.Problem(w, r, testLinkProblem(err))
		return
	}
	writeJSON(w, r, http.StatusOK, gen.DownloadLinkTest{Url: link.URL, ExpiresAt: link.ExpiresAt, Command: link.Command})
}

// testLinkProblem is the answer of a test that made no link: 422 at the member a save would refuse, 409 connection_unavailable for
// a connection that is failing on this replica.
func testLinkProblem(err error) error {
	var ae *downloadlinks.AddressError
	switch {
	case errors.As(err, &ae):
		return errs.E(errs.Invalid, errs.CodeValidation, "The address cannot name signed links.",
			errs.WithFields(errs.FieldError{Pointer: "/publicAddress", Code: downloadlinks.CodeInvalidValue, Detail: ae.Error()}))
	case errors.Is(err, downloadlinks.ErrUnknownConnection):
		return errs.E(errs.Invalid, errs.CodeValidation, "The installation has no such connection.",
			errs.WithFields(errs.FieldError{Pointer: "/connection", Code: downloadlinks.CodeInvalidValue, Detail: "the installation has no connection of this name"}))
	case errors.Is(err, downloadlinks.ErrConnectionUnavailable):
		return errs.E(errs.Conflict, errs.CodeConnectionUnavailable, "No test link was made: "+err.Error()+".")
	}
	return err
}
