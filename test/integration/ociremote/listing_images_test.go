//go:build integration

package ociremote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_API_029_TheListByContentIsTheGroupingOfTheListByTag: the list by content (group=content, Q-API-432) of a remote image holds one
// entry per manifest, led by its first tag in the order of the versions, with the other tags as aliases in that order; read page by page
// to its end it equals the grouping of the list by tag read whole, the oracle. The image is a CI's: 1,100 manifests with a version tag
// and a commit tag each (`1.<g>`, `sha-<g>`), three manifests with two tags that are no version (`main-<g>`, `edge-<g>`, fetched last),
// and the pulled `3.19`; so the versions lead, the three unranked images follow newest first by time, and the walk then passes 1,100
// commit tags that lead nothing, which the scan budget cuts into short pages with a cursor (R-API-010). The lead check of one batch reads
// about one tag entry per candidate. 2,206 tiny entries and 1,103 manifests written by SQL; about 6 s on the stack.
func TestR_API_029_TheListByContentIsTheGroupingOfTheListByTag(t *testing.T) {
	e := newEnv(t, options{})
	web := e.start(1)[0]
	e.pull(web, rAnon, "library/alpine", "3.19")
	e.st.Exec(`INSERT INTO blobs (sha256, sha1, md5, sha512, size)
		SELECT sha256(('m' || g)::bytea), substring(sha512(('m' || g)::bytea) FROM 1 FOR 20), substring(sha512(('m' || g)::bytea) FROM 1 FOR 16),
		       sha512(('m' || g)::bytea), 10
		  FROM generate_series(1, 1103) g`)
	e.st.Exec(`INSERT INTO oci_manifests (repo_id, name, digest, media_type, size, content_size)
		SELECT om.repo_id, om.name, sha256(('m' || g)::bytea), om.media_type, om.size, om.content_size
		  FROM oci_manifests om, generate_series(1, 1103) g
		 WHERE om.name = 'library/alpine' AND om.repo_id = (SELECT id FROM repositories WHERE key = $1)`, rAnon)
	e.st.Exec(`INSERT INTO remote_cache_entries (repo_id, path, variant, kind, blob_sha256, upstream_url, fetched_at, validated_at, expires_at,
	                                              last_used_at, background_sha256)
		SELECT c.repo_id, 'library/alpine/_tags/' || t.tag, '', 'metadata', sha256(('m' || t.g)::bytea), c.upstream_url,
		       c.fetched_at + make_interval(secs => t.g * 2 + t.n), c.validated_at, c.expires_at, c.last_used_at, sha256(('m' || t.g)::bytea)
		  FROM remote_cache_entries c,
		       (SELECT g, 0 AS n, CASE WHEN g <= 1100 THEN '1.' || g ELSE 'main-' || g END AS tag FROM generate_series(1, 1103) g
		        UNION ALL
		        SELECT g, 1, CASE WHEN g <= 1100 THEN 'sha-' || g ELSE 'edge-' || g END FROM generate_series(1, 1103) g) t
		 WHERE c.path = 'library/alpine/_tags/3.19' AND c.kind = 'metadata'`)
	e.st.Exec("ANALYZE remote_cache_entries")
	base := "/api/v1/packages/" + view.ID("oci", "library/alpine") + "/versions?repository=" + url.QueryEscape(testenv.DefaultProject+"/"+rAnon)

	type item struct {
		Version     string   `json:"version"`
		Digest      string   `json:"digest"`
		Aliases     []string `json:"aliases"`
		AliasesMore bool     `json:"aliasesMore"`
	}
	read := func(path string) ([]item, int, int) {
		t.Helper()
		var all []item
		requests, short := 0, 0
		cursor := ""
		for {
			requests++
			var page struct {
				Items      []item  `json:"items"`
				NextCursor *string `json:"nextCursor"`
			}
			a := e.st.Do(web, http.MethodGet, path+cursor, e.admin, nil)
			if a.Status != http.StatusOK || json.Unmarshal(a.Body, &page) != nil {
				t.Fatalf("%s: %d %s", path+cursor, a.Status, a.Body)
			}
			all = append(all, page.Items...)
			if page.NextCursor == nil {
				return all, requests, short
			}
			if requests > 200 {
				t.Fatalf("%s: no end after 200 requests", path)
			}
			if strings.Contains(path, "limit=50") && len(page.Items) < 50 {
				short++
			}
			cursor = "&cursor=" + url.QueryEscape(*page.NextCursor)
		}
	}
	tags, _, _ := read(base + "&include=digest&limit=500")
	if len(tags) != 2207 {
		t.Fatalf("the list by tag holds %d tags, want 2,207", len(tags))
	}
	// The oracle: the first tag of each digest leads, the others follow it in the list's order.
	var want []item
	at := map[string]int{}
	for _, tg := range tags {
		if i, ok := at[tg.Digest]; ok {
			want[i].Aliases = append(want[i].Aliases, tg.Version)
			continue
		}
		at[tg.Digest] = len(want)
		want = append(want, item{Version: tg.Version, Digest: tg.Digest, Aliases: []string{}})
	}
	images, requests, short := read(base + "&group=content&limit=50")
	if len(images) != len(want) {
		t.Fatalf("the list by content holds %d images, want %d", len(images), len(want))
	}
	for i := range want {
		g := images[i]
		if g.Version != want[i].Version || g.Digest != want[i].Digest || !slices.Equal(g.Aliases, want[i].Aliases) || g.AliasesMore {
			t.Fatalf("image %d: %+v, want %+v", i, g, want[i])
		}
	}
	if got := []string{images[0].Version, images[1].Version, images[1100].Version, images[1101].Version, images[1103].Version}; !slices.Equal(got,
		[]string{"3.19", "1.1100", "1.1", "edge-1103", "edge-1101"}) || !slices.Equal(images[1101].Aliases, []string{"main-1103"}) ||
		!slices.Equal(images[1100].Aliases, []string{"sha-1"}) {
		t.Errorf("the leaders 0, 1, 1100, 1101, 1103: %v, the aliases of 1.1 %v and edge-1103 %v", got, images[1100].Aliases, images[1101].Aliases)
	}
	// 1,104 images in pages of 50 are 23 requests; the 1,100 commit tags after the last image pass the scan budget of 1,024 tags, so at
	// least one page is short with a cursor, and at most two.
	if requests > 25 || short < 1 || short > 2 {
		t.Errorf("%d requests, %d of them short, to the end", requests, short)
	}
	t.Logf("the list by content: %d requests to its end, %d short", requests, short)

	// The lead check of one batch: 64 commit tags, each a probe of its version tag's entry.
	var vs, ds, ks, as []string
	for g := 1; g <= 64; g++ {
		vs = append(vs, fmt.Sprintf("'sha-%d'", g))
		ds = append(ds, fmt.Sprintf("sha256('m%d'::bytea)", g))
		ks = append(ks, `'\x00'::bytea`)
		as = append(as, "now()::text")
	}
	seen := testenv.QueryText(t, "package_view.sql", "PackageViewImageSeen", map[string]string{
		"versions": "ARRAY[" + strings.Join(vs, ",") + "]", "digests": "ARRAY[" + strings.Join(ds, ",") + "]",
		"keys": "ARRAY[" + strings.Join(ks, ",") + "]", "ats": "ARRAY[" + strings.Join(as, ",") + "]",
		"package_id": "(SELECT id FROM packages WHERE name = 'library/alpine')", "withheld": "'{}'", "whole": "true", "allowed": "'{}'",
		"remote": "true", "repo_id": "(SELECT id FROM repositories WHERE key = '" + rAnon + "')", "name": "'library/alpine'", "by_time": "true",
	})
	if n := e.st.Text("SELECT count(*)::text FROM (" + seen + ") x"); n != "64" {
		t.Errorf("the lead check found %s of 64 commit tags seen", n)
	}
	if rows := e.st.RowsRead(seen)["remote_cache_entries"]; rows > 3*64 {
		t.Errorf("the lead check of 64 candidates read %v tag entries", rows)
	}
}
