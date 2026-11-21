package limits

import (
	"net/netip"
	"strings"
)

// SubjectKind is the kind of the unit a budget hangs on (R-LIM-001). The values are the usage store's subject_kind.
type SubjectKind string

// The subject kinds.
const (
	KindUser           SubjectKind = "user"
	KindTechnicalToken SubjectKind = "technical_token"
	KindCI             SubjectKind = "ci"
	KindAnonymous      SubjectKind = "anonymous"
	KindSystem         SubjectKind = "system"
)

// APIName is the kind as the API spells it (technicalToken).
func (k SubjectKind) APIName() string {
	if k == KindTechnicalToken {
		return "technicalToken"
	}
	return string(k)
}

// ParseSubjectKind reads a kind in the API's or the store's spelling.
func ParseSubjectKind(s string) (SubjectKind, bool) {
	switch s {
	case "user", "ci", "anonymous", "system":
		return SubjectKind(s), true
	case "technicalToken", "technical_token":
		return KindTechnicalToken, true
	}
	return "", false
}

// UnknownAddress is the subject of an anonymous request without a client address (a command, a test): one subject for all
// of them (R-LIM-002).
const UnknownAddress = "unknown"

// SessionCredential is the credential column of a user's request through a UI session (R-LIM-027).
const SessionCredential = "session"

// Subject is the unit a request is charged to (R-LIM-001). Ref never holds a token secret or a hash of one.
type Subject struct {
	Kind SubjectKind
	// Ref is the principal ID of a user, the technical token's ID, "<issuer ID>:<project ID>" of a CI project, the address
	// prefix of an anonymous caller, or "system".
	Ref string
	// Credential is "session" or the personal token's ID for a user, ScanCredential for the image scanning's pull of the
	// subject system, empty for every other kind.
	Credential string
	// Groups are the group IDs the authentication stage resolved for a user, or the groups of a technical token's account;
	// a group rule matches through them (R-LIM-013 step 3.2).
	Groups []string
	// Admin says the subject is an administrator of R-LIM-012: decided at the instance only, the kind default Unlimited.
	Admin bool
}

// String is the subject as the request log names it (R-LIM-030): user:<id>, technical_token:<id>, ci:<issuer>:<project>,
// anonymous:<prefix>, system.
func (s Subject) String() string {
	if s.Kind == KindSystem {
		return string(KindSystem)
	}
	return string(s.Kind) + ":" + s.Ref
}

// System is the subject of the installation's own work (R-LIM-001, Q-LIM-19).
func System() Subject { return Subject{Kind: KindSystem, Ref: string(KindSystem)} }

// CallerKind is the principal kind of a request as the stage hands it over; the values are those of
// httpserver.PrincipalKind, which this domain package cannot import.
type CallerKind string

// The caller kinds the table of R-LIM-001 names. A kind it does not name is charged to the client address.
const (
	CallerAnonymous      CallerKind = "anonymous"
	CallerUser           CallerKind = "user"
	CallerServiceAccount CallerKind = "service_account"
	CallerCI             CallerKind = "ci"
	CallerBreakGlass     CallerKind = "break_glass"
	CallerBastion        CallerKind = "bastion"
	// CallerSystem is a system subject on a request: system:vuln-scanner-trivy, a scan token on the scan listener (R-OCI-037), or
	// system:malware-scanner-clamav, a blob token there (R-MAL-006).
	CallerSystem CallerKind = "system"
)

// KnownCallerKinds lists the kinds the table of R-LIM-001 names: a test compares it with the principal kinds of
// internal/httpserver, so a new kind fails until it is named here.
func KnownCallerKinds() []CallerKind {
	return []CallerKind{CallerAnonymous, CallerUser, CallerServiceAccount, CallerCI, CallerBreakGlass, CallerBastion, CallerSystem}
}

// ScanCredential is the credential column of the requests of the image scanning's pull, charged to the subject system
// (Q-LIM-66): the usage store shows what the scanner read per repository, under the name of the unit that read
// (ha-and-helm.md Q-HA-373).
const ScanCredential = "vuln-scanner-trivy" //nolint:gosec // G101: the name of the unit that read, not a credential

// MalwareScanCredential is the credential column of the malware scanner's reads of a blob on the scan listener, charged to the
// subject system under the repository its blob token names (Q-LIM-80), under the name of the unit that read (Q-HA-373).
const MalwareScanCredential = "malware-scanner-clamav"

