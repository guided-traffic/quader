//go:build integration

// Package ansiblevirtual tests the virtual class of the Ansible Galaxy format (R-ANS-022 to R-ANS-024) against the integration stack and the
// counting stub Galaxy of test/ansiblestub: a virtual repository over a local and a remote member, read and published to through the
// Galaxy API of the repository as `ansible-galaxy` sends it. The remote members reach the stub through app.Options.DialOverride.
//
// What these tests do not prove, and who does: the acceptance criteria that run `ansible-galaxy collection install` and `collection publish`
// need the client, which is the e2e set `ansible`; the same requests are sent here over HTTP and counted at the stub.
package ansiblevirtual

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	_ "github.com/guided-traffic/quader/internal/formats/all"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/ansiblestub"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

// The repositories of the installation.
const (
	rLocal   = "ans-local"     // the local member and the deployment target
	rGalaxy  = "galaxy"        // the remote member: the stub
	rVirtual = "ans"           // virtual: ans-local, galaxy; deployment target ans-local
	rReverse = "ans-rev"       // virtual: galaxy, ans-local; deployment target ans-local
	rNoLocal = "ans-ro"        // virtual: galaxy; no deployment target
	rLocalV  = "ans-localonly" // virtual: ans-local; deployment target ans-local
)

// venv is one installation with the repositories above, a stub, a reader, a writer and a writer without manage.
type venv struct {
	t    *testing.T
	st   *testenv.Stack
	stub *ansiblestub.Server
	run  *ansiblestub.Running
	rep  *testenv.Replica

	// reader reads everything, writer reads, deploys and manages the local member and the virtual repositories, plain does the same
	// without manage (it meets the shadowing check).
	reader, writer, plain string
	// writerP and plainP are the people behind writer and plain, for a browser's session of theirs.
	writerP, plainP testenv.Person
}

type options struct {
	// claim claims the pattern for ans-local (R-REPO-023).
	claim string
	// levels sets the visibility of repositories before the replica starts: the artifacts level and the wiki level by key (R-AUTHZ-033);
	// the project is internal then.
	levels map[string][2]string
}

