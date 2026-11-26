package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
)

// The tests of R-BAS-098: an artifact area of exactly areaObjects objects of objBytes, filled through fills as the engine fills,
// served again through Touch as the engine serves an entry from the cache, and swept whenever a commit asks for it, as the
// sweeper does. Every fill and every serve is one second after the one before, on the rig's clock, which sets the objects' own
// times and the markers' alike. With 20 objects the marks fall on whole objects: a sweep above 19 down to 17, at most 16
// protected.

const (
	objBytes    = 64
	areaObjects = 20
)

// protectRig is a cache whose artifact area holds exactly areaObjects objects as its store keeps them.
type protectRig struct {
	fill  *rig // the replica that fills and sweeps
	serve *rig // the replica that serves from the cache: fill itself, or another replica of the same bucket
	clk   *clock
	// stored is what the store holds for one object.
	stored int64
}

// The three layouts of R-BAS-098's acceptance: the volume, a bucket shared by two replicas (its listings are the plain layout's;
// the store under it is a volume, as in the other unit tests of the bucket's sweep), and the encrypted layout.
var protectLayouts = []struct {
	name string
	open func(t *testing.T) *protectRig
}{
	{"volume", func(t *testing.T) *protectRig {
		clk := wallClock()
		r := rigOn(t, filepath.Join(t.TempDir(), "volume"), clk, "volume")
		return sized(&protectRig{fill: r, serve: r, clk: clk})
	}},
	{"bucket", func(t *testing.T) *protectRig {
		clk, dir := wallClock(), filepath.Join(t.TempDir(), "bucket")
		return sized(&protectRig{fill: rigOn(t, dir, clk, "bucket"), serve: rigOn(t, dir, clk, "bucket"), clk: clk})
	}},
	{"encrypted", func(t *testing.T) *protectRig {
		clk := wallClock()
		r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), clk)
		return sized(&protectRig{fill: r, serve: r, clk: clk})
	}},
}

func wallClock() *clock { return &clock{t: time.Now().Truncate(time.Second)} }

// rigOn is a rig on the volume dir with the backend name of cfg (the bucket's sweep without a lease runs on any store).
func rigOn(t *testing.T, dir string, clk *clock, backend string) *rig {
	t.Helper()
	cfg := testConfig(dir)
	cfg.Backend = backend
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Metrics: m, Now: clk.Now}
	o.fill()
	vs, err := newVolumeStore(dir, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{Cache: build(newBackend(vs, o), testSigner(t), cfg, o), t: t, dir: dir, clock: clk, m: m, cfg: cfg}
}

// sized sets the limits of both replicas so that the artifact area holds exactly areaObjects objects as the store keeps them.
func sized(p *protectRig) *protectRig {
	p.stored = p.fill.back.storedSize(objBytes)
	for _, r := range []*rig{p.fill, p.serve} {
		r.started = p.clk.Now().Add(-foreignWindow) // past the first hour: the sweeps look for no other layout (R-BAS-061)
		r.Cache.cfg.DocumentsMaxBytes = config.ByteSize(16 * kib)
		r.Cache.cfg.MaxBytes = r.Cache.cfg.DocumentsMaxBytes + config.ByteSize(areaObjects*p.stored)
		r.Cache.cfg.MaxObjectBytes = config.ByteSize(objBytes)
	}
	return p
}

// objects is the bytes of n objects as the store holds them.
func (p *protectRig) objects(n int) float64 { return float64(int64(n) * p.stored) }

// commit fills one object as the engine does, a second after the last event, and reports whether it was kept.
func (p *protectRig) commit(seed uint64) (Entry, bool) {
	p.fill.t.Helper()
	data := payload(seed, objBytes)
	p.clk.Advance(time.Second)
	err := p.fill.commit(EntryArtifact, data)
	if err != nil && !errors.Is(err, ErrNoRoom) {
		p.fill.t.Fatal(err)
	}
	e := p.fill.entry(EntryArtifact, data)
	if err == nil {
		p.fill.setTime(p.fill.path(p.fill.back.lay.object(EntryArtifact, e.SHA256)), p.clk.Now())
	}
	return e, err == nil
}

// add is commit followed by the sweep the commit asked for, as the sweeper runs it; it checks the protected share after it.
func (p *protectRig) add(seed uint64) (Entry, bool) {
	p.fill.t.Helper()
	e, kept := p.commit(seed)
	if p.fill.pendingSweep() {
		if err := p.fill.sweep(context.Background(), nil, metrics.SweepSize); err != nil {
			p.fill.t.Fatal(err)
		}
		if got := p.protected(); got > p.objects(areaObjects*sweepProtectPercent/100) {
			p.fill.t.Fatalf("a sweep left %v bytes protected, above 80 %% of the area", got)
		}
	}
	return e, kept
}

