-- quader:phase expand
--
-- The byte, upstream and refused figures of the built-in profiles are set from what the uses R-LIM-010 promises each profile
-- cost (usage-and-limits.md R-LIM-042, Q-LIM-296; the owner's rule of 2026-11-26: the built-in values are generous, sized for
-- large companies). The first migration's values were estimates that a first pull of an image through a remote, a cold npm
-- install or a team behind one address met. A built-in profile whose figure an administrator changed keeps it, a number or
-- unlimited: only a value the first migration wrote is replaced (Q-LIM-297). The request figures (00085) and the parallel
-- transfers (00141) stay; Standard keeps 300 GB of cached bytes an hour and 1,000 GB a day, Restricted 50 GB an hour.

UPDATE limit_profile SET
    cached_bytes_per_second    = CASE WHEN cached_bytes_per_second = 200000000 THEN 250000000 ELSE cached_bytes_per_second END,
    uncached_bytes_per_second  = CASE WHEN uncached_bytes_per_second = 50000000 THEN 125000000 ELSE uncached_bytes_per_second END,
    uncached_bytes_per_hour    = CASE WHEN uncached_bytes_per_hour = 30000000000 THEN 100000000000 ELSE uncached_bytes_per_hour END,
    upstream_requests_per_hour = CASE WHEN upstream_requests_per_hour = 2000 THEN 20000 ELSE upstream_requests_per_hour END,
    refused_per_hour           = CASE WHEN refused_per_hour = 500 THEN 1000 ELSE refused_per_hour END,
    version = version + 1, updated_at = now()
 WHERE builtin AND key = 'standard'
   AND (cached_bytes_per_second = 200000000 OR uncached_bytes_per_second = 50000000 OR uncached_bytes_per_hour = 30000000000
        OR upstream_requests_per_hour = 2000 OR refused_per_hour = 500);

UPDATE limit_profile SET
    cached_bytes_per_second    = CASE WHEN cached_bytes_per_second = 500000000 THEN 1250000000 ELSE cached_bytes_per_second END,
    cached_bytes_per_hour      = CASE WHEN cached_bytes_per_hour = 1000000000000 THEN 3000000000000 ELSE cached_bytes_per_hour END,
    uncached_bytes_per_second  = CASE WHEN uncached_bytes_per_second = 100000000 THEN 250000000 ELSE uncached_bytes_per_second END,
    uncached_bytes_per_hour    = CASE WHEN uncached_bytes_per_hour = 100000000000 THEN 500000000000 ELSE uncached_bytes_per_hour END,
    upstream_requests_per_hour = CASE WHEN upstream_requests_per_hour = 10000 THEN 100000 ELSE upstream_requests_per_hour END,
    bytes_per_day              = CASE WHEN bytes_per_day = 5000000000000 THEN 30000000000000 ELSE bytes_per_day END,
    refused_per_hour           = CASE WHEN refused_per_hour = 2000 THEN 5000 ELSE refused_per_hour END,
    version = version + 1, updated_at = now()
 WHERE builtin AND key = 'technical'
   AND (cached_bytes_per_second = 500000000 OR cached_bytes_per_hour = 1000000000000 OR uncached_bytes_per_second = 100000000
        OR uncached_bytes_per_hour = 100000000000 OR upstream_requests_per_hour = 10000 OR bytes_per_day = 5000000000000
        OR refused_per_hour = 2000);

UPDATE limit_profile SET
    cached_bytes_per_second    = CASE WHEN cached_bytes_per_second = 50000000 THEN 100000000 ELSE cached_bytes_per_second END,
    uncached_bytes_per_second  = CASE WHEN uncached_bytes_per_second = 10000000 THEN 50000000 ELSE uncached_bytes_per_second END,
    uncached_bytes_per_hour    = CASE WHEN uncached_bytes_per_hour = 5000000000 THEN 20000000000 ELSE uncached_bytes_per_hour END,
    upstream_requests_per_hour = CASE WHEN upstream_requests_per_hour = 200 THEN 3000 ELSE upstream_requests_per_hour END,
    bytes_per_day              = CASE WHEN bytes_per_day = 100000000000 THEN 200000000000 ELSE bytes_per_day END,
    refused_per_hour           = CASE WHEN refused_per_hour = 100 THEN 300 ELSE refused_per_hour END,
    version = version + 1, updated_at = now()
 WHERE builtin AND key = 'restricted'
   AND (cached_bytes_per_second = 50000000 OR uncached_bytes_per_second = 10000000 OR uncached_bytes_per_hour = 5000000000
        OR upstream_requests_per_hour = 200 OR bytes_per_day = 100000000000 OR refused_per_hour = 100);

UPDATE limit_profile SET
    cached_bytes_per_second    = CASE WHEN cached_bytes_per_second = 20000000 THEN 100000000 ELSE cached_bytes_per_second END,
    cached_bytes_per_hour      = CASE WHEN cached_bytes_per_hour = 10000000000 THEN 30000000000 ELSE cached_bytes_per_hour END,
    uncached_bytes_per_second  = CASE WHEN uncached_bytes_per_second = 5000000 THEN 50000000 ELSE uncached_bytes_per_second END,
    uncached_bytes_per_hour    = CASE WHEN uncached_bytes_per_hour = 2000000000 THEN 20000000000 ELSE uncached_bytes_per_hour END,
    upstream_requests_per_hour = CASE WHEN upstream_requests_per_hour = 20 THEN 3000 ELSE upstream_requests_per_hour END,
    bytes_per_day              = CASE WHEN bytes_per_day = 30000000000 THEN 200000000000 ELSE bytes_per_day END,
    refused_per_hour           = CASE WHEN refused_per_hour = 60 THEN 300 ELSE refused_per_hour END,
    version = version + 1, updated_at = now()
 WHERE builtin AND key = 'anonymous'
   AND (cached_bytes_per_second = 20000000 OR cached_bytes_per_hour = 10000000000 OR uncached_bytes_per_second = 5000000
        OR uncached_bytes_per_hour = 2000000000 OR upstream_requests_per_hour = 20 OR bytes_per_day = 30000000000
        OR refused_per_hour = 60);
