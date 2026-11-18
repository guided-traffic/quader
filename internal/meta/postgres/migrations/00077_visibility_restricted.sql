-- quader:phase expand
--
-- The restricted visibility levels of the installation (R-AUTHZ-037, Q-AUTHZ-289): the levels that only an administrator may
-- choose for a project or a repository, beside the default level of 00073. The set holds only the three levels and never the
-- default level (Q-AUTHZ-284); the database refuses every other state, so that a write that is not the settings API (a restore, a
-- direct UPDATE) cannot leave a default that nobody but an administrator could choose. The service stores the set without
-- duplicates, in the order private, internal, public.

ALTER TABLE visibility_settings
    ADD COLUMN restricted_levels text[] NOT NULL DEFAULT '{}'
        CONSTRAINT visibility_settings_restricted_levels
        CHECK (restricted_levels <@ ARRAY['private', 'internal', 'public']::text[]
               AND NOT (default_level = ANY (restricted_levels)));
