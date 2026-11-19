-- Queries of the identity packages (internal/auth/principal, internal/auth/token;
-- R-AUTH-016 to R-AUTH-040). Those packages are domain packages and import no
-- driver, so every parameter is a plain Go type: identifiers travel as text and
-- are cast to uuid here, an optional value is the empty string, a negative number
-- or a NULL narg, and an interval is a number of seconds. Statements that return
-- at most one row are :many so the caller never meets the driver's no-rows error.

-- Principals -----------------------------------------------------------------

-- name: IdentityPrincipalByID :many
SELECT p.id::text AS id, p.kind, p.name, p.status, coalesce(u.is_break_glass, false) AS is_break_glass, coalesce(u.external, false) AS external
  FROM principal p LEFT JOIN app_user u ON u.principal_id = p.id
 WHERE p.id = sqlc.arg(id)::text::uuid;

-- name: IdentityPrincipalByName :many
SELECT p.id::text AS id, p.kind, p.name, p.status, coalesce(u.is_break_glass, false) AS is_break_glass, coalesce(u.external, false) AS external
  FROM principal p LEFT JOIN app_user u ON u.principal_id = p.id
 WHERE p.kind = sqlc.arg(kind)::text AND p.name = sqlc.arg(name)::text;

-- name: IdentityGroupsOfMember :many
SELECT DISTINCT g.group_id::text AS id, p.name
  FROM group_membership g JOIN principal p ON p.id = g.group_id
 WHERE g.member_id = sqlc.arg(member_id)::text::uuid
 ORDER BY p.name;

-- name: IdentityLockPrincipal :many
SELECT id::text AS id, kind, status FROM principal WHERE id = sqlc.arg(id)::text::uuid FOR UPDATE;

-- name: IdentityLockRevision :many
-- The revision of a principal, locking the row: the If-Match of an update is
-- compared with it in the transaction that writes (R-API-012).
SELECT revision FROM principal WHERE id = sqlc.arg(id)::text::uuid FOR UPDATE;

-- name: IdentityInsertPrincipal :many
INSERT INTO principal (id, kind, name, created_by)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(kind)::text, sqlc.arg(name)::text, NULLIF(sqlc.arg(created_by)::text, '')::uuid)
ON CONFLICT (kind, name) DO NOTHING
RETURNING id::text AS id;

-- name: IdentitySetPrincipalStatus :many
UPDATE principal SET status = sqlc.arg(status)::text
 WHERE id = sqlc.arg(id)::text::uuid AND status <> sqlc.arg(status)::text
RETURNING id::text AS id;

-- name: IdentityRenamePrincipal :many
UPDATE principal SET name = sqlc.arg(name)::text
 WHERE id = sqlc.arg(id)::text::uuid AND name <> sqlc.arg(name)::text
   AND NOT EXISTS (SELECT 1 FROM principal o WHERE o.kind = principal.kind AND o.name = sqlc.arg(name)::text)
RETURNING id::text AS id;

-- name: IdentityDeletePrincipal :many
DELETE FROM principal WHERE id = sqlc.arg(id)::text::uuid RETURNING id::text AS id;

-- Users ----------------------------------------------------------------------

-- name: IdentityInsertUser :exec
-- external_set: the creator names the flag (POST /users with the member); otherwise the new user takes the installation's
-- newUsersExternal as it stands in this transaction (R-AUTH-044, Q-AUTH-231).
INSERT INTO app_user (principal_id, email, email_verified, display_name, origin, last_login_at, last_interactive_auth_at, external)
VALUES (sqlc.arg(id)::text::uuid, NULLIF(sqlc.arg(email)::text, ''), sqlc.arg(email_verified)::boolean,
        NULLIF(sqlc.arg(display_name)::text, ''), sqlc.arg(origin)::text,
        CASE WHEN sqlc.arg(logged_in)::boolean THEN now() END, CASE WHEN sqlc.arg(logged_in)::boolean THEN now() END,
        CASE WHEN sqlc.arg(external_set)::boolean THEN sqlc.arg(external)::boolean
             ELSE coalesce((SELECT s.new_users_external FROM auth_settings s WHERE s.singleton), false) END);

-- name: IdentityGetUser :many
SELECT p.id::text AS id, p.name, p.status, p.created_at, p.revision,
       coalesce(u.email, '')::text AS email, u.email_verified, coalesce(u.display_name, '')::text AS display_name,
       u.origin, u.is_break_glass, coalesce(u.disabled_reason, '')::text AS disabled_reason,
       u.last_login_at, coalesce(u.scim_external_id, '')::text AS scim_external_id, u.external,
       coalesce((SELECT i.last_claims -> 'groupClaimMissing' FROM user_identity i
                  WHERE i.user_id = u.principal_id AND i.last_claims ? 'groupClaimMissing'
                  ORDER BY (i.last_claims -> 'groupClaimMissing' ->> 'since')::timestamptz DESC NULLS LAST, i.id DESC
                  LIMIT 1), 'null'::jsonb)::text AS group_claim_missing
  FROM principal p JOIN app_user u ON u.principal_id = p.id
 WHERE p.kind = 'user' AND p.name = sqlc.arg(name)::text;

-- name: IdentityGetUserByID :many
SELECT p.id::text AS id, p.name, p.status, p.created_at, p.revision,
       coalesce(u.email, '')::text AS email, u.email_verified, coalesce(u.display_name, '')::text AS display_name,
       u.origin, u.is_break_glass, coalesce(u.disabled_reason, '')::text AS disabled_reason,
       u.last_login_at, coalesce(u.scim_external_id, '')::text AS scim_external_id, u.external,
       coalesce((SELECT i.last_claims -> 'groupClaimMissing' FROM user_identity i
                  WHERE i.user_id = u.principal_id AND i.last_claims ? 'groupClaimMissing'
                  ORDER BY (i.last_claims -> 'groupClaimMissing' ->> 'since')::timestamptz DESC NULLS LAST, i.id DESC
                  LIMIT 1), 'null'::jsonb)::text AS group_claim_missing
  FROM principal p JOIN app_user u ON u.principal_id = p.id
 WHERE p.kind = 'user' AND p.id = sqlc.arg(id)::text::uuid;

-- name: IdentityListUsers :many
-- Keyset order by name. status is active, disabled, locked (the break-glass account
-- while its lockout is in force, R-AUTH-038) or ''; origin and group are '' for any;
-- external_filter is 'true', 'false' or ''; admin is 'true', 'false' or '' (the user holds the role admin directly or through
-- a group, or is the break-glass account).
SELECT p.id::text AS id, p.name, p.status, p.created_at, p.revision,
       coalesce(u.email, '')::text AS email, u.email_verified, coalesce(u.display_name, '')::text AS display_name,
       u.origin, u.is_break_glass, coalesce(u.disabled_reason, '')::text AS disabled_reason,
       u.last_login_at, coalesce(u.scim_external_id, '')::text AS scim_external_id, u.external, 'null'::text AS group_claim_missing
  FROM principal p JOIN app_user u ON u.principal_id = p.id
 WHERE p.kind = 'user'
   AND p.name > sqlc.arg(after)::text
   AND (sqlc.arg(external_filter)::text = '' OR u.external = (sqlc.arg(external_filter)::text = 'true'))
   AND (sqlc.arg(query)::text = '' OR p.name ILIKE '%' || sqlc.arg(query)::text || '%' OR u.email ILIKE '%' || sqlc.arg(query)::text || '%')
   AND (sqlc.arg(status)::text = ''
        OR (sqlc.arg(status)::text = 'locked' AND u.is_break_glass
            AND EXISTS (SELECT 1 FROM auth_lockout l WHERE l.account_key = 'break-glass' AND l.locked_until > now()))
        OR (sqlc.arg(status)::text <> 'locked' AND p.status = sqlc.arg(status)::text))
   AND (sqlc.arg(origin)::text = '' OR u.origin = sqlc.arg(origin)::text)
   AND (sqlc.arg(group_name)::text = '' OR EXISTS (
          SELECT 1 FROM group_membership m JOIN principal g ON g.id = m.group_id
           WHERE m.member_id = p.id AND g.name = sqlc.arg(group_name)::text))
   AND (sqlc.arg(admin)::text = '' OR (sqlc.arg(admin)::text = 'true') = (
          u.is_break_glass OR EXISTS (
            SELECT 1 FROM authz_role_binding b
             WHERE b.role = 'admin'
               AND (b.principal_id = p.id
                    OR b.principal_id IN (SELECT m.group_id FROM group_membership m WHERE m.member_id = p.id)))))
 ORDER BY p.name
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityAdminVia :many
-- Why the given users hold the role admin (R-AUTH-016 isAdmin and adminVia): "direct"
-- for a binding on the user, "group:<name>" for a binding on a group the user is a
-- member of.
SELECT b.principal_id::text AS user_id, 'direct'::text AS via
  FROM authz_role_binding b
 WHERE b.role = 'admin' AND b.principal_id = ANY(sqlc.arg(ids)::text[]::uuid[])
