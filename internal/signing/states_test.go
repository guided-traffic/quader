package signing_test

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
)

// TestR_SIG_045_TheStateTableAllowsItsMovesAndRefusesTheRest: the seven moves of Q-SIG-145 are allowed; a retired identity never moves,
// a current one is retired only after it became previous, and nothing goes back to pending.
func TestR_SIG_045_TheStateTableAllowsItsMovesAndRefusesTheRest(t *testing.T) {
	states := []string{signing.StatePending, signing.StateAnnounced, signing.StateCurrent, signing.StatePrevious, signing.StateRetired}
	allowed := map[[2]string]bool{
		{signing.StatePending, signing.StateAnnounced}: true, {signing.StatePending, signing.StateCurrent}: true,
		{signing.StateAnnounced, signing.StateCurrent}: true, {signing.StateAnnounced, signing.StatePrevious}: true,
		{signing.StateCurrent, signing.StatePrevious}: true, {signing.StatePrevious, signing.StateAnnounced}: true,
		{signing.StatePrevious, signing.StateRetired}: true,
	}
	for _, from := range states {
		for _, to := range states {
			if got := signing.CanMove(from, to); got != allowed[[2]string{from, to}] {
				t.Errorf("%s -> %s: %v", from, to, got)
			}
		}
	}
	if len(signing.Transitions()) != len(allowed) {
		t.Errorf("%d transitions documented, %d allowed", len(signing.Transitions()), len(allowed))
	}
}

// TestR_SIG_050_TheKeyIDOfNpmjsPublishedKeysIsComputedFromTheirKey: the keyids npmjs publishes in the release's snapshot of its keys
// (read 2026-11-21) are the SSH SHA-256 fingerprints of their keys.
func TestR_SIG_050_TheKeyIDOfNpmjsPublishedKeysIsComputedFromTheirKey(t *testing.T) {
	for want, raw := range map[string]string{
		"SHA256:DhQ8wR5APBvFHLF/+Tc+AYvPOdTpcIDqOhxsBHRwC7U": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEY6Ya7W++7aUPzvMTrezH6Ycx3c+HOKYCcNGybJZSCJq/fd7Qa8uuAKtdIkUQtQiEKERhAmE5lMMJhP8OkDOa2g==",
		"SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE1Olb3zMAFFxXKHiIkQO5cJ3Yhl5i6UPp+IhuteBJbuHcA5UogKo0EWtlWwW6KSaKoTNEYL7JlCQiVnkhBktUgg==",
	} {
		der, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			t.Fatal(err)
		}
		pub, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			t.Fatal(err)
		}
		got, err := signing.NPMKeyID(pub.(*ecdsa.PublicKey))
		if err != nil || got != want {
			t.Errorf("keyid %q, want %q (%v)", got, want, err)
		}
	}
}

