package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/bastion/handover"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The Bastion's side of the handover per blob (R-BAS-072 to R-BAS-077): the site double of the main installation answers
// a forward that offered with a handover of a blob the test put into the TLS store of maintest, and the engine reads it
// with the real link client. Blobs are 64 to 192 KiB, made in memory; an encrypted one is written with the format's writer
// under a data key the double hands over.

const (
	hoTgz  = "/api/npm/web/npm-all/@acme/big/-/big-1.0.0.tgz"
	hoTgz2 = "/api/npm/web/npm-all/@acme/big/-/big-2.0.0.tgz"
	hoTgz3 = "/api/npm/web/npm-all/@acme/big/-/big-3.0.0.tgz"
	hoTgz4 = "/api/npm/web/npm-all/@acme/big/-/big-4.0.0.tgz"
	hoPkg  = "@acme/big"
	// hoBound ends a wait for a state the read reaches; it only ends a hung test.
	hoBound = 15 * time.Second
)

// handed is how the double hands a path over: the blob, the store's object, and for an encrypted copy its stored size and
// the data key's bytes.
type handed struct {
	plain  []byte
	object string
	stored int64
	raw    []byte
}

// hoRig is a site harness whose main installation hands over the paths of handed, a store, and the link client.
type hoRig struct {
	*siteHarness
	store     *maintest.Store
	client    *handover.Client
	destroyed atomic.Int64
	// answers counts the handover answers given; closed counts those whose body the engine closed.
	answers, closed atomic.Int64

	mu     sync.Mutex
	handed map[string]handed
	// mutate changes a handover before it is answered; wholeRange answers the Bastion's continuation with a 200 of the
	// whole blob instead of the 206, rangeETag gives the 206 another ETag.
	mutate     func(*iface.Handover)
	wholeRange bool
	rangeETag  string
	// given are the data keys handed over, as the main installation's answers carried them.
	given []iface.HandoverKey
}

// keysLeft is the number of handed keys that were not destroyed.
func (x *hoRig) keysLeft() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	n := 0
	for _, k := range x.given {
		if k.Present() {
			n++
		}
	}
	return n
}

