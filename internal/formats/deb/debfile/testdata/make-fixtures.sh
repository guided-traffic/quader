#!/bin/sh
# Builds the package files of the reader's real-tool fixtures (R-DEB-039) with dpkg-deb in the pinned Debian 13 image of the APT
# clients (tools/versions.env, E2E_APTCLIENTS_DEBIAN13_IMAGE): the same tree compressed with xz (Debian's default), Zstandard (Ubuntu's)
# and gzip. Run it from anywhere; it writes quader-docs_1.2.0-1_all.<compression>.deb next to itself. The content is the repository's
# own.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
image=$(sed -n 's/^E2E_APTCLIENTS_DEBIAN13_IMAGE=//p' "$here/../../../../../tools/versions.env")
docker run --rm -v "$here:/out" "$image" sh -eu /out/build-in-image.sh
