//go:build e2e

package e2e

// The harness of the upgrade tests (R-HA-020, R-HA-052 "Rolling upgrade" and "Rollback"): a release is installed (a published
// one, or the working tree with its second UI bundle), replaced by the chart of the working tree and the image under test,
// and the installation is returned to the current dev release at the end. The loops that run under the upgrade are in
// upgrade_loops_test.go, the scenarios in upgrade_test.go and upgrade_bundles_test.go. docs/developer/e2e.md#the-upgrade-tests
// describes the whole.
//
// Where the previous release comes from (Q-HA-311): the newest release tag merged into HEAD, not at HEAD, whose chart the Helm
// repository the release pipeline publishes to lists (https://guided-traffic.github.io/quader/index.yaml). The repository
// gets a chart only after its image (`Release Docker & Helm`), so the image guidedtraffic/quader:<version> on Docker Hub,
// which the kind nodes pull themselves, exists for every listed chart. The dev values of that release come from git
// (`git show <tag>:deploy/dev/...`), so the release is installed with exactly the values its own repository used. The tags
// must be in the checkout (`git fetch --tags`); a checkout without a release tag fails the test and says so.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/e2e/priorrelease"
)

const (
	upgradeChartRepo     = "https://guided-traffic.github.io/quader/"
	upgradeMigrationsDir = "internal/meta/postgres/migrations"
	// upgradeRolesSelector selects the Deployments and pods of the release's roles, and not the dev profile's dependencies or
	// the driver pod.
	upgradeRolesSelector = "app.kubernetes.io/instance=quader,app.kubernetes.io/component in (api,packages,worker)"
)

// upgradeContractHeader is the header of a step that R-HA-021 allows only one release at a time or with downtime.
var upgradeContractHeader = regexp.MustCompile(`(?m)^-- quader:phase contract\b`)

func gitOut(t testing.TB, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", cfg.repo}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String()
}

// priorRelease is a released version to install: its tag, its version, and a directory with the dev values of its checkout.
type priorRelease struct {
	tag, version, valuesDir string
}

