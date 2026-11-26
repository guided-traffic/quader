#!/usr/bin/env bash
# Item 1 of W34-E4: each client command through a cold Bastion (a fresh process, an empty cache) and again through the same,
# now warm Bastion. Per run it writes the client's output and the tally (the request records of both processes in the run's
# window, and the growth of the Bastion's counters) to <E4>/results/<cmd>-<cred>-<cold|warm>.{txt,json}.
#
#   measure.sh [cred] [cmd ...]     cred: reader (default) or anon; cmds default to every command of run.sh client
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
# shellcheck source=deploy/ui-dev/instance.sh
. deploy/ui-dev/instance.sh
ui_instance "$root" >/dev/null
# shellcheck source=hack/bastion-measure/env.sh
. hack/bastion-measure/env.sh

cred="${1:-reader}"
shift || true
cmds=("$@")
[ "${#cmds[@]}" -gt 0 ] || cmds=(curl npm nuget docker-small docker-forty docker-big podman-small podman-forty podman-big apt galaxy)
run=hack/bastion-measure/run.sh
out="$E4/results"
mkdir -p "$out"

now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

one() { # one <cmd> <cold|warm>
  local name="$1-$cred-$2" from to
  $run metrics > "$out/$name.before.prom"
  sleep 1
  from="$(now)"
  $run client "$1" "$cred" > "$out/$name.txt" 2>&1 || echo "client exit $?" >> "$out/$name.txt"
  sleep 2
  to="$(now)"
  # The Bastion reports downloads served from its cache in batches every second; the window takes them in.
  $run metrics > "$out/$name.after.prom"
  echo "$from $to" > "$out/$name.window"
  "$E4/e4" tally -label "$name" -bastion-log "$E4/logs/bastion.log" -main-log "$E4/logs/main.log" -from "$from" -to "$to" \
    -metrics-before "$out/$name.before.prom" -metrics-after "$out/$name.after.prom" > "$out/$name.json"
  echo "$name: $(tail -1 "$out/$name.txt")"
  cat "$out/$name.json"
}

for c in "${cmds[@]}"; do
  $run bastion "${E4_CACHE:-20GiB}" >/dev/null
  # A feed entry the Bastion received within the skew margin before a fill makes the filled entry stale, and the warm run would
  # revalidate everything (Q-BAS-271, the e2e set's ociFeedQuiet of 16 s): the cold run starts 20 s after the Bastion is ready.
  sleep 20
  one "$c" cold
  one "$c" warm
done
