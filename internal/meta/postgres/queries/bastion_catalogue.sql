-- Queries of the public catalogue of the Bastion registrations (bastion.md R-BAS-101 to R-BAS-108; W-35's package P6, the tables of
-- 00146_bastion_catalogue). The builder (internal/bastionmain/catalogue) writes them in the worker's job bastion-catalogue, one run at a
-- time in the cluster; the catalogue routes of the Bastion interface read them. Identifiers travel as text and are cast here.

-- name: BastionCatalogueRegistrations :many
-- Every registration with its name and whether it is enabled: the builder builds for those whose web interface is on.
SELECT p.id::text AS id, p.name, (p.status = 'active')::boolean AS enabled
  FROM principal p JOIN bastion b ON b.principal_id = p.id
 WHERE p.kind = 'bastion'
 ORDER BY p.name;

-- name: BastionCatalogueState :many
-- The builder's state of a registration; no row before its first build and after its catalogue was deleted.
SELECT floor, seq, salt, base_url, stopped, public, built_at, checked_at
  FROM bastion_catalogue_state WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid;

-- name: BastionCatalogueStateInit :exec
-- The state of a registration's first build. floor is above every number a catalogue of the registration handed out before (the
-- caller takes the clock in microseconds), so that a cursor of an earlier catalogue answers reset.
INSERT INTO bastion_catalogue_state (bastion_id, floor, seq)
VALUES (sqlc.arg(bastion_id)::text::uuid, sqlc.arg(floor)::bigint, sqlc.arg(floor)::bigint - 1)
ON CONFLICT (bastion_id) DO NOTHING;

-- name: BastionCatalogueReserve :one
-- Reserves n numbers of the change list and answers the last of them; the caller numbers its rows from seq - n + 1 in the same
-- transaction, so the numbers of a run are above every number committed before it.
UPDATE bastion_catalogue_state SET seq = seq + sqlc.arg(n)::bigint
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid
RETURNING seq;

-- name: BastionCatalogueStateSet :exec
-- What a run leaves: the salt and the base URL it rendered with, why it stopped (empty when it did not), the public set, and its
-- times. built_at moves only with a run that changed a row.
UPDATE bastion_catalogue_state
   SET salt = sqlc.arg(salt)::bytea, base_url = sqlc.arg(base_url)::text, stopped = sqlc.arg(stopped)::text,
       public = sqlc.arg(public)::jsonb,
       built_at = CASE WHEN sqlc.arg(changed)::boolean THEN now() ELSE built_at END, checked_at = now()
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid;

-- name: BastionCatalogueStop :exec
-- Records that the builder stopped for a registration (bastion_base_url_unknown), creating the state when there is none.
INSERT INTO bastion_catalogue_state (bastion_id, floor, seq, stopped, checked_at)
VALUES (sqlc.arg(bastion_id)::text::uuid, sqlc.arg(floor)::bigint, sqlc.arg(floor)::bigint - 1, sqlc.arg(stopped)::text, now())
ON CONFLICT (bastion_id) DO UPDATE SET stopped = EXCLUDED.stopped, checked_at = now();

-- name: BastionCatalogueDrop :exec
-- Deletes a registration's catalogue whole: its rows, its units and its state (the web interface switched off, R-BAS-102). The
-- documents no other registration names go with the next sweep.
WITH r AS (DELETE FROM bastion_catalogue WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid),
     u AS (DELETE FROM bastion_catalogue_unit WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid)
DELETE FROM bastion_catalogue_state WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid;

-- name: BastionCatalogueHasState :many
-- The registrations that hold a catalogue or a state, so that the builder drops the catalogue of one whose web interface is off.
SELECT bastion_id::text AS bastion_id FROM bastion_catalogue_state;

-- name: BastionCatalogueUnits :many
-- The units of a registration with their fingerprints and the signature objects they name.
SELECT unit, fingerprint, refs FROM bastion_catalogue_unit WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid;

-- name: BastionCatalogueUnitSet :exec
INSERT INTO bastion_catalogue_unit (bastion_id, unit, fingerprint, refs)
VALUES (sqlc.arg(bastion_id)::text::uuid, sqlc.arg(unit)::text, sqlc.arg(fingerprint)::bytea, sqlc.arg(refs)::text[])
ON CONFLICT (bastion_id, unit) DO UPDATE SET fingerprint = EXCLUDED.fingerprint, refs = EXCLUDED.refs;

-- name: BastionCatalogueUnitDelete :exec
DELETE FROM bastion_catalogue_unit WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid AND unit = sqlc.arg(unit)::text;

