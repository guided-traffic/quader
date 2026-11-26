-- The statements of the facts of the roles a remote Ansible Galaxy repository read (internal/formatdata/ansible; R-ANS-056): the table
-- ansible_roles of migration ansible_roles. A time travels as the column's own type: the table is read and written by this package alone.

-- name: AnsibleRolesPut :exec
-- Records the facts of the roles of a lookup in the transaction that stores it. A role with a row keeps its README and the state of
-- it; a lookup that names another commit than the one the README was fetched for leaves the state as it is, and the format asks again.
INSERT INTO ansible_roles (repo_id, role_id, owner, name, description, github_user, github_repo, github_branch, tags, dependencies,
                           downloads, commit_sha, imported_at)
SELECT sqlc.arg(repo_id)::bigint, (sqlc.arg(ids)::bigint[])[i], (sqlc.arg(owners)::text[])[i], (sqlc.arg(names)::text[])[i],
       (sqlc.arg(descriptions)::text[])[i], (sqlc.arg(github_users)::text[])[i], (sqlc.arg(github_repos)::text[])[i],
       (sqlc.arg(github_branches)::text[])[i], ((sqlc.arg(tags)::text[])[i])::jsonb, ((sqlc.arg(dependencies)::text[])[i])::jsonb,
       NULLIF((sqlc.arg(downloads)::bigint[])[i], -1), (sqlc.arg(commits)::text[])[i],
       NULLIF((sqlc.arg(imported)::text[])[i], '')::timestamptz
  FROM generate_series(1, cardinality(sqlc.arg(ids)::bigint[])) AS i
ON CONFLICT (repo_id, role_id) DO UPDATE
   SET owner = EXCLUDED.owner, name = EXCLUDED.name, description = EXCLUDED.description, github_user = EXCLUDED.github_user,
       github_repo = EXCLUDED.github_repo, github_branch = EXCLUDED.github_branch, tags = EXCLUDED.tags,
       dependencies = EXCLUDED.dependencies, downloads = EXCLUDED.downloads, commit_sha = EXCLUDED.commit_sha,
       imported_at = EXCLUDED.imported_at, recorded_at = now();

-- name: AnsibleRoleGet :many
-- The facts of a role, and whether the repository has the role's package (a version of it was cached).
SELECT r.role_id, r.owner, r.name, r.description, r.github_user, r.github_repo, r.github_branch, r.tags, r.dependencies, r.downloads,
       r.commit_sha, r.imported_at, r.readme_state, r.readme_reason, r.readme_file, r.readme_commit, r.recorded_at,
       EXISTS (SELECT 1 FROM packages pk WHERE pk.repo_id = r.repo_id AND pk.name = 'role/' || r.role_id::text) AS packaged
  FROM ansible_roles r
 WHERE r.repo_id = sqlc.arg(repo_id)::bigint AND r.role_id = sqlc.arg(role_id)::bigint;

-- name: AnsibleRoleReadmeSet :execrows
-- The README of a role and its state (none, queued, available, absent, unavailable with a reason); a role without a row is left alone.
UPDATE ansible_roles
   SET readme_state = sqlc.arg(state)::text, readme_reason = sqlc.arg(reason)::text, readme_file = sqlc.arg(file)::text,
       readme_html = sqlc.arg(html)::text, readme_commit = CASE WHEN sqlc.arg(state)::text = 'queued' THEN readme_commit ELSE commit_sha END
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND role_id = sqlc.arg(role_id)::bigint;

-- name: AnsibleRoleQueued :execrows
-- Marks the README of a role queued, keeping what it holds until the fetch answers.
UPDATE ansible_roles SET readme_state = 'queued'
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND role_id = sqlc.arg(role_id)::bigint;

-- name: AnsibleRoleViewRows :many
-- The facts the package screens read for the given generic versions of roles (R-ANS-056): the package of a role is "role/<id>".
SELECT pv.id AS version_id, pk.name AS package, r.role_id, r.owner, r.name, r.description, r.github_user, r.github_repo, r.github_branch,
       r.tags, r.dependencies, r.downloads, r.imported_at, r.readme_state, r.readme_reason, r.readme_file,
       CASE WHEN sqlc.arg(readme)::boolean THEN r.readme_html ELSE '' END::text AS readme_html
  FROM package_versions pv
  JOIN packages pk ON pk.id = pv.package_id
  JOIN ansible_roles r ON r.repo_id = pk.repo_id AND pk.name = 'role/' || r.role_id::text
 WHERE pv.id = ANY(sqlc.arg(ids)::bigint[]);

-- name: AnsibleRoleViewFind :many
-- The generic versions of roles of the repositories (by key) that match, each with its rank (0 to 999), by the rule of AnsibleViewFind
-- (R-ANS-057): with a query, every word of it begins a word of the role's owner and name (weight A), of its tags (B) or of its
-- description (C), ranked by ts_rank; with a keyword, its tags hold it (rank 0). The vector is built per row of the repositories asked:
-- a remote holds the rows of the roles a client looked up, no index of the upstream's, so they are few beside its collections.
SELECT pv.id AS version_id,
       (CASE WHEN sqlc.arg(query)::text = '' THEN 0
             ELSE least(999, floor(1000 * ts_rank(
                    setweight(to_tsvector('simple'::regconfig, translate(ro.owner || ' ' || ro.name, '._', '  ')), 'A')
                    || setweight(jsonb_to_tsvector('simple'::regconfig, ro.tags, '["string"]'), 'B')
                    || setweight(to_tsvector('simple'::regconfig, ro.description), 'C'),
                    to_tsquery('simple', sqlc.arg(query)::text)))) END)::integer AS rank
  FROM ansible_roles ro
  JOIN repositories r ON r.id = ro.repo_id
  JOIN project p ON p.id = r.project_id
  JOIN packages pk ON pk.repo_id = ro.repo_id AND pk.name = 'role/' || ro.role_id::text
  JOIN package_versions pv ON pv.package_id = pk.id AND pv.trashed_at IS NULL
 WHERE (p.key || '/' || r.key) = ANY(sqlc.arg(repos)::text[])
   AND ((sqlc.arg(query)::text <> ''
         AND (setweight(to_tsvector('simple'::regconfig, translate(ro.owner || ' ' || ro.name, '._', '  ')), 'A')
              || setweight(jsonb_to_tsvector('simple'::regconfig, ro.tags, '["string"]'), 'B')
              || setweight(to_tsvector('simple'::regconfig, ro.description), 'C'))
             @@ to_tsquery('simple', sqlc.arg(query)::text))
        OR (sqlc.arg(keyword)::text <> '' AND ro.tags ? sqlc.arg(keyword)::text))
 ORDER BY pv.id
 LIMIT sqlc.arg(row_limit)::int;

-- name: AnsibleRolesPurge :execrows
-- The facts of one remote repository's roles (R-BAK-008): they go with its cache entries.
DELETE FROM ansible_roles WHERE repo_id = sqlc.arg(repo_id)::bigint;
