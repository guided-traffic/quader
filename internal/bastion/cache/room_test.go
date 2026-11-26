package cache

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/config"
)

// megabyteCache is a cache of max_bytes 1 MiB: 896 KiB for artifacts, 128 KiB for documents.
func megabyteCache(c *config.BastionCache) {
	c.MaxBytes, c.DocumentsMaxBytes, c.MaxObjectBytes, c.SpoolMaxBytes = mib, 128*kib, 256*kib, 512*kib
}

// commit fills data of the kind and commits it, as the engine does; it returns the commit's error.
func (r *rig) commit(kind EntryKind, data []byte) error {
	r.t.Helper()
	f, err := r.NewFill(kind, int64(len(data)))
	if err != nil {
		r.t.Fatal(err)
	}
	defer f.Abort()
	if _, err := f.Write(data); err != nil {
		r.t.Fatal(err)
	}
	return f.Commit(context.Background(), digest(data))
}

func (r *rig) usage() Usage {
	r.t.Helper()
	u, err := r.Backend().Usage(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	return u
}

// pendingSweep reports whether a sweep was asked for, and takes the request.
func (r *rig) pendingSweep() bool {
	select {
	case <-r.sweepNow:
		return true
	default:
		return false
	}
}

func sweeps(t testing.TB, m *metrics.Metrics, trigger string) float64 {
	t.Helper()
	return val(t, m.CacheSweeps.WithLabelValues(trigger))
}

// TestR_BAS_089_ACommitNeverTakesAnAreaPastItsLimit is the defect of W-34 reproduced: 32 fills of 64 KiB (2 MiB) against an
// artifact area of 896 KiB, with no sweep in between. Before R-BAS-089 the area held 960 KiB after the fifteenth commit, and
// went on to 2 MiB; now the commit that would pass the limit is refused, counted, asks for one sweep, and leaves nothing on
// the volume.
func TestR_BAS_089_ACommitNeverTakesAnAreaPastItsLimit(t *testing.T) {
	r := newRig(t, megabyteCache)
	limit, _ := r.limits()
	kept, refused := 0, 0
	for i := range 32 {
		err := r.commit(EntryArtifact, payload(uint64(1000+i), 64*kib))
		switch {
		case err == nil:
			kept++
		case errors.Is(err, ErrNoRoom):
			refused++
		default:
			t.Fatal(err)
		}
		if u := r.usage(); u.Artifacts > limit {
			t.Fatalf("after commit %d the artifacts hold %d bytes, above their limit of %d", i+1, u.Artifacts, limit)
		}
	}
	if kept != 14 || refused != 18 { // 14 x 64 KiB = 896 KiB is the limit itself
		t.Fatalf("kept %d, refused %d; want 14 and 18", kept, refused)
	}
	if got := val(t, r.m.CacheNoRoom.WithLabelValues(metrics.AreaArtifacts)); got != 18 {
		t.Errorf("quader_bastion_cache_no_room_total{artifacts} = %v, want 18", got)
	}
	if !r.pendingSweep() || r.pendingSweep() {
		t.Error("the commits over the high mark did not leave exactly one request for a sweep")
	}
	if left := filesBelow(t, r.back.spoolDir); left != 0 {
		t.Errorf("%d files of refused fills are left in the spool", left)
	}
}

// TestR_BAS_089_AFillWithoutRoomStillAnswersFromItsFile: a kept fill (the engine's) that is refused for want of room serves
// its bytes from its file until it is aborted, and nothing of it reaches the area.
func TestR_BAS_089_AFillWithoutRoomStillAnswersFromItsFile(t *testing.T) {
	r := newRig(t, megabyteCache)
	for i := range 14 {
		if err := r.commit(EntryArtifact, payload(uint64(i), 64*kib)); err != nil {
			t.Fatal(err)
		}
	}
	data := payload(99, 64*kib)
	f, err := r.NewFill(EntryArtifact, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f.Keep()
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Commit(context.Background(), digest(data)); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("commit into a full area: %v", err)
	}
	got := make([]byte, len(data))
	if _, err := f.Reader().ReadAt(got, 0); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("the refused fill does not answer from its file: %v", err)
	}
	f.Abort()
	if _, err := r.Backend().HeadObject(context.Background(), digest(data)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the refused bytes are in the area: %v", err)
	}
}

