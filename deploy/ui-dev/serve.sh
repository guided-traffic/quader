#!/usr/bin/env bash
# Writes quader.yaml again (config.sh), builds quader and the profile's sign-in provider (test/devidp) from the working tree,
# applies the migrations, starts the provider in the background, the image scanner (scanners.sh) and the malware scanner (malware.sh;
# UI_SCANNERS=0 runs neither, UI_MALWARE=0 no malware scanner), and runs one replica of role all in the foreground against the services
# of deploy/ui-dev/up.sh. Ctrl-C stops them all; run it again after a change of the Go code. UI_INSTANCE picks the instance
# (deploy/ui-dev/instance.sh), as for up.sh.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
state="$UI_STATE"
# A second start beside a running one failed on the port only after it had started scanners of its own, and its end stopped the
# running one's malware scanner (seen 2026-11-25): it is refused here, before anything is touched.
if (exec 3<>"/dev/tcp/127.0.0.1/$UI_BACKEND_PORT") 2>/dev/null; then
  echo "serve: the backend of instance $UI_INSTANCE answers on port $UI_BACKEND_PORT already; stop it first (Ctrl-C where it runs, or TERM to its serve.sh)" >&2
  exit 1
fi
# The accounts of the upstream registries (deploy/dev/upstream-accounts.sh, Q-ARCH-1066, Q-ARCH-1067): instance 1 reads the main
# checkout's .dev/credentials/upstream-registries.env, every other instance only the file UPSTREAM_ACCOUNTS_FILE names; the files go to
# $state/upstream-accounts, and config.sh declares an account for each registry that has them.
deploy/dev/upstream-accounts.sh files "$state/upstream-accounts" "$([ "$UI_INSTANCE" = 1 ] && echo main || echo none)"
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
# The image scanner vuln-scanner-trivy and its database's loader, the role scanner of the kinds trivy and trivy-db (Q-ARCH-1064), each
# logging to its own file, and the task that fetches the database once quader is ready; they end with this script. UI_SCANNERS=0 runs
# none of them.
pids=("$idp_pid")
# The scanner units' processes, which stop_scanners ends before quader (below).
scanner_pids=()
# stop_scanners ends the scanner units while quader's scan listener still runs, as the kubelet stops a pod while packages serves: each
# signs off and its row leaves the System page at once (Q-HA-495). Stopped after quader, every sign-off and draining report met a closed
# listener, and every restart left the rows of before listed as lost (seen 2026-11-25). The malware scanner's container gets TERM from
# malware.sh down.
stop_scanners() {
  if [ "${#scanner_pids[@]}" -gt 0 ]; then
    kill -TERM "${scanner_pids[@]}" 2>/dev/null || true
    wait "${scanner_pids[@]}" 2>/dev/null || true
  fi
  deploy/ui-dev/malware.sh down
}
trap 'stop_scanners; kill "${pids[@]}" 2>/dev/null || true; deploy/ui-dev/scanners.sh down' EXIT
if [ "${UI_SCANNERS:-1}" != 0 ]; then
  deploy/ui-dev/scanners.sh prepare
  # QUADER_POD_NAME names both processes after the unit, as the chart gives both containers of the pod the pod's name; only the
  # scanner reports itself, and a row's key is its process's UID, so two processes of one name never share a row (Q-HA-496).
  QUADER_POD_NAME="$UI_PROJECT-vuln-scanner-trivy" "$bin" serve --role scanner --config "$state/scanners/trivy.yaml" \
    > "$state/scanners/trivy.log" 2>&1 &
  scanner_pids+=("$!")
  QUADER_POD_NAME="$UI_PROJECT-vuln-scanner-trivy" "$bin" serve --role scanner --config "$state/scanners/trivy-db.yaml" \
    > "$state/scanners/trivy-db.log" 2>&1 &
  scanner_pids+=("$!")
  deploy/ui-dev/scanners.sh database &
  pids+=("$!")
  echo "scanners: vuln-scanner-trivy on https://127.0.0.1:$UI_TRIVY_PORT with its loader (logs: $state/scanners/trivy.log, trivy-db.log)"
fi
# The malware scanner malware-scanner-clamav, the chart's pod on Docker, and the updater malware-updater-freshclam, a process here, which
# starts once ClamAV's databases are in its directory (deploy/ui-dev/malware.sh, Q-ARCH-1065): they end with this script.
# UI_SCANNERS=0 or UI_MALWARE=0 runs none of them.
if [ "${UI_SCANNERS:-1}" != 0 ] && [ "${UI_MALWARE:-1}" != 0 ]; then
  deploy/ui-dev/malware.sh prepare
  deploy/ui-dev/malware.sh up
  for c in quader clamd; do
    docker logs -f "$UI_PROJECT-malware-scanner-clamav-$c" > "$state/scanners/clamav-$c.log" 2>&1 &
    pids+=("$!")
  done
  # QUADER_POD_NAME names the replica as the chart's pod name does, so the System page lists the unit by its name (Q-HA-373).
  (deploy/ui-dev/malware.sh signatures &&
    QUADER_POD_NAME="$UI_PROJECT-malware-updater-freshclam" exec "$bin" serve --role scanner --config "$state/scanners/freshclam.yaml") \
    > "$state/scanners/freshclam.log" 2>&1 &
  scanner_pids+=("$!")
  echo "malware: malware-scanner-clamav on https://127.0.0.1:$UI_CLAMAV_PORT (containers $UI_PROJECT-malware-scanner-clamav-quader and -clamd)," \
    "malware-updater-freshclam on https://127.0.0.1:$UI_FRESHCLAM_PORT (logs: $state/scanners/clamav-quader.log, clamav-clamd.log, freshclam.log)"
fi

cat <<EOF

  UI:        https://localhost:$UI_WEB_PORT/ui/   (make ui-dev-web UI_INSTANCE=$UI_INSTANCE; the certificate is signed by $state/certs/ca.crt)
  sign in:   admin / $(cat "$state/secrets/break-glass-password"), or as sam, priya, ada or noor (auditor) through "UI development users"
  backend:   https://127.0.0.1:$UI_BACKEND_PORT, ops https://127.0.0.1:$UI_OPS_PORT (instance $UI_INSTANCE)

EOF
# In the background and waited for, so that Ctrl-C or a TERM (make ui-renders stops a backend it started) reaches
# quader, and the provider ends with it.
"$bin" serve --role all --config "$state/quader.yaml" &
quader_pid=$!
stop() {
  stop_scanners
  kill -TERM "$quader_pid" 2>/dev/null || true
  wait "$quader_pid" 2>/dev/null || true
}
trap stop INT TERM
wait "$quader_pid"
