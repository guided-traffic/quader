package downloadlinks

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/qname"
)

// handoverRig is the links' rig with minSize 0, so that the rig's small items may be handed over.
func handoverRig(t *testing.T, mutate func(id string, c *config.StorageConnection)) *rig {
	t.Helper()
	r := newRig(t, mutate)
	s := r.links.Settings()
	s.MinSize = 0
	r.links.SetSettings(s)
	return r
}

// sealed is a replica's side over the rig's two connections, each encrypted where the case says, with the replica's storage keys,
// a blob store whose CopyDataKey the handover calls (through keySource, which keeps every key it handed out), and the trail the
// trace is written to. One blob of a few hundred bytes lies on both connections: a plain object where the connection is plain, a
// sealed object under a data key of its own where it is encrypted (each encrypted copy has its own data key, R-STOR-087).
type sealed struct {
	*rig
	keys  *keySource
	trail *trail
	tx    *runner
	met   *observability.Metrics
	plain []byte
	item  items.Item
	// dataKeys are the data keys of the copies on encrypted connections, by connection.
	dataKeys map[string]blobcrypt.DataKey
	// sks are the replica's storage keys, which a case can destroy.
	sks *storageKeys
}

// newSealed builds the case; storageKeys are the storage keys the replica holds, by connection (a connection missing from it holds
// none, and its objects are wrapped under a key the replica does not hold).
func newSealed(t *testing.T, encrypted map[string]bool, held map[string]bool) *sealed {
	t.Helper()
	r := handoverRig(t, func(id string, c *config.StorageConnection) { c.Encrypted = encrypted[id] })
	s := &sealed{rig: r, trail: &trail{}, tx: &runner{}, met: observability.NewMetrics("web"), dataKeys: map[string]blobcrypt.DataKey{}}
	s.plain = bytes.Repeat([]byte("a layer handed over to a Bastion, "), 12)
	d := blob.Digest(sha256.Sum256(s.plain))
	key := blob.Key("quader", d)
	stored := mustSealedSize(t, len(s.plain))
	sks := &storageKeys{keys: map[string]*blobcrypt.StorageKey{}, destroyed: map[string]string{}}
	s.sks = sks
	var copies []blob.Copy
	for _, id := range []string{"plain", "second"} {
		if !encrypted[id] {
			r.stores[id].Set(key, s.plain, time.Now())
			copies = append(copies, blob.Copy{Connection: id, ObjectKey: key, StoredSize: int64(len(s.plain)), State: blob.CopyPresent})
			continue
		}
		raw := make([]byte, 32)
		_, _ = rand.Read(raw)
		sk, err := blobcrypt.NewStorageKey("sk-"+id+"-1-0a1b2c3d", raw)
		if err != nil {
			t.Fatal(err)
		}
		if held[id] {
			sks.keys[id] = sk
		}
		body, rec, dk := sealObject(t, sk, s.plain)
		if err := r.stores[id].Put(context.Background(), key, bytes.NewReader(body), int64(len(body)), blob.Checksum{}, rec.Metadata()); err != nil {
			t.Fatal(err)
		}
		s.dataKeys[id] = dk
		copies = append(copies, blob.Copy{Connection: id, ObjectKey: key, StoredSize: stored, StorageKeyID: sk.ID, State: blob.CopyPresent})
	}
	cfg := blobtest.Storage(t, t.TempDir())
	store, err := blob.New(cfg, r.conns, noStatements{t}, blob.WithStorageKeys(sks))
	if err != nil {
		t.Fatal(err)
	}
	s.keys = &keySource{store: store}
	s.item = items.Item{
		ID: 9, RepoID: 3, Repository: qname.Repo{Project: "web", Key: "probe-local"}, Path: "app/_blobs/sha256:" + d.Hex(), Kind: items.KindFile,
		Size: int64(len(s.plain)), MediaType: "application/octet-stream", Digest: d, ContentStatus: blob.StatusOK, Copies: copies, CopiesRead: true,
	}
	s.relink()
	return s
}

