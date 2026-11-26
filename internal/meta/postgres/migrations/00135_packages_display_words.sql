-- quader:phase expand
-- quader:no-transaction
--
-- The search field's suggestions find a package by the words of its display name as well (api-and-ui.md R-API-064, Q-UI-1261): a
-- role is the package role/<id> named <owner>.<name>, and a person types its owner. The words are those of 00076, of the lower-cased
-- display name; the index holds only the rows whose display name is more than the name's casing (an Ansible role's today; a NuGet
-- ID differs only in case and is found by its name). It is built beside the table without blocking its writes and writes no row:
-- name_words stays as it is, since a stored generated column that took the display name would rewrite every package.
CREATE INDEX CONCURRENTLY IF NOT EXISTS packages_display_words ON packages
    USING gin ((to_tsvector('simple'::regconfig, regexp_replace(lower(display_name), '[^a-z0-9]+', ' ', 'g'))))
    WHERE lower(display_name) <> lower(name);
