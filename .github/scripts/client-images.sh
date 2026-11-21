#!/usr/bin/env bash
# The client images of the acceptance suites, built once per change of their inputs and reused (Q-ARCH-520, Q-ARCH-521):
#
#   client-images.sh key <set>                  prints the cache key of the set's images
#   client-images.sh load-or-build <set> <tar>  loads the images from <tar> when it is there, else builds them and writes <tar>
#
# <set> is npm, nuget, oci, apt or ansible: the images of `make <set>-clients-image`, under the names deploy/dev/e2e.sh loads
# into kind. In CI the composite .github/actions/client-images runs `key`, restores <tar> from the GitHub Actions cache under
# that key, runs `load-or-build` and saves <tar> when it was built; no workflow step builds a clients image another way (a
# test of internal/tools/ciworkflows holds that).
#
# Why: each build installs OS packages and tools from hosts outside (the Ubuntu and Debian archives, npm, NuGet, PyPI,
# Microsoft's registry, Docker Hub, quay.io). In Test and Release 44519652288 (2026-11-19) `e2e (nuget)` and `e2e (ci)` failed in
# the image build after ten minutes because archive.ubuntu.com did not answer (F-158), although nothing the image is made of had
# changed. With the cache a leg whose inputs did not change loads the images from the tar and contacts none of those hosts.
#
# The key is a hash of everything the images are made of, so that a changed input rebuilds and nothing else does:
#   - the files of the set's build context (its directory under test/e2e, node_modules left out): the Dockerfiles, build.sh,
#     the entrypoints, the lockfiles and constraint files;
#   - the pins of tools/versions.env the build reads (name and value);
#   - the forwarder test/e2e/clients/fwd that every set copies in (all images but Podman's), as the SHA-256 of the binary itself, built
#     here for linux/<arch>: its source, its dependencies and the Go toolchain all end up in that hash (the build is
#     reproducible: -trimpath, no cgo);
#   - this script, so that a change of what it writes or loads rebuilds;
#   - the architecture.
# What it leaves out on purpose: the base images are named by digest in the pins, so the pins cover them; the apt, pip and
# npm indexes at build time are not inputs of the repository (the pins name exact versions where the image needs one).
#
# A tar that does not load (a partial or corrupt cache entry) is a warning, not a failure: the script deletes it and builds.
# On a build it writes the tar under a temporary name and renames it at the end, so a failed save leaves no tar behind.
#
# Writes built=true|false (and for `key`: key=...) to GITHUB_OUTPUT when that is set.
set -euo pipefail

usage() { echo "usage: client-images.sh key <set> | load-or-build <set> <tar>   (set: npm, nuget, oci, apt, ansible)" >&2; exit 2; }
[ "$#" -ge 2 ] || usage
command="$1" set_name="$2"

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) arch="$(uname -m)" ;;
esac

