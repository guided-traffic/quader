-- The statements of the verification, the trust step and the management of signatures on artifacts (signatures.md R-SIG-008,
-- R-SIG-011, R-SIG-012, R-SIG-015 to R-SIG-021, R-SIG-034, R-SIG-038, R-SIG-039; internal/signatures/store and internal/signatures/job).
-- The conventions of queries/signatures.sql hold: every parameter carries its type, a time travels as RFC 3339 text ('' for none), a
-- batch travels as parallel arrays, one element per row. A project travels as its uuid's text, '' for the installation's scope; an
-- anchor set travels as the text of a bigint array ('{1,2}').

-- name: SignatureSettings :one
-- The one row with the name of its last writer, the public Sigstore root the task stored, and the backlog: signatures due or waiting
-- for their check, and trust rows computed under an older anchor epoch.
SELECT st.project_anchors, st.anchor_epoch, st.version, st.updated_at,
       COALESCE(CASE p.kind WHEN 'ci_mapping' THEN 'ci' ELSE p.kind END || ':' || p.name, '')::text AS updated_by_name,
       COALESCE(st.trust_root_error, '')::text AS trust_root_error, st.trust_root_error_at,
       (tr.name IS NOT NULL)::boolean AS root_stored, COALESCE(tr.tuf_version_number, 0)::bigint AS root_tuf_version,
       tr.fetched_at AS root_fetched_at,
       (SELECT count(*) FROM signatures s WHERE s.due OR s.state = 'pending')::bigint AS pending,
       (SELECT count(*) FROM signature_trust t WHERE t.anchor_epoch < st.anchor_epoch)::bigint AS stale
  FROM signature_settings st
  LEFT JOIN principal p ON p.id = st.updated_by
  LEFT JOIN signature_trust_roots tr ON tr.name = 'trusted_root.json'
 WHERE st.id;

-- name: SignatureLockSettings :one
-- The switch, the version and the epoch, locked for a write of the switch or of a project anchor.
SELECT project_anchors, version, anchor_epoch FROM signature_settings WHERE id FOR UPDATE;

-- name: SignatureUpdateSwitch :execrows
-- The switch under the row's version (R-SIG-019); the store raises the anchor epoch when its value changed (R-SIG-012).
UPDATE signature_settings
   SET project_anchors = sqlc.arg(project_anchors)::boolean, version = version + 1, updated_at = now(),
       updated_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
 WHERE id AND version = sqlc.arg(version)::bigint;

-- name: SignatureRaiseEpoch :one
-- A write of an anchor, a block or a stored public root raises the anchor epoch in its transaction (R-SIG-012).
UPDATE signature_settings SET anchor_epoch = anchor_epoch + 1 WHERE id RETURNING anchor_epoch;

-- name: SignatureEpoch :one
-- The anchor epoch and the switch, as a run of the job reads them once.
SELECT anchor_epoch, project_anchors FROM signature_settings WHERE id;

