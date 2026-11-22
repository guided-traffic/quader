# Several object-store connections: who can read, change or delete a copy, and what Quader believes about one

Status: specified on 2026-11-22, not built ([storage.md](../spec/storage.md) R-STOR-050 to R-STOR-063, R-STOR-026, R-STOR-027,
R-STOR-035; [ha-and-helm.md](../spec/ha-and-helm.md) R-HA-072; [backup-and-restore.md](../spec/backup-and-restore.md) R-BAK-030;
[ADR-239](../decisions.md#adr-239--several-object-store-connections-every-blob-on-every-connection-a-row-per-copy-one-reconciliation-and-a-scrub-per-copy)).
Until it is built an installation has one connection, `storage.s3`, and what [privilege-footprint.md](privilege-footprint.md) and
[transport-encryption.md](transport-encryption.md) say of the object store describes it.

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
| The restore Job | a write credential for the restoring installation's connections | write the restored blobs into the first connection |
| A provider's operators, and each principal its policies admit | the bucket | read every object; replace or delete one, which Quader sees only by a read (below) |
| A holder of the database | the rows of R-STOR-052 | say that a copy is present or verified when it is not; a drain then deletes beside it (H-1256) |

## What Quader believes about a copy, and on what evidence

| Belief | Evidence | Where |
|---|---|---|
| A copy exists (`present`) | the store's answer to the write that made it, a size from `HeadObject` after a multipart or a copy, or an object found by the orphan sweep (adoption) | R-STOR-052, R-STOR-026 |
| A copy is the blob (`verified_at`) | Quader hashed what it wrote and the store refused a wrong payload checksum (the start check establishes whether it does, R-STOR-035), or a read to the end matched the digest | R-STOR-052, R-STOR-055, R-STOR-027 |
| A copy is damaged | only evidence about the object: "no such object", a wrong size, a read to its end with another digest; never a timeout or an open circuit | R-STOR-055, R-STOR-027 |
| A location belongs to this connection | the marker `_connection` names this installation and this id | R-STOR-051 |

- **A replaced object is found by a read.** Whoever can write a connection's bucket can replace an object; the next full read of that
  copy (a download, the scrub within `scrub_period`, a reconciliation stream) finds the digest wrong, marks the copy `corrupt` and the
  reconciliation rewrites it from another connection. A ranged read serves whatever the bucket holds until then
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
| Its journal objects forged or deleted | The replay reads every connection and replays every differing copy, flagged ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-054, [H-553](../spec/backup-and-restore.md#h-553--a-journal-object-can-be-overwritten-with-the-runtime-credential)) |

## What this does not cover

- [H-11](../spec/storage.md#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks): object-store principals
  bypass Quader's authorization and integrity checks; with several connections, at every connection's provider.
- [H-553](../spec/backup-and-restore.md#h-553--a-journal-object-can-be-overwritten-with-the-runtime-credential): a journal object can be
  overwritten with the runtime credential; with several connections, with any connection's.
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
operator, because `forget-connection` deletes no object. Live whenever a connection is unreachable during a collection run, or removed
while it holds copies. What makes it matter is content whose removal is a legal or security duty; the operator then runs the sweep by hand
(`POST /api/v1/system/tasks/orphan-sweep/run` with `minAge`) once the connection answers, or removes the objects at the provider.

### H-1255 · A store that applies a delete after acknowledging it is not proven harmless to a later write

Component: assumption A3 of R-STOR-035, per connection. Subject: a blob deleted by collection and uploaded again soon after. The software
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

### H-1257 · A holder of a connection's credential can keep replicas from starting through its marker

Component: the marker `_connection` (R-STOR-051). Subject: the availability of new replicas. Whoever can write a connection's prefix can
replace the marker with one that names another installation; every replica that starts afterwards refuses that connection as a
configuration error, and with the connection not draining it stops (R-STOR-060). Running replicas keep serving. Live for every holder of a
runtime credential of a connection; it adds nothing to what such a holder can do to the content itself, which is all of it. Repair: delete
the forged marker; the next start writes it again.
