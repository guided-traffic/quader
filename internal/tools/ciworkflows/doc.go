// Package ciworkflows holds the checks of the CI files under .github that are not covered by actionlint: every OS package is
// installed through .github/scripts/apt-install.sh, whose bounds keep a package index that does not answer from hanging a job
// (Test and Release 44384406557: the gating job chart waited 19 minutes on the Ubuntu archive and the release was skipped),
// and the steps of the jobs that gate Semantic Release that download something carry a step timeout. The tests run the
// script with its commands stubbed, without root and without the network. docs/developer/releasing.md#os-packages-in-the-jobs
// describes the rules. The decision of .github/actions/select-leg (which leg of "HA and Performance (nightly)" runs, and whether a
// scheduled run defers to a full run of the last hours) runs the same way, with curl stubbed:
// docs/developer/releasing.md#a-scheduled-run-after-a-full-run.
//
// What does not change between runs comes from a cache (F-156, F-158; docs/developer/releasing.md#what-the-jobs-fetch-and-what-is-cached):
// every job that builds Go restores the complete module cache and downloads what is missing in a bounded step before it builds,
// the Go tools come from .github/actions/go-tool, the client images of the acceptance suites come from
// .github/actions/client-images in every leg whose sets start client pods (read from deploy/dev/e2e.sh), and the kind legs load the
// Quader image of their run. The scripts behind them, client-images.sh and go-mod-download.sh, run here with go and docker stubbed.
package ciworkflows
