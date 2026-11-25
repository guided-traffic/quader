package oci

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// Every decision of a route names the item it acts on (R-AUTHZ-005): a grant or a pattern scoped by path decides on the canonical path
// of the layout, the same one `/files/` classifies.
//
// Mechanism of R-OCI-006, not a proof of its criteria: it checks one step of the decision (ownership, shadowing, the canonical
// path, the token ceiling); the criteria are the overwrite, delete and catalog outcomes.
func TestEveryDecisionNamesTheCanonicalPathOfTheItem(t *testing.T) {
	var seen []formats.Target
	fx := newFixture(t, formattest.HostOptions{Allow: func(tg formats.Target) error {
		seen = append(seen, tg)
		if strings.HasPrefix(tg.Path, "secret/") {
			return errs.E(errs.Forbidden, errs.CodeForbidden, "path excluded")
		}
		return nil
	}})
	m, d := fx.image("app", "1", "layer")
	layer := sha([]byte("layer"))
	fx.wantStatus(fx.get("app/manifests/1"), http.StatusOK, "")
	fx.wantStatus(fx.get("app/manifests/"+d), http.StatusOK, "")
	fx.wantStatus(fx.get("app/blobs/"+layer), http.StatusOK, "")
	fx.wantStatus(fx.put("app/manifests/2", m, ctImage), http.StatusCreated, "")
	fx.wantStatus(fx.put("app/manifests/"+d, m, ctImage), http.StatusCreated, "")
	fx.wantStatus(fx.get("app/tags/list"), http.StatusOK, "")
	subject := sha([]byte("a subject nothing was pushed for"))
	fx.wantStatus(fx.get("app/referrers/"+subject), http.StatusOK, "")
	want := map[string]bool{
		TagPath("app", "1"): true, TagPath("app", "2"): true, ManifestPath("app", d): true, BlobPath("app", layer): true,
		TaglistPath("app"): true, ManifestPath("app", subject): true, // the tags list: the package document; referrers: the subject (Q-OCI-140)
	}
	got := map[string]bool{}
	for _, tg := range seen {
		if tg.Path == "" {
			t.Errorf("a decision without a path: %+v", tg)
		}
		got[tg.Path] = true
	}
	for p := range want {
		if !got[p] {
			t.Errorf("no decision for %s (decided: %v)", p, got)
		}
	}
	// The ownership claim of a push names the item it writes: the tag path, or the manifest path for a push by digest (Q-OCI-140).
	m3 := imageManifestBytes(fx.blob("app", []byte("{}")), fx.blob("app", []byte("digest layer")))
	d3 := sha(m3)
	fx.wantStatus(fx.put("app/manifests/"+d3, m3, ctImage), http.StatusCreated, "")
	claimed := map[string]bool{}
	for _, c := range fx.h.Claims() {
		if c.Target.Path == "" {
			t.Errorf("a claim without a path: %+v", c.Target)
		}
		claimed[c.Target.Path] = true
	}
	if !claimed[TagPath("app", "2")] || !claimed[ManifestPath("app", d3)] {
		t.Errorf("claimed paths = %v, want the tag path and the manifest path", claimed)
	}
	// A path the policy excludes is refused on pulls and pushes alike.
	cfg := fx.blob("secret/app", []byte("{}"))
	sm := imageManifestBytes(cfg)
	fx.wantStatus(fx.put("secret/app/manifests/1", sm, ctImage), http.StatusForbidden, "DENIED")
	fx.wantStatus(fx.get("secret/app/blobs/"+cfg), http.StatusForbidden, "")
	fx.wantStatus(fx.get("secret/app/manifests/1"), http.StatusForbidden, "")
	fx.wantStatus(fx.get("secret/app/manifests/"+sha(sm)), http.StatusForbidden, "")
}