-- name: SignatureAnchors :many
-- Anchors by id (0: any), by scope ('': every scope but project; 'project': every project's; else that scope) and by project ('': any),
-- in scope order and then by title, with the project's key, the creator's name and the number of signatures the anchor knows: in the
-- installation's scope, or for a project anchor in its project's.
SELECT a.id, a.scope, COALESCE(a.project_id::text, '')::text AS project_id, COALESCE(pr.key, '')::text AS project_key, a.key,
       a.title, a.kind, a.material, a.enabled, COALESCE(a.note, '')::text AS note, a.version, a.created_at, a.updated_at,
       COALESCE(CASE p.kind WHEN 'ci_mapping' THEN 'ci' ELSE p.kind END || ':' || p.name, '')::text AS created_by_name,
       (SELECT count(*) FROM signature_trust t
         WHERE t.anchor_ids @> ARRAY[a.id]::bigint[] AND t.project_id IS NOT DISTINCT FROM a.project_id)::bigint AS signatures
  FROM signature_anchors a
  LEFT JOIN project pr ON pr.id = a.project_id
  LEFT JOIN principal p ON p.id = a.created_by
 WHERE (sqlc.arg(id)::bigint = 0 OR a.id = sqlc.arg(id)::bigint)
   AND (sqlc.arg(project_id)::text = '' OR a.project_id::text = sqlc.arg(project_id)::text)
   AND (sqlc.arg(id)::bigint <> 0 OR sqlc.arg(project_id)::text <> ''
        OR (sqlc.arg(scope)::text = '' AND a.scope <> 'project') OR a.scope = sqlc.arg(scope)::text)
 ORDER BY CASE a.scope WHEN 'shipped' THEN 0 WHEN 'configuration' THEN 1 WHEN 'installation' THEN 2 ELSE 3 END, lower(a.title), a.id;

-- name: SignatureAnchorLock :many
-- One anchor, locked for a change.
SELECT id, scope, COALESCE(project_id::text, '')::text AS project_id, key, title, kind, material, enabled,
       COALESCE(note, '')::text AS note, version
  FROM signature_anchors WHERE id = sqlc.arg(id)::bigint FOR UPDATE;

-- name: SignatureAnchorsOfScope :many
-- Every anchor of a scope that names no project (shipped, configuration), locked, for quader migrate's declared state.
SELECT id, key, title, kind, material, enabled, COALESCE(note, '')::text AS note
  FROM signature_anchors WHERE scope = sqlc.arg(scope)::text AND project_id IS NULL
 ORDER BY key FOR UPDATE;

-- name: SignatureAnchorInsert :many
-- A new anchor; no row when the key is taken in the scope and project.
INSERT INTO signature_anchors (scope, project_id, key, title, kind, material, enabled, note, created_by)
VALUES (sqlc.arg(scope)::text, NULLIF(sqlc.arg(project_id)::text, '')::uuid, sqlc.arg(key)::text, sqlc.arg(title)::text,
        sqlc.arg(kind)::text, sqlc.arg(material)::jsonb, sqlc.arg(enabled)::boolean, NULLIF(sqlc.arg(note)::text, ''),
        NULLIF(sqlc.arg(actor)::text, '')::uuid)
ON CONFLICT (scope, project_id, key) DO NOTHING
RETURNING id;

-- name: SignatureAnchorUpdate :execrows
-- The members of an anchor a change may set; its version rises.
UPDATE signature_anchors
   SET title = sqlc.arg(title)::text, kind = sqlc.arg(kind)::text, material = sqlc.arg(material)::jsonb,
       enabled = sqlc.arg(enabled)::boolean, note = NULLIF(sqlc.arg(note)::text, ''), version = version + 1, updated_at = now()
 WHERE id = sqlc.arg(id)::bigint;

-- name: SignatureAnchorDelete :execrows
-- Removes an anchor.
DELETE FROM signature_anchors WHERE id = sqlc.arg(id)::bigint;

-- name: SignatureAnchorsEnabled :many
-- Every anchor that is switched on, in id order: what the job builds its scopes from (R-SIG-008, R-SIG-016). The caller drops the
-- project anchors while the switch is off.
SELECT id, scope, COALESCE(project_id::text, '')::text AS project_id, key, title, kind, material
  FROM signature_anchors WHERE enabled ORDER BY id;

-- name: SignatureAnchorRefs :many
-- The titles and scopes of anchors, by id.
SELECT id, title, scope FROM signature_anchors WHERE id = ANY(sqlc.arg(ids)::bigint[]) ORDER BY id;

-- name: SignatureBlocks :many
-- Blocks by id (an empty list: every block), newest first, with the creator's name and the signatures they block in the
-- installation's scope.
SELECT b.id, b.kind, b.value, COALESCE(b.issuer, '')::text AS issuer, b.reason, b.created_at,
       COALESCE(CASE p.kind WHEN 'ci_mapping' THEN 'ci' ELSE p.kind END || ':' || p.name, '')::text AS created_by_name,
       (SELECT count(*) FROM signature_trust t WHERE t.block_id = b.id AND t.project_id IS NULL)::bigint AS signatures
  FROM signature_blocks b
  LEFT JOIN principal p ON p.id = b.created_by
 WHERE cardinality(sqlc.arg(ids)::bigint[]) = 0 OR b.id = ANY(sqlc.arg(ids)::bigint[])
 ORDER BY b.created_at DESC, b.id DESC;

-- name: SignatureBlockInsert :many
-- A new block; no row when the same signer is blocked already.
INSERT INTO signature_blocks (kind, value, issuer, reason, created_by)
VALUES (sqlc.arg(kind)::text, sqlc.arg(value)::text, NULLIF(sqlc.arg(issuer)::text, ''), sqlc.arg(reason)::text,
        NULLIF(sqlc.arg(actor)::text, '')::uuid)
ON CONFLICT (kind, value, issuer) DO NOTHING
RETURNING id;

-- name: SignatureBlockDelete :many
-- Lifts a block and answers what it blocked.
DELETE FROM signature_blocks WHERE id = sqlc.arg(id)::bigint
RETURNING kind, value, COALESCE(issuer, '')::text AS issuer;

-- name: SignatureFindingHolders :many
-- The repositories that hold an attachment of an invalid signature (kind invalid) or of a signature whose installation trust row is
-- blocked (kind blocked).
SELECT DISTINCT r.id AS repo_id, pr.key AS project_key, r.key AS repo_key, r.format
  FROM signature_attachments a
  JOIN signatures s ON s.id = a.signature_id
  JOIN repositories r ON r.id = a.repo_id
  JOIN project pr ON pr.id = r.project_id
 WHERE ('invalid' = ANY(sqlc.arg(kinds)::text[]) AND s.state = 'invalid')
    OR ('blocked' = ANY(sqlc.arg(kinds)::text[])
        AND EXISTS (SELECT 1 FROM signature_trust t WHERE t.signature_id = s.id AND t.project_id IS NULL AND t.state = 'blocked'))
 ORDER BY pr.key, r.key;

-- name: SignatureFindings :many
-- One page of findings, newest first (R-SIG-038): invalid signatures with an attachment, found when they were checked, and signatures
-- whose installation trust row is blocked, found when it was computed; within the repositories asked (all_repos, or repo_ids), one
-- repository (repo_id, 0 none), a fragment of the signer's text or a key ID (q), after the cursor (after_at, after_id).
SELECT f.signature_id, f.kind, f.scheme, f.role, f.reason, f.signer, f.subject_sha256, f.subject_name, f.found_at, f.block_id
  FROM (SELECT s.id AS signature_id, 'invalid'::text AS kind, o.scheme, s.role, COALESCE(s.reason, '')::text AS reason, s.signer,
               s.subject_sha256, COALESCE(s.subject_name, '')::text AS subject_name,
               COALESCE(s.checked_at, s.received_at)::timestamptz AS found_at, 0::bigint AS block_id, s.key_ids
          FROM signatures s
          JOIN signature_objects o ON o.sha256 = s.object_sha256
         WHERE s.state = 'invalid' AND 'invalid' = ANY(sqlc.arg(kinds)::text[])
        UNION ALL
        SELECT s.id, 'blocked'::text, o.scheme, s.role, COALESCE(s.reason, '')::text, s.signer, s.subject_sha256,
               COALESCE(s.subject_name, '')::text, t.computed_at, COALESCE(t.block_id, 0)::bigint, s.key_ids
          FROM signature_trust t
          JOIN signatures s ON s.id = t.signature_id
          JOIN signature_objects o ON o.sha256 = s.object_sha256
         WHERE t.state = 'blocked' AND t.project_id IS NULL AND 'blocked' = ANY(sqlc.arg(kinds)::text[])) AS f
 WHERE EXISTS (SELECT 1 FROM signature_attachments a
                WHERE a.signature_id = f.signature_id
                  AND (sqlc.arg(all_repos)::boolean OR a.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]))
                  AND (sqlc.arg(repo_id)::bigint = 0 OR a.repo_id = sqlc.arg(repo_id)::bigint))
   AND (sqlc.arg(q)::text = '' OR strpos(lower(COALESCE(f.signer::text, '')), lower(sqlc.arg(q)::text)) > 0
        OR EXISTS (SELECT 1 FROM unnest(f.key_ids) k WHERE strpos(lower(k), lower(sqlc.arg(q)::text)) > 0))
   AND (sqlc.arg(after_at)::text = ''
        OR (f.found_at, f.signature_id) < (NULLIF(sqlc.arg(after_at)::text, '')::timestamptz, sqlc.arg(after_id)::bigint))
 ORDER BY f.found_at DESC, f.signature_id DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: SignatureFindingFiles :many