// TestR_SIG_048_TheKeysDocumentForEachCombinationOfStates: nothing while no identity is current, whatever previous identities exist;
// with a current identity the announced and the current key without an expiry and every previous one with the moment it stopped
// signing, newest first; pending and retired never; a renewal with the same key once, with the state of its newest row.
func TestR_SIG_048_TheKeysDocumentForEachCombinationOfStates(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2027, 2, 1, h, 0, 0, 0, time.UTC) }
	id := func(n int64, state, keyid string, expires time.Time) signing.Identity {
		return signing.Identity{ID: n, State: state, NPMKeyID: keyid, PublicKey: []byte{byte(n)}, ExpiresAt: expires}
	}
	pending := id(1, signing.StatePending, "SHA256:p", time.Time{})
	announced := id(2, signing.StateAnnounced, "SHA256:a", time.Time{})
	current := id(3, signing.StateCurrent, "SHA256:c", time.Time{})
	prevOld := id(4, signing.StatePrevious, "SHA256:o", at(1))
	prevNew := id(5, signing.StatePrevious, "SHA256:n", at(5))
	retired := id(6, signing.StateRetired, "SHA256:r", at(2))
	oldCert := id(7, signing.StatePrevious, "SHA256:k", at(3))
	renewal := id(8, signing.StateCurrent, "SHA256:k", time.Time{})
	removedRenewal := id(9, signing.StatePrevious, "SHA256:k", at(4))

	render := func(ids ...signing.Identity) string {
		out := ""
		for _, k := range signing.NPMKeys(ids) {
			out += k.KeyID + "=" + k.ExpiresText() + ";"
			if k.KeyType != signing.NPMKeyType || k.Scheme != signing.NPMKeyType {
				t.Errorf("%s: keytype %s scheme %s", k.KeyID, k.KeyType, k.Scheme)
			}
		}
		return out
	}
	for name, c := range map[string]struct {
		ids  []signing.Identity
		want string
	}{
		"none":                         {nil, ""},
		"only previous and retired":    {[]signing.Identity{prevOld, prevNew, retired}, ""},
		"pending and announced":        {[]signing.Identity{pending, announced}, ""},
		"current":                      {[]signing.Identity{current}, "SHA256:c=;"},
		"current and announced":        {[]signing.Identity{announced, current}, "SHA256:a=;SHA256:c=;"},
		"current and previous":         {[]signing.Identity{current, prevOld, prevNew, retired, pending}, "SHA256:c=;SHA256:n=2027-02-01T05:00:00.000Z;SHA256:o=2027-02-01T01:00:00.000Z;"},
		"a renewal with the same key":  {[]signing.Identity{oldCert, renewal}, "SHA256:k=;"},
		"the newest row of a key wins": {[]signing.Identity{current, oldCert, removedRenewal}, "SHA256:c=;SHA256:k=2027-02-01T04:00:00.000Z;"},
	} {
		if got := render(c.ids...); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// TestR_SIG_052_TheTrustedRootHoldsOneAuthorityPerDistinctChain: the current and two previous identities of one CA and one of another
// CA give two certificate authorities, each with its intermediate and its root and the span of the identities under it; pending,
// announced and retired identities add nothing, and sigstore-go reads the document back. Without a current or previous identity there
// is nothing to trust.
func TestR_SIG_052_TheTrustedRootHoldsOneAuthorityPerDistinctChain(t *testing.T) {
	a, b := signingtest.NewCA(t, "Example"), signingtest.NewCA(t, "Other")
	ident := func(n int64, ca *signingtest.CA, state string, from, to time.Time) signing.Identity {
		leaf, _ := ca.Issue(t, signingtest.Leaf{})
		return signing.Identity{
			ID: n, State: state, Certificate: leaf.Raw, Chain: [][]byte{ca.Intermediate.Raw, ca.Root.Raw}, NotBefore: from, NotAfter: to,
		}
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids := []signing.Identity{
		ident(1, a, signing.StatePrevious, t0, t0.AddDate(0, 3, 0)),
		ident(2, a, signing.StatePrevious, t0.AddDate(0, 2, 0), t0.AddDate(0, 5, 0)),
		ident(3, a, signing.StateCurrent, t0.AddDate(0, 4, 0), t0.AddDate(0, 7, 0)),
		ident(4, b, signing.StatePrevious, t0.AddDate(0, 1, 0), t0.AddDate(0, 2, 0)),
		ident(5, b, signing.StateRetired, t0, t0.AddDate(1, 0, 0)),
		ident(6, b, signing.StateAnnounced, t0, t0.AddDate(2, 0, 0)),
	}
	doc, ok, err := signing.TrustedRoot(ids)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	var parsed struct {
		MediaType              string `json:"mediaType"`
		CertificateAuthorities []struct {
			CertChain struct {
				Certificates []struct {
					RawBytes []byte `json:"rawBytes"`
				} `json:"certificates"`
			} `json:"certChain"`
			ValidFor struct{ Start, End time.Time } `json:"validFor"`
		} `json:"certificateAuthorities"`
		Tlogs  []any `json:"tlogs"`
		CTlogs []any `json:"ctlogs"`
		TSAs   []any `json:"timestampAuthorities"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.MediaType != signing.TrustedRootMediaType || len(parsed.CertificateAuthorities) != 2 || len(parsed.Tlogs)+len(parsed.CTlogs)+len(parsed.TSAs) != 0 {
		t.Fatalf("%s", doc)
	}
	spans := map[string][2]time.Time{}
	for _, c := range parsed.CertificateAuthorities {
		certs := c.CertChain.Certificates
		if len(certs) != 2 {
			t.Fatalf("a chain of %d", len(certs))
		}
		spans[string(certs[1].RawBytes)] = [2]time.Time{c.ValidFor.Start, c.ValidFor.End}
	}
	if s := spans[string(a.Root.Raw)]; !s[0].Equal(t0) || !s[1].Equal(t0.AddDate(0, 7, 0)) {
		t.Errorf("the span of the first CA is %v", s)
	}
	if s := spans[string(b.Root.Raw)]; !s[0].Equal(t0.AddDate(0, 1, 0)) || !s[1].Equal(t0.AddDate(0, 2, 0)) {
		t.Errorf("the span of the second CA is %v (the retired and the announced identity count)", s)
	}
	if _, err := root.NewTrustedRootFromJSON(doc); err != nil {
		t.Errorf("sigstore-go does not read it: %v", err)
	}
	if _, ok, _ := signing.TrustedRoot([]signing.Identity{ids[4], ids[5]}); ok {
		t.Error("a retired and an announced identity make a trusted root")
	}
	if pemText := string(signing.ChainPEM(ids[2])); len(pemText) == 0 || countBlocks(pemText) != 3 {
		t.Errorf("the chain has %d blocks", countBlocks(pemText))
	}
}

func countBlocks(s string) int {
	n := 0
	for i := 0; i+27 <= len(s); i++ {
		if s[i:i+27] == "-----BEGIN CERTIFICATE-----" {
			n++
		}
	}
	return n
}
