# Helm chart

The chart lives in [deploy/helm/quader](../../deploy/helm/quader). This page covers its layout, what
it renders, how its values reach the pods, how it is validated and tested, and how to extend it.
The second chart, `quader-bastion`, has its own section at the end: [The Bastion chart](#the-bastion-chart).
The requirements are in [ha-and-helm.md](../spec/ha-and-helm.md) (R-HA-002, R-HA-011, R-HA-016, R-HA-019,
R-HA-027 to R-HA-037, R-HA-042, R-HA-056, R-HA-060 to R-HA-063; the Jobs also R-BAK-012) and [endpoints.md](../spec/endpoints.md) (R-EP-008); the dev profile that installs it is described in
[local-profile.md](local-profile.md).

## Scope

Everything the chart renders is real and tested. It does not render what later phases add, and `values.schema.json` refuses their values
(`additionalProperties: false`), so a value of a later phase fails before anything renders instead
of being silently ignored.

| In the chart | Not in the chart yet |
|---|---|
| `api`, `packages` and `worker` Deployments, always (R-HA-002); path pools, worker pools and endpoint pools per role (R-HA-062, R-EP-008); see [The cut into roles](#the-cut-into-roles) | the Gateway the chart creates (`gateway.create`, R-HA-053), `contentOrigin` |
| role Services `<fullname>-api` and `<fullname>-packages`, pool Services, ops Service `<fullname>-ops`, `sessionAffinity: None` (R-HA-037) | |
| PodDisruptionBudget and HorizontalPodAutoscaler per Deployment, topology spread with `nodeTaintsPolicy: Honor` (R-HA-032) | |
| one NetworkPolicy per Deployment and per Job (migration, backup, restore, post-restore), ingress only (R-HA-042, ADR-154) | |
| PrometheusRule with 55 alerts of [_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl) (R-HA-045): 37 always, the four of each of the malware scanning, the signatures and the signing identity while their switch is on, and the six of the backup runs with `backup.enabled`, see [The PrometheusRule](#the-prometheusrule); `metrics.prometheusRule.*` | |
| migration Job as `pre-install,pre-upgrade` hook, with hook ConfigMap and ServiceAccount (R-HA-019); not rendered while `restore.enabled` | |
| post-restore hook Job `<fullname>-post-restore` with its ServiceAccount, policy and credentials file (R-HA-025 step 4), see [The post-restore Job](#the-post-restore-job) | |
| backup CronJob `<fullname>-backup` and restore Job `<fullname>-restore`, each with its ServiceAccount and a policy that admits nothing (R-HA-060, R-BAK-012), see [The backup and restore Jobs](#the-backup-and-restore-jobs) | |
| configuration file `quader.yaml`, Secret wiring including the login secrets `secrets.oidc` and `secrets.breakGlass`, downward-API variables (R-HA-030); `secrets.databaseBackup`, `secrets.backupTarget`, `secrets.backupTargetCA` | `trustedCA`, `bootstrap` |
| restricted-PSS security contexts, probes, spool and `/tmp` volumes (R-HA-033 to R-HA-035, R-HA-061) | `initContainers`, `sidecars`, `lifecycle`, Service types beyond the three in the schema |
| Ingress with the path rules of the roles (R-HA-037); HTTPRoute of the default endpoint (`gateway.*`, R-HA-038, see [The HTTPRoute](#the-httproute)); ServiceMonitor (`metrics.serviceMonitor.*`, R-HA-045, see [The ServiceMonitor](#the-servicemonitor)); TLS on Quader's listeners (R-HA-063); the values reference of the spec (R-HA-031, see [The values reference](#the-values-reference)) | the HTTPRoute of further endpoints (R-EP-008); profiles `small`, `medium`, `large` (R-HA-050) |
| further endpoints: Services, Ingress, optional pool per role, certificates (R-EP-008), see [Endpoints](#endpoints) | the validation rows of the features in the right column |

Gaps in what is there:

- **The `config` subtree is free-form.** R-HA-029 wants it validated against the schema the binary
  emits (`internal/config/schema.json`, not generated yet). The schema types only the keys the
  chart reads itself; a typo elsewhere in `config` fails at runtime (strict validation in the
  binary), not at `helm install`. When the binary emits its schema, `values.schema.json` takes it
  as the `config` property.
- **The container is started with `args: [serve, --role, <role>]` and no `command`.** It relies on
  the image's `ENTRYPOINT` being the Quader binary.
- **No kubeconform run** over the rendered output yet; the tool is not pinned.

## The cut into roles

The chart renders the roles of ADR-153 and ADR-155 ([roadmap](../coverage/roadmap.md#phase-6a--task-areas), WP6a.3). Every template that
renders something per Deployment ranges over one list, `quader.units` ([_units.tpl](../../deploy/helm/quader/templates/_units.tpl)): a
map per Deployment with its `name`, `role`, `pool`, `kind` (`main`, `pool`, `endpoint`), effective values `r`, and for a worker
pool its `queues`. The Deployment, its PodDisruptionBudget, HorizontalPodAutoscaler and NetworkPolicy therefore cannot disagree on
name, labels or selector.

| Object | Rendered | Spec |
|---|---|---|
| Deployments | `<fullname>-api`, `<fullname>-packages`, `<fullname>-worker`; path pools `<fullname>-<role>-<pool>`; worker pools `<fullname>-worker-<pool>`; endpoint pools `<fullname>-packages-ep-<name>` and `<fullname>-api-ep-<name>` (one per role the surface holds) | R-HA-002, R-HA-062, R-EP-008 |
| Services | `<fullname>-api` and `<fullname>-packages` on 8080; one per path pool; `<fullname>-ops`; per further endpoint `<fullname>-ep-<name>` and, for a surface of both roles, `<fullname>-api-ep-<name>`; worker pods have none | R-HA-002, R-HA-028, Q-EP-71 |
| Edge | per host the `Prefix` rules `/` and `/api/v1` to `api`, `/api`, `/files`, `/v2`, `/.well-known` to `packages`, and the paths of the path pools before them ([_edge.tpl](../../deploy/helm/quader/templates/_edge.tpl), `quader.edgeRules`); a further endpoint over one role is routed whole, one over both by the rows of its families | R-HA-037, Q-HA-71 |
| PDB, HPA | one per Deployment; the HPA (`autoscaling/v2`, CPU and memory targets, `metrics`, `behavior`) replaces `spec.replicas` | R-HA-032 |
| NetworkPolicy | one per Deployment, named as it, a rule per port its pods open; none for egress | R-HA-042, Q-HA-76 |
| Login secrets | `secrets.oidc[]` and `secrets.breakGlass` in `api` pods only (main, pools, endpoint pools) | R-HA-030, Q-HA-79 |
| Upstream accounts | `secrets.upstreamAccounts.<key>`: one projected volume `upstream-accounts`, a source per entry with `optional: true`, in every pod of `api`, `packages` and `worker` at `/var/run/secrets/quader/upstream-accounts/<key>/{username,password}`; `config.repositories.upstream_accounts` rendered from it ([_upstream_accounts.tpl](../../deploy/helm/quader/templates/_upstream_accounts.tpl)) | R-HA-073, Q-HA-493 |
| Declared Bastions | `bastions.declared[]`: one projected volume `bastion-tokens`, a source per entry with `optional: true`, in the migration Job alone at `/var/run/secrets/quader/bastion-tokens/<name>/token`; `config.bastions.declared` rendered from it with the path and the Secret's name ([_bastions.tpl](../../deploy/helm/quader/templates/_bastions.tpl)) | R-BAS-127, Q-BAS-469 |
| Environment | `QUADER_POOL_NAME` on every pool, `QUADER_JOBS_QUEUES` (a JSON list) on a worker pool, `QUADER_JOBS_EXCLUDE_QUEUES` on the main worker beside pools; the two preflight counts on the migration Job | R-HA-030, R-HA-016, Q-HA-82 |

A **pool** is the role's values with the entry's keys on top, key by key (`quader.poolValues`, so `false` and `0` override, and
`autoscaling` is inherited, Q-HA-84). A path pool opens the ports of the endpoints without a pool, like the main Deployment, but
only the default endpoint's edge routes its `paths` (Q-HA-83). The names of pools match `^[a-z][a-z0-9-]{0,19}$` and may not start
with `ep-`; `quader.validatePools` checks names, paths inside the role's families, queues and duplicates.

The **connection budget** (R-HA-016): `quader.connectionCounts` sums `maxReplicas` (or `replicaCount`) plus `maxSurge` over every
Deployment, serving roles and worker apart, and passes `QUADER_DATABASE_PREFLIGHT_EXPECTED_SERVING_REPLICAS` and
`..._WORKER_REPLICAS` to the migration Job; `NOTES.txt` prints the resulting `max_connections` (231 with the defaults). The key `expected_serving_replicas` is the binary's (WP6a.1); until it lands, `quader admin config validate` over
the rendered environment names it as unknown.

A path pool takes `routed: false` to render without its route, the first of two applies that add a pool (Q-HA-90). With an autoscaler a Deployment renders the live `spec.replicas` read through `lookup`, so switching the autoscaler on keeps the replicas (Q-HA-91). A provider of `config.auth.oidc.providers` that has a client secret and no entry in `secrets.oidc` fails the render (Q-HA-93).

The chart renders neither `--role web` nor `--role all`; the bridge of the upgrade from the shape before the cut, with its values
`web.*`, its guard and its alert, is gone, and the schema refuses the key `web`
([Q-HA-315](../spec/ha-and-helm.md#q-ha-315--the-charts-upgrade-bridge-across-the-cut-is-removed-the-binary-keeps-the-role-web-for-development-and-tests)).

The HTTPRoute of R-HA-038 loops over the same table, see [The HTTPRoute](#the-httproute).

## Layout

```text
deploy/helm/quader/
├── Chart.yaml              apiVersion v2; version = appVersion = 0.0.0-dev (the release pipeline, build.yml, rewrites both)
├── values.yaml             defaults; a value is commented only where its name does not say it
├── values.schema.json      draft-07, additionalProperties false for everything the chart owns
├── tests/rules_test.yaml   promtool test rules for the PrometheusRule (not packaged)
├── profiles/               the sizing profiles small, medium and large (R-HA-050); layered before the operator's file, packaged
├── examples/               values an installer starts from (packaged)
│   └── production.yaml         the production shape: autoscalers, PodDisruptionBudgets, topology spreading, the pooler, Ingress and HTTPRoute, ingress policies, ServiceMonitor and PrometheusRule, the backup (Q-HA-262)
├── ci/                     value sets the tests render (not packaged, see .helmignore)
│   ├── required-only.yaml      only the values R-HA-031 names as required; the backup stays off
│   ├── ha-ingress.yaml         every optional Secret, Ingress with TLS, ephemeral spool, the backup on
│   ├── single-node.yaml        no enforcement, one replica per role, standalone Valkey
│   ├── migrations-disabled.yaml  migrations.mode disabled, minAvailable budget, image digest
│   ├── monitoring.yaml         the PrometheusRule and the backup on, the vulnerabilities section the chart reads (the rule tests render this set)
│   ├── endpoints.yaml          two further endpoints on the main packages pods, own Ingress classes, certificates by SNI
│   ├── endpoints-pool.yaml     an endpoint with a pool of its own
│   ├── roles-pools.yaml        a path pool of api and of packages, two worker pools, an autoscaler, a mixed endpoint with pools

│   └── connections.yaml        two object-store connections: one from a provisioner's Secret, one with static keys and a private CA; the backup on with a read-only login each
└── templates/
    ├── _helpers.tpl        names, labels, image, byte and duration parsing, spool budget, config
    ├── _secrets.tpl        the Secret mounts of a pod, as one list used for volumes and mounts
    ├── _storage.tpl        the object-store connections: the elements with their ids and *_file keys, the mounts of secrets.storageConnections, backup.source, the refusals (R-HA-072)
    ├── _pod.tpl            probes, topology spread, downward-API variables
    ├── _validate.tpl       fail-fast validation (R-HA-029), endpoint validation (R-EP-008)
    ├── _units.tpl          the Deployments as data (quader.units), surface roles, endpoints per pod, replica counts
    ├── _edge.tpl           the edge's path rules (quader.edgeRules), read by the Ingresses and the BackendTLSPolicies
    ├── _validate_roles.tpl roles, pools, autoscalers, spool and memory budgets
    ├── _deployment.tpl     one Deployment of a unit
    ├── _endpoints.tpl      endpoint defaults, quader.yaml block, mounts
    ├── _notes.tpl          the text of NOTES.txt
    ├── _tls.tpl            the kinds of pod with a pair (quader.tlsKindTable), their names and Secrets, the edge's backend settings
    ├── _vuln.tpl           the image scanning's links and checks, the scan listener both scannings read through
    ├── _malware.tpl        the malware scanning's links and checks, clamd.conf, freshclam.conf, the updater's checks
    ├── _signatures.tpl     the anchors of signatures.anchors rendered into the configuration, their material mounted into the migration Job
    ├── vuln-scanner-trivy.yaml      the image scanning's scanner (Trivy) and its database's loader: Deployment, budget, autoscaler, Service, policy
    ├── _signing.tpl        the signing identity's source, its Secret, its three files in the configuration, its checks (R-HA-071)
    ├── _upstream_accounts.tpl  the accounts of the upstream registries: the projected volume, its mount, the configuration's paths, the checks (R-HA-073)
    ├── _bastions.tpl       the Bastion registrations of bastions.declared: the configuration's entries, the token Secrets mounted into the migration Job, the checks (R-BAS-127)
    ├── signing-certificate.yaml   the cert-manager Certificate <fullname>-signing of the signing identity, on request
    ├── malware-scanner-clamav.yaml  the malware scanner with clamd beside it: Deployment, budget, autoscaler, Service, policy, ConfigMap
    ├── malware-updater-freshclam.yaml  the updater with freshclam beside it: Deployment, Service, policy, ConfigMap
    ├── validate.yaml       renders nothing; includes the validation
    ├── deployments.yaml    one Deployment per unit
    ├── services.yaml       the role and pool Services, <fullname>-ops
    ├── pdb.yaml, hpa.yaml  one PodDisruptionBudget and, with autoscaling.enabled, one HorizontalPodAutoscaler per Deployment
    ├── endpoint-services.yaml, endpoint-ingress.yaml   per further endpoint
    ├── NOTES.txt           endpoints and warnings
    ├── networkpolicy.yaml  one ingress policy per Deployment and the migration Job's
    ├── prometheusrule.yaml the PrometheusRule (only with metrics.prometheusRule.enabled); the alerts are _rules.tpl
    ├── _rules.tpl          the alerts as a YAML list (quader.rules.all) and their names (quader.rules.names)
    ├── configmap.yaml      <fullname>-config (quader.yaml)
    ├── serviceaccount.yaml
    ├── migrate.yaml        hook ConfigMap, hook ServiceAccount and the migration Job
    ├── ingress.yaml        only with ingress.enabled
    └── extra-objects.yaml  extraObjects, rendered with tpl
```

| File | Responsibility |
|---|---|
| `_helpers.tpl` | `quader.fullname` (release name, or `<release>-quader`, never cut; `quader.validateNames` in `_validate.tpl` refuses one too long for the names built from it, [Q-HA-374](../spec/ha-and-helm.md#q-ha-374--the-chart-refuses-a-fullname-too-long-for-the-names-it-builds-and-never-cuts-it)), `quader.labels` / `quader.selectorLabels`, `quader.image` (digest wins over tag), `quader.bytes` and `quader.seconds` (parse sizes and durations; they fail on anything else, so a typo never becomes a wrong budget), `quader.spoolBudget`, `quader.config` |
| `_secrets.tpl` | `quader.secretMounts` builds one list per scope (`runtime` for the Deployments, `migrate` for the Job); `quader.secretVolumes` and `quader.secretVolumeMounts` render it |
| `_storage.tpl` | the object-store connections (R-HA-072): `quader.connectionId` (an element without one is `default`), `quader.connectionEntry`, `quader.connectionManagedKeys` (the `*_file` keys an entry and `trustedCA.useForObjectStore` set), `quader.storageConnectionsConfig` (the list in `quader.yaml`), `quader.storageSecretMounts` (the volumes per scope, joined into `quader.secretMounts`), `quader.backupSourceConfig` (the elements of `backup.source.connections`), `quader.journalCredentialElements` (the elements of the journal credentials file), `quader.journalReadersMissing` (the refusal of Q-HA-399), `quader.validateStorageMoved` (the values of the one store of before) and `quader.validateStorage` |
| `_units.tpl` | `quader.units`, the one list of Deployments; `quader.unitEndpoints` (which endpoint ports a pod opens, R-EP-002); `quader.poolValues`; `quader.connectionCounts` |
| `_validate.tpl`, `_validate_roles.tpl` | every `fail` of R-HA-029, each message naming the offending value |
| `deployments.yaml` | one loop over `quader.units`, so no role can drift from another; the same `quader.deployment` renders a pool |
| `migrate.yaml` | the Job and its hook resources; renders nothing with `migrations.mode: disabled` |

## What the chart renders

`<fullname>` is the release name, or `<release>-quader` when the release name does not contain
`quader`; `fullnameOverride` wins. It is never cut: the render fails when it is longer than the tightest name built from it allows,
40 characters with today's kinds ([Q-HA-374](../spec/ha-and-helm.md#q-ha-374--the-chart-refuses-a-fullname-too-long-for-the-names-it-builds-and-never-cuts-it)).

| Kind | Name | Notes |
|---|---|---|
| Deployment | `<fullname>-api`, `<fullname>-packages` | `quader serve --role api` / `--role packages`; ports `http` 8080 and `ops` 8081 (and the ports of the further endpoints they serve) |
| Deployment | `<fullname>-worker` | `quader serve --role worker`; port `ops` 8081 only |
| Deployment, PodDisruptionBudget, autoscaler (optional), NetworkPolicy | `<fullname>-vuln-scanner-trivy` | `quader serve --role scanner` (the kind `trivy`), while `config.vulnerabilities.enabled` and `config.vulnerabilities.images.enabled` (the defaults); ports `https` 8080 (the scanner's listener) and `ops` 8081; [The scanner](#the-scanner) below (R-HA-065) |
| Service | `<fullname>-vuln-scanner-trivy`, `<fullname>-packages-scan` | the links of the image scanning, always `ClusterIP`: 8080 of the scanner pods, 8082 (the scan listener) of the main `packages` pods (R-HA-066, Q-HA-319); `<fullname>-packages-scan` while either scanning is on |
| Deployment, PodDisruptionBudget, autoscaler (optional), NetworkPolicy, Service, ConfigMap | `<fullname>-malware-scanner-clamav`, `<fullname>-clamd` | the role `scanner` of the kind `clamav` with `clamd` beside it, while `config.malware.enabled` (the default); ports `https` 8080 and `ops` 8081; [The malware scanner](#the-malware-scanner) below (R-HA-068) |
| Deployment, NetworkPolicy, Service, ConfigMap | `<fullname>-malware-updater-freshclam` | the role `scanner` of the kind `freshclam` with `freshclam` beside it, one replica, while `config.malware.enabled` and `config.malware.database.source` is `updater` (the defaults); ports `https` 8080 and `ops` 8081; the ConfigMap holds `freshclam.conf`; [The updater](#the-updater) below (R-HA-069) |
| Deployment, Service (path pools only) | `<fullname>-api-<pool>`, `<fullname>-packages-<pool>`, `<fullname>-worker-<pool>` | per entry of `api.pools`, `packages.pools`, `worker.pools` |
| Service | `<fullname>-api`, `<fullname>-packages` | port `http` 8080, select the main pods of their role only, `sessionAffinity: None` |
| Service | `<fullname>-ops` | port `ops` 8081, selects every pod labelled `quader/serve: "true"` (`api`, `packages`, `worker`, `vuln-scanner-trivy`, `malware-scanner-clamav`, `malware-updater-freshclam`) |
| ConfigMap | `<fullname>-config` | `quader.yaml`, mounted at `/etc/quader/quader.yaml` |
| ServiceAccount | `<fullname>` | `automountServiceAccountToken: false` |
| PodDisruptionBudget | one per Deployment, named as it | `maxUnavailable: 1`, `unhealthyPodEvictionPolicy: AlwaysAllow` |
| HorizontalPodAutoscaler | one per Deployment, named as it | `autoscaling.enabled` of the role or pool; `autoscaling/v2` |
| NetworkPolicy | one per Deployment, named as it, and `<fullname>-migrate` (hook) | `networkPolicy.enabled`, ingress only, see [Network policy](#network-policy) |
| ConfigMap, ServiceAccount (hook, weight -10) | `<fullname>-migrate` | same `quader.yaml` as `<fullname>-config` |
| Job (hook, weight 0) | `<fullname>-migrate` | `quader migrate up`, which also applies `config.authz.role_bindings` (R-AUTHZ-010; `TestR_AUTHZ_010_DeclaredRoleBindingsReachTheMigrationJob`) and the trust anchors of `signatures.anchors` with the release's shipped ones (R-SIG-017, R-SIG-018; the anchors' ConfigMaps mounted read-only at `/etc/quader/signature-anchors/<key>/` in this Job only, `TestR_SIG_018_DeclaredAnchorsReachTheMigrationJob`); `backoffLimit` 2, deadline 1800 s, TTL 86400 s |
| Service, Ingress; pool Deployments, PodDisruptionBudgets, HorizontalPodAutoscalers `<fullname>-<role>-ep-<name>` | `<fullname>-ep-<name>` (and `-api`) | per further endpoint, see [Endpoints](#endpoints) |
| Ingress | `<fullname>` | `ingress.enabled`; per host the path rules of R-HA-037 (all `Prefix`, port name `http`); backend-over-TLS annotations per controller, see [TLS](#tls) |
| BackendTLSPolicy | one per Service the default Ingress names, named as the Service | `tls.backendTLSPolicy.enabled` only; Gateway API CRD, see [TLS](#tls) |
| anything | from `extraObjects` | |

The chart renders no Secret, no Role or RoleBinding, no PersistentVolumeClaim, no StatefulSet and
no CRD (R-HA-028, R-HA-033, R-HA-061); the tests check each.

Labels on every object: `app.kubernetes.io/name`, `instance`, `version`, `managed-by`,
`component` (`api`, `packages`, `worker`, `migrate`, `ops`, `config`, `ingress`, `serviceaccount`) and `helm.sh/chart`.
The pods of `api`, `packages` and `worker` also carry `quader/pool` (selector label: the pool's name, `ep-<name>` for an
endpoint pool, the empty value for the main Deployments, so that a pool never overlaps their selectors or a role Service's;
selector labels are immutable) and `quader/serve: "true"` (what the ops Service selects).

### Pod template

- `checksum/config` (hash of the rendered `quader.yaml`) and `checksum/encryption-keys` (hash of the
  key *names*) roll the pods when either changes. Secret contents are never hashed.
- `enableServiceLinks: false`, `automountServiceAccountToken: false`.
- Container: restricted-PSS security context, UID/GID 65532, read-only root file system.
- Probes (R-HA-034) on the named port `ops`: startup and liveness `GET /healthz`, readiness
  `GET /readyz`; each can be disabled or replaced (`probes.<name>.custom`).
- No `preStop` hook: the drain delay lives in the process (R-HA-011).
- Tolerations for `not-ready` and `unreachable` with `nodeFailureTolerationSeconds` (60).
- Topology spread (R-HA-032): `kubernetes.io/hostname` (`DoNotSchedule`) and
  `topology.kubernetes.io/zone` (`ScheduleAnyway`), both `maxSkew: 1`, `nodeTaintsPolicy: Honor`,
  `matchLabelKeys: [pod-template-hash]`. Non-empty `topologySpreadConstraints` replace them and
  carry their own `labelSelector`.
- Volumes (R-HA-061): `spool` (`emptyDir` with `sizeLimit`, or a generic ephemeral volume with
  `ReadWriteOnce` or `ReadWriteOncePod`), `tmp` (`emptyDir`), the config ConfigMap, the Secrets.

### Environment

The chart sets these variables and no others (R-HA-030):

| Variable | Value | Pods |
|---|---|---|
| `QUADER_POD_NAME`, `QUADER_POD_NAMESPACE`, `QUADER_NODE_NAME` | downward API (`metadata.name`, `metadata.namespace`, `spec.nodeName`) | every Quader pod |
| `QUADER_RUNTIME_MEMORY_LIMIT_BYTES` | `resourceFieldRef limits.memory` | every Deployment |
| `QUADER_RUNTIME_TERMINATION_GRACE_PERIOD` | `<terminationGracePeriodSeconds>s` | every Deployment |
| `QUADER_POOL_NAME` | the pool's name, `ep-<name>` for an endpoint pool | the pods of a pool (R-HA-062, R-EP-002) |
| `QUADER_JOBS_QUEUES` | the pool's `queues` as a JSON list, `["vuln"]` | a worker pool |
| `QUADER_JOBS_EXCLUDE_QUEUES` | the sorted union of the worker pools' queues as a JSON list | the main `worker` while a pool lists a queue |
| `QUADER_STORAGE_SPOOL_MAX_BYTES` | spool size minus `spool.headroom`, in bytes; not set when `config.storage.spool.max_bytes` is set | every Deployment |
| `GOMAXPROCS` | `resourceFieldRef requests.cpu`, only when the role has no CPU limit | every Deployment |
| `TMPDIR` | `/tmp` | every Quader pod |
| `QUADER_DATABASE_PREFLIGHT_EXPECTED_SERVING_REPLICAS`, `..._WORKER_REPLICAS` | `serving_max` and `worker_max` of R-HA-016 | migration Job |

`extraEnv` and `extraEnvFrom` append to this list. The binary's strict validation accepts the
`QUADER_` names above as schema keys or as reserved variables (R-OPS-028).

### Configuration file

`quader.config` (in `_helpers.tpl`) renders `config` merged with the keys the chart manages. The
chart-managed keys win, and setting one inside `config` fails the render naming it
(`config.storage.spool.dir`, `config.database.password_file`, `config.secrets.active_key_id`, …). The list
`storage.connections` is rendered whole by `quader.storageConnectionsConfig`, because a merge replaces a list: each element of
`config.storage.connections` with its `id` and the `*_file` keys of its entry (see [The object-store connections](#the-object-store-connections)).

### Secrets

Every Secret is pre-existing; the chart never creates one. Each is mounted read-only with `items`
(only the named keys), `defaultMode: 0440`, no `subPath`, so a rotation reaches the files, and
never as an environment variable, not even a provisioner's bucket, region and endpoint, which are files too (see
[The object-store connections](#the-object-store-connections)). A mount exists only in the pods that read it.

| Value | Keys (default names) | Mount path | Config key set by the chart | Pods |
|---|---|---|---|---|
| `secrets.database` | `username`, `password` | `/var/run/secrets/quader/database/` | `database.username_file`, `database.password_file` | all (the Job uses `secrets.databaseMigration` when set) |
| `secrets.databaseBackup` | `username`, `password` | `/var/run/secrets/quader/database/` (the backup pods' mount of the database login) | `database.username_file`, `database.password_file` | the backup pods only; empty means `secrets.database` |
| `secrets.databaseCA` | `ca.crt` | `/var/run/secrets/quader/database-ca/` | `database.ssl_root_cert_file` | all |
| `secrets.databaseClientCert` | `tls.crt`, `tls.key` | `/var/run/secrets/quader/database-client/` | `database.ssl_cert_file`, `database.ssl_key_file` | all |
| `secrets.encryption` | the names in `secrets.encryption.keys` | `/var/run/secrets/quader/encryption/` | `secrets.encryption_key_files` (ordered), `secrets.active_key_id` (first entry without `.key`) | every Deployment, the same Secret (R-HA-027) and the restore Job, which checks that it opens every key ID of the backup (R-BAK-020); the migration Job and the backup pods never mount it (R-HA-030, R-BAK-012) |
| `secrets.storageConnections[]` `existingSecret` (element with `credentials_source: static`) | `access-key-id`, `secret-access-key`, `session-token` (with `sessionTokenKey`) | `/var/run/secrets/quader/s3-<id>/` | the element's `access_key_id_file`, `secret_access_key_file`, `session_token_file` (always refused inside `config`) | the role Deployments, the restore Job (it writes the bucket) and the post-restore Job; not the backup pods |
| `secrets.storageConnections[]` `bucketKey`, `regionKey`, `endpointKey` | `bucket`, `region`, `endpoint` | `/var/run/secrets/quader/s3-<id>-location/` | the element's `bucket_file`, `region_file`, `endpoint_file` (refused inside `config` while the entry sets them) | the same pods and the backup pods |
| `secrets.storageConnections[]` `ca` | `ca.crt` (the field's `key`) | `/var/run/secrets/quader/s3-<id>-ca/` | the element's `ca_file` (refused inside `config` while set) | the same pods and the backup pods; never the migration Job |
| `secrets.backupTarget` (`config.backup.target.credentials.source: static`) | `access-key-id`, `secret-access-key`, `session-token` (with `sessionTokenKey`) | `/var/run/secrets/quader/backup-target/` | `backup.target.credentials.access_key_id_file`, `..._secret_access_key_file`, `..._session_token_file` (all three refused inside `config`) | the backup pods and the restore Job; no Deployment, not the migration Job |
| `secrets.storageConnections[]` `journalReader` (`journalCredentials.source: static` of `postRestore` or `restore`) | `access-key-id`, `secret-access-key` | `/var/run/secrets/quader/s3-<id>-journal/` | none; the element of the connection in the credentials file, `{connection, credentials_source: static, access_key_id_file, secret_access_key_file}` (R-BAK-030, Q-BAK-73); required in the entry of every connection without drain ([Q-HA-399](../spec/ha-and-helm.md#q-ha-399--a-static-journal-reader-is-required-for-every-connection-without-drain)) | the post-restore Job and the restore Job only |
| `secrets.storageConnections[]` `backupSource` | `access-key-id`, `secret-access-key` | `/var/run/secrets/quader/s3-<id>-backup/` | the element of the connection in `backup.source.connections`, `{connection, credentials_source: static, access_key_id_file, secret_access_key_file}` (R-BAK-030, Q-BAK-64; `backup.source` refused inside `config`) | the backup pods only; they do not mount the connection's runtime keys |
| `secrets.backupTargetCA` | `ca.crt` (the Secret's `key`) | `/var/run/secrets/quader/backup-target-ca/` | `backup.target.ca_file` (refused inside `config`, Q-HA-146) | the backup pods and the restore Job |
| `secrets.valkey` | `username` (omit with `usernameKey: ""`), `password` | `/var/run/secrets/quader/valkey/` | `valkey.username_file`, `valkey.password_file` | every Deployment |
| `secrets.valkeySentinel` | `username` (default: none), `password` | `/var/run/secrets/quader/valkey-sentinel/` | `valkey.sentinel.username_file`, `valkey.sentinel.password_file` | every Deployment |
| `secrets.valkeyCA` | `ca.crt` | `/var/run/secrets/quader/valkey-ca/` | `valkey.tls.ca_file` | every Deployment |
| `secrets.valkeyClientCert` | `tls.crt`, `tls.key` | `/var/run/secrets/quader/valkey-client/` | `valkey.tls.cert_file`, `valkey.tls.key_file` | every Deployment |
| `secrets.breakGlass` | `password` (the Secret's `key`) | `/var/run/secrets/quader/break-glass/` | `auth.break_glass.password_file` (also refused inside `config`); without the Secret `auth.break_glass.enabled: false` | `api` pods (main, pools, endpoint pools); never `packages`, `worker`, the Job or backup pods (R-AUTH-014, R-AUTH-042, Q-HA-79) |
| `secrets.oidc[]` (`providerId`, `existingSecret`, `key`) | `client-secret` (the entry's `key`) | `/var/run/secrets/quader/oidc-<providerId>/` | `auth.oidc.providers.<providerId>.client_secret_file` (refused inside `config` for a listed provider) | `api` pods only (R-AUTH-004, R-AUTH-042, Q-HA-79) |
| `secrets.metricsToken` (`config.metrics.auth: static`) | `token` | `/var/run/secrets/quader/metrics/` | `metrics.bearer_token_file` | every Deployment |
| `trustedCA` (`existingSecret` or `existingConfigMap`, `keys`) | the names in `trustedCA.keys` (default `ca-bundle.crt`) | `/etc/quader/ca/` | appended to `proxy.outbound.tls.ca_files` after the operator's own; with `useForObjectStore` the one file is the `ca_file` of every connection whose entry has no `ca` | every pod of the release: the Deployments and all Jobs ([Q-HA-272](../spec/ha-and-helm.md#q-ha-272--the-trusted-ca-bundle-is-mounted-in-every-pod-of-the-release-its-files-are-appended-to-ca_files-and-one-file-can-be-the-object-stores-ca)); the entry of `quader.secretMounts` named `trusted-ca` has `configMap` instead of `secret` for a ConfigMap source |
| `<fullname>-tls-<kind>` (the chart's Certificate) or `tls.existingSecrets.<kind>` | `tls.crt`, `tls.key`; `ca.crt` where a link verifies | `/var/run/secrets/quader/tls/` | `server.tls.certificates[0]`; `server.internal_tls.ca_file` | the pods of that kind only, see [Mounts follow the kinds](#mounts-follow-the-kinds); never a Job |
| `tls.clientCA.secretName` | `ca.crt` | `/var/run/secrets/quader/tls-client-ca/` | `server.tls.client_ca_file`, `server.tls.client_auth` | the pods that open :8080 (`api`, `packages`, their path pools), not an endpoint pool |
| `endpoints.<name>.tls.existingSecret`, each of `extraSecrets` | `tls.crt`, `tls.key` | `/var/run/secrets/quader/endpoints/<name>/<n>/` (`n` = position in `tls.certificates`) | `server.endpoints.<name>.tls.certificates` | the pods that serve the endpoint (the main Deployments of the roles its surface holds, or its pools) |
| `endpoints.<name>.tls.clientCaSecret` | `ca.crt` | `/var/run/secrets/quader/endpoints/<name>/client-ca/` | `server.endpoints.<name>.tls.client_ca_file` | the pods that serve the endpoint |

The file *names* inside a mount are fixed (`username`, `password`, `ca.crt`, …); the Secret's key
names are the values (`usernameKey`, `passwordKey`, …). The encryption keys are the exception:
file name equals Secret key, because the file name without `.key` is the key ID (R-ARCH-021).

## The signing identity

Requirement: R-HA-071 ([Q-HA-385](../spec/ha-and-helm.md#q-ha-385--the-signing-identitys-secret-reaches-the-worker-pods-only-and-the-chart-renders-its-certificate-on-request));
the binary's side is [signatures.md](signatures.md#stage-2-quader-signs) (R-SIG-046). At most one source: `signing.identity.existingSecret`
(an existing Secret with `tls.crt`, `tls.key` and `ca.crt`), or `signing.identity.certManager.issuerRef.name`, for which
[signing-certificate.yaml](../../deploy/helm/quader/templates/signing-certificate.yaml) renders the cert-manager `Certificate`
`<fullname>-signing` (ECDSA P-256 with `rotationPolicy: Always`, the usages `digital signature` and `code signing`, `config.server.base_url`
as the first URI, then `certManager.uris`). [_signing.tpl](../../deploy/helm/quader/templates/_signing.tpl) holds the source
(`quader.signingSource`), the Secret's name (`quader.signingSecret`), the three files the configuration gets (`quader.signingManagedConfig`:
`config.signing.identity.cert_file`, `key_file`, `ca_file` below `/var/run/secrets/quader/signing/`) and the checks
(`quader.validateSigning`: both sources fail naming both keys; the three files under `config` fail as the chart's). `quader.secretMounts`
adds the volume `secret-signing` for the role `worker` alone, so the main worker and every worker pool mount it read-only, mode 0440,
without `subPath`, and no pod of `api`, `packages`, a scanner, the updater or a Job does. A renewal rolls nothing: the checksum follows the
rendered configuration (the paths), and the worker's watcher reads the swapped files. The issuer must be able to issue `code signing`;
cert-manager's Secret carries `ca.crt` for a CA issuer, which the mount requires. The four alerts `QuaderSigning*` render while
`config.signing.enabled` (`quader.signingOn`). Test: `TestR_HA_071_TheSigningIdentityReachesTheWorkerPodsAlone`
([signing_test.go](../../test/chart/signing_test.go)).

## TLS

Requirement: R-HA-063 (Q-HA-32, the owner's Q-HA-345, ADR-215); the binary's side is R-ARCH-035 and R-ARCH-038. Every kind of pod
(`api`, `packages`, `worker`, `vuln-scanner-trivy`, `malware-scanner-clamav`, `malware-updater-freshclam`) has a pair and a name of its own, `<fullname>-<kind>.<namespace>.svc`, plus
`<fullname>-ops.<namespace>.svc` for the scrape ([_tls.tpl](../../deploy/helm/quader/templates/_tls.tpl)). With
`tls.certManager.issuerRef.name` the chart renders a cert-manager `Certificate` per kind ([certificates.yaml](../../deploy/helm/quader/templates/certificates.yaml));
without it, `tls.existingSecrets.<kind>` names an existing Secret per kind. The value names follow the Bastion's chart. `tls.enabled`
defaults to `true`. `tls.*` describes the default client listener `:8080` and the operations listener `:8081`; each entry of `endpoints:`
carries its own `tls` block.

<details>
<summary>Values reference</summary>

| Value | Default | Effect |
|---|---|---|
| `tls.enabled` | `true` | `false` renders no TLS wiring: HTTP probes, no `appProtocol`, no mount, no Certificate, no `server.tls` (plain HTTP inside the cluster, H-38) |
| `tls.certManager.issuerRef.kind`, `.name`, `.group` | `ClusterIssuer`, `""`, `cert-manager.io` | the issuer of the Certificates; `name` is required while enabled unless `existingSecrets` names the pairs |
| `tls.certManager.duration`, `.renewBefore` | `2160h`, `720h` | the Certificates' lifetime |
| `tls.certManager.privateKey.algorithm`, `.size` | `ECDSA`, `256` | the key; `rotationPolicy: Always`; RSA adds the usage `key encipherment` |
| `tls.certManager.extraDnsNames`, `.extraIpAddresses` | `[]`, `[]` | names every pair carries besides its own (the local profile: `localhost`, `127.0.0.1` for port-forwarded tests); no check relies on them |
| `tls.existingSecrets.api`, `.packages`, `.worker`, `.vuln-scanner-trivy`, `.malware-scanner-clamav`, `.malware-updater-freshclam` | `""` | a Secret per kind with `tls.crt`, `tls.key`, `ca.crt`; refused together with the issuer, when a rendered kind has none, and when two kinds name one Secret |
| `tls.minVersion` | `"1.2"` | `server.tls.min_version` (and `ops_tls` when rendered) |
| `tls.reloadInterval` | `10s` | `server.tls.reload_interval` |
| `tls.clientCA.secretName`, `.key`, `.auth` | `""`, `ca.crt`, `require` | mutual TLS on `:8080`: `server.tls.client_ca_file` and `client_auth` (`require` or `verify_if_given`) |
| `tls.backendTLSPolicy.enabled`, `.caConfigMapName` | `false`, `""` | optional `BackendTLSPolicy` per Service for a Gateway, verifying the Service's kind name |
| `ingress.backendTLS.controller` | `haproxy` | `haproxy` (HAProxy Ingress, primary), `traefik`, `ingress-nginx` or `manual` |
| `ingress.backendTLS.verify` | `true` | the controller verifies each backend under its kind's name; `false` accepts an unverified connection |
| `ingress.backendTLS.caSecret` | `""` | haproxy, ingress-nginx: the Secret with `ca.crt`; empty is the Secret of the pair the backend serves |
| `ingress.backendTLS.clientCertSecret` | `""` | haproxy: Secret with `tls.crt` and `tls.key` the controller presents to Quader; required with `tls.clientCA` |
| `ingress.backendTLS.serversTransports.api`, `.packages` | `""` | Traefik: `<namespace>-<name>@kubernetescrd` of a `ServersTransport` per kind; both required while `verify` |

</details>

What the chart does with them:

- **Certificates.** One per kind the release renders (`quader.tlsKinds`: `vuln-scanner-trivy` only while the image scanning is on,
  `malware-scanner-clamav` only while the malware scanning is on, `malware-updater-freshclam` only while the updater renders), named and
  written into `<fullname>-tls-<kind>`, with `server auth`, and `client auth` for `worker`, `vuln-scanner-trivy`, `malware-scanner-clamav`
  and `malware-updater-freshclam`. They are ordinary objects of
  the release, applied after the Deployments by Helm's order for custom resources: on a first install the pods wait in
  `ContainerCreating` until cert-manager has written their Secret.
- **Mount.** One directory volume of the pod's own kind's Secret, `items` for the keys the process reads (`tls.crt`, `tls.key`, and
  `ca.crt` where a link verifies with it, `quader.linkNeedsCA`), read-only, mode 0440, no `subPath`. The kubelet replaces the files,
  pair and CA together, with an atomic symlink swap on a Secret update; the process polls the content every `tls.reloadInterval` and
  swaps the certificate without a restart. The migration Job and the backup and restore pods serve no listener and mount none (R-HA-030).
- <a id="mounts-follow-the-kinds"></a>**Mounts follow the kinds.** A pod mounts the Secret of its own kind and no other
  (`quader.secretMounts`, `_secrets.tpl`): the main Deployment, the path pools and the endpoint pools of `api` the `api` pair, those of
  `packages` the `packages` pair, the worker and its pools the `worker` pair, each kind of the role scanner its own (`vuln-scanner-trivy.yaml`,
  `malware-scanner-clamav.yaml`, `malware-updater-freshclam.yaml`). A pod that serves a
  further endpoint with a pair of its own mounts that pair beside its kind's (the residual credentials of a pool are H-57 in
  [endpoints.md](../spec/endpoints.md)). The client CA (`tls.clientCA`) mounts into the main `api` and `packages` pods and their path
  pools only, because the operations listener never carries it and a further endpoint has its own `clientCaSecret`.
- **Config.** `server.tls` (and `ops_tls`) are chart-managed keys: setting `config.server.tls` or
  `config.ops_tls` fails while `tls.enabled` is true. Every pod reads the same file, because the pair of each kind lies at the same
  path. `checksum/config` follows `min_version` and the paths, not the Secret content, so a certificate rotation does not roll the pods.
- **Operations listener.** Without `tls.clientCA`, `ops_tls` keeps its default (`inherit: true`): the operations listener serves the
  pod's pair. With `tls.clientCA`, the chart renders `ops_tls` with `inherit: false` and the same pair but no client CA, because the
  kubelet's probes and Prometheus present no client certificate; mutual TLS applies to `:8080` only.
- **Probes.** `httpGet.scheme: HTTPS` on port `ops`. The kubelet does not verify the certificate.
  A `probes.*.custom` probe replaces the generated one and is on its own.
- **Services.** Both ports declare `appProtocol: https`.
- **NetworkPolicy** is unchanged.
- **ServiceMonitor**: see [The ServiceMonitor](#the-servicemonitor).

<a id="adding-a-kind-of-pod"></a>**Adding a kind of pod with a pair of its own** (the malware scanner of R-HA-068 was added this way):

1. Its name: a kind that exists to run one third-party tool is `<purpose>-scanner-<tool>` (`vuln-scanner-trivy`,
   `malware-scanner-clamav`), or `<purpose>-<function>-<tool>` when it scans nothing (`malware-updater-freshclam`,
   [Q-HA-375](../spec/ha-and-helm.md#q-ha-375--the-updater-is-malware-updater-freshclam)), by the owner's rule
   ([Q-HA-372](../spec/ha-and-helm.md#q-ha-372--a-unit-that-runs-a-tool-carries-the-tools-name) and [CLAUDE.md](../../CLAUDE.md)); its value block is the name in camel case (`vulnScannerTrivy`), its template file the name
   (`vuln-scanner-trivy.yaml`). A second tool for a purpose is a kind beside the first, and what both share (the job, the queue, the
   configuration key of the worker's link, the gate) keeps the purpose's name.
2. One entry in `quader.tlsKindTable` ([_tls.tpl](../../deploy/helm/quader/templates/_tls.tpl)): `kind` (its name
   `<fullname>-<kind>.<namespace>.svc`, its Secret `<fullname>-tls-<kind>`), `client` (true when it calls a link, so its pair gets
   `client auth`), `scanClient` (true for a kind of scanner: its name joins `server.scan_clients`, which the scan listener admits) and `on`
   (when the release renders it). The Certificate, the Secrets' validation and the scan listener's list follow.
3. A key `tls.existingSecrets.<kind>` in [values.yaml](../../deploy/helm/quader/values.yaml), `values.schema.json`, the values block of
   [ha-and-helm.md](../spec/ha-and-helm.md) and the README.
4. Its Deployment mounts `quader.tlsSecret (dict "root" $ "kind" "<kind>")` at `/var/run/secrets/quader/tls/` with `tls.crt`, `tls.key` and,
   when it verifies a link, `ca.crt`, as [vuln-scanner-trivy.yaml](../../deploy/helm/quader/templates/vuln-scanner-trivy.yaml) does.
5. A listener it serves to one caller names that caller's kind (`quader.tlsName`) in its configuration; a route of the scan listener that
   serves this kind alone narrows the listener's names with `httpserver.PeerName`
   ([link_tls.go](../../internal/httpserver/link_tls.go)).
6. The chart test counts its Certificate and checks that its pods mount its Secret and no other.

### Ingress and Gateway

Quader speaks HTTPS, so the controller must connect to the backend with HTTPS and should verify
it. The settings differ per controller and the chart sets only those of the controller named in
`ingress.backendTLS.controller`. HAProxy Ingress is the default and the tested target (the
standard edge of the owner's platform); the other rows are rendered and tested for their
annotations, but not run against a live controller. The annotations are inert on a controller
that does not read them, so a wrong `controller` does not break the Ingress, it leaves the
backend on plain HTTP and every request fails with a gateway error.

| `controller` | What the chart renders | What you provide |
|---|---|---|
| `haproxy` | on the Ingress, with the default prefix `haproxy-ingress.github.io/`: `secure-backends: "true"`, and with `clientCertSecret` `secure-crt-secret`; on each Service the Ingress routes to (`quader.backendServiceAnnotations`), with `verify`: `secure-sni: <fullname>-<kind>.<namespace>.svc` (the Service's own name for a further endpoint with a pair of its own) and `secure-verify-ca-secret: <caSecret, else the Secret of the pair the Service's pods serve>`, which HAProxy Ingress reads as backend keys per Service (Q-HA-348) | for `tls.clientCA`, a Secret with `tls.crt` and `tls.key` that the CA signs. The Secrets must be in the release namespace: reading across namespaces is off in HAProxy Ingress unless `cross-namespace-secrets-ca` / `-crt` allow it. |
| `ingress-nginx` | an Ingress per kind (`quader.ingressGroups`: `<fullname>` for `api` with the `spec.tls`, `<fullname>-packages` for `packages`; a further endpoint's after its Services), each with `nginx.ingress.kubernetes.io/backend-protocol: HTTPS` and, with `verify`, `proxy-ssl-verify: "on"`, `proxy-ssl-secret: <namespace>/<caSecret, else the kind's Secret>`, `proxy-ssl-name: <fullname>-<kind>.<namespace>.svc`, `proxy-ssl-server-name: "on"` | nothing for verification (and `tls.crt`/`tls.key` when `tls.clientCA` is set: the controller presents them to Quader) |
| `traefik` | Service annotations `traefik.ingress.kubernetes.io/service.serversscheme: https` and `...service.serverstransport` from `serversTransports.<kind>` | a `ServersTransport` per kind (a Traefik CRD the chart does not create) with `serverName: <fullname>-<kind>.<namespace>.svc`, `rootCAs` from the CA, and `certificatesSecrets` for mTLS |
| `manual` | nothing | the backend protocol, through `ingress.annotations` and `service.annotations` of your controller |

The HAProxy Ingress key names are checked against its
[configuration keys](https://haproxy-ingress.github.io/docs/configuration/keys/) page
(section "Secure backend"): `secure-backends`, `secure-crt-secret`, `secure-sni`,
`secure-verify-ca-secret`, `secure-verify-hostname`, plus `backend-protocol` (`h1-ssl`, `h2-ssl`)
as the alternative to `secure-backends`. The chart uses `secure-backends` because it leaves the
HTTP version alone. A changed `--annotations-prefix` of the controller needs `controller: manual`
and your own annotations. With `verify: false` the chart renders only `secure-backends` (or the
nginx and Traefik equivalent): encrypted, but a man in the middle is not detected.

The upstream project retired ingress-nginx in March 2026 (see [ha-and-helm.md](../spec/ha-and-helm.md)
R-HA-037); it stays supported here because installations still run it.

Gateway API: `tls.backendTLSPolicy.enabled: true` renders a `BackendTLSPolicy`
(`gateway.networking.k8s.io/v1`) per Service the default Ingress names (`<fullname>-api`, `<fullname>-packages`, the path pools' Services), named as the Service and targeting its port `http`, with
`validation.hostname` and a `caCertificateRefs` entry naming a ConfigMap. Gateway API takes a
ConfigMap there, not a Secret, so the chart cannot reference the Secret's `ca.crt`; you create
the ConfigMap (a cert-manager `trust-manager` Bundle does it). The chart renders no HTTPRoute yet (Q-HA-81),
so the policies apply to routes you create yourself.

## Endpoints

`endpoints` (values) is a map of endpoint name to entry; the entry `default` tunes the default endpoint, every other entry
is a further endpoint (R-EP-008, [endpoints.md](endpoints.md)). The commented block in
[values.yaml](../../deploy/helm/quader/values.yaml) lists every key with its default; `values.schema.json` refuses unknown
keys, and the keys `default` may not set (`port`, `baseUrl`, `tls`, `pool`, `ingress`, …).

| Per further endpoint | Object | Notes |
|---|---|---|
| `port`, `baseUrl`, … | `server.endpoints.<name>` in `quader.yaml` | `listen: ":<port>"`; `pool: ep-<name>` when `pool.enabled`; keys that are unset render their defaults (a further endpoint: `surface: [packages]`, `auth` bound SAT only, `exposure: rules`, `cache_fill: false`, `rate_limit_class: external`, `replica_header: never`, `max_in_flight: 1024`) |
| | Service `<fullname>-ep-<name>` and, for a surface over both roles, `<fullname>-api-ep-<name>` | port `ep-<name>` = `port` (target port numeric), `appProtocol: https` under `tls.enabled`, `sessionAffinity: None`; the first selects the pods of the role of the surface's `packages` and `files` families (`api` when it holds neither), the second the `api` pods, each the main Deployment or the pool; `service.type`, `annotations`, `loadBalancerSourceRanges` (Q-EP-71, `quader.surfaceRoles`) |
| `pool.enabled` | Deployment, PodDisruptionBudget, HorizontalPodAutoscaler `<fullname>-packages-ep-<name>` and, with `api` families, `<fullname>-api-ep-<name>` | `serve --role packages` / `--role api`, `QUADER_POOL_NAME=ep-<name>`, label `quader/pool: ep-<name>`, the role's values with the keys of `pool` on top (merged key by key, so `false` and `0` do override); only these Deployments open the port, the main pods do not (Q-EP-70). |
| `ingress.tlsPassthrough` | the same Ingress, passed through | one rule per host with `/` to the endpoint's Service, no `tls` entries and no backend-TLS keys on the Ingress or the Service; `haproxy-ingress.github.io/ssl-passthrough: "true"` for `haproxy`, `nginx.ingress.kubernetes.io/ssl-passthrough: "true"` for `ingress-nginx` (NOTES.txt names `--enable-ssl-passthrough`), nothing for `manual`, `traefik` refused (`quader.passthroughAnnotations`, Q-EP-96) |
| `auth.clientCertificates` | `server.endpoints.<name>.auth.client_certificates` | the client certificate as a credential (R-EP-010 item 5); needs `tls.clientCaSecret`, refused on the entry `default` and beside `auth.anonymous: true` |
| `ingress.enabled` | Ingress `<fullname>-ep-<name>` | `className`, per host (the host of `baseUrl`, `hosts`, then `wildcardHost`) `/` to the one Service when the surface lies within one role, else the rows of R-HA-037's table whose families it holds (`/api/v1` and `/` to the `-api` Service), all `Prefix`, backend by port number, `ingress.tls` as given; backend-over-TLS annotations from `ingress.backendTLS`, any key of the endpoint's own `ingress.backendTLS` winning; the verified name is the kind's, or with the endpoint's own pair the Service's own `<Service>.<ns>.svc`, which that pair must cover |
| `networkPolicy.enabled` (global) | a rule in the policy of every Deployment that opens the port | its port from `networkPolicy.from` (empty = anywhere); an endpoint pool's policy holds that rule and 8081, no 8080 (Q-HA-76) |
| `tls.existingSecret`, `extraSecrets`, `clientCaSecret` | mounts and `tls.certificates` | position 0 is `existingSecret`; without it and with `extraSecrets`, position 0 is the pair of the pod's kind; without any, no block is rendered and the endpoint serves `server.tls` |

The default endpoint's `server.endpoints.default.hosts` is rendered from `config.server.base_url` and
`config.server.additional_base_urls` as soon as there is a further endpoint (unless `endpoints.default.hosts` is set), so a
request that reaches `:8080` for another endpoint's host answers `421` (H-59). A client that reaches the default endpoint by
another name (the in-cluster Service name) needs that name in `endpoints.default.hosts`. NOTES.txt (the template `quader.notes`)
lists each endpoint with its Services, hosts and pods, and warns about an endpoint with `exposure: rules` on the main
Deployments or with an empty `networkPolicy.from`.

Not rendered: `gateway.*` (HTTPRoute, BackendTLSPolicy per endpoint; the default endpoint has none either).

### The object-store connections

R-HA-072, [_storage.tpl](../../deploy/helm/quader/templates/_storage.tpl). `config.storage.connections` is the binary's list
(storage.md R-STOR-050) and reaches `quader.yaml` whole: each element with its `id` filled in (the element without one is
`default`) and the `*_file` keys the chart sets. A list given in the values replaces the default one whole, so an element carries
only what it sets; its other keys have the binary's defaults. One connection is one element with a bucket.

The Secrets of a connection are one entry of `secrets.storageConnections`, keyed by `connectionId` like `secrets.oidc[]`
([Q-HA-390](../spec/ha-and-helm.md#q-ha-390--each-connections-secrets-are-an-entry-of-one-list-keyed-by-the-connection)). Each
field is a volume `secret-s3-<id>[-location|-ca|-backup|-journal]` of its own, because the pods that read it differ:

| Entry field | Volume, mount path | Keys set in the element | Pods |
|---|---|---|---|
| `existingSecret` with `credentials_source: static` | `secret-s3-<id>`, `/var/run/secrets/quader/s3-<id>/` | `access_key_id_file`, `secret_access_key_file`, `session_token_file` | role Deployments, restore, post-restore |
| `bucketKey`, `regionKey`, `endpointKey` (keys of `existingSecret`) | `secret-s3-<id>-location`, `…/s3-<id>-location/` | `bucket_file`, `region_file`, `endpoint_file`; an empty plain key is left out | the same and the backup pods |
| `ca` | `secret-s3-<id>-ca`, `…/s3-<id>-ca/` | `ca_file` | the same and the backup pods |
| `backupSource` | `secret-s3-<id>-backup`, `…/s3-<id>-backup/` | its element of `backup.source.connections` (`quader.backupSourceConfig`, while `backup.enabled`, R-BAK-030) | the backup pods |
| `journalReader` | `secret-s3-<id>-journal`, `…/s3-<id>-journal/` | its element of the credentials file (`quader.journalCredentialElements`) | restore, post-restore with `journalCredentials.source: static` |

A provisioner writes one Secret with the credentials and the location; one entry names it and its key names
([ci/connections.yaml](../../deploy/helm/quader/ci/connections.yaml) shows `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
`S3_BUCKET`, `S3_REGION`, `S3_ENDPOINT`). The location keys are projected without the credentials into the backup pods, which read
the bucket with their own login. The binary reads the `*_file` keys once at start (R-STOR-050), so a provisioner that changes the
bucket of a Secret needs a rollout; a change of a key name changes `quader.yaml` and with it `checksum/config`. The migration Job
and the scanner pods mount nothing of a connection.

The values of the one store of before (`config.storage.s3`, `secrets.s3`, `secrets.s3CA`, `secrets.backupSource`,
`secrets.journalReader`) are refused first, by `quader.validateStorageMoved`, with the keys that took their place; the schema
declares them as description-only markers so that Helm's own schema check does not refuse them first with a message that names no
new key ([Q-HA-395](../spec/ha-and-helm.md#q-ha-395--the-values-of-the-one-store-are-refused-by-the-templates-with-the-keys-that-took-their-place)).

## The scanner

[vuln-scanner-trivy.yaml](../../deploy/helm/quader/templates/vuln-scanner-trivy.yaml) renders `<fullname>-vuln-scanner-trivy`, the role
`scanner` of the kind `trivy`, beside the units of `_units.tpl`, from the helpers of [_vuln.tpl](../../deploy/helm/quader/templates/_vuln.tpl)
(R-HA-065, R-HA-066). It shares the pod pieces (probes, topology
spread, the security context) and none of the secrets, the shared extras (`extraEnv`, `extraEnvFrom`, `extraVolumes`, `extraVolumeMounts`,
`initContainers`, `sidecars`, Q-HA-321) or the service account (Q-HA-343) of the other Deployments.

| Piece | Where | What |
|---|---|---|
| when | `quader.imageScanOn` | `config.vulnerabilities.enabled` and `config.vulnerabilities.images.enabled`, both default `true`; off renders no object of the scanner, no scan listener, no internal CA |
| the image of the analyser | `trivy.image`, `trivy.pullPolicy`, `trivy.resources` | the publisher's `aquasec/trivy` by its index digest; `TestR_HA_070_*` holds it to `TRIVY_IMAGE` of `tools/versions.env`, and Renovate moves both lines in one pull request, a new digest by itself and a new version for review (Q-HA-381); a mirror sets its own name with the same digest (Q-HA-383) |
| init container | `vuln-scanner-trivy.yaml` | `trivy` of `trivy.image`: the command `cp /usr/local/bin/trivy /opt/trivy/trivy` in place of the image's entry point, the restricted security context of every container (the pod's user 65532, which the image does not set), `trivy.resources`, the one volume `trivy-bin` read-write, no environment, Secret or configuration (R-HA-070, Q-HA-378) |
| containers | `vuln-scanner-trivy.yaml` | `quader` (`--role scanner`, the kind `trivy`, `:8080` and `:8081`; its ephemeral-storage request 1280Mi, 256 MiB of it for the analyser's copy; `vulnerabilities.images.binary` defaults to `/opt/trivy/trivy`) and `database` (`--role scanner` with `QUADER_PROCESS_SCANNER_KIND=trivy-db`, the loader of the database, `QUADER_SERVER_OPS_LISTEN=:8083`, a liveness probe on `db-ops` and no readiness probe, `vulnScannerTrivy.database.resources`, Q-HA-379) |
| volumes | `vuln-scanner-trivy.yaml` | `config`, `spool` (an `emptyDir` of `vulnScannerTrivy.spool.sizeLimit`, 6Gi, or with `spool.type: ephemeral` a claim of `spool.ephemeral.size`, as every role's), `tmp`, `secret-tls` (`tls.crt`, `tls.key` and `ca.crt` of the scanner's own Secret, `quader.tlsSecret`, in both containers), `trivy-db` (`vulnScannerTrivy.database.sizeLimit`, 5Gi, following `spool.type`; read-write in `database`, read-only in `quader`, R-HA-070, Q-HA-380), `database-tmp` (the loader's `/tmp`), `trivy-bin` (an `emptyDir` of 256Mi on the node's disk; read-write in the init container, read-only at `/opt/trivy` in `quader`, Q-HA-378) |
| environment | `vuln-scanner-trivy.yaml` | the downward API, the runtime and spool variables, `vulnScannerTrivy.extraEnv` (the container `quader` only); the loader's kind, operations listener and budget (`QUADER_VULNERABILITIES_IMAGES_DATABASE_DIR_BUDGET` from `database.sizeLimit`); nothing from a Secret |
| managed keys | `quader.scannerManagedConfig`, `quader.scannerManagedPaths` | `vulnerabilities.images.scanner.url` = `https://<fullname>-vuln-scanner-trivy.<namespace>.svc:8080`, `vulnerabilities.images.registry.url` = `https://<fullname>-packages-scan.<namespace>.svc:8082`, the names of the kinds (`quader.tlsName`: the worker requires the scanner, the scanner admits the worker, the scanner requires `packages`, and `vulnerabilities.images.registry.client_name`, the name by which the scan listener records the image scanner's report of itself, R-OPS-048), `server.scan_clients` (`quader.scanClients`: every kind of scanner of `quader.tlsKindTable`), `server.internal_tls.ca_file` = `/var/run/secrets/quader/tls/ca.crt`; set by hand they fail the render |
| the other pods | `quader.linkNeedsCA`, `quader.scanListenEnv`, `quader.scanListenPort` | the item `ca.crt` of the pod's own Secret in every `worker` pod and the main `packages` pods; `QUADER_SERVER_SCAN_LISTEN=:8082` and the port `scan` on the main `packages` Deployment only |
| checks | `quader.validateVulnScannerTrivy`, `quader.validateRoles`, `quader.validate` | the scanner's spool of Q-HA-318 (the worker's spool is no longer checked for the image scanning, ADR-229), the database volume of at least 4Gi (Q-HA-380), the replicas; the scanner's Secret with `tls.existingSecrets` |
| alert | [_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl) | `QuaderVulnerabilityImageScannerUnavailable`; `QuaderReplicasNotReady` covers `<fullname>-vuln-scanner-trivy` (Q-HA-341) |

The tests are [vuln_scanner_trivy_test.go](../../test/chart/vuln_scanner_trivy_test.go) (about 2 s together on a quiet machine, measured on 2026-11-20; the init container's assertions of R-HA-070 added about 0.2 s on 2026-11-21) and the scanner's rows of
`roles_test.go`, `chart_test.go`, `tls_test.go`, `trustedca_test.go` and `prometheusrule_test.go`; the rule tests of the alert are in
`tests/rules_test.yaml`.

## The malware scanner

[malware-scanner-clamav.yaml](../../deploy/helm/quader/templates/malware-scanner-clamav.yaml) renders `<fullname>-malware-scanner-clamav`,
the role `scanner` of the kind `clamav` with `clamd` beside it, from the helpers of [_malware.tpl](../../deploy/helm/quader/templates/_malware.tpl) (R-HA-068). Like the
scanner it shares the pod pieces and none of the secrets, the shared extras or the service account of the other Deployments.

| Piece | Where | What |
|---|---|---|
| when | `quader.malwareOn` | `config.malware.enabled` (default `true`) |
| the image | `clamav.image`, `clamav.pullPolicy` | one value for `clamd` and the updater's `freshclam`, ClamAV's `clamav-debian` `_base` image by digest; `TestR_HA_068_*` and `TestR_HA_069_*` hold it to `CLAMAV_IMAGE` of `tools/versions.env`, and Renovate moves both lines in one pull request (Q-MAL-80, Q-HA-363) |
| containers | `malware-scanner-clamav.yaml` | `quader` (`serve --role scanner`, `QUADER_PROCESS_SCANNER_KIND=clamav`) and `clamd`, which waits for `/var/lib/clamav/.quader-loaded` and then runs `clamd --foreground`; its probes run `clamdscan --ping` once the marker exists |
| volumes | `malware-scanner-clamav.yaml` | `config`, `spool`, `tmp`, `secret-tls` (the malware scanner's own Secret with the CA) in `quader`; shared: `clamd-run` (`/run/clamd`, memory, 1 MiB), `clamd-db` (`/var/lib/clamav`, `malwareScannerClamav.database.sizeLimit`, read-only in `clamd`); `clamd-tmp` (`malwareScannerClamav.clamdTmp.sizeLimit`) and `clamd-config` (`<fullname>-clamd`) in `clamd` only |
| `clamd.conf` | `quader.clamdConf` | from `config.malware.scanner` (`slots`, `max_file_size`, `max_scan_size`, `scan_timeout`, `socket`): the socket of the pod and no TCP socket, the alerts on content clamd could not read, no PUA, no concurrent reload |
| managed keys | `quader.malwareManagedConfig`, `quader.malwareManagedPaths` | `malware.scanner.url`, `.server_name`, `.client_name`, `malware.scan_listener.url`, `.server_name`, `.client_name` (Q-MAL-61), and the malware scanner's name in `server.scan_clients`; the updater's keys below; set by hand they fail the render |
| the scan listener | `quader.scanListenOn` | the scan listener of the main `packages` pods, its Service, its internal CA and its rule open while either scanning is on |
| checks | `quader.validateMalware` | `clamd-tmp` at least `slots × (max_file_size + max_scan_size)`, `clamd`'s memory limit at least 1.5 GiB, the replicas, the budget's two keys; the malware scanner's Secret with `tls.existingSecrets` |
| alerts | [_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl) | `QuaderReplicasNotReady` covers `<fullname>-malware-scanner-clamav`; `QuaderMalwareFound`, `QuaderMalwareDatabaseStale`, `QuaderMalwareScannerUnavailable` and `QuaderMalwareBacklog` (R-MAL-026) while the malware scanning is on, their `promtool` tests in `tests/rules_test.yaml` |

No worker carries anything of ClamAV: the worker reaches the malware scanner and the updater over their links only.

The tests are [malware_test.go](../../test/chart/malware_test.go) (`TestR_HA_068_*` 0.9 s, `TestR_HA_069_*` 1.7 s, measured on
2026-11-21 on a shared machine) and the malware scanner's and the updater's rows of `roles_test.go`, `chart_test.go`, `tls_test.go`,
`vuln_scanner_trivy_test.go`, `trustedca_test.go`, `endpoints_test.go` and `prometheusrule_test.go`.

## The updater

[malware-updater-freshclam.yaml](../../deploy/helm/quader/templates/malware-updater-freshclam.yaml) renders
`<fullname>-malware-updater-freshclam`, `freshclam` of ClamAV's image beside a `quader` container of the role `scanner` and the kind
`freshclam` (R-HA-069, R-MAL-010, R-MAL-032, ADR-225). It is the one pod of the release that reaches ClamAV's network; the worker asks it
for new signatures over mutual TLS.

| Piece | Where | What |
|---|---|---|
| when | `quader.malwareUpdaterOn` | `config.malware.enabled` and `config.malware.database.source` `updater` (both the defaults); `source: repository` (an installation that fills a repository of its own, R-MAL-012) renders nothing of it |
| replicas | `malwareUpdaterFreshclam.replicaCount` | default 1, at least 1, outside `highAvailability.enforce`; `strategy: Recreate`; no budget, no autoscaler, no spread constraints, not in `QuaderReplicasNotReady` (Q-HA-376) |
| containers | `malware-updater-freshclam.yaml` | `quader` (`serve --role scanner`, `QUADER_PROCESS_SCANNER_KIND=freshclam`, ports 8080 and 8081, the chart's probes) and `freshclam`, which writes `freshclam --version` to `/var/lib/clamav/.quader-tool-version`, waits for `/var/lib/clamav/.quader-seeded` and runs `freshclam --foreground --daemon`; no probe |
| volumes | `malware-updater-freshclam.yaml` | `config`, `tmp`, `secret-tls` (the updater's own Secret with the CA) in `quader`; `freshclam-tmp` and `freshclam-config` (`<fullname>-malware-updater-freshclam`) in `freshclam`; shared: `freshclam-db` (`/var/lib/clamav`, `malwareUpdaterFreshclam.database.sizeLimit`, the default of `malware.updater.directory`, which the chart therefore leaves unset); no spool |
| `freshclam.conf` | `quader.freshclamConf` | `DatabaseMirror database.clamav.net`, or `PrivateMirror` from `malwareUpdaterFreshclam.privateMirror`; `Checks 12`, `ScriptedUpdates no`, `TestDatabases no`, `Foreground yes`, `LogTime yes`, no `NotifyClamd`; `HTTPProxyServer` and `HTTPProxyPort` from `malwareUpdaterFreshclam.proxy` (Q-HA-377) |
| managed keys | `quader.malwareManagedConfig` | while it renders: `malware.updater.url` `https://<fullname>-malware-updater-freshclam.<namespace>.svc:8080`, `malware.updater.server_name` `<fullname>-malware-updater-freshclam.<namespace>.svc`, `malware.updater.client_name` `<fullname>-worker.<namespace>.svc`, `malware.scan_listener.updater_client_name` `<fullname>-malware-updater-freshclam.<namespace>.svc`, and its name in `server.scan_clients` (the kind table's `scanClient`); without TLS only the `url`, in `http` |
| Service, policy | `malware-updater-freshclam.yaml`, `quader.scanListenRule` | `ClusterIP` 8080; 8080 from the worker that claims `malware` (`quader.malwareWorkerPool`), 8081 from the ops peers; the scan listener's 8082 admits its pods for the seed and the report; `quader/serve: "true"`, so `<fullname>-ops` scrapes its metric |
| checks | `quader.validateMalwareUpdater`, `values.schema.json` | `replicaCount` at least 1; `privateMirror` an `http(s)` URL not on `clamav.net`; the shape of `privateMirror` and `proxy` (no whitespace, credentials, query or fragment; the proxy `http://<host>[:<port>]`) is the schema's (Q-HA-384); its Secret with `tls.existingSecrets` |

<a id="the-updater-and-your-network"></a>**The updater and an operator's egress policy.** The chart renders no egress rule (ADR-154,
[H-516](../security/egress.md#h-516--the-chart-restricts-no-egress-of-its-pods)). Where the cluster denies egress by default, the
updater's pods need, and only they among Quader's pods need, DNS (port 53 to the cluster's resolver) and one of: HTTPS (443) to
`database.clamav.net`, which Cloudflare serves from addresses that change (a policy by address admits Cloudflare's published ranges, a
policy by name needs an engine that matches names); the host and port of `malwareUpdaterFreshclam.privateMirror`; or the host and port of
`malwareUpdaterFreshclam.proxy`. Admit them by a label of `malwareUpdaterFreshclam.podLabels`
([security/malware-scanning.md](../security/malware-scanning.md) H-1106). The worker no longer needs a way to ClamAV's network.

## Network policy

`networkPolicy.enabled` (default `true`, R-HA-042) renders one policy per Deployment, named as the Deployment and selecting its pods by
the selector labels, plus the migration Job's. They restrict **ingress only**, and by the owner's rule (ADR-154) the chart renders
no egress rule and no policy for pods it does not deploy: the pods' egress is the cluster's, and `values.schema.json` has no
`networkPolicy.egress` key ([H-516](../security/egress.md#h-516--the-chart-restricts-no-egress-of-its-pods)). Every rule names
exactly one port.

| Policy | Admits |
|---|---|
| `<fullname>-api`, `<fullname>-packages`, each path pool | TCP 8080 from `networkPolicy.ingress.http.from` (empty = from anywhere, so any ingress controller works wherever it runs); the port of every further endpoint the pods open, from that endpoint's `networkPolicy.from` (empty = from anywhere); TCP 8081 only from `networkPolicy.ingress.ops.from` (default: namespace `monitoring`) and `networkPolicy.probeCIDRs` |
| each endpoint pool (`<fullname>-packages-ep-<name>`, `<fullname>-api-ep-<name>`) | the endpoint's port as above and 8081; no 8080 |
| `<fullname>-worker`, each worker pool | TCP 8081 as above; a worker listens on no client port |
| `<fullname>-packages` (the main Deployment), one rule more while either scanning is on | TCP 8082, the scan listener, from the pods of the kinds of the role scanner the release renders, the updater's included (`quader.scanListenRule` of [_vuln.tpl](../../deploy/helm/quader/templates/_vuln.tpl)) |
| `<fullname>-vuln-scanner-trivy` | TCP 8080 from the pods of the worker Deployment that claims the queue `vuln` (the pool that lists it, else the main worker; `quader.vulnWorkerPool`); TCP 8081 as above |
| `<fullname>-malware-scanner-clamav`, `<fullname>-malware-updater-freshclam` | TCP 8080 from the pods of the worker Deployment that claims the queue `malware` (`quader.malwareWorkerPool`); TCP 8081 as above |
| `<fullname>-migrate` | nothing; `ingress: []` |

No rule admits one Quader pod from another but those of the scanning's seams (ADR-212, ADR-221): replicas never talk to each other, and
the worker's calls to the scanners and the scanners' reads from the scan listener are the one seam between roles. A caller in
another namespace that addresses a role Service directly (a Bastion) reaches 8080 only when `networkPolicy.ingress.http.from` admits it.

With neither an `ops.from` peer nor a `probeCIDRs` entry, the 8081 rule is left out rather than rendered with an
empty `from`, which Kubernetes reads as "from anywhere". Most CNIs exempt kubelet probes from policies; where one does
not, put the node CIDRs in `probeCIDRs`. Verified by render tests only (`TestNetworkPolicy`, `TestRolesNetworkPolicy`); that a CNI enforces
the policy and still passes the probes is not tested here. The policy for the Valkey and Sentinel pods
belongs to whoever runs Valkey (R-HA-042).

`config.server.trusted_proxies` stays empty by default. The values example names the addresses of the ingress
controller pods, never the pod CIDR: every pod in the cluster could otherwise set `X-Forwarded-For` and be taken
for any client. `config.server.forwarded_header` names the one header the edge rewrites (see
[architecture.md](architecture.md#client-address-and-base-url)).

## Validation

Two layers. `values.schema.json` checks types, enums and unknown keys on install, upgrade, lint
and template. `_validate.tpl` checks the rest and fails with a message that names the value. The
tests in `chart_test.go` assert both, row by row.

<details>
<summary>R-HA-029 rows: implemented and not yet</summary>

| Row | State |
|---|---|
| `config.server.base_url`, `config.database.host`, `secrets.database.existingSecret` empty | implemented |
| the object-store connections (R-HA-072, `quader.validateStorage`): the list empty or not a list, more than 8 elements, an `id` outside `^[a-z][a-z0-9-]{0,30}[a-z0-9]$`, two elements with one `id` (also two without one, both `default`, [Q-HA-394](../spec/ha-and-helm.md#q-ha-394--an-element-without-id-is-default-in-the-chart-as-in-the-binary)); an element without `bucket` and without `bucketKey`, or with a plain `bucket`, `region` or `endpoint` beside its file; `credentials_source: static` without an entry with `existingSecret`; an entry that names no element or a connection twice, a `*Key` without `existingSecret`, an `existingSecret` nothing reads ([Q-HA-398](../spec/ha-and-helm.md#q-ha-398--what-of-an-element-the-chart-manages-and-an-entry-that-nothing-reads)); `keyed_names` on an element without `encrypted: true` (`encrypted` itself passes through, and the chart renders no key and no Secret for it, Q-HA-415); `drain: true` on every element; `storage.replication.min_copies` outside 1 to the elements without `drain`; two elements at one location (normalised endpoint, bucket, prefixes that contain each other; skipped where the bucket or the endpoint comes from a file); a key file of an element, or a `*_file` key its entry or `trustedCA.useForObjectStore` sets | implemented, each naming the element by index and id, or the entry |
| the values of the one store of before: `config.storage.s3`, `secrets.s3`, `secrets.s3CA`, `secrets.backupSource`, `secrets.journalReader` (`quader.validateStorageMoved`, checked first) | implemented, naming the keys that took their place ([Q-HA-395](../spec/ha-and-helm.md#q-ha-395--the-values-of-the-one-store-are-refused-by-the-templates-with-the-keys-that-took-their-place)) |
| endpoints: a `port` that is missing, 8080, 8081 or another endpoint's; an invalid name or a Service name over 63 characters; a missing or non-origin `baseUrl`; a host in two endpoints, a wildcard host that covers another endpoint's host or is shared; an invalid wildcard host; `ui` in a further endpoint's `surface`; `extraSecrets`, `clientCaSecret`, `existingSecret` without `tls.enabled`; a pool below two replicas or without a budget under `enforce`; `config.server.endpoints` set (managed); `auth.clientCertificates` without `tls.clientCaSecret`, beside `auth.anonymous`, or on `default`; `ingress.tlsPassthrough` without `tls.enabled` or `tls.existingSecret`, with `ingress.tls`, with a surface of both roles, or with `traefik` (R-EP-008) | implemented, each naming `endpoints.<name>.<key>` |
| `secrets.encryption.existingSecret` or `.keys` empty (prints the creation command of R-HA-027) | implemented |
| `highAvailability.enforce` with fewer than 2 replicas (`replicaCount`, or the autoscaler's `minReplicas`), a disabled PDB, a host spread that is not `DoNotSchedule` or lacks `nodeTaintsPolicy: Honor` | implemented, per Deployment (role, pool, endpoint pool) and for custom constraints |
| `api`, `packages` or `worker` below 1 replica | implemented, naming `<role>.replicaCount` (`.autoscaling.minReplicas`) |
| an autoscaler without a metric, or whose `maxReplicas` is below `minReplicas` | implemented |
| pools: a repeated name or path prefix or queue, a name starting `ep-` or too long, a path outside the families of the role or equal to the role's own prefixes, a queue that does not exist | implemented, naming `<role>.pools[<name>]` and the value |
| `config.storage.upload.max_size` larger than the spool budget of a `packages` Deployment; the UI's upload limit (`config.ui.deploy.max_upload_bytes`, `config.storage.upload.max_size` when 0) larger than that of an `api` Deployment | implemented, naming both values |
| `terminationGracePeriodSeconds` below `config.health.shutdown_drain_delay` + 15 s | implemented, per Deployment |
| a serving Deployment (`api`, `packages`, a path pool or an endpoint pool) without `resources.limits.memory`, or with a limit that admits fewer than 64 requests (Q-HA-70); a limit that is not an integer with an optional binary or decimal unit | implemented, naming the limit and the values the request bytes come from |
| Sentinel mode without addresses or `primary_name`; fewer than 3 addresses under `enforce`; standalone under `enforce` | implemented |
| `secrets.valkey.existingSecret` empty | implemented |
| `secrets.oidc[]`: an invalid or repeated `providerId`, an empty `existingSecret`, a `providerId` without a provider of that ID in `config.auth.oidc.providers`, `client_secret_file` set in `config` for it; `config.auth.break_glass.password_file` set | implemented, each naming the value |
| `config.valkey.tls.mode: disable` without `allow_insecure` | implemented |
| `podDisruptionBudget` with both `minAvailable` and `maxUnavailable` | implemented |
| a chart-managed key set inside `config` | implemented (secret file paths, `storage.spool.dir`) |
| `config.metrics.auth: static` without `secrets.metricsToken` | implemented |
| `spool.headroom` not smaller than the spool; sizes that are not quantities | implemented |
| the spool budget of a worker Deployment that claims the queue `vuln` (the main worker unless a pool lists it, or that pool) below `config.vulnerabilities.sources.osv.max_archive_size` while `config.vulnerabilities.enabled` (default true) and the OSV mode (default `online`) is not `off` (R-VULN-025) | implemented, naming the archive limit and `worker.spool.sizeLimit` |
| the spool budget of a worker Deployment that claims the queue `vuln` below 2 GiB (the unpacked database) + 512 MiB while `config.vulnerabilities.enabled` and `config.vulnerabilities.images.enabled` (both default true) (Q-HA-318) | implemented, naming the Deployment, `worker.spool.sizeLimit` and `spool.headroom` |
| the spool budget of `vuln-scanner-trivy` (`vulnScannerTrivy.spool.sizeLimit` minus `spool.headroom`) below `config.vulnerabilities.images.scanner.slots` × (`max_image_size` + 64 MiB) while the image scanning is on (Q-HA-318) | implemented, naming the three values |
| `vulnScannerTrivy.replicaCount` below 1, or below 2 with `highAvailability.enforce` (R-HA-065); `malwareScannerClamav.replicaCount` likewise (R-HA-068) | implemented |
| `malwareUpdaterFreshclam.replicaCount` below 1 while the updater renders (no minimum of 2 under `enforce`, Q-HA-376); `malwareUpdaterFreshclam.privateMirror` that is no `http(s)` URL or names `clamav.net` (R-HA-069); the shape of `privateMirror` and `proxy` (Q-HA-384) | implemented, naming the key; the shape is the schema's |
| a chart-managed key of the malware scanning's links under `config`: `malware.scanner.*`, `malware.scan_listener.url`, `.server_name`, `.client_name`, `.updater_client_name`, `malware.updater.url`, `.server_name`, `.client_name` (R-HA-068, R-HA-069) | implemented, naming the key |
| a chart-managed key of the links set under `config`: `vulnerabilities.images.scanner.url`, `.server_name`, `.client_name`, `vulnerabilities.images.registry.url`, `.server_name`, `.client_name`, `server.internal_tls`, `server.scan_listen`, `server.scan_clients` (R-HA-066) | implemented, naming the key |
| a name in `metrics.prometheusRule.disabledAlerts` that is no alert of the chart | implemented, listing the names |
| `tls.enabled` with neither `tls.certManager.issuerRef.name` nor `tls.existingSecrets` (names both and the opt-out), with both, with a rendered kind without its Secret, or with one Secret for two kinds (R-HA-063, Q-HA-347); `ingress.enabled` with TLS, verification and Traefik but no `serversTransports.api` and `.packages`; `tls.clientCA` with haproxy and no `clientCertSecret`; the same rules for the merged `ingress.backendTLS` of each further endpoint with `ingress.enabled` (`endpoints.<name>.ingress.backendTLS.serversTransports.<kind>` of the kinds its surface holds, `.clientCertSecret` with the endpoint's `tls.clientCaSecret`); `tls.clientCA`, `tls.backendTLSPolicy` without `tls.enabled`; `tls.backendTLSPolicy.enabled` without `caConfigMapName` | implemented |
| `backup.enabled` (or `restore.enabled`) without `config.backup.target.bucket` | implemented, naming the value |
| a `<fullname>` too long for a name built from it alone (`quader.validateNames`): the Service and certificate name `<fullname>-<kind>` of every kind of `quader.tlsKindTable`, rendered or not, `<fullname>-ops`, `<fullname>-packages-scan` and the Jobs `-migrate`, `-restore`, `-post-restore-<8 hex>` (63 each), the CronJob `<fullname>-backup` (52); the bound is computed, today 37 from `<fullname>-malware-updater-freshclam` ([Q-HA-374](../spec/ha-and-helm.md#q-ha-374--the-chart-refuses-a-fullname-too-long-for-the-names-it-builds-and-never-cuts-it)) | implemented, naming the name, its length, and the longest release name and `fullnameOverride` that fit |
| `backup.schedule` outside the dialect of Quader's cron parser (six or seven fields, `?`, `L`, `W`, `#`, day of week `7`, `@every`, `@reboot`, `TZ=`, `CRON_TZ=`, an unknown macro) | implemented, the reason named; a value out of range and an expression that never fires are Kubernetes's at apply time |
| the target on the bucket and endpoint of any connection (the endpoints compared as the binary does, no scheme meaning https) without `allow_same_bucket`, or with one prefix containing the other; for any configured target, the backup on or off, because every replica's configuration check refuses it too; a connection whose bucket or endpoint comes from a file is the binary's to compare | implemented, naming the target and the connection |
| `backup.enabled` with a connection of `credentials_source: static` whose entry has no `backupSource` (R-BAK-030, Q-BAK-64) | implemented, naming the connection |
| `config.backup.target.credentials.source: static` without `secrets.backupTarget.existingSecret` while `backup.enabled` or `restore.enabled` renders a Job that mounts it | implemented, naming both |
| `postRestore.enabled` without an RFC 3339 `postRestore.restoredTo`, together with `restore.enabled`, or with `journalCredentials.source: static` and a connection without drain whose entry has no `journalReader` (also for `restore`; the refusal names each, Q-HA-399); `skipRevocationJournal` with a credentials source | implemented, naming the values |
| `restore.enabled` without `restore.backupId` (its form is the command's to check); with a replica count above 0 or an autoscaler on any role, pool or endpoint pool | implemented, naming `restore.enabled` and the value; the replica minimums do not apply meanwhile (Q-HA-148) |
| a chart-managed key of the backup set inside `config` (`backup.manual_run`, `backup.target.ca_file`, the three credential files) | implemented |
| pooler (`session_host`, control endpoint), the Gateway the chart creates, `contentOrigin` | with those features |
| `gateway.enabled` without the Gateway API (`.Capabilities` lacks `gateway.networking.k8s.io/v1/HTTPRoute`; with `helm template` pass `--api-versions gateway.networking.k8s.io/v1/HTTPRoute`), without `gateway.parentRefs`, or with `gateway.timeouts.backendRequest` above `gateway.timeouts.request` (a request timeout of `0s` is no timeout and exceeds nothing); `metrics.serviceMonitor.namespace` other than the release namespace while `tls.enabled` (the CA Secret is read from the ServiceMonitor's namespace) | implemented; a timeout that is no Gateway API duration is the schema's |
| a `ReadWriteMany` spool, `--set ingress.path=…`, any unknown key | schema |

</details>

## The HTTPRoute

`gateway.enabled` renders one HTTPRoute `<fullname>` ([httproute.yaml](../../deploy/helm/quader/templates/httproute.yaml), R-HA-038) for the
default endpoint: `gateway.parentRefs` (required), `hostnames` (default the host of `config.server.base_url`), and one rule per row of
`quader.edgeRules`, the table the Ingress reads, each a `PathPrefix` match to the Service of its role on `service.port`, with
`gateway.timeouts` and `gateway.filters` on every rule and `gateway.extraRules` appended.
The Ingress and the HTTPRoute may run together. The BackendTLSPolicies are `tls.backendTLSPolicy`'s and do not depend on `gateway.enabled`.
The check for the Gateway API is a capability, so every offline render of a set with `gateway.enabled` passes
`--api-versions gateway.networking.k8s.io/v1/HTTPRoute`: `templateArgs` in the chart tests and [lint.sh](../../test/chart/lint.sh) do, and
`helm lint`, which takes no API versions, runs with `--set gateway.enabled=false` on a set that enables it ([ci/gateway.yaml](../../deploy/helm/quader/ci/gateway.yaml)).
Not built: the HTTPRoute of further endpoints (R-EP-008) and the Gateway the chart creates (R-HA-053); the criteria with Envoy Gateway in
kind (200 MB `npm publish` through the route) are not part of the render tests.
Test: `TestR_HA_038_GatewayAPIExposure`.

## The ServiceMonitor

`metrics.serviceMonitor.enabled` renders one ServiceMonitor `<fullname>` ([servicemonitor.yaml](../../deploy/helm/quader/templates/servicemonitor.yaml)):
the port `ops` of `<fullname>-ops`, path `/metrics`, `role` and `pool` relabelings first (from the pod labels `app.kubernetes.io/component`
and `quader/pool`), then `metrics.serviceMonitor.relabelings`; with `tls.enabled` `scheme: https` and a `tlsConfig` that verifies
`<fullname>-ops.<namespace>.svc` (every kind's pair carries it, Q-HA-349) against `ca.crt` of the `api` kind's Secret,
read from the ServiceMonitor's namespace; with `config.metrics.auth: static` the bearer token of `secrets.metricsToken`. Test:
`TestServiceMonitor`; the requirement's ID is carried by `TestR_HA_045_PrometheusRule` (see [The PrometheusRule](#the-prometheusrule)).

## The backup and restore Jobs

Requirements: R-HA-060 and R-BAK-012; the commands are `quader backup create` and `quader backup restore` ([backup.md](backup.md)). They are
commands of the binary and not roles (Q-ARCH-342): the pods serve nothing, open no port, have no probe, carry no `quader/serve` label (no
Service and no Valkey policy selects them) and mount no volume but `/tmp` and the extra volumes of their own values (`backup.extraVolumes`, `restore.extraVolumes`, `postRestore.extraVolumes`).

| Object | Rendered | Notes |
|---|---|---|
| CronJob `<fullname>-backup` ([backup.yaml](../../deploy/helm/quader/templates/backup.yaml)) | with `backup.enabled` (default `false`, Q-HA-17; turned on, it needs `config.backup.target.bucket`) | `quader backup create --trigger schedule`, plus `--adopt-target` with `backup.adoptTarget`. `concurrencyPolicy: Forbid` is fixed; `suspend` is `backup.suspend` or `restore.enabled` |
| Job `<fullname>-restore` ([restore.yaml](../../deploy/helm/quader/templates/restore.yaml)) | with `restore.enabled` | `quader backup restore <restore.backupId>`, `--skip-revocation-journal` with `restore.skipRevocationJournal`, then `restore.extraArgs`. An ordinary Job, not a hook. After a failure for good the operator deletes it and runs the same `helm upgrade` again, which creates it anew (R-HA-060) |
| ServiceAccounts `<fullname>-backup`, `<fullname>-restore` | with their Job; `backup.serviceAccount.*`, `restore.serviceAccount.*` | no token; the annotations carry a workload identity, which is how the target's and the blob bucket's rights differ per Job (R-BAK-015) |
| NetworkPolicies `<fullname>-backup`, `<fullname>-restore` | with their Job and `networkPolicy.enabled` | `policyTypes: [Ingress]` and `ingress: []`; the Jobs need no rule, and the target is reached outbound, which the chart never restricts (ADR-154). The counters a backup pod exports on its operations port for clusters that scrape Jobs are therefore not reachable unless the operator adds a policy |
| the pod and the failure policy ([_backup.tpl](../../deploy/helm/quader/templates/_backup.tpl)) | `quader.jobPod`, `quader.jobPodFailurePolicy` | one template for both Jobs. The policy ignores `DisruptionTarget` first, so a drain or a preemption never consumes `backoffLimit`, then fails the Job on exit 2, 3, 4 and 6 (R-BAK-013); 75 and 1 are retried. `safe-to-evict: "false"` is always set |

Secrets are scoped by the argument `scope` of `quader.secretMounts` ([_secrets.tpl](../../deploy/helm/quader/templates/_secrets.tpl)):
`backup` (the login `secrets.databaseBackup` or else `secrets.database`, the CA of the database, the blob store's read credential and CA, the
target) and `restore` (the migration login or else `secrets.database`, the blob store, the target, and the key ring that the restore checks
before it loads anything). Valkey is in neither. The files of the target are chart-managed config keys (`quader.config`), so the shared
`<fullname>-config` names them and only the two scopes have anything at those paths. The Jobs take no global `extraEnv`, `extraVolumes` or
`extraEnvFrom` (Q-HA-149): what a Job mounts is what its values and its scope say.

**TLS.** The Jobs open no listener, so they mount none of the listener material. Every link they open is TLS-capable like the replicas':
the database with `verify-full` and `secrets.databaseCA` and `databaseClientCert`, each connection with the `ca` of its entry, the target with
`secrets.backupTargetCA` (Q-HA-146) or the system trust store. All of them are directory mounts without `subPath`, so a rotated Secret
reaches the files; whether a running process re-reads a CA is the binary's, and a Job lives hours at most.

**Restore.** While `restore.enabled` is set the chart renders no migration Job (and its ConfigMap, ServiceAccount; the policy stays, without
the hook annotations), suspends the CronJob and refuses a replica or an autoscaler on any role, pool or endpoint pool, naming
`restore.enabled` and the value. The replica minimums (at least one, at least two under `highAvailability.enforce`) do not apply meanwhile,
because nothing could satisfy both (Q-HA-148). The operator's sequence is in R-HA-060; the cluster side of it (a drain during the copy, the
restored installation) is the end-to-end suite's.

**Names.** The CronJob controller names a Job `<CronJob>-<10 digits>`, so the CronJob name is limited to 52 characters; it is one row of
`quader.validateNames`, and every `<fullname>` that check accepts leaves `<fullname>-backup` at 47 at most. `kubectl create job --from=cronjob/<fullname>-backup <name>` copies the arguments, so a manual run
records `trigger: schedule`; `config.backup.manual_run.*` (chart-managed, rendered with the CronJob) names it for the UI.

**Dev profile.** [values-dev.yaml](../../deploy/dev/values-dev.yaml) sets the target to the bucket `quader-backups` of the profile's SeaweedFS
(a bucket of its own beside `quader-blobs`), static keys from the secret of the blob store, the dev CA, the schedule `0 */6 * * *` and
`config.backup.expected_interval: 6h`.

## The post-restore Job

Requirement: R-HA-025 step 4 (Q-HA-150); the command is `quader admin post-restore` ([backup.md](backup.md)). `postRestore.enabled` renders the
Job `<fullname>-post-restore` ([post-restore.yaml](../../deploy/helm/quader/templates/post-restore.yaml)) with its ServiceAccount, a policy
with `ingress: []`, and the pod template of the other Jobs (`quader.jobPod`, scope `post-restore`). It is a `post-install,post-upgrade`
hook with the delete policy `before-hook-creation`: Helm waits for it and fails the upgrade when it fails, a second `helm upgrade` replaces
it, and a finished Job stays for its log. Run it with every role at zero (step 1 of the procedure), because a replica that started before it
keeps the old keyspace until it restarts.

| Value | Effect |
|---|---|
| `postRestore.restoredTo` | required, RFC 3339; the argument `--restored-to` |
| `postRestore.skipRevocationJournal` | `--skip-revocation-journal`; refused together with a `journalCredentials.source` other than `runtime` |
| `restore.journalCredentials.source` | the same three sources for the restore Job (`--journal-credentials-file /etc/quader/restore/credentials.yaml`, ConfigMap `<fullname>-restore`), Q-HA-159 |
| `postRestore.journalCredentials.source` | `runtime`: no file, the replay lists with the blob store's credential. `default`: the chart renders `credentials.yaml` (`connections:` with an element `credentials_source: default` per connection, Q-BAK-73) into the ConfigMap `<fullname>-post-restore` and passes `--credentials-file`, so the identity of the Job's ServiceAccount lists object versions (`postRestore.serviceAccount.annotations`). `static`: the same file names the `journalReader` of each connection's entry of `secrets.storageConnections` (`/var/run/secrets/quader/s3-<id>-journal/`), one element per connection that has one, mounted in the restore and post-restore Jobs only; every connection without drain needs one (Q-HA-399) |
| `postRestore.backoffLimit`, `activeDeadlineSeconds`, `terminationGracePeriodSeconds`, `resources`, `podAnnotations`, `extraEnv`, `serviceAccount.*` | as for the restore Job |

The Job is named `<fullname>-post-restore-<8 hex digits of the sha256 of restoredTo>` and carries the annotation `quader/restored-to`, so the Jobs of two restore points stay visible side by side; a repeat of the same point is a Job of the same name, which Helm replaces and the command answers without running its steps again (Q-HA-161), and NOTES prints a reminder to set `postRestore.enabled` back to false. Every role, pool and endpoint pool must be at zero (the rule of `restore.enabled`, H1 of the audit): the hook runs after the Deployments are applied, so a replica would serve the restored database before the journal replay.

The scope `post-restore` of `quader.secretMounts` mounts the owner login (`secrets.databaseMigration`, else `secrets.database`), the CA and
client certificate of the database, the blob store's keys and CA, and the journal credential; no Valkey Secret, no key ring, nothing of the
backup target. Validation: `postRestore.restoredTo` must be an RFC 3339 time, `postRestore.enabled` with `restore.enabled` is refused (a
backup restore runs the clean-up itself), `static` needs the `journalReader` of the entry of every connection without drain. Test: `TestR_HA_025_PostRestoreJob`.

## The values reference

The values block of [R-HA-031](../spec/ha-and-helm.md) is the reference; `TestR_HA_031_ValuesReference` ([values_reference_test.go](../../test/chart/values_reference_test.go))
holds it to [values.yaml](../../deploy/helm/quader/values.yaml): every key of values.yaml is in the block with the same default, every key of the
block is in values.yaml unless the feature that owns it is not rendered yet (`unbuiltPrefixes` in the test: the chart-created
Gateway, `contentOrigin`, `bootstrap`, Service tuning, the extension points, and the keys under `config`), and
[values.schema.json](../../deploy/helm/quader/values.schema.json) declares every key. A value added to the chart is added to the block in the
same change; a feature that arrives takes its prefix out of `unbuiltPrefixes`.

## The PrometheusRule

`metrics.prometheusRule.enabled` renders one PrometheusRule `<fullname>`, group `<fullname>`: every alert of
[_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl) except `disabledAlerts`, then `extraRules`, with `labels` on the object. With
`backup.enabled` the rule holds the 37 alerts that render always, the four of each of the malware scanning, the signatures and the signing identity while their switch is on, and the six of the backup runs; without the backup those six are left out (Q-HA-17),
because with no CronJob no run succeeds and `QuaderBackupStale` would fire one interval after every installation that leaves the backup
off. A name of the six stays a valid entry of `disabledAlerts` either way. The order: `QuaderReplicasNotReady`, `QuaderDatabaseUnreachable`,
`QuaderObjectStoreUnreachable` (every connection down on every replica), `QuaderObjectStoreConnectionUnreachable` (one of several down, Q-HA-391),
`QuaderObjectStoreUnreachableSingleReplica` (per connection), `QuaderStorageReplicationBacklog` (not for a filling connection,
[Q-HA-393](../spec/ha-and-helm.md#q-ha-393--the-replication-backlog-alert-leaves-a-filling-connection-out-by-a-gauge-of-its-own)), `QuaderStorageCopyDamaged`,
`QuaderStorageScrubBehind`, `QuaderStorageConnectionDetached` (R-STOR-060), `QuaderStorageUnknownKey` (records of storage keys the database lacks, never `destroyed_key`, R-STOR-088), `QuaderDatabasePoolSaturated`, `QuaderControlConnectionWaits`, `QuaderJobsStuck`, `QuaderLeaseLost`,
`QuaderMixedVersions`, `QuaderSpoolNearlyFull` (rows of R-HA-045's table), `QuaderVulnerabilityDataStale`, `QuaderVulnerabilitySyncFailing`,
`QuaderVulnerabilityRecordsRejected`, `QuaderVulnerabilityRecordsInvalid`, `QuaderVulnerabilityMaliciousRecordRejected`, `QuaderVulnerabilityClaimedNameMalicious` (R-VULN-024, Q-VULN-122), `QuaderMirrorFailing`, `QuaderMirrorStale` (R-OPS-046, Q-OPS-160, Q-OPS-167),
`QuaderBlobMissing` (a read found the object of a blob missing within the
hour, R-HA-024, Q-STOR-152), `QuaderBlobCorrupt` (no connection holds a blob whole, R-STOR-052), `QuaderGCStale`, `QuaderBackupCheckFailed` (R-HA-023), `QuaderRevocationJournalBacklog` (R-HA-054, per connection), `QuaderRevocationJournalQuarantined` (a row set aside, Q-HA-162), `QuaderValkeyUnavailable`, `QuaderValkeyUnavailableSingleReplica`,
`QuaderValkeySentinelsDegraded`, `QuaderValkeyFailoverFlapping`, `QuaderValkeyPrimaryDisagreement` (rows of the table), `QuaderBackupStale`,
`QuaderBackupFailed`, `QuaderBackupRunningLong`, `QuaderBackupGlobalHoldLong`, `QuaderBackupSourceDamage` and `QuaderBackupVerifyFindings` (R-BAK-014). Severities are in the spec's table (`critical`:
`QuaderReplicasNotReady`, `QuaderDatabaseUnreachable`, `QuaderObjectStoreUnreachable`, `QuaderValkeyUnavailable`, `QuaderValkeyPrimaryDisagreement`,
`QuaderVulnerabilityMaliciousRecordRejected`, `QuaderVulnerabilityClaimedNameMalicious`, `QuaderStorageUnknownKey`, `QuaderBlobMissing`, `QuaderBlobCorrupt`, `QuaderBackupCheckFailed`, `QuaderRevocationJournalQuarantined`, `QuaderBackupStale`, `QuaderBackupFailed`,
`QuaderBackupSourceDamage`, `QuaderBackupVerifyFindings`; the others `warning`).

**The platform check.** `QuaderBackupCheckFailed` is `min(quader_backup_check_ok) == 0` for an hour. The gauge is exported by every replica
from the report that the task `backup-check` stored in `system_state` (Q-OPS-185, [backup.md](backup.md#the-platform-check)); a protection
that is not declared (`config.backup.recovery_window`, `config.backup.object_versioning`) or that the credential may not read has no series, so
the alert is silent on an installation that relies on Quader's backup alone. With this alert every row of R-HA-045's table is rendered and
tested, and `TestR_HA_045_PrometheusRule` carries the requirement's ID.

**The alerts of the backup runs** read the gauges that every `api` and `worker` replica computes from `backup_run` in PostgreSQL
(R-BAK-014), so a finished backup pod need not be scraped. The two timestamps have no series until a run of that outcome exists
(Q-OPS-180), and the expressions handle the absence: `QuaderBackupFailed` is `max(last_failure) > (max(last_success) or vector(0))`, silent
without a failure and firing for a first backup that failed; `QuaderBackupStale` is `time() - max(last_success) > 1.5 * max(expected_interval)`
or, when no success series exists, `max(absent(last_success))` while the gauge of the expected interval has existed for a whole
`config.backup.expected_interval` (`offset`, Q-OPS-182), so a young installation is silent and one that never succeeded fires one interval
after the replicas first reported. The window is a literal in the rule, rendered from `config.backup.expected_interval` (default `24h`, parsed
by `quader.seconds`), the same value the replicas export; the chart has no threshold of its own. An `interrupted` run leaves no failure
series, so a drain followed by a successful retry never fires `QuaderBackupFailed`.

**Names.** The expressions are the specification's, and the binary exports every metric they read under that name (Q-HA-135): the
spool gauges are `quader_spool_used_bytes` and `quader_spool_free_bytes`, the pool metrics are in
[internal/meta/postgres/metrics.go](../../internal/meta/postgres/metrics.go) and
[internal/app/wire_gauges.go](../../internal/app/wire_gauges.go), the queue ages in the `JobQueueGauges` query, the storage summary's gauges
(`quader_storage_copies` and the backlog and fill per connection) in [internal/blob/summary/gauges.go](../../internal/blob/summary/gauges.go).
`TestR_HA_045_PrometheusRule` ([prometheusrule_test.go](../../test/chart/prometheusrule_test.go)) fails for an expression that reads a
`quader_*` name no code under `internal/` registers; since package F of the several connections registered the last of the object-store
alerts' series, it has no exception.

- `QuaderReplicasNotReady` reads kube-state-metrics: `min by (deployment)` of `kube_deployment_status_replicas_available` for the Deployments
  `<fullname>-(api|packages|worker)(-.*)?`, the main Deployments and their pools (`<fullname>-<role>-<pool>`); a Deployment
  that is scaled to one replica by choice fires too.
- Every selector carries `namespace="<release namespace>"`, so two installations in one Prometheus do not blend (Q-VULN-55). A scrape
  through the Prometheus operator always sets that label.
- The stale threshold is `config.vulnerabilities.stale_after` (default `24h`, parsed by `quader.seconds`), the key the binary reads; a
  `stale_after` that arrives only through `extraEnv` is not seen.
- A rule file renders no object when every alert is disabled and `extraRules` is empty.
- To add an alert: append it to `quader.rules.all` and its name to `quader.rules.names`, add a test that fires and one that stays silent to
  [tests/rules_test.yaml](../../deploy/helm/quader/tests/rules_test.yaml) (`TestR_VULN_024_PromtoolRules` fails otherwise; the expected
  annotations are the rule's text, so a changed text fails the test until the test file follows), a row to `alertSeverities` in
  [prometheusrule_test.go](../../test/chart/prometheusrule_test.go) and a row to R-HA-045.

[tests/rules_test.yaml](../../deploy/helm/quader/tests/rules_test.yaml) is rendered from `ci/monitoring.yaml` as release `quader` in the
namespace `default` and run with the pinned promtool (`make chart-test` fetches it, see [The Bastion chart](#the-bastion-chart)).

## Value sets and tests

`ci/*.yaml` are the value sets every requirement is asserted against. Each names `tls.certManager.issuerRef.name` or `tls.existingSecrets` (`ha-ingress.yaml`) or sets `tls.enabled: false` with a comment (`single-node.yaml`). The dev profile's
[values-dev.yaml](../../deploy/dev/values-dev.yaml) is rendered alongside them (E0.7). Adding a
file to `ci/` adds it to every per-set assertion with no code change.

[examples/production.yaml](../../deploy/helm/quader/examples/production.yaml) is not a `ci/` set: it is the file an operator copies, and every host, bucket, namespace and
Secret name in it is an example. [test/chart/production_test.go](../../test/chart/production_test.go) renders it and asserts, per Deployment (the three roles and the pool `vuln`), the
PodDisruptionBudget, the two topology spread constraints with `nodeTaintsPolicy: Honor` and the HorizontalPodAutoscaler that owns the replica count
(`TestR_HA_032_ProductionExample`); the ingress policies that name the Ingress controller's, the Gateway's and the monitoring namespace
(`TestR_HA_042_ProductionExample`); the ServiceMonitor, the PrometheusRule with every alert of R-HA-045 and the labels the Prometheus selects on
(`TestR_HA_045_ProductionExample`); and the HTTPRoute beside the Ingress with a BackendTLSPolicy per Service (`TestR_HA_038_ProductionExample`). `make chart-lint` lints and renders it with the `ci/` sets.

| Command | What it does |
|---|---|
| `make chart-lint` → [test/chart/lint.sh](../../test/chart/lint.sh) | `helm lint --strict` and `helm template` for every `ci/` set, `examples/production.yaml`, `values-dev.yaml`, and `values-dev.yaml` with `values-pooler.yaml` |
| `make chart-test` → [test/chart/test.sh](../../test/chart/test.sh) | the Go package [test/chart](../../test/chart/chart_test.go) with build tag `chart`; writes `.out/test-chart.json` for the coverage generator |
| `go test -tags chart ./test/chart/...` | the same, without the wrapper |
| [test/chart/image_test.go](../../test/chart/image_test.go) | the default image is `docker.io/guidedtraffic/quader` at the appVersion, and `values.yaml` has one `  tag:` line, which the release pipeline rewrites |

Both need only `helm` at the version in `tools/versions.env` (the test binary and `lint.sh` fail
with the install command when it differs) and no cluster. The tests run `helm template` and parse
the output; they are named after the requirement they verify (`R-HA-002 …`, `R-HA-061 …`), which
is where the coverage generator reads the IDs from.

| Test (subtest prefix) | Asserts |
|---|---|
| `R-HA-043` (`TestR_HA_043_TrustedCA`) | the bundle of a Secret or a ConfigMap is mounted without `subPath` at `/etc/quader/ca/` in every Deployment and Job, its files follow the operator's `ca_files`, `useForObjectStore` sets the `ca_file` of every connection without a `ca` of its own, PostgreSQL's CA stays apart, the refusals name the value |
| `R-HA-050` (`TestR_HA_050_SizingProfiles`) | each file of `profiles/` renders on [operator-minimum.yaml](../../test/chart/testdata/operator-minimum.yaml) and the rendered replicas, autoscalers, resources, spool and `max_conns` equal the row of the table in the specification |
| `R-HA-028` | lint passes for every set; no Secret, RBAC, CRD or subchart; names follow the fullname rule; standard labels |
| `R-HA-002` | every Deployment runs the `--role` its name says (`api`, `packages`, `worker`), never `web` or `all`; `api`/`packages`/`worker` replicas below 1 fail naming the key; each role Service selects the main pods of its role; the pool label is in the selectors |
| `R-HA-002`, `R-HA-028`, `R-HA-062` (`TestRolesDeployments`) | Deployment, PodDisruptionBudget and NetworkPolicy per role, pool and endpoint pool; component and pool labels; every selector selects the pods of its Deployment and no other; the queue variables (JSON lists) on the right Deployments; pool inheritance; the autoscaler leaves out `spec.replicas`; the connection counts and the budget in the notes |
| `R-HA-002`, `R-HA-028` (`TestRolesServices`) | each Service selects the pods of exactly its Deployment, sessionAffinity None, ports; the container ports of every Deployment |
| `R-HA-037`, `R-HA-062`, `R-EP-008` (`TestRolesEdge`) | the six fixed `Prefix` rules per host, the pool paths before them, a further endpoint whole or by the rows of its families, a mixed surface opens its port on both main Deployments |
| `R-HA-042` (`TestRolesNetworkPolicy`) | in every value set: ingress only, no `egress`, every rule one port, no empty `from`; per Deployment the ports it admits and their peers; the default policies |
| `R-HA-029`, `R-HA-062` (`TestRolesValidation`) | every pool, autoscaler and spool row fails and names the value |
| `R-HA-061` | no `ReadWriteMany`, `ReadOnlyMany`, PVC, StatefulSet, `volumeClaimTemplates`, hostPath; a `ReadWriteMany` spool fails schema validation; only the spool and `/tmp` are writable |
| `R-HA-029` | one subtest per implemented row fails and names the value; the schema refuses unknown keys and wrong types |
| `R-HA-029` and `R-HA-027` | without the encryption Secret the error prints the `kubectl create secret` command |
| `R-HA-027` | all pods mount the same key Secret; ordered ring, first is active; a key-list change rolls the pods |
| `R-HA-030` | file mount, `checksum/config`; Secrets read-only with items, mode 0440, no `subPath`, never in the environment; per-pod scoping; the config paths match the mounts; the environment set; service links off |
| `R-HA-033` | every pod and container of every set satisfies the restricted Pod Security Standard; UID 65532 |
| `R-HA-034`, `R-ARCH-024` | probes as the table of R-HA-034; ports; no `preStop` |
| `R-HA-011` (`TestChart`) | the chart half: grace period per role, its environment variable, above the drain delay |
| `R-HA-011` (`TestShutdownBudgetDerivation`) | builds the binary and runs `quader admin config validate` over the rendered file and environment of `packages` and `worker`: `server.shutdown_timeout` equals grace - drain - 5 s and its source is `derived` |
| `R-HA-042` (`TestNetworkPolicy`) | the policies of the roles: ports, peers, selectors that match the pods, no `from` meaning "anywhere" on 8081, `enabled: false` renders none |
| `R-HA-030` (`TestMigrationJobSecrets`) | the migration Job mounts no key ring; `api`, `packages` and `worker` do |
| `R-ARCH-006` (`TestTrustedProxiesExample`) | the `trusted_proxies` example is no pod-wide range and the default list is empty |
| `R-HA-035`, `R-HA-036` | spool `sizeLimit` and budget; `GOMAXPROCS` rule; default resources |
| `R-VULN-025` (`TestR_VULN_025_SpoolHoldsOneArchive`) | the worker spool must hold one archive: the defaults pass, a larger `max_archive_size` fails naming both values (also in mode `repository`), the headroom counts, mode `off` and a disabled scanning are not checked, the worker's `QUADER_STORAGE_SPOOL_MAX_BYTES` follows its own `sizeLimit` |
| `R-HA-070` (`TestR_HA_070_TheLoaderWritesTheDatabaseThatTheScannerReadsReadOnly`) | the init container `trivy` of `trivy.image`, equal to `TRIVY_IMAGE` of `tools/versions.env`, with the copy's command, the restricted security context, its resources and the one volume `trivy-bin` read-write; `trivy-bin` (256Mi) and `trivy-db` read-only in `quader` and its request of 1280Mi; a mirrored image with another pull policy and `global.imagePullSecrets` on the pod; an image without a tag refused; the container `database` of the kind `trivy-db` with `:8083`, liveness only, its mounts (`trivy-db` read-write, the scanner's Secret, its own `/tmp`), the volume following `spool.type`, the budget from `sizeLimit`, a `sizeLimit` below 4Gi refused; nothing of it with the image scanning off, and no init container, `trivy-bin` or `trivy-db` in another pod |
| `R-VULN-025` (`TestR_VULN_025_ConfigurationPassesThrough`) | the `vulnerabilities` section reaches `quader.yaml` as set; `ci/monitoring.yaml` passes `quader admin config validate` with the other sets |
| `R-VULN-024` (`TestR_VULN_024_PrometheusRuleValues`) | no rule by default; the alerts (the three of the vulnerability scanning, the two of the mirror, `QuaderBlobMissing`) and their severities; the `labels`; the threshold follows `stale_after`; every selector is scoped to the namespace; `disabledAlerts` and `extraRules`; a misspelt alert name fails; all alerts disabled renders nothing |
| `R-HA-045` (`TestR_HA_045_PrometheusRule`) | the rule is rendered with `enabled` and carries the `labels`, none by default or with `enabled: false`; the 55 alerts and their severities, `for` durations, summaries and descriptions; the role regex follows `<fullname>`; the expressions of the three rows with a special shape; every `quader_*` name an expression reads is registered by code under `internal/`; `disabledAlerts` |
| `R-HA-045` (`TestR_HA_045_TheDocumentsCountTheAlerts`) | the numbers of alerts that the README and this page state equal what [_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl) ships: all its alerts, and those outside a switch; it reads the files and renders nothing (milliseconds) |
| `R-VULN-024` (`TestR_VULN_024_PromtoolRules`) | `promtool check rules` and `promtool test rules` over the rendered rules, and one firing and one silent test per alert; the backup alerts' tests are `QuaderBackupStale` at 90 minutes after the last success (and for a backup that never ran), `QuaderBackupFailed` for a failure after the last success and for a first failure (silent after an interrupted run), `QuaderRevocationJournalBacklog` and the others |
| `R-HA-060`, `R-BAK-012` (`TestR_HA_060_BackupCronJob`) | the CronJob's fields, `Forbid`, the pod failure policy (an eviction ignored first, then 2, 3, 4, 6), `restartPolicy: Never`, no `activeDeadlineSeconds` by default, the grace period, the autoscaler annotation, `TMPDIR` and the one `emptyDir`, the memory limit, no port or probe, its own ServiceAccount, the values (rendered with `backupSets`, which turns the backup on); a value set renders the CronJob exactly when it sets `backup.enabled: true` |
| `R-HA-060`, `R-HA-045` (`TestR_HA_060_BackupOffByDefault`) | the default: `backup.enabled: false` in values.yaml, no CronJob, ServiceAccount, NetworkPolicy or `backup.manual_run` and no target bucket needed; the target's static keys required only while a Job mounts them; a restore with the backup off; the note in `NOTES.txt`; the six alerts of the backup runs only with the backup |
| `R-HA-060` (`TestR_HA_060_RestoreJob`) | the Job (not a hook), the arguments, the failure handling, the account, no migration Job, the suspended CronJob, the roles and pools at zero |
| `R-HA-029` (`TestR_HA_029_BackupValidation`) | every backup and restore row of the table fails and names the value; the schedules are checked against `jobs.ParseSchedule` |
| `R-HA-031` (`TestR_HA_031_BackupValuesSchema`, `TestR_HA_031_ValuesReference`) | the schema refuses typos and a grace period below 30 s; the values of the spec block render; values.yaml and the spec block agree |
| `R-HA-030`, `R-BAK-011` (`TestR_HA_030_BackupSecrets`) | which Secret reaches which pod: the target's keys and CA only into the backup pods and the restore Job, no replica and not the migration Job, no Valkey Secret in a Job, the key ring only in the restore Job, the login of each Job; no Secret as a variable; the managed config paths |
| `R-HA-025` (`TestR_HA_025_PostRestoreJob`) | the hook annotations, the arguments, the account, the policy, the scoped Secrets (the journal credential reaches this Job only), the three credential sources, the validation rows |
| `R-HA-059` (`TestR_HA_059_NoDisruptionBudgetForTheJobs`) | no PodDisruptionBudget selects a Job's pods |
| `R-HA-042` (`TestR_HA_042_JobsAdmitNothing`) | the Jobs' policies: ingress only, an explicit empty list, selecting exactly their pods; no egress in any set |
| `R-HA-029` (memory) | a serving pod (`api`, `packages`, a pool) without a memory limit, or one whose memory slots (`quader.requestBytes` and the three cache budgets, the formula of `internal/app/memory.go`) are fewer than 64, fails; `2G`, `64Mi` and a plain integer render |
| `R-HA-032` | PDB, spread, tolerations, default replica counts and rolling update |
| `R-HA-037`, `R-ARCH-032` | `sessionAffinity: None` everywhere, no affinity annotations, every Ingress rule `Prefix` and never the operations port, `ingress.path` fails naming `path` |
| `R-HA-019` | hook annotations and weights, `podFailurePolicy`, same `quader.yaml`, nothing from Valkey or S3; disabled renders no Job |
| `R-HA-056` | Valkey wiring reaches `api`, `packages` and `worker` only |
| `R-EP-008` (`TestEndpoints`) | two endpoints render two Services (own port, selecting the main packages pods), two Ingresses (class, hosts incl. the wildcard, TLS entry, backend), the port rules in the packages policy, the `server.endpoints` block and the default endpoint's hosts; mounts of the pairs without `subPath`; backend TLS per controller; the pool (Deployment of the role, PDB, Service, spread selector, policy with the operations port, no port on the main pods); NOTES; each validation row |
| `R-HA-072` (`TestR_HA_072_OneConnection`) | one element with a bucket renders that element with its `id` and no pod mounts an object-store Secret; static keys mount `s3-default` with the two keys into `api`, `packages`, `worker` and set the element's two files, the backup pod mounts the read login instead; key names and a session token are projected under the chart's file names; a `ca` is mounted as `ca.crt` in the roles and the backup pod |
| `R-HA-072` (`TestR_HA_072_SeveralConnectionsAndAProvisioner`, on `ci/connections.yaml`) | a provisioner's Secret renders `bucket_file`, `region_file`, `endpoint_file` and leaves the plain keys out; the writing pods mount both connections' keys, every reading pod the location and the CA, the backup pod no runtime key, the migration Job and the scanners nothing; an element of `backup.source.connections` per connection with its read login; the journal reader of every connection in the restore Job only, the credentials file naming each, and the refusal of a connection without one (Q-HA-399) |
| `R-HA-072`, `R-HA-029` (`TestR_HA_072_Refusals`, `TestR_HA_072_MovedValues`, `TestR_HA_072_AnInstallationMoves`) | every refusal of R-HA-072 names the value and the connection; the values of the one store of before fail naming the new keys; an installation's values of before fail and its values of after render the same store |
| `R-HA-030` (`TestTLSMountsFollowTheListeners`) | the listener pair is mounted by the pods that load it: everywhere without `tls.ops`; with it, not into the worker, into an endpoint pool only while an endpoint it serves has no pair of its own; the client CA reaches the main `packages` pods and not an endpoint pool |
| `R-HA-030`, `R-AUTH-014`, `R-AUTH-042` (`TestLoginSecrets`) | `secrets.breakGlass` and `secrets.oidc[]` mount a directory of the Secret read-only without `subPath` into `api` pods only (main, pools, endpoint pools; not `packages`, `worker` or the Job), render the config paths, take a different key, leave `enabled` alone with a Secret and render `auth.break_glass.enabled: false` without; the validation rows; the rendered file passes `quader admin config validate` |
| `R-HA-063`, `R-HA-030` (`TestTLS`, `TestTLSMountsFollowTheListeners`) | the Certificate per kind with its names and usages; each kind mounts its own Secret only, without `subPath`; neither issuer nor Secrets, both, or one Secret for two kinds fail; HTTPS probes, config paths, `appProtocol`; the edge's name per Service, an Ingress per kind for ingress-nginx; opt-out renders no TLS wiring; no TLS mount in the migration Job; client CA and `ops_tls` separation; per-controller Ingress settings (HAProxy Ingress default, verification and client-certificate rules); one `BackendTLSPolicy` per role Service; every value set agrees between config and probe scheme |

Not covered by these tests (needs a cluster): that the pods start in a restricted namespace, that
`kubectl exec … touch /x` fails, that the Job finishes before the Deployments roll. The dev
profile covers them once the Quader image serves ([local-profile.md](local-profile.md)).

## Extension checklists

**Add a value.**

1. Add it to [values.yaml](../../deploy/helm/quader/values.yaml) with its default; comment it only where its name does not
   say its meaning, unit, range or consequence, and give an `example:` where the default is empty.
2. Add it to [values.schema.json](../../deploy/helm/quader/values.schema.json) with its type and
   enum. The schema is `additionalProperties: false`; a value missing there fails every render.
3. Use it in the template, and add a validation row to `_validate.tpl` if a wrong value must fail.
4. Add a subtest to `chart_test.go` named with the requirement ID.
5. Update the tables on this page.

**Add a Secret.** (For TLS material, see [TLS](#tls).) Add an entry in `quader.secretMounts` (`_secrets.tpl`) with its scope, the config
key it sets in `quader.config`, the value in `values.yaml` and the schema, and the managed-key
check in `_validate.tpl`. Extend `R-HA-030 only the pods that need a Secret mount it`.

**Add an object.** New template file, label it with `quader.labels`, take its name from
`quader.fullname`, and extend `R-HA-028 names follow the fullname rule`. Check `R-HA-061` still
passes: no PVC, no StatefulSet.

**Add a role or pool.** Both roles come from one loop in `deployments.yaml`, `pdb.yaml` and the
validation; extend the loops, not a copy. Selector labels are immutable: a new selector label
needs a migration plan for the existing Deployments.

## The Bastion chart

The second chart, [deploy/helm/quader-bastion](../../deploy/helm/quader-bastion), deploys the Bastion: `quader serve --role bastion`,
a read-only, internet-facing cache in front of a main installation ([spec/bastion.md](../spec/bastion.md), R-BAS-001, R-BAS-002,
R-BAS-025, R-BAS-030, R-BAS-033, R-BAS-038, R-BAS-040 to R-BAS-047; the security view is
[security/bastion.md](../security/bastion.md)). It is separate from the chart `quader` on purpose (Q-BAS-5): no PostgreSQL, no
object store of the main installation, no migration Job, no second role, no Secret, no Role and no RoleBinding. The helper
names are `quader-bastion.*` (`_helpers.tpl`), the idioms are those of the chart `quader`: `<fullname>` (release name, or
`<release>-quader-bastion`, cut at 50 characters), the secret mount list used for volumes and mounts, the fail-fast
validation, restricted pod security, probes on the ops port, `extraObjects` through `tpl`.

```text
deploy/helm/quader-bastion/
├── Chart.yaml              version = appVersion = 0.0.0-dev (build.yml rewrites both with the release version)
├── values.yaml             the block of R-BAS-046, copied there verbatim
├── values.schema.json      draft-07, additionalProperties false outside config; config = the generated role schema (below)
├── chart-managed-keys.yaml generated: the config paths the chart renders and refuses (below)
├── ci/                     value sets the tests render (not packaged, see .helmignore)
│   ├── required-only.yaml      the five values a render needs (Q-BAS-55)
│   ├── bucket-valkey.yaml      bucket backend, Sentinel Valkey, trust bundle, tracing, ServiceMonitor, PrometheusRule
│   ├── persistent-routed.yaml  StatefulSet with a claim template, routing by path, edge-limit whitelist
│   ├── autoscaling.yaml        HPA, bastion.replicas = autoscaling.maxReplicas
│   ├── existing-tls.yaml       tls.existingSecret, no Certificate, emptyDir cache, CA bundle from a Secret
│   └── plaintext.yaml          tls.enabled: false, no edge limit, no Ingress, one replica
├── tests/rules_test.yaml   promtool test rules for the PrometheusRule (not packaged)
└── templates/
    ├── _helpers.tpl        names, labels, image, config merge, secret and ConfigMap mounts, probes, topology spread
    ├── _validate.tpl       fail-fast validation (R-BAS-045), managed-key refusal (R-BAS-002)
    ├── _rules.tpl          the alerts of R-BAS-041 and the list of their names
    ├── _notes.tpl          NOTES.txt
    ├── workload.yaml       Deployment, or StatefulSet with cacheVolume.type: persistent
    ├── configmap.yaml  services.yaml  ingress.yaml  certificate.yaml  networkpolicy.yaml  pdb.yaml  hpa.yaml
    ├── serviceaccount.yaml  servicemonitor.yaml  prometheusrule.yaml  extra-objects.yaml  validate.yaml  NOTES.txt
```

**What it renders.** The names are the table of R-BAS-045 (also in the [README](../../README.md#kubernetes-and-helm)). The workload is
a Deployment, or a StatefulSet with `cacheVolume.type: persistent`, whose claim template gives every pod a volume of its own; the
cache volume of the default `ephemeral` type is a generic ephemeral volume of the pod, `emptyDir` an `emptyDir`, and the bucket
backend mounts none (the spool of fills is `/tmp`). `replicas` is `replicaCount`, and absent with `autoscaling.enabled`, where the
HPA owns it. The NetworkPolicy holds ingress rules only, as the chart `quader`'s (R-BAS-047, ADR-154): the https port from
`networkPolicy.ingress.https.from`, the ops port from `networkPolicy.ingress.ops.from` and `probeCIDRs`. There is no
`networkPolicy.egress` value; the Bastion pods' egress is the cluster operator's
([H-522](../security/bastion.md#h-522--the-chart-restricts-no-egress-of-the-bastion-pods)).

**Configuration (R-BAS-002).** `quader-bastion.config` merges `config` with the keys the chart manages (managed wins):
`bastion.main.token_file`, `bastion.main.ca_file` and `bastion.cache.bucket.ca_file` (the one key of `trustedCA`),
`bastion.signing_key_file`, `bastion.cache.encryption_key_file` (`/var/run/secrets/quader/cache-key/key`, only while
`secrets.cacheKey.existingSecret` is set, R-BAS-061), `bastion.cache.volume.dir` (`/var/cache/quader-bastion`, volume backend) with `persistent` (true for `cacheVolume.type: persistent`, which a replica
reports as whether its public catalogue survives a restart, R-BAS-111), the bucket credential
files (`source: static`), `bastion.replicas`, `bastion.edge_limit` (from `edgeLimit`: `enabled`, `requests_per_second`, `connections`, which
the Bastion reports with every handshake, R-BAS-093), `valkey.username_file`, `valkey.password_file`, `valkey.sentinel.username_file`, `valkey.sentinel.password_file`
(from `secrets.valkeySentinel`; the Sentinels of a Valkey that requires a password need them, found by the end-to-end run) and `valkey.tls.ca_file` (the Valkey CA,
else the trust bundle) while Valkey is configured, and `server.tls` with the listener pair while `tls.enabled` (`server.ops_tls` keeps
its default, which inherits it). `quader-bastion.validate` refuses a managed key under `config`. The ConfigMap `<fullname>-config` is
mounted at `/etc/quader/quader.yaml` with `subPath`, and `checksum/config` is the hash of the merged text only: the Secrets are
mounted as directories without `subPath`, so a replaced Secret reaches the files (token, TLS, trust bundle are re-read by the
binary) and rolls nothing, while any change of `config` rolls the pods.

**Ingress annotations (R-BAS-033, R-BAS-038, R-BAS-044).** Verified against HAProxy Ingress `v0.16.3` (Q-BAS-56); prefix
`haproxy-ingress.github.io/`:

| Value | Annotations |
|---|---|
| `edgeLimit.enabled` | `limit-rps`, `limit-connections`, `limit-whitelist` (joined `edgeLimit.exemptCIDRs`, omitted while empty) |
| `cacheVolume.routeByPath` | `balance-algorithm: uri path-only`, `assign-backend-server-id: "true"`, `config-backend: hash-type consistent` |
| `tls.enabled` | `secure-backends: "true"`, `secure-sni: <fullname>.<namespace>.svc`, and with `ingress.backendTLS.verify` `secure-verify-ca-secret` (`caSecret`, else the listener's Secret) |

[test/chart/bastion_haproxy_test.go](../../test/chart/bastion_haproxy_test.go) fails when `DEV_HAPROXY_INGRESS_IMAGE` or
`DEV_HAPROXY_INGRESS_CHART_VERSION` of [tools/versions.env](../../tools/versions.env) moves away from the version the names were
read against: verify the keys in the controller's `keys.md`, `haproxy.tmpl` and `annotations/backend.go` at the new tag with `gh api`,
then move the constant. That the controller refuses a flood and pins a path is the e2e suite's (R-BAS-049).

**Validation.** Every row fails `helm template` and names the values: the two URLs (set, `https`), the token Secret, the signing key
with the bucket backend or Valkey, the bucket and its credentials Secret, `cacheVolume.type: persistent` with the bucket backend,
`routeByPath` without the volume backend or without `ingress.enabled`, `edgeLimit.connections` below 1, `config.server.trusted_proxies` while `ingress.enabled` (behind the Ingress every
request comes from the controller's pods, so without it all callers count as one address, [Q-BAS-130](../spec/bastion.md#q-bas-130--behind-the-ingress-the-chart-requires-the-trusted-proxies);
the message names the value and says to list the controller's pod CIDRs), the spool and the volumes it lives in (below), a certificate source, the
https ingress peer (an empty list means "anywhere"), one trust bundle key, an `ingress.annotations` key the chart renders, a name in `disabledAlerts` that
is no alert, autoscaling bounds and metric, `highAvailability.enforce` (two replicas, a PDB, host spreading `DoNotSchedule` with
`nodeTaintsPolicy: Honor`, no standalone Valkey, three Sentinels), and the managed keys. The flood figures, the blocking and the slots are
no values of the chart since W34-E9a: they are the Bastion's registration at the main installation (R-BAS-093), and the schema refuses
`config.bastion.limits`, `failed_auth`, `blocking` and `main.max_concurrent`, `anonymous_share`, `queue_timeout`.

**The spool and the volumes** ([Q-BAS-131](../spec/bastion.md#q-bas-131--the-bytes-of-the-fills-in-progress-are-bounded-and-the-chart-sizes-the-volumes-from-the-bound)).
The binary bounds the bytes of the fills in progress at `config.bastion.cache.spool_max_bytes` (2 GiB); the chart makes the volumes agree.
`quader-bastion.bytes` reads a size in the forms of both the role (`2GiB`) and Kubernetes (`2Gi`). With the bucket backend, and with the volume
backend while `secrets.cacheKey` is set (`quader-bastion.spoolInTmp`, [Q-BAS-214](../spec/bastion.md#q-bas-214--with-a-key-the-volume-backend-spools-its-fills-on-the-pods-temporary-volume)),
the fills are spooled into the `tmp` emptyDir: `tmp.sizeLimit` is empty by default and then `spool_max_bytes` plus 256 MiB (`2304Mi`,
`quader-bastion.tmpSizeLimit`), and a `tmp.sizeLimit` below that fails the render ("evicted" is in the message: an emptyDir over its limit evicts
the pod). With the volume backend without a key the fills go below `<cache dir>/tmp` and `tmp` stays 1 Gi; with a key only the sealed copy of a
fill is written there before its rename. Either way `cacheVolume.size` must hold `max_bytes` plus the spool. `spool_max_bytes` below
`max_object_bytes` fails as well. The values `tmp.sizeLimit` and `config.bastion.cache.spool_max_bytes` are in the values block of
R-BAS-046; change both places.

**Schema and the `config` subtree.** [values.schema.json](../../deploy/helm/quader-bastion/values.schema.json) is generated once from
the key set of `values.yaml` and edited by hand afterwards (like the one of the chart `quader`) for everything outside `config`, which
is `additionalProperties: false`. The `config` property is not hand-written: it is the schema the binary emits for the role `bastion`,
[internal/config/bastion.schema.json](../../internal/config/bastion.schema.json) without `$schema` and `$id` (the root is draft-07),
spliced in by `go generate ./internal/config` ([schemagen](../../internal/config/schemagen/main.go), flag `-bastion-chart`). So
`make generate` regenerates it after a change of [bastion_role.go](../../internal/config/bastion_role.go), and `make generate-check`
(CI) fails when the committed file differs. A typo, a wrong type or an out-of-range value under `config` fails `helm template`,
`helm lint` and `helm install` with the path of the key. Never edit the `config` property or `chart-managed-keys.yaml` by hand.

The keys the chart renders itself are tagged `chart:"managed"` in `bastion_role.go` and carry `x-quader-chart-managed` in the role
schema (the binary reads them, so the schema has them). The same run writes the topmost managed paths to
[chart-managed-keys.yaml](../../deploy/helm/quader-bastion/chart-managed-keys.yaml), which `templates/_validate.tpl` reads with
`.Files.Get` and refuses with a message naming the key set (`config.<path> is managed by the chart`); the `server.*` paths only while
`tls.enabled`. Adding a key the chart renders into `/etc/quader/quader.yaml`: tag the field, run `make generate`, render it in
`quader-bastion.config` in `_helpers.tpl`; `TestR_BAS_046_EveryRenderedKeyIsInTheRoleSchemaAndManaged` fails for a rendered key that
is missing in the schema or not tagged. `bastion.cache.bucket.credentials.web_identity_token_file` is deliberately not managed: the
chart mounts no token for it, the operator sets the path of the workload identity's token.

Tests: [bastion_schema_test.go](../../test/chart/bastion_schema_test.go) (`TestR_BAS_046_ConfigIsTheGeneratedRoleSchema`,
`TestR_BAS_046_ATypoUnderConfigFailsAtHelm`, `TestR_BAS_002_ChartManagedKeysAreRefusedUnderConfig`,
`TestR_BAS_046_EveryRenderedKeyIsInTheRoleSchemaAndManaged`).

**PrometheusRule and its tests (R-BAS-041).** `_rules.tpl` holds the alerts as a YAML list; `prometheusrule.yaml` drops the names
of `disabledAlerts` and appends `extraRules`. The Valkey alert exists only while `config.valkey.mode` is not `none`, and the replica
alert reads the StatefulSet's metric for a StatefulSet. Selectors carry the release's `namespace` and ops `service`, because one
Prometheus can scrape several Bastions; thresholds come from gauges of the Bastion. [tests/rules_test.yaml](../../deploy/helm/quader-bastion/tests/rules_test.yaml)
has one healthy test in which no alert fires and one test per alert in which it fires; `TestR_BAS_041_PromtoolRules` renders the
rule file from `ci/bucket-valkey.yaml`, runs `promtool check rules` and `promtool test rules`, and fails when an alert lacks either
kind of test. promtool is the release binary at `PROMTOOL_VERSION` of `tools/versions.env` (`go install` of the module is refused by
its `replace` directives): `make promtool` fetches it into `.bin/promtool-<version>` after checking the SHA-256 of the platform,
`make chart-test` depends on it and passes the path as `QUADER_PROMTOOL`, `make versions-check` compares installed copies with the pin.
Running the test by hand needs `QUADER_PROMTOOL`.

**Tests.** `make chart-lint` lints and renders every `ci/` set of both charts; `make chart-test` runs the Bastion's tests next to the
other chart tests:

| Test | Asserts |
|---|---|
| `TestR_BAS_045_RequiredValuesRenderTheResources` | the required values give a Deployment with two replicas, PDB, Ingress with the edge-limit annotations, NetworkPolicy and no Secret, Role, RoleBinding or PVC (in every set); the names; every validation row names its values |
| `TestR_BAS_045_HAProxyAnnotationsArePinnedToTheLocalControllerVersion` | the annotation names against the controller version of the local profile |
| `TestR_BAS_046_ValuesReferenceIsTheSpecBlock` | the key set of `helm show values` equals the block parsed from `docs/spec/bastion.md` (the block is a copy of `values.yaml`) |
| `TestR_BAS_002_ConfigurationOnlyThroughTheChart` | the ConfigMap and its mount, every `config` value in the file, the managed keys, Secret mounts without `subPath`, no Secret in the environment, managed keys refused, the checksum changes with a `config` value and not with the token Secret |
| `TestR_BAS_025_ReplicasAreRenderedIntoTheConfiguration` | `bastion.replicas` is `replicaCount`, or `autoscaling.maxReplicas` with an HPA and no `replicas` field |
| `TestR_BAS_045_TheClientAddressNeedsTheTrustedProxiesBehindTheIngress` | the render fails without `config.server.trusted_proxies` while the Ingress is on, names the value, passes with the Ingress off and with a list |
| `TestR_BAS_030_TheSpoolFitsTheTemporaryVolume` | the derived `tmp.sizeLimit` (`2304Mi`), a set one used as written, a volume too small for the spool or the cache refused, a spool below `max_object_bytes` refused |
| `TestR_BAS_030_CacheVolumeKinds` | persistent gives a StatefulSet with a claim template, ephemeral and `emptyDir`, the bucket backend no volume |
| `TestR_BAS_061_TheCacheKeyIsMountedAndNamedOnlyWithItsSecret` | without `secrets.cacheKey` no mount and no `bastion.cache.encryption_key_file`; with it the directory mount at `/var/run/secrets/quader/cache-key` and the key file named, with both backends and a key name of the Secret's own; the volume backend's `tmp` of `2304Mi` and the refusal of `1Gi` naming `secrets.cacheKey`; the managed key refused under `config` |
| `TestR_BAS_033_RouteByPath`, `TestR_BAS_038_EdgeLimitIsWiderThanTheBastionsOwn` | the annotations, the refusals naming both values, a user annotation that collides |
| `TestR_BAS_047_NetworkPolicy` | ingress peers and ports; in every set `policyTypes: [Ingress]`, no `egress` key, one port per rule, no empty peer list; `networkPolicy.egress` refused by the schema; the ops rule left out without a peer; `enabled: false` renders none |
| `TestR_BAS_043_PodDisruptionBudgetAndSpreading` | PDB, spread, `highAvailability.enforce`, probes |
| `TestR_BAS_044_TLS`, `TestR_BAS_040_ServiceMonitor` | Certificate or existing Secret, backend TLS annotations, trust bundles, the plaintext switch, the ServiceMonitor |
| `TestR_BAS_041_PrometheusRuleAlerts`, `TestR_BAS_041_PromtoolRules` | the alert names, `disabledAlerts`, scoping; promtool |

**Extension checklists.**

*Add a value.* Add it to [values.yaml](../../deploy/helm/quader-bastion/values.yaml) with its default (commented only where its name does not say it) and to
the values block of [spec/bastion.md](../spec/bastion.md) R-BAS-046 as the same line (the block is a verbatim copy; the key-set test
compares the keys), to
[values.schema.json](../../deploy/helm/quader-bastion/values.schema.json), use it in a template, add a validation row if a wrong value
must fail, and a subtest named with the requirement.

*Add an alert.* Add it to `_rules.tpl` and to `quader-bastion.rules.names`, to the table of R-BAS-041, and a failure test and a healthy
assertion to `tests/rules_test.yaml`; `TestR_BAS_041_PromtoolRules` fails without them.

*Add a Secret.* Add an entry to `quader-bastion.secretMounts` and the file path to `quader-bastion.config`, the managed key to the list in
`_validate.tpl` and to R-BAS-046's list of managed keys.
