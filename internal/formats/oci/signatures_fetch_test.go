package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// referrersAnswer is the referrers API's index of the manifests given.
func referrersAnswer(t *testing.T, ms ...[]byte) *discover.Response {
	t.Helper()
	type entry struct {
		MediaType    string `json:"mediaType"`
		Digest       string `json:"digest"`
		Size         int    `json:"size"`
		ArtifactType string `json:"artifactType"`
	}
	idx := struct {
		SchemaVersion int     `json:"schemaVersion"`
		MediaType     string  `json:"mediaType"`
		Manifests     []entry `json:"manifests"`
	}{SchemaVersion: 2, MediaType: "application/vnd.oci.image.index.v1+json", Manifests: []entry{}}
	for _, m := range ms {
		var head struct {
			ArtifactType string `json:"artifactType"`
		}
		if err := json.Unmarshal(m, &head); err != nil {
			t.Fatal(err)
		}
		idx.Manifests = append(idx.Manifests, entry{MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: sha(m), Size: len(m), ArtifactType: head.ArtifactType})
	}
	body, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	return &discover.Response{Status: http.StatusOK, Body: body}
}

// signatureFixture is a stub upstream of the signature fetch: what each request of R-SIG-026 answers.
type signatureFixture struct {
	referrers *discover.Response // nil: the API answers 404
	tags      map[string]bool    // the attachment tags the upstream has
}

func (s signatureFixture) answer(u *url.URL) *discover.Response {
	switch {
	case strings.Contains(u.Path, "/referrers/"):
		if s.referrers == nil {
			return &discover.Response{Status: http.StatusNotFound}
		}
		return s.referrers
	case strings.Contains(u.Path, "/manifests/"):
		tag := u.Path[strings.LastIndex(u.Path, "/")+1:]
		if s.tags[tag] {
			return &discover.Response{Status: http.StatusOK}
		}
	}
	return &discover.Response{Status: http.StatusNotFound}
}

// TestR_SIG_026_TheFetchAsksTheReferrersAndTheOlderTags: with the referrers API, the fetch asks it once, the .sig and the .att tags by
// HEAD and never the referrers tag; it stores the signature manifests signatures first, the layers that hold a signature, and an SBOM's
// manifest without its layers. Unit, milliseconds, manifests built by the test.
func TestR_SIG_026_TheFetchAsksTheReferrersAndTheOlderTags(t *testing.T) {
	subject := seedDigest
	notation := signatureManifest(artifactNotation, emptyConfig, seedSubject(), nil, descOf(layerJWS, sigEnvelope))
	spdx := signatureManifest("application/spdx+json", emptyConfig, seedSubject(), nil, descOf("application/spdx+json", sigSPDX))
	sigTag := "sha256-" + hexDigits(subject) + ".sig"
	sig := signatureManifest("", descOf("application/vnd.oci.image.config.v1+json", seedConfig), nil, nil,
		descOf(layerSimpleSigning, sigPayload1), descOf(layerSimpleSigning, sigPayload2))
	f := newFake(t, "https://registry.example.com/", ``)
	f.sig = signatureFixture{referrers: referrersAnswer(t, notation, spdx), tags: map[string]bool{sigTag: true}}.answer
	f.artifacts[ManifestPath("acme/a", sha(notation))] = notation
	f.artifacts[ManifestPath("acme/a", sha(spdx))] = spdx
	f.docs[TagPath("acme/a", sigTag)] = sig

	if err := adapter().FetchSignatures(context.Background(), f, SignatureFetchTarget("acme/a", subject)); err != nil {
		t.Fatal(err)
	}
	wantGets := []string{
		"GET /v2/acme/a/referrers/" + subject,
		"HEAD /v2/acme/a/manifests/" + sigTag,
		"HEAD /v2/acme/a/manifests/sha256-" + hexDigits(subject) + ".att",
	}
	if !reflect.DeepEqual(f.sigGets, wantGets) {
		t.Errorf("requests %v, want %v", f.sigGets, wantGets)
	}
	wantAsked := []string{
		"artifact " + ManifestPath("acme/a", sha(notation)), "artifact " + BlobPath("acme/a", sha(sigEnvelope)),
		"doc " + TagPath("acme/a", sigTag), "artifact " + BlobPath("acme/a", sha(sigPayload1)), "artifact " + BlobPath("acme/a", sha(sigPayload2)),
		"artifact " + ManifestPath("acme/a", sha(spdx)),
	}
	if !reflect.DeepEqual(f.asked, wantAsked) {
		t.Errorf("stored %v, want %v", f.asked, wantAsked)
	}
}

