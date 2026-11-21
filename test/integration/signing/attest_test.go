//go:build integration

package signing

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	sgbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats/oci"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signing/job"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
	"github.com/guided-traffic/quader/internal/signing/statement"
	"github.com/guided-traffic/quader/test/integration/testenv"

	ocidata "github.com/guided-traffic/quader/internal/formatdata/oci"
)

// The receipt attestations against the stack (signatures.md R-SIG-051 to R-SIG-055): a web replica that stores what the test uploads or
// seeds, and a worker whose job signing attests it with an identity imported in the admin area. The malware scanning is on without a
// scanner, so a verdict stays pending until the test writes one; wait_for_checks is two seconds.

// attestEnv is what every replica of these tests takes besides baseEnv.
func attestEnv() []string {
	return []string{"QUADER_SIGNING_ATTESTATIONS_WAIT_FOR_CHECKS=2s", "QUADER_MALWARE_ENABLED=true", "QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off"}
}

// attestFixture is the stack with a generic, a gated local OCI and a local npm repository, created before the replicas start, and a database handle of the
// test for the data port's rows and the malware stand-in.
type attestFixture struct {
	*env
	db    *postgres.DB
	oci   *ocidata.Store
	leaf  string // the identity's URI
	trust *root.TrustedRoot
}

func newAttestFixture(t *testing.T, name string) *attestFixture {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: name})
	st.Repository("files", "local", "generic", "{}")
	// docker-local serves only verified artifacts: the gate's readers must never count Quader's receipt as a publisher's signature.
	st.Repository("docker-local", "local", "oci", `{"signatures":{"onlyVerified":true}}`)
	st.Repository("npm-local", "local", "npm", "{}") // its client setup's tab verify
	rita := st.User("rita")
	st.Bind("admin", rita)
	e := &env{t: t, st: st, admin: st.Token(rita, scope.Scope{Inherit: true, Admin: true})}
	e.web = st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: append(baseEnv(), attestEnv()...)})
	e.wkr = st.Start(t, testenv.ReplicaOptions{Name: "worker1", Role: "worker", Env: append(baseEnv(), attestEnv()...)})
	loaded, err := config.Load(config.Options{Environ: st.Environ(), Command: config.CommandAdmin})
	if err != nil {
		t.Fatal(err)
	}
	db, err := postgres.Open(context.Background(), loaded.Config.Database, name, meta.RoleWeb, testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	store, err := ocidata.New(ocidata.Options{DB: db, Recorder: audit.Nop{}})
	if err != nil {
		t.Fatal(err)
	}
	f := &attestFixture{env: e, db: db, oci: store}
	ca := signingtest.NewCA(t, "Example Corp")
	id, _ := e.importIdentity(ca)
	if id.State != "current" {
		t.Fatalf("the installation's first identity is not current at once: %+v", id)
	}
	f.leaf = "https://quader.example.com/import"
	f.trust, err = root.NewTrustedRootFromJSON(e.call("GET", "/api/v1/signing/trusted-root.json", "", nil, http.StatusOK).Body)
	if err != nil {
		t.Fatalf("sigstore-go does not take the trusted root Quader serves: %v", err)
	}
	return f
}

