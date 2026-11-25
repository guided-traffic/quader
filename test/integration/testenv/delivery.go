package testenv

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The delivery rules of a remote repository (remote-curation.md R-CUR-001, R-CUR-006) as the tests of every area write them: a seed row
// for the state a test starts from, written before the first replica builds its snapshot, and the management API's write for what a
// test exercises, with a wait until a replica's snapshot decides by it.

// Rule is one delivery rule: Kind "section" with Pattern, "package" with Name, "version" with Name and Version; Effect "allow" or
// "block". A package's allow takes a range of versions (R-CUR-026): FromVersion, ToVersion and ToInclusive (nil: true).
type Rule struct {
	Kind        string `json:"kind"`
	Pattern     string `json:"pattern,omitempty"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Effect      string `json:"effect"`
	FromVersion string `json:"fromVersion,omitempty"`
	ToVersion   string `json:"toVersion,omitempty"`
	ToInclusive *bool  `json:"toInclusive,omitempty"`
}

// RangeRule is a package's allow with a range of versions (R-CUR-026): from a version (included, "" for none) up to a version (included
// when toInclusive, else below it; "" for none), in the format's canonical form.
func RangeRule(name, from, to string, toInclusive bool) Rule {
	return Rule{Kind: "package", Name: name, Effect: "allow", FromVersion: from, ToVersion: to, ToInclusive: &toInclusive}
}

// SectionRule is a rule on the names a pattern matches; it stands for an include pattern (allow, with the mode onlyAllowed) or an exclude
// pattern (block) of a remote (R-CUR-002).
func SectionRule(pattern, effect string) Rule {
	return Rule{Kind: "section", Pattern: pattern, Effect: effect}
}

// PackageRule is a rule on every version of one package.
func PackageRule(name, effect string) Rule { return Rule{Kind: "package", Name: name, Effect: effect} }

// VersionRule is a rule on one version of a package.
func VersionRule(name, version, effect string) Rule {
	return Rule{Kind: "version", Name: name, Version: version, Effect: effect}
}

// OnlyAllowed is the member remote.delivery of a remote that delivers only what a rule allows (R-CUR-001), to merge into the remote
// section of a seeded or created document.
func OnlyAllowed() map[string]any { return map[string]any{"mode": "onlyAllowed"} }

// DeliveryRule seeds a rule of the remote repository key ("<project>/<key>" or a bare key of [DefaultProject]) as a row, before the
// first replica starts: the snapshot reads it at its first build. The rule is written as given, so it must be in the format's canonical
// form (npm and the name patterns lower case); a test of the store's folding goes through [Stack.PutDeliveryRule].
func (s *Stack) DeliveryRule(key string, r Rule) {
	s.tb.Helper()
	project, k := splitName(key)
	null := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	inclusive := r.ToVersion == "" || r.ToInclusive == nil || *r.ToInclusive
	s.Exec(`INSERT INTO remote_delivery_rules (id, repo_id, kind, pattern, name, version, effect, from_version, to_version, to_inclusive)
		SELECT gen_random_uuid(), r.id, $3, $4, $5, $6, $7, $8, $9, $10 FROM repositories r JOIN project p ON p.id = r.project_id
		 WHERE p.key = $1 AND r.key = $2`,
		project, k, r.Kind, null(r.Pattern), null(r.Name), null(r.Version), r.Effect, null(r.FromVersion), null(r.ToVersion), inclusive)
	s.Exec(`UPDATE repositories SET delivery_rules_revision = delivery_rules_revision + 1
		WHERE key = $2 AND project_id = (SELECT id FROM project WHERE key = $1)`, project, k)
}

// PutDeliveryRule writes a rule through the management API (POST …/delivery-rules) as the token's holder and returns the answer.
func (s *Stack) PutDeliveryRule(r *Replica, token, key string, rule Rule) Answer {
	s.tb.Helper()
	project, k := splitName(key)
	return s.Do(r, http.MethodPost, ManagementURL(project, k)+"/delivery-rules", token, rule)
}

// MustPutDeliveryRule writes a rule that must be stored (201 or 200) and returns its ID.
func (s *Stack) MustPutDeliveryRule(r *Replica, token, key string, rule Rule) string {
	s.tb.Helper()
	a := s.PutDeliveryRule(r, token, key, rule)
	if a.Status != http.StatusCreated && a.Status != http.StatusOK {
		s.tb.Fatalf("delivery rule %+v on %s: %d %s", rule, key, a.Status, a.Body)
	}
	id, _ := a.JSON(s.tb)["id"].(string)
	return id
}

// Delivered asks the replica's snapshot whether the remote key delivers a package ("" version: the package's decision) or one of its
// versions (POST …/delivery-rules/check).
func (s *Stack) Delivered(r *Replica, token, key, name, version string) bool {
	s.tb.Helper()
	project, k := splitName(key)
	body := map[string]any{"name": name}
	if version != "" {
		body["version"] = version
	}
	a := s.Do(r, http.MethodPost, ManagementURL(project, k)+"/delivery-rules/check", token, body)
	if a.Status != http.StatusOK {
		s.tb.Fatalf("check %s %s on %s: %d %s", name, version, key, a.Status, a.Body)
	}
	var d struct {
		Delivered bool `json:"delivered"`
	}
	if err := json.Unmarshal(a.Body, &d); err != nil {
		s.tb.Fatal(err)
	}
	return d.Delivered
}

// WaitDelivered waits until the replica's snapshot decides the package (or version) as want: a rule's write reaches every replica
// through the change area repo_config within the snapshot's bound (R-CUR-006 "Propagation", H-132).
func (s *Stack) WaitDelivered(tb testing.TB, r *Replica, token, key, name, version string, want bool) {
	tb.Helper()
	WaitFor(tb, "the delivery decision of "+name+" "+version+" on "+key, 15*time.Second, func() bool {
		return s.Delivered(r, token, key, name, version) == want
	})
}
