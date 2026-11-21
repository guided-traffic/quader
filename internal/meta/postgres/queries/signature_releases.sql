-- The statements of stage 3, package 2 of the signatures (signatures.md R-SIG-070 to R-SIG-073; internal/signatures/store/releases.go):
-- the releases of a repository's gate, the stored items a preview judges, the candidates of an impact count and the list of refused
-- downloads. A digest travels as bytea, a principal as its uuid's text (empty for none).

-- name: SignatureGateReleased :many
-- Those of the subjects a release passes in the repository (R-SIG-070).
SELECT subject_sha256 FROM signature_releases
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND subject_sha256 = ANY(sqlc.arg(subjects)::bytea[]);

-- name: SignatureReleaseInsert :execrows
-- A release of an artifact in one repository (R-SIG-070); none when one passes it already.
INSERT INTO signature_releases (repo_id, subject_sha256, reason, refused_reason, released_by)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(subject)::bytea, sqlc.arg(reason)::text, sqlc.arg(refused_reason)::text,
        NULLIF(sqlc.arg(actor)::text, '')::uuid)
ON CONFLICT (repo_id, subject_sha256) DO NOTHING;

-- name: SignatureReleaseDelete :many
-- Withdraws a release (R-SIG-070); the reason it was granted with, none when no release passes the artifact.
DELETE FROM signature_releases WHERE repo_id = sqlc.arg(repo_id)::bigint AND subject_sha256 = sqlc.arg(subject)::bytea RETURNING reason;

-- name: SignatureReleases :many
-- The releases of the repositories, newest first, with the name of the principal that granted each ("<kind>:<name>", empty when it is gone);
-- subject narrows to one artifact (empty: every one).
SELECT sr.repo_id, sr.subject_sha256, sr.reason, sr.refused_reason, COALESCE(sr.released_by::text, '')::text AS released_by,
       COALESCE(CASE p.kind WHEN 'ci_mapping' THEN 'ci' ELSE p.kind END || ':' || p.name, '')::text AS released_by_name, sr.released_at
  FROM signature_releases sr
  LEFT JOIN principal p ON p.id = sr.released_by
 WHERE sr.repo_id = ANY(sqlc.arg(repo_ids)::bigint[])
   AND (octet_length(sqlc.arg(subject)::bytea) = 0 OR sr.subject_sha256 = sqlc.arg(subject)::bytea)
 ORDER BY sr.released_at DESC, sr.repo_id, sr.subject_sha256
 LIMIT sqlc.arg(max_rows)::int;

-- name: SignatureGateItems :many
-- One batch of the stored files of the repositories a preview judges (R-SIG-072), in the order of the index of live paths after the
-- position (after_repo, after_path); a generated item never meets the gate and is not read.
SELECT i.id, i.repo_id, i.path::text AS path, i.blob_sha256
  FROM items i
 WHERE i.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]) AND i.trashed_at IS NULL AND i.kind = 'file' AND NOT i.generated
   AND (i.repo_id, i.path) > (sqlc.arg(after_repo)::bigint, sqlc.arg(after_path)::text COLLATE "C")
 ORDER BY i.repo_id, i.path
 LIMIT sqlc.arg(max_rows)::int;

-- name: SignatureImpactByAnchors :many
-- The artifacts a change of anchors can take the state from (R-SIG-071): the subjects of counting signatures whose trust row, of any scope,
-- names one of the anchors, attached in the repositories, through the index signature_trust_anchors.
SELECT DISTINCT a.repo_id, s.subject_sha256
  FROM signature_trust t
  JOIN signatures s ON s.id = t.signature_id
  JOIN signature_attachments a ON a.signature_id = s.id
 WHERE t.anchor_ids && sqlc.arg(anchor_ids)::bigint[]
   AND s.role IN ('publisher', 'provenance')
   AND a.repo_id = ANY(sqlc.arg(repo_ids)::bigint[])
 LIMIT sqlc.arg(max_rows)::int;

