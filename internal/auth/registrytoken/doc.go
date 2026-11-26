// Package registrytoken is the registry token of the OCI registry's token service (R-OCI-004 to R-OCI-006, Q-OCI-5): a
// short-lived, self-contained credential that quader issues at /v2/_token and verifies itself on every route below /v2/.
//
// A registry token narrows the quader token that authenticated the token request; it never widens it. It names that quader
// token by its ID, its type and the SHA-256 of its secret, so the authentication stage verifies it with the token cache of
// R-AUTH-023 and a revocation of the quader token stops the registry token within the bound that holds for the quader token
// (ADR-163). Its grant is a ceiling of the actions the distribution token protocol names (pull, push, delete) per
// repository and image name, and the decision point decides every request again.
//
// The package holds:
//
//   - the form (token.go): qdr_rgt_ + base64url(payload) + "." + base64url(HMAC-SHA-256(key, "qdr_rgt_" + base64url(payload))),
//     the payload "2:<opaque key id>:<readable JSON claims>:<sealed claims>", at most 4 KiB; [Signer] issues it and verifies it and
//     the payload "1:<key id>:<JSON claims>" of the tokens issued before (bastion.md R-BAS-115, Q-BAS-388);
//   - the MAC keys and the sealing keys ([NewSigner] in token.go): derived with HKDF-SHA-256 from every key of the stored-secret key
//     ring with the infos "quader registry token v1" and "quader registry token seal v2" (secrets.KeyRing.Derive, R-ARCH-021), the
//     active key signing and sealing;
//   - the grant (grant.go): the actions, the entries and the check the registry root's enforcement point makes;
//   - the scope grammar of the distribution token protocol (scope.go) and the challenge of a 401 below /v2/ (challenge.go),
//     which the registry root shares;
//   - the metrics of the token service (metrics.go).
//
// The grant decision (the coarse check of each scope entry) and the enforcement point below /v2/ are the registry root's,
// in internal/api/registry: this package imports neither the decision point nor the repository model, so the
// authentication stage that verifies registry tokens does not depend on them.
//
// It is not a reference token (R-AUTH-022): it is never stored, and the scanner pattern of R-AUTH-022 does not match it.
package registrytoken
