package nuget

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

func render(method string, r *http.Request, err error) *httptest.ResponseRecorder {
	if r == nil {
		r = httptest.NewRequest(method, "https://quader.example.com/api/nuget/it/nuget-local/v3/index.json", nil)
	}
	w := httptest.NewRecorder()
	New().WriteError(w, r, err)
	return w
}

// TestR_NUGET_002_AnErrorIsOneLineOfCodeAndMessage: the body is text/plain, one line
// "<code>: <message>", whatever the detail holds.
func TestR_NUGET_002_AnErrorIsOneLineOfCodeAndMessage(t *testing.T) {
	w := render(http.MethodGet, nil, errs.E(errs.Invalid, errs.CodeNuGetInvalidPackage, "first line\nsecond line\r\nthird"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got, want := w.Body.String(), "nuget_invalid_package: first line second line  third"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("the answer is not marked nosniff")
	}
	head := render(http.MethodHead, httptest.NewRequest(http.MethodHead, "/x", nil), errs.E(errs.Invalid, errs.CodeNuGetInvalidPackage, "bad"))
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") != "26" {
		t.Errorf("HEAD: %d body bytes, Content-Length %q", head.Body.Len(), head.Header().Get("Content-Length"))
	}
}

// TestR_NUGET_012_A401ChallengesWithBasicAlone: the challenge is exactly Basic realm="quader",
// whatever the authentication stage put into the error, because NTLM or Negotiate beside
// Basic would make a client send Basic only after it restricted its authentication types.
func TestR_NUGET_012_A401ChallengesWithBasicAlone(t *testing.T) {
	for _, challenge := range []string{"", `Bearer realm="x"`, `Negotiate, Basic realm="quader"`} {
		err := errs.E(errs.Unauthenticated, errs.CodeUnauthenticated, "Credentials are required.")
		err.Challenge = challenge
		w := render(http.MethodGet, nil, err)
		if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") != `Basic realm="quader"` {
			t.Errorf("challenge %q: status %d, WWW-Authenticate %q", challenge, w.Code, w.Header().Get("WWW-Authenticate"))
		}
		if !strings.HasPrefix(w.Body.String(), errs.CodeUnauthenticated+": ") {
			t.Errorf("body = %q", w.Body.String())
		}
	}
}

// TestR_NUGET_002_RetryAfterIsAtMost30Seconds: the clients wait for the Retry-After of any
// answer and count the wait against the one timeout of the command.
func TestR_NUGET_002_RetryAfterIsAtMost30Seconds(t *testing.T) {
	for _, c := range []struct {
		wait time.Duration
		want string
	}{{0, "1"}, {2 * time.Second, "2"}, {1500 * time.Millisecond, "2"}, {30 * time.Second, "30"}, {5 * time.Minute, "30"}} {
		err := errs.E(errs.Unavailable, errs.CodeUnavailable, "busy", func(e *errs.Error) { e.RetryAfter = c.wait })
		if got := render(http.MethodGet, nil, err).Header().Get("Retry-After"); got != c.want {
			t.Errorf("RetryAfter %s: header %q, want %q", c.wait, got, c.want)
		}
	}
	// a 4xx that carries a wait keeps it, capped the same way
	err := errs.E(errs.RateLimited, errs.CodeRateLimited, "slow down", func(e *errs.Error) { e.RetryAfter = time.Hour })
	if got := render(http.MethodGet, nil, err).Header().Get("Retry-After"); got != "30" {
		t.Errorf("rate limit: Retry-After = %q", got)
	}
}

// TestR_SIG_068_TheChecksOfADownloadReachNuGetsClientsAsAWarning: NuGet's clients print no body of an error, and they print
// X-NuGet-Warning (Q-SIG-241, measured with dotnet restore of the .NET SDK 10.0.403): the refusals and the pending answers of the
// malware gate, the vulnerability block and the signature gate carry their text there, as printable ASCII on one line; every other
// error, a 404 above all, carries none. Unit, microseconds, no payload.
func TestR_SIG_068_TheChecksOfADownloadReachNuGetsClientsAsAWarning(t *testing.T) {
	text := "Quader refused example.toy 1.2.0 of web/nuget-local: it has no signature of its publisher or its build.\nDetails: x"
	for _, c := range []struct {
		kind errs.Kind
		code string
	}{
		{errs.Forbidden, errs.CodeSignatureNotVerified},
		{errs.Unavailable, errs.CodeSignatureCheckPending},
		{errs.Forbidden, errs.CodeMalwareFound},
		{errs.Forbidden, errs.CodeMalwareNotChecked},
		{errs.Unavailable, errs.CodeMalwareCheckPending},
		{errs.Forbidden, errs.CodeVulnerabilityBlocked},
		{errs.Forbidden, errs.CodeVulnerabilityDataMissing},
	} {
		w := render(http.MethodGet, nil, errs.E(c.kind, c.code, text))
		want := "Quader refused example.toy 1.2.0 of web/nuget-local: it has no signature of its publisher or its build. Details: x"
		if got := w.Header().Get("X-NuGet-Warning"); got != want {
			t.Errorf("%s: X-NuGet-Warning = %q, want %q", c.code, got, want)
		}
	}
	if got := render(http.MethodGet, nil, errs.E(errs.Forbidden, errs.CodeSignatureNotVerified, "café\x7f")).Header().Get("X-NuGet-Warning"); got != "caf? " {
		t.Errorf("a byte outside printable ASCII: X-NuGet-Warning = %q", got)
	}
	for _, err := range []error{
		errs.E(errs.NotFound, errs.CodeNotFound, "The package does not exist."),
		errs.E(errs.Forbidden, errs.CodeForbidden, "The permission read is required."),
		errs.E(errs.RateLimited, errs.CodeRateLimited, "slow down"),
		errs.E(errs.Unauthenticated, errs.CodeUnauthenticated, "Credentials are required."),
	} {
		if w := render(http.MethodGet, nil, err); w.Header().Get("X-NuGet-Warning") != "" {
			t.Errorf("%d %v carries X-NuGet-Warning %q", w.Code, err, w.Header().Get("X-NuGet-Warning"))
		}
	}
}

