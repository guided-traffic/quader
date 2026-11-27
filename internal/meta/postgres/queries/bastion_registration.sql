-- Queries of the Bastion registrations on the main installation (R-BAS-004, R-BAS-005; the statements of work
-- package 1, docs/developer/bastion.md). A registration is a principal of the kind 'bastion' plus the row of
-- the table bastion (00032); the principal row itself is written by the identity statements
-- (IdentityInsertPrincipal with kind 'bastion', IdentitySetPrincipalStatus, IdentityDeletePrincipal), the
-- tokens by IdentityInsertToken and the revocation statements, so that the Bastion token follows the same
-- storage, validation and revocation bound as every token (R-AUTH-022, R-AUTH-023, R-AUTH-029). This file holds
-- what is specific to registrations. Identifiers travel as text and are cast here, an optional value is the
-- empty string, and statements that return at most one row are :many (the pattern of identity.sql).

-- name: BastionInsert :exec
-- The subtype row of a registration whose principal row was just inserted. The kind guard (AI001) refuses a
-- principal of another kind; the name rule is the principal check principal_bastion_name.
INSERT INTO bastion (principal_id, description, egress)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(description)::text, sqlc.arg(egress)::text[]::cidr[]);

-- name: BastionGet :many
-- A registration by name. revision is the ETag (R-API-012); enabled is the principal's status; handover is the switch of the
-- handover per blob (R-BAS-071, 00110); egress the networks the Bastion connects from (R-BAS-017, 00145), each in the text form of
-- cidr, which always names the prefix length; gui, docs, hide_repositories and the limits_* members the rest of its configuration
-- (R-BAS-093, 00147; R-BAS-131, 00157);
-- declared_at the mark of a registration the configuration declares (R-BAS-127, 00148), NULL otherwise; catalogue_* the builder's last
-- outcome for its public catalogue, read from the state the builder keeps (R-BAS-104, 00146): whether one is held, why it stopped, and
-- the times of its last run and of the last run that changed it.
SELECT p.id::text AS id, p.name, (p.status = 'active')::boolean AS enabled, p.created_at, p.revision, b.description, b.handover,
       b.egress::text[] AS egress, b.gui, b.docs, b.hide_repositories, b.limits_mode, b.limits_preset, b.limits_flood, b.limits_slots,
       b.limits_throughput, b.limits_ceiling, b.declared_at, (s.bastion_id IS NOT NULL)::boolean AS catalogue_held,
       coalesce(s.stopped, '')::text AS catalogue_stopped, s.checked_at AS catalogue_checked_at, s.built_at AS catalogue_built_at
  FROM principal p JOIN bastion b ON b.principal_id = p.id
  LEFT JOIN bastion_catalogue_state s ON s.bastion_id = p.id
 WHERE p.kind = 'bastion' AND p.name = sqlc.arg(name)::text;

-- name: BastionLock :many
-- The registration by name, locking the principal row: an update or a delete compares If-Match with the
-- revision in the transaction that writes.
SELECT p.id::text AS id, p.name, (p.status = 'active')::boolean AS enabled, p.revision,
       (b.declared_at IS NOT NULL)::boolean AS declared, coalesce(b.declared_token::text, '')::text AS declared_token
  FROM principal p JOIN bastion b ON b.principal_id = p.id
 WHERE p.kind = 'bastion' AND p.name = sqlc.arg(name)::text
   FOR UPDATE OF p;

-- name: BastionList :many
-- Keyset order by name; there are at most bastions.max_registrations of them, so the caller may pass an empty
-- after and a generous max_rows. The members are BastionGet's.
SELECT p.id::text AS id, p.name, (p.status = 'active')::boolean AS enabled, p.created_at, p.revision, b.description, b.handover,
       b.egress::text[] AS egress, b.gui, b.docs, b.hide_repositories, b.limits_mode, b.limits_preset, b.limits_flood, b.limits_slots,
       b.limits_throughput, b.limits_ceiling, b.declared_at, (s.bastion_id IS NOT NULL)::boolean AS catalogue_held,
       coalesce(s.stopped, '')::text AS catalogue_stopped, s.checked_at AS catalogue_checked_at, s.built_at AS catalogue_built_at
  FROM principal p JOIN bastion b ON b.principal_id = p.id
  LEFT JOIN bastion_catalogue_state s ON s.bastion_id = p.id
 WHERE p.kind = 'bastion' AND p.name > sqlc.arg(after)::text
 ORDER BY p.name
 LIMIT sqlc.arg(max_rows)::int;

