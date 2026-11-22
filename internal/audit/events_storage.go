package audit

// The event types of the object-store connections (storage.md R-STOR-051) and of the stored content (R-STOR-027), none a webhook
// hook. No detail holds a credential, an endpoint or a bucket's address.
//
//   - system.storage.connection_added: a replica saw a connection for the first time (actor system:replica); the data name
//     the connection.
//   - system.storage.connection_forgotten: `quader admin storage forget-connection` gave a connection up (actor
//     system:admin); the data name it and count the copy and queue rows it deleted and the blobs that had no present copy
//     elsewhere.
//   - system.storage.connection_drained: the reconciliation found a draining connection without a copy (R-STOR-056; actor
//     system:worker); the data name it and count the copies the drain deleted.
//   - system.storage.blob_damaged: a read, the scrub, its recheck or the reconciliation found that no connection holds a blob
//     whole any more, and the blob's content state left ok (R-STOR-027, R-STOR-052; actor system:replica, outcome warning);
//     the data name the digest and the state. Asynchronous: the store's damage hook runs after the mark's transaction
//     committed. A copy's damage that leaves the blob ok is a metric and a log line, not an event.
//
// The events of the storage keys (R-STOR-083, R-STOR-085), actor system:admin, written by `quader admin storage keys` in the
// transaction that reads or writes the rows; their data name identifiers and counts, never a key or a sealed value:
//
//   - system.storage.keys_exported: the export printed the rows of one connection or of all; written before the document leaves.
//   - system.storage.keys_imported: an import inserted the missing rows and kept the equal ones.
//   - system.storage.key_generation_added: a new generation of a connection's storage key became active and the previous retired.

// Names of the connection events, for the producers.
const (
	EventStorageConnectionAdded     = "system.storage.connection_added"
	EventStorageConnectionForgotten = "system.storage.connection_forgotten"
	EventStorageKeysExported        = "system.storage.keys_exported"
	EventStorageKeysImported        = "system.storage.keys_imported"
	EventStorageKeyGenerationAdded  = "system.storage.key_generation_added"
	EventStorageConnectionDrained   = "system.storage.connection_drained"
	EventStorageBlobDamaged         = "system.storage.blob_damaged"
)

func init() {
	Register(EventType{
		Name: EventStorageConnectionAdded, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"connection":{"type":"string","maxLength":32}},"required":["connection"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventStorageConnectionForgotten, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"connection":{"type":"string","maxLength":32},"copies":{"type":"integer"},` +
			`"queued":{"type":"integer"},"blobsMissing":{"type":"integer"}},` +
			`"required":["connection","copies","queued","blobsMissing"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventStorageKeysExported, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"connection":{"type":"string","maxLength":32},"keys":{"type":"integer"},` +
			`"rootKeyIds":{"type":"array","maxItems":64,"items":{"type":"string","maxLength":64}}},` +
			`"required":["connection","keys","rootKeyIds"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventStorageKeysImported, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"connections":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":32}},` +
			`"inserted":{"type":"integer"},"kept":{"type":"integer"},"sourceInstallation":{"type":"string","maxLength":64},` +
			`"otherInstallation":{"type":"boolean"}},` +
			`"required":["connections","inserted","kept","sourceInstallation","otherInstallation"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventStorageKeyGenerationAdded, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"connection":{"type":"string","maxLength":32},"keyId":{"type":"string","maxLength":64},` +
			`"generation":{"type":"integer"},"retiredKeyId":{"type":"string","maxLength":64}},` +
			`"required":["connection","keyId","generation","retiredKeyId"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventStorageConnectionDrained, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"connection":{"type":"string","maxLength":32},"copiesDeleted":{"type":"integer"}},` +
			`"required":["connection","copiesDeleted"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventStorageBlobDamaged, Category: CategorySystem, Mode: Async,
		Schema: `{"type":"object","properties":{"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"status":{"enum":["missing","corrupt"]}},"required":["sha256","status"],"additionalProperties":false}`,
	})
	registerDownloadLinks()
}

// >>> download links

// EventStorageDownloadLinksChanged: the installation's switch of the delivery by signed links of the object store or its details
// changed (storage.md R-STOR-014): before and after hold enabled, minSize, linkLifeSeconds and publicAddresses. Journaled when the
// write switched the delivery off, so that a restore to an earlier point keeps links off; switching on and the details widen and
// are not journaled (an optional entry, as malware.settings.changed).
const EventStorageDownloadLinksChanged = "storage.download_links.changed"

func registerDownloadLinks() {
	state := `{"type":"object","properties":{"enabled":{"type":"boolean"},"minSize":{"type":"integer"},"linkLifeSeconds":{"type":"integer"},` +
		`"publicAddresses":{"type":"object"}},"required":["enabled","minSize","linkLifeSeconds","publicAddresses"],"additionalProperties":false}`
	Register(EventType{
		Name: EventStorageDownloadLinksChanged, Category: CategoryConfig, Mode: Transactional, Journal: JournalRevocation, Entry: EntryOptional,
		Schema: `{"type":"object","properties":{"before":` + state + `,"after":` + state + `},"required":["before","after"],"additionalProperties":false}`,
	})
}

// <<< download links
