package secrets

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

func TestRingsCompatible(t *testing.T) {
	rr := func(name, role, active string, ids ...string) ReplicaRing {
		return ReplicaRing{Name: name, Role: role, ActiveKeyID: active, KeyIDs: ids}
	}
	cases := []struct {
		name       string
		rings      []ReplicaRing
		referenced []string
		want       bool
	}{
		{"all rings equal", []ReplicaRing{rr("a", "web", "k1", "k1"), rr("b", "worker", "k1", "k1")}, []string{"k1"}, true},
		{"a replica lacks a key that is active elsewhere", []ReplicaRing{rr("a", "web", "k1", "k1"), rr("b", "web", "k2", "k1", "k2")}, nil, false},
		{"step 2 rolled out before step 1 reached every replica", []ReplicaRing{rr("a", "web", "k2", "k1"), rr("b", "worker", "k2", "k1", "k2")}, nil, false},
		{"a replica lacks a key that sealed values reference", []ReplicaRing{rr("a", "web", "k2", "k2"), rr("b", "web", "k2", "k1", "k2")}, []string{"k1"}, false},
		{"after step 1: new key present everywhere, old one active", []ReplicaRing{rr("a", "web", "k1", "k1", "k2"), rr("b", "web", "k1", "k1", "k2")}, []string{"k1"}, true},
		{"a lost replica is left out", []ReplicaRing{rr("a", "web", "k2", "k1", "k2"), {Name: "gone", Role: "web", Lost: true, ActiveKeyID: "k1", KeyIDs: []string{"k1"}}}, nil, true},
		{"a role that opens no sealed value is left out", []ReplicaRing{rr("a", "web", "k1", "k1"), rr("backup", "backup", "k0", "k0")}, nil, true},
		{"no replica", nil, nil, true},
	}
	for _, c := range cases {
		t.Run("R-API-049 "+c.name, func(t *testing.T) {
			if got := RingsCompatible(c.rings, c.referenced); got != c.want {
				t.Fatalf("RingsCompatible = %v, want %v", got, c.want)
			}
		})
	}
	t.Run("R-OPS-004 the ring of a replica of every role counts, and a role that never registers does not", func(t *testing.T) {
		for _, role := range []string{"api", "packages", "worker", "web", "all"} {
			if !opensSecrets(role) {
				t.Errorf("a %s replica is left out of the key ring rule", role)
			}
		}
		if opensSecrets("backup") || opensSecrets("") {
			t.Error("a role that never registers counts")
		}
	})
}

// R-ARCH-021, R-API-049: the one report of the command and the endpoint names per key ID its sealed values and its canary state, the
// live replicas' rings and their compatibility, and nothing of a key's material.
func TestR_ARCH_021_TheKeyRingReport(t *testing.T) {
	resetRegistry(t)
	Register("webhooks", "id", "secret")
	r := ring(t, "new", "old", "new")
	okCanary, err := r.SealCanary("new")
	if err != nil {
		t.Fatal(err)
	}
	other, err := Load(config.Secrets{EncryptionKeyFiles: []string{writeKey(t, t.TempDir(), "old", 200)}, ActiveKeyID: "old"})
	if err != nil {
		t.Fatal(err)
	}
	wrongCanary, err := other.SealCanary("old") // sealed by other key material under the same ID
	if err != nil {
		t.Fatal(err)
	}
	counter := fakeCounter{"webhooks.secret": {"new": 3, "old": 1, "gone": 2}}
	rings := []ReplicaRing{
		{Name: "quader-worker-b", Role: "worker", ActiveKeyID: "new", KeyIDs: []string{"old", "new"}},
		{Name: "quader-api-a", Role: "api", ActiveKeyID: "new", KeyIDs: []string{"new"}},
		{Name: "quader-api-lost", Role: "api", Lost: true, ActiveKeyID: "old", KeyIDs: []string{"old"}},
	}
	rep, err := r.BuildReport(context.Background(), counter, map[string]string{"new": okCanary, "old": wrongCanary}, rings)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]KeyReport{}
	for _, k := range rep.Keys {
		got[k.ID] = k
	}
	switch {
	case rep.ActiveKeyID != "new" || len(rep.Keys) != 3:
		t.Fatalf("%+v", rep)
	case got["new"] != KeyReport{ID: "new", Active: true, Loaded: true, SealedValues: 3, Canary: CanaryOK}:
		t.Errorf("new: %+v", got["new"])
	case got["old"] != KeyReport{ID: "old", Loaded: true, SealedValues: 1, Canary: CanaryFailed}:
		t.Errorf("old: %+v", got["old"])
	case got["gone"] != KeyReport{ID: "gone", SealedValues: 2, Canary: CanaryAbsent}:
		t.Errorf("gone: %+v", got["gone"])
	}
	if len(rep.Replicas) != 2 || rep.Replicas[0].Name != "quader-api-a" || strings.Join(rep.Replicas[1].KeyIDs, ",") != "new,old" {
		t.Errorf("replicas %+v: the live ones, by name, their key IDs sorted", rep.Replicas)
	}
	if rep.RingsCompatible {
		t.Error("compatible although quader-api-a lacks old and gone, which sealed values reference")
	}
	raw, _ := json.Marshal(rep)
	if strings.Contains(string(raw), "v1:") || strings.Contains(string(raw), "Lost") {
		t.Errorf("the report carries a sealed value or a field it should not: %s", raw)
	}
}
