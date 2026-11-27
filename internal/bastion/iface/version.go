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
//
// Version 8 (R-BAS-093 to R-BAS-099, R-BAS-101 to R-BAS-103, Q-BAS-322): one raise for the limits at a Bastion and its
// web interface. The handshake's [Configuration] (the top-level handover moved into its switches); the subject's
// [SubjectLimits] in the check answer and [HeaderLimits] in the marking; the [UsageReport] and [ReplicaStatus] of the
// download batch; the format table's [ErrorShape] and readWithBody; the Bastion's own codes named by their status
// (Q-BAS-321); the probe's headers gone; the contact's [HeaderEdgeLimit] and [HeaderCatalogueKept]; the registry token's
// payload version 2 read; the public catalogue's routes, types, path grammar and the feed kind [FeedCatalogue].
//
// Version 9 (R-BAS-021, R-BAS-059, R-LIM-045, Q-BAS-479, Q-BAS-480): the request header [HeaderForHead] of a GET a Bastion
// sends to answer a HEAD, which the main installation answers as the GET it is and counts as no download, and the status's
// [CacheStatus.Shared], which says whether the cache's figures are the replica's own or the whole Bastion's.
//
// Version 10 (R-BAS-128): what a visitor of the web interface still missed. The catalogue's document of a package's open findings
// per version ([CataloguePackageVulnerabilities]), the public set's projects with their display names and descriptions
// ([CatalogueProject]), and the configuration's [Display] with the installation's default form of a time.
const InterfaceVersion = 10
