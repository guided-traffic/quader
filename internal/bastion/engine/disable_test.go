package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// The disable at the replica (R-BAS-133, Q-BAS-514). Unit tests, milliseconds; the downloads are a few KiB held open by the test.

// setDisabled makes the handshake carry the way and lets the link apply it.
func (h *harness) setDisabled(mode iface.DisableMode) {
	h.t.Helper()
	h.main.mu.Lock()
	h.main.hs.Configuration.Disabled = nil
	if mode != "" {
		m := mode
		h.main.hs.Configuration.Disabled = &m
	}
	h.main.hs.Configuration.Revision++
	h.main.mu.Unlock()
	h.link.RequestHandshake()
	h.tick()
	if got := h.link.Configuration().Current().Disabled; got != mode {
		h.t.Fatalf("the replica applies %q, want %q", got, mode)
	}
}

// TestR_BAS_133_ADisabledReplicaAnswers503InTheFormatsShape: while disabled, every new request is answered 503 with the code and
// detail of the Bastion's own unavailable and no word of a disable, with Retry-After 300 capped by the format (NuGet's 30), in the
// format's shape (problem details for npm, the distribution's errors below the registry root), without a request to the main
// installation; the status says the way and nothing running. Enabled again, the replica serves. One case per shape kind.
func TestR_BAS_133_ADisabledReplicaAnswers503InTheFormatsShape(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.setDisabled(iface.DisableSoft)
	before := s.main.count()
	for name, c := range map[string]struct{ path, retry, shape string }{
		"npm, problem details":          {"/api/npm/web/npm-all/lodash", "300", `"code":"unavailable"`},
		"nuget, capped":                 {"/api/nuget/web/nuget-all/v3/index.json", "30", "unavailable"},
		"the registry root, its errors": {"/v2/web/docker-hub/library/alpine/manifests/3", "300", `"errors"`},
	} {
		t.Run(name, func(t *testing.T) {
			w := s.get(c.path, "Authorization", bearer(s.full))
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != c.retry || !strings.Contains(w.Body.String(), c.shape) {
				t.Fatalf("status %d, Retry-After %q, body %s", w.Code, w.Header().Get("Retry-After"), w.Body)
			}
			if strings.Contains(strings.ToLower(w.Body.String()), "disable") {
				t.Errorf("the answer tells of a disable: %s", w.Body)
			}
		})
	}
	if s.main.count() != before {
		t.Errorf("%d requests reached the main installation while disabled", s.main.count()-before)
	}
	if op := s.e.Operation(); op.Disabled == nil || *op.Disabled != iface.DisableSoft || op.Running != 0 {
		t.Errorf("operation = %+v, want soft with nothing running", op)
	}
	s.site.artifacts[tgz] = payload(4 << 10)
	s.setDisabled("")
	if w := s.get(tgz, "Authorization", bearer(s.full)); w.Code != http.StatusOK {
		t.Fatalf("enabled again: %d %s", w.Code, w.Body)
	}
	if op := s.e.Operation(); op.Disabled != nil {
		t.Errorf("operation after the enable = %+v", op)
	}
}

// heldDownload starts a download of a few KiB through a real server whose body from the main installation the test releases, and
// returns the release and the channel of the client's outcome.
func heldDownload(t *testing.T, s *siteHarness) (release func(), outcome <-chan error) {
	t.Helper()
	want := payload(8 << 10)
	pr, pw := io.Pipe()
	inner := s.main.doFn
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		if f.Request.URL.Path != tgz {
			return inner(f)
		}
		r := resp(http.StatusOK, marking(iface.ClassArtifact, iface.ReadFull, "web/npm-all", tokenSubject("full", nil)),
			http.Header{"Etag": {`"` + sha(want) + `"`}, "Content-Type": {"application/octet-stream"}}, "")
		r.Header.Set("Content-Length", itoa(len(want)))
		r.Body = pr
		return r, nil
	}
	srv := serve(t, s.harness)
	out := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+tgz, nil)
		req.Header.Set("Authorization", bearer(s.full))
		res, err := srv.Client().Do(req)
		if err != nil {
			out <- err
			return
		}
		defer res.Body.Close()
		got, err := io.ReadAll(res.Body)
		switch {
		case err != nil:
			out <- err
		case !bytes.Equal(got, want):
			out <- errors.New("the body differs")
		default:
			out <- nil
		}
	}()
	// Half of the body, then the download is running.
	if _, err := pw.Write(want[:len(want)/2]); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the download running", func() bool { return s.e.Operation().Running == 1 })
	return func() { _, _ = pw.Write(want[len(want)/2:]); _ = pw.Close() }, out
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestR_BAS_133_ASoftDisableLetsARunningDownloadFinish: a download that started before a soft disable runs to its end and the status
// counts it as running meanwhile, while a new request is refused.
func TestR_BAS_133_ASoftDisableLetsARunningDownloadFinish(t *testing.T) {
	s := newSiteHarness(t, options{})
	release, outcome := heldDownload(t, s)
	s.setDisabled(iface.DisableSoft)
	if op := s.e.Operation(); op.Running != 1 || op.Disabled == nil || *op.Disabled != iface.DisableSoft {
		t.Fatalf("operation = %+v, want soft with one running", op)
	}
	if w := s.get("/api/npm/web/npm-all/lodash", "Authorization", bearer(s.full)); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a new request while disabled: %d", w.Code)
	}
	release()
	select {
	case err := <-outcome:
		if err != nil {
			t.Fatalf("the running download did not finish: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the running download did not end")
	}
	waitUntil(t, "nothing running", func() bool { return s.e.Operation().Running == 0 })
}

// TestR_BAS_133_AHardDisableCutsIt: a hard disable cuts the running download, the client never sees a complete body, and the fill
// reading from the main installation ends.
func TestR_BAS_133_AHardDisableCutsIt(t *testing.T) {
	s := newSiteHarness(t, options{})
	_, outcome := heldDownload(t, s)
	s.setDisabled(iface.DisableHard)
	select {
	case err := <-outcome:
		if err == nil {
			t.Fatal("the cut download ended as a complete one")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hard disable did not cut the download")
	}
	waitUntil(t, "nothing running", func() bool { return s.e.Operation().Running == 0 })
}

// TestR_BAS_133_ARaceWithTheDisableIsAnsweredAsDisabled: a forward the main installation answers 503 bastion_disabled (the request
// raced the disable) is answered as a disabled replica answers, and the link asks for the handshake; the replica stays reachable.
func TestR_BAS_133_ARaceWithTheDisableIsAnsweredAsDisabled(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) { return nil, mainclient.ErrDisabled }
	w := s.get(tgz, "Authorization", bearer(s.full))
	var p map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != http.StatusServiceUnavailable || p["code"] != "unavailable" || w.Header().Get("Retry-After") != "300" {
		t.Fatalf("status %d, Retry-After %q, body %s", w.Code, w.Header().Get("Retry-After"), w.Body)
	}
	if !s.link.Reachable() {
		t.Error("the race made the replica unreachable")
	}
}
