-- The usage store of the download limits (usage-and-limits.md R-LIM-027 to R-LIM-029, R-LIM-033, R-LIM-034;
-- internal/limits/usage). A flush writes one statement per table for a batch, in key order; the reads serve the overview and
-- are never run by a protocol request.

-- name: UpsertUsageBuckets :execrows
-- One flush of the usage buffer (R-LIM-028): a JSON array of rows, each added to its bucket. The rows go in in key order so
-- that the batches of several replicas do not deadlock on each other's rows; a row of a repository deleted since is dropped.
INSERT INTO usage_bucket AS b (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id,
                               requests, refused, cached_bytes, uncached_bytes, upstream_requests, background_fetches,
                               limit_requests_hour, limit_cached_hour, limit_uncached_hour, limit_upstream_hour, limit_bytes_day)
SELECT d ->> 'g', (d ->> 's')::timestamptz, d ->> 'k', d ->> 'r', d ->> 'c', d ->> 'p', NULLIF((d ->> 'repo')::bigint, 0),
       (d ->> 'rq')::bigint, (d ->> 'rf')::bigint, (d ->> 'cb')::bigint, (d ->> 'ub')::bigint, (d ->> 'up')::bigint,
       (d ->> 'bg')::bigint,
       (d ->> 'lrh')::bigint, (d ->> 'lch')::bigint, (d ->> 'luh')::bigint, (d ->> 'lup')::bigint, (d ->> 'lbd')::bigint
  FROM jsonb_array_elements(sqlc.arg(rows)::text::jsonb) AS d
 WHERE NULLIF((d ->> 'repo')::bigint, 0) IS NULL OR EXISTS (SELECT 1 FROM repositories r WHERE r.id = (d ->> 'repo')::bigint)
 ORDER BY d ->> 'g', d ->> 's', d ->> 'k', d ->> 'r', d ->> 'c', d ->> 'p', (d ->> 'repo')::bigint
ON CONFLICT (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id) DO UPDATE
   SET requests = b.requests + EXCLUDED.requests, refused = b.refused + EXCLUDED.refused,
       cached_bytes = b.cached_bytes + EXCLUDED.cached_bytes, uncached_bytes = b.uncached_bytes + EXCLUDED.uncached_bytes,
       upstream_requests = b.upstream_requests + EXCLUDED.upstream_requests,
       background_fetches = b.background_fetches + EXCLUDED.background_fetches,
       limit_requests_hour = EXCLUDED.limit_requests_hour, limit_cached_hour = EXCLUDED.limit_cached_hour,
       limit_uncached_hour = EXCLUDED.limit_uncached_hour, limit_upstream_hour = EXCLUDED.limit_upstream_hour,
       limit_bytes_day = EXCLUDED.limit_bytes_day;

-- name: UpsertUsageEvents :execrows
-- The limits met in a flush: limited (enforce) and would_limit (audit) counts per figure (R-LIM-027).
INSERT INTO usage_limit_event AS e (granularity, bucket_start, subject_kind, subject_ref, place, repository_id, figure, mode, count)
SELECT d ->> 'g', (d ->> 's')::timestamptz, d ->> 'k', d ->> 'r', d ->> 'p', NULLIF((d ->> 'repo')::bigint, 0), d ->> 'f',
       d ->> 'm', (d ->> 'n')::bigint
  FROM jsonb_array_elements(sqlc.arg(rows)::text::jsonb) AS d
 WHERE NULLIF((d ->> 'repo')::bigint, 0) IS NULL OR EXISTS (SELECT 1 FROM repositories r WHERE r.id = (d ->> 'repo')::bigint)
 ORDER BY d ->> 'g', d ->> 's', d ->> 'k', d ->> 'r', d ->> 'p', (d ->> 'repo')::bigint, d ->> 'f', d ->> 'm'
ON CONFLICT (granularity, bucket_start, subject_kind, subject_ref, place, repository_id, figure, mode) DO UPDATE
   SET count = e.count + EXCLUDED.count;

