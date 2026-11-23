// Package bastionmark is the Mark stage of the Bastion chain on the main installation (R-BAS-013, R-BAS-014,
// R-BAS-052, R-BAS-059, Q-BAS-52, Q-BAS-70): for a request that carries a bastionmain.Recognition it installs the
// bastionmain.RouteObserver, refuses the writes a Bastion token must never make, and adds the marking headers
// at the status line; for every other request it strips every iface.HeaderPrefix header from the response, so
// that an ordinary caller never learns its own shape. No format code is involved: the stage knows the route
// from the observer calls of the dispatcher, of the raw file handler and of the registry's token service and
// catalog, the shape from the bastionmain.ShapeResolver, and what the lookups and the download recording found
// from the bastionmain.Absence record.
//
// It is an edge package because it reads the caller the authentication stage found (httpserver.Principal).
package bastionmark

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// ClassProbe is the class label of quader_bastion_forwarded_requests_total for a write probe (R-BAS-014). It is
// no iface.Class: a probe answer is `pass` on the wire.
const ClassProbe = "probe"

// Options configure [New].
type Options struct {
	// Shapes computes the caller's read shape on the addressed repository (R-BAS-012). Required for a shape in
	// the marking; without it no response is `shared` or `private`.
	Shapes bastionmain.ShapeResolver
	// MainURL is the main installation's own base URL (server.base_url), which a refused write names so that
	// the client knows where to publish (R-BAS-014). Empty leaves the URLs out of the detail.
	MainURL string
	// RegistryRoot is the ID of the format that declares the registry root ("oci", catalog.Facts.RegistryRoot): a refused
	// write of it names the main installation's image prefix of the repository, "<host>/<projectKey>/<repoKey>", which is
	// what a client pushes to, instead of a protocol base path (R-BAS-051). Empty names a protocol base path for every
	// format.
	RegistryRoot string
	// Metrics counts the forwarded requests by registration and class; nil counts nothing.
	Metrics *bastionmain.Metrics
	// Log receives what the stage cannot say on the wire; nil discards it.
	Log *slog.Logger
}

// Marker is the Mark stage.
type Marker struct{ o Options }

// New returns the stage over its options.
func New(o Options) *Marker {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Marker{o: o}
}

// Stage returns the middleware for bastionmain.Stages.Mark.
func (m *Marker) Stage() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec, ok := bastionmain.RecognitionFrom(r.Context())
			if !ok {
				next.ServeHTTP(&stripWriter{ResponseWriter: w}, r)
				return
			}
			st := &state{m: m, rec: rec, subject: iface.Subject{Kind: iface.SubjectInvalid}}
			ctx, absence := bastionmain.WithAbsence(r.Context())
			st.absence = absence
			ctx = httpserver.WithPrincipalNote(ctx, st.noteCaller)
			ctx = bastionmain.WithObserver(ctx, &observer{st: st})
			next.ServeHTTP(&markWriter{ResponseWriter: w, st: st}, r.WithContext(ctx))
		})
	}
}

// state is what one recognized request tells the marking: the caller, the route, the shape and the lookups.
type state struct {
	m       *Marker
	rec     bastionmain.Recognition
	absence *bastionmain.Absence

	mu      sync.Mutex
	subject iface.Subject // invalid until the authentication stage accepted a credential
	// grant is the grant of a registry token (R-OCI-005), nil for every other credential: a ceiling the decision point does
	// not see, so the read shape is capped at partial (R-BAS-052, Q-BAS-167) and a package shape is full only on an image
	// the grant holds pull on (R-BAS-082).
	grant *httpserver.RegistryGrant
	match *bastionmain.RouteMatch
	kind  string
	read  iface.Read
	// whole is the shape on the repository before the registry cap, "" when none was computed.
	whole iface.Read
	// decided holds what Decided was told: the request's context, the subject and the index the request decides on, so that
	// the package shape is computed at the status line on the same index (R-BAS-082).
	decided *decision
	probe   bool // the request was answered as a probe
}

// decision is the subject and the index of a request's decision, with the request's context.
type decision struct {
	ctx context.Context
	sub authz.Subject
	idx *authz.Index
}

