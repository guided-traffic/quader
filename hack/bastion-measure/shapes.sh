#!/usr/bin/env bash
# Item 4 of W34-E4: what each client prints for a refusal, its exit code and whether it retries by itself.
#
#   shapes.sh stub        every client against `e4 stub`, which answers the four error shapes of R-BAS-095 (specified, not built):
#                         503 busy with Retry-After 2, 429 rate_limited with Retry-After 2 (reads), 403 read_only (writes)
#   shapes.sh bastion     every client, anonymous and cold, against the Bastion as built today with an anonymous limit of
#                         1 a second and a burst of 1, and every write against it
#
# Output: <E4>/results/shapes/<case>-<client>.txt (the client's output and exit code) and, for the stub, the requests it received.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root" >/dev/null
# shellcheck source=hack/bastion-measure/env.sh
. hack/bastion-measure/env.sh

NPM_IMAGE="${NPM_IMAGE:-quader-e2e-clients:dev}"
NUGET_IMAGE="${NUGET_IMAGE:-quader-e2e-nugetclients:dev}"
DOCKER_IMAGE="${DOCKER_IMAGE:-quader-e2e-ociclients-docker:dev}"
PODMAN_IMAGE="${PODMAN_IMAGE:-quader-e2e-ociclients-podman:dev}"
APT_IMAGE="${APT_IMAGE:-quader-e2e-aptclients-debian13:dev}"
GALAXY_IMAGE="${GALAXY_IMAGE:-quader-e2e-ansibleclients-core217:dev}"
out="$E4/results/shapes"
mkdir -p "$out"
dargs=(--rm --add-host "$E4_HOST:host-gateway" --add-host "$E4_STUB_HOST:host-gateway" -v "$E4/certs/ca.crt:/e4/ca.crt:ro")