-- Where the signatures of a page are held within the repositories asked: per repository with an attachment, the files of the
-- subject's blob there ('' when the repository holds only the attachment, as an npm registry signature's), at most per_signature rows
-- per signature.
SELECT x.signature_id, x.repo_id, x.project_key, x.repo_key, x.format, x.path
  FROM (SELECT h.signature_id, h.repo_id, h.project_key, h.repo_key, h.format, h.path,
               row_number() OVER (PARTITION BY h.signature_id ORDER BY h.project_key, h.repo_key, h.path) AS n
          FROM (SELECT DISTINCT a.signature_id, r.id AS repo_id, pr.key AS project_key, r.key AS repo_key, r.format,
                       COALESCE(i.path, '')::text AS path
                  FROM signature_attachments a
                  JOIN signatures s ON s.id = a.signature_id
                  JOIN repositories r ON r.id = a.repo_id
                  JOIN project pr ON pr.id = r.project_id
                  LEFT JOIN items i ON i.repo_id = a.repo_id AND i.blob_sha256 = s.subject_sha256 AND i.kind = 'file'
                                   AND i.trashed_at IS NULL
                 WHERE a.signature_id = ANY(sqlc.arg(signature_ids)::bigint[])
                   AND (sqlc.arg(all_repos)::boolean OR a.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]))) AS h) AS x
 WHERE x.n <= sqlc.arg(per_signature)::bigint
 ORDER BY x.signature_id, x.project_key, x.repo_key, x.path;

