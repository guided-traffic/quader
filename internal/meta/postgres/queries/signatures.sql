-- The statements of the signature store (signatures.md R-SIG-003, R-SIG-004, R-SIG-010, R-SIG-011; internal/signatures/store). Digests
-- are bytea of 32 bytes; every parameter carries its type, because the main pool may run in pgx's Exec mode, which sends none
-- (sqlc.yaml); a time travels as RFC 3339 text, '' for none. A batch travels as parallel arrays, one element per row, in one statement; an empty text or bytea element stands for
-- NULL where the column takes none.

-- name: SignatureStoredPaths :many
-- What the paths of one repository hold now: a live file's blob, size and item, or a remote's stored document (no item). The commit
-- hook resolves the paths a candidate names here and in no other repository (Q-SIG-47).
SELECT i.path::text AS path, i.blob_sha256::bytea AS blob_sha256, i.size::bigint AS size, i.id::bigint AS item_id
  FROM items i
 WHERE i.repo_id = sqlc.arg(repo_id)::bigint AND i.path = ANY(sqlc.arg(paths)::text[]) AND i.kind = 'file' AND i.trashed_at IS NULL
UNION ALL
SELECT DISTINCT ON (e.path) e.path::text, e.blob_sha256::bytea, b.size::bigint, 0::bigint
  FROM remote_cache_entries e
  JOIN blobs b ON b.sha256 = e.blob_sha256
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.path = ANY(sqlc.arg(paths)::text[]) AND e.kind = 'metadata'
   AND e.upstream_gone_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM items x WHERE x.repo_id = e.repo_id AND x.path = e.path AND x.kind = 'file' AND x.trashed_at IS NULL)
 ORDER BY 1;

-- name: SignatureInsertObjects :exec
-- The originals of a batch, once per their own SHA-256: an object that is there stays as it was first seen (R-SIG-003).
INSERT INTO signature_objects (sha256, scheme, media_type, raw, item_blob, locator, size)
SELECT o.sha256, o.scheme, NULLIF(o.media_type, ''), NULLIF(o.raw, ''::bytea), NULLIF(o.item_blob, ''::bytea), NULLIF(o.locator, ''), o.size
  FROM (SELECT unnest(sqlc.arg(sha256s)::bytea[]) AS sha256, unnest(sqlc.arg(schemes)::text[]) AS scheme,
               unnest(sqlc.arg(media_types)::text[]) AS media_type, unnest(sqlc.arg(raws)::bytea[]) AS raw,
               unnest(sqlc.arg(item_blobs)::bytea[]) AS item_blob, unnest(sqlc.arg(locators)::text[]) AS locator,
               unnest(sqlc.arg(sizes)::bigint[]) AS size) AS o
ON CONFLICT (sha256) DO NOTHING;

-- name: SignatureInsertRows :exec
-- The signatures of a batch, once per object, part, subject and subject name; one that is there keeps its verdict (R-SIG-003). A row
-- that waits behind the bounds of R-SIG-013 (an object larger than signatures.max_object_size) is born malformed and not due (Q-SIG-49).
INSERT INTO signatures (object_sha256, part, subject_sha256, subject_name, role, content_sha256, content_locator, signed_digest,
                        digest_alg, state, reason, due, priority)
