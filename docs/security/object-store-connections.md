# Several object-store connections: who can read, change or delete a copy, and what Quader believes about one

Status: specified on 2026-11-22 ([storage.md](../spec/storage.md) R-STOR-050 to R-STOR-063, R-STOR-026, R-STOR-027,
R-STOR-035; [ha-and-helm.md](../spec/ha-and-helm.md) R-HA-072; [backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-030;
[ADR-239](../decisions.md#adr-239--several-object-store-connections-every-blob-on-every-connection-a-row-per-copy-one-reconciliation-and-a-scrub-per-copy)),
built: the connections, their copies, the commit and the reads over them, the marker and the start check per connection, the
reconciliation that copies every blob to every connection, fills a new one and drains an old one, the scrub with its repair and the
operator check per connection, sessions on their connection, collection per copy and the orphan sweep per connection, the backup and the
restore per connection and the journal per connection, and since 2026-11-23 the connections' read-only API and the tab **Storage** of
System status (R-STOR-061, R-STOR-028; [storage.md](../spec/storage.md#several-object-store-connections), the status there). What
[privilege-footprint.md](privilege-footprint.md) and
[transport-encryption.md](transport-encryption.md) say of the object store holds for each connection.

The perspective of this page is the stored content once it lies on several object stores: which parties can read it, change it or delete
it at each store, what Quader believes about a copy (the rows of R-STOR-052) and on what evidence, and what one store's failure, slowness
or takeover costs the others. Encryption of the stored content is stage 2 and described on its own pages
([stored-content-format.md](stored-content-format.md), [stored-content-keys.md](stored-content-keys.md),
[stored-content-visibility.md](stored-content-visibility.md)); without it, every party that can read a connection's bucket reads every blob.

## The parties

```
 replicas api, packages, worker            backup pod                restore Job
   one credential per connection             a read credential           a write credential
   (credentials_source of each element)      per connection (R-BAK-030)  per connection
        │  TLS, each connection's CA            │                           │
        ▼                                       ▼                           ▼
 ┌───────────────────────┐   ┌───────────────────────┐   ┌───────────────────────┐
 │ connection default    │   │ connection secondary  │   │ … up to eight         │
 │ provider A's bucket   │   │ provider B's bucket   │   │                       │
 │ sha256/  _uploads/    │   │ sha256/  _uploads/    │   │                       │
 │ _probe/  _journal/    │   │ _probe/  _journal/    │   │                       │
 │ _connection (marker)  │   │ _connection (marker)  │   │                       │
 └───────────────────────┘   └───────────────────────┘   └───────────────────────┘
        ▲                                       ▲
   provider A's operators and every            provider B's operators and every
   principal A's policies admit                principal B's policies admit
```

| Party | Holds | Can do to the content |
|---|---|---|
| A replica (`api`, `packages`, `worker`) | each configured connection's runtime credential (R-STOR-031, per connection) | read, write and delete every blob of every connection it has; it cannot destroy object versions where the credential lacks `s3:DeleteObjectVersion` ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-023, per connection) |
| The backup pod | a read credential per connection, or the connection's own with a workload identity (R-BAK-030) | read every blob; it writes no copy row and marks no copy |
| The restore Job | a write credential for the restoring installation's connections, and a journal reader per source connection | write the restored blobs into the first connection without drain, probe every connection, read the journal of every source connection |
| A provider's operators, and each principal its policies admit | the bucket | read every object; replace or delete one, which Quader sees only by a read (below) |
| A holder of the database | the rows of R-STOR-052 | say that a copy is present or verified when it is not; a drain then deletes beside it (H-1256) |

## What Quader believes about a copy, and on what evidence

| Belief | Evidence | Where |
|---|---|---|
| A copy exists (`present`) | the store's answer to the write that made it, a size from `HeadObject` after a multipart or a copy, or an object of a live blob the orphan sweep found on a connection without `drain: true` whose marker is absent or names this installation (adoption, Q-STOR-262) | R-STOR-052, R-STOR-026 |
| A copy is the blob (`verified_at`) | Quader hashed what it wrote and the store refused a wrong payload checksum (the start check establishes whether it does, R-STOR-035), or a read to the end matched the digest | R-STOR-052, R-STOR-055, R-STOR-027 |
| A copy is damaged | only evidence about the object: "no such object", a wrong size, a read to its end with another digest; never a timeout or an open circuit | R-STOR-055, R-STOR-027 |
| A location belongs to this connection | the marker `_connection` names this installation and this id | R-STOR-051 |

- **A replaced object is found by a read.** Whoever can write a connection's bucket can replace an object; the next full read of that
  copy (a download, the scrub within `scrub_period`, a reconciliation stream) finds the digest wrong and marks the copy `corrupt`; the scrub
  rewrites it from another connection in the same run and reads the result back before the copy counts (Q-STOR-265), and the
  reconciliation repairs what the scrub could not. A ranged read serves whatever the bucket holds until then
  ([H-11](../spec/storage.md#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks)), now on every connection.
- **A deleted object is found by a read** as well and restored from another connection; with versioning on a connection, also by
  `restore-versions` there ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-025).
- **No guarantee rests on a conditional write** (R-STOR-035 A7): a store that ignores `If-None-Match` cannot be made to break the commit,
  the reconciliation or collection by it.

## What one store's failure or takeover costs the others

| Event at one connection | Effect on the others and on clients |
|---|---|
| Outage (17.5 hours at the owner's provider in July 2026, per its status page, read by a research of 2026-11-22) | Uploads and reads go to the other connections; blobs only it holds answer `503`; its queue grows and is copied afterwards (R-STOR-036, R-STOR-056) |
| Slow answers within the timeouts | Reads wait up to `response_header_timeout` on it before they fall over (R-STOR-055); no other connection is affected |
| Its credential stolen | The thief reads every blob there (no encryption in stage 1); writes and deletes there are found by reads and repaired from the others; at a provider whose credentials are project-wide, the thief may also reach other buckets of that project (H-1253) |
| Its bucket emptied by its provider or a lifecycle rule | Every copy there is found missing by reads and the scrub and rewritten from the others; the drain's check before a deletion keeps the last copy (R-STOR-056) |
| Its journal objects forged or deleted | The replay reads every connection that can be listed and replays every differing copy, flagged with the connections that hold it; an entry only it holds and that cannot be listed is not replayed ([H-1284](backup-and-restore.md#h-1284--an-entry-only-a-connection-that-cannot-be-listed-holds-is-not-replayed)) ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-054, [H-553](../spec/backup-and-restore.md#h-553--a-journal-object-can-be-overwritten-with-the-runtime-credential)) |

## What this does not cover

- [H-11](../spec/storage.md#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks): object-store principals
  bypass Quader's authorization and integrity checks; with several connections, at every connection's provider.
- [H-553](../spec/backup-and-restore.md#h-553--a-journal-object-can-be-overwritten-with-the-runtime-credential): a journal object can be
  overwritten with the runtime credential; with several connections, with any connection's. A journal object written under a key that
  the other connections lack is copied to them by the journal pass of the reconciliation (R-STOR-056), which reads no digest of it; the
  replay reads every connection anyway, so the copy shows the replay nothing it would not have read.
- [H-591](upload-sessions.md#h-591--the-staging-object-is-copied-without-being-read-back): the staging object is copied without being
  read back; the scrub reads the copy within its first run (R-STOR-054).

### H-1251 · A new blob lies on one connection until the reconciliation copies it

Component: the commit (R-STOR-053) with the default `storage.replication.min_copies: 1`. Subject: every blob uploaded while another
connection is configured. The `201` comes when one connection holds the blob; the second copy follows within the minute normally and only
after the outage when the others are down. Lost data at that one provider inside that window (not an outage, which only delays, but a
loss) loses the blob; a remote repository's content is fetched again, a local upload is gone. Live in every installation with two or more
connections and the default; none of the recorded outages of the research of 2026-11-22 lost data. `min_copies: 2` closes it at the
price that an outage of all but one connection refuses uploads.

### H-1252 · A server-side copy or an adopted object counts as present before it is read

Component: the reconciliation's server-side copy within one endpoint (R-STOR-056), the finalize of an upload session (R-STOR-054) and the
orphan sweep's adoption (R-STOR-026). Subject: copies whose bytes no part of Quader has hashed. They count for availability at once and are
read by the next scrub run (hourly, unverified copies first); until then a read of such a copy is still verified at its end, a ranged read
is not. A store whose copy operation produced other bytes, or an object put under a content key by someone else and adopted, is served by
ranged reads until the scrub reads it. Live wherever two connections share an endpoint, for every finished upload session, and after a
restore or a crash that leaves objects without rows. A drain never deletes beside such a copy (Q-STOR-234).

### H-1253 · A connection's credential reaches more than its own bucket

Component: the runtime credential of each connection (R-STOR-031), and the providers' credential models. Subject: the other connections'
buckets and the backup target. Quader asks each connection's credential for its own bucket and prefix only; a provider whose credentials
reach every bucket of an account or project unless a bucket policy narrows them (the owner's provider documents exactly that, with
credentials groups and bucket policies to narrow them, research of 2026-11-22, not verified against the provider) gives a holder of one
connection's credential the other buckets of that project, the backup target included when it lies there. Live wherever two connections,
or a connection and the backup target, lie in one project of such a provider without bucket policies; the chart cannot see it. Mitigation
outside Quader: one credentials group per bucket with a bucket policy that admits only it, and the backup target in another project or
account.

### H-1254 · Deleted content outlives its deletion on a connection that was unreachable

Component: collection's release of a copy whose delete was never sent (R-STOR-057), and a `detached` connection (R-STOR-051). Subject:
deleted artifacts, including content deleted because it must not be kept (a leaked secret uploaded by mistake). A released copy stays in
its bucket until that connection's orphan sweep removes it: after `storage.orphans.min_age` (24 hours) at the next weekly run, so up to
about eight days after the deletion; a detached connection's objects stay until the connection is configured again or emptied by its
operator, because `forget-connection` deletes no object and the sweep walks only the connections a worker configures. Live whenever a
connection's circuit is open during a collection run while another connection is writable, or a connection is removed while it holds
copies; dormant with one connection, whose outage stops collection instead of releasing
([Q-STOR-260](../spec/storage.md#q-stor-260--collection-releases-a-copy-on-an-open-circuit-only-while-another-connection-is-writable)).
`quader_gc_copies_released_total{connection}` counts the releases. What makes it matter is content whose removal is a legal or security
duty; the operator then runs the sweep by hand (`POST /api/v1/system/tasks/orphan-sweep/run` with `minAge` and `connection`) once the
connection answers, or removes the objects at the provider.

### H-1255 · A store that applies a delete after acknowledging it is not proven harmless to a later write

Component: assumption A3 of R-STOR-035, per connection, for the deletes of collection and of the orphan sweep (R-STOR-057, R-STOR-026).
Subject: a blob deleted by collection, or an orphan deleted by the sweep, and uploaded again soon after. The software
behind the owner's provider documents that a delete whose copies cannot be removed within 30 seconds is queued and acknowledged (research
of 2026-11-22). If such a queued removal could reach an object written under the same key after the acknowledgement, a re-uploaded blob
would lose its object while its copy row says `present`. The consequence is bounded: the next read falls over and marks the copy
`missing`, and the reconciliation restores it from another connection; with one connection the blob is `missing` until it is uploaded
again. Not verified in either direction: the documentation names "latest wins" by completion time, and no check can prove it. Dormant until
a store is seen to do it; `delete_settle` (30 s by default) is the margin collection keeps before a digest can be written again.

### H-1256 · A drain trusts a copy that was verified within its period

Component: the drain of R-STOR-056. Subject: the last good copy of a blob. A drain deletes a copy beside a present copy on every other
connection, one of them verified within its connection's `scrub_period`, and confirms that copy's existence and size with `HeadObject` right
before. Bytes of the same length replaced at the survivor since its last verification (up to 30 days earlier) are not seen; the drain then
deletes the only good copy, and the blob is `corrupt` when it is next read. Live during a drain, and only together with a write into the
surviving bucket by another party or a silent corruption of the same length there. Reading the survivor whole before each deletion would
close it at the price of reading every blob once more per drain.

### H-1269 · A copy that read back whole may still be served stale by a store that applies overwrites late

Component: the scrub's repair (R-STOR-027, [Q-STOR-265](../spec/storage.md#q-stor-265--the-scrub-replaces-a-copy-it-found-damaged-in-the-same-run-and-reads-back-what-it-wrote-before-the-copy-counts)),
which overwrites a `corrupt` object with the blob's bytes and reads it back whole before the row says `present`, on a store that documents
eventual consistency for overwrites (Q-STOR-249; the owner's provider documents it, research of 2026-11-22, not verified). Subject: the
next reads of that copy. The read-back can be answered by a node that already holds the new bytes while another node still serves the old
ones; the row then says verified, and a later read may get the damaged bytes. A full read finds them, marks the copy `corrupt` again and the
scrub or the reconciliation repairs it again; a ranged read serves them (H-11). Live only on such a store, for the seconds to minutes the
store takes to apply an overwrite, and only after a damage that needed a repair; dormant on a store with strong consistency. No read of
Quader can prove the store has applied an overwrite everywhere.

### H-1260 · Two installations that first start on one empty location at the same moment both pass the marker once

Component: the marker `_connection` (R-STOR-051), which the start check reads and, when it is absent, writes with a plain `PutObject`
([startcheck.go](../../internal/blob/startcheck.go), `checkMarker`); no conditional write guards it, because stores ignore or do not
offer `If-None-Match` reliably (the specification lets nothing rest on it). Subject: the rule that a location belongs to one installation.
Two installations whose first starts on the same empty location overlap both find no marker, both write one, and both pass; the marker
written last names its installation, and the other installation refuses the connection at its next start. Until then both write blobs
below one prefix, and the collection of each may delete objects the other references. The orphan sweep (R-STOR-026) of the installation
whose marker stands deletes the other's objects too, as orphans without a blob row, once they are older than `storage.orphans.min_age`;
the sweep of the other one reads the marker that names another installation and leaves the location alone (Q-STOR-262). Live only for two
installations configured with the same bucket and prefix whose first starts overlap within the start check (seconds); dormant otherwise.
The location rule of one installation (R-STOR-050 rule 4) does not see another installation's configuration.

### H-1265 · The orphan sweep can adopt an object that a store still lists after its delete

Component: the adoption of the orphan sweep (R-STOR-026, Q-STOR-262) on a connection without `drain: true`. Subject: the copy rows of a
blob that was collected and uploaded again while one of its connections was not written. A store that applies deletes late (the software
behind the owner's provider queues a delete it cannot finish within 30 s, research of 2026-11-22) can still list the old object of such a
blob on a connection that the new upload did not write; the sweep finds a live blob without a copy row there and adopts the object, which
then disappears. The copy row says `present` until the next read falls over and marks it `missing` (R-STOR-055) or the scrub reads it
(R-STOR-027); the reconciliation then writes it again from the copy the upload made. No content is lost and no wrong content is served
(a read of the vanished object falls over to the next copy before its first byte). Live with two or more connections at a provider that applies deletes late,
for a blob collected and uploaded again between two sweeps; dormant with one connection, where the new upload writes the same key on the
one connection. A `HeadObject` before each adoption would not close it: a store that lists a deleted object may answer for it as well.

### H-1257 · A holder of a connection's credential can keep replicas from starting through its marker

Component: the marker `_connection` (R-STOR-051). Subject: the availability of new replicas. Whoever can write a connection's prefix can
replace the marker with one that names another installation; every replica that starts afterwards refuses that connection as a
configuration error, and with the connection not draining it stops (R-STOR-060). Running replicas keep serving. Live for every holder of a
runtime credential of a connection; it adds nothing to what such a holder can do to the content itself, which is all of it. Repair: delete
the forged marker; the next start writes it again.


### H-2420 · The copies per blob on the storage page are up to an hour old

Component: the storage summary's copies per blob, sole copies, scrub coverage and damage (storage.md R-STOR-106, R-STOR-107). Subject: an
administrator's judgement whether the installation survives the loss of one connection. The figures come from the hourly snapshot, so a
connection that lost its copies, or a reconciliation that caught up, shows up to an hour late; the overview names the snapshot's time
(`figuresAt`), and its connection states and stored bytes are read at the request. Live on every installation; it misleads only in the
hour after a change. `POST /api/v1/system/tasks/storage-summary/run` counts at once.

### H-2421 · The target of the copies is one worker's configuration

Component: the target of R-STOR-106 (Q-STOR-500). Subject: the blobs counted below the target. The worker that computes the summary counts
its own connections without `drain`; during a rolling update that adds, drains or removes a connection, another worker may hold another
list, so two snapshots an hour apart can count against two targets and the figures of one hour can be counted against a list that no
longer holds. Live only during such a rollout; the next summary after it counts against the new list.

### H-2422 · A connection's capacity is the operator's statement

Component: `capacity` of a connection's element (storage.md R-STOR-050, R-STOR-106, Q-STOR-503). Subject: what the storage page says of a
store's room. A store reports no capacity through S3, so the page shows the stored bytes against what the operator wrote; nothing checks the
statement against the provider's quota or the bucket's real limit, and nothing refuses an upload when the use passes it. A wrong statement
makes the page say "nearly full" too early or never. Live wherever `capacity` is set; dormant where it is empty, which states none.
