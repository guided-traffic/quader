package signatures

// The states of the member signatures of a file and a version (R-SIG-034), and of the badge (R-SIG-009).
const (
	BadgeVerifiedInternal = "verified_internal"
	BadgeVerifiedPublic   = "verified_public"
	BadgeUntrusted        = "untrusted"
	BadgeInvalid          = "invalid"
	BadgeNotChecked       = "not_checked"
	BadgeUnsupported      = "unsupported"
	BadgeNone             = "none"
)

// The states of a trust row (R-SIG-008), as internal/signatures/trust names them.
const (
	TrustPublic    = "public"
	TrustInternal  = "internal"
	TrustUntrusted = "untrusted"
	TrustBlocked   = "blocked"
)

// Counted is what the badge rule reads of one signature: its role, its integrity state, its trust state in the reader's scope ("" when
// it has no trust row) and whether that row is stale.
type Counted struct {
	Role  string
	State State
	Trust string
	Stale bool
}

// Badge is the state of an artifact (R-SIG-009, Q-SIG-25): over the signatures of the roles publisher and provenance, in this order,
// one internal is verified_internal, one public verified_public, one untrusted, blocked or no_key untrusted, one invalid invalid;
// otherwise not_checked (pending, failed), unsupported (unsupported, malformed) or none. A registry signature and an attestation never
// decide it. A valid signature whose trust row is not computed yet reads untrusted. stale says a deciding trust row lags a change of
// anchors (H-1175).
func Badge(sigs []Counted) (state string, stale bool) {
	best, bestStale := BadgeNone, false
	for _, c := range sigs {
		if c.Role != RolePublisher && c.Role != RoleProvenance {
			continue
		}
		s := badgeOf(c)
		switch r := badgeRank(s); {
		case r < badgeRank(best):
			best, bestStale = s, c.Stale
		case r == badgeRank(best):
			bestStale = bestStale || c.Stale
		}
	}
	return best, bestStale && (best == BadgeVerifiedInternal || best == BadgeVerifiedPublic || best == BadgeUntrusted)
}

// badgeRank orders the states of the rule of R-SIG-009: the lower wins.
func badgeRank(s string) int {
	switch s {
	case BadgeVerifiedInternal:
		return 0
	case BadgeVerifiedPublic:
		return 1
	case BadgeUntrusted:
		return 2
	case BadgeInvalid:
		return 3
	case BadgeNotChecked:
		return 4
	case BadgeUnsupported:
		return 5
	}
	return 6
}

// badgeOf is the state one signature would give the artifact alone.
func badgeOf(c Counted) string {
	switch c.State {
	case StateValid:
		switch c.Trust {
		case TrustInternal:
			return BadgeVerifiedInternal
		case TrustPublic:
			return BadgeVerifiedPublic
		}
		return BadgeUntrusted
	case StateNoKey:
		return BadgeUntrusted
	case StateInvalid:
		return BadgeInvalid
	case StatePending, StateFailed:
		return BadgeNotChecked
	case StateUnsupported, StateMalformed:
		return BadgeUnsupported
	}
	return BadgeNone
}