SELECT s.object_sha256, s.part, s.subject_sha256, NULLIF(s.subject_name, ''), s.role, NULLIF(s.content_sha256, ''::bytea),
       NULLIF(s.content_locator, ''), NULLIF(s.signed_digest, ''::bytea), NULLIF(s.digest_alg, ''),
       CASE WHEN s.too_large THEN 'malformed' ELSE 'pending' END, CASE WHEN s.too_large THEN 'limits_exceeded' END,
       NOT s.too_large, s.priority
  FROM (SELECT unnest(sqlc.arg(objects)::bytea[]) AS object_sha256, unnest(sqlc.arg(parts)::smallint[]) AS part,
               unnest(sqlc.arg(subjects)::bytea[]) AS subject_sha256, unnest(sqlc.arg(subject_names)::text[]) AS subject_name,
               unnest(sqlc.arg(roles)::text[]) AS role, unnest(sqlc.arg(content_sha256s)::bytea[]) AS content_sha256,
               unnest(sqlc.arg(content_locators)::text[]) AS content_locator, unnest(sqlc.arg(signed_digests)::bytea[]) AS signed_digest,
               unnest(sqlc.arg(digest_algs)::text[]) AS digest_alg, unnest(sqlc.arg(too_large)::boolean[]) AS too_large,
               unnest(sqlc.arg(priorities)::smallint[]) AS priority) AS s
ON CONFLICT (object_sha256, part, subject_sha256, subject_name) DO NOTHING;

-- name: SignatureIDs :many
-- The ids of the signatures of a batch, in the order of the arrays (ordinality).
SELECT k.ord::bigint AS ord, s.id::bigint AS id
  FROM (SELECT unnest(sqlc.arg(objects)::bytea[]) AS object_sha256, unnest(sqlc.arg(parts)::smallint[]) AS part,
               unnest(sqlc.arg(subjects)::bytea[]) AS subject_sha256, unnest(sqlc.arg(subject_names)::text[]) AS subject_name,
               generate_subscripts(sqlc.arg(objects)::bytea[], 1) AS ord) AS k
  JOIN signatures s ON s.object_sha256 = k.object_sha256 AND s.part = k.part AND s.subject_sha256 = k.subject_sha256
                   AND s.subject_name IS NOT DISTINCT FROM NULLIF(k.subject_name, '')
 ORDER BY k.ord;

-- name: SignatureReplaceAttachments :exec
-- The attachments a commit or a fill no longer makes (R-SIG-004): those of the carrier item, those of a stored document (its path and
-- every path#pointer below it), and, at a location the batch names, those of the same object against another subject (the file a
-- signature file stands beside was replaced), each unless the batch makes it again. An attachment of another part of the same object
-- and subject stays: a batch that names an object by its path names its first part only. Only attachments of the batch's own sources
-- go (Q-SIG-217): a signature posted through the management API at the archive's item (upload) outlives the detector's batch of that
-- item, and an attachment of a signature Quader made itself (issued, R-SIG-054) is no batch's: the job signing writes and removes it.
DELETE FROM signature_attachments a
 USING signatures s
 WHERE a.repo_id = sqlc.arg(repo_id)::bigint AND s.id = a.signature_id AND a.source = ANY(sqlc.arg(sources)::text[])
   AND ((sqlc.arg(item_id)::bigint <> 0 AND a.item_id = sqlc.arg(item_id)::bigint)
        OR (sqlc.arg(document)::text <> '' AND a.item_id IS NULL
            AND (a.location = sqlc.arg(document)::text OR starts_with(a.location, sqlc.arg(document)::text || '#')))
        OR EXISTS (SELECT 1
                     FROM (SELECT unnest(sqlc.arg(locations)::text[]) AS location, unnest(sqlc.arg(objects)::bytea[]) AS object_sha256,
                                  unnest(sqlc.arg(subjects)::bytea[]) AS subject_sha256) AS n
                    WHERE n.location = a.location AND n.object_sha256 = s.object_sha256 AND n.subject_sha256 <> s.subject_sha256))
   AND NOT EXISTS (SELECT 1 FROM (SELECT unnest(sqlc.arg(keep_ids)::bigint[]) AS id, unnest(sqlc.arg(keep_locations)::text[]) AS location) AS k
                    WHERE k.id = a.signature_id AND k.location = a.location);

