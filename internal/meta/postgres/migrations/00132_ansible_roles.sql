-- quader:phase expand
--
-- The facts of the roles a remote Ansible Galaxy repository read from its upstream (formats/ansible.md R-ANS-056, Q-ANS-110): a row per
-- role of the upstream's v1 API, keyed by the upstream's numeric id, written in the transaction that stores a role lookup. The README is
-- the upstream's rendered HTML of the role's content document (`v1/roles/<id>/content/`), written by the fill of that document, which
-- the job ansible-docs-fill fetches; it is untrusted and sanitized on every serving (R-ANS-039). The package of a role keeps its name
-- `role/<id>` (Q-ANS-36): the row is what the screens show of it, never what a pattern, a rule or a grant decides on.
--
-- The rows are derived from upstream documents and are no source of truth: they are not exported (R-BAK-004), go with the cache of a
-- remote (R-BAK-008) and are written again by the next lookup. readme_state is none (never asked), queued, available, absent (the
-- upstream has none) or unavailable (readme_reason says why); readme_commit is the commit the README was fetched for, so a lookup that
-- names another commit asks again.
CREATE TABLE ansible_roles (
    repo_id       bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    role_id       bigint      NOT NULL CHECK (role_id BETWEEN 1 AND 999999999999),
    owner         text        COLLATE "C" NOT NULL CHECK (char_length(owner) BETWEEN 1 AND 64),
    name          text        COLLATE "C" NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    description   text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 4096),
    github_user   text        NOT NULL DEFAULT '' CHECK (char_length(github_user) <= 256),
    github_repo   text        NOT NULL DEFAULT '' CHECK (char_length(github_repo) <= 256),
    github_branch text        NOT NULL DEFAULT '' CHECK (char_length(github_branch) <= 256),
    tags          jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(tags) = 'array'),
    dependencies  jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(dependencies) = 'array'),
    downloads     bigint      NULL CHECK (downloads >= 0),
    commit_sha    text        NOT NULL DEFAULT '' CHECK (char_length(commit_sha) <= 64),
    imported_at   timestamptz NULL,
    readme_state  text        NOT NULL DEFAULT 'none' CHECK (readme_state IN ('none', 'queued', 'available', 'absent', 'unavailable')),
    readme_reason text        NOT NULL DEFAULT '' CHECK (char_length(readme_reason) <= 64),
    readme_file   text        NOT NULL DEFAULT '' CHECK (char_length(readme_file) <= 256),
    readme_html   text        NOT NULL DEFAULT '' CHECK (octet_length(readme_html) <= 524288),
    readme_commit text        NOT NULL DEFAULT '' CHECK (char_length(readme_commit) <= 64),
    recorded_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, role_id)
);
