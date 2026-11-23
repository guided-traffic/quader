package handover_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/handover"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The link client and the readers of R-BAS-074 to R-BAS-076 against the TLS store of maintest. Bodies are made in memory:
// a plain blob of up to 192 KiB, and the stored form of an encrypted one written with the format's own writer.

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i>>9)
	}
	return b
}

func hexSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// rig is a store and a link client that trusts it, with the hosts of the Bastion and of the main installation refused.
type rig struct {
	store     *maintest.Store
	client    *handover.Client
	destroyed atomic.Int64
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{store: maintest.NewStore(t)}
	r.client = r.newClient(t, r.store.CAFile(t.TempDir()))
	return r
}

func (r *rig) newClient(t *testing.T, caFile string) *handover.Client {
	t.Helper()
	c, err := handover.New(handover.Options{
		Release: "test", CAFile: caFile, ConnectTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second,
		RefusedHosts:    []string{"https://packages.example.com", "https://quader.example.com", ""},
		TrustCheckEvery: 10 * time.Millisecond,
		OnKeyDestroyed:  func() { r.destroyed.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// plain puts a plain object and returns its handover.
func (r *rig) plain(path string, body []byte) iface.Handover {
	r.store.Put(path, body)
	return iface.Handover{
		Link: r.store.Link(path), ExpiresAt: time.Now().Add(time.Minute), Connection: "default",
		SHA256: hexSum(body), Size: int64(len(body)), StoredSize: int64(len(body)),
	}
}

// sealed is the stored form of plain under a new data key, whose final segment carries the digest of digestOf (the
// plaintext itself for an honest object), and the key's raw bytes.
func sealed(t *testing.T, plain, digestOf []byte) (stored, raw []byte) {
	t.Helper()
	key, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sec, err := blobcrypt.NewSection(key, seed, bytes.NewReader(plain), 0, int64(len(plain)), &blobcrypt.Final{Size: int64(len(plain)), Digest: sha256.Sum256(digestOf)})
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

// encrypted puts the stored form of plain and returns its handover with the data key.
func (r *rig) encrypted(t *testing.T, path string, plain []byte) (iface.Handover, []byte) {
	t.Helper()
	stored, raw := sealed(t, plain, plain)
	r.store.Put(path, stored)
	return encryptedHandover(t, r.store.Link(path), plain, stored, raw), raw
}

func encryptedHandover(t *testing.T, link string, plain, stored, raw []byte) iface.Handover {
	t.Helper()
	k, err := iface.NewHandoverKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return iface.Handover{
		Link: link, ExpiresAt: time.Now().Add(time.Minute), Connection: "default", SHA256: hexSum(plain),
		Size: int64(len(plain)), StoredSize: int64(len(stored)), Encrypted: true, DataKey: k,
	}
}

// readAll reads a read to its end and closes it.
func readAll(rd *handover.Read) ([]byte, error) {
	defer func() { _ = rd.Close() }()
	return io.ReadAll(rd)
}

func causeOf(t *testing.T, err error) *handover.Error {
	t.Helper()
	var he *handover.Error
	if !errors.As(err, &he) {
		t.Fatalf("error %v is not a *handover.Error", err)
	}
	return he
}

// TestR_BAS_074_TheLinkClientSendsNothingOfQuaders: the one GET of the link carries the User-Agent of the Bastion and no
// credential, no cookie, no header of the interface, no forwarded header and no trace context, whatever the caller's request
// held; the link's query reaches the store unchanged.
func TestR_BAS_074_TheLinkClientSendsNothingOfQuaders(t *testing.T) {
	r := newRig(t)
	body := payload(4 << 10)
	h := r.plain("/quader-blobs/sha256/"+hexSum(body), body)
	ctx := context.Background()
	rd, err := r.client.Open(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readAll(rd); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read %d bytes, %v", len(got), err)
	}
	reqs := r.store.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodGet || !strings.Contains(h.Link, reqs[0].RawQuery) {
		t.Fatalf("requests %+v", reqs)
	}
	hdr := reqs[0].Header
	if hdr.Get("User-Agent") != "quader-bastion/test" {
		t.Errorf("User-Agent %q", hdr.Get("User-Agent"))
	}
	for name := range hdr {
		switch c := http.CanonicalHeaderKey(name); {
		case c == "User-Agent", c == "Accept-Encoding" && hdr.Get(c) == "identity":
		default:
			t.Errorf("the link client sent the header %s: %v", c, hdr[name])
		}
	}
}

// TestR_BAS_074_TheLinkClientsRefusals: a link that is not https, that carries user information or that names a host of
// the Bastion or of the main installation (on any port) is refused without a request; a redirect is not followed; a
// 403 or 5xx of the store and another length are failures of the store, which start the cooldown, a 404 is not; an
// address that refuses the connection and a certificate the client does not trust are failures of the network. No error
// text holds the link's query.
func TestR_BAS_074_TheLinkClientsRefusals(t *testing.T) {
	r := newRig(t)
	body := payload(2 << 10)
	path := "/quader-blobs/sha256/" + hexSum(body)
	ok := r.plain(path, body)
	closed := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		return "https://" + addr + path + "?X-Amz-Signature=secret"
	}()
	type c struct {
		link    string
		setup   func()
		cause   handover.Cause
		request bool
	}
	cases := map[string]c{
		"an http link":                        {link: strings.Replace(ok.Link, "https://", "http://", 1), cause: handover.CauseOther},
		"user information":                    {link: strings.Replace(ok.Link, "https://", "https://u:p@", 1), cause: handover.CauseOther},
		"the Bastion's own host on a port":    {link: "https://PACKAGES.example.com.:8443" + path + "?X-Amz-Signature=secret", cause: handover.CauseOther},
		"the main installation's host":        {link: "https://quader.example.com" + path, cause: handover.CauseOther},
		"a redirect":                          {setup: func() { r.store.Redirect(path, "https://quader.example.com/files/x") }, cause: handover.CauseOther, request: true},
		"a 403 of the store":                  {setup: func() { r.store.SetStatus(path, http.StatusForbidden) }, cause: handover.CauseStore, request: true},
		"a 503 of the store":                  {setup: func() { r.store.SetStatus(path, http.StatusServiceUnavailable) }, cause: handover.CauseStore, request: true},
		"a 404 of the store":                  {setup: func() { r.store.SetStatus(path, http.StatusNotFound) }, cause: handover.CauseOther, request: true},
		"another length":                      {setup: func() { r.store.SetLength(path, int64(len(body)-1)) }, cause: handover.CauseStore, request: true},
		"a closed port":                       {link: closed, cause: handover.CauseNetwork},
		"a certificate the client distrusts":  {cause: handover.CauseNetwork},
		"a body longer than the stored size":  {setup: func() { r.store.Put(path, append(append([]byte(nil), body...), 'x')) }, cause: handover.CauseStore, request: true},
		"a body shorter than the stored size": {setup: func() { r.store.Put(path, body[:len(body)-1]) }, cause: handover.CauseStore, request: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r.store.SetStatus(path, 0)
			r.store.SetLength(path, -1)
			r.store.Redirect(path, "")
			r.store.Put(path, body)
			if tc.setup != nil {
				tc.setup()
			}
			client := r.client
			if name == "a certificate the client distrusts" {
				client = r.newClient(t, otherCAFile(t))
			}
			h := ok
			if tc.link != "" {
				h.Link = tc.link
			}
			before := len(r.store.Requests())
			rd, err := client.Open(context.Background(), h)
			if err == nil {
				_ = rd.Close()
				t.Fatal("the read was opened")
			}
			if he := causeOf(t, err); he.Cause != tc.cause || he.Cooldown() != (tc.cause != handover.CauseOther) {
				t.Errorf("cause %s (cooldown %v), want %s: %v", he.Cause, he.Cooldown(), tc.cause, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "X-Amz") {
				t.Errorf("the error names the link: %v", err)
			}
			if sent := len(r.store.Requests()) - before; (sent > 0) != tc.request || sent > 1 {
				t.Errorf("%d requests reached the store, want a request %v and at most one", sent, tc.request)
			}
		})
	}
}

// TestR_BAS_074_ThePlainAndTheEncryptedRead: a plain and an encrypted blob of three segments read whole equal the blob; on
// an encrypted copy S(size) is checked against storedSize before any request, a body under another key fails at its first
// segment, and a damaged third segment ends the read after the first two segments; a plain body whose digest differs ends
// in ErrDigest after its bytes, and before the first byte when it is one segment long; an encrypted body whose segments
// authenticate but whose plaintext is not the digest ends in ErrDigest.
func TestR_BAS_074_ThePlainAndTheEncryptedRead(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	blob := payload(192 << 10)

	h := r.plain("/plain/whole", blob)
	rd, err := r.client.Open(ctx, h)
	if err != nil || rd.Encrypted() {
		t.Fatalf("plain: %v", err)
	}
	if got, err := readAll(rd); err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("plain: %d bytes, %v", len(got), err)
	}

	he, _ := r.encrypted(t, "/enc/whole", blob)
	rd, err = r.client.Open(ctx, he)
	if err != nil || !rd.Encrypted() {
		t.Fatalf("encrypted: %v", err)
	}
	if got, err := readAll(rd); err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("encrypted: %d bytes, %v", len(got), err)
	}

	t.Run("S(size) is checked before the read", func(t *testing.T) {
		h, _ := r.encrypted(t, "/enc/size", blob)
		h.StoredSize--
		before := len(r.store.Requests())
		if _, err := r.client.Open(ctx, h); err == nil || causeOf(t, err).Cause != handover.CauseOther || len(r.store.Requests()) != before {
			t.Fatalf("a stored size that is not S(size): %v, %d requests", err, len(r.store.Requests())-before)
		}
	})
	t.Run("a body under another key fails at its first segment", func(t *testing.T) {
		h, _ := r.encrypted(t, "/enc/other", blob)
		other, raw := sealed(t, blob, blob)
		clear(raw)
		r.store.Put("/enc/other", other)
		if _, err := r.client.Open(ctx, h); err == nil || causeOf(t, err).Cause != handover.CauseOther || causeOf(t, err).Digest {
			t.Fatalf("another key: %v", err)
		}
	})
	t.Run("a damaged third segment ends the read after two segments", func(t *testing.T) {
		stored, raw := sealed(t, blob, blob)
		stored[2*blobcrypt.FullSegmentSize+100] ^= 0x01
		r.store.Put("/enc/third", stored)
		rd, err := r.client.Open(ctx, encryptedHandover(t, r.store.Link("/enc/third"), blob, stored, raw))
		if err != nil {
			t.Fatal(err)
		}
		got, err := readAll(rd)
		if err == nil || errors.Is(err, handover.ErrDigest) || len(got) != 2*blobcrypt.SegmentSize || !bytes.Equal(got, blob[:len(got)]) {
			t.Fatalf("a damaged third segment: %d bytes, %v", len(got), err)
		}
	})
	t.Run("a plain body that is not its digest", func(t *testing.T) {
		bad := append([]byte(nil), blob...)
		bad[len(bad)-1] ^= 0xff
		h := r.plain("/plain/bad", blob)
		r.store.Put("/plain/bad", bad)
		rd, err := r.client.Open(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := readAll(rd); !errors.Is(err, handover.ErrDigest) || len(got) != len(blob) {
			t.Fatalf("a changed plain body: %d bytes, %v", len(got), err)
		}
		small := payload(10 << 10)
		h = r.plain("/plain/small", small)
		small[0] ^= 0xff
		r.store.Put("/plain/small", small)
		if _, err := r.client.Open(ctx, h); err == nil || !causeOf(t, err).Digest || !errors.Is(err, handover.ErrDigest) {
			t.Fatalf("a changed plain body of one segment: %v", err)
		}
	})
	t.Run("an encrypted body whose plaintext is not the digest", func(t *testing.T) {
		other := payload(len(blob))
		other[0] ^= 0xff
		stored, raw := sealed(t, other, blob) // segments of other under the key, the final segment with blob's digest
		r.store.Put("/enc/digest", stored)
		rd, err := r.client.Open(ctx, encryptedHandover(t, r.store.Link("/enc/digest"), blob, stored, raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readAll(rd); !errors.Is(err, handover.ErrDigest) {
			t.Fatalf("the plaintext's digest: %v", err)
		}
	})
	t.Run("a body cut after its first segment", func(t *testing.T) {
		h := r.plain("/plain/cut", blob)
		r.store.CutAfter("/plain/cut", 100<<10)
		rd, err := r.client.Open(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		got, err := readAll(rd)
		if err == nil || errors.Is(err, handover.ErrDigest) || len(got) > 100<<10 || !bytes.Equal(got, blob[:len(got)]) {
			t.Fatalf("a cut body: %d bytes, %v", len(got), err)
		}
	})
}

// TestR_BAS_076_TheKeyIsDestroyedAtEveryEnd: the handed key is destroyed when Open returns, and the format's key when the
// read ends whole, when it fails before its first byte, when it breaks off after it and when its context is cancelled; no
// error text and no rendering holds the key.
func TestR_BAS_076_TheKeyIsDestroyedAtEveryEnd(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	blob := payload(192 << 10)
	var keys [][]byte
	check := func(t *testing.T, what string, h iface.Handover, err error, want int64) {
		t.Helper()
		if h.DataKey.Present() {
			t.Errorf("%s: the handed key is present after Open", what)
		}
		if got := r.destroyed.Load(); got != want {
			t.Errorf("%s: %d keys destroyed, want %d", what, got, want)
		}
		if err == nil {
			return
		}
		for _, k := range keys {
			for _, form := range []string{hex.EncodeToString(k), base64.RawURLEncoding.EncodeToString(k)} {
				if strings.Contains(err.Error(), form) {
					t.Errorf("%s: the error holds the key", what)
				}
			}
		}
	}

	h, raw := r.encrypted(t, "/k/whole", blob)
	keys = append(keys, raw)
	rd, err := r.client.Open(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if r.client.KeysHeld() != 1 {
		t.Fatalf("keys held during a read: %d", r.client.KeysHeld())
	}
	_, err = readAll(rd)
	check(t, "a whole read", h, err, 1)

	stored, raw := sealed(t, blob, blob)
	keys = append(keys, raw)
	stored[10] ^= 0x01
	r.store.Put("/k/first", stored)
	h = encryptedHandover(t, r.store.Link("/k/first"), blob, stored, raw)
	_, err = r.client.Open(ctx, h)
	check(t, "a failure before the first byte", h, err, 2)

	h, raw = r.encrypted(t, "/k/cut", blob)
	keys = append(keys, raw)
	r.store.CutAfter("/k/cut", 70<<10)
	rd, err = r.client.Open(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	_, err = readAll(rd)
	check(t, "a break after the first byte", h, err, 3)

	h, raw = r.encrypted(t, "/k/cancel", blob)
	keys = append(keys, raw)
	release, reached := r.store.HoldAfter("/k/cancel", 100<<10)
	defer release()
	cctx, cancel := context.WithCancel(ctx)
	rd, err = r.client.Open(cctx, h)
	if err != nil {
		t.Fatal(err)
	}
	<-reached
	cancel()
	_, err = readAll(rd)
	check(t, "a cancelled read", h, err, 4)
	if r.client.KeysHeld() != 0 {
		t.Fatalf("keys held after every read ended: %d", r.client.KeysHeld())
	}
	if s := (iface.Handover{DataKey: h.DataKey}).String(); strings.Contains(s, hex.EncodeToString(raw)) {
		t.Fatal("a handover renders its key")
	}
}

// TestR_BAS_074_TheTrustIsReadAgain: the client trusts the CA file as it is now: a file that does not hold the store's
// certificate fails the read, and the same file rewritten with it serves the next read without a new client (Q-BAS-237).
func TestR_BAS_074_TheTrustIsReadAgain(t *testing.T) {
	r := newRig(t)
	file := otherCAFile(t)
	client := r.newClient(t, file)
	body := payload(1 << 10)
	h := r.plain("/trust", body)
	if _, err := client.Open(context.Background(), h); err == nil || causeOf(t, err).Cause != handover.CauseNetwork {
		t.Fatalf("an untrusted store: %v", err)
	}
	if err := os.WriteFile(file, r.store.PEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rd, err := client.Open(context.Background(), h)
		if err == nil {
			if got, err := readAll(rd); err != nil || !bytes.Equal(got, body) {
				t.Fatalf("after the rotation: %d bytes, %v", len(got), err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the rewritten CA file was not read again: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// otherCAFile writes a bundle with a self-signed certificate of its own: every httptest server presents the same test
// certificate, so a bundle that does not trust the store must hold another one.
func otherCAFile(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "another CA"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "other-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
