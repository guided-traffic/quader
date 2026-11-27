# shellcheck shell=bash
# The names, ports and paths of one instance of the UI development profile; sourced by up.sh, config.sh, serve.sh, web.sh,
# renders.sh and down.sh of this directory.
#
#   ui_instance <checkout>      reads UI_INSTANCE; claims the instance for the checkout (below)
#   ui_release <checkout>       gives the checkout's claim of UI_INSTANCE back (down.sh --purge)
#
# Several instances run side by side on one machine, one per builder (F-207, F-208, F-053), the shape of the integration stack's
# instances (test/integration/instance.sh). Instance 1 is the profile as it was before instances existed, the owner's: compose
# project and container prefix quader-ui-dev, state in <checkout>/.dev/ui-dev, the ports 4200 (dev server), 19080 (quader), 19081
# (ops), 19090 (sign-in provider), 25432, 26390 and 28333 (PostgreSQL, Valkey, S3). Instance n (2 to 8) is quader-ui-dev<n>
# (containers quader-ui-dev<n>-postgres, ..., which no glob quader-ui-dev-* matches), state in <checkout>/.dev/ui-dev-<n>, and
# every port of instance 1 plus (n - 1) * 100. Instance 9 would put the dev server on 5000, which macOS's AirPlay receiver
# holds, so the number stops at 8; the ports of every profile are the table of docs/developer/build-and-test.md.
#
# Instance 1 is refused in a git worktree: its names and ports are the main checkout's stack, and a worktree's up.sh recreated
# its containers with the worktree's certificates under whoever was looking at it (2026-11-20).
#
# Instances 2 to 8 are claimed, not chosen (Q-UI-1044): two builders that chose instance 6 a minute apart on 2026-11-23 each
# started its containers, and the second recreated the first's with its own certificates. A claim is the file
# <main>/.dev/ui-dev-claims/<n> that holds the claiming checkout's path; <main> is the main checkout of the repository (git's
# common directory), so every worktree sees every claim. Claims are read and written under an flock(2) lock on
# <main>/.dev/ui-dev-claims.lock (flock(1) where it exists, lockf(1) otherwise). The rules:
#   - UI_INSTANCE unset: instance 1 in the main checkout; in a worktree the instance it claims already, else the first one of 2 to
#     8 that no existing checkout claims, which it claims.
#   - UI_INSTANCE=n (2 to 8): refused while another checkout that still exists claims it; otherwise claimed.
#   - An instance with no claim whose compose project has containers (started before claims existed) is someone's whose checkout is
#     not known: it is never picked, and naming it is refused until its owner ends it.
#   - A claim whose checkout no longer exists (a removed worktree) is stale: the claim is taken over, and the instance's containers
#     and volumes, which belonged to that checkout, are removed first (its database was written with another key ring).
#   - The claim stays through `make ui-dev-down` (the data is kept for the checkout) and ends with `make ui-dev-down PURGE=1`.
#   - A claim whose checkout still exists but has been idle for UI_CLAIM_IDLE_MINUTES (default 60) is abandoned and taken over the
#     same way, data removed first (Q-UI-1614): a builder's agent that ended left its claim, and on 2026-11-27 every gate found the
#     instances 2 to 8 claimed by finished builders. Idle means all of: no process has its working directory in the checkout or
#     names the checkout on its command line (lsof, else /proc; where neither answers the claim is never idle), no file of the
#     checkout outside node_modules, .angular, .out and .bin changed, and the claim was not used (each ui_instance of the owner
#     touches it) in those minutes. Instance 1 has no claim and is never taken.
#
# It sets (and exports, for docker compose and the dev server) UI_INSTANCE, UI_PROJECT, UI_STATE, UI_WEB_PORT, UI_BACKEND_PORT,
# UI_OPS_PORT, UI_IDP_PORT, UI_PG_PORT, UI_VALKEY_PORT, UI_S3_PORT, the scanners' UI_SCAN_PORT, UI_TRIVY_PORT, UI_TRIVY_OPS_PORT and
# UI_TRIVY_DB_OPS_PORT (19082 to 19085 in instance 1), and the malware scanning's UI_CLAMAV_PORT, UI_CLAMAV_OPS_PORT, UI_FRESHCLAM_PORT and
# UI_FRESHCLAM_OPS_PORT (19086 to 19089 in instance 1), and the profile's Bastion's UI_BASTION_PORT and UI_BASTION_OPS_PORT (19091 and 19092 in
# instance 1). The names carry no QUADER_ prefix: a quader process refuses
# every QUADER_ variable that is not a setting. UI_CLAIMS names another directory for the claims (a test of this file).

