-- quader:phase expand
--
-- Signatures on artifacts, stage 1, verification and trust (signatures.md R-SIG-008, R-SIG-011, R-SIG-012, R-SIG-015 to R-SIG-020,
-- R-SIG-041; Q-SIG-21, Q-SIG-26, Q-SIG-39, Q-SIG-42, Q-SIG-109): the evidence the trust step judges a signature by without reading
-- content, the installation's one row of settings (the switch for project Owners, the anchor epoch, the job's cursors and the state of
-- the task signature-trust-root), the trust anchors of the four scopes, the blocked signers, the trust rows of each scope that reaches
-- a signature, and the public Sigstore root and npm's keys the task stored. The store internal/signatures/store is the one door to
-- them; the job internal/signatures/job writes the verdicts and the trust rows.
--
-- A project is keyed by its uuid (00019_projects). No table here references repositories: a repository's removal takes its
-- attachments (00092), and the trust rows of what it reached are recomputed by the trust job.

-- What the trust step judges a signature by (scheme.Evidence as JSON, Q-SIG-109): the chain, the verified timestamps, a Sigstore bundle
-- with its digest, a keyless scheme's signature. Bounded twice the largest object (signatures.max_object_size is at most 1 MiB, but an
-- object carries its chain once; the evidence carries it once more at most).
ALTER TABLE signatures ADD COLUMN evidence bytea NULL CHECK (octet_length(evidence) <= 524288);

-- The installation's one row (R-SIG-019, R-SIG-012, R-SIG-011, R-SIG-017).
CREATE TABLE signature_settings (
    id                     boolean     PRIMARY KEY DEFAULT true CHECK (id),
    -- The switch: project Owners may add trust anchors for their project (off by default, R-SIG-019).
    project_anchors        boolean     NOT NULL DEFAULT false,
    -- Raised by every write of an anchor, a block, the switch and a stored public root; a trust row computed under a lower one is
    -- stale (R-SIG-012).
    anchor_epoch           bigint      NOT NULL DEFAULT 1 CHECK (anchor_epoch >= 1),
    -- Discovery's cursor over items in id order (R-SIG-011), and when the pass it belongs to started: a pass that reached the last
    -- item starts again from the first once a day.
    discovery_after        bigint      NOT NULL DEFAULT 0 CHECK (discovery_after >= 0),
    discovery_pass_at      timestamptz NOT NULL DEFAULT now(),
    -- The daily retry of failed signatures (R-SIG-011).
    failed_retried_at      timestamptz NULL,
    -- The last failure of the task signature-trust-root since its last success, and when the event signature.trust_root.failed was
    -- last recorded (at most once a day, R-SIG-039).
    trust_root_error       text        NULL CHECK (length(trust_root_error) <= 2000),
    trust_root_error_at    timestamptz NULL,
    trust_root_reported_at timestamptz NULL,
    -- version is the ETag of the settings (R-API-012); updated_by the principal of the last change of the switch.
    version                bigint      NOT NULL DEFAULT 1,
    updated_at             timestamptz NOT NULL DEFAULT now(),
    updated_by             uuid        NULL REFERENCES principal (id) ON DELETE SET NULL
);
INSERT INTO signature_settings (id) VALUES (true);

-- The trust anchors (R-SIG-015, R-SIG-016): shipped with the release, declared in the Helm values, added by an administrator, or
-- a project's own while the switch allows them.
CREATE TABLE signature_anchors (
    id          bigserial   PRIMARY KEY,
    scope       text        NOT NULL CHECK (scope IN ('shipped', 'configuration', 'installation', 'project')),
    project_id  uuid        NULL REFERENCES project (id) ON DELETE CASCADE,
    -- Unique per scope and project.
    key         text        NOT NULL CHECK (key ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(key) <= 64),
    -- The name shown at a signature.
    title       text        NOT NULL CHECK (length(title) BETWEEN 1 AND 128),
    kind        text        NOT NULL CHECK (kind IN ('ca', 'openpgp', 'public-key', 'sigstore')),
    -- anchors.Material with the kind's member: public material only.
    material    jsonb       NOT NULL CHECK (octet_length(material::text) <= 1048576),
    enabled     boolean     NOT NULL DEFAULT true,
    note        text        NULL CHECK (length(note) <= 1000),
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((scope = 'project') = (project_id IS NOT NULL)),
    UNIQUE NULLS NOT DISTINCT (scope, project_id, key)
);
CREATE INDEX signature_anchors_project ON signature_anchors (project_id) WHERE project_id IS NOT NULL;

-- The blocked signers (R-SIG-020): a certificate's or an OpenPGP key's fingerprint, an issuer and a serial, npm's or a raw key's
-- keyid, or a Sigstore identity (an OIDC issuer, NULL for any, and an identity pattern).
CREATE TABLE signature_blocks (
    id          bigserial   PRIMARY KEY,
    kind        text        NOT NULL CHECK (kind IN ('fingerprint', 'serial', 'keyid', 'identity')),
    value       text        NOT NULL CHECK (length(value) BETWEEN 1 AND 1024),
    issuer      text        NULL CHECK (issuer IS NULL OR (length(issuer) BETWEEN 1 AND 512 AND kind = 'identity')),
    reason      text        NOT NULL CHECK (length(reason) BETWEEN 1 AND 1000),
    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    UNIQUE NULLS NOT DISTINCT (kind, value, issuer)
);

-- The trust step's verdict of a valid or no_key signature in each scope that reaches it (R-SIG-008, Q-SIG-26): the installation's
-- (project_id NULL) and the project's of every project with anchors of its own that reaches it. Transient (R-SIG-041): computed from
-- the stored fields and recomputed after a restore.
CREATE TABLE signature_trust (
    signature_id  bigint      NOT NULL REFERENCES signatures (id) ON DELETE CASCADE,
    project_id    uuid        NULL REFERENCES project (id) ON DELETE CASCADE,
    state         text        NOT NULL CHECK (state IN ('public', 'internal', 'untrusted', 'blocked')),
    -- Every anchor of the scope that knows the signer (a gate of stage 3 asks for a set, R-SIG-043).
    anchor_ids    bigint[]    NOT NULL DEFAULT '{}',
    block_id      bigint      NULL REFERENCES signature_blocks (id) ON DELETE SET NULL,
    -- The anchor epoch it was computed under (R-SIG-012).
    epoch         bigint      NOT NULL,
    computed_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (signature_id, project_id)
);
CREATE INDEX signature_trust_epoch ON signature_trust (epoch);
CREATE INDEX signature_trust_project ON signature_trust (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX signature_trust_blocked ON signature_trust (computed_at, signature_id) WHERE state = 'blocked' AND project_id IS NULL;
CREATE INDEX signature_trust_anchors ON signature_trust USING gin (anchor_ids) WHERE project_id IS NULL;

-- The TUF targets the task signature-trust-root stored when they were newer than the release's snapshot (R-SIG-017).
CREATE TABLE signature_trust_roots (
    name         text        PRIMARY KEY CHECK (name IN ('trusted_root.json', 'registry.npmjs.org/keys.json')),
    document     bytea       NOT NULL CHECK (octet_length(document) BETWEEN 1 AND 1048576),
    tuf_version  bigint      NOT NULL CHECK (tuf_version >= 1),
    fetched_at   timestamptz NOT NULL DEFAULT now()
);

-- The change area of anchors, blocks and the switch (R-SIG-012, R-SIG-041): every replica refreshes its snapshot when it moves.
INSERT INTO cluster_epoch (area) VALUES ('signature_anchors');