-- name: BastionCount :one
-- The registrations that exist, for bastions.max_registrations (R-BAS-004).
SELECT count(*) FROM principal WHERE kind = 'bastion';

-- name: BastionUpdateDescription :exec
UPDATE bastion SET description = sqlc.arg(description)::text WHERE principal_id = sqlc.arg(id)::text::uuid;

-- name: BastionSetHandover :many
-- Switch the handover per blob of a registration on or off (R-BAS-071). No row when the member already is the one asked for.
UPDATE bastion SET handover = sqlc.arg(handover)::boolean
 WHERE principal_id = sqlc.arg(id)::text::uuid AND handover <> sqlc.arg(handover)::boolean
RETURNING principal_id::text AS id;

-- name: BastionSetEgress :many
-- Replace the networks a registration's Bastion connects from (R-BAS-017, R-UI-243); the service validated and ordered them. No row
-- when the list already is the one asked for.
UPDATE bastion SET egress = sqlc.arg(egress)::text[]::cidr[]
 WHERE principal_id = sqlc.arg(id)::text::uuid AND egress IS DISTINCT FROM sqlc.arg(egress)::text[]::cidr[]
RETURNING principal_id::text AS id;

-- name: BastionSnapshot :many
-- What every replica's snapshot of the registrations holds (internal/bastionmain/snapshot.go): the switch of the handover per blob
-- (R-BAS-071), the networks the Bastion connects from (R-BAS-017) and the configuration the handshake sends (R-BAS-093) of each
-- registration, with its revision and the rules of its place (R-LIM-043), each rule's profile figures as the API names them, in one
-- statement, so that the revision and the rules it reads are of one moment (Q-BAS-320). At most bastions.max_registrations rows, read
-- at the start, on a change of the area bastion_handover, bastion_egress, bastion_configuration or limits and once a minute; never by a
-- request.
SELECT b.principal_id::text AS id, p.name, p.revision, b.handover, b.egress::text[] AS egress, b.gui, b.docs, b.hide_repositories,
       b.limits_mode, b.limits_preset, b.limits_flood, b.limits_slots, b.limits_throughput, b.limits_ceiling,
       COALESCE((SELECT jsonb_agg(jsonb_build_object(
                    'id', a.id::text, 'whoKind', a.who_kind, 'whoRef', a.who_ref, 'profile', lp.key, 'overrides', a.overrides,
                    'builtin', a.builtin,
                    'figures', jsonb_build_object(
                        'requestsPerSecond', lp.requests_per_second, 'requestsBurst', lp.requests_burst,
                        'requestsPerHour', lp.requests_per_hour, 'cachedBytesPerSecond', lp.cached_bytes_per_second,
                        'cachedBytesPerHour', lp.cached_bytes_per_hour, 'uncachedBytesPerSecond', lp.uncached_bytes_per_second,
                        'uncachedBytesPerHour', lp.uncached_bytes_per_hour, 'upstreamRequestsPerHour', lp.upstream_requests_per_hour,
                        'parallelTransfers', lp.parallel_transfers, 'bytesPerDay', lp.bytes_per_day, 'refusedPerHour', lp.refused_per_hour))
                    ORDER BY a.id)
                   FROM limit_assignment a JOIN limit_profile lp ON lp.id = a.profile_id
                  WHERE a.bastion_id = b.principal_id), '[]'::jsonb)::jsonb AS rules
  FROM bastion b JOIN principal p ON p.id = b.principal_id
 WHERE p.kind = 'bastion'
 ORDER BY b.principal_id;

