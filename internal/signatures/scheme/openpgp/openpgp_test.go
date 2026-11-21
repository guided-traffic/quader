package openpgp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"

	qopenpgp "github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

// The release key of the tests (R-SIG-042): an EdDSA key of go-crypto made by internal/openpgp.Generate an hour ago, signing half an hour
// ago with SHA-512.

func releaseKey(t *testing.T) (*qopenpgp.Key, anchors.OpenPGPCertificate) {
	t.Helper()
	return pkitest.ReleaseKey(t, "Example Release", time.Now().Add(-time.Hour))
}

func detached(t *testing.T, k *qopenpgp.Key, data []byte) []byte {
	t.Helper()
	sig, err := qopenpgp.DetachSign(data, []*qopenpgp.Key{k}, time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

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

// detachedInput is a Release.gpg (or a generic .asc) beside its file: the signature is the object's blob, the file the subject.
func detachedInput(blobs *pkitest.Blobs, sig, file []byte, certs ...anchors.OpenPGPCertificate) scheme.Input {
	return scheme.Input{
		Scheme: signatures.SchemeOpenPGP, Role: signatures.RoleRegistry, ItemBlob: blobs.Put(sig), Subject: blobs.Put(file),
		Blobs: blobs, Keys: scheme.Keys{OpenPGP: certs}, Limits: scheme.DefaultLimits(), ReceivedAt: time.Now(),
	}
}

func TestR_SIG_006_ADetachedSignatureOfTheReleaseKeyIsValid(t *testing.T) {
	k, cert := releaseKey(t)
	release := []byte("Origin: Example\nSuite: stable\nSHA256:\n 0123 42 main/binary-amd64/Packages\n")
	out := run(t, detachedInput(pkitest.NewBlobs(), detached(t, k, release), release, cert))
	wantState(t, out, signatures.StateValid, "")
	r := out.Result
	if len(r.SignedDigest) != sha512Size || r.DigestAlg != "sha512" || out.Parts != 1 || out.Evidence == nil || out.Evidence.Key == nil {
		t.Fatalf("digest %x (%s), parts %d, evidence %+v", r.SignedDigest, r.DigestAlg, out.Parts, out.Evidence)
	}
	var s signatures.Signer
	if err := json.Unmarshal(r.Signer, &s); err != nil {
		t.Fatal(err)
	}
	if s.Fingerprint != cert.Fingerprint || s.Subject != cert.UserID || s.KeyID != cert.Fingerprint || r.ClaimedAt.IsZero() {
		t.Fatalf("signer %+v, claimed %v", s, r.ClaimedAt)
	}
	if !contains(r.KeyIDs, cert.Fingerprint) || !contains(r.KeyIDs, cert.Fingerprint[24:]) {
		t.Fatalf("key IDs %v lack the fingerprint and the key ID of %s", r.KeyIDs, cert.Fingerprint)
	}
}

const sha512Size = 64

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestR_SIG_006_AFlippedByteOfTheContentOrTheSignatureIsInvalid(t *testing.T) {
	k, cert := releaseKey(t)
	release := []byte("Origin: Example\nSuite: stable\n")
	sig := detached(t, k, release)
	flipped := binarySignature(t, sig)
	flipped[len(flipped)-1] ^= 1 // the last octet of the EdDSA signature's S
	for _, tc := range []struct {
		name   string
		in     scheme.Input
		reason string
	}{
		{"content", detachedInput(pkitest.NewBlobs(), sig, []byte("Origin: Example\nSuite: stablE\n"), cert), signatures.ReasonDigestMismatch},
		{"signature", detachedInput(pkitest.NewBlobs(), flipped, release, cert), signatures.ReasonSignatureMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, tc.in)
			wantState(t, out, signatures.StateInvalid, tc.reason)
			if out.Evidence != nil {
				t.Fatalf("an invalid signature has evidence")
			}
		})
	}
}

