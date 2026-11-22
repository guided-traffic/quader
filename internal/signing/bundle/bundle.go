// Package bundle signs Quader's receipt attestation (signatures.md R-SIG-052, Q-SIG-62, Q-SIG-63): the statement as a DSSE envelope
// (payload type application/vnd.in-toto+json, its pre-authentication encoding, one ECDSA P-256 signature over SHA-256) inside a Sigstore
// bundle v0.3 whose verification material is the identity's leaf certificate alone, with no transparency-log entry and no timestamp. The
// intermediates and the root come from the trusted root Quader serves (signing.TrustedRoot), so a verifier needs that file and no chain
// in the bundle: sigstore-go refuses a chain in a v0.3 bundle unless its verifier opts in, and cosign takes one only with
// --allow-certificate-chain.
//
// The bundle is built from the protobuf types of Sigstore's bundle specification that sigstore-go reads (Q-SIG-182): sigstore-go's own
// sign package would bring a Fulcio client with an OAuth flow (go-oidc, go-jose, a browser opener) into the binary for the one step this
// package takes itself, a DSSE signature with a key the worker holds. Only the job signing in the worker calls Sign: it is the one holder
// of the key (R-SIG-049).
package bundle

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	sgbundle "github.com/sigstore/sigstore-go/pkg/bundle"

	"github.com/guided-traffic/quader/internal/signing/statement"
)

// MediaType is the media type of the bundle (R-SIG-052), the artifact type and the layer type of an image's attestation referrer
// (R-SIG-054), and the content type of the API's answer.
const MediaType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// FileSuffix is what the API's download names a file's bundle after: <file>.quader.sigstore.json (R-SIG-054).
const FileSuffix = ".quader.sigstore.json"

// PAE is DSSE's pre-authentication encoding of a payload: what the signature covers (DSSE v1, the envelope's protocol).
func PAE(payloadType string, payload []byte) []byte {
	return fmt.Appendf(nil, "DSSEv1 %d %s %d %s", len(payloadType), payloadType, len(payload), payload)
}

// Sign signs a statement with the identity's key and answers the bundle's JSON (R-SIG-052). The key must be an ECDSA P-256 key and the
// leaf its certificate; the check of R-SIG-045 made sure of both when the identity was installed. The bundle is read back through
// sigstore-go's bundle type before it is answered, so a bundle the verifier's library would refuse to parse is never stored.
func Sign(stmt []byte, key crypto.Signer, leaf []byte) ([]byte, error) {
	if key == nil {
		return nil, errors.New("bundle: no key")
	}
	if pub, ok := key.Public().(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("bundle: the signing key is not an ECDSA P-256 key")
	}
	if len(leaf) == 0 {
		return nil, errors.New("bundle: the identity has no certificate")
	}
	sum := sha256.Sum256(PAE(statement.PayloadType, stmt))
	sig, err := key.Sign(rand.Reader, sum[:], crypto.SHA256)
	if err != nil {
		return nil, err
	}
	pb := &protobundle.Bundle{
		MediaType: MediaType,
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_Certificate{Certificate: &protocommon.X509Certificate{RawBytes: leaf}},
		},
		Content: &protobundle.Bundle_DsseEnvelope{DsseEnvelope: &protodsse.Envelope{
			Payload: stmt, PayloadType: statement.PayloadType, Signatures: []*protodsse.Signature{{Sig: sig}},
		}},
	}
	b, err := sgbundle.NewBundle(pb)
	if err != nil {
		return nil, fmt.Errorf("bundle: %w", err)
	}
	return b.MarshalJSON()
}

// MaxBytes bounds what Payload reads: a statement of statement.MaxBytes in base64 with the leaf and the signature fits in a quarter of it.
const MaxBytes = 64 << 10

// ErrNotBundle is a bundle Payload does not read: not of MediaType, without a DSSE envelope of an in-toto statement, or above MaxBytes.
var ErrNotBundle = errors.New("bundle: not a receipt's bundle")

// Payload answers the statement of a bundle Sign made (R-SIG-055): its DSSE envelope's payload, which the API decodes for the screens of
// a version and a file. It verifies nothing: its caller reads a bundle of its own store, which Sign read back before it was stored.
func Payload(raw []byte) ([]byte, error) {
	if len(raw) > MaxBytes {
		return nil, ErrNotBundle
	}
	var b struct {
		MediaType    string `json:"mediaType"`
		DSSEEnvelope *struct {
			Payload     []byte `json:"payload"`
			PayloadType string `json:"payloadType"`
		} `json:"dsseEnvelope"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotBundle, err)
	}
	if b.MediaType != MediaType || b.DSSEEnvelope == nil || b.DSSEEnvelope.PayloadType != statement.PayloadType || len(b.DSSEEnvelope.Payload) == 0 {
		return nil, ErrNotBundle
	}
	return b.DSSEEnvelope.Payload, nil
}