UNION
SELECT m.member_id::text AS user_id, 'group:' || g.name AS via
  FROM authz_role_binding b
  JOIN group_membership m ON m.group_id = b.principal_id
  JOIN principal g ON g.id = b.principal_id
 WHERE b.role = 'admin' AND m.member_id = ANY(sqlc.arg(ids)::text[]::uuid[])
 ORDER BY user_id, via;

-- name: IdentityAdminHolderIDs :many
-- Which of the given principals hold the role admin now (Q-AUTH-209): a binding on the principal itself, a binding on a
-- group the principal is a member of, or the break-glass account. A service account is bound directly (R-AUTH-019).
SELECT b.principal_id::text AS id
  FROM authz_role_binding b
 WHERE b.role = 'admin' AND b.principal_id = ANY(sqlc.arg(ids)::text[]::uuid[])
UNION
SELECT m.member_id::text AS id
  FROM authz_role_binding b
  JOIN group_membership m ON m.group_id = b.principal_id
 WHERE b.role = 'admin' AND m.member_id = ANY(sqlc.arg(ids)::text[]::uuid[])
UNION
SELECT u.principal_id::text AS id
  FROM app_user u
 WHERE u.is_break_glass AND u.principal_id = ANY(sqlc.arg(ids)::text[]::uuid[])
ORDER BY id;

-- name: IdentityGroupRoles :many
-- The system roles bound to a group, read only on the group record (R-AUTH-018).
SELECT role FROM authz_role_binding WHERE principal_id = sqlc.arg(group_id)::text::uuid ORDER BY role;

-- name: IdentityBreakGlassLockedUntil :many
-- The end of the lockout of the break-glass account while it is in force (R-AUTH-038);
-- no row when it is not locked.
SELECT locked_until FROM auth_lockout WHERE account_key = 'break-glass' AND locked_until > now();

-- name: IdentityUserIdentities :many
SELECT provider_id, issuer, (subject IS NOT NULL)::boolean AS linked FROM user_identity
 WHERE user_id = sqlc.arg(user_id)::text::uuid ORDER BY provider_id;

-- name: IdentityUserMemberships :many
SELECT p.name AS group_name, m.source
  FROM group_membership m JOIN principal p ON p.id = m.group_id
 WHERE m.member_id = sqlc.arg(member_id)::text::uuid
 ORDER BY p.name, m.source;

-- name: IdentityUpdateUserFields :exec
UPDATE app_user SET email = CASE WHEN sqlc.arg(set_email)::boolean THEN NULLIF(sqlc.arg(email)::text, '') ELSE email END,
                    display_name = CASE WHEN sqlc.arg(set_display_name)::boolean THEN NULLIF(sqlc.arg(display_name)::text, '') ELSE display_name END
 WHERE principal_id = sqlc.arg(id)::text::uuid;

-- name: IdentityRecordLogin :exec
UPDATE app_user SET email = NULLIF(sqlc.arg(email)::text, ''), email_verified = sqlc.arg(email_verified)::boolean,
                    display_name = NULLIF(sqlc.arg(display_name)::text, ''),
                    last_login_at = now(), last_interactive_auth_at = now()
 WHERE principal_id = sqlc.arg(id)::text::uuid;

-- name: IdentitySetUserDisabled :exec
UPDATE app_user SET disabled_reason = NULLIF(sqlc.arg(reason)::text, ''),
                    disabled_at = CASE WHEN sqlc.arg(reason)::text = '' THEN NULL ELSE now() END
 WHERE principal_id = sqlc.arg(id)::text::uuid;

-- name: IdentitySetUserExternal :many
-- The external flag of a user (R-AUTH-044); no row back: the flag already has the value.
UPDATE app_user SET external = sqlc.arg(external)::boolean
 WHERE principal_id = sqlc.arg(id)::text::uuid AND external IS DISTINCT FROM sqlc.arg(external)::boolean
RETURNING principal_id::text AS id;

-- name: IdentityUserDisabledReason :many
SELECT coalesce(disabled_reason, '')::text AS reason, is_break_glass FROM app_user WHERE principal_id = sqlc.arg(id)::text::uuid;

-- name: IdentityEndSessions :many
-- A disable ends every live session of the principal with a reason that withdraws it; WithdrawEndedSessionsOfPrincipal then marks the
-- ones that had ended already, in the same transaction (Q-AUTHZ-262).
UPDATE ui_session SET ended_at = now(), end_reason = sqlc.arg(reason)::text, withdrawn_at = now()
 WHERE principal_id = sqlc.arg(id)::text::uuid AND ended_at IS NULL
RETURNING id::text AS id;

-- name: IdentityCountSessions :one
SELECT count(*) FROM ui_session WHERE principal_id = sqlc.arg(id)::text::uuid AND ended_at IS NULL;

-- name: IdentityUserByIdentity :many
SELECT user_id::text AS user_id FROM user_identity
 WHERE issuer = sqlc.arg(issuer)::text AND subject = sqlc.arg(subject)::text;

-- name: IdentityClaimPendingLink :many
UPDATE user_identity SET subject = sqlc.arg(subject)::text, issuer = sqlc.arg(issuer)::text, linked_at = now()
 WHERE id = (SELECT i.id FROM user_identity i JOIN principal p ON p.id = i.user_id
              WHERE i.provider_id = sqlc.arg(provider_id)::text AND i.subject IS NULL
                AND p.kind = 'user' AND p.name = sqlc.arg(username)::text
              LIMIT 1)
   AND subject IS NULL
RETURNING user_id::text AS user_id;

-- name: IdentityInsertIdentity :many
INSERT INTO user_identity (user_id, provider_id, issuer, subject, linked_at)
VALUES (sqlc.arg(user_id)::text::uuid, sqlc.arg(provider_id)::text, sqlc.arg(issuer)::text,
        NULLIF(sqlc.arg(subject)::text, ''), CASE WHEN sqlc.arg(subject)::text <> '' THEN now() END)
ON CONFLICT DO NOTHING
RETURNING id;

-- name: IdentityLockGroupClaim :many
-- The stored state of the group claim of an identity (R-AUTH-008) under the lock of its row, and the transaction's time, which
-- is the time of the audit event that a change of the state records (Q-AUTH-403). claim_missing is 'null' while the claim was
-- usable at the identity's last sign-in, and before its first one.
SELECT coalesce(last_claims -> 'groupClaimMissing', 'null'::jsonb)::text AS claim_missing, now()::timestamptz AS now
  FROM user_identity
 WHERE issuer = sqlc.arg(issuer)::text AND subject = sqlc.arg(subject)::text
   FOR UPDATE;

-- name: IdentitySetLastClaims :exec
UPDATE user_identity SET last_claims = sqlc.arg(claims)::text::jsonb
 WHERE issuer = sqlc.arg(issuer)::text AND subject = sqlc.arg(subject)::text;