// relink builds the replica's side again over the rig's connections with the data keys, the trail and the metrics of the case.
func (s *sealed) relink() {
	set := s.links.Settings()
	s.links = New(Options{
		Connections: s.conns, Rule: HostRuleOf([]string{"quader.example.com"}), Handovers: on{handoverBastion: true}, Replica: "r1",
		DataKeys: s.keys, Runner: s.tx, Audit: s.trail, Metrics: NewMetrics(s.met),
	})
	s.links.SetSettings(set)
	s.links.RunLinkChecks(context.Background())
}

// offer runs the serve point's two functions for the case's item as a recognized forward of the registration dmz from the Bastion
// replica bastion-7 that offered up to 1 MiB, and returns whether it was handed over and the marking stage's record.
func (s *sealed) offer(t *testing.T) (bool, *bastionmain.Absence, []items.ServedEvent) {
	t.Helper()
	ctx := bastionmain.WithRecognition(context.Background(), bastionmain.Recognition{
		Bastion: "dmz", BastionID: handoverBastion, Replica: "bastion-7", Interface: iface.InterfaceVersion,
		Offer: iface.HandoverOffer{MaxSize: 1 << 20}, Offered: true,
	})
	ctx, abs := bastionmain.WithAbsence(ctx)
	s.trail.abs = abs
	var served []items.ServedEvent
	handover, handedOver := s.links.HandoverOptions(ctx, HandoverDownload{
		Item: s.item, Artifact: true, Decided: true, Endpoint: "default",
		Actor:  audit.Actor{Kind: "service_account", ID: "0192f1c4-0000-7000-8000-0000000000c1", Name: "ci-reader"},
		Source: audit.Source{ClientAddr: "203.0.113.70"},
		Record: func(e items.ServedEvent) { served = append(served, e) },
	})
	if handover == nil {
		t.Fatal("no handover was offered to the serve point")
	}
	ok := handover()
	if ok {
		handedOver()
	}
	return ok, abs, served
}

// declined is the declined offers' counter of one reason.
func (s *sealed) declined(t *testing.T, reason string) float64 {
	t.Helper()
	return gathered(t, s.met, "quader_storage_download_handovers_declined_total", map[string]string{"reason": reason})
}

const handoverBastion = "0192f1c4-0000-7000-8000-0000000000b1"

// on is a snapshot of the registrations whose handover is on, by principal ID.
type on map[string]bool

func (o on) On(id string) bool { return o[id] }

// keySource is the blob store's CopyDataKey with what it handed out kept, so that a case can see each key destroyed.
type keySource struct {
	store *blob.Store
	mu    sync.Mutex
	taken []blobcrypt.DataKey
	from  []blob.Copy
	errs  []error
}

func (k *keySource) CopyDataKey(ctx context.Context, d blob.Digest, size int64, cp blob.Copy) (blobcrypt.DataKey, error) {
	dk, err := k.store.CopyDataKey(ctx, d, size, cp)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.from = append(k.from, cp)
	if err == nil {
		k.taken = append(k.taken, dk)
	} else {
		k.errs = append(k.errs, err)
	}
	return dk, err
}

// allDestroyed reports whether every format's key the handover took was destroyed.
func (k *keySource) allDestroyed() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, dk := range k.taken {
		if _, err := dk.Key.Bytes(); err == nil {
			return false
		}
	}
	return true
}

func (k *keySource) calls() int { k.mu.Lock(); defer k.mu.Unlock(); return len(k.from) }

// trail is the audit's recorder: it keeps what is recorded, can fail or panic, and runs at before it records, so that a case can
// look at the world at the moment of the insert.
type trail struct {
	mu     sync.Mutex
	events []audit.Event
	fail   error
	panics bool
	at     func()
	abs    *bastionmain.Absence
}

func (r *trail) RecordTx(_ context.Context, _ meta.Tx, ev audit.Event) error {
	if r.at != nil {
		r.at()
	}
	if r.panics {
		panic("the trail broke")
	}
	if r.fail != nil {
		return r.fail
	}
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	return nil
}

