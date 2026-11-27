-- Queries of what a Bastion may distribute (bastion.md R-BAS-134, migration 00159): the projects an administrator allows per
-- Bastion (bastion_project) and the Bastions a project's Owner chooses per repository (repository_bastion). The database refuses a
-- choice whose project the Bastion does not allow (AI011) and deletes the choices with a project taken off the list or a repository
-- moved; the service checks first, so that an API caller gets 422 bastion_not_allowed at the member instead.

-- name: BastionDistributionShare :many
-- The registration by name, its principal row locked FOR SHARE: an allow or a removal waits for a deletion of the registration in
-- flight, and a deletion waits for them.
SELECT p.id::text AS id, p.name
  FROM principal p JOIN bastion b ON b.principal_id = p.id
 WHERE p.kind = 'bastion' AND p.name = sqlc.arg(name)::text
   FOR SHARE OF p;

-- name: BastionDistributionProjectShare :many
-- A project by key, locked FOR SHARE so that it is not deleted while it is allowed.
SELECT id::text AS id, key, display_name FROM project WHERE key = sqlc.arg(key)::text FOR SHARE;

-- name: BastionProjectAllow :many
-- Allows a project for a Bastion; no row when it is allowed already (the PUT is idempotent and records nothing then).
INSERT INTO bastion_project (bastion_id, project_id, allowed_by)
VALUES (sqlc.arg(bastion_id)::text::uuid, sqlc.arg(project_id)::text::uuid, sqlc.arg(allowed_by)::text)
ON CONFLICT (bastion_id, project_id) DO NOTHING
RETURNING allowed_at;

-- name: BastionProjectChoices :many
-- The repositories of a project that chose a Bastion, by key: what a removal of the project from the Bastion's list takes with it.
SELECT r.id, r.key
  FROM repository_bastion rb JOIN repositories r ON r.id = rb.repository_id
 WHERE rb.bastion_id = sqlc.arg(bastion_id)::text::uuid AND r.project_id = sqlc.arg(project_id)::text::uuid
 ORDER BY r.key;

-- name: BastionProjectRemove :many
-- Takes a project off a Bastion's list; the trigger bastion_project_removed deletes its repositories' choices for the Bastion in the
-- same statement. No row when the project was not allowed.
DELETE FROM bastion_project
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid AND project_id = sqlc.arg(project_id)::text::uuid
RETURNING project_id::text AS project_id;

-- name: BastionProjectList :many
-- One page of the projects a Bastion allows, by project key after the cursor's key: each with its display name, since when and by whom
-- it is allowed (the actor's name where the reference is a principal that exists), and the keys of its repositories that chose the
-- Bastion. A query matches the key or the display name, case-insensitively.
SELECT p.key, p.display_name, bp.allowed_at,
       COALESCE((SELECT a.name FROM principal a WHERE a.id::text = bp.allowed_by), bp.allowed_by)::text AS allowed_by,
       COALESCE((SELECT array_agg(r.key ORDER BY r.key)
                   FROM repository_bastion rb JOIN repositories r ON r.id = rb.repository_id
                  WHERE rb.bastion_id = bp.bastion_id AND r.project_id = bp.project_id), '{}')::text[] AS repositories
  FROM bastion_project bp JOIN project p ON p.id = bp.project_id
 WHERE bp.bastion_id = sqlc.arg(bastion_id)::text::uuid
   AND p.key > sqlc.arg(after)::text
   AND (sqlc.arg(query)::text = '' OR strpos(lower(p.key), lower(sqlc.arg(query)::text)) > 0
        OR strpos(lower(p.display_name), lower(sqlc.arg(query)::text)) > 0)
 ORDER BY p.key
 LIMIT sqlc.arg(max_rows)::int;

-- name: BastionProjectCount :one
-- The number of projects a Bastion allows and of the repositories distributed through it.
SELECT (SELECT count(*) FROM bastion_project WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid)::bigint AS projects,
       (SELECT count(*) FROM repository_bastion WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid)::bigint AS repositories;

-- name: ProjectBastionsOffered :many
-- The Bastions that allow a project, by name: what its Owner may choose for its repositories. disabled says the registration is disabled, soft or hard (R-BAS-133);
-- gui its web interface's switch; base_urls the distinct base URLs its replicas reported within the last 24 hours (one per
-- deployment), which the API holds to server.additional_base_urls.
SELECT p.name, (b.disabled IS NOT NULL)::boolean AS disabled, b.gui,
       COALESCE((SELECT array_agg(DISTINCT c.base_url ORDER BY c.base_url) FROM bastion_contact c
                  WHERE c.bastion_id = b.principal_id AND c.base_url <> ''
                    AND c.contact_at > now() - interval '24 hours'), '{}')::text[] AS base_urls
  FROM bastion_project bp JOIN bastion b ON b.principal_id = bp.bastion_id JOIN principal p ON p.id = b.principal_id
 WHERE bp.project_id = (SELECT id FROM project WHERE key = sqlc.arg(project)::text)
 ORDER BY p.name;

