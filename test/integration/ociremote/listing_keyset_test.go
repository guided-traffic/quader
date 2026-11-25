//go:build integration

package ociremote

import (
	"fmt"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_API_010_APageOfARemotesTagsReadsNoTagPastItsLimit: the tags of an image in a remote OCI repository, which are cache entries and
// no version rows (R-OCI-036), are listed a page at a time by a keyset query on the entry's stored name and tag (Q-OCI-267, H-829) and,
// in the order of the tags, on the entry's sort key (Q-API-429): each page's statement reads no tag entry past its limit, on the first
// page, on one deep in the list and oldest or lowest first, and the image's latest tag reads one entry. The sort key PostgreSQL derives
// from a tag (oci_tag_sort_key, migration 00116) is the format's, byte for byte, over a table of tags. 2,000 tiny tag entries of one
// pulled image are written by SQL, listed as a background download lists them; about 2 s on the stack.
func TestR_API_010_APageOfARemotesTagsReadsNoTagPastItsLimit(t *testing.T) {
	e := newEnv(t, options{})
	rep := e.start(1)[0]
	e.pull(rep, rAnon, "library/alpine", "3.19")
	e.st.Exec(`INSERT INTO remote_cache_entries (repo_id, path, variant, kind, blob_sha256, upstream_url, fetched_at, validated_at, expires_at,
	                                              last_used_at, background_sha256)
		SELECT c.repo_id, 'library/alpine/_tags/' || CASE WHEN g <= 2000 THEN 'm' || lpad(g::text, 4, '0') ELSE '0.' || (g - 2000)::text END,
		       '', 'metadata', c.blob_sha256, c.upstream_url,
		       c.fetched_at + make_interval(secs => g), c.validated_at, c.expires_at, c.last_used_at, c.blob_sha256
		  FROM remote_cache_entries c, generate_series(1, 2200) g
		 WHERE c.path = 'library/alpine/_tags/3.19' AND c.kind = 'metadata'`)
	e.st.Exec("ANALYZE remote_cache_entries")
	repo := e.st.Text(`SELECT id::text FROM repositories WHERE key = $1`, rAnon)
	mid := e.st.Text(`SELECT fetched_at::text FROM remote_cache_entries WHERE path = 'library/alpine/_tags/m1000'`)
	args := map[string]string{
		"repo_id": repo, "name": "'library/alpine'", "after_at": "'infinity'", "after_version": "''", "withheld": "'{}'",
		"whole": "true", "allowed": "'{}'", "max_rows": "51",
	}
	first := testenv.QueryText(t, "package_view.sql", "PackageViewTagPageDesc", args)
	args["after_at"], args["after_version"] = "'"+mid+"'", "'m1000'"
	deep := testenv.QueryText(t, "package_view.sql", "PackageViewTagPageDesc", args)
	args["after_at"], args["after_version"] = "'-infinity'", "''"
	asc := testenv.QueryText(t, "package_view.sql", "PackageViewTagPageAsc", args)
	// The tags that are no version (m0001 …), the second part of the order of the versions (Q-API-431): newest first by their fetch time.
	args["after_at"], args["after_version"] = "'infinity'", "''"
	unranked := testenv.QueryText(t, "package_view.sql", "PackageViewTagUnrankedDesc", args)
	args["after_at"], args["after_version"] = "'"+mid+"'", "'m1000'"
	unrankedDeep := testenv.QueryText(t, "package_view.sql", "PackageViewTagUnrankedDesc", args)
	args["after_at"], args["after_version"] = "'-infinity'", "''"
	unrankedAsc := testenv.QueryText(t, "package_view.sql", "PackageViewTagUnrankedAsc", args)
	if got := e.st.Text("SELECT min(version) || ' ' || max(version) FROM (" + unranked + ") x"); got != "m1950 m2000" {
		t.Errorf("the first page of the tags that are no version spans %s, want m1950 m2000", got)
	}
	for name, q := range map[string]string{
		"the first page": first, "a page deep in the list": deep, "the first page oldest first": asc,
		"the first page of the tags that are no version": unranked, "a page of them deep in the list": unrankedDeep,
		"the first page of them oldest first": unrankedAsc,
	} {
		if n := e.st.Text("SELECT count(*)::text FROM (" + q + ") x"); n != "51" {
			t.Errorf("%s holds %s tags, want 51", name, n)
		}
		if read := e.st.RowsRead(q)["remote_cache_entries"]; read > 51 {
			t.Errorf("%s read %v tag entries for 51", name, read)
		}
	}

	keyArgs := map[string]string{
		"repo_id": repo, "name": "'library/alpine'", "after_key": `'\x02'::bytea`, "withheld": "'{}'", "whole": "true", "allowed": "'{}'",
		"max_rows": "51",
	}
	kFirst := testenv.QueryText(t, "package_view.sql", "PackageViewTagKeyDesc", keyArgs)
	midKey, _ := e.st.VersionKey("oci", "0.100")
	keyArgs["after_key"] = fmt.Sprintf(`'\x%x'::bytea`, midKey)
	kDeep := testenv.QueryText(t, "package_view.sql", "PackageViewTagKeyDesc", keyArgs)
	keyArgs["after_key"] = `''::bytea`
	kAsc := testenv.QueryText(t, "package_view.sql", "PackageViewTagKeyAsc", keyArgs)
	for name, q := range map[string]string{"the first page by tag": kFirst, "a page by tag deep in the list": kDeep, "the first page by tag, lowest first": kAsc} {
		if n := e.st.Text("SELECT count(*)::text FROM (" + q + ") x"); n != "51" {
			t.Errorf("%s holds %s tags, want 51", name, n)
		}
		if read := e.st.RowsRead(q)["remote_cache_entries"]; read > 51 {
			t.Errorf("%s read %v tag entries for 51", name, read)
		}
		if n := e.st.Text("SELECT count(*)::text FROM (" + q + ") x WHERE version LIKE 'm%'"); n != "0" {
			t.Errorf("%s holds %s tags that are no version: the order of the tags reads the versions", name, n)
		}
	}
	latest := testenv.QueryText(t, "package_view.sql", "PackageViewLatest", map[string]string{
		"withheld": "'{}'", "whole": "'{" + repo + "}'", "rows": "'{}'", "packages": "ARRAY['oci' || chr(31) || 'library/alpine']",
		"scope": "'{" + repo + "}'", "has_repos": "false", "repos": "'{}'",
	})
	// The candidates of the latest tag: the highest version and the newest tag that is no version, one entry each.
	if got := e.st.Text("SELECT string_agg(version, ',' ORDER BY version) FROM (" + latest + ") x"); got != "3.19,m2000" {
		t.Errorf("the candidates of the latest tag: %q, want 3.19 and m2000", got)
	}
	if read := e.st.RowsRead(latest)["remote_cache_entries"]; read > 2 {
		t.Errorf("the latest tag read %v tag entries for two", read)
	}

	for _, tag := range []string{
		"3.19", "latest", "v1", "1.0", "1.0.0", "01.2", "1.0-rc1", "1.0-RC.2", "1.0-alpine", "1.0-rc1-x", "1-a",
		"99999999999999999999999999", "_x", "Latest", "1.2.3.4.5", "v2.9.0-beta.1", "1.0-dev12", "1.0-preview", "main", "sha-5ccfd1a", "edge",
	} {
		want, _ := e.st.VersionKey("oci", tag)
		if got := e.st.Text("SELECT encode(oci_tag_sort_key($1), 'hex')", tag); got != fmt.Sprintf("%x", want) {
			t.Errorf("the sort key of %q: PostgreSQL %s, the format %x", tag, got, want)
		}
	}
	if n := e.st.Text(`SELECT count(*)::text FROM remote_cache_entries WHERE tag IS NOT NULL AND tag_sort_key IS DISTINCT FROM oci_tag_sort_key(tag)`); n != "0" {
		t.Errorf("%s tag entries do not store the key of their tag", n)
	}
}