// read serves an entry from the cache once, as the engine's hit does, a second after the last event.
func (p *protectRig) read(e Entry) {
	p.clk.Advance(time.Second)
	p.serve.Touch(context.Background(), e)
}

func (p *protectRig) timedSweep() {
	p.fill.t.Helper()
	if err := p.fill.SweepOnce(context.Background(), nil); err != nil {
		p.fill.t.Fatal(err)
	}
}

func (p *protectRig) held(es ...Entry) int {
	n := 0
	for _, e := range es {
		if _, err := p.fill.Backend().HeadObject(WithArea(context.Background(), e.Kind), e.SHA256); err == nil {
			n++
		}
	}
	return n
}

func (p *protectRig) protected() float64 {
	return val(p.fill.t, p.fill.m.CacheProtected.WithLabelValues(metrics.AreaArtifacts))
}

// TestR_BAS_098_WhatWasServedTwiceOutlivesAFloodOfDistinctFills is the owner's case of W-34 scaled down: a fifth of the area
// read twice, then twice the area's objects fetched once each. Before R-BAS-098 the flood evicted the working set (a fill is
// an access, H-2272). A fill writes no marker, so only the working set is protected, and every sweep the flood starts evicts
// fetched-once objects only, while the flood's newest are kept as they arrive.
func TestR_BAS_098_WhatWasServedTwiceOutlivesAFloodOfDistinctFills(t *testing.T) {
	for _, l := range protectLayouts {
		t.Run(l.name, func(t *testing.T) {
			p := l.open(t)
			var work []Entry
			for i := range areaObjects / 5 {
				e, kept := p.add(uint64(i))
				if !kept {
					t.Fatalf("working set object %d was not kept", i)
				}
				work = append(work, e)
			}
			if marks := markers(t, p.fill); marks != 0 {
				t.Fatalf("the fills wrote %d markers; a fill must write none", marks)
			}
			for _, e := range work {
				p.read(e)
			}
			var flood []Entry
			for i := range 2 * areaObjects {
				e, _ := p.add(uint64(1000 + i))
				flood = append(flood, e)
			}
			if n := p.held(work...); n != len(work) {
				t.Fatalf("%d of the %d objects served twice survived the flood", n, len(work))
			}
			if sweeps(t, p.fill.m, metrics.SweepSize) == 0 || p.held(flood...) == len(flood) {
				t.Fatal("the flood started no sweep that evicted")
			}
			if p.held(flood[len(flood)-1]) != 1 {
				t.Error("the flood's last object was not kept: a new artifact is kept at its first fetch")
			}
			if got, want := p.protected(), p.objects(len(work)); got != want {
				t.Errorf("quader_bastion_cache_protected_bytes{artifacts} = %v, want %v", got, want)
			}
		})
	}
}

// TestR_BAS_098_AtMostEightyPercentIsProtectedAndTheOldestGiveWay: with 18 of 20 objects read twice, the newest 16 are
// protected; the two oldest reads are evicted with the objects read once, by last access, so the sweep down to 17 evicts the
// two read once and the oldest of the two that lost their protection.
func TestR_BAS_098_AtMostEightyPercentIsProtectedAndTheOldestGiveWay(t *testing.T) {
	for _, l := range protectLayouts {
		t.Run(l.name, func(t *testing.T) {
			p := l.open(t)
			var es []Entry
			for i := range areaObjects { // the area full to its limit, without the sweep the last commits asked for
				e, kept := p.commit(uint64(i))
				if !kept {
					t.Fatalf("object %d was not kept", i)
				}
				es = append(es, e)
			}
			for _, e := range es[2:] {
				p.read(e)
			}
			p.timedSweep()
			if n := p.held(es[:3]...); n != 0 {
				t.Errorf("%d of the two read once and the oldest read survived", n)
			}
			if n := p.held(es[3:]...); n != 17 {
				t.Errorf("%d of the 17 newest reads survived", n)
			}
			if got, want := p.protected(), p.objects(16); got != want {
				t.Errorf("quader_bastion_cache_protected_bytes{artifacts} = %v, want %v (80 %%)", got, want)
			}
		})
	}
}

