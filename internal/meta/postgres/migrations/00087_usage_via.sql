-- quader:phase expand
--
-- The usage of a well-known repository through a team's virtual repository (usage-and-limits.md R-LIM-027, R-LIM-029,
-- Q-LIM-63): a row of usage_bucket whose serving repository is a repository of the built-in project upstream and whose request
-- addressed a virtual repository of another project names that virtual repository in via_repository_id, so the Owner of its
-- project keeps seeing what the team's builds took from the public registries, and the serving repository's own rows stay its
-- total. NULL everywhere else. The column is part of the row's key: one serving repository reached through two virtual
-- repositories writes two rows. The daily sums keep the serving repository only.

ALTER TABLE usage_bucket ADD COLUMN via_repository_id bigint NULL REFERENCES repositories (id) ON DELETE CASCADE;

DROP INDEX usage_bucket_key;
CREATE UNIQUE INDEX usage_bucket_key ON usage_bucket
    (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id, via_repository_id) NULLS NOT DISTINCT;
-- The reads of a project's scope look the rows up by the virtual repositories of the project as well (R-LIM-029).
CREATE INDEX usage_bucket_via ON usage_bucket (via_repository_id, granularity, bucket_start) WHERE via_repository_id IS NOT NULL;
