//go:build integration

package querybudget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// linkBudget is R-ARCH-027 for a download handed to a signed link of the object store (storage.md R-STOR-070): the decision reads
// the request, the item row with its copies, the request's charge and the replica's snapshots, and signing is local, so a link
// costs the item lookup alone, as a stream does. The download is an OCI blob, whose route declares that the clients check the
// digest in its path (R-STOR-071). The switch is on with minSize 0 and a public address before the replica starts, whose snapshot
// reads it then; the blob is clean, so the malware gate passed on a decided state (Q-STOR-284); the caller is an administrator,
// whose profile Unlimited sets no figure (R-LIM-040).
func linkBudget(t *testing.T, e *env) {
	t.Helper()
	const repo = "oci-links"
	e.st.Repository(repo, "local", "oci", `{}`)
	endpoint, _ := url.Parse(testenv.S3Config(t).Endpoint)
	e.st.Exec(`UPDATE download_link_settings SET enabled = true, min_size = 0, public_addresses = jsonb_build_object('default', $1::text),
		version = version + 1`, "https://localhost:"+endpoint.Port())
	r := e.start(t, testenv.ReplicaOptions{Name: "web-links"})
	layer := []byte("a layer of a few bytes, handed to a link")
	sum := sha256.Sum256(layer)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if a := e.st.Do(r, "POST", "/v2/"+testenv.DefaultProject+"/"+repo+"/app/blobs/uploads/?digest="+digest, e.tokAdmin, layer,
		"Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
		t.Fatalf("the blob's upload: %d %s", a.Status, a.Body)
	}
	e.st.Exec(`INSERT INTO malware_scan (sha256, state, holder_repo_id, size, due, scanned_at)
		SELECT i.blob_sha256, 'clean', i.repo_id, i.size, false, now() FROM items i JOIN repositories r ON r.id = i.repo_id
		 WHERE r.key = $1 AND i.path = $2
		ON CONFLICT (sha256) DO UPDATE SET state = 'clean', due = false, scanned_at = now()`, repo, "app/_blobs/"+digest)
	path := "/v2/" + testenv.DefaultProject + "/" + repo + "/app/blobs/" + digest
	noFollow := &http.Client{
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       10 * time.Second,
	}
	get := func() (int, string) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, e.st.Origin(r.Addr("default"))+path, nil)
		req.Host = testenv.DefaultHost
		req.Header.Set("Authorization", "Bearer "+e.tokAdmin)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, resp.Header.Get("X-Request-Id")
	}
	// The link check of the replica's start runs beside its first readiness; the first link waits for it.
	testenv.WaitFor(t, "the first link", 30*time.Second, func() bool { code, _ := get(); return code == http.StatusTemporaryRedirect })
	got := e.perRequest(t, path, get, http.StatusTemporaryRedirect)
	total := 0
	for _, v := range got {
		total += v
	}
	t.Logf("statements per download by link: %d (%s)", total, describe(got))
	if total > 1 {
		t.Errorf("a download by link runs %d statements (%s), the budget of R-ARCH-027 is 1", total, describe(got))
	}
}
