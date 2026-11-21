-- The statements of the OCI data port for manifests: the rows a push writes, the checks it makes under the package's lock, and
-- the reads of the package view (internal/formatdata/oci/store_manifests.go; R-OCI-007, R-OCI-013, R-OCI-031, R-OCI-033).
-- A digest travels as bytea; the format's paths are built by the format and arrive as text.

-- name: OCIPutManifest :exec
-- Records a manifest of an image name; one that is recorded already changes nothing (its row is a function of its bytes).
INSERT INTO oci_manifests (repo_id, name, digest, media_type, artifact_type, subject, size, content_size, annotations, annotations_cut,
                           attachment)
VALUES (
  sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(digest)::bytea, sqlc.arg(media_type)::text,
  NULLIF(sqlc.arg(artifact_type)::text, ''), sqlc.narg(subject)::bytea, sqlc.arg(size)::bigint, sqlc.arg(content_size)::bigint,
  sqlc.narg(annotations)::jsonb, sqlc.arg(annotations_cut)::boolean, sqlc.arg(attachment)::boolean
)
ON CONFLICT (repo_id, name, digest) DO NOTHING;

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
  SELECT t.id, t.package_id, t.manifest_sha256 FROM oci_remote_tags t WHERE t.id = ANY(sqlc.arg(version_ids)::bigint[])
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
  SELECT t.id, t.package_id, t.version, t.manifest_sha256 FROM oci_remote_tags t WHERE t.id = ANY(sqlc.arg(version_ids)::bigint[])
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
  SELECT t.id, t.package_id, t.manifest_sha256 FROM oci_remote_tags t WHERE t.id = ANY(sqlc.arg(version_ids)::bigint[])
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
