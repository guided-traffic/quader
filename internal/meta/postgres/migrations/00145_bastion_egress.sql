-- quader:phase expand
--
-- The addresses a Bastion connects from belong to its registration (bastion.md R-BAS-017, R-BAS-093, the owner's Q-BAS-334;
-- api-and-ui.md R-UI-243): the main installation believes the client address and the host a Bastion forwards, and the client
-- address of its check, only from a request that comes from one of its own registration's networks. The list replaces the
-- configuration key bastions.trusted_proxies, which served every registration alike; nothing of the key is carried over (pre-alpha,
-- Q-ARCH-340). Empty for a new registration and for every registration of an existing database: then nothing a Bastion reports about
-- its callers counts, and every caller through it counts as the Bastion's own address (Q-BAS-430).
--
-- A forward never reads the column: every replica reads the registrations' lists into the snapshot of the registrations
-- (internal/bastionmain/snapshot.go), which the change area bastion_egress refreshes within the bound of R-ARCH-018 (2 s), so a
-- request costs no statement for it (R-ARCH-027). At most 32 networks, none of them /0 (Q-BAS-431); the service refuses an address
-- with host bits beyond its prefix before the cast here would.

ALTER TABLE bastion ADD COLUMN egress cidr[] NOT NULL DEFAULT '{}' CHECK (cardinality(egress) <= 32);

-- The list is a member of the registration's representation, so a change of it is a new revision (the ETag, 00013), as a change of
-- the description (00032) and of the handover (00110) is.
CREATE OR REPLACE TRIGGER bastion_revision AFTER UPDATE ON bastion
    FOR EACH ROW WHEN (OLD.description IS DISTINCT FROM NEW.description OR OLD.handover IS DISTINCT FROM NEW.handover
                       OR OLD.egress IS DISTINCT FROM NEW.egress)
    EXECUTE FUNCTION principal_revision_bump('principal_id');

INSERT INTO cluster_epoch (area) VALUES ('bastion_egress');
