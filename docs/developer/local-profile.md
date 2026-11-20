# Local development profile

The profile brings up an HA-shaped installation on a laptop with one command: a kind cluster with
one control-plane and three worker nodes in three zones, PostgreSQL with a standby, Valkey with a
replica and three Sentinels, an S3 store, an Ingress controller, the owner's stack of operators that
generate the Secrets and the certificates, and the Quader chart with every role: `api` 2 replicas, `packages` 3, `worker` 2 and a worker pool `vuln` of 2 ([Q-HA-87](../spec/ha-and-helm.md#q-ha-87--the-dev-profiles-sizing-of-the-roles-and-its-postgresql-connection-budget)).
**Every connection is TLS**: client to the edge, edge to Quader, Quader to PostgreSQL, Valkey, the Sentinels and
S3. It is the target of the failover and drain tests (E0.13, E0.14), of the certificate rotation drill (E0.15) and
the base of the end-to-end tests. The requirements are [R-HA-051](../spec/ha-and-helm.md),
[R-HA-063](../spec/ha-and-helm.md) and [R-ARCH-035](../spec/architecture.md); the chart it installs is described in
[helm-chart.md](helm-chart.md). Everything lives in [deploy/dev](../../deploy/dev).

```mermaid
flowchart LR
  host["host client / k6<br/>(dev CA)"] -- "https :28080 (NodePort 30443)" --> hap["HAProxy Ingress<br/>2 pods, control-plane node"]
  hap -- "HTTPS, each backend verified<br/>under its kind's name" --> web["quader api / packages / worker / scanner"]
  web -- "verify-full" --> pg[("CloudNativePG<br/>quader-db")]
  web -- "TLS 36379 / 16379" --> vk[("Valkey operator:<br/>2 Valkey + 3 Sentinels")]
  web -- "https, ca_file" --> s3[("SeaweedFS")]
  cm["cert-manager<br/>Issuer quader-dev-ca"] -. "quader-tls-&lt;kind&gt; (the chart's),<br/>quader-edge-tls, quader-s3-tls, quader-valkey-tls" .-> web
  iso["internal-secrets-<br/>operator"] -. "passwords, key ring" .-> web
```

## What runs

| Component | Where | What | Pinned by (`tools/versions.env`) |
|---|---|---|---|
| kind cluster `quader` | Docker | `quader-control-plane` and `quader-worker`, `quader-worker2`, `quader-worker3` in zones `quader-zone-a`, `-b`, `-c`; pod CIDR `10.244.0.0/16` | `KIND_VERSION`, `DEV_KIND_NODE_IMAGE` |
| HAProxy Ingress | namespace `ingress-controller` | Ingress controller, class `haproxy` (default), two replicas pinned to the control-plane node (never drained by the tests, and the source of every proxied request is that node's pod range, which `config.server.trusted_proxies` names); NodePort 30443 (HTTPS) mapped to `localhost:8080` (`QUADER_DEV_HTTP_PORT`); the owner's production chart pin, 0.16.3 since 2026-10-30 (controller image v0.16.3, which runs HAProxy 2.8.31) | `DEV_HAPROXY_INGRESS_CHART_VERSION`, `DEV_HAPROXY_INGRESS_IMAGE` |
| cert-manager | namespace `cert-manager` | issues every certificate from one self-signed root (`Issuer` `quader-dev-ca`, [pki.yaml](../../deploy/dev/pki.yaml)). trust-manager is not installed: no consumer needs a distributed bundle, each reads the `ca.crt` of its own Secret | `DEV_CERT_MANAGER_CHART_VERSION`, `DEV_CERT_MANAGER_*_IMAGE` |
| internal-secrets-operator | namespace `internal-secrets-operator-system` | generates the Secrets of [secrets.yaml](../../deploy/dev/secrets.yaml) from annotations; its memory limit is 512 MiB, because it caches every Secret of the cluster and its chart's 128 MiB OOM-killed it on a long-lived cluster (F-127), and every helm release of the profile keeps two revisions (`HELM_MAX_HISTORY`, [lib.sh](../../deploy/dev/lib.sh)) | `DEV_ISO_CHART_VERSION`, `DEV_ISO_IMAGE` |
| Valkey operator | namespace `valkey-operator-system` | owns the `Valkey` resource and its StatefulSets | `DEV_VALKEY_OPERATOR_CHART_VERSION`, `DEV_VALKEY_OPERATOR_IMAGE` |
| CloudNativePG operator | namespace `cnpg-system` | operator only | `CNPG_CHART_VERSION`, `DEV_CNPG_OPERATOR_IMAGE` |
| PostgreSQL 18 | namespace `quader` | CloudNativePG `Cluster` `quader-db`: 2 instances (primary and standby), synchronous replication with `dataDurability: preferred`, database and owner `quader`, TLS, `max_connections` 400 (the chart's budget of the roles is 303, 323 in the scatter profile; R-HA-016, Q-HA-87); `log_min_duration_statement` 50 ms for the role `quader` (set by `ALTER ROLE` in the bootstrap's `postInitApplicationSQL`: the role survives the upgrade set's `DROP DATABASE`, and the operator's own statements run as `postgres`, so they stay out; a slow statement or commit shows in the primary's log, which the cluster state keeps; the bootstrap runs once, so a cluster created before this setting has none: recreate it with `make dev-down && make dev-up`, or run `kubectl -n quader exec <primary> -c postgres -- psql -U postgres -c "ALTER ROLE quader SET log_min_duration_statement = '50ms'"` once), `log_parameter_max_length` 0 (no bound value in that log) and `track_wal_io_timing` on (the WAL write and fsync times in `pg_stat_io`), both cluster-wide for the readings of the clients-first test ([Q-REPO-236](../spec/repositories-and-proxy.md#q-repo-236--the-next-reading-of-clients-first-records-the-databases-waits-the-standbys-lag-the-runners-pressure-the-tests-events-and-the-slot-wait)) | `DEV_CNPG_POSTGRES_IMAGE` |
| Valkey 9.1 | namespace `quader` | `Valkey` resource `quader-valkey` ([valkey.yaml](../../deploy/dev/valkey.yaml)): StatefulSet `quader-valkey` (2 pods) and `quader-valkey-sentinel` (3 pods), TLS only, `emptyDir`, PodDisruptionBudgets, one pod of each tier per zone. The Alpine image has every tool the operator's scripts need (checked against `RequiredImageTools` of the operator, 1.14.12) | `DEV_VALKEY_IMAGE` |
| SeaweedFS | namespace `quader` | Deployment `quader-s3` (one `weed server` on the 20 Gi PersistentVolumeClaim `quader-s3-data` (a local-path volume, so the objects survive a scale to 0, [Q-STOR-147](../spec/storage.md#q-stor-147--the-dev-profiles-object-store-keeps-its-objects-over-an-outage)), pinned to the control-plane node like the edge, because the volume is bound to a node and no test drains this one; memory limit 4Gi: two 500 MB uploads at once were OOM-killed at 1Gi in E1.13), HTTPS on `quader-s3:9000` with `quader-s3-tls`, buckets `quader-blobs` (versioning on), `quader-pg-archive`, `quader-backups` | `DEV_SEAWEEDFS_IMAGE`, `DEV_AWSCLI_IMAGE` |
| Keycloak (test IdP) | namespace `quader` | Deployment `quader-keycloak` ([keycloak.yaml](../../deploy/dev/keycloak.yaml)), `start-dev` with a realm import, HTTPS only on `quader-keycloak:8443` with `quader-keycloak-tls`; realm `quader` with the users `alice` and `bob`, the groups `quader-team-web` and `quader-platform-admins`, the client `quader` (Q-AUTH-35, [test IdP](#the-test-idp)) | `DEV_KEYCLOAK_IMAGE` |
| Quader | namespace `quader` | Helm release `quader` with [values-dev.yaml](../../deploy/dev/values-dev.yaml): `api` 2 and `packages` 3 with 1 GiB limits, `worker` 2 (grace period 60 s, so a drain test does not wait 300 s) and the pool `vuln` 2 (queue `vuln`, the settings of `worker`), migration Job, `health.shutdown_drain_delay` 15 s (the edge took longer than the default 5 s to drop a draining pod's endpoint on the CI runners), caches of 64, 32 and 64 MiB so a pod of the serving roles keeps 858 memory slots instead of 90 (Q-ARCH-243), the chart's NetworkPolicies, `tls.enabled` with the chart's pairs per kind from the Issuer `quader-dev-ca` and `reloadInterval: 2s`, the endpoint `customers` on `:8443`; `up.sh` adds the Docker `kind` network ranges as `networkPolicy.probeCIDRs` | image `guidedtraffic/quader:dev` (the default of `up.sh`, loaded into kind from the local Docker; `make image` builds it) |

The namespace `quader` enforces the restricted Pod Security Standard (`enforce`, `audit` and `warn`),
and every dependency manifest satisfies it, the operators' generated pods included. Images are pinned by digest; the
manifests carry `${DEV_…_IMAGE}` placeholders that `lib.sh` (`render`) substitutes from `tools/versions.env`, and the
chart images are passed with `--set`, so the pins exist in one place only.

**Architecture note (arm64 hosts).** The Valkey operator and the internal-secrets-operator images are published for
`linux/amd64` only; the pins are the amd64 manifests. On an arm64 machine (Apple silicon) the Docker VM needs qemu
user-mode emulation for amd64 once: `docker run --privileged --rm "$DEV_BINFMT_IMAGE" --install amd64` (pinned in
`tools/versions.env`; the registration lasts until the Docker VM restarts). `up.sh` checks and prints the command.
CI runs on amd64 and needs nothing.

## Targets

All targets call a script in [deploy/dev](../../deploy/dev). They address the cluster through the
context `kind-quader` explicitly, so other kind clusters or whatever the kubeconfig currently
selects are never touched.

| Target | Script | What it does |
|---|---|---|
| `make dev-up` | `up.sh` | creates the cluster and installs everything; idempotent (a second run takes about 40 s). In the default mode the ID of the loaded Quader image is a pod annotation (`quader-dev/image-id`; the published mode sets none, [below](#environment)), so a newly built image rolls the `api`, `packages`, `worker` and `scanner` pods (the pool inherits the annotation of `worker`) and an unchanged one rolls nothing. `QUADER_IMAGE` names the image to load; the chart's `image.repository` stays `guidedtraffic/quader` unless `QUADER_HELM_EXTRA` sets it (an image loaded as `quader-x:dev` is `--set image.repository=library/quader-x`) |
| `make dev-down` | `down.sh` | deletes the kind cluster `quader` and `.dev/` |
| `make dev-failover` | `failover.sh [instance]` | PostgreSQL switchover to a ready standby on a schedulable node, waits until the old primary is a standby again and the new one serves `quader-db-rw`. Before it asks it waits up to 180 s for the CloudNativePG operator (a ready pod holds its lease) and a settled cluster (no switchover in flight, every instance ready), and fails naming the precondition it did not see ([Q-HA-126](../spec/ha-and-helm.md#q-ha-126--the-node-clearance-moves-the-primaries-before-the-cordon-and-failoversh-waits-for-what-a-switchover-needs)) |
| `make dev-valkey-failover` | `valkey-failover.sh` | `SENTINEL FAILOVER quader-valkey`, waits until another pod is primary |
| `make dev-valkey-kill` | `valkey-kill.sh` | force-deletes the Valkey primary pod, waits for a primary and a connected replica |
| `make dev-s3-outage DURATION=60s` | `s3-outage.sh 60s` | `s3-stop.sh` (the Deployment `quader-s3` to 0 replicas; the objects stay on the volume), the duration, `s3-start.sh` (back to 1, waits for the pod) |
| `make dev-valkey-outage DURATION=60s` | `valkey-outage.sh 60s` | `valkey-stop.sh`, the duration, `valkey-start.sh` (below) |
| `make dev-valkey-stop`, `make dev-valkey-start` | `valkey-stop.sh`, `valkey-start.sh` | the two halves of the outage: stop the operator, scale Valkey and the Sentinels to 0 and wait for the pods to be gone; scale back, start the operator and wait for a primary. The e2e tests hold the outage for as long as a "Valkey stopped" criterion needs (E1.8, E1.11, E1.14) and start it again in their cleanup. `make dev-valkey-outage` is stop, sleep, start |
| `deploy/dev/valkey-cli.sh <command...>` | `valkey-cli.sh` | runs `valkey-cli` against the current primary (found through the Sentinels, over TLS, the password read inside the pod): `FLUSHALL` for E1.11, `CLIENT PAUSE` for the latency injection of R-ARCH-034 (E1.8) |
| `make dev-pg-kill` | `pg-kill.sh` | force-deletes the PostgreSQL primary pod, waits until the operator reports a healthy cluster |
| `make dev-pg-stop DURATION=30s` | `pg-stop.sh 30s` | fences every instance of `quader-db` (`cnpg.io/fencedInstances`, PostgreSQL stops, pods and volumes stay), waits, removes the fence, waits for a healthy cluster |
| `make dev-drain` | `drain.sh` | the E0.14 test: drains a node with an `api`, a `packages` and a `worker` pod under the k6 load and cursor-persisting jobs; `drain.sh <node>` is the drain alone, `drain.sh --pick` prints the node; `drain.sh --also valkey\|postgres\|pooler <node>` also drains that dependency's pods on the node in a run of its own and waits until it is whole again (drain variants B, C and D of R-HA-059, [e2e.md](e2e.md#drain-variants-b-to-e)) |
| `make e2e SET=push\|failover\|access-long\|full\|drain\|npm\|npm-ha\|npm-ha-crash\|nuget\|nuget-ha\|<regexp>` | `e2e.sh` | the end-to-end suite, see [e2e.md](e2e.md); `npm-ha` drains a node and fails PostgreSQL and Valkey over under the npm load, `npm-ha-crash` force-deletes pods under it ([E2.11](e2e.md#drain-and-failover-under-npm-load)); `drain-load` and `drain-load-pooler` run drain variants B to E under the full load ([E5.9](e2e.md#the-drain-under-the-full-load)); `nuget-clients`, `nuget`, `nuget-closing`, `nuget-ha` start NuGet client pods (the .NET SDK and nuget.exe on Mono) and, but for the first, the counting NuGet test upstream ([the NuGet clients](e2e.md#the-nuget-clients)) |
| `make chart-lint`, `make chart-test` | `test/chart/lint.sh`, `test/chart/test.sh` | see [helm-chart.md](helm-chart.md#value-sets-and-tests) |

The failover scripts show that the failover happens and how long it takes. What they cost the clients
is measured by [the end-to-end suite](e2e.md), which runs them under load.

### Environment

| Variable | Default | Effect |
|---|---|---|
| `QUADER_SKIP_RELEASE=1` | unset | `up.sh` installs the dependency stack only, not the Quader release. Use it while the image does not serve yet, or to work on the dependencies |
| `QUADER_IMAGE` | `guidedtraffic/quader:dev` | the image `up.sh` loads into the cluster with `kind load docker-image`; build it with `make image` (its default `IMAGE`) first. `VERSION` defaults to `dev` (or the number of a `v0.*` tag on `HEAD`), the one non-release version the binary accepts ([e2e.md](e2e.md#findings-of-the-suite)) |
| `QUADER_DEV_HTTP_PORT` | `8080` | host port of the edge (HTTPS). Set it when 8080 is taken (`up.sh` checks and says so); `config.server.base_url` and `endpoints.customers.baseUrl` follow. Fixed when the cluster is created |
| `QUADER_PROFILE=scatter`, `bastion`, `edge` or `alerts` | `default` | the e2e profiles, see below. `edge` installs Envoy Gateway and `alerts` a Prometheus; both stay in the cluster after a later run with another profile |
| `QUADER_PITR=1` | unset | also installs the Barman Cloud plugin of CloudNativePG (`DEV_BARMAN_PLUGIN_*` in [versions.env](../../tools/versions.env)) and the `ObjectStore` [pitr.yaml](../../deploy/dev/pitr.yaml), and adds the plugin to the Cluster `quader-db`: WAL archive and base backups to the bucket `quader-pg-archive` over TLS (R-HA-023, Q-HA-210). Sticky for a cluster that exists; the PostgreSQL pods roll once. Run by the set `backup-pitr` ([e2e.md](e2e.md#the-point-in-time-restore-drill)) |
| `QUADER_POOLER=1` | unset | also installs the two transaction-mode poolers ([pooler.yaml](../../deploy/dev/pooler.yaml)) and layers [values-pooler.yaml](../../deploy/dev/values-pooler.yaml). The profile carries what the drain of a pooler instance needs (R-HA-059 rule 5, Q-HA-173): two instances per pooler spread over the nodes, a PodDisruptionBudget each, a `preStop` sleep of 90 s and a grace period of 120 s (CloudNativePG's pooler manager turns `SIGTERM` into `SIGINT` and exits within a second, so the sleep, with the pod out of its Service, is what lets Quader's connections move), `server_login_retry: "1"` on both poolers (a PostgreSQL switchover fails PgBouncer's server login twice, and the default of 15 s per failure took every replica out of its Service, Q-HA-302), a pooled connection lifetime of 60 s + 15 s and `pool.health_check_period: 5s` (the last connection is gone from a draining instance after 80 s), `server_connections: 48` (2 x 20 + 2 x 4), and the pooler Services in the cluster's `certificates.serverAltDNSNames` ([postgres.yaml](../../deploy/dev/postgres.yaml)). `up.sh` sets the role defaults that preflight reads on a pooled connection (`ALTER ROLE quader IN DATABASE quader SET statement_timeout = '30s'`, `lock_timeout = '5s'`, `idle_in_transaction_session_timeout = '60s'`, `search_path = quader`). Run by the set `vuln-pooler` (about 6 minutes) |
| `QUADER_HELM_EXTRA` | unset | extra arguments for the `helm upgrade` of the Quader release |
| `QUADER_DEV_CLUSTER`, `QUADER_DEV_NAMESPACE` | `quader`, `quader` | cluster and namespace names. The certificates name the namespace (`quader.<namespace>.svc`, ...), and the Valkey names in values-dev.yaml are written for `quader`, so change both only with a fresh cluster and matching values |

### The `scatter` profile

`QUADER_PROFILE=scatter deploy/dev/up.sh` (also on a running cluster) switches to the profile of
R-ARCH-032 and E0.11: three `api` and three `packages` replicas ([values-scatter.yaml](../../deploy/dev/values-scatter.yaml)),
and two HAProxy Ingress backend keys on Quader's Ingress, `balance-algorithm: roundrobin` and
`config-backend: "option http-server-close"`. The second one overrides the template's `option http-keep-alive`
for this backend only, so HAProxy keeps no idle connection to a pod and balances every request round robin on its
own. Run `up.sh` without the variable to switch back. [TestE99Scatter](../../test/e2e/e99_scatter_test.go) (E0.11)
switches to the profile, runs the M0 checks, shows sixty sequential requests reaching the three pods twenty times each
and runs the 100 requests/s load; `e2e.sh` returns the cluster to the default profile afterwards.

### The `drain` profile

`QUADER_PROFILE=drain deploy/dev/up.sh` is the `scatter` profile with `storage.gc.grace_period: 0s` ([values-drain.yaml](../../deploy/dev/values-drain.yaml)): a blob that nothing
references is collectable at once, so the full collection run that the drain test of E5.9 starts has blobs to delete and is still waiting for them to settle when its
worker is evicted ([the drain under the full load](e2e.md#the-drain-under-the-full-load), R-STOR-046). The sets `nuget-ha`, `drain-load` and `drain-load-pooler` select it with
`QUADER_E2E_SCATTER_PROFILE=drain`. Run `up.sh` without the variable to switch back.

### The `bastion` profile

`QUADER_PROFILE=bastion deploy/dev/up.sh` layers [values-bastion.yaml](../../deploy/dev/values-bastion.yaml) on the main installation for the
Bastions of R-BAS-049: the pod range `10.244.0.0/16` as `config.bastions.trusted_proxies` (the Bastion's pods call the main endpoint from the range of
whichever node they run on, and the main installation resolves the client address from what they send, R-BAS-017) and the Service addresses the Bastions call, `https://quader-api.quader.svc:8080` for `bastion.main.url` and `https://quader-packages.quader.svc:8080` for `bastion.main.packages_url` (a host the main installation does not serve is answered `421`, also on the interface routes), and the two base URLs
`https://bastion-volume.quader.localhost:<edge port>` and `https://bastion-bucket.quader.localhost:<edge port>` as
`config.server.additional_base_urls` (R-BAS-010; `up.sh` passes them because the edge port follows `QUADER_DEV_HTTP_PORT`). The Bastions themselves are not part of
`up.sh`: `deploy/dev/bastion.sh infra` and `install <volume|bucket>` make the namespace `quader-bastion`, a Valkey of its own, the certificates and the two releases of the chart
`quader-bastion` ([the Bastion tests](e2e.md#the-bastion-tests)); `bastion.sh down` removes them. Without the profile the Bastions are refused with `421 misdirected_request`,
because their host is not one the main installation serves.

### The `edge` profile

`QUADER_PROFILE=edge deploy/dev/up.sh` (R-HA-038, R-HA-039, exit criterion E5.5) installs a Gateway API implementation beside HAProxy Ingress and renders Quader's
HTTPRoute on it next to the Ingress. Envoy Gateway comes from its OCI chart (`DEV_ENVOY_GATEWAY_*` in [versions.env](../../tools/versions.env): the chart
version and the SHA-256 of its archive, which `oci_chart_tgz` of [lib.sh](../../deploy/dev/lib.sh) checks on every use, the controller image and the
proxy image by digest), with the Gateway API CRDs of that chart. [gateway.yaml](../../deploy/dev/gateway.yaml) makes the platform Gateway `shared` in the namespace
`gateway-system` (one HTTPS listener on 443 with the Ingress's certificate `quader-edge-tls`, read across namespaces through a `ReferenceGrant`), the
`ClientTrafficPolicy` that keeps encoded slashes and double slashes unchanged and sets no request-received timeout, and the `BackendTrafficPolicy`
without a request timeout; the proxy Service is `ClusterIP`, because the clients of the tests are pods
([Q-HA-260](../spec/ha-and-helm.md#q-ha-260--the-gateway-api-implementation-of-the-dev-stack-is-envoy-gateway-installed-by-the-profile-edge)).
[values-edge.yaml](../../deploy/dev/values-edge.yaml) enables `gateway`, the BackendTLSPolicies (`tls.backendTLSPolicy`, the CA ConfigMap `quader-tls-ca` that `up.sh`
makes from the dev CA, so Envoy verifies Quader for `quader.<ns>.svc`), HAProxy annotations without a body limit and with one-hour timeouts, a spool of 10Gi for the
`packages` pods and `storage.upload.max_size: 7GiB`. Run `up.sh` without the variable to switch back: the route and the policies go, Envoy Gateway stays.
The set `edge` ([e2e.md](e2e.md#the-edge-and-alert-tests)) uses it.

### The `alerts` profile

`QUADER_PROFILE=alerts deploy/dev/up.sh` (R-HA-045, R-OPS-045, R-OPS-046, exit criterion E5.5) installs kube-prometheus-stack into the namespace `monitoring`
(`DEV_PROMETHEUS_*` in [versions.env](../../tools/versions.env)): the Prometheus operator, one Prometheus that selects every ServiceMonitor and
PrometheusRule of the cluster, and kube-state-metrics; no Alertmanager, Grafana or node exporter
([Q-HA-263](../spec/ha-and-helm.md#q-ha-263--the-dev-stack-gets-a-prometheus-for-the-profile-alerts-kube-prometheus-stack-without-alertmanager-grafana-and-node-exporter-plain-http-on-its-own-interface)).
[values-alerts.yaml](../../deploy/dev/values-alerts.yaml) renders the chart's ServiceMonitor (every 10 s) and PrometheusRule. The Prometheus scrapes the ops
listener over TLS and verifies it against the dev CA for `quader-ops.<ns>.svc`, a name that every kind's pair carries (R-HA-063); the chart's
NetworkPolicy admits the scrape from the namespace `monitoring`. Its interface is plain HTTP inside the cluster: reach it with
`kubectl -n monitoring port-forward svc/prometheus-kube-prometheus-prometheus 9090` (see
[H-797](../security/transport-encryption.md#h-797--the-prometheus-of-the-profile-alerts-serves-its-interface-and-api-in-plain-http)). The set `alerts`
([e2e.md](e2e.md#the-edge-and-alert-tests)) uses it, on the cluster without the poolers.

## How `up.sh` works

1. Checks the tools against the pins and Docker (and, on arm64, amd64 emulation); on a mismatch it prints the exact
   install command (see [Tools](#tools)).
2. `kind create cluster` with [kind.yaml](../../deploy/dev/kind.yaml) and the pinned node image, then waits for
   every node to be Ready. (Pods scheduled while a worker is still NotReady would all land on the
   nodes that are up, and the host spreading of the dependencies would be decided by luck.)
3. Creates the namespace with the restricted labels.
4. In parallel, with logs in `.dev/logs/<name>.log`: `haproxy` (HAProxy Ingress); `pki` (cert-manager, then
   [pki.yaml](../../deploy/dev/pki.yaml), then waits for every Certificate to be Ready); `secrets` (the
   internal-secrets-operator, then [secrets.yaml](../../deploy/dev/secrets.yaml), then waits until the fields are
   filled); `vko` (the Valkey operator); `postgres` (the CloudNativePG operator, `postgres.yaml`, two ready
   instances); `valkey` (waits for `pki`, `secrets` and `vko`, applies the `Valkey` resource and waits for the pods);
   `s3` (waits for `pki` and `secrets`, applies `s3.yaml`, runs the bucket Job); `keycloak` (waits for `pki` and `secrets`,
   applies [keycloak.yaml](../../deploy/dev/keycloak.yaml) and waits up to 10 minutes for the rollout; the startup probe allows
   the same 10 minutes, so a slow start is not restarted inside the wait). A task another one needs leaves a
   marker file in `.dev/logs/`, so Valkey and S3 do not wait for PostgreSQL, the longest task. When a task fails, `up.sh`
   prints what the cluster says about it before it ends ([Troubleshooting](#troubleshooting)).
5. Writes the dev CA to `.dev/pki/ca.crt`, asks the Sentinels over TLS for the primary and prints it, prints the
   PostgreSQL primary.
6. Unless `QUADER_SKIP_RELEASE=1`: `kind load docker-image`, then
   `helm upgrade --install quader deploy/helm/quader -n quader -f deploy/dev/values-dev.yaml --wait`.
   The migration Job runs first as a hook.

Measured on an Apple-silicon laptop with Docker, cold (`make dev-down`, then `make dev-up`, the nodes pull every
image, the Quader image built beforehand): __COLD__ (E0.3 budget: 15 min). A repeat run: about 40 s.

### Secrets

Generated or created in namespace `quader`; nothing in `up.sh` makes a random value:

| Secret | Keys | Made by | Used by |
|---|---|---|---|
| `quader-db-app`, `quader-db-ca` | `username`, `password`; `ca.crt` | CloudNativePG | chart: `secrets.database`, `secrets.databaseCA` |
| `quader-valkey-auth` | `password` (32 characters) | internal-secrets-operator | the Valkey resource (`spec.auth`), and the chart as `secrets.valkey` **and** `secrets.valkeySentinel`, user `default` |
| `quader-valkey-tls` | `tls.crt`, `tls.key`, `ca.crt` | the Valkey operator, through cert-manager (`unifiedCertificate: true`: one Secret for both tiers) | Valkey and Sentinels; chart: `secrets.valkeyCA` (`ca.crt`) |
| `quader-s3` | `access-key-id` (`quader-dev`), `secret-access-key` (40 characters) | internal-secrets-operator | SeaweedFS and the bucket Job; chart: `secrets.s3` |
| `quader-encryption-keys` | `dev.key`: 32 random bytes, **raw** (`iso.gtrfc.com/type: bytes`) | internal-secrets-operator | chart: `secrets.encryption`; the file name without `.key`, `dev`, is the key ID. The key ring accepts a 32-byte file as it is and base64 text of 32 bytes |
| `quader-break-glass` | `password` (32 characters, plaintext: at least 20 are required) | internal-secrets-operator, **rotated every 24 h** (`iso.gtrfc.com/rotate: 24h`) | chart: `secrets.breakGlass` (mounted into `api` pods, renders `auth.break_glass.password_file`); the e2e tests read it for E1.1 and R-AUTH-042 |
| `quader-dev-ca` | `tls.crt`, `tls.key`, `ca.crt` | cert-manager (self-signed root, ten years) | the Issuer `quader-dev-ca` |
| `quader-tls-api`, `quader-tls-packages`, `quader-tls-worker`, `quader-tls-scanner` | `tls.crt`, `tls.key`, `ca.crt` | the chart's Certificates (`tls.certManager.issuerRef: quader-dev-ca`), cert-manager | the pair of each kind of pod, in that kind's pods only; each names `quader-<kind>.<ns>.svc` (the name HAProxy Ingress verifies per Service for `api` and `packages`, and the links check), `quader-ops.<ns>.svc` and, as the profile's extra names, `localhost` and `127.0.0.1` for the tests that reach one pod through a port-forward; the worker's and the scanner's allow client authentication. `quader-customers-tls` names `quader-ep-customers` and `quader-api-ep-customers` |
| `quader-edge-tls` | `tls.crt`, `tls.key` | cert-manager | the edge's certificate for `localhost` |
| `quader-s3-tls` | `tls.crt`, `tls.key`, `ca.crt` | cert-manager | SeaweedFS; Quader mounts its `ca.crt` through `secrets.s3CA`, which renders `storage.s3.ca_file` |
| `quader-customers-tls`, `quader-customers-wildcard-tls`, `quader-customers-edge-tls` | `tls.crt`, `tls.key`, `ca.crt` | cert-manager | the endpoint `customers` (below) |

**Rotation of `quader-break-glass`.** It is the one rotating Secret of the profile. The operator (1.6.14) takes `rotate` as a
Go duration plus a `d` suffix, overwrites the field when the interval has passed, and refuses intervals below its
`rotation.minInterval` (default `5m`; the profile installs it with the chart defaults) with a Warning Event on the Secret
([README of the operator](https://github.com/guided-traffic/internal-secrets-operator#automatic-rotation)). Each rotation
reaches the pods through the kubelet (a directory mount, no `subPath`); Quader notices the new file within
`auth.break_glass.check_interval` (10 s), then answers the local login with 503 `break_glass_rotating` for the 2-minute
`rotation_hold` and ends the break-glass sessions (Q-AUTH-14). That is acceptable in the dev profile, and it is why the interval is
24 h and not minutes. A rotation that falls into an e2e run (E1.1, E1.11) does not fail it: the local sign-ins of the suite
wait for the `Retry-After` of the 503, at most 3 minutes ([Q-AUTH-194](../spec/identity-and-authentication.md#q-auth-194--the-e2e-sign-ins-wait-out-the-break-glass-rotation-hold),
[e2e.md](e2e.md#the-break-glass-rotation-hold)); a hand-made client has to do the same. To trigger a
rotation on demand, set a short interval, wait until `generated-at` changes, and put it back; the hold starts when the pods
notice the new file (kubelet sync and `check_interval`, about a minute), not at the rotation:

```shell
kubectl --context kind-quader -n quader annotate secret quader-break-glass --overwrite iso.gtrfc.com/rotate=5m   # rotates when 5m passed since generated-at
kubectl --context kind-quader -n quader annotate secret quader-break-glass --overwrite iso.gtrfc.com/rotate=24h
```

OIDC sign-in is not affected by a break-glass rotation. Do not set `rotate` on any other Secret of the profile.

Never set `iso.gtrfc.com/rotate` on `quader-encryption-keys` (a rotation would destroy the encrypted data) or on
`quader-valkey-auth` (a changed password reaches no running Valkey pod, the operator's documentation). The generator
writes a field only when it is empty.

### Trusting the dev CA

The edge serves `https://localhost:8080` (or your `QUADER_DEV_HTTP_PORT`) with a certificate of the dev CA. `up.sh`
writes the CA to `.dev/pki/ca.crt` (ignored by Git):

```shell
curl --cacert .dev/pki/ca.crt https://localhost:8080/api/v1/system/ping
# to trust it system-wide (macOS), as an explicit choice of yours, not something the scripts do:
#   sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain .dev/pki/ca.crt
kubectl --context kind-quader -n quader get secret quader-dev-ca -o jsonpath='{.data.ca\.crt}' | base64 -d   # the same CA
```

A new cluster has a new CA; delete the old entry from your trust store when you remove the cluster.

### The second endpoint

values-dev.yaml carries `endpoints.customers` ([endpoints.md](endpoints.md)): the listener `:8443`, the host
`packages.quader.localhost` and the wildcard `*.packages.quader.localhost`, the surface `[packages, files, api]` (the raw
files and the management API are there for the exposure criteria of E1.14, Q-EP-54; `ui` is not allowed on a further
endpoint), service-account tokens bound to it and exposure by rules (the chart's defaults for a further endpoint), its own certificate
pair and a wildcard pair (`tls.existingSecret`, `tls.extraSecrets`, chosen by SNI), and its own Ingress on the same
HAProxy Ingress. HAProxy Ingress routes by `Host`, so a request with the default endpoint's host never reaches the
endpoint through its Ingress; the `421` is tested on the endpoint's Service. `*.localhost` resolves to the loopback
address on macOS and on systemd-resolved; otherwise add `127.0.0.1 packages.quader.localhost` to `/etc/hosts`.
The chart gives the default endpoint a host check too (its `hosts` are the host of `base_url`), so a request to the
default endpoint with another `Host` answers `421`.

### The backup target

values-dev.yaml turns the backup on (`backup.enabled: true`; the chart's default is off) and so renders the backup CronJob (R-HA-060,
[helm-chart.md](helm-chart.md#the-backup-and-restore-jobs)) with the schedule
`0 */6 * * *` and `config.backup.expected_interval: 6h`, so `QuaderBackupStale` and the overdue mark follow the schedule. The target is
the bucket `quader-backups` of the profile's SeaweedFS (created beside `quader-blobs` by the Job of [s3.yaml](../../deploy/dev/s3.yaml)),
a bucket of its own on the same endpoint, so the rule that a target must not be the blob store's bucket (R-BAK-002) holds without
`allow_same_bucket`. The link is HTTPS like every link of the profile: `secrets.backupTargetCA` is the SeaweedFS certificate Secret
`quader-s3-tls`, which also serves `secrets.s3CA`, and the keys are the blob store's (`secrets.backupTarget` names `quader-s3`), because the
profile has one S3 login; a real target has credentials of its own that no replica holds. The Jobs mount these in the backup and restore
pods only. A restore on the profile is the sequence of R-HA-060 with `restore.enabled`, `restore.backupId` and every replica count at zero;
the `dev-backup` and `dev-restore` targets and their drill belong to the end-to-end suite and are not part of the chart.

## Valkey and Sentinel

[valkey.yaml](../../deploy/dev/valkey.yaml) is a `Valkey` resource of the Valkey operator (`vko.gtrfc.com/v1`); the
operator writes the StatefulSets, Services, ConfigMaps, the certificate request and the PodDisruptionBudgets
(Q-HA-15 option (d) is now "the operator"). Facts worth knowing, verified on the running profile:

- TLS only: `port 0`, `tls-port 16379` (Valkey) and `36379` (Sentinel), `tls-replication yes`, `tls-auth-clients
  optional`. A plaintext connection is refused. One certificate, `quader-valkey-tls`, for both tiers.
- Names the chart uses: Sentinels `quader-valkey-sentinel-{0,1,2}.quader-valkey-sentinel-headless.quader.svc.cluster.local:36379`,
  primary name `quader-valkey` (the resource name), Sentinel answers `quader-valkey-<n>.quader-valkey-headless.quader.svc.cluster.local:16379`.
  The certificate carries exactly these names (`.svc.cluster.local` only), so a cluster with another domain needs
  `certManager.extraDnsNames`.
- No persistence (`emptyDir`, `save ""`, `appendonly no`), `maxmemory-policy noeviction` and no `maxmemory`.
- Sentinels: quorum 2, `down-after-milliseconds 5000`, `failover-timeout 60000` (constants of the operator, the
  values Quader's spec names). After a failover the Sentinels refuse another automatic one for twice the failover
  timeout (120 s); a `SENTINEL FAILOVER` does not wait for that.
- The operator's NetworkPolicy admits only its own components; `quader-valkey-clients` (in valkey.yaml) admits the pods
  labelled `app.kubernetes.io/name=quader` and `quader/serve=true` whose component is `api`, `packages`, `worker`, `e2e-driver`
  or `e2e-replica` (the extra replicas of `TestE12Valkey`) on 16379 and 36379, the data pods and the Sentinels alike: not the
  `scanner`, which carries the serve label for the ops Service and holds no Valkey credential (Q-HA-344; the E0.12 case with
  every Sentinel unreachable starts a replica with the scanner's component label and shows the policy drops it). A pod the
  suite adds that needs Valkey gets a component this list names. kind's CNI enforces both.
- Pods are spread by zone (`antiAffinity` `hard` on `topology.kubernetes.io/zone`, each StatefulSet repels its own
  kind) and have PodDisruptionBudgets (data `maxUnavailable: 1`, Sentinel `minAvailable` = quorum).
- `kubectl rollout status` does not work on these StatefulSets (update strategy `OnDelete`): the scripts wait for
  `readyReplicas` (`sts_wait_ready` in `lib.sh`).
- `valkey-kill.sh` reports two outcomes, both correct: the primary *moved* (the pod stayed away longer than 5 s) or
  *restarted in place* (the StatefulSet recreated it within 5 s, before any Sentinel declared it down; its data is
  gone and the replica resynchronised to nothing, which costs cache warmth only, R-HA-056).
- `valkey-outage.sh` cannot just scale the StatefulSets to 0: the operator restores them within seconds. The script
  stops the operator first (its Deployment to 0), scales the two StatefulSets to 0, waits, scales them back and starts
  the operator again; nothing fights the operator, it is part of the outage. The operator has no pause switch.

### Deviations from the spec in this profile (interim)

The Valkey operator has **no ACL users, no separate Sentinel password and no `maxmemory`** (it renders every client as
`user default on … ~* &* +@all`, one `requirepass` for Valkey and Sentinel, and only `maxmemory-policy`). The profile
therefore deviates from R-HA-056 and R-ARCH-031, in the dev profile only:

| Spec | Dev profile | Where the criterion is still proven |
|---|---|---|
| a Valkey ACL user limited to `~quader:*`, `&quader:*` and the listed commands, no scripting but the take's `EVALSHA` and `EVAL` (R-ARCH-031) | Quader connects as `default` with the cluster password (`secrets.valkey.usernameKey: ""`) | the integration stack keeps the ACL user (`test/integration/valkey`) |
| a Sentinel credential separate from the data credential (R-HA-056) | `secrets.valkeySentinel` points at the same Secret, `quader-valkey-auth` | integration stack |
| `maxmemory` set on Valkey (R-HA-056) | not set; the container limit (320 MiB) bounds it and the pod would be OOM-killed and restart empty, which costs cache warmth only (INV-3) | integration stack |

This is an owner decision pending (operator feature or another route); until then E0.12 on this profile proves the
connection names, TLS only, the start-up cases and the failovers, not the ACL.

### The anonymous rate limit

The management API limits requests without a credential per client address (R-API-016, `api.rate_limits.anonymous`, default
5 requests per second, burst 20). The e2e suite's load scripts (k6) and probes poll `/api/v1/system/ping` and
`/api/v1/system/replicas` anonymously, all from the one address of the host behind the edge, with hundreds of requests per
second. `values-dev.yaml` therefore sets `config.api.rate_limits.anonymous` to 1000 requests per second and a burst of 10000:
the limiter stays on, with a budget the load never reaches. The production default is unchanged; the limits themselves are
tested in `test/integration/api`. The integration environments that start the binary and poll anonymously
(`test/integration/server`, `test/integration/tls`) set the same budget through `QUADER_API_RATE_LIMITS_ANONYMOUS_*`.

## Reaching things

```shell
kubectl --context kind-quader -n quader get pods -o wide                                    # everything
curl -s --cacert .dev/pki/ca.crt https://localhost:8080/api/v1/system/ping                # through the edge (once Quader serves)
kubectl --context kind-quader -n quader get cluster quader-db                                # PostgreSQL, primary and standby
kubectl --context kind-quader -n quader get valkey,certificate                              # Valkey and every certificate
```

Sentinel and Valkey from inside their pods (the password is in the pod's environment, never typed):

```shell
kubectl --context kind-quader -n quader exec quader-valkey-sentinel-0 -c sentinel -- sh -c \
  'valkey-cli --tls --cacert /tls/ca.crt -p 36379 --no-auth-warning -a "$VALKEY_PASSWORD" \
   sentinel get-master-addr-by-name quader-valkey'
kubectl --context kind-quader -n quader exec quader-valkey-0 -c valkey -- sh -c \
  'valkey-cli --tls --cacert /tls/ca.crt -p 16379 --no-auth-warning -a "$VALKEY_PASSWORD" client list'
```

PostgreSQL and S3 from the host: `kubectl --context kind-quader -n quader port-forward svc/quader-db-rw 5432`
and `svc/quader-s3 9000` (https; trust `.dev/pki/ca.crt` and use the name `quader-s3.quader.svc`, for example with
`curl --resolve quader-s3.quader.svc:9000:127.0.0.1`); the credentials are in `quader-db-app` and `quader-s3`.

To renew the certificate of one kind of pod by hand, the way cert-manager's `cmctl renew` does (the e2e drill uses the same trigger on
each kind's):

```shell
kubectl --context kind-quader -n quader patch certificate quader-tls-api --subresource=status --type=json -p \
  '[{"op":"add","path":"/status/conditions/-","value":{"type":"Issuing","status":"True","reason":"ManuallyTriggered","message":"Certificate re-issuance manually triggered","lastTransitionTime":"'"$(date -u +%FT%TZ)"'"}}]'
```

### The test IdP

Keycloak in [keycloak.yaml](../../deploy/dev/keycloak.yaml) is the OpenID Provider of E1.1 and of the login criteria of
R-AUTH-013 (RP-initiated logout ends the IdP session) and R-AUTH-008 (a groups claim). Dex through dex-operator renders
neither the logout configuration nor users with groups, which is why the profile runs Keycloak (Q-AUTH-35).

| What | Value |
|---|---|
| Issuer | `https://quader-keycloak.quader.svc:8443/realms/quader` |
| Users (passwords in Secret `quader-keycloak`, fields `alice-password`, `bob-password`) | `alice`: groups `quader-team-web`, `quader-platform-admins`; `bob`: `quader-team-web` |
| Client | `quader`, confidential (`client-secret` of the same Secret), PKCE S256, redirect URI `https://localhost:8080/auth/oidc/keycloak/callback`, post-logout URI `https://localhost:8080/ui/logged-out` |
| Claims | `preferred_username`, `email` (verified), `name`, `groups` (plain group names) on the ID token and userinfo, `sid` |
| Administrator console | user `admin`, field `admin-password` |

The issuer is the Service name, so Quader's pods reach it directly and the issuer in the discovery document is the one
Quader is configured with. A browser on the host needs the name and a port-forward; the hosts entry needs administrator
rights and is yours to add. (The e2e tests need neither: their HTTP client dials the name through a port-forward of its
own and verifies the certificate against the dev CA for that name, [e2e.md](e2e.md#m1-tests).)

```shell
kubectl --context kind-quader -n quader port-forward svc/quader-keycloak 8443:8443
# once, as an explicit choice of yours:
#   echo '127.0.0.1 quader-keycloak.quader.svc' | sudo tee -a /etc/hosts
kubectl --context kind-quader -n quader get secret quader-keycloak -o jsonpath='{.data.alice-password}' | base64 -d
```

The provider entry is part of values-dev.yaml. The client secret (Secret `quader-keycloak`) and the break-glass password
(Secret `quader-break-glass`) are the chart values `secrets.oidc` and `secrets.breakGlass`, which mount them into the `api` pods
and render `client_secret_file` and `password_file`. The CA of `quader-keycloak-tls` is the value `trustedCA` of the dev values
(`existingSecret: quader-keycloak-tls`, `keys: [ca.crt]`, R-HA-043): the chart mounts it at `/etc/quader/ca/ca.crt` in every pod and lists it in
`proxy.outbound.tls.ca_files`, so the outbound client trusts the IdP's
certificate. The group rule maps the claim values `quader-team-web` and `quader-platform-admins` to the groups `team-web`
and `platform-admins`, which `create_groups` creates on the first login:

```yaml
auth:
  oidc:
    providers:
      keycloak:                                     # example: the ID in the redirect URI above
        display_name: Keycloak (dev)                # example
        issuer: https://quader-keycloak.quader.svc:8443/realms/quader   # example
        client_id: quader                            # example
        # client_secret_file is set by the chart from secrets.oidc (/var/run/secrets/quader/oidc-keycloak/client-secret)
        scopes: [openid, profile, email]            # default
        group_sync:
          rules:
            - match_regex: "quader-(.+)"             # example
              group: "$1"                           # example
        create_groups: true                         # example: the groups of the rules are created on login
```

and in the chart values:

```yaml
secrets:
  oidc:
    - {providerId: keycloak, existingSecret: quader-keycloak, key: client-secret}   # example
  breakGlass: {existingSecret: quader-break-glass, key: password}                    # example; password_file is rendered by the chart
```

`up.sh` sets `KC_QUADER_BASE_URL` on the Deployment from the edge port of the cluster (`https://localhost:<port>`), and the
realm file takes its redirect and post-logout URIs from it at import, so another `QUADER_DEV_HTTP_PORT` needs nothing
else (a changed port restarts Keycloak once, which imports the realm again).

## Tools

| Tool | Pin | Check |
|---|---|---|
| kind | `KIND_VERSION` | `kind version` |
| kubectl | `KUBECTL_VERSION` | `kubectl version --client` |
| Helm | `HELM_VERSION` | `helm version` |
| openssl | `OPENSSL_IMAGE` (a container image, no host install; used by the e2e probes) | `docker run --rm "$OPENSSL_IMAGE" version` |
| Docker | none | the daemon must answer; on arm64 it must run amd64 binaries (above) |

`up.sh` fails when kind, kubectl or Helm differs from its pin and prints the install command. The charts come from
their own repositories, nothing is added with `helm repo add`: `chart_tgz` in [lib.sh](../../deploy/dev/lib.sh) fetches each pinned
chart once with `helm pull --repo <url> --version <pin>` into `.dev/charts/` and every install reads that file, so a second `up.sh`
on the same cluster (the profile switch of an e2e test) needs no network. The cache outlives `make dev-down`; a new pin is a new file.
No script makes certificates by hand any more; cert-manager does. The scripts run under macOS `/bin/bash` 3.2 as well
as bash 5.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `port 8080 on localhost is in use` | another process holds the port. `QUADER_DEV_HTTP_PORT=18080 make dev-up`; `base_url` follows. The port is fixed per cluster: `make dev-down` first to change it |
| `kind create cluster failed` | see `.dev/logs/kind.log`; usually Docker is out of memory or a stale `quader-*` container exists (`docker ps -a --filter name=quader-`) |
| a dependency FAILED | the last lines of its log are printed; the full log is `.dev/logs/<haproxy\|pki\|secrets\|vko\|postgres\|valkey\|s3\|keycloak>.log`. Below them `up.sh` prints what the cluster says (`explain_setup_failure` in [lib.sh](../../deploy/dev/lib.sh), [Q-HA-304](../spec/ha-and-helm.md#q-ha-304--a-setup-that-fails-says-whether-a-registry-or-the-machine-was-the-cause-and-the-test-idp-is-not-restarted-inside-the-wait)): an image that could not be pulled, said to be a cause outside the repository; the eight slowest image pulls with their seconds; the containers a probe restarted; the pods that are not ready; the CPU, I/O and memory pressure of the machine the nodes share. Pulls of minutes and a high pressure with no failed pull mean a slow machine, not the profile. Re-run `make dev-up`, it is idempotent |
| `the image … is not in the local Docker` | build it (`make image`), or `QUADER_SKIP_RELEASE=1` for the dependencies only |
| Quader pods never ready after install | `kubectl logs` of a pod and `kubectl get pod -o yaml`; a replica waits for the schema until the migration Job has run, and refuses a version that is not `major.minor.patch` or `dev`: build with `make image VERSION=dev` |
| an operator pod `ImagePullBackOff` with `no match for platform` | the image is amd64 only and the node arm64: pin the amd64 manifest digest (already done) and register the emulator (above) |
| an operator pod `exec format error` | the Docker VM has no amd64 emulation: `docker run --privileged --rm "$DEV_BINFMT_IMAGE" --install amd64` |
| `curl: (60) SSL certificate problem` | the client does not trust the dev CA: `--cacert .dev/pki/ca.crt` |
| `quader-db-2` Pending after a drain | its volume is local to the drained node; it starts again after `kubectl uncordon`. `drain.sh` uncordons for you |
| drain hangs on a CloudNativePG pod | the primary's own PodDisruptionBudget (`quader-db-primary`, `minAvailable: 1`) refuses its eviction. Run `make dev-failover` first, or let `drain.sh` choose the node |
| `make dev-failover`: "no standby of quader-db can take over: … runs on the cordoned node" | a node is cordoned, and CloudNativePG has already switched the primary off it; a switchover back onto it would be undone. `kubectl uncordon` the node first |
| `make dev-failover`: "precondition not met within 180s" | the message names it: the operator has no ready pod holding its lease (`kubectl -n cnpg-system get pods,lease`), or the cluster is not settled (`kubectl -n quader get cluster quader-db`: a switchover in flight, an instance not ready) |
| Valkey kill: no failover although the pod is gone | a failover less than 120 s ago blocks the next automatic one (see above); the script waits up to 4 minutes |
| helm release stuck `pending-install` | `helm --kube-context kind-quader -n quader uninstall quader`; the hook Job, ConfigMap and ServiceAccount `quader-migrate` outlive an uninstall by design, delete them with `kubectl delete job,cm,sa quader-migrate` |
| a clean start | `make dev-down && make dev-up` |

## What is verified, and what is not

Verified by [the end-to-end suite](e2e.md) on this profile, with the release image, over TLS on every connection: the
cold `make dev-up` (2 min 33 s, budget 15 min), every Quader pod answering `200` on `:8081/readyz`, the replica list and
ping through the Ingress, the restricted Pod Security Standard and the chart's NetworkPolicy (enforced by kindnet: a
pod of another namespace reaches `:8080` and not `:8081`), the failing migration Job, the lease failover, the Valkey
connection names and the start-up cases, the Valkey failover, kill and outage under load, the `scatter` profile on
HAProxy Ingress, the PostgreSQL failovers under load, the node drain, plaintext refused on Quader and the object store,
the renewal of Quader's certificate by cert-manager under load, and the second endpoint. [e2e.md](e2e.md) has the
numbers. The pins of 2026-10-30 came up unchanged on a fresh cluster, `make dev-up` in 2 min 39 s: Helm 4.3.1 (the host
had 3.21.3; the pinned binary was put first on `PATH`), kubectl v1.37.2, kind v0.33.0 with node image
`kindest/node:v1.37.0`, HAProxy Ingress chart and image 0.16.3, CloudNativePG operator 1.30.1 with PostgreSQL 18.6
(`quader-db-1` and `-2` ready), aws-cli 2.39.2 (the bucket Job completed). The same cluster served the npm acceptance matrix.

Not verified here, or not configured:

- The ACL user, the separate Sentinel credential and `maxmemory` of Valkey (deviations above); R-ARCH-031's ACL
  criterion is proven by the integration stack only.
- The chart renders `auth.break_glass.enabled: false` without `secrets.breakGlass`; the dev values set it. The Keycloak CA is
  the `trustedCA` value. A real rotation of `quader-break-glass` by the operator (the 24 h
  interval, the hold and the ended sessions in a running cluster) is verified by the chart render and the code, not by a drill.
- Valkey and the S3 gateway read their certificates at start: a cert-manager renewal reaches the Valkey pods through
  the operator's own rolling restart (ADR 0030 of the operator, not exercised here) and SeaweedFS at its next restart.
  Quader re-reads the Valkey CA per handshake and the S3 CA per request.
- The operators' amd64 images run under qemu on an arm64 host; their timing there says nothing about production.
- WAL archiving to `quader-pg-archive` (point-in-time recovery is M5), a run of the backup CronJob against the profile and the
  restore drill (the chart renders both, `make chart-test` checks the render; the runs are the end-to-end suite's), the Playwright journeys of R-UI-047 (E1.11): the harness has no browser; the e2e tests drive the login with an HTTP
  client ([e2e.md](e2e.md#m1-tests)), and `TestE1_11Journeys` runs the suite as soon as `web/e2e` exists.
