#!/usr/bin/env bash
# The e2e Bastions: the chart quader-bastion in a second namespace of the dev cluster. The set bastion (R-BAS-049,
# docs/developer/e2e.md#the-bastion-tests) installs two, in front of the main installation of the `bastion` profile
# (QUADER_PROFILE=bastion up.sh); the set oci (R-BAS-060, docs/developer/e2e.md#the-bastion-case-of-the-set-oci) installs one, in front of
# the main installation of the profile `oci`:
#
#   bastion-volume  host bastion-volume.quader.localhost:<edge port>, registration `partners`, the volume backend, no Valkey
#   bastion-bucket  host bastion-bucket.quader.localhost:<edge port>, registration `partners-bucket`, a SeaweedFS bucket
#                   and the Valkey of the namespace (bastion.yaml)
#   bastion-oci     host bastion-oci.quader.localhost:<edge port>, registration `partners-oci`, one replica, the volume backend on an
#                   emptyDir, no Valkey (oci-bastion.yaml, oci-bastion-chart.yaml)
#
#   bastion.sh infra              the namespace, the dev CA and the bucket credentials copied into it, the Valkey, the
#                                 certificates, the signing key, the bucket; idempotent
#   bastion.sh infra oci          the namespace, the dev CA and its trust bundle, the edge certificate of bastion-oci: what the Bastion of the
#                                 set oci needs and nothing more; idempotent
#   bastion.sh install <volume|bucket|oci>
#                                 helm upgrade --install of one release and wait until its replicas are ready. The token Secret
#                                 (bastion-token-<variant>) must exist: the e2e test creates the registration and the token
#                                 through the management API and writes the Secret (test/e2e/bastion_env_test.go, oci_bastion_test.go)
#   bastion.sh status             the releases and pods
#   bastion.sh down               uninstall the releases and delete the namespace
#
# Environment: QUADER_E2E_BASTION_NAMESPACE (default quader-bastion), QUADER_IMAGE (default guidedtraffic/quader:dev, as up.sh),
# QUADER_BASTION_HELM_EXTRA (extra arguments of the helm upgrade, the mismatch scenario sets the image tag), QUADER_DEV_HTTP_PORT.
set -euo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

BNS="${QUADER_E2E_BASTION_NAMESPACE:-quader-bastion}"
IMAGE="${QUADER_IMAGE:-$DEV_IMAGE}"
EDGE_PORT="$(docker port "${CLUSTER}-control-plane" 30443/tcp 2>/dev/null | head -1 | sed 's/.*://' || true)"
EDGE_PORT="${EDGE_PORT:-$DEV_HTTP_PORT}"
bn() { kubectl --context "$CTX" -n "$BNS" "$@"; }

render_b() { render "$1" | sed -e "s|\${BASTION_NS}|$BNS|g"; }

