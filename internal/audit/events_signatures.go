package audit

// The event types of signatures on artifacts (signatures.md R-SIG-039; the journal flags are Q-SIG-42's). What narrows trust after a
// restore point is journaled, so that a restore re-applies it (R-SIG-041): an anchor removed, an anchor switched off or with material
// removed, a block, and the switch for project Owners turned off. An anchor added after the restore point is lost, which narrows.
//
// The actor of the job's and the task's events is system:signatures; of the anchors of the Helm values system:configuration.
const (
	// EventSignatureInvalid: a check made a signature invalid (R-SIG-006), once per signature.
	EventSignatureInvalid = "signature.invalid"
	// EventSignatureAnchorCreated: an anchor of any scope was created (R-SIG-015, R-SIG-018).
	EventSignatureAnchorCreated = "signature.anchor.created"
	// EventSignatureAnchorUpdated: an anchor changed. Journaled when it narrows (switched off, or material removed): the replay switches
	// it off or sets the narrower material again; the entry is optional for that reason.
	EventSignatureAnchorUpdated = "signature.anchor.updated"
	// EventSignatureAnchorRemoved: an anchor was removed. Journaled: the replay removes it again.
	EventSignatureAnchorRemoved = "signature.anchor.removed"
	// EventSignatureSettingsChanged: the switch for project Owners changed (R-SIG-019). Journaled when it was switched off: the replay
	// switches it off again; switching it on widens.
	EventSignatureSettingsChanged = "signature.settings.changed"
	// EventSignatureSignerBlocked: a block was added (R-SIG-020). Journaled: the replay blocks the signer again.
	EventSignatureSignerBlocked = "signature.signer.blocked"
	// EventSignatureSignerUnblocked: a block was lifted; it widens and is not journaled.
	EventSignatureSignerUnblocked = "signature.signer.unblocked"
	// EventSignatureTrustRootUpdated: the task signature-trust-root stored a newer public Sigstore root (R-SIG-017).
	EventSignatureTrustRootUpdated = "signature.trust_root.updated"
	// EventSignatureTrustRootFailed: a run of the task failed, at most once a day.
	EventSignatureTrustRootFailed = "signature.trust_root.failed"
	// EventSignatureDownloadRefused: a repository's gate refused a download (stage 3, R-SIG-075), or answered it pending after the wait,
	// at most once a minute per repository, path and client address on each replica.
	EventSignatureDownloadRefused = "signature.download_refused"
	// EventSignatureDownloadByWriter: a repository's gate served a download without an accepted signature only because the caller may
	// write to the repository and reads to sign (Q-SIG-211, Q-SIG-240), at most once a minute per repository, path, caller and client
	// address on each replica; outcome warning, with the reason a reader would have been refused for.
	EventSignatureDownloadByWriter = "signature.download_by_writer"
	// EventSignatureReleaseGranted: a holder of the triage right released an artifact for one repository's gate (stage 3, R-SIG-070). It
	// widens and is not journaled: a release granted after a restore point is lost.
	EventSignatureReleaseGranted = "signature.release.granted"
	// EventSignatureReleaseWithdrawn: a release was withdrawn. Journaled: the replay withdraws it again, and the Bastions drop the
	// repository's answers (R-SIG-075).
	EventSignatureReleaseWithdrawn = "signature.release.withdrawn"
)

