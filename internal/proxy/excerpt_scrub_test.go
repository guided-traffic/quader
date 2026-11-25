package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// R-CUR-011 R-OPS-013: the excerpt of an upstream's error answer and the address an item keeps of where its bytes came from hold no
// signature of a signed address (Q-OPS-426). Microseconds: two strings.
func TestR_OPS_013_AFailureAndAnItemKeepNoSignature(t *testing.T) {
	t.Run("the excerpt of an error page that names a signed address", func(t *testing.T) {
		page := `<Error><Code>AccessDenied</Code><Message>see https://b.s3.example/x?X-Amz-Credential=AKIASECRET1&validate_token=SECRET2</Message></Error>`
		if got := cleanExcerpt([]byte(page)); strings.Contains(got, "SECRET") || !strings.Contains(got, "AccessDenied") {
			t.Errorf("%s", got)
		}
	})
	t.Run("the final address of a redirected download", func(t *testing.T) {
		target, _ := url.Parse("https://cdn.example/artifact/x.tgz?verify=1764067200-SECRET3&limit=1")
		resp := &http.Response{Request: &http.Request{URL: target, Response: &http.Response{StatusCode: http.StatusFound}}}
		got := (&fetcher{}).finalAddress(resp)
		if got != "https://cdn.example/artifact/x.tgz?verify=[REDACTED]&limit=[REDACTED]" {
			t.Errorf("%s", got)
		}
	})
}
