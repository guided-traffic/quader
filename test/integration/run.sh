#!/usr/bin/env bash
# Runs integration tests against whichever instance of the integration stack is free, from the main checkout or any worktree:
#
#   test/integration/run.sh ./test/integration/coordination/
#   test/integration/run.sh -run 'TestR_ARCH_027' ./test/integration/querybudget/ ./test/integration/vulnblock/
#   test/integration/run.sh -timeout 60m ./test/integration/backup/
#   QUADER_TEST_INSTANCE=1 test/integration/run.sh ./test/integration/smoke/      # pinned to instance 1
#
# It runs `go test -count=1 -p 1 -tags integration -timeout 30m <arguments>` through locked.sh: the first instance up in the main
# checkout whose lock is free, or, when every one is taken, whichever becomes free first; the instance's lock is held for the run
# and its env file loaded (locked.sh says which instance it took and how long it waited). The arguments are the caller's packages
# and flags; -timeout 30m is added only when they carry no -timeout of their own. The exit code is go test's.
set -euo pipefail

if [ $# -eq 0 ]; then
  echo "usage: $0 [go test flags] <packages> [flags]" >&2
  exit 2
fi

timeout=yes
for a in "$@"; do
  case "$a" in
    -timeout | -timeout=* | --timeout | --timeout=*) timeout="" ;;
  esac
done
if [ -n "$timeout" ]; then
  set -- -timeout 30m "$@"
fi
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/locked.sh" go test -count=1 -p 1 -tags integration "$@"
