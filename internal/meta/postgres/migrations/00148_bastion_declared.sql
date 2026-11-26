-- quader:phase expand
--
-- A Bastion registration the installation's configuration declares (bastion.md R-BAS-127, Q-BAS-465 to Q-BAS-469): bastions.declared
-- names the registration, its description, the networks its Bastion connects from and the file of its token, and quader migrate
-- applies it on every run, so the registration and the token the Bastion holds exist again after a fresh database.
--
-- declared_at is the mark of a registration the configuration declares: set by the run that first applies its entry, cleared by the
-- run whose configuration no longer declares it. While it is set the API refuses to change the declared members (description, egress)
-- and to delete the registration (409 managed_by_config). declared_token is the token the configuration declares: the hash of the
-- value in the Secret, stored as every token is (R-AUTH-023), never the value. A run that finds another value in the file revokes it
-- (reason rotated) and points at the new row; the retention task may delete a revoked row, which clears the pointer.

ALTER TABLE bastion
    ADD COLUMN declared_at    timestamptz NULL,
    ADD COLUMN declared_token uuid        NULL REFERENCES api_token (id) ON DELETE SET NULL,
    ADD CONSTRAINT bastion_declared_token_check CHECK (declared_token IS NULL OR declared_at IS NOT NULL);

-- A declared token does not expire: its lifetime is its declaration's (Q-BAS-466). It ends when the Secret holds another value or the
-- configuration no longer declares the registration. created_via 'configuration' marks a row quader migrate stored or took over from a
-- token issued on the screen (the audit trail keeps how that one was issued); only such a Bastion token may lack an expiry, and the
-- authenticator accepts it only while it is its registration's declared_token. Every other Bastion token still expires (R-BAS-004).
ALTER TABLE api_token
    DROP CONSTRAINT api_token_check,
    ADD CONSTRAINT api_token_check CHECK (type = 'sat' OR expires_at IS NOT NULL OR (type = 'bst' AND created_via = 'configuration')),
    DROP CONSTRAINT api_token_created_via_check,
    ADD CONSTRAINT api_token_created_via_check CHECK (created_via IN ('ui', 'api', 'npm-web-login', 'device-login', 'admin-cli',
                                                                      'rotation', 'ci-exchange', 'configuration'));

-- The mark is part of the registration's representation (the card shows it, the API refuses on it), so a change of it is a new
-- revision (the ETag, 00013), as a change of every other member is (00147).
CREATE OR REPLACE TRIGGER bastion_revision AFTER UPDATE ON bastion
    FOR EACH ROW WHEN (OLD.description IS DISTINCT FROM NEW.description OR OLD.handover IS DISTINCT FROM NEW.handover
                       OR OLD.egress IS DISTINCT FROM NEW.egress OR OLD.gui IS DISTINCT FROM NEW.gui
                       OR OLD.docs IS DISTINCT FROM NEW.docs OR OLD.limits_mode IS DISTINCT FROM NEW.limits_mode
                       OR OLD.limits_preset IS DISTINCT FROM NEW.limits_preset OR OLD.limits_flood IS DISTINCT FROM NEW.limits_flood
                       OR OLD.limits_slots IS DISTINCT FROM NEW.limits_slots
                       OR OLD.limits_throughput IS DISTINCT FROM NEW.limits_throughput
                       OR OLD.limits_ceiling IS DISTINCT FROM NEW.limits_ceiling
                       OR OLD.declared_at IS DISTINCT FROM NEW.declared_at)
    EXECUTE FUNCTION principal_revision_bump('principal_id');