# ui_main <checkout> prints the main checkout of the checkout's repository, and the checkout itself outside git.
ui_main() {
  local common
  common="$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  if [ -z "$common" ]; then
    (cd "$1" && pwd)
    return 0
  fi
  dirname "$common"
}

# ui_is_worktree <checkout> succeeds in a git worktree that is not the main checkout.
ui_is_worktree() {
  local git_dir common
  git_dir="$(git -C "$1" rev-parse --absolute-git-dir 2>/dev/null || true)"
  common="$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  [ -n "$git_dir" ] && [ -n "$common" ] && [ "$git_dir" != "$common" ]
}

# ui_unclaimed_running <n> succeeds when instance n's compose project has containers.
ui_unclaimed_running() {
  [ -n "$(docker ps -aq --filter "label=com.docker.compose.project=quader-ui-dev$1" 2>/dev/null)" ]
}

# ui_checkout_busy <checkout> succeeds when a process works in the checkout: its working directory lies in it, or its command line
# names a path in it. It also succeeds when the machine cannot list the processes' working directories, so that a claim is only taken
# from a checkout that is known to be idle.
ui_checkout_busy() {
  local checkout="$1" cwds l
  if command -v lsof >/dev/null 2>&1; then
    cwds="$(lsof -w -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' || true)"
  elif [ -d /proc/self ]; then
    cwds="$(for l in /proc/[0-9]*/cwd; do readlink "$l" 2>/dev/null || true; done)"
  else
    return 0
  fi
  cwds="$cwds"$'\n'"$(ps -A -o args= 2>/dev/null || true)"
  awk -v c="$checkout" '$0 == c || index($0, c "/") || index($0, c " ") || (length($0) > length(c) && substr($0, length($0) - length(c)) == " " c) { found = 1 }
    END { exit !found }' <<<"$cwds"
}

# ui_claim_idle <claim file> <checkout> succeeds when the claim is abandoned: the checkout has no process (ui_checkout_busy), and
# neither the claim nor a file of the checkout (node_modules, .angular, .out and .bin left out: builds and renders write there while
# a process runs) changed for UI_CLAIM_IDLE_MINUTES. It prints nothing.
ui_claim_idle() {
  local claim="$1" checkout="$2" minutes="${UI_CLAIM_IDLE_MINUTES:-60}" recent
  case "$minutes" in '' | *[!0-9]* | 0) return 1 ;; esac
  recent="$(find "$claim" -mmin "-$minutes" -print 2>/dev/null || true)"
  [ -z "$recent" ] || return 1
  recent="$(find "$checkout" \( -name node_modules -o -name .angular -o -name .out -o -name .bin \) -prune -o -mmin "-$minutes" -print -quit 2>/dev/null || true)"
  [ -z "$recent" ] || return 1
  ! ui_checkout_busy "$checkout"
}

# ui_purge_instance <n> removes the containers and volumes of instance n's compose project: they belonged to a checkout whose claim
# ends, and its database was written with another key ring.
ui_purge_instance() {
  docker ps -aq --filter "label=com.docker.compose.project=quader-ui-dev$1" | xargs docker rm -f >/dev/null 2>&1 || true
  docker volume ls -q --filter "label=com.docker.compose.project=quader-ui-dev$1" | xargs docker volume rm >/dev/null 2>&1 || true
}

