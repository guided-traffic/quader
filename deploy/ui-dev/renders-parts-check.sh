#!/usr/bin/env bash
# make ui-renders-parts-check [PARTS=n]: the parts of `make ui-renders SHARD=i/n` hold every screen of the whole pass exactly once
# (Q-ARCH-1071). Nothing is rendered and no stack is needed: Playwright lists the tests of the render configuration (`--list`, with
# the projects and tags the runs use) once for the whole pass and once per part, and the lists are compared. A test of render.spec.ts
# that a part lists twice, or that no part lists, fails the check, and so does a spec file the whole pass lists beyond render.spec.ts
# and the measurements: a part loads render.spec.ts alone (playwright.renders.config.ts), so such a file would run in no leg.
# Seconds (after web-ci). CI: a step of the job ui-layout-fixtures, with the number of legs of the job ui-layout.
set -euo pipefail

parts="${1:-5}"
case "$parts" in '' | *[!0-9]* | 0 | 1) echo "renders-parts-check: the number of parts is a number of 2 or more, not '$parts'" >&2; exit 2 ;; esac

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root/web"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The measurements beside the screens: they run with the whole set and their GREP (web-ui.md), never in a part.
measurements='^(live-cost|slow-loads)\.spec\.ts$'

# list <file> [env...]: one line per test, "<project> <file> <title>", without the line and column of the declaration.
list() {
  local out="$1"
  shift
  env "$@" npx playwright test -c e2e/playwright.renders.config.ts --list --reporter=list >"$work/raw" 2>&1 || {
    cat "$work/raw" >&2
    return 1
  }
  sed -n -E 's/^ *\[([a-z]+)\] › ([^:]+):[0-9]+:[0-9]+ › (.*)$/\1 \2 \3/p' "$work/raw" | sort >"$out"
}

list "$work/whole" UI_RENDERS_SHARD=
if [ ! -s "$work/whole" ]; then
  echo "renders-parts-check: the whole pass lists no test" >&2
  exit 1
fi
: >"$work/parts"
for ((i = 1; i <= parts; i++)); do
  list "$work/part-$i" "UI_RENDERS_SHARD=$i/$parts"
  cat "$work/part-$i" >>"$work/parts"
done
sort -o "$work/parts" "$work/parts"

failed=0
others="$(awk '{print $2}' "$work/whole" | sort -u | grep -v -x 'render.spec.ts' | grep -v -E "$measurements" || true)"
if [ -n "$others" ]; then
  echo "renders-parts-check: the whole pass lists spec files that no part loads: $others" >&2
  failed=1
fi
grep -E '^[a-z]+ render\.spec\.ts ' "$work/whole" >"$work/screens" || true
twice="$(uniq -d "$work/parts")"
if [ -n "$twice" ]; then
  echo "renders-parts-check: in more than one of $parts parts:" >&2
  echo "$twice" >&2
  failed=1
fi
missing="$(comm -23 "$work/screens" <(sort -u "$work/parts"))"
if [ -n "$missing" ]; then
  echo "renders-parts-check: in the whole pass and in none of $parts parts:" >&2
  echo "$missing" >&2
  failed=1
fi
extra="$(comm -13 "$work/screens" <(sort -u "$work/parts"))"
if [ -n "$extra" ]; then
  echo "renders-parts-check: in a part and not in the whole pass:" >&2
  echo "$extra" >&2
  failed=1
fi
sizes=""
for ((i = 1; i <= parts; i++)); do sizes="$sizes $(wc -l <"$work/part-$i" | tr -d ' ')"; done
if [ "$failed" -ne 0 ]; then exit 1; fi
echo "renders-parts-check: $(wc -l <"$work/screens" | tr -d ' ') screen tests, each in exactly one of $parts parts (sizes:$sizes)"