-- name: BastionPlaceRules :many
-- The rules of one registration's place (R-LIM-043) as the snapshot reads them, for the registration's own document and for an update
-- that derives its preset (R-LIM-046).
SELECT a.id::text AS id, a.who_kind, a.who_ref, lp.key AS profile, a.overrides, a.builtin,
       jsonb_build_object(
           'requestsPerSecond', lp.requests_per_second, 'requestsBurst', lp.requests_burst,
           'requestsPerHour', lp.requests_per_hour, 'cachedBytesPerSecond', lp.cached_bytes_per_second,
           'cachedBytesPerHour', lp.cached_bytes_per_hour, 'uncachedBytesPerSecond', lp.uncached_bytes_per_second,
           'uncachedBytesPerHour', lp.uncached_bytes_per_hour, 'upstreamRequestsPerHour', lp.upstream_requests_per_hour,
           'parallelTransfers', lp.parallel_transfers, 'bytesPerDay', lp.bytes_per_day, 'refusedPerHour', lp.refused_per_hour)::jsonb AS figures
  FROM limit_assignment a JOIN limit_profile lp ON lp.id = a.profile_id
 WHERE a.bastion_id = sqlc.arg(id)::text::uuid
 ORDER BY a.id;

-- name: BastionPlaceRulesOfAll :many
-- The kind rules of every registration's place, for the list of registrations and the preset each reads (R-LIM-046).
SELECT a.bastion_id::text AS bastion_id, a.who_ref, lp.key AS profile, a.overrides
  FROM limit_assignment a JOIN limit_profile lp ON lp.id = a.profile_id
 WHERE a.bastion_id IS NOT NULL AND a.who_kind = 'every'
 ORDER BY a.bastion_id, a.who_ref;

-- name: BastionSetConfiguration :many
-- Replace the switches gui, docs and hide_repositories and the limits of a registration (R-BAS-093, R-BAS-131); the service validated
-- the members as a Bastion does. No row when nothing differs.
UPDATE bastion SET gui = sqlc.arg(gui)::boolean, docs = sqlc.arg(docs)::boolean,
       hide_repositories = sqlc.arg(hide_repositories)::boolean, limits_mode = sqlc.arg(limits_mode)::text,
       limits_preset = sqlc.arg(limits_preset)::text, limits_flood = sqlc.arg(limits_flood)::text::jsonb,
       limits_slots = sqlc.arg(limits_slots)::text::jsonb, limits_throughput = sqlc.arg(limits_throughput)::text::jsonb,
       limits_ceiling = sqlc.arg(limits_ceiling)::text::jsonb
 WHERE principal_id = sqlc.arg(id)::text::uuid
   AND (gui, docs, hide_repositories, limits_mode, limits_preset, limits_flood, limits_slots, limits_throughput, limits_ceiling)
       IS DISTINCT FROM
       (sqlc.arg(gui)::boolean, sqlc.arg(docs)::boolean, sqlc.arg(hide_repositories)::boolean, sqlc.arg(limits_mode)::text,
        sqlc.arg(limits_preset)::text, sqlc.arg(limits_flood)::text::jsonb, sqlc.arg(limits_slots)::text::jsonb,
        sqlc.arg(limits_throughput)::text::jsonb, sqlc.arg(limits_ceiling)::text::jsonb)
RETURNING principal_id::text AS id;

-- name: BastionKindRulesInsert :exec
-- The five kind rules of a new registration's place (R-LIM-046), built-in at their place: who_refs[i] takes the profile profiles[i].
INSERT INTO limit_assignment (id, place_kind, bastion_id, who_kind, who_ref, profile_id, capped, builtin, note, created_by, updated_by)
SELECT gen_random_uuid(), 'bastion', sqlc.arg(id)::text::uuid, 'every', r.who, p.id, false, true, r.note,
       NULLIF(sqlc.arg(actor)::text, '')::uuid, NULLIF(sqlc.arg(actor)::text, '')::uuid
  FROM (SELECT unnest(sqlc.arg(who_refs)::text[]) AS who, unnest(sqlc.arg(profiles)::text[]) AS profile,
               unnest(sqlc.arg(notes)::text[]) AS note) r
  JOIN limit_profile p ON p.key = r.profile;

