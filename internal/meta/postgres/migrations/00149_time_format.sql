-- quader:phase expand
--
-- The form of a time is the reader's choice (api-and-ui.md R-UI-250, R-API-021): user_preferences.time_format is one of the forms the
-- UI offers, or NULL for the installation's default (R-UI-251, kept in system_state under the key time_format, which needs no column).
-- No row changes: NULL is what every stored row means today, so no revision moves.

ALTER TABLE user_preferences
    ADD COLUMN time_format text CHECK (time_format IN ('dmy-weekday', 'dmy', 'iso', 'dmy-slash', 'mdy', 'locale'));
