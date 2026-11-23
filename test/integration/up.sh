#!/usr/bin/env bash
# Starts an instance of the integration stack (TLS on every connection, verified from the host), waits until it is healthy,
# creates the buckets and writes the instance's env file with the QUADER_TEST_* variables the tests read
# (see test/integration/testenv). Safe to run again: running containers are kept.
#
#   up.sh [instance]    instance 1 to 9, default $QUADER_TEST_INSTANCE or 1 (`make integration-up INSTANCE=2`)
#
# Instance 1 is the stack the pipeline starts: containers quader-it-*, .dev/integration/env. Instance n is quader-it<n>-* with
# .dev/integration-<n>/env and ports of its own (test/integration/instance.sh). Start instances in the main checkout: the
# instances belong to the machine, and locked.sh and run.sh look for their env files there.
#
#   Valkey runs on the 9.1 line only: quader is in development and guarantees no older line.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
# shellcheck source=test/integration/instance.sh
. test/integration/instance.sh
it_instance "${1:-${QUADER_TEST_INSTANCE:-1}}" "$root"

set -a
# shellcheck disable=SC1091
. tools/versions.env
set +a

line="${QUADER_TEST_VALKEY_LINE:-9.1}"
case "$line" in
  9.1) VALKEY_IMAGE="$VALKEY_91_IMAGE" ;;
  *) echo "QUADER_TEST_VALKEY_LINE must be 9.1 (got '$line')" >&2; exit 2 ;;
esac

state="$IT_STATE"
mkdir -p "$state"
certs_out="$(test/integration/certs.sh "$state/certs")"

export VALKEY_IMAGE
export POSTGRES_IMAGE="$INTEGRATION_POSTGRES_IMAGE"
export PGBOUNCER_IMAGE="$INTEGRATION_PGBOUNCER_IMAGE"
export SEAWEEDFS_IMAGE
export TOOLS_IMAGE="$INTEGRATION_TOOLS_IMAGE"
export CERT_DIR="$state/certs"

# New certificates: containers that started with the old ones would fail the
# tests' certificate verification, so start from scratch.
if [ -n "$certs_out" ]; then
  echo "$certs_out"
  docker compose -p "$IT_PROJECT" -f test/integration/compose.yaml down -v --remove-orphans 2>/dev/null || true
fi

# A different Valkey line needs fresh data nodes: Sentinel rewrote their configs.
if [ -f "$state/valkey-line" ] && [ "$(cat "$state/valkey-line")" != "$line" ]; then
  echo "Valkey line changed from $(cat "$state/valkey-line") to $line: recreating the stack"
  docker compose -p "$IT_PROJECT" -f test/integration/compose.yaml down -v --remove-orphans
fi
echo "$line" > "$state/valkey-line"

docker compose -p "$IT_PROJECT" -f test/integration/compose.yaml up -d --wait --wait-timeout 240

env_file="$state/env"
q() { printf "'%s'" "$1"; }
{
  echo "# Written by test/integration/up.sh for instance $IT_INSTANCE ($IT_PROJECT); sourced by locked.sh, run.sh and make test-integration."
  echo "QUADER_TEST_INSTANCE=$IT_INSTANCE"
  echo "QUADER_TEST_CA_FILE=$(q "$state/certs/ca.crt")"
  echo "QUADER_TEST_CERT_DIR=$(q "$state/certs")"
  echo "QUADER_TEST_PG_DSN=$(q "postgres://quader:quader-it@127.0.0.1:$IT_PG_PORT/quader?sslmode=verify-full&sslrootcert=$state/certs/ca.crt")"
  echo "QUADER_TEST_PG_PGBOUNCER_DSN=$(q "postgres://quader:quader-it@127.0.0.1:$IT_PGBOUNCER_PORT/quader?sslmode=verify-full&sslrootcert=$state/certs/ca.crt")"
  echo "QUADER_TEST_PG_HOST=127.0.0.1"
  echo "QUADER_TEST_PG_PORT=$IT_PG_PORT"
  echo "QUADER_TEST_PG_PGBOUNCER_PORT=$IT_PGBOUNCER_PORT"
  echo "QUADER_TEST_PG_USER=quader"
  echo "QUADER_TEST_PG_PASSWORD=quader-it"
  echo "QUADER_TEST_PG_DATABASE=quader"
  echo "QUADER_TEST_VALKEY_LINE=$line"
  echo "QUADER_TEST_VALKEY_SENTINELS=127.0.0.1:$IT_SENTINEL_1_PORT,127.0.0.1:$IT_SENTINEL_2_PORT,127.0.0.1:$IT_SENTINEL_3_PORT"
  echo "QUADER_TEST_VALKEY_NODES=127.0.0.1:$IT_VALKEY_PRIMARY_PORT,127.0.0.1:$IT_VALKEY_REPLICA_PORT"
  echo "QUADER_TEST_VALKEY_PRIMARY_NAME=quader"
  echo "QUADER_TEST_VALKEY_USERNAME=quader"
  echo "QUADER_TEST_VALKEY_PASSWORD=quader-valkey-it"
  echo "QUADER_TEST_VALKEY_SENTINEL_PASSWORD=sentinel-valkey-it"
  echo "QUADER_TEST_VALKEY_ADMIN_USERNAME=ops"
  echo "QUADER_TEST_VALKEY_ADMIN_PASSWORD=ops-valkey-it"
  echo "QUADER_TEST_VALKEY_CA_FILE=$(q "$state/certs/ca.crt")"
  echo "QUADER_TEST_VALKEY_KEY_PREFIX=quader:"
  echo "QUADER_TEST_VALKEY_CONTAINERS=$IT_PROJECT-valkey-primary,$IT_PROJECT-valkey-replica,$IT_PROJECT-valkey-sentinel-1,$IT_PROJECT-valkey-sentinel-2,$IT_PROJECT-valkey-sentinel-3"
  echo "QUADER_TEST_S3_ENDPOINT=https://127.0.0.1:$IT_S3_PORT"
  echo "QUADER_TEST_S3_REGION=us-east-1"
  echo "QUADER_TEST_S3_ACCESS_KEY=quader-it-access"
  echo "QUADER_TEST_S3_SECRET_KEY=quader-it-secret-0123456789abcdef"
  echo "QUADER_TEST_S3_BUCKET_BLOBS=quader-blobs"
  echo "QUADER_TEST_S3_BUCKET_BACKUPS=quader-backups"
  echo "QUADER_TEST_S3_FORCE_PATH_STYLE=true"
} > "$env_file.tmp"
mv "$env_file.tmp" "$env_file"

# Buckets: quader-blobs with versioning on, quader-backups.
set -a
# shellcheck disable=SC1090
. "$env_file"
set +a
go run ./test/integration/cmd/seed

echo
echo "integration stack instance $IT_INSTANCE ($IT_PROJECT) is up (Valkey $line); environment: $env_file"
