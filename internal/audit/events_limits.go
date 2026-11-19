package audit

// The event types of the download limits (usage-and-limits.md R-LIM-030). Every change of a profile, a rule, a remote's
// ceilings, a project's maximum or the settings is transactional; a block that a flush sets is recorded asynchronously and
// never fails a request. They are configuration events: a limit never grants, so none carries a journal entry.
const (
	EventLimitsProfileCreated        = "limits.profile.created"
	EventLimitsProfileUpdated        = "limits.profile.updated"
	EventLimitsProfileDeleted        = "limits.profile.deleted"
	EventLimitsAssignmentCreated     = "limits.assignment.created"
	EventLimitsAssignmentUpdated     = "limits.assignment.updated"
	EventLimitsAssignmentDeleted     = "limits.assignment.deleted"
	EventLimitsCeilingChanged        = "limits.ceiling.changed"
	EventLimitsProjectMaximumChanged = "limits.project_maximum.changed"
	EventLimitsModeChanged           = "limits.mode.changed"
	EventLimitsSettingsChanged       = "limits.settings.changed"
	EventLimitsBlockStarted          = "limits.block.started"
	EventLimitsBlockLifted           = "limits.block.lifted"
)

func init() {
	profile := `{"type":"object","properties":{"key":{"type":"string"},"changes":{"type":["object","null"]}},"required":["key"]}`
	Register(EventType{Name: EventLimitsProfileCreated, Category: CategoryConfig, Mode: Transactional, Schema: profile})
	Register(EventType{Name: EventLimitsProfileUpdated, Category: CategoryConfig, Mode: Transactional, Schema: profile})
	Register(EventType{Name: EventLimitsProfileDeleted, Category: CategoryConfig, Mode: Transactional, Schema: profile})
	assignment := `{"type":"object","properties":{"id":{"type":"string"},"place":{"type":"string"},"who":{"type":"object"},` +
		`"profile":{"type":"string"},"overrides":{"type":["object","null"]},"capped":{"type":"boolean"},"reason":{"type":"string"}},` +
		`"required":["place","who"]}`
	Register(EventType{Name: EventLimitsAssignmentCreated, Category: CategoryConfig, Mode: Transactional, Schema: assignment})
	Register(EventType{Name: EventLimitsAssignmentUpdated, Category: CategoryConfig, Mode: Transactional, Schema: assignment})
	Register(EventType{Name: EventLimitsAssignmentDeleted, Category: CategoryConfig, Mode: Transactional, Schema: assignment})
	change := `{"type":"object","properties":{"repository":{"type":"string"},"project":{"type":"string"},` +
		`"before":{"type":["object","string","null"]},"after":{"type":["object","string","null"]}}}`
	Register(EventType{Name: EventLimitsCeilingChanged, Category: CategoryConfig, Mode: Transactional, Schema: change})
	Register(EventType{Name: EventLimitsProjectMaximumChanged, Category: CategoryConfig, Mode: Transactional, Schema: change})
	Register(EventType{Name: EventLimitsModeChanged, Category: CategoryConfig, Mode: Transactional, Schema: change})
	Register(EventType{Name: EventLimitsSettingsChanged, Category: CategoryConfig, Mode: Transactional, Schema: change})
	block := `{"type":"object","properties":{"block":{"type":"string"},"subject":{"type":"string"},"place":{"type":"string"},` +
		`"until":{"type":"string"},"refused":{"type":"integer"},"limit":{"type":"integer"}},"required":["subject","place"]}`
	Register(EventType{Name: EventLimitsBlockStarted, Category: CategoryConfig, Mode: Async, Schema: block})
	Register(EventType{Name: EventLimitsBlockLifted, Category: CategoryConfig, Mode: Transactional, Schema: block})
}
