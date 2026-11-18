package app

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// requestOverhead is what a request holds besides a download's buffer and the
// object store's receive window: the connection, the goroutine stacks, the
// request and the object-store call. It is charged to every admitted request
// (Q-ARCH-328). 100 downloads of 1, 2 and 5 MiB held open measured 110 KiB of
// heap beside the buffer and the window and 73 KiB of stacks each, 636 to
// 659 KiB in all against the 640 KiB charged.
const requestOverhead = 128 << 10

// applyMemoryLimit applies R-HA-036: unless GOMEMLIMIT is set, the Go runtime's
// limit is runtime.memory_limit_ratio × runtime.memory_limit_bytes. It returns
// the limit in effect, 0 when none is known, and where it came from.
func applyMemoryLimit(rt config.Runtime, environ []string) (int64, string) {
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, "GOMEMLIMIT="); ok && v != "" {
			if l := debug.SetMemoryLimit(-1); l != math.MaxInt64 {
				return l, "GOMEMLIMIT"
			}
			return 0, "GOMEMLIMIT"
		}
	}
	if rt.MemoryLimitBytes <= 0 {
		return 0, ""
	}
	l := int64(rt.MemoryLimitRatio * float64(rt.MemoryLimitBytes))
	debug.SetMemoryLimit(l)
	return l, "runtime.memory_limit_bytes"
}

// cacheBudgets is what the per-replica caches hold when they are full
// (Q-ARCH-243): the rendition cache, the two metadata caches of the proxy and
// virtual engines and the cache of pass-through answers. The guard charges the
// bytes the caches hold now (cacheBytes); the budgets are the most they can
// hold, which the chart's check of the slots and the warning at start use.
func cacheBudgets(c *config.Config) int64 {
	return int64(c.Cache.RenditionBytes) + int64(c.Proxy.MetadataCacheBytes) + int64(c.Proxy.MergedMetadataCacheBytes) + int64(c.Proxy.QueryCacheBytes)
}

// cacheBytes is the weighted size of the replica's caches now.
func (a *App) cacheBytes() int64 {
	r := a.Caches()
	if r == nil {
		return 0
	}
	var n int64
	for _, s := range r.Stats() {
		n += s.Bytes
	}
	return n
}

// requestMemory is what a download of a large object holds, the unit of the
// memory slots (Q-ARCH-240): the buffer of a verified download, the object
// store's receive window and the overhead.
func requestMemory(c *config.Config) int64 {
	return blob.DownloadBufferBytes(c.Storage.Download) + int64(c.Storage.S3.HTTP.ReceiveWindow) + requestOverhead
}

// kernelGate returns the reader of the kernel's memory of the replica's cgroup
// and its budget, the container's limit less the Go memory limit (Q-ARCH-500),
// or why there is no kernel gate.
func (a *App) kernelGate(goLimit int64) (func() (int64, error), int64, string) {
	c := int64(a.cfg.Runtime.MemoryLimitBytes)
	if c > 0 && goLimit >= c {
		return nil, 0, "the Go memory limit leaves the kernel nothing of runtime.memory_limit_bytes"
	}
	read, err := httpserver.KernelMemory(a.root(), c)
	if err != nil {
		return nil, 0, err.Error()
	}
	return read, c - goLimit, ""
}

// defaultTCPWmemMax is the most Linux's autotuning grows a send buffer to by
// default (the third value of net.ipv4.tcp_wmem), used where it cannot be read.
const defaultTCPWmemMax = 4 << 20

// kernelRequestMemory is what one request may make the kernel hold
// (Q-ARCH-500): the send buffer of its connection, which the kernel doubles from
// server.send_buffer for its bookkeeping, or with autotuning the most the buffer
// grows to.
func kernelRequestMemory(c *config.Config, root string) int64 {
	if n := int64(c.Server.SendBuffer); n > 0 {
		return 2 * n
	}
	b, err := os.ReadFile(filepath.Join(root, "proc/sys/net/ipv4/tcp_wmem")) //nolint:gosec // G304: a fixed path below root, which is "/" outside tests
	if err != nil {
		return defaultTCPWmemMax
	}
	f := strings.Fields(string(b))
	if len(f) != 3 {
		return defaultTCPWmemMax
	}
	n, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil || n <= 0 {
		return defaultTCPWmemMax
	}
	return n
}

