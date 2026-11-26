//go:build integration

package bastioncache

import (
	"context"
	"errors"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_BAS_089_TwoReplicasSharingABucketKeepItBelowItsLimit: two replicas of a release of two on one bucket, an artifact
// area of 32 KiB, fills of 4 KiB. Each counts its own commits twice (Q-BAS-302) and learns the other's bytes from its listings:
// replica a keeps four, replica b's first commit lists them and b keeps two, and the bucket never holds more than the area.
func TestR_BAS_089_TwoReplicasSharingABucketKeepItBelowItsLimit(t *testing.T) {
	e := newEnv(t, "room")
	ctx := context.Background()
	small := func(c *config.BastionCache) { c.MaxBytes, c.DocumentsMaxBytes = 40*1024, 8*1024 }
	a, b := e.replicaOf(2, small), e.replicaOf(2, small)
	const area = 32 * 1024
	held := func() int64 {
		t.Helper()
		u, err := a.Backend().Usage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return u.Artifacts
	}
	fill := func(r *replica, seed uint64) error {
		t.Helper()
		data := payload(seed, 4*1024)
		f, err := r.NewFill(cache.EntryArtifact, int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Abort()
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
		err = f.Commit(ctx, digest(data))
		if got := held(); got > area {
			t.Fatalf("the bucket holds %d bytes of artifacts, above the area of %d", got, area)
		}
		return err
	}
	kept := func(r *replica, from uint64, n int) int {
		t.Helper()
		k := 0
		for i := range n {
			switch err := fill(r, from+uint64(i)); {
			case err == nil:
				k++
			case !errors.Is(err, cache.ErrNoRoom):
				t.Fatal(err)
			}
		}
		return k
	}
	if k := kept(a, 100, 6); k != 4 {
		t.Fatalf("replica a kept %d fills of 4 KiB counted twice against 32 KiB, want 4", k)
	}
	if k := kept(b, 200, 4); k != 2 {
		t.Fatalf("replica b kept %d after its first listing found a's 16 KiB, want 2", k)
	}
	if got := held(); got != 24*1024 {
		t.Fatalf("the bucket holds %d bytes, want 24 KiB", got)
	}
	// Without Valkey every replica sweeps: a's sweep lists b's bytes, and a fits what is left.
	if err := a.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if k := kept(a, 300, 2); k != 1 {
		t.Fatalf("replica a kept %d after its listing found 24 KiB, want 1", k)
	}
	for _, r := range []*replica{a, b} {
		if value(t, r.m.CacheNoRoom.WithLabelValues(metrics.AreaArtifacts)) == 0 {
			t.Error("a replica counted no fill without room")
		}
	}
}
