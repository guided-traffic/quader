-- quader:phase expand
--
-- An image's facts (formats/oci.md R-OCI-039, R-OCI-040, Q-OCI-271): what an image manifest names and what its configuration says, kept
-- with the manifest's row so that the version page reads them without opening a blob.
--
-- blobs is the config's and the layers' descriptors of an image manifest in the manifest's order ({"config": …, "layers": […], "more":
-- n}); NULL for an index, which names manifests. config_digest is the digest of an image's configuration (a config of the media type of
-- an image's configuration, never a Helm chart's or an artifact's), NULL for anything else. config_facts is what the bounded reader of
-- the format read of that configuration (the format's rows.ConfigFacts: the environment, the entrypoint and the command, the ports, the
-- user, the working directory, the labels and the history, or why it could not be read); NULL while the configuration was not read: a
-- remote's manifest is fetched before its config, and the first fetch of the config through the repository reads it.
--
-- The environment and the labels can hold what a builder baked into an image (a token in ENV): the column is read by the version page
-- for a caller who may read the config blob, never by a list or a search (R-OCI-040, docs/security/oci-registry.md). Both are bounded
-- here as the reader bounds them, and the rows stay derived data of the manifest's and the config's bytes.
ALTER TABLE oci_manifests
    ADD COLUMN config_digest bytea CHECK (config_digest IS NULL OR octet_length(config_digest) = 32),
    ADD COLUMN blobs         jsonb CHECK (blobs IS NULL OR octet_length(blobs::text) <= 262144),
    ADD COLUMN config_facts  jsonb CHECK (config_facts IS NULL OR octet_length(config_facts::text) <= 262144);

-- The manifests whose configuration is still to be read: a fetch of a blob asks whether it is one (internal/formats/oci, R-OCI-040).
-- Once read, a row leaves the index, so it stays as small as the images whose config no client fetched yet.
CREATE INDEX oci_manifests_config_unread ON oci_manifests (repo_id, name, config_digest)
    WHERE config_digest IS NOT NULL AND config_facts IS NULL;