func newEnv(t *testing.T, o options) *venv {
	t.Helper()
	ss, err := ansiblestub.NewSelfSigned(ansiblestub.LoopbackHosts...)
	if err != nil {
		t.Fatal(err)
	}
	e := &venv{t: t}
	if e.stub, err = ansiblestub.New(ansiblestub.Config{}); err != nil {
		t.Fatal(err)
	}
	if e.run, err = e.stub.Start("127.0.0.1:0", ss.ServerConfig()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.run.Close() })
	ca := testenv.WriteFile(t, t.TempDir(), "stub-ca.pem", string(ss.CAPEM))
	e.st = testenv.NewStack(t, testenv.StackOptions{Name: "ansiblevirtual", Env: []string{
		"QUADER_PROXY_OUTBOUND_TLS_CA_FILES=[" + strconv.Quote(filepath.ToSlash(ca)) + "]",
		`QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.1/32"]`,
		"QUADER_PROXY_LOCK_POLL_INTERVAL_MS=50",
		"QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off",
		"QUADER_API_RATE_LIMITS_ENABLED=false",
	}})
	host := func(h string) map[string]any { return map[string]any{"pattern": h, "sendCredentials": false} }
	e.remote(rGalaxy, e.run.APIRoot(), host(ansiblestub.DefaultGitHubHost), host(ansiblestub.DefaultCodeloadHost))
	local := `{"local":{"autoClaim":false}}`
	e.st.Repository(rLocal, "local", "ansible", local)
	for key, members := range map[string][]string{
		rVirtual: {rLocal, rGalaxy}, rReverse: {rGalaxy, rLocal}, rNoLocal: {rGalaxy}, rLocalV: {rLocal},
	} {
		e.st.Repository(key, "virtual", "ansible", "{}")
		for i, m := range members {
			e.st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id)
				SELECT v.id, $3, m.id FROM repositories v, repositories m WHERE v.key = $1 AND m.key = $2`, key, m, i)
		}
	}
	for _, key := range []string{rVirtual, rReverse, rLocalV} {
		e.st.Exec(`INSERT INTO virtual_deploy_targets (virtual_id, target_id)
			SELECT v.id, m.id FROM repositories v, repositories m WHERE v.key = $1 AND m.key = $2`, key, rLocal)
	}
	if o.claim != "" {
		e.st.Exec(`INSERT INTO namespace_claims (repo_id, pattern, source, scope) SELECT id, $1, 'manual', 'members' FROM repositories WHERE key = $2`, o.claim, rLocal)
	}
	rita, reader, writer, plain := e.st.User("rita"), e.st.User("reader"), e.st.User("writer"), e.st.User("plain")
	e.st.Bind("admin", rita)
	e.reader, e.writer = e.st.Token(reader, scope.Default()), e.st.Token(writer, scope.Default())
	e.plain = e.st.Token(plain, scope.Default())
	e.writerP, e.plainP = writer, plain
	all := []string{rLocal, rGalaxy, rVirtual, rReverse, rNoLocal, rLocalV}
	e.st.Policy("ans-readers", []string{"read"}, all, reader)
	e.st.Policy("ans-writers", []string{"read", "deploy", "delete", "manage"}, []string{rLocal, rVirtual, rReverse, rLocalV, rNoLocal}, writer)
	e.st.Policy("ans-writers-read", []string{"read"}, []string{rGalaxy}, writer)
	e.st.Policy("ans-plain", []string{"read", "deploy"}, []string{rLocal, rVirtual, rReverse, rLocalV}, plain)
	e.st.Policy("ans-plain-read", []string{"read"}, []string{rGalaxy}, plain)
	if len(o.levels) > 0 {
		e.st.Exec(`UPDATE project SET visibility = 'internal' WHERE key = $1`, testenv.DefaultProject)
		for key, l := range o.levels {
			e.st.Exec(`UPDATE repositories SET visibility_artifacts = $2, visibility_wiki = $3 WHERE key = $1`, key, l[0], l[1])
		}
	}
	e.rep = e.st.Start(t, testenv.ReplicaOptions{Name: "web-1", DialOverride: e.run.DialOverride()})
	return e
}

// remote creates a remote repository of the format ansible that reads the stub.
func (e *venv) remote(key, upstream string, extra ...map[string]any) {
	e.t.Helper()
	hosts := make([]any, len(extra))
	for i, h := range extra {
		hosts[i] = h
	}
	doc, err := json.Marshal(map[string]any{"remote": map[string]any{
		"url": upstream, "extraHosts": hosts, "authMode": "none", "username": "", "password": nil,
		"clientCertificate": nil, "headers": []any{}, "forwardAuthOnRedirect": false, "outboundProxy": map[string]any{"mode": "direct"},
		"connectTimeoutMs": 5000, "readTimeoutMs": 15000, "sourceNetwork": nil, "cacheFillAction": "read",
		"cache": map[string]any{
			"storeArtifacts": true, "metadataTtlSeconds": 3600, "metadataRefreshTimeoutSeconds": 10, "notFoundTtlSeconds": 60,
			"maxStaleSeconds": 0, "unusedRetentionHours": 0,
		},
		"integrity":    map[string]any{"checksumPolicy": "verify-if-available", "blockUnexpectedContentTypes": true, "allowedContentTypes": []any{}},
		"availability": map[string]any{"mode": "online", "failureThreshold": 3, "circuitOpenSeconds": 30},
		"limits":       map[string]any{"maxArtifactBytes": nil, "maxMetadataBytes": nil, "maxConcurrentFetches": 16},
	}})
	if err != nil {
		e.t.Fatal(err)
	}
	e.st.Repository(key, "remote", "ansible", string(doc))
}

func apiPath(repo, path string) string {
	return "/api/ansible/" + testenv.DefaultProject + "/" + repo + "/" + path
}

// get sends a GET below the repository with the token in the form ansible-galaxy sends a configured token.
func (e *venv) get(repo, path, token string, hdr ...string) testenv.Answer {
	e.t.Helper()
	return e.st.Do(e.rep, http.MethodGet, apiPath(repo, path), "", nil, append([]string{"Authorization", "Token " + token}, hdr...)...)
}

// publish sends a collection archive to a repository as `ansible-galaxy collection publish` does: a multipart body of the parts
// sha256 and file.
func (e *venv) publish(repo, token string, spec collectiontest.Spec) (testenv.Answer, []byte) {
	e.t.Helper()
	archive := collectiontest.Build(spec)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	sum := sha256.Sum256(archive)
	if err := mw.WriteField("sha256", hex.EncodeToString(sum[:])); err != nil {
		e.t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", spec.FileName())
	if err != nil {
		e.t.Fatal(err)
	}
	_, _ = fw.Write(archive)
	if err := mw.Close(); err != nil {
		e.t.Fatal(err)
	}
	return e.st.Do(e.rep, http.MethodPost, apiPath(repo, "v3/artifacts/collections/"), "", body.Bytes(),
		"Authorization", "Token "+token, "Content-Type", mw.FormDataContentType()), archive
}

// spec is a collection of the format the tests publish.
func spec(ns, name, version string) collectiontest.Spec {
	return collectiontest.Spec{Namespace: ns, Name: name, Version: version, Files: map[string]string{"README.md": "# " + ns + "." + name + " " + version + "\n"}}
}

type versionList struct {
	Meta  struct{ Count int }    `json:"meta"`
	Links struct{ Next *string } `json:"links"`
	Data  []struct {
		Version string `json:"version"`
		Href    string `json:"href"`
	} `json:"data"`
}

func (v versionList) versions() []string {
	out := make([]string, len(v.Data))
	for i, d := range v.Data {
		out[i] = d.Version
	}
	return out
}

func (e *venv) versions(repo, ns, name, token string, query ...string) (versionList, testenv.Answer) {
	e.t.Helper()
	path := "v3/collections/" + ns + "/" + name + "/versions/"
	if len(query) > 0 {
		path += "?" + query[0]
	}
	a := e.get(repo, path, token)
	var v versionList
	if a.Status == http.StatusOK {
		if err := json.Unmarshal(a.Body, &v); err != nil {
			e.t.Fatalf("%v: %s", err, a.Body)
		}
	}
	return v, a
}

// errorCode is the code of the first error of a Galaxy-shaped error body.
func errorCode(a testenv.Answer) string {
	var b struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(a.Body, &b)
	if len(b.Errors) == 0 {
		return ""
	}
	return b.Errors[0].Code
}

// stubVersions are the versions the stub holds of a collection, newest first.
func (e *venv) stubVersions(ns, name string) []ansiblestub.Version {
	e.t.Helper()
	corpus, err := e.stub.Corpus()
	if err != nil {
		e.t.Fatal(err)
	}
	for _, c := range corpus.Collections {
		if c.Namespace == ns && c.Name == name {
			return c.Versions
		}
	}
	e.t.Fatalf("the stub holds no %s.%s", ns, name)
	return nil
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func contains(a testenv.Answer, s string) bool { return strings.Contains(string(a.Body), s) }
