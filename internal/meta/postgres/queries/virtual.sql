-- Queries of virtual repositories (R-REPO-017, R-REPO-021, R-REPO-023): the
-- members, the deploy target and the pin rules, which are rows and not part of
-- the configuration document (Q-REPO-104). The repository package renders them
-- into the document on read and replaces them on write; the grant index reads
-- the members and the deploy target through the AuthzBuild* statements.

-- name: ListVirtualMembers :many
-- member_key is the member as the virtual repository's document writes it (R-REPO-017, R-REPO-071, Q-REPO-381): the bare key
-- of a repository of the virtual repository's own project, the qualified name upstream/<key> of a repository of the built-in
-- project, the only other project a member may belong to (the guard AI006).
SELECT m.virtual_id, m.position, m.member_id,
       (CASE WHEN r.project_id = v.project_id THEN r.key ELSE mp.key || '/' || r.key END)::text AS member_key
  FROM virtual_members m
  JOIN repositories r ON r.id = m.member_id
  JOIN repositories v ON v.id = m.virtual_id
  JOIN project mp ON mp.id = r.project_id
 WHERE m.virtual_id = ANY(sqlc.arg(ids)::bigint[])
 ORDER BY m.virtual_id, m.position;

-- name: ListVirtualDeployTargets :many
SELECT t.virtual_id, t.target_id, r.key AS target_key
  FROM virtual_deploy_targets t
  JOIN repositories r ON r.id = t.target_id
 WHERE t.virtual_id = ANY(sqlc.arg(ids)::bigint[]);

-- name: ListVirtualPinRules :many
SELECT virtual_id, position, package_patterns
  FROM virtual_pin_rules
 WHERE virtual_id = ANY(sqlc.arg(ids)::bigint[])
 ORDER BY virtual_id, position;

-- name: ListVirtualPinMembers :many
-- A rule's members come back in the order of the virtual repository's member
-- list, so that a read is stable whatever order the write named them in. A
-- member is written as ListVirtualMembers writes it.
SELECT p.virtual_id, p.position, p.member_id,
       (CASE WHEN r.project_id = v.project_id THEN r.key ELSE mp.key || '/' || r.key END)::text AS member_key
  FROM virtual_pin_members p
  JOIN repositories r ON r.id = p.member_id
  JOIN repositories v ON v.id = p.virtual_id
  JOIN project mp ON mp.id = r.project_id
  LEFT JOIN virtual_members m ON m.virtual_id = p.virtual_id AND m.member_id = p.member_id
 WHERE p.virtual_id = ANY(sqlc.arg(ids)::bigint[])
 ORDER BY p.virtual_id, p.position, m.position NULLS LAST, r.key;

-- name: LockRepositoriesByKeys :many
-- Resolves the repositories a virtual repository names and keeps the rows from
-- being marked for deletion, or switched, before the transaction ends: FOR SHARE
-- conflicts with the update that sets lifecycle = 'deleting' and with every write
-- of a repository row. The order by id is the lock order, one order for both
-- projects. keys are the bare keys of the virtual repository's own project,
-- upstream_keys the keys of the members written upstream/<key> (R-REPO-071,
-- Q-REPO-234); upstream says which project a row is of, and upstream_kind,
-- switched_on_before and retired are the row of upstream_repository a state is
-- derived from (Q-REPO-375), empty and false for a repository of the own project.
SELECT r.id, r.key, (p.key = sqlc.arg(upstream_project)::text)::boolean AS upstream, r.class, r.format, r.lifecycle, r.config,
       COALESCE(u.kind, '')::text AS upstream_kind,
       (u.first_switched_on_at IS NOT NULL)::boolean AS switched_on_before,
       (u.retired_at IS NOT NULL)::boolean AS retired
  FROM repositories r
  JOIN project p ON p.id = r.project_id
  LEFT JOIN upstream_repository u ON u.repository_id = r.id
 WHERE (p.key = sqlc.arg(project)::text AND r.key = ANY(sqlc.arg(keys)::text[]))
    OR (p.key = sqlc.arg(upstream_project)::text AND r.key = ANY(sqlc.arg(upstream_keys)::text[]))
 ORDER BY r.id
   FOR SHARE OF r;

