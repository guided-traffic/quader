package retention

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/journal"
)

// R-CUR-016 (remote-curation.md, "Discard"), R-HA-054: the replay handler of remote.examination.discarded is registered, reads the
// entry the discard writes, and skips an entry it cannot act on before it touches the database. The replay's writes are the
// integration area backup's (TestR_CUR_016_ADiscardAfterARestorePointIsReplayed). Milliseconds, no data.
func TestR_CUR_016_TheReplayOfADiscardReadsItsEntry(t *testing.T) {
	h, ok := journal.HandlerFor(audit.EventRemoteExaminationDiscarded)
	if !ok || h.Domain != "remotecuration" {
		t.Fatalf("the handler of %s: %v %+v", audit.EventRemoteExaminationDiscarded, ok, h)
	}
	written, err := json.Marshal(discardEntry{RepositoryID: "42", Key: "web/npm-remote", Name: "@acme/a", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	e, id, why := decodeDiscard(written)
	if why != "" || id != 42 || e.Name != "@acme/a" || e.Version != "1.0.0" {
		t.Fatalf("the entry the discard writes: %+v %d %q", e, id, why)
	}
	pkg, _ := json.Marshal(discardEntry{RepositoryID: "42", Key: "web/npm-remote", Name: "@acme/a"})
	if _, _, why := decodeDiscard(pkg); why != "" {
		t.Errorf("a package's entry: %q", why)
	}
	long := make([]byte, maxEntryVersion+1)
	for i := range long {
		long[i] = '1'
	}
	for name, raw := range map[string]string{
		"not json":        `[`,
		"no repository":   `{"repositoryId":"","name":"a"}`,
		"a repository 0":  `{"repositoryId":"0","name":"a"}`,
		"no name":         `{"repositoryId":"42","name":""}`,
		"a version 257 b": `{"repositoryId":"42","name":"a","version":"` + string(long) + `"}`,
	} {
		res, err := h.Apply(context.Background(), nil, journal.Env{}, journal.Object{ID: "x", Event: audit.EventRemoteExaminationDiscarded, Entry: json.RawMessage(raw)})
		if err != nil || res.Outcome != journal.Skipped {
			t.Errorf("%s: %+v %v, want skipped", name, res, err)
		}
	}
}
