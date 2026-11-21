#!/usr/bin/env bash
# One leg of the platform matrix of R-HA-049: the chart against the API server of one Kubernetes version, installed with one
# major version of Helm.
#
#   k8s-matrix.sh <kind node image> <helm major>      for example: k8s-matrix.sh "$K8S_MIN_KIND_NODE_IMAGE" 3
#
# It creates a one-node kind cluster of its own (quader-k8s-<minor>-helm<major>, or $QUADER_K8S_MATRIX_CLUSTER; removed on exit),
# checks that the API server is the minor version the node image names and that the helm on PATH is the major version asked
# for, and lints the chart. It asks the API server to admit the chart's path without cert-manager (tls.existingSecrets, a Secret
# per kind) while the cluster has no CRD at all, then applies the CRDs of cert-manager and has it admit every set of values
# below, which take the standard path (tls.certManager.issuerRef, a Certificate per kind; Q-HA-347), each with
# `helm install --dry-run=server`. Then it installs the chart for real without waiting (no database, no Valkey, no cert-manager
# controller and no image are there: the pods never start), reads back the fields that R-HA-049 names as the reason the chart
# needs Kubernetes 1.31 or later and the Certificate of every kind, upgrades it to another profile and removes it. The pods are
# not started, so this is the install half of the acceptance; the failure suite of R-HA-052 runs on the dev profile only.
#
# Helm 3 and Helm 4 are installed by the workflow (HELM_VERSION, HELM3_VERSION in tools/versions.env); this script uses the
# helm on PATH and fails when its major version is not the one asked for.
set -euo pipefail
# shellcheck source=deploy/dev/lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

IMAGE="${1:?usage: k8s-matrix.sh <kind node image> <helm major>}"
HELM_MAJOR="${2:?usage: k8s-matrix.sh <kind node image> <helm major>}"
for t in kind kubectl helm; do command -v "$t" >/dev/null 2>&1 || die "$t is required"; done

