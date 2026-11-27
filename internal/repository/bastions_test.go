package repository

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestR_BAS_134_TheMemberBastions: a write names a Bastion as a string or as the object a read returned, whose addresses and disabled
// are read-only and ignored; another member of an entry is refused like an unknown member of the document (R-REPO-008); what an update
// compares is the set of names, so a document read and sent back, or the same names in another order or twice, change nothing; a read
// of a document without Bastions shows an empty list, never null.
func TestR_BAS_134_TheMemberBastions(t *testing.T) {
	d, err := DecodeDocument([]byte(`{"key":"files","class":"local","format":"generic",` +
		`"bastions":["artifacts",{"name":"partners","addresses":["https://pkgs.example.com"],"disabled":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(bastionNames(d.Bastions), []string{"artifacts", "partners"}) || d.Bastions[1].Disabled || d.Bastions[1].Addresses != nil {
		t.Fatalf("%+v", d.Bastions)
	}
	if _, err := DecodeDocument([]byte(`{"key":"files","class":"local","format":"generic","bastions":[{"name":"a","host":"x"}]}`)); err == nil {
		t.Fatal("an entry with an unknown member was read")
	}
	if _, err := DecodeDocument([]byte(`{"key":"files","class":"local","format":"generic","bastions":[7]}`)); err == nil {
		t.Fatal("a number was read as a Bastion")
	}
	read := &Document{Bastions: []BastionRef{{Name: "partners", Addresses: []string{"https://pkgs.example.com"}}, {Name: "artifacts"}}}
	if bastionsChanged(read, &Document{Bastions: []BastionRef{{Name: "artifacts"}, {Name: "partners"}, {Name: "artifacts"}}}) {
		t.Fatal("the same names in another order, one of them twice, are a change")
	}
	if !bastionsChanged(read, &Document{Bastions: []BastionRef{{Name: "artifacts"}}}) || !bastionsChanged(&Document{}, read) {
		t.Fatal("a Bastion taken off or added is no change")
	}
	raw, err := json.Marshal(Document{Key: "files", Class: Local, Format: "generic"})
	if err != nil || !strings.Contains(string(raw), `"bastions":[]`) {
		t.Fatalf("%s %v", raw, err)
	}
	if !IsReservedMember("bastions") {
		t.Fatal("a format could be named bastions")
	}
}
