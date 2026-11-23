# Encryption of stored content: the build plan

Status: specified on 2026-11-22; package 1, the format and its tools, built on 2026-11-22 ([below](#package-1-the-format-as-built));
package 2, the keys, built on 2026-11-22 ([below](#package-2-the-keys-as-built)); package 3, the encrypted connections in the store and the
sessions, built on 2026-11-22 ([below](#package-3-encrypted-connections-as-built)): `encrypted: true` is accepted by the configuration
and, since package 6, by the chart; package 4, the jobs, the backup and the restore, built on 2026-11-23
([below](#package-4-as-built)); package 5, the Bastion's cache, built on 2026-11-22 on the format
([below](#package-5-the-bastions-cache-as-built)); package 6, the offline tool, the chart's acceptance, the rest of the API and the
documentation, built on 2026-11-23 ([below](#package-6-the-offline-tool-the-chart-the-api-and-the-documentation-as-built));
package 7, the destruction of a retired connection's keys,
specified on 2026-11-22, not built ([below](#package-7-destroying-a-retired-connections-keys-specified)) ([storage.md](../spec/storage.md#encryption-of-stored-content) R-STOR-048, R-STOR-075 to
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
| **3 · Encrypted connections in the store, and the sessions** (built, [below](#package-3-encrypted-connections-as-built)) | 1, 2, and stage 1's packages A and B | `encrypted: true` accepted with `keyed_names` and the part-size rule (R-STOR-075); the start check's requirement of `metadata`; the naming function for keyed names; the connection's encrypting writer and decrypting reader: the commit's writes with the record and `HeadObject`'s check, the copy row's `stored_size` and `storage_key_id` (R-STOR-080); whole and ranged reads, the marks, `unknown_key`, the metric (R-STOR-082); the sessions with the sealed data key, the tails, the counter and both finalize paths (R-STOR-081); the conformance pass with an encrypted connection; the integration case of a rotation (R-STOR-084) | `internal/config/storage.go` (stage 2's rules), `internal/blob` (all files but `gc*.go`, `sweep.go` and the packages `blobcrypt`, `keys`, `reconcile`, `scrub`, `verify`), `internal/blob/s3`, `internal/blob/blobtest`, `internal/uploads`, `internal/app/wire_blob.go`, `test/integration/blob/rotation_test.go`, the encrypted pass in `test/integration/blob/conformance_test.go` |
| **4 · The jobs, the backup and the restore** (built, [below](#package-4-as-built)) | 3, and stage 1's packages B (orphan sweep), C (reconciliation), D (scrub, operator check) and E (backup, restore) | The reconciliation's four cases and the spool for streams into an encrypted connection, the scrub's marks and repair, the orphan sweep's reading of the record, the operator check's record (R-STOR-087); the pool with records, `verify --deep` with and without `--key-ring`, the restore's table and its drawing of keys (R-BAK-031); the pool's keys and the re-pool (R-BAK-032): pool names with the key's identifier, the blob list's pool column and its final write after the copies, pins with `pool` kept until the run's end, the manifest's `storageKeys` and its schema, `retained[].poolKeyIds` and `sealedKeyIds`, the re-pool, a draining connection's copies after every other (Q-BAK-76), and the sweep, `verify` and the restore by pool name; the integration cases of the jobs and of the backup | `internal/blob/reconcile/`, `internal/blob/scrub/`, `internal/blob/sweep.go`, `internal/blob/verify`, `internal/backup`, `internal/backup/format` (blob list, manifest, schema), `cmd/quader/backup_*.go`, a migration step for `backup_blob_pin.pool`, `test/integration/blob/encrypted_jobs_test.go`, `test/integration/backup/encrypted_test.go` |
| **5 · The Bastion's cache** | 1 | R-BAS-061: the encrypted layout below the key's tree, the sealed entries, the audit spool in the format, the sweep of other trees; `bastion.cache.encryption_key_file`; `secrets.cacheKey` in the Bastion's chart and its render tests | `internal/bastion/cache`, `internal/config/bastion_role.go`, `deploy/helm/quader-bastion`, the Bastion's chart tests under `test/chart` |
| **6 · Offline tool, chart, API and documentation** (built, [below](#package-6-the-offline-tool-the-chart-the-api-and-the-documentation-as-built)) | 2 and 3, and stage 1's packages E (chart) and F (API, manual) | `quader admin storage offline` (R-STOR-089); the chart's acceptance of `encrypted`, the refusal of `keyed_names` alone and the rule of `QuaderStorageUnknownKey` (R-HA-072, R-HA-045, R-STOR-088); `keyedNames` and `storageKeys[]` in the connections' API (R-STOR-088, Q-API-405; built ahead with stage 1's package F, which leaves this package the acceptance case of an encrypted connection and `state: destroyed` with `destroyedAt`); `destroyed_key` in `offline list` (R-STOR-089, R-STOR-103); the README's and the manual's sentences of R-STOR-090; this page and the developer pages of the store brought to what runs | `cmd/quader/admin_storage_offline.go`, `deploy/helm/quader` (stage 2's lines), `test/chart` (the main chart's cases), the connections' handler file in `internal/api/v1` and its paths in `api/openapi.yaml`, `README.md`, `docs/manual`, `docs/developer` |
| **7 · Destroying a retired connection's keys** (specified, [below](#package-7-destroying-a-retired-connections-keys-specified)) | 2 and 3; it may land before 4, because the backups' check refuses what it cannot read (R-STOR-100) | R-STOR-097 to R-STOR-103: the migration step (`sealed_key` nullable, `destroyed_at`, the state CHECKs, `storage_key_exports`), the walk skipping destroyed rows; the holders and the backups' check read in one transaction; `quader admin storage keys destroy` with its dry run, confirmation, exit codes, text and JSON, the change payload version 2 and the journaled event `system.storage.keys_destroyed`; the holders forgetting and loading over destroyed rows; `destroyed_key` in `blobcrypt` and its mapping in the store's reader, the scrub and the orphan sweep; the destroyed-id rule at start, in `Ensure` and in the restore's drawing; export and import of destroyed rows; the journal's replay handler (ha-and-helm.md R-HA-054, Q-HA-416); the audit registry and labels; the promise and the operator's steps in the README and the manual (R-STOR-090) | the migration step and `queries/storage_keys.sql` under `internal/meta/postgres/`; `internal/blob/keys` (new `destroy.go` and `replay.go`; `holder.go`, `export.go`, `draw.go`, `rows.go`, `keystest`); the column `destroyed_at` in the statement `StorageConnectionKeys` of `queries/storage_summary.sql` and its mapping in `internal/blob/summary` (the API's field is package 6's); one reason in `internal/blob/blobcrypt/errors.go`; the mapping of `destroyed_key` in `internal/blob`'s reader (package 3's file, after 3 has landed); the destroyed-id rule in `internal/blob/record.go`; `cmd/quader/admin_storage_keys.go`; the handler's registration in `internal/app/wire_storage_keys.go`; `internal/audit/events_storage.go` and the audit labels with both translation files; `test/integration/blob/storage_keys_test.go`; `README.md`, `docs/manual`, the security pages, this page |

The order: 1 first (now); 2 as soon as stage 1's package A is on `main`; 5 beside 2, after 1; 3 when 1, 2 and stage 1's B have landed; 4 after
3 and stage 1's C, D and E; 6 after 3 with stage 1's E and F; 7 after 2 and 3 (4 has landed, so every backup run records the
`poolKeyIds` that R-STOR-100's check reads). Package 3 is the one that makes `encrypted: true` accepted; it changes the README
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
writes tails with `NewTailSection` and reads them with `NewTailReader`, and names objects with `KeyedName`. Package 4's orphan sweep runs
`OpenRecord` (the steps of `Open` that need no copy row) and compares `KeyedName` of the unwrapped digest; its operator check runs `Open`
against the copy row and the blob row. Package 5 seals its cache with the same
writer and readers. Package 6's offline tool reads a record file (the object's metadata as JSON, the form of the vectors' `.record.json`).

A failure of the body is an `*Error` with its reason; a body that ends early is a transport error with `io.ErrUnexpectedEOF` in its chain and
no reason, which marks nothing; a body longer than the object is `size`. A failed key lookup passes through as it is.

## Package 2: the keys as built

[R-STOR-083](../spec/storage.md#r-stor-083--the-storage-keys-and-the-name-keys) to R-STOR-085, with the builder's decisions Q-STOR-324 to
Q-STOR-329. It runs on an installation since package 3 accepts `encrypted: true`: a replica draws and holds the keys of each encrypted
connection; with plain connections only the holder of every replica stays empty. An installation with plain connections only gets the migration `00107_storage_keys` (an
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
Which encrypted connection gets keyed names is the element's `keyed_names` (`Connection.KeyedNames`, package 3).

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

## Package 3: encrypted connections as built

[R-STOR-075](../spec/storage.md#r-stor-075--an-encrypted-connection), R-STOR-080 to R-STOR-082, R-STOR-084's case with objects and R-STOR-086's
naming, with the builder's decisions Q-STOR-380 to Q-STOR-385. `encrypted: true` and `keyed_names` are accepted by the configuration; the
Helm chart refused `encrypted: true` until package 6. Nothing above `internal/blob` and `internal/uploads` learned of the encryption: a
format, the scanners and every caller of `blob.Store.Open` see plaintext.

| File | Responsibility |
|---|---|
| [storage.go](../../internal/config/storage.go), [storage_connections.go](../../internal/config/storage_connections.go) | `keyed_names` (a pointer, `true` by default on an encrypted element, `UsesKeyedNames`), the refusal of `keyed_names` on a plain element, the part sizes that hold whole segments while an element is encrypted |
| [datakey.go](../../internal/blob/datakey.go) | `StorageKeys` with `Active`, `NameKey` and `Lookup` (`*keys.Holder` implements it) |
| [connections.go](../../internal/blob/connections.go) | the binding of the replica's keys and metrics to each connection (`New` does it), `KeyedNames`, the naming function `Name` and `ObjectKey` |
| [sealedonly.go](../../internal/blob/sealedonly.go) | the client of an encrypted connection: a write of a blob's object without a record, and any copy from another bucket into one, is refused before it is sent (Q-STOR-382, `ErrPlaintextRefused`) |
| [sealedwrite.go](../../internal/blob/sealedwrite.go) | `sealing` (a data key, a seed and a record per write), `putSealed` (one request or the multipart of [transfer.go](../../internal/blob/transfer.go)), `StagedSealed` and the finalize of a session's staging object: the copy with the record, or the stream of the staging parts as stored (`streamStaged`) |
| [sealedread.go](../../internal/blob/sealedread.go) | the reader of every connection: `Read` (for the stage-1 jobs, without the blob's digest), `ReadBlob` (with it), the refusal of a record on a plain connection (Q-STOR-383), `sealedBody`, and `sealedDownload` (the marks and the stamp of a download) |
| [download.go](../../internal/blob/download.go) | `Store.Open` over `ReadBlob`: a refusal before the first byte marks the copy as its reason says and falls over; `unknown_key` marks nothing |
| [startcheck.go](../../internal/blob/startcheck.go) | `ErrMetadataNotKept`: an encrypted connection on a store without metadata stops the start |
| [conformance_sealed.go](../../internal/blob/conformance_sealed.go) | the conformance check of the stored format against a store: a write with the record, a whole and a ranged read, a copy that replaces the record |
| [metrics.go](../../internal/blob/metrics.go) | `quader_storage_encrypted_read_failures_total{connection,reason}`, the fall-over reason `record` |
| [uploads/sealed.go](../../internal/uploads/sealed.go) and the changes of [service.go](../../internal/uploads/service.go), [append.go](../../internal/uploads/append.go), [finalize.go](../../internal/uploads/finalize.go), [rows.go](../../internal/uploads/rows.go) | the sessions: `SessionKeys`, the data key drawn at the start or the move and sealed into the row, opened per request with a seed of its own (`withCrypt`), the parts as data segments, the tails as tail segments, the counter (Q-STOR-380), the last part with the final segment or the final segment alone (Q-STOR-381), a blob of tails only committed from its decrypted tails |
| [uploads.sql](../../internal/meta/postgres/queries/uploads.sql), [blob_copies.sql](../../internal/meta/postgres/queries/blob_copies.sql) | the session's sealed key, counter and parts as stored; the copy row's `storage_key_id`; `keyed_names` written and compared with the connection's other fixed properties |
| [wire_storage_keys.go](../../internal/app/wire_storage_keys.go), [wire_blob.go](../../internal/app/wire_blob.go) | the element's `keyed_names` for `Ensure`, `sessionKeys` over the key ring for the sessions |

**A write** (`putOn`) to an encrypted connection asks the holder for the connection's active storage key, names the object with the
connection's naming function (a keyed name below `keyed/` by default), draws a data key and a seed, wraps the key into the record, and sends
`NewSection` over the spool: one `PutObject` of `S(L)` bytes with the record up to `multipart.threshold`, the multipart above it with the
record on `CreateMultipartUpload`, each part's segments from `offset / 64 KiB` and the final segment after the last part's. The payload checksum
is a first pass over the section, the SDK's retry a `Seek` to 0. After the multipart, `HeadObject` must answer `S(L)` and the record's storage
key. The copy row records `S(L)`, the storage key and `verified` where the connection enforces payload checksums. With `min_copies` above 1 each
encrypted connection gets its own data key.

**A read** (`Store.Open`) takes the copies from the item lookup as before and calls `ReadBlob` with the blob's digest and size: a whole read is
one `GetObject`, a ranged read one ranged `GetObject` of the window's segments (and the final segment with them when the window reaches the
last data segment). The object size the store answers must be the copy's `stored_size`; then `blobcrypt.Open` runs steps 1 to 6 with the
holder's lookup, and only then is a reader returned. A refusal before the first byte is counted, marks the copy `corrupt` unless its reason is
`unknown_key`, and the read falls over; a refusal of the body marks and aborts the response. The reasons and which of them mark are
`blobcrypt.Reason` and `Reason.MarksCorrupt`: a further reason (the `destroyed_key` of a later package) is a constant and a case there, and
nothing here changes. A plain connection refuses an object that carries a record as `format`.

**A session** on an encrypted connection seals a data key into `upload_sessions.sealed_data_key` at its start (or at its move to an encrypted
connection before its first write); each request opens it with the key ring through `SessionKeys`, draws its own seed and destroys the key at
its end. A part is the data segments of its plain bytes from the part's offset; a tail object the tail segments at its session offset, read back
through `NewTailReader`, whose refusal fails the session like a tail of another checksum. The claim reserves `⌈part_size / 64 KiB⌉ + 2`
segments and the records add what each part and tail sealed; past `2^31` the request answers `409 session_exhausted` and the session is aborted.
The finalize sends the tail with the final segment as the last part (or the final segment alone, unrecorded, when the blob ends at a part
boundary), completes the upload and checks `S(L)`; the commit copies the staging object to the content name with a record that wraps the
session's data key (`copyReplacesMetadata`) or streams its parts as stored (without it), and a blob that never filled a part is written from its
decrypted tails with a new data key. A copy to another connection (`min_copies` above 1) reads the content object through the connection's
reader and writes it with a new data key.

Until package 4 the stage-1 jobs passed over or refused an encrypted connection (Q-STOR-384, ended); what each job does now is in
[package 4](#package-4-as-built). The collection deletes by the copy row's `object_key`, which is the keyed name, and `restore-versions`
removes delete markers, whose versions keep their records; neither changed. The revocation journal, the marker and the probes stay plain
(Q-STOR-315), which the connection's client lets pass.

**What package 6 builds on:** the counter `quader_storage_encrypted_read_failures_total` for `QuaderStorageUnknownKey`; the
chart's render, which still refuses `encrypted: true`; the connections' API, built ahead with stage 1's package F, which shows
`storage_connections.keyed_names` as `keyedNames` and counts `blob_copies.storage_key_id` per key in `storageKeys[].copies` through the storage
summary, whose `presentBytes` sum `stored_size` (S(L) on an encrypted connection); `TestR_STOR_084`'s first subtest shows the three on an
encrypted connection. **The signed links** (their second package, landed before this one): an encrypted connection's client wraps the guard
(`sealedOnly`) and forwards `Presign` and `Fetcher`, so the link check runs on an encrypted connection with its plain probe (R-STOR-074,
R-STOR-096) and passes where the store passes it; the connection serves no link, decided by `downloadlinks` on `Connection.Encrypted()`, and a
blob whose only copy lies there streams (`TestR_STOR_088_AnEncryptedConnectionsOnlyCopyIsStreamed` in `internal/downloadlinks`).

**Tests and their cost**, measured on 2026-11-22 in the unit job's mode (`go test -race -covermode=atomic -coverpkg=./...`) on the development
machine, and the integration cases under the stack's lock:

| Test | Tier | Data | Time |
|---|---|---|---|
| `TestR_STOR_080_WritingToAnEncryptedConnection` (1 KiB below `keyed/` with its record and copy row; 192 KiB in parts of 64 KiB with a part sent twice; the `HeadObject` check; two writes of one content; the client's refusals) | unit, `internal/blob`, two in-memory stores | up to 192 KiB | 0.18 s |
| `TestR_STOR_082_ReadingFromAnEncryptedConnection` (whole and ranged reads and the segments asked for; five mutations of R-STOR-078 on a whole and a ranged read; `unknown_key`; `verify: false`; the refusal on a plain connection; the stage-1 reader) | unit, `internal/blob` | a blob of 196,609 bytes | 0.34 s |
| `TestR_STOR_086_KeyedNamesDifferPerConnection`, `TestR_STOR_091_TheConformanceCheckOfTheStoredFormat` | unit, `internal/blob` | probe objects of 64 KiB and 100 bytes | 0.04 s |
| `TestR_STOR_081_UploadSessionsOnAnEncryptedConnection` (three requests with the copy and with the stream, a blob that ends at a part boundary, tails only, a changed tail, the counter) | unit, `internal/uploads`, two in-memory stores | parts of 64 KiB, below 256 KiB | 0.24 s |
| `TestR_STOR_075_AnEncryptedConnection`, `TestR_STOR_075_AnEncryptedConnectionNeedsMetadata` | unit, `internal/config`, `internal/app` | — | 0.09 s |
| `TestR_STOR_088_AnEncryptedConnectionsOnlyCopyIsStreamed` (the link check on an encrypted connection; no link for a blob whose only copy lies there) | unit, `internal/downloadlinks`, two in-memory stores | probes of the link check, an object of 64 bytes | below 0.01 s |
| `TestR_STOR_084_ARotationOfTheRootTouchesNoObject` (with the connections' list of the API over the same three blobs) | integration, area `blob`, PostgreSQL of the stack, an in-memory store | three blobs of 1 KiB | 0.9 s, most of it the migration of its database |
| the encrypted pass of `TestConformance` | integration, area `blob` and the job `s3-conformance`, the stack's store | a probe of 64 KiB | 0.04 s against Silo (the check runs in the loop of the checks and once more for its own assertion) |
| `TestR_STOR_081_ASessionOnAnEncryptedConnectionInPostgres` (the session's statements: the sealed key, the parts as stored, the counter, the claim's refusal) | integration, area `uploads`, the stack | one part of 5 MiB (the smallest S3 takes) and 10 KiB | 1.3 s |

The unit job gains about 0.9 s of tests and no package (every touched package runs already); the integration shard that runs `blob` gains
about 1 s, the one that runs `uploads` about 1.3 s, `s3-conformance` about 0.04 s; no chart test, end-to-end leg or nightly job
is added. With 25 formats the cost is the same: no format sees a connection or a key.

**Every defence taken out once**, one at a time, and the test that failed: the record's binding to the blob's digest in `ReadBlob` (the two
cases of "the object and record of another blob under this name"); the `HeadObject` check of the record after a multipart write ("a multipart
object whose HeadObject names another storage key fails the write"); the client's refusal of a plain object on an encrypted connection ("a write
of a blob's object without a record is refused"); the refusal of an object of the format on a plain connection ("a plain connection refuses an
object of the stored format"; with the copy row's size equal to the object's, so that the size check alone does not catch it); the session's
counter, in the service and in the simulated claim ("a request whose session's counter would pass the bound answers 409"), and in the claim's
statement of `uploads.sql` (the 409 case of `TestR_STOR_081_ASessionOnAnEncryptedConnectionInPostgres`); the keyed name (the 1 KiB case, the 192 KiB case, the `HeadObject` case, both session cases with a content object, and
`TestR_STOR_086`); `unknown_key` marking nothing ("a record of a storage key the replica does not hold falls over …"); the client's
forwarding of `Fetcher`, without which the link check of an encrypted connection has no client to fetch with ("an encrypted connection serves
no link and can serve a handover" of `TestR_STOR_074_WhichConnectionsCanServeLinks`).

## Package 4 as built

[R-STOR-087](../spec/storage.md#r-stor-087--the-reconciliation-the-scrub-the-orphan-sweep-and-collection-on-encrypted-connections) (the jobs) and
[R-BAK-031](../spec/backup-and-restore.md#r-bak-031--backup-and-restore-of-encrypted-connections),
[R-BAK-032](../spec/backup-and-restore.md#r-bak-032--the-pools-keys-their-record-and-the-re-pool) with Q-BAK-76 (the backup and the restore),
built on 2026-11-23 with the builder's decisions
[Q-STOR-440](../spec/storage.md#q-stor-440--a-stream-into-an-encrypted-connection-holds-one-part-in-memory-not-the-spool) to
[Q-STOR-442](../spec/storage.md#q-stor-442--the-operator-check-opens-a-records-steps-1-to-6-and-reports-unknown_key-as-a-finding-record) and
[Q-BAK-80](../spec/backup-and-restore.md#q-bak-80--a-re-pooled-pin-starts-with-the-pool-object-it-inherits) to
[Q-BAK-83](../spec/backup-and-restore.md#q-bak-83--a-deep-verification-without-the-key-ring-reads-an-encrypted-pool-objects-record-from-its-head).
Q-STOR-384 ended with it: the scrub's filtered view (`scrubbed`), `Connections.Without` and the refusals of `quader backup create` and
`restore` are gone, and with them the gaps H-1410 and H-1411. The rule every path keeps: plaintext of an encrypted connection's blob is
written only into a plain connection the reconciliation fills, or into a plain first connection of a restore; the pool keeps an encrypted
copy as stored.

<details>
<summary>The files, by area</summary>

| File | Responsibility |
|---|---|
| [sealedstream.go](../../internal/blob/sealedstream.go) | `Connection.WriteSealedStream`: a stream of known digest and size into an encrypted connection with a new data key under the active storage key, one part in memory (Q-STOR-440), the plaintext hashed as it passes and checked before the last part completes; `StreamSourceError` (the source's failure, evidence against it) and `ErrStreamNotBlob`; `Connection.UseKeys` for a connection without a replica's holder |
| [record.go](../../internal/blob/blobcrypt/record.go) | `OpenRecord` (steps 1 to 3 and 6 without a copy row: the orphan sweep) and `CheckRecordForm` (the record's four keys and form without a key: the backup pod) |
| [reconcile/copy.go](../../internal/blob/reconcile/copy.go), [queue.go](../../internal/blob/reconcile/queue.go) | the four cases: plain to plain as before; into an encrypted connection `sealed` (the source's `ReadBlob` into `WriteSealedStream`); encrypted to plain through the source's reader; a refusal of a marking reason is evidence against the source, `unknown_key` is not; the copy row records the object's key, `S(L)` and the storage key |
| [scrub/read.go](../../internal/blob/scrub/read.go), [repair.go](../../internal/blob/scrub/repair.go), [scrub.go](../../internal/blob/scrub/scrub.go), [recheck.go](../../internal/blob/scrub/recheck.go), [ledger.go](../../internal/blob/scrub/ledger.go), [metrics.go](../../internal/blob/scrub/metrics.go) | a whole read through the connection's reader judged by its reason (`judge`); `unknown_key` counted (`UnknownKey`, the result `unknown_key`), not marked; the repair writes an encrypted copy anew with `WriteSealedStream` and records its stored size and key (`Ledger.Whole`); [wire_storage_scrub.go](../../internal/app/wire_storage_scrub.go) hands the scrub every connection |
| [sweep_sealed.go](../../internal/blob/sweep_sealed.go), [sweep.go](../../internal/blob/sweep.go) | an encrypted connection's objects without a copy row placed by their record (`place`, `placeOne`): adopted, claimed as an orphan, or reported foreign and never deleted (Q-STOR-441) |
| [blob/verify/verify.go](../../internal/blob/verify/verify.go), [admin_storage.go](../../cmd/quader/admin_storage.go) | the operator check opens each encrypted copy's record from its `HeadObject` (finding `record` with the reason, Q-STOR-442); the command loads the key ring only when a connection is encrypted |
| [blobtest/keys.go](../../internal/blob/blobtest/keys.go) | `blobtest.Keys`, a replica's storage keys for unit tests (`Draw`, `Add`, `Forget`) |
| [format/bloblist.go](../../internal/backup/format/bloblist.go), [layout.go](../../internal/backup/format/layout.go), [manifest.go](../../internal/backup/format/manifest.go), [manifest.schema.json](../../internal/backup/format/manifest.schema.json) | the blob list's third column (`-`, a key's identifier, `+` only in the provisional list), `PoolObject` and `ParsePool` of `pool/sha256/<h0h1>/<h2h3>/<hex>[.<key id>]`, the manifest's required `storageKeys` |
| [create/diff.go](../../internal/backup/create/diff.go), [copy.go](../../internal/backup/create/copy.go), [finalize.go](../../internal/backup/create/finalize.go), [run.go](../../internal/backup/create/run.go), [create.go](../../internal/backup/create/create.go) | the provisional list and the pins with their pool, the re-pool of entries whose key's connection departs (Q-BAK-80, Q-BAK-81), the copy order (Q-BAK-76), an encrypted copy taken as stored (record form, stored size, the record of every ranged read, the entity tag of a server-side copy), the final list at the end of the phase `copying`, `storageKeys`, `repooled` and the warning for keys of connections that are gone |
| [storagekeys.go](../../internal/backup/storagekeys.go), [pgsnapshot.go](../../internal/backup/pgsnapshot.go), [runstore.go](../../internal/backup/runstore.go), [blobsource.go](../../internal/backup/blobsource.go), [runner.go](../../internal/backup/runner.go) | the backup's `storage_keys` read from its table file and opened with a key ring (`ReadBackupKeys`, `SealedKeys.Open`); the snapshot's key states; `AddPins` with pools and `SetPinPools`; the copy's head with record and entity tag; `retained[].poolKeyIds` and `sealedKeyIds` |
| [target/store.go](../../internal/backup/target/store.go), [s3/target.go](../../internal/blob/s3/target.go), [target_writer.go](../../internal/blob/s3/target_writer.go), [target_copy.go](../../internal/blob/s3/target_copy.go), [blobsink.go](../../internal/blob/s3/blobsink.go) | user metadata on the target's head, get, put and multipart; `ObjectRef.IfMatch` and `ErrSourceChanged`; the sink's `CopyAs` for a pool object written as stored |
| [retention/sweep.go](../../internal/backup/retention/sweep.go), [stage.go](../../internal/backup/retention/stage.go) | the keep set by pool name; the retained entries' key identifiers |
| [backup/verify/verify.go](../../internal/backup/verify/verify.go), [backup_verify.go](../../cmd/quader/backup_verify.go) | `verify` by pool name; `--deep` on an encrypted pool object from its head (`not_authenticated`, Q-BAK-83) or, with `--key-ring`, through the format's reader |
| [restore/sealed.go](../../internal/backup/restore/sealed.go), [checks.go](../../internal/backup/restore/checks.go), [blobs.go](../../internal/backup/restore/blobs.go), [restore.go](../../internal/backup/restore/restore.go) | check 3's storage keys, the keys drawn for a new encrypted sink and kept in the marker (Q-BAK-82), the restore's table of R-BAK-031, a resumed restore's check of the sink's objects |
| [fixup/storage_copies.go](../../internal/backup/fixup/storage_copies.go), [migrate_restore.go](../../internal/meta/postgres/migrate_restore.go), [backup_load.go](../../internal/meta/postgres/backup_load.go) | the drawn keys inserted, the copy rows of an encrypted sink under its names with `S(L)` and the record's key, the connection's `encrypted` and `keyed_names` |
| `00111_backup_pin_pool.sql`, [backup.sql](../../internal/meta/postgres/queries/backup.sql), [backup_snapshot.sql](../../internal/meta/postgres/queries/backup_snapshot.sql), [backup_restore.sql](../../internal/meta/postgres/queries/backup_restore.sql), [blob_scrub.sql](../../internal/meta/postgres/queries/blob_scrub.sql), [sweep.sql](../../internal/meta/postgres/queries/sweep.sql) | `backup_blob_pin.pool`; the statements above |

</details>

**What each job does with an encrypted connection.**

| Job | What it does |
|---|---|
| Reconciliation | into an encrypted connection: the source read through its reader (verified against the digest) and written with a new data key under the target's active key, never the source's ciphertext; out of one into a plain connection: the plaintext through the source's reader; a server-side copy only between plain connections |
| Scrub | reads every copy whole through the reader: a refusal of `format`, `record`, `size`, `commitment`, `segment`, `final` or `digest` marks the copy `corrupt` and queues the repair, `unknown_key` is counted and the copy stays due; the repair writes from another connection's copy, with a new data key on an encrypted one |
| Orphan sweep | an object without a copy row is placed by its record: adopted for a live blob (`present`, not verified; `corrupt` when the store's size is not `S(L)`), claimed when no blob row exists and it is older than `min_age`, otherwise reported foreign and never deleted |
| Operator check | opens each encrypted copy's record from the `HeadObject` it sends anyway; a refusal is a finding `record` with its reason |
| Collection, `restore-versions` | unchanged |
| Backup | an encrypted copy into the pool as stored, named by the key its record names; the pod holds no key |
| `verify --deep` | without `--key-ring` the record and size from the head, counted `not_authenticated`; with it, every encrypted pool object read through the reader |
| Restore | the table of R-BAK-031: as stored into the connection of the key's own id, decrypted and sealed anew into another encrypted id, decrypted and hashed into a plain one; refused at check 3 or step 3 when the backup lacks a key the pool names |

**Tests and their cost**, measured on 2026-11-23 in the unit job's mode (`go test -race -covermode=atomic -coverpkg=./...`) on the development
machine, and the integration cases with `test/integration/run.sh`:

| Test | Tier | Data | Time |
|---|---|---|---|
| `TestR_STOR_087_TheReconciliationsFourCases` (the four cases, a blob of three segments in parts, a plain source that is not the blob, a changed segment and an unknown key at the source) | unit, `internal/blob/reconcile`, in-memory stores | up to 128 KiB | 0.01 s |
| `TestR_STOR_087_TheScrubOverEncryptedCopies` (a verified read; one mutation per marking reason, `digest` by a forger who holds the key; `unknown_key`; a plain copy repaired from an encrypted one) | unit, `internal/blob/scrub` | blobs of 1 KiB | 0.01 s |
| `TestR_STOR_087_TheOrphanSweepPlacesAnObjectByItsRecord`, `TestR_STOR_087_TheOperatorCheckOpensTheRecordOfAnEncryptedCopy` | unit, `internal/blob`, `internal/blob/verify` | objects of 1 KiB | below 0.01 s |
| `TestR_BAK_031_ThePoolKeepsAnEncryptedCopyAsStored`, `TestR_BAK_032_TheCopyOrderAndTheRepool`, `TestR_BAK_032_TheFinalListResolvesThePendingLinesFromThePins` | unit, `internal/backup/create`, the pipeline's fakes | blobs of 580 bytes | 0.06 s |
| `TestR_BAK_031_AServerSideCopyReadsTheSourceOnlyWhileItsEntityTagHolds` | unit, `internal/blob/s3`, a fake S3 | a single copy and three part copies, no body | 0.02 s |
| the pool column, `ParsePool` and `storageKeys` in `TestBlobList`, `TestLayout`, `TestManifest`; `TestR_BAK_032_TheKeepSetIsASetOfPoolNames` | unit, `internal/backup/format`, `retention` | lists of a few lines | 0.03 s |
| `TestR_BAK_031_VerifyDeepOfEncryptedPoolObjects`, `TestR_BAK_031_TheRestoresTable` | unit, `internal/backup/verify`, `restore` | blobs of 1 KiB | 0.08 s |
| `TestR_STOR_087_TheJobsOnAnEncryptedConnection` (a reconciliation run with the summary's counts, the scrub's repair, the sweep's adoption, foreign object and orphan, the drain, a second encrypted connection) | integration, area `blob`, PostgreSQL of the stack, in-memory stores | ten blobs of 1 KiB | 3.1 s |
| `TestR_BAK_031_EncryptedConnectionsBackedUpAndRestored` (three backups, `verify --deep` without and with the key ring and a flipped bit, two restores into empty databases, the downloads, the drain and the re-pool, `keep_last: 1`) | integration, area `backup`, PostgreSQL and the stack's S3 | three blobs of 1 KiB | 6.4 s, 3.4 s of it the two restores (15.3 s in a run beside other builders' gates) |

The unit job gains about 0.2 s of tests and no package (every touched package runs already); the integration shard that runs `blob` gains
about 3 s and the one that runs `backup` about 6 s; no chart test, end-to-end leg, `s3-conformance` or nightly job is added. With 25
formats the cost is the same: no format sees a connection, a key or the pool.

**Every defence taken out once**, one at a time, and the test that failed:

| Defence | Taken out as | Failed |
|---|---|---|
| a new data key at a copy between two encrypted connections | the source's ciphertext and record copied as stored | the encrypted-to-encrypted case and the changed-segment case of `TestR_STOR_087_TheReconciliationsFourCases` |
| the scrub's mark for each reason that marks | each of `format`, `record`, `size`, `commitment`, `segment`, `final`, `digest` judged not to mark, one at a time | that reason's case of `TestR_STOR_087_TheScrubOverEncryptedCopies` (the `digest` case was added when its take-out passed) |
| `unknown_key` not marking | judged corrupt | its case of `TestR_STOR_087_TheScrubOverEncryptedCopies` |
| the sweep placing an object only under the name of the digest its record binds | the name not compared | two cases of `TestR_STOR_087_TheOrphanSweepPlacesAnObjectByItsRecord` |
| the sweep never deleting an object below `keyed/` it cannot place | such an object deleted | the sweep's case of `TestR_STOR_087_TheJobsOnAnEncryptedConnection` (integration) |
| a sound blob never recorded as damaged while one copy fails and another is present | the first copy that fails decides | four cases of `TestR_BAK_030_TheBackupReadsOneCopyPerBlob`, one of `TestR_BAK_006_SourceDamage`, the record case of `TestR_BAK_031_ThePoolKeepsAnEncryptedCopyAsStored` |
| the key in the pool object's name | `PoolObject` without it | `TestR_BAK_031_ThePoolKeepsAnEncryptedCopyAsStored`, `TestR_BAK_032_TheCopyOrderAndTheRepool`, `TestLayout`, `TestR_BAK_032_TheKeepSetIsASetOfPoolNames` |
| the restore's refusal of a pool object whose key the restored database does not hold | step 3's refusal; check 3's refusal, one at a time | the last case of `TestR_BAK_031_TheRestoresTable` each time (without step 3's refusal the object is still refused, as `unknown_key`, under another message) |
| the entity tag on a server-side copy of an encrypted copy | not set | the entity-tag case of `TestR_BAK_031_ThePoolKeepsAnEncryptedCopyAsStored` |

**What packages 6 and 7 build on.** Package 7's `destroyed_key` is a reason that marks: the scrub's `judge` and the reconciliation's
`sourceFailure` follow `Reason.MarksCorrupt`, the sweep reports every refused record as foreign, the operator check every refusal as a
finding `record`, so none of them changes; the restore's check 3 already refuses a destroyed key the pool names and an encrypted connection
of an id whose keys the backup holds destroyed (`SealedKey.Destroyed` reads the state `destroyed` or an empty sealed value). Every backup
since this package writes `storageKeys` into its manifest and `retained[].poolKeyIds` and `sealedKeyIds` into its result (never omitted),
which R-STOR-100's check reads; a retained manifest without `storageKeys` gives `null` there, which that check counts as unknown. Package 6 finds the counter of `unknown_key` copies in `quader_storage_scrub_copies_total` beside
`quader_storage_encrypted_read_failures_total`, and `OpenRecord` and `CheckRecordForm` for its offline tool; `quader backup verify --deep
--key-ring` takes a directory of the key ring's files, which no Job of the chart mounts, by R-BAK-012.

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

## Package 6: the offline tool, the chart, the API and the documentation as built

[R-STOR-089](../spec/storage.md#r-stor-089--the-offline-tool) (the offline tool), R-STOR-088's alert, the API's acceptance case and the
API's `state: destroyed` with `destroyedAt`, [R-HA-072](../spec/ha-and-helm.md#r-ha-072--the-object-store-connections-in-the-chart)'s
acceptance of `encrypted` and refusal of `keyed_names` alone, [R-HA-045](../spec/ha-and-helm.md#r-ha-045--monitoring-integration)'s row
`QuaderStorageUnknownKey`, and R-STOR-090's sentences in the README and the manual, with the decisions
[Q-STOR-450](../spec/storage.md#q-stor-450--a-pull-writes-a-directory-of-its-own-the-objects-their-record-files-and-the-connections-facts)
to Q-STOR-452 and the gaps H-1450 and H-1451 of [stored-content-keys.md](../security/stored-content-keys.md#what-this-does-not-cover).

| File | Responsibility |
|---|---|
| [offline/keys.go](../../internal/blob/offline/keys.go) | `LoadKeys`: the export read strictly (the rows of R-STOR-085, a destroyed key of R-STOR-103 without `sealed` and with `destroyedAt`), the key ring of a directory of `<key id>.key` files (only opening, so the first file is named active), the root keys' check before any key is opened, every sealed key opened once; `Keys.lookup` (a key of the export, an error for a destroyed one, nil for an unknown one), `nameKey`, `Close`; `Refusal` |
| [offline/names.go](../../internal/blob/offline/names.go) | `parseName` (the three families `keyed/`, `sha256/` and a backup's `pool/sha256/` with its `.<key id>`), `checkName` (a keyed name under the name key of the record key's connection, a plain name carrying the digest, a pool name carrying the digest and the key) |
| [offline/source.go](../../internal/blob/offline/source.go) | `Source`; `Bucket` over any `blob.ObjectStore`; `OpenElement`: the element of a file read through `config.Load` as the only element of `storage.connections` (`CommandImport`, an empty environment), so its rules and defaults are the binary's, and the client built with the default outbound settings; `Dir`: the directory of a pull, its `quader-pull.json`, names held below the directory |
| [offline/read.go](../../internal/blob/offline/read.go) | `Reader`: `check` (one Head, `blobcrypt.Open` with `Want{Known: false}` against the size the source answered, the name), `readBody` (`blobcrypt.NewReader` with the SHA-256, or a plain object hashed against its name), `List`, `Verify` (n at once, results in the walk's order), `Locate`, `Get` (a temporary file beside `--out`, a hard link that never replaces a file) |
| [offline/pull.go](../../internal/blob/offline/pull.go) | `Pull`: an empty or new directory, the manifest first, every object of a name of the layout with its record file, every file created new |
| [admin_storage_offline.go](../../cmd/quader/admin_storage_offline.go) | `quader admin storage offline list`, `get`, `verify`, `pull`: the flags (files only, no flag takes a key), the text lines and the streamed JSON document, the exit codes |
| [_storage.tpl](../../deploy/helm/quader/templates/_storage.tpl), [_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl), [values.yaml](../../deploy/helm/quader/values.yaml), [ci/connections.yaml](../../deploy/helm/quader/ci/connections.yaml) | `encrypted` passed through, `keyed_names` without `encrypted: true` refused; `QuaderStorageUnknownKey`; `encrypted: false` in the default element; the value set's second connection encrypted |
| [storage_connections_api.go](../../internal/api/v1/storage_connections_api.go), [summary/connections.go](../../internal/blob/summary/connections.go), `StorageConnectionKey` of [openapi.yaml](../../api/openapi.yaml) | `destroyedAt` of a key, `null` for any other; `state` names `destroyed` |

**The offline tool, step by step.** `LoadKeys` reads the export (at most 4 MiB, one JSON document, the identifier rule of Q-STOR-324, the
three states) and the ring, names every root key the export is sealed with that the ring lacks, then opens every sealed key. A source walks
`<prefix>/keyed/`, `<prefix>/sha256/` and `<prefix>/pool/sha256/` in name order. Per object `check` sends one Head (or reads the record file
of a pull's directory); an object without a record on a plain layout of a plain connection is `plain` with the digest its name carries;
otherwise `blobcrypt.Open` runs steps 1 to 6 with the export's lookup and the size the source answered, and the name is compared with the
wrap's digest (Q-STOR-452). `List` stops there; `Verify` and `Get` read the body through `blobcrypt.NewReader` with the SHA-256, which holds
the last segment back until the final segment and the digest passed. `Get` writes into `CreateTemp(dir(out), ".<out>.partial-*")` (mode
0600) and links it to `--out` only after the reader's end, so a refusal leaves nothing (Q-STOR-451); `os.Lstat` refuses an existing
`--out` first and the link refuses one that appeared meanwhile. `Pull` needs no key and copies the bytes as stored (Q-STOR-450). The reasons
of `blobcrypt` are the statuses as they are; `destroyed_key` is the export's tombstone, a status of the tool and no reason of `blobcrypt`
(package 7 adds its reason there for the store's reader). No message holds a key: `blobcrypt`'s refusals carry steps and positions, the
tool's carry identifiers, root keys, files and object names.

**The independent check of the format, a second time.** The offline tool is the second reader of the stored format beside the store's: it
reads without the copy rows (`Want{Known: false}`), so the digest and size come from the wrap and the name stands in for the database.
`TestR_STOR_079_TheOfflineToolReadsEveryObjectOfTheVectors` lays every object case of [testdata/vectors.json](../../internal/blob/blobcrypt/testdata/vectors.json)
into a pull's directory at the plain name of its copy's digest with the vectors' record file and an export of its reader keys sealed by a
ring of the test: **33 of the 33 object cases read as the vectors say** on the first run, on 2026-11-23: the 8 genuine objects `ok` with
their digest and size, the 25 negative ones refused with their reason, of which the two whose `record` comes from the copy's digest
(`other-name`, `other-record`) are `name_mismatch` without a database. The keyed name's known answer (R-STOR-086) reads `ok` at its name
and `name_mismatch` at another. The staging tail (`tail-131072`) is no object of a connection and is not read by the tool.
`TestR_STOR_089_TheOfflineToolReadsWhatTheStoreWrote` (`internal/blob`) holds the second reader to the first writer: two blobs written by the
store to an encrypted connection with keyed names, one of 131,073 bytes in three multipart parts of 64 KiB, verify and get `ok`, and a bit
flipped in the second data segment (the second part's) is `segment`.

**The chart.** `encrypted` passes through like every key of an element; `keyed_names` on an element without `encrypted: true` fails the
render with the binary's sentence; nothing else of R-HA-072 changes (the part sizes stay the binary's to check: the chart's byte parser
does not read every unit the binary does). The chart renders no key and no Secret for the stored content (Q-HA-415):
`TestR_HA_072_AnEncryptedConnection` compares every pod's volumes with a plain connection's. `ci/connections.yaml` renders its second
connection encrypted, so `TestRenderedConfigurationPassesTheBinary` runs the binary's validator over an encrypted element in every pod.

**The alert.** `QuaderStorageUnknownKey` is `sum by (connection) (increase(quader_storage_encrypted_read_failures_total{reason="unknown_key"}[5m])) > 0`
for 10 minutes, critical: reads that keep meeting unknown keys fire; one such read does not (its window holds 5 minutes, the alert waits
10); `reason="destroyed_key"` and every other reason never do. Its promtool test has the three cases.

**The API.** `storageKeys[].destroyedAt` is in the contract and mapped from `summary.StorageKey.DestroyedAt`; the statement
`StorageConnectionKeys` does not select `destroyed_at`, because the column is package 7's migration: until then every key's
`destroyedAt` is `null`. `TestR_STOR_088_AnEncryptedConnectionInTheAPI` shows the acceptance case of R-STOR-088 (an encrypted connection
with `keyedNames`, its two generations, the copies per key, no sealed value) and a destroyed key of a retired connection, from views written
by the test. The tab Storage prints a key's state as the API gives it and does not show `destroyedAt`; nothing on its screen changes with
this package (the column **Encrypted** read `encrypted` already).

**What package 7 must know.** The export's destroyed entry is read by the tool in the form of R-STOR-103 (`state: destroyed`,
`destroyedAt`, `retiredAt`, no `sealed`); `keys.DocumentKey` and `checkKeys` of [export.go](../../internal/blob/keys/export.go) do not
know it yet, so the export and the import of package 7 add `DestroyedAt` (`omitempty`) and make `Sealed` `omitempty`, and the tool's test
`…a record of a key the export lists as destroyed is destroyed_key…` is the check that the two agree. The API needs only the column in
`StorageConnectionKeys` and its mapping in `summary.Rows`. The alert needs nothing: it never counts `destroyed_key`, and the destroyed case
of its promtool test is in place.

**Tests and their cost**, measured on 2026-11-23 in the unit job's mode (`go test -race -covermode=atomic -coverpkg=./...`) on the
development machine, and the integration case under the stack's lock:

| Test | Tier | Data | Time |
|---|---|---|---|
| `TestR_STOR_089_TheOfflineReader` (destroyed and unknown keys, an export made before, foreign names and pull's skip, plain objects, pool objects and `Locate`, the directory's bounds, refused exports), `TestR_STOR_089_TheConnectionElement` | unit, `internal/blob/offline`, the in-memory store and temporary directories | objects of 10 bytes to 70 KB | 0.2 s |
| `TestR_STOR_079_TheOfflineToolReadsEveryObjectOfTheVectors` | unit, `internal/blob/offline` | the vector files, 651,536 bytes, one directory per case | 0.26 s |
| `TestR_STOR_089_TheOfflineToolReadsWhatTheStoreWrote` | unit, `internal/blob` (the sealed fixture) | 1 KiB and 131,073 bytes | 0.03 s |
| `TestR_STOR_089_QuaderAdminStorageOffline` (list, get and its refusal of an existing file, pull and `verify --dir`, a flipped bit, a ring without the root key, the usage, no key in any output) | unit, `cmd/quader`, the in-memory store | three objects of 1 KiB | 0.04 s |
| `TestR_STOR_088_AnEncryptedConnectionInTheAPI` | unit, `internal/api/v1` (the shared validator, parsed once per package) | — | 0.02 s |
| `TestR_HA_072_AnEncryptedConnection`, the two rows of `keyed_names` in `TestR_HA_072_Refusals`, the alert's rows of `TestR_HA_045_PrometheusRule` and its promtool case | chart | renders | about 0.5 s |
| `TestR_STOR_089_TheOfflineToolReadsTheStacksBucket` | integration, area `blob`, PostgreSQL and the S3 store of the stack | 1 KiB and 131,073 bytes | 0.8 s |

The unit job gains about 0.55 s of tests and one package's start (`internal/blob/offline`, about 2 s of process time, almost all of it the
start of a binary built with the race detector and the module's coverage); the chart job about 0.5 s; the integration shard that runs
`blob` about 1 s; no end-to-end leg, nightly job or `s3-conformance` gains anything. With 25 formats the cost is the same: no format sees
a connection or a key.

**Every defence taken out once**, one at a time, and the test that failed: the digest's check of an encrypted object
(`blobcrypt.NewReader` without the SHA-256: the vectors' `spliced` case); the digest's check of a plain object (`…a changed byte is
digest…`); the refusal of a record whose key the export does not hold (a lookup that falls back to another key: `…destroyed_key, one of a key
it lacks unknown_key…` and the vectors' `changed-key-unknown`); the refusal of a wrong key ring (the root keys' check and the opening's
refusal: `…a key ring without the export's root key exits 2 naming it`); the refusal to overwrite an existing file (the `Lstat` check, and
the link replaced by a rename: `…get of a digest writes its bytes and refuses to write over an existing file`); the name's check (`…another
keyed name…`, `…pool object…` and the vectors' `other-name`, `other-record`); that no key appears in an output (the data key's hex added to a
refusal's detail: `…no output of any command holds a key, a part of one or a sealed value`).

**What the integration case found.** The first run of the element's client failed every request: `OpenElement` had built it with the zero
`config.Outbound`, which trusts no root at all, while the binary's default trusts the system's store. The element is now loaded with the
configuration's defaults, `proxy.outbound` among them.

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
  - The backup's copy order puts a draining connection's copy behind every other copy since package 4 (`order` in
    [copy.go](../../internal/backup/create/copy.go), Q-BAK-76).
- **The replay handler takes the restoring configuration** (Q-HA-417): an entry whose connection `storage.connections` names is reported
  `skipped` with its reason as the result's note (printed as a warning line, listed among the summary's warnings) and changes nothing;
  the restore goes on. Its unit test has four cases: a sealed row destroyed, a destroyed row already in effect, a missing row inserted
  destroyed, and an entry for a configured connection skipped with its keys still sealed.
- **Since package 4** every backup run records `poolKeyIds` and `sealedKeyIds` for each backup it retains (`Retained` in
  [runner.go](../../internal/backup/runner.go)); a retained backup whose manifest has no `storageKeys` gives `null`, which R-STOR-100's check
  counts as unknown and refuses for, failing closed.

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

The destruction of keys (package 7), estimated (package 4's tests, measured, are [above](#package-4-as-built)):

| Test | Tier | Data | Run time (estimate) |
|---|---|---|---|
| The destruction, the refusals, a second run, forgetting at the change, a load over destroyed rows, `Ensure`'s refusal, export and import of destroyed rows, `activateHighest`, the walk skipping NULL, the backups' check (R-STOR-100's two cases) | unit, `internal/blob/keys` (keystest) | rows only | 0.08 s |
| The command: dry run, confirmation, exits `0`, `2` and `5`, JSON, audit event, outbox row, change | unit, `cmd/quader` | three keys | 0.05 s |
| A read that meets `destroyed_key` | unit, `internal/blob` (in-memory store) | objects of 1 KiB | 0.01 s |
| The journal's replay, its four cases (Q-HA-417's skip of a configured connection among them) | unit, `internal/blob/keys` | rows | 0.02 s |
| `TestR_STOR_097_…`: the CHECKs, a destruction in PostgreSQL with its event, outbox row and change, refused by a copy row and by a `backup_run` listing | integration, `test/integration/blob`, in the database of the keys' case | eight rows | 0.5 s |

The unit job gains about 0.2 s and no package start; the integration shard that runs `blob` about 0.5 s; the chart tests (the silent case of `QuaderStorageUnknownKey` on `destroyed_key` is one rule-test row), every end-to-end leg,
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

- Package 6: the offline tool against a provider other than the stack's store (Silo), and `pull` of a bucket with more than a few objects;
  `kubectl exec -i … import /dev/stdin`, the runbook's way to bring an export into a pod, was not run in a pod; the tool on Windows (the
  hard link of `get`, the paths of a pull) was not run.

- Package 3 ran against the stack's store (Silo) only: the encrypted pass of the conformance check and the session case of the area `uploads`.
  Whether the owner's in-cluster object gateway (Ceph 19.2.3, read from his cluster on 2026-11-22) keeps user metadata on a write and replaces
  it on a copy is what the start check establishes there once a release runs with an encrypted connection; its documentation was not read. The
  provider he has in mind documents a copy with new metadata only through the documentation of the software behind it (research of
  2026-11-22), not for itself.
- The SDK's retry of a part was simulated at the unit tier (a wrapper that reads a part's body twice); a retry by the AWS SDK against a store
  that fails a request was not provoked.
- A finalize that streams a staging object above a few megabytes, and the memory of its one part in flight on a replica, were not measured.
- The throughput of encryption on Quader's hardware; the proxy's measurement (about 4.5 GB/s per core) is from an Apple M5 Pro. Package 1 did
  not measure `blobcrypt`'s throughput either.
- The nightly job `fuzz-stored-format` has not run on GitHub yet: its 27 minutes are the sum of its fuzz times and an estimate of its setup,
  and whether the self-hosted runner's eight workers reach the local numbers above is not known.
- That `cipher.AEAD` from `crypto/cipher` may be used by several goroutines at once was not relied on: every `Section` and `Reader` builds its
  own cipher from the key.
- Package 7 (read, not run): whether the runtime role can run `VACUUM` on `storage_keys` (migration 00001 grants it no `MAINTAIN`, but
  whether an installation runs migrations and the replicas with the same login, which would own the table, was not read) and how much of a
  value PostgreSQL leaves readable on disk after vacuum.
- Package 4 ran against the stack's S3 with blobs of 1 KiB: a server-side copy of an encrypted copy above the single copy's limit (the
  multipart copy that sets the record and names the entity tag on every `UploadPartCopy`) ran against a fake S3 only, and whether a real
  store honours `x-amz-copy-source-if-match` on `UploadPartCopy` was not tried. What writing the final blob list costs on a first backup of
  millions of blobs (the ordered scan of the pins), and the memory of a stream's one part in flight into an encrypted connection on a
  worker, were not measured.