// releaseOf resolves a tag of the checkout and writes the dev values files of that tag into a directory of the test.
func releaseOf(t testing.TB, tag string) priorRelease {
	t.Helper()
	if !priorrelease.IsReleaseTag(tag) {
		t.Fatalf("%q is not a release tag (vX.Y.Z)", tag)
	}
	if _, err := exec.Command("git", "-C", cfg.repo, "rev-parse", "--verify", "--quiet", tag+"^{commit}").Output(); err != nil {
		t.Fatalf("the release tag %s is not in the checkout: run `git fetch --tags` (the upgrade tests install the release from its tag's chart repository entry and dev values)", tag)
	}
	dir := t.TempDir()
	for _, f := range []string{"values-dev.yaml", "values-scatter.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(gitOut(t, "show", tag+":deploy/dev/"+f)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return priorRelease{tag: tag, version: strings.TrimPrefix(tag, "v"), valuesDir: dir}
}

// previousRelease is the release the rolling upgrade starts from (Q-HA-311): the newest release tag merged into HEAD that does
// not point at HEAD (a checkout of a release tag upgrades from the release before it) and whose chart the repository lists.
// A newer tag the repository does not list yet (its release workflow is still running) or no longer offers (a release of the
// product's former name) is passed over and logged. A repository that does not answer, or a checkout without release tags,
// fails the test: the leg could not do its job. A repository that lists none of the release tags before HEAD skips it: there
// is no published release to upgrade from, which only the first release under a new name meets.
func previousRelease(t testing.TB) priorRelease {
	t.Helper()
	tags := strings.Fields(gitOut(t, "tag", "--merged", "HEAD"))
	if !slices.ContainsFunc(tags, priorrelease.IsReleaseTag) {
		t.Fatal("no release tag is merged into HEAD in the checkout: run `git fetch --tags` (a shallow clone needs fetch-depth 0)")
	}
	listed, err := priorrelease.Listed(fetchChartIndex(t), "quader")
	if err != nil {
		t.Fatalf("%sindex.yaml: %v", upgradeChartRepo, err)
	}
	tag, passed := priorrelease.Pick(tags, strings.Fields(gitOut(t, "tag", "--points-at", "HEAD")), listed)
	if len(passed) > 0 {
		t.Logf("newer release tags passed over: %s", strings.Join(passed, ", "))
	}
	if tag == "" {
		t.Skipf("no published release to upgrade from: %sindex.yaml lists %d versions of the chart quader and none of the release tags merged into HEAD", upgradeChartRepo, len(listed))
	}
	return releaseOf(t, tag)
}

// fetchChartIndex reads the chart repository's index.yaml, three tries at most.
func fetchChartIndex(t testing.TB) []byte {
	t.Helper()
	client := &http.Client{Timeout: 60 * time.Second}
	var last error
	for try := 1; try <= 3; try++ {
		resp, err := client.Get(upgradeChartRepo + "index.yaml")
		if err == nil {
			body, rerr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			switch {
			case rerr != nil:
				err = fmt.Errorf("status %d, reading the body: %w", resp.StatusCode, rerr)
			case resp.StatusCode != http.StatusOK:
				err = fmt.Errorf("status %d", resp.StatusCode)
			default:
				return body
			}
		}
		last = err
		time.Sleep(time.Duration(try) * 5 * time.Second)
	}
	t.Fatalf("GET %sindex.yaml: %v", upgradeChartRepo, last)
	return nil
}

// requireUpgradable ends the test when HEAD cannot be upgraded to from the release without downtime or a new database
// (Q-HA-312): a migration the release applied that HEAD changed, removed or renamed (the runner refuses the database), or a
// contract step added since (R-HA-021). Before the first stable release such a change is allowed (Q-ARCH-340,
// EnforceMigrationPhases off), and the test skips and names it; from then on it fails, because a release that needs it must
// name the release to start from or the downtime path.
func requireUpgradable(t testing.TB, rel priorRelease) {
	t.Helper()
	diff := gitOut(t, "diff", "--name-status", rel.tag, "HEAD", "--", upgradeMigrationsDir)
	t.Logf("the migrations changed since %s: %q", rel.tag, strings.Fields(diff))
	breaks, err := priorrelease.Breaks(diff, func(path string) (bool, error) {
		raw, err := os.ReadFile(filepath.Join(cfg.repo, path))
		if err != nil {
			return false, err
		}
		return upgradeContractHeader.Match(raw), nil
	})
	if err != nil {
		t.Fatalf("the migrations changed since %s: %v", rel.tag, err)
	}
	if len(breaks) == 0 {
		return
	}
	msg := fmt.Sprintf("the upgrade from %s to HEAD cannot run without downtime or a new database: %s", rel.tag, strings.Join(breaks, "; "))
	if !postgres.EnforceMigrationPhases {
		t.Skipf("%s. Before the first stable release such a change is allowed (Q-ARCH-340), so there is no zero-downtime upgrade to show", msg)
	}
	t.Fatalf("%s. R-HA-021 allows it one release at a time or with downtime: start the test from the release that precedes the step or name the downtime path in it", msg)
}

// ── helm, kind, scripts ──────────────────────────────────────────────────────

// upHelm runs helm against the cluster and keeps the output in upgrade-<label>.log of the artifacts.
func upHelm(t testing.TB, label string, timeout time.Duration, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "helm", append([]string{"--kube-context", cfg.kubeContext}, args...)...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	_ = os.WriteFile(filepath.Join(cfg.out, "upgrade-"+label+".log"), []byte("helm "+strings.Join(args, " ")+"\n\n"+buf.String()), 0o644)
	return buf.String(), err
}

func upChart() string { return filepath.Join(cfg.repo, "deploy", "helm", "quader") }

// upReleaseValues is the output of deploy/dev/release-values.sh: the helm arguments of the dev release in the scatter
// profile, of the working tree (valuesDir "") or of the copy of an older release's files.
func upReleaseValues(t testing.TB, valuesDir string) []string {
	t.Helper()
	return upReleaseValuesOf(t, valuesDir, upImage())
}

// upReleaseValuesOf is upReleaseValues for another image than the one under test: the pod annotation that rolls the pods
// when the loaded image changes carries that image's ID.
func upReleaseValuesOf(t testing.TB, valuesDir, image string) []string {
	t.Helper()
	args := []string{"scatter"}
	if valuesDir != "" {
		args = append(args, valuesDir)
	}
	cmd := exec.Command(filepath.Join(cfg.repo, "deploy", "dev", "release-values.sh"), args...)
	cmd.Dir = cfg.repo
	cmd.Env = append(os.Environ(), "QUADER_IMAGE="+image)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("release-values.sh %s: %v: %s", strings.Join(args, " "), err, errb.String())
	}
	return strings.Split(strings.TrimSpace(out.String()), "\n")
}

