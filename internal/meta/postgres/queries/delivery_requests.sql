-- Statements of the requests for an allow and of what was fetched for examination (remote-curation.md R-CUR-014 to R-CUR-016, work
-- package R2b of docs/developer/remote-curation.md): the request entries with their askers, the list, the examination a request starts
-- (a row or a bulk of the download queue), the examination rows a run writes, a rule write removes, a discard and the retention task
-- read, and the stored files of a version. An entry and a queue row cross the boundary as to_jsonb text, decoded by one function each.

-- name: DeliveryRequestOpenLock :many
-- The open entry of a subject, locked: an ask adds its asker to it (R-CUR-015 "One entry per subject"). version '' is the package.
SELECT to_jsonb(e.*)::text AS doc
  FROM remote_delivery_requests e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.name = sqlc.arg(name)::text
   AND COALESCE(e.version, '') = sqlc.arg(version)::text AND e.state = 'open'
   FOR UPDATE;

-- name: DeliveryRequestInsert :many
-- A new open entry of a subject; a concurrent first ask of the same subject inserts nothing and the caller reads the open entry.
INSERT INTO remote_delivery_requests (id, repo_id, name, version, state)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, NULLIF(sqlc.arg(version)::text, ''), 'open')
ON CONFLICT (repo_id, name, COALESCE(version, '')) WHERE state = 'open' DO NOTHING
RETURNING to_jsonb(remote_delivery_requests.*)::text AS doc;

-- name: DeliveryRequestGet :many
-- One entry of a repository; lock locks it for a decision or a withdrawal.
SELECT to_jsonb(e.*)::text AS doc
  FROM remote_delivery_requests e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.id = sqlc.arg(id)::text::uuid;

-- name: DeliveryRequestLock :many
SELECT to_jsonb(e.*)::text AS doc
  FROM remote_delivery_requests e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.id = sqlc.arg(id)::text::uuid
   FOR UPDATE;

-- name: DeliveryRequestAsker :many
-- One person's row of an entry.
SELECT a.withdrawn
  FROM remote_delivery_request_askers a
 WHERE a.request_id = sqlc.arg(request_id)::text::uuid AND a.principal = sqlc.arg(principal)::text::uuid;

-- name: DeliveryRequestAskerPut :exec
-- A person asks in an entry: a new row, or a row that had withdrawn asks again with its new note.
INSERT INTO remote_delivery_request_askers (request_id, principal, note)
VALUES (sqlc.arg(request_id)::text::uuid, sqlc.arg(principal)::text::uuid, sqlc.arg(note)::text)
ON CONFLICT (request_id, principal) DO UPDATE SET note = EXCLUDED.note, asked_at = now(), withdrawn = false;

-- name: DeliveryRequestAskerWithdraw :execrows
UPDATE remote_delivery_request_askers
   SET withdrawn = true
 WHERE request_id = sqlc.arg(request_id)::text::uuid AND principal = sqlc.arg(principal)::text::uuid AND NOT withdrawn;

-- name: DeliveryRequestCountAskers :one
-- Writes the entry's count of people who asked and did not withdraw, and its last activity, and returns the entry. touch moves last_at
-- (an ask); a withdrawal leaves it, so that the retention measures from the last ask or refusal (R-CUR-015 "Withdraw").
UPDATE remote_delivery_requests e
   SET askers  = (SELECT count(*) FROM remote_delivery_request_askers a WHERE a.request_id = e.id AND NOT a.withdrawn),
       last_at = CASE WHEN sqlc.arg(touch)::bool THEN now() ELSE e.last_at END
 WHERE e.id = sqlc.arg(id)::text::uuid
RETURNING to_jsonb(e.*)::text AS doc;

