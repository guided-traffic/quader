#!/usr/bin/env bash
# Builds the client images of the OCI acceptance suite (R-OCI-035, Q-OCI-21) into the local Docker: the static forwarder
# (test/e2e/clients/fwd, the one of the npm clients) for linux/<arch>, then Dockerfile.docker on the pinned dind image,
# Dockerfile.podman on the pinned Podman image and Dockerfile.cosign, the pinned cosign binary alone (signatures.md R-SIG-052).
# `make oci-clients-image` and deploy/dev/e2e.sh both call it.
#
#   build.sh [docker-image [podman-image [cosign-image]]]
#     default quader-e2e-ociclients-docker:<DEV_TAG>, quader-e2e-ociclients-podman:<DEV_TAG> and quader-e2e-ociclients-cosign:<DEV_TAG>
#
# Environment: QUADER_E2E_CLIENTS_ARCH (amd64|arm64, default the host's).
set -euo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../deploy/dev" && pwd)/lib.sh"

docker_image="${1:-quader-e2e-ociclients-docker:$DEV_TAG}"
podman_image="${2:-quader-e2e-ociclients-podman:$DEV_TAG}"
cosign_image="${3:-quader-e2e-ociclients-cosign:$DEV_TAG}"
arch="${QUADER_E2E_CLIENTS_ARCH:-$(arch)}"
for v in E2E_OCICLIENTS_DOCKER_IMAGE E2E_OCICLIENTS_PODMAN_IMAGE E2E_OCICLIENTS_COSIGN_IMAGE; do
  [ -n "${!v:-}" ] || die "$v is not set in tools/versions.env"
done
command -v docker >/dev/null 2>&1 || die "docker is required"

build="$(mktemp -d)"
trap 'rm -r "${build:?}"' EXIT
log "building the forwarder for linux/$arch"
(cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$build/fwd-linux-$arch" ./test/e2e/clients/fwd)
dir="$REPO_ROOT/test/e2e/ociclients"
log "building $docker_image on $E2E_OCICLIENTS_DOCKER_IMAGE"
docker buildx build --load --label quader.build=e2e-image --platform "linux/$arch" --build-arg DOCKER_IMAGE="$E2E_OCICLIENTS_DOCKER_IMAGE" \
  --build-context "fwd=$build" -f "$dir/Dockerfile.docker" -t "$docker_image" "$dir"
log "building $podman_image on $E2E_OCICLIENTS_PODMAN_IMAGE"
docker buildx build --load --label quader.build=e2e-image --platform "linux/$arch" --build-arg PODMAN_IMAGE="$E2E_OCICLIENTS_PODMAN_IMAGE" \
  -f "$dir/Dockerfile.podman" -t "$podman_image" "$dir"
log "building $cosign_image on $E2E_OCICLIENTS_COSIGN_IMAGE"
docker buildx build --load --label quader.build=e2e-image --platform "linux/$arch" --build-arg COSIGN_IMAGE="$E2E_OCICLIENTS_COSIGN_IMAGE" \
  -f "$dir/Dockerfile.cosign" -t "$cosign_image" "$dir"
# The images the tags named before are untagged now; the label (the Makefile's E2E_IMAGE_BUILD_LABEL) keeps the prune to them.
docker image prune --force --filter dangling=true --filter label=quader.build=e2e-image >/dev/null 2>&1 || log "the prune of the untagged e2e images failed; it fails nothing"