-- name: IdentityDeleteIdentitiesOfUser :one
WITH d AS (DELETE FROM user_identity WHERE user_id = sqlc.arg(user_id)::text::uuid RETURNING 1) SELECT count(*) FROM d;

-- name: IdentityReplaceIdentities :many
-- Replaces the pending links of a user: the identities listed stay or are added
-- as pending (a linked identity is never removed here). The providers of the links
-- removed are returned: they are part of the journal entry of user.updated.
DELETE FROM user_identity
 WHERE user_id = sqlc.arg(user_id)::text::uuid AND subject IS NULL
   AND NOT (provider_id = ANY(sqlc.arg(providers)::text[]))
RETURNING provider_id;

-- name: IdentityDeletePendingLinks :many
-- Replay of user.updated: deletes the pending links of the providers named; a linked identity is never touched.
DELETE FROM user_identity
 WHERE user_id = sqlc.arg(user_id)::text::uuid AND subject IS NULL
   AND provider_id = ANY(sqlc.arg(providers)::text[])
RETURNING provider_id;

-- Groups ---------------------------------------------------------------------

-- name: IdentityInsertGroup :exec
INSERT INTO user_group (principal_id, description, auto_join, origin)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(description)::text, sqlc.arg(auto_join)::boolean, sqlc.arg(origin)::text);

-- name: IdentityGetGroup :many
SELECT p.id::text AS id, p.name, g.description, g.auto_join, g.sync_detached, g.origin, p.created_at, p.revision,
       (SELECT count(DISTINCT m.member_id) FROM group_membership m WHERE m.group_id = p.id) AS member_count
  FROM principal p JOIN user_group g ON g.principal_id = p.id
 WHERE p.kind = 'group' AND p.name = sqlc.arg(name)::text;

-- name: IdentityListGroups :many
SELECT p.id::text AS id, p.name, g.description, g.auto_join, g.sync_detached, g.origin, p.created_at, p.revision,
       (SELECT count(DISTINCT m.member_id) FROM group_membership m WHERE m.group_id = p.id) AS member_count
  FROM principal p JOIN user_group g ON g.principal_id = p.id
 WHERE p.kind = 'group' AND p.name > sqlc.arg(after)::text
   AND (sqlc.arg(query)::text = '' OR p.name ILIKE '%' || sqlc.arg(query)::text || '%')
   AND (sqlc.arg(origin)::text = '' OR g.origin = sqlc.arg(origin)::text)
 ORDER BY p.name
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityUpdateGroup :exec
UPDATE user_group SET description = CASE WHEN sqlc.arg(set_description)::boolean THEN sqlc.arg(description)::text ELSE description END,
                      auto_join = CASE WHEN sqlc.arg(set_auto_join)::boolean THEN sqlc.arg(auto_join)::boolean ELSE auto_join END,
                      sync_detached = CASE WHEN sqlc.arg(set_sync_detached)::boolean THEN sqlc.arg(sync_detached)::boolean ELSE sync_detached END
 WHERE principal_id = sqlc.arg(id)::text::uuid;

-- name: IdentityGroupMembers :many
SELECT mp.kind AS member_kind, mp.name AS member_name, m.source
  FROM group_membership m JOIN principal mp ON mp.id = m.member_id
 WHERE m.group_id = sqlc.arg(group_id)::text::uuid
   AND (mp.kind, mp.name) > (sqlc.arg(after_kind)::text, sqlc.arg(after_name)::text)
   AND (sqlc.arg(source)::text = '' OR m.source = sqlc.arg(source)::text)
 ORDER BY mp.kind, mp.name, m.source
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityMemberSources :many
SELECT source FROM group_membership
 WHERE group_id = sqlc.arg(group_id)::text::uuid AND member_id = sqlc.arg(member_id)::text::uuid ORDER BY source;

-- name: IdentityAddMembership :many
INSERT INTO group_membership (group_id, member_id, source)
VALUES (sqlc.arg(group_id)::text::uuid, sqlc.arg(member_id)::text::uuid, sqlc.arg(source)::text)
ON CONFLICT DO NOTHING
RETURNING group_id::text AS group_id;

-- name: IdentityRemoveMembership :many
DELETE FROM group_membership
 WHERE group_id = sqlc.arg(group_id)::text::uuid AND member_id = sqlc.arg(member_id)::text::uuid AND source = sqlc.arg(source)::text
RETURNING group_id::text AS group_id;

-- name: IdentityCountMemberships :one
SELECT count(*) FROM group_membership WHERE group_id = sqlc.arg(group_id)::text::uuid;

-- name: IdentityCountMembershipsOfMember :one
SELECT count(*) FROM group_membership WHERE member_id = sqlc.arg(member_id)::text::uuid;

-- name: IdentityOIDCMemberships :many
-- The groups a member holds through one provider.
SELECT p.id::text AS group_id, p.name AS group_name
  FROM group_membership m JOIN principal p ON p.id = m.group_id
 WHERE m.member_id = sqlc.arg(member_id)::text::uuid AND m.source = sqlc.arg(source)::text
 ORDER BY p.name;

-- name: IdentityGroupsByName :many
SELECT p.id::text AS id, p.name FROM principal p JOIN user_group g ON g.principal_id = p.id
 WHERE p.kind = 'group' AND p.name = ANY(sqlc.arg(names)::text[]) AND NOT g.sync_detached;

-- name: IdentityGroupDetachedByName :one
-- Whether the group of the name is detached from group synchronization (Q-AUTH-206).
SELECT EXISTS (SELECT 1 FROM principal p JOIN user_group g ON g.principal_id = p.id
                WHERE p.kind = 'group' AND p.name = sqlc.arg(name)::text AND g.sync_detached);

-- name: IdentityAutoJoinGroups :many
SELECT p.id::text AS id, p.name FROM principal p JOIN user_group g ON g.principal_id = p.id
 WHERE g.auto_join ORDER BY p.name;

-- name: IdentityGroupHasAdminRole :one
SELECT EXISTS (SELECT 1 FROM authz_role_binding WHERE principal_id = sqlc.arg(id)::text::uuid);

-- name: IdentityGroupProjects :many
-- The keys of the projects the group is a member of. An auto-join group is never a
-- project member (every user is in it), so turning the flag on is refused while the
-- list is not empty (R-AUTH-019, R-AUTHZ-026); the member write reads the flag under
-- the same row lock (ProjectGroupAutoJoinShare).
SELECT p.key FROM project_member m JOIN project p ON p.id = m.project_id
 WHERE m.principal_id = sqlc.arg(id)::text::uuid ORDER BY p.key;

-- name: IdentityLockGroup :many
-- Locks a group's row, so that binding a role to the group and turning on its
-- auto-join flag, which each check the other, run one after the other
-- (R-AUTH-019, R-AUTHZ-022). No row for a principal that is not a group. The
-- flag read under the lock is the before-state of group.updated's journal entry.
SELECT principal_id::text AS id, auto_join, sync_detached FROM user_group WHERE principal_id = sqlc.arg(id)::text::uuid FOR UPDATE;

-- name: IdentityDeleteOIDCMemberships :many
-- Replay of a group rename (Q-AUTH-206): the memberships a group synchronization wrote, of every provider.
DELETE FROM group_membership
 WHERE group_id = sqlc.arg(group_id)::text::uuid AND source LIKE 'oidc:%'
RETURNING member_id::text AS member_id;

-- name: IdentityHoldsRole :one
-- Whether a principal holds a system role, through a binding of its own or of a
-- group it is a member of (R-AUTHZ-011, Q-AUTHZ-69). An empty role means any role.
SELECT EXISTS (
    SELECT 1 FROM authz_role_binding b
     WHERE (sqlc.arg(role)::text = '' OR b.role = sqlc.arg(role)::text)
       AND (b.principal_id = sqlc.arg(id)::text::uuid
            OR b.principal_id IN (SELECT m.group_id FROM group_membership m WHERE m.member_id = sqlc.arg(id)::text::uuid)));

