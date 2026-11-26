package engine

import (
	"bytes"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_BAS_089_AFillWithoutRoomIsServedAndNotKept: with an artifact area of 384 KiB, the second tarball of 300 KiB finds no
// room: its caller receives it whole from the fill's spool, nothing of it is kept, and the next request for it is a miss.
func TestR_BAS_089_AFillWithoutRoomIsServedAndNotKept(t *testing.T) {
	small := func(c *config.BastionConfig) {
		c.Bastion.Cache.MaxBytes, c.Bastion.Cache.DocumentsMaxBytes = 1<<20, 640<<10
	}
	s := newSiteHarness(t, options{cfg: []func(*config.BastionConfig){small}})
	auth := []string{"Authorization", bearer(s.full)}
	s.site.artifacts[tgz] = payload(300 << 10)
	s.site.artifacts[other] = payload(300<<10 + 1)
	if w := s.get(tgz, auth...); w.Code != 200 {
		t.Fatal(w.Code)
	}
	s.e.wg.Wait()
	if n := s.cache.objectCount(); n != 1 {
		t.Fatalf("%d objects after the first fill, want 1", n)
	}
	w := s.get(other, auth...)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), s.site.artifacts[other]) {
		t.Fatalf("the fill without room: %d, %d bytes", w.Code, w.Body.Len())
	}
	s.e.wg.Wait()
	if n := s.cache.objectCount(); n != 1 {
		t.Fatalf("%d objects after the fill without room, want 1", n)
	}
	if got := readCounter(s.m.CacheNoRoom.WithLabelValues(metrics.AreaArtifacts)); got != 1 {
		t.Fatalf("quader_bastion_cache_no_room_total{artifacts} = %v, want 1", got)
	}
	if left := s.cache.files("tmp"); len(left) != 0 {
		t.Fatalf("%d spool files left: %v", len(left), left)
	}
	before := s.main.count(mainclient.KindForward)
	if w := s.get(other, auth...); w.Code != 200 || s.main.count(mainclient.KindForward) != before+1 {
		t.Fatalf("the next request was not a miss: %d, forwards %d -> %d", w.Code, before, s.main.count(mainclient.KindForward))
	}
}

// TestR_BAS_090_FillBytesCarryTheClassOfTheCallerThatStartedTheFill: quader_bastion_fill_bytes_total counts a fill started with a
// credential and one started anonymously under their budget classes, both from the main installation.
func TestR_BAS_090_FillBytesCarryTheClassOfTheCallerThatStartedTheFill(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.artifacts[tgz] = payload(10 << 10)
	s.site.artifacts[other] = payload(3<<10 + 7)
	if w := s.get(tgz, "Authorization", bearer(s.full)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := s.get(other); w.Code != 200 {
		t.Fatal(w.Code)
	}
	s.e.wg.Wait()
	for _, c := range []struct {
		source, class string
		want          float64
	}{
		{metrics.SourceMain, metrics.BudgetAuthenticated, 10 << 10},
		{metrics.SourceMain, metrics.BudgetAnonymous, 3<<10 + 7},
		{metrics.SourceStore, metrics.BudgetAuthenticated, 0},
		{metrics.SourceStore, metrics.BudgetAnonymous, 0},
	} {
		if got := readCounter(s.m.FillBytes.WithLabelValues(c.source, c.class)); got != c.want {
			t.Errorf("quader_bastion_fill_bytes_total{source=%q,class=%q} = %v, want %v", c.source, c.class, got, c.want)
		}
	}
}