-- name: BastionKindRulesApply :many
-- A preset's kind rules (R-LIM-046): who_refs[i] takes the profile profiles[i] without overrides. A rule that already is the one
-- asked for is not written; the rules written are returned, so the caller records and announces only a change.
UPDATE limit_assignment a
   SET profile_id = p.id, overrides = '{}'::jsonb, version = a.version + 1, updated_at = now(),
       updated_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
  FROM (SELECT unnest(sqlc.arg(who_refs)::text[]) AS who, unnest(sqlc.arg(profiles)::text[]) AS profile) r
  JOIN limit_profile p ON p.key = r.profile
 WHERE a.bastion_id = sqlc.arg(id)::text::uuid AND a.who_kind = 'every' AND a.who_ref = r.who
   AND (a.profile_id <> p.id OR a.overrides <> '{}'::jsonb)
RETURNING a.id::text AS id, a.who_ref;

-- name: BastionHandoverOff :many
-- The journal's replay of bastion.handover.disabled (R-BAS-071, R-HA-054): the member off for the registration of the entry. No row
-- when it is off already or the registration is not in the database.
UPDATE bastion SET handover = false
 WHERE principal_id = sqlc.arg(id)::text::uuid AND handover
RETURNING principal_id::text AS id;

-- name: BastionSetEnabled :many
-- Enable or disable (R-BAS-004): a disabled registration refuses its tokens like a disabled service account
-- (R-AUTH-020). No row when the status already is the one asked for.
UPDATE principal SET status = CASE WHEN sqlc.arg(enabled)::boolean THEN 'active' ELSE 'disabled' END
 WHERE id = sqlc.arg(id)::text::uuid AND kind = 'bastion'
   AND status <> CASE WHEN sqlc.arg(enabled)::boolean THEN 'active' ELSE 'disabled' END
RETURNING id::text AS id;

-- name: BastionCountActiveTokens :one
-- The unrevoked, unexpired tokens of a registration, against auth.tokens.max_active_per_principal.bst
-- (R-BAS-004). Taken under BastionLock, so two issuances cannot both pass the limit.
SELECT count(*) FROM api_token
 WHERE principal_id = sqlc.arg(id)::text::uuid AND type = 'bst'
   AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now());

-- name: BastionListTokens :many
-- The tokens of a registration for the registration's document (R-BAS-004, R-BAS-007): ID, hint, expiry,
-- creation and last use. Revoked tokens are listed only when include_revoked is set. Newest first.
SELECT t.id::text AS id, t.hint, t.description, t.created_at, t.expires_at, t.revoked_at, t.last_used_at,
       coalesce(host(t.last_used_addr), '')::text AS last_used_addr,
       coalesce(t.expires_at IS NOT NULL AND t.expires_at <= now(), false)::boolean AS expired,
       EXISTS (SELECT 1 FROM bastion b WHERE b.declared_token = t.id)::boolean AS declared
  FROM api_token t
 WHERE t.principal_id = sqlc.arg(id)::text::uuid AND t.type = 'bst'
   AND (sqlc.arg(include_revoked)::boolean OR t.revoked_at IS NULL)
 ORDER BY t.id DESC;

-- name: BastionListTokensOfAll :many
-- The same for the list of registrations: one read for all of them.
SELECT t.principal_id::text AS bastion_id, t.id::text AS id, t.hint, t.description, t.created_at, t.expires_at,
       t.last_used_at, coalesce(t.expires_at IS NOT NULL AND t.expires_at <= now(), false)::boolean AS expired,
       EXISTS (SELECT 1 FROM bastion b WHERE b.declared_token = t.id)::boolean AS declared
  FROM api_token t
 WHERE t.type = 'bst' AND t.revoked_at IS NULL
 ORDER BY t.principal_id, t.id DESC;

-- name: BastionTokenOwner :many
-- The registration a token belongs to, for the revocation of one token by ID (R-BAS-004): no row when the
-- token is not a Bastion token of that registration. declared says it is the token the registration's configuration declares
-- (R-BAS-127), which the API does not revoke.
SELECT t.id::text AS id, t.principal_id::text AS bastion_id, t.revoked_at,
       EXISTS (SELECT 1 FROM bastion b WHERE b.declared_token = t.id)::boolean AS declared
  FROM api_token t JOIN principal p ON p.id = t.principal_id
 WHERE t.id = sqlc.arg(id)::text::uuid AND t.type = 'bst' AND p.kind = 'bastion' AND p.name = sqlc.arg(name)::text;

