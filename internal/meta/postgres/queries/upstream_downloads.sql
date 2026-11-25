-- Statements of the downloads from upstreams (internal/fills, operations-and-observability.md R-OPS-049 to R-OPS-052, migration 00118).
-- Plain Go types only: the rows cross the boundary as JSON text (a replica's running downloads are JSON already, a finished row is
-- to_jsonb of its row), so that the package needs no driver types.

-- name: UpstreamDownloadLiveUpsert :exec
-- A replica's running downloads at its publication (R-OPS-050): its one row, written at PostgreSQL's now().
INSERT INTO upstream_download_live (instance_uid, replica_name, written_at, downloads, omitted)
VALUES (sqlc.arg(instance_uid)::text::uuid, sqlc.arg(replica_name)::text, now(), sqlc.arg(downloads)::text::jsonb, sqlc.arg(omitted)::int)
ON CONFLICT (instance_uid) DO UPDATE
   SET replica_name = EXCLUDED.replica_name, written_at = EXCLUDED.written_at, downloads = EXCLUDED.downloads,
       omitted = EXCLUDED.omitted;

-- name: UpstreamDownloadLiveDelete :exec
-- The replica has no running download any more, or it shuts down (R-OPS-050).
DELETE FROM upstream_download_live WHERE instance_uid = sqlc.arg(instance_uid)::text::uuid;

-- name: UpstreamDownloadFinishedInsert :exec
-- Ended downloads of a publication (R-OPS-051), given as a JSON array of rows: the end a replica records replaces an earlier one of the
-- same id (an interrupted row the sweep wrote while the replica stalled). A download whose repository is gone is not written; a principal
-- that is gone reads as nobody. Names longer than the columns' bounds are cut.
INSERT INTO upstream_download_finished (id, repo_id, name, version, path, bytes, total, started_at, finished_at, duration_ms, outcome,
                                        failure_class, failure_code, origin, queue_origin, queue_row, by_principal, replica_name)
SELECT r.id, r.repo_id, left(r.name, 1024), left(r.version, 256), left(r.path, 4096), greatest(r.bytes, 0),
       CASE WHEN r.total >= 0 THEN r.total END, r.started_at, r.finished_at, greatest(r.duration_ms, 0), r.outcome,
       NULLIF(left(r.failure_class, 64), ''), NULLIF(left(r.failure_code, 128), ''), r.origin, NULLIF(r.queue_origin, ''),
       r.queue_row, p.id, left(r.replica_name, 253)
  FROM jsonb_to_recordset(sqlc.arg(rows)::text::jsonb) AS r(id uuid, repo_id bigint, name text, version text, path text, bytes bigint,
       total bigint, started_at timestamptz, finished_at timestamptz, duration_ms bigint, outcome text, failure_class text,
       failure_code text, origin text, queue_origin text, queue_row uuid, by_principal uuid, replica_name text)
  JOIN repositories rp ON rp.id = r.repo_id
  LEFT JOIN principal p ON p.id = r.by_principal
ON CONFLICT (id) DO UPDATE
   SET bytes = EXCLUDED.bytes, total = EXCLUDED.total, finished_at = EXCLUDED.finished_at, duration_ms = EXCLUDED.duration_ms,
       outcome = EXCLUDED.outcome, failure_class = EXCLUDED.failure_class, failure_code = EXCLUDED.failure_code,
       replica_name = EXCLUDED.replica_name;

-- name: UpstreamDownloadInterruptedInsert :exec
-- The downloads of a swept live row, ended interrupted (R-OPS-051): a recorded end of the same id is never overwritten.
INSERT INTO upstream_download_finished (id, repo_id, name, version, path, bytes, total, started_at, finished_at, duration_ms, outcome,
                                        failure_class, failure_code, origin, queue_origin, queue_row, by_principal, replica_name)
SELECT r.id, r.repo_id, left(r.name, 1024), left(r.version, 256), left(r.path, 4096), greatest(r.bytes, 0),
       CASE WHEN r.total >= 0 THEN r.total END, r.started_at, r.finished_at, greatest(r.duration_ms, 0), 'interrupted',
       NULL, NULL, r.origin, NULLIF(r.queue_origin, ''), r.queue_row, p.id, left(r.replica_name, 253)
  FROM jsonb_to_recordset(sqlc.arg(rows)::text::jsonb) AS r(id uuid, repo_id bigint, name text, version text, path text, bytes bigint,
       total bigint, started_at timestamptz, finished_at timestamptz, duration_ms bigint, origin text, queue_origin text,
       queue_row uuid, by_principal uuid, replica_name text)
  JOIN repositories rp ON rp.id = r.repo_id
  LEFT JOIN principal p ON p.id = r.by_principal
