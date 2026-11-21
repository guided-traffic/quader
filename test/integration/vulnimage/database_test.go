//go:build integration

package vulnimage

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/ocistub"
)

// The database's repository (R-VULN-044, R-VULN-045, Q-VULN-171 to Q-VULN-175): the task vuln-sync-trivy-db resolves the tag of the
// database's image through the repository the setting names, makes it hold the layer, and records the digest as the database in use; the
// loader of the scanner pod reads the layer through the scan listener (R-VULN-049), and a worker only looks up that the repository holds
// it. The archive is one of bytes (dbArchive) and the analyser is the fake one of the flow; no test reaches a public registry.

const (
	dbImage      = "aquasec/trivy-db"
	dbConfigType = "application/vnd.aquasec.trivy.config.v1+json"
)

// source is the row of the database's source.
func (f *flow) source(column string) string {
	f.t.Helper()
	return f.st.Text(`SELECT COALESCE((SELECT (` + column + `)::text FROM vuln_source_state WHERE source = 'trivy' AND ecosystem = 'OCI'), '')`)
}

// runDatabase starts the task and waits until its attempt changed the row: the run ended, recorded or failed.
func (f *flow) runDatabase() {
	f.t.Helper()
	before := f.source("concat_ws(' ', attempted_at, synced_at, failures)")
	if a := f.st.Do(f.api, "POST", "/api/v1/system/tasks/vuln-sync-trivy-db/run", f.adm, nil); a.Status != http.StatusAccepted && a.Status != http.StatusConflict {
		f.t.Fatalf("start the database's task: %d %s", a.Status, a.Body)
	}
	testenv.WaitFor(f.t, "the database's run to end", 60*time.Second, func() bool {
		now := f.source("concat_ws(' ', attempted_at, synced_at, failures)")
		return now != before && f.st.Number(`SELECT count(*) FROM job WHERE kind = 'vuln-sync-trivy-db' AND state IN ('queued', 'running')`) == 0
	})
	f.t.Cleanup(func() {
		if f.t.Failed() {
			f.t.Logf("source: %s", f.source("concat_ws(' ', state, etag, failures, last_error::text)"))
		}
	})
}

// pushDatabase pushes the archive as the database's artifact under aquasec/trivy-db:2 into a local OCI repository, as `oras push`
// does: the config, the layer of the database's media type and the manifest, through the registry protocol. It returns the layer's
// digest.
func (f *flow) pushDatabase(repo string, archive []byte) string {
	f.t.Helper()
	cfg := []byte("{}")
	base := "/v2/" + testenv.DefaultProject + "/" + repo + "/" + dbImage
	for _, b := range [][]byte{cfg, archive} {
		if a := f.do("POST", base+"/blobs/uploads/?digest="+digestOf(b), b, "Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
			f.t.Fatalf("blob upload: %d %s", a.Status, a.Body)
		}
	}
	m, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": dbConfigType, "digest": digestOf(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": imagescan.LayerMediaType, "digest": digestOf(archive), "size": len(archive)}},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if a := f.do("PUT", base+"/manifests/"+imagescan.DatabaseTag, m, "Content-Type", manifestType); a.Status != http.StatusCreated {
		f.t.Fatalf("manifest push: %d %s", a.Status, a.Body)
	}
	return digestOf(archive)
}

// databaseItem is the row of the sources API of the database (R-VULN-045).
type databaseItem struct {
	Mode, Repository, Reference string
	RepositoryClass             *string `json:"repositoryClass"`
	Failures                    int
	LastError                   *struct{ Code, Detail string } `json:"lastError"`
}

func (f *flow) databaseItem() databaseItem {
	f.t.Helper()
	a := f.do("GET", "/api/v1/vulnerabilities/sources", nil)
	if a.Status != http.StatusOK {
		f.t.Fatalf("sources: %d %s", a.Status, a.Body)
	}
	var list struct {
		Items []struct {
			Source string
			databaseItem
		}
	}
	if err := json.Unmarshal(a.Body, &list); err != nil {
		f.t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.Source == "trivy" {
			return it.databaseItem
		}
	}
	f.t.Fatalf("no item of the image scanning's database in %s", a.Body)
	return databaseItem{}
}

