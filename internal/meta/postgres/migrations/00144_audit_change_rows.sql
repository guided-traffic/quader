-- quader:phase expand
--
-- The audit log shows a push, a publish and a delete as one row too (operations-and-observability.md R-OPS-056, the owner's rule of
-- 2026-11-26 that every download is the unit a person asked for, and the decision Q-OPS-446 that his choice for the downloads (W33,
-- ADR-297) holds for the changes): the events stay one per file and per version, and the read of the screen's rows joins the events of
-- one act on one unit that one caller caused without a pause of more than a minute into one row (audit.DownloadGap). From this step on
-- the events of a push or a publish (artifact.deployed, package.published, npm.publish), of a delete (artifact.deleted,
-- oci.manifest.deleted, deb.package.deleted, npm.unpublish, package.deleted, ansible.version.deleted) and the allowed writes
-- (authz.write.allowed) name a unit (details.unit) as a download does.
--
-- audit_row_key replaces 00142's audit_download_key. Its first part is the act an action is part of: the actions of a push and of a
-- delete each join one row, so the key holds the act, not the action; every other action is its own act (a download, the data keys
-- handed for it, the allowed writes, each a row beside the act's own). Then the repository, the unit and the caller (its kind, its
-- principal, its name, the token it used and the address it came from), as a UUID made of the MD5 of their text. The name joins the
-- key so that two system actors without a principal (a retention run and the purge of the trash) are two callers. Two keys that
-- collide join two callers' rows of one unit into one row of the screen, never two events: the events and the export are unchanged.
--
-- The index of 00142 gives way to audit_event_row_idx over every grouped action; the events written before name no unit and are not
-- in it. audit_event_actor_name_idx makes the filter by an actor's name an index condition of the events, the rows and the export
-- (R-OPS-017): the filter is a principal's ID (audit_event_actor_idx of 00006) or a name, never both.
DROP INDEX audit_event_download_idx;
DROP FUNCTION audit_download_key(text, text, text, text, uuid, uuid, inet);

-- The act is the CASE at the key's start: the actions of a push and of a delete each name one act, every other action is its own. It
-- is written into the key's function rather than a function of its own: PostgreSQL builds and maintains an index with a search path
-- of the system's schemas alone, where an unqualified call of another function of this schema is not found.
CREATE FUNCTION audit_row_key(action text, repository text, unit text, actor_type text, actor_id uuid, actor_name text,
                              token_id uuid, source_ip inet)
    RETURNS uuid
    LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT md5(CASE
               WHEN action IN ('artifact.deployed', 'package.published', 'npm.publish') THEN 'upload'
               WHEN action IN ('artifact.deleted', 'oci.manifest.deleted', 'deb.package.deleted', 'npm.unpublish', 'package.deleted',
                               'ansible.version.deleted') THEN 'delete'
               ELSE action
             END
             || E'\x1f' || COALESCE(repository, '') || E'\x1f' || unit || E'\x1f' || actor_type
             || E'\x1f' || COALESCE(actor_id::text, '') || E'\x1f' || COALESCE(actor_name, '') || E'\x1f'
             || COALESCE(token_id::text, '') || E'\x1f' || COALESCE(host(source_ip), ''))::uuid
$$;

-- The walk of a row: the next event of a key after an event, and the check that an event has no predecessor of its key within the
-- pause. The index is on the partitioned table, so every monthly partition has it, those that audit_attach_partition() attaches later
-- as well; it holds the events of the grouped actions that name a unit and nothing else.
CREATE INDEX audit_event_row_idx ON audit_event
    (audit_row_key(action, repository, details ->> 'unit', actor_type, actor_id, actor_name, token_id, source_ip), occurred_at, id)
    WHERE action IN ('artifact.downloaded', 'bastion.data_key.handed', 'artifact.deployed', 'package.published', 'npm.publish',
                     'artifact.deleted', 'oci.manifest.deleted', 'deb.package.deleted', 'npm.unpublish', 'package.deleted',
                     'ansible.version.deleted', 'authz.write.allowed')
      AND (details ->> 'unit') IS NOT NULL;

CREATE INDEX audit_event_actor_name_idx ON audit_event (actor_name, occurred_at DESC) WHERE actor_name IS NOT NULL;
