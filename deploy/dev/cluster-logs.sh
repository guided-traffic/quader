#!/usr/bin/env bash
# Collects the pod logs of a failed e2e run into a directory, whole and compressed, for the artifact `e2e-cluster-state-<set>`
# (the step "Cluster state (failure only)" of .github/workflows). It runs after the tests, on the cluster that is still up.
#
#   cluster-logs.sh <dir>
#
# What it keeps, per pod of the namespace quader (every quader pod, the Valkey, PostgreSQL and test pods included), of every other
# namespace that holds a pod of either chart at the end (a Bastion's, quader-bastion, when the set's teardown did not remove it or
# QUADER_E2E_BASTION_KEEP=1 kept it; a second installation such as quader-restore) and of ingress-controller (the edge, HAProxy
# Ingress). A Bastion the set removed at its end is no pod here: its logs are in the evidence's kept-pod-logs/ (e2e-evidence.sh).
# Per pod:
#   log-<ns>-<pod>.txt.gz                      every container, from the start of the container's current log, no tail limit,
#                                              every line timestamped by the kubelet (--timestamps), so a failure window in the
#                                              test output can be cut out of every log by its time
#   log-<ns>-<pod>.<container>.previous.txt.gz the previous instance of a container that restarted (--previous)
# The whole window is there because the cluster is created for the job and the kubelet keeps up to containerLogMaxSize per
# container before it rotates (deploy/dev/kind.yaml raises it from 10Mi); `kubectl logs` reads only the current file. The edge
# writes one access line per request with the termination state, which names the side that ended the connection
# (deploy/dev/haproxy-values.yaml). Every log is compressed: the artifact is bounded by the size of the logs, which compress
# about tenfold, and is kept 14 days like the rest of the cluster state.
#
# Environment: QUADER_DEV_CLUSTER (default the checkout's cluster, dev-tag.sh --cluster), as in lib.sh; the script needs only kubectl,
# gzip and git.
set -u
dir="${1:?usage: cluster-logs.sh <dir>}"
ctx="kind-${QUADER_DEV_CLUSTER:-$("$(dirname "${BASH_SOURCE[0]}")/dev-tag.sh" --cluster)}"
mkdir -p "$dir"
# the namespaces of the products' pods, found by the charts' name label, beside the two that are always read
product_ns="$(kubectl --context "$ctx" get pods -A -l 'app.kubernetes.io/name in (quader,quader-bastion)' \
  -o jsonpath='{range .items[*]}{.metadata.namespace}{"\n"}{end}' 2>/dev/null | sort -u | grep -vx -e quader -e ingress-controller)"
# shellcheck disable=SC2086 # the namespaces are DNS labels, one word each
for ns in quader $product_ns ingress-controller; do
  for p in $(kubectl --context "$ctx" -n "$ns" get pods -o name 2>/dev/null); do
    name="${p#pod/}"
    kubectl --context "$ctx" -n "$ns" logs "$p" --all-containers --timestamps 2>&1 | gzip -c > "$dir/log-$ns-$name.txt.gz" || true
    # The previous instance, per container that restarted: --previous with --all-containers stops at the first container
    # that has none.
    for c in $(kubectl --context "$ctx" -n "$ns" get "$p" -o jsonpath='{range .status.containerStatuses[?(@.restartCount>0)]}{.name}{"\n"}{end}' 2>/dev/null); do
      kubectl --context "$ctx" -n "$ns" logs "$p" -c "$c" --timestamps --previous 2>&1 | gzip -c > "$dir/log-$ns-$name.$c.previous.txt.gz" || true
    done
  done
done
