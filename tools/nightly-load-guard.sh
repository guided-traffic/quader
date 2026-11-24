#!/usr/bin/env bash
# Says whether load legs of the nightly run now (docs/developer/releasing.md#a-code-push-during-a-nightly):
#
#   tools/nightly-load-guard.sh      (make nightly-load-guard)
#
# A push starts kind legs of Test and Release on the runners the nightly's load legs measure on, and ten of them made a load leg six
# to nine times slower (F-164). A push is still made at any time; each load leg records the jobs that ran beside it (Q-ARCH-1062,
# tools/neighbours). This script is for the case where the quiet matters: a single set dispatched to measure goes with no
# push beside it. It lists the jobs of every run of "HA and Performance (nightly)" in progress whose name is a load leg (LOAD below)
# and are in progress or queued.
#
# Exit 0: no load leg runs. Exit 1: the load legs it prints run. Exit 2: it cannot tell (gh missing, not signed in, the API
# refused); it says so and does not guess.
#
# GH names the gh binary (a test's stub).
set -euo pipefail

GH="${GH:-gh}"
WORKFLOW="HA and Performance (nightly)"
# The legs whose conditions are times under load: the latency and the vulnerability performance jobs, and every e2e set of an HA
# scenario, a drain, a fault, a failover, a crash, a soak, a pooler or a long run (the sets' names of .github/workflows/performance.yml).
LOAD='^(Latency|Vulnerability scanning performance|e2e \(([a-z0-9-]*-)?(ha|drain|faults|failover|crash|perf|soak|pooler|long)(-[a-z0-9-]*)?\))$'

if ! command -v "$GH" >/dev/null 2>&1; then
  echo "nightly-load-guard: $GH is not installed; cannot tell whether the nightly's load legs run" >&2
  exit 2
fi
if ! runs="$("$GH" run list --workflow "$WORKFLOW" --status in_progress --json databaseId,url --jq '.[] | "\(.databaseId) \(.url)"' 2>&1)"; then
  echo "nightly-load-guard: gh run list failed: $runs" >&2
  exit 2
fi
found=0
while read -r id url; do
  [ -n "${id:-}" ] || continue
  if ! jobs="$("$GH" run view "$id" --json jobs --jq '.jobs[] | select(.status == "in_progress" or .status == "queued") | .name' 2>&1)"; then
    echo "nightly-load-guard: gh run view $id failed: $jobs" >&2
    exit 2
  fi
  while IFS= read -r name; do
    if [[ "$name" =~ $LOAD ]]; then
      echo "nightly-load-guard: run $id ($url) is running the load leg: $name"
      found=1
    fi
  done <<<"$jobs"
done <<<"$runs"
if [ "$found" = 1 ]; then
  echo "nightly-load-guard: a code push now runs Test and Release's kind legs beside them, and each records them as its neighbours"
  exit 1
fi
echo "nightly-load-guard: no load leg of the nightly runs"
