-- Who depends on a package (api-and-ui.md R-API-086): the writes of the table package_dependencies (migration 00139), which a format's
-- extractor fills where the version's facts are written (internal/packages/dependents), and the selection of the task remote-describe's
-- entry that fills it for what was stored before. The reads of the package view are in queries/package_view.sql
-- (PackageViewDependents, PackageViewDependentVersions, PackageViewProvided).

-- name: PackageDependenciesClear :exec
-- The rows of a version before its extractor writes them again: a version's dependencies are replaced, never merged.
DELETE FROM package_dependencies WHERE version_id = sqlc.arg(version_id)::bigint;

-- name: PackageDependenciesPut :exec
-- The rows of a version, with its repository, and the mark that its dependencies were read by the extractor of version reader. The four
-- lists are side by side (unnest in the select list zips arrays of one length); a row that is there already is kept. A version that no
-- longer exists writes nothing.
WITH v AS (
  SELECT pv.id, p.repo_id
    FROM package_versions pv
    JOIN packages p ON p.id = pv.package_id
   WHERE pv.id = sqlc.arg(version_id)::bigint
), mark AS (
  INSERT INTO package_dependency_reads (version_id, reader)
  SELECT v.id, sqlc.arg(reader)::integer FROM v
  ON CONFLICT (version_id) DO UPDATE SET reader = EXCLUDED.reader
)
INSERT INTO package_dependencies (version_id, repo_id, name, kind, range, provides)
SELECT v.id, v.repo_id, d.name, d.kind, d.range, d.provides
  FROM v
 CROSS JOIN (SELECT unnest(sqlc.arg(names)::text[]) AS name, unnest(sqlc.arg(kinds)::text[]) AS kind,
                    unnest(sqlc.arg(ranges)::text[]) AS range, unnest(sqlc.arg(provides)::boolean[]) AS provides) d
ON CONFLICT DO NOTHING;

-- name: PackageDependenciesVersionOfItem :many
-- The version a file belongs to (items.version_id): a Debian package file and an Ansible Galaxy archive name their version by their item.
SELECT version_id::bigint AS version_id FROM items WHERE id = sqlc.arg(item_id)::bigint AND version_id IS NOT NULL;

-- name: PackageDependenciesUnread :many
-- The live versions of the formats after the cursor (by version id) whose dependencies no extractor of version reader or later has read:
-- what the task remote-describe's entry reads, at most row_limit of them, oldest id first.
SELECT pv.id AS version_id, r.format
  FROM package_versions pv
  JOIN packages p ON p.id = pv.package_id
  JOIN repositories r ON r.id = p.repo_id
 WHERE pv.id > sqlc.arg(after)::bigint AND pv.trashed_at IS NULL
   AND r.format = ANY(sqlc.arg(formats)::text[])
   AND NOT EXISTS (SELECT 1 FROM package_dependency_reads d WHERE d.version_id = pv.id AND d.reader >= sqlc.arg(reader)::integer)
 ORDER BY pv.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: PackageDependenciesNPMManifests :many
-- The stored manifests of npm versions, which name their dependencies (npm_versions.manifest; a local publish's and a remote's described
-- version's).
SELECT version_id, manifest FROM npm_versions WHERE version_id = ANY(sqlc.arg(version_ids)::bigint[]);

-- name: PackageDependenciesNuGetMetadata :many
-- The stored metadata of NuGet versions, whose dependencyGroups name their dependencies (nuget_version.metadata).
SELECT version_id, metadata FROM nuget_version WHERE version_id = ANY(sqlc.arg(version_ids)::bigint[]);

-- name: PackageDependenciesDebControls :many
-- The control paragraphs of the live package files of Debian versions, one per architecture: a version depends on what any of its files
-- depends on.
SELECT i.version_id::bigint AS version_id, b.control
  FROM items i
  JOIN deb_binaries b ON b.item_id = i.id
 WHERE i.version_id = ANY(sqlc.arg(version_ids)::bigint[]) AND i.trashed_at IS NULL
 ORDER BY i.version_id, b.item_id;

-- name: PackageDependenciesAnsible :many
-- The stored dependencies of Ansible Galaxy collection versions (ansible_collection_versions.dependencies), by the version of their archive.
SELECT i.version_id::bigint AS version_id, a.dependencies
  FROM items i
  JOIN ansible_collection_versions a ON a.item_id = i.id
 WHERE i.version_id = ANY(sqlc.arg(version_ids)::bigint[]);
