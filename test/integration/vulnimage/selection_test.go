//go:build integration

package vulnimage

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/vuln/store"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The selection of the image scanning against PostgreSQL and one web replica, without a worker or a scanner (Q-VULN-269, Q-VULN-270): the
// rows the job's statements read are written by the test, so each assertion needs no scan. Payloads of bytes.

const (
	indexType     = "application/vnd.oci.image.index.v1+json"
	ociConfigType = "application/vnd.oci.image.config.v1+json"
)

// selection is the stack, its web replica as a flow whose do and versionReport the test uses, and the store of the job's statements.
type selection struct {
	*flow
	images *store.Images
	repoID int64
}

func newSelection(t *testing.T) *selection {
	t.Helper()
	return newSelectionWith(t, `{"vulnerabilities":{"onMissingData":"allow"}}`)
}

// newSelectionWith is newSelection with the repository's settings.
func newSelectionWith(t *testing.T, settings string) *selection {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "vulnselect", Env: []string{
		"QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off", "QUADER_VULNERABILITIES_SOURCES_KEV_MODE=off", "QUADER_MAINTENANCE_TASKS=" + taskOff,
	}})
	rita := st.User("rita")
	st.Bind("admin", rita)
	st.Repository(repoKey, "local", "oci", settings)
	// A database in use, as its sync records it: an image whose platforms are all scanned is evaluated.
	st.Exec(`INSERT INTO vuln_source_state (source, ecosystem, state, etag, synced_at, full_synced_at) VALUES ('trivy', 'OCI', 'complete', $1, now(), now())`, dbDigest(1))
	f := &flow{t: t, st: st, adm: st.Token(rita, scope.Scope{Inherit: true, Admin: true})}
	f.api = st.Start(t, testenv.ReplicaOptions{Name: "web", Role: "web"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	db, err := postgres.Open(ctx, st.DatabaseConfig(), "vulnselect", "worker", testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return &selection{flow: f, images: store.NewImages(db), repoID: st.Number(`SELECT id FROM repositories WHERE key = $1`, repoKey)}
}

// pushBlob uploads one blob of the image name.
func (s *selection) pushBlob(name string, b []byte) {
	s.t.Helper()
	if a := s.do("POST", v2(name)+"/blobs/uploads/?digest="+digestOf(b), b, "Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
		s.t.Fatalf("blob upload: %d %s", a.Status, a.Body)
	}
}

// pushManifest stores a manifest under a reference (a tag or its digest) and returns its digest.
func (s *selection) pushManifest(name, ref, mediaType string, doc map[string]any) string {
	s.t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		s.t.Fatal(err)
	}
	if ref == "" {
		ref = digestOf(b)
	}
	if a := s.do("PUT", v2(name)+"/manifests/"+ref, b, "Content-Type", mediaType); a.Status != http.StatusCreated {
		s.t.Fatalf("manifest %s: %d %s", ref, a.Status, a.Body)
	}
	return digestOf(b)
}

// platformImage pushes an image of one platform by its digest and returns its descriptor in an index.
func (s *selection) platformImage(name, arch string) map[string]any {
	s.t.Helper()
	cfg := []byte(`{"architecture":"` + arch + `","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	layer := layerTar(s.t)
	s.pushBlob(name, cfg)
	s.pushBlob(name, layer)
	doc := map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": ociConfigType, "digest": digestOf(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digestOf(layer), "size": len(layer)}},
	}
	d := s.pushManifest(name, "", manifestType, doc)
	b, _ := json.Marshal(doc)
	return map[string]any{"mediaType": manifestType, "digest": d, "size": len(b), "platform": map[string]any{"architecture": arch, "os": "linux"}}
}

// attestation pushes Docker's attestation manifest of an image in the shape the recording of library/ubuntu:26.10 shows
// (internal/formats/oci/testdata/attestation): no subject, an image's config media type with the platform unknown, one in-toto layer. It
// returns its descriptor in an index, with Docker's annotations and the platform unknown/unknown.
func (s *selection) attestation(name, image string) map[string]any {
	s.t.Helper()
	statement := []byte(`{"_type":"https://in-toto.io/Statement/v0.1","predicateType":"https://spdx.dev/Document","subject":[{"name":"` + name +
		`","digest":{"sha256":"` + strings.TrimPrefix(image, "sha256:") + `"}}],"predicate":{}}`)
	cfg := []byte(`{"architecture":"unknown","os":"unknown","config":{},"rootfs":{"type":"layers","diff_ids":["` + digestOf(statement) + `"]}}`)
	s.pushBlob(name, cfg)
	s.pushBlob(name, statement)
	doc := map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": ociConfigType, "digest": digestOf(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{
			"mediaType": "application/vnd.in-toto+json", "digest": digestOf(statement), "size": len(statement),
			"annotations": map[string]string{"in-toto.io/predicate-type": "https://spdx.dev/Document"},
		}},
	}
	d := s.pushManifest(name, "", manifestType, doc)
	b, _ := json.Marshal(doc)
	return map[string]any{
		"mediaType": manifestType, "digest": d, "size": len(b), "platform": map[string]any{"architecture": "unknown", "os": "unknown"},
		"annotations": map[string]string{"vnd.docker.reference.type": "attestation-manifest", "vnd.docker.reference.digest": image},
	}
}

// multiPlatform pushes the tag 1.0 of the name as an index of two platform images, each followed by its attestation manifest, as Docker
// Hub's indexes are. It returns the two images' digests and the two attestations'.
func (s *selection) multiPlatform(name string) (images, attestations []string) {
	s.t.Helper()
	var entries []any
	for _, arch := range []string{"amd64", "arm64"} {
		img := s.platformImage(name, arch)
		att := s.attestation(name, img["digest"].(string))
		entries = append(entries, img, att)
		images, attestations = append(images, img["digest"].(string)), append(attestations, att["digest"].(string))
	}
	s.pushManifest(name, "1.0", indexType, map[string]any{"schemaVersion": 2, "mediaType": indexType, "manifests": entries})
	return images, attestations
}

// scanRows are the scan rows of the name as "<digest> <state>", in digest order.
func (s *selection) scanRows(name string) []string {
	s.t.Helper()
	var out []string
	s.st.Query(`SELECT 'sha256:' || encode(digest, 'hex') || ' ' || state FROM vuln_image_scan WHERE repo_id = $1 AND name = $2 ORDER BY digest`,
		func(r pgx.Rows) error {
			var v string
			if err := r.Scan(&v); err != nil {
				return err
			}
			out = append(out, v)
			return nil
		}, s.repoID, name)
	return out
}

// scanned records a finished detection of the manifest, as the job's VulnImageScanned does, against the database in use.
func (s *selection) scanned(name, digest string) {
	s.t.Helper()
	s.st.Exec(`UPDATE vuln_image_scan SET state = 'scanned', db_digest = $4, scanned_at = now(), os_family = 'ubuntu', os_name = '26.10', packages = 3
		WHERE repo_id = $1 AND name = $2 AND digest = decode($3, 'hex')`, s.repoID, name, strings.TrimPrefix(digest, "sha256:"), dbDigest(1))
}

// R-VULN-033, R-VULN-034, Q-VULN-269: a tag of Docker Hub's shape (an index of two platform images, each with its attestation manifest).
// Discovery queues the two images and not the attestations, and drops the failed row an attestation had from before (the owner's
// installation on 2026-11-24: layer_format, due in 25 days). The tag's evaluation is its platform images': pending while one waits,
// evaluated with both scanned, scan_failed while one failed; a child the index names and the repository does not store (a platform a
// remote has not fetched) does not count. The version view and the package list answer the same. Integration, about 3 s, bytes.
func TestR_VULN_034_AnImageIsItsPlatformImagesAndAnAttestationIsNoImageToScan(t *testing.T) {
	s := newSelection(t)
	images, attestations := s.multiPlatform("library/ubuntu")
	// The row an attestation got before the selection left it out.
	s.st.Exec(`INSERT INTO vuln_image_scan (repo_id, name, digest, state, attempts, error_code, error_detail, not_before)
		VALUES ($1, 'library/ubuntu', decode($2, 'hex'), 'failed', 1, 'layer_format', 'a layer is not a tar stream', now() + interval '25 days')`,
		s.repoID, strings.TrimPrefix(attestations[0], "sha256:"))

	if _, err := s.images.Discover(t.Context(), 0, 100); err != nil {
		t.Fatal(err)
	}
	want := []string{images[0] + " pending", images[1] + " pending"}
	slices.Sort(want)
	if got := s.scanRows("library/ubuntu"); !slices.Equal(got, want) {
		t.Fatalf("scan rows %q, want the two platform images alone, pending", got)
	}
	if r := s.versionReport("library/ubuntu", "1.0"); r.Repositories[0].Evaluation != "pending_scan" || len(r.Repositories[0].Images) != 2 {
		t.Fatalf("while both wait: %+v", r.Repositories[0])
	}

	s.scanned("library/ubuntu", images[0])
	if ev := s.versionReport("library/ubuntu", "1.0").Repositories[0].Evaluation; ev != "pending_scan" {
		t.Fatalf("with one platform scanned the evaluation is %q, want pending_scan", ev)
	}
	s.scanned("library/ubuntu", images[1])
	r := s.versionReport("library/ubuntu", "1.0")
	if ev := r.Repositories[0].Evaluation; ev != "evaluated" {
		t.Fatalf("with both platforms scanned the evaluation is %q: %+v", ev, r.Repositories[0])
	}
	if g := r.Repositories[0].Grade; g.Grade != "S" || g.CheckedAt == nil || g.Os == nil || *g.Os != "ubuntu 26.10" {
		t.Fatalf("grade %+v, want S with the scan time and the OS", g)
	}
	for _, m := range r.Repositories[0].Images {
		if !slices.Contains(images, m.Digest) || m.State != "scanned" {
			t.Fatalf("images %+v, want the two platform images", r.Repositories[0].Images)
		}
	}
	if g := s.listGrade("library/ubuntu"); g == nil || g.Grade != "S" || g.Evaluation != "evaluated" {
		t.Fatalf("the package list grades the image %+v, want S, evaluated", g)
	}

	// A platform the index names that the repository does not store, as a remote whose client pulled one platform holds the others.
	unfetched := strings.Repeat("ab", 32)
	s.st.Exec(`INSERT INTO oci_references (repo_id, name, manifest, target, kind, platform)
		SELECT repo_id, name, manifest, decode($2, 'hex'), 'manifest', 'linux/s390x' FROM oci_references
		 WHERE repo_id = $1 AND name = 'library/ubuntu' AND kind = 'manifest' LIMIT 1`, s.repoID, unfetched)
	if ev := s.versionReport("library/ubuntu", "1.0").Repositories[0].Evaluation; ev != "evaluated" {
		t.Fatalf("a platform the repository does not store made the evaluation %q", ev)
	}

	s.st.Exec(`UPDATE vuln_image_scan SET state = 'failed', error_code = 'layer_format' WHERE repo_id = $1 AND digest = decode($2, 'hex')`,
		s.repoID, strings.TrimPrefix(images[1], "sha256:"))
	if ev := s.versionReport("library/ubuntu", "1.0").Repositories[0].Evaluation; ev != "scan_failed" {
		t.Fatalf("with a platform failed the evaluation is %q, want scan_failed", ev)
	}
	if g := s.listGrade("library/ubuntu"); g == nil || g.Grade != "N/A" || g.Evaluation != "scan_failed" {
		t.Fatalf("the package list grades the image %+v with a platform failed", g)
	}
}

// R-VULN-015, Q-VULN-269: with onMissingData block a pull of Docker Hub's shape is gated at its platform images. The index, by tag and by
// digest, and an attestation manifest pass by themselves, since neither is an image to scan; a platform image is refused with
// vulnerability_data_missing, told it is not scanned yet, until its detection has finished, and served after; the version view of the tag
// says the same. Integration, about 3 s, bytes.
func TestR_VULN_015_AnIndexAndAnAttestationPassTheGateAndThePlatformImageIsGated(t *testing.T) {
	s := newSelectionWith(t, `{"vulnerabilities":{"onMissingData":"block"}}`)
	images, attestations := s.multiPlatform("library/ubuntu")
	if _, err := s.images.Discover(t.Context(), 0, 100); err != nil {
		t.Fatal(err)
	}
	pull := func(ref, accept string) testenv.Answer {
		t.Helper()
		return s.do("GET", v2("library/ubuntu")+"/manifests/"+ref, nil, "Accept", accept)
	}
	tag := pull("1.0", indexType)
	if tag.Status != http.StatusOK {
		t.Fatalf("the index by tag: %d %s", tag.Status, tag.Body)
	}
	index := tag.Header.Get("Docker-Content-Digest")
	for _, ref := range []string{index, attestations[0], attestations[1]} {
		if a := pull(ref, indexType+","+manifestType); a.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", ref, a.Status, a.Body)
		}
	}
	a := pull(images[0], manifestType)
	if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), `"DENIED"`) ||
		!strings.Contains(string(a.Body), "has not been scanned for vulnerabilities yet") || strings.Contains(string(a.Body), "advisory data") {
		t.Fatalf("a platform image not scanned yet: %d %s %v", a.Status, a.Body, a.Header)
	}
	// The version view says what the pull says: blocked, the image not scanned yet.
	if r := s.versionReport("library/ubuntu", "1.0").Repositories[0]; !r.Blocked || r.MissingData != "image_not_scanned" {
		t.Fatalf("the version view of a tag whose platform images are not scanned: %+v", r)
	}
	s.scanned("library/ubuntu", images[0])
	if a := pull(images[0], manifestType); a.Status != http.StatusOK {
		t.Fatalf("a scanned platform image: %d %s", a.Status, a.Body)
	}
	s.scanned("library/ubuntu", images[1])
	if r := s.versionReport("library/ubuntu", "1.0").Repositories[0]; r.Blocked || r.MissingData != "" {
		t.Fatalf("the version view with both platform images scanned: %+v", r)
	}
}

