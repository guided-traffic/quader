package audit

// The trace of a data key handed to a Bastion (storage.md R-STOR-096, Q-STOR-344; bastion.md R-BAS-070): for every handover of a
// copy on an encrypted connection, the main installation writes bastion.data_key.handed in a transaction of its own before the
// answer that carries the key leaves (internal/downloadlinks, handover.go), whatever audit.record_downloads says; a failed insert
// streams the download instead of handing the key. The trail therefore names every data key a Bastion has received, which bounds
// what a taken-over Bastion holds (security/bastion-handover.md H-1340) and what the destruction of a connection's keys leaves
// readable (R-STOR-098). It is not journaled: it records what left, and a restore cannot take a key back.
//
// The actor is the forward's caller (anonymous without a credential), the source the caller's address (bastion.md R-BAS-017), the
// resource the registration (bastion, its principal ID), the repository, project and path the item's. The details name the
// registration (bastion), the Bastion replica that forwarded (replica, Quader-Bastion-Replica, empty when it sent none), the blob's
// digest, the connection, the storage key that wrapped the data key (storageKeyId) and the link's expiry; never the key, the wrap
// or the link.

// EventBastionDataKeyHanded is the trace of one data key handed to a Bastion.
const EventBastionDataKeyHanded = "bastion.data_key.handed"

func init() {
	Register(EventType{
		Name: EventBastionDataKeyHanded, Category: CategoryContent, Mode: Transactional,
		Schema: `{"type":"object","properties":{"bastion":{"type":"string","maxLength":128},"replica":{"type":"string","maxLength":253},` +
			`"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},"connection":{"type":"string","maxLength":32},` +
			`"storageKeyId":{"type":"string","maxLength":64},"linkExpiresAt":{"type":"string"}},` +
			`"required":["bastion","replica","sha256","connection","storageKeyId","linkExpiresAt"],"additionalProperties":false}`,
	})
}
