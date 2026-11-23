#!/usr/bin/env bash
# Stops the backing services of the UI development profile. The data volumes are kept unless --purge is
# given; --purge also removes the generated state in .dev/ui-dev (certificates, secrets, key ring). UI_INSTANCE picks the
# instance (deploy/ui-dev/instance.sh): its project and its state directory, nothing of another instance.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
# compose.yaml requires the variables up.sh sets; the values do not matter for down.
export POSTGRES_IMAGE=unused VALKEY_IMAGE=unused SEAWEEDFS_IMAGE=unused CERT_DIR="$UI_STATE/certs"

if [ "${1:-}" = "--purge" ]; then
  docker compose -f deploy/ui-dev/compose.yaml down -v --remove-orphans
  rm -rf "$UI_STATE"
  echo "UI development services of instance $UI_INSTANCE ($UI_PROJECT) removed with their data and $UI_STATE"
else
  docker compose -f deploy/ui-dev/compose.yaml down --remove-orphans
  echo "UI development services of instance $UI_INSTANCE ($UI_PROJECT) stopped; the data is kept (deploy/ui-dev/down.sh --purge removes it)"
fi
