# Backup and restore

This page is for the people who protect a Quader installation against losing data. It explains what each layer of protection covers, how to set up and run Quader's own backups, how to check that a backup can be restored, how to bring the whole installation back from a backup or to a point in time through your database platform, and what a restore does to tokens and other access that was removed after the restore point.

## What protects what

| Layer | Protects | At worst you lose | Restored with |
|---|---|---|---|
| Quader's backup (`quader backup create`, the chart's CronJob) | Every row of the database as of one snapshot, and every blob a row references, in a bucket of its own | What happened since the last backup, by default up to a day | [Restore a backup](#restore-a-backup) |
| Your platform's recovery (PostgreSQL archiving, bucket versioning) | PostgreSQL up to a time you choose; the objects of blobs that were deleted | Minutes: the interval at which your platform archives the log | [Restore to a point in time](#restore-to-a-point-in-time) |
| What you keep yourself | The Secret with the stored-secret keys (`secrets.encryption`), your Helm values, the other Secrets | The stored upstream credentials and other sealed secrets, if the key Secret is lost | Recreate them before either restore |

Both restores are for the whole installation: the backup and the platform layer cannot bring back one repository or one package. The chart's backup is off by default, and the platform layer is your choice; [What this page does not cover](#what-this-page-does-not-cover) lists what is not built.

A backup holds the database, every blob a snapshot row references, and a manifest with versions, schema step, snapshot time, checksums and the key IDs the sealed values need. It does not hold:

- Valkey: nothing in it is the only copy, and a restored installation starts on a new keyspace;
- the stored-secret keys, the configuration file, the Helm values and the Kubernetes Secrets;
- UI sessions and other transient tables, uploads in progress, and the revocation journal, which stays in the buckets of the object-store connections on purpose ([What a restore does to access removed after the restore point](#what-a-restore-does-to-access-removed-after-the-restore-point));
- audit events written after the snapshot, which exist only in the `log: audit` stream ([Monitoring and operations](operations.md#the-audit-log)).

## Set up the backup

Turn the backup on in the Helm values and give it a bucket of its own. With `backup.enabled: true` the chart refuses to render without `config.backup.target.bucket` and renders the CronJob `<fullname>-backup`, with its own ServiceAccount.

```yaml
backup:
  enabled: true                    # default: false
  schedule: "0 2 * * *"            # default: five fields or an @-macro; no seconds, no TZ=
  timeZone: Etc/UTC                # default
config:
  backup:
    target:
      bucket: quader-backups        # example: required; a bucket of its own
      prefix: quader                # default
      region: eu-central-1         # example
    retention:
      keep_last: 7                 # default
      keep_within: 0s              # default: 0s keeps the newest keep_last only; example: 720h
    expected_interval: 24h         # default: read by the overdue mark and QuaderBackupStale
    exclude:
      remote_caches: false         # default; true leaves out blobs only remote-repository caches reference
```

The target must not be the bucket of an object-store connection on the same endpoint: the orphan sweep deletes every object under a connection's `prefix` that no row references, backups included. The chart and every backup command refuse that for each element of `storage.connections`, except with `config.backup.target.allow_same_bucket: true` for an evaluation setup, and then the target's prefix and the connection's must not contain each other. Prefer a bucket in another account or project. S3 Versioning with an Object Lock retention of at least `keep_last` times `expected_interval` keeps a compromised backup role from destroying backups within that time; with it, set `config.backup.copy.check_existing: always`. Neither is enforced by Quader.

**Credentials.**

| What the pod reaches | How |
|---|---|
| The target | `config.backup.target.credentials.source`: `default` (the workload identity on the ServiceAccount, `backup.serviceAccount.annotations`, the default) or `static` with `secrets.backupTarget.existingSecret` |
| The blob bucket of a connection whose element in `config.storage.connections` has `credentials_source: static` | the `backupSource` of the connection's entry in `secrets.storageConnections`, a login that may only read its bucket (`s3:GetObject`, `s3:ListBucket`); the chart requires it, so a compromised backup pod cannot write blobs or the journal. The backup reads each blob from one connection that holds it, and takes another when a copy is missing, damaged or its connection does not answer |
| PostgreSQL | `secrets.databaseBackup.existingSecret`, the login of `config.database.backup_role`; without one the pod uses `secrets.database` and logs a warning |

The backup pod holds no key ring and no Valkey credential. The CronJob's `concurrencyPolicy` is `Forbid`, and the database lets only one run write to a target at a time. A run missed by more than `backup.startingDeadlineSeconds` (3600 by default) is skipped, not started late. A drain never waits for a backup; the run ends itself as **Interrupted** within the grace period and the Job starts it again.

**Keep the keys.** Keep a copy of `secrets.encryption` outside the cluster and away from the backup target. A backup holds sealed values only, and a restore stops before it loads anything when the key ring cannot open them. A rotated key stays in your copy for as long as a retained backup lists it; `quader backup list` names the key IDs of each backup.

With an encrypted connection (`encrypted: true`, see the [configuration](configuration.md)) the key ring is also the key to your artifacts: without it every blob of every encrypted connection is lost, in the bucket and in every backup. Keep the export of the storage keys (`quader admin storage keys export`) beside your copy of the key ring, and export again after you add an encrypted connection, draw a new generation or rotate the key ring. A bucket and the key ring alone do not read a blob: the export, or a backup of the database, is the third part.

A backup keeps each blob of an encrypted connection as the connection stores it, encrypted, with its record; the pool object's name ends in the identifier of the storage key that opens it (`pool/sha256/ab/cd/<sha256>.sk-archive-1-3f9a0c2e`, example). The backup pod holds no key and decrypts nothing, and the backup's database holds the storage keys sealed under the key ring. A restore opens those keys with the key ring before it writes anything (check 3 below), so `secrets.encryption` must hold the key ring the backup's database was sealed with. Into an encrypted first connection with the id of the backed-up one the restore writes the objects as they are; into an encrypted connection of another id it draws new keys and encrypts every blob anew; into a plain one it decrypts. Export the storage keys again after a restore.

A copy of an encrypted bucket made by a tool that drops object metadata cannot be read: each object's key lies in its metadata. Copy such a bucket with `quader admin storage offline pull` ([below](#read-an-encrypted-bucket-without-quader)), or only with a tool that keeps metadata.

**Run a backup now.** The Backups screen shows the command, or run it from a shell:

```shell
kubectl -n quader create job --from=cronjob/quader-backup quader-backup-manual-$(date +%s)   # example: release quader in namespace quader
```

## Check that a backup restores

The tab **Backups** of System status shows the runs, the last success and the stored backups ([Backups](operations.md#backups)); it changes nothing and it cannot verify. Verifying is a command, and the pods of the release that hold the target's credentials are the backup Jobs, so run it as a Job built from the CronJob:

```shell
kubectl -n quader create job quader-backup-verify --from=cronjob/quader-backup --dry-run=client -o json \
  | jq '.spec.template.spec.containers[0].args = ["backup", "verify", "--latest", "--deep"]' \
  | kubectl -n quader create -f -                                                  # example: replace the arguments as you need
kubectl -n quader wait --for=condition=complete job/quader-backup-verify --timeout=12h   # example
kubectl -n quader logs job/quader-backup-verify
kubectl -n quader delete job quader-backup-verify
```

The same pattern runs `backup list` and `backup delete <id> --yes`. Without `--deep`, `verify` reads the manifest, hashes the table files and the blob list, and asks for every blob object by size; with `--deep` it downloads every object and recomputes its SHA-256, throttled by `--bytes-per-second`. Findings are printed one per line (`missing`, `size_mismatch`, `digest_mismatch`, `file_checksum_mismatch`) and the command exits `5`. An encrypted blob in the pool cannot be hashed without its key: `--deep` checks its record and size from the object's head instead, downloads nothing of it, and prints `not_authenticated <n>`, which is no finding. `--deep --key-ring <dir>`, with a directory of the key ring's `*.key` files, opens the backup's storage keys and decrypts every encrypted pool object against its digest; one that does not decrypt is a `digest_mismatch`. The backup Jobs do not mount the key ring, on purpose; run that verification from a pod you give both the key ring and the target's credentials, or rely on the restore drill, which reads every encrypted pool object through the key ring. A run of `verify` is recorded, and `QuaderBackupVerifyFindings` fires while the newest one has findings. Verification does not run on a schedule; start it yourself.

`verify` proves that a backup is complete and readable, not that the installation comes back. The only proof of that is a restore into a second namespace with a fresh database and an empty blob bucket; [Restore a backup](#restore-a-backup) is the same procedure.

`backup list` prints one line per backup, newest first: the ID, `complete` or `incomplete` (`--all` adds the incomplete ones), the snapshot time, the Quader version, the schema step, the sizes, the key IDs, and whether it records damaged blobs. A backup directory without a `manifest.json` is incomplete and can never be restored; that is also how to read the target when the installation is gone: list `<prefix>/backups/*/manifest.json` with your object store's tools. The directory name is the backup ID, of the form `20261028T020000Z-7f3a`.

## Restore a backup

A restore goes into a new, empty installation: an empty database schema and an empty prefix on every object-store connection. Restoring onto a prefix that holds objects is not built. The restore applies the migrations itself and replays the revocation journal, so no migration Job and no `post-restore` is needed.

### Preconditions

`quader backup restore` checks these before it writes anything and exits `6` naming the first that fails:

1. The manifest and every file it names are in the target.
2. The image embeds every migration step of the backup with the same SHA-256. Before the first stable release a migration file can change between builds, so restore with the version shown in `quader backup list`; the message names that version.
3. The key ring holds every key ID of the backup and opens every canary row: `secrets.encryption` must hold the keys the backup was sealed with, not only today's. It also opens every storage key the backup holds; a backup whose pool names a storage key it does not hold, or holds as destroyed, is refused here, and so is an encrypted connection whose id the backup holds destroyed keys of.
4. The schema `database.schema` (`quader` by default) exists and holds no relation. The restore does not create it: create it first, for example `CREATE SCHEMA quader AUTHORIZATION quader` in a new database.
5. The `prefix` of every element of `storage.connections` is empty in its bucket, apart from `_probe/` and the connection marker `_connection`. The marker may stay only when it names the installation this backup belongs to (a restore keeps that installation's id) and that connection; a marker of another installation or connection fails the check, and the message names the connection and says whose the marker is.
6. PostgreSQL is a supported version and the login owns the schema: `secrets.databaseMigration` when set, otherwise `secrets.database`.
7. The target and every element of `storage.connections` are reachable with the configured credentials; the restore writes one probe object to each connection and nothing to the target. It writes every blob into the first connection without `drain: true` and records the blobs as copies there; the other connections receive their copies from the reconciliation once the replicas run.
8. The revocation journal of at least one of the source's connections can be listed, or `restore.skipRevocationJournal: true` is set. The restore reads the journal of every source connection it can list. See [What a restore does to access removed after the restore point](#what-a-restore-does-to-access-removed-after-the-restore-point).

`restore.extraArgs: [--check]` runs these checks without changing anything: `kubectl -n quader logs job/quader-restore` shows the result. Delete the Job afterwards, because a Job's pod template cannot be changed by a later upgrade: `kubectl -n quader delete job quader-restore`.

If the old installation is still running:

- suspend its backup CronJob (`backup.suspend: true`) before you start: its retention could delete the backup you restore, and the restore would then stop at a missing object;
- stop its Deployments before clients are switched over, so nothing writes to it afterwards;
- keep its database and bucket until the restored installation is verified, and do not point the restored installation at the old blob prefix. A shared Valkey is harmless, because the restored installation uses a new keyspace.

### Restore

Install the chart with the normal values, every role at zero and `restore.enabled` set. The chart refuses a replica count above zero and autoscaling while `restore.enabled` is set, and names the value it refuses.

```shell
helm repo add quader https://guided-traffic.github.io/quader/
helm repo update
helm install quader quader/quader -n quader -f values.yaml --version <version> \
  --set api.replicaCount=0 --set api.autoscaling.enabled=false \
  --set packages.replicaCount=0 --set packages.autoscaling.enabled=false \
  --set worker.replicaCount=0 --set worker.autoscaling.enabled=false \
  --set restore.enabled=true --set restore.backupId=20261028T020000Z-7f3a   # example: the backup ID
kubectl -n quader wait --for=condition=complete job/quader-restore --timeout=24h  # example: repeat when it times out
helm upgrade quader quader/quader -n quader -f values.yaml --version <version>      # the normal values: starts the roles
```

Set the same zero on every entry of `api.pools`, `packages.pools`, `worker.pools` and on endpoint pools you configured. The restore Job is an ordinary Job, so no Helm or GitOps timeout limits it; `kubectl wait` bounds only how long your shell waits. While `restore.enabled` is set, the chart renders no migration Job and suspends the backup CronJob.

The Job copies the blobs, loads the tables in one transaction, applies the later migrations, replaces the keyspace and the installation's incarnation, and raises the revision columns so that a client's `ETag` from after the snapshot cannot match restored content. It then replays the revocation journal and verifies every blob. It prints a summary and the next steps. A restore that is evicted, killed or loses its database connection resumes in the Job's next pod from the point it reached.

**A Job that failed for good** (retries used up, or exit code `2`, `3`, `4` or `6`, which are not retried) is continued after you fix the cause: `kubectl -n quader delete job quader-restore`, then the `helm upgrade` with the restore values again. Its pod resumes. To start over instead, drop and recreate the schema and empty the blob prefix.

### After the restore

- **Back up again.** The first backup of the restored installation into the old target exits `4`, because the target names the old incarnation. Set `backup.adoptTarget: true` once, or give the installation a new target. The Backups screen shows a notice naming the backup and its snapshot time.
- **Clients.** With the same `server.base_url`, tokens that existed at the snapshot keep working unless the journal replay revoked them; tokens created later answer `401` and must be issued again; packages published after the snapshot are gone; everyone signs in to the UI again. Clients that cached a package document revalidate and get the restored state.
- **Review.** Read the output of the replay: [What a restore does to access removed after the restore point](#what-a-restore-does-to-access-removed-after-the-restore-point). The upstream, storage and format settings of repositories are not journaled and are back at the snapshot's values ([Known limitations](limitations.md#backup-and-restore)).
- **NuGet.** Versions deleted after the snapshot are back and listed as `revived`: delete again what must stay deleted. A version published after the snapshot keeps its number reserved for the same bytes, so a rebuild of that version is refused and needs a new number.

## Restore to a point in time

This restores PostgreSQL through your platform to a time *t*, without a Quader backup. It needs a platform that keeps a recovery window, and the bucket of every object-store connection either versioned or a garbage-collection grace period that covers the window.

### Prepare the platform

| What | How |
|---|---|
| PostgreSQL | Continuous archiving to a bucket other than the connections' buckets, with different credentials: with CloudNativePG the Barman Cloud plugin, on a managed service its point-in-time recovery |
| The bucket of each connection | Versioning with noncurrent expiry of at least the recovery window, or a `storage.gc.grace_period` of at least the window |
| Object-store credentials | The runtime credential must not hold `s3:DeleteObjectVersion`, `s3:PutBucketVersioning`, `s3:PutLifecycleConfiguration` or `s3:PutBucketPolicy`, so a compromised replica can create delete markers but cannot destroy history |

Declare the window in the configuration so that Quader checks it:

```yaml
config:
  backup:
    recovery_window: 168h          # example: the window your platform keeps; default 0s = undeclared
    object_versioning: true        # example: true asserts versioning with noncurrent expiry of at least recovery_window; default false
```

With `recovery_window` above `storage.gc.grace_period` (24 h by default) and `object_versioning: false`, a replica refuses to start and names the three keys. The daily task `backup-check`, and on demand `quader admin backup check`, report `archive_mode`, `object_versioning`, `noncurrent_expiry` and `grace_period`; a failed declared check fires `QuaderBackupCheckFailed` and the command exits `5`. The check reads settings; it does not prove that a restore works.

### Restore

Let *t* be the restore point, as an RFC 3339 time such as `2026-10-28T09:15:00Z`.

1. **Stop writers and backups.** The hold lets the chart accept zero replicas and suspends the backup CronJob; it refuses while any role has a replica or an autoscaler. Set the same zero on every entry of `api.pools`, `packages.pools`, `worker.pools` and on endpoint pools.

   ```shell
   helm upgrade quader quader/quader -n quader -f values.yaml --version <version> \
     --set postRestore.hold=true \
     --set api.replicaCount=0 --set api.autoscaling.enabled=false \
     --set packages.replicaCount=0 --set packages.autoscaling.enabled=false \
     --set worker.replicaCount=0 --set worker.autoscaling.enabled=false       # example
   kubectl -n quader delete job -l app.kubernetes.io/component=backup --ignore-not-found
   ```

   Wait `system.heartbeat_stale_after` (30 s by default).
2. **Restore PostgreSQL into a new cluster at *t*.** With CloudNativePG, a new `Cluster` with `bootstrap.recovery.source` and `recoveryTarget.targetTime` set to *t*; it never restores in place. On a managed service, its point-in-time recovery to a new instance.
3. **Point the release at it and run the clean-up.** In one upgrade, name the new database and the restore point. Keep the hold and the zero replicas; the chart refuses `postRestore.enabled` with a replica running or with `restore.enabled` set.

   ```shell
   helm upgrade quader quader/quader -n quader -f values.yaml --version <version> \
     --set postRestore.hold=true \
     --set api.replicaCount=0 --set api.autoscaling.enabled=false \
     --set packages.replicaCount=0 --set packages.autoscaling.enabled=false \
     --set worker.replicaCount=0 --set worker.autoscaling.enabled=false \
     --set config.database.host=quader-db-restored-rw.quader.svc \
     --set postRestore.enabled=true \
     --set-string postRestore.restoredTo=2026-10-28T09:15:00Z                # example: the new database and t
   kubectl -n quader logs -l app.kubernetes.io/component=post-restore --tail=-1
   ```

   If the new cluster has its own credentials or CA, change `secrets.database` and `secrets.databaseCA` with it. The encryption key Secret stays the same. The Helm hook Job `<fullname>-post-restore-<hash>` runs `quader admin post-restore --restored-to <t>`; Helm waits for it and fails the upgrade if it fails. It deletes the replica rows, returns running jobs to the queue, sets the deletion pause, ends every UI session, gives the installation a new keyspace and incarnation, raises the revision counters, verifies every live blob, replays the revocation journal entries newer than *t*, and writes `system.restored`. The replicas stay not ready until the Job has finished. A repeat with the same *t* changes nothing; a repeat after a failed run resumes at the replay; a different *t* is refused while one is unfinished.
4. **Repair missing blobs**, if step 3 reported any and the bucket is versioned. `quader admin storage restore-versions --before <t>` removes the delete markers younger than *t* from the keys of the copies without an object, on every connection or the one `--connection <id>` names, so that the version below the marker is current again. It needs, for each connection with a copy to repair, a login that may read that connection's bucket and holds `s3:ListBucketVersions` and `s3:DeleteObjectVersion`, passed only to this command as that connection's element of `--credentials-file`: the runtime credential deliberately lacks both. The chart renders no Job for it; the simplest way is to give the post-restore Job such logins (`postRestore.journalCredentials.source: static` and the `journalReader` of each entry of `secrets.storageConnections`), then run the command as a one-off Job from that Job's pod template, which mounts the credentials file:

   ```shell
   kubectl -n quader get jobs -l app.kubernetes.io/component=post-restore -o json \
     | jq '.items[0] | {apiVersion: "batch/v1", kind: "Job", metadata: {name: "quader-restore-versions"},
           spec: {backoffLimit: 0, template: (.spec.template
             | del(.metadata.labels["controller-uid", "batch.kubernetes.io/controller-uid", "job-name", "batch.kubernetes.io/job-name"]))}}
         | .spec.template.spec.containers[0].args = ["admin", "storage", "restore-versions",
             "--before", "2026-10-28T09:15:00Z", "--credentials-file", "/etc/quader/post-restore/credentials.yaml"]' \
     | kubectl -n quader create -f -                                          # example: t and the Job name
   kubectl -n quader wait --for=condition=complete job/quader-restore-versions --timeout=4h
   kubectl -n quader logs job/quader-restore-versions
   ```

   Add `--dry-run` to see what it would do. It prints one line per copy and a totals line, exits `5` while a blob has no copy with its object left (no delete marker younger than *t*, or no version below its markers), and writes `system.storage.versions_restored` when it removed markers. The credentials file is used on every connection of the run: when the connections need different logins, run the command once per connection with `--connection`. Run `quader admin storage verify` the same way before you continue.
5. **Start.** Run `helm upgrade` with the normal values, which ends the hold and resumes the backup CronJob. Your values must now name the new database; keep `config.database.host` as you set it in step 3. The migration Job brings an older restored schema forward.
6. **Verify, then resume deletions.** Check that what you expect is there. Garbage collection and the orphan sweep have been held back since step 3; resume them only after verifying:

   ```shell
   kubectl -n quader exec deploy/quader-worker -- quader admin maintenance resume-deletions   # example: a worker pod holds the release's configuration
   ```

   It prints when and why the pause was set and writes `system.deletions_resumed`; a repeat says that nothing changed. Objects written after *t* have no row in the restored database and are removed by the orphan sweep after `storage.orphans.min_age` (24 h by default), once deletions run again.

The same deletion pause can be set by hand before a planned drill, with `quader admin maintenance pause-deletions --reason <text>`: garbage collection, the orphan sweep, retention and cap enforcement delete nothing until it is cleared.

## What a restore does to access removed after the restore point

A database restored to *t* holds its tokens, users, group memberships, role bindings and policies as they were at *t*. A token revoked after *t* would work again, and a disabled user could sign in. Both restore paths therefore replay the **revocation journal**: a second record of every access removal, written to `_journal/revocations/` in the blob bucket, outside the database. `backup.revocation_journal` is on by default.

After a replay, a token revoked after *t* is rejected with `401`. The replay also re-applies disables of users and service accounts, removed group memberships and role bindings, deleted and narrowed policies, disabled CI issuers and mappings, narrowed repositories and download blocks, and it withholds npm versions, OCI manifests and Debian package files deleted after *t*. It never grants access and it deletes nothing. UI sessions end in both paths.

Each entry is one line of the output:

| Line | Meaning | What to do |
|---|---|---|
| `applied` | The removal took effect on the restored database | Nothing |
| `already in effect` | The restored database already had it | Nothing |
| `subject unknown` | The object is not in the restored database | Nothing, unless you expected it |
| `skipped` | An update that only added access, or an unknown event type | Grant the access again if it should exist |
| `suspended` | An update Quader could not apply without granting more than it should: a policy loses its entries, an issuer or mapping is disabled, a repository is disabled | Read the printed entry, correct the object, and enable it again ([After a restore](repositories.md#after-a-restore)) |
| `revived` | A NuGet version the journal records as deleted is present | Delete it again if it must stay deleted |
| `unreadable` | A journal object could not be read; the line names its key and the reason | Treat the entry as not applied, and repeat the removal by hand if it should hold |

If the replay leaves no administrator, the output says so; sign in with the break-glass account. `quader admin npm reinstate`, `oci reinstate` and `deb reinstate` put back a version, manifest or package file that a replay withheld, when its deletion was a mistake.

**What the replay cannot do.** These are the cases in which a revoked token can work again:

- entries not yet shipped when the old database stopped, normally less than one `backup.revocation_journal.ship_interval` (5 s by default); `QuaderRevocationJournalBacklog` warns of a backlog;
- a removal made while `backup.revocation_journal.enabled` was `false`;
- a journal that is gone with the buckets of every connection, or skipped with `--skip-revocation-journal` (`restore.skipRevocationJournal`, `postRestore.skipRevocationJournal`), which the restore records in `system.restored`;
- without bucket versioning, journal objects deleted with the runtime credential: give the restore a login that lists object versions (`restore.journalCredentials`, `postRestore.journalCredentials`) so a delete marker cannot hide an entry;
- a row that could not be written to the journal: `QuaderRevocationJournalQuarantined` fires and `quader admin post-restore` reports it.

For those, read the `log: audit` stream from *t* to the restore and repeat each revocation and removal with `quader admin token revoke` and the management API. Access granted after *t* is not restored in any case: grant it again.

## Read an encrypted bucket without Quader

When the database is lost and no backup holds the blobs, `quader admin storage offline` reads an encrypted connection's objects without a database, Valkey or a running Quader, from any machine with the `quader` binary. It needs three things you keep yourself: the export of the storage keys, the directory of the key ring files that sealed it (your copy of `secrets.encryption`, one `<key id>.key` file per key), and read access to the bucket, given as one element of `storage.connections` in a file. It writes nothing to the bucket, and it takes the keys only from those files, never from a command-line argument.

```yaml
# archive.yaml: the connection's element, as in config.storage.connections
id: archive                                          # example: the connection's id
bucket: quader-archive                               # example
endpoint: https://s3.example.com                     # example
credentials_source: static                           # example
access_key_id_file: /secure/archive/access-key-id    # example: a login that may read the bucket
secret_access_key_file: /secure/archive/secret-access-key   # example
encrypted: true
```

```shell
quader admin storage offline list   --connection-file archive.yaml --keys quader-storage-keys.json --key-ring keyring/   # one line per object
quader admin storage offline verify --connection-file archive.yaml --keys quader-storage-keys.json --key-ring keyring/   # reads every object whole
quader admin storage offline get 3f9a…c2e1 --out artifact.tgz --connection-file archive.yaml --keys quader-storage-keys.json --key-ring keyring/   # example: one blob by its SHA-256
quader admin storage offline pull   --connection-file archive.yaml --to archive-copy/                                   # a copy with every object's record
```

- **`list`** prints one line per object below `keyed/`, `sha256/` and a backup target's `pool/sha256/`: `ok` when its record opens with the export and its name belongs to its content, `name_mismatch`, `unknown_key` when the export lacks the key its record names (use a newer export), `destroyed_key` when the export lists that key as destroyed, or the record's refusal.
- **`get`** writes one blob into `--out`, a file that must not exist. The file appears only after every segment and the SHA-256 were checked; a damaged object writes nothing and exits `5`. Until then the bytes go to `.<file>.partial-…` beside it, which a `get` that was killed leaves behind: delete it.
- **`verify`** reads every object whole and exits `5` when one is damaged or unreadable with the export.
- **`pull`** copies the objects with their records into an empty directory; `list`, `get` and `verify` then read it with `--dir archive-copy/` in place of `--connection-file`. It needs no key.

A key ring that lacks a key the export was sealed with exits `2` and names it. An unencrypted connection reads too: its objects show as `plain` and are checked against the digest their names carry.

## Reference

<details>
<summary>The commands</summary>

Every command takes `--config <file>` (the chart mounts it at `/etc/quader/quader.yaml`) and `--log-level <level>`. The backup commands run in a Job from the CronJob, because only those pods hold the target's credentials. `quader admin` commands run in a worker pod (`kubectl exec deploy/<fullname>-worker -- quader admin …`), or, while the roles are at zero, in a Job built from the post-restore Job as in [Restore to a point in time](#restore-to-a-point-in-time).

| Command | What it does |
|---|---|
| `quader backup create [--trigger schedule\|manual] [--exclude-remote-caches] [--no-prune] [--adopt-target] [--json]` | Writes one backup to the target and applies the retention |
| `quader backup list [--all] [--json]` | Lists the backups in the target |
| `quader backup verify <id> \| --latest \| --all [--deep [--key-ring <dir>]] [--bytes-per-second N] [--json]` | Checks backups without changing them; `--key-ring` decrypts the encrypted pool objects |
| `quader backup delete <id> [--yes] [--allow-last]` | Deletes one backup regardless of the retention; the last complete backup needs `--allow-last` |
| `quader backup prune` | Applies the retention policy and sweeps the pool |
| `quader backup restore <id> [--check] [--skip-revocation-journal] [--journal-credentials-file <path>]` | Restores into an empty installation |
| `quader admin post-restore --restored-to <t> [--credentials-file <path>] [--skip-revocation-journal] [--json]` | The clean-up after a point-in-time restore |
| `quader admin backup check [--json]` | Reports the declared platform protection; exits `5` when one does not hold |
| `quader admin storage verify [--connection <id>] [--since <t>] [--concurrency N] [--max-items N] [--json]` | Checks that every copy of every live blob has its object with the right size, on each connection; lists the missing copies and exits `5` when a blob has no copy left (with `--connection`: when a copy there is missing) |
| `quader admin storage restore-versions --before <t> [--connection <id>] [--credentials-file <path>] [--dry-run] [--concurrency N] [--json]` | Removes delete markers younger than *t* from the copies without an object |
| `quader admin maintenance pause-deletions --reason <text> [--json]` | Sets the deletion pause |
| `quader admin maintenance resume-deletions [--json]` | Clears the deletion pause |
| `quader admin npm reinstate`, `oci reinstate`, `deb reinstate` | Put back a version, manifest or package file a replay withheld |

`--credentials-file` and `--journal-credentials-file` name a YAML file with a list `connections`, one element per connection with the keys `connection` (its id), `credentials_source` (`default`, `webIdentity` or `static`; `default` when left out), `access_key_id_file`, `secret_access_key_file`, `session_token_file`, `role_arn`, `web_identity_token_file` and `sts_endpoint`; an unknown key is refused. A connection the file names is read with that login and its object versions are listed; any other is read with the blob store's credential, which lists no versions. `storage verify` and `storage restore-versions` work on every element of `storage.connections`, or on the one `--connection` names.

Exit codes of the backup commands:

| Code | Meaning | Commands |
|---|---|---|
| `0` | Success, also with warnings | all |
| `1` | Failed: details in the log and the run | all |
| `2` | Usage or configuration error | all |
| `3` | Another target-writing run is alive | `create`, `delete` |
| `4` | The target belongs to another installation or incarnation, or adoption refused | `create`, `delete` |
| `5` | Findings: objects missing, damaged or mismatching | `verify` |
| `6` | Restore precondition not met, or an unknown manifest format | `restore`, `verify` |
| `75` | Temporary: database or target unreachable, lock or wait timed out, a restore in progress, stalled, interrupted | all |

Exit codes `2`, `3`, `4` and `6` end the Job at once; `1` and `75` are retried within `backoffLimit`, and a drain or eviction does not count against it. A run that makes no progress for `config.backup.stall_timeout` (30 minutes by default) ends itself.

</details>

<details>
<summary>The chart values</summary>

| Value | Default | Meaning |
|---|---|---|
| `backup.enabled` | `false` | Renders the CronJob; needs `config.backup.target.bucket` |
| `backup.schedule`, `backup.timeZone` | `0 2 * * *`, `Etc/UTC` | When it runs |
| `backup.suspend` | `false` | Pauses the CronJob; rendered `true` while `restore.enabled`, `postRestore.enabled` or `postRestore.hold` is set |
| `backup.adoptTarget` | `false` | Adds `--adopt-target`, to take over the target of the installation this one was restored from |
| `backup.backoffLimit`, `backup.activeDeadlineSeconds` | `2`, `0` (not rendered) | Retries of a run; a wall-clock limit |
| `backup.serviceAccount.*`, `backup.resources` | a ServiceAccount of its own; `500m`/`256Mi` requests and `1Gi` limit | The pod's identity and size |
| `restore.enabled` | `false` | Renders the Job `<fullname>-restore`, no migration Job and a suspended CronJob; every role must be at zero |
| `restore.backupId` | empty | Required with `restore.enabled` |
| `restore.skipRevocationJournal` | `false` | Skips the journal replay, only when the source bucket is gone |
| `restore.extraArgs` | `[]` | Appended to `quader backup restore`, such as `[--check]` |
| `restore.journalCredentials.source` | `runtime` | `runtime` (the blob store's credential, a plain listing), `default` (the Job's identity lists versions), `static` (the `journalReader` of each entry of `secrets.storageConnections`) |
| `restore.backoffLimit`, `restore.activeDeadlineSeconds` | `2`, `0` (not rendered) | Retries; a wall-clock limit |
| `postRestore.hold` | `false` | Allows every role at zero and suspends the backup CronJob, with no Job |
| `postRestore.enabled` | `false` | Renders the post-install and post-upgrade hook Job |
| `postRestore.restoredTo` | empty | Required with `postRestore.enabled`: the time *t*, RFC 3339 |
| `postRestore.skipRevocationJournal` | `false` | Skips the replay, only when the source bucket is gone |
| `postRestore.journalCredentials.source` | `runtime` | As for `restore`; with `default` or `static` the Job lists object versions |
| `postRestore.backoffLimit`, `postRestore.activeDeadlineSeconds` | `2`, `0` (not rendered) | Retries; a wall-clock limit |
| `secrets.backupTarget`, `secrets.databaseBackup`, and the `backupSource` and `journalReader` of each entry of `secrets.storageConnections` | no Secret | The credentials of [Set up the backup](#set-up-the-backup) and of the journal reader; each reaches only the Jobs that need it |

</details>

## What this page does not cover

- Verifying a backup on a schedule, restoring one repository or one package, and restoring onto a blob prefix that already holds objects are not built. Neither is a backup target other than S3.
- A backup proves completeness through checksums in its manifest, not authenticity: whoever can write the target can forge a consistent backup. Object Lock and restricted writers are the mitigation.
- Fail-over to a second site is outside this page. Restoring a backup is also how you move to a new cluster, a new PostgreSQL major version or another S3 store.
