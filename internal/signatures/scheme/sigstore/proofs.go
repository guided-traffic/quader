package sigstore

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"math/big"
	"time"

	"github.com/digitorus/pkcs7"
	"github.com/digitorus/timestamp"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// The proofs of time of a bundle (R-SIG-006, R-SIG-007, Q-SIG-41): every log entry and every RFC 3161 timestamp is verified against every
// trust root of the check that knows its log (by the log's key ID) or its authority (by the certificate that signed the token). A root
// that knows one and does not verify it makes the signature invalid (log_entry_invalid, timestamp_invalid); one no root knows proves
// nothing, and the time it states is the signature's claim.

// maxStamps is the most RFC 3161 timestamps a bundle may carry (sigstore-go's own bound).
const maxStamps = 32

// single is the bundle with one log entry or one timestamp: sigstore-go's checks of a log entry and of a timestamp run on it alone, so
// that each is verified against each root that knows it.
type single struct {
	*bundle.Bundle
	entries []*tlog.Entry
	stamps  [][]byte
}

// TlogEntries implements verify.SignedEntity.
func (s single) TlogEntries() ([]*tlog.Entry, error) { return s.entries, nil }

// Timestamps implements verify.SignedEntity.
func (s single) Timestamps() ([][]byte, error) { return s.stamps, nil }

// prove verifies the log entries and the timestamps of o against the roots; keys is the trust material of a key-signed bundle's key (nil
// for a certificate).
func prove(o *object, rd *reading, roots []*root.TrustedRoot, keys root.TrustedMaterial) error {
	entries, err := o.b.TlogEntries()
	if err != nil {
		return malformed("a log entry of the bundle does not parse")
	}
	for _, e := range entries {
		if err := proveEntry(o, rd, e, roots, keys); err != nil {
			return err
		}
	}
	stamps, err := o.b.Timestamps()
	if err != nil {
		return malformed("a timestamp of the bundle does not parse")
	}
	if len(stamps) > maxStamps {
		return tooLarge("the bundle carries more than %d timestamps", maxStamps)
	}
	for _, raw := range stamps {
		if err := proveStamp(o, rd, raw, roots); err != nil {
			return err
		}
	}
	return nil
}

// proveEntry verifies one log entry against every root that knows its log.
func proveEntry(o *object, rd *reading, e *tlog.Entry, roots []*root.TrustedRoot, keys root.TrustedMaterial) error {
	id := hex.EncodeToString([]byte(e.LogKeyID()))
	known := false
	for _, r := range roots {
		if _, ok := r.RekorLogs()[id]; !ok {
			continue
		}
		known = true
		tm := root.TrustedMaterial(r)
		if keys != nil {
			tm = root.TrustedMaterialCollection{r, keys}
		}
		times, err := verify.VerifyTlogEntry(single{Bundle: o.b, entries: []*tlog.Entry{e}}, tm, 1, true)
		if err != nil {
			return invalid(signatures.ReasonLogEntryInvalid, "the log entry %d of log %s does not verify against a trust root that knows the log", e.LogIndex(), id)
		}
		if len(times) == 0 {
			rd.claim(e.IntegratedTime()) // an inclusion proof without a promise signs no time
		}
		for _, t := range times {
			rd.prove(t.Time, "log")
		}
	}
	if !known {
		rd.claim(e.IntegratedTime())
	}
	return nil
}

// proveStamp verifies one RFC 3161 timestamp against every root that knows its authority, and keeps it as evidence when its message
// imprint matches the signature.
func proveStamp(o *object, rd *reading, raw []byte, roots []*root.TrustedRoot) error {
	st, err := parseStamp(raw)
	if err != nil {
		return malformed("an RFC 3161 timestamp of the bundle does not parse")
	}
	known := false
	for _, r := range roots {
		knows, verified := false, false
		for _, tsa := range r.TimestampingAuthorities() {
			if !st.knownBy(tsa) {
				continue
			}
			knows = true
			if t, err := tsa.Verify(raw, o.sig); err == nil {
				verified = true
				rd.prove(t.Time, "timestamp")
			}
		}
		if knows && !verified {
			return invalid(signatures.ReasonTimestampInvalid, "a timestamp by %s does not verify against a trust root that knows its authority", st.authority())
		}
		known = known || knows
	}
	if !known && st.info != nil {
		rd.claim(st.time)
	}
	if st.matches(o.sig) {
		rd.stamps = append(rd.stamps, scheme.Timestamp{Token: st.token, Time: st.time.UTC(), Chain: st.chain()})
	}
	return nil
}

