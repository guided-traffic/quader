package ansible

import (
	"context"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// galaxyDocs is the recorded Galaxy with its documents by path: the root, a collection's version list, its collection document and its
// highest version's document.
type galaxyDocs struct {
	galaxy
	docs map[string][]byte
}

func (g *galaxyDocs) Document(_ context.Context, path, _ string) ([]byte, bool, error) {
	if path == RootDocument {
		return recordedGalaxy(g.t, "gal-root.json"), true, nil
	}
	b, ok := g.docs[path]
	return b, ok, nil
}

// TestR_ANS_047_TheCatalogueTellsTagsDownloadsAndSignedState: from the Galaxy's recorded answers, a hit of the keyword search carries its
// tags and the Galaxy's is_signed and no count (the search answers none), a collection of a namespace's listing carries the Galaxy's
// download_count, and an opened collection its count and its highest version's tags and signed state, one request more (R-ANS-047,
// Q-ANS-129). Milliseconds; answers of a few kilobytes.
func TestR_ANS_047_TheCatalogueTellsTagsDownloadsAndSignedState(t *testing.T) {
	const api = "https://galaxy.ansible.com/api/v3/"
	ctx := context.Background()
	g := &galaxyDocs{galaxy: galaxy{t: t, answers: map[string]discover.Response{
		api + "plugin/ansible/search/collection-versions/?is_highest=true&keywords=keycloak_realm&limit=2&offset=0": {Status: 200, Body: recordedGalaxy(t, "gal-search-1.json")},
		api + "collections/?limit=2&namespace=osism&offset=0":                                                       {Status: 200, Body: recordedGalaxy(t, "gal-ns-0.json")},
	}}, docs: map[string][]byte{
		VersionsDocumentPath("osism", "sonic"):         recordedGalaxy(t, "gal-versions.json"),
		CollectionDocumentPath("osism", "sonic"):       recordedGalaxy(t, "gal-collection.json"),
		VersionDocumentPath("osism", "sonic", "2.0.1"): recordedGalaxy(t, "gal-version.json"),
	}}

	page, err := remoteAdapter{}.Search(ctx, g, discover.SearchQuery{Text: "keycloak_realm", Limit: 2})
	if err != nil || len(page.Hits) == 0 {
		t.Fatalf("search: %v %+v", err, page)
	}
	h := page.Hits[0]
	if !slices.Contains(h.Keywords, "keycloak") || h.Signed == nil || *h.Signed || h.Downloads != nil {
		t.Errorf("a hit of the search: %+v", h.Popularity)
	}

	level, err := remoteAdapter{}.Children(ctx, g, discover.LevelRequest{Level: discover.LevelRef{Path: []string{"osism"}}, Limit: 2})
	if err != nil || len(level.Packages) == 0 {
		t.Fatalf("level: %v %+v", err, level)
	}
	if d := level.Packages[0].Downloads; d == nil || *d != 6272 {
		t.Errorf("a collection of the namespace's listing: %+v", level.Packages[0].Popularity)
	}

	info, err := remoteAdapter{}.Package(ctx, g, "osism.sonic", discover.VersionsRequest{Limit: 10})
	if err != nil || !info.Found {
		t.Fatalf("package: %v %+v", err, info)
	}
	if info.Downloads == nil || *info.Downloads != 718 || !slices.Equal(info.Keywords, []string{"networking", "nos", "sonic"}) ||
		info.Signed == nil || *info.Signed {
		t.Errorf("the opened collection: %+v", info.Popularity)
	}
}