// A tag that is pushed again is no publish: the pre-check says so, as the commit does (a new tag and a manifest by digest are).
//
// Mechanism of R-OCI-006, not a proof of its criteria: it checks one step of the decision (ownership, shadowing, the canonical
// path, the token ceiling); the criteria are the overwrite, delete and catalog outcomes.
func TestThePrecheckSaysPublishForNewContentOnly(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{})
	m, d := fx.image("app", "1", "layer")
	last := func() formats.OwnershipWrite {
		c := fx.h.OwnershipChecks()
		return c[len(c)-1].Write
	}
	if w := last(); !w.Publish || !w.FirstVersion {
		t.Fatalf("a new tag of a new name: %+v", w)
	}
	fx.wantStatus(fx.put("app/manifests/1", m, ctImage), http.StatusCreated, "")
	if w := last(); w.Publish || w.FirstVersion {
		t.Fatalf("a tag pushed again: %+v", w)
	}
	fx.wantStatus(fx.put("app/manifests/2", m, ctImage), http.StatusCreated, "")
	if w := last(); !w.Publish || w.FirstVersion {
		t.Fatalf("a new tag of a name with versions: %+v", w)
	}
	fx.wantStatus(fx.put("app/manifests/"+d, m, ctImage), http.StatusCreated, "")
	if w := last(); !w.Publish {
		t.Fatalf("a manifest by digest: %+v", w)
	}
}

// Pushing the same tag again brings back a manifest item that was removed around the tag (a raw delete).
//
// Mechanism of R-OCI-013, not a proof of its criteria: it checks the parser, one step of the commit transaction or a store query;
// the criteria are the refusals by size and media type, the foreign layer, the artifact round trip and the concurrent tag.
func TestPushingATagAgainRestoresARemovedManifestItem(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{})
	m, d := fx.image("app", "1", "layer")
	ctx := context.Background()
	rq, _ := fx.h.NewRequest(ctx, http.MethodDelete, "app/manifests/"+d, nil)
	if err := fx.h.Remove(ctx, rq, "delete", ManifestPath("app", d)); err != nil {
		t.Fatal(err)
	}
	fx.wantStatus(fx.get("app/manifests/"+d), http.StatusNotFound, "MANIFEST_UNKNOWN")
	fx.wantStatus(fx.put("app/manifests/1", m, ctImage), http.StatusCreated, "")
	fx.wantStatus(fx.get("app/manifests/"+d), http.StatusOK, "")
}

// The tag is linked to a stored manifest; when that manifest is gone at the commit, the commit is refused inside the transaction and
// the push commits the manifest with the tag, so no tag points at a manifest that is not stored.
//
// Mechanism of R-OCI-013, not a proof of its criteria: it checks the parser, one step of the commit transaction or a store query;
// the criteria are the refusals by size and media type, the foreign layer, the artifact round trip and the concurrent tag.
func TestALinkedTagChecksTheManifestInTheTransaction(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{})
	cfg := fx.blob("app", []byte("{}"))
	m := imageManifestBytes(cfg)
	d := sha(m)
	fx.wantStatus(fx.put("app/manifests/"+d, m, ctImage), http.StatusCreated, "")
	fx.twin.hideInTx = map[string]bool{ManifestPath("app", d): true}
	fx.wantStatus(fx.put("app/manifests/latest", m, ctImage), http.StatusCreated, "")
	fx.wantStatus(fx.get("app/manifests/latest"), http.StatusOK, "")
	fx.wantStatus(fx.get("app/manifests/"+d), http.StatusOK, "")
	if vs, _, _ := fx.h.Packages().Versions(context.Background(), fx.h.Ref(), "app"); len(vs) != 1 {
		t.Fatalf("versions = %+v", vs)
	}
}

// The list of packages asks for the summary, which an image has none of: no read happens; the details read once for any number of tags.
//
// Mechanism of R-OCI-031, not a proof of its criteria: it checks the tag order or one step of the view; the criteria are the
// platforms in the API metadata and the sizeBytes of the image.
func TestTheSummaryReadsNothingAndTheDetailsReadOnce(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{})
	fx.image("a", "1", "one")
	fx.image("b", "1", "two")
	ctx := context.Background()
	va, _, _ := fx.h.Packages().Versions(ctx, fx.h.Ref(), "a")
	vb, _, _ := fx.h.Packages().Versions(ctx, fx.h.Ref(), "b")
	vs := []formats.ViewVersion{
		{ID: va[0].ID, Repo: fx.h.Ref(), Package: "a", Version: "1"}, {ID: vb[0].ID, Repo: fx.h.Ref(), Package: "b", Version: "1"},
	}
	view := New().View()
	out, err := view.Describe(ctx, fx.h, vs, formats.ViewSummary, nil)
	if err != nil || len(out) != 0 || fx.twin.calls["TagViews"] != 0 {
		t.Fatalf("summary: %v %v, %d reads", out, err, fx.twin.calls["TagViews"])
	}
	out, err = view.Describe(ctx, fx.h, vs, formats.ViewDetails, nil)
	if err != nil || len(out) != 2 || fx.twin.calls["TagViews"] != 1 {
		t.Fatalf("details: %v %v, %d reads", out, err, fx.twin.calls["TagViews"])
	}
}

