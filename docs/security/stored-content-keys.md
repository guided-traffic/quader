# The keys of the stored content: who holds which, and what losing or leaking one costs

Status: specified on 2026-11-22 ([storage.md](../spec/storage.md) R-STOR-083 to R-STOR-085, R-STOR-089, R-STOR-090;
[architecture.md](../spec/architecture.md) R-ARCH-021; [backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-031;
[bastion.md](../spec/bastion.md) R-BAS-061). The Bastion's cache key is built ([security/bastion.md](bastion.md#with-a-cache-key)). The
keys of the main installation are built (package 2 of [stored-content-encryption.md](../developer/stored-content-encryption.md#package-2-the-keys-as-built),
2026-11-22): the rows of `storage_keys` with their two sealed columns, the drawing with its refusal, the keys a replica holds, the export,
the import, a new generation and `blob.Store.CopyDataKey`. Since `encrypted: true` is still refused at start, no connection is encrypted, no
key is drawn on a running installation, and the key ring seals the upstream credentials and signing keys only, as
[data-and-secret-flow.md](data-and-secret-flow.md) describes; every "live with `encrypted: true`" below starts with package 3.

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
| A storage key | one sealed row of `storage_keys`; every backup of the database; the export | each replica that builds the blob area, at start or when a record names it; kept in memory for the replica's life | never itself; a further generation on command; its sealing at every rotation of the root |
| A name key | as a storage key | as a storage key | never |
| A data key | wrapped in its object's record, nowhere else | a writer for one write, a reader for one read; handed by `CopyDataKey` to a Bastion, that Bastion for one read ([bastion-handover.md](bastion-handover.md)) | never |
| An open session's data key | sealed in its `upload_sessions` row | the replica that serves a request of the session | resealed by the rotation like every sealed value |
| The Bastion's cache key | a Secret of the Bastion's chart (`secrets.cacheKey`), mounted as a file; the storage key and name key derived from it in each replica's memory for its life | the Bastion's pods, at start | by replacing the file, which empties the cache; the new replicas' sweeps delete the old tree ([H-1326](../spec/bastion.md#h-1326--after-a-key-change-the-old-tree-stays-readable-to-a-holder-of-the-old-key-until-the-sweep-has-deleted-it)) |

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
database lacks: reads fall over and mark nothing, `QuaderStorageUnknownKey` fires, until the export that holds the row is imported. Dormant
until package 3 accepts `encrypted: true`; then live only where a database was lost without its export, over a versioned bucket whose objects
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

### H-1308 · Storage keys are never deleted, so leaving a provider leaves readable ciphertext

Component: the rule that `storage_keys` rows are never deleted ([storage.md](../spec/storage.md) R-STOR-083, Q-STOR-309). Subject: the objects
a drained or abandoned connection leaves at its provider. The working paper said that whoever leaves a provider deletes its storage key and what
stays there becomes unreadable; Quader keeps every key, because noncurrent versions and backups' pool objects can hold objects it wrapped. What
the provider keeps after a drain (versions, a bucket nobody emptied) stays readable to whoever holds the database or a backup of it and the key
ring. Live whenever a connection is drained or forgotten while its bucket still holds objects. A command that destroys a retired connection's
keys once no retained backup names them is the owner's open question (Q-STOR-309 (b)); until then, emptying the bucket at the provider is the
operator's step.

### H-1309 · A replica's memory holds the storage keys for its life, and Go cannot erase a key

Component: the keys in memory ([storage.md](../spec/storage.md) R-STOR-083). Subject: the storage keys and name keys of the replica's
connections, and the data keys of its current transfers. A replica keeps the storage keys for its life and a writer or reader its data key for
one transfer; the writer overwrites its copy when it is done, but Go's AES implementation keeps an expanded key schedule in memory that no code
can erase, and the garbage collector may keep copies. A core dump, a memory read of the pod or its node yields them. Live with `encrypted: true`;
it is the same as for the key ring's own keys today.

### H-1310 · A database restored to a point before a generation was drawn cannot read that generation's objects without the export

Component: the generations of storage keys ([storage.md](../spec/storage.md) R-STOR-083, R-STOR-085). Subject: objects wrapped by a storage key
drawn after the point a database was restored to. Their records name an identifier the restored database lacks: reads fall over and mark
nothing, and `QuaderStorageUnknownKey` fires, until the export that holds the row is imported. Without such an export those objects are lost
where no other connection holds the blob. Dormant until a generation is drawn after the last backup and a restore goes back before it; a new
generation is rare (on command) and its command reminds the operator to export.

### H-1311 · A leaked data key of one blob has no answer short of a new connection

Component: the data key of one object ([storage.md](../spec/storage.md) R-STOR-076, R-STOR-083). Subject: that object's content. No command
writes one blob again with a new data key on the same connection; a leaked data key leaves that object readable to a holder of read access to
the bucket until the connection is replaced. Dormant while no Bastion registration has the handover on; then live for every blob of an encrypted
connection handed to a Bastion (decision 12 (b), [storage.md](../spec/storage.md) R-STOR-095, R-STOR-096, specified, not built), whose
collection by a taken-over Bastion is [bastion-handover.md](bastion-handover.md#h-1340--a-taken-over-bastion-collects-the-data-keys-of-the-encrypted-blobs-filled-through-it)
H-1340. Its planner decided on 2026-11-22 that no rewrite of one blob is built (Q-STOR-347): a rewrite would leave the noncurrent versions and
the backups' pools under the old key, and the trail `bastion.data_key.handed` names every key that left, so a later rewrite can take exactly
those blobs.
