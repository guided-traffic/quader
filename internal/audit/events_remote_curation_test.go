package audit

import "testing"

// TestR_CUR_019_TheEventsAreRegisteredWithTheirModesAndJournalFlags: the events of the table of R-CUR-019 with their mode, webhook
// flag and journal flag; only the discard of what was fetched for examination is journaled, with a required entry.
func TestR_CUR_019_TheEventsAreRegisteredWithTheirModesAndJournalFlags(t *testing.T) {
	for _, c := range []struct {
		name    string
		mode    Mode
		webhook bool
		journal Journal
	}{
		{EventRemoteRuleCreated, Transactional, true, NoJournal},
		{EventRemoteRuleUpdated, Transactional, true, NoJournal},
		{EventRemoteRuleDeleted, Transactional, true, NoJournal},
		{EventRemoteDeliveryRefused, Async, false, NoJournal},
		{EventRemoteRequestCreated, Transactional, true, NoJournal},
		{EventRemoteRequestAllowed, Transactional, true, NoJournal},
		{EventRemoteRequestDeclined, Transactional, true, NoJournal},
		{EventRemoteRequestWithdrawn, Transactional, true, NoJournal},
		{EventRemoteDownloadStarted, Transactional, false, NoJournal},
		{EventRemoteDownloadRetried, Transactional, false, NoJournal},
		{EventRemoteDownloadCancelled, Transactional, false, NoJournal},
		{EventRemoteExaminationDiscarded, Transactional, false, JournalRevocation},
	} {
		et, ok := Lookup(c.name)
		if !ok {
			t.Errorf("%s is not registered", c.name)
			continue
		}
		if et.Mode != c.mode || et.Webhook != c.webhook || et.Journal != c.journal {
			t.Errorf("%s: mode %s, webhook %v, journal %q", c.name, et.Mode, et.Webhook, et.Journal)
		}
		if c.journal == JournalRevocation && et.Entry != EntryRequired {
			t.Errorf("%s: entry %q, want required", c.name, et.Entry)
		}
	}
}
