-- quader:phase expand
--
-- A second reason the builder of the public catalogue stops for a registration (bastion.md R-BAS-104, H-2330): while the main
-- installation's default endpoint admits no request without a credential (auth.anonymous false), an anonymous visitor reads nothing
-- there, so the builder empties every registration's catalogue and records anonymous_requests_refused for its card.
ALTER TABLE bastion_catalogue_state
    DROP CONSTRAINT bastion_catalogue_state_stopped_check,
    ADD CONSTRAINT bastion_catalogue_state_stopped_check
        CHECK (stopped IN ('', 'bastion_base_url_unknown', 'anonymous_requests_refused'));
