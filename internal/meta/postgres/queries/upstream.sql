-- Queries of the built-in project upstream (repositories-and-proxy.md R-REPO-066 to R-REPO-073): the rows of upstream_repository
-- beside the repository rows of the project. The repository package writes them in the transaction of the repository write they
-- belong to; the facts are read with every snapshot of the repositories (Repository.Upstream). Statements that return at most one
-- row are :many, so the caller never meets the driver's no-rows error.

-- name: ListUpstreamRepositoryFacts :many
-- Every row, for the snapshot and the lists: the facts a repository of the project carries (its kind and what its state is
-- derived from, Q-REPO-375).
SELECT * FROM upstream_repository ORDER BY repository_id;

-- name: GetUpstreamRepositoryFacts :many
SELECT * FROM upstream_repository WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: InsertUpstreamRepository :exec
-- The row of a repository the reconcile creates (R-REPO-067 step 1), in the transaction of the repository row: the deferred guard
-- AI010 refuses the commit of a repository of the project without it.
INSERT INTO upstream_repository (repository_id, kind)
VALUES (sqlc.arg(repository_id)::bigint, sqlc.arg(kind)::text);

-- name: MarkUpstreamSwitchedOn :exec
-- Switching on (R-REPO-068): the first switch-on is kept, a later one changes nothing of it.
UPDATE upstream_repository
   SET first_switched_on_at = COALESCE(first_switched_on_at, now())
 WHERE repository_id = sqlc.arg(repository_id)::bigint AND kind = 'preset';

-- name: SetUpstreamClientsEnabled :exec
-- The switch "ClamAV definition updates" of a system repository (R-REPO-075, Q-REPO-414); the CHECK of migration 00094 keeps it to a
-- system row.
UPDATE upstream_repository
   SET clients_enabled = sqlc.arg(enabled)::boolean
 WHERE repository_id = sqlc.arg(repository_id)::bigint AND kind = 'system';

-- name: SetUpstreamRetired :exec
-- The product no longer declares the repository (R-REPO-067 step 5); the first retirement's time is kept.
UPDATE upstream_repository
   SET retired_at = COALESCE(retired_at, now())
 WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: ClearUpstreamRetired :exec
-- The product declares the repository again (R-REPO-067 step 4).
UPDATE upstream_repository SET retired_at = NULL WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: SetUpstreamAddressOverridden :exec
-- Whether the stored address differs from the product's (R-REPO-069, R-REPO-067 step 3).
UPDATE upstream_repository
   SET address_overridden = sqlc.arg(overridden)::boolean
 WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: SetUpstreamAccountRemoved :exec
-- The reconcile removed the account when the product moved its hosts (Q-REPO-377).
UPDATE upstream_repository SET account_removed_at = now() WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: ClearUpstreamAccountRemoved :exec
-- An administrator entered an account again (R-REPO-069).
UPDATE upstream_repository SET account_removed_at = NULL WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: SetUpstreamAccountConfigured :exec
-- quader migrate applied a declared account (R-REPO-077); the first application's time is kept.
UPDATE upstream_repository
   SET account_configured_at = COALESCE(account_configured_at, now())
 WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: ClearUpstreamAccountConfigured :exec
-- The configuration no longer declares the account (R-REPO-077).
UPDATE upstream_repository SET account_configured_at = NULL WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: UpstreamRepositoryLastDownloads :many
-- The last completed download of a repository and its last failed one, from the installation's last 100 (R-OPS-051): the account
-- section of a well-known repository's page (R-REPO-073 account.lastDownloadAt and account.lastFailure). No row when none is recent.
SELECT (max(finished_at) FILTER (WHERE outcome = 'completed'))::timestamptz AS last_download_at,
       ((array_agg(finished_at ORDER BY finished_at DESC) FILTER (WHERE outcome = 'failed'))[1])::timestamptz AS last_failure_at,
       -- '' without a failed one: the aggregate of no row is NULL, which the row's string does not take (seen 2026-11-25: every read
       -- of a registry whose recent downloads all completed answered 500).
       COALESCE(((array_agg(COALESCE(failure_code, '') ORDER BY finished_at DESC) FILTER (WHERE outcome = 'failed'))[1]), '')::text
         AS last_failure_code
  FROM upstream_download_finished
 WHERE repo_id = sqlc.arg(repository_id)::bigint
HAVING count(*) > 0;

-- name: PurgeUpstreamRepository :exec
-- The last transaction of the purge of a retired repository (R-REPO-067 step 5): what the removal of its row would have taken with
-- it, while the row stays (upstream_repository_purge, last replaced by 00096).
SELECT upstream_repository_purge(sqlc.arg(repository_id)::bigint);

-- name: CountUpstreamListedBy :many
-- For each repository of the project, the number of virtual repositories whose member lists or pin rules name it and the number
-- of their projects (R-REPO-073 listedBy and listedByProjects, Q-API-363). A repository nothing names has no row.
SELECT m.member_id::bigint AS repository_id, count(DISTINCT m.virtual_id)::bigint AS listed_by,
       count(DISTINCT v.project_id)::bigint AS listed_by_projects
  FROM (SELECT virtual_id, member_id FROM virtual_members
        UNION ALL
        SELECT virtual_id, member_id FROM virtual_pin_members) m
  JOIN upstream_repository u ON u.repository_id = m.member_id
  JOIN repositories v ON v.id = m.virtual_id
 GROUP BY m.member_id;

-- name: UpstreamListedByNames :many
-- The virtual repositories whose member lists or pin rules name a repository, by qualified name in byte order, at most limit of
-- them (repository.disabled's listedBy, R-REPO-068, Q-REPO-383).
SELECT l.project_key::text AS project_key, l.repository_key::text AS repository_key
  FROM (SELECT DISTINCT p.key AS project_key, v.key AS repository_key
          FROM (SELECT virtual_id, member_id FROM virtual_members
                UNION ALL
                SELECT virtual_id, member_id FROM virtual_pin_members) m
          JOIN repositories v ON v.id = m.virtual_id
          JOIN project p ON p.id = v.project_id
         WHERE m.member_id = sqlc.arg(repository_id)::bigint) l
 ORDER BY l.project_key COLLATE "C", l.repository_key COLLATE "C"
 LIMIT sqlc.arg(row_limit)::int;
