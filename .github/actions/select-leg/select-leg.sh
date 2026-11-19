#!/usr/bin/env bash
# The decision of action.yml in this directory: whether one leg of a job of "HA and Performance (nightly)" runs in this run.
# Writes run=true or run=false to GITHUB_OUTPUT; a leg that does not run says why in the job summary and the log.
#
# Environment: LEG, JOB, SETS, NIGHTLY (the inputs of action.yml), TOKEN (a token that may read the repository's Actions runs),
# and what the runner sets: GITHUB_EVENT_NAME, GITHUB_REPOSITORY, GITHUB_REF_NAME, GITHUB_WORKFLOW_REF, GITHUB_API_URL,
# GITHUB_SERVER_URL, GITHUB_OUTPUT, GITHUB_STEP_SUMMARY.
#
# 1. The dispatch input sets: an empty list runs every leg whose nightly is "true"; a list runs the legs it names by set or job key.
# 2. Only on the schedule, and only for a leg that would run: one call of the Actions API for the completed dispatches of this
#    workflow on this branch. When one of them ran every leg (its run name ends in "(all legs)"), ended with success, failure or
#    timed_out less than FULL_RUN_HOURS ago, the leg is skipped and names that run. The reason and the condition to revisit it are
#    in docs/developer/releasing.md#a-scheduled-run-after-a-full-run. A failed call or an answer that cannot be read runs the leg:
#    a night without its run is worse than a run twice.
set -euo pipefail

FULL_RUN_HOURS=12

# The job-level conditions of this workflow compare the same string, so a space would select a non-matrix job and not a matrix
# leg: a list with spaces fails here, in every leg, instead.
if [[ "$SETS" == *[[:space:]]* ]]; then
  echo "::error::the input sets is a comma-separated list without spaces, got '$SETS'"
  exit 1
fi
case "$NIGHTLY" in true|false) ;; *) echo "::error::the input nightly is true or false, got '$NIGHTLY'"; exit 1 ;; esac

say() {
  echo "$1"
  echo "$1" >> "$GITHUB_STEP_SUMMARY"
}

run=false
if [ -z "$SETS" ]; then
  run="$NIGHTLY"
else
  case ",$SETS," in
    *",$LEG,"*|*",$JOB,"*) run=true ;;
  esac
fi
if [ "$run" != true ]; then
  echo "run=false" >> "$GITHUB_OUTPUT"
  if [ -z "$SETS" ]; then
    say "Leg $LEG of job $JOB runs only on a dispatch that names it or $JOB: its steps are skipped."
  else
    say "Leg $LEG of job $JOB is not in the sets of this dispatch ($SETS): its steps are skipped."
  fi
  exit 0
fi
if [ "${GITHUB_EVENT_NAME:-}" != schedule ]; then
  echo "run=true" >> "$GITHUB_OUTPUT"
  exit 0
fi

# fail_open <reason>: the check could not decide, so the leg runs and says why.
fail_open() {
  echo "::warning::the check for a full run of this workflow in the last $FULL_RUN_HOURS hours did not decide ($1): leg $LEG runs"
  echo "The check for a full run of this workflow in the last $FULL_RUN_HOURS hours did not decide ($1): leg $LEG of job $JOB runs." >> "$GITHUB_STEP_SUMMARY"
  echo "run=true" >> "$GITHUB_OUTPUT"
  exit 0
}

[ -n "${TOKEN:-}" ] || fail_open "no token"
command -v curl >/dev/null || fail_open "curl is not on the runner"
command -v python3 >/dev/null || fail_open "python3 is not on the runner"
workflow=${GITHUB_WORKFLOW_REF%%@*}
workflow=${workflow##*/}
branch=$GITHUB_REF_NAME
url="${GITHUB_API_URL:-https://api.github.com}/repos/$GITHUB_REPOSITORY/actions/workflows/$workflow/runs?branch=$branch&event=workflow_dispatch&status=completed&per_page=100"
answer=$(mktemp)
errors=$(mktemp)
trap 'rm -f "$answer" "$errors"' EXIT
if ! curl -fsS --connect-timeout 10 --max-time 30 -o "$answer" \
    -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" -H "Authorization: Bearer $TOKEN" \
    "$url" 2>"$errors"; then
  fail_open "the Actions API did not answer: $(tr '\n' ' ' < "$errors" | cut -c1-300)"
fi

# Prints "<run number> <html url> <updated_at> <hours ago>" of the newest full dispatch that ended less than $1 hours ago, nothing
# when there is none; exits 2 with the reason on stderr when the answer cannot be read.
read -r -d '' NEWEST_FULL_RUN <<'EOF' || true
import datetime, json, sys

hours = float(sys.argv[1])
try:
    runs = json.load(sys.stdin)["workflow_runs"]
    if not isinstance(runs, list):
        raise TypeError("workflow_runs is not a list")
    now = datetime.datetime.now(datetime.timezone.utc)
    best = None
    for r in runs:
        if r["event"] != "workflow_dispatch" or r["status"] != "completed":
            continue
        if r["conclusion"] not in ("success", "failure", "timed_out"):
            continue
        if not str(r["display_title"]).endswith("(all legs)"):
            continue
        ended = datetime.datetime.fromisoformat(r["updated_at"].replace("Z", "+00:00"))
        if now - ended < datetime.timedelta(hours=hours) and (best is None or ended > best[0]):
            best = (ended, r)
    line = ""
    if best is not None:
        ended, r = best
        line = "%d %s %s %.1f" % (int(r["run_number"]), r["html_url"], r["updated_at"], (now - ended).total_seconds() / 3600)
except (ValueError, KeyError, TypeError, AttributeError) as e:
    print("%s: %s" % (type(e).__name__, e), file=sys.stderr)
    sys.exit(2)
if line:
    print(line)
EOF
if ! found=$(python3 -I -c "$NEWEST_FULL_RUN" "$FULL_RUN_HOURS" < "$answer" 2>"$errors"); then
  fail_open "the answer of the Actions API could not be read: $(tr '\n' ' ' < "$errors" | cut -c1-300)"
fi
if [ -z "$found" ]; then
  echo "No full run of this workflow on $branch ended in the last $FULL_RUN_HOURS hours: leg $LEG runs."
  echo "run=true" >> "$GITHUB_OUTPUT"
  exit 0
fi
read -r number html ended age <<< "$found"
echo "run=false" >> "$GITHUB_OUTPUT"
say "Leg $LEG of job $JOB is skipped: this scheduled run defers to run #$number ($html), which ran every leg of this workflow on $branch and ended at $ended, $age hours ago. A scheduled run skips its legs when such a run ended less than $FULL_RUN_HOURS hours before (docs/developer/releasing.md#a-scheduled-run-after-a-full-run)."
