-- Statements of the scanning of container images (R-VULN-033 to R-VULN-037, internal/vuln/imagescan). Plain Go types only: a digest is
-- bytea, a time travels as microseconds since the epoch.

-- name: VulnImageDiscover :execrows
-- The image manifests of active local and remote repositories of the format that have no scan row yet: a push, a fill, an image stored
-- before the scanning was switched on and a restored installation all arrive here. An index is not scanned, its children are; an artifact
-- that is no container image (a chart, a signature) is left out by its media types. A signature, an attestation or an SBOM by R-SIG-023 is
-- left out as well (signatures.md R-SIG-024, Q-VULN-269), because cosign's older layout and Docker's attestation manifests carry an image
-- configuration: a manifest the format recorded as an attachment by its content (oci_manifests.attachment: no image layer, a layer or a type
-- of R-SIG-023, such as the in-toto layer of Docker's attestation manifest), a referrer by its subject, and a manifest that is the object of
-- a signature in the repository. VulnImageManifests and VulnImageGradeScans count the children of an index by the same selection, and
-- VulnImageVerdict lets what it leaves out pass the missing-data rule of a pull.
INSERT INTO vuln_image_scan (repo_id, name, digest, not_before)
SELECT m.repo_id, m.name, m.digest, now() + make_interval(secs => sqlc.arg(delay_seconds)::float8)
  FROM oci_manifests m
  JOIN repositories r ON r.id = m.repo_id
 WHERE r.format = 'oci' AND r.class IN ('local', 'remote') AND r.lifecycle = 'active'
   AND m.media_type IN ('application/vnd.oci.image.manifest.v1+json', 'application/vnd.docker.distribution.manifest.v2+json')
   AND COALESCE(m.artifact_type, '') IN ('', 'application/vnd.oci.image.config.v1+json', 'application/vnd.docker.container.image.v1+json')
   AND m.subject IS NULL AND NOT m.attachment
   AND NOT EXISTS (SELECT 1 FROM vuln_image_scan s WHERE s.repo_id = m.repo_id AND s.name = m.name AND s.digest = m.digest)
   AND NOT EXISTS (SELECT 1 FROM signatures g JOIN signature_attachments a ON a.signature_id = g.id
                    WHERE g.object_sha256 = m.digest AND a.repo_id = m.repo_id)
 LIMIT sqlc.arg(max_rows)::int
ON CONFLICT DO NOTHING;

-- name: VulnImageDropAttachments :execrows
-- The scan rows of manifests that are no image to scan by VulnImageDiscover's selection (Q-VULN-269): an attachment, a referrer, the object
-- of a signature. A row of such a manifest was queued before the selection left it out (Docker's attestation manifests, whose in-toto layer
-- failed layer_format); it goes at the next run, and the findings it had resolve as those of a manifest that is gone.
DELETE FROM vuln_image_scan s
 USING oci_manifests m
 WHERE m.repo_id = s.repo_id AND m.name = s.name AND m.digest = s.digest
   AND (m.attachment OR m.subject IS NOT NULL
        OR EXISTS (SELECT 1 FROM signatures g JOIN signature_attachments a ON a.signature_id = g.id
                    WHERE g.object_sha256 = m.digest AND a.repo_id = m.repo_id));

-- name: VulnImageDue :many
-- The next manifests to work on: never scanned or to retry and due, or scanned against another database than the current one. The rows
-- of a repository that is disabled or being deleted are not due (R-VULN-042): the scanner's pull could not address it. The qualified
-- name of the repository is what the scan token and the call to the scanner name (R-VULN-041). The rows without a detection (pending, or
-- failed and due) come before the re-detections of scanned rows under a new database, each class by not_before (Q-VULN-200): a pushed
-- image waits for the slots, not for the cycle, and a scanned image keeps its findings while it waits. A terminal failure is due when
-- what it was judged under is none of the judges the job names now (Q-VULN-270): the bounds or the release changed.
SELECT s.repo_id::bigint AS repo_id, s.name::text AS name, s.digest AS digest, s.attempts::int AS attempts,
       p.key::text AS project_key, r.key::text AS repo_key
  FROM vuln_image_scan s
  JOIN repositories r ON r.id = s.repo_id
  JOIN project p ON p.id = r.project_id
 WHERE r.lifecycle = 'active' AND NOT COALESCE((r.config->>'disabled')::boolean, false)
   AND ((s.state = 'pending' AND s.not_before <= now())
     OR (s.state = 'failed' AND s.judged IS NULL AND s.not_before <= now() AND s.attempts < sqlc.arg(max_attempts)::int)
     OR (s.state = 'failed' AND s.judged IS NOT NULL AND NOT (s.judged = ANY(sqlc.arg(judges)::text[])))
     OR (s.state = 'scanned' AND s.db_digest IS DISTINCT FROM sqlc.arg(db_digest)::text))
 ORDER BY (s.state = 'scanned'), s.not_before, s.repo_id, s.name, s.digest
 LIMIT sqlc.arg(max_rows)::int;

-- name: VulnImageNextNew :one
-- The seconds until the first manifest that was discovered and still waits out its scan delay becomes due, or -1 when none waits
-- (Q-VULN-268): a run that finds nothing due runs again at that time instead of at the task's next slot. A row that was put off keeps
-- its code and is left to the schedule, so an outage of the scanners does not wake a run per deferral.
SELECT COALESCE(EXTRACT(EPOCH FROM min(s.not_before) - now()), -1)::float8 AS wait_seconds
  FROM vuln_image_scan s
 WHERE s.state = 'pending' AND s.error_code IS NULL AND s.not_before > now();

-- name: VulnImageDueCount :one
SELECT count(*)::bigint
  FROM vuln_image_scan s
  JOIN repositories r ON r.id = s.repo_id
 WHERE r.lifecycle = 'active' AND NOT COALESCE((r.config->>'disabled')::boolean, false)
   AND (s.state IN ('pending', 'failed') OR (s.state = 'scanned' AND s.db_digest IS DISTINCT FROM sqlc.arg(db_digest)::text));

-- name: VulnImageUnanalysed :many
-- The due manifests that have no analysis yet (Q-VULN-168): without a database the job has the scanner analyse them and leaves the rows
-- pending, so the first database detects without reading a layer. A row whose analysis is stored is not taken again, whatever version
-- made it (Q-VULN-199: the detection's scanner says whether it is current), so the run ends. A terminal failure is due as in VulnImageDue.
SELECT s.repo_id::bigint AS repo_id, s.name::text AS name, s.digest AS digest, s.attempts::int AS attempts,
       p.key::text AS project_key, r.key::text AS repo_key
  FROM vuln_image_scan s
  JOIN repositories r ON r.id = s.repo_id
  JOIN project p ON p.id = r.project_id
 WHERE r.lifecycle = 'active' AND NOT COALESCE((r.config->>'disabled')::boolean, false)
   AND ((s.state = 'pending' AND s.not_before <= now())
     OR (s.state = 'failed' AND s.judged IS NULL AND s.not_before <= now() AND s.attempts < sqlc.arg(max_attempts)::int)
     OR (s.state = 'failed' AND s.judged IS NOT NULL AND NOT (s.judged = ANY(sqlc.arg(judges)::text[]))))
   AND NOT EXISTS (SELECT 1 FROM vuln_image_sbom b WHERE b.digest = s.digest)
 ORDER BY s.not_before, s.repo_id, s.name, s.digest
 LIMIT sqlc.arg(max_rows)::int;

-- name: VulnImageScanned :exec
-- The record of a finished detection.
UPDATE vuln_image_scan
   SET state = 'scanned', db_digest = sqlc.arg(db_digest)::text, scanned_at = now(), attempts = 0, error_code = NULL, error_detail = NULL, judged = NULL,
       os_family = NULLIF(sqlc.arg(os_family)::text, ''), os_name = NULLIF(sqlc.arg(os_name)::text, ''), os_eosl = sqlc.arg(os_eosl)::boolean,
       packages = sqlc.arg(packages)::int
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND digest = sqlc.arg(digest);

-- name: VulnImageFailed :exec
-- A scan that did not finish: the state says so, so that nobody reads the absence of findings as clean. A row that had a detection keeps
-- it. A failure that may pass is due again after the back-off (judged empty); a terminal one names what it was judged under and is due
-- again when that changes (Q-VULN-270).
UPDATE vuln_image_scan
   SET state = 'failed', attempts = attempts + 1, not_before = now() + make_interval(secs => sqlc.arg(backoff_seconds)::float8),
       error_code = sqlc.arg(error_code)::text, error_detail = left(sqlc.arg(error_detail)::text, 1024),
       judged = NULLIF(sqlc.arg(judged)::text, '')
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND digest = sqlc.arg(digest);

-- name: VulnImageDeferred :exec
-- A scan that did not start because the spool of the worker had no room (Q-VULN-145): the row stays pending with the reason, is due again
-- after the wait, and its attempts are not spent, because nothing about the image failed. A row that had a detection is pending again, as after a failure, and keeps its findings until the next detection.
UPDATE vuln_image_scan
   SET state = 'pending', not_before = now() + make_interval(secs => sqlc.arg(wait_seconds)::float8),
       error_code = sqlc.arg(error_code)::text, error_detail = left(sqlc.arg(error_detail)::text, 1024), judged = NULL
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND digest = sqlc.arg(digest);

-- name: VulnImageRearm :execrows
-- A rescan: every manifest of the repository is due again, and an analysis is not repeated (the document of the analysis stays).
UPDATE vuln_image_scan
   SET state = 'pending', not_before = now(), attempts = 0, judged = NULL
 WHERE repo_id = sqlc.arg(repo_id)::bigint;

-- name: VulnImageOrphanFindings :many
-- The findings of images whose manifest is gone from the repository (the scan row went with it): they resolve with version_removed.
SELECT f.id::text AS id
  FROM vuln_finding f
 WHERE f.ecosystem = 'OCI' AND f.status IN ('needs_triage', 'confirmed', 'blocked', 'dismissed')
   AND NOT EXISTS (SELECT 1 FROM vuln_image_scan s
                    WHERE s.repo_id = f.repo_id AND s.name = f.package_name AND encode(s.digest, 'hex') = substr(f.version, 8))
 ORDER BY f.id
 LIMIT sqlc.arg(max_rows)::int;

-- name: VulnImageSBOMGet :many
-- The analysis of a manifest digest (R-VULN-043, Q-VULN-199): one per digest, the gzip as the scanner answered it, the analyser version
-- that made it and the image's packages.
SELECT sbom, analyser::text AS analyser, packages::int AS packages FROM vuln_image_sbom WHERE digest = sqlc.arg(digest);

-- name: VulnImageSBOMPut :exec
-- A new analysis of the digest replaces the one before it, whatever version made that one (Q-VULN-199).
INSERT INTO vuln_image_sbom (digest, analyser, sbom, packages)
VALUES (sqlc.arg(digest), sqlc.arg(analyser)::text, sqlc.arg(sbom), sqlc.arg(packages)::int)
ON CONFLICT (digest) DO UPDATE SET analyser = EXCLUDED.analyser, sbom = EXCLUDED.sbom, packages = EXCLUDED.packages, created_at = now();

-- name: VulnImageSBOMDelete :exec
-- A detection the analyser refused points at the analysis (Q-VULN-202): it goes, and the next attempt analyses the image again.
DELETE FROM vuln_image_sbom WHERE digest = sqlc.arg(digest);

-- name: VulnImageSBOMPrune :execrows
-- The analyses no manifest refers to any more. Nothing is pruned by version: a scanner of another version asks for a new analysis.
DELETE FROM vuln_image_sbom b
 WHERE NOT EXISTS (SELECT 1 FROM vuln_image_scan s WHERE s.digest = b.digest);

-- name: VulnImageDBGet :many
-- The database in use (R-VULN-044, Q-VULN-173): the row of the source trivy, ecosystem OCI, once a run recorded a layer. etag is the
-- layer's digest "sha256:<hex>" (archive_sha256 holds the same bytes); the layer itself is an item of the database's repository. The
-- watermark is the build time of its metadata.json, which the run records with it (Q-VULN-208); a row recorded before has none.
SELECT s.etag::text AS digest, COALESCE((EXTRACT(EPOCH FROM s.watermark) * 1000000)::bigint, 0)::bigint AS built_us
  FROM vuln_source_state s
 WHERE s.source = 'trivy' AND s.ecosystem = 'OCI' AND s.state = 'complete' AND s.etag LIKE 'sha256:%';

-- name: VulnImageDatabaseStale :many
-- The files of the database's image in its repository that the retention of Q-VULN-175 drops (Q-VULN-194): the layers and the manifests
-- below <image>/_blobs/ and <image>/_manifests/ stored before the cut-off, other than the paths the run keeps (the manifest of the tag it
-- read) and other than content that is the database in use, in path order after a cursor.
SELECT i.path::text AS path
  FROM items i
 WHERE i.repo_id = sqlc.arg(repo_id)::bigint AND i.kind = 'file' AND i.trashed_at IS NULL
   AND (left(i.path, length(sqlc.arg(blobs)::text)) = sqlc.arg(blobs)::text
        OR left(i.path, length(sqlc.arg(manifests)::text)) = sqlc.arg(manifests)::text)
   AND i.created_at < sqlc.arg(before)::text::timestamptz
   AND NOT (i.path = ANY(sqlc.arg(keep)::text[]))
   AND NOT vuln_image_database_in_use(i.blob_sha256)
   AND i.path > sqlc.arg(after_path)::text
 ORDER BY i.path
 LIMIT sqlc.arg(max_rows)::int;

-- name: VulnImageDatabaseInUse :one
-- The exemption of the remote-cache cleanup (storage.md R-STOR-045, Q-VULN-175): whether a stored file's content is the image
-- scanning's database in use. The cleanup's statement applies it through vuln_image_database_in_use (migration 00089).
SELECT vuln_image_database_in_use(sqlc.arg(sha256)::bytea)::boolean AS in_use;

-- name: VulnImageUpsertAdvisory :exec
-- The advisory of an image finding, as the analyser reported it: the findings API, the detail and the filters read the same table as
-- for the package formats. No affected rows: nothing matches it by name.
INSERT INTO vuln_advisory (ecosystem, id, canonical_id, modified, published, kind, summary, details, severity, severity_source, cvss_version, cvss_vector,
                           cvss_score, "references", cwe_ids, record_sha256)
VALUES ('OCI', sqlc.arg(id)::text, sqlc.arg(id)::text, to_timestamp(sqlc.arg(modified_us)::bigint / 1000000.0),
        CASE WHEN sqlc.arg(published_us)::bigint = 0 THEN NULL ELSE to_timestamp(sqlc.arg(published_us)::bigint / 1000000.0) END,
        'vulnerability', sqlc.arg(summary)::text, sqlc.arg(details)::text, sqlc.arg(severity)::text, sqlc.arg(severity_source)::text,
        NULLIF(sqlc.arg(cvss_version)::text, ''), NULLIF(sqlc.arg(cvss_vector)::text, ''), NULLIF(sqlc.arg(cvss_score)::float8, -1)::numeric(3,1),
        sqlc.arg(refs)::jsonb, sqlc.arg(cwe_ids)::text[], sqlc.arg(record_sha256))
ON CONFLICT (ecosystem, id) DO UPDATE
   SET modified = EXCLUDED.modified, published = EXCLUDED.published, summary = EXCLUDED.summary, details = EXCLUDED.details,
       severity = EXCLUDED.severity, severity_source = EXCLUDED.severity_source, cvss_version = EXCLUDED.cvss_version,
       cvss_vector = EXCLUDED.cvss_vector, cvss_score = EXCLUDED.cvss_score, "references" = EXCLUDED."references", cwe_ids = EXCLUDED.cwe_ids,
       record_sha256 = EXCLUDED.record_sha256
 WHERE vuln_advisory.record_sha256 <> EXCLUDED.record_sha256;

-- name: VulnImageKnownExploited :many
-- The identifiers among the advisories of a detection that the known-exploited catalog lists (R-VULN-008).
SELECT cve_id::text AS cve_id FROM vuln_kev WHERE cve_id = ANY(sqlc.arg(ids)::text[]);

-- name: VulnImageFindings :many
-- The findings of one manifest, resolved ones too, in the shape of the evaluation's reads.
SELECT f.id::text AS id, f.repo_id::bigint AS repo_id, f.package_name::text AS package_name, f.version::text AS version,
       f.advisory_id::text AS advisory_id, COALESCE(f.package_id, 0)::bigint AS package_id, COALESCE(f.version_id, 0)::bigint AS version_id,
       f.kind::text AS kind, f.severity::text AS severity, COALESCE(f.cvss_score, -1)::float8 AS cvss_score, f.known_exploited::boolean AS known_exploited,
       f.fix_available::boolean AS fix_available, f.fixed_versions::text[] AS fixed_versions, f.status::text AS status,
       COALESCE(f.status_before_resolution, '')::text AS status_before_resolution, COALESCE(f.resolved_reason, '')::text AS resolved_reason,
       (extract(epoch FROM f.advisory_modified) * 1000000)::bigint AS advisory_modified_us, f.revision::bigint AS revision
  FROM vuln_finding f
 WHERE f.repo_id = sqlc.arg(repo_id)::bigint AND f.ecosystem = 'OCI' AND f.package_name = sqlc.arg(name)::text AND f.version = sqlc.arg(version)::text;

-- name: VulnImagePackageID :many
SELECT p.id::bigint AS id FROM packages p WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = sqlc.arg(name)::text;

-- name: VulnImageSetComponents :exec
UPDATE vuln_finding SET components = sqlc.arg(components)::jsonb
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND ecosystem = 'OCI' AND package_name = sqlc.arg(name)::text AND version = sqlc.arg(version)::text
   AND advisory_id = sqlc.arg(advisory_id)::text AND components <> sqlc.arg(components)::jsonb;

-- name: VulnImageVerdict :one
-- What a pull of a manifest needs to know (R-VULN-015): the finding that blocks the manifest in the repository, if any, how far the
-- manifest has been scanned, and whether it is an image to scan at all. image is VulnImageDiscover's selection of the manifest's row: an
-- index, an attachment (Docker's attestation manifest), a referrer, a signature and an artifact that is no container image are no image
-- and are never scanned, so the missing-data rule does not hold them (Q-VULN-271); the platform image a client fetches next is gated on its
-- own. A manifest the repository holds no row of (a remote's miss by digest, before its fetch) counts as an image: what it is is not known
-- yet. One statement, the finding's unique index and the keys of the scan row and of the manifest row.
SELECT COALESCE(blk.id::text, '')::text AS finding_id, COALESCE(blk.advisory_id, '')::text AS advisory_id,
       COALESCE(blk.kind, '')::text AS kind, COALESCE(blk.status, '')::text AS status, COALESCE(s.state, '')::text AS scan_state,
       (s.db_digest IS NOT NULL)::boolean AS detected,
       (m.digest IS NULL
        OR (m.media_type IN ('application/vnd.oci.image.manifest.v1+json', 'application/vnd.docker.distribution.manifest.v2+json')
            AND COALESCE(m.artifact_type, '') IN ('', 'application/vnd.oci.image.config.v1+json', 'application/vnd.docker.container.image.v1+json')
            AND m.subject IS NULL AND NOT m.attachment
            AND NOT EXISTS (SELECT 1 FROM signatures g JOIN signature_attachments a ON a.signature_id = g.id
                             WHERE g.object_sha256 = m.digest AND a.repo_id = m.repo_id)))::boolean AS image
  FROM (SELECT 1) AS one
  LEFT JOIN vuln_image_scan s ON s.repo_id = sqlc.arg(repo_id)::bigint AND s.name = sqlc.arg(name)::text AND s.digest = sqlc.arg(digest)
  LEFT JOIN oci_manifests m ON m.repo_id = sqlc.arg(repo_id)::bigint AND m.name = sqlc.arg(name)::text AND m.digest = sqlc.arg(digest)
  LEFT JOIN LATERAL (
        SELECT f.id, f.advisory_id, f.kind, f.status
          FROM vuln_finding f
         WHERE f.repo_id = sqlc.arg(repo_id)::bigint AND f.ecosystem = 'OCI' AND f.package_name = sqlc.arg(name)::text
           AND f.version = sqlc.arg(version)::text
           AND f.status IN ('needs_triage', 'confirmed', 'blocked')
           AND (f.status = 'blocked' OR (f.kind = 'malicious' AND sqlc.arg(block_malicious)::boolean))
         ORDER BY (f.kind = 'malicious' AND sqlc.arg(block_malicious)::boolean) DESC, f.severity_rank DESC, f.advisory_id
         LIMIT 1) AS blk ON true;

-- name: VulnImageManifests :many
-- The manifests a tag stands for in the repositories, with how far each is scanned: the version views of the report (R-VULN-020). The tag
-- item's blob is the manifest it points at (R-OCI-007); the tag of a remote repository is its cache entry (R-OCI-036). For an index these
-- are its platform images (Q-VULN-269): the children the repository stores that VulnImageDiscover's selection scans. A child that is an
-- attachment (Docker's attestation manifest, platform unknown/unknown) is no image of the tag, and a child a remote has not fetched is not
-- in the repository and has nothing to check. An index has no scan row of its own and stands alone only while no platform image is stored.
WITH tagged AS (
    SELECT p.repo_id, p.name, i.blob_sha256 AS digest, ''::text AS platform
      FROM packages p
      JOIN package_versions pv ON pv.package_id = p.id
      JOIN items i ON i.id = pv.lead_item_id
     WHERE p.repo_id = ANY(sqlc.arg(repos)::bigint[]) AND p.name = sqlc.arg(name)::text AND pv.version = sqlc.arg(tag)::text AND pv.trashed_at IS NULL
    UNION ALL
    SELECT t.repo_id, t.name, t.manifest_sha256, ''::text
      FROM oci_remote_tags t
     WHERE t.repo_id = ANY(sqlc.arg(repos)::bigint[]) AND t.name = sqlc.arg(name)::text AND t.version = sqlc.arg(tag)::text
), images AS (
    SELECT tagged.*, false AS child FROM tagged
    UNION
    SELECT t.repo_id, t.name, r.target, COALESCE(r.platform, ''), true
      FROM tagged t
      JOIN oci_references r ON r.repo_id = t.repo_id AND r.name = t.name AND r.manifest = t.digest AND r.kind = 'manifest'
      JOIN oci_manifests c ON c.repo_id = r.repo_id AND c.name = r.name AND c.digest = r.target
     WHERE c.media_type IN ('application/vnd.oci.image.manifest.v1+json', 'application/vnd.docker.distribution.manifest.v2+json')
       AND COALESCE(c.artifact_type, '') IN ('', 'application/vnd.oci.image.config.v1+json', 'application/vnd.docker.container.image.v1+json')
       AND c.subject IS NULL AND NOT c.attachment
       AND NOT EXISTS (SELECT 1 FROM signatures g JOIN signature_attachments a ON a.signature_id = g.id
                        WHERE g.object_sha256 = c.digest AND a.repo_id = c.repo_id)
)
SELECT g.repo_id::bigint AS repo_id, ('sha256:' || encode(g.digest, 'hex'))::text AS digest, g.platform::text AS platform,
       COALESCE(s.state, '')::text AS state, COALESCE(s.error_code, '')::text AS error_code,
       COALESCE((extract(epoch FROM s.scanned_at) * 1000000)::bigint, 0)::bigint AS scanned_us,
       COALESCE(s.os_family, '')::text AS os_family, COALESCE(s.os_name, '')::text AS os_name, COALESCE(s.os_eosl, false)::boolean AS os_eosl,
       COALESCE(s.packages, 0)::int AS packages
  FROM images g
  LEFT JOIN vuln_image_scan s ON s.repo_id = g.repo_id AND s.name = g.name AND s.digest = g.digest
 WHERE g.child OR NOT EXISTS (SELECT 1 FROM images c WHERE c.repo_id = g.repo_id AND c.child)
 ORDER BY g.repo_id, g.platform, g.digest;

-- name: VulnImageGradeCounts :many
-- The open findings of the given (name, tag) pairs of images that the caller can read, per repository, tag and severity: a finding
-- counts for the tag whose manifest it names or whose index names that manifest (R-VULN-037), the counts behind the grades of R-VULN-039,
-- with how many of them have a fixed version (fixable). names and tags are parallel arrays.
WITH wanted AS (
    SELECT unnest(sqlc.arg(names)::text[]) AS name, unnest(sqlc.arg(tags)::text[]) AS tag
), tagged AS (
    SELECT p.repo_id, p.name, pv.version AS tag, i.blob_sha256 AS digest
      FROM wanted w
      JOIN packages p ON p.name = w.name
      JOIN repositories r ON r.id = p.repo_id
      JOIN package_versions pv ON pv.package_id = p.id AND pv.version = w.tag
      JOIN items i ON i.id = pv.lead_item_id
     WHERE r.format = 'oci' AND pv.trashed_at IS NULL
    UNION ALL
    SELECT t.repo_id, t.name, t.version AS tag, t.manifest_sha256 AS digest
      FROM wanted w
      JOIN oci_remote_tags t ON t.name = w.name AND t.version = w.tag
), images AS (
    SELECT repo_id, name, tag, digest FROM tagged
    UNION
    SELECT t.repo_id, t.name, t.tag, r.target
      FROM tagged t JOIN oci_references r ON r.repo_id = t.repo_id AND r.name = t.name AND r.manifest = t.digest AND r.kind = 'manifest'
)
SELECT g.repo_id::bigint AS repo_id, g.name::text AS name, g.tag::text AS version, f.severity::text AS severity, count(DISTINCT f.id)::bigint AS n,
       (count(DISTINCT f.id) FILTER (WHERE f.fix_available))::bigint AS fixable
  FROM images g
  JOIN vuln_finding f ON f.repo_id = g.repo_id AND f.ecosystem = 'OCI' AND f.package_name = g.name AND f.version = ('sha256:' || encode(g.digest, 'hex'))
 WHERE f.status IN ('needs_triage', 'confirmed', 'blocked')
   AND (NOT sqlc.arg(has_repos)::boolean OR f.repo_id = ANY(sqlc.arg(repos)::bigint[]))
   AND (f.repo_id = ANY(sqlc.arg(whole)::bigint[])
          OR concat_ws(chr(31), f.repo_id::text, f.package_name, g.tag) = ANY(sqlc.arg(rows)::text[])
          OR concat_ws(chr(31), f.repo_id::text, f.package_name) = ANY(sqlc.arg(package_rows)::text[]))
 GROUP BY g.repo_id, g.name, g.tag, f.severity
 ORDER BY g.repo_id, g.name, g.tag, f.severity;

-- name: VulnImageGradeScans :many
-- The manifests the given (name, tag) pairs stand for in the repositories, with how far each is scanned, its OS and whether that OS is at
-- its end of life: the scan facts of the grades of R-VULN-038 and of the list's grade (R-VULN-039). As VulnImageManifests, for many tags at
-- once: for an index its platform images, the children the repository stores that VulnImageDiscover's selection scans (Q-VULN-269), and
-- the index alone only while no platform image is stored.
WITH wanted AS (
    SELECT unnest(sqlc.arg(names)::text[]) AS name, unnest(sqlc.arg(tags)::text[]) AS tag
), tagged AS (
    SELECT p.repo_id, p.name, pv.version AS tag, i.blob_sha256 AS digest, ''::text AS platform
      FROM wanted w
      JOIN packages p ON p.name = w.name
      JOIN package_versions pv ON pv.package_id = p.id AND pv.version = w.tag
      JOIN items i ON i.id = pv.lead_item_id
     WHERE p.repo_id = ANY(sqlc.arg(repos)::bigint[]) AND pv.trashed_at IS NULL
    UNION ALL
    SELECT t.repo_id, t.name, t.version, t.manifest_sha256, ''::text
      FROM wanted w
      JOIN oci_remote_tags t ON t.name = w.name AND t.version = w.tag
     WHERE t.repo_id = ANY(sqlc.arg(repos)::bigint[])
), images AS (
    SELECT tagged.*, false AS child FROM tagged
    UNION
    SELECT t.repo_id, t.name, t.tag, r.target, COALESCE(r.platform, ''), true
      FROM tagged t
      JOIN oci_references r ON r.repo_id = t.repo_id AND r.name = t.name AND r.manifest = t.digest AND r.kind = 'manifest'
      JOIN oci_manifests c ON c.repo_id = r.repo_id AND c.name = r.name AND c.digest = r.target
     WHERE c.media_type IN ('application/vnd.oci.image.manifest.v1+json', 'application/vnd.docker.distribution.manifest.v2+json')
       AND COALESCE(c.artifact_type, '') IN ('', 'application/vnd.oci.image.config.v1+json', 'application/vnd.docker.container.image.v1+json')
       AND c.subject IS NULL AND NOT c.attachment
       AND NOT EXISTS (SELECT 1 FROM signatures g JOIN signature_attachments a ON a.signature_id = g.id
                        WHERE g.object_sha256 = c.digest AND a.repo_id = c.repo_id)
)
SELECT g.repo_id::bigint AS repo_id, g.name::text AS name, g.tag::text AS version, ('sha256:' || encode(g.digest, 'hex'))::text AS digest,
       g.platform::text AS platform, COALESCE(s.state, '')::text AS state,
       COALESCE((extract(epoch FROM s.scanned_at) * 1000000)::bigint, 0)::bigint AS scanned_us,
       COALESCE(s.os_family, '')::text AS os_family, COALESCE(s.os_name, '')::text AS os_name, COALESCE(s.os_eosl, false)::boolean AS os_eosl
  FROM images g
  LEFT JOIN vuln_image_scan s ON s.repo_id = g.repo_id AND s.name = g.name AND s.digest = g.digest
 WHERE g.child OR NOT EXISTS (
         SELECT 1 FROM images c WHERE c.repo_id = g.repo_id AND c.name = g.name AND c.tag = g.tag AND c.child)
 ORDER BY g.repo_id, g.name, g.tag, g.platform, g.digest;

-- name: VulnImageComponentsOf :many
-- The packages inside the image that findings name, for the findings of an image in a page of the report (R-VULN-012).
SELECT f.id::text AS id, f.components AS components FROM vuln_finding f WHERE f.id = ANY(sqlc.arg(ids)::text[]::uuid[]) AND f.ecosystem = 'OCI';
