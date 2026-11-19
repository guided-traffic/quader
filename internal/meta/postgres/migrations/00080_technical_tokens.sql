-- quader:phase expand
--
-- Technical tokens on three levels (W-26, the owner's decisions 6, 12 and 13 of 2026-11-19): R-AUTH-045, R-AUTH-046,
-- R-AUTHZ-039, Q-AUTH-415, Q-AUTHZ-296, Q-AUTHZ-297.
--
-- A technical token is a named line of qdr_sat_ secrets of one service account; its ID is the subject of a budget and survives
-- every rotation and every new secret. Every row of api_token of type sat names one. A service account belongs to the instance, a
-- project or, new here, a repository: a repository's account holds a set of actions on its repository at the account, moves with
-- the repository to another project and is deleted with it.
--
-- The stored service-account tokens of an earlier release name no technical token and cannot be named by one: they are deleted
-- here, their accounts stay (pre-alpha, no installation has to keep them, Q-AUTH-418). The check of a principal's name
-- learns the name of a repository's account, <project key>/<repository key>/<name>; it admits every name the check of 00075
-- admitted, so it refuses nothing that exists. Replacing it is a drop, which the phase rules of R-HA-018 would hold for a contract
-- step; they are off until the first stable release (Q-ARCH-340), and the step is an expand step as 00074 is.

-- The stored name of a repository's service account (R-AUTH-046), each part in the grammar of its table (00056).
ALTER TABLE principal
    DROP CONSTRAINT principal_check,
    ADD CONSTRAINT principal_check CHECK (CASE kind
        WHEN 'user'            THEN name ~ '^[a-z0-9][a-z0-9._@+-]{0,127}$' AND name NOT IN ('anonymous', 'authenticated', 'system', 'me')
        WHEN 'group'           THEN name ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
        WHEN 'service_account' THEN name ~ '^((?=[a-z0-9-]{2,32}/)[a-z][a-z0-9]*(?:-+[a-z0-9]+)*/((?=[a-z0-9._-]{2,64}/)[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*/)?)?[a-z0-9][a-z0-9-]{1,62}$'
        WHEN 'anonymous'       THEN name = 'anonymous'
        WHEN 'ci_mapping'      THEN name ~ '^[a-z0-9][a-z0-9._-]{1,63}/((?=[a-z0-9-]{2,32}/)[a-z][a-z0-9]*(?:-+[a-z0-9]+)*/)?[a-z0-9][a-z0-9._-]{1,63}$'
    END);

CREATE TABLE technical_token (
    id                  uuid        PRIMARY KEY,
    service_account_id  uuid        NOT NULL REFERENCES service_account (principal_id) ON DELETE CASCADE,
    name                text        NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
    description         text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 1024),
    created_at          timestamptz NOT NULL DEFAULT now(),
    created_by          uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    -- The ETag of the technical token's record (R-API-012): a change of its description moves it.
    revision            bigint      NOT NULL DEFAULT 1,
    UNIQUE (service_account_id, name)
);

DELETE FROM api_token WHERE type = 'sat';
ALTER TABLE api_token ADD COLUMN technical_token_id uuid NULL REFERENCES technical_token (id) ON DELETE CASCADE;
ALTER TABLE api_token ADD CONSTRAINT api_token_sat_technical CHECK ((type = 'sat') = (technical_token_id IS NOT NULL));
CREATE INDEX api_token_technical ON api_token (technical_token_id) WHERE technical_token_id IS NOT NULL;

-- A technical token belongs to the account that owns its secrets: a secret of another account's technical token would carry
-- one account's rights under another's budget. SQLSTATE AI005, as the other guards of a service account's place.
CREATE FUNCTION api_token_technical_owner_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.technical_token_id IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM technical_token t
                        WHERE t.id = NEW.technical_token_id AND t.service_account_id = NEW.principal_id) THEN
        RAISE EXCEPTION 'a secret belongs to a technical token of its own service account' USING ERRCODE = 'AI005';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER api_token_technical_owner BEFORE INSERT OR UPDATE OF technical_token_id, principal_id ON api_token
    FOR EACH ROW EXECUTE FUNCTION api_token_technical_owner_guard();

-- The repository of a service account (R-AUTH-046): set at creation, never changed; the account goes with the repository's row.
ALTER TABLE service_account ADD COLUMN repository_id bigint NULL REFERENCES repositories (id) ON DELETE CASCADE;
CREATE INDEX service_account_repository ON service_account (repository_id) WHERE repository_id IS NOT NULL;

-- The actions a repository's account holds on its repository (R-AUTHZ-039, Q-AUTHZ-296). One row per repository account.
CREATE TABLE service_account_repository_grant (
    service_account_id  uuid   PRIMARY KEY REFERENCES service_account (principal_id) ON DELETE CASCADE,
    actions             text[] NOT NULL CHECK (cardinality(actions) >= 1
                          AND actions <@ ARRAY['read', 'annotate', 'deploy', 'overwrite', 'delete', 'manage']::text[])
);

-- Only a repository's account holds actions at the account: any other account is granted by policies (AI005).
CREATE FUNCTION service_account_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM service_account WHERE principal_id = NEW.service_account_id AND repository_id IS NOT NULL) THEN
        RAISE EXCEPTION 'only a service account of a repository holds actions at the account' USING ERRCODE = 'AI005';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER service_account_grant_repository BEFORE INSERT OR UPDATE OF service_account_id ON service_account_repository_grant
    FOR EACH ROW EXECUTE FUNCTION service_account_grant_guard();

