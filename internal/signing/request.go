package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
)

// DefaultCommonName is the common name the key request's form holds (R-SIG-047).
const DefaultCommonName = "Quader signing"

// The bounds of a key request (R-SIG-047, Q-SIG-147).
const (
	maxSubjectField = 64
	maxNames        = 8
	maxURI          = 512
	maxEmail        = 254
)

// CodeInvalidValue is the code of a refused member of a request (R-SIG-047, R-SIG-055).
const CodeInvalidValue = "invalid_value"

// Subject is the distinguished name a key request asks for: common name, organization, organizational unit, locality, state or
// province, country.
type Subject struct {
	CN, O, OU, L, ST, C string
}

// KeyRequest is what an administrator asks the CA for (R-SIG-047): the subject and the alternative names.
type KeyRequest struct {
	Subject Subject
	URIs    []string
	Emails  []string
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// Normalized fills the defaults of the form (common name "Quader signing"; the base URL as the URI when no alternative name is given)
// and refuses a member out of its bounds: every field of the subject at most 64 characters, the country two capital letters, at most
// 8 URIs (absolute, with a host, at most 512 characters) and 8 e-mail addresses (at most 254). The answer carries at least one
// alternative name, which R-SIG-045 requires of the certificate.
func (r KeyRequest) Normalized(baseURL string) (KeyRequest, error) {
	out := KeyRequest{Subject: r.Subject}
	s := &out.Subject
	for _, f := range []*string{&s.CN, &s.O, &s.OU, &s.L, &s.ST, &s.C} {
		*f = strings.TrimSpace(*f)
	}
	if s.CN == "" {
		s.CN = DefaultCommonName
	}
	var fe []errs.FieldError
	bad := func(pointer, detail string) {
		fe = append(fe, errs.FieldError{Pointer: pointer, Code: CodeInvalidValue, Detail: detail})
	}
	for _, f := range []struct{ name, v string }{{"CN", s.CN}, {"O", s.O}, {"OU", s.OU}, {"L", s.L}, {"ST", s.ST}} {
		if utf8.RuneCountInString(f.v) > maxSubjectField {
			bad("/subject/"+f.name, "At most 64 characters.")
		}
	}
	if s.C != "" && !countryCode.MatchString(s.C) {
		bad("/subject/C", "The country is two capital letters (ISO 3166), such as DE.")
	}
	for _, u := range r.URIs {
		if u = strings.TrimSpace(u); u != "" {
			out.URIs = append(out.URIs, u)
		}
	}
	for _, e := range r.Emails {
		if e = strings.TrimSpace(e); e != "" {
			out.Emails = append(out.Emails, e)
		}
	}
	if len(out.URIs) == 0 && len(out.Emails) == 0 && baseURL != "" {
		out.URIs = []string{strings.TrimRight(baseURL, "/")}
	}
	if len(out.URIs) > maxNames {
		bad("/uris", "At most 8 URIs.")
	}
	if len(out.Emails) > maxNames {
		bad("/emails", "At most 8 e-mail addresses.")
	}
	for i, u := range out.URIs {
		p, err := url.Parse(u)
		if err != nil || !p.IsAbs() || p.Host == "" || len(u) > maxURI || p.User != nil {
			bad("/uris/"+strconv.Itoa(i), "An absolute URI with a host and without a user, at most 512 characters, such as https://quader.example.com.")
		}
	}
	for i, e := range out.Emails {
		a, err := mail.ParseAddress(e)
		if err != nil || a.Address != e || len(e) > maxEmail {
			bad("/emails/"+strconv.Itoa(i), "An e-mail address such as signing@example.com, at most 254 characters.")
		}
	}
	if len(out.URIs) == 0 && len(out.Emails) == 0 {
		bad("/uris", "At least one URI or e-mail address: a verifier matches the identity against the certificate's alternative names.")
	}
	if len(fe) > 0 {
		return KeyRequest{}, errs.E(errs.Invalid, errs.CodeValidation, strconv.Itoa(len(fe))+" member(s) of the key request are invalid.", errs.WithFields(fe...))
	}
	return out, nil
}

// The extensions a key request asks for (R-SIG-047): key usage digitalSignature and extended key usage codeSigning.
func oidKeyUsage() asn1.ObjectIdentifier    { return asn1.ObjectIdentifier{2, 5, 29, 15} }
func oidExtKeyUsage() asn1.ObjectIdentifier { return asn1.ObjectIdentifier{2, 5, 29, 37} }
func oidCodeSigning() asn1.ObjectIdentifier { return asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 3} }

// NewKey makes the identity's key: ECDSA on P-256 (Q-SIG-53).
func NewKey() (*ecdsa.PrivateKey, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("make the signing key: %w", err)
	}
	return k, nil
}

// CertificateRequest is the PKCS#10 request (PEM) for the key and a normalized request: the subject, the URIs and the e-mail addresses
// as alternative names, and the extensions digitalSignature and codeSigning, signed with the key.
func CertificateRequest(key *ecdsa.PrivateKey, r KeyRequest) (string, error) {
	ku, err := asn1.Marshal(asn1.BitString{Bytes: []byte{0x80}, BitLength: 1})
	if err != nil {
		return "", err
	}
	eku, err := asn1.Marshal([]asn1.ObjectIdentifier{oidCodeSigning()})
	if err != nil {
		return "", err
	}
	tmpl := &x509.CertificateRequest{
		Subject:        r.Subject.name(),
		EmailAddresses: r.Emails,
		ExtraExtensions: []pkix.Extension{
			{Id: oidKeyUsage(), Critical: true, Value: ku},
			{Id: oidExtKeyUsage(), Value: eku},
		},
	}
	for _, u := range r.URIs {
		p, err := url.Parse(u)
		if err != nil {
			return "", fmt.Errorf("the URI %q: %w", u, err)
		}
		tmpl.URIs = append(tmpl.URIs, p)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return "", fmt.Errorf("make the certificate request: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})), nil
}

func (s Subject) name() pkix.Name {
	n := pkix.Name{CommonName: s.CN}
	add := func(dst *[]string, v string) {
		if v != "" {
			*dst = append(*dst, v)
		}
	}
	add(&n.Organization, s.O)
	add(&n.OrganizationalUnit, s.OU)
	add(&n.Locality, s.L)
	add(&n.Province, s.ST)
	add(&n.Country, s.C)
	return n
}
