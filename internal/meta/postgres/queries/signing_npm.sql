-- The statements of npm's registry signature of the versions of local npm repositories in the job signing (signatures.md R-SIG-048 to
-- R-SIG-050; internal/signing/npmsign). The version's tarball is its lead item (package_versions.lead_item_id); Quader's signature of it
-- is a signature of the scheme npm-registry with identity_id set, attached issued to the tarball (R-SIG-054), written through the
-- signature store. A version's signature is served while its identity is current or previous and some identity is current (R-SIG-045);
-- the job asks "served" with an identity current, so the state alone decides there.

-- name: SigningNPMLockPackage :many
-- The package of a local npm version's tarball, locked FOR UPDATE: the signing of its versions, a publish and a tag change of the
-- package run one after the other. No row: the item is no tarball of a version of a local npm repository (any more).
SELECT p.id, p.repo_id, p.name
  FROM items i
  JOIN package_versions pv ON pv.id = i.version_id AND pv.lead_item_id = i.id
  JOIN packages p ON p.id = pv.package_id
  JOIN repositories r ON r.id = p.repo_id AND r.format = 'npm' AND r.class = 'local'
 WHERE i.id = sqlc.arg(item_id)::bigint
   FOR UPDATE OF p;

-- name: SigningNPMLockPackageWork :many
-- The other npm work of the package's versions that no other operation holds, locked, at most max_rows: one operation signs the
-- versions of a package together and raises its revision once (R-SIG-050). A row another operation holds is that operation's.
SELECT w.item_id
  FROM signing_work w
  JOIN package_versions pv ON pv.lead_item_id = w.item_id
 WHERE w.kind = 'npm' AND NOT w.failed AND pv.package_id = sqlc.arg(package_id)::bigint AND w.item_id <> sqlc.arg(item_id)::bigint
 ORDER BY w.item_id
 LIMIT sqlc.arg(max_rows)::bigint
   FOR UPDATE OF w SKIP LOCKED;

-- name: SigningNPMUnsigned :many
-- Of the tarballs named, those of the package's versions a document holds (live, not tombstoned) that carry no served signature, with
-- what the signed message needs: <name>@<version>:<integrity> (R-SIG-050).
SELECT i.id AS item_id, i.path::text AS path, pv.version, nv.integrity
  FROM package_versions pv
  JOIN packages p ON p.id = pv.package_id
  JOIN npm_versions nv ON nv.version_id = pv.id
  JOIN items i ON i.id = pv.lead_item_id AND i.kind = 'file' AND i.trashed_at IS NULL
 WHERE pv.package_id = sqlc.arg(package_id)::bigint AND pv.trashed_at IS NULL AND i.id = ANY(sqlc.arg(item_ids)::bigint[])
   AND NOT EXISTS (SELECT 1 FROM npm_tombstones t WHERE t.repo_id = p.repo_id AND t.name = p.name AND t.version = pv.version)
   AND NOT EXISTS (SELECT 1
                     FROM signature_attachments a
                     JOIN signatures s ON s.id = a.signature_id
                     JOIN signature_objects o ON o.sha256 = s.object_sha256 AND o.scheme = 'npm-registry'
                     JOIN signing_identities si ON si.id = s.identity_id AND si.state IN ('current', 'previous')
                    WHERE a.item_id = i.id AND a.source = 'issued')
 ORDER BY pv.id;

-- name: SigningNPMDoneWork :exec
-- The npm work of the tarballs an operation signed beside its own row, whose locks it holds.
DELETE FROM signing_work WHERE kind = 'npm' AND item_id = ANY(sqlc.arg(item_ids)::bigint[]);

-- name: SigningNPMTouch :exec
-- time.modified of a package whose document changed (R-NPM-012): forward by at least a millisecond, as a tag change moves it.
UPDATE npm_packages SET modified_at = greatest(modified_at + interval '1 millisecond', sqlc.arg(at)::text::timestamptz)
 WHERE package_id = sqlc.arg(package_id)::bigint;

-- name: SigningNPMDiscover :many
-- Discovery's batch (R-SIG-049): the tarballs of the versions of local npm repositories a document holds (the predicate of
-- SigningNPMUnsigned, so that what discovery queues the operation signs or finds done), after the cursor in item order, each with whether
-- it carries a served signature and whether a row of work exists for it: a row that waits or failed is the job's, and queuing it again
-- would start a failed row's attempts anew in every run.
SELECT i.id,
       EXISTS (SELECT 1
                 FROM signature_attachments a
                 JOIN signatures s ON s.id = a.signature_id
                 JOIN signature_objects o ON o.sha256 = s.object_sha256 AND o.scheme = 'npm-registry'
                 JOIN signing_identities si ON si.id = s.identity_id AND si.state IN ('current', 'previous')
                WHERE a.item_id = i.id AND a.source = 'issued')::boolean AS signed,
       EXISTS (SELECT 1 FROM signing_work w WHERE w.kind = 'npm' AND w.item_id = i.id)::boolean AS queued
  FROM items i
  JOIN package_versions pv ON pv.id = i.version_id AND pv.lead_item_id = i.id AND pv.trashed_at IS NULL
  JOIN packages p ON p.id = pv.package_id
  JOIN npm_versions nv ON nv.version_id = pv.id
  JOIN repositories r ON r.id = i.repo_id AND r.format = 'npm' AND r.class = 'local'
 WHERE i.id > sqlc.arg(after)::bigint AND i.kind = 'file' AND i.trashed_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM npm_tombstones t WHERE t.repo_id = p.repo_id AND t.name = p.name AND t.version = pv.version)
 ORDER BY i.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: SigningNPMPackagesOf :many
-- The packages of local npm repositories whose versions carry a signature of Quader's of the identities named, after the package id, at
-- most max_rows (R-SIG-048): the documents a change of what is served changes.
SELECT DISTINCT p.id, p.repo_id, p.name
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256 AND o.scheme = 'npm-registry'
  JOIN signature_attachments a ON a.signature_id = s.id AND a.source = 'issued'
  JOIN items i ON i.id = a.item_id
  JOIN package_versions pv ON pv.id = i.version_id AND pv.lead_item_id = i.id
  JOIN packages p ON p.id = pv.package_id
  JOIN repositories r ON r.id = p.repo_id AND r.format = 'npm' AND r.class = 'local'
 WHERE s.identity_id = ANY(sqlc.arg(identity_ids)::bigint[]) AND p.id > sqlc.arg(after)::bigint
 ORDER BY p.id
 LIMIT sqlc.arg(max_rows)::bigint;

-- name: SigningNPMItemsOf :many
-- The tarballs of the packages named that carry a signature of Quader's of the identities named: what a retirement leaves to sign again.
SELECT DISTINCT i.id
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256 AND o.scheme = 'npm-registry'
  JOIN signature_attachments a ON a.signature_id = s.id AND a.source = 'issued'
  JOIN items i ON i.id = a.item_id
  JOIN package_versions pv ON pv.id = i.version_id AND pv.lead_item_id = i.id
 WHERE s.identity_id = ANY(sqlc.arg(identity_ids)::bigint[]) AND pv.package_id = ANY(sqlc.arg(package_ids)::bigint[])
 ORDER BY i.id;
