package audit

// The event types of Quader's signing identity (signatures.md R-SIG-057). What withdraws an identity after a restore point is journaled,
// so that a restore re-applies it (R-SIG-059): a removal and a retirement. An identity installed after the restore point is lost with its
// rows, which narrows. No event carries key material.
//
// The actor of the job's and the files' events is system:signing.
const (
	// EventSigningKeyCreated: a pending key was made for a key request (R-SIG-047).
	EventSigningKeyCreated = "signing.identity.key_created"
	// EventSigningInstalled: an identity was announced or became current from an upload, an import or the files (R-SIG-046, R-SIG-047).
	EventSigningInstalled = "signing.identity.installed"
	// EventSigningSwitched: an announced identity became current (R-SIG-048).
	EventSigningSwitched = "signing.identity.switched"
	// EventSigningRemoved: the current identity was removed. Journaled: the replay makes it previous again.
	EventSigningRemoved = "signing.identity.removed"
	// EventSigningRetired: a previous identity was retired. Journaled: the replay retires it again.
	EventSigningRetired = "signing.identity.retired"
	// EventSigningRefused: a pair of the files failed R-SIG-045, once per content (R-SIG-046).
	EventSigningRefused = "signing.identity.refused"
	// EventSigningFailed: work reached its last attempt, at most once an hour per kind and code (R-SIG-049).
	EventSigningFailed = "signing.failed"
)

func init() {
	sha := `{"type":"string","pattern":"^[0-9a-f]{64}$"}`
	id := `"id":{"type":"integer","minimum":1}`
	Register(EventType{
		Name: EventSigningKeyCreated, Category: CategoryConfig, Mode: Transactional,
		Schema: `{"type":"object","properties":{` + id + `,"subject":{"type":"string"},` +
			`"names":{"type":"array","maxItems":64,"items":{"type":"string"}},"keySha256":` + sha + `},` +
			`"required":["id","subject","names","keySha256"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSigningInstalled, Category: CategoryConfig, Mode: Transactional,
		Schema: `{"type":"object","properties":{` + id + `,"source":{"type":"string","enum":["configuration","upload"]},` +
			`"state":{"type":"string","enum":["announced","current"]},"subject":{"type":"string"},"issuer":{"type":"string"},` +
			`"notAfter":{"type":"string","format":"date-time"},"fingerprint":` + sha + `,"npmKeyId":{"type":"string"}},` +
			`"required":["id","source","state","subject","issuer","notAfter","fingerprint","npmKeyId"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSigningSwitched, Category: CategoryConfig, Mode: Transactional,
		Schema: `{"type":"object","properties":{` + id + `,"previous":{"type":"integer","minimum":0}},` +
			`"required":["id","previous"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSigningRemoved, Category: CategoryConfig, Mode: Transactional, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{` + id + `,"fingerprint":` + sha + `},"required":["id","fingerprint"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSigningRetired, Category: CategoryConfig, Mode: Transactional, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{` + id + `,"fingerprint":` + sha + `,"signatures":{"type":"integer","minimum":0}},` +
			`"required":["id","fingerprint","signatures"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSigningRefused, Category: CategoryConfig, Mode: Async,
		Schema: `{"type":"object","properties":{"reason":{"type":"string"},"check":{"type":"string"},"fingerprint":{"type":"string"}},` +
			`"required":["reason","check","fingerprint"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventSigningFailed, Category: CategorySystem, Mode: Async,
		Schema: `{"type":"object","properties":{"kind":{"type":"string","enum":["npm","attestation"]},"code":{"type":"string"},` +
			`"count":{"type":"integer","minimum":1}},"required":["kind","code","count"],"additionalProperties":false}`,
	})
}
