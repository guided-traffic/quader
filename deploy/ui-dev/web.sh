#!/usr/bin/env bash
# The Angular dev server of the UI development profile (make ui-dev-web): the configuration `local` of web/angular.json (the proxy
# web/proxy.conf.mjs, HTTPS, the listen address 127.0.0.1) on the instance's port, with its certificate, its CA for the proxy's
# verification of quader, and its quader as the proxy's target. UI_INSTANCE picks the instance (deploy/ui-dev/instance.sh);
# instance 1 is what `npm run start:local` runs: port 4200, .dev/ui-dev/certs, https://localhost:19080. renders.sh starts it too.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=deploy/ui-dev/instance.sh
. "$root/deploy/ui-dev/instance.sh"
ui_instance "$root"
certs="$UI_STATE/certs"
[ -f "$certs/server.crt" ] || { echo "$certs/server.crt is missing: run 'make ui-dev-up UI_INSTANCE=$UI_INSTANCE' first" >&2; exit 1; }

cd "$root/web"
# exec: the ng process itself, so that a TERM (renders.sh stops what it started) reaches it.
NODE_EXTRA_CA_CERTS="$certs/ca.crt" QUADER_UI_DEV_BACKEND="https://localhost:$UI_BACKEND_PORT" \
  exec node_modules/.bin/ng serve --configuration local --port "$UI_WEB_PORT" --ssl-cert "$certs/server.crt" --ssl-key "$certs/server.key" "$@"
