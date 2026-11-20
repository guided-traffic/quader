-- quader:phase expand
--
-- The built-in project upstream (repositories-and-proxy.md R-REPO-066 to R-REPO-071, authorization.md R-AUTHZ-041, ADR-209): its row,
-- the table that says which of its repositories the product declares and why one is off, and the guards that keep the project the
-- product's whatever a writer checked first (SQLSTATE AI010). The repositories themselves come from the reconcile of `quader migrate`
-- (R-REPO-067), not from this file. The project is stored public and fixed, so that each of its repositories can take the level an
-- administrator chooses (Q-AUTHZ-301); who sees the project is decided by what the caller reads in it, not by that level.

INSERT INTO project (id, key, display_name, description, visibility)
VALUES ('00000000-0000-0000-0000-000000000100', 'upstream', 'Upstream',
        'The public registries this installation offers to every project.', 'public');

-- One row per well-known repository; the repository row keeps the configuration as for any repository. The state of a preset
-- repository is derived (Q-REPO-375): disabled with first_switched_on_at is switched off, disabled without it was never switched on,
-- retired_at marks a tombstone the product no longer declares (R-REPO-067 step 5, Q-REPO-376). A system repository has no state of
-- its own (R-REPO-070). address_overridden says that an administrator set the url, the extra hosts or the preset settings
-- (R-REPO-069); account_removed_at that the reconcile removed the account when the product moved its hosts (Q-REPO-377). The foreign
-- key keeps a well-known repository's row: nothing deletes one.
CREATE TABLE upstream_repository (
    repository_id         bigint      PRIMARY KEY REFERENCES repositories (id) ON DELETE RESTRICT,
    kind                  text        NOT NULL CHECK (kind IN ('preset', 'system')),
    first_switched_on_at  timestamptz NULL,
    retired_at            timestamptz NULL,
    address_overridden    boolean     NOT NULL DEFAULT false,
    account_removed_at    timestamptz NULL,
    CHECK (kind = 'preset' OR first_switched_on_at IS NULL)
);

-- The project row: never deleted, and its display name, description and level never change. Its key is fixed for every project
-- (AI003, 00019).
CREATE FUNCTION upstream_project_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'the built-in project upstream is not deleted' USING ERRCODE = 'AI010';
    END IF;
    IF (NEW.display_name, NEW.description, NEW.visibility) IS DISTINCT FROM (OLD.display_name, OLD.description, OLD.visibility) THEN
        RAISE EXCEPTION 'the display name, the description and the level of the built-in project upstream do not change'
              USING ERRCODE = 'AI010';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER project_upstream_fixed BEFORE UPDATE OF display_name, description, visibility OR DELETE ON project
    FOR EACH ROW WHEN (OLD.id = '00000000-0000-0000-0000-000000000100') EXECUTE FUNCTION upstream_project_fixed();

-- The project has no members, policies, service accounts (of the project or of one of its repositories) or CI mappings
-- (R-AUTHZ-041, Q-AUTHZ-300). The grants of a project policy need the policy, and a repository's account names its project.
CREATE FUNCTION upstream_project_object_refused() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'the built-in project upstream holds no %', TG_TABLE_NAME USING ERRCODE = 'AI010';
END $$;
CREATE TRIGGER project_member_upstream BEFORE INSERT OR UPDATE OF project_id ON project_member
    FOR EACH ROW WHEN (NEW.project_id = '00000000-0000-0000-0000-000000000100') EXECUTE FUNCTION upstream_project_object_refused();
CREATE TRIGGER authz_policy_upstream BEFORE INSERT OR UPDATE OF project_id ON authz_policy
    FOR EACH ROW WHEN (NEW.project_id = '00000000-0000-0000-0000-000000000100') EXECUTE FUNCTION upstream_project_object_refused();
CREATE TRIGGER service_account_upstream BEFORE INSERT OR UPDATE OF project_id ON service_account
    FOR EACH ROW WHEN (NEW.project_id = '00000000-0000-0000-0000-000000000100') EXECUTE FUNCTION upstream_project_object_refused();
CREATE TRIGGER ci_mappings_upstream BEFORE INSERT OR UPDATE OF project_id ON ci_mappings
    FOR EACH ROW WHEN (NEW.project_id = '00000000-0000-0000-0000-000000000100') EXECUTE FUNCTION upstream_project_object_refused();

