# shellcheck shell=bash
# The names, ports and paths of one instance of the UI development profile; sourced by up.sh, config.sh, serve.sh, web.sh,
# renders.sh and down.sh of this directory.
#
#   ui_instance <checkout>      reads UI_INSTANCE (default 1)
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
# its containers with the worktree's certificates under whoever was looking at it (2026-11-20). A builder takes 2 to 8.
#
# It sets (and exports, for docker compose and the dev server) UI_INSTANCE, UI_PROJECT, UI_STATE, UI_WEB_PORT, UI_BACKEND_PORT,
# UI_OPS_PORT, UI_IDP_PORT, UI_PG_PORT, UI_VALKEY_PORT and UI_S3_PORT. The names carry no QUADER_ prefix: a quader process refuses
# every QUADER_ variable that is not a setting.
ui_instance() {
  local checkout="$1" n="${UI_INSTANCE:-1}"
  case "$n" in
    [1-8]) ;;
    *) echo "the UI development profile's instance must be a number from 1 to 8 (UI_INSTANCE, got '$n')" >&2; return 2 ;;
  esac
  if [ "$n" = 1 ]; then
    local git_dir common
    git_dir="$(git -C "$checkout" rev-parse --absolute-git-dir 2>/dev/null || true)"
    common="$(git -C "$checkout" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
    if [ -n "$git_dir" ] && [ -n "$common" ] && [ "$git_dir" != "$common" ]; then
      echo "instance 1 of the UI development profile is the main checkout's (quader-ui-dev-*, ports 4200, 19080, 19081, 19090);" \
        "this is a worktree: set UI_INSTANCE to 2..8 (make ui-dev-up UI_INSTANCE=2)" >&2
      return 2
    fi
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
  export UI_INSTANCE UI_PROJECT UI_STATE UI_WEB_PORT UI_BACKEND_PORT UI_OPS_PORT UI_IDP_PORT UI_PG_PORT UI_VALKEY_PORT UI_S3_PORT
}