// attestation is the bundle the API serves for a file, or nil while there is none.
func (f *attestFixture) attestation(repo, path string) []byte {
	f.t.Helper()
	a := f.st.Do(f.web, "GET", "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+repo+"/attestation?path="+url.QueryEscape(path), f.admin, nil)
	switch a.Status {
	case http.StatusOK:
		return a.Body
	case http.StatusNotFound:
		return nil
	}
	f.t.Fatalf("GET attestation of %s: %d %s", path, a.Status, a.Body)
	return nil
}

// await polls the API until the file's attestation satisfies cond.
func (f *attestFixture) await(what, repo, path string, cond func(raw []byte, pred receipt) bool) ([]byte, receipt) {
	f.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		raw := f.attestation(repo, path)
		var p receipt
		if raw != nil {
			p = predicateOf(f.t, raw)
		}
		if cond(raw, p) {
			return raw, p
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("%s: not reached; the attestation is %s", what, raw)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// receipt is what the tests read of the predicate.
type receipt struct {
	Malware struct {
		State    string `json:"state"`
		Database string `json:"database"`
	} `json:"malware"`
	Origin struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Artifact struct {
		Name      string `json:"name"`
		MediaType string `json:"mediaType"`
	} `json:"artifact"`
	IssuedAt string `json:"issuedAt"`
}

func predicateOf(t *testing.T, raw []byte) receipt {
	t.Helper()
	var b struct {
		DSSE struct {
			Payload string `json:"payload"`
		} `json:"dsseEnvelope"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("the bundle: %v", err)
	}
	payload, err := base64.StdEncoding.DecodeString(b.DSSE.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		PredicateType string  `json:"predicateType"`
		Predicate     receipt `json:"predicate"`
	}
	if err := json.Unmarshal(payload, &st); err != nil || st.PredicateType != statement.PredicateType {
		t.Fatalf("the statement %s: %v", payload, err)
	}
	return st.Predicate
}

// verifies checks a bundle as cosign verify-blob-attestation does offline (R-SIG-052): sigstore-go's verifier against the trusted root the
// API served, at the current time, without a log or a timestamp, for the identity's URI and the artifact's digest.
func (f *attestFixture) verifies(raw []byte, digest [32]byte) error {
	f.t.Helper()
	var b sgbundle.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	v, err := verify.NewVerifier(f.trust, verify.WithCurrentTime())
	if err != nil {
		return err
	}
	id, err := verify.NewShortCertificateIdentity("", ".*", f.leaf, "")
	if err != nil {
		return err
	}
	_, err = v.Verify(&b, verify.NewPolicy(verify.WithArtifactDigest("sha256", digest[:]), verify.WithCertificateIdentity(id)))
	return err
}

// verdict writes a blob's malware verdict as the malware scanning's store does, with its word to the job signing in the transaction
// (malware/store.Options.VerdictChanged, the statement SigningWakeVerdict, and the job's wake-up).
func (f *attestFixture) verdict(digest [32]byte, state, database string) {
	f.t.Helper()
	ctx := context.Background()
	if err := f.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, _ := postgres.Unwrap(tx)
		if _, err := ptx.Exec(ctx, `UPDATE malware_scan SET state = $2, db_version = $3, db_built_at = now(), scanned_at = now(), due = false,
			signature = CASE WHEN $2 = 'infected' THEN 'Eicar-Test-Signature' END, first_found_at = CASE WHEN $2 = 'infected' THEN now() END
			WHERE sha256 = $1`, digest[:], state, database); err != nil {
			return err
		}
		if _, err := q.New(ptx).SigningWakeVerdict(ctx, digest[:]); err != nil {
			return err
		}
		return job.Wake(ctx, tx)
	}); err != nil {
		f.t.Fatal(err)
	}
}

// TestR_SIG_053_AFilesAttestationWaitsForItsVerdictAndFollowsTheMalwareState: a generic file's attestation waits for its malware verdict
// and, at the bound of wait_for_checks, is made with pending; the verdict clean makes it again with clean and the database; a second
// rescan with the same verdict changes nothing; the bundle verifies against the trusted root the API serves for the identity's URI; the
// client setup shows the tab verify with the cosign command, and npm's client npm audit signatures before it; deleting the file removes
// the attestation. Integration, about 6 s, a file of 16 bytes.
func TestR_SIG_053_AFilesAttestationWaitsForItsVerdictAndFollowsTheMalwareState(t *testing.T) {
	f := newAttestFixture(t, "attestfile")
	body := []byte("a file of bytes.")
	digest := sha256.Sum256(body)
	path := "/files/" + testenv.DefaultProject + "/files/tools/a.bin"
	if a := f.st.Do(f.web, "PUT", path, f.admin, body); a.Status/100 != 2 {
		t.Fatalf("PUT: %d %s", a.Status, a.Body)
	}
	raw, p := f.await("the attestation at the bound of wait_for_checks", "files", "tools/a.bin", func(raw []byte, p receipt) bool { return raw != nil })
	if p.Malware.State != statement.MalwarePending || p.Origin.Kind != statement.OriginUpload || p.Artifact.Name != "tools/a.bin" {
		t.Fatalf("the first attestation: %+v", p)
	}
	if err := f.verifies(raw, digest); err != nil {
		t.Fatalf("the bundle does not verify against the trusted root Quader serves: %v", err)
	}
	if err := f.verifies(raw, sha256.Sum256([]byte("other"))); err == nil {
		t.Fatal("the bundle verifies for other bytes")
	}

	f.verdict(digest, "clean", "main-62.daily-27450")
	_, p = f.await("the attestation is made again with the verdict", "files", "tools/a.bin", func(_ []byte, p receipt) bool { return p.Malware.State == "clean" })
	if p.Malware.Database != "main-62.daily-27450" {
		t.Errorf("the verdict's database: %+v", p.Malware)
	}
	issued := p.IssuedAt
	f.verdict(digest, "clean", "main-62.daily-27451")
	time.Sleep(time.Second)
	if _, again := f.await("the rescan settled", "files", "tools/a.bin", func(_ []byte, p receipt) bool { return true }); again.IssuedAt != issued {
		t.Errorf("a rescan that kept the state made the attestation again: %s, then %s", issued, again.IssuedAt)
	}

	setup := f.call("GET", "/api/v1/projects/"+testenv.DefaultProject+"/repositories/files/client-setup?client=curl&credentials=none", f.admin, nil, http.StatusOK)
	var cs struct {
		Snippets []struct {
			ID, Tab, Content string
		} `json:"snippets"`
	}
	unmarshal(t, setup, &cs)
	found := false
	for _, s := range cs.Snippets {
		if s.Tab == "verify" {
			found = s.ID == "curl.verify.attestation" && strings.Contains(s.Content, "cosign verify-blob-attestation") && strings.Contains(s.Content, f.leaf)
		}
	}
	if !found {
		t.Errorf("the client setup has no tab verify with the identity: %s", setup.Body)
	}
	// npm's client checks the registry signatures with its own command first (Q-SIG-163).
	setup = f.call("GET", "/api/v1/projects/"+testenv.DefaultProject+"/repositories/npm-local/client-setup?client=npm&credentials=none", f.admin, nil, http.StatusOK)
	cs.Snippets = nil
	unmarshal(t, setup, &cs)
	var verify []string
	for _, s := range cs.Snippets {
		if s.Tab == "verify" {
			verify = append(verify, s.ID)
		}
	}
	if strings.Join(verify, " ") != "npm.verify.signatures npm.verify.attestation" {
		t.Errorf("npm-local's tab verify: %v", verify)
	}

	if a := f.st.Do(f.web, "DELETE", path, f.admin, nil); a.Status/100 != 2 {
		t.Fatalf("DELETE: %d %s", a.Status, a.Body)
	}
	if f.attestation("files", "tools/a.bin") != nil || f.st.Number(`SELECT count(*) FROM signing_attestations`) != 0 {
		t.Error("the attestation stayed with its file deleted")
	}
}

// gateIgnoresTheReceipt checks the readers of the signature gate against an image whose only signature is Quader's receipt, standing
// as its referrer in the gated docker-local (R-SIG-062, R-SIG-072, R-SIG-073): the preview counts the image as its one artifact (the
// referrer, its config and the bundle are none) and refuses it as missing; the file's answer lists the receipt apart, as an attestation
// with issuedBy and without a badge, and its member gate refuses the image as missing with nothing that passed it.
func (f *attestFixture) gateIgnoresTheReceipt(img [32]byte) {
	f.t.Helper()
	base := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/docker-local"
	var pv struct {
		Artifacts, Pass, Pending int
		Refused                  map[string]int
	}
	unmarshal(f.t, f.call("GET", base+"/signatures/preview?anchors=", f.admin, nil, http.StatusOK), &pv)
	if pv.Artifacts != 1 || pv.Pass != 0 || pv.Pending != 0 || pv.Refused["missing"] != 1 {
		f.t.Errorf("the preview counted Quader's receipt: %+v", pv)
	}
	var rep struct {
		State        string `json:"state"`
		Signatures   []json.RawMessage
		Attestations []struct {
			Role     string          `json:"role"`
			IssuedBy json.RawMessage `json:"issuedBy"`
		}
		Gate *struct {
			Decision string          `json:"decision"`
			Reason   *string         `json:"reason"`
			PassedBy json.RawMessage `json:"passedBy"`
		} `json:"gate"`
	}
	unmarshal(f.t, f.call("GET", base+"/signatures?path="+url.QueryEscape("app/_manifests/"+dg(img)), f.admin, nil, http.StatusOK), &rep)
	if rep.State != "none" || len(rep.Signatures) != 0 || len(rep.Attestations) != 1 || rep.Attestations[0].Role != "attestation" ||
		len(rep.Attestations[0].IssuedBy) == 0 || string(rep.Attestations[0].IssuedBy) == "null" {
		f.t.Errorf("the file's answer shows Quader's receipt as a publisher's signature: state %q, %d signatures, attestations %+v",
			rep.State, len(rep.Signatures), rep.Attestations)
	}
	if g := rep.Gate; g == nil || g.Decision != "refused" || g.Reason == nil || *g.Reason != "missing" || (len(g.PassedBy) != 0 && string(g.PassedBy) != "null") {
		f.t.Errorf("the member gate passed the image on Quader's receipt: %+v", g)
	}
}

// seedOCI commits a file of the local OCI repository as a push stores it (oci.SeededItem; the rows are the test's) and returns its digest.
func (f *attestFixture) seedOCI(path, body, mediaType string) [32]byte {
	f.t.Helper()
	ctx := context.Background()
	rp, err := f.web.App.Repositories().ByName(ctx, qname.Repo{Project: testenv.DefaultProject, Key: "docker-local"})
	if err != nil {
		f.t.Fatal(err)
	}
	sp, err := f.web.App.Items().Spool(ctx, strings.NewReader(body), items.IngestOptions{ContentLength: int64(len(body))})
	if err != nil {
		f.t.Fatal(err)
	}
	defer sp.Close()
	if _, err := f.web.App.Items().Commit(ctx, items.CommitSpec{
		Repo: rp, Path: repository.Cleaned{Path: path}, Spooled: sp, MediaType: mediaType, Origin: items.OriginUpload, MayOverwrite: true,
		Payload: oci.SeededItem{}, Actor: audit.Actor{Kind: "user", Name: "rita"},
	}); err != nil {
		f.t.Fatalf("commit %s: %v", path, err)
	}
	return sha256.Sum256([]byte(body))
}

func dg(d [32]byte) string { return "sha256:" + hex.EncodeToString(d[:]) }

// TestR_SIG_054_AnImagesAttestationIsAReferrerUntilAnOlderSignatureTagArrives: an image of a local OCI repository gets Quader's
// attestation as a referrer of the form cosign 3 reads (artifact type and layer of a Sigstore bundle, the subject the image, the
// predicate type's annotation), a carrier the gate passes (oci_manifests.attachment) and no version; the repository's gate and its
// readers (the preview, the file's answer and its member gate) never count the receipt as a publisher's signature; when a tag
// sha256-<hex>.sig arrives for the digest, the referrer goes and the API serves the bundle. Integration, about 5 s, bytes.
func TestR_SIG_054_AnImagesAttestationIsAReferrerUntilAnOlderSignatureTagArrives(t *testing.T) {
	f := newAttestFixture(t, "attestoci")
	const manifestType = "application/vnd.oci.image.manifest.v1+json"
	repoID := f.st.Number(`SELECT r.id FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND r.key = 'docker-local'`, testenv.DefaultProject)
	cfg := f.seedOCI("app/_blobs/"+dg(sha256.Sum256([]byte(`{"architecture":"amd64"}`))), `{"architecture":"amd64"}`, "application/vnd.oci.image.config.v1+json")
	body := `{"schemaVersion":2,"mediaType":"` + manifestType + `","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + dg(cfg) + `","size":24},"layers":[]}`
	img := sha256.Sum256([]byte(body))
	f.seedOCI("app/_manifests/"+dg(img), body, manifestType)
	if err := f.db.InTx(context.Background(), func(tx meta.Tx) error {
		return f.oci.PutManifest(context.Background(), tx, rows.ManifestInput{
			Manifest:   rows.Manifest{RepoID: repoID, Name: "app", Digest: img, MediaType: manifestType, Size: int64(len(body))},
			References: []rows.Reference{{Target: cfg, Kind: rows.KindBlob}},
		})
	}); err != nil {
		t.Fatal(err)
	}
	f.verdict(img, "clean", "main-62")
	f.verdict(cfg, "clean", "main-62")

	deadline := time.Now().Add(20 * time.Second)
	for f.st.Text(`SELECT COALESCE((SELECT served_as FROM signing_attestations sa JOIN items i ON i.id = sa.item_id WHERE i.blob_sha256 = $1), '')`, img[:]) != "referrer" {
		if time.Now().After(deadline) {
			t.Fatal("the image's attestation does not stand as a referrer")
		}
		time.Sleep(200 * time.Millisecond)
	}
	row := f.st.Text(`SELECT artifact_type || '|' || (annotations->>'dev.sigstore.bundle.predicateType') || '|' || attachment::text FROM oci_manifests
		WHERE repo_id = $1 AND name = 'app' AND subject = $2`, repoID, img[:])
	parts := strings.Split(row, "|")
	if len(parts) != 3 {
		t.Fatalf("the referrer row: %q", row)
	}
	artifactType, predicate, attachment := parts[0], parts[1], parts[2] == "true"
	if artifactType != "application/vnd.dev.sigstore.bundle.v0.3+json" || predicate != statement.PredicateType || !attachment {
		t.Fatalf("the referrer row: %q %q attachment %v", artifactType, predicate, attachment)
	}
	if n := f.st.Number(`SELECT count(*) FROM package_versions pv JOIN packages p ON p.id = pv.package_id WHERE p.repo_id = $1`, repoID); n != 0 {
		t.Errorf("the referrer made %d versions", n)
	}
	if raw := f.attestation("docker-local", "app/_manifests/"+dg(img)); raw == nil || f.verifies(raw, img) != nil {
		t.Errorf("the image's bundle is not served or does not verify")
	}
	f.gateIgnoresTheReceipt(img)

	sigBody := `{"schemaVersion":2,"mediaType":"` + manifestType + `","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + dg(cfg) + `","size":24},"layers":[{"mediaType":"application/vnd.dev.cosign.simplesigning.v1+json","digest":"` + dg(cfg) + `","size":24}]}`
	f.seedOCI("app/_tags/sha256-"+hex.EncodeToString(img[:])+".sig", sigBody, manifestType)
	deadline = time.Now().Add(20 * time.Second)
	for f.st.Number(`SELECT count(*) FROM oci_manifests WHERE repo_id = $1 AND subject = $2 AND artifact_type = 'application/vnd.dev.sigstore.bundle.v0.3+json'`, repoID, img[:]) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("Quader's referrer stayed beside an older cosign signature tag")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if reason := f.st.Text(`SELECT COALESCE(api_reason, '') FROM signing_attestations sa JOIN items i ON i.id = sa.item_id WHERE i.blob_sha256 = $1`, img[:]); reason != "hides_signature" {
		t.Errorf("the reason the attestation is served through the API: %q", reason)
	}
	if raw := f.attestation("docker-local", "app/_manifests/"+dg(img)); raw == nil {
		t.Error("the API does not serve the image's bundle once its referrer went")
	}
}
