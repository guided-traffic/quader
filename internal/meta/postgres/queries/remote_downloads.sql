-- Statements of the download queue of a remote repository (internal/remotecuration/queue, remote-curation.md R-CUR-010 to R-CUR-013,
-- migration 00114). Plain Go types only (no pgtype): a row and a bulk cross the boundary as the JSON text of to_jsonb, which the queue
-- decodes with one function per table, so that every statement returns the same shape; ids travel as text and are cast to uuid here.
--
-- The bound of running rows per repository (proxy.downloads.per_repository) holds across replicas because every statement that starts
-- rows first takes the repository's row with FOR NO KEY UPDATE (RemoteDownloadLockRepository): a second start of the same repository
-- waits for the first's commit and then counts its rows. The lock does not conflict with the key-share lock a foreign key takes, so an
-- insert of a row is never held by it.

-- name: RemoteDownloadLockRepository :many
-- Serializes the starts of one repository's rows (see above); an empty answer: the repository is gone.
SELECT id FROM repositories WHERE id = sqlc.arg(repo_id)::bigint FOR NO KEY UPDATE;

-- name: RemoteDownloadCounts :one
-- The waiting and running rows of a repository: the free places and the bound of waiting rows (R-CUR-011).
SELECT count(*) FILTER (WHERE state = 'waiting')::int AS waiting,
       count(*) FILTER (WHERE state = 'running')::int AS running
  FROM remote_download
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND state IN ('waiting', 'running');

-- name: RemoteDownloadInsert :many
-- A new row, waiting. A version that has a waiting or running row already inserts nothing (remote_download_one_active): the answer is
-- empty and the caller reads the active row.
INSERT INTO remote_download (id, repo_id, name, version, probe, origin, bulk_id, request_id, started_by, ticket, state, max_attempts,
                             not_before, occurrences, failure)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text,
        NULLIF(sqlc.arg(probe)::text, '')::jsonb, sqlc.arg(origin)::text, NULLIF(sqlc.arg(bulk_id)::text, '')::uuid,
        NULLIF(sqlc.arg(request_id)::text, '')::uuid, NULLIF(sqlc.arg(started_by)::text, '')::uuid,
        NULLIF(sqlc.arg(ticket)::text, '')::jsonb, sqlc.arg(state)::text, sqlc.arg(max_attempts)::int,
        CASE WHEN sqlc.arg(not_before_us)::bigint = 0 THEN NULL ELSE to_timestamp(sqlc.arg(not_before_us)::bigint / 1000000.0) END,
        1, NULLIF(sqlc.arg(failure)::text, '')::jsonb)
ON CONFLICT DO NOTHING
RETURNING to_jsonb(remote_download.*)::text AS doc;

-- name: RemoteDownloadActive :many
-- The waiting and running rows of the versions of a package (remotecuration.Queue.Active, the conflict of an insert).
SELECT to_jsonb(d.*)::text AS doc
  FROM remote_download d
 WHERE d.repo_id = sqlc.arg(repo_id)::bigint AND d.name = sqlc.arg(name)::text AND d.version = ANY(sqlc.arg(versions)::text[])
   AND d.state IN ('waiting', 'running');

-- name: RemoteDownloadDue :many
-- The waiting rows of a repository whose next attempt came, in the order they run (R-CUR-011): a person's and a request's (and an
-- allow's) before a bulk's and a probe's, then by creation. Locked, so that a concurrent start skips them.
SELECT to_jsonb(d.*)::text AS doc
  FROM remote_download d
 WHERE d.repo_id = sqlc.arg(repo_id)::bigint AND d.state = 'waiting'
   AND (d.not_before IS NULL OR d.not_before <= to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0))
 ORDER BY CASE d.origin WHEN 'person' THEN 0 WHEN 'request' THEN 0 WHEN 'allow' THEN 0 WHEN 'client' THEN 0 ELSE 1 END,
          d.created_at, d.id
 LIMIT sqlc.arg(max_rows)::int
   FOR UPDATE SKIP LOCKED;

