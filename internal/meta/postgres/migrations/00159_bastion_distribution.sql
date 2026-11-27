-- quader:phase expand
--
-- What a Bastion may distribute, granted twice (bastion.md R-BAS-134, the owner's words of 2026-11-27, Q-BAS-519, Q-REPO-491): an
-- administrator allows projects per Bastion (bastion_project; a new Bastion allows none), and a project's Owner chooses per repository
-- the Bastions that distribute it (repository_bastion), among those that allow the project. Public stays the main installation's level:
-- a repository leaves the network through a Bastion only while both rows exist.
--
-- The database holds the one rule both tables meet in, whatever the API checked: a repository is distributed through a Bastion only
-- while the Bastion allows the repository's project. A choice for a Bastion that does not allow the project is refused (AI011); taking
-- a project off a Bastion's list deletes its repositories' choices for that Bastion, and moving a repository to another project deletes
-- its choices, each in the same statement's transaction. An administrator's widening never brings back a choice by itself.

CREATE TABLE bastion_project (
    bastion_id  uuid        NOT NULL REFERENCES bastion (principal_id) ON DELETE CASCADE,
    project_id  uuid        NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    allowed_at  timestamptz NOT NULL DEFAULT now(),
    -- The actor's reference as the audit event names it: a principal ID, or the name of an actor without one (admin-cli, break-glass).
    allowed_by  text        NOT NULL CHECK (char_length(allowed_by) <= 256),
    PRIMARY KEY (bastion_id, project_id)
);
CREATE INDEX bastion_project_project ON bastion_project (project_id);

CREATE TABLE repository_bastion (
    repository_id bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    bastion_id    uuid        NOT NULL REFERENCES bastion (principal_id) ON DELETE CASCADE,
    since         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, bastion_id)
);
CREATE INDEX repository_bastion_bastion ON repository_bastion (bastion_id);

-- A choice needs its project allowed: the repository row and the allow row are locked FOR SHARE before the check, so a removal or a
-- move in flight waits for this transaction or makes it fail (the pattern of 00045).
CREATE FUNCTION repository_bastion_allowed_guard() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    pid uuid;
BEGIN
    SELECT project_id INTO pid FROM repositories WHERE id = NEW.repository_id FOR SHARE;
    PERFORM 1 FROM bastion_project WHERE bastion_id = NEW.bastion_id AND project_id = pid FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'the Bastion % does not allow the project of the repository %', NEW.bastion_id, NEW.repository_id
            USING ERRCODE = 'AI011';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER repository_bastion_allowed BEFORE INSERT OR UPDATE ON repository_bastion
    FOR EACH ROW EXECUTE FUNCTION repository_bastion_allowed_guard();

-- A project taken off a Bastion's list takes its repositories' choices for that Bastion with it.
CREATE FUNCTION bastion_project_removed_cascade() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM repository_bastion rb
     USING repositories r
     WHERE rb.bastion_id = OLD.bastion_id AND r.id = rb.repository_id AND r.project_id = OLD.project_id;
    RETURN OLD;
END;
$$;
CREATE TRIGGER bastion_project_removed AFTER DELETE ON bastion_project
    FOR EACH ROW EXECUTE FUNCTION bastion_project_removed_cascade();

-- A repository that moves to another project loses its choices (repositories-and-proxy.md R-REPO-060): its address and its project
-- change, and the target project's Owner chooses again.
CREATE FUNCTION repository_bastion_moved_cascade() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM repository_bastion WHERE repository_id = NEW.id;
    RETURN NEW;
END;
$$;
CREATE TRIGGER repository_bastion_moved AFTER UPDATE OF project_id ON repositories
    FOR EACH ROW WHEN (OLD.project_id IS DISTINCT FROM NEW.project_id)
    EXECUTE FUNCTION repository_bastion_moved_cascade();

-- The change area of the distribution (R-BAS-134): every change of either table announces it, and the public catalogue's builder starts
-- a run after it (R-BAS-105).
INSERT INTO cluster_epoch (area) VALUES ('bastion_distribution');
