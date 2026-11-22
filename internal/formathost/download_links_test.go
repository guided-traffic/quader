package formathost

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/malware/gate"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// signedLinkItems is the item store of the serve point's tests: the row it was given, its bytes, and what was served and opened.
type signedLinkItems struct {
	ItemOps
	body   []byte
	opened int
	served []items.ServedEvent
}

func (l *signedLinkItems) Usable(_ context.Context, it items.Item) (items.Item, error) {
	return it, nil
}

func (l *signedLinkItems) Open(context.Context, items.Item, *items.Range) (io.ReadCloser, error) {
	l.opened++
	return io.NopCloser(bytes.NewReader(l.body)), nil
}

func (l *signedLinkItems) Served(_ context.Context, e items.ServedEvent) {
	l.served = append(l.served, e)
}

// linkServe is one download through the serve point with every condition of R-STOR-070 met unless a case changes one.
type linkServe struct {
	t      *testing.T
	store  *blobtest.Store
	conns  *blob.Connections
	links  *downloadlinks.Links
	met    *observability.Metrics
	items  *signedLinkItems
	repos  linkRepos
	row    items.Item
	malw   *gate.Gate
	figs   limits.Figures
	mode   limits.Mode
	hdr    http.Header
	method string
	route  formats.Route
	ctx    func(context.Context) context.Context
	used   bool // the caller's cachedBytesPerHour is used up before the download
}

var (
	linkHolder = qname.Repo{Project: "web", Key: "probe-local"}
	linkVirt   = qname.Repo{Project: "web", Key: "probe-all"}
)

func newLinkServe(t *testing.T) *linkServe {
	t.Helper()
	m := blobtest.New()
	p := "quader"
	conn := blob.NewConnection(config.StorageConnection{ID: "default", Bucket: "blobs", Prefix: &p, Endpoint: "https://" + blobtest.PresignHost, ForcePathStyle: true},
		blob.NewGuard(m, config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute}))
	conns, err := blob.NewConnections(conn)
	if err != nil {
		t.Fatal(err)
	}
	met := observability.NewMetrics("web")
	links := downloadlinks.New(downloadlinks.Options{Connections: conns, Rule: downloadlinks.HostRuleOf([]string{"quader.example.com"}), Replica: "r1", Metrics: downloadlinks.NewMetrics(met)})
	s := downloadlinks.Defaults()
	s.Enabled, s.MinSize = true, 0
	links.SetSettings(s)
	links.RunLinkChecks(context.Background())
	body := []byte("a tarball of a few bytes")
	var d blob.Digest
	d[0] = 0x7e
	key := blob.Key("quader", d)
	m.Set(key, body, time.Now())
	row := items.Item{
		ID: 9, RepoID: 3, Repository: linkHolder, Path: "a/-/a-1.0.0.tgz", Kind: items.KindFile, Size: int64(len(body)), MediaType: "application/octet-stream",
		Digest: d, SHA512: make([]byte, 64), ContentStatus: blob.StatusOK, ModifiedAt: time.Now(),
		Malware: malware.Verdict{State: malware.StateClean},
		Copies:  []blob.Copy{{Connection: "default", ObjectKey: key, StoredSize: int64(len(body)), State: blob.CopyPresent}}, CopiesRead: true,
	}
	return &linkServe{
		t: t, store: m, conns: conns, links: links, met: met, items: &signedLinkItems{body: body}, row: row, method: http.MethodGet, hdr: http.Header{},
		repos: linkRepos{by: map[qname.Repo]repository.Repository{
			linkHolder: {ID: "3", Name: linkHolder, Class: repository.Local, Format: "probe"},
			linkVirt:   {ID: "4", Name: linkVirt, Class: repository.Virtual, Format: "probe"},
		}},
		route: formats.Route{Name: "probe.tarball", Kind: formats.Download, ClientDigest: formats.ClientDigest{Checks: true, Against: "the integrity", Evidence: "a test"}},
		mode:  limits.ModeEnforce,
	}
}