# ui_claims_locked <checkout> <want> <mode> runs under the claims' lock (descriptor 8) and prints the instance: want is the number the
# caller named or empty; mode is claim or release.
ui_claims_locked() {
  local checkout="$1" want="$2" mode="$3" dir="$4" n owner
  if [ "$mode" = release ]; then
    if [ -f "$dir/$want" ] && [ "$(cat "$dir/$want")" = "$checkout" ]; then
      rm -f "$dir/$want"
    fi
    echo "$want"
    return 0
  fi
  if [ -z "$want" ]; then
    for n in 2 3 4 5 6 7 8; do
      if [ -f "$dir/$n" ] && [ "$(cat "$dir/$n")" = "$checkout" ]; then
        touch "$dir/$n"
        echo "$n"
        return 0
      fi
    done
    for n in 2 3 4 5 6 7 8; do
      if { [ ! -f "$dir/$n" ] && ! ui_unclaimed_running "$n"; } || { [ -f "$dir/$n" ] && [ ! -d "$(cat "$dir/$n")" ]; }; then
        want="$n"
        break
      fi
    done
    # No free instance: the first whose claim is abandoned (ui_claim_idle), taken over below.
    if [ -z "$want" ]; then
      for n in 2 3 4 5 6 7 8; do
        if [ -f "$dir/$n" ] && ui_claim_idle "$dir/$n" "$(cat "$dir/$n")"; then
          want="$n"
          break
        fi
      done
    fi
    if [ -z "$want" ]; then
      echo "every instance of the UI development profile (2 to 8) is claimed by a checkout that still exists and was used in the last" \
        "${UI_CLAIM_IDLE_MINUTES:-60} minutes:" >&2
      for n in 2 3 4 5 6 7 8; do
        if [ -f "$dir/$n" ]; then echo "  $n: $(cat "$dir/$n")" >&2; else echo "  $n: running without a claim" >&2; fi
      done
      echo "end one with 'make ui-dev-down UI_INSTANCE=<n> PURGE=1' in its checkout" >&2
      return 2
    fi
  fi
  # down.sh (UI_DOWN=1) may end such an instance: its owner stops what it started before claims existed.
  if [ ! -f "$dir/$want" ] && [ -z "${UI_DOWN:-}" ] && ui_unclaimed_running "$want"; then
    echo "instance $want of the UI development profile has containers and no claim: it was started before instances were claimed," \
      "by a checkout that is not known; leave UI_INSTANCE unset for a free one, or end it in its checkout ('make ui-dev-down UI_INSTANCE=$want PURGE=1')" >&2
    return 2
  fi
  if [ -f "$dir/$want" ]; then
    owner="$(cat "$dir/$want")"
    if [ "$owner" = "$checkout" ]; then
      touch "$dir/$want"
      echo "$want"
      return 0
    fi
    if [ ! -d "$owner" ]; then
      echo "instance $want of the UI development profile was claimed by $owner, which no longer exists: its containers and volumes" \
        "are removed and the instance is this checkout's" >&2
    elif ui_claim_idle "$dir/$want" "$owner"; then
      echo "instance $want of the UI development profile was claimed by $owner, which has had no process and no change for" \
        "${UI_CLAIM_IDLE_MINUTES:-60} minutes: its containers and volumes are removed and the instance is this checkout's" >&2
    else
      echo "instance $want of the UI development profile is claimed by $owner; leave UI_INSTANCE unset for a free one, or end" \
        "that claim there with 'make ui-dev-down UI_INSTANCE=$want PURGE=1'" >&2
      return 2
    fi
    ui_purge_instance "$want"
  fi
  printf '%s\n' "$checkout" > "$dir/$want.tmp"
  mv -f "$dir/$want.tmp" "$dir/$want"
  echo "$want"
}

# ui_claim <checkout> <want> <mode> takes the claims' lock and decides (ui_claims_locked).
ui_claim() {
  local checkout="$1" want="$2" mode="$3" dir out rc
  dir="${UI_CLAIMS:-$(ui_main "$checkout")/.dev/ui-dev-claims}"
  mkdir -p "$dir"
  exec 8>>"$dir.lock"
  if command -v flock >/dev/null; then
    flock 8
  elif command -v lockf >/dev/null; then
    lockf -s 8
  fi
  rc=0
  out="$(ui_claims_locked "$checkout" "$want" "$mode" "$dir")" || rc=$?
  exec 8>&-
  [ "$rc" = 0 ] || return "$rc"
  printf '%s\n' "$out"
}

