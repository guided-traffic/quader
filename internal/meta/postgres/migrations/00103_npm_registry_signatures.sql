-- quader:phase expand
--
-- Signatures on artifacts, stage 2, npm's registry signatures of local versions (signatures.md R-SIG-048 to R-SIG-050, R-SIG-054;
-- Q-SIG-152, Q-SIG-156 to Q-SIG-163). Quader's own npm signature is a signature of the store of stage 1 with signatures.identity_id set
-- (00101), attached to the version's tarball with the new source issued; the package documents of a local repository render the
-- signatures whose identity is listed. The one row of the signing's state learns which identities' signatures the documents served at
-- the job's last look, so a removal, a retirement or the first current identity after none raises the revision of every package whose
-- document changes (R-SIG-048).

-- An attachment of a signature Quader made itself (R-SIG-054). The commit hook and the fill of stage 1 replace only the attachments
-- they make; an issued one is written and removed by the job signing alone.
ALTER TABLE signature_attachments DROP CONSTRAINT signature_attachments_source_check;
ALTER TABLE signature_attachments ADD CONSTRAINT signature_attachments_source_check
    CHECK (source IN ('referrer', 'referrers_tag', 'cosign_tag', 'index_entry', 'document', 'embedded', 'sibling', 'release', 'upload',
                      'issued'));

-- "<kind>": the ids of the identities whose signatures the documents served when the job last compared (Q-SIG-160): every current and
-- previous identity while one is current, none otherwise.
ALTER TABLE signing_settings ADD COLUMN served jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (octet_length(served::text) <= 65536);
