#!/usr/bin/env bash
# The evidence of a stalled cluster: what e2e.sh records beside every set, green or red, so that a stall of the kind nodes (HAProxy
# 502 bursts, a request that waits 40 s, a certificate that fails verification four minutes into a leg) can be read afterwards
# from the artifact `e2e-artifacts-<set>` (.out/e2e/evidence/) instead of guessed (docs/developer/e2e.md#reading-a-stall).
#
#   e2e-evidence.sh start <dir>   starts the recorders and returns; e2e.sh calls it before the first step of a set
#   e2e-evidence.sh stop  <dir>   stops them and writes the files below; e2e.sh calls it from its EXIT trap
#   e2e-evidence.sh db-sample <dir>  takes one sample of the database's recorder (what the recorder does every interval; its test runs it)
#
# Recorders, all cheap on purpose (the probe of E0.15 once used more CPU than the installation, Q-HA-292): the samplers are shell loops
# that read /proc and /sys with builtins and fork only `date` and `sleep` once per sample; they run inside the node containers
# (`docker exec -d`) and write into the node, so no host process sits on a pipe and nothing outlives a run that is killed (the loops
# end at a stop file, or after QUADER_E2E_EVIDENCE_MAX_SECONDS). Nothing here needs more than Docker, kubectl, kind and jq.
#
#   samples.csv       one row per source and sample, sorted by time (columns below): the host, each kind node's cgroup, the edge, and
#                     the top-level cgroups of the machine (`cg:<name>`: the runner's pods, the Docker daemon's containers, …) with
#                     the CPU time each used, so that a stall can be told apart: the leg's own nodes or a neighbour (Q-HA-305); the
#                     cgroup parent of the job's Docker daemon and the pod it lies in (`daemon:parent`, `daemon:pod`) where that is
#                     not a top-level cgroup, and the containers of both that parent and the top-level `docker` (`cgd:<id12>`)
#   pod-memory.csv    where the daemon's cgroup parent lies inside a pod (the runner class `large`): the pod's memory.current, memory.peak,
#                     memory.max, OOM counters and what fills it (memory.stat: anon, file, kernel, shmem, slab_reclaimable, sock), every
#                     QUADER_E2E_EVIDENCE_POD_INTERVAL seconds; the last values and the peak of what cannot be given back head README.txt
#   pods.csv          per pod of each kind node, every QUADER_E2E_EVIDENCE_POD_INTERVAL seconds: CPU time, memory by kind (anon,
#                     file, socket buffers, kernel) and the OOM kills of its cgroup; pod-names.tsv maps the uids to names
#   containers.tsv    the containers of the job's own Docker daemon (kind nodes, k6, helpers), every 10 s: which `cgd:` rows are the leg's
#   db-connections.csv  the connections of the database's primary by client and state (pg_stat_activity: application_name, state,
#                     count) and the private memory of their backends (the sum of RssAnon of /proc/<pid>/status, KiB), every
#                     QUADER_E2E_EVIDENCE_POD_INTERVAL seconds, one `kubectl exec ... sh` per sample: whether a pool ran out or a pooler
#                     held its connections across a drain (F-169), and what a connection costs the primary's memory; the pod that
#                     answered is in each row, so a switchover shows as a new name
#   db-pools.csv      each pool's size of each replica (its metric quader_db_pool_max_connections, read once per pod through the API
#                     server's proxy to its ops port when the pod is first seen), so that db-connections.csv can be read against it
#   secrets.tsv       the Secrets of every namespace at the start and at the end: namespace, name, resourceVersion, creation time, uid,
#                     the managers that wrote them (managedFields). Names and metadata only, never a value.
#   secrets-watch.log every change of a Secret during the run, one line each, with the time the watch saw it
#   events.tsv        the Kubernetes events of every namespace as the watch saw them (the existing ones first), by time
#   pods-final.txt    `kubectl get pods -A -o wide` at the end
#   kept-pod-logs/    the logs of the pods that were deleted or evicted during the run and of every pod of the secrets operator and
#                     of cert-manager, gzip, one file per container instance: <namespace>_<pod>_<uid>__<container>__<restarts>.log.gz
#   README.txt        where the daemon put its containers and the pod's memory at the end, then this list
#
# Environment: QUADER_E2E_EVIDENCE=0 (e2e.sh: record nothing), QUADER_E2E_EVIDENCE_INTERVAL (seconds between two samples of a node,
# of the host and of the machine's cgroups, default 1), QUADER_E2E_EVIDENCE_EDGE_INTERVAL (the edge, default 2),
# QUADER_E2E_EVIDENCE_POD_INTERVAL (the pods and the Docker daemon's containers, default 5), QUADER_E2E_EVIDENCE_LOG_CAP_MIB (copy at most
# this much of a container's log, default 32), QUADER_E2E_EVIDENCE_MAX_SECONDS (a recorder's own end, default 21600).
#
# A recorder that cannot run (no cgroup pressure files, no edge pod) leaves its columns empty and says so on stderr; the set is never
# failed by its evidence.
set -uo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
set +e

cmd="${1:-}"
dir="${2:-}"
[ -n "$cmd" ] && [ -n "$dir" ] || die "usage: e2e-evidence.sh start|stop|db-sample <dir>"

SAMPLE_INTERVAL="${QUADER_E2E_EVIDENCE_INTERVAL:-1}"
EDGE_INTERVAL="${QUADER_E2E_EVIDENCE_EDGE_INTERVAL:-2}"
POD_INTERVAL="${QUADER_E2E_EVIDENCE_POD_INTERVAL:-5}"
LOG_CAP_MIB="${QUADER_E2E_EVIDENCE_LOG_CAP_MIB:-32}"
MAX_SECONDS="${QUADER_E2E_EVIDENCE_MAX_SECONDS:-21600}"
EDGE_NS="ingress-controller"
# the namespaces whose pods' logs are kept whole: the secrets operator that writes the generated Secrets, and cert-manager that issues the dev CA
KEEP_NS="internal-secrets-operator-system cert-manager"
NODE_STOP=/tmp/quader-e2e-evidence.stop
NODE_SAMPLES=/tmp/quader-e2e-evidence-samples.csv
NODE_HOST_SAMPLES=/tmp/quader-e2e-evidence-host.csv
NODE_PODS=/tmp/quader-e2e-evidence-pods.csv
NODE_LOGS=/tmp/quader-e2e-evidence-logs
NODE_LOGS_DONE=/tmp/quader-e2e-evidence-logs.done
EDGE_STOP=/tmp/quader-e2e-evidence.stop
# the helper container that reads the machine's cgroup tree through the job's Docker daemon (one per cluster)
CG_SAMPLES=/tmp/quader-e2e-evidence-cgroups.csv
CG_STOP=/tmp/quader-e2e-evidence-cgroups.stop
CG_PODMEM=/tmp/quader-e2e-evidence-podmem.csv
CG_NAME="quader-e2e-evidence-cgroups-$CLUSTER"