tag="${IMAGE#*:}"; tag="${tag%%@*}"                         # v1.34.11
want_minor="$(printf '%s' "$tag" | cut -d. -f2)"           # 34
[[ "$want_minor" =~ ^[0-9]+$ ]] || die "the node image '$IMAGE' has no Kubernetes version in its tag"
have_helm="$(helm version --template '{{.Version}}')"
[ "${have_helm#v}" != "$have_helm" ] && [ "$(printf '%s' "${have_helm#v}" | cut -d. -f1)" = "$HELM_MAJOR" ] \
  || die "helm $have_helm is on PATH, this leg is for Helm $HELM_MAJOR"

CLUSTER="${QUADER_K8S_MATRIX_CLUSTER:-quader-k8s-${want_minor}-helm${HELM_MAJOR}}"
CTX="kind-${CLUSTER}"
RELEASE=quader
NAMESPACE=quader
CHART="$REPO_ROOT/deploy/helm/quader"
OPERATOR="$REPO_ROOT/test/chart/testdata/operator-minimum.yaml"
OUT="$REPO_ROOT/.out/k8s-matrix"
mkdir -p "$OUT"

cleanup() { kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

log "Kubernetes $tag, helm $have_helm"
kind create cluster --name "$CLUSTER" --image "$IMAGE" --wait 180s
kc() { kubectl --context "$CTX" "$@"; }
hm() { helm --kube-context "$CTX" "$@"; }

got_minor="$(kc version -o json | sed -n 's/.*"minor": "\([0-9]*\).*/\1/p' | tail -n 1)"
[ "$got_minor" = "$want_minor" ] || die "the API server is Kubernetes 1.$got_minor, the node image names 1.$want_minor"
kc create namespace "$NAMESPACE"

log "helm lint"
helm lint "$CHART" -f "$CHART/ci/required-only.yaml"

# The path without cert-manager (Q-HA-347): ci/ha-ingress.yaml names a Secret per kind in tls.existingSecrets, so the chart renders
# no Certificate and the API server of a cluster without cert-manager's CRDs admits it.
log "Secret path: helm install --dry-run=server -f ci/ha-ingress.yaml, no CRD on the cluster"
hm install "$RELEASE" "$CHART" -n "$NAMESPACE" -f "$CHART/ci/ha-ingress.yaml" --dry-run=server >"$OUT/dry-run-$CLUSTER-secrets.yaml"

# The standard path renders a cert-manager Certificate per kind. The API server needs only the CRDs to admit and store them: no
# webhook of cert-manager is registered, so the CRD's schema is what checks them, and no controller issues them. The CRDs are
# those of the cert-manager chart the dev profile installs (DEV_CERT_MANAGER_CHART_VERSION in tools/versions.env).
log "the CRDs of cert-manager $DEV_CERT_MANAGER_CHART_VERSION"
cm_chart="$(chart_tgz https://charts.jetstack.io cert-manager "$DEV_CERT_MANAGER_CHART_VERSION")"
declare -a cm_crds=()
while IFS= read -r f; do cm_crds+=(--show-only "$f"); done < <(tar -tzf "$cm_chart" | sed -n 's#^cert-manager/\(templates/crd-[^/]*\.yaml\)$#\1#p')
[ "${#cm_crds[@]}" -gt 0 ] || die "the cert-manager chart $DEV_CERT_MANAGER_CHART_VERSION has no templates/crd-*.yaml"
helm template cert-manager "$cm_chart" --set crds.enabled=true "${cm_crds[@]}" >"$OUT/cert-manager-crds-$CLUSTER.yaml"
kc apply --server-side -f "$OUT/cert-manager-crds-$CLUSTER.yaml"
kc wait --for=condition=Established --timeout=60s -f "$OUT/cert-manager-crds-$CLUSTER.yaml"

# Each entry is the arguments of one install: value files and --set pairs. Each takes the standard path; the only kind they
# render that a bare kind cluster does not know is cert-manager's Certificate (no Gateway API, no Prometheus Operator).
declare -a SETS=(
  "-f $CHART/ci/required-only.yaml"
  "-f $CHART/ci/roles-pools.yaml"
  "-f $CHART/profiles/small.yaml -f $OPERATOR"
  "-f $CHART/profiles/medium.yaml -f $OPERATOR"
  "-f $CHART/profiles/large.yaml -f $OPERATOR"
  "-f $CHART/ci/required-only.yaml --set backup.enabled=true --set config.backup.target.bucket=quader-backups"
)
i=0
for args in "${SETS[@]}"; do
  i=$((i + 1))
  log "set $i: helm install --dry-run=server ${args//$CHART\//}"
  # shellcheck disable=SC2086 # the entries are word lists on purpose
  hm install "$RELEASE" "$CHART" -n "$NAMESPACE" $args --dry-run=server >"$OUT/dry-run-$CLUSTER-$i.yaml"
done

# The install is real, the pods are not: with the migration hook off Helm returns when the API server has accepted the objects.
log "helm install (medium profile, backup on), not waiting"
hm install "$RELEASE" "$CHART" -n "$NAMESPACE" -f "$CHART/profiles/medium.yaml" -f "$OPERATOR" \
  --set migrations.mode=disabled --set backup.enabled=true --set config.backup.target.bucket=quader-backups

# expect <what> <kubectl jsonpath> <wanted> <kubectl get args...>: the API server stored the field the chart rendered.
expect() {
  local what="$1" path="$2" wanted="$3"; shift 3
  local got; got="$(kc -n "$NAMESPACE" get "$@" -o "jsonpath=$path")"
  case " $got " in
    *" $wanted "*) log "ok: $what = $got" ;;
    *) die "$what is '$got' on this Kubernetes, want '$wanted'" ;;
  esac
}
expect "PodDisruptionBudget unhealthyPodEvictionPolicy" '{.spec.unhealthyPodEvictionPolicy}' AlwaysAllow pdb "$RELEASE-packages"
expect "topologySpreadConstraints nodeTaintsPolicy" '{.spec.template.spec.topologySpreadConstraints[*].nodeTaintsPolicy}' Honor deployment "$RELEASE-packages"
expect "CronJob timeZone" '{.spec.timeZone}' Etc/UTC cronjob "$RELEASE-backup"
expect "HorizontalPodAutoscaler minReplicas of the medium profile" '{.spec.minReplicas}' 4 hpa "$RELEASE-packages"
expect "autoscaling/v2 metric" '{.spec.metrics[*].type}' Resource hpa "$RELEASE-packages"
for kind in api packages worker vuln-scanner-trivy; do
  expect "the issuer of the Certificate of $kind" '{.spec.issuerRef.name}' cluster-ca certificate "$RELEASE-tls-$kind"
done

log "helm upgrade to the small profile"
hm upgrade "$RELEASE" "$CHART" -n "$NAMESPACE" -f "$CHART/profiles/small.yaml" -f "$OPERATOR" \
  --set migrations.mode=disabled --set backup.enabled=true --set config.backup.target.bucket=quader-backups
if kc -n "$NAMESPACE" get hpa "$RELEASE-packages" >/dev/null 2>&1; then die "the autoscaler of the medium profile is still there after the upgrade to small"; fi
expect "replicas of the small profile" '{.spec.replicas}' 3 deployment "$RELEASE-packages"

log "helm uninstall"
hm uninstall "$RELEASE" -n "$NAMESPACE"
left="$(kc -n "$NAMESPACE" get deployments,cronjobs,pdb,hpa,certificates.cert-manager.io -o name | wc -l | tr -d ' ')"
[ "$left" = "0" ] || die "$left objects of the release are left after helm uninstall"
log "Kubernetes $tag with Helm $have_helm: the chart installs, upgrades and uninstalls"
