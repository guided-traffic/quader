package rawkey

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

const npmKeyID = "SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA"

func run(t *testing.T, in scheme.Input) scheme.Output {
	t.Helper()
	out, err := Check(context.Background(), in)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return out
}

func wantState(t *testing.T, out scheme.Output, state signatures.State, reason string) {
	t.Helper()
	if out.Result.State != state || out.Result.Reason != reason {
		t.Fatalf("state %s (%s), want %s (%s); signer %s", out.Result.State, out.Result.Reason, state, reason, out.Result.Signer)
	}
}

func sign(t *testing.T, k *ecdsa.PrivateKey, digest []byte) []byte {
	t.Helper()
	sig, err := ecdsa.SignASN1(rand.Reader, k, digest)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// npmInput is an npm registry signature over "<name>@<version>:<integrity>": the entry {keyid, sig} as bytes, the message's digest kept
// by the row (Q-SIG-50).
func npmInput(message string, sig []byte, keys ...anchors.ParsedKey) scheme.Input {
	digest := sha256.Sum256([]byte(message))
	return scheme.Input{
		Scheme: signatures.SchemeNPMRegistry, Role: signatures.RoleRegistry, SubjectName: "left-pad@1.3.0",
		Raw:          fmt.Appendf(nil, `{"keyid":%q,"sig":%q}`, npmKeyID, base64.StdEncoding.EncodeToString(sig)),
		SignedDigest: digest[:], DigestAlg: "sha256", Keys: scheme.Keys{Public: keys}, Limits: scheme.DefaultLimits(), ReceivedAt: time.Now(),
	}
}

func TestR_SIG_006_AnNPMRegistrySignatureIsCheckedWithTheKeyItsKeyIDNames(t *testing.T) {
	k := pkitest.Key(t)
	key := anchors.ParsedKey{Key: k.Public(), KeyID: npmKeyID, SHA256: pki.KeyID(k.Public())}
	message := "left-pad@1.3.0:sha512-1hX9M2fsPT4gLk3l1qU0bQ=="
	digest := sha256.Sum256([]byte(message))
	sig := sign(t, k, digest[:])
	flipped := append([]byte(nil), sig...)
	flipped[len(flipped)-1] ^= 1
	expired := key
	expired.Expires = time.Now().Add(-24 * time.Hour)
	for _, tc := range []struct {
		name   string
		in     scheme.Input
		state  signatures.State
		reason string
	}{
		{"valid", npmInput(message, sig, key), signatures.StateValid, ""},
		{"a flipped byte of the message", npmInput(message+"x", sig, key), signatures.StateInvalid, signatures.ReasonSignatureMismatch},
		{"a flipped byte of the signature", npmInput(message, flipped, key), signatures.StateInvalid, signatures.ReasonSignatureMismatch},
		{"no key", npmInput(message, sig), signatures.StateNoKey, ""},
		{"a key that expired before the receipt", npmInput(message, sig, expired), signatures.StateNoKey, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, tc.in)
			wantState(t, out, tc.state, tc.reason)
			if tc.state == signatures.StateInvalid {
				return
			}
			r := out.Result
			if r.DigestAlg != "sha256" || len(r.SignedDigest) != sha256.Size || out.Evidence == nil || out.Evidence.Key.KeyID != npmKeyID {
				t.Fatalf("digest %x (%s), evidence %+v", r.SignedDigest, r.DigestAlg, out.Evidence)
			}
			if kind, ok := Verify(out.Evidence.Key.Kind, k.Public(), out.Evidence.Key.Hash, r.SignedDigest, out.Evidence.Key.Signature); !ok || kind != KindECDSA {
				t.Fatalf("the stored digest does not verify with the key added later")
			}
		})
	}
}

func TestR_SIG_032_ACosignSignBlobSignatureIsCheckedWithEveryKnownKey(t *testing.T) {
	k, other := pkitest.Key(t), pkitest.Key(t)
	key := anchors.ParsedKey{Key: k.Public(), SHA256: pki.KeyID(k.Public())}
	otherKey := anchors.ParsedKey{Key: other.Public(), SHA256: pki.KeyID(other.Public())}
	file := []byte("tool.tar.gz, a kilobyte of it")
	digest := sha256.Sum256(file)
	sigFile := []byte(base64.StdEncoding.EncodeToString(sign(t, k, digest[:])) + "\n")
	input := func(f []byte, keys ...anchors.ParsedKey) scheme.Input {
		blobs := pkitest.NewBlobs()
		return scheme.Input{
			Scheme: signatures.SchemePublicKey, Role: signatures.RolePublisher, ItemBlob: blobs.Put(sigFile), Subject: sha256.Sum256(f),
			Blobs: blobs, Keys: scheme.Keys{Public: keys}, Limits: scheme.DefaultLimits(), ReceivedAt: time.Now(),
		}
	}
	out := run(t, input(file, otherKey, key))
	wantState(t, out, signatures.StateValid, "")
	if out.Result.KeyIDs[0] != key.SHA256 || out.Evidence.Key.Kind != KindECDSA {
		t.Fatalf("key IDs %v, evidence %+v", out.Result.KeyIDs, out.Evidence.Key)
	}
	// A signature that names no key cannot tell another file from an unknown key: no known key verifies it.
	wantState(t, run(t, input([]byte("tool.tar.gz, replaced"), key)), signatures.StateNoKey, "")
	wantState(t, run(t, input(file, otherKey)), signatures.StateNoKey, "")
}
