package limits

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// TestR_BAS_099_ALimitsRefusalOfABastionForwardNamesNoPlaceAndNoPage: a refusal of R-LIM-020 that answers a forward of a Bastion
// names neither the place of the main installation that refused nor a page of the main installation's web interface; the figure,
// the limit, the window and the wait stay, since they are the caller's own (bastion.md R-BAS-099 row 7). The engine reads the
// forward from the request's context.
func TestR_BAS_099_ALimitsRefusalOfABastionForwardNamesNoPlaceAndNoPage(t *testing.T) {
	base := refusal{
		code: CodeDownloadLimitReached, figure: CachedBytesPerHour.String(), limit: 100 * mb, window: time.Hour,
		place: Place{Kind: PlaceInstance}, retry: time.Minute, signed: true, baseURL: "https://packages.example.com",
	}
	own := base.err()
	if own.Extensions["place"] == nil || !strings.Contains(own.Detail, "Your limits: https://packages.example.com/ui/profile/usage") {
		t.Fatalf("the refusal of an own caller: %+v", own)
	}
	fwd := base
	fwd.forward = true
	e := fwd.err()
	if _, ok := e.Extensions["place"]; ok || strings.Contains(e.Detail, "Your limits") || strings.Contains(e.Detail, "/ui/") {
		t.Fatalf("the refusal of a forward: %q %v", e.Detail, e.Extensions)
	}
	for _, k := range []string{"figure", "limit", "windowSeconds", "retryAfterSeconds"} {
		if _, ok := e.Extensions[k]; !ok {
			t.Errorf("the refusal of a forward lost %s: %v", k, e.Extensions)
		}
	}

	t.Run("the engine reads the forward from the context", func(t *testing.T) {
		f := BuiltinProfiles()[ProfileAnonymous]
		f[CachedBytesPerHour] = mb
		r := newRig(t, ModeEnforce, f)
		ctx := errs.WithBastionForward(context.Background())
		c := r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
		if err := r.e.Admit(ctx, c); err != nil {
			t.Fatal(err)
		}
		c.AddCached(2 * mb)
		r.e.Finish(c)
		r.e.Flush(ctx)
		c = r.e.Begin(anonCaller("198.51.100.9"), qname.Repo{})
		err := r.e.Admit(ctx, c)
		e, ok := errs.As(err)
		if !ok || e.Code != CodeDownloadLimitReached {
			t.Fatalf("the next download: %v", err)
		}
		if _, has := e.Extensions["place"]; has {
			t.Fatalf("a forward's refusal names the place: %v", e.Extensions)
		}
	})
}