-- name: SignatureViews :many
-- The signatures of the subjects (or npm's subject names) attached in the repositories asked, with their object's scheme, media type
-- and size, and their trust row in the scope: the project's row when one exists, else the installation's; with the current epoch; and
-- for a signature Quader made itself the identity that made it (R-SIG-055), and for its receipt attestation the bundle the API decodes
-- (about 2.5 KiB, read for that signature alone) and where the job serves it (signing_attestations, 00105).
SELECT s.id, s.object_sha256, s.part, s.subject_sha256, COALESCE(s.subject_name, '')::text AS subject_name, s.role,
       COALESCE(s.predicate_type, '')::text AS predicate_type, s.content_sha256, COALESCE(s.content_locator, '')::text AS content_locator,
       s.state, COALESCE(s.reason, '')::text AS reason, s.signer, s.key_ids, s.signed_digest, COALESCE(s.digest_alg, '')::text AS digest_alg,
       s.claimed_at, s.proven_at, COALESCE(s.time_proof, '')::text AS time_proof, s.received_at, s.checked_at, s.due, s.priority,
       s.attempts, s.not_before,
       o.scheme, COALESCE(o.media_type, '')::text AS media_type, o.size AS object_size,
       (t.signature_id IS NOT NULL)::boolean AS has_trust, COALESCE(t.project_id::text, '')::text AS trust_project_id,
       COALESCE(t.state, '')::text AS trust_state, COALESCE(t.anchor_ids, '{}')::bigint[] AS trust_anchor_ids,
       COALESCE(t.block_id, 0)::bigint AS trust_block_id, COALESCE(t.anchor_epoch, 0)::bigint AS trust_epoch, t.computed_at AS trust_computed_at,
       (SELECT st.anchor_epoch FROM signature_settings st WHERE st.id)::bigint AS current_epoch,
       COALESCE(s.identity_id, 0)::bigint AS identity_id, COALESCE(si.subject, '')::text AS identity_subject,
       COALESCE(si.cert_sha256, ''::bytea)::bytea AS identity_cert_sha256, COALESCE(si.state, '')::text AS identity_state,
       (CASE WHEN sa.signature_id IS NOT NULL THEN o.raw END)::bytea AS issued_bundle,
       COALESCE(sa.served_as, '')::text AS served_as, COALESCE(sa.api_reason, '')::text AS served_reason
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256
  LEFT JOIN signing_identities si ON si.id = s.identity_id
  LEFT JOIN LATERAL (SELECT x.signature_id, x.served_as, x.api_reason
                       FROM signing_attestations x
                      WHERE s.identity_id IS NOT NULL AND x.signature_id = s.id
                      LIMIT 1) AS sa ON true
  LEFT JOIN LATERAL (SELECT x.signature_id, x.project_id, x.state, x.anchor_ids, x.block_id, x.anchor_epoch, x.computed_at
                       FROM signature_trust x
                      WHERE x.signature_id = s.id
                        AND (x.project_id IS NULL OR x.project_id::text = sqlc.arg(project_id)::text)
                      ORDER BY x.project_id NULLS LAST
                      LIMIT 1) AS t ON true
 WHERE (s.subject_sha256 = ANY(sqlc.arg(subjects)::bytea[]) OR s.subject_name = ANY(sqlc.arg(subject_names)::text[]))
   AND EXISTS (SELECT 1 FROM signature_attachments a WHERE a.signature_id = s.id AND a.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]))
 ORDER BY s.id;

-- name: SignatureViewAttachments :many
-- The attachments of signatures in the repositories asked.
SELECT repo_id, signature_id, location, COALESCE(item_id, 0)::bigint AS item_id, source, COALESCE(name, '')::text AS name, created_at
  FROM signature_attachments
 WHERE signature_id = ANY(sqlc.arg(signature_ids)::bigint[]) AND repo_id = ANY(sqlc.arg(repo_ids)::bigint[])
 ORDER BY signature_id, repo_id, location;

-- name: SignatureObjectHolders :many
-- The repositories that hold an attachment of a signature of the object (the permission of its download, R-SIG-034).
SELECT DISTINCT r.id AS repo_id, pr.key AS project_key, r.key AS repo_key, r.format
  FROM signatures s
  JOIN signature_attachments a ON a.signature_id = s.id
  JOIN repositories r ON r.id = a.repo_id
  JOIN project pr ON pr.id = r.project_id
 WHERE s.object_sha256 = sqlc.arg(sha256)::bytea
 ORDER BY pr.key, r.key;

-- name: SignatureDueRows :many
-- The due signatures whose time has come, by priority and then not_before, but those the run has in flight; with their object and
-- what the blobs table knows of their subject (subject_stored false: no blob has the digest). One job runs at a time (coalesce), so
-- nothing is locked across a check.
SELECT s.id, s.object_sha256, s.part, s.subject_sha256, COALESCE(s.subject_name, '')::text AS subject_name, s.role,
       s.content_sha256, COALESCE(s.content_locator, '')::text AS content_locator, s.state, s.signed_digest,
       COALESCE(s.digest_alg, '')::text AS digest_alg, s.priority, s.attempts, s.not_before,
       o.scheme, COALESCE(o.media_type, '')::text AS media_type, o.raw, o.item_blob, COALESCE(o.locator, '')::text AS locator, o.size,
       o.first_seen_at,
       (b.sha256 IS NOT NULL)::boolean AS subject_stored, b.sha512 AS subject_sha512, COALESCE(b.size, 0)::bigint AS subject_size,
       (b.created_at)::timestamptz AS subject_created_at
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256
  LEFT JOIN blobs b ON b.sha256 = s.subject_sha256
 WHERE s.due AND s.not_before <= now() AND NOT (s.id = ANY(sqlc.arg(in_flight)::bigint[]))
 ORDER BY s.priority, s.not_before, s.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: SignatureLockState :many
