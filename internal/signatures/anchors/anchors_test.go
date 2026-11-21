package anchors

import (
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/deb/archivekeys"
	"github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/internal/signatures/shipped/tuftest"
)

// TestR_SIG_015_TheMaterialMatchesTheKind holds the checks every kind shares (R-SIG-015): the kind is one of the four, the member of the
// kind is set and no other.
func TestR_SIG_015_TheMaterialMatchesTheKind(t *testing.T) {
	pk := &PublicKeyMaterial{Keys: []PublicKeyEntry{{PEM: pubPEM(t, p256(t))}}}
	if _, err := Validate(KindPublicKey, Material{PublicKey: pk}, now); err != nil {
		t.Fatalf("valid: %v", err)
	}
	_, err := Validate("x509", Material{PublicKey: pk}, now)
	wantInvalid(t, err, CodeInvalidValue, "/kind")
	_, err = Validate(KindPublicKey, Material{PublicKey: pk, OpenPGP: &OpenPGPMaterial{Keys: "x"}}, now)
	wantInvalid(t, err, CodeInvalidValue, "/openpgp")
	_, err = Validate(KindCA, Material{}, now)
	wantInvalid(t, err, CodeInvalidValue, "/ca")
}

// TestR_SIG_015_CAAnchor holds the checks of a CA anchor (R-SIG-015, Q-SIG-30): a valid and an invalid sample per check.
func TestR_SIG_015_CAAnchor(t *testing.T) {
	ca := cert(t, certOpts{ca: true, cn: "Example CA"})
	many := strings.Repeat(ca, maxCACertificates+1)
	for _, tc := range []struct {
		name    string
		m       CAMaterial
		pointer string // "" when valid
	}{
		{"a P-256 CA certificate", CAMaterial{Certificates: ca}, ""},
		{"an RSA 2048 CA and a pinned Ed25519 end-entity certificate", CAMaterial{Certificates: cert(t, certOpts{ca: true, key: rsaKey(t, 2048)}) +
			cert(t, certOpts{key: edKey(t), eku: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}})}, ""},
		{"a P-521 certificate", CAMaterial{Certificates: cert(t, certOpts{key: ecKey(t, elliptic.P521())})}, ""},
		{"no certificate", CAMaterial{Certificates: " \n"}, "/ca/certificates"},
		{"text outside the blocks", CAMaterial{Certificates: "issued 2026\n" + ca}, "/ca/certificates"},
		{"a block that is no certificate", CAMaterial{Certificates: pubPEM(t, p256(t))}, "/ca/certificates"},
		{"a certificate that does not parse", CAMaterial{Certificates: "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"}, "/ca/certificates"},
		{"17 certificates", CAMaterial{Certificates: many}, "/ca/certificates"},
		{"more than 64 KiB", CAMaterial{Certificates: ca + strings.Repeat(" ", maxCABytes)}, "/ca/certificates"},
		{"RSA of 1024 bits", CAMaterial{Certificates: cert(t, certOpts{ca: true, key: rsaKey(t, 1024)})}, "/ca/certificates"},
		{"ECDSA over P-224", CAMaterial{Certificates: cert(t, certOpts{ca: true, key: ecKey(t, elliptic.P224())})}, "/ca/certificates"},
		{"expired at save", CAMaterial{Certificates: cert(t, certOpts{ca: true, notAfter: now.Add(-1)})}, "/ca/certificates"},
		{"a CA that may not sign certificates", CAMaterial{Certificates: cert(t, certOpts{ca: true, usage: x509.KeyUsageDigitalSignature})}, "/ca/certificates"},
		{"an end-entity certificate that may not sign", CAMaterial{Certificates: cert(t, certOpts{usage: x509.KeyUsageKeyEncipherment})}, "/ca/certificates"},
		{"code signing by a server-only certificate", CAMaterial{Certificates: cert(t, certOpts{eku: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})}, "/ca/certificates"},
		{"timestamping by a root without extended key usage", CAMaterial{Certificates: ca, Use: UseTimestamping}, ""},
		{"timestamping by a timestamp authority", CAMaterial{
			Use:          UseTimestamping,
			Certificates: cert(t, certOpts{eku: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}}),
		}, ""},
		{"timestamping by a code-signing certificate", CAMaterial{
			Use:          UseTimestamping,
			Certificates: cert(t, certOpts{eku: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}}),
		}, "/ca/certificates"},
		{"an unknown use", CAMaterial{Certificates: ca, Use: "signing"}, "/ca/use"},
		{"a subject restriction of O, OU, CN and C", CAMaterial{
			Certificates: ca,
			Subject:      map[string]string{"O": "Example GmbH", "OU": "CI", "CN": "Release", "C": "DE"},
		}, ""},
		{"a subject restriction of L", CAMaterial{Certificates: ca, Subject: map[string]string{"L": "Berlin"}}, "/ca/subject"},
		{"an empty subject value", CAMaterial{Certificates: ca, Subject: map[string]string{"O": " "}}, "/ca/subject/O"},
		{"alternative-name patterns", CAMaterial{Certificates: ca, SubjectAltNames: []string{`.+@example\.com`, `https://ci\.example\.com/.*`}}, ""},
		{"a pattern that does not compile", CAMaterial{Certificates: ca, SubjectAltNames: []string{`.*`, `(`}}, "/ca/subjectAltNames/1"},
		{
			"a pattern above 512 characters",
			CAMaterial{Certificates: ca, SubjectAltNames: []string{strings.Repeat("a", maxPatternLength+1)}},
			"/ca/subjectAltNames/0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.m
			p, err := Validate(KindCA, Material{CA: &m}, now)
			if tc.pointer != "" {
				wantInvalid(t, err, CodeInvalidValue, tc.pointer)
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if len(p.CA.Certificates) == 0 || len(p.Describes) != len(p.CA.Certificates) {
				t.Fatalf("parsed %d certificates, %d fingerprints", len(p.CA.Certificates), len(p.Describes))
			}
		})
	}
}