func newHandoverRig(t *testing.T, o options) *hoRig {
	t.Helper()
	x := &hoRig{store: maintest.NewStore(t), handed: map[string]handed{}}
	c, err := handover.New(handover.Options{
		Release: "test", CAFile: x.store.CAFile(t.TempDir()), ConnectTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second,
		RefusedHosts:   []string{"https://packages.example.com", "https://quader.example.com"},
		OnKeyDestroyed: func() { x.destroyed.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	x.client, o.handover = c, c
	x.siteHarness = newSiteHarness(t, o)
	x.setSwitch(true)
	via := x.main.doFn
	x.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) { return x.answer(via, f) }
	return x
}

// setSwitch sets the registration's switch and lets the link read it from the handshake.
func (x *hoRig) setSwitch(on bool) {
	x.main.mu.Lock()
	x.main.hs.Handover = on
	x.main.mu.Unlock()
	x.link.RequestHandshake()
	x.tick()
}

// hand makes the site serve blob at path and the main installation hand it over from the store, plain or encrypted, and
// returns the store's object.
func (x *hoRig) hand(t *testing.T, path string, blob []byte, encrypted bool) string {
	t.Helper()
	object := "/quader-blobs/sha256/" + sha(blob)
	h := handed{plain: blob, object: object, stored: int64(len(blob))}
	stored := blob
	if encrypted {
		stored, h.raw = sealBlob(t, blob)
		h.stored = int64(len(stored))
	}
	x.store.Put(object, stored)
	x.site.mu.Lock()
	x.site.artifacts[path] = blob
	x.site.mu.Unlock()
	x.mu.Lock()
	x.handed[path] = h
	x.mu.Unlock()
	return object
}

// sealBlob is the stored form of plain under a new data key, and the key's bytes.
func sealBlob(t *testing.T, plain []byte) (stored, raw []byte) {
	t.Helper()
	key, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sec, err := blobcrypt.NewSection(key, seed, bytes.NewReader(plain), 0, int64(len(plain)), &blobcrypt.Final{Size: int64(len(plain)), Digest: sha256.Sum256(plain)})
	if err != nil {
		t.Fatal(err)
	}
	if stored, err = io.ReadAll(sec); err != nil {
		t.Fatal(err)
	}
	if raw, err = key.Bytes(); err != nil {
		t.Fatal(err)
	}
	return stored, raw
}

// closeCounter counts the close of a handover answer's body.
type closeCounter struct {
	io.Reader
	once sync.Once
	n    *atomic.Int64
}

func (c *closeCounter) Close() error { c.once.Do(func() { c.n.Add(1) }); return nil }

// answer is the main installation's answer: the site's, as a 206 of the rest for the Bastion's continuation, or as a
// handover for a forward that offered and a path of handed whose blob is within the offer (storage.md R-STOR-095 steps 1
// and 5). The handover answer is the item's 200 without its bytes, its marking with the class handover and the package.
func (x *hoRig) answer(via func(mainclient.Forward) (*mainclient.Response, error), f mainclient.Forward) (*mainclient.Response, error) {
	r, err := via(f)
	if err != nil || r.Status != http.StatusOK || r.Marking.Class != iface.ClassArtifact {
		return r, err
	}
	r.Marking.Package = hoPkg
	x.mu.Lock()
	b, ok := x.handed[f.Request.URL.Path]
	mutate, whole, rangeETag := x.mutate, x.wholeRange, x.rangeETag
	x.mu.Unlock()
	if f.RangeFrom > 0 && whole {
		return r, nil
	}
	if f.RangeFrom > 0 {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		hdr := r.Header.Clone()
		hdr.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", f.RangeFrom, len(body)-1, len(body)))
		hdr.Set("Content-Length", strconv.FormatInt(int64(len(body))-f.RangeFrom, 10))
		if rangeETag != "" {
			hdr.Set("Etag", rangeETag)
		}
		return &mainclient.Response{Status: http.StatusPartialContent, Header: hdr, Body: io.NopCloser(bytes.NewReader(body[f.RangeFrom:])), Marking: r.Marking, Marked: true}, nil
	}
	if !ok || f.Offer.MaxSize < int64(len(b.plain)) {
		return r, nil
	}
	_ = r.Body.Close()
	h := iface.Handover{
		Link: x.store.Link(b.object), ExpiresAt: x.clock.now().Add(time.Minute), Connection: "default", SHA256: sha(b.plain),
		Size: int64(len(b.plain)), StoredSize: b.stored, Encrypted: b.raw != nil,
	}
	if b.raw != nil {
		if h.DataKey, err = iface.NewHandoverKey(b.raw); err != nil {
			return nil, err
		}
		x.mu.Lock()
		x.given = append(x.given, h.DataKey)
		x.mu.Unlock()
	}
	if mutate != nil {
		mutate(&h)
	}
	hdr := r.Header.Clone()
	hdr.Set("Content-Length", "0")
	mk := r.Marking
	mk.Class = iface.ClassHandover
	x.answers.Add(1)
	return &mainclient.Response{Status: http.StatusOK, Header: hdr, Body: &closeCounter{Reader: strings.NewReader(""), n: &x.closed}, Marking: mk, Marked: true, Handover: &h}, nil
}

// forwards returns the requests of kind forward or conditional the main installation received, in order.
func (x *hoRig) forwards() []recorded {
	var out []recorded
	for _, r := range x.main.requests() {
		if r.Kind == mainclient.KindForward || r.Kind == mainclient.KindConditional {
			out = append(out, r)
		}
	}
	return out
}

func (x *hoRig) lastForward(t *testing.T) recorded {
	t.Helper()
	fw := x.forwards()
	if len(fw) == 0 {
		t.Fatal("no forward")
	}
	return fw[len(fw)-1]
}

func (x *hoRig) handovers(outcome string) float64 {
	return readCounter(x.m.Handovers.WithLabelValues(outcome))
}

func (x *hoRig) fillBytes(source string) float64 {
	return readCounter(x.m.FillBytes.WithLabelValues(source))
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(hoBound):
		t.Fatalf("%s did not happen", what)
	}
}

// TestR_BAS_072_TheBastionOffersToTakeAHandover: a cold GET and the Bastion's own conditional request send the offer with
// the smaller of max_object_bytes and what the spool can take; a HEAD, a probe, the conditional request of a caller that
// follows a running fill, a request while the handshake says handover: false and one while the replica cools down send
// none (the continuation and the forward that replaces a failed handover: TestR_BAS_075_ADirectReadThatFails).
func TestR_BAS_072_TheBastionOffersToTakeAHandover(t *testing.T) {
	small := func(c *configT) { c.Bastion.Cache.MaxObjectBytes, c.Bastion.Cache.SpoolMaxBytes = 512<<10, 1<<20 }
	x := newHandoverRig(t, options{cfg: []func(*configT){small}})
	auth := []string{"Authorization", bearer(x.full)}
	x.site.artifacts[hoTgz] = payload(4 << 10)
	if w := x.get(hoTgz, auth...); w.Code != http.StatusOK || x.lastForward(t).Offer.MaxSize != 512<<10 {
		t.Fatalf("a cold GET: %d, offer %+v", w.Code, x.lastForward(t).Offer)
	}
	x.clock.advance(11 * time.Minute)
	if w := x.get(hoTgz, auth...); w.Code != http.StatusOK || x.lastForward(t).Kind != mainclient.KindConditional || x.lastForward(t).Offer.MaxSize != 512<<10 {
		t.Fatalf("the Bastion's own conditional request: %d, %+v", w.Code, x.lastForward(t))
	}
	x.site.artifacts[hoTgz2] = payload(4 << 10)
	if w := x.do(http.MethodHead, hoTgz2, nil, auth...); w.Code != http.StatusOK || x.lastForward(t).Method != http.MethodHead || x.lastForward(t).Offer.MaxSize != 0 {
		t.Fatalf("a HEAD: %d, %+v", w.Code, x.lastForward(t))
	}
	x.do(http.MethodPut, hoTgz3, strings.NewReader("x"), auth...)
	for _, r := range x.main.requests() {
		if r.Probe && r.Offer.MaxSize != 0 {
			t.Fatalf("a probe offered: %+v", r)
		}
	}
	x.setSwitch(false)
	if x.get(hoTgz3, auth...); x.lastForward(t).Offer.MaxSize != 0 || readCounter(x.m.HandoverEnabled) != 0 {
		t.Fatalf("the handshake said false and the forward offered: %+v", x.lastForward(t))
	}
	x.setSwitch(true)
	if readCounter(x.m.HandoverEnabled) != 1 {
		t.Fatal("quader_bastion_handover_enabled is not the handshake's member")
	}
	x.e.handoverCooldown = time.Second
	x.e.startCooldown()
	x.site.artifacts[hoTgz4] = payload(4 << 10)
	if x.get(hoTgz4, auth...); x.lastForward(t).Offer.MaxSize != 0 || readCounter(x.m.HandoverCooldown) != 1 {
		t.Fatalf("a forward while cooling down offered: %+v", x.lastForward(t))
	}
	x.clock.advance(time.Second)
	x.clock.advance(11 * time.Minute)
	if x.get(hoTgz4, auth...); x.lastForward(t).Offer.MaxSize == 0 || readCounter(x.m.HandoverCooldown) != 0 {
		t.Fatalf("after the cooldown: %+v, gauge %v", x.lastForward(t), readCounter(x.m.HandoverCooldown))
	}

	t.Run("the spool's room is the smaller value", func(t *testing.T) {
		y := newHandoverRig(t, options{cfg: []func(*configT){func(c *configT) { c.Bastion.Cache.MaxObjectBytes, c.Bastion.Cache.SpoolMaxBytes = 2<<20, 1<<20 }}})
		y.site.artifacts[hoTgz] = payload(1 << 10)
		if y.get(hoTgz, "Authorization", bearer(y.full)); y.lastForward(t).Offer.MaxSize != 1<<20 {
			t.Fatalf("offer %+v, want the spool's 1 MiB", y.lastForward(t).Offer)
		}
	})
	t.Run("a follower's conditional request offers nothing", func(t *testing.T) {
		y := newHandoverRig(t, options{})
		blob := payload(192 << 10)
		y.site.artifacts[hoTgz] = blob
		body := newSteppedBody(blob, 16<<10)
		via := y.main.doFn
		y.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
			r, err := via(f)
			if err == nil && r.Status == http.StatusOK {
				r.Body = body
			}
			return r, err
		}
		_, aDone := serveAsync(y.harness, hoTgz, "Authorization", bearer(y.full))
		body.release(1)
		eventuallyTrue(t, hoBound, "the leader's fill", func() bool { return y.main.count(mainclient.KindForward) == 1 })
		_, bDone := serveAsync(y.harness, hoTgz, "Authorization", bearer(y.part))
		eventuallyTrue(t, hoBound, "the follower's conditional request", func() bool { return y.main.count(mainclient.KindConditional) == 1 })
		body.release(16)
		waitDone(t, aDone)
		waitDone(t, bDone)
		for _, r := range y.forwards() {
			if r.Kind == mainclient.KindConditional && r.Offer.MaxSize != 0 {
				t.Fatalf("the follower's conditional request offered: %+v", r)
			}
		}
	})
}

