package v1

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
)

// BastionServices are what the operations of the Bastion interface need (R-BAS-010, R-BAS-011, R-BAS-015,
// R-BAS-016). The composition root fills it; a member that is nil answers the operations that need it with
// 503 unavailable instead of a panic.
type BastionServices struct {
	// Config is the bastions section: the bounds of a check, the feed and the intake.
	Config config.Bastions
	// Release is the release of this installation and BaseURL its server.base_url (the handshake's
	// installation member).
	Release string
	BaseURL string
	// AnonymousReads reports whether an anonymous request reads anything through the default endpoint: some repository's
	// artifacts are `public` and the endpoint's own rule admits requests without a credential (R-AUTH-037, R-BAS-019).
	AnonymousReads func(ctx context.Context) bool
	// Authenticate is the authentication stage of the default endpoint without the Bastion short-circuit:
	// the check runs the caller's credential through it exactly as a direct request is (R-AUTH-023,
	// R-AUTH-038). The request it gets carries the caller's address in its context.
	Authenticate func(*http.Request) (httpserver.Principal, error)
	// Shapes computes the read shapes (work package 3).
	Shapes bastionmain.ShapeResolver
	// Feed, Contacts and Downloads are the revocation feed (*bastionmain.Feed), the last-contact recorder and the download
	// intake.
	Feed      BastionFeedSource
	Contacts  *bastionmain.Contacts
	Downloads *bastionmain.Intake
	// Formats is the handshake's format table (R-BAS-057), [BastionFormatTable] of the registered formats; nil answers an
	// empty table.
	Formats iface.HandshakeFormats
	// BoundRepository is the dispatcher's reading of the binding of the request's host root (dispatch.Dispatcher
	// .BoundRepository): the repository of the format that declares the registry root which host_roots binds the host to,
	// false for an unbound host (R-EP-006, R-BAS-057, Q-BAS-188). Nil binds nothing.
	BoundRepository func(*http.Request) (qname.Repo, bool, error)
	// Handover is the replica's snapshot of the registrations whose handover per blob is on (R-BAS-071,
	// *bastionmain.RegistrationSnapshot); nil answers false in every handshake.
	Handover interface{ On(bastionID string) bool }
	// Configuration answers the configuration of the recognized registration (R-BAS-093); nil answers the interim
	// configuration (bastionmain.InterimConfiguration) with revision 0 and the switch of Handover. Until a registration
	// holds a configuration of its own (E8), the composition root answers the interim one with the registration's
	// revision and switch.
	Configuration func(ctx context.Context, rec bastionmain.Recognition) (iface.Configuration, error)
}

// BastionFeedSource is what the handshake and the feed operation read of the revocation feed (R-BAS-010, R-BAS-015):
// *bastionmain.Feed implements it over PostgreSQL.
type BastionFeedSource interface {
	// Position is the feed position of a handshake.
	Position(ctx context.Context) (string, error)
	// Read answers one feed request.
	Read(ctx context.Context, after string, limit int) (iface.FeedAnswer, error)
}

// writeInterface answers an operation of the interface: the body is the wire form of the type
// (iface.Marshal), never cached, and the answer names the interface version. The recognition stage has set the
// version header already; setting it again keeps the handler correct without the stage.
func writeInterface(w http.ResponseWriter, r *http.Request, status int, v any) {
	b, err := iface.Marshal(v)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.writeInterface"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", iface.MediaType)
	h.Set("Cache-Control", "no-store")
	h.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// bastionLog is the request's logger.
func bastionLog(r *http.Request) *slog.Logger { return httpserver.Logger(r.Context()) }

// BastionPrincipal is the principal of a request on an interface route: the registration whose token the
// recognition stage checked. It holds no grant and decides nothing; the request log and the rate limits name it.
func BastionPrincipal(rec bastionmain.Recognition) httpserver.Principal {
	return httpserver.Principal{
		Kind: httpserver.KindBastion, ID: rec.BastionID, Name: rec.Bastion,
		Credential: httpserver.Credential{Method: httpserver.MethodBearer, TokenID: rec.TokenID, TokenType: httpserver.TokenType(iface.TokenTypeBST)},
	}
}

// BastionRouteAuth wraps the authentication stage of an endpoint's chain: a request on an interface route
// carries the proof of the recognition stage and no credential of a caller, so it is admitted as the
// registration whatever the anonymous switches say (R-BAS-008, Q-BAS-52); the authentication stage is where an
// anonymous request of an endpoint that admits none is refused, and it must not refuse the Bastion.
//
// Every other request is the stage's, a forward of a Bastion included: it carries the caller's own credential, and a Bastion
// sends no write (R-BAS-095), so nothing is admitted for routing alone. admitted, when set, is told of the registration on an
// interface route (the request log names it).
func BastionRouteAuth(stage func(*http.Request) (httpserver.Principal, error), admitted func(*http.Request, httpserver.Principal)) func(*http.Request) (httpserver.Principal, error) {
	return func(r *http.Request) (httpserver.Principal, error) {
		if rec, ok := bastionmain.RecognitionFrom(r.Context()); ok && rec.Route {
			p := BastionPrincipal(rec)
			if admitted != nil {
				admitted(r, p)
			}
			return p, nil
		}
		return stage(r)
	}
}