-- name: SignatureUpsertAttachments :exec
-- The attachments of a batch (R-SIG-004).
INSERT INTO signature_attachments (repo_id, signature_id, location, item_id, source, name)
SELECT sqlc.arg(repo_id)::bigint, a.id, a.location, NULLIF(a.item_id, 0), a.source, NULLIF(a.name, '')
  FROM (SELECT unnest(sqlc.arg(ids)::bigint[]) AS id, unnest(sqlc.arg(locations)::text[]) AS location,
               unnest(sqlc.arg(item_ids)::bigint[]) AS item_id, unnest(sqlc.arg(sources)::text[]) AS source,
               unnest(sqlc.arg(names)::text[]) AS name) AS a
ON CONFLICT (repo_id, signature_id, location) DO UPDATE
   SET item_id = EXCLUDED.item_id, source = EXCLUDED.source, name = EXCLUDED.name;

-- name: SignatureSubjectArrived :execrows
-- A commit of a subject makes the signatures that wait for it due, with the priority of a subject that arrived after its signature
-- (R-SIG-011).
UPDATE signatures
   SET due = true, priority = 2, not_before = now()
 WHERE subject_sha256 = sqlc.arg(subject)::bytea AND state = 'pending' AND reason = 'subject_missing';

-- name: SignatureUpsertOrigin :exec
-- The origin of an uploaded item (R-SIG-010, Q-SIG-40): the principal, the credential's ID and, for a CI token, the claims of its
-- issuance record as they are now. A replaced item takes its new uploader.
INSERT INTO artifact_origins (item_id, principal_kind, principal_name, token_id, ci)
SELECT sqlc.arg(item_id)::bigint, sqlc.arg(principal_kind)::text, sqlc.arg(principal_name)::text,
       NULLIF(sqlc.arg(token_id)::text, '')::uuid,
       (SELECT jsonb_strip_nulls(jsonb_build_object(
                'issuer', ci.name, 'project_path', t.project_path, 'project_id', t.project_id, 'namespace_path',
                t.claims ->> 'namespace_path', 'pipeline_id', t.pipeline_id, 'job_id', t.job_id, 'ref', t.ref, 'ref_type', t.ref_type,
                'ref_protected', t.ref_protected, 'pipeline_source', t.pipeline_source, 'environment', t.environment, 'sha', t.sha,
                'user_login', t.user_login))
          FROM ci_token_issuances t
          JOIN ci_issuers ci ON ci.id = t.issuer_id
         WHERE sqlc.arg(token_id)::text <> '' AND t.token_id = NULLIF(sqlc.arg(token_id)::text, '')::uuid)
ON CONFLICT (item_id) DO UPDATE
   SET principal_kind = EXCLUDED.principal_kind, principal_name = EXCLUDED.principal_name, token_id = EXCLUDED.token_id,
       ci = EXCLUDED.ci, uploaded_at = now();

-- name: SignatureOrigin :many
-- The origin of an item, when an upload recorded one.
SELECT principal_kind, principal_name, COALESCE(token_id::text, '')::text AS token_id, ci, uploaded_at
  FROM artifact_origins WHERE item_id = sqlc.arg(item_id)::bigint;

-- name: SignatureObject :many
-- One signature object.
SELECT sha256, scheme, COALESCE(media_type, '')::text AS media_type, raw, item_blob, COALESCE(locator, '')::text AS locator, size,
       first_seen_at
  FROM signature_objects WHERE sha256 = sqlc.arg(sha256)::bytea;

-- name: SignatureByID :many
-- One signature.
SELECT id, object_sha256, part, subject_sha256, COALESCE(subject_name, '')::text AS subject_name, role,
       COALESCE(predicate_type, '')::text AS predicate_type, content_sha256, COALESCE(content_locator, '')::text AS content_locator,
       state, COALESCE(reason, '')::text AS reason, signer, key_ids, signed_digest, COALESCE(digest_alg, '')::text AS digest_alg,
       claimed_at, proven_at, COALESCE(time_proof, '')::text AS time_proof, received_at, checked_at, due, priority, attempts, not_before
  FROM signatures WHERE id = sqlc.arg(id)::bigint;

