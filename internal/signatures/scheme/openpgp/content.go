package openpgp

import (
	"bytes"
	"context"
	"hash"
	"strings"

	pgp "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// memoryBound bounds a content the check holds in memory (the cleartext of an inline signature, R-SIG-013): the smaller of
// signatures.max_content_size and 64 MiB, four times internal/openpgp's bound of a release document.
const memoryBound = 64 << 20

// digestOf computes the digest the packet commits to over the signed content, read once (Q-SIG-23): the packet's salt (version 6), the
// content (canonicalised for a text signature), the packet's trailer.
func digestOf(ctx context.Context, in scheme.Input, obj *object, sig *packet.Signature) ([]byte, error) {
	h, err := sig.PrepareVerify()
	if err != nil {
		return nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "the signature's hash cannot be computed")
	}
	w := h
	if sig.SigType == packet.SigTypeText {
		w = pgp.NewCanonicalTextHash(h)
	}
	if err := writeContent(ctx, in, obj, w); err != nil {
		return nil, err
	}
	h.Write(sig.HashSuffix)
	return h.Sum(nil), nil
}

// writeContent writes the signed content into w by the row's content locator.
func writeContent(ctx context.Context, in scheme.Input, obj *object, w hash.Hash) error {
	switch in.ContentLocator {
	case "":
		blob := in.ContentBlob
		if blob == ([32]byte{}) {
			blob = in.Subject
		}
		return pki.Stream(ctx, in, blob, w)
	case locatorCleartext:
		text, err := cleartext(ctx, in, obj)
		if err != nil {
			return err
		}
		_, _ = w.Write(text)
		return nil
	case locatorEntry + manifestEntry:
		manifest, err := ansibleManifest(ctx, in)
		if err != nil {
			return err
		}
		_, _ = w.Write(manifest)
		return nil
	}
	return pki.Malformed("the content locator %q is not one an OpenPGP signature names", in.ContentLocator)
}

// cleartext is the signed text of an inline signature: the object's own message when the content is the object (an InRelease), else the
// message of the content blob.
func cleartext(ctx context.Context, in scheme.Input, obj *object) ([]byte, error) {
	blob := in.ContentBlob
	if blob == ([32]byte{}) {
		blob = in.Subject
	}
	if obj.clear != nil && len(in.Raw) == 0 && blob == in.ItemBlob {
		return obj.clear.Bytes, nil
	}
	data, err := pki.Blob(ctx, in, blob, min(in.Limits.MaxContentSize, memoryBound))
	if err != nil {
		return nil, err
	}
	block, rest := clearsign.Decode(bytes.TrimLeft(data, " \t\r\n"))
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, pki.Malformed("the content is not a cleartext signed message")
	}
	return block.Bytes, nil
}

// trimEntry spells an archive entry's name without a leading "./".
func trimEntry(name string) string { return strings.TrimPrefix(name, "./") }
