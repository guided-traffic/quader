package ansible

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/galaxydoc"
)

// TestR_ANS_060_ACollectionIsDeprecatedAsAWhole: a deprecation names no version and carries a message for Quader's screens only; NuGet's
// reasons and alternate package and a message over 1,024 characters are refused with a sentence that says why.
func TestR_ANS_060_ACollectionIsDeprecatedAsAWhole(t *testing.T) {
	d, err := collectionDeprecation(formats.DeprecationChange{Package: "acme.tools", Deprecation: &formats.Deprecation{Message: " use acme.kit "}}, "u-1")
	if err != nil || d == nil || d.Message != "use acme.kit" || d.By != "u-1" {
		t.Errorf("deprecation %+v, %v", d, err)
	}
	if d, err := collectionDeprecation(formats.DeprecationChange{Package: "acme.tools"}, "u-1"); err != nil || d != nil {
		t.Errorf("a clear: %+v %v", d, err)
	}
	for name, c := range map[string]formats.DeprecationChange{
		"a version":              {Package: "acme.tools", Version: "1.0.0", Deprecation: &formats.Deprecation{}},
		"a clear of one version": {Package: "acme.tools", Version: "1.0.0"},
		"reasons":                {Package: "acme.tools", Deprecation: &formats.Deprecation{Reasons: []string{"Legacy"}}},
		"an alternate":           {Package: "acme.tools", Deprecation: &formats.Deprecation{Alternate: &formats.DeprecationAlternate{Package: "acme.kit"}}},
		"a long message":         {Package: "acme.tools", Deprecation: &formats.Deprecation{Message: strings.Repeat("x", formats.MaxDeprecationMessage+1)}},
	} {
		if _, err := collectionDeprecation(c, "u-1"); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestR_ANS_060_TheCollectionDocumentCarriesTheFlagAndAVirtualTakesItsFirstMembers: the collection document says deprecated as the
// summary holds it, and a virtual repository's merged document takes the flag of the first member that holds the collection.
func TestR_ANS_060_TheCollectionDocumentCarriesTheFlagAndAVirtualTakesItsFirstMembers(t *testing.T) {
	doc := galaxydoc.RenderCollection(galaxydoc.CollectionSummary{Namespace: "acme", Name: "tools", HighestVersion: "1.0.0", Deprecated: true}, galaxydoc.Addr{})
	var c struct {
		Deprecated bool `json:"deprecated"`
	}
	if err := json.Unmarshal(doc, &c); err != nil || !c.Deprecated {
		t.Errorf("document %s (%v)", doc, err)
	}
	merged := func(docs ...string) bool {
		t.Helper()
		var parts []formats.MemberDocument
		for _, d := range docs {
			parts = append(parts, formats.MemberDocument{Doc: []byte(d)})
		}
		b, err := mergeTimes(parts)
		if err != nil {
			t.Fatal(err)
		}
		var m timesDoc
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m.Deprecated
	}
	if !merged("", `{"deprecated":true}`, `{"deprecated":false}`) || merged(`{"deprecated":false}`, `{"deprecated":true}`) {
		t.Error("the merged flag is not the first holding member's")
	}
}
