package formattest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// CheckBastion is check 19 (bastion.md R-BAS-058, Q-BAS-179): a format keeps what a Quader Bastion needs of it, so that every
// Bastion serves it through its route kinds and its declared facts (catalog.Facts.Bastion) without a decision of its own.
const CheckBastion Check = 19

// keptByABastion reports whether a Bastion keeps an answer of this status of a Download, Metadata or Computed route as an entry
// whose headers it replays: a 200, a ranged 206 and a 304 to its own conditional (R-BAS-021 to R-BAS-023). A refusal is replayed
// with every header it carries and an error is passed on, so the header duty does not judge them (Q-BAS-184).
func keptByABastion(status int) bool {
	return status == http.StatusOK || status == http.StatusPartialContent || status == http.StatusNotModified
}

// keptKind reports whether a Bastion keeps answers of the route kind: a Download route's are artifacts, a Metadata or Computed
// route's are documents; a Volatile route's are passed on per request and never kept (R-BAS-013).
func keptKind(k formats.RouteKind) bool {
	return k == formats.Download || k == formats.Metadata || k == formats.Computed
}

// bastion is check 19. It needs no PostgreSQL and serves every content case once more, on a fresh Host seeded as the fixture says:
//
//   - every header of an answer a Bastion keeps (a 200, 206 or 304 of a Download, Metadata or Computed route) is a representation
//     header of the core (catalog.RepresentationHeaders), a per-request header of the core (catalog.PerRequestHeaders) or one the
//     format declares (catalog.Facts.Bastion.ReplayHeaders): a cached answer replays the first and the third and drops every
//     other, so an undeclared header would be on a miss and missing on a hit;
//   - a Download answer carries Content-Length and a strong ETag that is a content address, "<sha256 hex>" or
//     "sha256:<sha256 hex>", equal to the SHA-256 of its body: the Bastion keeps an artifact under its content address and
//     verifies it, and passes one without it through on every request;
//   - a document a Metadata or Computed route serves carries an ETag and answers the same request with that ETag in
//     If-None-Match 304: the Bastion revalidates the documents it keeps with conditional requests;
//   - the format's WriteError answers a rate limit with catalog.Facts.Bastion.RateLimited(): a Bastion's own rate-limit refusals
//     on the format's family take that status.
//
// A route whose answers cannot meet the content-address or the conditional duty by design is listed in Fixture.NoBastion with the
// reason; the header duty has no exemption, since a header is either declared or left out.
func (r *run) bastion() {
	routes := r.f.Routes()
	if len(routes) == 0 {
		r.failf("%s: the format has no routes and the check is not skipped (Skip[%d] needs a reason)", CheckBastion, CheckBastion)
		return
	}
	r.rateLimitStatus()
	r.forwardError()
	allowed := map[string]bool{}
	for _, h := range slices.Concat(catalog.RepresentationHeaders(), catalog.PerRequestHeaders(), r.d.Bastion.ReplayHeaders) {
		allowed[http.CanonicalHeaderKey(h)] = true
	}
	judged := map[string]bool{}
	exemptFailed := map[string]bool{}
	for _, c := range r.fx.Content {
		method := c.Method
		if method == "" {
			method = http.MethodGet
		}
		if (method != http.MethodGet && method != http.MethodHead) || (c.Status != 0 && c.Status >= http.StatusBadRequest) {
			continue
		}
		h := r.host("")
		if c.Seed != nil {
			if err := c.Seed(h); err != nil {
				r.failf("content case %q: the seed fails: %v", c.Name, err)
				continue
			}
		}
		rq, err := h.NewRequest(context.Background(), method, c.Path, bytes.NewReader(c.Body))
		if err != nil || !keptKind(rq.Route.Kind) {
			continue // a case that matches no route is check 7's finding; a Volatile answer is never kept
		}
		resp := h.ServeRequest(rq, false)
		if !keptByABastion(resp.Code) {
			continue // a case that does not serve is check 7's finding
		}
		judged[rq.Route.Name] = true
		r.replayHeaders(c, rq.Route, resp, allowed)
		var duty []string
		if rq.Route.Kind == formats.Download {
			duty = contentAddressed(method, resp)
		} else if method == http.MethodGet && resp.Code == http.StatusOK {
			duty = revalidates(h, c, resp)
		}
		if _, exempt := r.fx.NoBastion[rq.Route.Name]; exempt {
			exemptFailed[rq.Route.Name] = exemptFailed[rq.Route.Name] || len(duty) > 0
			continue
		}
		for _, d := range duty {
			r.failf("content case %q (route %q, %s): %s", c.Name, rq.Route.Name, rq.Route.Kind, d)
		}
	}
	for _, rt := range routes {
		if !keptKind(rt.Kind) || judged[rt.Name] {
			continue
		}
		if _, listed := r.fx.NoContent[rt.Name]; listed {
			continue
		}
		r.failf("route %q (%s) has no content case a Bastion keeps (a GET or HEAD answered 200, 206 or 304), so check 19 judged none of its answers: give it one, or list it in NoContent with the reason", rt.Name, rt.Kind)
	}
	r.noBastionEntries(routes, judged, exemptFailed)
}

