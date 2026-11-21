-- quader:phase expand
--
-- Two columns of 00098_signature_trust get names the revision-column rule does not claim (architecture.md R-ARCH-009, R-BAK-022:
-- every bigint column named rev_seq, revision, epoch or *_version is a revision column that a restore raises by 2^32). Neither is one
-- (signatures.md R-SIG-008, R-SIG-012, R-SIG-017):
--
--   - signature_trust.epoch is a copy of signature_settings.anchor_epoch, the epoch a trust row was computed under. Raising the copy and
--     not the counter it copies would make a row look current for good; the table is transient, and the counter stays as it was
--     restored. It takes the name of the counter: anchor_epoch.
--   - signature_trust_roots.tuf_version is the version number of the TUF snapshot a stored root was taken from: a number of the update
--     repository, which the daily task compares with the version of the snapshot it fetched. Raised by 2^32, the stored root would look
--     newer than every snapshot to come and would never be refreshed again. It becomes tuf_version_number.

ALTER TABLE signature_trust RENAME COLUMN epoch TO anchor_epoch;
ALTER INDEX signature_trust_epoch RENAME TO signature_trust_anchor_epoch;

ALTER TABLE signature_trust_roots RENAME COLUMN tuf_version TO tuf_version_number;
ALTER TABLE signature_trust_roots RENAME CONSTRAINT signature_trust_roots_tuf_version_check TO signature_trust_roots_tuf_version_number_check;
