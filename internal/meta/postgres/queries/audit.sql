-- Queries of the audit trail and the revocation journal (R-OPS-014 to R-OPS-017,
-- R-HA-054). The audit package is a domain package and imports no driver, so
-- every parameter is a plain Go type: identifiers and times travel as text and
-- are cast here, an absent optional filter is the empty string, and rows come
-- back with text identifiers and no NULLs.

-- name: InsertAuditEvents :many
-- One statement inserts a batch (a transactional event is a batch of one). The
-- batch is one JSON array of flat objects of text values, so the statement stays
-- one shape for any size. "at" is the event's time as RFC 3339 text; the empty
-- string means the transaction's now(), which is what a transactional event and
-- its journal row carry (R-HA-054). A repeated batch whose first attempt did
-- commit inserts nothing (the primary key), so a retry after an ambiguous
-- failure is harmless.
INSERT INTO audit_event (id, occurred_at, action, category, outcome, actor_type, actor_id, actor_name,
                         token_id, auth_method, source_ip, user_agent, trace_id, replica, endpoint,
                         resource_type, resource_id, repository, project, path, reason, details)
SELECT (e ->> 'id')::uuid, COALESCE(NULLIF(e ->> 'at', '')::timestamptz, now()), e ->> 'action',
       e ->> 'category', e ->> 'outcome', e ->> 'actor_type', NULLIF(e ->> 'actor_id', '')::uuid,
       NULLIF(e ->> 'actor_name', ''), NULLIF(e ->> 'token_id', '')::uuid, NULLIF(e ->> 'auth_method', ''),
       NULLIF(e ->> 'source_ip', '')::inet, NULLIF(e ->> 'user_agent', ''), NULLIF(e ->> 'trace_id', ''),
       NULLIF(e ->> 'replica', ''), NULLIF(e ->> 'endpoint', ''), NULLIF(e ->> 'resource_type', ''),
       NULLIF(e ->> 'resource_id', ''), NULLIF(e ->> 'repository', ''), NULLIF(e ->> 'project', ''), NULLIF(e ->> 'path', ''),
       NULLIF(e ->> 'reason', ''), e -> 'details'
  FROM jsonb_array_elements(sqlc.arg(events)::text::jsonb) AS e
ON CONFLICT (occurred_at, id) DO NOTHING
RETURNING id::text AS id, occurred_at;

-- name: InsertRevocationOutbox :execrows
-- The journal rows of flagged events, in the transaction of the change: each
-- event's id and occurred_at, its registry name and the producer's entry. One
-- statement inserts a batch, one JSON array of objects {id, at, event, entry},
-- so a change that records many flagged events (the tokens a disable revokes)
-- costs one statement, not one per event (R-HA-054).
INSERT INTO revocation_outbox (id, at, event, entry)
SELECT (r ->> 'id')::uuid, (r ->> 'at')::timestamptz, r ->> 'event', r -> 'entry'
  FROM jsonb_array_elements(sqlc.arg(entries)::text::jsonb) AS r;

