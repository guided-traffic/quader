package v1

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/errs"
)

// Tests of the operations of the delivery by signed links (storage.md R-STOR-014, R-STOR-074, Q-API-400) over the replica's side
// on in-memory connections and a store of the row in memory; the statements of the row are the integration test's
// (test/integration/blob/links_test.go).

// linkStoreFake is the row in memory: a write with another revision is stale, a write that changes nothing keeps the revision.
type linkStoreFake struct {
	mu      sync.Mutex
	row     downloadlinks.Settings
	actors  []audit.Actor
	updates int
}

func (f *linkStoreFake) Settings(context.Context) (downloadlinks.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.row
	out.PublicAddresses = maps.Clone(f.row.PublicAddresses)
	return out, nil
}

func (f *linkStoreFake) Update(_ context.Context, actor audit.Actor, next downloadlinks.Settings, version int64) (downloadlinks.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if version != f.row.Version {
		return downloadlinks.Settings{}, errs.Stale("The download link settings", f.row.Version)
	}
	next.Version, next.UpdatedAt, next.UpdatedByName = version+1, time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC), actor.Kind+":"+actor.Name
	next.PublicAddresses = maps.Clone(next.PublicAddresses)
	f.row, f.actors, f.updates = next, append(f.actors, actor), f.updates+1
	return next, nil
}

// linkRig is the replica's side over three in-memory connections: plain (https, its store honours the overrides), insecure (an
// http endpoint without an address) and dropping (its store drops Content-Disposition), with Quader at quader.example.com.
type linkRig struct {
	links  *downloadlinks.Links
	stores map[string]*blobtest.Store
	conns  *blob.Connections
	row    *linkStoreFake
}

func newLinkRig(t *testing.T) *linkRig {
	t.Helper()
	r := &linkRig{stores: map[string]*blobtest.Store{}}
	var list []*blob.Connection
	for _, id := range []string{"plain", "insecure", "dropping"} {
		m := blobtest.New()
		r.stores[id] = m
		p := "quader"
		el := config.StorageConnection{ID: id, Bucket: "blobs", Prefix: &p, Endpoint: "https://" + blobtest.PresignHost, ForcePathStyle: true}
		if id == "insecure" {
			el.Endpoint, el.AllowInsecureEndpoint = "http://store.internal:9000", true
		}
		if id == "dropping" {
			m.IgnoreOverride("response-content-disposition")
		}
		list = append(list, blob.NewConnection(el, blob.NewGuard(m, config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute})))
	}
	conns, err := blob.NewConnections(list...)
	if err != nil {
		t.Fatal(err)
	}
	r.conns = conns
	r.links = downloadlinks.New(downloadlinks.Options{Connections: conns, Rule: downloadlinks.HostRuleOf([]string{"quader.example.com"}), Replica: "r1"})
	row := downloadlinks.Defaults()
	row.Version, row.UpdatedAt = 1, time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	r.row = &linkStoreFake{row: row}
	r.links.SetSettings(row)
	r.links.RunLinkChecks(context.Background())
	return r
}