// TestR_BAS_074_TheDirectReadAndItsVerification: a plain blob of 192 KiB and an encrypted one are read from the store,
// committed and served. While the read runs, the handover answer's body is closed (its budget slot is free), the leader
// holds every byte but the last 64 KiB, and a second caller of the running fill follows it after its own conditional
// request, which offers nothing. What is kept is the plaintext with the marking's package, counted, under the content
// address; the store is read once; the next GET is a hit.
func TestR_BAS_074_TheDirectReadAndItsVerification(t *testing.T) {
	for _, enc := range []bool{false, true} {
		name := "plain"
		if enc {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			x := newHandoverRig(t, options{})
			blob := payload(192 << 10)
			obj := x.hand(t, hoTgz, blob, enc)
			release, reached := x.store.HoldAfter(obj, 160<<10)
			defer release()
			a, aDone := serveAsync(x.harness, hoTgz, "Authorization", bearer(x.full))
			waitClosed(t, reached, "the store's first 160 KiB")
			eventuallyTrue(t, hoBound, "the leader's first 128 KiB", func() bool { _, b := a.got(); return len(b) >= 128<<10 })
			time.Sleep(50 * time.Millisecond)
			if _, b := a.got(); len(b) != 128<<10 {
				t.Fatalf("the leader holds %d bytes while the read runs, want all but the last 64 KiB", len(b))
			}
			if x.answers.Load() != 1 || x.closed.Load() != 1 {
				t.Fatalf("%d handover answers, %d bodies closed while the read runs", x.answers.Load(), x.closed.Load())
			}
			b, bDone := serveAsync(x.harness, hoTgz, "Authorization", bearer(x.part))
			eventuallyTrue(t, hoBound, "the follower's conditional request", func() bool { return x.main.count(mainclient.KindConditional) == 1 })
			release()
			if waitDone(t, aDone) || waitDone(t, bDone) {
				t.Fatal("a caller was cut")
			}
			for who, w := range map[string]*liveWriter{"leader": a, "follower": b} {
				if st, got := w.got(); st != http.StatusOK || !bytes.Equal(got, blob) {
					t.Fatalf("%s: %d, %d bytes", who, st, len(got))
				}
			}
			fw := x.forwards()
			if len(fw) != 2 || fw[0].Offer.MaxSize == 0 || fw[1].Kind != mainclient.KindConditional || fw[1].Offer.MaxSize != 0 {
				t.Fatalf("the main installation's requests: %+v", fw)
			}
			eventuallyTrue(t, hoBound, "the entry", func() bool { return x.cache.entryCount() == 1 })
			ent, ok := x.e.loadEntry(context.Background(), hoTgz)
			if !ok || ent.Package != hoPkg || !ent.Counted || ent.Repository != "web/npm-all" || ent.SHA256 != sha(blob) || ent.Size != int64(len(blob)) {
				t.Fatalf("the entry: %+v %v", ent, ok)
			}
			if kept, ok := x.cache.object(sha(blob)); !ok || !bytes.Equal(kept, blob) {
				t.Fatal("the cache does not keep the plaintext under its content address")
			}
			if x.handovers(metrics.HandoverRead) != 1 || x.fillBytes(metrics.SourceStore) != float64(len(blob)) || x.fillBytes(metrics.SourceMain) != 0 {
				t.Fatalf("handovers read %v, fill bytes store %v main %v", x.handovers(metrics.HandoverRead), x.fillBytes(metrics.SourceStore), x.fillBytes(metrics.SourceMain))
			}
			if x.client.KeysHeld() != 0 || (enc && x.destroyed.Load() != 1) {
				t.Fatalf("keys held %d, destroyed %d", x.client.KeysHeld(), x.destroyed.Load())
			}
			before, stored := x.main.count(), len(x.store.Requests())
			if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) || x.main.count() != before || len(x.store.Requests()) != stored || stored != 1 {
				t.Fatalf("the hit: %d, %d requests, %d reads of the store", w.Code, x.main.count()-before, len(x.store.Requests()))
			}
		})
	}
	t.Run("with a cache key the artifact is kept in the cache's own layout", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "cache-key")
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		if err := os.WriteFile(keyFile, key, 0o600); err != nil {
			t.Fatal(err)
		}
		x := newHandoverRig(t, options{cfg: []func(*configT){func(c *configT) { c.Bastion.Cache.EncryptionKeyFile = keyFile }}})
		blob := payload(96 << 10)
		x.hand(t, hoTgz, blob, true)
		if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) {
			t.Fatalf("the read: %d", w.Code)
		}
		eventuallyTrue(t, hoBound, "the entry", func() bool { _, ok := x.e.loadEntry(context.Background(), hoTgz); return ok })
		x.mu.Lock()
		raw, stored := x.handed[hoTgz].raw, x.handed[hoTgz].stored
		x.mu.Unlock()
		_ = filepath.WalkDir(x.cache.dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				b, _ := os.ReadFile(p)
				if bytes.Contains(b, blob[:4096]) || bytes.Contains(b, raw) || int64(len(b)) == stored {
					t.Errorf("%s holds the plaintext, the handed key or the store's object", p)
				}
			}
			return nil
		})
		before := x.main.count()
		if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) || x.main.count() != before {
			t.Fatalf("the hit from the sealed cache: %d", w.Code)
		}
	})
}

