//go:build linux

package scanner

import "golang.org/x/sys/unix"

// NotDumpable makes the process not dumpable (Q-VULN-203): the kernel then requires CAP_SYS_PTRACE, which the scanner's pod drops
// (R-HA-033), to attach to the process or to open its /proc/<pid>/mem, whatever the node's ptrace scope says. The analysers the scanner
// starts run as its user; without the flag one that an image took over could read the scanner's pair and the scan tokens of the calls in
// flight from its memory, or change the answers of every later call. A child's execve makes the child dumpable again, so the flag guards
// the scanner and nothing else. The role scanner calls it at its start, before it reads a pair or opens a listener.
func NotDumpable() error { return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) }

// Dumpable reports whether the process is dumpable.
func Dumpable() (bool, error) {
	v, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	return v != 0, err
}
