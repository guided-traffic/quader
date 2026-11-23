package downloadlinks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_STOR_074_TheTestLink: the test writes the 44-byte probe below _probe/link-test- on the connection, signs a link to it against
// the address as typed with a life of 60 s and the file name quader-link-test.txt, answers it with the curl command, and deletes the
// probe when the link's life is over and the probes of earlier tests older than an hour; an encrypted connection is tested too; an
// address a save refuses, a connection the replica does not have and a connection whose circuit is open are refused (Q-UI-952).
func TestR_STOR_074_TheTestLink(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, func(id string, c *config.StorageConnection) {
		if id == "second" {
			c.Encrypted = true
		}
	})
	var scheduled []func()
	var delays []time.Duration
	r.links.afterFunc = func(d time.Duration, f func()) { delays, scheduled = append(delays, d), append(scheduled, f) }
	r.stores["plain"].Set("quader/_probe/link-test-0000", []byte("left"), time.Now().Add(-2*time.Hour))
	r.stores["plain"].Set("quader/_probe/link-test-0001", []byte("young"), time.Now().Add(-10*time.Minute))

	link, err := r.links.TestLink(ctx, "plain", "https://objects.example.com")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("R-STOR-074 the link names the address as typed, lives 60 s and serves the probe as quader-link-test.txt", func(t *testing.T) {
		u, _ := url.Parse(link.URL)
		q := u.Query()
		if u.Host != "objects.example.com" || !strings.HasPrefix(u.Path, "/quader/_probe/link-test-") || q.Get("X-Amz-Expires") != "60" ||
			q.Get("response-content-disposition") != `attachment; filename="quader-link-test.txt"` || q.Get("response-cache-control") != CacheControl {
			t.Fatalf("%s", link.URL)
		}
		if link.Command != "curl -fsS '"+link.URL+"'" || time.Until(link.ExpiresAt) > time.Minute || time.Until(link.ExpiresAt) < 50*time.Second {
			t.Fatalf("%+v", link)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
		resp, err := r.stores["plain"].Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != TestLinkBody || len(body) != 44 {
			t.Fatalf("%d %q", resp.StatusCode, body)
		}
	})
	t.Run("R-STOR-074 the probe goes when the link's life is over, and a probe older than an hour went before", func(t *testing.T) {
		keys := r.stores["plain"].Keys("quader/_probe/link-test-")
		if len(keys) != 2 || !slices.Contains(keys, "quader/_probe/link-test-0001") || slices.Contains(keys, "quader/_probe/link-test-0000") {
			t.Fatalf("before the life is over: %v", keys)
		}
		if len(scheduled) != 1 || delays[0] > time.Minute {
			t.Fatalf("scheduled %d, %v", len(scheduled), delays)
		}
		scheduled[0]()
		if keys := r.stores["plain"].Keys("quader/_probe/link-test-"); len(keys) != 1 || keys[0] != "quader/_probe/link-test-0001" {
			t.Fatalf("after: %v", keys)
		}
	})
	t.Run("R-STOR-096 an encrypted connection is tested too", func(t *testing.T) {
		if _, err := r.links.TestLink(ctx, "second", "https://objects.example.com"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("R-STOR-074 an address a save refuses, an unknown connection and an open circuit are refused", func(t *testing.T) {
		var ae *AddressError
		for _, addr := range []string{"http://objects.example.com", "https://s3.quader.example.com", "https://objects.example.com/bucket"} {
			if _, err := r.links.TestLink(ctx, "plain", addr); !errors.As(err, &ae) {
				t.Errorf("%s: %v", addr, err)
			}
		}
		if _, err := r.links.TestLink(ctx, "nope", "https://objects.example.com"); !errors.Is(err, ErrUnknownConnection) {
			t.Errorf("unknown: %v", err)
		}
		r.stores["plain"].SetFault(func(string, string) error {
			return &blob.StoreError{Op: "HeadObject", Kind: blob.KindTransient, Status: 503}
		})
		c, _ := r.conns.Get("plain")
		_, _ = c.Objects().Head(ctx, "k")
		if _, err := r.links.TestLink(ctx, "plain", "https://objects.example.com"); !errors.Is(err, ErrConnectionUnavailable) {
			t.Errorf("open circuit: %v", err)
		}
	})
}
