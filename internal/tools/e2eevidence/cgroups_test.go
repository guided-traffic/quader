package e2eevidence

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot is the repository root, three levels above this package.
var repoRoot = filepath.Join("..", "..", "..")

// The time every row carries: date is stubbed, so that the rows can be compared whole and macOS date (no %N) does not matter.
const stamp = "1795000000000"

// The containers of the fabricated trees. Their first twelve characters are what the cgd: rows carry.
var (
	ownNode   = container("0a36835cca9f") // the first kind node of the job's own daemon: what CGFIND looks for
	ownK6     = container("40969b0f89a2")
	neighbour = container("ec39c748fad2") // a container of another job's daemon in the top-level docker
	runnerCtr = container("5cceacfbb42d") // the runner pod's own containers (runner, dind), not the daemon's
	dindCtr   = container("2a751edd4def")
)

const (
	podUID   = "3f1c9a2e-7d4b-4c55-9e1a-2b6f8d0c4e71"
	otherPod = "kubepods/burstable/pod9b0e4c11-5a3d-4f2e-8c7b-1d2e3f4a5b6c"
	ownPod   = "kubepods/burstable/pod" + podUID
)

func container(prefix string) string { return prefix + strings.Repeat("0", 64-len(prefix)) }

// programs returns the bash programs that e2e-evidence.sh reads into variables from quoted heredocs (`<<'EOF'`), as the
// script hands them to `bash -c`: read strips the final newline.
func programs(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "dev", "e2e-evidence.sh"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(?ms)^read -r -d '' (\w+) <<'EOF'\n(.*?)\nEOF$`).FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = m[2]
	}
	for _, name := range []string{"READERS", "CGFIND", "CGSAMPLER"} {
		if out[name] == "" {
			t.Fatalf("e2e-evidence.sh has no program %s", name)
		}
	}
	return out
}

// cgroup writes the files the sampler reads into dir: cpu.stat with usage, the three pressure files (the same values
// everywhere) and, unless empty, cpu.weight.
func cgroup(t *testing.T, dir string, usage int, weight string) {
	t.Helper()
	files := map[string]string{
		"cpu.stat":        fmt.Sprintf("usage_usec %d\nuser_usec 1\nsystem_usec 1\n", usage),
		"cpu.pressure":    "some avg10=1.50 avg60=0.00 avg300=0.00 total=1001\nfull avg10=0.25 avg60=0.00 avg300=0.00 total=501\n",
		"memory.pressure": "some avg10=0.00 avg60=0.00 avg300=0.00 total=2002\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1002\n",
		"io.pressure":     "some avg10=0.10 avg60=0.00 avg300=0.00 total=3003\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1503\n",
	}
	if weight != "" {
		files["cpu.weight"] = weight + "\n"
	}
	write(t, dir, files)
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// machine fabricates the cgroup root of a runner machine as the job's Docker daemon sees it: kubepods with two pods and the
// top-level docker with a neighbour's container, and the job's own containers where layout puts them.
func machine(t *testing.T, layout string) string {
	t.Helper()
	root := t.TempDir()
	cgroup(t, filepath.Join(root, "kubepods"), 9000000, "1250")
	cgroup(t, filepath.Join(root, otherPod), 1000000, "")
	cgroup(t, filepath.Join(root, "docker"), 500, "100")
	cgroup(t, filepath.Join(root, "docker", neighbour), 300, "100")
	cgroup(t, filepath.Join(root, "system.slice"), 70, "100")
	switch layout {
	case "self-hosted":
		cgroup(t, filepath.Join(root, "docker", ownNode), 100, "100")
		cgroup(t, filepath.Join(root, "docker", ownK6), 50, "100")
		// the kind node's own cgroups, as the kubelet inside it creates them
		cgroup(t, filepath.Join(root, "docker", ownNode, "kubelet.slice", "kubelet-kubepods.slice"), 90, "")
	case "large":
		pod := filepath.Join(root, ownPod)
		cgroup(t, pod, 4000000, "")
		write(t, pod, map[string]string{
			"memory.current":      "12884901888\n",
			"memory.peak":         "17179869184\n",
			"memory.max":          "30064771072\n",
			"memory.events":       "low 0\nhigh 0\nmax 7\noom 1\noom_kill 2\noom_group_kill 0\n",
			"memory.events.local": "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\n",
		})
		cgroup(t, filepath.Join(pod, runnerCtr), 200000, "100")
		cgroup(t, filepath.Join(pod, dindCtr), 300000, "100")
		cgroup(t, filepath.Join(pod, "docker"), 3500000, "800")
		write(t, filepath.Join(pod, "docker"), map[string]string{"memory.current": "10737418240\n"})
		cgroup(t, filepath.Join(pod, "docker", ownNode), 3000000, "100")
		cgroup(t, filepath.Join(pod, "docker", ownK6), 400000, "100")
		cgroup(t, filepath.Join(pod, "docker", ownNode, "kubelet.slice", "kubelet-kubepods.slice"), 2900000, "")
	}
	return root
}

// run runs program with bash, date stubbed, and returns its standard output.
func run(t *testing.T, program string, args ...string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	stubs := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubs, "date"), []byte("#!/bin/sh\necho "+stamp+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, append([]string{"-c", program, "_"}, args...)...)
	cmd.Env = []string{"PATH=" + stubs + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + stubs}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, stderr.String())
	}
	return string(out)
}

func find(t *testing.T, root, id string) string {
	t.Helper()
	return strings.TrimSpace(run(t, programs(t)["CGFIND"], root, id))
}

// sample runs the sampler for one or two samples (it ends after two seconds, one sample a second, a container and memory
// sample every sample) with the daemon's parent and pod as the script passes them, and returns its rows by source and the
// rows of the pod memory file.
func sample(t *testing.T, root, parent, pod string) (rows map[string][]string, samples int, mem []string) {
	t.Helper()
	p := programs(t)
	work := t.TempDir()
	memfile := filepath.Join(work, "podmem.csv")
	samplesFile := filepath.Join(work, "samples.csv")
	// as the script starts it: the program text followed by a redirection, which binds to the program's last command
	if stray := run(t, p["READERS"]+"\n"+p["CGSAMPLER"]+" >>"+samplesFile, "1", filepath.Join(work, "stop"), "2", "1", root, parent, pod, memfile); stray != "" {
		t.Errorf("rows outside the samples file:\n%s", stray)
	}
	raw, err := os.ReadFile(samplesFile)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	rows = map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, ",")
		if len(f) != 26 {
			t.Fatalf("row of %d columns instead of the 26 of samples.csv: %q", len(f), line)
		}
		if f[0] != stamp {
			t.Fatalf("row without the sample's time: %q", line)
		}
		rows[f[1]] = append(rows[f[1]], line)
	}
	samples = len(rows["cg:docker"])
	if samples < 1 || samples > 2 {
		t.Fatalf("%d samples of cg:docker, want 1 or 2:\n%s", samples, out)
	}
	for src, r := range rows {
		if len(r) != samples {
			t.Errorf("%s: %d rows in %d samples", src, len(r), samples)
		}
	}
	if b, err := os.ReadFile(memfile); err == nil {
		mem = strings.Split(strings.TrimSpace(string(b)), "\n")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return rows, samples, mem
}

// cgRow is a row of a cgroup with the pressure values of cgroup(): the columns of the cg: rows as they were before the
// daemon's parent was followed.
func cgRow(source string, usage int, weight string, members int) string {
	return fmt.Sprintf("%s,%s,1.50,1001,0.25,501,0.00,2002,0.00,1002,,,,,,,,0.10,3003,0.00,1503,%d,,,%s,%d", stamp, source, usage, weight, members)
}

func cgdRow(id string, usage int) string {
	return fmt.Sprintf("%s,cgd:%s%s%d,,,,", stamp, id[:12], strings.Repeat(",", 20), usage)
}

func sources(rows map[string][]string) []string {
	var out []string
	for s := range rows {
		out = append(out, s)
	}
	return out
}

func wantRow(t *testing.T, rows map[string][]string, source, want string) {
	t.Helper()
	got := rows[source]
	if len(got) == 0 {
		t.Errorf("no %s row; sources: %v", source, sources(rows))
		return
	}
	if got[0] != want {
		t.Errorf("%s:\n got %s\nwant %s", source, got[0], want)
	}
}

func TestTheDaemonsParentIsFoundInsideTheRunnerPod(t *testing.T) {
	t.Parallel()
	root := machine(t, "large")
	want := "/" + ownPod + "/docker /" + ownPod + " 30064771072"
	if got := find(t, root, ownNode); got != want {
		t.Errorf("CGFIND: got %q, want %q", got, want)
	}
}

func TestTheDaemonsParentAtTheTopLevelIsNotInAPod(t *testing.T) {
	t.Parallel()
	root := machine(t, "self-hosted")
	if got := find(t, root, ownNode); got != "/docker - -" {
		t.Errorf("CGFIND: got %q, want %q", got, "/docker - -")
	}
}

func TestTheDaemonsParentOnTheSystemdDriversIsFoundByItsScope(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pod := "kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod3f1c9a2e_7d4b.slice"
	cgroup(t, filepath.Join(root, pod, "docker-"+ownNode+".scope"), 1, "100")
	write(t, filepath.Join(root, pod), map[string]string{"memory.max": "max\n"})
	want := "/" + pod + " /" + pod + " max"
	if got := find(t, root, ownNode); got != want {
		t.Errorf("CGFIND: got %q, want %q", got, want)
	}
}

func TestTheSearchForTheDaemonsParentStopsAtSixLevels(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cgroup(t, filepath.Join(root, "a", "b", "c", "d", "e", "f", ownNode), 1, "")
	if got := find(t, root, ownNode); got != "" {
		t.Errorf("CGFIND found a container seven levels down: %q", got)
	}
	cgroup(t, filepath.Join(root, "a", "b", "c", "d", "e", ownK6), 1, "")
	if got := find(t, root, ownK6); got != "/a/b/c/d/e - -" {
		t.Errorf("CGFIND six levels down: got %q", got)
	}
}

func TestTheSamplerOnALargeRunnerRecordsTheLegsOwnContainersAndThePodsMemory(t *testing.T) {
	t.Parallel()
	root := machine(t, "large")
	parent, pod, _ := strings.Cut(find(t, root, ownNode), " ")
	pod, _, _ = strings.Cut(pod, " ")
	rows, samples, mem := sample(t, root, parent, pod)

	// the top-level rows are as before: kubepods counts both pods, docker holds the neighbour only
	wantRow(t, rows, "cg:kubepods", cgRow("cg:kubepods", 9000000, "1250", 2))
	wantRow(t, rows, "cg:docker", cgRow("cg:docker", 500, "100", 1))
	wantRow(t, rows, "cg:system.slice", cgRow("cg:system.slice", 70, "100", 0))
	// the daemon's parent and its pod; the pod's members are its own two containers and the daemon's two
	wantRow(t, rows, "daemon:parent", cgRow("daemon:parent", 3500000, "800", 2))
	wantRow(t, rows, "daemon:pod", cgRow("daemon:pod", 4000000, "", 4))
	// the leg's own containers and the neighbour's, the runner pod's own containers not
	wantRow(t, rows, "cgd:"+ownNode[:12], cgdRow(ownNode, 3000000))
	wantRow(t, rows, "cgd:"+ownK6[:12], cgdRow(ownK6, 400000))
	wantRow(t, rows, "cgd:"+neighbour[:12], cgdRow(neighbour, 300))
	for _, id := range []string{runnerCtr, dindCtr} {
		if _, ok := rows["cgd:"+id[:12]]; ok {
			t.Errorf("a cgd: row for the runner pod's own container %s", id[:12])
		}
	}
	if len(rows) != 8 {
		t.Errorf("%d sources, want 8: %v", len(rows), sources(rows))
	}

	// one memory row per sample and one at the end
	if len(mem) != samples+1 {
		t.Fatalf("%d pod memory rows in %d samples, want %d: %q", len(mem), samples, samples+1, mem)
	}
	for _, r := range mem {
		if want := stamp + ",12884901888,17179869184,30064771072,1,2,0,10737418240"; r != want {
			t.Errorf("pod memory row:\n got %s\nwant %s", r, want)
		}
	}
}

func TestTheSamplerOnASelfHostedRunnerWritesTheRowsOfBefore(t *testing.T) {
	t.Parallel()
	root := machine(t, "self-hosted")
	rows, _, mem := sample(t, root, "/docker", "-")

	wantRow(t, rows, "cg:kubepods", cgRow("cg:kubepods", 9000000, "1250", 1))
	wantRow(t, rows, "cg:docker", cgRow("cg:docker", 500, "100", 3))
	wantRow(t, rows, "cgd:"+ownNode[:12], cgdRow(ownNode, 100))
	wantRow(t, rows, "cgd:"+ownK6[:12], cgdRow(ownK6, 50))
	wantRow(t, rows, "cgd:"+neighbour[:12], cgdRow(neighbour, 300))
	// the parent is the top-level docker: no daemon: rows, and each container once per sample (sample() counts them)
	if len(rows) != 6 {
		t.Errorf("%d sources, want 6: %v", len(rows), sources(rows))
	}
	if mem != nil {
		t.Errorf("pod memory rows without a pod: %q", mem)
	}
}

func TestTheSamplerWithoutTheDaemonsParentFallsBackToTheTopLevelDocker(t *testing.T) {
	t.Parallel()
	root := machine(t, "large")
	if got := find(t, root, container("ffffeeeedddd")); got != "" {
		t.Fatalf("CGFIND found a container that is not there: %q", got)
	}
	// the script passes "-" for what CGFIND did not print
	rows, _, mem := sample(t, root, "-", "-")
	wantRow(t, rows, "cg:docker", cgRow("cg:docker", 500, "100", 1))
	wantRow(t, rows, "cgd:"+neighbour[:12], cgdRow(neighbour, 300))
	if len(rows) != 4 {
		t.Errorf("%d sources, want cg:kubepods, cg:docker, cg:system.slice and the neighbour's cgd: row: %v", len(rows), sources(rows))
	}
	if mem != nil {
		t.Errorf("pod memory rows without a pod: %q", mem)
	}
}
