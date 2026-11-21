-- quader:phase expand
--
-- The certificate subject in the usage store (usage-and-limits.md R-LIM-001, R-LIM-027; formats/clamav.md R-CLAMAV-010, Q-CLAMAV-12):
-- a ClamAV installation admitted by its client certificate on an endpoint with auth.client_certificates is charged to the certificate's
-- subject (subject_ref, an RFC 4514 string of at most 255 bytes and its marker) with the certificate's serial as the credential
-- (credential_ref). The daily sums and the limit events keep the kind as text without a check of their own.

ALTER TABLE usage_bucket DROP CONSTRAINT usage_bucket_subject_kind_check;
ALTER TABLE usage_bucket ADD CONSTRAINT usage_bucket_subject_kind_check
    CHECK (subject_kind IN ('user', 'technical_token', 'ci', 'certificate', 'anonymous', 'system', 'overflow'));