// listGrade is the grade the package list shows for the name (R-VULN-039), nil when it shows none.
func (s *selection) listGrade(name string) *imageGrade {
	s.t.Helper()
	a := s.do("GET", "/api/v1/packages?format=oci&include=vulnerabilities", nil)
	var page struct {
		Items []struct {
			Name               string      `json:"name"`
			VulnerabilityGrade *imageGrade `json:"vulnerabilityGrade"`
		} `json:"items"`
	}
	if err := json.Unmarshal(a.Body, &page); err != nil || a.Status != http.StatusOK {
		s.t.Fatalf("list: %d %s", a.Status, a.Body)
	}
	for _, it := range page.Items {
		if it.Name == name {
			return it.VulnerabilityGrade
		}
	}
	return nil
}

// R-VULN-034, Q-VULN-270: a terminal failure is due again when what judged it changes, and not after a time: a row of a code of reading
// judged under another release and a row of a code of a bound judged under other bounds are due, the same rows under the current judges
// are not, whatever their attempts; a failure that may pass is due after its back-off below max_attempts. Integration, about 2 s, bytes.
func TestR_VULN_034_ATerminalFailureIsDueWhenWhatJudgedItChanges(t *testing.T) {
	s := newSelection(t)
	images, _ := s.multiPlatform("app")
	if _, err := s.images.Discover(t.Context(), 0, 100); err != nil {
		t.Fatal(err)
	}
	fail := func(digest, code, judged string, attempts int) {
		s.st.Exec(`UPDATE vuln_image_scan SET state = 'failed', error_code = $3, judged = NULLIF($4, ''), attempts = $5, not_before = now() - interval '1 second'
			WHERE repo_id = $1 AND digest = decode($2, 'hex')`, s.repoID, strings.TrimPrefix(digest, "sha256:"), code, judged, attempts)
	}
	const limits = "limits:max_layers=256,max_image_size=3221225472,max_uncompressed_size=34359738368,max_entries=5000000"
	due := func(judges ...string) []string {
		t.Helper()
		rows, err := s.images.Due(t.Context(), dbDigest(1), store.Retry{MaxAttempts: 5, Judges: judges}, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, "sha256:"+hex.EncodeToString(r.Digest[:]))
		}
		slices.Sort(out)
		return out
	}
	both := slices.Sorted(slices.Values(images))

	fail(images[0], "layer_format", "release:0.122.1", 9)
	fail(images[1], "image_too_large", limits, 9)
	if got := due(limits, "release:0.122.1"); len(got) != 0 {
		t.Fatalf("under the judges they were judged under %q are due", got)
	}
	if got := due(limits, "release:0.123.0"); !slices.Equal(got, []string{images[0]}) {
		t.Fatalf("after a release %q are due, want the layer_format row alone", got)
	}
	if got := due("limits:max_layers=256,max_image_size=6442450944,max_uncompressed_size=34359738368,max_entries=5000000", "release:0.122.1"); !slices.Equal(got, []string{images[1]}) {
		t.Fatalf("after the bounds changed %q are due, want the image_too_large row alone", got)
	}

	fail(images[0], "timeout", "", 4)
	fail(images[1], "timeout", "", 5)
	if got := due(limits, "release:0.122.1"); !slices.Equal(got, []string{images[0]}) {
		t.Fatalf("failures that may pass: %q are due, want the one below max_attempts (of %q)", got, both)
	}
}
