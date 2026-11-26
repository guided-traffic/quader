-- quader:phase expand
--
-- The changelog of a collection version (formats/ansible.md R-ANS-053, Q-ANS-98): read from the archive, at a local publish and at the
-- fill of a remote's cached archive, so a table of its own and not a file of ansible_version_docs, whose rows a remote has only after the
-- upstream's docs-blob. One row per version whose archive was read for it: format none records that the archive holds no changelog, so the
-- reindex and the start-up check of a worker do not read it again. It is derived data, not exported by a backup (R-BAK-004) and rebuilt by
-- the job ansible-reindex; deleting the version, the item or the repository deletes the row.
CREATE TABLE ansible_changelogs (
    repo_id   bigint  NOT NULL,
    name      text    COLLATE "C" NOT NULL,
    version   text    COLLATE "C" NOT NULL,
    -- The version of the code that read it (docgen.Version); a reindex reads an older one again.
    generator integer NOT NULL CHECK (generator >= 0),
    -- markdown: changelogs/changelog.yaml rendered by the generator, or a CHANGELOG.md; text: a CHANGELOG.rst, shown as text; none: no
    -- changelog in the archive.
    format    text    NOT NULL CHECK (format IN ('markdown', 'text', 'none')),
    -- The member it was read from, '' for none.
    source    text    NOT NULL DEFAULT '' CHECK (length(source) <= 256),
    -- At most 512 KiB (docgen.ChangelogBody); truncated says a bound cut it.
    body      text    NOT NULL DEFAULT '' CHECK (octet_length(body) <= 524288),
    truncated boolean NOT NULL DEFAULT false,
    read_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, name, version),
    FOREIGN KEY (repo_id, name, version) REFERENCES ansible_collection_versions (repo_id, name, version) ON DELETE CASCADE,
    CHECK (format <> 'none' OR (source = '' AND body = ''))
);
