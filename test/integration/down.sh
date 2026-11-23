#!/usr/bin/env bash
# Stops an instance of the integration stack and removes its volumes and the generated env file.
# The certificates in the instance's directory (.dev/integration/certs for instance 1) are kept.
#
#   down.sh [instance]    instance 1 to 9, default $QUADER_TEST_INSTANCE or 1 (`make integration-down INSTANCE=2`)
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
export VALKEY_IMAGE="$VALKEY_91_IMAGE" POSTGRES_IMAGE="$INTEGRATION_POSTGRES_IMAGE" PGBOUNCER_IMAGE="$INTEGRATION_PGBOUNCER_IMAGE" \
  SEAWEEDFS_IMAGE TOOLS_IMAGE="$INTEGRATION_TOOLS_IMAGE" CERT_DIR="$IT_STATE/certs"

docker compose -p "$IT_PROJECT" -f test/integration/compose.yaml down -v --remove-orphans
rm -f "$IT_STATE/env" "$IT_STATE/valkey-line"