-- name: RemoteDownloadStart :execrows
-- A waiting row becomes running with the job of its attempt.
UPDATE remote_download
   SET state = 'running', attempts = attempts + 1, job_id = sqlc.arg(job_id)::text::uuid, not_before = NULL,
       started_at = to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0), updated_at = to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0)
 WHERE id = sqlc.arg(id)::text::uuid AND state = 'waiting';

-- name: RemoteDownloadGet :many
-- One row; with repo_id 0 of any repository.
SELECT to_jsonb(d.*)::text AS doc
  FROM remote_download d
 WHERE d.id = sqlc.arg(id)::text::uuid AND (sqlc.arg(repo_id)::bigint = 0 OR d.repo_id = sqlc.arg(repo_id)::bigint);

-- name: RemoteDownloadSettle :execrows
-- The end of an attempt, written by its run: the state, the counters, the failure and the next attempt. Fenced by the attempt's job,
-- so that a run whose row was cancelled or found orphaned meanwhile writes nothing. An attempt that ended at an upstream's rate limit
-- is not counted (uncounted, Q-CUR-170).
UPDATE remote_download
   SET state = sqlc.arg(state)::text, outcome = NULLIF(sqlc.arg(outcome)::text, ''),
       attempts = CASE WHEN sqlc.arg(uncounted)::bool THEN GREATEST(attempts - 1, 0) ELSE attempts END,
       failure = NULLIF(sqlc.arg(failure)::text, '')::jsonb,
       files = sqlc.arg(files)::int, stored = stored + sqlc.arg(stored)::int, present = sqlc.arg(present)::int,
       refused = sqlc.arg(refused)::int, bytes = bytes + sqlc.arg(bytes)::bigint,
       not_before = CASE WHEN sqlc.arg(not_before_us)::bigint = 0 THEN NULL ELSE to_timestamp(sqlc.arg(not_before_us)::bigint / 1000000.0) END,
       job_id = NULL, updated_at = to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0),
       finished_at = CASE WHEN sqlc.arg(state)::text IN ('done', 'failed', 'cancelled')
                          THEN to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0) ELSE NULL END
 WHERE id = sqlc.arg(id)::text::uuid AND state = 'running' AND job_id = sqlc.arg(job_id)::text::uuid;

-- name: RemoteDownloadDueRepositories :many
-- The repositories that have a waiting row whose next attempt came: what the dispatch task starts (R-CUR-011).
SELECT DISTINCT repo_id
  FROM remote_download
 WHERE state = 'waiting' AND (not_before IS NULL OR not_before <= to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0))
 ORDER BY repo_id
 LIMIT 1000;

-- name: RemoteDownloadOrphans :many
-- A running row whose job is no longer queued or running (a worker that died, a job pruned) waits again without counting the attempt
-- (R-CUR-021). A row whose job was enqueued in the same transaction as its start is never seen without it.
UPDATE remote_download d
   SET state = 'waiting', attempts = GREATEST(d.attempts - 1, 0), job_id = NULL, not_before = NULL,
       updated_at = to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0)
 WHERE d.state = 'running'
   AND NOT EXISTS (SELECT 1 FROM job j WHERE j.id = d.job_id AND j.state IN ('queued', 'running', 'cancelling'))
RETURNING d.repo_id;

-- name: RemoteDownloadGauge :many
-- The rows by repository and state, for the gauge quader_remote_downloads (R-CUR-019).
SELECT repo_id, state, count(*)::bigint AS n
  FROM remote_download
 GROUP BY repo_id, state;

-- name: RemoteDownloadPrune :execrows
-- Retention (R-CUR-011): done and cancelled rows after done_retention, failed rows after failed_retention, at most 1,000 per call.
DELETE FROM remote_download
 WHERE id IN (
   SELECT id FROM remote_download
    WHERE finished_at IS NOT NULL
      AND ((state IN ('done', 'cancelled') AND finished_at < to_timestamp(sqlc.arg(done_before_us)::bigint / 1000000.0))
        OR (state = 'failed' AND finished_at < to_timestamp(sqlc.arg(failed_before_us)::bigint / 1000000.0)))
    ORDER BY finished_at
    LIMIT 1000);