-- name: BastionCatalogueUnitRows :many
-- The rows of a unit, deleted ones included, with the SHA-256 of their documents.
SELECT path, sha256, seq FROM bastion_catalogue
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid AND unit = sqlc.arg(unit)::text;

-- name: BastionCatalogueLiveUnits :many
-- The units that hold a live row: a unit the build no longer has is deleted whole.
SELECT DISTINCT unit FROM bastion_catalogue
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid AND sha256 IS NOT NULL;

-- name: BastionCatalogueDocumentPut :exec
-- Keeps a document's compressed bytes once for every row that names them.
INSERT INTO bastion_catalogue_document (sha256, size, gzip)
VALUES (sqlc.arg(sha256)::bytea, sqlc.arg(size)::integer, sqlc.arg(gzip)::bytea)
ON CONFLICT (sha256) DO NOTHING;

-- name: BastionCatalogueUpsert :exec
-- A path's new document (or its deletion, with a NULL sha256, whose size is then NULL too) under the change's number.
INSERT INTO bastion_catalogue (bastion_id, path, unit, sha256, size, seq, changed_at)
VALUES (sqlc.arg(bastion_id)::text::uuid, sqlc.arg(path)::text, sqlc.arg(unit)::text, sqlc.narg(sha256)::bytea,
        CASE WHEN sqlc.narg(sha256)::bytea IS NULL THEN NULL ELSE sqlc.arg(size)::integer END, sqlc.arg(seq)::bigint, now())
ON CONFLICT (bastion_id, path) DO UPDATE
   SET unit = EXCLUDED.unit, sha256 = EXCLUDED.sha256, size = EXCLUDED.size, seq = EXCLUDED.seq, changed_at = EXCLUDED.changed_at;

-- name: BastionCatalogueSweep :execrows
-- Deletes the documents no row names, the end of a builder's run. The builder runs one at a time (the job's key), so no
-- transaction names a document this statement removes.
DELETE FROM bastion_catalogue_document d
 WHERE NOT EXISTS (SELECT 1 FROM bastion_catalogue c WHERE c.sha256 = d.sha256);

-- name: BastionCatalogueChanges :many
-- The change list after the cursor, in the order of the numbers, at most max_rows; live_only leaves the deleted paths out (a list
-- read from the start).
SELECT path, sha256, COALESCE(size, 0)::integer AS size, seq FROM bastion_catalogue
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid AND seq > sqlc.arg(after)::bigint
   AND (NOT sqlc.arg(live_only)::boolean OR sha256 IS NOT NULL)
 ORDER BY seq
 LIMIT sqlc.arg(max_rows)::int;

-- name: BastionCatalogueDocuments :many
-- The live documents of the named paths with their compressed bytes, for the single and the batch read.
SELECT c.path, c.sha256, d.size, d.gzip
  FROM bastion_catalogue c JOIN bastion_catalogue_document d ON d.sha256 = c.sha256
 WHERE c.bastion_id = sqlc.arg(bastion_id)::text::uuid AND c.path = ANY(sqlc.arg(paths)::text[]);

