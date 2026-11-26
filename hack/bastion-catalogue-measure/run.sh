#!/usr/bin/env bash
# Runs one script of the catalogue measurement against this checkout's instance of the UI development profile
# (deploy/ui-dev/instance.sh picks it; a worktree uses the one it claims, never instance 1 of the main checkout).
#
#   hack/bastion-catalogue-measure/run.sh generate.mjs
#   hack/bastion-catalogue-measure/run.sh measure.mjs <out-dir> <pass>
#   hack/bastion-catalogue-measure/run.sh volatile.mjs <out-dir> <run>
#   hack/bastion-catalogue-measure/run.sh report.mjs <out-dir>
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=deploy/ui-dev/instance.sh
. "$root/deploy/ui-dev/instance.sh"
ui_instance "$root"
script="$1"
shift
NODE_EXTRA_CA_CERTS="$UI_STATE/certs/ca.crt" exec node "$root/hack/bastion-catalogue-measure/$script" "$@"
