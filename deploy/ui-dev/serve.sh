#!/usr/bin/env bash
# Writes quader.yaml again (config.sh), builds quader and the profile's sign-in provider (test/devidp) from the working tree,
# applies the migrations, starts the provider in the background and runs one replica of role all in the foreground against
# the services of deploy/ui-dev/up.sh. Ctrl-C stops both; run it again after a change of the Go code. UI_INSTANCE picks the
# instance (deploy/ui-dev/instance.sh), as for up.sh.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
state="$UI_STATE"
# quader.yaml is written again from the state directory of up.sh, so a change of the profile's configuration reaches it at this
# start; config.sh creates no certificate, secret or container and fails when up.sh has not run.
deploy/ui-dev/config.sh "$state"

bin="$state/bin/quader"
idp="$state/bin/devidp"
mkdir -p "$state/bin"
CGO_ENABLED=0 go build -trimpath -o "$bin" ./cmd/quader
CGO_ENABLED=0 go build -trimpath -o "$idp" ./test/devidp/cmd/devidp

"$bin" migrate up --config "$state/quader.yaml"

# The sign-in provider of the profile, on the loopback address; it ends with this script.
"$idp" -listen "127.0.0.1:$UI_IDP_PORT" -issuer "https://localhost:$UI_IDP_PORT" \
  -tls-cert "$state/certs/server.crt" -tls-key "$state/certs/server.key" \
  -client-id quader-ui-dev -client-secret-file "$state/secrets/idp-client-secret" -client-base "https://localhost:$UI_WEB_PORT" &
idp_pid=$!
trap 'kill "$idp_pid" 2>/dev/null || true' EXIT

cat <<EOF

  UI:        https://localhost:$UI_WEB_PORT/ui/   (make ui-dev-web UI_INSTANCE=$UI_INSTANCE; the certificate is signed by $state/certs/ca.crt)
  sign in:   admin / $(cat "$state/secrets/break-glass-password"), or as sam, priya, ada or noor (auditor) through "UI development users"
  backend:   https://127.0.0.1:$UI_BACKEND_PORT, ops https://127.0.0.1:$UI_OPS_PORT (instance $UI_INSTANCE)

EOF
# In the background and waited for, so that Ctrl-C or a TERM (make ui-renders stops a backend it started) reaches
# quader, and the provider ends with it.
"$bin" serve --role all --config "$state/quader.yaml" &
quader_pid=$!
stop() { kill -TERM "$quader_pid" 2>/dev/null || true; wait "$quader_pid" 2>/dev/null || true; }
trap stop INT TERM
wait "$quader_pid"