// noteCaller is called by the authentication stage with the caller it accepted. The subject of the marking is
// the token with its principal and expiry, the anonymous caller, or invalid for a credential the Bastion cannot
// use as one (a session cookie). A registry token is the token it was issued for, with the registry token's own
// expiry (auth.Credential.ExpiresAt is that, R-BAS-052), and its grant is remembered for the cap of its shapes.
func (s *state) noteCaller(p httpserver.Principal) {
	sub := iface.Subject{Kind: iface.SubjectInvalid}
	switch {
	case p.Anonymous:
		sub = iface.Subject{Kind: iface.SubjectAnonymous}
	case p.Credential.TokenID != "" && p.ID != "":
		sub = iface.Subject{Kind: iface.SubjectToken, TokenID: p.Credential.TokenID, PrincipalID: p.ID, ExpiresAt: p.Credential.ExpiresAt}
	}
	s.mu.Lock()
	s.subject, s.grant = sub, p.Credential.Registry
	s.mu.Unlock()
}

// NoteCaller tells the marking of a recognized Bastion request who the caller of a route turned out to be when the route
// authenticates the credential of its request itself and not the chain's authentication stage: the registry's token
// service, which presents the Basic credential or the form's password of a token request to the stage on its own
// (R-OCI-004, R-BAS-051). The marking then states that caller (Q-BAS-187), as it states the caller the chain's stage
// accepted on every other route; a refusal of the token service is a 401 and invalid like every other. It is a no-op on a
// request that is no recognized Bastion request.
func NoteCaller(ctx context.Context, p httpserver.Principal) {
	if o, ok := bastionmain.ObserverFrom(ctx).(*observer); ok {
		o.st.noteCaller(p)
	}
}

func (s *state) setRoute(mt bastionmain.RouteMatch) {
	s.mu.Lock()
	s.match, s.kind = &mt, mt.Kind
	s.mu.Unlock()
}

// observer is the bastionmain.RouteObserver of one recognized request.
type observer struct{ st *state }

// Matched refuses the writes of a Bastion and answers the probe of a read route that takes a body (R-BAS-014).
// A write is refused whether it came as a probe or, by a defect of the Bastion, as the real request: the
// Bastion token makes every write bastion_read_only.
func (o *observer) Matched(w http.ResponseWriter, _ *http.Request, mt bastionmain.RouteMatch) (bool, error) {
	st := o.st
	st.setRoute(mt)
	if mt.Method == http.MethodGet || mt.Method == http.MethodHead {
		return false, nil
	}
	if !readRoute(mt) {
		st.mu.Lock()
		st.probe = st.rec.Probe
		st.mu.Unlock()
		return false, st.m.readOnly(mt)
	}
	if !st.rec.Probe {
		// The real request of a read route that takes a body, forwarded after a probe answered 204.
		return false, nil
	}
	st.mu.Lock()
	st.probe = true
	st.mu.Unlock()
	w.Header().Set(iface.HeaderRoute, iface.RouteReadWithBody)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
	return true, nil
}

// readRoute reports whether the route reads: a kind that serves content or documents and the coarse action
// read. A kind this build does not know is no read route.
func readRoute(mt bastionmain.RouteMatch) bool {
	switch mt.Kind {
	case bastionmain.KindDownload, bastionmain.KindMetadata, bastionmain.KindComputed, bastionmain.KindVolatile:
		return mt.Action == authz.Read
	}
	return false
}

// readOnly is the refusal of a write in the shape of the format: 403 bastion_read_only, whose detail names
// the main installation's URL of the addressed repository (R-BAS-014).
func (m *Marker) readOnly(mt bastionmain.RouteMatch) error {
	detail := "This is a read-only Bastion; publish to the main installation."
	if base := strings.TrimRight(m.o.MainURL, "/"); base != "" && !mt.Repo.IsZero() {
		target := base + qname.ProtocolBase("/api/"+mt.Format+"/{project}/{key}/", mt.Repo)
		switch {
		case mt.Format == bastionmain.FormatFiles:
			target = base + qname.FilesBase(mt.Repo)
		case mt.Format != "" && mt.Format == m.o.RegistryRoot:
			// An image is pushed to a name below the host, not to a URL (R-OCI-001): quader.example.com/web/docker-local.
			if u, err := url.Parse(base); err == nil && u.Host != "" {
				target = u.Host + "/" + mt.Repo.String()
			}
		}
		detail = "This is a read-only Bastion of " + base + "; publish to " + target
	}
	return errs.E(errs.Forbidden, errs.CodeBastionReadOnly, detail)
}