-- One signature's state, locked for the write of its verdict.
SELECT state FROM signatures WHERE id = sqlc.arg(id)::bigint FOR UPDATE;

-- name: SignatureSetReceived :exec
-- The receipt of a signature (R-SIG-007): the earlier of its object's and its subject's, as the check used it.
UPDATE signatures SET received_at = NULLIF(sqlc.arg(received_at)::text, '')::timestamptz
 WHERE id = sqlc.arg(id)::bigint AND received_at > NULLIF(sqlc.arg(received_at)::text, '')::timestamptz;

-- name: SignatureAddParts :execrows
-- The rows of parts 1 to parts-1 of an object for the subject and subject name of a part-0 row (R-SIG-003): the parts a detector could
-- not name, due at the part-0 row's priority.
INSERT INTO signatures (object_sha256, part, subject_sha256, subject_name, role, content_sha256, content_locator, signed_digest,
                        digest_alg, priority)
SELECT s.object_sha256, p.part::smallint, s.subject_sha256, s.subject_name, s.role, s.content_sha256, s.content_locator,
       CASE WHEN s.content_sha256 IS NULL AND o.scheme = 'npm-registry' THEN s.signed_digest END,
       CASE WHEN s.content_sha256 IS NULL AND o.scheme = 'npm-registry' THEN s.digest_alg END, s.priority
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256
 CROSS JOIN generate_series(1, sqlc.arg(parts)::int - 1) AS p(part)
 WHERE s.id = sqlc.arg(id)::bigint AND s.part = 0
ON CONFLICT (object_sha256, part, subject_sha256, subject_name) DO NOTHING;

-- name: SignatureCopyPartAttachments :execrows
-- The attachments of a part-0 row, made for the rows of the object's other parts against the same subject and subject name.
INSERT INTO signature_attachments (repo_id, signature_id, location, item_id, source, name)
SELECT a.repo_id, n.id, a.location, a.item_id, a.source, a.name
  FROM signatures s
  JOIN signature_attachments a ON a.signature_id = s.id
  JOIN signatures n ON n.object_sha256 = s.object_sha256 AND n.subject_sha256 = s.subject_sha256
                   AND n.subject_name IS NOT DISTINCT FROM s.subject_name AND n.part BETWEEN 1 AND sqlc.arg(parts)::int - 1
 WHERE s.id = sqlc.arg(id)::bigint
ON CONFLICT (repo_id, signature_id, location) DO NOTHING;

-- name: SignatureInvalidDetails :one
-- What the event signature.invalid names (R-SIG-039): the object, the subject, the scheme, the role, the reason, the signer, the
-- repositories that hold an attachment (at most max_rows, as project/repository) and their count.
SELECT s.object_sha256, s.subject_sha256, COALESCE(s.subject_name, '')::text AS subject_name, o.scheme, s.role,
       COALESCE(s.reason, '')::text AS reason, s.signer,
       ARRAY(SELECT DISTINCT pr.key || '/' || r.key FROM signature_attachments a
               JOIN repositories r ON r.id = a.repo_id JOIN project pr ON pr.id = r.project_id
              WHERE a.signature_id = s.id ORDER BY 1 LIMIT sqlc.arg(max_rows)::int)::text[] AS repositories,
       (SELECT count(DISTINCT a.repo_id) FROM signature_attachments a WHERE a.signature_id = s.id)::bigint AS repository_count
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256
 WHERE s.id = sqlc.arg(id)::bigint;

-- name: SignatureUpstreamKeys :many
-- The keys the upstreams of the repositories that attach the signatures announce, for the integrity check alone (R-SIG-006): a
-- Debian remote's deb.upstreamKeys, and the blob of an npm remote's stored keys document at keys_path.
SELECT DISTINCT a.signature_id, r.id AS repo_id, r.format,
       COALESCE(CASE WHEN r.format = 'deb' THEN r.config -> 'deb' ->> 'upstreamKeys' END, '')::text AS deb_keys,
       (SELECT e.blob_sha256 FROM remote_cache_entries e
         WHERE r.format = 'npm' AND e.repo_id = r.id AND e.path = sqlc.arg(keys_path)::text AND e.kind = 'metadata'
           AND e.upstream_gone_at IS NULL
         ORDER BY e.fetched_at DESC LIMIT 1)::bytea AS npm_keys_blob
  FROM signature_attachments a
  JOIN repositories r ON r.id = a.repo_id
 WHERE a.signature_id = ANY(sqlc.arg(signature_ids)::bigint[]) AND r.class = 'remote' AND r.format IN ('deb', 'npm')
 ORDER BY a.signature_id, r.id;

