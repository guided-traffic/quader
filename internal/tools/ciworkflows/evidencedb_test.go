package ciworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The stand-ins of the database's recorder of deploy/dev/e2e-evidence.sh. kubectl names the primary quader-db-1, runs the script the
// recorder hands to `exec … -- sh -c SCRIPT _ /proc` here with STUB_PROC as the proc root (psql below answers for PostgreSQL), lists the
// replicas of STUB_REPLICAS, and answers the API server's proxy to a replica's /metrics from STUB_METRICS/<pod> (no file: the read
// fails); everything else answers nothing. psql prints STUB_PSQL. kind, docker and jq answer nothing, for the stop path. Every kubectl
// call goes to STUB_LOG.
var evidenceStubs = map[string]string{
	"kubectl": `#!/usr/bin/env bash
echo "kubectl $*" >> "$STUB_LOG"
case "$*" in
  *cnpg.io/instanceRole=primary*) echo pod/quader-db-1; exit 0 ;;
  *" exec quader-db-1 "*)
    while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
    shift
    args=("$@")
    args[$((${#args[@]} - 1))]="$STUB_PROC"
    exec "${args[@]}" ;;
  *"get pods -l app.kubernetes.io/instance=quader,quader/serve=true"*)
    for p in $STUB_REPLICAS; do echo "pod/$p"; done; exit 0 ;;
  *"get --raw /api/v1/namespaces/quader/pods/https:"*)
    pod="${!#}"; pod="${pod##*pods/https:}"; pod="${pod%%:8081/proxy/metrics}"
    [ -f "$STUB_METRICS/$pod" ] || exit 1
    cat "$STUB_METRICS/$pod"; exit 0 ;;
esac
exit 0
`,
	"psql": `#!/usr/bin/env bash
cat "$STUB_PSQL"
`,
	"kind":   "#!/usr/bin/env bash\nexit 0\n",
	"docker": "#!/usr/bin/env bash\nexit 1\n",
	"jq":     "#!/usr/bin/env bash\ncat >/dev/null\nexit 0\n",
}

// evidenceTree copies the recorder, the library it sources and the pins into a new tree.
func evidenceTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"deploy/dev/e2e-evidence.sh", "deploy/dev/lib.sh", "deploy/dev/dev-tag.sh", "tools/versions.env"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(f, ".sh") {
			mode = 0o755
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), raw, mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// lines are the lines of a file with the 13-digit time of each row replaced by T, sorted, the header first.
func lines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Split(strings.TrimSpace(regexp.MustCompile(`(?m)^[0-9]{13},`).ReplaceAllString(string(raw), "T,")), "\n")
	body := all[1:]
	sort.Strings(body)
	return append(all[:1], body...)
}

// TestEvidenceRecordsEachPoolsSizeAndTheMemoryOfTheConnections: the database's recorder of the evidence
// (deploy/dev/e2e-evidence.sh, db_sample) writes, per sample, the primary's client backends by application_name and state with the sum
// of their RssAnon, a backend without a readable status counted with 0 KiB, and each replica's pool sizes from its
// quader_db_pool_max_connections once per pod: a replica whose metrics cannot be read yet is asked again in the next sample, one that was
// read is not read again. stop writes db-connections.csv and db-pools.csv with their headers and removes the working files. Unit, about
// 2 s of shell on a loaded laptop (three runs of the script with its stand-ins), files of a few hundred bytes; no kind cluster: the
// stand-ins answer for kubectl and psql. The first file of a real cluster comes from the next kind leg that records evidence.
func TestEvidenceRecordsEachPoolsSizeAndTheMemoryOfTheConnections(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	root := evidenceTree(t)
	stubs, state, proc, metrics := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	for name, body := range evidenceStubs {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	psqlOut := filepath.Join(state, "psql.out")
	write(t, psqlOut, "101,quader-api-1/request,active\n102,quader-api-1/request,idle\n103,quader-api-1/request,idle\n104,-,idle\n")
	for pid, kib := range map[string]string{"101": "2048", "102": "1024", "103": "512"} {
		write(t, filepath.Join(proc, pid, "status"), "Name:\tpostgres\nVmRSS:\t   9999 kB\nRssAnon:\t   "+kib+" kB\nRssFile:\t 100 kB\n")
	}
	write(t, filepath.Join(metrics, "quader-api-1"), "# HELP quader_db_pool_max_connections Pool size.\n"+
		"quader_db_pool_max_connections{db_pool=\"control\"} 2\nquader_db_pool_max_connections{db_pool=\"request\"} 20\nquader_db_pool_connections{db_pool=\"request\",state=\"idle\"} 3\n")
	dir := filepath.Join(state, "evidence")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(state, "calls.log")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bash, append([]string{filepath.Join(root, "deploy", "dev", "e2e-evidence.sh")}, args...)...)
		cmd.Env = []string{
			"PATH=" + stubs + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + state, "TMPDIR=" + state,
			"QUADER_DEV_TAG=dev", "STUB_LOG=" + log, "STUB_PROC=" + proc, "STUB_PSQL=" + psqlOut, "STUB_METRICS=" + metrics,
			"STUB_REPLICAS=quader-api-1 quader-worker-1",
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("e2e-evidence.sh %v: %v\n%s", args, err, out)
		}
	}

	run("db-sample", dir)
	// quader-worker-1 answers its metrics from the second sample on, as a pod does once it serves.
	write(t, filepath.Join(metrics, "quader-worker-1"), "quader_db_pool_max_connections{db_pool=\"lease\"} 4\n")
	run("db-sample", dir)
	run("stop", dir)

	if got, want := strings.Join(lines(t, filepath.Join(dir, "db-connections.csv")), "\n"), strings.Join([]string{
		"unix_ms,pod,application_name,state,count,anon_kib",
		"T,quader-db-1,-,idle,1,0", "T,quader-db-1,-,idle,1,0",
		"T,quader-db-1,quader-api-1/request,active,1,2048", "T,quader-db-1,quader-api-1/request,active,1,2048",
		"T,quader-db-1,quader-api-1/request,idle,2,1536", "T,quader-db-1,quader-api-1/request,idle,2,1536",
	}, "\n"); got != want {
		t.Errorf("db-connections.csv:\n%s\nwant\n%s", got, want)
	}
	if got, want := strings.Join(lines(t, filepath.Join(dir, "db-pools.csv")), "\n"), strings.Join([]string{
		"unix_ms,pod,db_pool,max_connections",
		"T,quader-api-1,control,2", "T,quader-api-1,request,20", "T,quader-worker-1,lease,4",
	}, "\n"); got != want {
		t.Errorf("db-pools.csv:\n%s\nwant\n%s", got, want)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "pods/https:quader-api-1:8081/proxy/metrics"); n != 1 {
		t.Errorf("quader-api-1's metrics were read %d times, want once", n)
	}
	if n := strings.Count(string(raw), "pods/https:quader-worker-1:8081/proxy/metrics"); n != 2 {
		t.Errorf("quader-worker-1's metrics were read %d times, want twice (failed, then read)", n)
	}
	for _, f := range []string{".db-connections.csv", ".db-pools.csv", ".db-pools.seen"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("stop left %s", f)
		}
	}
}
