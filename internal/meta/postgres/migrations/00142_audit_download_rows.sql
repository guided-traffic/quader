-- quader:phase expand
--
-- The audit log shows a download as one row and keeps one event per item (operations-and-observability.md R-OPS-054, R-OPS-055, the
-- owner's decision of 2026-11-26, ADR-297). artifact.downloaded stays one event per item served, and bastion.data_key.handed one per
-- data key handed to a Bastion; from this step on both name the unit their item belongs to (details.unit: an image's name, a version's
-- name@version), and the read of the screen's rows joins the events of one action and one unit that one caller caused without a pause
-- of more than a minute into one row (audit.DownloadGap).
--
-- audit_download_key is the key of such a row: the action, the repository, the unit and the caller (its kind, its principal, the token
-- it used and the address it came from), as a UUID made of the MD5 of their text, so that the index stays small whatever the names'
-- length. Two keys that collide join two callers' rows of one unit into one row of the screen, never two events: the events and the
-- export are unchanged. NULL where the event names no unit: an event written before this step, or of a format whose unit is one file.
CREATE FUNCTION audit_download_key(action text, repository text, unit text, actor_type text, actor_id uuid, token_id uuid,
                                   source_ip inet)
    RETURNS uuid
    LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT md5(action || E'\x1f' || repository || E'\x1f' || unit || E'\x1f' || actor_type || E'\x1f' || COALESCE(actor_id::text, '')
             || E'\x1f' || COALESCE(token_id::text, '') || E'\x1f' || COALESCE(host(source_ip), ''))::uuid
$$;

-- The walk of a row: the next event of a key after an event, and the check that an event has no predecessor of its key within the
-- pause. The index is on the partitioned table, so every monthly partition has it, those that audit_attach_partition() attaches later
-- as well; it holds the events of transfers that name a unit and nothing else.
CREATE INDEX audit_event_download_idx ON audit_event
    (audit_download_key(action, repository, details ->> 'unit', actor_type, actor_id, token_id, source_ip), occurred_at, id)
    WHERE action IN ('artifact.downloaded', 'bastion.data_key.handed') AND (details ->> 'unit') IS NOT NULL;
