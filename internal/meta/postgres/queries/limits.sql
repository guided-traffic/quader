-- The store of the download limits (usage-and-limits.md R-LIM-010 to R-LIM-016, R-LIM-025, R-LIM-026; internal/limits/store).
-- IDs travel as text and figures as a JSON object of column names, so the domain package never imports the driver types; a
-- figure that is absent or null in the object is NULL, unlimited.

-- name: LimitProfiles :many
SELECT id::text AS id, key, name, description, builtin,
       requests_per_second, requests_burst, requests_per_hour, cached_bytes_per_second, cached_bytes_per_hour,
       uncached_bytes_per_second, uncached_bytes_per_hour, upstream_requests_per_hour, parallel_transfers, bytes_per_day,
       refused_per_hour, version, created_at, COALESCE(created_by::text, '')::text AS created_by, updated_at,
       COALESCE(updated_by::text, '')::text AS updated_by
  FROM limit_profile
 ORDER BY key COLLATE "C";

-- name: LimitProfileUsage :many
-- How many rules and project maxima name each profile (R-LIM-031 usedBy).
SELECT p.key,
       (SELECT count(*) FROM limit_assignment a WHERE a.profile_id = p.id)::bigint AS assignments,
       (SELECT count(*) FROM limit_project_maximum m WHERE m.profile_id = p.id)::bigint AS maxima
  FROM limit_profile p;

-- name: InsertLimitProfile :exec
INSERT INTO limit_profile (id, key, name, description, builtin,
                           requests_per_second, requests_burst, requests_per_hour, cached_bytes_per_second,
                           cached_bytes_per_hour, uncached_bytes_per_second, uncached_bytes_per_hour,
                           upstream_requests_per_hour, parallel_transfers, bytes_per_day, refused_per_hour,
                           created_by, updated_by)
SELECT sqlc.arg(id)::text::uuid, sqlc.arg(key), sqlc.arg(name), sqlc.arg(description), false,
       (f ->> 'requests_per_second')::bigint, (f ->> 'requests_burst')::bigint, (f ->> 'requests_per_hour')::bigint,
       (f ->> 'cached_bytes_per_second')::bigint, (f ->> 'cached_bytes_per_hour')::bigint,
       (f ->> 'uncached_bytes_per_second')::bigint, (f ->> 'uncached_bytes_per_hour')::bigint,
       (f ->> 'upstream_requests_per_hour')::bigint, (f ->> 'parallel_transfers')::bigint, (f ->> 'bytes_per_day')::bigint,
       (f ->> 'refused_per_hour')::bigint,
       NULLIF(sqlc.arg(actor)::text, '')::uuid, NULLIF(sqlc.arg(actor)::text, '')::uuid
  FROM (SELECT sqlc.arg(figures)::text::jsonb AS f) x;

-- name: UpdateLimitProfile :execrows
-- Replaces name, description and figures when the version matches (R-API-012).
UPDATE limit_profile p
   SET name = sqlc.arg(name), description = sqlc.arg(description),
       requests_per_second = (f ->> 'requests_per_second')::bigint, requests_burst = (f ->> 'requests_burst')::bigint,
       requests_per_hour = (f ->> 'requests_per_hour')::bigint,
       cached_bytes_per_second = (f ->> 'cached_bytes_per_second')::bigint, cached_bytes_per_hour = (f ->> 'cached_bytes_per_hour')::bigint,
       uncached_bytes_per_second = (f ->> 'uncached_bytes_per_second')::bigint,
       uncached_bytes_per_hour = (f ->> 'uncached_bytes_per_hour')::bigint,
       upstream_requests_per_hour = (f ->> 'upstream_requests_per_hour')::bigint,
       parallel_transfers = (f ->> 'parallel_transfers')::bigint, bytes_per_day = (f ->> 'bytes_per_day')::bigint,
       refused_per_hour = (f ->> 'refused_per_hour')::bigint,
       version = p.version + 1, updated_at = now(), updated_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
  FROM (SELECT sqlc.arg(figures)::text::jsonb AS f) x
 WHERE p.key = sqlc.arg(key) AND p.version = sqlc.arg(version);

-- name: DeleteLimitProfile :execrows
DELETE FROM limit_profile WHERE key = sqlc.arg(key) AND version = sqlc.arg(version) AND NOT builtin;

-- name: LimitAssignments :many
-- Every rule with the names of its place, the project's ID of a repository rule included (R-LIM-013: the index keys places
-- by name and the budget key by ID).
SELECT a.id::text AS id, a.place_kind, COALESCE(a.project_id::text, '')::text AS project_id,
       COALESCE(a.repository_id, 0)::bigint AS repository_id,
       COALESCE(pr.key, rp.key, '')::text AS project_key, COALESCE(r.key, '')::text AS repo_key,
       COALESCE(r.project_id::text, '')::text AS repo_project_id,
       a.who_kind, a.who_ref, p.key AS profile, a.overrides, a.capped, a.builtin, a.note, a.version,
       a.created_at, COALESCE(a.created_by::text, '')::text AS created_by, a.updated_at,
       COALESCE(a.updated_by::text, '')::text AS updated_by
  FROM limit_assignment a
  JOIN limit_profile p ON p.id = a.profile_id
  LEFT JOIN project pr ON pr.id = a.project_id
  LEFT JOIN repositories r ON r.id = a.repository_id
  LEFT JOIN project rp ON rp.id = r.project_id
 ORDER BY a.place_kind, a.created_at, a.id;

