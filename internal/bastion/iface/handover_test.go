package iface_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The interface's part of the handover per blob (bastion.md R-BAS-072, R-BAS-073, R-BAS-076): the offer, the value with
// every check of R-BAS-073, the class, and a data key that no rendering shows.

const (
	handoverDigest = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	handoverLink   = "https://objects.example.com/quader-blobs/quader/sha256/9f/86/" + handoverDigest +
		"?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=60&X-Amz-SignedHeaders=host&X-Amz-Signature=5ec7e75161a1b2c3d4e5f60718293a4b"
)

var handoverNow = time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

// testKey is the data key of the encrypted examples: 32 bytes whose hexadecimal and base64url forms are searched for.
func testKey(t *testing.T) (iface.HandoverKey, []byte) {
	t.Helper()
	raw := make([]byte, iface.HandoverKeyBytes)
	for i := range raw {
		raw[i] = byte(0xA0 + i)
	}
	k, err := iface.NewHandoverKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return k, raw
}

func plainHandover() iface.Handover {
	return iface.Handover{
		Link: handoverLink, ExpiresAt: handoverNow.Add(time.Minute), Connection: "default", SHA256: handoverDigest,
		Size: 5242880, StoredSize: 5242880,
	}
}

func encryptedHandover(t *testing.T) iface.Handover {
	h := plainHandover()
	h.Encrypted, h.StoredSize = true, 5245188 // S(5,242,880) = 5,242,880 + 28·80 + 68
	h.DataKey, _ = testKey(t)
	return h
}

