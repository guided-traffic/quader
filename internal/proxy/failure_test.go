package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-CUR-011, Q-CUR-20: a failure of an exchange with the upstream in precise words. Unit tier, milliseconds: the answers come from an
// httptest server in the test process, the transport failures from a closed port, a resolver that answers no name, an untrusted
// certificate and a deadline; the data is a few hundred bytes.

func testRemote() Remote {
	return Remote{Repo: repository.Repository{Name: qname.Repo{Project: "web", Key: "npm-remote"}}, id: 7}
}

// answer fetches path from a server that answers status with body and the headers, and classifies the answer as the engine does.
func answer(t *testing.T, status int, contentType, body string, header ...string) remotecuration.Failure {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/left-pad/-/left-pad-1.0.0.tgz?X-Amz-Signature=secret&token=t") //nolint:noctx // a test
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	f := classifyStatus(testRemote(), resp)
	f.x.cause = nil
	return Describe(withExchange(f.err, f.x))
}

// transport classifies a transport error as the engine does.
func transport(err error, target string, idle bool) remotecuration.Failure {
	u, _ := url.Parse(target)
	f := (&Engine{}).classifyError(testRemote(), u, err, idle)
	if f.x.Address == "" {
		f.x.Address = addressOf(u)
	}
	return Describe(withExchange(f.err, f.x))
}