// serve runs the download and returns the answer.
func (c *linkServe) serve() *httptest.ResponseRecorder {
	c.t.Helper()
	pk := mustPackages(c.t)
	h, err := New(Options{Runner: nopRunner{}, Decider: &fakeDecider{}, Items: c.items, Packages: pk, Repos: c.repos, Links: c.links, Malware: c.malw})
	if err != nil {
		c.t.Fatal(err)
	}
	r := httptest.NewRequest(c.method, "/api/probe/web/probe-local/a/-/a-1.0.0.tgz", nil)
	for k, v := range c.hdr {
		r.Header[k] = v
	}
	// The download limits: one profile for every anonymous caller.
	x := limits.NewIndex(limits.Snapshot{
		Profiles: []limits.Profile{{ID: "p1", Key: "p", Name: "p", Figures: c.figs}},
		Rules:    []limits.Rule{{ID: "r1", Place: limits.InstancePlace(), WhoKind: limits.WhoEvery, WhoRef: limits.EveryAnonymous, Profile: "p"}},
		Settings: limits.Settings{Mode: c.mode, BlockSeconds: 900, Version: 1},
	})
	eng := limits.NewEngine(limits.Options{Store: cluster.NewMemoryLimits(), Index: func() *limits.Index { return x }, Replica: "r1", LeaseTTL: time.Minute, Share: func() int { return 1 }})
	caller := limits.Caller{Kind: limits.CallerAnonymous, Anonymous: true, Client: netip.MustParseAddr("198.51.100.30")}
	ctx := context.Background()
	if c.used {
		pre := eng.Begin(caller, linkHolder)
		_ = eng.Admit(ctx, pre)
		pre.AddCached(int(c.figs[limits.CachedBytesPerHour]))
		eng.Finish(pre)
		eng.Flush(ctx)
	}
	charge := eng.Begin(caller, linkHolder)
	defer eng.Finish(charge)
	ctx, _ = observability.WithRequestInfo(limits.WithCharge(ctx, charge))
	if c.ctx != nil {
		ctx = c.ctx(ctx)
	}
	route := c.route
	rq := &formats.Request{
		Request: r.WithContext(ctx), Repo: &repository.Repository{ID: "3", Name: linkHolder, Class: repository.Local, Format: "probe"},
		Route: &route, Subject: authz.Subject{Name: "anonymous", Endpoint: "default"},
	}
	defer h.Begin(rq)()
	it := formats.Item{Ref: formats.ItemRef{ID: c.row.ID, Repo: formats.RepoRef{ID: 3, Name: linkHolder, Class: formats.Local, Format: "probe"}, Path: c.row.Path}, Handle: handle{it: c.row}}
	rec := httptest.NewRecorder()
	if err := h.Items().Serve(ctx, rq, rec, it, formats.ServeStored); err != nil {
		e, _ := errs.As(err)
		rec.Code = e.Status()
		rec.Header().Set("X-Code", e.Code)
	}
	return rec
}

