package audit

// The artifact events of the files transfer (R-OPS-016, R-STOR-005, R-STOR-018).
// artifact.deployed and artifact.deleted are transactional: the producer records
// them in the transaction of the change, one per file. Neither carries the
// revocation flag (R-OPS-014 lists none for them). artifact.downloaded is
// asynchronous and exists only with audit.record_downloads (R-OPS-015); the
// request log holds every download regardless.
//
// The repository the event concerns is Event.Repository and its path is
// Event.Path (the columns of the same names); the data below repeats the path,
// because a webhook delivery carries the data and not the columns.

// Names of the artifact events, for the producers. artifact.delete_stopped is asynchronous: the delete job records it when its guard
// stopped it (a refused item, a withdrawn principal or credential), with the subject that asked as the actor (R-STOR-018, Q-STOR-193).
const (
	EventArtifactDeployed      = "artifact.deployed"
	EventArtifactDeleted       = "artifact.deleted"
	EventArtifactDownloaded    = "artifact.downloaded"
	EventArtifactDeleteStopped = "artifact.delete_stopped"
)

func init() {
	Register(EventType{
		Name: EventArtifactDeployed, Category: CategoryContent, Mode: Transactional, Webhook: true,
		Schema: `{"type":"object","properties":{` +
			`"path":{"type":"string"},"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"size":{"type":"integer","minimum":0},"overwritten":{"type":"boolean"},` +
			`"package":{"type":"string"},"version":{"type":"string"},"unit":{"type":"string","maxLength":512}},` +
			`"required":["path","sha256","size","overwritten"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventArtifactDeleted, Category: CategoryContent, Mode: Transactional, Webhook: true,
		Schema: `{"type":"object","properties":{` +
			`"path":{"type":"string"},"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"size":{"type":"integer","minimum":0},` +
			`"package":{"type":"string"},"version":{"type":"string"},"unit":{"type":"string","maxLength":512}},` +
			`"required":["path","sha256","size"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventArtifactDeleteStopped, Category: CategoryContent, Mode: Async,
		Schema: `{"type":"object","properties":{` +
			`"path":{"type":"string"},"reason":{"type":"string"},"job":{"type":"string"},"removed":{"type":"integer","minimum":0}},` +
			`"required":["path","reason","job","removed"],"additionalProperties":false}`,
	})
	Register(EventType{
		Name: EventArtifactDownloaded, Category: CategoryContent, Mode: Async,
		// origin, bastion and replica are set for a download a Bastion served from its cache and reported
		// through the interface (R-BAS-016); a download the main installation served has none of them. delivery and linkExpiresAt
		// are set for a download handed to a signed link of the object store (storage.md R-STOR-073, Q-STOR-292); a download handed
		// over to a Bastion has delivery handover, bastion (the registration's name), connection and encrypted besides
		// (storage.md R-STOR-095), and no origin. size, package, version and unit are written from 2026-11-26 on
		// (operations-and-observability.md R-OPS-054): the item's size, the package and the version the format classifies it in (or
		// the tag its manifest was fetched by), and the unit whose events the audit log shows as one row (R-OPS-055); an item of no
		// package names none of the three, and a format whose unit is a file names no unit.
		Schema: `{"type":"object","properties":{` +
			`"path":{"type":"string"},"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},` +
			`"size":{"type":"integer","minimum":0},"package":{"type":"string"},"version":{"type":"string"},"unit":{"type":"string"},` +
			`"origin":{"type":"string","enum":["bastion"]},"bastion":{"type":"string"},"replica":{"type":"string"},` +
			`"delivery":{"type":"string","enum":["link","handover"]},"linkExpiresAt":{"type":"string"},` +
			`"connection":{"type":"string"},"encrypted":{"type":"boolean"}},` +
			`"required":["path","sha256"],"additionalProperties":false}`,
	})
}
