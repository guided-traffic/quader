-- quader:phase expand
--
-- The figure parallelDownloads is parallelTransfers (usage-and-limits.md R-LIM-004, R-LIM-024, Q-LIM-293; the owner's decision of
-- 2026-11-26: the limit counts transfers and says so, and its built-in values fit a large company). A download is the unit a person
-- asked for (an image, an install); the figure counts the transfers of its parts that run at once, so its column, the overrides of
-- the rules and of the projects' maxima, and the recorded limit events take the new name.

ALTER TABLE limit_profile RENAME COLUMN parallel_downloads TO parallel_transfers;
ALTER TABLE limit_profile RENAME CONSTRAINT limit_profile_parallel_downloads_check TO limit_profile_parallel_transfers_check;

UPDATE limit_assignment
   SET overrides = (overrides - 'parallelDownloads') || jsonb_build_object('parallelTransfers', overrides -> 'parallelDownloads')
 WHERE overrides ? 'parallelDownloads';
UPDATE limit_project_maximum
   SET overrides = (overrides - 'parallelDownloads') || jsonb_build_object('parallelTransfers', overrides -> 'parallelDownloads')
 WHERE overrides ? 'parallelDownloads';
UPDATE usage_limit_event SET figure = 'parallelTransfers' WHERE figure = 'parallelDownloads';

-- The built-in values, from a measurement of how many transfers each client holds at once (npm 15, pnpm up to 64, yarn 8 and 32,
-- docker 3 per daemon, podman 6 per pull, dotnet restore every package of the restore, APT and ansible-galaxy one). A built-in
-- profile whose figure an administrator changed keeps it: only the value the first migration wrote is replaced.
UPDATE limit_profile SET parallel_transfers = 1000, version = version + 1, updated_at = now()
 WHERE builtin AND key = 'technical' AND parallel_transfers = 50;
UPDATE limit_profile SET parallel_transfers = 250, version = version + 1, updated_at = now()
 WHERE builtin AND key = 'standard' AND parallel_transfers = 20;
UPDATE limit_profile SET parallel_transfers = 64, version = version + 1, updated_at = now()
 WHERE builtin AND key = 'restricted' AND parallel_transfers = 6;
UPDATE limit_profile SET parallel_transfers = 64, version = version + 1, updated_at = now()
 WHERE builtin AND key = 'anonymous' AND parallel_transfers = 4;