-- name: SignaturesBySubject :many
-- The signatures of a subject, in id order.
SELECT id, object_sha256, part, subject_sha256, COALESCE(subject_name, '')::text AS subject_name, role,
       COALESCE(predicate_type, '')::text AS predicate_type, content_sha256, COALESCE(content_locator, '')::text AS content_locator,
       state, COALESCE(reason, '')::text AS reason, signer, key_ids, signed_digest, COALESCE(digest_alg, '')::text AS digest_alg,
       claimed_at, proven_at, COALESCE(time_proof, '')::text AS time_proof, received_at, checked_at, due, priority, attempts, not_before
  FROM signatures WHERE subject_sha256 = sqlc.arg(subject)::bytea
 ORDER BY id;

-- name: SignaturesDue :many
-- The due signatures whose time has come, by priority and then not_before, locked for the job and skipped by another (R-SIG-011).
SELECT id, object_sha256, part, subject_sha256, COALESCE(subject_name, '')::text AS subject_name, role,
       COALESCE(predicate_type, '')::text AS predicate_type, content_sha256, COALESCE(content_locator, '')::text AS content_locator,
       state, COALESCE(reason, '')::text AS reason, signer, key_ids, signed_digest, COALESCE(digest_alg, '')::text AS digest_alg,
       claimed_at, proven_at, COALESCE(time_proof, '')::text AS time_proof, received_at, checked_at, due, priority, attempts, not_before
  FROM signatures
 WHERE due AND not_before <= now()
 ORDER BY priority, not_before, id
 LIMIT sqlc.arg(row_limit)::int
   FOR UPDATE SKIP LOCKED;

-- name: SignatureAttachments :many
-- Where one signature is attached, by repository and location.
SELECT repo_id, signature_id, location, COALESCE(item_id, 0)::bigint AS item_id, source, COALESCE(name, '')::text AS name, created_at
  FROM signature_attachments WHERE signature_id = sqlc.arg(signature_id)::bigint
 ORDER BY repo_id, location;

-- name: SignatureAttachmentsUnder :many
-- The attachments of one repository under a package or image name, with their signatures' subjects (what a version page reads).
SELECT a.repo_id, a.signature_id, a.location, COALESCE(a.item_id, 0)::bigint AS item_id, a.source, COALESCE(a.name, '')::text AS name,
       a.created_at, s.subject_sha256
  FROM signature_attachments a
  JOIN signatures s ON s.id = a.signature_id
 WHERE a.repo_id = sqlc.arg(repo_id)::bigint AND a.name = sqlc.arg(name)::text
 ORDER BY a.location, a.signature_id;

-- name: SignatureSetResult :execrows
-- The read and the integrity check of one signature (R-SIG-005, R-SIG-006) with the evidence the trust step judges it by (Q-SIG-109):
-- the row is no longer due.
UPDATE signatures
   SET state = sqlc.arg(state)::text, evidence = NULLIF(sqlc.arg(evidence)::bytea, ''::bytea), reason = NULLIF(sqlc.arg(reason)::text, ''), role = sqlc.arg(role)::text,
       predicate_type = NULLIF(sqlc.arg(predicate_type)::text, ''), signer = sqlc.narg(signer)::jsonb,
       key_ids = sqlc.arg(key_ids)::text[], signed_digest = NULLIF(sqlc.arg(signed_digest)::bytea, ''::bytea),
       digest_alg = NULLIF(sqlc.arg(digest_alg)::text, ''), claimed_at = NULLIF(sqlc.arg(claimed_at)::text, '')::timestamptz,
       proven_at = NULLIF(sqlc.arg(proven_at)::text, '')::timestamptz, time_proof = NULLIF(sqlc.arg(time_proof)::text, ''),
       checked_at = now(), due = false, attempts = 0
 WHERE id = sqlc.arg(id)::bigint;

