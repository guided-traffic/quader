#!/bin/sh
# PID 1 of the Docker container of an OCI clients pod: puts the dev CA where dockerd looks for a registry's CA, optionally
# names a registry mirror in daemon.json, starts the forwarder (test/e2e/clients/fwd) in the background and becomes dockerd.
# The pod shares one network namespace, so the forwarder's listener on 127.0.0.1:<edge port> is the registry the daemon, the
# CLI and Podman reach; `localhost:<edge port>` is the base URL of the setup snippets.
#
#   QUADER_CA_FILE         the dev CA, PEM (default /etc/quader-ca/ca.crt, a ConfigMap mount)
#   QUADER_REGISTRY_HOSTS  the registry hosts, host[:port] separated by blanks; each gets /etc/docker/certs.d/<host>/ca.crt
#   QUADER_ENGINE_MIRROR   https URL of the Docker Engine mirror (registry-mirrors of daemon.json); empty: none
#   arguments              the forwarder's: -listen, -to, -target-file
set -eu
ca="${QUADER_CA_FILE:-/etc/quader-ca/ca.crt}"
[ -s "$ca" ] || { echo "entrypoint: the dev CA $ca is missing or empty" >&2; exit 1; }
for h in ${QUADER_REGISTRY_HOSTS:?QUADER_REGISTRY_HOSTS is not set}; do
  mkdir -p "/etc/docker/certs.d/$h"
  cp "$ca" "/etc/docker/certs.d/$h/ca.crt"
done
# BuildKit inside dockerd fetches the token of `buildx build --push` with a client of its own, which reads the system trust and not certs.d.
cp "$ca" /usr/local/share/ca-certificates/quader-dev-ca.crt
update-ca-certificates >/dev/null
mkdir -p /etc/docker
if [ -n "${QUADER_ENGINE_MIRROR:-}" ]; then
  printf '{"registry-mirrors":["%s"]}\n' "$QUADER_ENGINE_MIRROR" >/etc/docker/daemon.json
else
  printf '{}\n' >/etc/docker/daemon.json
fi
/usr/local/bin/fwd "$@" &
# dockerd by the image's own entrypoint (cgroup nesting, iptables flavour, docker-init), with the unix socket only: with no
# arguments the image opens an unauthenticated TCP listener, which every pod of the cluster could reach.
exec dockerd-entrypoint.sh dockerd --host=unix:///var/run/docker.sock