// The sizes of the tags of a page are read once, answer for the ids asked and no other, and are the size of the image.
//
// Mechanism of R-OCI-031, not a proof of its criteria: it checks the tag order or one step of the view; the criteria are the
// platforms in the API metadata and the sizeBytes of the image.
func TestTheSizesReadOnceAndAnswerForTheTagsAsked(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{})
	fx.image("a", "1", "one")
	fx.image("b", "1", "two")
	ctx := context.Background()
	va, _, _ := fx.h.Packages().Versions(ctx, fx.h.Ref(), "a")
	vb, _, _ := fx.h.Packages().Versions(ctx, fx.h.Ref(), "b")
	both := []formats.ViewVersion{
		{ID: va[0].ID, Repo: fx.h.Ref(), Package: "a", Version: "1"}, {ID: vb[0].ID, Repo: fx.h.Ref(), Package: "b", Version: "1"},
	}
	var f formats.ViewSizer = New()
	got, err := f.ViewSizes(ctx, fx.h, both)
	if err != nil || len(got) != 2 || fx.twin.calls["TagSizes"] != 1 || fx.twin.calls["TagViews"] != 0 {
		t.Fatalf("sizes: %v %v, %d reads of the sizes, %d of the views", got, err, fx.twin.calls["TagSizes"], fx.twin.calls["TagViews"])
	}
	for _, v := range both {
		if s := got[v.ID]; s.Bytes <= 0 || s.Partial || s.Unknown {
			t.Errorf("the size of %s:%s = %+v", v.Package, v.Version, s)
		}
	}
	one, err := f.ViewSizes(ctx, fx.h, both[:1])
	if err != nil || len(one) != 1 || one[va[0].ID] != got[va[0].ID] {
		t.Fatalf("one tag asked: %v %v", one, err)
	}
	if none, err := f.ViewSizes(ctx, fx.h, nil); err != nil || len(none) != 0 {
		t.Fatalf("none asked: %v %v", none, err)
	}
}

// TestR_OCI_031_ATagThatIsNoVersionIsNoVersionOfTheFormat: a tag the tag order does not rank as a version (`latest`, `main`,
// `sha-5ccfd1a`) is no version of the format, as a string that is no tag is not: both sort before every version and among themselves by
// their bytes, their key has the class versionkey.NotVersion, and every version's has the class Version (Q-OCI-269), so a list orders
// them below the versions by their time (api-and-ui.md R-API-029). Milliseconds, a table of strings.
func TestR_OCI_031_ATagThatIsNoVersionIsNoVersionOfTheFormat(t *testing.T) {
	v := New().View()
	unranked := []string{"zz z", "", "-x", "\x00?!", "latest", "main", "sha-5ccfd1a", "abc", "a", "Latest", "_x", "edge"}
	versions := []string{"1.0", "v2", "1.0-rc1", "5.4.7", "1.29.3-perl", "20240315"}
	for _, u := range unranked {
		if k := v.SortKey(u); k[0] != versionkey.NotVersion {
			t.Errorf("the key of %q has the class %#x, want NotVersion", u, k[0])
		}
		for _, ver := range versions {
			if v.Compare(u, ver) >= 0 || v.Compare(ver, u) <= 0 {
				t.Errorf("%q against the version %q", u, ver)
			}
		}
		for _, w := range unranked {
			if c, want := v.Compare(u, w), strings.Compare(u, w); (c < 0) != (want < 0) || (c > 0) != (want > 0) {
				t.Errorf("Compare(%q, %q) = %d, want the bytes' order %d", u, w, c, want)
			}
		}
	}
	for _, ver := range versions {
		if k := v.SortKey(ver); k[0] != versionkey.Version {
			t.Errorf("the key of %q has the class %#x, want Version", ver, k[0])
		}
	}
}