-- name: SignatureDefer :execrows
-- A check that could not run: one attempt spent, due again at not_before; or, the subject not stored yet, pending with the reason
-- subject_missing and not due until the subject arrives (R-SIG-011).
UPDATE signatures
   SET attempts = attempts + sqlc.arg(spend)::int, not_before = COALESCE(NULLIF(sqlc.arg(not_before)::text, '')::timestamptz, now()), due = sqlc.arg(due)::boolean,
       state = sqlc.arg(state)::text, reason = NULLIF(sqlc.arg(reason)::text, '')
 WHERE id = sqlc.arg(id)::bigint;

-- name: SignatureRemoveOrphans :execrows
-- The signatures without an attachment, at most row_limit of them (R-SIG-011).
DELETE FROM signatures s
 WHERE s.id IN (SELECT x.id FROM signatures x
                 WHERE NOT EXISTS (SELECT 1 FROM signature_attachments a WHERE a.signature_id = x.id)
                 ORDER BY x.id LIMIT sqlc.arg(row_limit)::int);

-- name: SignatureRemoveOrphanObjects :execrows
-- The signature objects without a signature, at most row_limit of them (R-SIG-011).
DELETE FROM signature_objects o
 WHERE o.sha256 IN (SELECT x.sha256 FROM signature_objects x
                     WHERE NOT EXISTS (SELECT 1 FROM signatures s WHERE s.object_sha256 = x.sha256)
                     ORDER BY x.sha256 LIMIT sqlc.arg(row_limit)::int);

-- name: SignatureDetach :execrows
-- Removes the attachments at locations of a repository: a signature a client posted and removed again (R-SIG-031). The job removes a
-- signature that loses its last attachment (R-SIG-011).
DELETE FROM signature_attachments
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND location = ANY(sqlc.arg(locations)::text[]);
-- name: SignatureDiscoverItems :many
-- Discovery's batch (R-SIG-011): the live files after the cursor in item order, with their repository, that a client or a fill stored
-- (a file Quader derived carries no signature of a publisher, Q-SIG-52). A system repository of the project upstream (the analysers'
-- databases, R-REPO-070) holds the product's own files and is not asked: its files cost discovery nothing. The referrer manifest of an
-- attestation of Quader's own (signing_attestations.referrer, R-SIG-054) is not asked either: Quader checks no signature it made itself.
SELECT i.id, i.path::text AS path, i.blob_sha256, i.size::bigint AS size, COALESCE(i.media_type, '')::text AS media_type,
       COALESCE(i.upstream_url, '')::text AS upstream_url, r.id AS repo_id, r.key AS repo_key, r.format, r.class,
       pr.key AS project_key
  FROM items i
  JOIN repositories r ON r.id = i.repo_id
  JOIN project pr ON pr.id = r.project_id
 WHERE i.id > sqlc.arg(after)::bigint AND i.kind = 'file' AND i.trashed_at IS NULL AND NOT i.generated
   AND NOT EXISTS (SELECT 1 FROM upstream_repository u WHERE u.repository_id = r.id AND u.kind = 'system')
   AND NOT EXISTS (SELECT 1 FROM signing_attestations sa WHERE sa.repo_id = i.repo_id AND sa.referrer = i.blob_sha256)
 ORDER BY i.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: SignatureInsertIssuedObject :exec
-- The original of a signature Quader made itself (R-SIG-054): npm's registry signature entry, {"keyid", "sig"}, once per its SHA-256.
INSERT INTO signature_objects (sha256, scheme, media_type, raw, size)
VALUES (sqlc.arg(sha256)::bytea, sqlc.arg(scheme)::text, NULLIF(sqlc.arg(media_type)::text, ''), sqlc.arg(raw)::bytea,
        octet_length(sqlc.arg(raw)::bytea))
ON CONFLICT (sha256) DO NOTHING;

