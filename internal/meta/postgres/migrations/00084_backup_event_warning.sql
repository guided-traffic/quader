-- quader:phase expand
--
-- A backup run that ended completed_with_warnings records system.backup.completed with the outcome warning (R-BAK-014,
-- Q-BAK-61): the backup exists, and an administrator should look at what the run left out or could not clean up. The function of
-- 00050 accepted success and failure only; it now accepts warning as well, for system.backup.completed and for no other event of
-- the lane. Everything else is as in 00050: the four events, the actor system:backup, SECURITY DEFINER.
--
-- CREATE OR REPLACE keeps the owner and the grants of the function and resets its other properties, the fixed search_path among
-- them, so that is set again below.

CREATE OR REPLACE FUNCTION backup_record_event(p_action text, p_outcome text, p_replica text, p_reason text, p_details jsonb) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
    IF p_action NOT IN ('system.backup.completed', 'system.backup.failed', 'system.backup.deleted', 'system.backup.target_adopted') THEN
        RAISE EXCEPTION 'backup_record_event writes backup events only, not %', p_action USING ERRCODE = '42501';
    END IF;
    IF p_outcome NOT IN ('success', 'failure') AND NOT (p_outcome = 'warning' AND p_action = 'system.backup.completed') THEN
        RAISE EXCEPTION 'backup_record_event: outcome % is not success or failure, nor warning for system.backup.completed', p_outcome
            USING ERRCODE = '42501';
    END IF;
    INSERT INTO audit_event (id, occurred_at, action, category, outcome, actor_type, actor_name, replica, reason, details)
    VALUES (gen_random_uuid(), now(), p_action, 'system', p_outcome, 'system', 'backup', NULLIF(p_replica, ''), NULLIF(p_reason, ''),
            COALESCE(p_details, '{}'::jsonb));
END $$;

DO $$
BEGIN
    EXECUTE format('ALTER FUNCTION %I.backup_record_event(text, text, text, text, jsonb) SET search_path = %I, pg_temp',
                   current_schema(), current_schema());
END $$;