-- name: InsertLimitAssignment :exec
INSERT INTO limit_assignment (id, place_kind, project_id, repository_id, who_kind, who_ref, profile_id, overrides, capped,
                              note, created_by, updated_by)
SELECT sqlc.arg(id)::text::uuid, sqlc.arg(place_kind), NULLIF(sqlc.arg(project_id)::text, '')::uuid,
       NULLIF(sqlc.arg(repository_id)::bigint, 0), sqlc.arg(who_kind), sqlc.arg(who_ref), p.id,
       sqlc.arg(overrides)::text::jsonb, sqlc.arg(capped), sqlc.arg(note),
       NULLIF(sqlc.arg(actor)::text, '')::uuid, NULLIF(sqlc.arg(actor)::text, '')::uuid
  FROM limit_profile p WHERE p.key = sqlc.arg(profile);

-- name: UpdateLimitAssignment :execrows
UPDATE limit_assignment a
   SET profile_id = p.id, overrides = sqlc.arg(overrides)::text::jsonb, capped = sqlc.arg(capped), note = sqlc.arg(note),
       version = a.version + 1, updated_at = now(), updated_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
  FROM limit_profile p
 WHERE p.key = sqlc.arg(profile) AND a.id = sqlc.arg(id)::text::uuid AND a.version = sqlc.arg(version);

-- name: DeleteLimitAssignment :execrows
DELETE FROM limit_assignment WHERE id = sqlc.arg(id)::text::uuid AND version = sqlc.arg(version) AND NOT builtin;

-- name: LimitCeilings :many
SELECT repository_id, class, uncached_bytes_per_hour, upstream_requests_per_hour, version, updated_at,
       COALESCE(updated_by::text, '')::text AS updated_by
  FROM limit_ceiling
 ORDER BY repository_id, class;

-- name: UpsertLimitCeiling :exec
INSERT INTO limit_ceiling (repository_id, class, uncached_bytes_per_hour, upstream_requests_per_hour, updated_by)
VALUES (sqlc.arg(repository_id), sqlc.arg(class), NULLIF(sqlc.arg(uncached_bytes_per_hour)::bigint, 0),
        NULLIF(sqlc.arg(upstream_requests_per_hour)::bigint, 0), NULLIF(sqlc.arg(actor)::text, '')::uuid)
ON CONFLICT (repository_id, class) DO UPDATE
   SET uncached_bytes_per_hour = EXCLUDED.uncached_bytes_per_hour,
       upstream_requests_per_hour = EXCLUDED.upstream_requests_per_hour,
       version = limit_ceiling.version + 1, updated_at = now(), updated_by = EXCLUDED.updated_by;

-- name: LimitProjectMaxima :many
SELECT m.project_id::text AS project_id, pr.key AS project_key, p.key AS profile, m.overrides, m.version, m.updated_at,
       COALESCE(m.updated_by::text, '')::text AS updated_by
  FROM limit_project_maximum m
  JOIN limit_profile p ON p.id = m.profile_id
  JOIN project pr ON pr.id = m.project_id
 ORDER BY pr.key COLLATE "C";

-- name: UpsertLimitProjectMaximum :execrows
-- Sets or replaces a project's maximum; version 0 creates, any other replaces when it matches.
INSERT INTO limit_project_maximum (project_id, profile_id, overrides, updated_by)
SELECT sqlc.arg(project_id)::text::uuid, p.id, sqlc.arg(overrides)::text::jsonb, NULLIF(sqlc.arg(actor)::text, '')::uuid
  FROM limit_profile p WHERE p.key = sqlc.arg(profile)
ON CONFLICT (project_id) DO UPDATE
   SET profile_id = EXCLUDED.profile_id, overrides = EXCLUDED.overrides, version = limit_project_maximum.version + 1,
       updated_at = now(), updated_by = EXCLUDED.updated_by
 WHERE limit_project_maximum.version = sqlc.arg(version);

-- name: DeleteLimitProjectMaximum :execrows
DELETE FROM limit_project_maximum WHERE project_id = sqlc.arg(project_id)::text::uuid AND version = sqlc.arg(version);

-- name: LimitSettings :one
SELECT mode, block_seconds, version, updated_at, COALESCE(updated_by::text, '')::text AS updated_by
  FROM limit_settings WHERE id;

-- name: UpdateLimitSettings :execrows
UPDATE limit_settings
   SET mode = sqlc.arg(mode), block_seconds = sqlc.arg(block_seconds), version = version + 1, updated_at = now(),
       updated_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
 WHERE id AND version = sqlc.arg(version);

