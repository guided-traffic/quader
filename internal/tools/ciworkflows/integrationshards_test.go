package ciworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// The integration packages run in the shards of the job integration of Test and Release, each with a stack of its own, and the
// Makefile places every package by a rule on its name (Q-ARCH-544, Q-ARCH-565; docs/developer/build-and-test.md). A package the
// rule placed nowhere would never run in CI, one it placed twice would cost its minutes twice, and a shard the coverage evidence
// does not wait for would drop the requirements only its report proves. This test reads the matrix, asks the Makefile for the
// packages of each shard and of none, and reads the list of reports the evidence requires.

// shardPackages is what `make test-integration SHARD=<shard>` would run, and every integration package, from the Makefile.
func shardPackages(printer, shard string) (pkgs, all []string, err error) {
	cmd := exec.Command("make", "-s", "-f", "Makefile", "-f", printer, "ciworkflows-print-shard", "SHARD="+shard)
	cmd.Dir = repoRoot
	// A run under `make test` passes its own flags and level down; the child is a make of its own.
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "MAKEFLAGS" && name != "MFLAGS" && name != "MAKELEVEL" && name != "SHARD" {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, &makeError{err: err, out: string(out)}
	}
	for _, l := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(l, "pkgs "); ok {
			pkgs = strings.Fields(rest)
		}
		if rest, ok := strings.CutPrefix(l, "all "); ok {
			all = strings.Fields(rest)
		}
	}
	return pkgs, all, nil
}

type makeError struct {
	err error
	out string
}

func (e *makeError) Error() string { return e.err.Error() + ": " + e.out }

func TestEveryIntegrationPackageRunsInExactlyOneShard(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not on PATH")
	}
	w := loadWorkflows(t)["release.yml"]
	j, ok := w.Jobs["integration"]
	if !ok {
		t.Fatal("release.yml has no job integration")
	}
	var strategy struct {
		Matrix struct {
			Shard []string `yaml:"shard"`
		} `yaml:"matrix"`
	}
	if err := j.Strategy.Decode(&strategy); err != nil {
		t.Fatalf("the strategy of job integration: %v", err)
	}
	shards := strategy.Matrix.Shard
	if len(shards) < 2 {
		t.Fatalf("job integration runs the shards %v; want a matrix of shards", shards)
	}

	printer := filepath.Join(t.TempDir(), "print.mk")
	if err := os.WriteFile(printer, []byte("ciworkflows-print-shard:\n\t@echo 'pkgs $(INTEGRATION_PKGS)'\n\t@echo 'all $(INTEGRATION_ALL)'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// One letter past the last shard is no shard: the Makefile must refuse it rather than run something.
	beyond := string(rune(shards[len(shards)-1][0] + 1))
	type answer struct {
		pkgs, all []string
		err       error
	}
	answers := make(map[string]answer, len(shards)+1)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range append(slices.Clone(shards), beyond) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pkgs, all, err := shardPackages(printer, s)
			mu.Lock()
			answers[s] = answer{pkgs, all, err}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if answers[beyond].err == nil {
		t.Errorf("make test-integration SHARD=%s is accepted; the matrix ends at %s", beyond, shards[len(shards)-1])
	}
	var all []string
	where := map[string][]string{}
	for _, s := range shards {
		a := answers[s]
		if a.err != nil {
			t.Fatalf("make test-integration SHARD=%s: %v", s, a.err)
		}
		if len(a.pkgs) == 0 {
			t.Errorf("shard %s runs no package", s)
		}
		all = a.all
		for _, p := range a.pkgs {
			where[p] = append(where[p], s)
		}
	}
	if len(all) < 10 {
		t.Fatalf("only %d integration packages; the Makefile is read wrongly", len(all))
	}
	for _, p := range all {
		if got := where[p]; len(got) != 1 {
			t.Errorf("package %s runs in the shards %v; want exactly one", p, got)
		}
		delete(where, p)
	}
	for p, s := range where {
		t.Errorf("shard %v runs %s, which is not an integration package", s, p)
	}

	// The coverage evidence requires the report of every shard, and of no other.
	var check string
	for _, s := range w.Jobs["coverage-evidence"].Steps {
		if strings.Contains(s.Run, "test-integration-") {
			check = s.Run
		}
	}
	var reported []string
	for _, m := range regexp.MustCompile(`\btest-integration-([a-z])\b`).FindAllStringSubmatch(check, -1) {
		reported = append(reported, m[1])
	}
	slices.Sort(reported)
	want := slices.Sorted(slices.Values(shards))
	if !slices.Equal(reported, want) {
		t.Errorf("the coverage evidence requires the reports of the shards %v; the matrix runs %v", reported, want)
	}

	// A step that runs on one shard names a shard of the matrix.
	for _, s := range j.Steps {
		for _, m := range regexp.MustCompile(`matrix\.shard == '([^']*)'`).FindAllStringSubmatch(s.If, -1) {
			if !slices.Contains(shards, m[1]) {
				t.Errorf("step %q runs on shard %s, which the matrix does not run", s.Name, m[1])
			}
		}
	}
}
