#!/usr/bin/env bash
# The image scanner of the UI development profile (Q-ARCH-1064): what the owner looks at behaves as the product does, so an image
# stored on the profile is scanned for vulnerabilities as R-VULN-050 says. serve.sh runs it; nobody needs to call it by hand.
#
#   scanners.sh prepare   the pair of vuln-scanner-trivy, the configuration of its two processes (the role scanner of the kind trivy
#                         and the loader of its database, the kind trivy-db: the chart's two containers of the pod) and the
#                         analyser's wrapper; serve.sh then starts both processes beside quader and ends them with it
#   scanners.sh database  waits for quader, signs in as the break-glass account and starts the task vuln-sync-trivy-db once, which
#                         makes upstream/scanner-trivy hold the database (about 142 MB from mirror.gcr.io the first time, a check of
#                         the tag's document after that); serve.sh runs it in the background
#   scanners.sh down      removes an analyser's container that a killed call left behind
#
# UI_SCANNERS=0 runs none: config.sh then writes no scan listener and no scanner's address, and the profile's images say "No database"
# or "No scanner" (R-VULN-039). The render runs set it (renders.sh): their screens' scanner replicas are rows the seed writes.
#
# The processes are quader's, built from the working tree for this machine (serve.sh), with the configuration the chart gives a scanner
# pod. The analyser is the product's pin, Trivy of TRIVY_IMAGE, whose binary is built for Linux only: vulnerabilities.images.binary is a
# wrapper that runs each call of the analyser in a container of that image, with no network (the scanner runs it with --offline-scan
# anyway), as the caller's user, with the scanner's own directory mounted at the same path and the database read-only (Q-VULN-196).
# The pair is issued by the instance's own CA (certs/ca.key of up.sh) for vuln-scanner-trivy.quader.test and 127.0.0.1; quader keeps
# presenting certs/server.crt (localhost), which the scanner admits by that name. Everything listens on 127.0.0.1 only.
#
# What it keeps: <state>/scanners (the pair, the configurations, the logs, the scanner's spool and the database's copy, 1.7 GB unpacked on
# 2026-11-25; the repository upstream/scanner-trivy holds the layer, 143 MB, in the instance's S3 store). Measured from nothing on
# instance 6 that day: up.sh 8 s, quader ready 10 s after serve.sh started (binaries from the build cache), the database recorded after
# 17 s (143,486,506 bytes from mirror.gcr.io in 1.8 s) and loaded after 22 s; the two processes hold about 68 MB each while idle, the
# analyser's container lives for one call. ClamAV is not run (docs/developer/web-ui.md, gap H-1891 of docs/spec/architecture.md).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
set -a
# shellcheck disable=SC1091
. tools/versions.env
set +a
# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root"
state="$UI_STATE"
dir="$state/scanners"
name=vuln-scanner-trivy.quader.test
label="quader-ui-dev-analyser=$UI_PROJECT"

# pair: the scanner's certificate from the instance's CA, issued once and again when the CA is newer.
pair() {
  if [ -f "$dir/trivy.crt" ] && [ "$dir/trivy.crt" -nt "$state/certs/ca.crt" ]; then
    return 0
  fi
  cat > "$dir/trivy.cnf" <<CNF
[req]
distinguished_name = dn
req_extensions = ext
prompt = no
[dn]
CN = $name
[ext]
subjectAltName = DNS:$name, IP:127.0.0.1
extendedKeyUsage = serverAuth, clientAuth
keyUsage = digitalSignature, keyEncipherment
CNF
  openssl() { docker run --rm --user "$(id -u):$(id -g)" -v "$dir:$dir" -v "$state/certs:$state/certs:ro" -w "$dir" "$OPENSSL_IMAGE" "$@"; }
  openssl genrsa -out trivy.key 2048 2>/dev/null
  openssl req -new -key trivy.key -config trivy.cnf -out trivy.csr
  openssl x509 -req -in trivy.csr -CA "$state/certs/ca.crt" -CAkey "$state/certs/ca.key" -CAcreateserial -CAserial trivy.srl \
    -days 365 -sha256 -extfile trivy.cnf -extensions ext -out trivy.crt 2>/dev/null
  chmod 0600 "$dir/trivy.key"
  rm -f "$dir/trivy.csr" "$dir/trivy.cnf" "$dir/trivy.srl"
}

