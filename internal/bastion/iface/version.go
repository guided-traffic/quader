package iface

// InterfaceVersion is the version of the Bastion interface (R-BAS-009,
// Q-BAS-20, Q-BAS-26): a positive integer that starts at 1 and is raised by every
// change of the interface, whatever its kind. Both sides carry it in
// [HeaderInterface]; main installation and Bastion talk only when the two values
// are equal.
//
// A change of this value belongs in the same change as the regenerated
// testdata/interface.txt (go test ./internal/bastion/iface -run TestR_BAS_009
// -update-interface), and the release notes announce it.
//
// Version 5 (R-BAS-087): the marking's package headers, the check's packages,
// and the unverified reading of a registry token's grant and expiry.
//
// Version 6 (R-BAS-073): the handover per blob. The header [HeaderHandover] with
// the offer [HandoverOffer] and the value [Handover], the class [ClassHandover],
// and the handshake's member handover.
//
// Version 7 (R-BAS-091): the format table's retryAfterMaxSeconds, the longest wait
// the Bastion's own refusals tell a client of the format ([HandshakeFormat]).
const InterfaceVersion = 7
