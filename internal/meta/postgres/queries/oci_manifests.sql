-- The statements of the OCI data port for manifests: the rows a push writes, the checks it makes under the package's lock, and
-- the reads of the package view (internal/formatdata/oci/store_manifests.go; R-OCI-007, R-OCI-013, R-OCI-031, R-OCI-033).
-- A digest travels as bytea; the format's paths are built by the format and arrive as text.

-- name: OCIPutManifest :exec
-- Records a manifest of an image name; its row is a function of its bytes, so one that is recorded already keeps what it holds. Only
-- the facts of an image (R-OCI-040) that a row lacks are written again: a row recorded before they were kept, or one whose
-- configuration was not read. A configuration that another manifest of the name names was read already when that one's was: its facts
-- are a function of the config's bytes, so they are taken over.
INSERT INTO oci_manifests (repo_id, name, digest, media_type, artifact_type, subject, size, content_size, annotations, annotations_cut,
                           attachment, config_digest, blobs, config_facts)
VALUES (
  sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(digest)::bytea, sqlc.arg(media_type)::text,
  NULLIF(sqlc.arg(artifact_type)::text, ''), sqlc.narg(subject)::bytea, sqlc.arg(size)::bigint, sqlc.arg(content_size)::bigint,
  sqlc.narg(annotations)::jsonb, sqlc.arg(annotations_cut)::boolean, sqlc.arg(attachment)::boolean,
  sqlc.narg(config_digest)::bytea, sqlc.narg(blobs)::jsonb,
  COALESCE(sqlc.narg(config_facts)::jsonb,
           (SELECT s.config_facts FROM oci_manifests s
             WHERE s.repo_id = sqlc.arg(repo_id)::bigint AND s.name = sqlc.arg(name)::text
               AND s.config_digest = sqlc.narg(config_digest)::bytea AND s.config_facts IS NOT NULL
             LIMIT 1))
)
ON CONFLICT (repo_id, name, digest) DO UPDATE
   SET config_digest = COALESCE(oci_manifests.config_digest, EXCLUDED.config_digest),
       blobs = COALESCE(oci_manifests.blobs, EXCLUDED.blobs),
       config_facts = COALESCE(oci_manifests.config_facts, EXCLUDED.config_facts)
 WHERE (oci_manifests.blobs IS NULL AND EXCLUDED.blobs IS NOT NULL)
    OR (oci_manifests.config_facts IS NULL AND EXCLUDED.config_facts IS NOT NULL);

-- name: OCIConfigWaiting :many
-- The manifests of the name that name the blob as a configuration not read yet (an image's, R-OCI-040, or a Helm chart's, R-OCI-042):
-- the question of every commit of a blob, answered from the partial index of such manifests, whose answer is the read the commit queues.
SELECT digest FROM oci_manifests
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND config_digest = sqlc.arg(config_digest)::bytea
   AND config_facts IS NULL
 ORDER BY digest
 LIMIT 64;

-- name: OCIDescribeTarget :many
-- What the job oci-describe reads of a manifest (R-OCI-040, Q-OCI-271): its media type, whether its blobs are recorded, its
-- configuration's digest and media type and whether it is read, and the stored sizes of the manifest's and the configuration's items
-- under the name (-1 for one the repository does not hold).
SELECT m.media_type::text AS media_type, (m.blobs IS NULL)::boolean AS blobs_unread, m.config_digest,
       (m.config_facts IS NULL)::boolean AS facts_unread, COALESCE(m.blobs -> 'config' ->> 'mediaType', '')::text AS config_media_type,
       COALESCE((SELECT i.size FROM items i
                  WHERE i.repo_id = m.repo_id AND i.path = m.name || '/_manifests/sha256:' || encode(m.digest, 'hex')
                    AND i.kind = 'file' AND i.trashed_at IS NULL), -1)::bigint AS manifest_size,
       COALESCE((SELECT i.size FROM items i
                  WHERE i.repo_id = m.repo_id AND i.path = m.name || '/_blobs/sha256:' || encode(m.config_digest, 'hex')
                    AND i.kind = 'file' AND i.trashed_at IS NULL), -1)::bigint AS config_size
  FROM oci_manifests m
 WHERE m.repo_id = sqlc.arg(repo_id)::bigint AND m.name = sqlc.arg(name)::text AND m.digest = sqlc.arg(digest)::bytea;

