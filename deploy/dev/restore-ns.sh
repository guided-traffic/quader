#!/usr/bin/env bash
# The second installation of the restore drill (R-BAK-021, R-HA-060, docs/developer/e2e.md#the-backup-tests): a release of the chart
# in the namespace quader-restore, next to the main installation of the dev cluster, with a database, a Valkey and a blob bucket
# of its own. The backup it restores is the one the main installation wrote into the bucket quader-backups.
#
#   restore-ns.sh infra                  the namespace, the dev CA, the key ring and the S3 login copied into it, the certificate,
#                                        a fresh CloudNativePG cluster (with the schema R-BAK-020 check 4 requires), the Valkey
#                                        and an empty blob bucket; any earlier restore installation of the namespace is removed first
#   restore-ns.sh install <backup-id> [helm args...]
#                                        helm install with restore.enabled, the backup ID and both roles at zero (R-BAK-021); returns
#                                        when the Job is created, it does not wait for it (the Job is an ordinary Job)
#   restore-ns.sh serve [helm args...]   the final helm upgrade of R-BAK-021: restore.enabled off, a replica of every role, waits until
#                                        they are ready
#   restore-ns.sh status                 the release and the pods
#   restore-ns.sh down                   uninstall the release and delete the namespace
#
# Environment: QUADER_E2E_RESTORE_NAMESPACE (default quader-restore), QUADER_IMAGE (default guidedtraffic/quader:dev),
# QUADER_RESTORE_HELM_EXTRA (extra helm arguments for install and serve; a word-split string),
# QUADER_RESTORE_PROFILE (unset or empty: the base values of values-restore.yaml; `vuln`: also values-restore-vuln.yaml, the scanning
# reading the test advisory source and two packages replicas, Q-VULN-127 and docs/developer/e2e.md#the-vulnerability-tests).
# Install and serve set server.base_url to the edge port of the source (the cluster's published port 30443, as release-values.sh reads it),
# so that a restored token and the restored CI issuers keep the source's address (R-BAK-021, Q-BAK-60).
set -euo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

RNS="${QUADER_E2E_RESTORE_NAMESPACE:-quader-restore}"
IMAGE="${QUADER_IMAGE:-$DEV_IMAGE}"
case "${QUADER_RESTORE_PROFILE:-}" in "" | vuln) ;; *) die "QUADER_RESTORE_PROFILE must be empty or vuln, not '${QUADER_RESTORE_PROFILE}'" ;; esac
rn() { kubectl --context "$CTX" -n "$RNS" "$@"; }

# render_r <manifest> substitutes the namespaces and the pinned images.
render_r() { render "$1" | sed -e "s|\${RESTORE_NS}|$RNS|g"; }

# copy_secret <source name> <target name> <type> <source key:target key>...: a Secret of the main namespace, copied into this one.
copy_secret() {
  local src="$1" dst="$2" type="$3"; shift 3
  local data="{}" spec
  for spec in "$@"; do
    data="$(printf '%s' "$data" | jq --arg k "${spec#*:}" --arg v "$(kn get secret "$src" -o "go-template={{index .data \"${spec%%:*}\"}}")" '. + {($k): $v}')"
  done
  jq -n --arg ns "$RNS" --arg name "$dst" --arg type "$type" --argjson data "$data" \
    '{apiVersion: "v1", kind: "Secret", type: $type, metadata: {name: $name, namespace: $ns}, data: $data}' \
    | kubectl --context "$CTX" apply -f - >/dev/null
}

probe_cidrs() {
  local out="[" sep="" c
  for c in $(docker network inspect kind -f '{{range .IPAM.Config}}{{.Subnet}} {{end}}' 2>/dev/null || true); do out="$out$sep\"$c\""; sep=","; done
  printf '%s]' "$out"
}

# values_file renders values-restore.yaml for this namespace and prints its path.
values_file() {
  local f="$STATE_DIR/values-restore-rendered.yaml"
  mkdir -p "$STATE_DIR"
  render_r "$DEV_DIR/values-restore.yaml" > "$f"
  printf '%s\n' "$f"
}

# vuln_values_file renders values-restore-vuln.yaml for this namespace and prints its path.
vuln_values_file() {
  local f="$STATE_DIR/values-restore-vuln-rendered.yaml"
  mkdir -p "$STATE_DIR"
  render_r "$DEV_DIR/values-restore-vuln.yaml" > "$f"
  printf '%s\n' "$f"
}

# edge_port prints the host port of the source installation's edge, as release-values.sh reads it.
edge_port() {
  local p
  p="$(docker port "${CLUSTER}-control-plane" 30443/tcp 2>/dev/null | head -1 | sed 's/.*://' || true)"
  printf '%s\n' "${p:-$DEV_HTTP_PORT}"
}

helm_args() {
  local image_id; image_id="$(docker image inspect -f '{{.Id}}' "$IMAGE")"
  printf '%s\n' -f "$(values_file)"
  case "${QUADER_RESTORE_PROFILE:-}" in
    "") ;;
    vuln) printf '%s\n' -f "$(vuln_values_file)" ;;
  esac
  printf '%s\n' --set-string "config.server.base_url=https://localhost:$(edge_port)" --set-json "networkPolicy.ingress.probeCIDRs=$(probe_cidrs)" \
    --set-string "api.podAnnotations.quader-dev/image-id=$image_id" --set-string "packages.podAnnotations.quader-dev/image-id=$image_id" \
    --set-string "worker.podAnnotations.quader-dev/image-id=$image_id" \
    --set-string "backup.podAnnotations.quader-dev/image-id=$image_id" --set-string "restore.podAnnotations.quader-dev/image-id=$image_id"
  # The chart runs $IMAGE, the checkout's tag, not the tag of values-restore.yaml (F-160).
  RELEASE_VALUES=(); image_values image "$IMAGE"
  printf '%s\n' "${RELEASE_VALUES[@]}"
}