// upImage is the image under test, the one `make image` builds and up.sh loads.
func upImage() string {
	if v := os.Getenv("QUADER_IMAGE"); v != "" {
		return v
	}
	return "guidedtraffic/quader:dev"
}

// upKindLoad puts the image under test onto the kind nodes, as up.sh does before its helm upgrade.
func upKindLoad(t testing.TB) {
	t.Helper()
	upKindLoadImage(t, upImage())
}

// upKindLoadImage puts an image of the local Docker onto the kind nodes.
func upKindLoadImage(t testing.TB, image string) {
	t.Helper()
	out, err := exec.Command("kind", "load", "docker-image", image, "--name", strings.TrimPrefix(cfg.kubeContext, "kind-")).CombinedOutput()
	if err != nil {
		t.Fatalf("kind load docker-image %s: %v\n%s (run `make image` first)", image, err, tail(string(out), 800))
	}
}

// ── the installation ─────────────────────────────────────────────────────────

func upPodsExist(t testing.TB) bool {
	t.Helper()
	out, err := kubectl("-n", cfg.namespace, "get", "pods", "-l", upgradeRolesSelector, "-o", "name")
	return err != nil || strings.TrimSpace(out) != ""
}

// resetInstallation removes the release, empties the database (a release of another schema cannot be installed on this one)
// and Valkey, and leaves the dependencies of the dev profile (PostgreSQL, Valkey, S3, the IdP, the test upstreams) running.
// The blobs in the object store stay: they are addressed by content and nothing references them.
func resetInstallation(t testing.TB) {
	t.Helper()
	if out, err := upHelm(t, "uninstall", 10*time.Minute, "uninstall", "quader", "-n", cfg.namespace, "--wait", "--ignore-not-found"); err != nil {
		t.Fatalf("helm uninstall: %v\n%s", err, out)
	}
	waitFor(t, "the pods of the release to be gone", 5*time.Minute, 2*time.Second, func() bool { return !upPodsExist(t) })
	primary := pgPrimary(t)
	for _, stmt := range []string{"DROP DATABASE IF EXISTS quader WITH (FORCE)", "CREATE DATABASE quader OWNER quader"} {
		if out, err := kubectl("-n", cfg.namespace, "exec", primary, "-c", "postgres", "--", "psql", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", stmt); err != nil {
			t.Fatalf("%s: %v\n%s", stmt, err, out)
		}
	}
	script(t, "valkey-cli.sh", "FLUSHALL")
	// the administrator token and the fixtures of an earlier installation are gone with the database
	adminOnce = sync.Once{}
	adminTok, adminErr = "", nil
}

