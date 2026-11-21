package bastionmain

import (
	"reflect"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// TestR_SIG_075_TheGatesNarrowingsAreContentEntries: a withdrawn release is the content of its repository (and the virtual ones above it);
// a removed anchor, a block, an anchor switched off or losing a fingerprint and project anchors switched off are the content of every
// repository whose gate is on when the feed is read; a change of an anchor that only widens or renames and switching project anchors on
// yield none; with no gated repository the entry is dropped (R-SIG-075, Q-SIG-94). Unit, milliseconds.
func TestR_SIG_075_TheGatesNarrowingsAreContentEntries(t *testing.T) {
	gated := []rawEntry{{kind: iface.FeedContent, gated: true}}
	if got := mapEvent(FeedEvent{Action: audit.EventSignatureReleaseWithdrawn, Repository: "web/files-internal"}, "default"); !reflect.DeepEqual(got,
		[]rawEntry{{kind: iface.FeedContent, repository: "web/files-internal"}}) {
		t.Errorf("a withdrawn release = %+v", got)
	}
	fp := `[{"kind":"openpgp","value":"C1F3"}]`
	for _, c := range []struct {
		name    string
		action  string
		details string
		want    []rawEntry
	}{
		{"an anchor removed", audit.EventSignatureAnchorRemoved, `{}`, gated},
		{"a signer blocked", audit.EventSignatureSignerBlocked, `{}`, gated},
		{"an anchor switched off", audit.EventSignatureAnchorUpdated, `{"before":{"enabled":true,"fingerprints":` + fp + `},"after":{"enabled":false,"fingerprints":` + fp + `}}`, gated},
		{"an anchor losing a fingerprint", audit.EventSignatureAnchorUpdated, `{"before":{"enabled":true,"fingerprints":` + fp + `},"after":{"enabled":true,"fingerprints":[]}}`, gated},
		{"an anchor renamed", audit.EventSignatureAnchorUpdated, `{"before":{"enabled":true,"title":"a","fingerprints":` + fp + `},"after":{"enabled":true,"title":"b","fingerprints": ` + fp + `}}`, nil},
		{"an anchor switched on", audit.EventSignatureAnchorUpdated, `{"before":{"enabled":false},"after":{"enabled":true}}`, nil},
		{"an anchor's change that does not read", audit.EventSignatureAnchorUpdated, `{"before":7}`, gated},
		{"project anchors off", audit.EventSignatureSettingsChanged, `{"before":{"projectAnchors":true},"after":{"projectAnchors":false}}`, gated},
		{"project anchors on", audit.EventSignatureSettingsChanged, `{"before":{"projectAnchors":false},"after":{"projectAnchors":true}}`, nil},
	} {
		if got := mapEvent(FeedEvent{Action: c.action, Details: []byte(c.details)}, "default"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %+v", c.name, got)
		}
	}
	at := time.Unix(1, 0).UTC()
	l := lookups{gated: []string{"web/files-internal", "web/files-all"}}
	want := []iface.FeedEntry{{ID: "e1", At: at, Kind: iface.FeedContent, Repositories: []string{"web/files-internal", "web/files-all"}}}
	if got := l.entries(FeedEvent{ID: "e1", At: at}, gated); !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %+v", got)
	}
	if got := (lookups{}).entries(FeedEvent{ID: "e2", At: at}, gated); len(got) != 0 {
		t.Errorf("no gated repository = %+v", got)
	}
}
