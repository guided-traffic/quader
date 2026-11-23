package formathost

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/malware/gate"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/repository"
)

// switches is a snapshot of the registrations whose handover is on, by principal ID.
type switches map[string]bool

func (s switches) On(id string) bool { return s[id] }

const handoverBastionID = "0192f1c4-0000-7000-8000-0000000000b1"

// handoverServe is one download through the serve point as a recognized forward of the registration dmz that offered a handover
// of up to 1 MiB, with every condition of R-STOR-095 met unless a case changes one: the registration's switch on, the marking stage
// in the chain, the links' minSize 0, the connection's link check passed. The switch of the clients' links is off: it is no
// condition of a handover (Q-STOR-345).
type handoverServe struct {
	*linkServe
	sw      switches
	rec     *bastionmain.Recognition // nil: the request carries no valid Bastion token
	marked  bool                     // the marking stage installed its record (bastionmain.WithAbsence)
	absence *bastionmain.Absence
	charge  *limits.Charge

	// The encrypted pass (R-STOR-095 steps 8 and 10): enc makes the connection encrypted and its object a sealed one under a data key
	// of its own (encrypt); keys is the blob store's CopyDataKey with what it handed out, trail the audit's recorder of the trace.
	enc   bool
	sk    *blobcrypt.StorageKey
	dk    blobcrypt.DataKey
	keys  *takenKeys
	trail *traceTrail
}

// takenKeys is the blob store's CopyDataKey with every call counted and every key kept, so that a case can see that a download that
// streams took none, and that a handed one was destroyed.
type takenKeys struct {
	store *blob.Store
	n     int
	taken []blobcrypt.DataKey
}

func (k *takenKeys) CopyDataKey(ctx context.Context, d blob.Digest, size int64, cp blob.Copy) (blobcrypt.DataKey, error) {
	k.n++
	dk, err := k.store.CopyDataKey(ctx, d, size, cp)
	if err == nil {
		k.taken = append(k.taken, dk)
	}
	return dk, err
}

// traceTrail keeps the transactional events of the case; traceRunner runs their transaction.
type traceTrail struct{ events []audit.Event }

func (r *traceTrail) RecordTx(_ context.Context, _ meta.Tx, ev audit.Event) error {
	r.events = append(r.events, ev)
	return nil
}

func (r *traceTrail) Record(context.Context, audit.Event) {}

type traceRunner struct{}

func (traceRunner) InTx(_ context.Context, fn func(meta.Tx) error) error { return fn(traceTx{}) }

type traceTx struct{}

func (traceTx) Before(func(context.Context, meta.Tx) error) {}
func (traceTx) After(func())                                {}
func (traceTx) OnRollback(func())                           {}
func (traceTx) Value(any) any                               { return nil }
func (traceTx) SetValue(any, any)                           {}

// oneStorageKey is the replica's storage key of the connection default.
type oneStorageKey struct{ sk *blobcrypt.StorageKey }

func (o oneStorageKey) Active(context.Context, string) (*blobcrypt.StorageKey, error) {
	return o.sk, nil
}

func (oneStorageKey) NameKey(string) (blobcrypt.Key, error) {
	return blobcrypt.Key{}, errors.New("no name key")
}

func (o oneStorageKey) Lookup(context.Context, string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		if id == o.sk.ID {
			return o.sk, nil
		}
		return nil, nil
	}
}

// encrypt puts the item's bytes on the connection as an encrypted object, sealed under a data key of its own and wrapped under the
// replica's storage key, with the row's digest and stored size its, and builds the replica's side again over an encrypted connection.
func (h *handoverServe) encrypt(t *testing.T) {
	t.Helper()
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	sk, err := blobcrypt.NewStorageKey("sk-default-1-0a1b2c3d", raw)
	if err != nil {
		t.Fatal(err)
	}
	key, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := blobcrypt.NewSeed(rand.Reader)
	dk := blobcrypt.DataKey{Key: key, Digest: sha256.Sum256(h.items.body), Size: int64(len(h.items.body))}
	sec, err := blobcrypt.NewSection(key, seed, bytes.NewReader(h.items.body), 0, dk.Size, &blobcrypt.Final{Size: dk.Size, Digest: dk.Digest})
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
	d := blob.Digest(dk.Digest)
	obj := blob.Key("quader", d)
	if err := h.store.Put(context.Background(), obj, bytes.NewReader(body), int64(len(body)), blob.Checksum{}, rec.Metadata()); err != nil {
		t.Fatal(err)
	}
	stored, err := blobcrypt.StoredSize(dk.Size)
	if err != nil {
		t.Fatal(err)
	}
	h.row.Digest = d
	h.row.Copies = []blob.Copy{{Connection: "default", ObjectKey: obj, StoredSize: stored, StorageKeyID: sk.ID, State: blob.CopyPresent}}
	h.enc, h.sk, h.dk = true, sk, dk
	h.relink(t, nil)
}

