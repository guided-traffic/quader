-- The statements of the task remote-describe (internal/packages/remotedescribe; npm R-NPM-050, Debian R-DEB-037).

-- name: RemoteDescribePending :one
-- The jobs of the given kinds that wait to run: the task queues no more while the describe jobs it and the fills queued are this many.
SELECT count(*)::bigint FROM job WHERE state = 'queued' AND kind = ANY(sqlc.arg(kinds)::text[]);
