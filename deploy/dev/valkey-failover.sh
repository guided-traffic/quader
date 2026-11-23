#!/usr/bin/env bash
# Valkey planned failover (R-HA-051 dev-valkey-failover): SENTINEL FAILOVER quader on one
# Sentinel, then waits until the Sentinels report another primary and the old primary is
# the replica.
#
# A Sentinel refuses a failover it cannot do ("-NOGOODSLAVE" while it knows no healthy replica, "-INPROG" while one runs) and the
# script used to ignore the answer and wait 60 s for a primary that could not move: right after drain variant B, whose drain had
# restarted Sentinels and the replica, it said only "the primary did not move within 60 s" (F-040). So it first waits, at most 60 s,
# until a Sentinel lists a replica that is neither subjectively down nor disconnected, asks that Sentinel, takes its answer: OK goes
# on, any other answer is printed and the next Sentinel is asked; when none says OK the script fails with their answers.
set -euo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
require_cluster

before="$(valkey_primary)" || die "no Sentinel reports a primary"
log "primary before: $before"

# healthy_replica <sentinel pod>: 0 when the Sentinel lists a replica whose flags carry neither s_down nor disconnected.
healthy_replica() {
  sentinel_cli "$1" sentinel replicas quader-valkey 2>/dev/null | awk '
    prev == "flags" { if ($0 !~ /s_down|o_down|disconnected/) ok = 1 }
    { prev = $0 }
    END { exit ok ? 0 : 1 }'
}

deadline=$(( $(date +%s) + 60 ))
answers=""
asked=""
while [ -z "$asked" ]; do
  for i in 0 1 2; do
    s="quader-valkey-sentinel-$i"
    healthy_replica "$s" || continue
    answer="$(sentinel_cli "$s" sentinel failover quader-valkey 2>&1 | head -1 || true)"
    if [ "$answer" = "OK" ]; then
      asked="$s"
      break
    fi
    log "$s answered the failover with: ${answer:-nothing}"
    answers="${answers:+$answers; }$s: ${answer:-no answer}"
  done
  [ -n "$asked" ] && break
  [ "$(date +%s)" -lt "$deadline" ] || die "no Sentinel started the failover within 60 s (a Sentinel with a healthy replica answered: ${answers:-none of the three listed a healthy replica})"
  sleep 2
done
log "$asked started the failover"

deadline=$(( $(date +%s) + 60 ))
while :; do
  after="$(valkey_primary || true)"
  if [ -n "$after" ] && [ "$after" != "$before" ]; then
    break
  fi
  [ "$(date +%s)" -lt "$deadline" ] || die "the primary did not move within 60 s of $asked's OK (still ${after:-unknown})"
  sleep 1
done
pod="${after%%.*}"
until [ "$(valkey_cli "$pod" role 2>/dev/null | head -1)" = "master" ]; do
  [ "$(date +%s)" -lt "$deadline" ] || die "$pod does not report role master"
  sleep 1
done
log "primary after:  $after"