// installPrior installs the release from the chart repository with the dev values of its own tag.
func installPrior(t testing.TB, rel priorRelease) time.Duration {
	t.Helper()
	args := []string{"install", "quader", "quader", "--repo", upgradeChartRepo, "--version", rel.version, "-n", cfg.namespace}
	args = append(args, upReleaseValues(t, rel.valuesDir)...)
	args = append(args, "--set", "image.tag="+rel.version, "--wait", "--timeout", "15m")
	start := time.Now()
	if out, err := upHelm(t, "install-"+rel.version, 20*time.Minute, args...); err != nil {
		t.Fatalf("helm install of %s: %v\n%s", rel.tag, err, tail(out, 3000))
	}
	took := time.Since(start)
	t.Logf("installed %s (chart %s, image guidedtraffic/quader:%s) in %s", rel.tag, rel.version, rel.version, took.Round(time.Second))
	return took
}

// upgradeToTree runs helm upgrade to the chart of the working tree and the image under test with the dev values of the
// scatter profile, and the extra arguments (`--set web.enabled=true`). It returns the window the upgrade took.
func upgradeToTree(t testing.TB, label string, extra ...string) (from, to time.Time, out string, err error) {
	t.Helper()
	args := []string{"upgrade", "quader", upChart(), "-n", cfg.namespace}
	args = append(args, upReleaseValues(t, "")...)
	args = append(args, extra...)
	args = append(args, "--wait", "--timeout", "15m")
	from = time.Now()
	out, err = upHelm(t, label, 20*time.Minute, args...)
	to = time.Now()
	if err != nil && !strings.HasPrefix(label, "guard") { // the guard refuses at render time, before any hook runs
		collectMigrationEvidence(t, label)
	}
	return from, to, out, err
}

// collectMigrationEvidence keeps what the migration Job of a failed helm upgrade said: the next reset removes the Job.
func collectMigrationEvidence(t testing.TB, label string) {
	t.Helper()
	logs, _ := kubectl("-n", cfg.namespace, "logs", "-l", "job-name=quader-migrate", "--all-containers", "--tail=60", "--prefix")
	desc, _ := kubectl("-n", cfg.namespace, "describe", "job", "quader-migrate")
	_ = os.WriteFile(filepath.Join(cfg.out, "upgrade-"+label+"-migrate.log"), []byte(logs+"\n\n"+desc), 0o644)
	if strings.TrimSpace(logs) != "" {
		t.Logf("the migration Job's pods said:\n%s", tail(logs, 3000))
	}
}

// ── what the installation reports ────────────────────────────────────────────

type upReplica struct {
	Name    string   `json:"name"`
	Roles   []string `json:"roles"`
	Pool    string   `json:"pool"`
	Version string   `json:"version"`
	State   string   `json:"state"`
	Lost    bool     `json:"lost"`
}

// role is the role of a replica as the registry names it (Q-ARCH-341).
func (r upReplica) role() string {
	return strings.Join(r.Roles, ",")
}

func upReplicas() ([]upReplica, error) {
	code, _, body, err := edge("/api/v1/system/replicas")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("GET /api/v1/system/replicas: %d %s", code, strings.TrimSpace(body))
	}
	var l struct{ Items []upReplica }
	if err := json.Unmarshal([]byte(body), &l); err != nil {
		return nil, err
	}
	var live []upReplica
	for _, r := range l.Items {
		if !r.Lost {
			live = append(live, r)
		}
	}
	return live, nil
}