-- name: UsageTotals :many
-- The consumption of every subject in a period and scope (R-LIM-034): one row per subject. repos empty is the instance.
SELECT b.subject_kind, b.subject_ref,
       sum(b.requests)::bigint AS requests, sum(b.refused)::bigint AS refused, sum(b.cached_bytes)::bigint AS cached_bytes,
       sum(b.uncached_bytes)::bigint AS uncached_bytes, sum(b.upstream_requests)::bigint AS upstream_requests
  FROM usage_bucket b
 WHERE b.granularity = sqlc.arg(granularity) AND b.bucket_start >= sqlc.arg(since)::text::timestamptz
   AND (cardinality(sqlc.arg(repos)::bigint[]) = 0 OR b.repository_id = ANY(sqlc.arg(repos)::bigint[]))
   AND b.subject_kind = ANY(sqlc.arg(kinds)::text[])
   AND (sqlc.arg(subject_ref)::text = '' OR b.subject_ref = sqlc.arg(subject_ref)::text)
 GROUP BY b.subject_kind, b.subject_ref;

-- name: UsageEventTotals :many
-- The limits met per subject in a period and scope, by mode.
SELECT e.subject_kind, e.subject_ref, e.mode, sum(e.count)::bigint AS events, max(e.bucket_start)::timestamptz AS last_at
  FROM usage_limit_event e
 WHERE e.granularity = sqlc.arg(granularity) AND e.bucket_start >= sqlc.arg(since)::text::timestamptz
   AND (cardinality(sqlc.arg(repos)::bigint[]) = 0 OR e.repository_id = ANY(sqlc.arg(repos)::bigint[]))
   AND (sqlc.arg(subject_ref)::text = '' OR e.subject_ref = sqlc.arg(subject_ref)::text)
 GROUP BY e.subject_kind, e.subject_ref, e.mode;

-- name: UsageByPlace :many
-- The last 60 minutes and the last 24 hours of every subject per place, with the limits the rows recorded: the utilisation
-- of R-LIM-034 (Q-UI-732), independent of the period.
SELECT b.subject_kind, b.subject_ref, b.place,
       COALESCE(sum(b.requests) FILTER (WHERE b.bucket_start >= sqlc.arg(hour_since)::text::timestamptz), 0)::bigint AS hour_requests,
       COALESCE(sum(b.cached_bytes) FILTER (WHERE b.bucket_start >= sqlc.arg(hour_since)::text::timestamptz), 0)::bigint AS hour_cached,
       COALESCE(sum(b.uncached_bytes) FILTER (WHERE b.bucket_start >= sqlc.arg(hour_since)::text::timestamptz), 0)::bigint AS hour_uncached,
       COALESCE(sum(b.upstream_requests) FILTER (WHERE b.bucket_start >= sqlc.arg(hour_since)::text::timestamptz), 0)::bigint AS hour_upstream,
       COALESCE(sum(b.cached_bytes + b.uncached_bytes), 0)::bigint AS day_bytes,
       COALESCE(max(b.limit_requests_hour), 0)::bigint AS limit_requests_hour,
       COALESCE(max(b.limit_cached_hour), 0)::bigint AS limit_cached_hour,
       COALESCE(max(b.limit_uncached_hour), 0)::bigint AS limit_uncached_hour,
       COALESCE(max(b.limit_upstream_hour), 0)::bigint AS limit_upstream_hour,
       COALESCE(max(b.limit_bytes_day), 0)::bigint AS limit_bytes_day,
       bool_and(b.limit_requests_hour IS NULL AND b.limit_cached_hour IS NULL AND b.limit_uncached_hour IS NULL
                AND b.limit_upstream_hour IS NULL AND b.limit_bytes_day IS NULL)::boolean AS unlimited
  FROM usage_bucket b
 WHERE b.granularity = '5m' AND b.bucket_start >= sqlc.arg(day_since)::text::timestamptz
   AND (cardinality(sqlc.arg(repos)::bigint[]) = 0 OR b.repository_id = ANY(sqlc.arg(repos)::bigint[]))
   AND (sqlc.arg(subject_ref)::text = '' OR b.subject_ref = sqlc.arg(subject_ref)::text)
 GROUP BY b.subject_kind, b.subject_ref, b.place;

