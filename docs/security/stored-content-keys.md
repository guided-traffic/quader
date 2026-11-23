# The keys of the stored content: who holds which, and what losing or leaking one costs

Status: specified on 2026-11-22 ([storage.md](../spec/storage.md) R-STOR-083 to R-STOR-085, R-STOR-089, R-STOR-090;
[architecture.md](../spec/architecture.md) R-ARCH-021; [backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-031;
[bastion.md](../spec/bastion.md) R-BAS-061). The Bastion's cache key is built ([security/bastion.md](bastion.md#with-a-cache-key)). The
keys of the main installation are built (package 2 of [stored-content-encryption.md](../developer/stored-content-encryption.md#package-2-the-keys-as-built),
2026-11-22): the rows of `storage_keys` with their two sealed columns, the drawing with its refusal, the keys a replica holds, the export,
the import, a new generation and `blob.Store.CopyDataKey`. `encrypted: true` is accepted since package 3 (2026-11-22): a replica draws the
keys of an encrypted connection at its first sight, wraps every object's data key under the active storage key, and seals an upload session's
data key into its row with the key ring ([storage.md](../spec/storage.md) R-STOR-080, R-STOR-081). The Helm chart passes `encrypted: true`
since package 6 (2026-11-23) and mounts nothing more for it, and `quader admin storage offline` opens the keys of an export with the files of
the key ring, outside any installation ([storage.md](../spec/storage.md) R-STOR-089). The destruction of a retired
connection's keys ([storage.md](../spec/storage.md) R-STOR-097 to R-STOR-103, Q-STOR-309 (b),
[ADR-255](../decisions.md#adr-255--the-storage-keys-of-a-retired-connection-can-be-destroyed-so-that-what-stays-at-a-provider-becomes-unreadable))
is specified, not built (package 7); its part in the backup, the storage key in each pool object's name and the manifest's `storageKeys`
([backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-032), is built with package 4 (2026-11-23). Since package 4 the restore Job
opens every storage key a backup holds with the key ring before it writes, and draws the keys of an encrypted connection the backup holds
none of (R-BAK-031).

The perspective of this page is the keys of encrypted connections: where each lies, which process opens it, how it rotates, and what its loss
or its leak costs and needs. The format they protect is [stored-content-format.md](stored-content-format.md).

## The hierarchy

```
 key ring (the root, files of a Secret)            pods api, packages, worker (web, all) and the restore Job
   │ seals, AES-256-GCM bound to table, column, row
   ▼
 storage_keys rows in PostgreSQL                   one storage key per encrypted connection and generation, one name key per connection
   │ storage key wraps, per object, with a salt       (export: the same rows, still sealed, in a file beside the ring's escrow copy)
   ▼
 object metadata: quader-wrap                      one data key per object written on a connection
   │ data key derives the segment key
   ▼
 object body: segments
```

| Key | Lies | Opened by | Rotates |
|---|---|---|---|
| The root (the key ring) | files of `secrets.encryption` in the pods that serve and in the restore Job; escrowed by the operator (R-BAK-028) | the pods it is mounted in, at start | the existing rotation: two rollouts, the re-encrypt task, the old key removed (R-STOR-084) |
| A storage key | one sealed row of `storage_keys`; every backup of the database; the export | each replica that builds the blob area, at start or when a record names it; kept in memory for the replica's life, or until it is destroyed | never itself; a further generation on command; its sealing at every rotation of the root; destroyed with every key of its connection on command once the connection is retired ([below](#destroying-a-retired-connections-keys)) |
| A name key | as a storage key | as a storage key | never; destroyed with its connection's storage keys |
| A data key | wrapped in its object's record, nowhere else | a writer for one write, a reader for one read; handed by `CopyDataKey` to a Bastion, that Bastion for one read ([bastion-handover.md](bastion-handover.md)) | never |
| An open session's data key | sealed in its `upload_sessions` row | the replica that serves a request of the session | resealed by the rotation like every sealed value |
| The Bastion's cache key | a Secret of the Bastion's chart (`secrets.cacheKey`), mounted as a file; the storage key and name key derived from it in each replica's memory for its life | the Bastion's pods, at start | by replacing the file, which empties the cache; the new replicas' sweeps delete the old tree ([H-1326](../spec/bastion.md#h-1326--after-a-key-change-the-old-tree-stays-readable-to-a-holder-of-the-old-key-until-the-sweep-has-deleted-it)) |

**Outside the installation:** `quader admin storage offline` (R-STOR-089), run by the operator on a machine of his choice, opens every sealed
key of the export it is given with the key ring files it is given, holds them in memory for the run and destroys them at its end; it takes
both from files, never from an argument, and prints no key ([H-1450](#h-1450--the-offline-tool-opens-every-key-of-the-export-on-the-operators-machine)).
Its `pull` holds no key.

**Who holds nothing:** the backup pods (they copy ciphertext with its record and confirm it by size, R-BAK-031), the scanners (they read
plaintext through Quader's scan listener), the migration Job, and the Bastion, which holds no storage key, no name key and nothing of the key
ring; the export holds sealed values only. With the handover switched on for its registration, a Bastion holds the data key of one object for
the length of one read ([bastion.md](../spec/bastion.md) R-BAS-076, [bastion-handover.md](bastion-handover.md)).

## Rotation, step by step

1. A new key file joins the ring; rollout. 2. It becomes active; rollout. 3. The re-encrypt task reseals every registered value, the storage
keys and name keys among them; the keys inside are unchanged, so nothing reloads and no object is read or written. 4. The old file goes once
no value names it; rollout. 5. A new export ([storage.md](../spec/storage.md) R-STOR-084).

The guards the key ring already has cover the new rows by the registry: the task refuses to run while a live replica's ring lacks a key that
seals one of them (`409 key_ring_incompatible`), and a replica whose ring cannot open a key that a sealed row names refuses to start (the
canary). A key drawn again after a lost database gets a new random identifier, so it never opens an object of its predecessor; a replica refuses
to draw keys over a location that already holds objects.

## Loss and leak

| Key | Its loss costs | Its leak, with what the holder needs besides, gives | The answer to a leak |
|---|---|---|---|
| The key ring | every blob of every encrypted connection, in its bucket and every backup | nothing without the database; with it, every storage key; with a bucket too, its blobs | rotate the root; with the database leaked too, replace every encrypted connection |
| A storage key | the objects it wrapped, unless another connection holds the blob | with read access to the bucket, those objects; with write access, objects that authenticate | a new connection filled with new data keys, the old one drained |
| A name key | nothing unreadable; new writes on the connection cannot be named | the confirmation of known files by listing | a new connection |
| A data key | its object | that object, with read access | none short of a new connection |
| The Bastion's cache key | the cache, which refills | the cache's content, with access to the cache store, until the old tree is swept | a new key; emptying the old tree at the provider is faster than the sweep |

## Destroying a retired connection's keys

Specified, not built ([storage.md](../spec/storage.md) R-STOR-097 to R-STOR-103,
[backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-032, [ha-and-helm.md](../spec/ha-and-helm.md) R-HA-054). `quader admin storage
keys destroy --connection <id>` destroys every key of a connection that is retired or unrecorded: every generation of its storage key and its
name key. The rows stay, `destroyed` and without their sealed values; a replica that still holds one of the keys destroys it at the change of
the area `storage_keys`; a read that meets a record under a destroyed key answers `destroyed_key` and marks the copy. The command refuses
while anything Quader still serves needs a key: a live replica that configures the connection, a copy, a queue row, an upload session, or a
retained backup whose pool names a key. It reports, and does not refuse for, the holders it cannot remove: the exports made before, the
retained backups whose database holds the keys sealed, PostgreSQL's own copies, and the data keys handed to a Bastion. A restore to a point
before the destruction destroys the keys again through the revocation journal.

**Who may run it.** Whoever may `kubectl exec` into a pod with the runtime database role: it needs no key ring and opens nothing. A writer of
the database could already set `sealed_key` to NULL, so the command adds no power beyond that role; its refusals protect copies, sessions and
retained backups. What remains is that such a user can destroy a retired connection's keys earlier than the operator wanted, cutting off a
recovery through the offline tool. No operation of the API destroys a key ([Q-STOR-410](../spec/storage.md#q-stor-410--the-operator-runs-the-destruction-at-a-pod-without-the-key-ring-and-no-api-operation-does)).

**The promise, in the operator's words.** This paragraph goes into the README's backup step and into the manual's section "Keep the keys" and
its page of the connections when package 7 builds the command ([storage.md](../spec/storage.md) R-STOR-090); until then it describes what
is specified, not what runs:

> When you leave a provider, drain its connection, remove it, and run `quader admin storage keys destroy`: Quader forgets that connection's
> storage keys, and what the provider still holds of it (objects nobody deleted, old versions, the provider's own copies) becomes ciphertext
> that neither the provider, nor whoever obtains it, nor Quader can read. It stays readable only to someone who has your key ring together
> with a copy of those keys made before the destruction: an export of the storage keys, a Quader backup, or a PostgreSQL backup or
> point-in-time archive that is still within its retention. Destroy those exports; the promise is complete once those backups have aged out,
> and the ciphertext itself stays at the provider until you empty the bucket.

What it does not hold is in the gaps below: [H-1370](#h-1370--an-export-made-before-the-destruction-opens-what-the-provider-keeps) to
[H-1375](#h-1375--a-forged-journal-entry-can-destroy-a-retired-connections-keys-at-a-restore-before-the-operator-does), and
[H-1308](#h-1308--leaving-a-provider-leaves-ciphertext-there-readable-until-the-connections-keys-are-destroyed) for the time before the
command runs.

## What this does not cover

- What a record and a segment withstand once their keys are safe: [stored-content-format.md](stored-content-format.md).
- [H-74](privilege-footprint.md#h-74--the-default-object-store-credential-chain-can-reach-node-metadata): the object-store credential chain;
  a bucket's credential is not a key of its content, and an encrypted connection's credential reads ciphertext only.

### H-1328 · The refusal over a used location sees current objects only

Component: the drawing of keys at a connection's first sight ([storage.md](../spec/storage.md) R-STOR-083,
[Q-STOR-326](../spec/storage.md#q-stor-326--a-replica-holds-the-keys-of-every-encrypted-connection-before-it-turns-ready-and-a-draining-one-draws-nothing)),
`LocationUsed` in [draw.go](../../internal/blob/keys/draw.go). Subject: the objects that keys of an earlier database wrapped and that are not
current. The listing that guards the drawing lists the current objects below `sha256/` and `keyed/`, one key each; a versioned bucket whose
earlier objects were all deleted (only noncurrent versions remain) looks empty, and a replica draws new keys over it. A later
`restore-versions` ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-025) makes such an object current again with a record that names a key the
database lacks: reads fall over and mark nothing, and `QuaderStorageUnknownKey` fires, until the export that holds the row is imported. Live only where a database was lost without its export, over a versioned bucket of an encrypted connection whose objects
were deleted. Listing the versions as well would need `s3:ListBucketVersions`, which the runtime credential does not have
([privilege-footprint.md](privilege-footprint.md)).

### H-1304 · Every pod that holds the key ring opens every blob of every encrypted connection

Component: the custody of the key ring ([architecture.md](../spec/architecture.md) R-ARCH-021) and of the storage keys
([storage.md](../spec/storage.md) R-STOR-083). Subject: every blob of every encrypted connection. The pods `api`, `packages` and `worker`
(`web` and `all` in development) and the restore Job hold the key ring, read `storage_keys` and keep every storage key of their configured
connections in memory; whoever runs code in one of them, or reads its memory, opens every blob. The cut of the roles narrows nothing here,
because `api` serves the UI's shared assets and avatars from the same connections. Live with `encrypted: true`; it is the same boundary the
sealed credentials have today, now with the content behind it.

### H-1305 · Losing the key ring loses every encrypted blob

Component: the root (the owner's decision 4 (a), [storage.md](../spec/storage.md) R-STOR-085). Subject: every blob of every encrypted
connection and every backup's pool objects from them. Today a lost key ring costs the upstream credentials and the signing keys, which can be
entered again; with an encrypted connection it costs every encrypted blob, and no backup brings them back, because every backup's storage keys
are sealed under the same ring. Live with `encrypted: true`; it is why a connection is not encrypted unless told (decision 6). The only
protection is the operator's escrow of the ring outside the cluster and away from the backup target ([backup-and-restore.md](../spec/backup-and-restore.md)
R-BAK-028), which the README and the manual say again ([storage.md](../spec/storage.md) R-STOR-090).

### H-1306 · An export opens only with the root key that sealed it

Component: the export of the storage keys ([storage.md](../spec/storage.md) R-STOR-085). Subject: a recovery that rests on an export. The
export holds the rows as they lie, sealed under the root keys it names; after a rotation's last step removes such a key, an older export opens
only with that key from escrow. An operator who keeps the old export and drops the old key has no export. Live after every rotation of the root
until the operator exports again (step 5 of R-STOR-084); the database's own backups are not affected, since their manifests list their root keys
and escrow keeps those (R-BAK-028).

### H-1307 · A leaked storage key is answered by a refill that takes as long as the data

Component: the answer to a leak ([storage.md](../spec/storage.md) R-STOR-085, R-STOR-087). Subject: the blobs of the connection whose storage
key leaked. A new generation protects nothing already written, so the answer is a new connection that the reconciliation fills with new data
keys; until the old connection is drained, its objects stay in its bucket under the leaked key, readable by a holder of that key and of read
access to the bucket, and noncurrent versions and backups' pool objects stay readable after the drain. The refill takes as long as copying the
connection's data through a worker (at `storage.reconcile.bytes_per_second`, 100 MiB/s by default: about three hours per TiB). Dormant until a
storage key leaks.

### H-1308 · Leaving a provider leaves ciphertext there, readable until the connection's keys are destroyed

Component: the keys of a retired connection ([storage.md](../spec/storage.md) R-STOR-083, R-STOR-097 to R-STOR-099, Q-STOR-309 (b)). Subject:
the objects a drained or abandoned connection leaves at its provider. Quader never deletes them: a drain deletes only the copies that have rows,
and `forget-connection` deletes none, so noncurrent versions, leftovers and the provider's own copies stay. From the connection's retirement
until `quader admin storage keys destroy` runs they are readable to whoever holds the database or a backup of it and the key ring. After the
destruction they are readable only to the holders of
[H-1370](#h-1370--an-export-made-before-the-destruction-opens-what-the-provider-keeps) to
[H-1372](#h-1372--postgresql-keeps-the-sealed-value-after-the-destruction) and
[H-1374](#h-1374--a-data-key-handed-to-a-bastion-outlives-its-storage-key). The objects, and what a lister learns of them (their count, sizes
and times, [H-1312](stored-content-visibility.md#h-1312--the-stored-size-gives-away-the-exact-plaintext-size)), stay at the provider until
the operator empties the bucket. Dormant until package 3 accepts `encrypted: true`; then live for every retired encrypted connection whose bucket
still holds objects, until its keys are destroyed (package 7, specified, not built).

### H-1309 · A replica's memory holds the storage keys for its life, and Go cannot erase a key

Component: the keys in memory ([storage.md](../spec/storage.md) R-STOR-083). Subject: the storage keys and name keys of the replica's
connections, and the data keys of its current transfers. A replica keeps the storage keys for its life and a writer or reader its data key for
one transfer; the writer overwrites its copy when it is done, but Go's AES implementation keeps an expanded key schedule in memory that no code
can erase, and the garbage collector may keep copies. A core dump, a memory read of the pod or its node yields them. Live with `encrypted: true`;
it is the same as for the key ring's own keys today. The destruction of a retired connection's keys (R-STOR-101) makes a replica destroy and
drop those keys at the change, but an AES key schedule already built for a transfer in flight survives the forgetting until the transfer ends
and the garbage collector frees it; no live replica configures a connection whose keys are destroyed (R-STOR-098), so this concerns a stale or
partitioned replica only.

### H-1310 · A database restored to a point before a generation was drawn cannot read that generation's objects without the export

Component: the generations of storage keys ([storage.md](../spec/storage.md) R-STOR-083, R-STOR-085). Subject: objects wrapped by a storage key
drawn after the point a database was restored to. Their records name an identifier the restored database lacks: reads fall over and mark
nothing, and `QuaderStorageUnknownKey` fires, until the export that holds the row is imported. Without such an export those objects are lost
where no other connection holds the blob. Dormant until a generation is drawn after the last backup and a restore goes back before it; a new
generation is rare (on command) and its command reminds the operator to export.

### H-1450 · The offline tool opens every key of the export on the operator's machine

Component: `quader admin storage offline` ([storage.md](../spec/storage.md) R-STOR-089,
[Q-STOR-452](../spec/storage.md#q-stor-452--the-offline-tool-opens-every-key-of-the-export-before-it-reads-and-looks-a-record-up-across-the-export)),
`LoadKeys` in [keys.go](../../internal/blob/offline/keys.go). Subject: every storage key and name key of the export, of every connection it
lists, also those of connections the run does not read. The tool opens all of them before it reads an object, so that a wrong ring is refused
at once and a backup's pool, whose records name the keys of the connections they came from, reads with one export; they live in the memory of
the operator's machine for the run, as a replica's do for its life ([H-1309](#h-1309--a-replicas-memory-holds-the-storage-keys-for-its-life-and-go-cannot-erase-a-key)),
and whoever reads that machine's memory meanwhile has them. Live whenever the tool runs; it needs the export and the ring, which together open
every blob anyway (R-STOR-085), so the machine must be one the operator would trust with the ring's escrow copy. `export --connection <id>`
narrows an export to one connection's keys.

### H-1451 · A get that is killed leaves the plaintext it had written in a temporary file

Component: `quader admin storage offline get` ([storage.md](../spec/storage.md) R-STOR-089,
[Q-STOR-451](../spec/storage.md#q-stor-451--get-writes-a-temporary-file-beside---out-and-links-it-only-after-the-digest-passed)), `Get` in
[read.go](../../internal/blob/offline/read.go). Subject: the plaintext of the blob being read. `get` writes into `.<out>.partial-<random>`
beside `--out` (mode 0600) and links it to `--out` only after the final segment and the SHA-256 passed; a refusal removes it. A process killed
before its end (a signal it cannot catch, a power loss) leaves the file with every segment that had authenticated so far but without the last
and without the digest's check. Live on every interrupted `get`; the file is the operator's own, in the directory he named, and carries the
`.partial-` mark; delete it by hand.

### H-1311 · A leaked data key of one blob has no answer short of a new connection

Component: the data key of one object ([storage.md](../spec/storage.md) R-STOR-076, R-STOR-083). Subject: that object's content. No command
writes one blob again with a new data key on the same connection; a leaked data key leaves that object readable to a holder of read access to
the bucket until the connection is replaced. Dormant while no Bastion registration has the handover on; then live for every blob of an encrypted
connection handed to a Bastion (decision 12 (b), [storage.md](../spec/storage.md) R-STOR-095, R-STOR-096, specified, not built), whose
collection by a taken-over Bastion is [bastion-handover.md](bastion-handover.md#h-1340--a-taken-over-bastion-collects-the-data-keys-of-the-encrypted-blobs-filled-through-it)
H-1340. Its planner decided on 2026-11-22 that no rewrite of one blob is built (Q-STOR-347): a rewrite would leave the noncurrent versions and
the backups' pools under the old key, and the trail `bastion.data_key.handed` names every key that left, so a later rewrite can take exactly
those blobs.

### H-1370 · An export made before the destruction opens what the provider keeps

Component: the export of the storage keys and the destruction ([storage.md](../spec/storage.md) R-STOR-085, R-STOR-098, R-STOR-103).
Subject: the objects a retired connection left at its provider. An export holds every key sealed as it lay; with the key ring that sealed it,
the offline tool reads the connection's bucket without a database (R-STOR-089), whatever the database says about the key afterwards. Quader
cannot see escrow or delete a file, so the destruction reports the exports it knows of (the table `storage_key_exports`, the last one's time)
and never refuses for them. Live from the destruction until the operator destroys every export made before it, once package 7 is built;
dormant until then, since no key can be destroyed.

### H-1371 · A Quader backup taken before the destruction holds the keys sealed until it leaves retention

Component: the backups' database snapshots ([backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-005, R-BAK-032;
[storage.md](../spec/storage.md) R-STOR-098). Subject: the keys of the destroyed connection. Every backup taken while the connection existed
holds `storage_keys` with its keys sealed; its table file and the key ring open them outside Quader, and a restore of it loads them again,
which the journal's replay undoes ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-054) unless the restore skips the journal or a lifecycle
rule removed the entry. The destruction reports these backups (`retained[].sealedKeyIds`) and does not refuse for them, since the newest
backup always holds the keys and is never deleted. Live from the destruction until the last such backup leaves retention, or the operator
deletes it, once package 7 is built.

### H-1372 · PostgreSQL keeps the sealed value after the destruction

Component: the `UPDATE` that destroys a key ([storage.md](../spec/storage.md) R-STOR-097, Q-STOR-412). Subject: the sealed values of the
destroyed keys. PostgreSQL keeps the old row version until vacuum and the reuse of its space, the write-ahead log until it is recycled, and a
point-in-time archive, a standby and a volume snapshot for their own retention; a `DELETE` would leave the same. A point-in-time restore to
before the destruction brings the keys back, and `admin post-restore`'s replay destroys them again. The command does not vacuum: migration
00001 grants the runtime role no `MAINTAIN`, and vacuum erases no bytes of the log or an archive. Live from the destruction until the
database's operator has vacuumed and the archive's retention has passed, once package 7 is built.

### H-1373 · The check of the backups sees only the newest listing of the configured target

Component: the backups' part of the destruction ([storage.md](../spec/storage.md) R-STOR-100, Q-STOR-403). Subject: backups whose pool holds
objects under the destroyed keys and that the check does not see: in another or an earlier target, of another lineage (an installation
restored from the same backup), or made by other means. Their pool objects become unreadable with the destruction, and a restore of such a
backup loses the blobs whose only pool object they are. The check trusts the newest retention listing of the configured target and refuses
when that run's target differs from the configured one. Dormant while an installation backs up into one target; live when an operator moved
the target, or keeps backups made by other means, and destroys a connection's keys, once package 7 is built.

### H-1374 · A data key handed to a Bastion outlives its storage key

Component: the handover of a data key ([storage.md](../spec/storage.md) R-STOR-095, R-STOR-096) and the destruction (R-STOR-098). Subject:
the objects of the destroyed connection whose data keys a Bastion received. A data key opens its object without the storage key; whoever kept
a handed key ([bastion-handover.md](bastion-handover.md#h-1340--a-taken-over-bastion-collects-the-data-keys-of-the-encrypted-blobs-filled-through-it)
H-1340) reads that object from the provider's leftovers after the destruction. The audit events `bastion.data_key.handed` name the blobs, and
the destruction counts them within the audit trail's retention. Dormant while no Bastion registration has the handover on; then live for
every object of a destroyed connection that was handed, once packages 7 and the handover are built.

### H-1375 · A forged journal entry can destroy a retired connection's keys at a restore before the operator does

Component: the replay of `system.storage.keys_destroyed` ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-054, Q-HA-416, Q-HA-417).
Subject: the keys of a connection that is retired and that the operator has not destroyed yet. Whoever holds a connection's runtime
credential can write journal objects ([H-553](../spec/backup-and-restore.md#h-553--a-journal-object-can-be-overwritten-with-the-runtime-credential)).
The replay skips an entry whose connection the restoring configuration names (Q-HA-417), so a configured connection's keys stay; but an
entry forged with the identifiers of a retired connection destroys its keys at the next restore, before the operator decided to, and with
them the recovery of its leftovers through an export and the offline tool (R-STOR-089). What makes it real: a retired encrypted connection
whose keys the operator keeps on purpose (to read the provider's leftovers once more), a holder of a runtime credential who writes such an
entry, and a restore of a backup or a point-in-time restore after it. Nothing Quader serves is lost, since a retired connection holds no
copy. Dormant until package 7 is built and a connection is encrypted.
