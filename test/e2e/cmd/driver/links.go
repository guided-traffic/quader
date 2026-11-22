package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/outbound"
)

// The commands of the end-to-end cases of the signed links (storage.md R-STOR-070, R-STOR-074): the switch written through the
// store a replica's management API writes through (its change area reaches every replica within 2 s), and an object of the
// bucket overwritten behind Quader's back, as a principal who can write the bucket could (H-11), so that a client's digest check
// is shown to refuse a link's altered bytes.
//
//	driver links on|off                      the switch on with minSize 0, or off; prints the stored settings
//	driver overwrite <sha256 hex>            other bytes of the same length over the blob's object on the first connection: the
//	                                         header time of a gzip stream flipped (it still unpacks), any other object random

func linksCmd(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		return fmt.Errorf("usage: driver links on|off")
	}
	p, err := openPeer(ctx, cfg, "e2e-links")
	if err != nil {
		return err
	}
	defer p.close()
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	rec, err := audit.New(audit.Options{DB: p.db, Replica: "e2e-driver", Config: cfg.Audit, Log: log, AppLog: log})
	if err != nil {
		return err
	}
	store := downloadlinks.NewStore(downloadlinks.StoreOptions{Runner: p.db, Changes: p.changes, Audit: rec, Log: log})
	cur, err := store.Settings(ctx)
	if err != nil {
		return err
	}
	next := cur
	next.Enabled = args[0] == "on"
	if next.Enabled {
		next.MinSize = 0 // the cases move a package of kilobytes (the working rules: tests stay runnable)
	}
	stored, err := store.Update(ctx, audit.Actor{Kind: "system", Name: "e2e-driver"}, next, cur.Version)
	if err != nil {
		return err
	}
	return printJSON(out, map[string]any{"enabled": stored.Enabled, "minSize": stored.MinSize, "version": stored.Version})
}

func overwriteCmd(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: driver overwrite <sha256 hex>")
	}
	raw, err := hex.DecodeString(args[0])
	if err != nil || len(raw) != len(blob.Digest{}) {
		return fmt.Errorf("%q is no SHA-256 in hex", args[0])
	}
	var d blob.Digest
	copy(d[:], raw)
	conns, err := config.ResolveConnections(cfg.Storage.Connections, nil)
	if err != nil {
		return err
	}
	f := outbound.New(cfg.Proxy.Outbound)
	client, err := s3.NewFromFactory(ctx, config.ClientConfig(0, conns[0], cfg.Storage.ObjectStore), f)
	if err != nil {
		return err
	}
	key := blob.Key(conns[0].KeyPrefix(), d)
	rc, info, err := client.Get(ctx, key, nil)
	if err != nil {
		return err
	}
	other, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return err
	}
	altered := "random"
	if len(other) > 8 && other[0] == 0x1f && other[1] == 0x8b {
		// A gzip stream (an npm tarball, an image layer, a collection): the modification time of its header is outside its CRC, so
		// the altered bytes still unpack to the same files, and only a client that checks the digest tells them from the stored ones.
		for i := 4; i < 8; i++ {
			other[i] ^= 0xff
		}
		altered = "gzip-mtime"
	} else {
		_, _ = rand.Read(other)
	}
	if err := client.Put(ctx, key, bytes.NewReader(other), info.Size, blob.Checksum{}, nil); err != nil {
		return err
	}
	return printJSON(out, map[string]any{"key": key, "size": info.Size, "altered": altered})
}