-- name: IdentityLockAdminBindings :exec
-- Locks the admin role bindings. Every change that can take the role admin from
-- its last holder takes this lock first and counts the holders in a statement
-- of its own afterwards, so two such changes run one after the other and the
-- second sees what the first left (R-AUTHZ-022).
SELECT 1 FROM authz_role_binding WHERE role = 'admin' FOR UPDATE;

-- name: IdentityAdminHolders :one
-- The active users and service accounts that hold the role admin, directly or
-- through a group, and whether the break-glass administrator can sign in: the
-- last_admin guard of R-AUTHZ-022 counts who can still act as an administrator,
-- not the bindings.
SELECT (SELECT count(*) FROM principal p
         WHERE p.status = 'active' AND p.kind IN ('user', 'service_account')
           AND EXISTS (SELECT 1 FROM authz_role_binding b
                        WHERE b.role = 'admin'
                          AND (b.principal_id = p.id
                               OR b.principal_id IN (SELECT m.group_id FROM group_membership m WHERE m.member_id = p.id))))::bigint AS holders,
       EXISTS (SELECT 1 FROM app_user u JOIN principal p ON p.id = u.principal_id
                WHERE u.is_break_glass AND p.status = 'active') AS break_glass_active;

-- Service accounts -----------------------------------------------------------

-- name: IdentityInsertServiceAccount :exec
INSERT INTO service_account (principal_id, description) VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(description)::text);

-- name: IdentityInsertProjectServiceAccount :exec
-- A service account of a project (Q-AUTHZ-169, Q-AUTHZ-209): its principal's name is
-- <project key>/<name>; the row's trigger refuses any other name.
INSERT INTO service_account (principal_id, description, project_id)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(description)::text, sqlc.arg(project_id)::text::uuid);

-- name: IdentityGetServiceAccount :many
-- project is the key of the account's project, empty for a global account; repository the key of a repository's account
-- within its project (R-AUTH-046), empty otherwise, with repository_id and the actions it holds (R-AUTHZ-039);
-- technical_tokens counts the account's technical tokens (R-AUTH-045).
SELECT p.id::text AS id, p.name, p.status, p.created_at, p.revision, s.description,
       COALESCE(pr.key, '')::text AS project, COALESCE(r.key, '')::text AS repository,
       COALESCE(s.repository_id, 0)::bigint AS repository_id, COALESCE(g.actions, '{}')::text[] AS actions,
       (SELECT count(*) FROM technical_token t WHERE t.service_account_id = p.id)::bigint AS technical_tokens
  FROM principal p JOIN service_account s ON s.principal_id = p.id
  LEFT JOIN project pr ON pr.id = s.project_id
  LEFT JOIN repositories r ON r.id = s.repository_id
  LEFT JOIN service_account_repository_grant g ON g.service_account_id = p.id
 WHERE p.kind = 'service_account' AND p.name = sqlc.arg(name)::text;

-- name: IdentityListServiceAccounts :many
-- project filters by the key of the project; empty lists every account, global, project and repository ones. The accounts of a
-- project's repositories belong to the project's filter (R-AUTH-046).
SELECT p.id::text AS id, p.name, p.status, p.created_at, p.revision, s.description,
       COALESCE(pr.key, '')::text AS project, COALESCE(r.key, '')::text AS repository,
       COALESCE(s.repository_id, 0)::bigint AS repository_id, COALESCE(g.actions, '{}')::text[] AS actions,
       (SELECT count(*) FROM technical_token t WHERE t.service_account_id = p.id)::bigint AS technical_tokens
  FROM principal p JOIN service_account s ON s.principal_id = p.id
  LEFT JOIN project pr ON pr.id = s.project_id
  LEFT JOIN repositories r ON r.id = s.repository_id
  LEFT JOIN service_account_repository_grant g ON g.service_account_id = p.id
 WHERE p.kind = 'service_account' AND p.name > sqlc.arg(after)::text
   AND (sqlc.arg(query)::text = '' OR p.name ILIKE '%' || sqlc.arg(query)::text || '%')
   AND (sqlc.arg(project)::text = '' OR pr.key = sqlc.arg(project)::text)
 ORDER BY p.name
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityListProjectServiceAccounts :many
-- The accounts of one project by the key of the project, ordered by stored name
-- (<project>/<name>); the text filter matches the name within the project. The accounts of the project's repositories are
-- listed under their repository (IdentityListRepositoryServiceAccounts).
SELECT p.id::text AS id, p.name, p.status, p.created_at, p.revision, s.description,
       pr.key::text AS project,
       (SELECT count(*) FROM technical_token t WHERE t.service_account_id = p.id)::bigint AS technical_tokens
  FROM principal p JOIN service_account s ON s.principal_id = p.id
  JOIN project pr ON pr.id = s.project_id
 WHERE p.kind = 'service_account' AND pr.key = sqlc.arg(project)::text AND s.repository_id IS NULL
   AND p.name > sqlc.arg(after)::text
   AND (sqlc.arg(query)::text = '' OR substr(p.name, char_length(pr.key) + 2) ILIKE '%' || sqlc.arg(query)::text || '%')
 ORDER BY p.name
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityListRepositoryServiceAccounts :many
-- The accounts of one repository (R-AUTH-046), ordered by stored name (<project>/<repository>/<name>); the text filter matches the
-- name within the repository.
SELECT p.id::text AS id, p.name, p.status, p.created_at, p.revision, s.description,
       pr.key::text AS project, r.key::text AS repository, r.id AS repository_id, COALESCE(g.actions, '{}')::text[] AS actions,
       (SELECT count(*) FROM technical_token t WHERE t.service_account_id = p.id)::bigint AS technical_tokens
  FROM principal p JOIN service_account s ON s.principal_id = p.id
  JOIN repositories r ON r.id = s.repository_id
  JOIN project pr ON pr.id = s.project_id
  LEFT JOIN service_account_repository_grant g ON g.service_account_id = p.id
 WHERE p.kind = 'service_account' AND s.repository_id = sqlc.arg(repository_id)::bigint
   AND p.name > sqlc.arg(after)::text
   AND (sqlc.arg(query)::text = ''
        OR substr(p.name, char_length(pr.key) + char_length(r.key) + 3) ILIKE '%' || sqlc.arg(query)::text || '%')
 ORDER BY p.name
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityInsertRepositoryServiceAccount :exec
-- A service account of a repository (R-AUTH-046): its principal's name is <project>/<repository>/<name>, its project the
-- repository's; the row's trigger refuses anything else (AI005).
INSERT INTO service_account (principal_id, description, project_id, repository_id)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(description)::text, sqlc.arg(project_id)::text::uuid, sqlc.arg(repository_id)::bigint);

-- name: IdentitySetRepositoryGrant :exec
-- The actions a repository's account holds on its repository (R-AUTHZ-039).
INSERT INTO service_account_repository_grant (service_account_id, actions)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(actions)::text[])
ON CONFLICT (service_account_id) DO UPDATE SET actions = EXCLUDED.actions;

-- name: IdentityRepositoryShare :many
-- The active repository a service account is created for, locked FOR SHARE: its deletion (which marks it deleting under
-- FOR UPDATE) and its move wait for this transaction, so the account is created in the project the repository is in and is
-- moved with it, or it finds the repository gone (R-AUTH-046).
SELECT r.id, r.project_id::text AS project_id
  FROM repositories r JOIN project p ON p.id = r.project_id
 WHERE p.key = sqlc.arg(project)::text AND r.key = sqlc.arg(key)::text AND r.lifecycle = 'active'
   FOR SHARE OF r;

-- name: IdentityActiveRepository :many
-- The ID of an active repository by its project and key, without a lock: the reads of a repository's accounts.
SELECT r.id
  FROM repositories r JOIN project p ON p.id = r.project_id
 WHERE p.key = sqlc.arg(project)::text AND r.key = sqlc.arg(key)::text AND r.lifecycle = 'active';