// Caller is what the authentication stage decided about a request, as plain values (the edge converts its principal).
type Caller struct {
	Kind      CallerKind
	Anonymous bool
	// PrincipalID is the principal-table ID.
	PrincipalID string
	// SessionID is set for a request through a UI session; TokenID for a quader token (the underlying token of a registry
	// token); TechnicalTokenID for a token of a technical token (R-AUTH-045).
	SessionID        string
	TokenID          string
	TechnicalTokenID string
	// CIIssuerID and CIProjectID are the issuer's immutable ID and the project ID of a CI token's frozen bundle (Q-LIM-1).
	CIIssuerID  string
	CIProjectID string
	// Groups are the group IDs the stage resolved.
	Groups []string
	// Admin is whether the principal holds the system role admin now, whatever the token's scope (Q-LIM-18).
	Admin bool
	// Client is the client address after the trusted-proxy rule (R-ARCH-006).
	Client netip.Addr
	// System is the name of a system subject (CallerSystem): vuln-scanner-trivy or malware-scanner-clamav, the credential column its reads
	// are recorded with (Q-LIM-66, Q-LIM-80). Empty is the image scanner's.
	System string
}

// SubjectOf derives the subject of a request from what authentication decided (R-LIM-001). It reads nothing else.
func SubjectOf(c Caller) (Subject, bool) {
	anon := Subject{Kind: KindAnonymous, Ref: AddressPrefix(c.Client)}
	if c.Anonymous {
		return anon, true
	}
	switch c.Kind {
	case CallerUser, CallerBreakGlass:
		if c.PrincipalID == "" {
			return anon, false
		}
		cred := SessionCredential
		if c.TokenID != "" {
			cred = c.TokenID
		}
		return Subject{Kind: KindUser, Ref: c.PrincipalID, Credential: cred, Groups: c.Groups, Admin: c.Admin || c.Kind == CallerBreakGlass}, true
	case CallerServiceAccount:
		if c.TechnicalTokenID == "" {
			// A service account's credential without a technical token is not a case of R-LIM-001: charged to the address.
			return anon, false
		}
		return Subject{Kind: KindTechnicalToken, Ref: c.TechnicalTokenID, Groups: c.Groups, Admin: c.Admin}, true
	case CallerCI:
		if c.CIIssuerID == "" || c.CIProjectID == "" {
			return anon, false
		}
		return Subject{Kind: KindCI, Ref: c.CIIssuerID + ":" + c.CIProjectID}, true
	case CallerAnonymous:
		return anon, true
	case CallerSystem:
		// The image scanning's pull and the malware scanner's read are the installation's own work: recorded under the repository,
		// with the scanner's name as the credential, limited by nothing, and they take no parallel lease (Q-LIM-66, Q-LIM-80).
		s := System()
		s.Credential = ScanCredential
		if c.System == MalwareScanCredential {
			s.Credential = MalwareScanCredential
		}
		return s, true
	}
	return anon, false
}

// AddressPrefix is the anonymous subject of a client address (R-LIM-002): an IPv4 address itself, the IPv6 /64 it lies in
// written as <prefix>::/64 in canonical form, an IPv4-mapped address as its IPv4 address, the zone dropped. An invalid
// address is UnknownAddress.
func AddressPrefix(a netip.Addr) string {
	if !a.IsValid() {
		return UnknownAddress
	}
	a = a.WithZone("").Unmap()
	if a.Is4() {
		return a.String()
	}
	p, err := a.Prefix(64)
	if err != nil {
		return UnknownAddress
	}
	return p.String()
}

// addressOfSubject reads the address of an anonymous subject back, for the address rules: the IPv4 address, or the first
// address of the /64.
func addressOfSubject(ref string) (netip.Addr, bool) {
	if strings.Contains(ref, "/") {
		p, err := netip.ParsePrefix(ref)
		if err != nil {
			return netip.Addr{}, false
		}
		return p.Addr(), true
	}
	a, err := netip.ParseAddr(ref)
	return a, err == nil
}

// CanonicalPrefix reads the prefix of an address rule (R-LIM-011): an IPv4 prefix /8 to /32 or an IPv6 prefix /32 to /64,
// stored with its host bits cleared. A bare address is its own /32 or /64 is refused for IPv6 (it would be longer than /64).
func CanonicalPrefix(s string) (netip.Prefix, bool) {
	if !strings.Contains(s, "/") {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return netip.Prefix{}, false
		}
		a = a.Unmap()
		if a.Is4() {
			return netip.PrefixFrom(a, 32), true
		}
		return netip.Prefix{}, false
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, false
	}
	a := p.Addr()
	if a.Zone() != "" {
		return netip.Prefix{}, false
	}
	bits := p.Bits()
	if a.Is4In6() {
		a = a.Unmap()
		bits -= 96
	}
	switch {
	case a.Is4() && (bits < 8 || bits > 32):
		return netip.Prefix{}, false
	case a.Is6() && (bits < 32 || bits > 64):
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(a, bits).Masked(), true
}