func TestR_CUR_011_TheFailureOfAnExchangeInPreciseWords(t *testing.T) {
	type want struct {
		class  remotecuration.FailureClass
		step   remotecuration.FailureStep
		code   string
		status int
	}
	check := func(t *testing.T, name string, got remotecuration.Failure, w want) {
		t.Helper()
		if got.Class != w.class || got.Step != w.step || got.Code != w.code || got.Status != w.status {
			t.Errorf("%s: class %s step %s code %s status %d, want %s %s %s %d", name, got.Class, got.Step, got.Code, got.Status,
				w.class, w.step, w.code, w.status)
		}
		if strings.Contains(got.Address, "?") || strings.Contains(got.Address, "secret") || strings.Contains(got.Detail, "secret") {
			t.Errorf("%s: the query reached the failure: %q, %q", name, got.Address, got.Detail)
		}
		if got.Detail == "" {
			t.Errorf("%s: no sentence for a person", name)
		}
	}

	t.Run("answers", func(t *testing.T) {
		for _, c := range []struct {
			status int
			want   want
		}{
			{401, want{remotecuration.ClassUpstreamAuth, remotecuration.StepStatus, errs.CodeUpstreamAuthFailed, 401}},
			{403, want{remotecuration.ClassUpstreamAuth, remotecuration.StepStatus, errs.CodeUpstreamAuthFailed, 403}},
			{429, want{remotecuration.ClassUpstreamRateLimited, remotecuration.StepStatus, errs.CodeUpstreamRateLimited, 429}},
			{500, want{remotecuration.ClassUpstreamError, remotecuration.StepStatus, errs.CodeUpstreamError, 500}},
			{502, want{remotecuration.ClassUpstreamError, remotecuration.StepStatus, errs.CodeUpstreamError, 502}},
			{503, want{remotecuration.ClassUpstreamError, remotecuration.StepStatus, errs.CodeUpstreamError, 503}},
			{504, want{remotecuration.ClassUpstreamError, remotecuration.StepStatus, errs.CodeUpstreamError, 504}},
			{400, want{remotecuration.ClassUpstreamUnexpected, remotecuration.StepStatus, errs.CodeUpstreamUnexpectedStatus, 400}},
			{418, want{remotecuration.ClassUpstreamUnexpected, remotecuration.StepStatus, errs.CodeUpstreamUnexpectedStatus, 418}},
			{301, want{remotecuration.ClassUpstreamUnexpected, remotecuration.StepStatus, errs.CodeUpstreamUnexpectedStatus, 301}},
		} {
			got := answer(t, c.status, "text/plain", "the upstream says no")
			check(t, fmt.Sprint(c.status), got, c.want)
			if !strings.HasSuffix(got.Address, "/left-pad/-/left-pad-1.0.0.tgz") || !strings.HasPrefix(got.Address, "http://127.0.0.1:") {
				t.Errorf("%d: address %q, want the stub's address without its query", c.status, got.Address)
			}
			if got.Excerpt != "the upstream says no" {
				t.Errorf("%d: excerpt %q", c.status, got.Excerpt)
			}
		}
	})

	t.Run("the excerpt is cut at 512 bytes and cleaned; a binary answer has none", func(t *testing.T) {
		got := answer(t, 503, "application/json", "{\"error\":\"down\x00\x01é\"}\n"+strings.Repeat("x", 600))
		if len(got.Excerpt) != 512 || !strings.HasPrefix(got.Excerpt, "{\"error\":\"down????\"}\n") {
			t.Errorf("excerpt %d bytes: %q", len(got.Excerpt), got.Excerpt[:30])
		}
		if bin := answer(t, 500, "application/octet-stream", "\x1f\x8b binary"); bin.Excerpt != "" {
			t.Errorf("a binary answer gave the excerpt %q", bin.Excerpt)
		}
		if none := answer(t, 502, "", "plain"); none.Excerpt != "plain" {
			t.Errorf("an answer without a type gave %q", none.Excerpt)
		}
	})

	t.Run("the upstream's Retry-After", func(t *testing.T) {
		got := answer(t, 429, "text/plain", "slow down", "Retry-After", "90")
		if got.RetryAfter != 90 || !strings.Contains(got.Detail, "90 s") {
			t.Errorf("retryAfter %d, detail %q", got.RetryAfter, got.Detail)
		}
	})

	t.Run("transport", func(t *testing.T) {
		// A closed port: the connection is refused.
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		closed := "http://" + l.Addr().String() + "/x?token=secret"
		_ = l.Close()
		_, refused := http.Get(closed) //nolint:noctx,bodyclose // a test; no answer comes
		check(t, "refused", transport(refused, closed, false),
			want{remotecuration.ClassNetwork, remotecuration.StepConnect, errs.CodeUpstreamUnreachable, 0})

		// A resolver that answers no name.
		r := &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("no DNS server in the test")
		}}
		_, dnsErr := r.LookupHost(context.Background(), "registry.invalid")
		check(t, "dns", transport(&url.Error{Op: "Get", URL: "https://registry.invalid/x", Err: dnsErr}, "https://registry.invalid/x?sig=secret", false),
			want{remotecuration.ClassNetwork, remotecuration.StepResolve, errs.CodeUpstreamUnreachable, 0})

		// A certificate this process does not trust.
		tlsSrv := httptest.NewTLSServer(http.NotFoundHandler())
		defer tlsSrv.Close()
		_, tlsErr := http.Get(tlsSrv.URL + "/x") //nolint:noctx,bodyclose // a test; the handshake fails
		got := transport(tlsErr, tlsSrv.URL+"/x?sig=secret", false)
		check(t, "tls", got, want{remotecuration.ClassTLS, remotecuration.StepTLS, errs.CodeTLSUntrusted, 0})
		if !strings.Contains(got.Detail, "proxy.outbound.tls.ca_files") {
			t.Errorf("tls: the sentence does not say what helps: %q", got.Detail)
		}

		// A deadline while the answer is awaited, and a body that stopped arriving.
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tlsSrv.URL, nil)
		_, deadline := http.DefaultClient.Do(req) //nolint:bodyclose // no answer comes
		check(t, "deadline", transport(deadline, tlsSrv.URL, false),
			want{remotecuration.ClassNetwork, remotecuration.StepRequest, errs.CodeUpstreamTimeout, 0})
		check(t, "idle", transport(errors.New("read: stalled"), tlsSrv.URL, true),
			want{remotecuration.ClassNetwork, remotecuration.StepBody, errs.CodeUpstreamTimeout, 0})

		// The egress policy and the source network refuse before a connection.
		check(t, "egress", transport(fmt.Errorf("dial: %w", outbound.ErrEgressDenied), "https://10.0.0.1/x", false),
			want{remotecuration.ClassEgressDenied, remotecuration.StepConnect, errs.CodeEgressDenied, 0})
		check(t, "source network", transport(fmt.Errorf("dial: %w", outbound.ErrSourceNetworkUnavailable), "https://10.0.0.1/x", false),
			want{remotecuration.ClassNetwork, remotecuration.StepConnect, errs.CodeSourceNetworkUnavailable, 0})
		check(t, "redirects", transport(fmt.Errorf("get: %w", outbound.ErrTooManyRedirects), "https://a.example/x", false),
			want{remotecuration.ClassUpstreamUnexpected, remotecuration.StepRequest, errs.CodeTooManyRedirects, 0})
	})

	t.Run("what the engine refuses without an exchange", func(t *testing.T) {
		for _, c := range []struct {
			name string
			err  error
			want want
		}{
			{"integrity", errs.E(errs.UpstreamFailed, errs.CodeUpstreamIntegrityMismatch, "x"), want{remotecuration.ClassIntegrity, remotecuration.StepVerify, errs.CodeUpstreamIntegrityMismatch, 0}},
			{"content type", errs.E(errs.UpstreamFailed, errs.CodeUpstreamContentTypeBlocked, "x"), want{remotecuration.ClassContentRefused, remotecuration.StepVerify, errs.CodeUpstreamContentTypeBlocked, 0}},
			{"too large", errs.E(errs.UpstreamFailed, errs.CodeUpstreamItemTooLarge, "x"), want{remotecuration.ClassBound, remotecuration.StepBody, errs.CodeUpstreamItemTooLarge, 0}},
			{"vulnerability", errs.E(errs.Forbidden, errs.CodeVulnerabilityBlocked, "x"), want{remotecuration.ClassGateRefused, remotecuration.StepGate, errs.CodeVulnerabilityBlocked, 0}},
			{"populate", errs.E(errs.Forbidden, errs.CodePopulateNotPermitted, "x"), want{remotecuration.ClassGateRefused, remotecuration.StepGate, errs.CodePopulateNotPermitted, 0}},
			{"not found", errs.E(errs.NotFound, errs.CodeNotFound, "x"), want{remotecuration.ClassUpstreamNotFound, remotecuration.StepStatus, errs.CodeNotFound, 0}},
			{"object store", errs.E(errs.Unavailable, errs.CodeObjectStoreUnavail, "x"), want{remotecuration.ClassStorage, remotecuration.StepStore, errs.CodeObjectStoreUnavail, 0}},
			{"offline", errs.E(errs.NotFound, errs.CodeNotCachedOffline, "x"), want{remotecuration.ClassCircuitOpen, remotecuration.StepRequest, errs.CodeNotCachedOffline, 0}},
			{"circuit", circuitWait(5 * time.Second), want{remotecuration.ClassCircuitOpen, remotecuration.StepRequest, "circuit_open", 0}},
			{"spool", &BackgroundWait{Reason: WaitSpoolBusy, After: time.Second}, want{remotecuration.ClassStorage, remotecuration.StepStore, "spool_busy", 0}},
			{"rate limit deferred", &mirror.DeferredError{Until: time.Now().Add(time.Hour), Cause: &testRateLimit{d: 120 * time.Second}}, want{remotecuration.ClassUpstreamRateLimited, remotecuration.StepStatus, errs.CodeUpstreamRateLimited, 0}},
			{"internal", errors.New("boom"), want{remotecuration.ClassInternal, remotecuration.StepPlan, errs.CodeInternal, 0}},
		} {
			got := Describe(c.err)
			check(t, c.name, got, c.want)
			if !c.want.class.Retryable() && c.want.class == remotecuration.ClassUpstreamAuth {
				t.Errorf("%s retryable", c.name)
			}
		}
	})
}

type testRateLimit struct{ d time.Duration }

func (r *testRateLimit) Error() string                     { return "rate limited" }
func (r *testRateLimit) RetryAfter() (time.Duration, bool) { return r.d, true }
func (r *testRateLimit) RateLimitReason() string           { return "too_many_requests" }