// Decided computes the caller's read shape on the addressed repository (R-BAS-012). The subject and the
// index are the ones the decision just used, so the shape agrees with the decision of this request; both are
// kept for the shape on the response's package, which is known only at the status line (R-BAS-082).
func (o *observer) Decided(r *http.Request, d bastionmain.RouteDecision) {
	st := o.st
	st.setRoute(d.Match)
	if st.m.o.Shapes == nil || d.Index == nil {
		return
	}
	st.mu.Lock()
	st.decided = &decision{ctx: r.Context(), sub: d.Subject, idx: d.Index}
	st.mu.Unlock()
	key := d.Match.Repo
	shapes, err := st.m.o.Shapes.Shapes(r.Context(), d.Index, d.Subject, []qname.Repo{key})
	if err != nil {
		// The marking stays without a shape: the response is `pass`, never shared, and a package of it is partial.
		st.m.o.Log.Warn("bastion: the read shape could not be computed", "repository", key.String(), "error", err)
		return
	}
	st.mu.Lock()
	st.whole = shapes[key].Read
	st.read = bastionmain.CapRegistryShape(st.whole, st.grant != nil)
	st.mu.Unlock()
}

// packageRead is the caller's shape on the response's package pkg of the repository key (R-BAS-080, R-BAS-082): full on every
// package for a full shape on the repository, partial on every package for none or no shape, and the decision point's package
// shape, computed on the index the request decided on, for a partial one; a registry token's grant limits it to the images it
// holds pull on. It runs at the status line, once per response that names a package, outside the state's lock.
func (m *Marker) packageRead(whole iface.Read, d *decision, grant *httpserver.RegistryGrant, key qname.Repo, pkg string) iface.Read {
	read := iface.ReadPartial
	switch {
	case whole == iface.ReadFull:
		read = iface.ReadFull
	case whole == iface.ReadPartial && d != nil:
		got, err := m.o.Shapes.PackageShapes(d.ctx, d.idx, d.sub, key, []string{pkg})
		if err != nil {
			m.o.Log.Warn("bastion: the read shape of a package could not be computed", "repository", key.String(), "error", err)
			break
		}
		read = got[pkg]
	}
	return bastionmain.CapRegistryPackage(read, grant != nil, grant.PullsImage(key, pkg))
}

// marking derives the headers of the response from the state and the response headers h at the status line.
func (s *state) marking(status int, h http.Header) iface.Marking {
	s.mu.Lock()
	mk := iface.Marking{Class: iface.ClassPass, Interface: iface.InterfaceVersion, Subject: s.subject}
	match, kind, probe, read, whole, decided, grant := s.match, s.kind, s.probe, s.read, s.whole, s.decided, s.grant
	s.mu.Unlock()
	if status == http.StatusUnauthorized && !insufficientScope(h) {
		// A rejected credential, or a caller the main installation asks to sign in: either way the credential
		// is of no use to the Bastion (R-BAS-013). A registry token whose grant lacks the route's action is
		// another matter: the credential is valid and the client fetches a token for the scope the challenge
		// names, so the subject the authentication stage accepted stands (R-BAS-052).
		mk.Subject = iface.Subject{Kind: iface.SubjectInvalid}
	}
	if match == nil {
		return mk
	}
	mk.Repository, mk.Read = match.Repo.String(), read
	if probe {
		return mk
	}
	content := kind == bastionmain.KindDownload || kind == bastionmain.KindMetadata || kind == bastionmain.KindComputed
	switch {
	case content && (status == http.StatusOK || status == http.StatusNotModified):
		// Serving a 200 counted a download when the core's download recording noted it (R-BAS-059); a 304 never does.
		mk.Counted = status == http.StatusOK && s.absence.Counted()
		// The response's package, when every rights decision of the request named the one package (R-BAS-081), and the
		// caller's shape on it (R-BAS-082).
		if pkg := s.absence.Package(); iface.ValidPackage(pkg) {
			mk.Package, mk.PackageRead = pkg, s.m.packageRead(whole, decided, grant, match.Repo, pkg)
		}
		mk.Class = classOf(kind, mk)
		if status == http.StatusOK && s.handover() != "" {
			// The serve point answered the forward with the file's 200 without its bytes and noted the handover's value
			// (storage.md R-STOR-095 step 11): only the class handover explains the empty body (R-BAS-073).
			mk.Class = iface.ClassHandover
		}
	case content && status == http.StatusNotFound && mk.Read == iface.ReadFull && s.absence.Absent():
		// absent stays tied to a full shape on the repository (Q-BAS-243).
		mk.Class = iface.ClassAbsent
	}
	return mk
}

