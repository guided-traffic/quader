package formattest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
)

// CheckLinks is check 20 (storage.md R-STOR-071, ADR-243): a route that declares that every client of the format checks a digest
// (Route.ClientDigest.Checks) answers its downloads through Host.Items().Serve, the one place where the serve point of R-STOR-070 can
// hand a download to a signed link of the object store, and its answer is the stored blob under the blob's content address, so that
// the bytes a client checks on a link are the ones it would have received from Quader.
const CheckLinks Check = 20

// links is check 20. It needs no PostgreSQL and serves every GET content case of a declaring route once more, on a fresh Host seeded as
// the fixture says (Q-STOR-295):
//
//   - every 200 answer was served through Host.Items().Serve (the Host recorded the item): a handler that writes the bytes itself never
//     reaches the decision of R-STOR-070, so its declaration would say something the serve point cannot act on;
//   - it carries a body, and a strong ETag that is a content address, "<sha256 hex>" or "sha256:<sha256 hex>", equal to the SHA-256 of
//     the body;
//   - the body is the stored blob Items().Serve served: a link hands the client the object as stored, so an answer the format changed
//     on its way out would be refused by the client on a link;
//   - every declaring route has at least one such case.
//
// Unlike check 19 it has no fixture field for exemptions, and NoContent and NoBastion, the exemptions of checks 7, 16 and 19, do not
// reach it: a route whose answers cannot meet it does not declare. A format that declares nothing passes it.
func (r *run) links() {
	var declared []string
	for _, rt := range r.f.Routes() {
		if rt.ClientDigest.Checks {
			declared = append(declared, rt.Name)
		}
	}
	if len(declared) == 0 {
		return
	}
	served := map[string]bool{}
	for _, c := range r.fx.Content {
		if (c.Method != "" && c.Method != http.MethodGet) || (c.Status != 0 && c.Status >= http.StatusBadRequest) {
			continue
		}
		h := r.host("")
		if c.Seed != nil {
			if err := c.Seed(h); err != nil {
				r.failf("content case %q: the seed fails: %v", c.Name, err)
				continue
			}
		}
		rq, err := h.NewRequest(context.Background(), http.MethodGet, c.Path, bytes.NewReader(c.Body))
		if err != nil || !rq.Route.ClientDigest.Checks {
			continue // a case that matches no route is check 7's finding
		}
		resp := h.ServeRequest(rq, false)
		if resp.Code != http.StatusOK {
			continue // a case that does not serve is check 7's finding; the route then needs another case below
		}
		served[rq.Route.Name] = true
		for _, d := range storedBlobAnswer(h, resp) {
			r.failf("content case %q (route %q declares ClientDigest.Checks): %s", c.Name, rq.Route.Name, d)
		}
	}
	for _, name := range declared {
		if !served[name] {
			r.failf("route %q declares ClientDigest.Checks and no GET content case answered it 200, so nothing shows that its downloads are the stored blob served through Host.Items().Serve: give it a case that serves a stored item, or take the declaration away (a route whose answers cannot meet check 20 does not declare; NoContent does not exempt it)", name)
		}
	}
}

// storedBlobAnswer is the duty of a 200 of a declaring route: served through Items().Serve, a body, a content-address ETag equal to
// the body's SHA-256, and the body equal to a stored item the request served.
func storedBlobAnswer(h *Host, resp *Response) []string {
	paths := h.ServedPaths()
	if len(paths) == 0 {
		return []string{"the 200 answer was not served through Host.Items().Serve, so the serve point of R-STOR-070 never sees it and no download of the route can be a link: serve the stored item through rq.Host.Items().Serve, or take the declaration away"}
	}
	body := resp.Body.Bytes()
	if len(body) == 0 {
		return []string{"the 200 answer carries no body, so nothing shows that the route's downloads are the stored blob"}
	}
	got := sha256.Sum256(body)
	sum := hex.EncodeToString(got[:])
	var out []string
	etag := resp.Result().Header.Get("Etag")
	switch want, ok := contentAddressOf(etag); {
	case !ok:
		out = append(out, fmt.Sprintf("the 200 answer has the ETag %q, which is no content address (\"<sha256 hex>\" or \"sha256:<sha256 hex>\"): a route whose clients check a digest answers the stored blob under its address, which Host.Items().Serve sets, and NoBastion does not exempt it", etag))
	case want != sum:
		out = append(out, fmt.Sprintf("the ETag %s names another SHA-256 than the body's (%s)", etag, sum))
	}
	if !slices.ContainsFunc(paths, func(p string) bool { stored, ok := h.storedSHA256(p); return ok && stored == sum }) {
		out = append(out, fmt.Sprintf("the body (SHA-256 %s) is none of the stored items Host.Items().Serve served (%v): a link hands the client the object as stored, which is not what the route answers", sum, paths))
	}
	return out
}

// storedSHA256 is the SHA-256 of the stored item at a path, as Items().Serve names it in its ETag; ok is false for no item there.
func (h *Host) storedSHA256(p string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rec, ok := h.items[p]
	if !ok {
		return "", false
	}
	return hex.EncodeToString(rec.digests.SHA256), true
}

// declaresClientDigest names the first route of the format that declares that its clients check a digest, "" for none.
func (r *run) declaresClientDigest() string {
	for _, rt := range r.f.Routes() {
		if rt.ClientDigest.Checks {
			return rt.Name
		}
	}
	return ""
}