// root is where /proc and /sys are read: "/" outside tests.
func (a *App) root() string {
	if a.o.CgroupRoot == "" {
		return "/"
	}
	return a.o.CgroupRoot
}

// kernelReadFailed logs the first failed reading of the kernel's memory; the
// gate keeps its state until a reading succeeds.
func (a *App) kernelReadFailed() func(error) {
	var once sync.Once
	return func(err error) {
		once.Do(func() { a.log.Warn("reading the kernel's memory of the replica's cgroup failed", "error", err) })
	}
}

// startMemoryGuard sets the Go memory limit and, on a process with client
// listeners, starts the guard of R-ARCH-005 step 4 that every endpoint's chain
// shares. Without a known limit the guard is nil and admits everything.
func (a *App) startMemoryGuard(ctx context.Context) *httpserver.MemoryGuard {
	limit, source := applyMemoryLimit(a.cfg.Runtime, a.o.Environ)
	if len(a.eps) == 0 {
		if limit > 0 {
			a.log.Info("memory limit", "go_memory_limit_bytes", limit, "source", source)
		}
		return nil
	}
	per, budgets := requestMemory(a.cfg), cacheBudgets(a.cfg)
	kernel, kernelBudget, kernelOff := a.kernelGate(limit)
	kernelPer := kernelRequestMemory(a.cfg, a.root())
	g := httpserver.NewMemoryGuard(httpserver.MemoryGuardConfig{
		Limit: limit, RequestBytes: per, Overhead: requestOverhead, Held: a.cacheBytes,
		Kernel: kernel, KernelBudget: kernelBudget, KernelRequestBytes: kernelPer,
		OnSlots: a.tele.metrics.MemorySlots,
		OnChange: func(gate string, closed bool, n uint64) {
			what, mark := "the live heap", limit
			if gate == httpserver.ShedKernel {
				what, mark = "the kernel's memory of the replica (socket buffers and its own)", kernelBudget
			}
			if closed {
				a.log.Warn(what+" reached the high-water mark: new requests answer 503 server_busy until it falls below the low-water mark",
					"gate", gate, "bytes", n, "budget_bytes", mark)
				return
			}
			a.log.Info(what+" fell below the low-water mark: requests are admitted again", "gate", gate, "bytes", n, "budget_bytes", mark)
		},
		OnError: a.kernelReadFailed(),
	})
	a.tele.metrics.MemorySlots(g.Slots())
	if g == nil {
		a.log.Warn("no memory limit is known (runtime.memory_limit_bytes, GOMEMLIMIT): the client listeners admit requests up to max_in_flight only")
		return nil
	}
	a.tele.metrics.MemorySlotsInUse(g.InUse)
	if float64(budgets) >= 0.8*float64(limit) {
		a.log.Error("the caches' byte budgets take the memory the requests need once the caches are full: one request at a time is then admitted; lower cache.rendition_bytes, proxy.metadata_cache_bytes, proxy.merged_metadata_cache_bytes and proxy.query_cache_bytes or raise the memory limit",
			"cache_budget_bytes", budgets, "go_memory_limit_bytes", limit)
	}
	a.log.Info("memory limit", "go_memory_limit_bytes", limit, "source", source, "memory_slots", g.Slots(), "request_bytes", per, "cache_budget_bytes", budgets,
		"kernel_budget_bytes", kernelBudget, "kernel_request_bytes", kernelPer)
	if kernelOff != "" {
		a.log.Info("no kernel gate: the socket buffers of the replica's connections are not bounded by the memory guard", "reason", kernelOff)
	}
	a.bg.Add(1)
	go func() {
		defer a.bg.Done()
		g.Run(ctx, httpserver.MemorySampleInterval)
	}()
	return g
}
