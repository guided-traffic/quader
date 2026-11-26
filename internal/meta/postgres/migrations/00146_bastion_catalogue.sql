-- quader:phase expand
--
-- The public catalogue of a Bastion registration (bastion.md R-BAS-101 to R-BAS-108, W-35's package P6): the anonymous view of
-- the main installation, built per registration whose web interface is on, as documents named by the path grammar of R-BAS-103
-- and handed to the Bastion through a change list (internal/bastionmain/catalogue).
--
-- Every table is derived data: the builder renders it again from the packages, the findings and the verdicts. A backup does not
-- carry it (R-BAK-004, transient), so a restored installation starts with no catalogue; the state row it creates then starts its
-- sequence above every number handed out before (the clock), so the cursor a Bastion kept answers reset (R-BAS-101).

-- A document's bytes, gzip-compressed alone, addressed by the SHA-256 of its raw bytes (Q-BAS-346), once for every registration and
-- every path that holds the same bytes: the documents of a README, a docs entry or a set of findings do not change with the
-- registration, and an Ansible module's entry that did not change between two versions is one document (the measurement of
-- 2026-11-26: 14,200 entries were 1,264 distinct contents). A document no row names is deleted at the end of a builder's run.
CREATE TABLE bastion_catalogue_document (
    sha256 bytea   PRIMARY KEY CHECK (octet_length(sha256) = 32),
    size   integer NOT NULL CHECK (size >= 0),
    gzip   bytea   NOT NULL
);

-- The change list of a registration: one row per document path it ever held, the last change's number (seq), and the document or
-- deleted. unit names what the builder renders the row from: a package's catalogue ID, a generic repository
-- (files/<project>/<repo>) or '' for the documents of the registration as a whole (the index, the set-up per repository, the
-- signing anchors, the signature objects and the state); the builder replaces a unit's rows at once.
CREATE TABLE bastion_catalogue (
    bastion_id uuid        NOT NULL REFERENCES bastion (principal_id) ON DELETE CASCADE,
    path       text        COLLATE "C" NOT NULL CHECK (octet_length(path) BETWEEN 1 AND 4096),
    unit       text        COLLATE "C" NOT NULL,
    sha256     bytea       NULL REFERENCES bastion_catalogue_document (sha256),
    size       integer     NULL CHECK (size >= 0),
    seq        bigint      NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (bastion_id, path),
    -- A deleted path names no document; a live one names its document and its size.
    CHECK ((sha256 IS NULL) = (size IS NULL))
);
-- The change list reads after a cursor; the builder reads a unit's rows; the end of a run deletes the documents no row names.
CREATE INDEX bastion_catalogue_by_seq ON bastion_catalogue (bastion_id, seq);
CREATE INDEX bastion_catalogue_by_unit ON bastion_catalogue (bastion_id, unit);
CREATE INDEX bastion_catalogue_by_sha ON bastion_catalogue (sha256) WHERE sha256 IS NOT NULL;

-- The builder's state of a registration: the range of the change list's numbers (a cursor below floor or above seq answers
-- reset), the base URL the documents were rendered on, the salt every fingerprint carries (what changes every document at once:
-- the public set, the documentation switch, the base URL, the README policy), the reason the builder stopped for the registration
-- (bastion_base_url_unknown, shown on the card), the public set the routes answer (iface.CataloguePublic), and the times of the last
-- run that changed something and of the last run.
CREATE TABLE bastion_catalogue_state (
    bastion_id uuid        PRIMARY KEY REFERENCES bastion (principal_id) ON DELETE CASCADE,
    floor      bigint      NOT NULL CHECK (floor > 0),
    seq        bigint      NOT NULL CHECK (seq >= floor - 1),
    salt       bytea       NOT NULL DEFAULT '\x'::bytea,
    base_url   text        NOT NULL DEFAULT '',
    stopped    text        NOT NULL DEFAULT '' CHECK (stopped IN ('', 'bastion_base_url_unknown')),
    public     jsonb       NOT NULL DEFAULT '{"projects": [], "repositories": []}',
    built_at   timestamptz NULL,
    checked_at timestamptz NULL
);

-- The fingerprint of a unit when the builder last rendered it (R-BAS-105): the reconciliation renders a unit again only when the
-- fingerprint of its rows in the database differs. refs are the signature objects the unit's documents name, so that the set of
-- signature objects of the registration is their union.
CREATE TABLE bastion_catalogue_unit (
    bastion_id  uuid   NOT NULL REFERENCES bastion (principal_id) ON DELETE CASCADE,
    unit        text   COLLATE "C" NOT NULL,
    fingerprint bytea  NOT NULL,
    refs        text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (bastion_id, unit)
);
