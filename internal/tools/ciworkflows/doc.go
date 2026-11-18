// Package ciworkflows holds the checks of the CI files under .github that are not covered by actionlint: every OS package is
// installed through .github/scripts/apt-install.sh, whose bounds keep a package index that does not answer from hanging a job
// (Test and Release 44384406557: the gating job chart waited 19 minutes on the Ubuntu archive and the release was skipped),
// and the steps of the jobs that gate Semantic Release that download something carry a step timeout. The tests run the
// script with its commands stubbed, without root and without the network. docs/developer/releasing.md#os-packages-in-the-jobs
// describes the rules.
package ciworkflows
