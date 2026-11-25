#!/usr/bin/env bash
# The Angular dev server of the UI development profile (make ui-dev-web): the configuration `local` of web/angular.json (the proxy
# web/proxy.conf.mjs, HTTPS, the listen address 127.0.0.1) on the instance's port, with its certificate, its CA for the proxy's
# verification of quader, and its quader as the proxy's target. UI_INSTANCE picks the instance (deploy/ui-dev/instance.sh);
# instance 1 is what `npm run start:local` runs: port 4200, .dev/ui-dev/certs, https://localhost:19080. renders.sh starts it too.
# UI_LOCALE=de serves the German build instead (the configuration `de` of web/angular.json, Q-UI-1048): the dev server builds one locale.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=deploy/ui-dev/instance.sh
. "$root/deploy/ui-dev/instance.sh"
ui_instance "$root"
certs="$UI_STATE/certs"
[ -f "$certs/server.crt" ] || { echo "$certs/server.crt is missing: run 'make ui-dev-up UI_INSTANCE=$UI_INSTANCE' first" >&2; exit 1; }

case "${UI_LOCALE:-}" in
  "" | de) ;;
  *) echo "UI_LOCALE=$UI_LOCALE: empty (English) or de" >&2; exit 1 ;;
esac
cd "$root/web"
# The packages are installed again when the lock file is newer than the last install: a checkout that took a new dependency by a merge
# kept its old node_modules, the dev server's build failed on the missing package, and the browser went on showing the bundle of
# before without a sign (seen 2026-11-25).
if [ ! -f node_modules/.package-lock.json ] || [ package-lock.json -nt node_modules/.package-lock.json ]; then
  echo "web: package-lock.json is newer than node_modules; npm ci" >&2
  npm ci
fi
# exec: the ng process itself, so that a TERM (renders.sh stops what it started) reaches it.
NODE_EXTRA_CA_CERTS="$certs/ca.crt" QUADER_UI_DEV_BACKEND="https://localhost:$UI_BACKEND_PORT" \
  exec node_modules/.bin/ng serve --configuration "local${UI_LOCALE:+,$UI_LOCALE}" --port "$UI_WEB_PORT" --ssl-cert "$certs/server.crt" --ssl-key "$certs/server.key" "$@"
