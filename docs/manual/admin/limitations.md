# Known limitations

This page lists what this version of Quader does not do, and the known weak spots an administrator should plan around. Each entry says what is missing and, where one exists, what to do instead. Nothing here is a promise about later versions.

## Package formats

- **The formats in this version.** Quader serves generic files, npm packages, NuGet packages, container images (OCI), Debian packages (APT), Ansible Galaxy collections and roles, and ClamAV's signature databases. For NuGet (.NET) a local repository takes a push, an unlist, a relist and a delete and serves its service index, package files, package documents and search, and a remote repository proxies and caches a NuGet v3 feed (nuget.org by default) and passes a search on to it, answering from the packages it has cached when the upstream cannot be asked; a virtual repository merges its members (local repositories and remotes), with their searches. Python, Maven and every other package format are not supported.
- **Container images (OCI)** are served by local, remote and virtual repositories ([Container image repositories](container-repositories.md#not-supported-in-this-version) lists what is missing): an endpoint with exposure rules serves no container images, untagged manifests are kept until somebody deletes them, a mirror copies no referrers other than an image's signatures and attestations, and several tags in one push are not supported.
- **Debian packages (APT)** are served by local, remote (pull-through or mirror) and virtual repositories ([Debian (APT) repositories](apt-repositories.md#not-supported-in-this-version) lists what is missing): there are no nested virtual Debian repositories, no uploads of source packages, no `dput`, `.udeb` or debug-symbol packages, no `Valid-Until` in Quader's own releases, and no snapshots of a suite.
- **Generic repositories are local only.** There are no remote or virtual generic repositories.
- **npm commands that are not implemented.** `npm login` and `npm adduser`, `npm search`, `npm token`, `npm access`, `npm org`, `npm team`, `npm deprecate`, staged publishing and trusted publishing through OIDC. These requests answer `404`, or `501` for a `POST`. Instead of `npm login`, put a Quader token into `.npmrc` as the client setup of the repository shows.
- **npm 6 audits.** The legacy audit endpoints answer `501`; npm 7 or later, pnpm and Yarn use the bulk endpoint, which works.

## CI pipelines and automation

- **GitLab is the only CI issuer.** GitHub Actions and generic OpenID Connect issuers answer `422` `kind_not_available`, and the `CI_JOB_TOKEN` fallback is not built. A pipeline of another system uses a service-account token with a narrow scope and an expiry, stored as a masked CI variable. [GitLab CI federation](gitlab-ci.md) describes what exists.
- **No webhooks.** The `webhook-admin` role and the webhook audit category exist, but there are no webhooks to configure.

## Vulnerability scanning

- Quader matches the stored versions of npm and NuGet repositories against the public advisory data it mirrors ([Advisory sources](vulnerability-sources.md)). It matches by package identity and does not analyse package content: dependencies bundled inside a tarball and assemblies inside a `.nupkg` are not scanned. Container images are read by an analyser instead ([Container images](vulnerability-sources.md#container-images)). Debian and Ansible Galaxy repositories take no part in the scanning.
- `npm audit`, `pnpm audit` and `yarn npm audit` (pnpm 11 and later, Yarn 4) are answered from Quader's own advisory data on local, remote and virtual npm repositories while the repository's audit mode is `local`, which is the default while vulnerability scanning is on; Quader sends nothing out. In mode `forward`, remote and virtual repositories forward the audit to the remote's upstream or to its configured provider and pass the answer on unchanged. The cache view of a remote and endpoints with exposure rules answer `501`; endpoints without cache fill are answered in mode `local` and are `501` in mode `forward`.
- Names that the installation treats as internal (a namespace claim of a local repository covers them) are withheld from the audit in mode `local` and reported as clean, so a public advisory of a package that shares an internal name is not applied to it. npm prints a notice about withheld names; pnpm and Yarn do not. In mode `forward`, names the remote has never fetched are withheld as well; install through Quader before you audit, and watch `quader_npm_audit_names_withheld_total`.
- NuGet repositories of every class list the vulnerability information in their service index once the NuGet advisory data has been imported; a restore that audits packages (`NuGetAudit`) then reports advisories from Quader's data. Until then the resource is not listed and the restore skips the audit silently.

## Backup and restore

- Quader backs itself up with `quader backup create` (the chart's backup CronJob) into a target bucket of its own, restores into an
  empty installation with `quader backup restore` (the chart's restore Job), and replays the revocation journal so that a restore
  keeps revoked tokens revoked, disabled accounts disabled and removed permissions removed ([Backup and restore](backup-and-restore.md)).
  The chart's backup is off by default: `backup.enabled: true` turns it on and then needs `config.backup.target.bucket` and the
  target's credentials.
- A restore re-applies the narrowings made after the restore point that decide who may read what: patterns, virtual members, pin
  rules, claims, a stronger vulnerability setting, a pending identity link removed, a group renamed away from the name the
  identity provider fills and a lowered token lifetime maximum. Where re-applying one could widen what a repository serves, the
  restore disables the repository and lists it as `suspended`. The settings of how a repository talks to its upstream and what it
  accepts are not journaled and come back as they were at the restore point: the hosts that receive the upstream credential,
  the checksum and content-type policy of a remote, offline mode, a local repository's client checksum policy and upload limits,
  and the settings of the npm and NuGet members. Review them after a restore.
- A restore from the database platform's own point-in-time backups is followed by `quader admin post-restore` (the chart's
  `postRestore` hook Job), which replays the journal from the restore point; `quader admin backup check` reports whether the
  platform protection you declared (`backup.recovery_window`, `backup.object_versioning`) is in place.
- Keep the Secret with the stored-secret keys (`secrets.encryption`) outside the cluster: a backup holds sealed values only, and
  values sealed with a key that is lost cannot be opened.

## Upgrades and high availability

- **No backward compatibility before the first stable release.** An upgrade may require a fresh database, no rollback is supported, and replicas of two versions are not guaranteed to work side by side during a rolling update ([Upgrades](upgrades.md#before-the-first-stable-release)). The System status screen shows such a mixed state.
- **Memory under drains.** In test runs of the releases before the split into roles, `web` pods with a 1 GiB memory limit were killed for exceeding it under heavy npm install load during a node drain. The cause is not fixed. Keep the chart's default limit of `packages` (2 GiB) or more.
- **Drain delay.** The default `health.shutdown_drain_delay` of 5 s was measured against the HAProxy Ingress of the test cluster under load: its endpoint removal took at most 1.8 s. Other ingress controllers, and clusters with slow endpoint propagation, may need longer. If clients see connection errors during drains, raise it; the drain test of the development repository measures the time for an edge and fails when the delay is shorter.
- **No egress NetworkPolicy.** The pods' outgoing traffic is not restricted by the chart. The same holds for the chart `quader-bastion` ([The Quader Bastion](bastion.md)).
- **One database and one Valkey user per installation.** Two installations in one PostgreSQL database delay each other through shared locks. Two installations that use one Valkey instance with the same user can read each other's keys.
- **Read-only mode covers client writes and the scheduler's deleting tasks.** It refuses package, file and management writes and stops the garbage collection, the OCI sweep, the fold of cache invalidations and the cleanup policies from being scheduled (see [Banner and maintenance mode](operations.md#banner-and-maintenance-mode)). The audit retention, the advisory sync and the cleanup of sessions continue, because they write their own data, and a mirror pass continues while cache filling is on. It is not the deletion pause of a restore (`quader admin maintenance pause-deletions`), which stops deletion only.

## Web UI

- No search over files from the top bar; it finds repositories, packages and projects ([Search](../user/search.md)). Use the pages of a repository's files.
- No warning before a session ends. Sessions end after 1 hour idle or 12 hours in total by default (`auth.sessions.idle_timeout`, `absolute_timeout`).
- No trash can: deleted files and packages cannot be restored. The pages of a repository's files cannot copy or move files or edit properties.
- The token screens cannot bind a token to an endpoint; use the management API or `quader admin token`, see [Endpoints and exposure rules](endpoints.md#bind-a-token-to-an-endpoint).

## Audit trail

- **Not tamper-evident.** The database owner and any superuser can change or delete audit rows, and in single-role mode so can Quader's own database login. Ship the `audit` log stream to a store outside Quader's control if you need an unchangeable copy.
- **Reads are not audited.** Viewing the audit log leaves no event; only an export is recorded.
- **Events can be lost when a process is killed.** Denials, token rejections, logins and downloads are written in batches. A replica killed without a normal shutdown (out of memory, node loss) loses the events still in its buffer.
- **Last use of a token is approximate.** It is written in batches and at most once per `auth.tokens.last_used_granularity` (default `5m`); a replica killed without a normal shutdown loses the uses of its last minute. Uses before the upgrade that introduced it are not known.

## Storage

- **Several object-store connections.** `storage.connections` takes up to eight connections. The reconciliation (the task `storage-reconcile`, every minute) copies every blob to every connection without `drain: true`, fills a connection you add and, with `drain: true`, moves what only that connection holds to the others and then deletes its copies. The scrub (the task `scrub`, hourly) reads every copy whole once in its connection's `scrub_period`, copies made inside one endpoint from one bucket to another first, so a drain beside such copies waits up to about two hours; it replaces a damaged copy from a connection that holds the blob whole. Garbage collection deletes every copy on its own connection, and the weekly orphan sweep (the task `orphan-sweep`) walks every connection. The page [Storage](storage.md) of Settings shows each connection and changes none: connections are added and drained in the configuration. Its fill, damaged copies and scrub come from the storage summary, which runs hourly, so they and `QuaderStorageCopyDamaged` lag up to an hour behind the copies. A backup that runs while one upgrade adds a connection and drains another can miss a blob the drain deleted: add the connection and drain the old one in two upgrades. One connection is the plain case and works as before.
- **Encrypted connections.** `encrypted: true` keeps a connection's blobs in Quader's own encrypted format; the configuration and the Helm chart accept it, and the scrub, the reconciliation, the orphan sweep, the backup and the restore handle it. The backup pod holds no key, so it takes an encrypted blob into the pool by its size and record without decrypting it: a blob damaged at the provider before its first backup, and not yet found by the scrub, enters the pool unnoticed and stops a restore of that backup. `quader backup verify --deep --key-ring <dir>` finds it. `quader admin storage offline` reads an encrypted bucket without Quader, with the export of the storage keys and the key ring's files ([configuration](configuration.md), [backup](backup-and-restore.md#read-an-encrypted-bucket-without-quader)).
- **No quotas.** The storage summary (`GET /api/v1/storage/summary`) has no quota figure, and a repository has no size limit; an upload is bounded only by `storage.upload.max_size`, the repository's own upload size limit and the spool space of the replica. Grant deploy only to the accounts that publish.

## Sign-in and recovery

- **A break-glass account without a password file starts silently.** With `auth.break_glass.enabled: true` and no password file, Quader starts and refuses every local login. The chart turns the account off when `secrets.breakGlass` is empty; outside the chart, check the setting. Test the break-glass login once after you set it up.
- **The break-glass account can be kept locked.** Five wrong passwords lock it for 15 minutes, from any address. Whoever reaches the local login can keep it locked. Restrict `/auth/local/` at the edge, and clear a lock with `quader admin break-glass unlock`.
- **The identity provider can remove the last administrator.** Group sync at sign-in removes memberships the provider no longer reports, also the last one of the administrator group. Keep the break-glass account available for recovery.

## Database, Valkey and object store access

- **Single-role database mode is the chart default.** Without `secrets.databaseMigration`, Quader's runtime login owns the schema and may change its structure. Configure a separate migration login to remove that right.
- **Valkey without its own user.** Without a configured Valkey user name (`secrets.valkey.usernameKey` empty), Quader connects as the `default` user, which usually holds every right on the instance. Give Quader an ACL user of its own where your Valkey setup can create one; besides the key, counter, sorted-set and Pub/Sub commands it needs `EVALSHA` and `EVAL`, for the one script that decides and counts a request's rate limits.
- **Sentinel trust is a warning only.** Sentinels without a password and Valkey certificates verified against the system roots are logged at start, not refused. Set `secrets.valkeySentinel` and `secrets.valkeyCA`.
- **Object-store credentials.** With `credentials_source: default` in an element of `storage.connections` (the default), the cloud SDK's credential chain can reach the node's metadata service. Use `static` or `webIdentity`.

## TLS and network

- A loaded certificate keeps being served after it expires; only the expiry metric warns. Alert on it as shown in [TLS and certificates](tls-and-certificates.md).
- Client certificates are not checked for revocation (no CRL, no OCSP).
- Connections that are open during a certificate rotation keep the old certificate until they close.
- `tracing.ca_file` and the stored-secret key files are read only at start; a change needs a restart.
- The ops listener has no sign-in of its own; the metrics token (`metrics.auth: static`) is optional. The chart's NetworkPolicy is what keeps others out.
- The client address in logs, audit events and rate limits follows the forwarded header of the proxies in `server.trusted_proxies`. List only proxies that append the real client address.

## Endpoints

- Pods that serve a further endpoint, a pool included, hold the same database, Valkey and object-store credentials as every `api` and `packages` pod.
- Exposure rules are tied to an endpoint name; an endpoint configured under an old name takes over the old rules.
- npm package documents on an endpoint with rules keep their real `time.created`, `time.modified` and `Last-Modified`, so customers can tell that a package changed.
- A rule on a virtual repository also covers packages of members added later.
- An endpoint with exposure rules (`exposure: rules`) does not serve container images, Debian repositories or Ansible Galaxy repositories: the OCI, Debian and Ansible Galaxy formats have no exposure adapter yet. Registry, APT and `ansible-galaxy` clients use an endpoint with `exposure: all`.

## Remote repositories

- A missing upstream secret file, or a stored secret sealed with a key the replica lacks, is noticed at the repository's first request, which answers `502 upstream_secret_unavailable`, not at start-up. Run the repository's upstream test (`POST /api/v1/projects/{projectKey}/repositories/{repoKey}/upstream-test`) after rolling out secret files.
- Whoever may configure remote repositories can point any upstream secret of `config_as_code.secrets_dir` at a host of their choice. Mount only the upstream secrets that those administrators may use.
