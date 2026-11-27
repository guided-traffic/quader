package bastionmain

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// Recognition is the result of recognizing a request of a Bastion (R-BAS-008, Q-BAS-25): the request carried a
// valid `bst` token of a registration, in Quader-Bastion-Authorization on a forwarded request or in
// Authorization on an interface route. A request with Quader-Bastion-* headers and no valid token is served as
// an ordinary request and carries no Recognition; one with an invalid token is answered 401
// bastion_token_invalid before it gets this far.
//
// The recognition stage (work package 2) stores it with [WithRecognition]. Nothing else may: a Recognition in
// a context is the proof that the token was checked, which the shared authorization step of the interface
// routes and the marking rely on.
type Recognition struct {
	// Bastion is the registration's name and BastionID its principal ID.
	Bastion   string
	BastionID string
	// TokenID is the ID of the Bastion token that authenticated the request and TokenExpiresAt its expiry
	// (the handshake reports both, R-BAS-010).
	TokenID        string
	TokenExpiresAt time.Time
	// Interface is the interface version the request announced; recognition requires it to equal
	// iface.InterfaceVersion, so after recognition the two are the same number.
	Interface int
	// Replica is the Quader-Bastion-Replica header, empty when absent.
	Replica string
	// Route is true on a route of the interface (below iface.PathPrefix): the token was taken from
	// Authorization and the request is no forwarded read. False on a forwarded request, which the caller's
	// own credential decides (R-BAS-005).
	Route bool
	// Offer is the Bastion's offer to take a handover per blob instead of the bytes (R-BAS-072): the value of
	// Quader-Bastion-Handover on a forwarded GET, read by the recognition stage. Offered is false when the forward carried no
	// offer, more than one, or a value iface.ParseHandoverOffer refuses (no offer, R-BAS-073), and on every other request. The
	// header itself never reaches a stage behind the recognition: the serve point reads the offer from here alone
	// (storage.md R-STOR-095 step 1).
	Offer   iface.HandoverOffer
	Offered bool
	// ForHead is true on a forwarded GET that the Bastion sent to answer a caller's HEAD from what it keeps (R-BAS-021, Q-BAS-479):
	// Quader-Bastion-For-Head with its one value, read by the recognition stage like the offer and removed from the request. The
	// answer is the GET's, marking included; the download recording skips it ([ForHead]).
	ForHead bool
	// FromEgress is true when the request came from one of the networks its registration lists as the addresses its Bastion
	// connects from (R-BAS-017, Q-BAS-334): the address is the one stage 5 resolved with server.trusted_proxies, before anything
	// the Bastion forwards counts. Only then do the caller's address and host a forward carries count, and the client address of a
	// check (Q-BAS-430, Q-BAS-432); otherwise the request is the Bastion's own, from the address it came from.
	FromEgress bool
}

type recognitionKey struct{}

// WithRecognition returns ctx carrying rec. Only the recognition stage calls it.
func WithRecognition(ctx context.Context, rec Recognition) context.Context {
	return context.WithValue(ctx, recognitionKey{}, rec)
}

// RecognitionFrom returns the recognition of the request, if it is a Bastion request.
func RecognitionFrom(ctx context.Context) (Recognition, bool) {
	rec, ok := ctx.Value(recognitionKey{}).(Recognition)
	return rec, ok
}

// ForHead reports whether the request is a recognized forward that a Bastion sent as a GET to answer a caller's HEAD of a document
// (R-BAS-059, Q-BAS-479): its answer is no download, so the download statistics, the repository's activity, the event
// artifact.downloaded and the download figures of the limits leave it out, as they leave out a HEAD. The statement holds only once
// the request matched a route of a document (Q-BAS-482): a Bastion sends it for nothing else, and on a download route, or before the
// route is known, the GET counts as the download it is, so that a Bastion token never takes an artifact's download out of the
// count or out of the download figures. Any other request, and a request with the header that is no recognized forward, reports
// false. It is read where a download is counted, never earlier, because the route is known only after the limits' stage.
func ForHead(ctx context.Context) bool {
	rec, ok := RecognitionFrom(ctx)
	return ok && !rec.Route && rec.ForHead && absenceFrom(ctx).documentRoute()
}

// StatesForHead reports whether the request is a recognized forward that carried the statement of a HEAD, whatever its route: the
// limits' stage, which runs before the route is known, asks it to decide whether to ask [ForHead] later at all.
func StatesForHead(ctx context.Context) bool {
	rec, ok := RecognitionFrom(ctx)
	return ok && !rec.Route && rec.ForHead
}
