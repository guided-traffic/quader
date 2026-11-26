#!/usr/bin/env bash
# make ui-renders: every screen of the web UI as pictures, as the three kinds of user, in both colour schemes and at
# three widths, into .out/ui-renders with the contact sheet index.html (docs/developer/web-ui.md#renders-of-every-screen).
#
# Starts what is not running and stops again only what it started: the services of the UI development profile
# (up.sh, always: it is idempotent), quader with the profile's sign-in provider (serve.sh), the profile's Bastion (bastion.sh; UI_BASTION=0
# starts none) and the Angular dev server.
# The seed and the pictures are web/e2e/playwright.renders.config.ts; arguments go to Playwright (--grep <screen>).
# Locally, and in the job ui-layout of Test and Release on every push (Q-ARCH-510), where the profile is the job's own.
# Instances 2 to 8 render against an instance of their own (deploy/ui-dev/instance.sh): its containers, its quader and sign-in
# provider, its dev server, its database for the seed; a worktree claims a free one when UI_INSTANCE is unset, instance 1 is the
# main checkout's.
# UI_RENDERS_LOCALE=de renders the screens marked `de` against the dev server's German build (Q-UI-1048), UI_RENDERS_ENGINE=firefox or
# webkit renders in that engine (Q-UI-1049); each into a folder of its own (.out/ui-renders-de, -firefox, -webkit-de).
# UI_RENDERS_SHARD=i/n (make ui-renders SHARD=i/n) renders the i-th of n parts of the screens, as the legs of the job ui-layout do
# (Q-ARCH-1071): every n-th pair of screen and kind of user (web/e2e/renders/paths.ts). A part leaves the fixtures of the layout checks
# out, which the job ui-layout-fixtures runs once for all parts (make ui-layout-test), and keeps the pictures of the other parts, so
# that the parts run one after the other fill one contact sheet.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
mkdir -p .out
backend_log="$root/.out/ui-renders.backend.log"
web_log="$root/.out/ui-renders.web.log"

# The layout checks first, on their fixtures (seconds, no stack): a check that cannot find the defects it names judges nothing. In
# Chromium, the engine of the screens' renders; `make ui-layout-test` runs them in Firefox and WebKit too (Q-UI-1045).
# A run in another engine proves them in that engine. A part of the screens leaves them to the run of the whole or to
# `make ui-layout-test` (in CI the job ui-layout-fixtures).
engine="${UI_RENDERS_ENGINE:-chromium}"
locale="${UI_RENDERS_LOCALE:-en}"
case "$locale" in en | de) ;; *) echo "UI_RENDERS_LOCALE=$locale: en or de" >&2; exit 1 ;; esac
shard="${UI_RENDERS_SHARD:-}"
if [ -n "$shard" ] && ! [[ "$shard" =~ ^[1-9][0-9]*/[1-9][0-9]*$ && "${shard%/*}" -le "${shard#*/}" ]]; then
  echo "UI_RENDERS_SHARD=$shard: i/n with 1 <= i <= n, such as 2/5" >&2
  exit 1
fi
if [ -z "$shard" ]; then
  (cd web && npx playwright test -c e2e/playwright.layout.config.ts --project "$engine")
fi

deploy/ui-dev/up.sh

started=()
cleanup() {
  for pid in "${started[@]+"${started[@]}"}"; do kill -TERM "$pid" 2>/dev/null || true; done
  for pid in "${started[@]+"${started[@]}"}"; do wait "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT

wait_for() { # wait_for <what> <seconds> <url> <log> <pid>: until the URL answers; fails at once when the process ended
  local i
  for ((i = 0; i < $2; i++)); do
    curl -skf -o /dev/null "$3" && return 0
    if ! kill -0 "$5" 2>/dev/null; then
      echo "ui-renders: $1 ended before it answered at $3; the end of $4:" >&2
      tail -n 20 "$4" >&2
      return 1
    fi
    sleep 1
  done
  echo "ui-renders: $1 did not answer at $3 within $2 s (log: $4)" >&2
  return 1
}

# A backend the run starts runs no scanner (Q-ARCH-1064): the screens' scanner replicas are rows the seed writes, and no render fetches
# the analyser's database. UI_SCANNERS=1 starts them for a walk that needs them.
export UI_SCANNERS="${UI_SCANNERS:-0}"
if ! curl -skf -o /dev/null "https://127.0.0.1:$UI_OPS_PORT/readyz"; then
  echo "ui-renders: starting quader and the sign-in provider (log: $backend_log)"
  deploy/ui-dev/serve.sh > "$backend_log" 2>&1 &
  started+=("$!")
  wait_for quader 600 "https://127.0.0.1:$UI_OPS_PORT/readyz" "$backend_log" "$!"
fi

# The profile's Bastion (bastion.sh) beside quader, for the pages of its web interface (R-UI-230): registered at the instance, its
# interface and documentation on, its catalogue built from the seed. UI_BASTION=0 starts none, and the Bastion's screens fail.
bastion_log="$root/.out/ui-renders.bastion.log"
if [ "${UI_BASTION:-1}" != 0 ] && ! curl -skf -o /dev/null "https://127.0.0.1:$UI_BASTION_OPS_PORT/readyz"; then
  echo "ui-renders: starting the profile's Bastion (log: $bastion_log)"
  deploy/ui-dev/bastion.sh > "$bastion_log" 2>&1 &
  started+=("$!")
  wait_for "the Bastion" 600 "https://127.0.0.1:$UI_BASTION_OPS_PORT/readyz" "$bastion_log" "$!"
fi

if ! curl -skf -o /dev/null "https://localhost:$UI_WEB_PORT/ui/"; then
  echo "ui-renders: starting the dev server in $locale (log: $web_log)"
  # web.sh execs the ng process itself, not npm around it, so that the TERM of cleanup reaches it.
  UI_LOCALE="$([ "$locale" = en ] || echo "$locale")" deploy/ui-dev/web.sh > "$web_log" 2>&1 &
  started+=("$!")
  wait_for "the dev server" 300 "https://localhost:$UI_WEB_PORT/ui/" "$web_log" "$!"
fi
# The dev server builds one locale: one that runs already must serve the run's (its page's lang attribute).
served="$(curl -skf "https://localhost:$UI_WEB_PORT/ui/" | sed -n 's/.*<html lang="\([a-z-]*\)".*/\1/p' | head -n 1)"
if [ "$served" != "$locale" ]; then
  echo "ui-renders: the dev server on port $UI_WEB_PORT serves '$served', the run wants '$locale': stop it, and renders.sh starts one in $locale" >&2
  exit 1
fi

# The renders' edge, state and database are the instance's (web/e2e/renders/paths.ts, seed.ts).
export UI_RENDERS_EDGE="https://localhost:$UI_WEB_PORT" UI_RENDERS_STATE="$UI_STATE" UI_RENDERS_POSTGRES="$UI_PROJECT-postgres"
export UI_RENDERS_BASTION="https://localhost:$UI_BASTION_PORT"
cd web
# Without arguments every screen is rendered into an empty folder; with --grep, and in a part, the other pictures stay.
if [ "$#" -eq 0 ] && [ -z "$shard" ]; then export UI_RENDERS_CLEAN=1; fi
npx playwright test -c e2e/playwright.renders.config.ts "$@"
