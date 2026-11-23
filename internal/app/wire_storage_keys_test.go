package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

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
