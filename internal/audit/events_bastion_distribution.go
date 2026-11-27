package audit

// What a Bastion may distribute (bastion.md R-BAS-134, Q-BAS-521): an administrator allows a project for a Bastion
// (bastion.project.allowed) or takes it off the list (bastion.project.removed, which takes its repositories' choices for that Bastion
// with it); a project's Owner adds a Bastion to a repository (repository.bastion.added) or takes it off (repository.bastion.removed).
// The two removals narrow what leaves the main installation, so both are journaled: the removal's entry is {bastionId, projectId}, a
// choice's {repositoryId, bastionId}, and the replay deletes the rows again after a restore to a point before them
// (internal/bastionmain/distribution_replay.go). The two widenings carry no entry. The feed maps all four (R-BAS-015).

// Names of the distribution events, for the producers.
const (
	EventBastionProjectAllowed    = "bastion.project.allowed"
	EventBastionProjectRemoved    = "bastion.project.removed"
	EventRepositoryBastionAdded   = "repository.bastion.added"
	EventRepositoryBastionRemoved = "repository.bastion.removed"
)

func init() {
	project := `{"type":"object","required":["project"],"properties":{"project":{"type":"string"},"projectId":{"type":"string"},` +
		`"repositories":{"type":"array","maxItems":1000,"items":{"type":"string"}}}}`
	Register(EventType{Name: EventBastionProjectAllowed, Category: CategoryIdentity, Mode: Transactional, Webhook: true, Schema: project})
	Register(EventType{
		Name: EventBastionProjectRemoved, Category: CategoryIdentity, Mode: Transactional, Webhook: true, Journal: JournalRevocation, Schema: project,
	})
	choice := `{"type":"object","required":["bastion"],"properties":{"bastion":{"type":"string"},"bastionId":{"type":"string"},` +
		`"cause":{"type":"string","enum":["owner","move"]}}}`
	Register(EventType{Name: EventRepositoryBastionAdded, Category: CategoryRepository, Mode: Transactional, Webhook: true, Schema: choice})
	Register(EventType{
		Name: EventRepositoryBastionRemoved, Category: CategoryRepository, Mode: Transactional, Webhook: true, Journal: JournalRevocation, Schema: choice,
	})
}
