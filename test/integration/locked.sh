#!/usr/bin/env bash
# Runs a command under the lock of a free instance of the shared integration stack, with that instance's environment loaded:
#
#   test/integration/locked.sh go test -count=1 -tags integration ./test/integration/coordination/
#   test/integration/locked.sh make test-integration
#   QUADER_TEST_INSTANCE=1 test/integration/locked.sh make test-integration      # pinned to instance 1
#
# run.sh is the short form for `go test` with the suite's flags.
#
# The stacks belong to the main checkout, where `make integration-up [INSTANCE=<n>]` ran and wrote the instance's env file
# (.dev/integration/env for instance 1, .dev/integration-<n>/env for instance n, test/integration/instance.sh); every worktree of
# the repository uses them. The script finds the main checkout through git's common directory (the parent of
# `git rev-parse --git-common-dir`, the same from the main checkout and from any worktree), so it tests the current directory's
# code against the main checkout's stacks.
#
# Which instance: QUADER_TEST_INSTANCE pins one, and the script waits for that instance's lock without a timeout. Without it every
# instance whose env file exists in the main checkout is a candidate: the script takes the first whose lock is free without
# waiting, and when every one is taken it tries them all again every two seconds, so it takes whichever becomes free first. It
# says on stderr which instance it took and how long it waited, then loads that instance's env file and runs the command in the
# current directory. It exports QUADER_TEST_INSTANCE as the instance it took, so `make test-integration` reads that instance's env
# file and rotate-cert.sh acts on that instance's containers.
#
# The lock of an instance is an flock(2) lock on <main>/.dev/integration.lock (instance 1) or <main>/.dev/integration-<n>.lock. It
# lives on an open descriptor (9) that the command inherits, so it is held for as long as the command or any process it started
# that kept the descriptor (a `go test` binary) is alive: killing the wrapper does not hand the stack to the next run while the
# tests still run. A lock file is never removed; a lock is the flock on it, not its existence. flock(1) takes the lock where it
# exists (Linux), lockf(1) on the descriptor otherwise (macOS, FreeBSD).
#
# The script's own variables carry the prefix QUADER_TEST_, as the stack's do: a quader process refuses every other
# QUADER_ variable that is not a setting (reservedEnv in internal/config/load.go), and the tests start quader processes
# with the environment they inherited.
set -euo pipefail

if [ $# -eq 0 ]; then
  echo "usage: $0 <command> [arguments...]" >&2
  exit 2
fi

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=test/integration/instance.sh
. "$here/instance.sh"

# QUADER_TEST_STACK_ROOT names another checkout whose stacks to use (a test of this script, or a stack started elsewhere).
main="${QUADER_TEST_STACK_ROOT:-$(dirname "$(cd "$(git rev-parse --git-common-dir)" && pwd)")}"

say() { echo "locked.sh: $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }

if [ -n "${QUADER_TEST_INSTANCE:-}" ]; then
  it_instance "$QUADER_TEST_INSTANCE" "$main"
  if [ ! -f "$IT_STATE/env" ]; then
    echo "$IT_STATE/env is missing: run 'make integration-up INSTANCE=$IT_INSTANCE' in $main first" >&2
    exit 1
  fi
  candidates="$IT_INSTANCE"
else
  candidates=""
  for n in 1 2 3 4 5 6 7 8 9; do
    it_instance "$n" "$main"
    if [ -f "$IT_STATE/env" ]; then
      candidates="$candidates $n"
    fi
  done
  candidates="${candidates# }"
  if [ -z "$candidates" ]; then
    echo "no instance of the integration stack is up in $main (no .dev/integration/env): run 'make integration-up' there first" >&2
    exit 1
  fi
fi

if command -v flock >/dev/null; then
  try_lock() { flock -n 9; }
  wait_lock() { flock 9; }
elif command -v lockf >/dev/null; then
  try_lock() { lockf -s -t 0 9; }
  wait_lock() { lockf -s 9; }
else
  try_lock() { say "neither flock nor lockf is installed: running without the lock"; }
  wait_lock() { :; }
fi
# The inode the path names now, and the inode of the open descriptor (GNU stat, then BSD stat).
inode() { stat -L -c %i "$1" 2>/dev/null || stat -L -f %i "$1" 2>/dev/null || true; }

# take <instance> wait|nowait opens the instance's lock file on descriptor 9 and locks it: at once, or after waiting when the
# second argument is wait. It succeeds when the lock is held on the file the path names now: a `lockf` without -k, typed by hand,
# removes the file when its command ends, and a process that waited on the removed file then holds a lock nobody else sees.
take() {
  it_instance "$1" "$main"
  mkdir -p "$(dirname "$IT_LOCK")"
  exec 9>>"$IT_LOCK"
  if ! try_lock; then
    if [ "$2" != wait ]; then
      exec 9>&-
      return 1
    fi
    say "waiting for the lock of instance $1 of the integration stack ($IT_LOCK)"
    wait_lock
  fi
  local now
  now="$(inode "$IT_LOCK")"
  if [ -n "$now" ] && [ "$now" = "$(inode /dev/fd/9)" ]; then
    return 0
  fi
  exec 9>&-
  return 1
}

start="$(date +%s)"
chosen=""
took="took"
# A command run through this script that runs it again already holds the lock (QUADER_TEST_INSTANCE is then the instance it
# took); a second lock would wait on itself.
if [ "$candidates" = "${QUADER_TEST_INSTANCE:-}" ] && it_instance "$candidates" "$main" && [ "${QUADER_TEST_LOCK_HELD:-}" = "$IT_LOCK" ]; then
  chosen="$candidates"
  took="already holds"
elif [ "$candidates" = "${candidates%% *}" ]; then
  until take "$candidates" wait; do :; done
  chosen="$candidates"
else
  waiting=""
  while [ -z "$chosen" ]; do
    for n in $candidates; do
      if take "$n" nowait; then
        chosen="$n"
        break
      fi
    done
    if [ -z "$chosen" ]; then
      if [ -z "$waiting" ]; then
        say "waiting for a free instance of the integration stack (instances $candidates are all taken)"
        waiting=1
      fi
      sleep 2
    fi
  done
fi

it_instance "$chosen" "$main"
export QUADER_TEST_INSTANCE="$chosen" QUADER_TEST_LOCK_HELD="$IT_LOCK"
say "$took instance $chosen ($IT_PROJECT) after waiting $(($(date +%s) - start)) s (pid $$): $*"

set -a
# shellcheck disable=SC1091
. "$IT_STATE/env"
set +a
exec "$@"
