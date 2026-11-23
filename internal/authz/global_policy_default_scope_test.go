package authz

import (
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// TestR_AUTHZ_013_AGlobalReadPolicyAdmitsATokenOfTheDefaultScope: a global policy that grants read on an OCI repository admits a
// pull through a token of the default scope ({inherit: true}, scope.Default) exactly as it admits the owner's session: the
// manifest by its tag (metadata) and a blob by its digest (artifact), both with the action read (R-AUTHZ-013: an inheriting
// token is bounded by nothing but the owner's rights; R-AUTHZ-007: a pull is read). A policy whose selector names no
// repository grants nothing, and the answer is deny.no-grant: what the integration helper testenv.Stack.Policy writes,
// without a word, when a key it is given resolves to no repository of the stack (the finding of 2026-11-21 in the area
// signatures, where the evaluation was suspected). Unit, microseconds, no data.
func TestR_AUTHZ_013_AGlobalReadPolicyAdmitsATokenOfTheDefaultScope(t *testing.T) {
	f := newFixture().repo("oci-local", "local", "oci", "").
		policy(policyDef{name: "oci-readers", selectors: []string{"oci-local"}, entries: map[string][]string{idAlice: {"read"}}}).
		policy(policyDef{name: "no-repository", entries: map[string][]string{idBob: {"read"}}})
	e := newTestEngine(t, f, func(o *Options) {
		reg := testRegistry()
		reg.Register(catalog.Entry{Facts: catalog.Facts{
			ID: "oci", Classes: []catalog.Class{catalog.Local, catalog.Remote, catalog.Virtual}, Layout: "oci",
			PatternSubject: catalog.SubjectName, PackageNames: true,
		}, Names: exactNames{}})
		o.Formats = reg
	})
	def := scope.Default()
	pull := []Coordinate{
		{Repository: qr("oci-local"), Class: "local", Format: "oci", Kind: KindMetadata, Package: "app", Path: "app/manifests/1.0", Actions: []Action{Read}},
		{
			Repository: qr("oci-local"), Class: "local", Format: "oci", Kind: KindArtifact, Package: "app",
			Path: "app/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000", Actions: []Action{Read},
		},
	}
	for _, c := range pull {
		for name, sub := range map[string]Subject{
			"the session":                  userSub(idAlice, "alice"),
			"a token of the default scope": tokenScop(userSub(idAlice, "alice"), def),
		} {
			if d := e.Decide(t.Context(), sub, c); d.Outcome != Allow {
				t.Errorf("%s, %s %s: %v %s, want allowed by the global policy", name, c.Kind, c.Path, d.Outcome, d.Reason)
			}
		}
		d := e.Decide(t.Context(), tokenScop(userSub(idBob, "bob"), def), c)
		if d.Outcome == Allow || d.Reason != ReasonNoGrant {
			t.Errorf("a policy without a repository, %s %s: %v %s, want deny.no-grant", c.Kind, c.Path, d.Outcome, d.Reason)
		}
	}
}
