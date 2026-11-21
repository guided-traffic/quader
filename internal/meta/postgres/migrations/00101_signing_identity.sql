-- quader:phase expand
--
-- Signatures on artifacts, stage 2, the signing identity and the frame of the job signing (signatures.md R-SIG-045 to R-SIG-049,
-- R-SIG-057, R-SIG-059; Q-SIG-145 to Q-SIG-152): the installation's identities with their states, the work of the job signing, the one
-- row of its state, the column that names the identity a signature of Quader's own was made with, and the change area every replica's
-- snapshot of the identities follows. The store internal/signing is the one door to the tables; the job internal/signing/job runs the
-- work.

-- One signing identity per installation (Q-SIG-13), over time a row per key and certificate: pending (an uploaded key with its request,
-- no certificate yet), announced (listed before it signs, R-SIG-048), current (the one that signs), previous (signed until expires_at),
-- retired (withdrawn by an administrator). The private key of the Helm values is never stored (R-SIG-046); an uploaded one is sealed
-- by the key ring under the row's id (R-SIG-047, R-ARCH-021) and dropped when the identity is retired (Q-SIG-149).
CREATE TABLE signing_identities (
    id              bigserial   PRIMARY KEY,
    source          text        NOT NULL CHECK (source IN ('configuration', 'upload')),
    state           text        NOT NULL CHECK (state IN ('pending', 'announced', 'current', 'previous', 'retired')),
    -- DER SubjectPublicKeyInfo of the ECDSA P-256 key, and its SHA-256 (a renewal with the same key keeps it).
    public_key      bytea       NOT NULL CHECK (octet_length(public_key) BETWEEN 1 AND 1024),
    key_sha256      bytea       NOT NULL CHECK (octet_length(key_sha256) = 32),
    -- npm's keyid of the key: SHA256: and the unpadded base64 of the SHA-256 of its SSH wire form (R-SIG-050).
    npm_keyid       text        NOT NULL CHECK (npm_keyid ~ '^SHA256:[A-Za-z0-9+/]{43}$'),
    -- An upload's private key under the key ring; NULL for the configuration and for a retired identity.
    sealed_key      text        NULL CHECK (length(sealed_key) <= 4096),
    -- pending: the PEM certificate request handed out (R-SIG-047).
    csr             text        NULL CHECK (length(csr) <= 8192),
    -- The DER leaf; NULL while pending. Its SHA-256 is the identity's fingerprint.
    certificate     bytea       NULL CHECK (octet_length(certificate) <= 16384),
    cert_sha256     bytea       NULL CHECK (octet_length(cert_sha256) = 32),
    -- DER intermediates, then the root (R-SIG-045: at most 8 certificates of 16 KiB with the leaf).
    chain           bytea[]     NOT NULL DEFAULT '{}' CHECK (cardinality(chain) <= 7),
    subject         text        NULL CHECK (length(subject) <= 1024),
    issuer          text        NULL CHECK (length(issuer) <= 1024),
    -- The alternative names: the URIs first, then the e-mail addresses.
    names           text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(names) <= 64),
    not_before      timestamptz NULL,
    not_after       timestamptz NULL,
    announced_at    timestamptz NULL,
    current_at      timestamptz NULL,
    -- previous: the moment it stopped signing (R-SIG-048); npm's client compares a version's time with it.
    expires_at      timestamptz NULL,
    retired_at      timestamptz NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    created_by      uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    CHECK ((source = 'upload' AND state <> 'retired') = (sealed_key IS NOT NULL)),
    CHECK ((state = 'pending') = (certificate IS NULL)),
    CHECK ((certificate IS NULL) = (cert_sha256 IS NULL)),
    CHECK (state <> 'pending' OR source = 'upload'),
    CHECK (state <> 'previous' OR expires_at IS NOT NULL),
    CHECK (state <> 'retired' OR retired_at IS NOT NULL),
    UNIQUE (cert_sha256)
);
CREATE UNIQUE INDEX signing_identities_pending ON signing_identities ((true)) WHERE state = 'pending';
CREATE UNIQUE INDEX signing_identities_announced ON signing_identities ((true)) WHERE state = 'announced';
CREATE UNIQUE INDEX signing_identities_current ON signing_identities ((true)) WHERE state = 'current';

-- The work of the job signing (R-SIG-049): a row per item and kind, written in the commit's transaction by the packages that sign (npm's
-- registry signature, the receipt attestation) and by the job's own discovery. code is the reason of the last attempt that could not run
-- (Q-SIG-150).
CREATE TABLE signing_work (
    kind        text        NOT NULL CHECK (kind IN ('npm', 'attestation')),
    item_id     bigint      NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    reason      text        NOT NULL CHECK (reason IN ('upload', 'fill', 'checks', 'malware', 'rotation', 'retired', 'discovery')),
    priority    smallint    NOT NULL CHECK (priority BETWEEN 0 AND 3),
    not_before  timestamptz NOT NULL DEFAULT now(),
    attempts    integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    failed      boolean     NOT NULL DEFAULT false,
    code        text        NULL CHECK (length(code) <= 64),
    PRIMARY KEY (kind, item_id)
);
CREATE INDEX signing_work_due ON signing_work (priority, not_before) WHERE NOT failed;

-- The one row of the signing's state (Q-SIG-148): the last refused pair of the files (R-SIG-046, recorded once per content), the daily
-- retry of failed work and the hourly report per kind and code (R-SIG-049), and discovery's cursor per kind over items in id order.
CREATE TABLE signing_settings (
    id                   boolean     PRIMARY KEY DEFAULT true CHECK (id),
    refused_sha256       bytea       NULL CHECK (octet_length(refused_sha256) = 32),
    refused_reason       text        NULL CHECK (length(refused_reason) <= 1000),
    refused_fingerprint  text        NULL CHECK (length(refused_fingerprint) <= 64),
    refused_at           timestamptz NULL,
    failed_retried_at    timestamptz NULL,
    -- "<kind>/<code>": the RFC 3339 time signing.failed was last recorded for it.
    failed_reported      jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (octet_length(failed_reported::text) <= 65536),
    -- "<kind>": the last item id discovery looked at.
    discovery            jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (octet_length(discovery::text) <= 4096),
    CHECK ((refused_sha256 IS NULL) = (refused_reason IS NULL))
);
INSERT INTO signing_settings (id) VALUES (true);

-- A signature of Quader's own names the identity that made it (R-SIG-054): the packages that sign write it; a retirement counts by it
-- (R-SIG-057). The rows of stage 1 have none.
ALTER TABLE signatures ADD COLUMN identity_id bigint NULL REFERENCES signing_identities (id);
CREATE INDEX signatures_issued ON signatures (identity_id) WHERE identity_id IS NOT NULL;

-- The change area of the identities (R-SIG-059): every replica refreshes its snapshot when it moves.
INSERT INTO cluster_epoch (area) VALUES ('signing_identities');
