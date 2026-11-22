-- The installation's switch of the delivery by signed links of the object store and its details (storage.md R-STOR-014, 00108):
-- read by every replica's snapshot, written by the operations of the management API with If-Match.

-- name: DownloadLinkSettings :one
SELECT st.enabled, st.min_size, st.link_life_seconds, st.public_addresses::text AS public_addresses, st.version, st.updated_at,
       COALESCE(st.updated_by::text, '')::text AS updated_by,
       COALESCE(CASE p.kind WHEN 'ci_mapping' THEN 'ci' ELSE p.kind END || ':' || p.name, '')::text AS updated_by_name
  FROM download_link_settings st
  LEFT JOIN principal p ON p.id = st.updated_by
 WHERE st.id;

-- name: DownloadLinkSettingsForUpdate :one
SELECT enabled, min_size, link_life_seconds, public_addresses::text AS public_addresses, version
  FROM download_link_settings
 WHERE id
   FOR UPDATE;

-- name: UpdateDownloadLinkSettings :execrows
UPDATE download_link_settings
   SET enabled = sqlc.arg(enabled)::boolean, min_size = sqlc.arg(min_size)::bigint,
       link_life_seconds = sqlc.arg(link_life_seconds)::integer, public_addresses = sqlc.arg(public_addresses)::jsonb,
       version = version + 1, updated_at = now(), updated_by = NULLIF(sqlc.arg(actor)::text, '')::uuid
 WHERE id AND version = sqlc.arg(version)::bigint;

-- name: DisableDownloadLinks :execrows
-- The replay of storage.download_links.changed that switched the delivery off (R-STOR-014): off again, nothing else changed.
UPDATE download_link_settings
   SET enabled = false, version = version + 1, updated_at = now(), updated_by = NULL
 WHERE id AND enabled;
