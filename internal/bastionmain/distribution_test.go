package bastionmain

import (
	"context"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/journal"
)

// TestR_BAS_134_TheNarrowingsAreJournaledAndReplayed: bastion.project.removed and repository.bastion.removed are transactional events
// with the journal flag, their widenings bastion.project.allowed and repository.bastion.added are not; the replay handler of the domain
// bastion replays both removals and skips an entry without its IDs before it touches the database. The replay against PostgreSQL is
// test/integration/bastionreg's.
func TestR_BAS_134_TheNarrowingsAreJournaledAndReplayed(t *testing.T) {
	for _, ev := range []string{audit.EventBastionProjectRemoved, audit.EventRepositoryBastionRemoved} {
		typ, ok := audit.Lookup(ev)
		if !ok || typ.Mode != audit.Transactional || typ.Journal != audit.JournalRevocation || typ.Entry != audit.EntryRequired {
			t.Fatalf("%s: %+v", ev, typ)
		}
		h, ok := journal.HandlerFor(ev)
		if !ok || h.Domain != "bastion" {
			t.Fatalf("%s: %+v %v", ev, h, ok)
		}
	}
	for _, ev := range []string{audit.EventBastionProjectAllowed, audit.EventRepositoryBastionAdded} {
		typ, ok := audit.Lookup(ev)
		if !ok || typ.Mode != audit.Transactional || typ.Journal == audit.JournalRevocation {
			t.Fatalf("%s: %+v", ev, typ)
		}
		if _, ok := journal.HandlerFor(ev); ok {
			t.Fatalf("%s, a widening, is replayed", ev)
		}
	}
	h, _ := journal.HandlerFor(audit.EventBastionProjectRemoved)
	for event, entry := range map[string]string{
		audit.EventBastionProjectRemoved:    `{"bastionId":"0192f1c4-0000-7000-8000-0000000000b1","project":"platform"}`,
		audit.EventRepositoryBastionRemoved: `{"repositoryId":"x7","bastionId":"0192f1c4-0000-7000-8000-0000000000b1"}`,
	} {
		res, err := h.Apply(context.Background(), nil, journal.Env{}, journal.Object{Event: event, Entry: []byte(entry)})
		if err != nil || res.Outcome != journal.Skipped {
			t.Fatalf("%s with %s: %+v %v", event, entry, res, err)
		}
	}
}

// TestR_BAS_134_TheFeedTellsTheBastionsOfADistributionChange: the four events are read by the feed (Q-BAS-521); a widening asks for the
// handshake again (settings), a narrowing also drops every permission answer (grants), as a lowered level does.
func TestR_BAS_134_TheFeedTellsTheBastionsOfADistributionChange(t *testing.T) {
	kinds := func(action string) []iface.FeedKind {
		m, ok := mappers()[action]
		if !ok || !slices.Contains(FeedActions(), action) {
			t.Fatalf("%s is not read by the feed", action)
		}
		var out []iface.FeedKind
		for _, e := range m(FeedEvent{Action: action}, &detailsDoc{}, "default") {
			out = append(out, e.kind)
		}
		return out
	}
	for action, want := range map[string][]iface.FeedKind{
		audit.EventBastionProjectAllowed:    {iface.FeedSettings},
		audit.EventRepositoryBastionAdded:   {iface.FeedSettings},
		audit.EventBastionProjectRemoved:    {iface.FeedGrants, iface.FeedSettings},
		audit.EventRepositoryBastionRemoved: {iface.FeedGrants, iface.FeedSettings},
	} {
		if got := kinds(action); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", action, got, want)
		}
	}
}
