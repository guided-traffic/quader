#!/usr/bin/env bash
# Stops the backing services of the UI development profile. The data volumes are kept unless --purge is
# given; --purge also removes the generated state in .dev/ui-dev (certificates, secrets, key ring) and ends the checkout's claim of
# the instance. UI_INSTANCE picks the instance, a worktree's claimed one when it is unset (deploy/ui-dev/instance.sh): its project
# and its state directory, nothing of another instance.
# docs/developer/web-ui.md#local-ui-development
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
# UI_DOWN: an instance started before claims existed has none, and its owner may still end it (instance.sh).
UI_DOWN=1 ui_instance "$root"
# compose.yaml requires the variables up.sh sets; the values do not matter for down.
export POSTGRES_IMAGE=unused VALKEY_IMAGE=unused SEAWEEDFS_IMAGE=unused CERT_DIR="$UI_STATE/certs"
# An analyser's container of the image scanner (scanners.sh) that a killed call left behind.
docker ps -aq --filter "label=quader-ui-dev-analyser=$UI_PROJECT" | xargs docker rm -f >/dev/null 2>&1 || true

if [ "${1:-}" = "--purge" ]; then
  docker compose -f deploy/ui-dev/compose.yaml down -v --remove-orphans
  rm -rf "$UI_STATE"
  ui_release "$root"
  echo "UI development services of instance $UI_INSTANCE ($UI_PROJECT) removed with their data and $UI_STATE"
else
  docker compose -f deploy/ui-dev/compose.yaml down --remove-orphans
  echo "UI development services of instance $UI_INSTANCE ($UI_PROJECT) stopped; the data is kept (deploy/ui-dev/down.sh --purge removes it)"
fi