-- name: SignatureInsertIssued :one
-- The signature row of a signature Quader made itself (R-SIG-054): valid as written and never due, because Quader checks no signature
-- it made (Q-SIG-31's rule); identity_id names the identity that made it. ECDSA signs with a fresh nonce, so the object is new and the
-- row too; one that is there (the same bytes again) answers its id. An attestation also carries its predicate type, the signer its
-- certificate names and the moment it claims (claimed_at, '' for none).
WITH ins AS (
    INSERT INTO signatures (object_sha256, part, subject_sha256, subject_name, role, predicate_type, signed_digest, digest_alg, state,
                            key_ids, due, checked_at, identity_id, signer, claimed_at)
    VALUES (sqlc.arg(object_sha256)::bytea, 0, sqlc.arg(subject_sha256)::bytea, NULLIF(sqlc.arg(subject_name)::text, ''),
            sqlc.arg(role)::text, NULLIF(sqlc.arg(predicate_type)::text, ''), NULLIF(sqlc.arg(signed_digest)::bytea, ''::bytea),
            NULLIF(sqlc.arg(digest_alg)::text, ''), 'valid', sqlc.arg(key_ids)::text[], false, now(), sqlc.arg(identity_id)::bigint,
            sqlc.narg(signer)::jsonb, NULLIF(sqlc.arg(claimed_at)::text, '')::timestamptz)
    ON CONFLICT (object_sha256, part, subject_sha256, subject_name) DO NOTHING
    RETURNING id)
SELECT id FROM ins
UNION ALL
SELECT s.id FROM signatures s
 WHERE s.object_sha256 = sqlc.arg(object_sha256)::bytea AND s.part = 0 AND s.subject_sha256 = sqlc.arg(subject_sha256)::bytea
   AND s.subject_name IS NOT DISTINCT FROM NULLIF(sqlc.arg(subject_name)::text, '')
LIMIT 1;

-- name: SignatureAttachIssued :exec
-- The attachment of a signature Quader made itself to the item it signs, in the item's repository (R-SIG-054): it goes with the item.
INSERT INTO signature_attachments (repo_id, signature_id, location, item_id, source, name)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(signature_id)::bigint, sqlc.arg(location)::text, sqlc.arg(item_id)::bigint, 'issued',
        NULLIF(sqlc.arg(name)::text, ''))
ON CONFLICT (repo_id, signature_id, location) DO UPDATE SET item_id = EXCLUDED.item_id, source = 'issued', name = EXCLUDED.name;

-- name: SignatureDetachIssuedExcept :execrows
-- The attachments of the scheme's signatures Quader made of an item but the one just recorded (R-SIG-053): an attestation made again
-- replaces the item's earlier one, whichever identity made that. The job signature-verify removes a signature that loses its last
-- attachment (R-SIG-011).
DELETE FROM signature_attachments a
 USING signatures s, signature_objects o
 WHERE a.item_id = sqlc.arg(item_id)::bigint AND a.source = 'issued' AND a.signature_id <> sqlc.arg(keep)::bigint
   AND s.id = a.signature_id AND s.identity_id IS NOT NULL AND o.sha256 = s.object_sha256 AND o.scheme = sqlc.arg(scheme)::text;

-- name: SignatureDetachRetiredIssued :execrows
-- The attachments of the scheme's signatures Quader made of an item with an identity that is retired (R-SIG-048): the signature made
-- again replaces them. The job signature-verify removes a signature that loses its last attachment (R-SIG-011).
DELETE FROM signature_attachments a
 USING signatures s, signature_objects o, signing_identities si
 WHERE a.item_id = sqlc.arg(item_id)::bigint AND a.source = 'issued' AND s.id = a.signature_id AND o.sha256 = s.object_sha256
   AND o.scheme = sqlc.arg(scheme)::text AND si.id = s.identity_id AND si.state = 'retired';
