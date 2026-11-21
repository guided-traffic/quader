//go:build !linux

package scanner

// NotDumpable does nothing outside Linux (Q-VULN-203): the flag is the Linux kernel's, and a scanner runs on Linux only (its pod, the
// integration stack's replica on a developer's machine excepted, which runs no analyser of an image's choosing).
func NotDumpable() error { return nil }

// Dumpable reports that the process is dumpable: outside Linux the flag does not exist.
func Dumpable() (bool, error) { return true, nil }