-- name: ListAuditEventsNewest :many
-- repository is the qualified name <project>/<key> and project the project key (Q-AUTHZ-209).
-- Newest first, keyset after (cursor_at, cursor_id) when has_cursor. The empty
-- string is "no filter". actor_id is a UUID or the nil UUID when the actor was
-- given as a name; the action prefix is matched literally.
SELECT id::text AS id, occurred_at, action, category, outcome, actor_type,
       COALESCE(actor_id::text, '')::text AS actor_id, COALESCE(actor_name, '')::text AS actor_name,
       COALESCE(token_id::text, '')::text AS token_id, COALESCE(auth_method, '')::text AS auth_method,
       COALESCE(host(source_ip), '')::text AS source_ip, COALESCE(user_agent, '')::text AS user_agent,
       COALESCE(trace_id, '')::text AS trace_id, COALESCE(replica, '')::text AS replica,
       COALESCE(endpoint, '')::text AS endpoint, COALESCE(resource_type, '')::text AS resource_type,
       COALESCE(resource_id, '')::text AS resource_id, COALESCE(repository, '')::text AS repository, COALESCE(project, '')::text AS project,
       COALESCE(path, '')::text AS path, COALESCE(reason, '')::text AS reason, details
  FROM audit_event
 WHERE occurred_at >= sqlc.arg(from_at)::text::timestamptz
   AND occurred_at < sqlc.arg(to_at)::text::timestamptz
   AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action)::text)
   AND (sqlc.arg(action_prefix)::text = '' OR starts_with(action, sqlc.arg(action_prefix)::text))
   AND (sqlc.arg(category)::text = '' OR category = sqlc.arg(category)::text)
   AND (sqlc.arg(outcome)::text = '' OR outcome = sqlc.arg(outcome)::text)
   AND (sqlc.arg(actor)::text = '' OR actor_id = sqlc.arg(actor_id)::text::uuid OR actor_name = sqlc.arg(actor)::text)
   AND (sqlc.arg(repository)::text = '' OR repository = sqlc.arg(repository)::text)
   AND (sqlc.arg(project)::text = '' OR project = sqlc.arg(project)::text)
   AND (sqlc.arg(resource_type)::text = '' OR resource_type = sqlc.arg(resource_type)::text)
   AND (sqlc.arg(resource_id)::text = '' OR resource_id = sqlc.arg(resource_id)::text)
   AND (sqlc.arg(trace_id)::text = '' OR trace_id = sqlc.arg(trace_id)::text)
   AND (NOT sqlc.arg(has_cursor)::bool
        OR (occurred_at, id) < (sqlc.arg(cursor_at)::text::timestamptz, sqlc.arg(cursor_id)::text::uuid))
 ORDER BY occurred_at DESC, id DESC
 LIMIT sqlc.arg(row_limit)::int;

-- name: ListAuditEventsOldest :many
-- The same filters, oldest first (the export reads in this order).
SELECT id::text AS id, occurred_at, action, category, outcome, actor_type,
       COALESCE(actor_id::text, '')::text AS actor_id, COALESCE(actor_name, '')::text AS actor_name,
       COALESCE(token_id::text, '')::text AS token_id, COALESCE(auth_method, '')::text AS auth_method,
       COALESCE(host(source_ip), '')::text AS source_ip, COALESCE(user_agent, '')::text AS user_agent,
       COALESCE(trace_id, '')::text AS trace_id, COALESCE(replica, '')::text AS replica,
       COALESCE(endpoint, '')::text AS endpoint, COALESCE(resource_type, '')::text AS resource_type,
       COALESCE(resource_id, '')::text AS resource_id, COALESCE(repository, '')::text AS repository, COALESCE(project, '')::text AS project,
       COALESCE(path, '')::text AS path, COALESCE(reason, '')::text AS reason, details
  FROM audit_event
 WHERE occurred_at >= sqlc.arg(from_at)::text::timestamptz
   AND occurred_at < sqlc.arg(to_at)::text::timestamptz
   AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action)::text)
   AND (sqlc.arg(action_prefix)::text = '' OR starts_with(action, sqlc.arg(action_prefix)::text))
   AND (sqlc.arg(category)::text = '' OR category = sqlc.arg(category)::text)
   AND (sqlc.arg(outcome)::text = '' OR outcome = sqlc.arg(outcome)::text)
   AND (sqlc.arg(actor)::text = '' OR actor_id = sqlc.arg(actor_id)::text::uuid OR actor_name = sqlc.arg(actor)::text)
   AND (sqlc.arg(repository)::text = '' OR repository = sqlc.arg(repository)::text)
   AND (sqlc.arg(project)::text = '' OR project = sqlc.arg(project)::text)
   AND (sqlc.arg(resource_type)::text = '' OR resource_type = sqlc.arg(resource_type)::text)
   AND (sqlc.arg(resource_id)::text = '' OR resource_id = sqlc.arg(resource_id)::text)
   AND (sqlc.arg(trace_id)::text = '' OR trace_id = sqlc.arg(trace_id)::text)
   AND (NOT sqlc.arg(has_cursor)::bool
        OR (occurred_at, id) > (sqlc.arg(cursor_at)::text::timestamptz, sqlc.arg(cursor_id)::text::uuid))
 ORDER BY occurred_at ASC, id ASC
 LIMIT sqlc.arg(row_limit)::int;