-- name: OCIPutBlobs :exec
-- Records the blobs and the configuration's digest of a manifest stored before they were kept (R-OCI-040, the job oci-describe), and
-- takes over the facts of a configuration another manifest of the name names and that was read already.
UPDATE oci_manifests m
   SET blobs = sqlc.arg(blobs)::jsonb, config_digest = sqlc.narg(config_digest)::bytea,
       config_facts = (SELECT s.config_facts FROM oci_manifests s
                        WHERE s.repo_id = m.repo_id AND s.name = m.name AND s.config_digest = sqlc.narg(config_digest)::bytea
                          AND s.config_facts IS NOT NULL
                        LIMIT 1)
 WHERE m.repo_id = sqlc.arg(repo_id)::bigint AND m.name = sqlc.arg(name)::text AND m.digest = sqlc.arg(digest)::bytea
   AND m.blobs IS NULL;

-- name: OCIUndescribedManifests :many
-- The manifests after the cursor (by the id of the manifest's item) that the job oci-describe has to describe, at most row_limit of them
-- (the task remote-describe, R-OCI-040, Q-OCI-271), in local and remote repositories alike: an image manifest stored before its blobs
-- were kept, and a manifest whose configuration is stored and not read. Each half is answered from a partial index of its own; a
-- configuration the repository does not hold is no candidate, since nothing could read it without a request to an upstream.
SELECT i.id, m.repo_id, m.name::text AS name, m.digest
  FROM oci_manifests m
  JOIN items i ON i.repo_id = m.repo_id AND i.path = m.name || '/_manifests/sha256:' || encode(m.digest, 'hex')
              AND i.kind = 'file' AND i.trashed_at IS NULL
 WHERE i.id > sqlc.arg(after)::bigint
   AND ((m.blobs IS NULL
         AND m.media_type IN ('application/vnd.oci.image.manifest.v1+json', 'application/vnd.docker.distribution.manifest.v2+json'))
     OR (m.config_digest IS NOT NULL AND m.config_facts IS NULL
         AND EXISTS (SELECT 1 FROM items c
                      WHERE c.repo_id = m.repo_id AND c.path = m.name || '/_blobs/sha256:' || encode(m.config_digest, 'hex')
                        AND c.kind = 'file' AND c.trashed_at IS NULL)))
 ORDER BY i.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: OCIPutConfigFacts :exec
-- Records what the reader read of a configuration in every manifest of the name that names it and has none yet (R-OCI-040).
UPDATE oci_manifests
   SET config_facts = sqlc.arg(config_facts)::jsonb
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND config_digest = sqlc.arg(config_digest)::bytea
   AND config_facts IS NULL;

-- name: OCIGateFacts :many
-- What the signature gate asks of manifests of image names of a repository (signatures.md R-SIG-063): whether each is an attachment the
-- format recognised at its commit, and the indexes of the name that list it. The rows of every asked name and every asked digest come
-- back, and the caller keeps the pairs it asked for; a manifest that is not recorded has no row.
SELECT m.name::text AS name, m.digest, m.attachment,
       COALESCE((SELECT array_agg(r.manifest ORDER BY r.manifest)
                   FROM oci_references r
                  WHERE r.repo_id = m.repo_id AND r.name = m.name AND r.target = m.digest AND r.kind = 'manifest'), '{}')::bytea[] AS indexes
  FROM oci_manifests m
 WHERE m.repo_id = sqlc.arg(repo_id)::bigint AND m.digest = ANY(sqlc.arg(digests)::bytea[]) AND m.name = ANY(sqlc.arg(names)::text[]);

-- name: OCISetIndexContentSize :exec
-- Sets the content size of an index to the sum of its children's, saturated at the largest bigint (R-OCI-031, Q-OCI-130). The
-- required children are stored before the index (the commit transaction holds them against a removal) and a listed child cannot be
-- removed while the index exists, so their sum is final and, for a child that is an index itself, correct at any depth. A foreign
-- child (a descriptor with urls) is not required: it adds nothing here when it is not stored yet, and nothing later (H-672).
UPDATE oci_manifests m
   SET content_size = LEAST(COALESCE((
         SELECT sum(c.content_size)
           FROM oci_references r
           JOIN oci_manifests c ON c.repo_id = r.repo_id AND c.name = r.name AND c.digest = r.target
          WHERE r.repo_id = m.repo_id AND r.name = m.name AND r.manifest = m.digest AND r.kind = 'manifest'), 0),
         9223372036854775807)::bigint
 WHERE m.repo_id = sqlc.arg(repo_id)::bigint AND m.name = sqlc.arg(name)::text AND m.digest = sqlc.arg(digest)::bytea;

-- name: OCITagSizes :many
-- A remote's tag is read by its cache entry's id, the negated version id the listing gave (R-OCI-036): the listing decided that it is
-- listed, so the entry and its package are read by the primary key alone, which the view, read from the manifest's side, would not
-- (Q-OCI-267). The same in OCITagViews and OCIChildPlatforms.
-- What the size of the image each of the tags with the given version ids points at is made of (R-OCI-031, Q-OCI-221): the content
-- size of its manifest (the config and the layers an image manifest names) and, for an index, how many children it names, how many of
-- them this repository holds and their summed content size, counted once over every child and once over the children that are images
-- of a platform (an attestation manifest is named with the platform unknown/unknown). A remote repository holds a child once a client
-- fetched it, so the sum is read here and not from the index's own row, which was written before. One statement for any number of
-- packages.
WITH tags AS (
  SELECT pv.id, pv.package_id, i.blob_sha256 AS manifest_sha256
    FROM package_versions pv JOIN items i ON i.id = pv.lead_item_id
   WHERE pv.id = ANY(sqlc.arg(version_ids)::bigint[]) AND pv.trashed_at IS NULL AND i.trashed_at IS NULL
  UNION ALL
  SELECT -e.id, p.id, e.blob_sha256
    FROM remote_cache_entries e
    JOIN packages p ON p.repo_id = e.repo_id AND p.name = e.tag_name
   WHERE e.id = ANY(SELECT -x FROM unnest(sqlc.arg(version_ids)::bigint[]) AS x)
)
SELECT t.id AS version_id, m.content_size,
       count(r.target)::bigint AS children,
       count(c.digest)::bigint AS children_stored,
       LEAST(COALESCE(sum(c.content_size), 0), 9223372036854775807)::bigint AS children_size,
       (count(r.target) FILTER (WHERE r.platform IS DISTINCT FROM 'unknown/unknown'))::bigint AS platforms,
       (count(c.digest) FILTER (WHERE r.platform IS DISTINCT FROM 'unknown/unknown'))::bigint AS platforms_stored,
       LEAST(COALESCE(sum(c.content_size) FILTER (WHERE r.platform IS DISTINCT FROM 'unknown/unknown'), 0), 9223372036854775807)::bigint AS platforms_size
FROM tags t
JOIN packages p ON p.id = t.package_id
JOIN oci_manifests m ON m.repo_id = p.repo_id AND m.name = p.name AND m.digest = t.manifest_sha256
LEFT JOIN oci_references r ON r.repo_id = m.repo_id AND r.name = m.name AND r.manifest = m.digest AND r.kind = 'manifest'
LEFT JOIN oci_manifests c ON c.repo_id = r.repo_id AND c.name = r.name AND c.digest = r.target
GROUP BY t.id, m.content_size
ORDER BY t.id;

-- name: OCIPutReferences :exec
-- The edges of a manifest, one per target; an edge that is recorded already changes nothing. They travel as one JSON array of
-- {"target": "<hex>", "kind": "...", "platform": "..."} (sqlc has no multi-array unnest); an empty platform is none.
INSERT INTO oci_references (repo_id, name, manifest, target, kind, platform)
SELECT sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(manifest)::bytea, decode(e ->> 'target', 'hex'), e ->> 'kind',
       NULLIF(e ->> 'platform', '')
FROM jsonb_array_elements(sqlc.arg(refs)::jsonb) AS e
ON CONFLICT (repo_id, name, manifest, target) DO NOTHING;

-- name: OCIExistingItems :many
-- The live files among the paths (R-OCI-013 step 4: what a manifest names must be stored under the name).
SELECT path::text AS path
FROM items
WHERE repo_id = sqlc.arg(repo_id)::bigint AND path = ANY(sqlc.arg(paths)::text[]) AND kind = 'file' AND trashed_at IS NULL;

-- name: OCILockExistingItems :many
-- The same inside the commit transaction, with the rows held against a concurrent removal until the manifest's rows are written.
SELECT path::text AS path
FROM items
WHERE repo_id = sqlc.arg(repo_id)::bigint AND path = ANY(sqlc.arg(paths)::text[]) AND kind = 'file' AND trashed_at IS NULL
ORDER BY path
FOR SHARE;

-- name: OCIWithheldAmong :many
-- The digests among the given ones that the journal replay withheld under the name (R-OCI-033).
SELECT digest
FROM oci_tombstones
WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND digest = ANY(sqlc.arg(digests)::bytea[]);

-- name: OCITagViews :many
-- The tags with the given version ids and the rows of the manifests they point at, for the package view (R-OCI-031): the lead item
-- of the version is the tag item, whose blob is the manifest's; the tag of a remote repository (a negative id, R-OCI-036) is its cache
-- entry, whose document is the manifest. Any number of packages in one statement.
WITH tags AS (
  SELECT pv.id, pv.package_id, pv.version, i.blob_sha256 AS manifest_sha256
    FROM package_versions pv JOIN items i ON i.id = pv.lead_item_id
   WHERE pv.id = ANY(sqlc.arg(version_ids)::bigint[]) AND pv.trashed_at IS NULL AND i.trashed_at IS NULL
  UNION ALL
  SELECT -e.id, p.id, e.tag, e.blob_sha256
    FROM remote_cache_entries e
    JOIN packages p ON p.repo_id = e.repo_id AND p.name = e.tag_name
   WHERE e.id = ANY(SELECT -x FROM unnest(sqlc.arg(version_ids)::bigint[]) AS x)
)
SELECT t.id AS version_id, t.version::text AS tag, m.digest, m.media_type, COALESCE(m.artifact_type, '')::text AS artifact_type,
       m.size, m.content_size
FROM tags t
JOIN packages p ON p.id = t.package_id
JOIN oci_manifests m ON m.repo_id = p.repo_id AND m.name = p.name AND m.digest = t.manifest_sha256
ORDER BY t.id;

-- name: OCIChildPlatforms :many
-- The children the indexes of the given versions name, with the platform of the edge and, when this repository holds the child's
-- manifest, its own facts (R-OCI-031): a remote repository holds a platform once a client fetched it, and the page lists the others as
-- not fetched (Q-OCI-221).
WITH tags AS (
  SELECT pv.id, pv.package_id, i.blob_sha256 AS manifest_sha256
    FROM package_versions pv JOIN items i ON i.id = pv.lead_item_id
   WHERE pv.id = ANY(sqlc.arg(version_ids)::bigint[]) AND pv.trashed_at IS NULL AND i.trashed_at IS NULL
  UNION ALL
  SELECT -e.id, p.id, e.blob_sha256
    FROM remote_cache_entries e
    JOIN packages p ON p.repo_id = e.repo_id AND p.name = e.tag_name
   WHERE e.id = ANY(SELECT -x FROM unnest(sqlc.arg(version_ids)::bigint[]) AS x)
)
SELECT t.id AS version_id, COALESCE(r.platform, '')::text AS platform, r.target AS digest, (c.digest IS NOT NULL)::boolean AS stored,
       COALESCE(c.media_type, '')::text AS media_type, COALESCE(c.size, 0)::bigint AS size,
       COALESCE(c.content_size, 0)::bigint AS content_size
FROM tags t
JOIN packages p ON p.id = t.package_id
JOIN oci_references r ON r.repo_id = p.repo_id AND r.name = p.name AND r.manifest = t.manifest_sha256 AND r.kind = 'manifest'
LEFT JOIN oci_manifests c ON c.repo_id = r.repo_id AND c.name = r.name AND c.digest = r.target
ORDER BY t.id, r.platform NULLS LAST, r.target;

-- name: OCIStoredTags :many
-- The tags a local repository serves of a name, newest push first: live versions whose manifest the replay did not withhold.
SELECT pv.version::text AS tag
FROM package_versions pv
JOIN packages p ON p.id = pv.package_id
JOIN items i ON i.id = pv.lead_item_id
WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = sqlc.arg(name)::text AND pv.trashed_at IS NULL AND i.trashed_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM oci_tombstones t WHERE t.repo_id = p.repo_id AND t.name = p.name AND t.digest = i.blob_sha256)
ORDER BY pv.id DESC
LIMIT sqlc.arg(max_rows)::int;

