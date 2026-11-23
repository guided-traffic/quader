# shellcheck shell=bash
# The names, ports and paths of one instance of the integration stack; sourced by up.sh, down.sh, rotate-cert.sh and locked.sh.
#
#   it_instance <n> <checkout>
#
# Several instances run side by side on one machine, each with a lock of its own (locked.sh, run.sh). Instance 1 is the stack
# the pipeline starts and the one every script defaulted to before instances existed: compose project and container prefix
# quader-it, state in <checkout>/.dev/integration, lock <checkout>/.dev/integration.lock, the ports below. Instance n (2 to 9)
# is quader-it<n>, .dev/integration-<n>, .dev/integration-<n>.lock, and every port of instance 1 plus (n - 1) * 100. The step of
# 100 keeps the instances apart from each other and from every other port the repository's local profiles take (the table in
# docs/developer/build-and-test.md, "Instances and their ports"); instance 10 would take instance 1's PgBouncer port (15432 + 900 + 100 =
# 16432), so the number stops at 9. conf/sentinel.sh derives its primary's port from its own by the same step.
#
# It sets (and exports, for docker compose) IT_INSTANCE, IT_PROJECT, IT_STATE, IT_LOCK, IT_PG_PORT, IT_PGBOUNCER_PORT,
# IT_VALKEY_PRIMARY_PORT, IT_VALKEY_REPLICA_PORT, IT_SENTINEL_1_PORT, IT_SENTINEL_2_PORT, IT_SENTINEL_3_PORT and IT_S3_PORT. The
# names carry no QUADER_ prefix: a quader process refuses every QUADER_ variable that is not a setting.

it_instance() {
  local n="$1" checkout="$2"
  case "$n" in
    [1-9]) ;;
    *) echo "the integration stack's instance must be a number from 1 to 9 (got '$n')" >&2; return 2 ;;
  esac
  local off=$(((n - 1) * 100))
  IT_INSTANCE="$n"
  if [ "$n" = 1 ]; then
    IT_PROJECT=quader-it
    IT_STATE="$checkout/.dev/integration"
  else
    IT_PROJECT="quader-it$n"
    IT_STATE="$checkout/.dev/integration-$n"
  fi
  IT_LOCK="$IT_STATE.lock"
  IT_PG_PORT=$((15432 + off))
  IT_PGBOUNCER_PORT=$((16432 + off))
  IT_VALKEY_PRIMARY_PORT=$((16379 + off))
  IT_VALKEY_REPLICA_PORT=$((16380 + off))
  IT_SENTINEL_1_PORT=$((26381 + off))
  IT_SENTINEL_2_PORT=$((26382 + off))
  IT_SENTINEL_3_PORT=$((26383 + off))
  IT_S3_PORT=$((18333 + off))
  export IT_INSTANCE IT_PROJECT IT_STATE IT_LOCK IT_PG_PORT IT_PGBOUNCER_PORT IT_VALKEY_PRIMARY_PORT IT_VALKEY_REPLICA_PORT \
    IT_SENTINEL_1_PORT IT_SENTINEL_2_PORT IT_SENTINEL_3_PORT IT_S3_PORT
}
