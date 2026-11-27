-- quader:phase expand
--
-- The switch Hide repositories of a Bastion's web interface (bastion.md R-BAS-131): while it is on, the Bastion's pages name no
-- repository a visitor reads and the public catalogue's builder renders no repository's index and no remote's upstream. A member of
-- the registration's configuration beside gui and docs (00147), off for every registration, new and existing: off shows the
-- repositories, the default of the owner's W-40.
ALTER TABLE bastion
    ADD COLUMN hide_repositories boolean NOT NULL DEFAULT false;

-- The switch is part of the registration's representation and of the configuration a Bastion applies, so a change of it is a new
-- revision (the ETag, 00013), as a change of every other member is (00147, 00148).
CREATE OR REPLACE TRIGGER bastion_revision AFTER UPDATE ON bastion
    FOR EACH ROW WHEN (OLD.description IS DISTINCT FROM NEW.description OR OLD.handover IS DISTINCT FROM NEW.handover
                       OR OLD.egress IS DISTINCT FROM NEW.egress OR OLD.gui IS DISTINCT FROM NEW.gui
                       OR OLD.docs IS DISTINCT FROM NEW.docs OR OLD.hide_repositories IS DISTINCT FROM NEW.hide_repositories
                       OR OLD.limits_mode IS DISTINCT FROM NEW.limits_mode
                       OR OLD.limits_preset IS DISTINCT FROM NEW.limits_preset OR OLD.limits_flood IS DISTINCT FROM NEW.limits_flood
                       OR OLD.limits_slots IS DISTINCT FROM NEW.limits_slots
                       OR OLD.limits_throughput IS DISTINCT FROM NEW.limits_throughput
                       OR OLD.limits_ceiling IS DISTINCT FROM NEW.limits_ceiling
                       OR OLD.declared_at IS DISTINCT FROM NEW.declared_at)
    EXECUTE FUNCTION principal_revision_bump('principal_id');