-- name: SignatureImpactByKeys :many
-- The artifacts a block of a key, a fingerprint or a serial can take the state from (R-SIG-071): the subjects of counting signatures that
-- carry one of the identifiers, attached in the repositories, through the index signatures_keys.
SELECT DISTINCT a.repo_id, s.subject_sha256
  FROM signatures s
  JOIN signature_attachments a ON a.signature_id = s.id
 WHERE s.key_ids && sqlc.arg(key_ids)::text[]
   AND s.role IN ('publisher', 'provenance')
   AND a.repo_id = ANY(sqlc.arg(repo_ids)::bigint[])
 LIMIT sqlc.arg(max_rows)::int;

-- name: SignatureImpactByIdentity :many
-- The artifacts a block of a Sigstore identity can take the state from (R-SIG-071): the subjects of counting signatures with an identity,
-- attached in the repositories, with the identity and its OIDC issuer; the caller matches the block's pattern.
SELECT DISTINCT a.repo_id, s.subject_sha256, (s.signer ->> 'identity')::text AS identity,
       COALESCE(s.signer ->> 'oidcIssuer', '')::text AS oidc_issuer
  FROM signatures s
  JOIN signature_attachments a ON a.signature_id = s.id
 WHERE COALESCE(s.signer ->> 'identity', '') <> ''
   AND s.role IN ('publisher', 'provenance')
   AND a.repo_id = ANY(sqlc.arg(repo_ids)::bigint[])
 LIMIT sqlc.arg(max_rows)::int;

-- name: SignatureRefusalHolders :many
-- The repositories with a refused download (R-SIG-073), whose readable ones scope the list.
SELECT DISTINCT r.id AS repo_id, pr.key AS project_key, r.key AS repo_key, r.format
  FROM signature_refusals f
  JOIN repositories r ON r.id = f.repo_id
  JOIN project pr ON pr.id = r.project_id
 ORDER BY pr.key, r.key;

-- name: SignatureRefusals :many
-- One page of refused downloads, the latest refusal first (R-SIG-073), within the repositories asked, one repository (repo_id, 0 none),
-- one reason (empty: every one), after the cursor (after_at, after_repo, after_subject); with the release that passes the artifact since.
SELECT f.repo_id, pr.key AS project_key, r.key AS repo_key, r.format, f.subject_sha256, f.path, f.reason, f.first_at, f.last_at, f.count,
       (sr.repo_id IS NOT NULL)::boolean AS released, COALESCE(sr.reason, '')::text AS release_reason,
       COALESCE(CASE p.kind WHEN 'ci_mapping' THEN 'ci' ELSE p.kind END || ':' || p.name, '')::text AS released_by_name,
       sr.released_at
  FROM signature_refusals f
  JOIN repositories r ON r.id = f.repo_id
  JOIN project pr ON pr.id = r.project_id
  LEFT JOIN signature_releases sr ON sr.repo_id = f.repo_id AND sr.subject_sha256 = f.subject_sha256
  LEFT JOIN principal p ON p.id = sr.released_by
 WHERE f.repo_id = ANY(sqlc.arg(repo_ids)::bigint[])
   AND (sqlc.arg(repo_id)::bigint = 0 OR f.repo_id = sqlc.arg(repo_id)::bigint)
   AND (sqlc.arg(reason)::text = '' OR f.reason = sqlc.arg(reason)::text)
   AND (sqlc.arg(after_at)::text = ''
        OR (f.last_at, f.repo_id, f.subject_sha256)
           < (NULLIF(sqlc.arg(after_at)::text, '')::timestamptz, sqlc.arg(after_repo)::bigint, sqlc.arg(after_subject)::bytea))
 ORDER BY f.last_at DESC, f.repo_id DESC, f.subject_sha256 DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: SignatureGateBlobItem :many
-- A live stored file with the blob in the repositories (R-SIG-070: the artifact a release names), through the index items_blob.
SELECT i.repo_id, i.path::text AS path
  FROM items i
 WHERE i.blob_sha256 = sqlc.arg(blob)::bytea AND i.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]) AND i.kind = 'file' AND i.trashed_at IS NULL
 ORDER BY i.repo_id, i.path
 LIMIT 1;
