#!/usr/bin/env bash
# Starts the backing services of the UI development profile (deploy/ui-dev/compose.yaml), creates the
# buckets, and writes everything quader needs into .dev/ui-dev: the certificates, the secret files, the key
# ring, the break-glass password and, through config.sh, quader.yaml. Safe to run again: running containers, the data and
# every generated secret are kept. deploy/ui-dev/serve.sh starts quader against it. UI_INSTANCE=2..8 starts another instance
# beside instance 1, with its own containers, ports and state (deploy/ui-dev/instance.sh); a worktree claims
# a free one when it names none, and a number another checkout claims is refused.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

set -a
# shellcheck disable=SC1091
. tools/versions.env
set +a

# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
state="$UI_STATE"
certs="$state/certs"
secrets="$state/secrets"
mkdir -p "$state" "$secrets/keys" "$state/spool"

compose=(docker compose -f deploy/ui-dev/compose.yaml)
export POSTGRES_IMAGE="$INTEGRATION_POSTGRES_IMAGE"
export VALKEY_IMAGE="$VALKEY_91_IMAGE"
export SEAWEEDFS_IMAGE
export CERT_DIR="$certs"

# The integration stack's certificate script, into this profile's own directory: its own CA, the same
# SANs (localhost, 127.0.0.1). New material means containers that hold the old certificates are recreated;
# the volumes stay.
certs_out="$(test/integration/certs.sh "$certs")"
if [ -n "$certs_out" ]; then
  echo "$certs_out"
  "${compose[@]}" down --remove-orphans 2>/dev/null || true
fi

# Secret files, written once. The values of the backing services are fixed in compose.yaml and
# test/integration/conf/s3.json; the key ring and the break-glass password are random per checkout.
put() { # put <file> <value>: write only when missing, readable by the caller only
  [ -f "$1" ] && return 0
  (umask 077 && printf '%s\n' "$2" > "$1")
}
put "$secrets/db-user" quader
put "$secrets/db-pass" quader-ui-dev
put "$secrets/valkey-pass" quader-ui-dev-valkey
put "$secrets/s3-id" quader-it-access
put "$secrets/s3-secret" quader-it-secret-0123456789abcdef
put "$secrets/keys/dev.key" "$(head -c 32 /dev/urandom | base64)"
put "$secrets/break-glass-password" "$(head -c 18 /dev/urandom | base64 | tr '+/' '-_')"
# The client secret quader and the profile's sign-in provider (test/devidp, started by serve.sh) share.
put "$secrets/idp-client-secret" "$(head -c 18 /dev/urandom | base64 | tr '+/' '-_')"

"${compose[@]}" up -d --wait --wait-timeout 240

# Buckets: the integration stack's seed command, pointed at this profile's S3 store.
QUADER_TEST_S3_ENDPOINT="https://127.0.0.1:$UI_S3_PORT" QUADER_TEST_S3_REGION=us-east-1 \
  QUADER_TEST_S3_ACCESS_KEY="$(cat "$secrets/s3-id")" QUADER_TEST_S3_SECRET_KEY="$(cat "$secrets/s3-secret")" \
  QUADER_TEST_S3_BUCKET_BLOBS=quader-blobs QUADER_TEST_S3_BUCKET_BACKUPS=quader-backups \
  QUADER_TEST_CA_FILE="$certs/ca.crt" go run ./test/integration/cmd/seed

# quader.yaml: written by config.sh, which serve.sh runs at every start as well, so a change of the configuration reaches an
# existing state directory without this script.
deploy/ui-dev/config.sh "$state"

echo
echo "UI development services of instance $UI_INSTANCE ($UI_PROJECT) are up; configuration: $state/quader.yaml"
echo "next: make ui-dev-backend UI_INSTANCE=$UI_INSTANCE (quader on https://127.0.0.1:$UI_BACKEND_PORT), then make ui-dev-web UI_INSTANCE=$UI_INSTANCE (https://localhost:$UI_WEB_PORT/ui/)"
