package audit

// The event types of the curation of a remote repository (remote-curation.md R-CUR-019). Every event names the repository by its ID and
// qualified name, as every event of a repository does; the actor is the person, or a system subject for a task
// (system:examination-cleanup for the retention of R-CUR-016).
//
// Only a discard of what was fetched for examination is journaled (R-CUR-016): a restore removes it again. A narrowing of the rules or
// of the mode is recorded as repository.narrowed beside its own event (R-CUR-006); the rules' events carry no journal flag of their own.
const (
	// EventRemoteRuleCreated: a delivery rule was added to a remote repository (R-CUR-006).
	EventRemoteRuleCreated = "remote.rule.created"
	// EventRemoteRuleUpdated: a write of a subject that had a rule replaced its effect or note.
	EventRemoteRuleUpdated = "remote.rule.updated"
	// EventRemoteRuleDeleted: a rule was removed.
	EventRemoteRuleDeleted = "remote.rule.deleted"
	// EventRemoteDeliveryRefused: the rules refused a client's request, at most once a minute per repository, package, version and
	// client address on each replica (R-CUR-004).
	EventRemoteDeliveryRefused = "remote.delivery.refused"
	// EventRemoteRequestCreated: a person asked for an allow and opened a request entry (R-CUR-015).
	EventRemoteRequestCreated = "remote.request.created"
	// EventRemoteRequestAllowed: a request entry was allowed; the rule it wrote is recorded by its own event.
	EventRemoteRequestAllowed = "remote.request.allowed"
	// EventRemoteRequestDeclined: a request entry was declined with a reason.
	EventRemoteRequestDeclined = "remote.request.declined"
	// EventRemoteRequestWithdrawn: an asker left a request entry.
	EventRemoteRequestWithdrawn = "remote.request.withdrawn"
	// EventRemoteDownloadStarted: a person started a download ahead of a version, a package or a sub-section (R-CUR-010).
	EventRemoteDownloadStarted = "remote.download.started"
	// EventRemoteDownloadRetried: a failed or cancelled row of the queue, or every failed row that matches, waits again (R-CUR-012).
	EventRemoteDownloadRetried = "remote.download.retried"
	// EventRemoteDownloadCancelled: a row of the queue or a bulk was cancelled (R-CUR-012).
	EventRemoteDownloadCancelled = "remote.download.cancelled"
	// EventRemoteExaminationDiscarded: what a download ahead stored for examination was removed, by a person, by a decline or by the
	// retention task (R-CUR-016). Journaled: the replay evicts what is stored of the subject and is still not delivered.
	EventRemoteExaminationDiscarded = "remote.examination.discarded"
)

func init() {
	const (
		name    = `"name":{"type":"string","minLength":1,"maxLength":1024}`
		version = `"version":{"type":["string","null"],"maxLength":256}`
		subject = `"subject":{"type":"string","minLength":1,"maxLength":1024}`
		reqID   = `"requestId":{"type":["string","null"]}`
	)
	effect := `{"type":"string","enum":["allow","block"]}`
	rule := `{"type":"object","properties":{"ruleId":{"type":"string"},"kind":{"type":"string","enum":["section","package","version"]},` +
		subject + `,"effect":` + effect + `,"previousEffect":{"anyOf":[` + effect + `,{"type":"null"}]},` +
		`"note":{"type":"string","maxLength":1000},` + reqID + `},` +
		`"required":["ruleId","kind","subject","effect"],"additionalProperties":false}`
	for _, n := range []string{EventRemoteRuleCreated, EventRemoteRuleUpdated, EventRemoteRuleDeleted} {
		Register(EventType{Name: n, Category: CategoryRepository, Mode: Transactional, Webhook: true, Schema: rule})
	}
	Register(EventType{
		Name: EventRemoteDeliveryRefused, Category: CategoryContent, Mode: Async,
		Schema: `{"type":"object","properties":{"path":{"type":"string"},"package":{"type":"string"},` + version + `,` +
			`"reason":{"type":"string","enum":["blocked","not_allowed"]},"kind":{"type":"string","enum":["document","artifact"]}},` +
			`"required":["path","package","version","reason","kind"],"additionalProperties":false}`,
	})
	request := `{"type":"object","properties":{` + reqID + `,` + name + `,` + version + `,"reason":{"type":"string","maxLength":1000}},` +
		`"required":["requestId","name","version"],"additionalProperties":false}`
	for _, n := range []string{EventRemoteRequestCreated, EventRemoteRequestAllowed, EventRemoteRequestDeclined, EventRemoteRequestWithdrawn} {
		Register(EventType{Name: n, Category: CategoryRepository, Mode: Transactional, Webhook: true, Schema: request})
	}
	download := `{"type":"object","properties":{"level":{"type":"string","enum":["version","package","section"]},` + subject + `,` +
		version + `,"bounds":{"type":"object"},"bulk":{"type":["string","null"]},"row":{"type":["string","null"]},` +
		`"count":{"type":"integer","minimum":0}},"additionalProperties":false}`
	for _, n := range []string{EventRemoteDownloadStarted, EventRemoteDownloadRetried, EventRemoteDownloadCancelled} {
		Register(EventType{Name: n, Category: CategoryRepository, Mode: Transactional, Schema: download})
	}
	Register(EventType{
		Name: EventRemoteExaminationDiscarded, Category: CategoryContent, Mode: Transactional, Journal: JournalRevocation,
		Schema: `{"type":"object","properties":{` + name + `,` + version + `,"files":{"type":"integer","minimum":0},` +
			`"cause":{"type":"string","enum":["person","retention","declined"]}},` +
			`"required":["name","version","files","cause"],"additionalProperties":false}`,
	})
}