# Each client function takes the origin's host:port and read|write, runs the client's normal command against it, and prints
# its output; the last line is the exit code.
c_npm() {
  local hp="$1" what="$2"
  docker run "${dargs[@]}" -e HP="$hp" -e WHAT="$what" -e TOK="${TOK:-}" --entrypoint sh "$NPM_IMAGE" -c '
    mkdir -p /tmp/w && cd /tmp/w; printf "cafile=/e4/ca.crt\naudit=false\nfund=false\nupdate-notifier=false\n" > .npmrc; [ "$WHAT" = read ] || printf "//%s/api/npm/e4/npm-local/:_authToken=%s\n" "$HP" "${TOK:-x}" >> .npmrc
    reg="https://$HP/api/npm/e4/npm-local/"
    if [ "$WHAT" = read ]; then timeout 300 npm11 install e4pkg-001 --registry "$reg" --no-audit; else
      printf "{\"name\":\"e4-shape\",\"version\":\"1.0.0\"}" > package.json; timeout 300 npm11 publish --registry "$reg"; fi; echo "exit $?"'
}
c_nuget() {
  local hp="$1" what="$2"
  docker run "${dargs[@]}" -e HP="$hp" -e WHAT="$what" -e DOTNET_CLI_TELEMETRY_OPTOUT=1 -e DOTNET_NOLOGO=1 -e SSL_CERT_FILE=/tmp/bundle.crt \
    --entrypoint sh "$NUGET_IMAGE" -c '
    cat /etc/ssl/certs/ca-certificates.crt /e4/ca.crt > /tmp/bundle.crt; mkdir -p /tmp/w && cd /tmp/w
    src="https://$HP/api/nuget/e4/nuget-local/v3/index.json"
    if [ "$WHAT" = read ]; then
      printf "<configuration><packageSources><clear /><add key=\"e4\" value=\"%s\" /></packageSources></configuration>" "$src" > nuget.config
      printf "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetAudit>false</NuGetAudit></PropertyGroup><ItemGroup><PackageReference Include=\"E4.Pkg001\" Version=\"1.0.0\" /></ItemGroup></Project>" > e4.csproj
      timeout 300 dotnet restore
    else
      mkdir p && printf "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net10.0</TargetFramework><PackageId>E4.Shape</PackageId><Version>1.0.0</Version></PropertyGroup></Project>" > p/p.csproj
      dotnet pack p -o pkgs >/dev/null 2>&1; timeout 300 dotnet nuget push pkgs/E4.Shape.1.0.0.nupkg -s "$src" -k x
    fi; echo "exit $?"'
}
c_docker() {
  local hp="$1" what="$2"
  docker run "${dargs[@]}" --privileged -e HP="$hp" -e WHAT="$what" --entrypoint sh "$DOCKER_IMAGE" -c '
    mkdir -p "/etc/docker/certs.d/$HP"; cp /e4/ca.crt "/etc/docker/certs.d/$HP/ca.crt"
    (dockerd-entrypoint.sh dockerd --host=unix:///var/run/docker.sock >/tmp/dockerd.log 2>&1 &)
    for i in $(seq 1 60); do docker info >/dev/null 2>&1 && break; sleep 0.5; done
    if [ "$WHAT" = read ]; then timeout 300 docker pull "$HP/e4/docker-local/small:1"; else
      echo x > /tmp/f; tar cf /tmp/e.tar -C /tmp f; docker import /tmp/e.tar "$HP/e4/docker-local/shape:1" >/dev/null; timeout 300 docker push "$HP/e4/docker-local/shape:1"; fi; echo "exit $?"'
}
c_podman() {
  local hp="$1" what="$2"
  docker run "${dargs[@]}" --privileged -e HP="$hp" -e WHAT="$what" --entrypoint sh "$PODMAN_IMAGE" -c '
    mkdir -p "/etc/containers/certs.d/$HP"; cp /e4/ca.crt "/etc/containers/certs.d/$HP/ca.crt"
    if [ "$WHAT" = read ]; then timeout 300 podman pull "$HP/e4/docker-local/small:1"; else
      echo x > /tmp/f; tar cf /tmp/e.tar -C /tmp f; podman import /tmp/e.tar "$HP/e4/docker-local/shape:1" >/dev/null; timeout 300 podman push "$HP/e4/docker-local/shape:1"; fi; echo "exit $?"'
}
c_apt() {
  local hp="$1" what="$2"
  [ "$what" = read ] || { echo "APT has no write; exit -"; return 0; }
  docker run "${dargs[@]}" -e HP="$hp" --entrypoint sh "$APT_IMAGE" -c '
    echo "Acquire::https::CAInfo \"/e4/ca.crt\";" > /etc/apt/apt.conf.d/99e4; rm -f /etc/apt/sources.list.d/*
    echo "deb [trusted=yes] https://$HP/api/deb/e4/apt stable main" > /etc/apt/sources.list.d/e4.list
    timeout 300 apt-get update -o Acquire::Languages=none; echo "exit $?"'
}
c_galaxy() {
  local hp="$1" what="$2"
  docker run "${dargs[@]}" -e HP="$hp" -e WHAT="$what" -e TOK="${TOK:-}" -e SSL_CERT_FILE=/e4/ca.crt -e REQUESTS_CA_BUNDLE=/e4/ca.crt --entrypoint sh "$GALAXY_IMAGE" -c '
    srv="https://$HP/api/ansible/e4/galaxy/"
    if [ "$WHAT" = read ]; then timeout 300 ansible-galaxy collection install e4.app -p /tmp/c --server "$srv"; else
      cd /tmp && ansible-galaxy collection init e4.shape >/dev/null && ansible-galaxy collection build e4/shape >/dev/null &&
      timeout 300 ansible-galaxy collection publish e4-shape-1.0.0.tar.gz --server "$srv" --token "${TOK:-x}"; fi; echo "exit $?"'
}
c_curl() {
  local hp="$1" what="$2"
  docker run "${dargs[@]}" -e HP="$hp" -e WHAT="$what" --entrypoint sh "$APT_IMAGE" -c '
    u="https://$HP/files/e4/files/e4/file-1mib.bin"
    if [ "$WHAT" = read ]; then timeout 300 curl -sS --fail-with-body --cacert /e4/ca.crt -o /tmp/body "$u"; rc=$?; head -c 600 /tmp/body; else
      head -c 1024 /dev/urandom > /tmp/x; timeout 300 curl -sS --fail-with-body --cacert /e4/ca.crt -T /tmp/x "$u"; rc=$?; fi; echo; echo "exit $rc"'
}

clients=(npm nuget docker podman apt galaxy curl)

with_stub() { # with_stub <case> <stub flags...>
  local name="$1"
  shift
  "$E4/e4" stub -listen "127.0.0.1:$E4_STUB_PORT" -cert "$E4/certs/e4.crt" -key "$E4/certs/e4.key" "$@" > "$out/$name.stub.jsonl" 2>/dev/null &
  local pid=$!
  sleep 1
  local what=read
  [ "$name" = read_only ] && what="write"
  for c in "${clients[@]}"; do
    echo "== $name $c" >&2
    local from
    from="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    "c_$c" "$E4_STUB_HOST:$E4_STUB_PORT" "$what" > "$out/stub-$name-$c.txt" 2>&1 || true
    echo "window $from $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$out/stub-$name-$c.txt"
  done
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}

case "${1:-}" in
  one) # one <stub|bastion> <client> <read|write>: a single client against the running stub or Bastion
    hp="$E4_STUB_HOST:$E4_STUB_PORT"
    [ "$2" = bastion ] && hp="$E4_HOST:$E4_BASTION_PORT"
    "c_$3" "$hp" "$4"
    ;;
  stub)
    with_stub busy -status 503 -code busy -retry-after 2
    with_stub rate_limited -status 429 -code rate_limited -retry-after 2
    with_stub read_only -status 403 -code read_only -retry-after "" -writes-only
    ;;
  bastion)
    E4_BASTION_EXTRA="  limits:
    anonymous:
      requests_per_second: 1
      burst: 1" hack/bastion-measure/run.sh bastion >/dev/null
    sleep 20
    for c in "${clients[@]}"; do
      echo "== bastion read $c" >&2
      "c_$c" "$E4_HOST:$E4_BASTION_PORT" read > "$out/bastion-limit-$c.txt" 2>&1 || true
      sleep 2
    done
    hack/bastion-measure/run.sh bastion >/dev/null
    # Today a write is refused only after the credential was checked: the reader's token.
    TOK="$(cat "$E4/client/reader-token")"
    export TOK
    for c in "${clients[@]}"; do
      echo "== bastion write $c" >&2
      "c_$c" "$E4_HOST:$E4_BASTION_PORT" write > "$out/bastion-write-$c.txt" 2>&1 || true
    done
    ;;
  *) sed -n '2,10p' "$0"; exit 2 ;;
esac