// TestR_BAS_089_TheHighMarkAsksForASweepAtCommitAndAtAnnouncement: the commit that takes the area over 95 % asks for a sweep,
// and so does a fill whose announced length would; a burst of commits leaves one request, never one each.
func TestR_BAS_089_TheHighMarkAsksForASweepAtCommitAndAtAnnouncement(t *testing.T) {
	r := newRig(t, megabyteCache)
	for i := range 13 { // 832 KiB, below 95 % of 896 KiB (851.2 KiB)
		if err := r.commit(EntryArtifact, payload(uint64(i), 64*kib)); err != nil {
			t.Fatal(err)
		}
	}
	if r.pendingSweep() {
		t.Fatal("a sweep was asked for below the high mark")
	}
	f, err := r.NewFill(EntryArtifact, 64*kib)
	if err != nil {
		t.Fatal(err)
	}
	f.Abort()
	if !r.pendingSweep() {
		t.Fatal("a fill announced above the high mark did not ask for a sweep")
	}
	if g, err := r.NewFill(EntryArtifact, -1); err != nil || r.pendingSweep() {
		t.Fatalf("a fill without a length asked for a sweep: %v", err)
	} else {
		g.Abort()
	}
	if err := r.commit(EntryArtifact, payload(13, 64*kib)); err != nil {
		t.Fatal(err)
	}
	if !r.pendingSweep() {
		t.Fatal("the commit over the high mark did not ask for a sweep")
	}
	for i := range 5 {
		_ = r.commit(EntryArtifact, payload(uint64(50+i), 4*kib))
	}
	if !r.pendingSweep() || r.pendingSweep() {
		t.Fatal("a burst of commits over the high mark left other than one request")
	}
}

// TestR_BAS_089_EveryListingSetsTheEstimate: bytes that another replica or an earlier configuration wrote are found by the
// first commit's listing; bytes another replica's sweep evicted are found by the next listing, after which the area takes
// fills again; a content address the area holds already costs no room.
func TestR_BAS_089_EveryListingSetsTheEstimate(t *testing.T) {
	r := newRig(t, megabyteCache)
	var held []string
	for i := range 14 {
		held = append(held, r.putObject(EntryArtifact, payload(uint64(i), 64*kib)))
	}
	if err := r.commit(EntryArtifact, payload(77, 4*kib)); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("the first commit did not see what the area held before it: %v", err)
	}
	if err := r.commit(EntryArtifact, payload(3, 64*kib)); err != nil {
		t.Fatalf("bytes held already were refused: %v", err)
	}
	for _, sha := range held[:4] { // another replica's sweep, unseen
		if err := r.Backend().DeleteObject(context.Background(), sha); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.commit(EntryArtifact, payload(77, 4*kib)); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("an eviction was known before a listing: %v", err)
	}
	if err := r.SweepOnce(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := r.commit(EntryArtifact, payload(77, 4*kib)); err != nil {
		t.Fatalf("the listing of the sweep did not set the estimate: %v", err)
	}
	if got, want := r.est.bytes(areaArtifacts), int64(10*64*kib+4*kib); got != want {
		t.Errorf("estimate %d, want %d", got, want)
	}
	if sweeps(t, r.m, metrics.SweepInterval) != 1 || sweeps(t, r.m, metrics.SweepSize) != 0 {
		t.Errorf("sweeps by trigger: interval %v, size %v", sweeps(t, r.m, metrics.SweepInterval), sweeps(t, r.m, metrics.SweepSize))
	}
}

