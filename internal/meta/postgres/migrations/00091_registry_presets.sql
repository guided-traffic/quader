-- quader:phase expand
--
-- The installation's registry presets (repositories-and-proxy.md R-REPO-076, Q-REPO-417 to Q-REPO-419): the list an administrator
-- keeps of the company's own registries, which the form of a new remote repository offers as buttons in every project. One row, present
-- from the start so that a read always finds it; presets is the list as the API answers it, each entry format, displayName, url and
-- extraHosts, validated by internal/repository before it is written. It holds no credential. version is the ETag source (R-API-012).

CREATE TABLE registry_presets (
    singleton   boolean     PRIMARY KEY DEFAULT true CHECK (singleton),
    presets     jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(presets) = 'array'),
    version     bigint      NOT NULL DEFAULT 1,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL
);
INSERT INTO registry_presets (singleton) VALUES (true);
