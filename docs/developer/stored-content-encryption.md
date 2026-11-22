# Encryption of stored content: the build plan

Status: specified on 2026-11-22; package 1, the format and its tools, built on 2026-11-22 ([below](#package-1-the-format-as-built)), nothing
calls it yet; packages 2 to 6 not built ([storage.md](../spec/storage.md#encryption-of-stored-content) R-STOR-048, R-STOR-075 to
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
`CopyDataKey` returns the `DataKey` that `Open` yields. Package 3 writes a part or a whole object with `NewSection` over a section of the
spool (the payload checksum is a first pass over the same `Section`, the SDK's retry a `Seek` to 0), makes the record with `Wrap` and
`Metadata`, checks a `HeadObject` with `Open`, reads with `Open` and `NewReader` or `PlanRange`, `Open` (the total as `ObjectSize`) and
`NewRangeReader`, counts a failure by `ReasonOf` and marks by `MarksCorrupt`; a session seals its data key's `Key.Bytes()` into its row,
writes tails with `NewTailSection` and reads them with `NewTailReader`, and names objects with `KeyedName`. Package 4's orphan sweep and
operator check run `Open` with `Want{Known: false}` and compare `KeyedName` of the unwrapped digest. Package 5 seals its cache with the same
writer and readers. Package 6's offline tool reads a record file (the object's metadata as JSON, the form of the vectors' `.record.json`).

A failure of the body is an `*Error` with its reason; a body that ends early is a transport error with `io.ErrUnexpectedEOF` in its chain and
no reason, which marks nothing; a body longer than the object is `size`. A failed key lookup passes through as it is.

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
