package v1

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// TestR_CUR_028_TheFactsOfAnUpstreamPackageAreTheSchemas: the API's enum of a link's kind lists the catalogue's kinds, and the about of
// a package answer is null where the answers told nothing and otherwise holds the members the schema requires, null where empty.
// Microseconds on the shared parse.
func TestR_CUR_028_TheFactsOfAnUpstreamPackageAreTheSchemas(t *testing.T) {
	links := specDocument(t).Components.Schemas["UpstreamAbout"].Value.Properties["links"].Value.Items.Value.Properties["kind"]
	var got []string
	for _, v := range links.Value.Enum {
		got = append(got, v.(string))
	}
	var want []string
	for _, k := range discover.LinkKinds() {
		want = append(want, string(k))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("R-CUR-028 the API's link kinds are %v, the catalogue's %v", got, want)
	}
	if aboutRow(discover.About{}) != nil {
		t.Fatal("R-CUR-028 facts that tell nothing are not null")
	}
	b, err := json.Marshal(aboutRow(discover.About{Version: "1.0.0", Dependencies: []discover.Dependency{{Name: "x"}},
		Deprecation: &discover.Deprecation{Reasons: []string{"Legacy"}}}))
	if err != nil {
		t.Fatal(err)
	}
	const wantJSON = `{"dependencies":[{"group":null,"name":"x","range":null}],"deprecation":{"alternate":null,"alternateRange":null,` +
		`"message":null,"reasons":["Legacy"]},"license":null,"links":[],"version":"1.0.0"}`
	if string(b) != wantJSON {
		t.Fatalf("R-CUR-028 about = %s\nwant %s", b, wantJSON)
	}
}
