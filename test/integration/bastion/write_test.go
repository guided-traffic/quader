//go:build integration

package bastion

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// readOnlyText is the detail of the refusal of a write through a Bastion (R-BAS-099 row 1, Q-BAS-321).
const readOnlyText = "This address is read-only: it serves downloads and accepts no uploads."

// relaxed raises the flood figures of the Bastion's configuration (R-BAS-093) so that a loop of requests in a test is not the
// limit's subject.
func relaxed(c *iface.Configuration) {
	f := &c.Limits.Flood
	f.Anonymous = iface.FloodClass{RequestsPerSecond: 100000, Burst: 100000, MissesPerMinute: 100000}
	f.Authenticated = iface.FloodClass{RequestsPerSecond: 100000, Burst: 100000, MissesPerMinute: 100000}
}

// then applies the changes of a configuration in their order.
func then(fs ...func(*iface.Configuration)) func(*iface.Configuration) {
	return func(c *iface.Configuration) {
		for _, f := range fs {
			f(c)
		}
	}
}

// noBlock raises the blocking threshold, so that the refusals a test counts never block its address.
func noBlock(c *iface.Configuration) { c.Limits.Flood.Blocking.Threshold = 100000 }

// TestR_BAS_095_WritesAreRefusedAtTheBastion: an `npm publish`-shaped PUT and a `nuget push`-shaped PUT, with the publisher's token
// and without a credential, are refused 403 read_only by the Bastion itself in the shape each format declares, naming no address
// (R-BAS-099 row 1); the Bastion reads none of the upload body and asks the main installation nothing, which records no
// package.published. A read route that takes a body (npm's bulk advisories) is forwarded with its body and served by the main
// installation.
func TestR_BAS_095_WritesAreRefusedAtTheBastion(t *testing.T) {
	w := newWorld(t, worldOptions{})
	w.content()
	b := w.newBastion(t, bastionOptions{Configure: relaxed}).wait()
	r := b.reps[0]
	published := w.st.Number(`SELECT count(*) FROM audit_event WHERE action IN ('package.published', 'npm.publish')`)
	m := w.front.Mark()

	for name, tok := range map[string]string{"with the publisher's token": w.publisher, "without a credential": ""} {
		t.Run("npm publish "+name, func(t *testing.T) {
			got := r.do(t, "PUT", "/api/npm/"+npmAll+"/"+escapeNPM("@acme/sdk"), callOpts{token: tok, addr: "198.51.100.91", body: npmPublishBody(t, "@acme/sdk", "9.0.0"), header: []string{"Content-Type", "application/json"}})
			if got.Status != http.StatusForbidden || got.Code() != "read_only" || got.Header.Get("Content-Type") != "application/json" ||
				!bytes.Contains(got.Body, []byte(`"error":"`+readOnlyText+`"`)) {
				t.Fatalf("npm publish: %d %v %s", got.Status, got.Header, got.Body)
			}
		})
		t.Run("nuget push "+name, func(t *testing.T) {
			body, ct := nugetForm(t, nupkg(t, "Acme.Sdk", "9.0.0"))
			got := r.do(t, "PUT", "/api/nuget/"+ngAll+"/v3/package", callOpts{token: tok, addr: "198.51.100.92", body: body, header: []string{"Content-Type", ct}})
			if got.Status != http.StatusForbidden || string(got.Body) != "read_only: "+readOnlyText || !strings.HasPrefix(got.Header.Get("Content-Type"), "text/plain") {
				t.Fatalf("nuget push: %d %q, Content-Type %q", got.Status, got.Body, got.Header.Get("Content-Type"))
			}
		})
	}
	t.Run("less than 64 KiB of the body are read", func(t *testing.T) {
		// The client announces 4 MiB and sends 1 KiB. A Bastion that read the body would wait for the rest.
		c, err := net.Dial("tcp", strings.TrimPrefix(r.client, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(20 * time.Second))
		head := "PUT /api/npm/" + npmAll + "/x HTTP/1.1\r\nHost: " + baseHost + "\r\nAuthorization: Bearer " + w.publisher + "\r\nX-Forwarded-For: 198.51.100.93\r\nContent-Type: application/json\r\nContent-Length: 4194304\r\n\r\n"
		if _, err := c.Write(append([]byte(head), bytes.Repeat([]byte("x"), 1024)...)); err != nil {
			t.Fatal(err)
		}
		res, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			t.Fatalf("no answer before the body was sent: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d", res.StatusCode)
		}
	})
	t.Run("the main installation was asked nothing and recorded no publish", func(t *testing.T) {
		if c := w.front.Cost(m); c != 0 {
			t.Fatalf("the writes cost the main installation %d requests: %+v", c, w.front.Since(m))
		}
		if got := w.st.Number(`SELECT count(*) FROM audit_event WHERE action IN ('package.published', 'npm.publish')`); got != published {
			t.Fatalf("publish events: %d before, %d after", published, got)
		}
	})
	t.Run("a read route with a body", func(t *testing.T) {
		path := "/api/npm/" + npmAll + "/-/npm/v1/security/advisories/bulk"
		body := []byte(`{"@acme/sdk":["1.0.0"],"plain-lib":["1.0.0"]}`)
		m := w.front.Mark()
		got := r.do(t, "POST", path, callOpts{token: w.full.token, addr: "198.51.100.95", body: body, header: []string{"Content-Type", "application/json"}})
		want := w.api("POST", path, w.full.token, body)
		if got.Status != want.Status || want.Status != http.StatusOK || !bytes.Equal(bytes.TrimSpace(got.Body), bytes.TrimSpace(want.Body)) {
			t.Fatalf("bulk advisories: Bastion %d %.200s, main installation %d %.200s", got.Status, got.Body, want.Status, want.Body)
		}
		// The format table names the route a read route with a body (R-BAS-095): it is forwarded with its body at once.
		if fw := w.front.Forwards(m, path); len(fw) != 1 || fw[0].Method != "POST" {
			t.Fatalf("%d forwards of the bulk advisories, want one POST: %+v", len(fw), fw)
		}
	})
}
