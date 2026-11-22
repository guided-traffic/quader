# Encryption of stored content: the build plan

Status: specified on 2026-11-22, not built ([storage.md](../spec/storage.md#encryption-of-stored-content) R-STOR-048, R-STOR-075 to
R-STOR-091; [backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-031; [bastion.md](../spec/bastion.md) R-BAS-061;
[architecture.md](../spec/architecture.md) R-ARCH-021; [ha-and-helm.md](../spec/ha-and-helm.md) R-HA-072;
[ADR-244](../decisions.md#adr-244--encrypted-connections-quaders-own-segment-format-bound-to-the-content-the-wrapped-key-once-at-the-object-a-storage-key-per-connection-under-the-key-ring),
[ADR-245](../decisions.md#adr-245--the-bastions-cache-is-encrypted-in-the-stored-format-under-one-key-from-its-chart); security pages
[stored-content-format.md](../security/stored-content-format.md), [stored-content-keys.md](../security/stored-content-keys.md),
[stored-content-visibility.md](../security/stored-content-visibility.md)). Stage 2 of the owner's topic on the stored content. This page is
the build plan: the six packages, what each owns, the order against stage 1's packages ([blob-store.md](blob-store.md#several-connections-the-build-plan-specified-not-built)),
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
  because the export's import can still bring the key back.
- **No copy of ciphertext between connections.** Every copy into an encrypted connection is new bytes under a new data key; server-side copies
  stay a matter between plain connections.

## Work packages

At most four packages run at once, counting the packages of stage 1 that run then. Directories (or, where named, files) of packages that run
at the same time are disjoint.

| Package | After | What it builds | Owns |
|---|---|---|---|
| **1 · The format and its tools** | nothing; it can start before stage 1 lands | The codec of R-STOR-076 to R-STOR-078: segments, the final segment, `S(L)` and its inverse, the range planner, the nonce derivation of R-STOR-080, the record (encode, decode, wrap, unwrap, commitment), the keyed name of R-STOR-086, the tail segments of R-STOR-081, a reader that runs the checks of R-STOR-077 in their order with named errors; the vector files and the independent check of R-STOR-079 (run once, the result recorded below); the five fuzz targets, the nightly job of R-STOR-091 and its Makefile target | `internal/blob/blobcrypt/` (new), `tools/blobvectors/` (new), the job `fuzz-stored-format` in `.github/workflows/performance.yml`, a Makefile block `fuzz-stored-format` |
| **2 · The keys** | stage 1's package A (the table `storage_connections`, the connection set, the marker and the start check) | The migration of R-STOR-083 (`storage_keys`, `storage_connections.keyed_names`, `upload_sessions.sealed_data_key` and `sealed_segments`, the stored columns of `upload_session_parts`); the two sealed columns registered; drawing keys at first sight with the refusal over a used location; holding them per replica, the change area `storage_keys` and the re-read for an unknown identifier; `quader admin storage keys export`, `import` and `new-generation` with their audit events (R-STOR-085); `CopyDataKey` and its architecture rule (R-STOR-083) | `internal/blob/keys/` (new), the stage's migration and `queries/storage_keys.sql` under `internal/meta/postgres/`, `cmd/quader/admin_storage_keys.go`, `internal/app/wire_storage_keys.go`, the change area in `internal/cluster/areas.go`, the rule in `internal/archtest` |
| **3 · Encrypted connections in the store, and the sessions** | 1, 2, and stage 1's packages A and B | `encrypted: true` accepted with `keyed_names` and the part-size rule (R-STOR-075); the start check's requirement of `metadata`; the naming function for keyed names; the connection's encrypting writer and decrypting reader: the commit's writes with the record and `HeadObject`'s check, the copy row's `stored_size` and `storage_key_id` (R-STOR-080); whole and ranged reads, the marks, `unknown_key`, the metric (R-STOR-082); the sessions with the sealed data key, the tails, the counter and both finalize paths (R-STOR-081); the conformance pass with an encrypted connection; the integration case of a rotation (R-STOR-084) | `internal/config/storage.go` (stage 2's rules), `internal/blob` (all files but `gc*.go`, `sweep.go` and the packages `blobcrypt`, `keys`, `reconcile`, `scrub`, `verify`), `internal/blob/s3`, `internal/blob/blobtest`, `internal/uploads`, `internal/app/wire_blob.go`, `test/integration/blob/rotation_test.go`, the encrypted pass in `test/integration/blob/conformance_test.go` |
| **4 · The jobs, the backup and the restore** | 3, and stage 1's packages B (orphan sweep), C (reconciliation), D (scrub, operator check) and E (backup, restore) | The reconciliation's four cases and the spool for streams into an encrypted connection, the scrub's marks and repair, the orphan sweep's reading of the record, the operator check's record (R-STOR-087); the pool with records, `verify --deep` with and without `--key-ring`, the restore's table and its drawing of keys (R-BAK-031); the integration cases of the jobs and of the backup | `internal/blob/reconcile/`, `internal/blob/scrub/`, `internal/blob/sweep.go`, `internal/blob/verify`, `internal/backup`, `cmd/quader/backup_*.go`, `test/integration/blob/encrypted_jobs_test.go`, `test/integration/backup/encrypted_test.go` |
| **5 · The Bastion's cache** | 1 | R-BAS-061: the encrypted layout below the key's tree, the sealed entries, the audit spool in the format, the sweep of other trees; `bastion.cache.encryption_key_file`; `secrets.cacheKey` in the Bastion's chart and its render tests | `internal/bastion/cache`, `internal/config/bastion_role.go`, `deploy/helm/quader-bastion`, the Bastion's chart tests under `test/chart` |
| **6 · Offline tool, chart, API and documentation** | 2 and 3, and stage 1's packages E (chart) and F (API, manual) | `quader admin storage offline` (R-STOR-089); the chart's acceptance of `encrypted`, the refusal of `keyed_names` alone and the rule of `QuaderStorageUnknownKey` (R-HA-072, R-HA-045, R-STOR-088); `keyedNames` and `storageKeys[]` in the connections' API (R-STOR-088, Q-API-405); the README's and the manual's sentences of R-STOR-090; this page and the developer pages of the store brought to what runs | `cmd/quader/admin_storage_offline.go`, `deploy/helm/quader` (stage 2's lines), `test/chart` (the main chart's cases), the connections' handler file in `internal/api/v1` and its paths in `api/openapi.yaml`, `README.md`, `docs/manual`, `docs/developer` |

The order: 1 first (now); 2 as soon as stage 1's package A is on `main`; 5 beside 2, after 1; 3 when 1, 2 and stage 1's B have landed; 4 after
3 and stage 1's C, D and E; 6 after 3 with stage 1's E and F. Package 3 is the one that makes `encrypted: true` accepted; it changes the README
and the manual's pages of R-STOR-090 in the same change if package 6 has not yet landed, so that no release accepts the key without the
sentence about the lost key ring.

## The vectors and their independent check

The known answers of [storage.md](../spec/storage.md) R-STOR-076, R-STOR-077 and R-STOR-086 were computed for the specification on 2026-11-22
by a throwaway implementation written from the specification's text in Python 3.12 with the `cryptography` package 38.0.4, independent of any
code of this repository. Package 1 writes the vector files from its Go implementation, checks that they contain those answers unchanged,
checks every case once with `tools/blobvectors/check.py` (Python's `cryptography`, by hand, not in CI), and records here the date and the
outcome. *(Not done yet.)*

## Tests and their cost

R-STOR-091's table: about 2.9 s of Go unit tests, 0.5 s of chart tests, 10 s in the integration area `blob` (a rotation and the jobs), 8 s in
`backup` (one case), 3 s in `s3-conformance`, nothing in any end-to-end leg, and one nightly job of about 27 minutes for the five fuzz targets.
With 25 formats the cost is the same: no format sees a connection or a key.

| Fuzz target | Package | What it holds |
|---|---|---|
| `FuzzPlainSize` | `internal/blob/blobcrypt` | the inverse of `S` never panics and agrees with `S` |
| `FuzzOpenRecord` | `internal/blob/blobcrypt` | any metadata map yields a data key or one of the named errors |
| `FuzzReadObject` | `internal/blob/blobcrypt` | any body under a valid record yields the plaintext or a named error, and no byte of a segment that failed |
| `FuzzReadRange` | `internal/blob/blobcrypt` | any range over a vector object yields exactly its window or an error, and no byte that was not authenticated |
| `FuzzOpenTail` | `internal/blob/blobcrypt` | a staging tail object yields its plaintext or a named error |

Locally: `make fuzz-stored-format FUZZTIME=1m`; a failing input stays in `internal/blob/blobcrypt/testdata/fuzz`.

## Not verified

- Nothing of this stage was run against an object store. Whether the owner's in-cluster object gateway (Ceph 19.2.3, read from his cluster on
  2026-11-22) keeps user metadata on a write and replaces it on a copy is what the start check establishes there once the release runs; its
  documentation was not read. The provider he has in mind documents a copy with new metadata only through the documentation of the software
  behind it (research of 2026-11-22), not for itself.
- The throughput of encryption on Quader's hardware; the proxy's measurement (about 4.5 GB/s per core) is from an Apple M5 Pro.