down() {
  helm --kube-context "$CTX" -n "$RNS" uninstall quader >/dev/null 2>&1 || true
  kubectl --context "$CTX" delete namespace "$RNS" --ignore-not-found --wait=true >/dev/null
  kn delete job quader-s3-restore-bucket --ignore-not-found >/dev/null
}

infra() {
  require_cluster
  log "removing an earlier restore installation"
  down
  log "namespace $RNS"
  kubectl --context "$CTX" apply -f - >/dev/null <<NSYAML
apiVersion: v1
kind: Namespace
metadata:
  name: $RNS
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
    app.kubernetes.io/part-of: quader-dev
NSYAML
  copy_secret quader-dev-ca quader-dev-ca kubernetes.io/tls tls.crt:tls.crt tls.key:tls.key ca.crt:ca.crt
  # The key ring of the source: a restore refuses a key ring that does not open the backup's canary rows (R-BAK-020 check 3).
  copy_secret quader-encryption-keys quader-encryption-keys Opaque dev.key:dev.key
  copy_secret quader-s3 quader-s3 Opaque access-key-id:access-key-id secret-access-key:secret-access-key
  copy_secret quader-s3-tls quader-s3-tls Opaque ca.crt:ca.crt

  log "applying restore-ns.yaml (issuer, PostgreSQL, Valkey, empty bucket)"
  render_r "$DEV_DIR/restore-ns.yaml" | kubectl --context "$CTX" apply -f - >/dev/null

  local start; start="$(date +%s)"
  until rn get secret restore-valkey-auth -o "go-template={{with index .data \"password\"}}{{.}}{{end}}" 2>/dev/null | grep -q .; do
    [ $(( $(date +%s) - start )) -lt 120 ] || die "Secret restore-valkey-auth has no field password after 120s: is the internal-secrets-operator running?"
    sleep 1
  done
  until rn get statefulset restore-valkey restore-valkey-sentinel >/dev/null 2>&1; do sleep 1; done
  (NS="$RNS"; sts_wait_ready restore-valkey 2 300; sts_wait_ready restore-valkey-sentinel 3 300)
  until rn get cluster quader-db >/dev/null 2>&1; do sleep 1; done
  rn wait --for=condition=Ready cluster/quader-db --timeout=10m >/dev/null
  kn wait --for=condition=Complete job/quader-s3-restore-bucket --timeout=5m >/dev/null
  log "infra ready in $(elapsed "$start")"
}

install() {
  local id="${1:?usage: restore-ns.sh install <backup-id> [helm args...]}"; shift
  local args; mapfile -t args < <(helm_args)
  log "helm install quader ($RNS), restore of $id, both roles at zero"
  # shellcheck disable=SC2086
  helm --kube-context "$CTX" install quader "$REPO_ROOT/deploy/helm/quader" -n "$RNS" "${args[@]}" \
    --set api.replicaCount=0 --set api.autoscaling.enabled=false \
    --set packages.replicaCount=0 --set packages.autoscaling.enabled=false \
    --set worker.replicaCount=0 --set worker.autoscaling.enabled=false \
    --set restore.enabled=true --set-string "restore.backupId=$id" ${QUADER_RESTORE_HELM_EXTRA:-} "$@"
}

serve() {
  local args; mapfile -t args < <(helm_args)
  log "helm upgrade quader ($RNS), restore.enabled off"
  # shellcheck disable=SC2086
  helm --kube-context "$CTX" upgrade quader "$REPO_ROOT/deploy/helm/quader" -n "$RNS" "${args[@]}" \
    --wait --timeout 10m ${QUADER_RESTORE_HELM_EXTRA:-} "$@"
  rn get pods -o wide
}

case "${1:-}" in
  infra) infra ;;
  install) shift; install "$@" ;;
  serve) shift; serve "$@" ;;
  status) helm --kube-context "$CTX" ls -n "$RNS"; rn get pods,jobs -o wide ;;
  down) down; log "the restore installation is removed" ;;
  *) die "usage: restore-ns.sh infra | install <backup-id> | serve | status | down" ;;
esac
