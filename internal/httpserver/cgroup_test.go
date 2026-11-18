package httpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCgroup writes /proc/self/cgroup and the cgroup's memory.max and
// memory.stat under a temporary root and returns the root.
func fakeCgroup(t *testing.T, self, rel, memMax, stat string) string {
	t.Helper()
	root := t.TempDir()
	write := func(p, s string) {
		p = filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("proc/self/cgroup", self)
	if memMax != "" {
		write(filepath.Join("sys/fs/cgroup", rel, "memory.max"), memMax+"\n")
	}
	write(filepath.Join("sys/fs/cgroup", rel, "memory.stat"), stat)
	return root
}

func TestKernelMemory(t *testing.T) {
	// memory.stat of the forced reproduction of Q-ARCH-500, the second before the OOM kill (sizes in bytes).
	const stat = "anon 649068544\nfile 2097152\nkernel 3145728\nkernel_stack 2621440\npagetables 262144\nsock 401604608\nshmem 0\n"

	t.Run("Q-ARCH-500 the kernel's memory is sock plus kernel of the replica's own cgroup v2", func(t *testing.T) {
		root := fakeCgroup(t, "0::/\n", "", "1073741824", stat)
		read, err := KernelMemory(root, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := read(); err != nil || n != 401604608+3145728 {
			t.Fatalf("read %d, %v; want sock + kernel", n, err)
		}
	})

	t.Run("Q-ARCH-500 a cgroup below the root is found by its path, and a kernel without the kernel line is summed from its parts", func(t *testing.T) {
		rel := "/kubepods/burstable/pod1/cri-containerd-abc.scope"
		root := fakeCgroup(t, "0::"+rel+"\n", rel, "1073741824", "anon 1\nsock 1000\nkernel_stack 10\npagetables 20\nslab 30\npercpu 40\nvmalloc 50\nfile 99\n")
		read, err := KernelMemory(root, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := read(); n != 1150 {
			t.Fatalf("read %d, want 1000 + 150", n)
		}
	})

	t.Run("Q-ARCH-500 no reader where the cgroup is not the replica's own or cannot be read", func(t *testing.T) {
		for _, c := range []struct {
			name, self, memMax, stat, want string
			limit                          int64
		}{
			{"another limit, a CI job's cgroup with several replicas", "0::/\n", "8589934592", stat, "is not runtime.memory_limit_bytes", 1 << 30},
			{"no limit at all", "0::/\n", "max", stat, "is not runtime.memory_limit_bytes", 1 << 30},
			{"no configured limit", "0::/\n", "1073741824", stat, "runtime.memory_limit_bytes is not set", 0},
			{"cgroup v1 only", "4:memory:/docker/abc\n", "1073741824", stat, "not in a cgroup v2", 1 << 30},
			{"no memory controller", "0::/\n", "", stat, "no memory controller", 1 << 30},
			{"no sock line", "0::/\n", "1073741824", "anon 1\n", "no sock line", 1 << 30},
		} {
			root := fakeCgroup(t, c.self, "", c.memMax, c.stat)
			read, err := KernelMemory(root, c.limit)
			if read != nil || err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: reader %v, error %v; want an error with %q", c.name, read != nil, err, c.want)
			}
		}
		if _, err := KernelMemory(t.TempDir(), 1<<30); err == nil {
			t.Error("a root without /proc gave a reader")
		}
	})
}