-- name: GetAuditEvent :many
-- One event by id. The caller bounds occurred_at around the time a UUIDv7 id
-- carries, so only the partitions of that window are read.
SELECT id::text AS id, occurred_at, action, category, outcome, actor_type,
       COALESCE(actor_id::text, '')::text AS actor_id, COALESCE(actor_name, '')::text AS actor_name,
       COALESCE(token_id::text, '')::text AS token_id, COALESCE(auth_method, '')::text AS auth_method,
       COALESCE(host(source_ip), '')::text AS source_ip, COALESCE(user_agent, '')::text AS user_agent,
       COALESCE(trace_id, '')::text AS trace_id, COALESCE(replica, '')::text AS replica,
       COALESCE(endpoint, '')::text AS endpoint, COALESCE(resource_type, '')::text AS resource_type,
       COALESCE(resource_id, '')::text AS resource_id, COALESCE(repository, '')::text AS repository, COALESCE(project, '')::text AS project,
       COALESCE(path, '')::text AS path, COALESCE(reason, '')::text AS reason, details
  FROM audit_event
 WHERE id = sqlc.arg(id)::text::uuid
   AND occurred_at >= sqlc.arg(from_at)::text::timestamptz
   AND occurred_at <= sqlc.arg(to_at)::text::timestamptz
 LIMIT 1;

-- name: MaintainAuditPartitions :exec
-- Creates the partitions of the current and the next three months and drops
-- the expired ones (R-OPS-015). The function is SECURITY DEFINER; the runtime
-- role may execute it.
SELECT audit_maintain_partitions();

-- name: CountReadyAuditPartitions :one
-- How many of the partitions of the current and the next month exist (0 to 2).
SELECT ((to_regclass('audit_event_' || to_char(date_trunc('month', now() AT TIME ZONE 'UTC'), 'YYYYMM')) IS NOT NULL)::int
      + (to_regclass('audit_event_' || to_char(date_trunc('month', now() AT TIME ZONE 'UTC') + interval '1 month', 'YYYYMM')) IS NOT NULL)::int)::int AS ready;

-- name: GetAuditRetention :one
SELECT retention_days FROM audit_policy WHERE id = 1;

-- name: SetAuditRetention :execrows
-- Writes the configured retention; only the schema owner may (Q-OPS-4). It
-- touches the row only when the value changes.
UPDATE audit_policy
   SET retention_days = sqlc.arg(retention_days)::int, updated_at = now()
 WHERE id = 1 AND retention_days IS DISTINCT FROM sqlc.arg(retention_days)::int;

