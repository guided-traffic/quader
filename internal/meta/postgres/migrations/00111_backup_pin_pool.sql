-- quader:phase expand
--
-- The pool object a backup's pin names (backup-and-restore.md R-BAK-032, Q-BAK-75): '-' for the plain pool object, the identifier of
-- the storage key that wraps an encrypted connection's copy as stored. NULL until the copy of a new digest is confirmed; a re-pooled
-- pin starts with the pool object it inherits, which its confirmed copy replaces (Q-BAK-80). The pins stay until the run's final blob
-- list is written from them.
ALTER TABLE backup_blob_pin
    ADD COLUMN pool text CHECK (pool IS NULL OR pool = '-' OR pool ~ '^[a-z0-9-]{3,64}$');
