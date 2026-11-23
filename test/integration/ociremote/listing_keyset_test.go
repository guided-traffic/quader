//go:build integration

package ociremote

import (
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_API_010_APageOfARemotesTagsReadsNoTagPastItsLimit: the tags of an image in a remote OCI repository, which are cache entries and
// no version rows (R-OCI-036), are listed a page at a time by a keyset query on the entry's stored name and tag (Q-OCI-267, H-829): the
// page's statement reads no tag entry past its limit, on the first page, on one deep in the list and oldest first. 2,000 tiny tag entries
// of one pulled image are written by SQL, listed as a background download lists them; about 2 s on the stack.
func TestR_API_010_APageOfARemotesTagsReadsNoTagPastItsLimit(t *testing.T) {
	e := newEnv(t, options{})
	rep := e.start(1)[0]
	e.pull(rep, rAnon, "library/alpine", "3.19")
	e.st.Exec(`INSERT INTO remote_cache_entries (repo_id, path, variant, kind, blob_sha256, upstream_url, fetched_at, validated_at, expires_at,
	                                              last_used_at, background_sha256)
		SELECT c.repo_id, 'library/alpine/_tags/m' || lpad(g::text, 4, '0'), '', 'metadata', c.blob_sha256, c.upstream_url,
		       c.fetched_at + make_interval(secs => g), c.validated_at, c.expires_at, c.last_used_at, c.blob_sha256
		  FROM remote_cache_entries c, generate_series(1, 2000) g
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
	for name, q := range map[string]string{"the first page": first, "a page deep in the list": deep, "the first page oldest first": asc} {
		if n := e.st.Text("SELECT count(*)::text FROM (" + q + ") x"); n != "51" {
			t.Errorf("%s holds %s tags, want 51", name, n)
		}
		if read := e.st.RowsRead(q)["remote_cache_entries"]; read > 51 {
			t.Errorf("%s read %v tag entries for 51", name, read)
		}
	}
}
