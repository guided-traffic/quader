#!/usr/bin/env bash
# Fetches the `go test -json` reports of the end-to-end legs (and of the nightly latency gates) that have no whole report in this
# workflow run (Q-ROAD-11, Q-ARCH-811):
#
#   fetch-e2e-reports.sh <reports of this run> <destination>
#
# The coverage evidence job reads the reports of every test tier. The kind legs of Test and Release are path-filtered (a push may
# run none of them) and the long legs run in the nightly workflow, so for every leg whose report artifact is not among the reports
# of this run, the report of that leg made for the nearest commit is carried: the commit of this run itself or its nearest
# ancestor on main that has one, whichever workflow wrote it. The order is the commit's, not the time an artifact was created:
# with runs of main side by side, an older commit's run can finish after a newer one's, and a newer commit's report is never
# carried into an older commit's record (Test and Release 45159763759 carried the vulnerability report of a later commit).
#
# Which reports count:
# - a report is whole when every package it started also ended (passed, failed or was skipped) and it holds at least one event; a
#   report of this run that is not whole (its leg was stopped while a package ran) is moved to <reports of this run>.cut/ and the
#   leg is carried like a leg that did not run;
# - a candidate comes from a completed run of release.yml or performance.yml on main of this repository, started by a push, a
#   schedule or a dispatch (no pull request: its head branch can be called main in a fork), and not cancelled: a leg cut off
#   mid-set uploads the report of the tests it reached (Test and Release 43065872066 carried such a report of e2e (deb) and lost
#   R-DEB-004);
# - its run's commit is this run's commit or an ancestor of it (GITHUB_SHA, by default HEAD; the job checks out the whole
#   history), and its run started at most QUADER_E2E_REPORT_MAX_AGE_HOURS (default 168, a week) before now: a leg whose newest
#   report is older proves nothing until it runs again, so a requirement cannot stay in the record on the report of a test that
#   has since been deleted;
# - of the candidates of one name the nearest commit wins, of one commit the newest run; a candidate that is not whole is skipped
#   for the next one.
#
# Each carried report lands in <destination>/<artifact name>/. The output names every carried report with its run, commit and
# distance, every report that was not whole, and every leg whose reports are all older than the bound. This script never
# decides what a report proves.
#
# Environment: GITHUB_TOKEN (permission actions: read), GITHUB_REPOSITORY, GITHUB_RUN_ID, GITHUB_SHA (default HEAD),
# GITHUB_API_URL (default https://api.github.com). QUADER_E2E_REPORT_RUNS_RELEASE (default 40) and QUADER_E2E_REPORT_RUNS_NIGHTLY
# (default 14) bound how many runs are listed; QUADER_E2E_REPORT_MAX_AGE_HOURS (default 168) bounds how old a carried report may be.
set +x
set -euo pipefail

here="${1:-}"
dest="${2:-}"
[ -n "$here" ] && [ -n "$dest" ] || { echo "usage: fetch-e2e-reports.sh <reports of this run> <destination>" >&2; exit 2; }
: "${GITHUB_TOKEN:?GITHUB_TOKEN is not set}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is not set}"
api="${GITHUB_API_URL:-https://api.github.com}"
head="$(git rev-parse --verify "${GITHUB_SHA:-HEAD}^{commit}")"
max_age_hours="${QUADER_E2E_REPORT_MAX_AGE_HOURS:-168}"
# The reports of the kind legs, and the one of the latency gates of the nightly (make test-performance).
name_re='^(e2e(-[a-z]+)*-report(-.+)?|test-performance)$'

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# The token goes to curl on stdin as a config, not on the command line.
call() {
  printf 'header = "Authorization: Bearer %s"\n' "$GITHUB_TOKEN" |
    curl -q -fsSL --retry 3 --retry-delay 5 --config - -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28' "$@"
}

