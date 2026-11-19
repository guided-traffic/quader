#!/usr/bin/env bash
# Downloads every module of go.sum into the module cache, bounded in time: go-mod-download.sh
# Run in the repository root after the Go setup; .github/actions/go-modules calls it between the restore and the save of the
# module cache (Q-ARCH-523, Q-ARCH-524).
#
# Why: in Test and Release 44511234820 (2026-11-19) the gating job `chart` downloaded 75 modules inside `make chart-test`; the
# download of one of them from proxy.golang.org sent nothing for 16 minutes until the proxy closed the connection, the job
# reached its 20-minute limit and the release was skipped (F-156). go has no overall bound on a module fetch, and the restored
# cache of actions/setup-go held only what the first job after a change of go.sum had needed.
#
#   1. `go mod download` fetches every module the build list of go.sum needs, so that no later step of the job downloads one: a
#      job that restored a complete cache fetches nothing here, and its make targets fetch nothing either.
#   2. The command runs under a wall-clock bound (timeout, TERM, then KILL 10 s later): 300 s by default, QUADER_GO_DOWNLOAD_SECONDS
#      overrides it. A full download took 31 s on the development machine and 13 to 34 s for the 75 modules of a job in the
#      green run before F-156; the bound is an order above that, against a hang, not against a slow day.
#   3. It runs with -x, which prints every request to the proxy and its answer. On a hang the script names the requests that
#      got no answer, so the log says which host and which module; on success it says how many files it fetched from where.
set -euo pipefail

bound="${QUADER_GO_DOWNLOAD_SECONDS:-300}"
kill_after=10

log="$(mktemp)"
trap 'rm -f "$log"' EXIT

start=$SECONDS
status=0
timeout -k "$kill_after" "$bound" go mod download -x >"$log" 2>&1 || status=$?
elapsed=$((SECONDS - start))

# -x prints "# get <url>" when a request starts and "# get <url>: <status> (<time>)" when it ends.
started="$(sed -n -E 's/^# get ([^ ]+)$/\1/p' "$log" | LC_ALL=C sort -u)"
finished="$(sed -n -E 's/^# get ([^ ]+): .*/\1/p' "$log" | LC_ALL=C sort -u)"
unanswered="$(LC_ALL=C comm -23 <(printf '%s\n' "$started") <(printf '%s\n' "$finished") | sed '/^$/d')"
count="$(printf '%s\n' "$finished" | sed '/^$/d' | wc -l | tr -d ' ')"
hosts="$(printf '%s\n' "$finished" | sed -n -E 's#^[a-z]+://([^/]+)/.*#\1#p' | LC_ALL=C sort -u | paste -sd, -)"

if [ "$status" -eq 0 ]; then
  if [ "$count" -eq 0 ]; then
    echo "go-mod-download: every module of go.sum is in the module cache; nothing fetched (${elapsed} s)"
  else
    echo "go-mod-download: fetched ${count} files from ${hosts} in ${elapsed} s"
  fi
  exit 0
fi

grep -v '^# get ' "$log" | tail -20 >&2 || true
proxy="$(go env GOPROXY 2>/dev/null || echo unknown)"
case "$status" in
  124|137)
    echo "::error::the download of the Go modules did not finish in ${bound} s (GOPROXY=${proxy}); ${count} files had arrived. Requests without an answer:"
    printf '%s\n' "$unanswered" | head -20 | sed 's/^/  /'
    ;;
  *)
    echo "::error::go mod download failed with exit ${status} after ${elapsed} s (GOPROXY=${proxy}); the lines above are its errors"
    ;;
esac
exit "$status"