ON CONFLICT (id) DO NOTHING;

-- name: UpstreamDownloadFinishedTrim :exec
-- Keeps the newest 100 by (finished_at, id) (R-OPS-051): every row older than the 100th goes. With fewer rows the subquery is empty and
-- the comparison removes nothing.
DELETE FROM upstream_download_finished
 WHERE (finished_at, id) < (SELECT f.finished_at, f.id FROM upstream_download_finished f
                             ORDER BY f.finished_at DESC, f.id DESC OFFSET 99 LIMIT 1);

-- name: UpstreamDownloadSweep :many
-- The live rows of replicas that stopped publishing (R-OPS-051): removed, and returned with the time of their last write as Unix
-- microseconds, so that the caller records their downloads as interrupted in the same transaction.
DELETE FROM upstream_download_live
 WHERE written_at < now() - make_interval(secs => sqlc.arg(older_than_seconds)::double precision)
RETURNING instance_uid::text AS instance_uid, replica_name, (extract(epoch FROM written_at) * 1000000)::bigint AS written_us,
          downloads::text AS downloads;

-- name: UpstreamDownloadLiveList :many
-- Every live row with its age on PostgreSQL's clock in microseconds (R-OPS-050, R-OPS-052); the caller applies the freshness rule.
SELECT instance_uid::text AS instance_uid, replica_name, (extract(epoch FROM written_at) * 1000000)::bigint AS written_us,
       (extract(epoch FROM now() - written_at) * 1000000)::bigint AS age_us, downloads::text AS downloads, omitted
  FROM upstream_download_live
 ORDER BY instance_uid;

-- name: UpstreamDownloadFinishedList :many
-- The newest 100 ended downloads (R-OPS-051), as JSON text of their rows.
SELECT to_jsonb(f.*)::text AS doc
  FROM upstream_download_finished f
 ORDER BY f.finished_at DESC, f.id DESC
 LIMIT 100;

-- name: UpstreamDownloadNow :one
-- PostgreSQL's clock in Unix microseconds: the asOf of a read (R-OPS-052).
SELECT (extract(epoch FROM now()) * 1000000)::bigint AS now_us;

-- name: UpstreamDownloadRepositoryNames :many
-- The current project key, key and format of the repositories a read names (R-OPS-052); a removed one is absent.
SELECT r.id, p.key AS project, r.key, r.format
  FROM repositories r
  JOIN project p ON p.id = r.project_id
 WHERE r.id = ANY(sqlc.arg(ids)::bigint[]);

-- name: UpstreamDownloadPrincipalNames :many
-- The current names of the principals a read names (R-OPS-052); a removed one, anonymous and authenticated are absent.
SELECT id::text AS id, name
  FROM principal
 WHERE id = ANY(sqlc.arg(ids)::text[]::uuid[]) AND kind NOT IN ('anonymous', 'authenticated');

-- name: UpstreamDownloadLimited :many
-- The download queues an upstream's rate limit holds at the read (remote_download_limits of 00120, remote-curation.md R-CUR-011,
-- Q-CUR-176): what the panel says beside what runs. A limit is one row per repository that an upstream limits, so the statement reads
-- a handful of rows and counts each one's waiting rows by the index of the list (at most proxy.downloads.max_waiting_per_repository).
SELECT l.repo_id, p.key AS project, r.key, r.format, COALESCE(r.config -> 'remote' ->> 'url', '')::text AS url,
       (extract(epoch FROM l.since) * 1000000)::bigint AS since_us, (extract(epoch FROM l.until) * 1000000)::bigint AS until_us,
       l.hinted, COALESCE((l.failure ->> 'status')::int, 0)::int AS status,
       (SELECT count(*) FROM remote_download d WHERE d.repo_id = l.repo_id AND d.state = 'waiting')::bigint AS waiting
  FROM remote_download_limits l
  JOIN repositories r ON r.id = l.repo_id
  JOIN project p ON p.id = r.project_id
 WHERE l.until > now()
 ORDER BY l.since, l.repo_id
 LIMIT 100;
