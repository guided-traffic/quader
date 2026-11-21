package repository

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/qname"
)

// gateCatalog is the test catalog with npm and Debian.
func gateCatalog() *catalog.Registry {
	r := vulnCatalog()
	r.Register(catalog.Entry{Facts: catalog.Facts{
		ID: "deb", Classes: []Class{Local, Remote, Virtual}, Layout: "deb", ProtocolBasePath: "/api/deb/{project}/{key}/", PatternSubject: SubjectPath,
	}})
	return r
}

func gateValidate(t *testing.T, body string) error {
	t.Helper()
	d, err := DecodeDocument([]byte(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, v := validate(gateCatalog(), &d, true)
	return v.err()
}

// TestR_SIG_061_TheGateMemberIsValidated: onlyVerified and anchors are taken on every class of npm; on a Debian repository both answer 422
// not_applicable (Q-SIG-80); a reference that names no scope, one named twice and a 33rd answer 422 invalid_value at their pointer; the
// snapshot reads the member (R-SIG-061). Unit, milliseconds.
func TestR_SIG_061_TheGateMemberIsValidated(t *testing.T) {
	for _, class := range []string{"local", "remote", "virtual"} {
		body := fmt.Sprintf(`{"key":"npm-x","class":%q,"format":"npm","signatures":{"onlyVerified":true,"anchors":["installation/release-key"]}}`, class)
		if class == "remote" {
			body = strings.Replace(body, `"format":"npm"`, `"format":"npm","remote":{"url":"https://registry.npmjs.org/"}`, 1)
		}
		if err := gateValidate(t, body); err != nil {
			t.Errorf("%s: %v", class, err)
		}
	}
	err := gateValidate(t, `{"key":"debian","class":"local","format":"deb","signatures":{"onlyVerified":true,"anchors":["installation/a"]}}`)
	if f := fieldsOf(err); f["/signatures/onlyVerified"] != errs.CodeNotApplicable || f["/signatures/anchors"] != errs.CodeNotApplicable {
		t.Errorf("Debian: %v", f)
	}
	if err := gateValidate(t, `{"key":"debian","class":"local","format":"deb","signatures":{"onlyVerified":false}}`); err != nil {
		t.Errorf("Debian with the gate off: %v", err)
	}
	err = gateValidate(t, `{"key":"npm-x","class":"local","format":"npm","signatures":{"onlyVerified":true,"anchors":["release-key","installation/a","installation/a"]}}`)
	if f := fieldsOf(err); f["/signatures/anchors/0"] != codeInvalidValue || f["/signatures/anchors/2"] != codeInvalidValue || f["/signatures/anchors/1"] != "" {
		t.Errorf("references: %v", f)
	}
	refs := make([]string, 33)
	for i := range refs {
		refs[i] = fmt.Sprintf("%q", fmt.Sprintf("installation/a%d", i))
	}
	err = gateValidate(t, `{"key":"npm-x","class":"local","format":"npm","signatures":{"onlyVerified":true,"anchors":[`+strings.Join(refs, ",")+`]}}`)
	if f := fieldsOf(err); f["/signatures/anchors"] != codeInvalidValue {
		t.Errorf("33 references: %v", f)
	}
	var s Settings
	if err := json.Unmarshal([]byte(`{"signatures":{"onlyVerified":true,"anchors":["shipped/sigstore-public"]}}`), &s); err != nil {
		t.Fatal(err)
	}
	if g := signatureGateOf(s.Signatures); !g.OnlyVerified || !slices.Equal(g.Anchors, []string{"shipped/sigstore-public"}) {
		t.Errorf("snapshot %+v", g)
	}
}

func gateDoc(onlyVerified bool, anchors ...string) *Document {
	return &Document{Settings: Settings{Signatures: &SignatureSettings{OnlyVerified: &onlyVerified, Anchors: anchors}}}
}

// TestR_SIG_061_AnOwnerNarrowsAndDoesNotWeaken: switching the gate on and removing a reference are the part signatures of
// repository.narrowed; switching it off, accepting any anchor and adding a reference are weakenings a project Owner may not make (403
// signature_weakening_refused), the triage capability and an administrator may, and the update's details mark with block: lifted
// (R-SIG-061, Q-MAL-31's rule). Unit, milliseconds.
func TestR_SIG_061_AnOwnerNarrowsAndDoesNotWeaken(t *testing.T) {
	s := &Service{cat: vulnCatalog()}
	r := Repository{ID: "8", Name: qname.Repo{Project: "web", Key: "files-internal"}, Class: Local, Format: "npm"}
	const a, b = "installation/a", "installation/b"
	for _, c := range []struct {
		name             string
		cur, nd          *Document
		narrows, weakens bool
	}{
		{"absent to on", &Document{}, gateDoc(true), true, false},
		{"any to a list", gateDoc(true), gateDoc(true, a), true, false},
		{"a reference removed", gateDoc(true, a, b), gateDoc(true, a), true, false},
		{"on to off", gateDoc(true), gateDoc(false), false, true},
		{"a list to any", gateDoc(true, a), gateDoc(true), false, true},
		{"a reference added", gateDoc(true, a), gateDoc(true, a, b), false, true},
		{"unchanged", gateDoc(true, a), gateDoc(true, a), false, false},
	} {
		e := s.narrowing(r, c.cur, c.nd, nil, nil)
		if got := slices.Contains(e.parts(), partSignatures); got != c.narrows || (c.narrows && !e.Signatures.After.OnlyVerified) {
			t.Errorf("%s: narrows %v (%+v)", c.name, got, e.Signatures)
		}
		err := restrictedVulnerabilities(Caller{Restricted: true}, c.cur, c.nd)
		if e, _ := errs.As(err); (err != nil) != c.weakens || (err != nil && e.Code != errs.CodeSignatureWeakeningRefused) {
			t.Errorf("%s: an Owner's write answers %v", c.name, err)
		}
		if err := restrictedVulnerabilities(Caller{Restricted: true, MayWeakenVulnerabilities: true}, c.cur, c.nd); err != nil {
			t.Errorf("%s: the triage capability is refused: %v", c.name, err)
		}
		if d := updateDetails(r, nil, c.cur, c.nd); (d.Block == "lifted") != c.weakens || (c.weakens && !slices.Contains(d.Weakened, "signatures")) {
			t.Errorf("%s: details %+v", c.name, d)
		}
	}
}

// TestR_SIG_061_TheReplayOfThePartSignatures: the replay sets the gate on with the narrower of the restored and the journaled anchors; a
// member that says the same already leaves it in effect; two lists without a common reference disable the repository; a malformed part
// is refused by the reader (R-SIG-061, R-REPO-061). Unit, milliseconds.
func TestR_SIG_061_TheReplayOfThePartSignatures(t *testing.T) {
	part := func(anchors ...string) *signaturesChange {
		return &signaturesChange{After: signatureGateMember{OnlyVerified: true, Anchors: anchors}}
	}
	r := &replayer{doc: map[string]json.RawMessage{}}
	r.set.Signatures = &SignatureSettings{Anchors: []string{"installation/a"}}
	r.signatures(part("installation/b"))
	if g := signatureGateOf(r.set.Signatures); !g.OnlyVerified || !slices.Equal(g.Anchors, []string{"installation/b"}) || !r.touched {
		t.Fatalf("restored off: %+v touched %v", g, r.touched)
	}
	same := &replayer{doc: map[string]json.RawMessage{}}
	on := true
	same.set.Signatures = &SignatureSettings{OnlyVerified: &on, Anchors: []string{"installation/a"}}
	same.signatures(part())
	if same.touched {
		t.Fatal("a narrower restored member was written again")
	}
	disjoint := &replayer{doc: map[string]json.RawMessage{}}
	disjoint.set.Signatures = &SignatureSettings{OnlyVerified: &on, Anchors: []string{"installation/a"}}
	disjoint.signatures(part("installation/c"))
	if !disjoint.set.Disabled || !slices.Equal(disjoint.suspended, []string{partSignatures}) {
		t.Fatalf("two lists without a common reference: disabled %v, suspended %v", disjoint.set.Disabled, disjoint.suspended)
	}
	if why := checkSignaturesPart(&signaturesChange{After: signatureGateMember{OnlyVerified: false}}); why == "" {
		t.Error("a part that does not switch the gate on is read")
	}
	if why := checkSignaturesPart(part("nope")); why == "" {
		t.Error("a part with a malformed reference is read")
	}
}
