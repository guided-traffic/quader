package audit

// The events of the Bastion registrations on the main installation (R-BAS-004). A registration is a principal
// of the kind bastion; its events are about the registration, so their resource type is bastion, and the token
// events of a Bastion token (token.created, token.revoked, registered with the identity events) carry the same
// resource type. A disable and a deletion end what the Bastion may do, so both are journaled (R-HA-054, Q-BAS-145): a
// disable is a member of the registration's configuration and revokes nothing (R-BAS-133), and without its entry a restore to
// a point before it would let the Bastion work again. Their entry is the registration's principal ID, its name and the way
// of the disable, soft or hard (hard for a deletion); the replay disables the registration that way, never more lightly than
// it is. A deletion also journals each token it revokes (token.revoked). bastion.disabled carries the way in its details as
// well. bastion.updated (the description) and bastion.enabled narrow nothing and carry no entry; the feed maps bastion.disabled
// and bastion.enabled to a settings entry, so every Bastion handshakes again within one poll.
//
// The switch of the handover per blob (R-BAS-071, Q-BAS-232): bastion.handover.enabled and bastion.handover.disabled, about
// the registration as well. Switching it off narrows what leaves the main installation, so it is journaled with the entry
// {principalId, name}, and the replay switches the member off (internal/bastionmain/handover.go); switching it on widens and
// carries no entry.

// The configuration of a Bastion (R-BAS-093, Q-BAS-320): bastion.configuration.changed is written in the transaction of every change of
// what the handshake's member configuration carries - a switch, the mode, the preset, the flood figures, the slots, the throughput, the
// anonymous ceiling, a rule of the Bastion's place or the figures of a profile such a rule names - with the parts changed and, for the
// registration's own members, their values before and after. The feed maps it to a settings entry, so every Bastion handshakes again
// within one poll (R-BAS-015). It widens or narrows nothing a Bastion keeps of a caller's answers and carries no journal entry; a
// switch-off of the handover keeps its own journaled event beside it.

// A registration the installation's configuration declares (R-BAS-127): quader migrate records bastion.created with the source
// configuration, and bastion.updated with the reason configuration for the mark (field declared), the description, the networks and a
// token issued on the screen that became the declared one (field token, with tokenId), or with the reason configuration_removed when
// the configuration no longer declares it. The declared token's own events are token.created (createdVia configuration) and
// token.revoked (reason rotated, or admin on a removed declaration), as every Bastion token's.

// Names of the Bastion registration events, for the producers.
const (
	EventBastionCreated  = "bastion.created"
	EventBastionUpdated  = "bastion.updated"
	EventBastionDisabled = "bastion.disabled"
	EventBastionEnabled  = "bastion.enabled"
	EventBastionDeleted  = "bastion.deleted"

	EventBastionHandoverEnabled  = "bastion.handover.enabled"
	EventBastionHandoverDisabled = "bastion.handover.disabled"

	EventBastionConfigurationChanged = "bastion.configuration.changed"
)

func init() {
	bastion := func(name string, journal Journal, schema string) {
		Register(EventType{Name: name, Category: CategoryIdentity, Mode: Transactional, Webhook: true, Journal: journal, Schema: schema})
	}
	bastion(EventBastionCreated, NoJournal, `{"type":"object","properties":{"source":{"type":"string","enum":["api","admin-cli","configuration"]},"description":{"type":"string"}}}`)
	bastion(EventBastionUpdated, NoJournal, `{"type":"object","properties":{"fields":`+eventSchemaNames+`,"reason":{"type":"string","enum":["configuration","configuration_removed"]},"tokenId":{"type":"string"}}}`)
	bastion(EventBastionDisabled, JournalRevocation, `{"type":"object","required":["mode"],"properties":{"mode":{"type":"string","enum":["soft","hard"]}}}`)
	bastion(EventBastionEnabled, NoJournal, `{"type":"object"}`)
	bastion(EventBastionDeleted, JournalRevocation, `{"type":"object","properties":{"tokensRevoked":{"type":"integer"}}}`)
	bastion(EventBastionHandoverEnabled, NoJournal, `{"type":"object"}`)
	bastion(EventBastionHandoverDisabled, JournalRevocation, `{"type":"object"}`)
	bastion(EventBastionConfigurationChanged, NoJournal,
		`{"type":"object","required":["parts"],"properties":{"parts":`+eventSchemaNames+`,"before":{"type":"object"},"after":{"type":"object"}}}`)
}