// TestR_SIG_015_CAAnchorParsed holds what a CA anchor shows and how its patterns match (R-SIG-015): the lower-case hex SHA-256, the subject
// and the expiry of each certificate, the use code signing when unset, patterns anchored to the whole name; and stored material whose
// certificate expired since still parses.
func TestR_SIG_015_CAAnchorParsed(t *testing.T) {
	pemText := cert(t, certOpts{ca: true, cn: "Example CA", notAfter: now.AddDate(0, 1, 0)})
	p, err := Validate(KindCA, Material{CA: &CAMaterial{Certificates: pemText, SubjectAltNames: []string{`a.c`}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	c := p.CA.Certificates[0]
	if f := p.Describes[0]; f.Kind != "sha256" || f.Value != sha256Hex(c.Raw) || f.Value != strings.ToLower(f.Value) ||
		!strings.Contains(f.Subject, "CN=Example CA") || !f.NotAfter.Equal(c.NotAfter) {
		t.Fatalf("fingerprint %+v", f)
	}
	if p.CA.Use != UseCodeSigning || !p.CA.SANPatterns[0].MatchString("abc") || p.CA.SANPatterns[0].MatchString("xabc") {
		t.Fatalf("use %q, pattern %s", p.CA.Use, p.CA.SANPatterns[0])
	}
	later := Material{CA: &CAMaterial{Certificates: pemText}}
	if _, err := Validate(KindCA, later, now.AddDate(0, 2, 0)); err == nil {
		t.Fatal("an expired certificate is accepted on save")
	}
	if _, err := Parse(KindCA, later); err != nil {
		t.Fatalf("stored material with an expired certificate: %v", err)
	}
}

// pgpKey is an armored public certificate made by internal/openpgp at created, and its secret key.
func pgpKey(t *testing.T, uid string, created int) (public, secret, fp string) {
	t.Helper()
	sec, pub, fp, err := openpgp.Generate(uid, now.AddDate(0, 0, created))
	if err != nil {
		t.Fatal(err)
	}
	return string(pub), string(sec), fp
}

// TestR_SIG_015_OpenPGPAnchor holds the checks of an OpenPGP anchor (R-SIG-015) by the rules of deb.upstreamKeys: a valid and an invalid
// sample per check, and what it shows.
func TestR_SIG_015_OpenPGPAnchor(t *testing.T) {
	one, secret, fp := pgpKey(t, "Release signing", -30)
	two, _, _ := pgpKey(t, "Nightly signing", -30)
	future, _, _ := pgpKey(t, "Future signing", 30)
	var thirtyThree strings.Builder
	for range maxOpenPGPCerts + 1 {
		k, _, _ := pgpKey(t, "Many", -1)
		thirtyThree.WriteString(k)
	}
	for _, tc := range []struct {
		name, keys, code string // code "" when valid
	}{
		{"one certificate", one, ""},
		{"two certificates", one + two, ""},
		{"no certificate", "", CodeInvalidValue},
		{"no armor", "mQINBF...", CodeInvalidValue},
		{"33 certificates", thirtyThree.String(), CodeInvalidValue},
		{"more than 256 KiB", one + strings.Repeat("\n", maxOpenPGPBytes), CodeInvalidValue},
		{"not yet valid at save", future, CodeInvalidValue},
		{"a secret key", secret, CodeSecretMaterial},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Validate(KindOpenPGP, Material{OpenPGP: &OpenPGPMaterial{Keys: tc.keys}}, now)
			if tc.code != "" {
				wantInvalid(t, err, tc.code, "/openpgp/keys")
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			got := p.OpenPGP[0]
			if got.Fingerprint != fp || len(got.KeyIDs) != 1 || got.KeyIDs[0] != fp[24:] || got.UserID != "Release signing" ||
				!strings.Contains(string(got.Armored), "BEGIN PGP PUBLIC KEY BLOCK") {
				t.Fatalf("certificate %+v", got)
			}
			if f := p.Describes[0]; f.Kind != "openpgp" || f.Value != fp || f.Subject != got.UserID {
				t.Fatalf("fingerprint %+v", f)
			}
		})
	}
	if _, err := Parse(KindOpenPGP, Material{OpenPGP: &OpenPGPMaterial{Keys: future}}); err != nil {
		t.Fatalf("stored material is judged by its validity: %v", err)
	}
}

// TestR_SIG_015_OpenPGPKeyIDsOfSubkeys reads the key IDs of a certificate with subkeys (R-SIG-015): the Debian archive keyring's keys carry
// signing subkeys, each listed after the primary key's ID, in internal/openpgp's spelling.
func TestR_SIG_015_OpenPGPKeyIDsOfSubkeys(t *testing.T) {
	p, err := Parse(KindOpenPGP, Material{OpenPGP: &OpenPGPMaterial{Keys: string(archivekeys.Debian())}})
	if err != nil {
		t.Fatal(err)
	}
	subkeys := 0
	for _, c := range p.OpenPGP {
		if len(c.KeyIDs) == 0 || c.KeyIDs[0] != c.Fingerprint[24:] {
			t.Fatalf("%s: key IDs %v", c.Fingerprint, c.KeyIDs)
		}
		for _, id := range c.KeyIDs {
			if len(id) != 16 || strings.ToUpper(id) != id {
				t.Fatalf("%s: key ID %q", c.Fingerprint, id)
			}
		}
		subkeys += len(c.KeyIDs) - 1
	}
	if subkeys == 0 {
		t.Fatal("no subkey read from the Debian keyring")
	}
}

// TestR_SIG_015_PublicKeyAnchor holds the checks of a public-key anchor (R-SIG-015): a valid and an invalid sample per check, and what it
// shows.
func TestR_SIG_015_PublicKeyAnchor(t *testing.T) {
	key := p256(t)
	keyid := "SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA"
	many := make([]PublicKeyEntry, maxPublicKeys+1)
	for i := range many {
		many[i] = PublicKeyEntry{PEM: pubPEM(t, key)}
	}
	for _, tc := range []struct {
		name    string
		keys    []PublicKeyEntry
		pointer string
	}{
		{"P-256 with npm's keyid", []PublicKeyEntry{{PEM: pubPEM(t, key), KeyID: keyid, Expires: &now}}, ""},
		{"P-384, RSA 2048 and Ed25519", []PublicKeyEntry{
			{PEM: pubPEM(t, ecKey(t, elliptic.P384()))},
			{PEM: pubPEM(t, rsaKey(t, 2048))},
			{PEM: pubPEM(t, edKey(t))},
		}, ""},
		{"no key", nil, "/publicKey/keys"},
		{"17 keys", many, "/publicKey/keys"},
		{"no PEM", []PublicKeyEntry{{PEM: "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE"}}, "/publicKey/keys/0/pem"},
		{"two blocks in one key", []PublicKeyEntry{{PEM: pubPEM(t, key) + pubPEM(t, key)}}, "/publicKey/keys/0/pem"},
		{"a certificate", []PublicKeyEntry{{PEM: pubPEM(t, key)}, {PEM: cert(t, certOpts{})}}, "/publicKey/keys/1/pem"},
		{"a key that does not parse", []PublicKeyEntry{{PEM: "-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n"}}, "/publicKey/keys/0/pem"},
		{"RSA of 1024 bits", []PublicKeyEntry{{PEM: pubPEM(t, rsaKey(t, 1024))}}, "/publicKey/keys/0/pem"},
		{"ECDSA over P-224", []PublicKeyEntry{{PEM: pubPEM(t, ecKey(t, elliptic.P224()))}}, "/publicKey/keys/0/pem"},
		{"a keyid without SHA256:", []PublicKeyEntry{{PEM: pubPEM(t, key), KeyID: "jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA"}}, "/publicKey/keys/0/keyid"},
		{"a keyid of 16 bytes", []PublicKeyEntry{{PEM: pubPEM(t, key), KeyID: "SHA256:AAAAAAAAAAAAAAAAAAAAAA"}}, "/publicKey/keys/0/keyid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Validate(KindPublicKey, Material{PublicKey: &PublicKeyMaterial{Keys: tc.keys}}, now)
			if tc.pointer != "" {
				wantInvalid(t, err, CodeInvalidValue, tc.pointer)
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if len(p.Public) != len(tc.keys) || len(p.Describes) != len(tc.keys) {
				t.Fatalf("parsed %d keys, %d fingerprints", len(p.Public), len(p.Describes))
			}
		})
	}
	p, err := Validate(KindPublicKey, Material{PublicKey: &PublicKeyMaterial{Keys: []PublicKeyEntry{{PEM: pubPEM(t, key), KeyID: keyid, Expires: &now}}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(key.Public())
	k, f := p.Public[0], p.Describes[0]
	if k.SHA256 != sha256Hex(der) || k.KeyID != keyid || !k.Expires.Equal(now) || f.Kind != "sha256" || f.Value != k.SHA256 || f.Subject != keyid {
		t.Fatalf("key %+v, fingerprint %+v", k, f)
	}
}

// TestR_SIG_015_SigstoreAnchor holds the checks of a Sigstore identity anchor (R-SIG-015, Q-SIG-27): a valid and an invalid sample per
// check, and what it shows.
func TestR_SIG_015_SigstoreAnchor(t *testing.T) {
	own := tuftest.TrustedRoot(t, "Example")
	var doc map[string]any
	if err := json.Unmarshal(own, &doc); err != nil {
		t.Fatal(err)
	}
	without := func(member string) json.RawMessage {
		d := map[string]any{}
		for k, v := range doc {
			d[k] = v
		}
		d[member] = []any{}
		b, _ := json.Marshal(d)
		return b
	}
	gitlab := "https://gitlab.example.com"
	for _, tc := range []struct {
		name    string
		m       SigstoreMaterial
		pointer string
	}{
		{"an exact identity against the public root", SigstoreMaterial{Issuer: gitlab, Identity: "release@example.com", TrustRoot: TrustRootPublic}, ""},
		{"a pattern against an own root", SigstoreMaterial{
			Issuer: gitlab, IdentityPattern: `https://gitlab\.example\.com/platform/.+`,
			TrustRoot: TrustRootOwn, TrustedRoot: own,
		}, ""},
		{"any issuer on save", SigstoreMaterial{Identity: "a", TrustRoot: TrustRootPublic}, "/sigstore/issuer"},
		{"an issuer over HTTP", SigstoreMaterial{Issuer: "http://gitlab.example.com", Identity: "a", TrustRoot: TrustRootPublic}, "/sigstore/issuer"},
		{"an issuer without a host", SigstoreMaterial{Issuer: "https://", Identity: "a", TrustRoot: TrustRootPublic}, "/sigstore/issuer"},
		{
			"an identity and a pattern",
			SigstoreMaterial{Issuer: gitlab, Identity: "a", IdentityPattern: "a", TrustRoot: TrustRootPublic},
			"/sigstore/identityPattern",
		},
		{"no identity", SigstoreMaterial{Issuer: gitlab, TrustRoot: TrustRootPublic}, "/sigstore/identity"},
		{"a pattern that does not compile", SigstoreMaterial{Issuer: gitlab, IdentityPattern: "(", TrustRoot: TrustRootPublic}, "/sigstore/identityPattern"},
		{"a pattern above 512 characters", SigstoreMaterial{
			Issuer: gitlab, IdentityPattern: strings.Repeat("a", maxPatternLength+1),
			TrustRoot: TrustRootPublic,
		}, "/sigstore/identityPattern"},
		{"an unknown trust root", SigstoreMaterial{Issuer: gitlab, Identity: "a", TrustRoot: "staging"}, "/sigstore/trustRoot"},
		{"a public root with a document", SigstoreMaterial{Issuer: gitlab, Identity: "a", TrustRoot: TrustRootPublic, TrustedRoot: own}, "/sigstore/trustedRoot"},
		{"an own root without a document", SigstoreMaterial{Issuer: gitlab, Identity: "a", TrustRoot: TrustRootOwn}, "/sigstore/trustedRoot"},
		{"an own root that does not parse", SigstoreMaterial{
			Issuer: gitlab, Identity: "a", TrustRoot: TrustRootOwn,
			TrustedRoot: json.RawMessage(`{"mediaType":"application/json"}`),
		}, "/sigstore/trustedRoot"},
		{"an own root without a certificate authority", SigstoreMaterial{
			Issuer: gitlab, Identity: "a", TrustRoot: TrustRootOwn,
			TrustedRoot: without("certificateAuthorities"),
		}, "/sigstore/trustedRoot"},
		{"an own root without a log or a timestamp authority", SigstoreMaterial{
			Issuer: gitlab, Identity: "a", TrustRoot: TrustRootOwn,
			TrustedRoot: without("tlogs"),
		}, "/sigstore/trustedRoot"},
		{
			"an own root above 1 MiB",
			SigstoreMaterial{
				Issuer: gitlab, Identity: "a", TrustRoot: TrustRootOwn,
				TrustedRoot: append(append(json.RawMessage{}, own[:len(own)-1]...), []byte(strings.Repeat(" ", maxTrustedRootBytes)+"}")...),
			},
			"/sigstore/trustedRoot",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.m
			p, err := Validate(KindSigstore, Material{Sigstore: &m}, now)
			if tc.pointer != "" {
				wantInvalid(t, err, CodeInvalidValue, tc.pointer)
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if p.Sigstore.Issuer != gitlab || len(p.Describes) == 0 || !strings.HasPrefix(p.Describes[0].Subject, gitlab) {
				t.Fatalf("parsed %+v, %+v", p.Sigstore, p.Describes)
			}
		})
	}
	p, err := Parse(KindSigstore, Material{Sigstore: &SigstoreMaterial{IdentityPattern: `.*`, TrustRoot: TrustRootPublic}})
	if err != nil || p.Sigstore.Pattern == nil || !p.Sigstore.Pattern.MatchString("anyone") || p.Sigstore.TrustedRoot != nil {
		t.Fatalf("the shipped form of any issuer: %v, %+v", err, p)
	}
	p, err = Validate(KindSigstore, Material{Sigstore: &SigstoreMaterial{
		Issuer: gitlab, IdentityPattern: `ci@.+`, TrustRoot: TrustRootOwn,
		TrustedRoot: own,
	}}, now)
	if err != nil || p.Sigstore.Pattern.MatchString("xci@a") || len(p.Describes) != 2 || p.Describes[1].Value != sha256Hex(own) ||
		string(p.Sigstore.TrustedRoot) != string(own) {
		t.Fatalf("an own root: %v, %+v", err, p)
	}
}

// TestR_SIG_015_APrivateKeyAnywhereIsSecretMaterial puts a private key into every text of every kind (R-SIG-015): each answers
// secret_material at the member that holds it, before any other check.
func TestR_SIG_015_APrivateKeyAnywhereIsSecretMaterial(t *testing.T) {
	priv := privPEM(t, p256(t))
	rsaPriv := strings.ReplaceAll(priv, "PRIVATE KEY", "RSA PRIVATE KEY")
	_, pgpSecret, _ := pgpKey(t, "Secret signing", -1)
	escaped, _ := json.Marshal(map[string]string{"note": priv})
	ca := cert(t, certOpts{ca: true})
	for _, tc := range []struct {
		kind    string
		m       Material
		pointer string
	}{
		{KindCA, Material{CA: &CAMaterial{Certificates: ca + priv}}, "/ca/certificates"},
		{KindCA, Material{CA: &CAMaterial{Certificates: ca, Subject: map[string]string{"CN": rsaPriv}}}, "/ca/subject/CN"},
		{KindCA, Material{CA: &CAMaterial{Certificates: ca, SubjectAltNames: []string{"a", priv}}}, "/ca/subjectAltNames/1"},
		{KindOpenPGP, Material{OpenPGP: &OpenPGPMaterial{Keys: pgpSecret}}, "/openpgp/keys"},
		{KindPublicKey, Material{PublicKey: &PublicKeyMaterial{Keys: []PublicKeyEntry{{PEM: priv}}}}, "/publicKey/keys/0/pem"},
		{KindPublicKey, Material{PublicKey: &PublicKeyMaterial{Keys: []PublicKeyEntry{{PEM: pubPEM(t, p256(t)), KeyID: priv}}}}, "/publicKey/keys/0/keyid"},
		{KindSigstore, Material{Sigstore: &SigstoreMaterial{Issuer: "https://a.example", Identity: priv, TrustRoot: TrustRootPublic}}, "/sigstore/identity"},
		{
			KindSigstore,
			Material{Sigstore: &SigstoreMaterial{Issuer: "https://a.example", Identity: "a", TrustRoot: TrustRootOwn, TrustedRoot: escaped}},
			"/sigstore/trustedRoot",
		},
		{KindPublicKey, Material{
			PublicKey: &PublicKeyMaterial{Keys: []PublicKeyEntry{{PEM: pubPEM(t, p256(t))}}},
			CA:        &CAMaterial{Certificates: priv},
		}, "/ca/certificates"},
	} {
		t.Run(fmt.Sprintf("%s at %s", tc.kind, tc.pointer), func(t *testing.T) {
			_, err := Validate(tc.kind, tc.m, now)
			wantInvalid(t, err, CodeSecretMaterial, tc.pointer)
		})
	}
}

// TestR_SIG_015_PacketFraming reads the two packet header formats of RFC 9580 section 4.2 that a certificate may use, and refuses a
// truncated packet: the reader behind the key IDs of TestR_SIG_015_OpenPGPKeyIDsOfSubkeys.
func TestR_SIG_015_PacketFraming(t *testing.T) {
	content := []byte{4, 0, 0, 0, 0, 17, 1, 2} // version 4, time 0, algorithm 17 (DSA), two octets of key material
	for name, packet := range map[string][]byte{
		"current format": append([]byte{0xc0 | tagPublicKey, byte(len(content))}, content...),
		"legacy format":  append([]byte{0x80 | tagPublicKey<<2 | 1, 0, byte(len(content))}, content...),
	} {
		tag, got, rest, err := nextPacket(packet)
		if err != nil || tag != tagPublicKey || string(got) != string(content) || len(rest) != 0 {
			t.Fatalf("%s: tag %d, %d octets, %v", name, tag, len(got), err)
		}
		k, err := readKeyPacket(got)
		if err != nil || k.algorithm != 17 || len(k.keyID) != 16 || k.fingerprint[24:] != k.keyID || signAlgorithm(k.algorithm) {
			t.Fatalf("%s: %+v, %v", name, k, err)
		}
	}
	if _, _, _, err := nextPacket([]byte{0xc0 | tagPublicKey, 10, 4}); err == nil {
		t.Fatal("a truncated packet is read")
	}
}
