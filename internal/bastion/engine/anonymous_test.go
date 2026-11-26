package engine

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/answers"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// What an anonymous request may cause at the main installation (bastion.md R-BAS-094): the anonymous registry token is the
// Bastion's own (Q-BAS-335), and the anonymous caller's documents are kept for the metadata period (R-BAS-023). Against the
// double of the main installation of support_test.go; payloads are bytes.

// answersSkew lies beyond the margin within which a feed entry received before an answer still judges it (R-BAS-020 rule 3).
const answersSkew = answers.SkewMargin + time.Second

// anonymousTokenOf reads the token of a token answer of the Bastion and checks the answer's form (formats/oci.md R-OCI-004 step 4).
func anonymousTokenOf(t *testing.T, code int, h http.Header, body []byte) string {
	t.Helper()
	var a struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		IssuedAt    string `json:"issued_at"`
	}
	if code != http.StatusOK || json.Unmarshal(body, &a) != nil {
		t.Fatalf("no token answer: %d %s", code, body)
	}
	if a.Token == "" || a.Token != a.AccessToken || a.ExpiresIn != 300 || a.IssuedAt != epoch.Format(time.RFC3339) {
		t.Fatalf("the token answer: %s", body)
	}
	for k, want := range map[string]string{
		"Content-Type": "application/json", "Cache-Control": "no-store", "Docker-Distribution-Api-Version": "registry/2.0",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	return a.Token
}

