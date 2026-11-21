-- The statements of the signing identity and of the job signing (signatures.md R-SIG-045 to R-SIG-049, R-SIG-057, R-SIG-059;
-- internal/signing and internal/signing/job). A time travels as RFC 3339 text in UTC with microseconds ('' for none), a principal as
-- the text of its uuid ('' for none), as the Debian key statements do. Only internal/signing writes these tables; the sealed key is read
-- by SigningSealedKey alone, which only internal/signing/key calls (R-SIG-049). The documents of a local npm repository and the
-- signatures' views read the states of the identities to serve and show Quader's own signatures (R-SIG-050, R-SIG-055).

-- name: SigningIdentities :many
-- Every identity in every state, oldest first, without its private part.
SELECT i.id, i.source, i.state, i.public_key, i.key_sha256, i.npm_keyid, COALESCE(i.csr, '')::text AS csr,
       COALESCE(i.certificate, ''::bytea)::bytea AS certificate, COALESCE(i.cert_sha256, ''::bytea)::bytea AS cert_sha256,
       i.chain, COALESCE(i.subject, '')::text AS subject, COALESCE(i.issuer, '')::text AS issuer, i.names,
       COALESCE(to_char(i.not_before AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS not_before,
       COALESCE(to_char(i.not_after AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS not_after,
       COALESCE(to_char(i.announced_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS announced_at,
       COALESCE(to_char(i.current_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS current_at,
       COALESCE(to_char(i.expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS expires_at,
       COALESCE(to_char(i.retired_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS retired_at,
       to_char(i.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')::text AS created_at,
       COALESCE(CASE p.kind WHEN 'ci_mapping' THEN 'ci' ELSE p.kind END || ':' || p.name, '')::text AS created_by_name
  FROM signing_identities i
  LEFT JOIN principal p ON p.id = i.created_by
 ORDER BY i.id;

-- name: SigningLockSettings :exec
-- The one row of the signing's state, locked: every change of the identities takes it first, so two writers run one after the other
-- also while no identity exists yet.
SELECT id FROM signing_settings WHERE id FOR UPDATE;

-- name: SigningLockIdentities :many
-- The ids and states of every identity that is not retired, locked in id order: every change of the identities waits for the one
-- before it, so the unique indexes of the states are never met by two writers at once.
SELECT id, state FROM signing_identities WHERE state <> 'retired' ORDER BY id FOR UPDATE;

-- name: SigningNow :one
-- The transaction's time as RFC 3339 text: the checks of R-SIG-045 and the switch of R-SIG-048 judge against the database's clock.
SELECT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')::text AS now;

-- name: SigningNextID :one
-- The id of a row about to be inserted: an uploaded key is sealed under its row's id before the insert (R-SIG-047).
SELECT nextval(pg_get_serial_sequence('signing_identities', 'id'))::bigint AS id;

-- name: SigningInsertIdentity :exec
-- A new identity: a pending key (certificate empty), or an installed one in the state announced or current.
INSERT INTO signing_identities (id, source, state, public_key, key_sha256, npm_keyid, sealed_key, csr, certificate, cert_sha256, chain,
                                subject, issuer, names, not_before, not_after, announced_at, current_at, created_by)
VALUES (sqlc.arg(id)::bigint, sqlc.arg(source)::text, sqlc.arg(state)::text, sqlc.arg(public_key)::bytea, sqlc.arg(key_sha256)::bytea,
        sqlc.arg(npm_keyid)::text, NULLIF(sqlc.arg(sealed_key)::text, ''), NULLIF(sqlc.arg(csr)::text, ''),
        NULLIF(sqlc.arg(certificate)::bytea, ''::bytea), NULLIF(sqlc.arg(cert_sha256)::bytea, ''::bytea), sqlc.arg(chain)::bytea[],
        NULLIF(sqlc.arg(subject)::text, ''), NULLIF(sqlc.arg(issuer)::text, ''), sqlc.arg(names)::text[],
        NULLIF(sqlc.arg(not_before)::text, '')::timestamptz, NULLIF(sqlc.arg(not_after)::text, '')::timestamptz,
        CASE WHEN sqlc.arg(state)::text IN ('announced', 'current') THEN now() END,
        CASE WHEN sqlc.arg(state)::text = 'current' THEN clock_timestamp() END,
        NULLIF(sqlc.arg(created_by)::text, '')::uuid);

-- name: SigningInstallCertificate :execrows
-- The certificate the company's CA issued for the pending key (R-SIG-047): the identity is announced or current.
UPDATE signing_identities
   SET state = sqlc.arg(state)::text, csr = NULL, certificate = sqlc.arg(certificate)::bytea, cert_sha256 = sqlc.arg(cert_sha256)::bytea,
       chain = sqlc.arg(chain)::bytea[], subject = NULLIF(sqlc.arg(subject)::text, ''), issuer = NULLIF(sqlc.arg(issuer)::text, ''),
       names = sqlc.arg(names)::text[], not_before = sqlc.arg(not_before)::text::timestamptz, not_after = sqlc.arg(not_after)::text::timestamptz,
       announced_at = now(), current_at = CASE WHEN sqlc.arg(state)::text = 'current' THEN clock_timestamp() END
 WHERE id = sqlc.arg(id)::bigint AND state = 'pending';

-- name: SigningAnnounce :execrows
-- An identity is listed before it signs (R-SIG-048): a new one beside the current, or a previous one brought back.
UPDATE signing_identities SET state = 'announced', announced_at = now(), current_at = NULL, expires_at = NULL
 WHERE id = sqlc.arg(id)::bigint AND state IN ('previous', 'announced');

-- name: SigningMakeCurrent :execrows
-- The switch's first half, or an identity that is current at once because none is (R-SIG-048).
UPDATE signing_identities SET state = 'current', current_at = clock_timestamp(), expires_at = NULL,
       announced_at = COALESCE(announced_at, now())
 WHERE id = sqlc.arg(id)::bigint AND state IN ('announced', 'previous');

-- name: SigningMakePrevious :execrows
-- The switch's second half, a removal, or the identity of the files once the values name none: it stops signing now (R-SIG-048).
UPDATE signing_identities SET state = 'previous', expires_at = clock_timestamp()
 WHERE id = sqlc.arg(id)::bigint AND state IN ('current', 'announced');

-- name: SigningRetire :execrows
-- A previous identity withdrawn (R-SIG-048): its sealed key goes with it (Q-SIG-149).
UPDATE signing_identities SET state = 'retired', retired_at = now(), sealed_key = NULL
 WHERE id = sqlc.arg(id)::bigint AND state = 'previous';

-- name: SigningDeletePending :execrows
-- The pending key and its request discarded (R-SIG-047).
DELETE FROM signing_identities WHERE state = 'pending';

-- name: SigningLockCurrentShared :many
-- A signing transaction's hold on the identity it signs with (R-SIG-048): no row when the identity is not current any more, so a
-- signature is never written after its identity's switch; the switch locks the row exclusively and waits for every holder.
SELECT id, COALESCE(to_char(not_after AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS not_after
  FROM signing_identities WHERE id = sqlc.arg(id)::bigint AND state = 'current' FOR SHARE;

-- name: SigningSignatureCount :one
-- The signatures an identity made (R-SIG-054), the count signing.identity.retired records.
SELECT count(*)::bigint FROM signatures WHERE identity_id = sqlc.arg(identity_id)::bigint;

-- name: SigningSealedKey :many
-- The sealed private key of an uploaded identity that is not retired; the worker's key holder opens it (R-SIG-047, R-SIG-049).
SELECT sealed_key::text FROM signing_identities WHERE id = sqlc.arg(id)::bigint AND sealed_key IS NOT NULL;

-- name: SigningSealedKeysAfter :many
-- The re-encryption walk over signing_identities.sealed_key (R-ARCH-021): a batch after the id, locked.
SELECT id, sealed_key::text AS sealed_key FROM signing_identities
 WHERE sealed_key IS NOT NULL AND id > sqlc.arg(after_id)::bigint
 ORDER BY id LIMIT sqlc.arg(batch)::bigint FOR UPDATE;

-- name: SigningUpdateSealedKey :exec
UPDATE signing_identities SET sealed_key = sqlc.arg(sealed)::text WHERE id = sqlc.arg(id)::bigint AND sealed_key IS NOT NULL;

-- name: SigningJournalRemove :execrows
-- The replay of signing.identity.removed (R-SIG-059): the identity of the id and certificate stops signing again.
UPDATE signing_identities SET state = 'previous', expires_at = COALESCE(expires_at, clock_timestamp())
 WHERE id = sqlc.arg(id)::bigint AND cert_sha256 = sqlc.arg(cert_sha256)::bytea AND state IN ('current', 'announced');

-- name: SigningJournalRetire :execrows
-- The replay of signing.identity.retired (R-SIG-059): the identity of the id and certificate is withdrawn again, whatever state the
-- restore brought back, and its sealed key goes.
UPDATE signing_identities
   SET state = 'retired', retired_at = COALESCE(retired_at, now()), sealed_key = NULL, expires_at = COALESCE(expires_at, clock_timestamp())
 WHERE id = sqlc.arg(id)::bigint AND cert_sha256 = sqlc.arg(cert_sha256)::bytea AND state <> 'retired';

-- name: SigningSettings :one
-- The one row of the signing's state.
SELECT COALESCE(refused_sha256, ''::bytea)::bytea AS refused_sha256, COALESCE(refused_reason, '')::text AS refused_reason,
       COALESCE(refused_fingerprint, '')::text AS refused_fingerprint,
       COALESCE(to_char(refused_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS refused_at,
       discovery
  FROM signing_settings WHERE id;

-- name: SigningSetRefused :execrows
-- A pair of the files that failed R-SIG-045 (R-SIG-046): one row when its content is not the refusal recorded last, so the event is
-- recorded once per content.
UPDATE signing_settings
   SET refused_sha256 = sqlc.arg(content_sha256)::bytea, refused_reason = sqlc.arg(reason)::text,
       refused_fingerprint = NULLIF(sqlc.arg(fingerprint)::text, ''), refused_at = now()
 WHERE id AND refused_sha256 IS DISTINCT FROM sqlc.arg(content_sha256)::bytea;

-- name: SigningClearRefused :execrows
-- A pair of the files passed: the refusal shown on the admin page is gone.
UPDATE signing_settings SET refused_sha256 = NULL, refused_reason = NULL, refused_fingerprint = NULL, refused_at = NULL
 WHERE id AND refused_sha256 IS NOT NULL;

-- name: SigningSetDiscovery :exec
-- Discovery's cursor of a kind (R-SIG-049).
UPDATE signing_settings SET discovery = discovery || jsonb_build_object(sqlc.arg(kind)::text, sqlc.arg(after_id)::bigint) WHERE id;

-- name: SigningClaimDailyRetry :execrows
-- The daily retry of failed work (R-SIG-049): one row when a day passed since the last, which the caller then makes due.
UPDATE signing_settings SET failed_retried_at = now()
 WHERE id AND (failed_retried_at IS NULL OR failed_retried_at < now() - interval '1 day');

-- name: SigningUnfailWork :execrows
UPDATE signing_work SET failed = false, attempts = 0, not_before = now(), code = NULL WHERE failed;

-- name: SigningClaimFailedReport :execrows
-- signing.failed at most once an hour per kind and code (R-SIG-057): one row when the report may be recorded now.
UPDATE signing_settings
   SET failed_reported = failed_reported || jsonb_build_object(sqlc.arg(key)::text, to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
 WHERE id AND (failed_reported ->> sqlc.arg(key)::text IS NULL
               OR (failed_reported ->> sqlc.arg(key)::text)::timestamptz < now() - interval '1 hour');

-- name: SigningEnqueueWork :execrows
-- Work for items of one kind (R-SIG-049), in the caller's transaction: a row that waits keeps the higher priority and the earlier time,
-- and a failed one starts its attempts again.
INSERT INTO signing_work (kind, item_id, reason, priority, not_before)
SELECT sqlc.arg(kind)::text, i, sqlc.arg(reason)::text, sqlc.arg(priority)::smallint,
       now() + make_interval(secs => sqlc.arg(delay_secs)::double precision)
  FROM unnest(sqlc.arg(item_ids)::bigint[]) AS i
ON CONFLICT (kind, item_id) DO UPDATE
   SET reason = CASE WHEN excluded.priority < signing_work.priority OR signing_work.failed THEN excluded.reason ELSE signing_work.reason END,
       priority = LEAST(signing_work.priority, excluded.priority),
       not_before = CASE WHEN signing_work.failed THEN excluded.not_before ELSE LEAST(signing_work.not_before, excluded.not_before) END,
       attempts = CASE WHEN signing_work.failed THEN 0 ELSE signing_work.attempts END,
       failed = false, code = CASE WHEN signing_work.failed THEN NULL ELSE signing_work.code END;

-- name: SigningEnqueueWorkWhileCurrent :execrows
-- Work for items of one kind in the caller's transaction, written only while an identity is current (R-SIG-049, Q-SIG-158): a publish
-- before the installation's first identity leaves its version to discovery, so an installation that never signs keeps no work.
INSERT INTO signing_work (kind, item_id, reason, priority, not_before)
SELECT sqlc.arg(kind)::text, i, sqlc.arg(reason)::text, sqlc.arg(priority)::smallint, now()
  FROM unnest(sqlc.arg(item_ids)::bigint[]) AS i
 WHERE EXISTS (SELECT 1 FROM signing_identities WHERE state = 'current')
ON CONFLICT (kind, item_id) DO UPDATE
   SET reason = CASE WHEN excluded.priority < signing_work.priority OR signing_work.failed THEN excluded.reason ELSE signing_work.reason END,
       priority = LEAST(signing_work.priority, excluded.priority),
       not_before = CASE WHEN signing_work.failed THEN excluded.not_before ELSE LEAST(signing_work.not_before, excluded.not_before) END,
       attempts = CASE WHEN signing_work.failed THEN 0 ELSE signing_work.attempts END,
       failed = false, code = CASE WHEN signing_work.failed THEN NULL ELSE signing_work.code END;

-- name: SigningServed :one
-- The ids of the identities whose signatures of a kind the documents served when the job last compared (Q-SIG-160); found is false
-- before the first comparison.
SELECT COALESCE(served -> sqlc.arg(kind)::text, '[]'::jsonb)::text AS ids, (served ? sqlc.arg(kind)::text)::boolean AS found
  FROM signing_settings WHERE id;

-- name: SigningSetServed :exec
UPDATE signing_settings SET served = served || jsonb_build_object(sqlc.arg(kind)::text, sqlc.arg(ids)::text::jsonb) WHERE id;

-- name: SigningDueWork :many
-- The due work of the kinds a handler exists for, by priority and time, without the keys in flight (R-SIG-049).
SELECT kind, item_id, reason, priority::int AS priority,
       to_char(not_before AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')::text AS not_before, attempts
  FROM signing_work
 WHERE NOT failed AND not_before <= now() AND kind = ANY(sqlc.arg(kinds)::text[])
   AND NOT ((kind || ':' || item_id::text) = ANY(sqlc.arg(in_flight)::text[]))
 ORDER BY priority, not_before, item_id
 LIMIT sqlc.arg(max_rows)::bigint;

-- name: SigningLockWork :many
-- One row of work, locked for its operation; none when another replica holds it or it is done.
SELECT attempts FROM signing_work
 WHERE kind = sqlc.arg(kind)::text AND item_id = sqlc.arg(item_id)::bigint AND NOT failed
 FOR UPDATE SKIP LOCKED;

-- name: SigningDoneWork :exec
DELETE FROM signing_work WHERE kind = sqlc.arg(kind)::text AND item_id = sqlc.arg(item_id)::bigint;

-- name: SigningDeferWork :exec
-- An attempt spent (R-SIG-049): due again after the back-off, or failed after the last attempt, with the code of the attempt.
UPDATE signing_work
   SET attempts = attempts + 1, not_before = sqlc.arg(not_before)::text::timestamptz, failed = sqlc.arg(failed)::boolean,
       code = NULLIF(sqlc.arg(code)::text, '')
 WHERE kind = sqlc.arg(kind)::text AND item_id = sqlc.arg(item_id)::bigint;

-- name: SigningBacklog :many
-- The work per kind, priority and whether it failed, with the oldest due time of what waits (R-SIG-055, R-SIG-057).
SELECT kind, priority::int AS priority, failed, count(*)::bigint AS rows,
       COALESCE(to_char(min(not_before) FILTER (WHERE NOT failed AND not_before <= now()) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')::text AS oldest_due
  FROM signing_work
 GROUP BY kind, priority, failed
 ORDER BY kind, priority, failed;
