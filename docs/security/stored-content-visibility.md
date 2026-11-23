# What a provider still sees of encrypted content

Status: specified on 2026-11-22 ([storage.md](../spec/storage.md) R-STOR-075, R-STOR-086, R-STOR-088;
[backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-031; [bastion.md](../spec/bastion.md) R-BAS-061). The Bastion's cache with a key
is built ([security/bastion.md](bastion.md#with-a-cache-key)); the encrypted connections of the main installation are built (package 3) and
their backups too (package 4), but the Helm chart refuses `encrypted: true` until package 6, so until then a provider of an installation of the
chart sees every blob in the clear under its SHA-256, as [object-store-connections.md](object-store-connections.md) says.

The perspective of this page is a party that can list and read a bucket but holds no key: the provider and its operators, a principal its
policies admit, whoever copied the bucket, a reader of a backup's target, a holder of a Bastion's cache store. It says what such a party still
learns once a connection is encrypted, object family by object family. What it can change, and what is refused, is
[stored-content-format.md](stored-content-format.md).

## On an encrypted connection

| Object | What a lister or reader learns | Why it is so |
|---|---|---|
| A blob, `keyed/<..>/<..>/<HMAC>` | that an object exists, its stored size (which gives the exact plaintext size), its time of writing, its entity tag (an MD5 of ciphertext or the multipart form), the storage key's identifier in its metadata, the times it is read (from the provider's own logs) | the name is an HMAC under a key the provider lacks, the content is AES-256-GCM; sizes, times and access are what a store must know to store |
| A blob without keyed names, `sha256/<digest>` | the same, and which known file it is: anyone who has a public artifact computes its digest and finds it | the detail `keyed_names: false` (R-STOR-086) |
| An upload session's staging object and tails, `_uploads/…` | sizes and times of an upload in progress | encrypted under the session's data key (R-STOR-081) |
| The revocation journal, `_journal/revocations/…` | revoked token identifiers, principal identifiers, times | plain on purpose (R-STOR-088, Q-STOR-315) |
| The marker `_connection` and the probes `_probe/…` | the installation's identifier and the connection's; random bytes | plain on purpose (R-STOR-088) |

A copy row in Quader's database names the object; the provider does not see the database.

## Elsewhere

| Where | What a party without a key learns |
|---|---|
| A backup's target | the pool names blobs by digest, the encrypted ones included, beside the database files, which are readable until R-BAK-027 encrypts them (backup-and-restore.md Q-BAK-65) |
| A plain connection of the same installation | every blob in the clear: encryption is per connection, and a plain connection also serves signed links (R-STOR-088) |
| A Bastion's cache store with a cache key | object sizes, counts, times, the access markers' times, the key's identifier, the replica's name and time in the name of each file of the audit spool (R-BAS-061) |
| A Bastion's cache store without a key | everything it holds, as today (the default) |

## What this does not cover

- [H-13](../spec/storage.md#h-13--deduplication-reveals-the-existence-of-known-content-through-upload-timing) and
  [H-552](../spec/storage.md#h-552--a-deduplication-hit-across-projects-can-be-told-apart-by-its-timing): the timing of a deduplication hit tells
  a client that content exists; encryption at the store changes nothing there.
- [H-48](../spec/backup-and-restore.md#h-48--database-files-are-readable-to-every-reader-of-the-target-until-p1): the database files of a
  backup are readable to the target's readers.

### H-1312 · The stored size gives away the exact plaintext size

Component: the stored size `S(L)` ([storage.md](../spec/storage.md) R-STOR-076). Subject: every blob of an encrypted connection. The overhead
is fixed (28 bytes per 64 KiB and 68 bytes), so the plaintext size follows exactly from the size a listing shows; an artifact of a known size
(a public tarball, a known image layer) is confirmed with good probability by its size alone, keyed names notwithstanding. Live on every
encrypted connection. Padding objects to size classes would narrow it at a price in storage that nobody has chosen; the owner has not been asked.

### H-1313 · A provider sees when objects are written and read

Component: the object store itself. Subject: the activity of an installation. Writes happen at upload, reads at download (Quader streams every
download of an encrypted connection), so a provider's request logs show which object is read when and how often; with the size of H-1312 that
can point at a popular artifact. Live on every connection, encrypted or not; no design of the stored format can hide it.

### H-1314 · The backup's pool names blobs by their digest

Component: the pool of a backup ([backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-031, Q-BAK-65). Subject: blobs of encrypted
connections in a backup. A pool object keeps the ciphertext and the record but is named `pool/sha256/<h0h1>/<h2h3>/<digest>.<key id>`
(R-BAK-032, built with package 4: a name that carries the storage key's identifier so that a retired connection's keys can be destroyed), so a
reader of the backup's
target confirms known files by name, which the keyed names hide at the connection. Live for every backup of an installation with an encrypted
connection; it adds nothing while the database files beside it are readable (H-48), and becomes the leak once R-BAK-027 encrypts them. Naming
the pool by the copies' keyed names is the owner's open question (Q-BAK-65 (b)).

### H-1315 · An encrypted connection without keyed names lets a lister confirm stored files

Component: the detail `keyed_names: false` ([storage.md](../spec/storage.md) R-STOR-086). Subject: every blob of such a connection. Its names
are the digests of R-STOR-001, so anyone who may list the bucket confirms, without a key, that a known file is stored and since when. Live only
where an operator set `keyed_names: false`; the default is `true`.

### H-1316 · The journal, the marker and the probes are plain on an encrypted connection

Component: the key families other than the blobs ([storage.md](../spec/storage.md) R-STOR-088, Q-STOR-315). Subject: the revocation journal's
entries. The journal lies plain on every connection: a provider reads which tokens were revoked and which principals they belonged to, by
identifier, and when; a holder of a runtime credential can still overwrite an entry
([H-553](../spec/backup-and-restore.md#h-553--a-journal-object-can-be-overwritten-with-the-runtime-credential)). The marker names the
installation and the connection; the probes are random. Live on every encrypted connection; the journal holds identifiers, no content. The
destruction of a retired connection's keys is journaled ([storage.md](../spec/storage.md) R-STOR-099, specified, not built): its entry tells
every provider of a connection that does not drain which connection's keys were destroyed and when, by identifiers only.

### H-1317 · A plain connection beside an encrypted one keeps the content in the clear

Component: encryption per connection ([storage.md](../spec/storage.md) R-STOR-075, R-STOR-088). Subject: every blob of an installation that
mixes plain and encrypted connections. The encrypted provider sees ciphertext; the plain provider sees every blob in the clear, and signed links
come from the plain copy where the switch allows them (Q-STOR-291). Encryption per connection protects one provider's copy, not the content.
Live in every installation with both kinds; it is the owner's choice per connection, and the admin area shows `encrypted` per connection.

### H-1318 · The Bastion's cache shows sizes, counts and times, and stays plain without a key

Component: the Bastion's cache store ([bastion.md](../spec/bastion.md) R-BAS-061, R-BAS-030). Subject: what a Bastion holds in the DMZ. With
a cache key a holder of the cache store learns object sizes (and so plaintext sizes, the frame in front of each object adding a fixed 182
bytes), their number, their times and the access markers' times, which the eviction needs, and from the names of the audit spool's files the
replicas that spooled and when; the answers in Valkey stay signed and not encrypted. Without a key, the default, the cache store holds artifacts,
documents, entries naming request paths, and the audit spool of download events in the clear, as today. Live for every Bastion with a cache; a
key in its chart closes everything but sizes and times.
