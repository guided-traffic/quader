-- quader:phase expand
--
-- The server stores no phrase it makes up (api-and-ui.md R-UI-214, Q-UI-1653): the token of an upload from the screen (R-ANS-061,
-- R-NUGET-006) is created_via 'upload' and has no description, and so has the token a Bastion's declaration names (created_via
-- 'configuration', bastion.md R-BAS-127). The screen words both from created_via. Every other token keeps a description of 1 to
-- 1024 characters, as its creator wrote it.
ALTER TABLE api_token
    DROP CONSTRAINT api_token_created_via_check,
    ADD CONSTRAINT api_token_created_via_check CHECK (created_via IN ('ui', 'api', 'npm-web-login', 'device-login', 'admin-cli',
                                                                      'rotation', 'ci-exchange', 'configuration', 'upload')),
    DROP CONSTRAINT api_token_description_check,
    ADD CONSTRAINT api_token_description_check CHECK (char_length(description) <= 1024
                                                      AND (char_length(description) >= 1 OR created_via IN ('upload', 'configuration')));