// linkCall sends one request with an optional JSON body and If-Match.
func linkCall(h http.Handler, method, path, body, ifMatch string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	switch method {
	case http.MethodPatch:
		req.Header.Set("Content-Type", "application/merge-patch+json")
	case http.MethodPost:
		req.Header.Set("Content-Type", "application/json")
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// linkPointers are the pointers and codes of a problem's errors.
func linkPointers(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var p struct {
		Code   string `json:"code"`
		Errors []struct{ Pointer, Code string }
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	out := map[string]string{"": p.Code}
	for _, e := range p.Errors {
		out[e.Pointer] = e.Code
	}
	return out
}

func linkConnection(t *testing.T, s gen.DownloadLinkSettings, name string) gen.DownloadLinkConnection {
	t.Helper()
	for _, c := range s.Connections {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no connection %s in %+v", name, s.Connections)
	return gen.DownloadLinkConnection{}
}

// TestR_STOR_014_TheOperations: the settings are read with their ETag and the routes whose clients check, the switch by every
// signed-in caller with the formats; a change needs If-Match (428 without, 412 stale), settings:write (403), values in their
// ranges (422 at the pointer) and only the members of the patch (400 for another); a null address returns the connection to its
// endpoint; the answer is the row after the change.
func TestR_STOR_014_TheOperations(t *testing.T) {
	val := newValidator(t)
	r := newLinkRig(t)
	s := newSetup(t, func(o *Options) {
		o.API.Config = config.API{MaxRequestBodyBytes: 1 << 20}
		o.DownloadLinks = DownloadLinksServices{Store: r.row, Links: r.links}
	}, authed)
	h := val.Handler(t, s.handler)

	t.Run("R-STOR-014 the settings carry their ETag, the defaults and the routes whose clients check", func(t *testing.T) {
		w := linkCall(h, http.MethodGet, "/api/v1/download-links/settings", "", "")
		if w.Code != http.StatusOK || w.Header().Get("ETag") != `"1"` {
			t.Fatalf("%d %q %s", w.Code, w.Header().Get("ETag"), w.Body)
		}
		var got gen.DownloadLinkSettings
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Enabled || got.MinSize != 1048576 || got.LinkLifeSeconds != 60 || len(got.Connections) != 3 || got.UpdatedBy != nil {
			t.Fatalf("%+v", got)
		}
		routes := map[string]string{}
		for _, rt := range got.Routes {
			routes[rt.Route] = rt.Format
		}
		if routes["oci.blob.get"] != "oci" || routes["ansible.artifact.get"] != "ansible" || routes["npm.tarball.get"] != "" {
			t.Fatalf("routes %+v", got.Routes)
		}
	})
	t.Run("R-STOR-014 every signed-in caller reads the switch and the formats it reaches", func(t *testing.T) {
		w := linkCall(h, http.MethodGet, "/api/v1/download-links/switch", "", "")
		if w.Code != http.StatusOK || w.Body.String() != `{"enabled":false,"formats":["ansible","oci"]}`+"\n" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
	t.Run("R-STOR-014 a change without If-Match is 428, with a stale one 412", func(t *testing.T) {
		if w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"enabled":true}`, ""); w.Code != http.StatusPreconditionRequired {
			t.Errorf("without: %d %s", w.Code, w.Body)
		}
		if w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"enabled":true}`, `"7"`); w.Code != http.StatusPreconditionFailed {
			t.Errorf("stale: %d %s", w.Code, w.Body)
		}
		if r.row.updates != 0 {
			t.Fatalf("written %d times", r.row.updates)
		}
	})
	t.Run("R-STOR-014 values outside their ranges are 422 at their pointers, another member 400", func(t *testing.T) {
		w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"minSize":-1,"linkLifeSeconds":5,"publicAddresses":{"nope":"https://objects.example.com"}}`, `"1"`)
		got := linkPointers(t, w.Body.Bytes())
		if w.Code != http.StatusUnprocessableEntity || got["/minSize"] != "invalid_value" || got["/linkLifeSeconds"] != "invalid_value" || got["/publicAddresses/nope"] != "invalid_value" {
			t.Fatalf("%d %v", w.Code, got)
		}
		if w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"routes":[]}`, `"1"`); w.Code != http.StatusBadRequest {
			t.Fatalf("another member: %d %s", w.Code, w.Body)
		}
	})
	t.Run("R-STOR-014 an administrator's change answers the row after it; a null returns the connection to its endpoint", func(t *testing.T) {
		w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"enabled":true,"minSize":0,"publicAddresses":{"plain":"https://objects.example.com"}}`, `"1"`)
		if w.Code != http.StatusOK || w.Header().Get("ETag") != `"2"` {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var got gen.DownloadLinkSettings
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if plain := linkConnection(t, got, "plain"); !got.Enabled || got.MinSize != 0 || plain.PublicAddress == nil || *plain.PublicAddress != "https://objects.example.com" ||
			got.UpdatedBy == nil || *got.UpdatedBy != "user:alice" {
			t.Fatalf("%+v", got)
		}
		w = linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"publicAddresses":{"plain":null}}`, `"2"`)
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if w.Code != http.StatusOK || linkConnection(t, got, "plain").PublicAddress != nil || !got.Enabled {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		if r.row.actors[0].Name != "alice" || r.row.updates != 2 {
			t.Fatalf("%+v", r.row.actors)
		}
	})
	t.Run("R-STOR-014 a caller without settings:write is refused the change and the test link", func(t *testing.T) {
		deny := newSetup(t, func(o *Options) {
			o.Access = denyCapability("settings:write")
			o.API.Config = config.API{MaxRequestBodyBytes: 1 << 20}
			o.DownloadLinks = DownloadLinksServices{Store: r.row, Links: r.links}
		}, authed)
		dh := val.Handler(t, deny.handler)
		if w := linkCall(dh, http.MethodPatch, "/api/v1/download-links/settings", `{"enabled":false}`, `"3"`); w.Code != http.StatusForbidden {
			t.Errorf("patch: %d %s", w.Code, w.Body)
		}
		if w := linkCall(dh, http.MethodPost, "/api/v1/download-links/test", `{"connection":"plain","publicAddress":"https://objects.example.com"}`, ""); w.Code != http.StatusForbidden {
			t.Errorf("test: %d %s", w.Code, w.Body)
		}
	})
	t.Run("R-STOR-014 without the area the operations answer 503", func(t *testing.T) {
		none := newSetup(t, nil, authed)
		if w := linkCall(none.handler, http.MethodGet, "/api/v1/download-links/switch", "", ""); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%d", w.Code)
		}
	})
}

