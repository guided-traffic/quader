//go:build linux

package scanner

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// dumpableHelper names the mode of the child process of TestR_VULN_036_AChildOfTheSameUserCannotOpenTheMemoryOfANonDumpableProcess: "notdump" makes itself not dumpable, "dump" stays as it is.
const dumpableHelper = "QUADER_TEST_DUMPABLE_HELPER"

// TestR_VULN_036_DumpableHelper is the child of the test below, started by it with dumpableHelper set; run alone it does nothing.
func TestR_VULN_036_DumpableHelper(t *testing.T) {
	mode := os.Getenv(dumpableHelper)
	if mode == "" {
		t.Skip("the child of TestR_VULN_036_AChildOfTheSameUserCannotOpenTheMemoryOfANonDumpableProcess")
	}
	if mode == "notdump" {
		if err := NotDumpable(); err != nil {
			t.Fatal(err)
		}
		if d, err := Dumpable(); err != nil || d {
			t.Fatalf("still dumpable: %v %v", d, err)
		}
	}
	_, _ = os.Stdout.WriteString("ready\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n') // until the parent closes stdin
}

// Q-VULN-203, Q-VULN-211: a process of the same user without CAP_SYS_PTRACE cannot open the memory of a process that made itself not
// dumpable, and can open it while it is dumpable. The scanner's analysers are such processes of its user; the test runs the other direction
// (the parent opens its child's memory), because a node's Yama scope 1 refuses a child's access to its parent whatever the flag says and
// would hide what the flag does. About 0.1 s; skipped where the test holds CAP_SYS_PTRACE (root) or the node refuses even a dumpable child.
func TestR_VULN_036_AChildOfTheSameUserCannotOpenTheMemoryOfANonDumpableProcess(t *testing.T) {
	if os.Getenv(dumpableHelper) != "" {
		t.Skip("this is the helper's process")
	}
	if holdsPtrace(t) {
		t.Skip("the test process holds CAP_SYS_PTRACE, which passes the flag; the scanner's pod drops it (R-HA-033)")
	}
	open := func(mode string) error {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestR_VULN_036_DumpableHelper$", "-test.count=1") //nolint:gosec // the test binary itself
		cmd.Env = append(os.Environ(), dumpableHelper+"="+mode)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = stdin.Close()
			_ = cmd.Wait()
		}()
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "ready" {
			t.Fatalf("the helper did not start: %q %v", line, err)
		}
		f, err := os.Open("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/mem")
		if err == nil {
			_ = f.Close()
		}
		return err
	}
	if err := open("dump"); err != nil {
		t.Skipf("this node refuses a parent the memory of its dumpable child (%v): the flag's effect cannot be shown here", err)
	}
	if err := open("notdump"); err == nil {
		t.Fatal("the memory of a process that made itself not dumpable was opened by a process of the same user")
	}
}

// holdsPtrace reports whether the test process holds CAP_SYS_PTRACE (bit 19 of CapEff).
func holdsPtrace(t *testing.T) bool {
	t.Helper()
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "CapEff:"); ok {
			caps, err := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
			return err == nil && caps&(1<<19) != 0
		}
	}
	return false
}