-- name: ListAuditRowsNewest :many
-- The rows of the audit log's screen (R-OPS-055): the events of ListAuditEventsNewest, newest first, leaving out every event of a
-- transfer (artifact.downloaded, bastion.data_key.handed) that names a unit and follows an event of its key (audit_download_key:
-- action, repository, unit, caller) by at most gap_seconds within the range and the trace filter. What stays is a single event, or the first event of a download, whose items ListAuditDownloadMembers
-- walks. The test of a predecessor is an index probe of audit_event_download_idx; it reads nothing of an event of another kind.
SELECT e.id::text AS id, e.occurred_at, e.action, e.category, e.outcome, e.actor_type,
       COALESCE(e.actor_id::text, '')::text AS actor_id, COALESCE(e.actor_name, '')::text AS actor_name,
       COALESCE(e.token_id::text, '')::text AS token_id, COALESCE(e.auth_method, '')::text AS auth_method,
       COALESCE(host(e.source_ip), '')::text AS source_ip, COALESCE(e.user_agent, '')::text AS user_agent,
       COALESCE(e.trace_id, '')::text AS trace_id, COALESCE(e.replica, '')::text AS replica,
       COALESCE(e.endpoint, '')::text AS endpoint, COALESCE(e.resource_type, '')::text AS resource_type,
       COALESCE(e.resource_id, '')::text AS resource_id, COALESCE(e.repository, '')::text AS repository, COALESCE(e.project, '')::text AS project,
       COALESCE(e.path, '')::text AS path, COALESCE(e.reason, '')::text AS reason, e.details
  FROM audit_event e
 WHERE e.occurred_at >= sqlc.arg(from_at)::text::timestamptz
   AND e.occurred_at < sqlc.arg(to_at)::text::timestamptz
   AND (sqlc.arg(action)::text = '' OR e.action = sqlc.arg(action)::text)
   AND (sqlc.arg(action_prefix)::text = '' OR starts_with(e.action, sqlc.arg(action_prefix)::text))
   AND (sqlc.arg(category)::text = '' OR e.category = sqlc.arg(category)::text)
   AND (sqlc.arg(outcome)::text = '' OR e.outcome = sqlc.arg(outcome)::text)
   AND (sqlc.arg(actor)::text = '' OR e.actor_id = sqlc.arg(actor_id)::text::uuid OR e.actor_name = sqlc.arg(actor)::text)
   AND (sqlc.arg(repository)::text = '' OR e.repository = sqlc.arg(repository)::text)
   AND (sqlc.arg(project)::text = '' OR e.project = sqlc.arg(project)::text)
   AND (sqlc.arg(resource_type)::text = '' OR e.resource_type = sqlc.arg(resource_type)::text)
   AND (sqlc.arg(resource_id)::text = '' OR e.resource_id = sqlc.arg(resource_id)::text)
   AND (sqlc.arg(trace_id)::text = '' OR e.trace_id = sqlc.arg(trace_id)::text)
   AND (NOT sqlc.arg(has_cursor)::bool
        OR (e.occurred_at, e.id) < (sqlc.arg(cursor_at)::text::timestamptz, sqlc.arg(cursor_id)::text::uuid))
   AND (e.action NOT IN ('artifact.downloaded', 'bastion.data_key.handed') OR (e.details ->> 'unit') IS NULL OR NOT EXISTS (
         SELECT 1
           FROM audit_event p
          WHERE p.action IN ('artifact.downloaded', 'bastion.data_key.handed') AND (p.details ->> 'unit') IS NOT NULL
            AND audit_download_key(p.action, p.repository, p.details ->> 'unit', p.actor_type, p.actor_id, p.token_id, p.source_ip)
              = audit_download_key(e.action, e.repository, e.details ->> 'unit', e.actor_type, e.actor_id, e.token_id, e.source_ip)
            AND p.occurred_at >= e.occurred_at - make_interval(secs => sqlc.arg(gap_seconds)::int)
            AND p.occurred_at >= sqlc.arg(from_at)::text::timestamptz
            AND (p.occurred_at, p.id) < (e.occurred_at, e.id)
            AND (sqlc.arg(trace_id)::text = '' OR p.trace_id = sqlc.arg(trace_id)::text)))
 ORDER BY e.occurred_at DESC, e.id DESC
 LIMIT sqlc.arg(row_limit)::int;

