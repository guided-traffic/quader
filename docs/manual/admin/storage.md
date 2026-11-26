# Storage

What the page Admin area › Settings › Storage shows of the stored content: which object stores are connected and whether they are reachable, whether every blob survives the loss of one store, what Quader encrypts, and the one setting of the page, the download links.

**Admin area › Settings › Storage** (`/admin/settings/storage`) is open to every holder of the capability `settings:read` (roles `admin`, `security-admin`, `auditor`). The connections themselves are set in the deployment's configuration, `config.storage.connections` ([Configuration](configuration.md)); the page shows them and changes none. A holder of `settings:write` (the role `admin`) has the page's acts: **Count now**, **Run the reconciliation now**, **Check again** and the section **Download links**.

The page reads again every 30 seconds while you see it, and every 3 seconds while a run you started from it runs. The copies per blob, the scrub's coverage and the damaged copies come from the hourly storage summary (the task `storage-summary`); the line under the title says when it last counted, and **Count now** counts at once. The connections' states and the stored bytes are read at every refresh.

## The summary

Six tiles at the top, each with its state in one word and its figures:

| Tile | States | What they mean |
|---|---|---|
| **Connections** | **Healthy**, **Degraded**, **Unreachable** | Every connection reachable on every replica; one unreachable on some replica or one detached; none reachable |
| **Availability** | **Redundant**, **Catching up**, **At risk**, **One store** | Every blob on every connection; blobs below the target while the reconciliation copies them; blobs on one connection only while a connection is unreachable or the reconciliation is more than an hour behind; one connection only |
| **Encryption** | **Encrypted**, **Partly**, **Provider only**, **Off** | Quader encrypts every connection; some; none, but every store is asked for server-side encryption; neither |
| **Integrity** | **Verified**, **Checking**, **Behind**, **Repairing**, **Damaged** | Nothing damaged and every copy read whole within its period; the scrub has not read every copy yet; copies overdue; damaged copies are being repaired; a blob has no whole copy |
| **Problems now** | **None**, **Open**, **Ongoing** | Nothing; copies wait for their repair; a connection is unreachable or a blob unavailable |
| **Stored** | | The stored bytes and the blobs, as the dashboard counts them |

Green stands only for a good state. **One store** and **Off** are no faults: an installation with one object store and no encryption by Quader is the plain setup. A tile with a section below is a link to it. The same tiles, with one figure each, are the card **Storage** of the [Admin dashboard](dashboard.md).

## Connections

One row per connection in the order of the configuration, which is the order uploads try them and reads try their copies:

- **Connection**: its id, and under it the endpoint's host and `bucket/prefix`.
- **State**: **Active**; **Filling** with the share of the blobs it holds while the reconciliation copies them to a connection you added; **Draining** with the copies left; **Drained**; **Detached** with the blobs only it holds; **Unreachable on *m* of *n* replicas** while its circuit is open there; **Configured on *m* of *n* replicas** while a rollout runs old and new configurations side by side.
- **Role**: **Uploads 1. · Reads 1.** is its place in the list; a draining connection takes no upload. **Links** says whether clients get signed links to it (the section Download links), **Bastion** whether a Bastion reads it directly.
- **Region**.
- **Use**: the stored bytes against the `capacity` you stated in the connection's element, as a bar; **Nearly full** from 80 %, **Full** from 95 %. Without `capacity` the cell says **no capacity set**. Quader reads no capacity from the store and never refuses an upload for it.

