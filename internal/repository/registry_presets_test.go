package repository

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_REPO_076_ARegistryPresetIsStoredInItsNormalForm: the name is trimmed, the URL normalized as R-REPO-024 stores it, the host
// patterns trimmed and in lower case, and a preset without hosts holds an empty list.
func TestR_REPO_076_ARegistryPresetIsStoredInItsNormalForm(t *testing.T) {
	got, err := CheckRegistryPresets(presetCatalog(), []RegistryPreset{
		{
			Format: "twopreset", DisplayName: " Harbor ", URL: "https://Harbor.Example.com:443",
			ExtraHosts: []ExtraHost{{Pattern: " Harbor.Example.com ", SendCredentials: true}},
		},
		{Format: "pkgfmt", DisplayName: "Verdaccio", URL: "https://npm.example.com/registry/"},
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	want := []RegistryPreset{
		{
			Format: "twopreset", DisplayName: "Harbor", URL: "https://harbor.example.com/",
			ExtraHosts: []ExtraHost{{Pattern: "harbor.example.com", SendCredentials: true}},
		},
		{Format: "pkgfmt", DisplayName: "Verdaccio", URL: "https://npm.example.com/registry/", ExtraHosts: []ExtraHost{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored form:\n got %+v\nwant %+v", got, want)
	}
}

// TestR_REPO_076_TheListIsRefusedAtThePointerOfEachProblem: each refusal of R-REPO-076 is a field error of one 422 at its pointer.
func TestR_REPO_076_TheListIsRefusedAtThePointerOfEachProblem(t *testing.T) {
	harbor := RegistryPreset{Format: "twopreset", DisplayName: "Harbor", URL: "https://harbor.example.com/"}
	eleven := make([]RegistryPreset, 0, MaxRegistryPresetsPerFormat+1)
	for i := range MaxRegistryPresetsPerFormat + 1 {
		eleven = append(eleven, RegistryPreset{Format: "pkgfmt", DisplayName: fmt.Sprintf("r%d", i), URL: fmt.Sprintf("https://r%d.example.com/", i)})
	}
	for name, c := range map[string]struct {
		in      []RegistryPreset
		pointer string
		code    string
	}{
		"an unknown format": {[]RegistryPreset{{Format: "nope", DisplayName: "x", URL: "https://x.example/"}}, "/presets/0/format", errs.CodeUnknownFormat},
		"a format without remote repositories": {
			[]RegistryPreset{{Format: "generic", DisplayName: "x", URL: "https://x.example/"}}, "/presets/0/format", errs.CodeUnsupportedClass,
		},
		"an empty name":      {[]RegistryPreset{{Format: "twopreset", DisplayName: "  ", URL: "https://x.example/"}}, "/presets/0/displayName", codeInvalidValue},
		"a name too long":    {[]RegistryPreset{{Format: "twopreset", DisplayName: strings.Repeat("n", 65), URL: "https://x.example/"}}, "/presets/0/displayName", codeInvalidValue},
		"a URL with a user":  {[]RegistryPreset{{Format: "twopreset", DisplayName: "x", URL: "https://u:p@x.example/"}}, "/presets/0/url", codeInvalidUpstreamURL},
		"a relative URL":     {[]RegistryPreset{{Format: "twopreset", DisplayName: "x", URL: "registry.example"}}, "/presets/0/url", codeInvalidUpstreamURL},
		"a well-known URL":   {[]RegistryPreset{{Format: "twopreset", DisplayName: "x", URL: "https://Registry.Lab.Example"}}, "/presets/0/url", codeInvalidValue},
		"a name twice":       {[]RegistryPreset{harbor, {Format: "twopreset", DisplayName: "HARBOR", URL: "https://h2.example/"}}, "/presets/1/displayName", codeInvalidValue},
		"a URL twice":        {[]RegistryPreset{harbor, {Format: "twopreset", DisplayName: "Harbor 2", URL: "https://harbor.example.com"}}, "/presets/1/url", codeInvalidValue},
		"an invalid pattern": {[]RegistryPreset{{Format: "twopreset", DisplayName: "x", URL: "https://x.example/", ExtraHosts: []ExtraHost{{Pattern: "x.example:443"}}}}, "/presets/0/extraHosts/0/pattern", codeInvalidPattern},
		"a pattern twice": {
			[]RegistryPreset{{Format: "twopreset", DisplayName: "x", URL: "https://x.example/", ExtraHosts: []ExtraHost{{Pattern: "a.example"}, {Pattern: "A.example"}}}},
			"/presets/0/extraHosts/1/pattern", codeInvalidValue,
		},
		"eleven presets of one format": {eleven, "/presets", codeOutOfRange},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := CheckRegistryPresets(presetCatalog(), c.in)
			e, ok := errs.As(err)
			if !ok || e.Status() != 422 || e.Code != errs.CodeValidation {
				t.Fatalf("got %v, want 422 validation_failed", err)
			}
			if got := fieldsOf(err)[c.pointer]; got != c.code {
				t.Errorf("field %s: got %q, want %q (all: %v)", c.pointer, got, c.code, fieldsOf(err))
			}
		})
	}
	t.Run("the same name in two formats is allowed", func(t *testing.T) {
		if _, err := CheckRegistryPresets(presetCatalog(), []RegistryPreset{harbor, {Format: "pkgfmt", DisplayName: "Harbor", URL: "https://harbor.example.com/"}}); err != nil {
			t.Errorf("got %v", err)
		}
	})
}

// TestR_REPO_076_TheChangeIsAuditedWithTheListBeforeAndAfter: settings.registry_presets.changed carries both lists, and nothing of a
// credential, because a preset has no member for one.
func TestR_REPO_076_TheChangeIsAuditedWithTheListBeforeAndAfter(t *testing.T) {
	after := []RegistryPreset{{Format: "oci", DisplayName: "Harbor", URL: "https://harbor.example.com/", ExtraHosts: []ExtraHost{{Pattern: "harbor.example.com", SendCredentials: true}}}}
	ev := registryPresetsEvent(Caller{}, []RegistryPreset{}, after)
	want := `{"after":[{"format":"oci","displayName":"Harbor","url":"https://harbor.example.com/","extraHosts":[{"pattern":"harbor.example.com","sendCredentials":true}]}],"before":[]}`
	if ev.Type != audit.EventRegistryPresetsChanged || string(ev.Details) != want {
		t.Errorf("event %s %s", ev.Type, ev.Details)
	}
	var round map[string][]RegistryPreset
	if err := json.Unmarshal(ev.Details, &round); err != nil || !registryPresetsEqual(round["after"], after) {
		t.Errorf("details do not read back: %v", err)
	}
}
