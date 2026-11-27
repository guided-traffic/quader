# Storage

What the page Admin area › Settings › Storage shows of the stored content: which object stores are connected and whether they are reachable, whether every blob survives the loss of one store, what Quader encrypts, how the stored data are checked, what went wrong in a period, and the one setting of the page, the download links.

**Admin area › Settings › Storage** (`/admin/settings/storage`) is open to every holder of the capability `settings:read` (roles `admin`, `security-admin`, `auditor`). The connections themselves are set in the deployment's configuration, `config.storage.connections` ([Configuration](configuration.md)); the page shows them and changes none. A holder of `settings:write` (the role `admin`) has the page's acts: **Count now**, **Run the reconciliation now**, **Scrub now**, **Check again**, **Acknowledge** and the section **Download links**. None of them deletes content, drains a connection or touches a key; each is in the audit log.

**The period** beside the title (**24 h**, **7 d**, **30 d**, **90 d**, **1 year**; 30 days unless you choose another) sets what the tile Problems, the sections Integrity and Problems and the counts of Availability look back over. It stays in the address, so a link carries it. Quader keeps this history for `storage.history.retention`, 400 days by default ([Configuration](configuration.md)).

The page reads again every 30 seconds while you see it, and every 3 seconds while a run you started from it runs. The copies per blob, the scrub's coverage and the damaged copies come from the hourly storage summary (the task `storage-summary`); the line under the title says when it last counted, and **Count now** counts at once. The connections' states and the stored bytes are read at every refresh.

## The summary

Six tiles at the top, each with its state in one word and its figures:

| Tile | States | What they mean |
|---|---|---|
| **Connections** | **Healthy**, **Degraded**, **Unreachable** | Every connection reachable on every replica; one unreachable on some replica or one detached; none reachable |
| **Availability** | **Redundant**, **Catching up**, **At risk**, **One store** | Every blob on every connection; blobs below the target while the reconciliation copies them; blobs on one connection only while a connection is unreachable or the reconciliation is more than an hour behind; one connection only |
| **Encryption** | **Encrypted**, **Partly**, **Provider only**, **Off** | Quader encrypts every connection; some; none, but every store is asked for server-side encryption; neither |
| **Integrity** | **Verified**, **Checking**, **Behind**, **Repairing**, **Damaged** | Nothing damaged and every copy read whole within its period; the scrub has not read every copy yet; copies overdue; damaged copies are being repaired; a blob has no whole copy |
| **Problems · 30 d** | **None**, **Resolved**, **Open**, **Ongoing** | Nothing in the period; only problems that ended (and damaged copies you acknowledged); a damaged copy waits for its repair; a connection is unreachable or a blob unavailable now, or an outage, a refused upload or a read no copy served lasts or ended within the last hour. Under it, the worst problem of the period |
| **Stored** | | The stored bytes and the blobs, as the dashboard counts them |

Green stands only for a good state. **One store** and **Off** are no faults: an installation with one object store and no encryption by Quader is the plain setup. Every tile but Stored is a link to its section. The same tiles, with one figure each, are the card **Storage** of the [Admin dashboard](dashboard.md).

## Connections

One row per connection in the order of the Helm configuration (`config.storage.connections`), which is the order uploads try them and reads try their copies:

- **Connection**: its id, and under it the endpoint's host and `bucket/prefix`.
- **State**: **Active**; **Filling** with the share of the blobs it holds while the reconciliation copies them to a connection you added; **Draining** with the copies left; **Drained**; **Detached** with the blobs only it holds; **Unreachable on *m* of *n* replicas** while its circuit is open there; **Configured on *m* of *n* replicas** while a rollout runs old and new configurations side by side.
- **Role**: **Uploads 1. · Reads 1.** is its place in the list; a draining connection takes no upload. **Links** says whether clients get signed links to it (the section Download links), **Bastion** whether a Bastion reads it directly.
- **Region**.
- **Use**: the stored bytes against the `capacity` you stated in the connection's element, as a bar; **Nearly full** from 80 %, **Full** from 95 %. Without `capacity` the cell says **no capacity set**. Quader reads no capacity from the store and never refuses an upload for it.
- **30 d**: the connection's cells of the period as a thin strip, red where clients were refused and amber where Quader absorbed something; on a narrow screen it gives way, and the section Problems shows the same cells with their words.