-- name: BastionTokenBySecret :many
-- The lookup of the Bastion authenticator (internal/bastionmain.TokenAuthenticator): one indexed read by the
-- secret hash, joined with the registration; a row of another type is never returned here. rejection_due says
-- whether token.rejected is due for this token (at most once an hour, as for every token, R-AUTH-023 step 4); declared whether it
-- is the token its registration's configuration declares (R-BAS-127), the one Bastion token that may have no expiry.
SELECT t.id::text AS token_id, t.principal_id::text AS bastion_id, p.name, (p.status = 'active')::boolean AS enabled,
       t.expires_at, coalesce(t.expires_at IS NOT NULL AND t.expires_at <= now(), false)::boolean AS expired,
       (t.revoked_at IS NOT NULL)::boolean AS revoked, t.hint,
       coalesce(t.rejection_logged_at IS NULL OR t.rejection_logged_at < now() - interval '1 hour', true)::boolean AS rejection_due,
       coalesce(b.declared_token = t.id, false)::boolean AS declared
  FROM api_token t JOIN principal p ON p.id = t.principal_id JOIN bastion b ON b.principal_id = p.id
 WHERE t.secret_sha256 = sqlc.arg(secret_sha256)::bytea AND t.type = 'bst' AND p.kind = 'bastion';


-- name: BastionDeclaredList :many
-- The registrations the configuration declares (R-BAS-127) with the hash of their declared token, for the run of quader migrate that
-- applies bastions.declared: the ones it no longer declares lose the mark and their token. Taken under the configuration lock.
SELECT p.id::text AS id, p.name, coalesce(b.declared_token::text, '')::text AS declared_token,
       coalesce(t.secret_sha256, ''::bytea)::bytea AS declared_sha256, (t.revoked_at IS NOT NULL)::boolean AS declared_revoked
  FROM principal p JOIN bastion b ON b.principal_id = p.id LEFT JOIN api_token t ON t.id = b.declared_token
 WHERE p.kind = 'bastion' AND b.declared_at IS NOT NULL
 ORDER BY p.name;

-- name: BastionSetDeclared :many
-- Set the mark of a registration the configuration declares, keeping the time of its first declaration, or clear it with its token
-- pointer (R-BAS-127). No row when the mark already is the one asked for.
UPDATE bastion
   SET declared_at = CASE WHEN sqlc.arg(declared)::boolean THEN coalesce(declared_at, now()) END,
       declared_token = CASE WHEN sqlc.arg(declared)::boolean THEN declared_token END
 WHERE principal_id = sqlc.arg(id)::text::uuid AND (declared_at IS NOT NULL) <> sqlc.arg(declared)::boolean
RETURNING principal_id::text AS id;

-- name: BastionSetDeclaredToken :exec
-- Point a declared registration at the token its configuration declares (R-BAS-127), or at none.
UPDATE bastion SET declared_token = NULLIF(sqlc.arg(token_id)::text, '')::uuid WHERE principal_id = sqlc.arg(id)::text::uuid;

-- name: BastionTokenByHash :many
-- A token whose value has the hash, whatever its state: quader migrate takes a value the configuration declares that was issued on
-- the screen as the declared token, and refuses one that is revoked, expired or another principal's (R-BAS-127). At most one row
-- (secret_sha256 is unique).
SELECT t.id::text AS id, t.principal_id::text AS principal_id, t.type, (t.revoked_at IS NOT NULL)::boolean AS revoked,
       coalesce(t.expires_at IS NOT NULL AND t.expires_at <= now(), false)::boolean AS expired, coalesce(p.name, '')::text AS owner_name
  FROM api_token t LEFT JOIN principal p ON p.id = t.principal_id
 WHERE t.secret_sha256 = sqlc.arg(secret_sha256)::bytea;

-- name: BastionTokenUnexpire :exec
-- A token issued on the screen whose value the configuration declares becomes the declared token: it no longer expires, its lifetime
-- is its declaration's, and the row is the configuration's (created_via configuration, which the check api_token_check requires of a
-- Bastion token without an expiry; token.created keeps how it was issued) (R-BAS-127, Q-BAS-466).
UPDATE api_token SET expires_at = NULL, created_via = 'configuration' WHERE id = sqlc.arg(id)::text::uuid AND type = 'bst';
