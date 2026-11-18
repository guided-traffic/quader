package app

import (
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

func TestMemoryLimit(t *testing.T) {
	before := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(before) })

	t.Run("R-HA-036 without GOMEMLIMIT the Go memory limit is runtime.memory_limit_ratio times runtime.memory_limit_bytes", func(t *testing.T) {
		debug.SetMemoryLimit(math.MaxInt64)
		l, src := applyMemoryLimit(config.Runtime{MemoryLimitRatio: 0.85, MemoryLimitBytes: 1 << 30}, nil)
		ratio := 0.85
		if want := int64(ratio * (1 << 30)); l != want || debug.SetMemoryLimit(-1) != want || src != "runtime.memory_limit_bytes" {
			t.Fatalf("limit %d from %q, runtime %d", l, src, debug.SetMemoryLimit(-1))
		}
	})
	t.Run("R-HA-036 an explicit GOMEMLIMIT wins and is left as the runtime read it", func(t *testing.T) {
		debug.SetMemoryLimit(300 << 20)
		l, src := applyMemoryLimit(config.Runtime{MemoryLimitRatio: 0.85, MemoryLimitBytes: 1 << 30}, []string{"GOMEMLIMIT=300MiB"})
		if l != 300<<20 || debug.SetMemoryLimit(-1) != 300<<20 || src != "GOMEMLIMIT" {
			t.Fatalf("limit %d from %q", l, src)
		}
		debug.SetMemoryLimit(math.MaxInt64)
		if l, _ := applyMemoryLimit(config.Runtime{MemoryLimitRatio: 0.85, MemoryLimitBytes: 1 << 30}, []string{"GOMEMLIMIT=off"}); l != 0 {
			t.Fatalf("GOMEMLIMIT=off gave a limit of %d", l)
		}
	})
	t.Run("R-HA-036 without a known container limit nothing is set", func(t *testing.T) {
		debug.SetMemoryLimit(math.MaxInt64)
		if l, src := applyMemoryLimit(config.Runtime{MemoryLimitRatio: 0.85}, nil); l != 0 || src != "" || debug.SetMemoryLimit(-1) != math.MaxInt64 {
			t.Fatalf("limit %d from %q", l, src)
		}
	})
	t.Run("Q-ARCH-500 the kernel gate's budget is the container's limit less the Go memory limit, read from the replica's own cgroup", func(t *testing.T) {
		root := t.TempDir()
		for p, s := range map[string]string{
			"proc/self/cgroup":          "0::/\n",
			"sys/fs/cgroup/memory.max":  "1073741824\n",
			"sys/fs/cgroup/memory.stat": "anon 1\nsock 4096\nkernel 1024\n",
		} {
			p = filepath.Join(root, p)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		a := &App{cfg: &config.Config{}, o: Options{CgroupRoot: root}}
		a.cfg.Runtime.MemoryLimitBytes = 1 << 30
		goLimit := int64(912680550) // 0.85 of 1 GiB, the dev profile's packages pod
		read, budget, off := a.kernelGate(goLimit)
		if read == nil || off != "" || budget != 1<<30-goLimit {
			t.Fatalf("reader %v, budget %d, off %q; want a reader and %d", read != nil, budget, off, 1<<30-goLimit)
		}
		if n, err := read(); n != 5120 || err != nil {
			t.Fatalf("read %d, %v", n, err)
		}
		if read, _, off := a.kernelGate(1 << 30); read != nil || off == "" {
			t.Fatal("a Go limit that leaves the kernel nothing gave a kernel gate")
		}
		a.cfg.Runtime.MemoryLimitBytes = 256 << 20
		if read, _, off := a.kernelGate(goLimit / 4); read != nil || !strings.Contains(off, "not the replica's own") {
			t.Fatalf("a cgroup with another limit gave a kernel gate (%q)", off)
		}
	})
	t.Run("R-ARCH-005 a request may hold the download buffer, the object store's receive window and the overhead", func(t *testing.T) {
		c := &config.Config{}
		c.Storage.Download = config.StorageDownload{BufferSize: 256 << 10, HoldBack: 64 << 10}
		c.Storage.S3.HTTP.ReceiveWindow = 256 << 10
		if got := requestMemory(c); got != 640<<10 {
			t.Fatalf("request memory %d, want 640 KiB", got)
		}
	})
}
