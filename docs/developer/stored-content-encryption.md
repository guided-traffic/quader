# Encryption of stored content: the build plan

Status: specified on 2026-11-22; package 1, the format and its tools, built on 2026-11-22 ([below](#package-1-the-format-as-built));
package 2, the keys, built on 2026-11-22 ([below](#package-2-the-keys-as-built)); package 5, the Bastion's cache, built on 2026-11-22 on the
format ([below](#package-5-the-bastions-cache-as-built)); no object of the main installation is written or read in the format yet, and no
connection can be encrypted; packages 3, 4 and 6 not built; package 7, the destruction of a retired connection's keys, specified on
2026-11-22, not built ([below](#package-7-destroying-a-retired-connections-keys-specified)) ([storage.md](../spec/storage.md#encryption-of-stored-content) R-STOR-048, R-STOR-075 to
R-STOR-091, R-STOR-097 to R-STOR-103; [backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-031, R-BAK-032; [bastion.md](../spec/bastion.md) R-BAS-061;
[architecture.md](../spec/architecture.md) R-ARCH-021; [ha-and-helm.md](../spec/ha-and-helm.md) R-HA-072;
[ADR-244](../decisions.md#adr-244--encrypted-connections-quaders-own-segment-format-bound-to-the-content-the-wrapped-key-once-at-the-object-a-storage-key-per-connection-under-the-key-ring),
[ADR-245](../decisions.md#adr-245--the-bastions-cache-is-encrypted-in-the-stored-format-under-one-key-from-its-chart); security pages
[stored-content-format.md](../security/stored-content-format.md), [stored-content-keys.md](../security/stored-content-keys.md),
[stored-content-visibility.md](../security/stored-content-visibility.md)). Stage 2 of the owner's topic on the stored content. This page is
the build plan: the seven packages, what each owns, the order against stage 1's packages ([blob-store.md](blob-store.md#several-connections-the-build-plan)),
the tests and what they cost.

## What has to be in your head first

- **Stage 1 leaves the seams; stage 2 fills them.** The store's interface carries object metadata, the copy row has `object_key`,
  `stored_size` and `storage_key_id`, every size check compares `stored_size`, and each connection has one reader and one naming function
  ([storage.md](../spec/storage.md) R-STOR-062). On a plain connection the reader is the identity and the name is `blob.Key`; stage 2 gives an
  encrypted connection a reader that decrypts and a name that is keyed. Nothing above `internal/blob` learns of encryption: a format, the
  scanners and every caller of `blob.Store.Open` see plaintext.
- **The format is a leaf.** `internal/blob/blobcrypt` knows bytes, keys and records, standard library only; it knows no connection, no
  database and no store. Everything else calls it.
- **The record lies once, at the object.** No column holds a data key or a wrap; `blob_copies.storage_key_id` is bookkeeping (the counts of
  the API, which generation wraps how much), never what a read opens. A read opens what the object's metadata names.
- **Storage keys are rows, sealed by the key ring.** Register the sealed columns and the key ring's status, canary, compatibility check and
  re-encrypt task cover them; there is no second mechanism to build for the rotation.
- **An unknown key is not damage.** Every other failure of the record or a segment marks the copy `corrupt`; `unknown_key` marks nothing,
  because the export's import can still bring the key back. A destroyed key is damage: `destroyed_key` marks, because nothing brings that key
  back (package 7, R-STOR-101).
- **A key is destroyed only with its whole retired connection, and its row stays.** Nothing destroys a key by itself; the operator's command
  refuses while anything Quader still serves needs one, the pool of a retained backup included, which is why package 4 names an encrypted
  pool object by its key and copies it again from a connection that stays (R-STOR-097 to R-STOR-100, R-BAK-032).
- **No copy of ciphertext between connections.** Every copy into an encrypted connection is new bytes under a new data key; server-side copies
  stay a matter between plain connections.

## Work packages

At most four packages run at once, counting the packages of stage 1 that run then. Directories (or, where named, files) of packages that run
at the same time are disjoint.

| Package | After | What it builds | Owns |
|---|---|---|---|
| **1 · The format and its tools** | nothing; it can start before stage 1 lands | The codec of R-STOR-076 to R-STOR-078: segments, the final segment, `S(L)` and its inverse, the range planner, the nonce derivation of R-STOR-080, the record (encode, decode, wrap, unwrap, commitment), the keyed name of R-STOR-086, the tail segments of R-STOR-081, a reader that runs the checks of R-STOR-077 in their order with named errors; the vector files and the independent check of R-STOR-079 (run once, the result recorded below); the five fuzz targets, the nightly job of R-STOR-091 and its Makefile target | `internal/blob/blobcrypt/` (new), `tools/blobvectors/` (new), the job `fuzz-stored-format` in `.github/workflows/performance.yml`, a Makefile block `fuzz-stored-format` |
| **2 · The keys** (built, [below](#package-2-the-keys-as-built)) | stage 1's package A (the table `storage_connections`, the connection set, the marker and the start check) | The migration of R-STOR-083 (`storage_keys`, `storage_connections.keyed_names`, `upload_sessions.sealed_data_key` and `sealed_segments`, the stored columns of `upload_session_parts`); the two sealed columns registered; drawing keys at first sight with the refusal over a used location; holding them per replica, the change area `storage_keys` and the re-read for an unknown identifier; `quader admin storage keys export`, `import` and `new-generation` with their audit events (R-STOR-085); `CopyDataKey` and its architecture rule (R-STOR-083; the callers it allows are `internal/blob` and `internal/downloadlinks`, the handover's package, R-STOR-096 and [bastion-handover.md](bastion-handover.md)) | `internal/blob/keys/` (new), the stage's migration and `queries/storage_keys.sql` under `internal/meta/postgres/`, `cmd/quader/admin_storage_keys.go`, `internal/app/wire_storage_keys.go`, the change area in `internal/cluster/areas.go`, the rule in `internal/archtest` |
| **3 · Encrypted connections in the store, and the sessions** | 1, 2, and stage 1's packages A and B | `encrypted: true` accepted with `keyed_names` and the part-size rule (R-STOR-075); the start check's requirement of `metadata`; the naming function for keyed names; the connection's encrypting writer and decrypting reader: the commit's writes with the record and `HeadObject`'s check, the copy row's `stored_size` and `storage_key_id` (R-STOR-080); whole and ranged reads, the marks, `unknown_key`, the metric (R-STOR-082); the sessions with the sealed data key, the tails, the counter and both finalize paths (R-STOR-081); the conformance pass with an encrypted connection; the integration case of a rotation (R-STOR-084) | `internal/config/storage.go` (stage 2's rules), `internal/blob` (all files but `gc*.go`, `sweep.go` and the packages `blobcrypt`, `keys`, `reconcile`, `scrub`, `verify`), `internal/blob/s3`, `internal/blob/blobtest`, `internal/uploads`, `internal/app/wire_blob.go`, `test/integration/blob/rotation_test.go`, the encrypted pass in `test/integration/blob/conformance_test.go` |
| **4 · The jobs, the backup and the restore** | 3, and stage 1's packages B (orphan sweep), C (reconciliation), D (scrub, operator check) and E (backup, restore) | The reconciliation's four cases and the spool for streams into an encrypted connection, the scrub's marks and repair, the orphan sweep's reading of the record, the operator check's record (R-STOR-087); the pool with records, `verify --deep` with and without `--key-ring`, the restore's table and its drawing of keys (R-BAK-031); the pool's keys and the re-pool (R-BAK-032): pool names with the key's identifier, the blob list's pool column and its final write after the copies, pins with `pool` kept until the run's end, the manifest's `storageKeys` and its schema, `retained[].poolKeyIds` and `sealedKeyIds`, the re-pool, a draining connection's copies after every other (Q-BAK-76), and the sweep, `verify` and the restore by pool name; the integration cases of the jobs and of the backup | `internal/blob/reconcile/`, `internal/blob/scrub/`, `internal/blob/sweep.go`, `internal/blob/verify`, `internal/backup`, `internal/backup/format` (blob list, manifest, schema), `cmd/quader/backup_*.go`, a migration step for `backup_blob_pin.pool`, `test/integration/blob/encrypted_jobs_test.go`, `test/integration/backup/encrypted_test.go` |
| **5 · The Bastion's cache** | 1 | R-BAS-061: the encrypted layout below the key's tree, the sealed entries, the audit spool in the format, the sweep of other trees; `bastion.cache.encryption_key_file`; `secrets.cacheKey` in the Bastion's chart and its render tests | `internal/bastion/cache`, `internal/config/bastion_role.go`, `deploy/helm/quader-bastion`, the Bastion's chart tests under `test/chart` |
| **6 · Offline tool, chart, API and documentation** | 2 and 3, and stage 1's packages E (chart) and F (API, manual) | `quader admin storage offline` (R-STOR-089); the chart's acceptance of `encrypted`, the refusal of `keyed_names` alone and the rule of `QuaderStorageUnknownKey` (R-HA-072, R-HA-045, R-STOR-088); `keyedNames` and `storageKeys[]` in the connections' API (R-STOR-088, Q-API-405), with `state: destroyed` and `destroyedAt`; `destroyed_key` in `offline list` (R-STOR-089, R-STOR-103); the README's and the manual's sentences of R-STOR-090; this page and the developer pages of the store brought to what runs | `cmd/quader/admin_storage_offline.go`, `deploy/helm/quader` (stage 2's lines), `test/chart` (the main chart's cases), the connections' handler file in `internal/api/v1` and its paths in `api/openapi.yaml`, `README.md`, `docs/manual`, `docs/developer` |
| **7 · Destroying a retired connection's keys** (specified, [below](#package-7-destroying-a-retired-connections-keys-specified)) | 2 and 3; it may land before 4, because the backups' check refuses what it cannot read (R-STOR-100) | R-STOR-097 to R-STOR-103: the migration step (`sealed_key` nullable, `destroyed_at`, the state CHECKs, `storage_key_exports`), the walk skipping destroyed rows; the holders and the backups' check read in one transaction; `quader admin storage keys destroy` with its dry run, confirmation, exit codes, text and JSON, the change payload version 2 and the journaled event `system.storage.keys_destroyed`; the holders forgetting and loading over destroyed rows; `destroyed_key` in `blobcrypt` and its mapping in the store's reader, the scrub and the orphan sweep; the destroyed-id rule at start, in `Ensure` and in the restore's drawing; export and import of destroyed rows; the journal's replay handler (ha-and-helm.md R-HA-054, Q-HA-416); the audit registry and labels; the promise and the operator's steps in the README and the manual (R-STOR-090) | the migration step and `queries/storage_keys.sql` under `internal/meta/postgres/`; `internal/blob/keys` (new `destroy.go` and `replay.go`; `holder.go`, `export.go`, `draw.go`, `rows.go`, `keystest`); one reason in `internal/blob/blobcrypt/errors.go`; the mapping of `destroyed_key` in `internal/blob`'s reader (package 3's file, after 3 has landed); the destroyed-id rule in `internal/blob/record.go`; `cmd/quader/admin_storage_keys.go`; the handler's registration in `internal/app/wire_storage_keys.go`; `internal/audit/events_storage.go` and the audit labels with both translation files; `test/integration/blob/storage_keys_test.go`; `README.md`, `docs/manual`, the security pages, this page |

The order: 1 first (now); 2 as soon as stage 1's package A is on `main`; 5 beside 2, after 1; 3 when 1, 2 and stage 1's B have landed; 4 after
3 and stage 1's C, D and E; 6 after 3 with stage 1's E and F; 7 after 2 and 3, before or after 4 (until 4 lands, every installation
with a backup is refused by R-STOR-100's check, because no retained backup records `poolKeyIds` yet). Package 3 is the one that makes `encrypted: true` accepted; it changes the README
and the manual's pages of R-STOR-090 in the same change if package 6 has not yet landed, so that no release accepts the key without the
sentence about the lost key ring.

## Package 1: the format as built

[internal/blob/blobcrypt](../../internal/blob/blobcrypt/doc.go) is the format `quader-blob-v1`, a leaf of the standard library (AES-256-GCM,
HKDF-SHA-256, HMAC-SHA-256, SHA-256), which `TestThePackageImportsTheStandardLibraryOnly` holds. It knows no connection, database or store.

| File | Responsibility |
|---|---|
| [format.go](../../internal/blob/blobcrypt/format.go) | the constants of R-STOR-076, the derivations (`K_seg`, `C`, `K_tail`, `K_w`), the additional data of a segment, a tail segment and a wrap, the nonces of R-STOR-080 and [Q-STOR-321](../spec/storage.md#q-stor-321--a-staging-tails-nonces-take-its-session-offset-and-a-tail-holds-at-least-one-byte) |
| [keys.go](../../internal/blob/blobcrypt/keys.go) | `Key` (32 secret bytes the package owns; copies share them, `Destroy` clears them for all, a destroyed key refuses every use), `StorageKey`, `DataKey` (a key with the digest, size and storage key its record binds; `Commitment`), `NewSeed`, `ValidKeyID`; `String`, `GoString` and `LogValue` render no key |
| [size.go](../../internal/blob/blobcrypt/size.go) | `StoredSize` (`S(L)`), `PlainSize` (its inverse, refusing with `size`), `TailStoredSize`, `PlanRange` and `Range` (the segments and stored bytes of a window, R-STOR-082) |
| [seal.go](../../internal/blob/blobcrypt/seal.go) | the writer: `NewSection` (an object's data segments with or without the final segment, an `io.ReadSeeker` that seals as it is read, one segment in memory) and `NewTailSection` (a staging tail) |
| [record.go](../../internal/blob/blobcrypt/record.go) | `Record` and `Metadata`, `Wrap`, and `Open`: steps 1 to 6 of R-STOR-077 in their order against a `Want` (the copy row's digest, size and stored size, or none for a reader without a database, and the size the store answered) through a `KeyLookup` |
| [read.go](../../internal/blob/blobcrypt/read.go) | step 7: `NewReader` (whole), `NewRangeReader` (a window) and `NewTailReader`, one `Reader` that releases a segment's bytes only after its tag and holds the window's bytes of the last segment until the final segment (its length and digest) and, where it hashes, the SHA-256 have passed |
| [name.go](../../internal/blob/blobcrypt/name.go) | `KeyedName` and `KeyedPrefix` (R-STOR-086) |
| [errors.go](../../internal/blob/blobcrypt/errors.go) | `Reason` (the label `reason` of R-STOR-082), `Error`, the sentinels `ErrFormat` to `ErrDigest`, `ReasonOf`, `Reason.MarksCorrupt` (all but `unknown_key`) |

What packages 2 to 6 build on: package 2 opens a sealed row into `NewStorageKey(id, raw)` and draws keys with `NewKey(crypto/rand.Reader)`;
`CopyDataKey` returns the `DataKey` that `Open` yields (both built, [below](#package-2-the-keys-as-built)). Package 3 writes a part or a whole object with `NewSection` over a section of the
spool (the payload checksum is a first pass over the same `Section`, the SDK's retry a `Seek` to 0), makes the record with `Wrap` and
`Metadata`, checks a `HeadObject` with `Open`, reads with `Open` and `NewReader` or `PlanRange`, `Open` (the total as `ObjectSize`) and
`NewRangeReader`, counts a failure by `ReasonOf` and marks by `MarksCorrupt`; a session seals its data key's `Key.Bytes()` into its row,
writes tails with `NewTailSection` and reads them with `NewTailReader`, and names objects with `KeyedName`. Package 4's orphan sweep and
operator check run `Open` with `Want{Known: false}` and compare `KeyedName` of the unwrapped digest. Package 5 seals its cache with the same
writer and readers. Package 6's offline tool reads a record file (the object's metadata as JSON, the form of the vectors' `.record.json`).

A failure of the body is an `*Error` with its reason; a body that ends early is a transport error with `io.ErrUnexpectedEOF` in its chain and
no reason, which marks nothing; a body longer than the object is `size`. A failed key lookup passes through as it is.

## Package 2: the keys as built

[R-STOR-083](../spec/storage.md#r-stor-083--the-storage-keys-and-the-name-keys) to R-STOR-085, with the builder's decisions Q-STOR-324 to
Q-STOR-329. Nothing of it runs on an installation yet: `encrypted: true` is refused at start until package 3, so no replica draws or holds a
key, and the holder of every replica stays empty. An installation with plain connections only gets the migration `00107_storage_keys` (an
empty table, four columns and one row of `cluster_epoch`), an empty holder in its blob area, two more columns that the task
`secrets-reencrypt` walks (both empty) and three commands; its start and its downloads do nothing new.

| File | Responsibility |
|---|---|
| [00107_storage_keys.sql](../../internal/meta/postgres/migrations/00107_storage_keys.sql) | `storage_keys` with the CHECKs of Q-STOR-324 and the partial unique index of the active key; `storage_connections.keyed_names`; `upload_sessions.sealed_data_key` and `sealed_segments`; `upload_session_parts.stored_size` and `stored_sha256`; the row `storage_keys` of `cluster_epoch` |
| [storage_keys.sql](../../internal/meta/postgres/queries/storage_keys.sql), [tables_storage_keys.go](../../internal/meta/postgres/tables_storage_keys.go) | the statements of the package (the insert is `ON CONFLICT DO NOTHING`); the table is export class `data` |
| [keys/ids.go](../../internal/blob/keys/ids.go) | the kinds and states, `ParseID` (an identifier names kind, connection and generation), the drawing of an identifier |
| [keys/columns.go](../../internal/blob/keys/columns.go) | the two sealed columns registered with `internal/secrets` (`Column`, `SessionColumn`), `SealSessionKey` and `OpenSessionKey` for package 3, the seal and the opening of a row, `Refusal` |
| [keys/rows.go](../../internal/blob/keys/rows.go) | `Row`, the interface `Rows` and its PostgreSQL implementation `PG` |
| [keys/holder.go](../../internal/blob/keys/holder.go) | `Holder`: the keys of one replica by identifier, `Load`, `Active`, `NameKey`, `Lookup` with the re-read at most once per 10 s per connection, `ApplyChange` and `FlushArea` of the change area `storage_keys` (a `cluster.Listener`; the name is `keys.ChangeArea`, since nothing below `internal/blob` imports `internal/cluster`, R-STOR-046), `Close` |
| [keys/draw.go](../../internal/blob/keys/draw.go) | `Ensure`: the drawing at first sight, `LocationUsed` (one listing of `sha256/` and of `keyed/`) and the refusal |
| [keys/generation.go](../../internal/blob/keys/generation.go) | `NewGeneration` |
| [keys/export.go](../../internal/blob/keys/export.go) | `Document`, `Export`, `ReadDocument`, `Import` and `ImportError` |
| [keys/reseal.go](../../internal/blob/keys/reseal.go) | `Reseal`: the walks of `secrets-reencrypt` over both columns |
| [keys/keystest/db.go](../../internal/blob/keys/keystest/db.go) | the simulated tables of the unit tests (the identifier rule and the three unique constraints) |
| [blob/datakey.go](../../internal/blob/datakey.go) | `StorageKeys` (what the store asks of the holder), `WithStorageKeys`, `CopyDataKey` |
| [admin_storage_keys.go](../../cmd/quader/admin_storage_keys.go) | `quader admin storage keys export`, `import`, `new-generation` |
| [wire_storage_keys.go](../../internal/app/wire_storage_keys.go) | the holder of the blob area, `ensureStorageKeys`, the listener of the change area, the walk of `secrets-reencrypt` |
| [archtest/datakey.go](../../internal/archtest/datakey.go) | `CheckDataKeyCallers`, the rule of R-STOR-096 |
| [events_storage.go](../../internal/audit/events_storage.go), [audit-labels.ts](../../web/src/app/features/audit/audit-labels.ts) | the three audit events and their plain names in the audit log's filter (with both translation files and the fixture the Go test writes, R-UI-037) |

**At a start** (the blob area, [wire_blob.go](../../internal/app/wire_blob.go)): the holder is built over the database and the key ring before
the store, which takes it with `blob.WithStorageKeys`; the connections are recorded and checked as before; then `ensureStorageKeys` runs
`Holder.Ensure` for every encrypted connection in list order before the replica turns ready (Q-STOR-326). `Ensure` reads the connection's
rows; when it lacks the active storage key, or the name key with keyed names, and does not drain, it lists `sha256/` and `keyed/` (one key
each), reads the rows again in a transaction, refuses when the location holds an object and the rows are still missing, and otherwise draws
32 bytes per key from `crypto/rand`, seals each under its new identifier with the key ring (`storage_keys.sealed_key`, row `key_id`),
destroys the key and inserts the rows; then it loads the rows into memory. Two replicas that draw at the same moment insert one row of each
kind: the unique constraints let the slower insert insert nothing, and it reads the winner's rows in the same transaction; a replica commits
its rows before it wraps anything under them, so its objects never look to another replica like those of an earlier database. A refusal
(a used location, a key the ring cannot open) ends the start; a failure of the database or the store is retried. The infrastructure bundle
then adds the holder as a listener of the change log ([wire_keys.go](../../internal/app/wire_keys.go)), which exists only after the blob area.
Which encrypted connection gets keyed names is `keyedNames`, true with `encrypted: true` until the element carries `keyed_names` (package 3).

**While it runs:** a change of the area `storage_keys` (payload `{"connection": "<id>"}`) marks the connection, and the next `Active` reads its
rows again; a record that names an identifier the replica does not hold makes `Lookup` read them again, at once after a change and otherwise
at most once per 10 seconds, and returns nil (`unknown_key`) when they do not hold it. Rows are never deleted, so a held key is never replaced
or dropped; `Close` destroys every key when the replica stops. (Package 7 changes this: a change whose payload names destroyed identifiers,
and a load that finds a destroyed row, destroy and drop the held key, R-STOR-101.) `CopyDataKey` finds the present copy on the connection and the blob's size,
sends one `HeadObject` and runs `blobcrypt.Open` with the holder's `Lookup`; it marks nothing.

**The commands** (with the runtime database role, like `storage verify`): `export [--connection <id>]` reads the rows and writes
`system.storage.keys_exported` in one transaction, then prints the document (Q-STOR-329); it needs no key ring. `import <file>
[--other-installation]` reads the file (bounded, strict JSON, the identifier rule), opens every value with the configured ring, and in one
transaction that locks the kinds of each connection keeps equal rows, refuses another value under an existing identifier or another
identifier at an existing place, inserts the rest retired and makes the highest generation active (Q-STOR-325), announces the area per
connection and writes `system.storage.keys_imported`. `new-generation --connection <id>` locks the connection's storage keys, opens the active
one with the ring (Q-STOR-328), draws generation `n + 1`, retires `n`, inserts `n + 1`, announces the area and writes
`system.storage.key_generation_added`. Both writing commands print the reminder to export. Every refusal exits `2` with a sentence that
names identifiers and root keys; a database that does not answer exits `75`.

**What package 3 builds on.** The writer asks `Holder.Active(ctx, connection)` for the storage key to `blobcrypt.Wrap` under and
`Holder.NameKey(connection)` for `blobcrypt.KeyedName`; the reader passes `Holder.Lookup(ctx, connection)` to `blobcrypt.Open` (extend
`blob.StorageKeys`, which holds `Lookup` only, with the two). A session seals its data key into `upload_sessions.sealed_data_key` with
`keys.SealSessionKey(ring, sessionID, key)` and opens it with `keys.OpenSessionKey`; the walk of a rotation already covers the column. The
element's `keyed_names` replaces `keyedNames` in [wire_storage_keys.go](../../internal/app/wire_storage_keys.go), and
`storage_connections.keyed_names` waits for `RecordConnections` to write and compare it as a fixed property. A key is never to be logged or
put in an error: the holder's keys render their identifier only.

**Tests and their cost**, measured on 2026-11-22 in the unit job's mode (`go test -race -covermode=atomic -coverpkg=./...`) on the development
machine, and the integration case under the stack's lock:

| Test | Tier | Data | Time |
|---|---|---|---|
| `TestR_STOR_083_TheFirstReplicaDrawsTheKeysAndOthersReadThem`, `…AUsedLocationWithoutKeysRefusesTheStart`, `…ANewGenerationReachesEveryReplica`, `…NoKeyLeavesTheHolder`, `…AKeyTheRingCannotOpenStopsTheStart`, `…TheSealedColumnsAreRegistered`, `…TheHolderFollowsTheRegisteredChangeArea`, `TestR_STOR_084_TheWalkResealsBothColumnsAndChangesNoKey`, `TestKeyIdentifiers` | unit, `internal/blob/keys` (keystest, blobtest) | keys of 32 bytes, objects of a few bytes | 0.04 s |
| `TestR_STOR_083_CopyDataKeyOpensTheRecordOfThePresentCopy` | unit, `internal/blob` (the fake database, two in-memory stores) | objects of up to 1,200 bytes | 0.02 s |
| `TestR_STOR_085_TheExportAndTheImportRoundTrip`, `TestR_STOR_085_AnImportThatDoesNotFitExitsTwoAndWritesNothing`, `TestR_STOR_083_NewGenerationIsAuditedAndAnnounced` | unit, `cmd/quader` (keystest) | three keys, files of about 2 KB | 0.03 s |
| `TestR_STOR_096_CopyDataKeyHasTwoCallers` | unit, `internal/archtest` (a fixture and the tree's syntax) | ten small files | 0.1 s besides the `go list` of the tree the package's other tests run anyway |
| `TestR_STOR_083_TheStorageKeysInPostgreSQL` (the migration's rules, two replicas drawing at the same moment, the re-encryption of both columns at a rotation of the root, a new generation through the change area) | integration, `test/integration/blob` | a database of its own, eight rows | 0.8 s, most of it the migration of the database |

The unit job gains about 0.2 s and one package's start (about 2 s of process time for `internal/blob/keys`, almost all of it the start of a
binary built with the race detector and the module's coverage); the integration shard that runs `blob` about 1 s; `TestSecretsReencryptTask`
of the area `repository` counts five columns instead of three and costs nothing more. No chart test, end-to-end leg or nightly job is added;
with 25 formats the cost is the same, since no format sees a key.

**Every defence taken out once**, one at a time, and the test that failed: the unique constraints of the table (the integration case's
drawing at the same moment and its migration subtest); the simulation's unique constraints (`…a replica that loses the race to insert reads
the winner's rows`); `ON CONFLICT DO NOTHING` (the integration case's drawing at the same moment); the refusal over a used location
(`TestR_STOR_083_AUsedLocationWithoutKeysRefusesTheStart` and `…NoKeyLeavesTheHolder`); the second read of the rows after the listing (`…an
object another replica wrote under the keys it drew meanwhile is no reason to refuse`); the import's opening of every value before a write
(four cases of `TestR_STOR_085_AnImportThatDoesNotFitExitsTwoAndWritesNothing`: a ring without the root key, a ring whose key of that name is
another, a changed value, a moved value); the import's check of another installation, of an existing key's value and of the identifier rule
(one case each); the table's identifier rule (the integration case's migration subtest); the ring check of a new generation
(`…NoKeyLeavesTheHolder`); the rule of `CopyDataKey` and its refusal of a subpackage (`TestR_STOR_096_CopyDataKeyHasTwoCallers`); the re-read
bound (`…a replica that missed the change reads the rows again …`); the change area's mark (the unit test and the integration case's change
area subtest); `Close` destroying the keys (`…Close destroys every key`); the walk over `upload_sessions.sealed_data_key` (the unit and the
integration walk).

## Package 5: the Bastion's cache as built

[R-BAS-061](../spec/bastion.md#r-bas-061--the-cache-encrypted-in-the-stored-format) in `internal/bastion/cache`
([crypt.go](../../internal/bastion/cache/crypt.go), [sealed.go](../../internal/bastion/cache/sealed.go),
[foreign.go](../../internal/bastion/cache/foreign.go), the `sealedLayout` of [layout.go](../../internal/bastion/cache/layout.go)), with
`bastion.cache.encryption_key_file` in [bastion_role.go](../../internal/config/bastion_role.go) and `secrets.cacheKey` in the chart
`quader-bastion`. It uses `blobcrypt` as the paragraph above foresees: `NewStorageKey` for the storage key derived from the file, `NewKey`
and `NewSeed` per object, `NewSection` and `Wrap` to write, `Open`, `NewReader`, `PlanRange` and `NewRangeReader` to read, `ReasonOf` to
tell a refusal from a transport error. It needed one allowance in the architecture test: the cache may import `internal/blob/blobcrypt`
(scoped like `internal/blob/s3`, [bastion.go](../../internal/archtest/bastion.go)). Four points the specification left open or could not
have as written were decided by the builder (Q-BAS-212 to Q-BAS-215): the record lies in front of each object's body, because neither of the
cache's backends keeps object metadata; entries and the spool's files are objects of the format bound to their names, in place of a seal under
an entries' key; with a key the volume backend spools its fills on the pod's temporary volume; and when the sweep looks for other layouts.
The layout family by family, the reads and the errors are on [bastion.md](bastion.md#the-cache-backends-package-5); the tests and their
measured cost are in that page's [Tests](bastion.md#tests): about 0.2 s of unit tests (the package's run in the unit job's mode grows by
0.3 s), 0.2 s in the integration area `bastioncache`, eight chart renders, nothing in any end-to-end leg; with 25 formats the cost is the
same, since no format sees the cache's key.

## Package 7: destroying a retired connection's keys (specified)

[R-STOR-097](../spec/storage.md#r-stor-097--a-destroyed-key) to R-STOR-103, with the decisions Q-STOR-400 to Q-STOR-413, and in the backup
[R-BAK-032](../spec/backup-and-restore.md#r-bak-032--the-pools-keys-their-record-and-the-re-pool) with Q-BAK-74 to Q-BAK-76 (package 4's
part), the journal's replay of R-HA-054 with Q-HA-416 and Q-HA-417 (the event joins R-HA-054's table, and with it the test that holds the table to the event registry, when package 7 registers it), and the gaps H-1308 and H-1370 to H-1375 of
[stored-content-keys.md](../security/stored-content-keys.md#destroying-a-retired-connections-keys)
([ADR-255](../decisions.md#adr-255--the-storage-keys-of-a-retired-connection-can-be-destroyed-so-that-what-stays-at-a-provider-becomes-unreadable)).
What the builder reads first, beyond the requirements:

- **The order of the destroying transaction** (R-STOR-099): lock the key rows of both kinds and the `storage_connections` row, run every
  holder of R-STOR-098 and the backups' check of R-STOR-100, compare the confirmation, then the `UPDATE`, the change, the audit event with its
  outbox row, the commit. A dry run runs the same reads and writes nothing.
- **What was read for it on 2026-11-22**, on `main` at the rest of package E:
  - `retired_at` is set by the reconciliation for a drained connection that no live replica configures (`ReconcileRetire` in
    [reconcile.sql](../../internal/meta/postgres/queries/reconcile.sql), called from [drain.go](../../internal/blob/reconcile/drain.go)),
    which deletes the connection's queue rows with it, and by `forget-connection` (`ForgetConnection` in
    [record.go](../../internal/blob/record.go)).
  - `StorageConnectionConfiguredLive` ([blob_copies.sql](../../internal/meta/postgres/queries/blob_copies.sql)) reads the replica registry
    only, so it answers for a connection without a `storage_connections` row as for one with.
  - `quader backup prune` and `delete` record a run of operation `delete` with `result.retention`
    ([retention/runner.go](../../internal/backup/retention/runner.go)), which R-STOR-100's check reads.
  - `CountSealed` counts non-NULL values only ([sealed.go](../../internal/meta/postgres/listq/sealed.go)); `StorageKeysSealedAfter` selects
    every row today and gains `AND sealed_key IS NOT NULL`.
  - `internal/blob` (with `internal/blob/keys`) is in the infrastructure layer and `internal/journal` in the domain layer, and a lower layer
    never imports a higher one ([layers.go](../../internal/archtest/layers.go)); so the replay handler of `system.storage.keys_destroyed` is a
    function of `internal/blob/keys` that takes a `meta.Tx`, registered with `journal.Register` from
    [wire_storage_keys.go](../../internal/app/wire_storage_keys.go).
  - The checks of the migration files are switched off until the first stable release ([database.md](database.md), the job `compat`), so a
    new step and an edit of `00107` in place would both pass; the specification asks for a new step.
  - The backup's copy order as built puts a draining connection's copy last within each group (`order` in
    [copy.go](../../internal/backup/create/copy.go)); Q-BAK-76 moves it behind every other copy (package 4).
- **The replay handler takes the restoring configuration** (Q-HA-417): an entry whose connection `storage.connections` names is reported
  `skipped` with its reason as the result's note (printed as a warning line, listed among the summary's warnings) and changes nothing;
  the restore goes on. Its unit test has four cases: a sealed row destroyed, a destroyed row already in effect, a missing row inserted
  destroyed, and an entry for a configured connection skipped with its keys still sealed.
- **Until package 4 lands**, every installation that records a backup run is refused by R-STOR-100's check (no retained backup records
  `poolKeyIds`), which fails closed; an installation with `backup.enabled: false` and no run passes.

## The vectors and their independent check

The known answers of [storage.md](../spec/storage.md) R-STOR-076, R-STOR-077 and R-STOR-086 were computed for the specification on 2026-11-22
by a throwaway implementation written from the specification's text in Python 3.12 with the `cryptography` package 38.0.4, independent of any
code of this repository. Package 1 writes the vector files from its Go implementation, checks that they contain those answers unchanged,
checks every case once with `tools/blobvectors/check.py` (Python's `cryptography`, by hand, not in CI), and records here the date and the
outcome.

**Checked on 2026-11-22, every case agrees.** In this order:

1. The Go implementation, written from the specification, reproduced every known answer of R-STOR-076, R-STOR-077 and R-STOR-086 on its first
   run (`TestR_STOR_076_TheKnownAnswersOfTheStoredFormatReproduce`, `TestR_STOR_077_TheKnownAnswersOfTheRecordReproduce`,
   `TestKeyedNameKnownAnswer`), before anything else was compared.
2. [tools/blobvectors](../../tools/blobvectors/main.go) wrote [testdata/vectors.json](../../internal/blob/blobcrypt/testdata/vectors.json) and
   the files below `testdata/objects/` (35 cases, 651,536 bytes): the sizes of R-STOR-079 by the rule, the known answers (the object `a` among
   them, beside the rule's `l1`), an older genuine object (the last row of R-STOR-078), the staging tail at 131,072, the keyed name, and 25
   negative cases, one or more per row of R-STOR-078 (4 `size`, 4 `segment`, 4 `final`, 5 `record`, 2 `format`, 3 `unknown_key`,
   2 `commitment`, 1 `digest`). `TestTheCommittedVectorsAreWhatTheWriterWrites` holds the files to what the command writes.
3. [check.py](../../tools/blobvectors/check.py) (Python 3.12.13, `cryptography` 38.0.4) rebuilt every positive case from its inputs byte for
   byte, derived every listed nonce from its seed, read every case through steps 1 to 7 and refused every negative case with its reason, and
   compared the specification's known answers and its size table: 35 of 35 agree.
4. Only then was the planner's reference of the specification's examples read and run against the vector files (its functions unchanged,
   through a harness that loads the files and applies the step order of R-STOR-077): 34 of 34 cases agree. It does not cover the tail case,
   since it implements no tail; the tail and its nonce rule (Q-STOR-321) rest on `check.py` and the Go tests alone.

`python3 -I tools/blobvectors/check.py internal/blob/blobcrypt/testdata` repeats step 3; it exits 0 when every case agrees.

## Tests and their cost

R-STOR-091's table: about 2.9 s of Go unit tests, 0.5 s of chart tests, 10 s in the integration area `blob` (a rotation and the jobs), 8 s in
`backup` (one case), 3 s in `s3-conformance`, nothing in any end-to-end leg, and one nightly job of about 27 minutes for the five fuzz targets.
With 25 formats the cost is the same: no format sees a connection or a key.

The destruction of keys (package 7) and the pool's keys (package 4), estimated:

| Test | Tier | Data | Run time (estimate) |
|---|---|---|---|
| The destruction, the refusals, a second run, forgetting at the change, a load over destroyed rows, `Ensure`'s refusal, export and import of destroyed rows, `activateHighest`, the walk skipping NULL, the backups' check (R-STOR-100's two cases) | unit, `internal/blob/keys` (keystest) | rows only | 0.08 s |
| The command: dry run, confirmation, exits `0`, `2` and `5`, JSON, audit event, outbox row, change | unit, `cmd/quader` | three keys | 0.05 s |
| A read that meets `destroyed_key` | unit, `internal/blob` (in-memory store) | objects of 1 KiB | 0.01 s |
| The journal's replay, its four cases (Q-HA-417's skip of a configured connection among them) | unit, `internal/blob/keys` | rows | 0.02 s |
| The blob list's pool column, the re-pool's merge, the final list from the pins, the manifest's `storageKeys` (package 4) | unit, `internal/backup/format` and `create` | lists of tens of lines | 0.1 s |
| `TestR_STOR_097_…`: the CHECKs, a destruction in PostgreSQL with its event, outbox row and change, refused by a copy row and by a `backup_run` listing | integration, `test/integration/blob`, in the database of the keys' case | eight rows | 0.5 s |
| The re-pool and the prune (package 4, R-BAK-031's case extended) | integration, `test/integration/backup` | three blobs of 1 KiB | +3 s |

The unit job gains about 0.3 s and no package start; the integration shard that runs `blob` about 0.5 s and the one that runs `backup`
about 3 s; the chart tests (the silent case of `QuaderStorageUnknownKey` on `destroyed_key` is one rule-test row), every end-to-end leg,
`s3-conformance` and the nightly nothing. With 25 formats the cost is the same, since no format sees a key.

| Fuzz target | Package | What it holds |
|---|---|---|
| `FuzzPlainSize` | `internal/blob/blobcrypt` | the inverse of `S` never panics and agrees with `S` |
| `FuzzOpenRecord` | `internal/blob/blobcrypt` | any metadata map yields a data key or one of the named errors, and a key only from a genuine record |
| `FuzzReadObject` | `internal/blob/blobcrypt` | any body under a valid record yields the plaintext or a named error, and no byte of a segment that failed |
| `FuzzReadRange` | `internal/blob/blobcrypt` | any range over a vector object yields exactly its window or an error, and no byte that was not authenticated |
| `FuzzOpenTail` | `internal/blob/blobcrypt` | a staging tail object yields its plaintext or a named error |

Locally: `make fuzz-stored-format STORED_FORMAT_FUZZTIME=1m`; a failing input stays in `internal/blob/blobcrypt/testdata/fuzz`. Each target ran
30 s on 2026-11-22 on the development machine without a finding (FuzzPlainSize 3.8 million inputs, FuzzOpenRecord 2.2 million,
FuzzReadObject 2.4 million, FuzzReadRange 1.4 million, FuzzOpenTail 69,000: its inputs are a tail of 64 KiB each).

**The nightly job** `fuzz-stored-format` of [performance.yml](../../.github/workflows/performance.yml) ("fuzz the stored format's readers")
runs `make fuzz-stored-format STORED_FORMAT_FUZZTIME=5m` on a self-hosted runner beside the lanes, with select-leg in front like every job of
the workflow (`-f sets=fuzz-stored-format` dispatches it alone), and uploads `testdata/fuzz` as the artifact `fuzz-stored-format` when it
fails. Its cost: 25 minutes of fuzzing and about 2 minutes of checkout, Go and modules, about 27 runner minutes a night (estimated; it has
not run on GitHub yet). The closing run does not start it
([Q-STOR-323](../spec/storage.md#q-stor-323--the-closing-run-does-not-start-the-fuzz-job-of-the-stored-format)).

**Package 1's unit tests, measured** on 2026-11-22 in the unit job's mode (`go test -race -covermode=atomic -coverpkg=./...`, the development
machine), all at the unit tier and in parallel:

| Tests | Data | Time |
|---|---|---|
| `TestR_STOR_078_EveryAttackOfTheTableIsRefusedWithItsReason` (every row, whole and ranged, with the counting reader), `TestR_STOR_078_NoByteLeavesBeforeItsSegmentAuthenticated`, `TestABodyOfAnotherLengthThanItsSizeIsCaught`, `TestR_STOR_077_AnObjectWithoutRecordIsRefusedAsFormat` | objects of 40 bytes to 196,609 bytes, made in memory | 0.10 s |
| `TestR_STOR_079_*` (every case of the vector file, the known answers in it, below 1 MB), `TestR_STOR_076_*` and `TestR_STOR_077_*` known answers, `TestKeyedNameKnownAnswer` | the vector files, 651,536 bytes | 0.06 s |
| `TestR_STOR_076_StoredSizeAndItsInverseAgree` (the table, windows of ±100 around the first four boundaries and the last three before 4 TiB, sampled boundaries up to the limit), `TestRangePlansTheSegmentsOfTheWindow` | none: arithmetic | 0.01 s |
| `TestR_STOR_077_ARecordRoundTripsAndStaysSmall`, `…TheStepsRunInTheirOrder`, `TestKeysRenderNothingOfThemselves`, `TestADestroyedKeyRefusesEveryUse`, `TestKeyedNamesDifferPerNameKey`, `TestSectionsAreDeterministicAndCompose`, `TestSectionsRefuseRunsThatBreakTheLayout`, `TestTailSegments`, `TestThePackageImportsTheStandardLibraryOnly` | up to 196,685 bytes | 0.04 s |
| the seed corpora of the five fuzz targets | the vectors | 0.08 s |
| `TestTheCommittedVectorsAreWhatTheWriterWrites` (`tools/blobvectors`) | writes the 651,536 bytes to a temporary directory | 0.06 s |

That is 0.35 s of tests, against the 0.4 s R-STOR-091 estimates; each of the two packages costs about 1.2 s of process time in the unit job,
almost all of it the start of a binary built with the race detector and the module's coverage, which every package pays. No other job gains
anything: no integration, end-to-end or chart test is added.

Every defence of the format was taken out once, one at a time, and at least one of these tests failed each time: the size check of step 5,
the index and the mark in a segment's additional data, steps 1, 4 and 6, the final segment's length and digest, the plaintext's SHA-256,
the hold-back of the last segment, the identifier in the wrap's additional data, `unknown_key` told apart from `record`, the offset and the
last mark of a tail segment, the segment key's own label, and the refusal of a body longer than the object.

## Not verified

- Nothing of this stage was run against an object store. Whether the owner's in-cluster object gateway (Ceph 19.2.3, read from his cluster on
  2026-11-22) keeps user metadata on a write and replaces it on a copy is what the start check establishes there once the release runs; its
  documentation was not read. The provider he has in mind documents a copy with new metadata only through the documentation of the software
  behind it (research of 2026-11-22), not for itself.
- The throughput of encryption on Quader's hardware; the proxy's measurement (about 4.5 GB/s per core) is from an Apple M5 Pro. Package 1 did
  not measure `blobcrypt`'s throughput either.
- The nightly job `fuzz-stored-format` has not run on GitHub yet: its 27 minutes are the sum of its fuzz times and an estimate of its setup,
  and whether the self-hosted runner's eight workers reach the local numbers above is not known.
- That `cipher.AEAD` from `crypto/cipher` may be used by several goroutines at once was not relied on: every `Section` and `Reader` builds its
  own cipher from the key.
- Package 7 and package 4's pool keys (read, not run): whether the runtime role can run `VACUUM` on `storage_keys` (migration 00001 grants it
  no `MAINTAIN`, but whether an installation runs migrations and the replicas with the same login, which would own the table, was not read)
  and how much of a value PostgreSQL leaves readable on disk after vacuum; what writing the final blob list after the copies costs on a first
  backup of millions of blobs (the ordered scan of the pins), a measurement to take, not a test. Nothing of either was run, and nothing was
  tried against an object store.
