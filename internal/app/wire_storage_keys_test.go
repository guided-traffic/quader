package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/journal"
)

// TestR_HA_054_TheReplayOfADestructionIsRegistered: system.storage.keys_destroyed has the replay handler of the storage keys, which
// skips an entry of a connection the restoring configuration names with its note and reads a malformed entry as skipped; the four
// outcomes of the handler's work are internal/blob/keys's (TestR_HA_054_TheReplayOfADestruction).
func TestR_HA_054_TheReplayOfADestructionIsRegistered(t *testing.T) {
	h, ok := journal.HandlerFor(audit.EventStorageKeysDestroyed)
	if !ok || h.Domain != "storage_keys" {
		t.Fatalf("R-HA-054 no handler for %s: %+v", audit.EventStorageKeysDestroyed, h)
	}
	o := journal.Object{
		ID: "0193aaaa-0000-7000-8000-000000000001", Event: audit.EventStorageKeysDestroyed,
		Entry: json.RawMessage(`{"connection":"archive","keyIds":["sk-archive-1-0000aaaa"],"destroyedAt":"2027-04-02T09:00:00Z"}`),
	}
	res, err := h.Apply(context.Background(), nil, journal.Env{Now: time.Now(), StorageConnections: []string{"archive"}}, o)
	if err != nil || res.Outcome != journal.Skipped || res.Subject != "archive" || res.Note != "connection archive is in this installation's configuration; its keys are not destroyed" {
		t.Errorf("R-HA-054 Q-HA-417 a configured connection: %+v %v", res, err)
	}
	o.Entry = json.RawMessage(`{"connection":"archive","keyIds":["sk-other-1-0000aaaa"],"destroyedAt":"2027-04-02T09:00:00Z"}`)
	if res, err := h.Apply(context.Background(), nil, journal.Env{StorageConnections: []string{"default"}}, o); err != nil || res.Outcome != journal.Skipped || len(res.Detail) == 0 {
		t.Errorf("R-HA-054 a malformed entry: %+v %v", res, err)
	}
}

// TestR_STOR_075_AnEncryptedConnectionNeedsMetadata is the start check's requirement of R-STOR-075 (Q-STOR-306): an encrypted
// connection whose store does not keep an object's metadata stops the start with the message of the specification; the same store
// starts a plain connection.
func TestR_STOR_075_AnEncryptedConnectionNeedsMetadata(t *testing.T) {
	opts := blob.StartCheckOptions{Replica: "unit", Await: []blob.AwaitOption{blob.WithAwaitBackoff(time.Millisecond, 5*time.Millisecond)}}
	start := func(encrypted bool) error {
		mem := blobtest.New()
		mem.DropMetadata = true
		p := "quader"
		cs, err := blob.NewConnections(blob.NewConnection(config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: encrypted}, mem))
		if err != nil {
			t.Fatal(err)
		}
		_, err = startConnections(context.Background(), cs, true, opts, true)
		return err
	}
	t.Run("R-STOR-075 an encrypted connection on a store that drops metadata stops the start", func(t *testing.T) {
		err := start(true)
		want := "connection archive: the store did not keep the metadata of the probe object; an encrypted connection keeps each object's wrapped data key there (storage.md R-STOR-077)"
		if !errors.Is(err, blob.ErrMetadataNotKept) || !strings.Contains(err.Error(), want) {
			t.Fatalf("the start ended with %v", err)
		}
	})
	t.Run("R-STOR-075 a plain connection on the same store starts", func(t *testing.T) {
		if err := start(false); err != nil {
			t.Fatal(err)
		}
	})
}