-- name: IdentityRepositoryAccounts :many
-- The service accounts of a repository by its ID, with their stored names: the deletion of the repository records their end,
-- and a move renames them (R-AUTH-046).
SELECT s.principal_id::text AS id, p.name
  FROM service_account s JOIN principal p ON p.id = s.principal_id
 WHERE s.repository_id = sqlc.arg(repository_id)::bigint
 ORDER BY p.name;

-- name: IdentityRenameRepositoryAccounts :many
-- The new stored names of a repository's accounts whose repository moved (R-AUTH-046, R-REPO-060): prefix is
-- "<new project key>/<repository key>/", the name within the repository stays.
UPDATE principal p SET name = sqlc.arg(prefix)::text || split_part(p.name, '/', 3)
  FROM service_account s
 WHERE s.principal_id = p.id AND s.repository_id = sqlc.arg(repository_id)::bigint
RETURNING p.id::text AS id, p.name;

-- name: IdentityMoveRepositoryAccounts :execrows
-- The project of a repository's accounts follows the repository in the transaction of its move (R-AUTH-046); the names are
-- renamed first, so the row guard sees the new name with the new project.
UPDATE service_account SET project_id = sqlc.arg(project_id)::text::uuid WHERE repository_id = sqlc.arg(repository_id)::bigint;

-- name: IdentityUpdateServiceAccount :exec
UPDATE service_account SET description = sqlc.arg(description)::text WHERE principal_id = sqlc.arg(id)::text::uuid;

-- Tokens ---------------------------------------------------------------------

-- name: IdentityTokenBySecret :many
-- The request path: one indexed lookup by the secret hash, joined with the owner.
SELECT t.id::text AS id, t.type, t.scope, t.principal_id::text AS owner_id,
       p.kind AS owner_kind, p.name AS owner_name, p.status AS owner_status, coalesce(u.is_break_glass, false) AS owner_break_glass,
       t.expires_at, coalesce(t.expires_at IS NOT NULL AND t.expires_at <= now(), false)::boolean AS expired,
       (t.revoked_at IS NOT NULL)::boolean AS revoked, t.hint, t.ci_context,
       coalesce(t.technical_token_id::text, '')::text AS technical_token_id,
       coalesce(t.rejection_logged_at IS NULL OR t.rejection_logged_at < now() - interval '1 hour', true)::boolean AS rejection_due
  FROM api_token t JOIN principal p ON p.id = t.principal_id LEFT JOIN app_user u ON u.principal_id = p.id
 WHERE t.secret_sha256 = sqlc.arg(secret_sha256)::bytea;

-- name: IdentityMarkRejectionLogged :many
UPDATE api_token SET rejection_logged_at = now()
 WHERE id = sqlc.arg(id)::text::uuid AND (rejection_logged_at IS NULL OR rejection_logged_at < now() - interval '1 hour')
RETURNING id::text AS id;

-- name: IdentityCountActiveTokens :one
SELECT count(*) FROM api_token
 WHERE principal_id = sqlc.arg(principal_id)::text::uuid AND type = sqlc.arg(type)::text
   AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now());

-- name: IdentityInsertToken :one
-- ttl_secs is relative to the database clock, negative for no expiry; cap_micros (Unix microseconds, 0 for none)
-- keeps a derived token from outliving the token that created it, whatever the clocks say (R-AUTH-027).
INSERT INTO api_token (id, type, secret_sha256, hint, principal_id, description, scope, self_rotatable, created_by, created_via,
                       expires_at, rotated_from, derived_from, technical_token_id)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(type)::text, sqlc.arg(secret_sha256)::bytea, sqlc.arg(hint)::text,
        sqlc.arg(principal_id)::text::uuid, sqlc.arg(description)::text, sqlc.arg(scope)::text::jsonb, sqlc.arg(self_rotatable)::boolean,
        NULLIF(sqlc.arg(created_by)::text, '')::uuid, sqlc.arg(created_via)::text,
        CASE WHEN sqlc.arg(ttl_secs)::float8 < 0 THEN NULL
             WHEN sqlc.arg(cap_micros)::bigint > 0
                  THEN least(now() + make_interval(secs => sqlc.arg(ttl_secs)::float8), to_timestamp(sqlc.arg(cap_micros)::bigint / 1000000.0))
             ELSE now() + make_interval(secs => sqlc.arg(ttl_secs)::float8) END,
        NULLIF(sqlc.arg(rotated_from)::text, '')::uuid, NULLIF(sqlc.arg(derived_from)::text, '')::uuid,
        NULLIF(sqlc.arg(technical_token_id)::text, '')::uuid)
RETURNING expires_at, created_at;

-- name: IdentityTokenRow :many
-- A Bastion token (type bst) is managed through its registration (queries/bastion_registration.sql, R-BAS-004)
-- and is not a token of the token API: no row here, so GET, DELETE and rotate of /tokens/{id} answer 404 for it.
SELECT t.id::text AS id, t.type, t.hint, t.principal_id::text AS owner_id, p.kind AS owner_kind, p.name AS owner_name,
       t.description, t.scope, t.self_rotatable, t.created_at, coalesce(t.created_by::text, '')::text AS created_by,
       t.created_via, t.expires_at, t.revoked_at, coalesce(t.revoked_by::text, '')::text AS revoked_by,
       coalesce(t.revocation_reason, '')::text AS revocation_reason, coalesce(t.rotated_from::text, '')::text AS rotated_from,
       coalesce(t.derived_from::text, '')::text AS derived_from, t.last_used_at, coalesce(host(t.last_used_addr), '')::text AS last_used_addr,
       coalesce(t.expires_at IS NOT NULL AND t.expires_at <= now(), false)::boolean AS expired, t.ci_context,
       coalesce(t.technical_token_id::text, '')::text AS technical_token_id, coalesce(tt.name, '')::text AS technical_token_name
  FROM api_token t JOIN principal p ON p.id = t.principal_id LEFT JOIN technical_token tt ON tt.id = t.technical_token_id
 WHERE t.id = sqlc.arg(id)::text::uuid AND t.type <> 'bst';