// TestR_BAS_075_ADirectReadThatFails: a link the store refuses with 403 ends in a forward without the offer whose bytes the
// caller receives, counts fallback and starts the cooldown; a cut after 128 KiB of a plain object and a flipped third
// segment of an encrypted one continue with Range: bytes=131072- and If-Range, without the offer, and the caller receives
// the whole blob; a plain object replaced by other bytes of the same length cuts every caller before its last 64 KiB,
// keeps nothing, and the next miss of the key sends no offer.
func TestR_BAS_075_ADirectReadThatFails(t *testing.T) {
	t.Run("a refused link falls back and cools down", func(t *testing.T) {
		x := newHandoverRig(t, options{})
		x.e.handoverCooldown = time.Second
		blob := payload(64 << 10)
		x.store.SetStatus(x.hand(t, hoTgz, blob, false), http.StatusForbidden)
		if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) {
			t.Fatalf("the fallback: %d, %d bytes", w.Code, w.Body.Len())
		}
		fw := x.forwards()
		if len(fw) != 2 || fw[0].Offer.MaxSize == 0 || fw[1].Offer.MaxSize != 0 || fw[1].RangeFrom != 0 {
			t.Fatalf("the forwards: %+v", fw)
		}
		if x.handovers(metrics.HandoverFallback) != 1 || readCounter(x.m.HandoverCooldown) != 1 {
			t.Fatalf("fallback %v, cooldown %v", x.handovers(metrics.HandoverFallback), readCounter(x.m.HandoverCooldown))
		}
		x.hand(t, hoTgz2, payload(8<<10), false)
		if x.get(hoTgz2, "Authorization", bearer(x.full)); x.lastForward(t).Offer.MaxSize != 0 {
			t.Fatal("a miss offered while the replica cools down")
		}
		x.clock.advance(time.Second)
		x.hand(t, hoTgz3, payload(8<<10), false)
		if x.get(hoTgz3, "Authorization", bearer(x.full)); x.handovers(metrics.HandoverRead) != 1 || readCounter(x.m.HandoverCooldown) != 0 {
			t.Fatalf("after the cooldown: read %v, gauge %v", x.handovers(metrics.HandoverRead), readCounter(x.m.HandoverCooldown))
		}
	})
	for _, enc := range []bool{false, true} {
		name := "a cut plain object continues with a range"
		if enc {
			name = "a flipped third segment of an encrypted object continues with a range"
		}
		t.Run(name, func(t *testing.T) {
			x := newHandoverRig(t, options{})
			blob := payload(192 << 10)
			obj := x.hand(t, hoTgz, blob, enc)
			if enc {
				stored, raw := sealBlob(t, blob)
				stored[2*blobcrypt.FullSegmentSize+100] ^= 0x01
				x.store.Put(obj, stored)
				x.mu.Lock()
				x.handed[hoTgz] = handed{plain: blob, object: obj, stored: int64(len(stored)), raw: raw}
				x.mu.Unlock()
			} else {
				x.store.CutAfter(obj, 128<<10)
			}
			if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) {
				t.Fatalf("the continued fill: %d, %d bytes", w.Code, w.Body.Len())
			}
			c := x.lastForward(t)
			if len(x.forwards()) != 2 || c.RangeFrom != 131072 || c.IfRange != `"`+sha(blob)+`"` || c.Offer.MaxSize != 0 {
				t.Fatalf("the continuation: %+v", x.forwards())
			}
			if x.handovers(metrics.HandoverContinued) != 1 || x.fillBytes(metrics.SourceStore) != 128<<10 || x.fillBytes(metrics.SourceMain) != 64<<10 {
				t.Fatalf("continued %v, fill bytes store %v main %v", x.handovers(metrics.HandoverContinued), x.fillBytes(metrics.SourceStore), x.fillBytes(metrics.SourceMain))
			}
			eventuallyTrue(t, hoBound, "the entry", func() bool { return x.cache.entryCount() == 1 })
			if x.client.KeysHeld() != 0 {
				t.Fatal("a key is held after the continued fill")
			}
		})
	}
	t.Run("a value R-BAS-073 refuses falls back without a read and without the cooldown", func(t *testing.T) {
		for name, mutate := range map[string]func(*iface.Handover){
			"an expired link": func(h *iface.Handover) { h.ExpiresAt = epoch.Add(-time.Second) },
			"a blob above the offer": func(h *iface.Handover) {
				h.Size = 3 << 30
				h.StoredSize, _ = blobcrypt.StoredSize(h.Size)
			},
			"a digest that is not the ETag's": func(h *iface.Handover) { h.SHA256 = strings.Repeat("ab", 32) },
			"the Bastion's own host": func(h *iface.Handover) {
				h.Link = strings.Replace(h.Link, strings.TrimPrefix(h.Link[:strings.Index(h.Link[8:], "/")+8], "https://"), "packages.example.com:8443", 1)
			},
			"the main installation's host": func(h *iface.Handover) {
				h.Link = strings.Replace(h.Link, strings.TrimPrefix(h.Link[:strings.Index(h.Link[8:], "/")+8], "https://"), "QUADER.example.com", 1)
			},
		} {
			t.Run(name, func(t *testing.T) {
				x := newHandoverRig(t, options{})
				blob := payload(32 << 10)
				x.hand(t, hoTgz, blob, true)
				x.mutate = mutate
				if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) {
					t.Fatalf("the fallback: %d, %d bytes", w.Code, w.Body.Len())
				}
				fw := x.forwards()
				if len(fw) != 2 || fw[1].Offer.MaxSize != 0 || len(x.store.Requests()) != 0 || x.handovers(metrics.HandoverFallback) != 1 ||
					readCounter(x.m.HandoverCooldown) != 0 || x.client.KeysHeld() != 0 || x.keysLeft() != 0 {
					t.Fatalf("forwards %+v, %d reads of the store, fallback %v, cooldown %v", fw, len(x.store.Requests()), x.handovers(metrics.HandoverFallback), readCounter(x.m.HandoverCooldown))
				}
			})
		}
	})
	for name, set := range map[string]func(*hoRig){
		"a continuation answered 200 fails the fill":           func(x *hoRig) { x.wholeRange = true },
		"a continuation with another validator fails the fill": func(x *hoRig) { x.rangeETag = `"another"` }, // the right bytes, though
	} {
		t.Run(name, func(t *testing.T) {
			x := newHandoverRig(t, options{})
			blob := payload(192 << 10)
			x.store.CutAfter(x.hand(t, hoTgz, blob, false), 128<<10)
			set(x)
			a, aDone := serveAsync(x.harness, hoTgz, "Authorization", bearer(x.full))
			if !waitDone(t, aDone) {
				t.Fatal("the caller of a fill whose continuation was refused was not cut")
			}
			if _, got := a.got(); len(got) > len(blob)-64<<10 || x.cache.entryCount() != 0 || x.lastForward(t).RangeFrom != 131072 {
				t.Fatalf("%d bytes, %d entries, %+v", len(got), x.cache.entryCount(), x.lastForward(t))
			}
		})
	}
	t.Run("a replaced plain object cuts every caller and marks the key", func(t *testing.T) {
		x := newHandoverRig(t, options{})
		blob := payload(192 << 10)
		obj := x.hand(t, hoTgz, blob, false)
		bad := append([]byte(nil), blob...)
		bad[len(bad)-1] ^= 0xff
		x.store.Put(obj, bad)
		release, reached := x.store.HoldAfter(obj, 160<<10)
		a, aDone := serveAsync(x.harness, hoTgz, "Authorization", bearer(x.full))
		waitClosed(t, reached, "the store's first 160 KiB")
		b, bDone := serveAsync(x.harness, hoTgz, "Authorization", bearer(x.part))
		eventuallyTrue(t, hoBound, "the follower's conditional request", func() bool { return x.main.count(mainclient.KindConditional) == 1 })
		release()
		if !waitDone(t, aDone) || !waitDone(t, bDone) {
			t.Fatal("a caller of a fill whose digest failed was not cut")
		}
		for who, w := range map[string]*liveWriter{"leader": a, "follower": b} {
			if _, got := w.got(); len(got) > len(blob)-64<<10 {
				t.Fatalf("the %s received %d bytes, more than all but the last 64 KiB", who, len(got))
			}
		}
		if x.cache.entryCount() != 0 || x.handovers(metrics.HandoverRefused) != 1 {
			t.Fatalf("entries %d, refused %v", x.cache.entryCount(), x.handovers(metrics.HandoverRefused))
		}
		if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) || x.lastForward(t).Offer.MaxSize != 0 {
			t.Fatalf("the next miss of the marked key: %d, %+v", w.Code, x.lastForward(t))
		}
	})
}