// handover is the encoded value of the handover the serve point noted for a forward that offered one (R-BAS-073), "" for none:
// the class handover and the header Quader-Bastion-Handover go to a recognized forward that carried the offer and to nothing
// else.
func (s *state) handover() string {
	if !s.rec.Offered || s.rec.Route || s.rec.Probe {
		return ""
	}
	return s.absence.Handover()
}

// classOf is the class of a 200 or 304 of a content route: an artifact for a download; for a document, shared when the caller
// reads the whole repository or the whole package the document belongs to (Q-BAS-244), private for any other shape, and pass
// without one.
func classOf(kind string, mk iface.Marking) iface.Class {
	if kind == bastionmain.KindDownload {
		return iface.ClassArtifact
	}
	switch {
	case mk.Read == iface.ReadFull || (mk.Package != "" && mk.PackageRead == iface.ReadFull):
		return iface.ClassShared
	case mk.Read == iface.ReadPartial || mk.Read == iface.ReadNone:
		return iface.ClassPrivate
	}
	return iface.ClassPass
}

// insufficientScope reports whether a 401's challenge says error="insufficient_scope" (RFC 6750 section 3.1): the
// enforcement point's refusal of a registry token whose grant lacks the route's action (R-OCI-004).
func insufficientScope(h http.Header) bool {
	for _, v := range h.Values("WWW-Authenticate") {
		if strings.Contains(strings.ToLower(v), `error="`+registrytoken.ErrorInsufficientScope+`"`) {
			return true
		}
	}
	return false
}

// markWriter adds the marking at the status line.
type markWriter struct {
	http.ResponseWriter
	st    *state
	wrote bool
}

func (w *markWriter) mark(code int) {
	if w.wrote {
		return
	}
	// 1xx informational responses do not start the response.
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		return
	}
	w.wrote = true
	h := w.Header()
	// Nothing below this stage states a marking but the observer's probe answer.
	probeRoute := h.Get(iface.HeaderRoute)
	iface.StripMarking(h)
	if w.st.probe && probeRoute != "" {
		h.Set(iface.HeaderRoute, probeRoute)
	}
	if w.st.rec.Route {
		// An interface route has no addressed repository and no caller but the Bastion.
		h.Set(iface.HeaderClass, string(iface.ClassPass))
		h.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
		return
	}
	mk := w.st.marking(code, h)
	mk.Set(h)
	if mk.Class == iface.ClassHandover {
		// The value is a bearer link for its life (H-12): it goes into this one header, which the Bastion's client strips before
		// its engine sees the answer, and no request log keeps a header (R-OPS-010, R-OPS-013).
		h.Set(iface.HeaderHandover, w.st.handover())
	}
	if mt := w.st.m.o.Metrics; mt != nil && mt.Forwarded != nil {
		class := string(mk.Class)
		if w.st.probe {
			class = ClassProbe
		}
		mt.Forwarded.WithLabelValues(w.st.rec.Bastion, class).Inc()
	}
}

// WriteHeader implements http.ResponseWriter.
func (w *markWriter) WriteHeader(code int) {
	w.mark(code)
	w.ResponseWriter.WriteHeader(code)
}

// Write implements http.ResponseWriter.
func (w *markWriter) Write(p []byte) (int, error) {
	w.mark(http.StatusOK)
	return w.ResponseWriter.Write(p)
}

// Flush implements http.Flusher.
func (w *markWriter) Flush() {
	w.mark(http.StatusOK)
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap returns the wrapped writer for http.ResponseController.
func (w *markWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// stripWriter removes every iface.HeaderPrefix header from the response of a request that is no recognized
// Bastion request (R-BAS-013).
type stripWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *stripWriter) strip(code int) {
	if w.wrote || (code >= 100 && code < 200 && code != http.StatusSwitchingProtocols) {
		return
	}
	w.wrote = true
	iface.StripMarking(w.Header())
}

// WriteHeader implements http.ResponseWriter.
func (w *stripWriter) WriteHeader(code int) {
	w.strip(code)
	w.ResponseWriter.WriteHeader(code)
}

// Write implements http.ResponseWriter.
func (w *stripWriter) Write(p []byte) (int, error) {
	w.strip(http.StatusOK)
	return w.ResponseWriter.Write(p)
}

// Flush implements http.Flusher.
func (w *stripWriter) Flush() {
	w.strip(http.StatusOK)
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap returns the wrapped writer for http.ResponseController.
func (w *stripWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