-- name: UsageHistory :many
-- The five-minute points of one subject in the last 24 hours (R-LIM-034 history).
SELECT b.bucket_start, sum(b.requests)::bigint AS requests, sum(b.refused)::bigint AS refused,
       sum(b.cached_bytes)::bigint AS cached_bytes, sum(b.uncached_bytes)::bigint AS uncached_bytes,
       sum(b.upstream_requests)::bigint AS upstream_requests
  FROM usage_bucket b
 WHERE b.granularity = '5m' AND b.bucket_start >= sqlc.arg(since)::text::timestamptz AND b.subject_kind = sqlc.arg(subject_kind)
   AND b.subject_ref = sqlc.arg(subject_ref)
   AND (cardinality(sqlc.arg(repos)::bigint[]) = 0 OR b.repository_id = ANY(sqlc.arg(repos)::bigint[]))
 GROUP BY b.bucket_start
 ORDER BY b.bucket_start;

-- name: UsageRepositories :many
-- One subject's consumption per serving repository in a period.
SELECT COALESCE(b.repository_id, 0)::bigint AS repository_id, COALESCE(p.key, '')::text AS project_key,
       COALESCE(r.key, '')::text AS repo_key,
       sum(b.requests)::bigint AS requests, sum(b.refused)::bigint AS refused, sum(b.cached_bytes)::bigint AS cached_bytes,
       sum(b.uncached_bytes)::bigint AS uncached_bytes, sum(b.upstream_requests)::bigint AS upstream_requests
  FROM usage_bucket b
  LEFT JOIN repositories r ON r.id = b.repository_id
  LEFT JOIN project p ON p.id = r.project_id
 WHERE b.granularity = sqlc.arg(granularity) AND b.bucket_start >= sqlc.arg(since)::text::timestamptz AND b.subject_kind = sqlc.arg(subject_kind)
   AND b.subject_ref = sqlc.arg(subject_ref)
   AND (cardinality(sqlc.arg(repos)::bigint[]) = 0 OR b.repository_id = ANY(sqlc.arg(repos)::bigint[]))
 GROUP BY b.repository_id, p.key, r.key
 ORDER BY sum(b.requests) DESC;

-- name: UsageCredentials :many
-- A user's consumption per credential in a period: session or the personal token's ID (R-LIM-027).
SELECT b.credential_ref, sum(b.requests)::bigint AS requests, sum(b.cached_bytes)::bigint AS cached_bytes,
       sum(b.uncached_bytes)::bigint AS uncached_bytes
  FROM usage_bucket b
 WHERE b.granularity = sqlc.arg(granularity) AND b.bucket_start >= sqlc.arg(since)::text::timestamptz AND b.subject_kind = 'user'
   AND b.subject_ref = sqlc.arg(subject_ref)
   AND (cardinality(sqlc.arg(repos)::bigint[]) = 0 OR b.repository_id = ANY(sqlc.arg(repos)::bigint[]))
 GROUP BY b.credential_ref
 ORDER BY sum(b.requests) DESC;

-- name: UsageModePreview :many
-- What enforce would have refused or slowed in a period: the would_limit events of the five-minute buckets (R-LIM-033).
SELECT e.subject_kind, e.subject_ref, e.figure, sum(e.count)::bigint AS events
  FROM usage_limit_event e
 WHERE e.granularity = '5m' AND e.bucket_start >= sqlc.arg(since)::text::timestamptz AND e.mode = 'audit'
 GROUP BY e.subject_kind, e.subject_ref, e.figure;

-- name: FoldUsageDays :execrows
-- limits-cleanup (R-LIM-028): the hourly rows older than the cutoff become daily sums without a person, per day, repository
-- and kind of caller.
INSERT INTO usage_daily_summary AS s (day, repository_id, subject_kind, subjects, requests, refused, cached_bytes,
                                     uncached_bytes, upstream_requests, background_fetches, limited_events, would_limit_events)
