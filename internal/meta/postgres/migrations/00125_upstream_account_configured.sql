-- quader:phase expand
--
-- The account of a well-known repository that the installation's configuration declares (repositories-and-proxy.md R-REPO-077,
-- Q-REPO-484): quader migrate sets the time when it applies an entry of repositories.upstream_accounts and removes the account an
-- administrator had stored, and clears it when the entry is gone. It is the migration's bookkeeping, which keeps a second run from
-- writing again; a replica decides at use from its own configuration. The value of the account is in the mounted files alone.
ALTER TABLE upstream_repository ADD COLUMN account_configured_at timestamptz NULL;