-- name: SignatureDiscovery :one
-- Discovery's cursor and the start of its pass (R-SIG-011).
SELECT discovery_after, discovery_pass_at FROM signature_settings WHERE id;

-- name: SignatureSetDiscovery :exec
-- Advances discovery's cursor; a restart begins a new pass from the first item.
UPDATE signature_settings
   SET discovery_after = sqlc.arg(after)::bigint,
       discovery_pass_at = CASE WHEN sqlc.arg(restart)::boolean THEN now() ELSE discovery_pass_at END
 WHERE id;

-- name: SignatureRetryFailed :execrows
-- Once a day the failed signatures are due again at the priority of discovery, with their attempts anew (R-SIG-011).
WITH slot AS (
    UPDATE signature_settings SET failed_retried_at = now()
     WHERE id AND (failed_retried_at IS NULL OR failed_retried_at <= now() - interval '1 day')
    RETURNING 1)
UPDATE signatures
   SET due = true, priority = 3, attempts = 0, not_before = now()
 WHERE state = 'failed' AND NOT due AND EXISTS (SELECT 1 FROM slot);

-- name: SignatureStats :one
-- What the worker's gauges show after a run (R-SIG-039): the due rows by priority, the oldest due row's receipt, the invalid
-- signatures with an attachment, the trust rows under an older epoch, and the public root the task stored.
SELECT (SELECT count(*) FROM signatures WHERE due AND priority = 0)::bigint AS due0,
       (SELECT count(*) FROM signatures WHERE due AND priority = 1)::bigint AS due1,
       (SELECT count(*) FROM signatures WHERE due AND priority = 2)::bigint AS due2,
       (SELECT count(*) FROM signatures WHERE due AND priority = 3)::bigint AS due3,
       COALESCE((SELECT extract(epoch FROM now() - min(received_at)) FROM signatures WHERE due), 0)::double precision AS oldest_pending,
       (SELECT count(*) FROM signatures s WHERE s.state = 'invalid'
           AND EXISTS (SELECT 1 FROM signature_attachments a WHERE a.signature_id = s.id))::bigint AS invalid,
       (SELECT count(*) FROM signature_trust t, signature_settings st WHERE st.id AND t.anchor_epoch < st.anchor_epoch)::bigint AS stale,
       (SELECT tr.fetched_at FROM signature_trust_roots tr WHERE tr.name = 'trusted_root.json') AS root_fetched_at;

-- name: SignatureTrustBatch :many
-- The next signatures in id order after after_id whose state the trust step judges (valid, no_key) and whose installation row is
-- missing or was computed under an older epoch than epoch, with the stored fields the judgement reads. A signature Quader made itself
-- (identity_id set) gets no trust row (R-SIG-054).
SELECT s.id, o.scheme, s.state, s.key_ids, s.signed_digest, COALESCE(s.digest_alg, '')::text AS digest_alg, s.evidence, s.signer,
       s.received_at, s.proven_at, COALESCE(s.time_proof, '')::text AS time_proof
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256
 WHERE s.id > sqlc.arg(after_id)::bigint AND s.state IN ('valid', 'no_key') AND s.identity_id IS NULL
   AND NOT EXISTS (SELECT 1 FROM signature_trust t
                    WHERE t.signature_id = s.id AND t.project_id IS NULL AND t.anchor_epoch >= sqlc.arg(epoch)::bigint)
 ORDER BY s.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: SignatureTrustOf :many
-- The stored fields the judgement reads of the signatures named.
SELECT s.id, o.scheme, s.state, s.key_ids, s.signed_digest, COALESCE(s.digest_alg, '')::text AS digest_alg, s.evidence, s.signer,
       s.received_at, s.proven_at, COALESCE(s.time_proof, '')::text AS time_proof
  FROM signatures s
  JOIN signature_objects o ON o.sha256 = s.object_sha256
 WHERE s.id = ANY(sqlc.arg(ids)::bigint[])
 ORDER BY s.id;

-- name: SignatureReachingProjects :many
-- The projects among project_ids that reach the signatures (R-SIG-008): an attachment in one of their repositories, or in a
-- repository of the project upstream_key that one of their virtual repositories lists.
SELECT a.signature_id, r.project_id::text AS project_id
  FROM signature_attachments a
  JOIN repositories r ON r.id = a.repo_id
 WHERE a.signature_id = ANY(sqlc.arg(signature_ids)::bigint[]) AND r.project_id::text = ANY(sqlc.arg(project_ids)::text[])
