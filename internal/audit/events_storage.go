package audit

// The event types of the object-store connections (storage.md R-STOR-051), all transactional and none a webhook hook. No
// detail holds a credential, an endpoint or a bucket's address.
//
//   - system.storage.connection_added: a replica saw a connection for the first time (actor system:replica); the data name
//     the connection.
//   - system.storage.connection_forgotten: `quader admin storage forget-connection` gave a connection up (actor
//     system:admin); the data name it and count the copy and queue rows it deleted and the blobs that had no present copy
//     elsewhere.
//
// system.storage.connection_drained is the reconciliation's (R-STOR-056) and is registered with it.

// Names of the connection events, for the producers.
const (
	EventStorageConnectionAdded     = "system.storage.connection_added"
	EventStorageConnectionForgotten = "system.storage.connection_forgotten"
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
}
