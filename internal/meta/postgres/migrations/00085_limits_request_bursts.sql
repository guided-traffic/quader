-- quader:phase expand
--
-- The request figures of the built-in profiles are set from a measurement (usage-and-limits.md R-LIM-010, Q-LIM-59; the owner's
-- decision of 2026-11-20: larger default bursts, set so that normal use never reaches them). docker pull and docker push do not retry
-- a 429 of the registry; one pull of an image of 40 layers sends 48 requests within a third of a second, an npm ci of 475 packages
-- 475 within 2.5 s. Only the three request figures change; every other figure keeps its value, Unlimited stays empty.

UPDATE limit_profile SET requests_per_second = 1000, requests_burst = 20000, requests_per_hour = 1000000,
                         version = version + 1, updated_at = now()
 WHERE builtin AND key = 'technical';
UPDATE limit_profile SET requests_per_second = 200, requests_burst = 4000, requests_per_hour = 100000,
                         version = version + 1, updated_at = now()
 WHERE builtin AND key = 'standard';
UPDATE limit_profile SET requests_per_second = 100, requests_burst = 1000, requests_per_hour = 25000,
                         version = version + 1, updated_at = now()
 WHERE builtin AND key = 'restricted';
UPDATE limit_profile SET requests_per_second = 50, requests_burst = 1000, requests_per_hour = 20000,
                         version = version + 1, updated_at = now()
 WHERE builtin AND key = 'anonymous';
