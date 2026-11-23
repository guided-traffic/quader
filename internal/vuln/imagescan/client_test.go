package imagescan

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// scannerStub is the scanner's listener behind TLS: it answers what the test sets and counts the connections it accepts.
type scannerStub struct {
	srv    *httptest.Server
	conns  atomic.Int64
	calls  atomic.Int64
	answer func(w http.ResponseWriter, r *http.Request)
}

func newScannerStub(t *testing.T) *scannerStub {
	t.Helper()
	s := &scannerStub{}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		s.answer(w, r)
	}))
	s.srv.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			s.conns.Add(1)
		}
	}
	s.srv.StartTLS()
	t.Cleanup(s.srv.Close)
	return s
}

func (s *scannerStub) client(t *testing.T) *Client {
	t.Helper()
	pool := s.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	c, err := NewClient(ClientOptions{
		URL: s.srv.URL, Deadline: 5 * time.Second,
		TLS:   func() *tls.Config { return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12} },
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func problem(status int, code string, terminal bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(CallProblem{Code: code, Detail: "d", Terminal: terminal})
	}
}

func testCall() Call {
	return Call{Repository: qname.Repo{Project: "web", Key: "docker-local"}, Name: "app", Digest: "sha256:" + strings.Repeat("ab", 32), ScanID: "sid"}
}

func TestR_VULN_041_EveryAnswerOfTheScannerMapsToItsCodeAttemptAndWait(t *testing.T) {
	analysis := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", AnalysisMediaType)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set(AnalyserVersionHeader, "0.76.2")
		zw := gzip.NewWriter(w)
		_, _ = zw.Write([]byte(`{"bomFormat":"CycloneDX","specVersion":"1.6"}`))
		_ = zw.Close()
	}
	cases := []struct {
		name     string
		answer   func(w http.ResponseWriter, r *http.Request)
		code     string
		spent    bool // an attempt is spent: no Defer
		terminal bool
		wait     time.Duration
	}{
		{"422 with a terminal code of the image", problem(422, CodePathTraversal, true), CodePathTraversal, true, true, 0},
		{"422 with a code that may pass", problem(422, CodeLayerMissing, false), CodeLayerMissing, true, false, 0},
		{"422 spool_too_small", problem(422, CodeSpoolTooSmall, false), CodeSpoolTooSmall, false, false, time.Hour},
		{"503 busy", problem(503, CodeScannerBusy, false), CodeScannerBusy, false, false, time.Minute},
		{"503 draining", problem(503, CodeScannerDraining, false), CodeScannerBusy, false, false, time.Minute},
		{"502", problem(502, CodeRegistryUnavailable, false), CodeRegistryUnavailable, false, false, time.Minute},
		{"403", problem(403, CodePullRefused, false), CodePullRefused, false, false, 5 * time.Minute},
		{"Q-VULN-237 503 scanner_disk_failed", problem(503, CodeScannerDiskFailed, false), CodeScannerDiskFailed, false, false, time.Minute},
		{"Q-VULN-226 409 repository_disabled", problem(409, CodeRepositoryDisabled, false), CodeRepositoryDisabled, false, false, time.Minute},
		{"an answer that is no analysis", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(AnalyserVersionHeader, "0.76.2")
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("not gzip"))
		}, CodeAnalyserFailed, true, false, 0},
		{"an analysis without the analyser version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{}")) }, CodeAnalyserFailed, true, false, 0},
		{"an unknown code", problem(422, "rm -rf", true), CodeAnalyserFailed, true, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newScannerStub(t)
			s.answer = tc.answer
			_, err := s.client(t).Analyse(t.Context(), testCall())
			var se *ScanError
			if !errors.As(err, &se) || se.Code != tc.code || se.Terminal != tc.terminal || se.Defer != tc.wait || (se.Defer == 0) != tc.spent {
				t.Fatalf("%v (%+v)", err, se)
			}
		})
	}
	t.Run("200 is the analysis with its version", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = analysis
		a, err := s.client(t).Analyse(t.Context(), testCall())
		if err != nil || a.AnalyserVersion != "0.76.2" || !bytes.Contains(a.Document, []byte("CycloneDX")) {
			t.Fatalf("%+v %v", a, err)
		}
	})
	t.Run("Q-VULN-162 a busy answer is tried three times, each on a new connection, before the scan is put off", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = problem(503, CodeScannerBusy, false)
		_, _ = s.client(t).Analyse(t.Context(), testCall())
		if s.calls.Load() != 3 || s.conns.Load() != 3 {
			t.Fatalf("%d calls on %d connections", s.calls.Load(), s.conns.Load())
		}
	})
	t.Run("Q-VULN-170 every call opens a connection of its own", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = analysis
		c := s.client(t)
		for range 4 {
			if _, err := c.Analyse(t.Context(), testCall()); err != nil {
				t.Fatal(err)
			}
		}
		if s.conns.Load() != 4 || c.Connections() != 4 {
			t.Fatalf("%d connections for 4 calls", s.conns.Load())
		}
	})
	t.Run("no scanner: scanner_unavailable without an attempt", func(t *testing.T) {
		c, err := NewClient(ClientOptions{URL: "https://127.0.0.1:1", Deadline: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Analyse(t.Context(), testCall())
		var se *ScanError
		if !errors.As(err, &se) || se.Code != CodeScannerUnavailable || se.Defer != time.Minute {
			t.Fatalf("%v", err)
		}
	})
	t.Run("a refused handshake: scanner_unavailable without an attempt", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = analysis
		c, err := NewClient(ClientOptions{URL: s.srv.URL, Deadline: time.Second, TLS: func() *tls.Config { return &tls.Config{MinVersion: tls.VersionTLS12} }})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Analyse(t.Context(), testCall())
		var se *ScanError
		if !errors.As(err, &se) || se.Code != CodeScannerUnavailable {
			t.Fatalf("%v", err)
		}
	})
	t.Run("a scanner that refuses the worker's certificate: scanner_unavailable, no attempt, also under TLS 1.3", func(t *testing.T) {
		// Under TLS 1.3 the server checks the client's certificate after the client's side of the handshake is done, so the
		// refusal arrives as an alert when the request is already written: it is still a refused handshake, not a lost call.
		s := &scannerStub{answer: problem(200, "", false)}
		s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.answer(w, r) }))
		s.srv.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS13}
		s.srv.StartTLS()
		t.Cleanup(s.srv.Close)
		_, err := s.client(t).Analyse(t.Context(), testCall())
		var se *ScanError
		if !errors.As(err, &se) || se.Code != CodeScannerUnavailable || se.Defer == 0 {
			t.Fatalf("%v", err)
		}
	})
	t.Run("a connection that breaks after the call was sent: scanner_lost, one attempt", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}
		_, err := s.client(t).Analyse(t.Context(), testCall())
		var se *ScanError
		if !errors.As(err, &se) || se.Code != CodeScannerLost || se.Defer != 0 {
			t.Fatalf("%v", err)
		}
	})
	t.Run("the call's deadline: timeout, one attempt", func(t *testing.T) {
		s := newScannerStub(t)
		s.answer = func(_ http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }
		c := s.client(t)
		c.o.Deadline = 50 * time.Millisecond
		_, err := c.Analyse(t.Context(), testCall())
		var se *ScanError
		if !errors.As(err, &se) || se.Code != CodeTimeout || se.Defer != 0 {
			t.Fatalf("%v", err)
		}
	})
	t.Run("a cancelled job closes the call and is no scan error", func(t *testing.T) {
		s := newScannerStub(t)
		ended := make(chan struct{})
		s.answer = func(_ http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			close(ended)
		}
		ctx, cancel := context.WithCancelCause(t.Context())
		cause := errors.New("drain")
		go func() { time.Sleep(50 * time.Millisecond); cancel(cause) }()
		_, err := s.client(t).Analyse(ctx, testCall())
		if !errors.Is(err, cause) {
			t.Fatalf("%v", err)
		}
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Fatal("the scanner did not see the call end")
		}
	})
}
