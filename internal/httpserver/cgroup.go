package httpserver

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// KernelMemory returns a reader of the memory the kernel charges to the
// replica's cgroup beside the Go runtime (Q-ARCH-500): the socket buffers of its
// connections (`sock` of memory.stat) and the kernel's own memory (`kernel`, or
// on a kernel without that line its parts). root is "/" outside tests.
//
// The reader is returned only when the replica's cgroup v2 has the memory limit
// the replica was configured with (runtime.memory_limit_bytes, which the chart
// takes from the container's limit): a cgroup with another limit is not the
// replica's own, such as the cgroup of a CI job that runs several replicas, and
// its sockets are not the replica's. The error says why there is no reader.
func KernelMemory(root string, limit int64) (func() (int64, error), error) {
	if limit <= 0 {
		return nil, errors.New("runtime.memory_limit_bytes is not set")
	}
	b, err := os.ReadFile(filepath.Join(root, "proc/self/cgroup")) //nolint:gosec // G304: a fixed path below root, which is "/" outside tests
	if err != nil {
		return nil, fmt.Errorf("no cgroup: %w", err)
	}
	var rel string
	found := false
	for line := range strings.SplitSeq(string(b), "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok {
			rel, found = p, true
			break
		}
	}
	if !found {
		return nil, errors.New("not in a cgroup v2")
	}
	dir := filepath.Join(root, "sys/fs/cgroup", filepath.Clean("/"+rel))
	m, err := os.ReadFile(filepath.Join(dir, "memory.max")) //nolint:gosec // G304: the replica's own cgroup, named by the kernel in /proc/self/cgroup
	if err != nil {
		return nil, fmt.Errorf("the cgroup has no memory controller: %w", err)
	}
	if got := strings.TrimSpace(string(m)); got != strconv.FormatInt(limit, 10) {
		return nil, fmt.Errorf("the cgroup's memory.max %s is not runtime.memory_limit_bytes %d, so the cgroup is not the replica's own", got, limit)
	}
	stat := filepath.Join(dir, "memory.stat")
	read := func() (int64, error) {
		b, err := os.ReadFile(stat) //nolint:gosec // G304: the replica's own cgroup, named by the kernel in /proc/self/cgroup
		if err != nil {
			return 0, err
		}
		return kernelBytes(b)
	}
	if _, err := read(); err != nil {
		return nil, err
	}
	return read, nil
}

// kernelBytes is `sock` plus `kernel` of a memory.stat, or `sock` plus the
// parts of `kernel` where that line is missing.
func kernelBytes(stat []byte) (int64, error) {
	var sock, kernel, parts int64
	haveSock, haveKernel := false, false
	sc := bufio.NewScanner(bytes.NewReader(stat))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "sock":
			sock, haveSock = n, true
		case "kernel":
			kernel, haveKernel = n, true
		case "kernel_stack", "pagetables", "sec_pagetables", "percpu", "slab", "vmalloc":
			// the parts of `kernel` on a kernel that does not write that line itself
			parts += n
		}
	}
	if !haveSock {
		return 0, errors.New("memory.stat has no sock line")
	}
	if !haveKernel {
		kernel = parts
	}
	return sock + kernel, nil
}
