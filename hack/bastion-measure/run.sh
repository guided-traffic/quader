#!/usr/bin/env bash
# Starts and stops the processes of the measurement W34-E4 and runs one client command through the Bastion (README.md).
#
#   run.sh main                     start the main installation (role all) in the background; log $E4/logs/main.log
#   run.sh bastion [max_bytes]      start a Bastion with an empty cache (default 20GiB); log $E4/logs/bastion.log;
#                                   E4_SERVER_EXTRA and E4_BASTION_EXTRA are lines added to its server and bastion sections
#   run.sh bastion2                 start a second replica of the same registration on the stub's ports whose cached artifacts
#                                   are revalidated after 1 s (the conditional requests of item 3); log $E4/logs/bastion2.log
#   run.sh stop main|bastion|bastion2
#   run.sh client <cmd> <cred>      run one client command in its container; cmd is npm, nuget, docker-small, docker-forty,
#                                   docker-big, podman-small, podman-forty, podman-big, apt, galaxy, curl; cred is reader, admin or anon
#   run.sh metrics                  print the Bastion's metrics
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root" >/dev/null
# shellcheck source=hack/bastion-measure/env.sh
. hack/bastion-measure/env.sh

# The client images of the format journeys (make npm-clients-image, nuget-clients-image, oci-clients-image, apt-clients-image,
# ansible-clients-image), each on the pinned base image of tools/versions.env; built once on this machine, never pulled here.
NPM_IMAGE="${NPM_IMAGE:-quader-e2e-clients:dev}"
NUGET_IMAGE="${NUGET_IMAGE:-quader-e2e-nugetclients:dev}"
DOCKER_IMAGE="${DOCKER_IMAGE:-quader-e2e-ociclients-docker:dev}"
PODMAN_IMAGE="${PODMAN_IMAGE:-quader-e2e-ociclients-podman:dev}"
APT_IMAGE="${APT_IMAGE:-quader-e2e-aptclients-debian13:dev}"
GALAXY_IMAGE="${GALAXY_IMAGE:-quader-e2e-ansibleclients-core217:dev}"

pidfile() { echo "$E4/$1.pid"; }

stop() {
  local f
  f="$(pidfile "$1")"
  if [ -f "$f" ]; then
    kill -TERM "$(cat "$f")" 2>/dev/null || true
    for _ in $(seq 1 50); do kill -0 "$(cat "$f")" 2>/dev/null || break; sleep 0.2; done
    rm -f "$f"
  fi
}

wait_ready() { # wait_ready <ops port>
  for _ in $(seq 1 120); do
    if curl -fsk "https://127.0.0.1:$1/readyz" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  echo "not ready on ops port $1" >&2
  return 1
}

start_main() {
  stop main
  "$E4/quader" serve --role all --config "$E4/main.yaml" >> "$E4/logs/main.log" 2>&1 &
  echo $! > "$(pidfile main)"
  wait_ready "$E4_MAIN_OPS_PORT"
  echo "main: https://localhost:$E4_MAIN_PORT (pid $(cat "$(pidfile main)"))"
}

# start_bastion <name> <max_bytes> <port> <ops port> <server lines> <bastion lines>: one replica with an empty cache.
start_bastion() {
  local name="$1" max="$2" port="$3" ops="$4"
  stop "$name"
  rm -rf "$E4/cache-$name"
  mkdir -p "$E4/cache-$name"
  cat > "$E4/$name.yaml" <<YAML
# Written by hack/bastion-measure/run.sh: a Bastion of the measurement, one replica, the volume backend, no Valkey,
# every figure at its default unless the run names one.
server:
  listen: 127.0.0.1:$port
  ops_listen: 127.0.0.1:$ops
  base_url: https://$E4_HOST:$E4_BASTION_PORT
  tls:
    certificates:
      - cert_file: "$E4/certs/e4.crt"
        key_file: "$E4/certs/e4.key"
$5
bastion:
  main:
    url: https://localhost:$E4_MAIN_PORT
    token_file: "$E4/bastion-token"
    ca_file: "$E4/certs/ca.crt"
  cache:
    max_bytes: $max
    volume:
      dir: "$E4/cache-$name"
$6
YAML
  "$E4/quader" serve --role bastion --config "$E4/$name.yaml" >> "$E4/logs/$name.log" 2>&1 &
  echo $! > "$(pidfile "$name")"
  wait_ready "$ops"
  # Ready means the cache backend answered; the handshake with the main installation follows.
  for _ in $(seq 1 60); do
    if curl -fsk "https://127.0.0.1:$ops/metrics" | grep -q '^quader_bastion_main_reachable 1'; then
      echo "$name: https://$E4_HOST:$E4_BASTION_PORT on 127.0.0.1:$port, cache $max (pid $(cat "$(pidfile "$name")"))"
      return 0
    fi
    sleep 0.5
  done
  echo "$name did not reach the main installation" >&2
  return 1
}