// R-VULN-044 acceptance (local): with a local OCI repository named in the setting and the artifact pushed through the registry protocol,
// the task makes the source complete with the layer's digest, the loader of the scanner pod loads it from the repository through the scan
// listener (R-VULN-049), and an image reaches evaluated; a second run records nothing new; with the tag deleted, the run fails
// database_missing and the sources API shows it (R-VULN-045).
func TestR_VULN_044_ALocalRepositoryHoldsTheDatabaseAndAWorkerReadsItFromThere(t *testing.T) {
	const dbRepo = "scanner-db"
	f := newFlowWith(t, options{
		realDatabase: true,
		seed: func(st *testenv.Stack) {
			st.Repository(dbRepo, "local", "oci", `{"vulnerabilities":{"enabled":false}}`)
		},
		stackEnv: []string{"QUADER_VULNERABILITIES_IMAGES_DATABASE_REPOSITORY=" + testenv.DefaultProject + "/" + dbRepo},
	})
	layer := f.pushDatabase(dbRepo, dbArchive(t))
	f.push("app", "1.0", newImage(t))
	f.runDatabase()
	if f.source("state") != "complete" || f.source("etag") != layer {
		t.Fatalf("the database in use is %q (%s), want the pushed layer %s", f.source("etag"), f.source("state"), layer)
	}
	if got := f.source("encode(archive_sha256, 'hex')"); "sha256:"+got != layer {
		t.Fatalf("archive_sha256 %s", got)
	}
	// Q-VULN-208: the run records the build time of the layer's metadata.json, which the scan listener's listing names.
	if got := f.source("watermark"); !strings.HasPrefix(got, "2026-11-08 12:51:20") {
		t.Fatalf("the database's build time %q", got)
	}
	f.waitLoaded(layer)
	f.runScan()
	r := f.waitEvaluation("app", "1.0", "evaluated")
	if len(r.Repositories[0].Findings) != 1 {
		t.Fatalf("findings: %+v", r.Repositories[0].Findings)
	}

	epoch := f.source("data_epoch")
	f.runDatabase()
	if f.source("data_epoch") != epoch || f.source("failures") != "0" {
		t.Fatalf("a second run of the same layer changed the data: epoch %s then %s, failures %s", epoch, f.source("data_epoch"), f.source("failures"))
	}
	// A row the upgrade left (Q-VULN-191: the digest without its SHA-256, which the trim's exemption reads) is recorded again, as the
	// same database.
	f.st.Exec(`UPDATE vuln_source_state SET archive_sha256 = NULL WHERE source = 'trivy' AND ecosystem = 'OCI'`)
	f.runDatabase()
	if "sha256:"+f.source("encode(archive_sha256, 'hex')") != layer || f.source("data_epoch") != epoch {
		t.Fatalf("the upgraded row: archive_sha256 %s, epoch %s then %s", f.source("encode(archive_sha256, 'hex')"), epoch, f.source("data_epoch"))
	}

	// A database in use whose layer the repository does not hold (a holder of delete removed it): the scan acts as before the first
	// database and wakes the task at once, which records the repository's layer again (R-VULN-044).
	f.st.Exec(`UPDATE vuln_source_state SET etag = $1 WHERE source = 'trivy' AND ecosystem = 'OCI'`, "sha256:"+strings.Repeat("0", 64))
	f.runScan()
	testenv.WaitFor(t, "the task the scan woke to record the layer again", 60*time.Second, func() bool { return f.source("etag") == layer })

	if a := f.do("DELETE", "/v2/"+testenv.DefaultProject+"/"+dbRepo+"/"+dbImage+"/manifests/"+imagescan.DatabaseTag, nil); a.Status != http.StatusAccepted {
		t.Fatalf("delete the tag: %d %s", a.Status, a.Body)
	}
	f.runDatabase()
	it := f.databaseItem()
	if it.Mode != "repository" || it.Repository != testenv.DefaultProject+"/"+dbRepo || it.Reference != dbImage+":"+imagescan.DatabaseTag ||
		it.RepositoryClass == nil || *it.RepositoryClass != "local" {
		t.Errorf("the database's row: %+v", it)
	}
	if it.Failures != 1 || it.LastError == nil || it.LastError.Code != "database_missing" || !strings.Contains(it.LastError.Detail, dbRepo) {
		t.Fatalf("the failure: %+v %+v", it, it.LastError)
	}
	if f.source("etag") != layer {
		t.Errorf("the failed run replaced the database in use: %s", f.source("etag"))
	}
}

