-- quader:phase expand
--
-- What the reading of a Debian package file's data archive keeps (formats/debian.md R-DEB-039, Q-DEB-140): the newest part of its
-- changelog, its copyright file with the licences of its machine-readable fields, the target of its documentation directory when that is
-- a link, and its file list, or the reason the reader refused the archive. One row per package file (local or cached by a remote), written
-- by the job deb-contents after the file is stored; the row goes with the file's row of deb_binaries. reader is the version of the reader
-- that wrote it: the task remote-describe queues a file again whose row an older reader wrote. Derived data: a backup does not export it
-- and a restore reads the files again (R-BAK-004).
--
-- files is a JSON array of objects {p: path, s: size, k: kind, t: target} in the archive's order, at most 10,000 of them and 1 MiB of
-- names; file_count counts every entry that is not a directory.
CREATE TABLE deb_contents (
    item_id             bigint      PRIMARY KEY REFERENCES deb_binaries (item_id) ON DELETE CASCADE,
    reader              integer     NOT NULL CHECK (reader >= 1),
    state               text        NOT NULL CHECK (state IN ('read', 'refused')),
    reason              text        NOT NULL DEFAULT '' CHECK (reason IN ('', 'unsupported_compression', 'invalid_archive', 'too_large',
                                                                       'too_many_entries', 'timeout', 'no_content')),
    detail              text        NOT NULL DEFAULT '' CHECK (octet_length(detail) <= 1024),
    changelog           text        NOT NULL DEFAULT '' CHECK (octet_length(changelog) <= 65536),
    changelog_file      text        NOT NULL DEFAULT '' CHECK (changelog_file IN ('', 'changelog.Debian.gz', 'changelog.gz')),
    changelog_truncated boolean     NOT NULL DEFAULT false,
    copyright           text        NOT NULL DEFAULT '' CHECK (octet_length(copyright) <= 65536),
    copyright_truncated boolean     NOT NULL DEFAULT false,
    licenses            text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(licenses) <= 32),
    doc_link            text        NOT NULL DEFAULT '' CHECK (octet_length(doc_link) <= 256),
    files               jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(files) = 'array'),
    file_count          integer     NOT NULL DEFAULT 0 CHECK (file_count >= 0),
    read_at             timestamptz NOT NULL DEFAULT now(),
    CHECK ((state = 'refused') = (reason <> ''))
);
