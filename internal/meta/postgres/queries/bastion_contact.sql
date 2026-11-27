-- Queries of the last contact of a Bastion replica (R-BAS-010, R-BAS-007; work package 2 writes, work package
-- 1 reads). The table bastion_contact (00032) holds one row per registration and replica.

-- name: BastionContactUpsert :one
-- Record the contact of a replica from the handshake, at most once a minute per replica: the row is written
-- when it is new, when its last write is a minute old, or when a reported value differs. written says
-- whether a row was written; changed says whether a reported value differs from the stored row (or there was
-- none), which is the condition of the asynchronous event bastion.contact (R-BAS-010). edge_limit is the edge figure the
-- replica reports ('' for none) and catalogue_kept whether its catalogue survives a restart (NULL for none, R-BAS-093, 00147).
-- base_url_moved says whether the replica reports a base URL that none of the registration's replicas reported within the last 24
-- hours: a new deployment, or one back after a day's silence (R-BAS-135). Such a contact starts the builder of the public catalogue,
-- whose base URL is the lexically first listed one among the base URLs heard within 24 hours (BastionRecentBaseURLs, Q-BAS-524), so a new
-- deployment may move it (R-BAS-105, Q-BAS-499). It is false for a contact that reports none, for a new replica of a deployment that is
-- already heard, and for a pair of deployments whose replicas take turns, which would otherwise start a run at each of their writes.
WITH old AS (
    SELECT client_address, release, interface_version, base_url, edge_limit, catalogue_kept
      FROM bastion_contact
     WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid AND replica = sqlc.arg(replica)::text
), heard AS (
    SELECT DISTINCT base_url
      FROM bastion_contact
     WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid AND base_url <> '' AND contact_at > now() - interval '24 hours'
), up AS (
    INSERT INTO bastion_contact AS c (bastion_id, replica, contact_at, client_address, release, interface_version, base_url, token_id,
                                      edge_limit, catalogue_kept)
    VALUES (sqlc.arg(bastion_id)::text::uuid, sqlc.arg(replica)::text, now(), NULLIF(sqlc.arg(client_address)::text, '')::inet,
            sqlc.arg(release)::text, sqlc.arg(interface_version)::int, sqlc.arg(base_url)::text,
            NULLIF(sqlc.arg(token_id)::text, '')::uuid, sqlc.arg(edge_limit)::text,
            NULLIF(sqlc.arg(catalogue_kept)::text, '')::boolean)
    ON CONFLICT (bastion_id, replica) DO UPDATE
       SET contact_at = excluded.contact_at, client_address = excluded.client_address, release = excluded.release,
           interface_version = excluded.interface_version, base_url = excluded.base_url, token_id = excluded.token_id,
           edge_limit = excluded.edge_limit, catalogue_kept = excluded.catalogue_kept
     WHERE c.contact_at <= excluded.contact_at - interval '1 minute'
        OR c.client_address IS DISTINCT FROM excluded.client_address OR c.release <> excluded.release
        OR c.interface_version <> excluded.interface_version OR c.base_url <> excluded.base_url
        OR c.edge_limit <> excluded.edge_limit OR c.catalogue_kept IS DISTINCT FROM excluded.catalogue_kept
    RETURNING 1
)
SELECT (SELECT count(*) FROM up)::bigint AS written,
       (NOT EXISTS (SELECT 1 FROM old)
        OR EXISTS (SELECT 1 FROM old o
                    WHERE o.client_address IS DISTINCT FROM NULLIF(sqlc.arg(client_address)::text, '')::inet
                       OR o.release <> sqlc.arg(release)::text OR o.interface_version <> sqlc.arg(interface_version)::int
                       OR o.base_url <> sqlc.arg(base_url)::text OR o.edge_limit <> sqlc.arg(edge_limit)::text
                       OR o.catalogue_kept IS DISTINCT FROM NULLIF(sqlc.arg(catalogue_kept)::text, '')::boolean))::boolean AS changed,
       (sqlc.arg(base_url)::text <> ''
        AND NOT EXISTS (SELECT 1 FROM heard h WHERE h.base_url = sqlc.arg(base_url)::text))::boolean AS base_url_moved;