The chevron before a connection's name opens every setting of its element with the setting's key, so you find the line to change: the endpoint, bucket and prefix, region, credential source (never a credential), path-style addressing, the checksum modes, batch delete, the storage class, server-side encryption with its KMS key's identifier, the scrub's period and speed, the capacity and whether Quader encrypts it; then what the start check found, and when it was first seen, filled, the current pass started and the last full pass ended. The settings are those of the replica that answered; a connection it does not configure shows only its bucket and prefix. Retired connections are one line under the table. How to add, drain and remove a connection is in [Monitoring and operations](operations.md#add-or-remove-a-connection).

## Availability

Quader keeps every blob on every connection without `drain`: that is the **Target**. Before an upload is answered, `storage.replication.min_copies` copies are written (**written before answering**); the reconciliation (the task `storage-reconcile`, every minute) copies the rest.

- **Copies per blob** is one bar: how many blobs have each number of copies. A part below the target is a warning, blobs without a copy an error.
- The line under it answers whether the installation survives the loss of one connection: **Survives the loss of any one connection**, or **Losing default: 1,234 blobs (12.0 GiB) unavailable** for the connection whose loss would cost most (the others on demand), or **One store: no copy elsewhere**.
- Per connection: **Holds** (the share of every blob with a whole copy there), **Only here** (blobs whose only copy lies there, with their size), **Backlog** (the reconciliation's queue for it and the age of the oldest gap, **Behind for over an hour** when `QuaderStorageReplicationBacklog` would fire), **Reconciliation** (when the current pass started and when the last full pass ended), and the period's counts: reads served **from another copy** because this one failed, writes taken by **the next connection**, and **copies failed** that the reconciliation could not make.
- **Run the reconciliation now** starts the task at once.
- **Blobs no connection holds whole**, a closed section that appears only while there is one: the first 20 with their digest, size, state (**Missing**, **Corrupt**), each copy's state and the first item that references them. Such a blob is not served until a copy returns: a later upload of the same file, a remote repository's refetch or the object's return. **Check again** reads every copy of the blob now, shows what it found and writes no object; each check is in the audit log as `storage.blob.rechecked`. Past 20, `quader admin storage verify` lists every one; `GET /api/v1/storage/damaged-blobs?scope=copies` lists the blobs with a damaged copy that another connection still holds whole, which the scrub and the reconciliation repair on their own.

## Encryption

Per connection that holds content:

- **Quader**: **On since** the date its first storage key was created, with the share of its copies written in Quader's own format (100 % on every encrypted connection; anything less is shown as **Differs** and is a fault to report) and **Keyed names**; or **Off**. Quader's encryption is set per connection with `encrypted: true` ([Configuration](configuration.md)); switching it on is a new connection that the reconciliation fills.
- **Object store**: **Configured** is the server-side encryption the store is asked for (`sse_mode`, with the KMS key's identifier); **Actual** is what the store reported for the probe object of the last start check (`AES256`, `aws:kms` with its key, or **not reported**), **not checked yet** before one. **Differs** warns when the store did not apply what it is asked for; **bucket default** says that the bucket encrypts on its own although Quader asks for nothing, which is no fault. Objects written before a change of `sse_mode` keep the encryption they were written with.
- **Storage keys**: the active key's identifier and generation, the retired and destroyed ones counted, and the whole list on demand. Identifiers only; the keys themselves never leave Quader's database, and their export is a command at a pod ([Runbook](runbook.md)).

**What Quader encrypts** lists every kind of stored content with a mark: the artifacts and the upload parts on each encrypted connection, and in the clear on a plain one (a plain connection beside an encrypted one keeps the content readable to its provider); the upload spool on the pod's disk, not encrypted; the secrets in the database (credentials, signing keys, storage keys), sealed under the root key; the rest of the database, not encrypted (encrypt its volume); a backup's artifacts as their connection stores them; a backup's database files, not encrypted by Quader; the revocation journal, the marker and the probes, plain on purpose, since they hold no content; a Bastion's cache, encrypted when its chart gives it a key. The help beside a line says why.

**Root key** is the key ring of the secrets in the database: the active key's identifier, the number of keys, **Rings agree** while every replica holds every key in use, and when the values were last sealed again (`secrets-reencrypt`). The ring keeps no date of a key's activation, so the page names the last re-sealing.

## Integrity

How each connection's data are checked, and what was found damaged in the period.

- **Checks**: **Digest at upload** (every upload's SHA-256, SHA-1, MD5 and SHA-512 are kept); **Download check** (a whole download is checked against the SHA-256 before its last bytes leave, `storage.download.verify`; **Download check off** when it is switched off); on a connection Quader encrypts, **Every read checked** since the date of its first storage key: Quader's format authenticates every 64 KiB of every read, ranges included, so a changed byte is never served; on a plain connection **Digest checks only**: whole reads and the scrub, not ranges. **Writes confirmed** says that the store refused a write with a wrong checksum at its start check.
- **Scrub**: the share of the connection's copies read whole within its `scrub_period` (30 days by default), the copies overdue, and when the scrub last ran and runs next. **Scrub now** starts the scrub for that one connection. The scrub replaces a damaged copy from another connection in the same run.
- **Damaged · 30 d**: the copies found damaged in the period with their size, and whether they are **all resolved**, some **open** or **acknowledged**. Reads refused because a copy names a storage key the replica does not know are counted here too; they mark no copy and point at the keys, not at the data.

**Findings** lists each damaged copy of the period, open ones first shown while there are any (**Open** | **All**): when it was found, on which connection, the blob's short digest with the first item that holds it, its size, what found it and why (**Scrub · Digest**, **Download · Not found**, or a step of Quader's format such as **Segment**), and its state:

| State | Meaning |
|---|---|
| **Open** | Not repaired yet; in red with **No other copy** when no other connection held the blob when it was found, so it is not served |
| **Acknowledged** | Open, and an administrator recorded that they know of it, with their note |
| **Repaired** | The copy was written again from another one |
| **Found again** | The object was there again without a repair (a store that answered wrongly for a while) |
| **Superseded** | A missing copy was then found corrupt, or the other way round; the next line is its successor |
| **Deleted** | The blob or the copy was deleted |
| **Resolved** | The hourly task found the copy whole again (after a restore, for example) |

Quader repairs a damaged copy by itself from another copy; a copy is never resolved by a click. The menu of an open finding has **Check again**, which reads every copy of the blob now and writes nothing, and **Acknowledge**, which records that you know of it with an optional note of up to 500 characters that every reader of the page sees. Acknowledging repairs nothing: the finding stays open until its copy is written again, found again or deleted, and the tile Problems stops counting it as needing an act. Each acknowledgement is in the audit log as `storage.finding.acknowledged`.

## Problems

What went wrong in the period, per connection:

- **The strip**: one cell per hour (24 h), six hours (7 d), day (30 d), three days (90 d) or week (1 year), counted back from now. A cell is red when clients were refused (an upload refused, a read no copy could serve, a blob without another copy), amber when Quader absorbed it (a connection unreachable on a replica, a failing start check, a damaged copy that was repaired, reads and writes that went to another copy, a copy the reconciliation could not make), empty otherwise. Point at a cell, or tap it, for what happened in it.
- **The list**, newest first: when it began, the connection, what it was (**Unreachable**, **Start check failed**, **Copies damaged**, **Uploads refused**, **Reads not served**) with its codes, how long or how much, and whether it is **Ongoing** or **Ended**. An outage that several replicas saw at once is one line with the number of replicas; the copies damaged on one connection in one day are one line. Reads and writes that went to another copy are no problem of their own: they are what Quader absorbed of an outage or a damaged copy, counted in the strip and in Availability.

A replica records an outage only once its connection's circuit opened, after five failures in a row; shorter hiccups are absorbed by the retries and not recorded. A replica that dies loses up to a minute of counts.

## Download links

The last section is the switch of the delivery by signed links of the object store, with its details; it is described in [Download links](download-links.md).

## In the API

`GET /api/v1/storage/overview?period=30d` answers the summary's states and figures, `GET /api/v1/system/storage-connections` (one connection: `…/<id>`) the connections, `GET /api/v1/storage/history?period=30d` the cells of the period, `GET /api/v1/storage/problems` and `GET /api/v1/storage/findings` the problems and the damaged copies of a period, page by page, `GET /api/v1/system/encryption` the root key's ring, `GET /api/v1/storage/summary` the whole hourly summary with every repository's logical size; each needs `settings:read`. `POST /api/v1/storage/findings/<id>/acknowledge` with `{"note": "…"}` acknowledges a finding and needs `settings:write`.