# The columns of samples.csv; every recorder below writes rows of exactly this width (an empty field where a source has no value).
SAMPLES_HEADER='ts_ms,source,cpu_some_avg10,cpu_some_total_us,cpu_full_avg10,cpu_full_total_us,mem_some_avg10,mem_some_total_us,mem_full_avg10,mem_full_total_us,load1,load5,load15,edge_curr_conns,edge_curr_ssl_conns,edge_conn_rate,edge_idle_pct,io_some_avg10,io_some_total_us,io_full_avg10,io_full_total_us,cpu_usage_us,cpu_steal_us,cpus,cpu_weight,members'
PODS_HEADER='ts_ms,node,pod_uid,cpu_usage_us,mem_current,mem_anon,mem_file,mem_sock,mem_kernel,oom_kill'
PODMEM_HEADER='ts_ms,mem_current,mem_peak,mem_max,oom,oom_kill,oom_local,docker_mem_current,mem_anon,mem_file,mem_kernel,mem_shmem,mem_slab_reclaimable,mem_sock'

nodes() { kind get nodes --name "$CLUSTER" 2>/dev/null; }

# gib VALUE: a byte count of a cgroup memory file in GiB with one decimal; `max` is no limit, anything else is not known
gib() {
  case "$1" in
    max) echo "no limit" ;;
    '' | *[!0-9]*) echo "not known" ;;
    *) awk -v b="$1" 'BEGIN { printf "%.1f GiB\n", b / 1073741824 }' ;;
  esac
}

# ── shared reader functions (prepended to the loops below; bash builtins only) ──────────────────────────────────────────────────
# psi FILE: R = "some_avg10,some_total,full_avg10,full_total" of a pressure file (`some avg10=0.00 avg60=0.00 avg300=0.00 total=123`
# and `full ...`); the total is microseconds of stall since boot, so the difference of two totals is the stall inside that window,
# whatever the ten-second average smooths. usage FILE: U = usage_usec of a cpu.stat. kv FILE KEY...: the values of `key value` lines.
read -r -d '' READERS <<'EOF'
psi() {
  local file=$1 l s="" f=""
  R=",,,"
  [ -r "$file" ] || return 0
  while read -r l; do
    set -- $l
    case $1 in
      some) s="${2#avg10=},${5#total=}" ;;
      full) f="${2#avg10=},${5#total=}" ;;
    esac
  done < "$file"
  R="${s:-,},${f:-,}"
}
usage() {
  local k v
  U=""
  [ -r "$1" ] || return 0
  while read -r k v; do [ "$k" = usage_usec ] && { U=$v; return 0; }; done < "$1"
}
EOF

# ── the sampler (runs inside a node container or, for the host on Linux, locally) ────────────────────────────────────────────────
# $1 source name, $2 interval, $3 and $4 the prefix and suffix of the pressure files (`/sys/fs/cgroup/` `.pressure` for a node's cgroup,
# whose cpu.stat is read too; `/proc/pressure/` and nothing for the host), $5 1 for the host (adds the load average, the CPU time of the
# whole machine and its steal time from /proc/stat, and the number of CPUs), $6 stop file, $7 max seconds, $8 a pod sample every this
# many rows (0: none), $9 the pods file.
# The host's busy time is user + nice + system + irq + softirq + steal of /proc/stat's first line, in USER_HZ (100 on Linux), written
# as microseconds; the difference of two rows over their distance is the number of CPUs the machine had in use.
# The pod rows: every pod cgroup of the kubelet inside the node (systemd or cgroupfs layout), its CPU time, its memory by kind and the
# OOM kills of its cgroup (memory.events), so that an OOMKilled container shows what filled its limit (heap, page cache or socket buffers).
read -r -d '' SAMPLER <<'EOF'
src=$1; iv=$2; pre=$3; suf=$4; host=$5; stop=$6; max=$7; podevery=$8; podfile=$9
cpus=""
if [ "$host" = 1 ]; then
  cpus=0
  while read -r k _; do case $k in cpu[0-9]*) cpus=$((cpus + 1)) ;; esac; done < /proc/stat
fi
pods() {
  local now=$1 d name uid k v cpu cur anon file sock kern oom
  # systemd driver (kind: cgroupRoot /kubelet): kubelet-kubepods-pod<uid>.slice, kubelet-kubepods-<qos>-pod<uid>.slice with _ for -;
  # cgroupfs driver: [kubelet/]kubepods/[<qos>/]pod<uid>
  for d in /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/*-pod*.slice /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/*/*-pod*.slice \
           /sys/fs/cgroup/kubelet/kubepods/pod* /sys/fs/cgroup/kubelet/kubepods/*/pod* /sys/fs/cgroup/kubepods/pod* /sys/fs/cgroup/kubepods/*/pod*; do
    [ -d "$d" ] || continue
    name=${d##*/}; uid=${name##*-pod}; uid=${uid#pod}; uid=${uid%.slice}; uid=${uid//_/-}
    usage "$d/cpu.stat"; cpu=$U
    cur=""; read -r cur < "$d/memory.current" 2>/dev/null
    anon=""; file=""; sock=""; kern=""; oom=""
    while read -r k v; do
      case $k in anon) anon=$v ;; file) file=$v ;; sock) sock=$v ;; kernel) kern=$v ;; esac
    done < "$d/memory.stat" 2>/dev/null
    while read -r k v; do [ "$k" = oom_kill ] && oom=$v; done < "$d/memory.events" 2>/dev/null
    printf '%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n' "$now" "${src#node:}" "$uid" "$cpu" "$cur" "$anon" "$file" "$sock" "$kern" "$oom" >> "$podfile"
  done
}
n=0
while [ "$SECONDS" -lt "$max" ] && [ ! -e "$stop" ]; do
  now=$(date +%s%3N)
  psi "${pre}cpu$suf"; c=$R
  psi "${pre}memory$suf"; m=$R
  psi "${pre}io$suf"; io=$R
  l1=""; l5=""; l15=""; busy=""; steal=""
  if [ "$host" = 1 ]; then
    read -r l1 l5 l15 _ < /proc/loadavg
    read -r _ u ni sy _ _ irq sirq st _ < /proc/stat
    busy=$(( (u + ni + sy + irq + sirq + st) * 10000 )); steal=$(( st * 10000 ))
  else
    usage "${pre}cpu.stat"; busy=$U
  fi
  printf '%s,%s,%s,%s,%s,%s,%s,,,,,%s,%s,%s,%s,,\n' "$now" "$src" "$c" "$m" "$l1" "$l5" "$l15" "$io" "$busy" "$steal" "$cpus" || exit 0
  if [ "$podevery" -gt 0 ] && [ $((n % podevery)) -eq 0 ]; then pods "$now"; fi
  n=$((n + 1))
  sleep "$iv"