-- name: RemoteDownloadPruneBulks :execrows
-- A bulk that ended and has no row left goes with its last row (R-CUR-011).
DELETE FROM remote_download_bulks b
 WHERE b.finished_at IS NOT NULL AND b.state <> 'expanding'
   AND b.finished_at < to_timestamp(sqlc.arg(done_before_us)::bigint / 1000000.0)
   AND NOT EXISTS (SELECT 1 FROM remote_download d WHERE d.bulk_id = b.id);

-- name: RemoteDownloadList :many
-- One page of a repository's queue (R-CUR-012): running before waiting before the rest, newest first within each; the rows queued
-- together (a bulk's, which share one creation time) in the order they run, by their id (UUIDv7, the order of RemoteDownloadDue's tie,
-- Q-CUR-181); every filter is optional (an empty value matches all). The cursor is the last row's rank, creation time and id.
SELECT to_jsonb(d.*)::text AS doc,
       (CASE d.state WHEN 'running' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END)::int AS rank
  FROM remote_download d
 WHERE d.repo_id = sqlc.arg(repo_id)::bigint
   AND (sqlc.arg(state)::text = '' OR d.state = sqlc.arg(state)::text)
   AND (sqlc.arg(class)::text = '' OR d.failure->>'class' = sqlc.arg(class)::text)
   AND (sqlc.arg(origin)::text = '' OR d.origin = sqlc.arg(origin)::text)
   AND (sqlc.arg(bulk_id)::text = '' OR d.bulk_id = NULLIF(sqlc.arg(bulk_id)::text, '')::uuid)
   AND (sqlc.arg(name)::text = '' OR d.name = sqlc.arg(name)::text)
   AND (sqlc.arg(after_rank)::int < 0
        OR (CASE d.state WHEN 'running' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END) > sqlc.arg(after_rank)::int
        OR ((CASE d.state WHEN 'running' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END) = sqlc.arg(after_rank)::int
            AND ((extract(epoch FROM d.created_at) * 1000000)::bigint < sqlc.arg(after_us)::bigint
                 OR ((extract(epoch FROM d.created_at) * 1000000)::bigint = sqlc.arg(after_us)::bigint
                     AND d.id::text > sqlc.arg(after_id)::text))))
 ORDER BY CASE d.state WHEN 'running' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END, d.created_at DESC, d.id::text
 LIMIT sqlc.arg(max_rows)::int;

-- name: RemoteDownloadStateCounts :one
-- The counts the list answers beside its page (R-CUR-012).
SELECT count(*) FILTER (WHERE state = 'waiting')::bigint AS waiting,
       count(*) FILTER (WHERE state = 'running')::bigint AS running,
       count(*) FILTER (WHERE state = 'failed')::bigint AS failed,
       count(*) FILTER (WHERE state = 'done')::bigint AS done
  FROM remote_download
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND (sqlc.arg(bulk_id)::text = '' OR bulk_id = NULLIF(sqlc.arg(bulk_id)::text, '')::uuid);

-- name: RemoteDownloadRetry :many
-- A failed or cancelled row waits again with its attempts reset (R-CUR-012); a row of origin client becomes a download ahead of its
-- version by the caller, charged as the caller's (origin person). Answers the row, or nothing for a row in another state.
UPDATE remote_download
   SET state = 'waiting', attempts = 0, max_attempts = sqlc.arg(max_attempts)::int, not_before = NULL, outcome = NULL,
       finished_at = NULL, job_id = NULL, updated_at = now(),
       origin = CASE WHEN origin = 'client' THEN 'person' ELSE origin END,
       started_by = CASE WHEN origin = 'client' THEN NULLIF(sqlc.arg(started_by)::text, '')::uuid ELSE started_by END,
       ticket = CASE WHEN origin = 'client' THEN NULLIF(sqlc.arg(ticket)::text, '')::jsonb ELSE ticket END
 WHERE id = sqlc.arg(id)::text::uuid AND repo_id = sqlc.arg(repo_id)::bigint AND state IN ('failed', 'cancelled')