-- name: InsertLimitBlock :exec
INSERT INTO limit_block (id, subject_kind, subject_ref, place, started_at, until, refused_count, refused_limit)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(subject_kind), sqlc.arg(subject_ref), sqlc.arg(place), sqlc.arg(started_at)::text::timestamptz,
        sqlc.arg(until)::text::timestamptz, sqlc.arg(refused_count), sqlc.arg(refused_limit))
ON CONFLICT (id) DO NOTHING;

-- name: LimitBlocks :many
-- Blocks, newest first; active ones only when asked; one place, or every place when it is empty.
SELECT id::text AS id, subject_kind, subject_ref, place, started_at, until, refused_count, refused_limit, lifted_at,
       COALESCE(lifted_by::text, '')::text AS lifted_by
  FROM limit_block
 WHERE (NOT sqlc.arg(active_only)::boolean OR (lifted_at IS NULL AND until > now()))
   AND (sqlc.arg(place)::text = '' OR place = sqlc.arg(place)::text OR place = ANY(sqlc.arg(places)::text[]))
 ORDER BY started_at DESC
 LIMIT sqlc.arg(row_limit);

-- name: LimitBlock :one
SELECT id::text AS id, subject_kind, subject_ref, place, started_at, until, refused_count, refused_limit, lifted_at,
       COALESCE(lifted_by::text, '')::text AS lifted_by
  FROM limit_block WHERE id = sqlc.arg(id)::text::uuid;

-- name: LiftLimitBlock :execrows
UPDATE limit_block SET lifted_at = now(), lifted_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
 WHERE id = sqlc.arg(id)::text::uuid AND lifted_at IS NULL AND until > now();

-- name: ActiveLimitBlocks :many
-- The active blocks of subjects, for the state of the overview (R-LIM-034).
SELECT subject_kind, subject_ref, place, max(until)::timestamptz AS until
  FROM limit_block
 WHERE lifted_at IS NULL AND until > now()
 GROUP BY subject_kind, subject_ref, place;

-- name: LimitUserNames :many
-- The names of the users a page of the limits or the usage names (R-LIM-032, R-LIM-034).
SELECT p.id::text AS id, p.name, COALESCE(u.display_name, '')::text AS display_name
  FROM principal p
  LEFT JOIN app_user u ON u.principal_id = p.id
 WHERE p.id = ANY(sqlc.arg(ids)::text[]::uuid[]);

-- name: LimitTechnicalTokenNames :many
-- The names and accounts of technical tokens by ID (R-LIM-032, R-LIM-034); the account's principal name is its reference
-- without the sa: prefix.
SELECT t.id::text AS id, t.name, p.name AS account
  FROM technical_token t
  JOIN principal p ON p.id = t.service_account_id
 WHERE t.id = ANY(sqlc.arg(ids)::text[]::uuid[]);

-- name: LimitSearchTechnicalTokens :many
-- Technical tokens for the rule dialog (R-LIM-034): by the token's or its account's name; a project narrows to the accounts of
-- the project and of its repositories.
SELECT t.id::text AS id, t.name, p.name AS account
  FROM technical_token t
  JOIN principal p ON p.id = t.service_account_id
 WHERE (sqlc.arg(q)::text = '' OR strpos(lower(t.name), lower(sqlc.arg(q)::text)) > 0
        OR strpos(lower(p.name), lower(sqlc.arg(q)::text)) > 0)
   AND (sqlc.arg(project)::text = '' OR p.name LIKE sqlc.arg(project)::text || '/%')
 ORDER BY t.name COLLATE "C", p.name COLLATE "C"
 LIMIT sqlc.arg(row_limit);

-- name: LimitCIProjects :many
-- The CI projects that tokens were issued for (R-LIM-034), with the project path the newest issuance named: by issuer and
-- project ID, or by a text in the path or equal to the ID. refs narrows to issuer:project pairs when not empty.
SELECT DISTINCT ON (i.issuer_id, i.project_id)
       i.issuer_id::text AS issuer_id, c.name AS issuer, i.project_id, COALESCE(i.project_path, '')::text AS project_path
  FROM ci_token_issuances i
  JOIN ci_issuers c ON c.id = i.issuer_id
 WHERE (cardinality(sqlc.arg(refs)::text[]) = 0 OR i.issuer_id::text || ':' || i.project_id = ANY(sqlc.arg(refs)::text[]))
   AND (sqlc.arg(q)::text = '' OR strpos(lower(COALESCE(i.project_path, '')), lower(sqlc.arg(q)::text)) > 0
        OR i.project_id = sqlc.arg(q)::text)
 ORDER BY i.issuer_id, i.project_id, i.issued_at DESC
 LIMIT sqlc.arg(row_limit);

-- name: LimitTokenHints :many
-- The hint and description of personal tokens by ID, for a user's usage per credential (R-LIM-034).
SELECT id::text AS id, hint, description FROM api_token WHERE id = ANY(sqlc.arg(ids)::text[]::uuid[]);