-- name: DeleteVirtualMembers :exec
DELETE FROM virtual_members WHERE virtual_id = sqlc.arg(virtual_id)::bigint;

-- name: InsertVirtualMember :exec
INSERT INTO virtual_members (virtual_id, position, member_id)
VALUES (sqlc.arg(virtual_id)::bigint, sqlc.arg(position)::int, sqlc.arg(member_id)::bigint);

-- name: DeleteVirtualDeployTarget :exec
DELETE FROM virtual_deploy_targets WHERE virtual_id = sqlc.arg(virtual_id)::bigint;

-- name: InsertVirtualDeployTarget :exec
INSERT INTO virtual_deploy_targets (virtual_id, target_id)
VALUES (sqlc.arg(virtual_id)::bigint, sqlc.arg(target_id)::bigint);

-- name: DeleteVirtualPinRules :exec
-- The members of the rules go with them (ON DELETE CASCADE).
DELETE FROM virtual_pin_rules WHERE virtual_id = sqlc.arg(virtual_id)::bigint;

-- name: InsertVirtualPinRule :exec
INSERT INTO virtual_pin_rules (virtual_id, position, package_patterns)
VALUES (sqlc.arg(virtual_id)::bigint, sqlc.arg(position)::int, sqlc.arg(package_patterns)::text[]);

-- name: InsertVirtualPinMember :exec
INSERT INTO virtual_pin_members (virtual_id, position, member_id)
VALUES (sqlc.arg(virtual_id)::bigint, sqlc.arg(position)::int, sqlc.arg(member_id)::bigint);

-- name: ListReferencingVirtuals :many
-- The virtual repositories that list a repository as a member, a deploy target
-- or a pin-rule member (R-REPO-013). The rows are locked: a detach rewrites them.
SELECT r.id, r.key
  FROM repositories r
 WHERE r.id IN (SELECT virtual_id FROM virtual_members WHERE member_id = sqlc.arg(id)::bigint
                UNION SELECT virtual_id FROM virtual_deploy_targets WHERE target_id = sqlc.arg(id)::bigint
                UNION SELECT virtual_id FROM virtual_pin_members WHERE member_id = sqlc.arg(id)::bigint)
 ORDER BY r.key
   FOR UPDATE;

-- name: DetachMember :exec
-- The detach of R-REPO-013: the repository leaves every member list, deploy
-- target and pin rule in the transaction that marks it for deletion.
DELETE FROM virtual_members WHERE member_id = sqlc.arg(id)::bigint;

-- name: DetachDeployTarget :exec
DELETE FROM virtual_deploy_targets WHERE target_id = sqlc.arg(id)::bigint;

-- name: DetachPinMember :exec
DELETE FROM virtual_pin_members WHERE member_id = sqlc.arg(id)::bigint;

-- name: TouchConfigVersions :exec
-- A virtual repository whose member list changed under a detach is a changed
-- document: its ETag moves (R-REPO-011).
UPDATE repositories
   SET config_version = config_version + 1, updated_at = now()
 WHERE id = ANY(sqlc.arg(ids)::bigint[]);

-- name: AuthzBuildVirtualMembers :many
-- The member lists the grant index compiles (R-AUTHZ-009), in the snapshot of
-- the index build, each member with the key of its own project: the virtual
-- repository's, or the built-in project upstream (R-REPO-071, the guard AI006).
SELECT m.virtual_id, mp.key::text AS member_project, r.key AS member_key
  FROM virtual_members m
  JOIN repositories r ON r.id = m.member_id
  JOIN project mp ON mp.id = r.project_id
 ORDER BY m.virtual_id, m.position;

-- name: AuthzBuildDeployTargets :many
SELECT t.virtual_id, r.key AS target_key
  FROM virtual_deploy_targets t
  JOIN repositories r ON r.id = t.target_id
 ORDER BY t.virtual_id;