// describeReplicas groups the replicas by role and version, with their states: "api dev ready x3".
func describeReplicas(rs []upReplica) string {
	count := map[string]int{}
	for _, r := range rs {
		count[fmt.Sprintf("%s %s %s", r.role(), r.Version, r.State)]++
	}
	var parts []string
	for k, n := range count {
		parts = append(parts, fmt.Sprintf("%s x%d", k, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// waitReplicas polls the replica registry until cond holds and fails with the last registry when it does not within the time.
func waitReplicas(t testing.TB, what string, within time.Duration, cond func(rs []upReplica) bool) []upReplica {
	t.Helper()
	var last []upReplica
	var lastErr error
	ok := false
	start := time.Now()
	for time.Since(start) < within {
		last, lastErr = upReplicas()
		if lastErr == nil && cond(last) {
			ok = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !ok {
		t.Fatalf("%s: not within %s; the registry lists: %s (error %v)", what, within, describeReplicas(last), lastErr)
	}
	return last
}

// upStates is the set of states the replicas are in: a replica of the old release turns `draining` when it is asked to stop.
func upStates(rs []upReplica) []string {
	seen := map[string]bool{}
	for _, r := range rs {
		seen[r.State] = true
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func upVersions(rs []upReplica) []string {
	seen := map[string]bool{}
	for _, r := range rs {
		seen[r.Version] = true
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// migrateStatus is the report of `quader migrate status` in a worker pod. The upgrade verification reads GET
// /api/v1/system/migrations (upMigrations); the command reads the rollback's release in its own worker, which the edge cannot
// pick, and is the cross-check of the endpoint, which answers from the same code (Q-HA-116).
type migrateStatus struct {
	Schema struct {
		Applied     int    `json:"applied"`
		CompatFloor string `json:"compatFloor"`
	} `json:"schema"`
	Binary struct {
		Version        string `json:"version"`
		RequiredSchema int    `json:"requiredSchema"`
	} `json:"binary"`
	Pending []struct {
		Version int    `json:"version"`
		Name    string `json:"name"`
		Class   string `json:"class"`
	} `json:"pending"`
	Servable bool   `json:"servable"`
	Verdict  string `json:"verdict"`
}

func upMigrateStatus(t testing.TB) migrateStatus {
	t.Helper()
	out := kn(t, "exec", "deploy/quader-worker", "--", "quader", "migrate", "status")
	var s migrateStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("quader migrate status: %v\n%s", err, tail(out, 600))
	}
	return s
}

// migrationReport is the answer of GET /api/v1/system/migrations (R-HA-018), the members the verification reads.
type migrationReport struct {
	Schema struct {
		Applied     int    `json:"applied"`
		CompatFloor string `json:"compatFloor"`
	} `json:"schema"`
	Binary struct {
		Version        string `json:"version"`
		RequiredSchema int    `json:"requiredSchema"`
	} `json:"binary"`
	Pending []struct {
		Version            int      `json:"version"`
		Name               string   `json:"name"`
		Class              string   `json:"class"`
		RequiresBackground []string `json:"requiresBackground"`
	} `json:"pending"`
	Applied []struct {
		Version int    `json:"version"`
		Name    string `json:"name"`
	} `json:"applied"`
	Background []struct {
		Name    string `json:"name"`
		Release string `json:"release"`
		State   string `json:"state"`
	} `json:"background"`
}

// unfinished lists the background migrations that have not succeeded, "name (state)".
func (r migrationReport) unfinished() []string {
	var out []string
	for _, b := range r.Background {
		if b.State != "succeeded" {
			out = append(out, b.Name+" ("+b.State+")")
		}
	}
	return out
}

// upMigrations reads GET /api/v1/system/migrations with the administrator's token through the edge, from a replica of the
// release version (Q-HA-116). An answer from a pod of the release before that the edge still reaches while it drains, or a
// 404 of a release without the route, is not the report of the release under test and is read again; the first answer of a
// replica of version is returned. Any other status fails the test.
func upMigrations(t testing.TB, version string) migrationReport {
	t.Helper()
	// The token alone: the route needs no project, so the read does not depend on ensureE2EProject (adminAPI).
	a := newAPI(t).withToken(adminToken(t))
	deadline := time.Now().Add(90 * time.Second)
	var others []string
	for {
		r, err := a.send("GET", "/api/v1/system/migrations", nil)
		if err != nil {
			t.Fatalf("GET /api/v1/system/migrations: %v", err)
		}
		var rep migrationReport
		switch r.Status {
		case 200:
			r.JSON(t, &rep)
			if rep.Binary.Version == version {
				t.Logf("GET /api/v1/system/migrations answered by %s (%s)", r.Replica(), version)
				return rep
			}
			others = append(others, r.Replica()+" "+rep.Binary.Version)
		case 404:
			others = append(others, r.Replica()+" 404")
		default:
			t.Fatalf("GET /api/v1/system/migrations: %s", r)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no replica of %s answered GET /api/v1/system/migrations within 90 s; answers from %v", version, others)
		}
		time.Sleep(time.Second)
	}
}

// requireMigrationsDone is R-HA-020's third verification step for the release version: GET /api/v1/system/migrations shows
// no pending step, the schema the binary requires is applied, and the expected background migrations, every one the binary
// declares, reach `succeeded` (a failed one fails at once). `quader migrate status` in the worker of that release, which runs
// the same code, must agree on the schema, the binary and the pending steps (Q-HA-116).
func requireMigrationsDone(t testing.TB, version string) {
	t.Helper()
	rep := upMigrations(t, version)
	if len(rep.Pending) != 0 || rep.Schema.Applied < rep.Binary.RequiredSchema || rep.Binary.RequiredSchema == 0 {
		t.Errorf("migrations after the upgrade to %s: pending %+v, applied %d, required %d", version, rep.Pending, rep.Schema.Applied, rep.Binary.RequiredSchema)
	}
	if len(rep.Background) == 0 {
		t.Errorf("the report of %s lists no background migration; the binary declares nuget_semver2 (Q-HA-114)", version)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for len(rep.unfinished()) > 0 {
		for _, b := range rep.Background {
			if b.State == "failed" {
				t.Fatalf("the background migration %s failed after the upgrade to %s", b.Name, version)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("background migrations not succeeded 3 minutes after the upgrade to %s: %v", version, rep.unfinished())
		}
		time.Sleep(5 * time.Second)
		rep = upMigrations(t, version)
	}
	t.Logf("migrations of %s: schema %d (floor %s), %d applied, none pending, background %+v", version, rep.Schema.Applied, rep.Schema.CompatFloor, len(rep.Applied), rep.Background)
	cli := upMigrateStatus(t)
	if cli.Binary.Version != version || cli.Binary.RequiredSchema != rep.Binary.RequiredSchema || cli.Schema.Applied != rep.Schema.Applied ||
		cli.Schema.CompatFloor != rep.Schema.CompatFloor || len(cli.Pending) != len(rep.Pending) || !cli.Servable {
		t.Errorf("`quader migrate status` in the worker disagrees with the endpoint: CLI %+v, endpoint schema %+v binary %+v pending %d", cli, rep.Schema, rep.Binary, len(rep.Pending))
	}
}

// deploymentState is the identity of a Deployment: a changed UID is a recreation, a changed generation a changed spec.
type deploymentState struct {
	UID        string
	Generation int64
}

func upDeployments(t testing.TB) map[string]deploymentState {
	t.Helper()
	out := kn(t, "get", "deployments", "-l", upgradeRolesSelector, "-o", "json")
	var l struct {
		Items []struct {
			Metadata struct {
				Name       string `json:"name"`
				UID        string `json:"uid"`
				Generation int64  `json:"generation"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		t.Fatal(err)
	}
	m := map[string]deploymentState{}
	for _, it := range l.Items {
		m[it.Metadata.Name] = deploymentState{UID: it.Metadata.UID, Generation: it.Metadata.Generation}
	}
	return m
}

// helmRevision is the number of the release's latest revision.
func helmRevision(t testing.TB) int {
	t.Helper()
	out, err := upHelm(t, "history", 2*time.Minute, "history", "quader", "-n", cfg.namespace, "-o", "json")
	if err != nil {
		t.Fatalf("helm history: %v\n%s", err, out)
	}
	var h []struct {
		Revision    int    `json:"revision"`
		Status      string `json:"status"`
		Chart       string `json:"chart"`
		AppVersion  string `json:"app_version"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(out), &h); err != nil || len(h) == 0 {
		t.Fatalf("helm history: %v\n%s", err, out)
	}
	return h[len(h)-1].Revision
}

func deploymentNames(m map[string]deploymentState) []string {
	var names []string
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// rolloutComplete is R-HA-020's first verification step: `kubectl rollout status` of every Deployment of the release.
func rolloutComplete(t testing.TB, names []string) {
	t.Helper()
	for _, name := range names {
		out, err := kubectl("-n", cfg.namespace, "rollout", "status", "deployment/"+name, "--timeout=5m")
		if err != nil {
			t.Errorf("rollout status of %s: %v", name, err)
			continue
		}
		t.Logf("rollout status %s: %s", name, strings.TrimSpace(out))
	}
}

// ── the administrator of a reset installation ───────────────────────────────

// bootstrapAdmin is the bootstrap of adminToken for a reset installation: `quader admin` in a worker pod, then a wait until the binding of the new administrator is in force on the replicas
// that answer, which a count of consecutive answers from at least two replicas shows (the edge scatters per request). The
// token is stored where adminAPI reads it, so the fixtures of the other suites build on it unchanged.
func bootstrapAdmin(t testing.TB) {
	t.Helper()
	adminOnce = sync.Once{}
	adminOnce.Do(func() {
		if _, err := quaderAdmin(t, "", "service-account", "create", "e2e-admin", "--description", "end-to-end tests", "--admin"); err != nil && !strings.Contains(err.Error(), "exist") {
			adminErr = err
			return
		}
		adminTok, adminErr = quaderAdmin(t, `{"inherit":true,"admin":true}`, "token", "create", "--service-account", "e2e-admin", "--ttl", "24h", "--scope-file", "/dev/stdin")
		if adminErr != nil {
			return
		}
		a := newAPI(t).withToken(adminTok)
		deadline := time.Now().Add(60 * time.Second)
		streak := 0
		seen := map[string]bool{}
		for streak < 12 || len(seen) < 2 {
			if time.Now().After(deadline) {
				adminErr = fmt.Errorf("the binding of e2e-admin is not in force on two replicas within 60 s (%d consecutive answers from %d replicas)", streak, len(seen))
				return
			}
			if r, err := a.send("GET", "/api/v1/service-accounts", nil); err == nil && r.Status == 200 {
				streak++
				seen[r.Replica()] = true
			} else {
				streak, seen = 0, map[string]bool{}
				time.Sleep(500 * time.Millisecond)
			}
		}
	})
	if adminErr != nil {
		t.Fatalf("bootstrap administrator: %v", adminErr)
	}
}

// ── the end of a test ────────────────────────────────────────────────────────

// markScatterProfile tells the suite that the cluster is in the scatter profile already, so that the fixtures of the npm and
// NuGet suites (npmFx, nugetFx call useScatter) do not run up.sh and replace the release under test. TestMain returns the
// cluster to the default profile at the end.
func markScatterProfile() {
	scatterOnce.Do(func() {})
	recordProfile("scatter", nil)
}

// restoreCurrentRelease leaves the cluster with the current dev release installed, in the default profile, so that the
// sets that run after this one find what they expect. A release that helm does not know is replaced from scratch; one it
// knows is upgraded in place, which is an ordinary upgrade.
func restoreCurrentRelease(t testing.TB) {
	t.Helper()
	if _, err := upHelm(t, "status", time.Minute, "status", "quader", "-n", cfg.namespace); err != nil {
		t.Logf("helm does not know the release quader (%v): replacing the installation", err)
		resetInstallation(t)
	}
	if err := runUp("default"); err != nil {
		t.Errorf("returning to the current dev release: %v", err)
		return
	}
	t.Logf("the current dev release is installed again (default profile)")
}