// claim keeps the earliest time a log entry or a timestamp states that no root verified.
func (rd *reading) claim(t time.Time) {
	if t.IsZero() {
		return
	}
	if rd.claimed.IsZero() || t.Before(rd.claimed) {
		rd.claimed = t.UTC()
	}
}

// prove keeps the earliest time a root verified, with its proof ("log" or "timestamp").
func (rd *reading) prove(t time.Time, how string) {
	if t.IsZero() {
		return
	}
	if rd.proven.IsZero() || t.Before(rd.proven) {
		rd.proven, rd.proof = t.UTC(), how
	}
}

// stamp is an RFC 3161 timestamp as the check reads it: the token, its signer's issuer and serial, and, when the token's own signature
// verifies, what it states.
type stamp struct {
	token  []byte
	issuer []byte
	serial *big.Int
	certs  []*x509.Certificate
	p7     *pkcs7.PKCS7
	info   *timestamp.Timestamp
	time   time.Time
}

// tsResponse is a TimeStampResp (RFC 3161): the status and the token.
type tsResponse struct {
	Status asn1.RawValue
	Token  asn1.RawValue `asn1:"optional"`
}

// parseStamp reads a timestamp response (or a bare token, as some writers store it). A token whose own signature does not verify parses,
// with no info: a root that knows its authority finds it invalid.
func parseStamp(raw []byte) (*stamp, error) {
	token := raw
	var resp tsResponse
	if rest, err := asn1.Unmarshal(raw, &resp); err == nil && len(rest) == 0 && len(resp.Token.FullBytes) > 0 {
		token = resp.Token.FullBytes
	}
	p7, err := pkcs7.Parse(token)
	if err != nil {
		return nil, err
	}
	if len(p7.Signers) != 1 {
		return nil, errors.New("sigstore: a timestamp token has one signer")
	}
	st := &stamp{
		token: token, issuer: p7.Signers[0].IssuerAndSerialNumber.IssuerName.FullBytes,
		serial: p7.Signers[0].IssuerAndSerialNumber.SerialNumber, certs: p7.Certificates, p7: p7,
	}
	if info, err := timestamp.Parse(token); err == nil {
		st.info, st.time = info, info.Time
	}
	return st, nil
}

// knownBy reports whether a timestamping authority of a root signed the token: the authority's leaf, or a certificate the token carries
// that a certificate of the authority's chain issued, verifies the token's signature. A token that only names the authority is not its:
// it proves nothing and makes nothing invalid.
func (st *stamp) knownBy(tsa root.TimestampingAuthority) bool {
	a, ok := tsa.(*root.SigstoreTimestampingAuthority)
	if !ok {
		return false
	}
	if a.Leaf != nil {
		return st.signedBy(a.Leaf)
	}
	for _, c := range st.p7.Certificates {
		for _, issuer := range append([]*x509.Certificate{a.Root}, a.Intermediates...) {
			if issuer != nil && c.CheckSignatureFrom(issuer) == nil && st.signedBy(c) {
				return true
			}
		}
	}
	return false
}

// signedBy reports whether c's key made the token's signature (its signer's issuer and serial name c).
func (st *stamp) signedBy(c *x509.Certificate) bool {
	if !bytes.Equal(c.RawIssuer, st.issuer) || st.serial == nil || c.SerialNumber.Cmp(st.serial) != 0 {
		return false
	}
	cp := *st.p7
	cp.Certificates = []*x509.Certificate{c}
	return cp.VerifyWithChainAtTime(nil, c.NotBefore) == nil
}

// matches reports whether the token's message imprint is the hash of the signature it stamps.
func (st *stamp) matches(sig []byte) bool {
	if st.info == nil || !st.info.HashAlgorithm.Available() {
		return false
	}
	h := st.info.HashAlgorithm.New()
	h.Write(sig)
	return bytes.Equal(h.Sum(nil), st.info.HashedMessage)
}

// authority names the token's authority: the subject of its signer certificate when the token carries it, else its issuer.
func (st *stamp) authority() string {
	for _, c := range st.certs {
		if bytes.Equal(c.RawIssuer, st.issuer) && st.serial != nil && c.SerialNumber.Cmp(st.serial) == 0 {
			return c.Subject.String()
		}
	}
	var rdn pkix.RDNSequence
	if _, err := asn1.Unmarshal(st.issuer, &rdn); err != nil {
		return ""
	}
	var n pkix.Name
	n.FillFromRDNSequence(&rdn)
	return "a certificate of " + n.String()
}

// chain is the authority's chain as the token carries it, DER.
func (st *stamp) chain() [][]byte {
	var out [][]byte
	for _, c := range st.certs {
		out = append(out, c.Raw)
	}
	return out
}
