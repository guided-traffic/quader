#!/usr/bin/env bash
# The report of the scan of the images the chart runs and Quader does not build (job upstream-image-scan of release.yml,
# Q-ARCH-596, H-1003, H-1109):
#
#   upstream-image-report.sh <label> <image> <trivy json report> [<label> <image> <report>]...
#
# For each image it writes the findings of its JSON report (the scan already kept only CRITICAL and HIGH with a fix and left out
# what .trivyignore-upstream.yaml accepts) into the job summary, prints a warning annotation when there is one, and converts the
# report into trivy-upstream-<label>.sarif for the artifact trivy-upstream-report. It never fails for a finding: the job reports
# and gates nothing. A report that is missing or cannot be read is a warning too, because the scan step that should have written
# it failed on its own and says why. Exit 2 on a usage error only.
#
# Needs jq; trivy (for the SARIF) is used when it is on PATH. GITHUB_STEP_SUMMARY is written when it is set.
set -uo pipefail

if [ "$#" -eq 0 ] || [ $(($# % 3)) -ne 0 ]; then
  echo "usage: upstream-image-report.sh <label> <image> <trivy json report> [<label> <image> <report>]..." >&2
  exit 2
fi

summary="${GITHUB_STEP_SUMMARY:-/dev/null}"
warn() { echo "::warning title=Upstream image scan::$*"; }

{
  echo "## The images the chart runs"
  echo
  echo "Findings of CRITICAL or HIGH that have a fix, in images Quader does not build. They hold no release; the fix is the"
  echo "publisher's new build and a raise of the pin in tools/versions.env (docs/developer/releasing.md#the-images-the-chart-runs)."
  echo
} >> "$summary"

sarif() {
  if command -v trivy >/dev/null 2>&1; then
    trivy convert --quiet --format sarif --output "trivy-upstream-$1.sarif" "$2" ||
      warn "the report of $1 could not be converted to SARIF"
  fi
}

if ! command -v jq >/dev/null 2>&1; then
  warn "jq is not on PATH; the findings are only in the reports of the artifact trivy-upstream-report"
  while [ "$#" -gt 0 ]; do
    [ -s "$3" ] && sarif "$1" "$3"
    shift 3
  done
  exit 0
fi

while [ "$#" -gt 0 ]; do
  label="$1" image="$2" report="$3"
  shift 3
  if [ ! -s "$report" ] || ! jq -e '.SchemaVersion' "$report" >/dev/null 2>&1; then
    warn "the scan of $label ($image) wrote no readable report; its step's log says why"
    printf '### %s\n\n`%s`: no report, the scan did not finish.\n\n' "$label" "$image" >> "$summary"
    continue
  fi
  count="$(jq '[.Results[]?.Vulnerabilities[]?] | length' "$report")"
  {
    printf '### %s\n\n`%s`: %s finding(s).\n\n' "$label" "$image" "$count"
    if [ "$count" -gt 0 ]; then
      echo "<details><summary>The findings</summary>"
      echo
      echo "| Package | Vulnerability | Severity | Installed | Fixed | Where |"
      echo "|---|---|---|---|---|---|"
      jq -r '.Results[]? as $r | $r.Vulnerabilities[]? |
        "| \(.PkgName) | \(.VulnerabilityID) | \(.Severity) | \(.InstalledVersion) | \(.FixedVersion // "") | \($r.Target) |"' "$report"
      echo
      echo "</details>"
      echo
    fi
  } >> "$summary"
  echo "upstream-image-report: $label $image: $count finding(s)"
  if [ "$count" -gt 0 ]; then
    warn "$count finding(s) of CRITICAL or HIGH with a fix in $label ($image); the job summary lists them"
  fi
  sarif "$label" "$report"
done
exit 0
