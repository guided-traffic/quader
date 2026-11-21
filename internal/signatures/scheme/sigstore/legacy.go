package sigstore

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"strings"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/tlog"

	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// Cosign's older layout (R-SIG-005, R-SIG-023): a simple-signing payload or a DSSE envelope in a layer, with the signature (base64),
// the certificate and the chain (PEM), the log entry with its signed entry timestamp and an RFC 3161 timestamp in the layer descriptor's
// annotations (names verified in cosign's pkg/oci/static/signature.go). The check converts it into a bundle of version 0.1, which allows
// an inclusion promise without an inclusion proof and a chain beside the leaf, so that one path verifies both layouts and the trust step
// judges the converted bundle (Q-SIG-109).

// mediaBundleV01 is the media type of the converted bundle.
const mediaBundleV01 = "application/vnd.dev.sigstore.bundle+json;version=0.1"

// simpleSigning is cosign's simple-signing payload (cosign's specs/SIGNATURE_SPEC.md).
type simpleSigning struct {
	Critical struct {
		Identity struct {
			DockerReference string `json:"docker-reference"`
		} `json:"identity"`
		Image struct {
			DockerManifestDigest string `json:"docker-manifest-digest"`
		} `json:"image"`
		Type string `json:"type"`
	} `json:"critical"`
}

// legacyRekorBundle is the annotation dev.sigstore.cosign/bundle: a Rekor entry with its signed entry timestamp.
type legacyRekorBundle struct {
	SignedEntryTimestamp string `json:"SignedEntryTimestamp"`
	Payload              struct {
		Body           string `json:"body"`
		IntegratedTime int64  `json:"integratedTime"`
		LogIndex       int64  `json:"logIndex"`
		LogID          string `json:"logID"`
	} `json:"Payload"`
}

// legacyTimestamp is the annotation dev.sigstore.cosign/rfc3161timestamp.
type legacyTimestamp struct {
	SignedRFC3161Timestamp string `json:"SignedRFC3161Timestamp"`
}

// legacyEnvelope is a DSSE envelope as cosign writes it into an .att layer.
type legacyEnvelope struct {
	PayloadType string `json:"payloadType"`
	Payload     string `json:"payload"`
	Signatures  []struct {
		KeyID string `json:"keyid"`
		Sig   string `json:"sig"`
	} `json:"signatures"`
}

// fromSimpleSigning converts a simple-signing layer and its annotations into a bundle with a message signature over the payload.
func fromSimpleSigning(payload []byte, ann map[string]string, l scheme.Limits) (*object, error) {
	if err := checkJSON(payload, l); err != nil {
		return nil, err
	}
	var ss simpleSigning
	if err := json.Unmarshal(payload, &ss); err != nil {
		return nil, malformed("the simple-signing payload does not parse")
	}
	sig, ok := decodeBase64(ann[annSignature])
	if !ok || len(sig) == 0 {
		return nil, malformed("the layer carries no signature annotation")
	}
	vm, notes, err := legacyMaterial(ann, l)
	if err != nil {
		return nil, err
	}
	pb := &protobundle.Bundle{
		MediaType: mediaBundleV01, VerificationMaterial: vm,
		Content: &protobundle.Bundle_MessageSignature{MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: sha256Of(payload)},
			Signature:     sig,
		}},
	}
	o, err := fromProto(pb, nil, l)
	if err != nil {
		return nil, err
	}
	o.legacy, o.payload, o.notes = &ss, payload, notes
	return o, nil
}

// fromLegacyEnvelope converts a DSSE layer of an .att manifest and its annotations into a bundle with the envelope.
func fromLegacyEnvelope(data []byte, ann map[string]string, l scheme.Limits) (*object, error) {
	if err := checkJSON(data, l); err != nil {
		return nil, err
	}
	var env legacyEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, malformed("the DSSE envelope does not parse")
	}
	payload, ok := decodeBase64(env.Payload)
	if !ok {
		return nil, malformed("the DSSE envelope's payload is not base64")
	}
	penv := &protodsse.Envelope{Payload: payload, PayloadType: env.PayloadType}
	for _, s := range env.Signatures {
		sig, ok := decodeBase64(s.Sig)
		if !ok {
			return nil, malformed("a DSSE signature is not base64")
		}
		penv.Signatures = append(penv.Signatures, &protodsse.Signature{Sig: sig, Keyid: s.KeyID})
	}
	vm, notes, err := legacyMaterial(ann, l)
	if err != nil {
		return nil, err
	}
	pb := &protobundle.Bundle{MediaType: mediaBundleV01, VerificationMaterial: vm, Content: &protobundle.Bundle_DsseEnvelope{DsseEnvelope: penv}}
	o, err := fromProto(pb, nil, l)
	if err != nil {
		return nil, err
	}
	o.notes = notes
	return o, nil
}

