// Command e4 is the tool of the measurement W34-E4 (docs/developer/bastion.md, "What a client's command costs through a
// Bastion"): it fills the main installation with generated content (seed), sums the request records of the two processes over a
// window (tally), answers the Bastion's specified refusals for the clients (stub) and asks a Bastion for distinct artifacts
// faster than its cache holds them (flood). It is a measurement aid, run by hack/bastion-measure/run.sh, never part of the
// product.
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: e4 seed|tally|stub|flood|latency [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(os.Args[2:])
	case "tally":
		err = tally(os.Args[2:])
	case "stub":
		err = stub(os.Args[2:])
	case "flood":
		err = flood(os.Args[2:])
	case "latency":
		err = latency(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "e4:", err)
		os.Exit(1)
	}
}

// client is an HTTP client of the main installation or the Bastion: its base URL, the CA that verifies it and a credential.
type client struct {
	base  string
	host  string // the Host of every request when not the base URL's (a Bastion named by its base URL's host)
	token string
	hc    *http.Client
}

func newClient(base, caFile, token string, timeout time.Duration) (*client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile) //nolint:gosec // the CA file of the measurement, named by the caller
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificate in " + caFile)
		}
	}
	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		MaxIdleConnsPerHost: 256,
		ForceAttemptHTTP2:   true,
	}
	return &client{base: strings.TrimRight(base, "/"), token: token, hc: &http.Client{Transport: tr, Timeout: timeout}}, nil
}

// answer is a read answer: the status, the header and the body.
type answer struct {
	Status int
	Header http.Header
	Body   []byte
}

// do sends a request with the client's token as Bearer (none when empty); hdr are name, value pairs.
func (c *client) do(method, path string, body []byte, hdr ...string) (answer, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rd) //nolint:noctx // a measurement tool, bounded by the client's timeout
	if err != nil {
		return answer{}, err
	}
	if c.host != "" {
		req.Host = c.host
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return answer{}, err
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	return answer{Status: res.StatusCode, Header: res.Header, Body: b}, err
}

// must is do that fails unless the status is one of want.
func (c *client) must(what, method, path string, body []byte, want []int, hdr ...string) (answer, error) {
	for attempt := 0; ; attempt++ {
		a, err := c.do(method, path, body, hdr...)
		if err != nil {
			return a, fmt.Errorf("%s: %w", what, err)
		}
		// A busy machine answers a write of the seed 503 database_timeout now and then; the seed is not what is measured.
		if (a.Status == http.StatusTooManyRequests || a.Status == http.StatusServiceUnavailable) && attempt < 30 {
			time.Sleep(time.Second)
			continue
		}
		for _, w := range want {
			if a.Status == w {
				return a, nil
			}
		}
		return a, fmt.Errorf("%s: %s %s answered %d: %.400s", what, method, path, a.Status, a.Body)
	}
}

func readToken(file string) (string, error) {
	if file == "" {
		return "", nil
	}
	b, err := os.ReadFile(file) //nolint:gosec // a token file of the measurement, named by the caller
	return strings.TrimSpace(string(b)), err
}
