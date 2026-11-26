//go:build integration

package bastion

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formatdata/clamav"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	clamavformat "github.com/guided-traffic/quader/internal/formats/clamav"
	"github.com/guided-traffic/quader/internal/formats/clamav/cvd"
	"github.com/guided-traffic/quader/internal/formats/clamav/cvd/cvdtest"
	"github.com/guided-traffic/quader/internal/formats/clamav/rows"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/malware/database"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The ClamAV definitions through a Bastion (bastion.md R-BAS-056): freshclam's requests replayed by Go's client through an in-process
// Bastion against upstream/clamav of the main installation, with containers of a few hundred bytes (cvdtest), as R-CLAMAV-009 does at
// the main installation.

// reconcileUpstream runs the reconcile of `quader migrate`, so the project upstream holds clamav.
func reconcileUpstream(t *testing.T, st *testenv.Stack) {
	t.Helper()
	cfg := &config.Config{Database: st.DatabaseConfig()}
	cfg.Cluster = config.Cluster{EpochPollInterval: time.Second, EpochMaxStaleness: 20 * time.Second, ChangeRetention: time.Hour}
	cfg.Health.WatchdogTimeout = 30 * time.Second
	cfg.Audit = config.Audit{AsyncBuffer: 100, RetentionDays: 365}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := postgres.Migrate(ctx, st.DatabaseConfig(), postgres.MigrateOptions{
		Instance: "it-migrate", Version: "0.1.0", Log: testenv.Quiet(), AuditRetentionDays: &cfg.Audit.RetentionDays,
		Migrate:  config.Migrate{AdvisoryLockWait: 20 * time.Second, DDLLockTimeout: 2 * time.Second, DDLLockRetries: 3},
		Declared: app.UpstreamStep(cfg, catalog.Default(), "it-migrate", testenv.Quiet()),
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

// commitContainer commits daily.cvd at version into upstream/clamav through the main replica's item store as system work and marks
// it the set in use, as the database task does.
func (w *world) commitContainer(version int, built time.Time) []byte {
	w.t.Helper()
	ctx := context.Background()
	repo, err := w.main.App.Repositories().ByName(ctx, wellknown.Clamav)
	if err != nil {
		w.t.Fatal(err)
	}
	b := cvdtest.Container("daily", version, cvdtest.Options{BuiltAt: built, Signatures: 1},
		cvdtest.Member{Name: "daily.ndb", Body: []byte("Quader.Test.Sig:0:*:51554144\n")})
	h, err := cvd.ParseHeader(b)
	if err != nil {
		w.t.Fatal(err)
	}
	sp, err := w.main.App.Items().Spool(ctx, bytes.NewReader(b), items.IngestOptions{ContentLength: -1, MaxSize: 1 << 20})
	if err != nil {
		w.t.Fatal(err)
	}
	defer sp.Close()
	if _, err := w.main.App.Items().Commit(ctx, items.CommitSpec{
		Repo: repo, Path: repository.Cleaned{Path: clamavformat.ContainerPath("daily", version, "daily.cvd")}, Spooled: sp,
		MediaType: "application/octet-stream", Origin: items.OriginUpload, MayOverwrite: true, Actor: database.Actor(),
		Payload: rows.Container{
			DB: "daily", Version: version, BuildTimeText: h.BuildTimeText, BuiltAt: h.BuiltAt, Signatures: h.Signatures,
			FunctionalityLevel: h.FunctionalityLevel, Builder: h.Builder, HeaderMD5: h.MD5, ComputedMD5: h.MD5, Verification: rows.VerificationSignature,
		},
	}); err != nil {
		w.t.Fatalf("commit daily.cvd: %v", err)
	}
	db, err := postgres.Open(ctx, w.st.DatabaseConfig(), "it-clamav", "web", testenv.Quiet())
	if err != nil {
		w.t.Fatal(err)
	}
	defer db.Close()
	port, err := clamav.New(clamav.Options{DB: db})
	if err != nil {
		w.t.Fatal(err)
	}
	if err := db.InTx(ctx, func(tx meta.Tx) error {
		return port.MarkSets(ctx, tx, repo.NumericID(), []rows.DBVersion{{DB: "daily", Version: version}}, nil)
	}); err != nil {
		w.t.Fatal(err)
	}
	return b
}

// TestR_BAS_056_TheClamAVDefinitionsThroughABastion: with ClamAV definition updates on at public, freshclam's requests through the
// Bastion: the first 512 bytes of daily.cvd with an If-Modified-Since before the build cold (206), the whole file (200), the check
// again with the Last-Modified it was given (304 from the Bastion, no request to the main installation within the metadata period). An address over
// the Bastion's request limit is answered 503 with Retry-After on /api/clamav/… and 429 on /api/npm/…. About 5 s, a container of a few
// hundred bytes.
func TestR_BAS_056_TheClamAVDefinitionsThroughABastion(t *testing.T) {
	w := newWorld(t, worldOptions{Seed: func(w *world) { reconcileUpstream(t, w.st) }})
	built := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	daily := w.commitContainer(28190, built)
	w.mustAPI("PUT", "/api/v1/upstream-repositories/clamav/clients", http.StatusOK, map[string]any{"enabled": true})
	time.Sleep(2500 * time.Millisecond) // every replica serves the switch within 2 s (R-REPO-049)
	w.setUpOlderThanTheFeed()

	t.Run("freshclam's requests", func(t *testing.T) {
		b := w.newBastion(t, bastionOptions{Configure: relaxed}).wait()
		r := b.reps[0]
		const path = "/api/clamav/upstream/clamav/daily.cvd"
		before := built.Add(-time.Hour).Format(http.TimeFormat)
		head := r.do(t, "GET", path, callOpts{header: []string{"Range", "bytes=0-511", "If-Modified-Since", before}})
		if head.Status != http.StatusPartialContent || len(head.Body) != 512 || !bytes.Equal(head.Body, daily[:512]) {
			t.Fatalf("the check cold: %d, %d bytes %s", head.Status, len(head.Body), head.Body)
		}
		// freshclam sends back the date of the copy it holds: the Last-Modified it was given.
		after := head.Header.Get("Last-Modified")
		if after == "" {
			t.Fatalf("the container carries no Last-Modified: %v", head.Header)
		}
		if whole := r.do(t, "GET", path, callOpts{}); whole.Status != http.StatusOK || !bytes.Equal(whole.Body, daily) {
			t.Fatalf("the whole container: %d, %d bytes", whole.Status, len(whole.Body))
		}
		m := w.front.Mark()
		if again := r.do(t, "GET", path, callOpts{header: []string{"Range", "bytes=0-511", "If-Modified-Since", after}}); again.Status != http.StatusNotModified {
			t.Fatalf("the check with the build time: %d %s", again.Status, again.Body)
		}
		if c := w.front.Cost(m); c != 0 {
			t.Errorf("the check with the build time cost the main installation %d requests", c)
		}
	})
	t.Run("the Bastion's own rate limit is 503 for the format that declares it", func(t *testing.T) {
		tight := func(c *iface.Configuration) {
			c.Limits.Flood.Anonymous = iface.FloodClass{RequestsPerSecond: 1, Burst: 1, MissesPerMinute: 1000}
			c.Limits.Flood.Authenticated = iface.FloodClass{RequestsPerSecond: 1, Burst: 1, MissesPerMinute: 4000}
		}
		b := w.newBastion(t, bastionOptions{Configure: tight}).wait()
		r := b.reps[0]
		o := callOpts{addr: "198.51.100.56"}
		var clam, npm reply
		for i := 0; i < 5; i++ {
			clam = r.do(t, "GET", "/api/clamav/upstream/clamav/daily.cvd", o)
		}
		for i := 0; i < 5; i++ {
			npm = r.do(t, "GET", "/api/npm/"+npmAll+"/plain-lib", o)
		}
		if clam.Status != http.StatusServiceUnavailable || clam.Header.Get("Retry-After") == "" || clam.Code() != "rate_limited" {
			t.Errorf("the ClamAV definitions over the limit: %d %v %s", clam.Status, clam.Header, clam.Body)
		}
		if npm.Status != http.StatusTooManyRequests {
			t.Errorf("npm over the limit: %d %s", npm.Status, npm.Body)
		}
	})
}