done
EOF

# ── where the job's Docker daemon puts its containers (runs once, in a helper container like the sampler below) ──────────────────
# The daemon's cgroup parent is not assumed: the cgroup directory of one of its containers (the first kind node) is looked up in the
# mounted tree, shallow levels first, down to six levels, and its parent is the daemon's. On a `self-hosted` runner and on Docker
# Desktop that is the top-level `/docker`; on a `large` runner, whose daemon has a cgroup parent inside its own pod since 2026-11-18,
# it is `/kubepods/<qos>/pod<uid>/docker` (kubelet cgroup driver cgroupfs; the systemd layout `…-pod<uid>.slice` and a daemon on the
# systemd cgroup driver, `docker-<id>.scope`, are found the same way). /proc/self/cgroup of a container is no shortcut: on cgroup v2 a
# container has its own cgroup namespace and reads `0::/`, and a path read there is relative to the daemon's namespace, which need not
# be the root of the tree the sampler reads.
# $1 the mounted tree, $2 the full id of the container. Prints `<parent> <pod> <pod memory.max>`, the paths from the tree's root with
# a leading slash and `-` for no pod (the parent is not below a `kubepods` cgroup) or no memory.max; nothing when the container is
# not found.
read -r -d '' CGFIND <<'EOF'
root=$1; id=$2; pat=""; found=""
for _ in 1 2 3 4 5 6; do
  for d in "$root"/$pat"$id" "$root"/$pat"docker-$id.scope"; do
    if [ -d "$d" ]; then found=$d; break 2; fi
  done
  pat="$pat*/"
