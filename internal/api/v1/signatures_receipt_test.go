package v1

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signing/bundle"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
	"github.com/guided-traffic/quader/internal/signing/statement"
)

// receiptBundle is a receipt the job would store for the fixture's tarball: an upload from a pipeline, a provenance, a clean verdict and
// an evaluation, signed by a leaf of a CA of the test.
func receiptBundle(t *testing.T, tgz [32]byte) []byte {
	t.Helper()
	at := time.Date(2026, 11, 21, 10, 2, 3, 456_000_000, time.UTC)
	stmt, err := statement.Build(statement.Facts{
		InstallationID: "6c3e", BaseURL: "https://quader.example.com", Host: "quader.example.com", Project: "web", Repository: "npm-local",
		Format: "npm", Class: "local", Path: "ui/-/ui-1.0.0.tgz", Name: "ui", Version: "1.0.0", Size: 18342, SHA256: tgz, ReceivedAt: at,
		IssuedAt: at.Add(3 * time.Second),
		Origin: statement.Origin{Kind: statement.OriginUpload, PrincipalKind: "ci_mapping", PrincipalName: "gitlab-platform", CI: map[string]any{
			"issuer": "gitlab", "project_path": "web/ui", "pipeline_id": "48211", "ref": "main", "ref_protected": true,
		}},
		Signatures: []statement.Signature{{Role: "provenance", Scheme: "sigstore-bundle", Integrity: "valid", Identity: "https://gitlab.example.com/web/ui//.gitlab-ci.yml@refs/heads/main"}},
		Malware:    statement.Malware{State: "clean", Database: "main-62.daily-27450", DatabaseBuiltAt: at.Add(-4 * time.Hour), ScannedAt: at.Add(2 * time.Second)},
		Vulnerabilities: statement.Vulnerabilities{
			State: "evaluated", Grade: "A", Counts: &statement.Counts{Low: 2}, DataAt: at.Add(-6 * time.Hour),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ca := signingtest.NewCA(t, "Example Corp")
	leaf, key := ca.Issue(t, signingtest.Leaf{})
	raw, err := bundle.Sign(stmt, key, leaf.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestR_SIG_055_QuadersReceiptInTheSignaturesAnswers: the signatures answer lists Quader's receipt attestation apart, issued, with its
// identity, what its statement says (the time of receipt and of issue, the origin with the pipeline's claims, the integrity states, the
// malware and the vulnerability state with their dates) and where it is served, and with the reason for an image served through the API;
// a bundle that does not decode keeps where it is served and leaves the statement out; every answer validates against the API document.
// Unit, 0.03 s in the unit job's mode once the package's shared parse is done, a bundle of about 2.5 KiB signed by a CA of the test.
func TestR_SIG_055_QuadersReceiptInTheSignaturesAnswers(t *testing.T) {
	sv := func(st *fakeSigStore) SignatureServices {
		return SignatureServices{Store: st, Enabled: true, Repos: sigRepos()}
	}
	schema := specDocument(t).Components.Schemas["SignatureVersionReport"]
	if schema == nil {
		t.Fatal("the API document has no SignatureVersionReport")
	}
	receipt := func(st *fakeSigStore, tgz [32]byte, r *signatures.Receipt) {
		v := sigViewFor(11, tgz, signatures.RoleAttestation, signatures.StateValid, nil, 1)
		v.Signature.PredicateType, v.Signature.TimeProof = statement.PredicateType, ""
		v.Signature.Signer = mustJSON(signatures.Signer{Subject: "CN=Quader signing", Issuer: "CN=Example Code Signing CA", Identity: "https://quader.example.com"})
		v.Attachments[0].Source = "issued"
		v.IssuedBy = &signatures.IssuedBy{Identity: 3, Subject: "CN=Quader signing", Fingerprint: strings.Repeat("ab", 32), State: "current"}
		v.Receipt = r
		st.views = append(st.views, v)
	}
	answer := func(t *testing.T, st *fakeSigStore) map[string]any {
		t.Helper()
		_, in, _ := sigFixture()
		out, err := (&Server{}).versionSignatureReport(context.Background(), sv(st), in, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(out)
		var doc map[string]any
		_ = json.Unmarshal(b, &doc)
		if err := schema.Value.VisitJSON(doc); err != nil {
			t.Fatalf("the answer does not validate: %v\n%s", err, b)
		}
		return doc
	}
	ownReceipt := func(doc map[string]any) map[string]any {
		local := doc["repositories"].([]any)[0].(map[string]any)
		for _, a := range local["attestations"].([]any) {
			if e := a.(map[string]any); e["id"] == float64(11) {
				return e
			}
		}
		return nil
	}

	t.Run("a file's receipt, served through the API", func(t *testing.T) {
		st, _, tgz := sigFixture()
		receipt(st, tgz, &signatures.Receipt{Bundle: receiptBundle(t, tgz), ServedAs: "api"})
		doc := answer(t, st)
		e := ownReceipt(doc)
		if e == nil || e["source"] != "issued" || e["servedAs"] != "api" || e["servedReason"] != nil || e["issuedBy"] == nil || e["trust"] != nil {
			t.Fatalf("%v", e)
		}
		att, _ := e["attestation"].(map[string]any)
		if att == nil || att["receivedAt"] != "2026-11-21T10:02:03.456Z" || att["issuedAt"] != "2026-11-21T10:02:06.456Z" {
			t.Fatalf("%v", att)
		}
		origin := att["origin"].(map[string]any)
		ci, _ := origin["ci"].(map[string]any)
		if origin["kind"] != "uploaded" || origin["principalName"] != "gitlab-platform" || ci == nil || ci["projectPath"] != "web/ui" || ci["refProtected"] != true {
			t.Fatalf("origin %v", origin)
		}
		malware := att["malware"].(map[string]any)
		vulns := att["vulnerabilities"].(map[string]any)
		sigs := att["signatures"].([]any)
		if malware["state"] != "clean" || malware["database"] != "main-62.daily-27450" || malware["scannedAt"] == nil || malware["released"] != false {
			t.Fatalf("malware %v", malware)
		}
		if vulns["state"] != "evaluated" || vulns["grade"] != "A" || vulns["counts"].(map[string]any)["low"] != float64(2) || vulns["dataAt"] == nil {
			t.Fatalf("vulnerabilities %v", vulns)
		}
		if len(sigs) != 1 || sigs[0].(map[string]any)["integrity"] != "valid" || sigs[0].(map[string]any)["role"] != "provenance" {
			t.Fatalf("signatures %v", sigs)
		}
		malware["state"] = "safe"
		if err := schema.Value.VisitJSON(doc); err == nil {
			t.Fatal("the schema takes a malware state it does not name: the validation above proves nothing")
		}
	})
	t.Run("an image's receipt served through the API names why", func(t *testing.T) {
		st, _, tgz := sigFixture()
		receipt(st, tgz, &signatures.Receipt{Bundle: receiptBundle(t, tgz), ServedAs: "api", Reason: "hides_signature"})
		if e := ownReceipt(answer(t, st)); e == nil || e["servedAs"] != "api" || e["servedReason"] != "hides_signature" {
			t.Fatalf("%v", e)
		}
	})
	t.Run("a bundle that does not decode keeps where it is served", func(t *testing.T) {
		st, _, tgz := sigFixture()
		receipt(st, tgz, &signatures.Receipt{Bundle: []byte(`{"mediaType":"application/octet-stream"}`), ServedAs: "referrer"})
		if e := ownReceipt(answer(t, st)); e == nil || e["servedAs"] != "referrer" || e["attestation"] != nil {
			t.Fatalf("%v", e)
		}
	})
	t.Run("a signature that is no receipt carries none of the three", func(t *testing.T) {
		st, _, _ := sigFixture()
		for _, a := range answer(t, st)["repositories"].([]any)[0].(map[string]any)["attestations"].([]any) {
			e := a.(map[string]any)
			if e["attestation"] != nil || e["servedAs"] != nil || e["servedReason"] != nil {
				t.Fatalf("%v", e)
			}
		}
	})
}