-- name: DeliveryRequestOpenOfPerson :one
-- The open entries of a repository a person asks in and did not withdraw from: the bound proxy.delivery.open_requests_per_person.
SELECT count(*)::bigint
  FROM remote_delivery_request_askers a
  JOIN remote_delivery_requests e ON e.id = a.request_id
 WHERE a.principal = sqlc.arg(principal)::text::uuid AND NOT a.withdrawn AND e.repo_id = sqlc.arg(repo_id)::bigint AND e.state = 'open';

-- name: DeliveryRequestSetState :exec
-- The end of an entry: allowed or declined by a person (with the reason of a decline), or withdrawn by its last asker.
UPDATE remote_delivery_requests
   SET state = sqlc.arg(state)::text, decided_by = NULLIF(sqlc.arg(decided_by)::text, '')::uuid,
       decided_at = CASE WHEN sqlc.arg(state)::text IN ('allowed', 'declined') THEN now() ELSE decided_at END,
       decline_reason = NULLIF(sqlc.arg(decline_reason)::text, '')
 WHERE id = sqlc.arg(id)::text::uuid;

-- name: DeliveryRequestSetExaminationRefused :exec
-- Why the entry's examination did not start ('' clears it).
UPDATE remote_delivery_requests
   SET examination_refused = NULLIF(sqlc.arg(reason)::text, '')
 WHERE id = sqlc.arg(id)::text::uuid;

-- name: DeliveryRequestsList :many
-- One page of a repository's entries (R-CUR-015 "The list"): open entries first, then by the last ask or refusal, newest first. Every
-- filter is optional: the state; origin person (someone asks) or client (clients were refused); mine (the principal asks in it,
-- withdrawn or not); a principal of '' matches all. The cursor is the last entry's open flag, last activity and id.
SELECT to_jsonb(e.*)::text AS doc
  FROM remote_delivery_requests e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint
   AND (sqlc.arg(state)::text = '' OR e.state = sqlc.arg(state)::text)
   AND (sqlc.arg(origin)::text = '' OR (sqlc.arg(origin)::text = 'person' AND e.askers > 0)
        OR (sqlc.arg(origin)::text = 'client' AND e.refusals > 0))
   AND (sqlc.arg(mine)::text = '' OR EXISTS (SELECT 1 FROM remote_delivery_request_askers a
                                             WHERE a.request_id = e.id AND a.principal = NULLIF(sqlc.arg(mine)::text, '')::uuid))
   AND (sqlc.arg(after_open)::int < 0
        OR (CASE WHEN e.state = 'open' THEN 0 ELSE 1 END) > sqlc.arg(after_open)::int
        OR ((CASE WHEN e.state = 'open' THEN 0 ELSE 1 END) = sqlc.arg(after_open)::int
            AND ((extract(epoch FROM e.last_at) * 1000000)::bigint < sqlc.arg(after_us)::bigint
                 OR ((extract(epoch FROM e.last_at) * 1000000)::bigint = sqlc.arg(after_us)::bigint
                     AND e.id::text < sqlc.arg(after_id)::text))))
 ORDER BY CASE WHEN e.state = 'open' THEN 0 ELSE 1 END, e.last_at DESC, e.id::text DESC
 LIMIT sqlc.arg(max_rows)::int;

-- name: DeliveryRequestAskersOf :many
-- The people who ask in the entries, with their names and notes, oldest ask first.
SELECT a.request_id::text AS request_id, a.principal::text AS principal, COALESCE(p.name, '')::text AS name, a.note, a.asked_at
  FROM remote_delivery_request_askers a
  LEFT JOIN principal p ON p.id = a.principal
 WHERE a.request_id = ANY(sqlc.arg(ids)::text[]::uuid[]) AND NOT a.withdrawn
 ORDER BY a.request_id, a.asked_at, a.principal;

-- name: DeliveryRequestRows :many
-- The queue rows of an entry, newest first: its examination and the downloads its allow started (R-CUR-015).
SELECT to_jsonb(d.*)::text AS doc
  FROM remote_download d
 WHERE d.repo_id = sqlc.arg(repo_id)::bigint AND d.request_id = ANY(sqlc.arg(ids)::text[]::uuid[])
 ORDER BY d.request_id, d.created_at DESC, d.id;

