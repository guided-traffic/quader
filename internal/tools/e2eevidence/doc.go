// Package e2eevidence holds the tests of the cgroup readers of deploy/dev/e2e-evidence.sh, the stall recorder of the e2e
// sets (Q-HA-305): the search for the cgroup parent of the job's Docker daemon and the sampler of the machine's cgroups run
// with bash against cgroup trees fabricated in a temporary directory, in the layout of a `self-hosted` runner (the daemon's
// containers under the top-level `docker`) and of a `large` runner (under `kubepods/burstable/pod<uid>/docker`), and the
// line of README.txt that sums up the runner pod's memory runs against a fabricated pod-memory.csv. No Docker, kind or cgroup
// file system is needed. docs/developer/e2e.md#reading-a-stall describes the rows.
package e2eevidence