// TestR_BAS_098_AnAttackerWhoReadsEveryObjectTwiceDoesNotProtectEverything: an attacker that reads each of twice the area's
// objects twice gets them protected, never more than 80 % of the area, ranked by last access (add checks the bound
// after every sweep). The rest of the area stays for what is fetched once, so a new object is kept and survives the sweeps
// after it, and a working set read twice after the attack takes the place of the attacker's oldest objects. What the bound
// does not do is keep a working set older than the attacker's reads: displacing it costs the attacker twice the requests
// (H-2290). The order does not depend on the layout, which the two tests above cover; this one runs on the volume.
func TestR_BAS_098_AnAttackerWhoReadsEveryObjectTwiceDoesNotProtectEverything(t *testing.T) {
	for _, l := range protectLayouts[:1] {
		t.Run(l.name, func(t *testing.T) {
			p := l.open(t)
			var attack []Entry
			for i := range 2 * areaObjects {
				e, kept := p.add(uint64(5000 + i))
				if !kept {
					t.Fatalf("attack object %d was not kept", i)
				}
				p.read(e)
				attack = append(attack, e)
			}
			p.timedSweep()
			if got, want := p.protected(), p.objects(16); got != want {
				t.Fatalf("after the attack %v bytes are protected, want %v (80 %%)", got, want)
			}
			// A plain user fetches new objects once, as many as the area holds: each is kept, and the newest survives.
			var fresh []Entry
			for i := range areaObjects {
				e, kept := p.add(uint64(9000 + i))
				if !kept {
					t.Fatalf("a new object %d was refused after the attack", i)
				}
				fresh = append(fresh, e)
			}
			if p.held(fresh[len(fresh)-1]) != 1 {
				t.Fatal("the newest single fetch was evicted: the attacker's objects hold the whole area")
			}
			// A working set of a fifth of the area, each object read again after its fetch: it takes the attacker's oldest place.
			var work []Entry
			for i := range areaObjects / 5 {
				e, _ := p.add(uint64(7000 + i))
				p.read(e)
				work = append(work, e)
			}
			p.timedSweep()
			if n := p.held(work...); n != len(work) {
				t.Errorf("%d of the %d objects read twice after the attack are held", n, len(work))
			}
			// The newest 16 reads are the working set's 4 and the attacker's 12 newest; its older ones are protected no longer.
			if got, want := p.protected(), p.objects(16); got != want {
				t.Errorf("quader_bastion_cache_protected_bytes{artifacts} = %v, want %v", got, want)
			}
			if n := p.held(attack[len(attack)-12:]...); n != 12 {
				t.Errorf("%d of the attacker's 12 newest reads are held, want 12", n)
			}
		})
	}
}

// TestR_BAS_098_AnObjectFilledByOneReplicaAndServedByAnotherIsProtected: on a shared bucket "served twice" is a fill by any
// replica and a serve from the bucket by any replica (Q-BAS-462); two fills of the same object, one per replica, are two
// fetches from the main installation and protect nothing.
func TestR_BAS_098_AnObjectFilledByOneReplicaAndServedByAnotherIsProtected(t *testing.T) {
	clk, dir := wallClock(), filepath.Join(t.TempDir(), "bucket")
	p := sized(&protectRig{fill: rigOn(t, dir, clk, "bucket"), serve: rigOn(t, dir, clk, "bucket"), clk: clk})
	served, _ := p.add(1)
	p.read(served)
	twice := payload(2, objBytes)
	for _, r := range []*rig{p.fill, p.serve} {
		if err := r.commit(EntryArtifact, twice); err != nil {
			t.Fatal(err)
		}
	}
	p.timedSweep()
	if got, want := p.protected(), p.objects(1); got != want {
		t.Fatalf("quader_bastion_cache_protected_bytes{artifacts} = %v, want %v: only the object served from the bucket", got, want)
	}
}

// TestR_BAS_098_ARefillIsProtectedAgainAtItsNextServe: a sweep deletes an object with its marker; when this replica fills it
// again, its next serve writes the marker within access_resolution, so the refill is protected at once (Q-BAS-464).
func TestR_BAS_098_ARefillIsProtectedAgainAtItsNextServe(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	data := payload(3, 50)
	e := r.entry(EntryArtifact, data)
	r.fillObject(EntryArtifact, data)
	r.Touch(ctx, e)
	if err := r.Backend().DeleteObject(ctx, e.SHA256); err != nil {
		t.Fatal(err)
	}
	r.fillObject(EntryArtifact, data)
	r.clock.Advance(time.Minute)
	r.Touch(ctx, e)
	if _, err := os.Stat(filepath.Join(r.dir, "access", e.SHA256)); err != nil {
		t.Fatalf("the refill's serve inside access_resolution wrote no marker: %v", err)
	}
}

// markers counts the access markers of a rig's layout.
func markers(t *testing.T, r *rig) int {
	t.Helper()
	n := 0
	err := r.back.s.list(context.Background(), r.back.lay.markerDir(), func(string, int64, time.Time) error { n++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	return n
}
