package errs

// The two lists that let the walk of a virtual read go on past a member, the one rule of every format: the engine that npm, NuGet,
// Ansible Galaxy and generic files resolve through (internal/virtual) and OCI's own walk (internal/formats/oci) both ask them
// (repositories-and-proxy.md R-REPO-019 steps 4 and 7, formats/oci.md Q-OCI-245). An answer neither names ends the walk with that
// answer: a block of the vulnerability scanning, the malware gate or the signature gate, a gate's pending answer, and any refusal or
// 503 written later. The lists live here because a format may import errs and not the engine (R-ARCH-008).

// IsMemberMiss reports whether a member's answer is a miss the walk names, "this member does not have it" for the caller: a 404 of
// any code (a 404 says that nothing exists for the caller, R-AUTHZ-016), a read the caller may not make in the member (403 forbidden:
// an unreadable member is skipped and never turns the answer into 403, R-AUTHZ-009 rules 2 and 8), or a fill of its cache the caller
// may not make (403 populate_not_permitted, R-AUTHZ-008).
func IsMemberMiss(err error) bool {
	e, ok := As(err)
	if !ok {
		return false
	}
	switch e.Kind {
	case NotFound:
		return true
	case Forbidden:
		return e.Code == CodeForbidden || e.Code == CodePopulateNotPermitted
	}
	return false
}

// IsMemberFailure reports whether a member's answer is a failure the walk names, "this member could not be asked": its upstream could
// not be used (502 and 504, every code of R-REPO-029), is assumed offline or rate limits Quader, every fetch slot stayed busy, its
// stored object is lost (content_unavailable, Q-REPO-205), or a dependency answers 503 unavailable. The walk goes on, and the first
// such failure is the answer when no member serves the item (R-REPO-019 step 7). Any other 503 ends the walk: a gate's pending answer
// (the member holds the item and decides, R-MAL-018, signatures.md R-SIG-065) and a 503 of the replica's own database, which is no
// statement about the member.
func IsMemberFailure(err error) bool {
	e, ok := As(err)
	if !ok {
		return false
	}
	switch e.Kind {
	case UpstreamFailed, UpstreamTimeout:
		return true
	}
	switch e.Code {
	case CodeUpstreamAssumedOffline, CodeUpstreamRateLimited, CodeFetchQueueTimeout, CodeContentUnavailable, CodeUnavailable:
		return true
	}
	return false
}
