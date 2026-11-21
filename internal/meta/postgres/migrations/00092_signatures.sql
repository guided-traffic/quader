-- quader:phase expand
--
-- Signatures on artifacts, stage 1, the store (signatures.md R-SIG-003, R-SIG-004, R-SIG-010; Q-SIG-21, Q-SIG-22, Q-SIG-40, Q-SIG-47 to
-- Q-SIG-52): the original of a signature once per its own SHA-256, one row per signature and subject for the integrity check, the
-- attachments that say where a signature reaches its subject in a repository, and the origin of an upload. The commit hook of the item
-- store and the fill of a remote's document write them in their own transaction (internal/signatures/store); the job of R-SIG-011 reads
-- and checks them. No foreign key to blobs: a foreign key there would hold a blob against the collection (storage.md R-STOR-024).

CREATE TABLE signature_objects (
    sha256        bytea       PRIMARY KEY CHECK (octet_length(sha256) = 32),
    scheme        text        NOT NULL CHECK (scheme IN ('sigstore-bundle', 'cosign-legacy', 'notation-jws', 'notation-cose', 'nuget-cms',
                                                         'openpgp', 'npm-registry', 'public-key', 'unknown')),
    media_type    text        NULL CHECK (length(media_type) <= 255),
    -- The bytes, when no stored file keeps them: an entry of a document (Q-SIG-22).
    raw           bytea       NULL CHECK (octet_length(raw) <= 262144),
    -- The blob that holds the original when a stored file does, and where inside it: a zip entry, a JSON pointer; "" or NULL for the
    -- whole blob. An OCI signature's object is the manifest that carries it (Q-SIG-48).
    item_blob     bytea       NULL CHECK (octet_length(item_blob) = 32),
    locator       text        NULL CHECK (length(locator) <= 1024),
    -- The size of the object's bytes. A stored file larger than signatures.max_object_size is recorded with its size and read as
    -- malformed (limits_exceeded), so the size has no upper bound here (Q-SIG-49); raw has one.
    size          bigint      NOT NULL CHECK (size >= 1),
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((raw IS NULL) <> (item_blob IS NULL))
);

CREATE TABLE signatures (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    object_sha256   bytea       NOT NULL REFERENCES signature_objects (sha256) ON DELETE CASCADE,
    -- The signature within the object: an OpenPGP packet, a countersignature, a layer of an older cosign signature.
    part            smallint    NOT NULL DEFAULT 0 CHECK (part >= 0),
    subject_sha256  bytea       NOT NULL CHECK (octet_length(subject_sha256) = 32),
    -- npm: <name>@<version>, part of the signed message.
    subject_name    text        NULL CHECK (length(subject_name) <= 512),
    role            text        NOT NULL CHECK (role IN ('publisher', 'provenance', 'registry', 'attestation')),
    predicate_type  text        NULL CHECK (length(predicate_type) <= 512),
    -- What a keyless scheme or NuGet hashes, when it is not the subject's bytes as stored: the blob that holds it (NULL: the subject's,
    -- or a message the row holds by its digest) and where inside it ("unsigned", "cleartext", "entry:<name>"; Q-SIG-48).
    content_sha256  bytea       NULL CHECK (octet_length(content_sha256) = 32),
    content_locator text        NULL CHECK (length(content_locator) <= 1024),
    state           text        NOT NULL DEFAULT 'pending'
                                CHECK (state IN ('pending', 'valid', 'invalid', 'no_key', 'unsupported', 'malformed', 'failed')),
    reason          text        NULL CHECK (length(reason) <= 64),
    signer          jsonb       NULL CHECK (octet_length(signer::text) <= 65536),
    key_ids         text[]      NOT NULL DEFAULT '{}',
    signed_digest   bytea       NULL CHECK (octet_length(signed_digest) <= 64),
    digest_alg      text        NULL CHECK (length(digest_alg) <= 32),
    claimed_at      timestamptz NULL,
    proven_at       timestamptz NULL,
    time_proof      text        NULL CHECK (time_proof IN ('timestamp', 'log')),
    received_at     timestamptz NOT NULL DEFAULT now(),
    checked_at      timestamptz NULL,
    due             boolean     NOT NULL DEFAULT true,
    priority        smallint    NOT NULL DEFAULT 1 CHECK (priority BETWEEN 0 AND 3),
    attempts        integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    not_before      timestamptz NOT NULL DEFAULT now(),
    -- NULLS NOT DISTINCT: a signature without a subject name is one row per object, part and subject, so the commit hook's
    -- ON CONFLICT DO NOTHING finds it (R-SIG-003).
    UNIQUE NULLS NOT DISTINCT (object_sha256, part, subject_sha256, subject_name)
);
CREATE INDEX signatures_subject ON signatures (subject_sha256);
CREATE INDEX signatures_due     ON signatures (priority, not_before) WHERE due;
CREATE INDEX signatures_keys    ON signatures USING gin (key_ids);
CREATE INDEX signatures_invalid ON signatures (checked_at) WHERE state = 'invalid';

CREATE TABLE signature_attachments (
    repo_id       bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    signature_id  bigint      NOT NULL REFERENCES signatures (id) ON DELETE CASCADE,
    -- The item path of the carrier, or path#pointer in a document.
    location      text        NOT NULL CHECK (length(location) BETWEEN 1 AND 2048),
    -- The item that carries the original, when one does: the attachment follows it (R-SIG-004). A remote's document is no item.
    item_id       bigint      NULL REFERENCES items (id) ON DELETE CASCADE,
    source        text        NOT NULL CHECK (source IN ('referrer', 'referrers_tag', 'cosign_tag', 'index_entry', 'document', 'embedded',
                                                         'sibling', 'release', 'upload')),
    -- The package or image name the attachment is under: what the format's Classify says of the location.
    name          text        NULL CHECK (length(name) <= 255),
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, signature_id, location)
);
CREATE INDEX signature_attachments_item ON signature_attachments (item_id) WHERE item_id IS NOT NULL;
CREATE INDEX signature_attachments_repo_name ON signature_attachments (repo_id, name);
CREATE INDEX signature_attachments_signature ON signature_attachments (signature_id);
CREATE INDEX signature_attachments_location ON signature_attachments (repo_id, location);

CREATE TABLE artifact_origins (
    item_id         bigint      PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    -- The kind of the principal behind the upload as the audit records it: user, service_account, ci, anonymous, ...
    principal_kind  text        NOT NULL CHECK (length(principal_kind) BETWEEN 1 AND 32),
    principal_name  text        NOT NULL CHECK (length(principal_name) <= 255),
    -- The credential's ID, never a secret.
    token_id        uuid        NULL,
    -- A CI upload: the issuer's name and the allowlisted claims of the issuance record (gitlab-ci-oidc.md R-CI-020), never user_email.
    ci              jsonb       NULL CHECK (octet_length(ci::text) <= 8192),
    uploaded_at     timestamptz NOT NULL DEFAULT now()
);