// TestR_BAS_094_TheBastionAnswersAnAnonymousTokenRequestItself: an anonymous GET and POST of the token service are answered by
// the Bastion without a request to the main installation, with a token of the registry token's form whose payload is version 1
// with the key ID "bastion" and only the claims "a" and "exp" (Q-BAS-335). Presented, the token is dropped and the request is
// the anonymous caller's, forwarded without a credential: at the replica that issued it, at another replica of the Bastion and at
// another Bastion alike, since nobody verifies it (Q-BAS-435). With anonymous reads off the request is refused 401 by the
// Bastion, before the first handshake 503; a request with a credential is still forwarded.
func TestR_BAS_094_TheBastionAnswersAnAnonymousTokenRequestItself(t *testing.T) {
	s := newSiteHarness(t, options{})
	registryMain(s)
	manifest := "/v2/web/docker/app/manifests/sha256:" + strings.Repeat("ab", 32)
	viaRegistry := s.main.doFn
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		if f.Request.URL.Path != manifest {
			return viaRegistry(f)
		}
		if f.Request.Header.Get("Authorization") != "" {
			return resp(http.StatusUnauthorized, marking(iface.ClassPass, "", "", iface.Subject{Kind: iface.SubjectInvalid}), nil, ""), nil
		}
		body := `{"schemaVersion":2}`
		h := http.Header{"Content-Type": {"application/vnd.oci.image.manifest.v1+json"}, "Etag": {`"sha256:` + sha([]byte(body)) + `"`}}
		return resp(http.StatusOK, marking(iface.ClassArtifact, iface.ReadFull, "web/docker", iface.Subject{Kind: iface.SubjectAnonymous}), h, body), nil
	}
	var tok string

	t.Run("GET and POST without a credential are answered at the Bastion", func(t *testing.T) {
		before := s.main.count()
		w := s.get("/v2/_token?scope=repository%3Aweb%2Fdocker%2Fapp%3Apull&service=packages.example.com")
		tok = anonymousTokenOf(t, w.Code, w.Header(), w.Body.Bytes())
		p := s.do(http.MethodPost, "/v2/_token", strings.NewReader("grant_type=password&service=packages.example.com&scope=repository%3Aweb%2Fdocker%2Fapp%3Apull"),
			"Content-Type", "application/x-www-form-urlencoded")
		second := anonymousTokenOf(t, p.Code, p.Header(), p.Body.Bytes())
		if second == tok {
			t.Error("two anonymous tokens carry the same MAC")
		}
		if s.main.count() != before {
			t.Fatalf("an anonymous token request cost the main installation %d requests", s.main.count()-before)
		}
		if counterValue(t, s.harness, "requests", metrics.ClassPass, metrics.ResultHit) < 2 {
			t.Error("the answers are not counted as answered at the Bastion")
		}
	})
	t.Run("the token's form", func(t *testing.T) {
		if !iface.RegistryTokenWellFormed(tok) || !iface.RegistryTokenAnonymous(tok) {
			t.Fatalf("not an anonymous registry token: %s", tok)
		}
		if exp, ok := iface.RegistryTokenExpiry(tok); !ok || !exp.Equal(epoch.Add(300*time.Second)) {
			t.Fatalf("expiry %v %v", exp, ok)
		}
		if g, ok := iface.RegistryTokenGrant(tok); !ok || len(g) != 0 {
			t.Fatalf("a grant %v %v", g, ok)
		}
		encoded, mac, _ := strings.Cut(strings.TrimPrefix(tok, iface.RegistryTokenPrefix), ".")
		payload, _ := base64.RawURLEncoding.DecodeString(encoded)
		if want := `1:bastion:{"a":true,"exp":` + itoa(int(epoch.Add(300*time.Second).Unix())) + `}`; string(payload) != want {
			t.Fatalf("payload %q, want %q", payload, want)
		}
		if raw, err := base64.RawURLEncoding.DecodeString(mac); err != nil || len(raw) != 32 {
			t.Fatalf("the MAC is %d bytes: %v", len(raw), err)
		}
	})
	t.Run("presented at any replica of any Bastion it is the anonymous caller", func(t *testing.T) {
		replica := newSiteHarness(t, options{clock: s.clock}) // another replica of the Bastion, or another Bastion: neither holds a key
		replica.main.doFn = s.main.doFn
		w := replica.get("/v2/_token?service=packages.example.com")
		foreign := anonymousTokenOf(t, w.Code, w.Header(), w.Body.Bytes())
		for name, c := range map[string]struct {
			h   *siteHarness
			tok string
		}{
			"its own token at the replica that issued it": {s, tok}, "its token at another replica": {replica, tok},
			"another Bastion's token": {s, foreign},
		} {
			before := len(c.h.main.requests())
			if w := c.h.get(manifest, "Authorization", bearer(c.tok)); w.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", name, w.Code, w.Body)
			}
			for _, r := range c.h.main.requests()[before:] {
				if r.Header.Get("Authorization") != "" || r.Class != mainclient.Anonymous {
					t.Fatalf("%s: the main installation saw %q in class %s", name, r.Header.Get("Authorization"), r.Class)
				}
			}
		}
	})
	t.Run("a HEAD without a credential is the token service's 405 at the Bastion", func(t *testing.T) {
		before := s.main.count()
		w := s.do(http.MethodHead, "/v2/_token?service=packages.example.com", nil)
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, POST" || s.main.count() != before {
			t.Fatalf("%d %v, %d requests", w.Code, w.Header(), s.main.count()-before)
		}
	})
	t.Run("a request with a credential is forwarded", func(t *testing.T) {
		before := s.main.count(mainclient.KindForward)
		basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+s.full))
		if w := s.get("/v2/_token?service=packages.example.com", "Authorization", basic); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "qdr_rgt_issued") {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		if s.main.count(mainclient.KindForward) != before+1 {
			t.Fatal("a token request with a credential was not forwarded")
		}
	})
	t.Run("with anonymous reads off it is refused without a request", func(t *testing.T) {
		s.setAnonymous(false)
		defer s.setAnonymous(true)
		before := s.main.count(mainclient.KindForward)
		for _, w := range []struct {
			code int
			body string
		}{
			{s.get("/v2/_token?service=packages.example.com").Code, "GET"},
			{s.do(http.MethodPost, "/v2/_token", strings.NewReader("grant_type=password"), "Content-Type", "application/x-www-form-urlencoded").Code, "POST"},
		} {
			if w.code != http.StatusUnauthorized {
				t.Fatalf("%s: %d", w.body, w.code)
			}
		}
		if s.main.count(mainclient.KindForward) != before {
			t.Fatal("a refused token request was forwarded")
		}
	})
	t.Run("before the first handshake it is 503", func(t *testing.T) {
		down := newSiteHarness(t, options{down: true})
		if w := down.get("/v2/_token?service=packages.example.com"); w.Code != http.StatusServiceUnavailable || registryDetail(t, w) != "main_unreachable" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
}

// TestR_BAS_094_AnAnonymousDocumentIsKeptForTheMetadataPeriod: a document the main installation filters for the anonymous
// subject is fetched once and served to every anonymous caller for metadata_revalidate_after without a request, also beside a
// shared entry of the same document; after the period one conditional request validates it again, and a content entry of the feed
// that names its repository ends it at once (R-BAS-023, R-BAS-094). A copy without a validator is kept as well (Q-BAS-437); a
// caller with a token is still asked about on every use.
func TestR_BAS_094_AnAnonymousDocumentIsKeptForTheMetadataPeriod(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.addReader("", &reader{id: "anon", read: iface.ReadPartial})
	s.site.docs[doc] = "all packages"
	period := s.cfg.Bastion.Cache.MetadataRevalidateAfter
	anon := func(path string) {
		t.Helper()
		if w := s.get(path); w.Code != http.StatusOK || w.Body.String() != "filtered:"+s.site.docs[path] {
			t.Fatalf("%s: %d %q", path, w.Code, w.Body)
		}
	}
	cost := func(what string, want int, f func()) []recorded {
		t.Helper()
		before := s.main.count()
		f()
		got := s.main.requests()[before:]
		if len(got) != want {
			t.Fatalf("%s: %d requests to the main installation, want %d: %+v", what, len(got), want, got)
		}
		return got
	}

	cost("ten anonymous reads within the period", 1, func() {
		for range 10 {
			anon(doc)
		}
	})
	if counterValue(t, s.harness, "requests", metrics.ClassPrivate, metrics.ResultHit) != 9 {
		t.Fatal("the reads after the first are not counted as hits")
	}
	if s.cache.entryCount() != 0 || s.cache.objectCount() != 0 {
		t.Fatal("the anonymous copy reached the cache backend")
	}
	s.clock.advance(period + time.Second)
	reqs := cost("a read after the period, then nine within the next", 1, func() {
		for range 10 {
			anon(doc)
		}
	})
	if reqs[0].Kind != mainclient.KindConditional || reqs[0].INM != `"p0-anon"` || reqs[0].Header.Get("Authorization") != "" {
		t.Fatalf("the revalidation: %+v", reqs[0])
	}

	s.content("c1", "web/npm-all")
	s.clock.advance(time.Second)
	s.tick()
	cost("a read after a content entry of the repository", 1, func() { anon(doc) })
	// A copy validated within 15 s after the entry is judged by it as well (R-BAS-020 rule 3), as a shared entry is.
	s.clock.advance(answersSkew)
	cost("a read once the entry lies further back", 1, func() { anon(doc) })
	cost("the next read", 0, func() { anon(doc) })

	t.Run("beside a shared entry", func(t *testing.T) {
		s.site.docs[doc+"-both"] = "both"
		s.get(doc+"-both", "Authorization", bearer(s.full))
		// The anonymous caller's shape on the repository is known and partial: one conditional request with the shared entry's
		// validator, answered with the caller's copy, which serves the next four.
		reqs := cost("five anonymous reads of a document a full reader keeps shared", 1, func() {
			for range 5 {
				anon(doc + "-both")
			}
		})
		if reqs[0].Kind != mainclient.KindConditional || reqs[0].INM != `"d0"` {
			t.Fatalf("%+v", reqs)
		}
	})
	t.Run("without a validator", func(t *testing.T) {
		bare := doc + "-bare"
		s.site.docs[bare] = "no etag"
		viaSite := s.main.doFn
		s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
			r, err := viaSite(f)
			if err == nil && f.Request.URL.Path == bare {
				r.Header.Del("Etag")
			}
			return r, err
		}
		defer func() { s.main.doFn = viaSite }()
		cost("three anonymous reads of a document without an ETag", 1, func() {
			for range 3 {
				anon(bare)
			}
		})
	})
	t.Run("a caller with a token is asked about on every use", func(t *testing.T) {
		cost("three reads of a partial reader", 3, func() {
			for range 3 {
				s.get(doc, "Authorization", bearer(s.part))
			}
		})
	})
}
