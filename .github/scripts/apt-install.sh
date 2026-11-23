#!/usr/bin/env bash
# Installs Ubuntu packages in a CI job, bounded in time: apt-install.sh <package>...
#
# Every step of the workflows that needs an OS package calls this script; no step runs apt-get itself (a test of
# internal/tools/ciworkflows holds that). The reason is a package index that does not answer: apt-get waited on
# archive.ubuntu.com for 19 minutes until GitHub cancelled the gating job `chart` at its limit (Test and Release 44384406557,
# 2026-11-18), and a job that hangs cannot be re-run before it ends. The numbers below come from the green run before it
# (44372205911, every job's log): docs/developer/releasing.md#os-packages-in-the-jobs has the measurements.
#
#   1. A package that is installed already (dpkg-query) is skipped. When every package is installed the script ends at once and
#      touches neither the network nor sudo, so on a runner image that carries the packages the step costs nothing.
#   2. The package indexes are read once per job: a later call in the same job (RUNNER_TEMP holds a stamp) does not read them
#      again. The update reads the Ubuntu archive only, when the runner image has /etc/apt/sources.list.d/ubuntu.sources: a
#      third-party source of the image (the git PPA) answers slowly or not at all (503 on 2026-11-11) and holds nothing a job
#      installs. Without that file every source of the image is read.
#   3. Each apt-get call runs under a wall-clock bound (timeout, TERM, then KILL 10 s later) and with apt's own bound: 20 s
#      without a byte on a connection ends the request. apt's own bound does not end a source that sends a byte now and then
#      (a test server sending one byte every 5 s held apt-get update until the wall-clock bound ended it), so the wall-clock
#      bound is the one that holds. The update may take 60 s, the download of the packages 90 s, the unpacking 120 s.
#   4. The update has to end by itself wherever it can, because an update that the bound ends keeps nothing: the indexes of
#      the sources that had answered are lost with it (HA and Performance 44453781574, nuget-ha: security.ubuntu.com was
#      silent during the first update and archive.ubuntu.com during the second, both were ended at 60 s, and the install
#      found no index at all). apt gives up a silent source after 41 s per try with the 20 s above (measured there and in a
#      container), so the update retries no request itself: it ends inside its bound, keeps the indexes of the sources that
#      answered and exits 0. The retry is this script's: an update that did not read every source, or that the bound ended,
#      runs once more, on a new connection. The download retries each request 3 times; a download that the bound ended runs
#      once more, one that failed by itself is not repeated.
#   5. An update that is incomplete after its second run is a warning: the install goes on with the indexes that are present.
#      A failed download or unpacking fails the step and names the packages and the hosts apt was waiting on. An update that
#      never finished leaves no indexes on a runner image that ships none, and the install then fails at once with "Unable
#      to locate package".
#
# Worst case: 2 x 60 + 2 x 90 + 120 s, plus 10 s per kill, under 8 minutes; the shortest job that installs a package has 15.
# The packages are downloaded first (--download-only) and unpacked from the cache (--no-download), so the bound that can
# cut a stalled download never cuts dpkg in the middle of a package.
#
# QUADER_APT_SOURCES names another file for the Ubuntu sources (the tests use it). QUADER_APT_OPTIONS adds apt options, one word
# each: the builds of the clients images (test/e2e/*/Dockerfile) run this script for their packages and pass
# `-o APT::Install-Recommends=false`, so an image gets what it names and nothing the base image's apt would recommend, without a
# file in the image that would change the apt of the APT clients' tests.
set -euo pipefail

[ "$#" -gt 0 ] || { echo "usage: apt-install.sh <package>..." >&2; exit 2; }

update_seconds=60
download_seconds=90
unpack_seconds=120
kill_after=10
apt_options=(-o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 -o Dpkg::Use-Pty=0)
# shellcheck disable=SC2206 # one word per option, split on purpose
[ -z "${QUADER_APT_OPTIONS:-}" ] || apt_options+=(${QUADER_APT_OPTIONS})
update_retries=0
download_retries=3

missing=()
for p in "$@"; do
  if ! dpkg-query -W -f='${db:Status-Status}\n' "$p" 2>/dev/null | grep -x installed >/dev/null; then
    missing+=("$p")
  fi
done
if [ "${#missing[@]}" -eq 0 ]; then
  echo "apt-install: $* already installed, nothing to fetch"
  exit 0
fi
echo "apt-install: installing ${missing[*]}"

if [ "$(id -u)" -eq 0 ]; then
  as_root=(env)
else
  as_root=(sudo -n env)
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# bounded SECONDS LOG ARG...: apt-get ARG... as root under the wall-clock bound, its output in the job log and in LOG. Prints
# nothing itself; the exit status is apt-get's, or 124 (137 after the KILL) when the bound ended it.
bounded() {
  local seconds="$1" log="$2" status
  shift 2
  set +e
  "${as_root[@]}" DEBIAN_FRONTEND=noninteractive timeout -k "$kill_after" "$seconds" apt-get "${apt_options[@]}" "$@" 2>&1 | tee "$log"
  status="${PIPESTATUS[0]}"
  set -e
  return "$status"
}

timed_out() { [ "$1" -eq 124 ] || [ "$1" -eq 137 ]; }

