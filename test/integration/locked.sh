#!/usr/bin/env bash
# Runs a command under the lock of the shared integration stack, with the stack's environment loaded:
#
#   test/integration/locked.sh go test -count=1 -tags integration ./test/integration/coordination/
#   test/integration/locked.sh make test-integration
#
# The stack belongs to the main checkout, where `make integration-up` ran and wrote .dev/integration/env; every worktree of
# the repository uses that one stack. The script finds the main checkout through git's common directory (the parent of
# `git rev-parse --git-common-dir`, the same from the main checkout and from any worktree), waits without a
# timeout for an exclusive flock(2) lock on <main>/.dev/integration.lock, loads <main>/.dev/integration/env and runs the
# command in the current directory.
#
# The lock lives on an open descriptor (9) that the command inherits, so it is held for as long as the command or any
# process it started that kept the descriptor (a `go test` binary) is alive: killing the wrapper does not hand the stack
# to the next run while the tests still run. The lock file is never removed; a lock is the flock on it, not its
# existence. flock(1) takes the lock where it exists (Linux), lockf(1) on the descriptor otherwise (macOS, FreeBSD).
#
# The script's own variables carry the prefix QUADER_TEST_, as the stack's do: a quader process refuses every other
# QUADER_ variable that is not a setting (reservedEnv in internal/config/load.go), and the tests start quader processes
# with the environment they inherited.
set -euo pipefail

if [ $# -eq 0 ]; then
  echo "usage: $0 <command> [arguments...]" >&2
  exit 2
fi

# QUADER_TEST_STACK_ROOT names another checkout whose stack to use (a second stack, or a test of this script).
main="${QUADER_TEST_STACK_ROOT:-$(dirname "$(cd "$(git rev-parse --git-common-dir)" && pwd)")}"
lock="$main/.dev/integration.lock"
env_file="$main/.dev/integration/env"
if [ ! -f "$env_file" ]; then
  echo "$env_file is missing: run 'make integration-up' in $main first" >&2
  exit 1
fi

say() { echo "locked.sh: $(date '+%Y-%m-%d %H:%M:%S') $*" >&2; }

# A command run through this script that runs it again already holds the lock; a second lock would wait on itself.
if [ "${QUADER_TEST_LOCK_HELD:-}" != "$lock" ]; then
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

  mkdir -p "$(dirname "$lock")"
  while :; do
    exec 9>>"$lock"
    if ! try_lock; then
      say "waiting for the integration stack's lock $lock"
      wait_lock
    fi
    # A `lockf` without -k, typed by hand, removes the file when its command ends. A process that waited on the removed file
    # then holds a lock nobody else sees, while the next one creates and locks a new file: lock the file the path names.
    now="$(inode "$lock")"
    if [ -n "$now" ] && [ "$now" = "$(inode /dev/fd/9)" ]; then
      break
    fi
    exec 9>&-
  done
  export QUADER_TEST_LOCK_HELD="$lock"
  say "holding the integration stack's lock (pid $$): $*"
fi

set -a
# shellcheck disable=SC1090
. "$env_file"
set +a
exec "$@"