-- name: IdentityListTokens :many
-- Keyset order by id descending (a UUIDv7 sorts by creation time). Bastion tokens (type bst) are not listed: they
-- belong to a registration and are shown with it (R-BAS-004). A line root narrows the list to that token and the
-- tokens whose derived_from chain leads to it or to one of its rotation predecessors: what a token whose scope does
-- not inherit sees of its owner's tokens, the rule of IdentityTokenDescendsFrom (R-AUTH-028, Q-AUTH-199).
WITH RECURSIVE roots(id) AS (
    SELECT NULLIF(sqlc.arg(line_root)::text, '')::uuid WHERE sqlc.arg(line_root)::text <> ''
    UNION
    SELECT t.rotated_from FROM api_token t JOIN roots ON t.id = roots.id WHERE t.rotated_from IS NOT NULL
), line(id) AS (
    SELECT c.id FROM api_token c JOIN roots ON c.derived_from = roots.id
    UNION
    SELECT c.id FROM api_token c JOIN line ON c.derived_from = line.id
)
SELECT t.id::text AS id, t.type, t.hint, t.principal_id::text AS owner_id, p.kind AS owner_kind, p.name AS owner_name,
       t.description, t.scope, t.self_rotatable, t.created_at, coalesce(t.created_by::text, '')::text AS created_by,
       t.created_via, t.expires_at, t.revoked_at, coalesce(t.revoked_by::text, '')::text AS revoked_by,
       coalesce(t.revocation_reason, '')::text AS revocation_reason, coalesce(t.rotated_from::text, '')::text AS rotated_from,
       coalesce(t.derived_from::text, '')::text AS derived_from, t.last_used_at, coalesce(host(t.last_used_addr), '')::text AS last_used_addr,
       coalesce(t.expires_at IS NOT NULL AND t.expires_at <= now(), false)::boolean AS expired, t.ci_context,
       coalesce(t.technical_token_id::text, '')::text AS technical_token_id, coalesce(tt.name, '')::text AS technical_token_name
  FROM api_token t JOIN principal p ON p.id = t.principal_id LEFT JOIN technical_token tt ON tt.id = t.technical_token_id
 WHERE t.type <> 'bst'
   AND (sqlc.arg(after)::text = '' OR t.id < sqlc.arg(after)::text::uuid)
   AND (sqlc.arg(owner_id)::text = '' OR t.principal_id = sqlc.arg(owner_id)::text::uuid)
   AND (sqlc.arg(line_root)::text = '' OR t.id = NULLIF(sqlc.arg(line_root)::text, '')::uuid OR t.id IN (SELECT line.id FROM line))
   AND (sqlc.arg(type)::text = '' OR t.type = sqlc.arg(type)::text)
   AND (CASE sqlc.arg(state)::text
          WHEN 'active'  THEN t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())
          WHEN 'expired' THEN t.revoked_at IS NULL AND t.expires_at IS NOT NULL AND t.expires_at <= now()
          WHEN 'revoked' THEN t.revoked_at IS NOT NULL
          ELSE true END)
   AND (sqlc.arg(expires_before_unix)::bigint = 0 OR t.expires_at < to_timestamp(sqlc.arg(expires_before_unix)::bigint))
   AND (sqlc.arg(expires_after_unix)::bigint = 0 OR t.expires_at > to_timestamp(sqlc.arg(expires_after_unix)::bigint))
   AND (sqlc.arg(last_used_before_unix)::bigint = 0 OR t.last_used_at IS NULL OR t.last_used_at < to_timestamp(sqlc.arg(last_used_before_unix)::bigint))
 ORDER BY t.id DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityTouchTokensLastUsed :execrows
-- One flush of the last-used buffer (R-AUTH-031). A JSON array of {"id", "at", "addr"}.
-- The row changes only when it has no last use or the new one is a granularity
-- newer, so a token costs one write per granularity however many replicas saw it.
-- The time is the replica's clock, clamped to the database's now(), so a replica whose
-- clock runs ahead cannot leave a time in the future that later uses would never beat.
-- The rows are locked in id order, so two replicas flushing together cannot deadlock.
WITH u AS (
    SELECT (e ->> 'id')::uuid AS id, least((e ->> 'at')::timestamptz, now()) AS at, NULLIF(e ->> 'addr', '')::inet AS addr
      FROM jsonb_array_elements(sqlc.arg(uses)::text::jsonb) AS e
), due AS (
    SELECT t.id
      FROM api_token t JOIN u ON u.id = t.id
     WHERE t.last_used_at IS NULL
        OR t.last_used_at < u.at - make_interval(secs => sqlc.arg(granularity_secs)::float8)
     ORDER BY t.id
       FOR UPDATE OF t
)
UPDATE api_token t
   SET last_used_at = u.at, last_used_addr = coalesce(u.addr, t.last_used_addr)
  FROM u, due
 WHERE t.id = u.id AND t.id = due.id;

-- name: IdentityRevokeCascade :many
-- Revokes a token and, in the same statement, every unrevoked token whose
-- derived_from chain leads to it; with follow_rotation also the rotation
-- successors and their derived tokens (R-AUTH-029). The root keeps its own
-- revocation fields when it is already revoked.
WITH RECURSIVE tree(id) AS (
    SELECT t0.id FROM api_token t0 WHERE t0.id = sqlc.arg(id)::text::uuid
    UNION
    SELECT c.id FROM api_token c JOIN tree ON c.derived_from = tree.id
     OR (sqlc.arg(follow_rotation)::boolean AND c.rotated_from = tree.id)
)
UPDATE api_token a
   SET revoked_at = now(), revoked_by = NULLIF(sqlc.arg(revoked_by)::text, '')::uuid,
       revocation_reason = CASE WHEN a.id = sqlc.arg(id)::text::uuid THEN sqlc.arg(reason)::text ELSE 'parent_revoked' END
  FROM tree
 WHERE a.id = tree.id AND a.revoked_at IS NULL
RETURNING a.id::text AS id, encode(a.secret_sha256, 'hex') AS secret_hex, a.principal_id::text AS owner_id,
          a.type, (a.id = sqlc.arg(id)::text::uuid) AS is_root, coalesce(a.technical_token_id::text, '')::text AS technical_token_id;

-- name: IdentityLockTokenTree :many
-- The first statement of a revocation (R-AUTH-029): it locks the token and the
-- tree IdentityRevokeCascade would walk, in id order. A create or rotation that
-- hangs a token on the tree locks the same rows (IdentityLockTokenLineage), so
-- it either commits first, and the cascade, a statement of its own with a newer
-- snapshot, sees the new token; or it waits and then finds its parent revoked.
WITH RECURSIVE tree(id) AS (
    SELECT t0.id FROM api_token t0 WHERE t0.id = sqlc.arg(id)::text::uuid
    UNION
    SELECT c.id FROM api_token c JOIN tree ON c.derived_from = tree.id
     OR (sqlc.arg(follow_rotation)::boolean AND c.rotated_from = tree.id)
)
SELECT a.id::text AS id FROM api_token a JOIN tree ON tree.id = a.id ORDER BY a.id FOR UPDATE OF a;

-- name: IdentityLockTokenLineage :many
-- Locks, in id order, the tokens a new token hangs on (the token it is derived
-- from, the token it rotates) and every token on their derived_from chains
-- (R-AUTH-027, R-AUTH-030). The facts are read by IdentityTokenLineageFacts in a
-- statement of its own, so they include what committed while this one waited.
WITH RECURSIVE up(id) AS (
    SELECT t.id FROM api_token t WHERE t.id = ANY(sqlc.arg(ids)::text[]::uuid[])
    UNION
    SELECT t.derived_from FROM api_token t JOIN up ON t.id = up.id WHERE t.derived_from IS NOT NULL
)
SELECT a.id::text AS id FROM api_token a JOIN up ON up.id = a.id ORDER BY a.id FOR UPDATE OF a;

-- name: IdentityTokenLineageFacts :many
-- What a create or rotation checks on the rows IdentityLockTokenLineage locked:
-- whether each is still live, its expiry in Unix microseconds (0 for none),
-- whether it may rotate itself, and whether it was already rotated.
SELECT a.id::text AS id, a.self_rotatable,
       (a.revoked_at IS NULL AND (a.expires_at IS NULL OR a.expires_at > now()))::boolean AS live,
       coalesce((extract(epoch FROM a.expires_at) * 1000000)::bigint, 0)::bigint AS expires_us,
       EXISTS (SELECT 1 FROM api_token s WHERE s.rotated_from = a.id) AS rotated
  FROM api_token a
 WHERE a.id = ANY(sqlc.arg(ids)::text[]::uuid[]);

-- name: IdentityTokenDescendsFrom :one
-- Whether a token is in the line of another (R-AUTH-028 to R-AUTH-030, Q-AUTH-199): whether its derived_from chain
-- leads to that token or to one of that token's rotation predecessors (its rotated_from chain), whose successors carry
-- the identical scope. These are what a token whose scope does not inherit its owner's rights sees and acts on by
-- ownership besides itself. A token is not in its own line through this query, and a predecessor itself is not in it;
-- a chain whose next row the retention task has deleted ends there, so the answer fails closed (Q-AUTH-15).
WITH RECURSIVE roots(id) AS (
    SELECT sqlc.arg(ancestor)::text::uuid
    UNION
    SELECT t.rotated_from FROM api_token t JOIN roots ON t.id = roots.id WHERE t.rotated_from IS NOT NULL
), up(id) AS (
    SELECT t.derived_from FROM api_token t WHERE t.id = sqlc.arg(id)::text::uuid AND t.derived_from IS NOT NULL
    UNION
    SELECT t.derived_from FROM api_token t JOIN up ON t.id = up.id WHERE t.derived_from IS NOT NULL
)
SELECT EXISTS (SELECT 1 FROM up JOIN roots ON roots.id = up.id)::boolean AS descends;