// TestR_STOR_074_TheConnectionAndItsAddress: the settings' answer names quader_host for an address below server.base_url, not_https
// for an http endpoint without an address, and link_check_failed for a store that drops Content-Disposition; a PATCH with such an
// address answers 422 at /publicAddresses/plain; the test link answers a link signed against the entered address, 422 for an address
// a save refuses and a connection the installation does not have, and 409 while the circuit is open.
func TestR_STOR_074_TheConnectionAndItsAddress(t *testing.T) {
	val := newValidator(t)
	r := newLinkRig(t)
	s := newSetup(t, func(o *Options) {
		o.API.Config = config.API{MaxRequestBodyBytes: 1 << 20}
		o.DownloadLinks = DownloadLinksServices{Store: r.row, Links: r.links}
	}, authed)
	h := val.Handler(t, s.handler)
	// An address below the base URL saved before the host was Quader's: the row holds it and the replica's snapshot follows.
	// The row also holds the address of a connection the configuration no longer has.
	r.row.row.PublicAddresses = map[string]string{"plain": "https://s3.quader.example.com", "gone": "https://old.example.com"}
	r.links.SetSettings(r.row.row)

	t.Run("R-STOR-074 each connection names its reasons", func(t *testing.T) {
		w := linkCall(h, http.MethodGet, "/api/v1/download-links/settings", "", "")
		var got gen.DownloadLinkSettings
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		codes := func(c gen.DownloadLinkConnection) string {
			var out []string
			for _, rs := range c.Reasons {
				out = append(out, string(rs.Code))
			}
			return strings.Join(out, ",")
		}
		plain, insecure, dropping := linkConnection(t, got, "plain"), linkConnection(t, got, "insecure"), linkConnection(t, got, "dropping")
		if plain.Usable || codes(plain) != "quader_host" || plain.Reasons[0].Detail == nil || *plain.Reasons[0].Detail != "quader.example.com" {
			t.Errorf("plain %+v", plain)
		}
		if insecure.Usable || !strings.HasPrefix(codes(insecure), "not_https") || insecure.Endpoint != "http://store.internal:9000" || insecure.PublicAddress != nil {
			t.Errorf("insecure %+v", insecure)
		}
		if dropping.Usable || codes(dropping) != "link_check_failed" || !strings.Contains(*dropping.Reasons[0].Detail, "Content-Disposition") || !dropping.HandoverUsable {
			t.Errorf("dropping %+v", dropping)
		}
		// Q-UI-996: the page words the reason from its fields, not from the English detail.
		if c := dropping.Reasons[0].Check; c == nil || c.Outcome != gen.OverrideIgnored || c.Header == nil || *c.Header != "Content-Disposition" ||
			c.ExpectedValue == nil || !strings.HasPrefix(*c.ExpectedValue, "attachment;") || c.Status != nil {
			t.Errorf("dropping's check %+v", c)
		}
		if plain.Reasons[0].Check != nil {
			t.Errorf("a reason other than link_check_failed carries a check: %+v", plain.Reasons[0].Check)
		}
	})
	t.Run("R-STOR-074 a PATCH with an address below a host of Quader is 422 at its pointer", func(t *testing.T) {
		w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"publicAddresses":{"dropping":"https://blobs.quader.example.com"}}`, `"1"`)
		if got := linkPointers(t, w.Body.Bytes()); w.Code != http.StatusUnprocessableEntity || got["/publicAddresses/dropping"] != "invalid_value" {
			t.Fatalf("%d %v", w.Code, got)
		}
	})
	t.Run("R-STOR-014 Q-STOR-355 a stored address the write leaves alone is not checked again; one it names is", func(t *testing.T) {
		if w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"minSize":0}`, `"1"`); w.Code != http.StatusOK {
			t.Fatalf("a change of the size: %d %s", w.Code, w.Body)
		}
		w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"publicAddresses":{"gone":"https://new.example.com"}}`, `"2"`)
		if got := linkPointers(t, w.Body.Bytes()); w.Code != http.StatusUnprocessableEntity || got["/publicAddresses/gone"] != "invalid_value" {
			t.Fatalf("%d %v", w.Code, got)
		}
		if w := linkCall(h, http.MethodPatch, "/api/v1/download-links/settings", `{"publicAddresses":{"gone":null}}`, `"2"`); w.Code != http.StatusOK {
			t.Fatalf("removing it: %d %s", w.Code, w.Body)
		}
	})
	t.Run("R-STOR-074 the test link is signed against the address as typed, unsaved", func(t *testing.T) {
		w := linkCall(h, http.MethodPost, "/api/v1/download-links/test", `{"connection":"dropping","publicAddress":"https://objects.example.com:8443"}`, "")
		var got gen.DownloadLinkTest
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		u, _ := url.Parse(got.Url)
		if u.Host != "objects.example.com:8443" || !strings.HasPrefix(u.Path, "/quader/_probe/link-test-") || got.Command != "curl -fsS '"+got.Url+"'" {
			t.Fatalf("%+v", got)
		}
		if keys := r.stores["dropping"].Keys("quader/_probe/link-test-"); len(keys) != 1 {
			t.Fatalf("probes %v", keys)
		}
	})
	t.Run("R-STOR-074 an address a save refuses and an unknown connection are 422, an open circuit 409", func(t *testing.T) {
		w := linkCall(h, http.MethodPost, "/api/v1/download-links/test", `{"connection":"plain","publicAddress":"http://objects.example.com"}`, "")
		if got := linkPointers(t, w.Body.Bytes()); w.Code != http.StatusUnprocessableEntity || got["/publicAddress"] != "invalid_value" {
			t.Errorf("address: %d %v", w.Code, got)
		}
		w = linkCall(h, http.MethodPost, "/api/v1/download-links/test", `{"connection":"nope","publicAddress":"https://objects.example.com"}`, "")
		if got := linkPointers(t, w.Body.Bytes()); w.Code != http.StatusUnprocessableEntity || got["/connection"] != "invalid_value" {
			t.Errorf("connection: %d %v", w.Code, got)
		}
		r.stores["plain"].SetFault(func(string, string) error {
			return &blob.StoreError{Op: "HeadObject", Kind: blob.KindTransient, Status: 503}
		})
		c, _ := r.conns.Get("plain")
		_, _ = c.Objects().Head(context.Background(), "k")
		w = linkCall(h, http.MethodPost, "/api/v1/download-links/test", `{"connection":"plain","publicAddress":"https://objects.example.com"}`, "")
		if got := linkPointers(t, w.Body.Bytes()); w.Code != http.StatusConflict || got[""] != "connection_unavailable" {
			t.Errorf("open circuit: %d %s", w.Code, w.Body)
		}
	})
}

// TestQ_UI_996_TheLinkChecksFields: each outcome of a failed link check carries the fields its sentence needs and no others.
func TestQ_UI_996_TheLinkChecksFields(t *testing.T) {
	if c := downloadLinkCheck(&blob.LinkCheck{}); c == nil || c.Outcome != gen.NotRun || c.Status != nil {
		t.Errorf("not run %+v", c)
	}
	if c := downloadLinkCheck(&blob.LinkCheck{Ran: true, Detail: "the link was not answered: timeout"}); c == nil || c.Outcome != gen.NoAnswer {
		t.Errorf("no answer %+v", c)
	}
	c := downloadLinkCheck(&blob.LinkCheck{Ran: true, Answered: true, Status: 403, Bytes: 0, WantBytes: 2048})
	if c == nil || c.Outcome != gen.UnexpectedAnswer || *c.Status != 403 || *c.Bytes != 0 || *c.ExpectedStatus != 200 || *c.ExpectedBytes != 2048 || c.Header != nil {
		t.Errorf("unexpected answer %+v", c)
	}
	if downloadLinkCheck(nil) != nil {
		t.Error("a reason without a check carries one")
	}
}
