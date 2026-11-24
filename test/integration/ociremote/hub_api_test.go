//go:build integration

package ociremote

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// hubAPIStub answers as Docker Hub's API (hub.docker.com), the catalogue host of the Docker Hub preset (R-CUR-008): the tags of the
// stub corpus's image acme/a with a size each, newest first, on one page. Any other path answers 404. It counts its requests.
type hubAPIStub struct {
	addr     string
	requests atomic.Int64
}

// hubTags are the tags the stub's API lists of acme/a, as the registry stub holds them.
var hubTags = []string{"2.0", "1.2", "1.1", "1.0", "latest"}

func startHubAPI(t *testing.T, cfg *tls.Config) *hubAPIStub {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := &hubAPIStub{addr: ln.Addr().String()}
	srv := &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return s
}

func (s *hubAPIStub) serve(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	if r.URL.Path != "/v2/namespaces/acme/repositories/a/tags" {
		http.NotFound(w, r)
		return
	}
	type tag struct {
		Name        string `json:"name"`
		FullSize    int64  `json:"full_size"`
		LastUpdated string `json:"last_updated"`
	}
	out := struct {
		Count   int     `json:"count"`
		Next    *string `json:"next"`
		Results []tag   `json:"results"`
	}{Count: len(hubTags)}
	for _, name := range hubTags {
		out.Results = append(out.Results, tag{Name: name, FullSize: 1024, LastUpdated: "2026-11-01T00:00:00Z"})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
