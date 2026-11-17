#!/usr/bin/env bash
# Starts the integration stack (TLS on every connection, verified from the host), waits until it is healthy, creates the buckets
# and writes .dev/integration/env with the QUADER_TEST_* variables the tests read
# (see test/integration/testenv). Safe to run again: running containers are kept.
#
#   Valkey runs on the 9.1 line only: quader is in development and guarantees no older line.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

set -a
# shellcheck disable=SC1091
. tools/versions.env
set +a

line="${QUADER_TEST_VALKEY_LINE:-9.1}"
case "$line" in
  9.1) VALKEY_IMAGE="$VALKEY_91_IMAGE" ;;
  *) echo "QUADER_TEST_VALKEY_LINE must be 9.1 (got '$line')" >&2; exit 2 ;;
esac

state="$root/.dev/integration"
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
  docker compose -f test/integration/compose.yaml down -v --remove-orphans 2>/dev/null || true
fi

# A different Valkey line needs fresh data nodes: Sentinel rewrote their configs.
if [ -f "$state/valkey-line" ] && [ "$(cat "$state/valkey-line")" != "$line" ]; then
  echo "Valkey line changed from $(cat "$state/valkey-line") to $line: recreating the stack"
  docker compose -f test/integration/compose.yaml down -v --remove-orphans
fi
echo "$line" > "$state/valkey-line"

docker compose -f test/integration/compose.yaml up -d --wait --wait-timeout 240

env_file="$state/env"
q() { printf "'%s'" "$1"; }
{
  echo "# Written by test/integration/up.sh; sourced by make test-integration."
  echo "QUADER_TEST_CA_FILE=$(q "$state/certs/ca.crt")"
  echo "QUADER_TEST_CERT_DIR=$(q "$state/certs")"
  echo "QUADER_TEST_PG_DSN=$(q "postgres://quader:quader-it@127.0.0.1:15432/quader?sslmode=verify-full&sslrootcert=$state/certs/ca.crt")"
  echo "QUADER_TEST_PG_PGBOUNCER_DSN=$(q "postgres://quader:quader-it@127.0.0.1:16432/quader?sslmode=verify-full&sslrootcert=$state/certs/ca.crt")"
  echo "QUADER_TEST_PG_HOST=127.0.0.1"
  echo "QUADER_TEST_PG_PORT=15432"
  echo "QUADER_TEST_PG_PGBOUNCER_PORT=16432"
  echo "QUADER_TEST_PG_USER=quader"
  echo "QUADER_TEST_PG_PASSWORD=quader-it"
  echo "QUADER_TEST_PG_DATABASE=quader"
  echo "QUADER_TEST_VALKEY_LINE=$line"
  echo "QUADER_TEST_VALKEY_SENTINELS=127.0.0.1:26381,127.0.0.1:26382,127.0.0.1:26383"
  echo "QUADER_TEST_VALKEY_NODES=127.0.0.1:16379,127.0.0.1:16380"
  echo "QUADER_TEST_VALKEY_PRIMARY_NAME=quader"
  echo "QUADER_TEST_VALKEY_USERNAME=quader"
  echo "QUADER_TEST_VALKEY_PASSWORD=quader-valkey-it"
  echo "QUADER_TEST_VALKEY_SENTINEL_PASSWORD=sentinel-valkey-it"
  echo "QUADER_TEST_VALKEY_ADMIN_USERNAME=ops"
  echo "QUADER_TEST_VALKEY_ADMIN_PASSWORD=ops-valkey-it"
  echo "QUADER_TEST_VALKEY_CA_FILE=$(q "$state/certs/ca.crt")"
  echo "QUADER_TEST_VALKEY_KEY_PREFIX=quader:"
  echo "QUADER_TEST_VALKEY_CONTAINERS=quader-it-valkey-primary,quader-it-valkey-replica,quader-it-valkey-sentinel-1,quader-it-valkey-sentinel-2,quader-it-valkey-sentinel-3"
  echo "QUADER_TEST_S3_ENDPOINT=https://127.0.0.1:18333"
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
echo "integration stack is up (Valkey $line); environment: $env_file"
