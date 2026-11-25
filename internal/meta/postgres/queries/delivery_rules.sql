-- Statements of the delivery rules of a remote repository (remote-curation.md R-CUR-001 to R-CUR-006, work package R2 of
-- docs/developer/remote-curation.md): the snapshot's read of every rule set, the rules' API, the revision of a rule set, the journal's
-- replay of a narrowing, and the record of a client's refusal in the request list (R-CUR-004, R-CUR-015).

-- name: DeliveryRulesAll :many
-- Every rule of every remote repository, for the replica's snapshot (R-CUR-006 "Propagation"): the subject, the effect and the range
-- of a package's allow (R-CUR-026) only.
SELECT r.repo_id, r.kind, r.pattern, r.name, r.version, r.effect, r.from_version, r.to_version, r.to_inclusive
  FROM remote_delivery_rules r
 ORDER BY r.repo_id, r.kind, r.id;

-- name: DeliveryRulesOfRepository :many
-- The rules of one repository, for a snapshot entry looked up alone and for the replay of a narrowing.
SELECT r.id, r.kind, r.pattern, r.name, r.version, r.effect, r.from_version, r.to_version, r.to_inclusive
  FROM remote_delivery_rules r
 WHERE r.repo_id = sqlc.arg(repo_id)::bigint
 ORDER BY r.kind, r.id;

-- name: DeliveryRulesOfPackage :many
-- The package and version rules of one package with what the API answers of them: what a write that decides a package or a version
-- compiles beside the sections (R-CUR-024, Q-CUR-207), so its cost does not grow with the repository's other rules.
SELECT r.id, r.kind, r.pattern, r.name, r.version, r.effect, r.from_version, r.to_version, r.to_inclusive, r.note, r.request_id,
       r.created_by, r.created_at, COALESCE(p.name, '')::text AS created_by_name
  FROM remote_delivery_rules r
  LEFT JOIN principal p ON p.id = r.created_by
 WHERE r.repo_id = sqlc.arg(repo_id)::bigint AND r.name = sqlc.arg(name)::text
 ORDER BY r.kind, r.version;

-- name: DeliveryRuleSections :many
-- The section rules of one repository, at most 100 (R-CUR-006).
SELECT r.pattern, r.effect
  FROM remote_delivery_rules r
 WHERE r.repo_id = sqlc.arg(repo_id)::bigint AND r.kind = 'section'
 ORDER BY r.id;

-- name: DeliveryRulesDeleteVersions :many
-- Removes every version rule of one package and returns them: the package's switch with Reset (R-CUR-024).
DELETE FROM remote_delivery_rules
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND kind = 'version' AND name = sqlc.arg(name)::text
RETURNING id, kind, pattern, name, version, effect, note, request_id, created_by, created_at;

-- name: DeliveryRuleInsertMany :exec
-- One allowing package rule per name, in one statement (R-CUR-025 allow-stored); the names are the format's normalized ones.
INSERT INTO remote_delivery_rules (id, repo_id, kind, name, effect, note, created_by)
SELECT u.id::uuid, sqlc.arg(repo_id)::bigint, 'package', u.name, 'allow', '', NULLIF(sqlc.arg(created_by)::text, '')::uuid
  FROM (SELECT unnest(sqlc.arg(ids)::text[]) AS id, unnest(sqlc.arg(names)::text[]) AS name) AS u;