-- name: IdentityTokenBySecretForRevoke :many
SELECT t.id::text AS id, t.principal_id::text AS owner_id, p.name AS owner_name, p.kind AS owner_kind
  FROM api_token t JOIN principal p ON p.id = t.principal_id
 WHERE t.secret_sha256 = sqlc.arg(secret_sha256)::bytea;

-- name: IdentityRevokeAllOfPrincipal :many
UPDATE api_token SET revoked_at = now(), revoked_by = NULLIF(sqlc.arg(revoked_by)::text, '')::uuid,
                     revocation_reason = sqlc.arg(reason)::text
 WHERE principal_id = sqlc.arg(principal_id)::text::uuid AND revoked_at IS NULL
RETURNING id::text AS id, coalesce(technical_token_id::text, '')::text AS technical_token_id;

-- name: IdentityCountTokensOfPrincipal :one
SELECT count(*) FROM api_token WHERE principal_id = sqlc.arg(principal_id)::text::uuid;

-- name: IdentitySetTokenExpiry :many
-- Rotation: the predecessor expires at the earlier of its expiry and now + overlap.
UPDATE api_token SET expires_at = least(coalesce(expires_at, 'infinity'::timestamptz), now() + make_interval(secs => sqlc.arg(overlap_secs)::float8))
 WHERE id = sqlc.arg(id)::text::uuid AND revoked_at IS NULL
RETURNING expires_at, encode(secret_sha256, 'hex') AS secret_hex;

-- name: IdentityDeleteExpiredTokens :one
WITH d AS (
    DELETE FROM api_token WHERE id IN (
        SELECT id FROM api_token
         WHERE least(coalesce(revoked_at, 'infinity'::timestamptz), coalesce(expires_at, 'infinity'::timestamptz))
               < now() - make_interval(secs => sqlc.arg(retention_secs)::float8)
         ORDER BY id LIMIT sqlc.arg(max_rows)::int)
    RETURNING 1)
SELECT count(*) FROM d;

-- name: IdentityDeleteEndedTechnicalTokens :one
-- A technical token whose last secret row the retention deleted is deleted with it (R-AUTH-045): a technical token is created
-- with its first secret in one transaction, so one without any row has none live and no history. A technical token a new
-- secret holds locked (IdentityLockTechnicalToken) is skipped and looked at by the next run.
WITH d AS (
    DELETE FROM technical_token t
     WHERE t.id IN (SELECT x.id FROM technical_token x
                     WHERE NOT EXISTS (SELECT 1 FROM api_token a WHERE a.technical_token_id = x.id)
                       FOR UPDATE SKIP LOCKED)
    RETURNING 1)
SELECT count(*) FROM d;

-- Technical tokens (R-AUTH-045) ---------------------------------------------------

-- name: IdentityInsertTechnicalToken :many
-- No row: the account holds a technical token of the name (409 already_exists).
INSERT INTO technical_token (id, service_account_id, name, description, created_by)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(account_id)::text::uuid, sqlc.arg(name)::text, sqlc.arg(description)::text,
        NULLIF(sqlc.arg(created_by)::text, '')::uuid)
ON CONFLICT (service_account_id, name) DO NOTHING
RETURNING created_at;

-- name: IdentityCountTechnicalTokens :one
-- The technical tokens of an account, ended ones included (auth.tokens.max_technical_tokens_per_account).
SELECT count(*) FROM technical_token WHERE service_account_id = sqlc.arg(account_id)::text::uuid;

-- name: IdentityCountLiveTechnicalTokens :one
-- The technical tokens of an account with a live secret (auth.tokens.max_active_per_principal.sat): a rotation's predecessor in
-- its overlap belongs to the same technical token and is not counted again.
SELECT count(DISTINCT a.technical_token_id) FROM api_token a
 WHERE a.principal_id = sqlc.arg(account_id)::text::uuid AND a.type = 'sat'
   AND a.revoked_at IS NULL AND (a.expires_at IS NULL OR a.expires_at > now());

-- name: IdentityTechnicalTokens :many
-- The technical tokens of an account by name, or the one of the name when name is not empty, with the name of their creator.
SELECT t.id::text AS id, t.service_account_id::text AS account_id, t.name, t.description, t.created_at, t.revision,
       coalesce(c.kind, '')::text AS created_by_kind, coalesce(c.name, '')::text AS created_by_name
  FROM technical_token t LEFT JOIN principal c ON c.id = t.created_by
 WHERE t.service_account_id = sqlc.arg(account_id)::text::uuid
   AND (sqlc.arg(name)::text = '' OR t.name = sqlc.arg(name)::text)
 ORDER BY t.name;

-- name: IdentityTechnicalTokenSecrets :many
-- The secret rows of the technical tokens of an account, newest first per technical token: the record shows the live ones, or
-- the last one of a token that has none (R-AUTH-045).
SELECT a.id::text AS id, a.technical_token_id::text AS technical_token_id, a.hint, a.scope, a.created_at, a.expires_at,
       a.revoked_at, a.last_used_at, coalesce(a.rotated_from::text, '')::text AS rotated_from,
       (a.revoked_at IS NULL AND (a.expires_at IS NULL OR a.expires_at > now()))::boolean AS live
  FROM api_token a
 WHERE a.principal_id = sqlc.arg(account_id)::text::uuid AND a.type = 'sat'
   AND (sqlc.arg(technical_token_id)::text = '' OR a.technical_token_id = NULLIF(sqlc.arg(technical_token_id)::text, '')::uuid)
 ORDER BY a.technical_token_id, a.created_at DESC, a.id DESC;

-- name: IdentityLockTechnicalToken :many
-- The technical token of the name, locked: a new secret, a description change and a delete of one technical token run one after
-- the other.
SELECT t.id::text AS id, t.name, t.description, t.revision
  FROM technical_token t
 WHERE t.service_account_id = sqlc.arg(account_id)::text::uuid AND t.name = sqlc.arg(name)::text
   FOR UPDATE;

-- name: IdentityTechnicalTokenLive :one
-- Whether a technical token has a live secret (409 technical_token_active for a new secret).
SELECT EXISTS (SELECT 1 FROM api_token a
                WHERE a.technical_token_id = sqlc.arg(id)::text::uuid
                  AND a.revoked_at IS NULL AND (a.expires_at IS NULL OR a.expires_at > now()))::boolean AS live;

-- name: IdentityUpdateTechnicalToken :exec
UPDATE technical_token SET description = sqlc.arg(description)::text, revision = revision + 1 WHERE id = sqlc.arg(id)::text::uuid;

-- name: IdentityLiveSecretsOf :many
-- The unrevoked secrets of a technical token, in id order: its delete revokes each (R-AUTH-045).
SELECT a.id::text AS id FROM api_token a
 WHERE a.technical_token_id = sqlc.arg(id)::text::uuid AND a.revoked_at IS NULL
 ORDER BY a.id;

-- name: IdentityDeleteTechnicalToken :execrows
DELETE FROM technical_token WHERE id = sqlc.arg(id)::text::uuid;

-- name: IdentityTokensEnteringNotice :many
SELECT t.id::text AS id, t.type, t.hint, t.description, t.expires_at, p.kind AS owner_kind, p.name AS owner_name, p.id::text AS owner_id
  FROM api_token t JOIN principal p ON p.id = t.principal_id
 WHERE t.revoked_at IS NULL AND t.type IN ('pat', 'sat') AND t.expiry_notified_at IS NULL
   AND t.expires_at IS NOT NULL AND t.expires_at > now()
   AND t.expires_at <= now() + make_interval(secs => sqlc.arg(notice_secs)::float8)
 ORDER BY t.expires_at
 LIMIT sqlc.arg(max_rows)::int;

