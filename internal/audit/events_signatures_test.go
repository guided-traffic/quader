package audit

import "testing"

// TestR_SIG_039_TheEventsAreRegisteredWithTheirSchemasAndJournalFlags: the nine events of R-SIG-039 with the mode and the journal flag
// of the table (Q-SIG-42): a removed anchor and a block always carry an entry, an anchor's change only when it narrows and the switch
// only when it is turned off.
func TestR_SIG_039_TheEventsAreRegisteredWithTheirSchemasAndJournalFlags(t *testing.T) {
	for _, c := range []struct {
		name    string
		mode    Mode
		journal Journal
		entry   EntryRule
	}{
		{EventSignatureInvalid, Transactional, NoJournal, ""},
		{EventSignatureAnchorCreated, Transactional, NoJournal, ""},
		{EventSignatureAnchorUpdated, Transactional, JournalRevocation, EntryOptional},
		{EventSignatureAnchorRemoved, Transactional, JournalRevocation, EntryRequired},
		{EventSignatureSettingsChanged, Transactional, JournalRevocation, EntryOptional},
		{EventSignatureSignerBlocked, Transactional, JournalRevocation, EntryRequired},
		{EventSignatureSignerUnblocked, Transactional, NoJournal, ""},
		{EventSignatureTrustRootUpdated, Transactional, NoJournal, ""},
		{EventSignatureTrustRootFailed, Async, NoJournal, ""},
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

// TestR_SIG_075_TheGatesEventIsRegistered: signature.download_refused of the gate per repository is asynchronous and not journaled, with
// its schema (R-SIG-075).
func TestR_SIG_075_TheGatesEventIsRegistered(t *testing.T) {
	et, ok := Lookup(EventSignatureDownloadRefused)
	if !ok || et.Mode != Async || et.Journal != NoJournal || et.Schema == "" || et.Category != CategoryContent {
		t.Fatalf("%+v %v", et, ok)
	}
}
