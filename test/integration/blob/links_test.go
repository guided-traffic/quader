//go:build integration

package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_STOR_014_TheSwitchReachesEveryReplica: the switch written through the store of the management API on one replica reaches
// another replica within 2 s with Valkey stopped (the change log in PostgreSQL carries it): a GET of an OCI blob there answers 307
// to a link of the stack's store under the public address, which a plain HTTP client without Quader's credential fetches with the
// blob's bytes and the overridden headers; switched off, the next GET there streams the bytes within 2 s, and the change carries
// the journal entry (R-STOR-073 for the request log's delivery).
func TestR_STOR_014_TheSwitchReachesEveryReplica(t *testing.T) {
	// The stack runs no malware scanner, so a blob would stay without a verdict, which keeps it from links (Q-STOR-284): the
	// scanning is off here, and the gate's part of the decision is the unit test's.
	st := testenv.NewStack(t, testenv.StackOptions{Name: "links", Env: []string{"QUADER_MALWARE_ENABLED=false"}})
	rita := st.User("rita")
	st.Bind("admin", rita)
	tok := st.Token(rita, scope.Scope{Inherit: true, Admin: true}) // the profile Unlimited: no pace figure (R-LIM-040)
	st.Repository("oci-local", "local", "oci", `{}`)
	st.Policy("writers", []string{"read", "deploy"}, []string{"oci-local"}, rita)
	web1 := st.Start(t, testenv.ReplicaOptions{Name: "web1"})
	web2 := st.Start(t, testenv.ReplicaOptions{Name: "web2"})

	// A blob of a few bytes, uploaded in one request: oci.blob.get declares that the clients check the digest in its path (R-STOR-071).
	layer := []byte("a layer of a few bytes, handed to a link")
	sum := sha256.Sum256(layer)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if a := st.Do(web1, "POST", "/v2/"+testenv.DefaultProject+"/oci-local/app/blobs/uploads/?digest="+digest, tok, layer,
		"Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
		t.Fatalf("the blob's upload: %d %s", a.Status, a.Body)
	}
	path := "/v2/" + testenv.DefaultProject + "/oci-local/app/blobs/" + digest

	// The store as the management API writes through it: the change area and the audit event in the transaction of the change.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := postgres.Open(ctx, st.DatabaseConfig(), "links-api", "web", testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	changes, err := cluster.NewChanges(cluster.ChangesOptions{DB: db.ControlPool(), Config: config.Cluster{}, WatchdogTimeout: 30 * time.Second, Log: testenv.Quiet(), Metrics: prometheus.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := audit.New(audit.Options{DB: db, Replica: "links-api", Config: config.Audit{AsyncBuffer: 100}, Log: testenv.Quiet(), AppLog: testenv.Quiet()})
	if err != nil {
		t.Fatal(err)
	}
	store := downloadlinks.NewStore(downloadlinks.StoreOptions{Runner: db, Changes: changes, Audit: rec, Log: testenv.Quiet()})
	cur, err := store.Settings(ctx)
	if err != nil || cur.Enabled {
		t.Fatalf("a new installation: %+v %v", cur, err)
	}
	// The stack's store answers on 127.0.0.1 and localhost with one certificate; localhost is no host of the replicas
	// (server.base_url http://quader.test), so a client drops its credential on the redirect.
	endpoint, _ := url.Parse(testenv.S3Config(t).Endpoint)
	public := "https://localhost:" + endpoint.Port()
	on := cur
	on.Enabled, on.MinSize, on.PublicAddresses = true, 0, map[string]string{config.DefaultConnectionID: public}
	if _, err := store.Update(ctx, audit.Actor{Kind: "user", Name: "rita"}, on, cur.Version); err != nil {
		t.Fatal(err)
	}

	noFollow := &http.Client{
		Transport:     &http.Transport{TLSClientConfig: testenv.TLSConfig(t)},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       10 * time.Second,
	}
	get := func(r *testenv.Replica) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, st.Origin(r.Addr("default"))+path, nil)
		req.Host = testenv.DefaultHost
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	var location string
	start, status := time.Now(), 0
	for time.Since(start) < 2*time.Second+500*time.Millisecond {
		resp := get(web2)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		location, status = resp.Header.Get("Location"), resp.StatusCode
		if status == http.StatusTemporaryRedirect {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	took := time.Since(start)
	if status != http.StatusTemporaryRedirect {
		var lines []string
		for _, l := range strings.Split(web2.Metrics(t), "\n") {
			if strings.HasPrefix(l, "quader_storage_download_link") {
				lines = append(lines, l)
			}
		}
		t.Fatalf("no 307 from the other replica within 2.5 s: %d; its metrics: %v", status, lines)
	}
	t.Logf("the switch reached the other replica in %s", took)
	if took > 2*time.Second {
		t.Errorf("the switch took %s to reach the other replica, the bound is 2 s (R-ARCH-018)", took)
	}
	if !strings.HasPrefix(location, public+"/") || !strings.Contains(location, "X-Amz-Signature=") {
		t.Fatalf("the link %q does not name the public address", location)
	}

	t.Run("R-STOR-072 a plain client without Quader's credential fetches the blob by the link", func(t *testing.T) {
		plain := &http.Client{Transport: &http.Transport{TLSClientConfig: testenv.TLSConfig(t)}, Timeout: 10 * time.Second}
		resp, err := plain.Get(location)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, layer) {
			t.Fatalf("the link answered %d with %d bytes", resp.StatusCode, len(body))
		}
		if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control = %q, want private, no-store", got)
		}
		if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment; filename") || !strings.Contains(got, hex.EncodeToString(sum[:])) {
			t.Errorf("Content-Disposition = %q, want an attachment named after the blob", got)
		}
	})

	cur, err = store.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	off := cur
	off.Enabled = false
	if _, err := store.Update(ctx, audit.Actor{Kind: "user", Name: "rita"}, off, cur.Version); err != nil {
		t.Fatal(err)
	}
	took = testenv.WaitFor(t, "the bytes from the other replica", 2*time.Second+500*time.Millisecond, func() bool {
		resp := get(web2)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK && bytes.Equal(body, layer)
	})
	if took > 2*time.Second {
		t.Errorf("switching off took %s to reach the other replica, the bound is 2 s", took)
	}
	t.Run("R-STOR-014 switching off is journaled and switching on is not", func(t *testing.T) {
		if n := st.Number(`SELECT count(*) FROM revocation_outbox WHERE event = $1`, audit.EventStorageDownloadLinksChanged); n != 1 {
			t.Fatalf("%d journal entries, want the one of switching off", n)
		}
		if n := st.Number(`SELECT count(*) FROM audit_event WHERE action = $1`, audit.EventStorageDownloadLinksChanged); n != 2 {
			t.Fatalf("%d audit events, want 2", n)
		}
	})
}