-- name: OCIWithheldVersions :many
-- The live versions (tags) of the repositories (by "<project>/<repository>") that point at a manifest the replay withheld.
SELECT pv.id
FROM package_versions pv
JOIN packages p ON p.id = pv.package_id
JOIN repositories r ON r.id = p.repo_id
JOIN project rp ON rp.id = r.project_id
JOIN items i ON i.id = pv.lead_item_id
JOIN oci_tombstones t ON t.repo_id = p.repo_id AND t.name = p.name AND t.digest = i.blob_sha256
WHERE pv.trashed_at IS NULL AND (rp.key || '/' || r.key) = ANY(sqlc.arg(repos)::text[]);

-- name: OCITagFacts :many
-- The facts of the manifests the tags with the given version ids point at, for the package screens (R-OCI-039, R-OCI-040): the tag's own
-- manifest (child false) and, for an index, every child the repository holds, in platform order. The media types and the annotations are
-- read always (the kind and the description of a package list, R-OCI-041), and the flag `deprecated` of a Helm chart's Chart.yaml, one bit
-- of the configuration that marks the version in every list (R-API-085); the blobs and the configuration only with with_facts, which the
-- version page asks for: a configuration can hold what only a caller who may read its blob may see.
WITH tags AS (
  SELECT pv.id, pv.package_id, i.blob_sha256 AS manifest_sha256
    FROM package_versions pv JOIN items i ON i.id = pv.lead_item_id
   WHERE pv.id = ANY(sqlc.arg(version_ids)::bigint[]) AND pv.trashed_at IS NULL AND i.trashed_at IS NULL
  UNION ALL
  SELECT -e.id, p.id, e.blob_sha256
    FROM remote_cache_entries e
    JOIN packages p ON p.repo_id = e.repo_id AND p.name = e.tag_name
   WHERE e.id = ANY(SELECT -x FROM unnest(sqlc.arg(version_ids)::bigint[]) AS x)
), facts AS (
  SELECT t.id AS version_id, p.repo_id, false AS child, ''::text AS platform, m.digest, m.media_type, COALESCE(m.artifact_type, '') AS artifact_type,
         m.annotations, m.config_digest,
         CASE WHEN sqlc.arg(with_facts)::boolean THEN m.blobs END AS blobs,
         CASE WHEN sqlc.arg(with_facts)::boolean THEN m.config_facts END AS config_facts,
         COALESCE(m.config_facts->'chart'->>'deprecated' = 'true', false) AS chart_deprecated,
         (sqlc.arg(with_facts)::boolean AND m.config_facts IS NULL AND m.config_digest IS NOT NULL
          AND EXISTS (SELECT 1 FROM items ci
                       WHERE ci.repo_id = m.repo_id AND ci.path = m.name || '/_blobs/sha256:' || encode(m.config_digest, 'hex')
                         AND ci.kind = 'file' AND ci.trashed_at IS NULL)) AS config_stored
    FROM tags t
    JOIN packages p ON p.id = t.package_id
    JOIN oci_manifests m ON m.repo_id = p.repo_id AND m.name = p.name AND m.digest = t.manifest_sha256
  UNION ALL
  SELECT t.id, p.repo_id, true, COALESCE(r.platform, ''), c.digest, c.media_type, COALESCE(c.artifact_type, ''), c.annotations, c.config_digest,
         CASE WHEN sqlc.arg(with_facts)::boolean THEN c.blobs END,
         CASE WHEN sqlc.arg(with_facts)::boolean THEN c.config_facts END,
         false,
         (sqlc.arg(with_facts)::boolean AND c.config_facts IS NULL AND c.config_digest IS NOT NULL
          AND EXISTS (SELECT 1 FROM items ci
                       WHERE ci.repo_id = c.repo_id AND ci.path = c.name || '/_blobs/sha256:' || encode(c.config_digest, 'hex')
                         AND ci.kind = 'file' AND ci.trashed_at IS NULL))
    FROM tags t
    JOIN packages p ON p.id = t.package_id
    JOIN oci_references r ON r.repo_id = p.repo_id AND r.name = p.name AND r.manifest = t.manifest_sha256 AND r.kind = 'manifest'
    JOIN oci_manifests c ON c.repo_id = r.repo_id AND c.name = r.name AND c.digest = r.target
)
SELECT version_id::bigint AS version_id, repo_id::bigint AS repo_id, child::boolean AS child, platform::text AS platform, digest,
       media_type::text AS media_type, artifact_type::text AS artifact_type, annotations, config_digest, blobs::jsonb AS blobs,
       config_facts::jsonb AS config_facts, config_stored::boolean AS config_stored, chart_deprecated::boolean AS chart_deprecated