metrics() { curl -fsk "https://127.0.0.1:$E4_BASTION_OPS_PORT/metrics"; }

# The client containers: the Bastion's host is the host gateway, the profile's CA is mounted, the client files of the seed
# (<E4>/client) are the working directory. Nothing is pulled: every image is the pin of tools/versions.env, on this machine.
docker_args=(--rm --add-host "$E4_HOST:host-gateway" --add-host "$E4_STUB_HOST:host-gateway" -v "$E4/certs/ca.crt:/e4/ca.crt:ro" -v "$E4/client:/e4/client:ro")

client() {
  local cmd="$1" cred="$2" tok="" reg="$E4_HOST:$E4_BASTION_PORT" base="https://$E4_HOST:$E4_BASTION_PORT"
  [ "$cred" = reader ] && tok="$(cat "$E4/client/reader-token")"
  [ "$cred" = admin ] && tok="$(cat "$E4/admin-token")"
  case "$cmd" in
    npm)
      local npmrc="registry=$base/api/npm/$E4_PROJECT/npm-local/
cafile=/e4/ca.crt
audit=false
fund=false
update-notifier=false"
      [ -n "$tok" ] && npmrc+="
//$reg/api/npm/$E4_PROJECT/npm-local/:_authToken=$tok"
      docker run "${docker_args[@]}" -e NPMRC="$npmrc" --entrypoint sh "$NPM_IMAGE" -c \
        'set -e; mkdir /tmp/w; cd /tmp/w; cp /e4/client/npm/package.json /e4/client/npm/package-lock.json .; printf "%s\n" "$NPMRC" > .npmrc;
         echo "npm $(npm11 --version)"; npm11 ci --no-audit --ignore-scripts --loglevel=warn; ls node_modules | wc -l'
      ;;
    nuget)
      local creds=""
      [ -n "$tok" ] && creds="<packageSourceCredentials><e4><add key=\"Username\" value=\"e4\" /><add key=\"ClearTextPassword\" value=\"$tok\" /></e4></packageSourceCredentials>"
      docker run "${docker_args[@]}" -e CFG="<?xml version=\"1.0\" encoding=\"utf-8\"?><configuration><packageSources><clear /><add key=\"e4\" value=\"$base/api/nuget/$E4_PROJECT/nuget-local/v3/index.json\" /></packageSources>$creds</configuration>" \
        -e DOTNET_CLI_TELEMETRY_OPTOUT=1 -e DOTNET_NOLOGO=1 -e SSL_CERT_FILE=/tmp/bundle.crt --entrypoint sh "$NUGET_IMAGE" -c \
        'set -e; cat /etc/ssl/certs/ca-certificates.crt /e4/ca.crt > /tmp/bundle.crt; mkdir /tmp/w; cd /tmp/w;
         cp /e4/client/nuget/e4.csproj .; printf "%s" "$CFG" > nuget.config; dotnet --version; dotnet restore -v q; echo "restore exit $?"'
      ;;
    docker-*)
      local img="${cmd#docker-}"
      docker run "${docker_args[@]}" --privileged -e TOK="$tok" -e REG="$reg" -e REF="$reg/$E4_PROJECT/docker-local/$img:1" \
        --entrypoint sh "$DOCKER_IMAGE" -c \
        'mkdir -p "/etc/docker/certs.d/$REG"; cp /e4/ca.crt "/etc/docker/certs.d/$REG/ca.crt";
         (dockerd-entrypoint.sh dockerd --host=unix:///var/run/docker.sock >/tmp/dockerd.log 2>&1 &);
         for i in $(seq 1 60); do docker info >/dev/null 2>&1 && break; sleep 0.5; done; docker version --format "{{.Server.Version}}";
         [ -z "$TOK" ] || echo "$TOK" | docker login -u e4 --password-stdin "$REG" >/dev/null;
         docker pull -q "$REF"; echo "pull exit $?"'
      ;;
    podman-*)
      local img="${cmd#podman-}"
      docker run "${docker_args[@]}" --privileged -e TOK="$tok" -e REG="$reg" -e REF="$reg/$E4_PROJECT/docker-local/$img:1" \
        --entrypoint sh "$PODMAN_IMAGE" -c \
        'mkdir -p "/etc/containers/certs.d/$REG"; cp /e4/ca.crt "/etc/containers/certs.d/$REG/ca.crt"; podman --version;
         [ -z "$TOK" ] || echo "$TOK" | podman login -u e4 --password-stdin "$REG" >/dev/null;
         podman pull -q "$REF" >/dev/null; echo "pull exit $?"'
      ;;
    apt)
      docker run "${docker_args[@]}" -e TOK="$tok" -e BASE="$base" -e HOSTPORT="$reg" --entrypoint sh "$APT_IMAGE" -c \
        'set -e; echo "Acquire::https::CAInfo \"/e4/ca.crt\";" > /etc/apt/apt.conf.d/99e4;
         rm -f /etc/apt/sources.list.d/*; echo "deb [trusted=yes] $BASE/api/deb/e4/apt stable main" > /etc/apt/sources.list.d/e4.list;
         [ -z "$TOK" ] || printf "machine %s\nlogin e4\npassword %s\n" "$HOSTPORT" "$TOK" > /etc/apt/auth.conf.d/e4.conf;
         apt-get --version | head -1; apt-get update -q -o Acquire::Languages=none; apt-get install -y -q --no-install-recommends e4-tool1 e4-tool2 e4-tool3 e4-tool4 e4-tool5'
      ;;
    galaxy)
      local auth=""
      [ -n "$tok" ] && auth="--token $tok"
      docker run "${docker_args[@]}" -e AUTH="$auth" -e BASE="$base" -e SSL_CERT_FILE=/e4/ca.crt -e REQUESTS_CA_BUNDLE=/e4/ca.crt \
        --entrypoint sh "$GALAXY_IMAGE" -c \
        'set -e; ansible-galaxy --version | head -1; ansible-galaxy collection install e4.app -p /tmp/c --server "$BASE/api/ansible/e4/galaxy/" $AUTH'
      ;;
    curl)
      local h=()
      [ -n "$tok" ] && h=(-H "Authorization: Bearer $tok")
      docker run "${docker_args[@]}" --entrypoint sh "$APT_IMAGE" -c \
        "curl --version | head -1; curl -sS --cacert /e4/ca.crt ${h[*]:+-H \"${h[1]}\"} -o /dev/null -w 'status %{http_code}, %{size_download} bytes, %{time_total} s\n' $base/files/$E4_PROJECT/files/e4/file-1mib.bin"
      ;;
    *) echo "unknown client command $cmd" >&2; return 2 ;;
  esac
}

case "${1:-}" in
  main) start_main ;;
  bastion) start_bastion bastion "${2:-20GiB}" "$E4_BASTION_PORT" "$E4_BASTION_OPS_PORT" "${E4_SERVER_EXTRA:-}" "${E4_BASTION_EXTRA:-}" ;;
  bastion2) start_bastion bastion2 20GiB "$E4_STUB_PORT" "$((E4_STUB_PORT + 1))" "" "    artifact_revalidate_after: 1s" ;;
  stop) stop "$2" ;;
  client) client "$2" "$3" ;;
  metrics) metrics ;;
  *) sed -n '2,14p' "$0"; exit 2 ;;
esac
