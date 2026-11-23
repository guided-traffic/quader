//go:build integration

package bastion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
	"github.com/guided-traffic/quader/internal/bastion/server"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_BAS_074_ABastionReadsAHandedFileFromTheObjectStore is the Bastion's half of R-BAS-070's integration case: the role
// in one process, started from a configuration file, against the in-process main installation of maintest, which answers
// the forward of a cold GET below /files/ with a handover of a link the stack's object store signed (SeaweedFS over TLS).
// The Bastion offers, reads the file of 64 KiB from the store with the stack's CA in bastion.main.ca_file, keeps it and
// serves it; quader_bastion_fill_bytes_total{source="store"} rises by 65,536; a second GET is a hit. A second file whose
// link names a closed port ends in the fallback: the forward without the offer, whose bytes the main installation sends.
// The main installation that signs the link itself (storage.md R-STOR-095) joins this case with package 2 of the build plan
// (docs/developer/bastion-handover.md).
func TestR_BAS_074_ABastionReadsAHandedFileFromTheObjectStore(t *testing.T) {
	s := testenv.S3Config(t)
	ca := testenv.CAFile(t)
	ctx := context.Background()
	dir := t.TempDir()

	// The file in the stack's store, below a prefix of this run in the backups bucket.
	accessFile, secretFile := writeFile(t, dir, "access-key", s.AccessKey+"\n"), writeFile(t, dir, "secret-key", s.SecretKey+"\n")
	bucket := config.BastionCacheBucket{
		Endpoint: s.Endpoint, Region: s.Region, Bucket: s.BackupsBucket, ForcePathStyle: s.ForcePathStyle, CAFile: ca,
		Credentials: config.BastionBucketCredentials{Source: "static", AccessKeyIDFile: accessFile, SecretAccessKeyFile: secretFile},
	}
	raw, err := s3.NewFromFactory(ctx, bucket.StorageS3(), outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}}))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	file := make([]byte, 64<<10)
	for i := range file {
		file[i] = byte(i*13 + i>>10)
	}
	sum := sha256.Sum256(file)
	digest := hex.EncodeToString(sum[:])
	key := "it-bastionhandover/" + testenv.RandHex(5) + "/" + digest
	if err := raw.PutPlain(ctx, key, bytes.NewReader(file), int64(len(file)), sum[:]); err != nil {
		t.Fatalf("put: %v", err)
	}
	t.Cleanup(func() { _ = raw.Delete(context.Background(), key) })
	// The link names the store as localhost: the main installation of maintest is 127.0.0.1, and a link on a host of the
	// main installation is refused whatever its port (R-BAS-074). The stack's certificate covers both names.
	address := strings.Replace(s.Endpoint, "127.0.0.1", "localhost", 1)
	link, err := raw.Presign(ctx, key, blob.PresignOptions{Life: time.Minute, Address: address})
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	closed := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return "https://localhost:" + strconv.Itoa(port) + "/" + s.BackupsBucket + "/" + key + "?X-Amz-Signature=0"
	}()

	// The main installation: a reader of the repository web/generic, the registration's switch on, and the two files.
	const stored, unreachable = "/files/web/generic/handover/stored.bin", "/files/web/generic/handover/unreachable.bin"
	main := maintest.New(t)
	main.SetAnonymous(false)
	main.SetHandover(true)
	reader := maintest.GenerateTokenWithPrefix(t, "qdr_pat_")
	sub := iface.Subject{Kind: iface.SubjectToken, TokenID: "tok-1", PrincipalID: "p-1"}
	main.SetCheck(func(req iface.CheckRequest) iface.CheckAnswer {
		out := iface.CheckAnswer{Subject: iface.Subject{Kind: iface.SubjectInvalid}, Repositories: map[string]iface.RepositoryRead{}}
		if req.Credential != nil && *req.Credential == reader {
			out.Subject = sub
			for _, r := range req.Repositories {
				out.Repositories[r] = iface.RepositoryRead{Read: iface.ReadFull}
			}
		}
		return out
	})
	main.SetForward(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != reader {
			main.Mark(w, iface.Marking{Class: iface.ClassPass, Subject: iface.Subject{Kind: iface.SubjectInvalid}})
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mk := iface.Marking{Class: iface.ClassArtifact, Repository: "web/generic", Read: iface.ReadFull, Subject: sub, Counted: true}
		w.Header().Set("Etag", `"`+digest+`"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		h := iface.Handover{Link: link.URL, ExpiresAt: link.ExpiresAt, Connection: "default", SHA256: digest, Size: int64(len(file)), StoredSize: int64(len(file))}
		if r.URL.Path == unreachable {
			h.Link = closed
		}
		if main.HandOver(w, r, mk, h) {
			return
		}
		main.Mark(w, mk)
		w.Header().Set("Content-Length", strconv.Itoa(len(file)))
		_, _ = w.Write(file)
	})

	// The Bastion, whose one bundle trusts the main installation and the store (bastion.main.ca_file, Q-BAS-237).
	stackCA, err := os.ReadFile(ca)
	if err != nil {
		t.Fatal(err)
	}
	mainCA, err := os.ReadFile(main.CAFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	bundle := writeFile(t, dir, "ca-bundle.crt", string(mainCA)+string(stackCA))
	tokenFile := writeFile(t, dir, "token", main.NewToken()+"\n")
	cfgFile := writeFile(t, dir, "quader.yaml", fmt.Sprintf(`server:
  listen: 127.0.0.1:0
  ops_listen: localhost:0
  base_url: %s
bastion:
  replicas: 1
  main:
    url: %s
    token_file: %s
    ca_file: %s
    handshake_interval: 5s
    connect_timeout: 1s
    response_header_timeout: 5s
  feed:
    poll_interval: 1s
  audit:
    flush_interval: 1h
  cache:
    backend: volume
    volume:
      dir: %s/cache
`, baseURL, main.URL(), tokenFile, bundle, dir))
	loaded, err := config.LoadBastion(config.Options{File: cfgFile, Command: config.CommandServe})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	logs := &syncBuf{}
	srv, err := server.New(server.Options{Loaded: loaded, Environ: []string{"QUADER_POD_NAME=bastion-handover"}, LogWriter: logs, DrainDelay: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("server: %v\n%s", err, logs)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("start: %v\n%s", err, logs)
	}
	t.Cleanup(func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown: %v", err)
		}
		if t.Failed() {
			t.Logf("log of the Bastion:\n%s", testenv.Tail(logs.String(), 40))
		}
	})
	c := &http.Client{Timeout: 20 * time.Second}
	client, ops := "http://"+srv.ClientAddr().String(), "http://"+srv.OpsAddr().String()
	scrape := func() string {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ops+"/metrics", nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	testWait(t, "the Bastion ready, connected and told the handover is on", func() bool {
		m := scrape()
		return srv.Health().Ready() && strings.Contains(m, "\nquader_bastion_main_reachable 1\n") && strings.Contains(m, "\nquader_bastion_handover_enabled 1\n")
	})
	get := func(path string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, client+path, nil)
		req.Host = baseHost
		req.Header.Set("Authorization", "Bearer "+reader)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return resp.StatusCode, b
	}

	// A cold GET: the forward offers, the main installation hands over, the Bastion reads the store.
	if code, body := get(stored); code != http.StatusOK || !bytes.Equal(body, file) {
		t.Fatalf("the cold GET: %d, %d bytes\n%s", code, len(body), testenv.Tail(logs.String(), 20))
	}
	fw := main.Forwards()
	if len(fw) != 1 {
		t.Fatalf("the cold GET cost %d forwards, want 1", len(fw))
	}
	if offer, ok := fw[0].Offer(); !ok || offer.MaxSize < int64(len(file)) {
		t.Fatalf("the forward's offer: %+v %v", offer, ok)
	}
	m := scrape()
	if v := metricValue(t, m, `quader_bastion_fill_bytes_total{source="store"}`); v != float64(len(file)) {
		t.Fatalf("fill bytes from the store: %v, want %d", v, len(file))
	}
	if v := metricValue(t, m, `quader_bastion_handovers_total{outcome="read"}`); v != 1 {
		t.Fatalf("handovers read: %v", v)
	}
	if v := metricValue(t, m, `quader_bastion_fill_bytes_total{source="main"}`); v != 0 {
		t.Fatalf("fill bytes from the main installation: %v", v)
	}
	// A second GET is a hit.
	if code, body := get(stored); code != http.StatusOK || !bytes.Equal(body, file) || len(main.Forwards()) != 1 {
		t.Fatalf("the hit: %d, %d bytes, %d forwards", code, len(body), len(main.Forwards()))
	}

	// A link on a closed port: the fallback, the bytes through the main installation.
	if code, body := get(unreachable); code != http.StatusOK || !bytes.Equal(body, file) {
		t.Fatalf("the fallback: %d, %d bytes", code, len(body))
	}
	fw = main.Forwards()
	if len(fw) != 3 {
		t.Fatalf("the fallback cost %d forwards in all, want 3", len(fw))
	}
	_, first := fw[1].Offer()
	_, second := fw[2].Offer()
	if !first || second {
		t.Fatalf("the fallback's forwards: offered %v, then %v", first, second)
	}
	m = scrape()
	if metricValue(t, m, `quader_bastion_handovers_total{outcome="fallback"}`) != 1 || metricValue(t, m, "quader_bastion_handover_cooldown") != 1 {
		t.Fatalf("after the fallback: fallback %v, cooldown %v", metricValue(t, m, `quader_bastion_handovers_total{outcome="fallback"}`), metricValue(t, m, "quader_bastion_handover_cooldown"))
	}
	// The request log names the outcome, and no log line holds the link.
	testWait(t, "the request log's bastion_handover", func() bool {
		l := logs.String()
		return strings.Contains(l, `"bastion_handover":"read"`) && strings.Contains(l, `"bastion_handover":"fallback"`)
	})
	if strings.Contains(logs.String(), "X-Amz-Signature") || strings.Contains(logs.String(), key) {
		t.Fatal("a log line of the Bastion holds the link")
	}
}