UNION
SELECT a.signature_id, v.project_id::text
  FROM signature_attachments a
  JOIN repositories m ON m.id = a.repo_id
  JOIN project up ON up.id = m.project_id AND up.key = sqlc.arg(upstream_key)::text
  JOIN virtual_members vm ON vm.member_id = m.id
  JOIN repositories v ON v.id = vm.virtual_id
 WHERE a.signature_id = ANY(sqlc.arg(signature_ids)::bigint[]) AND v.project_id::text = ANY(sqlc.arg(project_ids)::text[])
 ORDER BY 1, 2;

-- name: SignatureTrustUpsert :exec
-- The trust rows of a batch under one epoch. A row of a signature, a project or a block that went meanwhile is not written (the
-- block's reference is then empty).
INSERT INTO signature_trust (signature_id, project_id, state, anchor_ids, block_id, anchor_epoch, computed_at)
SELECT t.signature_id, NULLIF(t.project_id, '')::uuid, t.state, t.anchor_ids::bigint[],
       (SELECT b.id FROM signature_blocks b WHERE b.id = t.block_id), sqlc.arg(epoch)::bigint, now()
  FROM (SELECT unnest(sqlc.arg(signature_ids)::bigint[]) AS signature_id, unnest(sqlc.arg(project_ids)::text[]) AS project_id,
               unnest(sqlc.arg(states)::text[]) AS state, unnest(sqlc.arg(anchor_ids)::text[]) AS anchor_ids,
               unnest(sqlc.arg(block_ids)::bigint[]) AS block_id) AS t
 WHERE EXISTS (SELECT 1 FROM signatures s WHERE s.id = t.signature_id)
   AND (t.project_id = '' OR EXISTS (SELECT 1 FROM project p WHERE p.id::text = t.project_id))
ON CONFLICT (signature_id, project_id) DO UPDATE
   SET state = EXCLUDED.state, anchor_ids = EXCLUDED.anchor_ids, block_id = EXCLUDED.block_id, anchor_epoch = EXCLUDED.anchor_epoch,
       computed_at = EXCLUDED.computed_at;

-- name: SignatureTrustPrune :execrows
-- The project rows of the signatures that the batch does not write again: a project that no longer reaches a signature, or whose
-- anchors no longer apply.
DELETE FROM signature_trust t
 WHERE t.signature_id = ANY(sqlc.arg(signature_ids)::bigint[]) AND t.project_id IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM (SELECT unnest(sqlc.arg(keep_signature_ids)::bigint[]) AS signature_id,
                                         unnest(sqlc.arg(keep_project_ids)::text[]) AS project_id) AS k
                    WHERE k.signature_id = t.signature_id AND k.project_id = t.project_id::text);

-- name: SignatureTrustDelete :execrows
-- Every trust row of the signatures named (an integrity verdict without a trust step).
DELETE FROM signature_trust WHERE signature_id = ANY(sqlc.arg(signature_ids)::bigint[]);

-- name: SignatureTrustDropOther :execrows
-- At most row_limit trust rows of signatures in a state without a trust step (R-SIG-008: anchors never repair the bytes).
DELETE FROM signature_trust t
 WHERE t.signature_id IN (SELECT x.signature_id FROM signature_trust x JOIN signatures s ON s.id = x.signature_id
                           WHERE s.state NOT IN ('valid', 'no_key') LIMIT sqlc.arg(row_limit)::int);

-- name: SignatureSetIntegrity :execrows
-- The integrity verdict of a no_key signature an anchor's key decided (R-SIG-012): valid, or invalid with the reason.
UPDATE signatures SET state = sqlc.arg(state)::text, reason = NULLIF(sqlc.arg(reason)::text, ''), checked_at = now()
 WHERE id = sqlc.arg(id)::bigint AND state = 'no_key';

-- name: SignatureTrustBacklog :one
-- Whether the trust step has work: a valid or no_key signature without an installation row at the epoch, or a trust row of a
-- signature in another state.
SELECT (EXISTS (SELECT 1 FROM signatures s
                 WHERE s.state IN ('valid', 'no_key') AND s.identity_id IS NULL
                   AND NOT EXISTS (SELECT 1 FROM signature_trust t
                                    WHERE t.signature_id = s.id AND t.project_id IS NULL AND t.anchor_epoch >= st.anchor_epoch))
        OR EXISTS (SELECT 1 FROM signature_trust t JOIN signatures s ON s.id = t.signature_id
                    WHERE s.state NOT IN ('valid', 'no_key')))::boolean AS pending
  FROM signature_settings st WHERE st.id;

-- name: SignatureTrustRoots :many
-- The TUF targets the task stored.
SELECT name, document, tuf_version_number, fetched_at FROM signature_trust_roots ORDER BY name;

