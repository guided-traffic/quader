-- quader:phase expand
--
-- A Bastion's configuration belongs to its registration (bastion.md R-BAS-093, the owner's Q-BAS-323; usage-and-limits.md R-LIM-043
-- to R-LIM-046): the switches of its web interface and documentation, the mode and the preset of its download limits, its flood
-- protection per address, its slots, its throughput and its anonymous ceiling are members of the row, the rules of its place are rows
-- of limit_assignment at the new place kind 'bastion', and the two profiles of a Bastion are built-ins. The handshake sends all of it
-- as one member with the registration's revision (Q-BAS-320); a forward never reads it: every replica keeps it in the snapshot of the
-- registrations (internal/bastionmain/snapshot.go), which the change areas bastion_configuration and limits refresh within the bound
-- of R-ARCH-018 (2 s).
--
-- The figures are the measured ones of 2026-11-26 (developer/bastion.md, "What a client's command costs through a Bastion"): the
-- preset Internet for every registration, new and existing, in Enforce, the slots of R-BAS-025, the anonymous ceiling of 100 requests a
-- second with a burst of 2,000 and 36,000 an hour, and a throughput of 200 MB/s with a quarter for anonymous callers (Q-BAS-455). internal/bastionmain holds the same values (presets.go); an
-- integration test compares a new registration with them.

-- The switches and the limits of the registration. The JSON members are the interface's shapes (iface.Flood, iface.Slots,
-- iface.Throughput, iface.Ceiling); the service validates them as the Bastion does (iface.Configuration.Validate) before it writes.
-- limits_preset is the preset an administrator last applied; the registration reads custom once a kind rule or a flood figure
-- differs from that preset's (R-LIM-046), which the service derives and never stores.
ALTER TABLE bastion
    ADD COLUMN gui               boolean NOT NULL DEFAULT false,
    ADD COLUMN docs              boolean NOT NULL DEFAULT false,
    ADD COLUMN limits_mode       text    NOT NULL DEFAULT 'enforce' CHECK (limits_mode IN ('audit', 'enforce')),
    ADD COLUMN limits_preset     text    NOT NULL DEFAULT 'internet' CHECK (limits_preset IN ('internet', 'partnerNetwork', 'internal')),
    ADD COLUMN limits_flood      jsonb   NOT NULL DEFAULT
        '{"anonymous": {"requestsPerSecond": 50, "burst": 1000, "missesPerMinute": 1000},
          "authenticated": {"requestsPerSecond": 200, "burst": 4000, "missesPerMinute": 4000},
          "gui": {"requestsPerSecond": 20, "burst": 100},
          "failedAuth": {"maxFailures": 20, "windowSeconds": 600, "timeoutSeconds": 900},
          "blocking": {"threshold": 3000, "windowSeconds": 60, "durationSeconds": 900, "maxDurationSeconds": 86400},
          "exemptNetworks": []}'
        CHECK (jsonb_typeof(limits_flood) = 'object'),
    ADD COLUMN limits_slots      jsonb   NOT NULL DEFAULT '{"maxConcurrent": 64, "anonymousShare": 0.25, "queueTimeoutMillis": 5000}'
        CHECK (jsonb_typeof(limits_slots) = 'object'),
    ADD COLUMN limits_throughput jsonb   NOT NULL DEFAULT
        '{"bytesPerSecond": 200000000, "anonymousShare": 0.25, "bytesPerHour": {"anonymous": null, "signedIn": null, "catalogue": null}}'
        CHECK (jsonb_typeof(limits_throughput) = 'object'),
    ADD COLUMN limits_ceiling    jsonb   NOT NULL DEFAULT '{"requestsPerSecond": 100, "burst": 2000, "requestsPerHour": 36000}'
        CHECK (jsonb_typeof(limits_ceiling) = 'object');

