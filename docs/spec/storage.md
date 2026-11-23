# Storage

Status: specification — not implemented

This page specifies how Quader keeps package content and the metadata that describes it: the
content-addressed blob store on one or several S3-compatible object-store connections (INV-4,
R-STOR-050), the PostgreSQL schema for
repositories, items, packages, versions, blobs, blob references and properties (INV-3), the upload
pipeline (spooling, single-pass hashing, size limits, multipart transfer, deduplication, deploy by
checksum), the download path (streaming, HTTP range and conditional requests, integrity
verification, delivery by signed links of the object store as a switch of the installation, R-STOR-014), item operations (information,
listing, delete, copy, move, properties), the trash can, garbage collection that is safe against
concurrent uploads, the integrity scrub and orphan sweep, quotas and capacity thresholds, retention
and cleanup policies, the S3 configuration and credential chain, several connections with a copy
of every blob on each and the reconciliation between them (R-STOR-050 to R-STOR-063), the encryption
of stored content per connection in a format of Quader's own (R-STOR-048, R-STOR-075 to R-STOR-091), the
consistency assumptions about the object store, the behavior while the object store is unavailable, which role runs each part
of the storage work across any number of replicas (R-STOR-046), and what storage contributes to
Quader's own backup and restore (R-STOR-047). Repository semantics (classes, virtual resolution,
pull-through) are in [repositories-and-proxy.md](repositories-and-proxy.md); the permission model
is in [authorization.md](authorization.md); the format plug-in interface, the job framework, the
Valkey usage map and the shutdown order are in [architecture.md](architecture.md); Helm values,
volumes, the platform backup layer and failure drills are in [ha-and-helm.md](ha-and-helm.md);
the backup job, its garbage-collection hold and the restore procedure are in
[backup-and-restore.md](backup-and-restore.md).

Covers: the catalog pages [storage-and-blobs.md](../features/storage-and-blobs.md) (`STOR`),
[retention-and-cleanup.md](../features/retention-and-cleanup.md) (`RET`) and
[artifacts.md](../features/artifacts.md) (`ART`, the content-handling entries), plus OPS-066 and
OPS-067 from [operations-and-monitoring.md](../features/operations-and-monitoring.md).

## Terms

| Term | Meaning on this page |
|---|---|
| Blob | One immutable byte sequence, identified by its SHA-256, stored once on each connection that holds it, as one object there. |
| Connection | One bucket and prefix on one S3-compatible endpoint, with its credentials; an installation has one to eight (R-STOR-050). |
| Copy | A blob's object on one connection, with its row in `blob_copies` (R-STOR-052). |
| Item | Anything addressable by repository (project key and repository key) and path: a file or a folder. A file item references exactly one blob. |
| Reference | A row in any table that points at a blob through a `blob_sha256` foreign key (file items, including items in the trash can; stored run reports; cached documents of other subsystems). |
| Package, version | The logical unit a format defines (an npm package and its versions, a NuGet package id and its versions). A version groups one or more file items. |
| Lead item | The file item whose timestamps, downloads and properties stand for its whole version in retention decisions (npm: the tarball; NuGet: the `.nupkg`). |
| Spool | Scratch space of one pod (an `emptyDir` or a generic ephemeral volume, never shared and never persistent, INV-13) that holds an upload for the duration of one request or job while it is hashed and before it is written to S3. |
| Upload session | The state of one blob uploaded over several requests: a PostgreSQL row and an S3 multipart upload, so that any replica continues it (R-STOR-011, INV-18). |
| Hold | A running backup's protection of the blobs it still has to copy from garbage collection ([backup-and-restore.md](backup-and-restore.md) R-BAK-007). |
| Deletion pause | The `system_state` flag of [ha-and-helm.md](ha-and-helm.md) R-HA-025 that stops every storage task from deleting until an operator resumes deletions. |
| Principal | A user, service account or CI identity as defined in [identity-and-authentication.md](identity-and-authentication.md). |

## Handover to other pages

Catalog entries of the three source pages that this page does not own:

| Catalog IDs | Owner | Reason |
|---|---|---|
| ART-006, ART-040, ART-041, ART-048, ART-049 | [api-and-ui.md](api-and-ui.md) | UI screens; they consume the APIs specified here. |
| ART-031 | [authorization.md](authorization.md) | Effective permissions of an item are an evaluation of the permission model. |
| ART-050, RMT-017, RMT-021, RMT-028, REPO-019, REPO-025, REPO-027 | [repositories-and-proxy.md](repositories-and-proxy.md) | Repository settings and remote-cache metadata semantics. This page specifies the storage mechanisms those settings drive (R-STOR-014, R-STOR-044, R-STOR-045). |
| OPS-065 | [operations-and-observability.md](operations-and-observability.md) | The task framework: schedules under `maintenance.tasks.<name>`, the task and job API, job visibility (R-OPS-019). The storage tasks run on it; their semantics, parameters and results are specified here (R-STOR-025 to R-STOR-028, R-STOR-045; Q-STOR-10). |
| SRCH-008 | no page yet | Checksum search; the indexes it needs are defined in R-STOR-003. |

Storage-facing hooks the format plug-in interface in [architecture.md](architecture.md) has to
offer, used throughout this page:

| Hook | Called | Purpose |
|---|---|---|
| `IngestOptions` | before an upload is spooled | Expected digests from the protocol (npm `integrity`, OCI digest), whether SHA-512 is needed (it is always computed, Q-STOR-2), media type. |
| `OnCommit` | inside the commit transaction of an upload and of a remote artifact fill (`CommitEvent.Origin`) | Create or update the package and version rows, mark the lead item, write format metadata. The event carries the `PackageWriter` bound to the transaction, which increments `packages.rev_seq` on every write of generic rows (once per package per transaction), and the format's data port (R-STOR-003, architecture.md R-ARCH-009). |
| `OnItemsRemoved`, and the optional `OnItemsRestored` | inside the delete transaction (milestone M2; reason `delete` only). The trash, move and restore transactions run them from P1: until the trash exists the item store has no restore hook and `OnItemsRestored`, an optional interface found when the format registers (`formats.ItemsRestorer`, Q-ARCH-256), is never called | Keep format indexes consistent (an npm packument no longer lists a removed version); the event names each removed item and the version it belonged to, and carries the `PackageWriter`, which increments `packages.rev_seq`. Formats with version tombstones write them on removal and remove a restored version's tombstone (R-STOR-022, R-STOR-023). The sync delete and every batch of the `items-delete` job raise it, in the removing transaction. |
| `RemoveVersion` (P1; not yet in the contract) | by retention and cap enforcement | Remove every item that belongs only to one version (an OCI tag with its manifest and exclusively owned children). It joins the contract as an optional interface with the retention it serves (R-ARCH-025, Q-ARCH-256); until then the contract has no such method. |
| `VersionGroup` (P1; not yet in the contract) | after a push, for repository caps | Name the group whose variants a cap counts (snapshot base version, image name). Same rule as `RemoveVersion`. |

## Blob store layout

### R-STOR-001 · Content-addressed object key layout
Priority: **MVP** · Covers: STOR-001

On each connection that holds it (R-STOR-052), a blob MUST be stored as exactly one object whose key is derived only from its
SHA-256 and that connection's prefix (on an encrypted connection with keyed names, from an HMAC of the SHA-256 under the connection's
name key, `<prefix>/keyed/…`, R-STOR-086; every reader takes the name from the copy row, R-STOR-062):

```text
<prefix>/sha256/<h0h1>/<h2h3>/<h0…h63>
```

`<h0…h63>` is the lowercase hexadecimal SHA-256 of the content; `<h0h1>` and `<h2h3>` are its
first and second byte. Example: content with SHA-256
`5be0c1d2…` *(example)* lies at `quader/sha256/5b/e0/5be0c1d2…`. With an empty prefix the key
starts at `sha256/` (no leading slash).

- The key MUST NOT contain any part of a project key, repository key, item path, file name or user input.
  Repository paths live only in PostgreSQL; the same blob serves any number of items in any
  number of repositories, of one project or of several: a remote repository of the same public registry in two
  projects stores a package that both fetched once ([Q-STOR-154](#q-stor-154--blobs-are-deduplicated-across-projects)).
- The two-level fan-out (65,536 leaf prefixes) is fixed and not configurable. It spreads requests
  over many key prefixes, which object stores use to partition request rates, and it lets the
  orphan sweep (R-STOR-026) work prefix by prefix and resume.
- Quader writes only these key families below `<prefix>/`:

  | Key family | Content | Lifecycle |
  |---|---|---|
  | `sha256/…` | Blobs | Created by the upload pipeline, deleted only by garbage collection (R-STOR-024) and the orphan sweep (R-STOR-026). |
  | `keyed/…` | Blobs on an encrypted connection with keyed names, in place of `sha256/` (R-STOR-086) | As `sha256/`. |
  | `_uploads/<session-id>/data`, `_uploads/<session-id>/tail-<writer>` | The S3 multipart upload of a multi-request upload session, and the session's tail of less than one part, one object per request that wrote it (R-STOR-011, P1) | Everything below `_uploads/<session-id>/` is deleted on finalize, abort or expiry; a superseded tail is deleted once its successor is recorded; the orphan sweep removes leftovers (R-STOR-026). |
  | `_probe/<replica-id>` | Startup and conformance probe objects (R-STOR-035) | Deleted after each probe. |
  | `_journal/revocations/<yyyy>/<mm>/<dd>/<id>.json` | Revocation journal entries ([ha-and-helm.md](ha-and-helm.md) R-HA-054), on every connection that is not draining (R-STOR-059) | Written by the shipping job (a re-ship writes identical content); never deleted by Quader; expired only by an operator lifecycle rule. |
  | `_connection` | The marker that names the installation and the connection a location belongs to (R-STOR-051) | Written once by the start check; never deleted by Quader. |

  Any other object below `<prefix>/` is foreign: Quader reports it and never deletes it. With several connections every family
  lies below each connection's own prefix (R-STOR-059).
- A backup copies only `sha256/` objects that its database snapshot references; the other key
  families are not part of a backup ([backup-and-restore.md](backup-and-restore.md) R-BAK-001).
  A backup target never lies below `<prefix>/`, and `<prefix>/` never lies below a backup target
  (R-BAK-002), so neither the orphan sweep nor a restore meets the other's objects.
- Objects are written with the bucket's default storage class unless
  the connection's `storage_class` names another (R-STOR-050). Object content type is
  `application/octet-stream`; response headers come from item metadata, never from object
  metadata.

Acceptance criteria:

- After `curl -fsS -H "Authorization: Bearer $QUADER_TOKEN" -T widget.bin https://quader.example.com/files/web/generic-local/tools/widget.bin`
  *(example)*, `aws s3api head-object --bucket quader-blobs --key quader/sha256/<h0h1>/<h2h3>/<hex>`
  succeeds, where `<hex>` is the output of `shasum -a 256 widget.bin`.
- Uploading the same file to a second repository and a second path creates no second object
  (`aws s3api list-objects-v2 --prefix quader/sha256/<h0h1>/<h2h3>/` lists it once).
- A path containing `../`, `%2F` or a key of another tenant has no effect on the object key (unit
  test over the key function: input is a 32-byte digest, nothing else).

### R-STOR-002 · Digest set per blob and per file
Priority: **MVP** · Covers: ART-008, STOR-004

- The upload pipeline MUST compute SHA-256, SHA-1, MD5 and SHA-512 of every blob in one pass over
  the bytes (R-STOR-004) and store all four on the blob row. SHA-256 is the identity; SHA-1 and MD5
  exist only for client compatibility and search and MUST NOT be used to decide that two contents
  are equal (deduplication, deploy by checksum).
- Each file item MUST additionally keep the digests the deploying client declared (*declared
  digests*), separately from the computed ones, together with a flag that records a declared
  digest that disagreed with the content (only reachable through sidecar uploads, R-STOR-010; a
  mismatching declaration on the upload request itself rejects the upload).
- Digests MUST appear in: the deploy response (R-STOR-007), item information (R-STOR-016), listings
  (SHA-256 and SHA-1 per file, R-STOR-017), download responses (`ETag` and `Repr-Digest`,
  R-STOR-012) and digest sidecar files (R-STOR-010).
- Clients declare digests with the `Repr-Digest` request field of
  [RFC 9530](https://www.rfc-editor.org/rfc/rfc9530.html) (`sha-256`, `sha-512`); SHA-1 and MD5
  declarations are accepted only through sidecar files and format protocols that carry them (npm
  `shasum`).

Acceptance criteria:

- For a deployed file, `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/items?path=…` returns `digests.sha256`,
  `digests.sha1`, `digests.md5` and `digests.sha512` equal to `shasum -a 256`, `shasum -a 1`,
  `md5sum` and `shasum -a 512` of the file.
- `curl -T f.bin -H "Repr-Digest: sha-256=:<base64 of a wrong digest>:" …` answers `409` and
  creates no item and no blob.

## Metadata schema

### R-STOR-003 · PostgreSQL schema for repositories, items, packages, blobs, references and properties
Priority: **MVP** · Covers: ART-001, STOR-001, STOR-002

The tables below are the storage core. Other pages add tables and columns through migrations
(INV-3, [architecture.md](architecture.md) R-ARCH-026); they MUST follow the reference rules at the end of this entry.

- `repositories` holds identity and class; the configuration document `config` belongs to
  [repositories-and-proxy.md](repositories-and-proxy.md) except its `storage` member, which this
  page defines (`maxUploadSize`, `quota`, `downloadLinks` of R-STOR-014, `checksumPolicy`).
- `items` holds files and folders. A file references one blob. Folders are rows so that they can
  carry properties and be created explicitly; implicit folders (created as parents of a file) are
  removed when their last child goes, explicit ones stay.
- Trashed items stay in `items` with `trashed_at` set; they are still references, so their blobs
  survive until the trash can is emptied (R-STOR-022). The path uniqueness index covers only
  untrashed rows, so a path can be trashed several times and redeployed.
- A local repository, and a virtual repository of a format whose indexes a job builds (the Debian format, R-ARCH-012 item 2), holds
  **generated items**: package-less items with `items.generated` set, written only by the publication port described below and
  never by a client. A virtual repository holds them in its own `items`, which is the exception to "a virtual repository stores no
  content" that Q-STOR-180 records; they count toward its usage and carry no package or version.
- The **publication port** (`items.Publisher`, [Q-STOR-182](#q-stor-182--generated-items-are-published-through-one-port-blobs-first-then-one-transaction)) writes the
  files of a generation: it stores every file's blob first, outside any transaction (R-STOR-005), then runs one transaction that
  upserts a generated item per file (an identical blob changes nothing but `modified_at`, which the caller sets, so two generations never
  share a `Last-Modified`), deletes the generated items it is told to remove, and runs the caller's own writes with the transaction. A
  commit that reports it was superseded rolls everything back; the stored blobs are left to garbage collection. Quota admission does
  not apply (Q-STOR-181).
- `packages` and `package_versions` are filled by format plug-ins in the commit transaction;
  `items.version_id` links files to their version and `package_versions.lead_item_id` names the
  lead item.
- `packages.rev_seq` is the package's revision sequence ([architecture.md](architecture.md)
  R-ARCH-009). The core reads it, without format code, for read-your-writes metadata (R-ARCH-012)
  and for the merged-metadata check of virtual repositories (R-ARCH-015); a format keeps no second
  counter of its own.
  - Every storage hook or format write that changes a package's rows increments its `rev_seq` in
    the same transaction: publish or upload, delete, trash restore, copy, move, import, retention
    removal and remote cache fill. A copy increments it in the target repository, a move in both
    (R-STOR-019). Conformance kit check 9 (R-ARCH-010) fails a write that forgets it.
  - `rev_seq` never decreases for a repository and package name. A `packages` row therefore stays
    when the package's last version is removed, trashed or purged, and goes only with its
    repository (`ON DELETE CASCADE`). A row without live versions is a package that does not exist
    for every protocol and API. Deleting and re-creating the row would restart the sequence at 1,
    and a rendition cached under an earlier value could then be served again.
- Paths are stored without a leading slash, with `COLLATE "C"`, so that byte order defines listing
  order and a subtree is a plain index range (`path >= 'a/b/' AND path < 'a/b0'`).

Path rules enforced on every write (protocol plug-ins may be stricter): valid UTF-8 in Unicode
NFC; at most 1,024 bytes; segments separated by `/`, each 1–255 bytes; no empty, `.` or `..`
segment; no control characters (U+0000–U+001F, U+007F) and no backslash. Violations answer `400`.

<details>
<summary>DDL sketch: core storage tables</summary>

```sql
CREATE TABLE repositories (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id  uuid NOT NULL REFERENCES project (id),   -- authorization.md R-AUTHZ-026: every repository is in one project
    key         text NOT NULL CHECK (char_length(key) BETWEEN 2 AND 64 AND key ~ '^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$'),  -- INV-5; unique within the project
    format      text NOT NULL,
    class       text NOT NULL CHECK (class IN ('local', 'remote', 'virtual')),
    config      jsonb NOT NULL DEFAULT '{}',   -- repositories-and-proxy.md; member "storage" defined here
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repositories_project_key UNIQUE (project_id, key)
);

CREATE TABLE blobs (
    sha256            bytea PRIMARY KEY CHECK (octet_length(sha256) = 32),
    sha1              bytea NOT NULL CHECK (octet_length(sha1) = 20),
    md5               bytea NOT NULL CHECK (octet_length(md5) = 16),
    sha512            bytea NOT NULL CHECK (octet_length(sha512) = 64),
    size              bigint NOT NULL CHECK (size >= 0),
    state             text NOT NULL DEFAULT 'live' CHECK (state IN ('live', 'deleting')),
    content_status    text NOT NULL DEFAULT 'ok' CHECK (content_status IN ('ok', 'missing', 'corrupt')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    state_changed_at  timestamptz NOT NULL DEFAULT now(),
    object_deleted_at timestamptz,             -- set by GC once the S3 delete has succeeded
    checked_at        timestamptz              -- last recheck of a blob without a present copy (Q-STOR-153, R-STOR-055)
);
CREATE INDEX blobs_sha1     ON blobs (sha1);   -- sidecars, checksum search
CREATE INDEX blobs_md5      ON blobs (md5);
CREATE INDEX blobs_deleting ON blobs (state_changed_at) WHERE state = 'deleting';
CREATE INDEX blobs_damaged  ON blobs (content_status) WHERE content_status <> 'ok';
-- where a blob's objects lie and what is known about each: blob_copies, storage_connections and blob_copy_queue (R-STOR-052);
-- content_status is derived from the copies there

CREATE TABLE packages (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id       bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name          text NOT NULL,               -- normalized by the format (npm: as published; NuGet: lowercase id)
    display_name  text NOT NULL,
    attributes    jsonb NOT NULL DEFAULT '{}', -- format-owned (npm dist-tags, NuGet owners …)
    rev_seq       bigint NOT NULL DEFAULT 1,   -- +1 on every change to the package's rows (R-ARCH-009)
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repo_id, name)                     -- also serves the rev_seq reads (R-ARCH-012, R-ARCH-015)
);
CREATE INDEX packages_name ON packages (name); -- from M4b, built CONCURRENTLY in a step of its own: lookup by name across repositories (vulnerability-scanning.md R-VULN-011, Q-VULN-24)

CREATE TABLE package_versions (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    package_id       bigint NOT NULL REFERENCES packages (id) ON DELETE CASCADE,
    version          text NOT NULL,            -- normalized (NuGet: normalized SemVer 2.0)
    display_version  text NOT NULL,
    lead_item_id     bigint,                   -- FK added below (cycle with items)
    attributes       jsonb NOT NULL DEFAULT '{}',
    created_at       timestamptz NOT NULL DEFAULT now(),
    created_by       uuid,                     -- principal id (identity-and-authentication.md)
    trashed_at       timestamptz
);
CREATE UNIQUE INDEX package_versions_live ON package_versions (package_id, version) WHERE trashed_at IS NULL;

CREATE TABLE items (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id            bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    path               text COLLATE "C" NOT NULL,
    parent_path        text COLLATE "C" NOT NULL,   -- '' for top-level entries
    kind               text NOT NULL CHECK (kind IN ('file', 'folder')),
    blob_sha256        bytea REFERENCES blobs (sha256),
    size               bigint,
    media_type         text,
    declared_sha256    bytea,
    declared_sha512    bytea,
    declared_sha1      bytea,
    declared_md5       bytea,
    declared_mismatch  boolean NOT NULL DEFAULT false,
    explicit_folder    boolean NOT NULL DEFAULT false,
    generated          boolean NOT NULL DEFAULT false, -- format-generated metadata document
    version_id         bigint REFERENCES package_versions (id) ON DELETE SET NULL,
    upstream_url       text,                           -- remote caches; further cache columns: repositories-and-proxy.md
    created_at         timestamptz NOT NULL DEFAULT now(),
    created_by         uuid,
    modified_at        timestamptz NOT NULL DEFAULT now(),  -- content or location changed
    modified_by        uuid,
    updated_at         timestamptz NOT NULL DEFAULT now(),  -- any change, including properties
    trashed_at         timestamptz,
    trashed_by         uuid,
    trash_operation    uuid,                           -- groups the items of one delete call
    CHECK ((kind = 'file') = (blob_sha256 IS NOT NULL)),
    CHECK ((kind = 'file') = (size IS NOT NULL))
);
CREATE UNIQUE INDEX items_live_path ON items (repo_id, path) WHERE trashed_at IS NULL;
CREATE INDEX items_children ON items (repo_id, parent_path, path) WHERE trashed_at IS NULL;
CREATE INDEX items_blob     ON items (blob_sha256);
CREATE INDEX items_version  ON items (version_id);
CREATE INDEX items_trash    ON items (trashed_at, repo_id, path) WHERE trashed_at IS NOT NULL;

ALTER TABLE package_versions
    ADD FOREIGN KEY (lead_item_id) REFERENCES items (id) ON DELETE SET NULL;

CREATE TABLE item_properties (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id  bigint NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    key      text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 255),
    value    text NOT NULL CHECK (char_length(value) <= 2400)
);
-- values may exceed the btree entry limit; index a hash and a prefix instead of the full value
CREATE UNIQUE INDEX item_properties_unique ON item_properties (item_id, key, md5(value));
CREATE INDEX item_properties_lookup ON item_properties (key, left(value, 256));

CREATE TABLE item_download_stats (
    item_id           bigint PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    downloads         bigint NOT NULL DEFAULT 0,
    last_download_at  timestamptz,
    last_downloader   uuid                     -- principal id; NULL for anonymous or erased principals
);

CREATE TABLE blob_gc_queue (                    -- no FK: the blob row may already be gone
    sha256       bytea PRIMARY KEY,
    enqueued_at  timestamptz NOT NULL
);

CREATE TABLE repository_usage (
    repo_id        bigint PRIMARY KEY REFERENCES repositories (id) ON DELETE CASCADE,
    logical_bytes  bigint NOT NULL DEFAULT 0,   -- sum of untrashed file sizes
    files          bigint NOT NULL DEFAULT 0,
    folders        bigint NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE storage_usage_totals (             -- one row: physical totals of all live blobs
    id             smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    physical_bytes bigint NOT NULL DEFAULT 0,
    blobs          bigint NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE storage_usage_deltas (             -- append-only, folded by the worker loop below
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id        bigint,                      -- NULL: physical (blob) delta
    logical_bytes  bigint NOT NULL DEFAULT 0,
    files          bigint NOT NULL DEFAULT 0,
    folders        bigint NOT NULL DEFAULT 0,
    physical_bytes bigint NOT NULL DEFAULT 0,
    blobs          bigint NOT NULL DEFAULT 0
);
-- no index besides the primary key: the fold takes rows in id order, and no request reads them
```

</details>

<details>
<summary>DDL sketch: reference guard and release triggers</summary>

```sql
-- Every table with a blob reference names the column blob_sha256, declares
-- FOREIGN KEY (blob_sha256) REFERENCES blobs (sha256), indexes it, and installs both triggers.

CREATE FUNCTION blob_ref_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.blob_sha256 IS NULL
       OR (TG_OP = 'UPDATE' AND NEW.blob_sha256 IS NOT DISTINCT FROM OLD.blob_sha256) THEN
        RETURN NEW;
    END IF;
    -- FOR SHARE conflicts with the FOR UPDATE lock garbage collection takes (R-STOR-024)
    PERFORM 1 FROM blobs WHERE sha256 = NEW.blob_sha256 AND state = 'live' FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'blob % is not live', encode(NEW.blob_sha256, 'hex')
              USING ERRCODE = 'AS001';      -- mapped to a retry by the commit protocol (R-STOR-005)
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION blob_ref_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO blob_gc_queue (sha256, enqueued_at)
    SELECT DISTINCT o.blob_sha256, now()
      FROM old_rows o
     WHERE o.blob_sha256 IS NOT NULL
    ON CONFLICT (sha256) DO UPDATE SET enqueued_at = EXCLUDED.enqueued_at;
    RETURN NULL;
END $$;

CREATE TRIGGER items_blob_guard BEFORE INSERT OR UPDATE OF blob_sha256 ON items
    FOR EACH ROW EXECUTE FUNCTION blob_ref_guard();
CREATE TRIGGER items_blob_release_del AFTER DELETE ON items
    REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION blob_ref_release();
-- UPDATE OF blob_sha256 (overwrite in place) uses a variant that compares old and new rows.
```

</details>

Reference rules (normative for every page that adds a reference to a blob):

1. The referencing column MUST be named `blob_sha256`, MUST carry a foreign key to
   `blobs(sha256)`, MUST be indexed, and the table MUST install the guard and release triggers.
2. No code path may insert a reference except through the guard; the guard is in the database, so
   a forgotten application check cannot create a reference to a blob that is being deleted.
3. Garbage collection derives the list of referencing tables from the PostgreSQL catalog
   (foreign keys whose target is `blobs`) inside each collection transaction (R-STOR-024). A table
   added by a newer migration is therefore honored even by an older replica during a rolling
   upgrade.
4. A migration test MUST fail when a foreign key to `blobs` lacks the index or either trigger.

Usage counters (`repository_usage` per repository, `storage_usage_totals` for the physical
totals) are maintained by statement-level triggers on `items` and `blobs` that append to
`storage_usage_deltas`. Writers never update a shared counter row, so concurrent uploads to one
repository from any number of `web` replicas do not contend on it (Q-STOR-12). The physical
totals count `live` blob rows: a row inserted as `live` adds to them, and a row leaving `live`
(R-STOR-024 step 3) subtracts; a row that the orphan sweep inserts as `deleting` (R-STOR-026)
never counts.

- **Fold.** Every `worker` replica runs the fold loop every `storage.usage.fold_interval` (5 s
  by default, R-STOR-046), and runs the next iteration at once while the previous one took a full
  10,000 rows, so a backlog cannot grow with the write rate. An iteration is a few statements on
  the main pool, so the short interval costs little. One iteration is one transaction:

  ```sql
  WITH d AS (
      DELETE FROM storage_usage_deltas
       WHERE id IN (SELECT id FROM storage_usage_deltas ORDER BY id LIMIT 10000
                    FOR UPDATE SKIP LOCKED)
      RETURNING repo_id, logical_bytes, files, folders, physical_bytes, blobs)
  SELECT repo_id, sum(logical_bytes), sum(files), sum(folders), sum(physical_bytes), sum(blobs)
    FROM d GROUP BY repo_id ORDER BY repo_id NULLS LAST;
  -- same transaction: upsert the repo_id rows into repository_usage in this order,
  -- then add the physical sums to storage_usage_totals; COMMIT
  ```

  Concurrent folds on several workers take disjoint delta rows (`SKIP LOCKED`) and update the
  counter rows in one order, so they neither count a delta twice nor deadlock
  ([deadlocks](https://www.postgresql.org/docs/current/explicit-locking.html#LOCKING-DEADLOCKS):
  "acquire locks on multiple objects in a consistent order"). No lease and no Valkey lock is
  involved; a worker drained mid-fold rolls back and its rows are folded by another one. The
  upsert joins `repositories`, so the deltas of a repository deleted meanwhile are dropped instead
  of failing every later iteration on the foreign key (physical deltas carry no repository and
  are unaffected). An iteration that fails is repeated at the next interval.
- **Reads.** Readers use the folded rows. Quota admission (R-STOR-029) reads one row by primary
  key per enabled limit, and the storage summary (R-STOR-028) reads them as they are; both lag by
  at most one fold interval plus one iteration. Neither adds the unfolded deltas: their number
  grows with the cluster's write rate, so summing them in every admission would make each
  upload's cost grow with the upload rate of all replicas. A reader that needs exact values
  (tests, the acceptance criterion below) adds the unfolded deltas in the same statement as the
  folded rows, so one snapshot covers both and a concurrent fold can neither hide a delta nor
  count it twice.
- **Recount.** `storage_usage_recount()` recomputes `repository_usage` and `storage_usage_totals`
  from `items` and `blobs` and empties `storage_usage_deltas`, in one transaction. It runs only
  where no writer runs: as a fix-up of a backup restore (R-STOR-047), because a backup exports
  only referenced blob rows and the physical totals would otherwise still count rows the restore
  never loads.

Acceptance criteria:

- `quader migrate` on an empty database creates the tables; the migration test lists every
  foreign key to `blobs` with index and triggers present.
- A property value of 2,400 four-byte characters can be stored and found by key and value.
- Two items with paths `a/b` and `a/b0` in one repository: a subtree query for `a/b` returns only
  `a/b` and its descendants.
- Deleting the only version of a package leaves its `packages` row in place with a higher
  `rev_seq`; publishing a new version afterwards raises it again rather than restarting at 1.
- Three `worker` replicas fold while 32 goroutines on three `web` replicas upload to and delete
  from four repositories for five minutes. One fold interval after the load stops,
  `repository_usage` equals the sums over untrashed file items per repository,
  `storage_usage_totals` equals the count and size of `live` blobs, and
  `pg_stat_database.deadlocks` has not grown.
- A repository is deleted while its items are removed in the background and the fold runs: the
  fold keeps folding, `storage_usage_deltas` is empty one fold interval after the removal ends,
  and no iteration fails twice in a row.

## Upload

### R-STOR-004 · Upload pipeline: admission, spool and single-pass hashing
Priority: **MVP** · Covers: STOR-019, STOR-001

Every write of content (raw `PUT /files/…`, npm publish, NuGet push, remote-cache fill, report
generation) MUST go through one pipeline:

```text
request ─▶ authorize (deploy, overwrite) ─▶ admission ─▶ spool + hash (one pass)
        ─▶ verify size and declared digests ─▶ commit protocol (R-STOR-005)
        ─▶ 201 to the client ─▶ remove spool file
```

1. **Admission.** Before reading the body the pipeline MUST reject:
   - a declared `Content-Length` above the effective maximum upload size with `413`
     ([RFC 9110 §15.5.14](https://www.rfc-editor.org/rfc/rfc9110#section-15.5.14)); the
     effective maximum is the smaller of `storage.upload.max_size` and the repository's
     `config.storage.maxUploadSize`;
   - an upload that would exceed a quota with `507` (R-STOR-029);
   - an upload for which the spool budget cannot be reserved, or whose spool volume has less than
     `storage.spool.min_free_bytes` free, with `503` and `Retry-After`.

   Rejecting before reading means no `100 Continue` is sent to clients that use
   `Expect: 100-continue`. Bodies without a declared length (chunked) reserve spool budget in
   increments while streaming. Such a body fails:
   - with `413` as soon as it passes the effective maximum upload size; `413` is reserved for that
     case;
   - with `503` and `Retry-After` when it cannot reserve its next increment, because the spool
     budget is exhausted or the spool volume has fallen below `storage.spool.min_free_bytes`. The
     spool file is removed and nothing is committed, as for any cut upload.

   The second status matters for clients that always stream. The NuGet clients send every push
   chunked and retry a 5xx after waiting for its `Retry-After`, which
   [formats/nuget.md](formats/nuget.md) R-NUGET-002 caps at 30 s on NuGet endpoints
   (R-NUGET-032). A `503` therefore delays a push on a full spool until budget is free, possibly on
   another replica, while a `413` would fail it for good with a message that blames the package.
2. **Spool.** Bodies with a known length up to `storage.spool.memory_threshold` MAY be buffered in
   memory; all others MUST be written to a new file in `storage.spool.dir`, created with mode
   `0600`, named by a random UUID. The spool is scratch space of one pod: an `emptyDir` or a
   generic ephemeral volume (one ReadWriteOnce or ReadWriteOncePod claim per pod, deleted with
   it), never shared between pods and never persistent (INV-13, [ha-and-helm.md](ha-and-helm.md)
   R-HA-035, R-HA-061). It holds data of the running request or job only and never holds
   acknowledged data. `web` and `worker` pods each have one (R-STOR-046).
3. **Hash.** While spooling, SHA-256, SHA-1, MD5 and SHA-512 MUST be computed in the same pass
   (one read of the stream, fanned out to the four hash states) and the byte count recorded.
4. **Verify.** After the body ends the pipeline MUST compare the byte count with
   `Content-Length` (mismatch: `400`) and every declared or protocol-supplied digest
   (`Repr-Digest`, npm `integrity`/`shasum`, OCI digest) with the computed one (mismatch: `409`,
   nothing stored).
5. **Commit.** R-STOR-005 writes the object if needed and commits the metadata. The client
   receives `201` only after the object is durable in S3 and the commit transaction has
   committed: a `201` always means "durable in the object store and visible".
6. **Cleanup.** The spool file and its budget reservation MUST be released on every exit path
   (success, error, client disconnect). At startup a replica MUST delete every file in its spool
   directory, because no spool file survives its request.
7. **No state between requests.** Nothing an upload leaves in the spool or in memory is used by a
   later request (INV-18): the next request of the same client may reach any replica. A protocol
   that sends one blob over several requests MUST use an upload session (R-STOR-011), whose state
   is in PostgreSQL and whose bytes are in S3.
8. **Shutdown.** At the shutdown deadline ([architecture.md](architecture.md) R-ARCH-024 step 5)
   an upload whose body is still arriving is cut: its spool file is removed, nothing is
   committed, and the client's retry reaches another replica. An upload whose body has arrived
   completely is not cut at the deadline: it runs on into the margin that R-ARCH-024 grants (at
   most 3 s of the grace period's 5 s margin; [architecture.md](architecture.md) P-11 bounds that
   work). If its object write has not finished by the end of that margin, it is cut before its
   commit in the same way, and its multipart upload is aborted. Once its commit transaction has
   started, it is neither cancelled nor cut before its status line, so a drain never makes a
   client retry a publish that committed, a retry that would meet its own version as a conflict
   (`409`). A request
   of an upload session (R-STOR-011) that is cut keeps the parts it has recorded and never aborts
   the session's multipart upload: it cancels its outstanding `UploadPart`, waits for the call to
   return, and releases its writer claim in one short transaction, so the client's retry continues
   the session on another replica at once instead of waiting for the claim to expire. A
   remote-cache fill still running 5 s before the deadline is cancelled before its commit, and its
   flight lock is released with `failed` (R-ARCH-024 step 4, R-ARCH-033).

Remote-cache fills use the same pipeline; whether the waiting client is served while the upstream
body is still being spooled is decided in [repositories-and-proxy.md](repositories-and-proxy.md).

Configuration:

```yaml
storage:
  upload:
    max_size: 10GiB           # default (proposed); must not exceed storage.spool.max_bytes
  spool:
    dir: /var/lib/quader/spool  # default (proposed)
    max_bytes: 50GiB          # default (proposed); budget shared by all concurrent uploads of a replica
    min_free_bytes: 2GiB      # default (proposed)
    memory_threshold: 1MiB    # default (proposed)
    warn_percent: 80          # default (proposed); metric and log warning above this budget use
```

Metrics (names per [operations-and-observability.md](operations-and-observability.md)
conventions): `quader_spool_used_bytes` (the budget that running uploads hold), `quader_spool_free_bytes` (free space of the spool volume),
`quader_storage_upload_rejections_total{reason}`.

Acceptance criteria:

- With `storage.upload.max_size: 10MiB`, `curl -T 11mb.bin …/files/web/generic-local/x.bin` answers `413` without the
  body being transferred (`curl -v` shows no `100 Continue`).
- Killing the client mid-upload leaves no spool file and no blob row; the spool budget metric
  returns to its previous value.
- With `storage.spool.max_bytes: 100MiB` and `storage.upload.max_size: 90MiB`, two concurrent
  chunked uploads of 80 MiB each (`curl -T - …/files/web/generic-local/a.bin < 80mb.bin`, which sends
  `Transfer-Encoding: chunked`): one answers `201`, the other `503` with `Retry-After` and leaves
  no spool file and no item; repeated afterwards, it answers `201`. A chunked upload of 95 MiB
  answers `413`.
- `npm publish` of a tarball whose `dist.integrity` was edited to a wrong value answers an error
  and stores nothing.
- A replica restarted with files in its spool directory starts with an empty spool directory. Only the
  files the spool creates are removed: regular files named by a random UUID, as R-STOR-004 creates them. Any
  other entry makes the start fail, and a `lost+found` directory is the one entry tolerated
  ([Q-STOR-16](#q-stor-16--what-the-spool-may-delete-at-start)).
- A test build delays the S3 write of a 500 MiB `curl -T` past the end of the shutdown margin and
  the serving pod is deleted: `curl` reports an error, no item row and no blob row exists, and
  `aws s3api list-multipart-uploads` lists no upload of that pod. Repeating the same `curl -T`
  through the Service answers `201`.
- `kubectl delete pod` of the `web` replica that receives a chunked upload's streamed `PATCH`
  (P1, R-STOR-011): the session's multipart upload still exists afterwards, and the client's
  status request and next `PATCH` through the Service are answered `204` and `202` without a `416`.

### R-STOR-005 · Blob commit protocol and deduplication
Priority: **MVP** · Covers: STOR-001, STOR-002, STOR-019

After hashing, the pipeline knows the SHA-256 and holds the content in the spool. The commit
protocol MUST guarantee: *a committed reference always points at a blob row in state `live` whose
object exists in S3*; with several connections, at least `storage.replication.min_copies` copies in state `present`, each written
before its row (R-STOR-053), and "write object" below writes to the first writable connections of R-STOR-053. It works together with
garbage collection (R-STOR-024) and needs no lock held across an S3 transfer.

Existence of a blob is decided by its PostgreSQL row, not by an S3 `HEAD` (INV-4 refined: the
row is the authority; an object without a row is an orphan, R-STOR-026; an object of a live blob without its copy row is
adopted by the orphan sweep of its connection).

```text
putDone := false
loop (at most storage.upload.commit_attempts times):
  BEGIN (READ COMMITTED)
  SELECT state, content_status FROM blobs WHERE sha256 = $h FOR SHARE
  case no row:
      if not putDone or now - putAt > min(storage.upload.commit_deadline, storage.gc.delete_settle / 2):
          ROLLBACK; write object (R-STOR-006); putDone, putAt := true, now; continue
      INSERT INTO blobs (…, state = 'live') ON CONFLICT DO NOTHING
      if no row inserted: ROLLBACK; continue          -- a concurrent uploader won; re-read
  case state = 'deleting':
      ROLLBACK; putDone := false                      -- a GC delete may still land on the key
      wait until the row is gone (≤ storage.upload.blob_wait_timeout); continue
  case state = 'live' and content_status <> 'ok':
      ROLLBACK; write object; mark content_status = 'ok' in its own transaction; continue
  case state = 'live':                                -- deduplication hit: no S3 write
  insert or update the item (guard trigger re-checks under the same lock),
  format OnCommit hook, properties, audit event, event outbox
  COMMIT
```

- **Several files in one transaction.** A request that writes more than one file (an upload and the files derived from it, a provenance bundle sent with a publish; Q-ARCH-228) runs the loop
  over the distinct blobs of all of them: each round locks the blob rows in digest order, a blob that needs an object write, a repair or a wait makes the round roll back, and the
  objects are written outside any transaction; when every row is `live` the one link step writes every item row, hook and audit row in that transaction. Two files of one content are
  one blob. A failure after an object write queues every written digest for the collector (R-STOR-024), as a failed single commit does.
- **Vulnerability evaluation, from M4b** ([vulnerability-scanning.md](vulnerability-scanning.md)
  R-VULN-011). When the link step links a content item to a package version (origin `Upload` or
  `Fill`) of a format with the vulnerability adapter, the same transaction evaluates that version
  after the format's `OnCommit`, with one indexed query on `vuln_affected (ecosystem, name)`, and
  inserts its findings, so a finding exists when the publish answers. It reads no blob and contacts
  no upstream (V-4 of R-VULN-028: p99 ≤ 5 ms added to the commit). Re-evaluations after an
  advisory change find stored versions by name across repositories through `packages_name`
  (R-STOR-003), which the unique key `(repo_id, name)` does not serve.
- The `FOR SHARE` lock is held from the state check to commit; garbage collection needs a
  `FOR UPDATE` lock on the same row to start a deletion, so it either sees the new reference or
  makes this transaction wait and then fail the guard.
- `putDone` is reset whenever a `deleting` row is seen: an object written before a collection
  claimed the row may have been deleted by it, so the object is written again after the row is
  gone.
- The insert of a new blob row is bounded by `min(storage.upload.commit_deadline, storage.gc.delete_settle / 2)`
  counted from the end of the object write. After that bound the object is written again before the insert, so
  an insert never follows a collection's delete that may still be in flight (R-STOR-024). `delete_settle` has a
  minimum of 1 s.
- `storage.upload.commit_deadline` bounds the time between a successful object write and the
  insert of a new blob row. The orphan sweep deletes only objects older than
  `storage.orphans.min_age` (R-STOR-026); the deadline MUST be less than half of that age, so an
  object this uploader wrote cannot be swept before its row exists.
- A failure after a successful object write and before commit (database error, client gone)
  leaves either an orphan object (swept later) or a `live` row without references (collected by
  the full sweep after the grace period); the pipeline SHOULD enqueue the digest in
  `blob_gc_queue` on that path so that the incremental collection finds it.
- After `commit_attempts` the request fails with `503` and `Retry-After`.
- The link step itself repeats when a concurrent writer changes the path or its ancestors between two of its statements: a delete
  removed an ancestor folder it had just found (it creates the folder again), or an upload took the path (it becomes an overwrite).
  It runs at most five rounds and then fails with `503` `unavailable` and `Retry-After` ("The path is being changed by concurrent
  requests; retry."). A round repeats only after another transaction committed such a change inside it, and the folders a round
  creates are its own until it commits, so the bound is reached only when other writers commit changes of the same ancestors five
  times within one link step.
- Overwrite: when an untrashed item exists at the path, the caller MUST hold `overwrite` in
  addition to `deploy` (INV-9), unless the existing item references the same blob, in which case
  the request succeeds as a no-op with `200` and needs only `deploy`. A replaced user-deployed
  file goes to the trash can when it is enabled (R-STOR-022, Q-STOR-5); a replaced `generated`
  item does not. Two concurrent uploads to one path serialize on the path index; the second is
  an overwrite.
- Item timestamps: `created_at` and `modified_at` are the commit time of the transaction, so the
  creation time is never later than the last-modified time.

Configuration:

```yaml
storage:
  upload:
    commit_attempts: 5         # default (proposed)
    commit_deadline: 1h        # default (proposed); must be < storage.orphans.min_age / 2
    blob_wait_timeout: 120s    # default (proposed); must exceed 2 × storage.gc.delete_settle (R-STOR-024)
```

Acceptance criteria:

- Publishing the same npm tarball to `npm-a` and `npm-b`
  (`npm publish --registry https://quader.example.com/api/npm/web/npm-b/`) causes exactly one S3
  `PutObject` (S3 request metric `quader_storage_s3_requests_total{operation="put"}` grows by one) and
  two item rows referencing one blob row.
- Race test (Go integration test against PostgreSQL and the per-change S3 store of
  [ha-and-helm.md](ha-and-helm.md) R-HA-051, run in CI): 32 goroutines upload
  the same content to random paths while other goroutines delete those items and garbage
  collection runs with `storage.gc.grace_period: 0s` in a loop for 10 minutes; afterwards every untrashed and
  trashed file item's blob is `live` and `aws s3api head-object` succeeds for each of them.
- An upload that finds the row in state `deleting` succeeds after the collection finishes and the
  object exists afterwards.

### R-STOR-006 · Transfer to S3: single request, multipart, thresholds
Priority: **MVP** · Covers: STOR-023

- Content up to `storage.object_store.multipart.threshold` MUST be written with one `PutObject`; larger
  content MUST use a multipart upload whose parts are read directly from the spool file (section
  readers on the file; no per-part memory buffers).
- Part size is `storage.object_store.multipart.part_size`, raised when needed so that the content fits into
  10,000 parts (rounded up to whole MiB). S3 limits as documented by AWS: parts of 5 MiB to 5 GiB
  except the last, at most 10,000 parts, objects up to 48.8 TiB
  ([multipart upload limits](https://docs.aws.amazon.com/AmazonS3/latest/userguide/qfacts.html)).
  S3-compatible stores may have lower limits; `storage.upload.max_size` must respect them.
- Each upload uses at most `storage.object_store.multipart.concurrency` parallel part requests; a replica
  uses at most `storage.object_store.multipart.max_concurrent_parts` in total.
- Because the digests are known before the transfer starts, every `PutObject` and every part
  SHOULD carry a payload checksum the store verifies (the connection's `payload_checksum`): SHA-256
  where the store supports additional checksums (AWS S3), otherwise `Content-MD5`. A store-side
  mismatch fails the transfer.
- On any failure the pipeline MUST call `AbortMultipartUpload`; the orphan sweep aborts uploads
  left behind by crashed replicas (R-STOR-026), and operators SHOULD also configure a bucket
  lifecycle rule that aborts incomplete multipart uploads after one day
  ([AWS lifecycle rule](https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpu-abort-incomplete-mpu-lifecycle-config.html)).
  The multipart upload of an upload session (R-STOR-011) lives at most
  `storage.uploads.session_max_age`, which stays below both that rule and the sweep's
  `storage.orphans.multipart_max_age`, because both count from the upload's initiation; neither
  can abort an open session.
- After `CompleteMultipartUpload` the pipeline MUST confirm the object size with `HeadObject`
  before the row may become `live`.
- With several connections everything above applies per connection, with that connection's `payload_checksum`, checksum modes and
  storage class, and `max_concurrent_parts` counts the parts of all connections of a replica (R-STOR-050, R-STOR-053); the keys below
  are `storage.object_store.multipart.*` and the connection's `payload_checksum`.

Configuration:

```yaml
storage:
  connections:
    - payload_checksum: sha256  # default (proposed): sha256 | md5 | none; per connection
  object_store:
    multipart:
      threshold: 100MiB         # default (proposed)
      part_size: 16MiB          # default (proposed); minimum 5MiB
      concurrency: 4            # default (proposed); parallel parts per upload
      max_concurrent_parts: 64  # default (proposed); per replica
```

Acceptance criteria:

- A 300 MiB generic upload produces one multipart upload with 19 parts (16 MiB parts) in the
  request log of the S3 store, and `aws s3api head-object` reports 314,572,800 bytes.
- Killing the replica in the middle of a multipart upload leaves an incomplete multipart upload
  that `aws s3api list-multipart-uploads` shows until the next orphan sweep, which aborts it.
- With `payload_checksum: none` on the connection against a store without additional-checksum support, uploads work.

### R-STOR-007 · Raw deploy and folder creation
Priority: **MVP** · Covers: ART-002

`PUT /files/{projectKey}/{repoKey}/{path}` deploys one file into a local repository, or into the default
deployment target of a virtual repository ([repositories-and-proxy.md](repositories-and-proxy.md)).

| | |
|---|---|
| Request | Body: the content. Optional `Repr-Digest` (sha-256, sha-512); optional `Content-Type` (stored as media type); optional properties (R-STOR-009); `Authorization` per INV-8. |
| Response | `201 Created` with `Location` (the `/files` URL) and the item description below; `200` for an identical re-upload (R-STOR-005). |
| Permission | `deploy` on the path; additionally `overwrite` when replacing different content. |
| Errors | `400` invalid path or length mismatch; `401`; `403`; `404` unknown repository; `405` repository class does not accept deploys (remote, virtual without deployment target); `409` digest mismatch, or a format rule refuses the path, or a path conflict (`path_conflict`); an existing item with other content and no `overwrite` permission answers `403` (Q-STOR-17); `413` too large; `503` spool or object store unavailable; `507` quota exceeded. Bodies are problem details per [api-and-ui.md](api-and-ui.md). |

`PUT /files/{projectKey}/{repoKey}/{path}/` (trailing slash, empty body) creates an explicit folder and its
missing parents and answers `201`; an existing folder answers `200`.

Response sketch:

```json
{
  "repository": "web/generic-local",
  "path": "tools/widget/2.3/widget-2.3.bin",
  "kind": "file",
  "size": 48213,
  "mediaType": "application/octet-stream",
  "created": { "at": "2026-10-28T09:14:03.117Z", "by": "user:alice" },
  "digests": { "sha256": "5be0c1d2…", "sha512": "…", "sha1": "…", "md5": "…" },
  "declaredDigests": { "sha256": "5be0c1d2…" },
  "downloadUrl": "https://quader.example.com/files/web/generic-local/tools/widget/2.3/widget-2.3.bin",
  "itemUrl": "https://quader.example.com/api/v1/projects/web/repositories/generic-local/items?path=tools%2Fwidget%2F2.3%2Fwidget-2.3.bin"
}
```

Format repositories accept raw deploys only where the format plug-in allows them (an npm
repository accepts tarballs only through `npm publish`); the plug-in answers `409` with a
reason otherwise. In this build a format accepts them exactly when its protocol is `/files/`
itself (the generic format): a deploy of a file or of a folder into a repository of any other
format answers `409` `conflict`, naming the format's protocol address, after the decision and
before the body is read, so no body is spooled and no `100 Continue` is sent
([architecture.md](architecture.md) Q-ARCH-223). A folder is refused as a file is: an empty folder
at a path the format writes (a version's tarball path) would block its next write there.

Acceptance criteria:

- `curl -fsS -u ci:$QUADER_TOKEN -T widget.bin https://quader.example.com/files/web/generic-local/tools/widget.bin`
  answers `201` with the JSON above; repeating it answers `200`.
- `curl -X PUT …/files/web/generic-local/empty-folder/` answers `201`; the folder appears in the
  listing (R-STOR-017) with no children.
- A `PUT` to a virtual repository without a deployment target answers `405`.

### R-STOR-008 · Deploy by checksum and checksum-first transfer
Priority: **P1** · Covers: ART-004, STOR-003

A client can create a file item from content Quader already stores without sending the bytes.

- **Explicit form.** `PUT /files/{projectKey}/{repoKey}/{path}` with an empty body, the header
  `Quader-Deploy-By-Checksum: ?1` (a structured-field boolean) and
  `Repr-Digest: sha-256=:<base64>:`. If a `live` blob with that SHA-256 and `content_status = 'ok'`
  exists **and the caller can read at least one untrashed item that references it**, the item is
  created through the normal commit transaction and the answer is `201` with the R-STOR-007 item
  description. Otherwise the answer is `404` with problem code `content_not_found`, telling the
  client to upload normally. A `sha-512` member, if present, MUST also match.
- **Transparent form.** A normal upload that carries `Repr-Digest: sha-256=…` and
  `Expect: 100-continue` MAY be answered `201` before the body is read, when the same conditions
  hold; RFC 9110 allows a final response instead of `100 Continue`
  ([§10.1.1](https://www.rfc-editor.org/rfc/rfc9110#section-10.1.1)). Without the conditions the
  server sends `100 Continue` and the upload proceeds; the received bytes MUST then match the
  announced digest (`409` otherwise).
- Only SHA-256 identifies content here. SHA-1 and MD5 announcements are not accepted for lookup
  ([RFC 9530](https://www.rfc-editor.org/rfc/rfc9530.html) lists both as deprecated; chosen-prefix
  SHA-1 collisions are practical).
- Readability is evaluated with the permission evaluator of [authorization.md](authorization.md)
  over at most `storage.deploy_by_checksum.max_candidates` referencing items; if none of them is
  readable the answer is `404`, exactly as for unknown content.
- Downloads get the same shortcut through conditional requests (`If-None-Match` with the
  SHA-256 `ETag`, R-STOR-012).

Permission: `deploy` on the target path (and `overwrite` when replacing).

Configuration:

```yaml
storage:
  deploy_by_checksum:
    enabled: true          # default (proposed)
    max_candidates: 100    # default (proposed)
```

Acceptance criteria:

- After uploading `widget.bin` to `generic-a`, a user with read on `generic-a` runs
  `curl -X PUT -H "Quader-Deploy-By-Checksum: ?1" -H "Repr-Digest: sha-256=:$(openssl dgst -sha256 -binary widget.bin | base64):" …/files/web/generic-b/widget.bin`
  and gets `201`; no S3 request is made.
- A user without read on `generic-a` gets `404` for the same request.
- `curl -T big.bin -H "Expect: 100-continue" -H "Repr-Digest: sha-256=:…:"` for stored content
  completes without transferring the body (`curl -v` shows `201` and no `100 Continue`).

### R-STOR-009 · Properties attached at deploy time
Priority: **P1** · Covers: ART-003

- A raw deploy MAY carry properties as matrix parameters directly after the repository key:
  `PUT /files/{projectKey}/{repoKey};ci.run=8812;git.sha=4f2a9c1/{path}` *(example)*. Each `key=value` pair
  becomes a property of the deployed file; a key repeated or a value list separated by `,` gives
  several values. Keys and values are percent-decoded once.
- Matrix parameters are recognized only in the repository-key segment, the second segment after `/files/`. Project and
  repository keys cannot contain `;` or `%` (INV-5, [architecture.md](architecture.md) R-ARCH-005), so the syntax is
  unambiguous: the keys end at the first `;`, and only the parameters after it are percent-decoded; file names that contain `;` stay legal and
  are never parsed. This position also serves build tools that template a deploy base URL and
  append the artifact path (Maven distribution URLs).
- Alternatively the header `Quader-Properties` carries a structured-field dictionary of string
  or inner-list values (`Quader-Properties: ci.run="8812", targets=("linux" "darwin")`
  *(example)*).
- Deploy-time properties are written in the commit transaction and follow the property model
  (R-STOR-020); they require `annotate` in addition to `deploy`. Without `annotate` the deploy
  fails with `403` and nothing is stored.

Acceptance criteria:

- `curl -T w.jar "https://quader.example.com/files/web/maven-local;build.number=17/com/acme/w/1.0/w-1.0.jar"`
  stores the file at `com/acme/w/1.0/w-1.0.jar` with property `build.number=17`.
- A file named `a;b=c.txt` uploaded to `…/files/web/generic-local/dir/a;b=c.txt` keeps its name and
  gets no property.

### R-STOR-010 · Client checksum policy and digest sidecar files
Priority: **P1** · Covers: ART-009

- Every repository has `config.storage.checksumPolicy`: `verify-client` or `trust-server`.
  Default (proposed): `verify-client` for the Maven-family formats, `trust-server` for all
  others.
- A request for `<file>.sha256`, `<file>.sha512`, `<file>.sha1` or `<file>.md5` through `/files`
  where no item with that exact path exists is a *sidecar* request:
  - `trust-server`: answer `200` with the lowercase hex digest from the blob row
    (`text/plain`).
  - `verify-client`: answer `200` only if the client declared that digest and it matched; `404`
    if it was never declared; `409` while a declared value disagrees.
- A `PUT` of a sidecar path where the target file exists is not stored as an item: the body is
  parsed as a hex digest and recorded as the declared digest of the target (match: `201`;
  mismatch: `declared_mismatch = true`, answer `201` under `trust-server`, `409` under
  `verify-client`). A sidecar uploaded before its file is kept in the short-lived PostgreSQL table
  `pending_sidecars` and applied when the file arrives (`storage.sidecars.pending_ttl`), whichever
  replica receives the file (INV-18).
- A mismatching `Repr-Digest` on the upload of the file itself is always rejected (R-STOR-004);
  the policy governs only declarations that arrive separately.

Configuration:

```yaml
storage:
  sidecars:
    pending_ttl: 1h      # default (proposed)
```

Acceptance criteria:

- `mvn deploy` against a `verify-client` Maven repository uploads `.sha1` and `.md5` files; a
  following `curl …/w-1.0.jar.sha1` returns the digest; a manual `curl -T wrong.sha1 …/w-1.0.jar.sha1`
  makes the same request answer `409`.
- In a `trust-server` generic repository, `curl …/widget.bin.sha256` returns the digest without
  any sidecar ever being uploaded.

### R-STOR-011 · Multi-request upload sessions
Priority: **P1** · Covers: quader-only

Some protocols upload one blob over several requests: the OCI chunked blob upload (see the OCI
format page once written) and resumable generic uploads. The Service spreads requests at random
(INV-12), so consecutive requests of one session reach different replicas, and a node drain may
end any of them. Between requests a session therefore keeps its state in PostgreSQL and its bytes
in one S3 multipart upload (INV-18, Q-STOR-11). No replica's spool or memory holds anything of a
session after a request ends, and Valkey holds nothing of it: losing a session would cost the
client a complete re-upload, more than the one retry that [architecture.md](architecture.md)
R-ARCH-032 allows for state in Valkey.

**State.** One `upload_sessions` row per session and one `upload_session_parts` row per completed
part hold:

- the repository the session writes to, the repository the client addressed and the format's binding (the OCI image name),
  which every request must repeat: a request for another repository or binding answers as for an unknown session
  (Q-OCI-12); the principal, the received byte count and the serialized state of the four
  hash functions at that count. Go's hash implementations marshal and restore their internal state
  ([`crypto/sha256`](https://pkg.go.dev/crypto/sha256) and the SHA-1, MD5 and SHA-512 packages),
  so any replica continues hashing where the previous request stopped;
- the S3 multipart upload of the staging object `<prefix>/_uploads/<session-id>/data`, with the
  server-side encryption of R-STOR-032, on the session's connection (`connection_id`, R-STOR-054): a session stays on the connection
  where it started, with every object below `_uploads/<session-id>/` and the copy of its finalize. It is created with the first full part, by the request that
  sends it, and recorded with that part (`s3_upload_id` is empty until then), so a session that is
  started and never continued costs a row and nothing in the bucket;
- the *tail*: the bytes after the last completed part, always fewer than
  `storage.uploads.part_size`, kept as *segments*: one object `<prefix>/_uploads/<session-id>/tail-<writer>`
  per request that wrote bytes to it, where `<writer>` is the claim token of that request, and one
  row of `upload_session_tails` per segment with its order, writer, size and SHA-256. Every request
  writes its segment under its own key, so a writer that lost its claim can never overwrite a
  segment that the session records ([Q-STOR-174](#q-stor-174--the-tail-is-a-list-of-segments-that-are-read-once-and-the-multipart-upload-is-created-with-the-first-part));
- the writer claim (`writer`, `writer_until`) and the state.

**A request that carries bytes** (an OCI `PATCH`, or the body of the closing `PUT`):

1. **Authorize.** The request's principal MUST be the session's principal, and `deploy` on the
   session's repository is evaluated again on every request, so a permission revoked during an
   upload ends it. A request of another principal answers `404`, as for an unknown session. The
   session ID is a random UUID (version 4) and appears in upload URLs, so it is not a secret.
2. **Claim.** A closing `PUT` that finds the session in `completing`, `assembled` or `done` is a
   retried finalize (below). Any other request that finds the session not `open`, or older than
   `storage.uploads.session_max_age`, answers `404` (OCI `BLOB_UPLOAD_UNKNOWN`), and an open
   session past that age is set to `aborted`; its objects are removed under *Expiry*. Every
   request checks the age at its claim, and the renewal before each part and before the tail is read refuses a session past it, so a
   session can never be continued past the multipart expiry of R-STOR-006 by more than the request that was running, however long
   collection runs are delayed or paused. Otherwise one short
   transaction sets `writer` to a fresh random token and
   `writer_until = now() + storage.uploads.writer_lease`, only if no other claim is unexpired and
   the request's first byte equals `received`. Otherwise the answer is `416` with the current
   range, which is what the OCI specification requires for a chunk out of order. A claim held by a
   replica that was drained is released at once (R-STOR-004 step 8); only a crashed holder's claim
   blocks the session until `writer_until`. No transaction stays open after the claim
   (R-ARCH-019).
3. **Stream.** The body is read first, through the restored hash states into the spool, in
   segments: the first is as long as is missing for the part that the recorded tail began, the
   next ones are parts of exactly `part_size`. A segment that completes a part is joined with the
   recorded tail segments, which are read back from S3 here and only here, each checked against its
   recorded size and SHA-256 (a mismatch, a longer object or a missing one fails the session); the
   tail's bytes are already in the hash state and are not hashed again. The claim is extended
   right before each part is sent, and the upload of the part has a deadline below the renewed
   lease, so that it cannot outlive its claim ([Q-STOR-175](#q-stor-175--a-part-upload-is-sent-under-a-freshly-renewed-claim-and-ends-before-its-lease-does)). Each full part is sent with
   `UploadPart` (with the payload checksum of R-STOR-006) under the next consecutive part number,
   as S3 requires when parts carry checksums
   ([multipart upload](https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html)).
   One transaction then records the part row (number, size, ETag, SHA-256), the new `received`,
   the hash state, `tail_size = 0` and no tail segments (the tail is now inside the part), the
   upload id when this part created the upload, `last_activity_at` and a renewed `writer_until`,
   conditional on this request's writer token. A request whose token no longer matches stops and
   answers `416`, or `404` if the session ended meanwhile.
4. **End of body.** The remaining bytes, fewer than are missing for a part, are written as this
   request's own tail segment `tail-<writer>` and nothing is read back. A tail that has the most
   segments (32) is merged with them into one segment instead, which reads it once. One transaction
   records `received`, the hash state, the tail's size, the segment row, `last_activity_at`, and
   releases the claim, conditional on the writer token; it runs under a context of its own, so that
   a drain does not cut a COMMIT in flight. Only after that commit does the request delete the
   segments a merge superseded. A request whose transaction failed with an error that leaves it
   unknown whether it committed deletes nothing; it deletes its own segment only when the failure
   is certain, the claim being lost. The response reports the
   received range (OCI `Range: 0-<received − 1>`).
5. **Limits.** A request that would raise `received` above the effective maximum upload size of
   R-STOR-004 answers `413` and aborts the session. A request occupies in the spool at most the part it
   completes (the tail joined with its own bytes) and the segment its own bytes made, and a request
   that completes no part occupies its own bytes only, whatever the chunk size.

**Finalize** (the OCI closing `PUT` with `digest`):

1. Any bytes in the request are handled as above. The computed SHA-256 MUST equal the `digest`
   the client names; on a mismatch the protocol's error is returned (OCI `DIGEST_INVALID`) and the
   session stays open. The change from `open` to `completing` takes the writer claim as in step 2
   and is refused with `416` while another request holds an unexpired claim; claims need state
   `open`, so no request writes bytes once a finalize has begun.
2. State `completing`: one continuation takes a short claim for the step, reads the tail segments
   back, checks them as in step 3, and uploads them joined as the last part,
   which S3 allows to be smaller than 5 MiB
   ([`EntityTooSmall`](https://docs.aws.amazon.com/AmazonS3/latest/API/API_CompleteMultipartUpload.html)
   applies to every other part), and `CompleteMultipartUpload` runs with the recorded parts, their
   ETags and checksums. A store refuses a part whose ETag differs from the recorded one
   (`InvalidPart`) or assembles the part that is named: the conformance check of R-STOR-035 pins
   it, and a part sent by a request that had lost its claim is also kept from the store by the
   renewal and the deadline of step 3. A session without a full part has no multipart upload: its
   single tail segment is its staging object, the merge of several segments (put as `data`) or an
   empty object is.
3. State `assembled`: the commit protocol of R-STOR-005 runs, with a server-side copy of the
   staging object to the content address as its object write (`CopyObject` up to 5 GiB,
   `UploadPartCopy` above); a deduplication hit copies nothing. No byte passes through Quader.
4. The state becomes `done` in the transaction that links the item (the same transaction as step
   3's commit, [Q-STOR-171](#q-stor-171--a-session-ends-in-the-transaction-that-links-the-item)),
   every object below `_uploads/<session-id>/` is deleted after it, and the answer is `201`.

Each finalize step is recorded before the next starts, and each is idempotent. A finalize cut by
a drain (R-ARCH-024) is therefore repeated by the client's retry on any replica and continues at
the recorded state, without a claim: the retry's `digest` must equal the recorded one (otherwise
`DIGEST_INVALID`), its body is not read again, and a session already `done` answers `201`. A
`CompleteMultipartUpload` that answers `NoSuchUpload` in
state `completing` is resolved by a `HeadObject` of the staging object: an object of the session's
size means the earlier attempt completed. When two finalize requests of one session overlap (a
client retry while the first still runs), a step that fails because the other already advanced
the session (the staging object is gone, the state moved on) re-reads the state and continues
from there, so both answer `201` for the one stored blob.

**Resumption.** A request cut while its body arrives loses only the bytes after its last recorded
part, and `received` stays at its last recorded value. A request cut by a drain releases its claim
first (R-STOR-004 step 8). A client that asks for the upload status (OCI `GET` on
the upload location, answered `204` with `Range`) continues from there on any replica; one that
resends from its own offset gets `416` and asks
([OCI distribution specification](https://github.com/opencontainers/distribution-spec/blob/main/spec.md)).
The session start answers with `OCI-Chunk-Min-Length: <part_size>`, which that specification lets a
registry send and clients SHOULD honour; smaller chunks are accepted through the tail.

**Expiry.** A session without a recorded request for `storage.uploads.session_ttl`, or an open
session older than `storage.uploads.session_max_age`, is aborted by the next collection run
(R-STOR-025): state `aborted`, `AbortMultipartUpload`, every object below
`_uploads/<session-id>/` deleted; the run does the same S3 clean-up for sessions that a request
already set to `aborted`, and for a finished one once the replay window of 15 minutes has passed (when the request that finished it did not remove them), and then deletes
their rows ([Q-STOR-170](#q-stor-170--the-collection-run-aborts-a-live-session-for-its-idle-time-and-an-open-one-for-its-age)). Requests
enforce `session_max_age` themselves (step 2), so a delayed or paused collection run only delays
the clean-up. `session_max_age` MUST be shorter than `storage.orphans.multipart_max_age` and than
the bucket's lifecycle rule (R-STOR-006). Startup refuses a value not below `multipart_max_age`;
keeping the lifecycle rule at one day or longer is the operator's part, because the permissions of
R-STOR-031 do not let Quader read it. The orphan sweep (R-STOR-026) removes what an aborted run
left, on each connection, with `session_max_age` as the age of the staging objects and
`storage.orphans.multipart_max_age` as that of the multipart uploads
([Q-STOR-263](#q-stor-263--the-staging-sweep-moves-from-the-full-collection-to-the-orphan-sweep-per-connection-without-a-cursor-of-its-own)).

```sql
CREATE TABLE upload_sessions (
    id                uuid PRIMARY KEY,            -- random (version 4); appears in upload URLs
    repo_id           bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,   -- the repository the session writes to
    addressed_repo_id bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,   -- the repository the client addressed (a virtual one forwards to its deployment target)
    binding           text CHECK (length(binding) <= 512),   -- a string of the format, repeated by every request: the OCI image name
    principal_id      uuid,                        -- every request must come from this principal
    received          bigint NOT NULL DEFAULT 0,
    hash_state        bytea NOT NULL,              -- SHA-256, SHA-1, MD5, SHA-512 states at `received`
    part_size         bigint NOT NULL,             -- fixed when the session starts
    s3_upload_id      text NOT NULL,               -- multipart upload of _uploads/<id>/data; empty until the first part creates it
    tail_size         bigint NOT NULL DEFAULT 0 CHECK (tail_size >= 0 AND tail_size < part_size),   -- the sum of upload_session_tails.size
    writer            uuid,                        -- claim token of the request writing now
    writer_until      timestamptz,
    digest            bytea,                       -- SHA-256 named by the client at finalize
    state             text NOT NULL CHECK (state IN ('open', 'completing', 'assembled', 'done', 'aborted')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    last_activity_at  timestamptz NOT NULL DEFAULT now(),
    item_id           bigint,                       -- the item a done session committed; a repeated closing request is answered from it
    connection_id     text NOT NULL REFERENCES storage_connections (id)   -- the connection the session's objects lie on (R-STOR-054)
);
CREATE INDEX upload_sessions_live ON upload_sessions (last_activity_at)
    WHERE state IN ('open', 'completing', 'assembled');

CREATE TABLE upload_session_parts (
    session_id   uuid NOT NULL REFERENCES upload_sessions (id) ON DELETE CASCADE,
    part_number  integer NOT NULL CHECK (part_number BETWEEN 1 AND 10000),
    size         bigint NOT NULL,
    etag         text NOT NULL,
    sha256       bytea NOT NULL,                   -- payload checksum sent with the part
    PRIMARY KEY (session_id, part_number)
);

CREATE TABLE upload_session_tails (
    session_id uuid NOT NULL REFERENCES upload_sessions (id) ON DELETE CASCADE,
    seq        integer NOT NULL CHECK (seq >= 1),   -- the order of the segments
    writer     uuid NOT NULL,                       -- names the object _uploads/<id>/tail-<writer>
    size       bigint NOT NULL CHECK (size > 0),
    sha256     bytea NOT NULL CHECK (octet_length(sha256) = 32),   -- checked when the segment is read back
    PRIMARY KEY (session_id, seq)
);
```

All three tables have the backup export class `transient` (R-STOR-047): staged bytes are not backed up,
and a client restarts its upload after a restore.

Configuration:

```yaml
storage:
  uploads:
    part_size: 16MiB        # default (proposed); 5MiB to 5GiB (the largest part of an S3 upload); storage.upload.max_size must not exceed 10,000 parts
    writer_lease: 60s       # default (proposed); renewed right before each part is sent and with every recorded part; below session_ttl, and above the time one part takes to send (a part upload is cut at three quarters of it)
    session_ttl: 4h         # default (proposed); idle time after which a session is aborted
    session_max_age: 20h    # default (proposed); must be < storage.orphans.multipart_max_age and the lifecycle rule
    max_open_sessions: 100  # default (proposed); open sessions one principal holds on one repository; a start above it answers 429 (Q-STOR-176)
```

Acceptance criteria:

- A chunked upload whose three chunks are sent through three different replicas (the test pins
  each request to one pod) yields a blob whose SHA-256 equals the file's.
- A 2 GiB blob sent as one streamed `PATCH` while the receiving `web` pod is drained: the `PATCH`
  is cut, the upload status reports a multiple of `part_size`, the client resumes through the
  Service on another replica without receiving a `416`, and the closing `PUT` answers `201` with
  the right digest. Afterwards `aws s3api list-multipart-uploads` shows no upload of that session.
- A fault-injection build delays a `PATCH` after its body ended until its claim has expired and a
  second `PATCH` has continued the session; the first then writes its tail object and fails its
  transaction. The session's recorded tail is unchanged, and the closing `PUT` answers `201` with
  the right digest.
- A session older than `session_max_age` while the deletion pause keeps collection from cleaning
  up: its next `PATCH` answers `404` with `BLOB_UPLOAD_UNKNOWN`, the session is `aborted`, and the
  first collection run after deletions resume removes its multipart upload and objects.
- 100 chunks of 1 MiB each produce parts of 16 MiB (`aws s3api list-parts` during the upload) and
  a correct blob.
- Two concurrent `PATCH` requests for the same range: one is recorded, the other answers `416`,
  and the blob is correct.
- A closing `PUT` that is cut after `CompleteMultipartUpload` and repeated on another replica
  answers `201` and stores one blob.
- A session idle for longer than `session_ttl` is aborted by the next collection run; its
  multipart upload and staging objects are gone.
- A request with another principal's token on the session's URL answers `404`.

## Download

### R-STOR-012 · Streaming download, Range and conditional requests
Priority: **MVP** · Covers: ART-011

`GET` and `HEAD` on `/files/{projectKey}/{repoKey}/{path}` (and every format endpoint that returns stored
content) resolve the item (virtual and remote resolution:
[repositories-and-proxy.md](repositories-and-proxy.md)) and stream its blob from S3, from the first copy that answers in the order
of R-STOR-055.

- **Streaming.** The body MUST be copied from the S3 `GetObject` response to the client through
  a bounded buffer (`storage.download.buffer_size`), never spooled. Client disconnect cancels the
  S3 request. A download holds one buffer, taken from a pool and no larger than its object: the
  verifying reader of R-STOR-013 writes the cleared bytes from its own buffer, which is at least
  twice `storage.download.hold_back` ([architecture.md](architecture.md) Q-ARCH-241).
- **HEAD and conditional requests** MUST be answered from PostgreSQL alone, without an S3 call.
- **Response fields:** `Content-Length`; `Content-Type` from the item; `ETag: "<sha256 hex>"`
  (a strong validator: the content address); `Last-Modified` from `modified_at`;
  `Accept-Ranges: bytes`; `Repr-Digest: sha-256=:<base64>:`, plus `sha-512` when the request
  asks for it with `Want-Repr-Digest` ([RFC 9530](https://www.rfc-editor.org/rfc/rfc9530.html)).
- **Every blob body has a declared length.** Every response that carries a stored blob's bytes
  (`200`, and `206` with the length of the range) MUST carry `Content-Length` and MUST NOT use the
  chunked transfer coding. The size comes from the blob row, so it is known before the first byte
  is sent. A body that ends early, for example because a replica's shutdown deadline cut it
  ([architecture.md](architecture.md) R-ARCH-024 step 5, Q-ARCH-18), is therefore detectable by
  the client as incomplete and is never taken for the whole file. A remote-cache fill that the
  fetching replica streams to its own client while the fetch runs follows the withheld-tail rule
  of [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-041 instead.
- **User content is never rendered as part of Quader's origin.** Responses carrying stored
  content MUST include these headers:
  - `X-Content-Type-Options: nosniff`;
  - `Content-Security-Policy: sandbox; default-src 'none'`;
  - `Cross-Origin-Resource-Policy: same-site`;
  - `Content-Disposition: attachment; filename*=UTF-8''<percent-encoded file name>`.

  The core sets them, not the handler ([architecture.md](architecture.md) R-ARCH-007). The
  `/files/` handler and the format dispatcher wrap the response writer before the handler runs,
  and the wrapper checks the headers again at `WriteHeader`. A handler can set `Content-Type` but
  cannot remove or weaken these headers. `same-site` keeps other sites from loading stored content
  as a subresource, while the UI can still show package images with `img`. The optional content
  origin (R-ARCH-029, P1) changes this to `cross-origin` on the content host only.
- **Conditional:** `If-None-Match` (ETag) and `If-Modified-Since` answer `304`.
- **Range** ([RFC 9110 §14](https://www.rfc-editor.org/rfc/rfc9110#section-14)): a single byte
  range MUST be served with `206` and `Content-Range`, forwarded to S3 as a ranged `GetObject`;
  `If-Range` with the ETag or date is honored; an unsatisfiable range answers `416` with
  `Content-Range: bytes */<size>`. A request with several ranges is answered `200` with the full
  content (RFC 9110 permits ignoring `Range`).
- **Resuming on any replica.** Every replica MUST answer `Range` with `If-Range: "<sha256 hex>"`
  alike: `206` with the requested bytes when the ETag matches the item's current blob, `200` with
  the full content when it does not. The ETag is the content address and the item lookup is in
  PostgreSQL, so no replica needs state from the one that served the earlier part (INV-18). A
  download cut at one replica's shutdown deadline therefore resumes with `206` on whichever
  replica the client reaches next through the Service ([architecture.md](architecture.md)
  R-ARCH-024, Q-ARCH-18).
- **Caching:** `Cache-Control: private, no-cache` for authenticated requests, `no-cache` for
  anonymous ones; clients revalidate with the ETag.
- **Errors:** `401`; `403` no `read`; `404` no such untrashed item, and also for a stored item
  that its format refuses through the serve-time check of [architecture.md](architecture.md)
  R-ARCH-008. npm refuses the tarball of a tombstoned version, of a version a restore's journal
  replay withheld, and of a version removed upstream, on `/files` exactly as on its own tarball
  route ([formats/npm.md](formats/npm.md) R-NPM-003, R-NPM-015, R-NPM-026, R-NPM-047). The check
  runs before the `HEAD` and conditional answers, so neither a `200` to `HEAD` nor a `304` tells a
  client that the refused bytes are still stored. Problem code
  `content_unavailable` when the blob's `content_status` is not `ok`: `503` with `Retry-After` for
  `missing`, because the object may come back through a version restore, replication or a restore
  ([ha-and-helm.md](ha-and-helm.md) R-HA-024, [backup-and-restore.md](backup-and-restore.md)
  R-BAK-001), and `500` for `corrupt`; remote repositories instead treat the item as a cache miss
  and refetch, and a request that cannot refetch it is answered `503 content_unavailable`
  ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-025 "Lost content", Q-REPO-202). The first read
  that finds no object marks that copy `missing` and falls over to the next copy (R-STOR-055); the blob becomes `missing` when no
  copy is present any more ([ha-and-helm.md](ha-and-helm.md) R-HA-024, Q-STOR-151), so every later request is answered from
  PostgreSQL. The mark does not outlive the loss (Q-STOR-153): once the blob's last
  existence check (`checked_at`) is 30 s old, the next read asks the store again with one `HeadObject`, whichever
  replica (the read whose statement moves `checked_at` asks, every other read of the interval does not); an object
  that is back, unchanged in size, sets `content_status = 'ok'` and the read serves it, and one that is still gone
  starts the interval again. A lost object therefore costs the store one request per blob and 30 s, not one per read.
  `/files/` never contacts an upstream: an item of a remote repository answers the `503` there. `503` with `Retry-After` when the object store is unreachable (R-STOR-036).
- A `GET` on a folder returns the directory index (R-STOR-017).
- `?stats=false` suppresses the download statistics update (R-STOR-015).

Permission: `read` on the item (anonymous where granted).

Configuration:

```yaml
storage:
  download:
    buffer_size: 256KiB    # default (proposed)
```

Acceptance criteria:

- `curl -r 0-99 -o part …/files/web/generic-local/widget.bin` answers `206` with 100 bytes equal to
  `head -c 100 widget.bin`.
- `curl -I …` answers `200` with the headers above while the S3 request counter does not grow.
- `curl -H 'If-None-Match: "<sha256>"' …` answers `304`.
- `curl -r 5000000000- …` on a small file answers `416` with `Content-Range: bytes */<size>`.
- `curl -r 100- -H 'If-Range: "<sha256>"' …`, sent to each `web` pod in turn through
  `kubectl port-forward`, answers `206` with bytes equal to `tail -c +101 widget.bin` on every pod;
  with `If-Range: "<another sha256>"` it answers `200` with the full content. `curl -D - -o /dev/null`
  of the full file shows `Content-Length` and no `Transfer-Encoding: chunked`.
- An uploaded `evil.html` served from `/files` carries `Content-Disposition: attachment`, the
  sandbox CSP, `nosniff` and `Cross-Origin-Resource-Policy: same-site`; a browser opening the URL
  downloads it instead of rendering it.
- A test handler that deletes `Content-Security-Policy` and sets
  `Cross-Origin-Resource-Policy: cross-origin` before writing still sends the sandbox CSP and
  `same-site`.
- `npm install @acme/widget --registry https://quader.example.com/api/npm/web/npm-all/` and
  `dotnet restore` against the NuGet source download through this path.

### R-STOR-013 · Download integrity verification
Priority: **MVP** · Covers: quader-only

- For full-content `GET` responses streamed through Quader, the server MUST compute the SHA-256
  of the bytes it sends and hold back the final `storage.download.hold_back` bytes until the S3
  body has ended and the digest matches the blob's SHA-256.
- When the digest does not match, or the S3 body does not deliver exactly the blob's size, the
  server MUST NOT send the held-back bytes and MUST abort the response (HTTP/1.1: close the
  connection; HTTP/2 and HTTP/3: reset the stream). The client sees a truncated transfer, never a
  complete corrupted file.
- Only evidence about the stored object changes its status: an S3 response whose
  `Content-Length` differs from the copy's `stored_size`, or a body read to its end whose SHA-256 differs,
  MUST set that copy's state to `corrupt` (R-STOR-055); the blob's `content_status` becomes `corrupt`, with an audit event and the
  metric `quader_storage_blobs_damaged_total{status="corrupt"}`, when no other copy is present (R-STOR-052). A transport error while reading the body
  (connection reset, idle timeout, unexpected end of the body) aborts the response the same way
  but MUST NOT change the status, as for the scrub (R-STOR-027). A check that is on by default must
  not take content offline (R-STOR-012 answers `500` for a `corrupt` blob) because of a network
  fault.
- Ranged responses and presigned redirects (R-STOR-014) cannot be verified this way; see H-11.
- The check is on by default (Q-STOR-6). `storage.download.verify: false` disables it for
  replicas whose CPU, not their network, limits download throughput. The reads of the UI's shared asset store are verified
  whatever the setting says, because their bytes run in the browser as Quader's code
  ([api-and-ui.md](api-and-ui.md#q-ui-143--a-file-of-the-shared-store-is-verified-whatever-storagedownloadverify-says)).

Configuration:

```yaml
storage:
  download:
    verify: true          # default (proposed)
    hold_back: 64KiB      # default (proposed); at least 1 KiB: 0 would release bytes before the digest check
```

Acceptance criteria:

- Overwriting an object in the S3 store with different bytes of the same length (`aws s3 cp`
  directly into the bucket) and downloading the item with `curl --http1.1` makes `curl` report a partial
  transfer (exit code 18), and the blob shows `content_status = 'corrupt'`.
- A fault proxy that resets the connection between Quader and the S3 store in the middle of a download
  makes `curl` report a partial transfer, and the blob keeps `content_status = 'ok'`; the next
  download succeeds.

<a id="r-stor-014--presigned-download-redirect-per-repository"></a>

### R-STOR-014 · Delivery by signed links of the object store: the installation's switch
Priority: **P1** · Covers: STOR-032, STOR-033

Decided by the owner on 2026-11-22 (decision 11 of his working paper on the stored content, option (a);
[ADR-242](../decisions.md#adr-242--delivery-by-signed-links-of-the-object-store-one-switch-of-the-installation-where-a-link-costs-nothing)):
"Wir brauchen ein Setting um durch den Admin bestimmen zu lassen ob wir unsere Artefakte direkt oder durch signierte s3 Links
ausliefern lassen" and "Es gibt ein Flag um die Daten im s3 verschlüsselt abzulegen, dann müssen wir natürlich die Daten ausliefern
und können das nicht mehr durch s3 links machen". In his option: one switch of the installation in the admin area, off by default; it
applies only where a link costs nothing (the connection is not encrypted, the format's client checks a digest itself, no pace limit
applies to the caller, no Bastion is between, only `GET`); the minimum size, the link's life and the public host are details behind a
disclosure; a repository can opt out. *Changed on 2026-11-22 by [Q-LIM-99](usage-and-limits.md#q-lim-99--a-speed-limit-does-not-stop-a-link-the-budgets-are-charged-at-the-issue), decided by the development agent after the
owner left the open questions to it: a pace limit of the caller is no longer a condition; the hour and day budgets are charged at the
issue.* This block replaces the switch per repository it specified before
([ADR-045](../decisions.md#adr-045--presigned-download-redirects-as-a-per-repository-opt-in-p2), superseded). Built behind the API on
2026-11-22 (package 2 of [developer/download-links.md](../developer/download-links.md#work-packages)); the operations below, the settings page
and the repository's section on the screen on 2026-11-23 (package 3).

**As built** (2026-11-22, package 2): the row `download_link_settings` (migration 00108) with its store, its audit event and the replay
of a switch turned off ([internal/downloadlinks/store.go](../../internal/downloadlinks/store.go)), the replica's snapshot and its change
area `download_link_settings` ([snapshot.go](../../internal/downloadlinks/snapshot.go)), the validation of a write
(`downloadlinks.Validate`, [settings.go](../../internal/downloadlinks/settings.go)), the repository's member `storage.downloadLinks` with the
closed object `storage` ([internal/repository/storage.go](../../internal/repository/storage.go)), and the serve point of R-STOR-070. The
end-to-end driver (`driver links on|off`, [links.go](../../test/e2e/cmd/driver/links.go)) writes the row through the store as well.

**As built** (2026-11-23, package 3): the four operations are [download_links_api.go](../../internal/api/v1/download_links_api.go), their
answers `DownloadLinkSettings` and `DownloadLinkSwitch` of the OpenAPI document. `GET …/settings` reads the row from PostgreSQL and each
connection's state from the answering replica (`downloadlinks.Links.States`); a connection's `publicAddress` is the saved address and
`null` where its links name the endpoint, which `endpoint` holds
([Q-STOR-355](#q-stor-355--a-connections-publicaddress-in-the-answer-is-the-saved-one-null-for-the-endpoint)). `routes` walks the
registered formats' routes for `ClientDigest.Checks`, so a format that declares later appears in the answer and on the screens without a
change of either; `GET …/switch` reads the replica's snapshot and names those formats. `PATCH` applies the merge patch to the four members
(any other member answers `400` at its pointer), checks the result with `downloadlinks.Validate` against the replica's connections and host
rule, and writes it with the revision of `If-Match`; a stored address the write leaves as it is is not checked again, so the address of
a connection the configuration no longer has refuses no other change and stays in the row until a write sends `null` for it
(Q-STOR-355). The test link is `downloadlinks.Links.TestLink`
([testlink.go](../../internal/downloadlinks/testlink.go), R-STOR-074). The screens are R-UI-200 and R-UI-201.

**What the switch does.** A download that meets every condition of R-STOR-070 is answered `307 Temporary Redirect` to a link to the
stored object, signed for `GET` (R-STOR-072); the client fetches the bytes from the object store. Everything else is answered by Quader
as before (R-STOR-012, R-STOR-013): every document and index, `HEAD`, a conditional request, a range, a file below the minimum size,
every file of `/files/`, and every route not every client of whose format checks a digest itself (R-STOR-071).

**The settings** ([Q-STOR-280](#q-stor-280--the-switch-is-one-row-of-its-own-with-its-own-operations-as-the-malware-scans)):

| Setting (API) | Value of a new installation | Allowed | On the screen (R-UI-200) |
|---|---|---|---|
| `enabled` | `false` | | the switch |
| `minSize` | `1048576` (1 MiB) | 0 to 1,099,511,627,776 bytes (1 TiB) | **Details**, closed |
| `linkLifeSeconds` | `60` | 10 to 3,600 | **Details** |
| `publicAddresses` | none: each connection's link goes to the connection's own endpoint | per connection an `https` origin (R-STOR-074) | **Details**, one field per connection that can serve links, its endpoint shown as the value ([Q-STOR-281](#q-stor-281--the-public-address-for-links-is-a-setting-of-the-admin-area-per-connection)) |

`minSize`, `linkLifeSeconds` and `publicAddresses` are also the details of a handover to a Bastion (R-STOR-095,
[Q-STOR-342](#q-stor-342--the-details-of-the-installations-links-hold-for-a-handover)), which `enabled` does not govern: a handover is
switched per Bastion registration ([bastion.md](bastion.md) R-BAS-071).

- **Stored** in one row, as the malware scan's switch is (R-MAL-019, [00088_malware.sql](../../internal/meta/postgres/migrations/00088_malware.sql)
  lines 64 to 71):

  ```sql
  CREATE TABLE download_link_settings (
      id                 boolean     PRIMARY KEY DEFAULT true CHECK (id),
      enabled            boolean     NOT NULL DEFAULT false,
      min_size           bigint      NOT NULL DEFAULT 1048576 CHECK (min_size BETWEEN 0 AND 1099511627776),
      link_life_seconds  integer     NOT NULL DEFAULT 60 CHECK (link_life_seconds BETWEEN 10 AND 3600),
      public_addresses   jsonb       NOT NULL DEFAULT '{}',   -- connection name -> https origin; absent: the connection's endpoint
      version            bigint      NOT NULL DEFAULT 1,
      updated_at         timestamptz NOT NULL DEFAULT now(),
      updated_by         uuid        NULL REFERENCES principal (id) ON DELETE SET NULL
  );
  INSERT INTO download_link_settings (id) VALUES (true);
  ```

  Every replica reads it from a snapshot that a change area of its own, `download_link_settings`, refreshes within the bound of
  [architecture.md](architecture.md) R-ARCH-018 (2 s), so a download costs no query for it. The row is part of the database and of its
  backup like every settings row.
- **Read and changed** through the operations below (tag `download-links`,
  [Q-API-400](api-and-ui.md#q-api-400--the-signed-links-have-operations-of-their-own-under-apiv1download-links)). A change needs
  `If-Match` (`428` without, `412` stale) and records `storage.download_links.changed` with `before` and `after`. A change that switches
  the delivery off carries the journal flag ([authorization.md](authorization.md) R-AUTHZ-031), so a restore to an earlier point keeps
  links off; switching on and the details are not journaled.

  | Method | Path | What | Permission |
  |---|---|---|---|
  | GET | `/api/v1/download-links/settings` | `{"enabled", "minSize", "linkLifeSeconds", "connections": [{"name", "encrypted", "endpoint", "publicAddress", "usable", "reasons": [{"code", "detail"}], "handoverUsable", "handoverReasons": [{"code", "detail"}]}], "routes": [{"format", "route"}], "version", "updatedAt", "updatedBy"}`: every connection with whether it can serve links now and why not (R-STOR-074), and whether it can serve a handover to a Bastion and why not (R-STOR-096), and the routes whose clients check a digest (R-STOR-071) | `settings:read` |
  | PATCH | `/api/v1/download-links/settings` | a merge patch of `enabled`, `minSize`, `linkLifeSeconds` and `publicAddresses` (a connection's `null` returns it to its endpoint), `If-Match` | `settings:write` |
  | POST | `/api/v1/download-links/test` | `{"connection", "publicAddress"}` as entered, saved or not: a test link (R-STOR-074) | `settings:write` |
  | GET | `/api/v1/download-links/switch` | `{"enabled", "formats"}`: whether the switch is on and the formats with a route whose clients check a digest, for the repository's settings page (R-UI-201) | every signed-in caller |

- **Validation** of a write: `minSize` and `linkLifeSeconds` in their ranges; each public address as R-STOR-074 says (an `https` origin,
  not a host of Quader nor below one); a connection name the installation does not have is `422 invalid_value`.
- **The configuration** carries no key of links. `storage.s3.presign.endpoint`, `ttl` and `min_size` are removed; whatever the
  configuration of several connections becomes (stage 1 of the owner's topic, specified in parallel), it gains none. The chart's values
  never listed them ([values.yaml](../../deploy/helm/quader/values.yaml) lines 428 to 437).

**The repository's opt-out** ([Q-STOR-282](#q-stor-282--a-repository-opts-out-with-one-member-and-the-addressed-and-the-holding-repository-must-both-allow)):

```yaml
storage:
  downloadLinks: true    # default; false: no download of this repository is handed to a link, whatever the installation's switch says
```

- Accepted on every class (local, remote, virtual) of a format with at least one route whose clients check a digest (R-STOR-071), and
  answered `422 not_applicable` on any other repository, so that no accepted setting is ignored. An absent member is the default.
- It only takes away: with the installation's switch off it changes nothing.
- **Who:** every writer of the repository's settings ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-061), an Owner of
  the project included, in both directions. `false` records `repository.narrowed` with the member, journaled; `true` records
  `repository.updated`. A well-known repository of `upstream` is set by an administrator, as its other narrowing settings
  (R-REPO-069); a system repository keeps the default, and none of its routes declares a client's check.
- **`storage` becomes a closed object** with the members `maxUploadSize` and `downloadLinks`; any other member is
  `422 invalid_value` at its pointer, `storage.presignedDownloads` included, whose detail names this block
  ([Q-STOR-283](#q-stor-283--the-member-storagepresigneddownloads-is-refused-and-storage-is-a-closed-object)). Nothing rewrites a
  stored document: neither the OpenAPI document nor the UI ever knew the old member.
- **The screen** is a closed section of the repository's settings page, never part of a creation form (R-UI-201).

**How the two combine.** A download is handed to a link only when the switch is on, and neither the repository the client addressed nor
the repository that holds the item says `downloadLinks: false`:

- **A virtual repository**: the addressed virtual repository and the member that serves the item must both allow it. A team that wants
  every download through Quader sets it on the virtual repository it uses; a member that opted out stays out through every virtual
  repository.
- **A remote repository**: a hit of a stored item is a download like a local one; a miss is streamed from the upstream by the leader's
  stream ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-041) and is never a link; a remote with
  `cache.storeArtifacts: false` stores nothing and never serves one.
- **An encrypted connection** serves no link (R-STOR-070 step 12, R-STOR-074). This is what [R-STOR-048](#r-stor-048--client-side-encryption-of-stored-content)
  asks of the encryption (R-STOR-088).

Acceptance criteria:

- Integration (area `blob`, about 3 s, against the stack's object store): `PATCH /api/v1/download-links/settings` with
  `{"enabled": true, "minSize": 0}` by an administrator answers `200`, by an auditor `403`, without `If-Match` `428`; a `GET` of an OCI
  blob through another replica answers `307` within 2 s, with Valkey stopped as well, and the `Location` fetched by a plain HTTP client
  returns the blob's bytes; `{"enabled": false}` makes the next `GET` there `200` with the bytes within 2 s, and its audit event carries
  the journal flag (`TestR_STOR_014_TheSwitchReachesEveryReplica`). *As built: package 2 wrote the switch through the store
  ([Q-STOR-353](#q-stor-353--the-cases-of-package-2-write-the-switch-through-the-store-and-the-end-to-end-cases-are-named-after-their-legs-sets));
  since package 3 the case switches on and off through `PATCH` on the first replica and checks the `428` and an auditor's `403` first.*
- Unit (`internal/api/v1`): the settings carry their `ETag`, the defaults and the routes whose clients check (`oci.blob.get`,
  `ansible.artifact.get`, not `npm.tarball.get`); the switch names `ansible` and `oci` for every signed-in caller; a change without
  `If-Match` is `428`, with a stale one `412`, by a caller without `settings:write` `403`; values outside their ranges and an unknown
  connection are `422` at their pointers, another member `400`; a `null` address returns the connection to its endpoint; without the area
  the operations answer `503` (`TestR_STOR_014_TheOperations`).
- Unit (`internal/downloadlinks`): a new installation is off with 1 MiB and 60 s; a write outside the ranges, an address that is not an
  `https` origin or lies below a host of Quader, and a connection the installation does not have are refused; a merge patch's `null` returns
  a connection to its endpoint (`TestR_STOR_014_TheSettings`).
- Unit (`internal/repository`): `{"storage": {"downloadLinks": false}}` on `oci-local` by its Owner is stored and records
  `repository.narrowed`; `{"storage": {"presignedDownloads": true}}` answers `422` at `/storage/presignedDownloads`; `downloadLinks` on a
  NuGet repository answers `422 not_applicable` (`TestR_STOR_014_TheRepositorysOptOut`). *As built: the validation, the narrowing with
  its part `downloadLinks`, the Owner's write, the snapshot's reading, the replay and the well-known repository's rule each at their
  function, without a database; which formats take the member is the catalog's `ClientDigest`, derived from the routes by
  `formats.Register` like `Mirror`.*

### R-STOR-070 · The conditions of one download, in the order of the serve point
Priority: **P1** · Covers: STOR-032, STOR-033

The decision is made at the one serve point every stored item a format serves passes: `itemStore.Serve`
([ingest.go](../../internal/formathost/ingest.go) lines 387 to 444) and `edge.ServeItem` ([content.go](../../internal/api/edge/content.go)
lines 56 to 128), for a format's routes, a remote repository's hit and every tier of a virtual repository alike (the layering:
[architecture.md](architecture.md) Q-ARCH-570). **`/files/` is never a
link**, whatever the format of the repository ([handler.go](../../internal/api/rawfiles/handler.go) lines 264 to 296): its clients are
plain download tools and browsers, which check no digest (curl, R-STOR-071).

The steps, in this order. A step that does not hold leaves the download to Quader, which streams it as today; the steps that answer by
themselves say so.

1. **The route.** The matched route declares that its clients check a digest (`Route.ClientDigest.Checks`, R-STOR-071), and the method
   is `GET`. A `HEAD` is answered from PostgreSQL as always (R-STOR-012): a link signed for `GET` fails on `HEAD`, and the public
   registries answer `HEAD` themselves.
2. **Not through a Bastion.** The request carries no recognized Bastion token (`bastionmain.RecognitionFrom`,
   [recognition.go](../../internal/bastionmain/recognition.go) lines 46 to 47, set by the recognition stage before authentication). A
   Bastion does not follow a redirect ([client.go](../../internal/bastion/mainclient/client.go) lines 82 to 84), caches what it serves,
   and would hand its caller in the DMZ the main installation's object store. A Bastion's request may get a handover instead, which the
   Bastion reads itself and never hands on (R-STOR-095).
3. **Not a system read.** The scanners' reads (`systemRead`, [system_read.go](../../internal/formathost/system_read.go) lines 13 to 15)
   are streamed.
4. **The switch and the opt-out.** The installation's switch is on, and neither the addressed repository nor the repository that holds
   the item says `downloadLinks: false` (R-STOR-014).
5. **The content.** The blob's content state is `ok` and the format's serve-time check passed: the two steps `Serve` runs first today
   (`Usable`, `ServeCheck`), whose refusals answer as they do.
6. **Every gate decided and passed** ([Q-STOR-284](#q-stor-284--a-download-whose-gates-are-not-all-decided-and-passed-is-never-a-link)).
   The three gates are decided together before one wait as today (`serveChecks`, [signature_gate.go](../../internal/formathost/signature_gate.go)
   lines 63 to 97; R-SIG-069); a refusal and a wait that ends without a verdict answer as they do. A download that passes is handed to a
   link only when every gate that applies passed on a decided state:
   - malware ([malware-scanning.md](malware-scanning.md) R-MAL-013): the scanning is off, or the blob's verdict is `clean`, or the blob is
     released; never while it has no verdict yet, also where the gate is not strict and streams such a blob, and never for `unscannable`
     or `failed` without a release;
   - vulnerability block ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015): not blocked, and no wait for the data;
   - signature gate ([signatures.md](signatures.md) R-SIG-064): off for the repository, or passed.
7. **Conditionals** are evaluated from the item row: `304` and `412` are Quader's answers.
8. **No range.** A request with a `Range` header, with or without `If-Range`, is answered by Quader: `206`, `200` or `416` as R-STOR-012
   says ([Q-STOR-285](#q-stor-285--quader-answers-every-request-with-a-range-itself)).
9. **The size** of the item is at least `minSize`.
10. **No pace figure is a condition** ([usage-and-limits.md](usage-and-limits.md) R-LIM-040, [Q-LIM-99](usage-and-limits.md#q-lim-99--a-speed-limit-does-not-stop-a-link-the-budgets-are-charged-at-the-issue)): a
    caller whose budget key sets `cachedBytesPerSecond` gets a link as every other, in `enforce` and in `audit` alike; the transfer by a
    link is not paced, and its size is charged to the hour and day budgets at the issue (step 14).
11. **Admit.** The check before a download starts runs as for every download ([usage-and-limits.md](usage-and-limits.md) R-LIM-021):
    the hour and day windows and the parallel lease. A refusal is the `429` it is today, before any link.
12. **A connection** that is not encrypted, holds the blob, is not failing and can serve links (R-STOR-074). The decision asks the
    connection's breaker itself: signing is local work and bypasses it today ([guard.go](../../internal/blob/guard.go) lines 259 to 263).
13. **The credential and the signature.** The credential that signs lives at least 5 seconds longer (R-STOR-072), and signing succeeds; a
    signing error is counted (R-STOR-073) and the download streams.
14. **Issue**: the answer of R-STOR-072, counted and recorded as R-STOR-073 says.

Steps 1 to 10 read the request, the item row, the request's charge and the replica's snapshots: no statement and no round trip. Step 11
is the round trip a streamed download takes as well, and steps 12 and 13 are local. A download handed to a link takes no request slot of
the replica beyond its own answer, no copy buffer and no memory charge of the transfer.

**As built** (2026-11-22, package 2): steps 1 to 6 and 8 to 10 in `itemStore.linkOptions`
([download_links.go](../../internal/formathost/download_links.go)), called from `itemStore.Serve` after `serveChecks`, which reports whether
every gate passed on a decided state (`sgate.Serve.RunDecided`, [serve.go](../../internal/signatures/gate/serve.go),
[Q-STOR-350](#q-stor-350--a-gate-passed-on-a-decided-state-at-its-first-decision-a-download-that-waited-streams)); step 7 and step 11 in
`edge.ServeItem`, which asks `ServeOptions.Link` after `Admit` for a `GET` answered `200` and writes the `307`
([content.go](../../internal/api/edge/content.go)); steps 12 and 13 in `downloadlinks.Links.Sign` over the copies the item lookup read
([links.go](../../internal/downloadlinks/links.go)); step 10 reads no figure, and step 14's charge is `limits.Charge.ChargeLink`
([links.go](../../internal/limits/links.go)). A blob whose content state is not `ok` at step 5 is counted with the reason `connection`. An item
whose copies a recheck of a missing blob dropped (`items.Store.Usable`) reads them with one statement before it is signed: the one case of
a download by link that costs a second statement, as its stream would. `/files/` holds no input of the switch.

Acceptance criteria:

- Unit (`internal/formathost`, the in-memory object store with its signing and its `RoundTrip`, one table, under a second): with every
  condition met a tarball `GET` answers `307`; each condition taken away alone answers `200` with the bytes: a route without the
  declaration, a recognized Bastion request, the switch off, the holding member of a virtual repository opted out, a blob without a
  malware verdict under a gate that is not strict, a `Range`, a size below `minSize`, an open circuit, an encrypted connection (a fake of
  stage 1), a credential that expires in 3 s; a caller with `cachedBytesPerSecond` gets `307` in `enforce` and in `audit` (Q-LIM-99);
  `HEAD` answers `200` without a store request; `If-None-Match` with the content address answers `304`; a used-up `cachedBytesPerHour`
  answers `429` and no link (`TestR_STOR_070_TheConditionsOfALink`).
- Unit (`internal/api/rawfiles`): with the switch on, every file of `/files/` is streamed (`TestR_STOR_070_FilesIsNeverALink`). *As built:
the switch is no input of the package, so the test holds its source to it: no file imports `internal/downloadlinks` and no
`edge.ServeOptions` it builds sets `Link` or `Linked`.*
- Unit (`internal/signatures/gate`): a writer's pass of the signature gate (Q-SIG-211) is decided, and its trace
  (`signature.download_by_writer` and its count, Q-SIG-240) is written once by the decision, whether the download then goes by link or
  streams; a verified pass writes none (`TestR_STOR_070_AWritersReadIsDecidedAndRecordedOnceLinkedOrStreamed`).
- Integration (area `querybudget`): a download handed to a link costs the item lookup alone, one statement, as its stream does
  (R-ARCH-027; `TestR_ARCH_027_ADownloadCostsOneStatement`, its case of a signed link).

### R-STOR-071 · The format declares whether its clients check a digest
Priority: **P1** · Covers: quader-only

A link replaces Quader's check of the bytes (R-STOR-013) with the client's. That is no loss only where every client of the format
checks, so the format says it per route, in the format contract, and a format to come is covered without a decision of its own: the
owner's standing rule is that a decision taken at one format holds for every format. This sits beside the facts a format declares for a
Bastion ([bastion.md](bastion.md) R-BAS-058): those are format-wide and reach the Bastion in its handshake, this one is a fact of one
route and is read only at the main installation's serve point.

**As built** (2026-11-22, package 1 of [developer/download-links.md](../developer/download-links.md#work-packages)): `formats.Route.ClientDigest`
and `formats.ClientDigest` ([route.go](../../internal/formats/route.go)), the refusals of `Register` (`checkClientDigest`,
[formats.go](../../internal/formats/formats.go)), kit check 20 (`formattest.CheckLinks`,
[checks_links.go](../../internal/formats/formattest/checks_links.go); what it judges:
[Q-STOR-295](#q-stor-295--kit-check-20-judges-every-200-a-declaring-route-answers-to-a-get-against-the-stored-blob-and-a-skip-while-a-route-declares-is-refused)),
its two defects with the option `DeclaresClientDigest` in the test format
([Q-STOR-296](#q-stor-296--each-defect-of-check-20-takes-the-exemption-another-check-grants-its-route-so-that-it-fails-check-20-alone)), the three
declarations of the table below and the table test. The contract's version stays 10. The serve point of R-STOR-070 reads the declaration since package 2 (2026-11-22); the client families the
table names as still to be shown are shown by that package's end-to-end cases (R-STOR-074's last criterion), and a family that takes altered
bytes turns its route's declaration back. *Changed on 2026-11-22 by package 2: the npm leg's case showed Yarn 4 installing a tarball whose
object was altered, so `npm.tarball.get` declares nothing
([Q-STOR-354](#q-stor-354--yarn-4-installs-a-tarball-whose-object-was-altered-so-npms-tarball-route-declares-nothing)).*

- **The contract's addition** ([ADR-243](../decisions.md#adr-243--a-route-declares-whether-its-clients-check-a-digest-and-kit-check-20-holds-it);
  [Q-STOR-286](#q-stor-286--the-declaration-is-a-field-of-the-route)). `formats.Route` gains `ClientDigest`; the Go block of
  [architecture.md](architecture.md) R-ARCH-008 holds it, as the contract test holds that block to the code:

  ```go
  // ClientDigest says whether every client of the format compares the whole body of the route's answer with a
  // digest it knew before the request and refuses the bytes when they differ (R-STOR-071). Only such a route
  // hands a download to a signed link of the object store (R-STOR-070); the zero value is "does not check".
  type ClientDigest struct {
  	Checks   bool   // shown by the clients' source or by a test with the real client
  	Against  string // where the clients' expected digest comes from, e.g. "the digest in the path"
  	Evidence string // the source read or the test that shows it
  }
  ```

- **`Register`** (and so kit check 1) refuses a route that declares `Checks` and is not of the kind `Download` with the method `GET`, or
  whose `Against` or `Evidence` is empty.
- **Kit check 20** (`checks_links.go` of the conformance kit, beside kit check 19 of the Bastion's facts in `checks_bastion.go`, whose
  pattern it follows: a declared fact of the format, held by the kit over the format's own content cases): every route that declares `Checks` has a content case it serves with `200`
  and a body, through `Items().Serve` (a handler that writes the bytes itself never reaches the decision of R-STOR-070), and the answer's
  `ETag` is the content address of the body (`"<hex>"` or `"sha256:<hex>"`), so the bytes a client checks are the stored blob's. A
  defect of the test format each: `DeclaresUnservedBody`, `DeclaresUnaddressedBody`. It runs in every format's
  `TestR_ARCH_010_Conformance` without PostgreSQL. Unlike check 19 it has no fixture field for exemptions: a route whose answer cannot
  meet it does not declare.
- **The table test** `TestR_STOR_071_EveryFormatDeclaresWhetherItsClientsCheck` in [all_test.go](../../internal/formats/all/all_test.go)
  lists every registered format with the routes that declare `Checks`; a format added later, or a declaration changed, fails it until
  its row says so. A declaration is therefore a reviewed act with its evidence, never a default.
- **The rule of evidence.** A route declares `Checks` only when every client family the format supports checks: shown in the client's
  source, or by the case of R-STOR-070's end-to-end test with the real client in the format's own leg (a link whose object was altered
  in the bucket, which the client must refuse). A family that was not read declares "does not check" until a builder has shown it with
  the real client; a family that does not refuse the altered bytes turns the route's declaration back to "does not check" before it
  lands, and the finding goes to the owner.

What each format declares (the clients' behaviour read on 2026-11-22 in their source, by the research of the owner's topic):

| Format | Route | Declares | Against | Evidence | What is still to be shown with the real client |
|---|---|---|---|---|---|
| OCI | `oci.blob.get` | checks | the digest in the path | containerd checks on commit ("unexpected commit digest", `plugins/content/local/writer.go`, `(*writer).Commit`); the distribution specification asks clients to check (SHOULD) | CRI-O and the Docker Engine's older pull path; the leg's Docker Engine and Podman refused an altered layer in the case of 2026-11-22 |
| OCI | `oci.manifest.get.digest` | does not check | | the store's answer carries no `Docker-Content-Digest`, no public registry redirects a manifest, and what a client does with such an answer was not read | |
| npm | `npm.tarball.get` | does not check | | npm 12, pnpm 12 and Yarn 1 refused a tarball whose object was altered in the bucket; Yarn 4.18.1 installed it: its npm fetcher (`NpmSemverFetcher.fetchFromNetwork`) compares nothing with `dist.integrity`, and its lockfile's checksum is the one a first resolve wrote from the bytes it got (the case of 2026-11-22, [Q-STOR-354](#q-stor-354--yarn-4-installs-a-tarball-whose-object-was-altered-so-npms-tarball-route-declares-nothing)) | |
| Ansible Galaxy | `ansible.artifact.get` | checks | `artifact.sha256` of the collection version | `ansible-galaxy` compares the SHA-256 ("Mismatch artifact hash with downloaded file", `concrete_artifact_manager.py`, `_download_file`) and never sends its credential after a redirect | none: ansible-core 2.17 and 2.21 refused an altered archive in the case of 2026-11-22 |
| Ansible Galaxy | `ansible.role.archive` | does not check | | not read: a role carries no digest the client could hold | |
| NuGet | `nuget.flatcontainer.nupkg`, `nuget.flatcontainer.nuspec`, `nuget.flatcontainer.embedded`, `nuget.symbols` | does not check | | no check found; the client's own handling beyond .NET's `HttpClientHandler` was not read | the NuGet client |
| Debian | `deb.pool.get`, `deb.members.get`, `deb.flat.file`, `deb.dists.by_hash` | does not check | | apt checks a file against the signed index, but its hash path was not read | apt: a builder may add the case to the leg `apt` and then declare it |
| generic | (no routes; `/files/`) | never | | `curl` checks nothing; `/files/` is never a link (R-STOR-070) | |
| ClamAV definitions | `clamav.container.get`, `clamav.signature.get` | cannot declare: the routes are `Metadata` | | `freshclam` was not researched | |

Acceptance criteria:

- Unit (`internal/formats`, `internal/formats/formattest`): `Register` refuses a declaring `Metadata` route and a declaration without
  evidence (`TestR_STOR_071_RegisterRefusesADeclarationItCannotHold`); each defect fails check 20 and no other check, with the exemption
  another check grants its route (Q-STOR-296); OCI, npm, NuGet, Debian, Ansible Galaxy, the ClamAV definitions and generic pass it, in
  each format's `TestR_ARCH_010_Conformance` (`TestR_STOR_071_KitCheck20`).
- Unit (`internal/formats/all`): the table names `oci.blob.get` and `ansible.artifact.get` and no other route
  (`TestR_STOR_071_EveryFormatDeclaresWhetherItsClientsCheck`).
- End-to-end, one case in each leg of a format that declares `Checks`, constant in size (R-STOR-070's client case, below).

### R-STOR-072 · The link: signed for GET, its headers, its host and its life
Priority: **P1** · Covers: STOR-032

- **Signed** with the query parameters of AWS Signature Version 4 for `GET` of the object's key on the connection R-STOR-074 chose, by
  that connection's runtime credential (R-STOR-031), against its public address. `ObjectStore.Presign` takes what it signs as options
  (the life, the three overrides below, the address) and answers the link with its expiry
  ([Q-STOR-293](#q-stor-293--a-link-is-signed-by-the-connections-runtime-credential)). As built: `blob.PresignOptions` and `blob.Link`
  ([objects.go](../../internal/blob/objects.go)); the S3 adapter retrieves the credential once and signs with it, so the expiry and the
  signature agree ([client.go](../../internal/blob/s3/client.go)); the in-memory store signs with a key of its own and answers its links
  as a store does ([presign.go](../../internal/blob/blobtest/presign.go)).
- **The headers of the store's answer** ([Q-STOR-287](#q-stor-287--a-links-answer-carries-the-content-type-the-file-name-and-no-store-and-the-redirect-carries-the-four-content-headers)).
  The signed query asks the store for three of the answer's headers, the only kind of header a link can ask for:
  `response-content-type` (the content type Quader's own answer would carry: the item's), `response-content-disposition`
  (`attachment; filename*=UTF-8''<percent-encoded file name>`, as R-STOR-012) and `response-cache-control` (`private, no-store`).
  `X-Content-Type-Options`, the sandbox `Content-Security-Policy` and `Cross-Origin-Resource-Policy` of R-STOR-012 cannot be asked for;
  they guard Quader's origin, and the store's answer comes from a host that is no host of Quader (below). The object is stored as
  `application/octet-stream` (R-STOR-001), which no browser renders, also where a store ignores the overrides, and a connection whose
  link check fails serves no link (R-STOR-074). R-STOR-012's rule that every response carrying stored content has the four headers holds
  for every answer Quader writes; the redirect carries no content and carries them all the same.
- **`https` only.** The link's scheme is `https`; a connection whose public address is not `https` serves no link (R-STOR-074). Several
  clients follow a redirect from `https` to `http` (curl, npm, the clients written in Go), which would hand the signed query to the
  network.
- **The links' host** ([Q-STOR-288](#q-stor-288--the-links-host-is-no-host-of-quader-and-lies-below-none)). The host of the link as it is
  signed (with virtual-hosted addressing the bucket's name before the address's host) must not be a host Quader answers on, nor a
  subdomain of one. The clients written in Go keep their `Authorization` on a redirect to the same host or a subdomain of it
  (containerd and Podman do not strip it), and an object store refuses a signed link that arrives with a credential (`400`), so such a
  host would break every image pull by link. The hosts are those of `server.base_url`, `server.additional_base_urls`,
  `server.content_base_url`, every endpoint's `base_url`, `hosts` and the host names of its `host_roots`; a host that matches an
  endpoint's `wildcard_host`, or lies below its suffix, is refused too. Checked where the address is saved and at the start of every
  replica (R-STOR-074).
- **The life** ([Q-STOR-290](#q-stor-290--a-link-lives-60-seconds-unless-the-administrator-says-otherwise-at-most-an-hour)) is
  `linkLifeSeconds`, bounded by the credential that signs it: a link expires at the earlier of now plus the life and the
  credential's own expiry, where the credential carries one (temporary credentials, R-STOR-031); one that ends within 5 seconds signs no
  link, and the download streams. A static key that its provider lets expire on a date Quader is not told about can end a link early;
  the client then gets the store's `403` and asks Quader again.
- **The redirect** ([Q-STOR-289](#q-stor-289--the-redirect-is-a-307)): `307 Temporary Redirect`, `Location: <link>`,
  `Cache-Control: no-store`, `Content-Length: 0`, the content headers of R-ARCH-007, which the content wrapper sets on every answer of a
  content route, and none of `ETag`, `Last-Modified`, `Accept-Ranges` and `Repr-Digest`, which describe a body the redirect does not
  have.
- **The `Location` is a bearer** for the link's life (H-12): it is never logged (the request log keeps no header, and its path redaction
  covers `X-Amz-Signature`, `X-Amz-Credential` and `X-Amz-Security-Token`, R-OPS-010, R-OPS-013), never part of an error, never cached by
  Quader, and issued for one request only.
- **What the client then sees** is the store's answer: the bytes, the store's own `ETag` (no content address), `Content-Length`. A link
  fetched after its expiry is the store's `403`; a client that retries asks Quader's address again (a redirect is followed from the
  original request) and gets a new link or the stream. A download that is running when the link expires goes on: the store checks the
  expiry when the request arrives.

Acceptance criteria:

- Unit (`internal/blob/s3`): a link signed with a life of 60 s carries `X-Amz-Expires=60`, the three overrides in its signed query and
  the public address's host; with credentials that expire in 30 s, its expiry is at most 30 s away (`TestR_STOR_072_TheLink`).
- Unit (the package of the host rule): `objects.example.com` is accepted beside `quader.example.com`; `quader.example.com`,
  `s3.quader.example.com`, `blobs.packages.example.com` beside a `wildcard_host` `*.packages.example.com`, and the bucket host
  `quader.quader.example.com` of virtual-hosted addressing against an address `quader.example.com` are refused
  (`TestR_STOR_072_TheLinksHost`).
- Unit (`internal/api/edge`): the `307` carries `Location`, `Cache-Control: no-store`, `Content-Length: 0` and the four content headers,
  and no `ETag` (`TestR_STOR_072_TheRedirect`).
- Unit (`internal/blob`): a link lives its life in whole seconds, the earlier of it and the credential's expiry, and a credential that ends
  within 5 s signs none, in the S3 adapter and in the in-memory store alike (`blob.LinkLife`, `TestR_STOR_072_TheLinksLife`).
- Conformance (`checkPresign`, the job `s3-conformance` against Silo, milliseconds): the link's answer carries the overridden
  `Content-Type`, `Content-Disposition` and `Cache-Control`.

### R-STOR-073 · What a link counts and records
Priority: **P1** · Covers: quader-only

Everything is counted when the link is issued, not when the client fetches it. Quader does not learn whether, when or how often the link
was fetched, nor how many bytes arrived.

- **Usage** ([usage-and-limits.md](usage-and-limits.md) R-LIM-040): the request, as every request at stage 7, and the item's size as
  cached bytes of the budget key at issuance: in the hour and day windows at the next flush and in the usage store under the repository
  that served.
- **Download statistics** (R-STOR-015): one download of the item, `last_download_at` and `last_downloader`, as R-STOR-015 counts "a
  presigned redirect". A link fetched twice in its life counts once; a link never fetched counts.
- **The repository's activity** is touched as for every download.
- **Audit** ([Q-STOR-292](#q-stor-292--a-link-is-recorded-as-the-download-it-stands-for)): `artifact.downloaded`, under
  `audit.record_downloads` as every download ([operations-and-observability.md](operations-and-observability.md) R-OPS-016), with
  `delivery: "link"` and `linkExpiresAt` in `details` beside `path` and `sha256`. No event of its own.
- **Request log** ([operations-and-observability.md](operations-and-observability.md) R-OPS-010): status `307`, the new field
  `delivery` with `link` (`null` for every other request), `bytes_cached` the item's size, `response_bytes` `0`.
- **Metrics** (closed labels, R-OPS-007): `quader_storage_download_links_total{format}` (links issued),
  `quader_storage_download_link_bytes_total{format}` (their sizes), `quader_storage_download_links_declined_total{reason}` for a `GET` of
  a declaring route that was streamed while the switch was on (`bastion`, `opt_out`, `gate`, `range`, `size`, `connection`,
  `credential`, `signing`; no figure of the limits is a reason, Q-LIM-99), and the gauge `quader_storage_download_links_usable{connection}`, 1 while a connection can serve links on
  the replica (R-STOR-074). `quader_storage_s3_requests_total` counts no request for a link: signing is local.
- **The object store's own log**, where its operator turned one on, holds the request of the link with the object key, the time, the
  address the store saw and the signer: the connection's credential, never Quader's caller. Signing makes no request, so the store records
  no issuance (H-1278). An entry of that log joins Quader's audit event by the object key (the digest) and the link's life.

Acceptance criteria:

- Unit (`internal/formathost`): one link adds one download to the statistics buffer, the item's size to the charge's cached bytes, one
  `artifact.downloaded` with `delivery: "link"` when downloads are audited, and one to `quader_storage_download_links_total` of the format;
  a streamed `GET` declined for its range adds one to `..._declined_total{reason="range"}` (`TestR_STOR_073_WhatALinkCounts`). As built:
  the counting is `ServeOptions.Linked` ([download_links.go](../../internal/formathost/download_links.go)), the metrics
  [metrics.go](../../internal/downloadlinks/metrics.go), the request log's field `RequestInfo.SetDelivery`.

### R-STOR-074 · The connection that serves a link, its public address, its link check and the test link
Priority: **P1** · Covers: STOR-032

Several connections are stage 1 of the owner's topic, specified in parallel: a connection has the property `encrypted` and a state, and a
blob has a copy per connection. This block is written against those terms. Until stage 1 is built there is the one connection of today,
which is not encrypted ([R-STOR-048](#r-stor-048--client-side-encryption-of-stored-content) is not built) and which the answers name
`default`.

- **Which connection** ([Q-STOR-291](#q-stor-291--any-connection-that-is-not-encrypted-and-holds-a-sound-copy-serves-the-link)): the first
  connection in the read order of stage 1 that is not encrypted, holds a copy of the blob that is not marked damaged, is not failing (its
  circuit closed) and can serve links (below). None: the download streams, read as stage 1 reads.
- **The public address** of a connection is what a client reaches: an `https://host[:port]` origin without path, query or user
  information, set per connection in the admin area (`publicAddresses`, R-STOR-014); without one it is the connection's endpoint. The
  store has to accept a request signed for that host (its own name, or a proxy in front of it that keeps the `Host` header); that is the
  operator's network work, which the test link shows.
- **Can serve links** when the connection is not encrypted, its public address is `https`, the host of a link signed against it is no host
  of Quader nor below one (R-STOR-072), and its link check passed on the replica. Each replica decides it from its own configuration at
  start and at every refresh of the settings' snapshot; a replica that finds a connection unable logs it once per change (`warn`, the
  reason and the host it collides with) and sets the gauge of R-STOR-073 to 0. The settings' answer names the reasons per connection:
  `encrypted`, `not_https`, `quader_host` (with the host), `link_check_failed` (with what the store answered). Whether a connection can serve
  a handover to a Bastion is R-STOR-096: the same address rules and the first part of the link check, on encrypted connections too.
- **The link check** ([Q-STOR-294](#q-stor-294--each-connection-proves-at-start-that-a-link-with-the-overrides-works)): a check of the
  store's conformance (R-STOR-035) that every replica serving downloads runs for each connection at its start, whether or not the rest of
  the start-up check runs, and again when the switch is turned on: it writes a probe object of R-STOR-001's family `_probe/`, signs a
  link with the three overrides against the connection's endpoint, fetches it, and passes when the answer is `200` with the probe's bytes
  and the three overridden headers; then it deletes the probe. A store that refuses or ignores an override fails it. A failed link check
  takes the connection out of links on that replica and stops nothing else: the downloads stream. It records its two parts apart, the link
  works and the link honours the overrides, because a handover needs the first only (R-STOR-096,
  [Q-STOR-346](#q-stor-346--the-link-check-tells-whether-a-link-works-apart-from-whether-it-honours-the-overrides)). It is the extended `checkPresign` of
  [conformance.go](../../internal/blob/conformance.go), marked optional so that its failure fails no start.
- **The test link** ([Q-UI-952](api-and-ui.md#q-ui-952--the-test-is-a-link-the-administrator-opens-from-a-client-of-his-network)),
  `POST /api/v1/download-links/test` with a connection and an address as entered, saved or not: the API checks the address as a save
  would (`422`), answers `409 connection_unavailable` for a connection that is failing (*changed on 2026-11-22 by R-STOR-096: an
  encrypted connection is tested too, since a Bastion reads it by a link; its probe is plain*), writes the probe
  `_probe/link-test-<random>` with the 44 bytes `Quader download link test: this link works.\n` on that connection, and answers
  `{"url", "expiresAt", "command"}`: a link to it, signed against the entered address with a life of 60 s and the file name
  `quader-link-test.txt`, and the `curl -fsS '<url>'` that fetches it. The administrator opens it in his browser or runs the command on a
  client machine within the minute: what the store answers there is what his network gets. Quader sends no request to the address. The
  replica deletes the probe when the link's life is over; a probe that a stopped replica left is deleted by the next test, which removes
  every `_probe/link-test-` object older than an hour.
- **The store's own limits** apply to the transfers of links as well and are not Quader's to enforce: a provider limits requests,
  concurrent requests and bandwidth per account or project (H-1283).

**As built** (2026-11-22, package 2): several connections are built, and the choice reads the copies of the item lookup
(`blob.Connections.ServableOf`, [copies.go](../../internal/blob/copies.go)), so a link costs no statement. The link check is `blob.CheckLink`
([linkcheck.go](../../internal/blob/linkcheck.go)), its two parts `blob.LinkCheck.Works` and `Overrides`, recorded on the connection; it
runs on every replica with a client listener, in the background of its start, again when the switch turns on and at the snapshot's safety
period while it has not answered ([Q-STOR-351](#q-stor-351--the-link-check-runs-on-every-replica-with-a-client-listener-in-the-background-and-again-until-it-answers)).
Each connection's state, `Usable` with its reasons and `HandoverUsable` with its own, is `downloadlinks.ConnectionState`
([links.go](../../internal/downloadlinks/links.go)); the host rule is `downloadlinks.HostRule` ([hosts.go](../../internal/downloadlinks/hosts.go)).
A connection that cannot serve links is logged while the switch is on
([Q-STOR-352](#q-stor-352--a-connection-that-cannot-serve-links-is-logged-while-the-switch-is-on)). The test link is
`downloadlinks.Links.TestLink` ([testlink.go](../../internal/downloadlinks/testlink.go), package 3, 2026-11-23): it refuses an address as a
save would, writes the probe through the connection's own store without a payload checksum, signs the link with the overrides
`text/plain; charset=utf-8`, `attachment; filename="quader-link-test.txt"` and `private, no-store`, and deletes the probe with a timer when
the link's life is over; each test first deletes the probes of earlier tests older than an hour (at most ten pages of the listing). A
connection whose circuit is open, whose probe cannot be written or whose link cannot be signed answers `409 connection_unavailable`, and the
operation is not admitted in the read-only maintenance mode
([Q-STOR-356](#q-stor-356--a-test-link-of-a-connection-that-cannot-take-its-probe-is-409-and-the-test-waits-out-the-maintenance-mode)).

Acceptance criteria:

- Unit (`internal/api/v1`): the settings' answer names `quader_host` for an address below `server.base_url`, `not_https` for an `http`
  endpoint without an address, and `link_check_failed` for a store fake that drops `Content-Disposition`; a `PATCH` with such an address
  answers `422` at `/publicAddresses/default`; the test link answers a link signed against the entered address, and `409` while the
  circuit is open (`TestR_STOR_074_TheConnectionAndItsAddress`). *As built: the connections are three in-memory ones (`plain`, `insecure`,
  `dropping`), so the pointer is the connection's own; the test link's `422` at `/publicAddress` and at `/connection` is checked too.*
- Unit (`internal/downloadlinks`): the test link writes the 44-byte probe below `_probe/link-test-`, names the address as typed, lives
  60 s, serves the probe as `quader-link-test.txt`, and its probe goes when the life is over; a probe older than an hour went before; an
  encrypted connection is tested; an address a save refuses, an unknown connection and an open circuit are refused
  (`TestR_STOR_074_TheTestLink`).
- Unit (`internal/blob`): the link check fails against an in-memory store that ignores `response-content-disposition`, and the
  connection serves no link while the others' downloads go on (`TestR_STOR_074_TheLinkCheck`).
- Unit (`internal/downloadlinks`): the reasons per connection (`encrypted` for a fake of stage 2, `not_https`, `quader_host` with the host,
  `link_check_failed` with what the store answered) and the handover's own; the link from the first copy on a connection that can serve
  links, against its public address, with the three overrides; an encrypted connection, a copy marked damaged and an open circuit pass to
  the next copy or stream (`TestR_STOR_074_WhichConnectionsCanServeLinks`, `TestR_STOR_074_TheConnectionOfALink`).
- End-to-end, in the leg of each format that declares `Checks` (`oci`, `ansible`), one case with bytes to kilobytes: with
  `{"enabled": true, "minSize": 0}` the leg's clients fetch a fresh one-file artifact through a `307` (Docker and Podman;
  `ansible-galaxy`), and after its object was overwritten in the bucket with other bytes of the same length (a gzip stream's header time
  flipped, so the files are the same and only a digest tells the bytes apart) every one of them refuses it
  (`TestOCI_R_STOR_071_ClientsCheckALink`, `TestAnsible_R_STOR_071_ClientsCheckALink`, named after the prefixes their legs select by,
  Q-STOR-353). The npm leg's case turned npm's declaration back and was removed with it (Q-STOR-354).

### R-STOR-095 · The handover to a Bastion at the serve point
Priority: **P1** · Covers: quader-only

The owner's decision 12 (b) of 2026-11-22 ([bastion.md](bastion.md) R-BAS-070): for a forward of a Bastion, the main installation may
answer a download with a link of the object store and, for a copy on an encrypted connection, the copy's data key, instead of the bytes;
the Bastion reads the object itself. Built for plain copies (package 2 of
[developer/bastion-handover.md](../developer/bastion-handover.md)) and for encrypted ones, steps 8 and 10 (package 4), as built below. The decision is made at the serve point of R-STOR-070 (`edge.ServeItem`, through
`itemStore.Serve` for a format's routes and through `/files/`), in `internal/downloadlinks` beside the links'
([Q-STOR-340](#q-stor-340--the-handover-is-decided-in-the-package-of-the-links-the-one-caller-of-copydatakey-besides-the-store));
`edge.ServeItem` takes it as `ServeOptions.Handover`, which it calls after `Admit` and only for a `GET` that would carry the whole body.
A Bastion's request never gets a link of R-STOR-072 (R-STOR-070 step 2); this is its own answer. Every condition that does not hold streams
the download as today; the steps that answer by themselves say so.

1. **The offer and the switch.** The request is a recognized forward of a Bastion (`bastionmain.RecognitionFrom`, no probe) that carries
   `Quader-Bastion-Handover: accept; max-size=<n>` with a valid `n`, its method is `GET`, and its registration's `handover` is on in the
   replica's snapshot ([bastion.md](bastion.md) R-BAS-071, R-BAS-072).
2. **An artifact.** The serve is a stored file through the item store's serve of a route of the kind `Download`, or a file of `/files/`;
   never a document, a system read, a remote repository's miss (streamed by the leader, R-REPO-041) or an answer a format writes itself.
3. **The content and the gates**, as R-STOR-070 steps 5 and 6: the blob's content state is `ok`, the format's serve-time check passed, and
   every gate that applies passed on a decided state (Q-STOR-284); a refusal and a wait answer as they do.
4. **Conditionals and ranges** are answered by Quader as R-STOR-070 steps 7 and 8 say; a conditional request whose answer is the whole file
   (`200`) goes on.
5. **The size** is at least the links' `minSize` (R-STOR-014,
   [Q-STOR-342](#q-stor-342--the-details-of-the-installations-links-hold-for-a-handover)) and at most `n`.
6. **Admit** ([usage-and-limits.md](usage-and-limits.md) R-LIM-021): the hour and day windows and the parallel lease; a refusal is the
   `429` it is today. A speed figure (`cachedBytesPerSecond`) is no condition
   ([Q-STOR-343](#q-stor-343--a-speed-limit-does-not-stop-a-handover)).
7. **A copy** on a connection that can serve a handover (R-STOR-096): of the copies `blob.Store.Servable` lists (R-STOR-062), the first on
   a connection that is not encrypted, else the first on an encrypted one
   ([Q-STOR-341](#q-stor-341--a-plain-copy-is-handed-before-an-encrypted-one)). None: the download streams.
8. **The data key**, for a copy on an encrypted connection: `blob.Store.CopyDataKey` opens its record (one `HeadObject`, steps 1 to 6 of
   R-STOR-077, R-STOR-083); a failure streams, and the stream's read meets the copy and marks it as R-STOR-082 says. *Amended on
   2026-11-23 ([Q-STOR-471](#q-stor-471--the-link-is-signed-before-the-data-key-is-taken)):* it runs after step 9, so a download that
   streams for its credential or its signature takes no key and sends no `HeadObject`.
9. **The credential and the signature**, as R-STOR-070 step 13: the signing credential lives at least 5 seconds more, and the link is signed
   for `GET` of the copy's `object_key` against the connection's public address, with no override, for `linkLifeSeconds` bounded by the
   credential (R-STOR-072); a failure streams.
10. **The trace of a data key**, for a copy on an encrypted connection: `bastion.data_key.handed` is written in a transaction of its own
    (R-STOR-096); a failure streams, and the data key is destroyed. *Amended on 2026-11-23
    ([Q-STOR-472](#q-stor-472--the-value-is-encoded-before-the-trace-and-the-key-destroyed-before-the-insert)):* it is written after the
    value is encoded and the key destroyed, and before the answer is noted for the marking.
11. **Issue** the answer of [bastion.md](bastion.md) R-BAS-073: the file's own `200` without its bytes, the class `handover` and the value
    in `Quader-Bastion-Handover`.

Steps 1 to 7 read the request, the item row, the request's charge and the replica's snapshots, as R-STOR-070's do; step 8 is one
`HeadObject` and step 10 one insert, both for an encrypted copy only; signing is local. What a handover counts and charges, all at issuance,
as a link does (R-STOR-073, [usage-and-limits.md](usage-and-limits.md) R-LIM-040 and Q-LIM-98), the caller being the forward's subject
(R-LIM-003):

- one download in the statistics (R-STOR-015) and the repository's activity, and the note of `Quader-Bastion-Counted: download`
  ([bastion.md](bastion.md) Q-BAS-186), so that the Bastion reports what it serves of the blob afterwards (R-BAS-059);
- the file's size as cached bytes of the budget key; the parallel lease ends with the answer;
- `artifact.downloaded` under `audit.record_downloads` with `delivery: "handover"`, `bastion` (the registration's name), `connection`,
  `encrypted` and `linkExpiresAt` in `details`, beside `path` and `sha256`;
- the request log: status `200`, `delivery` `handover` ([operations-and-observability.md](operations-and-observability.md) R-OPS-010),
  `bytes_cached` the size, `response_bytes` `0`;
- `quader_storage_download_handovers_total{bastion, kind}` with `kind` `link` or `link_and_key`, and
  `quader_storage_download_handovers_declined_total{reason}` for an offer that was streamed (`switch_off`, `not_artifact`, `gate`, `size`,
  `connection`, `key`, `credential`, `signing`, `trace`); `bastion` is the registration's name, bounded by `bastions.max_registrations`.

Acceptance criteria:

- Unit (`internal/downloadlinks`, `internal/api/edge`, `internal/formathost`, the in-memory store with its signing, one table, under a
  second): with every condition met a tarball `GET` of a recognized forward that offered is answered with the handover (`200`,
  `Content-Length: 0`, the item's headers, the class `handover`, `Quader-Bastion-Counted`); each condition taken away alone streams the bytes:
  no offer, the registration's handover off, the same request without a Bastion token, `HEAD`, a range, a size below `minSize`, a size above
  `max-size`, a blob without a malware verdict under a gate that is not strict, an open circuit, a connection whose link check did not
  answer, a credential that expires in 3 s; `If-None-Match` with the content address answers `304`; a used-up `cachedBytesPerHour` answers
  `429` and no handover; a `cachedBytesPerSecond` does not stop one; one handover adds one download, the size to the charge's cached bytes and
  one `artifact.downloaded` with `delivery: "handover"` (`TestR_STOR_095_TheConditionsOfAHandover`).
- Unit (`internal/api/rawfiles`): a file of `/files/` is handed over like a format's artifact (`TestR_STOR_095_AFileOfFilesIsHandedOver`).

*As built on 2026-11-23 (package 2, plain copies):* [handover.go](../../internal/downloadlinks/handover.go) holds step 1 (the offer from
the `Recognition`, which the recognition stage alone fills, and the registration's switch from the replica's snapshot,
`bastionmain.HandoverSwitches`), 5, 7 and 9, and what step 11 counts; `Links.HandoverOptions` gives the serve point
`ServeOptions.Handover` and `HandedOver`, which `edge.ServeItem` calls after `Admit` for a `GET` whose answer is the whole file with `200`
([content.go](../../internal/api/edge/content.go)), and which [ingest.go](../../internal/formathost/ingest.go) (`handoverOptions` in
[download_links.go](../../internal/formathost/download_links.go): a route of the kind `Download`, not a system read, the gates decided)
and [handler.go](../../internal/api/rawfiles/handler.go) (every file, the gates of `files.Service.Stat`, which now reports whether they
passed on a decided state) set. The route's `ClientDigest` is no condition, nor are the switch of the clients' links and a repository's
`storage.downloadLinks` (Q-STOR-345). Step 7 named no copy on an encrypted connection until package 4 (below), so a blob whose
servable copies were all encrypted was streamed with the reason `connection`; a plain copy whose `stored_size` is not the blob's size is
skipped. The answer is the file's `200` through the format's own writers with `Content-Length: 0`; the value is noted for the marking
stage, which writes the class and the header (R-BAS-073's as-built note), and a refusal of `iface.EncodeHandover` streams. Counted at the
issue as a link is: `Charge.ChargeLink` (the size as cached bytes, the request log's `bytes_cached`), `items.Store.Served` with
`Delivery: "handover"`, the registration, the connection and `encrypted` (`artifact.downloaded`'s details, whose schema gains them), the
request log's `delivery` `handover`, `quader_storage_download_handovers_total{bastion, kind="link"}`; the label `bastion` joins R-OPS-007's
list ([Q-STOR-430](#q-stor-430--the-label-bastion-joins-the-allowed-labels-of-r-ops-007)). `quader_storage_download_handovers_declined_total`
counts `switch_off`, `not_artifact`, `gate` (and a content state that is not `ok` as `connection`, as the links count it), `size`,
`connection`, `credential` and `signing`; `key` and `trace` come with package 4. An offer answered by Quader's own `304`, `206`, `412` or
`429` is no declined handover and is not counted. A writer's read the signature gate passed is traced (`signature.download_by_writer`) by
`sgate.Serve.RunDecided` before the decision, whether it is then handed over, linked or streamed. Tests:
`TestR_STOR_095_TheConditionsOfAHandover` and `TestR_STOR_095_WhatAHandoverCounts` (`internal/formathost`, the in-memory store; each
condition taken away alone, the link's life bounded by a credential that ends in 20 s, a speed figure no condition),
`TestR_STOR_095_AFileOfFilesIsHandedOver` (`internal/api/rawfiles`), `TestR_STOR_096_TheCopyOfAHandover` (`internal/downloadlinks`, since
package 4 `TestR_STOR_096_ThePlainCopyFirstAndTheTraceOfAKey`); the
integration cases `TestR_BAS_070_ALargeFileIsReadFromTheObjectStore` (area `bastion`: the main installation and a Bastion joined; it replaced
the main installation's half, `TestR_STOR_095_AForwardThatOfferedIsHandedOverFromTheStacksStore`, on 2026-11-23,
[bastion.md](bastion.md) R-BAS-070) and the query budget's handed-over OCI blob (area `querybudget`, one statement, R-ARCH-027).

*As built on 2026-11-23 (package 4, data keys):* [handover.go](../../internal/downloadlinks/handover.go) holds steps 8 and 10 and the
encrypted half of 7. Step 7 is two passes over the copies `ServableOf` lists (`handoverCopy`): the first on a connection that can serve a
handover and is not encrypted, and only without one the first such copy on an encrypted connection whose row names the stored size
`S(size)` (a copy of another stored size is passed over, as a plain copy of another length is). Step 9 signs the link; then step 8 calls
`Options.DataKeys.CopyDataKey` (the blob store, wired in [wire_downloadlinks.go](../../internal/app/wire_downloadlinks.go)) for that one
copy, with the copy row and the blob's size the item lookup read: one `HeadObject` and no statement
([Q-STOR-470](#q-stor-470--copydatakey-opens-the-copy-the-requests-lookup-read-and-reads-no-row)); its bytes go into the value's
`iface.HandoverKey` and the format's key is destroyed at once. The value names `encrypted: true`, `storedSize` `S(size)` and the
`dataKey`; it is encoded, the key destroyed (`Links.destroy`; `Links.KeysHeld` counts the keys held and is 0 whenever no handover is being
decided), then step 10 writes `bastion.data_key.handed` in a transaction of its own through `Options.Runner` and `Options.Audit` (the main
pool and the audit's recorder), and only then is the value noted for the marking stage
([Q-STOR-472](#q-stor-472--the-value-is-encoded-before-the-trace-and-the-key-destroyed-before-the-insert)). A failure of step 8 (a key the
replica does not hold, a record that does not bind the blob, the store's answer to the `HeadObject`) streams with the reason `key`; a failed
insert streams with `trace`; a value the encoding refuses after the key was taken streams with `signing`, as for a plain copy; every path
destroys the key, a panic's included (a deferred destroy). The metric's kind is `link_and_key` for an encrypted copy, and
`artifact.downloaded` carries `encrypted: true`. `HandoverDownload` gained the forward's caller (`Actor`, `Source`, `Endpoint`), which the
serve points of `itemStore` and `/files/` fill and the trace and the served download name. A handover of an encrypted copy costs two
statements, the item lookup and the trace's insert (`GetLiveItem`, `InsertAuditEvents`, measured once on 2026-11-23 against the stack;
[architecture.md](architecture.md) R-ARCH-027's budget line). Tests: `TestR_STOR_096_ThePlainCopyFirstAndTheTraceOfAKey`
(`internal/downloadlinks`: the plain copy first, the named copy's own key with `S(size)`, the trace after the key's destruction and before
the answer with its members, a failed insert, a panic, a refused value, an unknown key, the credential and the signature before the key),
`TestR_STOR_095_TheConditionsOfAHandover` (`internal/formathost`: every condition once more on an encrypted connection, where a download
that streams takes no key and writes no trace, and the value with its key and trace when every condition holds),
`TestR_STOR_095_WhatAHandoverCounts` (the kind `link_and_key`), `TestR_STOR_083_CopyDataKeyOpensTheRecordOfTheCopy` (`internal/blob`), and
the integration case `TestR_BAS_070_AnEncryptedFileIsReadFromTheObjectStoreWithItsDataKey` ([bastion.md](bastion.md) R-BAS-070).

### R-STOR-096 · The connection a handover names, its link check and the trace of a data key
Priority: **P1** · Covers: quader-only

- **A connection can serve a handover** when its public address (R-STOR-074) is `https`, the host of a link signed against it is no host of
  Quader nor below one (R-STOR-072), and the first part of its link check passed on the replica: the probe's link answered `200` with the
  probe's bytes ([Q-STOR-346](#q-stor-346--the-link-check-tells-whether-a-link-works-apart-from-whether-it-honours-the-overrides)). The
  second part, whether the store honours the three overrides, matters to the clients' links only; a handover's link asks for none. An
  encrypted connection can serve a handover: its probe objects are plain (Q-STOR-315), so its link check runs as on any connection. The link
  check runs on every connection at every start of a replica that serves downloads, and when the switch of links or the handover of a
  registration is turned on.
- **The settings' answer** (`GET /api/v1/download-links/settings`, R-STOR-014) names per connection `handoverUsable` and `handoverReasons`
  (`not_https`, `quader_host`, `link_check_failed`), which the Bastions screen shows ([bastion.md](bastion.md) R-BAS-071). The test link of
  R-STOR-074 is offered for an encrypted connection as well, so the administrator tests from the Bastion's network the address a handover
  names.
- **The caller of `CopyDataKey`** (R-STOR-083's room): `internal/downloadlinks`, the handover's package; the architecture test allows calls
  from `internal/blob` and `internal/downloadlinks` and fails every other. The returned `DataKey` is destroyed once its bytes are in the
  answer.
- **The trace of a data key** ([Q-STOR-344](#q-stor-344--every-data-key-handed-out-is-an-audit-event-written-before-it-leaves)):
  `bastion.data_key.handed`, a transactional event (R-OPS-015) written in a transaction of its own before the answer leaves, whatever
  `audit.record_downloads` says: actor the forward's caller (or `anonymous`), `source_ip` the caller's address (bastion.md R-BAS-017),
  `repository` and `project`, `path`, and in `details` the registration (`bastion`), the replica (`Quader-Bastion-Replica`), `sha256`,
  `connection`, `storageKeyId` (the identifier of the storage key that wrapped the data key) and `linkExpiresAt`; never the key, the wrap or the
  link. It is not journaled. The audit trail therefore lists every data key a Bastion has received, which bounds what a taken-over Bastion
  holds ([security/bastion-handover.md](../security/bastion-handover.md) H-1340). A handover of a plain copy writes none: its trace is
  `artifact.downloaded` where downloads are audited, as a link's (R-STOR-073).
- **No rewrite of one blob** under a new data key
  ([Q-STOR-347](#q-stor-347--no-rewrite-of-one-blob-under-a-new-data-key-the-trail-names-every-key-that-left)): a data key that left stays
  the key of its object until the connection is replaced (R-STOR-085,
  [security/stored-content-keys.md](../security/stored-content-keys.md) H-1311); the trail names every one.

Acceptance criteria:

- Unit (`internal/downloadlinks`, the in-memory store with a plain and an encrypted connection): with a copy on each, the plain one is named
  and no key is handed; with the encrypted copy alone, the handed data key is the copy's (it opens the object), `bastion.data_key.handed` is
  written before the answer with its details and without the key, and a failing insert streams the download
  (`TestR_STOR_096_ThePlainCopyFirstAndTheTraceOfAKey`). *As built on 2026-11-23 (package 2):* the plain copy first and an encrypted copy
  alone streamed (`TestR_STOR_096_TheCopyOfAHandover`); the handed key and its trace are package 4's. *As built on 2026-11-23 (package 4):*
  the test of that name replaces it and holds the criterion; besides, the trace is written after the key is destroyed and before the
  answer is noted, and a refused value, a panic of the trail and a key the replica does not hold leave no key held.
- *As built on 2026-11-23 (package 4), the trace:* `bastion.data_key.handed` is registered in
  [events_bastion_datakey.go](../../internal/audit/events_bastion_datakey.go) (category `content`, transactional, no journal flag, no
  webhook) and written by `Links.trace`. Its row: `actor_*` the forward's caller (`anonymous` without a credential), `source_ip` the caller's
  address, `endpoint` the forward's, `resource_type` `bastion` and `resource_id` the registration's principal ID (so the audit query's
  resource filter lists every key one Bastion received), `repository`, `project` and `path` the item's; `details` exactly `bastion`,
  `replica` (empty when the forward named none), `sha256`, `connection`, `storageKeyId` and `linkExpiresAt` (RFC 3339, UTC), which its data
  schema declares as the only members. The resource is the build's addition: the requirement names none.
- Unit (`internal/blob`): a store fake that ignores `response-content-disposition` passes the first part of the link check and fails the
  second, so its connection can serve a handover and no link (`TestR_STOR_096_TheLinkCheckHasTwoParts`). Built with the links' package 2,
  which records the two parts and decides `HandoverUsable` with its reasons, and offers the test link for an encrypted connection;
  package 2 of the handover adds the run of the link checks when a registration's handover is turned on (`Snapshot.CheckLinks`, asked by
  the snapshot of the switches, `TestR_BAS_071_TheSnapshotOfTheSwitches`).
- Unit (`internal/archtest`): a call of `CopyDataKey` from a package other than `internal/blob` and `internal/downloadlinks` fails
  (`TestR_STOR_096_CopyDataKeyHasTwoCallers`).

### R-STOR-048 · Client-side encryption of stored content
Priority: **P1** · Covers: STOR-022

Decided by the owner on 2026-11-22 (decisions 1 to 7, 13 and 15 of his working paper on the stored content;
[ADR-244](../decisions.md#adr-244--encrypted-connections-quaders-own-segment-format-bound-to-the-content-the-wrapped-key-once-at-the-object-a-storage-key-per-connection-under-the-key-ring)):
Quader can keep the blobs of a connection encrypted in the object store (`encrypted: true`, off by default), in a format of Quader's own
after the model of the segment chain of his s3-encryption-proxy, bound to the content and not to the object's name, written in this
specification with test vectors, fuzzing and an offline tool that reads a blob without a running Quader. This replaces his decision of
2026-11-06, which imported that proxy's storage format and Go module; nothing of it was built, and its eleven open points are answered by the
section [Encryption of stored content](#encryption-of-stored-content) (R-STOR-075 to R-STOR-091). Stage 2 of the owner's topic, on the seams
of stage 1 (R-STOR-062); its format (`internal/blob/blobcrypt`, R-STOR-076 to R-STOR-079) and its keys (`internal/blob/keys`, R-STOR-083 to
R-STOR-085) are built, no object is written or read in the format yet, and the rest is specified, not built. Raised from P2 to P1 with the topic, as stages 1 and 3 were.

Why: a provider, its operators and whoever can read or copy a connection's bucket see ciphertext only, under names that do not tell which
file is stored, while the key stays with Quader; and a changed object fails authentication on every read, ranged reads included, which
narrows [H-11](#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks) for that connection.

What it means, each in its requirement:

- **Per connection.** `encrypted` is a fixed property of a connection (R-STOR-050, R-STOR-051, R-STOR-075); switching it on or off is a new
  connection that the reconciliation fills (R-STOR-087). An installation can mix plain and encrypted connections.
- **The format** (R-STOR-076 to R-STOR-079): one random data key per stored object; AES-256-GCM over segments of 64 KiB, each authenticating
  the format, its index and whether it is the last; a final segment with the plaintext's length and SHA-256; nothing bound to the object's name,
  so that the finalize of an upload session and the backup's pool keep copying objects without decrypting them.
- **The record** (R-STOR-077): the data key wrapped under the connection's storage key, bound to the digest, with the key's identifier, the
  format and a commitment to the data key, in four metadata keys written in the same request as the bytes, and nowhere else.
- **The keys** (R-STOR-083 to R-STOR-085): the key ring of stored secrets is the root; a storage key per encrypted connection and generation,
  drawn by Quader and sealed in one row; no level of keys per project or repository; a rotation of the root re-seals rows and touches no
  object; an export of the sealed storage keys for safekeeping beside the key ring.
- **Names** (R-STOR-086): on an encrypted connection an HMAC of the digest under a name key, unless the detail `keyed_names` says otherwise.
- **Every download is decrypted and streamed by Quader**: an encrypted connection serves no signed link (R-STOR-014, R-STOR-088).
  Server-side encryption (R-STOR-032) stays possible beneath.
- **Elsewhere**: the writes and reads (R-STOR-080 to R-STOR-082), the jobs (R-STOR-087), the offline tool (R-STOR-089), the backup's pool
  ([backup-and-restore.md](backup-and-restore.md) R-BAK-031) and the Bastion's cache ([bastion.md](bastion.md) R-BAS-061).

Why not the proxy's format, as the research of 2026-11-22 found it in the proxy's code at its release 5.4.2: it binds every segment to the
object's name, so an object copied to another name does not open, which rules out Quader's finalize by a server-side copy and the backup's
copies unless they decrypt and encrypt again; it carries exactly one wrap per object and its rules forbid more; it commits to no key; and
its Go interface is declared no compatibility promise. The proxy cannot read Quader's objects, and Quader does not read the proxy's.

Acceptance criteria (each requirement of the section has its own; these are the whole):

- With `encrypted: true`, `aws s3api head-object` of a blob's object shows the four record keys, the object's name is no digest of the blob,
  and no 16-byte run of the uploaded file appears in the object; a download through Quader returns the original bytes.
- A blob whose stored bytes were changed fails on a whole and on a ranged read and is marked `corrupt` on that connection.
- `quader admin storage offline get` with the key ring files and the export reads the blob from the bucket with no database.

### R-STOR-015 · Download statistics
Priority: **P1** · Covers: ART-029

- A download counts when Quader answers `200` with the full content, `206` for a range starting
  at byte 0, or issues a presigned redirect; `HEAD`, `304`, sidecars and requests with
  `?stats=false` do not count.
- A request of the system subject `system:vuln-scanner-trivy` (a scan token on the scan listener, [formats/oci.md](formats/oci.md) R-OCI-037)
  counts no download and moves no `last_download_at`: the image scanning reads the image for the installation's findings and delivers
  nothing to a consumer ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-042). Neither do the worker's reads of the image
  scanning's database through the item store (R-VULN-044), which are no requests.
- Through a virtual repository the count goes to the item of the serving member.
- The first answer of a remote miss, which the remote engine streams to the client while it fetches, counts on the item it stored
  ([repositories-and-proxy.md](repositories-and-proxy.md) Q-REPO-331). A document a format answers from a store of its own with the
  bytes of an item (a remote OCI tag's manifest) counts on that item through `ItemStore.RecordDownload` (Q-OCI-222). Which downloads are
  the fetches of a version is the format's rule ([api-and-ui.md](api-and-ui.md) R-API-079).
- Each `web` replica aggregates counts in memory and flushes them every
  `storage.stats.flush_interval` with one upsert per item (`downloads = downloads + n`,
  `last_download_at = greatest(…)`, `last_downloader` of the latest download), in one transaction
  per batch with the items in id order. Requests reach replicas at random, so several replicas
  flush the same popular items in every interval; the common order keeps their batches from
  deadlocking on each other's rows. The upsert joins `items`, so the counts of an item removed
  since its download are dropped instead of failing the batch on the foreign key; a batch that
  fails anyway stays in the buffer and is retried at the next flush.
- The buffer is flushed at shutdown ([architecture.md](architecture.md) R-ARCH-024 step 6), so a
  drain loses no counts; a replica crash loses at most one interval. Statistics are not audit
  data. No request reads the buffer: the API below and the retention conditions read PostgreSQL,
  which every replica sees alike (INV-18, Q-STOR-12).
- `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/stats?path=…` answers
  `{"downloads": 1832, "lastDownloadAt": "2026-10-27T22:01:55Z", "lastDownloader": "ci:acme/web"}`
  *(example)*. Permission: `read`.
- `last_downloader` is set to `NULL` when the principal is erased
  ([identity-and-authentication.md](identity-and-authentication.md)).
- These statistics count downloads per item, not per caller. What each caller downloads, in requests and bytes per time bucket, is the
  usage store of [usage-and-limits.md](usage-and-limits.md) R-LIM-027, written by the same kind of per-replica buffer and read by the
  usage overview; neither reads the other.

Configuration:

```yaml
storage:
  stats:
    flush_interval: 10s    # default (proposed)
```

Acceptance criteria:

- Three `npm install` runs that each download a tarball raise its `downloads` by three after at
  most `flush_interval`.
- `curl -I` does not change the count.

## Item operations

### R-STOR-016 · Item information
Priority: **MVP** · Covers: ART-027, ART-001

`GET /api/v1/projects/{projectKey}/repositories/{repoKey}/items?path={path}` returns the metadata of a file or folder
(an empty `path` addresses the repository root).

```json
{
  "repository": "web/npm-local",
  "path": "@acme/widget/-/widget-1.4.0.tgz",
  "kind": "file",
  "size": 48213,
  "mediaType": "application/gzip",
  "created":  { "at": "2026-10-28T09:14:03.117Z", "by": "ci:acme/widget" },
  "modified": { "at": "2026-10-28T09:14:03.117Z", "by": "ci:acme/widget" },
  "updatedAt": "2026-10-28T10:02:41.009Z",
  "digests": { "sha256": "…", "sha512": "…", "sha1": "…", "md5": "…" },
  "declaredDigests": { "sha512": "…", "sha1": "…" },
  "declaredMismatch": false,
  "contentStatus": "ok",
  "package": { "name": "@acme/widget", "version": "1.4.0", "lead": true },
  "upstreamUrl": null,
  "downloadUrl": "https://quader.example.com/files/web/npm-local/@acme/widget/-/widget-1.4.0.tgz"
}
```

Folders return `kind: "folder"`, `explicit`, timestamps and the URL of their listing; they do not
embed their children (R-STOR-017). Through a virtual repository the item of the serving member
is returned with an additional `resolvedFrom` repository key, a key of the same project (virtual members are repositories of the virtual repository's project). Permission: `read`. Errors: `400`
malformed path; `401`; `403`; `404`.

Acceptance criteria:

- After `dotnet nuget push Acme.Widget.1.4.0.nupkg --source https://quader.example.com/api/nuget/web/nuget-local/v3/index.json --api-key $QUADER_TOKEN`
  the item of the `.nupkg` shows `package.name = "acme.widget"`, `version = "1.4.0"`,
  `lead = true`.

### R-STOR-017 · Folder listing and directory index
Priority: **P1** · Covers: ART-028, ART-042

- `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/children?path=&recursive=false&maxDepth=&folders=true&cursor=&limit=`
  lists the untrashed children of a folder in byte order of their path, with cursor pagination
  (`limit` default 1,000, maximum 10,000). `recursive=true` walks the subtree, optionally bounded
  by `maxDepth`.
- Entry: `{"path": "<relative>", "kind": "file", "size": 48213, "modifiedAt": "…", "sha256": "…", "sha1": "…"}`;
  folders carry no size or digests.
- `GET /files/{projectKey}/{repoKey}/{folder}/` answers with a directory index: HTML for `Accept: text/html`
  (every name HTML-escaped, every link written as `./` followed by the percent-encoded name so that
  a name such as `javascript:x` stays a relative path, the same CSP as R-STOR-012 without
  `sandbox`, no scripts), JSON
  (the same entries) otherwise. Under the repository's checksum policy (R-STOR-010) sidecar
  entries appear next to files.
- The HTML index is rendered by Quader, not stored content, so it carries no `Content-Disposition`.
  It still carries `nosniff` and `Cross-Origin-Resource-Policy: same-site` (`same-origin` when the
  session cookie authenticated the request, [architecture.md](architecture.md) R-ARCH-007). The core's writer
  wrapper of R-STOR-012 selects this header set for a folder `GET` before the handler runs, so the
  handler cannot weaken it either.
- Virtual repositories list the merged view defined in
  [repositories-and-proxy.md](repositories-and-proxy.md).

Permission: `read` on the folder; entries the caller cannot read are omitted.

Acceptance criteria:

- `curl -H 'Accept: text/html' …/files/web/generic-local/tools/` returns links for each child; a file
  named `<script>.txt` appears escaped, and a file named `javascript:alert(1)` is linked as
  `./javascript%3Aalert(1)`.
- Listing a folder with 25,000 children with `limit=10000` takes three pages and returns every
  child exactly once.

### R-STOR-018 · Delete items
Priority: **MVP** · Covers: ART-034, STOR-002

- `DELETE /files/{projectKey}/{repoKey}/{path}` (or `DELETE /api/v1/projects/{projectKey}/repositories/{repoKey}/items?path=…`)
  removes a file, or a folder with its whole subtree, and answers `204`.
- Deletion touches only PostgreSQL: items are moved to the trash can (local repositories with the
  trash can enabled, R-STOR-022) or deleted; blobs stay until garbage collection (R-STOR-024).
- The format plug-in's `OnItemsRemoved` hook runs in the same transaction, so format indexes
  never list removed content.
- Permission: `delete` on every item of the subtree, each on the coordinate its format gives it
  (package name and version for npm, [endpoints.md](endpoints.md) R-EP-014). The check completes
  before anything is removed; if any item is not deletable the answer is `403` and nothing changes.
  Removing files of a package format is a mutating operation on their packages: the ownership question
  of [authorization.md](authorization.md) R-AUTHZ-018 is asked for every package whose files the
  delete removes, in the transaction that removes them or queues their job (`403
  package_owned_by_other_owner`, Q-AUTHZ-161).
- The items API deletes as `/files/` does, the repository's patterns and its disabled flag included,
  except for a holder of `content:bypass`, whose cleanup removes items outside the patterns and in a
  disabled repository ([authorization.md](authorization.md) Q-AUTHZ-162).
- A subtree larger than `storage.delete.max_sync_items` is removed by a background job
  (`202 Accepted` with the job URL, job API per [architecture.md](architecture.md)); the
  permission check still completes before the first removal. The job removes in transactions of
  `storage.delete.batch_size` items; a crash resumes with the remainder. The job removes only
  items the check saw (Q-STOR-143): one that is deployed into the subtree afterwards stays. A
  subtree that empties between the count and the check queues no job, and a job that names no
  highest item id or no subject removes nothing (Q-STOR-192).
- The job acts for the caller only while a request of that caller would be allowed (Q-STOR-190). Every
  batch decides again, on what the caller holds when the batch begins, `delete` on each item it removes,
  the ownership question above for its files, and every path a storage hook cascades to: the principal
  must still be active and of its kind, its name and groups are those it has then, and the credential
  that asked must not have been withdrawn ([authorization.md](authorization.md) Q-AUTHZ-260, Q-AUTHZ-261,
  Q-AUTHZ-262). A refused item, a principal that is no longer active and a credential that was revoked,
  withdrawn or purged fail the job without a further attempt, with the reason in the job's error and in
  the audit event `artifact.delete_stopped`; what earlier batches removed stays removed (Q-STOR-193,
  Q-STOR-194). A refusal names the path the request named, never the refused item. A batch that begins
  later than `auth.revocation_bound` after such a change removes nothing.
- The job holds its repository by its ID (Q-STOR-191): a repository moved to another project is decided,
  and named in the job's events, where it is now, and a repository created under the old name is never
  touched; a repository that is gone or being deleted ends the job as done, because its own deletion
  removes the rest. A disabled repository stops the job unless the caller holds `content:bypass`, as for
  the request (Q-AUTHZ-162).
- A **generated item** (the files of a job-built index, written by the publication port, Q-STOR-182) is never written, deleted,
  copied or moved by a client: a delete whose subtree holds one, and an upload to a path that holds one, answer `409 generated_item`
  and change nothing (Q-STOR-210). The port itself replaces and removes them.
- Remote repositories: a delete removes the cached copy permanently (no trash can); the next
  request refetches it. Virtual repositories answer `405`.
- Deleting a whole repository (operation of
  [repositories-and-proxy.md](repositories-and-proxy.md)) removes its items permanently in the
  background; their blobs are queued for collection.

Configuration:

```yaml
storage:
  delete:
    max_sync_items: 10000   # default (proposed)
    batch_size: 5000        # default (proposed)
```

Acceptance criteria:

- `curl -X DELETE …/files/web/npm-local/@acme/widget/-/widget-1.4.0.tgz` answers `204`; afterwards
  `npm view @acme/widget versions --registry …` no longer lists 1.4.0.
- Deleting a folder that contains one file the caller may not delete answers `403` and removes
  nothing.
- In an npm repository with `packageOwnership: enforce`, `DELETE /files/web/npm-local/@acme/` by a
  holder of `delete` who does not own one of the scope's packages answers `403`
  `package_owned_by_other_owner` and removes nothing, through the job path as well
  (`TestR_AUTHZ_018_RawRemovalsAskOwnership`,
  `TestR_STOR_018_ARawRemovalAboveTheSyncLimitAsksOwnershipBeforeItsJob`).
- A delete large enough for the job is queued; then the caller's token is revoked, the caller is
  disabled, or the caller leaves the group whose policy grants `delete`. The job fails with the reason
  and removes none of the files; a token that merely expired lets the job finish
  (`TestR_STOR_018_TheDeleteJobDecidesEveryBatchOnTheStandingOfItsSubject`). A token revoked while a
  running worker is between two batches stops the job at the next one, and the files of the earlier
  batches stay removed (the same test, and `TestR_STOR_018_AWithdrawalBetweenTwoBatchesStopsTheJobAtTheNext`
  for a principal). The job records `artifact.delete_stopped` with the reason code.
- A repository moved to another project between two batches of the job is decided where it is now, and a
  repository created under the old name keeps its files; a repository deleted between two batches ends
  the job as done (`TestR_STOR_018_TheDeleteJobFollowsItsRepositoryByID`). An ownership refusal of a
  later batch, and a refused folder batch, stop the job at its first attempt
  (`TestR_STOR_018_EveryBatchOfTheDeleteJobAsksTheFormatAndDecidesItsFolders`).

### R-STOR-019 · Copy and move
Priority: **P1** · Covers: ART-032, ART-033, STOR-002

```http
POST /api/v1/content/copy
POST /api/v1/content/move
Content-Type: application/json

{
  "source": { "repository": "web/generic-staging", "path": "tools/widget/2.3" },
  "target": { "repository": "web/generic-release", "path": "tools/widget/2.3" },
  "dryRun": false,
  "stopOnError": true,
  "overwrite": false,
  "copyProperties": true
}
```

- `repository` is the qualified name `<projectKey>/<repoKey>`; source and target may be repositories of different
  projects, each decided by its own grants.
- Both operations are metadata-only: copy inserts item rows that reference the same blobs (via
  the guard trigger), move updates repository and path. No S3 request is made.
- `target.path` names the exact destination (Q-STOR-8). If a folder already exists there, the
  source content is merged into it; it is never nested inside. Repeating a request therefore yields
  the same tree.
- A target item that already references the same blob as its source is left as it is and counted
  as `unchanged`; it is not a conflict, so a repeated copy succeeds without `overwrite`. A target
  item with different content is a conflict (`exists`) unless `overwrite` is true.
- A file whose target path is an existing folder, or a folder whose target path is an existing
  file, is a per-item conflict (`type_conflict`) that `overwrite` does not resolve: a folder is
  never replaced by a file or the other way round.
- Every target path MUST be admitted by the target repository's include and exclude patterns
  ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-005); a path they do not admit
  is reported per item (`not_admitted`).
- Sources: copy from local repositories and remote caches; move from local repositories only.
  Targets: a local repository, or a virtual repository with a deployment target, of the same
  format as the source or of the generic format. The target format plug-in may reject items that
  do not fit its layout (reported per item).
- A format may also refuse an item with its own per-item codes, whatever `overwrite` says. NuGet
  refuses a version that the target holds with other bytes (`nuget_version_exists`) and a version
  that is tombstoned there (`nuget_version_deleted`); a version the target holds with identical
  bytes counts as `unchanged` ([formats/nuget.md](formats/nuget.md) R-NUGET-005, R-NUGET-006).
- **Shadowing check.** An item may give a local target repository L, whose format supports claims
  (`pinsAndClaims`, R-REPO-003), its first version of a package name n outside L's claims. L is the
  deployment target when the target is a virtual repository. For each such item, the shadowing
  check of [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-048 runs after the item's
  permission checks and before the operation's transaction, with the caller in the publisher's
  place. For a background job it runs before each batch transaction.
  - It runs once per (L, n) and operation, and its outcome applies to every item of n in the
    operation. Its probes hold no transaction open and store nothing.
  - A refused item is reported per item with `shadows_remote_package` or `shadow_check_unavailable`
    and left unchanged, like any other per-item error under `stopOnError`.
  - A caller holding `manage` on the coordinate in L, or `content:bypass`, proceeds as R-REPO-048's
    outcome table says. The audit event `repository.remote_package_shadowed` is then written in the
    operation's transaction.
  - Without this check, a `deploy` holder could publish a name into a repository that no virtual
    repository contains, where nothing is exposed, and then copy or move the package into a shared
    one.
- Permissions: copy needs `read` on each source item and `deploy` on each target path; move
  needs `delete` on each source item and `deploy` on each target path; replacing an existing
  target item needs `overwrite` (the replaced item goes to the trash can).
- Properties are copied when `copyProperties` is true (default); download statistics are not.
  Copied items get the caller and the current time as creator.
- Format plug-in hooks run for both repositories in the same transaction. Each increments the
  `rev_seq` of every package it changes: in the target repository for a copy, in both repositories
  for a move (R-STOR-003).
- A move out of a local npm or NuGet repository runs the source format's removal hook
  (`OnItemsRemoved`), which writes the version's tombstone in the source repository
  ([formats/npm.md](formats/npm.md) R-NPM-008, [formats/nuget.md](formats/nuget.md) R-NUGET-006).
  Clients of the source repository may have cached the version, so its number must not return
  there with other bytes.
- `dryRun: true` evaluates permissions, conflicts, format rules and the shadowing check, and
  changes nothing. It reports the same per-item codes as a real run would.
- Per-item codes: `exists`, `type_conflict`, `not_admitted`, `forbidden`, `shadows_remote_package`,
  `shadow_check_unavailable`, and the target format's own codes (NuGet: `nuget_version_exists`,
  `nuget_version_deleted`).
- Response `200`: `{"files": 12, "folders": 3, "unchanged": 0, "bytes": 81233920, "messages": [{"level": "error", "path": "tools/widget/2.3/a.bin", "code": "exists", "text": "target exists and overwrite is false"}]}`.
  Above `storage.delete.max_sync_items` items the operation runs as a background job (`202`).

Acceptance criteria:

- Copying a 2 GiB file between repositories completes in under one second on the reference test
  setup and issues no S3 request.
- A dry run of a move without `delete` on the source lists one `forbidden` message per item and
  changes nothing.
- Copying `tools/widget/2.3` from `generic-staging` to `generic-release` twice with
  `overwrite: false` succeeds both times; the second response reports every file as `unchanged`
  and no `error` message, and the target tree has no `2.3/2.3` folder.
- Set-up as in [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-048:
  `npm-all` = [`npm-local`, `npm-remote`], the stub upstream of `npm-remote` serves `lodash`, and
  `npm-sandbox` is a member of no virtual repository. `ci-bot` holds `deploy` but not
  `manage`. It publishes `lodash@4.17.22` to `npm-sandbox` and copies it to `npm-local`. The item
  is reported with `shadows_remote_package` and `npm-local` holds no `lodash`. A dry run of the same
  copy reports the same code. The same copy by `lead`, who holds `manage`, succeeds and writes a
  `repository.remote_package_shadowed` event.
- Moving NuGet `Acme.Widgets` 1.2.6 from `nuget-local` to `nuget-release` leaves a tombstone in
  `nuget-local`. Moving it back, also with `overwrite: true`, reports `nuget_version_deleted`, and
  the version stays in `nuget-release`.
- After a copy of `@acme/ui@1.4.0` into `npm-release`, the next
  `npm view @acme/ui versions --registry …/api/npm/web/npm-release/` lists 1.4.0 on every replica.

### R-STOR-020 · Item properties
Priority: **P1** · Covers: ART-021, ART-022, ART-023, ART-024, ART-025

- Properties are string key/value annotations on files, folders and the repository root; a key
  holds a set of values. Keys: 1–255 characters, no control characters and none of `= , ; |`.
  Values: up to 2,400 characters, no control characters except tab. At most
  `storage.properties.max_values_per_item` values per item.
- Keys starting with `quader.` are reserved for system-maintained properties and cannot be
  written by clients.
- Writable in local repositories and remote caches; readable but not writable through virtual
  repositories (`405`).
- API (permission: `read` for reading, `annotate` for writing):

  | Method | Path | Body | Result |
  |---|---|---|---|
  | `GET` | `/api/v1/projects/{projectKey}/repositories/{repoKey}/properties?path=&keys=a&keys=b` | — | `200 {"properties": {"stage": ["qa"]}}`; `{}` when none |
  | `PATCH` | `/api/v1/projects/{projectKey}/repositories/{repoKey}/properties?path=&recursive=false` | `application/merge-patch+json` ([RFC 7396](https://www.rfc-editor.org/rfc/rfc7396)): `{"stage": "qa", "targets": ["linux", "darwin"], "obsolete": null}` | `204`; a string or array replaces the key's values, `null` removes the key, absent keys stay |
  | `DELETE` | `/api/v1/projects/{projectKey}/repositories/{repoKey}/properties?path=&keys=a&keys=b&recursive=false` | — | `204` |

- `keys` is repeated once per key (`keys=qa.status&keys=build.number`), the multi-value rule of
  [api-and-ui.md](api-and-ui.md) R-API-011 as settled in R-API-026. A comma-separated list is not
  accepted: `,` cannot occur in a key, so `keys=a,b` answers `400`.

- A recursive change applies to the item and every item below it in one transaction and is
  refused with `422` when the subtree exceeds `storage.properties.max_recursive_items`.
- Property changes update `items.updated_at`, emit an audit event and feed retention
  conditions (R-STOR-038).

Configuration:

```yaml
storage:
  properties:
    max_values_per_item: 1000   # default (proposed)
    max_recursive_items: 100000 # default (proposed)
```

Acceptance criteria:

- `curl -X PATCH -H 'Content-Type: application/merge-patch+json' -d '{"stage":"qa"}' "…/properties?path=tools/widget.bin"`
  answers `204`; `GET` returns `{"properties":{"stage":["qa"]}}`.
- With `stage` and `targets` set, `GET "…/properties?path=tools/widget.bin&keys=stage&keys=targets"`
  returns both keys, and `keys=stage,targets` answers `400`.
- Setting a property on a virtual repository path answers `405`.

### R-STOR-021 · Property sets
Priority: **P1** · Covers: ART-026

- A property set is an administrator-defined, named list of property definitions (name, value
  type `any` | `single` | `multi`, optional default, allowed values) assigned to repositories.
  The UI offers the assigned sets when annotating items; the server does not enforce them.
- API (permission: system administrator; assigning to a repository also allowed with `manage`
  on that repository):

  | Method | Path | Notes |
  |---|---|---|
  | `GET`, `POST` | `/api/v1/property-sets` | list, create |
  | `GET`, `PUT`, `DELETE` | `/api/v1/property-sets/{name}` | `DELETE` answers `409` while repositories are assigned |
  | `PUT` | `/api/v1/property-sets/{name}/repositories` | body: list of qualified repository names (`web/generic-local`) |

```sql
CREATE TABLE property_sets (
    name        text PRIMARY KEY CHECK (name ~ '^[A-Za-z0-9._-]{1,64}$'),
    description text NOT NULL DEFAULT ''
);
CREATE TABLE property_set_entries (
    set_name       text NOT NULL REFERENCES property_sets (name) ON DELETE CASCADE,
    property       text NOT NULL,
    value_type     text NOT NULL CHECK (value_type IN ('any', 'single', 'multi')),
    default_value  text,
    allowed_values text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (set_name, property)
);
CREATE TABLE property_set_repositories (
    set_name  text NOT NULL REFERENCES property_sets (name) ON DELETE RESTRICT,
    repo_id   bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    PRIMARY KEY (set_name, repo_id)
);
```

Acceptance criteria:

- Deleting a set assigned to `generic-local` answers `409`; after unassigning, `204`.

## Trash can

### R-STOR-022 · Trash can
Priority: **P1** · Covers: ART-036, RET-038

- Runtime settings (stored in PostgreSQL, changed through `PUT /api/v1/settings/storage`, cached
  per replica and invalidated through the change area `cache` of
  [architecture.md](architecture.md) R-ARCH-018: the PostgreSQL change log, with the Valkey message
  as the fast path, so a change applies on every replica within `cluster.epoch_poll_interval` even
  while Valkey is unavailable):

  ```json
  { "trash": { "enabled": true, "retention": "336h" } }
  ```

  `enabled: true` and `retention: 336h` (14 days) are the defaults (proposed).
- The storage settings are one document with a revision (`trash` here, `quota` in R-STOR-029).
  `GET /api/v1/settings/storage` returns `ETag: "<revision>"`; `PUT` replaces the whole document
  and requires `If-Match` ([api-and-ui.md](api-and-ui.md) R-API-012, Q-API-2): 428
  `precondition_required` without it, 412 `precondition_failed` when stale, `If-Match: *` for a
  deliberate blind write. Without the guard, a stale `PUT` that changes the trash retention would
  silently reset a quota another administrator had just set, and the reverse.
- With the trash can enabled, deletions of items in **local** repositories set `trashed_at`,
  `trashed_by` and a `trash_operation` id on every affected item (and version row) instead of
  removing them. This applies to user deletes (R-STOR-018), overwrites of user-deployed files
  (R-STOR-005), retention policies unless they bypass it (R-STOR-037), and repository caps
  (R-STOR-044). Remote-cache deletes, generated items and repository deletion bypass it.
- Trashed items are invisible to every protocol endpoint, `/files`, listings and package
  metadata; they remain blob references, count toward physical storage, and do not count
  toward the repository quota (R-STOR-029).
- Expiry is computed from the current setting: an item expires when
  `trashed_at < now() - retention`. Shortening the retention takes effect on the next purge.
  Disabling the trash can stops new entries; existing entries expire normally.
- The purge runs with every garbage-collection run (R-STOR-025): it deletes expired trashed
  items in batches, which queues their blobs for collection.
- Format tombstones ([formats/npm.md](formats/npm.md) R-NPM-008,
  [formats/nuget.md](formats/nuget.md) R-NUGET-006) are not trash entries. A format writes them in its removal hook, in the transaction that trashes or
  removes the version.
  - The purge, permanent removal and the manual purge of R-STOR-023 leave them in place, so a
    version number stays reserved after its content is gone.
  - Only a restore of the version (R-STOR-023) and the deletion of the whole repository remove
    one, and `quader admin npm reinstate` for a tombstone that a restore's journal replay wrote for
    a version that is still stored with its integrity ([formats/npm.md](formats/npm.md)
    R-NPM-047). None of them admits other bytes under the version number (R-NPM-008).
  - Tombstones do not appear in the trash-can listing.
- Without the trash can, every deletion, including policy deletions, is permanent.
- Visibility: a principal sees trashed items of an original path where it holds `deploy` or
  `manage` (evaluated as if the item were live).

Acceptance criteria:

- With defaults, `curl -X DELETE …/files/web/generic-local/a.bin` makes the file disappear from
  `/files` and appear in `GET /api/v1/trash-can/items?repository=web/generic-local`.
- After setting `retention` to `0s` (a `PUT` with the current `If-Match`) and running garbage
  collection, the entry is gone and its blob is queued.
- `PUT /api/v1/settings/storage` without `If-Match` answers 428 and changes nothing; with the
  `ETag` read before another administrator's `PUT` it answers 412 with the new tag as
  `currentEtag`.
- With defaults, uploading `a1.bin` and then `a2.bin` to `…/files/web/generic-local/a.bin` (the second
  upload with `overwrite`) serves `a2.bin`'s content, and the trash can lists one entry for the
  path whose restoration (with `overwrite: true`) serves `a1.bin`'s content again (Q-STOR-5).

### R-STOR-023 · Restore and permanent removal from the trash can
Priority: **P1** · Covers: ART-037, ART-038

| Method | Path | Body / parameters | Result | Permission |
|---|---|---|---|---|
| `GET` | `/api/v1/trash-can/items` | `repository`, `path` (prefix), `cursor`, `limit` | entries `{id, repository, path, kind, size, trashedAt, trashedBy, operationId, expiresAt}` | visibility rule of R-STOR-022 |
| `POST` | `/api/v1/trash-can/restorations` | `{"ids": [..]}` or `{"operationId": "…"}` or `{"source": {"repository": "web/generic-local", "path": "…"}}`; optional `"target": {"repository": "…", "path": "…"}` (qualified names `<projectKey>/<repoKey>`); `"overwrite": false` | `200 {"restored": n, "skipped": n, "messages": [..]}`; `202` with job URL above `storage.delete.max_sync_items` | `deploy` and `delete` on the original path; `deploy` (and `overwrite` if replacing) on a different target |
| `DELETE` | `/api/v1/trash-can/items` | `ids` or `repository` + `path` | `204` | `delete` or `manage` on the original path |
| `POST` | `/api/v1/trash-can/purge` | — | `202` | system administrator |

- Restore clears the trash fields, re-links the version row and runs the format plug-in's
  optional `OnItemsRestored` hook (`formats.ItemsRestorer`, found when the format registers). When an untrashed item already occupies the target path, that entry is
  skipped with a message unless `overwrite` is true (the occupant then goes to the trash can).
  When the version was re-published meanwhile, the version's entries are skipped.
- `OnItemsRestored` lets the format remove the restored version's tombstone in the restore
  transaction, because the original bytes come back ([formats/npm.md](formats/npm.md) R-NPM-008,
  [formats/nuget.md](formats/nuget.md) R-NUGET-006). Permanent removal and purge leave tombstones in place
  (R-STOR-022).
  - A restore with a different `target` repository leaves the original repository's tombstone in
    place, because the version does not return there.
  - The target format may refuse such an entry with its own codes, as for a copy (R-STOR-019).
- **Shadowing check.** An entry may give a local repository L, whose format supports claims
  (`pinsAndClaims`), its first version of a package name n outside L's claims. L is the original
  repository or the given `target`. For each such entry, the shadowing check of
  [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-048 runs after the permission
  checks and before the restore transaction, with the caller in the publisher's place.
  - A refused entry is reported in `messages` with `shadows_remote_package` or
    `shadow_check_unavailable`, counted as `skipped`, and stays in the trash can.
  - A caller holding `manage` on the coordinate in L, or `content:bypass`, proceeds as R-REPO-048
    says, with the audit event in the restore transaction.
  - Without this check, a caller could publish and delete a version in a repository that no
    virtual repository contains, and then restore it with `target` into a shared one where it
    holds only `deploy`.
- Restoring a trash operation restores exactly the items one delete call removed, so a folder
  comes back as it was.
- Denied permissions answer `403`.

Acceptance criteria:

- Deleting an npm version through the API and restoring its `operationId` makes
  `npm view @acme/widget versions` list the version again and `npm install @acme/widget@1.4.0`
  succeed.
- A restore into a path that is occupied, without `overwrite`, reports one `skipped` message and
  leaves both items unchanged.
- After a hard delete of NuGet `Acme.Widgets` 1.2.3 and `POST /api/v1/trash-can/purge`, a push of
  1.2.3 still answers 409 `nuget_version_deleted`. Restoring a deleted 1.2.4 before the purge
  removes its tombstone and serves the original `.nupkg`.
- Set-up as in the shadowing criterion of R-STOR-019. `ci-bot` holds `deploy` and `delete` on `npm-sandbox` and only `deploy`
  on `npm-local`. It publishes `lodash@4.17.22` to `npm-sandbox`, deletes it, and restores the
  entry with `target` `npm-local`. The response counts the entry as `skipped` with
  `shadows_remote_package`, the entry stays in the trash can, and `npm-local` holds no `lodash`.

## Garbage collection and integrity

### R-STOR-024 · Race-free garbage collection
Priority: **MVP** · Covers: STOR-036

Garbage collection deletes blobs that no reference uses any more. It MUST never delete a blob
that a committed reference uses, and MUST never let an upload commit a reference to a blob whose
object it is deleting.

**Candidates.**

- *Incremental*: digests in `blob_gc_queue` whose `enqueued_at` is older than
  `storage.gc.grace_period` (the release trigger enqueues a digest whenever a reference to it
  disappears).
- *Full sweep*: every `live` blob older than the grace period, found in keyset batches over
  `blobs.sha256` with an anti-join against all referencing tables, skipping digests whose queue
  entry is younger than the grace period. The set-based query only proposes candidates; each one
  is confirmed by the per-blob protocol.
- *Interrupted deletions*: rows in state `deleting`.

Every candidate query reads the digest's `blob_gc_queue.enqueued_at` along with it, as `$seen`:
the incremental query from the queue row, the full sweep through a left join (NULL when the
digest has no entry). Step (3) uses it to keep a release that commits during the run.

**Per-blob protocol.**

```sql
BEGIN ISOLATION LEVEL READ COMMITTED;
-- (1) lock: waits for every transaction that holds FOR SHARE on the row (an upload or copy
--     that is adding a reference) and blocks new ones until this transaction ends
SELECT state FROM blobs WHERE sha256 = $1 FOR UPDATE;
-- (2) separate statement, so it runs on a snapshot taken after the lock was granted;
--     the table list comes from the foreign keys that point at blobs, read in this transaction
SELECT EXISTS (SELECT 1 FROM items WHERE blob_sha256 = $1)
    OR EXISTS (SELECT 1 FROM retention_runs WHERE blob_sha256 = $1)
    OR …;
-- (2b) only when (2) returned false; a separate statement started after (2):
--      the hold of a running backup (backup-and-restore.md R-BAK-007)
SELECT EXISTS (SELECT 1 FROM backup_run r
                WHERE r.hold = 'all' AND r.lease_expires_at > now())
    OR EXISTS (SELECT 1 FROM backup_blob_pin p JOIN backup_run r ON r.id = p.run_id
                WHERE p.sha256 = $1 AND r.hold = 'pinned' AND r.lease_expires_at > now());
-- (3) only when (1) returned state = 'live', (2) returned false and (2b) returned false
--     now() is the transaction's start: the claim time of the hand-over rules below
UPDATE blobs SET state = 'deleting', state_changed_at = now() WHERE sha256 = $1;
--     and its copies with it (R-STOR-057); a copy a drain claimed keeps its claim time
UPDATE blob_copies SET state = 'deleting', state_changed_at = now() WHERE sha256 = $1 AND state <> 'deleting';
--     also when (2) found a reference or (1) found no 'live' row; not when (2b) held it;
--     only the entry the candidate query read, never one a later release wrote
DELETE FROM blob_gc_queue WHERE sha256 = $1 AND enqueued_at = $seen;
COMMIT;
-- (4) outside any transaction; 204 and 404 both count as success; once per copy row, on its
--     connection, each success deleting its copy row; a copy whose delete is never sent is released (R-STOR-057)
S3 DeleteObject <copy's object_key>
-- (5) when the blob has no copy row left
UPDATE blobs SET object_deleted_at = now() WHERE sha256 = $1 AND state = 'deleting';
-- (6) later, once object_deleted_at < now() - storage.gc.delete_settle; in the same transaction
--     the blob's rows of blob_copy_queue (R-STOR-052)
DELETE FROM blobs WHERE sha256 = $1 AND state = 'deleting';
```

**Why it is safe.**

1. *A reference is only ever committed to a `live` row, under a `FOR SHARE` lock held until
   commit.* The guard trigger enforces this in the database for every referencing table.
2. *`live` → `deleting` happens only under `FOR UPDATE` and after a reference check whose
   snapshot was taken after the lock was granted.* `FOR UPDATE` conflicts with `FOR SHARE`
   ([PostgreSQL row-level locks](https://www.postgresql.org/docs/current/explicit-locking.html#LOCKING-ROWS)).
   A transaction that added a reference either committed before the lock was granted, so the
   check in step (2), which under READ COMMITTED takes a fresh snapshot per statement
   ([isolation levels](https://www.postgresql.org/docs/current/transaction-iso.html#XACT-READ-COMMITTED)),
   sees it and the blob is kept; or it requests its `FOR SHARE` lock later, waits for the
   collector to commit, then re-reads the row, finds `deleting`, and the guard fails. The commit
   protocol (R-STOR-005) then waits for the row to disappear and writes the object again. The
   check MUST therefore be a separate statement after the lock, and the transaction MUST NOT
   run at REPEATABLE READ or SERIALIZABLE (their snapshot would predate the lock).
3. *`deleting` is terminal; the object is deleted only in that state; the row disappears only
   after the delete succeeded and `delete_settle` has passed.* A new row for the same digest can
   be inserted only after that (primary key), and its uploader writes the object only after it
   inserted or found the new row, that is, after the delete. Under the consistency assumptions of
   R-STOR-035 the later write survives. `delete_settle` covers a delete request still in flight in
   a proxy or SDK retry when the collector already saw a success.
4. *A row becomes `live` only after its object write succeeded* (R-STOR-005, R-STOR-006).

From 1–4: every committed reference points at a `live` row whose object exists. Damage from
outside Quader (objects deleted or altered directly in the bucket) is detected by the scrub
(R-STOR-027), not prevented.

The hold check (2b) only ever keeps a blob, so it cannot weaken 1–4. It gives a running backup
liveness: a blob that the backup's snapshot references stays in the bucket until the backup has
copied it. [backup-and-restore.md](backup-and-restore.md) R-BAK-007 proves this and needs from
this protocol only that (2b) starts after (2) of the same blob. A held blob stays `live`, its
queue entry stays, `quader_gc_candidates_held_total` grows, and the next run considers it again.
A run that finds a valid `all` hold when it reaches its candidates skips them and ends; the
per-blob check still guards the race, and the early exit only saves the locking.

**No release is lost from the queue.** The queue row is deleted only while its `enqueued_at`
still equals `$seen`. Take a digest whose reference check (2) found a reference R. A transaction
that removes R and commits during the collector's transaction moves the queue row's
`enqueued_at` forward through the release trigger. Without the condition, the collector's
`DELETE … WHERE sha256 = $1` would, under READ COMMITTED, either see the updated row (the release
committed before the statement began) or wait for the releasing transaction's row lock and
re-evaluate its condition against the updated row; either way it would match and delete the
entry. The blob would then be unreferenced with no queue entry, and only the next full sweep
(R-STOR-025) would reclaim it. With the condition, the updated row does not match, the entry
stays, and the next incremental run handles the digest. A NULL `$seen` deletes nothing, so an
entry written after a full-sweep candidate was read also stays. `$seen` is passed back exactly as
the candidate query returned it, never recomputed. The condition decides only how soon a blob is
reclaimed: rules 1–4 above do not rely on the queue, so safety is the same with or without it.

**Recovery and hand-over between runs.** Every step is idempotent. A collector that crashes after
(3) leaves a `deleting` row; a later run repeats (4)–(6). Rows in `deleting` are processed on every
run before new candidates, and before a run ends it waits up to `delete_settle` to finish (6) for
the rows it deleted and (4)–(6) for handed-over rows that became eligible meanwhile, so an upload
waiting for such a row (R-STOR-005) is not left until the next scheduled run. With several
`worker` replicas and node drains at any time (INV-12), a collection run regularly moves from one
worker to another (R-ARCH-016), so the hand-over has rules of its own:

- A collector reads its own monotonic clock before it begins a claim transaction; that reading
  is no later than the transaction's start, which (3) stores as `state_changed_at`. It sends (4)
  for a row it claimed itself only while less than `storage.gc.handover_delay −
  storage.gc.delete_settle` has passed on that clock since the reading, checks this immediately
  before the call, and gives the call that instant as its deadline. In batch mode the earliest
  reading of the batch counts. A row it did not delete in time is left to a later run. The
  collector never compares its clock with the database's, and a lock wait inside the claim
  transaction only shortens its window.
- A run finishes a `deleting` row without `object_deleted_at` that it did not claim itself only
  after the row's `state_changed_at` is older than `handover_delay`.
- On shutdown, the collection handler cancels its S3 calls and waits for them to return before its
  job is released ([architecture.md](architecture.md) R-ARCH-016), so it sends nothing after the
  release. In the same step it records (5) for the rows whose (4) succeeded and hands its other
  claims over: one statement sets
  `state_changed_at = now() − (handover_delay − delete_settle)` on them, so the run that
  continues the job finishes them after `delete_settle` instead of `handover_delay`. A drain therefore delays the re-upload of a digest that was being collected
  by seconds, not minutes. If that statement fails, the rows wait for `handover_delay`, as after a
  crash.
- A run that is in its wait for `delete_settle` when its replica begins its shutdown ends the wait at
  once and its job is released ([architecture.md](architecture.md) R-ARCH-024 step 1, Q-ARCH-501):
  the rows it deleted are removable `delete_settle` after their delete whichever run finishes them,
  so the run that continues the job on another worker does what the wait would have done, and the
  stopping worker holds nothing for it (Q-STOR-217).

Without these rules, a collector that was drained, paused or partitioned after (3) could send its
delete late. By then another run could have finished the row, and an uploader could have written
the same digest again for a new `live` row (rule 3). The late delete would remove that new object,
and a committed reference would point at nothing. Under the rules, the new row can exist only
after `handover_delay`, or `delete_settle` after a release that followed the return of all the
collector's calls; by then the first collector's delete has either completed or will not be sent,
and `delete_settle` still absorbs a request already in flight. The remaining gap is a
process frozen between its deadline check and the write of the request itself, which the scrub
detects (R-STOR-027). An upload that meets such a row waits `storage.upload.blob_wait_timeout`,
then answers `503` and is retried by its client (R-STOR-005). After a drain the wait is short,
because the drained collector handed its rows over. After a crash, uploads of the digests the lost
collector had claimed can answer `503` for up to `handover_delay` plus one `gc` interval; this
touches only content that was unreferenced for the whole grace period and is uploaded again in
that window.

**Grace period.** The protocol is correct without it; the grace period exists for three other
reasons: (a) content deleted and re-published shortly after (common in CI) is still stored, so
the re-upload is a deduplication hit; (b) a point-in-time restore of the platform's database
backup ([ha-and-helm.md](ha-and-helm.md) R-HA-023 to R-HA-025) to any moment within the last
`grace_period` finds every blob it references still in the bucket, because a blob is deleted no
earlier than `grace_period` after its last reference disappeared (longer restore windows need
bucket versioning with noncurrent-version expiry at least that long, R-HA-024); (c) defense in
depth. Quader's own backups do not depend on it: the hold (2b) keeps every blob a running backup
still has to copy, whatever the grace period is, and a completed backup holds its blobs in its own
target ([backup-and-restore.md](backup-and-restore.md) R-BAK-006, R-BAK-007).

**Deletion pause.** Before every batch of state changes (trash purge, (3), (4) and (6)) a run reads
`system_state.deletions_paused_at` ([ha-and-helm.md](ha-and-helm.md) R-HA-025). While it is set,
the run changes nothing and ends with `result.paused: true`; the next run after an operator
resumes deletions continues.

**Deletion batching.** The collector MAY claim up to `storage.gc.batch_size` blobs, one
transaction each or one transaction for the batch with the locks taken in digest order, and
delete their objects with `DeleteObjects` (up to 1,000 keys per request, per connection) when the
connection's `batch_delete` is true (R-STOR-050). In batch mode one hold-check statement covers the batch
(`p.sha256 = ANY($1)`, returning the held digests), issued after the batch's last reference check
(R-BAK-007), and one queue deletion pairs each digest with its own `$seen`
(`DELETE FROM blob_gc_queue q USING unnest($1::bytea[], $2::timestamptz[]) AS s(sha256, seen)
WHERE q.sha256 = s.sha256 AND q.enqueued_at = s.seen`), leaving out the held digests.

**Relation to other jobs.** Retention runs, deletes, copies and backup runs may run concurrently
with collection; the protocol needs no mutual exclusion beyond the row locks and the hold check.
Two collection runs that overlap, for example while a lost worker's run is still finishing, are
safe for the same reason; the exclusive job key of R-STOR-025 only avoids the duplicate work, and
no advisory-lock lease or Valkey lock is involved (Q-STOR-13). Every statement runs in a short
transaction on the main pool, so collection works through a transaction-mode pooler. The
physical-usage delta of a blob is recorded when its row leaves `live` in step (3) (R-STOR-003).

Configuration:

```yaml
storage:
  gc:
    grace_period: 24h        # default (proposed)
    delete_settle: 30s       # default (proposed); at least 1 s
    handover_delay: 10m      # default (proposed); must exceed jobs.lease_duration + 2 × delete_settle
    batch_size: 1000         # default (proposed)
    delete_concurrency: 8    # default (proposed)
```

Metrics (the `quader_gc_*` family of
[operations-and-observability.md](operations-and-observability.md) R-OPS-008):
`quader_gc_deleted_blobs_total`, `quader_gc_reclaimed_bytes_total`,
`quader_gc_candidates_kept_total`, `quader_gc_candidates_held_total` (kept by a backup hold,
R-BAK-007), `quader_gc_deleting_rows`, `quader_gc_copies_released_total{connection}` (R-STOR-057).

Acceptance criteria:

- The race test of R-STOR-005 passes, including a variant in which the collector process is
  killed at random points.
- A deleted item's blob still exists 23 hours later and is gone after the first collection run
  past 24 hours (with defaults and the trash can disabled).
- A test that inserts a reference row with `blob_sha256` of a `deleting` blob through plain SQL
  fails with SQLSTATE `AS001`.
- A test migration adds a new referencing table while an old binary runs collection; a blob
  referenced only from the new table is kept.
- The hold criteria of [backup-and-restore.md](backup-and-restore.md) R-BAK-007 pass: with a
  backup paused after its pins are committed, a pinned, unreferenced blob stays `live` through a
  collection run with `grace_period: 0s`, and the first run after the backup deletes it.
- A fault-injection build pauses a collector right after its reference check (2) found an item
  that references the digest. A second transaction deletes that item and commits, and the
  collector then continues. Afterwards `blob_gc_queue` holds the digest with the second
  transaction's `enqueued_at`, and the next incremental run with `grace_period: 0s` deletes the
  blob without a full sweep.
- A fault-injection build pauses a collector between (3) and (4) with `kill -STOP` for longer than
  `handover_delay`, while another worker takes over the run and an uploader re-publishes the same
  content. After `kill -CONT`, the paused collector sends no `DeleteObject` (S3 request log), and
  the re-published item downloads with the right SHA-256.
- A fault-injection build holds a collector's `DeleteObjects` call after (3), and its pod is
  deleted gracefully. The pod sends no `DeleteObject` after its job is released; the run continues
  on the other `worker` replica and removes the claimed rows within `2 × delete_settle` plus one
  job pick-up, and an upload of one of those digests started during the drain answers `201`
  within `storage.upload.blob_wait_timeout`.
- With `system_state.deletions_paused_at` set, a collection run with `grace_period: 0s` changes no
  row and deletes no object, and its result carries `paused: true`.

### R-STOR-025 · Garbage-collection schedule, full sweep and manual run
Priority: **MVP** · Covers: STOR-037

Collection runs on the shared task framework (Q-STOR-10): the job table, scheduler and job leases
(`locked_until`) of [architecture.md](architecture.md) R-ARCH-016, the task and job API of
[operations-and-observability.md](operations-and-observability.md) R-OPS-019 and R-OPS-020. This
requirement fixes the two collection tasks, their parameters and results.

- Two tasks: `gc` (incremental) and `gc-full` (full sweep). Both are registered in the queue
  `maintenance` with the dedup key `gc` in `exclusive` mode, so at most one collection run is
  queued or running at any time, whichever task started it, on whichever `worker` replica claims
  it (R-STOR-046). The exclusion lives in the job table in PostgreSQL (INV-3); it avoids duplicate
  work, while correctness rests on the row locks of R-STOR-024 (Q-STOR-13).
- Each run first purges expired trash entries (R-STOR-022), then aborts expired upload sessions
  (R-STOR-011, from P1 on), then finishes `deleting` rows under the hand-over rules of R-STOR-024,
  then processes candidates. The deletion pause of R-STOR-024 stops every step; the backup hold
  (R-STOR-024 step 2b) applies to the blob candidates, the only step that can remove content a
  running backup still has to copy.
- A node drain releases a running collection job ([architecture.md](architecture.md) R-ARCH-016):
  `gc-full` continues from its keyset checkpoint on another worker; `gc` needs no checkpoint,
  because its candidates are the remaining `blob_gc_queue` entries.
- A scheduled slot that finds the other collection task queued or running is refused by the shared
  key, not dropped: the scheduler offers a task's latest due slot again on every tick
  ([architecture.md](architecture.md) R-ARCH-016), so it is enqueued once the blocking run has
  ended. A full sweep whose slot collides with an incremental run therefore starts after that run.
  Incremental slots that fall inside a full sweep collapse into the first slot after it, which loses
  nothing because the sweep deletes everything those runs would have deleted.
- The full sweep checkpoints its keyset position into the job's `checkpoint`; a run that a lost
  replica returns to the queue continues where it stopped.
- Trigger and observation (permission: system administrator for `POST`, `system.read` for `GET`):

  | Method | Path | Body | Result |
  |---|---|---|---|
  | `POST` | `/api/v1/system/tasks/gc/run`, `/api/v1/system/tasks/gc-full/run` | `{"dryRun": false}` | `202 {"job": {…}}`; `409 {"job": {…}}` with the active job while any collection run is queued or running |
  | `GET` | `/api/v1/system/jobs?kind=gc`, `?kind=gc-full` | `state`, `cursor`, `limit` | runs; each job's `result` carries `dryRun`, `trashPurged`, `candidates`, `kept`, `deleted`, `bytesReclaimed`, `errors` |
  | `GET` | `/api/v1/system/jobs/{id}` | — | one run |
  | `POST` | `/api/v1/system/jobs/{id}/cancel` | — | `202`; the run stops after the current batch |
  | `GET` | `/api/v1/system/tasks` | — | `schedule`, `enabled` and `nextRunAt` of both tasks |

- A dry run reports what would be deleted and changes nothing, the trash purge included.

**Schedule syntax** (Q-STOR-4). Every schedule in Quader uses this dialect and one parser:
maintenance tasks (`maintenance.tasks.<name>`), retention policies (R-STOR-040) and any later
schedule.

- Five fields separated by spaces: minute (0–59), hour (0–23), day of month (1–31), month (1–12
  or `JAN`–`DEC`), day of week (0–6 with 0 = Sunday, or `SUN`–`SAT`). Names are case-insensitive.
- A field is `*`, a value, an ascending range `a-b`, a step `*/n` or `a-b/n`, or a comma-separated
  list of these.
- Day matching follows [POSIX `crontab`](https://pubs.opengroup.org/onlinepubs/9799919799/utilities/crontab.html):
  when both day of month and day of week are restricted (neither field starts with `*`), a day
  matches if either field matches; otherwise both must match.
- Macros: `@yearly` (also `@annually`), `@monthly`, `@weekly`, `@daily` (also `@midnight`) and
  `@hourly`, with the meanings of
  [Kubernetes CronJob schedules](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#schedule-syntax).
- Evaluated in UTC. There is no time-zone setting and no `TZ=` or `CRON_TZ=` prefix.
- Rejected (`422` `invalid_schedule` through the API; a configuration error naming the key at
  startup): six- or seven-field expressions (seconds or year field), `?`, `L`, `W`, `#`, the
  day-of-week value `7`, descending ranges, `@every` and `@reboot`, and an expression that does
  not fire within four years of the time it is saved (`0 0 30 2 *`). The error text states the
  dialect, including "day of week 0–6, Sunday = 0".
- Rejecting `?`, `7` and six-field forms is deliberate: a Quartz-style expression, whose day of
  week counts from 1 = Sunday, fails loudly instead of running on the wrong day.

Configuration:

```yaml
maintenance:
  tasks:
    gc:      {enabled: true, schedule: "*/30 * * * *"}   # default (proposed): incremental every 30 minutes
    gc-full: {enabled: true, schedule: "0 2 * * 0"}      # default (proposed): full sweep Sundays 02:00 UTC
```

Acceptance criteria:

- `curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' -d '{"dryRun":false}' …/api/v1/system/tasks/gc-full/run`
  answers `202`; `POST …/api/v1/system/tasks/gc/run` while it runs answers `409` with the same job
  ID.
- With two replicas, each scheduled slot of `gc` appears exactly once in
  `GET /api/v1/system/jobs?kind=gc` over a 24-hour window without a `gc-full` run, and no slot
  ever appears twice.
- With both default schedules, which both fire on Sundays at 02:00 UTC, a `gc-full` job for that
  slot runs every Sunday, and no `gc` job and `gc-full` job ever run at the same time.
- `kubectl drain` of the node whose `worker` runs a `gc-full` job: the job continues on the other
  `worker` replica from its checkpoint with `attempt` unchanged, and afterwards exactly the blobs
  are gone that an undisturbed run deletes over the same fixture.
- Parser table test: `0 3 * * 1` and `0 3 * * mon` fire on Mondays at 03:00 UTC; `0 0 1 * 1` fires
  on the first of each month and on every Monday; `0 3 * * 7`, `0 30 3 * * ?`, `30 3 ? * 2`,
  `@every 1h`, `CRON_TZ=Europe/Berlin 0 3 * * *` and `0 0 30 2 *` are rejected.

### R-STOR-026 · Orphan sweep
Priority: **P1** · Covers: STOR-038, OPS-067

The reverse check: walk each connection and remove the objects no row stands for, or adopt the object of a live blob whose copy row is
missing. *Built* (package B of stage 1): [sweep.go](../../internal/blob/sweep.go) (`Collector.Sweep`), the job adapter `RunSweepJob`
([gcjob.go](../../internal/blob/gcjob.go)), the registration in [wire_storage_sweep.go](../../internal/app/wire_storage_sweep.go), the
statements in [sweep.sql](../../internal/meta/postgres/queries/sweep.sql).

- **Per connection.** The sweep lists `<prefix>/sha256/<h0h1>/` of each connection in the worker's configuration with `drain: false`
  or `drain: true` (256 top-level prefixes each, `ListObjectsV2`), connection after connection in list order, and checkpoints
  `{connection, prefix}` into the job's `checkpoint` after each prefix, and `{connection, "staging"}` after the connection's staging area.
  A connection whose circuit is open is skipped and named in the result (`circuit_open`); one whose store fails during the walk is named
  with what was done (`unavailable`); the others go on. Before it walks a connection the sweep reads its marker (R-STOR-051): a marker that
  names another connection or installation leaves the connection alone (`foreign_marker`), so the sweep never adopts or deletes in a
  location that is not this installation's
  ([Q-STOR-262](#q-stor-262--the-sweep-acts-on-evidence-a-listing-alone-does-not-give)). An absent marker is no such evidence.
- **Each listed object** is looked up by `(connection_id, object_key)` in `blob_copies` and, by the digest its name carries, in
  `blobs` ([Q-STOR-238](#q-stor-238--the-orphan-sweep-adopts-the-object-of-a-live-blob-and-claims-an-orphan-through-the-blob-row));
  on an encrypted connection with keyed names, whose names carry no digest, the sweep lists `keyed/` and learns the digest of an object
  without a copy row from its record (R-STOR-087):
  - **A copy row exists**: a listed size other than `stored_size` of a `present` copy, confirmed by a `HeadObject`, marks the copy
    `corrupt` and queues its repair (R-STOR-056); otherwise nothing. A copy row in state `deleting` of a live blob is a drain's claim and
    is left to it.
  - **No copy row, the blob `live`**: the object is adopted: one transaction locks the blob as a mark does (`FOR NO KEY UPDATE`,
    R-STOR-052; it must still be `live`), inserts the copy row `present` and not verified, or `corrupt` with a queued repair when its size,
    confirmed by a `HeadObject`, differs from `blobs.size`, and re-derives the blob's content state, so a blob that no connection held
    whole is whole again. This is how an object written by a commit that lost its round, by a reconciliation run that crashed before its
    row, or after a point-in-time restore of the database, is counted again. On a connection with `drain: true` nothing is adopted: an
    object a drain deleted may still be listed by a store that applies deletes late, and the drain needs no copy there (Q-STOR-262).
  - **No copy row, the blob `deleting`**: skipped; collection or a drain owns it.
  - **No blob row**, and `LastModified` older than the run's `minAge` (default `storage.orphans.min_age`; an object whose store gives no
    time is left): the sweep MUST claim the digest by inserting a `deleting` blob row (`INSERT … ON CONFLICT DO NOTHING`; the digest
    columns other than `sha256` are zero bytes of their length and `size` is the listed size, never read for a `deleting` row, which never
    counts in the physical totals, R-STOR-003), and in the same statement the copy row of the object in state `deleting`, before it deletes
    the object on that connection, then follow steps (4)–(6) of R-STOR-024 with its hand-over rules; the copy row lets collection's step
    (4) finish a delete that a sweep left (R-STOR-057). The rows whose delete has settled are dropped after each prefix. An uploader that wrote
    the same object concurrently finds the `deleting` row and writes again after it is gone (R-STOR-005); the commit deadline keeps freshly
    written objects out of reach of `min_age`. An insert that did nothing (the row exists) leaves the object for the next run. The sweep
    needs no backup-hold check: it deletes only objects without a blob row, and a backup's snapshot references none of them
    ([backup-and-restore.md](backup-and-restore.md) R-BAK-007).
- The sweep honours the deletion pause of R-STOR-024 before every prefix: once it is set, the sweep deletes and aborts nothing for the
  rest of the run, walks on, ends with `result.paused: true` and saves no checkpoint past the last prefix it walked before the pause, so
  that a later run with `resume: true` continues there. Adoptions and marks are not deletions and continue
  ([Q-STOR-261](#q-stor-261--under-the-deletion-pause-the-sweep-walks-on-adopting-and-marking-and-a-resume-starts-where-it-stopped-deleting)).
- Keys below `<prefix>/sha256/` that do not match the layout, and any other key below `<prefix>/` outside the families of R-STOR-059, are
  reported (the log names the first 20 per connection, `foreignKeys` counts all) and never deleted.
- **The staging area per connection.** The sweep also aborts multipart uploads below each connection's `<prefix>/` initiated longer than
  `storage.orphans.multipart_max_age` ago, and deletes the objects below `_uploads/` of sessions that are aborted, done, unknown or pinned
  to another connection (R-STOR-011, R-STOR-054), and the leftover probe objects below `_probe/`, older than
  `storage.uploads.session_max_age`. An open session's multipart upload is never that old, because `session_max_age` is shorter than
  `multipart_max_age`. A store that documents no lifecycle rule to abort incomplete uploads (the owner's provider lists none, a research
  of 2026-11-22, not verified here) relies on this step. It replaces the pass the full collection sweep made over the first connection's
  `_uploads/` until the sweep was a task of its own
  ([Q-STOR-263](#q-stor-263--the-staging-sweep-moves-from-the-full-collection-to-the-orphan-sweep-per-connection-without-a-cursor-of-its-own)).
- The sweep is the task `orphan-sweep` of the shared task framework (Q-STOR-10; R-OPS-019). Trigger:
  `POST /api/v1/system/tasks/orphan-sweep/run` with `{"dryRun": false, "resume": true, "minAge": "24h", "connection": ""}` (all members
  optional; the values shown are the defaults, `minAge` defaulting to `storage.orphans.min_age`, an empty `connection` meaning every one)
  → `202 {"job": {…}}`, `409` with the active job. Observation and stop through `/api/v1/system/jobs/{id}` and
  `/api/v1/system/jobs/{id}/cancel`. The job `result` carries `dryRun`, `paused`, `resumedFrom` (the cursor it started after), `errors`,
  per connection `prefixesDone`, `prefixesTotal`, `objectsSeen`, `orphansFound`, `orphansDeleted`, `bytesReclaimed`, `adopted`,
  `copiesMarked`, `multipartAborted`, `stagingDeleted` (Q-STOR-263) and `foreignKeys`, and the skipped connections as `{connection,
  reason}`. A dry run counts what it would adopt, mark, delete and abort and changes nothing. Permission: system administrator. The kind
  is exclusive under its own name: a sweep may run beside a collection run, both resting on the row locks of R-STOR-024.
- `resume: true` starts at the checkpoint of the most recent `orphan-sweep` job with the same `connection` and `dryRun` that ended
  `cancelled`, `failed` or paused, if it is newer than the most recent one with those arguments that walked to its end; otherwise the
  sweep starts at prefix `00` of the first connection. Scheduled runs use `resume: true`. An attempt that the framework retries or a drain
  hands to another worker continues from its own checkpoint first.
- A `minAge` not greater than twice `storage.upload.commit_deadline` is rejected with `422`: a shorter age could reach objects whose
  uploader has not yet inserted their row (R-STOR-005). A `connection` that `storage.connections` does not name is rejected with `422`
  as well. The run refuses such a `minAge` itself, for a job that reached the table another way.

Configuration:

```yaml
storage:
  orphans:
    min_age: 24h              # default (proposed); must be > 2 × storage.upload.commit_deadline
    multipart_max_age: 24h    # default (proposed)
maintenance:
  tasks:
    orphan-sweep: {enabled: true, schedule: "0 5 * * 6"}   # default (proposed): Saturdays 05:00 UTC
```

Acceptance criteria (integration, PostgreSQL with two in-memory stores, a fake clock; `TestR_STOR_026_OrphanSweep`):

- An object put under `quader/sha256/00/00/<64 hex>` on the second connection, with no blob row, is reported by a dry run and removed by
  a sweep once older than `min_age`; an object with a non-hex name under `sha256/` is only reported.
- The object of a live blob on the second connection without a copy row is adopted: the row is `present` and not verified, and the scrub
  reads it first.
- A sweep cancelled at prefix `7f` of the second connection and triggered again with `resume: true` continues there without re-listing
  the first connection or earlier prefixes.
- With the first store's circuit open, the sweep walks the second connection and names the first as skipped.
- `POST …/tasks/orphan-sweep/run` with `{"minAge": "1h"}` and the default `commit_deadline` of 1h answers `422` (unit,
  `internal/blob` `TestR_STOR_026_TheParametersOfTheOrphanSweep` and `internal/api/v1` `TestTaskParameters`).

### R-STOR-027 · Integrity scrub
Priority: **P1** · Covers: quader-only

The owner's decision 10 of 2026-11-22: a worker job reads every copy once within a period, throttled, checks it whole, marks damage and
replaces a damaged copy from another connection. **Built** (package D of the build plan,
[docs/developer/blob-store.md](../developer/blob-store.md#the-scrub-r-stor-027)): the job with its repair, the gauge of the overdue copies,
the audit event of a detection, the list of damaged blobs and the recheck; the gauges of the storage summary named below (`quader_storage_blobs_damaged{status}`, `quader_storage_copies{connection,state}`) are built with package F (R-STOR-028). Beside the scrub, the
downloads detect (R-STOR-013), a missing object is marked and rechecked after 30 s (R-STOR-012, Q-STOR-151, Q-STOR-153), and the operator
check moved to R-STOR-058.

- **Per copy, whole.** Every `present` copy on a connection in the worker's configuration is read to its end through the connection's
  reader (R-STOR-062) at least once per that connection's `scrub_period` (default `720h`), and its SHA-256 and length are compared with the
  blob's digest and the copy's `stored_size`. Due copies are those whose `verified_at` is `NULL` or older than the period, oldest first:
  copies never verified (a server-side copy, an adoption, a write without an enforced payload checksum) come first
  ([Q-STOR-239](#q-stor-239--the-scrub-reads-every-copy-whole-once-per-period-per-connection-and-has-no-separate-existence-check)).
- **Outcome.** A copy is judged by its bytes alone: their number against `stored_size` and their SHA-256 against the digest, at most
  `stored_size + 1` bytes read; nothing the store says about the object (a `Content-Length`, an ETag, a checksum header) counts, because a
  store may have no upload checksum of its own. A match stamps `verified_at` and `checked_at`, unless the row was written again since the
  read began (another `created_at`). A digest or length mismatch marks the copy `corrupt`, "no such object" marks it `missing`, each with
  the counter `quader_storage_copies_damaged_total{connection,status}` and a queued repair (R-STOR-056); the blob's state follows by
  R-STOR-052. A timeout, a `5xx`, throttling or a cut transfer changes nothing; the copy stays due and is counted in
  `quader_storage_scrub_copies_total{connection,result="error"}` (the other results `ok`, `corrupt`, `missing`); an open circuit ends that
  connection's reader for the run. `quader_storage_scrub_read_bytes_total{connection}` counts the bytes. A mark is the store's one mark of
  every reader (`MarkCopyDamaged`): a "no such object" is looked for once more with `HeadObject` before it stands.
- **A copy marked missing is read again.** Before its walk, each run reads the copies of its connection marked `missing` whose store last
  answered for them more than a day ago (`checked_at`): one whose object reads whole again is `present` and verified and the blob follows,
  one still gone or damaged waits another day, so a store's wrong "no such object" heals without a read of the blob
  ([Q-STOR-265](#q-stor-265--the-scrub-replaces-a-copy-it-found-damaged-in-the-same-run-and-reads-back-what-it-wrote-before-the-copy-counts)).
- **Repair from another connection** ([Q-STOR-265](#q-stor-265--the-scrub-replaces-a-copy-it-found-damaged-in-the-same-run-and-reads-back-what-it-wrote-before-the-copy-counts)).
  The scrub replaces a copy it marked in the same run, on a connection without `drain: true`, from a `present` copy on another reachable
  connection (verified before not verified, then by list order): the source is read to its end through the worker and checked against
  the digest while it is written (one `PutObject` up to a part, a multipart upload of ranged reads above, one part held in memory), a
  source found damaged is marked and the next one tried, and the written object is read back whole before anything is recorded. The copy
  row becomes `present` and verified, and its queue row goes, only while it is still the row that was found damaged (the same state and
  the same time of that change), so no insert bound is needed. A failed write, a read-back that does not match (a store that applies an
  overwrite late) or no source leaves the copy marked and queued for the reconciliation (R-STOR-056);
  `quader_storage_scrub_repairs_total{connection,outcome}` counts `repaired`, `failed` and `no_source`. With one connection, or when no copy
  is whole, the blob is `missing` or `corrupt` and is listed by the API below; it is repaired as before: a later upload of identical
  content rewrites the object (R-STOR-005), a remote repository treats the item as a cache miss and refetches it
  ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-025 "Lost content", Q-REPO-202), and a `missing` mark heals by itself when
  the object comes back (R-STOR-055).
- **The job.** Task and kind `scrub` in the queue `maintenance`, `exclusive`, scheduled hourly. A run reads every connection at once, one
  reader per connection, each throttled to that connection's `scrub_bytes_per_second`, so a slow provider does not hold back a fast one;
  a run ends after `storage.scrub.run_duration` and the next continues. In a steady state about a 720th of a connection's copies becomes
  due every hour.
- **What it costs.** Each connection is read in full once per period: a connection holding 2 TiB reads 2 TiB per 30 days, about
  0.8 MiB/s on average, in hourly runs throttled to `scrub_bytes_per_second`. At a provider outside the installation's network that is
  egress traffic; its price is the provider's (the owner's provider lists no egress charge in its price list, and a page that said traffic
  is free now redirects; a research of 2026-11-22, not verified). The reads count against the provider's request and bandwidth limits that
  serving shares (documented there as 2,500 requests per second, 500 concurrent requests and 500 MB/s per project, same research). A
  connection whose present bytes exceed `scrub_period × scrub_bytes_per_second × run_duration / 1h` cannot keep up:
  `quader_storage_scrub_overdue_copies{connection}` grows and `QuaderStorageScrubBehind` fires. A copy is overdue when its last
  verification, or its write when it was never verified, lies more than its connection's `scrub_period` and one day back; every replica
  that has the connection counts them from PostgreSQL
  ([Q-STOR-266](#q-stor-266--a-copy-is-overdue-one-day-after-its-period-and-every-replica-counts-the-overdue-copies-from-postgresql)).
- **Detection writes an audit event per blob** whose state changes (here, in R-STOR-013, and a read that the store answers "no such
  object", [ha-and-helm.md](ha-and-helm.md) R-HA-024, Q-STOR-151, and the reconciliation's source, R-STOR-056):
  `system.storage.blob_damaged` with the digest and the state, actor `system:replica`, outcome `warning`, asynchronous because the store's
  damage hook runs after the mark's transaction committed; and increments `quader_storage_blobs_damaged_total{status}`. A copy's damage
  that leaves the blob `ok` is a metric and a log line, not an audit event. The gauge `quader_storage_blobs_damaged{status}`
  reports the current number of damaged blobs, and `quader_storage_copies{connection,state}` the copies per state, from the latest storage
  summary (R-STOR-028).
- **The list of damaged blobs.** `GET /api/v1/storage/damaged-blobs?scope=blobs|copies&connection=&status=&cursor=&limit=` (permission
  `storage.read`, which is the capability `settings:read`,
  [Q-STOR-267](#q-stor-267--the-damaged-blobs-list-is-read-with-settingsread-and-the-recheck-writes-no-object)): `scope=blobs` (default) lists the blobs no connection holds whole (`content_status` other than
  `ok`); `scope=copies` lists the blobs with at least one `missing` or `corrupt` copy, the self-healing view. Each entry carries `sha256`,
  `size`, `contentStatus`, `copies[]` (`connection`, `state`, `verifiedAt`, `checkedAt`, `stateChangedAt`), the first 20 referencing items
  (`project`, `repository`, `path`, `trashed`) and `itemCount`
  ([Q-API-386](api-and-ui.md#q-api-386--the-damaged-blobs-list-has-two-scopes-and-shows-each-copy)).
  `POST /api/v1/storage/damaged-blobs/{sha256}/recheck` reads every copy of that blob on the answering replica's connections whole at once,
  unthrottled, sets the states by what it finds, queues the repairs, and answers with the copies and the content state they give
  (permission `settings:write`, the system administrator's): a present copy that matches is stamped, one that does not is marked; a
  `missing` or `corrupt` copy that now reads whole is `present` again, one that still does not keeps its mark; it writes no object
  (Q-STOR-267).
- **Trigger.** `POST /api/v1/system/tasks/scrub/run` with `{"connection": ""}` (optional; empty: every connection) runs the due reads; a
  connection the worker does not configure fails the job at once. Observation and stop through the job API; the job's result carries, per
  connection, `read`, `verified`, `corrupt`, `missing`, `errors`, `bytes`, `repaired`, `repairsFailed` and why its reader `ended`
  (`done`, `duration`, `draining`, `circuit_open`, `cancelled`, `error`). Permission: system administrator (`settings:write`).
- **How the evidence becomes true** (Q-STOR-240). The tests of other parts that named R-STOR-027 are renamed to what they test: the lost
  content of a remote repository (`internal/proxy/lost_test.go`, `test/integration/proxy/lost_test.go`,
  `test/integration/npm/remote_lost_test.go`, `test/integration/nuget/remote_lost_test.go`, and the subtests of
  `internal/virtual/artifact_test.go` and `internal/api/edge/content_test.go`) to R-REPO-025, the operator check
  (`internal/blob/verify/verify_test.go`) to R-STOR-058, and the mark of a download (`internal/blob/download_test.go`) to R-STOR-055. The
  requirement is claimed only by the scrub's own tests (`internal/blob/scrub/scrub_test.go`, `internal/api/v1/storage_damaged_api_test.go`,
  `internal/app/wire_storage_scrub_test.go`, `test/integration/blob/scrub_test.go`), so its line in `implemented.txt` stays true and the
  record, which the pipeline writes, needs no edit by hand.

Configuration:

```yaml
storage:
  connections:
    - scrub_period: 720h           # default; per connection (R-STOR-050)
      scrub_bytes_per_second: 50MiB  # default; per connection and run
  scrub:
    run_duration: 50m              # default
maintenance:
  tasks:
    scrub: {enabled: true, schedule: "7 * * * *"}   # default: hourly
```

Acceptance criteria:

- A copy whose bytes were replaced behind Quader's back (same length) is marked `corrupt` by a scrub run, the blob stays `ok`, the scrub's
  repair rewrites the copy from the other connection and records it verified, and the next run finds nothing due (integration, PostgreSQL
  with two in-memory stores).
- With one connection and its object deleted, a scrub run marks the copy `missing` and the blob `missing`; the damaged-blobs API lists the
  items referencing it; re-uploading the same file with `curl -T` sets both `present` and `ok`.
- A store answering `503` to every read during a run leaves every state unchanged (unit, `internal/blob/scrub`).
- With a throttle of 1 KiB/s and 4 KiB due, a run's reads take at least four seconds of the fake clock (unit; the configuration's minimum
  of 1 MiB/s is the loader's rule, the throttle's arithmetic is the same at a thousandth of the data).

## Capacity

### R-STOR-028 · Storage summary
Priority: **P1** · Covers: STOR-039

*Built* (package F of the several connections): [internal/blob/summary](../../internal/blob/summary/) (`Compute`, `Assemble`, `Save`,
`Latest`, `RunJob`, the gauges), the statements of [storage_summary.sql](../../internal/meta/postgres/queries/storage_summary.sql), the
task's registration in [wire_storage_summary.go](../../internal/app/wire_storage_summary.go), the read path in
[storage_connections_api.go](../../internal/api/v1/storage_connections_api.go); the snapshot table is migration 00109. The member `quota`
is not built ([Q-STOR-370](#q-stor-370--the-storage-summary-has-no-member-quota-until-the-quota-settings-exist)).

- The `storage-summary` task, its schedule, the snapshot table and the response envelope
  (`computedAt`, `durationMs`) are owned by
  [operations-and-observability.md](operations-and-observability.md) R-OPS-023. This requirement
  owns the figures and the read path (Q-STOR-10):
  - `GET /api/v1/storage/summary` returns the latest snapshot (permission `storage.read`; `404`
    before the first computation). Reads never compute.
  - A recomputation is started only through `POST /api/v1/system/tasks/storage-summary/run`
    (R-OPS-019; system administrator; `202` with the job, `409` with the running job). There is
    no second refresh path ([api-and-ui.md](api-and-ui.md) R-API-020: one path per function).
- The task reads the folded counters of R-STOR-003 and the partial indexes on `blobs` instead of
  scanning items, and adds the deltas the fold has not taken yet (an exact read, as R-STOR-003's pattern shows); its statements run at
  `READ COMMITTED` one after the other ([architecture.md](architecture.md) R-ARCH-019), the copies of the connections before the live
  blobs, so that an upload between them shows only as a gap and the backlog and the fill stay within their bounds
  ([Q-STOR-372](#q-stor-372--the-connections-list-derives-every-state-the-same-for-every-replica-and-counts-over-the-copies-of-live-blobs)).
  The figures of the connections are one pass over the copies of live blobs.
- Figures:

  ```json
  {
    "computedAt": "2026-10-28T09:17:00.412Z",
    "durationMs": 412,
    "blobs": { "count": 412093, "bytes": 1893341184512 },
    "logical": { "files": 1203381, "folders": 98112, "bytes": 4120938391040 },
    "deduplicationRatio": 0.4594,
    "trash": { "items": 3210, "bytes": 9123012345 },
    "pendingCollection": { "blobs": 812, "bytes": 1029381233 },
    "damaged": { "missing": 0, "corrupt": 0 },
    "quota": { "enabled": true, "capacityBytes": 5497558138880, "usedRatio": 0.3444, "warnPercent": 80, "limitPercent": 95 },
    "repositories": [
      { "project": "web", "key": "npm-local", "class": "local", "format": "npm", "files": 52011, "folders": 8812, "bytes": 81233920123, "packages": 812, "versions": 9120, "shareOfLogicalBytes": 0.0197 }
    ]
  }
  ```

  *(example values)*

  | Member | Definition |
  |---|---|
  | `blobs` | `live` blob rows: the physical content, including content referenced only from the trash can and content awaiting collection. |
  | `logical` | Untrashed file items and folders of all repositories that hold items: the local and remote ones and a virtual repository's generated items (Q-STOR-215); every reference counts, so one blob in two repositories counts twice. |
  | `deduplicationRatio` | `blobs.bytes / logical.bytes`; `0` when `logical.bytes` is 0. |
  | `trash` | Trashed items and their logical bytes (R-STOR-022). |
  | `pendingCollection` | Digests in `blob_gc_queue` plus rows in state `deleting`, with their sizes. |
  | `damaged` | Blob counts per `content_status` other than `ok` (R-STOR-027). |
  | `connections[]` | Per connection that is not retired (R-STOR-061): `id`, `presentCopies`, `presentBytes` (stored bytes), `missingCopies`, `corruptCopies`, `verifiedWithinPeriod` (present copies read whole within the connection's `scrub_period` of the computing worker, 720 h for a connection it does not configure), `scrubOverdue` (not within the period and a day, Q-STOR-266), `backlogBlobs` and `backlogBytes` (live blobs and their plain bytes without a present copy on it), `fillRatio` (`presentCopies` / `blobs.count`, `1` when there is no blob), `soleCopies` (live blobs whose only present copy lies there, [Q-STOR-371](#q-stor-371--the-summary-counts-the-blobs-only-one-connection-holds)); counted over the copies of live blobs. The copies per storage key (R-STOR-088) are kept for the connections' list and not served here. |
  | `quota` | The system quota settings of R-STOR-029 and `usedRatio = blobs.bytes / capacityBytes`; `capacityBytes` and `usedRatio` are `null` when no capacity is configured. Not built: the member is absent until R-STOR-029's settings exist (Q-STOR-370). |
  | `repositories[]` | One entry per local and remote repository, and per virtual repository that holds generated items (Q-STOR-215); a virtual repository without items is not listed. `packages` and `versions` count untrashed rows. |

- **Per-repository share (STOR-039).** `shareOfLogicalBytes` is the repository's logical bytes
  divided by `logical.bytes` of the whole installation, a ratio from 0 to 1 rounded to four
  decimals (`0` when the total is 0). It is never a share of capacity: object storage reports no
  capacity, and the configured capacity measures physical bytes, which deduplication makes
  non-additive across repositories. The shares of all listed repositories sum to 1 within
  rounding.
- Sizes are integers in bytes and ratios are numbers, never formatted strings (R-OPS-023).
- Spool usage is per replica and exported only as metrics (R-STOR-004).

Configuration: the schedule is `maintenance.tasks.storage-summary` of R-OPS-019
(`17 * * * *`, hourly, default proposed there).

Acceptance criteria:

- After uploading the same 10 MiB file to two repositories and refreshing, `logical.bytes` grows by
  20 MiB and `blobs.bytes` by 10 MiB.
- In an installation whose only content is in `npm-local`, that entry's `shareOfLogicalBytes` is
  `1`; after copying all of it to `generic-local`, both entries show `0.5` while `blobs.bytes` is
  unchanged.

### R-STOR-029 · Quotas and capacity thresholds
Priority: **P1** · Covers: STOR-040, OPS-066

An object store does not report free space through the S3 API, so capacity is configured, not
measured.

The download limits of [usage-and-limits.md](usage-and-limits.md) do not count storage: a repository's and a project's storage quota
stay with this requirement and R-STOR-049 (Later there).

- System settings (runtime, `PUT /api/v1/settings/storage` with `If-Match`; one revisioned
  document with the trash settings, R-STOR-022):

  ```json
  { "quota": { "enabled": false, "capacityBytes": null, "warnPercent": 80, "limitPercent": 95 } }
  ```

  Defaults (proposed) as shown. Used capacity is physical bytes (all `live` blobs, including
  trash and content awaiting collection).
- Per repository: `config.storage.quota` (bytes of untrashed logical content; default none).
- Above `warnPercent`: warning log, metric `quader_storage_quota_used_ratio`, an event for
  webhooks ([operations-and-observability.md](operations-and-observability.md)) and a banner in
  the UI. At or above `limitPercent`, or when a repository quota would be exceeded: every upload
  is rejected with `507 Insufficient Storage` and problem code `quota_exceeded` (type
  `/api/v1/problems/quota_exceeded`, [api-and-ui.md](api-and-ui.md) R-API-009; Q-STOR-7); deploy
  by checksum and copy into a repository also count against its repository quota.
- Status codes (Q-STOR-7): `507` for the system capacity limit and for repository quotas; `413`
  only for the per-request size limit (R-STOR-004). The rejection happens at admission, before
  the body is read, whenever the request declares its length.
- A `507` carries no `Retry-After` (nothing predicts when space is freed). The problem details
  name the limit: `quota` (`"system"` or the repository's qualified name `<projectKey>/<repoKey>`), `limitBytes` and `usedBytes`. On npm
  endpoints the body follows R-NPM-005 of [formats/npm.md](formats/npm.md) with
  `code: "quota_exceeded"` and a text that names the repository or the system limit; NuGet clients
  show the status line `507 (Insufficient Storage)`.
- Clients retry a `5xx`: npm twice by default, after 10 s and 60 s, so `npm publish` reports a
  quota failure after about 70 s; NuGet with exponential backoff over a few seconds. Every retry is
  rejected again at admission. This delay is the accepted cost of naming the cause in the status
  code (Q-STOR-7).
- Enforcement reads the folded counters of R-STOR-003, so it lags by at most one fold interval
  plus one fold iteration (`storage.usage.fold_interval`, 5 s by default); uploads committed in
  that time and uploads in flight may exceed a quota. The counters live in PostgreSQL only, never
  in Valkey or in a replica's memory, so every replica decides alike, whichever one a request
  reaches, and a Valkey failover cannot reset a quota (Q-STOR-12). One admission costs one
  primary-key read per limit that is enabled, whatever the number of replicas and the upload
  rate.
- Deletes, moves within a repository, property changes and downloads are never blocked by a
  quota.
- A project has no quota of its own: it is charged the logical size of its files and shows it ([R-STOR-049](#r-stor-049--the-storage-of-a-project),
  Q-STOR-155), and a quota on that figure is not specified yet (Q-STOR-158).
- The background passes of a mirrored remote ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-052) read the
  same counters: a repository quota bounds a mirror like `remote.mirror.maxBytes`, and a pass adds no artifact while the
  system's used capacity is at or above `warnPercent`, so that a mirror never brings the installation to the limit at which
  uploads are refused. Client fills of a mirrored remote follow the rules above.
- The spool volume is the only replica-local disk on the upload path; its thresholds are checked
  when an upload is admitted (R-STOR-004).

Acceptance criteria:

- With `capacityBytes` set to the current physical size and `limitPercent: 95`,
  `npm publish` fails with a `507` error whose printed text names the system limit; deleting
  content and running collection lets it succeed again.
- With a repository quota exceeded, `dotnet nuget push` fails with
  `507 (Insufficient Storage)` and stores nothing.
- A repository quota of 100 MiB rejects the upload that would cross it, once one fold interval
  has passed since the uploads that filled it, and accepts uploads to other repositories.

### R-STOR-049 · The storage of a project
Priority: **MVP** · Covers: quader-only

A project is charged the logical size of the files it holds (Q-STOR-155). The physical figure of the object store stays a
figure of the installation.

- **The figure.** `logicalBytes` is the sum, over the repositories of the project, of their `repository_usage.logical_bytes`
  (R-STOR-003): the sizes of the untrashed files, each in full, whether the same content is also referenced by another
  repository of the project or by a repository of another project (INV-4). It is read from the folded counters, one row per
  repository by primary key; no item is scanned, and no counter of its own is kept per project. It lags a write by at most one
  fold interval plus one iteration, as the repository counters do. A repository that is being deleted counts until its row is
  gone (R-REPO-013).
- **Where it is served.** The `Project` representation of [api-and-ui.md](api-and-ui.md) carries it as the integer
  `logicalBytes` in the list (`GET /api/v1/projects`), the read and the answers of create and replace. It follows the read
  rules of the project itself (R-AUTHZ-026): a member reads the figure of its project, a holder of `projects:read` the figure
  of every project, and a project the caller may not read answers `404` with no figure. The project's settings page shows it
  and the admin area's project list has it as a column.
- **Moves.** The counters belong to the repository by ID, so a repository that moves to another project (R-REPO-060) takes its
  share along: the next read of the two projects shows the figure moved, with no recount.
- **Charged is reported.** Charging means this figure; no upload is refused for a project (Q-STOR-158). The system capacity
  limit and the repository quota of R-STOR-029 are the only limits.
- **The physical figure.** The admin dashboard shows what the object store keeps (`storage.bytes` of
  `GET /api/v1/system/overview`, [operations-and-observability.md](operations-and-observability.md) R-OPS-004), a figure of the
  installation. The projects' figures add up to more than it when content is shared (Q-STOR-154).

Acceptance criteria:

- A project with two repositories, each holding the same 10 MiB file, shows `logicalBytes` of 20 MiB while the physical size
  of the store grew by 10 MiB.
- The same file in a repository of another project is charged to that project in full as well; deleting it there leaves the
  first project's figure unchanged.
- After a repository with 10 MiB moves from project `a` to project `b` and one fold interval passed, `a` shows 10 MiB less
  and `b` 10 MiB more.
- A member of a project without a system role reads the figure of its project and gets `404` for another.

## Object-store configuration

### R-STOR-030 · Object-store connection and bucket settings
Priority: **MVP** · Covers: STOR-006, STOR-020, STOR-029

**TLS (R-ARCH-035, INV-20).** An `https://` endpoint is the production form. The object store's CA trust is part of
the TLS material of R-ARCH-035: a connection's `ca_file` (supported since M0, per connection since several connections) replaces the
system roots for that connection's client, and the file is reloaded like the other bundles (Q-STOR-14). A proxy credential file is
still refused. An `http://` endpoint is a development exception behind the connection's `allow_insecure_endpoint: true`, and the development
and integration profiles use TLS, so no profile needs the exception. Where the endpoint rules below name plain HTTP as
a permitted form, this paragraph governs.

- Quader talks to the bucket of each of its connections (R-STOR-050; one to eight, each holding every blob that is not being
  drained, R-STOR-052) through the S3 API with Signature Version 4 (AWS SDK for Go v2). This replaces "one bucket" (superseded on
  2026-11-22 by the owner's decisions on several connections). Any store that implements the operations below and meets R-STOR-035 is
  supported (AWS S3, MinIO, Ceph RGW and similar); the keys of this requirement are keys of each connection's element.
- Operations used: `PutObject`, `GetObject` (with `Range`), `HeadObject`, `DeleteObject`,
  `DeleteObjects` (optional), `ListObjectsV2`, `CreateMultipartUpload`, `UploadPart`,
  `UploadPartCopy`, `CompleteMultipartUpload`, `AbortMultipartUpload`, `ListMultipartUploads`,
  and presigning for the links of R-STOR-014 and their check (R-STOR-074). Quader never creates or configures the bucket.
- Configuration is read at startup; a change needs a rolling restart (Helm, INV-13). All
  replicas MUST use the same connections; a connection's bucket and prefix are fixed once it holds a copy, and moving content to
  another bucket or prefix is a new connection that the reconciliation fills (R-STOR-051). During a rolling update replicas with
  the old and the new list run side by side, which the copies and states of R-STOR-051 and R-STOR-052 tolerate.
- Validation at startup: bucket required; prefix without leading or trailing `/` and without
  `//`, at most 256 characters of `[A-Za-z0-9!_.*'()/-]`; `endpoint`, when set, an absolute
  `https://` URL (`http://` only with `allow_insecure_endpoint: true`). An endpoint without a scheme
  is a host, with an optional port, and means `https://` (Q-STOR-15): `s3.eu01.example.cloud` is read
  as `https://s3.eu01.example.cloud`, as a bucket provisioner writes it. The effective configuration
  shows the URL with the scheme added, and its source is `derived`. A scheme-less host never becomes `http://`, not even with
  `allow_insecure_endpoint`; plaintext is always written out.
- S3-compatible stores: `force_path_style: true` for stores without virtual-hosted addressing;
  explicit `region` (any value the store accepts); `checksums.request_calculation: when_required`
  and `checksums.response_validation: when_required` for stores that reject the SDK's default
  payload checksums (SDK options `RequestChecksumCalculation` and `ResponseChecksumValidation`,
  [aws package](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/aws)); `ca_file` for private CAs.
- Every key can be overridden by environment variables per INV-15: the list of connections is one key,
  `QUADER_STORAGE_CONNECTIONS`, whose value is the list as JSON; the shared block has a variable per key, for example
  `QUADER_STORAGE_OBJECT_STORE_HTTP_DIAL_TIMEOUT`.

Configuration: the keys of a connection and of `storage.object_store` are listed in R-STOR-050 (the checksum modes are the
element's `request_checksum_calculation` and `response_checksum_validation`; `startup_check` is `storage.object_store.startup_check`).

Acceptance criteria:

- The CI matrix runs the storage integration suite against SeaweedFS on every change and against
  Silo, the maintained fork of the MinIO server (the image pinned as `SILO_IMAGE`, on every change, Q-HA-282) and, once an AWS account exists, AWS S3, with the documented settings
  for each ([ha-and-helm.md](ha-and-helm.md) R-HA-051, Q-HA-3). Ceph RGW and other stores are
  qualified by the conformance check of R-STOR-035, not by a CI job.
- A prefix of `/` or `a//b` stops startup with a configuration error naming the key.

### R-STOR-031 · Object-store credentials
Priority: **MVP** · Covers: STOR-021

| `credentials.source` | Behavior |
|---|---|
| `default` (default, proposed) | The AWS SDK default chain: environment variables, shared config files, web identity token (used by [IAM roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)), container credentials (used by [EKS Pod Identity](https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html)), instance metadata. No secret in Quader's configuration. |
| `webIdentity` | Exchange the projected service-account token in `web_identity_token_file` for temporary credentials of `role_arn` at `sts_endpoint` (AssumeRoleWithWebIdentity). For S3-compatible stores with an STS that trusts the Kubernetes service-account issuer, and for AWS outside EKS. |
| `static` | Access key ID and secret read from files (`access_key_id_file`, `secret_access_key_file`, optional `session_token_file`), mounted from a Kubernetes Secret. Files are re-read on change so rotated keys apply without restart. Inline values are rejected (INV-15). |

- An optional `role_arn` with `default` or `static` assumes that role on top of the base
  credentials.
- Temporary credentials are refreshed by the SDK before expiry; a refresh failure counts as an
  object-store failure (R-STOR-033).
- Minimum permissions on the bucket and prefix: `s3:GetObject`, `s3:PutObject`,
  `s3:DeleteObject`, `s3:AbortMultipartUpload`, `s3:ListMultipartUploadParts` on
  `arn:aws:s3:::<bucket>/<prefix>/*`; `s3:ListBucket` (which may be conditioned on `s3:prefix`
  = `<prefix>/*`) and `s3:ListBucketMultipartUploads` on the bucket. No bucket-level write, no
  `CreateBucket`, no ACL permissions. With SSE-KMS add `kms:GenerateDataKey` and `kms:Decrypt` on the key
  (R-STOR-032).

- With several connections each has its own credential (R-STOR-050), and the permissions above are granted per connection on its
  bucket and prefix. A provider whose credentials reach every bucket of an account or project unless a bucket policy narrows them
  (the owner's provider documents it so, a research of 2026-11-22) makes one connection's credential a credential of the others'
  buckets, and of a backup target in the same project
  ([H-1253](../security/object-store-connections.md#h-1253--a-connections-credential-reaches-more-than-its-own-bucket)).

Configuration (keys of a connection's element, R-STOR-050):

```yaml
storage:
  connections:
    - credentials_source: default          # default (proposed): default | webIdentity | static
      role_arn: ""                         # example: arn:aws:iam::123456789012:role/quader
      web_identity_token_file: ""          # example: /var/run/secrets/tokens/s3
      sts_endpoint: ""                     # example: https://minio.example.com
      access_key_id_file: ""               # example: /var/run/secrets/quader/s3-default/access-key-id
      secret_access_key_file: ""           # example: /var/run/secrets/quader/s3-default/secret-access-key
      session_token_file: ""
```

Acceptance criteria:

- On EKS with a service account annotated for IRSA and `source: default`, the startup check
  passes with no S3 secret in the cluster.
- With `static`, replacing the Secret's files makes subsequent S3 requests use the new key without
  a pod restart.
- A policy limited to the permissions above passes the conformance check (R-STOR-035).

### R-STOR-032 · Server-side encryption
Priority: **P1** · Covers: STOR-022

- `sse.mode`: `none` (rely on the bucket's default encryption), `s3` (SSE-S3, AES-256) or `kms`
  (SSE-KMS with `kms_key_id`, and S3 Bucket Keys when `bucket_key` is true)
  ([SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html)).
  The mode is sent on every `PutObject` and `CreateMultipartUpload`.
- With `kms`, the credentials need `kms:GenerateDataKey` (writes, multipart) and `kms:Decrypt`
  (reads, multipart completion, presigned downloads).
- SSE with customer-provided keys is not supported: it would make Quader hold the data key and break
  presigned redirects. Client-side encryption is
  [R-STOR-048](#r-stor-048--client-side-encryption-of-stored-content) (specified, not built), per connection and
  in a format of Quader's own (R-STOR-075, R-STOR-076); this mode stays possible beneath it.
- Changing the mode affects new objects only; existing objects keep their encryption.

Configuration (keys of a connection's element, R-STOR-050; each connection has its own mode):

```yaml
storage:
  connections:
    - sse_mode: none            # default (proposed): none | s3 | kms
      sse_kms_key_id: ""        # example: arn:aws:kms:eu-central-1:123456789012:key/0f1e…
      sse_bucket_key: true      # default (proposed)
```

Acceptance criteria:

- With `mode: kms`, `aws s3api head-object` of a new blob shows `ServerSideEncryption: aws:kms`
  and the configured key; downloads and multipart uploads work with the minimal KMS permissions.

### R-STOR-033 · Connection pool, timeouts, retries and circuit breaker
Priority: **MVP** · Covers: STOR-024, STOR-017

- The S3 client of each connection uses one HTTP connection pool per replica with `storage.object_store.http.max_connections`, and
  explicit dial, TLS-handshake, response-header and idle timeouts. Body transfers have no fixed
  total timeout; they fail when no byte moves for `storage.object_store.http.idle_transfer_timeout`.
- Over HTTP/2, the store may send at most `storage.object_store.http.receive_window` bytes of one object
  ahead of what Quader has read; the transport's own default, 4 MiB, would let every slow download
  hold that much in memory ([architecture.md](architecture.md) Q-ARCH-241). Over HTTP/1.1, which
  AWS S3 speaks, the kernel's socket buffer holds what is in flight and the key has no effect.
- Retries: the SDK's standard retryer with `retry.max_attempts` and exponential backoff capped at
  `retry.max_backoff`, for throttling, `5xx` and network errors on idempotent operations (all
  operations Quader uses are idempotent on content-addressed keys). Streaming `GetObject` bodies
  are not retried after the first byte reached the client.
- The pool and `storage.object_store.multipart.max_concurrent_parts` protect one replica. The store sees
  their sum over all replicas, which grows with the replica count; sizing the store for it is
  [ha-and-helm.md](ha-and-helm.md) R-HA-050's.
- Circuit breaker per connection and replica (R-STOR-060): after `circuit_breaker.failure_threshold` consecutive failures
  (network errors, timeouts, `5xx`, credential errors; not `404` or other `4xx`) the circuit opens
  for `circuit_breaker.open_duration`; blob operations then fail fast (R-STOR-036); one probe
  request closes it again. A credential error is a failure to obtain a credential, or an answer that names the credential as
  the cause: `InvalidAccessKeyId`, `SignatureDoesNotMatch`, `ExpiredToken`, `InvalidToken`, `AuthorizationHeaderMalformed`.
  A plain `AccessDenied`, a missing bucket and the other `4xx` answers are answers about the request and do not count
  ([ha-and-helm.md](ha-and-helm.md) Q-HA-254). When the circuit opens on such an answer, the replica logs one `WARN` line with
  `connection` and `credential_source` (the connection's `credentials_source`) and the store's code, never a key or a secret (R-HA-007).
- Metrics: `quader_storage_s3_requests_total{connection,operation,outcome}`,
  `quader_storage_s3_request_duration_seconds{connection,operation}`, `quader_storage_s3_bytes_total{connection,direction}`,
  `quader_storage_s3_circuit_open{connection}` (R-STOR-060).

Configuration (shared by every connection, R-STOR-050):

```yaml
storage:
  object_store:
    http:
      max_connections: 256         # default (proposed)
      dial_timeout: 5s             # default (proposed)
      tls_handshake_timeout: 5s    # default (proposed)
      response_header_timeout: 30s # default (proposed)
      idle_conn_timeout: 90s       # default (proposed)
      idle_transfer_timeout: 60s   # default (proposed)
      receive_window: 256KiB       # default; 16KiB to 1GiB, HTTP/2 only
    retry:
      max_attempts: 5              # default (proposed)
      max_backoff: 20s             # default (proposed)
    circuit_breaker:
      failure_threshold: 5         # default (proposed)
      open_duration: 10s           # default (proposed)
```

Acceptance criteria:

- Injecting two `503 SlowDown` answers through a fault proxy before a `PutObject` succeeds makes
  the upload succeed; six consecutive failures make the next download answer `503` within
  milliseconds and `quader_storage_s3_circuit_open` report 1.

### R-STOR-034 · Outbound proxy for object-store traffic
Priority: **P1** · Covers: STOR-025

- `storage.object_store.http.proxy_url` sets an HTTP(S) proxy for the S3 and STS traffic of every connection only; credentials, when
  needed, are read from `storage.object_store.http.proxy_credentials_file` (`user:password`), never inline. `storage.object_store.http.no_proxy` is a
  comma-separated list of host names, domain suffixes (`.example.com`) and CIDR ranges.
- When `proxy_url` is empty, the standard `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` environment
  variables apply.
- This proxy is independent of the outbound proxies of remote repositories
  ([repositories-and-proxy.md](repositories-and-proxy.md)).

Configuration:

```yaml
storage:
  object_store:
    http:
      proxy_url: ""                  # default (proposed): empty = environment
      proxy_credentials_file: ""     # example: /var/run/secrets/quader-proxy/credentials
      no_proxy: ""                   # example: .svc.cluster.local,10.0.0.0/8
```

Acceptance criteria:

- With `proxy_url` pointing at a test proxy, all S3 requests appear in the proxy log and none
  bypass it; hosts in `no_proxy` are reached directly.

### R-STOR-035 · Object-store consistency assumptions and conformance check
Priority: **MVP** · Covers: STOR-029, STOR-006

The correctness of R-STOR-005, R-STOR-024, R-STOR-026, R-STOR-056 and R-STOR-057 rests on these assumptions about the
object store. They hold **per connection** (R-STOR-050): each connection is checked on its own, and nothing assumes that two
connections agree in time or order. Operators MUST use stores that satisfy them.

| ID | Assumption |
|---|---|
| A1 | Read-after-write consistency for new objects: a read started after a successful write of a key that did not exist observes it. Required. AWS documents strong consistency for S3 also for overwrites, deletes and listings ([consistency model](https://docs.aws.amazon.com/AmazonS3/latest/userguide/Welcome.html#ConsistencyModel)). Overwrites and deletes may be **eventually** consistent, as the owner's provider and the software behind it document (a research of 2026-11-22): a read shortly after an overwrite or a delete may still see the earlier object. Quader tolerates it: the same key always holds the same content, a stale `404` after a re-upload marks a copy `missing` that the recheck and the reconciliation restore (R-STOR-055, R-STOR-056), and a repaired copy read stale is marked again and repaired again until the store agrees ([Q-STOR-249](#q-stor-249--a1-is-required-for-new-objects-overwrites-and-deletes-may-be-eventual-and-no-guarantee-rests-on-a-conditional-write)). |
| A2 | Single-key atomicity: a read returns the old or the new object, never a partial one; a multipart object becomes visible only on completion. |
| A3 | A request whose response has been received is not executed again later, and of two completed writes or deletes of one key the one that completed later wins ("latest wins"). `delete_settle` (R-STOR-024) absorbs in-flight retries up to its length. A store that acknowledges a delete and removes the object's replicas afterwards (the software behind the owner's provider queues a delete it cannot finish within 30 s, same research) meets A3 only if that removal cannot reach a later write of the key; not verified ([H-1255](../security/object-store-connections.md#h-1255--a-store-that-applies-a-delete-after-acknowledging-it-is-not-proven-harmless-to-a-later-write)). |
| A4 | Each connection is one bucket in one consistency domain, written by one installation (the marker of R-STOR-051). Copies between connections are Quader's own reconciliation (R-STOR-056). A store's replication that lets another site or bucket write or delete keys under a connection's prefix is not supported, also not between two connections of one installation; one-way replication from a connection's bucket to a passive disaster-recovery bucket that is not a connection is ([ha-and-helm.md](ha-and-helm.md) R-HA-026). This replaces "one bucket in one consistency domain" (superseded on 2026-11-22). |
| A5 | Object `LastModified` comes from the store's clock, which differs from replica clocks by at most a few minutes; `storage.orphans.min_age` and `commit_deadline` keep a margin of hours. |
| A6 | Concurrent writes to one key carry the same content (content addressing), so last-writer-wins is harmless: on a plain connection identical bytes; on an encrypted connection each a whole encryption of that content with its own data key and its record in the same request, so whichever completes later is the object and reads back (A2 keeps record and bytes one object; R-STOR-088). On several connections each key lies on one connection, so writes to different connections never meet. |
| A7 | No guarantee rests on a conditional write (`If-None-Match`, `If-Match` on `PutObject`, `CopyObject` or `CompleteMultipartUpload`). Stores that refuse them, or accept and ignore them (the software behind the owner's provider documents both, depending on its configuration, same research), are supported. |

- AWS S3 satisfies A1–A2 by documentation. MinIO and Ceph RGW (within one zone) are expected to;
  the conformance check is the operator's evidence. Nothing was run against the owner's provider; its documentation states A1 for new
  objects and eventual consistency for overwrites and deletes, and is silent on conditional writes, checksum headers and a copy with new
  metadata (a research of 2026-11-22). What a connection supports is therefore established by the start check below, not assumed.
- **Startup check** (`storage.object_store.startup_check: true`), on each connection: write, read back, compare and delete
  `<prefix>/_probe/<replica-id>`, then list `<prefix>/sha256/` with one key, then the part-identity check (a part number sent twice must
  not assemble the other upload's bytes, R-STOR-011), then the marker (R-STOR-051), then the **capabilities**
  ([Q-STOR-248](#q-stor-248--the-start-check-establishes-what-a-connection-supports-and-refuses-or-degrades-by-a-stated-rule)):

  | Capability | How it is established | When it fails |
  |---|---|---|
  | The configured payload checksum and checksum modes | The probe's `PutObject` and parts carry them | A refusal (`400 InvalidArgument`, `InvalidRequest`, `501 NotImplemented`) is a configuration error naming the connection's `payload_checksum` or `request_checksum_calculation` and the value to try (`md5`, `none`, `when_required`) |
  | The payload checksum is enforced | One `PutObject` with a wrong checksum below `_probe/` | Accepted: `capabilities.payloadChecksumEnforced: false`; the connection's writes are `present` and not verified (R-STOR-052), so the scrub reads them first |
  | `DeleteObjects` | One request over two probe keys, when `batch_delete: true` | A configuration error naming `batch_delete` |
  | Metadata survives a write; a copy replaces it | A `PutObject` with one metadata key, `HeadObject`, a `CopyObject` onto itself with `REPLACE`, `GetObject`: the copy replaces the metadata only if the object kept its bytes (a gateway release is known to have lost the data of such a copy) | Recorded as `capabilities.metadata` and `capabilities.copyReplacesMetadata`; a plain connection needs neither; an encrypted connection without `metadata` is a configuration error, and without `copyReplacesMetadata` its sessions' finalize streams (R-STOR-075, R-STOR-081) |
  | A server-side copy from every other connection on the same endpoint | One `CopyObject` of a probe object across the two buckets, with this connection's credential | Recorded per pair; the reconciliation streams between them (R-STOR-056) |
  | A signed link with its overrides (stage 3's link check, R-STOR-074) | As R-STOR-074 says, on every replica that serves downloads, whether or not the rest of the check runs | Optional: the connection serves no link on that replica; nothing else stops |

  The results are written to `storage_connections.capabilities` and shown by the API (R-STOR-061). A replica does not
  become ready before the check has passed on the connections R-STOR-060 names (R-STOR-036, Q-STOR-1). Configuration and permission
  errors (`AccessDenied`, `NoSuchBucket`, invalid region) stop the process with a clear message.
  Transient errors (timeouts, connection errors, `5xx`, throttling) are retried with exponential
  backoff capped at 30 s for as long as they last; the replica stays not ready, MUST NOT exit, and
  logs the failing step once per minute. Exiting would turn a store outage into a crash loop whose
  restart back-off (up to five minutes in Kubernetes) delays readiness after the store returns;
  [ha-and-helm.md](ha-and-helm.md) R-HA-008 and R-HA-034 treat a wait for the schema the same way.
- **Conformance check**: `quader admin storage check [--connection <id>]` runs, on each connection or the one named, the startup check
  with its capabilities plus multipart
  upload and abort, a completion that names a part (a part number sent twice must not end up in an
  object that names the other upload's ETag, R-STOR-011), ranged read, server-side copy (the object write of an upload session's
  finalize, R-STOR-011), `DeleteObjects`, presigning, read-after-delete and
  list-after-write, and reports each result; overwrite and delete visibility are reported as "strong" or "eventual (tolerated, A1)",
  never as a failure. The startup check runs the part-identity check as well, so a replica does not serve on a store that fails it. It
  changes nothing outside `_probe/` and the marker.

Configuration:

```yaml
storage:
  object_store:
    startup_check: true      # default (proposed)
```

Acceptance criteria:

- `quader admin storage check` passes against SeaweedFS (local profile, every change) and against
  MinIO (every change) and AWS S3 (once an AWS account exists); against a bucket without `s3:DeleteObject` it reports exactly that
  failure.
- A replica started with a wrong bucket name exits with a non-zero status and a log line naming
  the bucket.
- A replica started while the local profile's S3 store is stopped stays not ready for ten minutes
  with an unchanged container restart count, and becomes ready within 60 s after the store is
  started again.

### R-STOR-036 · Behavior during an object-store outage
Priority: **MVP** · Covers: quader-only

With several connections the table holds when **every** connection is unreachable; the next table holds when some are
(R-STOR-053 to R-STOR-060).

| Operation | While S3 is unreachable or the circuit is open |
|---|---|
| Upload of new content (any protocol) | `503` with `Retry-After`; nothing committed; spool file removed. No local queue: a `201` always means durable in S3. |
| Upload of content already stored (deduplication hit), deploy by checksum | Succeeds (PostgreSQL only). |
| Copy, move, delete, properties, trash operations | Succeed (PostgreSQL only). |
| `HEAD`, `304` revalidation, item information, listings, UI, API | Succeed. |
| Download of stored content | `503` with `Retry-After`; no link is issued (R-STOR-014). |
| Remote-repository miss | Behavior defined in [repositories-and-proxy.md](repositories-and-proxy.md) (it MAY pass the upstream body through without caching). |
| Garbage collection, orphan sweep, scrub | Back off; claimed `deleting` rows are retried later; no status becomes `missing` because of errors. |
| Retention runs | Continue; their deletions touch only PostgreSQL. |
| Readiness | Object storage gates only a replica's first readiness (Q-STOR-1, INV-12 as revised): a replica becomes ready only after the startup check of R-STOR-035 passed, so a rollout with a broken S3 configuration stalls. Afterwards an outage leaves running replicas ready ([operations-and-observability.md](operations-and-observability.md) R-OPS-001, `health.require_object_store: false`): `/readyz` answers `200` with `"degraded": ["objectStore"]`, the circuit breaker fails blob operations per request (R-STOR-033), and `quader_storage_s3_circuit_open` feeds the shared-outage and single-replica alerts ([ha-and-helm.md](ha-and-helm.md) R-HA-045). With the opt-in `health.require_object_store: true`, R-OPS-001's `objectStore` check gates readiness continuously with that page's failure and success thresholds; this is meant for installations behind a load balancer that can fail over to another installation. |

| Operation | While one or more connections, but not all, are unreachable or their circuits open |
|---|---|
| Upload of new content | Written to the next writable connection in the same request; `201` once `min_copies` connections hold it, otherwise `503` (R-STOR-053); queue rows for the connections that missed it |
| An upload session on an unreachable connection | Its requests answer `503` with `Retry-After` until the connection answers; a new upload goes to a writable connection (R-STOR-054) |
| Download of stored content | Served from another present copy (R-STOR-055); `503` with `Retry-After` for a blob whose only copies lie on unreachable connections; no copy and no blob is marked because of the outage |
| Garbage collection | Deletes on the reachable connections, releases the copies whose delete it cannot send to the orphan sweep of their connection, and goes on (R-STOR-057) |
| Orphan sweep, scrub | Skip the unreachable connection and go on with the others |
| Reconciliation | The queue rows of the unreachable connection wait without spending attempts; copied oldest first once it answers (R-STOR-056) |
| Revocation journal | Shipped to the reachable connections; the outbox keeps the entry for the unreachable one (R-STOR-059) |
| Readiness | Running replicas stay ready with `objectStore` degraded; a replica that starts becomes ready when one connection that is not draining passed its start check (R-STOR-060) |

There is no storage-specific readiness configuration: the opt-in uses the thresholds of R-OPS-001,
so one hysteresis mechanism exists. The longest outage of an object store known to this specification is about 17.5 hours, of the
owner's provider in July 2026 (a research of 2026-11-22 from its status page; earlier ones there lasted up to about 10 hours): with one
connection that is 17.5 hours of `503` for uploads and downloads, and of replicas that start and stay not ready (Q-STOR-1); with two, it
is a backlog the reconciliation catches up (R-STOR-056).

Acceptance criteria:

- Stopping the local profile's S3 store: `npm publish` of a new version fails with a `503`;
  `npm view` and `curl -I` of existing tarballs succeed; deleting an item succeeds; restarting the
  store lets the publish succeed without manual steps; no blob is marked `missing`.
- With defaults, the local profile's S3 store stopped for five minutes: `/readyz` keeps answering
  `200` with `"degraded": ["objectStore"]` on every running replica, the UI and
  `GET /api/v1/repositories` keep working, and `quader_storage_s3_circuit_open` reports 1 on every
  replica. A replica started during the outage stays not ready. With
  `health.require_object_store: true`, every replica's `/readyz` answers `503` after R-OPS-001's
  failure threshold.

## Several object-store connections

The owner's decisions of 2026-11-22 on the encryption and integrity of the stored content and several object stores: Quader keeps
its blobs on several object-store connections at once, so that the outage of one provider does not stop it, and finds a damaged copy
that nobody downloads. The work comes in three stages (his decision 14): **stage 1**, this section, several connections, the
reconciliation between them and the scrub; **stage 2**, the encryption of stored content per connection (his decisions 1 to 6, 13
and 15, specified in the section [Encryption of stored content](#encryption-of-stored-content) on top of the seams of R-STOR-062,
summarized by [R-STOR-048](#r-stor-048--client-side-encryption-of-stored-content)); **stage 3**, the delivery by signed links
(his decision 11, specified beside this section; R-STOR-014). His answers that this section carries out:

| Decision | His answer | Where |
|---|---|---|
| 7 Changing a connection | A connection's properties are fixed once it holds data; changing them means a new connection (also the same bucket under another prefix), which the reconciliation fills, and the old one goes | R-STOR-051, R-STOR-056 |
| 8 When an upload is done | When one connection holds it; the reconciliation brings it to the others and catches up after an outage; a minimum number of copies before the answer is a detail setting | R-STOR-053, R-STOR-056 |
| 9 Where connections are kept | In the chart as a list, credentials from Secrets as today; the admin area shows each with state, fill and backlog and does not change them | R-STOR-050, [ha-and-helm.md](ha-and-helm.md) R-HA-072, [api-and-ui.md](api-and-ui.md) R-UI-196 |
| 10 The scrub | A worker job reads every copy once within a period (proposed: 30 days), throttled, checks it whole, marks damage and replaces a damaged copy from another connection | R-STOR-027 |
| 14 Stages | Several connections, reconciliation and scrub first; encryption second; signed links beside stage 1 | this section, R-STOR-062 |

**Status.** Partly built, in the packages of [docs/developer/blob-store.md](../developer/blob-store.md#several-connections-the-build-plan).
Built (package A): the configuration and its validation (R-STOR-050); the records, the fixed properties, the marker and `forget-connection`
(R-STOR-051); the copies and the derived content state (R-STOR-052); the commit over the connections with its copy and queue rows
(R-STOR-053); the reads over the copies (R-STOR-055); the start check and the conformance check per connection with the capabilities
(R-STOR-035); the breaker, the metrics with `connection`, the readiness and the registry's object-store view (R-STOR-060); the seams of
R-STOR-062. Built (package B): sessions on their connection (R-STOR-054), collection per copy with the release (R-STOR-057) and the
orphan sweep per connection with the adoption (R-STOR-026), with the decisions Q-STOR-260 to Q-STOR-264. Built (package C): the
reconciliation with its queue, its pass, the server-side and the streamed copies, the drain, the retirement, the journal pass and its
metrics (R-STOR-056). Built (package D): the scrub with its repair, the gauge of the overdue copies, the audit event of a detection, the
list of damaged blobs and the recheck (R-STOR-027), the operator check and `restore-versions` per connection (R-STOR-058). Built (package
E): the chart's list ([ha-and-helm.md](ha-and-helm.md) R-HA-072), the backup and restore per connection
([backup-and-restore.md](backup-and-restore.md) R-BAK-030) and the journal per connection (R-STOR-059). Built (package F): the connections in
the API and the system information (R-STOR-061), the storage summary with its figures per connection, its task and its gauges (R-STOR-028),
the tab **Storage** of System status and the dashboard's lines ([api-and-ui.md](api-and-ui.md) R-UI-196, R-UI-197), with the decisions
Q-STOR-370 to Q-STOR-372. One connection behaves as before this section, except that the orphan sweep now walks its `sha256/` too.

**What "several connections" is not.** Every connection that is not draining holds every blob (a mirror kept by Quader), unlike the
sharded stores of the catalog that spread blobs over sub-stores for capacity (STOR-013, STOR-026, and the parked "sharding over several
buckets" of [FULL.md](../projektplan/FULL.md) block E), which stay deferred. A store's own replication between buckets
([ha-and-helm.md](ha-and-helm.md) R-HA-026) is not a connection either.

### R-STOR-050 · Connections in the configuration
Priority: **P1** · Covers: STOR-011, STOR-012

- `storage.connections` is a list of one to eight connections; the single block `storage.s3` is gone (no backward compatibility
  before the first stable release, Q-ARCH-340). Each element names one bucket and prefix on one S3-compatible endpoint and carries
  everything that may differ between providers; what is the same for every connection (the HTTP client, retries, the circuit
  breaker's thresholds, multipart sizing, the probe at start) is the block `storage.object_store`
  ([Q-STOR-220](#q-stor-220--the-connections-are-a-list-of-flat-elements-and-the-shared-client-settings-a-block-of-their-own),
  [Q-STOR-222](#q-stor-222--what-may-differ-between-providers-is-a-key-of-the-connection-the-rest-is-shared)).
- **The identifier** `id` (default `default`) names the connection for its whole life: in the rows of its copies (R-STOR-052), in metrics and logs as the
  label `connection` (R-STOR-060), in the API and the admin area (R-STOR-061). It matches `^[a-z][a-z0-9-]{0,30}[a-z0-9]$`, is unique in
  the list, never changes and is never given to another connection (R-STOR-051).
- **Order.** The list order is the write order and the read order: an upload goes to the first connection that can take it
  (R-STOR-053), a read tries the copies in the order of their connections (R-STOR-055). An operator puts the store nearest to the
  replicas first. Reordering the list is allowed at any rollout and moves no data
  ([Q-STOR-221](#q-stor-221--the-list-order-is-the-write-order-and-the-read-order)).
- **Credentials, CA and address per connection.** The credential keys and their semantics are R-STOR-031's, flattened into the element
  (`credentials_source`, `role_arn`, `web_identity_token_file`, `sts_endpoint`, `access_key_id_file`, `secret_access_key_file`,
  `session_token_file`); `ca_file` is the connection's trust anchor (R-STOR-030's TLS rule, reloaded the same way); the server-side
  encryption of R-STOR-032 is `sse_mode`, `sse_kms_key_id`, `sse_bucket_key`.
- **A provisioner's Secret.** A bucket provisioner writes the bucket, the region and the endpoint into a Secret. `bucket_file`,
  `region_file` and `endpoint_file` name a file whose trimmed content is the value; each excludes its plain key, is read once at start
  (as the plain keys are, R-STOR-030), and the chart sets it from the Secret's keys
  ([ha-and-helm.md](ha-and-helm.md) R-HA-072, [Q-STOR-223](#q-stor-223--a-provisioners-secret-reaches-a-connection-through-files-named-by-the-element)).
- **The address for links** is no key of the connection: stage 3 keeps a public address per connection, by its `id`, in the admin area
  with the links' switch (R-STOR-074,
  Q-STOR-281); the links' own keys
  (`storage.s3.presign.*`) are gone.
- **`encrypted`** is a property of every connection, `false` by default (the owner's decision 6); `true` makes the connection keep its
  blobs encrypted (R-STOR-075, with its detail `keyed_names`), accepted since stage 2's package 3 was built on 2026-11-22.
- **`drain: true`** takes a connection out (R-STOR-051): no upload goes to it, the reconciliation makes sure every blob it holds lies on
  the other connections, and then its copies are deleted.
- **One connection stays as simple as today.** The plain path is one element with an `id` and a bucket (and, with static keys, one
  Secret in the chart): `storage.connections: [{bucket: quader-blobs}]`, whose `id` is `default`. Every other key has its default in the field;
  the minimum of copies, the reconciliation and the scrub need no setting. An installation with one connection behaves as before this
  section, except that the scrub now runs (R-STOR-027) and its damage cannot be repaired from another connection.

Validation at start (a violation is a configuration error naming the key and the connection):

1. One to eight elements ([Q-STOR-247](#q-stor-247--at-most-eight-connections)); every `id` matching the pattern and unique; an element
   without `id` is `default`, and at most one element may leave it out (so the one connection of the plain path, and of stage 3's
   answers today, is `default`).
2. `bucket` or `bucket_file` (exactly one); `endpoint` and `endpoint_file`, `region` and `region_file` exclude each other; a file that
   cannot be read or is empty is an error naming the key.
3. `prefix` and `endpoint` by R-STOR-030's rules (a host without a scheme means `https://`).
4. No two connections at one location: two elements whose normalised endpoints are equal (an empty endpoint compares as the AWS
   endpoint of its region), whose buckets are equal, and whose prefixes are equal or one contains the other, are refused. The
   connection marker of R-STOR-051 catches the location that two different addresses reach.
5. The backup target lies at no connection's location (R-BAK-002, extended to every connection).
6. R-STOR-075's rules for `encrypted` and `keyed_names`.
7. At least one element without `drain: true`, and `storage.replication.min_copies` between 1 and the number of elements without it.
8. The credential rules of R-STOR-031 (`static` needs both key files) and of R-STOR-032 (`sse_mode: kms` needs `sse_kms_key_id`) per
   element; `scrub_period` at least `24h`, `scrub_bytes_per_second` at least `1MiB`.
9. The rules against the database (fixed properties, retired identifiers, the marker) are R-STOR-051's.

Configuration (the element with every key; the defaults are in the fields):

```yaml
storage:
  connections:                            # one to eight; list order = write order and read order
    - id: default                         # default: default, for at most one element; never changes, never reused
      endpoint: ""                        # default: empty = AWS endpoint resolution for region; a host without a scheme means https://
      endpoint_file: ""                   # default: empty; a file holding the endpoint (a provisioner's Secret); excludes endpoint
      region: ""                          # default; example: eu-central-1
      region_file: ""                     # default: empty
      bucket: ""                          # example: quader-blobs; required unless bucket_file
      bucket_file: ""                     # default: empty
      prefix: quader                      # default; fixed once the connection holds data (R-STOR-051)
      force_path_style: false             # default
      allow_insecure_endpoint: false      # default
      ca_file: ""                         # default: empty = the outbound trust store
      storage_class: ""                   # default: empty = the bucket's default
      payload_checksum: sha256            # default: sha256 | md5 | none
      request_checksum_calculation: when_supported   # default: when_supported | when_required
      response_checksum_validation: when_supported   # default: when_supported | when_required
      batch_delete: true                  # default; false for a store without DeleteObjects
      credentials_source: default         # default: default | webIdentity | static
      role_arn: ""                        # default: empty
      web_identity_token_file: ""         # default: empty
      sts_endpoint: ""                    # default: empty
      access_key_id_file: ""              # default: empty; set by the chart with static keys
      secret_access_key_file: ""          # default: empty
      session_token_file: ""              # default: empty
      sse_mode: none                      # default: none | s3 | kms
      sse_kms_key_id: ""                  # default: empty
      sse_bucket_key: true                # default
      scrub_period: 720h                  # default: every copy read whole once in 30 days (R-STOR-027)
      scrub_bytes_per_second: 50MiB       # default: per connection and run
      drain: false                        # default
      encrypted: false                    # default; true: Quader encrypts the connection's blobs (R-STOR-075)
      keyed_names: true                   # default with encrypted: true; refused with encrypted: false (R-STOR-086)
  object_store:                           # the client of every connection (R-STOR-033 to R-STOR-035)
    http:
      max_connections: 256                # default; per connection and replica
      dial_timeout: 5s                    # default
      tls_handshake_timeout: 5s           # default
      response_header_timeout: 30s        # default
      idle_conn_timeout: 90s              # default
      idle_transfer_timeout: 60s          # default
      receive_window: 256KiB              # default; HTTP/2 only
      proxy_url: ""                       # default: empty = environment
      proxy_credentials_file: ""          # default: empty
      no_proxy: ""                        # default: empty
    retry:
      max_attempts: 5                     # default
      max_backoff: 20s                    # default
    circuit_breaker:                      # one breaker per connection and replica
      failure_threshold: 5                # default
      open_duration: 10s                  # default
    multipart:
      threshold: 100MiB                   # default
      part_size: 16MiB                    # default
      concurrency: 4                      # default
      max_concurrent_parts: 64            # default; per replica, over all connections
    startup_check: true                   # default (R-STOR-035)
  replication:
    min_copies: 1                         # default; copies written before an upload is answered (R-STOR-053)
  reconcile:                              # R-STOR-056
    concurrency: 4                        # default; copies at a time in one run
    bytes_per_second: 100MiB              # default; streamed bytes per run, server-side copies not counted
    server_side: auto                     # default: auto | never
    queue_limit: 10000                    # default; gaps the pass queues per connection before it waits for the copier
    retry_max_backoff: 1h                 # default
    run_duration: 10m                     # default; a run ends after this and the next scheduled run continues
  scrub:
    run_duration: 50m                     # default; a run of the hourly task ends after this (R-STOR-027)
maintenance:
  tasks:
    storage-reconcile: {enabled: true, schedule: "* * * * *"}   # default: every minute
```

Acceptance criteria:

- A table test of the configuration (unit, `internal/config`): one element with `id` and `bucket` loads with every default; a missing
  `id`, a duplicate `id`, `Primary` (pattern), nine elements, `bucket` with `bucket_file`, two elements on one endpoint and bucket with
  the prefixes `quader` and `quader/a`, `drain: true` on every element, and
  `min_copies: 3` with two elements are each refused with the key and the connection named.
- The environment variable `QUADER_STORAGE_CONNECTIONS` holds the whole list as JSON (INV-15; a list of elements is one key,
  [internal/config/schema.go](../../internal/config/schema.go) `isStructList`), and overrides the file's list.

### R-STOR-051 · A connection's life and its fixed properties
Priority: **P1** · Covers: STOR-012

- **The record.** The first replica that starts with a connection inserts its row in `storage_connections` (R-STOR-052) with the
  properties that decide where and how its data lies: `bucket`, `prefix`, `encrypted` and, on an encrypted connection, `keyed_names`
  (R-STOR-075). These are fixed once the connection holds a copy (the owner's decision 7). A replica whose element differs from the row in
  one of them while the connection holds a copy refuses to start: "storage.connections[0] (default): bucket, prefix, encrypted and
  keyed_names are fixed: the connection holds 412093 copies in quader-blobs/quader. To move the data, add a connection with another id and
  set drain: true on this one (storage.md R-STOR-051, R-STOR-075)". A row without a copy takes the new
  values. Endpoint, region, credentials, CA, checksum and tuning keys may change at any rollout
  ([Q-STOR-224](#q-stor-224--bucket-prefix-and-encrypted-are-fixed-endpoint-and-credentials-may-change-and-a-marker-names-the-location)).
- **The marker.** Each connection's location holds one object `<prefix>/_connection` with `{"installationId", "connectionId",
  "createdAt"}`. The start check (R-STOR-035) reads it: absent, it is written (and, when the connection holds copies, a warning says the
  marker was missing and the scrub will find what the location lacks); present with this installation and this id, the check passes;
  present with another installation or another id, the connection is refused as a configuration error naming both ("quader-blobs/quader
  is connection 'archive' of installation 0b7c3f5e-…; a location belongs to one connection of one installation. Remove the object
  quader/_connection only if that installation is gone."). Two connections, or two installations, can therefore not share a location,
  however their addresses are spelled.
- **States.** A connection's state is derived, never set by hand:

  | State | When | Uploads | Reads | Reconciliation |
  |---|---|---|---|---|
  | `filling` | In the configuration, `drain: false`, and no full pass of the reconciliation has yet ended with every live blob present on it | yes | its present copies | copies every blob to it |
  | `active` | As `filling`, after the first full pass without a gap (`filled_at`) | yes | its present copies | copies new blobs and repairs |
  | `draining` | In the configuration with `drain: true` and still holding a copy | no | its present copies, last | moves what only it holds, then deletes its copies |
  | `drained` | `drain: true` and no copy left (`drained_at`) | no | — | — |
  | `detached` | Holds copies, is not `retired`, and no live replica has it in its configuration | — | — | its copies count for no blob's redundancy; the blobs it alone holds answer `503` |
  | `retired` | Was `drained` and left every live replica's configuration, or was forgotten (below) | — | — | — |

  Which replicas have a connection in their configuration comes from the object-store view of the replica registry
  ([operations-and-observability.md](operations-and-observability.md) R-OPS-003), so a rolling update, during which old and new
  configurations run side by side, flips no stored state; `filled_at`, `drained_at` and `retired_at` are written by the reconciliation
  run (R-STOR-056).
- **Taking a connection in** is adding an element: it starts `filling`, the reconciliation copies every live blob to it, and it turns
  `active` (the owner's decision 7). **Taking one out** is `drain: true`, then removing the element once the admin area shows `drained`.
  **Changing a fixed property** (another bucket, another prefix, encryption on in stage 2) is both: a new element with a new id, the old
  one drained.
- **Removed while it still holds copies.** An element removed without draining leaves the connection `detached`: replicas start, no
  copy is read from it, the blobs whose only copies lie there answer `503 content_unavailable` with `Retry-After`, the alert
  `QuaderStorageConnectionDetached` fires, and putting the element back restores everything
  ([Q-STOR-225](#q-stor-225--a-connection-removed-while-it-holds-copies-is-detached-not-refused)). A connection that is gone for good is
  forgotten with `quader admin storage forget-connection <id>`: refused while a live replica has it in its configuration; it deletes the
  connection's copy and queue rows, recomputes the content state of every blob it held (one without a present copy elsewhere becomes
  `missing`, R-STOR-052), sets `retired_at`, writes the audit event `system.storage.connection_forgotten` with the counts, and never
  deletes an object.
- **Identifiers are not reused.** A configuration that names a `retired` id is refused at start ("storage.connections[1] (archive): id: the connection id archive was retired on
  2026-12-03; choose another id"), so a row of history, and stage 2's keys of that connection, never meet another bucket. An id is retired
  when its row has `retired_at` or when `storage_keys` holds a destroyed key of it (R-STOR-102), because a restore of a backup empties
  `storage_connections` and keeps `storage_keys`.
- Audit events (the registry of [operations-and-observability.md](operations-and-observability.md) R-OPS-014): `system.storage.connection_added`
  (first sight, actor `system:replica`), `system.storage.connection_drained` (actor `system:worker`, with the copies deleted) and
  `system.storage.connection_forgotten` (actor `system:admin`).

Acceptance criteria:

- With a connection that holds copies, a replica whose element names another prefix exits with the message above; with a connection that
  holds none, it starts and the row shows the new prefix (integration, PostgreSQL and two in-memory stores).
- A second installation (another `installationId`) configured on the first one's bucket and prefix is refused at start and names the
  object `quader/_connection`; the first installation is untouched.
- An element removed while its connection holds the only copy of a blob: replicas become ready, a download of that blob answers `503` with
  `Retry-After`, `GET /api/v1/system/storage-connections` lists the connection `detached`, and the element put back serves the blob again.
- `quader admin storage forget-connection archive` while a replica configures `archive` exits `2`; after the element is removed it exits
  `0`, the blob that only `archive` held is `missing`, and a configuration naming `archive` again is refused at start.

### R-STOR-052 · The copies of a blob
Priority: **P1** · Covers: quader-only

The blob row stays the authority on whether a blob exists (R-STOR-005); a row per copy says where its object lies and what is known
about it ([Q-STOR-226](#q-stor-226--a-row-per-copy-without-a-foreign-key-to-blobs-and-the-blobs-content-state-derived-from-its-copies)).

```sql
CREATE TABLE storage_connections (          -- one row per connection id ever seen (R-STOR-051); export class transient
    id                text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9-]{0,30}[a-z0-9]$'),
    bucket            text NOT NULL,         -- fixed once the connection holds a copy
    prefix            text NOT NULL,         -- fixed once the connection holds a copy
    encrypted         boolean NOT NULL DEFAULT false,   -- fixed; stage 1 writes only false
    capabilities      jsonb NOT NULL DEFAULT '{}',      -- what the start check found (R-STOR-035)
    first_seen_at     timestamptz NOT NULL DEFAULT now(),
    filled_at         timestamptz,           -- first full pass without a gap: filling -> active
    drained_at        timestamptz,           -- drain: true and no copy left
    retired_at        timestamptz,           -- drained and configured nowhere, or forgotten
    pass_started_at   timestamptz,           -- the reconciliation's current full pass
    last_full_pass_at timestamptz
);

CREATE TABLE blob_copies (                   -- no foreign key to blobs (reference rule 3 would count it as a reference)
    sha256           bytea NOT NULL CHECK (octet_length(sha256) = 32),
    connection_id    text  NOT NULL REFERENCES storage_connections (id),
    object_key       text  NOT NULL,         -- the object's name on that connection; stage 1: <prefix>/sha256/<h0h1>/<h2h3>/<hex>
    stored_size      bigint NOT NULL CHECK (stored_size >= 0),  -- bytes as stored; stage 1: blobs.size
    storage_key_id   text,                   -- NULL in stage 1; stage 2: the storage key that wrapped the object's data key
    state            text NOT NULL CHECK (state IN ('present', 'missing', 'corrupt', 'deleting')),
    created_at       timestamptz NOT NULL DEFAULT now(),   -- when the object was written
    verified_at      timestamptz,            -- last time the whole stored object was shown to be the blob (below)
    checked_at       timestamptz,            -- last time the store answered for it (a read, a check, the scrub)
    state_changed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sha256, connection_id)
);
CREATE UNIQUE INDEX blob_copies_key      ON blob_copies (connection_id, object_key);                  -- the orphan sweep's lookup by name
CREATE INDEX        blob_copies_scrub    ON blob_copies (connection_id, verified_at NULLS FIRST) WHERE state = 'present';  -- the scrub's order, the counts per connection
CREATE INDEX        blob_copies_damaged  ON blob_copies (connection_id, state) WHERE state IN ('missing', 'corrupt');
CREATE INDEX        blob_copies_deleting ON blob_copies (state_changed_at) WHERE state = 'deleting';

CREATE TABLE blob_copy_queue (               -- gaps the reconciliation fills (R-STOR-056); no foreign key to blobs
    connection_id   text  NOT NULL REFERENCES storage_connections (id),
    sha256          bytea NOT NULL CHECK (octet_length(sha256) = 32),
    enqueued_at     timestamptz NOT NULL DEFAULT now(),
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error      text,
    PRIMARY KEY (connection_id, sha256)
);
CREATE INDEX blob_copy_queue_due    ON blob_copy_queue (next_attempt_at);
CREATE INDEX blob_copy_queue_oldest ON blob_copy_queue (connection_id, enqueued_at);
```

- **No foreign key to `blobs`** in either table: R-STOR-024 reads every single-column foreign key to `blobs` as a reference
  ([queries/blob.sql](../../internal/meta/postgres/queries/blob.sql) `BlobReferencingColumns`), so such a key would keep every blob
  forever. The rows of a blob are deleted in the transaction of collection step (6) that drops the blob row (R-STOR-057). A migration
  test fails on a foreign key from either table to `blobs`.
- **Copy states** ([Q-STOR-227](#q-stor-227--four-copy-states-and-verified-means-the-whole-stored-object-was-shown-to-be-the-blob)):
  `present`, the object was written and confirmed (a size from `HeadObject` after a multipart or copy, or the store's answer to a single
  `PutObject`); `missing`, the store answered "no such object" for it; `corrupt`, its size differs from `stored_size` or a read to its end
  had another digest; `deleting`, collection or a drain claimed it for deletion. A connection without a row for a blob does not hold it
  (a gap, R-STOR-056). Only evidence about the object changes a copy's state, as R-STOR-013 says of the blob: a timeout, a `5xx` or an
  open circuit changes nothing.
- **Verified** (`verified_at`) means that the whole stored object was shown to be the blob: Quader hashed the bytes it wrote and the store
  enforced the payload checksum of every request that carried them (R-STOR-006; enforcement is a capability the start check establishes
  per connection, R-STOR-035), or a read to the end through Quader matched the digest (a download, the scrub, the source of a streamed
  copy). A server-side copy, a write without an enforced payload checksum and an adopted object (R-STOR-026) are `present` and not
  verified until the scrub reads them, which it does first.
- **The blob's content state follows from its copies.** `blobs.content_status` stays one column, so that every reader still refuses a
  damaged blob from one row (R-STOR-012) and the indexes and alerts that read it stay: `ok` while at least one copy on a connection that is
  not `retired` is `present`; otherwise `corrupt` if a copy is `corrupt`, else `missing`. The statement that changes a copy's state
  recomputes it in the same transaction; a change of the blob's state counts `quader_storage_blobs_damaged_total{status}` and writes the
  audit event of R-STOR-027, as today. **A "no such object" from one connection marks that copy, never the blob, while another copy is
  present** (R-STOR-055).
- **Columns that move.** `blobs.deep_checked_at` and the index `blobs_check` go (the scrub orders copies, not blobs);
  `blobs.checked_at` stays and keeps one meaning, the recheck claim of a blob without a present copy (Q-STOR-153, R-STOR-055).
- **Indexes the jobs need** (above): the reconciliation reads the queue by `next_attempt_at` and its age per connection by
  `(connection_id, enqueued_at)`, and walks live blobs by the primary key of `blobs` with one lookup of their copies per batch
  (`sha256 = ANY(…)` on the primary key of `blob_copies`); the scrub reads `blob_copies_scrub` per connection; the damaged list, the
  repair and the counts read `blob_copies_damaged`; collection reads a blob's copies by the primary key and finishes interrupted copy
  deletions by `blob_copies_deleting`; the orphan sweep finds a listed object by `(connection_id, object_key)`, which keeps working when
  stage 2 gives a connection names that are not the digest.
- `upload_sessions` gains `connection_id text NOT NULL REFERENCES storage_connections (id)` (R-STOR-054).

Acceptance criteria:

- A unit test over the derivation: copies `present` + `missing` give `ok`; `missing` + `corrupt` give `corrupt`; `missing` alone gives
  `missing`; a `present` copy on a `retired` connection counts for nothing; a `present` copy on a `detached` one counts.
- The migration test lists no foreign key from `blob_copies` or `blob_copy_queue` to `blobs`, and every foreign key to `blobs` still
  has its index and both triggers (R-STOR-003).

### R-STOR-053 · Where an upload is written and when it is done
Priority: **P1** · Covers: STOR-011

- **Writable connections** of a replica are those in its configuration with `drain: false` whose circuit is closed on that replica,
  in list order.
- **An upload is done when `storage.replication.min_copies` connections hold it** (default 1, the owner's decision 8). The commit
  protocol (R-STOR-005) writes the object from the spool to the first `min_copies` writable connections in parallel
  ([Q-STOR-229](#q-stor-229--an-upload-goes-to-the-first-writable-connection-and-falls-over-to-the-next-within-the-request),
  [Q-STOR-230](#q-stor-230--min_copies-is-a-file-setting-and-a-request-that-reaches-fewer-connections-is-refused)); a write that fails
  with a transient error or meets an open circuit is repeated on the next writable connection within the same request, and the spool
  still holds the bytes, so a provider's outage costs an upload no retry of its client. Fewer than `min_copies` successful writes answer
  `503` with `Retry-After` and commit nothing; the objects written are left to the orphan sweep of their connections (R-STOR-026).
- **The commit's guarantee per connection.** The invariant of R-STOR-005 becomes: *a committed reference always points at a blob row in
  state `live` that had at least `min_copies` copies in state `present` when the reference was committed, each written before the row or
  its copy row was*. The link round inserts a copy row for every object this commit wrote within the insert bound of R-STOR-005 (counted
  from the end of the earliest of its writes), also on a round that turns out a deduplication hit (a `missing` or `corrupt` row becomes `present`, a `deleting` one is left, [Q-STOR-253](#q-stor-253--a-commits-write-overwrites-a-missing-or-corrupt-copy-row)), and a queue
  row (R-STOR-052) for every other connection in this replica's configuration with `drain: false`. A deduplication hit writes nothing and
  needs no connection, as before (R-STOR-036): an existing blob with fewer present copies is the reconciliation's.
- **Repair.** The commit's repair case (`live` and `content_status <> 'ok'`, no present copy) writes the object to the first
  `min_copies` writable connections, sets their copy rows `present` and, by the derivation, the blob `ok`.
- **"A `201` always means durable"** (R-STOR-036) holds per copy: durable on `min_copies` connections. With the default, a new blob lies
  on one connection until the reconciliation has copied it (seconds to minutes normally; the length of an outage when the others are
  down); the gap is [H-1251](../security/object-store-connections.md#h-1251--a-new-blob-lies-on-one-connection-until-the-reconciliation-copies-it).
- **The assumptions per connection.** R-STOR-035's assumptions hold per connection and are checked per connection; no guarantee of the
  commit, the reconciliation or collection rests on a conditional write (`If-None-Match`, `If-Match`), which some stores refuse or accept
  and ignore (R-STOR-035 A7).
- **Transfer.** R-STOR-006 applies per connection with that connection's `payload_checksum`, checksum modes and storage class; the
  replica's `storage.object_store.multipart.max_concurrent_parts` is shared by all its connections.

Acceptance criteria (unit, `internal/blob` with two in-memory stores and the fake database):

- With both connections reachable and `min_copies: 1`, an upload writes one object on the first store, inserts one `present` copy row
  and one queue row for the second.
- With a fault that answers every `PutObject` of the first store with `503`, the upload answers `201`, the object lies on the second
  store, and its copy row and a queue row for the first are inserted.
- With `min_copies: 2` and the second store failing, the upload answers `503`, no blob row exists, and the object on the first store has
  no copy row.

### R-STOR-054 · An upload session stays on its connection
Priority: **P1** · Covers: quader-only

*Built* (package B): [internal/uploads](../../internal/uploads/) (`Service.writable`, `Service.connection`, `pin`), the session step in
[gc_uploads.go](../../internal/blob/gc_uploads.go).

- A multi-request upload session (R-STOR-011) is pinned to one connection, recorded in `upload_sessions.connection_id`: the first
  writable connection when the session starts, or, when none is writable then, the first without `drain: true`; the start asks no store
  ([Q-STOR-264](#q-stor-264--a-session-starts-on-the-first-writable-connection-without-asking-the-store-and-moves-at-its-first-write-while-it-has-written-nothing)).
  Its multipart upload `_uploads/<session-id>/data`, its tail segments and the server-side copy of its finalize live on that connection,
  because a multipart upload exists in one store only and a server-side copy stays inside one
  ([Q-STOR-231](#q-stor-231--a-session-stays-on-its-connection-and-answers-503-while-it-is-unreachable)).
- A session that has not yet written an object (`received = 0`) moves to the next writable connection when its connection is not
  writable at its first write (its circuit open on the replica, `drain: true`, or not configured there); the move is recorded under the
  writing request's claim.
- **When its connection fails.** A request of a session whose connection is unreachable, whose circuit is open or which this replica
  does not configure answers `503` with `Retry-After` (`storage.object_store.circuit_breaker.open_duration`), and the session stays as it
  is; a session that has written stays also on a connection that is given `drain: true` meanwhile; a client that keeps retrying
  continues once the connection is back, and one that gives up starts a new upload, which goes to a writable connection. The abandoned
  session expires by `storage.uploads.session_ttl` (R-STOR-011); its objects are removed on its connection when that connection is
  reachable again, or by the orphan sweep there (R-STOR-026).
- **Finalize.** The commit's object write is the server-side copy of R-STOR-011 on the session's connection, and its copy row records
  that connection (`present`, not verified: the scrub reads it first, which also covers
  [H-591](../security/upload-sessions.md#h-591--the-staging-object-is-copied-without-being-read-back) within one scrub run).
  With `min_copies` above 1 the finalize then streams the staging object through the replica to the next writable connections, hashing
  it (those copies are verified; a mismatch fails the finalize with `503` and leaves the session `assembled` for the client's retry); with the default it copies nothing else and queues the other connections.
- The session step of collection (R-STOR-025) removes a session's objects on the session's connection; a session on a connection that
  is unreachable keeps its row until the connection answers, unless the row is older than `storage.uploads.session_max_age`, when the row
  goes and the orphan sweep removes its objects.

Acceptance criteria (unit, `internal/uploads` with two in-memory stores; `TestR_STOR_054_AnUploadSessionStaysOnItsConnection`):

- A session started while both stores are reachable records the first connection; its parts and tails lie only there; finalize writes
  the content object there and queues the second.
- With the first store failing after the session's first part, the next `PATCH` answers `503` with `Retry-After`; after the fault is
  lifted the same session continues and finalizes with the right digest.
- A session whose first request meets the first store failing, before any object, lies on the second store.
- The session step removes a session's objects on its own connection; on a connection whose circuit is open a session keeps its row,
  unless it is older than `session_max_age` (integration, `TestR_STOR_054_TheSessionStepWorksOnEachSessionsConnection`).

### R-STOR-055 · Reads over the copies
Priority: **P1** · Covers: STOR-012

- **Order.** A read (a download, a range, an internal reader of [R-STOR-013](#r-stor-013--download-integrity-verification)) takes the
  copies of the blob in state `present` whose connection is in this replica's configuration, in list order, a `draining` connection's
  last, and leaves out a connection whose circuit is open on this replica. With no such copy it answers `503 content_unavailable` with
  `Retry-After` and marks nothing. A download takes the copies from its item lookup, which reads them with the item, so that it costs the
  one statement of [architecture.md](architecture.md) R-ARCH-027
  ([Q-STOR-275](#q-stor-275--a-download-takes-its-blobs-copies-from-its-item-lookup)); a reader without an item reads them itself.
- **Falling over.** A copy is left for the next one, before the first byte reached the client, when its connection answers "no such
  object", a transient error, an open circuit or a `Content-Length` other than `stored_size`. A failure after the first byte aborts the
  response as R-STOR-013 says, and the client's retry or resume (`Range` with `If-Range`, R-STOR-012) takes the next copy
  ([Q-STOR-232](#q-stor-232--a-read-falls-over-only-before-its-first-byte)).
- **What is recorded.** "No such object" marks that copy `missing`, then looks once more with `HeadObject` on the same connection, as
  Q-STOR-151 does for the blob today, so that a concurrent repair stays; a size other than `stored_size`, or a digest mismatch at the end of
  a verified read, marks it `corrupt`. Each mark counts `quader_storage_copies_damaged_total{connection,status}`, queues the copy for
  repair (R-STOR-056) and, by the derivation of R-STOR-052, changes the blob's state only when no other copy is present. A transient error
  marks nothing. Each fall-over counts `quader_storage_reads_fallen_over_total{connection,reason}`.
- **Verification as today.** A full read is verified with the hold-back of R-STOR-013 and compared with the copy's `stored_size`; a range
  is served as stored (H-11). A verified full read stamps the copy's `verified_at` and `checked_at` when `verified_at` is older than 24
  hours, so downloads of a hot blob serve as its scrub at one write per copy and day
  ([Q-STOR-233](#q-stor-233--a-verified-download-stamps-its-copy-at-most-once-a-day)).
- **The recheck of a missing blob** (Q-STOR-153) asks each `missing` copy on a reachable connection with one `HeadObject`; an object of
  `stored_size` sets that copy `present` and the blob `ok`.
- **A slow connection** that answers within its timeouts is not left: a read waits up to `response_header_timeout` on it before it falls
  over. A store that is slow but not failing (the owner's provider reported slow answers for about 17.5 hours on 2026-07-08/09, a
  research of 2026-11-22 from its status page, not verified here) costs reads time, not answers; an operator who sees it moves the
  connection down the list.

Acceptance criteria (unit, `internal/blob`, two in-memory stores, the fault hook `SetFault`):

- The object deleted from the first store: the download answers `200` with the right bytes from the second, the first copy is `missing`, a
  queue row for the first connection exists, and the blob is `ok`.
- The object on the first store replaced by other bytes of the same length: the download is aborted at its end (R-STOR-013), the copy is
  `corrupt`, and the next download serves the second copy whole.
- Both objects deleted: the download answers `503 content_unavailable`, the blob is `missing`, and one `HeadObject` per copy follows
  within the recheck interval, not one per read.
- Every `GetObject` of the first store answering `503`: downloads are served from the second, and no copy changes state.

### R-STOR-056 · The reconciliation
Priority: **P1** · Covers: STOR-015

The worker job that brings every blob onto every connection that is not draining, catches up after an outage, fills a new connection,
empties a draining one, and replaces damaged copies.

- **The job.** Task and kind `storage-reconcile` in the queue `maintenance`, `exclusive` under its name, scheduled every minute and
  signalled after a commit that queued a gap ([architecture.md](architecture.md) R-ARCH-016, R-ARCH-018). A run ends when the queue holds
  no due row and the pass is done or waiting, or after `storage.reconcile.run_duration`; the pass's and the drains' positions are a row of
  `system_state` written after every batch, so a drain of the worker moves the run to another without loss and the next scheduled run goes
  on where the last ended ([Q-STOR-235](#q-stor-235--the-reconciliation-is-one-exclusive-job-per-installation),
  [Q-STOR-257](#q-stor-257--the-pass-queues-a-gap-only-where-another-connection-holds-the-blob-starts-hourly-or-at-once-while-a-connection-fills-and-keeps-its-position-in-a-row-of-its-own)).
- **What it works from** ([Q-STOR-228](#q-stor-228--a-queue-for-what-happens-and-a-pass-for-what-was-missed)):
  1. the **queue** `blob_copy_queue`, filled by the commit (R-STOR-053), by a copy marked `missing` or `corrupt` (R-STOR-055,
     R-STOR-027) and by the pass; rows are taken by `next_attempt_at`, oldest first, for connections in the worker's configuration whose
     circuit is closed;
  2. the **pass**, a walk over the live blobs by digest in batches of 1,000 with one lookup of their copies per batch, which queues the
     gaps of every connection with `drain: false` in the worker's configuration (`ON CONFLICT DO NOTHING`): a live blob without a present
     copy on the connection while another connection holds a present copy. It waits at its position while a connection's queue holds
     `storage.reconcile.queue_limit` rows. The pass finds what no queue row stands for: a connection that is new, a blob committed by a
     replica whose configuration lacked the connection during a rollout, a gap left by a lost queue row. A full pass records
     `last_full_pass_at`; the first one that ends with no gap and an empty queue for a `filling` connection sets its `filled_at` (R-STOR-051).
     A new full pass starts when a connection with `drain: false` has never ended one, is `filling` with an empty queue, or the last ended
     an hour ago (Q-STOR-257).
- **One copy.** For a queue row (connection C, blob B): a row whose blob is not `live`, whose C is not a target any more (`drain: true`, not
  configured) or already holds a `present` copy is deleted, and so is a row whose blob has no `present` copy on any connection: the commit's
  repair or the recheck gives it one, and the next pass queues the gap again
  ([Q-STOR-258](#q-stor-258--five-reasons-of-a-failure-a-queue-row-without-a-source-anywhere-is-deleted-and-a-target-whose-circuit-opened-during-the-run-spends-no-attempt)).
  Otherwise the source is a `present` copy on another reachable connection, verified before not verified, then by list order:
  - **within one endpoint, a server-side copy** (`storage.reconcile.server_side: auto`, where C and the source share a normalised
    endpoint): `CopyObject` across buckets up to 5 GiB, `UploadPartCopy` above, signed with C's credential, which needs read access to the
    source (the start check establishes it per pair of connections, R-STOR-035); an `AccessDenied` or `NotImplemented` falls back to
    streaming for that pair for the rest of the run. The copy is confirmed by `HeadObject` (`stored_size`) and is `present`, not verified;
  - **across providers, a stream through the worker**: a `GetObject` of the source piped into `PutObject` up to
    `storage.object_store.multipart.part_size`, and above it ranged parts of the source into C's multipart upload, one part held in memory at
    a time ([Q-STOR-255](#q-stor-255--a-streamed-copy-holds-one-part-in-memory-one-request-up-to-a-part-ranged-reads-into-a-multipart-upload-above-it)),
    with C's payload checksums, hashed while it passes; a SHA-256 other than the digest aborts the upload before its completion, marks the
    source copy `corrupt` (a read to the end is evidence) and tries the next source, and a source the store reports missing is marked
    `missing`. The copy is `present` and verified (when C enforces payload checksums).
  - **Recording.** One transaction: the blob's lock as a mark takes it (`FOR NO KEY UPDATE`) must find `live`, then the copy row of C is set
    `present` (inserted, or a `missing` or `corrupt` row overwritten; a row claimed by a drain is left), the queue row deleted, the source
    of a streamed copy stamped verified, and the blob's content state follows
    ([Q-STOR-256](#q-stor-256--a-copy-is-recorded-under-the-lock-of-a-mark-the-source-of-a-streamed-copy-is-stamped-verified-and-the-blobs-content-state-follows)).
    A blob found `deleting` leaves the object to C's orphan sweep. The row is recorded within the insert bound of R-STOR-005 after the write
    ended, or the object is written again, so a collection delete that may still be in flight never meets a copy recorded after it
    (R-STOR-024 rule 3).
  - **A failure** counts `attempts`, sets `next_attempt_at` to `now() + min(2^attempts minutes, storage.reconcile.retry_max_backoff)` and
    `last_error`, and counts `quader_storage_reconcile_failures_total{connection,reason}`; the reasons are `no_source`, `unavailable`,
    `source_damaged`, `refused` and `claimed` (Q-STOR-258). A write that C's circuit breaker refused because the circuit opened during the
    run is no failure: its row waits like every row of a connection whose circuit is open.
- **Verification before a copy counts.** A copy counts for redundancy once `present`; it counts for a drain's deletion only when it is
  verified within its connection's `scrub_period` (below), and a server-side copy is the scrub's first work
  ([Q-STOR-234](#q-stor-234--a-server-side-copy-counts-as-present-and-is-the-scrubs-first-work-a-drain-needs-a-verified-one)).
- **Throttling.** At most `storage.reconcile.concurrency` copies at a time; the streamed bytes of a run at most
  `storage.reconcile.bytes_per_second`; server-side copies move no byte through the worker and are bounded by the concurrency alone.
- **Catching up after an outage.** While C's circuit is open on the worker, its queue rows wait without spending attempts; once it
  answers, they are copied oldest first. A recorded outage of the owner's provider lasted about 17.5 hours (2026-07-08/09, a research
  of 2026-11-22, not verified here): at 5 new blobs per second *(example)* that is about 315,000 queue rows, a few tens of megabytes of
  PostgreSQL, and at 10 GiB of new content per hour *(example)* 175 GiB, which `bytes_per_second: 100MiB` copies in about half an hour.
- **Draining** ([Q-STOR-236](#q-stor-236--a-drain-deletes-a-copy-once-every-other-connection-holds-the-blob-and-one-copy-is-verified)).
  For a connection D with `drain: true`, the run walks D's copies that are not claimed for deletion, by object name; a blob that lacks a
  present copy on a connection with `drain: false` is queued there; a copy on D whose blob has a `present` copy on every connection with
  `drain: false` in the worker's configuration, one of them verified within its `scrub_period`, is deleted: the verified copy is confirmed
  with `HeadObject` (its `stored_size`) right before, then in one transaction the blob is locked as a mark locks it (it must be `live`), the
  survivors are read again and D's row set `deleting`, then the object is deleted under the hand-over deadline of R-STOR-024, then the row.
  A claim that a run left unfinished is taken over after `handover_delay` and finished by the next run
  ([Q-STOR-259](#q-stor-259--the-drain-walks-every-copy-it-has-not-claimed-reads-the-survivors-again-under-the-blobs-lock-finishes-its-own-claims-and-a-draining-connection-is-no-longer-filled)).
  The deletion pause of [ha-and-helm.md](ha-and-helm.md) R-HA-025 stops it. The last copy of a blob is never deleted. When D holds no copy,
  `drained_at` is set and `system.storage.connection_drained` written. D's `_journal/` objects and its marker stay: Quader never deletes
  journal objects (R-HA-054).
- **Retiring.** A `drained` connection that no live replica has in its configuration gets `retired_at`.
- **The journal pass.** At the end of a full pass the run lists `_journal/revocations/` on every connection with `drain: false` and copies
  each object one has and another lacks (R-STOR-059).
- **Backlog, metric and alert.** Every `web` and `worker` replica exports from PostgreSQL
  `quader_storage_reconcile_queue{connection}` (queue rows), `quader_storage_reconcile_queue_oldest_seconds{connection}` (the age of the
  oldest) and `quader_storage_connection_filling{connection}` (1 while `filling`, R-STOR-051; [ha-and-helm.md](ha-and-helm.md) Q-HA-393),
  and from the storage summary (R-STOR-028) `quader_storage_connection_backlog_blobs{connection}`,
  `quader_storage_connection_backlog_bytes{connection}` (live blobs and bytes without a present copy on the connection) and
  `quader_storage_connection_fill_ratio{connection}`; the run counts `quader_storage_reconcile_copied_total{connection,mode}` and
  `quader_storage_reconcile_copied_bytes_total{connection,mode}` (`mode` `server`, `stream`). `QuaderStorageReplicationBacklog` fires when
  the oldest queue row of a connection that is not `filling` is older than one hour for 30 minutes ([ha-and-helm.md](ha-and-helm.md)
  R-HA-045).
- **One connection.** The queue stays empty and the pass finds nothing; the run costs one batch query per thousand blobs per full pass,
  one full pass an hour (Q-STOR-257).

**Status.** Built (package C of [docs/developer/blob-store.md](../developer/blob-store.md#the-reconciliation-r-stor-056)):
[internal/blob/reconcile](../../internal/blob/reconcile/), the hooks in [wire_storage_reconcile.go](../../internal/app/wire_storage_reconcile.go)
and the signal after a commit ([reconcile_seams.go](../../internal/blob/reconcile_seams.go)); the decisions Q-STOR-255 to Q-STOR-259. The
storage summary's three gauges are built with package F (R-STOR-028, [gauges.go](../../internal/blob/summary/gauges.go)); the gauges of the
reconciliation also export
`quader_storage_connection_detached` of R-STOR-060. The acceptance criteria below are
[reconcile_test.go](../../test/integration/blob/reconcile_test.go) and, against two real buckets,
[connections_s3_test.go](../../test/integration/blob/connections_s3_test.go); the copies themselves, the throttle, the journal pass and
the signal are unit tests in [internal/blob/reconcile](../../internal/blob/reconcile/). The scrub (R-STOR-027) reads a server-side copy
within the hour after it was made (never-verified copies first) and stamps its `verified_at`, which is the stamp the drain reads, so a drain
beside such copies waits up to about two hours (the next hourly scrub run, then the drain's next walk, Q-STOR-259).

Acceptance criteria (integration, PostgreSQL of the stack with two in-memory stores, unless named):

- With the second store failing, ten uploads land on the first and queue ten rows; after the fault is lifted one run copies all ten, each
  copy row `present` and verified, and the queue is empty.
- A connection added to an installation of twenty blobs is `filling`, gets every blob in one or more runs, and turns `active` after the
  first full pass without a gap.
- `drain: true` on the first connection with every blob on both: after a run the first store holds no blob object, its rows are gone, the
  connection is `drained`, every blob downloads from the second; a blob whose copy on the second is not verified is kept on the first until
  the scrub verified it; a blob whose only copy is on the first is copied before it is deleted.
- A fault that pauses a run after its copy write for longer than `min(commit_deadline, delete_settle / 2)` makes it write again before it
  records the row.
- Integration against S3 (the stack's SeaweedFS, two connections on the buckets `QUADER_TEST_S3_BUCKET_BLOBS` and
  `QUADER_TEST_S3_BUCKET_BACKUPS` under a prefix of the run): a copy between them is server-side; with `server_side: never` it is streamed;
  both downloads return the blob's bytes.

### R-STOR-057 · Collection with several connections
Priority: **P1** · Covers: quader-only

R-STOR-024's per-blob protocol stays; its step (4) deletes every copy, and one connection's outage stops neither the run nor an
uploader for long.

*Built* (package B): [gc.go](../../internal/blob/gc.go) (`deleteObjects`, `removeCopies`, `deleteOn`, `recordDeleted`), the statements
`ClaimBlobCopies`, `CollectCopies`, `DeleteCollectedCopies` ([sweep.sql](../../internal/meta/postgres/queries/sweep.sql)) and
`RecordBlobsDeleted` ([blob.sql](../../internal/meta/postgres/queries/blob.sql)).

- **Step (4) per copy.** The claim (step 3) sets the blob's copy rows `deleting` in its transaction (a copy a drain claimed keeps its
  claim time). After it, the run reads the blob's copy rows and deletes each object on its connection, the connections at once, with
  `DeleteObjects` per connection when its `batch_delete` is true, under the hand-over deadline of R-STOR-024; each successful delete
  deletes its copy row. `object_deleted_at` (step 5) is set when the blob has no copy row left; step (6) drops the blob row and its queue
  rows after `delete_settle`. A copy row in state `deleting` of a blob that is `live` is a drain's claim (R-STOR-056); collection reads the
  copies of the blobs in state `deleting` only, and finishing a drain's delete of a blob it claimed afterwards is harmless.
- **A copy whose delete was never sent is released** ([Q-STOR-237](#q-stor-237--collection-releases-a-copy-whose-delete-was-never-sent-to-the-orphan-sweep-of-its-connection)):
  when the copy's connection is not in this replica's configuration, or its circuit is open (the guard refused the request before it
  left) while another connection of the replica is writable, the row is deleted without a request; the object becomes an orphan of that
  connection, which its orphan sweep removes (R-STOR-026), and `quader_gc_copies_released_total{connection}` and the result's
  `copiesReleased` count it. A delete whose outcome is unknown (a timeout, a transient error after the request left) keeps its row and the
  blob `deleting`, and a later run repeats it under the hand-over rules, as for one store. An upload of the same content therefore waits
  for a `deleting` row (R-STOR-005) no longer than for one store, whatever the state of the other connections.
- **One connection's open circuit stops nothing else**: the run goes on with the other connections and blobs. While no connection of
  the replica is writable (every circuit open, as with the one connection of a plain installation when it is down), the run releases
  nothing and stops its object-store phase as one store's open circuit always did; its claims are handed over
  ([Q-STOR-260](#q-stor-260--collection-releases-a-copy-on-an-open-circuit-only-while-another-connection-is-writable)).
- The session step works per session on its connection (R-STOR-054).
- The orphan that a release leaves is deleted content that stays in the bucket of the connection that was down until its sweep runs
  ([H-1254](../security/object-store-connections.md#h-1254--deleted-content-outlives-its-deletion-on-a-connection-that-was-unreachable)).

Acceptance criteria (integration, PostgreSQL with two in-memory stores; `TestR_STOR_057_CollectionWithSeveralConnections`):

- An unreferenced blob on both connections is collected: both objects are gone, then the rows.
- With the second store's circuit open, the collection deletes the first object, releases the second copy, drops the blob row after
  `delete_settle`, and an upload of the same content started meanwhile answers `201`; the orphan sweep of the second connection removes the
  released object after `min_age` (fake clock), and adopts the released object of the content uploaded again.
- A fault that times out the second store's delete keeps the blob `deleting`; the next run deletes it.
- With every circuit open the run stops and releases nothing.

### R-STOR-058 · The operator check of every copy
Priority: **P1** · Covers: quader-only

The operator check moved here from R-STOR-027, whose scrub it is not
([Q-STOR-240](#q-stor-240--the-scrubs-evidence-becomes-true-by-moving-the-tests-that-name-it-for-another-part)). **Built** (package D,
[blob-store.md](../developer/blob-store.md#storage-verify-r-stor-058-q-stor-200)), with `restore-versions` per connection
([Q-STOR-268](#q-stor-268--restore-versions-takes-each-connections-element-of-the-journal-readers-credentials-file)).

- `quader admin storage verify [--connection <id>] [--since <RFC 3339 time>] [--concurrency N] [--max-items N] [--json]` asks the store
  of each configured connection (or of the one named) for the object of every `present` copy of a `live` blob, with `HeadObject`, using
  the runtime database role and each connection's runtime credential, and changes nothing (it neither sets a state nor writes an audit
  event; the marks stay the scrub's and the reads'). It reads the copies in pages of 1,000 blobs by digest, so its memory does not grow
  with the table, and runs `--concurrency` checks at a time (default 16).
- **Findings.** A copy whose object the store reports missing, or whose size differs from `stored_size`, is a finding of its connection;
  a blob is **lost** when none of its copies is there with its size. The command prints one line per missing copy (`copy <connection>
  <sha256>`) and, for a lost blob, one line per item that references it (`item <project>/<repository>:<path>`, `(trashed)` for a trashed
  one, at most `--max-items` per blob, default 20, with the number of the rest) and a line for a lost blob that no item references (one
  that only a cache metadata entry, a UI asset or an avatar references prints so), then the totals per connection and in all. The last
  line keeps the shape the recovery drills read: `storage verify: <n> blobs checked, <m> without object, <s> of another size, <u> not
  checked, <i> items affected`, where *m* and *s* count the lost blobs.
- **Lost** ([Q-STOR-269](#q-stor-269--the-operator-check-calls-a-blob-lost-when-no-copy-it-could-ask-is-there-and-no-present-copy-was-left-unasked)):
  a blob none of whose copies that the run asked answered with its object of its stored size, and none of whose `present` copies was left
  unasked (on a connection the configuration lacks, outside `--connection`, or not answered). A blob without a `present` copy is lost
  without a question to a store.
- An answer other than "no such object" (a timeout, `5xx`, throttling) is retried twice and then counted as *not checked*, never as
  missing; `AccessDenied` ends the run for that connection (a store answers it for a missing object when the credential may not list the
  bucket), whose remaining copies count as not checked, and the run goes on with the others. `--since` limits the run to blobs created or changed at or after the time (Q-STOR-200); `--json` prints the report as one
  document.
- **Exit status:** `0` when no blob is lost (missing copies are listed, the scrub and the reconciliation repair them); `5` when at least
  one blob is lost, or, with `--connection`, when a copy on that connection is missing or of another size; `2` a usage or configuration
  error, a store that refused a connection's checks included; `75` when nothing is lost but a check was not answered; `1` another failure ([backup-and-restore.md](backup-and-restore.md) R-BAK-013 holds
  the codes).
- The recovery procedure runs it after a point-in-time restore ([ha-and-helm.md](ha-and-helm.md) R-HA-024, R-HA-025), whose
  post-restore step reads `Report.Lost` and the missing copies per connection; the pod-crash scenario of R-HA-052 and the cold-install
  scenario of exit criterion E5.2 run it as the condition "no item row without its object".

Acceptance criteria:

- With the object of one blob deleted behind Quader's back on its only connection, the command exits `5` and prints the items that
  reference the blob, a trashed one marked; with the object back it exits `0`. A cut-off after every row checks nothing
  (`TestR_HA_024_StorageVerifyNamesTheItemsWhoseObjectIsGone`, `TestR_HA_025_StorageVerifySinceNarrowsTheRun`).
- With two connections and one blob's object deleted on the first only, it exits `0` and lists the missing copy; deleted on both, it exits
  `5`; with `--connection default` and the first object deleted, it exits `5` (integration, `test/integration/storageverify`).

### R-STOR-059 · The other key families on several connections
Priority: **P1** · Covers: quader-only

| Family (below each connection's `<prefix>/`) | On which connections | Rule |
|---|---|---|
| `sha256/…` | each connection that holds a copy | R-STOR-052; written by the commit, the reconciliation and a restore; deleted by collection, a drain and the orphan sweep |
| `_uploads/<session-id>/…` | the session's connection | R-STOR-054 |
| `_probe/<replica-id>` | each connection, at each start and conformance check | R-STOR-035 |
| `_connection` | each connection | the marker of R-STOR-051; written once, never deleted by Quader |
| `_journal/revocations/…` | each connection with `drain: false` | below |

- **The revocation journal** ([ha-and-helm.md](ha-and-helm.md) R-HA-054) is shipped to every connection with `drain: false` in the
  shipper's configuration, through each connection's guarded client, so it passes the circuit breaker and the metrics
  ([internal/app/wire_journal.go](../../internal/app/wire_journal.go) `journalTargets`, [ship.go](../../internal/journal/ship/ship.go)).
  An outbox row is deleted once every such connection holds its object; until then a table `revocation_outbox_shipped (outbox_id,
  connection_id)` records the connections that do, and `quader_revocation_journal_pending{connection}` shows the backlog per connection
  (every `web` and `worker` replica counts it from PostgreSQL for the connections without drain of its configuration).
  A connection that is unreachable delays only its own copy: a run of the shipper leaves it out for the rest of the run and walks the
  outbox past the rows that wait for it ([Q-STOR-270](#q-stor-270--a-shipper-run-leaves-out-a-connection-that-failed-and-walks-on-past-the-rows-that-wait-for-it)).
  A connection taken in later receives the older entries through the journal pass of the reconciliation (R-STOR-056)
  ([Q-STOR-241](#q-stor-241--the-journal-goes-to-every-connection-and-an-outbox-row-waits-for-each)).
- **The replay** reads `_journal/revocations/` on every connection of the source that can be listed, merges by key, counts identical bytes
  under one key on two connections as one copy, and treats different bytes under one key as R-HA-054 treats two versions of a key (each
  replayed, flagged with the connections that hold each) ([replay/reader.go](../../internal/journal/replay/reader.go)). A connection that
  cannot be listed is reported as unreadable and the replay goes on with the others; when none can be listed the replay fails
  ([Q-STOR-271](#q-stor-271--a-replay-goes-on-without-a-connection-that-cannot-be-listed-and-fails-when-none-can)). A restore reads the
  connections its manifest names (backup-and-restore.md R-BAK-030), `quader admin post-restore` every connection of its configuration.
- Every other key below a connection's `<prefix>/` is foreign: reported by the orphan sweep, never deleted.

Acceptance criteria (unit, `internal/journal` with two in-memory stores):

- With the second store failing, an entry ships to the first, its outbox row stays with one shipped row, and `quader_revocation_journal_pending{connection="secondary"}`
  is 1; after the fault is lifted the entry lies on both and the outbox row is gone (`TestR_STOR_059_TheJournalGoesToEveryConnection`; the
  gauge's count per connection in `TestR_BAK_014_Gauges` of `internal/backup/gauges` and over PostgreSQL with a shipped mark in
  `TestR_BAK_014_GaugesOverPostgres`).
- A replay over two stores whose objects of one key differ replays both and flags them; identical objects are replayed once
  (`TestR_STOR_059_TheReplayReadsEveryConnection`).

**Status.** Built (the rest of package E): the shipper per connection through the guards, the marks of `revocation_outbox_shipped`, the
backlog per connection, the replay over every connection of a restore and of a post-restore. The journal pass of the reconciliation is
package C's.

### R-STOR-060 · The health of the connections
Priority: **P1** · Covers: STOR-012

- **Circuit breaker per connection and replica**, with the thresholds of `storage.object_store.circuit_breaker` and R-STOR-033's rules of
  what counts; one connection's open circuit changes nothing on the others.
- **Metrics.** The families of R-STOR-033 keep their names and gain the label `connection` (an id of the configuration, at most eight,
  [operations-and-observability.md](operations-and-observability.md) R-OPS-007): `quader_storage_s3_requests_total{connection,operation,outcome}`,
  `quader_storage_s3_request_duration_seconds{connection,operation}`, `quader_storage_s3_bytes_total{connection,direction}`,
  `quader_storage_s3_circuit_open{connection}` ([Q-STOR-243](#q-stor-243--the-object-store-metrics-keep-their-names-and-gain-the-label-connection)).
  New families: those of R-STOR-055, R-STOR-056, R-STOR-057 and R-STOR-027, `quader_storage_copies{connection,state}` (from the storage
  summary) and `quader_storage_connection_detached{connection}` (1 while `detached`).
- **Alerts** (rendered and tested by [ha-and-helm.md](ha-and-helm.md) R-HA-045): `QuaderObjectStoreUnreachable` (every connection's circuit
  open on every replica, critical), `QuaderObjectStoreConnectionUnreachable` (one connection's circuit open on every replica while another
  is reachable, warning), `QuaderObjectStoreUnreachableSingleReplica` (per connection), `QuaderStorageReplicationBacklog`,
  `QuaderStorageCopyDamaged` (a damaged copy not repaired for an hour), `QuaderBlobCorrupt` (beside `QuaderBlobMissing`),
  `QuaderStorageScrubBehind` and `QuaderStorageConnectionDetached`.
- **Readiness** ([Q-STOR-242](#q-stor-242--a-replica-is-first-ready-when-one-connection-passed-and-no-connection-has-a-configuration-error)).
  The check `objectStore` of [operations-and-observability.md](operations-and-observability.md) R-OPS-001 covers every connection. **First
  readiness:** the start check (R-STOR-035) has passed on at least one connection with `drain: false`, and no connection with
  `drain: false` has a configuration error (a configuration error of such a connection stops the process, as for one store today); a
  connection that fails only transiently does not hold a replica back. A replica that starts while one of two connections is down
  therefore becomes ready and serves from the other; a replica that starts while every connection is down stays not ready, as one with
  one store does today (Q-STOR-1). **Afterwards** the check is reported: `degraded` while any configured connection's circuit is open on
  the replica, with the connections named in its reason (`"unavailable: secondary"`); with `health.require_object_store: true` it gates
  while no connection with `drain: false` is reachable. A draining connection's configuration error is logged and shown, and does not stop
  the process, so a connection whose credential was already withdrawn can still be drained of what it holds elsewhere.
- **The object-store view of the replica registry.** Each heartbeat writes the replica's connections with their circuit state and `drain`
  flag ([operations-and-observability.md](operations-and-observability.md) R-OPS-003), which R-STOR-051's states and the API read.
- **The start check and the conformance check per connection** are R-STOR-035's, run for each connection; the conformance command takes
  `--connection <id>`, and for two connections on one endpoint it adds the server-side copy between them.

Acceptance criteria:

- A replica started with the second of two stores failing (in-memory fault, transient) becomes ready with `objectStore` degraded and its
  reason naming the second; started with both failing it stays not ready; started with a configuration error (`AccessDenied`) on the
  second it exits naming the connection; with `drain: true` on the second it starts (unit, `internal/app` readiness evaluation).
- The rule tests of R-HA-045 for each alert above, one firing and one silent.

### R-STOR-061 · The connections in the API
Priority: **P1** · Covers: quader-only

*Built* (package F): [storage_connections_api.go](../../internal/api/v1/storage_connections_api.go) over
[internal/blob/summary](../../internal/blob/summary/connections.go) (`Rows`, `Row.State`, `Views`, `LocalOf`) and the system information's
facts in [facts.go](../../internal/app/facts.go); the decisions Q-STOR-372 and
[Q-API-406](api-and-ui.md#q-api-406--a-connection-of-the-list-carries-the-answering-replicas-settings-and-its-states-are-an-open-string).

- `GET /api/v1/system/storage-connections` → `200` (operation `listStorageConnections`) and
  `GET /api/v1/system/storage-connections/{id}` → `200` or `404` (`getStorageConnection`), both with `settings:read`, the permission of the
  replica list ([Q-API-385](api-and-ui.md#q-api-385--the-connections-are-an-operation-of-the-system-family-read-with-settingsread)).
  They read PostgreSQL only: the rows of R-STOR-052, the replica registry's object-store view, the queue, and the figures of the latest
  storage summary with its `computedAt`. No secret, no credential file path and no key appear.

  ```json
  {
    "connections": [
      {
        "id": "default", "position": 1, "state": "active",
        "endpoint": "https://object.storage.eu01.onstackit.cloud", "bucket": "quader-blobs", "prefix": "quader",
        "credentialSource": "static", "encrypted": false, "keyedNames": false, "drain": false,
        "replicas": {"configured": 7, "live": 7, "circuitOpen": 0},
        "queue": {"blobs": 0, "oldestQueuedAt": null},
        "summary": {"id": "default", "computedAt": "2026-11-22T10:17:00Z", "presentCopies": 412093, "presentBytes": 1893341184512,
                    "backlogBlobs": 0, "backlogBytes": 0, "fillRatio": 1.0, "missingCopies": 0, "corruptCopies": 0,
                    "verifiedWithinPeriod": 401233, "scrubOverdue": 0, "soleCopies": 0},
        "scrubPeriodSeconds": 2592000, "filledAt": "2026-11-23T08:02:11Z", "drainedAt": null, "retiredAt": null,
        "lastFullPassAt": "2026-11-24T09:58:40Z",
        "capabilities": {"payloadChecksumEnforced": true, "batchDelete": true, "metadata": true, "copyReplacesMetadata": true,
                         "serverSideCopyFrom": {}, "checkedAt": "2026-11-22T06:00:12Z"},
        "settings": {"region": "eu01", "forcePathStyle": false, "payloadChecksum": "sha256", "requestChecksumCalculation": "when_supported",
                     "responseChecksumValidation": "when_supported", "batchDelete": true, "storageClass": "", "sseMode": "none",
                     "scrubBytesPerSecond": 52428800},
        "storageKeys": []
      }
    ]
  }
  ```

  *(example values)*. `position` is the place in the configuration of the replica that answered (`null` for a connection it does not
  configure), and so are `endpoint`, `credentialSource`, `drain`, `scrubPeriodSeconds` and `settings` (the non-secret settings of its
  element that R-UI-196's fold shows; Q-API-406); `state` is R-STOR-051's, derived the same on every replica (Q-STOR-372); `summary` is
  `null` before the first storage summary and for a connection it did not count; `storageKeys[]` are R-STOR-088's, each with the copies
  of the latest summary. The list holds every connection ever seen, retired ones too: first the answering replica's in the order of its
  configuration, then the others by id.
- `GET /api/v1/system/info` (R-OPS-004) replaces its object `objectStore` by `objectStore.connections[]` with `id`, `endpoint`, `bucket`,
  `prefix`, `credentialSource`, `encrypted` and `drain` per connection of the answering replica.
- The storage summary (R-STOR-028) gains `connections[]` with the figures of `summary` above; `blobs` and `damaged` keep their meaning
  (blobs, not copies).
- The routes of R-API-040 gain `POST /api/v1/system/tasks/storage-reconcile/run` (`admin`) and the two reads above.
- The storage summary's gauges (R-STOR-056, R-STOR-060, [operations-and-observability.md](operations-and-observability.md) R-OPS-008):
  every replica with the main pool reads the snapshot in the background and exports `quader_storage_copies{connection,state}`,
  `quader_storage_connection_backlog_blobs`, `quader_storage_connection_backlog_bytes`, `quader_storage_connection_fill_ratio`,
  `quader_storage_blobs_damaged{status}` and `quader_storage_summary_age_seconds`; before the first summary none has a series.

Acceptance criteria:

- `GET /api/v1/system/storage-connections` with two connections, one `filling` with half the blobs, lists both in order with their states
  and the figures of the summary; with a caller without `settings:read` it answers `403`; no field holds a file path or a secret
  (unit, `internal/api/v1`, `TestR_STOR_061_…`).

### R-STOR-062 · The seams the encryption and the signed links build on
Priority: **P1** · Covers: quader-only

Stage 1 leaves exactly this room for stages 2 and 3 and specifies neither
([Q-STOR-244](#q-stor-244--the-store-interface-carries-object-metadata-and-every-size-check-compares-the-copys-stored-size),
[Q-STOR-245](#q-stor-245--stage-3-asks-the-store-which-copies-can-serve-a-blob-now)).

- **Object metadata in the store's interface** ([internal/blob/objects.go](../../internal/blob/objects.go) `ObjectStore`, which carries none
  today): `Put` and `CreateMultipart` take a `Metadata` (`map[string]string`, keys `[a-z0-9-]+`, the prefix `quader-` reserved for Quader,
  at most 2 KiB in all, the bound AWS documents for user metadata); `Copy` takes one too, `nil` keeping the source's metadata
  (`x-amz-metadata-directive: COPY`) and a value replacing it (`REPLACE`); `ObjectInfo` of `Head` and `Get` returns it. The S3 adapter,
  the guard and `blobtest` carry it. **Stage 1 writes no metadata.** The start check establishes per connection whether metadata survives a
  write and whether a copy replaces it (R-STOR-035), so stage 2 learns from a row, not from a promise, whether a provider takes its
  wrapped key; stage 2 decides what it puts there.
- **The copy row** carries what stage 2 fills: `object_key` (the name on that connection; every reader takes it from the row, and only a
  write asks the connection's naming function, which in stage 1 returns `blob.Key(prefix, digest)`), `storage_key_id` (`NULL` in stage 1)
  and `stored_size` beside the blob's plain `size`.
- **Every check of a stored object's size compares with the copy's `stored_size`**, not with `blobs.size`: the commit after a multipart or
  copy, the download, the recheck, the operator check, the reconciliation, the scrub, the backup and the restore (today they compare the
  plain size, the research's list in R-STOR-048's open point 8 notwithstanding). In stage 1 the two are equal; stage 2 changes the value,
  not the checks.
- **Reading a copy** goes through one function per connection that returns the blob's bytes and verifies them against the digest
  (identity in stage 1); stage 2 makes it decrypt and authenticate. The scrub, the reconciliation's stream and the backup's stream use it.
- **`encrypted`** is a fixed property of a connection (R-STOR-050, R-STOR-051); a server-side copy is made only between two
  connections with `encrypted: false`: stage 2 writes every copy into an encrypted connection with a new data key (R-STOR-087).
- **For stage 3** (R-STOR-074, "a connection that is not encrypted, holds a sound copy of the blob, is not failing and passed its link
  check"): `blob.Store.Servable(ctx, digest) ([]Copy, error)` returns the copies a read would try now, in the order of R-STOR-055
  (`present`, configured on this replica, circuit closed), each with its connection's `id`, `object_key` and `stored_size`; the connection
  offers `Encrypted()`, the result of its link check, and signing through its guard (`Presign(ctx, key, ttl, …)` against the address stage
  3 hands it), which today bypasses the breaker ([internal/blob/guard.go](../../internal/blob/guard.go) `Presign`). The `id` is the key
  of stage 3's public addresses; the one connection of today is `default` there and here. When and for whom a link is issued is stage 3's.

Acceptance criteria:

- `blobtest` and the S3 adapter (against the fake endpoint) round-trip metadata on `Put`, `CreateMultipart` and `Get`/`Head`, keep it on a
  `Copy` with `nil` and replace it on a `Copy` with a value (unit).
- `Servable` lists the copies in read order and leaves out a missing copy and a connection whose circuit is open (unit).

### R-STOR-063 · The tests of the feature
Priority: **P1** · Covers: quader-only

By the repository's rule that tests stay runnable ([Q-STOR-246](#q-stor-246--every-behaviour-once-at-the-cheapest-tier-no-end-to-end-leg)):
every behaviour once, at the cheapest tier that shows it; nothing grows with the number of formats (a format never sees a connection: it
reaches the store through the host, [architecture.md](architecture.md) Q-ARCH-28); no end-to-end leg; payloads of bytes to kilobytes.

| Test | Tier | Data | Run time (estimate) |
|---|---|---|---|
| Configuration table (R-STOR-050) | unit, `internal/config` | — | 0.1 s |
| Commit routing, fall-over within the request, `min_copies` (R-STOR-053); reads, fall-over, marks, recheck, the daily stamp (R-STOR-055); derivation (R-STOR-052) | unit, `internal/blob`, two `blobtest` stores, the fault hook, the fake database | objects of 1 KiB | 0.6 s |
| Metadata on the seam, `Servable` (R-STOR-062) | unit, `internal/blob/s3` against the fake endpoint, `blobtest` | — | 0.2 s |
| Sessions on their connection (R-STOR-054) | unit, `internal/uploads`, two `blobtest` stores, parts of 1 KiB (`MinPartSize`) | ≤ 8 KiB | 0.3 s |
| Journal per connection, replay union (R-STOR-059) | unit, `internal/journal` | entries of < 1 KiB | 0.2 s |
| Readiness per connection (R-STOR-060) | unit, `internal/app` | — | 0.1 s |
| Scrub reader: digest, size, transient, throttle (R-STOR-027) | unit, `internal/blob/scrub` | 4 KiB | 0.3 s |
| API of the connections (R-STOR-061), damaged blobs (R-STOR-027) | unit, `internal/api/v1` | — | 0.3 s |
| Admin tab and dashboard line ([api-and-ui.md](api-and-ui.md) R-UI-196, R-UI-197) | Vitest | fixtures | 1.5 s |
| Chart: the list, the Secrets and the refusals ([ha-and-helm.md](ha-and-helm.md) R-HA-072) | chart render tests | — | 1 s |
| Fixed properties, detached, forget (R-STOR-051); reconciliation: queue, catch-up, fill, drain, insert bound (R-STOR-056); collection per copy and release (R-STOR-057); orphan sweep, adoption (R-STOR-026); scrub with repair (R-STOR-027) | integration, `test/integration/blob`, PostgreSQL of the stack with two `blobtest` stores in process | ≤ 30 objects of 1 KiB | 15 s |
| The race of R-STOR-005 and R-STOR-024 | integration, the existing race test run with two connections instead of one (it replaces, not adds) | as today | 0 s added |
| Two real buckets: routing, server-side and streamed copy, metadata kept by a copy, fall-over and repair (R-STOR-056, R-STOR-062) | integration, `test/integration/blob`, the stack's SeaweedFS, the buckets `QUADER_TEST_S3_BUCKET_BLOBS` and `QUADER_TEST_S3_BUCKET_BACKUPS` ([testenv.go](../../test/integration/testenv/testenv.go)) | 3 objects of 1 KiB | 5 s |
| Operator check per connection (R-STOR-058) | integration, `test/integration/storageverify` (two connections instead of one) | 3 objects | 1 s added |
| Restore fills the first connection, the reconciliation the second ([backup-and-restore.md](backup-and-restore.md) R-BAK-030); the backup takes the second copy | integration, `test/integration/backup`: the destination of `TestR_BAK_021_RestoreEndToEnd` has a second connection, the damaged-source case a second in-memory store; the reconciliation's half with package C | the existing cases' data, one blob of 15 bytes more | 0 s added (built) |
| Metadata round trip, copy with replaced metadata, a wrong payload checksum refused, the server-side copy between two buckets (R-STOR-035) | the conformance checks: unit against `blobtest`, and the job `s3-conformance` against Silo | probe objects | 2 s added to the job |

What each pipeline job gains (estimates): the unit job about 4 s (Go) and 1.5 s (Vitest); the chart test about 1 s; the integration shard
that runs `blob` about 20 s, the one that runs `storageverify` about 1 s, the one that runs `backup` under 1 s (the checks and the probe of a second prefix, one blob more); `s3-conformance` about
2 s; every end-to-end leg nothing. With 25 formats the cost is the same. Nothing of this needs the owner's provider; what only its real
bucket can show (R-STOR-035's capabilities there) is a run of `quader admin storage check` against it, not a test.

## Encryption of stored content

Stage 2 of the owner's topic of 2026-11-22 on the stored content: a connection can keep its blobs encrypted by Quader, so that its
provider, the provider's operators and whoever can read the bucket see ciphertext only, and the key stays with Quader. It builds on the
seams stage 1 left (R-STOR-062: object metadata on the store's interface, the copy row's name, stored size and key identifier, every size
check on the stored size, one reader per connection). [R-STOR-048](#r-stor-048--client-side-encryption-of-stored-content) is the summary;
this section is the specification. His answers that this section carries out
([ADR-244](../decisions.md#adr-244--encrypted-connections-quaders-own-segment-format-bound-to-the-content-the-wrapped-key-once-at-the-object-a-storage-key-per-connection-under-the-key-ring)):

| Decision | His answer | Where |
|---|---|---|
| 1 Import or build | (a) A format of Quader's own, after the model of the segment chain of his s3-encryption-proxy, bound to the content and not to the object's name; written in this specification, with test vectors, fuzzing and an offline tool that reads a blob without a running Quader. It replaces his decision of 2026-11-06 (that proxy's format, imported as a Go module) | R-STOR-076 to R-STOR-079, R-STOR-089 |
| 2 Where the wrapped data key lies | (e) Once, in the object's metadata, written in the same request as the bytes, bound to the digest, under the connection's storage key; the database holds no key of a blob, only the state, the stored size and the storage key's identifier ("Zweimal ablegen heißt doch es könnte auseinander laufen") | R-STOR-077, R-STOR-083 |
| 3 Keys per project and repository | (a) None; each wrap carries the identifier of its key, so that a level can be added later | R-STOR-077 |
| 4 The root and its rotation | (a) The existing key ring of stored secrets seals the storage keys as it seals every other secret; its existing rotation is the rotation, and no object is touched by it | R-STOR-083, R-STOR-084 |
| 5 Object names on an encrypted connection | (a) A keyed MAC of the digest; by his note ("Ggf. optional?") a detail of an encrypted connection, on by default, fixed once the connection holds data | R-STOR-086 |
| 6 The default of a connection | (a) Not encrypted unless told | R-STOR-075 |
| 7 Changing a connection | (a) Its properties are fixed once it holds data; switching encryption on or off is a new connection, also the same bucket under another prefix, which the reconciliation fills | R-STOR-087 |
| 13 The Bastion's cache | (a) Encrypted in the same format once its chart gives it a key; off by default | [bastion.md](bastion.md) R-BAS-061 |
| 15 Where the storage keys lie | (a) Once: a sealed row of the database per connection and generation, with a command that prints them sealed for safekeeping beside the key ring | R-STOR-083, R-STOR-085 |
| 12 A Bastion and the main store | (b) The handover of a short-lived link and of the data key of one released blob, specified on 2026-11-22 in R-STOR-095, R-STOR-096 and [bastion.md](bastion.md) R-BAS-070 to R-BAS-079; this section leaves the room: one function that yields a copy's data key to the main installation's own code | R-STOR-083 |

**Status.** The format is built: the package `internal/blob/blobcrypt` holds the codec of R-STOR-076 to R-STOR-079, the tail segments of
R-STOR-081 and the keyed name of R-STOR-086, with the vector files, their independent check and the fuzz targets of R-STOR-079 and the
nightly job of R-STOR-091 (package 1 of the developer page). The Bastion's cache seals with it once its chart gives it a key
([bastion.md](bastion.md) R-BAS-061, package 5, built). The keys are built (package 2, on 2026-11-22): the migration of R-STOR-083 with the
table `storage_keys` and its two sealed columns, the drawing at a connection's first sight with the refusal over a used location, the keys a
replica holds with the change area `storage_keys` and the re-read for an unknown identifier, `quader admin storage keys export`, `import` and
`new-generation` with their audit events (R-STOR-085), and `blob.Store.CopyDataKey` with its architecture rule (`internal/blob/keys`,
[admin_storage_keys.go](../../cmd/quader/admin_storage_keys.go)). The encrypted connections are built (package 3, on 2026-11-22):
`encrypted: true` and `keyed_names` are accepted (R-STOR-075), the start check requires `metadata`, every write of a blob to an encrypted
connection is the stored format with its record (R-STOR-080), the sessions seal their data keys and their staging (R-STOR-081), the reads
open the record and authenticate every segment and mark as R-STOR-082 says, and the objects get keyed names (R-STOR-086). The connections'
API shows `keyedNames` and `storageKeys[]` (R-STOR-088, built ahead with stage 1's package F). The jobs of R-STOR-087, the backup's pool with
its keys and the restore (backup-and-restore.md R-BAK-031, R-BAK-032) are built (package 4, on 2026-11-23): every stage-1 job reads and
writes an encrypted connection in the stored format, the backup and the restore handle its copies. The offline tool (R-STOR-089), the chart's
acceptance of `encrypted` with its refusal of `keyed_names` alone ([ha-and-helm.md](ha-and-helm.md) R-HA-072), the alert
`QuaderStorageUnknownKey`, the API's `state: destroyed` with `destroyedAt` and R-STOR-090's sentences are built (package 6, on 2026-11-23).
The order of the packages is on the developer page
[stored-content-encryption.md](../developer/stored-content-encryption.md). The destruction of a retired connection's keys, so that what stays
at a provider becomes unreadable (R-STOR-097 to R-STOR-103,
[Q-STOR-309](#q-stor-309--a-storage-key-is-a-row-with-an-identifier-of-its-own-drawn-at-the-connections-first-start-never-deleted) (b),
[ADR-255](../decisions.md#adr-255--the-storage-keys-of-a-retired-connection-can-be-destroyed-so-that-what-stays-at-a-provider-becomes-unreadable)),
is built (package 7, 2026-11-23); its part in the backup, the pool's keys of R-BAK-032, is built with package 4. Its security pages are [stored-content-format.md](../security/stored-content-format.md) (the format and what it withstands),
[stored-content-keys.md](../security/stored-content-keys.md) (the keys and who holds them) and
[stored-content-visibility.md](../security/stored-content-visibility.md) (what stays visible to a provider).

**The keys, in the owner's terms** (the table he asked for on 2026-11-22):

| Key | How many | Protects | Lies | Comes into being | Changes |
|---|---|---|---|---|---|
| The root: his instance key-encryption key, the existing key ring of stored secrets ([architecture.md](architecture.md) R-ARCH-021) | one active, retired ones only to open | the storage keys and name keys, as every other sealed value of the database | as files of the key ring Secret, in the pods `api`, `packages`, `worker` and the restore Job; never in the database, never in a bucket | by the operator, required today | by the existing rotation: a new key, two rollouts, the re-encrypt task, the old key removed (R-STOR-084) |
| A storage key: a key-encryption key per encrypted connection | one active per encrypted connection; a further generation only on command | the data keys of every object of that connection | sealed under the root in one row of `storage_keys`; opened only in the memory of the pods | drawn by Quader when the connection is first seen; no operator handles it | itself never; its sealing at every rotation of the root; destroyed with every other key of its connection, on command, once the connection is retired (R-STOR-099) |
| A data key: the key of one stored object | one per object written on a connection | the bytes of that object, in segments of 64 KiB | wrapped under the storage key in the object's metadata, bound to the digest; open only in memory during an upload or a download | drawn by the writer of that object | never |
| The name key (decision 5) | one per encrypted connection with keyed names | nothing; it turns a digest into the object's name | as a storage key | with the connection's first storage key | never, or every object would get another name; destroyed with the connection's storage keys (R-STOR-099) |

**Terms of this section.**

| Term | Meaning |
|---|---|
| Encrypted connection | A connection with `encrypted: true` (R-STOR-075). |
| Record | The four metadata keys of an object on an encrypted connection: the format, the storage key's identifier, the wrapped data key and the commitment (R-STOR-077). |
| Segment | 64 KiB of plaintext sealed with AES-256-GCM, stored with its nonce and tag (R-STOR-076). |
| Final segment | The last segment of an object, which carries its plaintext length and SHA-256 (R-STOR-076). |
| Stored size | The size of the object on the connection; on an encrypted connection `S(L)` of the plaintext size `L` (R-STOR-076). |

### R-STOR-075 · An encrypted connection
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-22 (package 3): [storage_connections.go](../../internal/config/storage_connections.go) (the rules),
[startcheck.go](../../internal/blob/startcheck.go) (`metadata`), [sealedonly.go](../../internal/blob/sealedonly.go) (the client of an encrypted
connection refuses a blob's object without a record, [Q-STOR-382](#q-stor-382--the-client-of-an-encrypted-connection-refuses-a-blobs-object-without-a-record)).

- **`encrypted: true`** on an element of `storage.connections` (R-STOR-050) makes the connection encrypted: every blob object it holds is
  stored in the format of R-STOR-076 with its record (R-STOR-077), and every staging object of an upload session on it too (R-STOR-081).
  `false` is the value in the field (the owner's decision 6), and the plain path stays one line; the encrypted one is one more:
  `storage.connections: [{bucket: quader-blobs, encrypted: true}]`.
- **`keyed_names`** is the detail of an encrypted connection (decision 5 with his note): `true` by default on an encrypted connection, so
  that a lister of the bucket cannot confirm that a known file is stored (R-STOR-086); `false` keeps the names of R-STOR-001. On a
  connection with `encrypted: false` the key is not offered: a value there is refused ("keyed_names is a detail of an encrypted connection;
  on a connection that is not encrypted the content is readable anyway").
- **Fixed with the connection.** `encrypted` and `keyed_names` are fixed once the connection holds a copy, as bucket and prefix are
  (R-STOR-051, [Q-STOR-224](#q-stor-224--bucket-prefix-and-encrypted-are-fixed-endpoint-and-credentials-may-change-and-a-marker-names-the-location));
  `storage_connections` gains `keyed_names boolean NOT NULL DEFAULT false` (R-STOR-083's migration). Switching encryption on or off is a
  new connection that the reconciliation fills (decision 7, R-STOR-087).
- **Nothing more for an operator.** The configuration holds no key and the chart no Secret for it: Quader draws the connection's storage key
  and name key when it first sees the connection and keeps them sealed in the database (R-STOR-083). What the operator keeps is the key
  ring, as today, and the export of the storage keys (R-STOR-085, R-STOR-090).
- **What the store must support** (the start check of R-STOR-035, [Q-STOR-306](#q-stor-306--an-encrypted-connection-needs-metadata-and-without-a-copy-that-replaces-it-the-finalize-streams)):
  `capabilities.metadata` is required, because the record lives there; an encrypted connection whose store does not keep the metadata of
  the probe's `PutObject` is refused as a configuration error ("connection archive: the store did not keep the metadata of the probe
  object; an encrypted connection keeps each object's wrapped data key there (storage.md R-STOR-077)"). `capabilities.copyReplacesMetadata`
  is optional: without it the finalize of an upload session streams through the replica (R-STOR-081). Everything else is as for any
  connection.
- **Part sizes.** While any connection is encrypted, `storage.object_store.multipart.part_size` and `storage.uploads.part_size` are
  multiples of 64 KiB, so that a part holds whole segments; the defaults (16 MiB) are.

Validation at start, beside R-STOR-050's (each a configuration error naming the key and the connection):

1. `keyed_names` set on an element with `encrypted: false`.
2. A part size that is not a multiple of 65,536 while an element is encrypted.
3. `encrypted: true` on a connection whose start check finds no `metadata` capability (above).
4. The rules against the database: a changed `encrypted` or `keyed_names` while the connection holds a copy (R-STOR-051), and an encrypted
   connection without keys whose location already holds objects (R-STOR-083).

Configuration (the keys this section adds to an element of R-STOR-050):

```yaml
storage:
  connections:
    - id: archive                       # example
      bucket: quader-archive            # example
      encrypted: true                   # default: false (the owner's decision 6)
      keyed_names: true                 # default with encrypted: true; refused with encrypted: false (R-STOR-086)
```

Acceptance criteria:

- A table test of the configuration (unit, `internal/config`): `encrypted: true` alone loads with `keyed_names: true`; `keyed_names: false`
  on a plain element, and `storage.uploads.part_size: 100000` beside an encrypted element, are each refused with the key named.
- A replica whose encrypted connection's in-memory store drops metadata (the fault hook) exits with the message above; with
  `encrypted: false` on the same store it starts (unit, `internal/app`).

### R-STOR-076 · The stored format: segments, the final segment and the stored size
Priority: **P1** · Covers: quader-only

The format `quader-blob-v1` is Quader's own (the owner's decision 1 (a)), specified here so that another implementation can read and write
it without Quader's code ([Q-STOR-300](#q-stor-300--segments-of-64-kib-under-aes-256-gcm-with-the-nonce-stored-in-each-segment),
[Q-STOR-302](#q-stor-302--an-object-ends-with-a-final-segment-that-carries-its-length-and-its-sha-256),
[Q-STOR-305](#q-stor-305--one-object-holds-at-most-4-tib-of-plaintext)). It is modelled on the segment chain of the owner's
s3-encryption-proxy (a third project's tool, at its release 5.4.2: AES-256-GCM over 64 KiB segments, a random nonce per segment, a sealed
trailer) and differs from it where Quader needs it to: nothing is bound to the object's name, the end of an object is a marked final segment,
the record binds the digest, names its key and commits to the data key (R-STOR-077). The proxy cannot read Quader's objects, and Quader does
not read the proxy's. The code is the package `internal/blob/blobcrypt`, standard library only.

**Notation.** `‖` is concatenation; `u8(x)` one byte, `u64(x)` eight bytes, big-endian; `FMT` the 14 ASCII bytes `quader-blob-v1`;
`HKDF(ikm, salt, info)` HKDF-SHA-256 ([RFC 5869](https://www.rfc-editor.org/rfc/rfc5869)) with 32 bytes of output, an absent salt being
the empty string; `Seal(k, n, p, a)` AES-256-GCM ([NIST SP 800-38D](https://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf))
with key `k`, 12-byte nonce `n`, plaintext `p` and additional data `a`, returning the ciphertext followed by the 16-byte tag.

- **The data key and what derives from it.** Each object has a data key `DEK` of 32 bytes from the operating system's random source,
  drawn by the writer of that object and used for no other object (R-STOR-080, R-STOR-087). `DEK` itself seals nothing; from it derive
  the segment key `K_seg = HKDF(DEK, "", FMT ‖ " segments")`, the commitment `C = HKDF(DEK, "", FMT ‖ " commitment")` (R-STOR-077) and the
  key of an upload session's tails `K_tail = HKDF(DEK, "", FMT ‖ " staging tails")` (R-STOR-081).
- **Data segments.** A plaintext of `L` bytes is cut into `n = ⌈L / 65,536⌉` data segments: segment `i` (`0 ≤ i < n`) holds the plaintext
  bytes `[65,536·i, min(65,536·(i+1), L))`, so every data segment but the last holds 65,536 bytes and the last 1 to 65,536. Segment `i` is
  stored as `N_i ‖ Seal(K_seg, N_i, P_i, FMT ‖ u8(0x00) ‖ u64(i))`, 28 bytes more than its plaintext.
- **The final segment** follows at index `n`: its plaintext is `T = u64(L) ‖ SHA-256(plaintext)` (40 bytes), stored as
  `N_n ‖ Seal(K_seg, N_n, T, FMT ‖ u8(0x01) ‖ u64(n))`, 68 bytes. The mark `0x01` says "last": an object ends with exactly one final
  segment and holds none elsewhere. An empty plaintext is the final segment alone.
- **What a segment authenticates:** the format's identifier (so its version is authenticated), its index, and whether it is the last; with
  the data key, the object it belongs to. **Not** the object's name, the bucket or the connection: an object keeps its meaning when it is
  copied, by the finalize of an upload session (R-STOR-081) and into a backup's pool (backup-and-restore.md R-BAK-031).
- **Nonces.** 12 bytes stored in front of each segment's ciphertext; a reader takes them as stored and needs no rule for them. A writer
  MUST NOT seal two different plaintexts, or one plaintext with two different additional data, under one `K_seg` and one nonce; sealing the
  same plaintext with the same additional data and nonce again (a retry, byte for byte the same) is allowed. Quader's writers derive the
  nonces from a seed per part (R-STOR-080,
  [Q-STOR-301](#q-stor-301--a-writer-derives-the-nonces-from-a-seed-per-part-so-that-a-retry-sends-the-same-bytes-without-keeping-them));
  any other source that keeps the rule is valid.
- **The object** is `seg_0 ‖ … ‖ seg_{n−1} ‖ final`, with no header: what a reader needs before the first segment comes with the record.
- **The stored size** is `S(L) = L + 28·⌈L / 65,536⌉ + 68`. The inverse: a size below 68 is not in the format; with `D = S − 68`, `D = 0` is
  `L = 0`; otherwise `n = ⌈D / 65,564⌉` and `r = D − 65,564·(n − 1)`, and the size is in the format only if `29 ≤ r ≤ 65,564`, with
  `L = D − 28·n`. A size outside the format is refused before a byte is read.

  | `L` (plaintext) | `S(L)` (stored) |
  |---|---|
  | 0 | 68 |
  | 1 | 97 |
  | 65,535 | 65,631 |
  | 65,536 | 65,632 |
  | 65,537 | 65,661 |
  | 16 MiB (one part by default) | 16,784,452 |
  | 100 MiB (the multipart threshold) | 104,902,468 |
  | 10 GiB (the default upload limit) | 10,742,005,828 |
  | 4 TiB (the format's limit) | 4,399,925,559,364 |

  Not in the format, for example: 0, 67, 69, 96, 65,633, 65,660. The overhead is 0.043 % plus 68 bytes; a blob of 1 MiB grows by 516 bytes.
- **Limits.** `L ≤ 2^42` (4 TiB), so an object has at most `2^26` data segments and is at most 5 TiB stored, the largest object an
  S3 store keeps; a writer refuses more. One data key seals at most `2^31` distinct segments (R-STOR-081 counts them for a session,
  [Q-STOR-318](#q-stor-318--a-session-counts-the-segments-it-seals-under-its-data-key)), half the `2^32` invocations NIST SP 800-38D §8.3
  allows one key with random 96-bit nonces (the research of 2026-11-22, topic B.2).

**Known answers.** Computed for this specification on 2026-11-22 by an implementation independent of Quader's code (Python 3.12 with the
`cryptography` package 38.0.4, written from the text above); they are part of the vector file of R-STOR-079. With `DEK` the bytes `00 01 … 1f`
and the nonces of R-STOR-080 from the seed `60 61 … 7f`:

- `K_seg` = `3671ce0541d176175bb26410c7f0fc2da1b39c6c2aac6f5e6370a6d8ecab751d`;
  `C` = `1f85b68c333456650f07544250d71ec2183829bbd73dbe32cead4ecbc7e47733`;
  `K_tail` = `c1ad0bdb618d3d83b751d1369a6ad44e43b17af56a2f61426198c078b8c87af5`.
- The empty plaintext is stored as the 68 bytes
  `0a34ba7650ec909a28eb0538f53f7a84be9ab01ed68575fe09db310b105f9920c9514c8d9c6697e73cd3ad5a6ffa194411d4ecc171d9eb47d7abee250da8fbad07dd2cad`.
- The plaintext `a` (`L = 1`, SHA-256 `ca978112…48bb`) as the 97 bytes
  `05c628666f8f6834df05aa27f848cff1e0157ab14ec8c092f5c25a9c72182e548168cd912fdcca99e766a8b8075a85ce9f94df79a3778bef284bd3aa9bfabf3803858b1f077d62b7ffbc5fef76005697ce8f12da2adf7fb2b20f25458434a6de71`.
- The 65,537 bytes `i mod 251` (SHA-256 `237356e1…529d`) as 65,661 bytes whose SHA-256 is
  `f547d3127b4c42a11f36ff45996bf301cd35d84bbde6c4d0c22b0beb850ff144`.

Acceptance criteria:

- The known answers above and the vectors of R-STOR-079 reproduce (unit, `internal/blob/blobcrypt`).
- `S` and its inverse agree on the boundary table, every size in the "not in the format" list is refused, and a writer refuses
  `L = 2^42 + 1` (unit; the inverse is also a fuzz target, R-STOR-079).

### R-STOR-077 · The record at the object: the wrapped data key, the key's identifier, the format and the commitment
Priority: **P1** · Covers: quader-only

The data key lies once, at the object, in the same request as the bytes (the owner's decision 2 (e)): as four keys of the object's metadata
below the prefix `quader-`, which stage 1 reserves for Quader (R-STOR-062). The database keeps no key of a blob; the copy row keeps its state,
its stored size and the storage key's identifier (`blob_copies.storage_key_id`), which is bookkeeping, never what a read opens
([Q-STOR-303](#q-stor-303--the-wrap-carries-the-digest-and-the-size-in-its-sealed-plaintext-under-a-key-derived-per-wrap),
[Q-STOR-304](#q-stor-304--a-commitment-to-the-data-key-in-the-record-checked-before-the-first-segment)).

| Metadata key | Value |
|---|---|
| `quader-format` | `quader-blob-v1` |
| `quader-key` | the identifier of the storage key that wrapped the data key (R-STOR-083), 3 to 64 characters of `[a-z0-9-]`, for example `sk-archive-1-3f9a0c2e` |
| `quader-wrap` | `W` in base64url without padding ([RFC 4648](https://www.rfc-editor.org/rfc/rfc4648) §5): 116 bytes, 155 characters |
| `quader-commitment` | `C` in base64url without padding: 32 bytes, 43 characters |

- **The wrap.** `W = salt ‖ nonce ‖ Seal(K_w, nonce, DEK ‖ digest ‖ u64(L), FMT ‖ " wrap" ‖ u8(len(id)) ‖ id)`, where `salt` (16 bytes) and
  `nonce` (12 bytes) are drawn from the random source for each wrap, `K_w = HKDF(storage key, salt, FMT ‖ " wrap")`, `digest` is the blob's
  SHA-256 (32 bytes), `L` its plaintext size and `id` the storage key's identifier as in `quader-key`. A key derived per wrap puts no bound of
  its own on the number of wraps under one storage key: two wraps share a key only if they draw the same salt, which after `2^48` wraps has a
  probability below `2^-32`.
- **The binding to the content.** The wrap is bound to the digest and the size through its sealed plaintext, and to its storage key through
  its additional data: a reader compares the digest and the size it unwraps with those of the copy it reads (the row's `sha256` and the blob's
  `size`) and refuses a difference. Whoever can only write the bucket cannot make a wrap (he has no storage key), and a genuine wrap of
  another blob names another digest. The offline tool, which has no database, learns the digest from the wrap (R-STOR-089).
- **The commitment.** A reader computes `C` from the data key it unwrapped and compares it in constant time with `quader-commitment` before
  it opens a segment, so that a record can name one data key only, whoever made it.
- **Size.** The four keys and values take about 290 bytes, well below the 2 KiB stage 1 allows (R-STOR-062) and the user metadata bounds
  AWS documents (2 KB) and the software behind the owner's provider documents (24 KiB; research of 2026-11-22, not verified at the provider).
- **Room for another level of keys** (decision 3): each wrap names its key. A later level (a key per project) would add further wraps under
  further keys (`quader-wrap-2` beside `quader-key-2`); this version writes and reads exactly the four keys and ignores any other key below
  `quader-`.

**The order in which a reader checks** (each failure has its name; R-STOR-082 says what each one marks):

1. `quader-format` is present and equal to `quader-blob-v1`; otherwise `format`.
2. `quader-key` names a storage key of this connection (R-STOR-083); a replica that does not hold it reads the rows again once; still
   unknown, `unknown_key`. An identifier whose row is destroyed is `destroyed_key` (R-STOR-101,
   [Q-STOR-406](#q-stor-406--a-read-that-meets-a-destroyed-key-answers-destroyed_key-and-marks-the-copy)). A `quader-key` that is
   missing, or is no identifier of the table above, is `record`
   ([Q-STOR-322](#q-stor-322--a-missing-or-malformed-quader-key-is-record-not-unknown_key)).
3. `W` opens under that key; otherwise `record`.
4. The unwrapped digest and size equal the copy's; otherwise `record`.
5. `S(L)` equals the copy's `stored_size` and the size or total the store answered; otherwise `size`.
6. `C` from the data key equals `quader-commitment`; otherwise `commitment`.
7. The segments in their order, then the final segment (whose `L` and digest must equal the copy's), then on a whole read the SHA-256 of
   the plaintext; otherwise `segment`, `final` or `digest`.

Steps 1 to 6 come before the first byte; step 7 is the body.

**Known answers** (computed as in R-STOR-076): with the storage key `20 21 … 3f`, the identifier `sk-default-1-0a1b2c3d`, the salt
`80 81 … 8f`, the wrap's nonce `90 91 … 9b` and the plaintext `a` of R-STOR-076, `quader-wrap` is
`gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam7m4IyuNMsFf5oiyZvVVCx8bBxSnY5kNUOulc9ByS8wknq4HpO9Zz-lqXOkpDX_rlAAJ5DJCXzMRmCNWG9_7zOuut11EonL3axPC5fq0E9cwuDeQCQ1A6Xs`
and `quader-commitment` is `H4W2jDM0VmUPB1RCUNcewhg4KbvXPb4yzq1Oy8fkdzM`; the same wrap opened with the identifier
`sk-default-2-0a1b2c3d` fails.

Acceptance criteria:

- A record written and read back round-trips; the known answers reproduce; the record of every vector is at most 2 KiB (unit,
  `internal/blob/blobcrypt`).
- Each mutation of R-STOR-078 that changes the record is refused with the name of the step above (unit).

### R-STOR-078 · What the format withstands
Priority: **P1** · Covers: quader-only

Each attack of the research's table (s3-encryption-proxy's codec, run against it on 2026-11-22) and of the state of the art (topic B.2),
with what stops it in this format. The attacker can write the bucket and holds no storage key, unless the row says otherwise.

| Attack | Stopped by | Refused |
|---|---|---|
| Truncation at a segment boundary, the final segment cut off | the size (`S(L)` against the stored size and the store's answer); the missing final segment | before the first byte, whole and ranged |
| Truncation inside a segment, or bytes appended | the size rule (not in the format, or not `S(L)`) | before the first byte |
| Segments reordered | the index in each segment's additional data | at the first moved segment |
| A segment dropped | the size; the index | before the first byte |
| A segment of another object put in | another data key: its tag fails | at that segment |
| A data segment presented as final, or the final segment as data | the mark in the additional data | at that segment |
| A whole object put under another blob's name | the wrap's digest is not the copy's | before the first byte |
| The record of another object put on this body | the wrap's digest is not the copy's; unchecked, the other data key would fail the first segment | before the first byte |
| A changed record: format, key identifier, wrap or commitment | steps 1, 2 and 3, 3, 6 of R-STOR-077 | before the first byte |
| A record made under a storage key of another connection or another installation | an identifier this connection does not hold, or a wrap that does not open | before the first byte |
| The object copied to another connection of the same installation | that connection holds no key of this identifier | before the first byte |
| A ciphertext that opens under two data keys (key commitment, Albertini and others, USENIX Security 2022) | the commitment: a second data key has another `C` | before the first byte |
| Parts of two sealings of one upload session spliced (a part sent again with other bytes) | the final segment's SHA-256 and the reader's digest | at the end of a whole read |
| One bit changed anywhere | the tag of that segment, or of the wrap | at that segment, or before the first byte |
| An older genuine object of the same content put back (a noncurrent version, `restore-versions`, a restored database) | not stopped, and harmless: the same plaintext under a storage key of this connection that is still trusted, because a leaked storage key is answered by replacing the connection (R-STOR-085); unless its key was destroyed (R-STOR-097), which happens only to a retired connection: then the record names a destroyed key | `destroyed_key`, before the first byte (R-STOR-101) |

What the format does not stop is on its security page: damage outside a ranged read's window
([H-1300](../security/stored-content-format.md#h-1300--a-ranged-read-does-not-see-damage-outside-its-window)), objects written by a holder
of a storage key ([H-1301](../security/stored-content-format.md#h-1301--a-holder-of-a-storage-key-can-write-objects-that-authenticate)),
and the plaintext's size ([H-1312](../security/stored-content-visibility.md#h-1312--the-stored-size-gives-away-the-exact-plaintext-size)).

Where a failure surfaces: a failure of steps 1 to 6 comes before the first byte, so the read falls over to another copy (R-STOR-055); a
failure inside the body comes after earlier segments were released, each of them authenticated, and aborts the response as R-STOR-013 says.

Acceptance criteria:

- One unit test per row (`internal/blob/blobcrypt`), each a mutation of a vector's object or record that is refused with its named reason,
  and for the last row a test that the older object opens and yields the same plaintext; every test runs in milliseconds.

### R-STOR-079 · Test vectors, fuzz targets and an independent check of the format
Priority: **P1** · Covers: quader-only

- **The vectors are files of the repository**: `internal/blob/blobcrypt/testdata/vectors.json` and, per case, the stored object
  `testdata/objects/<case>.bin` and its record `testdata/objects/<case>.record.json`. Each case gives its inputs in hex (data key, storage
  key, identifier, salt, the wrap's nonce, the seed and, explicitly, every segment's nonce, so that a reader needs no nonce rule), the
  plaintext as a rule (`i mod 251` over `L` bytes), and the outputs (the record, the stored object's size and SHA-256, the keyed name). Cases:
  `L` = 0, 1, 65,535, 65,536, 65,537 and 196,609 (three full segments and one byte); a staging tail at session offset 131,072 (R-STOR-081);
  a keyed name (R-STOR-086); and one negative case per row of R-STOR-078, a mutated object or record with the reason it must fail with. The
  files together stay below 1 MB.
- **Captured once, checked independently, never regenerated.** The builder writes them from the first implementation and checks every case
  once with an implementation independent of it before they are committed: a script `tools/blobvectors/check.py` on Python's `cryptography`,
  run by hand and not in CI, whose result and date the developer page records. The known answers of R-STOR-076 and R-STOR-077 were computed
  that way for this specification and must appear in the file unchanged. A change of the format is a new identifier (`quader-blob-v2`) with
  vectors of its own.
- **Fuzz targets** in `internal/blob/blobcrypt`, named for what they read: `FuzzPlainSize` (the inverse of `S` never panics and agrees
  with `S`), `FuzzOpenRecord` (any metadata map yields a data key or one of the named errors), `FuzzReadObject` (any body under a valid
  record yields the plaintext or a named error, and never releases a byte of a segment that failed), `FuzzReadRange` (any range over a vector
  object yields exactly the window's bytes or an error, never a byte that was not authenticated) and `FuzzOpenTail` (R-STOR-081's tail
  objects). Their seeds are the vectors and run in every `go test`; the nightly runs each for five minutes (R-STOR-091,
  [Q-STOR-319](#q-stor-319--every-behaviour-of-the-encryption-once-at-the-cheapest-tier-and-its-fuzz-targets-in-the-nightly)).
- **No interoperability test with the proxy**: the formats differ by design (R-STOR-076).

Acceptance criteria:

- `go test ./internal/blob/blobcrypt/` reads every case of the vector file, reproduces the positive ones and refuses the negative ones with
  their reasons; the five fuzz targets exist and their seed corpora pass.
- The developer page names the date and the outcome of the independent check, and the commit that adds the vectors names it too.

### R-STOR-080 · Writing to an encrypted connection: one request and Quader's multipart
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-22 (package 3): [sealedwrite.go](../../internal/blob/sealedwrite.go) and the multipart of
[transfer.go](../../internal/blob/transfer.go). The streams of the reconciliation, of the scrub's repair and of a restore into an encrypted
connection (package 4, on 2026-11-23) go through `Connection.WriteSealedStream` ([sealedstream.go](../../internal/blob/sealedstream.go)),
the same writes with one part in memory in place of the spool
([Q-STOR-440](#q-stor-440--a-stream-into-an-encrypted-connection-holds-one-part-in-memory-not-the-spool)).

Every write of a blob from a spool goes this way on an encrypted connection: an upload (R-STOR-004 to R-STOR-006), the fill of a remote
repository (the proxy's artifacts and documents, through `items.Spool` and `blob.Store.CommitAll`), an import (through the target format's
publish protocol), the generated items of a virtual repository, the shared assets of the UI and the avatars, the commit's repair case, and
the streams of the reconciliation and of a restore into an encrypted connection (R-STOR-087, backup-and-restore.md R-BAK-031).

- **Per write and connection** a new data key, the connection's active storage key (R-STOR-083) and the name from the connection's naming
  function (R-STOR-086). With `min_copies` above 1, each encrypted connection gets its own data key.
- **Nonces from a seed.** The writer draws a 32-byte seed from the random source once per write and derives each segment's nonce as the
  first 12 bytes of `HMAC-SHA-256(seed, FMT ‖ " nonce" ‖ u8(mark) ‖ u64(i))`, with the segment's mark (`0x00` data, `0x01` final) and index.
  The spool does not change during the request, so a part encrypted twice is the same bytes twice.
- **The ciphertext is never kept.** The body of each request is a seekable reader that encrypts its section of the spool as it is read, one
  segment in memory; the SDK's retry seeks back and gets byte for byte what it sent before (Q-STOR-301).
- **Payload checksums** (the connection's `payload_checksum`, R-STOR-006) are taken over the stored bytes, so the writer encrypts a part once
  to hash it and once to send it; `none` skips the first pass. The blob's own SHA-256 cannot be the checksum of the request on an encrypted
  connection as it is on a plain one.
- **One request** (a plaintext up to `multipart.threshold`, and the empty blob): one `PutObject` of `S(L)` bytes with the record.
- **Quader's multipart**: `CreateMultipartUpload` with the record (the digest is known: the spool hashed it); parts of `part_size` plaintext,
  each stored as its segments; the last part holds the remaining data segments and the final segment; `CompleteMultipartUpload` with each
  part's checksum; then `HeadObject`, whose size must be `S(L)` and whose `quader-key` must be the one written. Another size or record fails
  the write as a wrong size does today: the commit falls over to the next connection and the object is left to the orphan sweep (R-STOR-053).
- **The copy row**: `stored_size = S(L)`, `storage_key_id` the identifier written, `present`, and verified where the connection enforces
  payload checksums (R-STOR-052): Quader encrypted what it hashed, and the store refused any other bytes.
- **Cost**: with a payload checksum, AES-256-GCM twice and SHA-256 once more per stored byte; without, AES-256-GCM once. The proxy's codec
  measured about 4.5 GB/s per core for encryption on an Apple M5 Pro (research of 2026-11-22, its own report); nothing was measured on
  Quader's hardware.

Acceptance criteria (unit, `internal/blob`, the in-memory store with an encrypted connection and its fault hook):

- An upload of 1 KiB lies on the encrypted connection as 1,120 bytes with the four record keys; its copy row has `stored_size` 1,120 and the
  storage key's identifier; a download returns the 1 KiB.
- An upload of 192 KiB with a part size of 64 KiB, whose second `UploadPart` fails once by the fault hook, completes; the bytes of the
  repeated part equal those of the failed attempt (the fault hook keeps both), and the object reads back.
- Two uploads of the same content to one connection write two different bodies (each its own data key); the later one is the object, and it
  reads back (A6 on an encrypted connection, R-STOR-088).

### R-STOR-081 · Upload sessions on an encrypted connection
Priority: **P1** · Covers: STOR-022

A multi-request upload session (R-STOR-011) stays on its connection (R-STOR-054); on an encrypted one it encrypts each part as it is sent
and carries its data key between requests in its row
([Q-STOR-307](#q-stor-307--a-sessions-data-key-is-sealed-in-its-row-and-its-tails-are-sealed-under-a-key-of-their-own)).

**Built** on 2026-11-22 (package 3): [internal/uploads](../../internal/uploads/sealed.go) and the commit of the staging object in
[sealedwrite.go](../../internal/blob/sealedwrite.go), with the builder's decisions
[Q-STOR-380](#q-stor-380--a-claim-reserves-one-part-and-the-records-count-what-was-sealed) (the counter) and
[Q-STOR-381](#q-stor-381--a-final-segment-sent-alone-is-not-recorded-as-a-part) (a final segment alone). A session that moves to an encrypted
connection before its first write (R-STOR-054) draws its data key at the move.

- **The data key** is drawn when the session starts and sealed with the key ring into `upload_sessions.sealed_data_key`, a column registered
  with `internal/secrets` (table `upload_sessions`, primary key `id`, R-ARCH-021), so that any replica that takes the writer claim opens it;
  nothing of it stays in a replica's memory, spool or Valkey after a request. The re-encrypt task of a rotation reseals it like every sealed
  value (R-STOR-084).
- **The staging object** `_uploads/<session-id>/data` is created without a record: its digest is not known yet, and it is never read as a
  blob.
- **Parts.** A request's parts are cut from its spool segment as today, each `part_size` of plaintext, and stored as data segments of the blob
  whose indexes count from the part's offset (`received / 65,536`); the request draws a new seed, so a part that a later request sends again
  gets other nonces. `upload_session_parts` gains `stored_size` and `stored_sha256`, the size and SHA-256 of the part as sent (on a plain
  connection equal to `size` and `sha256`); the payload checksum and the completion use them.
- **Tails.** A tail object `_uploads/<session-id>/tail-<writer>` holds its bytes as tail segments under `K_tail`: segment `j` of a tail object
  that starts at the session offset `o` is `N ‖ Seal(K_tail, N, P_j, FMT ‖ u8(mark) ‖ u64(o) ‖ u64(j))`, with the mark `0x02` and `0x03` on its
  last segment, and no final segment. Every tail segment but the last holds 65,536 bytes, a tail holds at least one byte (a request whose
  remainder is empty writes none), so a tail of `m` bytes is stored as `m + 28·⌈m / 65,536⌉`; Quader's writers derive a tail segment's nonce
  as the first 12 bytes of `HMAC-SHA-256(seed, FMT ‖ " nonce" ‖ u8(mark) ‖ u64(o) ‖ u64(j))`
  ([Q-STOR-321](#q-stor-321--a-staging-tails-nonces-take-its-session-offset-and-a-tail-holds-at-least-one-byte)). The row keeps the tail's
  plaintext size and SHA-256 as today; reading a tail back authenticates every segment and then compares size and SHA-256.
- **Counting.** The claim transaction adds to `upload_sessions.sealed_segments` what a request may seal before a record counts it, one part
  and two segments (`⌈part_size / 65,536⌉ + 2`), and the record of each part and each tail object adds the segments it sealed; a request
  that would pass `2^31` answers `409` with the code `session_exhausted` and the session is aborted (Q-STOR-318,
  [Q-STOR-380](#q-stor-380--a-claim-reserves-one-part-and-the-records-count-what-was-sealed)). A client that sends a blob once never comes
  near it.
- **Finalize**, on the session's connection:
  1. The remaining plaintext (the tails, decrypted) becomes the last data segments, followed by the final segment with `L` and the digest of
     the session's hash state; without a tail the final segment alone is the last part (68 bytes, which S3 allows as the last part), sent
     under the finalize's short claim and not recorded ([Q-STOR-381](#q-stor-381--a-final-segment-sent-alone-is-not-recorded-as-a-part)).
     `CompleteMultipartUpload`, then `HeadObject` of the staging object: `S(L)`.
  2. Where the connection's capabilities say `copyReplacesMetadata`: the copy to the content name carries the record, a wrap under the active
     storage key bound to the digest (`CopyObject` with `REPLACE` up to 5 GiB; above, the multipart copy whose `CreateMultipartUpload` carries
     the record). `HeadObject` confirms size and record; the copy row is `present` and not verified, as stage 1 says (the scrub reads it first).
  3. Where it does not: the replica streams the staging object into a multipart upload of the content name created with the record, whose parts
     are the staging parts as stored (their `stored_sha256` as payload checksums) and the last part; it authenticates every segment and hashes
     the plaintext on the way, and the copy is verified. A mismatch fails the finalize with `503` and leaves the session `assembled`.
  4. A blob smaller than one part (tails only) is written from its decrypted tails as one request (R-STOR-080), not copied.
  With `min_copies` above 1, each other connection gets the blob as stage 1 says, an encrypted one through its own write with a new data key
  (R-STOR-087).
- **Expiry and abort** as R-STOR-011 and R-STOR-054; the sealed data key goes with the row.

Acceptance criteria (unit, `internal/uploads`, two in-memory stores, parts of 64 KiB):

- A session of three requests (64 KiB; 64 KiB and 10 KiB; 20 KiB) on an encrypted connection finalizes with the right digest; the content
  object carries the record and reads back; no staging object or tail contains a run of 16 bytes of the input.
- On a connection whose capabilities say `copyReplacesMetadata: false`, the finalize streams and the copy row is verified.
- A request whose session's counter would pass the bound (the counter set near it) answers `409 session_exhausted`.

### R-STOR-082 · Reading from an encrypted connection
Priority: **P1** · Covers: STOR-022

Every read of a copy on an encrypted connection goes through the connection's reader of R-STOR-062 (the identity on a plain connection),
which decrypts and authenticates; no caller of `blob.Store.Open` sees ciphertext, and none needs a key.

**Built** on 2026-11-22 (package 3): [sealedread.go](../../internal/blob/sealedread.go) and `Store.Open`
([download.go](../../internal/blob/download.go)), with the builder's decisions
[Q-STOR-383](#q-stor-383--a-plain-connection-refuses-an-object-that-carries-a-record) (a plain connection refuses an object of the stored
format) and [Q-STOR-385](#q-stor-385--an-encrypted-read-holds-back-its-last-segment) (the hold-back of an encrypted read).

- **A whole read** is one `GetObject`, as today: the record comes with the response's headers, and steps 1 to 6 of R-STOR-077 run before the
  first byte reaches the caller, so a failure falls over to the next copy (R-STOR-055). Plaintext is released segment by segment, each after
  its tag; the plaintext of the last data segment is held until the final segment has opened with the copy's `L` and digest and the
  SHA-256 of the plaintext equals the digest (R-STOR-013, Q-STOR-385). A mismatch aborts the response and marks the copy `corrupt`.
- **`storage.download.verify: false`** skips only the SHA-256 of the plaintext; decryption authenticates every segment and the final segment
  whatever the setting says, and the final segment's digest is still compared.
- **A ranged read** ([Q-STOR-317](#q-stor-317--a-ranged-read-is-one-request-for-the-segments-of-its-window)) of the plaintext bytes
  `[a, b]` reads the data segments `⌊a / 65,536⌋` to `⌊b / 65,536⌋` with one ranged `GetObject` of their stored bytes, and the final segment
  with them when the window reaches the last data segment; the response's total must equal `stored_size`, steps 1 to 6 run, every segment is
  authenticated, and only the window's bytes leave. It reads at most two segments more than the window. A ranged read on an encrypted connection
  therefore never hands out a byte that was not authenticated, where a ranged read on a plain connection serves what the bucket holds (H-11);
  damage outside the window is not seen ([H-1300](../security/stored-content-format.md#h-1300--a-ranged-read-does-not-see-damage-outside-its-window)).
- **`HEAD` and conditional requests** of an artifact are answered from the item row as today (R-STOR-012), with no request to the store.
  `HeadObject` against the store (after a write, the recheck of a missing copy, the operator check, a drain's confirmation) returns the stored
  size and the record; the size is compared with `stored_size`, the record where R-STOR-087 says.
- **Readers inside Quader** reach the same path through `blob.Store.Open`: the ranges of NuGet's reader
  ([wire_nuget_blobs.go](../../internal/app/wire_nuget_blobs.go)), of the format discovery ([discover.go](../../internal/formathost/discover.go))
  and of item content ([content.go](../../internal/items/content.go)), the signature job's ranges
  ([blobs.go](../../internal/signatures/job/blobs.go)), and the whole reads of the proxy's documents and of both scanners, which read through
  Quader's scan listener ([scanread.go](../../internal/malware/scanread/scanread.go), [scanlisten.go](../../internal/api/scanlisten/scanlisten.go)).
- **Every size check** compares the copy's `stored_size`, as stage 1 made them (R-STOR-062).
- **What a failure marks.** `record`, `size`, `commitment`, `segment`, `final` and `digest` are evidence about the object: the copy is marked
  `corrupt` and its repair queued (R-STOR-055); `format` too, because an object without the record on an encrypted connection is not Quader's
  writing of that blob. `unknown_key` marks nothing ([Q-STOR-311](#q-stor-311--a-record-that-names-an-unknown-storage-key-is-not-damage)): the
  read falls over and the alert of R-STOR-088 fires. `destroyed_key` marks the copy `corrupt` and queues its repair, because a destroyed key
  never comes back, and fires no alert (R-STOR-101). A transport error marks nothing, as today. Every failure counts
  `quader_storage_encrypted_read_failures_total{connection,reason}` with the step's name as `reason`.
- **Cost**: the same requests as today; AES-256-GCM per byte read; a ranged read of 512 bytes reads one segment of 65,564 bytes.

Acceptance criteria (unit, `internal/blob`, the in-memory store):

- A whole read of a 196,609-byte blob returns its bytes; a ranged read of the bytes 65,530 to 65,540 asks the store for the stored bytes of
  segments 0 and 1 only and returns 11 bytes; a ranged read of the last 10 bytes reads segments 2 and 3 and the final segment.
- Each mutation of R-STOR-078 is refused on a whole and on a ranged read with its reason and marks the copy `corrupt`; a record that names an
  identifier the replica does not hold falls over to the second copy, marks nothing and counts `unknown_key`.
- With `storage.download.verify: false`, a flipped bit in the second segment still aborts the read.

### R-STOR-083 · The storage keys and the name keys
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-22 (package 2 of [stored-content-encryption.md](../developer/stored-content-encryption.md#package-2-the-keys-as-built)):
[internal/blob/keys](../../internal/blob/keys/doc.go), the migration `00107_storage_keys`, `blob.Store.CopyDataKey` and its rule in
`internal/archtest`; the writer that wraps under the active key and the reader that looks a key up are package 3's, built the same day.

The storage keys lie once: a sealed row of the database per connection and generation (the owner's decision 15 (a)), sealed by the key ring
like every other secret of the database (decision 4 (a))
([Q-STOR-309](#q-stor-309--a-storage-key-is-a-row-with-an-identifier-of-its-own-drawn-at-the-connections-first-start-never-deleted),
[Q-STOR-310](#q-stor-310--a-replica-holds-the-storage-keys-in-memory-and-reads-the-rows-again-for-an-unknown-identifier)).

```sql
CREATE TABLE storage_keys (                 -- export class data: every backup holds it (R-STOR-047)
    key_id        text PRIMARY KEY CHECK (key_id ~ '^(sk-[a-z][a-z0-9-]{0,30}[a-z0-9]-[1-9][0-9]{0,5}|nk-[a-z][a-z0-9-]{0,30}[a-z0-9])-[0-9a-f]{8}$'),
    connection_id text NOT NULL,             -- no foreign key: storage_connections is transient in a backup, the keys are not
    kind          text NOT NULL CHECK (kind IN ('storage', 'name')),
    generation    integer NOT NULL CHECK (generation BETWEEN 1 AND 999999),   -- always 1 for a name key
    sealed_key    text NOT NULL CHECK (length(sealed_key) <= 512),  -- the 32 bytes sealed with the key ring: table storage_keys, column sealed_key, row (key_id)
    state         text NOT NULL CHECK (state IN ('active', 'retired')),     -- active: wraps new objects (storage) or names them (name)
    created_at    timestamptz NOT NULL DEFAULT now(),
    retired_at    timestamptz,
    UNIQUE (connection_id, kind, generation),
    -- the identifier names the kind, the connection and the generation (Q-STOR-324)
    CHECK (key_id = CASE kind WHEN 'storage' THEN 'sk-' || connection_id || '-' || generation || '-'
                              ELSE 'nk-' || connection_id || '-' END || right(key_id, 8)),
    CHECK (kind = 'storage' OR generation = 1),
    CHECK ((state = 'retired') = (retired_at IS NOT NULL))
);
CREATE UNIQUE INDEX storage_keys_active ON storage_keys (connection_id, kind) WHERE state = 'active';

ALTER TABLE storage_connections ADD COLUMN keyed_names boolean NOT NULL DEFAULT false;     -- R-STOR-075
ALTER TABLE upload_sessions ADD COLUMN sealed_data_key text CHECK (length(sealed_data_key) <= 512);  -- R-STOR-081; sealed, NULL on a plain connection
ALTER TABLE upload_sessions ADD COLUMN sealed_segments bigint NOT NULL DEFAULT 0 CHECK (sealed_segments >= 0);  -- R-STOR-081
ALTER TABLE upload_session_parts ADD COLUMN stored_size bigint CHECK (stored_size > 0),
                                 ADD COLUMN stored_sha256 bytea CHECK (stored_sha256 IS NULL OR octet_length(stored_sha256) = 32);  -- R-STOR-081
INSERT INTO cluster_epoch (area) VALUES ('storage_keys');                                    -- the change area below
```

- **The sealed columns** `storage_keys.sealed_key` (row key `key_id`) and `upload_sessions.sealed_data_key` (row key `id`) are registered with
  `internal/secrets` ([architecture.md](architecture.md) R-ARCH-021), so the per-key counts, the canary check, the compatibility check of the
  replicas' rings and the re-encrypt task cover them without a line of their own. A sealed key moved to another row does not open, and since
  the identifier names the connection, the kind and the generation, which the table holds the row to, a key cannot be given to another
  connection either ([Q-STOR-324](#q-stor-324--the-identifier-of-a-key-names-its-kind-connection-and-generation-and-the-table-holds-the-row-to-it)).
- **Identifiers.** A storage key is `sk-<connection id>-<generation>-<8 random hex>`, a name key `nk-<connection id>-<8 random hex>`: the random
  part makes a key drawn again for the same connection and generation (after a database was lost) a different identifier, so it never opens an
  object of its predecessor by mistake.
- **When keys are drawn.** When a replica first records an encrypted connection without key rows (R-STOR-051's row on first sight, or a row
  without a copy that turns encrypted), it draws the storage key of generation 1 and, with `keyed_names`, the name key, 32 bytes each from the
  random source, and inserts them `active` in the same transaction (a replica that loses the race to insert reads the winner's). It refuses to
  draw when the connection's location already holds an object below `sha256/` or `keyed/` (one listing of each with one key): such objects were
  written under keys of an earlier database, and the start stops as a configuration error: "connection archive holds objects but the database
  has no storage keys for it; import the exported storage keys (quader admin storage keys import) or empty the location (storage.md R-STOR-083)".
  The rows are read again after the listing, in the transaction that would insert, so an object another replica wrote under the keys it drew
  meanwhile is no reason to refuse; the refusal holds for a missing name key as well ("… has no name key for it …"). A replica holds the keys
  of every encrypted connection before it turns ready, and a draining connection draws nothing ([Q-STOR-326](#q-stor-326--a-replica-holds-the-keys-of-every-encrypted-connection-before-it-turns-ready-and-a-draining-one-draws-nothing)).
- **A further generation** only on command: `quader admin storage keys new-generation --connection <id>` opens generation `n` with the
  command's key ring (so the new key is sealed under a ring the installation uses, [Q-STOR-328](#q-stor-328--a-new-generation-needs-a-key-ring-that-opens-the-generation-it-retires)), inserts generation `n + 1` `active`
  and sets generation `n` `retired` in one transaction, writes the audit event `system.storage.key_generation_added` (actor `system:admin`,
  with the identifiers) and prints the reminder to export (R-STOR-085). New objects are wrapped under it from the moment a replica learns it;
  older generations keep opening what they wrapped. The name key has no generations.
- **The row is never deleted; its key is destroyed on command.** Quader deletes no row of `storage_keys`: not when a connection drains,
  retires or is forgotten (R-STOR-051), and not when its keys are destroyed. Nothing destroys a key by itself, because the noncurrent
  versions of its bucket and the pool of a retained backup can hold objects wrapped by it (backup-and-restore.md R-BAK-031, R-BAK-032). The
  operator destroys every key of a retired connection with `quader admin storage keys destroy` once no holder of R-STOR-098 and R-STOR-100
  refuses (R-STOR-097 to R-STOR-099): the row stays with the state `destroyed` and without its sealed value, so its identifier answers
  "destroyed" and never "unknown", and the connection id is never used again (R-STOR-102). A row is a few hundred bytes; an installation has a
  handful.
- **How a replica holds them.** After the start check of an encrypted connection, a replica reads the connection's rows, opens them with its
  key ring and keeps the keys in memory for its life, by identifier, or until they are destroyed (R-STOR-101); nothing else holds them, and their `String` and `LogValue` render
  identifiers only. The change area `storage_keys` ([architecture.md](architecture.md) R-ARCH-018) brings a new generation within its bound; a
  record that names an identifier the replica does not hold makes it read the connection's rows again, at most once per 10 seconds per connection.
  The pods that hold them are those that hold the key ring and build the blob area: `api`, `packages`, `worker` (and `web`, `all` in the
  development roles) and the restore Job. The backup pods, the scanners and the migration Job hold none.
- **A data key** is drawn by a writer for one object and held for the request, opened by a reader for one read; neither caches it. The writer
  overwrites its copy of the key when it is done; Go's AES implementation keeps an expanded key schedule that no code can erase, so the memory
  of a running pod holds the keys of its current transfers and its storage keys
  ([H-1309](../security/stored-content-keys.md#h-1309--a-replicas-memory-holds-the-storage-keys-for-its-life-and-go-cannot-erase-a-key)).
- **The room for the handover** of decision 12 (b) ([Q-STOR-320](#q-stor-320--the-handovers-room-is-one-function-of-the-store-that-nothing-calls-in-this-stage)):
  `blob.Store.CopyDataKey(ctx, digest, size, copy) (blobcrypt.DataKey, error)` opens the record of that copy (one `HeadObject`) and returns
  the copy's data key with its plaintext size and commitment, after steps 1 to 6 of R-STOR-077 against the copy row and the blob's digest and
  size its caller read; it reads no row (*amended on 2026-11-23*,
  [Q-STOR-470](#q-stor-470--copydatakey-opens-the-copy-the-requests-lookup-read-and-reads-no-row): it read the blob row and the copies again
  in a transaction of its own and took the connection's identifier). Its one caller is the handover of package 4
  (`internal/downloadlinks`, R-STOR-095 step 8); an architecture test (`internal/archtest`, `CheckDataKeyCallers`) allows calls from
  `internal/blob` and from the handover's package only, which is `internal/downloadlinks` (R-STOR-096, Q-STOR-340, [Q-STOR-327](#q-stor-327--the-rule-of-copydatakey-reads-the-syntax-of-every-package-and-reserves-the-methods-name)). `DataKey`
  renders nothing of the key when logged.

Acceptance criteria:

- The first replica that starts with an encrypted connection inserts one storage key and one name key, both sealed; a second replica started
  at the same moment reads the same rows; a replica whose connection's location holds an object but whose database has no key for it exits
  with the message above (unit, `internal/blob/keys`, the fake database and the in-memory store).
- After `quader admin storage keys new-generation --connection archive`, a new upload carries the new identifier, an object of generation 1
  still reads, and a second replica that has not yet seen the change reads the new object after reading the rows again (unit).
- The registry test of R-ARCH-021 lists both new sealed columns; a migration test lists no foreign key on `storage_keys`.
- An architecture test fails for a call of `CopyDataKey` from any other package.

### R-STOR-084 · A rotation of the root
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-22: the walk over both sealed columns (package 2) and the case with objects
([rotation_test.go](../../test/integration/blob/rotation_test.go), package 3).

The rotation of the owner's instance key is the existing rotation of the key ring (decision 4 (a); [architecture.md](architecture.md)
R-ARCH-021, [ha-and-helm.md](ha-and-helm.md) R-HA-027). The root seals only the storage keys and name keys, so a rotation re-seals a few
rows and touches no object.

1. The new key file joins the ring, `active_key_id` unchanged; rollout. Every replica can open values sealed with it.
2. `active_key_id` names the new key; rollout. New storage keys (a new connection, a new generation) are sealed with it.
3. `POST /api/v1/system/tasks/secrets-reencrypt/run` reseals every registered value under the active key: the upstream credentials, the
   signing keys, `storage_keys.sealed_key` and the open sessions' `upload_sessions.sealed_data_key`. The keys inside do not change, so no
   replica reloads anything and no object is read or written. The task's existing guards hold for the new rows: it answers `409
   key_ring_incompatible` while a live replica's ring lacks a key that seals one of them.
4. When `quader admin secrets status` shows no value under the old key, its file is removed; rollout. The start-up canary refuses a replica
   whose ring cannot open a key that a sealed row still names, the storage keys' rows included.
5. The operator exports the storage keys again (R-STOR-085): an export made before step 3 opens only with the old key, which escrow then keeps
   ([H-1306](../security/stored-content-keys.md#h-1306--an-export-opens-only-with-the-root-key-that-sealed-it)). Old backups keep needing the
   root keys their manifests list, as today (backup-and-restore.md R-BAK-028); their storage keys are sealed under those.

Noncurrent versions in a bucket and pool objects of a backup are wrapped under storage keys, not under the root, so a rotation leaves them
readable.

Acceptance criteria (integration, `test/integration/blob`, PostgreSQL of the stack with an in-memory store, two key files):

- With an encrypted connection holding three blobs, steps 1 to 4 run through the code paths of the task: after step 3 every row of
  `storage_keys` is sealed under the new key, the entity tags of the three objects are unchanged, the blobs download, and a replica started with
  only the new key reads them (`TestR_STOR_084_…`).

### R-STOR-085 · Export, import, loss and leak of the keys
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-22 (package 2): the export and the import in [internal/blob/keys](../../internal/blob/keys/export.go), the commands in
[admin_storage_keys.go](../../cmd/quader/admin_storage_keys.go).

- **Export** ([Q-STOR-312](#q-stor-312--the-export-is-one-document-of-sealed-rows-and-the-import-adds-missing-rows)):
  `quader admin storage keys export [--connection <id>]` prints to standard output one JSON document of the rows of `storage_keys`, sealed as
  they lie; it opens nothing and needs no key ring:

  ```json
  {"format": "quader-storage-keys-v1", "installationId": "0b7c3f5e-…", "exportedAt": "2026-12-01T09:00:00Z",
   "rootKeyIds": ["2026-12"],
   "keys": [{"keyId": "sk-archive-1-3f9a0c2e", "connection": "archive", "kind": "storage", "generation": 1, "state": "active",
             "createdAt": "2026-11-30T08:00:00Z", "sealed": "v1:2026-12:…"},
            {"keyId": "nk-archive-71be04d9", "connection": "archive", "kind": "name", "generation": 1, "state": "active",
             "createdAt": "2026-11-30T08:00:00Z", "sealed": "v1:2026-12:…"}]}
  ```

  *(example values)*. `rootKeyIds` names the root keys the values are sealed with; a destroyed key is listed without `sealed` (R-STOR-103).
  The document is kept beside the key ring's escrow copy:
  with the ring it opens every storage key, without it nothing (R-STOR-090). It runs where `admin storage verify` runs (the database role
  `web`); `system.storage.keys_exported` is audited in the transaction that reads the rows, before the document leaves, and an export of a
  connection that has no rows exits `2` ([Q-STOR-329](#q-stor-329--the-export-is-audited-in-the-transaction-that-reads-it-before-the-document-leaves)).
- **Import**: `quader admin storage keys import <file>` opens every value with the configured ring (so a wrong ring is refused before
  anything is written), inserts the rows that are missing, keeps the rows that are equal, and refuses a row whose identifier exists with another
  value or a file of another installation (`--other-installation` accepts one, for a recovery into a new installation). Of a connection's
  storage keys, the database's and the file's, the highest generation is active afterwards ([Q-STOR-325](#q-stor-325--an-import-inserts-what-is-missing-as-retired-and-makes-the-highest-generation-of-each-connection-active)); a row whose connection, kind
  and generation the database holds under another identifier refuses the import as well. Every refusal exits `2`, writes nothing and names
  identifiers and root keys only. It is the way back
  after a database restored to a point before a connection or a generation existed, and the input of the offline tool (R-STOR-089).
  `system.storage.keys_imported` is audited.
- **What the loss of each key costs:**

  | Lost | Cost | Answer |
  |---|---|---|
  | The key ring (every root key) | Every sealed value, so every storage key, so every blob of every encrypted connection, in its bucket and in every backup; plain connections lose nothing of their blobs | none; the operator's escrow of the ring (R-BAK-028) is the only protection ([H-1305](../security/stored-content-keys.md#h-1305--losing-the-key-ring-loses-every-encrypted-blob)) |
  | A row of `storage_keys` (a database lost or restored too far back without the export) | The objects wrapped by that key on its connection and in backups' pools; where another connection holds the blob, nothing: the reconciliation repairs from it after the connection is replaced | the export's import; else a new connection filled from the others |
  | A name key | Nothing is unreadable (reads take the name from the copy row, the record carries the digest); new writes to the connection cannot be named | the export's import; else a new connection |
  | A record (one object's metadata) | That object: it is `corrupt` | the reconciliation from another connection, a new upload, a refetch (R-STOR-027) |
  | A retired connection's keys, destroyed on command (R-STOR-099) | What its provider still holds of it, on purpose: that is what the command is for; nothing Quader still serves, because the command refuses while a copy, a session or a retained backup's pool needs a key (R-STOR-098, R-STOR-100) | none inside Quader; an export made before the destruction still opens the objects with the key ring, through the offline tool (R-STOR-089, [H-1370](../security/stored-content-keys.md#h-1370--an-export-made-before-the-destruction-opens-what-the-provider-keeps)) |
  | The Bastion's cache key | Its cache, which refills | a new key (R-BAS-061) |

- **What the leak of each key needs:**

  | Leaked | What its holder can do | Answer |
  |---|---|---|
  | The key ring alone | Nothing with the blobs: storage keys lie in the database | the rotation (R-STOR-084) |
  | The key ring with the database (or a backup) | Open every storage key; with read access to a bucket, every blob there | the rotation, and for each encrypted connection a new one that the reconciliation fills with new data keys, then the old drained |
  | A storage key | With read access to its bucket, every object it wrapped; with write access, objects that authenticate ([H-1301](../security/stored-content-format.md#h-1301--a-holder-of-a-storage-key-can-write-objects-that-authenticate)) | a new connection (also the same bucket under another prefix) that the reconciliation fills with new data keys, then the old one drained; a new generation alone protects nothing already written ([H-1307](../security/stored-content-keys.md#h-1307--a-leaked-storage-key-is-answered-by-a-refill-that-takes-as-long-as-the-data)) |
  | A name key | Confirm, by listing the bucket, that a known file is stored | a new connection |
  | A destroyed key, from a copy made before the destruction (an export, a Quader backup, PostgreSQL's own copies), with the key ring | With read access to the retired connection's bucket, what its provider still holds | destroy the exports, let the backups and the point-in-time archive age out, empty the bucket ([H-1370](../security/stored-content-keys.md#h-1370--an-export-made-before-the-destruction-opens-what-the-provider-keeps) to [H-1372](../security/stored-content-keys.md#h-1372--postgresql-keeps-the-sealed-value-after-the-destruction)) |
  | One data key | That one object's content, with read access to the bucket | none short of a new connection; live for every blob handed to a Bastion while its registration's handover is on (R-STOR-096, [bastion.md](bastion.md) R-BAS-070; [H-1311](../security/stored-content-keys.md#h-1311--a-leaked-data-key-of-one-blob-has-no-answer-short-of-a-new-connection)) |

Acceptance criteria (unit, `cmd/quader` with the fake database):

- `export` prints the rows sealed and `import` of that document into an empty table restores them; an import with a ring that lacks the
  sealing key exits `2` naming it; an import of a row whose identifier exists with another value exits `2`; an import of a file of another
  installation exits `2` unless `--other-installation`.

### R-STOR-086 · Keyed object names
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-22: the name in `internal/blob/blobcrypt` (package 1), the naming function `Connection.Name` and the writes under it
(package 3, [connections.go](../../internal/blob/connections.go)).

The owner's decision 5 (a) with his note: on an encrypted connection an object's name is a keyed MAC of the digest, a detail that is on by
default and fixed once the connection holds data
([Q-STOR-313](#q-stor-313--keyed-names-lie-below-keyed-and-are-an-hmac-of-the-digest-under-a-name-key-of-the-connection)).

- **The name**: `m = HMAC-SHA-256(name key, FMT ‖ " name" ‖ digest)` over the 32-byte digest, and the object lies at
  `<prefix>/keyed/<m0m1>/<m2m3>/<64 hex of m>`, in the two-level fan-out of R-STOR-001. With the name key `40 41 … 5f`, the blob `a` of
  R-STOR-076 lies at `quader/keyed/99/42/9942d541941e3141ba35550ee20b7bbf8c60fb3acc8123325c1b293b0e2922a6` (computed as in R-STOR-076).
- **Who needs the mapping.** Only a write asks the connection's naming function; every other reader takes the name from the copy row's
  `object_key` (R-STOR-062): a download, the scrub, the reconciliation's source, collection and a drain. The orphan sweep, which meets
  objects without a row, reads the record (R-STOR-087); the offline tool computes the name from the digest in the record and compares
  (R-STOR-089).
- **What a lister of the bucket still learns**: the number of objects, their stored sizes, which give away the exact plaintext sizes
  ([H-1312](../security/stored-content-visibility.md#h-1312--the-stored-size-gives-away-the-exact-plaintext-size)), their times of writing,
  and their entity tags (an MD5 of the ciphertext, or the multipart form); not which public file an object is, as long as the name key does not
  leak. Without keyed names (`keyed_names: false`) a lister confirms every known file by its digest
  ([H-1315](../security/stored-content-visibility.md#h-1315--an-encrypted-connection-without-keyed-names-lets-a-lister-confirm-stored-files)).
- `keyed/` joins the key families of R-STOR-001 and R-STOR-059 on an encrypted connection with keyed names, in place of `sha256/`.

Acceptance criteria:

- The known answer above reproduces; the names of two connections with two name keys differ for one digest; an object listed below `keyed/`
  is found by `(connection_id, object_key)` (unit, `internal/blob/blobcrypt` and `internal/blob`).

### R-STOR-087 · The reconciliation, the scrub, the orphan sweep and collection on encrypted connections
Priority: **P1** · Covers: STOR-015, STOR-022

What stage 1's jobs do when a connection is encrypted (decision 7: switching encryption on or off is a new connection that the
reconciliation fills; [Q-STOR-308](#q-stor-308--a-copy-into-an-encrypted-connection-always-gets-a-new-data-key)). **Built** on 2026-11-23
(package 4): the reconciliation's copier ([copy.go](../../internal/blob/reconcile/copy.go)), the scrub's reader and repair
([read.go](../../internal/blob/scrub/read.go), [repair.go](../../internal/blob/scrub/repair.go)), the orphan sweep's reading of the record
([sweep_sealed.go](../../internal/blob/sweep_sealed.go)), the operator check's record ([verify.go](../../internal/blob/verify/verify.go)) and
the stream into an encrypted connection ([sealedstream.go](../../internal/blob/sealedstream.go)), with the decisions
[Q-STOR-440](#q-stor-440--a-stream-into-an-encrypted-connection-holds-one-part-in-memory-not-the-spool) to
[Q-STOR-442](#q-stor-442--the-operator-check-opens-a-records-steps-1-to-6-and-reports-unknown_key-as-a-finding-record).

- **The reconciliation's copies** (R-STOR-056), from a present copy on connection A to connection B:

  | A | B | What the worker does | The copy on B |
  |---|---|---|---|
  | plain | plain | as stage 1: a server-side copy within one endpoint, a stream across | as stage 1 |
  | plain | encrypted | reads A whole through its reader into B's writer, which hashes the plaintext as it passes (a mismatch completes nothing, marks A's copy `corrupt` and tries the next source) and writes it as R-STOR-080 says, with a new data key, one part in memory (Q-STOR-440) | `present`, verified where B enforces payload checksums |
  | encrypted | plain | reads A through its reader (decrypting, authenticating, hashing) and streams the plaintext to B as stage 1 streams | as stage 1's stream |
  | encrypted | encrypted | as plain to encrypted: decrypted from A, a new data key and new bytes on B, never a copy of A's ciphertext | `present`, verified where B enforces payload checksums |

  A server-side copy is made only between two plain connections (R-STOR-062), so `storage.reconcile.server_side` concerns them alone. A
  stream into an encrypted connection holds one part (`storage.object_store.multipart.part_size`) in memory, as a streamed copy between plain
  connections does (Q-STOR-255, Q-STOR-440). A source read through its connection's reader is bound to the blob's digest and size; a refusal
  of a reason that marks is evidence against the source, `unknown_key` is not (Q-STOR-311). Switching encryption on is therefore the owner's
  scenario: a new element with `encrypted: true` (the same bucket under another prefix will do), the reconciliation fills it, the old element
  gets `drain: true`; while it runs every blob lies twice. Switching it off, and answering a leaked storage key, are the same with the roles
  exchanged.
- **The scrub** (R-STOR-027) reads every copy whole through its reader: on an encrypted connection that is steps 1 to 7 of R-STOR-077,
  every segment, the final segment and the digest. Every named failure but `unknown_key` marks the copy `corrupt` and queues its repair,
  `destroyed_key` included (R-STOR-101); `unknown_key` leaves it due and counts it (`unknownKey` in the run's result, the result
  `unknown_key` of `quader_storage_scrub_copies_total`). The repair writes the copy anew from another connection's copy, with a new data key
  on an encrypted connection, under the same name (the name depends on the digest and the name key only), reads it back whole and records its
  stored size and storage key. The blob's size a read binds is the plaintext size the copy's stored size stands for (the inverse of `S`,
  R-STOR-076): with the digest bound as well it is the blob row's. On a plain connection a read refused as `format` (an object that carries a
  record, Q-STOR-383) marks the copy too.
- **A drain** (R-STOR-056) deletes the draining connection's objects as stage 1 says; its storage keys stay (R-STOR-083) until the operator
  destroys them after the connection is retired (R-STOR-099). A drain deletes only the objects that have copy rows, and `forget-connection`
  deletes none, so noncurrent versions and leftovers stay at the provider either way.
- **The orphan sweep** (R-STOR-026) on an encrypted connection with keyed names lists `<prefix>/keyed/<xx>/`; an object whose name has a copy
  row is checked as stage 1 says (a listed size other than `stored_size` marks it `corrupt`). An object without a copy row is placed by its
  record ([Q-STOR-314](#q-stor-314--the-orphan-sweep-reads-the-record-of-an-object-it-cannot-place-and-never-deletes-one-without-a-readable-record)):
  one `HeadObject`, steps 1 to 3 and 6 of R-STOR-077 and the check that the name is the keyed name of the unwrapped digest; then, by that
  digest, stage 1's rules (adopted `present` and not verified for a live blob, with its `storage_key_id`, or `corrupt` with its repair queued
  when the size the store answers is not `S(L)`; claimed through a `deleting` blob row and deleted when no blob row exists and it is older
  than `min_age`). An object whose record does not open, whose record names a destroyed key (`destroyed_key`, R-STOR-101), whose name is not
  the keyed name of its digest, or whose wrap binds the live blob's digest with another size, is reported as foreign and never deleted
  ([Q-STOR-441](#q-stor-441--the-orphan-sweep-places-every-object-without-a-copy-row-by-its-record-also-below-sha256)). On an encrypted
  connection without keyed names the sweep lists `sha256/` as stage 1 says, and every object without a copy row, an orphan as much as one
  to adopt, is placed by its record and its name's digest the same way (Q-STOR-441).
- **Collection** (R-STOR-057) deletes by the copy row's `object_key`; nothing changes.
- **The operator check** (`quader admin storage verify`, R-STOR-058) also opens the record of each copy on an encrypted connection (one unwrap
  from the `HeadObject` it sends anyway, no body read): a record that fails steps 1 to 6 against the copy row and the blob row is a finding
  `record` of that copy with the step's reason, and a blob none of whose copies is there is lost with the kind `record` when one of them had
  such a finding; the command loads the key ring only when a connection is encrypted
  ([Q-STOR-442](#q-stor-442--the-operator-check-opens-a-records-steps-1-to-6-and-reports-unknown_key-as-a-finding-record)).
- **`restore-versions`** (R-HA-025, per connection) makes an older version current again; its record is its own, under a key of the
  connection, which is not destroyed while the connection is configured or holds anything (R-STOR-098), so it reads (R-STOR-078's last row),
  unless its key was destroyed; `restore-versions` never runs on a retired connection, since no replica configures it.

Acceptance criteria (integration, `test/integration/blob`, PostgreSQL of the stack with two in-memory stores):

- A plain connection with ten blobs and a new encrypted connection: one reconciliation run copies all ten, each object on the encrypted store
  carries a record and a keyed name, each copy row is verified, and after `drain: true` on the plain one and a run every blob downloads from the
  encrypted store.
- Between two encrypted connections the copy's body differs from the source's and both read back.
- A scrub run over an encrypted copy whose second segment was changed behind Quader's back marks it `corrupt`, and the reconciliation's next run
  rewrites it from the other connection.
- The orphan sweep adopts an object below `keyed/` of a live blob without a copy row, reports one whose record names an unknown key, and deletes
  an object of a blob that has no row after `min_age` (fake clock).

### R-STOR-088 · What an encrypted connection changes elsewhere
Priority: **P1** · Covers: STOR-022

**Built**: the links and the handover with package 3 and the links' packages; the API with stage 1's package F and, for its acceptance case
and `state: destroyed` with `destroyedAt`, package 6 ([storage_connections_api.go](../../internal/api/v1/storage_connections_api.go); the
statement reads `destroyed_at`, the column of package 7's migration, and a destroyed key shows its time); the alert with
package 6 ([_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl), on 2026-11-23).

- **Signed links** (R-STOR-014, R-STOR-070 step 12, R-STOR-074): an encrypted connection never serves a link, because a link would hand out
  ciphertext; a plain connection that holds a sound copy of the same blob still may
  ([Q-STOR-291](#q-stor-291--any-connection-that-is-not-encrypted-and-holds-a-sound-copy-serves-the-link)), which is why an installation that
  keeps a plain connection beside an encrypted one keeps its content in the clear at the plain provider
  ([H-1317](../security/stored-content-visibility.md#h-1317--a-plain-connection-beside-an-encrypted-one-keeps-the-content-in-the-clear)).
  A handover to a Bastion may name an encrypted connection, with the copy's data key, where no plain copy can serve (R-STOR-095).
- **The object store's own encryption** (R-STOR-032) stays possible beneath, per connection (`sse_mode`); it protects the provider's disks,
  not against the provider.
- **Assumption A6** (R-STOR-035) changes wording: concurrent writes to one key carry the same content; on a plain connection identical bytes, on
  an encrypted one each a whole encryption of that content with its own record in the same request, so whichever completes later is the object
  and reads back (A2 makes the record and the body one object). The conformance check of identical bytes stays a check of the store.
- **The scanners** (malware and image) read through Quader's scan listener and never from a bucket, so they need no key and see plaintext only
  as today.
- **The revocation journal, the probe objects and the marker stay plain** on an encrypted connection
  ([Q-STOR-315](#q-stor-315--the-journal-the-probes-and-the-marker-stay-plain-on-an-encrypted-connection)): the journal is read by a restore
  that replaces the database, which holds the storage keys, so a key from that database is the wrong key for it, and it carries identifiers,
  no content; the probes are random bytes that live for a check; the marker must be read before any key is loaded. What a provider learns from
  them is on the visibility page
  ([H-1316](../security/stored-content-visibility.md#h-1316--the-journal-the-marker-and-the-probes-are-plain-on-an-encrypted-connection)).
- **The API** of the connections (R-STOR-061) shows `keyedNames` and, per connection, its storage keys by identifier, generation, state
  (`active`, `retired` or `destroyed`, with `destroyedAt` for a destroyed one, R-STOR-097), creation
  time and the number of copies that name each (from `blob_copies.storage_key_id`), never a key or a sealed value; no operation of the API
  creates or changes a key ([Q-API-405](api-and-ui.md#q-api-405--the-connection-shows-its-storage-keys-by-identifier-and-no-operation-changes-them)).
  Built ahead of this stage with package F of stage 1 (R-STOR-061): the members, the copies per key from the storage summary, and the
  tab's fold, which lists the keys of a connection that has any. The admin tab of R-UI-196 shows `encrypted` already; this stage adds
  nothing to a screen.
- **The storage summary** (R-STOR-028) counts a connection's `presentBytes` in stored bytes; the logical figures of repositories and projects
  and the quotas count plaintext sizes, as before (Q-STOR-155).
- **The alert** `QuaderStorageUnknownKey` fires, critical, when `quader_storage_encrypted_read_failures_total{reason="unknown_key"}` grows on a
  connection for 10 minutes: storage keys are missing and copies are unreadable until the export is imported (rendered and tested by
  [ha-and-helm.md](ha-and-helm.md) R-HA-045 like stage 1's alerts). The expression is
  `sum by (connection) (increase(quader_storage_encrypted_read_failures_total{reason="unknown_key"}[5m])) > 0` for 10 minutes: one such read
  does not fire it, reads that keep meeting unknown keys do. It does not count `reason="destroyed_key"`: the import would refuse
  a destroyed key, so its advice would be wrong, and such a copy is marked and repaired instead (R-STOR-101).
- **Throughput.** The targets of the transfer path are measured again with an encrypted connection once it is built; a measurement, not a test.

Acceptance criteria:

- With an encrypted first connection and a plain second, a download that meets every other condition of R-STOR-070 is linked to the second
  connection; with only the encrypted one it streams (unit, `internal/formathost`, the stage 3 decision against two in-memory stores).
- `GET /api/v1/system/storage-connections` of an encrypted connection lists `keyedNames: true` and its storage key's identifier and generation,
  and no field holds a sealed value (unit, `internal/api/v1`).
- The rule test of `QuaderStorageUnknownKey`, one firing and one silent (chart tests).

### R-STOR-089 · The offline tool
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 6): [internal/blob/offline](../../internal/blob/offline/doc.go), the command in
[admin_storage_offline.go](../../cmd/quader/admin_storage_offline.go), with the decisions
[Q-STOR-450](#q-stor-450--a-pull-writes-a-directory-of-its-own-the-objects-their-record-files-and-the-connections-facts) to
[Q-STOR-452](#q-stor-452--the-offline-tool-opens-every-key-of-the-export-before-it-reads-and-looks-a-record-up-across-the-export).

A command of Quader's binary that reads and verifies blobs of an encrypted connection without a database, without Valkey and without a
running Quader (the owner's decision 1 (a); [Q-STOR-316](#q-stor-316--the-offline-tool-is-a-command-of-the-binary-and-reads-a-bucket-or-a-directory-with-record-files)).

```text
quader admin storage offline list   --keys <export.json> --key-ring <dir> (--connection-file <element.yaml> | --dir <path>) [--json]
quader admin storage offline get    --keys <export.json> --key-ring <dir> (--connection-file … | --dir …) <sha256 or object name> --out <file> [--json]
quader admin storage offline verify --keys <export.json> --key-ring <dir> (--connection-file … | --dir …) [--concurrency N] [--json]
quader admin storage offline pull   --connection-file <element.yaml> --to <dir> [--json]
```

- **What it needs**: the files of the key ring that sealed the export (`--key-ring`, a directory of `*.key` files, the format of R-ARCH-021;
  only opening, no active key), the export of R-STOR-085 (`--keys`), and read access to the objects: a connection element of R-STOR-050 in a
  file (endpoint, bucket, prefix, credentials, CA) or a directory written by `pull`.
- **`list`** walks the objects below the prefix (`keyed/`, `sha256/`, and a backup's `pool/sha256/`), reads each record (one `HeadObject`),
  opens the wrap with the storage key it names and prints the object name, the stored size, the plaintext size and digest from the wrap, the
  key's identifier and whether the name is the keyed name of the digest (or the digest itself): `ok`, `name_mismatch`, `record`, `unknown_key`,
  and `destroyed_key` where the export lists the key as destroyed (R-STOR-103). An export made before the destruction still holds the sealed
  row and opens the object
  ([H-1370](../security/stored-content-keys.md#h-1370--an-export-made-before-the-destruction-opens-what-the-provider-keeps)).
- **`get`** decrypts one blob, found by its digest (the keyed name computed with the name key of the export) or by its object name, checks every
  segment and the SHA-256, and writes the plaintext; a failure writes nothing and exits `5`.
- **`verify`** reads every object whole as the scrub does and reports per object; exit `0` when all are sound, `5` with a finding.
- **`pull`** copies the objects of a connection into a directory together with each record as a file `<object name>.record.json`, because a
  copy of a bucket by a tool that does not keep metadata loses the records, and with them every data key
  ([H-1303](../security/stored-content-format.md#h-1303--a-copy-of-an-encrypted-bucket-that-drops-the-metadata-cannot-be-read)).
- It writes nothing to a bucket. It reads a plain connection too (no record: printed as `plain`, verified against the digest its name carries).
- **As built.** The element file is read through the configuration's own loader as the only element of `storage.connections`, so its rules
  and defaults are the binary's (a `keyed_names` without `encrypted`, an unknown key, an endpoint without TLS refused with their sentences);
  the shared client settings and the outbound settings take their defaults, and nothing of an installation's configuration, its database or
  Quader's own environment variables is read. The object store's client is built as the binary builds it: with `credentials_source: default` its
  credentials, and its region where the element names none, come from the AWS SDK's own chain (the `AWS_*` variables, the files under
  `~/.aws`, the metadata service); with `static` or `webIdentity` the element must name its region and that chain is not consulted; an
  element without a region that finds none is refused with the binary's sentence.
  Every sealed key of the export is opened before anything is read, and a ring that lacks a root key the export is sealed with is
  refused naming each such root key (Q-STOR-452). A line of `list` and `verify` is `<status> <object name> stored=<n> size=<n> sha256=<hex>
  key=<id>[: <detail>]`, then a line of totals; `--json` streams one document `{source, objects[], totals, error}`. The statuses are `ok`,
  `plain`, `name_mismatch` (also a name that is not of the layout), `unknown_key`, `destroyed_key`, the reasons of R-STOR-077 (`format`,
  `record`, `size`, `commitment`, and for `verify` and `get` `segment`, `final`, `digest`) and `unread` (the store did not answer). `list` and
  `verify` exit `0` when every object is sound, `5` with a finding, `75` when objects stayed unread and none was refused; `get` exits `5`
  for a refusal and for a blob in none of the places it looks (its keyed name, its plain name, the pool), and `2` for an `--out` that exists
  (Q-STOR-451); an export, ring or element that does not fit exits `2`. `pull` writes into an empty or new directory only (Q-STOR-450).
- **Destroyed keys.** An export's entry of R-STOR-103 (`state: destroyed`, `destroyedAt`, no `sealed`) is read; a record that names it is
  `destroyed_key`. Nothing else of the destruction is the tool's.

Acceptance criteria (unit, `cmd/quader`, an in-memory store and a temporary directory, kilobytes):

- With three blobs on an encrypted connection, the export and the ring files: `list` prints three `ok` lines with their digests; `get` of one
  digest writes its bytes; `pull` then `verify --dir` exits `0`; an object with a flipped bit makes `verify` exit `5` and `get` of it write
  nothing; a ring without the export's root key exits `2` naming it.

### R-STOR-090 · What the operator keeps, and the documentation
Priority: **P1** · Covers: STOR-022

- **What to keep.** For an encrypted installation the operator keeps, outside the cluster and away from the backup target: the key ring Secret
  with every retired key that a retained backup or a kept export still names (R-BAK-028, unchanged), and the latest export of the storage keys
  (R-STOR-085), made again after a connection is added with `encrypted: true`, after a new generation and after each rotation of the root. A
  bucket and the key ring alone no longer read a blob: the export (or any backup of the database) is the third part, and that is the price of
  the keys lying once (the owner's decision 15).
- **The README's** installation step that turns on the backup (step 6, "Keep a copy of the key ring …") gains, when the feature is built:
  "With an encrypted connection the key ring is also the key to your artifacts: without it every blob of every encrypted connection is lost, in
  the bucket and in every backup. Keep the export of the storage keys (`quader admin storage keys export`) beside your copy of the key ring, and
  export again after you add an encrypted connection, draw a new generation or rotate the key ring."
- **The manual** (administrator's backup page, section "Keep the keys", and the configuration page of the connections) gains the same sentences,
  the line `encrypted: true` with when to set it ("when the provider of a connection must not read the artifacts"), the cost (no signed links
  from an encrypted connection, the CPU of encryption, the scrub reading ciphertext), and the warning that a copy of an encrypted bucket made by a
  tool that drops object metadata cannot be read: `quader admin storage offline pull`, or a copy that keeps metadata
  ([H-1303](../security/stored-content-format.md#h-1303--a-copy-of-an-encrypted-bucket-that-drops-the-metadata-cannot-be-read)).
- **Leaving a provider** (R-STOR-097 to R-STOR-103): the README's backup step, the manual's section "Keep the keys" and its page of the
  connections gain the promise in the operator's words, quoted on the security page
  ([stored-content-keys.md](../security/stored-content-keys.md#destroying-a-retired-connections-keys)), and the steps in their order:
  1. set `drain: true` and wait for `drained`;
  2. remove the element (or `quader admin storage forget-connection` for a connection that is gone);
  3. wait for, or delete with `quader backup delete <id>`, the retained backups the dry run names;
  4. run `quader admin storage keys destroy --connection <id>` and read what it reports;
  5. run it again with the `--confirm` value it printed;
  6. destroy the exports of the storage keys made before;
  7. empty the bucket at the provider;
  8. export the storage keys again.
- These edits land with the build of the feature (the documentation rule of the repository: a page describes what runs), not before; the
  sentences about leaving a provider landed with package 7 on 2026-11-23, which builds the command. The others landed with package 3, which makes
  `encrypted: true` accepted; the offline tool they name, its section in the manual's backup page, the installation page's paragraph on an
  encrypted connection and the runbook's entry of `QuaderStorageUnknownKey` landed with package 6 (2026-11-23).

Acceptance criteria:

- The package that makes `encrypted: true` accepted changes the README and the two manual pages in the same change; the manual's build check
  passes.
- Package 7 adds the promise and the eight steps to the README and the two manual pages in the change that builds the command.

### R-STOR-097 · A destroyed key
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 7 of [stored-content-encryption.md](../developer/stored-content-encryption.md#package-7-destroying-a-retired-connections-keys-as-built):
the migration step [00112_storage_keys_destroyed](../../internal/meta/postgres/migrations/00112_storage_keys_destroyed.sql), the
destruction in [internal/blob/keys](../../internal/blob/keys/destroy.go)). The working paper promised that
whoever leaves a provider destroys its storage key, so that what stays there becomes unreadable;
[Q-STOR-309](#q-stor-309--a-storage-key-is-a-row-with-an-identifier-of-its-own-drawn-at-the-connections-first-start-never-deleted) (b)
builds it: the operator destroys every key of a retired connection on command (R-STOR-098 to R-STOR-100), and the row of each key stays as a
mark that the key is gone ([Q-STOR-402](#q-stor-402--a-destroyed-key-keeps-its-row-loses-its-sealed-value-and-names-its-time)).

The migration step of package 7, `00112` behind package 4's `00111` (the step names the CHECKs it adds `storage_keys_state_check`,
`storage_keys_retired_check`, `storage_keys_destroyed_sealed_check` and `storage_keys_destroyed_at_check`, and drops the two of 00107 they
replace):

```sql
ALTER TABLE storage_keys ALTER COLUMN sealed_key DROP NOT NULL,
                         ADD COLUMN destroyed_at timestamptz;
-- the CHECKs of the state, replacing (state = 'retired') = (retired_at IS NOT NULL):
--   state IN ('active', 'retired', 'destroyed')
--   (state = 'active') = (retired_at IS NULL)              -- a destroyed key is retired as well
--   (state = 'destroyed') = (sealed_key IS NULL)
--   (state = 'destroyed') = (destroyed_at IS NOT NULL)
CREATE TABLE storage_key_exports (                         -- export class data (R-STOR-103, Q-STOR-408)
    id          bigserial PRIMARY KEY,
    exported_at timestamptz NOT NULL DEFAULT now(),
    instance    text NOT NULL,                             -- the pod that ran the export
    key_ids     text[] NOT NULL                            -- the identifiers the document listed
);
```

- **The state `destroyed`.** The row keeps its identifier, connection, kind, generation and `created_at`; `sealed_key` is NULL, `destroyed_at`
  is the time of the destruction, `retired_at` the time the key was retired or, for the key that was active, the destruction's. Nothing can
  open the key again from the database, and no command brings it back (R-STOR-103).
- **Why the row stays.** An identifier met later (in a record at the provider, in a backup's blob list, in an export, in an import) answers
  "destroyed", not "unknown" (R-STOR-101). `storage_keys` is export class `data` and `storage_connections` is `transient`
  (backup-and-restore.md R-BAK-004), so the mark survives a restore and keeps the connection id retired (R-STOR-102).
- **NULL, not a marker.** A value that is not an envelope of the key ring would enter the sealed-column registry: the backup's
  `secrets.keyIds` would list it under the empty key identifier, and the walk of `secrets-reencrypt` would count it failed and fail the task.
  The walk's statement `StorageKeysSealedAfter` gains `AND sealed_key IS NOT NULL`; `CountSealed` already counts non-NULL values only
  ([sealed.go](../../internal/meta/postgres/listq/sealed.go)).
- **In PostgreSQL** an `UPDATE` leaves the old row version until vacuum reuses it, and the write-ahead log and any archive keep it for their
  retention, as a `DELETE` would ([Q-STOR-412](#q-stor-412--postgresqls-remnants-are-documented-not-vacuumed-by-the-command),
  [H-1372](../security/stored-content-keys.md#h-1372--postgresql-keeps-the-sealed-value-after-the-destruction)).

Acceptance criteria (integration, `test/integration/blob`, PostgreSQL of the stack, the database of the keys' case; the subtests `R-STOR-097 …`
of `TestR_STOR_083_TheStorageKeysInPostgreSQL`, written in [storage_keys_destroy_test.go](../../test/integration/blob/storage_keys_destroy_test.go)):

- A `destroyed` row with a sealed value, a `destroyed` row without `destroyed_at`, and an `active` row with NULL are each refused by a CHECK.
- With one destroyed row beside sealed ones, the walk of `secrets-reencrypt` skips it and ends with `failed: 0`, and `CountSealed` of
  `storage_keys.sealed_key` does not count it.

### R-STOR-098 · When a connection's keys may be destroyed
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 7; `keys.Destroy` and the holders' read `keys.PG.Holders`). Only a whole connection is destroyed, never one
generation: one run destroys every row of the connection
that is not destroyed yet, every generation of its storage key and its name key
([Q-STOR-400](#q-stor-400--only-a-whole-retired-connection-is-destroyed-every-generation-and-the-name-key)). A retired generation of a live
connection is never destroyed: no job wraps its objects again, and `restore-versions` would bring objects under it back into service.

**Holders that refuse** ([Q-STOR-401](#q-stor-401--a-connections-keys-may-be-destroyed-once-it-is-retired-or-unrecorded-and-nothing-holds-it)).
The destroying transaction locks the connection's key rows (both kinds) and its `storage_connections` row (`FOR UPDATE`, where the row
exists), then reads each of these:

| Holder | Needs the key because | How the command knows | Rule |
|---|---|---|---|
| The command's own configuration | It would load the keys | `storage.connections` of the command | The connection is in it: exit `2`, as `forget-connection` refuses |
| A live replica that configures the connection | It holds the keys in memory and reads or writes with them | the replica registry's object-store view (`StorageKeysLiveReplicas`, the count of the rows `StorageConnectionConfiguredLive` reads, heartbeats younger than `system.heartbeat_stale_after`), which reads no `storage_connections` row | refused (exit `5`) |
| The connection's state | Filling, active, draining, or drained and still configured: reads and writes are allowed. Detached: putting the element back must restore everything (Q-STOR-225) | `storage_connections.retired_at`; no row at all (after a restore of a backup, which records only the restoring configuration's connections, backup-and-restore.md R-BAK-030) counts as unrecorded | allowed only when `retired_at` is set or no row exists; otherwise refused, naming the state and the next step: `drained` (drained_at set, not retired yet), `configured` (a live replica has it, not drained: filling, active or draining) or `detached` (neither) |
| Copy rows | Their objects are read with these keys | `blob_copies` where `connection_id` is the connection (any state) or `storage_key_id` is one of its identifiers | refused while one exists (none for a retired connection: a safety net) |
| Queue rows | The connection is not settled | `blob_copy_queue` where `connection_id` is the connection | refused while one exists |
| Upload sessions | A finalize wraps under the active key (R-STOR-081) | `upload_sessions.connection_id` is the connection | refused; the report names `storage.uploads.session_max_age` |
| The pools of retained backups | A restore and `verify --deep --key-ring` open those objects with the key | the newest retention listing (R-STOR-100) | refused while a listed backup's `poolKeyIds` names one of the connection's keys, or while the listing cannot be trusted |

`retired_at` is set by the reconciliation for a drained connection that no live replica configures (`ReconcileRetire` in
[reconcile.sql](../../internal/meta/postgres/queries/reconcile.sql), which deletes its queue rows with it) and by `forget-connection`
(R-STOR-051).

**Holders that are reported and never refuse.** Each stays able to open what the provider keeps, together with the key ring:

| Holder | How it is known | Why it does not refuse |
|---|---|---|
| Exports made before the destruction | the table `storage_key_exports` (R-STOR-103); in detail, the audit trail within `audit.retention_days` | Quader cannot see escrow or delete files; the destruction is complete only where no export can bring the key back ([H-1370](../security/stored-content-keys.md#h-1370--an-export-made-before-the-destruction-opens-what-the-provider-keeps)) |
| Retained Quader backups whose database holds the keys sealed | `retained[].sealedKeyIds` of the listing (backup-and-restore.md R-BAK-032) | every backup since the connection existed holds them, and the newest backup is never deleted, so a refusal could never be met; the keys are gone from Quader's backups once these leave retention ([H-1371](../security/stored-content-keys.md#h-1371--a-quader-backup-taken-before-the-destruction-holds-the-keys-sealed-until-it-leaves-retention)) |
| PostgreSQL's own copies: old row versions, the write-ahead log, a point-in-time archive, standbys, volume snapshots | not knowable to Quader | documented ([H-1372](../security/stored-content-keys.md#h-1372--postgresql-keeps-the-sealed-value-after-the-destruction)) |
| Noncurrent versions and leftover objects in the connection's bucket | not knowable: the runtime credential cannot list versions (H-1328) | they are what the destruction makes unreadable |
| The revocation journal | — | it holds no key and no content (Q-STOR-315) |
| Data keys handed to a Bastion | the audit events `bastion.data_key.handed` within retention | a handed key was never wrapped again ([H-1374](../security/stored-content-keys.md#h-1374--a-data-key-handed-to-a-bastion-outlives-its-storage-key)) |
| Directories written by `offline pull` | not knowable | they need an export and the key ring |

Acceptance criteria (unit, `internal/blob/keys` with keystest; the exit code `5` is the command's mapping of the refusal, shown once in
`cmd/quader`):

- Each of these refuses with its sentence and writes nothing: the connection configured on a live replica; the connection
  draining; the connection detached; one copy row; one queue row; one upload session (`TestR_STOR_098_EachHolderRefusesAndNothingIsWritten`).
- A retired connection without holders, and an unrecorded connection whose keys exist, both pass; a second run reports "nothing to do" and
  exits `0` (`TestR_STOR_099_TheDestructionOfARetiredConnection`).

### R-STOR-099 · `quader admin storage keys destroy`
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 7; [admin_storage_keys_destroy.go](../../cmd/quader/admin_storage_keys_destroy.go) over `keys.Destroy`).

```text
quader admin storage keys destroy --connection <id> [--confirm <token>] [--dry-run] [--json] [--config <file>]
```

- **A dry run by default** ([Q-STOR-404](#q-stor-404--a-dry-run-by-default-and-a-confirmation-bound-to-the-installation-and-the-keys)).
  Without `--confirm` the command checks and reports, changes nothing and prints the confirmation. `--confirm <token>` destroys when the token
  matches and every check passes. `--dry-run` with `--confirm` checks the token and the holders and destroys nothing, a Job's preflight.
- **The confirmation** is `<connection>-<8 hex>`: the hex is the first 4 bytes of
  `SHA-256("quader-storage-keys-destroy-v1\n" ‖ installationId ‖ "\n" ‖ the connection's identifiers that are not destroyed, sorted, joined by "\n")`.
  Only the dry run shows it, so no keyboard slip produces it; one copied from another installation does not match, nor one taken before an
  import brought another generation. It does not expire, so a Kubernetes Job can carry it after a reviewed dry run; there is no prompt.
- **Where it runs**: the database role `web`, as `export` does; it needs no key ring, because it opens nothing
  ([Q-STOR-410](#q-stor-410--the-operator-runs-the-destruction-at-a-pod-without-the-key-ring-and-no-api-operation-does)). No operation of the
  API destroys a key (Q-API-405).
- **The transaction**, in this order:
  1. lock the rows (R-STOR-098);
  2. run every check of R-STOR-098 and R-STOR-100;
  3. compare the token;
  4. `UPDATE storage_keys SET state = 'destroyed', sealed_key = NULL, destroyed_at = now(), retired_at = COALESCE(retired_at, now())
     WHERE connection_id = $1 AND state <> 'destroyed'`;
  5. announce the change area `storage_keys` with the payload `{"connection": "archive", "destroyed": ["nk-archive-71be04d9", "sk-archive-1-3f9a0c2e", …]}`
     *(example values)*, the payload's version 2 (version 1 carries the connection alone);
  6. write the audit event `system.storage.keys_destroyed`: transactional, journal flag `revocation`, actor `system:admin`; details
     `connection`, `keyIds`, `exports {count, lastAt}`, `backupListing {runId, listedAt, target}` or null, `retainedHoldingSealed`; the
     journal entry `{connection, keyIds, destroyedAt}` (ha-and-helm.md R-HA-054,
     [Q-HA-416](ha-and-helm.md#q-ha-416--a-destruction-of-storage-keys-is-journaled-and-replayed-after-a-restore)); a restore replays it
     except for a connection the restoring configuration names, which it reports `skipped`
     ([Q-HA-417](ha-and-helm.md#q-ha-417--the-replay-destroys-the-keys-only-of-a-connection-the-restoring-configuration-does-not-name));
  7. commit;
  8. print.
- **Already destroyed**: when every row is `destroyed` the command exits `0`, prints "connection archive: its storage keys were destroyed on
  2027-04-02; nothing to do" and writes no event.

| Exit | When |
|---|---|
| `0` | destroyed; already destroyed; a dry run whose checks pass |
| `2` | usage; the connection has no rows; the connection is in the command's own configuration; the token does not match ("--confirm archive-1111aaaa does not match the keys of connection archive now; the dry run prints the current confirmation") |
| `5` | refused by a holder of R-STOR-098 or R-STOR-100, in a dry run as well, so that a Job tells "not yet" from "wrong call" ([Q-STOR-405](#q-stor-405--a-holders-refusal-exits-5)) |
| `75` | the database could not be reached |
| `1` | any other failure |

The text it prints *(example values)*:

```text
connection archive: retired on 2027-03-02 (drained 2027-03-01)
keys: sk-archive-1-3f9a0c2e (generation 1, retired), sk-archive-2-9b1d77e0 (generation 2, active), nk-archive-71be04d9 (name key)
refused by: none
still able to open what archive's provider keeps, with the key ring:
  exports made before: 4, the last on 2027-02-14T09:00:00Z; destroy those files
  retained backups whose database holds these keys sealed: 7 on s3://quader-backups/quader (listed by run 0193… at 2027-03-10T02:41:17Z); they leave retention not before 2027-04-01
  PostgreSQL: old row versions, the write-ahead log and any point-in-time archive until they turn over
  data keys handed to a Bastion: 3 (audit trail, last 365 days)
dry run: nothing was destroyed. To destroy, run: quader admin storage keys destroy --connection archive --confirm archive-5c1e09ab
```

After a destruction the last line reads "destroyed 3 keys of connection archive; what archive's bucket still holds stays there as
ciphertext; export the storage keys again: quader admin storage keys export". `--json` prints the same as `{connection, state, retiredAt,
keys[], refusals[{holder, count, say}], remaining{exports, backups, database, handedDataKeys}, confirm, dryRun, destroyed}`; it holds no
sealed value.

*As built:* `keys[]` are `{keyId, kind, generation, state}`, the storage keys by generation, then the name key; `destroyed` is the list of
the identifiers this run destroyed (empty in a dry run); the report adds `drainedAt` and, for a connection whose keys are all destroyed,
`destroyedAt` and `state: destroyed`. `remaining.exports` is `{count, lastAt}`, `remaining.backups` is `{recorded, count, target, runId,
listedAt, leaveNotBefore}` (`count` is null while a retained backup records no `sealedKeyIds`), `remaining.database` the sentence of the
text. A holder's refusal prints the report with a line `refused by:` and one line per holder (`  copies: 1 copy rows lie on connection
archive …`) and ends with "refused: nothing was destroyed; run the command again once the holders above are gone", without a
confirmation; `--dry-run` with `--confirm` ends with "dry run: the confirmation matches and every check passes; nothing was destroyed. To
destroy, run: …". Where a value is not known the text says so: "exports made before: none recorded", "retained backups whose database
holds these keys sealed: none, no backup recorded in this database" or "… not known on s3://… (the listing of run … records no
sealedKeyIds)", "data keys handed to a Bastion: 0 (audit trail, last 365 days)" (with `audit.retention_days`). The change is the payload
version 2 of R-STOR-101 (`keys.DestroyedChange`).

Acceptance criteria (unit, `cmd/quader` with keystest; `TestR_STOR_099_TheDestroyCommand`):

- Without `--confirm` the command prints the confirmation, changes nothing and exits `0`.
- A confirmation computed for another installation id, or before a generation was added, exits `2`.
- The right confirmation destroys every key of every generation and the name key in one transaction, together with one
  `system.storage.keys_destroyed`, one outbox row and one change of the area; `--json` holds no sealed value. At the unit tier the event's
  journal entry stands for the outbox row, which the audit writer inserts; the outbox row itself is shown in PostgreSQL by the subtest
  `R-STOR-099 a destruction in PostgreSQL …` of the keys' integration case.

### R-STOR-100 · What the backups hold decides as well
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 7, [backups.go](../../internal/blob/keys/backups.go); the record it reads is package 4's,
backup-and-restore.md R-BAK-032, built the same day: every backup since writes `storageKeys` into its manifest and every retention listing
`retained[].poolKeyIds` and `sealedKeyIds`, arrays, `[]` where empty; a retained backup older than that gives `null`, and the check refuses
for it, which fails closed). The keys' integration case reads hand-written runs; the backup's encrypted case
(`TestR_BAK_031_EncryptedConnectionsBackedUpAndRestored`) reads the runs a real backup records: refused while the first backup's pool names
the key, passing after the re-pool and `keep_last: 1`. The command reads no pool and holds no
credential of the backup's target; it judges the backups by the newest retention listing in `backup_run.result` and the key identifiers each
retained backup recorded ([Q-STOR-403](#q-stor-403--the-backups-are-judged-by-the-newest-retention-listing-and-the-key-identifiers-each-backup-recorded)).

1. **No backup known.** The command's configuration names no backup target (`backup.target.bucket` empty; the chart's `backup.enabled` is
   not a key of the configuration, and the chart requires the bucket with it) and `backup_run` holds no `create` or `delete` run: the check
   passes and the report says "no backup recorded in this database"
   ([Q-STOR-461](#q-stor-461--no-backup-known-is-a-configuration-without-a-backup-target-and-a-runs-target-is-compared-by-bucket-and-prefix)).
2. **Otherwise** all of these must hold, or the command refuses with exit `5` and names the reason:
   - no target-writing run is active (the row of `backup_run_one_active`);
   - the newest finished `create` or `delete` run carries `result.retention`; a later `create --no-prune` fails this, and the command says to
     run `quader backup prune` (a `prune` records a `delete` run with `result.retention`, as a `delete` does:
     [retention/runner.go](../../internal/backup/retention/runner.go));
   - that run's `target` equals the configured target, compared as `s3://<bucket>/<prefix>` (a create records the endpoint after it, a delete
     or a prune does not, Q-STOR-461); otherwise the command refuses and says that backups in the other target are not seen
     ([H-1373](../security/stored-content-keys.md#h-1373--the-check-of-the-backups-sees-only-the-newest-listing-of-the-configured-target));
   - every `retained[].poolKeyIds` is present (a retained backup whose manifest has no `storageKeys` gives null, which counts as unknown);
   - no `retained[].poolKeyIds` holds one of the connection's identifiers.

   A refusal for a retained backup names each such backup and when it leaves retention at the earliest (its `snapshotAt` plus `keep_within`,
   and how many newer backups `keep_last` still needs), and suggests `quader backup delete <id>`.
3. **No race with a run that starts after the check.** The connection has no copies, so a new run cannot pool anything under its keys; and it
   cannot inherit one either, because the previous backup is always retained and the listing just showed that it names none of them.

Acceptance criteria (unit, `internal/blob/keys` with keystest and `backup_run` rows; `TestR_STOR_100_TheBackupsDecideAsWell`, and in
PostgreSQL the refusal by a retained backup's pool in the keys' integration case):

- A listing whose retained entry names `sk-archive-1-…` refuses and names the backup.
- An active run, a newer `create --no-prune`, a null `poolKeyIds` and another target each refuse.
- Without a backup target and without a run, the check passes with the report's line.

### R-STOR-101 · Replicas forget destroyed keys, and reads answer `destroyed_key`
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 7: [holder.go](../../internal/blob/keys/holder.go), `ReasonDestroyedKey` in
[errors.go](../../internal/blob/blobcrypt/errors.go), the case of the scrub's whole read in
[scrub/read.go](../../internal/blob/scrub/read.go)).

- **At the change** ([Q-STOR-407](#q-stor-407--replicas-forget-destroyed-keys-at-the-change-and-a-load-skips-them)). A holder whose
  payload names destroyed identifiers destroys those keys at once (`Key.Destroy`) and drops them, with the connection's active key and name
  key; within one `cluster.epoch_poll_interval` (1 s by default), at once through Valkey. A replica that missed the change catches up at its
  next poll or through `FlushArea`. *As built* ([Q-STOR-462](#q-stor-462--a-destructions-change-makes-a-replica-forget-every-key-of-the-connection)):
  a payload of version 2 that carries `destroyed` makes the holder destroy every key it holds of the connection, whatever the list names, and
  the command cuts the list from its end to fit the change log's 1 KiB bound (an empty list included); a flush marks every connection, and the
  next `Lookup` or `Active` of a marked connection reads its rows before it hands out a held key.
- **Loading.** A load never opens a destroyed row and drops a held key whose row is now destroyed; it no longer refuses a row whose sealed
  value is NULL, and records the identifier in a set of destroyed identifiers per connection.
- **A read in flight** fails with `blobcrypt`'s error of a destroyed key (`errDestroyed` in
  [keys.go](../../internal/blob/blobcrypt/keys.go)), which has no reason and marks nothing; an AES key schedule already built
  may finish its transfer
  ([H-1309](../security/stored-content-keys.md#h-1309--a-replicas-memory-holds-the-storage-keys-for-its-life-and-go-cannot-erase-a-key)).
  In practice no live replica holds the keys when the command runs, since a replica that configures the connection refuses it
  (R-STOR-098); the forgetting is the defence for a stale or partitioned replica.
- **What a read answers** ([Q-STOR-406](#q-stor-406--a-read-that-meets-a-destroyed-key-answers-destroyed_key-and-marks-the-copy)). A record
  that names a destroyed identifier makes `Lookup` return the reason `destroyed_key` (a constant of
  [errors.go](../../internal/blob/blobcrypt/errors.go) whose `MarksCorrupt` is true): the read falls over to the next copy, the copy is
  marked `corrupt` and its repair queued, and `quader_storage_encrypted_read_failures_total{reason="destroyed_key"}` counts it.
  `QuaderStorageUnknownKey` does not fire (R-STOR-088). The scrub does the same (R-STOR-087), and the orphan sweep reports such an object as
  foreign and never deletes it (Q-STOR-314). *As built:* the lookup returns an error that wraps `blobcrypt.ErrDestroyedKey`, which
  `blobcrypt.Open` passes through and the store's reader counts and marks by its reason, with no change to the reader but a comment. The
  scrub judges a refusal by `Reason.MarksCorrupt` (package 4), so `destroyed_key` marks the copy and queues its repair without a case of its
  own; the orphan sweep's reading of a record (package 4, Q-STOR-441) treats any refusal, `destroyed_key` included, as a record that does not
  open: reported as foreign, never adopted, never deleted.
- The offline tool's `list` prints `destroyed_key` when the export lists the key as destroyed (R-STOR-089, R-STOR-103); built with package 6, it
  reads the entry package 7's export writes.

Acceptance criteria:

- After the change, `Held` lists none of the identifiers and the key objects refuse use; a load skips destroyed rows without a refusal (unit,
  `internal/blob/keys`; `TestR_STOR_101_ReplicasForgetDestroyedKeysAndReadsAnswerDestroyedKey`).
- A read of a copy whose record names a destroyed identifier falls over to the second copy, marks the first `corrupt` and counts
  `destroyed_key` (unit, `internal/blob`, the in-memory store; `TestR_STOR_101_AReadThatMeetsADestroyedKeyFallsOverAndMarksTheCopy`).
- The rule test of `QuaderStorageUnknownKey` stays silent on `destroyed_key` (chart tests, package 6).

### R-STOR-102 · A connection id with destroyed keys is retired for good
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 7: the start check in [record.go](../../internal/blob/record.go), `blob.RetiredForGood`, and the refusal in
`Holder.Ensure` and `NewGeneration`;
[Q-STOR-411](#q-stor-411--a-connection-id-with-destroyed-keys-is-retired-for-good)).

- A start whose `storage.connections` names an id, plain or encrypted, of which `storage_keys` holds a destroyed row is a configuration
  error: "storage.connections[1] (archive): the storage keys of connection archive were destroyed on 2027-04-02; the id is retired for good;
  choose another id (storage.md R-STOR-102)". `retired_at` alone would not do: a restore of a backup empties `storage_connections`
  (R-STOR-051).
- `Ensure`, and the restore Job's drawing of keys (backup-and-restore.md R-BAK-031), refuse the same way. *As built:* `Ensure` refuses
  before it lists the location and again in the drawing transaction, whether the connection drains or not, and a new generation of such a
  connection is refused too. The restore Job draws the keys of a new encrypted connection in a path of its own (`drawKeys`, Q-BAK-82); its
  check 3 refuses every connection of the restoring configuration, plain or encrypted, whose id the backup holds destroyed keys of, before
  anything is drawn or written (`TestR_STOR_102_TheRestoreRefusesAConnectionIDWithDestroyedKeys`). The element in front of the sentence is the
  replica's `storage.connections[<index>] (<id>):`, counted from 0.

Acceptance criteria (the start check runs against PostgreSQL only, like every record of the connections, so it is shown in the keys'
integration case, `R-STOR-102 a start whose configuration names the id is refused …`; `Ensure` at the unit tier,
`TestR_STOR_102_AnIDWithDestroyedKeysIsRetiredForGood`):

- The start check and `Ensure` both refuse with this sentence.

### R-STOR-103 · Export and import know destroyed keys
Priority: **P1** · Covers: STOR-022

**Built** on 2026-11-23 (package 7, [export.go](../../internal/blob/keys/export.go);
[Q-STOR-408](#q-stor-408--every-export-is-a-row-of-storage_key_exports-and-names-its-keys),
[Q-STOR-409](#q-stor-409--an-import-skips-a-destroyed-key-reports-it-and-inserts-the-destroyed-rows-the-database-lacks),
[Q-STOR-460](#q-stor-460--an-import-changes-nothing-of-a-connection-whose-keys-the-file-and-the-database-disagree-on-destroyed)).

- **The export** lists a destroyed key as `{"keyId", "connection", "kind", "generation", "state": "destroyed", "createdAt", "retiredAt",
  "destroyedAt"}`, without `sealed`. It writes one row of `storage_key_exports` in its transaction and puts `keyIds` into
  `system.storage.keys_exported` (Q-STOR-329), so that the destruction can report every export made before it, also after the audit trail's
  retention.
- **The import** inserts the destroyed rows the database lacks; it skips a sealed row of the file whose identifier the database holds
  destroyed and reports it as `destroyed` in its text, its JSON and `system.storage.keys_imported`. Nothing brings a destroyed key back; the
  offline tool stays the way to read with an older export (R-STOR-089).
- `activateHighest` and `checkKeys` ignore destroyed rows: the highest generation that is not destroyed is active (Q-STOR-325), and a
  connection whose keys are all destroyed has none.
- *As built* (Q-STOR-460): a connection of which the database holds a destroyed row gets nothing back, also a sealed row of the file whose
  identifier the database lacks; its sealed values are not opened either, so an export sealed under a root key the ring no longer has still
  imports the other connections. A destroyed row of the file is never applied to a key the database holds sealed, nor inserted beside the
  sealed keys of its connection: the import reports it as `stillSealed` (text, JSON and the event) and leaves it, since an import never
  destroys a key and a destroyed row would retire a live connection's id (R-STOR-102). `checkKeys` accepts a destroyed row only without a
  sealed value and with `retiredAt` and `destroyedAt`.

Acceptance criteria (unit, `cmd/quader` with keystest; `TestR_STOR_103_ExportAndImportKnowDestroyedKeys`):

- An export after a destruction prints the destroyed rows without `sealed`, and a row of `storage_key_exports`.
- An import of an older export inserts nothing for the destroyed connection, reports its identifiers, and imports the other connections'
  missing rows.

### R-STOR-091 · The tests of the feature
Priority: **P1** · Covers: quader-only

By the repository's rule that tests stay runnable ([Q-STOR-319](#q-stor-319--every-behaviour-of-the-encryption-once-at-the-cheapest-tier-and-its-fuzz-targets-in-the-nightly)):
every behaviour once, at the cheapest tier that shows it; nothing per format (a format never sees a connection or a key); no end-to-end leg;
kilobytes.

| Test | Tier | Data | Run time (estimate) |
|---|---|---|---|
| Vectors, known answers, every attack of R-STOR-078, `S` and its inverse, nonces, tails, keyed names (R-STOR-076 to R-STOR-079, R-STOR-086) | unit, `internal/blob/blobcrypt` | the vector files, below 1 MB | 0.4 s |
| Writes, retries, reads whole and ranged, marks, `unknown_key`, A6 (R-STOR-080, R-STOR-082) | unit, `internal/blob`, the in-memory store with an encrypted connection and the fault hook | objects up to 192 KiB | 0.5 s |
| Sessions: sealed data key, tails, counter, finalize by copy and by stream (R-STOR-081) | unit, `internal/uploads`, two in-memory stores | parts of 64 KiB, below 256 KiB | 0.4 s |
| Keys: drawing, the refusal over a used location, generations, reload, the handover's architecture test (R-STOR-083) | unit, `internal/blob/keys`, `internal/archtest` | — | 0.2 s |
| Export and import (R-STOR-085), the offline tool (R-STOR-089) | unit, `cmd/quader`, `internal/blob/offline` (the vector files among them), `internal/blob` (what the store wrote) | three objects of 1 KiB; the vector files; objects up to 131,073 bytes | 0.3 s (the offline tool measured 0.55 s on 2026-11-23) |
| The offline tool against the stack's bucket filled by a store with an encrypted connection, without the database (R-STOR-089) | integration, `test/integration/blob` | two objects, 1 KiB and 131,073 bytes | 0.8 s (measured) |
| Configuration and start-check refusals (R-STOR-075); the API's fields (R-STOR-088); the link to the plain copy (R-STOR-088) | unit, `internal/config`, `internal/app`, `internal/api/v1`, `internal/formathost` | — | 0.3 s |
| The Bastion's encrypted cache (bastion.md R-BAS-061) | unit, `internal/bastion/cache`, a temporary volume | objects of 1 KiB | 0.3 s |
| Chart: `encrypted` and `keyed_names` accepted, the refusal, the alert's rule ([ha-and-helm.md](ha-and-helm.md) R-HA-072, R-HA-045) | chart render tests | — | 0.5 s |
| A rotation of the root (R-STOR-084) | integration, `test/integration/blob`, PostgreSQL of the stack, an in-memory store | three objects of 1 KiB | 4 s |
| The reconciliation on and off and between encrypted connections, the scrub, the orphan sweep (R-STOR-087) | integration, `test/integration/blob`, PostgreSQL with two in-memory stores | ≤ 12 objects of 1 KiB | 6 s |
| Backup of an encrypted connection, deep verification without and with the ring, restore into the same and into another connection (backup-and-restore.md R-BAK-031) | integration, `test/integration/backup`, one case | three objects of 1 KiB | 8 s |
| One pass of the conformance checks with an encrypted connection against a real store: a write with the record, a whole and a ranged read, the finalize's copy with the record, the metadata kept by `HeadObject` | the job `s3-conformance` against Silo; the same checks against the in-memory store in the unit tier | probe objects | 3 s added to the job |
| Fuzz targets of R-STOR-079 | the nightly (`performance.yml`), a job "fuzz the stored format's readers", five minutes per target | generated | about 27 min of one runner, nightly only |
| Destroying a retired connection's keys (R-STOR-097 to R-STOR-103): the holders, the command, the backups' check, forgetting, `destroyed_key`, the retired id, export and import, the journal's replay | unit, `internal/blob/keys`, `cmd/quader`, `internal/blob`, `internal/blob/scrub`, `internal/app`; integration, `test/integration/blob` (the keys' case) | rows, three keys, objects of 1 KiB | 0.16 s unit, 0.5 s integration (measured on 2026-11-23: 0.24 s unit in the unit job's mode, 0.08 s in the keys' integration case) |
| The pool's keys and the re-pool (backup-and-restore.md R-BAK-032) | unit, `internal/backup/format` and `create`; integration, `test/integration/backup` (R-BAK-031's case extended) | lists of tens of lines; three objects of 1 KiB | 0.1 s unit, 3 s integration |

What each pipeline job gains (estimates): the unit job about 2.9 s; the chart test about 0.5 s; the integration shard that runs `blob` about
10 s and the one that runs `backup` about 8 s; `s3-conformance` about 3 s; every end-to-end leg nothing; the nightly one fuzz job of about
27 minutes beside its legs; the destruction of keys and the re-pool add about 0.3 s to the unit job, 0.5 s to the shard that runs `blob`
and 3 s to the one that runs `backup`, and nothing elsewhere. With 25 formats the cost is the same. No fuzz job ran nightly before it: the fuzz legs of npm and NuGet run only on
a dispatch with `closing=true` ([release.yml](../../.github/workflows/release.yml), jobs `fuzz-npm` and `fuzz-nuget`), so this job is the first
of its kind in the nightly. The closing run does not start it: the nightly fuzzes these readers every night, and a longer run is
`make fuzz-stored-format STORED_FORMAT_FUZZTIME=30m` by hand
([Q-STOR-323](#q-stor-323--the-closing-run-does-not-start-the-fuzz-job-of-the-stored-format)). Nothing of this needs the owner's
store; whether his in-cluster object gateway keeps the record and replaces it on a copy is what the start check establishes there once the
release runs (R-STOR-035), not a test.

## Retention and cleanup

Retention policies run inside Quader's job framework and delete through the normal delete path,
so format indexes, the trash can, audit events and webhooks behave exactly as for a manual delete.
Retention policies are instance-wide and managed by system administrators; they name repositories with the
selectors of an instance context ([authorization.md](authorization.md) R-AUTHZ-004: `<projectGlob>/<keyGlob>` or a
class), so one policy can reach repositories of several projects (project-scoped policies: Deferred).

### R-STOR-037 · Retention policy model and lifecycle
Priority: **P1** · Covers: RET-001, RET-013, RET-019

- Kinds: `package-cleanup` (R-STOR-038) and `build-cleanup` (R-STOR-043).
- Common fields: `key` (`^[A-Za-z0-9_-]{3,64}$`, unique), `description` (≤ 256 characters),
  `schedule` (cron, R-STOR-040; empty = manual only), `maxRunDurationMinutes` (1–300),
  `bypassTrash` (default `false`), `enabled`.
- The key `disable-all` is reserved, because `/api/v1/retention/policies/disable-all` (R-STOR-041)
  sits where a policy key would. A create with it answers `422 reserved_name`
  ([api-and-ui.md](api-and-ui.md) R-API-004). The key schema in the OpenAPI document lists it
  under `x-quader-reserved-names`. Matching is exact, as in the router, so `Disable-All` is not
  reserved.
- A policy is created disabled; a create request with `"enabled": true` is rejected with `422`.
  Enabling and disabling are separate calls. Disabled policies can be dry-run, edited and
  deleted; only enabled policies run on schedule or manually.
- A policy with an active run cannot be updated or deleted (`409`). Deleting a policy keeps its
  run history (runs keep a snapshot of the configuration).
- API (permission: system administrator):

  | Method | Path | Result |
  |---|---|---|
  | `GET` | `/api/v1/retention/policies?kind=` | list with last-run summary and `nextRunAt` |
  | `POST` | `/api/v1/retention/policies` | `201`; `409` key exists; `422` invalid; `422 reserved_name` for the key `disable-all` |
  | `GET`, `PUT`, `DELETE` | `/api/v1/retention/policies/{key}` | `GET` returns `ETag: "<revision>"`; `PUT` replaces the configuration, the key is immutable, and `If-Match` is required ([api-and-ui.md](api-and-ui.md) R-API-012, Q-API-2): `428` `precondition_required` without it, `412` `precondition_failed` when stale, `If-Match: *` for a deliberate blind write; `DELETE` accepts `If-Match` |
  | `POST` | `/api/v1/retention/policies/{key}/enable`, `…/disable` | `204` |

```sql
CREATE TABLE retention_policies (
    key                  text PRIMARY KEY CHECK (key ~ '^[A-Za-z0-9_-]{3,64}$'
                                             AND key <> 'disable-all'),  -- reserved (R-API-004)
    kind                 text NOT NULL CHECK (kind IN ('package-cleanup', 'build-cleanup')),
    description          text NOT NULL DEFAULT '' CHECK (char_length(description) <= 256),
    enabled              boolean NOT NULL DEFAULT false,
    schedule             text,
    max_run_minutes      integer CHECK (max_run_minutes BETWEEN 1 AND 300),
    bypass_trash         boolean NOT NULL DEFAULT false,
    spec                 jsonb NOT NULL,          -- scope and conditions, validated against a JSON Schema
    revision             integer NOT NULL DEFAULT 1,
    created_at           timestamptz NOT NULL DEFAULT now(),
    created_by           uuid,
    updated_at           timestamptz NOT NULL DEFAULT now(),
    updated_by           uuid
);
```

Acceptance criteria:

- `POST` with `"enabled": true` answers `422`; after `POST …/enable`, the policy runs on its
  schedule.
- `PUT` with the current `If-Match` during a run answers `409`.
- `PUT` without `If-Match` answers `428`; a `PUT` with a stale `If-Match`, for example the tag
  read before another administrator widened the repository `exclude` list, answers `412` and the
  stored scope stays as that administrator left it.
- `POST /api/v1/retention/policies` with key `disable-all` answers `422 reserved_name` and creates
  no policy.

### R-STOR-038 · Package cleanup policy: scope and conditions
Priority: **P1** · Covers: RET-003, RET-004, RET-005, RET-006, RET-007, RET-008, RET-022

```json
{
  "key": "npm-ci-prereleases",
  "kind": "package-cleanup",
  "description": "Remove CI prereleases nobody pulled for 30 days",
  "schedule": "30 2 * * 6",
  "maxRunDurationMinutes": 120,
  "bypassTrash": false,
  "spec": {
    "formats": ["npm"],
    "repositories": { "include": ["*/npm-ci-*"], "exclude": ["web/npm-ci-pinned"] },
    "paths": { "include": [], "exclude": [] },
    "packages": { "include": ["@acme/*"], "exclude": ["@acme/legacy-sdk"] },
    "versions": { "include": ["*-ci.*"], "exclude": [] },
    "conditions": {
      "olderThanDays": 90,
      "unusedForDays": 30,
      "properties": { "include": { "lifecycle": ["deprecated"] }, "exclude": { "retain": ["true"] } },
      "keepNewest": 3
    }
  }
}
```

*(example)*

- **Scope.** `formats` (at least one) and `repositories.include` (at least one; `@any-local` for
  all) select **local** repositories only; remote and virtual repositories are never processed
  (remote caches: R-STOR-045). `paths` narrow by path patterns matched against the lead item's
  path. `packages` and `versions` match normalized names and versions.
- **Patterns (Q-STOR-9).** Retention uses the one matcher that
  [authorization.md](authorization.md) owns, so a pattern copied between a permission policy and a
  cleanup policy selects the same things:
  - `repositories.include` and `repositories.exclude` take the repository selectors of an instance context of
    R-AUTHZ-004 (`web/npm-ci`, `web/npm-ci-*`, `*/npm-ci-*`, `@any-local` with an optional format filter such as
    `@any-local:npm`), parsed and validated by the same code: a bare key is refused with `422` `invalid_pattern`, and a
    key selector (both parts literal) must name an existing repository when the policy is saved. Class selectors other than `@any-local` are rejected with
    `422`, because only local repositories are processed.
  - In `repositories.include`, key selectors are stored by repository ID as R-AUTHZ-004 states, so
    a repository deleted and created again under the same name is not covered; a policy whose
    include selectors all disappeared matches nothing and is reported with `"inert": true`.
  - In `repositories.exclude`, key selectors are matched by name (project key and key) and are not removed when the
    repository is deleted. An exclusion protects whatever repository carries the name, so deleting
    and re-creating a protected repository never exposes it to the policy; binding the exclusion
    to the ID, as for includes, would silently remove that protection. Authorization has no
    repository-level exclude list, so this is the only point where retention binds a selector
    differently.
  - `packages` and `versions` use the name glob of R-AUTHZ-005 (`*` any run of characters except
    `/`, `**` any run, `?` one character except `/`, `\` escapes the next character), compared
    after the format's normalization and with its case rule (NuGet IDs case-insensitive, npm names
    case-sensitive).
  - `paths` use the path pattern of R-AUTHZ-005 (`*` within one segment, `**` as a whole segment).
  - An empty `include` list leaves a dimension unrestricted except for `repositories`, which
    needs at least one entry; an `exclude` match always removes a version from the scope. Invalid
    patterns answer `422` `invalid_pattern`.
- **Conditions.** At least one of `olderThanDays`, `unusedForDays`, `properties.include` and
  `keepNewest` is required, so a policy cannot select a whole scope by accident. All given
  conditions combine with AND:
  1. `olderThanDays`: the version's `created_at` is before run start minus the days.
  2. `unusedForDays`: the lead item's `last_download_at`, or its `created_at` if it was never
     downloaded, is before run start minus the days.
  3. `properties.include`: the lead item carries each listed key with at least one listed value;
     `properties.exclude`: a version whose lead item carries a listed key and value is never
     selected.
  4. `keepNewest: N` (N ≥ 1): per package, the N versions with the latest `created_at` are never
     selected, whatever the other conditions say; the newest version is therefore always kept.
- A day is 24 hours counted back from the run's start; no calendar arithmetic.
- Selected versions are removed through the format plug-in's `RemoveVersion` hook (an optional interface added
  with retention, R-ARCH-025), into the trash
  can unless `bypassTrash` is set or the trash can is disabled.
- Repository caps (R-STOR-044) and policies act independently; whichever keeps less wins.

Acceptance criteria:

- Fixture: package `@acme/w` with versions 1.0–1.5 created 200 to 10 days ago, 1.1 downloaded
  yesterday. A dry run with `olderThanDays: 60, unusedForDays: 60, keepNewest: 2` selects exactly
  the versions older than 60 days that were not downloaded in 60 days and are not among the two
  newest.
- A policy with only `formats` and `repositories` is rejected with `422`.
- A policy with `repositories.include: ["*/npm-ci-*"]` and `repositories.exclude: ["web/npm-ci-pinned"]`:
  after `npm-ci-pinned` of project `web` is deleted and created again, a dry run selects nothing in it; `npm-ci-pinned`
  of project `mobile` is in scope. `repositories.include: ["npm-ci-*"]` is refused with `422` `invalid_pattern`.

### R-STOR-039 · Lead item, version units and container images
Priority: **P1** · Covers: RET-009, RET-010

- Every format plug-in MUST declare the lead item of each version when it commits the version
  (npm: the tarball; NuGet: the `.nupkg`; Maven: the POM; container images: the manifest or
  index the tag points at). Conditions are evaluated on the lead item only.
- A version is removed as one unit through `RemoveVersion` (see above); partially removed versions do not
  occur. Generic repositories have no versions: each file is its own unit, `keepNewest` is not
  available (`422`), age, usage and property conditions apply per file.
- Container images (OCI, Docker and Helm OCI repositories; P1 formats): a tag is a version; an
  index and its child manifests form one unit and are removed together or not at all; layers
  and configs are removed when no remaining manifest of the repository references them (the
  format plug-in owns that rule). `keepNewest` counts tags per image name. Kept revisions of an
  overwritten tag (R-STOR-044) are removed together with their tag.
- A format without a lead-item declaration cannot be selected by package policies (`422` on
  save).

Acceptance criteria:

- For a Maven fixture, a policy whose property condition matches a property set only on the
  `.jar` of a version, not on its `.pom` (the lead item), selects nothing.
- Removing a multi-platform image tag by policy removes the index and all child manifests in one
  transaction; a concurrent `docker pull` of the tag sees either the whole image or `404`.

### R-STOR-040 · Policy scheduling, run limits and concurrency
Priority: **P1** · Covers: RET-011, RET-012, RET-018

- Schedules use the schedule syntax of R-STOR-025 (Q-STOR-4): five fields in UTC, day of week
  0–6 with 0 = Sunday or the names `SUN`–`SAT`. `7`, `?` and six-field expressions are rejected,
  so an expression written for a dialect whose week starts at 1 = Sunday cannot be misread. An
  empty schedule means manual runs only (RET-011).
- Minimum interval (RET-011): saving a policy fails with `422` `schedule_too_frequent` when two
  consecutive fire times within the 1,461 days (four years, so 29 February occurs) after the
  save are less than `retention.min_schedule_interval` apart. With the default of 6h,
  `@hourly`, `0 * * * *` and `0 0,3 * * *` are rejected, `0 */6 * * *` and `30 2 * * 6` accepted.
  The limit is checked when a policy is saved; changing the key later does not alter stored
  policies.
- A run stops when it reaches `maxRunDurationMinutes`, `retention.max_versions_per_run` removed
  versions, or `retention.error_threshold_percent` failed versions (after at least 100 attempts).
  Everything removed stays removed; the next run re-evaluates from scratch, so a backlog is
  worked off incrementally. Selection is idempotent and safe to interrupt at any version.
- At most `retention.max_concurrent_runs` runs execute system-wide (counted in PostgreSQL); further
  runs wait as `queued` (visible in the history) up to `retention.queue_size`; beyond that, a
  scheduled trigger is skipped with a log entry and an audit event, a manual trigger answers
  `429`. A policy never runs twice at the same time; a trigger while it runs answers `409`.
- **Run limit across workers.** A worker that claims a run job starts the run in one short
  transaction that first locks the single row of `retention_run_gate` (`SELECT … FOR UPDATE`),
  then counts the runs in `running` or `stopping`, and sets the run to `running` only below the
  limit. Two workers that claim runs at the same moment therefore cannot both take the last slot,
  and the lock is held for that transaction only, so it works through a transaction-mode pooler.
  At the limit, the job goes back to `queued` with `run_at` one `jobs.poll_interval` later and
  without counting the attempt (the release statement of [architecture.md](architecture.md)
  R-ARCH-016), and the run stays `queued`. A run resumed after a drain is already `running` and
  keeps its slot without passing the gate again. The enqueuing side (scheduler or manual trigger)
  applies `retention.queue_size` with the same lock.

  ```sql
  -- one row, seeded by the migration that creates the table
  CREATE TABLE retention_run_gate (id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1));
  ```
- Runs are jobs of the `default` queue, claimed by `worker` replicas (R-STOR-046); the scheduler
  that enqueues due runs is the singleton under the `scheduler` lease of
  [architecture.md](architecture.md) R-ARCH-016 and R-ARCH-017. "A policy never runs twice at the
  same time" is the exclusive dedup key `retention:<policy key>` in the job table, in PostgreSQL;
  no Valkey lock is involved (INV-3, Q-STOR-13).
- **Drain.** A node drain releases a running run (R-ARCH-016: back to `queued`, `attempt`
  unchanged). Another worker continues it: it keeps the run row, its counters and its deadline
  (`maxRunDurationMinutes` counts from the first start) and evaluates the selection again, which is
  idempotent. The run's status stays `running`; a drain never fails a run.
- **Lost worker.** A run whose worker disappears without releasing it (crash, node loss: its job
  lease expires) is marked `failed` with reason `worker-lost`: the job kind is registered with
  `MaxAttempts: 1`, so the reaper of R-ARCH-016 fails it instead of re-queueing it, and the next
  scheduled or manual run starts from scratch.
- **Deletion pause.** Before every batch of removals a run reads the deletion pause of R-STOR-024;
  while it is set, the run ends with status `stopped` and `error.code = deletions_paused`, and
  removes nothing more.
- Runs and garbage collection may overlap (R-STOR-024).

Configuration:

```yaml
retention:
  min_schedule_interval: 6h      # default (proposed)
  max_concurrent_runs: 3         # default (proposed)
  queue_size: 10                 # default (proposed)
  max_versions_per_run: 100000   # default (proposed)
  error_threshold_percent: 10    # default (proposed)
  batch_size: 500                # default (proposed); versions removed per transaction
```

Acceptance criteria:

- `schedule: "0 * * * *"` is rejected; `"0 */6 * * *"` is accepted; `"0 3 * * 7"` and
  `"0 30 3 ? * SUN"` are rejected with `invalid_schedule`.
- A run with `maxRunDurationMinutes: 1` over 50,000 matching versions stops after about one minute
  with status `stopped-limit`; the next run continues with the remaining versions.
- `kubectl drain` of the node whose `worker` runs a retention run over 50,000 matching versions:
  the run finishes on the other `worker` replica with status `completed`, and its report lists
  every removed version once.
- With `retention.max_concurrent_runs: 1` and three `worker` replicas, manual runs of three
  policies are triggered within one second: at no time are two of them `running`, and all three
  end `completed`.

### R-STOR-041 · Manual run, dry run, stop and disable all
Priority: **P1** · Covers: RET-014, RET-015, RET-016, RET-017

| Method | Path | Body | Result |
|---|---|---|---|
| `POST` | `/api/v1/retention/policies/{key}/runs` | `{"dryRun": false}` | `202 {"runId": "…"}`; `409` disabled policy for a real run or run active; `429` queue full |
| `POST` | `/api/v1/retention/runs/{runId}/stop` | — | `202`; status `stopping`, then `stopped` |
| `POST` | `/api/v1/retention/runs/stop-all` | — | `202`; stops every running and queued run |
| `POST` | `/api/v1/retention/policies/disable-all` | — | `200 {"disabled": n}` |

- A dry run is allowed on disabled policies, evaluates exactly like a real run, removes nothing
  and produces a report (R-STOR-042). It reflects the moment it ran.
- A stopped run cannot be resumed; the next run starts from scratch.
- A stop reaches the run through PostgreSQL, whichever `web` replica receives it: stop and
  stop-all set the runs to `stopping` and their jobs to `cancelling` (queued runs end `stopped` at
  once). The run reads its status before every batch, and the job heartbeat of
  [architecture.md](architecture.md) R-ARCH-016 cancels its context, so it ends `stopped` after the
  current batch on whichever `worker` it runs.
- Every manual trigger, stop and disable-all records the acting principal in the run and in the
  audit log. Permission: system administrator.

Acceptance criteria:

- A dry run of a disabled policy answers `202`, finishes with status `completed` and
  `dryRun: true`, and leaves every version in place.
- `stop-all` during two active runs makes both end as `stopped` within one batch.

### R-STOR-042 · Run history and reports
Priority: **P1** · Covers: RET-020, RET-021

- Every run, real or dry, is a row with: policy key and configuration snapshot, `dryRun`,
  `manual`, `triggeredBy`, `status` (`queued`, `running`, `running-with-errors`, `completed`,
  `completed-with-errors`, `stopping`, `stopped`, `stopped-limit`, `failed`), start and end,
  versions evaluated, selected, removed, failed, logical and physical bytes freed (physical:
  blobs no other reference uses at the time of removal).
- Per-version records (package, version, repository, lead path, size, created, last download,
  status, message) are kept for `retention.item_detail_retention`.
- When a run selected at least one version, a CSV report is generated at the end (RFC 4180,
  UTF-8, one row per version) and stored as a blob referenced by the run row, so it survives as
  long as the run. Runs that select nothing have no report.
- API (system administrator):

  | Method | Path | Result |
  |---|---|---|
  | `GET` | `/api/v1/retention/runs?policy=&status=&from=&to=&dryRun=&excludeEmpty=&cursor=&limit=&sort=` | newest first |
  | `GET` | `/api/v1/retention/runs/{runId}` | one run with configuration snapshot |
  | `GET` | `/api/v1/retention/runs/{runId}/report` | `text/csv`; `404` when none |
  | `GET` | `/api/v1/retention/runs/{runId}/items?status=&cursor=` | per-version records while retained |

- Timestamps are RFC 3339 throughout.

```sql
CREATE TABLE retention_runs (
    id               uuid PRIMARY KEY,
    policy_key       text,                     -- not a FK: history outlives the policy
    policy_snapshot  jsonb NOT NULL,
    dry_run          boolean NOT NULL,
    manual           boolean NOT NULL,
    triggered_by     uuid,
    status           text NOT NULL,
    started_at       timestamptz,
    finished_at      timestamptz,
    counters         jsonb NOT NULL DEFAULT '{}',
    blob_sha256      bytea REFERENCES blobs (sha256),   -- CSV report; guard and release triggers installed
    error            text
);
CREATE INDEX retention_runs_blob ON retention_runs (blob_sha256);
CREATE INDEX retention_runs_policy ON retention_runs (policy_key, started_at DESC);

CREATE TABLE retention_run_items (
    run_id        uuid NOT NULL REFERENCES retention_runs (id) ON DELETE CASCADE,
    seq           integer NOT NULL,
    repository    text NOT NULL,
    package       text NOT NULL,
    version       text NOT NULL,
    lead_path     text NOT NULL,
    size          bigint,
    created_at    timestamptz,
    last_download timestamptz,
    status        text NOT NULL,
    message       text,
    PRIMARY KEY (run_id, seq)
);
```

Configuration:

```yaml
retention:
  history_retention: 2160h       # default (proposed): runs and reports kept 90 days
  item_detail_retention: 720h    # default (proposed): per-version records kept 30 days
```

Acceptance criteria:

- After a dry run that selects three versions, `curl …/runs/{id}/report` returns a CSV with a
  header and three rows; a run selecting nothing answers `404` for the report.
- The report blob survives garbage collection while the run row exists and is collected after
  the run row expires.

### R-STOR-043 · Build cleanup policy and per-build retention
Priority: **P1** · Covers: RET-023, RET-037

Build records are owned by a separate spec page, `builds.md` with prefix `R-BLD`, to be written
before milestone M9 (Q-STOR-3). This requirement fixes the cleanup mechanics against the contract
that page has to provide:

- A build run has a name, a number, a start time and a list of produced files given as
  repository, path and SHA-256.
- Build records live in PostgreSQL tables, not as items of a repository. No package policy,
  path-based cleanup, copy, move or repository delete can reach them.
- Build permissions (`delete` on a build) come from [authorization.md](authorization.md)
  R-AUTHZ-027.

Cleanup mechanics:

- `build-cleanup` policy spec: `builds.include` / `builds.exclude` (the name glob of R-AUTHZ-005,
  as in R-STOR-038; build names may contain `/`, which `*` does not cross and `**` does),
  `olderThanDays` (required), `removeArtifacts` (default `true`).
- A selected build run's record is removed; with `removeArtifacts`, each recorded file is
  removed when the item at the recorded path still references the recorded SHA-256 (an
  overwritten path is left alone and reported). The result per build is `removed`,
  `partially-removed (x of y)` or `skipped`.
- Per-build retention (RET-037): `PUT /api/v1/builds/{buildName}/retention` with
  `{"keepNewest": 20, "maxAgeDays": 180, "protectedNumbers": ["1.0.0-rc.1"], "removeArtifacts": false, "wait": false}`
  *(example)* stores the parameters on the build name and applies them **at once**: the call
  enqueues a `build-retention` job for the name. With `wait: false` (default) it answers `202`
  with the job reference of R-OPS-019. With `wait: true` it waits up to
  `retention.build_retention_wait`; if the job finished, it answers `200` with the job result,
  otherwise `202` with the job reference, so a long removal never holds a request open past
  that bound. The stored parameters are applied again after every new run of that build name is
  published.
- A run is removed when it is outside the newest `keepNewest` runs **or** started more than
  `maxAgeDays` days before the evaluation; each rule removes on its own. Runs whose number is in
  `protectedNumbers` are never removed.
- Without `keepNewest` and `maxAgeDays` the `PUT` stores nothing and answers `422`: a retention
  without a keep rule would remove nothing. `GET` on the same path returns the stored parameters
  (`404` when none); `DELETE` removes them (`204`) and removes no run.
- Permission: `delete` on the build for `PUT` and `DELETE`, `read` on the build for `GET`.
- Build records and reports are never selected by package policies.

Acceptance criteria:

- With `keepNewest: 2` on build `web`, publishing a third build removes the oldest record (and
  its files when `removeArtifacts` is true).
- On a build with three runs, `PUT …/retention` with `{"keepNewest": 1, "wait": true}` answers
  `200` and leaves one run; after `DELETE …/retention`, publishing a fourth run removes nothing.
- `PUT …/retention` with `{"protectedNumbers": ["7"]}` alone answers `422` and stores nothing.

Configuration:

```yaml
retention:
  build_retention_wait: 60s    # default (proposed); longest wait of a PUT with "wait": true
```

### R-STOR-044 · Repository caps enforced on push
Priority: **P1** · Covers: RET-035

The caps themselves (maximum distinct snapshots per artifact, maximum distinct tags per image,
kept revisions per overwritten tag) are repository settings owned by
[repositories-and-proxy.md](repositories-and-proxy.md). This page specifies enforcement.

- All three caps are counts (RET-035): distinct snapshot versions per artifact, distinct tag names
  per image, kept revisions per tag name. Federated repositories are not part of the MVP (INV-5);
  when they arrive, they use the same count semantics, not an age in days.
- After a push, the format plug-in's `VersionGroup` (an optional interface added with the caps, R-ARCH-025) names the affected group, and the push's
  commit transaction appends one row (repository, group, push time) to the table `cap_outbox`.
  Rows are only inserted and deleted, never updated, so concurrent pushes to one group never wait
  for each other or for enforcement.
- A loop on every `worker` replica (R-STOR-046) runs every `retention.caps.interval`. One
  iteration is one transaction: it locks up to 100 outbox rows with `FOR UPDATE SKIP LOCKED`,
  evaluates each group among them, removes the oldest variants beyond the cap through the normal
  delete path, oldest first, and deletes exactly the rows it locked. Each variant is removed by
  the version row the evaluation read, so a tag re-pushed meanwhile, which has a new current
  version row, is not removed.
  - A push that commits during an iteration has a row the iteration did not lock, so its group is
    evaluated again. No push is lost, as it would be with one upserted row per group, where a push
    whose upsert met the row locked by a running evaluation would add nothing that the evaluation
    could still see.
  - A worker drained or lost mid-iteration rolls back, and its rows wait for another worker.
  - Two workers may evaluate one group at once when they locked different rows of it. Each removes
    only what is beyond the cap in the state it read, a variant the other already removed is
    skipped, and removals take their locks oldest first, so the outcome is that of one evaluation
    of the later state and the two cannot deadlock. No lease and no Valkey lock is involved.
  - While the deletion pause of R-STOR-024 is set, the loop claims nothing.
- "Oldest" is the earliest `created_at` of the variant's current version row: for a snapshot its
  deploy time, for a tag the time the tag name last received a push (a re-pushed tag counts as
  new). No tag name is exempt, `latest` included; a tag that keeps receiving pushes is never the
  oldest.
- Removals by caps go to the trash can when it is enabled (R-STOR-022), for snapshots and tags
  alike, and are audited like any delete.
- Enforcement is lazy: only groups that received a push are evaluated; lowering a cap does not
  shrink groups until their next push.
- For kept tag revisions, a revision is a hidden version row of the tag; when a tag is removed
  by the tag cap or a policy, its kept revisions go with it.
- A snapshot cap below 2 is rejected (an in-flight multi-file deploy would otherwise remove its
  own siblings).

Configuration:

```yaml
retention:
  caps:
    interval: 5m      # default (proposed)
```

Acceptance criteria:

- With a tag cap of 3 on `docker-local`, pushing tags `t1`–`t4` of one image leaves `t2`–`t4`
  within one interval; `t1` is in the trash can.
- With the same cap, pushing `latest`, `t1`, `t2`, re-pushing `latest`, then pushing `t3` removes
  `t1`, not `latest`.
- With a tag cap of 3 and two `worker` replicas, 20 tags of one image are pushed concurrently
  through three `web` replicas while the loop runs with `retention.caps.interval: 1s`. One
  interval after the last push, exactly the 3 newest tags remain, `cap_outbox` is empty, and
  `pg_stat_database.deadlocks` has not grown.

### R-STOR-045 · Unused remote-cache expiry
Priority: **P1** · Covers: RET-036

- The per-remote-repository setting "unused expiry" (a duration; zero or absent keeps cached
  content forever) is owned by [repositories-and-proxy.md](repositories-and-proxy.md).
- The task `remote-cache-cleanup` of the shared task framework (Q-STOR-10; R-OPS-019) removes
  cached file items of remote repositories whose last use, `coalesce(last_download_at,
  created_at)`, is older than their repository's expiry. Removal is permanent (no trash can); the
  content is fetched again on the next request. Removed blobs are collected by R-STOR-024.
- The task never removes the item of the layer that is the image scanning's database in use: the item
  `<image>/_blobs/<digest>` of the repository `vulnerabilities.images.database.repository` names, by the digest the source `trivy`
  records ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-044, [repositories-and-proxy.md](repositories-and-proxy.md)
  R-REPO-070). The workers read it through the item store, which is no download, so its last use would otherwise stay its creation. The
  task's candidate statement applies the function `vuln_image_database_in_use(blob_sha256)` of migration 00089 to every candidate, which
  answers from the scanning's own state ([vulnerability-scanning.md](vulnerability-scanning.md) Q-VULN-190); the function is built, the
  task is not. Until it is, the image scanning's task trims the database's repository itself by the same rule, the repository's own cache
  period, after each run that succeeded (Q-VULN-194): `upstream/scanner-trivy` holds the layer in use and two days of builds.
- The task processes repositories one after another in batches and checkpoints after each batch,
  so a drained run continues on another `worker` from its checkpoint. It honours the deletion
  pause of R-STOR-024 before every batch. Manual start:
  `POST /api/v1/system/tasks/remote-cache-cleanup/run` (system administrator,
  `202 {"job": {…}}`, `409` while a run is active).

Configuration:

```yaml
maintenance:
  tasks:
    remote-cache-cleanup: {enabled: true, schedule: "0 3 * * *"}   # default (proposed): daily 03:00 UTC
```

Acceptance criteria:

- With expiry `48h` on `npm-remote`, a tarball last downloaded 3 days ago is removed by the job; a
  following `npm install` fetches it from the upstream again.

## Roles, scale-out and backups

### R-STOR-046 · Storage work per role and across replicas
Priority: **MVP** · Covers: quader-only

Every storage mechanism runs on the role INV-2 assigns, behaves the same on any number of replicas
of that role, and uses no state that an earlier request left on one replica (INV-18). The expected
load exceeds one replica (INV-12), so no mechanism on this page may depend on the replica count.

| Work | Role | How replicas share it |
|---|---|---|
| Uploads (R-STOR-004 to R-STOR-010), downloads (R-STOR-012 to R-STOR-014), item operations up to `storage.delete.max_sync_items`, remote-cache fills | `web` | Any replica serves any request; metadata in PostgreSQL, content in S3 |
| Multi-request upload sessions (R-STOR-011, P1) | `web` | Session row and parts in PostgreSQL, bytes in one S3 multipart upload; each request may reach another replica |
| Download-statistics buffer (R-STOR-015) | `web` | Per replica; flushed every interval and at shutdown, in item order |
| Usage fold (R-STOR-003), cap enforcement (R-STOR-044, P1) | `worker`, every replica | Loops that lock their rows with `FOR UPDATE SKIP LOCKED`; any number of workers, no lease |
| Collection, orphan sweep, scrub, reconciliation, remote-cache cleanup (R-STOR-025 to R-STOR-027, R-STOR-045, R-STOR-056) | `worker` | Jobs of the queue `maintenance` with an exclusive dedup key in the job table (Q-STOR-10, Q-STOR-13); each walks every connection of the worker's configuration |
| Storage summary (R-STOR-028) | `worker` | The task of [operations-and-observability.md](operations-and-observability.md) R-OPS-023; reads are served by any `web` replica from its snapshot table |
| Retention runs, build retention, background deletes, copies, moves and trash restorations (R-STOR-018, R-STOR-019, R-STOR-023, R-STOR-040, R-STOR-043) | `worker` | Jobs of the queue `default` ([architecture.md](architecture.md) R-ARCH-016; retention runs and build retention with the dedup keys registered there); the retention run limit through the row lock of `retention_run_gate` (R-STOR-040) |
| Retention reports (R-STOR-042) | `worker` | Written by the run through the upload pipeline and the worker's spool |
| Startup check (R-STOR-035) | `web`, `worker` | Per replica and connection, before its first readiness (R-STOR-060) |

- **Local storage.** The spool is the only local storage on this page: one per `web` and `worker`
  pod, an `emptyDir` or a generic ephemeral volume, never shared between pods and never persistent
  (INV-13, R-STOR-004). No storage mechanism needs a ReadWriteMany or a persistent volume.
- **Exclusion.** Every rule that keeps storage work from running twice lives in PostgreSQL: job
  dedup keys, row locks and `SKIP LOCKED` claims (INV-3). None uses Valkey or an advisory-lock
  lease (Q-STOR-13). The correctness of the commit protocol, collection and the orphan sweep rests
  on row locks alone; exclusion only saves duplicate work.
- **Valkey.** Storage reads and writes nothing in Valkey itself. It uses Valkey only through
  [architecture.md](architecture.md): job wake-up signals and the change messages of the `cache`
  area (R-ARCH-018), and the flight lock of remote fills (R-ARCH-033). While Valkey is unavailable,
  every storage operation behaves as with it; job pick-up waits for `jobs.poll_interval`, and a
  change to the storage settings waits for the change-log poll.
- **Shared values.** Counters and limits that must agree across replicas live in PostgreSQL:
  usage, quotas and download statistics (Q-STOR-12). Per-replica values exist only to protect the
  replica itself: the spool budget, the S3 connection pool and part concurrency, and the circuit
  breaker (R-STOR-004, R-STOR-033).
- **Splitting the service.** Because every replica of a role can do everything this table assigns
  to the role, the cut into `api` and `packages` (phase 6a), a `packages` pool that serves only some package paths, or
  a worker pool that claims only the `maintenance` queue ([ha-and-helm.md](ha-and-helm.md) R-HA-062), needs no storage
  change.
- **Drain.** A node drain costs no acknowledged data and fails no storage operation that its client
  retries:
  - on `web`: an upload cut before its commit leaves nothing (R-STOR-004); a cut download resumes
    with `Range` on any replica (R-STOR-012); an upload session continues on another replica from
    its last recorded part, because the cut request releases its writer claim (R-STOR-004 step 8,
    R-STOR-011); download counts are flushed (R-STOR-015);
  - on `worker`: jobs are released with their checkpoints (R-STOR-025, R-STOR-026, R-STOR-045),
    retention runs continue on another worker (R-STOR-040), a collector waits for its S3 calls
    and hands its unfinished claims over before its job is released (R-STOR-024), and an
    interrupted fold or cap iteration rolls back (R-STOR-003, R-STOR-044).

Acceptance criteria:

- With Valkey stopped, the storage integration suite (upload, deduplication, download with
  `Range`, delete, copy, collection with `grace_period: 0s`, quota rejection) passes against three
  `web` and two `worker` replicas, and no replica turns unready.
- The scatter profile of [architecture.md](architecture.md) R-ARCH-032 passes the R-STOR-005 race
  test through the Service, with uploads, deletes and downloads spread round robin over three `web`
  replicas and collection running on two `worker` replicas.
- During [ha-and-helm.md](ha-and-helm.md) R-HA-059's drain test, one `worker` runs `gc-full` and
  another a retention run; both finish on the remaining `worker` replicas, and no client operation
  of the test's load fails. The drain test (E5.9) runs the
  full collection run of this criterion; the retention run is not built yet, and the jobs of the queue `default` it stands for are
  judged on the drain test's probe jobs ([Q-STOR-211](#q-stor-211--r-stor-046s-drain-criterion-runs-a-real-full-collection-run-on-the-evicted-worker-and-judges-the-retention-half-on-the-probe-jobs-until-retention-runs-exist)).
- A source test fails when a package below `internal/blob` or the storage job handlers imports the
  Valkey client or calls a session-level advisory-lock function.

### R-STOR-047 · Storage state in backups and restores
Priority: **MVP** · Covers: quader-only

[backup-and-restore.md](backup-and-restore.md) owns the backup job, its format and the restore
procedure (INV-19). This requirement states what storage provides to them and what a restore
leaves for storage.

- **Referenced blobs.** A backup finds the blobs its snapshot references through the reference
  rules of R-STOR-003, as collection does (rule 3: the referencing tables come from the catalog's
  foreign keys to `blobs`). A table that a later page adds under those rules is backed up without
  a change to the backup.
- **Garbage collection.** The per-blob protocol of R-STOR-024 contains the hold check (2b), so a
  running backup never loses a blob it still has to copy, whatever `storage.gc.grace_period` is
  and however long the backup runs. The orphan sweep needs none (R-STOR-026). Collection and a
  backup run at the same time; neither waits for the other.
- **Export classes** of this page's tables ([backup-and-restore.md](backup-and-restore.md)
  R-BAK-004):

  | Table | Class | Reason |
  |---|---|---|
  | `blobs` | `blobs` | Only rows that the snapshot references; unreferenced and `deleting` rows are not restored |
  | `blob_copies`, `blob_copy_queue`, `storage_connections`, `revocation_outbox_shipped` | `transient` | They describe the objects of the source's connections; a restore writes its own copies and the fix-up records them (R-STOR-052, [backup-and-restore.md](backup-and-restore.md) R-BAK-030) |
  | `blob_gc_queue` | `transient` | Candidates only |
  | `upload_sessions`, `upload_session_parts`, `upload_session_tails` | `transient` | Staged bytes under `_uploads/` are not backed up; clients restart their uploads |
  | `pending_sidecars` | `transient` | Declarations waiting for a file for at most `storage.sidecars.pending_ttl` |
  | `repository_usage`, `storage_usage_totals`, `storage_usage_deltas` | `data` | Exported, then recomputed by the restore (below) |
  | every other table of this page | `data` | Items (trashed ones included), packages, versions, properties, download statistics, property sets, retention policies, runs and run items, `retention_run_gate` (its one row), `cap_outbox` (pending cap evaluations; evaluating a group again is harmless) |

- **Restore fix-ups.** In the fix-up transaction of R-BAK-022 the restore calls
  `storage_usage_recount()` (R-STOR-003), because the `blobs` filter drops rows that the exported
  physical totals still count; the quota of R-STOR-029 would otherwise decide on wrong numbers.
  Blob rows of digests the backup recorded as damaged are loaded with
  `content_status = 'missing'` (R-BAK-021 step 4), so their downloads answer `503` with
  `Retry-After` as for any missing object (R-STOR-012,
  [backup-and-restore.md](backup-and-restore.md) R-BAK-001). Trashed items keep
  their `trashed_at`, so trash expiry continues from the original deletion.
- **No storage work during a restore.** While `system_state` marks a restore in progress, `worker`
  replicas wait at their schema check and claim no job ([architecture.md](architecture.md)
  R-ARCH-024 step 3). No collection, orphan sweep or retention run can therefore meet blob objects
  that the restore has copied before the rows that reference them are loaded.
- **Key families.** Only `sha256/` objects that the snapshot references are backed up (R-STOR-001), each read from one copy
  ([backup-and-restore.md](backup-and-restore.md) R-BAK-030). A restore into an empty installation writes exactly those, on the
  first connection, so it leaves no orphans; the reconciliation fills the other connections. A restore
  onto an existing blob prefix (R-BAK-025, P1) sets the deletion pause, and after deletions resume,
  the orphan sweep (R-STOR-026) removes what the restored database does not reference.
- **Platform restores.** A point-in-time restore of the platform's database backup relies on the
  grace period or bucket versioning instead ([ha-and-helm.md](ha-and-helm.md) R-HA-024, R-STOR-024
  "Grace period"), and on the deletion pause that `quader admin post-restore` sets (R-HA-025).

Acceptance criteria:

- A backup taken while three unreferenced `live` blobs and two `deleting` rows exist is restored.
  The first storage summary afterwards (R-STOR-028) reports `blobs.count` and `blobs.bytes` equal
  to `SELECT count(*), sum(size) FROM blobs WHERE state = 'live'`, and a system quota set just
  above that size admits an upload that fits.
- A unit test compares the backup table registrations of this page's tables with the table above.

## Configuration reference

<details>
<summary>All storage, retention and storage-task keys of quader.yaml</summary>

```yaml
storage:
  connections:                           # R-STOR-050; one to eight; list order = write and read order
    - id: default                        # default: default, for at most one element
      endpoint: ""                       # default (proposed)
      endpoint_file: ""                  # default
      region: ""                         # default; example: eu-central-1
      region_file: ""                    # default
      bucket: ""                         # example: quader-blobs; required unless bucket_file
      bucket_file: ""                    # default
      prefix: quader                     # default (proposed)
      force_path_style: false            # default (proposed)
      allow_insecure_endpoint: false     # default (proposed)
      ca_file: ""                        # default (proposed)
      storage_class: ""                  # default (proposed)
      payload_checksum: sha256           # default (proposed)
      request_checksum_calculation: when_supported  # default (proposed)
      response_checksum_validation: when_supported  # default (proposed)
      batch_delete: true                 # default (proposed)
      credentials_source: default        # default (proposed)
      role_arn: ""
      web_identity_token_file: ""
      sts_endpoint: ""
      access_key_id_file: ""
      secret_access_key_file: ""
      session_token_file: ""
      sse_mode: none                     # default (proposed)
      sse_kms_key_id: ""
      sse_bucket_key: true               # default (proposed)
      scrub_period: 720h                 # default
      scrub_bytes_per_second: 50MiB      # default
      drain: false                       # default
      encrypted: false                   # default; true refused until stage 2 (R-STOR-075)
      keyed_names: true                  # default with encrypted: true; refused with encrypted: false (R-STOR-086)
  object_store:                          # every connection's client (R-STOR-033 to R-STOR-035)
    http:
      max_connections: 256               # default (proposed)
      dial_timeout: 5s                   # default (proposed)
      tls_handshake_timeout: 5s          # default (proposed)
      response_header_timeout: 30s       # default (proposed)
      idle_conn_timeout: 90s             # default (proposed)
      idle_transfer_timeout: 60s         # default (proposed)
      receive_window: 256KiB             # default; HTTP/2 only (R-STOR-033)
      proxy_url: ""                      # default (proposed)
      proxy_credentials_file: ""
      no_proxy: ""
    retry:
      max_attempts: 5                    # default (proposed)
      max_backoff: 20s                   # default (proposed)
    circuit_breaker:
      failure_threshold: 5               # default (proposed)
      open_duration: 10s                 # default (proposed)
    multipart:
      threshold: 100MiB                  # default (proposed)
      part_size: 16MiB                   # default (proposed)
      concurrency: 4                     # default (proposed)
      max_concurrent_parts: 64           # default (proposed)
    startup_check: true                  # default (proposed)
  replication:
    min_copies: 1                        # default (R-STOR-053)
  reconcile:                             # R-STOR-056
    concurrency: 4                       # default
    bytes_per_second: 100MiB             # default
    server_side: auto                    # default
    queue_limit: 10000                   # default
    retry_max_backoff: 1h                # default
    run_duration: 10m                    # default
  spool:
    dir: /var/lib/quader/spool          # default (proposed)
    max_bytes: 50GiB                     # default (proposed)
    min_free_bytes: 2GiB                 # default (proposed)
    memory_threshold: 1MiB               # default (proposed)
    warn_percent: 80                     # default (proposed)
  upload:
    max_size: 10GiB                      # default (proposed)
    commit_attempts: 5                   # default (proposed)
    commit_deadline: 1h                  # default (proposed)
    blob_wait_timeout: 120s              # default (proposed)
  uploads:                               # multi-request upload sessions (R-STOR-011, P1)
    part_size: 16MiB                     # default (proposed)
    writer_lease: 60s                    # default (proposed)
    session_ttl: 4h                      # default (proposed)
    session_max_age: 20h                 # default (proposed); < storage.orphans.multipart_max_age
  deploy_by_checksum:
    enabled: true                        # default (proposed)
    max_candidates: 100                  # default (proposed)
  sidecars:
    pending_ttl: 1h                      # default (proposed)
  download:
    buffer_size: 256KiB                  # default (proposed)
    verify: true                         # default (proposed)
    hold_back: 64KiB                     # default (proposed); at least 1 KiB
  stats:
    flush_interval: 10s                  # default (proposed)
  delete:
    max_sync_items: 10000                # default (proposed)
    batch_size: 5000                     # default (proposed)
  properties:
    max_values_per_item: 1000            # default (proposed)
    max_recursive_items: 100000          # default (proposed)
  usage:
    fold_interval: 5s                    # default (proposed); also the lag of quota decisions (R-STOR-029)
  gc:
    grace_period: 24h                    # default (proposed)
    delete_settle: 30s                   # default (proposed); at least 1 s
    handover_delay: 10m                  # default (proposed)
    batch_size: 1000                     # default (proposed)
    delete_concurrency: 8                # default (proposed)
  orphans:
    min_age: 24h                         # default (proposed)
    multipart_max_age: 24h               # default (proposed)
  scrub:
    run_duration: 50m                    # default (R-STOR-027; the period and the throttle are keys of each connection)
retention:
  min_schedule_interval: 6h              # default (proposed)
  max_concurrent_runs: 3                 # default (proposed)
  queue_size: 10                         # default (proposed)
  max_versions_per_run: 100000           # default (proposed)
  error_threshold_percent: 10            # default (proposed)
  batch_size: 500                        # default (proposed)
  history_retention: 2160h               # default (proposed)
  item_detail_retention: 720h            # default (proposed)
  build_retention_wait: 60s              # default (proposed)
  caps:
    interval: 5m                         # default (proposed)
maintenance:
  tasks:                                 # task framework of R-OPS-019; schedule syntax of R-STOR-025
    gc:                   {enabled: true, schedule: "*/30 * * * *"}  # default (proposed)
    gc-full:              {enabled: true, schedule: "0 2 * * 0"}     # default (proposed)
    orphan-sweep:         {enabled: true, schedule: "0 5 * * 6"}     # default (proposed)
    scrub:                {enabled: true, schedule: "7 * * * *"}     # default: hourly (R-STOR-027)
    storage-reconcile:    {enabled: true, schedule: "* * * * *"}     # default: every minute (R-STOR-056)
    storage-summary:      {enabled: true, schedule: "17 * * * *"}    # default (proposed, R-OPS-019)
    remote-cache-cleanup: {enabled: true, schedule: "0 3 * * *"}     # default (proposed)
```

Durations use Go duration syntax (`h`, `m`, `s`; there is no `d` unit); sizes use IEC units
(`KiB`, `MiB`, `GiB`). Runtime settings (trash can, system quota) live in PostgreSQL and are
changed through `PUT /api/v1/settings/storage` with `If-Match` (R-STOR-022). Readiness has no storage key: the opt-in
`health.require_object_store` belongs to R-OPS-001 (R-STOR-036). The internal loops
(`storage.usage.fold_interval` and `retention.caps.interval` on every `worker` replica,
`storage.stats.flush_interval` on every `web` replica, R-STOR-046) are not maintenance tasks and
keep their own interval keys. There is no storage key for Valkey: storage uses it only through
[architecture.md](architecture.md) (R-STOR-046). Startup validation refuses
`storage.gc.handover_delay` not above `jobs.lease_duration` plus twice `storage.gc.delete_settle`,
`storage.uploads.session_max_age` not below `storage.orphans.multipart_max_age`,
`storage.upload.blob_wait_timeout` not above twice `storage.gc.delete_settle`, a
`storage.upload.max_size` above 10,000 × `storage.uploads.part_size`, and the rules of R-STOR-050 and R-STOR-051 for the
connections; each message names the keys.

</details>

## Deferred

<details>
<summary>Deferred catalog entries (48)</summary>

| Catalog ID | Priority | Note |
|---|---|---|
| STOR-005 | P2 | SHA-256 backfill: not needed, every digest is computed at ingest from the first release. |
| STOR-007 | P2 | Composable provider chain: one kind of store by design (INV-4); several connections of that kind, each holding every blob, are R-STOR-050. |
| STOR-008 | P2 | Local file-system store: unsafe with stateless replicas; MinIO covers on-premises use. |
| STOR-009 | P2 | Node-local read cache in front of S3. |
| STOR-010 | P2 | Database blob store: contradicts INV-4 (no binary content in PostgreSQL); not planned. |
| STOR-013 | P2 | Cross-zone sharding; several connections in several regions, each holding every blob, need no zones (R-STOR-050). |
| STOR-014 | P2 | Cluster-shared node-local stores. |
| STOR-016 | P2 | Asynchronous upload queue: conflicts with "201 means durable in S3" on stateless replicas. |
| STOR-018 | Later | Binary fetch workers for federated repositories. |
| STOR-026 | P2 | Multi-bucket sharding, spreading blobs over buckets for capacity; not the mirror of R-STOR-050, in which every connection holds every blob. |
| STOR-027 | P2 | Migration of an existing store into object storage. |
| STOR-028 | P2 | Storage-class (tiering) compatibility; `storage_class` exists, guidance pending. |
| STOR-030 | P2 | Google Cloud Storage native client. |
| STOR-031 | P2 | Azure Blob Storage. |
| STOR-032, STOR-033 | P2 | Specified as R-STOR-014 so the download path leaves room for it. |
| STOR-034 | P2 | CDN-signed download URLs. |
| STOR-035 | P2 | Client choice of the redirect target. |
| STOR-041 | Out of scope | Embedded database compaction; Quader has no embedded database. |
| ART-005 | P2 | Deploy an archive and extract it on arrival. |
| ART-007 | P2 | Client-driven multipart upload with presigned part URLs; R-STOR-011 sessions are the server-side counterpart. |
| ART-010 | P2 | SHA-256 backfill operation; not needed (see STOR-005). |
| ART-012 | P2 | Property-constrained resolution on download. |
| ART-013 | P2 | Latest-version tokens in download paths. |
| ART-014 | P2 | Download an entry from inside an archive. |
| ART-015 | P2 | Folder or repository download as an archive. |
| ART-016 | P2 | Download the artifacts of a build as an archive. |
| ART-017 | P2 | Server-side download without transferring content. |
| ART-018, ART-019 | P2 | User-created time-limited signed download URLs and key rotation. |
| ART-020 | P2 | Filtered (templated) resources. |
| ART-030 | P2 | Last-modified lookup in a subtree. |
| ART-035 | P2 | Bulk version deletion dialog. |
| ART-039 | P2 | Purge of redundant trash-can folder records; the trash design here keeps no such records. |
| ART-043 | P2 | Public repository view under a path prefix. |
| ART-044 | P2 | Browsing uncached remote content. |
| ART-045 | P2 | Content browsing of files and archives (would need a separate content origin). |
| ART-046 | P2 | Followers (change notifications). |
| ART-047 | P2 | WebDAV access. |
| ART-051 | P2 | Background task list (job API in [architecture.md](architecture.md)). |
| ART-052 | P2 | RubyGems version list. |
| ART-053 | Out of scope | Push to a discontinued external distribution service. |
| RET-002 | P2 | Project-scoped policies managed by a project's Owner; retention policies are instance-wide. |
| RET-024 | Later | Release-set cleanup policy. |
| RET-025 | Later | Cleanup and archiving on federated repositories. |
| RET-026 | P2 | Retention and push replication. |
| RET-027 – RET-034 | Later | Package archive tier, archive pairing, search, restore, reports, overview, logging, artifact-level archive. |
| RET-039 | Later | Release-set retention on receiving instances. |
| RET-040, RET-041 | Out of scope | License-tier gating; portal provisioning of hosted archive instances. |
| RET-042 | P2 | Search-driven and scripted custom cleanup. |

</details>

## Security notes

- **No user input reaches object keys.** Keys derive only from a SHA-256 the server computed
  (R-STOR-001), so path traversal or key injection into the bucket is impossible by construction.
- **Deploy by checksum requires read access to existing content and SHA-256.** Without the read
  check, knowing a digest would be enough to copy content into a repository the attacker can
  read. Without the SHA-256 restriction, a crafted SHA-1 collision could bind a victim's path to
  attacker-chosen bytes. Unknown and unreadable content answer the same `404` (R-STOR-008).
- **Stored content is served as an attachment in a sandbox.** Anyone with `deploy` can upload HTML
  or SVG; served inline from Quader's origin it would run script with the session of whoever
  opens it (the UI session cookie is same-origin). `Content-Disposition: attachment`, `nosniff`,
  `Content-Security-Policy: sandbox; default-src 'none'` and
  `Cross-Origin-Resource-Policy: same-site` close that path; directory indexes escape every name
  (R-STOR-012, R-STOR-017). The core sets these headers in a writer wrapper that no format or file
  handler can weaken ([architecture.md](architecture.md) R-ARCH-007), so a handler that forgets them
  does not reopen the path. A proxy that strips response headers, or a browser that ignores both
  `attachment` and the sandbox, still does; that residual is H-3 in
  [architecture.md](architecture.md), which the optional content origin (R-ARCH-029, P1) narrows.
- **The bucket is a trust boundary.** Principals with access to the bucket read every blob of
  every repository, bypassing Quader's permissions. The IAM policy of R-STOR-031 grants Quader
  only its prefix; operators keep S3 Block Public Access on, require TLS (`aws:SecureTransport`
  in the bucket policy) and grant no other principal access to the prefix. Residual risk: H-11.
  With several connections every connection's bucket is such a boundary, at every provider
  that holds one (R-STOR-050); the security page of the connections
  ([object-store-connections.md](../security/object-store-connections.md)) has what that adds.
  An encrypted connection moves the boundary for content to the key ring: its bucket's readers see
  ciphertext, and every pod that holds the key ring opens every blob (R-STOR-075, R-STOR-083;
  [stored-content-keys.md](../security/stored-content-keys.md)).
- **Credentials.** S3 credentials come from workload identity or from mounted files, never from
  inline configuration (R-STOR-031); the outbound proxy password likewise (R-STOR-034). Static
  keys are the weakest mode and need rotation by the operator.
- **Spool files contain unencrypted uploads** on the pod's scratch volume for the duration of a
  request. They are created `0600`, removed on every exit path and wiped at startup
  (R-STOR-004). With a generic ephemeral volume, the files of a killed pod stay on the released
  volume unless its storage class deletes it (`reclaimPolicy: Delete`,
  [ha-and-helm.md](ha-and-helm.md) R-HA-048). Node-level disk encryption is the platform's
  responsibility; the residual risk is H-2 in [architecture.md](architecture.md).
- **Upload sessions are bound to their principal.** Any replica continues a session (R-STOR-011),
  so the session ID, which appears in URLs and logs, must not authorize anything by itself. Every
  request of a session is checked against the session's principal and authorized for `deploy`
  again; another principal gets the same `404` as for an unknown session.
- **A delete job acts only while its caller could.** The job of a large delete stores who asked and
  with which credential, never a grant, and decides every batch again on what that caller holds then
  (R-STOR-018, Q-STOR-190): a revoked token, a withdrawn session, a disabled principal or a group the
  caller left stops it within `auth.revocation_bound`, and records `artifact.delete_stopped`. It
  holds its repository by ID, so a repository created under a moved one's old name is never touched
  (Q-STOR-191). The batch in flight at that moment still finishes (H-658 in
  [file-transfer.md](../security/file-transfer.md)).
- **Integrity.** Uploads are verified against declared digests and, where the store supports it,
  by the store against the payload checksum (R-STOR-006); full downloads are verified before the
  last bytes leave (R-STOR-013); the scrub reads every copy whole once per period and catches
  damage at rest (R-STOR-027), and the reconciliation replaces a damaged copy from another
  connection (R-STOR-056).
- **Mass deletion is privileged and observable.** Retention policies, trash purge, garbage
  collection and the orphan sweep are system-administrator operations; policies start disabled
  and can be dry-run; every run, trigger and removal is audited (INV-16).
- **Download statistics are personal data.** `last_downloader` links a principal to a download;
  it is cleared when the principal is erased (R-STOR-015).

## Decisions

Each question below was decided by the development agent from the listed options; the owner reviews the choices at the walkthrough.

### Q-STOR-1 · Readiness tied to object-storage reachability (INV-12)

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Revisited on 2026-10-28 after the owner added the Valkey, horizontal-scale, backup and storage-class constraints: the choice stays (d). The rationale now covers a node drain during an object-store outage and states that Valkey plays no part in this rule; no requirement text changed.

All replicas share one bucket. If readiness follows S3, an S3 outage makes every replica unready at
the same moment and the Service loses all endpoints.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Keep INV-12 as written | Readiness fails once S3 checks have failed for a while, with hysteresis | Literal invariant; traffic leaves a replica whose own path to S3 is broken | A shared outage turns into a total outage: UI, API, logins, CI token exchange, metadata, deletes and deduplicated uploads go down with blob traffic, although R-STOR-036 could serve them |
| (b) Process and PostgreSQL only | Readiness ignores S3; the circuit breaker fails blob requests individually; metrics and alerts report S3 health | Degraded service instead of a total outage; nothing to coordinate | A release with a wrong endpoint, network path or CA, whose errors look transient, becomes ready and replaces every working pod, breaking all blob traffic; no built-in way to take an installation out of an outer load balancer that could fail over to a second installation |
| (c) Fail only the odd replica | Readiness fails on S3 only when this replica fails while others succeed, compared through PostgreSQL | Routes around one replica's broken network path | Cross-replica coordination for a case the circuit breaker and client retries already contain; undefined with two replicas that disagree |
| (d) (b) after start-up, (a) as opt-in | A replica becomes ready only after the startup check of R-STOR-035 passed; afterwards S3 failures degrade blob requests and raise alerts (`health.require_object_store: false`); `true` makes R-OPS-001's `objectStore` check gate readiness continuously | Default keeps the control plane and metadata up; a rollout with a broken S3 configuration still stalls; installations behind a cross-site failover can still drain an installation whose bucket is down | One more configuration branch to test; a replica that loses only its own S3 path stays in the Service and answers blob requests with a fast `503` until the alert is acted on; a replica whose process restarts during an outage (OOM kill, node loss) stays out of the Service until the store returns |

**Choice:** (d) — object storage gates only a replica's first readiness (through the startup
check); after that, running replicas stay ready during an S3 outage, and continuous gating is an
opt-in.

**Rationale:** Readiness exists to move traffic from a broken replica to a healthy one. With a
dependency that every replica shares there is no healthy replica to move to, so (a) only removes
the parts that still work, including the UI and API operators need to diagnose the outage. The
single-replica case that (c) targets is already visible through the alert
`QuaderObjectStoreUnreachableSingleReplica` ([ha-and-helm.md](ha-and-helm.md) R-HA-045), and
clients retry its `503` onto other replicas. Gating the first readiness keeps the one protection
(a) offers that matters: new pods with a wrong bucket, endpoint or credential never become ready,
so a rolling update stalls instead of replacing working replicas. The opt-in costs one boolean that
R-OPS-001 already defines, and it serves the one setup where continuous gating helps: a global
load balancer in front of two installations. Using R-OPS-001's thresholds for the opt-in
removes this page's second hysteresis mechanism. The cost of gating the first readiness is that a
replica restarted during an outage stays out until the store returns. The outage itself never
causes such a restart, because liveness never reads a dependency
([operations-and-observability.md](operations-and-observability.md) Q-OPS-1), so only an
unrelated crash or node loss during the outage costs that replica's capacity. The same decision
is recorded as Q-HA-1 in [ha-and-helm.md](ha-and-helm.md) and Q-OPS-2 in
[operations-and-observability.md](operations-and-observability.md).

The owner's drain constraint (INV-12) does not change this. A node drained during an S3 outage
evicts one pod per role; its replacement stays not ready, so the disruption budget
(`maxUnavailable: 1`, [ha-and-helm.md](ha-and-helm.md) R-HA-032, R-HA-059) refuses the next
eviction and the drain waits until the store returns. The outage costs each role at most one
replica of capacity, and the Service never loses its last ready endpoints. Under (a) the same
drain would find every replica already unready. Valkey is no input to this rule: its outage
degrades and never gates readiness (INV-12), and object-storage readiness reads nothing from it.
The development agent would revisit this if a deployment shows a replica-local S3 failure that the
circuit breaker does not contain, or if drains during S3 outages stall maintenance windows often
enough to matter.

**Applied in the spec:** R-STOR-036: the readiness row now states first-readiness gating, the
default and the opt-in; the keys `storage.s3.readiness.fail_after` and `recover_after` are removed
(here and in the configuration reference); an acceptance criterion checks `/readyz` during an S3
outage with both settings and for a replica started during the outage. R-STOR-035: the startup
check keeps retrying transient errors without exiting, so a replica started during an outage waits
not ready instead of crash-looping, and becomes ready as soon as the store returns rather than after
a restart back-off; `storage.s3.startup_timeout` is removed (here and in the configuration
reference) and an acceptance criterion covers the case. This changes INV-12; the edit to
[README.md](README.md) is listed as a cross-page consequence. The revisit of 2026-10-28 changed
no requirement text.

### Q-STOR-2 · SHA-512 for every blob or only on demand

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

INV-4 says SHA-512 "where a format needs it".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every blob | SHA-512 is computed in the single hashing pass of every upload and stored on the blob row | Both MVP formats need it: npm's `dist.integrity` is a `sha512-…` SRI string, and [formats/nuget.md](formats/nuget.md) verifies remote packages by their SHA-512 hash; content first stored through one format is complete when another format references it; `Repr-Digest: sha-512` and `.sha512` sidecars work everywhere | One more hash state per upload and 64 bytes per blob row |
| (b) On demand | Computed only when the format plug-in asks for it; filled later from S3 when another format needs it | Saves the SHA-512 pass for generic content | Needs a backfill job that re-reads objects from S3 (the machinery STOR-005 describes) and a synchronous re-read when a deduplication hit lacks the digest; two code paths |
| (c) Format-owned only | No SHA-512 on the blob row; formats keep it in their own tables | Smaller blob row | Sidecars, `Repr-Digest`, deploy-by-checksum verification and cross-format reuse lose their single source; each format derives the digest itself |

**Choice:** (a) — every upload computes and stores SHA-512 alongside SHA-256, SHA-1 and MD5.

**Rationale:** With npm and NuGet first, nearly every upload needs SHA-512 anyway, so (b) would save
almost nothing and still require the backfill path. Deduplication makes "the format that uploaded
first" arbitrary: a tarball first stored through a generic repository and later referenced by npm
must not trigger a re-read from S3. NuGet's version tombstones also compare SHA-512
([formats/nuget.md](formats/nuget.md) R-NUGET-006). The extra hash runs in the same pass as the others. CPU time is
small next to network and S3 latency. The development agent would revisit this only if profiling
shows uploads to be CPU-bound.

**Applied in the spec:** No change; the requirement text already follows this option (R-STOR-002,
R-STOR-004, `blobs.sha512 NOT NULL` in R-STOR-003). This refines INV-4 rather than contradicting
it; the alignment of INV-4's wording is listed as a cross-page consequence.

### Q-STOR-3 · Owner of build records

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

RET-023 and RET-037 (P1) need build records (catalog `BLD`), but no spec page owns them.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) New page `builds.md` | A spec page with prefix `R-BLD` owns build records, their API and their lifecycle; this page keeps only the cleanup mechanics (R-STOR-043) | One owner per model; matches the conclusion of [authorization.md](authorization.md) (Q-AUTHZ-8) and [api-and-ui.md](api-and-ui.md) (Q-API-5); storage needs only a narrow contract | One more page to write before M9 |
| (b) This page | storage.md also specifies build records | No new page | Mixes two subjects; publishing, promotion and CI integration have nothing to do with blob storage |
| (c) Defer | RET-023 and RET-037 move to P2 together with build metadata | Less P1 scope | Leaves 15 P1 `BLD` entries and two P1 `RET` entries unmet |
| (d) In [gitlab-ci-oidc.md](gitlab-ci-oidc.md) | GitLab CI is the main producer, so its page owns build records | Close to the main producer | Ties a format-neutral model to one CI system and to an authentication page |

**Choice:** (a) — build records get their own page `builds.md` (`R-BLD`); R-STOR-043 stays here
and is written against a fixed contract.

**Rationale:** Build records are a resource model of their own with publishing, listing,
permissions (R-AUTHZ-027) and later promotion. Every page that met the question reached the
same answer, so one page avoids three partial owners. Storage needs only four facts from it:
name, number, start time, and the produced files as repository, path and SHA-256. The prototype is
not blocked, because builds belong to milestone M9. The development agent would revisit this only
if the builds page turned out to be no larger than the contract, which the 15 P1 `BLD` entries
rule out.

**Applied in the spec:** R-STOR-043 names the owner and states the contract `builds.md` has to
provide, including that build records live in PostgreSQL tables and not as items of a repository,
so no package policy, path cleanup or repository delete can reach them (the hazard the catalog
describes under RET-037). Creating `builds.md` and adding it to the page table is a cross-page
consequence.

### Q-STOR-4 · Cron dialect shared by all schedules

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Storage tasks, retention policies and the maintenance tasks of
[operations-and-observability.md](operations-and-observability.md) all take schedules. The
catalog's dialect (RET-011) has a seconds field and `?`, and leaves open whether the day of week
counts 0–6 from Sunday or 1–7 with Sunday = 1.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Five-field cron in UTC | POSIX/Kubernetes fields, day of week 0–6 with Sunday = 0 or names, the Kubernetes macro set; strict rejection of other dialects' syntax | Operators read it from Kubernetes CronJobs; one parser; UTC has no skipped or doubled runs at daylight-saving changes | No local-time maintenance windows |
| (b) Six-field with seconds and `?` | The catalog's dialect | Expressions written for that dialect paste unchanged | Seconds precision has no use for jobs bounded to six hours or more; unfamiliar to Kubernetes operators; day of week 1 = Sunday conflicts with every crontab |
| (c) (a) plus a time zone | Same syntax, evaluated in a configurable zone | Local-time windows | Daylight-saving gaps and repeats need rules; can be added later without breaking (a) |
| (d) Accept both (a) and (b) | Dialect chosen by field count | Migration convenience | The dialects number weekdays differently (1 is Monday in one, Sunday in the other), so a pasted expression silently runs on the wrong day: the very ambiguity RET-011 leaves open |

**Choice:** (a) — five-field cron in UTC with the Kubernetes macros; `?`, `7`, six-field forms,
`@every` and time-zone prefixes are rejected.

**Rationale:** One parser for every schedule, in the syntax Kubernetes operators already read
(Q-OPS-7 in [operations-and-observability.md](operations-and-observability.md) decided the same
and points to R-STOR-025 as the normative dialect). Kubernetes itself accepts `?` as `*`; Quader
rejects it on purpose. Strict rejection turns the dangerous case of (d), an expression
from a 1 = Sunday dialect, into a loud `422` instead of a cleanup on the wrong day. UTC avoids
daylight-saving runs that are skipped or doubled. Option (c) can be added later as a separate field
without changing stored expressions. The development agent would revisit this if operators need
local-time maintenance windows, and would then add (c).

**Applied in the spec:** R-STOR-025 now contains the normative schedule syntax: fields and names,
POSIX day matching, macros, rejected forms with the error code `invalid_schedule`, never-firing
expressions, and a parser test table. R-STOR-040 refers to it and fixes the RET-011 ambiguities:
day of week 0–6 with Sunday = 0, and the 6-hour minimum checked over four years of fire times.

### Q-STOR-5 · Does overwriting a file send the previous content to the trash can?

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Yes, for user-deployed files | In local repositories with the trash can enabled, the replaced item goes to the trash can; generated metadata is replaced without trash | `overwrite` gets the same safety net as `delete`; a CI job re-publishing a release path is recoverable | Trash volume in repositories that overwrite routinely, bounded by the retention period |
| (b) No | Overwrite replaces in place; only deletes go to the trash can | No trash volume from overwrites | The one destructive action the trash can does not cover |
| (c) Per-repository switch | `config.storage.trashOnOverwrite`, default on | Lets a repository that overwrites large files opt out | A setting whose only use is to turn protection off; more surface for a case not yet observed |

**Choice:** (a) — an overwrite of a user-deployed file in a local repository sends the previous
item to the trash can when the trash can is enabled.

**Rationale:** An overwrite destroys the previous content exactly like a delete, and the typical
accident is a pipeline publishing to an existing release path. For the MVP formats the cost is
small: npm and NuGet versions are immutable (a re-publish answers `409`, R-NPM-005 and
[formats/nuget.md](formats/nuget.md)), so overwrites happen mainly on raw and generic paths. (c)
can be added without changing (a)'s default. The development agent would revisit this if a
repository that overwrites large files routinely makes the trash volume a measurable cost.

**Applied in the spec:** No change to the normative text; R-STOR-005 and R-STOR-022 already follow
this option. R-STOR-022 gains an acceptance criterion that restores overwritten content from the
trash can.

### Q-STOR-6 · Download verification on by default

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) On by default | Full streamed downloads are hashed and the last bytes are held back until the digest matches (R-STOR-013) | A client never receives a complete corrupted or tampered file | One SHA-256 pass per downloaded byte |
| (b) Off by default | Operators enable it for high-assurance installations | No CPU cost | Operators rarely know they need it until a corrupted artifact has spread |
| (c) Sampled or conditional | Verify only blobs whose last deep check is older than a threshold, or every n-th download | Lower CPU cost | Breaks the guarantee for exactly the blobs between checks, which is where undetected damage sits |

**Choice:** (a) — verification is on by default; `storage.download.verify: false` remains for
CPU-bound replicas.

**Rationale:** The cost is low: Go's `crypto/sha256` uses the CPU's SHA instructions on arm64 and,
since [Go 1.21](https://go.dev/doc/go1.21), on amd64 (3–4 times faster). That puts hashing well
above a replica's network throughput. The benefit is a guarantee that (c) gives up exactly where
it matters. A default-on check is only acceptable if it cannot hurt availability: a false
`corrupt` makes R-STOR-012 answer `500` for that content until it is repaired, so only evidence
about the stored object (a wrong S3 length or a complete read with the wrong digest) may mark a
blob, never a connection reset or timeout. Ranged downloads and presigned redirects stay
unverified whatever the setting (H-11). The development agent would revisit this if profiling
shows downloads to be CPU-bound on hardware without SHA instructions; the switch exists for that.

**Applied in the spec:** R-STOR-013 keeps `verify: true` as the default and now separates the two
outcomes of a failed check: the response is always aborted, but only a wrong S3
`Content-Length` or a complete read with the wrong SHA-256 marks the blob `corrupt`; transport
errors leave the status unchanged, with an acceptance criterion using a fault proxy.

### Q-STOR-280 · The switch is one row of its own with its own operations, as the malware scan's

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the owner decided one switch of the installation in the admin area (R-STOR-014). The installation has two places for such a
setting: the configuration, which a rollout changes, and a row in the database with an API, which the malware scan's "Only checked
artifacts can be downloaded" uses (R-MAL-019) and every replica reads within 2 s. A document of storage settings (`trash`, `quota`,
`PUT /api/v1/settings/storage`, R-STOR-022) is specified and not built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One row `download_link_settings`, operations under `/api/v1/download-links/`, a change area of its own (chosen) | The precedent of R-MAL-019 | An administrator's operational switch changes without a rollout and reaches every replica within the bound; the pieces exist once already (snapshot, change area, `If-Match`, audit with journal flag) | A table and four operations more |
| (b) A member `links` of the storage settings document | One document for storage | One page, one revision | The document is not built; `PUT` replaces the whole document, so the links' package would build the trash can's and the quota's machinery with it |
| (c) Configuration keys, as `storage.s3.presign.*` are today | The chart decides | Nothing to build at runtime | Not the admin area the owner chose; every change is a rollout |

**Choice:** (a). **Rationale:** it is what the owner asked for and what the installation already does for the one comparable switch. The
planner would revisit it when the storage settings document is built and a second runtime setting of the delivery appears.
**Applied in the spec:** R-STOR-014, R-UI-200, [Q-API-400](api-and-ui.md#q-api-400--the-signed-links-have-operations-of-their-own-under-apiv1download-links).

### Q-STOR-281 · The public address for links is a setting of the admin area per connection

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the owner listed "öffentlicher Host" among the details of the switch in the admin area. Connections are kept in the chart and
shown, not changed, in the admin area (decision 9 of the same paper). Today the address is the configuration key
`storage.s3.presign.endpoint` ([storage.go](../../internal/config/storage.go) lines 110 to 115). A link must name a host the client
reaches, which for a store inside the cluster is a name in front of it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A field per connection in the switch's details, its value the connection's endpoint until changed (chosen) | Stored in `public_addresses` of the row, keyed by the connection's name | The owner's wording; an address the administrator can test before he saves it (R-STOR-074); no rollout | A value about a connection that lives outside the chart; each replica checks it against its own configuration |
| (b) A property of each connection in the chart, shown read-only | As today's key | The connection's facts in one place | Not what the owner chose; a rollout per change |
| (c) Both, the admin area overriding the chart | | Either way works | Two places for one value, which drift |

**Choice:** (a). **Rationale:** the address is the delivery's, not the connection's: the connection itself is reached by its endpoint,
and only links use the address. The planner would revisit it if stage 1 makes the connections changeable in the admin area.
**Applied in the spec:** R-STOR-014, R-STOR-074, R-UI-200.

### Q-STOR-282 · A repository opts out with one member, and the addressed and the holding repository must both allow

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: "Je Repository abschaltbar" (decision 11 (a)). A download through a virtual repository involves the addressed repository and
the member that holds the item; the old R-STOR-014 asked only the member, the malware scan asks either (Q-MAL-32). The malware scan
treats switching its member off as a weakening an Owner may not make (R-MAL-020).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `storage.downloadLinks`, default `true`; both the addressed and the holding repository must allow; every writer of the settings both ways (chosen) | An opt-out that only takes away | A team's virtual repository and a member's own choice both hold; the Owner who allows links again returns to the administrator's decision and cannot go beyond it | One member more |
| (b) Only the holding member decides | As the old block | The repository that stores the bytes decides | A team cannot keep its virtual repository's downloads in Quader |
| (c) Switching links back on needs `repositories:write-config` | As the malware scan's weakening | An administrator controls every repository that delivers by link | The installation's switch is that control already; the conditions of R-STOR-070 hold whatever the repository says |

**Choice:** (a). **Rationale:** the opt-out narrows a delivery the administrator has already allowed under conditions that keep the
content checked; the repository cannot widen it. The planner would revisit (c) if a link of one repository turns out to carry a risk the
installation's conditions do not cover. **Applied in the spec:** R-STOR-014, R-STOR-070 step 4, R-UI-201.

### Q-STOR-283 · The member storage.presignedDownloads is refused, and storage is a closed object

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: `storage.presignedDownloads` is accepted and ignored today (R-STOR-014's earlier form; [validate.go](../../internal/repository/validate.go)
lines 141 to 148). The project is pre-alpha with no installation to keep working (the repository's working rules). Neither the OpenAPI
document nor the UI knows the member.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `storage` holds `maxUploadSize` and `downloadLinks`; any other member is `422 invalid_value` (chosen) | A write that names the old member is refused with a detail that names R-STOR-014 | No accepted setting is ignored; a typo is found at once | A stored document with the old member is refused when it is sent back whole |
| (b) Drop unknown members on write | The canonical form | Nothing is refused | The writer is not told that his member means nothing |
| (c) Map `presignedDownloads: true` to the new member | | | A compatibility path the rules forbid, and the meaning is the opposite (opt-in against opt-out) |

**Choice:** (a). **Rationale:** the rules of the pre-alpha stage and the principle that a setting is either honoured or refused. No
stored document is rewritten; one sent back whole with the old member is refused at its pointer, and its writer removes it.
**Applied in the spec:** R-STOR-014.

### Q-STOR-284 · A download whose gates are not all decided and passed is never a link

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: a link cannot be taken back during its life. Where the malware gate is not strict, a blob without a verdict yet is served
(R-MAL-013); its verdict usually follows within minutes of the upload. Streamed, a request after an `infected` verdict is refused; a link
issued before it still delivers the bytes until it expires.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A link only on decided passing states: malware `clean` or released or off, no block or wait of the vulnerability data, the signature gate passed or off (chosen) | A pending, `unscannable` or `failed` blob streams | A link never hands out bytes whose verdict is still open; fresh content, the most likely to be found infected, stays behind Quader | Fresh large artifacts stream until their scan |
| (b) A link wherever the gates let the download through | Non-strict pending blobs go by link | More downloads by link | A finding minutes after the upload does not reach links issued before it |
| (c) No link while the malware scanning is on at all | | | The owner's switch would mean nothing on an installation that scans |

**Choice:** (a). **Rationale:** it bounds what H-1275 can deliver to content whose verdicts were in when the link was issued. The planner
would revisit it if most large downloads turn out to be of blobs younger than their scan. **Applied in the spec:** R-STOR-070 step 6.

### Q-STOR-285 · Quader answers every request with a range itself

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: a link signed for `GET` can carry a client's `Range` to the store (the clients copy it on a redirect). Quader's own answer to
`If-Range` compares the content address (R-STOR-012); the store's `ETag` is not the content address, so an `If-Range` sent there never
matches and the store answers the whole object. A client's digest covers the whole object only.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Any `Range` is answered by Quader (chosen) | `206`, `200`, `416` as R-STOR-012 | Resume on any replica stays exact; the first transfer of a large file, the one that matters, goes by link | A resumed transfer goes through Quader |
| (b) A link for a range without `If-Range` | | More bytes by link | Two rules for one request kind; a store's support of ranges on a link is not documented at every provider |
| (c) A link for every range | | Most bytes by link | `If-Range` turns every resume into a full transfer |

**Choice:** (a). **Applied in the spec:** R-STOR-070 step 8.

### Q-STOR-286 · The declaration is a field of the route

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: whether a client checks a digest is a fact of one route (an OCI blob is checked against the digest in its path, a manifest by
tag is not). The contract keeps facts of a route on `formats.Route` (`APIKey`, `TokenScheme`, `Wiki`, [route.go](../../internal/formats/route.go)
lines 99 to 140) and format-wide facts in `catalog.Facts` (the Bastion's facts, ADR-238), and the serve point knows the matched route
(`rq.Route`, [ingest.go](../../internal/formathost/ingest.go) lines 427 to 429).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `Route.ClientDigest` with the evidence beside it (chosen) | One field per route | The fact stands next to the route it describes; the serve point reads it from `rq.Route`; the zero value streams | One more field every route carries |
| (b) A list of route names in `catalog.Facts` | Format-wide facts | The catalog is a leaf every layer reads | A name list that can drift from the routes; the catalog has no notion of routes |
| (c) A method of the format that answers per request | | Can look at a request | Code where a fact suffices, and the kit cannot list what a format declares |

**Choice:** (a). **Applied in the spec:** R-STOR-071, [ADR-243](../decisions.md#adr-243--a-route-declares-whether-its-clients-check-a-digest-and-kit-check-20-holds-it).

### Q-STOR-287 · A link's answer carries the content type, the file name and no-store, and the redirect carries the four content headers

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: R-STOR-012 requires four headers on every response that carries stored content; the earlier R-STOR-014 named two for the link
(`response-content-disposition`, `response-content-type`). A signed link can ask the store only for the `response-*` headers;
`X-Content-Type-Options`, `Content-Security-Policy` and `Cross-Origin-Resource-Policy` cannot be set on it. Their purpose is that user
content is never rendered as part of Quader's origin.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The link asks for content type, `attachment` with the file name and `private, no-store`; the `307` carries all four (chosen) | R-STOR-012 holds for every answer Quader writes | The store's answer comes from a host that is never Quader's or below it (Q-STOR-288), so there is no origin of Quader to protect there; the stored type `application/octet-stream` is not rendered | Three headers are missing on the store's answer (H-1279) |
| (b) No link unless all four can be set | | R-STOR-012 holds word for word | No store can set them: the switch would mean nothing |
| (c) Only the two of the earlier block | | Less to sign | A shared cache could keep a bearer link's bytes |

**Choice:** (a). **Applied in the spec:** R-STOR-072, R-STOR-074 (the link check proves the store honours the three).

### Q-STOR-288 · The links' host is no host of Quader and lies below none

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: read in the clients' source on 2026-11-22: Go's HTTP client keeps `Authorization` on a redirect to the same host or a subdomain
of it, and containerd and Podman do not strip it; an object store refuses a signed link that arrives with a credential (`400`). Quader
answers on the hosts of its base URLs, its endpoints' host sets, their wildcard hosts and the host names its endpoints bind
(`server.base_url`, `additional_base_urls`, `content_base_url`, `endpoints.*`, [server.go](../../internal/config/server.go) lines 28 to 38,
[endpoints.go](../../internal/config/endpoints.go) lines 62 to 65).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Refuse a link host that is, or lies below, a host of Quader or below a wildcard's suffix, at save and at every start (chosen) | The host as signed, the bucket's name included where the addressing puts it there | Closes the case that breaks every image pull by link | A host Quader is reached by and its configuration does not list is not seen (H-1282) |
| (b) Refuse only equal hosts | | Simpler | The subdomain case, the one the Go clients meet, stays open |
| (c) No rule, a note in the manual | | | A misconfiguration that fails every pull with an error from the store |

**Choice:** (a). **Applied in the spec:** R-STOR-072, R-STOR-074.

### Q-STOR-289 · The redirect is a 307

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the earlier block said `302`. The reference registry and Docker Hub and ghcr.io answer `307`, quay.io `302`; every client read
follows both for a `GET` (apt, npm, Go, curl, .NET, Python's `urllib`).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `307 Temporary Redirect` (chosen) | The method and body stay | What the reference registry sends; its meaning is exact for every method | None found for `GET` |
| (b) `302 Found` | | The earlier text | A method change for other methods is allowed by its definition |
| (c) `303 See Other` | | | Says the target is another resource, which it is not |

**Choice:** (a). **Applied in the spec:** R-STOR-072; [formats/oci.md](formats/oci.md) R-OCI-010 already names `307`.

### Q-STOR-290 · A link lives 60 seconds unless the administrator says otherwise, at most an hour

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: a link is a bearer for its life (H-12). Clients follow a redirect at once; a link that expires during a transfer lets it finish
(the store checks the expiry on arrival); a retry asks Quader again. Signature Version 4 allows up to 7 days; temporary credentials end
earlier (STS one hour by default; at the owner's provider, 48 hours on its own servers).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) 60 s in the field, 10 s to 3,600 s, bounded by the credential (chosen) | Today's configuration default | Short exposure; nothing a client needs is lost | A client that queues a redirect for more than a minute fetches a new link |
| (b) 20 minutes, as the reference registry | | Fewer new links on slow starts | Twenty minutes of a bearer |
| (c) Up to 7 days | | | A link becomes something to pass around |

**Choice:** (a). **Applied in the spec:** R-STOR-014, R-STOR-072.

### Q-STOR-291 · Any connection that is not encrypted and holds a sound copy serves the link

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: with several connections (stage 1) a blob has a copy on each; Quader reads in an order and falls over. The owner: an encrypted
connection's data is delivered by Quader. A plain copy of the same bytes may exist on another connection (the owner's datacenter store in
the clear beside an encrypted provider, the paper's example).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The first connection in the read order that is not encrypted, holds a copy not marked damaged and is not failing (chosen) | A link from the plain copy where there is one | The plain copy is in the clear anyway; the encrypted provider's bytes never leave by link | A link can come from another connection than Quader would read from |
| (b) Only the connection Quader would read from, and only if it is not encrypted | | One connection per blob in every path | An installation whose first connection is encrypted would never use links |

**Choice:** (a). **Applied in the spec:** R-STOR-074.

### Q-STOR-292 · A link is recorded as the download it stands for

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the object store's log names the signer and records no issuance; the store vendor recommends the application's own record of
each issuance. Quader audits downloads only with `audit.record_downloads`, because of their volume; the request log records every
request.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `artifact.downloaded` with `delivery` and `linkExpiresAt` under `audit.record_downloads`; the request log's `delivery` for every link (chosen) | A link is a download | One rule for downloads; every issuance is in the request log with the caller, the path and the time | An installation that audits no download has the request log only |
| (b) An event `artifact.link_issued` always | | Every bearer handed out is in the audit trail | Every layer of every pull in the audit trail, the volume `record_downloads` exists to avoid |

**Choice:** (a). **Applied in the spec:** R-STOR-073.

### Q-STOR-293 · A link is signed by the connection's runtime credential

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: a signed link allows exactly the signed `GET`, whatever else its credential may do; it stops working when its credential is
revoked. At the owner's provider credentials are scoped to a project, not a bucket, and can be held to reading by a bucket policy.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The runtime credential of the connection (chosen) | Nothing to configure | The plain path; what the store signs today | Revoking every link at once means revoking Quader's credential |
| (b) A read-only credential per connection for signing only | A second login per connection | A lever that ends every outstanding link without stopping Quader; a bucket policy can narrow the signer alone | One more secret per connection in the chart, for a lever needed rarely |

**Choice:** (a). **Rationale:** a link lives a minute by default; the lever matters only for longer lives. The planner would revisit it
when an administrator needs links of more than a few minutes. **Applied in the spec:** R-STOR-072, H-12.

### Q-STOR-294 · Each connection proves at start that a link with the overrides works

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the owner's provider documents signed links for `GET`, and says nothing about their longest life, about ranges and about the
`response-*` overrides; the software behind it documents the overrides as supported (read on 2026-11-22, not confirmed for the provider,
and nothing can be run against his bucket today). A store that ignores the overrides would serve a link without `attachment` and with the
stored type.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) An optional check at every start of each connection, and when the switch is turned on; a failure takes the connection out of links (chosen) | The conformance check `checkPresign`, extended | Every provider is found out on the installation that uses it, without a measurement in the pipeline | One probe write and one `GET` per connection and start |
| (b) The conformance job in the pipeline only | | Nothing at runtime | The pipeline runs against Silo, never against the owner's provider |
| (c) Trust the documentation | | | Not documented for the provider that matters |

**Choice:** (a). **Applied in the spec:** R-STOR-074.

### Q-STOR-295 · Kit check 20 judges every 200 a declaring route answers to a GET, against the stored blob, and a skip while a route declares is refused

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-071 asks of a declaring route "a content case it serves with `200` and a body, through `Items().Serve`", whose `ETag`
is the content address of the body, "so the bytes a client checks are the stored blob's". Open were whether one such case is enough when
the route has others, what "through `Items().Serve`" is measured by, whether the body is compared with the stored blob or only with its
`ETag`, and whether the check may be skipped. Read on 2026-11-22: kit check 16 already holds every `GET` of a `Download` route that a case
serves to the admission of `Items().Serve`, and check 19 every such answer to a content-address `ETag` equal to its body's SHA-256, unless
`NoContent` or `NoBastion` lists the route.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every `GET` case a declaring route answers `200` is held: the in-memory host recorded the item `Items().Serve` served, a body, a content-address `ETag` equal to the body's SHA-256, and the body's SHA-256 is the one of a stored item the request served; at least one such case per declaring route; a skip of the check is refused while a route declares; `NoContent` and `NoBastion` do not reach it (chosen) | Every answer the serve point could hand to a link is judged as the link would deliver it | A declaration cannot rest on one good case beside answers that are not the stored blob; the exemptions of checks 7, 16 and 19 cannot carry a declaration | A route only remote or virtual repositories serve cannot declare, since the kit's cases run on the fixture's class (local unless it says otherwise) |
| (b) One case per declaring route suffices, the others are not judged | The specification's minimum | Fewer failures to read | A route whose other answers are rendered by the format would declare all the same |
| (c) The `ETag` alone, as check 19 judges it | | Nothing new to compute | An answer the format changed on its way out under a matching `ETag` passes, and a link would deliver the stored object instead |

**Choice:** (a). A format that declares nothing passes the check without a skip, as NuGet, Debian, the ClamAV definitions and generic
do. **Applied in the spec:** R-STOR-071; code: kit check 20 ([checks_links.go](../../internal/formats/formattest/checks_links.go)).

### Q-STOR-296 · Each defect of check 20 takes the exemption another check grants its route, so that it fails check 20 alone

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-071 asks for the defects `DeclaresUnservedBody` and `DeclaresUnaddressedBody`, each failing check 20 "and no other
check". Measured on 2026-11-22 with the test format's unchanged fixture: a declaring tarball route refused in a local repository fails
checks 7, 16, 17, 19 and 20; one written by the handler itself (`WritesOwnDownload`) fails 16, 17, 19 and 20; one answered with a revision
as `ETag` fails 19 and 20. Every way a declaring route can miss check 20 is one that checks 16 and 19 already catch, unless the route
carries the exemption those checks grant: `NoContent` (a route whose cases only refuse) or `NoBastion` (an answer without a content
address by design).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each defect is a format variant, and its test gives it the fixture entry such a format would carry: `DeclaresUnservedBody` refuses the tarball `501` in a local repository and is listed in `NoContent` with cases that expect it; `DeclaresUnaddressedBody` answers the tarball with a revision as `ETag` and is listed in `NoBastion`. The test format declares nothing by default; the option `DeclaresClientDigest` is the conforming variant (chosen) | The two tests show the one thing check 20 adds: the exemptions of the other checks do not carry a declaration | The test format and its fixture stay as they are; the defects model a remote-only route (as Ansible Galaxy's role archive) and a route without a content address | The defect alone, with the unchanged fixture, fails checks 19 or 7, 16 and 17 as well |
| (b) A route of its own in the test format for the defect (a `Download` route only the remote class serves, listed in `NoContent` in the base fixture) | One more route every check of the kit runs over | The unchanged fixture shows check 20 alone | A route that exists only for one defect; every kit test of the test format serves it |
| (c) The defects fail check 20 together with checks 16, 17 and 19 | | No fixture entry in the test | Contradicts the specification's "and no other check"; the test cannot show what check 20 adds |

**Choice:** (a). `TestR_STOR_071_KitCheck20` runs each defect with its entry over every check but check 6 and asserts that check 20 fails
alone, and that check 20 is not skipped while a route declares. **Applied in the spec:** R-STOR-071; code:
[testformat](../../internal/formats/formattest/testformat/format.go), [format_test.go](../../internal/formats/formattest/testformat/format_test.go).

### Q-STOR-7 · Status code for an exceeded quota

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `507` for quotas, `413` for size | `507 Insufficient Storage` for the system capacity limit and repository quotas; `413` only for the per-request size limit | The status line names the cause; a NuGet push failure is reported as status code and reason phrase, so `507 (Insufficient Storage)` is the message the user sees; one code per cause on every endpoint | `5xx` invites client retries: npm retries non-streamed `PUT` requests on any status ≥ 500 ([make-fetch-happen](https://github.com/npm/make-fetch-happen/blob/main/lib/remote.js)), by default twice, after 10 s and 60 s ([`fetch-retries`, `fetch-retry-*`](https://docs.npmjs.com/cli/using-npm/config#fetch-retries)), so `npm publish` reports the failure after about 70 s; NuGet retries every `5xx` with exponential backoff ([HttpRetryHandler](https://github.com/NuGet/NuGet.Client/blob/dev/src/NuGet.Core/NuGet.Protocol/HttpSource/HttpRetryHandler.cs)) |
| (b) `413` for both | Quota and size limit answer `413 Content Too Large` | Not retried by clients | Misdiagnoses the cause: a NuGet user sees "Request Entity Too Large" and shrinks a package that is not too large |
| (c) `403` with a problem code | Quota exceeded is reported as a refusal | Not retried | NuGet shows "Forbidden" and sends the user into a permission investigation |
| (d) `507`, but `413` on npm endpoints | As (a), except that npm endpoints answer a quota with `413` and the R-NPM-005 body | npm fails at once, and still names the limit because it prints the body's `error` text whatever the status | One cause, two codes depending on the endpoint: metrics and alerts by status, the problem registry and `/files` disagree with npm; npm prints "413 Request Entity Too Large" in front of a text about a quota; every later format whose client retries `5xx` would ask for its own exception |

**Choice:** (a) — `507` for every quota and capacity limit, `413` only for the per-request size
limit.

**Rationale:** For dotnet and nuget.exe the status line is the only text a failed push shows: the
push calls `EnsureSuccessStatusCode` and prints no body. Go's HTTP server cannot set a custom
reason phrase, and HTTP/2 has none, so the code itself has to name the cause; that rules out (b)
and (c). npm prints R-NPM-005's `error` text whatever the code, and that text names the limit.
The cost of (a) is the retry delay on npm, about 70 s with default settings, plus a few seconds
on NuGet. A quota failure cannot succeed by retrying until an administrator frees space or raises
the limit, so the delay postpones a failure the user has to escalate anyway; it does not turn a
success into a failure. Each retry is rejected again at admission before the body is read,
whenever the length is declared; npm, which sends without `Expect: 100-continue`, still transmits
part of its publish document before the connection is closed. (d) removes the delay for one
client at the price of a second code for the same cause on every surface that counts status
codes. The development agent would revisit this if CI users report the npm delay as a problem;
the change would then be (d).

**Applied in the spec:** R-STOR-029 states the code split, says that a `507` carries no
`Retry-After`, names the client retries and their delay, uses the problem code `quota_exceeded`
(snake case as [api-and-ui.md](api-and-ui.md) R-API-009 requires; the same code on npm endpoints),
adds the problem members `quota`, `limitBytes` and `usedBytes`, and adds acceptance criteria for
`npm publish` and `dotnet nuget push`. The matching rows in the npm and NuGet error tables are
cross-page consequences.

### Q-STOR-8 · Copy and move target semantics

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Exact destination, merge | `target.path` is where the source ends up; an existing folder there is merged into; identical items count as unchanged | Idempotent: repeating a promotion yields the same tree and succeeds | A merge can mix old and new files in a folder (only additions; `overwrite: false` never replaces a file) |
| (b) Unix `cp` semantics | Copying folder `a/b` onto an existing `x/b` produces `x/b/b` | Familiar from shells | The result depends on state the caller may not know; the second run of a promotion script nests instead of updating |
| (c) Exact destination, existing folder is a conflict | Like (a), but the call fails when the target folder exists unless `merge: true` is given | Explicit about merging | A repeated promotion fails unless the flag is set; the protection it adds is already given by `overwrite: false` |

**Choice:** (a) — the target path is the exact destination, existing folders are merged, and
items with the same content count as `unchanged`.

**Rationale:** An API call cannot see whether the target exists at the moment it runs, so (b)
makes one request's result depend on hidden state. (c) adds a flag whose protection already exists,
because `overwrite: false` never replaces a different file during a merge. Counting identical items
as `unchanged` makes a repeated copy succeed, which is what promotion scripts need. The development
agent would revisit this if users ask for "fail when the target folder exists"; that can come as an
extra flag without changing the default.

**Applied in the spec:** R-STOR-019 adds the `unchanged` count, the `type_conflict` rule for file
against folder (not resolvable by `overwrite`), the requirement that target paths pass the target
repository's include and exclude patterns (R-REPO-005), and an acceptance criterion for a repeated
copy.

### Q-STOR-9 · One pattern syntax across retention and permissions

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Retention scopes (R-STOR-038) and permission policies ([authorization.md](authorization.md))
both match repository keys, paths and package names.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One matcher, owned by authorization.md | Repository selectors of R-AUTHZ-004; name glob and path pattern of R-AUTHZ-005 | A pattern copied between a permission policy and a cleanup policy selects the same things; one implementation and one test suite | Retention inherits authorization's rule that `*` does not cross `/` (scoped npm names need `@*/*` or `**`) |
| (b) Own syntax per subsystem | Retention defines its own globs | Free to tailor | Two syntaxes guarantee that a copied pattern silently matches something else; for a deletion feature, a silent mismatch deletes |
| (c) Prefix-only name patterns everywhere | Exact names or a trailing `*`, as [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-005 uses for routing | Simplest | Cannot express the version selections cleanup needs, such as `*-ci.*` or `*-SNAPSHOT` |

**Choice:** (a) — retention uses the repository selectors and patterns of R-AUTHZ-004 and
R-AUTHZ-005 through the same code.

**Rationale:** Operators write both kinds of pattern for the same repositories, and in retention a
pattern that matches more than intended removes content. R-AUTHZ-005's glob contains the prefix
form, so `@acme/*` and `Acme.*` mean the same in routing, permissions and cleanup. Binding
include selectors to repository IDs, as R-AUTHZ-004 does, means that a repository deleted and
created again under the same key is not cleaned by an old policy without anyone noticing. Retention
also has something authorization lacks, a repository-level exclude list, and there the same
binding would work against safety: an excluded repository that is deleted and re-created would
lose its protection while a key pattern in the include list still covers it. Exclusions are
therefore matched by key. The syntax and the code stay shared; only the binding of an exclusion
differs. The development agent would revisit this only if authorization.md changes its matcher,
and retention would then follow automatically.

**Applied in the spec:** R-STOR-038 now defines the scope through R-AUTHZ-004 selectors
(`@any-local` for all; other class selectors `422`; include key selectors stored by repository ID,
with inert policies; exclude key selectors matched by key and kept when the repository is deleted)
and R-AUTHZ-005 name and path patterns, including each format's case rule, with an acceptance
criterion for a re-created excluded repository. R-STOR-043 uses the same name glob for build names.
Converging the routing patterns of R-REPO-005 and ADR-037 on the same matcher is a cross-page
consequence.

### Q-STOR-10 · Storage maintenance jobs and the storage summary: one task framework, one path

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

This page specified its own run API for storage jobs: `/api/v1/maintenance/gc-runs`,
`…/orphan-sweeps`, `…/scrub-runs`, a `maintenance_runs` table, `storage.*.schedule` keys and
`/api/v1/storage/summary`. Meanwhile [architecture.md](architecture.md) (job table, scheduler,
checkpoints) and [operations-and-observability.md](operations-and-observability.md) (R-OPS-019,
R-OPS-020, R-OPS-023: `/api/v1/system/tasks/{name}/run`, `/api/v1/system/jobs`,
`maintenance.tasks.<name>`, `/api/v1/system/storage/summary` with its own refresh path) define
the same functions. [api-and-ui.md](api-and-ui.md) R-API-020 required exactly one path per
function. For STOR-039, the two summary requirements also disagreed on the per-repository share:
a percentage here, a ratio there.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Shared task framework | Storage jobs are tasks of R-OPS-019 on the job table of architecture.md, started only through the task endpoint and stopped only through job cancel. This page defines their semantics, parameters and results, and owns the read paths of their results (`/api/v1/storage/summary`, `/api/v1/storage/damaged-blobs`); R-OPS-023 owns the summary task and snapshot table | One scheduler, one run table, one stop mechanism, one job screen; exclusive enqueue, exactly-once slots, leases and checkpoints already exist | Storage-specific listings become `?kind=` filters on the generic job list |
| (b) Storage keeps its own API | `/api/v1/maintenance/*` endpoints, `maintenance_runs` and `storage.*.schedule` stay; the operations page points here | Storage-tailored endpoints | Storage becomes the only subsystem with a second scheduler configuration, a second run table and a second stop API; the job model's guarantees are rebuilt |
| (c) Both | Storage endpoints as aliases over framework jobs | Familiar paths for either reader | Two paths per function, contrary to R-API-020's acceptance criterion |
| (d) (a) plus read-only domain views of runs | Start and stop as in (a); storage adds `GET /api/v1/storage/gc-runs` and similar listings that reference the jobs, as [api-and-ui.md](api-and-ui.md) R-API-015 and Q-API-9 permit | Storage-shaped history (filters on reclaimed bytes, per-run counts as columns) without a second start or stop path | A second listing of the same runs to build, paginate and authorize; the job list already filters by `kind` and `state` and carries each run's result; no MVP screen needs more |

**Choice:** (a) — storage jobs are tasks of the shared framework (`gc`, `gc-full`,
`orphan-sweep`, `scrub`, `storage-summary`, `remote-cache-cleanup`) with one start and one stop
path each; storage keeps only the read paths of its results.

**Rationale:** The job table already offers everything storage jobs need: `exclusive` dedup keys
(one collection run at a time across `gc` and `gc-full`), `(task, scheduled_for)` slots that
enqueue exactly once, leases, `checkpoint` for resumable sweeps, and cancel at the next checkpoint.
The scheduler already reads `maintenance.tasks.<name>`. Under (b) one developer would build and
test all of that twice. Collection needs two tasks rather than one task with a `mode`, because
R-OPS-019 gives each task exactly one schedule and the incremental and full runs need different
ones; the shared dedup key keeps them from overlapping, and a slot it refuses is offered again
on the next scheduler tick, so the weekly sweep is postponed, never lost. (d) is the alternative
that Q-API-9 leaves open; it adds a second listing without adding information, and it can be added
later without moving any path, so the prototype does without it. The STOR-039 share is fixed as a
ratio of logical bytes: object storage reports no capacity, and physical bytes cannot be split
across repositories because deduplication shares them. Retention policy runs are not covered by
this decision: they are per-policy resources with their own history and report (R-STOR-041,
R-STOR-042), which R-API-020 explicitly keeps on their resource paths, while their execution uses
the same job queue. The development agent would revisit this if a storage job needs something the
job model cannot express. Resuming an orphan sweep across runs fits it, through the checkpoint of
the last incomplete job.

**Applied in the spec:** R-STOR-025 uses tasks `gc` and `gc-full` with a shared exclusive key,
states how a slot refused by that key is enqueued later, triggers and observes runs through the
task and job API, uses `maintenance.tasks.*` schedules, drops the `maintenance_runs` table, and
its acceptance criteria now hold in weeks with a full sweep. R-STOR-040 registers retention runs
with one attempt, so a lost worker fails the run as specified instead of the reaper re-queueing
it. R-STOR-026 uses task `orphan-sweep`, resumes from the last
incomplete job's checkpoint, and rejects a `minAge` not above twice the commit deadline. R-STOR-027
uses task `scrub`, moves the damaged-blob API to `/api/v1/storage/damaged-blobs` and defines the
damage counter and gauge. R-STOR-028 keeps the read path `GET /api/v1/storage/summary`, starts
recomputation only through the task, and defines the figures, `shareOfLogicalBytes` as a 0–1
ratio, and the listed repositories. R-STOR-045 uses task `remote-cache-cleanup`. The GC
metrics are renamed to the `quader_gc_*` family of R-OPS-008, and the configuration reference is
updated. The matching edits on the operations, API and HA pages are cross-page consequences.

### Q-STOR-11 · Multi-request uploads across replicas

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Superseded in part by [Q-STOR-174](#q-stor-174--the-tail-is-a-list-of-segments-that-are-read-once-and-the-multipart-upload-is-created-with-the-first-part) (the tail is a list of segments, read once, and the multipart upload is created with the first part) and [Q-STOR-175](#q-stor-175--a-part-upload-is-sent-under-a-freshly-renewed-claim-and-ends-before-its-lease-does) (the part identity is guarded by the claim, the deadline and the store's rule, not by the ETag check alone): where the text below says that small chunks rewrite the tail or that `CompleteMultipartUpload` alone turns a stale part into a failed session, those two decisions apply.

INV-12 and INV-18 (owner constraints of 2026-10-28) send the requests of one upload session to
random replicas without affinity, and a node drain may end any request. R-STOR-011 staged each
chunk as an object and assembled the chunks at finalize. The OCI distribution specification
requires chunks in order, answers an out-of-order chunk with `416`, lets a client read the
current offset with `GET`, and lets a registry announce a minimum chunk size
(`OCI-Chunk-Min-Length`, which clients SHOULD honour). S3 requires every part except the last to
have at least 5 MiB, and consecutive part numbers when parts carry checksums.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Staged chunk objects, assembled at finalize | The earlier R-STOR-011: each chunk becomes an object; finalize assembles them with `UploadPartCopy` when every chunk but the last has 5 MiB, otherwise by streaming all chunks through the finalizing replica | Simple chunk requests; no part bookkeeping | With small chunks the finalize request streams the whole blob through one replica, so its duration grows with the blob and a drain cuts it, losing the assembly; a client that streams the whole blob in one `PATCH` loses everything when that request is cut |
| (b) One S3 multipart upload per session, parts of a fixed size, a bounded tail, progress in PostgreSQL | Each request appends to the session's multipart upload in parts of `part_size`, records every part, hash state and offset in PostgreSQL under a writer claim, and keeps fewer than `part_size` leftover bytes as a tail object that it writes under its own key and records with its SHA-256; finalize completes the upload and copies it server-side to the content address | Per-request work is bounded by the chunk plus one part; a long streamed request records progress at every part, so a drain loses at most one part and the client resumes on any replica; finalize moves no byte through Quader; a stale writer can at worst fail the session, never corrupt it, because `CompleteMultipartUpload` checks every part's ETag and the stale writer's tail lands under a key the session never records | A part table and a writer claim; small chunks rewrite the tail, up to one part per request; one server-side copy at finalize |
| (c) Session affinity | Route all requests of a session to one replica and keep the bytes in its spool | No staging in S3 | Contradicts INV-12 and INV-18 (no affinity); a drain or crash loses the session; the spool must hold whole blobs |
| (d) Session state in Valkey, bytes in S3 | Hash state, offset and part list in Valkey | No PostgreSQL write per part | A Valkey failover or flush loses every open session and costs its client a complete re-upload, more than the one retry [architecture.md](architecture.md) R-ARCH-032 allows for state in Valkey; part lists exceed R-ARCH-030's 1 KiB value limit |

**Choice:** (b) — one S3 multipart upload per session with parts of `storage.uploads.part_size`,
a tail object below one part, and the session's offset, hash state and parts recorded in
PostgreSQL under a writer claim.

**Rationale:** The owner's drain constraint is what separates the options. Under (a), the request
that does the most work, the finalize, grows with the blob, and the common client behaviour of one
streamed `PATCH` per blob puts the whole blob into one cuttable request. Under (b), no request
holds more than one part of unrecorded work, and the OCI protocol already has the resume path
(`GET` for the offset, `416` for a wrong one), so a drain costs one part and one retry. (c) is
excluded by the owner's constraint, and (d) by INV-3, because a lost session is not a performance
cost but a lost upload. The writer claim keeps two requests from writing one session; the ETag
check of `CompleteMultipartUpload` turns the remaining race, a writer that lost its claim and
still uploads a part, into a failed session instead of a wrong blob. The tail has no such check
in S3, because it is a plain object, so each request writes its own tail object and the session
records which one counts, with its SHA-256; a stale writer's tail is never read. The final digest
comes from the recorded hash states, not from re-reading the object, which is why every byte that
enters a part must be the byte that was hashed. A drain must not leave the claim behind either: a
client that meets `416` for as long as a cut request's claim lasts may exhaust its retries, so a
cut request releases its claim, and only a crash leaves one to expire. The development agent
would revisit this if a supported store's server-side copy of a 5 GiB blob takes longer than the
`web` grace period, which would make every drain during a large finalize cost a retry.

**Applied in the spec:** R-STOR-011 is rewritten: state, claim, parts, tail (one object per
writing request, checked by its recorded SHA-256 on read-back), finalize steps (the change to
`completing` takes the claim; overlapping finalizes converge), resumption, expiry (every request
enforces `session_max_age`), the `upload_sessions` and `upload_session_parts` tables, configuration
and acceptance criteria. R-STOR-004 step 8 releases a cut request's claim and keeps the session's
multipart upload. R-STOR-001 names the staging keys, R-STOR-006 and R-STOR-026 keep open sessions
out of reach of multipart expiry, R-STOR-025 lets collection runs abort expired sessions,
R-STOR-047 classes both tables `transient`, and the security notes bind a session to its
principal. Cross-page: [backup-and-restore.md](backup-and-restore.md) R-BAK-004 (initial
`transient` table list); [architecture.md](architecture.md) R-ARCH-032 (the chunked-upload entry
names the multipart upload and the part table) and R-ARCH-024 step 5 (a cut session request keeps
its recorded parts and releases its claim).

### Q-STOR-12 · Counters that several replicas update: usage, quotas and download statistics

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

The load exceeds one replica (INV-12), so uploads, deletes and downloads of one repository arrive
on many replicas at once. Usage counters feed quotas (R-STOR-029) and the storage summary
(R-STOR-028); download statistics feed retention's `unusedForDays` (R-STOR-038). INV-3 now offers
Valkey for shared ephemeral state, on the condition that losing it costs performance only.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Append-only deltas in PostgreSQL, folded by every worker | Triggers append deltas; every `worker` replica folds them with `SKIP LOCKED`, updating counter rows in one order; readers, quota admission included, read the folded rows; download statistics are buffered per `web` replica and flushed in item order | No hot row on the write path, whatever the replica count; durable and identical for every replica; no lease, so folding scales with workers and survives drains; one primary-key read per quota admission, whatever the upload rate; independent of Valkey | Counters and quota decisions lag by one fold interval; a quota can be exceeded by what is committed in that time and by the content in flight |
| (b) One counter row per repository, updated in every commit | The commit transaction updates `repository_usage` directly | Exact at all times | Every upload to a repository serializes on one row until its commit; with many replicas this row becomes the bottleneck, the pattern R-ARCH-018 already forbids for change areas |
| (c) Counters in Valkey (`INCRBY`), persisted periodically | Writers count in Valkey; a job copies the totals to PostgreSQL | Fast, no PostgreSQL write | A failover with asynchronous replication loses increments, so quotas and the summary drift until a recount; a quota would then admit uploads beyond its limit after a Valkey incident, which is a correctness cost INV-3 rules out |
| (d) Per-replica buffers for usage too | Each replica sums its deltas in memory and flushes them | Fewer rows | A crash loses deltas permanently, so counters drift; between flushes each replica decides quotas on different numbers, which INV-18 rules out |

**Choice:** (a) — usage stays in append-only PostgreSQL deltas, folded by every `worker` replica
with `SKIP LOCKED` and a fixed update order, read in folded form by quota admission, and
recomputed after a backup restore; download statistics stay in per-replica buffers flushed in item
order and at shutdown.

**Rationale:** A quota is a limit operators rely on, so its counter must survive any Valkey
incident; that rules out (c), and (d) fails the same test for a crashed replica. (b) is exact but
puts every upload of a busy repository in one queue, which the owner's load assumption makes the
first bottleneck. (a) was already the design; the new constraints add that the fold must run on
any number of workers without a lease and survive a drain, which `SKIP LOCKED` and a fixed lock
order give, and that a restore must recompute the totals, because the backup does not export
unreferenced blob rows. Admission reads the folded rows only: adding the unfolded deltas would make
it exact, but their number grows with the upload rate of all replicas, so every admission would
read more rows as the installation grows, and the extra precision only narrows an overshoot that
in-flight uploads cause anyway. The fold interval drops from 30 s to 5 s, so the overshoot that
the lag adds stays at a few seconds of uploads, and a fold iteration is cheap enough to run that
often. Download statistics stay buffered: they are not audit data, a
crash loses
at most ten seconds of counts against `unusedForDays` measured in days, and the shutdown flush
makes a drain lose none. The development agent would revisit this if the fold falls behind under
the reference load (`storage_usage_deltas` growing for longer than ten fold intervals), which
would call for partitioning the deltas by repository.

**Applied in the spec:** R-STOR-003 (fold SQL, repeated iterations under backlog, the join that
keeps a deleted repository from blocking the fold, `storage_usage_totals` counting `live` rows,
reads of the folded rows, `storage_usage_recount()`, acceptance criteria with three workers and
with a repository deletion), R-STOR-015 (`web` role, item order, removed items dropped, shutdown
flush), R-STOR-024 (the physical delta at step 3), R-STOR-029 (PostgreSQL only, folded rows, one
primary-key read per limit), R-STOR-046, R-STOR-047, and the configuration reference
(`storage.usage.fold_interval` 5 s instead of 30 s). Cross-page:
[backup-and-restore.md](backup-and-restore.md) R-BAK-022 (the recount as a fix-up).

### Q-STOR-13 · Exclusion of storage maintenance runs: job key, fenced lease or Valkey lock

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

[ha-and-helm.md](ha-and-helm.md) R-HA-006 lists garbage collection, the orphan sweep and retention
runs as singleton work under a fenced advisory-lock lease, with every write on the lease's session.
This page, [architecture.md](architecture.md) R-ARCH-016 and Q-STOR-10 register the same work as
jobs with an exclusive dedup key, whose handlers write through the main pool. INV-3 keeps mutual
exclusion whose failure would corrupt data in PostgreSQL. With n workers and drains at any time
(INV-12), a run moves from one worker to another as a matter of routine.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Exclusive job key; correctness from row locks; a hand-over rule for S3 deletes | One run at a time through the job table; every step is a short transaction whose safety rests on row locks; a collector deletes objects only within a bound after its claim, and another run finishes a claim only after it | Exclusion in PostgreSQL without a session; runs work through a transaction-mode pooler; a drain releases the run with its checkpoint and hands its claimed rows over; the hand-over rule bounds the late-delete race, the one hazard that a run moving between workers creates | A new setting (`storage.gc.handover_delay`); a row abandoned by a crashed collector waits that long, and uploads of its digest answer `503` meanwhile |
| (b) A fenced advisory-lock lease, as R-HA-006 describes, in addition to the job | The handler holds a session lock; all its writes run on that session | A partitioned handler cannot commit after losing the lease | Collection's writes are already safe when two runs overlap, so the fence protects nothing in PostgreSQL that needs it; the dangerous late action is the S3 delete, which no database fence stops; the busiest maintenance writer moves onto session connections, outside the transaction-mode pooler |
| (c) A Valkey lock | `SET NX` with a TTL around each run | No PostgreSQL session | A failover loses the lock and starts a second run; while Valkey is unavailable the runs either stop or run unguarded; INV-3 keeps such exclusion in PostgreSQL |
| (d) No exclusion | Runs may overlap freely | Nothing to build | Duplicate work and duplicate result counts; overlapping scheduled slots |

**Choice:** (a) — storage maintenance runs are excluded only by the exclusive job key in
PostgreSQL; their safety rests on row locks, and S3 deletes follow the hand-over rule of
R-STOR-024. They take no advisory-lock lease and no Valkey lock.

**Rationale:** R-STOR-024's proof shows that two overlapping collectors cannot delete a referenced
blob, and the orphan sweep's claim insert and retention's delete path rely on row locks the same
way. Exclusion is therefore an efficiency property, and the job key provides it in PostgreSQL. The
one hazard that comes from runs moving between workers is a delete that a drained, paused or
partitioned collector sends late, after another run finished the row and an uploader stored the
digest again. A database fence would not stop that request; the hand-over rule does, except for a
process frozen between its deadline check and the write itself, which the scrub detects. The
rule's clock is the collector's own, read before the claim transaction, because comparing a
replica clock with the database's would let clock offset eat the margin. A drained collector is
not a lost one: it knows its calls have returned, so it hands its rows over at release and costs
an upload of the same digest seconds instead of `handover_delay`. (b) costs
session connections for no correctness gain, and (c) would make exclusion depend on Valkey's
availability. The development agent would revisit this if a storage task appears whose
correctness needs exclusion; that task would then take (b).

**Applied in the spec:** R-STOR-024 (hand-over rules with the collector's own clock, the hand-over
of unfinished claims at release, the end-of-run pass, `storage.gc.handover_delay`, relation to
other jobs, acceptance criteria with a paused and with a drained collector), R-STOR-005
(`blob_wait_timeout` above twice `delete_settle`), R-STOR-025 (queue, job key, drain), R-STOR-040
(dedup key, the run limit through `retention_run_gate`, drain, lost worker), R-STOR-041 (stop
through the job state), R-STOR-044 (append-only `SKIP LOCKED` outbox), R-STOR-046.
Cross-page: [ha-and-helm.md](ha-and-helm.md) R-HA-006 (garbage collection, the orphan sweep and
retention runs leave the list of lease holders) and R-HA-002 (its acceptance criterion identifies
the GC worker through `job.locked_by`, not `quader_lease_held`);
[architecture.md](architecture.md) R-ARCH-016 (registration table: queue `maintenance` for the
storage tasks, `default` for retention runs) and Q-ARCH-14 (its rationale no longer names
garbage-collection decisions as what the lease fence protects).

### Q-STOR-14 · Object-store settings the outbound layer does not yet support, and plaintext endpoints

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Refuse every key without its outbound option at start-up | `storage.s3.ca_file`, `http.max_connections`, `http.tls_handshake_timeout`, `http.idle_conn_timeout` and `http.proxy_credentials_file` fail the configuration until the outbound layer supports them | Nothing is silently ignored | Five keys that the schema lists are unusable until the outbound work lands |
| (b) Accept every key and ignore the unsupported ones with a warning | The keys load; the warning names each | Keys usable at once | An operator believes a setting is applied when it is not |
| (c) Accept the keys, refuse the one whose effect is a security decision, and let the others take the outbound defaults, stated in the code (chosen) | `storage.s3.ca_file` is refused with an error naming the fix (add the CA to `proxy.outbound.tls.ca_files`). `max_connections`, `tls_handshake_timeout` and `idle_conn_timeout` take the factory's defaults; the S3 client does not read `proxy_credentials_file` yet. | A trust-store choice is never taken silently; the timeouts and limits stay usable | The three timeouts and the connection limit are not configurable until the outbound options exist |

Plaintext endpoints (chosen with (c)): the endpoint rules of `config.Storage.Validate` apply, and an `http://` endpoint
needs `storage.s3.allow_insecure_endpoint: true`. The development profile and the single-node CI values set it.

**Choice:** (c), with the plaintext rule above.

**Rationale:** The one setting that changes which authority a connection trusts (`ca_file`) must fail loudly, because a
silent fallback to the system roots would verify against a different store than the one configured. The limits and
timeouts have a sane factory default, so a refusal would only block the operator for no safety gain. The development
agent would revisit (c) when the outbound layer gains `WithRootCAFile` and the remaining options, because then the keys
are applied rather than defaulted.

**Revisited in M0 (2026-10-29):** the outbound layer gained the trust store of a single client
(`outbound.TrustStore`), so `storage.s3.ca_file` is supported and replaces the system roots for the object-store client,
reloaded like every other bundle ([blob/s3/client.go](../../internal/blob/s3/client.go)). The option (c) reasoning holds
for `proxy_credentials_file`, which is still refused with an error naming the reason, and for the timeouts and the
connection limit, which still take the factory's defaults.

**Applied in the spec:** R-STOR-033 names the metric label `operation` (replacing `op`, Q-OPS-26) in its metrics line,
and the acceptance criterion of R-STOR-005 that reads the same counter uses `operation` too; no other requirement text changed
except R-STOR-030, which now states that `ca_file` is supported.

### Q-STOR-15 · Object-store endpoint without a scheme

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Refuse it | `storage.s3.endpoint` must carry `https://` or `http://` | One form | The bucket provisioner writes the endpoint as a host without a scheme, so the chart would have to compose a URL from the Secret value |
| (b) A host without a scheme means `https://`; plaintext needs `http://` and `allow_insecure_endpoint` (chosen) | The value is normalised before validation and written back at load | The provisioner's value works unchanged; the secure form is the default one; a plaintext store is still spelled out | A typo such as `htp://host` is not a scheme-less host: it contains `://` and is refused as an unsupported scheme |
| (c) A scheme-less host means `http://` when `allow_insecure_endpoint` is set | The flag decides the scheme | Short for development | The flag would change what the value means, and a host that is meant as `https` silently goes plaintext |

**Choice:** (b).

**Rationale:** TLS is the rule for every connection (INV-20), so the shorter spelling must be the secure one. The
rewrite happens once, at load, so the client, the validation and the effective-configuration view agree on the same
URL. `storage.s3.credentials.sts_endpoint` is not rewritten: it is not a store address a provisioner writes. The
development agent would revisit this if a provisioner appears whose host values already include a scheme or a path.

**Applied in the spec:** R-STOR-030 (validation bullet and the configuration block).

### Q-STOR-16 · What the spool may delete at start

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

The spool directory is scratch space of one pod. Its files are removed at start, and the directory may be a volume that
holds more than the spool wrote.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Empty the whole directory | every entry is removed | simplest; the directory is always clean | a misconfigured `storage.spool.dir` pointing at shared data deletes that data |
| (b) Remove only the regular files named by a UUID; refuse the start on any other entry (a `lost+found` directory is tolerated) | the spool's own files, nothing else | a wrong path fails before anything is deleted | a directory with any other content stops the replica until someone looks |
| (c) Remove the files named by a UUID and leave other entries alone | a lenient delete | the replica starts | a foreign entry stays in a scratch directory that is meant to be empty |

**Choice:** (b).

**Rationale:** The spool directory is configured by path, and a deletion at start is irreversible. The rule removes exactly
the files the spool creates, which are regular files with random UUID names and mode `0600`, and it refuses to act on a
directory that is not the spool's. The development agent would revisit this only if a supported volume puts entries of its
own into the spool directory, which the chart does not do (an `emptyDir` or an ephemeral volume).

**Applied in the spec:** [R-STOR-004](#r-stor-004--upload-pipeline-admission-spool-and-single-pass-hashing) (the start rule of
the spool, the bullet after the restart rule); the implementation is `NewSpool` in [internal/blob/spool.go](../../internal/blob/spool.go).

### Q-STOR-17 · Replacing a file without overwrite answers 403

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

The error row of R-STOR-007 lists 409 for "an existing item and no `overwrite` permission"; R-AUTHZ-006, R-AUTHZ-016 and R-GENERIC-004 say 403.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) 409 `conflict` | The refusal is a conflict with the existing item | As the R-STOR-007 row says | Contradicts R-AUTHZ-016 ("a write-type request without a required action: 403, naming the missing action") and the acceptance criteria of R-AUTHZ-006 and R-GENERIC-004 |
| (b) 403 `forbidden` naming `overwrite` (chosen) | A missing action is a missing permission | One rule for every refused write; a client sees which action to ask for | The row of R-STOR-007 changes |

**Choice:** (b). A replacement with different bytes by a caller who holds `deploy` and `overwrite` answers 201; an identical re-upload answers 200 and needs `deploy` only.

**Rationale:** Three pages and two acceptance criteria say 403. The development agent would revisit this if the 409 of the row was meant for a format rule, which the generic format has none of.

**Applied in the spec:** R-STOR-007 (the error row).

### Q-STOR-18 · Implicit folders are rows that go with their last child, serialised by row locks

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

R-STOR-003 says implicit folders are created as parents of a file and removed when their last child goes; a deploy and a delete of the last child can overlap.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Folders are derived from the paths | No folder rows; a listing groups the paths | Nothing to keep in step | Explicit empty folders, folder properties and the folder count of the usage fold need rows; a listing is a scan |
| (b) Folder rows, created with the file and removed with the last child (chosen) | A deploy inserts the missing ancestors (ON CONFLICT DO NOTHING) and holds its parent FOR SHARE until it commits; a delete locks the folder FOR UPDATE and looks for children in a statement of its own | A listing is an index range; the usage counters and explicit folders work; the lock order cannot leave a child without its folder | Every deploy takes one more row lock; a deploy can find its parent gone and repeats the step (bounded) |

**Choice:** (b).

**Rationale:** The integration test `TestConcurrentWritersOnOnePath` runs deploys and deletes of the same folders on two replicas and finds no item without its parent folder. The development agent would revisit this if the share lock on a hot folder shows up as contention.

**Applied in the spec:** R-STOR-003 reads unchanged; the protocol is in [docs/developer/files.md](../developer/files.md).

### Q-STOR-19 · A delete is permanent in M1, one event per file, a large subtree by the job items-delete

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. **Superseded in part by [Q-STOR-190](#q-stor-190--the-delete-job-decides-every-batch-again-on-what-the-subject-that-asked-holds-then)**: the job decides every batch again, on what the caller holds then (option (a), with the subject the job carries).

R-STOR-018 deletes into the trash can where it is enabled (P1, R-STOR-022), writes the format hook in the same transaction, and removes a subtree above `storage.delete.max_sync_items` by a background job after the permission check.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The job re-checks the permission per batch | The job decides `delete` again for every item it removes | No item is removed that the caller may not delete at that time | The job has no caller: the authorization state of the request would have to be rebuilt in the worker |
| (b) The check completes before the job is enqueued, the job removes what it finds (chosen) | The request checks every item of the subtree (403 and nothing changes otherwise); the job lists and removes in batches | R-STOR-018 as written ("the permission check still completes before the first removal") | An item a concurrent deploy adds below the folder between the check and the job is removed unchecked (H-154) |

**Choice:** (b). Without the trash can an item is deleted for good and its blob goes to the collection through the statement trigger. A file produces one `artifact.deleted` event in the transaction that removes it.

**Rationale:** The synchronous path checks and removes the same rows in one transaction, so it has no such window. The development agent would revisit this when the trash can exists (the delete then moves rows and the window is harmless).

**Applied in the spec:** R-STOR-018 reads unchanged; [docs/developer/files.md](../developer/files.md).

### Q-STOR-20 · A listing scans a bounded number of rows and its cursor is the last row scanned

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

R-STOR-017 omits the entries the caller may not read and pages with a cursor; a caller who may read little of a huge folder would otherwise make one request scan it all.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Filter in SQL | Push the grant into the query | Exact pages | The grant index is memory-only and has no SQL form (R-AUTHZ-015) |
| (b) Scan up to 100,000 rows per page and return a short page with a cursor (chosen) | The cursor is the path of the last row the page scanned | Bounded work per request; a client continues while `nextCursor` is not null | A page can hold fewer entries than `limit` while more follow |

**Choice:** (b). The default page size is 1,000 and the maximum 10,000, as R-STOR-017 says; they are not the general limits of the other lists.

**Rationale:** The decision per entry reads memory only, so the bound is on rows read from PostgreSQL. The development agent would revisit the figure if a real folder needs more.

**Applied in the spec:** R-STOR-017 reads unchanged; the operation `listChildren` describes it. What the cursor holds when the page ends on a row the caller may not read, and how many rows one query reads, are Q-STOR-62.

### Q-STOR-21 · A folder addressed without its trailing slash is redirected, a download counts after its body was sent

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

R-STOR-012 says a GET on a folder returns the directory index, whose links are relative to the folder; R-STOR-015 counts a download when Quader answers 200, or 206 from byte 0.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Serve the index at both URLs | No redirect | One request | The relative links of the HTML index point one level too high |
| (b) 301 to the URL with the slash; count after the whole response body was copied (chosen) | The index lives at the URL with a slash; the counter is called when the copy ended without error | Links work; a cut or aborted transfer is no download | A client that stops reading after the first bytes of a range from 0 is not counted |

**Choice:** (b).

**Rationale:** Both are what a file server and a statistics counter of record do. The development agent would revisit the counting rule if the audit of downloads needs the cut ones.

**Applied in the spec:** R-STOR-012 and R-STOR-015 read unchanged.

### Q-STOR-62 · A listing that ends on a hidden row: its cursor and its cost

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Under Q-STOR-20 a page that reached the scan limit returned the path of the last row it scanned as its cursor. That row may be
one the caller may not read, and a cursor is base64 JSON, so the client could decode a hidden name (R-AUTHZ-016). A query read
`limit + 1` rows, so `limit=1` on a folder of 100,000 hidden rows cost 50,000 queries.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The path of the last row scanned | As before | Exact resumption | Names a hidden row |
| (b) A sealed cursor | Encrypt the path with a key every replica holds | Reveals nothing | No such key exists; it would be one more secret to distribute and rotate |
| (c) The item's id (chosen) | The cursor holds the numeric id of the last row scanned; the next page reads its path in the repository below the listed folder and continues after it; a purged row is 400 `invalid_cursor` | No name leaves the server; any replica resumes | A forged id tells where that item sorts among the visible entries (H-208) |
| (d) The last visible entry | Resume there | Reveals nothing | Every page scans the hidden run again, and a run longer than the scan limit never ends |

**Choice:** (c) when a page ends at the scan limit beyond its last visible entry; every other page keeps the path of its last
entry, which the caller may read. A query reads `limit + 1` rows and doubles that after each query that left the page short, up
to 5,000, so a page costs a few dozen queries at most (2 + 4 + … + 4096 rows, then the rest of the 100,000 in chunks of 5,000).

**Rationale:** the scan limit exists for callers who see little of a folder; for them the cursor must not carry what they cannot
see, and the cost of a page must not depend on its size. The development agent would revisit (b) when a cluster-wide key for
other purposes exists.

**Applied in the spec:** none; R-STOR-017 and Q-STOR-20 read unchanged apart from the pointer to this decision.

### Q-STOR-143 · The delete job removes only the items its permission check saw

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. **Superseded in part by [Q-STOR-190](#q-stor-190--the-delete-job-decides-every-batch-again-on-what-the-subject-that-asked-holds-then)**: the bound by the highest id stays, and every batch is decided again on what the caller holds then, so the insert in flight is decided too and H-154 is closed.

R-STOR-018 decides `delete` for every item of a subtree larger than `storage.delete.max_sync_items` at the request and removes in a
job. The job listed what it found, so a file that a holder of `deploy` added below the folder between the check and the batch that
reached it was removed without a decision (H-154).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Keep it as a documented gap | H-154 as written | No change | A principal without `delete` loses a file to another's delete |
| (b) Decide again per batch for the stored subject | The job carries the subject and calls `PathsAllowed` for every batch | Also follows a grant that was removed meanwhile | A job has no request: the subject, its token scope and the endpoint ceiling would have to be stored and rebuilt; a refused batch leaves a half-deleted subtree |
| (c) Fix the set at check time (chosen) | The job carries the highest item id the check saw (`DeleteArgs.MaxID`) and removes only items up to it | One integer; the set that was decided is the set that goes; no subject in a job | An item with a lower id that is committed after the check's snapshot (an insert in flight at that moment) is still removed |

**Choice:** (c). The folders go the same way; a folder that still has a child (the late file) stays, with the ancestors above it.

**Rationale:** the grant that applied at the request is the one the caller was told about, and the job is the continuation of that
request. Item ids come from one sequence, so everything inserted after the check has a higher id. The development agent would
revisit it if a trash can (R-STOR-022) made the window recoverable, or if the in-flight insert has to be closed too (a snapshot
horizon stored with the job).

**Applied in the spec:** R-STOR-018 above; [file-transfer.md](../security/file-transfer.md) H-154 is narrowed to the in-flight insert.

### Q-STOR-144 · A deploy locks the whole ancestor chain, and `overwrite` is settled before the commit transaction

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Two defects of the link step of a deploy (R-STOR-005, R-STOR-007, R-STOR-017). It held the parent folder `FOR SHARE` and checked that
the ancestors are folders, not that they exist, so a delete of the subtree that removes the emptied ancestors while the deploy runs
could leave a file below folders that are gone. And when a concurrent upload created the path after the checks, the link step decided
`overwrite` inside the commit transaction, where a decision that waits for the index holds the blob row lock and the item lock.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Lock the parent only, as before | | One lock | The grandparents are not protected |
| (b) Lock every ancestor `FOR SHARE`, deepest first, require all present (chosen) | `LockFoldersShared` after `ensureParents`; a missing one repeats the round | A delete waits for the deploy at every level and finds the child | One statement with as many rows as the path has folders |
| (c) Lock them top down | | Natural reading order | The delete removes folders deepest first and would meet it in the opposite order: a deadlock |
| (d) Decide `overwrite` in the transaction | As before | No extra decision | A wait inside the transaction |
| (e) Settle `overwrite` before the commit (chosen) | `PathsAllowed` for `overwrite` beside the commit-time check, without an audit event; the link step returns `errOverwriteDenied`, and the refusal is then decided and audited outside | No decision inside the transaction | One more evaluation per deploy that did not see the path |

**Choice:** (b) and (e). The `overwrite` evaluation is `PathsAllowed` of the same index family as the commit-time check, not the very
index `Begin` returned (that call has no non-auditing form); the two evaluations are one request apart.

**Rationale:** the lock order of a delete is the one a deploy must follow; the deterministic test holds the lock of a folder two levels
above the file and removes the chain under it. The development agent would revisit the index identity when `DecideOn` gets a
non-auditing form.

**Applied in the spec:** none; R-STOR-017 and R-STOR-007 read unchanged.

### Q-STOR-145 · A repository's `maxUploadSize` is a validated integer and fails closed

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

`config.storage.maxUploadSize` (R-STOR-004) was read into an `int64` at deploy time, and the document check only required `storage` to
be an object. `1e9` was accepted and decoded to zero, which is "no limit"; the limit failed open.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Validate at write only | 422 `invalid_value` at `/storage/maxUploadSize` for anything but a non-negative integer literal | The error is where the mistake is made | A stored document that predates the rule still reads as none |
| (b) Validate at write, refuse at use (chosen) | (a), and a stored value that is not an integer refuses the upload with 500 | No path ends in "unlimited" | An old bad document blocks uploads until corrected |

**Choice:** (b). The literal form is digits only (`1e9`, `1.5`, `-1`, `"10"`, `null` are refused); it must fit an int64.

**Rationale:** a limit someone set and mistyped is not a limit they meant to lift. The development agent would revisit it if the
management API gains a migration that rewrites such documents.

**Applied in the spec:** R-STOR-004 reads unchanged; the same change makes deleting in a virtual repository `405` in code, as R-STOR-018 already says.

### Q-STOR-146 · A remote fill is an item with its upstream URL and no `artifact.deployed` event

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the proxy stores the artifact of a remote repository as an item of that repository (R-REPO-025) through the item store's one commit
(Q-ARCH-207). `artifact.deployed` is specified as "a file was stored in a local repository by any upload path" (R-OPS-014), and the cache event
`artifact.cached` is webhook-only and raised by the proxy.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The commit writes `artifact.deployed` for every origin | One audit row per commit | One rule | A cache fill would claim an upload that nobody made, and the audit trail would hold one row per cached tarball |
| (b) Only an upload writes it; a fill stores `upstream_url` on the item and the proxy raises its own event (chosen) | The audit row is part of the commit for `OriginUpload` | The event means what R-OPS-014 says; the item still names where its bytes came from | The proxy owns the fill's event |

**Choice:** (b). The `upstream_url` column of the item is written by the insert and the replace of a file and read into `items.Item.UpstreamURL`;
the item description (`upstreamUrl`) shows it for a cached item and `null` for an uploaded one. The store removes any user info from the URL before it stores or reports it.

**Rationale:** an audit trail records what a person or a credential did. The development agent would revisit it if an audit requirement for
fills appears.

**Applied in the spec:** R-STOR-016 (the description's `upstreamUrl`) reads unchanged.

### Q-STOR-147 · The dev profile's object store keeps its objects over an outage

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the object-store row of R-HA-052 and the acceptance criteria of R-HA-010 stop the store of the local profile by scaling it to 0. The store
was one pod on an `emptyDir`, so a scale to 0 deleted every object while the metadata kept naming them: after the return every download of content
written before the outage answered `503 content_unavailable`, and so did every `npm ci` of the load's lockfile, whatever Quader did (R-STOR-036: no
blob is marked missing because of errors, which holds only for a store that still has its objects).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A PersistentVolumeClaim for the data directory (chosen) | `deploy/dev/s3.yaml` claims 20 Gi of the cluster's default class (local-path in kind); `s3-stop.sh` scales the Deployment to 0 and `s3-start.sh` back | The outage is the one the spec names; the objects are there after the return, as they are after a real store's outage | The volume is bound to the control-plane node the store already runs on; a store that was wiped by an earlier outage had been wiped with the cluster anyway |
| (b) Cut the store off the network instead of stopping it | A NetworkPolicy or a Service without endpoints | The data stays on the `emptyDir` | A connection that Quader's pool holds open keeps working through its conntrack entry, so the outage is partial and depends on what the pool held; the chart's policies are ingress rules only (ADR-154) |
| (c) Keep the `emptyDir` and rebuild every fixture after an outage | The scenario re-creates the content of the repositories it uses | No change to the profile | Every other test's content is gone after the scenario, and the metadata of earlier runs names objects that no longer exist |

**Choice:** (a).

**Rationale:** the outage must be what R-HA-052 says, and its aftermath must be a store that has what it had. The development agent would revisit it if
the local profile moves to a store that cannot use a volume.

**Applied in the spec:** R-HA-052, the object-store row (the data survives the outage).

### Q-STOR-148 · What the npm loops may see of an object-store outage

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-HA-052 states the pass condition of the object-store row for the service ("metadata, UI and API succeed throughout; blob transfers answer `503`
with `Retry-After`") and not for the clients of the load. An `npm ci` with Quader's cache warm still reads every tarball of its lockfile, which are
blobs: during the outage they answer `503`, and npm's default retries (two retries, 10 s to 60 s apart, `Retry-After` is not read) end after about
half a minute, so a command that overlaps an outage of 5 minutes cannot succeed.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every npm command exits 0 | The rule of the other scenarios | One rule | Cannot hold: the spec requires the `503` that npm gives up on |
| (b) A command that overlaps the outage and its recovery bound may fail, and then only with the registry's `503`; every other command exits 0, and every `npm ci` loop completes a command after the bound (chosen) | The load is metadata (k6's documents and the API probe) and blob transfers that are expected to be `503` | States what R-HA-052 requires of the clients; a failure with another status, or one outside the window, is a finding; the commands after the bound read the whole lockfile from the restored store | The loops are partly idle during the outage |
| (c) Pause the loops during the outage | The load stops where it cannot succeed | No expected failures to classify | It tests nothing about the transition into and out of the outage |

**Choice:** (b), `outageCommands` in [ha_suite_test.go](../../test/e2e/ha_suite_test.go). The recovery bound is `storage.s3.circuit_breaker.open_duration` + 10 s after the store returns.

**Rationale:** R-HA-052's pass condition is about the service; the clients' share of it is exactly "`503` while the blobs are unreachable, success when they
are back". The development agent would revisit it if npm learns to honour `Retry-After` on a `503` of a tarball.

**Applied in the spec:** R-HA-052, the object-store row (what the clients of the load see).

### Q-STOR-149 · An upload that waits for 100 Continue is refused before its body while the circuit is open

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-HA-010 requires that "an upload rejected because the circuit is open [...] be rejected before the body is read (`503` with `Retry-After`), so clients
that send `Expect: 100-continue` (`dotnet nuget push`, `curl -T`) do not transfer the body at all". R-STOR-036 requires that an upload of content that is already
stored succeeds in an outage, which only the body can show. `Guard.Open` had no caller: `TestHASuite_ObjectStoreOutage` found `curl -T` of 64 MiB with
`Expect: 100-continue` sending all 64 MiB and then receiving the `503`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The spool refuses every new upload while the circuit is open | `Store.Ingest` checks the circuit before the first read | One place | A deduplicated upload of a client without announced digests fails in an outage, against R-STOR-036's second row; the check cannot tell the clients that wait for `100 Continue` from the others |
| (b) The chain only marks a request that carries a body and `Expect: 100-continue`; the upload entry point refuses it (chosen) | `Chain.ExpectMark` puts `blob.WithAwaitedBody` in the request context; `Store.Ingest` asks `RejectUpload` before its first read of the body | The refusal happens after authentication and authorization, at the one place that is about to read an upload: an API write, an exchange route, an anonymous route and a reader without permission are answered as they are without an outage (`403` stays `403`); a remote fill and an upload without `Expect` are never refused early, and a deduplicated upload of the latter succeeds | A client that waits for `100 Continue` and sends content that is already stored is refused in an outage and succeeds when it retries |
| (b0) A stage of the chain refuses every request with `Expect: 100-continue` (the first version of (b)) | `Chain.ExpectGate` | Simple | It ran before authorization on every route and method: non-upload writes failed in an outage against R-HA-052, an unauthenticated exchange request revealed the replica's store state, and a reader got `503` instead of `403` (code review of 2026-11-06) |
| (c) Leave it | The body is read, then refused | No code | The criterion of R-HA-010 fails |

**Choice:** (b), [chain.go](../../internal/httpserver/chain.go) (`expectStage`, marks only), [store.go](../../internal/blob/store.go) (`WithAwaitedBody`, `Ingest`, `RejectUpload`), wired in
[wire_endpoints.go](../../internal/app/wire_endpoints.go). The `Retry-After` of every `503` that the open circuit causes (the refusal, a call on the open circuit, the
failure that opened it, a failing probe) is `storage.s3.circuit_breaker.open_duration` rounded up to whole seconds, at least 5, as R-HA-010 states; a failure before
the circuit opened keeps 5. A call that timed out in the transport (a dial to a store that is gone) is a `503` like any other transient failure and was a `500`. Between the end of the open duration
and the probe (the next call to the store: the readiness check makes one every 5 s) the circuit does not reject, so an upload that arrives in that gap sends its body and
is answered `503` after it; the test requires three in five of the uploads of an outage to be refused before their body. Tests: `TestExpectMark` (the chain refuses nothing),
`TestIngestRefusesAnAwaitedBodyWhileTheCircuitIsOpen` (the `503` before the first read, none without the mark); that `403` precedes it is the order of `Service.Deploy` (the decision before `Spool`).

**Rationale:** Go's server sends `100 Continue` when the handler first reads the body, so a refusal before that read means the client never sends it. The sentence of R-HA-010 names `Expect: 100-continue` as the reason; a client without it sends its body anyway and keeps R-STOR-036's deduplication.
The development agent would revisit it if a client without `Expect` shows up that sends large bodies in outages.

**Applied in the spec:** R-HA-010 (first bullet: a write that sends `Expect: 100-continue`).

### Q-STOR-150 · How long the outage test keeps the store stopped, and what a k6 failure inside it is

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the nightly run 42177273488 failed the 30 s case of `TestHASuite_ObjectStoreOutage` twice. (1) `s3-stop.sh` takes 26 s on a CI runner (the store's pod terminates slowly) and the
test counted the 30 s from before it, so the store was stopped for 4 s after the script returned: the upload prober, which starts then, made one probe where the test requires at least three
(R-HA-010, three in five refused before the body). (2) A k6 `tarball 503` of 2,043 ms that started 55 ms before the scale-down (the transport timeout of the request that met the store's
leaving pod) was counted as outside the outage because the k6 line carries the request's start. Neither is a product defect: the `503` was the registry's, with a replica header.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Lower the number of uploads or the share the test requires | Three in five of at least one | Passes | Weakens R-HA-010's criterion; the 5 min case would not need it |
| (b) The store is held stopped for d from the return of `s3-stop.sh`; a k6 failure counts as inside the window when its request overlapped it (chosen) | `stoppedAt`, `haWindow.overlaps` | The outage is what its name says and the prober has d seconds, about six upload probes in 30 s; the same overlap rule as the `npm` commands | The 30 s case lasts about 56 s from the first second of the scale-down |
| (c) Shorten `s3-stop.sh` (a grace period of 0 on the store) | Less to wait | Same count | A different termination than the profile has in the other sets; the stop time is not the harness's to hide |

**Choice:** (b), [ha_suite_test.go](../../test/e2e/ha_suite_test.go) (`objectStoreOutage`, `haWindow.overlaps`).

**Rationale:** the spec's "stopped for 30 s" is a statement about the store being unreachable, which `s3-stop.sh` establishes by returning. The development agent would revisit it if the stop script is made fast enough that both counts agree.

**Applied in the spec:** R-HA-052 (the object-store row: the 30 s and 5 min count from the store being stopped).

### Q-STOR-151 · A read that finds no object marks the blob missing, and looks once more after the mark

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: finding F-62 ([repositories-and-proxy.md](repositories-and-proxy.md) Q-REPO-202). [ha-and-helm.md](ha-and-helm.md) R-HA-024 says that a read
of a missing object counts `quader_storage_blobs_damaged_total{status="missing"}` and marks the blob `content_status = 'missing'`; `blob.Store.Open`
returned `ErrNotFound` and marked nothing, so the loss was invisible to every other request and to the counter. A remote repository's refill needs
the mark as well: the commit protocol deduplicates against a live row that is `ok` (R-STOR-005) and would not write the object again. The mark
changes only a row that is `ok`, so it never undoes a repair that came after it; a repair that came between the read and the mark (a commit of the
same content) it would undo.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every read that the store answers "no such object" marks the blob, then looks for the object once more; an object that is there again makes the row `ok` (chosen) | `Store.markMissing` in `Store.Open`: `MarkBlobDamaged`, then `HeadObject`, then `MarkBlobOK` | One rule for every reader (downloads, documents, ranged reads); the counter and `QuaderBlobMissing` see local and cached losses alike; whichever order a repair and the mark take, the row ends `ok` when the object is there | One `HeadObject` per detection; a detection that a concurrent repair overtook is counted once |
| (b) Mark only from the remote engine's path | A file of a local repository keeps asking the store | The counter and the alert would miss the losses of local repositories, which are the data loss R-HA-024 is about |
| (c) Look before the mark | No row changes for an object that is back | A repair between the look and the mark is still undone |
| (d) Mark without a look | Simplest | The race above leaves an object that is there marked `missing`; a local file then answers `503` until the next upload of the same content |

**Choice:** (a).

What it means for a local repository (unchanged answer, a new mark): the first read of a lost object answers `503 content_unavailable` with
`Retry-After: 30` as before, and marks the blob; later reads are answered the same `503` from PostgreSQL without asking the store (R-STOR-012) for
30 s per blob, and the counter counts the blob once and the log names its digest. The mark is cleared by an upload of identical content (R-STOR-005's
repair), by a remote's refill of the same content, and by the recheck of a read once the mark is 30 s old ([Q-STOR-153](#q-stor-153--a-missing-mark-heals-by-itself-a-read-asks-the-store-again-once-per-blob-and-30-s)),
which finds an object that an operator restored, or that a store answering 404 for objects that exist (a wrong `storage.s3.prefix` that the start-up
check cannot see, because it writes its own probe below whatever prefix it is given, R-STOR-035) had hidden. Such a store marks every blob that is read
while it does; `QuaderBlobMissing` fires at the first, and the marks heal within 30 s of the next read after the prefix is corrected.
What counts as "no such object" is the client's mapping ([internal/blob/s3/errors.go](../../internal/blob/s3/errors.go)): the codes
`NoSuchKey` and `NotFound`, and any other `404` that is not `NoSuchBucket` or `NoSuchUpload`. So a gateway, load balancer or proxy in
front of the store that answers `404` while it is rolled out or misrouted marks every blob that is read meanwhile, exactly as a wrong prefix
does, and the remote repositories fetch those items again from their upstreams (Q-REPO-202). The marks heal by the recheck of Q-STOR-153
once the store answers again. A narrower mapping (`NoSuchKey` alone) was weighed and not taken: S3-compatible stores differ in the code of a
missing key, and a loss the mapping does not mark is a loss a remote cannot repair, because the commit of the same content deduplicates on
a row that says `ok` (R-STOR-005).

Not changed: the audit event that R-STOR-027 asks of every detection is not written, for `missing` as for `corrupt`: the store's damage hook
(`blob.WithDamageHook`) has no caller in the composition root.

**Rationale:** the mark is R-HA-024's own requirement, and the read is the only place where a loss is found before the scrub exists. The development
agent would revisit the look after the mark when the recheck of R-STOR-027 ships (the recheck could then clear a stale mark instead). A mark that
outlived its loss was found in review (amended 2026-11-06): Q-STOR-153 makes it heal.

**Applied in the spec:** R-STOR-012 (the first read marks), R-STOR-027 (a read is a detection). Code: `internal/blob/download.go` (`markMissing`,
`markDamaged`). Tests: `TestDownloadVerification` (`internal/blob/download_test.go`), `TestR_HA_024_ALocalTarballWhoseObjectIsMissingIs503AndItsBlobIsMarkedMissing`
(`test/integration/npm/remote_lost_test.go`).

### Q-STOR-152 · The chart renders QuaderBlobMissing, severity critical

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: [ha-and-helm.md](ha-and-helm.md) R-HA-045 lists `QuaderBlobMissing` (`increase(quader_storage_blobs_damaged_total{status="missing"}[1h]) > 0`,
for 0m) among the alerts the chart does not render yet. Until Q-STOR-151 nothing produced the counter outside a test. F-62 was an object store that
lost its data without anyone being told.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Render it now, severity `critical` (chosen) | One rule in `_rules.tpl`, scoped to the namespace, summed over the replicas | A lost object of a local repository is data loss; the operator learns of a loss from the first read | It also fires for a cache whose items are filled again at once |
| (b) Render it, severity `warning` | Same rule | Fewer pages for a remote that heals itself | Data loss of a local repository is not a warning |
| (c) Leave it with the other rows of R-HA-045 | Nothing changes | — | The counter would exist without anyone watching it |

**Choice:** (a). The counter has no repository label (a blob is shared by every item of the same content); the alert's description points to the
log lines that name the digest and, for a cached item, the repository and the path.

**Rationale:** an object store that loses objects is an incident whatever repository the object served. The development agent would revisit the
severity if remote caches are seen to page operators for losses that healed themselves.

**Applied in the spec:** [ha-and-helm.md](ha-and-helm.md) R-HA-045 (status). Chart: `deploy/helm/quader/templates/_rules.tpl`,
`deploy/helm/quader/tests/rules_test.yaml`, `test/chart/vuln_rules_test.go`.

### Q-STOR-153 · A missing mark heals by itself: a read asks the store again, once per blob and 30 s

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: review of Q-STOR-151. A store that answers 404 for objects that exist, for example a wrong `storage.s3.prefix` that an operator corrects an
hour later, marks every blob read in that hour `missing`; R-STOR-012 then answers every read of them from PostgreSQL, and before this decision only an
upload of the same content cleared a mark (the scrub's recheck is P1 and not built). Before Q-STOR-151, correcting the prefix healed everything: a
mistake that is otherwise reversible would have left damage that does not go away. The owner's rule is reliability first.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A read of a blob marked `missing` asks the store again once its last existence check is 30 s old; the claim of the look is a conditional `UPDATE` of the row's `checked_at` (chosen) | `ClaimMissingRecheck`: `checked_at` moves only when it is 30 s old, so one read per blob and interval asks, across replicas; `HeadObject`; found with the row's size, `MarkBlobOK` and the read serves it; not found, the interval starts again | One look per blob and 30 s whatever the number of replicas and reads; the column exists (`checked_at`, "last existence check", R-STOR-027), so no migration; the scrub that orders by it later sees the reads' checks | A read of a missing blob costs one statement more (an `UPDATE` that matches nothing inside the interval) |
| (b) A bounded map of last-check times per replica | Each replica keeps when it last asked about a blob | No statement per read | n replicas ask n times per interval, the map forgets under pressure, and a replica that starts asks at once |
| (c) Ask the store on every read of a missing blob | No short circuit for `missing` | Heals at the first read | A lost popular object is a request storm against the store, which R-STOR-012's short circuit exists to prevent |
| (d) Keep the mark until an upload or the scrub's recheck | Q-STOR-151 as it was | The store is never asked about a missing blob | The reversible mistake above becomes permanent damage |

**Choice:** (a). The interval is a constant, `blob.MissingRecheckInterval`, 30 s: the `Retry-After` a client is given for a missing object, so the
client that retries is the one whose read looks. No setting: nothing in the spec asks to tune it, and 30 s bounds both the store's load (one
`HeadObject` per lost blob and interval) and how long a healed blob is still refused. The detection itself (`MarkBlobDamaged`) sets `checked_at`, so
the interval starts at the mark, and the look after the mark of Q-STOR-151 is not followed by a second one. A `corrupt` blob is not rechecked: a
`HeadObject` cannot tell whether its bytes are right.

Where it runs: every read that would refuse a blob marked `missing` from PostgreSQL. `items.Store.Usable` is `ContentError` with the recheck; the
generic layer's `Stat` (`/files/`), the format host's `Serve` (every format's download, the remote hit, the cache view and the tiers of a virtual
repository) and the mirror source of the vulnerability scanning call it. The remote engine classifies a marked artifact with it and a marked document
with `blob.Store.RecheckMissing` before it counts either as lost ([repositories-and-proxy.md](repositories-and-proxy.md) Q-REPO-202): a cached item
whose object is back is served without an upstream fetch. `HEAD` and conditional requests of a missing blob, which ask nothing of the store
otherwise, may make that one look per interval. The engine of a `View` (the explanation on `api`) asks no store.

The same heals the marks that a `404` of something in front of the store left (a gateway during a rollout, Q-STOR-151): within 30 s of
the first read of each blob after the store answers again.

**Rationale:** a mark is a statement about the store at one moment; it has to be checked again, and the column for that already exists. The
development agent would revisit the interval if a store's outage that answers 404 is seen to make the looks a load, and would move the recheck into the
scrub's existence check when R-STOR-027's scrub ships, keeping the read's look for the blobs the scrub has not reached.

**Applied in the spec:** R-STOR-012 (errors), R-STOR-027 (repair), Q-STOR-151 (consequences), [ha-and-helm.md](ha-and-helm.md) R-HA-024. Code:
`internal/blob/download.go` (`RecheckMissing`, `MissingRecheckInterval`), `internal/meta/postgres/queries/blob.sql` (`ClaimMissingRecheck`,
`MarkBlobDamaged`), `internal/items/read.go` (`Usable`), `internal/files/download.go`, `internal/formathost/ingest.go`, `internal/vuln/sync/mirror.go`,
`internal/proxy/artifact.go` (`classify`), `internal/proxy/metadata.go`. Tests: `TestDownloadVerification` (`internal/blob/download_test.go`),
`TestR_HA_024_AMissingMarkHealsOnceTheObjectIsBack` (`test/integration/npm/remote_lost_test.go`), `internal/proxy/lost_test.go`.

### Q-STOR-154 · Blobs are deduplicated across projects

Status: **Decided by the owner on 2026-11-06.**

Context: every repository belongs to exactly one project and no repository is shared
([Q-AUTHZ-206](authorization.md#q-authz-206--every-repository-belongs-to-exactly-one-project)); a public registry is proxied by
a remote repository in each project that needs it
([Q-REPO-212](repositories-and-proxy.md#q-repo-212--a-repository-is-addressed-inside-its-project-and-no-repository-is-shared)).
The blob store keys a blob by its SHA-256 alone (R-STOR-001) and the commit protocol finds an existing blob before writing one
(R-STOR-005), so identical content is stored once whichever repository holds it. S3 itself keeps one object per key and does
not deduplicate.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Global, as built (chosen) | One blob per content in the installation, referenced by any number of items in any number of projects | The largest saving: a public package that several projects proxy is stored once | A deduplication hit writes nothing to S3, so its answer can come sooner; who already holds a file can learn whether another project holds it too ([H-552](#h-552--a-deduplication-hit-across-projects-can-be-told-apart-by-its-timing)) |
| (b) Per project | The digest of a blob is salted per project; identical content in two projects is two blobs | No signal across projects | Every public package is stored once per project that proxies it; the salt has to travel with every reference, backup and restore |

**Choice:** (a), the owner's, as the store works today. Deploy by checksum keeps its rule: it creates an item from a stored
blob only for a caller who may read an item that references it (R-STOR-008), so knowing a digest is never enough. The
signal that remains concerns content the caller already has; for a public package it tells nothing.

**Applied in the spec:** R-STOR-001 and R-STOR-005 unchanged; the gap H-552.

### Q-STOR-155 · A project is charged the logical size of what it holds

Status: **Decided by the owner on 2026-11-06.** Built: [R-STOR-049](#r-stor-049--the-storage-of-a-project); what is charged, and what it does not do yet, is [Q-STOR-158](#q-stor-158--a-projects-figure-is-the-sum-of-its-repositories-folded-counters-and-reports-only).

Context: blobs are deduplicated across projects ([Q-STOR-154](#q-stor-154--blobs-are-deduplicated-across-projects)), so the bytes
a project holds and the bytes the object store keeps for it differ. The project's dashboard and later quotas need one figure.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Logical (chosen) | A project counts the full size of every file it holds, whether another project holds the same content | Stable: another project's upload or delete does not move it; easy to explain; a fair base for a quota | The projects' figures add up to more than the store keeps |
| (b) A physical share | Each blob's size is split among the projects that reference it | The figures add up to the store's | A project's figure moves when others upload or delete; unpredictable as a quota |
| (c) The first uploader | A blob counts for the project that stored it first | Adds up | Others get content free; the charge moves when the first one deletes |

**Choice:** (a). The physical figure stays instance-wide: the admin dashboard shows what the store keeps (R-OPS-004's overview,
from the maintained totals), the project shows the logical size of its files, versions in the trash can included until they are
removed.

**Applied in the spec:** R-STOR-049 (the figure and its read path), R-STOR-029 (a project has no quota of its own), Q-STOR-158 (how it is computed and what is left).

### Q-STOR-158 · A project's figure is the sum of its repositories' folded counters, and reports only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: Q-STOR-155 decides that a project shows and is charged the logical size of the files it holds, and says the figure is the base of later
quotas. The folded counters of R-STOR-003 exist per repository only, count untrashed rows, and R-STOR-029 enforces a repository quota on them.
Three things were open: where the project's figure comes from, whether charging already refuses uploads, and whether versions in the trash can count
as Q-STOR-155 says. The trash can (R-STOR-022) is not built; a delete removes the rows.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Sum of the repositories' folded counters on read; reporting only (chosen) | `ProjectList` and `ProjectGet` add `repository_usage.logical_bytes` over `repositories.project_id`; no refusal | No new state: nothing to fold, recount or restore; a move needs no bookkeeping because the counters follow the repository ID; the project figure and its repositories' figures always agree; one primary-key read per repository of the project, not per item | A list of all projects reads every repository's counter row (bounded by the number of repositories); an admission check on the sum would cost a read per repository of the project, not one |
| (b) A counter per project, folded like the repository's | `project_usage` beside `repository_usage`, deltas carry the project | One row per admission check | A move has to transfer the share between two rows in the move transaction, a restore recounts a second table, and the figure can disagree with the repositories' |
| (c) (a) plus a project quota enforced on upload | A setting per project and a `507` naming it | A project is bounded, not only reported | Q-STOR-155 names quotas as later and does not decide their setting, error shape or who may set them; every upload would sum its project's repositories |
| (d) (a) plus a counter of trashed bytes now | The deltas and `repository_usage` also carry the bytes of trashed rows | Matches the text "trash included" of Q-STOR-155 | Nothing can be trashed yet, so the column would be zero everywhere; the fold and its tests change for a feature that does not exist |

**Choice:** (a). Charged is reported: "is charged" in Q-STOR-155 is the figure a project shows and the base a quota will read, not a refusal.
The figure counts the untrashed rows, as the repository counters do; today that is every file, because the trash can does not exist.

**Revisit when:** the trash can is built (R-STOR-022): then `repository_usage` gets the bytes of trashed rows, the project's figure adds them as
Q-STOR-155 says, and R-STOR-029's repository quota keeps reading the untrashed ones. A project quota is wanted: then (b), so that an admission
reads one row, together with the quota's setting and error shape.

**Applied in the spec:** R-STOR-049; R-STOR-029 states that a project has no quota; Q-STOR-155 is built.

### Q-STOR-162 · The usage recount is a SQL function, and the collector's hold check is a generated query

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The restore recomputes the counters in Go | Queries over `items` and `blobs` in the restore | No migration | The counters and the triggers that maintain them are the migrations' (R-STOR-003); a second description of the sums in Go would drift |
| (b) `storage_usage_recount()` in migration 00046 and `BackupBlobHeld` in `queries/backup.sql` (chosen) | The function deletes the deltas and recomputes `repository_usage` from untrashed items and `storage_usage_totals` from live blobs in the caller's transaction; the query is the statement of R-BAK-007 that the per-blob transaction runs as step 2b | The sums stay next to the triggers; a test compares the function with the triggers' results; the collector's work package calls one named query | A function in the schema, which `quader migrate` has to apply before a restore can run it |

**Choice:** (b).

**Rationale:** R-STOR-003 and R-STOR-047 name the function but no migration created it. The development agent would revisit this when a counter is added to the fold, which changes the function in the same migration.

**Applied:** R-STOR-003 "Recount", R-STOR-024 step 2b, R-STOR-047; `internal/meta/postgres/migrations/00046_backup.sql`, `internal/meta/postgres/queries/backup.sql`, `test/integration/postgres/backup_runstore_test.go`.

### Q-STOR-166 · The avatar image is a blob of the object store, not a column

Status: Decided by the development agent — selected from the options below; the owner reviews it at the walkthrough.

Context: the owner chose "als Blob gespeichert" for the uploaded avatar (R-API-055) and left to the development agent whether that means the content-addressed blob store (INV-4) or a `bytea` column in PostgreSQL. The image is a PNG of 256 x 256 pixels, a few KiB for a drawing and up to about 100 KiB for a photograph.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A blob of the blob store; a row names it by digest (chosen) | Spooled, hashed and committed by the protocol of R-STOR-005, deduplicated, read back verified; `user_avatars.blob_sha256` has the foreign key and the reference triggers | One rule for stored content: a backup exports the blob rows that the snapshot's rows reference (R-BAK-004), the collector deletes a blob nobody references (R-STOR-024), the scrub checks it (R-STOR-027), the read is verified (R-STOR-013); the same image uploaded by two users is one object; the image does not load PostgreSQL's buffers and WAL with 100 KiB per user; matches the owner's word and the UI asset store (Q-HA-120) | A read is a database lookup and an object-store `GET` (the browser caches it for a year, Q-API-209); an object store outage makes avatars unavailable (`503`), not the user records |
| (b) A `bytea` column of `user_avatars` | The image in the row | One system; a transaction covers row and bytes; no collection | The backup of the table carries the images through `COPY`; a replica reads 100 KiB through a pooled connection per avatar; a second place that stores content, with none of the blob store's verification, deduplication or scrub; TOAST and WAL growth for a cosmetic feature |
| (c) The image in the row for small ones, in the store for large | Both | Fast for small | Two paths to test and to keep consistent; the output is at most about 260 KiB anyway |

**Choice:** (a). The blob is addressed by the SHA-256 of the PNG the server made, which is also the avatar's URL (Q-API-207). The object lives under the blob store's own key (`<prefix>/sha256/<h0h1>/<h2h3>/<digest>`), not under a prefix of its own: nothing distinguishes it from other content, and the references decide when it goes. The row is the whole of the avatar's metadata: user, digest, size, time of the change.

**Rationale:** the question is not where 100 KiB fits but how many rules for stored content the installation should have. (a) reuses all of them and adds one table; the cost, an object-store read per avatar, is paid once per browser per avatar. The development agent revisits this if avatars become the dominant object-store reads of an installation (a metric `quader_http_requests_total` for the route would show it) and a per-replica cache of the 256 px images is wanted.

**Applied:** R-API-055 (Storage); `internal/meta/postgres/migrations` (`user_avatars`), `internal/avatar/store.go`. Test: `TestR_API_055_AvatarLifecycle` (the object is read back from the bucket).

### Q-STOR-167 · A row names the avatar, the blob reference rules keep and collect it

Status: Decided by the development agent — selected from the options below; the owner reviews it at the walkthrough.

Context: a blob that a row references must not be collected (R-STOR-003), and one that no row references must be (R-STOR-024). An avatar's blob is referenced by one or more `user_avatars` rows (identical images deduplicate); it is replaced by an upload, dropped by a removal and by the deletion of the user (`ON DELETE CASCADE`).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The reference rules of R-STOR-003, unchanged: `blob_sha256` with the foreign key and an index, the guard trigger, the two release triggers; the table is `data` (chosen) | The migration installs the three triggers, as for `items` and `ui_assets`; a replaced, removed or cascade-deleted row queues its blob in `blob_gc_queue`; the collector deletes it after its checks | A new table needs no code in the collector: it reads the catalog of columns named `blob_sha256` that reference `blobs` (`postgres.LoadBlobRefs`), so the new reference is seen without an edit; no leak path | The release trigger needs an `id` column for the update case |
| (b) Delete the object in the request that replaces it | `DELETE` of the object after the commit | Immediate | Races the other user who uploaded the same image; breaks R-STOR-024's rule that only the collector deletes |
| (c) Class `transient` | Avatars are gone after a restore | Smaller backup | A restore silently removes every user's picture; the owner's data, not a cache |

**Choice:** (a). The upload commits the blob and the row in the link transaction of the commit protocol (R-STOR-005): the guard trigger proves the blob live under the commit's lock, so a collector that races the upload cannot delete what the row now references. The write of the same image again changes nothing (`WHERE blob_sha256 <> EXCLUDED.blob_sha256`). The table is registered `data` in `internal/backup/tables` (`tables_avatar.go`); its blobs travel with it as the blobs of every `data` table's references do (R-BAK-004; the backup job itself is not built yet). A replaced blob is queued and deleted by the collector after `storage.gc.delete_settle` and its reference check; until then the bytes are in the bucket but no route serves them (H-561).

**Rationale:** the avatar is the third user of the reference rules after items and the UI asset store, and costs a migration and a registration. The development agent revisits this if a second table needs the same "one blob per owner" shape: then the pattern is a helper, not a third copy.

**Applied:** R-API-055 (Storage); migration `user_avatars`; `internal/meta/postgres/tables_avatar.go`; `internal/meta/postgres/queries/avatars.sql`; [backup-and-restore.md](backup-and-restore.md) R-BAK-004 (the table is `data` by default). Tests: `TestR_API_055_AvatarLifecycle` (the replaced blob and the blob of a deleted user are queued), the migration test that every table is registered.

### Q-STOR-165 · R-STOR-030 to R-STOR-033 hold for the backup target's client as they do for the blob store's

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The target is configured like `storage.s3`, with every key | `backup.target` repeats the transport, retry and checksum sections | Full control per target | A large section for a client that runs for minutes a day; the chart renders it twice |
| (b) The target takes the endpoint, TLS, credential and encryption keys of `backup.target` and the transport defaults of `storage.s3` (chosen) | `s3.NewTarget` builds a `config.StorageS3` from `backup.target` with the default timeouts (5 s dial, 30 s response header, 60 s idle transfer), five attempts with a 20 s backoff cap and `when_supported` checksums; `backup.target.ca_file` is the target's whole trust store | The rules of R-STOR-030 (https, normalized endpoint, own CA), R-STOR-031 (credential sources, file rotation) and R-STOR-033 (timeouts, stall detector) apply by construction | A target behind a proxy or with a tuned retry has no key for it until one is added to `backup.target` |

The runtime credential is never read: the target's credential comes from `backup.target.credentials` and the SDK default chain of the backup pod's environment, and the target section is not derived from `storage.s3` (R-BAK-015).

**Choice:** (b).

**Rationale:** The second client needs the same hardening as the first, and the backup pod is the only place it runs. The development agent would revisit this when an operator needs a proxy or other transport settings for the target.

**Applied:** R-STOR-030, R-STOR-031, R-STOR-033, R-BAK-015; `internal/blob/s3/target.go` (`targetStorage`), `internal/blob/s3/client.go` (`factoryClient`).

### Q-STOR-164 · The blob sink of a restore verifies by hash when it streams and by size when the store copies

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Re-read every written object and hash it | A full read of the blob store after the copy | Catches a tampered pool object | Doubles the transfer of a restore that is bounded by transfer |
| (b) Hash while streaming, check the size after a server-side copy (chosen) | `s3.BlobSink.Put` hashes the stream and never completes a multipart upload or sends a single request whose SHA-256 differs from the digest; `CopyFrom` confirms the size with `HeadObject` | A restore costs one pass; a streamed object is never made visible with wrong bytes | A pool object altered in place keeps its size and passes a server-side copy (H-558); `quader backup verify --deep` is the check |

**Choice:** (b). A part is buffered in memory (`storage.s3.multipart.part_size`), an object up to `multipart.threshold` is buffered whole; no spool file is used.
**Rationale:** A restore must finish at the speed of the store. Revisit if the store reports the full-object SHA-256 of a copy: the sink would compare it, as R-BAK-006 requires of the create run.
**Applied:** R-BAK-006, R-BAK-021 step 3, R-STOR-047 (the recount after the load); `internal/blob/s3/blobsink.go`.

### Q-STOR-163 · The collector's hold check is part of its claim transaction, and a valid global hold ends a run before its candidates

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Keep the `Hold` hook of the collector and let the composition root set it | Any function decides | Testable with a fake | Nothing sets it; a collector built without it deletes blobs a running backup holds |
| (b) `BackupBlobHeld` is called by the claim itself, after the reference check and before the state change, as a statement of its own (chosen) | The hook is removed; the early exit reads `BackupGlobalHoldValid` after the interrupted deletions are finished and before the candidates | A collector cannot be built without the check; the order of R-STOR-024 steps 2 and 2b is in one function | The unit tests of the collector's ordering need the database (they already do) |

**Choice:** (b). A dry run counts a held blob as held. `GCResult.holdSkipped` says that a run ended at the early exit. The batch form (`BackupHeldDigests`) is not used: the collector claims one blob per transaction.

**Rationale:** The proof of R-BAK-007 needs only that (2b) starts after (2) of the same blob, which the single transaction gives. The property test of `test/integration/backup/create_hold_test.go` fails when the check is removed. The development agent would revisit this if the collector claims batches in one transaction.

**Applied:** R-STOR-024 step 2b; `internal/blob/gc.go`.

### Q-STOR-168 · The requests whose answer comes when the store has finished the work have no response-header timeout and no byte-idle watchdog, and the idle watchdog of a download measures the network only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One HTTP client and one watchdog rule for every request | The 30 s response-header timeout and the byte-idle watchdog apply to copies and completions as to uploads | One rule | A completion or a copy has no body to feed the watchdog and sends no header until it is done; the rule cancels the longest legitimate requests |
| (b) A second HTTP client for these requests and a deadline derived from the size (chosen) | `Client` built by `NewFromFactory` holds `copyAPI`, the same settings with `response_header_timeout` 0; `CopyObject`, `UploadPartCopy` and `CompleteMultipartUpload` use it under a deadline of 2 minutes plus the size at a conservative rate; the download watchdog is armed only while a read waits for the network | A slow assembly or copy completes; a stuck store is still cut off; a consumer that is busy elsewhere (R-BAK-021 step 3 with all workers busy) does not fail its source download | A client built from a bare HTTP client (`New`) keeps one client; tests only |

**Choice:** (b).

**Rationale:** R-STOR-033 asks that transfers fail when they stall, not when they are long; for a request without a body, "stalled" is only decidable against the size. The development agent would revisit this if the store is found to send keep-alive bytes during a completion, which would allow the byte-idle rule back for it.

**Applied:** R-STOR-033, R-BAK-006; `internal/blob/s3/client.go` (`copyAPI`, `CompleteMultipart`), `internal/blob/s3/idle.go` (`body.Read`), `internal/blob/s3/target_copy.go`.

### Q-STOR-170 · The collection run aborts a live session for its idle time and an open one for its age

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every live session by idle time and age, as the text of R-STOR-011 read | `completing` and `assembled` sessions are aborted when older than `session_max_age` as well | One rule | A finalize that began just before the age limit and is assembling or committing is aborted by the run under its feet: the copy fails and the client restarts a 100 GB upload |
| (b) Idle time for every live session, age for the open ones only (chosen) | A session that is `completing` or `assembled` is aborted when no step was recorded for `session_ttl`; an `open` one also when older than `session_max_age`. A finished or aborted session loses its objects at the next run (a finished one when its idle time has passed, so that a repeated closing request still answers `201`), and then its row | The run never ends a finalize that is making progress; every request still refuses an open session past its age, so no session is continued beyond the multipart expiry | A stuck finalize that is older than `session_max_age` stays until its idle time, at most `session_ttl` after the last step |

**Choice:** (b).

**Rationale:** the bound that protects the multipart upload (`session_max_age` below `multipart_max_age`) matters for the bytes still arriving, which only an `open` session has; a finalize has them all and records a step (and `last_activity_at`) every time it advances. The development agent would revisit this if a finalize can stay in `completing` for hours (a copy of a very large blob), which would call for a heartbeat instead of the idle time.

**Applied:** R-STOR-011 *Expiry*; `internal/meta/postgres/queries/uploads.sql` (`UploadExpire`, `UploadListCleanup`, `UploadDeleteCleaned`), `internal/blob/gc_uploads.go`. Tests: `TestR_STOR_011_TheCollectionRunExpiresAbandonedSessions` (integration).

### Q-STOR-171 · A session ends in the transaction that links the item

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `done` after the commit, as a step of its own | The commit transaction links the item; a second statement sets `done` and the objects are deleted | Simple | A finalize cut between the two repeats the commit (it finds the blob and the item, and changes nothing), and the session is `assembled` with an item that exists |
| (b) `done` inside the commit transaction (chosen) | The commit protocol's link function ends the session in the transaction that links the item (`CommitSpec.Extra`, `uploads.CommitFunc`'s `markDone`); a session aborted meanwhile makes the statement affect no row and the commit roll back | The item and the end of the session are one commit; no state where an item exists and its session is not done | The host's commit takes a step of the caller (`ingestor.commit`) |

**Choice:** (b).

**Rationale:** it removes a state that only a cut between two statements could produce, and it makes an abort during a finalize win cleanly: the item is not written for a session the client cancelled. The objects are deleted after the commit and a failure of that is left to the run.

The session also records the item it committed (`item_id`, from the commit's `Extra` step), so that a repeated closing request after `done` is answered from the record: it commits nothing, records no second decision and re-creates no item that was deleted meanwhile.

**Applied:** R-STOR-011 *Finalize* step 4; `internal/uploads/finalize.go` (`complete`), `internal/formathost/uploads.go` (`Finalize`), `internal/formathost/ingest.go` (`commit`). Tests: `TestR_STOR_011_ACutFinalizeIsRepeatedOnAnotherReplicaAtTheRecordedState`, `TestR_STOR_011_FinalizeCommitsTheStagedBlobInTheTransactionThatEndsTheSession`.

### Q-STOR-172 · The full collection sweep removes what no session stands for until the orphan sweep is a task of its own

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Wait for the orphan sweep of R-STOR-026 | Leftovers of the staging area (a replica killed between creating the multipart upload and recording the session, the sessions of a deleted repository) stay until the sweep exists | No second place | The sweep is not built; the leftovers stay for good, and the bucket's lifecycle rule is the only remover |
| (b) The session step of the full collection sweep removes them (chosen) | `gc-full` lists `_uploads/<id>/` directories, reads which ids have a row, and deletes the objects of the others and aborts the multipart uploads of the others (and of finished sessions), if they are older than `session_max_age`; a directory that is not a UUID is logged and left | The staging area has a remover now; the code is the pass R-STOR-026 describes for this prefix and moves into the sweep when it exists | `gc-full` runs weekly by default, so a leftover lives up to a week beyond its age |

**Choice:** (b).

**Rationale:** the age is `session_max_age`, not `multipart_max_age`: an object of an id without a row can only be a session that is over or never started, and a session's row is recorded before any object of it exists (the multipart upload is created with the first part, and recorded with it within `recordTimeout`), so an object or an upload older than `session_max_age` that no row names is not a session that is starting: the cutoff on the object's own age is what keeps the sweep out of the reach of one that is. The development agent would revisit this when the orphan sweep is built: the pass moves there unchanged.

**Applied:** R-STOR-011 *Expiry*. Superseded by [Q-STOR-263](#q-stor-263--the-staging-sweep-moves-from-the-full-collection-to-the-orphan-sweep-per-connection-without-a-cursor-of-its-own)
since the orphan sweep is a task of its own: the pass moved there, per connection, and the cursor in `system_state` went with the page limit
it served.

The expiry and the cleaning go on batch after batch (200 rows) for a time budget of two minutes each per run, re-read the deletion pause
before every batch, and stop at a batch in which nothing moved; a backlog is the next run's, and a pause set during the step ends it.

### Q-STOR-173 · The sessions are counted by `quader_storage_upload_sessions_total`, not by an OCI metric

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `quader_oci_upload_sessions_total{outcome}`, as R-OCI-034 wrote it | The OCI handlers count what they see | The name the OCI page already has | They see neither the sessions the collection run expires nor those the service aborts (a size limit, a damaged tail); the counter would need the service to call the format, or the format to count what it cannot see |
| (b) `quader_storage_upload_sessions_total{outcome}`, counted where the sessions end (chosen) | `completed` when a finalize commits, `aborted` when a session is cancelled, fails or exceeds its size, `expired` when a request refuses an open session past its age or the collection run aborts one; each counted once, by the call that changed the state | One counter for every format that will use the sessions; no cross-package call; the family sits beside the other `quader_storage_*` families of `blob.Metrics` | R-OCI-034 names another family; the OCI page points here |
| (c) Both | The service counts, an OCI alias | One name per audience | Two series for one fact |

**Choice:** (b).

**Rationale:** the sessions are the storage mechanism of R-STOR-011, used by one format today and by a generic resumable upload tomorrow; the outcome is decided in `internal/uploads` and `internal/blob`, and a format-named counter would have to be fed from outside. The label is `outcome` only; a format label would be unbounded in nothing, but no reader asks for it yet. The development agent would revisit this if an operator needs the sessions per format.

**Applied:** [formats/oci.md](formats/oci.md) R-OCI-034 (the row names this family); `internal/blob/metrics.go` (`UploadSession`), `internal/uploads` (`complete`, `Abort`, `abortAndClean`, `lookup`), `internal/blob/gc_uploads.go` (`expireSessions`). Tests: `TestR_STOR_011_SessionsAreCountedByHowTheyEnded`.

### Q-STOR-174 · The tail is a list of segments that are read once, and the multipart upload is created with the first part

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One tail, rewritten by every request (the first cut) | A request reads the whole recorded tail back, appends its bytes and writes a new tail | Few objects | A client that sends one byte per request makes the replica read and write up to a part (16 MiB) and hold it in the spool each time: about 10^7 times the request's bytes, for a few cents of its own; a stalled second byte holds the spool |
| (b) A minimum size for a chunk that is not the last | A shorter `PATCH` is refused | Nothing is read back | The OCI distribution specification lets a registry announce a minimum with `OCI-Chunk-Min-Length` and does not make a refusal of a shorter chunk a conformance rule; Docker and Podman send what their layer buffer holds, and a client that ignores the header would fail |
| (c) Segments (chosen) | The body is read first. A body shorter than what is missing for a part is written as one more tail segment, with its size and SHA-256 in a row of `upload_session_tails`, and nothing is read back. The request whose bytes complete a part reads the segments once and joins them with its bytes into the part. A tail with 32 segments is merged by the request that would add the 33rd | Each byte is written once and read once; a request's spool is its own bytes plus the part it completes; the amplification is one merge of at most a part per 33 requests | More rows and objects per session (at most 32 segments per part); the segments are checked one by one |

**Choice:** (c), with the multipart upload created when the first full part is sent and its id recorded with that part.

**Rationale:** the cost of a request is then bounded by its own bytes except for the request that completes a part, whose bytes pay for the read, and the merge, which is one read of at most a part per 33 requests: an attacker's cost-to-effect ratio is the part size over 33, not over 1. The lazy multipart upload removes the only S3 request `Start` made, so that a session that is started and not continued costs a row (the open sessions of an attacker no longer fill the staging area of the store, only the table: H-590). The development agent would revisit the cap of 32 if the merge shows up in the profile of a real client. The claim is extended before the segments are read, because a request that lost it may find them removed by the request that completed the part, and then stops with 416 instead of failing the session.

**Applied:** R-STOR-011 *State*, *Stream*, *End of body*, the schema block; migration 00057, `queries/uploads.sql`; `internal/uploads/append.go` (`stream`, `endOfBody`, `joinTails`), `internal/uploads/finalize.go` (`tailAsPart`, `completeSmall`). Tests: `TestR_STOR_011_SmallChunksAreWrittenOnceAndReadOnceByTheRequestThatCompletesAPart`, `TestR_STOR_011_ASessionCostsNoObjectBeforeItsFirstPart`.

### Q-STOR-175 · A part upload is sent under a freshly renewed claim and ends before its lease does

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Rely on the store to refuse a stale part at the completion | Only the record of a part is conditional on the claim | Nothing to add | A request that kept uploading past its lease sends its part under a number the recorded request used; a store that assembles the last upload of a number whatever ETag is named would store the stale bytes under the digest of the recorded ones (a poisoned layer of the public deduplicated store) |
| (b) Renew the claim before the segments are read and right before each `UploadPart`, and give the upload a deadline of three quarters of the lease (chosen) | `UploadRenew` is conditional on the writer token; a request that lost the claim reads no segment (another request may have removed them) and sends nothing; the client side of an upload that outlasts its deadline is cut; the check of R-STOR-035, run at every start, requires that a store assembles the part that is named or refuses | A request that lost its claim does not send a part after the recorded one from its side; a store that had received the whole body before the cut can still store it, and the part-identity rule is what keeps that from the blob; a store that does not hold the rule stops the replica at start | `writer_lease` has to exceed the time one part takes (a 5 GiB part within three quarters of 60 s needs about 119 MB/s); startup refuses a configuration whose part cannot be sent at 32 MiB/s within that time |
| (c) A heartbeat during the upload | A goroutine renews the lease while the part is sent | Long parts fit any lease | A cut upload can still land after a late failed renewal; more moving parts |

**Choice:** (b). A finalize that sends the tail as the last part takes a claim of its own for that step (`UploadClaimCompleting`), so that two continuations do not upload it twice; the one that does not get it is told to retry.

**Rationale:** the store's behaviour is not under Quader's control, the deadline of the client side is; the rest is the part-identity rule, which the startup check enforces. SeaweedFS 4.51 keeps both uploads of a part number and assembles the one whose ETag is named, which the check and the integration test pin. The development agent would revisit (b) for a deployment whose parts are larger than its lease allows, which (c) would serve.

**Applied:** R-STOR-011 *Stream* step 3 and *Finalize* step 2; R-STOR-035; `internal/uploads/append.go` (`sendPart`, `partDeadline`), `internal/blob/conformance.go` (`checkPartIdentity`), `internal/config/storage.go` (the rules `writer_lease` below `session_ttl` and a part sendable at 32 MiB/s within three quarters of the lease), `StartupCheck` (runs `checkPartIdentity`). Tests: `TestR_STOR_011_APartUploadCannotOutliveItsClaim`, `TestR_STOR_011_AStaleWriterSendsNoPartAfterTheRecordedOne`, `TestR_STOR_011_TheStoreAssemblesThePartThatIsNamedWhenANumberIsSentTwice`.

### Q-STOR-176 · The open sessions of a principal on a repository are capped, an ended session leaves no row, and the session step has a time budget

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Only the collection run bounds the sessions (it was 25 batches of 200 per step and run) | A client that starts sessions faster than the runs remove them grows the table without limit | Nothing to configure | The ceiling was about 240,000 sessions a day at the default schedule; a holder of `deploy` that starts 50 a second grows the table by four million a day, and the cleaning sorts every aborted row for each batch |
| (b) A cap on the open sessions of one principal on one repository, at `Start`; the request that ends a session removes its row; a time budget and an index for the run (chosen) | `storage.uploads.max_open_sessions` (100): the start takes the lock of the repository row, counts the live sessions (`open`, `completing`, `assembled`) of the principal on it and inserts none above the cap, in one transaction, so that racing starts hold the cap exactly; it answers 429 with `Retry-After`. `Abort` and every failure that ends a session remove its objects and, when that succeeded, its row; a finished session stays for a replay window of 15 minutes and is marked `cleaned` when its request removed the objects. The run deletes rows that need no call to the store (`cleaned`, or nothing staged) in bulk, removes the objects of the others, and moves a session it could not clean to the back of the line. Expiry and cleaning each run batches of 200 for two minutes | The sessions one caller holds *at once* are bounded where they start, and a caller that starts and cancels in a loop leaves no row behind; the removal of the others needs no ceiling | Principals and repositories each have their own cap, so many of either still grow the table (H-590); a caller that starts and abandons sessions fills its own cap for `session_ttl` (below); the lock serializes the starts of one repository |
| (c) A global cap | The installation refuses starts above a number | A hard bound | One caller can use it up for all; it needs a counter that survives replicas |

**Choice:** (b).

**Rationale:** the cap is the bound where the work is created, and the immediate removal keeps a start-and-cancel loop from growing the table at all, which the run alone could not (it removes at a rate, an attacker starts at another). What the cap does not bound is a caller that starts sessions and leaves them open: each holds a place for `session_ttl` (4 h), so one principal blocks itself, and, for an **anonymous** caller, every anonymous caller of the repository, with 100 starts per 4 h (H-590, H-592). A CI job is keyed on its token (`Subject.TokenID`: each exchange issues one), so the sessions of one token do not fill the cap of another token under the same mapping; a job can hold several tokens (several id_tokens), and an issuer that does not make an id_token single-use lets one be exchanged several times, so the token is a finer key than the mapping and not a guarantee of one cap per job. A shorter idle time for sessions that never received a byte was considered and not taken: a caller that sends one byte per session keeps it at `session_ttl` anyway, and the extra key would be a second knob for the same exposure. The index on `(repo_id, COALESCE(principal_id, all-zero uuid))` serves the count (`IS NOT DISTINCT FROM` cannot use an index; a session without a principal is the all-zero uuid for the count), and `(state, last_activity_at)` serves the listing in order; `TestR_STOR_011_TheCapCountAndTheCleaningListingUseTheirIndexes` runs `EXPLAIN` over 60,000 rows and requires both indexes and no sort. The locks of a start are taken in one order, a key share on the addressed repository (a virtual one whose configuration may be updated at the same time locks its row and then its members) before the lock on the target row, so a start through a virtual repository and the update of its configuration never wait for each other in a cycle (`TestR_STOR_011_AStartThroughAVirtualRepositoryDoesNotDeadlockWithItsConfigurationUpdate`); an advisory lock is not used (R-ARCH-017). The development agent would revisit a global cap, or a per-endpoint one for anonymous callers, if H-590 or H-592 shows up in a real installation.

**Applied:** R-STOR-011 (configuration, *Expiry*), `internal/uploads/service.go` (`Start`, `endAndRemove`), `internal/uploads/finalize.go` (`complete`), `internal/formathost/uploads.go` (`scopeOf`), `internal/meta/postgres/queries/uploads.sql` (`UploadLockRepository`, `UploadInsertSession`, `UploadListAborted`, `UploadListDone`, `UploadDeleteCleanedBulk`, `UploadMarkCleaned`, `UploadBumpActivity`), migration 00058, `internal/blob/gc_uploads.go` (`cleanSessions`, `inBudget`), `internal/config/storage.go`. Tests: `TestR_STOR_011_AStartAboveTheCapOfOpenSessionsIsRefused`, `TestR_STOR_011_ConcurrentStartsNeverExceedTheCapOfOpenSessions`, `TestR_STOR_011_AnAbortThatRemovedTheObjectsDeletesTheRowAndOneThatDidNotLeavesItForTheRun`, `TestR_STOR_011_ASessionOfACIJobIsKeyedOnItsToken`.

### Q-STOR-180 · A virtual repository of a format with a job-built index holds the generated items of its indexes

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the Debian format builds the indexes of a local and of a virtual repository by a job, as signed snapshots served by hash
(ADR-170). A virtual repository merges its members' indexes into one for every caller and signs it with its own key (R-DEB-025), so
the merged files are its own and cannot be rendered per request.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The virtual repository holds generated items in its own `items` (chosen) | Package-less items, written by the publication port; served on the format's routes; counted in usage | Every replica serves the same bytes; by-hash retention and `If-Modified-Since` work as for a local repository; one mechanism for both classes | A virtual repository holds content, the one exception to its definition |
| (b) A per-replica cache of the merged index | Render on demand | No stored content | Replicas could sign and serve different bytes; an index of tens of megabytes per request (R-DEB-026) |
| (c) Store the merged files in the format's tables | A blob reference outside `items` | No change of the item rules | A second reference rule for garbage collection (R-STOR-003 rule 1) |

**Choice:** (a).

**Rationale:** a virtual repository accepts generated items from the publication port and from nothing else (T-17's plan, WP8). The
development agent would revisit it if a second format needed a stored merged document.

**Applied in the spec:** R-STOR-003 (above), R-STOR-028 (the summary lists a virtual repository that holds generated items; built with the virtual class).

### Q-STOR-182 · Generated items are published through one port: blobs first, then one transaction

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a generation of an index is several files (the release documents, the indexes, their by-hash copies) that a client must see
together or not at all, and the same job writes rows beside them (the generation record, the suite's published numbers).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `items.Publisher.Publish(repo, files, remove, commit)` (chosen) | Blobs are stored outside any transaction; one transaction upserts the items, removes what is given up and runs `commit` with the transaction for the caller's rows; `ErrSuperseded` from `commit` rolls everything back | A reader sees a previous generation or the new one; a build of an older snapshot never replaces a newer generation; no format writes items on its own | The port is a new entry of the item store |
| (b) The format uses the commit protocol per file | One transaction per file | Existing code | A reader could see a mixture of two generations |
| (c) A staging prefix and a rename | Two-phase | No new port | Items have no rename that is atomic across paths |

**Choice:** (a). `modified_at` of every item of a generation is the `Release` date the caller passes, so two generations never share a
`Last-Modified` and a client's `If-Modified-Since` never gets a stale 304 (R-STOR-012).

**Rationale:** the port is declared with the contract of the Debian format and built for both classes in `internal/items/publish.go` (Q-STOR-210).
The development agent would revisit it if a second job-built format wanted a different atomicity.

**Applied in the spec:** R-STOR-003 (above), [formats/debian.md](formats/debian.md) R-DEB-010. Code: [publish.go](../../internal/items/publish.go).

### Q-STOR-190 · The delete job decides every batch again, on what the subject that asked holds then

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

R-STOR-018 decides `delete` on every item of a subtree at the request and finishes a subtree above `storage.delete.max_sync_items`
in the job `items-delete`. The job carried the subject of the request in full and decided only what a storage hook cascades to
(the tags of an OCI manifest, Q-OCI-76), on the groups, the token scope and the CI grants of the moment of the request; the items
of the subtree were never decided again (Q-STOR-143). Removing the caller from the group that granted `delete`, revoking the
token or disabling the principal therefore neither stopped nor narrowed a running or a queued job, and a job queued behind others
can start long after its request. The integration test `TestR_STOR_018_TheDeleteJobDecidesEveryBatchOnTheStandingOfItsSubject`
reproduced it: each of the three changes, made before the job's first batch, left the job to remove all 14 files.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The request's decision holds (Q-STOR-143) | The job removes what the request decided | One decision; the job holds no authority | A withdrawn token or principal keeps deleting until the job ends |
| (b) Decide the cascade only, on the subject as the request saw it (as built) | The job carries the whole subject and decides the paths a hook cascades to | The cascade stays inside the request's grants | The subtree is never decided again, and the frozen groups, scope and CI grants outlive every withdrawal |
| (c) Decide every batch again on the subject as it stands then (chosen) | The job carries who asked, with which credential, on which endpoint, and no grant; each batch reads what that subject holds now, takes the index installed now and decides on it `delete` on every item of the batch, the ownership question of R-AUTHZ-018 for its files and every path a hook cascades to | A withdrawal or a narrowed grant stops the job at the next batch, within `auth.revocation_bound`; an insert in flight at the request (H-154) is decided too | One principal lookup and one token or session row read per batch; a refusal leaves a partly removed subtree; a job fails where (a) would have finished |
| (d) Cancel the job when a change could concern it | A listener on the change areas `auth` and `authz` cancels the jobs of the principal | No work per batch | It needs a map from changes to jobs; a change that does not touch the subtree cancels too; a missed event lets the job run on |

**Choice:** (c). A batch is one transaction of the files (`storage.delete.batch_size`) or of the folders; the standing is read
once per batch, before its transaction, and every question of the batch is answered on one index. A folder is decided by its
path; the files below it were decided in their batches. The bound of Q-STOR-143 stays: the job removes only items up to the
highest id the request saw. A refused item, a principal that is no longer active and a withdrawn credential fail the job without
a further attempt (`jobs.Permanent`) with the 403 or the 401 a request would get now; a store that does not answer, or an index
that is not current, fails the attempt and the queue retries it. A job that carries a subject is never run unguarded: on a replica
without the guard it fails. What the job reads, and which ends of a credential stop it, are
[authorization.md](authorization.md) Q-AUTHZ-260 and Q-AUTHZ-261.

**Rationale:** the job is the continuation of the request, so it may do for the caller what a request of that caller may do at
that moment and no more. A request with a revoked token, or of a disabled principal, or after the group was left, is refused
within the revocation bound; a job that went on deleting for it would be the one path around that bound. The cost per batch is
the standing (a principal lookup, usually a cache hit, and one indexed read of the token or session row, plus one per
re-authentication a session went through), the index taken by `Begin`, and, in a repository of a format with package names, the
ownership question for every package the batch's files belong to: per package an `INSERT … ON CONFLICT` of the package row, its
lock `FOR UPDATE` and the reads of its owners, as the request did for the whole subtree. That is a few statements per package beside a
batch of thousands of row deletions. The development agent would revisit the refusal when the trash
can (R-STOR-022) makes a delete recoverable (a refused batch could then put back what the job took), or if the reads per batch
show up in the cost of a large delete.

**Applied in the spec:** R-STOR-018 (the job's bullet, two acceptance criteria) and the security note on delete jobs; [identity-and-authentication.md](identity-and-authentication.md) R-AUTH-023 (the bound for the job); Q-STOR-19 and Q-STOR-143 are superseded in
part; [authorization.md](authorization.md) Q-AUTHZ-260, Q-AUTHZ-261, R-AUTHZ-018 (the batches ask the ownership again), Q-AUTHZ-161 superseded in part; [formats/npm.md](formats/npm.md) R-NPM-017 (the raw delete);
[formats/oci.md](formats/oci.md) H-653; [file-transfer.md](../security/file-transfer.md) (the delete job, H-154 closed, H-658,
H-659). Code: `internal/items/delete.go` (`JobGuard`, `BatchGuard`, `batchGuard`, `refusal`), `internal/files/delete.go`
(`DeleteJobGuard`, `Standings`), `internal/authz/jobsubject.go`, `internal/auth/standing.go`, `internal/auth/token/standing.go`,
`internal/auth/session/standing.go`, `internal/meta/postgres/queries/login.sql` (`SessionStandingChain`),
`internal/app/wire_files.go`. Tests: `TestR_STOR_018_TheDeleteJobDecidesEveryBatchOnTheStandingOfItsSubject`,
`TestR_STOR_018_AWithdrawalBetweenTwoBatchesStopsTheJobAtTheNext`, `TestR_STOR_018_TheDeleteJobReadsTheSessionThatAskedFromItsRow`,
`TestR_OCI_018_ADeleteJobHoldsTheCascadeToTheGrantsOfTheCaller`, the unit tests `TestJobSubject`, `TestJobStanding`,
`TestDeleteJobGuard`, `TestStanding`.

### Q-STOR-191 · The delete job finds its repository by ID, and a repository that is gone ends it as done

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

The guard of Q-STOR-190 looked the job's repository up by the name the job carries, while the job removes by the repository's ID
([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-060: a running job holds its repository by the ID, which a move does
not change). A repository moved to another project made the lookup fail and the job retry; a repository created under the old name
in that time was then decided on, its grants, its classifier and its package owners, while the job went on removing the moved
repository's items. The guard also ignored the repository's `disabled` flag, which refuses a request's delete for every caller
without `content:bypass`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) By name, refusing a name that no longer leads to the job's ID | Compare the ID behind the name | One lookup | A moved repository stops every job of it, for a reason the caller cannot see |
| (b) By ID, deciding on the name the repository has now (chosen) | `repos.ByID(DeleteArgs.RepoID)`; `Begin`, the coordinate of every path and the events name the current name; NotFound or lifecycle `deleting` ends the job as done; a disabled repository stops it unless the batch's subject holds `content:bypass` | The job decides on the repository it removes from, where it is now; a move to a project the caller has no grant in stops the job there | A move is a narrowing the caller does not see until the job's error |
| (c) Refuse a move while a delete job runs | A check in the move | The question does not arise | One more coupling between the project and the item store; a job queued behind others blocks a move for its whole wait |

**Choice:** (b). `ErrRepositoryGone` is the guard's answer for a repository that is gone or being deleted; the job returns success,
because the repository's deletion job removes the rest of its items, and never retries. The disabled flag is decided on the batch's
index with `HoldsOn(content:bypass)`, as a request's delete in its strict form refuses a disabled repository to every caller without it.
The repository snapshot and the authorization index follow the change log each on its own, so for a moment after a move one can hold
another repository under the name the other gives: after `Begin` the guard compares the ID the index holds under the current name
(`Index.RepositoryID`) with the job's, and a difference fails the attempt, which the queue retries, instead of deciding the batch on
the other repository.

**Rationale:** the ID is what the job removes by, so it is what the job must decide on; deciding on a name is deciding on whatever
holds the name now. The development agent would revisit (c) if a move ever has to wait for the work of its repository.

**Applied in the spec:** R-STOR-018 (the bullet on the repository, an acceptance criterion). Code: `internal/files/delete.go`
(`DeleteJobGuard`), `internal/items/delete.go` (`ErrRepositoryGone`, `BatchGuard.Name`, `RunDeleteJob`), `internal/authz/project.go`
(`Index.RepositoryID`, `Engine.RepositoryIDOn`). Tests: `TestR_STOR_018_TheDeleteJobFollowsItsRepositoryByID`, `TestDeleteJobGuard`.

### Q-STOR-192 · A delete job always carries its bound and its subject, and an empty subtree queues none

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

`DeleteArgs.MaxID` zero meant "no bound", a reading kept for jobs queued before the field existed; a subtree that emptied between the
count and the check queued a job with that zero, which then removed whatever a deploy had put below the folder since, undecided. A
job without a subject ran without the guard.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Keep zero as no bound | As it was | No change | Removes items nobody decided, through a reading nothing needs before the first stable release |
| (b) No job for an empty listing; a job needs a bound and a subject; the erasure of a cache says so explicitly (chosen) | `enqueueDelete` answers `Done` when the check listed nothing; `RunDeleteJob` refuses, permanently, a job without `MaxID` or `Subject`; the queries take `bounded` beside `max_id`, and `EraseCache`, which no request asks for, sets the unexported `all` | Every item a job removes was listed by the check that queued it; no job runs unguarded | None that matters before the first stable release |

**Choice:** (b).

**Rationale:** the bound and the subject are what make the job a continuation of a decided request; a job without them is a removal
nobody decided. The development agent would revisit it only if a job ever has to remove more than a request listed.

**Applied in the spec:** R-STOR-018 (the job's bullet). Code: `internal/items/delete.go` (`enqueueDelete`, `RunDeleteJob`,
`batchGuard`, `DeleteArgs.all`), `internal/items/erase.go`, `internal/meta/postgres/queries/files.sql` (`ListSubtreeBatch`,
`ItemInRemovalScope`). Tests: `TestBatchGuardOfTheDeleteJob`.

### Q-STOR-193 · A stopped delete job records why, and a job's error never carries a database's text

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

A job its guard stopped left its reason in the job's row only, which the job retention prunes, and no audit trail named the subject
whose withdrawal stopped it. The job's error is read through `GET /api/v1/system/jobs/{id}` by the job's creator and by every
holder of `settings:read`: a 403 named the refused item, which may be one the caller cannot read, and a 503 carried its whole cause
chain, database text included, in `detail`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The job's error is enough | As it was | No new event | Pruned with the job; names a hidden item; leaks a database's text |
| (b) An audit event, the root path in the 403, a generic text for causes (chosen) | `artifact.delete_stopped` (asynchronous; the subject as actor, outcome `denied`, the reason code, the job, the root path, the items removed); the guard's 403 names `DeleteArgs.Path`; the worker shows the first `errs.Error` in a job's error chain as code and detail only, a wrapper's own words before it kept, and the delete job reports every error but a refusal and its own permanent ones with a generic text, its cause in the log | The audit trail keeps who stopped a delete and why; the job's error says no more than the request's answer would | One more event type |

**Choice:** (b). The shared worker change holds for every job kind: the first error written for a caller in the chain, wherever a
wrapper put it, keeps its code and detail in the job's error and its operation and cause go to the log only; the rest of the text
stays as its producer wrote it for the reader (R-OPS-019). A wrapper that rewords the text so that the error's own words are not in
it shows the code and detail alone. The worker logs the error also when the lease was lost and when the failure could not be
written.

**Rationale:** a stop is a security-relevant outcome of a request the caller made, so it belongs to the audit trail with the actor
of that request; the job's error is read by others than the caller and says what the request's own answer would say. The development
agent would revisit the generic text if operators need the cause in the job API rather than in the log.

**Applied in the spec:** R-STOR-018 (the job's bullet); [operations-and-observability.md](operations-and-observability.md) the
event table (`artifact.delete_stopped`) and R-OPS-019 (the `detail` of a job's error). Code: `internal/items/delete.go`
(`recordStopped`, `jobFailure`), `internal/files/delete.go` (`DeleteJobGuard`), `internal/jobs/worker.go` (`jobDetail`),
`internal/audit/events_files.go`. Tests: `TestR_STOR_018_TheDeleteJobDecidesEveryBatchOnTheStandingOfItsSubject` (the event),
`TestDeleteJobGuard`, `TestBatchGuardOfTheDeleteJob`, `TestJobDetail`.

### Q-STOR-194 · A credential whose row is gone stops the job with a reason of its own, and that is the bound of an expired credential

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

A token or a session that ended by time does not stop its job (Q-AUTHZ-261). Its row is purged later: a token
`auth.tokens.retention_after_end` after its expiry or revocation (default 2160 h, at least 1 h), a session 30 days after its end
(R-AUTH-043). The job then could no longer tell an expiry from a revocation, and stopped with "the token … was revoked", a reason
that was not true.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The purge as the bound, with its own reason (chosen) | A missing row stops the job: "no longer exists: its row was removed after it ended" (`invalid_token`, `session_expired`) | One rule (a credential the job can check), and the operator sets the bound with the retention | The bound follows a setting made for another purpose: 1 h to 90 days and more for a token, 30 days for a session |
| (b) A grace of its own after the end by time | A setting `storage.delete.credential_grace`; the job stops once the credential ended longer ago | A bound that says what it is for | One more setting; a job queued behind others stops for its wait, not for anyone's decision |
| (c) No bound | A missing row is ignored | A job always finishes | A job would act for a credential nothing records any more |

**Choice:** (a). The bound is stated in H-659 of [file-transfer.md](../security/file-transfer.md).

**Rationale:** the job acts only while it can check the credential that asked; once the row is gone it cannot, and it says so. A
delete job runs minutes, rarely hours, so the bound matters only for a job that waited behind a long queue. The development agent
would revisit (b) if jobs that wait longer than a token's retention become common.

**Applied in the spec:** R-STOR-018 (the job's bullet). Code: `internal/auth/standing.go` (`token`, `session`),
`internal/auth/session/standing.go` (`ErrPurged`). Tests: `TestJobStanding`, `TestR_STOR_018_TheDeleteJobReadsTheSessionThatAskedFromItsRow`.

### Q-STOR-210 · The publication port reuses the blob commit protocol, writes no event and no hook, and a client cannot touch what it wrote

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: `items.Publisher.Publish` (Q-STOR-182) is the one writer of generated items. Three questions remained: how the blobs reach the
store, what a publication records, and how R-STOR-018 keeps a client from deleting or overwriting the result.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `blob.CommitAll` for the blobs, the link function as the publication, no audit event or storage hook per file, a 409 in the item store for a client (chosen) | Objects are written first, outside any transaction; the link transaction holds the share lock of every blob row, upserts the items, removes the generated items it is told to, and runs the caller's function; a failure or `ErrSuperseded` queues the digests for collection | The commit protocol of every other write: no new object-store path, the same garbage-collection rule, equal blobs stored once; a reader sees the previous generation or the new one | A publication of 2,000 files is one long link transaction; the hook of the format never sees its own files |
| (b) One `Commit` per file | Per-file transactions | Existing code | A reader could see a mixture of two generations (Q-STOR-182 (b)) |
| (c) Audit event and hook per file | As an upload | Uniform | An index is rewritten every few seconds: the audit trail would hold thousands of events no one asked for, and the format's hook would take its own index for a client's upload |

**Choice:** (a). A path that holds a client's file takes the new content and becomes generated; a path in `remove` that holds a client's file is
left alone; an identical blob changes only `modified_at`, which the caller sets (the `Release` date), and `updated_at`; a zero `ModifiedAt` is
the transaction's time. `Publish` takes the repository it is given whatever its class: that a virtual repository accepts generated items from
this port and from nothing else is the virtual class's rule (T-17 WP8), whose seam is this port. A client's upload to a path that holds a
generated item and a client's delete of a subtree that holds one answer 409 `generated_item` (checked in the link step and in the delete's
request and removing transaction); a companion committed as `Generated` replaces it as before.

**Rationale:** nothing here needed a second way to store a blob. The development agent would revisit (c) if the audit trail should show index
publications.

**Applied in the spec:** R-STOR-018 (above), Q-STOR-182. Code: [publish.go](../../internal/items/publish.go), [commit.go](../../internal/items/commit.go),
[delete.go](../../internal/items/delete.go), [files.sql](../../internal/meta/postgres/queries/files.sql). Tests: `test/integration/itempublish`.

### Q-STOR-215 · The storage summary counts the generated items of a virtual repository and lists the repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Added with the virtual class of the Debian format (T-17 WP8).

Context: a Debian virtual repository holds the generated items of its merged indexes (Q-STOR-180). R-STOR-028 said that virtual repositories hold no items
and are not listed.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The summary counts the items of every repository and lists a virtual repository that holds any (chosen) | `logical` and `repositories[]` follow the items table whatever the class | The figures add up to the usage the counters keep (R-STOR-003); an operator sees what a virtual repository stores | A virtual repository appears in the list with the bytes of its indexes |
| (b) Leave virtual repositories out | The old rule | The list holds the repositories that hold packages | The indexes are logical bytes that no entry owns; the shares do not sum to 1 |
| (c) Add the indexes to the member that built them | An owner for the bytes | Sums | The bytes of a merged index belong to no member |

**Choice:** (a). The usage counters are kept by the items table whatever the class (R-STOR-003), so the figures of a virtual repository are those
of its generated items. Verified: the publication port accepts a virtual repository and no remote one (`TestQ_STOR_180_PublishTargetClasses`).
Not verified: the summary task itself (R-STOR-028) is not built yet, so this decision is for it.

**Rationale:** the usage of an installation is the sum of the usage of its repositories. The development agent would revisit it if the list became
long for installations with many virtual repositories.

**Applied in the spec:** R-STOR-028. Code: [publish_virtual.go](../../internal/items/publish_virtual.go).

### Q-STOR-200 · `quader admin storage verify` reads the live blob rows, reports their items, changes nothing, and `--since` selects rows by their creation or state change

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-HA-024 and R-HA-025 name `quader admin storage verify --since <t − storage.gc.grace_period>` and say it prints the number of referenced blobs whose objects are missing; R-HA-052's pod-crash row and exit criterion E5.2 need "no item without its object". The spec did not say what the command reads, what it writes, which credential it holds, how it stays within memory on a large table or what `--since` selects. Verified in this repository: `blobs` has no column for the time a row's object was last needed; its time columns are `created_at`, `state_changed_at`, `checked_at` and `deep_checked_at`; only `items`, `remote_cache_entries`, `ui_assets` and `user_avatars` reference `blobs`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The scrub's existence check, run on demand as a command (reuse the task) | `POST /api/v1/system/tasks/scrub/run` | Exists already | It writes `content_status` and audit events; its order and rate are the scrub's; it needs a running replica and an administrator token, and a restore drill runs before replicas serve |
| (b) A read-only command over the live `blobs` rows, paged by digest, that lists the items per damaged blob (chosen) | One `HeadObject` per row, pages of 1,000, 16 at a time; the findings are the items of `items` that reference the blob | Needs only the database and the store credential of any replica; changes nothing; the report answers "which item" | A second reading of existence next to the scrub's, which the decision accepts because the scrub's marks are state and this is a report |
| (c) Compare the table with a listing of the bucket | `ListObjectsV2` of the prefix against `blobs` | One request per 1,000 objects, not per blob | Reads every object key of the prefix, including orphans and the objects of other installations in a shared bucket; the listing is eventually consistent on some stores (R-STOR-035) |
| (d) Check only the blobs `items` references | A join in the page query | Fewer checks | A blob that is `live` and unreferenced is still a row whose object must exist (R-STOR-005 writes the object first); a second arm of SQL for no saving at the sizes in question |

**Choice:** (b). The package is [internal/blob/verify](../../internal/blob/verify/verify.go), the command [admin_storage.go](../../cmd/quader/admin_storage.go), the queries `VerifyBlobPage`, `VerifyBlobItems` and `VerifyBlobItemCount` in [blob.sql](../../internal/meta/postgres/queries/blob.sql). The run reads `state = 'live'` rows (a `deleting` row's object may be gone by design) with the web database role (it only reads) and the runtime credential. `--since <t>` keeps the rows with `GREATEST(created_at, state_changed_at) >= t`: the rows written or changed since the cut-off. A missing object and a size different from `blobs.size` are findings; a transient store error after three attempts is "not checked" and exits `75` when nothing else is wrong, so a store outage never reads as a clean run or as a loss.

**Rationale:** the question after a crash or a restore is "which item cannot be served", and an operator cannot ask the API for it while the replicas are held not ready (R-HA-025). The command is a report: it leaves `content_status` to the scrub so that running it in the middle of an incident changes nothing a replica reads. Not verified: that the rows a point-in-time restore brings back and a GC deleted after *t* are exactly the rows that `--since <t − grace_period>` selects. They are the rows that were `deleting` or unreferenced at the time of the delete, which `blobs` does not date; `--since` therefore narrows a run, and a restore procedure that wants every lost object runs the command without it. The development agent revisits this when `blobs` gains a column that dates the last reference, or when a table is large enough that a full run is too slow (then a per-prefix split of the digest range, `--concurrency` aside).

**Applied in the spec:** R-STOR-027 (the operator check and its acceptance criterion), R-HA-052's pod-crash row. Code and tests: [verify.go](../../internal/blob/verify/verify.go), [admin_storage.go](../../cmd/quader/admin_storage.go), `test/integration/storageverify`.
### Q-STOR-211 · R-STOR-046's drain criterion runs a real full collection run on the evicted worker and judges the retention half on the probe jobs until retention runs exist

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the criterion has one `worker` run `gc-full` and another a retention run during the drain test of R-HA-059; both finish on the
remaining workers. Two things stand in the way. A full run on an installation of a few thousand blobs ends in seconds, and a drain that
meets no run in flight tests nothing. Retention runs (R-STOR-040) are not built; they belong to the lifecycle work of the full scope.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Start `gc-full` and hope the drain meets it | A run on the existing data | No new profile | A full run with no candidates ends at once; the precondition (a run in flight on the evicted worker) is a matter of timing |
| (b) A profile without a grace period, fresh garbage, and a start of the run until the evicted worker claims it (chosen) | Profile `drain` (the scatter profile with `storage.gc.grace_period: 0s`); the test uploads and deletes six files, starts `gc-full`, and reads the job's replica; a run on another worker is cancelled and started again with fresh garbage. A run that deleted rows ends by waiting `delete_settle` (30 s, R-STOR-024), so the drain meets it in flight; the test then requires: `succeeded`, `attempt` 1, a different final replica, and a finish after the eviction of the worker that held it | The precondition is forced and proved, not hoped for; the product is unchanged | A profile with a grace period of 0 for these legs (the backup profile has it too); the number of rounds is probabilistic (up to 16; in the first run five of six rounds went to the same worker) |
| (c) A test hook in the collector that slows the run | A fault-injection build | Deterministic length | A build other than the one that ships; the criterion is about the shipped release at shutdown |
| (d) Wait for retention runs | Do not build the criterion | Honest | Leaves the collection half, which is real today, unjudged |

**Choice:** (b), with the retention half judged on the `default` queue's probe jobs: the drain test runs six `system.probe` jobs through
every drain, and each that moves must resume from its cursor with `attempt` 1 (R-HA-011); that is the claim "a job of the queue `default`
finishes on another worker", with the job kind that exists. **Rationale:** the criterion's point is that the release at shutdown hands a
long job over without an attempt and without a loss; the collection run is the storage job with the most state at that moment (claimed rows,
S3 calls in flight, R-STOR-024). The retention run joins the test when R-STOR-040 is built: `drain_storage_test.go` starts it beside
`gc-full` and applies the same four checks. The development agent would revisit (b) if a run shows the settle wait shorter than the time
from the start of the run to the eviction (the test then fails with the finish time and the eviction time, not silently).
**Applied:** `deploy/dev/values-drain.yaml`, `deploy/dev/up.sh` (profile `drain`), `test/e2e/drain_storage_test.go`, `docs/developer/e2e.md`.

### Q-STOR-216 · The settle wait of a collection run ends with a pass that started after its deadline

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: `TestGarbageCollection` ("the full sweep finds an unreferenced blob...") failed in Test and Release 42818570044 (`gc_test.go:170`): a blob's object was gone and its row stood in state `deleting` after the run that deleted it ended, 1.35 s into a subtest whose settle is 150 ms. The settle wait (step 5 of R-STOR-024) runs a pass over the settled rows, counts the rows still to settle, and left when the deadline (`delete_settle` plus two polls, 180 ms in the tests) had passed. Verified by a test that holds the run for longer than the deadline between the pass and the check: the run ended with its row in `deleting`. A pass is a few statements, so a runner that stalls for 200 ms between two of them is enough. Production meets it only as a delay: the row is dropped by the next run, and an upload that waits for that row (R-STOR-005) waits for it. Nothing is lost and no object is left behind. Not verified: that the failed run was stalled in that place (the log keeps no timing of the passes).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Widen the deadline | More slack than two polls | One number | The next stall is longer; every run that deletes waits longer |
| (b) Read the deadline before the pass; the wait ends only with a pass that started after it (chosen) | `expired` is read at the top of the loop, so a pass that began after the deadline is the last one | Exact: a row deleted by this run is removable `delete_settle` after its delete, which is before the wait began, so a pass after the deadline drops it; the wait is not longer when nothing stalls | One more pass in the case that needed it |
| (c) Leave it to the next run | Accept the delay | Nothing to change | An upload waiting for the row waits for the next scheduled run, which the wait exists to avoid |

**Choice:** (b), in `settle` ([gc.go](../../internal/blob/gc.go)); the fault point `settle:counted` after each pass lets a test hold the run there.

**Rationale:** The wait's promise is that the rows a run deleted are gone when it returns; the deadline bounds the time the run spends, not the passes it makes. The development agent would revisit the choice if a run is seen to end with a row in `deleting` that was not deleted by itself or a predecessor within `delete_settle`.

**Applied in the spec:** none (R-STOR-024 step 5 already says the run waits to drop the rows it deleted). Test: `TestR_STOR_024_TheSettleWaitEndsWithAPassAfterItsDeadline` ([gc_test.go](../../test/integration/blob/gc_test.go)).

### Q-STOR-217 · A collection run in its settle wait hands the wait over when its replica begins to stop

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: F-137 ([architecture.md](architecture.md) Q-ARCH-501): in the nightly 44233617621 the evicted worker's `gc-full` job had deleted
its objects before the eviction and spent the drain in its settle wait (R-STOR-024 step 5, `delete_settle` 30 s in the drain profile),
which ended 0.24 s before the release instant of the worker's plan; the job finished on the stopping worker instead of moving, and
R-STOR-046's drain criterion failed. The wait does no work: it lets `delete_settle` pass and then drops the rows.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Keep waiting until the release | As before | Nothing to change | The job moves or not by a race of fractions of a second against the plan; the stopping worker holds it for up to the whole server phase |
| (b) End the wait at the drain and release the job (chosen) | `RunOptions.Draining` closes the wait with `blob.ErrHandedOver`; the job handler turns it into a release (`jobs.ErrReleased`) | The run that continues on another worker drops the rows at the same moment the wait would have; the job always moves when the drain meets it waiting | The run's result is not stored for the stopping attempt (a released attempt stores none) |
| (c) End the wait at the drain and finish the job | Return success without waiting | The job ends at once | The rows wait for the next scheduled run, which is what the wait exists to avoid (Q-STOR-216) |

**Choice:** (b), in `settle` ([gc.go](../../internal/blob/gc.go)), `RunJob` ([gcjob.go](../../internal/blob/gcjob.go)) and the
handler in [wire_jobs.go](../../internal/app/wire_jobs.go). The pass that comes before each wait still runs, so the rows whose
`delete_settle` has passed are dropped before the hand-over. **Rationale:** the promise of the wait (an upload waiting for a row is not
left until the next scheduled run) is kept by the job, which continues at once on a worker that stays. The development agent would
revisit it if the run that continues the job is seen to wait longer than `delete_settle` for rows the handed-over run deleted.

**Applied in the spec:** R-STOR-024 (the hand-over rules). Test: "a run in its settle wait when the replica begins its shutdown hands
the wait over at once" in `TestGCJobHandler` ([gc_test.go](../../test/integration/blob/gc_test.go)).

### Q-STOR-220 · The connections are a list of flat elements and the shared client settings a block of their own

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That connections are
kept in the chart as a list is the owner's (2026-11-22, decision 9).

Context: `storage.s3` is one nested block ([internal/config/storage.go](../../internal/config/storage.go) `StorageS3`, with
`credentials`, `sse`, `http`, `retry`, `circuit_breaker`, `multipart`, `presign` inside). The configuration loader accepts a list of
elements only when each element is flat: scalars, pointers to scalars and lists of strings
([internal/config/schema.go](../../internal/config/schema.go) `checkElement`), and the whole list is one key whose environment value
is JSON (`isStructList`).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A list of flat elements for what belongs to a connection, a shared block `storage.object_store` for the client (chosen) | `credentials_source`, `sse_mode` … in the element; `http`, `retry`, `circuit_breaker`, `multipart` once | Fits the loader's rule unchanged; a provider's differences stay per connection; one place for the tuning that is the same | The credential and SSE keys are spelled flat, unlike `backup.target` |
| (b) A list of nested elements | Every block of `storage.s3` inside each element | Each connection tunable in full | The loader's rule changes for one key; eight copies of tuning nobody varies |
| (c) A map keyed by the id | `storage.connections.default.…` | Nested blocks, per-key environment variables | A map has no order, so read and write order need a key of their own; the id becomes a key name |

**Choice:** (a). **Rationale:** what differs between providers is the address, the credential, the encryption and a few protocol
switches; the client's timeouts and pools are the replica's. The planner would revisit this if two providers ever need different
timeouts, by adding the one key to the element.

**Applied in the spec:** R-STOR-030 to R-STOR-035, R-STOR-050, the configuration reference.

### Q-STOR-221 · The list order is the write order and the read order

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The list order for both (chosen) | The first writable connection takes an upload, reads try the copies in that order | One rule an operator reads off the list; no setting | A connection cannot be first for reads and last for writes |
| (b) `read_priority` and `write_priority` per connection | Two numbers | Reads near, writes elsewhere | Two orders to keep consistent; the simple path gets two keys |
| (c) Reads by measured latency | The fastest answering connection first | Adapts by itself | Behaviour that changes under load and is hard to reason about in an incident |

**Choice:** (a). **Rationale:** the store nearest to the replicas should take both; a case for two orders has not appeared. Reordering
moves no data, so a wrong order costs a rollout.

**Applied in the spec:** R-STOR-050, R-STOR-053, R-STOR-055.

### Q-STOR-222 · What may differ between providers is a key of the connection, the rest is shared

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: providers differ in checksum defaults, conditional writes and `DeleteObjects` (the research of 2026-11-22: the AWS SDK's
default checksums are the likeliest break with S3-compatible stores; the software behind the owner's provider refuses the checksum
trailer of newer SDK defaults). `storage.gc.batch_delete` was installation-wide.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `payload_checksum`, both checksum modes, `batch_delete`, `storage_class`, the scrub's period and throttle per connection (chosen) | Each provider's quirks where the provider is | A second provider never forces its quirks on the first | `storage.gc.batch_delete` moves |
| (b) All shared | One value each | Fewer keys | Adding a provider that refuses SHA-256 checksums lowers the integrity of the first |

**Choice:** (a). **Rationale:** these are the keys whose right value depends on the store, as the research found. The scrub's period is a
connection's because its cost is the provider's egress.

**Applied in the spec:** R-STOR-024, R-STOR-050.

### Q-STOR-223 · A provisioner's Secret reaches a connection through files named by the element

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: today the chart maps a provisioner's Secret keys `bucketKey`, `regionKey` and `endpointKey` to the environment variables
`QUADER_STORAGE_S3_BUCKET` and so on ([deploy/helm/quader/templates/_endpoints.tpl](../../deploy/helm/quader/templates/_endpoints.tpl)).
A list element has no variable of its own (the list is one JSON variable), and the owner's tool stack provisions buckets through such
Secrets.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `bucket_file`, `region_file`, `endpoint_file` in the element, the chart mounts the Secret's keys (chosen) | A file's trimmed content is the value, read at start | Every connection can take its own provisioner's Secret; the same file mounts as for credentials | Three keys more in the element |
| (b) The chart renders the whole list as JSON from the Secret | `secretKeyRef` cannot compose JSON | — | Not possible in a pod spec |
| (c) Helm `lookup` of the Secret at render | The chart reads the Secret | No new key | Fails under GitOps renderers, which have no cluster access |

**Choice:** (a). **Rationale:** the only mechanism that works per element under every renderer. The values are no secrets; they are read
once at start like the plain keys.

**Applied in the spec:** R-STOR-050, [ha-and-helm.md](ha-and-helm.md) R-HA-072.

### Q-STOR-224 · Bucket, prefix and encrypted are fixed, endpoint and credentials may change, and a marker names the location

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That a connection's
properties are fixed once it holds data is the owner's (2026-11-22, decision 7). That a new database on an old bucket meets the marker is an
open question for the owner (his installation is a playground he resets), specified meanwhile with (a).

Context: the owner fixed "the properties"; which ones decide where and how data lies is the planner's reading. An endpoint changes for
legitimate reasons (a renamed host, an internal and an external address of one store), and then the data is the same.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Fixed: bucket, prefix, `encrypted` (and stage 2's naming); free: endpoint, region, credentials, CA, tuning; a marker object names installation and connection at the location (chosen) | A replica refuses a changed fixed property while the connection holds a copy; the marker refuses a location that belongs to another connection | A moved host is a rollout; a misconfiguration that points two ids or two installations at one location is refused, however the addresses are spelled | One object per connection Quader writes; a database reset with an old bucket meets the marker |
| (b) Every key fixed | Any change is a new connection | Simplest rule | A renamed host costs a full copy of the data |
| (c) Fixed properties without a marker | Compare only what the configuration says | Nothing written to the bucket | Two spellings of one location are two connections; a drain of one deletes the other's objects |

**Choice:** (a). **Rationale:** the marker closes the one case in which the copies' bookkeeping would delete live data (two connections
on one location, a drain of one); the price is an object per connection. The planner would revisit it if a store refuses a write outside
`sha256/`.

**Applied in the spec:** R-STOR-001, R-STOR-051, R-STOR-059.

### Q-STOR-225 · A connection removed while it holds copies is detached, not refused

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. An open question for
the owner, specified meanwhile with (a).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Detached: replicas start, its copies are not read, blobs only it holds answer `503`, an alert fires, putting the element back restores everything; `forget-connection` gives it up (chosen) | Availability first, nothing destroyed | A provider that is gone does not block a rollout; a mistake is undone by the next rollout | A blob only that connection held is unavailable until the element is back or the connection is forgotten |
| (b) Refused: replicas do not start while a non-drained connection is missing | The rollout stalls | Nobody loses access by accident | A provider that went away for good blocks every rollout until an operator runs a command |
| (c) Forgotten at once | Rows deleted when the element goes | Nothing lingers | A typo in a values file marks blobs missing |

**Choice:** (a). **Rationale:** the configuration is the operator's statement of what exists; Quader keeps what it knows and makes the
consequence visible. Recommendation to the owner: (a).

**Applied in the spec:** R-STOR-051.

### Q-STOR-226 · A row per copy without a foreign key to blobs, and the blob's content state derived from its copies

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: `blobs` holds no location and one `content_status` per blob ([00002_storage_core.sql](../../internal/meta/postgres/migrations/00002_storage_core.sql));
a "no such object" marks the blob `missing` installation-wide ([internal/blob/download.go](../../internal/blob/download.go) `markMissing`).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `blob_copies` with no foreign key to `blobs`; `content_status` kept and derived in the transaction that changes a copy (chosen) | Readers refuse from one row as today | Collection's catalog rule untouched; the alerts and indexes on `content_status` stay | Two places to keep in step, in one transaction |
| (b) A foreign key from `blob_copies` to `blobs` | Referential integrity | The database guards orphans of rows | R-STOR-024 counts every foreign key to `blobs` as a reference: no blob would ever be collected |
| (c) Drop `content_status`, compute it from the copies on every read | No derived column | One truth | Every refusal of a damaged blob costs a join; the alert on the counter needs a new source |

**Choice:** (a). **Rationale:** the derived column is maintained by the only statements that change its inputs, so it cannot drift; the
foreign key is impossible by the reference rule.

**Applied in the spec:** R-STOR-003, R-STOR-052.

### Q-STOR-227 · Four copy states, and verified means the whole stored object was shown to be the blob

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `present`, `missing`, `corrupt`, `deleting`; `verified_at` set by an enforced payload checksum on write or a full read that matched (chosen) | A gap is the absence of a row | A drain and the scrub know which copies were shown whole; a server-side copy's weaker evidence is visible | Whether a store enforces the checksum is established, not assumed (R-STOR-035) |
| (b) Add `pending` rows before a write | Every gap is a row | Gaps visible in one table | A row per connection per blob before anything happened; the queue already says it |
| (c) No verified mark | `present` is enough | Simpler | A drain could delete the copy that was read, leaving one never read |

**Choice:** (a). **Rationale:** what deletes data must know the survivor was read; the mark is cheap.

**Applied in the spec:** R-STOR-052, R-STOR-055, R-STOR-056.

### Q-STOR-228 · A queue for what happens and a pass for what was missed

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `blob_copy_queue` written in the commit and by each mark, plus a pass over the live blobs that queues what no row stands for (chosen) | Event and sweep | A new blob reaches its second copy within the minute; a rollout, a lost row or a new connection is caught by the pass | Two producers of one queue |
| (b) Only a pass | Walk the blobs | One mechanism | A new blob waits for the next pass; the pass runs continuously |
| (c) Only the queue | Event-driven | Cheapest | A blob committed by a replica that did not know a connection is never copied |

**Choice:** (a). **Rationale:** a rolling update runs old and new configurations side by side, so an event-only design loses gaps by
construction; a pass alone makes the window of one copy as long as a pass. The pass waits at `queue_limit` rows so the queue stays bounded
during a fill.

**Applied in the spec:** R-STOR-052, R-STOR-056.

### Q-STOR-229 · An upload goes to the first writable connection and falls over to the next within the request

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That an upload is done
when one connection holds it is the owner's (decision 8).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) First writable in list order; a transient failure or an open circuit retries on the next within the request (chosen) | The spool still holds the bytes | A provider's outage costs no client a retry | A request may take two writes' time |
| (b) First writable only, `503` on its failure | The client retries | Simpler | An outage of the first store costs every upload until its circuit opens |
| (c) Round robin | Spread writes | Even load | The nearest store loses its role; reads miss the order |

**Choice:** (a). **Applied in the spec:** R-STOR-053.

### Q-STOR-230 · `min_copies` is a file setting, and a request that reaches fewer connections is refused

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. The minimum as a
detail setting is the owner's (decision 8).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `storage.replication.min_copies` in the file configuration, default 1; fewer successful writes answer `503` (chosen) | Infrastructure beside the connections | One place with the list it counts; GitOps | A change is a rollout |
| (b) A runtime setting in the admin area | Changeable at once | Quick | The admin area does not change connections (decision 9), and this number is bounded by their count |
| (c) Below the minimum, accept and alert | Never refuse | Uploads always work | The setting would promise what it does not keep |

**Choice:** (a). **Applied in the spec:** R-STOR-050, R-STOR-053.

### Q-STOR-231 · A session stays on its connection and answers 503 while it is unreachable

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: a session's multipart upload exists in one store only ([00054_upload_sessions.sql](../../internal/meta/postgres/migrations/00054_upload_sessions.sql)
`s3_upload_id`), and its finalize is a server-side copy inside that store ([internal/blob/staged.go](../../internal/blob/staged.go)).
The owner's paper said: if its connection fails, the client starts again.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Pinned; `503` with `Retry-After` while the connection is unreachable; a client that gives up starts a new upload elsewhere (chosen) | No new timer | A short blip costs a retry, not a restart; a long outage ends in the restart the owner named | A client retrying against a long outage spends its retries |
| (b) Abort at once (`404 BLOB_UPLOAD_UNKNOWN`) | The client restarts | Fastest to a new connection | A breaker that opens for 10 s costs a multi-gigabyte restart |
| (c) Move the session by reading its parts back | Continue elsewhere | No restart | The parts are on the store that is down |

**Choice:** (a). **Applied in the spec:** R-STOR-054.

### Q-STOR-232 · A read falls over only before its first byte

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Before the first byte only; after it, abort as today and let the client's retry or resume take the next copy (chosen) | One copy per response | Every byte of a response comes from one object; a digest mismatch blames one copy | A cut response costs the client a resume |
| (b) Continue a cut body from the next copy with a ranged read | Seamless | No resume | A mismatch at the end cannot be blamed on one copy; stage 2's segments complicate the join |

**Choice:** (a). **Rationale:** resumption on any replica exists (R-STOR-012), and evidence must name one copy.

**Applied in the spec:** R-STOR-055.

### Q-STOR-233 · A verified download stamps its copy at most once a day

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Stamp `verified_at` when it is older than 24 hours (chosen) | At most one write per copy and day | Hot blobs are scrubbed by their downloads; the scrub reads less | A write on the download path once a day per copy |
| (b) Never | Only the scrub verifies | No write on the path | Every hot blob is read again by the scrub |
| (c) Every download | Always current | — | A write per download |

**Choice:** (a). **Applied in the spec:** R-STOR-055.

### Q-STOR-234 · A server-side copy counts as present and is the scrub's first work, a drain needs a verified one

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `present` after a size check, not verified; the scrub reads unverified copies first; a drain deletes only beside a copy verified within its period (chosen) | Cheap copies, safe deletions | No byte through the worker; nothing is deleted on the word of an unread copy | Until the scrub reached it, a server-side copy's bytes are taken on the store's word |
| (b) Read every server-side copy back before it counts | Verified at once | Strongest | A full read of every copy, which is what the server-side copy avoids |
| (c) Stream every copy | Always verified | One path | Bytes through the worker inside one endpoint |

**Choice:** (a). **Rationale:** a copy counts for availability at once and for deletion only when read; the gap until the scrub is
[H-1252](../security/object-store-connections.md#h-1252--a-server-side-copy-or-an-adopted-object-counts-as-present-before-it-is-read).

**Applied in the spec:** R-STOR-052, R-STOR-056, R-STOR-027.

### Q-STOR-235 · The reconciliation is one exclusive job per installation

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One `exclusive` job with `concurrency` copies at a time (chosen) | As the other storage tasks | One owner of the pass and the drain; drains hand it over by checkpoint | One worker's bandwidth bounds streamed copies |
| (b) One job per connection | Parallel per target | More throughput across providers | Two jobs may pick the same source; drains coordinate across jobs |
| (c) Every worker takes queue rows with `SKIP LOCKED` | Scale with workers | Throughput | The pass and the drain still need one owner; two mechanisms |

**Choice:** (a). **Rationale:** streamed copies are a catch-up, not the serving path; at 100 MiB/s a 17.5-hour backlog of 175 GiB
*(example)* is half an hour. The planner would revisit it when a measured catch-up is too slow.

**Applied in the spec:** R-STOR-056.

### Q-STOR-236 · A drain deletes a copy once every other connection holds the blob and one copy is verified

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That the old
connection goes is the owner's (decision 7); that Quader deletes its copies is an open question for the owner, specified meanwhile with (a).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The drain deletes each copy on the draining connection beside a present copy on every other connection, one verified within its period and confirmed by `HeadObject` just before (chosen) | Quader removes what it put there; journal and marker stay | Switching encryption on (stage 2) leaves no plaintext behind; the same bucket under another prefix is not paid twice | Objects are deleted from a bucket; an operator who wanted a frozen copy must not drain |
| (b) The drain only stops using the connection; the objects stay for the operator | Nothing deleted | Reversible by hand | Encryption switched on keeps every plaintext copy until someone deletes it; storage paid twice |
| (c) Delete without checking the survivors | Fast | — | The last good copy can go |

**Choice:** (a). **Rationale:** a drain moves data; leaving it behind defeats the cases the owner named. The residual is
[H-1256](../security/object-store-connections.md#h-1256--a-drain-trusts-a-copy-that-was-verified-within-its-period). Recommendation to
the owner: (a).

**Applied in the spec:** R-STOR-051, R-STOR-056.

### Q-STOR-237 · Collection releases a copy whose delete was never sent to the orphan sweep of its connection

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: an open circuit ends a collection run today ([internal/blob/gc.go](../../internal/blob/gc.go) `stopsRun`), and an upload of
the same content waits for the `deleting` row (R-STOR-005).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A delete never sent (open circuit, connection not configured here) drops its copy row; the object is that connection's orphan; an unknown outcome keeps the row (chosen) | The blob row goes after `delete_settle` | An upload of the content is not held up by another provider's outage; no late delete is possible for a request never sent | Deleted content stays on that connection until its sweep (H-1254) |
| (b) Keep the row until every connection confirms | Exact | Nothing left behind | An uploader of that content gets `503` for the length of an outage (17.5 hours at the owner's provider) |
| (c) Skip the blob until all connections answer | Nothing changes | Simple | The same wait for uploaders, and the blob stays claimed |

**Choice:** (a). **Applied in the spec:** R-STOR-057.

### Q-STOR-238 · The orphan sweep adopts the object of a live blob and claims an orphan through the blob row

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Adopt an object of a live blob without a copy row (unverified); claim an orphan with a `deleting` blob row whose other digests are zero bytes (chosen) | One protocol for every object found | A crashed reconciliation, a commit's lost round and a point-in-time restore lose no copy; the commit protocol sees the claim | A `deleting` row with placeholder digests |
| (b) Delete every object without a copy row | Simple | — | Deletes good copies and copies them again |
| (c) Claim orphans in a table of their own | Clean rows | No placeholders | The commit protocol does not see the claim and races the delete |

**Choice:** (a). **Applied in the spec:** R-STOR-026.

### Q-STOR-239 · The scrub reads every copy whole once per period per connection and has no separate existence check

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. The scrub as a full
read in a period is the owner's (decision 10, option a over c).

Context: R-STOR-027 had a weekly existence check and a deep check every 90 days, neither built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One period per connection (default 30 days), one throttle per connection, full reads only, hourly runs (chosen) | What the owner chose | One clock; a missing object is found by a read within the period and by every download | A deleted object nobody downloads is found up to 30 days later |
| (b) Keep a weekly `HeadObject` beside the full reads | Two clocks | A deletion found within a week | Two settings, which the owner declined in option (c) |
| (c) One installation-wide period | Simpler | One key | An expensive provider cannot be read less often |

**Choice:** (a). **Applied in the spec:** R-STOR-027, R-STOR-050.

### Q-STOR-240 · The scrub's evidence becomes true by moving the tests that name it for another part

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: R-STOR-027 is in [implemented.txt](../coverage/implemented.txt) while the scrub is not built
([docs/developer/blob-store.md](../developer/blob-store.md) says so); the tests that name it test the lost content of a remote
repository and the operator check.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Rename those tests to R-REPO-025 and to R-STOR-058 (the operator check moved there), remove R-STOR-027 from the list in the same change (chosen) | Each test names what it tests | The record tells the truth; the scrub's own tests bring it back | A rename across five files |
| (b) Split R-STOR-027 into parts | Several IDs | Keeps the names | The lost-content part belongs to R-REPO-025 already |
| (c) Leave it | — | Nothing to do | The record claims a scrub that does not exist |

**Choice:** (a). **Applied in the spec:** R-STOR-027, R-STOR-058.

### Q-STOR-241 · The journal goes to every connection, and an outbox row waits for each

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Ship to every connection with `drain: false`; delete the outbox row when each holds it; the journal pass copies older entries to a new connection (chosen) | The journal survives the loss of any one provider | A restore after a provider's loss still replays | A table of shipped marks; an unreachable connection keeps rows |
| (b) Ship to the first connection only | As today | Simplest | The provider that is lost may be the one with the journal |
| (c) Ship to any one, let the reconciliation copy | One write | Fast | Entries depend on a job that may be behind when they matter |

**Choice:** (a). **Applied in the spec:** R-STOR-059, [ha-and-helm.md](ha-and-helm.md) R-HA-054.

### Q-STOR-242 · A replica is first ready when one connection passed and no connection has a configuration error

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: Q-STOR-1 holds a replica back until the start check passed, so a broken configuration never replaces working pods. With two
connections, a pod that starts during one provider's outage (17.5 hours in July 2026 at the owner's provider) should serve from the
other.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Ready when one connection with `drain: false` passed and none has a configuration error; reported afterwards per connection; with `require_object_store` gating when none is reachable (chosen) | Availability from any connection | A drain during one provider's outage adds capacity; a wrong credential still stalls the rollout | A pod that cannot reach the second connection's network path becomes ready |
| (b) Every connection must pass | Strict | A broken path to any store stalls the rollout | One provider's outage blocks every new pod, the case the owner wants to survive |
| (c) Only the first connection must pass | Simple | — | The first may be the one that is down |

**Choice:** (a). **Applied in the spec:** R-STOR-060, [operations-and-observability.md](operations-and-observability.md) R-OPS-001.

### Q-STOR-243 · The object-store metrics keep their names and gain the label connection

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Same names, label `connection` (chosen) | `quader_storage_s3_circuit_open{connection}` | Dashboards keep working with a `sum`/`min`; the label is bounded (eight) | The rules change their aggregation |
| (b) New names per connection | — | — | Unbounded names |

**Choice:** (a). **Applied in the spec:** R-STOR-033, R-STOR-060, [operations-and-observability.md](operations-and-observability.md) R-OPS-007.

### Q-STOR-244 · The store interface carries object metadata, and every size check compares the copy's stored size

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That the wrapped data
key lies in the object's metadata is the owner's (decision 2, option e), for stage 2.

Context: the interface has no metadata anywhere ([internal/blob/objects.go](../../internal/blob/objects.go)); six checks compare a stored
object's size with the plain size of the row (the research of 2026-11-22: commit, download, recheck, verify, backup, restore).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Stage 1 adds metadata to `Put`, `CreateMultipart`, `Copy`, `Head`, `Get` and writes none; every check compares `stored_size`; the start check records what a connection does with metadata (chosen) | The seam once, now | Stage 2 changes values, not interfaces; its provider question is answered by a check | An unused parameter until stage 2 |
| (b) Leave it to stage 2 | Smaller now | — | Stage 2 reopens every caller and every size check |

**Choice:** (a). **Applied in the spec:** R-STOR-052, R-STOR-062.

### Q-STOR-245 · Stage 3 asks the store which copies can serve a blob now

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `Store.Servable(digest)` with the read order and each connection's `Encrypted()`, its link check and a guarded `Presign` (chosen) | One answer for the read and the link | A link never names a copy a read would skip | The guard's presign passes the breaker check now |
| (b) Stage 3 reads the rows itself | No new method | — | Two orders that can drift |

**Choice:** (a). **Applied in the spec:** R-STOR-062.

### Q-STOR-246 · Every behaviour once at the cheapest tier, no end-to-end leg

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Unit tests with two in-memory stores and the fault hook; integration with PostgreSQL and in-memory stores; one case against two real buckets; the race test run with two connections instead of one (chosen) | About 30 s added to CI, nothing per format | The rule of the repository; no format sees a connection | The S3 adapter's cross-bucket copy is shown against SeaweedFS and Silo only |
| (b) An end-to-end leg with two buckets | Real replicas | Closest to production | Minutes per run for behaviour the integration tier shows |

**Choice:** (a). **Applied in the spec:** R-STOR-063.

### Q-STOR-247 · At most eight connections

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Eight (chosen) | A bound | Bounds the label, the clients per replica and every blob's copies | An arbitrary number |
| (b) No bound | — | — | Metric cardinality and copies grow with a typo |

**Choice:** (a). **Applied in the spec:** R-STOR-050.

### Q-STOR-248 · The start check establishes what a connection supports and refuses or degrades by a stated rule

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the owner's provider documents neither conditional writes, nor checksum headers, nor a copy with new metadata; the software
behind it documents conditional writes as unsupported (refused with `501` or accepted and ignored) and the newer SDK's checksum trailer as
refused (a research of 2026-11-22). Nothing was run against that provider.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The start check probes the configured checksums (refusal is a configuration error naming the key and the value to try), whether a wrong checksum is refused (else writes are not verified), `DeleteObjects`, metadata and a copy with replaced metadata (recorded), and server-side copies between connections on one endpoint (recorded) (chosen) | Every replica, every start, a few requests per connection | A provider's gap is found before data depends on it; each gap has one stated consequence | Six requests more per connection at start |
| (b) The operator declares capabilities | Keys per capability | No probe | A wrong declaration is found by a failure in production |
| (c) The conformance command only | On demand | No cost at start | A replica serves on a store nobody checked |

**Choice:** (a). **Applied in the spec:** R-STOR-035, R-STOR-052, R-STOR-062.

### Q-STOR-249 · A1 is required for new objects, overwrites and deletes may be eventual, and no guarantee rests on a conditional write

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: A1 demanded strong consistency for overwrites and deletes. The owner's provider documents read-after-write for new objects and
eventual consistency for overwrites and deletes. Quader writes one content per key and deletes only through collection, a drain and the
sweep.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Require A1 for new objects; tolerate eventual overwrites and deletes with "latest wins" (A3); state A7 (chosen) | A stale read is the same content or a `404` that the recheck and the reconciliation heal | The owner's provider qualifies; the protocols are argued on what they need | A repaired copy may be read stale and repaired again |
| (b) Keep strong A1 | As before | Simplest argument | Refuses the owner's provider by documentation |
| (c) Use conditional writes where offered | Stronger write-once | Fewer stale cases | Two code paths, and stores that ignore the header give a false sense of safety |

**Choice:** (a). **Rationale:** the content address makes every write of a key the same bytes, so what eventual overwrite consistency can
show is the same content, and what eventual delete consistency can show is a copy that is still there; neither is wrong content.

**Applied in the spec:** R-STOR-035, R-STOR-053.

### Q-STOR-250 · The blobs that exist become copies of the connection default, which its first replica binds

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: the specification says nothing about the blobs an installation holds when it takes the migration of the connections (00106).
The owner's installation and the development stacks hold data. The migration knows that there was one connection, which the element
without an id names (`default`, R-STOR-050), but neither its bucket nor its prefix, and every reader takes an object's name from its copy row
(R-STOR-062).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The migration writes the row `default` with an empty bucket, which no configured connection can have, and a copy per blob whose name lacks the prefix; the first replica that configures `default` binds the row to its element and writes its prefix in front of the names (chosen) | One `UPDATE` at the first start | Every existing blob is readable at once, whatever prefix the installation used | A row of the migration that only one start completes |
| (b) The copies with the prefix `quader` assumed | No bind | Simple | An installation with another prefix reads nothing and marks every blob missing |
| (c) No copies; the orphan sweep adopts the objects (R-STOR-026) | No migration of data | Uses the general mechanism | Every blob is unreadable until the sweep of package B has run over the bucket |
| (d) No data kept: the installations are reset | Nothing to build | The rule of no backward compatibility | The owner's playground and the development stacks lose their content for no reason the code needs |

**Choice:** (a). **Rationale:** the copies say what was true before the migration (one connection held every blob), and only the prefix is
missing, which the first start knows. A blob in state `deleting` keeps its claim on its copy, and one whose object collection already
deleted gets none. An installation that names its one connection other than `default` at that start leaves the copies on a `default` that
no replica configures, which is detached (R-STOR-051) until the element is renamed back. The builder would drop it at the first stable
release, when migrations of data become a duty of their own.

**Applied in the spec:** R-STOR-051; code: migration 00106, `Store.RecordConnections` ([record.go](../../internal/blob/record.go)).

### Q-STOR-251 · The files of a connection's location are read when the clients are built, not when the configuration is loaded

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-050 rule 2 reads `bucket_file`, `region_file` and `endpoint_file` "once at start", and rule 4 refuses two connections at one
location. `quader admin config validate` and a render check of the chart load the configuration where the provisioner's Secret is not
mounted.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Loading validates what the configuration says (the exclusive keys, rule 4 for the elements whose location is written in it); the files are read, and rule 4 checked again on the read values, where a replica or a command builds its clients (`config.ResolveConnections`) (chosen) | Two steps | `config validate` runs anywhere; a missing file is an error naming the key at the start that needs it | The location of a file-based element is checked at start, not at validation |
| (b) The files are read by the loader | One step | Every error at load | Validation fails wherever the Secret is not mounted, also in the chart's checks |

**Choice:** (a). **Applied in the spec:** R-STOR-050; code: [storage_connections.go](../../internal/config/storage_connections.go).

### Q-STOR-252 · The first readiness waits for each connection's first answer

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-060 makes a replica ready once the start check passed on one connection without `drain: true` and no such connection has
a configuration error. A connection that failed transiently at that moment may show a configuration error later.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The start waits until one connection without drain passed and every connection without drain answered once (passed, failed transiently or failed with a configuration error); a configuration error stops the start; a later one is logged and shown, and the connection keeps failing through its circuit (chosen) | Each store is asked once before the replica serves | A wrong credential on a reachable store stops the rollout; an unreachable store does not hold it | A configuration error that appears after the readiness does not stop a running replica |
| (b) Ready at the first pass | Fastest | A slow second store does not delay the start | A wrong credential of the second store is found only after the replica serves |
| (c) Every connection must pass | Strictest | — | Contradicts R-STOR-060: one provider's outage would hold every new pod back |

**Choice:** (a). **Rationale:** the rule's two halves are both decided by each connection's first answer. The check `objectStore` is
then a reported check (gating with `health.require_object_store` only while no connection without drain answers), since the start check
already held the first readiness.

**Applied in the spec:** R-STOR-060; code: `blob.AwaitConnections` ([startcheck.go](../../internal/blob/startcheck.go)), `objectStoreCheck`
([wire_blob.go](../../internal/app/wire_blob.go)).

### Q-STOR-253 · A commit's write overwrites a missing or corrupt copy row

Status: **Decided by the builder** — selected from the options below; the owner reviews it. It changes a sentence of R-STOR-053, which
said `ON CONFLICT DO NOTHING` for the copy rows of a round that turns out a deduplication hit.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The copy row of an object this commit wrote becomes `present`: inserted, or a `missing`, `corrupt` or `present` row overwritten; a row in state `deleting` is left to collection (chosen) | The write is evidence about the object | A copy marked missing while this commit wrote the object again is not queued for a repair it does not need | — |
| (b) `ON CONFLICT DO NOTHING` | As R-STOR-053 said | Never touches a row it did not insert | A copy the commit just wrote stays `missing` until the reconciliation repairs it again |

**Choice:** (a). **Applied in the spec:** R-STOR-053; code: `PutBlobCopy` ([blob_copies.sql](../../internal/meta/postgres/queries/blob_copies.sql)).

### Q-STOR-254 · The names the start check and the reads give their findings

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-035 records a server-side copy from each other connection "per pair" without a name, R-STOR-055 counts
`quader_storage_reads_fallen_over_total{connection,reason}` without listing the reasons, and R-STOR-035 lets `quader admin storage
check` report results without an exit code.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `capabilities.serverSideCopyFrom` maps the id of each other connection on the same endpoint to the result, with `checkedAt` beside the four flags of R-STOR-061; the reasons are `not_found`, `unavailable` (a transient error or an open circuit) and `size`; `check` exits `5` when a check that is not optional failed and `2` for a usage or configuration error, and a missing capability is reported `absent`, not failed (chosen) | Names fixed in code | The API of package F and the alerts read fixed names; the check's exit codes are the operator commands' (R-BAK-013) | — |
| (b) A list of pairs; one reason per error code | — | — | Unbounded label values; two shapes for one fact |

**Choice:** (a). **Applied in the spec:** R-STOR-035, R-STOR-055, R-STOR-061; code: `blob.Capabilities`, `blob/metrics.go`,
[storage_connections.go](../../cmd/quader/storage_connections.go).

### Q-STOR-255 · A streamed copy holds one part in memory: one request up to a part, ranged reads into a multipart upload above it

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-056 pipes "a `GetObject` of the source (ranged parts above the multipart threshold)" into `PutObject` or the target's
multipart upload. The store's interface takes a seekable body for a single request, because a retry sends it again, and the payload
checksum of a request before its bytes (R-STOR-006, [objects.go](../../internal/blob/objects.go)). The multipart threshold is 100 MiB by
default; the worker's spool is 5 GiB in the chart, shared with the image scanning's database, while an upload may be 10 GiB.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One `PutObject` up to `storage.object_store.multipart.part_size` (16 MiB); above it a multipart upload whose every part is one ranged `GetObject` read into one buffer of a part, hashed in order; the SHA-256 is compared with the digest before `CompleteMultipartUpload`, and a mismatch aborts the upload (chosen) | At most `storage.reconcile.concurrency` × `part_size` of memory per run, 64 MiB by default | Bounded memory whatever the blob's size; nothing on disk; a wrong digest never completes an object; each part's payload checksum is known before it is sent | A blob between 16 MiB and 100 MiB is a multipart object on the target where a commit writes one request |
| (b) The commit's threshold, the whole body in memory below it | As a commit writes | The same shape of object as a commit's | Up to 100 MiB per copy and 400 MiB per run in memory |
| (c) Through the worker's spool and the commit's transfer | Reuses the transfer | Retries a part from disk | The spool's budget beside the scanner's database is about 3 GiB: a blob of 10 GiB cannot be copied |
| (d) An unseekable stream into one `PutObject` with the digest as its checksum | No buffer | No memory | The interface and the SDK need a seekable body for a retry |

**Choice:** (a). **Rationale:** the stream is a catch-up path, not the serving path; its memory must not grow with the largest blob, and
the target's payload checksum must cover every byte it accepts. The builder would revisit it if a provider bills requests heavily enough
that the multipart shape of blobs between one part and the threshold matters.

**Applied in the spec:** R-STOR-056 (the streamed copy); code: [copy.go](../../internal/blob/reconcile/copy.go) `copier.stream`.

### Q-STOR-256 · A copy is recorded under the lock of a mark, the source of a streamed copy is stamped verified, and the blob's content state follows

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-056 records a copy under `SELECT … FOR SHARE`, as the commit locks a blob. R-STOR-052 says that the statement that changes a
copy recomputes the blob's content state in the same transaction, which needs the lock the marks take (`FOR NO KEY UPDATE`, Q-STOR-226): a
copy whose source was marked between the read and the record would otherwise leave a blob `corrupt` beside a present copy. R-STOR-052 counts
"the source of a streamed copy" as verification, which R-STOR-056 does not record.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The record locks the blob as a mark does, finds it live, refuses a record later than the insert bound after the write, leaves a copy row claimed for deletion, writes the copy `present` (`PutBlobCopy`, Q-STOR-253), deletes the queue row, stamps the source of a streamed copy verified and sets the blob `ok` (chosen) | One transaction of six statements | The derivation holds after every record; the stream's read of its source is not lost evidence | The lock waits for a commit's `FOR SHARE` of the same blob for those statements |
| (b) `FOR SHARE` as written, no derivation | The commit's lock | Never waits for a commit | A blob marked meanwhile stays damaged beside a present copy until its next mark |
| (c) `FOR SHARE`, and the scrub derives again | Leave it to package D | — | Ties a statement's correctness to another job's period |

**Choice:** (a). **Applied in the spec:** R-STOR-056 (recording); code: [queue.go](../../internal/blob/reconcile/queue.go) `run.record`.

### Q-STOR-257 · The pass queues a gap only where another connection holds the blob, starts hourly or at once while a connection fills, and keeps its position in a row of its own

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-052 calls a connection without a row for a blob a gap; a row in state `missing` or `corrupt` is the same lack, and a blob
that no connection holds present has no source, so queuing it would fail at every back-off. R-STOR-056 does not say when a new full pass
starts. Its checkpoint "carries the pass's and the drain's positions", but a run ends after `run_duration` and the next scheduled run is a
job of its own, which starts without the checkpoint of the last; a pass over a large installation that waits at `queue_limit` lasts many
runs.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A gap is a live blob without a present copy on a connection with `drain: false` while another connection holds a present copy; a full pass starts when a target has never ended one, is filling with an empty queue (the last pass's gaps are copied), or the last ended an hour ago; the positions of the pass and of the drains are the row `storage_reconcile_state` of `system_state`, written after every batch (chosen) | A pass per hour, back to back while a connection fills | One connection costs one batch query per 1,000 blobs an hour; a fill ends with the pass that finds nothing; a gap a rollout left on an active connection is queued within the hour; a pass longer than a run goes on in the next, and a run released at a replica's drain loses nothing | A gap no queue row stands for waits up to an hour |
| (b) A full pass in every run | Continuous | A missed gap is found within minutes | One connection scans every blob every minute |
| (c) A pass only while a connection fills | Cheapest | — | The gaps of a rollout or a lost row on an active connection are never found |
| (d) The positions in the job's checkpoint only | As written | No row of its own | Every run starts the pass again: a pass longer than `run_duration` never ends |

**Choice:** (a). **Rationale:** the queue carries what happens (Q-STOR-228); the pass is the net, and an hour matches the threshold of
`QuaderStorageReplicationBacklog`. A blob without a present copy anywhere is the commit's repair and the recheck's (R-STOR-053, R-STOR-055);
once one of them gives it a copy, the next pass queues its other connections. The builder would revisit the interval if a measured gap
of an active connection mattered within the hour.

**Applied in the spec:** R-STOR-056 (the pass); code: [pass.go](../../internal/blob/reconcile/pass.go), [state.go](../../internal/blob/reconcile/state.go),
`ReconcilePassDue` ([reconcile.sql](../../internal/meta/postgres/queries/reconcile.sql)).

### Q-STOR-258 · Five reasons of a failure, a queue row without a source anywhere is deleted, and a target whose circuit opened during the run spends no attempt

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-056 counts `quader_storage_reconcile_failures_total{connection,reason}` without naming the reasons; it deletes a queue row
whose blob is not live, whose target is no target or holds a present copy, and is silent on a row whose blob no connection holds present
(a copy marked missing on the one connection there is, R-STOR-055) and on a target whose circuit opens while its copies of a batch run
("its queue rows wait without spending attempts").

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The reasons `no_source` (no other connection the worker reaches holds a present copy), `unavailable` (a transient error or an open circuit at the target or at every source, or the insert bound after every write), `source_damaged` (every source that answered was marked missing or corrupt), `refused` (the target's answer that retrying soon does not fix) and `claimed` (the target's row is claimed by an unfinished drain); a row whose blob has no present copy anywhere is deleted; a write that the target's breaker refused leaves its row as it is (chosen) | Five label values fixed in code | Bounded labels the alerts and dashboards can read; with one connection the queue stays empty, as R-STOR-056 says, because a mark's row has no source and goes; a probe that is out does not cost the other copies of the batch two minutes | — |
| (b) One reason per store error code | — | Detail | Unbounded label values |
| (c) Keep a row without a source with its back-off | — | Nothing deleted | With one connection every lost blob keeps a row, and `QuaderStorageReplicationBacklog` fires beside `QuaderBlobMissing` |

**Choice:** (a). **Applied in the spec:** R-STOR-056 (a failure, the metrics); code: [queue.go](../../internal/blob/reconcile/queue.go),
[metrics.go](../../internal/blob/reconcile/metrics.go).

### Q-STOR-259 · The drain walks every copy it has not claimed, reads the survivors again under the blob's lock, finishes its own claims, and a draining connection is no longer filled

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-056 walks D's present copies, confirms the verified survivor with `HeadObject`, locks the blob `FOR SHARE`, sets D's row
`deleting`, deletes under the hand-over deadline, then the row. It is silent on D's `missing` and `corrupt` copies (D holds no copy only once
they go too), on a mark of a survivor between the walk and the claim, on a claim whose run ended before its delete, on how often a walk starts
again while copies wait for the scrub, and on `filled_at` of a connection that drains or stops draining.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The walk takes every copy of D that is not `deleting`, by object name; the claim locks the blob as a mark does and checks again that every target holds a present copy and one is verified within its period; a claim on a live blob older than `handover_delay` is taken over by a compare-and-set of its time at the next run's start and finished; a walk that deleted or queued nothing is followed by the next after the pass interval; `drain: true` clears `filled_at`, a pass clears `drained_at` of its targets; the drained check runs at the end of each walk and at a run's start (chosen) | Each case has one rule | A draining connection with a damaged copy still drains; a survivor marked between the walk and the claim is seen; nothing a run left half done stays; a drain that waits for the scrub costs one walk an hour | A survivor verified by the scrub waits up to an hour for the deletion beside it |
| (b) Present copies only, `FOR SHARE` without a second look | As written | Fewer statements | A connection with one missing copy never drains; a survivor marked corrupt meanwhile can leave the last good copy deleted |
| (c) Leave an unfinished claim to collection | — | No code | Collection claims the copies of a blob it collects; a drain's claim of a live blob stays for ever |

**Choice:** (a). **Rationale:** a drain deletes data, so it reads the survivors once more where a mark cannot come between, and a drain that
cannot finish is worse than one that waits. A copy claimed for deletion whose blob is live is the drain's alone: collection claims copies
only of a blob it claimed (R-STOR-057).

**Applied in the spec:** R-STOR-051 (the states' columns), R-STOR-056 (draining); code: [drain.go](../../internal/blob/reconcile/drain.go).
### Q-STOR-260 · Collection releases a copy on an open circuit only while another connection is writable

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-057 releases a copy whose connection's circuit is open, so that an upload of the same content does not wait for another
provider's outage (Q-STOR-237). It names no exception for the case in which no connection is writable, which is every outage of the one
connection of a plain installation (the owner's has one, on an object gateway of his provider).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Release on an open circuit only while another connection of the replica is writable; with every circuit open the run stops its object-store phase and hands its claims over, as one store's open circuit always did (chosen) | The release serves the uploader it was made for | One connection behaves as before; an outage of every store leaves no orphans to sweep | While every store is down, the claimed blobs wait for their stores, as they did |
| (b) Release on every open circuit | The rule as written | One rule | With one connection, every outage turns the run's candidates into orphans that stay until the weekly sweep (H-1254 for one connection), although no uploader could store the content meanwhile |
| (c) Never release; keep the row until the connection answers | Exact | Nothing left behind | The wait for uploaders that Q-STOR-237 removed |

**Choice:** (a). **Rationale:** an upload needs a writable connection; while there is none, releasing gains no uploader anything and costs
deleted content a week in the bucket. A request the guard refused before it left is released as well while another connection is
writable: no late delete can come of it. The builder would revisit this if a store's circuit flaps so that releases and stops alternate.

**Applied in the spec:** R-STOR-057; code: `Collector.releasable`, `removeCopies` ([gc.go](../../internal/blob/gc.go)).

### Q-STOR-261 · Under the deletion pause the sweep walks on, adopting and marking, and a resume starts where it stopped deleting

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-026 says both that a sweep under the deletion pause "deletes nothing and ends with `result.paused: true`, and a later run
resumes at the checkpoint" and that "adoptions and marks are not deletions and continue"; its resume rule names only jobs that ended
`cancelled` or `failed`, and a paused run ends as a job that succeeded.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The run walks on adopting and marking, deletes and aborts nothing once it saw the pause, saves no checkpoint past the prefix before it, and `resume: true` takes the checkpoint of a run that ended paused as it takes that of a cancelled one (chosen) | Both sentences hold | A restore drill under the pause gets its copies counted again by the adoption; the deletions that were held back are made by the next run from where they stopped | A resumed run lists the paused stretch once more |
| (b) The run stops at the pause | Stop and resume | Simple | Adoptions and marks wait for the end of the pause, against the rule's last sentence |
| (c) Walk on and start the next run at the beginning | No resume after a pause | No change to the resume rule | The deletions held back wait for a whole walk |

**Choice:** (a). **Rationale:** the pause is about deletions; what restores the copies of a restored database is exactly what should go on.
The resume also matches the arguments (`connection`, `dryRun`), so that a dry run never sets where a real run starts. The builder would
revisit this when a pause is shown to last so long that the repeated listing matters.

**Applied in the spec:** R-STOR-026; code: `sweep.readPause`, `sweep.checkpoint` ([sweep.go](../../internal/blob/sweep.go)),
`SweepResumePoint` ([sweep.sql](../../internal/meta/postgres/queries/sweep.sql)).

### Q-STOR-262 · The sweep acts on evidence a listing alone does not give

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: one provider applies overwrites and deletes with a delay (R-STOR-035 A3, Q-STOR-249), so a listing can show an object whose
delete was acknowledged; the owner's object gateway documents queued deletes. R-STOR-026 adopts an object of a live blob on every
connection, marks a copy by its listed size, locks an adoption `FOR SHARE`, and reads no marker.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) No adoption on a connection with `drain: true`; a size it acts on (a mark, a corrupt adoption) confirmed by a `HeadObject`; an adoption under the mark's lock that re-derives the content state; the connection's marker read first and a foreign one ends the walk there (chosen) | Every act rests on a second piece of evidence | A drain's own delete, still listed, is not taken back as a copy; a stale listing marks nothing; an adopted copy heals a blob no connection held whole; another installation's objects are never adopted or deleted | One `HeadObject` per mismatch and one `GetObject` per connection and run |
| (b) As written | Fewer requests | Simplest | A drain that deleted a copy may find it adopted again and never reach `drained`; a blob marked missing stays missing after its object was adopted; a sweep deletes in a location whose marker another installation wrote |
| (c) A second listing of every prefix | Two looks at everything | Strongest against a stale listing | Doubles the cost of a walk for a case a `HeadObject` covers |

**Choice:** (a). **Rationale:** the deletions rest on the blob row and `min_age` already (R-STOR-026); what a listing alone decided was
the adoption and the mark, and those get their second look here. The adoption takes `FOR NO KEY UPDATE` instead of `FOR SHARE` because it
changes the blob's content state, as every statement that changes a copy does (R-STOR-052). An object that a store still lists after its
delete can be adopted on a connection that is not draining: [H-1265](../security/object-store-connections.md#h-1265--the-orphan-sweep-can-adopt-an-object-that-a-store-still-lists-after-its-delete).

**Applied in the spec:** R-STOR-026; code: `sweep.page`, `sweep.mark`, `sweep.adopt`, `sweep.foreignMarker` ([sweep.go](../../internal/blob/sweep.go)),
`WithInstallation`.

### Q-STOR-263 · The staging sweep moves from the full collection to the orphan sweep, per connection, without a cursor of its own

Status: **Decided by the builder** — selected from the options below; the owner reviews it. It supersedes
[Q-STOR-172](#q-stor-172--the-full-collection-sweep-removes-what-no-session-stands-for-until-the-orphan-sweep-is-a-task-of-its-own),
which said the pass moves into the sweep once it exists.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The orphan sweep's staging step, after each connection's 256 prefixes: multipart uploads below the whole prefix older than `multipart_max_age` aborted, objects of sessions over, unknown or pinned to another connection and leftover probes older than `session_max_age` deleted, counted in `stagingDeleted`; the job's checkpoint is the progress, the page limit and the `system_state` cursor go (chosen) | One pass per connection | Every connection's staging area has a remover; the multipart uploads of a commit's crashed blob write below `sha256/` are aborted too; one checkpoint | A drain during the step repeats the step |
| (b) Keep it in `gc-full`, per connection | Two walkers | No change of task | Two passes over the same prefixes, one with a cursor of its own |

**Choice:** (a). **Applied in the spec:** R-STOR-011 *Expiry*, R-STOR-026; code: `sweep.staging` ([sweep.go](../../internal/blob/sweep.go));
the session step keeps the rows ([gc_uploads.go](../../internal/blob/gc_uploads.go)).

### Q-STOR-264 · A session starts on the first writable connection without asking the store, and moves at its first write while it has written nothing

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-054 pins a session to "the first writable connection when the session starts" and moves one that has written nothing
"at its first write"; it does not say what a start does while no connection is writable. A start costs a row and nothing in the bucket
(Q-STOR-174).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The start records the first writable connection, or the first without `drain: true` when none is writable, and asks no store; the first write moves the session to the first writable connection while its own is not writable (chosen) | Starts never fail on the store | A start stays a row; a client that starts during an outage gets its `503` at the first chunk, where it retries | A session can start on a connection that is down |
| (b) The start answers `503` while no connection is writable | Fail early | The client learns at once | A start, which needs no store, fails for the store's sake |
| (c) The start probes the first connection | Fresh evidence | Finds a store that failed without opening its circuit | A request to the store per start |

**Choice:** (a). **Applied in the spec:** R-STOR-054; code: `Service.startConnection`, `Service.writable` ([service.go](../../internal/uploads/service.go)).
### Q-STOR-265 · The scrub replaces a copy it found damaged in the same run and reads back what it wrote before the copy counts

Status: **Decided by the development agent** — selected from the options below; the owner reviews it. It changes a sentence of R-STOR-027,
which gave the repair to the reconciliation, and adds `quader_storage_scrub_repairs_total`.

Context: the owner's decision 10 says the scrub "replaces a damaged copy from another connection". One store of the owner's research has no
upload checksum of its own and one applies overwrites and deletes with a delay (Q-STOR-249); no guarantee may rest on a conditional write.
The reconciliation (package C) is built beside the scrub and may be behind or not yet landed.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The scrub repairs a copy it marked in the same run from a present copy on another connection, streamed through the worker and checked against the digest on the way, reads the written object back whole, and records it `present` and verified only while the row is still the one found damaged (same state, same `state_changed_at`); a failure leaves the copy marked and queued for the reconciliation (chosen) | One repair path in the run that found the damage | A damaged copy is replaced within the hour that found it, also while the reconciliation is behind; the read-back judges the store's result by its bytes, which a store without an upload checksum and one with late overwrites need; the identity check needs no insert bound, because a row that collection dropped and a commit wrote again has another identity | Two writers can repair one copy (the scrub and the reconciliation): harmless, both write the blob's bytes under its content address and only one record matches the identity; the repair's bytes pass the worker, within the connection's throttle |
| (b) The scrub marks and queues; only the reconciliation repairs (R-STOR-027 before) | One writer | No second write path | A damage found by the scrub waits for another job; without that job nothing repairs; the reconciliation's stream has no read-back of what the store kept |
| (c) The scrub repairs and the reconciliation leaves marked copies alone | One writer per cause | Clear ownership | A copy marked by a download or the orphan sweep is repaired only when the scrub reaches it again, which it never does: a marked copy is not present, so never due |

**Choice:** (a). **Rationale:** what is damaged should be whole again as soon as Quader knows it, and a write that is not read back proves
nothing on the stores Quader must support. The read-back of a store with late overwrites may still see the damaged bytes; the copy then
stays marked, which is the safe direction. The development agent would revisit it when the reconciliation reads back its streamed copies
too, which would let the scrub hand the write to it.

**Applied in the spec:** R-STOR-027; code: [internal/blob/scrub/repair.go](../../internal/blob/scrub/repair.go), `ScrubRecordWhole`
([blob_scrub.sql](../../internal/meta/postgres/queries/blob_scrub.sql)); gaps
[H-1269](../security/object-store-connections.md#h-1269--a-copy-that-read-back-whole-may-still-be-served-stale-by-a-store-that-applies-overwrites-late).

**Added at the landing on the reconciliation (package C).** *One mark.* The scrub marks a damaged copy, a source found damaged and a copy
its recheck finds damaged with the store's `MarkCopyDamaged`, the mark of the download and of the reconciliation's source; it reports
whether the copy stands marked, and only then does the scrub repair. A "no such object" is looked for once more with `HeadObject` before it
stands, so a store that answered one request wrongly leaves no mark. *A failed repair.* The mark queues a row of `blob_copy_queue`; the
reconciliation takes it (a target row `missing` or `corrupt` is copied over, a row without a present copy anywhere is deleted, Q-STOR-258),
so a repair the scrub could not finish is retried there with its back-off. *A copy marked missing is read again.* A marked copy is no longer
present, so it is never due for the walk, and with one connection, or no whole copy elsewhere, nothing else would ever look at it; a store
that answered "no such object" wrongly for longer than the second look (a gateway rolled out or misrouted) would leave its blob `missing`
until a read's recheck (R-STOR-055) or the recheck of the API. So each run first reads the copies of its connection marked `missing` whose
store last answered for them more than `MissingLookInterval` (one day) ago: an object that reads whole again makes the copy `present` and
verified and the blob follows; one still gone or damaged has its `checked_at` moved and waits another day.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each run reads the copies marked missing whose look is a day old, records one that reads whole and moves the look of the others (chosen) | One request per missing copy and day | A wrong "no such object" heals within a day of the store answering right again, also for a blob nobody reads; a copy whose object is really gone costs one cheap request a day | A real loss is asked for every day |
| (b) Never again; leave it to reads, the recheck and the reconciliation | No requests | Cheapest | With one connection an unread blob stays `missing` after a store's mistake until an operator rechecks it |
| (c) Every run | Hourly | Heals within the hour | Twenty-four requests a day per lost copy, and a bucket that was emptied costs a request per copy every hour |

### Q-STOR-266 · A copy is overdue one day after its period, and every replica counts the overdue copies from PostgreSQL

Status: **Decided by the development agent** — selected from the options below; the owner reviews it.

Context: R-STOR-027 names `quader_storage_scrub_overdue_copies{connection}` and the alert `QuaderStorageScrubBehind` (`> 0` for 6 hours,
R-HA-045) without saying when a copy is overdue or who exports the gauge. In a steady state about a 720th of a connection's copies becomes due
every hour and is read by the next hourly run, so the copies merely past their period are never zero for long on a large connection.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Overdue: a present copy whose `verified_at`, or `created_at` when never verified, lies more than its connection's `scrub_period` and one day back; read from PostgreSQL in the background by every replica that has the connection, for its connections (chosen) | A grace of one day | Quiet in the steady state and through a skipped run or a rollout; a connection that cannot keep up crosses the line within a day and the alert fires six hours later; `max by (connection)` over replicas is exact | Behind by less than a day is not shown |
| (b) Past the period, no grace | Strict | Shows the first hour behind | 400,000 copies make about nine new due copies a minute: the gauge is above zero almost always and the alert fires on a healthy connection |
| (c) The run sets the gauge at its end on the worker that ran it | No query | Cheap | A worker that ran an earlier run keeps its old value; the alert's `max` reads a stale series |

**Choice:** (a). **Rationale:** the alert is about a connection whose period cannot be kept; one day is far below the 30-day period and far
above a run's hour. The count uses the index `blob_copies_scrub` (two range counts per connection). The development agent would revisit the
grace if an installation's period is set near its 24-hour minimum, where a day of grace doubles it.

**Applied in the spec:** R-STOR-027; code: `scrub.OverdueGauge`, `scrub.OverdueGrace` ([metrics.go](../../internal/blob/scrub/metrics.go)),
`ScrubOverdue`.

### Q-STOR-267 · The damaged-blobs list is read with settings:read, and the recheck writes no object

Status: **Decided by the development agent** — selected from the options below; the owner reviews it.

Context: R-STOR-027 names the permission `storage.read`, which [operations-and-observability.md](operations-and-observability.md) lists as a
proposed name; the capability catalog of [authorization.md](authorization.md) R-AUTHZ-010 has no such capability, and `system.read` there is
the capability `settings:read`. The recheck "sets the states by what it finds" without saying what a copy already marked becomes, and it runs
on an `api` replica inside a request.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The list with `settings:read`, the recheck with `settings:write` (as the task trigger); the recheck reads every copy unthrottled, stamps or marks a present copy, sets a marked copy that now reads whole present again, leaves a marked copy that still does not as it is, and writes no object (chosen) | Existing capabilities | The holders of the system status tab (R-UI-196) read the list; no new capability, scope item or role binding; the recheck is bounded by one read per copy and never waits for a write | A reader of the storage list also reads every other system page |
| (b) A new capability `storage:read` | Narrower | Least privilege | A catalog entry, scope items, role bindings and tests for one list, before any role would hold it alone |
| (c) The recheck repairs too | One click heals | Faster | A request that writes up to a blob's size to a store, under the request's deadline |

**Choice:** (a). **Applied in the spec:** R-STOR-027, [api-and-ui.md](api-and-ui.md) R-API-040; code:
[storage_damaged_api.go](../../internal/api/v1/storage_damaged_api.go), [recheck.go](../../internal/blob/scrub/recheck.go).

### Q-STOR-268 · restore-versions takes each connection's element of the journal reader's credentials file

Status: **Decided by the development agent** — selected from the options below; the owner reviews it. It changes a sentence of
[backup-and-restore.md](backup-and-restore.md) R-BAK-030, which spoke of "that connection's entry of `--credentials-file`".

Context: `restore-versions` lists and deletes object versions on every connection of its run, with a credential the runtime one lacks
(R-HA-023). Package D built the command per connection on a base where the file of `--credentials-file` held one credential
(`replay.LoadCredentials`, the file of `post-restore`) and chose (a), to be revisited once package E's per-connection file existed. Package
E landed in the same change and made that file a list of elements keyed by the connection
([backup-and-restore.md](backup-and-restore.md) [Q-BAK-73](backup-and-restore.md#q-bak-73--the-credentials-file-of-a-journal-reader-is-a-list-of-elements-keyed-by-the-connection)),
which the chart renders for the post-restore Job with one `journalReader` per connection.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One credential of `--credentials-file` on every connection of the run; connections whose versioning logins differ get a run each with `--connection` | One credential per run | No second file format before package E's | The file holds no single credential once Q-BAK-73 is built; an installation with two providers runs the command twice |
| (b) Each connection takes its element of the journal reader's file, and a connection the file names none for keeps its own credential, as for a journal reader (chosen) | One run, one file, one rule for the file | The file the chart already renders for the post-restore Job, so the one-off Job from its pod template works on every connection; one format and one check (`replay.LoadCredentials`); a connection without missing copies needs no versioning login | A connection with missing copies that the file leaves out ends with its store's refusal of the version listing (exit 2), not before the run |
| (c) As (b), but a connection the file names none for refuses the run before anything is read | Strict | A missing login is named first | A draining connection without a `journalReader` (Q-HA-399 requires one only without drain) refuses every run over all connections, even with nothing missing there |

**Choice:** (b). **Rationale:** the step is a rare, manual one after a point-in-time restore; it should take the file the operator already
has, with the meaning that file has everywhere, and it needs a versioning login only where a copy is missing.

**Applied in the spec:** R-STOR-058, [backup-and-restore.md](backup-and-restore.md) R-BAK-030; code:
[admin_restoreversions.go](../../cmd/quader/admin_restoreversions.go) `restoreVersionsCredentials`, `connectionClients` in
[admin_storage.go](../../cmd/quader/admin_storage.go).

### Q-STOR-269 · The operator check calls a blob lost when no copy it could ask is there and no present copy was left unasked

Status: **Decided by the development agent** — selected from the options below; the owner reviews it.

Context: R-STOR-058 calls a blob lost "when none of its copies is there with its size", but a run asks only the present copies on the
connections of its configuration (or the one named), and a check may go unanswered. Its exit codes say nothing of a store that refuses one
connection's checks.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Lost: no asked copy answered with its object of its stored size, and no present copy was left unasked or unanswered; a blob without a present copy is lost without a question; a store that answers `AccessDenied` ends that connection's checks, its copies count as not checked, and the run exits `2` unless a blob is lost (chosen) | Lost only on evidence | A blob that may still be whole on a connection the run could not ask is never called lost; a blob the database already holds damaged on every copy is called lost, as an operator after a restore needs; a refused connection is a configuration error, not a temporary one | A run with `--connection` cannot call a blob with copies elsewhere lost: the named connection's findings carry its exit `5` instead |
| (b) Lost when every asked copy is missing | Simpler | — | A blob whose only other copy lies on a connection that refused or did not answer would be called lost |
| (c) A refused connection ends the whole run | As before for one store | Simple | The other connections' findings go unreported |

**Choice:** (a). **Applied in the spec:** R-STOR-058; code: [internal/blob/verify/verify.go](../../internal/blob/verify/verify.go),
`verifyExit` ([admin_storage.go](../../cmd/quader/admin_storage.go)).
### Q-STOR-270 · A shipper run leaves out a connection that failed and walks on past the rows that wait for it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-STOR-059 says that an unreachable connection delays only its own copy. The shipper of one connection stopped at its first
failure and read the outbox from its start at every run; with a row that waits for one connection, a run that reads from the start reads
that row again and never reaches the rows behind it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A connection whose write fails is left out for the rest of the run; the run reads the outbox once in id order after a position, so it passes the rows that wait; it ends when every connection failed or the outbox is read, and its error names each failed connection (chosen) | `Claim(after, limit)`, a set of failed connections per run | A connection that is down costs one failed request per run, not one per row; the others receive every row in the same run; the job's error says which connection lags | A run that meets a connection that is down returns an error, so the job records a failure every ship interval for as long as the outage lasts |
| (b) Retry each row on each connection | No state per run | Simple | A connection that is down costs a timeout per row and the run never ends within its bound |
| (c) Stop the run at the first failure, as with one connection | As before | Simple | The connections that answer wait for the one that does not, which R-STOR-059 rules out |

**Choice:** (a). **Rationale:** the job's failure is the truth (an entry is not where it must be), the backlog gauge per connection and
`QuaderRevocationJournalBacklog` say which; the shipping to the others never waits. The development agent would revisit this if the
recorded failures of a long outage drowned other job failures.

**Applied in the spec:** R-STOR-059. Code: [ship.go](../../internal/journal/ship/ship.go), `ClaimRevocationOutbox` in
[backup.sql](../../internal/meta/postgres/queries/backup.sql).

### Q-STOR-271 · A replay goes on without a connection that cannot be listed, and fails when none can

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-STOR-059 says the replay reads every "reachable" connection of the source, and R-HA-054 that a journal that cannot be listed is
a journal not known to be complete. With several connections both hold at once: every entry lies on every connection without drain.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A connection whose listing fails is reported as an unreadable line with its reason and left out; the others are read and merged; no connection listed fails the replay and check 8 (chosen) | Reachable means listed | A restore after the loss of one provider replays from the others, which is why the journal goes to each (Q-STOR-241); the output says which connection was missing | An entry that only the missing connection holds (shipped while the others were down) is not replayed, and the output's line is the only sign |
| (b) Any connection that cannot be listed fails the replay | Every connection or nothing | The journal is known complete | One provider's loss makes every restore refuse the journal, the disaster the several connections exist for |

**Choice:** (a). **Rationale:** the outbox keeps a row until every connection holds its object, so an entry is on one connection alone only
while the others were down; a restore in that window reads the line that names the missing connection. The development agent would
revisit this if the replay's output proved too easy to overlook.

**Applied in the spec:** R-STOR-059, [backup-and-restore.md](backup-and-restore.md) R-BAK-030. Code:
[replay/reader.go](../../internal/journal/replay/reader.go).
### Q-STOR-275 · A download takes its blob's copies from its item lookup

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-055 has a read take the blob's present copies in an order; the first build read them with a statement of their own
(`BlobCopies`) before it asked the object store. [architecture.md](architecture.md) R-ARCH-027 allows a local download one statement,
the item lookup, and the budget tests of `test/integration/querybudget` measured two (`GetLiveItem`, `BlobCopies`), three for the remote
member of a virtual repository. Every route that serves a stored item reads it with the one lookup `GetLiveItem`
([files.sql](../../internal/meta/postgres/queries/files.sql)): the downloads of every format, `/files/`, the registry's blobs and
manifests, ranges, the remote hit and the tiers of a virtual repository, the requests a Bastion forwards.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The item lookup reads the copy rows of the item's blob as one JSON array (a lateral join on `blob_copies` and `storage_connections`), the item carries them, and `blob.Store.Open` takes them (`OpenOptions.Copies`, `CopiesRead`) instead of reading them (chosen) | One join more in a statement every download runs; the copies are those of the same snapshot as the item and its blob | A download costs the one statement of R-ARCH-027 on every route that reads its item with the lookup; the copies are exactly as current as with a separate read just before the object store is asked, so R-STOR-055 holds unchanged; no state in the process | The statement every download runs grows by an index scan of the blob's copies (one to eight rows); a reader that builds its item another way still asks for the copies itself |
| (b) A cache of the copies per blob in each replica | No statement on a hit | No change of the lookup | Wrong for as long as it lives: a copy another replica marked missing or corrupt is tried again (a corrupt copy would be served to a range, H-11, where a fresh read leaves it out), a copy collection or a drain deleted is asked for, a new copy after a repair is not used; a change area for every mark would cost a write per mark and still leave a window |
| (c) Exempt the read of the copies from the count, or raise the budget | Nothing to build | — | Breaks R-ARCH-027 and its tests; the budget is the product's, not the build's |

**Choice:** (a). **Rationale:** the copies are a fact about the item's blob, which the lookup reads already; a separate read gave the
read nothing a join does not, and a cache would give it less than R-STOR-055 promises. Where a read changes the facts itself before it
serves (the recheck of a missing blob, Q-STOR-153, which sets a copy present), the item drops the copies it read and `Open` reads them
again. Internal readers that hold no item (the NuGet view's ranged reads, the scans, the proxy's stored documents on a miss of their cache)
read the copies once per blob with `Store.Copies`; the NuGet view's reader reads them once for all its ranges.

**Applied in the spec:** R-STOR-055; code: `GetLiveItem`, `items.Item.Copies`, `blob.ParseCopies`, `blob.OpenOptions`
([download.go](../../internal/blob/download.go)).

### Q-STOR-300 · Segments of 64 KiB under AES-256-GCM with the nonce stored in each segment

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That the format is
Quader's own, after the model of the proxy's segment chain and bound to the content, is the owner's (2026-11-22, decision 1 (a)).

Context: the proxy seals 64 KiB segments with AES-256-GCM and a random nonce stored per segment; Tink and age derive the nonce from a prefix
and the index and mark the last segment in it (research of 2026-11-22, topic B.1). A part of an upload session can be sent again by a later
request with other bytes (R-STOR-011), and a reused nonce with another plaintext loses the authentication of that key (NIST SP 800-38D §8).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) 64 KiB segments, AES-256-GCM, a 12-byte nonce stored in front of each segment, from a source the writer chooses (chosen) | 28 bytes per segment | A reader needs no nonce rule; a part sealed again by a later request draws other nonces, so no reuse whatever the client sends; standard library only | 12 bytes per segment more than a derived nonce (0.018 %) |
| (b) Nonce = a per-object prefix and the index (Tink, age) | No stored nonce | Smallest overhead | A part sent again with other bytes reuses its nonces under the same key: forgery of that object becomes possible |
| (c) ChaCha20-Poly1305 or AES-GCM-SIV | Another AEAD | GCM-SIV tolerates a reused nonce | Not in Go's standard library (GCM-SIV) or slower without hardware AES on the servers Quader targets; nothing gained over (a) |

**Choice:** (a). **Rationale:** the stored nonce removes the one misuse Quader's own upload path could cause; 64 KiB keeps the proxy's
measured balance between overhead and the over-read of a range. The planner would revisit the segment size if a measurement shows ranged
reads of small files dominating.

**Applied in the spec:** R-STOR-076, R-STOR-081.

### Q-STOR-301 · A writer derives the nonces from a seed per part so that a retry sends the same bytes without keeping them

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: Quader sends every part with a payload checksum computed before the request and lets the SDK retry a part by seeking its body back
(R-STOR-006, [transfer.go](../../internal/blob/transfer.go) `wholeChecksum`); with random nonces per sealing, a retry would send other bytes
than the checksum describes unless the ciphertext is kept.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Nonces from `HMAC-SHA-256(seed, …)` with a seed drawn per write (per request of a session), the body encrypted on the fly (chosen) | Seek back, encrypt again, same bytes | No ciphertext held: one segment of memory per reader; the checksum and the retry agree | The plaintext is encrypted twice when a payload checksum is wanted |
| (b) Random nonces, the ciphertext of a part kept in memory | Hash it, send it, keep it for a retry | One encryption | Up to `max_concurrent_parts` × part size (64 × 16 MiB = 1 GiB) of memory per replica |
| (c) Random nonces, the ciphertext spooled to disk | As (b) on disk | One encryption | A second spool of the size of every upload in flight |

**Choice:** (a). **Rationale:** AES-GCM costs less than the disk or memory that would hold its output; the nonces stay unique because a new
seed comes with every plaintext the writer did not already seal.

**Applied in the spec:** R-STOR-076, R-STOR-080, R-STOR-081.

### Q-STOR-302 · An object ends with a final segment that carries its length and its SHA-256

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the proxy ends an object with a trailer at index `2^64 − 1` (length and CRC32C) and marks no last segment; the formats that got
truncation right mark the last segment (research, topic B.4). An upload session learns that a part is the last only at its finalize, after
the part was sealed and sent.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A final segment at index `n`, marked `0x01`, holding `u64(L)` and the plaintext's SHA-256 (chosen) | Data segments are never the last | Truncation, extension and a moved end fail by mark and index; a session seals it at its finalize; the digest inside lets a reader without a database check the body | 68 bytes per object |
| (b) The proxy's trailer at `2^64 − 1` with CRC32C | No mark | The proxy's layout | No "last" in any segment's authentication; CRC32C proves less than the digest Quader has anyway |
| (c) Mark the last data segment | No extra segment | Smallest | A session cannot know the last segment when it seals a full part |

**Choice:** (a). **Rationale:** it is the marked last segment the standards recommend, placed where a session can write it.

**Applied in the spec:** R-STOR-076, R-STOR-081.

### Q-STOR-303 · The wrap carries the digest and the size in its sealed plaintext under a key derived per wrap

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That the wrap lies once
at the object, bound to the digest, under the connection's storage key, is the owner's (2026-11-22, decision 2 (e)).

Context: on a connection with keyed names the object's name does not tell the digest; a reader with a database knows it from the copy row,
the offline tool without one does not. The proxy's wrap binds nothing (its additional data is a constant) and uses the key directly.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The wrap's plaintext is `DEK ‖ digest ‖ u64(L)`, the additional data names the key, `K_w` is derived from the storage key with a random salt per wrap (chosen) | A reader compares the unwrapped digest with the copy's | The offline tool learns the digest from the record; no bound on wraps per storage key; another blob's wrap names another digest | 40 bytes more in the wrap |
| (b) The digest in the additional data only | Bound, not carried | Shorter | Opening needs the digest first: the offline tool could not open an object under a keyed name |
| (c) The digest in a metadata key of its own | Readable by anyone | Simple | Gives the provider the digest that keyed names hide |

**Choice:** (a). **Rationale:** the binding is the same as by additional data (a wrap that names another digest is refused), and it keeps the
record self-describing for the one reader without a database.

**Applied in the spec:** R-STOR-077, R-STOR-089.

### Q-STOR-304 · A commitment to the data key in the record, checked before the first segment

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: AES-GCM is not key-committing: a ciphertext can be made to open under two keys into two valid files (Albertini and others, 2020/1456).
The proxy addresses it nowhere. In this stage every copy has its own data key and only Quader makes wraps; the handover of decision 12 (b) and
a later level of keys per project (decision 3) would let more than one record or reader stand for one body.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `C = HKDF(DEK, "", FMT ‖ " commitment")` in the record, the segment key derived from `DEK` as well, compared before the first segment (chosen) | One record names one data key | AWS's committing suites do the same; cheap (32 bytes, one HKDF); fails before a byte | Binds the record, not the body (H-1302) |
| (b) No commitment | As the proxy | Nothing to write | The form of the attack the research names stays open for every later reader of a body |
| (c) The commitment in a header at the start of the body | Binds the body | Every reader of the body sees one commitment | A ranged read away from the start needs a second request for the header |

**Choice:** (a). **Rationale:** it closes the attack wherever a record is read and costs a ranged read nothing; what (c) adds matters only once
one body carries several records, which no path of this stage creates.

**Applied in the spec:** R-STOR-076, R-STOR-077.

### Q-STOR-305 · One object holds at most 4 TiB of plaintext

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `L ≤ 2^42` (chosen) | At most `2^26` data segments, stored at most 4,399,925,559,364 bytes | Below the 5 TiB an S3 object can hold; far above `storage.upload.max_size` (10 GiB by default) and a multipart upload of 10,000 parts of 16 MiB (156 GiB) | A limit of its own to state |
| (b) 5 TiB of plaintext, as the proxy | Its limit | The same number as S3 | The stored object would exceed what S3 stores |

**Choice:** (a). **Rationale:** the stored object must fit the store.

**Applied in the spec:** R-STOR-076.

### Q-STOR-306 · An encrypted connection needs metadata, and without a copy that replaces it the finalize streams

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: stage 1's start check records `capabilities.metadata` and `capabilities.copyReplacesMetadata` (R-STOR-035). The software behind the
owner's provider documents a copy with new metadata; his own installation's object gateway (Ceph, in his cluster) was not researched; nothing
was run against either.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `metadata` required; without `copyReplacesMetadata` the finalize streams the staging object with the record (chosen) | Refuse what cannot work, degrade what can | The record cannot live anywhere else (decision 2); a store without the copy loses only the server-side finalize | A stream through the replica for every session on such a store |
| (b) Both required | Refuse more | One path | A store that keeps metadata but cannot replace it on a copy could not be encrypted at all |
| (c) Neither required, the record kept in an object beside each blob | A second object per blob | Works on any store | Two places for one key, the drift the owner rejected |

**Choice:** (a). **Rationale:** the stage-1 pattern: a missing required ability refuses the connection, an optional one degrades by a stated rule.

**Applied in the spec:** R-STOR-035, R-STOR-075, R-STOR-081.

### Q-STOR-307 · A session's data key is sealed in its row and its tails are sealed under a key of their own

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: a session's requests reach any replica (INV-18); its row keeps the hash state between them
([00054_upload_sessions.sql](../../internal/meta/postgres/migrations/00054_upload_sessions.sql)); its tails are objects of their own, read
back by the next request (R-STOR-011).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The data key sealed with the key ring in `upload_sessions.sealed_data_key`; tails sealed under `K_tail` with their offset in the additional data (chosen) | Any replica opens it; a tail never passes as a blob segment | Nothing in memory or Valkey between requests; the re-encrypt task covers it; no plaintext tail at the provider | One more sealed column |
| (b) The data key in the staging object's metadata, wrapped under the storage key | Like a blob's record | No column | A staging object's metadata is set at its creation and cannot change; the provider sees a wrap of an unfinished object |
| (c) Tails in plaintext | As today | Simplest | Up to one part of plaintext per session lies at the provider |

**Choice:** (a). **Rationale:** the session's state already lives in its row; its key belongs beside it, sealed like every secret there.

**Applied in the spec:** R-STOR-081, R-STOR-083.

### Q-STOR-308 · A copy into an encrypted connection always gets a new data key

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That switching encryption
on or off is a new connection which the reconciliation fills is the owner's (2026-11-22, decision 7). **An open question for the owner**,
specified meanwhile with (a).

Context: between two encrypted connections the reconciliation can carry the data key (copy the ciphertext, wrap the key again under the other
storage key, server-side within one endpoint) or write new bytes. The answer to a leaked storage key is a new connection filled with new data
keys (R-STOR-085).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) New data key, new bytes for every copy into an encrypted connection (chosen) | Decrypt, encrypt again, through the worker | Each connection's keys protect its copies alone: a leaked storage key never reaches another connection's objects; the leak's answer needs no mode; two providers hold no identical ciphertext | No server-side copy between two encrypted connections on one endpoint |
| (b) Carry the data key | Same bytes, a new wrap | Server-side copies within one endpoint; less CPU | A leak at one connection exposes the copies on every connection that carried its keys; the leak's answer needs a mode that forbids carrying |
| (c) Carry by default, new keys on a flag of the connection | Both | Cheap by default | A flag an operator must remember exactly when a key has leaked |

**Choice:** (a). **Rationale:** a server-side copy between encrypted connections arises only when a provider keeps two of them (another prefix,
the name detail changed); across providers every copy streams anyway. Recommendation to the owner: (a).

**Applied in the spec:** R-STOR-062, R-STOR-087.

### Q-STOR-309 · A storage key is a row with an identifier of its own, drawn at the connection's first start, never deleted

Status: **Decided by the development agent on 2026-11-22**, after the owner's word that its favourite is built and not what was specified
first: option (b); what the owner had been told when he decided the topic. That the storage keys lie once, a sealed row per connection and
generation with an export command, is the owner's (2026-11-22, decision 15 (a)); a destroyed key keeps its row and loses its sealed value
([Q-STOR-402](#q-stor-402--a-destroyed-key-keeps-its-row-loses-its-sealed-value-and-names-its-time)). *Superseded in place on 2026-11-22:
decided by the planner in the owner's absence, put to the owner as an open question (whether leaving a provider shall destroy its keys),
specified meanwhile with (a).*

Context: the working paper said in passing that whoever leaves a provider deletes its storage key, and what stays there becomes unreadable.
Noncurrent versions of a bucket and the pool of a backup hold objects wrapped by that key (R-BAK-031). A backup's pool is shared, keyed by
the digest and never copied again (R-BAK-006), so without a change a retained backup would name a retired connection's key for as long as the
blob lives, and a command refused while a backup names a key would be refused for good.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Identifier `sk-<connection>-<generation>-<random>`; drawn when the connection is first recorded; refused over a location that holds objects; never deleted | Keys outlive their connection | No restore and no `restore-versions` ever meets an object whose key is gone; a key drawn again after a lost database never opens its predecessor's objects | Leaving a provider leaves ciphertext there that the database and every backup can still open (H-1308) |
| (b) As (a), and a command destroys a retired connection's keys, refused while a retained backup's run names a key (chosen) | Crypto-shredding on request | What the paper promised | Backups must record which keys their pool objects name; a refused command when one backup still holds one |
| (c) Keys deleted when a connection retires | Automatic | Leftovers unreadable at once | A backup's pool objects from that connection become unreadable silently |

**Choice:** (b), specified in R-STOR-097 to R-STOR-103 and backup-and-restore.md R-BAK-032 and decided in detail in Q-STOR-400 to
Q-STOR-413, backup-and-restore.md Q-BAK-74 to Q-BAK-76 and ha-and-helm.md Q-HA-416 and Q-HA-417
([ADR-255](../decisions.md#adr-255--the-storage-keys-of-a-retired-connection-can-be-destroyed-so-that-what-stays-at-a-provider-becomes-unreadable)).
**Rationale:** it is what the owner was told when he decided the topic, and the format needs no change for it. Its price is paid in the
backup's pool: an encrypted pool object is named by its key and copied again from a connection that stays once its own connection drains or
goes (Q-BAK-74), so that a retained backup stops naming a departed connection's key once older backups leave retention. What the promise
holds and what it does not is in the operator's words on [stored-content-keys.md](../security/stored-content-keys.md#destroying-a-retired-connections-keys).
*Superseded in place on 2026-11-22: the choice was (a), with the recommendation (a) now and (b) when an installation must make a provider's
leftovers unreadable; the rationale was that nothing is lost by accident.*

**Applied in the spec:** R-STOR-083, R-STOR-085, R-STOR-097 to R-STOR-103, and the edits of R-STOR-051, R-STOR-077, R-STOR-078,
R-STOR-082, R-STOR-087 to R-STOR-090; backup-and-restore.md R-BAK-031, R-BAK-032; ha-and-helm.md R-HA-054.

### Q-STOR-310 · A replica holds the storage keys in memory and reads the rows again for an unknown identifier

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Opened at start, kept for the replica's life, refreshed by the change area `storage_keys` and by a read of an unknown identifier at most every 10 s per connection (chosen) | No query per read | A new generation reaches every replica within the change areas' bound; a replica that is late reads the rows when it meets the new identifier | Keys in memory for the replica's life (H-1309) |
| (b) Open a storage key per read | Rows read and opened per request | Nothing kept | A query and an AES open per download |
| (c) A two-phase activation of a generation, as the key ring's rollout | Every replica confirms before any wraps | Strict | The keys live in the database every replica reads; the lazy read gives the same guarantee without a protocol |

**Choice:** (a). **Rationale:** the rule "every replica can open before any seals" holds because a replica that cannot open reads the shared
rows; the key ring needs two rollouts only because its keys are files. *Qualified on 2026-11-22 by
[Q-STOR-407](#q-stor-407--replicas-forget-destroyed-keys-at-the-change-and-a-load-skips-them): a key whose row is destroyed is dropped at the
change, so "for the replica's life" ends there.*

**Applied in the spec:** R-STOR-083.

### Q-STOR-311 · A record that names an unknown storage key is not damage

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Fall over, mark nothing, count it, alert critical after 10 minutes (chosen) | A missing key is a configuration fault | The scrub and the reconciliation never overwrite an object that the export's import could still open | A damaged identifier inside a genuine record is not repaired until someone looks |
| (b) Mark the copy `corrupt` | As any failure | Self-healing | After a database restored too far back, the repair rewrites every object of the lost generation from other copies or loses the blobs whose only copy it was |

**Choice:** (a). **Rationale:** a key that can come back is the one failure a repair must not race. *Qualified on 2026-11-22 by
[Q-STOR-406](#q-stor-406--a-read-that-meets-a-destroyed-key-answers-destroyed_key-and-marks-the-copy): an identifier whose row is destroyed
is not unknown; it is `destroyed_key`, which marks the copy, because that key never comes back.*

**Applied in the spec:** R-STOR-077, R-STOR-082, R-STOR-087, R-STOR-088.

### Q-STOR-312 · The export is one document of sealed rows and the import adds missing rows

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) JSON with the sealed values as they lie, the installation and the root key identifiers; the import opens each value first and adds missing rows (chosen) | The ring opens it, nothing else | No key ring needed to export; a wrong ring is refused before a write; useful after a restore to an earlier point and as the offline tool's input | Opens only with the root keys it names (H-1306) |
| (b) Plain keys, for the operator to keep | Readable alone | Needs no ring | A file that opens every blob by itself, beside the ring or anywhere |
| (c) No export; backups of the database carry the rows | Nothing new | Nothing to build | Not what the owner chose; no recovery path for a generation drawn after the last backup |

**Choice:** (a). **Rationale:** the owner's words, "printed sealed for safekeeping beside the key ring".

**Applied in the spec:** R-STOR-085, R-STOR-089.

### Q-STOR-313 · Keyed names lie below keyed/ and are an HMAC of the digest under a name key of the connection

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. Keyed names on an encrypted
connection, on by default and fixed, are the owner's (decision 5 (a) with his note, read and decided by the development agent on 2026-11-22).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `keyed/<m0m1>/<m2m3>/<m>`, `m = HMAC-SHA-256(name key, FMT ‖ " name" ‖ digest)`, a random name key per connection kept like a storage key (chosen) | Another family, the same fan-out | A keyed name never passes for a digest; the orphan sweep tells the families apart | One more key per connection |
| (b) The name key derived from the storage key | No row | One key less | A new generation would rename every object |
| (c) The name key derived from the root | No row | One key less | A rotation of the root would rename every object |

**Choice:** (a). **Rationale:** a name must outlive every rotation and generation, so its key is a row of its own that never changes.

**Applied in the spec:** R-STOR-001, R-STOR-086.

### Q-STOR-314 · The orphan sweep reads the record of an object it cannot place and never deletes one without a readable record

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One `HeadObject` and an unwrap for an object without a copy row; adopt or claim by the unwrapped digest; report an unreadable one (chosen) | The record is the map | Exactly the stage-1 rules, with the digest from the record | One request per object without a row (rare: a crashed write, a restore) |
| (b) Delete what has no row after `min_age` | No record read | Cheap | Deletes the objects of a live blob whose row a restore lost |
| (c) Keep a table of names | A mapping in the database | No request | A second place for what the copy row already holds |

**Choice:** (a). **Rationale:** Quader always writes the record with the bytes, so an object without a readable record is not Quader's writing.

**Applied in the spec:** R-STOR-026, R-STOR-087.

### Q-STOR-315 · The journal, the probes and the marker stay plain on an encrypted connection

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Plain (chosen) | As stage 1 | The journal is read by a restore that replaces the database where the storage keys lie; the marker is read before any key; the probes live for a check | The provider reads the journal's identifiers (H-1316) |
| (b) The journal encrypted under the storage key | Sealed entries | Hides identifiers and would authenticate entries against a bucket writer (H-553) | A restore from a backup older than a generation could not read the newest entries, which are the ones the journal exists for |

**Choice:** (a). **Rationale:** the journal's integrity wants a signature under a key outside the database, a topic of its own; encryption under
a key from the database it replaces would break the one case it is for.

**Applied in the spec:** R-STOR-088.

### Q-STOR-316 · The offline tool is a command of the binary and reads a bucket or a directory with record files

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. An offline tool is the owner's
(decision 1 (a)).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `quader admin storage offline` with `list`, `get`, `verify` and `pull`, from a connection element or a directory written by `pull` (chosen) | One binary | The same codec as the server; no database, no Valkey | A directory copied by another tool without its metadata cannot be read (H-1303) |
| (b) A separate small binary | Its own release | Smaller | Two artifacts to release and keep in step |
| (c) Read only from a bucket | No directory mode | Simpler | No way to read a copy kept on a disk |

**Choice:** (a). **Rationale:** the binary already carries the codec and the key ring's file format; `pull` keeps the records with the bytes.

**Applied in the spec:** R-STOR-089.

### Q-STOR-317 · A ranged read is one request for the segments of its window

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One ranged `GetObject` of the window's segments, the final segment with them at the end; the record comes with the answer (chosen) | As many requests as today | No `HEAD`; every byte released is authenticated | Over-reads up to two segments |
| (b) `HEAD` and a ranged `GET` pinned with `If-Match` (the proxy's library) | Two requests | The total is known before | Twice the requests; the store's answer to the `GET` carries the same information |

**Choice:** (a). **Rationale:** the store sends the metadata and the total with every ranged answer.

**Applied in the spec:** R-STOR-082.

### Q-STOR-318 · A session counts the segments it seals under its data key

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `sealed_segments` in the row, raised in the claim transaction, refused past `2^31` (chosen) | A hard bound | Keeps one key below the NIST bound however often a client sends a part again | One column and one addition per request |
| (b) No count | Trust the session's age limit | Nothing to build | A client that sends parts again in a loop for the 20 hours a session may live is bounded only by arithmetic, not by a check |

**Choice:** (a). **Rationale:** a bound that is checked costs less than an argument that it cannot be reached.

**Applied in the spec:** R-STOR-076, R-STOR-081.

### Q-STOR-319 · Every behaviour of the encryption once at the cheapest tier, and its fuzz targets in the nightly

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the repository's rule that tests stay runnable (owner, 2026-11-20). No fuzz job runs in the nightly today; the fuzz legs of npm and
NuGet run only on a dispatch with `closing=true` ([release.yml](../../.github/workflows/release.yml)).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Vectors and every attack at the unit tier; one integration case for a rotation, one for the jobs, one in the backup area; one conformance pass with an encrypted connection; a nightly fuzz job, five minutes per target (chosen) | About 2.9 s of unit tests, 18 s of integration, 3 s of conformance per push; 27 minutes nightly | Nothing per format; the readers of an untrusted format are fuzzed every night | A new nightly job |
| (b) Fuzz only on the closing dispatch, as npm and NuGet | No nightly job | Nothing new in the nightly | The readers of the bytes every encrypted download passes would be fuzzed only at the end of a phase |
| (c) An end-to-end case per format with encryption on | Real clients | Shows each client | Grows with every format and shows nothing a format can change |

**Choice:** (a). **Rationale:** the format is read on every download of an encrypted connection; its readers deserve the nightly.

**Applied in the spec:** R-STOR-079, R-STOR-091.

### Q-STOR-320 · The handover's room is one function of the store that nothing calls in this stage

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. The handover of a link and a
data key per released blob to a Bastion is the owner's (decision 12 (b)) and is specified by a later planner.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `blob.Store.CopyDataKey(ctx, digest, connectionID)`, held to its callers by an architecture test (chosen) | One function, no caller | The later planner builds on a tested seam; nothing hands out a key before then | A function without a caller in this stage |
| (b) Nothing now | — | Less code | The record's reading would be designed twice |

**Choice:** (a). **Rationale:** the owner asked for the room; one function with a guard is the smallest room that can be tested.

**Applied in the spec:** R-STOR-083; its caller is named by R-STOR-096 ([Q-STOR-340](#q-stor-340--the-handover-is-decided-in-the-package-of-the-links-the-one-caller-of-copydatakey-besides-the-store)).

### Q-STOR-321 · A staging tail's nonces take its session offset, and a tail holds at least one byte

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-081 gives a tail segment's sealing and its additional data (`FMT ‖ u8(mark) ‖ u64(o) ‖ u64(j)`), but no nonce rule and no
layout beyond "segment `j`"; R-STOR-080's rule (`HMAC-SHA-256(seed, FMT ‖ " nonce" ‖ u8(mark) ‖ u64(i))`) is written for an object's
segments. A reader needs neither, since every nonce is stored, but a writer must keep R-STOR-076's rule under `K_tail`, and the vector
file lists every nonce of its tail case.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The nonce is the first 12 bytes of `HMAC-SHA-256(seed, FMT ‖ " nonce" ‖ u8(mark) ‖ u64(o) ‖ u64(j))`, the shape of the tail's additional data; every segment but the last holds 65,536 bytes; a tail holds at least one byte, so its last segment, marked `0x03`, always exists (chosen) | One nonce rule per kind of segment, the data segments' layout | No two tail segments sealed under one seed share a nonce, even if a request ever wrote tails at two offsets; the stored size `m + 28·⌈m / 65,536⌉` follows from the row's size; a cut at a segment boundary removes the `0x03` segment or changes the size | A request whose remainder is empty writes no tail object |
| (b) R-STOR-080's rule with `j` alone | One rule for both kinds | Shorter | Unique only while each request writes one tail; a second tail of a request at another offset would repeat every nonce under `K_tail` |
| (c) An empty tail as one segment of 28 bytes | Every request writes a tail | A tail always exists | An object that holds nothing, written and read back for nothing |

**Choice:** (a). **Applied in the spec:** R-STOR-081; code: `NewTailSection` and `NewTailReader` of
[internal/blob/blobcrypt](../../internal/blob/blobcrypt/seal.go), the vector `tail-131072`.

### Q-STOR-322 · A missing or malformed quader-key is record, not unknown_key

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: step 2 of R-STOR-077 names `unknown_key` for an identifier the connection does not hold, and Q-STOR-311 makes that reason mark
nothing, because the export's import can bring the key back. Open was what a record gives whose `quader-key` is missing, or is no identifier
at all (other characters, more than 64 of them).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `record`: the copy is marked `corrupt` and repaired from another connection (chosen) | Only a well-formed identifier can be unknown | No import can bring back a key whose identifier the table `storage_keys` cannot hold (its `CHECK` refuses it), so nothing waits for one; the alert `QuaderStorageUnknownKey` stays a sign of missing rows | A record damaged in exactly this field is repaired, not reported as a missing key |
| (b) `unknown_key` | Any identifier the replica does not hold | The words of step 2 | The copy is never repaired, and the alert says storage keys are missing when a record is damaged |

**Choice:** (a). By the same reading a missing `quader-wrap` is `record` (step 3) and a missing `quader-commitment` is `commitment`
(step 6). **Applied in the spec:** R-STOR-077; code: `Open` of [internal/blob/blobcrypt](../../internal/blob/blobcrypt/record.go).

### Q-STOR-323 · The closing run does not start the fuzz job of the stored format

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-091 said that a dispatch with `closing=true` runs the job for 30 minutes per target, as the fuzz legs of npm and NuGet run.
`closing` is an input of Test and Release (`release.yml`), which the closing run dispatches; the job lies in the nightly workflow
(`performance.yml`), which has no such input and which the closing run does not start
([releasing.md](../developer/releasing.md#the-run-that-closes-a-phase)).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The nightly job alone, five minutes per target every night; a longer run by hand with `make fuzz-stored-format STORED_FORMAT_FUZZTIME=30m`, or a dispatch of the nightly with `-f sets=fuzz-stored-format` (chosen) | No variant for the closing run | The readers get 25 minutes of fuzzing every night, more in a week than 2.5 hours once at a phase's end; nothing outside the job changes | No longer run is tied to the end of a phase |
| (b) An input `closing` in the nightly workflow | The nightly learns the closing run | The 30 minutes the sentence named | A second workflow to dispatch for one closing run, and an input that every other job of the nightly ignores |
| (c) Five legs in Test and Release under `closing=true`, as `fuzz-npm` and `fuzz-nuget` | Like the formats' fuzz runs | The closing run holds every fuzz leg | 2.5 runner hours more per closing run for readers the nightly fuzzes anyway |

**Choice:** (a). **Applied in the spec:** R-STOR-091; code: the job `fuzz-stored-format` of
[performance.yml](../../.github/workflows/performance.yml) and the Makefile block `fuzz-stored-format`.

### Q-STOR-324 · The identifier of a key names its kind, connection and generation, and the table holds the row to it

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-083 seals a key under the row key `key_id`, so the key ring's additional data binds the table, the column and the identifier,
and the identifier is `sk-<connection>-<generation>-<random>` or `nk-<connection>-<random>`. The columns `connection_id`, `kind` and
`generation` were bound to nothing: a writer of the database, or a file to import, could give a sealed storage key of one connection another
`connection_id`, and a replica would then hold one connection's key as another's, which R-STOR-078's row "the object copied to another
connection" relies on not to happen.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The identifier is the truth: CHECKs of the table that `key_id` is the kind's prefix, `connection_id`, the generation of a storage key and 8 hex digits, and that a name key has generation 1; the holder and the import check the same with `keys.ParseID` (chosen) | Three columns that follow from one | The seal's binding to the identifier binds the connection, the kind and the generation with it; the database refuses a changed column, the import a changed file | Two CHECKs beyond the DDL as first written |
| (b) The code checks, the table does not | As (a) without the CHECKs | The DDL as first written | A database writer is held by the replica's check alone |
| (c) The seal's row key is the identifier and the three columns | `secrets.Register("storage_keys", "key_id,connection_id,kind,generation", …)` | The key ring binds all four | A composite row key for columns that follow from one; the export and every walk carry four parts |

**Choice:** (a). **Applied in the spec:** R-STOR-083 (the DDL); code: `ParseID` of [ids.go](../../internal/blob/keys/ids.go), the CHECKs of
[00107_storage_keys.sql](../../internal/meta/postgres/migrations/00107_storage_keys.sql), `checkKeys` of [export.go](../../internal/blob/keys/export.go).

### Q-STOR-325 · An import inserts what is missing as retired and makes the highest generation of each connection active

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-085 says the import inserts the rows that are missing and keeps the rows that are equal; the table holds one active storage
key per connection. A database restored to a point before generation `n + 1` was drawn has generation `n` active, while the export lists `n`
retired and `n + 1` active: inserting `n + 1` as the file says conflicts, and the state of the equal row `n` differs between the two.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A missing row goes in retired; afterwards the storage key of the highest generation of the connection, of the database's and the file's, is active and the others retired, and a name key is active; an equal row keeps the database's state before that rule; a row whose connection, kind and generation the database holds under another identifier refuses the import (chosen) | Generations only grow | One rule whatever the order of exports and restores; the replicas wrap under the newest key the installation ever drew | An older generation the database had active is retired when the file brings a newer one |
| (b) The file's states win | The export's view | Simple | An older export would make an older generation active again over one drawn after it |
| (c) The database's states win, missing rows retired | Nothing active changes | Never moves the active key | After a restore to before generation `n + 1` the replicas go on wrapping under `n`, and the newest key only reads |

**Choice:** (a). *Qualified on 2026-11-22 by
[Q-STOR-409](#q-stor-409--an-import-skips-a-destroyed-key-reports-it-and-inserts-the-destroyed-rows-the-database-lacks): destroyed rows take
no part in the rule, so the highest generation that is not destroyed is active.* **Applied in the spec:** R-STOR-085, R-STOR-103; code:
`Import` and `activateHighest` of [export.go](../../internal/blob/keys/export.go).

### Q-STOR-326 · A replica holds the keys of every encrypted connection before it turns ready, and a draining one draws nothing

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-083 draws the keys when a replica first records an encrypted connection, refuses over a used location, and loads the rows
after the start check. Open were the place in the start, what a store that cannot be listed does to it, and what a draining connection and a
missing name key get.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) After the start checks and before readiness, for every encrypted connection in list order: draw what a connection without drain lacks, then load its rows; a failure of the database or the store is retried and holds the start, a refusal (a used location without keys, a key the ring cannot open) ends it; the refusal holds for every kind it would draw, the name key included (chosen) | One place, before any read or write | No replica serves a copy of an encrypted connection whose keys it lacks, and none counts `unknown_key` for a key it never loaded; a draining connection takes no write, so it needs no new key | A new encrypted connection whose store cannot be listed at its first start holds the replica's start back |
| (b) In the start check's callback per connection, also after readiness | When the connection's check passes | An unreachable new store holds nothing back | A refusal after readiness cannot end the start; every reader and writer would need a state "without keys" |
| (c) Lazily, at a connection's first write or read | No step at start | Nothing at start | The refusal over a used location would come as a failed upload |

**Choice:** (a). Revisit when encrypted connections run (package 3): (b) if a new encrypted connection's store is often unreachable at its
first start. **Applied in the spec:** R-STOR-083; code: `Ensure` of [draw.go](../../internal/blob/keys/draw.go), `ensureStorageKeys` of
[wire_storage_keys.go](../../internal/app/wire_storage_keys.go).

### Q-STOR-327 · The rule of CopyDataKey reads the syntax of every package and reserves the method's name

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-083 and R-STOR-096 ask for an architecture test that fails for a call of `CopyDataKey` from any package but `internal/blob`
and `internal/downloadlinks`. The rules of `internal/archtest` read either the import graph (`go list`) or typed syntax (`LoadTyped`, seconds
per run).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Syntax below `internal/` and `cmd/`: every selector `CopyDataKey` and every string literal that holds the name, outside the two packages (their subpackages refused too), the test files and the trees the rules leave out (chosen) | The name is reserved | Catches a call, a method value, a call through an interface or an embedding and a call by reflection; milliseconds; a fixture shows each | A method of that name on another type elsewhere is refused as well |
| (b) Typed: the selections of `(*blob.Store).CopyDataKey` only | Exact | No false positive | Misses a call through an interface and by reflection; seconds per run |
| (c) An import rule | No import of `internal/blob` outside the two | Cheap | Every format and the item store import `internal/blob` |

**Choice:** (a). **Applied in the spec:** R-STOR-083, R-STOR-096; code: `CheckDataKeyCallers` of
[datakey.go](../../internal/archtest/datakey.go), `TestR_STOR_096_CopyDataKeyHasTwoCallers`.

### Q-STOR-328 · A new generation needs a key ring that opens the generation it retires

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: `quader admin storage keys new-generation` seals the new key with the command's key ring. A command run with a ring the replicas do
not hold (another installation's files, a wrong `active_key_id`) would seal a key no replica opens: every replica that learns of it fails to
load it and goes on wrapping under the retired generation.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The command opens the active generation with its ring before it draws; a failure exits `2` and names the root key (chosen) | The ring proves itself | A key sealed under a ring the installation does not use is never written | One open more per command |
| (b) Seal and trust the configuration | — | Simpler | The failure shows on every replica, after the commit, as a key that does not load |

**Choice:** (a). **Applied in the spec:** R-STOR-083; code: `NewGeneration` of [generation.go](../../internal/blob/keys/generation.go).

### Q-STOR-329 · The export is audited in the transaction that reads it, before the document leaves

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-085 audits `system.storage.keys_exported`, without saying when; the export is the one copy of the sealed keys that leaves the
database on purpose.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Read the rows and write the event in one transaction, then print; `--connection` with an id that has no rows exits `2`, and an export of every connection of an installation without keys prints a document without keys (chosen) | No export without its trace | The audit trail names every export that left; a mistyped id is not taken for an empty connection | An export whose printing fails after the commit is audited though nothing left |
| (b) Print, then audit | The trace follows | The trail lists only exports that left whole | A failed insert leaves an export without a trace |
| (c) An asynchronous event | The usual event of a read | Never fails the command | An export could leave without a trace |

**Choice:** (a). **Applied in the spec:** R-STOR-085; code: `Export` of [export.go](../../internal/blob/keys/export.go), `runKeysExport` of
[admin_storage_keys.go](../../cmd/quader/admin_storage_keys.go).

### Q-STOR-340 · The handover is decided in the package of the links, the one caller of CopyDataKey besides the store

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it. That a Bastion gets a link
and the data key per released blob is the owner's (2026-11-22, decision 12 (b)).

Context: the links' decision lives in the domain package `internal/downloadlinks`, and `edge.ServeItem` takes it as an option
([architecture.md](architecture.md) Q-ARCH-570): the public addresses, the host rule, the link check, the signing and the connections' state
are there. Q-STOR-320 left `CopyDataKey` without a caller and asked this planner to name its package.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `internal/downloadlinks`, a file of its own beside the links' decision; `edge.ServeItem` takes `ServeOptions.Handover`; the architecture test allows `CopyDataKey` from it and from `internal/blob` (chosen) | One package signs every link the installation hands out | Nothing of the links' machinery is exported or copied; one place to review what leaves the main installation | One package with two answers |
| (b) A package of its own, `internal/handover` | Separate | | The links' machinery shared through exports of their package, or duplicated |
| (c) The main installation's side of the Bastion, `internal/bastionmain` | Bastion code together | | A package that knows the interface would sign links and open data keys |

**Choice:** (a). **Applied in the spec:** R-STOR-095, R-STOR-096, R-STOR-083.

### Q-STOR-341 · A plain copy is handed before an encrypted one

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Of the copies a read would try now, the first on a connection that is not encrypted, else the first on an encrypted one (chosen) | A data key leaves only where no plain copy can serve | The owner's "the data key where the blob lies on an encrypted connection", read narrowly | The Bastion may read from a provider later in the list than the main installation would |
| (b) The read order alone | The operator's order | | A data key leaves although a plain copy was there |

**Choice:** (a). **Applied in the spec:** R-STOR-095.

### Q-STOR-342 · The details of the installation's links hold for a handover

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `minSize`, `linkLifeSeconds` and the public addresses of R-STOR-014 (chosen) | One set of details for every link the installation signs | Nothing new for the administrator; a test sets `minSize` 0 in the existing row and moves kilobytes | The minimum size of the clients' links and of handovers is one number |
| (b) Details of their own | | Two thresholds | Three fields more for an expert case |
| (c) Fixed values | | | A test could not use kilobytes |

**Choice:** (a). **Applied in the spec:** R-STOR-014, R-STOR-095.

### Q-STOR-343 · A speed limit does not stop a handover

Status: **Decided by the planner in the owner's absence** — selected from the options below; **put to the owner** beside his open question on
[Q-LIM-97](usage-and-limits.md#q-lim-97--a-pace-figure-keeps-a-download-in-quader-in-both-modes); the choice is built meanwhile.

Context: Q-LIM-97 (a) keeps a caller with `cachedBytesPerSecond` away from links, on the owner's condition for decision 11 ("für den
Aufrufer gilt kein Tempolimit"). Decision 12 names no such condition. What a Bastion serves from its cache is charged nowhere and was never
paced ([bastion.md](bastion.md) R-BAS-059); only its cold fills were paced, by the main installation's stream.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) No condition; the hour and day windows are checked by `Admit`, and the size is charged at issuance (chosen) | A handover for every caller the conditions allow | The figure protects the `packages` pods, which a handover's transfer does not pass; the Bastion's hits were never paced | A paced caller's cold fill is not paced |
| (b) As for links: a set figure keeps the download a stream | One rule for every link | Consistent with Q-LIM-97 (a) | Every built-in profile but Unlimited sets the figure: on a new installation a handover would reach almost no caller |

**Choice:** (a). **Applied in the spec:** R-STOR-095; [bastion.md](bastion.md) R-BAS-070.

### Q-STOR-344 · Every data key handed out is an audit event written before it leaves

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `bastion.data_key.handed`, transactional, in a transaction of its own before the answer, whatever `audit.record_downloads` says; a failed insert streams the download (chosen) | No key leaves without its record | The trail lists which keys each Bastion holds | One insert per handover of an encrypted copy |
| (b) A detail of `artifact.downloaded` | No new event | | Asynchronous (a crashed replica loses its buffer, [security/audit.md](../security/audit.md) H-122) and switched off with `audit.record_downloads` |
| (c) A metric only | | Cheap | Names no blob |

**Choice:** (a). **Applied in the spec:** R-STOR-095, R-STOR-096.

### Q-STOR-345 · A repository's opt-out of links does not reach a handover

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `storage.downloadLinks: false` keeps the clients' links away; a handover is the registration's switch alone (chosen) | The member keeps its meaning | Only formats whose clients check a digest take the member (R-STOR-014); a handover reaches every format; the administrator who registers a Bastion decides what it receives | A project Owner cannot keep the data keys of his repository's encrypted blobs out of the DMZ |
| (b) It stops handovers too | An Owner's say | | NuGet and Debian repositories cannot set the member (`not_applicable`); it would need another name and every format |

**Choice:** (a). The planner would revisit this when a project needs it: then a member of its own, on every format.
**Applied in the spec:** R-STOR-095.

### Q-STOR-346 · The link check tells whether a link works apart from whether it honours the overrides

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Two results: the probe's link answers `200` with its bytes, and it carries the three overridden headers (chosen) | A handover needs the first, a client's link both | A store that ignores an override still serves handovers | Two results to show |
| (b) One result | | | A store that ignores `response-content-disposition` would serve no handover for no reason |

**Choice:** (a). **Applied in the spec:** R-STOR-074, R-STOR-096.

### Q-STOR-347 · No rewrite of one blob under a new data key; the trail names every key that left

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: gap H-1311 asked the planner of the handover whether a rewrite of one blob is needed. The reconciliation's repair already writes a
copy anew with a new data key under the same name (R-STOR-087); on a versioned bucket the older version stays, and a backup's pool keeps the
object with its record ([backup-and-restore.md](backup-and-restore.md) R-BAK-031).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) None in this stage; the answer to a taken-over Bastion is its switch off, its token revoked and, where its holder may also read the bucket or a backup's target, a new connection that the reconciliation fills with new data keys (chosen) | The answer to a leaked storage key, applied to data keys | Nothing new to build; the trail of Q-STOR-344 names exactly the blobs concerned, so a later rewrite can take them | Until a refill, a kept data key opens its object wherever its ciphertext lies |
| (b) A command that rewrites the blobs a Bastion received, from the trail, through the reconciliation's repair | A rewrite per blob | Narrows the window to the current version | Noncurrent versions and backups' pools keep the old ciphertext under the old key, so it closes nothing where the holder can read them |

**Choice:** (a). **Applied in the spec:** R-STOR-096; [security/stored-content-keys.md](../security/stored-content-keys.md) H-1311.

### Q-STOR-400 · Only a whole retired connection is destroyed: every generation and the name key

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

Context: Q-STOR-309 (b) destroys a retired connection's keys. A connection has generations of its storage key and a name key. No job wraps
an object again: a rotation reseals rows, and Q-STOR-347 rules out rewriting one blob.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A whole retired connection: every generation and the name key in one run (chosen) | One unit, one state of the connection | Nothing still configured loses a key; the answer to a leaked key is a new connection anyway (R-STOR-085) | No shredding of one generation without replacing the connection |
| (b) Also a retired generation of a live connection once no copy row names it | Shredding per generation | Finer | `restore-versions` on the live connection would bring back objects under the destroyed generation, and blobs would be lost |
| (c) One key by identifier | The finest | Any key | The same loss as (b), and a name key alone destroyed leaves new writes unnameable |

**Choice:** (a). **Applied in the spec:** R-STOR-098.

### Q-STOR-401 · A connection's keys may be destroyed once it is retired or unrecorded and nothing holds it

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `retired_at` set or no row; configured by no live replica and not by the command; no copy, queue or session row (chosen) | The holders of R-STOR-098, read in the destroying transaction | A configured connection holds its keys in memory; a detached one may come back | An unrecorded connection is trusted on the replica registry alone |
| (b) Also drained but still configured | Earlier | The operator need not remove the element first | A replica still holds and may use the keys |
| (c) Also detached | Earlier still | — | Detached is reversible by design (Q-STOR-225): putting the element back must restore everything |

**Choice:** (a). The reconciliation sets `retired_at` for a drained connection that no live replica configures, and `forget-connection` for
one that is gone (R-STOR-051). **Applied in the spec:** R-STOR-098.

### Q-STOR-402 · A destroyed key keeps its row, loses its sealed value and names its time

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built. It touches the owner's
decision 15 (a) of 2026-11-22 (the storage keys lie once, as sealed rows, with an export command): the row still lies once, but it may lose
its sealed value.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The row stays with the state `destroyed`, `sealed_key` NULL and `destroyed_at` (chosen) | A mark that the key is gone | An identifier met later answers "destroyed", not "unknown"; the mark survives a restore (`storage_keys` is `data`, `storage_connections` `transient`) and keeps the id retired; the registry's counts skip NULL | Three columns' rules change; the row is no longer always sealed |
| (b) The row deleted | Nothing left | Simplest | An identifier met later is "unknown", whose advice is to import the export; an import could bring the key back; the id is free again after a restore |
| (c) The value replaced by random bytes or a marker | The row as it was | No NULL | A value that is no envelope enters the sealed-column registry: the backup's `secrets.keyIds` and the re-encrypt task break on it |

**Choice:** (a). In PostgreSQL an `UPDATE` and a `DELETE` leave the same remnants (Q-STOR-412). **Applied in the spec:** R-STOR-097.

### Q-STOR-403 · The backups are judged by the newest retention listing and the key identifiers each backup recorded

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The newest retention listing in `backup_run.result`, with the key identifiers each retained backup recorded in its manifest (chosen) | A read of the database | No credential of the target in the command and no pool read; the backup role already writes `backup_run` | After a restore of a backup the record is empty until the next run, so the command refuses until then (R-STOR-100) |
| (b) The command lists the target and reads the manifests | The target as the record | Always current | The command would need the target's credential |
| (c) The command reads the pool | Every object's record | Exact | Hours for a large pool, and the credential of (b) |

**Choice:** (a). **Applied in the spec:** R-STOR-100; backup-and-restore.md R-BAK-032.

### Q-STOR-404 · A dry run by default, and a confirmation bound to the installation and the keys

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A dry run by default; `--confirm` with a value the dry run prints, a hash over the installation and the identifiers not yet destroyed (chosen) | The confirmation binds to what was reviewed | No slip produces it; it does not match another installation or a newer generation; a Job can carry it | One more run of the command |
| (b) `--confirm <connection id>` | The id typed again | Simple | Typed from memory |
| (c) `--yes` | A flag | Simplest | Habitual |
| (d) A prompt | Interactive | Familiar | A Job cannot answer it |

**Choice:** (a). **Applied in the spec:** R-STOR-099.

### Q-STOR-405 · A holder's refusal exits 5

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `5` for a holder's refusal; `2` for usage, a connection without rows, the command's own configuration and a wrong confirmation (chosen) | "Not yet" apart from "wrong call" | A Job retries on `5` and alerts on `2` | Unlike `forget-connection`, which refuses with `2` |
| (b) `2` for everything | As `forget-connection` | One code | A Job cannot tell a state that changes from a mistake |

**Choice:** (a). **Applied in the spec:** R-STOR-099.

### Q-STOR-406 · A read that meets a destroyed key answers destroyed_key and marks the copy

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A reason of its own, `destroyed_key`: the copy is marked `corrupt` and repaired, no unknown-key alert (chosen) | A key that is gone is not missing | Q-STOR-311's reason not to mark, that the key can come back, does not hold; the repair takes the blob from another connection | A further reason in the metric |
| (b) `unknown_key` | As any key a replica lacks | No new reason | The alert would advise an import that the import refuses (R-STOR-103) |
| (c) `destroyed_key` that marks nothing | Counted only | No repair races anything | A copy that can never be read again stays a copy |

**Choice:** (a). **Applied in the spec:** R-STOR-077, R-STOR-082, R-STOR-087, R-STOR-088, R-STOR-101.

### Q-STOR-407 · Replicas forget destroyed keys at the change, and a load skips them

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The change carries the identifiers, and holders destroy the keys at once; a load skips destroyed rows (chosen) | Payload version 2 of the area `storage_keys` | Bounded by the change log (1 s by default, at once through Valkey) | A new payload version |
| (b) Lazily, at the next load | No payload change | Simple | A key can stay for a pod's life when nothing loads that connection again |
| (c) At the next restart | Nothing | Nothing to build | The same, longer |

**Choice:** (a). **Applied in the spec:** R-STOR-083, R-STOR-101.

### Q-STOR-408 · Every export is a row of storage_key_exports and names its keys

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A table `storage_key_exports` (export class `data`) and `keyIds` in `system.storage.keys_exported`; the destruction reports them (chosen) | What left, kept beside the keys | It outlives the audit trail's retention and lands in backups | A table for a report |
| (b) The audit trail only | No table | Nothing new | Gone after `audit.retention_days` |
| (c) Refuse while exports exist unless acknowledged | A gate | Forces a look | It cannot be met: escrow is invisible to Quader |
| (d) A counter on the key rows | No table | Small | Every export writes a new version of each row, a further copy of the sealed value |

**Choice:** (a). **Applied in the spec:** R-STOR-097, R-STOR-103.

### Q-STOR-409 · An import skips a destroyed key, reports it and inserts the destroyed rows the database lacks

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Skip the sealed row whose identifier the database holds destroyed, report it, and insert the destroyed rows the database lacks (chosen) | Nothing comes back | A recovery of other connections from an older export still works; the offline tool stays the way to read with the old export | An operator who regrets a destruction has no command to undo it |
| (b) Refuse the whole import | Strict | Nothing half done | An older export can no longer restore the other connections |
| (c) Insert it again | An undo | Reversible | The destruction would be one import away from void |

**Choice:** (a). **Applied in the spec:** R-STOR-103.

### Q-STOR-410 · The operator runs the destruction at a pod, without the key ring, and no API operation does

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `quader admin storage keys destroy` at a pod, with the database role `web`, no key ring (chosen) | Where `export`, `import` and `new-generation` run | The custody of the keys stays the operator's; Q-API-405 says no operation of the API changes a key | Only Kubernetes RBAC on `exec` gates it |
| (b) An operation of the API | In the admin area | Convenient | An API token or a stolen session would reach an irreversible step |
| (c) A Job of the chart | A value and a rollout | Declarative | A value left in a file destroys at the next rollout |

**Choice:** (a). A writer of the database could set `sealed_key` to NULL directly, so the command adds no power beyond the database role; its
refusals protect copies, sessions and retained backups. **Applied in the spec:** R-STOR-099.

### Q-STOR-411 · A connection id with destroyed keys is retired for good

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) An id of which `storage_keys` holds a destroyed row is refused at start, by `Ensure` and by the restore's drawing (chosen) | The mark is the record | Survives a restore, which empties `storage_connections` | One more query at start |
| (b) `retired_at` alone | As today | Nothing new | Lost in a restore of a backup |

**Choice:** (a). **Applied in the spec:** R-STOR-051, R-STOR-102.

### Q-STOR-412 · PostgreSQL's remnants are documented, not vacuumed by the command

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Documented: the database's operator vacuums and keeps the archive's retention (chosen) | Quader says what remains | Honest about what a database keeps | The old row version stays until vacuum and reuse |
| (b) The command runs `VACUUM storage_keys` | One statement more | Narrows the window of the old row version | It needs the table's ownership or `MAINTAIN`, which migration 00001 does not grant the runtime role; it erases no bytes of the write-ahead log, an archive or a standby |

**Choice:** (a). **Applied in the spec:** R-STOR-097; [security/stored-content-keys.md](../security/stored-content-keys.md) H-1372.

### Q-STOR-413 · The destruction is a package of its own, its part in the backup in package 4

Status: **Decided by the development agent** — selected from the options below; the owner sees it when it is built.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Package 7 for the keys, the command, the reads and the journal; the pool's part in package 4 (chosen) | Disjoint directories | Package 7 may land before 4, because the backups' check refuses what it cannot read (R-STOR-100) | Two packages to schedule |
| (b) Everything in package 4 | One package | One schedule | Package 4 already holds the jobs, the backup and the restore |
| (c) Everything in package 6 | With the documentation | — | Package 6 owns no code of the keys |

**Choice:** (a). **Applied in the spec:** [stored-content-encryption.md](../developer/stored-content-encryption.md).

### Q-STOR-460 · An import changes nothing of a connection whose keys the file and the database disagree on destroyed

Status: **Decided by the development agent** (package 7, at its build) — selected from the options below; the owner sees it when it is
built.

Context: Q-STOR-409 skips a sealed row whose identifier the database holds destroyed and inserts the destroyed rows the database lacks. The
build met two cases it does not name: a sealed row of the file whose connection the database holds destroyed but whose identifier it lacks
(an export taken after a generation the restored database never drew), and a destroyed row of the file for a connection whose keys the
database holds sealed (a database restored to before the destruction, or a file someone wrote: a destroyed row carries no sealed value, so
anybody can write one).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A connection the database holds destroyed gets no sealed row back; a destroyed row of the file is never applied to, or inserted beside, keys the database holds sealed, and is reported `stillSealed` (chosen) | The destroyed mark wins where the database has it, and an import never destroys | No key of a destroyed connection comes back, by identifier or by generation; a written file cannot retire a live connection's id or destroy its keys | A database restored to before a destruction keeps the keys until the journal's replay or the command destroys them |
| (b) Only the identifier decides, as Q-STOR-409 reads | Literal | Simplest | A key of a destroyed connection can come back under a new identifier; a destroyed row next to sealed ones makes the start refuse a live connection (R-STOR-102) |
| (c) The import also destroys the keys the file lists destroyed | The file as the record | One step | Any file, which needs no key ring for a destroyed row, would destroy a live connection's keys |

**Choice:** (a). The sealed values of a connection the database holds destroyed are not opened either, so an old export whose root key the
ring no longer has still restores the other connections. **Applied in the spec:** R-STOR-103.

### Q-STOR-461 · No backup known is a configuration without a backup target, and a run's target is compared by bucket and prefix

Status: **Decided by the development agent** (package 7, at its build) — selected from the options below; the owner sees it when it is
built.

Context: R-STOR-100's first case reads "`backup.enabled` is false in the command's configuration". `backup.enabled` is a value of the chart
(it renders the CronJob), not a key of Quader's configuration; the chart requires `config.backup.target.bucket` with it, and every backup
command needs the bucket. The runs record their target in two forms: a create `s3://<bucket>/<prefix> (<endpoint or aws>)`, a delete and a
prune `s3://<bucket>/<prefix>` without a slash at either end of the prefix.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) No backup known: `backup.target.bucket` is empty and no create or delete run is recorded; the newest listing's target and the configured one are compared as `s3://<bucket>/<prefix>` (chosen) | The configuration's own key, and the part both forms share | Reads what the command has; one rule for both forms | Two targets of the same bucket and prefix at different endpoints are taken for one |
| (b) A new key `backup.enabled` in the configuration | The specification's wording | Literal | A second switch beside the bucket that can disagree with it |
| (c) Compare the target strings as recorded | Exact | No parsing | A delete's listing never equals a create's configured target, so a prune would not satisfy the check |

**Choice:** (a). The endpoint stays out of the comparison because a delete does not record it; that two installations back up into the
same bucket and prefix at two endpoints is outside what the check sees, as H-1373 says of other targets. **Applied in the spec:** R-STOR-100.

### Q-STOR-462 · A destruction's change makes a replica forget every key of the connection

Status: **Decided by the development agent** (package 7, at its build) — selected from the options below; the owner sees it when it is
built.

Context: R-STOR-101 has a holder destroy the keys whose identifiers the change's payload names. A change's payload is bounded at 1 KiB less
its envelope ([changelog_notify.go](../../internal/cluster/changelog_notify.go)); a connection id of 32 characters makes an identifier of up
to 52, so the list fits only about fifteen keys, and a connection with more generations would not fit. A destruction is always of a whole
connection (Q-STOR-400).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A payload of version 2 with `destroyed` makes the holder destroy every key it holds of the connection; the command cuts the list from its end to fit, down to an empty list (chosen) | The list is a record, the connection decides | Any number of generations; the replica forgets everything at once | A change written by a database writer drops a live connection's keys from memory until the next use reads them again from their sealed rows |
| (b) Drop only the identifiers listed, refuse a destruction whose list does not fit | Literal | The payload says exactly what goes | A connection with many generations cannot be destroyed |
| (c) Several changes, one per part of the list | Every identifier announced | Literal | Several changes for one transaction, each a partial state |

**Choice:** (a). A holder never takes a key for destroyed from the payload alone: `destroyed_key` comes only from rows the holder read as
destroyed, so a forged change costs a re-read, never a copy marked corrupt. **Applied in the spec:** R-STOR-101.

### Q-STOR-350 · A gate passed on a decided state at its first decision; a download that waited streams

Status: **Decided by the development agent** (package 2 of the signed links) — selected from the options below; the owner reviews it.

Context: Q-STOR-284 hands a download to a link only when every gate that applies passed on a decided state. The gates decide together
before one wait (R-SIG-069); a pending blob under a strict malware gate, or a pending signature, waits, and the wait can end with the
verdict that lets the download through. A writer of the repository reads an unsigned artifact through the signature gate by his right
(Q-SIG-211), a decided answer of the gate that the owner kept with a trace of each such read (`signature.download_by_writer`, Q-SIG-240);
a vulnerability block in the mode `report` is recorded and served (R-VULN-015).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Decided is what the checks found at their first decision: the scanning off or the blob clean or released, no block (also none in `report`), the signature gate off or passed, by a verified signature or for a writer; a download that waited and a block in `report` stream (chosen) | `sgate.Serve.RunDecided` reports it from what the decision finds anyway, and records a writer's read there, once, whether the download then streams or goes by link | No second read of a verdict; a writer's read leaves the same trace by link as streamed | A download that waited for its verdict streams once more; it is the first after the upload, and the next one goes by link |
| (b) Read the verdicts again after a wait | | The download after the wait goes by link | A second statement on the path of a download (R-ARCH-027) for a case of seconds |
| (c) A writer's pass streams as well | | A link only for a verified signature | The writer's read the owner kept would differ by delivery for no gain: its trace is the same either way |

**Choice:** (a). The development agent first chose to stream a writer's pass and turned to (a) on the same day, when the trace of a
writer's read landed (Q-SIG-240): the trace is written by the decision, before the serve point streams or hands the download to a link.
**Applied in the spec:** R-STOR-070 (as built).

### Q-STOR-351 · The link check runs on every replica with a client listener, in the background, and again until it answers

Status: **Decided by the development agent** (package 2 of the signed links) — selected from the options below; the owner reviews it.

Context: R-STOR-074 lets "every replica serving downloads" run the link check at its start and when the switch is turned on, and the
settings' answer names per connection whether it can serve links on the replica that answers, which is an `api` replica. The check writes
one probe, signs one link and fetches it per connection; a store that is unreachable at that moment gives no answer.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every role with a client listener (`api`, `packages`, `web`, `all`) runs it, in the background of its start, again when its snapshot sees the switch turned on, and at the snapshot's safety period (a minute) while a connection's check has not answered; the worker none (chosen) | The area `download_links` of those roles | The answer of an `api` replica names its own check; a slow store never holds a start or a readiness; a store that was down at the start is asked again | One probe per connection and start on `api` replicas too |
| (b) Only the replicas that serve downloads | The specification's words | Fewer probes | An `api` replica would answer `link_check_failed` for a connection that serves links |
| (c) Before the first readiness | | No link before the check | A store that answers slowly holds every start |

**Choice:** (a). A check that cannot write its probe keeps what the connection answered before, so a short outage of the store does not
take a connection out of links. **Applied in the spec:** R-STOR-074 (as built), [architecture.md](architecture.md) R-ARCH-001's table.

### Q-STOR-352 · A connection that cannot serve links is logged while the switch is on

Status: **Decided by the development agent** (package 2 of the signed links) — selected from the options below; the owner reviews it.

Context: R-STOR-074 has a replica log a connection it finds unable once per change, with the reason and the host. Most installations do not
use links, and an encrypted connection, or a store reached over `http` inside the cluster, is unable for links by design.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Logged at `warn` once per change of the reasons, while the switch is on and once the connection's link check answered (chosen) | Switching on logs every unable connection once | The log speaks when an administrator wants links and a connection cannot give them | An installation with the switch off does not learn it from the log; the settings' answer and the gauge say it |
| (b) Always, once per change | | Seen before switching on | A warning at every start of every installation that has an encrypted connection or an `http` endpoint |

**Choice:** (a). **Applied in the spec:** R-STOR-074 (as built).

### Q-STOR-353 · The cases of package 2 write the switch through the store, and the end-to-end cases are named after their legs' sets

Status: **Decided by the development agent** (package 2 of the signed links) — selected from the options below; the owner reviews it.

Context: the integration case of R-STOR-014 and the end-to-end cases of R-STOR-074 switch the delivery on through `PATCH
/api/v1/download-links/settings`, which package 3 builds after package 2. The end-to-end sets select their tests by name prefix
(`^TestNPM(...)`, `^TestOCI`, `^TestAnsible`, [e2e.sh](../../deploy/dev/e2e.sh)); the names `TestR_STOR_071_<Format>ClientsCheckALink` match none.
The dev profile's malware scanning may have no verdict for a fresh blob within a test's minutes.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The integration case writes through `downloadlinks.Store` (what the operations will call) with its change area, and the end-to-end cases through the driver pod's `driver links on\|off` and `driver overwrite`, as the driver writes what M0 had no API for; the blob is marked `clean` by a statement; the cases are named `Test<Set>_R_STOR_071_ClientsCheckALink` (chosen) | Package 3 adds the operations' answers to the integration case | The cases run now and show the store, the snapshot and the clients; the requirement stays in the name | A statement stands in for the scanner in the end-to-end cases |
| (b) Wait for package 3 | | One path | The serve point would land without its clients' evidence, which R-STOR-071 asks for before it lands |
| (c) Keep the names and widen each set's pattern | | The specification's names | Three set patterns grow an exception each |

**Choice:** (a). **Applied in the spec:** R-STOR-014's and R-STOR-074's criteria.

### Q-STOR-354 · Yarn 4 installs a tarball whose object was altered, so npm's tarball route declares nothing

Status: **Decided by the development agent** (package 2 of the signed links) by R-STOR-071's rule of evidence, which turns a route's
declaration back when a supported client family takes altered bytes; the finding goes to the owner, who may choose (b).

Context: the npm leg's case of R-STOR-074 ran on 2026-11-22 on a kind cluster of the package's own, with the leg's client image (npm 12.2.1,
pnpm 12.16.1, Yarn 4.18.1, Yarn 1.22.22). Every family installed a fresh one-file package through a `307` to a link. After the tarball's
object was overwritten in the bucket with the same gzip stream whose header time was flipped (the same files, other bytes, another
SHA-512), npm 12, pnpm 12 and Yarn 1 refused it on the integrity, and Yarn 4 installed it. Its npm fetcher (`NpmSemverFetcher.fetchFromNetwork`
in `@yarnpkg/cli-dist` 4.18.1) converts the tarball to its zip archive without comparing it with `dist.integrity`; its checksum check
(`checksumBehavior`) compares with the checksum its lockfile holds, which a first resolve writes from the bytes it got. A project whose
lockfile was written from the stored bytes refuses on the checksum; a new project, a new dependency or an update installs what the link
serves. Streamed, Quader's own check (R-STOR-013) aborts such a download.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `npm.tarball.get` declares nothing, so every npm download streams through Quader's check (chosen) | The declaration and the table test change; the npm leg's case is removed, the integration and query-budget cases of the links use an OCI blob | The rule as written: no link where a supported client takes altered bytes | npm's tarballs offload nothing |
| (b) The serve point declines a link to a request whose `User-Agent` names Yarn 2 or later, and npm's route declares again | A step of R-STOR-070 that reads the client's header; the declaration becomes "every client but the ones recognized" | npm, pnpm and Yarn 1 get links | The header is the client's to set: a Yarn behind a wrapper or a proxy that rewrites it gets a link; a contract change (a declaration per client family); the owner's decision |
| (c) Keep the declaration and record the gap | | The offload | Against the rule: a Yarn 4 user installs what a tampered bucket holds |

**Choice:** (a). **Applied in the spec:** R-STOR-071's table, its as-built note and criteria, R-STOR-014's and R-STOR-074's criteria; code:
[routes.go](../../internal/formats/npm/routes.go), the table test in [all_test.go](../../internal/formats/all/all_test.go).

### Q-STOR-355 · A connection's publicAddress in the answer is the saved one, null for the endpoint

Status: **Decided by the development agent** (package 3 of the signed links) — selected from the options below; the owner reviews it.

Context: R-STOR-014's answer names per connection `endpoint` and `publicAddress` without saying whether `publicAddress` is the saved setting
or the origin the links name now. The settings page shows the saved address, else the endpoint, as the field's value (Q-UI-951), and its
merge patch sends `null` to return a connection to its endpoint; an address equal to the endpoint is not saved, so a later change of the
endpoint is followed.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `publicAddress` is the saved address, `null` when none is saved (chosen) | The row's value; the effective origin is `publicAddress ?? endpoint` | The page knows whether a `null` has to be sent; the answer says what a restore or another administrator would see in the row | A reader computes the effective origin |
| (b) The effective origin, the endpoint where none is saved | | What the links name, as is | A saved address equal to the endpoint and none saved look the same: the page cannot send the `null` that makes the endpoint followed again |
| (c) Both, as two members | | Nothing to compute | One more member for what `endpoint` already gives |

**Choice:** (a). The answer lists the connections the replica configures; an address the row holds for a connection the configuration no
longer has is not shown, so a write checks only the addresses it changes, and such an entry never blocks a save of the page.
**Applied in the spec:** R-STOR-014 (as built), the OpenAPI document's `DownloadLinkConnection`; code: `touched` in
[download_links_api.go](../../internal/api/v1/download_links_api.go).

### Q-STOR-356 · A test link of a connection that cannot take its probe is 409, and the test waits out the maintenance mode

Status: **Decided by the development agent** (package 3 of the signed links) — selected from the options below; the owner reviews it.

Context: R-STOR-074 answers `409 connection_unavailable` for a connection that is failing. A store can refuse the probe's write while the
circuit is still closed (a full bucket, a policy that forbids the prefix, a store that went away a second ago), and signing can fail. The
read-only maintenance mode (R-OPS-024) refuses every write but those the list of `x-quader-maintenance: allowed` names; the test writes an
object of the family `_probe/` and nothing in PostgreSQL.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every failure to write the probe or to sign its link is `409 connection_unavailable` with what the store said; the operation is not admitted in the maintenance mode (chosen) | One answer for "this connection cannot give a link now" | The administrator learns the store's refusal at once; a maintenance window is for storage work, and the test of an address is no part of keeping the installation usable, which is what the allowed list holds | A test during a maintenance window waits for its end |
| (b) A refused write is `503`, an open circuit `409` | | Kinds kept apart | Two answers for one fact; the page would explain both |
| (c) The test is admitted in the maintenance mode | | The address can be tested while the installation is read-only | A write to the store during a window that may be moving it, and a change of R-OPS-024's list for a convenience |

**Choice:** (a). **Applied in the spec:** R-STOR-074 (as built); code: [testlink.go](../../internal/downloadlinks/testlink.go), the problem
code `connection_unavailable` in [codes.go](../../internal/errs/codes.go).

### Q-STOR-370 · The storage summary has no member quota until the quota settings exist

Status: **Decided by the development agent** while building package F of the several connections; the owner reviews it.

Context: R-STOR-028 lists `quota` with the system quota settings of R-STOR-029 (`PUT /api/v1/settings/storage`), which are not built: no
setting, no enforcement, nothing to read.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The member is absent until R-STOR-029 is built (chosen) | The summary carries what exists | No figure is made up; R-STOR-029's package adds the member with its settings | A client of the summary finds no quota yet |
| (b) A constant `{"enabled": false, …}` | The member as specified | The document's shape is complete | It says a quota is configurable and off, which it is not |

**Choice:** (a). **Applied in the spec:** R-STOR-028, [operations-and-observability.md](operations-and-observability.md) R-OPS-023; the
API document's `StorageSummary` has no `quota`.

### Q-STOR-371 · The summary counts the blobs only one connection holds

Status: **Decided by the development agent** while building package F; the owner reviews it.

Context: [api-and-ui.md](api-and-ui.md) R-UI-196 shows a detached connection with "*n* blobs are only here", and R-STOR-028's figures
have no such count. Counting it per request would read every copy of the connection.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `soleCopies` per connection in the summary: the live blobs whose only present copy lies there (chosen) | Counted in the summary's one pass, one probe of the copies' primary key per present copy | The tab reads it with the other figures; what a removal costs is known for every connection, not only a detached one | Up to an hour old, like every figure of the summary |
| (b) Counted by the connections' list at the request | Current | Exact | A scan of a connection's copies on every refresh of the tab |

**Choice:** (a). **Applied in the spec:** R-STOR-028, R-STOR-061; code: `StorageSummaryConnections` in
[storage_summary.sql](../../internal/meta/postgres/queries/storage_summary.sql).

### Q-STOR-372 · The connections' list derives every state the same for every replica and counts over the copies of live blobs

Status: **Decided by the development agent** while building package F; the owner reviews it.

Context: R-STOR-051's table names the states and when each holds, not which wins where two could (a connection configured with drain on
some live replicas and without on others during a rollout, a drained connection still in a configuration), nor the state of a connection
that no live replica configures and that holds no copy. R-STOR-028 says the figures are "counted from the indexes"; a backlog of live blobs
minus present copies takes two counts, and [architecture.md](architecture.md) R-ARCH-019 runs transactions at `READ COMMITTED`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) From the row and the registry's live replicas, in this order: `retired` once `retired_at` is set; `filling` or `active` while one live replica configures it without drain (by `filled_at`); `drained` once `drained_at` is set; `detached` while no live replica configures it, with or without copies; else `draining`. The figures at `READ COMMITTED` (R-ARCH-019 keeps `REPEATABLE READ` for the backup and the grant index), the copies joined to live blobs counted first, the live blobs as the folded counter plus its unfolded deltas after them, backlog and fill bounded to their range (chosen) | One derivation in SQL and Go | Every replica answers the same state; a rollout never shows a connection that still takes uploads as draining; an upload between the statements shows as a gap of the moment, never as a negative backlog | A connection in no configuration and without copies shows as detached until `forget-connection` retires it |
| (b) The answering replica's configuration decides | Local | Simple | Two refreshes that reach two replicas of a rollout show two states |
| (c) The figures in one `REPEATABLE READ` snapshot | One moment | Exact to the row | A third exception to R-ARCH-019 for a figure that is an hour old anyway |

**Choice:** (a). **Applied in the spec:** R-STOR-028, R-STOR-061; code: `Row.State` in
[connections.go](../../internal/blob/summary/connections.go), `Compute` in [summary.go](../../internal/blob/summary/summary.go).

### Q-STOR-380 · A claim reserves one part, and the records count what was sealed

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-081 had the claim transaction add "the number of segments the request may seal (`⌈bytes / 65,536⌉ + 2`)". The claim runs
before the body is read, and a request's body has no length the session service is told (a chunked body has none), so `bytes` is not known
there. A request may send many parts; a part it sealed and sent is recorded by the same request unless the request loses its claim or dies
between the upload and the record.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The claim reserves `⌈part_size / 65,536⌉ + 2` (what a request may seal and never record: one part, its final segment, one more); the record of each part, of each tail object and of the last part adds the segments it sealed (chosen) | Every segment sealed under the session's data key is counted, a part twice at most | A checked bound that does not depend on a length the service does not know; one addition in statements the request runs anyway; a session of small requests loses 258 segments of the bound per request with parts of 16 MiB, 8.3 million requests | The counter runs ahead of what was sealed by the reserve of every request |
| (b) The claim reserves the most the request may send (the effective upload limit) | No count at the records | Literally "the request may seal" | With the 10 GiB limit a reserve of 163,842 segments per request: a session ends after about 13,000 requests, which a client of small chunks reaches |
| (c) Count at the records only | Exact | No reserve | A part sealed and sent and then not recorded (a lost claim, a crash) is never counted |

**Choice:** (a). The tail segments (under `K_tail`, a key of its own) are counted in the same column, which errs on the safe side. A final
segment sent alone by a continuation of a finalize that holds no claim (Q-STOR-381) is one segment that is not counted.
**Applied in the spec:** R-STOR-081; code: `UploadClaim`, `UploadRecordPart`, `UploadRecordEnd`, `UploadClearTail` of
[uploads.sql](../../internal/meta/postgres/queries/uploads.sql), [sealed.go](../../internal/uploads/sealed.go).

### Q-STOR-381 · A final segment sent alone is not recorded as a part

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: a session whose blob ends at a part boundary has no tail at its finalize; R-STOR-081 sends the final segment alone as the last
part. `upload_session_parts.size` is the part's plain size with `CHECK (size > 0)` (migration 00054), and that part holds no plain byte.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The final segment is sent and the upload completed with it under the finalize's short claim, without a row; whether the parts already end with the final segment is read from the rows (every part but the last holds `part_size`) (chosen) | One continuation sends it; one cut before the completion sends it again under its own seed | No migration; the parts' rows keep their meaning; a repeated finalize is idempotent | The part's ETag lives only in the continuation that sent it |
| (b) A migration that allows `size = 0` and records the part | A row like every part | Uniform | A migration for one part of 68 bytes, numbered after migrations of other packages in flight |
| (c) Hold back a full last part as a tail until more bytes come | Never a part without data | No part of its own | Changes the cutting of every encrypted session; a tail of a whole part breaks the rule that a tail is less than one part |

**Choice:** (a). **Applied in the spec:** R-STOR-081; code: `completeWithFinal` and `finalIn` of
[internal/uploads](../../internal/uploads/finalize.go).

### Q-STOR-382 · The client of an encrypted connection refuses a blob's object without a record

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: package 3 makes `encrypted: true` accepted; the jobs that write blobs without the store's writer (the reconciliation's stream, the
scrub's repair, R-STOR-087) learn the stored format with package 4. Between the two, such a job would put a blob's plaintext into an
encrypted bucket, where nothing reads it and its provider reads everything.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The client every caller gets from an encrypted connection (`Connection.Objects`) refuses a `PutObject`, a `CreateMultipartUpload` or a copy into a blob's name (below `sha256/` or `keyed/`) whose metadata is not a record, and every copy from another bucket there (chosen) | One guard, before the request is sent | No path, built or to come, writes a plain blob into an encrypted connection by mistake; the staging area, the marker, the probes and the journal pass | A job that is refused retries after its back-off and reads its source again |
| (b) Teach each job now | No guard | Each job works | Package 4's whole scope inside package 3 |
| (c) Nothing until package 4 | — | — | The plaintext lands in the bucket |

**Choice:** (a). The guard checks that a record is there, not that it is genuine: the store's writers make records, and the reader refuses
any that does not open. **Applied in the spec:** R-STOR-075, R-STOR-080; code: [sealedonly.go](../../internal/blob/sealedonly.go).

### Q-STOR-383 · A plain connection refuses an object that carries a record

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: a plain connection's reader is the identity (R-STOR-062), and a ranged read serves what the bucket holds (H-11). An object of the
stored format at a plain connection's name (a copy of an encrypted bucket under a plain element, a write of another installation) would be
served as the blob's bytes: ciphertext to a client, and to a ranged read without a check at all.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The reader of a plain connection refuses an object whose metadata names the format (`quader-format`) as `format`: the copy is marked `corrupt`, the read falls over (chosen) | One look at metadata the GET returns anyway | No ciphertext reaches a reader of a plain connection, ranged or whole | — |
| (b) Leave it to the digest check | As stage 1 | Nothing to build | A ranged read and a read with `storage.download.verify: false` serve the ciphertext |

**Choice:** (a). **Applied in the spec:** R-STOR-082; code: `readPlain` of [sealedread.go](../../internal/blob/sealedread.go).

### Q-STOR-384 · Until the jobs read the stored format, each passes over or refuses an encrypted connection

Status: **Decided by the builder** — selected from the options below; the owner reviews it. *Ended on 2026-11-23 by package 4: every job reads
and writes the stored format (R-STOR-087, backup-and-restore.md R-BAK-031); the filtered view of the scrub and the two refusals of the backup
commands are gone, and so are the gaps the decision recorded.*

Context: R-STOR-087 and R-BAK-031 are package 4's. After package 3 the stage-1 jobs meet encrypted copies: read through the connection's
reader they get plaintext (the scrub would compare its length with the stored size and mark every copy `corrupt`); the backup copies objects
as they lie and checks them against the digest; the restore writes the pool's objects as they lie into the first connection without drain.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each job does what is safe now (chosen): collection works (it deletes by the copy row's name); the reconciliation reads an encrypted source through its reader and writes a plain target, and a write into an encrypted target is refused by Q-STOR-382 and retried after its back-off; the scrub passes over every encrypted connection and says so at its start; the orphan sweep finds nothing below `sha256/` of a connection with keyed names and reports `keyed/` as a foreign key, never deleting there; the operator check compares the stored size; a backup run refuses to start while a connection is encrypted; a restore into an encrypted first connection refuses to start | Nothing marks, deletes or writes wrongly | No job damages an encrypted copy or puts plaintext into its bucket, and what is missing says so | A second encrypted connection is not filled; no backup of an installation with an encrypted connection |
| (b) Refuse `encrypted: true` until package 4 | Package 3 accepts nothing | Nothing half-working | Package 3's own build cannot run on an installation, and the packages' order puts package 4 after it |
| (c) Let each job run as stage 1 | — | — | The scrub marks every encrypted copy corrupt; the restore writes plaintext |

**Choice:** (a). With `storage.replication.min_copies` equal to the number of connections a commit writes every encrypted connection
itself, so a second one is filled without the reconciliation. **Applied in the spec:** R-STOR-087, R-STOR-080, until package 4 closed the two
gaps it recorded (an encrypted copy neither scrubbed, repaired nor backed up; the sweep adopting an unplaced encrypted object as corrupt).

### Q-STOR-385 · An encrypted read holds back its last segment

Status: **Decided by the builder** — selected from the options below; the owner reviews it.

Context: R-STOR-082 held back "the last `storage.download.hold_back` bytes" until the final segment and the digest passed. The reader of the
format (`blobcrypt`, package 1) releases a segment only after its tag and holds the plaintext of the last data segment until the final
segment and, where it hashes, the SHA-256 have passed.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The reader's hold-back: the last data segment, 1 to 65,536 bytes (chosen) | One mechanism | Every byte released is authenticated, and a client never receives a whole object that failed, which is what R-STOR-013 asks; nothing to configure | Fewer bytes held than `hold_back` when the last segment is short |
| (b) A second hold-back of `hold_back` bytes in front of the reader | As a plain read | The same number as a plain read | A buffer that protects nothing more: no byte it holds is unauthenticated |

**Choice:** (a). **Applied in the spec:** R-STOR-082; code: `sealedDownload` of [sealedread.go](../../internal/blob/sealedread.go).

### Q-STOR-430 · The label bastion joins the allowed labels of R-OPS-007

Status: **Decided by the development agent** during the build of R-STOR-095 (package 2 of the handover) — selected from the options
below; the owner sees it when it is built.

Context: R-STOR-095 names `quader_storage_download_handovers_total{bastion, kind}`. R-OPS-007 permits only the labels of its table, and
the registry of `internal/observability` refuses any other when a family is registered. The main installation's
`quader_bastion_interface_requests_total` and `quader_bastion_forwarded_requests_total` carry the label `bastion` already, registered
beside that check on the same registry (bastion.md R-BAS-008), with the registration's name as its value, bounded by
`bastions.max_registrations`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `bastion` joins R-OPS-007's table: a Bastion registration's name, bounded by `bastions.max_registrations` (chosen) | One rule for the families of both sides of the main installation | The operator sees which Bastion receives handovers; the registry's check holds the new family | A principal's name as a label value, which the forbidden labels name for users and service accounts; a registration's name is an operator's choice in a list of at most 256 |
| (b) The family without the label | No per-Bastion count | Nothing to change | The handovers of a partner's Bastion and the DMZ's cannot be told apart |
| (c) Registered beside the check, as `internal/bastionmain` does | The requirement as written | | The rule is bypassed in silence a second time |

**Choice:** (a). **Applied in the spec:** [operations-and-observability.md](operations-and-observability.md) R-OPS-007's table; code:
`allowedLabels` in [labels.go](../../internal/observability/labels.go).

### Q-STOR-440 · A stream into an encrypted connection holds one part in memory, not the spool

Status: **Decided by the builder** (package 4) — selected from the options below; the owner reviews it.

Context: R-STOR-087 had the reconciliation read a source "into its spool" and write the spool to an encrypted connection, and R-STOR-080
named the streams of the reconciliation and of a restore among the writes "from a spool". The scrub's repair writes into an encrypted
connection as well and holds no spool; the restore Job has none configured. The writer encrypts a section of plaintext as it is read, twice
where a payload checksum is taken, and the SDK's retry seeks back in it (Q-STOR-301): what it needs is the part, not the whole blob.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One writer for every stream into an encrypted connection: the plaintext read part by part into memory (`multipart.part_size`, raised to fit 10,000 parts), each part sealed under the write's data key as it is sent, the SHA-256 and the length checked before the last part and the completion (chosen) | `Connection.WriteSealedStream` | The same memory as a streamed copy between plain connections (Q-STOR-255); no disk and no spool budget for a job; one function for the reconciliation, the scrub's repair and the restore; a stream that is not the blob completes nothing | A retry of the whole copy reads the source again |
| (b) Spool the source, then the store's writer | As specified | A retry of a part reads the spool, not the source | The scrub and the restore Job would need a spool of their own; a blob as large as the spool's budget blocks every other stream of the worker |

**Choice:** (a). **Applied in the spec:** R-STOR-080, R-STOR-087, backup-and-restore.md R-BAK-031; code:
[sealedstream.go](../../internal/blob/sealedstream.go).

### Q-STOR-441 · The orphan sweep places every object without a copy row by its record, also below sha256/

Status: **Decided by the builder** (package 4) — selected from the options below; the owner reviews it.

Context: R-STOR-087 read steps 1 to 3 of the record of an object below `keyed/` that has no copy row and applied stage 1's rules by the
digest; below `sha256/` of an encrypted connection without keyed names it opened the record only for an adoption. Q-STOR-314's rule is that
the sweep never deletes an object without a readable record. Steps 4 and 5 compare with a copy row the object does not have.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) For every object without a copy row on an encrypted connection: one `HeadObject`, steps 1 to 3 and 6 (`blobcrypt.OpenRecord`), the name checked against the wrap's digest; adopted `corrupt` when the stored size is not `S(L)`; reported as foreign when the wrap binds the live blob's digest with another size; an orphan claimed only once placed (chosen) | Q-STOR-314 below `sha256/` as below `keyed/` | No object of an encrypted connection is deleted unless its record opens and names it; a genuine record on a cut body is adopted and repaired as stage 1 adopts a short object | One `HeadObject` per orphan below `sha256/` that stage 1 deleted by its name |
| (b) Steps 1 to 6 with the head's size as the object's (`blobcrypt.Open` without a copy row) | No new function in `blobcrypt` | One call | A cut body yields no digest, so it can be neither adopted nor named |

**Choice:** (a). **Applied in the spec:** R-STOR-087; code: [sweep_sealed.go](../../internal/blob/sweep_sealed.go), `OpenRecord` of
[record.go](../../internal/blob/blobcrypt/record.go).

### Q-STOR-442 · The operator check opens a record's steps 1 to 6 and reports unknown_key as a finding record

Status: **Decided by the builder** (package 4) — selected from the options below; the owner reviews it.

Context: R-STOR-087 asked the operator check for a finding `record` when steps 1 to 5 fail. The check holds the copy row and the blob row,
so `blobcrypt.Open` runs steps 1 to 6 at the cost of one comparison more. An unknown key is no damage (Q-STOR-311), but the copy cannot be read
now, which is what the operator check reports.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Steps 1 to 6; every refusal is a finding `record` with its reason (`unknown_key` among them); a blob none of whose copies is there is lost of kind `record`; the key ring is loaded only when a connection is encrypted (chosen) | One unwrap per copy, from the `HeadObject` it sends anyway | The operator sees the reason, and an unreadable blob as lost, whatever the cause; a plain installation's check needs no key ring | A blob whose key only awaits the import is reported lost until then |
| (b) Leave `unknown_key` out of the findings | As the scrub | No alarm before the import | A blob nobody can read now passes the check that exists to find exactly that |

**Choice:** (a). **Applied in the spec:** R-STOR-087; code: `openRecord` of [verify.go](../../internal/blob/verify/verify.go), `recordKeys`
of [admin_storage.go](../../cmd/quader/admin_storage.go).

### Q-STOR-450 · A pull writes a directory of its own: the objects, their record files and the connection's facts

Status: **Decided by the builder** (package 6) — selected from the options below; the owner reviews it.

Context: R-STOR-089 says that `pull` copies the objects with each record as `<object name>.record.json` and that `list`, `get` and `verify`
read such a directory with `--dir`. Those need the connection's id (its name key), its prefix and whether it is encrypted with keyed names,
which the element names and a directory of objects does not.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each object at `<dir>/<object name>`, its metadata as `<object name>.record.json`, the element's facts in `<dir>/quader-pull.json` (format `quader-offline-pull-v1`); `--to` empty or new, every file created new (0600, directories 0700), an object whose name is not of the layout skipped and counted (chosen) | A pull is one self-describing directory | `--dir` reads it as `--connection-file` reads the bucket; no name of the bucket leads out of the directory; nothing of another pull is mixed in | A pull that stops cannot be resumed: it starts again in a new directory |
| (b) No facts file: the prefix and the connection taken from the names and the records | Nothing beside the objects | One file less | A plain object and a stripped encrypted one look alike; the name key's connection is a guess |
| (c) Resume into a directory that holds a pull | Skip what is there | Long pulls survive a stop | A file of another pull, or one changed since, could be taken for the object |

**Choice:** (a). **Applied in the spec:** R-STOR-089; code: [pull.go](../../internal/blob/offline/pull.go), `Dir` of
[source.go](../../internal/blob/offline/source.go).

### Q-STOR-451 · Get writes a temporary file beside --out and links it only after the digest passed

Status: **Decided by the builder** (package 6) — selected from the options below; the owner reviews it.

Context: R-STOR-089: "a failure writes nothing and exits `5`". The reader of `blobcrypt` hands out only authenticated segments and holds the
last back until the final segment and the SHA-256 passed, but everything before the last segment has left by then.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `--out` must not exist; the plaintext goes to `.<out>.partial-<random>` beside it (mode 0600), which becomes `--out` by a hard link, which never replaces a file, only after the reader's end; any refusal removes it (chosen) | The file the operator named appears whole or not at all | A script never finds a partial `--out`; no file is overwritten, also one created meanwhile | A process killed before its end leaves the temporary file ([H-1451](../security/stored-content-keys.md#h-1451--a-get-that-is-killed-leaves-the-plaintext-it-had-written-in-a-temporary-file)); a file system without hard links refuses |
| (b) Write `--out` directly and remove it on a refusal | One file | Simple | A partial file under the operator's name while the read runs, and after a kill |
| (c) Hold the plaintext in memory | Nothing on disk until it passed | No partial file | A blob of gigabytes |
| (d) Standard output | A pipe | Composable | Bytes leave before the digest is checked and cannot be called back |

**Choice:** (a). **Applied in the spec:** R-STOR-089; code: `Get` of [read.go](../../internal/blob/offline/read.go).

### Q-STOR-452 · The offline tool opens every key of the export before it reads, and looks a record up across the export

Status: **Decided by the builder** (package 6) — selected from the options below; the owner reviews it.

Context: an export may hold the keys of several connections (R-STOR-085). A connection's objects name its own storage keys; a backup's pool
names the keys of the connections each object came from (R-BAK-032). Without the copy rows, the name stands in for the database: a keyed
name is the name key's MAC of the digest (R-STOR-086).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Open every sealed key of the export at the start, refusing a ring that lacks a root key of the export by name; look a record's key up among all of them; check a keyed name under the name key of the connection the record's key names (chosen) | One export reads a connection and a pool alike | A wrong ring is refused before the first object; a pool reads; a connection whose element names another id still checks its names | Every key of the export lies in the machine's memory for the run ([H-1450](../security/stored-content-keys.md#h-1450--the-offline-tool-opens-every-key-of-the-export-on-the-operators-machine)); a ring without an old root key refuses an export whose other rows it could open |
| (b) Open only the keys of the element's connection, lazily | The fewest keys in memory | Smaller exposure | A pool cannot be read; a wrong ring shows only at the first object; an element without the right id reads nothing |
| (c) The keys of the record's connection only for a connection's objects, all for a pool | Two rules | Closer to the store's lookup per connection | Two behaviours to explain for one bucket's names |

**Choice:** (a). An export narrowed with `--connection` holds only that connection's keys. **Applied in the spec:** R-STOR-089; code:
`LoadKeys`, `lookup` and `checkName` of [internal/blob/offline](../../internal/blob/offline/keys.go).

### Q-STOR-470 · CopyDataKey opens the copy the request's lookup read, and reads no row

Status: **Decided by the builder** (package 4 of the handover) — selected from the options below; the owner reviews it.

Context: R-STOR-095 says that step 8 is one `HeadObject` and that a handover of an encrypted copy costs, besides the item lookup, the
trace's insert (step 10). `blob.Store.CopyDataKey(ctx, digest, connectionID)`, built with the keys (Q-STOR-320), read the blob row and the
copies again in a transaction of its own before its `HeadObject`: two statements and a transaction more per handover of an encrypted copy.
The item lookup of the download has read the copies and the blob's size in the same request (Q-STOR-275), and a download's reader opens an
encrypted copy with exactly those (`Connection.ReadBlob`).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `CopyDataKey(ctx, digest, size, copy)`: the copy row and the size the caller read, one `HeadObject`, steps 1 to 6 of R-STOR-077 against them; no statement (chosen) | The handover decides on the request's rows, as the stream does | Two statements per handover of an encrypted copy, the lookup and the trace's insert, as R-STOR-095 counts; the key is that object's own, bound by its record to the digest and the sizes | A copy marked damaged between the lookup and the handover is still named; the Bastion's read of it fails at its first segment and falls back, and the main installation's read marks it (R-BAS-075) |
| (b) As built with the keys: the rows read again in a transaction of its own | A fresher copy row | A copy marked a moment before is not named | Four statements and two transactions per handover of an encrypted copy; the stream and the handover decide on different rows of one request |

**Choice:** (a). The name stays, so the architecture rule of its callers (Q-STOR-327) holds unchanged. **Applied in the spec:** R-STOR-083,
R-STOR-095; [architecture.md](architecture.md) R-ARCH-027's budget line; code: [datakey.go](../../internal/blob/datakey.go),
`dataKey` of [handover.go](../../internal/downloadlinks/handover.go).

### Q-STOR-471 · The link is signed before the data key is taken

Status: **Decided by the builder** (package 4 of the handover) — selected from the options below; the owner reviews it.

Context: R-STOR-095 numbers the data key (step 8) before the credential and the signature (step 9). Both must hold, so the order changes
only which reason a download that fails both counts, how long the key lives, and whether a download that streams for its signature took a
key and sent a `HeadObject`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Step 9, then step 8 (chosen) | The key is taken last, right before the value is encoded | No key is taken and no `HeadObject` sent for a download that streams for its credential or its signature; the key lives the shortest time | A copy whose record does not open and whose credential expires counts `credential`, not `key` |
| (b) The numbered order | As written | The reasons in the order of the list | A key taken and destroyed again for every download whose link cannot be signed |

**Choice:** (a). **Applied in the spec:** R-STOR-095 step 8; code: `handover` of [handover.go](../../internal/downloadlinks/handover.go).

### Q-STOR-472 · The value is encoded before the trace, and the key destroyed before the insert

Status: **Decided by the builder** (package 4 of the handover) — selected from the options below; the owner reviews it.

Context: R-STOR-095 puts the trace (step 10) before the issue (step 11), and R-STOR-096 wants the trace committed before the answer leaves and
the key destroyed once its bytes are in the answer. Between them lie the encoding of the value, which can refuse it (a link above 8 KiB,
R-BAS-073), and the insert, a round trip to the database.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Encode, destroy the key, write the trace, note the value for the marking (chosen) | The trace is the last step before the answer | The trail names no key whose value was never sent; the key is destroyed before the database round trip; no key without its trace | The encoded value, a string, holds the key in base64url until the answer is written and the collector reclaims it ([security/bastion-handover.md](../security/bastion-handover.md) H-1490) |
| (b) Trace, then encode | The numbered order read literally | | A refused encoding leaves a trace of a key that never left; the key lives across the insert |

**Choice:** (a). **Applied in the spec:** R-STOR-095 step 10; code: `HandoverOptions` of [handover.go](../../internal/downloadlinks/handover.go).

## What this does not cover

### H-11 · Object-store principals bypass Quader's authorization and integrity checks

Anyone who can read the bucket prefix reads every blob of every repository; anyone who can write
it can replace content. Quader detects replacement only on full streamed downloads (R-STOR-013)
and in the scrub (R-STOR-027): ranged downloads and presigned redirects deliver whatever the
bucket holds, and between two scrub passes altered content can be served this way. There is no
client-side encryption and no object signature. Live today for every installation; the exposure
is the set of principals with access to the bucket, which Quader cannot see. Mitigations are
operational: least-privilege bucket policy, no other principals on the prefix, S3 Object Lock or
versioning to make replacement detectable and reversible. An encrypted connection (R-STOR-075, built) narrows it for
that connection: its readers see ciphertext under keyed names, and a replaced
object fails authentication on every read, ranged ones included (R-STOR-082); what stays is on the pages
[stored-content-format.md](../security/stored-content-format.md) and
[stored-content-visibility.md](../security/stored-content-visibility.md), and the gap stays whole on every
plain connection.

### H-12 · Presigned download URLs are bearer capabilities

A signed link (R-STOR-014, R-STOR-072) grants the object to anyone holding it until it expires (`linkLifeSeconds`, 60 s unless the
administrator sets up to an hour) or the credential that signed it expires or is revoked. It cannot be revoked alone, is not bound to
the client, can be fetched several times and passed on, and may be logged by proxies, browsers or CI logs. Revoking the requester's
token does not invalidate links already issued. Dormant: not built, and off by default once built; live on an installation whose
administrator switched it on, for every download R-STOR-070 hands to a link, whoever the caller (an anonymous caller of a public
repository included). What makes it real is a link that leaves the client: a CI log that prints redirects, a proxy that logs query
strings. The lever that ends every outstanding link at once is the revocation of the connection's credential, which stops Quader's own
access too (Q-STOR-293).

### H-13 · Deduplication reveals the existence of known content through upload timing

A principal with `deploy` on any repository can upload a large file it already knows and observe
whether the S3 write was skipped (deduplication hit, R-STOR-005): the upload finishes measurably
faster when the same content is stored anywhere, including repositories the principal cannot
read. The attacker learns only that content it already possesses exists in the installation;
relevant where repositories separate tenants. Live today for every upload path. Deploy by
checksum does not leak this (unreadable content answers `404`).

### H-14 · No per-principal limit on concurrent uploads or spool use

Component: the spool admission of R-STOR-004 on every replica that takes uploads (`packages`, and the development roles `web` and `all`). Subject: every principal that
uploads. The spool budget is per replica and shared by all principals. A single token with
`deploy` can hold many slow concurrent uploads and exhaust the budget, making every other upload
on that replica fail with `503` until the requests end or are cut. The Service spreads the
attacker's connections over all `web` replicas (INV-12), so the same token can exhaust every
replica's budget; adding replicas raises the number of connections needed, not the protection.
The cluster-scope limiter of [architecture.md](architecture.md) R-ARCH-034 counts requests per
second, not bodies in flight, so it does not bound this. Live in the MVP for every principal with
`deploy` on any repository; it becomes real where upload clients are not all trusted. Mitigation
outside Quader: per-client connection limits and request-body timeouts at the edge proxy
([ha-and-helm.md](ha-and-helm.md) R-HA-039). A per-principal limit on bodies in flight would have
to count across replicas and release the slots of a crashed replica; it is not specified on this
page.

### H-552 · A deduplication hit across projects can be told apart by its timing

A blob is stored once for the whole installation ([Q-STOR-154](#q-stor-154--blobs-are-deduplicated-across-projects)). When an
upload's content is already stored, the commit protocol (R-STOR-005) writes nothing to S3 and the answer can come sooner than
for new content. A member of one project who uploads a file it already has can therefore learn whether some other repository,
in any project, holds the same bytes. Live wherever content is uploaded; what makes it matter is a private file whose
presence elsewhere is itself confidential, not a public package. Accepted by the owner on 2026-11-06: the caller needs the exact
bytes already, and padding a hit to the time of a write would only narrow the signal. Revisit when an operator has to keep
confidential files of projects apart; closing it then means answering a hit no sooner than a write of the same size would take.