// wireOf builds a value from raw JSON members, for the values EncodeHandover refuses to write.
func wireOf(t *testing.T, members map[string]any) string {
	t.Helper()
	b, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// plainMembers are the members of plainHandover as JSON values.
func plainMembers() map[string]any {
	return map[string]any{
		"link": handoverLink, "expiresAt": handoverNow.Add(time.Minute).Format(time.RFC3339), "connection": "default",
		"sha256": handoverDigest, "size": 5242880, "storedSize": 5242880, "encrypted": false,
	}
}

func encryptedMembers(key []byte) map[string]any {
	m := plainMembers()
	m["encrypted"], m["storedSize"], m["dataKey"] = true, 5245188, base64.RawURLEncoding.EncodeToString(key)
	return m
}

func with(m map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == nil {
			delete(out, kv[i].(string))
			continue
		}
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// TestR_BAS_073_TheHandoverValue: a plain and an encrypted value round-trip through the header's form, with exactly the
// members of R-BAS-073; every malformed value the requirement lists, and the others DecodeHandover refuses, is refused; the
// encoder refuses to write what the decoder refuses; handover is a valid class.
func TestR_BAS_073_TheHandoverValue(t *testing.T) {
	_, raw := testKey(t)
	for name, h := range map[string]iface.Handover{"plain": plainHandover(), "encrypted": encryptedHandover(t)} {
		hdr := http.Header{}
		if err := h.Set(hdr); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		back, ok, err := iface.HandoverFrom(hdr)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", name, ok, err)
		}
		if back.Link != h.Link || !back.ExpiresAt.Equal(h.ExpiresAt) || back.Connection != h.Connection || back.SHA256 != h.SHA256 ||
			back.Size != h.Size || back.StoredSize != h.StoredSize || back.Encrypted != h.Encrypted || !bytes.Equal(back.DataKey.Bytes(), h.DataKey.Bytes()) {
			t.Errorf("%s: %+v came back as %+v", name, h, back)
		}
		body, err := base64.RawURLEncoding.DecodeString(hdr.Get(iface.HeaderHandover))
		if err != nil {
			t.Fatal(err)
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(body, &members); err != nil {
			t.Fatal(err)
		}
		want := []string{"connection", "encrypted", "expiresAt", "link", "sha256", "size", "storedSize"}
		if h.Encrypted {
			want = append(want, "dataKey")
			if got := string(members["dataKey"]); got != `"`+base64.RawURLEncoding.EncodeToString(raw)+`"` || len(got) != 43+2 {
				t.Errorf("the data key is written %s", got)
			}
		}
		if len(members) != len(want) {
			t.Errorf("%s: members %v, want %v", name, keys(members), want)
		}
		for _, k := range want {
			if _, ok := members[k]; !ok {
				t.Errorf("%s: member %q missing", name, k)
			}
		}
	}
	if _, ok, err := iface.HandoverFrom(http.Header{}); ok || err != nil {
		t.Fatalf("an answer without the header: ok=%v err=%v", ok, err)
	}
	v, _ := iface.EncodeHandover(plainHandover())
	if _, _, err := iface.HandoverFrom(http.Header{iface.HeaderHandover: {v, v}}); !errors.Is(err, iface.ErrHandover) {
		t.Fatalf("two values: %v", err)
	}

	short := raw[:31]
	refused := map[string]string{
		// The cases R-BAS-073's criterion names.
		"an http link":                     wireOf(t, with(plainMembers(), "link", strings.Replace(handoverLink, "https:", "http:", 1))),
		"a data key of 31 bytes":           wireOf(t, with(encryptedMembers(raw), "dataKey", base64.RawURLEncoding.EncodeToString(short))),
		"a data key on a plain copy":       wireOf(t, with(plainMembers(), "dataKey", base64.RawURLEncoding.EncodeToString(raw))),
		"a digest of 63 digits":            wireOf(t, with(plainMembers(), "sha256", handoverDigest[:63])),
		"a digest in upper case":           wireOf(t, with(plainMembers(), "sha256", strings.ToUpper(handoverDigest))),
		"a digest that is not hexadecimal": wireOf(t, with(plainMembers(), "sha256", "z"+handoverDigest[1:])),
		"an encrypted storedSize of size":  wireOf(t, with(encryptedMembers(raw), "storedSize", 5242880)),
		"an encrypted storedSize off by 1": wireOf(t, with(encryptedMembers(raw), "storedSize", 5245189)),
		"more than 8 KiB":                  wireOf(t, with(plainMembers(), "link", handoverLink+"&pad="+strings.Repeat("a", 6200))),
		// The other members' rules.
		"a relative link":                    wireOf(t, with(plainMembers(), "link", "/quader-blobs/x")),
		"a link without a host":              wireOf(t, with(plainMembers(), "link", "https:///quader-blobs/x")),
		"a link with user information":       wireOf(t, with(plainMembers(), "link", "https://user:pw@objects.example.com/x")),
		"a link with a fragment":             wireOf(t, with(plainMembers(), "link", handoverLink+"#f")),
		"an opaque link":                     wireOf(t, with(plainMembers(), "link", "https:objects.example.com")),
		"no expiry":                          wireOf(t, with(plainMembers(), "expiresAt", nil)),
		"a connection in upper case":         wireOf(t, with(plainMembers(), "connection", "Default")),
		"a connection of one character":      wireOf(t, with(plainMembers(), "connection", "d")),
		"no connection":                      wireOf(t, with(plainMembers(), "connection", nil)),
		"a negative size":                    wireOf(t, with(plainMembers(), "size", -1, "storedSize", -1)),
		"a plain storedSize other than size": wireOf(t, with(plainMembers(), "storedSize", 5242881)),
		"an encrypted copy without a key":    wireOf(t, with(encryptedMembers(raw), "dataKey", nil)),
		"an empty data key":                  wireOf(t, with(encryptedMembers(raw), "dataKey", "")),
		"a data key with padding":            wireOf(t, with(encryptedMembers(raw), "dataKey", base64.URLEncoding.EncodeToString(raw))),
		"a data key in standard base64":      wireOf(t, with(encryptedMembers(raw), "dataKey", strings.Repeat("+", 43))),
		"a data key that is no string":       wireOf(t, with(encryptedMembers(raw), "dataKey", 7)),
		"a data key with stray bits":         wireOf(t, with(encryptedMembers(raw), "dataKey", strayBits(base64.RawURLEncoding.EncodeToString(raw)))),
		"a value with stray bits":            strayBits(padded2(wireOf(t, plainMembers()))),
		"a size above 4 TiB, encrypted":      wireOf(t, with(encryptedMembers(raw), "size", int64(1)<<43, "storedSize", int64(1)<<43)),
		"an unknown member":                  wireOf(t, with(plainMembers(), "extra", 1)),
		"a size that is no number":           wireOf(t, with(plainMembers(), "size", "5242880")),
		"trailing data":                      base64.RawURLEncoding.EncodeToString(append(mustJSON(t, plainMembers()), []byte(" {}")...)),
		"broken JSON":                        base64.RawURLEncoding.EncodeToString(mustJSON(t, plainMembers())[:40]),
		"base64 with padding":                base64.URLEncoding.EncodeToString(padded(mustJSON(t, plainMembers()))),
		"standard base64":                    "+/" + wireOf(t, plainMembers()),
		"an empty value":                     "",
	}
	for name, v := range refused {
		if _, err := iface.DecodeHandover(v); !errors.Is(err, iface.ErrHandover) {
			t.Errorf("%s: DecodeHandover = %v, want ErrHandover", name, err)
		}
	}

	for name, mutate := range map[string]func(*iface.Handover){
		"an http link":                    func(h *iface.Handover) { h.Link = "http://objects.example.com/x" },
		"a data key on a plain copy":      func(h *iface.Handover) { h.DataKey, _ = testKey(t) },
		"an encrypted copy without a key": func(h *iface.Handover) { h.Encrypted, h.StoredSize = true, 5245188 },
		"an encrypted storedSize of size": func(h *iface.Handover) { h.Encrypted, h.DataKey = true, encryptedHandover(t).DataKey },
		"more than 8 KiB":                 func(h *iface.Handover) { h.Link += "&pad=" + strings.Repeat("a", 6200) },
		"a destroyed key": func(h *iface.Handover) {
			*h = encryptedHandover(t)
			h.DataKey.Destroy()
		},
	} {
		h := plainHandover()
		mutate(&h)
		if _, err := iface.EncodeHandover(h); !errors.Is(err, iface.ErrHandover) {
			t.Errorf("%s: EncodeHandover = %v, want ErrHandover", name, err)
		}
	}
	if !iface.ClassHandover.Valid() {
		t.Error("handover is not a valid class")
	}
	if m, ok, err := iface.ParseMarking(http.Header{iface.HeaderClass: {"handover"}}); err != nil || !ok || m.Class != iface.ClassHandover {
		t.Errorf("ParseMarking of class handover: %+v %v %v", m, ok, err)
	}
}

// TestR_BAS_073_TheChecksBeforeTheRead: the checks that depend on the moment and the request: the link's expiry, the offered
// size, the ETag's content address and the hosts of the Bastion and the main installation.
func TestR_BAS_073_TheChecksBeforeTheRead(t *testing.T) {
	good := iface.HandoverCheck{
		Now: handoverNow, Offer: iface.HandoverOffer{MaxSize: 5242880}, ETag: `"sha256:` + handoverDigest + `"`,
		RefusedHosts: []string{"packages.example.com", "quader.example.com", "quader-packages.quader.svc"},
	}
	for name, h := range map[string]iface.Handover{"plain": plainHandover(), "encrypted": encryptedHandover(t)} {
		if err := h.Check(good); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := plainHandover().Check(with2(good, func(c *iface.HandoverCheck) { c.ETag = `"` + handoverDigest + `"` })); err != nil {
		t.Fatalf("the ETag of a file: %v", err)
	}
	for name, mutate := range map[string]func(*iface.HandoverCheck){
		"an expired link":            func(c *iface.HandoverCheck) { c.Now = handoverNow.Add(time.Minute) },
		"no clock":                   func(c *iface.HandoverCheck) { c.Now = time.Time{} },
		"a blob above the offer":     func(c *iface.HandoverCheck) { c.Offer.MaxSize = 5242879 },
		"no offer":                   func(c *iface.HandoverCheck) { c.Offer = iface.HandoverOffer{} },
		"another digest in the ETag": func(c *iface.HandoverCheck) { c.ETag = `"sha256:` + strings.Repeat("0", 64) + `"` },
		"a weak ETag":                func(c *iface.HandoverCheck) { c.ETag = `W/"` + handoverDigest + `"` },
		"no ETag":                    func(c *iface.HandoverCheck) { c.ETag = "" },
		"the Bastion's own host":     func(c *iface.HandoverCheck) { c.RefusedHosts = append(c.RefusedHosts, "objects.example.com") },
		"a host in upper case":       func(c *iface.HandoverCheck) { c.RefusedHosts = []string{"OBJECTS.example.com"} },
		"a host with a trailing dot": func(c *iface.HandoverCheck) { c.RefusedHosts = []string{"objects.example.com."} },
		"a host on another port too": func(c *iface.HandoverCheck) { c.RefusedHosts = []string{"objects.example.com"} },
	} {
		c := good
		c.RefusedHosts = append([]string(nil), good.RefusedHosts...)
		mutate(&c)
		h := plainHandover()
		if name == "a host on another port too" {
			h.Link = strings.Replace(h.Link, "objects.example.com", "objects.example.com:9000", 1)
		}
		if err := h.Check(c); !errors.Is(err, iface.ErrHandover) {
			t.Errorf("%s: Check = %v, want ErrHandover", name, err)
		}
	}
	empty := plainHandover()
	empty.Size, empty.StoredSize = 0, 0
	if err := empty.Check(good); err != nil {
		t.Errorf("an empty blob: %v", err)
	}
	if err := empty.Check(with2(good, func(c *iface.HandoverCheck) { c.Offer = iface.HandoverOffer{} })); !errors.Is(err, iface.ErrHandover) {
		t.Errorf("an empty blob without an offer: %v", err)
	}
	bad := plainHandover()
	bad.Link = "http://objects.example.com/x"
	if err := bad.Check(good); !errors.Is(err, iface.ErrHandover) {
		t.Errorf("Check of a value DecodeHandover refuses: %v", err)
	}
}

func with2(c iface.HandoverCheck, f func(*iface.HandoverCheck)) iface.HandoverCheck {
	f(&c)
	return c
}

// TestR_BAS_072_TheOffer: the offer has one spelling, "accept; max-size=<n>" with n from 1 to 2^63−1; anything else is
// refused, and an offer of nothing is not written.
func TestR_BAS_072_TheOffer(t *testing.T) {
	for _, n := range []int64{1, 1048576, 1<<63 - 1} {
		h := http.Header{}
		iface.HandoverOffer{MaxSize: n}.Set(h)
		o, ok, err := iface.HandoverOfferFrom(h)
		if err != nil || !ok || o.MaxSize != n || h.Get(iface.HeaderHandover) != fmt.Sprintf("accept; max-size=%d", n) {
			t.Errorf("%d: %q came back as %+v ok=%v err=%v", n, h.Get(iface.HeaderHandover), o, ok, err)
		}
	}
	for _, n := range []int64{0, -1} {
		h := http.Header{}
		iface.HandoverOffer{MaxSize: n}.Set(h)
		if len(h) != 0 {
			t.Errorf("an offer of %d is written: %v", n, h)
		}
	}
	if _, ok, err := iface.HandoverOfferFrom(http.Header{}); ok || err != nil {
		t.Errorf("no header: ok=%v err=%v", ok, err)
	}
	for _, v := range []string{
		"", "accept", "accept; max-size=", "accept; max-size=0", "accept; max-size=01", "accept; max-size=+1", "accept; max-size=-1",
		"accept; max-size=9223372036854775808", "accept; max-size=1 ", "accept;max-size=1", "accept;  max-size=1", "Accept; max-size=1",
		"accept; max-size=1; x=2", "reject; max-size=1", "accept; max-size=1x",
	} {
		if _, err := iface.ParseHandoverOffer(v); !errors.Is(err, iface.ErrHandover) {
			t.Errorf("ParseHandoverOffer(%q) = %v, want ErrHandover", v, err)
		}
	}
	if _, ok, err := iface.HandoverOfferFrom(http.Header{iface.HeaderHandover: {"accept; max-size=1", "accept; max-size=2"}}); ok || !errors.Is(err, iface.ErrHandover) {
		t.Errorf("two offers: ok=%v err=%v", ok, err)
	}
}

// TestR_BAS_076_AHandedKeyIsRenderedNowhere: the data key's bytes, in hexadecimal and in base64url, and the link's
// signature appear in no rendering of fmt, slog or JSON of the key or the handover, and the key in no error text of a
// refusal; a destroyed key is gone.
func TestR_BAS_076_AHandedKeyIsRenderedNowhere(t *testing.T) {
	key, raw := testKey(t)
	secrets := []string{hex.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw)[:40], "5ec7e75161a1b2c3d4e5f60718293a4b"}
	h := encryptedHandover(t)
	var out []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d", "%x", "%X", "%q", "%T %p"} {
		out = append(out, fmt.Sprintf(verb, key), fmt.Sprintf(verb, h), fmt.Sprintf(verb, &h), fmt.Sprintf(verb, struct{ H iface.Handover }{h}), fmt.Sprintf(verb, []iface.HandoverKey{key}))
	}
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	log.Info("handover", "value", h, "key", key, "nested", struct{ K iface.HandoverKey }{key})
	slog.New(slog.NewTextHandler(&logBuf, nil)).Info("handover", "value", h, "key", key)
	out = append(out, logBuf.String())
	j, err := json.Marshal(struct {
		H iface.Handover
		K iface.HandoverKey
	}{h, key})
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, string(j))

	wire := func(m map[string]any) string { return wireOf(t, m) }
	refusals := []string{
		wire(with(encryptedMembers(raw), "storedSize", 5242880)),
		wire(with(encryptedMembers(raw), "link", "http://objects.example.com/x")),
		wire(with(encryptedMembers(raw), "dataKey", base64.RawURLEncoding.EncodeToString(raw[:31]))),
		wire(with(encryptedMembers(raw), "dataKey", base64.RawURLEncoding.EncodeToString(raw)+"A")),
		wire(with(encryptedMembers(raw), "dataKey", "*"+base64.RawURLEncoding.EncodeToString(raw)[1:])),
		wire(with(encryptedMembers(raw), "extra", base64.RawURLEncoding.EncodeToString(raw))),
		base64.RawURLEncoding.EncodeToString(append(mustJSON(t, encryptedMembers(raw)), '\x01')),
		wire(with(plainMembers(), "dataKey", base64.RawURLEncoding.EncodeToString(raw))),
	}
	for _, v := range refusals {
		_, err := iface.DecodeHandover(v)
		if err == nil {
			t.Fatalf("a malformed value was accepted")
		}
		out = append(out, err.Error())
	}
	if err := h.Check(iface.HandoverCheck{Now: handoverNow.Add(time.Hour)}); err != nil {
		out = append(out, err.Error())
	}
	bad := h
	bad.StoredSize++
	if _, err := iface.EncodeHandover(bad); err != nil {
		out = append(out, err.Error())
	}
	if _, err := iface.NewHandoverKey(raw[:31]); err != nil {
		out = append(out, err.Error())
	}
	for _, o := range out {
		for _, s := range secrets {
			if strings.Contains(o, s) {
				t.Errorf("%q shows in %s", s, o)
			}
		}
	}
	if !strings.Contains(fmt.Sprint(h), handoverDigest) || !strings.Contains(logBuf.String(), "5245188") {
		t.Errorf("the rendering lost what it should show: %s / %s", fmt.Sprint(h), logBuf.String())
	}

	k2, _ := iface.NewHandoverKey(raw)
	copyOf := k2
	if !k2.Present() || !bytes.Equal(k2.Bytes(), raw) {
		t.Fatal("a new key does not hold its bytes")
	}
	k2.Destroy()
	if copyOf.Present() || copyOf.Bytes() != nil {
		t.Fatal("a copy of a destroyed key still holds it")
	}
	var zero iface.HandoverKey
	zero.Destroy()
	if zero.Present() || zero.Bytes() != nil {
		t.Fatal("the zero key is a key")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// strayBits sets the lowest bit of the last character of an unpadded base64url text whose length is not a multiple of 4,
// a bit no byte uses: the strict decoder refuses it, a lenient one reads the same bytes.
func strayBits(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, s[len(s)-1])
	return s[:len(s)-1] + string(alphabet[last|1])
}

// padded2 makes a value whose length is not a multiple of 4 by appending JSON whitespace before the encoding.
func padded2(v string) string {
	b, _ := base64.RawURLEncoding.DecodeString(v)
	for len(b)%3 == 0 {
		b = append(b, ' ')
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// padded appends spaces until base64 needs padding for b.
func padded(b []byte) []byte {
	for len(b)%3 == 0 {
		b = append(b, ' ')
	}
	return b
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
