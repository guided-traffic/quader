-- quader:phase expand
--
-- ClamAV's definitions as a repository format (formats/clamav.md R-CLAMAV-002, R-CLAMAV-003; repositories-and-proxy.md R-REPO-075;
-- ADR-225): the switch "ClamAV definition updates" of a system repository, and the side row of every version of a database.

-- clients_enabled: the endpoint of upstream/clamav serves clients at its level while it is true (Q-REPO-414). Only a system repository
-- can set it; the repository service sets it only for the declaration that lets clients in.
ALTER TABLE upstream_repository ADD COLUMN clients_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE upstream_repository ADD CONSTRAINT upstream_repository_clients_system CHECK (kind = 'system' OR NOT clients_enabled);

-- One row per version of a database (a package of the format, a version row of package_versions): every piece of the container's
-- metadata (rows.Container as metadata), the detached signature's facts, the marks of the set in use and of the one before it, and the
-- time the cleanup removed its files. The row goes with its version row; the cleanup keeps the version row and this one as history
-- (R-MAL-034), and the database task deletes both once malware.rescan.cycle has passed.
CREATE TABLE clamav_version (
    version_id        bigint      PRIMARY KEY REFERENCES package_versions (id) ON DELETE CASCADE,
    db                text        NOT NULL CHECK (db IN ('main', 'daily', 'bytecode')),
    version           integer     NOT NULL CHECK (version >= 1),
    -- The container's file name, NULL while only the detached signature is stored.
    file              text        NULL CHECK (file ~ '^(main|daily|bytecode)\.(cvd|cud)$'),
    built_at          timestamptz NULL,
    metadata          jsonb       NULL,
    sign_name         text        NULL CHECK (length(sign_name) <= 64),
    sign_size         bigint      NULL CHECK (sign_size >= 0),
    sign_signatures   integer     NULL CHECK (sign_signatures >= 0),
    -- A version may be named by the set in use and by the set before it at once: an unchanged main.
    in_use            boolean     NOT NULL DEFAULT false,
    previous          boolean     NOT NULL DEFAULT false,
    stored_at         timestamptz NOT NULL DEFAULT now(),
    files_removed_at  timestamptz NULL,
    CHECK (files_removed_at IS NULL OR NOT (in_use OR previous))
);

CREATE INDEX clamav_version_in_use ON clamav_version (db) WHERE in_use;
CREATE INDEX clamav_version_removed ON clamav_version (files_removed_at) WHERE files_removed_at IS NOT NULL;
