package audit

import "testing"

// TestR_SIG_057_TheSigningEventsAreRegisteredWithTheirSchemasAndJournalFlags: the seven events of R-SIG-057 with the mode and the journal
// flag of the table: a removal and a retirement always carry an entry, the refusal of the files and a failed operation are recorded
// asynchronously.
func TestR_SIG_057_TheSigningEventsAreRegisteredWithTheirSchemasAndJournalFlags(t *testing.T) {
	for _, c := range []struct {
		name    string
		mode    Mode
		journal Journal
		entry   EntryRule
	}{
		{EventSigningKeyCreated, Transactional, NoJournal, ""},
		{EventSigningInstalled, Transactional, NoJournal, ""},
		{EventSigningSwitched, Transactional, NoJournal, ""},
		{EventSigningRemoved, Transactional, JournalRevocation, EntryRequired},
		{EventSigningRetired, Transactional, JournalRevocation, EntryRequired},
		{EventSigningRefused, Async, NoJournal, ""},
		{EventSigningFailed, Async, NoJournal, ""},
	} {
		et, ok := Lookup(c.name)
		if !ok {
			t.Errorf("%s is not registered", c.name)
			continue
		}
		if et.Mode != c.mode || et.Journal != c.journal || et.Entry != c.entry {
			t.Errorf("%s: mode %s, journal %q, entry %q", c.name, et.Mode, et.Journal, et.Entry)
		}
		if et.Schema == "" {
			t.Errorf("%s has no schema", c.name)
		}
	}
}