// TestR_BAS_089_DocumentsHaveTheirOwnRoom: a full documents area refuses documents and takes artifacts, and the other way.
func TestR_BAS_089_DocumentsHaveTheirOwnRoom(t *testing.T) {
	r := newRig(t, megabyteCache)
	for i := range 2 {
		if err := r.commit(EntryDocument, payload(uint64(i), 64*kib)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.commit(EntryDocument, payload(9, kib)); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("a document past 128 KiB: %v", err)
	}
	if err := r.commit(EntryArtifact, payload(9, kib)); err != nil {
		t.Fatalf("an artifact beside full documents: %v", err)
	}
	if val(t, r.m.CacheNoRoom.WithLabelValues(metrics.AreaDocuments)) != 1 || val(t, r.m.CacheNoRoom.WithLabelValues(metrics.AreaArtifacts)) != 0 {
		t.Error("the refusal was not counted in the documents' area alone")
	}
}

// TestR_BAS_089_ASharedBucketCountsEachCommitOncePerReplica: with the bucket backend and bastion.replicas 2 a replica counts
// its commits twice; a replica without the sweep lease lists only after it committed, and deletes nothing.
func TestR_BAS_089_ASharedBucketCountsEachCommitOncePerReplica(t *testing.T) {
	r := newRig(t, megabyteCache, func(c *config.BastionCache) { c.Backend = "bucket" }) // the store stays a volume
	r.est = newEstimate(2)
	kept := 0
	for i := range 14 {
		if err := r.commit(EntryArtifact, payload(uint64(i), 64*kib)); err == nil {
			kept++
		}
	}
	if kept != 7 {
		t.Fatalf("kept %d fills of 64 KiB against 896 KiB counted twice, want 7", kept)
	}
	lease := &fakeLease{}
	st := leaseSource{lease: lease, shared: true}
	if err := r.SweepOnce(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if got := r.est.bytes(areaArtifacts); got != 7*64*kib {
		t.Fatalf("after the listing of a replica without the lease the estimate is %d, want what the bucket holds", got)
	}
	if sweeps(t, r.m, metrics.SweepInterval) != 0 {
		t.Error("a replica without the lease counted a sweep")
	}
	r.putObject(EntryArtifact, payload(500, 64*kib)) // another replica's fill
	if err := r.SweepOnce(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if got := r.est.bytes(areaArtifacts); got != 7*64*kib {
		t.Fatalf("a replica that committed nothing since its listing listed again: estimate %d", got)
	}
}

// TestR_BAS_089_TheSealedLayoutCountsStoredBytes: with a cache key the area counts the objects of the stored format, which
// are what a listing finds: two plaintexts of 64 KiB fit an area of 128 KiB, their sealed objects do not.
func TestR_BAS_089_TheSealedLayoutCountsStoredBytes(t *testing.T) {
	r := sealedRig(t, t.TempDir(), writeKeyFile(t, keyK()), newClock(), func(c *config.BastionCache) {
		c.MaxBytes, c.DocumentsMaxBytes, c.MaxObjectBytes = 256*kib, 128*kib, 128*kib
	})
	if err := r.commit(EntryArtifact, payload(1, 64*kib)); err != nil {
		t.Fatal(err)
	}
	if err := r.commit(EntryArtifact, payload(2, 64*kib)); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("the second sealed object passed the area: %v", err)
	}
	stored, _ := blobcrypt.StoredSize(64 * kib)
	if got, want := r.usage().Artifacts, frameSize(idK)+stored; got != want || r.est.bytes(areaArtifacts) != want {
		t.Fatalf("the area holds %d, the estimate says %d, want %d", got, r.est.bytes(areaArtifacts), want)
	}
}

// TestR_BAS_089_TheSweeperListsAtStartAndSweepsAtOnceWhenAsked: a cache over its high mark at start is swept by the listing
// of the start, counted by its trigger, without waiting for sweep_interval; a request while the sweeper waits is served by
// one sweep.
func TestR_BAS_089_TheSweeperListsAtStartAndSweepsAtOnceWhenAsked(t *testing.T) {
	r := newRig(t, megabyteCache, func(c *config.BastionCache) { c.SweepInterval = time.Hour })
	for i := range 16 { // 1 MiB of artifacts: the area was larger before max_bytes was lowered
		r.putObject(EntryArtifact, payload(uint64(i), 64*kib))
	}
	r.gap = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.RunSweeper(ctx, nil) }()
	defer func() { cancel(); <-done }()
	limit, _ := r.limits()
	// The sweep is counted when it starts and sets the estimate from its last listing when it ends: until then the area is full
	// and a commit is refused, so the test waits for the estimate as well.
	eventually(t, "the sweep of the start", func() bool {
		return sweeps(t, r.m, metrics.SweepSize) == 1 && r.usage().Artifacts <= percent(limit, sweepTargetPercent) &&
			r.est.bytes(areaArtifacts) <= percent(limit, sweepTargetPercent)
	})
	for i := range 3 {
		if err := r.commit(EntryArtifact, payload(uint64(100+i), 64*kib)); err != nil {
			t.Fatal(err)
		}
	}
	// The fill announced above the mark may ask before its commit does: one or two more sweeps.
	eventually(t, "the sweep a commit asked for", func() bool {
		return sweeps(t, r.m, metrics.SweepSize) >= 2 && r.usage().Artifacts <= percent(limit, sweepTargetPercent)
	})
	if sweeps(t, r.m, metrics.SweepInterval) != 0 {
		t.Error("the timer swept")
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within 5 s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