func (r *trail) Record(context.Context, audit.Event) {}

// runner counts the transactions.
type runner struct{ n int }

func (r *runner) InTx(_ context.Context, fn func(meta.Tx) error) error { r.n++; return fn(nopTx{}) }

type nopTx struct{}

func (nopTx) Before(func(context.Context, meta.Tx) error) {}
func (nopTx) After(func())                                {}
func (nopTx) OnRollback(func())                           {}
func (nopTx) Value(any) any                               { return nil }
func (nopTx) SetValue(any, any)                           {}

// noStatements is the blob store's database: CopyDataKey reads no row (Q-STOR-470), so a transaction fails the test.
type noStatements struct{ t *testing.T }

func (n noStatements) InTx(context.Context, func(meta.Tx) error) error {
	n.t.Error("the handover's data key ran a statement")
	return errors.New("no statements")
}

// storageKeys are the replica's storage keys by connection: the seam blob.StorageKeys. A connection in destroyed answers its key's
// identifier as the holder answers a destroyed row (blobcrypt.ErrDestroyedKey, R-STOR-101).
type storageKeys struct {
	keys      map[string]*blobcrypt.StorageKey
	destroyed map[string]string // connection -> the identifier of its destroyed storage key
}

func (s *storageKeys) Active(_ context.Context, connection string) (*blobcrypt.StorageKey, error) {
	if sk := s.keys[connection]; sk != nil {
		return sk, nil
	}
	return nil, errors.New("no storage key")
}

func (*storageKeys) NameKey(string) (blobcrypt.Key, error) {
	return blobcrypt.Key{}, errors.New("no name key")
}

func (s *storageKeys) Lookup(_ context.Context, connection string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		if s.destroyed[connection] == id {
			return nil, fmt.Errorf("storage key %s: %w", id, blobcrypt.ErrDestroyedKey)
		}
		if sk := s.keys[connection]; sk != nil && sk.ID == id {
			return sk, nil
		}
		return nil, nil
	}
}

// destroy makes a connection's storage key one whose row is destroyed: the replica holds it no more, and its identifier answers
// destroyed_key (the key destruction of R-STOR-097 to R-STOR-101).
func (s *sealed) destroy(connection string) {
	sk := s.sks.keys[connection]
	delete(s.sks.keys, connection)
	s.sks.destroyed[connection] = sk.ID
}