// databaseStub is a registry that serves the database's artifact and nothing else, on the loopback names of test/ocistub, with no token
// realm and no blob redirect. It returns the stub, its address, the stack's environment that trusts and admits it, and the layer.
func databaseStub(t *testing.T, archive []byte) (*ocistub.Server, *ocistub.Running, []string, string) {
	t.Helper()
	ss, err := ocistub.NewSelfSigned(ocistub.LoopbackHosts...)
	if err != nil {
		t.Fatal(err)
	}
	stub, err := ocistub.New(ocistub.Config{Corpus: ocistub.Empty()})
	if err != nil {
		t.Fatal(err)
	}
	if err := stub.SetMode(ocistub.Mode{Auth: ocistub.AuthNone, BlobsDirect: true}); err != nil {
		t.Fatal(err)
	}
	_, layer := stub.PutArtifact(dbImage, imagescan.DatabaseTag, dbConfigType, []byte("{}"), imagescan.LayerMediaType, archive)
	run, err := stub.Start("127.0.0.1:0", ss.ServerConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = run.Close() })
	ca := testenv.WriteFile(t, t.TempDir(), "stub-ca.pem", string(ss.CAPEM))
	return stub, run, []string{
		"QUADER_PROXY_OUTBOUND_TLS_CA_FILES=[" + strconv.Quote(filepath.ToSlash(ca)) + "]", `QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.1/32"]`,
	}, layer
}

