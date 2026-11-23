#!/usr/bin/env bash
# Runs one fetch from a host outside under a wall-clock bound, and again when it fails: fetch.sh <what> <seconds> <attempts> <command>...
#
#   <what>      the fetch in words, for the message: "npm ci of the npm clients", "nuget.exe from dist.nuget.org"
#   <seconds>   the bound of one attempt: TERM at <seconds>, KILL 10 s later
#   <attempts>  how often the command runs at most
#
# The builds of the clients images (test/e2e/*/Dockerfile, through the named build context `ci` their build.sh passes), the
# pre-pull of their base images (.github/scripts/client-images.sh) and the builds of the e2e driver and the test upstreams
# (deploy/dev/e2e.sh, the Makefile's *-stub-image targets) run every fetch that is not an OS package through this script; the
# OS packages go through apt-install.sh beside it, which knows what an ended update of the package indexes loses. The reason is
# 2026-11-23, 11:45 to 12:30: the Ubuntu archive answered so slowly that the update of the package indexes in the build of the
# APT clients image read one index for 25 minutes (Test and Release 45157748657, `e2e (deb)`), and the builds of the NuGet
# clients image in four legs of two pushes and seven legs of the
# nightly ran into the step's limit of 30 minutes; none of those legs ran a test, and each said only "The action has timed out".
# The idle timeouts of the tools (apt's 120 s, npm's fetch-timeout, pip's --timeout, curl's --speed-time) end a connection that
# sends nothing, not one that sends a byte now and then, which is what such a mirror does. The wall-clock bound ends both.
#
# An attempt that fails or is ended waits 5 s times its number (QUADER_FETCH_PAUSE overrides the 5; the tests set 0) and the
# command runs again: archive.ubuntu.com, the npm registry and Docker Hub resolve to several addresses, so a new attempt is a
# new connection and often another server. After the last attempt the script exits 1 with one line that names the fetch, the
# bound and every attempt's end, for example
#
#   ::error::fetch: nuget.exe 7.9.0 from dist.nuget.org failed in 3 attempts of at most 60 s each (1: no end within 60 s; 2: exit 22; 3: exit 22)
#
# The line is an annotation where a job runs the script; inside a docker build, where the runner reads no workflow command,
# client-images.sh finds it in the build's output by `::error::` and repeats it with the image's name. A command that succeeds
# prints nothing of this script's but a line per failed attempt before it.
set -euo pipefail

[ "$#" -ge 4 ] || { echo "usage: fetch.sh <what> <seconds> <attempts> <command>..." >&2; exit 2; }
what="$1" seconds="$2" attempts="$3"
shift 3
case "$seconds$attempts" in *[!0-9]*) echo "fetch.sh: <seconds> and <attempts> are whole numbers, got $seconds and $attempts" >&2; exit 2 ;; esac
[ "$seconds" -gt 0 ] && [ "$attempts" -gt 0 ] || { echo "fetch.sh: <seconds> and <attempts> are at least 1" >&2; exit 2; }
pause="${QUADER_FETCH_PAUSE:-5}"

ends=""
attempt=1
while :; do
  status=0
  timeout -k 10 "$seconds" "$@" || status=$?
  [ "$status" -ne 0 ] || exit 0
  case "$status" in
    124|137) end="no end within $seconds s" ;;
    *) end="exit $status" ;;
  esac
  ends="${ends:+$ends; }$attempt: $end"
  if [ "$attempt" -ge "$attempts" ]; then
    echo "::error::fetch: $what failed in $attempts attempts of at most $seconds s each ($ends)"
    exit 1
  fi
  echo "fetch: $what, attempt $attempt of $attempts: $end; trying again in $((pause * attempt)) s"
  sleep "$((pause * attempt))"
  attempt=$((attempt + 1))
done
