#!/bin/sh
# Entrypoint of an APT client pod (test/e2e/aptclients/Dockerfile): APT and curl trust the dev CA, the image's own sources are gone
# (the client talks to quader only), and the forwarder carries 127.0.0.1:<edge port> to the in-cluster edge, so that the base URL of
# the setup snippets (https://localhost:<edge port>/api/deb/...) works unchanged and its certificate is verified for `localhost`.
# The arguments are the forwarder's.
set -eu
: "${QUADER_CA_FILE:?}"
rm -f /etc/apt/sources.list
rm -f /etc/apt/sources.list.d/*
mkdir -p /etc/apt/keyrings /etc/apt/auth.conf.d /etc/apt/apt.conf.d
printf 'Acquire::https::CAInfo "%s";\n' "$QUADER_CA_FILE" >/etc/apt/apt.conf.d/99quader-ca
printf 'cacert = "%s"\n' "$QUADER_CA_FILE" >/root/.curlrc
exec /usr/local/bin/fwd "$@"