-- name: IdentityMarkExpiryNotified :many
UPDATE api_token SET expiry_notified_at = now() WHERE id = sqlc.arg(id)::text::uuid AND expiry_notified_at IS NULL
RETURNING id::text AS id;

-- Runtime settings -----------------------------------------------------------

-- name: IdentityGetSettings :many
-- Intervals travel as seconds; -1 is NULL (the YAML value applies).
SELECT new_users_external,
       coalesce(extract(epoch FROM pat_default_ttl), -1)::float8 AS pat_default_secs,
       coalesce(extract(epoch FROM pat_max_ttl), -1)::float8 AS pat_max_secs,
       coalesce(extract(epoch FROM sat_default_ttl), -1)::float8 AS sat_default_secs,
       coalesce(extract(epoch FROM sat_max_ttl), -1)::float8 AS sat_max_secs,
       coalesce(extract(epoch FROM expiry_notice), -1)::float8 AS expiry_notice_secs,
       revision
  FROM auth_settings;

-- name: IdentityUpdateSettings :many
-- expected is the revision the caller loaded, or -1 for a blind write (If-Match: *);
-- no returned row means the revision moved on.
UPDATE auth_settings
   SET new_users_external = sqlc.arg(new_users_external)::boolean,
       pat_default_ttl = CASE WHEN sqlc.arg(pat_default_secs)::float8 < 0 THEN NULL ELSE make_interval(secs => sqlc.arg(pat_default_secs)::float8) END,
       pat_max_ttl = CASE WHEN sqlc.arg(pat_max_secs)::float8 < 0 THEN NULL ELSE make_interval(secs => sqlc.arg(pat_max_secs)::float8) END,
       sat_default_ttl = CASE WHEN sqlc.arg(sat_default_secs)::float8 < 0 THEN NULL ELSE make_interval(secs => sqlc.arg(sat_default_secs)::float8) END,
       sat_max_ttl = CASE WHEN sqlc.arg(sat_max_secs)::float8 < 0 THEN NULL ELSE make_interval(secs => sqlc.arg(sat_max_secs)::float8) END,
       expiry_notice = CASE WHEN sqlc.arg(expiry_notice_secs)::float8 < 0 THEN NULL ELSE make_interval(secs => sqlc.arg(expiry_notice_secs)::float8) END,
       revision = revision + 1, updated_at = now(), updated_by = NULLIF(sqlc.arg(updated_by)::text, '')::uuid
 WHERE singleton AND (sqlc.arg(expected)::bigint < 0 OR revision = sqlc.arg(expected)::bigint)
RETURNING revision;

-- name: IdentityLowerPATMax :many
-- Replay of settings.auth.changed (Q-AUTH-207): the runtime maximum is taken to the journaled value when that is shorter; NULL is the
-- YAML value and the longest. A default above the new maximum is taken down to it, a NULL default stays NULL (the effective
-- value clamps it). No row: the maximum is at most the value already.
UPDATE auth_settings
   SET pat_max_ttl = make_interval(secs => sqlc.arg(secs)::float8),
       pat_default_ttl = CASE WHEN pat_default_ttl > make_interval(secs => sqlc.arg(secs)::float8) THEN make_interval(secs => sqlc.arg(secs)::float8) ELSE pat_default_ttl END,
       revision = revision + 1, updated_at = now()
 WHERE singleton AND (pat_max_ttl IS NULL OR pat_max_ttl > make_interval(secs => sqlc.arg(secs)::float8))
RETURNING revision;

-- name: IdentityLowerSATMax :many
-- As IdentityLowerPATMax, for service account tokens.
UPDATE auth_settings
   SET sat_max_ttl = make_interval(secs => sqlc.arg(secs)::float8),
       sat_default_ttl = CASE WHEN sat_default_ttl > make_interval(secs => sqlc.arg(secs)::float8) THEN make_interval(secs => sqlc.arg(secs)::float8) ELSE sat_default_ttl END,
       revision = revision + 1, updated_at = now()
 WHERE singleton AND (sat_max_ttl IS NULL OR sat_max_ttl > make_interval(secs => sqlc.arg(secs)::float8))
RETURNING revision;

-- Group members, one row per member (R-AUTH-018) --------------------------------

-- name: IdentityGroupMemberPage :many
-- The members of a group ordered by kind and name, each with all its sources; the
-- optional source keeps the members that hold it. after_kind and after_name are the
-- last member of the previous page ('' on the first). A user comes with what its row shows (R-UI-151): the ID (the seed of the
-- generated avatar), the display name and the avatar's digest.
SELECT mp.kind AS member_kind, mp.name AS member_name, array_agg(m.source ORDER BY m.source)::text[] AS sources,
       mp.id::text AS member_id, coalesce(u.display_name, '')::text AS display_name,
       coalesce(encode(a.blob_sha256, 'hex'), '')::text AS avatar_sha256
  FROM group_membership m JOIN principal mp ON mp.id = m.member_id
  LEFT JOIN app_user u ON u.principal_id = mp.id
  LEFT JOIN user_avatars a ON a.user_id = mp.id
 WHERE m.group_id = sqlc.arg(group_id)::text::uuid
 GROUP BY mp.kind, mp.name, mp.id, u.display_name, a.blob_sha256
HAVING (mp.kind, mp.name) > (sqlc.arg(after_kind)::text, sqlc.arg(after_name)::text)
   AND (sqlc.arg(source)::text = '' OR sqlc.arg(source)::text = ANY(array_agg(m.source)))
 ORDER BY mp.kind, mp.name
 LIMIT sqlc.arg(max_rows)::int;

-- User preferences (R-API-021) ----------------------------------------------------

-- name: PreferencesGet :many
SELECT coalesce(locale, '')::text AS locale, coalesce(theme, '')::text AS theme, keyboard_shortcuts, revision
  FROM user_preferences WHERE user_id = sqlc.arg(user_id)::text::uuid;

-- name: PreferencesUpsert :many
-- A user without a row has the defaults at revision 1; the first write inserts
-- revision 2. if_revision is 0 for "no condition" (If-Match: *); a row that exists
-- is updated only when its revision matches, so a concurrent write is a 412.
INSERT INTO user_preferences AS p (user_id, locale, theme, keyboard_shortcuts, revision)
VALUES (sqlc.arg(user_id)::text::uuid, NULLIF(sqlc.arg(locale)::text, ''), NULLIF(sqlc.arg(theme)::text, ''),
        sqlc.arg(keyboard_shortcuts)::boolean, 2)
ON CONFLICT (user_id) DO UPDATE
   SET locale = EXCLUDED.locale, theme = EXCLUDED.theme, keyboard_shortcuts = EXCLUDED.keyboard_shortcuts,
       revision = p.revision + 1, updated_at = now()
 WHERE sqlc.arg(if_revision)::bigint = 0 OR p.revision = sqlc.arg(if_revision)::bigint
RETURNING coalesce(p.locale, '')::text AS locale, coalesce(p.theme, '')::text AS theme, p.keyboard_shortcuts,
          p.revision;

-- Project service accounts (R-AUTHZ-026, Q-AUTHZ-169) ----------------------------

-- name: IdentityProjectShare :many
-- The project a service account is created for, locked FOR SHARE: a concurrent
-- delete of the project (FOR UPDATE, which counts the accounts under that lock)
-- waits for this transaction, so the new account is counted or the project is gone.
SELECT id::text AS id, key FROM project WHERE key = sqlc.arg(key)::text FOR SHARE;

-- name: IdentityIsProjectServiceAccount :many
-- One row when the principal is a service account of a project; the key of that
-- project. A group membership of such an account is refused (Q-AUTHZ-175).
SELECT pr.key
  FROM service_account s
  JOIN project pr ON pr.id = s.project_id
 WHERE s.principal_id = sqlc.arg(id)::text::uuid;