// remoteConfig is the configuration of a remote OCI repository of the stub, its scanning off.
func remoteConfig(t *testing.T, url string) string {
	t.Helper()
	doc, err := json.Marshal(map[string]any{"vulnerabilities": map[string]any{"enabled": false}, "remote": map[string]any{
		"url": url, "extraHosts": []any{}, "authMode": "none", "username": "",
		"password": nil, "clientCertificate": nil, "headers": []any{}, "forwardAuthOnRedirect": false, "outboundProxy": map[string]any{"mode": "direct"},
		"connectTimeoutMs": 5000, "readTimeoutMs": 15000, "sourceNetwork": nil, "cacheFillAction": "read",
		"cache": map[string]any{
			"storeArtifacts": true, "metadataTtlSeconds": 300, "metadataRefreshTimeoutSeconds": 10, "notFoundTtlSeconds": 60,
			"maxStaleSeconds": 0, "unusedRetentionHours": 0,
		},
		"integrity":    map[string]any{"checksumPolicy": "verify-if-available", "blockUnexpectedContentTypes": true, "allowedContentTypes": []any{}},
		"availability": map[string]any{"mode": "online", "failureThreshold": 3, "circuitOpenSeconds": 30},
		"limits":       map[string]any{"maxArtifactBytes": nil, "maxMetadataBytes": nil, "maxConcurrentFetches": 16},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(doc)
}

// R-VULN-044 acceptance (remote), Q-VULN-172, Q-VULN-175: with a remote OCI repository in the setting, the first run stores the layer in
// the repository, with one upstream request for the tag and one for the blob, recorded under the subject system and filled by
// system:image-database; the layer in use is what the cleanup's exemption keeps; a second run sends one revalidation of the tag and no
// blob request; with the repository disabled, the run fails database_repository_off.
func TestR_VULN_044_ARemoteRepositoryStoresTheLayerOnceAndRevalidatesTheTag(t *testing.T) {
	const dbRepo = "trivy-remote"
	stub, run, env, layer := databaseStub(t, dbArchive(t))
	f := newFlowWith(t, options{
		realDatabase: true, workerDial: run.DialOverride(),
		seed:     func(st *testenv.Stack) { st.Repository(dbRepo, "remote", "oci", remoteConfig(t, run.URL)) },
		stackEnv: append(env, "QUADER_VULNERABILITIES_IMAGES_DATABASE_REPOSITORY="+testenv.DefaultProject+"/"+dbRepo),
	})
	blobPath := "/v2/" + dbImage + "/blobs/" + layer
	f.runDatabase()
	if f.source("state") != "complete" || f.source("etag") != layer {
		t.Fatalf("the database in use is %q (%s), want the stub's layer %s; stub: %s", f.source("etag"), f.source("state"), layer, stub.Counts().Summary())
	}
	if m, b := stub.Counts().ManifestGets(dbImage, imagescan.DatabaseTag), stub.Counts().Requests("GET", blobPath); m != 1 || b != 1 {
		t.Fatalf("the first run made %d manifest and %d blob requests, want one each: %s", m, b, stub.Counts().Summary())
	}
	item := dbImage + "/_blobs/" + layer
	if n := f.st.Number(`SELECT count(*) FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = $1 AND i.path = $2 AND i.trashed_at IS NULL`, dbRepo, item); n != 1 {
		t.Fatalf("the repository holds %d items %s", n, item)
	}
	if n := f.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'authz.write.allowed' AND reason = 'image-database' AND actor_type = 'system' AND repository = $1`, testenv.DefaultProject+"/"+dbRepo); n < 1 {
		t.Errorf("no populate event of system:image-database: %s", f.st.Text(`SELECT COALESCE(string_agg(concat_ws(' ', action, reason, actor_type, actor_name, repository), '; '), 'none') FROM audit_event WHERE action LIKE 'authz.%'`))
	}
	// Q-VULN-175: the exemption the remote-cache cleanup applies holds for the layer in use and for no other content.
	if f.st.Text(`SELECT vuln_image_database_in_use(i.blob_sha256)::text FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = $1 AND i.path = $2`, dbRepo, item) != "true" ||
		f.st.Text(`SELECT vuln_image_database_in_use(sha256('other'::bytea))::text`) != "false" {
		t.Error("the cleanup's exemption does not single out the layer in use")
	}
	repoID := f.st.Number(`SELECT id FROM repositories WHERE key = $1`, dbRepo)
	testenv.WaitFor(t, "the run's exchanges in the usage store under system", 30*time.Second, func() bool {
		return f.st.Number(`SELECT COALESCE(sum(upstream_requests), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'system' AND repository_id = $1`, repoID) >= 2
	})

	// The format revalidates a tag's document by a HEAD of the manifest, which names its digest (formats/oci.md R-OCI-022).
	tagPath := "/v2/" + dbImage + "/manifests/" + imagescan.DatabaseTag
	tagRequests := func(c ocistub.Counts) int { return c.Requests("GET", tagPath) + c.Requests("HEAD", tagPath) }
	before := stub.Counts()
	f.runDatabase()
	after := stub.Counts()
	if m, b := tagRequests(after)-tagRequests(before), after.Requests("GET", blobPath)-before.Requests("GET", blobPath); m != 1 || b != 0 {
		t.Fatalf("the second run made %d requests of the tag and %d of the blob, want one revalidation and none: %s", m, b, after.Summary())
	}

	// Q-VULN-194: a repository of the installation's own, here a remote without a cache period, is its operator's to keep: a new build and
	// two days later the first layer is still there.
	_, next := stub.PutArtifact(dbImage, imagescan.DatabaseTag, dbConfigType, []byte("{}"), imagescan.LayerMediaType, dbArchiveOf(t, "the next build"))
	f.runDatabase()
	f.st.Exec(`UPDATE items SET created_at = now() - interval '3 days' WHERE repo_id = (SELECT id FROM repositories WHERE key = $1)`, dbRepo)
	f.runDatabase()
	if f.source("etag") != next || f.st.Number(`SELECT count(*) FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = $1 AND i.path = $2`, dbRepo, item) != 1 {
		t.Fatalf("an own repository dropped the first layer, or did not record the next (%s)", f.source("etag"))
	}

	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + dbRepo
	got := f.do("GET", path, nil)
	if a := f.st.Do(f.api, "PATCH", path, f.adm, []byte(`{"disabled":true}`), "If-Match", got.Header.Get("ETag"), "Content-Type", "application/merge-patch+json"); a.Status != http.StatusOK {
		t.Fatalf("disable: %d %s", a.Status, a.Body)
	}
	// The worker's repository reader follows the change at its next epoch: the task is run until it reads the repository as disabled.
	testenv.WaitFor(t, "a run to fail database_repository_off", 60*time.Second, func() bool {
		f.runDatabase()
		return f.source("last_error->>'code'") == "database_repository_off"
	})
	if f.source("etag") != next {
		t.Errorf("the failed run replaced the database in use: %s", f.source("etag"))
	}
}

// R-VULN-044 acceptance (the well-known repository), R-REPO-070: with the image scanning on and no setting, the task resolves
// aquasec/trivy-db:2 through upstream/scanner-trivy, whose address an administrator pointed at the stub; the system's in-process read
// reaches a repository that answers every protocol request 404.
func TestR_VULN_044_TheDefaultReadsThroughUpstreamScannerTrivy(t *testing.T) {
	stub, run, env, layer := databaseStub(t, dbArchive(t))
	f := newFlowWith(t, options{realDatabase: true, workerDial: run.DialOverride(), stackEnv: env, seed: func(st *testenv.Stack) {
		// The project upstream and its repositories are the reconcile's of `quader migrate` (R-REPO-067); the address is an
		// administrator's (R-REPO-069), written here before the replicas start.
		if err := st.Declare(); err != nil {
			t.Fatal(err)
		}
		st.Exec(`UPDATE repositories SET config = jsonb_set(config, '{remote,url}', to_jsonb($1::text))
			WHERE key = 'scanner-trivy' AND project_id = (SELECT id FROM project WHERE key = 'upstream')`, run.URL)
	}})
	if a := f.do("GET", "/v2/upstream/scanner-trivy/"+dbImage+"/manifests/"+imagescan.DatabaseTag, nil, "Accept", manifestType); a.Status != http.StatusNotFound {
		t.Fatalf("a client's pull of upstream/scanner-trivy: %d, want 404", a.Status)
	}
	f.runDatabase()
	if f.source("state") != "complete" || f.source("etag") != layer {
		t.Fatalf("the database in use is %q (%s), want the stub's layer %s; stub: %s", f.source("etag"), f.source("state"), layer, stub.Counts().Summary())
	}
	if n := f.st.Number(`SELECT count(*) FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = 'scanner-trivy' AND i.path = $1`, dbImage+"/_blobs/"+layer); n != 1 {
		t.Fatalf("upstream/scanner-trivy holds %d items of the layer", n)
	}
	it := f.databaseItem()
	if it.Repository != "upstream/scanner-trivy" || it.RepositoryClass == nil || *it.RepositoryClass != "remote" || it.Failures != 0 {
		t.Errorf("the database's row: %+v", it)
	}

	// Q-VULN-175, Q-VULN-194: upstream/scanner-trivy keeps the layer in use and lets the older ones go 48 hours after they were stored,
	// after a run that succeeded and only then. The test moves the files' time back instead of waiting two days.
	const repo = `(SELECT id FROM repositories WHERE key = 'scanner-trivy')`
	has := func(path string) bool {
		return f.st.Number(`SELECT count(*) FROM items WHERE repo_id = `+repo+` AND path = $1 AND kind = 'file'`, path) == 1
	}
	files := func() string {
		return f.st.Text(`SELECT COALESCE(string_agg(path, ' ' ORDER BY path), '') FROM items WHERE repo_id = ` + repo + ` AND kind = 'file'`)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("files of upstream/scanner-trivy: %s", files())
		}
	})
	first := dbImage + "/_blobs/" + layer
	firstManifests := strings.Fields(f.st.Text(`SELECT COALESCE(string_agg(path, ' '), '') FROM items WHERE repo_id = ` + repo + ` AND path LIKE '%/\_manifests/%'`))

	// The publisher builds a new database: the run records it, and the first layer, minutes old, stays.
	_, second := stub.PutArtifact(dbImage, imagescan.DatabaseTag, dbConfigType, []byte("{}"), imagescan.LayerMediaType, dbArchiveOf(t, "the next build"))
	f.runDatabase()
	if f.source("etag") != second || !has(first) {
		t.Fatalf("after the second build: in use %s, the first layer held %v; %s", f.source("etag"), has(first), files())
	}
	f.st.Exec(`UPDATE items SET created_at = now() - interval '3 days' WHERE repo_id = ` + repo)

	// A run that fails removes nothing: the tag names a manifest without a layer of the database.
	stub.PutArtifact(dbImage, imagescan.DatabaseTag, dbConfigType, []byte("{}"), "application/vnd.oci.image.layer.v1.tar", []byte("no database"))
	f.runDatabase()
	if f.source("last_error->>'code'") != "database_missing" || !has(first) || !has(dbImage+"/_blobs/"+second) {
		t.Fatalf("a failed run: %s, the first layer held %v; %s", f.source("last_error->>'code'"), has(first), files())
	}

	// The tag names the database in use again: the run succeeds, the first layer and its manifest go, two days old, and the layer in use
	// stays although it is as old. The removals are the item store's, audited for system:image-database.
	stub.PutArtifact(dbImage, imagescan.DatabaseTag, dbConfigType, []byte("{}"), imagescan.LayerMediaType, dbArchiveOf(t, "the next build"))
	f.runDatabase()
	if f.source("failures") != "0" || has(first) || !has(dbImage+"/_blobs/"+second) {
		t.Fatalf("after the retention: failures %s, the first layer held %v, the layer in use held %v; %s",
			f.source("failures"), has(first), has(dbImage+"/_blobs/"+second), files())
	}
	for _, m := range firstManifests {
		if has(m) {
			t.Errorf("the first build's manifest %s stays; %s", m, files())
		}
	}
	if n := f.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'artifact.deleted' AND actor_type = 'system' AND actor_name = 'image-database'`); n < 1 {
		t.Errorf("no removal audited for system:image-database: %s", f.st.Text(`SELECT COALESCE(string_agg(concat_ws(' ', action, actor_type, actor_name), '; '), 'none') FROM audit_event WHERE action LIKE 'artifact.%'`))
	}
}
