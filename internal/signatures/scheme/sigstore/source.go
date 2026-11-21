package sigstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// Where the bytes of a signature are (R-SIG-023, R-SIG-028, R-SIG-032, Q-SIG-48): an npm attestation's bundle is the object's Raw bytes;
// a generic <file>.sigstore.json and npm's local .sigstore companion are the object's blob; an OCI signature's object is the manifest
// that carries it, and the signature is its layer Part: a bundle layer, a simple-signing layer of cosign's older layout with the
// signature, certificate, chain, log entry and timestamp in the layer descriptor's annotations, or a DSSE envelope with the same
// annotations. A layer of any other media type (an SBOM) is never fetched.

// The media types and annotations of cosign and Sigstore (cosign's pkg/types/media.go and pkg/oci/static/signature.go).
const (
	mediaBundlePrefix  = "application/vnd.dev.sigstore.bundle"
	mediaSimpleSigning = "application/vnd.dev.cosign.simplesigning.v1+json"
	mediaDSSE          = "application/vnd.dsse.envelope.v1+json"
	annSignature       = "dev.cosignproject.cosign/signature"
	annCertificate     = "dev.sigstore.cosign/certificate"
	annChain           = "dev.sigstore.cosign/chain"
	annBundle          = "dev.sigstore.cosign/bundle"
	annTimestamp       = "dev.sigstore.cosign/rfc3161timestamp"
)

// manifest is what the check reads of an OCI manifest: its layers and their annotations.
type manifest struct {
	Layers []descriptor `json:"layers"`
}

// descriptor is one layer of a manifest.
type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations"`
}

// probe tells a bundle from a manifest.
type probe struct {
	MediaType string          `json:"mediaType"`
	Layers    json.RawMessage `json:"layers"`
}

// load finds the signature's bytes and parses them into an object.
func load(ctx context.Context, in scheme.Input) (*object, error) {
	data, err := objectBytes(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := checkJSON(data, in.Limits); err != nil {
		return nil, err
	}
	var p probe
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, malformed("the signature is not a JSON object")
	}
	switch {
	case strings.HasPrefix(p.MediaType, mediaBundlePrefix):
		return fromBundle(data, in.Limits)
	case len(p.Layers) > 0:
		return fromManifest(ctx, in, data)
	}
	return nil, malformed("the signature is neither a Sigstore bundle nor an OCI manifest")
}

// objectBytes returns the object: its Raw bytes or its blob, and the part of it its locator names.
func objectBytes(ctx context.Context, in scheme.Input) ([]byte, error) {
	data := in.Raw
	if len(data) == 0 {
		var err error
		if data, err = blob(ctx, in, in.ItemBlob); err != nil {
			return nil, err
		}
	}
	if int64(len(data)) > in.Limits.MaxObjectSize {
		return nil, tooLarge("the signature is larger than signatures.max_object_size")
	}
	if in.Locator == "" {
		return data, nil
	}
	ptr, ok := strings.CutPrefix(in.Locator, "#")
	if !ok {
		return nil, malformed("the place %q inside the stored file is not one this scheme reads", in.Locator)
	}
	return pointer(data, ptr, in.Limits)
}

// blob reads a stored blob, at most signatures.max_object_size bytes, and checks that it is the blob of its digest.
func blob(ctx context.Context, in scheme.Input, digest [32]byte) ([]byte, error) {
	if in.Blobs == nil {
		return nil, transient(errors.New("sigstore: no blob store"))
	}
	c, err := in.Blobs.Content(ctx, digest)
	if errors.Is(err, scheme.ErrBlobMissing) {
		return nil, malformed("the stored content of the signature is gone")
	}
	if err != nil {
		return nil, transient(err)
	}
	if c.Size() > in.Limits.MaxObjectSize {
		return nil, tooLarge("a part of the signature is larger than signatures.max_object_size")
	}
	b, err := c.Bytes(in.Limits.MaxObjectSize)
	if err != nil {
		return nil, transient(err)
	}
	if sha256.Sum256(b) != digest {
		return nil, malformed("the stored content does not match its digest")
	}
	return b, nil
}

