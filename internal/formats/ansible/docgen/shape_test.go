package docgen_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
)

const callback = `DOCUMENTATION = r'''
name: timer
type: aggregate
short_description: Adds the time of a play
version_added: "1.1.0"
version_added_collection: acme.tools
positional: _input, unit
options:
  sort_order:
    description: The order of the summary.
    type: str
    version_added: "1.2.0"
    ini:
      - section: callback_timer
        key: sort_order
      - section: [not, a, scalar]
    env:
      - name: TIMER_SORT_ORDER
        deprecated: {why: x}
    vars:
      - name: timer_sort_order
    keyword:
      - name: timer_order
    cli:
      - name: sort_order
        option: --sort-order
    unknown_key: dropped
'''
`

// TestR_ANS_049_TheGeneratorKeepsTheConfigurationOfAnOption: the keys ini, env, vars, keyword and cli of an option are kept with their
// scalar members, version_added_collection of the entry and of an option is kept, and positional is kept; another key is dropped (R-ANS-049,
// R-ANS-050).
func TestR_ANS_049_TheGeneratorKeepsTheConfigurationOfAnOption(t *testing.T) {
	set := build(t, docgen.DefaultLimits(), "", map[string]string{"plugins/callback/timer.py": callback})
	e := entry(t, set, "callback", "timer")
	var ds struct {
		Doc struct {
			VersionAddedCollection string           `json:"version_added_collection"`
			Positional             []string         `json:"positional"`
			Options                []map[string]any `json:"options"`
		} `json:"doc"`
	}
	if err := json.Unmarshal(e.Doc, &ds); err != nil || len(ds.Doc.Options) != 1 {
		t.Fatalf("doc_strings %s: %v", e.Doc, err)
	}
	if ds.Doc.VersionAddedCollection != "acme.tools" || !reflect.DeepEqual(ds.Doc.Positional, []string{"_input, unit"}) {
		t.Errorf("version_added_collection %q, positional %q", ds.Doc.VersionAddedCollection, ds.Doc.Positional)
	}
	o := ds.Doc.Options[0]
	want := map[string]any{
		"ini":     []any{map[string]any{"section": "callback_timer", "key": "sort_order"}},
		"env":     []any{map[string]any{"name": "TIMER_SORT_ORDER"}},
		"vars":    []any{map[string]any{"name": "timer_sort_order"}},
		"keyword": []any{map[string]any{"name": "timer_order"}},
		"cli":     []any{map[string]any{"name": "sort_order", "option": "--sort-order"}},
	}
	for k, v := range want {
		if !reflect.DeepEqual(o[k], v) {
			t.Errorf("option key %s = %#v, want %#v", k, o[k], v)
		}
	}
	if _, ok := o["unknown_key"]; ok {
		t.Error("a key outside the contract was kept")
	}
}