func newHandoverServe(t *testing.T, mutate func(*config.StorageConnection)) *handoverServe {
	t.Helper()
	c := newLinkServe(t)
	h := &handoverServe{
		linkServe: c, sw: switches{handoverBastionID: true}, marked: true,
		rec: &bastionmain.Recognition{
			Bastion: "dmz", BastionID: handoverBastionID, Interface: iface.InterfaceVersion,
			Offer: iface.HandoverOffer{MaxSize: 1 << 20}, Offered: true,
		},
	}
	h.relink(t, mutate)
	return h
}

// relink builds the replica's side again over a connection of the case, with the snapshot of the switches.
func (h *handoverServe) relink(t *testing.T, mutate func(*config.StorageConnection)) {
	t.Helper()
	p := "quader"
	el := config.StorageConnection{ID: "default", Bucket: "blobs", Prefix: &p, Endpoint: "https://" + blobtest.PresignHost, ForcePathStyle: true}
	if mutate != nil {
		mutate(&el)
	}
	el.Encrypted = el.Encrypted || h.enc
	conns, err := blob.NewConnections(blob.NewConnection(el, blob.NewGuard(h.store, config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute})))
	if err != nil {
		t.Fatal(err)
	}
	h.conns, h.met, h.trail = conns, observability.NewMetrics("web"), &traceTrail{}
	o := downloadlinks.Options{
		Connections: conns, Rule: downloadlinks.HostRuleOf([]string{"quader.example.com"}), Handovers: h.sw, Replica: "r1",
		Runner: traceRunner{}, Audit: h.trail, Metrics: downloadlinks.NewMetrics(h.met),
	}
	if h.enc {
		store, err := blob.New(blobtest.Storage(t, t.TempDir()), conns, nopRunner{}, blob.WithStorageKeys(oneStorageKey{h.sk}))
		if err != nil {
			t.Fatal(err)
		}
		h.keys = &takenKeys{store: store}
		o.DataKeys = h.keys
	}
	h.links = downloadlinks.New(o)
	s := downloadlinks.Defaults()
	s.MinSize = 0
	h.links.SetSettings(s)
	h.links.RunLinkChecks(context.Background())
}

func (h *handoverServe) serve() *httptest.ResponseRecorder {
	h.t.Helper()
	extra := h.ctx
	h.ctx = func(ctx context.Context) context.Context {
		if extra != nil {
			ctx = extra(ctx)
		}
		h.charge = limits.FromContext(ctx)
		if h.rec != nil {
			ctx = bastionmain.WithRecognition(ctx, *h.rec)
		}
		if h.marked {
			ctx, h.absence = bastionmain.WithAbsence(ctx)
		}
		return ctx
	}
	defer func() { h.ctx = extra }()
	return h.linkServe.serve()
}

// streamed reports a failure when the answer is not the file's bytes, or a handover was noted.
func (h *handoverServe) streamed(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := http.StatusOK
	if h.hdr.Get("Range") != "" {
		want = http.StatusPartialContent
	}
	if rec.Code != want || !bytes.Equal(rec.Body.Bytes(), h.items.body) || h.absence.Handover() != "" {
		t.Fatalf("%d %q, handover noted %v", rec.Code, rec.Body.String(), h.absence.Handover() != "")
	}
}

// handoverDeclined is the value of the declined offers' counter of one reason.
func (h *handoverServe) handoverDeclined(t *testing.T, reason string) float64 {
	t.Helper()
	return counter(t, h.met, "quader_storage_download_handovers_declined_total", "reason", reason)
}

