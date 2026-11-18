-- The outcome warning of an audit event (Q-AUTH-402): an action that happened and that an administrator should look at, such as
-- a sign-in whose group claim went missing (R-AUTH-008). It is not a failure, so it is a value of its own beside success, denied
-- and failure. The check of the partitioned table applies to every partition.

ALTER TABLE audit_event DROP CONSTRAINT audit_event_outcome_check;
ALTER TABLE audit_event ADD CONSTRAINT audit_event_outcome_check
    CHECK (outcome IN ('success', 'denied', 'failure', 'warning'));
