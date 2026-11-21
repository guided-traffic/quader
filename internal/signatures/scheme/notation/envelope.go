package notation

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/veraison/go-cose"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// The media types of a Notation envelope (an OCI layer's, and the scheme of a generic sibling file).
const (
	mediaJWS  = "application/jose+json"
	mediaCOSE = "application/cose"
)

// envelope is a Notation envelope as the check found it: its media type, its bytes, and the certificate chain it carries (DER, leaf
// first), read before the library parses it.
type envelope struct {
	mediaType string
	raw       []byte
	chain     [][]byte
	parts     int
}

// ociManifest is what the check reads of an OCI manifest that carries a Notation signature: its layers.
type ociManifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Layers        []ociLayer `json:"layers"`
}

// ociLayer is a layer descriptor.
type ociLayer struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// load finds the envelope: layer Part of the OCI manifest that is the object (R-SIG-023, Q-SIG-48), or the object itself (a generic
// sibling file, R-SIG-032). It bounds the envelope by signatures.max_object_size and its JSON or CBOR by R-SIG-013, and reads its chain.
func load(ctx context.Context, in scheme.Input) (*envelope, error) {
	obj, err := pki.Object(ctx, in)
	if err != nil {
		return nil, err
	}
	env, err := fromManifest(ctx, in, obj)
	if err != nil {
		return nil, err
	}
	if env == nil {
		env = &envelope{mediaType: mediaJWS, raw: obj, parts: 1}
		if in.Scheme == signatures.SchemeNotationCOSE {
			env.mediaType = mediaCOSE
		}
	}
	switch env.mediaType {
	case mediaJWS:
		env.chain, err = jwsChain(env.raw, in.Limits)
	case mediaCOSE:
		env.chain, err = coseChain(env.raw, in.Limits)
	default:
		err = pki.Unsupported(signatures.ReasonSchemeUnknown, "the layer's media type %q is no Notation envelope", env.mediaType)
	}
	if err != nil {
		return nil, err
	}
	return env, nil
}

// fromManifest reads the envelope from layer Part of an OCI manifest; nil when the object is no manifest.
func fromManifest(ctx context.Context, in scheme.Input, obj []byte) (*envelope, error) {
	if len(obj) == 0 || obj[0] != '{' {
		return nil, nil
	}
	if err := untrusted.CheckJSON(obj, jsonLimits(in.Limits)); err != nil {
		return nil, pki.TooLarge("the object is out of the JSON bounds")
	}
	var m ociManifest
	if err := json.Unmarshal(obj, &m); err != nil || m.SchemaVersion != 2 || len(m.Layers) == 0 {
		return nil, nil //nolint:nilerr // JSON that is no manifest is a JWS envelope itself, read as one below
	}
	if in.Part < 0 || in.Part >= len(m.Layers) {
		return nil, pki.Malformed("the manifest has %d layers, no layer %d", len(m.Layers), in.Part)
	}
	l := m.Layers[in.Part]
	if l.Size > in.Limits.MaxObjectSize {
		return nil, pki.TooLarge("the envelope layer is %d bytes, more than signatures.max_object_size", l.Size)
	}
	digest, ok := sha256Digest(l.Digest)
	if !ok {
		return nil, pki.Malformed("the envelope layer's digest is not a SHA-256")
	}
	raw, err := pki.Blob(ctx, in, digest, in.Limits.MaxObjectSize)
	if err != nil {
		return nil, err
	}
	return &envelope{mediaType: l.MediaType, raw: raw, parts: 1}, nil
}

// jwsChain bounds a JWS envelope's JSON and reads its x5c header.
func jwsChain(raw []byte, l scheme.Limits) ([][]byte, error) {
	if err := untrusted.CheckJSON(raw, jsonLimits(l)); err != nil {
		return nil, pki.TooLarge("the JWS envelope is out of the JSON bounds")
	}
	var e struct {
		Header struct {
			CertChain [][]byte `json:"x5c"`
		} `json:"header"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, pki.Malformed("the JWS envelope does not parse")
	}
	if len(e.Header.CertChain) == 0 {
		return nil, pki.Malformed("the JWS envelope carries no certificate chain")
	}
	return e.Header.CertChain, nil
}

// coseChain bounds a COSE envelope's CBOR and reads its x5chain header (label 33).
func coseChain(raw []byte, l scheme.Limits) ([][]byte, error) {
	if err := pki.CheckCBOR(raw, l.ASN1Depth); err != nil {
		return nil, err
	}
	var msg cose.Sign1Message
	if err := msg.UnmarshalCBOR(raw); err != nil {
		return nil, pki.Malformed("the COSE envelope does not parse")
	}
	var out [][]byte
	switch v := msg.Headers.Unprotected[cose.HeaderLabelX5Chain].(type) {
	case []byte:
		out = append(out, v)
	case []any:
		for _, c := range v {
			der, ok := c.([]byte)
			if !ok {
				return nil, pki.Malformed("the COSE envelope's chain holds something other than certificates")
			}
			out = append(out, der)
		}
	}
	if len(out) == 0 {
		return nil, pki.Malformed("the COSE envelope carries no certificate chain")
	}
	return out, nil
}

func jsonLimits(l scheme.Limits) untrusted.Limits {
	return untrusted.Limits{JSONDepth: l.JSONDepth, JSONMembers: l.JSONValues}
}

// sha256Digest reads "sha256:<64 hex>".
func sha256Digest(s string) ([32]byte, bool) {
	var d [32]byte
	const prefix = "sha256:"
	if len(s) != len(prefix)+64 || s[:len(prefix)] != prefix {
		return d, false
	}
	b, err := hex.DecodeString(s[len(prefix):])
	if err != nil {
		return d, false
	}
	copy(d[:], b)
	return d, true
}
