-- The statements of the background fetch of a remote's signatures (signatures.md R-SIG-026, R-SIG-067, Q-SIG-33, Q-SIG-106;
-- internal/signatures/fetch).

-- name: SignatureFetchQueue :many
-- Claims a target for the job signature-fetch: a new target, one never fetched (a failed one included), or, as a refresh, one whose
-- last fetch is older than the refresh period; a target queued less than a day ago is not queued again (a job that never ended frees it
-- after a day). The subject is kept once known. A row comes back when the job is to be queued.
INSERT INTO signature_fetches AS f (repo_id, target, format, subject_sha256, queued_at)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(target)::text, sqlc.arg(format)::text, NULLIF(sqlc.arg(subject)::bytea, ''::bytea), now())
ON CONFLICT (repo_id, target) DO UPDATE SET queued_at = now(), subject_sha256 = COALESCE(EXCLUDED.subject_sha256, f.subject_sha256)
 WHERE (f.queued_at IS NULL OR f.queued_at < now() - interval '1 day')
   AND (f.fetched_at IS NULL
        OR (sqlc.arg(refresh)::boolean AND f.fetched_at < now() - make_interval(secs => sqlc.arg(refresh_after_seconds)::float8)))
RETURNING f.repo_id;

-- name: SignatureFetchDone :many
-- The end of a run: the target is free again; a run that asked the upstream to the end is a fetch, which clears the failures. The
-- subject comes back for the signal a waiting download of stage 3's gate listens to (R-SIG-067).
UPDATE signature_fetches
   SET queued_at = NULL,
       fetched_at = CASE WHEN sqlc.arg(fetched)::boolean THEN now() ELSE fetched_at END,
       attempts = CASE WHEN sqlc.arg(fetched)::boolean THEN 0 ELSE attempts END,
       failed_at = CASE WHEN sqlc.arg(fetched)::boolean THEN NULL ELSE failed_at END
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND target = sqlc.arg(target)::text
RETURNING COALESCE(subject_sha256, ''::bytea)::bytea AS subject;

-- name: SignatureFetchFailed :many
-- A run that failed: one failure more; the job's last attempt frees the target and marks it failed until the next fill or refresh
-- queues it again.
UPDATE signature_fetches
   SET attempts = attempts + 1,
       queued_at = CASE WHEN sqlc.arg(final)::boolean THEN NULL ELSE queued_at END,
       failed_at = CASE WHEN sqlc.arg(final)::boolean THEN now() ELSE failed_at END
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND target = sqlc.arg(target)::text
RETURNING COALESCE(subject_sha256, ''::bytea)::bytea AS subject;

-- name: SignatureFetchForget :exec
-- The state of a target whose item went (a removed or evicted manifest).
DELETE FROM signature_fetches WHERE repo_id = sqlc.arg(repo_id)::bigint AND target = sqlc.arg(target)::text;

-- name: SignatureFetchRemote :many
-- The repository of a run: its format, and whether its detail setting signatures.fetch (default true) lets it fetch. A repository that
-- is gone, no remote repository or not active answers no row.
SELECT r.format::text AS format, COALESCE((r.config -> 'signatures' ->> 'fetch')::boolean, true)::boolean AS fetch
  FROM repositories r
 WHERE r.id = sqlc.arg(repo_id)::bigint AND r.class = 'remote' AND r.lifecycle = 'active';