ui_instance() {
  local checkout n
  checkout="$(cd "$1" && pwd)"
  n="${UI_INSTANCE:-}"
  case "$n" in
    "" | [1-8]) ;;
    *) echo "the UI development profile's instance must be a number from 1 to 8 (UI_INSTANCE, got '$n')" >&2; return 2 ;;
  esac
  if ui_is_worktree "$checkout"; then
    if [ "$n" = 1 ]; then
      echo "instance 1 of the UI development profile is the main checkout's (quader-ui-dev-*, ports 4200, 19080, 19081, 19090);" \
        "this is a worktree: leave UI_INSTANCE unset, and it claims a free one of 2 to 8" >&2
      return 2
    fi
    n="$(ui_claim "$checkout" "$n" claim)" || return 2
  elif [ -z "$n" ]; then
    n=1
  elif [ "$n" != 1 ]; then
    n="$(ui_claim "$checkout" "$n" claim)" || return 2
  fi
  local off=$(((n - 1) * 100))
  UI_INSTANCE="$n"
  if [ "$n" = 1 ]; then
    UI_PROJECT=quader-ui-dev
    UI_STATE="$checkout/.dev/ui-dev"
  else
    UI_PROJECT="quader-ui-dev$n"
    UI_STATE="$checkout/.dev/ui-dev-$n"
  fi
  UI_WEB_PORT=$((4200 + off))
  UI_BACKEND_PORT=$((19080 + off))
  UI_OPS_PORT=$((19081 + off))
  UI_IDP_PORT=$((19090 + off))
  UI_PG_PORT=$((25432 + off))
  UI_VALKEY_PORT=$((26390 + off))
  UI_S3_PORT=$((28333 + off))
  # The scanners of the profile (deploy/ui-dev/scanners.sh, Q-ARCH-1064): quader's scan listener, vuln-scanner-trivy's listener and
  # operations listener, and the operations listener of its database's loader.
  UI_SCAN_PORT=$((19082 + off))
  UI_TRIVY_PORT=$((19083 + off))
  UI_TRIVY_OPS_PORT=$((19084 + off))
  UI_TRIVY_DB_OPS_PORT=$((19085 + off))
  # The malware scanning of the profile (deploy/ui-dev/malware.sh, Q-ARCH-1065): the listener and the operations listener of
  # malware-scanner-clamav, which Docker publishes on 127.0.0.1 from the scanner's container, and those of malware-updater-freshclam.
  UI_CLAMAV_PORT=$((19086 + off))
  UI_CLAMAV_OPS_PORT=$((19087 + off))
  UI_FRESHCLAM_PORT=$((19088 + off))
  UI_FRESHCLAM_OPS_PORT=$((19089 + off))
  # The profile's Bastion (deploy/ui-dev/bastion.sh): its client listener, the address its web interface and its package routes answer
  # on (https://localhost:<port>), and its operations listener.
  UI_BASTION_PORT=$((19091 + off))
  UI_BASTION_OPS_PORT=$((19092 + off))
  export UI_INSTANCE UI_PROJECT UI_STATE UI_WEB_PORT UI_BACKEND_PORT UI_OPS_PORT UI_IDP_PORT UI_PG_PORT UI_VALKEY_PORT UI_S3_PORT
  export UI_SCAN_PORT UI_TRIVY_PORT UI_TRIVY_OPS_PORT UI_TRIVY_DB_OPS_PORT UI_CLAMAV_PORT UI_CLAMAV_OPS_PORT UI_FRESHCLAM_PORT UI_FRESHCLAM_OPS_PORT
  export UI_BASTION_PORT UI_BASTION_OPS_PORT
}

# ui_release <checkout> ends the checkout's claim of UI_INSTANCE, as ui_instance set it; instance 1 has no claim.
ui_release() {
  local checkout
  checkout="$(cd "$1" && pwd)"
  [ "${UI_INSTANCE:-1}" != 1 ] || return 0
  ui_claim "$checkout" "$UI_INSTANCE" release >/dev/null
}