// binarySignature dearmors an armored signature.
func binarySignature(t *testing.T, armored []byte) []byte {
	t.Helper()
	block, err := armor.Decode(bytes.NewReader(armored))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(block.Body); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestR_SIG_006_AKeyAddedLaterVerifiesTheStoredDigestAsTheContentDoes(t *testing.T) {
	k, cert := releaseKey(t)
	file := bytes.Repeat([]byte("tool.tar.gz "), 100)
	sig := detached(t, k, file)
	blobs := pkitest.NewBlobs()
	unknown := run(t, detachedInput(blobs, sig, file))
	wantState(t, unknown, signatures.StateNoKey, "")
	known := run(t, detachedInput(blobs, sig, file, cert))
	wantState(t, known, signatures.StateValid, "")
	if !bytes.Equal(unknown.Result.SignedDigest, known.Result.SignedDigest) {
		t.Fatalf("the stored digest differs with and without a key")
	}
	m, err := VerifyDigest(unknown.Evidence.Key.Signature, unknown.Result.SignedDigest, cert, time.Now())
	if err != nil || !m.Named || !m.Verified || !m.ValidAt || m.Fingerprint != cert.Fingerprint {
		t.Fatalf("the key added later: %+v, %v", m, err)
	}
	_, other := pkitest.ReleaseKey(t, "Other", time.Now().Add(-time.Hour))
	if m, err := VerifyDigest(unknown.Evidence.Key.Signature, unknown.Result.SignedDigest, other, time.Now()); err != nil || m.Named || m.Verified {
		t.Fatalf("another key: %+v, %v", m, err)
	}
	tampered := bytes.Clone(unknown.Result.SignedDigest)
	tampered[5] ^= 1
	if m, _ := VerifyDigest(unknown.Evidence.Key.Signature, tampered, cert, time.Now()); !m.Named || m.Verified {
		t.Fatalf("a digest of other content verifies: %+v", m)
	}
}

func TestR_SIG_030_AnInReleaseOfTwoKeysHasTwoValidParts(t *testing.T) {
	k1, cert1 := releaseKey(t)
	k2, cert2 := pkitest.ReleaseKey(t, "Example Archive 2", time.Now().Add(-time.Hour))
	text := []byte("Origin: Example\nLabel: Example\nSuite: stable\nDate: Sat, 21 Nov 2026 10:00:00 UTC\n")
	inrelease, err := qopenpgp.ClearSign(text, []*qopenpgp.Key{k1, k2}, time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	blobs := pkitest.NewBlobs()
	d := blobs.Put(inrelease)
	for part := range 2 {
		out := run(t, scheme.Input{
			Scheme: signatures.SchemeOpenPGP, Role: signatures.RoleRegistry, ItemBlob: d, Part: part, Subject: d, ContentBlob: d,
			ContentLocator: locatorCleartext, Blobs: blobs, Keys: scheme.Keys{OpenPGP: []anchors.OpenPGPCertificate{cert1, cert2}},
			Limits: scheme.DefaultLimits(),
		})
		wantState(t, out, signatures.StateValid, "")
		if out.Parts != 2 {
			t.Fatalf("parts %d", out.Parts)
		}
	}
}

func TestR_SIG_031_AnAnsibleCollectionIsCheckedToEveryFile(t *testing.T) {
	k, cert := releaseKey(t)
	plugin := []byte("def run(): pass\n")
	good := buildCollection(t, plugin, sha256Hex(plugin))
	other := buildCollection(t, plugin, sha256Hex([]byte("another plugin")))
	for _, tc := range []struct {
		name  string
		c     collectionArchive
		state signatures.State
	}{
		{"every file matches", good, signatures.StateValid},
		{"FILES.json names another digest", other, signatures.StateInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blobs := pkitest.NewBlobs()
			archive := blobs.Put(tc.c.archive)
			out := run(t, scheme.Input{
				Scheme: signatures.SchemeOpenPGP, Role: signatures.RolePublisher, Raw: detached(t, k, tc.c.manifest), Subject: archive,
				ContentBlob: archive, ContentLocator: locatorEntry + manifestEntry, Blobs: blobs,
				Keys: scheme.Keys{OpenPGP: []anchors.OpenPGPCertificate{cert}}, Limits: scheme.DefaultLimits(),
			})
			reason := ""
			if tc.state == signatures.StateInvalid {
				reason = signatures.ReasonDigestMismatch
			}
			wantState(t, out, tc.state, reason)
		})
	}
}

// collectionArchive is an Ansible collection built by the test: the archive and its MANIFEST.json.
type collectionArchive struct {
	archive, manifest []byte
}

// buildCollection builds a collection of one plugin whose FILES.json names pluginSum, and whose MANIFEST.json names that FILES.json.
func buildCollection(t *testing.T, plugin []byte, pluginSum string) collectionArchive {
	t.Helper()
	files := fmt.Appendf(nil, `{"files":[{"name":".","ftype":"dir","chksum_type":null,"chksum_sha256":null,"format":1},`+
		`{"name":"plugins/run.py","ftype":"file","chksum_type":"sha256","chksum_sha256":%q,"format":1}],"format":1}`, pluginSum)
	manifest := fmt.Appendf(nil, `{"collection_info":{"namespace":"example","name":"tools","version":"1.0.0"},`+
		`"file_manifest_file":{"name":"FILES.json","ftype":"file","chksum_type":"sha256","chksum_sha256":%q,"format":1},"format":1}`, sha256Hex(files))
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"MANIFEST.json", manifest}, {"FILES.json", files}, {"plugins/run.py", plugin}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return collectionArchive{archive: buf.Bytes(), manifest: manifest}
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestR_SIG_013_AWeakHashAndAHostileObjectAreRefused(t *testing.T) {
	e, cert := pkitest.ExpiringKey(t, "Weak", time.Now().Add(-time.Hour), 24*time.Hour)
	data := []byte("a file")
	one := binarySignature(t, pkitest.DetachSign(t, e, data, time.Now().Add(-time.Minute)))
	sha1sig := bytes.Clone(one)
	_, hdr, err := newLength(sha1sig[1:])
	if err != nil {
		t.Fatal(err)
	}
	sha1sig[1+hdr+3] = 2 // go-crypto signs no SHA-1: the packet's hash octet is set to SHA-1 (2) after signing
	many := bytes.Repeat(one, maxPackets+1)
	for _, tc := range []struct {
		name   string
		sig    []byte
		state  signatures.State
		reason string
	}{
		{"SHA-1", sha1sig, signatures.StateUnsupported, signatures.ReasonWeakAlgorithm},
		{"more packets than the bound", many, signatures.StateMalformed, signatures.ReasonLimitsExceeded},
		{"a packet that declares more than follows", []byte{0xc2, 0xff, 0x7f, 0xff, 0xff, 0xff, 4, 0}, signatures.StateMalformed, signatures.ReasonLimitsExceeded},
		{"no packet", []byte("minisign signature"), signatures.StateMalformed, signatures.ReasonContentUnreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantState(t, run(t, detachedInput(pkitest.NewBlobs(), tc.sig, data, cert)), tc.state, tc.reason)
		})
	}
}
