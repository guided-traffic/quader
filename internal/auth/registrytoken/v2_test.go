package registrytoken

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// payloadOf decodes the payload of a registry token.
func payloadOf(t *testing.T, tok string) []byte {
	t.Helper()
	encoded, _, _ := strings.Cut(strings.TrimPrefix(tok, Prefix), ".")
	p, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// readablePart returns the opaque key ID, the readable claims and the sealed part of a payload of version 2.
func readablePart(t *testing.T, payload []byte) (kid string, readable map[string]json.RawMessage, sealed []byte) {
	t.Helper()
	rest, ok := bytes.CutPrefix(payload, []byte("2:"))
	if !ok {
		t.Fatalf("payload version: %q", payload)
	}
	k, body, _ := bytes.Cut(rest, []byte(":"))
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&readable); err != nil {
		t.Fatal(err)
	}
	return string(k), readable, body[dec.InputOffset()+1:]
}

// TestR_BAS_115_ARegistryTokenReadsOnlyItsAnonymityExpiryAndGrant (formats/oci.md R-OCI-005, Q-BAS-339, Q-BAS-388): a token of a
// principal is issued in payload version 2, whose readable claims are a, exp and g alone and whose key ID names no key of the ring;
// the endpoint, the principal, the Quader token behind it and the issue time are sealed, so no byte of the payload names them, and
// two tokens of the same claims seal them apart. The verifier reads the sealed claims back, and still reads a token of version 1
// until it expires.
func TestR_BAS_115_ARegistryTokenReadsOnlyItsAnonymityExpiryAndGrant(t *testing.T) {
	s := signer(t, "k1", keyFile(t, t.TempDir(), "k1", 1))
	c := patClaims()
	c.Grant.Catalog = true
	tok, err := s.Issue(c)
	if err != nil {
		t.Fatal(err)
	}
	payload := payloadOf(t, tok)
	kid, readable, sealed := readablePart(t, payload)
	if kid == "k1" || len(kid) != 2*opaqueKeyIDBytes {
		t.Errorf("the key ID %q is not opaque", kid)
	}
	for k := range readable {
		if k != "a" && k != "exp" && k != "g" {
			t.Errorf("the readable claims name %q: %s", k, payload)
		}
	}
	if _, ok := readable["g"]; !ok {
		t.Errorf("the grant is not readable: %s", payload)
	}
	hash := base64.RawURLEncoding.EncodeToString(c.Binding.SecretSHA256[:])
	// The sealed part is random bytes in base64: a short name such as "k1" turns up in it by chance about once in fifteen tokens,
	// so the names are looked for in what is readable, the version, the key ID and the readable claims.
	clear := payload[:len(payload)-len(sealed)]
	for _, secret := range []string{"k1", c.PrincipalID, c.Binding.TokenID, `"pat"`, hash, "default", `"user"`, `"e"`, `"iat"`, `"c"`} {
		if bytes.Contains(clear, []byte(secret)) {
			t.Errorf("the payload names %q", secret)
		}
	}

	t.Run("the verifier reads the sealed claims back", func(t *testing.T) {
		got, err := s.Verify(tok, "default", t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if got.KeyID != "k1" || got.PrincipalKind != c.PrincipalKind || got.PrincipalID != c.PrincipalID || got.Binding != c.Binding ||
			got.Endpoint != "default" || !got.IssuedAt.Equal(c.IssuedAt) || !got.Grant.Catalog || !got.Grant.Allows(webRepo, "app", Push) {
			t.Fatalf("claims %+v", got)
		}
	})
	t.Run("the sealed part is opaque: the same claims seal apart, and a changed sealed byte does not verify", func(t *testing.T) {
		again, err := s.Issue(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, other := readablePart(t, payloadOf(t, again)); bytes.Equal(other, sealed) {
			t.Error("two tokens of the same claims carry the same sealed part")
		}
		forged := append([]byte(nil), payload...)
		forged[len(forged)-1] ^= 1
		signed := Prefix + b64(forged)
		key, _ := s.keys.Key("k1")
		if _, err := s.Verify(signed+"."+b64(mac(key, signed)), "default", t0); err == nil {
			t.Error("a token whose sealed part was changed under a valid MAC verified")
		}
	})
	t.Run("a token of version 1 is still verified until it expires", func(t *testing.T) {
		old := forge(t, s, patClaims())
		if !bytes.HasPrefix(payloadOf(t, old), []byte("1:k1:{")) {
			t.Fatalf("not a version 1 token: %q", payloadOf(t, old))
		}
		got, err := s.Verify(old, "default", t0.Add(time.Minute))
		if err != nil || got.PrincipalID != c.PrincipalID || got.KeyID != "k1" {
			t.Fatalf("%+v %v", got, err)
		}
		if _, err := s.Verify(old, "default", t0.Add(5*time.Minute)); err == nil {
			t.Error("an expired token of version 1 verified")
		}
	})
	t.Run("an anonymous token reads a and exp alone", func(t *testing.T) {
		anon, err := s.Issue(Claims{Anonymous: true, Endpoint: "default", IssuedAt: t0, ExpiresAt: t0.Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		_, r, _ := readablePart(t, payloadOf(t, anon))
		if len(r) != 2 || string(r["a"]) != "true" || r["exp"] == nil {
			t.Errorf("readable claims %v", r)
		}
	})
	t.Run("a scan token keeps its system subject readable", func(t *testing.T) {
		scan, err := scanSigner(t).Issue(scanClaims())
		if err != nil {
			t.Fatal(err)
		}
		_, r, _ := readablePart(t, payloadOf(t, scan))
		if string(r["s"]) != `"`+ScanSubject+`"` || r["sid"] != nil || r["d"] != nil {
			t.Errorf("readable claims %v", r)
		}
	})
}
