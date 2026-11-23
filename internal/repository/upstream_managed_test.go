package repository

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// storedPreset is the stored document of a well-known npm repository with an account.
func storedPreset(t *testing.T) Document {
	t.Helper()
	d, err := DecodeDocument([]byte(`{"key":"npmjs","class":"remote","format":"npmlike","description":"npm public registry",
		"disabled":false,"visibility":{"artifacts":"internal","wiki":"internal"},
		"remote":{"url":"https://registry.example.org/","extraHosts":[],"authMode":"basic","username":"u","password":{"set":true},
		"mode":"pullThrough","cacheFillAction":"read","availability":{"mode":"online","failureThreshold":5,"circuitOpenSeconds":30}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// patched applies a merge patch to the stored document as the API does.
func patched(t *testing.T, cur Document, patch string) Document {
	t.Helper()
	base, _ := json.Marshal(cur)
	var b, p map[string]any
	_ = json.Unmarshal(base, &b)
	if err := json.Unmarshal([]byte(patch), &p); err != nil {
		t.Fatal(err)
	}
	var merge func(dst, src map[string]any)
	merge = func(dst, src map[string]any) {
		for k, v := range src {
			if m, ok := v.(map[string]any); ok {
				if d, ok := dst[k].(map[string]any); ok {
					merge(d, m)
					continue
				}
			}
			if v == nil {
				delete(dst, k)
				continue
			}
			dst[k] = v
		}
	}
	merge(b, p)
	raw, _ := json.Marshal(b)
	nd, err := DecodeDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	return nd
}

func pointersOf(fs []errs.FieldError) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		if f.Code != errs.CodeManagedByProduct {
			continue
		}
		out = append(out, f.Pointer)
	}
	return out
}

// TestR_REPO_069_AMergePatchOfAWellKnownRepositoryChangesOnlyTheNarrowingSettings: per member of the table of R-REPO-069, a merge
// patch of a repository of the project upstream that changes a member the product or an operation of R-REPO-073 owns is refused at
// that member with managed_by_product; the narrowing settings pass; a member sent with its stored value passes (Q-REPO-378, Q-REPO-386).
func TestR_REPO_069_AMergePatchOfAWellKnownRepositoryChangesOnlyTheNarrowingSettings(t *testing.T) {
	cur := storedPreset(t)
	for _, tc := range []struct {
		name  string
		kind  wellknown.Kind
		patch string
		want  []string
	}{
		{"the document sent back as it was read", wellknown.KindPreset, `{}`, nil},
		{
			"the narrowing settings", wellknown.KindPreset,
			`{"adminNotes":"x","vulnerabilities":{"enabled":true,"blockMalicious":true},
			  "remote":{"cacheFillAction":"deploy","availability":{"mode":"offline"},"delivery":{"mode":"onlyAllowed"}}}`, nil,
		},
		{"the url", wellknown.KindPreset, `{"remote":{"url":"https://registry.example.com/"}}`, []string{"/remote/url"}},
		{"the mode", wellknown.KindPreset, `{"remote":{"mode":"mirror"}}`, []string{"/remote/mode"}},
		{"the account", wellknown.KindPreset, `{"remote":{"password":{"value":"s3cret"},"username":"v"}}`, []string{"/remote/password", "/remote/username"}},
		{
			"the state and the level", wellknown.KindPreset, `{"disabled":true,"visibility":{"artifacts":"private","wiki":"private"}}`,
			[]string{"/disabled", "/visibility"},
		},
		{
			"a fixed member and the availability's other members", wellknown.KindPreset,
			`{"description":"mine","hideExistence":"true","remote":{"availability":{"failureThreshold":9}}}`,
			[]string{"/description", "/hideExistence", "/remote/availability/failureThreshold"},
		},
		{"the format's member", wellknown.KindPreset, `{"npm":{"auditMode":"forward"}}`, []string{"/npm"}},
		// R-SIG-040: an administrator switches the background fetch of a well-known upstream's signatures off.
		{"the fetch of the upstream's signatures", wellknown.KindPreset, `{"signatures":{"fetch":false}}`, nil},
		{"the signatures of a system repository", wellknown.KindSystem, `{"signatures":{"fetch":false}}`, []string{"/signatures"}},
		{
			"the delivery mode and the scanning of a system repository", wellknown.KindSystem,
			`{"remote":{"delivery":{"mode":"allExceptBlocked"}},"vulnerabilities":{"enabled":true}}`,
			[]string{"/remote/delivery", "/vulnerabilities"},
		},
	} {
		t.Run("R-REPO-069 "+tc.name, func(t *testing.T) {
			nd := patched(t, cur, tc.patch)
			got, err := managedPointers(tc.kind, &cur, &nd)
			if err != nil {
				t.Fatal(err)
			}
			if p := pointersOf(got); !slices.Equal(p, tc.want) {
				t.Fatalf("managed_by_product at %v, want %v", p, tc.want)
			}
		})
	}
}

// TestR_REPO_062_ARemoteOfAPresetsURLTakesTheSettingsThePresetCarries: a remote created from a preset's URL takes the members of the
// format's settings member the preset carries, as it takes the preset's hosts; a member the document sets stays (Q-REPO-379).
func TestR_REPO_062_ARemoteOfAPresetsURLTakesTheSettingsThePresetCarries(t *testing.T) {
	info := catalog.Entry{Facts: catalog.Facts{ID: "deb", UpstreamPresets: []catalog.UpstreamPreset{{
		ID: "archive", URL: "https://archive.example.org/", Default: true, Settings: json.RawMessage(`{"upstreamKeys":"KEYS"}`),
	}}}}
	doc := func(url, deb string) Document {
		d := Document{Key: "k", Class: Remote, Format: "deb"}
		d.Remote = json.RawMessage(`{"url":"` + url + `"}`)
		if deb != "" {
			d.Deb = json.RawMessage(deb)
		}
		return d
	}
	d := doc("https://archive.example.org/", "")
	applyPresetSettings(info, &d)
	if string(d.Deb) != `{"upstreamKeys":"KEYS"}` {
		t.Fatalf("deb = %s", d.Deb)
	}
	d = doc("https://archive.example.org/", `{"upstreamKeys":null,"compression":["xz"]}`)
	applyPresetSettings(info, &d)
	if strings.Contains(string(d.Deb), "KEYS") {
		t.Fatalf("a member the document sets was replaced: %s", d.Deb)
	}
	d = doc("https://other.example.org/", "")
	applyPresetSettings(info, &d)
	if len(d.Deb) != 0 {
		t.Fatalf("another URL takes the preset's settings: %s", d.Deb)
	}
}

// TestR_REPO_070_TheCachePeriodOfScannerTrivyIsTheProducts: the validation refuses remote.cache.unusedRetentionHours until the cleanup
// is built (R-REPO-046); the product's value on scanner-trivy passes when the product writes it or a write leaves it as stored.
func TestR_REPO_070_TheCachePeriodOfScannerTrivyIsTheProducts(t *testing.T) {
	held := func() *validation {
		v := &validation{}
		v.add(productHeldPointer, errs.CodeNotApplicable, "not yet")
		v.add("/remote/url", "invalid_upstream_url", "bad")
		return v
	}
	doc := func(hours string) *Document {
		return &Document{Key: "scanner-trivy", Class: Remote, Format: "oci", Settings: Settings{Remote: json.RawMessage(`{"url":"https://mirror.example.org/","cache":{"unusedRetentionHours":` + hours + `}}`)}}
	}
	v := held()
	dropProductHeld(v, nil, doc("48"))
	if len(v.fields) != 1 || v.fields[0].Pointer != "/remote/url" {
		t.Fatalf("the product's own write: %+v", v.fields)
	}
	v = held()
	dropProductHeld(v, doc("48"), doc("48"))
	if len(v.fields) != 1 {
		t.Fatalf("a write that leaves the value: %+v", v.fields)
	}
	v = held()
	dropProductHeld(v, doc("48"), doc("1"))
	if len(v.fields) != 2 {
		t.Fatalf("a write that changes the value keeps the refusal: %+v", v.fields)
	}
}

// TestR_REPO_073_TheRefusalsOfTheAddressNameTheMembersOfTheBody: a refused write of the address or the account names the members of
// the operation's body, not those of the repository document.
func TestR_REPO_073_TheRefusalsOfTheAddressNameTheMembersOfTheBody(t *testing.T) {
	err := errs.E(errs.Invalid, errs.CodeValidation, "x", errs.WithFields(
		errs.FieldError{Pointer: "/remote/url"}, errs.FieldError{Pointer: "/remote/extraHosts/0/pattern"},
		errs.FieldError{Pointer: "/deb/upstreamKeys"}, errs.FieldError{Pointer: "/remote/password"}, errs.FieldError{Pointer: "/remote/authMode"}))
	got, _ := errs.As(UpstreamPointers(err, false))
	var ptrs []string
	for _, f := range got.Fields {
		ptrs = append(ptrs, f.Pointer)
	}
	if want := "/url /extraHosts/0/pattern /upstreamKeys /account/password /account/authMode"; strings.Join(ptrs, " ") != want {
		t.Fatalf("pointers %v", ptrs)
	}
	got, _ = errs.As(UpstreamPointers(err, true))
	if got.Fields[3].Pointer != "/password" {
		t.Fatalf("the account's body: %v", got.Fields[3].Pointer)
	}
}
