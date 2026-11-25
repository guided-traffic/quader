#!/usr/bin/env bash
# Deletes the kind cluster "quader" and the profile's local state (.dev/logs; .dev/integration is left alone). Other kind clusters are not
# touched.
set -euo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

if command -v kind >/dev/null 2>&1 && kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  log "deleting the kind cluster '$CLUSTER'"
  kind delete cluster --name "$CLUSTER"
else
  log "no kind cluster '$CLUSTER'"
fi
# .dev/ is shared with the integration stack (.dev/integration, its certificates and env file), which
# a dev-profile teardown must not touch: only this profile's own directories go.
if [ -d "$STATE_DIR/logs" ]; then
  log "removing $STATE_DIR/logs"
  rm -rf "$STATE_DIR/logs"
fi
# The copies of the upstream registries' accounts the cluster used (upstream-accounts.sh); the file they came from, under
# .dev/credentials, is the owner's and stays.
if [ -d "$UPSTREAM_ACCOUNTS_DIR" ]; then
  log "removing $UPSTREAM_ACCOUNTS_DIR"
  rm -rf "$UPSTREAM_ACCOUNTS_DIR"
fi
