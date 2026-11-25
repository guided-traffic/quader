-- quader:phase expand
--
-- Every list of versions is newest first by its format's order, and a string the format does not rank comes below every version, newest
-- first by its time (api-and-ui.md R-API-029, Q-API-431; formats/oci.md R-OCI-031, Q-OCI-269). A key's first byte is its class
-- (internal/formats/versionkey): 0x01 a version, 0x00 a string that is none. An image tag that is no version (`latest`, `main`,
-- `sha-5ccfd1a`) was a version of the class 0x01 followed by 0x00, which no test of the class can tell from npm's 0x01 followed by its
-- first number; it is now NotVersion, its key 0x00, the tag and the end of a text, as the format's SortKey gives it.
--
-- 1. The key of an image tag: a tag that is no version is NotVersion, like a string that is no tag.
CREATE OR REPLACE FUNCTION oci_tag_sort_key(tag text) RETURNS bytea
    LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
  text_end constant bytea := '\x0001';
  m        text[];
  nums     text[];
  n        text;
  last     int;
  suffix   text;
  rank     int;
  k        bytea;
BEGIN
  m := regexp_match(tag, '^v?([0-9]+(?:\.[0-9]+)*)(?:-(.+))?$');
  IF tag !~ '^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$' OR m IS NULL THEN
    RETURN '\x00'::bytea || convert_to(tag, 'UTF8') || text_end;
  END IF;
  nums := string_to_array(m[1], '.');
  last := 0;
  FOR i IN 1 .. cardinality(nums) LOOP
    nums[i] := ltrim(nums[i], '0');
    IF nums[i] <> '' THEN
      last := i;
    END IF;
  END LOOP;
  k := '\x0101'::bytea;
  FOR i IN 1 .. last LOOP
    n := nums[i];
    k := k || '\x01'::bytea || int2send(length(n)::smallint) || convert_to(n, 'UTF8');
  END LOOP;
  suffix := COALESCE(m[2], '');
  rank := CASE WHEN suffix = '' THEN 2
               WHEN lower(suffix) ~ '^(alpha|beta|rc|pre|dev)[0-9.]*$' THEN 0
               ELSE 1 END;
  RETURN k || '\x00'::bytea || set_byte('\x00'::bytea, 0, rank) || convert_to(suffix, 'UTF8') || text_end
           || convert_to(tag, 'UTF8') || text_end;
END $$;

-- 2. A stored generated column keeps the value of the function it was computed with: the remote tag entries whose key has the old
-- class are written again, which recomputes it (ALTER COLUMN … SET EXPRESSION needs PostgreSQL 17; the minimum is 16).
UPDATE remote_cache_entries SET path = path
 WHERE tag_name IS NOT NULL AND tag_sort_key >= '\x0100'::bytea AND tag_sort_key < '\x0101'::bytea;

-- 3. A local version row's key is the format's code's: the image tags of the old class lose theirs, and the background migration
-- version_sort_key fills them again (formats/view.go SortKey: "a change of the encoding is a migration step that sets the format's
-- stored keys to NULL"); the reads give such a row its format's key meanwhile (Q-API-430).
UPDATE package_versions pv
   SET sort_key = NULL, prerelease = NULL
  FROM packages p
  JOIN repositories r ON r.id = p.repo_id AND r.format = 'oci'
 WHERE pv.package_id = p.id AND pv.sort_key >= '\x0100'::bytea AND pv.sort_key < '\x0101'::bytea;

-- 4. The second part of a list in the order of the versions: a package's versions its format does not rank, newest first by their time,
-- a keyset on (time, version) as the order of publication (00113), for the version rows and for a remote's tag entries.
CREATE INDEX package_versions_unranked ON package_versions (package_id, created_at, version COLLATE "C")
    WHERE trashed_at IS NULL AND sort_key < '\x01'::bytea;
CREATE INDEX remote_cache_entries_tag_unranked ON remote_cache_entries (repo_id, tag_name, fetched_at, tag)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL AND tag_sort_key < '\x01'::bytea;
