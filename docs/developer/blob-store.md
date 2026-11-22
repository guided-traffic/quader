# Blob store

Content-addressed storage on one to eight S3-compatible object-store connections: [internal/blob](../../internal/blob/) (everything that
does not depend on an S3 client), [internal/blob/s3](../../internal/blob/s3/) (the AWS SDK adapter, the only package that imports
the SDK), [internal/blob/blobtest](../../internal/blob/blobtest/) (an in-memory store with fault injection for tests).
Requirements: R-STOR-001 to R-STOR-006, R-STOR-013, R-STOR-024, R-STOR-025, R-STOR-030 to R-STOR-035, R-STOR-046, R-STOR-050 to
R-STOR-053, R-STOR-055, R-STOR-056, R-STOR-060, R-STOR-062.
The row is the authority on whether a blob exists; a row per copy (`blob_copies`) says on which connection its object lies, under which
name and size, and what is known about it; the object is the content. Nothing in this tree uses Valkey or a
session-level advisory lock (R-STOR-046, enforced by [internal/archtest](../../internal/archtest/)). The first caller of `Ingest`,
`Commit` and `Open` is the files transfer ([files.md](files.md)): its link function is the item insert or replace, and a download opens
the verifying reader or a range.

## Files

| File | Responsibility |
|---|---|
| [key.go](../../internal/blob/key.go) | `Digest`, `Key(prefix, digest)`, `ParseKey`, `Digests` (the four digests and the size) |
| [objects.go](../../internal/blob/objects.go) | `ObjectStore` interface (with `Metadata` on `Put`, `CreateMultipart`, `Copy`, `Head`, `Get`), `BucketCopier` (a copy from another bucket of one endpoint), `StoreError` and its kinds, `ErrNotFound`, `ErrCircuitOpen` |
| [connections.go](../../internal/blob/connections.go) | `Connection` (one element: its client, prefix, naming function `ObjectKey`, `Drain`, `Encrypted`, `Capabilities`, `Verifies`, `Read`, guarded `Presign`, `LinkCheck`), `Connections` (the ordered set: `List`, `Get`, `First`, `Targets`, `Writable`, the read order) |
| [copies.go](../../internal/blob/copies.go) | `Copy`, the copy and content states, `DeriveContentStatus`, `Store.Copies`, `ParseCopies` (the copies of an item lookup), `Store.Servable`, the marks of a copy (`markCopy`, `copyBack`), the daily stamp, the copy and queue rows of a commit (`recordWrites`) |
| [record.go](../../internal/blob/record.go) | `Store.RecordConnections` (first sight, fixed properties, retired ids, the bind of the migration's row), `RecordCapabilities`, `HoldsCopies`, `ForgetConnection`, `Refusal` |
| [startcheck.go](../../internal/blob/startcheck.go) | `CheckConnection` (the start check of one connection with the marker and the capabilities), `AwaitConnections` (the first readiness of R-STOR-060), `MarkerKey` |
| [spool.go](../../internal/blob/spool.go) | `Spool`: admission, the budget, spool files, the single hashing pass, cleanup |
| [store.go](../../internal/blob/store.go), [commit.go](../../internal/blob/commit.go) | `Store`, `Ingest`, `Commit` (the commit protocol), `Lookup` |
| [transfer.go](../../internal/blob/transfer.go) | single `PutObject` or multipart from the spool file, part sizing, abort |
| [staged.go](../../internal/blob/staged.go), [spool_segment.go](../../internal/blob/spool_segment.go) | the staging area of the upload sessions: `SessionKey`, `Staged` (a blob that is in the bucket already, committed by a server-side copy), `Spool.IngestSegment` (one part of a request, behind the recorded tail, through the session's hash functions) |
| [gc_uploads.go](../../internal/blob/gc_uploads.go) | the session step of a collection run: expiry, `RemoveSession`, the sweep of `_uploads/` |
| [download.go](../../internal/blob/download.go) | `Store.Open` over the copies and the verifying reader (hold-back, the copy's stored size), the mark of a copy a connection does not have (`copyGone`) and the recheck of a missing blob (`RecheckMissing`) |
| [guard.go](../../internal/blob/guard.go) | `Guard`: circuit breaker and request metrics with the label `connection` around any `ObjectStore`, one per connection; `Circuit`, the guarded `Presign` and `CopyFrom` |
| [gc.go](../../internal/blob/gc.go), [gcjob.go](../../internal/blob/gcjob.go) | `Collector` (per-blob protocol, hand-over, pause, hold, fault hook) and the job adapter |
| [usagefold.go](../../internal/blob/usagefold.go) | `UsageFolder`: the fold loop of the usage counters (R-STOR-003) and its `Fold` iteration |
| [conformance.go](../../internal/blob/conformance.go) | startup check, conformance checks (with the capability checks), `AwaitStartupCheck` |
| [sql.go](../../internal/blob/sql.go), [tables.go](../../internal/blob/tables.go), [metrics.go](../../internal/blob/metrics.go), [errors.go](../../internal/blob/errors.go) | the `DB` interface, the `Rows` seam (`WithRows`) and its sqlc implementation, export-class registration, metric families, `errs` mapping |
| [restoreversions/restoreversions.go](../../internal/blob/restoreversions/restoreversions.go), [s3/versions.go](../../internal/blob/s3/versions.go) | `quader admin storage restore-versions` (R-HA-025 step 5, [backup.md](backup.md#the-platform-layer-and-the-point-in-time-drill)): runs `verify`, lists the versions of each key without an object (`Client.ListVersions`), removes the delete markers younger than *t* (`Client.DeleteVersion`); never an object version |
| [verify/verify.go](../../internal/blob/verify/verify.go), [verify/pg.go](../../internal/blob/verify/pg.go) | `quader admin storage verify` ([below](#storage-verify-r-stor-027-q-stor-200)): `Run` over a `Source` (the pages and the items) and an `Objects` (`Head`), `PGSource` |
| [s3/client.go](../../internal/blob/s3/client.go), [s3/copy.go](../../internal/blob/s3/copy.go) | `s3.Client` (the `ObjectStore` and the `BucketCopier`: `Location`, `CopyFrom`), `NewFromFactory` |
| [reconcile/](../../internal/blob/reconcile/), [reconcile_seams.go](../../internal/blob/reconcile_seams.go) | the reconciliation between the connections ([below](#the-reconciliation-r-stor-056)); the seams it needs of the store: `Store.MarkCopyDamaged` and `Store.SetGapSignal` |
| [blobtest/endpoint.go](../../internal/blob/blobtest/endpoint.go) | `blobtest.NewEndpoint`: in-memory stores of the buckets of one endpoint, which copy from each other (`CopyFrom`) |
| [s3/credentials.go](../../internal/blob/s3/credentials.go) | credential sources of R-STOR-031, SDK configuration |
| [s3/errors.go](../../internal/blob/s3/errors.go), [s3/idle.go](../../internal/blob/s3/idle.go) | SDK error classification; stall detection |

## Key layout and digests (R-STOR-001, R-STOR-002)

`blob.Key("quader", d)` is `quader/sha256/<h0h1>/<h2h3>/<hex>`; with an empty prefix the key starts at `sha256/`. The
function takes a 32-byte digest and nothing else, so no repository, path or user input reaches a key. `ParseKey` is its
inverse and refuses every key that does not follow the layout exactly (the orphan sweep will report those as foreign).

`Spool.Ingest` computes SHA-256, SHA-1, MD5 and SHA-512 and the size in one pass over the body (one `Read` loop fanning
out to four hash states and the spool file). SHA-512 is computed for every upload (Q-STOR-2). SHA-256 is the identity;
SHA-1 and MD5 are stored for client compatibility only.

## Upload pipeline (R-STOR-004)

```text
Store.Ingest(body, IngestOptions) ─▶ *Spooled ─▶ Store.Commit(spooled, link) ─▶ spooled.Close()
```

- Admission happens before the first byte is read: a declared length above `min(IngestOptions.MaxSize,
  storage.upload.max_size)` is `413` (`ErrTooLarge`), no budget or too little free space on the spool volume is `503`
  with `Retry-After` (`ErrSpoolFull`). A body of unknown length reserves budget in 4 MiB steps (just what it needs when
  a step does not fit) and fails `413` past the maximum or `503` when the next step cannot be reserved.
- Bodies of known length up to `storage.spool.memory_threshold` stay in memory and use no budget; every other body goes
  to a new file, mode `0600`, named by a random UUID, in `storage.spool.dir`.
- After the body: byte count against `Content-Length` (`400`, `ErrLengthMismatch`), declared digests (`409`,
  `ErrDigestMismatch`, `Expected` carries SHA-256, SHA-512, SHA-1, MD5).
- `NewSpool` deletes only what the spool creates (regular files named by a v4 UUID) at start and refuses to start if
  the directory holds anything else (the error names the entry; `lost+found` of a fresh volume is tolerated), so a
  misconfigured `storage.spool.dir` cannot wipe a data directory. `Spooled.Close` removes the file and returns the
  budget; it is idempotent, and `Ingest` closes its own result on every error path. The caller closes after `Commit`.
- The free-space probe is `statfs` (`spool_unix.go`); on a platform without it the `min_free_bytes` check is skipped.

All errors are `*errs.Error` values with the sentinel as cause (`errors.Is(err, blob.ErrSpoolFull)`).

## Commit protocol (R-STOR-005)

`Store.Commit(ctx, spooled, link)` runs the loop of the specification; `link` runs in the transaction that holds the
row's `FOR SHARE` lock and adds the reference (item insert, hooks). `Store.CommitAll(ctx, []*Spooled, link)` is the same loop over several blobs
and one `link` (a request that writes more than one file, [items.md](items.md)): blobs of one content are one blob, the rows are locked and inserted in digest
order (two commits over overlapping sets never wait on each other's insert in opposite orders), a round in which any blob needs a write, a repair or a wait is rolled back and
the next round starts after all of them were served, and a failure queues every digest that was written. `Commit` is `CommitAll` of one. The statements are sqlc queries in
[queries/blob.sql](../../internal/meta/postgres/queries/blob.sql), run on the transaction behind `meta.Tx`
(`postgres.Unwrap` + `q.New`, [sql.go](../../internal/blob/sql.go)); `meta.Tx` itself cannot run SQL. `Store` needs
only `DB.InTx(ctx, func(meta.Tx) error)`, which `*postgres.DB` has. The commit and download paths reach the table
through the small `Rows` interface so that the unit tests can simulate it (`WithRows` in `export_test.go`); the
collector has no such seam.

The row is inserted for an object no row announced only within `min(commit_deadline, delete_settle / 2)` of the end
of the object write. Longer ago, a collector may have deleted the key and dropped a row we never saw, so the
object is written again. `delete_settle` therefore has a minimum of 1 s.

| Row state seen | Action |
|---|---|
| no row, object not written (or written longer than `commit_deadline` ago) | roll back, write the object to the connections, loop |
| no row, object written | `INSERT … ON CONFLICT DO NOTHING`, then a copy row per object written and a queue row per other connection without drain (`recordWrites`); 0 rows means a concurrent winner, roll back and loop |
| `deleting` | roll back, forget the writes, poll until the row is gone (`blob_wait_timeout`, else `503`), loop |
| `live`, `content_status <> 'ok'` | roll back, write the object to the connections, record their copies and let the content state follow (`repair`), loop |
| `live` | run `link`, commit (a deduplication hit when this call wrote nothing; when it wrote within the insert bound, its copy rows too) |

**The object write over the connections** (R-STOR-053, `writeObjects` in [transfer.go](../../internal/blob/transfer.go)): the content goes to
the first `storage.replication.min_copies` writable connections (`Connections.Writable`: no `drain`, the circuit closed on this replica) in
parallel, each with its own payload checksum. A write that fails with a transient error or an open circuit is started on the next writable
connection while the spool holds the bytes; fewer than `min_copies` successful writes are the `503` of R-STOR-036, and what was written is
left without a copy row for the orphan sweep. A copy row is `present`, with `stored_size` the bytes written, and `verified_at` now when the
connection enforces the payload checksum (`Connection.Verifies`, the start check's capability). The upsert of the copy row overwrites a
`missing` or `corrupt` row and leaves a `deleting` one ([Q-STOR-253](../spec/storage.md#q-stor-253--a-commits-write-overwrites-a-missing-or-corrupt-copy-row)).
The content state of a blob follows from its copies (`DeriveContentStatus`); the statements that change a copy lock the blob row `FOR NO
KEY UPDATE` first, so two marks of one blob, and a mark and a commit, never derive from each other's stale view.

SQLSTATE `AS001` from `link` (the guard trigger saw a row that is not live) restarts the loop. After
`storage.upload.commit_attempts` rounds the error is `503` (`ErrCommitAttempts`). A failure after the object write
queues the digest in `blob_gc_queue`, so the incremental collection finds the orphan. `CommitResult` says whether this
call wrote the object, inserted the row, or deduplicated.

## Transfer (R-STOR-006)

Per connection, up to `storage.object_store.multipart.threshold`: one `PutObject`. Above it, Quader's own multipart upload (Q-ARCH-4):
part size is `part_size`, raised so the content fits 10,000 parts (whole MiB, `blob.PartSize`); each part is an
`io.SectionReader` on the spool file; `multipart.concurrency` parts per upload, `multipart.max_concurrent_parts` per
`Store` over all its connections. Any failure or cancellation aborts the upload with a context of its own. After
`CompleteMultipartUpload` the object size is checked with `HeadObject`. The payload checksum is the connection's `payload_checksum`: for a
single request SHA-256 or MD5 of the whole content (known from the hashing pass), for parts the digest of the part (computed by one
extra read of the part from the spool file) and `ChecksumAlgorithm: SHA256` on `CreateMultipartUpload`. Stage 1 writes no metadata.

## Object-store client (R-STOR-030 to R-STOR-033)

The configuration is a list `storage.connections` of flat elements and the shared block `storage.object_store`
([storage.go](../../internal/config/storage.go), [storage_connections.go](../../internal/config/storage_connections.go)): the loader
validates what the configuration says (R-STOR-050), `config.ResolveConnections` reads the files of a provisioner's Secret
(`bucket_file`, `region_file`, `endpoint_file`) and checks the location rule again where the clients are built
([Q-STOR-251](../spec/storage.md#q-stor-251--the-files-of-a-connections-location-are-read-when-the-clients-are-built-not-when-the-configuration-is-loaded)),
and `config.ClientConfig(position, element, objectStore)` is the `config.StorageS3` of one connection's client, the type the backup target and the
Bastion's bucket use as well. The operator commands and the backup reach the first connection (`Storage.FirstClient`) until packages D and
E teach them the others. An error that names a setting of a connection has the form of the validation,
`storage.connections[<n>] (<id>): <key>` (`StorageS3.Names`, filled by `ClientConfig`, and `Connection.Element` in the blob store);
the backup target's errors name `backup.target.<key>` and the Bastion's `bastion.cache.bucket.<key>`.

`s3.NewFromFactory(ctx, config.ClientConfig(i, el, cfg.ObjectStore), outboundFactory)` takes its HTTP client from `internal/outbound` (purpose
`objectstore`) with the connect and response-header timeouts and the proxy of `storage.object_store.http`. Retries are the SDK's
standard retryer (`retry.max_attempts`, `retry.max_backoff`). A transfer in which no byte moves for
`http.idle_transfer_timeout` is cancelled (`idle.go`). Credentials (the connection's `credentials_source`):

| Source | Behavior | Tested |
|---|---|---|
| `static` | key files read again at most once per second; a rotated Secret applies without restart | unit (rotation against a fake endpoint), integration |
| `webIdentity` | `stscreds` web identity provider at `sts_endpoint` | unit against a fake STS; not against a real STS |
| `default` | `config.LoadDefaultConfig` (environment, shared files, IRSA, Pod Identity, instance metadata) | not tested here |

`role_arn` with `default` or `static` assumes the role on top; that path is not tested. Errors are classified in
`s3/errors.go`: a missing key is `ErrNotFound`; `AccessDenied`, `NoSuchBucket`, bad credentials are `KindConfig`;
5xx, throttling, no answer and credential retrieval failures are `KindTransient`; other 4xx are `KindOther`.

`blob.NewGuard(store, cfg.ObjectStore.CircuitBreaker, blob.WithGuardConnection(id), …)` wraps any `ObjectStore`, one per connection:
`failure_threshold` consecutive transient failures open the circuit for `open_duration`, then one probe request goes through. Config
errors, 404 and other 4xx are answers, not failures; a cancelled caller is no evidence. One connection's open circuit changes nothing on
another's. Metrics, each with the label `connection`: `quader_storage_s3_requests_total{connection,operation,outcome}`,
`quader_storage_s3_request_duration_seconds`, `quader_storage_s3_bytes_total{connection,direction}`,
`quader_storage_s3_circuit_open{connection}` (0 from the guard's construction); the reads add `quader_storage_copies_damaged_total{connection,status}`
and `quader_storage_reads_fallen_over_total{connection,reason}` (`not_found`, `unavailable`, `size`,
[Q-STOR-254](../spec/storage.md#q-stor-254--the-names-the-start-check-and-the-reads-give-their-findings)). The guard's `Presign` refuses while
the circuit is open (R-STOR-062), and `CopyFrom` passes the breaker. The composition root builds `blob.NewMetrics(m)` once and passes it to
the spool, the guards and the collector.

Endpoint: a connection's `endpoint` without a scheme is a host and means `https://` (Q-STOR-15). `config.NormalizeEndpoint` does it once at
load (`Config.Finalize`, the source of `storage.connections` becomes `derived`) and again in `s3.New`, so a client built from a hand-made
config agrees. Plaintext is always spelled `http://` with `allow_insecure_endpoint`; the flag never changes a scheme-less host. A link is
signed against the connection's own endpoint; the public address a link names is stage 3's (R-STOR-074).

TLS: an `https://` endpoint verifies against the factory's trust store (system roots plus
`proxy.outbound.tls.ca_files`), or, with the connection's `ca_file` set, against that bundle only: `NewFromFactory` builds a
trust store of its own (`outbound.Factory.TrustStore` + `outbound.WithRootCAs`), read again like the factory's, so a
rotated CA applies to the next connection without a restart (R-ARCH-035). A `ca_file` that does not load at start is a
configuration error naming the key. `Client.HTTPClient()` is the client the store is reached with; a caller that
fetches a presigned URL itself (the conformance check) uses it, so the fetch trusts the same CA.

Gaps against the specification, all visible at construction (nothing is silently ignored):

- `storage.object_store.http.proxy_credentials_file`: `NewFromFactory` refuses it. `outbound.WithProxyURL` sends only the user information of the proxy URL, which the configuration forbids (R-OPS-013: URL keys carry no credentials), and a file read once would not follow a rotation. Use a proxy that authenticates by source address until the outbound layer takes a re-read proxy credential.
- `http.max_connections`, `tls_handshake_timeout`, `idle_conn_timeout`: the outbound factory has
  no options for them, so its defaults apply (`proxy_url` and `no_proxy` as host patterns are passed through; CIDR
  patterns in `no_proxy` are not).
- `quader_storage_s3_requests_total` counts calls of the `ObjectStore` interface, not the SDK's retry attempts.

## Download verification (R-STOR-013, Q-STOR-6)

`Store.Open(ctx, digest, size, OpenOptions)` takes the blob's copies from `OpenOptions.Copies` when the caller read them
(`CopiesRead`), else reads them (`Store.Copies`, one indexed lookup). A download reads them with its item lookup: `GetLiveItem`
([files.sql](../../internal/meta/postgres/queries/files.sql)) aggregates the copy rows of the item's blob into the JSON column
`blob_copies`, `items.Item` carries them parsed (`blob.ParseCopies`), `items.Store.Open` hands them on, and `items.Store.Usable` drops them
after a recheck that set a copy present, so every route that reads its item with the lookup costs that one statement (R-ARCH-027,
Q-STOR-275). The NuGet view's ranged reader reads them once for all its ranges. `Open` returns a reader of the first copy that
answers (R-STOR-055): the `present` copies on connections this replica configures, in list order, a draining connection's last, a
connection with an open circuit left out. A copy is left for the next before the first byte when its connection answers "no such object",
fails transiently or answers with a size other than the copy's `stored_size`; after the first byte a failure aborts the response. With no
copy to take, the read is `ErrNotFound` (no present copy on a configured connection, which `items.Store.Open` answers `503
content_unavailable`), the store's `503` (the copies' connections failed), or `ErrCorrupt` (the last copy had another size). A full read is
verified unless `storage.download.verify` is false and the caller did not set `OpenOptions.Verify` (the UI's shared asset store sets it,
Q-UI-143): SHA-256 is computed while reading and the last `hold_back` bytes are not released before the body ended and the digest
matched. On a mismatch the reader returns `ErrCorrupt` instead of the held bytes (the HTTP layer aborts the response) and marks that copy
`corrupt`. A match stamps the copy's `verified_at` at most once a day (`VerifiedStampInterval`, Q-STOR-233). Only evidence about an object
marks its copy; the blob's content state follows from its copies, so a copy's damage changes the blob only when no other copy is present.
A transport error aborts the read but changes no state. `WithDamageHook` is where the audit event hangs; the composition root binds none
yet, so no detection writes one. A range is returned as stored (H-11).

A connection that answers "no such object" (`ErrNotFound`), for a full or a ranged read, marks that copy `missing` and queues its repair
(`copyGone`, `markCopy`, R-HA-024, [Q-STOR-151](../spec/storage.md#q-stor-151--a-read-that-finds-no-object-marks-the-blob-missing-and-looks-once-more-after-the-mark)):
`SetBlobCopyState` changes only a `present` copy, the change is counted (`quader_storage_copies_damaged_total`), and the blob turns
`missing` (counted by `quader_storage_blobs_damaged_total{status="missing"}`, the alert `QuaderBlobMissing`) only when no other copy is
present. Then the object is looked for once more on the same connection (`HeadObject`): a repair that wrote it again between the read and
the mark would otherwise be undone, so a copy whose object is there again with its stored size is `present` again. A read of a blob
without a present copy is `ErrNotFound`; `items.Store.Open` answers it `503 content_unavailable`, and a remote repository fills the item again
([proxy.md](proxy.md#lost-content-r-stor-027-q-repo-202-to-q-repo-204)). The mark is cleared by the commit protocol's repair (the next
commit of the same content writes the object, table above), and it heals by itself
([Q-STOR-153](../spec/storage.md#q-stor-153--a-missing-mark-heals-by-itself-a-read-asks-the-store-again-once-per-blob-and-30-s)):
`Store.RecheckMissing(digest, size)`, which every read of a blob marked `missing` calls through `items.Store.Usable` (`/files/`, the format
host's `Serve`, the remote engine), claims the one look of `MissingRecheckInterval` (30 s) with `ClaimMissingRecheck`, a conditional `UPDATE`
of the row's `checked_at` that moves it only when it is that old, so one read per blob and interval asks whatever the number of replicas.
The claimer sends one `HeadObject` per `missing` copy on a reachable connection of this replica; an object of the copy's stored size sets
that copy `present` and the blob `ok`, and the read serves it; anything else leaves the mark, and the moved `checked_at` starts the
interval again. The statement that turns a blob away from `ok` (`SetBlobContentStatus`) sets `checked_at` too, so the interval starts at the
detection. `blobs.checked_at` keeps this one meaning; the scrub orders copies by their own `verified_at` (R-STOR-052).

What a download holds ([Q-ARCH-241](../spec/architecture.md#q-arch-241--a-download-holds-one-buffer-of-its-own-size-and-the-object-store-sends-no-more-than-one-window-ahead)):

- The verifying reader has one buffer (`verifyBuffer`): `buffer_size`, at least `2 × hold_back` and 4 KiB, at most the object's
  size plus one byte (the byte that would show a longer object). It is taken on the first read from `Buffer`, pools of
  power-of-two classes from 4 KiB to 1 GiB ([buffers.go](../../internal/blob/buffers.go)), and given back by `Close`; a read after
  `Close` fails.
- `fill` moves the withheld tail to the front and reads until the buffer is full or the body ended; everything but the last
  `hold_back` bytes is then cleared. With the minimum of twice the hold-back, a refill clears at least as much as it moves. A body
  longer than the row has nothing more cleared and is read to its end, where the digest check fails.
- It implements `io.WriterTo`, and `edge.ServeItem` uses it, so a verified download needs no copy buffer. Any other body (a range,
  `verify: false`) is copied through a pooled buffer of at most `buffer_size` and its length.
- `blob.DownloadBufferBytes` is the class of the full buffer, the unit of the memory guard's request bytes;
  `blob.DownloadCost(cfg, size)` is what a download of that size holds (its buffer class plus `min(size, receive_window)`), which
  `Store.Open` hands to the request's memory charge through `blob.WithMemory` before it asks the object store
  ([architecture.md](architecture.md#stage-4-and-the-memory-guard), Q-ARCH-328).
- The object-store client's HTTP/2 receive window per stream is `storage.object_store.http.receive_window` (`outbound.WithStreamReceiveWindow`);
  without it Go's transport lets the store send 4 MiB of an object ahead of a slow reader.

## Staged blobs and the upload sessions (R-STOR-011)

The service that holds the sessions is [internal/uploads](../../internal/uploads/) (see [oci.md](oci.md#upload-sessions-r-stor-011)); this package
gives it three things. `ObjectStore.Copy(src, dst, size, md)` is a server-side copy (`CopyObject` up to 5 GiB, `UploadPartCopy` parts of
1 GiB above, the encryption of every write; `md` nil keeps the source's metadata, carried over by hand on the multipart copy;
[s3/copy.go](../../internal/blob/s3/copy.go)). `blob.Staged(conn, digests, key)` returns a `*Spooled` whose content is the object at `key`
on the connection `conn`: `Commit` and `CommitAll` take it like any spooled body, and the only difference is the protocol's object write
(`putStaged`: the copy to the content address on that connection, then a size check; its copy row is `present` and not verified, so the
scrub reads it first), which a deduplication hit never runs. With `min_copies` above 1 the content object is then streamed through the
spool, hashed against the session's digests, to the next writable connections. If the staging object is gone the commit fails with
`ErrStagingGone`. A staged body has no reader (its `Reader` fails) and no spool budget. Until package B pins each session
(R-STOR-054), the service starts every session on the first connection without drain (`uploads.Options.Connection`).
`Spool.IngestSegment(ctx, prefix, body, n, tee)` reads at most `n` bytes into a `*Spooled` (memory below the spool's threshold, a spool file
above it, its budget reserved as the bytes arrive), the bytes of `prefix` first (the recorded tail, which the session's hash state has seen
already) and then `body`, each byte of which goes through `tee` before it is kept; the result carries the segment's SHA-256 and MD5, the
payload checksums of a part.

## Garbage collection (R-STOR-024, R-STOR-025)

`blob.NewCollector(cfg.GC, prefix, objects, db, blob.WithBatchDelete(conn.BatchDelete()))`, built on the first connection without drain
(collection per copy is package B's, R-STOR-057; until then it deletes there only, and the drop of a blob row in step 6 takes its copy and
queue rows, `DropBlobRows`); `Run(ctx, RunOptions{Mode, DryRun, Cursor, Checkpoint})` returns a
`GCResult` (`dryRun, trashPurged, candidates, kept, held, deleted, bytesReclaimed, errors, paused`, and the counters of the
session step: `uploadSessionsAborted, uploadSessionsCleaned, uploadObjectsDeleted, uploadsAborted`). A run:

0. with `WithUploadSessions(cfg.Uploads)` set, runs the session step ([gc_uploads.go](../../internal/blob/gc_uploads.go)): it aborts the
   sessions that are idle for `session_ttl` or (open ones) older than `session_max_age`, removes the multipart upload and the objects of the
   aborted ones and, once their idle time has passed, of the finished ones, deletes their rows, and in a full sweep removes what no session row
   stands for below `<prefix>/_uploads/` ([Q-STOR-170](../spec/storage.md#q-stor-170--the-collection-run-aborts-a-live-session-for-its-idle-time-and-an-open-one-for-its-age),
   [Q-STOR-172](../spec/storage.md#q-stor-172--the-full-collection-sweep-removes-what-no-session-stands-for-until-the-orphan-sweep-is-a-task-of-its-own)).
   The step takes at most 25 batches of 200 per run, lists the staging area in pages (the optional `MultipartPager` of the store, 500 uploads at a time), re-reads the deletion pause before every batch, keeps the position of the staging sweep in `system_state` (`upload_sweep_cursor`) so that successive runs progress, and counts the sessions it expires in `quader_storage_upload_sessions_total{outcome="expired"}` ([Q-STOR-173](../spec/storage.md#q-stor-173--the-sessions-are-counted-by-quader_storage_upload_sessions_total-not-by-an-oci-metric)).
   A failure of the step is counted in `errors` and logged; it does not stop the blob collection below it. A dry run counts the sessions it
   would abort and changes nothing;
1. reads the deletion pause (`system_state` key `deletions_paused_at`; see below) and ends with `paused: true` if set;
2. finishes `deleting` rows left by earlier runs: step (6) once `delete_settle` has passed since the delete, steps
   (4) to (6) for a row nobody finished within `handover_delay`. Such a row is taken over with a compare-and-set
   (`UPDATE … SET state_changed_at = now() … RETURNING`), so of two runs that read it one takes it and the other
   finds it fresh; the winner deletes under the same hand-over deadline as for its own claims;
3. reads candidates in keyset batches of `batch_size` (incremental: `blob_gc_queue` entries older than `grace_period`;
   full: live blobs older than the grace period, anti-joined against every table with a foreign key to `blobs`, read
   from `pg_constraint` once per batch, resuming after `Cursor`; a table added by a migration during one batch is
   seen from the next batch on) and runs the per-blob protocol, one transaction each: `FOR UPDATE`,
   then a separate reference-check statement, then `UPDATE … 'deleting'` and the queue delete guarded by `$seen`;
4. deletes the claimed objects (`DeleteObjects` when `batch_delete`, else `delete_concurrency` parallel `DeleteObject`)
   only while less than `handover_delay − delete_settle` has passed on the collector's own clock since the reading
   before the earliest claim, with that instant as the call's deadline; records `object_deleted_at`;
5. waits up to `delete_settle` (plus two polls) to drop the rows it deleted; the wait ends with a pass that started after its deadline
   ([Q-STOR-216](../spec/storage.md#q-stor-216--the-settle-wait-of-a-collection-run-ends-with-a-pass-that-started-after-its-deadline)).
   When `RunOptions.Draining` closes (the job's `Run.Draining()`, the replica's shutdown), the wait ends with `blob.ErrHandedOver`
   after its current pass; the job handler in [wire_jobs.go](../../internal/app/wire_jobs.go) turns that into `jobs.ErrReleased`, so
   the job is released at once and the run that continues on another worker drops the rows
   ([Q-STOR-217](../spec/storage.md#q-stor-217--a-collection-run-in-its-settle-wait-hands-the-wait-over-when-its-replica-begins-to-stop)).

On cancellation (a drain) the run cancels its S3 calls, waits for them, records the deletes that succeeded and sets
`state_changed_at = now() − (handover_delay − delete_settle)` on its other claims, so the run that continues finishes
them after `delete_settle`. A crash needs none of this: the rows wait for `handover_delay`.

Extension points on `Collector`: `Hold` (the backup hold of step 2b, runs in the claim transaction; nil holds nothing),
`Paused` (replaceable; the default reads the key-value row `system_state.deletions_paused_at` because `system_state`
in migration 00001 is key-value, not the column the sketch in R-HA-025 shows) and `Fault` (a test hook called at named
points, `claim:before|locked|checked|committed`, `delete:before|after`, `record:after`, `finish:delete`, `drop:before`, `settle:counted`;
a non-nil error ends the run at once as a process kill would, with `errors.Is(err, blob.ErrFault)`).

Not in this package yet: the trash purge that R-STOR-025 puts at the start of a run (its package does not exist; the job handler calls
it before `Run` and puts the count in `trashPurged`), the orphan sweep of `sha256/` (R-STOR-026; its pass over `_uploads/` is the
session step) and the scrub (R-STOR-027).

### Job registration

[gcjob.go](../../internal/blob/gcjob.go) holds `Collector.RunJob(ctx, mode, args, run)`, the body of the handlers; it
takes the narrow `JobRun` interface (`*jobs.Run` implements it) so that this infrastructure package does not import the
domain package `internal/jobs`. The kinds and tasks are registered where both are visible, as
`jobs.RegisterCoordinationKinds` does for the cluster: the comment at the top of gcjob.go shows the four
registrations (`gc` and `gc-full`, queue `maintenance`, exclusive, both tasks with dedup key `gc`).

## Usage fold (R-STOR-003)

The statement-level triggers of migration 00003 append to `storage_usage_deltas`; nothing else writes `repository_usage` or
`storage_usage_totals`. [usagefold.go](../../internal/blob/usagefold.go) is the loop that folds them, built in `wireBlob`
([wire_blob.go](../../internal/app/wire_blob.go)) on every role that runs job loops (`worker`, `all`) and started as a component:

- `Fold(ctx)` is one iteration and one `InTx` on the main pool: `FoldTakeUsageDeltas` (`DELETE ... WHERE id IN (SELECT ... ORDER BY id
  LIMIT 10000 FOR UPDATE SKIP LOCKED) RETURNING`, grouped by repository, `ORDER BY repo_id NULLS LAST`), `FoldUpsertRepositoryUsage`
  (one statement over the groups in that order, joined with `repositories ... FOR KEY SHARE`, so the deltas of a deleted repository
  are dropped and the repository cannot go before the insert), then `FoldAddPhysicalUsage` for the group without a repository, last.
  The statements are in [usage.sql](../../internal/meta/postgres/queries/usage.sql). `repository_usage.updated_at` is the transaction's
  `now()`: the mirror's byte bound compares its own stores with it, and a transaction start is never later than the snapshot that
  took the rows, so the bound over-counts at worst and never under-counts ([bounds.go](../../internal/mirror/bounds.go)).
- The loop runs `Fold`, runs it again at once while it took `UsageFoldBatch` (10,000) rows, and otherwise waits
  `storage.usage.fold_interval`. A failed iteration is logged at warn level and repeated at the next interval. There is no lease and
  no Valkey lock: concurrent folds of any number of replicas take disjoint rows and lock the counter rows in one order. A deadlock
  would be retried by `InTx` and not show in the loop, which is why the integration test reads `pg_stat_database.deadlocks`.
- Drain: `Drain` stops new iterations, `Settle` waits for the one in flight, `Quiesce` (5 s before the deadline) cancels it and its
  transaction rolls back; its rows are folded by another replica. The loop is watched as `blob.usage_fold`, each iteration has the
  call deadline of `cluster.CallTimeout`, and the wait beats so a long interval is no stuck loop.
- No metric: the specification names none. A failure shows in the log; the backlog is `SELECT count(*) FROM storage_usage_deltas`.
- Readers that must be exact add the unfolded deltas to the folded rows in one statement (`pgEnv.exact` in
  [usagefold_test.go](../../test/integration/blob/usagefold_test.go) is the pattern); quota admission and the storage summary read
  the folded rows and lag by one interval plus one iteration. `storage_usage_recount()` of R-STOR-047 is not built.

### The figure of a project (R-STOR-049, Q-STOR-158)

A project keeps no counter of its own. `ProjectList` and `ProjectGet` ([project.sql](../../internal/meta/postgres/queries/project.sql))
add a column `logical_bytes`: `sum(repository_usage.logical_bytes)` over the repositories whose `project_id` is the project, a join
by primary key, so no item is scanned and a repository that moves ([move.go](../../internal/project/move.go)) takes its share with
it, with no bookkeeping. `project.Project.LogicalBytes` carries it and `toProject` in
[projects_api.go](../../internal/api/v1/projects_api.go) serves it as `logicalBytes`; it lags a write like the repository counters do.
It counts untrashed rows; when the trash can is built, the trashed bytes need a counter of their own in the deltas and in
`repository_usage` (Q-STOR-158 names the condition). The physical figure of the dashboard is `storage_usage_totals`
(`OverviewStorage`), unrelated to this sum. The integration test is
[storage_test.go](../../test/integration/projects/storage_test.go): it seeds blobs and items, folds with `blob.UsageFolder` and reads
the figure over HTTP.

## Storage verify (R-STOR-027, Q-STOR-200)

`quader admin storage verify` ([cmd/quader/admin_storage.go](../../cmd/quader/admin_storage.go)) checks that the object of every `live` blob row is in the store.
It is a command of the binary like `admin backup check`: it loads the configuration without a role, opens the database in the `web` role and the S3 client
of the first connection (the runtime credential, so it needs no permission the replicas lack), and never opens Valkey, so it runs in any pod of the installation
(`kubectl exec deploy/quader-worker -- quader admin storage verify`) and, with the replicas held not ready, in a one-off pod of the release's configuration.

| Part | Responsibility |
|---|---|
| `verify.Run` | Pages through `blobs` by digest (`VerifyBlobPage`, 1,000 rows, keyset on `sha256`), so memory is one page and the findings; runs up to `--concurrency` `Head` calls per page; one `Head` per row, three attempts with a 1 s and 2 s back-off for an error that is not "no such object"; a `KindConfig` error (`AccessDenied`) ends the run |
| `verify.PGSource` | The two readers of the run (`db.Read`, one short read each, no transaction held over the slow checks): the page and the items of a damaged blob (`VerifyBlobItems`, `VerifyBlobItemCount`, [blob.sql](../../internal/meta/postgres/queries/blob.sql)) |
| `verify.Report` | `Checked`, `Missing`, `SizeMismatch`, `Findings` (with up to `--max-items` items each, trashed ones marked), `Unchecked`, `ItemsAffected`; `--json` prints it whole |
| `writeVerifyReport` | The text: one line per item of a damaged blob, a line for a damaged blob without an item, one per blob that was not checked, the totals last (the line the e2e tests read) |

It writes nothing: no `content_status`, no audit event. Exit codes follow [R-BAK-013](../spec/backup-and-restore.md#r-bak-013--exit-codes-interruption-and-resumption):
`5` for a finding, `75` when only unchecked blobs remain, `2` for usage and configuration, `1` otherwise. A caller in Go (the post-restore step of R-HA-025,
which prints a count from) calls `verify.Run` and reads `Report.Missing` and `Report.SizeMismatch`. Tests: the unit tests in
[verify_test.go](../../internal/blob/verify/verify_test.go) (pages, findings, retries, the cut-off) and [test/integration/storageverify](../../test/integration/storageverify/verify_test.go)
(real PostgreSQL and S3: objects deleted behind the installation's back, a trashed item, `--since`); the end-to-end use is `TestColdCrash` and `TestNPMHACrash`
([e2e.md](e2e.md#a-replica-killed-during-a-cold-install)).

## Conformance check (R-STOR-035)

`blob.StartupCheck` writes, reads back, compares and deletes `<prefix>/_probe/<replica>` and lists
`<prefix>/sha256/` with one key. `blob.AwaitStartupCheck` repeats it: a configuration error (`AccessDenied`,
`NoSuchBucket`, bad credentials) is returned at once, a transient error or an open circuit is retried with exponential
backoff capped at 30 s and logged once per minute, and the call returns only when the check passed or the context ended.
`blob.RunConformance` runs `blob.ConformanceChecks()`, which also cover: a missing key reads as not found, overwrite
and delete visibility to the next read and list (A1), concurrent writes of identical bytes (A6), a ranged read, a
multipart object invisible until completion and complete after, an aborted upload leaving neither object nor listed
upload (A2), a completion that assembles the part it names and refuses an ETag the store never issued (a part number sent twice, A2), a server-side copy that replaces its destination and reports a missing source as not found (the object write of
a session's finalize, R-STOR-011), `DeleteObjects` (optional: its failure names the connection's `batch_delete`), a presigned
URL, and the capability checks (`Capability`: a wrong payload checksum refused, metadata kept by a write, metadata replaced by a copy onto itself
that keeps the object's bytes, a server-side copy from each other connection on the endpoint, `ConformanceOptions.Peers`). The visibility of an overwrite and a delete is
reported as `strong` or `eventual (tolerated, A1)` in `CheckResult.Note`, never as a failure (Q-STOR-249). Everything it writes lies below
`<prefix>/_probe/` and is removed. Conditional writes are not checked: Quader uses none.

**The start check per connection** (R-STOR-035, [startcheck.go](../../internal/blob/startcheck.go)): `CheckConnection` is the startup check
with the connection's payload checksum (a refusal is a configuration error naming `payload_checksum` and `request_checksum_calculation` and
the values to try), the marker `<prefix>/_connection` (written when absent, refused when it names another installation or id, R-STOR-051),
and the capabilities, which `Store.RecordCapabilities` writes into `storage_connections.capabilities` and `Connection.SetCapabilities`
keeps for `Verifies`. `AwaitConnections` runs every connection's check at once with the backoff of `AwaitStartupCheck` and returns when the
check passed on one connection without drain and every such connection answered once
([Q-STOR-252](../spec/storage.md#q-stor-252--the-first-readiness-waits-for-each-connections-first-answer)); a configuration error of a
connection without drain is the start's error, a draining connection's is logged. Before the checks, `Store.RecordConnections` records the
connections (first sight with the audit event `system.storage.connection_added`, the fixed properties, a retired id refused, the bind of
the migration's row, [Q-STOR-250](../spec/storage.md#q-stor-250--the-blobs-that-exist-become-copies-of-the-connection-default-which-its-first-replica-binds)).
The readiness check `objectStore` asks every connection with one `HeadObject` on a sentinel key and names the failing ones
(`unavailable: secondary`); it is reported, and with `health.require_object_store` gating while no connection without drain answers
([wire_blob.go](../../internal/app/wire_blob.go) `objectStoreCheck`). Each heartbeat writes the connections, their circuits and their
drain flags into `replica.object_store` (`objectStoreView`, R-OPS-003).

`quader admin storage check [--connection <id>]` ([storage_connections.go](../../cmd/quader/storage_connections.go)) runs
`RunConformance` on every connection, or the one named, with the others as peers, reads the marker without writing one, needs no database,
prints one line per check (`ok`, `absent` for a missing capability, `FAILED`) and exits `5` when a check that is not optional failed.
`quader admin storage forget-connection <id>` is `blob.ForgetConnection` with the audit event `system.storage.connection_forgotten`: exit
`2` while its own configuration or a live replica (the registry's object-store view) has the id.

### Running it against another S3

The integration tests are the check. `TestConformance` needs only the `QUADER_TEST_S3_*` variables, no PostgreSQL and no
Valkey:

```sh
export QUADER_TEST_S3_ENDPOINT=https://s3.example.com        # example
export QUADER_TEST_S3_REGION=eu-central-1                    # example
export QUADER_TEST_S3_ACCESS_KEY=… QUADER_TEST_S3_SECRET_KEY=…
export QUADER_TEST_S3_BUCKET_BLOBS=quader-conformance         # example; the test writes below it/<run id>/ and removes it
export QUADER_TEST_S3_FORCE_PATH_STYLE=true                  # false for virtual-hosted addressing
go test -tags integration -run Conformance -v ./test/integration/blob/...
```

Store-specific settings (all optional): `QUADER_TEST_S3_PAYLOAD_CHECKSUM` (`sha256` default, `md5`, `none`),
`QUADER_TEST_S3_REQUEST_CHECKSUM` and `QUADER_TEST_S3_RESPONSE_CHECKSUM` (`when_supported` default, `when_required`),
`QUADER_TEST_S3_BATCH_DELETE` (`true` default). Against SeaweedFS (the stack) the checks pass with the defaults, with
`md5`, and with `none` plus `when_required`. The stack's gateway is https with a private CA, which the suite passes as
the connection's `ca_file`; the presigned fetch must use `Client.HTTPClient()` (or a client with `WithRootCAs`) to trust it. SeaweedFS creates a missing bucket on the first write, so a wrong bucket name is
not an error there; the credential test uses a wrong secret key instead.

## The reconciliation (R-STOR-056)

[internal/blob/reconcile](../../internal/blob/reconcile/) is the worker job `storage-reconcile` that brings every blob onto every
connection without `drain: true`. It is infrastructure and imports no domain package; [wire_storage_reconcile.go](../../internal/app/wire_storage_reconcile.go)
registers the kind (queue `maintenance`, exclusive) and the task (`* * * * *`, `maintenance.tasks.storage-reconcile`, not scheduled in
read-only maintenance mode) and binds `Reconciler.RunJob` as the handler on worker and all. Its statements are
[reconcile.sql](../../internal/meta/postgres/queries/reconcile.sql) beside `LockBlobForMark`, `BlobCopies`, `PutBlobCopy` and
`StampBlobCopyVerified` of [blob_copies.sql](../../internal/meta/postgres/queries/blob_copies.sql).

| File | Responsibility |
|---|---|
| [reconcile.go](../../internal/blob/reconcile/reconcile.go) | `Options`, `Reconciler`, `Run` (the loop of a run), `RunJob`, `Result`, the insert bound |
| [queue.go](../../internal/blob/reconcile/queue.go) | the queue step: `copyBatch` (due rows of reachable connections, `concurrency` at a time), `copyRow`, the sources in order, `copyAndRecord`, `record`, `fail` |
| [copy.go](../../internal/blob/reconcile/copy.go) | the copies themselves, object stores only: `copier.server` (`BucketCopier.CopyFrom` and `HeadObject`), `copier.stream` (one request up to a part, ranged reads into a multipart upload above), the classification of a failure as the source's (with its evidence) or the target's |
| [pass.go](../../internal/blob/reconcile/pass.go) | the pass: `passDue`, a batch of 1,000 live blobs with their present copies, the gaps queued, the wait at `queue_limit`, the end of a full pass (`last_full_pass_at`, `filled_at`) |
| [drain.go](../../internal/blob/reconcile/drain.go) | the drain's walk, the survivors, the confirmation with `HeadObject`, the claim, the deletes under the hand-over deadline, the unfinished claims, `drained_at` with the event, `retired_at` |
| [journal.go](../../internal/blob/reconcile/journal.go) | the journal pass: the listings of `<prefix>/_journal/revocations/` walked side by side, a page each, and the objects one lacks copied |
| [state.go](../../internal/blob/reconcile/state.go) | the positions of the pass and the drains in `system_state` (`storage_reconcile_state`) |
| [limiter.go](../../internal/blob/reconcile/limiter.go), [signal.go](../../internal/blob/reconcile/signal.go), [metrics.go](../../internal/blob/reconcile/metrics.go) | the throttle of the streamed bytes, the signal after a commit, the counters and the gauges |

**A run** loads its positions, clears `filled_at` of every connection with `drain: true`, finishes the drain's claims older than
`gc.handover_delay`, and then repeats one round until nothing moves, `storage.reconcile.run_duration` has passed or the replica drains:

1. **Queue.** Up to `8 × concurrency` due rows of the connections the worker configures with a closed circuit (`ReconcileDueCopies`). A row is
   deleted when its blob is not live, its connection has `drain: true`, already holds a present copy, or the blob has no present copy on any
   connection (Q-STOR-258). Otherwise the sources are the present copies on the other reachable connections, verified first, then in list
   order. A source copy is tried server-side when `server_side` is `auto`, both connections lie on one endpoint and the target's start check
   recorded `serverSideCopyFrom[source]` (Q-STOR-254); a refusal sends the pair to streaming for the rest of the run. A streamed copy holds
   one part (Q-STOR-255) and is checked against the digest before the target sees a completed object; a source found missing, of another
   size or with another digest is marked (`Store.MarkCopyDamaged`, which queues its repair) and the next source is tried. The record
   (Q-STOR-256) runs under `LockBlobForMark`, refuses a record later than `min(upload.commit_deadline, gc.delete_settle / 2)` after the write
   (the object is written again, at most three times), writes the copy `present` with `PutBlobCopy`, deletes the queue row, stamps a
   streamed copy's source verified and sets the blob `ok`. A failure counts `attempts` with the back-off of R-STOR-056 and one of five
   reasons; a write the target's breaker refused leaves the row as it is.
2. **Pass** (Q-STOR-257). A new pass starts when `ReconcilePassDue` says so (a target never ended one, is filling with an empty queue, or
   the last ended `reconcile.PassInterval`, an hour, ago). A batch queues, for each target, the live blobs it lacks while another connection
   holds them; the pass waits while a target's queue holds `queue_limit` rows. A pass that reaches the end records `last_full_pass_at` for the
   targets it had from its start, sets `filled_at` of those it found no gap on and whose queue is empty, and runs the journal pass when the
   worker reaches two targets or more.
3. **Drains** (Q-STOR-259). For each connection with `drain: true` and a closed circuit, unless the deletion pause is set, one batch of 500
   of its copies by object name: a blob a target lacks is queued there; a blob whose targets all hold it, one verified within its
   connection's `scrub_period`, has that copy confirmed with `HeadObject`, is claimed (under `LockBlobForMark`, its survivors read again) and
   its object deleted on the draining connection, `DeleteObjects` when its `batch_delete` is true, until `handover_delay − delete_settle`
   after the claims; the rows of the deletes that worked go. A walk that deleted or queued nothing is followed by the next after the pass
   interval. A draining connection without a copy is drained (`ReconcileMarkDrained`, the event `system.storage.connection_drained` with the
   copies deleted in the same transaction).

At its end a run retires every drained connection that no live replica configures (`ReconcileRetire`, with its queue rows) and saves its
positions; the result is stored with the job (`copied`, `serverSide`, `streamed`, `bytes`, `failed`, `dropped`, `queued`, `passBlobs`,
`passEnded`, `filled`, `drained`, `retired`, `copiesDeleted`, `journalCopied`, `ended`).

**The signal.** With two or more connections without drain, `wireStorageReconcile` starts a `reconcile.Signal` and gives its `Notify` to
`Store.SetGapSignal`; `CommitAll` calls it after a commit whose link round queued a gap (`signalGaps` in
[reconcile_seams.go](../../internal/blob/reconcile_seams.go)), and the repair case after it recorded its copies. The signal enqueues the kind
at most once a second per replica; the exclusive key folds the enqueue into the run that is queued or running. With one connection no commit
queues a gap and no signal is built.

**Metrics.** The run counts `quader_storage_reconcile_copied_total{connection,mode}`, `quader_storage_reconcile_copied_bytes_total{connection,mode}`
(the blob's size for both modes) and `quader_storage_reconcile_failures_total{connection,reason}`. Every replica with the main pool adds a
source of the database gauges (`ReconcileGauges`, evaluated in the background like the job gauges): `quader_storage_reconcile_queue`,
`quader_storage_reconcile_queue_oldest_seconds`, `quader_storage_connection_filling` and `quader_storage_connection_detached` per
connection that is not retired, four series each, so an empty queue shows 0. Filling is a connection without `filled_at` that a live
replica of the registry configures without drain (Q-HA-393); the alert `QuaderStorageReplicationBacklog` leaves it out. Detached is a
connection that holds a copy and that no live replica configures (R-STOR-051), which `QuaderStorageConnectionDetached` reads; the live
replicas are the registry's rows whose heartbeat is younger than `system.heartbeat_stale_after`. The storage summary's backlog and fill ratio are package F's.

**What a run holds.** At most `concurrency` copies at a time, each at most one part of `object_store.multipart.part_size` in memory
(64 MiB with the defaults); the streamed bytes of a run pass one throttle of `bytes_per_second`; a server-side copy moves no byte through the
worker. The copies of a run share the replica's object-store clients and their breakers, so a run's failures open a connection's circuit for
the uploads of that replica as well.

**Contracts with the other packages.** A copy row in state `deleting` whose blob is live is the drain's claim: collection (package B) claims
copies only of a blob it claimed. The drain does not take the backup hold of R-STOR-024 step 2b: the backup reads one copy per blob and must
read another when its first is gone (package E); until then the backup reads the first connection only
([H-1262](../security/object-store-connections.md#h-1262--until-the-backup-reads-every-connection-a-drain-of-the-first-connection-empties-what-it-reads)).
The scrub (package D) is what verifies a server-side copy; until it is built only a verified download does
([H-1261](../security/object-store-connections.md#h-1261--until-the-scrub-is-built-a-server-side-copy-is-verified-only-by-a-download)).

## Tests

| Tier | Where | What it proves |
|---|---|---|
| unit | [internal/blob](../../internal/blob/), [s3](../../internal/blob/s3/), [blobtest](../../internal/blob/blobtest/) | key layout, spool admission and cleanup on every error path, single pass digests, the commit protocol and its branches against a fake database, multipart (threshold, parts, concurrency, abort, checksums), the breaker, verification, the mark of a missing object, its repair and its recheck once per interval (`TestDownloadVerification`, Q-STOR-151, Q-STOR-153), conformance logic, the SDK adapter against a fake endpoint (retries, error mapping, stall, credentials, metadata `TestR_STOR_062_TheAdapterCarriesObjectMetadata`); with two in-memory stores, the fault hook and the fake database ([connections_test.go](../../internal/blob/connections_test.go)): the derivation (R-STOR-052), the commit's routing, fall-over and `min_copies` (R-STOR-053), the reads, their fall-over, marks, recheck and daily stamp (R-STOR-055), `Servable` and the metadata of `blobtest` (R-STOR-062), the start check with the marker and the capabilities and `AwaitConnections` (R-STOR-035, R-STOR-060) |
| unit | [internal/config](../../internal/config/storage_connections_test.go), [internal/app](../../internal/app/wire_blob_test.go) | the configuration table of R-STOR-050 and the files of a provisioner's Secret; the readiness of R-STOR-060 and the registry's object-store view |
| unit | [internal/blob/reconcile](../../internal/blob/reconcile/) | the streamed copy in one request and in parts, its digest and size checks, the source's and the target's failures; the server-side copy, its fall-back to streaming for the run and without the start check's word; the throttle; the journal pass with an object above an entry's bound; the gauges' series; the signal folding commits into one enqueue (about 0.3 s, objects of up to 2.5 KiB) |
| integration | [reconcile_test.go](../../test/integration/blob/reconcile_test.go) | PostgreSQL of the stack with in-memory stores: the catch-up after an open circuit without spent attempts, the fill of a connection added to twenty blobs, the drain with a blob only the first holds, an unverified survivor, the deletion pause, the event, the retirement, and the insert bound (about 3.7 s, four databases, at most twenty objects of 1 KiB) |
| integration | [connections_s3_test.go](../../test/integration/blob/connections_s3_test.go) | two real buckets of the stack's SeaweedFS: the routing, a server-side and a streamed copy between them, the metadata a cross-bucket copy keeps, a read that falls over and the reconciliation's repair (about 1.1 s, one database, three objects of 1 KiB) |
| integration | [test/integration/blob](../../test/integration/blob/) | SeaweedFS conformance; the 100-upload race through four replicas; the race with deletes and collection; the collector killed or cancelled at random points; the drain; hand-over; `AS001`; the usage fold (counters after uploads, an overwrite and a delete against a recount; two loops and four callers on 50,000 and 40,000 deltas with the deadlock counter of the database; a deleted repository with unfolded deltas; a backlog of 25,000 under an interval of one hour); a table added by a newer release; pause, hold, dry run; checkpoint resume; the job handler; verification of a corrupted object; the records of the connections against PostgreSQL with in-memory stores ([connections_test.go](../../test/integration/blob/connections_test.go): fixed properties, detached, forget, the bind of the migration's row, the copy and queue rows of a commit and their drop, R-STOR-051, R-STOR-052) |

`make test` runs the unit tier, `make test-integration` the integration tier (`QUADER_TEST_BLOB_RACE_SECONDS` stretches the race test from 15 s to a soak run). The
unit fake (`fakedb_test.go`) simulates only the table operations the commit protocol and the download path use; the real locking
behavior is what the integration tier checks, with the collector's reference check mutated away to see the tests fail.

`QUADER_TEST_PG_POOLED=1` runs the blob and the coordination integration suites with the main pool through PgBouncer in transaction mode
(`testenv.PgPooled`; the role defaults of R-HA-014 are set by `testenv.ThroughPgBouncer`). CI should run the suite once each way.

An object-store response without `Content-Length` has `ObjectInfo.Size == -1` (unknown), never 0: the early size check
of a download is skipped and the digest at the end of the body decides. A caller's own deadline ending a store call
(`context.DeadlineExceeded` that is not a transient `StoreError`) is no answer for the circuit breaker.

## Several connections: the build plan

Stage 1 of the owner's topic on the stored content ([storage.md](../spec/storage.md#several-object-store-connections) R-STOR-050 to
R-STOR-063, R-STOR-026, R-STOR-027; [ha-and-helm.md](../spec/ha-and-helm.md) R-HA-072; [backup-and-restore.md](../spec/backup-and-restore.md)
R-BAK-030; [api-and-ui.md](../spec/api-and-ui.md) R-UI-196, R-UI-197;
[ADR-239](../decisions.md#adr-239--several-object-store-connections-every-blob-on-every-connection-a-row-per-copy-one-reconciliation-and-a-scrub-per-copy)).
The sections above describe what is built: packages A and C. The build comes in six packages; each is a whole feature for one builder, the
directories (or, inside `internal/app` and `internal/api/v1`, the files) of packages that run at the same time are disjoint, and at most
four run at once. Package A left the hooks of B, C and D in [wire_storage_sweep.go](../../internal/app/wire_storage_sweep.go),
[wire_storage_reconcile.go](../../internal/app/wire_storage_reconcile.go) and [wire_storage_scrub.go](../../internal/app/wire_storage_scrub.go):
`register<X>(r *jobs.Registry)`, called by `jobRegistry`, and `wire<X>(ctx, blobArea) error`, called at the end of `wireBlob`; package C
filled those of the reconciliation, the others do nothing yet.

| Package | After | What it builds | Owns |
|---|---|---|---|
| **A · Connections and copies** | — | The configuration (`storage.connections`, `storage.object_store`, `storage.replication`, `storage.reconcile`, `storage.scrub`; `storage.s3` removed) and its validation (R-STOR-050); one client and one guard per connection, the ordered set with its naming function and `Servable` (R-STOR-062); object metadata on the interface, the S3 adapter and `blobtest`; the migrations (`storage_connections`, `blob_copies`, `blob_copy_queue`, `revocation_outbox_shipped`, `upload_sessions.connection_id`; `deep_checked_at` and `blobs_check` dropped); the commit writing copy rows and queue rows (R-STOR-053); reads over the copies (R-STOR-055); the fixed properties, the marker and `forget-connection` (R-STOR-051); the start check and the conformance check per connection with the capabilities (R-STOR-035); the breaker, the metrics with `connection`, readiness and the replica registry's object-store view (R-STOR-060); every size check on `stored_size`; empty registration files for B, C and D in `internal/app` | `internal/config`, `internal/blob` (all files but `gc*.go` and the new packages of B to D), `internal/blob/s3`, `internal/blob/blobtest`, `internal/meta/postgres/migrations` and `queries`, `internal/app/wire_blob.go`, `internal/app/wire_jobs.go`, `internal/cluster/replica_registry.go`, `cmd/quader/admin_storage.go` (`check`) |
| **B · Sessions, collection, orphan sweep** | A | Sessions on their connection (R-STOR-054); collection per copy with the release (R-STOR-057); the orphan sweep per connection with the adoption (R-STOR-026) | `internal/uploads`, `internal/blob/gc.go`, `gc_uploads.go`, `gcjob.go`, `internal/blob/sweep.go` (new), `internal/app/wire_storage_sweep.go`, `test/integration/blob/gc_*`, `sweep_test.go` |
| **C · Reconciliation** (built) | A | The job with queue, pass, server-side and streamed copies, insert bound, drain, retire, journal pass and its metrics (R-STOR-056); the integration case against two real buckets (R-STOR-063) | `internal/blob/reconcile/` (new), `internal/app/wire_storage_reconcile.go`, `test/integration/blob/reconcile_test.go`, `connections_s3_test.go` |
| **D · Scrub, damaged blobs, operator commands** | A | The scrub per copy with repair (R-STOR-027); the damaged-blobs API with its two scopes; `admin storage verify` and `restore-versions` per connection (R-STOR-058); the renames that make R-STOR-027's evidence true (Q-STOR-240) | `internal/blob/scrub/` (new), `internal/blob/verify`, `internal/blob/restoreversions`, `internal/app/wire_storage_scrub.go`, `internal/api/v1` (the damaged-blobs files and their paths in `openapi.yaml`), `cmd/quader/admin_storage*.go` (but `check`), `cmd/quader/admin_restoreversions.go`, the five test files that name R-STOR-027, `docs/coverage/implemented.txt`, `test/integration/storageverify` |
| **E · Chart, backup and restore, journal** | A | The chart's list and Secrets with the refusals and the alerts (R-HA-072, R-HA-045); the backup reading one copy per blob and the restore into the first connection with its fix-up (R-BAK-030); the journal shipped per connection through the guards and replayed from every connection (R-STOR-059); the development profile and the integration stack's `QUADER_STORAGE_CONNECTIONS` | `deploy/helm/quader`, `test/chart`, `deploy/dev`, `test/integration/testenv`, `internal/backup`, `internal/journal`, `internal/app/wire_journal.go`, `internal/app/wire_backup_check.go`, `cmd/quader/backup_*.go`, `admin_postrestore.go`, `admin_backupcheck.go`, `test/integration/backup` |
| **F · The surface and the manual** | C, D | The connections' API and the system information (R-STOR-061); the storage summary per connection (R-STOR-028); the tab **Storage** and the dashboard's lines (R-UI-196, R-UI-197), rendered under rule 11; the operator manual, the README and the developer pages that describe a running installation (configuration, installation, backup and restore, TLS, the runbook, the chart, the local profile, observability, and this page's sections above) | `internal/api/v1` (the connections' files and paths), the storage summary's task, `web/src/app/features/system`, `docs/manual`, `README.md`, `docs/developer` |

The tests and their cost are R-STOR-063's table (about 30 s added to CI, nothing per format, no end-to-end leg). Nothing of it needs the
owner's provider; what only its real bucket can show is a run of `quader admin storage check` against it, which stays not verified until
someone runs it.

Stage 2, the encryption of stored content per connection, has its own build plan, which starts its packages after the packages of this
table named there: [stored-content-encryption.md](stored-content-encryption.md).
