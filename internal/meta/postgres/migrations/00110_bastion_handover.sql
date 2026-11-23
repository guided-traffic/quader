-- quader:phase expand
--
-- The switch of the handover per blob to a Bastion (bastion.md R-BAS-071, Q-BAS-232): a member of the registration, off by
-- default, so that a new registration and every registration of an existing database hand nothing over. A forward never reads
-- the column: every replica reads the registrations whose handover is on from a snapshot that the change area bastion_handover
-- refreshes (internal/bastionmain/handover.go), so a download costs no statement for it (R-ARCH-027).

ALTER TABLE bastion ADD COLUMN handover boolean NOT NULL DEFAULT false;

-- The switch is a member of the registration's representation, so a change of it is a new revision (the ETag, 00013), as a
-- change of the description is (00032).
CREATE OR REPLACE TRIGGER bastion_revision AFTER UPDATE ON bastion
    FOR EACH ROW WHEN (OLD.description IS DISTINCT FROM NEW.description OR OLD.handover IS DISTINCT FROM NEW.handover)
    EXECUTE FUNCTION principal_revision_bump('principal_id');

INSERT INTO cluster_epoch (area) VALUES ('bastion_handover');
