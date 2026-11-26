#!/usr/bin/env bash
# Items 2 and 3 of W34-E4: the overflow attack scaled down to a cache of 2 GiB, and the main installation's latency per credential
# check and per conditional request, idle and under the attack.
#
#   attack.sh latency <label>    the latency series alone (600 checks, 600 conditional requests through the replica bastion2)
#   attack.sh run <label> <spread> [seconds]
#                                a Bastion with a cache of 2 GiB that trusts this machine's forwarded-for; a warm npm ci; then e4 flood
#                                (32 connections, anonymous, <spread> addresses, 0 for one) for [seconds] (default 120), the latency
#                                series from its 20th second, an npm ci with the reader's token from its 60th
#
# Output: <E4>/results/attack/<label>.*
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root" >/dev/null
# shellcheck source=hack/bastion-measure/env.sh
. hack/bastion-measure/env.sh

run=hack/bastion-measure/run.sh
out="$E4/results/attack"
mkdir -p "$out"
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# The replica that turns every read of an npm tarball into a conditional request: its artifact_revalidate_after is 1 s. It is a
# second replica of the same registration, with a cache of its own; it is filled once before a series.
bastion2() {
  if ! curl -fsk "https://127.0.0.1:$((E4_STUB_PORT + 1))/readyz" >/dev/null 2>&1; then
    $run bastion2 >/dev/null
    sleep 20
    "$E4/e4" latency -main "https://localhost:$E4_MAIN_PORT" -ca "$E4/certs/ca.crt" -bastion-token-file "$E4/bastion-token" \
      -cred-file "$E4/client/reader-token" -host "$E4_HOST:$E4_BASTION_PORT" -via "127.0.0.1:$E4_STUB_PORT" -n 475 > /dev/null
    sleep 2
  fi
}

series() { # series <label>
  local from to
  from="$(now)"
  uptime > "$out/$1.load"
  "$E4/e4" latency -main "https://localhost:$E4_MAIN_PORT" -ca "$E4/certs/ca.crt" -bastion-token-file "$E4/bastion-token" \
    -cred-file "$E4/client/reader-token" -host "$E4_HOST:$E4_BASTION_PORT" -via "127.0.0.1:$E4_STUB_PORT" -n 600 > "$out/$1.client.json"
  uptime >> "$out/$1.load"
  sleep 1
  to="$(now)"
  "$E4/e4" tally -latency -label "$1" -main-log "$E4/logs/main.log" -bastion-log "$E4/logs/bastion.log" -from "$from" -to "$to" > "$out/$1.main.json"
  echo "$1: $(cat "$out/$1.main.json")"
}

case "${1:-}" in
  latency)
    bastion2
    series "${2:-idle}"
    ;;
  run)
    label="$2" spread="$3" secs="${4:-120}"
    bastion2
    # documents_max_bytes is a share of max_bytes (default 2 GiB): at its default a cache of 2 GiB keeps no artifact at all
    # (measured: every fill "no room"), so the scaled-down cache gives documents 256 MiB and artifacts the rest.
    E4_SERVER_EXTRA='  trusted_proxies: ["127.0.0.1/32"]' E4_BASTION_EXTRA="${E4_BASTION_EXTRA-    documents_max_bytes: 256MiB}" \
      $run bastion 2GiB >/dev/null
    sleep 20
    # The normal client's working set: one npm ci fills it, a second shows it warm before the attack.
    $run client npm reader > "$out/$label.npm-fill.txt" 2>&1
    from="$(now)"
    $run client npm reader > "$out/$label.npm-before.txt" 2>&1
    sleep 2
    echo "npm-before $from $(now)" > "$out/$label.windows"
    from="$(now)"
    "$E4/e4" flood -addr "127.0.0.1:$E4_BASTION_PORT" -host "$E4_HOST:$E4_BASTION_PORT" -ca "$E4/certs/ca.crt" \
      -metrics "https://127.0.0.1:$E4_BASTION_OPS_PORT/metrics" -c 32 -spread "$spread" -duration "${secs}s" > "$out/$label.flood.jsonl" &
    fpid=$!
    echo "flood $from" >> "$out/$label.windows"
    sleep 20
    series "$label-latency"
    sleep $((60 - 20 - 20 > 0 ? 60 - 40 : 1))
    n="$(now)"
    $run client npm reader > "$out/$label.npm-during.txt" 2>&1
    echo "npm-during $n $(now)" >> "$out/$label.windows"
    wait "$fpid"
    echo "flood-end $(now)" >> "$out/$label.windows"
    sleep 2
    n="$(now)"
    $run client npm reader > "$out/$label.npm-after.txt" 2>&1
    echo "npm-after $n $(now)" >> "$out/$label.windows"
    cat "$out/$label.windows"
    ;;
  *) sed -n '2,12p' "$0"; exit 2 ;;
esac
