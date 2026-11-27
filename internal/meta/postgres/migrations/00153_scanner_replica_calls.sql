-- quader:phase expand
--
-- A malware scanner's report names the calls it has in flight (operations-and-observability.md R-OPS-048, Q-OPS-460; malware-scanning.md
-- R-MAL-041): the digest of each blob a slot holds and since when, so that the settings page says what each replica works on from the
-- replica's own report, the same source as its slots in use (Q-MAL-110). Only the kind clamav reports calls; at most one per slot. The
-- scanner states them of itself (H-1161): shown, nothing decides on them.
ALTER TABLE scanner_replica
    ADD COLUMN calls jsonb NULL
        CHECK (calls IS NULL
               OR (kind = 'clamav' AND jsonb_typeof(calls) = 'array' AND jsonb_array_length(calls) <= slots_total));