# The images of each set (the names deploy/dev/e2e.sh loads), its build context and the pins its build.sh reads.
case "$set_name" in
  npm)
    dir=test/e2e/clients
    images=(quader-e2e-clients:dev)
    pins=(E2E_CLIENTS_NODE_IMAGE E2E_CLIENTS_CURL_VERSION E2E_CLIENTS_JQ_VERSION)
    build() { "$repo_root/test/e2e/clients/build.sh" "${images[0]}"; } ;;
  nuget)
    dir=test/e2e/nugetclients
    images=(quader-e2e-nugetclients:dev)
    pins=(E2E_NUGETCLIENTS_SDK_IMAGE E2E_NUGETCLIENTS_SDK8_IMAGE E2E_NUGETCLIENTS_SDK9_IMAGE E2E_NUGETCLIENTS_MONO_VERSION
      E2E_NUGETCLIENTS_JQ_VERSION E2E_NUGETCLIENTS_NUGET_EXE_VERSION E2E_NUGETCLIENTS_NUGET_EXE_SHA256)
    build() { "$repo_root/test/e2e/nugetclients/build.sh" "${images[0]}"; } ;;
  oci)
    dir=test/e2e/ociclients
    images=(quader-e2e-ociclients-docker:dev quader-e2e-ociclients-podman:dev quader-e2e-ociclients-cosign:dev)
    pins=(E2E_OCICLIENTS_DOCKER_IMAGE E2E_OCICLIENTS_PODMAN_IMAGE E2E_OCICLIENTS_COSIGN_IMAGE)
    build() { "$repo_root/test/e2e/ociclients/build.sh" "${images[0]}" "${images[1]}" "${images[2]}"; } ;;
  apt)
    dir=test/e2e/aptclients
    images=(quader-e2e-aptclients-debian13:dev quader-e2e-aptclients-debian12:dev quader-e2e-aptclients-ubuntu2404:dev quader-e2e-aptclients-ubuntu2204:dev)
    pins=(E2E_APTCLIENTS_DEBIAN13_IMAGE E2E_APTCLIENTS_DEBIAN12_IMAGE E2E_APTCLIENTS_UBUNTU2404_IMAGE E2E_APTCLIENTS_UBUNTU2204_IMAGE)
    build() { "$repo_root/test/e2e/aptclients/build.sh" quader-e2e-aptclients-; } ;;
  ansible)
    dir=test/e2e/ansibleclients
    images=(quader-e2e-ansibleclients-core217:dev quader-e2e-ansibleclients-core221:dev)
    pins=(E2E_ANSIBLECLIENTS_CORE217_IMAGE E2E_ANSIBLECLIENTS_CORE217_VERSION E2E_ANSIBLECLIENTS_CORE221_IMAGE E2E_ANSIBLECLIENTS_CORE221_VERSION)
    build() { "$repo_root/test/e2e/ansibleclients/build.sh" quader-e2e-ansibleclients-; } ;;
  *) usage ;;
esac

output() { if [ -n "${GITHUB_OUTPUT:-}" ]; then echo "$1" >> "$GITHUB_OUTPUT"; fi; }

sha() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum | awk '{print $1}'; else shasum -a 256 | awk '{print $1}'; fi
}

# inputs prints one line per input of the key: the lines are sorted where their order is not fixed, so the same inputs give
# the same text on every runner.
inputs() {
  echo "set $set_name"
  echo "arch linux/$arch"
  echo "script $(sha < "$script")"
  local pattern
  pattern="$(IFS='|'; echo "${pins[*]}")"
  grep -E "^(${pattern})=" "$repo_root/tools/versions.env" | LC_ALL=C sort
  local f
  while IFS= read -r f; do
    echo "file $f $(sha < "$repo_root/$f")"
  done < <(cd "$repo_root" && find "$dir" -type f ! -path '*/node_modules/*' | LC_ALL=C sort)
  local tmp
  tmp="$(mktemp -d)"
  # The binary build.sh copies into the image, built the same way: its hash covers the source, the modules and the toolchain.
  (cd "$repo_root" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$tmp/fwd" ./test/e2e/clients/fwd)
  echo "forwarder $(sha < "$tmp/fwd")"
  rm -rf "$tmp"
}

case "$command" in
  key)
    [ "$#" -eq 2 ] || usage
    text="$(inputs)"
    key="e2e-clients-${set_name}-linux-${arch}-$(printf '%s\n' "$text" | sha)"
    printf '%s\n' "$text" | sed 's/^/client-images: /' >&2
    echo "$key"
    output "key=$key"
    ;;
  load-or-build)
    [ "$#" -eq 3 ] || usage
    tar="$3"
    trap 'rm -f "$tar.part"' EXIT
    if [ -s "$tar" ]; then
      start=$SECONDS
      if docker load -i "$tar" && docker image inspect "${images[@]}" >/dev/null; then
        echo "client-images: loaded ${images[*]} from $tar in $((SECONDS - start)) s; nothing built, nothing fetched"
        output "built=false"
        exit 0
      fi
      echo "::warning::client-images: the cached images of the set $set_name did not load from $tar; building them"
      rm -f "$tar"
    fi
    start=$SECONDS
    build
    mkdir -p "$(dirname "$tar")"
    docker save -o "$tar.part" "${images[@]}"
    mv "$tar.part" "$tar"
    echo "client-images: built ${images[*]} and wrote $tar ($(du -h "$tar" | awk '{print $1}')) in $((SECONDS - start)) s"
    output "built=true"
    ;;
  *) usage ;;
esac