-- Every member is part of the registration's representation and of the configuration a Bastion applies, so a change of one is a
-- new revision (the ETag, 00013), as a change of the description (00032), the handover (00110) and the networks (00145) is.
CREATE OR REPLACE TRIGGER bastion_revision AFTER UPDATE ON bastion
    FOR EACH ROW WHEN (OLD.description IS DISTINCT FROM NEW.description OR OLD.handover IS DISTINCT FROM NEW.handover
                       OR OLD.egress IS DISTINCT FROM NEW.egress OR OLD.gui IS DISTINCT FROM NEW.gui
                       OR OLD.docs IS DISTINCT FROM NEW.docs OR OLD.limits_mode IS DISTINCT FROM NEW.limits_mode
                       OR OLD.limits_preset IS DISTINCT FROM NEW.limits_preset OR OLD.limits_flood IS DISTINCT FROM NEW.limits_flood
                       OR OLD.limits_slots IS DISTINCT FROM NEW.limits_slots
                       OR OLD.limits_throughput IS DISTINCT FROM NEW.limits_throughput
                       OR OLD.limits_ceiling IS DISTINCT FROM NEW.limits_ceiling)
    EXECUTE FUNCTION principal_revision_bump('principal_id');

-- The place of a Bastion (R-LIM-043, Q-LIM-304): one per registration, deleted with it. An administrator's kind rule exists at a
-- Bastion's place as at the instance, since a preset decides administrators there (R-LIM-046).
ALTER TABLE limit_assignment
    ADD COLUMN bastion_id uuid NULL REFERENCES bastion (principal_id) ON DELETE CASCADE,
    DROP CONSTRAINT limit_assignment_place_kind_check,
    DROP CONSTRAINT limit_assignment_check,
    DROP CONSTRAINT limit_assignment_check2,
    ADD CONSTRAINT limit_assignment_place_kind_check CHECK (place_kind IN ('instance', 'project', 'repository', 'bastion')),
    ADD CONSTRAINT limit_assignment_place_check CHECK (
           (place_kind = 'instance' AND project_id IS NULL AND repository_id IS NULL AND bastion_id IS NULL)
        OR (place_kind = 'project' AND project_id IS NOT NULL AND repository_id IS NULL AND bastion_id IS NULL)
        OR (place_kind = 'repository' AND project_id IS NULL AND repository_id IS NOT NULL AND bastion_id IS NULL)
        OR (place_kind = 'bastion' AND project_id IS NULL AND repository_id IS NULL AND bastion_id IS NOT NULL)),
    ADD CONSTRAINT limit_assignment_administrator_check CHECK (
        place_kind IN ('instance', 'bastion') OR NOT (who_kind = 'every' AND who_ref = 'administrator'));
DROP INDEX limit_assignment_who;
CREATE UNIQUE INDEX limit_assignment_who ON limit_assignment
    (place_kind, project_id, repository_id, bastion_id, who_kind, who_ref) NULLS NOT DISTINCT;
CREATE INDEX limit_assignment_bastion ON limit_assignment (bastion_id) WHERE bastion_id IS NOT NULL;

-- A change of a rule of a Bastion's place is a change of its configuration: the registration's revision rises (Q-BAS-320). A rule
-- of any other place names no registration, and the statement updates nothing.
CREATE TRIGGER limit_assignment_bastion_revision AFTER INSERT OR UPDATE OR DELETE ON limit_assignment
    FOR EACH ROW EXECUTE FUNCTION principal_revision_bump('bastion_id');

-- So is a change of the figures of a profile a rule of a Bastion's place names: the configuration carries each rule's effective
-- figures, so the revision of every registration whose place names the profile rises, and of no other (Q-BAS-320).
CREATE FUNCTION limit_profile_bastion_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE principal SET revision = revision + 1
     WHERE id IN (SELECT DISTINCT a.bastion_id FROM limit_assignment a WHERE a.profile_id = NEW.id AND a.bastion_id IS NOT NULL);
    RETURN NULL;
END
$$;
CREATE TRIGGER limit_profile_bastion_revision AFTER UPDATE ON limit_profile
    FOR EACH ROW WHEN ((OLD.requests_per_second, OLD.requests_burst, OLD.requests_per_hour, OLD.cached_bytes_per_second,
                        OLD.cached_bytes_per_hour, OLD.uncached_bytes_per_second, OLD.uncached_bytes_per_hour,
                        OLD.upstream_requests_per_hour, OLD.parallel_transfers, OLD.bytes_per_day, OLD.refused_per_hour)
                       IS DISTINCT FROM
                       (NEW.requests_per_second, NEW.requests_burst, NEW.requests_per_hour, NEW.cached_bytes_per_second,
                        NEW.cached_bytes_per_hour, NEW.uncached_bytes_per_second, NEW.uncached_bytes_per_hour,
                        NEW.upstream_requests_per_hour, NEW.parallel_transfers, NEW.bytes_per_day, NEW.refused_per_hour))
    EXECUTE FUNCTION limit_profile_bastion_revision();

