package nuget

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/registration"
	"github.com/guided-traffic/quader/internal/formats/nuget/remote"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
	"github.com/guided-traffic/quader/internal/formats/nuget/search"
	"github.com/guided-traffic/quader/internal/formats/nuget/version"
)

// TestR_NUGET_036_ADeprecationHoldsWhatTheCatalogEntryCarries: the reasons are NuGet's three, each once, in nuget.org's spelling
// whatever case was sent; an alternate package without a range gets "*", which the clients need to format it; what the catalog entry
// cannot carry, and a version left out, is refused with a sentence that says why.
func TestR_NUGET_036_ADeprecationHoldsWhatTheCatalogEntryCarries(t *testing.T) {
	got, err := deprecationRow(&formats.Deprecation{
		Reasons: []string{"legacy", "CRITICALBUGS"}, Message: "  Use Acme.Http 3.  ", Alternate: &formats.DeprecationAlternate{Package: "Acme.Http"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Reasons, []string{"Legacy", "CriticalBugs"}) || got.Message != "Use Acme.Http 3." ||
		got.AlternatePackage == nil || got.AlternatePackage.ID != "Acme.Http" || got.AlternatePackage.Range != "*" {
		t.Errorf("row %+v %+v", got, got.AlternatePackage)
	}
	for name, d := range map[string]*formats.Deprecation{
		"no reason":       {Message: "x"},
		"unknown reason":  {Reasons: []string{"Obsolete"}},
		"a reason twice":  {Reasons: []string{"Other", "other"}},
		"a long message":  {Reasons: []string{"Other"}, Message: strings.Repeat("ä", formats.MaxDeprecationMessage+1)},
		"a bad id":        {Reasons: []string{"Other"}, Alternate: &formats.DeprecationAlternate{Package: "no spaces allowed"}},
		"a bad range":     {Reasons: []string{"Other"}, Alternate: &formats.DeprecationAlternate{Package: "Acme.Http", Range: "[2.0,1.0]"}},
		"the empty range": {Reasons: []string{"Other"}, Alternate: &formats.DeprecationAlternate{Package: "Acme.Http", Range: "(,)"}},
	} {
		if _, err := deprecationRow(d); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if d, err := deprecationRow(nil); err != nil || d != nil {
		t.Errorf("a clear: %+v %v", d, err)
	}
	if _, _, err := New().Deprecate(t.Context(), nil, formats.DeprecationChange{Package: "Acme.Widgets", Deprecation: &formats.Deprecation{Reasons: []string{"Other"}}}); err == nil ||
		!strings.Contains(err.Error(), "name the version") {
		t.Errorf("a deprecation without a version: %v", err)
	}
}

// TestR_NUGET_036_TheRegistrationAndTheSearchServeAStoredVersionsDeprecation: a version whose row holds a deprecation is served with it
// in its catalog entry and in a search result that names it, in the catalog entry's shape the clients read; a member an upstream's
// document passes through wins over the row's.
func TestR_NUGET_036_TheRegistrationAndTheSearchServeAStoredVersionsDeprecation(t *testing.T) {
	const dep = `{"reasons":["Legacy"],"message":"Use 3.","alternatePackage":{"id":"Acme.Http","range":"*"}}`
	md := []byte(`{"description":"d","deprecation":` + dep + `}`)
	v := version.MustParse("2.1.0")
	row := rows.RegistrationVersion{
		VersionEntry: rows.VersionEntry{LowerVersion: v.URLForm(), FullVersion: v.FullNormalized(), DeclaredID: "Acme.Old", Listed: true},
		PackageTypes: []string{"dependency"}, Metadata: md,
	}
	d := registration.Doc{
		Feed: "https://q.example/api/nuget/p/r/v3/", Hive: registration.SemVer2, LowerID: "acme.old",
		Paging: registration.Paging{PageSize: 64, InlineBelow: 128},
	}
	// The index inlines its pages, so dotnet list package --deprecated finds the member without following a link.
	entryOf := func(d registration.Doc) string {
		t.Helper()
		b, err := d.Index([]rows.RegistrationVersion{row})
		if err != nil {
			t.Fatal(err)
		}
		var idx struct {
			Items []struct {
				Items []struct {
					CatalogEntry struct {
						Deprecation json.RawMessage `json:"deprecation"`
					} `json:"catalogEntry"`
				} `json:"items"`
			} `json:"items"`
		}
		if err := json.Unmarshal(b, &idx); err != nil || len(idx.Items) != 1 || len(idx.Items[0].Items) != 1 {
			t.Fatalf("index %s (%v)", b, err)
		}
		return string(idx.Items[0].Items[0].CatalogEntry.Deprecation)
	}
	if got := entryOf(d); got != dep {
		t.Errorf("catalog entry deprecation %s", got)
	}
	d.Extras = map[string]registration.Passthrough{v.URLForm(): {Deprecation: json.RawMessage(`{"reasons":["Other"]}`)}}
	if got := entryOf(d); got != `{"reasons":["Other"]}` {
		t.Errorf("the upstream's member: %s", got)
	}
	doc, err := search.Render("https://q.example/api/nuget/p/r/v3/", true, 1, []rows.SearchHit{{
		LowerID: "acme.old", DisplayName: "Acme.Old", Latest: row,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `"deprecation":`+dep) {
		t.Errorf("the search result carries no deprecation: %s", doc)
	}
}

// TestR_NUGET_036_ARemoteKeepsTheDeprecationItsUpstreamStates: the deprecation of each version a registration document of an upstream
// inlines is read without the upstream's addresses, a version without one is named with none (its row's is cleared), a reason the
// clients would not know becomes Other and an alternate without a range gets "*".
func TestR_NUGET_036_ARemoteKeepsTheDeprecationItsUpstreamStates(t *testing.T) {
	leaf := func(v, dep string) remote.Leaf {
		entry := map[string]json.RawMessage{"id": json.RawMessage(`"Acme.Old"`), "version": json.RawMessage(`"` + v + `"`)}
		if dep != "" {
			entry["deprecation"] = json.RawMessage(dep)
		}
		return remote.Leaf{Entry: entry}
	}
	got := upstreamDeprecations("acme.old", []remote.Leaf{
		leaf("1.0.0", `{"@id":"https://api.nuget.org/x#deprecation","@type":"deprecation","reasons":["Legacy"],"message":"old",`+
			`"alternatePackage":{"@id":"https://api.nuget.org/y","id":"Acme.New","range":"[2.0.0, )"}}`),
		leaf("1.1.0", `{"message":"no reason given","alternatePackage":{"id":"Acme.New"}}`),
		leaf("2.0.0", ""),
	})
	if d := got["1.0.0"]; d == nil || !slices.Equal(d.Reasons, []string{"Legacy"}) || d.Message != "old" || d.AlternatePackage.ID != "Acme.New" ||
		d.AlternatePackage.Range != "[2.0.0, )" {
		t.Errorf("1.0.0: %+v", d)
	}
	if d := got["1.1.0"]; d == nil || !slices.Equal(d.Reasons, []string{"Other"}) || d.AlternatePackage.Range != "*" {
		t.Errorf("1.1.0: %+v", d)
	}
	if d, named := got["2.0.0"]; !named || d != nil {
		t.Errorf("2.0.0: named %v, %+v", named, d)
	}
}
