// Command shippedanchors refreshes the release's snapshot of the public Sigstore TUF repository (signatures.md R-SIG-017): it runs the
// refresh of internal/signatures/shipped against the repository from the TUF root sigstore-go embeds, through a client that records
// every document the repository serves, and writes the two targets (trusted_root.json and registry.npmjs.org/keys.json), the TUF
// metadata the snapshot test verifies them with, and snapshot.json (the repository, the TUF snapshot's version, the time) below
// internal/signatures/shipped/data. `make shipped-anchors` runs it; the result is committed. nuget.org's certificates and the Debian and
// Ubuntu keyrings are files of their own and are not touched.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
)

func main() {
	out := flag.String("out", "internal/signatures/shipped/data", "the data directory of internal/signatures/shipped")
	url := flag.String("url", shipped.DefaultURL, "the TUF repository")
	flag.Parse()
	if err := run(*out, *url); err != nil {
		fmt.Fprintln(os.Stderr, "shippedanchors:", err)
		os.Exit(1)
	}
}

func run(out, url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	factory := outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true, MinVersion: "1.2"}})
	if err := factory.Err(); err != nil {
		return fmt.Errorf("outbound client: %w", err)
	}
	client := factory.Client(outbound.PurposeSignatureTrustRoot, outbound.WithTimeout(time.Minute))
	rec := &recorder{next: client.Transport, files: map[string][]byte{}}
	client.Transport = rec
	now := time.Now().UTC().Truncate(time.Second)
	got, err := shipped.Refresh(ctx, shipped.RefreshOptions{URL: url, Client: client, Now: now})
	if err != nil {
		return err
	}
	if err := writeTree(out, url, rec.files); err != nil {
		return err
	}
	meta, err := json.MarshalIndent(map[string]any{"url": url, "tufVersion": got.TUFVersion, "builtAt": now}, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(out, "snapshot.json"), append(meta, '\n'), 0o600); err != nil {
		return fmt.Errorf("snapshot.json: %w", err)
	}
	fmt.Printf("TUF snapshot version %d of %s, read %s\n", got.TUFVersion, url, now.Format(time.RFC3339))
	return nil
}

// hashedTarget is a target's file name under consistent snapshots: the SHA-256 in hex, a dot and the name.
var hashedTarget = regexp.MustCompile(`^[0-9a-f]{64}\.`)

// writeTree replaces data/tuf and data/targets with what the refresh read: a metadata file at the path the repository served it, a
// target at its name without the hash prefix.
func writeTree(out, base string, files map[string][]byte) error {
	for _, dir := range []string{"tuf", "targets"} {
		if err := os.RemoveAll(filepath.Join(out, dir)); err != nil {
			return fmt.Errorf("clear %s: %w", dir, err)
		}
	}
	names := make([]string, 0, len(files))
	for u := range files {
		names = append(names, u)
	}
	sort.Strings(names)
	for _, u := range names {
		rel, ok := strings.CutPrefix(u, strings.TrimSuffix(base, "/")+"/")
		if !ok {
			return fmt.Errorf("%s is outside %s", u, base)
		}
		dest := filepath.Join(out, "tuf", filepath.FromSlash(rel))
		if t, isTarget := strings.CutPrefix(rel, "targets/"); isTarget {
			dir, file := filepath.Split(t)
			dest = filepath.Join(out, "targets", filepath.FromSlash(dir), hashedTarget.ReplaceAllString(file, ""))
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		if err := os.WriteFile(dest, files[u], 0o600); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		fmt.Println("wrote", dest)
	}
	return nil
}

// recorder is a transport that keeps the body of every answer 200 by its URL.
type recorder struct {
	next  http.RoundTripper
	mu    sync.Mutex
	files map[string][]byte
}

// RoundTrip implements http.RoundTripper.
func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := r.next.RoundTrip(req)
	if err != nil || res.StatusCode != http.StatusOK {
		return res, err
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", req.URL, err)
	}
	r.mu.Lock()
	r.files[req.URL.String()] = b
	r.mu.Unlock()
	res.Body = io.NopCloser(bytes.NewReader(b))
	res.ContentLength = int64(len(b))
	return res, nil
}
