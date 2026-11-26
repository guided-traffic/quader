package errs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
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

// TestR_BAS_114_AForwardReadsATextOfTheInstallationOnlyWhereTheErrorVouchesForIt: the rules of rendering a forward's problem
// (bastion.md R-BAS-114, Q-BAS-385 to Q-BAS-387), one case each. A detail an error carries for a forward replaces its own; a member
// only this installation reads is left out; every other problem of a status of 500 or more reads the neutral text of its status
// without its field errors, unless it is a rate limit or vouches for its text; a 4xx keeps its own detail. An ordinary caller reads
// every error as it was made.
func TestR_BAS_114_AForwardReadsATextOfTheInstallationOnlyWhereTheErrorVouchesForIt(t *testing.T) {
	render := func(ctx context.Context, err error) Problem {
		r := httptest.NewRequest(http.MethodGet, "/api/npm/web/npm-all/lodash", nil).WithContext(ctx)
		p, _ := ProblemFor(r, err, "req-1")
		return p
	}
	fwd := WithBastionForward(context.Background())
	for _, c := range []struct {
		name   string
		err    *Error
		detail string
		gone   string // an extension member a forward does not read
	}{
		{"a forward detail", E(Forbidden, CodeForbidden, "Blocked. Details: https://quader.test/ui/x", WithForwardDetail("Blocked.")), "Blocked.", ""},
		{"a member of the installation", E(Forbidden, CodeForbidden, "Blocked.", WithInstallationExtension("findingId", "0192"), WithExtension("advisory", "GHSA-1")), "Blocked.", "findingId"},
		{"500", E(Internal, CodeInternal, "the replica r-7 lost its database"), DetailInternal, ""},
		{"501", E(NotImplemented, CodeNotImplemented, "web/npm-remote forwards no audit"), DetailNotServedHere, ""},
		{"502 with field errors", E(UpstreamFailed, "deb_upstream_signature_invalid", "not signed", WithFields(FieldError{Pointer: "/key", Code: "revoked", Detail: "the key ABCD is revoked"})), DetailBadGateway, ""},
		{"503", E(Unavailable, CodeUnavailable, "The files service is not available on this replica."), DetailUnavailable, ""},
		{"504", E(UpstreamTimeout, CodeDatabaseTimeout, "The database did not answer."), DetailGatewayTimeout, ""},
		{"a 503 that vouches for its text", E(Unavailable, CodeMalwareCheckPending, "Quader is checking a.tgz for malware.", WithForwardDetail("Quader is checking a.tgz for malware.")), "Quader is checking a.tgz for malware.", ""},
		{"a rate limit", E(RateLimited, CodeRateLimited, "The request limit is used up. Try again in 15 minutes."), "The request limit is used up. Try again in 15 minutes.", ""},
		{"a 4xx of its own", E(NotFound, CodeNotFound, "The collection a.b has no version 1.0.0 in this repository."), "The collection a.b has no version 1.0.0 in this repository.", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := render(fwd, c.err)
			if p.Detail != c.detail || p.Code != c.err.Code || p.Status != c.err.Status() || len(p.Errors) != 0 && p.Status >= 500 {
				t.Errorf("a forward reads %+v", p)
			}
			if _, kept := p.Extensions[c.gone]; c.gone != "" && kept {
				t.Errorf("a forward reads the member %s: %+v", c.gone, p.Extensions)
			}
			if c.gone != "" && p.Extensions["advisory"] != "GHSA-1" {
				t.Errorf("a forward lost a member of its own: %+v", p.Extensions)
			}
			own := render(context.Background(), c.err)
			want := c.err.Detail
			if c.err.Kind == Internal {
				want = DetailInternal
			}
			if own.Detail != want || len(own.Errors) != len(c.err.Fields) || (c.gone != "" && own.Extensions[c.gone] == nil) {
				t.Errorf("an own caller reads %+v", own)
			}
		})
	}
}

// TestR_BAS_114_TheRegistryDocumentsWhatACallerOfABastionReads: the problem registry, whose pages the main installation serves, holds
// every code of a Bastion's own answers (R-BAS-029) with a page that names no party, and the page of each code whose text a forward
// reads otherwise (R-BAS-029's table of what a forward relays) says so.
func TestR_BAS_114_TheRegistryDocumentsWhatACallerOfABastionReads(t *testing.T) {
	for _, code := range []string{
		iface.CodeNotFound, iface.CodeBadGateway, iface.CodeUnavailable, iface.CodeBusy, iface.CodeGatewayTimeout, iface.CodeAuthTimeout,
		iface.CodeAddressBlocked, iface.CodeInvalidToken, iface.CodeCredentialsNeeded, iface.CodeRateLimited, iface.CodeMethodNotAllowed,
		iface.CodePayloadTooLarge, iface.CodeMisdirected, iface.CodeReadOnly, CodeNotReady,
	} {
		info, ok := Lookup(code)
		if !ok {
			t.Errorf("%s: a code of a Bastion's own answers has no page", code)
			continue
		}
		if strings.Contains(info.Title+info.Detail, "main installation") {
			t.Errorf("%s: the page names the main installation: %q", code, info.Detail)
		}
	}
	for _, code := range []string{CodeVulnerabilityBlocked, CodeNotAllowedInRepository} {
		if info, _ := Lookup(code); !strings.Contains(info.Detail, "Bastion") {
			t.Errorf("%s: the page does not say what a caller of a Bastion reads: %q", code, info.Detail)
		}
	}
}