-- A repository of the project exists only with its row in upstream_repository, checked at commit so that the reconcile writes the
-- two rows in either order; the row is read again then, so a repository inserted and removed in one transaction passes.
CREATE FUNCTION upstream_repository_declared() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM repositories WHERE id = NEW.id AND project_id = '00000000-0000-0000-0000-000000000100')
       AND NOT EXISTS (SELECT 1 FROM upstream_repository WHERE repository_id = NEW.id) THEN
        RAISE EXCEPTION 'repository % of the built-in project upstream has no row in upstream_repository', NEW.id
              USING ERRCODE = 'AI010';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER repositories_upstream_declared AFTER INSERT ON repositories
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (NEW.project_id = '00000000-0000-0000-0000-000000000100') EXECUTE FUNCTION upstream_repository_declared();

-- A row of upstream_repository names a repository of the project, and only of it.
CREATE FUNCTION upstream_repository_of_project() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM repositories WHERE id = NEW.repository_id AND project_id = '00000000-0000-0000-0000-000000000100') THEN
        RAISE EXCEPTION 'upstream_repository names repository %, which is no repository of the built-in project upstream', NEW.repository_id
              USING ERRCODE = 'AI010';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER upstream_repository_project BEFORE INSERT OR UPDATE OF repository_id ON upstream_repository
    FOR EACH ROW EXECUTE FUNCTION upstream_repository_of_project();

-- A repository of the project keeps its project, key, class and format, and no repository of another project moves into it
-- (R-REPO-069: nobody creates, moves or renames a repository there).
CREATE FUNCTION upstream_repository_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.project_id, NEW.key, NEW.class, NEW.format) IS DISTINCT FROM (OLD.project_id, OLD.key, OLD.class, OLD.format) THEN
        RAISE EXCEPTION 'repository % keeps its project, key, class and format in the built-in project upstream, and none moves into it', OLD.id
              USING ERRCODE = 'AI010';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER repositories_upstream_fixed BEFORE UPDATE OF project_id, key, class, format ON repositories
    FOR EACH ROW WHEN (OLD.project_id = '00000000-0000-0000-0000-000000000100' OR NEW.project_id = '00000000-0000-0000-0000-000000000100')
    EXECUTE FUNCTION upstream_repository_fixed();

-- A virtual repository lists repositories of its own project and of upstream (R-REPO-071, Q-REPO-371): the same-project guard of
-- 00045 (AI006) admits a member of the built-in project in virtual_members and virtual_pin_members. A deploy target stays a
-- repository of the own project: a well-known repository is a remote.
CREATE OR REPLACE FUNCTION virtual_same_project_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    other uuid;
    own   uuid;
BEGIN
    PERFORM 1 FROM repositories
     WHERE id IN ((to_jsonb(NEW) ->> TG_ARGV[0])::bigint, (to_jsonb(NEW) ->> TG_ARGV[1])::bigint)
     ORDER BY id FOR SHARE;
    SELECT project_id INTO other FROM repositories WHERE id = (to_jsonb(NEW) ->> TG_ARGV[0])::bigint;
    SELECT project_id INTO own FROM repositories WHERE id = (to_jsonb(NEW) ->> TG_ARGV[1])::bigint;
    IF other = '00000000-0000-0000-0000-000000000100' AND TG_TABLE_NAME IN ('virtual_members', 'virtual_pin_members') THEN
        RETURN NEW;
    END IF;
    IF other IS DISTINCT FROM own THEN
        RAISE EXCEPTION '%.% must name a repository of the project of %.% or of the project upstream',
              TG_TABLE_NAME, TG_ARGV[0], TG_TABLE_NAME, TG_ARGV[1] USING ERRCODE = 'AI006';
    END IF;
    RETURN NEW;
END $$;