-- name: DeliveryRequestBulks :many
-- The bulks of an entry that wait to be expanded (its examination of a package before its rows exist).
SELECT b.id::text AS id, b.request_id::text AS request_id, b.state, b.origin
  FROM remote_download_bulks b
 WHERE b.repo_id = sqlc.arg(repo_id)::bigint AND b.request_id = ANY(sqlc.arg(ids)::text[]::uuid[])
 ORDER BY b.created_at DESC;

-- name: DeliveryRequestsOpenOf :many
-- The open entries of a package's subjects (remotecuration.Requests.Open): the package's own entry and those of the versions given.
SELECT e.id::text AS id, COALESCE(e.version, '')::text AS version, e.askers, e.refusals
  FROM remote_delivery_requests e
 WHERE e.repo_id = sqlc.arg(repo_id)::bigint AND e.name = sqlc.arg(name)::text AND e.state = 'open'
   AND (e.version IS NULL OR e.version = ANY(sqlc.arg(versions)::text[]));

-- name: DeliveryRequestsPrune :execrows
-- The retention of entries nobody asks in any more (R-CUR-015 "Withdraw"): an open or withdrawn entry without an asker whose last ask
-- or refusal is older than proxy.delivery.request_retention; one batch.
DELETE FROM remote_delivery_requests
 WHERE id IN (SELECT e.id FROM remote_delivery_requests e
               WHERE e.askers = 0 AND e.state IN ('open', 'withdrawn') AND e.last_at < to_timestamp(sqlc.arg(before_us)::bigint / 1000000.0)
               ORDER BY e.last_at
               LIMIT sqlc.arg(max_rows)::int);

-- name: DeliveryRequestsOpenCounts :many
-- The open entries by repository, for the gauge quader_remote_delivery_requests_open.
SELECT repo_id, count(*)::bigint AS n FROM remote_delivery_requests WHERE state = 'open' GROUP BY repo_id;

-- name: RemoteDownloadBulkInsertOf :one
-- A bulk that a request or an allow starts for the newest versions of a package (Q-CUR-141): the origin of its rows, the request and
-- the asker's charge.
INSERT INTO remote_download_bulks (id, repo_id, level, subject, max_versions, max_bytes, include_prereleases, state, started_by, origin,
                                   request_id, ticket)
VALUES (sqlc.arg(id)::text::uuid, sqlc.arg(repo_id)::bigint, sqlc.arg(level)::text, sqlc.arg(subject)::text,
        sqlc.arg(max_versions)::int, sqlc.arg(max_bytes)::bigint, sqlc.arg(include_prereleases)::bool, 'expanding',
        NULLIF(sqlc.arg(started_by)::text, '')::uuid, sqlc.arg(origin)::text, NULLIF(sqlc.arg(request_id)::text, '')::uuid,
        NULLIF(sqlc.arg(ticket)::text, '')::jsonb)
RETURNING to_jsonb(remote_download_bulks.*)::text AS doc;

-- name: RemoteExaminationPut :exec
-- The examination row of a version a run stored while the rules did not deliver it (R-CUR-016 "What is examination"): written once; a
-- later run keeps fetched_at and adds the files it found (at most 2,000 in all) and the request when the row had none.
INSERT INTO remote_examinations (repo_id, name, version, request_id, files)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, NULLIF(sqlc.arg(request_id)::text, '')::uuid,
        (ARRAY(SELECT DISTINCT f FROM unnest(sqlc.arg(files)::text[]) AS f ORDER BY f LIMIT 2000)))