RETURNING to_jsonb(remote_download.*)::text AS doc;

-- name: RemoteDownloadRetryMatching :many
-- Every failed row of a repository that matches the class and the bulk, at most max_rows, one per version that has no active row
-- (POST downloads/retry, R-CUR-012). Rows of origin client keep their origin: a bulk retry names no caller's version.
UPDATE remote_download d
   SET state = 'waiting', attempts = 0, max_attempts = sqlc.arg(max_attempts)::int, not_before = NULL, outcome = NULL,
       finished_at = NULL, job_id = NULL, updated_at = now()
 WHERE d.id IN (
   SELECT DISTINCT ON (f.name, f.version) f.id
     FROM remote_download f
    WHERE f.repo_id = sqlc.arg(repo_id)::bigint AND f.state = 'failed' AND f.origin <> 'client'
      AND (sqlc.arg(class)::text = '' OR f.failure->>'class' = sqlc.arg(class)::text)
      AND (sqlc.arg(bulk_id)::text = '' OR f.bulk_id = NULLIF(sqlc.arg(bulk_id)::text, '')::uuid)
      AND NOT EXISTS (SELECT 1 FROM remote_download a
                       WHERE a.repo_id = f.repo_id AND a.name = f.name AND a.version = f.version AND a.state IN ('waiting', 'running'))
    ORDER BY f.name, f.version, f.created_at DESC
    LIMIT sqlc.arg(max_rows)::int)
RETURNING d.id::text AS id;

-- name: RemoteDownloadCancelWaiting :many
-- A waiting row is cancelled (R-CUR-012); a running one is answered as it is and its job is cancelled by the caller.
UPDATE remote_download
   SET state = 'cancelled', finished_at = now(), updated_at = now(), not_before = NULL
 WHERE id = sqlc.arg(id)::text::uuid AND repo_id = sqlc.arg(repo_id)::bigint AND state = 'waiting'
RETURNING to_jsonb(remote_download.*)::text AS doc;

-- name: RemoteDownloadCancelBulkWaiting :execrows
-- The waiting rows of a bulk are cancelled with it (POST download-bulks/{id}/cancel).
UPDATE remote_download
   SET state = 'cancelled', finished_at = now(), updated_at = now(), not_before = NULL
 WHERE bulk_id = sqlc.arg(bulk_id)::text::uuid AND state = 'waiting';

-- name: RemoteDownloadClientFailureUpdate :many
-- A client's failed fetch of a file that has its row already: the next failure counts and replaces the failure (Q-CUR-19).
UPDATE remote_download
   SET occurrences = occurrences + 1, failure = sqlc.arg(failure)::text::jsonb, updated_at = now(), finished_at = now()
 WHERE id = (SELECT c.id FROM remote_download c
              WHERE c.repo_id = sqlc.arg(repo_id)::bigint AND c.origin = 'client' AND c.state = 'failed'
                AND c.failure->>'file' = sqlc.arg(file)::text
              ORDER BY c.created_at DESC LIMIT 1)
RETURNING id::text AS id;

-- name: RemoteDownloadClientFailureInsert :exec
-- The first failure of a file a client fetched: a row of origin client, failed (Q-CUR-19).
INSERT INTO remote_download (id, repo_id, name, version, origin, state, attempts, max_attempts, failure, finished_at)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, 'client', 'failed', 1, 1,
        sqlc.arg(failure)::text::jsonb, now());