# requests LOG: per request apt printed (Get:1, Ign:1, ...), its last state and its host.
requests() {
  LC_ALL=C awk '
    $1 ~ /^(Get|Hit|Ign|Err):[0-9]+$/ && $2 ~ /^[a-z+]+:\/\// {
      split($1, s, ":"); host = $2; sub(/^[a-z+]+:\/\//, "", host); sub(/\/.*/, "", host)
      state[s[2]] = s[1]; at[s[2]] = host
    }
    END { for (i in state) print state[i], at[i] }' "$1"
}

# failed LOG: the hosts of the requests apt gave up on (their last line is Ign or Err); empty when there are none. The helpers
# from here on only word a message: they never fail, so a tool that breaks while reading the log cannot change the exit status
# of the step (macOS awk crashed now and then on these programs when the environment held variables with newlines).
failed() {
  requests "$1" | awk '$1 == "Ign" || $1 == "Err" { print $2 }' | LC_ALL=C sort -u | paste -sd ' ' - || true
}

# waiting LOG: who a call that the bound ended was waiting on. apt prints a request when it starts it and nothing when it
# ends, so only a request whose last line is Ign or Err is known to have failed; without one, every host apt started a
# request to is named ("one of ...").
waiting() {
  local h
  h="$(failed "$1")"
  [ -n "$h" ] || h="$(requests "$1" | awk '{ print $2 }' | LC_ALL=C sort -u | paste -sd ' ' - || true)"
  [ -z "$h" ] || [ -n "$(failed "$1")" ] || h="one of $h"
  echo "${h:-no host, apt had started no request}"
}

# unread LOG: the hosts of the requests apt gave up for good (their last line is Err); empty when it read every source.
unread() {
  requests "$1" | awk '$1 == "Err" { print $2 }' | LC_ALL=C sort -u | paste -sd ' ' - || true
}

# why LOG STATUS: apt's last error line (E: ...), or the exit status when it printed none.
why() {
  local e
  e="$(grep '^E: ' "$1" | tail -n 1)" || true
  echo "${e:-apt-get exited with $2}"
}

# The package indexes.
stamp="${RUNNER_TEMP:-}/apt-install-indexes-read"
sources="${QUADER_APT_SOURCES:-/etc/apt/sources.list.d/ubuntu.sources}"
scope=()
if [ -f "$sources" ]; then
  scope=(-o "Dir::Etc::sourcelist=$sources" -o "Dir::Etc::sourceparts=-" -o "APT::Get::List-Cleanup=0")
fi
indexes="read"
if [ -n "${RUNNER_TEMP:-}" ] && [ -f "$stamp" ]; then
  echo "apt-install: the package indexes were read by an earlier step of this job"
else
  for attempt in 1 2; do
    status=0
    bounded "$update_seconds" "$work/update.log" -o "Acquire::Retries=$update_retries" ${scope[@]+"${scope[@]}"} update || status=$?
    lost="$(unread "$work/update.log")"
    if [ "$status" -eq 0 ] && [ -z "$lost" ]; then
      [ -z "${RUNNER_TEMP:-}" ] || touch "$stamp"
      break
    fi
    if timed_out "$status"; then
      what="did not finish in ${update_seconds} s (waiting on $(waiting "$work/update.log"))"
    elif [ "$status" -eq 0 ]; then
      what="could not read $lost"
    else
      what="failed ($(why "$work/update.log" "$status"))"
    fi
    if [ "$attempt" -eq 1 ]; then
      echo "::warning::apt-get update $what; reading the indexes once more"
      continue
    fi
    indexes="incomplete"
    echo "::warning::apt-get update $what, the second time; installing from the indexes present"
  done
fi

# The packages: downloaded under the bound, then unpacked from the cache.
for attempt in 1 2; do
  status=0
  bounded "$download_seconds" "$work/download.log" -o "Acquire::Retries=$download_retries" install -y --download-only "${missing[@]}" || status=$?
  [ "$status" -ne 0 ] || break
  if timed_out "$status" && [ "$attempt" -eq 1 ]; then
    echo "::warning::the download of ${missing[*]} did not finish in ${download_seconds} s (waiting on $(waiting "$work/download.log")); trying once more"
    continue
  fi
  if timed_out "$status"; then
    echo "::error::the download of ${missing[*]} did not finish in ${download_seconds} s twice (waiting on $(waiting "$work/download.log")). Re-run the job."
  else
    hosts="$(failed "$work/download.log")"
    note=""
    [ "$indexes" = read ] || note="; the package indexes are incomplete, see the update above"
    echo "::error::apt-get could not fetch ${missing[*]} ($(why "$work/download.log" "$status")${hosts:+; no answer from $hosts}$note). Re-run the job if a mirror was down."
  fi
  exit 1
done

status=0
bounded "$unpack_seconds" "$work/unpack.log" install -y --no-download "${missing[@]}" || status=$?
if [ "$status" -ne 0 ]; then
  if timed_out "$status"; then
    echo "::error::installing ${missing[*]} from the downloaded packages did not finish in ${unpack_seconds} s"
  else
    echo "::error::installing ${missing[*]} from the downloaded packages failed ($(why "$work/unpack.log" "$status"))"
  fi
  exit 1
fi
echo "apt-install: installed ${missing[*]}"