// TestR_NUGET_001_AV2PathAnswersWithTheServiceIndexOfTheAddressedRepository: the 404 of a
// path that is no v3 resource names the absolute URL of the service index, built from the
// refusal the dispatcher attached (Q-ARCH-263), so the message is the repository the client
// addressed and never a guess.
func TestR_NUGET_001_AV2PathAnswersWithTheServiceIndexOfTheAddressedRepository(t *testing.T) {
	v2 := errs.E(errs.NotFound, errs.CodeNuGetV2Unsupported, "No NuGet v3 resource has this path.")
	r := httptest.NewRequest(http.MethodGet, "https://quader.example.com/api/nuget/it/nuget-local/FindPackagesById()?id='Acme.Widgets'", nil)
	r = formats.WithRefusal(r, formats.Refusal{Format: ID, Name: qr("nuget-local"), BaseURL: "https://quader.example.com"})
	w := render(http.MethodGet, r, v2)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "nuget_v2_unsupported: ") || !strings.Contains(body, "https://quader.example.com/api/nuget/it/nuget-local/v3/index.json") {
		t.Errorf("body = %q", body)
	}
	if strings.ContainsAny(body, "\r\n") {
		t.Errorf("body is not one line: %q", body)
	}

	t.Run("the cache view of a remote repository", func(t *testing.T) {
		r := formats.WithRefusal(httptest.NewRequest(http.MethodGet, "/x", nil), formats.Refusal{Format: ID, Name: qr("nuget-org~cache"), BaseURL: "https://quader.example.com"})
		if body := render(http.MethodGet, r, v2).Body.String(); !strings.Contains(body, "/api/nuget/it/nuget-org~cache/v3/index.json") {
			t.Errorf("body = %q", body)
		}
	})
	t.Run("a refusal that happened before the repository was known cannot link", func(t *testing.T) {
		w := render(http.MethodGet, nil, v2)
		if w.Code != http.StatusNotFound || !strings.HasPrefix(w.Body.String(), "nuget_v2_unsupported: ") || strings.Contains(w.Body.String(), "https://") {
			t.Errorf("status %d, body %q", w.Code, w.Body.String())
		}
	})
}

// TestR_NUGET_006_APushToARemoteRepositoryAnswers405: the answer of a write addressed to a
// class that takes none says why, in the native shape, with the methods the path has.
func TestR_NUGET_006_APushToARemoteRepositoryAnswers405(t *testing.T) {
	err := errs.E(errs.MethodNotAllowed, errs.CodeMethodNotAllowed, "A remote repository does not serve this request.",
		errs.WithAllow("GET", "HEAD"), errs.WithExtension("repositoryClass", "remote"))
	w := render(http.MethodPut, httptest.NewRequest(http.MethodPut, "/x", nil), err)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("status %d, Allow %q", w.Code, w.Header().Get("Allow"))
	}
	if !strings.Contains(w.Body.String(), "remote repository") || !strings.HasPrefix(w.Body.String(), errs.CodeMethodNotAllowed+": ") {
		t.Errorf("body = %q", w.Body.String())
	}
}

// TestR_NUGET_002_AnInternalFailureHidesItsCause: a failure of the server answers with the
// generic detail, never with the text of its cause.
func TestR_NUGET_002_AnInternalFailureHidesItsCause(t *testing.T) {
	w := render(http.MethodGet, nil, errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(http.ErrAbortHandler)))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "abort") {
		t.Errorf("status %d, body %q", w.Code, w.Body.String())
	}
}

// TestR_NUGET_002_TheSessionIDIsRecordedScrubbed: the X-NuGet-Session-Id of a client is logged to
// correlate the requests of one restore; control characters never reach the record, and a
// request without a record or without the header is left alone.
func TestR_NUGET_002_TheSessionIDIsRecordedScrubbed(t *testing.T) {
	if got := cleanField("abc\r\ndef\x00\x7fghi"); got != "abcdefghi" {
		t.Errorf("cleanField = %q", got)
	}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	recordClientFacts(r) // no request record in the context
	ctx, info := observability.WithRequestInfo(r.Context())
	r = r.WithContext(ctx)
	r.Header.Set(headerSessionID, "5c1f\n0d")
	recordClientFacts(r)
	recordClientFacts(httptest.NewRequest(http.MethodGet, "/y", nil).WithContext(ctx)) // no header
	if info == nil {
		t.Fatal("no request record")
	}
}

// TestR_REPO_072_APlaceholdersSentenceIsTheOneLineOfTheNotFound: the 404 of a name a placeholder would have been asked for keeps
// NuGet's status and code, and its sentence is the message of the one line the client prints (R-NUGET-002).
func TestR_REPO_072_APlaceholdersSentenceIsTheOneLineOfTheNotFound(t *testing.T) {
	member := qname.Repo{Project: wellknown.ProjectKey, Key: "nuget-org"}
	w := render(http.MethodGet, nil, repository.PlaceholderNotFound(member, wellknown.StateSwitchedOff, 0))
	want := "not_found: " + repository.PlaceholderSentence(member, wellknown.StateSwitchedOff, 0)
	if w.Code != http.StatusNotFound || w.Body.String() != want {
		t.Fatalf("status %d body %q, want %q", w.Code, w.Body.String(), want)
	}
}