// legacyMaterial is the verification material of the annotations: the certificate and the chain (or a key of the anchors, with no
// hint), the log entry and the timestamp.
func legacyMaterial(ann map[string]string, l scheme.Limits) (*protobundle.VerificationMaterial, []string, error) {
	ders, err := pemCertificates(ann[annCertificate]+"\n"+ann[annChain], l)
	if err != nil {
		return nil, nil, err
	}
	vm := &protobundle.VerificationMaterial{}
	if len(ders) == 0 {
		vm.Content = &protobundle.VerificationMaterial_PublicKey{PublicKey: &protocommon.PublicKeyIdentifier{}}
	} else {
		chain := &protocommon.X509CertificateChain{}
		for _, d := range ders {
			chain.Certificates = append(chain.Certificates, &protocommon.X509Certificate{RawBytes: d})
		}
		vm.Content = &protobundle.VerificationMaterial_X509CertificateChain{X509CertificateChain: chain}
	}
	var notes []string
	if s := ann[annBundle]; s != "" {
		entry, note, err := legacyEntry(s, l)
		if err != nil {
			return nil, nil, err
		}
		if entry != nil {
			vm.TlogEntries = []*protorekor.TransparencyLogEntry{entry}
		}
		if note != "" {
			notes = append(notes, note)
		}
	}
	if s := ann[annTimestamp]; s != "" {
		ts, err := legacyRFC3161(s, l)
		if err != nil {
			return nil, nil, err
		}
		vm.TimestampVerificationData = &protobundle.TimestampVerificationData{
			Rfc3161Timestamps: []*protocommon.RFC3161SignedTimestamp{{SignedTimestamp: ts}},
		}
	}
	return vm, notes, nil
}

// pemCertificates reads the PEM certificates of the annotations, leaf first, within signatures.max_chain and the size of each.
func pemCertificates(s string, l scheme.Limits) ([][]byte, error) {
	var out [][]byte
	rest := []byte(strings.TrimSpace(s))
	for len(rest) > 0 {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		out = append(out, b.Bytes)
		if len(out) > l.MaxChain {
			return nil, tooLarge("the signature carries more than signatures.max_chain (%d) certificates", l.MaxChain)
		}
	}
	return out, boundChain(len(out), out, l)
}

// legacyEntry converts the annotation dev.sigstore.cosign/bundle into a log entry with its inclusion promise. An entry of a kind
// sigstore-go does not read (the intoto 0.0.1 entries of old cosign attestations) is left out with a note: it proves no time.
func legacyEntry(s string, l scheme.Limits) (*protorekor.TransparencyLogEntry, string, error) {
	if err := checkJSON([]byte(s), l); err != nil {
		return nil, "", err
	}
	var rb legacyRekorBundle
	if err := json.Unmarshal([]byte(s), &rb); err != nil {
		return nil, "", malformed("the log entry annotation does not parse")
	}
	set, okSET := decodeBase64(rb.SignedEntryTimestamp)
	body, okBody := decodeBase64(rb.Payload.Body)
	logID, errID := hex.DecodeString(rb.Payload.LogID)
	if !okSET || !okBody || errID != nil {
		return nil, "", malformed("the log entry annotation is not encoded as cosign writes it")
	}
	if err := checkJSON(body, l); err != nil {
		return nil, "", err
	}
	var kv struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal(body, &kv); err != nil {
		return nil, "", malformed("the log entry's body does not parse")
	}
	tle := &protorekor.TransparencyLogEntry{
		LogIndex: rb.Payload.LogIndex, LogId: &protocommon.LogId{KeyId: logID}, IntegratedTime: rb.Payload.IntegratedTime,
		KindVersion:       &protorekor.KindVersion{Kind: kv.Kind, Version: kv.APIVersion},
		InclusionPromise:  &protorekor.InclusionPromise{SignedEntryTimestamp: set},
		CanonicalizedBody: body,
	}
	if _, perr := tlog.ParseTransparencyLogEntry(tle); perr != nil {
		// An entry kind sigstore-go does not read (the intoto 0.0.1 entries of old attestations) is dropped with a note: it proves no
		// time, and the signature is judged without it (R-SIG-005).
		return nil, "log entry of kind " + kv.Kind + " " + kv.APIVersion + " is not read and proves no time: " + perr.Error(), nil //nolint:nilerr // the note carries the error
	}
	return tle, "", nil
}

// legacyRFC3161 reads the annotation dev.sigstore.cosign/rfc3161timestamp: the timestamp response, DER.
func legacyRFC3161(s string, l scheme.Limits) ([]byte, error) {
	if err := checkJSON([]byte(s), l); err != nil {
		return nil, err
	}
	var ts legacyTimestamp
	if err := json.Unmarshal([]byte(s), &ts); err != nil {
		return nil, malformed("the timestamp annotation does not parse")
	}
	b, ok := decodeBase64(ts.SignedRFC3161Timestamp)
	if !ok || len(b) == 0 {
		return nil, malformed("the timestamp annotation holds no timestamp")
	}
	return b, nil
}

// decodeBase64 decodes standard or URL-safe base64, padded or not (DSSE allows both alphabets).
func decodeBase64(s string) ([]byte, bool) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, true
		}
	}
	return nil, false
}