-- name: BastionCataloguePackageFacts :many
-- The fingerprint of every package by its format and name, over every repository that holds it (R-BAS-105): what changes a document
-- of the package and is not a time. The versions and their trash, the package's revision (a change of its metadata), the findings
-- with their status and what the page shows of them, the files' verdicts and releases, the Debian contents' state, the signatures with
-- their trust, the Ansible documentation built for the versions (its generator, its entries and its bytes, so a rebuild that changes
-- the search words changes it too) and the state of a role's documentation, the image scans' outcome.
-- No time of a check (scanned_at, checked_at, synced_at, computed_at) enters it: a rescan that finds the same changes no document.
SELECT r.format, p.name,
       md5(string_agg(
         p.id::text || '/' || p.rev_seq::text || '/' ||
         COALESCE((SELECT string_agg(pv.id::text || CASE WHEN pv.trashed_at IS NULL THEN '' ELSE 't' END, ',' ORDER BY pv.id)
                     FROM package_versions pv WHERE pv.package_id = p.id), '') || '/' ||
         COALESCE((SELECT string_agg(f.advisory_id || ' ' || f.version || ' ' || f.status || ' ' || f.severity || ' ' || f.known_exploited::text
                                     || ' ' || array_to_string(f.fixed_versions, ' ') || ' ' || COALESCE(f.cvss_score::text, ''), ',' ORDER BY f.id)
                     FROM vuln_finding f WHERE f.package_id = p.id), '') || '/' ||
         COALESCE((SELECT string_agg(encode(i.blob_sha256, 'hex') || ' ' || COALESCE(ms.state, '') || ' ' || COALESCE(ms.signature, '') || ' '
                                     || (mr.sha256 IS NOT NULL)::text || ' ' || COALESCE(dc.state, ''), ',' ORDER BY i.id)
                     FROM package_versions pv
                     JOIN items i ON i.version_id = pv.id AND i.trashed_at IS NULL AND i.kind = 'file'
                     LEFT JOIN malware_scan ms ON ms.sha256 = i.blob_sha256
                     LEFT JOIN malware_release mr ON mr.sha256 = i.blob_sha256
                     LEFT JOIN deb_contents dc ON dc.item_id = i.id
                    WHERE pv.package_id = p.id), '') || '/' ||
         COALESCE((SELECT string_agg(s.id::text || ' ' || s.state || ' ' || COALESCE(st.state, ''), ',' ORDER BY s.id, st.state)
                     FROM package_versions pv
                     JOIN items i ON i.version_id = pv.id AND i.trashed_at IS NULL AND i.kind = 'file'
                     JOIN signatures s ON s.subject_sha256 = i.blob_sha256
                     LEFT JOIN signature_trust st ON st.signature_id = s.id
                    WHERE pv.package_id = p.id), '') || '/' ||
         COALESCE((SELECT string_agg(vd.version || ' ' || vd.generator::text || ' ' || vd.entry_count::text || ' ' || vd.total_bytes::text,
                                     ',' ORDER BY vd.version)
                     FROM ansible_version_docs vd WHERE vd.repo_id = p.repo_id AND vd.name = p.name), '') || '/' ||
         COALESCE((SELECT string_agg(af.version || ' ' || af.state, ',' ORDER BY af.version)
                     FROM ansible_doc_fetches af WHERE af.repo_id = p.repo_id AND af.name = p.name), '') || '/' ||
         COALESCE((SELECT string_agg(encode(vs.digest, 'hex') || ' ' || vs.state || ' ' || vs.os_eosl::text || ' ' || vs.packages::text, ','
                                     ORDER BY vs.digest)
                     FROM vuln_image_scan vs WHERE vs.repo_id = p.repo_id AND vs.name = p.name), ''),
         '|' ORDER BY p.id)) AS fingerprint
  FROM packages p JOIN repositories r ON r.id = p.repo_id
 GROUP BY r.format, p.name;

-- name: BastionCatalogueRepositoryFacts :many
-- The fingerprint of every repository of a format whose items are paths (generic) by its qualified name: its live files with their
-- blobs, verdicts and releases.
SELECT (pr.key || '/' || r.key)::text AS repository,
       md5(COALESCE(string_agg(i.path || ' ' || encode(i.blob_sha256, 'hex') || ' ' || COALESCE(ms.state, '') || ' '
                               || (mr.sha256 IS NOT NULL)::text, ',' ORDER BY i.path), '')) AS fingerprint
  FROM repositories r
  JOIN project pr ON pr.id = r.project_id
  LEFT JOIN items i ON i.repo_id = r.id AND i.trashed_at IS NULL AND i.kind = 'file'
  LEFT JOIN malware_scan ms ON ms.sha256 = i.blob_sha256
  LEFT JOIN malware_release mr ON mr.sha256 = i.blob_sha256
 WHERE r.format = ANY(sqlc.arg(formats)::text[])
 GROUP BY pr.key, r.key;

-- name: BastionCatalogueDocSearchWords :many
-- The words of the search vector of every entry of an Ansible collection version's documentation in a repository (formats/ansible.md
-- R-ANS-036): the lexemes the main installation's search matches a term's prefix against, which the builder hands to the Bastion's
-- catalogue as the tree's searchWords, so that a version's documentation is searched in the browser by the main installation's rule
-- (bastion.md R-BAS-124). One statement per version over its rows (at most 5,000), by the primary key.
SELECT e.content_type, e.content_name, tsvector_to_array(e.search)::text[] AS words
  FROM ansible_doc_entries e
  JOIN repositories r ON r.id = e.repo_id
  JOIN project pr ON pr.id = r.project_id
 WHERE pr.key = sqlc.arg(project)::text AND r.key = sqlc.arg(repository)::text
   AND e.name = sqlc.arg(name)::text AND e.version = sqlc.arg(version)::text;