// noBastionEntries holds the exemptions of the fixture to what they claim: a reason, a route a Bastion keeps answers of, a case
// that served it, and a duty its answers do not meet.
func (r *run) noBastionEntries(routes []formats.Route, judged, failed map[string]bool) {
	for _, name := range slices.Sorted(maps.Keys(r.fx.NoBastion)) {
		reason := r.fx.NoBastion[name]
		i := slices.IndexFunc(routes, func(rt formats.Route) bool { return rt.Name == name })
		switch {
		case reason == "":
			r.failf("NoBastion lists route %q without a reason", name)
		case i < 0 || !keptKind(routes[i].Kind):
			r.failf("NoBastion lists %q, which is no Download, Metadata or Computed route of the format", name)
		case !judged[name]:
			r.failf("NoBastion lists route %q, which no content case served, so nothing shows that its answers need the exemption", name)
		case !failed[name]:
			r.failf("NoBastion lists route %q (%s), whose answers meet the content-address and the conditional duty: remove the entry", name, reason)
		}
	}
}

// replayHeaders is the header duty: every header of an answer a Bastion keeps is the core's or declared.
func (r *run) replayHeaders(c ContentCase, rt *formats.Route, resp *Response, allowed map[string]bool) {
	names := make([]string, 0, len(resp.Result().Header))
	for k := range resp.Result().Header {
		names = append(names, http.CanonicalHeaderKey(k))
	}
	slices.Sort(names)
	for _, h := range names {
		if allowed[h] {
			continue
		}
		if strings.HasPrefix(h, "Quader-") {
			r.failf("content case %q (route %q, %s): the %d answer carries %s, a header with Quader's prefix that a format cannot declare and a Bastion's cached answer drops: leave it out of what a Bastion keeps, or make it a per-request header of the core (catalog.PerRequestHeaders)",
				c.Name, rt.Name, rt.Kind, resp.Code, h)
			continue
		}
		r.failf("content case %q (route %q, %s): the %d answer carries %s, which is neither a representation nor a per-request header of the core nor declared in catalog.Facts.Bastion.ReplayHeaders, so a Bastion's cached answer drops it: declare it if every reader of the answer may receive it, or leave it out",
			c.Name, rt.Name, rt.Kind, resp.Code, h)
	}
}

// contentAddressed is the duty of a Download answer: Content-Length, and a strong ETag that is the SHA-256 of the body.
func contentAddressed(method string, resp *Response) []string {
	hd := resp.Result().Header
	etag := hd.Get("Etag")
	sum, form := contentAddressOf(etag)
	var out []string
	if !form {
		out = append(out, fmt.Sprintf("the %d answer has the ETag %q, which is no content address (\"<sha256 hex>\" or \"sha256:<sha256 hex>\"), so a Bastion passes its bytes through on every request: serve stored items through Host.Items().Serve, which sets it", resp.Code, etag))
	}
	if resp.Code != http.StatusOK {
		return out // a 206 or 304 carries no whole body to compare
	}
	n, err := strconv.ParseInt(hd.Get("Content-Length"), 10, 64)
	switch {
	case err != nil || n < 0:
		out = append(out, fmt.Sprintf("the 200 answer has no Content-Length (%q), so a Bastion passes its bytes through on every request", hd.Get("Content-Length")))
	case method == http.MethodGet && n != int64(resp.Body.Len()):
		out = append(out, fmt.Sprintf("the 200 answer says Content-Length %d and carries %d bytes", n, resp.Body.Len()))
	}
	if form && method == http.MethodGet {
		if got := sha256.Sum256(resp.Body.Bytes()); hex.EncodeToString(got[:]) != sum {
			out = append(out, fmt.Sprintf("the ETag %s names another SHA-256 than the body's (%x): a Bastion verifies the bytes against it and keeps nothing", etag, got))
		}
	}
	return out
}

// contentAddressOf reads the SHA-256 a strong ETag names: "<64 hex>" (R-STOR-012) or "sha256:<64 hex>" (the registry's spelling,
// R-OCI-008), lower case. ok is false for a weak tag and any other value.
func contentAddressOf(etag string) (sum string, ok bool) {
	if len(etag) < 2 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		return "", false
	}
	v := strings.TrimPrefix(etag[1:len(etag)-1], "sha256:")
	if len(v) != sha256.Size*2 || strings.ToLower(v) != v {
		return "", false
	}
	if _, err := hex.DecodeString(v); err != nil {
		return "", false
	}
	return v, true
}