// TestR_STOR_070_TheConditionsOfALink: with every condition met a tarball GET answers 307 to a link the in-memory store serves; each
// condition taken away alone answers 200 with the bytes; HEAD answers 200 without a store request; If-None-Match with the content
// address answers 304; a used-up cachedBytesPerHour answers 429 and no link.
func TestR_STOR_070_TheConditionsOfALink(t *testing.T) {
	t.Run("R-STOR-070 every condition met: 307, and the link serves the bytes", func(t *testing.T) {
		c := newLinkServe(t)
		rec := c.serve()
		if rec.Code != http.StatusTemporaryRedirect || c.items.opened != 0 {
			t.Fatalf("%d, opened %d", rec.Code, c.items.opened)
		}
		req := httptest.NewRequest(http.MethodGet, rec.Header().Get("Location"), nil)
		resp, err := c.store.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !bytes.Equal(got, c.items.body) || resp.Header.Get("Content-Disposition") != "attachment; filename*=UTF-8''a-1.0.0.tgz" {
			t.Fatalf("the link answered %d %q %v", resp.StatusCode, got, resp.Header)
		}
	})
	for _, tc := range []struct {
		name, reason string // reason: the declined downloads' label it counts, "" for none
		mod          func(c *linkServe)
	}{
		{"a route without the declaration", "", func(c *linkServe) { c.route.ClientDigest = formats.ClientDigest{} }},
		{"a recognized Bastion request", downloadlinks.DeclinedBastion, func(c *linkServe) {
			c.ctx = func(ctx context.Context) context.Context {
				return bastionmain.WithRecognition(ctx, bastionmain.Recognition{Bastion: "dmz", Interface: 4})
			}
		}},
		{"the switch off", "", func(c *linkServe) {
			s := c.links.Settings()
			s.Enabled = false
			c.links.SetSettings(s)
		}},
		{"the holding member of a virtual repository opted out", downloadlinks.DeclinedOptOut, func(c *linkServe) {
			c.repos.by[linkHolder] = repositoryOptedOut(c.repos.by[linkHolder])
		}},
		{"a blob without a malware verdict under a gate that is not strict", downloadlinks.DeclinedGate, func(c *linkServe) {
			c.malw = gate.New(gate.Options{Enabled: true})
			c.row.Malware = malware.Verdict{}
		}},
		{"a Range", downloadlinks.DeclinedRange, func(c *linkServe) { c.hdr.Set("Range", "bytes=0-") }},
		{"a size below minSize", downloadlinks.DeclinedSize, func(c *linkServe) {
			s := c.links.Settings()
			s.MinSize = c.row.Size + 1
			c.links.SetSettings(s)
		}},
		{"an open circuit", downloadlinks.DeclinedConnection, func(c *linkServe) {
			c.store.SetFault(func(string, string) error {
				return &blob.StoreError{Op: "HeadObject", Kind: blob.KindTransient, Status: 503}
			})
			_, _ = c.conns.First().Objects().Head(context.Background(), "k")
			c.store.SetFault(nil)
		}},
		{"an encrypted connection", downloadlinks.DeclinedConnection, func(c *linkServe) {
			c.met = observability.NewMetrics("web")
			c.links = encryptedLinks(t, c.store, c.met)
		}},
		{"a credential that expires in 3 s", downloadlinks.DeclinedCredential, func(c *linkServe) {
			c.store.SetCredentialExpiry(time.Now().Add(3 * time.Second))
		}},
	} {
		t.Run("R-STOR-070 "+tc.name+" streams the bytes", func(t *testing.T) {
			c := newLinkServe(t)
			tc.mod(c)
			rec := c.serve()
			want := http.StatusOK
			if c.hdr.Get("Range") != "" {
				want = http.StatusPartialContent // the range of the whole file, answered by Quader (Q-STOR-285)
			}
			if rec.Code != want || !bytes.Equal(rec.Body.Bytes(), c.items.body) || rec.Header().Get("Location") != "" {
				t.Fatalf("%d %q", rec.Code, rec.Body.String())
			}
			for _, r := range downloadlinks.DeclinedReasons() {
				want := 0.0
				if r == tc.reason {
					want = 1
				}
				if got := counter(t, c.met, "quader_storage_download_links_declined_total", "reason", r); got != want {
					t.Errorf("declined for %s: %v, want %v", r, got, want)
				}
			}
		})
	}
	for _, mode := range []limits.Mode{limits.ModeEnforce, limits.ModeAudit} {
		t.Run("R-STOR-070 R-LIM-040 a caller with a speed limit gets a link in "+string(mode)+", charged its size at the issue", func(t *testing.T) {
			c := newLinkServe(t)
			c.mode, c.figs[limits.CachedBytesPerSecond] = mode, 1_000_000
			var charge *limits.Charge
			c.ctx = func(ctx context.Context) context.Context {
				charge = limits.FromContext(ctx)
				return ctx
			}
			if rec := c.serve(); rec.Code != http.StatusTemporaryRedirect {
				t.Fatalf("%d: a speed limit kept the download from a link (Q-LIM-99)", rec.Code)
			}
			if cached, _ := charge.Totals(); cached != c.row.Size {
				t.Fatalf("the charge holds %d cached bytes, want %d", cached, c.row.Size)
			}
		})
	}
	t.Run("R-STOR-070 a system read is never a link", func(t *testing.T) {
		// Serve streams the scanners' reads before it builds the options; the decision holds it on its own as well.
		c := newLinkServe(t)
		h, err := New(Options{Runner: nopRunner{}, Decider: &fakeDecider{}, Items: c.items, Packages: mustPackages(t), Repos: c.repos, Links: c.links})
		if err != nil {
			t.Fatal(err)
		}
		route := c.route
		rq := &formats.Request{
			Request: httptest.NewRequest(http.MethodGet, "/api/probe/web/probe-local/a/-/a-1.0.0.tgz", nil),
			Repo:    &repository.Repository{ID: "3", Name: linkHolder, Class: repository.Local, Format: "probe"},
			Route:   &route, Subject: authz.Subject{Name: "vuln-scanner-trivy", Kind: principal.KindSystem, Endpoint: "default"},
		}
		it := formats.Item{Ref: formats.ItemRef{ID: c.row.ID, Repo: formats.RepoRef{ID: 3, Name: linkHolder, Class: formats.Local, Format: "probe"}, Path: c.row.Path}}
		var o edge.ServeOptions
		itemStore{h: h}.linkOptions(context.Background(), rq, it, c.row, true, &o)
		if o.Link != nil {
			t.Fatal("a system read was offered a link")
		}
	})
	t.Run("R-STOR-070 HEAD answers 200 without a store request", func(t *testing.T) {
		c := newLinkServe(t)
		c.method = http.MethodHead
		before := c.store.Count("presign") // the link check's
		if rec := c.serve(); rec.Code != http.StatusOK || c.items.opened != 0 || c.store.Count("presign") != before || c.store.Count("get") != 0 {
			t.Fatalf("%d, opened %d, presigned %d", rec.Code, c.items.opened, c.store.Count("presign")-before)
		}
	})
	t.Run("R-STOR-070 If-None-Match with the content address answers 304", func(t *testing.T) {
		c := newLinkServe(t)
		c.hdr.Set("If-None-Match", c.row.ETag())
		before := c.store.Count("presign")
		if rec := c.serve(); rec.Code != http.StatusNotModified || c.store.Count("presign") != before {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("R-STOR-070 R-LIM-040 a used-up cachedBytesPerHour answers 429 and no link", func(t *testing.T) {
		c := newLinkServe(t)
		c.figs[limits.CachedBytesPerHour], c.used = 1_000, true
		before := c.store.Count("presign")
		if rec := c.serve(); rec.Code != http.StatusTooManyRequests || rec.Header().Get("X-Code") != limits.CodeDownloadLimitReached || c.store.Count("presign") != before {
			t.Fatalf("%d %s", rec.Code, rec.Header().Get("X-Code"))
		}
	})
}

// TestR_STOR_073_WhatALinkCounts: one link adds one download to the statistics with the link's delivery and expiry for the audit
// event, the item's size to the charge's cached bytes, the request log's delivery, and one to the issued links of the format; a
// streamed GET declined for its range adds one to the declined downloads of the reason range.
func TestR_STOR_073_WhatALinkCounts(t *testing.T) {
	c := newLinkServe(t)
	var charge *limits.Charge
	var info *observability.RequestInfo
	c.ctx = func(ctx context.Context) context.Context {
		charge, info = limits.FromContext(ctx), observability.RequestInfoFrom(ctx)
		return ctx
	}
	if rec := c.serve(); rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("%d", rec.Code)
	}
	if len(c.items.served) != 1 || c.items.served[0].Delivery != "link" || c.items.served[0].LinkExpiresAt.IsZero() || !c.items.served[0].CountStats {
		t.Fatalf("served %+v", c.items.served)
	}
	if cached, _ := charge.Totals(); cached != c.row.Size {
		t.Fatalf("the charge holds %d cached bytes, want %d", cached, c.row.Size)
	}
	if info == nil {
		t.Fatal("no request info")
	}
	if got := counter(t, c.met, "quader_storage_download_links_total", "format", "probe"); got != 1 {
		t.Fatalf("issued %v", got)
	}
	if got := counter(t, c.met, "quader_storage_download_link_bytes_total", "format", "probe"); got != float64(c.row.Size) {
		t.Fatalf("bytes %v", got)
	}
	r := newLinkServe(t)
	r.links, r.met = c.links, c.met
	r.hdr.Set("Range", "bytes=0-")
	if rec := r.serve(); rec.Code != http.StatusPartialContent {
		t.Fatalf("%d", rec.Code)
	}
	if got := counter(t, c.met, "quader_storage_download_links_declined_total", "reason", downloadlinks.DeclinedRange); got != 1 {
		t.Fatalf("declined for the range: %v", got)
	}
}

// counter is the value of a counter's series with one label, 0 for a series that does not exist.
func counter(t *testing.T, m *observability.Metrics, name, label, value string) float64 {
	t.Helper()
	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, s := range mf.GetMetric() {
			for _, l := range s.GetLabel() {
				if l.GetName() == label && l.GetValue() == value {
					return s.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// repositoryOptedOut is a repository whose member storage.downloadLinks is false, read as the snapshot reads a stored document.
func repositoryOptedOut(r repository.Repository) repository.Repository {
	out, err := repository.Build(r.NumericID(), 1, r.Name, r.Class, r.Format, []byte(`{"storage":{"downloadLinks":false}}`), nil)
	if err != nil {
		panic(err)
	}
	return out
}

// encryptedLinks is a replica's side whose one connection is encrypted (a fake of stage 2).
func encryptedLinks(t *testing.T, m *blobtest.Store, met *observability.Metrics) *downloadlinks.Links {
	t.Helper()
	p := "quader"
	conn := blob.NewConnection(config.StorageConnection{ID: "default", Bucket: "blobs", Prefix: &p, Endpoint: "https://" + blobtest.PresignHost, ForcePathStyle: true, Encrypted: true},
		blob.NewGuard(m, config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute}))
	conns, _ := blob.NewConnections(conn)
	l := downloadlinks.New(downloadlinks.Options{Connections: conns, Rule: downloadlinks.HostRuleOf(nil), Replica: "r1", Metrics: downloadlinks.NewMetrics(met)})
	s := downloadlinks.Defaults()
	s.Enabled, s.MinSize = true, 0
	l.SetSettings(s)
	l.RunLinkChecks(context.Background())
	return l
}
