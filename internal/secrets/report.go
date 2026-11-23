package secrets

import (
	"context"
	"sort"

	"github.com/guided-traffic/quader/internal/config"
)

// The key-ring status of R-ARCH-021 as `quader admin secrets status` and GET /api/v1/system/encryption (R-API-049) both report it: one
// implementation, here. It reports key IDs, the sealed values per key ID, the canary state per key ID and the rings of the live
// replicas, never key material, fingerprints, ciphertext or plaintext.

// The canary states of a key ID, as the ring that reads them sees them.
const (
	// CanaryOK: the ring opens the stored canary of the key ID.
	CanaryOK = "ok"
	// CanaryFailed: a canary row exists and the ring cannot open it (the key is missing, or other key material holds the ID).
	CanaryFailed = "failed"
	// CanaryAbsent: no canary row for the key ID yet.
	CanaryAbsent = "absent"
)

// ReplicaRing is the key ring one replica published with its heartbeat (R-OPS-003): key IDs only.
type ReplicaRing struct {
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Lost        bool     `json:"-"`
	ActiveKeyID string   `json:"activeKeyId"`
	KeyIDs      []string `json:"keyIds"`
}

// KeyReport is one key ID of a [Report].
type KeyReport struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`
	// Loaded is true for a key the reading ring holds; a key ID that sealed values reference and the ring lacks is listed with false.
	Loaded       bool   `json:"loaded"`
	SealedValues int64  `json:"sealedValues"`
	Canary       string `json:"canary"`
}

// Report is the key-ring status: the reading ring's active key, every key ID of the ring and every one a sealed value references, the
// rings of the live replicas, and whether they are compatible.
type Report struct {
	ActiveKeyID     string        `json:"activeKeyId"`
	Keys            []KeyReport   `json:"keys"`
	Replicas        []ReplicaRing `json:"replicas"`
	RingsCompatible bool          `json:"ringsCompatible"`
}

// BuildReport reads the sealed values per key ID through counter and joins them with the canaries (key ID to the stored canary's
// sealed value) and the replicas' rings of the registry. The replicas listed are the live ones of a role that opens sealed values,
// sorted by name.
func (r *KeyRing) BuildReport(ctx context.Context, counter ValueCounter, canaries map[string]string, rings []ReplicaRing) (Report, error) {
	st, err := r.StatusWithCounts(ctx, counter)
	if err != nil {
		return Report{}, err
	}
	out := Report{ActiveKeyID: st.ActiveKeyID, Keys: make([]KeyReport, 0, len(st.Keys)), Replicas: []ReplicaRing{}}
	var referenced []string
	for _, k := range st.Keys {
		kr := KeyReport{ID: k.KeyID, Active: k.Active, Loaded: k.Loaded, Canary: r.canaryState(k, canaries)}
		if k.SealedValues != nil {
			kr.SealedValues = *k.SealedValues
		}
		if kr.SealedValues > 0 {
			referenced = append(referenced, k.KeyID)
		}
		out.Keys = append(out.Keys, kr)
	}
	out.RingsCompatible = RingsCompatible(rings, referenced)
	for _, rep := range rings {
		if rep.Lost || !opensSecrets(rep.Role) {
			continue
		}
		rep.KeyIDs = append([]string{}, rep.KeyIDs...)
		sort.Strings(rep.KeyIDs)
		out.Replicas = append(out.Replicas, rep)
	}
	sort.Slice(out.Replicas, func(i, j int) bool { return out.Replicas[i].Name < out.Replicas[j].Name })
	return out, nil
}

// canaryState is the canary of one key ID as the ring sees it.
func (r *KeyRing) canaryState(k KeyStatus, canaries map[string]string) string {
	sealed, has := canaries[k.KeyID]
	switch {
	case !has:
		return CanaryAbsent
	case !k.Loaded || r.CheckCanary(k.KeyID, sealed) != nil:
		return CanaryFailed
	}
	return CanaryOK
}

// RingsCompatible is the rule of R-API-049: every live replica's ring holds every key ID that is active on any live replica or
// referenced by a sealed value. False is the state in which a value sealed by one replica cannot be opened by another. A lost replica
// is left out; so are roles that open no sealed value.
func RingsCompatible(rings []ReplicaRing, referenced []string) bool {
	need := map[string]bool{}
	for _, id := range referenced {
		need[id] = true
	}
	var live []ReplicaRing
	for _, r := range rings {
		if r.Lost || !opensSecrets(r.Role) {
			continue
		}
		live = append(live, r)
		need[r.ActiveKeyID] = true
	}
	for _, r := range live {
		have := map[string]bool{}
		for _, id := range r.KeyIDs {
			have[id] = true
		}
		for id := range need {
			if !have[id] {
				return false
			}
		}
	}
	return true
}

// opensSecrets reports whether a replica of the role opens stored secrets, so that its key ring counts in the compatibility rule:
// every role that registers (Q-OPS-161). packages opens the secrets of the remote repositories it fetches with, api the ones it seals,
// worker the ones its jobs use.
func opensSecrets(role string) bool { return config.ValidRole(role) }
