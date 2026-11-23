#!/usr/bin/env bash
# make ui-renders: every screen of the web UI as pictures, as the three kinds of user, in both colour schemes and at
# three widths, into .out/ui-renders with the contact sheet index.html (docs/developer/web-ui.md#renders-of-every-screen).
#
# Starts what is not running and stops again only what it started: the services of the UI development profile
# (up.sh, always: it is idempotent), quader with the profile's sign-in provider (serve.sh) and the Angular dev server.
# The seed and the pictures are web/e2e/playwright.renders.config.ts; arguments go to Playwright (--grep <screen>).
# Locally, and in the job ui-layout of Test and Release on every push (Q-ARCH-510), where the profile is the job's own.
# UI_INSTANCE=2..8 renders against an instance of its own (deploy/ui-dev/instance.sh): its containers, its quader and sign-in
# provider, its dev server, its database for the seed; a worktree has to name one, instance 1 is the main checkout's.
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
# Chromium, the engine of the screens' renders; `make ui-layout-test` runs them in Firefox and WebKit too (G5-53).
(cd web && npx playwright test -c e2e/playwright.layout.config.ts --project chromium)

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

if ! curl -skf -o /dev/null "https://127.0.0.1:$UI_OPS_PORT/readyz"; then
  echo "ui-renders: starting quader and the sign-in provider (log: $backend_log)"
  deploy/ui-dev/serve.sh > "$backend_log" 2>&1 &
  started+=("$!")
  wait_for quader 600 "https://127.0.0.1:$UI_OPS_PORT/readyz" "$backend_log" "$!"
fi

if ! curl -skf -o /dev/null "https://localhost:$UI_WEB_PORT/ui/"; then
  echo "ui-renders: starting the dev server (log: $web_log)"
  # web.sh execs the ng process itself, not npm around it, so that the TERM of cleanup reaches it.
  deploy/ui-dev/web.sh > "$web_log" 2>&1 &
  started+=("$!")
  wait_for "the dev server" 300 "https://localhost:$UI_WEB_PORT/ui/" "$web_log" "$!"
fi

# The renders' edge, state and database are the instance's (web/e2e/renders/paths.ts, seed.ts).
export UI_RENDERS_EDGE="https://localhost:$UI_WEB_PORT" UI_RENDERS_STATE="$UI_STATE" UI_RENDERS_POSTGRES="$UI_PROJECT-postgres"
cd web
# Without arguments every screen is rendered into an empty folder; with --grep the other pictures stay.
if [ "$#" -eq 0 ]; then export UI_RENDERS_CLEAN=1; fi
npx playwright test -c e2e/playwright.renders.config.ts "$@"
