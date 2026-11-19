package e2eevidence

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A kernel that lacks a field of memory.stat leaves its column empty, and a pod without memory.stat leaves all six empty; the
// columns before them are as on a kernel that has them.
func TestThePodsMemoryWithoutAFieldOfMemoryStatLeavesItsColumnEmpty(t *testing.T) {
	t.Parallel()
	for stat, want := range map[string]string{
		"anon 8589934592\nfile 3221225472\nshmem 1073741824\nsock 16777216\n": "8589934592,3221225472,,1073741824,,16777216",
		"": ",,,,,",
	} {
		root := machine(t, "large")
		if stat == "" {
			if err := os.Remove(filepath.Join(root, ownPod, "memory.stat")); err != nil {
				t.Fatal(err)
			}
		} else {
			write(t, filepath.Join(root, ownPod), map[string]string{"memory.stat": stat})
		}
		_, _, mem := sample(t, root, "/"+ownPod+"/docker", "/"+ownPod)
		if len(mem) == 0 {
			t.Fatal("no pod memory row")
		}
		for _, r := range mem {
			if w := stamp + ",12884901888,17179869184,30064771072,1,2,0,10737418240," + want; r != w {
				t.Errorf("pod memory row:\n got %s\nwant %s", r, w)
			}
		}
	}
}

// podMemoryLine runs PODMEMLINE as the script's stop does, with the script's gib() before it, on a pod-memory.csv with the
// script's header and rows.
func podMemoryLine(t *testing.T, rows ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "dev", "e2e-evidence.sh"))
	if err != nil {
		t.Fatal(err)
	}
	gib := regexp.MustCompile(`(?ms)^gib\(\) \{\n.*?\n\}$`).FindString(string(b))
	if gib == "" {
		t.Fatal("e2e-evidence.sh has no gib()")
	}
	header := regexp.MustCompile(`(?m)^PODMEM_HEADER='(.*)'$`).FindStringSubmatch(string(b))
	if header == nil {
		t.Fatal("e2e-evidence.sh has no PODMEM_HEADER")
	}
	file := filepath.Join(t.TempDir(), "pod-memory.csv")
	if err := os.WriteFile(file, []byte(strings.Join(append([]string{header[1]}, rows...), "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(run(t, gib+"\n"+programs(t)["PODMEMLINE"], file))
}

// memRow is a row of pod-memory.csv in the order of PODMEM_HEADER, the sizes in GiB ("" for an empty column): the limit
// 28 GiB, oom 1, oom_kill 2, oom_local 0, the daemon's containers 18 GiB, file 5 GiB.
func memRow(cur, peak, anon, kernel, shmem, slabReclaimable, sock string) string {
	b := func(v string) string {
		if v == "" {
			return ""
		}
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err != nil {
			panic(err)
		}
		return fmt.Sprintf("%d", int64(f*(1<<30)))
	}
	return strings.Join([]string{stamp, b(cur), b(peak), "30064771072", "1", "2", "0", b("18"), b(anon), b("5"), b(kernel), b(shmem), b(slabReclaimable), b(sock)}, ",")
}

// The line of README.txt: the last row's values, and over every row the peak of the anonymous part and the peak of what cannot
// be given back (anon + shmem + sock + kernel - slab_reclaimable), which need not fall into the same row.
func TestThePodMemoryLineGivesThePeakOfTheAnonymousPart(t *testing.T) {
	t.Parallel()
	got := podMemoryLine(t,
		memRow("10", "12", "4", "1", "0.5", "0.25", "0"),
		memRow("26", "28", "7", "1", "1", "0.5", "0.125"), // the anonymous peak; 8.625 GiB that cannot be given back
		memRow("20", "28", "6", "2", "2", "0.5", "0"),     // 9.5 GiB that cannot be given back, the peak of that
	)
	want := "Pod memory         peak 28.0 GiB of a limit of 28.0 GiB, of that at most 7.0 GiB anonymous and at most 9.5 GiB that cannot be given back " +
		"(anon, shmem, sock, kernel less its reclaimable slab); at the end 20.0 GiB, of that the daemon's containers 18.0 GiB; " +
		"OOM at the pod's own limit 0, OOM events below the pod 1, OOM kills below the pod 2 (pods.csv names a killed kind pod)"
	if got != want {
		t.Errorf("README line:\n got %s\nwant %s", got, want)
	}
}

// A kernel without the field kernel: the anonymous peak is known, what cannot be given back is not; a row without memory.stat
// knows neither; a file without rows gives no line.
func TestThePodMemoryLineSaysWhatIsNotKnown(t *testing.T) {
	t.Parallel()
	got := podMemoryLine(t, memRow("10", "12", "4", "", "0.5", "0.25", "0"), memRow("11", "12", "5", "", "0.5", "0.25", "0"))
	if !strings.Contains(got, "of that at most 5.0 GiB anonymous and a part that cannot be given back not known (") {
		t.Errorf("README line of a kernel without the field kernel: %s", got)
	}
	got = podMemoryLine(t, memRow("10", "12", "", "", "", "", ""))
	if !strings.Contains(got, "of that an anonymous part not known and a part that cannot be given back not known (") {
		t.Errorf("README line of a row without memory.stat: %s", got)
	}
	if got := podMemoryLine(t); got != "" {
		t.Errorf("a line from a file without rows: %q", got)
	}
}