# whole <dir>: every `go test -json` file of the directory holds an event, and every package it started also ended.
whole() {
  local f found=0
  for f in "$1"/*.json; do
    [ -e "$f" ] || continue
    found=1
    jq -R -s -e '
      [split("\n")[] | fromjson? | objects] as $ev
      | ($ev | length) > 0
        and (([$ev[] | select(.Test == null and .Action == "start") | .Package] | unique)
             - ([$ev[] | select(.Test == null and (.Action == "pass" or .Action == "fail" or .Action == "skip")) | .Package] | unique)
             | length) == 0' "$f" >/dev/null 2>&1 || return 1
  done
  [ "$found" = 1 ]
}

# This run's reports: one that is not whole is set aside, and its leg is carried like a leg that did not run.
: > "$tmp/cut.txt"
if [ -d "$here" ]; then
  for dir in "$here"/*/; do
    [ -d "$dir" ] || continue
    name="$(basename "$dir")"
    if ! whole "$dir"; then
      mkdir -p "$here.cut"
      rm -rf "${here:?}.cut/$name"
      mv "$dir" "$here.cut/$name"
      echo "$name" >> "$tmp/cut.txt"
      echo "not whole: $name of this run (a package started and did not end); its leg is carried like one that did not run"
    fi
  done
fi

cutoff=$(( $(date +%s) - max_age_hours * 3600 ))
: > "$tmp/runs.jsonl"
for entry in "release.yml:${QUADER_E2E_REPORT_RUNS_RELEASE:-40}" "performance.yml:${QUADER_E2E_REPORT_RUNS_NIGHTLY:-14}"; do
  workflow="${entry%%:*}"
  count="${entry##*:}"
  call "$api/repos/$GITHUB_REPOSITORY/actions/workflows/$workflow/runs?branch=main&status=completed&per_page=$count" |
    jq -c --arg self "$GITHUB_RUN_ID" --arg repo "$GITHUB_REPOSITORY" '.workflow_runs[]
      | select((.id | tostring) != $self)
      | select(.conclusion != "cancelled")
      | select(.event == "push" or .event == "schedule" or .event == "workflow_dispatch")
      | select(.head_repository.full_name == $repo)
      | {id, sha: .head_sha, created_at, epoch: (.created_at | fromdateiso8601)}' >> "$tmp/runs.jsonl"
done

# The runs of this commit and of its ancestors, with the number of commits between theirs and this one.
: > "$tmp/ancestors.jsonl"
while read -r r; do
  sha="$(jq -r .sha <<<"$r")"
  git cat-file -e "$sha^{commit}" 2>/dev/null || continue
  git merge-base --is-ancestor "$sha" "$head" || continue
  jq -c --argjson distance "$(git rev-list --count "$sha..$head")" '. + {distance: $distance}' <<<"$r" >> "$tmp/ancestors.jsonl"
done < "$tmp/runs.jsonl"

: > "$tmp/candidates.jsonl"
while read -r r; do
  run="$(jq -r .id <<<"$r")"
  page=1
  while :; do
    call "$api/repos/$GITHUB_REPOSITORY/actions/runs/$run/artifacts?per_page=100&page=$page" > "$tmp/page.json"
    jq -c --arg re "$name_re" --argjson run "$r" \
      '.artifacts[] | select(.expired | not) | select(.name | test($re)) | {name, id} + {run: $run.id, sha: $run.sha, created_at: $run.created_at, epoch: $run.epoch, distance: $run.distance}' \
      "$tmp/page.json" >> "$tmp/candidates.jsonl"
    [ "$(jq '.artifacts | length' "$tmp/page.json")" -eq 100 ] || break
    page=$((page + 1))
  done
done < "$tmp/ancestors.jsonl"

mkdir -p "$dest"
fetched=0
for name in $( { jq -r .name "$tmp/candidates.jsonl"; cat "$tmp/cut.txt"; } | sort -u); do
  if [ -d "$here/$name" ]; then
    continue
  fi
  # The nearest commit first, of one commit the newest run; only the runs inside the bound.
  jq -c -s --arg name "$name" --argjson cutoff "$cutoff" \
    'map(select(.name == $name and .epoch >= $cutoff)) | sort_by(.distance, -.epoch) | .[]' "$tmp/candidates.jsonl" > "$tmp/ordered.jsonl"
  if [ "$(jq -s --arg name "$name" 'map(select(.name == $name)) | length' "$tmp/candidates.jsonl")" -eq 0 ]; then
    echo "no whole report: $name; no earlier run of this commit or an ancestor has one, not carried"
    continue
  fi
  if [ ! -s "$tmp/ordered.jsonl" ]; then
    newest="$(jq -r -s --arg name "$name" 'map(select(.name == $name)) | max_by(.epoch) | "\(.created_at), run \(.run)"' "$tmp/candidates.jsonl")"
    echo "too old: $name (its newest report is of $newest, more than $max_age_hours hours ago); not carried"
    continue
  fi
  carried=""
  while read -r c; do
    id="$(jq -r .id <<<"$c")"
    run="$(jq -r .run <<<"$c")"
    sha="$(jq -r .sha <<<"$c")"
    rm -rf "$tmp/artifact"
    mkdir -p "$tmp/artifact"
    call -o "$tmp/artifact.zip" "$api/repos/$GITHUB_REPOSITORY/actions/artifacts/$id/zip"
    unzip -q -o "$tmp/artifact.zip" -d "$tmp/artifact"
    if ! whole "$tmp/artifact"; then
      echo "not whole: $name of run $run (commit ${sha:0:9}); the next one is tried"
      continue
    fi
    rm -rf "${dest:?}/$name"
    mv "$tmp/artifact" "$dest/$name"
    echo "carried: $name from run $run (commit ${sha:0:9}, $(jq -r .distance <<<"$c") commits before this one, $(jq -r .created_at <<<"$c"))"
    carried=1
    fetched=$((fetched + 1))
    break
  done < "$tmp/ordered.jsonl"
  [ -n "$carried" ] || echo "no whole report: $name; not carried"
done
echo "carried reports: $fetched"
