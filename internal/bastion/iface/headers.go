package iface

import "net/http"

// The headers of the interface (R-BAS-008, R-BAS-010, R-BAS-013, R-BAS-017, R-BAS-096). Names
// are canonical MIME header keys; compare them through net/http's Header, which
// canonicalizes.

// Headers a Bastion sends on every request to the main installation.
const (
	// HeaderInterface carries the interface version, on every request of the
	// Bastion and on every answer of the main installation to a Bastion request
	// (R-BAS-009).
	HeaderInterface = "Quader-Bastion-Interface"
	// HeaderAuthorization carries the Bastion token as "Bearer <token>" on a
	// forwarded request; the caller's own credential stays in Authorization
	// (R-BAS-008). On the interface routes the token is Authorization itself.
	HeaderAuthorization = "Quader-Bastion-Authorization"
	// HeaderReplica carries the pod name of the sending replica (R-BAS-008).
	HeaderReplica = "Quader-Bastion-Replica"
)

// Headers a Bastion sends on the handshake only: what the main installation records with the replica's contact
// (R-BAS-010). Each is optional: a Bastion that does not know the value sends nothing, and the contact then holds none.
const (
	// HeaderEdgeLimit is the edge figure of the ingress in front of the replica (R-BAS-093), in the form of
	// [EdgeLimit.Header]: the card warns where a per-address figure of the configuration lies above it.
	HeaderEdgeLimit = "Quader-Bastion-Edge-Limit"
	// HeaderCatalogueKept says whether the replica's copy of the public catalogue survives a restart (W-35, R-BAS-101):
	// "true" or "false".
	HeaderCatalogueKept = "Quader-Bastion-Catalogue-Kept"
)

// Headers the main installation adds to the response of a recognized Bastion
// request (R-BAS-013). They are stripped from every other response.
const (
	// HeaderClass is the [Class] of the response.
	HeaderClass = "Quader-Bastion-Class"
	// HeaderRepository is the addressed repository's qualified name "<projectKey>/<repoKey>".
	HeaderRepository = "Quader-Bastion-Repository"
	// HeaderRead is the caller's [Read] shape on the addressed repository.
	HeaderRead = "Quader-Bastion-Read"
	// HeaderSubject is the caller's subject in the format of [Subject.Header].
	HeaderSubject = "Quader-Bastion-Subject"
	// HeaderLimits is the caller's subject key and rule at the Bastion's place, in the form of [SubjectLimits.Header],
	// on an answer whose [HeaderSubject] is a token and for which the main installation resolved a rule (R-BAS-096). It is
	// a member of the [Marking] ([Subject.Limits]).
	HeaderLimits = "Quader-Bastion-Limits"
	// HeaderCounted is [CountedDownload] on the response whose serving counted a
	// download at the main installation (R-BAS-059, Q-BAS-177): the Bastion records
	// it on the entry and reports every serve of that entry from its cache. It is
	// a member of the [Marking] ([Marking.Counted]).
	HeaderCounted = "Quader-Bastion-Counted"
	// HeaderPackage is the package the response belongs to (R-BAS-081, R-BAS-082), percent-encoded by the rule of
	// [Marking.Set]: present on a 200 or 304 of a Download, Metadata or Computed route whose request named exactly one
	// package, absent everywhere else. It is a member of the [Marking] ([Marking.Package]).
	HeaderPackage = "Quader-Bastion-Package"
	// HeaderPackageRead is the caller's [Read] shape on the package of [HeaderPackage], [ReadFull] or [ReadPartial],
	// present exactly with it (R-BAS-082). It is a member of the [Marking] ([Marking.PackageRead]).
	HeaderPackageRead = "Quader-Bastion-Package-Read"
)

// HeaderHandover is the one header of the interface that travels both ways (R-BAS-072, R-BAS-073). On a forward of the
// Bastion it is the offer to take a handover, a [HandoverOffer] in the form "accept; max-size=<n>"; on the answer of the
// main installation, whose class is then [ClassHandover], it is the [Handover]: base64url JSON with a signed link and, for
// an encrypted copy, a data key. Both sides redact it like Authorization in every log, span and support bundle (R-OPS-013),
// and the Bastion's client strips it from every answer before the engine sees it, like every header with [HeaderPrefix].
const HeaderHandover = "Quader-Bastion-Handover"

// HeaderForHead is [ForHeadValue] on a forwarded GET that a Bastion sends to answer a caller's HEAD from what it keeps
// (R-BAS-021, R-BAS-059, Q-BAS-479): the fill of a HEAD miss of a document and the conditional GET that revalidates a document
// for a HEAD. The main installation answers it like the GET it is, its marking included, but records no download for it: a
// HEAD is none. The Bastion sets it after the caller's headers were copied, so a caller's own value never reaches the main
// installation; the main installation reads it only on a recognized forward, and no stage behind the recognition sees it.
const HeaderForHead = "Quader-Bastion-For-Head"

// ForHeadValue is the one value of [HeaderForHead].
const ForHeadValue = "true"

// ForHead reports whether a request's header says that its GET is sent for a HEAD: exactly one value, [ForHeadValue]. Any
// other value, or more than one, is no such statement, and the GET counts as the download it is.
func ForHead(h http.Header) bool {
	vs := h.Values(HeaderForHead)
	return len(vs) == 1 && vs[0] == ForHeadValue
}

// HeaderPrefix starts every header of the interface. The main installation never
// echoes a request header with this prefix and redacts [HeaderAuthorization] and
// [HeaderHandover] like Authorization in every log, span and support bundle
// (R-BAS-008, R-BAS-073, R-OPS-013).
const HeaderPrefix = "Quader-Bastion-"

// The values of the headers above.
const (
	// CountedDownload is the value of [HeaderCounted]: serving the response
	// counted a download (R-BAS-059).
	CountedDownload = "download"
	// BearerScheme is the scheme of the Bastion token in [HeaderAuthorization] and
	// in Authorization on the interface routes.
	BearerScheme = "Bearer"
	// UserAgentPrefix starts the User-Agent of a Bastion: "quader-bastion/<release>"
	// (R-BAS-008, R-BAS-010).
	UserAgentPrefix = "quader-bastion/"
	// TokenTypeBST is the token type of the Bastion token (R-BAS-004).
	TokenTypeBST = "bst"
	// TokenPrefix is the prefix of a Bastion token.
	TokenPrefix = "qdr_bst_" //nolint:gosec // G101: the prefix of a token type, not a credential
)

// The forwarded-for headers the Bastion sends to the main installation
// (R-BAS-017). The Bastion writes exactly one entry in each family and replaces
// whatever the caller sent.
const (
	HeaderForwardedFor   = "X-Forwarded-For"
	HeaderForwardedProto = "X-Forwarded-Proto"
	HeaderForwardedHost  = "X-Forwarded-Host"
	HeaderForwardedPort  = "X-Forwarded-Port"
	HeaderForwarded      = "Forwarded"
)