-- name: RemoteDownloadClientFailureTrim :execrows
-- At most max_rows rows of origin client per repository; the oldest go (proxy.downloads.client_failures_per_repository).
DELETE FROM remote_download
 WHERE id IN (
   SELECT id FROM remote_download
    WHERE repo_id = sqlc.arg(repo_id)::bigint AND origin = 'client' AND state = 'failed'
    ORDER BY updated_at DESC
   OFFSET sqlc.arg(max_rows)::int);

-- name: RemoteDownloadBulkInsert :one
-- A person's bulk with its bounds; from_version and to_version '' are no bound on that side (R-CUR-010, Q-CUR-193).
INSERT INTO remote_download_bulks (id, repo_id, level, subject, max_versions, max_bytes, include_prereleases, state, started_by,
                                   from_version, to_version, to_inclusive)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(repo_id)::bigint, sqlc.arg(level)::text, sqlc.arg(subject)::text,
        sqlc.arg(max_versions)::int, sqlc.arg(max_bytes)::bigint, sqlc.arg(include_prereleases)::bool, 'expanding',
        NULLIF(sqlc.arg(started_by)::text, '')::uuid, NULLIF(sqlc.arg(from_version)::text, ''), NULLIF(sqlc.arg(to_version)::text, ''),
        sqlc.arg(to_inclusive)::bool)
RETURNING to_jsonb(remote_download_bulks.*)::text AS doc;

-- name: RemoteDownloadBulkGet :many
SELECT to_jsonb(b.*)::text AS doc
  FROM remote_download_bulks b
 WHERE b.id = sqlc.arg(id)::text::uuid AND (sqlc.arg(repo_id)::bigint = 0 OR b.repo_id = sqlc.arg(repo_id)::bigint);

-- name: RemoteDownloadBulkLock :many
-- A bulk for its expansion's step, locked so that two dispatches do not expand it at once.
SELECT to_jsonb(b.*)::text AS doc
  FROM remote_download_bulks b
 WHERE b.id = sqlc.arg(id)::text::uuid AND b.state = 'expanding'
   FOR UPDATE SKIP LOCKED;

-- name: RemoteDownloadBulksExpanding :many
-- The bulks whose expansion is not finished (the dispatch task continues each by one step).
SELECT id::text AS id, repo_id FROM remote_download_bulks WHERE state = 'expanding' ORDER BY created_at LIMIT 100;

-- name: RemoteDownloadBulkUpdate :execrows
UPDATE remote_download_bulks
   SET state = sqlc.arg(state)::text, versions_found = sqlc.arg(versions_found)::int, versions_queued = sqlc.arg(versions_queued)::int,
       cursor = NULLIF(sqlc.arg(cursor)::text, '')::jsonb, failure = NULLIF(sqlc.arg(failure)::text, '')::jsonb,
       finished_at = CASE WHEN sqlc.arg(state)::text IN ('done', 'bounded', 'cancelled', 'failed') THEN now() ELSE NULL END
 WHERE id = sqlc.arg(id)::text::uuid;

-- name: RemoteDownloadBulkBytes :one
-- The bytes the rows of a bulk stored so far: the bound maxBytes of a size the upstream does not announce (R-CUR-010).
SELECT COALESCE(sum(bytes), 0)::bigint AS bytes FROM remote_download WHERE bulk_id = sqlc.arg(bulk_id)::text::uuid;

-- name: RemoteDownloadBulksSettled :many
-- The bulks whose expansion ended (queued) and whose rows all ended: done, or bounded when the expansion stopped at a bound.
UPDATE remote_download_bulks b
   SET state = CASE WHEN b.failure IS NOT NULL AND b.failure->>'class' = 'bound' THEN 'bounded' ELSE 'done' END, finished_at = now()
 WHERE b.state = 'queued'
   AND NOT EXISTS (SELECT 1 FROM remote_download d WHERE d.bulk_id = b.id AND d.state IN ('waiting', 'running'))
RETURNING b.id::text AS id;

