#!/usr/bin/env bash
# Builds the clients image of the npm acceptance matrix (R-NPM-045) into the local Docker:
# the static forwarder (test/e2e/clients/fwd) for linux/<arch>, then test/e2e/clients/Dockerfile on the
# pinned Node image. `make npm-clients-image` and deploy/dev/e2e.sh both call it.
#
#   build.sh [image]    default image quader-e2e-clients:<DEV_TAG>
#
# Environment: QUADER_E2E_CLIENTS_ARCH (amd64|arm64, default the host's).
set -euo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../deploy/dev" && pwd)/lib.sh"

image="${1:-quader-e2e-clients:$DEV_TAG}"
arch="${QUADER_E2E_CLIENTS_ARCH:-$(arch)}"
for v in E2E_CLIENTS_NODE_IMAGE E2E_CLIENTS_CURL_VERSION E2E_CLIENTS_JQ_VERSION; do
  [ -n "${!v:-}" ] || die "$v is not set in tools/versions.env"
done
command -v docker >/dev/null 2>&1 || die "docker is required"

build="$(mktemp -d)"
trap 'rm -rf "$build"' EXIT
log "building the forwarder for linux/$arch"
(cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$build/fwd-linux-$arch" ./test/e2e/clients/fwd)
image_ci_context "$build/ci"
log "building $image on $E2E_CLIENTS_NODE_IMAGE"
docker buildx build --load --label quader.build=e2e-image --platform "linux/$arch" \
  --build-arg NODE_IMAGE="$E2E_CLIENTS_NODE_IMAGE" \
  --build-arg CURL_VERSION="$E2E_CLIENTS_CURL_VERSION" --build-arg JQ_VERSION="$E2E_CLIENTS_JQ_VERSION" --build-context "fwd=$build" --build-context "ci=$build/ci" \
  -f "$REPO_ROOT/test/e2e/clients/Dockerfile" -t "$image" "$REPO_ROOT/test/e2e/clients"
# The images the tags named before are untagged now; the label (the Makefile's E2E_IMAGE_BUILD_LABEL) keeps the prune to them.
docker image prune --force --filter dangling=true --filter label=quader.build=e2e-image >/dev/null 2>&1 || log "the prune of the untagged e2e images failed; it fails nothing"
