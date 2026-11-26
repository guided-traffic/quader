-- quader:phase expand
--
-- Who depends on a package (api-and-ui.md R-API-086, Q-API-455): one row per name a stored version depends on, in the kind its format
-- names (npm's manifest fields, Debian's relation fields; NuGet and Ansible Galaxy have one list, kind ''), with the range it asks for
-- as written, and the names a version provides (Debian's Provides, provides = true), under which it is "used" as well. name is the
-- name as the format matches it (NuGet's ID in lower case, Debian's without its architecture qualifier, an Ansible collection's
-- namespace.name in lower case, an npm name as written): the read compares it with packages.name. Written where a version's facts are
-- written (a local publish's commit, a remote's fill or describe) by the format's extractor (internal/packages/dependents), and for what
-- was stored before by the task remote-describe.
--
-- repo_id is the repository of the version, repeated here so that the read walks the index of the repositories the reader may read and
-- touches no row of another (R-API-086: a dependent the reader may not read changes neither the answer nor its time). It needs no key
-- of its own: the row goes with its version (the cascade from package_versions), and the version with its repository.
--
-- package_dependency_reads marks a version whose dependencies the extractor read, with the extractor's version: a version without
-- dependencies has no row above, and the task selects a version without a mark, or with an older extractor's, again.
--
-- Derived data: a backup exports neither table, and after a restore the task remote-describe reads the stored rows again (R-BAK-004).
CREATE TABLE package_dependencies (
    version_id bigint  NOT NULL REFERENCES package_versions (id) ON DELETE CASCADE,
    repo_id    bigint  NOT NULL,
    name       text    COLLATE "C" NOT NULL CHECK (length(name) BETWEEN 1 AND 256),
    kind       text    COLLATE "C" NOT NULL CHECK (length(kind) <= 64),
    range      text    COLLATE "C" NOT NULL CHECK (length(range) <= 256),
    provides   boolean NOT NULL DEFAULT false,
    PRIMARY KEY (version_id, provides, kind, name, range)
);
CREATE INDEX package_dependencies_by_name ON package_dependencies (repo_id, name) INCLUDE (version_id, provides);

CREATE TABLE package_dependency_reads (
    version_id bigint  PRIMARY KEY REFERENCES package_versions (id) ON DELETE CASCADE,
    reader     integer NOT NULL CHECK (reader >= 1)
);
