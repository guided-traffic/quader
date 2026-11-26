//go:build integration

package querybudget

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// handoverBudget is R-ARCH-027 for a download handed over to a Bastion (storage.md R-STOR-095): the registration's switch is read
// from the replica's snapshot, the offer from the recognition, the copies come with the item lookup and signing is local, so a
// handover costs the item lookup alone, as a stream and a link do. The download is an OCI blob of a recognized forward that offers
// a handover, with the caller's credential an administrator's (the profile Unlimited sets no figure); the links' minSize is 0 and
// the connection's public address set before the replica starts, the registration and its switch are made through the API.
func handoverBudget(t *testing.T, e *env) {
	t.Helper()
	const repo = "oci-handover"
	e.st.Repository(repo, "local", "oci", `{}`)
	endpoint, _ := url.Parse(testenv.S3Config(t).Endpoint)
	e.st.Exec(`UPDATE download_link_settings SET min_size = 0, public_addresses = jsonb_build_object('default', $1::text), version = version + 1`,
		"https://localhost:"+endpoint.Port())
	r := e.start(t, testenv.ReplicaOptions{Name: "web-handover"})
	if a := e.st.Do(r, "POST", "/api/v1/bastions", e.tokAdmin, map[string]any{"name": "dmz"}); a.Status != http.StatusCreated {
		t.Fatalf("register: %d %s", a.Status, a.Body)
	}
	a := e.st.Do(r, "POST", "/api/v1/bastions/dmz/tokens", e.tokAdmin, map[string]any{})
	bst, _ := a.JSON(t)["token"].(string)
	if a.Status != http.StatusCreated || bst == "" {
		t.Fatalf("token: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(r, "PATCH", "/api/v1/bastions/dmz", e.tokAdmin, map[string]any{"switches": map[string]any{"handover": true}},
		"Content-Type", "application/merge-patch+json", "If-Match", "*"); a.Status != http.StatusOK {
		t.Fatalf("switch on: %d %s", a.Status, a.Body)
	}
	layer := []byte("a layer of a few bytes, handed over to a Bastion")
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
	forward := []string{
		iface.HeaderAuthorization, "Bearer " + bst, iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion),
		iface.HeaderHandover, iface.HandoverOffer{MaxSize: 1 << 20}.String(),
	}
	handedOver := func() bool {
		a := e.st.Do(r, "GET", path, e.tokAdmin, nil, forward...)
		return a.Status == http.StatusOK && a.Header.Get(iface.HeaderClass) == string(iface.ClassHandover) && len(a.Body) == 0
	}
	// The snapshot follows the switch within 2 s; the link check of the replica's start runs beside its first readiness.
	testenv.WaitFor(t, "the first handover", 15*time.Second, handedOver)
	got := perRequestAs(t, e, r, path, e.tokAdmin, forward...)
	if !handedOver() {
		t.Fatal("the downloads of the window were not handed over")
	}
	total := 0
	for _, v := range got {
		total += v
	}
	t.Logf("statements per download handed over: %d (%s)", total, describe(got))
	if total > 1 {
		t.Errorf("a download handed over runs %d statements (%s), the budget of R-ARCH-027 is 1", total, describe(got))
	}
}