-- The two profiles of a Bastion (R-LIM-046), built-ins of R-LIM-010: not deleted, not renamed, their figures changeable. Each figure
-- holds, with a third of room, the commands of one address (anonymous) or one subject (signed in) within an hour, at the counts
-- measured on 2026-11-26 and the sizes of R-LIM-042; the uses each figure holds are R-LIM-046's table. Bytes are bytes: MB is 10^6,
-- GB 10^9.
INSERT INTO limit_profile (id, key, name, description, builtin,
                           requests_per_second, requests_burst, requests_per_hour,
                           cached_bytes_per_second, cached_bytes_per_hour,
                           uncached_bytes_per_second, uncached_bytes_per_hour,
                           upstream_requests_per_hour, parallel_transfers, bytes_per_day, refused_per_hour)
VALUES
    (gen_random_uuid(), 'bastion-anonymous', 'Bastion: anonymous', 'Callers without a credential at a Bastion that faces the internet, per address.', true,
     50, 1250, 20000, 100000000, 30000000000, 50000000, 9000000000, 5200, 270, 220000000000, 300),
    (gen_random_uuid(), 'bastion-signed-in', 'Bastion: signed-in', 'Signed-in callers at a Bastion that faces the internet.', true,
     200, 5400, 100000, 250000000, 160000000000, 125000000, 20000000000, 12000, 340, 1300000000000, 1000);

-- The kind rules of the preset Internet for every registration that exists (R-LIM-046); a new registration gets them from the
-- service in its creating transaction. They are built-in at their place: changed, never deleted.
INSERT INTO limit_assignment (id, place_kind, bastion_id, who_kind, who_ref, profile_id, capped, builtin, note)
SELECT gen_random_uuid(), 'bastion', b.principal_id, 'every', r.who, p.id, false, true, r.note
  FROM bastion b
 CROSS JOIN (VALUES ('anonymous', 'bastion-anonymous', 'Callers without a credential.'),
                    ('user', 'bastion-signed-in', 'Signed-in users.'),
                    ('technical_token', 'bastion-signed-in', 'Technical tokens.'),
                    ('ci', 'bastion-signed-in', 'CI projects.'),
                    ('administrator', 'bastion-signed-in', 'Administrators.')) AS r (who, profile, note)
  JOIN limit_profile p ON p.key = r.profile;

-- What a replica reports of itself at its contact (R-BAS-010, Q-BAS-354): the edge figure of its ingress ("off", "rps=<n>",
-- "connections=<n>" or both, empty while it reports none) and whether its public catalogue survives a restart (NULL while it reports
-- none).
ALTER TABLE bastion_contact
    ADD COLUMN edge_limit     text    NOT NULL DEFAULT '' CHECK (char_length(edge_limit) <= 64),
    ADD COLUMN catalogue_kept boolean NULL;

-- The state each replica reports with its batch (R-BAS-016, R-LIM-045): its cache, its throughput, who is limited and the addresses
-- it blocks now. A report replaces its replica's row; the row goes with the replica's contact, and the service deletes a row that was
-- not reported again for an hour and leaves out every block that ended (Q-BAS-443, H-2283). Transient: a restore brings no state
-- back, the next batch writes it.
CREATE TABLE bastion_status (
    bastion_id   uuid        NOT NULL,
    replica      text        NOT NULL,
    reported_at  timestamptz NOT NULL,
    status       jsonb       NOT NULL CHECK (jsonb_typeof(status) = 'object'),
    PRIMARY KEY (bastion_id, replica),
    FOREIGN KEY (bastion_id, replica) REFERENCES bastion_contact (bastion_id, replica) ON DELETE CASCADE
);
CREATE INDEX bastion_status_reported ON bastion_status (reported_at);

-- The change area of the configuration (R-ARCH-018): a change of a member above announces it, and every replica's snapshot reads the
-- registrations again.
INSERT INTO cluster_epoch (area) VALUES ('bastion_configuration');
