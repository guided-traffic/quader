package store

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/trust"
)

func publicKeyMaterial(t *testing.T) anchors.Material {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return anchors.Material{PublicKey: &anchors.PublicKeyMaterial{Keys: []anchors.PublicKeyEntry{{
		PEM: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})),
	}}}}
}

func fields(t *testing.T, err error) []errs.FieldError {
	t.Helper()
	e, ok := errs.As(err)
	if !ok || e.Kind != errs.Invalid || e.Code != errs.CodeValidation {
		t.Fatalf("want validation_failed, got %v", err)
	}
	return e.Fields
}

// TestR_SIG_015_TheChecksOnSaveAnswerAtTheMember: a create is refused before any write with the member's pointer and the code of
// R-SIG-015: a key out of its pattern, an unknown kind, a scope the API does not create, and a private key in the material
// (secret_material at the member that holds it).
func TestR_SIG_015_TheChecksOnSaveAnswerAtTheMember(t *testing.T) {
	s := New(Options{})
	ctx := context.Background()
	_, err := s.CreateAnchor(ctx, audit.Actor{}, signatures.AnchorInput{Scope: anchors.ScopeShipped, Key: "Bad Key", Title: "", Kind: "x"})
	got := map[string]string{}
	for _, f := range fields(t, err) {
		got[f.Pointer] = f.Code
	}
	for _, p := range []string{"/scope", "/key", "/title", "/kind"} {
		if got[p] != anchors.CodeInvalidValue {
			t.Errorf("%s: %q, want invalid_value (all: %v)", p, got[p], got)
		}
	}
	secret := `{"publicKey":{"keys":[{"pem":"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"}]}}`
	_, err = s.CreateAnchor(ctx, audit.Actor{}, signatures.AnchorInput{
		Scope: anchors.ScopeInstallation, Key: "k", Title: "K", Kind: anchors.KindPublicKey, Material: json.RawMessage(secret),
	})
	fe := fields(t, err)
	if len(fe) != 1 || fe[0].Code != anchors.CodeSecretMaterial || fe[0].Pointer != "/publicKey/keys/0/pem" {
		t.Fatalf("secret material answered %+v", fe)
	}
	_, err = s.CreateAnchor(ctx, audit.Actor{}, signatures.AnchorInput{
		Scope: anchors.ScopeInstallation, Key: "k", Title: "K", Kind: anchors.KindPublicKey, Material: json.RawMessage(`{"other":{}}`),
	})
	if fe := fields(t, err); len(fe) != 1 || fe[0].Code != anchors.CodeInvalidValue {
		t.Fatalf("an unknown member answered %+v", fe)
	}
}

// TestR_SIG_020_TheChecksOfABlock: the kind, a value of 1 to 1024 characters, an identity pattern that compiles, an issuer only for an
// identity, and a reason of 1 to 1000 characters.
func TestR_SIG_020_TheChecksOfABlock(t *testing.T) {
	ok := []signatures.BlockInput{
		{Kind: trust.BlockFingerprint, Value: "AB12", Reason: "key leaked"},
		{Kind: trust.BlockIdentity, Value: "https://gitlab.example.com/.+", Issuer: "https://gitlab.example.com", Reason: "r"},
	}
	for _, in := range ok {
		if err := checkBlock(in); err != nil {
			t.Errorf("%+v refused: %v", in, err)
		}
	}
	bad := map[string]signatures.BlockInput{
		"/kind":   {Kind: "subject", Value: "v", Reason: "r"},
		"/value":  {Kind: trust.BlockIdentity, Value: "(", Reason: "r"},
		"/issuer": {Kind: trust.BlockKeyID, Value: "v", Issuer: "https://x", Reason: "r"},
		"/reason": {Kind: trust.BlockSerial, Value: "v", Reason: " "},
	}
	for pointer, in := range bad {
		fe := fields(t, checkBlock(in))
		if len(fe) != 1 || fe[0].Pointer != pointer {
			t.Errorf("%+v: %+v, want one refusal at %s", in, fe, pointer)
		}
	}
}

// TestR_SIG_039_WhatNarrowsIsJournaled: switching an anchor off and removing material narrow (Q-SIG-42); switching it on and adding
// material do not; the replay's comparator agrees.
func TestR_SIG_039_WhatNarrowsIsJournaled(t *testing.T) {
	a, b := publicKeyMaterial(t), publicKeyMaterial(t)
	both := anchors.Material{PublicKey: &anchors.PublicKeyMaterial{Keys: append(append([]anchors.PublicKeyEntry{}, a.PublicKey.Keys...), b.PublicKey.Keys...)}}
	enc := func(m anchors.Material) json.RawMessage {
		raw, err := encodeMaterial(m)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	on := func(m anchors.Material) AnchorState { return AnchorState{Enabled: true, Material: enc(m)} }
	off := func(m anchors.Material) AnchorState { return AnchorState{Enabled: false, Material: enc(m)} }
	k := anchors.KindPublicKey
	cases := []struct {
		name          string
		before, after AnchorState
		narrows       bool
		// replay is what the replay's comparator says: a replacement removes and adds, so the replay fails closed (suspends).
		replay bool
	}{
		{"switched off", on(a), off(a), true, true},
		{"switched on", off(a), on(a), false, false},
		{"material removed", on(both), on(a), true, true},
		{"material added", on(a), on(both), false, false},
		{"material replaced", on(a), on(b), true, false},
		{"title only", on(a), on(a), false, true},
	}
	for _, c := range cases {
		if got := narrows(k, c.before, c.after); got != c.narrows {
			t.Errorf("%s: narrows %v, want %v", c.name, got, c.narrows)
		}
		if got := (anchorNarrower{kind: k}).Narrows(c.before, c.after); got != c.replay {
			t.Errorf("%s: the replay's comparator says %v, want %v", c.name, got, c.replay)
		}
	}
}

// TestR_SIG_041_TheReplayRefusesWhatIsNoEntry: an entry that does not read is reported malformed before anything is written.
func TestR_SIG_041_TheReplayRefusesWhatIsNoEntry(t *testing.T) {
	ctx := context.Background()
	for name, apply := range map[string]func(context.Context, meta.Tx, journal.Env, journal.Object) (journal.Result, error){
		"removed": replayAnchorRemoved, "updated": replayAnchorUpdated, "blocked": replayBlock, "switch": replaySwitchOff,
	} {
		r, err := apply(ctx, nil, journal.Env{}, journal.Object{Entry: json.RawMessage(`{"projectAnchors":true,"kind":"x","id":-1}`)})
		if err != nil || r.Outcome != journal.Skipped {
			t.Errorf("%s: %+v, %v", name, r, err)
		}
	}
}

// TestJSONPointer: the value at a pointer of a document, with escapes, and nothing for a pointer that names nothing.
func TestJSONPointer(t *testing.T) {
	doc := []byte(`{"dist":{"signatures":[{"keyid":"a"},{"keyid":"b/c~"}]}}`)
	got, err := jsonPointer(doc, "/dist/signatures/1", 1024)
	if err != nil || string(got) != `{"keyid":"b/c~"}` {
		t.Fatalf("pointer: %s, %v", got, err)
	}
	if _, err := jsonPointer(doc, "/dist/signatures/2", 1024); err == nil {
		t.Fatal("an index past the end answered a value")
	}
	if got := int64Array([]int64{3, 1}); got != "{3,1}" {
		t.Fatalf("array %s", got)
	}
}