-- name: RemoteDownloadBulkList :many
-- One page of a repository's bulks (GET download-bulks, Q-CUR-168): newest first; the cursor is the last bulk's creation time and id.
SELECT to_jsonb(b.*)::text AS doc
  FROM remote_download_bulks b
 WHERE b.repo_id = sqlc.arg(repo_id)::bigint
   AND (sqlc.arg(after_id)::text = ''
        OR (extract(epoch FROM b.created_at) * 1000000)::bigint < sqlc.arg(after_us)::bigint
        OR ((extract(epoch FROM b.created_at) * 1000000)::bigint = sqlc.arg(after_us)::bigint AND b.id::text < sqlc.arg(after_id)::text))
 ORDER BY b.created_at DESC, b.id::text DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: RemoteDownloadBulksStateCounts :many
-- The rows of the bulks of a page by state (GET download-bulks).
SELECT bulk_id::text AS bulk_id, state, count(*)::bigint AS n
  FROM remote_download
 WHERE bulk_id = ANY(sqlc.arg(bulk_ids)::text[]::uuid[])
 GROUP BY bulk_id, state;

-- name: RemoteDownloadBulkStateCounts :many
-- The rows of a bulk by state (GET download-bulks/{id}).
SELECT state, count(*)::bigint AS n FROM remote_download WHERE bulk_id = sqlc.arg(bulk_id)::text::uuid GROUP BY state;

-- name: RemoteDownloadWaiting :one
-- The waiting rows of a repository: the bound a bulk's expansion pauses at (R-CUR-011).
SELECT count(*)::int AS n FROM remote_download WHERE repo_id = sqlc.arg(repo_id)::bigint AND state = 'waiting';

-- name: RemoteDownloadDebDirs :many
-- The release directories of a Debian remote whose stored release a key verifies, whatever its mirror scope: where a download ahead of
-- one version looks for the version's files (internal/formatdata/deb, rows.MirrorScope.AnyIndex, R-CUR-011).
SELECT DISTINCT dir
  FROM deb_remote_releases
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND document <> 'absent' AND verified_by IS NOT NULL
 ORDER BY dir;

-- name: RemoteDownloadLimitGet :many
-- The upstream's rate limit that holds a repository's queue, or held it last (remote_download_limits, migration 00120, Q-CUR-170).
SELECT to_jsonb(l.*)::text AS doc FROM remote_download_limits l WHERE l.repo_id = sqlc.arg(repo_id)::bigint;

-- name: RemoteDownloadLimitPut :exec
-- Writes a repository's limit; the caller holds the repository's lock and computed every member from the one it read.
INSERT INTO remote_download_limits (repo_id, since, until, waits, hinted, failure, updated_at)
VALUES (sqlc.arg(repo_id)::bigint, to_timestamp(sqlc.arg(since_us)::bigint / 1000000.0), to_timestamp(sqlc.arg(until_us)::bigint / 1000000.0),
        sqlc.arg(waits)::int, sqlc.arg(hinted)::bool, sqlc.arg(failure)::text::jsonb, now())
ON CONFLICT (repo_id) DO UPDATE
   SET since = EXCLUDED.since, until = EXCLUDED.until, waits = EXCLUDED.waits, hinted = EXCLUDED.hinted, failure = EXCLUDED.failure,
       updated_at = now();

-- name: RemoteDownloadLimitClear :exec
-- The first attempt the upstream answered without a limit ends the repository's limit.
DELETE FROM remote_download_limits WHERE repo_id = sqlc.arg(repo_id)::bigint;

-- name: RemoteDownloadLimitsPrune :execrows
-- A limit that ended, of a repository whose queue has no waiting or running row left (the task remote-downloads-dispatch).
DELETE FROM remote_download_limits l
 WHERE l.until < to_timestamp(sqlc.arg(now_us)::bigint / 1000000.0)
   AND NOT EXISTS (SELECT 1 FROM remote_download d WHERE d.repo_id = l.repo_id AND d.state IN ('waiting', 'running'));
