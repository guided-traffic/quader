//go:build integration

package vulnsync

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/vuln/store"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// Q-VULN-175, Q-VULN-194: the trim of the database's repository names the layers and manifests of the database's image that were stored
// before the bound, and nothing else: not the layer in use however old it is, not the manifest the run read, not a file at or after the
// bound, not the tag, not another image's files, not another repository's, not a file in the trash. The bound is a time the test sets;
// no file is older than its row says. Paging by the last path returns the same names.
func TestQ_VULN_194_TheTrimNamesTheOldFilesOfTheImageThatAreNotInUse(t *testing.T) {
	st := testenv.NewStack(t, testenv.StackOptions{Name: "dbtrim"})
	st.Repository(testenv.DefaultProject+"/scanner-db", "remote", "oci", "")
	st.Repository(testenv.DefaultProject+"/elsewhere", "remote", "oci", "")
	repoID := st.Number(`SELECT id FROM repositories WHERE key = 'scanner-db'`)

	before := time.Date(2026, 11, 18, 23, 0, 0, 0, time.UTC)
	const image = "aquasec/trivy-db"
	// file stores one file of a few bytes, named after its content, in the repository, created at the offset from the bound.
	file := func(repo, path, content string, age time.Duration, trashed bool) {
		st.Exec(`INSERT INTO blobs (sha256, sha1, md5, sha512, size)
			SELECT sha256(v), substring(sha512(v) FROM 1 FOR 20), decode(md5(v), 'hex'), sha512(v), octet_length(v) FROM (SELECT $1::text::bytea AS v) c
			ON CONFLICT DO NOTHING`, content)
		st.Exec(`INSERT INTO items (repo_id, path, parent_path, kind, blob_sha256, size, created_at, trashed_at)
			SELECT r.id, $2, regexp_replace($2, '/[^/]*$', ''), 'file', sha256($3::text::bytea), octet_length($3::text::bytea), $4::timestamptz,
			       CASE WHEN $5 THEN now() END
			  FROM repositories r WHERE r.key = $1`, repo, path, content, before.Add(age), trashed)
	}
	old := -time.Second
	file("scanner-db", image+"/_blobs/sha256:inuse", "in use", -72*time.Hour, false)
	file("scanner-db", image+"/_blobs/sha256:old", "old", old, false)
	file("scanner-db", image+"/_blobs/sha256:atbound", "at the bound", 0, false)
	file("scanner-db", image+"/_blobs/sha256:young", "young", time.Second, false)
	file("scanner-db", image+"/_manifests/sha256:read", "the manifest the run read", -72*time.Hour, false)
	file("scanner-db", image+"/_manifests/sha256:oldmanifest", "an old manifest", old, false)
	file("scanner-db", image+"/_tags/2", "the tag", -72*time.Hour, false)
	file("scanner-db", image+"/_blobs/sha256:trashed", "trashed", -72*time.Hour, true)
	file("scanner-db", "aquasec/trivy-java-db/_blobs/sha256:other", "another image", -72*time.Hour, false)
	file("elsewhere", image+"/_blobs/sha256:old", "old", -72*time.Hour, false)
	st.Exec(`INSERT INTO vuln_source_state (source, ecosystem, state, etag, archive_sha256) VALUES ('trivy', 'OCI', 'complete', 'sha256:inuse', sha256('in use'::bytea))`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, st.DatabaseConfig(), "dbtrim", "worker", testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	images := store.NewImages(db)
	keep := []string{image + "/_manifests/sha256:read"}
	want := []string{image + "/_blobs/sha256:old", image + "/_manifests/sha256:oldmanifest"}

	got, err := images.DatabaseStale(ctx, repoID, image, keep, before, "", 100)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("named %q (%v), want %q", got, err, want)
	}
	var paged []string
	for after := ""; ; {
		page, err := images.DatabaseStale(ctx, repoID, image, keep, before, after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		paged, after = append(paged, page...), page[len(page)-1]
	}
	if !slices.Equal(paged, want) {
		t.Errorf("paged %q, want %q", paged, want)
	}

	// The layer in use is named once another digest is in use.
	st.Exec(`UPDATE vuln_source_state SET etag = 'sha256:young', archive_sha256 = sha256('young'::bytea) WHERE source = 'trivy'`)
	got, err = images.DatabaseStale(ctx, repoID, image, keep, before, "", 100)
	if err != nil || !slices.Contains(got, image+"/_blobs/sha256:inuse") {
		t.Errorf("after the database in use changed: %q (%v)", got, err)
	}
}