The chevron before a connection's name opens every setting of its element with the setting's key, so you find the line to change: the endpoint, bucket and prefix, region, credential source (never a credential), path-style addressing, the checksum modes, batch delete, the storage class, server-side encryption with its KMS key's identifier, the scrub's period and speed, the capacity and whether Quader encrypts it; then what the start check found, and when it was first seen, filled, the current pass started and the last full pass ended. The settings are those of the replica that answered; a connection it does not configure shows only its bucket and prefix. Retired connections are one line under the table. How to add, drain and remove a connection is in [Monitoring and operations](operations.md#add-or-remove-a-connection).

## Availability

Quader keeps every blob on every connection without `drain`: that is the **Target**. Before an upload is answered, `storage.replication.min_copies` copies are written (**written before answering**); the reconciliation (the task `storage-reconcile`, every minute) copies the rest.

- **Copies per blob** is one bar: how many blobs have each number of copies. A part below the target is a warning, blobs without a copy an error.
- The line under it answers whether the installation survives the loss of one connection: **Survives the loss of any one connection**, or **Losing default: 1,234 blobs (12.0 GiB) unavailable** for the connection whose loss would cost most (the others on demand), or **One store: no copy elsewhere**.
- Per connection: **Holds** (the share of every blob with a whole copy there), **Only here** (blobs whose only copy lies there, with their size), **Backlog** (the reconciliation's queue for it and the age of the oldest gap, **Behind for over an hour** when `QuaderStorageReplicationBacklog` would fire), **Reconciliation** (when the current pass started and when the last full pass ended).
- **Run the reconciliation now** starts the task at once.
- **Blobs no connection holds whole**, a closed section that appears only while there is one: the first 20 with their digest, size, state (**Missing**, **Corrupt**), each copy's state and the first item that references them. Such a blob is not served until a copy returns: a later upload of the same file, a remote repository's refetch or the object's return. **Check again** reads every copy of the blob now, shows what it found and writes no object; each check is in the audit log as `storage.blob.rechecked`. Past 20, `quader admin storage verify` lists every one; `GET /api/v1/storage/damaged-blobs?scope=copies` lists the blobs with a damaged copy that another connection still holds whole, which the scrub and the reconciliation repair on their own.

## Encryption

Per connection that holds content:

- **Quader**: **On since** the date its first storage key was created, with the share of its copies written in Quader's own format (100 % on every encrypted connection; anything less is shown as **Differs** and is a fault to report) and **Keyed names**; or **Off**. Quader's encryption is set per connection with `encrypted: true` ([Configuration](configuration.md)); switching it on is a new connection that the reconciliation fills.
- **Object store**: **Configured** is the server-side encryption the store is asked for (`sse_mode`, with the KMS key's identifier); **Actual**, what the store applied, says **not checked yet** in this version.
- **Storage keys**: the active key's identifier and generation, the retired and destroyed ones counted, and the whole list on demand. Identifiers only; the keys themselves never leave Quader's database, and their export is a command at a pod ([Runbook](runbook.md)).

**What Quader encrypts** lists every kind of stored content with a mark: the artifacts and the upload parts on each encrypted connection, and in the clear on a plain one (a plain connection beside an encrypted one keeps the content readable to its provider); the upload spool on the pod's disk, not encrypted; the secrets in the database (credentials, signing keys, storage keys), sealed under the root key; the rest of the database, not encrypted (encrypt its volume); a backup's artifacts as their connection stores them; a backup's database files, not encrypted by Quader; the revocation journal, the marker and the probes, plain on purpose, since they hold no content; a Bastion's cache, encrypted when its chart gives it a key. The help beside a line says why.

**Root key** is the key ring of the secrets in the database: the active key's identifier, the number of keys, **Rings agree** while every replica holds every key in use, and when the values were last sealed again (`secrets-reencrypt`). The ring keeps no date of a key's activation, so the page names the last re-sealing.

## Download links

The last section is the switch of the delivery by signed links of the object store, with its details; it is described in [Download links](download-links.md).

## In the API

`GET /api/v1/storage/overview` answers the summary's states and figures, `GET /api/v1/system/storage-connections` (one connection: `…/<id>`) the connections, `GET /api/v1/system/encryption` the root key's ring, `GET /api/v1/storage/summary` the whole hourly summary with every repository's logical size; each needs `settings:read`.