SELECT (b.bucket_start AT TIME ZONE 'UTC')::date, b.repository_id, b.subject_kind, count(DISTINCT b.subject_ref),
       sum(b.requests), sum(b.refused), sum(b.cached_bytes), sum(b.uncached_bytes), sum(b.upstream_requests),
       sum(b.background_fetches),
       COALESCE((SELECT sum(e.count) FROM usage_limit_event e
                  WHERE e.granularity = '1h' AND e.mode = 'enforce' AND e.bucket_start < sqlc.arg(cutoff)::text::timestamptz
                    AND (e.bucket_start AT TIME ZONE 'UTC')::date = (b.bucket_start AT TIME ZONE 'UTC')::date
                    AND e.repository_id IS NOT DISTINCT FROM b.repository_id AND e.subject_kind = b.subject_kind), 0),
       COALESCE((SELECT sum(e.count) FROM usage_limit_event e
                  WHERE e.granularity = '1h' AND e.mode = 'audit' AND e.bucket_start < sqlc.arg(cutoff)::text::timestamptz
                    AND (e.bucket_start AT TIME ZONE 'UTC')::date = (b.bucket_start AT TIME ZONE 'UTC')::date
                    AND e.repository_id IS NOT DISTINCT FROM b.repository_id AND e.subject_kind = b.subject_kind), 0)
  FROM usage_bucket b
 WHERE b.granularity = '1h' AND b.bucket_start < sqlc.arg(cutoff)::text::timestamptz
 GROUP BY (b.bucket_start AT TIME ZONE 'UTC')::date, b.repository_id, b.subject_kind
ON CONFLICT (day, repository_id, subject_kind) DO UPDATE
   SET subjects = s.subjects + EXCLUDED.subjects, requests = s.requests + EXCLUDED.requests, refused = s.refused + EXCLUDED.refused,
       cached_bytes = s.cached_bytes + EXCLUDED.cached_bytes, uncached_bytes = s.uncached_bytes + EXCLUDED.uncached_bytes,
       upstream_requests = s.upstream_requests + EXCLUDED.upstream_requests,
       background_fetches = s.background_fetches + EXCLUDED.background_fetches,
       limited_events = s.limited_events + EXCLUDED.limited_events,
       would_limit_events = s.would_limit_events + EXCLUDED.would_limit_events;

-- name: DeleteUsageBuckets :execrows
DELETE FROM usage_bucket WHERE granularity = sqlc.arg(granularity) AND bucket_start < sqlc.arg(cutoff)::text::timestamptz;

-- name: DeleteUsageEvents :execrows
DELETE FROM usage_limit_event WHERE granularity = sqlc.arg(granularity) AND bucket_start < sqlc.arg(cutoff)::text::timestamptz;

-- name: DeleteUsageSummaries :execrows
DELETE FROM usage_daily_summary WHERE day < sqlc.arg(cutoff)::text::date;

-- name: DeleteLimitBlocksBefore :execrows
DELETE FROM limit_block WHERE until < sqlc.arg(cutoff)::text::timestamptz;

-- name: EraseUsageOfDeletedSubjects :execrows
-- Erasure (R-LIM-028, Q-LIM-25): the rows of users and technical tokens that no longer exist. A technical token's rows are
-- kept until their retention when its table does not exist yet (work package 1 adds it).
DELETE FROM usage_bucket b
 WHERE b.subject_kind = 'user' AND NOT EXISTS (SELECT 1 FROM principal p WHERE p.id::text = b.subject_ref);

-- name: EraseEventsOfDeletedSubjects :execrows
DELETE FROM usage_limit_event e
 WHERE e.subject_kind = 'user' AND NOT EXISTS (SELECT 1 FROM principal p WHERE p.id::text = e.subject_ref);

-- name: EraseBlocksOfDeletedSubjects :execrows
DELETE FROM limit_block k
 WHERE k.subject_kind = 'user' AND NOT EXISTS (SELECT 1 FROM principal p WHERE p.id::text = k.subject_ref);

-- name: DeleteRulesOfDeletedSubjects :many
-- The rules whose user or group no longer exists (R-LIM-011): returned for the audit events of limits-cleanup.
DELETE FROM limit_assignment a
 WHERE a.who_kind IN ('user', 'group') AND NOT EXISTS (SELECT 1 FROM principal p WHERE p.id::text = a.who_ref)
RETURNING a.id::text AS id, a.place_kind, a.who_kind, a.who_ref;
