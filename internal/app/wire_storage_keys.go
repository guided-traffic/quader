package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The keys of encrypted stored content (storage.md R-STOR-083, R-STOR-084): packages 2 and 3 of the encryption
// (docs/developer/stored-content-encryption.md). The blob area builds the replica's holder before the store, which reads the keys
// through blob.WithStorageKeys, and fills it after the start checks; the infrastructure bundle lets it follow the change area
// storage_keys, which exists only after the blob area; the task secrets-reencrypt walks the two sealed columns. The upload sessions
// seal their data keys with the key ring through sessionKeys (R-STOR-081).

// newStorageKeys builds the replica's holder of the storage keys over the database and the key ring, and stops it with the replica,
// which destroys every key it holds.
func (a *App) newStorageKeys(ctx context.Context, ring *secrets.KeyRing, db *postgres.DB, log *slog.Logger) (*storagekeys.Holder, error) {
	h, err := storagekeys.New(storagekeys.Options{DB: db, Ring: ring, Log: log})
	if err != nil {
		return nil, err
	}
	return h, a.startComponent(ctx, closer{name: "storage-keys", close: h.Close})
}

// ensureStorageKeys holds the keys of every encrypted connection before the replica turns ready (R-STOR-083, Q-STOR-326): it draws
// those a connection without drain lacks and loads its rows. A state the operator must answer (a location that holds objects of keys
// the database lacks, a key the ring cannot open) ends the start; a failure of the database or the store is retried, like the
// records of the connections before it.
func (a *App) ensureStorageKeys(ctx context.Context, h *storagekeys.Holder, conns *blob.Connections, log *slog.Logger) error {
	for _, c := range conns.List() {
		if !c.Encrypted() {
			continue
		}
		spec := storagekeys.Connection{ID: c.ID(), KeyedNames: c.KeyedNames(), Drain: c.Drain(), Prefix: c.Prefix(), Objects: c.Objects()}
		for delay := time.Second; ; delay = min(delay*2, 15*time.Second) {
			err := h.Ensure(ctx, spec)
			if err == nil {
				break
			}
			if storagekeys.IsRefusal(err) {
				return fmt.Errorf("object store: %s %w", c.Element(), err)
			}
			log.Warn("the storage keys of an encrypted connection could not be read or drawn; retrying", "connection", c.ID(), "error", err, "retry_in", delay.String())
			if !sleep(ctx, delay) {
				return ctx.Err()
			}
		}
	}
	return nil
}

// sessionKeys seals and opens the data key of an upload session on an encrypted connection with the replica's key ring
// (upload_sessions.sealed_data_key, R-STOR-081): the seam uploads.SessionKeys.
type sessionKeys struct{ ring storagekeys.Sealer }

func (k sessionKeys) Seal(session string, dataKey blobcrypt.Key) (string, error) {
	return storagekeys.SealSessionKey(k.ring, session, dataKey)
}

func (k sessionKeys) Open(session, sealed string) (blobcrypt.Key, error) {
	return storagekeys.OpenSessionKey(k.ring, session, sealed)
}

// listenStorageKeys makes the replica's storage keys follow the change area storage_keys (R-STOR-083): the change log exists only after
// the blob area, so the infrastructure bundle, which has both, adds the holder as a listener.
func listenStorageKeys(bl blobArea, co coordArea) {
	if bl.storageKeys != nil && co.changes != nil {
		co.changes.AddListener(bl.storageKeys)
	}
}

// storageKeyColumns are the sealed columns of the storage keys, which the task secrets-reencrypt walks with resealStorageKeys.
func storageKeyColumns() []string {
	return []string{storagekeys.Column().String(), storagekeys.SessionColumn().String()}
}

// resealStorageKeys is the walk of secrets-reencrypt over storage_keys.sealed_key and upload_sessions.sealed_data_key (R-STOR-084
// step 3).
func resealStorageKeys(ctx context.Context, d resealDeps) (storagekeys.ResealResult, error) {
	return storagekeys.Reseal(ctx, d.db, nil, d.ring, 0)
}