FROM facts
ORDER BY version_id, child, platform = '', platform, digest;

-- name: OCIViewDeprecated :many
-- The tags of the given repositories (qualified names), stored ones and a remote's, whose manifest is a Helm chart's whose Chart.yaml says
-- `deprecated: true` (R-API-085), at most max_rows; the caller intersects them with what the subject may read.
SELECT t.id::bigint AS version_id
  FROM (SELECT pv.id, pv.package_id, i.blob_sha256 AS manifest_sha256
          FROM package_versions pv JOIN items i ON i.id = pv.lead_item_id
         WHERE pv.trashed_at IS NULL AND i.trashed_at IS NULL
        UNION ALL
        SELECT rt.id, rt.package_id, rt.manifest_sha256 FROM oci_remote_tags rt) t
  JOIN packages p ON p.id = t.package_id
  JOIN repositories r ON r.id = p.repo_id
  JOIN project rp ON rp.id = r.project_id
  JOIN oci_manifests m ON m.repo_id = p.repo_id AND m.name = p.name AND m.digest = t.manifest_sha256
 WHERE (rp.key || '/' || r.key) = ANY(sqlc.arg(repos)::text[])
   AND m.config_facts->'chart'->>'deprecated' = 'true'
 ORDER BY 1
 LIMIT sqlc.arg(max_rows)::int;