// TestR_SIG_026_WithoutTheReferrersAPITheReferrersTagIsAsked: a 404 of the API sends the fetch to the referrers tag by HEAD; its index
// names the referrers that are then stored. A tag the upstream does not have costs a HEAD and no GET. Unit, milliseconds.
func TestR_SIG_026_WithoutTheReferrersAPITheReferrersTagIsAsked(t *testing.T) {
	subject := seedDigest
	notation := signatureManifest(artifactNotation, emptyConfig, seedSubject(), nil, descOf(layerJWS, sigEnvelope))
	fallback := "sha256-" + hexDigits(subject)
	f := newFake(t, "https://registry.example.com/", ``)
	f.sig = signatureFixture{tags: map[string]bool{fallback: true}}.answer
	f.docs[TagPath("acme/a", fallback)] = referrersAnswer(t, notation).Body
	f.artifacts[ManifestPath("acme/a", sha(notation))] = notation
	if err := adapter().FetchSignatures(context.Background(), f, SignatureFetchTarget("acme/a", subject)); err != nil {
		t.Fatal(err)
	}
	wantGets := []string{
		"GET /v2/acme/a/referrers/" + subject, "HEAD /v2/acme/a/manifests/" + fallback + ".sig", "HEAD /v2/acme/a/manifests/" + fallback + ".att",
		"HEAD /v2/acme/a/manifests/" + fallback,
	}
	if !reflect.DeepEqual(f.sigGets, wantGets) {
		t.Errorf("requests %v, want %v", f.sigGets, wantGets)
	}
	wantAsked := []string{
		"doc " + TagPath("acme/a", fallback), "artifact " + ManifestPath("acme/a", sha(notation)), "artifact " + BlobPath("acme/a", sha(sigEnvelope)),
	}
	if !reflect.DeepEqual(f.asked, wantAsked) {
		t.Errorf("stored %v, want %v", f.asked, wantAsked)
	}
}

// TestR_SIG_026_TheFetchStoresAtMostFourManifests: signatures.remote.max_manifests_per_subject bounds what one image costs; a target
// that names no manifest asks nothing. Unit, milliseconds.
func TestR_SIG_026_TheFetchStoresAtMostFourManifests(t *testing.T) {
	var ms [][]byte
	f := newFake(t, "https://registry.example.com/", ``)
	for i := range 6 {
		env := []byte(`{"payload":"e30","protected":"e30","signature":"` + strings.Repeat("A", i+1) + `"}`)
		m := signatureManifest(artifactNotation, emptyConfig, seedSubject(), nil, descOf(layerJWS, env))
		ms = append(ms, m)
		f.artifacts[ManifestPath("acme/a", sha(m))] = m
	}
	f.sig = signatureFixture{referrers: referrersAnswer(t, ms...)}.answer
	if err := adapter().FetchSignatures(context.Background(), f, SignatureFetchTarget("acme/a", seedDigest)); err != nil {
		t.Fatal(err)
	}
	manifests := 0
	for _, a := range f.asked {
		if strings.Contains(a, "/_manifests/") {
			manifests++
		}
	}
	if manifests != maxFetchManifests {
		t.Errorf("%d manifests stored, want %d", manifests, maxFetchManifests)
	}
	g := newFake(t, "https://registry.example.com/", ``)
	for _, target := range []string{"", "acme/a", "acme/a@1.0", "ACME@" + seedDigest} {
		if err := adapter().FetchSignatures(context.Background(), g, target); err != nil {
			t.Errorf("%q: %v", target, err)
		}
	}
	if len(g.sigGets)+len(g.asked) != 0 {
		t.Errorf("a target that names no manifest asked %v %v", g.sigGets, g.asked)
	}
}

