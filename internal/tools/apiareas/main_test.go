package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const before = `
components:
  schemas:
    Repository:
      properties:
        key: {type: string}
        notes: {type: string}
    ConsumerView:
      properties:
        key: {type: string}
`

const after = `
components:
  schemas:
    Repository:
      properties:
        key: {type: string, maxLength: 64}
    ConsumerView:
      properties:
        key: {type: string}
        disabled: {type: boolean}
`

// The members a revision added, removed and changed, and the areas whose tests name each (Q-ARCH-703). Milliseconds.
func TestTheChangedMembersAndTheAreasThatNameThem(t *testing.T) {
	got, err := diff([]byte(before), []byte(after))
	if err != nil {
		t.Fatal(err)
	}
	want := []change{{"ConsumerView", "disabled", "added"}, {"Repository", "key", "changed"}, {"Repository", "notes", "removed"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes %v, want %v", got, want)
	}

	tests := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(tests, p)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tests, p), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("transfer/lifecycle_test.go", `if _, ok := view["disabled"]; ok {}`)
	write("files/a_test.go", `// disabled without quotes is no key`)
	write("files/env.go", `"disabled"`) // not a test file
	write("packages/x_test.go", `m["notes"]`)
	idx, err := index(tests)
	if err != nil {
		t.Fatal(err)
	}
	if a := areasNaming(idx, "disabled"); !reflect.DeepEqual(a, []string{"transfer"}) {
		t.Errorf("disabled: %v, want [transfer]", a)
	}
	if a := areasNaming(idx, "notes"); !reflect.DeepEqual(a, []string{"packages"}) {
		t.Errorf("notes: %v, want [packages]", a)
	}
}

// The repository's own document parses, and its schemas have members. About a second (1 MB of YAML).
func TestTheRealDocumentHasMembers(t *testing.T) {
	b, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m, err := members(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) < 100 {
		t.Fatalf("%d members", len(m))
	}
}
