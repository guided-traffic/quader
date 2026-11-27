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
//   - system.storage.keys_exported: the export printed the rows of one connection or of all, and names their identifiers (R-STOR-103);
//     written before the document leaves.
//   - system.storage.keys_imported: an import inserted the missing rows and kept the equal ones; it names the keys it skipped because
//     the database holds their connection destroyed, and the destroyed keys of the file it left because the database holds them sealed.
//   - system.storage.key_generation_added: a new generation of a connection's storage key became active and the previous retired.
//   - system.storage.keys_destroyed: `quader admin storage keys destroy` destroyed every key of a retired connection (R-STOR-099);
//     journaled with the entry {connection, keyIds, destroyedAt}, so that a restore to before it destroys the keys again (R-HA-054,
//     Q-HA-416). Its data report the exports, the backup listing and the retained backups that may still open what the provider keeps.

// Names of the connection events, for the producers.
const (
	EventStorageConnectionAdded     = "system.storage.connection_added"
	EventStorageConnectionForgotten = "system.storage.connection_forgotten"
	EventStorageKeysExported        = "system.storage.keys_exported"
	EventStorageKeysImported        = "system.storage.keys_imported"
	EventStorageKeyGenerationAdded  = "system.storage.key_generation_added"
	EventStorageKeysDestroyed       = "system.storage.keys_destroyed"
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
			`"rootKeyIds":{"type":"array","maxItems":64,"items":{"type":"string","maxLength":64}},` +
			`"keyIds":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":64}}},` +
			`"required":["connection","keys","rootKeyIds","keyIds"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventStorageKeysImported, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"connections":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":32}},` +
			`"inserted":{"type":"integer"},"kept":{"type":"integer"},` +
			`"destroyed":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":64}},` +
			`"stillSealed":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":64}},` +
			`"sourceInstallation":{"type":"string","maxLength":64},"otherInstallation":{"type":"boolean"}},` +
			`"required":["connections","inserted","kept","destroyed","stillSealed","sourceInstallation","otherInstallation"],"additionalProperties":false}`,
	})
	registerKeysDestroyed()
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
	registerBlobRechecked()
	registerFindingAcknowledged()
}

// >>> finding acknowledged

// EventStorageFindingAcknowledged: an administrator acknowledged an unresolved finding of a damaged copy (POST
// /api/v1/storage/findings/{id}/acknowledge, storage.md R-STOR-111, Q-OPS-472): the data name the finding's id, its connection, its
// blob's digest and the note (at most 500 characters, empty without one). Transactional with the acknowledgement; not journaled, since
// an acknowledgement resolves, writes and grants nothing (Q-STOR-530).
const EventStorageFindingAcknowledged = "storage.finding.acknowledged"

func registerFindingAcknowledged() {
	Register(EventType{
		Name: EventStorageFindingAcknowledged, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"finding":{"type":"integer","minimum":1},"connection":{"type":"string","maxLength":32},` +
			`"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},"note":{"type":"string","maxLength":500}},` +
			`"required":["finding","connection","sha256","note"],"additionalProperties":false}`,
	})
}

// <<< finding acknowledged

// >>> blob rechecked

// EventStorageBlobRechecked: an administrator had every copy of a blob read whole now (POST
// /api/v1/storage/damaged-blobs/{sha256}/recheck, storage.md R-STOR-027): the data name the digest, the content state the copies give
// after the reads and each copy's connection and state. Transactional, written before the answer (Q-API-477); not journaled, since a
// recheck writes no object and grants nothing.
const EventStorageBlobRechecked = "storage.blob.rechecked"

func registerBlobRechecked() {
	Register(EventType{
		Name: EventStorageBlobRechecked, Category: CategorySystem, Mode: Transactional,
		Schema: `{"type":"object","properties":{"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"contentStatus":{"enum":["ok","missing","corrupt"]},` +
			`"copies":{"type":"array","maxItems":64,"items":{"type":"object","properties":{"connection":{"type":"string","maxLength":32},` +
			`"state":{"enum":["present","missing","corrupt","deleting"]}},"required":["connection","state"],"additionalProperties":false}}},` +
			`"required":["sha256","contentStatus","copies"],"additionalProperties":false}`,
	})
}

// <<< blob rechecked

// >>> storage keys destroyed

// registerKeysDestroyed registers system.storage.keys_destroyed (storage.md R-STOR-099, ha-and-helm.md R-HA-054): transactional,
// journaled with a required entry. Its data name the connection and the identifiers destroyed and report what may still open the
// provider's leftovers: the exports made before (count, the last one's time), the retention listing the backups' check read (or null
// when no backup is recorded) and how many retained backups hold the keys sealed (null when the listing does not say).
func registerKeysDestroyed() {
	ids := `{"type":"array","minItems":1,"maxItems":1000,"items":{"type":"string","maxLength":64}}`
	Register(EventType{
		Name: EventStorageKeysDestroyed, Category: CategorySystem, Mode: Transactional, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{"connection":{"type":"string","maxLength":32},"keyIds":` + ids + `,` +
			`"exports":{"type":"object","properties":{"count":{"type":"integer"},"lastAt":{"type":["string","null"]}},` +
			`"required":["count","lastAt"],"additionalProperties":false},` +
			`"backupListing":{"type":["object","null"],"properties":{"runId":{"type":"string","maxLength":64},` +
			`"listedAt":{"type":["string","null"]},"target":{"type":"string","maxLength":1024}},` +
			`"required":["runId","listedAt","target"],"additionalProperties":false},` +
			`"retainedHoldingSealed":{"type":["integer","null"]}},` +
			`"required":["connection","keyIds","exports","backupListing","retainedHoldingSealed"],"additionalProperties":false}`,
	})
}

// <<< storage keys destroyed

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