// syncBuffer is a log sink a test reads while the engine writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// TestR_BAS_076_TheDataKeyIsWrittenNowhere: after a read that ends whole, one that fails before its first byte, one that
// fails after it, and one whose caller leaves while it runs, every handed key is destroyed (the link client's hook counts
// it) and no key is held; no file of the cache or of the spool, no record of the captured log and no metric of the test's
// registry holds a key's bytes in hexadecimal or base64.
func TestR_BAS_076_TheDataKeyIsWrittenNowhere(t *testing.T) {
	logs := &syncBuffer{}
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatal(err)
	}
	x := newHandoverRig(t, options{m: m, log: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	auth := []string{"Authorization", bearer(x.full)}
	key := func(path string) []byte { x.mu.Lock(); defer x.mu.Unlock(); return x.handed[path].raw }

	whole := payload(96 << 10)
	x.hand(t, hoTgz, whole, true)
	if w := x.get(hoTgz, auth...); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), whole) {
		t.Fatalf("a whole read: %d", w.Code)
	}
	first := payload(100 << 10)
	obj := x.hand(t, hoTgz2, first, true)
	other, otherKey := sealBlob(t, first)
	clear(otherKey)
	x.store.Put(obj, other) // a body under another key: its first segment does not open
	if w := x.get(hoTgz2, auth...); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), first) || x.handovers(metrics.HandoverFallback) != 1 {
		t.Fatalf("a failure before the first byte: %d, fallback %v", w.Code, x.handovers(metrics.HandoverFallback))
	}
	after := payload(192 << 10)
	x.store.CutAfter(x.hand(t, hoTgz3, after, true), 100<<10)
	if w := x.get(hoTgz3, auth...); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), after) || x.handovers(metrics.HandoverContinued) != 1 {
		t.Fatalf("a failure after the first byte: %d, continued %v", w.Code, x.handovers(metrics.HandoverContinued))
	}
	left := payload(160 << 10)
	release, reached := x.store.HoldAfter(x.hand(t, hoTgz4, left, true), 100<<10)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodGet, "https://packages.example.com"+hoTgz4, nil).WithContext(ctx)
	r.RemoteAddr = "203.0.113.7:4711"
	r.Header.Set("Authorization", bearer(x.full))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		x.e.ServeHTTP(newLiveWriter(), r)
	}()
	waitClosed(t, reached, "the store's first 100 KiB")
	cancel()
	waitClosed(t, done, "the caller's request to end")
	if x.client.KeysHeld() != 1 {
		t.Fatalf("the read of a caller that left holds %d keys, want its own until it ends", x.client.KeysHeld())
	}
	release()
	eventuallyTrue(t, hoBound, "the read of the caller that left to end", func() bool { return x.client.KeysHeld() == 0 })
	if got := x.destroyed.Load(); got != 4 || x.keysLeft() != 0 {
		t.Fatalf("%d keys destroyed, want 4; %d handed keys left", got, x.keysLeft())
	}
	eventuallyTrue(t, hoBound, "the fill of the caller that left to be kept", func() bool { _, ok := x.e.loadEntry(context.Background(), hoTgz4); return ok })

	var forms []string
	for _, p := range []string{hoTgz, hoTgz2, hoTgz3, hoTgz4} {
		k := key(p)
		forms = append(forms, hex.EncodeToString(k), base64.RawURLEncoding.EncodeToString(k), base64.RawStdEncoding.EncodeToString(k))
	}
	holds := func(where, text string) {
		for _, f := range forms {
			if strings.Contains(text, f) {
				t.Errorf("%s holds a data key", where)
			}
		}
	}
	_ = filepath.WalkDir(x.cache.dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			b, _ := os.ReadFile(p)
			holds(p, string(b))
			for _, p := range []string{hoTgz, hoTgz2, hoTgz3, hoTgz4} {
				if bytes.Contains(b, key(p)) {
					t.Errorf("%s holds a data key's bytes", p)
				}
			}
		}
		return nil
	})
	holds("the log", logs.String())
	if !strings.Contains(logs.String(), "a handover is replaced by the forward without the offer") {
		t.Error("the fallback was not logged")
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		holds("the metric "+f.GetName(), f.String())
	}
}

