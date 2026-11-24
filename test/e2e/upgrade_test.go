//go:build e2e

package e2e

// The upgrade tests (docs/developer/e2e.md#the-upgrade-tests):
//
//   - TestUpgrade_R_HA_020_AcrossTwoBundles (upgrade_bundles_test.go, the set `upgrade`) upgrades the working tree from its
//     second UI bundle to the image under test: the rolling upgrade of one commit, which needs no published artifact.
//   - TestUpgrade_R_HA_020_RollingFromThePreviousRelease (the set `upgrade-release`) is R-HA-020's acceptance criterion and the
//     rows "Rolling upgrade" and "Rollback" of R-HA-052: the newest published release before the working tree (Q-HA-311) is
//     upgraded in one helm upgrade with the migration Job and rolled back, under the loops.
//
// Both replace the installation: they remove the release, empty the database, Valkey and the installation's object-store
// locations (resetInstallation), install the release to start from, and at the end return the cluster to the current dev release (deploy/dev/e2e.sh).

import (
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// upgradeHold is how long the loops keep running before and after each step: the tail of an upgrade (a drained pod, a
// switched edge) must meet them too.
const upgradeHold = 45 * time.Second

// underTestVersion is the version the image under test reports (`quader version`), which the registry lists for its replicas.
func underTestVersion(t testing.TB) string {
	t.Helper()
	out, err := exec.Command("docker", "run", "--rm", upImage(), "version").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run %s version: %v\n%s (run `make image` first)", upImage(), err, out)
	}
	m := regexp.MustCompile(`^quader (\S+)`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("cannot read the version from %q", out)
	}
	return m[1]
}

// upSampler asks the registry every two seconds while a step runs and keeps what it saw: the version sets (a state with two
// versions is the mixed-version window), the replica states and the outcomes of its own requests through the edge.
type upSampler struct {
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	versions map[string]int // "0.62.0,dev" -> samples
	codes    map[int]int
	states   map[string]int // "draining,ready" -> samples
}

