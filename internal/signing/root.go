package signing

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
)

// TrustedRootMediaType is the media type of the trusted root Quader serves (R-SIG-052).
const TrustedRootMediaType = root.TrustedRootMediaType01

// TrustedRoot is the Sigstore trusted root of the identities (R-SIG-052): one certificate authority per distinct chain of the current
// and the previous identities, its intermediates first and its root last, valid from the earliest not_before to the latest not_after of
// the identities under it, and no transparency log, certificate-transparency log or timestamp authority. ok is false when no identity is
// current or previous: there is nothing to trust.
func TrustedRoot(ids []Identity) (doc []byte, ok bool, err error) {
	type authority struct {
		chain      []*x509.Certificate
		start, end time.Time
	}
	byChain := map[string]*authority{}
	var order []string
	for _, i := range ids {
		if (i.State != StateCurrent && i.State != StatePrevious) || len(i.Chain) == 0 {
			continue
		}
		key := string(bytes.Join(i.Chain, nil))
		a := byChain[key]
		if a == nil {
			certs, err := parseAll(i.Chain)
			if err != nil {
				return nil, false, fmt.Errorf("the chain of identity %d: %w", i.ID, err)
			}
			a = &authority{chain: certs, start: i.NotBefore, end: i.NotAfter}
			byChain[key] = a
			order = append(order, key)
		}
		if i.NotBefore.Before(a.start) {
			a.start = i.NotBefore
		}
		if i.NotAfter.After(a.end) {
			a.end = i.NotAfter
		}
	}
	if len(order) == 0 {
		return nil, false, nil
	}
	sort.Strings(order)
	cas := make([]root.CertificateAuthority, 0, len(order))
	for _, k := range order {
		a := byChain[k]
		n := len(a.chain)
		cas = append(cas, &root.FulcioCertificateAuthority{
			Root: a.chain[n-1], Intermediates: a.chain[:n-1], ValidityPeriodStart: a.start.UTC(), ValidityPeriodEnd: a.end.UTC(),
		})
	}
	tr, err := root.NewTrustedRoot(TrustedRootMediaType, cas, nil, nil, nil)
	if err != nil {
		return nil, false, err
	}
	doc, err = tr.MarshalJSON()
	if err != nil {
		return nil, false, err
	}
	return doc, true, nil
}

// ChainPEM is the identity's certificates as PEM: the leaf, its intermediates, its root (R-SIG-052's chain.pem).
func ChainPEM(i Identity) []byte {
	var b bytes.Buffer
	for _, der := range append([][]byte{i.Certificate}, i.Chain...) {
		if len(der) > 0 {
			_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: der})
		}
	}
	return b.Bytes()
}

func parseAll(ders [][]byte) ([]*x509.Certificate, error) {
	out := make([]*x509.Certificate, 0, len(ders))
	for _, d := range ders {
		c, err := x509.ParseCertificate(d)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