func init() {
	fingerprints := `"fingerprints":{"type":"array","maxItems":64,"items":{"type":"object"}}`
	scope := `"scope":{"type":"string","enum":["shipped","configuration","installation","project"]}`
	anchorState := `{"type":"object","properties":{"title":{"type":"string"},"enabled":{"type":"boolean"},"note":{"type":"string"},` +
		fingerprints + `},"required":["title","enabled"],"additionalProperties":false}`
	Register(EventType{
		Name: EventSignatureInvalid, Category: CategoryContent, Mode: Transactional,
		Schema: `{"type":"object","properties":{"object":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"subject":{"type":"string","pattern":"^[0-9a-f]{64}$"},"subjectName":{"type":"string"},"scheme":{"type":"string"},` +
			`"role":{"type":"string"},"reason":{"type":"string"},"signer":{"type":"object"},` +
			`"repositories":{"type":"array","maxItems":100,"items":{"type":"string"}},"repositoryCount":{"type":"integer"}},` +
			`"required":["object","subject","scheme","role","reason","repositories","repositoryCount"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureAnchorCreated, Category: CategoryConfig, Mode: Transactional,
		Schema: `{"type":"object","properties":{"id":{"type":"integer"},` + scope + `,"project":{"type":"string"},` +
			`"kind":{"type":"string","enum":["ca","openpgp","public-key","sigstore"]},"title":{"type":"string"},` + fingerprints + `},` +
			`"required":["id","scope","kind","title","fingerprints"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureAnchorUpdated, Category: CategoryConfig, Mode: Transactional, Journal: JournalRevocation, Entry: EntryOptional,
		Schema: `{"type":"object","properties":{"id":{"type":"integer"},"before":` + anchorState + `,"after":` + anchorState + `},` +
			`"required":["id","before","after"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureAnchorRemoved, Category: CategoryConfig, Mode: Transactional, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{"id":{"type":"integer"},` + scope + `,"project":{"type":"string"},` + fingerprints + `},` +
			`"required":["id","scope","fingerprints"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureSettingsChanged, Category: CategoryConfig, Mode: Transactional, Journal: JournalRevocation, Entry: EntryOptional,
		Schema: `{"type":"object","properties":{` +
			`"before":{"type":"object","properties":{"projectAnchors":{"type":"boolean"}},"required":["projectAnchors"],"additionalProperties":false},` +
			`"after":{"type":"object","properties":{"projectAnchors":{"type":"boolean"}},"required":["projectAnchors"],"additionalProperties":false}},` +
			`"required":["before","after"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureSignerBlocked, Category: CategoryConfig, Mode: Transactional, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{"id":{"type":"integer"},` +
			`"kind":{"type":"string","enum":["fingerprint","serial","keyid","identity"]},"value":{"type":"string"},` +
			`"issuer":{"type":"string"},"reason":{"type":"string"}},` +
			`"required":["id","kind","value","reason"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureSignerUnblocked, Category: CategoryConfig, Mode: Transactional,
		Schema: `{"type":"object","properties":{"id":{"type":"integer"},` +
			`"kind":{"type":"string","enum":["fingerprint","serial","keyid","identity"]},"value":{"type":"string"},"issuer":{"type":"string"}},` +
			`"required":["id","kind","value"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureTrustRootUpdated, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"tufVersion":{"type":"integer"},` +
			`"targets":{"type":"array","maxItems":8,"items":{"type":"string"}}},` +
			`"required":["tufVersion","targets"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureDownloadRefused, Category: CategoryContent, Mode: Async,
		Schema: `{"type":"object","properties":{"repository":{"type":"string"},"path":{"type":"string"},` +
			`"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"reason":{"type":"string","enum":["missing","untrusted","not_chosen","invalid","blocked","not_checked","pending"]}},` +
			`"required":["repository","path","sha256","reason"],"additionalProperties":false}`,
	})
	gateReason := `{"type":"string","enum":["missing","untrusted","not_chosen","invalid","blocked","not_checked","pending"]}`
	Register(EventType{
		Name: EventSignatureDownloadByWriter, Category: CategoryContent, Mode: Async,
		Schema: `{"type":"object","properties":{"repository":{"type":"string"},"path":{"type":"string"},"artifact":{"type":"string"},` +
			`"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},"reason":` + gateReason + `},` +
			`"required":["repository","path","artifact","sha256","reason"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureReleaseGranted, Category: CategoryContent, Mode: Transactional,
		Schema: `{"type":"object","properties":{"repository":{"type":"string"},"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"refusedReason":` + gateReason + `,"reason":{"type":"string"}},` +
			`"required":["repository","sha256","refusedReason","reason"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureReleaseWithdrawn, Category: CategoryContent, Mode: Transactional, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{"repository":{"type":"string"},"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"reason":{"type":"string"}},"required":["repository","sha256","reason"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSignatureTrustRootFailed, Category: CategorySystem, Mode: Async,
		Schema: `{"type":"object","properties":{"code":{"type":"string"},"detail":{"type":"string"}},` +
			`"required":["code","detail"],"additionalProperties":false}`,
	})
}