// pointer resolves a JSON pointer (RFC 6901) inside a document and returns the value it names as JSON.
func pointer(data []byte, ptr string, l scheme.Limits) ([]byte, error) {
	if err := checkJSON(data, l); err != nil {
		return nil, err
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, malformed("the stored document is not JSON")
	}
	for _, tok := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		switch node := v.(type) {
		case map[string]any:
			v = node[tok]
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(node) {
				return nil, malformed("the place %q inside the stored document does not exist", ptr)
			}
			v = node[i]
		default:
			return nil, malformed("the place %q inside the stored document does not exist", ptr)
		}
	}
	out, err := json.Marshal(v)
	if err != nil || v == nil {
		return nil, malformed("the place %q inside the stored document holds nothing", ptr)
	}
	return out, nil
}

// fromManifest follows an OCI signature manifest to its layer Part and reads the signature it holds.
func fromManifest(ctx context.Context, in scheme.Input, data []byte) (*object, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, malformed("the signature manifest does not parse")
	}
	if in.Part < 0 || in.Part >= len(m.Layers) {
		return nil, malformed("the signature manifest has no layer %d", in.Part)
	}
	d := m.Layers[in.Part]
	kind := layerKind(d.MediaType)
	if kind == "" {
		return nil, unsupported(signatures.ReasonSchemeUnknown, "the layer of media type %q holds no signature Quader reads", d.MediaType)
	}
	digest, ok := sha256Digest(d.Digest)
	if !ok {
		return nil, malformed("the layer's digest %q is not a SHA-256 digest", d.Digest)
	}
	if d.Size > in.Limits.MaxObjectSize {
		return nil, tooLarge("the signature layer is larger than signatures.max_object_size")
	}
	layer, err := blob(ctx, in, digest)
	if err != nil {
		return nil, err
	}
	var o *object
	switch kind {
	case mediaBundlePrefix:
		o, err = fromBundle(layer, in.Limits)
	case mediaSimpleSigning:
		o, err = fromSimpleSigning(layer, d.Annotations, in.Limits)
	default:
		o, err = fromLegacyEnvelope(layer, d.Annotations, in.Limits)
	}
	if err != nil {
		return nil, err
	}
	o.parts = signatureLayers(m)
	return o, nil
}

// layerKind is the kind of signature a layer holds by its media type, "" for none.
func layerKind(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, mediaBundlePrefix):
		return mediaBundlePrefix
	case mediaType == mediaSimpleSigning, mediaType == mediaDSSE:
		return mediaType
	}
	return ""
}

// signatureLayers counts the layers of a manifest that hold a signature: the parts of a cosign signature manifest (R-SIG-023).
func signatureLayers(m manifest) int {
	n := 0
	for _, l := range m.Layers {
		if layerKind(l.MediaType) != "" {
			n++
		}
	}
	return n
}

// sha256Digest parses an OCI digest "sha256:<64 hex>".
func sha256Digest(s string) ([32]byte, bool) {
	var d [32]byte
	h, ok := strings.CutPrefix(s, "sha256:")
	if !ok || len(h) != 64 {
		return d, false
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		return d, false
	}
	copy(d[:], b)
	return d, true
}

// checkJSON bounds a JSON document before any decoder reads it (R-SIG-013): a crossed bound is malformed (limits_exceeded).
func checkJSON(data []byte, l scheme.Limits) error {
	if err := untrusted.CheckJSON(data, untrusted.Limits{JSONDepth: l.JSONDepth, JSONMembers: l.JSONValues}); err != nil {
		return tooLarge("a JSON document of the signature is nested deeper or holds more values than the bounds allow")
	}
	return nil
}