func startSampler() *upSampler {
	s := &upSampler{stop: make(chan struct{}), done: make(chan struct{}), versions: map[string]int{}, codes: map[int]int{}, states: map[string]int{}}
	go func() {
		defer close(s.done)
		for {
			rs, err := upReplicas()
			s.mu.Lock()
			if err != nil {
				s.codes[0]++
			} else {
				s.codes[200]++
				s.versions[strings.Join(upVersions(rs), ",")]++
				s.states[strings.Join(upStates(rs), ",")]++
			}
			s.mu.Unlock()
			select {
			case <-s.stop:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return s
}

func (s *upSampler) finish() *upSampler {
	close(s.stop)
	<-s.done
	return s
}

// mixed says whether any sample listed more than one version.
func (s *upSampler) mixed() bool {
	for v := range s.versions {
		if strings.Contains(v, ",") {
			return true
		}
	}
	return false
}

func requireUpgradeSetup(t *testing.T) {
	t.Helper()
	for _, name := range []string{"QUADER_E2E_NPMSTUB_HOST", "QUADER_E2E_NUGETSTUB_HOST", "QUADER_E2E_NPM_CLIENTS_PODS", "QUADER_E2E_NUGET_CLIENTS_PODS"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is not set: run the upgrade tests through `deploy/dev/e2e.sh upgrade` or `upgrade-release` (make e2e SET=…)", name)
		}
	}
	if err := exec.Command("docker", "image", "inspect", upImage()).Run(); err != nil {
		t.Fatalf("the image %s is not in the local Docker: run `make image`", upImage())
	}
}

// TestUpgrade_R_HA_020_RollingFromThePreviousRelease is R-HA-020's acceptance criterion and the rows "Rolling upgrade" and
// "Rollback" of R-HA-052: the newest published release before the working tree (Q-HA-311) is upgraded with one
// `helm upgrade --wait` (the migration Job first, then every Deployment rolling on its own) under the loops, R-HA-020's
// verification passes, and `helm rollback` returns to the release under the same loops. A commit that changed a migration
// the release applied, or added a contract step, is not upgraded to without downtime: the test skips and names it before the
// first stable release and fails after it (Q-HA-312). A replica of the old release in the state `draining` while it stops is
// part of the window, not an error: the registry lists it until its row is deleted or, at the latest,
// system.heartbeat_stale_after after its last heartbeat (R-OPS-003).
func TestUpgrade_R_HA_020_RollingFromThePreviousRelease(t *testing.T) {
	requireUpgradeSetup(t)
	prior := previousRelease(t)
	requireUpgradable(t, prior)
	version := underTestVersion(t)
	t.Logf("the upgrade starts from %s (chart quader %s, image guidedtraffic/quader:%s); image under test %s", prior.tag, prior.version, prior.version, version)
	t.Cleanup(func() { restoreCurrentRelease(t) })
	upKindLoad(t)

	resetInstallation(t)
	installPrior(t, prior)
	fx := buildUpgradeFixture(t)
	loops := startUpgradeLoops(t, fx, "rolling")
	time.Sleep(upgradeHold)

	// the upgrade
	rev := helmRevision(t)
	sampler := startSampler()
	from1, to1, out, err := upgradeToTree(t, "upgrade")
	s1 := sampler.finish()
	if err != nil {
		t.Fatalf("helm upgrade --wait from %s: %v\n%s", prior.tag, err, tail(out, 3000))
	}
	t.Logf("upgrade took %s; versions seen in the registry %v, replica states %v, registry reads %v", to1.Sub(from1).Round(time.Second), s1.versions, s1.states, s1.codes)
	if !s1.mixed() {
		t.Errorf("the registry never listed two versions (%v): the upgrade did not run in the mixed-version window", s1.versions)
	}

	// R-HA-020's verification
	t.Run("R-HA-020 verification: rollout status, the replicas list only the new version, GET /api/v1/system/migrations shows no pending step and the background migrations succeeded", func(t *testing.T) {
		rolloutComplete(t, deploymentNames(upDeployments(t)))
		rs := waitReplicas(t, "the registry lists only the new version", 3*time.Minute, func(rs []upReplica) bool {
			return slices.Equal(upVersions(rs), []string{version})
		})
		t.Logf("the registry lists: %s", describeReplicas(rs))
		requireMigrationsDone(t, version)
	})
	time.Sleep(upgradeHold)

	// the rollback, to the revision the upgrade started from
	sampler = startSampler()
	fromR := time.Now()
	out, err = upHelm(t, "rollback", 20*time.Minute, "rollback", "quader", strconv.Itoa(rev), "-n", cfg.namespace, "--wait", "--timeout", "15m")
	toR := time.Now()
	sr := sampler.finish()
	if err != nil {
		t.Fatalf("helm rollback to revision %d: %v\n%s", rev, err, tail(out, 3000))
	}
	t.Logf("rollback took %s; versions seen in the registry %v, replica states %v, registry reads %v", toR.Sub(fromR).Round(time.Second), sr.versions, sr.states, sr.codes)
	t.Run("R-HA-052 R-HA-019 rollback: the replicas list only the previous release, which serves the migrated schema", func(t *testing.T) {
		rolloutComplete(t, deploymentNames(upDeployments(t)))
		rs := waitReplicas(t, "the registry lists only the previous release", 3*time.Minute, func(rs []upReplica) bool {
			return slices.Equal(upVersions(rs), []string{prior.version})
		})
		t.Logf("the registry lists: %s", describeReplicas(rs))
		st := upMigrateStatus(t)
		if !st.Servable || st.Binary.Version != prior.version || len(st.Pending) != 0 {
			t.Errorf("the previous release on the migrated schema: servable %v (%s), binary %s, pending %v", st.Servable, st.Verdict, st.Binary.Version, st.Pending)
		}
	})
	time.Sleep(upgradeHold)

	res := loops.stop(t)
	res.requireClean(t, upWindow{"the upgrade", from1, to1}, upWindow{"the rollback", fromR, toR})
	res.requireEdgeServed(t)
	res.publishedAreReadable(t, fx)
}
