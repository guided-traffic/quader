//go:build integration

package blob

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/offline"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// TestR_STOR_089_TheOfflineToolReadsTheStacksBucket is R-STOR-089's one integration case: a store with an encrypted connection on the
// stack's S3 bucket and PostgreSQL writes two blobs (one of three segments), the storage keys are exported as quader admin storage
// keys export prints them, and the offline reader then reaches the bucket through a connection element in a file, with the export and
// the key ring's file, and no database: its list opens both records from HeadObject's metadata, its verify reads both whole, and its
// get writes one blob's bytes.
func TestR_STOR_089_TheOfflineToolReadsTheStacksBucket(t *testing.T) {
	ctx := context.Background()
	p := newPG(t)
	e := newS3(t, "offline")
	cfg := e.storage(t)
	conn := e.conn
	conn.ID, conn.Encrypted = "vault", true
	cfg.Connections = []config.StorageConnection{conn}

	ringDir := t.TempDir()
	ringFile := filepath.Join(ringDir, "2026-12.key")
	if err := os.WriteFile(ringFile, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))), 0o600); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{ringFile}, ActiveKeyID: "2026-12"})
	if err != nil {
		t.Fatal(err)
	}
	h := newHolder(t, p, ring)
	conns, err := blob.NewConnections(blob.NewConnection(conn, e.raw))
	if err != nil {
		t.Fatal(err)
	}
	s, err := blob.New(cfg, conns, p.db, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond), blob.WithStorageKeys(h))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordConnections(ctx); err != nil {
		t.Fatal(err)
	}
	c := conns.First()
	if err := h.Ensure(ctx, storagekeys.Connection{ID: c.ID(), KeyedNames: c.KeyedNames(), Prefix: c.Prefix(), Objects: c.Objects()}); err != nil {
		t.Fatal(err)
	}
	small, large := memData(1024, 1), memData(2*65536+1, 2)
	dSmall, dLarge := commit(t, s, small), commit(t, s, large)

	doc, err := storagekeys.Export(ctx, p.db, nil, "it-offline", "", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	export := filepath.Join(t.TempDir(), "export.json")
	element := filepath.Join(t.TempDir(), "vault.yaml")
	el := fmt.Sprintf("id: vault\nbucket: %s\nprefix: %s\nendpoint: %s\nregion: %s\nforce_path_style: %t\nca_file: %s\ncredentials_source: static\naccess_key_id_file: %s\nsecret_access_key_file: %s\nencrypted: true\n",
		conn.Bucket, conn.KeyPrefix(), conn.Endpoint, conn.Region, conn.ForcePathStyle, conn.CAFile, conn.AccessKeyIDFile, conn.SecretAccessKeyFile)
	for f, b := range map[string][]byte{export: raw, element: []byte(el)} {
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	objects, info, err := offline.OpenElement(ctx, element)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := offline.LoadKeys(export, ringDir)
	if err != nil {
		t.Fatal(err)
	}
	defer ks.Close()
	r := offline.NewReader(offline.Bucket(objects, info), ks)
	for name, walk := range map[string]func(func(offline.Result) error) (offline.Totals, error){
		"list":   func(emit func(offline.Result) error) (offline.Totals, error) { return r.List(ctx, emit) },
		"verify": func(emit func(offline.Result) error) (offline.Totals, error) { return r.Verify(ctx, 2, emit) },
	} {
		digests := map[string]bool{}
		tot, err := walk(func(res offline.Result) error { digests[res.SHA256] = res.Status == offline.StatusOK; return nil })
		if err != nil || tot.OK != 2 || tot.Objects != 2 || !digests[dSmall.Hex()] || !digests[dLarge.Hex()] {
			t.Errorf("%s: %+v %v, %v", name, tot, digests, err)
		}
	}
	name, err := r.Locate(ctx, dLarge.Hex())
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "large.bin")
	if res, err := r.Get(ctx, name, out); err != nil || !res.Sound() {
		t.Fatalf("get = %+v, %v", res, err)
	}
	if b, err := os.ReadFile(out); err != nil || !bytes.Equal(b, large) {
		t.Errorf("get wrote %d bytes, %v", len(b), err)
	}
}
