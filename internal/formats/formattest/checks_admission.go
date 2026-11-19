package formattest

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
)

// admissionRefusal is the refusal of the download limits the host of check 16 answers every admission with: the 429 of a used-up
// hour budget (R-LIM-020).
func admissionRefusal() error {
	return errs.E(errs.RateLimited, "download_limit_reached", "The download limit of 1 MB per hour is used up. Try again in 60 seconds.",
		errs.WithRetryAfter(time.Minute), errs.WithExtension("figure", "cachedBytesPerHour"))
}

// admission is check 16 (usage-and-limits.md R-LIM-036): every content case of a Download route that the fixture serves (a GET
// with a status below 400) runs again against a host whose admission refuses. The answer must be 429 with Retry-After, and
// no item may have been served: a format that writes a download's body itself, instead of answering it through Host.Serve or
// the remote engine, escapes the admission and the pacing of the download limits, and fails here.
func (r *run) admission() {
	ran := map[string]bool{}
	for _, c := range r.fx.Content {
		method := c.Method
		if method == "" {
			method = http.MethodGet
		}
		if method != http.MethodGet || (c.Status != 0 && c.Status >= http.StatusBadRequest) {
			continue
		}
		var o HostOptions
		if r.fx.Host != nil {
			o = r.fx.Host()
		}
		admitted := 0
		o.Admit = func(context.Context, *formats.Request) error {
			admitted++
			return admissionRefusal()
		}
		h := NewHost(r.f, o)
		if c.Seed != nil {
			if err := c.Seed(h); err != nil {
				r.failf("content case %q: the seed fails: %v", c.Name, err)
				continue
			}
		}
		rq, err := h.NewRequest(context.Background(), method, c.Path, bytes.NewReader(c.Body))
		if err != nil {
			continue // a case that matches no route is check 7's finding
		}
		if rq.Route.Kind != formats.Download {
			continue
		}
		ran[rq.Route.Name] = true
		resp := h.ServeRequest(rq, false)
		switch {
		case admitted == 0:
			r.failf("content case %q: route %s answered a download without the admission of Host.Serve (status %d)", c.Name, rq.Route.Name, resp.Code)
		case resp.Code != http.StatusTooManyRequests:
			r.failf("content case %q: route %s answered %d to a refused admission, want 429", c.Name, rq.Route.Name, resp.Code)
		case resp.Header().Get("Retry-After") == "":
			r.failf("content case %q: route %s answered 429 without Retry-After", c.Name, rq.Route.Name)
		}
		h.mu.Lock()
		served := h.served
		h.mu.Unlock()
		if served {
			r.failf("content case %q: route %s served the item after the admission refused", c.Name, rq.Route.Name)
		}
	}
	for _, rt := range r.f.Routes() {
		if rt.Kind != formats.Download || rt.Method != http.MethodGet || ran[rt.Name] {
			continue
		}
		if _, listed := r.fx.NoContent[rt.Name]; listed {
			continue
		}
		r.failf("route %s of kind Download has no content case the fixture serves: check 16 cannot hold its admission", rt.Name)
	}
}
