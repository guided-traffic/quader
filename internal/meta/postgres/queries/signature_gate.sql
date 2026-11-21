-- The statements of the signature gate of a repository, stage 3 of the signatures (signatures.md R-SIG-061 to R-SIG-067, R-SIG-073;
-- internal/signatures/store/gate.go). A project travels as its uuid's text, '' for the installation's scope; a digest as bytea.

-- name: SignatureGateRows :many
-- The counting signatures (the roles publisher and provenance, R-SIG-062) of the subjects attached in the repositories of a gate's view,
-- with what the gate judges each by (R-SIG-066): the integrity state, the key IDs, the OIDC issuer and the identity of the signer, and the
-- trust row of the scope (the project's when one exists, else the installation's) with its anchors and the epoch it was computed under;
-- and whether Quader issued the signature itself (identity_id, stage 2), which never passes (R-SIG-062). The signature's id names what
-- passed an artifact in the API's member gate (R-SIG-073).
SELECT s.id, s.subject_sha256, s.state, s.key_ids,
       COALESCE(s.signer ->> 'oidcIssuer', '')::text AS oidc_issuer, COALESCE(s.signer ->> 'identity', '')::text AS identity,
       (s.identity_id IS NOT NULL)::boolean AS issued,
       (t.signature_id IS NOT NULL)::boolean AS has_trust, COALESCE(t.state, '')::text AS trust_state,
       COALESCE(t.anchor_ids, '{}')::bigint[] AS trust_anchor_ids, COALESCE(t.anchor_epoch, 0)::bigint AS trust_epoch
  FROM signatures s
  LEFT JOIN LATERAL (SELECT x.signature_id, x.state, x.anchor_ids, x.anchor_epoch
                       FROM signature_trust x
                      WHERE x.signature_id = s.id
                        AND (x.project_id IS NULL OR x.project_id::text = sqlc.arg(project_id)::text)
                      ORDER BY x.project_id NULLS LAST
                      LIMIT 1) AS t ON true
 WHERE s.subject_sha256 = ANY(sqlc.arg(subjects)::bytea[])
   AND s.role IN ('publisher', 'provenance')
   AND EXISTS (SELECT 1 FROM signature_attachments a WHERE a.signature_id = s.id AND a.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]))
 ORDER BY s.id;

-- name: SignatureGateFetches :many
-- The state of the background fetch of the subjects' signatures in the remote repositories of a gate's view (R-SIG-067).
SELECT f.repo_id, f.target::text AS target, f.subject_sha256, (f.queued_at IS NOT NULL)::boolean AS queued,
       (f.fetched_at IS NOT NULL)::boolean AS fetched, (f.failed_at IS NOT NULL)::boolean AS failed
  FROM signature_fetches f
 WHERE f.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]) AND f.subject_sha256 = ANY(sqlc.arg(subjects)::bytea[]);

-- name: SignatureGateObjectsRead :many
-- Those of the digests that are the object of a signature whose scheme was read, within the bound of an object (R-SIG-063: a signature
-- file passes the gate while this holds).
SELECT sha256 FROM signature_objects
 WHERE sha256 = ANY(sqlc.arg(digests)::bytea[]) AND scheme <> 'unknown' AND size <= sqlc.arg(max_size)::bigint;

-- name: SignatureGateAnchors :many
-- Every anchor as the replica's snapshot of the gate holds it (R-SIG-066): its scope and key for the references of a repository's member,
-- whether it is switched on, and the epoch of the write that last narrowed it.
SELECT id, scope, COALESCE(project_id::text, '')::text AS project_id, key, enabled, narrowed_epoch
  FROM signature_anchors
 ORDER BY id;

-- name: SignatureGateBlocks :many
-- Every block with the epoch its creation raised (R-SIG-066).
SELECT id, kind, value, COALESCE(issuer, '')::text AS issuer, anchor_epoch FROM signature_blocks ORDER BY id;

-- name: SignatureGateSettings :one
-- The switch for project Owners and the anchor epoch, for the replica's snapshot.
SELECT project_anchors, anchor_epoch FROM signature_settings WHERE id;

-- name: SignatureGatePrioritize :execrows
-- The priority work of a waiting download (R-SIG-067): the subjects' pending signatures are due at priority 0 now.
UPDATE signatures SET priority = 0, not_before = LEAST(not_before, now())
 WHERE subject_sha256 = ANY(sqlc.arg(subjects)::bytea[]) AND due AND state = 'pending' AND (priority > 0 OR not_before > now());

-- name: SignatureGateRaiseFetch :execrows
-- The priority work of a waiting download (R-SIG-067): the waiting job signature-fetch of a target runs first. An enqueue of a key that
-- waits returns the waiting job unchanged, so the gate raises it here; a job that runs already is left alone.
UPDATE job SET priority = 0, run_at = LEAST(run_at, now())
 WHERE kind = 'signature-fetch' AND dedup_key = ANY(sqlc.arg(keys)::text[]) AND state = 'queued' AND (priority > 0 OR run_at > now());

-- name: SignatureRefusalRecord :exec
-- A download a gate refused (R-SIG-073): one row per repository and artifact; a second refusal raises last_at and count.
INSERT INTO signature_refusals (repo_id, subject_sha256, path, reason)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(subject)::bytea, sqlc.arg(path)::text, sqlc.arg(reason)::text)
ON CONFLICT (repo_id, subject_sha256) DO UPDATE
   SET path = EXCLUDED.path, reason = EXCLUDED.reason, last_at = now(), count = signature_refusals.count + 1;

-- name: SignatureRefusalsExpire :execrows
-- The refused downloads older than signatures.gate.refusals_retention, removed by discovery (R-SIG-073).
DELETE FROM signature_refusals WHERE last_at < now() - make_interval(secs => sqlc.arg(retention_seconds)::float8);

-- name: SignatureAnchorNarrowed :exec
-- A write that narrowed an anchor records the epoch it raised (R-SIG-066).
UPDATE signature_anchors SET narrowed_epoch = sqlc.arg(epoch)::bigint WHERE id = sqlc.arg(id)::bigint;

-- name: SignatureBlockEpoch :exec
-- A new block records the epoch its creation raised (R-SIG-066).
UPDATE signature_blocks SET anchor_epoch = sqlc.arg(epoch)::bigint WHERE id = sqlc.arg(id)::bigint;

-- name: SignatureGateAnchorRefs :many
-- The anchors a repository's member signatures.anchors may name (R-SIG-061): every anchor of the installation's scopes and those of the
-- repository's project (by its key), as "<scope>/<key>".
SELECT (a.scope || '/' || a.key)::text AS ref
  FROM signature_anchors a
 WHERE a.project_id IS NULL OR a.project_id = (SELECT p.id FROM project p WHERE p.key = sqlc.arg(project_key)::text)
 ORDER BY 1;