// TestR_SIG_025_TheMirrorPlanOfATagAsksForItsSignatures: the plan of a kept tag asks for the signatures of its manifest as the
// background fetch does ("Referrers are not mirrored" is superseded); an attachment tag of the list is no version of the plan. Unit,
// milliseconds.
func TestR_SIG_025_TheMirrorPlanOfATagAsksForItsSignatures(t *testing.T) {
	body, d := imageManifest("a", "l")
	notation := signatureManifest(artifactNotation, emptyConfig, &sigDesc{MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: d, Size: len(body)}, nil, descOf(layerJWS, sigEnvelope))
	f := newFake(t, "https://registry.example.com/", `{"mirror":{"tags":["*"]}}`)
	f.get = func(*url.URL) *discover.Response { return tagsAnswer("1.0", "sha256-"+hexDigits(d)+".sig") }
	f.docs[TagPath("acme/a", "1.0")] = body
	f.artifacts[ManifestPath("acme/a", sha(notation))] = notation
	f.sig = signatureFixture{referrers: referrersAnswer(t, notation)}.answer
	plan, err := adapter().MirrorPlan(context.Background(), f, "acme/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.sigGets) == 0 || f.sigGets[0] != "GET /v2/acme/a/referrers/"+d {
		t.Errorf("signature requests %v, want the referrers of %s first", f.sigGets, d)
	}
	stored := false
	for _, a := range f.asked {
		stored = stored || a == "artifact "+ManifestPath("acme/a", sha(notation))
	}
	if !stored {
		t.Errorf("the referrer was not stored: %v", f.asked)
	}
	for _, it := range plan.Items {
		if it.Version != "1.0" {
			t.Errorf("an item of the version %q, want only 1.0", it.Version)
		}
	}
}

// fillWriter is the package writer of a fill test: the two calls recordFilled makes.
type fillWriter struct{ formats.PackageWriter }

func (fillWriter) EnsureLocked(_ context.Context, repo formats.RepoRef, n, _ string) (formats.PackageRef, bool, error) {
	return formats.PackageRef{ID: 1, Repo: repo, Name: n}, false, nil
}

func (fillWriter) BumpRevision(context.Context, formats.PackageRef) (formats.RevSeq, time.Time, error) {
	return 2, time.Now(), nil
}

// TestR_SIG_026_AFillOfATopLevelManifestQueuesTheFetch: the fill of a tag's manifest queues the background fetch of its signatures; the
// fill of an attachment tag and of a signature manifest queues none; a manifest fetched by digest queues it when no stored index lists
// it. Unit, milliseconds.
func TestR_SIG_026_AFillOfATopLevelManifestQueuesTheFetch(t *testing.T) {
	a := adapter()
	tw := newTwin()
	repo := formats.RepoRef{ID: 7, Class: formats.Remote, Format: ID}
	fillTag := func(tag string, body []byte) {
		t.Helper()
		p := formats.Path{Path: TagPath("acme/a", tag)}
		prep, err := a.PrepareFill(context.Background(), rctx(t, "https://registry.example.com/"), p, "", bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.OnFill(context.Background(), nil, formats.FillEvent{Repo: repo, Path: p, Prepared: prep, Packages: fillWriter{}, Data: tw}); err != nil {
			t.Fatal(err)
		}
	}
	body, d := imageManifest("top", "l")
	fillTag("1.0", body)
	sig := signatureManifest("", descOf("application/vnd.oci.image.config.v1+json", seedConfig), nil, nil, descOf(layerSimpleSigning, sigPayload1))
	fillTag("sha256-"+hexDigits(d)+".sig", sig)
	fillTag("sig-under-a-plain-tag", sig)
	if want := []string{SignatureFetchTarget("acme/a", d)}; !reflect.DeepEqual(tw.fetches, want) {
		t.Errorf("queued %v, want %v", tw.fetches, want)
	}

	byDigest, d2 := imageManifest("pulled-by-digest", "l2")
	ins, err := a.InspectFill(context.Background(), rctx(t, "https://registry.example.com/"), formats.Path{Path: ManifestPath("acme/a", d2)}, spooled(byDigest))
	if err != nil {
		t.Fatal(err)
	}
	if err := New().remoteCommit(context.Background(), nil, formats.CommitEvent{Repo: repo, Inspection: ins, Packages: fillWriter{}, Data: tw}); err != nil {
		t.Fatal(err)
	}
	if n := len(tw.fetches); n != 2 || tw.fetches[1] != SignatureFetchTarget("acme/a", d2) {
		t.Errorf("queued %v, want the manifest pulled by digest second", tw.fetches)
	}
}
