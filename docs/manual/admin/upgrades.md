# Upgrades

This page explains how Quader versions its releases, what an upgrade can and cannot keep before the first stable release, how to upgrade an installation with Helm, how the database migrations run during an upgrade, which corrections of stored data run afterwards, what to check afterwards, and which parts of the upgrade path are not verified yet.

## Versions and release notes

Quader is released from its main branch by an automated pipeline that derives the version from the commit messages:

| Change in the release | Version step |
|---|---|
| A fix or a performance improvement | patch, for example `0.5.0` to `0.5.1` |
| A new feature | minor, for example `0.5.1` to `0.6.0` |
| A breaking change | minor as well, while the major version is 0 |

Quader is pre-1.0 and stays on major version 0 until a 1.0 release is decided. Under major version 0 a minor release can contain a breaking change, such as a renamed configuration key or a changed default. The release notes, generated from the same commit messages, list breaking changes in a section of their own. Read the notes of every release between your version and the target on the [releases page](https://github.com/guided-traffic/quader/releases) before you upgrade. Automated dependency updates are released as patch releases.

The image `docker.io/guidedtraffic/quader` and the Helm chart carry the same version as the release. A chart installs the image of its own version unless you set `image.tag` or `image.digest`, so upgrading the chart upgrades Quader.

## Before the first stable release

Quader is pre-alpha, and until its first stable release it keeps no backward compatibility. Plan every upgrade with this in mind:

- **An upgrade may require a fresh database.** A release may change a migration that an earlier release already applied. The migration Job then refuses your database with `migration <name>.sql was changed after this database applied it (checksum mismatch)`, applies nothing, and `helm upgrade` fails while the old pods keep serving. The way forward is a new, empty database for the new release; the data of the old one is not carried over.
- **No rollback is supported.** Once the migration Job of the new release has run, nothing guarantees that the previous release works against the schema it left. `helm rollback` is not a way back; the backup you took before the upgrade is (see [Rolling back](#rolling-back)).
- **Old and new pods are not guaranteed to work side by side.** During the rolling update both versions serve requests, and a change between them may break requests for that time. Upgrade in a maintenance window if an interruption matters to you.
- **Anything can change.** A release may rename or remove a configuration key or a chart value, change the Management API or change a stored format, without a deprecation period or an alias. Read the release notes, as above.

This ends with the first stable release.

## Before you upgrade

1. Read the release notes, as above, and adjust your values file to any renamed or removed key.
2. Back up the PostgreSQL database with the tools of your database platform, such as the backups of [CloudNativePG](https://cloudnative-pg.io/documentation/). Migrations are not reversible. Quader's own backup ([Backups](operations.md#backups)) restores into a new, empty installation and is no rollback of an upgrade, so back up the database here as well. The object store needs no extra step: migrations do not touch it.
3. Check that the new chart accepts your values. Rendering runs the chart's schema and its checks without changing anything:

   ```shell
   helm repo update quader
   helm search repo quader/quader --versions
   helm template quader quader/quader -n quader -f values.yaml --version <version> > /dev/null   # example: the target version
   ```

   This does not check the keys inside `config`; the new binary checks them when the migration Job starts (see below).
4. Check the background data migrations of the running version. A migration of the target that removes stored data can depend on one of them; its release notes name it, and it must show `succeeded`:

   ```shell
   curl -s -H "Authorization: Bearer $QUADER_TOKEN" https://quader.example.com/api/v1/system/migrations   # example: a token with settings:read and your base URL
   ```

   The answer lists each background migration under `background` with its `state` (`pending`, `running`, `failed` or `succeeded`), the rows it has `processed` and when it started. The migration Job of the target checks the same: it refuses such a migration while the background migration it depends on has not succeeded (exit status 3, nothing applied), and the previous version keeps serving. No migration of this version depends on a background migration.

## Upgrade with Helm

```shell
helm upgrade quader quader/quader -n quader -f values.yaml --version <version>     # example: the target version
kubectl -n quader logs job/quader-migrate
kubectl -n quader rollout status deployment/quader-api
kubectl -n quader rollout status deployment/quader-packages
kubectl -n quader rollout status deployment/quader-worker
```

`helm upgrade` runs these steps in order:

1. Helm validates the values. A value the new chart does not know, or a required value that is missing, stops the upgrade before anything changes.
2. Helm deletes the previous migration Job and starts a new `quader-migrate` Job with the new image and the new configuration. With `secrets.databaseMigration` set, the Job signs in as that schema owner.
3. The Job loads the configuration (an invalid key stops it here), checks that the database's `max_connections` covers the connection pools of the configured replica counts, takes the migration lock, waiting up to `migrate.advisory_lock_wait` (5 minutes) for another run to finish, and applies every pending migration in order. It exits successfully when nothing was pending.
4. Only after the Job has succeeded does Helm update the Deployments. Kubernetes then replaces the `api`, `packages` and `worker` pods one at a time: a new pod is started before an old one stops, and an old pod drains its requests or jobs before it exits.

If the Job fails, `helm upgrade` fails and the Deployments stay as they were: the replicas of the previous version keep serving. The Job retries a failed run twice (`migrations.backoffLimit`) within 30 minutes (`migrations.activeDeadlineSeconds`); a run that a node drain evicts does not count as a failure, and a refusal (exit status 3) fails the Job at once because retrying cannot change it. The log of the Job names the cause.

### The web UI during the rollout

While the rollout runs, a browser's requests reach pods of both versions, and each version's UI consists of files named after their content. So that a page loads whichever pod answers, every `api` pod stores the UI files of its version in the object store before it reports ready, and serves the files of the other version from there. You need to do nothing for it:

- A new `api` pod logs `the UI's hashed files are in the shared asset store` with the number of files once it has stored them. The first pod of a new version stores about 270 files (about 5 MB in both languages); the pods after it find them there.
- The files of a version stay in the object store for 7 days after its last pod stopped, so that a browser tab opened before the upgrade keeps working, and are then removed by the garbage collection of the object store. The database table `ui_assets` names them; it is not part of a backup, and the pods store their files again after a restore.
- If the object store cannot be reached during the rollout, a page of one version can fail to load a file from a pod of the other one: the browser shows an empty page, the pod answers `503` and logs `a hashed file of another release could not be read from the shared asset store`. Reloading the page fixes it. A pod that cannot store its files within a minute starts anyway and logs an error; it tries again every minute.
- The first version that has this store cannot serve the files of the version you upgrade from, which never stored them: during that one upgrade a browser can still show an empty page once, until it is reloaded. Upgrades from that version on are not affected.

### Without the migration hook

With `migrations.mode: disabled` the chart renders no Job, and you run the migration yourself, with the new image and the same configuration and database credentials, before you roll the Deployments:

```shell
quader migrate up --config /etc/quader/quader.yaml      # example path; prints a JSON summary on success
```

A new replica that starts before the migration has run stays unready and waits until the schema is current.

## How the migrations work

- The migrations are embedded in the binary and numbered. Each runs in its own transaction and is recorded with a checksum; a recorded migration whose file has changed is refused.
- One run at a time: the migration lock is a PostgreSQL advisory lock, so a second Job or a manual run waits for the first.
- A migration may add, drop, rename or tighten anything. Before the first stable release nothing keeps a replica of the previous version working after it (see above).
- A migration can carry the minimum Quader version it needs. The runner refuses it (exit status 3, nothing applied) while a live replica runs an older version, and once it is applied, a replica of an older version exits with `schema requires Quader >= <version>`. One migration of this version carries such a minimum, `0.1.0`, which every release meets.
- A migration that removes something a background data migration converts also names that background migration. The runner refuses it (exit status 3, nothing applied) until the background migration has succeeded, and names it with its state.
- A replica checks the schema when it starts and keeps checking it: if the schema is behind the newest migration its binary embeds, the replica stays or becomes unready until the migration has run.

`GET /api/v1/system/migrations` shows the state without taking the lock, to a caller with `settings:read`: the schema version and its compatibility floor, the version and required schema of the replica that answers, the migrations that replica's binary has and the database has not applied (`pending`, each with what the runner checks before it), every applied migration with its release and time, and the background data migrations with their states. During a rolling upgrade, replicas of both versions answer, each for its own binary. `quader migrate status` prints the same report from the same code and adds whether that binary may serve:

```shell
curl -s -H "Authorization: Bearer $QUADER_TOKEN" https://quader.example.com/api/v1/system/migrations   # example: a token with settings:read
kubectl -n quader exec deploy/quader-worker -- /usr/local/bin/quader migrate status
```

## Background data migrations

A correction of stored data runs after the upgrade as a job on a worker, not in the migration Job, so the Job does not take longer with more data. This version has two.

### The sort keys of the versions

`data_migration_version_sort_key` stores, for every version stored before the upgrade, the key by which Quader orders a package's versions and finds its latest version. Until it has reached a version, the package screens and the API still list that version in its right place, only more slowly for a package with very many versions.

- The first worker that starts after the upgrade queues it. Pods of the previous version keep storing versions without the key while the rollout runs; a run that finds such versions left at its end says `state: waiting` and tries again a minute later. Once the rollout has finished, a run ends with `state: filled` and the number of versions it `filled`.
- Follow it in the tab **Jobs** of **Admin area › System › System status** with the kind `data_migration_version_sort_key`.
- `GET /api/v1/system/migrations` lists it under `background` as `version_sort_key`: `pending` until a worker runs it and while it waits for the next run, `running` while it runs, `failed` when its last run failed, and `succeeded` once its completion is recorded, with the number of versions it filled.
- After a rollback and an upgrade again, the first worker that starts finds the versions stored meanwhile and runs it again by itself.

### The SemVer 2 classification of NuGet versions

`data_migration_nuget_semver2` finds the NuGet versions stored by 0.9.0 or older that have a dependency range with a SemVer 2.0.0 bound, such as `[2.0.0-rc.1, )`, and marks them SemVer 2.0.0. NuGet clients older than 4.3 then no longer get these versions in the SemVer 1 registrations or in a search without `semVerLevel=2.0.0`.

- The first worker that starts after the upgrade queues it. While a pod of 0.9.0 or older is still live, it waits: the result says `state: waiting` and names those pods, and it tries again a minute later. Once the rollout has finished, it runs.
- Follow it in the tab **Jobs** of **Admin area › System › System status** with the kind `data_migration_nuget_semver2`. A run that reached the end shows `state: recomputed` with the number of versions it `examined` and `changed` and the `packages` they belong to. A version it could not read is counted as `unreadable` and named in the worker's log.
- `GET /api/v1/system/migrations` lists it under `background` as `nuget_semver2` (release `0.10.0`): `pending` until a worker runs it and while it waits for older pods, `running` while it runs, `failed` when its last run failed, and `succeeded` once its completion is recorded, with the number of versions it examined.
- It runs once per installation: Quader records the completion in the database, and a worker that starts later queues nothing. A package it changed gets a new revision, so clients and caches see the change.

## After the upgrade

1. `helm status quader -n quader` shows `deployed`, and `helm history quader -n quader` lists the new revision.
2. The Job log ends with a JSON summary: `applied` lists the migrations of this run, `nothingToDo` is `true` when none was pending, and `schemaApplied` is the schema version now.
3. `GET /api/v1/system/migrations` (see [How the migrations work](#how-the-migrations-work)) answers with the new version in `binary.version`, an empty `pending`, and every entry of `background` `succeeded`. A background migration that is still `pending` or `running` finishes on its own; one that shows `failed` has a failed job under **Admin area › System › Jobs** whose error names the cause.
4. Every pod of `quader-api`, `quader-packages` and `quader-worker` is ready and none restarts repeatedly.
5. **Admin area › System › System status** shows **All ready**; every replica in the replica table shows the new **Version**, and the **Database** part shows the new **Schema version**.
6. Sign in, open a repository and download a file, as a quick check of the full request path.

[Monitoring and operations](operations.md) describes the metrics and logs to watch.

## Bastions

The main installation and its [Bastions](bastion.md) talk to each other only when both speak the same version of the Bastion interface. A release whose notes say that it raises this version needs the main installation and every Bastion upgraded together: a replica of either side on another version treats the other as unreachable, serves what its cache and its answers allow for the grace period (`bastion.answers.grace_period`, default `15m`) and then answers `503`, and the alert `QuaderBastionInterfaceMismatch` fires. Upgrade the main installation first and the Bastions right after it, within the grace period; the **Admin area › Bastions** screen shows the interface version each replica reports.

| Interface version | What it changed |
|---|---|
| 7 | A Bastion's own refusals tell a NuGet or Ansible Galaxy client to wait at most 30 seconds, as the main installation does, and name the whole wait in their text. A main installation of version 7 and a Bastion of version 6 do not talk |
| 6 | Direct reads from the object store (the handover of large files) |

## Rolling back

Before the first stable release no rollback is supported. `helm rollback` runs no migration job: the migration hook runs on install and upgrade only. The schema therefore stays at the newer version, and the older pods start whenever the schema is at least the one their binary needs and no applied migration requires a newer Quader, but nothing guarantees that they work against the newer schema. To go back, restore the database backup you took before the upgrade along with the rollback; everything written to Quader since that backup is lost.

After a rollback to 0.9.0 or older, NuGet versions pushed during the rollback are classified the old way again, and `data_migration_nuget_semver2` does not run again by itself when you upgrade once more. Remove its record, then restart a worker pod, which queues it:

```shell
psql "$DATABASE_URL" -c "DELETE FROM quader.system_state WHERE key = 'nuget_semver2_recomputed'"   # example connection and schema
kubectl -n quader rollout restart deploy/quader-worker
```

## What is not verified yet

- **Mixed versions during the rollout.** For the length of a rolling update, replicas of the old and the new version serve requests side by side. Before the first stable release this is not supported (see above); if an interruption matters to you, upgrade in a maintenance window.
- **Skipping releases.** The runner applies every pending migration in order, so an upgrade across several releases is a single migration run. An upgrade path across several published releases is not covered by a test, and before the first stable release it may require a fresh database like any other upgrade.
- **The upgrade from the previous release.** A test upgrades the newest published release to each new version and rolls it back while clients work, but before the first stable release it runs only on request, not for every release, and it is left out for a version that changed a database migration the previous release had applied: such a version needs a fresh database.
- **Rollback**, which is not supported before the first stable release, as described above.

[Known limitations](limitations.md) lists the other gaps of this version.