// TestR_BAS_077_AWithdrawalDuringADirectRead: a content entry for the repository that arrives while a direct read runs
// leaves the fill's caller served and the entry committed; the next caller's request for the key is a conditional request,
// and the main installation's refusal is replayed.
func TestR_BAS_077_AWithdrawalDuringADirectRead(t *testing.T) {
	x := newHandoverRig(t, options{})
	blob := payload(128 << 10)
	release, reached := x.store.HoldAfter(x.hand(t, hoTgz, blob, false), 100<<10)
	a, aDone := serveAsync(x.harness, hoTgz, "Authorization", bearer(x.full))
	waitClosed(t, reached, "the store's first 100 KiB")
	x.clock.advance(time.Second)
	x.content("c1", "web/npm-all")
	x.tick()
	x.site.mu.Lock()
	delete(x.site.artifacts, hoTgz) // withdrawn at the main installation
	x.site.mu.Unlock()
	release()
	if waitDone(t, aDone) {
		t.Fatal("the fill's caller was cut by the withdrawal")
	}
	if st, got := a.got(); st != http.StatusOK || !bytes.Equal(got, blob) {
		t.Fatalf("the fill's caller: %d, %d bytes", st, len(got))
	}
	eventuallyTrue(t, hoBound, "the entry", func() bool { return x.cache.entryCount() == 1 })
	w := x.get(hoTgz, "Authorization", bearer(x.full))
	if c := x.lastForward(t); w.Code != http.StatusNotFound || c.Kind != mainclient.KindConditional || c.INM != `"`+sha(blob)+`"` {
		t.Fatalf("the next caller: %d, %+v", w.Code, c)
	}
}
