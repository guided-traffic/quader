# Privilege footprint

Status: built for the PostgreSQL grants of the runtime role (since M0) and of the audit objects (M1), the pod security context,
the service account and the network ingress of every Deployment and Job of the chart. The Valkey user and the object-store permissions are configured outside Quader; this page says what
Quader asks for and what the operator has to provide.

This page lists what Quader itself is allowed to do on each system it depends on, and what follows when a process is
compromised. It describes the chart and the code as they are.

## PostgreSQL

Two credential modes exist (R-HA-014, [ha-and-helm.md](../spec/ha-and-helm.md#r-ha-014--database-provisioning-roles-and-privileges)):

| Mode | Chart | Who owns the schema | What a request-serving login may do |
|---|---|---|---|
| Two-role | `secrets.databaseMigration.existingSecret` set | the migration role (`quader_owner`) | DML on the tables of the schema, through the default privileges of the first migration ([00001_foundation.sql](../../internal/meta/postgres/migrations/00001_foundation.sql)) |
| Single-role | `secrets.databaseMigration.existingSecret` empty (the chart default) | the runtime login itself | everything the owner may do: DDL included |

In single-role mode a replica logs a warning at start: a SQL injection in the application would have DDL rights
([preflight.go](../../internal/meta/postgres/preflight.go), `preflight`). This is the default of the chart. The gap is
[H-71](#h-71--single-role-mode-gives-the-runtime-login-ddl-rights).

Two-role mode adds one revocation. The bookkeeping tables `schema_migration` and `schema_state` are written only by the
migration role; migration 5 revokes `INSERT`, `UPDATE`, `DELETE` and `TRUNCATE` on them from `quader_app`
([00005_bookkeeping_privileges.sql](../../internal/meta/postgres/migrations/00005_bookkeeping_privileges.sql)). `system_state` stays
writable for the runtime, because the runtime sets the deletion pause and later keys of its own.

At start, every replica runs the preflight ([preflight.go](../../internal/meta/postgres/preflight.go)):

- the server version (16 or later), and `UTF8` encoding;
- a superuser runtime role is refused unless `database.preflight.allow_superuser: true`;
- after migration 5 has applied, a runtime login that can still write `schema_migration` or `schema_state` is refused, with the
  `REVOKE` that would fix it named in the error (`checkBookkeeping`). A superuser or the owner of the tables is skipped;
- behind a transaction-mode pooler (`database.pooler.mode: transaction`), the session settings that cannot travel as startup
  parameters must come from the role's defaults (`checkRoleDefaults`).

Not checked by the preflight: the grants of the audit objects and of the backup role that R-HA-014 lists. The audit tables
exist, and migration 6 revokes `UPDATE`, `DELETE` and `TRUNCATE` on `audit_event` and every write on `audit_policy` from
`quader_app` ([00006_audit.sql](../../internal/meta/postgres/migrations/00006_audit.sql)), but no start checks that those
grants still hold ([H-75](#h-75--the-audit-object-grants-are-not-checked-at-start)). A backup role without its grants is found
by the backup run, which fails with `backup_grant_missing` ([backup_create.go](../../cmd/quader/backup_create.go), `mapOpenError`).

The migration Job ([migrate.yaml](../../deploy/helm/quader/templates/migrate.yaml)) runs `quader migrate up` with the migration
credentials when they are set, with no key ring, no object-store or Valkey Secret and no service-account token. It serves no
listener. Its exit status `3` (a refused contract guard or a half-restored database) fails the Job without a retry.

Advisory locks need no grant beyond the connection's own rights. The lock keys are described on
[isolation-and-coordination.md](isolation-and-coordination.md).

## Valkey

The client reads the user name from `valkey.username_file` and the password from `valkey.password_file`. An empty
`username_file` means the `default` user ([config/valkey.go](../../internal/config/valkey.go); the empty name in
[valkey_auth.go](../../internal/cluster/valkey_auth.go), `readSecretFile`). The Sentinel pair is separate
(`valkey.sentinel.username_file`, `password_file`).

What Quader asks Valkey to allow for its own user is in [R-ARCH-031](../spec/architecture.md#r-arch-031--valkey-client-sentinel-discovery-tls-acl-and-degradation):
a key prefix, the command set the client uses, and no administration. The command set holds `EVALSHA` and `EVAL` for one script, the
take of a request's rate windows ([usage-and-limits.md](../spec/usage-and-limits.md#q-lim-57--the-request-windows-are-decided-and-counted-in-one-step-by-a-script-in-the-round-trip-of-stage-7)):
a script runs under the ACL of the user that sends it, so it reaches no command and no key beyond the user's; what the two commands
add is CPU time in Valkey, which `busy-reply-threshold` bounds for a script that would loop, and a writer under the user's credential
could already refuse every request with 429 ([H-49](../spec/architecture.md#h-49--write-access-to-valkey-denies-service)). A replica's first
connection writes its probe key and runs the take with no key: a user without `EVAL` or `EVALSHA`, or whose key pattern does not
cover the key prefix, is refused there, and the replica stays unready like one with a wrong password, instead of serving with
every limit counted per replica ([Q-HA-331](../spec/ha-and-helm.md#q-ha-331--an-answer-is-the-login-or-its-refusal-and-the-first-connection-also-writes-the-probe-key-and-runs-the-take-script));
another command missing from the user shows only at its first use ([H-1050](../spec/ha-and-helm.md#h-1050--only-the-probe-write-and-the-take-script-are-checked-at-start-another-missing-command-shows-at-its-first-use)).
A starting replica waits for that answer for at most `valkey.startup_answer_timeout` (30 s); one that got none becomes ready
degraded and sees a wrong credential only when Valkey answers ([Q-HA-293](../spec/ha-and-helm.md#q-ha-293--a-replica-waits-for-valkeys-first-answer-before-its-first-readiness-for-a-bounded-time)). The valkey operator that the owner's
stack uses creates no ACL users and no separate Sentinel password yet. Until it does, the local profile runs Quader as the
operator's `default` user with the cluster password, and the same Secret authenticates against the Sentinels: its values set
`secrets.valkey.usernameKey: ""` ([values-dev.yaml](../../deploy/dev/values-dev.yaml); the owner's decision is recorded in
[ha-and-helm.md](../spec/ha-and-helm.md), Q-HA-15). That is [H-72](#h-72--the-default-valkey-user-holds-every-right-until-acl-users-exist).

## Object store

Quader calls these S3 operations and no others ([internal/blob/s3](../../internal/blob/s3)):

| Operations | Who calls them | Code |
|---|---|---|
| `PutObject`, `CreateMultipartUpload`, `UploadPart`, `CompleteMultipartUpload`, `AbortMultipartUpload`, `HeadObject`, `GetObject`, `DeleteObject`, `DeleteObjects`, `ListObjectsV2` | the blob store of the replicas, the journal shipper, the backup's reads of the blob bucket, the backup target client | [client.go](../../internal/blob/s3/client.go) |
| `CopyObject`, `UploadPartCopy` | the server-side copy of a blob into the target at a backup, and into the blob store at a restore (R-BAK-006); and the copy of the finalize of an upload session, from its staging object to the content address, on every replica that takes uploads (R-STOR-011; one `CopyObject` up to 5 GiB, parts of 1 GiB above) | [target_copy.go](../../internal/blob/s3/target_copy.go), [blobsink.go](../../internal/blob/s3/blobsink.go), [copy.go](../../internal/blob/s3/copy.go) |
| `ListObjectVersions`, and `GetObject` with a `VersionId` | the journal reader of a restore and of the post-restore, with the credential of `restore.journalCredentials` or `postRestore.journalCredentials` | [journalstore.go](../../internal/blob/s3/journalstore.go) |
| `GetBucketVersioning`, `GetBucketLifecycleConfiguration` | the platform check of the blob bucket's protection (R-HA-023); a credential that may not read them gives "unknown", the common answer for the runtime credential | [protection.go](../../internal/blob/s3/protection.go) |

The client also implements `ListMultipartUploads` and a presigned `GetObject`. `ListMultipartUploads` is called by the staging sweep of the
collection run (a `worker`, in a full sweep, R-STOR-011 Expiry) and by the object-store conformance check of R-STOR-035
([conformance.go](../../internal/blob/conformance.go)); the presigned `GetObject` only by the check in the integration tier. Specified and
not built ([download-links.md](download-links.md), [storage.md](../spec/storage.md) R-STOR-072, R-STOR-074): with the delivery by signed
links switched on, a replica that serves downloads signs a `GetObject` of one key for a client, which that client then sends with the
runtime credential's authority until the link expires, and its start writes, signs and fetches one probe per connection. Every replica's
startup check sends a part number twice in a multipart upload and completes it, naming one ETag, then deletes the object
(`CreateMultipartUpload`, `UploadPart`, `CompleteMultipartUpload`, `DeleteObject` below `<prefix>/_probe/`); `AbortMultipartUpload` is called only when
the store refuses the completion or the check is cleaned up, and the sweep deletes the staging objects and aborts the leftover probe objects and
uploads it finds; all of these are within the permissions of R-STOR-031. Quader never calls `CreateBucket`, any `PutBucket*`, or any call that configures
versioning, lifecycle or object lock ([internal/blob](../../internal/blob) has no such call). The one call of `DeleteObjectVersion` is
`quader admin storage restore-versions` ([versions.go](../../internal/blob/s3/versions.go)), which an operator runs by hand with a credential
of its own (`--credentials-file`, with `s3:ListBucketVersions` and `s3:DeleteObjectVersion`) and which deletes only delete markers, never
an object version ([backup-and-restore.md](backup-and-restore.md#the-version-repair-of-a-platform-restore)); no role, Job or CronJob of the
chart calls it. The bucket is an operator resource.

The minimum permissions per prefix are R-STOR-031 ([storage.md](../spec/storage.md)). The stackit S3 provisioner that the owner
uses grants a workload policy that denies `PutBucketVersioning`, `PutLifecycleConfiguration`,
`PutBucketObjectLockConfiguration` and `PutBucketPolicy`, and its action list has no `DeleteObjectVersion`. This was read in
the provisioner's documentation on 2026-10-29, not in this repository. Consequences for the design: a versioned blob bucket
and an incomplete-multipart lifecycle rule cannot be set by Quader, and the backup target cannot use Object Lock.

The credential source is one of `default`, `webIdentity` and `static` (`storage.connections[].credentials_source`). With `static`,
Quader reads the key files and nothing else. With `default` the AWS SDK's default chain is used, which is
[H-74](#h-74--the-default-object-store-credential-chain-can-reach-node-metadata).

## Pod and service account

Every pod of the chart runs with the defaults of [values.yaml](../../deploy/helm/quader/values.yaml):

| Setting | Default | Where |
|---|---|---|
| `runAsNonRoot`, `runAsUser`, `runAsGroup`, `fsGroup` | `true`, `65532` | `podSecurityContext` |
| seccomp | `RuntimeDefault` | `podSecurityContext`, `securityContext` |
| `allowPrivilegeEscalation`, `privileged` | `false`, `false` | `securityContext` |
| `readOnlyRootFilesystem` | `true`; `/tmp` is an `emptyDir` of `tmp.sizeLimit` | `securityContext`, `tmp` |
| capabilities | `drop: [ALL]` | `securityContext` |
| service account token | not mounted (`automountServiceAccountToken: false`, both in the service account and the pod) | `serviceAccount`, [migrate.yaml](../../deploy/helm/quader/templates/migrate.yaml) |

The chart renders no `Role`, `RoleBinding` or `ClusterRole`. The service account has no RBAC, and Quader never calls the
Kubernetes API. The chart cannot set Pod Security Admission labels on a namespace; the local profile sets them in its scripts
([local-profile.md](../developer/local-profile.md)).

## The scanner

Built ([R-HA-065](../spec/ha-and-helm.md#r-ha-065--the-scanner-deployment), [vuln-scanner-trivy.yaml](../../deploy/helm/quader/templates/vuln-scanner-trivy.yaml),
[R-ARCH-036](../spec/architecture.md#r-arch-036--the-role-scanner-what-it-serves-builds-reads-and-holds)). The pod that analyses container
images, the parser of the most hostile input Quader reads, has the smallest footprint of the release:

| System | What the scanner may do |
|---|---|
| PostgreSQL | nothing: no credential, no CA, no connection; no row in the replica registry |
| Valkey | nothing: no credential, no connection; Valkey's ingress example of R-HA-042 does not list the component `scanner` |
| Object store | nothing: no credential, no connection |
| Key ring | nothing: no key file; it cannot sign or verify a token or open a sealed secret |
| Kubernetes | nothing: no service-account token, no RBAC; the pod names no service account of the chart, which may carry a cloud identity of the object store, and runs under the namespace's `default` ([Q-HA-343](../spec/ha-and-helm.md#q-ha-343--the-scanner-pod-names-no-service-account-of-the-chart), [H-1070](#h-1070--the-scanner-runs-under-the-namespaces-default-service-account)) |
| The registry | pull, through the scan listener of `packages`, of the one manifest and the blobs of its name that the call's scan token names, for the token's minutes; never fill a remote, never write |
| The pod | the security context of every pod; one writable spool and `/tmp`; the scanner's own TLS pair and the internal CA, one Secret ([R-HA-063](../spec/ha-and-helm.md#r-ha-063--chart-tls-wiring)) |

What an analyser that an image took over can do with this footprint is on [image-scanning.md](image-scanning.md). What it does not bound is
the network: [H-1036](image-scanning.md#h-1036--the-chart-restricts-no-egress-of-the-scanner).

With the detection in the scanner and the analyser from its own image (built on 2026-11-21;
[R-HA-070](../spec/ha-and-helm.md#r-ha-070--the-analyser-from-its-own-image-and-the-database-in-the-scanner-pod)) the pod keeps this
footprint and gains three things, none of them a credential: an init container of the analyser's own image that mounts one volume and
copies the binary; a container `database` of the Quader image with the scanner's own pair, which reads the database routes of the scan
listener and writes the database volume; and two volumes, the binary and the database, both read-only in the scanner's container.

The malware scanner (specified, not built; [R-HA-068](../spec/ha-and-helm.md#r-ha-068--the-malware-scanner-deployment)) has the same
footprint, with a read of one blob instead of one image, and a second container `clamd` of ClamAV's official image that mounts no TLS
pair and no configuration of Quader and opens no network listener. `freshclam` runs in the updater, a Deployment of its own
([R-HA-069](../spec/ha-and-helm.md#r-ha-069--the-updater-deployment-malware-updater-freshclam); the chart renders it, and no worker pod carries `freshclam`): its
`quader` container has the scanners' footprint, its `freshclam` container holds no credential and mounts no Secret, and it is the only pod
that reaches ClamAV's network ([H-1113](malware-scanning.md#h-1113--a-taken-over-freshclam-has-the-updater-pods-network-position)). What
`clamd` and `freshclam` can do when taken over is on [malware-scanning.md](malware-scanning.md) and [clamav-definitions.md](clamav-definitions.md).

## Network

The chart renders one ingress `NetworkPolicy` per Deployment, named as the Deployment, and one per Job, when `networkPolicy.enabled`
is `true` (the default) ([networkpolicy.yaml](../../deploy/helm/quader/templates/networkpolicy.yaml); the Deployments are those of
[_units.tpl](../../deploy/helm/quader/templates/_units.tpl), `quader.units`). Every rule names its port; no rule admits one Quader pod from
another, because replicas never talk to each other:

| Pods | Ingress admitted | Egress |
|---|---|---|
| `api`, `packages` and their path pools | `:8080` from `networkPolicy.ingress.http.from` (empty: from anywhere); the port of each further endpoint the pods open, from that endpoint's `networkPolicy.from` (empty: from anywhere); `:8081` from `networkPolicy.ingress.ops.from` (default: the `monitoring` namespace) and `networkPolicy.ingress.probeCIDRs` | not rendered |
| the main `packages` Deployment, in addition, while either scanning is on | `:8082`, the scan listener, from the `vuln-scanner-trivy` and `malware-scanner-clamav` pods only ([R-HA-066](../spec/ha-and-helm.md#r-ha-066--the-links-of-the-image-scanning-inside-the-chart)) | not rendered |
| the endpoint pools of further endpoints | the endpoint's port, from its `networkPolicy.from`; `:8081` from the ops sources | not rendered |
| `worker` and its pools | `:8081` only, from the ops sources | not rendered |
| `vuln-scanner-trivy` ([vuln-scanner-trivy.yaml](../../deploy/helm/quader/templates/vuln-scanner-trivy.yaml)) | `:8080` from the pods of the worker Deployment that claims the queue `vuln` only; `:8081` from the ops sources | not rendered ([H-1036](image-scanning.md#h-1036--the-chart-restricts-no-egress-of-the-scanner)) |
| `malware-scanner-clamav` ([malware-scanner-clamav.yaml](../../deploy/helm/quader/templates/malware-scanner-clamav.yaml)) | `:8080` from the pods of the worker Deployments that claim the queue `malware` only; `:8081` from the ops sources; the scan listener `:8082` of `packages` also admits it ([R-HA-068](../spec/ha-and-helm.md#r-ha-068--the-malware-scanner-deployment)) | not rendered ([H-1036](image-scanning.md#h-1036--the-chart-restricts-no-egress-of-the-scanner)) |
| migration Job, backup CronJob's pods, restore Job, post-restore Job | nothing (`ingress: []`) | not rendered |

With no ops source at all the `:8081` rule is left out rather than rendered with an empty `from`, which would admit every peer. The two
rules of the image scanning are the only ones that admit one Quader pod from another; neither follows `networkPolicy.ingress.http.from`.

No egress policy is rendered, so a compromised replica can reach any address the pod network reaches
([H-516](egress.md#h-516--the-chart-restricts-no-egress-of-its-pods)). The well-known repositories of the project `upstream`
([repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-066) add no right and no connection: the
migration Job's reconcile writes their rows with the login it already holds for the schema and contacts no upstream, and a replica
reaches a public registry only after an administrator switched its repository on, through the client and the address policy every
remote uses ([egress.md](egress.md#the-well-known-repositories-what-an-installation-reaches-by-default)). With no egress policy, that
switch is the installation's only gate to those registries.
The ingress policy is enforced on the kind profile's CNI, which is verified in the end-to-end suite; other CNIs are not verified.

Each further endpoint adds a listener on its own port and a Service, and its pods run with the credentials of their role
([H-57](../spec/endpoints.md#h-57--a-further-endpoint-runs-with-every-credential-of-a-web-replica)).

## What this does not cover

The gaps of the spec pages that bound or widen this footprint:

- [H-49](../spec/architecture.md#h-49--write-access-to-valkey-denies-service): write access to Valkey denies service.
- [H-56](../spec/architecture.md#h-56--the-object-store-endpoint-skips-the-egress-address-policy): the object-store endpoint skips the egress address policy.
- [H-517](../spec/architecture.md#h-517--the-cut-into-roles-separates-the-login-secrets-not-the-data-credentials): every role connects with the same database login and the same object-store and Valkey credentials, but the `scanner`, which holds none.
- [H-57](../spec/endpoints.md#h-57--a-further-endpoint-runs-with-every-credential-of-a-web-replica): a further endpoint runs with every credential of its role.
- [H-41](../spec/operations-and-observability.md#h-41--ingress-enforcement-is-verified-on-one-cni-only): the enforcement of the ingress NetworkPolicy is verified on one CNI only.
- [H-516](egress.md#h-516--the-chart-restricts-no-egress-of-its-pods): the chart restricts no egress of its pods.
- [H-1036](image-scanning.md#h-1036--the-chart-restricts-no-egress-of-the-scanner): the same for the scanner, whose pod holds no credential but reads hostile images.
- [H-1035](image-scanning.md#h-1035--a-taken-over-analyser-holds-the-scanners-pair): a taken-over analyser holds the scanner's pair, which passes nowhere the scanner does not.

### H-71 · Single-role mode gives the runtime login DDL rights

Stage: the database's schema, changed by any SQL the runtime login sends ([preflight.go](../../internal/meta/postgres/preflight.go)).

Live with the chart default: `secrets.databaseMigration.existingSecret` is empty, so the runtime login owns the schema and holds
DDL rights. A replica only warns at start. Subject: the schema `quader` and everything in it, including the bookkeeping
tables, which in this mode are writable by the same login.

What makes it real: a SQL injection in any path that builds SQL from input (none is known; the code uses parameters and
`pgx.Identifier`), or a leaked runtime password. The two-role mode removes the DDL right; the chart does not enforce it.

### H-72 · The default Valkey user holds every right until ACL users exist

Stage: every connection from a replica to a data node and to a Sentinel ([cluster/valkey_auth.go](../../internal/cluster/valkey_auth.go)).

Live wherever `valkey.username_file` is empty or the chart's `secrets.valkey.usernameKey` is empty, and so in the local profile,
whose values set `usernameKey: ""` ([values-dev.yaml](../../deploy/dev/values-dev.yaml)). The owner's valkey operator cannot yet
create ACL users (its own documentation names this gap, checked 2026-10-29), so an installation on that operator runs Quader as the
`default` user; the owner decided on 2026-10-29 that the local profile follows that path as an interim. The `default` user is given `~*`, `&*` and `+@all` by the operator. Subject: every key of the
Valkey instance, including the keys of other installations and of other applications on the same instance, and the commands
that flush or script.

What makes it real: a leaked Valkey password, or a replica compromised by the code path that writes to Valkey. Its effect is
still bounded by what Valkey may hold (see [data-and-secret-flow.md](data-and-secret-flow.md)): a Valkey write denies service
and never grants access, which is [H-49](../spec/architecture.md#h-49--write-access-to-valkey-denies-service).

### H-73 · Installations that share one Valkey share its user

Stage: the Valkey instance, when two installations of Quader connect to it with the same user.

Dormant: live only when two installations use one Valkey instance and one user. Quader's keyspace is separated per installation
by a key prefix and the keyspace ID ([README.md](../../README.md#valkey), `Keyspace`), but an ACL user with `~*` reads all of
them. The separation is a naming convention, not an access control.

What makes it real: a second installation on a shared instance, configured without its own ACL user. The mitigation is one
Valkey instance per installation, or one ACL user per installation once the operator provides them.

### H-74 · The default object-store credential chain can reach node metadata

Stage: the credentials of the object-store client when `storage.connections[].credentials_source` is `default`
([blob/s3/credentials.go](../../internal/blob/s3/credentials.go), `awsConfig`).

Live on a cloud without workload identity, with the default source and no environment or shared-config credentials: the AWS
SDK's default chain then reaches the instance metadata service of the node. The object-store client applies no egress address
policy ([H-56](../spec/architecture.md#h-56--the-object-store-endpoint-skips-the-egress-address-policy)), so the metadata
address is not refused by Quader. Whether the metadata request itself goes through that client was not verified in this run.
Subject: the node's identity, which may carry rights beyond the bucket.

What makes it real: a node role with rights beyond the bucket, and no workload identity bound to the pod. Setting
`credentials.source: static` or `webIdentity` removes the chain from the path.

### H-75 · The audit-object grants are not checked at start

Stage: the start-up preflight of a two-role installation ([preflight.go](../../internal/meta/postgres/preflight.go)).

Live on every two-role installation: the audit tables exist since migration 6, which revokes the writes the design forbids
([00006_audit.sql](../../internal/meta/postgres/migrations/00006_audit.sql)), and the preflight checks none of their grants
(`preflight` checks the server, the superuser, the bookkeeping tables and the role defaults;
[R-HA-014](../spec/ha-and-helm.md#r-ha-014--database-provisioning-roles-and-privileges) lists the audit objects as well). A grant
that is missing or surplus on `audit_event` or `audit_policy` stays until someone reads the grants by hand. In single-role mode the
runtime login owns the tables, and the revocation does not apply ([H-71](#h-71--single-role-mode-gives-the-runtime-login-ddl-rights)).
Subject: the append-only property of the audit trail against a runtime role.

What makes it real: the audit tables created without the check, or a later `GRANT` that re-opens a write the design revokes.

### H-1070 · The scanner runs under the namespace's `default` service account

Stage: the pod of [vuln-scanner-trivy.yaml](../../deploy/helm/quader/templates/vuln-scanner-trivy.yaml), which names no service account
([Q-HA-343](../spec/ha-and-helm.md#q-ha-343--the-scanner-pod-names-no-service-account-of-the-chart)).

Dormant with the chart's defaults: `automountServiceAccountToken: false`, and the chart gives `default` no RBAC and no annotation. Subject:
whatever a cluster attaches to the namespace's `default` account: an RBAC binding, an image pull secret, or a cloud identity that a
web-identity webhook injects as a projected token by the account's annotation, which `automountServiceAccountToken: false` does not stop.
An analyser that an image took over would then hold that identity.

What makes it real: an operator or a platform default that binds a right or a cloud identity to `default` in the release's namespace. Leaving
`default` bare, or running Quader in a namespace of its own, keeps it dormant; the chart does not check it.
