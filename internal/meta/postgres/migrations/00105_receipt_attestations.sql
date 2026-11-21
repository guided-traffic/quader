-- quader:phase expand
--
-- Signatures on artifacts, stage 2, the receipt attestations (signatures.md R-SIG-051 to R-SIG-054, R-SIG-059; Q-SIG-152, Q-SIG-183,
-- Q-SIG-184): an attestation of Quader's own is a signature of stage 1's store (the role attestation, the scheme sigstore-bundle, the
-- bundle in signature_objects.raw, signatures.identity_id set) attached with the source issued (00103) in its repository to the item it
-- attests, so it goes with that item. signing_attestations is the job signing's index of what it issued: one row per attested item (an item
-- belongs to one repository), the identity that made it, the malware state and the blobs it names (so a verdict that changes finds the
-- attestations to make again), and where it is served: as an image's referrer, whose manifest's digest it keeps, or through the API.

CREATE TABLE signing_attestations (
    item_id        bigint      PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    -- The item's repository, no key of its own: the row goes with its item, and a purge of a retired repository takes the items.
    repo_id        bigint      NOT NULL,
    signature_id   bigint      NOT NULL REFERENCES signatures (id) ON DELETE CASCADE,
    identity_id    bigint      NOT NULL REFERENCES signing_identities (id),
    -- The malware state the statement names (R-MAL-002's states, not_scanned, pending) and whether a release passed the blob: a change of
    -- either makes the attestation again (R-SIG-053).
    malware_state  text        NOT NULL CHECK (malware_state IN ('not_scanned', 'pending', 'clean', 'infected', 'unscannable', 'failed')),
    released       boolean     NOT NULL DEFAULT false,
    -- The blobs whose verdicts the statement names: the item's own, or an image's config and layers (formats.AttestationSubject.Scanned).
    scanned        bytea[]     NOT NULL DEFAULT '{}' CHECK (cardinality(scanned) <= 256),
    -- referrer: an image's attestation stands among its referrers as the manifest of this digest (R-SIG-054); api: served by the API.
    served_as      text        NOT NULL CHECK (served_as IN ('referrer', 'api')),
    referrer       bytea       NULL CHECK (octet_length(referrer) = 32),
    -- Why an image's attestation is served through the API: hides_signature (an older cosign signature tag stands for the digest) or
    -- no_referrers_api (a remote or a virtual repository, Q-SIG-183).
    api_reason     text        NULL CHECK (api_reason IN ('hides_signature', 'no_referrers_api')),
    issued_at      timestamptz NOT NULL,
    CHECK ((served_as = 'referrer') = (referrer IS NOT NULL))
);
CREATE INDEX signing_attestations_scanned ON signing_attestations USING gin (scanned);
CREATE INDEX signing_attestations_identity ON signing_attestations (identity_id);
CREATE INDEX signing_attestations_signature ON signing_attestations (signature_id);
-- The referrers the job wrote, which stage 1's discovery and the job's own leave out item by item.
CREATE INDEX signing_attestations_referrer ON signing_attestations (repo_id, referrer) WHERE referrer IS NOT NULL;
