package errs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestR_BAS_099_AProblemForABastionsCallerNamesNothingOfTheInstallation: a problem rendered for a request the Bastion answers
// has the type about:blank and the status phrase as its title (Q-BAS-321); one rendered by the main installation for a
// recognized forward has them too, no requestId, and the neutral detail for a failure of the remote engine, whose own detail
// names the member repository (bastion.md R-BAS-099 rows 3, 7 and 15). Any other request keeps the relative type of R-ARCH-023.
func TestR_BAS_099_AProblemForABastionsCallerNamesNothingOfTheInstallation(t *testing.T) {
	upstream := E(UpstreamFailed, CodeUpstreamError, "The upstream of web/npm-remote answered 503.")
	render := func(ctx context.Context, err error) Problem {
		r := httptest.NewRequest(http.MethodGet, "/api/npm/web/npm-all/lodash", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		Write(w, r, err, "req-1", nil)
		var p Problem
		if jerr := json.Unmarshal(w.Body.Bytes(), &p); jerr != nil {
			t.Fatal(jerr)
		}
		return p
	}

	own := render(context.Background(), upstream)
	if own.Type != ProblemTypePrefix+CodeUpstreamError || own.RequestID != "req-1" || !strings.Contains(own.Detail, "web/npm-remote") {
		t.Fatalf("an own caller: %+v", own)
	}
	if BastionForward(context.Background()) || BastionForward(WithBastionAnswer(context.Background())) {
		t.Fatal("only a forward is a forward")
	}

	at := render(WithBastionAnswer(context.Background()), upstream)
	if at.Type != AboutBlank || at.Title != "Bad Gateway" || at.RequestID != "req-1" || at.Code != CodeUpstreamError {
		t.Fatalf("at the Bastion: %+v", at)
	}

	ctx := WithBastionForward(context.Background())
	if !BastionForward(ctx) {
		t.Fatal("the forward is not read back")
	}
	fwd := render(ctx, upstream)
	if fwd.Type != AboutBlank || fwd.Title != "Bad Gateway" || fwd.RequestID != "" || fwd.Code != CodeUpstreamError ||
		fwd.Detail != "The package could not be fetched right now." {
		t.Fatalf("a forward: %+v", fwd)
	}
	for _, code := range []string{
		CodeUpstreamTimeout, CodeUpstreamUnreachable, CodeUpstreamRateLimited, CodeUpstreamAuthFailed,
		CodeUpstreamUnexpectedStatus, CodeEgressDenied, CodeTLSUntrusted, CodeSourceNetworkUnavailable, CodePopulateNotPermitted,
	} {
		p := render(ctx, E(UpstreamFailed, code, "The upstream of web/npm-remote at registry.internal failed."))
		if strings.Contains(p.Detail, "web/npm-remote") || strings.Contains(p.Detail, "registry.internal") || p.Code != code {
			t.Errorf("%s: %+v", code, p)
		}
	}
	if p := render(ctx, E(NotFound, CodeNotFound, "The resource does not exist.")); p.Detail != "The resource does not exist." {
		t.Errorf("a detail that names nothing changed: %+v", p)
	}
}
