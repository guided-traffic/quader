package observability

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

// signedQueries are the queries of the signed addresses an upstream hands back, one per kind (R-OPS-013 rule 7, Q-OPS-426). Every
// value that opens the address contains SECRET; the tests assert that none of them survives.
func signedQueries() map[string]string {
	return map[string]string{
		"a presigned S3 address": "response-content-disposition=attachment%3B%20filename%3Dx.tar.gz&X-Amz-Algorithm=AWS4-HMAC-SHA256" +
			"&X-Amz-Credential=AKIASECRET1%2F20261125%2Feu-central-1%2Fs3%2Faws4_request&X-Amz-Date=20261125T101010Z&X-Amz-Expires=600" +
			"&X-Amz-Security-Token=SECRET2&X-Amz-SignedHeaders=host&X-Amz-Signature=SECRET3",
		"a Galaxy content guard":                   "expires=1764067200&validate_token=SECRET4abc:SECRET5def",
		"a Google Cloud Storage signed address":    "X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Credential=svc%40p.iam.gserviceaccount.com%2FSECRET6&X-Goog-Date=20261125T101010Z&X-Goog-Expires=900&X-Goog-SignedHeaders=host&X-Goog-Signature=SECRET7",
		"an Azure SAS of a package host":           "sv=2025-01-05&se=2026-11-25T10%3A20%3A00Z&sr=b&sp=r&spr=https&sig=SECRET8%2Bx%3D",
		"a CloudFront signed address":              "Expires=1764067200&Signature=SECRET9~x__&Key-Pair-Id=K2JCJMDEHXQW5F",
		"Docker Hub's blob redirect":               "verify=1764067200-SECRET10",
		"a GitHub release asset redirect":          "sp=r&sv=2018-11-09&sr=b&spr=https&se=2026-11-25T10%3A20%3A00Z&rscd=attachment&sig=SECRET11&jwt=SECRET12.payload.sig",
		"Quay's CDN":                               "cf_sign=SECRET13&cf_expiry=1764067200&region=us-east-1&namespace=quay",
		"a CDN's hmac":                             "hmac=SECRET14&exp=1764067200",
		"a bare token standing as the whole query": "eyJhbGciOiJIUzI1NiJ9SECRET15payloadsignature",
	}
}

func TestR_OPS_013_ASignedAddressKeepsNoSignature(t *testing.T) {
	for kind, q := range signedQueries() {
		u, err := url.Parse("https://cdn.example/artifact/x.tar.gz?" + q)
		if err != nil {
			t.Fatal(err)
		}
		hop := &http.Request{Method: http.MethodGet, URL: u, Response: &http.Response{StatusCode: http.StatusFound}}
		for form, got := range map[string]string{
			"as Quader's own address":                ScrubURL(u),
			"as a handed address":                    ScrubHandedURL(u),
			"as a redirect's request":                ScrubRequestURL(hop),
			"in free text, as Go's errors quote it":  Scrub(`Get "` + u.String() + `": context deadline exceeded`),
			"in free text, relative":                 Scrub("GET /artifact/x.tar.gz?" + q + " 403"),
			"through a logger with the default list": NewScrubber(DefaultQueryParams()).String(u.String()),
		} {
			// A relative address in text is caught by the redaction list alone, which needs a name: a bare token there stays (H-2061).
			if strings.Contains(got, "SECRET") && (form != "in free text, relative" || strings.Contains(q, "=")) {
				t.Errorf("%s %s: %s", kind, form, got)
			}
		}
	}
	t.Run("R-OPS-013 a handed address keeps every name and no value", func(t *testing.T) {
		u, _ := url.Parse("https://b.s3.example/artifact/x?" + signedQueries()["a presigned S3 address"] + "&limit=5")
		got := ScrubHandedURL(u)
		for _, want := range []string{"X-Amz-Signature=[REDACTED]", "X-Amz-Expires=[REDACTED]", "limit=[REDACTED]", "https://b.s3.example/artifact/x?"} {
			if !strings.Contains(got, want) {
				t.Errorf("%q lacks %q", got, want)
			}
		}
	})
}

func TestR_OPS_013_AnAddressKeepsTheValuesQuaderWrote(t *testing.T) {
	u, _ := url.Parse("https://bot:pw@galaxy.example/api/v3/search/?keywords=nginx&is_highest=true&limit=10&offset=0&account=SECRET&uploads#frag")
	t.Run("R-OPS-013 Quader's own address keeps the values of its parameters, no other, no user information, no fragment", func(t *testing.T) {
		got := ScrubURL(u)
		want := "https://galaxy.example/api/v3/search/?keywords=nginx&is_highest=true&limit=10&offset=0&account=[REDACTED]&uploads"
		if got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
	})
	t.Run("R-OPS-013 the same address as a redirect's target keeps none", func(t *testing.T) {
		got := ScrubRequestURL(&http.Request{URL: u, Response: &http.Response{}})
		if strings.Contains(got, "nginx") || strings.Contains(got, "SECRET") || !strings.Contains(got, "keywords=[REDACTED]") {
			t.Errorf("%s", got)
		}
		if first := ScrubRequestURL(&http.Request{URL: u}); !strings.Contains(first, "keywords=nginx") {
			t.Errorf("a first hop is Quader's own address: %s", first)
		}
	})
	t.Run("R-OPS-013 the redaction list wins over the parameters Quader writes", func(t *testing.T) {
		s := NewScrubber([]string{"q"})
		v, _ := url.Parse("https://nuget.example/query?q=secretpkg&take=20")
		if got := s.URL(v); got != "https://nuget.example/query?q=[REDACTED]&take=20" {
			t.Errorf("%s", got)
		}
	})
	t.Run("R-OPS-013 an address is rendered alike a second time", func(t *testing.T) {
		once := ScrubURL(u)
		v, _ := url.Parse(once)
		if twice := ScrubURL(v); twice != once || Scrub(once) != once {
			t.Errorf("%s\n%s\n%s", once, twice, Scrub(once))
		}
	})
	t.Run("R-OPS-013 free text keeps the values Quader writes and an address without a query", func(t *testing.T) {
		in := `GET "https://registry.npmjs.org/-/v1/search?text=left-pad&size=20&from=0" failed; see https://status.example/`
		if got := Scrub(in); got != in {
			t.Errorf("got  %s\nwant %s", got, in)
		}
	})
}

func TestR_OPS_013_TheDefaultListIsTheConfigurationsDefault(t *testing.T) {
	t.Run("R-OPS-013 log.request.redact_query_params defaults to DefaultQueryParams", func(t *testing.T) {
		f, _ := reflect.TypeFor[config.LogRequest]().FieldByName("RedactQueryParams")
		var def []string
		if err := json.Unmarshal([]byte(f.Tag.Get("default")), &def); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(def, DefaultQueryParams()) {
			t.Errorf("config default %v\nDefaultQueryParams %v", def, DefaultQueryParams())
		}
	})
	t.Run("R-OPS-013 no parameter Quader writes is on the redaction list", func(t *testing.T) {
		deny := map[string]bool{}
		for _, p := range DefaultQueryParams() {
			deny[strings.ToLower(p)] = true
		}
		for _, p := range ProductQueryParams() {
			if deny[strings.ToLower(p)] {
				t.Errorf("%s is on both lists", p)
			}
		}
	})
}
