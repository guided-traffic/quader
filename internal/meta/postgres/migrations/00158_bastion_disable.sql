-- quader:phase expand
--
-- Disabling a Bastion, soft or hard, told over its connection (bastion.md R-BAS-133): a member of the registration's configuration
-- beside its switches (00147, 00157), NULL while the Bastion works, 'soft' (no new request, what runs finishes) or 'hard' (what runs is
-- cut too), with the time it was set. A disable no longer touches the registration's principal: its tokens stay valid, so the Bastion
-- keeps its line to the main installation, learns the disable at its next handshake and confirms it with its status.
ALTER TABLE bastion
    ADD COLUMN disabled text CHECK (disabled IN ('soft', 'hard')),
    ADD COLUMN disabled_at timestamptz,
    ADD CONSTRAINT bastion_disabled_at CHECK ((disabled IS NULL) = (disabled_at IS NULL));

-- Until now a disable set the principal's status, which refused the registration's tokens. Nothing sets it for a Bastion any more, so
-- a registration disabled that way is disabled hard instead, the narrower of the two, and its principal is active again: without this
-- it would work again unseen after the upgrade.
UPDATE bastion b SET disabled = 'hard', disabled_at = now()
  FROM principal p
 WHERE p.id = b.principal_id AND p.kind = 'bastion' AND p.status = 'disabled';
UPDATE principal SET status = 'active' WHERE kind = 'bastion' AND status = 'disabled';

-- The disable is part of the registration's representation and of the configuration a Bastion applies, so a change of it is a new
-- revision (the ETag, 00013), as a change of every other member is (00147, 00148, 00157).
CREATE OR REPLACE TRIGGER bastion_revision AFTER UPDATE ON bastion
    FOR EACH ROW WHEN (OLD.description IS DISTINCT FROM NEW.description OR OLD.handover IS DISTINCT FROM NEW.handover
                       OR OLD.egress IS DISTINCT FROM NEW.egress OR OLD.gui IS DISTINCT FROM NEW.gui
                       OR OLD.docs IS DISTINCT FROM NEW.docs OR OLD.hide_repositories IS DISTINCT FROM NEW.hide_repositories
                       OR OLD.limits_mode IS DISTINCT FROM NEW.limits_mode
                       OR OLD.limits_preset IS DISTINCT FROM NEW.limits_preset OR OLD.limits_flood IS DISTINCT FROM NEW.limits_flood
                       OR OLD.limits_slots IS DISTINCT FROM NEW.limits_slots
                       OR OLD.limits_throughput IS DISTINCT FROM NEW.limits_throughput
                       OR OLD.limits_ceiling IS DISTINCT FROM NEW.limits_ceiling
                       OR OLD.declared_at IS DISTINCT FROM NEW.declared_at
                       OR OLD.disabled IS DISTINCT FROM NEW.disabled OR OLD.disabled_at IS DISTINCT FROM NEW.disabled_at)
    EXECUTE FUNCTION principal_revision_bump('principal_id');
