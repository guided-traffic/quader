-- quader:phase expand
--
-- The version list in the order of the versions, and the latest version of a package, are read by an index (api-and-ui.md R-API-029,
-- R-API-010, Q-API-429, ADR-278; formats/oci.md H-829). A version row stores the sort key of its format's order (the format's
-- ViewAdapter.SortKey, internal/formats/versionkey: bytewise the order of the format's Compare) and whether the format calls it a
-- pre-release; the host writes both with the row. A remote OCI repository's tags are cache entries (R-OCI-036): their key is derived
-- here from the stored tag, by the same encoding as the format's (internal/formats/oci/tagorder Key), and none of them is a
-- pre-release, as the format's exposure adapter calls none.
--
-- The key of a stored version is the format's code's and cannot be computed here: the columns have no default, so this step refuses a
-- database that holds version rows (pre-alpha, no installation has to keep them: Q-ARCH-340, CLAUDE.md). Such a database is started
-- afresh.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM package_versions) THEN
    RAISE EXCEPTION 'migration 00116: package_versions holds rows written without their sort key; this release needs a fresh database (pre-alpha, Q-ARCH-340)';
  END IF;
END $$;

ALTER TABLE package_versions
    ADD COLUMN sort_key   bytea   NOT NULL CHECK (octet_length(sort_key) > 0),
    ADD COLUMN prerelease boolean NOT NULL;

-- A page of a package's versions in the order of the versions (keyset on the key); the latest version, the highest key of the
-- package's releases, else of its pre-releases, each the first entry of an index of its own kind.
CREATE INDEX package_versions_by_key ON package_versions (package_id, sort_key) WHERE trashed_at IS NULL;
CREATE INDEX package_versions_latest_release ON package_versions (package_id, sort_key) WHERE trashed_at IS NULL AND NOT prerelease;
CREATE INDEX package_versions_latest_prerelease ON package_versions (package_id, sort_key) WHERE trashed_at IS NULL AND prerelease;

-- The key of an image tag (R-OCI-031, Q-OCI-17): the bytes of versionkey.Of(tagorder.Key(tag), ValidTag(tag), tag). A string that is no
-- tag is 0x00 and its text; a tag is 0x01, then 0x00 for a tag that is no version, or for a version 0x01, each number up to
-- the last that is not zero as 0x01, its digit count in two bytes and its digits without leading zeros, 0x00, the rank of its suffix
-- (0x00 a pre-release, 0x01 a variant, 0x02 none) and the suffix as text; then the tag as text. A text is its bytes and 0x00 0x01 (a
-- text in PostgreSQL holds no 0x00 to escape). TestR_OCI_036_ARemotesTagsCarryTheKeyOfTheFormat compares it with the format's.
CREATE FUNCTION oci_tag_sort_key(tag text) RETURNS bytea
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
  IF tag !~ '^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$' THEN
    RETURN '\x00'::bytea || convert_to(tag, 'UTF8') || text_end;
  END IF;
  m := regexp_match(tag, '^v?([0-9]+(?:\.[0-9]+)*)(?:-(.+))?$');
  IF m IS NULL THEN
    RETURN '\x0100'::bytea || convert_to(tag, 'UTF8') || text_end;
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

-- The tag as 00113 derives the column tag (a generated column cannot read another); NULL for every entry that is no tag.
ALTER TABLE remote_cache_entries
    ADD COLUMN tag_sort_key bytea GENERATED ALWAYS AS (oci_tag_sort_key((regexp_match(path, '^(.+)/_tags/([^/]+)$'))[2])) STORED;

-- A page of a name's tags in the order of the tags, and its latest tag.
CREATE INDEX remote_cache_entries_tag_key ON remote_cache_entries (repo_id, tag_name, tag_sort_key)
    WHERE tag_name IS NOT NULL AND kind = 'metadata' AND variant = '' AND upstream_gone_at IS NULL;

-- The views of 00113 and 00079 with the key and the pre-release flag: the same rows, the same rules.
CREATE OR REPLACE VIEW oci_remote_tags AS
SELECT (-e.id)::bigint            AS id,
       p.id                     AS package_id,
       e.repo_id                AS repo_id,
       p.name                   AS name,
       e.tag::text              AS version,
       e.fetched_at             AS created_at,
       e.blob_sha256            AS manifest_sha256,
       e.tag_sort_key           AS sort_key
  FROM remote_cache_entries e
  JOIN repositories r ON r.id = e.repo_id AND r.class = 'remote' AND r.format = 'oci'
  JOIN packages p ON p.repo_id = e.repo_id AND p.name = e.tag_name
  JOIN oci_manifests om ON om.repo_id = e.repo_id AND om.name = e.tag_name AND om.digest = e.blob_sha256
 WHERE e.kind = 'metadata'
   AND e.variant = ''
   AND e.upstream_gone_at IS NULL
   AND e.tag_name IS NOT NULL
   AND (EXISTS (SELECT 1 FROM mirror_state ms WHERE ms.repo_id = e.repo_id)
        OR e.background_sha256 = e.blob_sha256
        OR EXISTS (SELECT 1
                     FROM items i
                     JOIN item_download_stats s ON s.item_id = i.id
                    WHERE i.repo_id = e.repo_id
                      AND i.path = e.tag_name || '/_manifests/sha256:' || encode(e.blob_sha256, 'hex')
                      AND i.trashed_at IS NULL
                      AND s.downloads > 0));

CREATE OR REPLACE VIEW package_version_listing AS
SELECT pv.id, pv.package_id, pv.version, pv.created_at, pv.lead_item_id, pv.trashed_at, NULL::bytea AS document_sha256,
       pv.sort_key, pv.prerelease
  FROM package_versions pv
UNION ALL
SELECT t.id, t.package_id, t.version, t.created_at, NULL::bigint, NULL::timestamptz, t.manifest_sha256, t.sort_key, false
  FROM oci_remote_tags t;