-- A virtual repository moves with its members of upstream (R-REPO-060, R-REPO-071, Q-REPO-389): only members of its own project
-- keep it from moving (AI007, 00024). Everything else of the guard stays.
CREATE OR REPLACE FUNCTION repository_move_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NOT DISTINCT FROM OLD.project_id THEN
        RETURN NEW;
    END IF;
    IF EXISTS (SELECT 1 FROM authz_policy_selector s JOIN authz_policy a ON a.id = s.policy_id
                WHERE s.repository_id = OLD.id AND a.project_id = OLD.project_id)
       OR EXISTS (SELECT 1 FROM virtual_members WHERE member_id = OLD.id)
       OR EXISTS (SELECT 1 FROM virtual_members vm JOIN repositories m ON m.id = vm.member_id
                   WHERE vm.virtual_id = OLD.id AND m.project_id IS DISTINCT FROM '00000000-0000-0000-0000-000000000100')
       OR EXISTS (SELECT 1 FROM virtual_pin_members WHERE member_id = OLD.id)
       OR EXISTS (SELECT 1 FROM virtual_deploy_targets WHERE target_id = OLD.id OR virtual_id = OLD.id) THEN
        RAISE EXCEPTION 'the repository % is held by its project and does not change it', OLD.id USING ERRCODE = 'AI007';
    END IF;
    RETURN NEW;
END $$;

-- The purge of a retired well-known repository (R-REPO-067 step 5, Q-REPO-376): the job removes its items in batches and then, in one
-- transaction, what the deletion of the row would have taken with it by its cascading foreign keys, while the row stays as a tombstone.
-- What stays is configuration, not content: its secrets, the grants, rules, limits and claims that name it, and the member rows of
-- the virtual repositories that keep it as a placeholder. The test of the integration area upstream holds this list to the foreign
-- keys of the schema, so a table that a migration adds is purged or kept by a decision.
CREATE FUNCTION upstream_repository_purge(rid bigint) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM upstream_repository WHERE repository_id = rid AND retired_at IS NOT NULL) THEN
        RAISE EXCEPTION 'repository % is no retired repository of the built-in project upstream', rid USING ERRCODE = 'AI010';
    END IF;
    DELETE FROM usage_limit_event WHERE repository_id = rid;
    DELETE FROM usage_daily_summary WHERE repository_id = rid;
    DELETE FROM usage_bucket WHERE repository_id = rid;
    DELETE FROM repository_usage WHERE repo_id = rid;
    DELETE FROM vuln_finding WHERE repo_id = rid;
    DELETE FROM remote_cache_invalidations WHERE repo_id = rid;
    DELETE FROM remote_url_map WHERE repo_id = rid;
    DELETE FROM remote_cache_entries WHERE repo_id = rid;
    DELETE FROM remote_upstream_state WHERE repo_id = rid;
    DELETE FROM mirror_pending WHERE repo_id = rid;
    DELETE FROM mirror_state WHERE repo_id = rid;
    DELETE FROM upload_sessions WHERE repo_id = rid OR addressed_repo_id = rid;
    DELETE FROM ansible_doc_fetches WHERE repo_id = rid;
    DELETE FROM ansible_version_docs WHERE repo_id = rid;
    DELETE FROM ansible_upstream_versions WHERE repo_id = rid;
    DELETE FROM ansible_import_tasks WHERE repo_id = rid;
    DELETE FROM ansible_tombstones WHERE repo_id = rid;
    DELETE FROM ansible_collection_versions WHERE repo_id = rid;
    DELETE FROM deb_remote_files WHERE repo_id = rid;
    DELETE FROM deb_remote_indexes WHERE repo_id = rid;
    DELETE FROM deb_remote_releases WHERE repo_id = rid;
    DELETE FROM deb_binaries WHERE repo_id = rid;
    DELETE FROM deb_suites WHERE repo_id = rid;
    DELETE FROM deb_tombstones WHERE repo_id = rid;
    DELETE FROM oci_tombstones WHERE repo_id = rid;
    DELETE FROM oci_manifests WHERE repo_id = rid;
    DELETE FROM nuget_symbol_file WHERE repository_id = rid;
    DELETE FROM nuget_tombstone WHERE repository_id = rid;
    DELETE FROM nuget_version WHERE repository_id = rid;
    DELETE FROM npm_tombstones WHERE repo_id = rid;
    DELETE FROM npm_remote_packages WHERE repo_id = rid;
    DELETE FROM npm_packages WHERE repo_id = rid;
    DELETE FROM packages WHERE repo_id = rid;
    DELETE FROM items WHERE repo_id = rid;
END $$;
