package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/secrets"
)

// R-ARCH-021: quader admin secrets status prints, per key ID, whether it is active, whether the pod's ring holds it, the sealed values
// under it and its canary; the live replicas' rings; and whether they are compatible. Key IDs and counts only.
func TestR_ARCH_021_TheSecretsStatusCommandPrintsTheKeyRingReport(t *testing.T) {
	rep := secrets.Report{
		ActiveKeyID: "2026-10",
		Keys: []secrets.KeyReport{
			{ID: "2026-10", Active: true, Loaded: true, SealedValues: 14, Canary: secrets.CanaryOK},
			{ID: "2026-04", Loaded: true, SealedValues: 0, Canary: secrets.CanaryOK},
			{ID: "2025-10", SealedValues: 2, Canary: secrets.CanaryAbsent},
		},
		Replicas: []secrets.ReplicaRing{
			{Name: "quader-api-6f9c7d8b5-x2v4q", Role: "api", ActiveKeyID: "2026-10", KeyIDs: []string{"2026-04", "2026-10"}},
		},
	}
	var out bytes.Buffer
	if err := writeSecretsStatus(&out, rep); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"active key  2026-10",
		"KEY ID   ACTIVE  IN THIS RING  SEALED VALUES  CANARY",
		"2026-10  yes     yes           14             ok",
		"2026-04  no      yes           0              ok",
		"2025-10  no      no            2              absent",
		"quader-api-6f9c7d8b5-x2v4q  api   2026-10     2026-04, 2026-10",
		"rings compatible  no",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the output lacks %q:\n%s", want, got)
		}
	}
	out.Reset()
	if err := writeSecretsStatus(&out, secrets.Report{ActiveKeyID: "k", RingsCompatible: true}); err != nil ||
		!strings.Contains(out.String(), "no live replica in the registry") || !strings.Contains(out.String(), "rings compatible  yes") {
		t.Errorf("without replicas: %v\n%s", err, out.String())
	}
	if !strings.Contains(adminUsage, "secrets status") {
		t.Error("the admin usage does not list secrets status")
	}
}
