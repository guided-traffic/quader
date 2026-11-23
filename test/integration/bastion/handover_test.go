//go:build integration

package bastion

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_STOR_095_AForwardThatOfferedIsHandedOverFromTheStacksStore is the main installation's side of R-BAS-070 against the
// stack's object store (package 2 of the handover; the Bastion's read is package 3's): a recognized forward of a file of 64 KiB
// below /files/ that offers to take a handover is answered, while the registration's switch is off, with the file's bytes, and
// once the switch is on with the file's 200 without its bytes and the class handover, whose link a plain client without a
// credential reads the bytes from at the store; the request log's line says delivery handover with no response bytes, and the
// metric counts the handover. An ordinary request that sends the offer's header gets the file and no marking. The main
// installation is the web role of the stack in this process, with the links' minSize 0 and the connection's public address on
// localhost (the store's certificate names it).
func TestR_STOR_095_AForwardThatOfferedIsHandedOverFromTheStacksStore(t *testing.T) {
	const (
		repo = "generic-handover"
		path = "/files/" + testenv.DefaultProject + "/" + repo + "/dist/big.bin"
	)
	st := testenv.NewStack(t, testenv.StackOptions{Name: "hand"})
	st.Repository(repo, "local", "generic", `{}`)
	ops := st.User("ops")
	st.Bind("admin", ops)
	admin := st.Token(ops, scope.Scope{Inherit: true, Admin: true})
	endpoint, err := url.Parse(testenv.S3Config(t).Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	st.Exec(`UPDATE download_link_settings SET min_size = 0, public_addresses = jsonb_build_object('default', $1::text), version = version + 1`,
		"https://localhost:"+endpoint.Port())
	main := st.Start(t, testenv.ReplicaOptions{Name: "web-1"})

	if a := st.Do(main, "POST", "/api/v1/bastions", admin, map[string]any{"name": "dmz"}); a.Status != http.StatusCreated {
		t.Fatalf("register: %d %s", a.Status, a.Body)
	}
	a := st.Do(main, "POST", "/api/v1/bastions/dmz/tokens", admin, map[string]any{})
	bst, _ := a.JSON(t)["token"].(string)
	if a.Status != http.StatusCreated || bst == "" {
		t.Fatalf("token: %d %s", a.Status, a.Body)
	}
	data := make([]byte, 64<<10)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if a := st.Do(main, "PUT", path, admin, data, "Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
		t.Fatalf("deploy: %d %s", a.Status, a.Body)
	}
	sum := sha256.Sum256(data)
	// The malware scanning is on by default: a clean verdict makes the gate pass on a decided state (R-STOR-095 step 3).
	st.Exec(`INSERT INTO malware_scan (sha256, state, holder_repo_id, size, due, scanned_at)
		SELECT i.blob_sha256, 'clean', i.repo_id, i.size, false, now() FROM items i JOIN repositories r ON r.id = i.repo_id
		 WHERE r.key = $1 AND i.path = $2
		ON CONFLICT (sha256) DO UPDATE SET state = 'clean', due = false, scanned_at = now()`, repo, "dist/big.bin")

	forward := func() testenv.Answer {
		t.Helper()
		return st.Do(main, "GET", path, admin, nil,
			iface.HeaderAuthorization, "Bearer "+bst, iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion),
			iface.HeaderHandover, iface.HandoverOffer{MaxSize: 1 << 20}.String())
	}
	t.Run("R-BAS-071 with the switch off the forward is streamed", func(t *testing.T) {
		a := forward()
		if a.Status != http.StatusOK || !bytes.Equal(a.Body, data) || a.Header.Get(iface.HeaderClass) != string(iface.ClassArtifact) ||
			a.Header.Get(iface.HeaderHandover) != "" {
			t.Fatalf("%d, %d bytes, class %q", a.Status, len(a.Body), a.Header.Get(iface.HeaderClass))
		}
	})
	if a := st.Do(main, "PATCH", "/api/v1/bastions/dmz", admin, map[string]any{"handover": true},
		"Content-Type", "application/merge-patch+json", "If-Match", "*"); a.Status != http.StatusOK || a.JSON(t)["handover"] != true {
		t.Fatalf("switch on: %d %s", a.Status, a.Body)
	}
	var handed testenv.Answer
	// The replica's snapshot follows the change area within 2 s; the link check of its start runs beside its first readiness.
	testenv.WaitFor(t, "the first handover", 15*time.Second, func() bool {
		handed = forward()
		return handed.Header.Get(iface.HeaderClass) == string(iface.ClassHandover)
	})
	t.Run("R-STOR-095 a forward that offered is answered by the file's 200 without its bytes and the class handover", func(t *testing.T) {
		if handed.Status != http.StatusOK || len(handed.Body) != 0 || handed.Header.Get("Content-Length") != "0" ||
			handed.Header.Get(iface.HeaderCounted) != "download" {
			t.Fatalf("%d, %d bytes, headers %v", handed.Status, len(handed.Body), handed.Header)
		}
		h, err := iface.DecodeHandover(handed.Header.Get(iface.HeaderHandover))
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Check(iface.HandoverCheck{
			Now: time.Now(), Offer: iface.HandoverOffer{MaxSize: 1 << 20}, ETag: handed.Header.Get("ETag"),
			RefusedHosts: []string{testenv.DefaultHost},
		}); err != nil {
			t.Fatalf("the Bastion would refuse the value: %v", err)
		}
		if h.SHA256 != hex.EncodeToString(sum[:]) || h.Size != int64(len(data)) || h.Encrypted || h.Connection != "default" {
			t.Fatalf("value %v", h)
		}
		got := fetchPlain(t, h.Link)
		if !bytes.Equal(got, data) {
			t.Fatalf("the link read %d bytes, not the file's", len(got))
		}
	})
	t.Run("R-STOR-095 the request log says delivery handover, and the metric counts it", func(t *testing.T) {
		testenv.WaitFor(t, "the request line", 5*time.Second, func() bool {
			for _, line := range strings.Split(main.Logs.String(), "\n") {
				if strings.Contains(line, `"request completed"`) && strings.Contains(line, `"delivery":"handover"`) &&
					strings.Contains(line, `"response_bytes":0`) && strings.Contains(line, "/dist/big.bin") {
					return true
				}
			}
			return false
		})
		if m := main.Metrics(t); !strings.Contains(m, `quader_storage_download_handovers_total{bastion="dmz",kind="link"}`) {
			t.Fatal("quader_storage_download_handovers_total has no series of the registration")
		}
	})
	t.Run("R-BAS-073 an ordinary request that sends the offer gets the file and no marking", func(t *testing.T) {
		a := st.Do(main, "GET", path, admin, nil, iface.HeaderHandover, iface.HandoverOffer{MaxSize: 1 << 20}.String())
		if a.Status != http.StatusOK || !bytes.Equal(a.Body, data) {
			t.Fatalf("%d, %d bytes", a.Status, len(a.Body))
		}
		for k := range a.Header {
			if strings.HasPrefix(http.CanonicalHeaderKey(k), iface.HeaderPrefix) {
				t.Errorf("the answer carries %s", k)
			}
		}
	})
}

// fetchPlain reads a link as the Bastion's link client will: a client of its own that trusts the stack's CA, sends no credential
// and follows no redirect.
func fetchPlain(t *testing.T, link string) []byte {
	t.Helper()
	pem, err := os.ReadFile(testenv.CAFile(t))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("the stack's CA does not parse")
	}
	c := &http.Client{
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       10 * time.Second,
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, link, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("the link: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the link answered %d", resp.StatusCode)
	}
	return body
}