# wrapper: vulnerabilities.images.binary. The scanner starts it with an empty environment but HOME, TMPDIR and GOMEMLIMIT, so the
# docker client and its daemon's address are written in.
wrapper() {
  local docker host
  docker="$(command -v docker)"
  host="$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)"
  cat > "$dir/trivy.tmp" <<SH
#!/bin/sh
# Written by deploy/ui-dev/scanners.sh: Trivy of TRIVY_IMAGE for the image scanner of the UI development profile, one container per call.
exec "$docker" -H "${host:-unix:///var/run/docker.sock}" run --rm -i --network none --user "$(id -u):$(id -g)" --label "$label" \\
  -e HOME -e TMPDIR -e GOMEMLIMIT -v "$dir:$dir" -v "$dir/trivy-db:$dir/trivy-db:ro" -w "\$PWD" "$TRIVY_IMAGE" "\$@"
SH
  chmod 0755 "$dir/trivy.tmp"
  mv "$dir/trivy.tmp" "$dir/trivy"
}

# config <kind> <ops port>: the configuration of one of the two processes.
config() {
  local kind="$1" ops="$2" file="$dir/$1.yaml"
  cat > "$file.tmp" <<YAML
# Written by deploy/ui-dev/scanners.sh at every start of the UI development profile, so edit the script, not this file.
process:
  scanner_kind: $kind
server:
  listen: 127.0.0.1:$UI_TRIVY_PORT
  ops_listen: 127.0.0.1:$ops
  tls:
    certificates:
      - cert_file: "$dir/trivy.crt"
        key_file: "$dir/trivy.key"
  internal_tls:
    ca_file: "$state/certs/ca.crt"
storage:
  spool:
    dir: "$dir/spool"
vulnerabilities:
  images:
    binary: "$dir/trivy"
    scanner:
      # quader presents certs/server.crt, which lists localhost.
      client_name: localhost
    registry:
      url: https://127.0.0.1:$UI_SCAN_PORT
    database:
      dir: "$dir/trivy-db"
YAML
  mv "$file.tmp" "$file"
}

prepare() {
  [ "${UI_SCANNERS:-1}" != 0 ] || return 0
  docker image inspect "$TRIVY_IMAGE" >/dev/null 2>&1 || {
    echo "scanners: $TRIVY_IMAGE is not on this machine; it is pulled once now (docker pull)" >&2
    docker pull -q "$TRIVY_IMAGE" >/dev/null
  }
  mkdir -p "$dir/spool" "$dir/tmp" "$dir/trivy-db"
  pair
  wrapper
  config trivy "$UI_TRIVY_OPS_PORT"
  config trivy-db "$UI_TRIVY_DB_OPS_PORT"
}

down() {
  docker ps -aq --filter "label=$label" | xargs docker rm -f >/dev/null 2>&1 || true
}

# database: the task vuln-sync-trivy-db once, as an administrator does with "Fetch the image database now" (R-VULN-045).
database() {
  [ "${UI_SCANNERS:-1}" != 0 ] || return 0
  local base="https://127.0.0.1:$UI_BACKEND_PORT" origin="https://localhost:$UI_WEB_PORT" jar i
  for ((i = 0; i < 600; i++)); do
    curl -skf -o /dev/null "https://127.0.0.1:$UI_OPS_PORT/readyz" && break
    sleep 1
  done
  jar="$(mktemp)"
  curl -sk -c "$jar" -o /dev/null -H "Origin: $origin" -H 'Content-Type: application/json' \
    --data "{\"username\":\"admin\",\"password\":\"$(cat "$state/secrets/break-glass-password")\"}" "$base/auth/local/login"
  curl -sk -b "$jar" -o /dev/null -w 'scanners: vuln-sync-trivy-db started (%{http_code})\n' -X POST -H "Origin: $origin" \
    -H "X-CSRF-Token: $(awk '$6 ~ /csrf/ {print $7}' "$jar" | head -n 1)" "$base/api/v1/system/tasks/vuln-sync-trivy-db/run"
  rm -f "$jar"
}

case "${1:-}" in
  prepare) prepare ;;
  down) down ;;
  database) database ;;
  *) echo "usage: scanners.sh prepare | database | down" >&2; exit 2 ;;
esac