# copy_secret <name> <target name> <type> <source key:target key>...: a Secret of the main namespace, copied into the Bastion namespace.
copy_secret() {
  local src="$1" dst="$2" type="$3"; shift 3
  local data="{}" spec
  for spec in "$@"; do
    data="$(printf '%s' "$data" | jq --arg k "${spec#*:}" --arg v "$(kn get secret "$src" -o "go-template={{index .data \"${spec%%:*}\"}}")" '. + {($k): $v}')"
  done
  jq -n --arg ns "$BNS" --arg name "$dst" --arg type "$type" --argjson data "$data" \
    '{apiVersion: "v1", kind: "Secret", type: $type, metadata: {name: $name, namespace: $ns}, data: $data}' \
    | kubectl --context "$CTX" apply -f - >/dev/null
}

# infra_base: the namespace and the dev CA with its trust bundle, which every e2e Bastion needs.
infra_base() {
  log "namespace $BNS"
  kubectl --context "$CTX" apply -f - >/dev/null <<NSYAML
apiVersion: v1
kind: Namespace
metadata:
  name: $BNS
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
    app.kubernetes.io/part-of: quader-dev
NSYAML

  # The dev CA signs for this namespace too (the Issuer of bastion.yaml and of oci-bastion.yaml reads it). A development key, copied on purpose.
  copy_secret quader-dev-ca quader-dev-ca kubernetes.io/tls tls.crt:tls.crt tls.key:tls.key ca.crt:ca.crt
  # The trust of the Bastion (trustedCA): the dev CA as one bundle file.
  copy_secret quader-dev-ca bastion-ca-bundle Opaque ca.crt:ca-bundle.crt
}

# infra_oci: what the Bastion of the set oci needs besides: the Issuer and the edge certificate of its host (oci-bastion.yaml).
infra_oci() {
  require_cluster
  local start; start="$(date +%s)"
  infra_base
  render_b "$DEV_DIR/oci-bastion.yaml" | kubectl --context "$CTX" apply -f - >/dev/null
  bn wait --for=condition=Ready certificate/bastion-oci-edge-tls --timeout=3m >/dev/null
  log "infra of the Bastion of the set oci ready in $(elapsed "$start")"
}

infra() {
  require_cluster
  infra_base
  # The bucket credentials: the identity of the dev SeaweedFS (secrets.yaml), copied.
  copy_secret quader-s3 bastion-s3 Opaque access-key-id:access-key-id secret-access-key:secret-access-key

  kn delete job quader-s3-bastion-bucket --ignore-not-found >/dev/null
  log "applying bastion.yaml (Valkey, certificates, signing key, bucket)"
  render_b "$DEV_DIR/bastion.yaml" | kubectl --context "$CTX" apply -f - >/dev/null

  local start spec; start="$(date +%s)"
  for spec in bastion-valkey-auth:password bastion-signing-key:key; do
    until [ -n "$(bn get secret "${spec%%:*}" -o "go-template={{with index .data \"${spec#*:}\"}}{{.}}{{end}}" 2>/dev/null || true)" ]; do
      [ $(( $(date +%s) - start )) -lt 120 ] || die "Secret ${spec%%:*} has no field ${spec#*:} after 120s: is the internal-secrets-operator running?"
      sleep 1
    done
  done
  bn wait --for=condition=Ready certificate/bastion-volume-edge-tls certificate/bastion-bucket-edge-tls --timeout=3m >/dev/null
  until bn get statefulset bastion-valkey bastion-valkey-sentinel >/dev/null 2>&1; do sleep 1; done
  (NS="$BNS"; sts_wait_ready bastion-valkey 2 300; sts_wait_ready bastion-valkey-sentinel 3 300)
  kn wait --for=condition=Complete job/quader-s3-bastion-bucket --timeout=5m >/dev/null
  log "infra ready in $(elapsed "$start")"
}

install() {
  local variant="$1" values
  case "$variant" in
    volume|bucket) values="$DEV_DIR/bastion-chart-$variant.yaml" ;;
    oci) values="$DEV_DIR/oci-bastion-chart.yaml" ;;
    *) die "install: volume, bucket or oci, not '$variant'" ;;
  esac
  bn get secret "bastion-token-$variant" >/dev/null 2>&1 || die "Secret bastion-token-$variant is missing; the e2e test creates it from the registration's token"
  local probe="[" sep="" c
  for c in $(docker network inspect kind -f '{{range .IPAM.Config}}{{.Subnet}} {{end}}' 2>/dev/null || true); do probe="$probe$sep\"$c\""; sep=","; done
  probe="$probe]"
  # The image keeps its tag while the image behind it changes (kind load): the ID as a pod annotation rolls the pods exactly then.
  local image_id; image_id="$(docker image inspect -f '{{.Id}}' "$IMAGE")"
  # The chart runs $IMAGE, the checkout's tag, not the tag of bastion-chart-common.yaml (F-077); the interface-mismatch scenario's
  # QUADER_BASTION_HELM_EXTRA comes after and sets its own.
  RELEASE_VALUES=(); image_values image "$IMAGE"
  log "helm upgrade --install bastion-$variant ($BNS)"
  # shellcheck disable=SC2086
  helm --kube-context "$CTX" upgrade --install "bastion-$variant" "$REPO_ROOT/deploy/helm/quader-bastion" -n "$BNS" \
    -f "$DEV_DIR/bastion-chart-common.yaml" -f "$values" \
    --set "config.server.base_url=https://bastion-$variant.quader.localhost:$EDGE_PORT" \
    --set-json "networkPolicy.ingress.probeCIDRs=$probe" \
    --set-string "podAnnotations.quader-dev/image-id=$image_id" "${RELEASE_VALUES[@]}" \
    --force-conflicts --wait --timeout 5m ${QUADER_BASTION_HELM_EXTRA:-}
  bn get pods -l "app.kubernetes.io/instance=bastion-$variant" -o wide
}

case "${1:-}" in
  infra)
    case "${2:-}" in
      "") infra ;;
      oci) infra_oci ;;
      *) die "infra: nothing or oci, not '${2:-}'" ;;
    esac ;;
  install) install "${2:-}" ;;
  status) helm --kube-context "$CTX" ls -n "$BNS"; bn get pods -o wide ;;
  down)
    for v in volume bucket oci; do helm --kube-context "$CTX" -n "$BNS" uninstall "bastion-$v" >/dev/null 2>&1 || true; done
    kn delete job quader-s3-bastion-bucket --ignore-not-found >/dev/null
    kubectl --context "$CTX" delete namespace "$BNS" --ignore-not-found --wait=true >/dev/null
    log "the e2e Bastions are removed" ;;
  *) die "usage: bastion.sh infra [oci] | install <volume|bucket|oci> | status | down" ;;
esac