// TestR_STOR_095_TheConditionsOfAHandover: with every condition met a GET of a recognized forward that offered is answered with
// the file's 200 without its bytes (Content-Length: 0, the item's headers), the value noted for the marking holds a link the
// in-memory store serves the bytes for, and the object is not opened; each condition taken away alone streams the bytes and counts
// its reason; If-None-Match answers 304, a used-up cachedBytesPerHour 429, HEAD the row's headers, none of them with a link signed.
func TestR_STOR_095_TheConditionsOfAHandover(t *testing.T) {
	t.Run("R-STOR-095 every condition met: the file's 200 without its bytes, and the noted link serves them", func(t *testing.T) {
		h := newHandoverServe(t, nil)
		rec := h.serve()
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "0" || h.items.opened != 0 {
			t.Fatalf("%d, %d bytes, Content-Length %q, opened %d", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Length"), h.items.opened)
		}
		if rec.Header().Get("ETag") != h.row.ETag() || rec.Header().Get("Content-Type") != "application/octet-stream" || rec.Header().Get("Location") != "" {
			t.Fatalf("the item's headers: %v", rec.Header())
		}
		if !h.absence.Counted() {
			t.Fatal("the 200 of a handover does not note the counted download for Quader-Bastion-Counted")
		}
		v, err := iface.DecodeHandover(h.absence.Handover())
		if err != nil {
			t.Fatalf("the noted value: %v", err)
		}
		if err := v.Check(iface.HandoverCheck{Now: time.Now(), Offer: iface.HandoverOffer{MaxSize: 1 << 20}, ETag: rec.Header().Get("ETag")}); err != nil {
			t.Fatalf("the Bastion would refuse the value: %v", err)
		}
		if v.Encrypted || v.DataKey.Present() || v.Connection != "default" || v.Size != h.row.Size || v.StoredSize != h.row.Size || v.SHA256 != h.row.Digest.Hex() {
			t.Fatalf("value %v", v)
		}
		u, _ := url.Parse(v.Link)
		for _, override := range []string{"response-content-type", "response-content-disposition", "response-cache-control"} {
			if u.Query().Has(override) {
				t.Errorf("the link asks for %s: a handover's link carries no override (R-STOR-095 step 9)", override)
			}
		}
		resp, err := h.store.Do(httptest.NewRequest(http.MethodGet, v.Link, nil))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !bytes.Equal(got, h.items.body) {
			t.Fatalf("the link answered %d %q", resp.StatusCode, got)
		}
		if len(h.trail.events) != 0 {
			t.Fatalf("a plain copy's handover wrote %d traces of a data key", len(h.trail.events))
		}
	})
	t.Run("R-STOR-095 R-STOR-096 every condition met on an encrypted connection: the value carries the copy's data key and S(size), after its trace", func(t *testing.T) {
		h := newHandoverServe(t, nil)
		h.encrypt(t)
		rec := h.serve()
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "0" || h.items.opened != 0 {
			t.Fatalf("%d, %d bytes, opened %d", rec.Code, rec.Body.Len(), h.items.opened)
		}
		v, err := iface.DecodeHandover(h.absence.Handover())
		if err != nil {
			t.Fatalf("the noted value: %v", err)
		}
		if err := v.Check(iface.HandoverCheck{Now: time.Now(), Offer: iface.HandoverOffer{MaxSize: 1 << 20}, ETag: rec.Header().Get("ETag")}); err != nil {
			t.Fatalf("the Bastion would refuse the value: %v", err)
		}
		if stored, _ := blobcrypt.StoredSize(h.row.Size); !v.Encrypted || v.StoredSize != stored || !v.DataKey.Present() {
			t.Fatalf("value %v", v)
		}
		resp, err := h.store.Do(httptest.NewRequest(http.MethodGet, v.Link, nil))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		k, _ := blobcrypt.KeyFromBytes(v.DataKey.Bytes())
		rd, err := blobcrypt.NewReader(resp.Body, blobcrypt.DataKey{Key: k, Digest: h.dk.Digest, Size: h.dk.Size}, true)
		if err != nil {
			t.Fatalf("the handed key does not open the object at the link: %v", err)
		}
		if got, err := io.ReadAll(rd); err != nil || !bytes.Equal(got, h.items.body) {
			t.Fatalf("the object read with the handed key: %v", err)
		}
		if len(h.trail.events) != 1 || h.trail.events[0].Type != audit.EventBastionDataKeyHanded || h.trail.events[0].Actor.Kind != "anonymous" ||
			h.trail.events[0].Endpoint != "default" || h.trail.events[0].Repository != linkHolder.String() {
			t.Fatalf("the trace: %+v", h.trail.events)
		}
		if h.keys.n != 1 || h.links.KeysHeld() != 0 {
			t.Fatalf("%d keys taken, %d held after the answer", h.keys.n, h.links.KeysHeld())
		}
		for _, dk := range h.keys.taken {
			if _, err := dk.Key.Bytes(); err == nil {
				t.Fatal("the format's key outlived the handover")
			}
		}
	})
	for _, tc := range []struct {
		name, reason string // reason: the declined offers' label it counts, "" for none
		mod          func(t *testing.T, h *handoverServe)
	}{
		{"no offer", "", func(_ *testing.T, h *handoverServe) { h.rec.Offered = false }},
		{"the registration's handover off", downloadlinks.HandoverDeclinedSwitchOff, func(_ *testing.T, h *handoverServe) { delete(h.sw, handoverBastionID) }},
		{"the same request without a Bastion token, with the offer's header", "", func(_ *testing.T, h *handoverServe) {
			h.rec = nil
			h.hdr.Set(iface.HeaderHandover, "accept; max-size=1048576")
		}},
		{"a forward whose answer the marking stage does not mark", "", func(_ *testing.T, h *handoverServe) { h.marked = false }},
		{"a Range", "", func(_ *testing.T, h *handoverServe) { h.hdr.Set("Range", "bytes=0-") }},
		{"a document", downloadlinks.HandoverDeclinedNotArtifact, func(_ *testing.T, h *handoverServe) { h.route.Kind = formats.Metadata }},
		{"a blob without a malware verdict under a gate that is not strict", downloadlinks.HandoverDeclinedGate, func(_ *testing.T, h *handoverServe) {
			h.malw = gate.New(gate.Options{Enabled: true})
			h.row.Malware = malware.Verdict{}
		}},
		{"a size below minSize", downloadlinks.HandoverDeclinedSize, func(_ *testing.T, h *handoverServe) {
			s := h.links.Settings()
			s.MinSize = h.row.Size + 1
			h.links.SetSettings(s)
		}},
		{"a size above the offer's max-size", downloadlinks.HandoverDeclinedSize, func(_ *testing.T, h *handoverServe) { h.rec.Offer.MaxSize = h.row.Size - 1 }},
		{"an open circuit", downloadlinks.HandoverDeclinedConnection, func(_ *testing.T, h *handoverServe) {
			h.store.SetFault(func(string, string) error {
				return &blob.StoreError{Op: "HeadObject", Kind: blob.KindTransient, Status: 503}
			})
			_, _ = h.conns.First().Objects().Head(context.Background(), "k")
			h.store.SetFault(nil)
		}},
		{"a connection whose link check failed", downloadlinks.HandoverDeclinedConnection, func(_ *testing.T, h *handoverServe) {
			h.conns.First().SetLinkCheck(blob.LinkCheck{Ran: true, Detail: "the link answered 403 with 0 bytes"})
			h.links.SetSettings(h.links.Settings())
		}},
		{"a connection whose link check has not answered", downloadlinks.HandoverDeclinedConnection, func(_ *testing.T, h *handoverServe) {
			h.conns.First().SetLinkCheck(blob.LinkCheck{})
			h.links.SetSettings(h.links.Settings())
		}},
		{"a public address on a host of Quader", downloadlinks.HandoverDeclinedConnection, func(_ *testing.T, h *handoverServe) {
			s := h.links.Settings()
			s.PublicAddresses = map[string]string{"default": "https://s3.quader.example.com"}
			h.links.SetSettings(s)
		}},
		{"a copy on a connection the replica does not configure", downloadlinks.HandoverDeclinedConnection, func(_ *testing.T, h *handoverServe) {
			h.row.Copies[0].Connection = "detached"
		}},
		{"a credential that expires in 3 s", downloadlinks.HandoverDeclinedCredential, func(_ *testing.T, h *handoverServe) {
			h.store.SetCredentialExpiry(time.Now().Add(3 * time.Second))
		}},
		{"a signing error", downloadlinks.HandoverDeclinedSigning, func(_ *testing.T, h *handoverServe) {
			h.store.SetFault(func(op, _ string) error {
				if op == "presign" {
					return errors.New("no signer")
				}
				return nil
			})
		}},
	} {
		// Each condition taken away alone, on a plain connection and on an encrypted one: a condition that guards a plain handover
		// guards the data key as well, so a forward that streams takes no key, sends no HeadObject for one and writes no trace.
		for _, enc := range []bool{false, true} {
			name := "R-STOR-095 " + tc.name + " streams the bytes"
			if enc {
				name = "R-STOR-095 R-STOR-096 on an encrypted connection, " + tc.name + " streams the bytes and takes no data key"
			}
			t.Run(name, func(t *testing.T) {
				h := newHandoverServe(t, nil)
				if enc {
					h.encrypt(t)
				}
				tc.mod(t, h)
				h.streamed(t, h.serve())
				for _, r := range downloadlinks.HandoverDeclinedReasons() {
					want := 0.0
					if r == tc.reason {
						want = 1
					}
					if got := h.handoverDeclined(t, r); got != want {
						t.Errorf("declined for %s: %v, want %v", r, got, want)
					}
				}
				if enc && (h.keys.n != 0 || len(h.trail.events) != 0 || h.links.KeysHeld() != 0) {
					t.Fatalf("a download that streams took %d data keys and wrote %d traces", h.keys.n, len(h.trail.events))
				}
			})
		}
	}
	for _, tc := range []struct {
		name string
		mod  func(h *handoverServe)
	}{
		{"a route whose clients check no digest", func(h *handoverServe) { h.route.ClientDigest = formats.ClientDigest{} }},
		{"a repository that opted out of the clients' links (Q-STOR-345)", func(h *handoverServe) {
			h.repos.by[linkHolder] = repositoryOptedOut(h.repos.by[linkHolder])
		}},
		{"the switch of the clients' links on", func(h *handoverServe) {
			s := h.links.Settings()
			s.Enabled = true
			h.links.SetSettings(s)
		}},
	} {
		t.Run("R-STOR-095 "+tc.name+" is handed over, and a Bastion's forward never gets a client's link", func(t *testing.T) {
			h := newHandoverServe(t, nil)
			tc.mod(h)
			rec := h.serve()
			if rec.Code != http.StatusOK || rec.Body.Len() != 0 || h.absence.Handover() == "" || rec.Header().Get("Location") != "" {
				t.Fatalf("%d %q", rec.Code, rec.Body.String())
			}
		})
	}
	t.Run("R-STOR-095 R-STOR-072 the link lives linkLifeSeconds, bounded by the signing credential", func(t *testing.T) {
		h := newHandoverServe(t, nil)
		h.store.SetCredentialExpiry(time.Now().Add(20 * time.Second))
		if rec := h.serve(); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Fatalf("%d", rec.Code)
		}
		v, err := iface.DecodeHandover(h.absence.Handover())
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(v.Link)
		if life, _ := strconv.Atoi(u.Query().Get("X-Amz-Expires")); life < 1 || life > 20 || time.Until(v.ExpiresAt) > 20*time.Second {
			t.Fatalf("X-Amz-Expires %q, expires in %s: the credential ends in 20 s", u.Query().Get("X-Amz-Expires"), time.Until(v.ExpiresAt))
		}
	})
	for _, mode := range []limits.Mode{limits.ModeEnforce, limits.ModeAudit} {
		t.Run("R-STOR-095 Q-STOR-343 a caller with a speed limit is handed over in "+string(mode)+", charged its size at the issue", func(t *testing.T) {
			h := newHandoverServe(t, nil)
			h.mode, h.figs[limits.CachedBytesPerSecond] = mode, 1_000_000
			if rec := h.serve(); rec.Code != http.StatusOK || h.absence.Handover() == "" {
				t.Fatalf("%d: a speed limit kept the download from a handover", rec.Code)
			}
			if cached, _ := h.charge.Totals(); cached != h.row.Size {
				t.Fatalf("the charge holds %d cached bytes, want %d", cached, h.row.Size)
			}
		})
	}
	t.Run("R-STOR-095 HEAD answers the row's headers without a store request", func(t *testing.T) {
		h := newHandoverServe(t, nil)
		h.method = http.MethodHead
		before := h.store.Count("presign")
		if rec := h.serve(); rec.Code != http.StatusOK || h.store.Count("presign") != before || h.absence.Handover() != "" ||
			rec.Header().Get("Content-Length") != strconv.FormatInt(h.row.Size, 10) {
			t.Fatalf("%d, presigned %d", rec.Code, h.store.Count("presign")-before)
		}
	})
	t.Run("R-STOR-095 If-None-Match with the content address answers 304", func(t *testing.T) {
		h := newHandoverServe(t, nil)
		h.hdr.Set("If-None-Match", h.row.ETag())
		before := h.store.Count("presign")
		if rec := h.serve(); rec.Code != http.StatusNotModified || h.store.Count("presign") != before || h.absence.Handover() != "" {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("R-STOR-095 a used-up cachedBytesPerHour answers 429 and no handover", func(t *testing.T) {
		h := newHandoverServe(t, nil)
		h.figs[limits.CachedBytesPerHour], h.used = 1_000, true
		before := h.store.Count("presign")
		if rec := h.serve(); rec.Code != http.StatusTooManyRequests || h.store.Count("presign") != before || h.absence.Handover() != "" {
			t.Fatalf("%d %s", rec.Code, rec.Header().Get("X-Code"))
		}
	})
	t.Run("R-STOR-095 a system read is never a handover", func(t *testing.T) {
		h := newHandoverServe(t, nil)
		host, err := New(Options{Runner: nopRunner{}, Decider: &fakeDecider{}, Items: h.items, Packages: mustPackages(t), Repos: h.repos, Links: h.links})
		if err != nil {
			t.Fatal(err)
		}
		route := h.route
		ctx, _ := bastionmain.WithAbsence(bastionmain.WithRecognition(context.Background(), *h.rec))
		rq := &formats.Request{
			Request: httptest.NewRequest(http.MethodGet, "/api/probe/web/probe-local/a/-/a-1.0.0.tgz", nil).WithContext(ctx),
			Repo:    &repository.Repository{ID: "3", Name: linkHolder, Class: repository.Local, Format: "probe"},
			Route:   &route, Subject: authz.Subject{Name: "vuln-scanner-trivy", Kind: principal.KindSystem, Endpoint: "default"},
		}
		var o edge.ServeOptions
		itemStore{h: host}.handoverOptions(ctx, rq, h.row, true, &o)
		if o.Handover != nil || h.handoverDeclined(t, downloadlinks.HandoverDeclinedNotArtifact) != 1 {
			t.Fatal("a system read was offered a handover")
		}
	})
}

// TestR_STOR_095_WhatAHandoverCounts: one handover adds one download to the statistics with the delivery handover, the registration,
// the connection, whether it is encrypted and the link's expiry for the audit event, the item's size to the charge's cached bytes,
// and one to the handovers of the registration of the kind link, or link_and_key for an encrypted copy (R-STOR-095 step 11).
func TestR_STOR_095_WhatAHandoverCounts(t *testing.T) {
	for _, enc := range []bool{false, true} {
		kind := downloadlinks.HandoverKindLink
		if enc {
			kind = downloadlinks.HandoverKindLinkAndKey
		}
		t.Run("R-STOR-095 a handover of the kind "+kind, func(t *testing.T) {
			h := newHandoverServe(t, nil)
			if enc {
				h.encrypt(t)
			}
			if rec := h.serve(); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
				t.Fatalf("%d", rec.Code)
			}
			if len(h.items.served) != 1 {
				t.Fatalf("served %+v", h.items.served)
			}
			e := h.items.served[0]
			if e.Delivery != items.DeliveryHandover || e.Bastion != "dmz" || e.Connection != "default" || e.Encrypted != enc || e.LinkExpiresAt.IsZero() ||
				!e.CountStats || e.Endpoint != "default" || e.Actor.Kind != "anonymous" {
				t.Fatalf("served %+v", e)
			}
			if cached, _ := h.charge.Totals(); cached != h.row.Size {
				t.Fatalf("the charge holds %d cached bytes, want %d", cached, h.row.Size)
			}
			mfs, err := h.met.Registry().Gather()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, mf := range mfs {
				if mf.GetName() != "quader_storage_download_handovers_total" {
					continue
				}
				for _, s := range mf.GetMetric() {
					labels := map[string]string{}
					for _, l := range s.GetLabel() {
						labels[l.GetName()] = l.GetValue()
					}
					found = labels["bastion"] == "dmz" && labels["kind"] == kind && s.GetCounter().GetValue() == 1
				}
			}
			if !found {
				t.Fatalf("quader_storage_download_handovers_total{bastion=\"dmz\",kind=%q} is not 1", kind)
			}
			if errs := observability.CheckFamilies(mfs); len(errs) > 0 {
				t.Fatalf("the families break R-OPS-007: %v", errs)
			}
		})
	}
}