done
[ -n "$found" ] || exit 0
parent=${found%/*}; parent=${parent#"$root"}
pod=""; acc=""; kube=0
IFS=/ read -r -a parts <<< "${parent#/}"
for c in "${parts[@]}"; do
  acc="$acc/$c"
  case $c in kubepods*) kube=1 ;; esac
  if [ "$kube" = 1 ]; then case $c in pod*|*-pod*.slice) pod=$acc ;; esac; fi
done
lim=""
[ -n "$pod" ] && read -r lim 2>/dev/null < "$root$pod/memory.max"
printf '%s %s %s\n' "${parent:-/}" "${pod:--}" "${lim:--}"
EOF

# ── the machine's cgroups (runs in a helper container of the job's Docker daemon, the cgroup tree mounted at /host-cgroup) ────────
# On the CI runner the job's Docker daemon sees the cgroup root of the whole machine; the runner pods of both classes are under
# `kubepods`, the containers of a `self-hosted` job's daemon under the top-level `docker` beside every other such daemon's, those of a
# `large` job's daemon under its own pod (Q-HA-305). Each second, one row per top-level cgroup with a cpu.stat (`cg:<name>`): its
# pressure, its CPU time, its cpu.weight and the number of members (pods under kubepods, containers under docker). Where the daemon's
# cgroup parent (CGFIND) is not a top-level cgroup, the same row for it (`daemon:parent`), and where it lies in a pod, for the pod
# (`daemon:pod`). Every $4-th sample one row per container of the top-level `docker` and of the daemon's parent (`cgd:<id12>`, CPU
# time only), so that the leg's own containers (containers.tsv) and a neighbour's can be told apart, and one row of the pod's memory
# (memory.current, memory.peak, memory.max, the oom and oom_kill counts of memory.events, which include every OOM kill below the pod,
# the oom count of memory.events.local, which counts the pod's own limit only, memory.current of the daemon's parent, then from the
# pod's memory.stat anon, file, kernel, shmem, slab_reclaimable and sock; a field the kernel lacks stays empty) into $8, again once
# at the end.
# $1 interval, $2 stop file, $3 max seconds, $4 a container sample every this many rows, $5 the mounted tree, $6 the daemon's cgroup
# parent and $7 its pod as CGFIND prints them (empty or `-`: none), $8 the pod memory file.
read -r -d '' CGSAMPLER <<'EOF'
iv=$1; stop=$2; max=$3; every=$4; root=$5; parent=${6#-}; pod=${7#-}; memfile=$8
[ "$parent" = / ] && parent=""
row() {
  local d=$2 x k=0 w=""
  psi "$d/cpu.pressure"; c=$R
  psi "$d/memory.pressure"; m=$R
  psi "$d/io.pressure"; io=$R
  usage "$d/cpu.stat"
  read -r w < "$d/cpu.weight" 2>/dev/null
  for x in "$d"/*/ "$d"/*/*/; do
    x=${x%/}; x=${x##*/}
    case $x in pod*|*-pod*) k=$((k + 1)) ;; ????????????????????????????????????????????????????????????????) k=$((k + 1)) ;; esac
  done
  printf '%s,%s,%s,%s,,,,,,,,%s,%s,,,%s,%s\n' "$now" "$1" "$c" "$m" "$io" "$U" "$w" "$k"
}
ctrs() {
  local x id
  for x in "$1"/*/; do
    x=${x%/}; id=${x##*/}; id=${id#docker-}; id=${id%.scope}
    [ ${#id} -eq 64 ] || continue
    usage "$x/cpu.stat"
    printf '%s,cgd:%s,,,,,,,,,,,,,,,,,,,,%s,,,,\n' "$now" "${id:0:12}" "$U"
  done
}
mem() {
  local d=$root$pod k v cur="" peak="" lim="" oom="" kill="" loc="" dcur="" anon="" file="" kern="" shm="" srec="" sock=""
  read -r cur 2>/dev/null < "$d/memory.current"
  read -r peak 2>/dev/null < "$d/memory.peak"
  read -r lim 2>/dev/null < "$d/memory.max"
  while read -r k v; do case $k in oom) oom=$v ;; oom_kill) kill=$v ;; esac; done 2>/dev/null < "$d/memory.events"
  while read -r k v; do [ "$k" = oom ] && loc=$v; done 2>/dev/null < "$d/memory.events.local"
  [ -n "$parent" ] && read -r dcur 2>/dev/null < "$root$parent/memory.current"
  while read -r k v; do
    case $k in anon) anon=$v ;; file) file=$v ;; kernel) kern=$v ;; shmem) shm=$v ;; slab_reclaimable) srec=$v ;; sock) sock=$v ;; esac
  done 2>/dev/null < "$d/memory.stat"
  printf '%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n' "$now" "$cur" "$peak" "$lim" "$oom" "$kill" "$loc" "$dcur" "$anon" "$file" "$kern" "$shm" "$srec" "$sock" >> "$memfile"
}
# one group to the end: the script appends `>>file` to this program, which applies to its last command only
{
n=0
while [ "$SECONDS" -lt "$max" ] && [ ! -e "$stop" ]; do
  now=$(date +%s%3N)
  for d in "$root"/*/; do
    d=${d%/}; name=${d##*/}
    [ -r "$d/cpu.stat" ] || continue
    row "cg:$name" "$d"
    if [ "$name" = docker ] && [ $((n % every)) -eq 0 ]; then ctrs "$d"; fi
  done
  if [ -n "$parent" ]; then
    case $parent in /*/*) row daemon:parent "$root$parent" ;; esac
    if [ "$parent" != /docker ] && [ $((n % every)) -eq 0 ]; then ctrs "$root$parent"; fi
  fi
  if [ -n "$pod" ]; then
    row daemon:pod "$root$pod"
    [ $((n % every)) -eq 0 ] && mem
  fi
  n=$((n + 1))
  sleep "$iv"
done
if [ -n "$pod" ]; then now=$(date +%s%3N); mem; fi
}
EOF

# ── the pod's memory in one line (runs on the host at the stop, with gib() prepended) ──────────────────────────────────────────────
# $1 pod-memory.csv. Prints the line of README.txt: the last row's peak, limit, current use, the daemon's containers and OOM counters,
# and over every row the peak of the anonymous memory and the peak of what the kernel cannot give back without swap: anon, shmem
# (tmpfs and shared memory, counted in file), sock (socket buffers) and kernel less slab_reclaimable. A row that lacks one of
# those fields counts for neither peak that needs it; nothing when the file has no row.
read -r -d '' PODMEMLINE <<'EOF'
read -r cur peak lim oom kill loc dcur anon held < <(awk -F, '
  function num(x) { return x ~ /^[0-9]+$/ }
  function v(x) { return x == "" ? "-" : x }
  NR == 1 { next }
  {
    cur = $2; peak = $3; lim = $4; oom = $5; kill = $6; loc = $7; dcur = $8
    if (num($9) && (anon == "" || $9 + 0 > anon + 0)) anon = $9
    if (num($9) && num($11) && num($12) && num($13) && num($14)) {
      h = sprintf("%.0f", $9 + $12 + $11 - $13 + $14)
      if (held == "" || h + 0 > held + 0) held = h
    }
  }
  END { if (NR > 1) print v(cur), v(peak), v(lim), v(oom), v(kill), v(loc), v(dcur), v(anon), v(held) }' "$1")
[ -n "${cur:-}" ] || exit 0
known() { if [ "$1" = - ]; then echo "not known"; else echo "$1"; fi; }
a="at most $(gib "$anon") anonymous"; [ "$anon" = - ] && a="an anonymous part not known"
h="at most $(gib "$held") that cannot be given back"; [ "$held" = - ] && h="a part that cannot be given back not known"
echo "Pod memory         peak $(gib "$peak") of a limit of $(gib "$lim"), of that $a and $h (anon, shmem, sock, kernel less its reclaimable slab); at the end $(gib "$cur"), of that the daemon's containers $(gib "$dcur"); OOM at the pod's own limit $(known "$loc"), OOM events below the pod $(known "$oom"), OOM kills below the pod $(known "$kill") (pods.csv names a killed kind pod)"
EOF

# ── the log keeper (runs inside a node container) ────────────────────────────────────────────────────────────────────────────────
# Copies what the kubelet has written to /var/log/pods/*/*/*.log into a directory of the node, appending only what is new, every two
# seconds, so the log of a pod that is force-deleted or evicted is still there when the kubelet removes the original. It stops one pass
# after the stop file appears and leaves a marker. $1 stop file, $2 target directory, $3 cap per container log in bytes, $4 marker, $5 max seconds.
read -r -d '' KEEPER <<'EOF'
stop=$1; dest=$2; cap=$3; done=$4; max=$5
mkdir -p "$dest"
declare -A off
pass() {
  local size path key o
  while read -r size path; do
    key=${path#/var/log/pods/}; key=${key//\//__}
    o=${off[$key]:-0}
    [ "$size" -lt "$o" ] && o=0
    if [ "$size" -gt "$o" ] && [ "$o" -lt "$cap" ]; then
      tail -c +$((o + 1)) "$path" 2>/dev/null >> "$dest/$key"
      off[$key]=$size
    fi
  done < <(stat -c '%s %n' /var/log/pods/*/*/*.log 2>/dev/null)
}
while [ "$SECONDS" -lt "$max" ]; do
  pass
  [ -e "$stop" ] && break
  sleep 2
done
: > "$done"
EOF

# ── the edge recorder (runs inside an edge pod, writes to the kubectl exec stream) ───────────────────────────────────────────────
# HAProxy's own counters through its admin socket: current connections, current TLS connections, the connection rate and the idle
# percentage of the process. $1 source name, $2 interval, $3 stop file, $4 max seconds.
read -r -d '' EDGE <<'EOF'
src=$1; iv=$2; stop=$3; max=$4
end=$(( $(date +%s) + max ))
while [ "$(date +%s)" -lt "$end" ] && [ ! -e "$stop" ]; do
  t="$(date +%s)000"   # the edge image's date has whole seconds only
  echo "show info" | socat stdio /var/run/haproxy/admin.sock | awk -v src="$src" -v t="$t" -F': ' '
    $1 == "CurrConns" { c = $2 } $1 == "CurrSslConns" { s = $2 } $1 == "ConnRate" { r = $2 } $1 == "Idle_pct" { i = $2 }
    END { printf "%s,%s,,,,,,,,,,,,%s,%s,%s,%s,,,,,,,,,\n", t, src, c, s, r, i }' || exit 0
  sleep "$iv"
done
EOF

secrets_snapshot() {
  # Metadata only: the fields are picked one by one, no value of `.data` is read into the output.
  kubectl --context "$CTX" get secrets -A -o json 2>/dev/null | jq -r --arg l "$1" '.items[] | [$l, .metadata.namespace, .metadata.name, .metadata.resourceVersion, .metadata.creationTimestamp, .metadata.uid, ([.metadata.managedFields[]?.manager] | unique | join("+"))] | @tsv'
}

# DBSTAT runs in the postgres container of the primary (POSIX sh, psql and awk of the image): the client backends by application_name
# and state, with the sum of the RssAnon of each backend's /proc/<pid>/status ($1 is the proc root), as `application_name,state,count,
# anon_kib`. RssAnon is what a backend holds privately (its caches, sorts and plans); shared_buffers are shared memory and not in it.
# PostgreSQL tells a backend's memory only to that backend (pg_backend_memory_contexts) or into the server log
# (pg_log_backend_memory_contexts), neither cheap for every connection every few seconds; the kernel's line is one read per backend.
read -r -d '' DBSTAT <<'EOF'
proc=$1
psql -d postgres -AtF, -c "SELECT pid, coalesce(nullif(application_name, ''), '-'), coalesce(state, '-') FROM pg_stat_activity WHERE backend_type = 'client backend'" |
while IFS=, read -r pid app state; do
  anon=0
  if [ -r "$proc/$pid/status" ]; then
    while read -r k v _; do
      if [ "$k" = RssAnon: ]; then anon=$v; break; fi
    done <"$proc/$pid/status"
  fi
  printf '%s,%s,%s\n' "$app" "$state" "$anon"
done | awk -F, '{ k = $1 "," $2; n[k]++; a[k] += $3 } END { for (k in n) printf "%s,%d,%d\n", k, n[k], a[k] }'
EOF

# The replicas whose pools db-pools.csv records: the serving pods of the release, as the end-to-end tests select them (selQuader).
DB_REPLICAS='app.kubernetes.io/instance=quader,quader/serve=true,app.kubernetes.io/component in (api,packages,worker)'
# The scheme of the replicas' ops port 8081 (e2e.sh: QUADER_DEV_OPS_SCHEME, https unless a profile says otherwise).
OPS_PROXY_SCHEME=""
[ "${QUADER_DEV_OPS_SCHEME:-https}" = https ] && OPS_PROXY_SCHEME="https:"

# db_sample: one sample of the database's recorder. The connections of the primary of quader-db and their memory, the primary looked
# up for every sample, so that a switchover is followed; and the pool sizes of every replica not recorded yet (a pool's size is
# configuration, fixed for a pod's life, so one read per pod suffices; a pod whose metrics cannot be read yet is asked again in the next
# sample). A part that cannot run (no primary, an exec or a read that fails) writes no row.
db_sample() {
  local pod t
  t="$(date +%s)000"
  pod="$(kubectl --context "$CTX" -n "$NS" get pods -l cnpg.io/cluster=quader-db,cnpg.io/instanceRole=primary -o name 2>/dev/null | head -n 1)"
  pod="${pod#pod/}"
  if [ -n "$pod" ]; then
    kubectl --context "$CTX" -n "$NS" exec "$pod" -c postgres -- sh -c "$DBSTAT" _ /proc \
      2>/dev/null | while IFS= read -r line; do printf '%s,%s,%s\n' "$t" "$pod" "$line"; done >>"$dir/.db-connections.csv"
  fi
  touch "$dir/.db-pools.seen"
  for pod in $(kubectl --context "$CTX" -n "$NS" get pods -l "$DB_REPLICAS" -o name 2>/dev/null); do
    pod="${pod#pod/}"
    grep -qxF "$pod" "$dir/.db-pools.seen" && continue
    kubectl --context "$CTX" get --raw "/api/v1/namespaces/$NS/pods/$OPS_PROXY_SCHEME$pod:8081/proxy/metrics" 2>/dev/null \
      | awk -v t="$t" -v pod="$pod" '/^quader_db_pool_max_connections\{/ && match($0, /db_pool="[^"]*"/) {
          printf "%s,%s,%s,%s\n", t, pod, substr($0, RSTART + 9, RLENGTH - 10), $NF; found = 1 } END { exit !found }' >>"$dir/.db-pools.csv" \
      && echo "$pod" >>"$dir/.db-pools.seen"
  done
}

# db_sampler: db_sample every POD_INTERVAL seconds, until the stop file.
db_sampler() {
  while [ ! -e "$dir/.stop" ]; do
    db_sample
    sleep "$POD_INTERVAL"
  done
}

# one edge stream per edge pod, started by a supervisor that looks for new pods every ten seconds
edge_supervisor() {
  local pod
  while [ ! -e "$dir/.stop" ]; do
    # the job's own containers, so that the cgd: rows of the leg and of a neighbour can be told apart
    docker ps --no-trunc --format '{{.ID}} {{.Names}} {{.Image}}' 2>/dev/null | while read -r id name image; do
      printf '%s\t%s\t%s\t%s\n' "$(date +%s)" "${id:0:12}" "$name" "$image"
    done >>"$dir/.containers.tsv"
    for pod in $(kubectl --context "$CTX" -n "$EDGE_NS" get pods -o name 2>/dev/null); do
      pod="${pod#pod/}"
      if [ -f "$dir/.edge/$pod.pid" ] && kill -0 "$(cat "$dir/.edge/$pod.pid")" 2>/dev/null; then continue; fi
      kubectl --context "$CTX" -n "$EDGE_NS" exec "$pod" -- rm -f "$EDGE_STOP" >/dev/null 2>&1
      kubectl --context "$CTX" -n "$EDGE_NS" exec "$pod" -- sh -c "$EDGE" _ "edge:$pod" "$EDGE_INTERVAL" "$EDGE_STOP" "$MAX_SECONDS" >>"$dir/.edge/$pod.csv" 2>>"$dir/.edge/$pod.err" &
      echo $! >"$dir/.edge/$pod.pid"
    done
    sleep 10
  done
}

case "$cmd" in
start)
  rm -rf "$dir"
  mkdir -p "$dir/.edge" "$dir/kept-pod-logs"
  date +%s >"$dir/.started"
  node_list="$(nodes)"
  if [ -z "$node_list" ]; then log "evidence: no kind node of cluster $CLUSTER, nothing recorded"; exit 0; fi
  first=""
  pod_every=$(( (POD_INTERVAL + SAMPLE_INTERVAL - 1) / SAMPLE_INTERVAL ))
  for n in $node_list; do
    [ -n "$first" ] || first="$n"
    # a loop of an earlier run that was killed ends at the stop file before the new one starts
    docker exec "$n" touch "$NODE_STOP" >/dev/null 2>&1
  done
  sleep 1
  for n in $node_list; do
    docker exec "$n" rm -f "$NODE_STOP" "$NODE_SAMPLES" "$NODE_HOST_SAMPLES" "$NODE_PODS" "$NODE_LOGS_DONE" >/dev/null 2>&1
    docker exec "$n" rm -rf "$NODE_LOGS" >/dev/null 2>&1
    docker exec -d "$n" bash -c "$READERS
$SAMPLER >>$NODE_SAMPLES" _ "node:$n" "$SAMPLE_INTERVAL" /sys/fs/cgroup/ .pressure 0 "$NODE_STOP" "$MAX_SECONDS" "$pod_every" "$NODE_PODS" >/dev/null 2>&1 \
      || log "evidence: the sampler did not start in $n"
    docker exec -d "$n" bash -c "$KEEPER" _ "$NODE_STOP" "$NODE_LOGS" $((LOG_CAP_MIB * 1024 * 1024)) "$NODE_LOGS_DONE" "$MAX_SECONDS" >/dev/null 2>&1 \
      || log "evidence: the log keeper did not start in $n"
  done
  # the host: the pressure of the machine the nodes share. On Linux it is read here; on Docker Desktop /proc/pressure of a node container
  # is the VM's, which is the machine the nodes share.
  if [ -r /proc/pressure/cpu ] && [ "$(uname -s)" = Linux ]; then
    echo local >"$dir/.hostmode"
    bash -c "$READERS
$SAMPLER" _ host "$SAMPLE_INTERVAL" /proc/pressure/ "" 1 "$dir/.stop" "$MAX_SECONDS" 0 /dev/null >>"$dir/.host.csv" 2>/dev/null &
    echo $! >"$dir/.host.pid"
  else
    echo "node:$first" >"$dir/.hostmode"
    docker exec -d "$first" bash -c "$READERS
$SAMPLER >>$NODE_HOST_SAMPLES" _ host "$SAMPLE_INTERVAL" /proc/pressure/ "" 1 "$NODE_STOP" "$MAX_SECONDS" 0 /dev/null >/dev/null 2>&1 \
      || log "evidence: the host sampler did not start in $first"
  fi
  # the machine's cgroups: a helper container of the job's Docker daemon, from the kind nodes' image (bash is in it), with the daemon's
  # cgroup tree mounted read-only; on the CI runner that is the root of the whole machine (Q-HA-305). A one-off container of the same
  # kind first finds where the daemon put the first kind node (CGFIND), once per set.
  first_id=""; node_image=""; cg_parent="-"; cg_pod="-"; cg_max="-"
  read -r first_id node_image < <(docker inspect -f '{{.Id}} {{.Config.Image}}' "$first" 2>/dev/null)
  docker rm -f "$CG_NAME" >/dev/null 2>&1
  if [ -n "$node_image" ]; then
    read -r cg_parent cg_pod cg_max < <(docker run --rm --network none -v /sys/fs/cgroup:/host-cgroup:ro --entrypoint bash "$node_image" \
      -c "$CGFIND" _ /host-cgroup "$first_id" 2>/dev/null)
    : "${cg_parent:=-}" "${cg_pod:=-}" "${cg_max:=-}"
    echo "$cg_parent $cg_pod $cg_max" >"$dir/.cgparent"
    if [ "$cg_parent" = - ]; then
      log "evidence: the cgroup of $first was not found in its Docker daemon's cgroup tree (six levels): cgd: rows of the top-level docker only, no pod memory"
    elif [ "$cg_pod" = - ]; then
      log "evidence: the job's Docker daemon puts its containers under $cg_parent of the machine's cgroups, not inside a pod"
    else
      log "evidence: the job's Docker daemon puts its containers under $cg_parent of the machine's cgroups, inside the pod $cg_pod (memory.max $(gib "$cg_max")): daemon:parent and daemon:pod rows, the pod's memory in pod-memory.csv"
    fi
  fi
  if [ -n "$node_image" ] && docker run -d --name "$CG_NAME" --network none -v /sys/fs/cgroup:/host-cgroup:ro --entrypoint bash "$node_image" \
      -c "$READERS
$CGSAMPLER >>$CG_SAMPLES" _ "$SAMPLE_INTERVAL" "$CG_STOP" "$MAX_SECONDS" "$pod_every" /host-cgroup "$cg_parent" "$cg_pod" "$CG_PODMEM" >/dev/null 2>&1; then :
  else log "evidence: the cgroup sampler did not start: the machine's cgroups are not recorded"; fi
  secrets_snapshot start >"$dir/.secrets-start.tsv"
  # a fifo between the watch and the stamping loop, so that the watch's own pid is the one to stop (a pipeline gives the last stage's)
  mkfifo "$dir/.secrets-watch.fifo"
  while read -r line; do printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$line"; done <"$dir/.secrets-watch.fifo" >"$dir/secrets-watch.log" &
  kubectl --context "$CTX" get secrets -A -w -o custom-columns='NS:.metadata.namespace,NAME:.metadata.name,RV:.metadata.resourceVersion,CREATED:.metadata.creationTimestamp,UID:.metadata.uid' --no-headers >"$dir/.secrets-watch.fifo" 2>/dev/null &
  echo $! >"$dir/.secrets-watch.pid"
  kubectl --context "$CTX" get events -A -w -o json --output-watch-events >"$dir/.events.jsonl" 2>/dev/null &
  echo $! >"$dir/.events.pid"
  edge_supervisor &
  echo $! >"$dir/.supervisor.pid"
  db_sampler &
  echo $! >"$dir/.db.pid"
  log "evidence: recording the pressure and CPU time of the host, of the machine's cgroups and of ${node_list//$'\n'/ } and their pods, the edge's connections, the Secrets and the events into $dir"
  ;;
stop)
  [ -d "$dir" ] || exit 0
  touch "$dir/.stop"
  node_list="$(nodes)"
  # the watchers and the supervisor first, then the loops that stop themselves
  for f in .supervisor.pid .secrets-watch.pid .events.pid .host.pid .db.pid; do
    [ -f "$dir/$f" ] && kill "$(cat "$dir/$f")" 2>/dev/null
  done
  for n in $node_list; do docker exec "$n" touch "$NODE_STOP" >/dev/null 2>&1; done
  docker exec "$CG_NAME" touch "$CG_STOP" >/dev/null 2>&1
  for pod in $(kubectl --context "$CTX" -n "$EDGE_NS" get pods -o name 2>/dev/null); do
    kubectl --context "$CTX" -n "$EDGE_NS" exec "${pod#pod/}" -- touch "$EDGE_STOP" >/dev/null 2>&1
  done
  secrets_snapshot end >"$dir/.secrets-end.tsv"
  { printf 'when\tnamespace\tname\tresourceVersion\tcreated\tuid\twrittenBy\n'; cat "$dir/.secrets-start.tsv" "$dir/.secrets-end.tsv"; } >"$dir/secrets.tsv"
  kubectl --context "$CTX" get pods -A -o wide >"$dir/pods-final.txt" 2>&1
  # wait for the log keepers' last pass (two seconds) and the edge streams to end
  for n in $node_list; do
    i=0
    until docker exec "$n" test -e "$NODE_LOGS_DONE" >/dev/null 2>&1 || [ "$i" -ge 10 ]; do sleep 1; i=$((i + 1)); done
  done
  for p in "$dir"/.edge/*.pid; do
    [ -f "$p" ] || continue
    pid="$(cat "$p")"; i=0
    while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 10 ]; do sleep 1; i=$((i + 1)); done
    kill "$pid" 2>/dev/null
  done
  # samples.csv
  {
    echo "$SAMPLES_HEADER"
    {
      for n in $node_list; do docker exec "$n" cat "$NODE_SAMPLES" 2>/dev/null; done
      case "$(cat "$dir/.hostmode" 2>/dev/null)" in
        local) cat "$dir/.host.csv" 2>/dev/null ;;
        node:*) mode="$(cat "$dir/.hostmode")"; docker exec "${mode#node:}" cat "$NODE_HOST_SAMPLES" 2>/dev/null ;;
      esac
      # the helper's loop ends at its stop file and the container with it: a stopped container takes no exec, its file is copied out
      docker cp "$CG_NAME:$CG_SAMPLES" - 2>/dev/null | tar -xOf - 2>/dev/null
      cat "$dir"/.edge/*.csv 2>/dev/null
    } | grep -E '^[0-9]{13},' | sort -t, -k1,1n -k2,2
  } >"$dir/samples.csv"
  docker cp "$CG_NAME:$CG_PODMEM" - 2>/dev/null | tar -xOf - 2>/dev/null | grep -E '^[0-9]{13},' | sort -t, -k1,1n >"$dir/.podmem.csv"
  if [ -s "$dir/.podmem.csv" ]; then { echo "$PODMEM_HEADER"; cat "$dir/.podmem.csv"; } >"$dir/pod-memory.csv"; fi
  docker rm -f "$CG_NAME" >/dev/null 2>&1
  # pods.csv and the names of the pods it counts by uid (the pods there at the end, and every pod the events name)
  {
    echo "$PODS_HEADER"
    for n in $node_list; do docker exec "$n" cat "$NODE_PODS" 2>/dev/null; done | grep -E '^[0-9]{13},' | sort -t, -k1,1n -k2,2 -k3,3
  } >"$dir/pods.csv"
  {
    printf 'pod_uid\tnamespace\tname\n'
    {
      kubectl --context "$CTX" get pods -A -o json 2>/dev/null | jq -r '.items[] | [.metadata.uid, .metadata.namespace, .metadata.name] | @tsv'
      [ -s "$dir/.events.jsonl" ] && jq -r '.object.involvedObject | select(.kind == "Pod" and .uid != null) | [.uid, .namespace, .name] | @tsv' "$dir/.events.jsonl" 2>/dev/null
    } | sort -u
  } >"$dir/pod-names.tsv"
  { printf 'seen\tid12\tname\timage\n'; sort -t$'\t' -k2,2 -u "$dir/.containers.tsv" 2>/dev/null; } >"$dir/containers.tsv"
  # events.tsv: the watch's objects, one row each
  if [ -s "$dir/.events.jsonl" ]; then
    jq -r '.object | [(.lastTimestamp // .eventTime // .metadata.creationTimestamp), .metadata.namespace, (.involvedObject.kind + "/" + .involvedObject.name), .type, .reason, ((.count // 1) | tostring), (.message | gsub("[\t\n]"; " "))] | @tsv' "$dir/.events.jsonl" 2>/dev/null \
      | sort -u >"$dir/events.tsv"
  fi
  # kept-pod-logs: the pods that no longer run (deleted or evicted) and every pod of the kept namespaces
  # a static pod's log directory carries the kubelet's config hash, not the uid of its mirror pod
  live="$(kubectl --context "$CTX" get pods -A -o json 2>/dev/null | jq -r '.items[] | select(.status.phase != "Failed" and .metadata.deletionTimestamp == null) | .metadata.uid, (.metadata.annotations["kubernetes.io/config.hash"] // empty)')"
  for n in $node_list; do
    keep=""
    for f in $(docker exec "$n" ls "$NODE_LOGS" 2>/dev/null); do
      podkey="${f%%__*}"          # <namespace>_<pod>_<uid>
      ns="${podkey%%_*}"; uid="${podkey##*_}"
      want=0
      case " $KEEP_NS " in *" $ns "*) want=1 ;; esac
      printf '%s\n' "$live" | grep -qx "$uid" || want=1
      [ "$want" = 1 ] && keep="$keep $f"
    done
    [ -n "$keep" ] || continue
    mkdir -p "$dir/.tmp-$n"
    # shellcheck disable=SC2086
    docker exec "$n" tar -C "$NODE_LOGS" -cf - $keep 2>/dev/null | tar -xf - -C "$dir/.tmp-$n" 2>/dev/null
    for f in "$dir/.tmp-$n"/*; do
      [ -f "$f" ] || continue
      gzip -c "$f" >"$dir/kept-pod-logs/$(basename "$f" .log).log.gz"
    done
    rm -rf "$dir/.tmp-$n"
  done
  for n in $node_list; do
    docker exec "$n" rm -rf "$NODE_LOGS" "$NODE_SAMPLES" "$NODE_HOST_SAMPLES" "$NODE_PODS" "$NODE_LOGS_DONE" "$NODE_STOP" >/dev/null 2>&1
  done
  for pod in $(kubectl --context "$CTX" -n "$EDGE_NS" get pods -o name 2>/dev/null); do
    kubectl --context "$CTX" -n "$EDGE_NS" exec "${pod#pod/}" -- rm -f "$EDGE_STOP" >/dev/null 2>&1
  done
  # README.txt: first where the daemon put its containers and the pod's memory at the end, then the list of files
  cg_parent=""; cg_pod=""; cg_max=""
  [ -f "$dir/.cgparent" ] && read -r cg_parent cg_pod cg_max <"$dir/.cgparent"
  {
    if [ -z "$cg_parent" ]; then
      echo "Docker daemon      its cgroup tree was not read: no cg:, cgd: or daemon: rows"
    elif [ "$cg_parent" = - ]; then
      echo "Docker daemon      the cgroup of the first kind node was not found in the daemon's cgroup tree: the cgd: rows are those of the top-level docker only, no pod memory"
    elif [ "$cg_pod" = - ]; then
      echo "Docker daemon      puts its containers under $cg_parent of the machine's cgroups, not inside a pod: their memory counts against no pod's limit, no pod-memory.csv"
    else
      echo "Docker daemon      puts its containers under $cg_parent of the machine's cgroups, inside the pod $cg_pod, whose memory limit counts them"
    fi
    if [ -s "$dir/pod-memory.csv" ]; then
      bash -c "$(declare -f gib)
$PODMEMLINE" _ "$dir/pod-memory.csv"
    fi
    echo
    cat <<'README'
samples.csv        pressure (PSI cpu, memory and io: some/full avg10 and total microseconds) and CPU time of the host (source host: also the load average, the steal time and the number of CPUs), of the cgroup of each kind node (node:<name>) and of the machine's top-level cgroups (cg:<name>: also cpu.weight and the number of pods or containers in it), the same for the cgroup parent of the job's Docker daemon where it is not a top-level cgroup (daemon:parent) and for the pod it lies in (daemon:pod), the CPU time of every container of the machine's top-level docker cgroup and of the daemon's parent (cgd:<id12>), HAProxy's connection counters of each edge pod (edge:<pod>), every 1 to 5 s, by time
pod-memory.csv     where the daemon's parent lies inside a pod: the pod's memory.current, memory.peak, memory.max, the oom and oom_kill counts of memory.events (every OOM below the pod, the kind pods' own limits included), the oom count of memory.events.local (the pod's own limit only), memory.current of the daemon's parent, and from the pod's memory.stat anon, file (page cache, shmem included), kernel, shmem, slab_reclaimable and sock, every 5 s and at the end
pods.csv           per pod of each kind node: CPU time, memory.current, anon, file, socket buffers, kernel memory and the OOM kills of its cgroup, every 5 s
pod-names.tsv      the namespace and name of each pod uid in pods.csv
containers.tsv     the containers of the job's own Docker daemon (the kind nodes, k6, helpers) by their 12-character id: every other cgd: row is a neighbour's
db-connections.csv the connections of the database's primary by client and state (pg_stat_activity) and the private memory of their backends (the sum of RssAnon, KiB), every 5 s: unix_ms,pod,application_name,state,count,anon_kib
db-pools.csv       each pool's size of each replica (quader_db_pool_max_connections), read once when the pod is first seen: unix_ms,pod,db_pool,max_connections
secrets.tsv       the Secrets of every namespace at the start and at the end (resourceVersion, creation time, uid, writers); names and metadata only, no value
secrets-watch.log  every change of a Secret during the run, with the time the watch saw it
events.tsv         the Kubernetes events of every namespace, by time
pods-final.txt     kubectl get pods -A -o wide at the end
kept-pod-logs/     logs of pods deleted or evicted during the run, and of the secrets operator and cert-manager, one file per container instance
README
  } >"$dir/README.txt"
  { echo "unix_ms,pod,application_name,state,count,anon_kib"; cat "$dir/.db-connections.csv" 2>/dev/null; } >"$dir/db-connections.csv"
  { echo "unix_ms,pod,db_pool,max_connections"; cat "$dir/.db-pools.csv" 2>/dev/null; } >"$dir/db-pools.csv"
  rm -f "$dir"/.db-connections.csv "$dir"/.db-pools.csv "$dir"/.db-pools.seen
  rm -f "$dir"/.secrets-watch.fifo "$dir"/.stop "$dir"/.started "$dir"/.hostmode "$dir"/.*.pid "$dir"/.host.csv "$dir"/.events.jsonl "$dir"/.secrets-*.tsv "$dir"/.containers.tsv \
    "$dir"/.cgparent "$dir"/.podmem.csv
  rm -rf "$dir/.edge"
  log "evidence written to $dir"
  ;;
db-sample)
  [ -d "$dir" ] || die "e2e-evidence.sh db-sample: no directory $dir"
  db_sample
  ;;
*) die "usage: e2e-evidence.sh start|stop|db-sample <dir>" ;;
esac
exit 0
