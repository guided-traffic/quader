package audit

// The events of the Ansible Galaxy format (R-ANS-030). package.published and artifact.deployed are recorded for every format; these are
// the format's own. Two are journaled (R-HA-054, R-ANS-029, Q-ANS-9): the deletion of a version and the clearing of a tombstone. A
// restore returns the repository to its snapshot, and the replay of these entries only restricts: a deleted version number is
// withheld again, a cleared tombstone is cleared again, and the replay never deletes content. The producers record each event in the
// transaction of the change.
//
// ansible.import_task.failed is about a publish that stored nothing; repeated failures of one principal are a security-relevant sign
// (an archive probing the reader), so it is an event of its own, written through the asynchronous buffer like the other events of a
// refused request. ansible.hub.exchange_failed carries no token and no part of one (R-ANS-019).
//
// ansible.signature.added and ansible.signature.removed are the posting and the removal of a detached signature of a version through
// the management API (signatures.md R-SIG-031, Q-SIG-107). They carry the key's fingerprint, never the signature; they are not
// journaled, since a restore returns the signature rows with their snapshot.

// Names of the Ansible events, for the producers.
const (
	EventAnsibleVersionDeleted    = "ansible.version.deleted"
	EventAnsibleTombstoneCleared  = "ansible.tombstone.cleared"
	EventAnsibleImportTaskFailed  = "ansible.import_task.failed"
	EventAnsibleHubExchangeFailed = "ansible.hub.exchange_failed"
	EventAnsibleSignatureAdded    = "ansible.signature.added"
	EventAnsibleSignatureRemoved  = "ansible.signature.removed"
)

const (
	ansibleName        = `"name":{"type":"string","maxLength":129}`
	ansibleVersion     = `"version":{"type":"string","maxLength":128}`
	ansibleSHA256      = `"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"}`
	ansibleFingerprint = `"fingerprint":{"type":"string","pattern":"^[0-9a-f]{40}$|^[0-9a-f]{64}$"}`
)

func init() {
	// A version of a local repository was deleted by a person or a job: its number is withheld (R-ANS-012).
	Register(EventType{
		Name: EventAnsibleVersionDeleted, Category: CategoryContent, Mode: Transactional, Webhook: true, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{` + ansibleName + `,` + ansibleVersion + `,"stored":{"type":"string","maxLength":128},` + ansibleSHA256 + `},` +
			`"required":["name","version","sha256"],"additionalProperties":false}`,
	})
	// A tombstone was cleared by an administrator, so the number can be published with other bytes. Journaled so that a restore from
	// an older backup, which still holds the tombstone, does not bring a cleared number back as withheld (R-ANS-029).
	Register(EventType{
		Name: EventAnsibleTombstoneCleared, Category: CategoryContent, Mode: Transactional, Webhook: true, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{` + ansibleName + `,` + ansibleVersion + `},` +
			`"required":["name","version"],"additionalProperties":false}`,
	})
	// A publish got as far as the inspection of the archive and the archive was refused (R-ANS-009).
	Register(EventType{
		Name: EventAnsibleImportTaskFailed, Category: CategoryContent, Mode: Async,
		Schema: `{"type":"object","properties":{"code":{"type":"string","maxLength":64},` +
			`"reason":{"type":"string","maxLength":64},"name":{"type":"string","maxLength":129},"version":{"type":"string","maxLength":128}},` +
			`"required":["code"],"additionalProperties":false}`,
	})
	// The exchange of the offline token of a hub for an access token failed (R-ANS-019).
	Register(EventType{
		Name: EventAnsibleHubExchangeFailed, Category: CategoryRepository, Mode: Async,
		Schema: `{"type":"object","properties":{"reason":{"type":"string","maxLength":128},"status":{"type":"integer"}},` +
			`"required":["reason"],"additionalProperties":false}`,
	})
	// A client posted a detached signature for a version of a local repository; replaced is true when it took the place of the
	// signature of the same key (R-SIG-031).
	Register(EventType{
		Name: EventAnsibleSignatureAdded, Category: CategoryContent, Mode: Transactional, Webhook: true,
		Schema: `{"type":"object","properties":{` + ansibleName + `,` + ansibleVersion + `,` + ansibleFingerprint + `,"replaced":{"type":"boolean"}},` +
			`"required":["name","version","fingerprint"],"additionalProperties":false}`,
	})
	// The signature of a key was removed from a version of a local repository (R-SIG-031).
	Register(EventType{
		Name: EventAnsibleSignatureRemoved, Category: CategoryContent, Mode: Transactional, Webhook: true,
		Schema: `{"type":"object","properties":{` + ansibleName + `,` + ansibleVersion + `,` + ansibleFingerprint + `},` +
			`"required":["name","version","fingerprint"],"additionalProperties":false}`,
	})
}
