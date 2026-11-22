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

// TestR_SIG_075_TheGatesEventsAreRegistered: signature.download_refused and signature.download_by_writer (Q-SIG-240) of the gate per
// repository are asynchronous and not journaled; a release granted is transactional and not journaled (it widens), a release withdrawn transactional and journaled (R-SIG-075).
func TestR_SIG_075_TheGatesEventsAreRegistered(t *testing.T) {
	for _, c := range []struct {
		name    string
		mode    Mode
		journal Journal
	}{
		{EventSignatureDownloadRefused, Async, NoJournal},
		{EventSignatureDownloadByWriter, Async, NoJournal},
		{EventSignatureReleaseGranted, Transactional, NoJournal},
		{EventSignatureReleaseWithdrawn, Transactional, JournalRevocation},
	} {
		et, ok := Lookup(c.name)
		if !ok || et.Mode != c.mode || et.Journal != c.journal || et.Schema == "" || et.Category != CategoryContent {
			t.Errorf("%s: %+v %v", c.name, et, ok)
		}
	}
}
