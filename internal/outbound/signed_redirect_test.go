package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability"
)

// signedRedirects are the queries of the signed addresses an upstream redirects a download to, one per kind (R-OPS-013 rule 7,
// Q-OPS-426); every value that opens one contains SECRET.
var signedRedirects = map[string]string{
	"S3":         "response-content-disposition=attachment&X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIASECRET1%2Fs3&X-Amz-Security-Token=SECRET2&X-Amz-Expires=600&X-Amz-Signature=SECRET3",
	"Galaxy":     "expires=1764067200&validate_token=SECRET4:SECRET5",
	"GCS":        "X-Goog-Credential=svc%2FSECRET6&X-Goog-Signature=SECRET7",
	"Azure SAS":  "sv=2025-01-05&se=2026-11-25T10%3A20%3A00Z&sp=r&sig=SECRET8",
	"CloudFront": "Expires=1764067200&Signature=SECRET9&Key-Pair-Id=K2JCJMDEHXQW5F",
	"Docker Hub": "verify=1764067200-SECRET10",
	"GitHub":     "sp=r&sig=SECRET11&jwt=SECRET12",
	"Quay":       "cf_sign=SECRET13&cf_expiry=1764067200&namespace=SECRET14",
}

// R-OPS-011 R-OPS-013: a download that an upstream redirects to a signed address is logged and traced hop by hop; the first hop, the
// address Quader built, keeps the values of the parameters Quader writes (limit), the redirect's target keeps the names of its
// query and none of its values. Milliseconds: two loopback requests per kind, bodies of five bytes.
func TestR_OPS_013_ARedirectToASignedAddressReachesNoRecordWithItsSignature(t *testing.T) {
	for kind, q := range signedRedirects {
		t.Run(kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/blob?"+q, http.StatusFound)
					return
				}
				_, _ = io.WriteString(w, "bytes")
			}))
			defer srv.Close()
			var buf bytes.Buffer
			lg := observability.NewLogger(config.Log{Level: "info", Format: "json", Request: config.LogRequest{RedactQueryParams: observability.DefaultQueryParams()}}, &buf)
			exp := tracetest.NewInMemoryExporter()
			tr, err := observability.Tracing(config.Tracing{Enabled: true, SampleRatio: 1, QueueSize: 8}, observability.WithSpanExporter(exp))
			if err != nil {
				t.Fatal(err)
			}
			c := New(allowLoopback(), WithUpstreamLog(lg), WithTracer(tr)).Client(PurposeRemote)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/start?limit=5&account=SECRET0", nil)
			resp, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			_ = tr.ForceFlush(context.Background())

			if strings.Contains(buf.String(), "SECRET") {
				t.Errorf("a signature reached the upstream log:\n%s", buf.String())
			}
			var urls []string
			for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
				var rec map[string]any
				if err := json.Unmarshal(line, &rec); err != nil {
					t.Fatalf("%v: %s", err, line)
				}
				urls = append(urls, rec["url"].(string))
			}
			if len(urls) != 2 || !strings.HasSuffix(urls[0], "/start?limit=5&account=[REDACTED]") || !strings.Contains(urls[1], "/blob?") {
				t.Fatalf("urls = %q", urls)
			}
			name, _, _ := strings.Cut(q, "=")
			if !strings.Contains(urls[1], name+"=[REDACTED]") {
				t.Errorf("the redirect's target lost its names: %s", urls[1])
			}
			for _, s := range exp.GetSpans() {
				for _, a := range s.Attributes {
					if strings.Contains(a.Value.String(), "SECRET") {
						t.Errorf("span %s: %s=%s", s.Name, a.Key, a.Value.String())
					}
				}
			}
			if got := observability.ScrubRequestURL(resp.Request); strings.Contains(got, "SECRET") {
				t.Errorf("the address an answer came from, as the engine stores it: %s", got)
			}
		})
	}
}