-- name: BastionContactList :many
-- The contacts of one registration, the most recent first (the registration's document, R-BAS-004).
SELECT replica, contact_at, coalesce(host(client_address), '')::text AS client_address, release, interface_version,
       base_url, coalesce(token_id::text, '')::text AS token_id, edge_limit, coalesce(catalogue_kept::text, '')::text AS catalogue_kept
  FROM bastion_contact
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid
 ORDER BY contact_at DESC, replica;

-- name: BastionContactListAll :many
-- The contacts of every registration, for the list.
SELECT bastion_id::text AS bastion_id, replica, contact_at, coalesce(host(client_address), '')::text AS client_address,
       release, interface_version, base_url, coalesce(token_id::text, '')::text AS token_id, edge_limit,
       coalesce(catalogue_kept::text, '')::text AS catalogue_kept
  FROM bastion_contact
 ORDER BY bastion_id, contact_at DESC, replica;

-- name: BastionRecentBaseURLs :many
-- The distinct base URLs a registration's replicas reported within the last 24 hours, one per deployment, in lexical order: what
-- client-setup with bastion=<name> and the builder of the public catalogue hold to server.additional_base_urls (R-BAS-007, R-BAS-135).
-- No row before the first contact or after a day without one.
SELECT DISTINCT c.base_url
  FROM bastion_contact c JOIN principal p ON p.id = c.bastion_id
 WHERE p.kind = 'bastion' AND p.name = sqlc.arg(name)::text AND c.base_url <> '' AND c.contact_at > now() - interval '24 hours'
 ORDER BY c.base_url;

-- name: BastionContactPrune :execrows
-- Drop the contacts of replicas not heard for older_than_secs seconds (30 days): the pod names of old
-- rollouts. Run by the writer after an upsert that inserted a row.
DELETE FROM bastion_contact WHERE contact_at < now() - make_interval(secs => sqlc.arg(older_than_secs)::float8);

-- name: BastionStatusUpsert :execrows
-- Replace the state a replica reported with its batch (R-LIM-045, 00147). A replica without a contact row writes nothing: the row
-- goes with the contact, and a replica reports only after its handshake.
INSERT INTO bastion_status AS s (bastion_id, replica, reported_at, status)
SELECT c.bastion_id, c.replica, now(), sqlc.arg(status)::text::jsonb
  FROM bastion_contact c
 WHERE c.bastion_id = sqlc.arg(bastion_id)::text::uuid AND c.replica = sqlc.arg(replica)::text
ON CONFLICT (bastion_id, replica) DO UPDATE SET reported_at = excluded.reported_at, status = excluded.status;

-- name: BastionStatusList :many
-- The states of one registration's replicas, reported within older_than_secs seconds, the newest first (R-UI-240).
SELECT replica, reported_at, status
  FROM bastion_status
 WHERE bastion_id = sqlc.arg(bastion_id)::text::uuid
   AND reported_at >= now() - make_interval(secs => sqlc.arg(within_secs)::float8)
 ORDER BY reported_at DESC, replica;

-- name: BastionStatusListAll :many
-- The same for the list of registrations.
SELECT bastion_id::text AS bastion_id, replica, reported_at, status
  FROM bastion_status
 WHERE reported_at >= now() - make_interval(secs => sqlc.arg(within_secs)::float8)
 ORDER BY bastion_id, reported_at DESC, replica;

-- name: BastionStatusPrune :execrows
-- Drop the states not reported again for older_than_secs seconds (an hour, Q-BAS-443): with them go the addresses of their blocks.
DELETE FROM bastion_status WHERE reported_at < now() - make_interval(secs => sqlc.arg(older_than_secs)::float8);
