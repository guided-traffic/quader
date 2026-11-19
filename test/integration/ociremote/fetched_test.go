//go:build integration

package ociremote

import (
	"encoding/json"
	"testing"
	"time"
)

// TestR_API_081_ARemoteListsATagOnceAClientFetchedItAndEveryCellIsTrue is the owner's objection of 2026-11-19 (W-24): a remote lists
// what was fetched through it, the downloads and the last download count the pulls of the tag, and a size that is not known is no
// number. A HEAD of the tag checks it and lists nothing; the GET of the tag lists it with one download; the GET of the index by its
// digest is a second pull of the tag, the GET of a platform manifest is none; the size of the index is not known while no platform is
// held, partial while one of two is and whole once both are (Q-OCI-221, Q-OCI-222).
func TestR_API_081_ARemoteListsATagOnceAClientFetchedItAndEveryCellIsTrue(t *testing.T) {
	e := newEnv(t, options{})
	rep := e.start(1)[0]

	if a := e.do(rep, "HEAD", rAnon, "acme/b/manifests/latest"); a.Status != 200 {
		t.Fatalf("HEAD of the tag: %d", a.Status)
	}
	time.Sleep(2500 * time.Millisecond) // two flush intervals: a download, had one been counted, would be in the database
	if got := e.listing(rep, e.reader, rAnon); len(got) != 0 {
		t.Fatalf("a tag a client only checked is listed: %v", keysOf(got))
	}

	a := e.get(rep, rAnon, "acme/b/manifests/latest")
	if a.Status != 200 {
		t.Fatalf("GET of the tag: %d %s", a.Status, a.Body)
	}
	index := a.Header.Get("Docker-Content-Digest")
	var idx struct {
		Manifests []struct {
			Digest   string
			Platform struct{ OS, Architecture string }
		}
	}
	if err := json.Unmarshal(a.Body, &idx); err != nil {
		t.Fatal(err)
	}
	var platforms []string
	for _, m := range idx.Manifests {
		if m.Platform.OS != "unknown" {
			platforms = append(platforms, m.Digest)
		}
	}
	if len(platforms) != 2 {
		t.Fatalf("the index names %d platforms, want 2: %s", len(platforms), a.Body)
	}
	e.fetched(rAnon, index)
	tag := func() map[string]any {
		t.Helper()
		id := e.listing(rep, e.reader, rAnon)["acme/b"]["id"].(string)
		vs := e.tags(rep, e.reader, id, rAnon)
		if len(vs) != 1 || vs[0]["version"] != "latest" {
			t.Fatalf("the tags of acme/b: %v", vs)
		}
		return vs[0]
	}
	v := tag()
	if v["downloadCount"] != float64(1) || v["lastDownloadedAt"] == nil {
		t.Errorf("after the GET of the tag: downloadCount %v, lastDownloadedAt %v, want 1 and a time", v["downloadCount"], v["lastDownloadedAt"])
	}
	if v["sizeBytes"] != nil || v["sizePartial"] != false {
		t.Errorf("no platform is held: sizeBytes %v, sizePartial %v, want null and false (R-API-080)", v["sizeBytes"], v["sizePartial"])
	}

	// One platform: the size is that platform's, partial; the pull of a platform manifest is no second pull of the tag.
	if a := e.get(rep, rAnon, "acme/b/manifests/"+platforms[0]); a.Status != 200 {
		t.Fatalf("GET of a platform: %d", a.Status)
	}
	v = tag()
	if size, _ := v["sizeBytes"].(float64); size <= 0 || v["sizePartial"] != true {
		t.Errorf("one platform held: sizeBytes %v, sizePartial %v, want a size and true", v["sizeBytes"], v["sizePartial"])
	}
	partial, _ := v["sizeBytes"].(float64)

	// The GET of the index by its digest (the pull of a client that resolved the tag with a HEAD) is a pull of the tag.
	if a := e.get(rep, rAnon, "acme/b/manifests/"+index); a.Status != 200 {
		t.Fatalf("GET of the index by digest: %d", a.Status)
	}
	eventually(t, 10*time.Second, "the pull by digest to be counted", func() bool { return tag()["downloadCount"] == float64(2) })

	if a := e.get(rep, rAnon, "acme/b/manifests/"+platforms[1]); a.Status != 200 {
		t.Fatalf("GET of the second platform: %d", a.Status)
	}
	v = tag()
	if size, _ := v["sizeBytes"].(float64); size <= partial || v["sizePartial"] != false {
		t.Errorf("both platforms held: sizeBytes %v (one was %v), sizePartial %v, want the whole sum", v["sizeBytes"], partial, v["sizePartial"])
	}
	time.Sleep(2500 * time.Millisecond)
	if v = tag(); v["downloadCount"] != float64(2) {
		t.Errorf("the platform manifests counted as pulls of the tag: downloadCount %v", v["downloadCount"])
	}

	// The tag's page lists every platform with its own size, or as not held.
	id := e.listing(rep, e.reader, rAnon)["acme/b"]["id"].(string)
	d := e.st.Do(rep, "GET", "/api/v1/packages/"+id+"/versions/latest", e.reader, nil)
	if d.Status != 200 {
		t.Fatalf("the details: %d %s", d.Status, d.Body)
	}
	md := d.JSON(t)["metadata"].(map[string]any)
	ps, _ := md["platforms"].([]any)
	held := 0
	for _, p := range ps {
		pm := p.(map[string]any)
		if pm["stored"] == true {
			held++
			if pm["size"] == nil {
				t.Errorf("a held platform has no size: %v", pm)
			}
		} else if pm["size"] != nil {
			t.Errorf("a platform that is not held states a size: %v", pm)
		}
	}
	if len(ps) != 4 || held != 2 {
		t.Errorf("the details list %d platforms, %d held: want the two images and the two attestations, the images held", len(ps), held)
	}
}
