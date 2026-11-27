-- quader:phase expand
--
-- The server stores no phrase it makes up (api-and-ui.md R-UI-214, Q-UI-1653): the token npm's web login mints (created_via
-- 'npm-web-login', identity-and-authentication.md R-AUTH-034) and the token a command-line tool's browser login mints
-- (created_via 'device-login', R-AUTH-036) have no description, as the token of an upload from the screen and a Bastion's declared
-- token have none. The screen words them from created_via and the creation time. Every other token keeps a description of 1 to
-- 1024 characters, as its creator wrote it.
ALTER TABLE api_token
    DROP CONSTRAINT api_token_description_check,
    ADD CONSTRAINT api_token_description_check CHECK (char_length(description) <= 1024
                                                      AND (char_length(description) >= 1
                                                           OR created_via IN ('upload', 'configuration', 'npm-web-login',
                                                                              'device-login')));