-- name: SignatureStoreTrustRoot :exec
-- Stores a TUF target the task fetched.
INSERT INTO signature_trust_roots (name, document, tuf_version_number, fetched_at)
VALUES (sqlc.arg(name)::text, sqlc.arg(document)::bytea, sqlc.arg(tuf_version_number)::bigint, now())
ON CONFLICT (name) DO UPDATE SET document = EXCLUDED.document, tuf_version_number = EXCLUDED.tuf_version_number, fetched_at = EXCLUDED.fetched_at;

-- name: SignatureTrustRootSucceeded :exec
-- A run of the task that succeeded clears the last failure.
UPDATE signature_settings SET trust_root_error = NULL, trust_root_error_at = NULL WHERE id;

-- name: SignatureTrustRootFailed :one
-- A run of the task that failed: the failure is kept, and report says whether the event signature.trust_root.failed is due (at most
-- once a day, R-SIG-039).
UPDATE signature_settings
   SET trust_root_error = left(sqlc.arg(detail)::text, 2000), trust_root_error_at = now(),
       trust_root_reported_at = CASE WHEN trust_root_reported_at IS NULL OR trust_root_reported_at <= now() - interval '1 day'
                                     THEN now() ELSE trust_root_reported_at END
 WHERE id
RETURNING (trust_root_reported_at = now())::boolean AS report;

-- name: SignatureJournalSwitchOff :execrows
-- The replay of a switch turned off: off again.
UPDATE signature_settings SET project_anchors = false, anchor_epoch = anchor_epoch + 1, version = version + 1, updated_at = now()
 WHERE id AND project_anchors;

-- name: SignatureJournalBlock :execrows
-- The replay of a block: the signer blocked again, unless a block of it is there.
INSERT INTO signature_blocks (kind, value, issuer, reason)
VALUES (sqlc.arg(kind)::text, sqlc.arg(value)::text, NULLIF(sqlc.arg(issuer)::text, ''), sqlc.arg(reason)::text)
ON CONFLICT (kind, value, issuer) DO NOTHING;

-- name: SignatureJournalRemoveAnchor :execrows
-- The replay of a removed anchor: the anchor of the id, scope and key goes again.
DELETE FROM signature_anchors
 WHERE id = sqlc.arg(id)::bigint AND scope = sqlc.arg(scope)::text AND key = sqlc.arg(key)::text;

-- name: SignaturePublicProvenance :many
-- The provenance bundles among stored files of a repository that npm may be served (R-SIG-028, Q-SIG-35): a signature attached at the
-- path, valid, whose installation trust row is not blocked and names the shipped anchor sigstore-public while it is switched on.
SELECT DISTINCT ON (a.location) a.location::text AS path, COALESCE(s.predicate_type, '')::text AS predicate_type
  FROM signature_attachments a
  JOIN signatures s ON s.id = a.signature_id
  JOIN signature_trust t ON t.signature_id = s.id AND t.project_id IS NULL
  JOIN signature_anchors an ON an.scope = 'shipped' AND an.key = 'sigstore-public' AND an.enabled
 WHERE a.repo_id = sqlc.arg(repo_id)::bigint AND a.location = ANY(sqlc.arg(paths)::text[]) AND s.state = 'valid'
   AND t.state <> 'blocked' AND an.id = ANY(t.anchor_ids)
 ORDER BY a.location, s.id;

-- name: SignatureVersionBlobs :many
-- The blobs of the files of some versions of a package in some repositories, in one statement for a page of the version list (R-SIG-034):
-- the subjects whose counted signatures give each version its badge, and the paths a repository's gate classifies (R-SIG-073). A version
-- a repository holds as a document and not as files (a remote's OCI tag, R-OCI-036, whose document is the manifest the tag names)
-- answers the document's blob, marked as one and without a path (Q-SIG-219).
SELECT pv.version::text AS version, pv.display_version::text AS display_version, p.repo_id::bigint AS repo_id, i.blob_sha256,
       i.path::text AS path, false::boolean AS document
  FROM packages p
  JOIN package_versions pv ON pv.package_id = p.id AND pv.trashed_at IS NULL
  JOIN items i ON i.version_id = pv.id AND i.kind = 'file' AND i.trashed_at IS NULL
 WHERE p.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]) AND p.name = sqlc.arg(name)::text
   AND (pv.version = ANY(sqlc.arg(versions)::text[]) OR pv.display_version = ANY(sqlc.arg(versions)::text[]))
UNION ALL
SELECT l.version::text, l.version::text, p.repo_id::bigint, l.document_sha256, ''::text, true::boolean
  FROM packages p
  JOIN package_version_listing l ON l.package_id = p.id AND l.document_sha256 IS NOT NULL
 WHERE p.repo_id = ANY(sqlc.arg(repo_ids)::bigint[]) AND p.name = sqlc.arg(name)::text
   AND l.version = ANY(sqlc.arg(versions)::text[]);