-- name: OCIDownloadVersions :many
-- The version of a client's download of an image (operations-and-observability.md R-OPS-049 "A client's pull of an image"): for each
-- (repository, name, digest of the download's first file), the tag of the remote whose manifest is that digest, names it or names a
-- manifest that names it (an index, its platform manifest, its config and layers), the most recently validated first; a tag of the
-- referrers tag schema (`sha256-<hex>`) and an attachment tag are none. Without such a tag, the digest of the topmost manifest the file
-- belongs to (a pull by digest); "" when the file belongs to no stored manifest. The queries are a JSON array of {ord, repo_id, name,
-- digest (hex)}; the answer has one row per query, in the order of ord. Both tag spellings of the referrers tag schema and of an
-- attachment (`sha256-<hex>`, `sha256-<hex>.sig`) begin with sha256-.
SELECT t.ord::bigint AS ord,
       COALESCE(
         (SELECT substr(e.path, char_length(t.name) + 8)
            FROM (SELECT decode(t.digest, 'hex') AS m
                  UNION ALL
                  SELECT r1.manifest FROM oci_references r1
                   WHERE r1.repo_id = t.repo_id AND r1.name = t.name AND r1.target = decode(t.digest, 'hex')
                  UNION ALL
                  SELECT r2.manifest FROM oci_references r1
                    JOIN oci_references r2 ON r2.repo_id = r1.repo_id AND r2.name = r1.name AND r2.target = r1.manifest
                   WHERE r1.repo_id = t.repo_id AND r1.name = t.name AND r1.target = decode(t.digest, 'hex')) c
            JOIN remote_cache_entries e ON e.repo_id = t.repo_id AND e.blob_sha256 = c.m
           WHERE e.kind = 'metadata' AND e.variant = '' AND e.upstream_gone_at IS NULL
             AND left(e.path, char_length(t.name) + 7) = t.name || '/_tags/'
             AND position('/' IN substr(e.path, char_length(t.name) + 8)) = 0
             AND substr(e.path, char_length(t.name) + 8) NOT LIKE 'sha256-%'
           ORDER BY e.validated_at DESC, e.path
           LIMIT 1),
         (SELECT 'sha256:' || encode(c.m, 'hex')
            FROM (SELECT decode(t.digest, 'hex') AS m, 0 AS lvl
                  UNION ALL
                  SELECT r1.manifest, 1 FROM oci_references r1
                   WHERE r1.repo_id = t.repo_id AND r1.name = t.name AND r1.target = decode(t.digest, 'hex')
                  UNION ALL
                  SELECT r2.manifest, 2 FROM oci_references r1
                    JOIN oci_references r2 ON r2.repo_id = r1.repo_id AND r2.name = r1.name AND r2.target = r1.manifest
                   WHERE r1.repo_id = t.repo_id AND r1.name = t.name AND r1.target = decode(t.digest, 'hex')) c
            JOIN oci_manifests om ON om.repo_id = t.repo_id AND om.name = t.name AND om.digest = c.m
           ORDER BY c.lvl DESC, c.m
           LIMIT 1),
         '')::text AS version
  FROM jsonb_to_recordset(sqlc.arg(queries)::text::jsonb) AS t(ord bigint, repo_id bigint, name text, digest text)
 ORDER BY t.ord;