// sealObject seals plain under a new data key, wraps it under sk and returns the stored bytes, the record and the data key.
func sealObject(t *testing.T, sk *blobcrypt.StorageKey, plain []byte) ([]byte, blobcrypt.Record, blobcrypt.DataKey) {
	t.Helper()
	k, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := blobcrypt.NewSeed(rand.Reader)
	dk := blobcrypt.DataKey{Key: k, Digest: sha256.Sum256(plain), Size: int64(len(plain))}
	sec, err := blobcrypt.NewSection(k, seed, bytes.NewReader(plain), 0, dk.Size, &blobcrypt.Final{Size: dk.Size, Digest: dk.Digest})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(sec)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := blobcrypt.Wrap(sk, dk, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return body, rec, dk
}

func mustSealedSize(t *testing.T, n int) int64 {
	t.Helper()
	s, err := blobcrypt.StoredSize(int64(n))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// gathered is the value of one series of a counter family, 0 when it does not exist.
func gathered(t *testing.T, m *observability.Metrics, family string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != family {
			continue
		}
		for _, s := range mf.GetMetric() {
			match := true
			for _, l := range s.GetLabel() {
				if v, ok := labels[l.GetName()]; ok && v != l.GetValue() {
					match = false
				}
			}
			if match {
				return s.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// opens reports whether the handed key reads the object behind the value's link to the blob's plaintext.
func (s *sealed) opens(t *testing.T, v iface.Handover) bool {
	t.Helper()
	resp, err := s.stores[v.Connection].Do(httptest.NewRequest(http.MethodGet, v.Link, nil))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("the link: %v", err)
	}
	defer resp.Body.Close()
	k, err := blobcrypt.KeyFromBytes(v.DataKey.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Destroy()
	r, err := blobcrypt.NewReader(resp.Body, blobcrypt.DataKey{Key: k, Digest: [32]byte(s.item.Digest), Size: s.item.Size}, true)
	if err != nil {
		return false
	}
	got, err := io.ReadAll(r)
	return err == nil && bytes.Equal(got, s.plain)
}

// TestR_STOR_096_ThePlainCopyFirstAndTheTraceOfAKey: of the copies a read would try now, the handover names the first on a
// connection that can serve a handover and is not encrypted, and only without one the first encrypted one (Q-STOR-341); for that
// copy it hands that copy's own data key, which opens its object, with the stored size S(size); bastion.data_key.handed is written
// in a transaction of its own after the key was destroyed and before the answer is noted, with its members and without the key or
// the link; a failing insert streams; every path destroys the key; a key the replica does not hold, a credential that expires and a
// signing error stream, the last two without taking a key (Q-STOR-471).
func TestR_STOR_096_ThePlainCopyFirstAndTheTraceOfAKey(t *testing.T) {
	ctx := context.Background()
	const offer = 1 << 20
	t.Run("R-STOR-095 the first good copy, against its public address, without overrides", func(t *testing.T) {
		r := newRig(t, nil)
		s := r.links.Settings()
		s.MinSize, s.PublicAddresses = 0, map[string]string{"plain": "https://objects.example.com"}
		r.links.SetSettings(s)
		h, _, declined := r.links.handover(ctx, r.item(t, 64), offer)
		if declined != "" || h.Connection != "plain" || h.Encrypted || h.Size != 64 || h.StoredSize != 64 {
			t.Fatalf("%q %v", declined, h)
		}
		u, _ := url.Parse(h.Link)
		q := u.Query()
		if u.Host != "objects.example.com" || q.Has("response-content-type") || q.Has("response-content-disposition") || q.Has("response-cache-control") ||
			q.Get("X-Amz-Expires") != "60" {
			t.Fatalf("%s", h.Link)
		}
	})
	t.Run("R-STOR-096 with an encrypted copy first and a plain one second, the plain one is named and no key is taken", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true}, map[string]bool{"plain": true})
		h, _, declined := s.links.handover(ctx, s.item, offer)
		if declined != "" || h.Connection != "second" || h.Encrypted || h.DataKey.Present() || h.StoredSize != s.item.Size {
			t.Fatalf("%q %v", declined, h)
		}
		if s.keys.calls() != 0 || s.stores["plain"].Count("head") != 0 {
			t.Fatalf("a data key was asked for (%d) while a plain copy could serve", s.keys.calls())
		}
	})
	t.Run("R-STOR-095 R-STOR-096 R-STOR-087 with encrypted copies alone, the named copy's own data key is handed with S(size)", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{"plain": true, "second": true})
		// The first copy's connection cannot serve a handover, so the second is named: its key, never the first's.
		s.conns.List()[0].SetLinkCheck(blob.LinkCheck{Ran: true, Detail: "the link answered 403"})
		s.links.SetSettings(s.links.Settings())
		h, keyID, declined := s.links.handover(ctx, s.item, offer)
		defer s.links.destroy(h.DataKey)
		if declined != "" || h.Connection != "second" || !h.Encrypted || !h.DataKey.Present() || keyID != "sk-second-1-0a1b2c3d" {
			t.Fatalf("%q %v %q", declined, h, keyID)
		}
		if want := mustSealedSize(t, len(s.plain)); h.StoredSize != want {
			t.Fatalf("storedSize %d, want S(size) %d", h.StoredSize, want)
		}
		if !s.opens(t, h) {
			t.Fatal("the handed key does not open the named copy's object")
		}
		first, _ := blobcrypt.KeyFromBytes(h.DataKey.Bytes())
		if first.Equal(s.dataKeys["plain"].Key) || !first.Equal(s.dataKeys["second"].Key) {
			t.Fatal("the handed key is not the named copy's own")
		}
		first.Destroy()
		if n := s.stores["second"].Count("head"); n != 1 {
			t.Fatalf("%d HeadObject requests, want one (R-STOR-095 step 8)", n)
		}
		if !s.keys.allDestroyed() {
			t.Fatal("the format's key outlived the copy into the value")
		}
	})
	t.Run("R-STOR-096 the trace is written after the key is destroyed and before the answer, with its members, and the key leaves", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"second": true}, map[string]bool{"second": true})
		s.conns.List()[0].SetLinkCheck(blob.LinkCheck{Ran: true, Detail: "the link answered 403"})
		s.links.SetSettings(s.links.Settings())
		var atInsert struct {
			noted              bool
			held               int64
			formatKeyDestroyed bool
		}
		s.trail.at = func() {
			atInsert.noted, atInsert.held, atInsert.formatKeyDestroyed = s.trail.abs.Handover() != "", s.links.KeysHeld(), s.keys.allDestroyed()
		}
		ok, abs, served := s.offer(t)
		if !ok || abs.Handover() == "" {
			t.Fatal("not handed over")
		}
		if atInsert.noted || atInsert.held != 0 || !atInsert.formatKeyDestroyed {
			t.Fatalf("at the trace's insert: answer noted %v, keys held %d, the format's key destroyed %v", atInsert.noted, atInsert.held, atInsert.formatKeyDestroyed)
		}
		if s.tx.n != 1 || len(s.trail.events) != 1 {
			t.Fatalf("%d transactions, %d events: the trace is one insert in a transaction of its own", s.tx.n, len(s.trail.events))
		}
		v, err := iface.DecodeHandover(abs.Handover())
		if err != nil {
			t.Fatal(err)
		}
		if !v.Encrypted || v.Connection != "second" || !s.opens(t, v) {
			t.Fatalf("the value %v does not carry the copy's key", v)
		}
		ev := s.trail.events[0]
		var details map[string]string
		if err := json.Unmarshal(ev.Details, &details); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"bastion": "dmz", "replica": "bastion-7", "sha256": s.item.Digest.Hex(), "connection": "second",
			"storageKeyId": "sk-second-1-0a1b2c3d", "linkExpiresAt": v.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if len(details) != len(want) {
			t.Fatalf("details %v", details)
		}
		for k, w := range want {
			if details[k] != w {
				t.Errorf("details[%s] = %q, want %q", k, details[k], w)
			}
		}
		if ev.Type != audit.EventBastionDataKeyHanded || ev.Actor.Name != "ci-reader" || ev.Source.ClientAddr != "203.0.113.70" ||
			ev.Repository != "web/probe-local" || ev.Project != "web" || ev.Path != s.item.Path || ev.Endpoint != "default" ||
			ev.Subject != (audit.Subject{Kind: "bastion", ID: handoverBastion, Name: "dmz"}) || ev.Journal != nil {
			t.Fatalf("event %+v", ev)
		}
		raw := s.dataKeys["second"].Key
		b, _ := raw.Bytes()
		all, _ := json.Marshal(ev)
		for _, form := range []string{hex.EncodeToString(b), base64.RawURLEncoding.EncodeToString(b), base64.StdEncoding.EncodeToString(b), v.Link, "X-Amz-Signature"} {
			if strings.Contains(string(all), form) {
				t.Fatalf("the trace holds the key or the link (%.12s…)", form)
			}
		}
		if len(served) != 1 || !served[0].Encrypted || served[0].Connection != "second" || served[0].Actor.Name != "ci-reader" {
			t.Fatalf("served %+v", served)
		}
		if got := gathered(t, s.met, "quader_storage_download_handovers_total", map[string]string{"bastion": "dmz", "kind": HandoverKindLinkAndKey}); got != 1 {
			t.Fatalf("quader_storage_download_handovers_total{kind=link_and_key} = %v", got)
		}
		if s.links.KeysHeld() != 0 || !s.keys.allDestroyed() {
			t.Fatal("a key outlived the handover")
		}
	})
	t.Run("R-STOR-095 step 10 a failing insert streams and counts trace, and no key is held", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{"plain": true, "second": true})
		s.trail.fail = errors.New("the database refused the insert")
		ok, abs, served := s.offer(t)
		if ok || abs.Handover() != "" || len(served) != 0 || s.declined(t, HandoverDeclinedTrace) != 1 {
			t.Fatalf("handed over %v, noted %v, declined for trace %v", ok, abs.Handover() != "", s.declined(t, HandoverDeclinedTrace))
		}
		if s.links.KeysHeld() != 0 || !s.keys.allDestroyed() || s.keys.calls() != 1 {
			t.Fatalf("keys held %d, the format's keys destroyed %v", s.links.KeysHeld(), s.keys.allDestroyed())
		}
	})
	t.Run("R-STOR-096 a panic of the trail leaves no key held", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{"plain": true, "second": true})
		s.trail.panics = true
		func() {
			defer func() { _ = recover() }()
			s.offer(t)
		}()
		if s.links.KeysHeld() != 0 || !s.keys.allDestroyed() || s.keys.calls() != 1 {
			t.Fatalf("keys held %d after a panic", s.links.KeysHeld())
		}
	})
	t.Run("R-STOR-095 a value refused on its form after the key was taken streams, writes no trace and holds no key", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{"plain": true, "second": true})
		set := s.links.Settings()
		set.PublicAddresses = map[string]string{"plain": "https://" + strings.Repeat("a", 9000) + ".example.com"} // a link above 8 KiB
		s.links.SetSettings(set)
		ok, abs, _ := s.offer(t)
		if ok || abs.Handover() != "" || s.keys.calls() != 1 || len(s.trail.events) != 0 || s.tx.n != 0 || s.declined(t, HandoverDeclinedSigning) != 1 {
			t.Fatalf("handed over %v, keys taken %d, traces %d", ok, s.keys.calls(), len(s.trail.events))
		}
		if s.links.KeysHeld() != 0 || !s.keys.allDestroyed() {
			t.Fatal("the key outlived a refused value")
		}
	})
	t.Run("R-STOR-095 step 8 a key the replica does not hold streams with the reason key, and nothing is traced", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{})
		ok, abs, _ := s.offer(t)
		if ok || abs.Handover() != "" || s.declined(t, HandoverDeclinedKey) != 1 || len(s.trail.events) != 0 || s.links.KeysHeld() != 0 {
			t.Fatalf("handed over %v, declined for key %v", ok, s.declined(t, HandoverDeclinedKey))
		}
	})
	t.Run("R-STOR-095 step 8 R-STOR-101 a destroyed storage key fails CopyDataKey with destroyed_key: the forward streams with the reason key, and nothing is traced", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{"plain": true, "second": true})
		s.destroy("plain")
		ok, abs, served := s.offer(t)
		if ok || abs.Handover() != "" || len(served) != 0 || s.declined(t, HandoverDeclinedKey) != 1 {
			t.Fatalf("handed over %v, declined for key %v", ok, s.declined(t, HandoverDeclinedKey))
		}
		if len(s.keys.errs) != 1 {
			t.Fatalf("%d failed openings of a data key, want one", len(s.keys.errs))
		}
		if r, _ := blobcrypt.ReasonOf(s.keys.errs[0]); r != blobcrypt.ReasonDestroyedKey {
			t.Fatalf("CopyDataKey: %v, want destroyed_key", s.keys.errs[0])
		}
		if len(s.trail.events) != 0 || s.tx.n != 0 || s.links.KeysHeld() != 0 || s.stores["second"].Count("head") != 0 {
			t.Fatalf("%d traces, %d transactions, %d keys held; the next copy was asked: the forward must stream, not fall to another key",
				len(s.trail.events), s.tx.n, s.links.KeysHeld())
		}
	})
	t.Run("R-STOR-095 a credential that expires and a signing error stream without taking a key", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{"plain": true, "second": true})
		s.stores["plain"].SetCredentialExpiry(time.Now().Add(3 * time.Second))
		if ok, _, _ := s.offer(t); ok || s.declined(t, HandoverDeclinedCredential) != 1 {
			t.Fatalf("a credential that expires in 3 s: handed over %v", ok)
		}
		s.stores["plain"].SetCredentialExpiry(time.Time{})
		s.stores["plain"].SetFault(func(op, _ string) error {
			if op == "presign" {
				return errors.New("no signer")
			}
			return nil
		})
		if ok, _, _ := s.offer(t); ok || s.declined(t, HandoverDeclinedSigning) != 1 {
			t.Fatalf("a signing error: handed over %v", ok)
		}
		if s.keys.calls() != 0 || s.stores["plain"].Count("head") != 0 {
			t.Fatalf("%d data keys asked for before the signature held", s.keys.calls())
		}
	})
	t.Run("R-STOR-096 an encrypted copy whose row names another stored size, a failed link check, a detached connection and a plain copy of another length hand it to the next", func(t *testing.T) {
		s := newSealed(t, map[string]bool{"plain": true, "second": true}, map[string]bool{"plain": true, "second": true})
		it := s.item
		it.Copies = append([]blob.Copy(nil), s.item.Copies...)
		it.Copies[0].StoredSize = s.item.Size
		h, _, declined := s.links.handover(ctx, it, offer)
		s.links.destroy(h.DataKey)
		if declined != "" || h.Connection != "second" {
			t.Fatalf("another stored size: %q %v", declined, h)
		}
		it.Copies[1].StoredSize = s.item.Size
		if _, _, declined := s.links.handover(ctx, it, offer); declined != HandoverDeclinedConnection {
			t.Fatalf("no encrypted copy of S(size) left: %q", declined)
		}
		r := handoverRig(t, nil)
		r.conns.List()[0].SetLinkCheck(blob.LinkCheck{Ran: true, Detail: "the link answered 403"})
		r.links.SetSettings(r.links.Settings())
		if h, _, _ := r.links.handover(ctx, r.item(t, 64), offer); h.Connection != "second" {
			t.Fatalf("a failed link check: %v", h)
		}
		r = handoverRig(t, nil)
		pi := r.item(t, 64)
		pi.Copies[0].Connection = "detached"
		if h, _, _ := r.links.handover(ctx, pi, offer); h.Connection != "second" {
			t.Fatalf("a detached connection: %v", h)
		}
		pi = r.item(t, 64)
		pi.Copies[0].StoredSize = 65
		if h, _, _ := r.links.handover(ctx, pi, offer); h.Connection != "second" {
			t.Fatalf("a copy of another length: %v", h)
		}
		pi.Copies[1].State = blob.CopyCorrupt
		if _, _, declined := r.links.handover(ctx, pi, offer); declined != HandoverDeclinedConnection {
			t.Fatalf("no copy left: %q", declined)
		}
	})
	t.Run("R-STOR-095 the size lies within minSize and the offer's max-size", func(t *testing.T) {
		r := newRig(t, nil) // minSize is the default 1 MiB
		if _, _, declined := r.links.handover(ctx, r.item(t, 64), offer); declined != HandoverDeclinedSize {
			t.Fatalf("below minSize: %q", declined)
		}
		s := r.links.Settings()
		s.MinSize = 0
		r.links.SetSettings(s)
		if _, _, declined := r.links.handover(ctx, r.item(t, 64), 63); declined != HandoverDeclinedSize {
			t.Fatalf("above the offer: %q", declined)
		}
		if _, _, declined := r.links.handover(ctx, r.item(t, 64), 64); declined != "" {
			t.Fatalf("at the offer: %q", declined)
		}
	})
}