ON CONFLICT (repo_id, name, version) DO UPDATE
   SET files = ARRAY(SELECT DISTINCT f FROM unnest(remote_examinations.files || EXCLUDED.files) AS f ORDER BY f LIMIT 2000),
       request_id = COALESCE(remote_examinations.request_id, EXCLUDED.request_id);

-- name: RemoteExaminationsOf :many
-- The examination rows of a package, or of one of its versions when version is not ''.
SELECT x.name, x.version, x.files, x.fetched_at, COALESCE(x.request_id::text, '')::text AS request_id
  FROM remote_examinations x
 WHERE x.repo_id = sqlc.arg(repo_id)::bigint AND x.name = sqlc.arg(name)::text
   AND (sqlc.arg(version)::text = '' OR x.version = sqlc.arg(version)::text)
 ORDER BY x.version;

-- name: RemoteExaminationsPage :many
-- One page of a repository's examination rows in the order of their subjects, after (name, version): what a section rule's write
-- reads for the rows it comes to deliver.
SELECT x.name, x.version
  FROM remote_examinations x
 WHERE x.repo_id = sqlc.arg(repo_id)::bigint AND (x.name, x.version) > (sqlc.arg(after_name)::text, sqlc.arg(after_version)::text)
 ORDER BY x.name, x.version
 LIMIT sqlc.arg(max_rows)::int;

-- name: RemoteExaminationDelete :execrows
-- Removes the examination rows of a package's versions.
DELETE FROM remote_examinations
 WHERE repo_id = sqlc.arg(repo_id)::bigint AND name = sqlc.arg(name)::text AND version = ANY(sqlc.arg(versions)::text[]);

-- name: RemoteExaminationsOlder :many
-- One batch of the examination rows fetched before a time, the oldest first, after the keyset of the previous batch (the retention
-- task of R-CUR-016, which reads each repository's days from its snapshot).
SELECT x.repo_id, x.name, x.version, x.fetched_at
  FROM remote_examinations x
 WHERE x.fetched_at < to_timestamp(sqlc.arg(before_us)::bigint / 1000000.0)
   AND (x.fetched_at, x.repo_id, x.name, x.version)
       > (to_timestamp(sqlc.arg(after_us)::bigint / 1000000.0), sqlc.arg(after_repo)::bigint, sqlc.arg(after_name)::text,
          sqlc.arg(after_version)::text)
 ORDER BY x.fetched_at, x.repo_id, x.name, x.version
 LIMIT sqlc.arg(max_rows)::int;

-- name: RemoteExaminationMakeDue :exec
-- The replay of a discard (R-CUR-016): the version's examination row is made due for the next retention run, which removes what is
-- stored of it while the rules still do not deliver it; a version without a row gets one.
INSERT INTO remote_examinations (repo_id, name, version, fetched_at)
VALUES (sqlc.arg(repo_id)::bigint, sqlc.arg(name)::text, sqlc.arg(version)::text, to_timestamp(0))
ON CONFLICT (repo_id, name, version) DO UPDATE SET fetched_at = to_timestamp(0);

-- name: RemoteStoredVersionFiles :many
-- The stored files of a package's versions in a repository by the generic package rows: the items linked to a live version and the
-- version's lead item, with the version as the package rows hold it. A format whose remote writes no version rows (OCI) answers none.
SELECT pv.version, i.path
  FROM packages p
  JOIN package_versions pv ON pv.package_id = p.id AND pv.trashed_at IS NULL
  JOIN items i ON (i.version_id = pv.id OR i.id = pv.lead_item_id) AND i.trashed_at IS NULL AND i.kind = 'file'
 WHERE p.repo_id = sqlc.arg(repo_id)::bigint AND p.name = sqlc.arg(name)::text
 ORDER BY pv.version, i.path
 LIMIT 10000;

-- name: RemoteRepositoryExists :one
-- Whether a repository row exists, for the replay of a discard.
SELECT EXISTS (SELECT 1 FROM repositories WHERE id = sqlc.arg(repo_id)::bigint)::bool;