-- A change of the actions is a change of the account: its revision (the ETag of R-API-012) moves.
CREATE TRIGGER service_account_grant_revision AFTER INSERT OR UPDATE ON service_account_repository_grant
    FOR EACH ROW EXECUTE FUNCTION principal_revision_bump('service_account_id');

-- The place of a service account (Q-AUTHZ-169, Q-AUTHZ-209, Q-AUTHZ-297). The function of 00019 learns the repository:
--   - an instance-wide account's name has no slash, and it has no project;
--   - a project's account is <project key>/<name>, its project never changes (AI003);
--   - a repository's account is <project key>/<repository key>/<name>, its project is the repository's, and its repository never
--     changes (AI005); its project changes only together with its repository's, in the transaction of a move (R-REPO-060).
CREATE OR REPLACE FUNCTION service_account_project_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    k  text;
    n  text;
    rk text;
    rp uuid;
    prefix text;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.repository_id IS DISTINCT FROM OLD.repository_id THEN
        RAISE EXCEPTION 'the repository of a service account cannot change' USING ERRCODE = 'AI005';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.project_id IS DISTINCT FROM OLD.project_id AND NEW.repository_id IS NULL THEN
        RAISE EXCEPTION 'the project of a service account cannot change' USING ERRCODE = 'AI003';
    END IF;
    SELECT name INTO n FROM principal WHERE id = NEW.principal_id;
    IF n IS NULL THEN
        RAISE EXCEPTION 'the principal of a service account does not exist' USING ERRCODE = 'AI003';
    END IF;
    IF NEW.repository_id IS NOT NULL THEN
        SELECT r.key, r.project_id, p.key INTO rk, rp, k
          FROM repositories r JOIN project p ON p.id = r.project_id
         WHERE r.id = NEW.repository_id;
        IF rk IS NULL OR NEW.project_id IS DISTINCT FROM rp THEN
            RAISE EXCEPTION 'a service account of a repository belongs to the repository''s project' USING ERRCODE = 'AI005';
        END IF;
        prefix := k || '/' || rk || '/';
        IF left(n, char_length(prefix)) <> prefix OR char_length(n) = char_length(prefix)
           OR position('/' IN substr(n, char_length(prefix) + 1)) > 0 THEN
            RAISE EXCEPTION 'the name of a repository''s service account is <project key>/<repository key>/<name>' USING ERRCODE = 'AI005';
        END IF;
    ELSIF NEW.project_id IS NULL THEN
        IF position('/' IN n) > 0 THEN
            RAISE EXCEPTION 'the name of an instance-wide service account has no project prefix' USING ERRCODE = 'AI003';
        END IF;
    ELSE
        SELECT key INTO k FROM project WHERE id = NEW.project_id;
        IF k IS NULL OR left(n, char_length(k) + 1) <> k || '/' OR char_length(n) = char_length(k) + 1
           OR position('/' IN substr(n, char_length(k) + 2)) > 0 THEN
            RAISE EXCEPTION 'the name of a project service account is <project key>/<name>' USING ERRCODE = 'AI003';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER service_account_project BEFORE INSERT OR UPDATE OF project_id, repository_id ON service_account
    FOR EACH ROW EXECUTE FUNCTION service_account_project_guard();

-- A repository's accounts follow it: at the end of the transaction that changes a repository's project, each of its accounts
-- belongs to the new project (R-REPO-060). The move renames them in its transaction; a move that does not is refused (AI005).
CREATE FUNCTION repository_accounts_follow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM service_account WHERE repository_id = NEW.id AND project_id IS DISTINCT FROM NEW.project_id) THEN
        RAISE EXCEPTION 'the service accounts of repository % did not move with it', NEW.id USING ERRCODE = 'AI005';
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER repository_accounts_follow AFTER UPDATE OF project_id ON repositories
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION repository_accounts_follow();

-- A repository's account is gone with its repository (ON DELETE CASCADE of repository_id); its principal row goes with it, and
-- takes the account's tokens, memberships and grants along (R-AUTH-046). The deletion of the repository's row notifies the
-- change areas; the identity hook of the deletion (internal/app/wire_identity.go) records the accounts' end before the row goes.
CREATE FUNCTION service_account_repository_gone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.repository_id IS NOT NULL THEN
        DELETE FROM principal WHERE id = OLD.principal_id;
    END IF;
    RETURN OLD;
END $$;
CREATE TRIGGER service_account_repository_gone AFTER DELETE ON service_account
    FOR EACH ROW EXECUTE FUNCTION service_account_repository_gone();

-- A repository's account is the principal of no policy, global or of a project, and of no role binding (R-AUTHZ-039): its
-- rights are the actions at the account. The function of 00024 learns it; SQLSTATE AI005 as before.
CREATE OR REPLACE FUNCTION project_account_principal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    p uuid;
    r bigint;
BEGIN
    SELECT project_id, repository_id INTO p, r FROM service_account WHERE principal_id = NEW.principal_id;
    IF r IS NOT NULL THEN
        RAISE EXCEPTION 'a repository''s service account is not a principal of %.%', TG_TABLE_NAME, 'principal_id'
              USING ERRCODE = 'AI005';
    END IF;
    IF p IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_ARGV[0] = 'global' OR p IS DISTINCT FROM (to_jsonb(NEW) ->> 'project_id')::uuid THEN
        RAISE EXCEPTION 'a project service account is not a principal of %.%', TG_TABLE_NAME, 'principal_id'
              USING ERRCODE = 'AI005';
    END IF;
    RETURN NEW;
END $$;
