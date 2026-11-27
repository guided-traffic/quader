package iface

// The problem codes the two sides exchange or produce for the interface
// (R-BAS-008, R-BAS-009, R-BAS-029, R-BAS-095, R-BAS-133). The main installation answers the
// first five as problem details of the management API, also on a package route;
// the Bastion produces the rest itself. internal/errs registers every one of them
// with its kind, which gives the HTTP status.
const (
	// CodeTokenInvalid (401): the Bastion token on a request is invalid, revoked
	// or expired. It concerns the Bastion, never the
	// caller, and the Bastion never passes it on (R-BAS-008, R-BAS-027).
	CodeTokenInvalid = "bastion_token_invalid"
	// CodeInterfaceMismatch (400): the request's [HeaderInterface] differs from
	// the main installation's, or is missing. The problem carries
	// [ProblemMemberInterfaceVersion] (R-BAS-009).
	CodeInterfaceMismatch = "bastion_interface_mismatch"
	// CodeBaseURLUnknown (409): client-setup for a Bastion whose reported base URL
	// is not an entry of server.additional_base_urls (R-BAS-007).
	CodeBaseURLUnknown = "bastion_base_url_unknown"
	// CodeNotImplemented (501): an operation of the Bastion work that is not built
	// yet; it leaves with the last stub.
	CodeNotImplemented = "bastion_not_implemented"
	// CodeDisabled (503): a forward, a check or a catalogue route of a Bastion
	// whose registration an administrator has disabled (R-BAS-133). It concerns
	// the Bastion, never the caller, and the Bastion never passes it on.
	CodeDisabled = "bastion_disabled"
)

// The codes of the Bastion's own answers (R-BAS-029, Q-BAS-321). Each is named by
// its status and nothing behind it, so that a caller learns no more than the status
// line tells (R-BAS-099): not_found and unavailable are the core's codes as well.
// The statuses are in the table of R-BAS-029; a 401 carries the challenge of
// R-AUTH-002 for the path family and every answer carries Cache-Control: no-store.
const (
	// CodeReadOnly (403): a write through a Bastion (R-BAS-095); the main
	// installation's backstop refuses a forwarded write with it too.
	CodeReadOnly = "read_only"
	// CodeNotFound (404): a path outside the mirrored families, answered by the
	// Bastion itself (R-BAS-003, R-BAS-018).
	CodeNotFound          = "not_found"
	CodeBadGateway        = "bad_gateway"          // 502
	CodeUnavailable       = "unavailable"          // 503, Retry-After: 30
	CodeBusy              = "busy"                 // 503, Retry-After: 1
	CodeGatewayTimeout    = "gateway_timeout"      // 504
	CodeAuthTimeout       = "auth_timeout"         // 429
	CodeAddressBlocked    = "address_blocked"      // 429
	CodeInvalidToken      = "invalid_token"        // 401, shared with the main installation's registry
	CodeCredentialsNeeded = "credentials_required" // 401, shared with the main installation's registry
	CodeRateLimited       = "rate_limited"         // 429, shared
	CodeMethodNotAllowed  = "method_not_allowed"   // 405, shared
	CodePayloadTooLarge   = "payload_too_large"    // 413, shared
	CodeMisdirected       = "misdirected_request"  // 421, shared
)

// ProblemMemberInterfaceVersion is the member of the problem details of a
// [CodeInterfaceMismatch] answer that carries the main installation's version
// (R-BAS-009).
const ProblemMemberInterfaceVersion = "interfaceVersion"

// The problem media type of every error answer of the interface.
const ProblemMediaType = "application/problem+json"