-- name: ListAuditDownloadMembers :many
-- The items of downloads (R-OPS-055): from each start event of starts (a JSON array of {"ord", "id", "at"}, at as RFC 3339 text)
-- the events of its key in time order, each the next one after the one before within gap_seconds, before to_at and within the trace
-- filter, at most member_limit + 1 per start, so that the caller learns that a download goes on beyond what it reads. Each step is
-- one probe of audit_event_download_idx; the walk stops at the first pause longer than the gap. n is the position from the start
-- event (1); the events come back whole, read by their primary key.
WITH RECURSIVE chain AS (
    SELECT (s ->> 'ord')::int AS ord, e.occurred_at, e.id, 1 AS n,
           audit_download_key(e.action, e.repository, e.details ->> 'unit', e.actor_type, e.actor_id, e.token_id, e.source_ip) AS k
      FROM jsonb_array_elements(sqlc.arg(starts)::text::jsonb) AS s
      JOIN audit_event e ON e.occurred_at = (s ->> 'at')::timestamptz AND e.id = (s ->> 'id')::uuid
     WHERE e.action IN ('artifact.downloaded', 'bastion.data_key.handed') AND (e.details ->> 'unit') IS NOT NULL
    UNION ALL
    SELECT c.ord, nx.occurred_at, nx.id, c.n + 1, c.k
      FROM chain c
      CROSS JOIN LATERAL (
          SELECT m.occurred_at, m.id
            FROM audit_event m
           WHERE m.action IN ('artifact.downloaded', 'bastion.data_key.handed') AND (m.details ->> 'unit') IS NOT NULL
             AND audit_download_key(m.action, m.repository, m.details ->> 'unit', m.actor_type, m.actor_id, m.token_id, m.source_ip) = c.k
             AND (m.occurred_at, m.id) > (c.occurred_at, c.id)
             AND m.occurred_at <= c.occurred_at + make_interval(secs => sqlc.arg(gap_seconds)::int)
             AND m.occurred_at < sqlc.arg(to_at)::text::timestamptz
             AND (sqlc.arg(trace_id)::text = '' OR m.trace_id = sqlc.arg(trace_id)::text)
           ORDER BY m.occurred_at, m.id
           LIMIT 1) nx
     WHERE c.n <= sqlc.arg(member_limit)::int
)
SELECT c.ord::int AS ord, c.n::int AS n, c.k::text AS key,
       a.id::text AS id, a.occurred_at, a.action, a.category, a.outcome, a.actor_type,
       COALESCE(a.actor_id::text, '')::text AS actor_id, COALESCE(a.actor_name, '')::text AS actor_name,
       COALESCE(a.token_id::text, '')::text AS token_id, COALESCE(a.auth_method, '')::text AS auth_method,
       COALESCE(host(a.source_ip), '')::text AS source_ip, COALESCE(a.user_agent, '')::text AS user_agent,
       COALESCE(a.trace_id, '')::text AS trace_id, COALESCE(a.replica, '')::text AS replica,
       COALESCE(a.endpoint, '')::text AS endpoint, COALESCE(a.resource_type, '')::text AS resource_type,
       COALESCE(a.resource_id, '')::text AS resource_id, COALESCE(a.repository, '')::text AS repository, COALESCE(a.project, '')::text AS project,
       COALESCE(a.path, '')::text AS path, COALESCE(a.reason, '')::text AS reason, a.details
  FROM chain c
  JOIN audit_event a ON a.occurred_at = c.occurred_at AND a.id = c.id
 ORDER BY c.ord, c.n;

-- name: AuditDownloadStartKey :one
-- The key of the download whose first event is id at at (R-OPS-055), as text: a download event that names a unit and has no event of
-- its key before it within gap_seconds; '' for any other event or none. The items of a download are read from its first event only.
SELECT COALESCE((
    SELECT audit_download_key(e.action, e.repository, e.details ->> 'unit', e.actor_type, e.actor_id, e.token_id, e.source_ip)::text
      FROM audit_event e
     WHERE e.occurred_at = sqlc.arg(at)::text::timestamptz AND e.id = sqlc.arg(id)::text::uuid
       AND e.action IN ('artifact.downloaded', 'bastion.data_key.handed') AND (e.details ->> 'unit') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
             FROM audit_event p
            WHERE p.action IN ('artifact.downloaded', 'bastion.data_key.handed') AND (p.details ->> 'unit') IS NOT NULL
              AND audit_download_key(p.action, p.repository, p.details ->> 'unit', p.actor_type, p.actor_id, p.token_id, p.source_ip)
                = audit_download_key(e.action, e.repository, e.details ->> 'unit', e.actor_type, e.actor_id, e.token_id, e.source_ip)
              AND p.occurred_at >= e.occurred_at - make_interval(secs => sqlc.arg(gap_seconds)::int)
              AND (p.occurred_at, p.id) < (e.occurred_at, e.id))
), '')::text AS key;