-- name: DeliveryStoredPackagesPage :many
-- One page of the names of the packages a repository stores, in name order after a name (R-CUR-025's summary and allow-stored).
SELECT p.name
  FROM packages p
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name > sqlc.arg(after_name)::text
 ORDER BY p.name
 LIMIT sqlc.arg(max_rows)::int;

-- name: DeliveryRulesList :many
-- One page of a repository's rules, newest first (R-CUR-006): the optional kind and effect, q a substring of the subject, and the
-- keyset of the previous page (created_at, id). The creator's name is joined.
SELECT r.id, r.kind, r.pattern, r.name, r.version, r.effect, r.from_version, r.to_version, r.to_inclusive, r.note, r.request_id,
       r.created_by, r.created_at, COALESCE(p.name, '')::text AS created_by_name
  FROM remote_delivery_rules r
  LEFT JOIN principal p ON p.id = r.created_by
 WHERE r.repo_id = sqlc.arg(repo_id)::bigint
   AND (sqlc.arg(kind)::text = '' OR r.kind = sqlc.arg(kind)::text)
   AND (sqlc.arg(effect)::text = '' OR r.effect = sqlc.arg(effect)::text)
   AND (sqlc.arg(q)::text = '' OR strpos(lower(COALESCE(r.pattern, r.name || COALESCE('@' || r.version, ''))), lower(sqlc.arg(q)::text)) > 0)
   AND (sqlc.arg(after_at)::text = ''
        OR (r.created_at, r.id) < (sqlc.arg(after_at)::text::timestamptz, sqlc.arg(after_id)::text::uuid))
 ORDER BY r.created_at DESC, r.id DESC
 LIMIT sqlc.arg(row_limit)::int;

-- name: DeliveryRuleBySubject :many
-- The rule of a subject, locked: a write of a subject that has a rule replaces its effect, its range and its note (R-CUR-006 "One rule
-- per subject", R-CUR-026).
SELECT r.id, r.effect, r.from_version, r.to_version, r.to_inclusive, r.note, r.request_id, r.created_by, r.created_at
  FROM remote_delivery_rules r
 WHERE r.repo_id = sqlc.arg(repo_id)::bigint AND r.kind = sqlc.arg(kind)::text
   AND COALESCE(r.pattern, '') = sqlc.arg(pattern)::text AND COALESCE(r.name, '') = sqlc.arg(name)::text
   AND COALESCE(r.version, '') = sqlc.arg(version)::text
   FOR UPDATE;

-- name: DeliveryRuleInsert :exec
-- A rule; from_version and to_version '' are no bound on that side (R-CUR-026), to_inclusive true without an upper bound.
INSERT INTO remote_delivery_rules (id, repo_id, kind, pattern, name, version, effect, from_version, to_version, to_inclusive, note,
                                   request_id, created_by)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(repo_id)::bigint, sqlc.arg(kind)::text, NULLIF(sqlc.arg(pattern)::text, ''),
        NULLIF(sqlc.arg(name)::text, ''), NULLIF(sqlc.arg(version)::text, ''), sqlc.arg(effect)::text,
        NULLIF(sqlc.arg(from_version)::text, ''), NULLIF(sqlc.arg(to_version)::text, ''),
        sqlc.arg(to_version)::text = '' OR sqlc.arg(to_inclusive)::boolean, sqlc.arg(note)::text,
        NULLIF(sqlc.arg(request_id)::text, '')::uuid, NULLIF(sqlc.arg(created_by)::text, '')::uuid);

-- name: DeliveryRuleReplace :exec
-- A write of a subject that has a rule: its effect, its range (R-CUR-026; '' is no bound on that side, which a block always has) and
-- note, and the request that caused the write when one did.
UPDATE remote_delivery_rules
   SET effect = sqlc.arg(effect)::text, from_version = NULLIF(sqlc.arg(from_version)::text, ''),
       to_version = NULLIF(sqlc.arg(to_version)::text, ''), to_inclusive = sqlc.arg(to_version)::text = '' OR sqlc.arg(to_inclusive)::boolean,
       note = sqlc.arg(note)::text,
       request_id = COALESCE(NULLIF(sqlc.arg(request_id)::text, '')::uuid, request_id)
 WHERE id = sqlc.arg(id)::text::uuid;

-- name: DeliveryRuleDelete :many
-- Removes one rule of the repository and returns it.
DELETE FROM remote_delivery_rules
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND id = sqlc.arg(id)::text::uuid
RETURNING id, kind, pattern, name, version, effect, from_version, to_version, to_inclusive, note, request_id;

-- name: DeliveryRuleGet :many
SELECT r.id, r.kind, r.pattern, r.name, r.version, r.effect, r.from_version, r.to_version, r.to_inclusive, r.note, r.request_id,
       r.created_by, r.created_at, COALESCE(p.name, '')::text AS created_by_name
  FROM remote_delivery_rules r
  LEFT JOIN principal p ON p.id = r.created_by
 WHERE r.repo_id = sqlc.arg(repo_id)::bigint AND r.id = sqlc.arg(id)::text::uuid;

-- name: DeliveryRuleCounts :one
-- The repository's rules and its section rules, for the limits of R-CUR-006.
SELECT count(*)::bigint AS rules, (count(*) FILTER (WHERE kind = 'section'))::bigint AS sections
  FROM remote_delivery_rules
 WHERE repo_id = sqlc.arg(repo_id)::bigint;

-- name: DeliveryRulesRaiseRevision :one
-- Raises the revision of the repository's rule set in the writing transaction (R-CUR-006) and returns it; the row lock orders the
-- writes of one repository's rules.
UPDATE repositories
   SET delivery_rules_revision = delivery_rules_revision + 1
 WHERE id = sqlc.arg(repo_id)::bigint
RETURNING delivery_rules_revision;

-- name: DeliveryRulesRaiseRevisionMode :one
-- DeliveryRulesRaiseRevision that also reads the repository's delivery mode under the row lock ('' is allExceptBlocked): a switch and
-- allow-stored decide against the mode as stored, not as a replica's snapshot holds it (R-CUR-024, R-CUR-025).
UPDATE repositories
   SET delivery_rules_revision = delivery_rules_revision + 1
 WHERE id = sqlc.arg(repo_id)::bigint
RETURNING delivery_rules_revision, COALESCE(config -> 'remote' -> 'delivery' ->> 'mode', '')::text AS mode;

-- name: DeliveryRefusalCount :execrows
-- Counts a client's refusals in the request entry of the subject (R-CUR-004 "Record", R-CUR-015): the open entry, else the entry
-- declined last, whose later refusals count in it. version '' is the package whole.
UPDATE remote_delivery_requests
   SET refusals = refusals + sqlc.arg(refusals)::bigint, last_at = now()
 WHERE id = (SELECT e.id FROM remote_delivery_requests e
              WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.name = sqlc.arg(name)::text
                AND COALESCE(e.version, '') = sqlc.arg(version)::text AND e.state IN ('open', 'declined')
              ORDER BY (e.state = 'open') DESC, e.last_at DESC
              LIMIT 1);

-- name: DeliveryRefusalOpen :exec
-- Opens the entry of a subject that only clients asked for: no asker, the refusals counted. A concurrent first refusal of the same
-- subject on another replica adds to the entry it opened.
INSERT INTO remote_delivery_requests (id, repo_id, name, version, state, refusals)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, NULLIF(sqlc.arg(version)::text, ''), 'open',
        sqlc.arg(refusals)::bigint)
ON CONFLICT (repo_id, name, COALESCE(version, '')) WHERE state = 'open'
DO UPDATE SET refusals = remote_delivery_requests.refusals + EXCLUDED.refusals, last_at = now();

-- name: DeliveryRefusalBound :execrows
-- Keeps at most max_entries open entries without an asker per repository (proxy.delivery.max_client_entries, R-CUR-015): the entry
-- with the oldest last refusal goes first.
DELETE FROM remote_delivery_requests
 WHERE id IN (SELECT e.id FROM remote_delivery_requests e
               WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.state = 'open' AND e.askers = 0
               ORDER BY e.last_at DESC, e.id DESC
              OFFSET sqlc.arg(max_entries)::int);