-- name: RepositoryBastionsOf :many
-- The Bastions that distribute the given repositories, by repository and name, with the registration's state and the base URLs its
-- replicas reported within the last 24 hours (as ProjectBastionsOffered). A row exists only while the Bastion allows the
-- repository's project (00159), so these are the effective rows.
SELECT rb.repository_id, p.name, (b.disabled IS NOT NULL)::boolean AS disabled,
       COALESCE((SELECT array_agg(DISTINCT c.base_url ORDER BY c.base_url) FROM bastion_contact c
                  WHERE c.bastion_id = rb.bastion_id AND c.base_url <> ''
                    AND c.contact_at > now() - interval '24 hours'), '{}')::text[] AS base_urls
  FROM repository_bastion rb JOIN principal p ON p.id = rb.bastion_id JOIN bastion b ON b.principal_id = rb.bastion_id
 WHERE rb.repository_id = ANY(sqlc.arg(ids)::bigint[])
 ORDER BY rb.repository_id, p.name;

-- name: RepositoryBastionsAll :many
-- RepositoryBastionsOf for every repository: the list's one read (R-REPO-012).
SELECT rb.repository_id, p.name, (b.disabled IS NOT NULL)::boolean AS disabled,
       COALESCE((SELECT array_agg(DISTINCT c.base_url ORDER BY c.base_url) FROM bastion_contact c
                  WHERE c.bastion_id = rb.bastion_id AND c.base_url <> ''
                    AND c.contact_at > now() - interval '24 hours'), '{}')::text[] AS base_urls
  FROM repository_bastion rb JOIN principal p ON p.id = rb.bastion_id JOIN bastion b ON b.principal_id = rb.bastion_id
 ORDER BY rb.repository_id, p.name;

-- name: RepositoryBastionsAllowed :many
-- Of the names given, the Bastions that allow the project: a choice of the repository's Owner is checked against it before the rows
-- are written.
SELECT p.id::text AS id, p.name
  FROM bastion_project bp JOIN principal p ON p.id = bp.bastion_id
 WHERE bp.project_id = sqlc.arg(project_id)::text::uuid AND p.name = ANY(sqlc.arg(names)::text[])
   FOR SHARE OF bp;

-- name: RepositoryBastionAdd :exec
INSERT INTO repository_bastion (repository_id, bastion_id)
VALUES (sqlc.arg(repository_id)::bigint, sqlc.arg(bastion_id)::text::uuid)
ON CONFLICT (repository_id, bastion_id) DO NOTHING;

-- name: RepositoryBastionRemove :many
-- Takes a Bastion off a repository; no row when it did not distribute it.
DELETE FROM repository_bastion rb
 USING principal p
 WHERE rb.repository_id = sqlc.arg(repository_id)::bigint AND p.id = rb.bastion_id AND p.name = sqlc.arg(name)::text
RETURNING rb.bastion_id::text AS bastion_id;

-- name: RepositoryBastionRemoveByID :many
-- The replay of repository.bastion.removed (Q-BAS-521): the row by the IDs the journal entry carries.
DELETE FROM repository_bastion
 WHERE repository_id = sqlc.arg(repository_id)::bigint AND bastion_id = sqlc.arg(bastion_id)::text::uuid
RETURNING repository_id;

-- name: RepositoryBastionsOfMove :many
-- The Bastions a repository chose, by ID and name: what its move to another project takes with it (R-REPO-060), read in the move's
-- transaction before the trigger repository_bastion_moved deletes them.
SELECT rb.bastion_id::text AS bastion_id, p.name
  FROM repository_bastion rb JOIN principal p ON p.id = rb.bastion_id
 WHERE rb.repository_id = sqlc.arg(repository_id)::bigint
 ORDER BY p.name;