// revalidates is the duty of a document: an ETag, and 304 to the same request with that ETag in If-None-Match, on the Host that
// served it.
func revalidates(h *Host, c ContentCase, resp *Response) []string {
	etag := resp.Result().Header.Get("Etag")
	if etag == "" {
		return []string{"the document carries no ETag, so a Bastion cannot revalidate the copy it keeps and fetches it whole every period: give it a validator and answer If-None-Match with 304"}
	}
	rq, err := h.NewRequest(context.Background(), http.MethodGet, c.Path, bytes.NewReader(c.Body))
	if err != nil {
		return []string{fmt.Sprintf("the conditional request cannot be built: %v", err)}
	}
	rq.Header.Set("If-None-Match", etag)
	again := h.ServeRequest(rq, false)
	if again.Code != http.StatusNotModified {
		return []string{fmt.Sprintf("the document answered %d to If-None-Match %s, its own ETag, want 304: a Bastion revalidates the documents it keeps with conditional requests, and each one costs a whole transfer", again.Code, etag)}
	}
	return nil
}

// hiddenMember is the member repository a failure of the remote engine names in its own detail: a forward of a Bastion reads
// none of it (bastion.md R-BAS-099 row 15).
const hiddenMember = "kit/hidden-remote"

// forwardError is the duty of WriteError towards a forward of a Bastion (R-BAS-099 rows 3, 7 and 15): the format renders its
// error through errs.ProblemFor, so that the answer to a recognized forward (errs.WithBastionForward) names no member repository
// the caller did not address, carries no request ID of the main installation, and, where its shape is problem details, has the
// type about:blank.
func (r *run) forwardError() {
	rec := httptest.NewRecorder()
	ctx := errs.WithBastionForward(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, DefaultBaseURL+"/", nil)
	req.Header.Set("X-Request-Id", "kit-request-1")
	rec.Header().Set("X-Request-Id", "kit-request-1")
	r.f.WriteError(rec, req, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The upstream of "+hiddenMember+" answered 503."))
	var all strings.Builder
	for k, vs := range rec.Header() {
		if k != "X-Request-Id" { // the chain's own header, which a Bastion does not pass on
			all.WriteString(k + ": " + strings.Join(vs, ", ") + "\n")
		}
	}
	all.Write(rec.Body.Bytes())
	switch {
	case strings.Contains(all.String(), hiddenMember):
		r.failf("WriteError names the member repository of a remote's failure in the answer to a forward of a Bastion (%q): render the error through errs.ProblemFor, which gives a forward the neutral detail (R-BAS-099)", all.String())
	case strings.Contains(all.String(), "kit-request-1"):
		r.failf("WriteError names the main installation's request ID in the answer to a forward of a Bastion: render the error through errs.ProblemFor (R-BAS-099)")
	case strings.Contains(all.String(), errs.ProblemTypePrefix):
		r.failf("WriteError gives the answer to a forward of a Bastion the problem type %s…, a path of the management API: render the error through errs.ProblemFor, which gives it about:blank (R-BAS-099)", errs.ProblemTypePrefix)
	}
}

// longestBastionWait is the longest wait a Bastion's own refusal names: a block at its default max_duration (R-BAS-037).
const longestBastionWait = 24 * time.Hour

// rateLimitStatus is the duty of WriteError: a rate limit answers the status the format's facts declare, and tells the client
// the wait capped by the longest wait they declare (RetryAfterMax, R-BAS-058): a second as a second, a day as the cap, or as a
// day when the format declares none. A Bastion's own refusals on the format's family apply the declared cap, so a cap
// WriteError applies and the facts do not declare would reach the format's clients from a Bastion uncapped.
func (r *run) rateLimitStatus() {
	want := r.d.Bastion.RateLimited()
	for _, wait := range []time.Duration{time.Second, longestBastionWait} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, DefaultBaseURL+"/", nil)
		r.f.WriteError(rec, req, errs.E(errs.RateLimited, errs.CodeRateLimited, "Too many requests from this address.",
			errs.WithRetryAfter(wait)))
		if rec.Code != want {
			r.failf("WriteError answers a rate limit %d and catalog.Facts.Bastion says %d: a Bastion's own rate-limit refusals on the format's family take the declared status, so declare the status WriteError renders (RateLimitedStatus, 429 or 503)",
				rec.Code, want)
			return
		}
		told := strconv.FormatInt(int64(r.d.Bastion.CapRetryAfter(wait)/time.Second), 10)
		if got := rec.Header().Get("Retry-After"); got != told {
			r.failf("WriteError tells a rate limit of %s to wait Retry-After %q and catalog.Facts.Bastion says %s (RetryAfterMax %s): a Bastion's own refusals on the format's family tell the declared wait, so declare the cap WriteError applies, or 0 when it tells the true wait",
				wait, got, told, r.d.Bastion.RetryAfterMax)
		}
	}
}
