package limits

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"
)

// Q-LIM-66: a scan token's request is system work: the subject system with the credential image-scanner, recorded under the
// repository, and no request figure, ceiling, block or parallel lease refuses it, whatever the figures say.
func TestQ_LIM_66_TheImageScanningsPullIsRecordedUnderSystemAndLimitedByNothing(t *testing.T) {
	s, ok := SubjectOf(Caller{Kind: CallerSystem})
	if !ok || s.Kind != KindSystem || s.Ref != "system" || s.Credential != ScanCredential || s.String() != "system" {
		t.Fatalf("subject %+v", s)
	}
	one := BuiltinProfiles()[ProfileAnonymous]
	for f := range one {
		one[f] = 1
	}
	r := newRig(t, ModeEnforce, one)
	ctx := context.Background()
	repo := qname.Repo{Project: "web", Key: "docker-local"}
	for range 20 {
		c := r.e.Begin(Caller{Kind: CallerSystem}, repo)
		if st := r.e.Check(ctx, c, r.clk.now(), nil); st.Err != nil {
			t.Fatalf("checked: %v", st.Err)
		}
		if err := r.e.Admit(ctx, c); err != nil {
			t.Fatalf("admitted: %v", err)
		}
		c.AddCached(1024)
		r.e.Finish(c)
	}
	if r.store.trips.Load() != 0 {
		t.Fatalf("%d round trips to the limits store for system work", r.store.trips.Load())
	}
	r.e.Flush(ctx)
	req, refused, cached, _, _, _ := r.usage.sum()
	if req != 20 || refused != 0 || cached != 20*1024 {
		t.Fatalf("usage: %d requests, %d refused, %d cached bytes", req, refused, cached)
	}
	r.usage.mu.Lock()
	defer r.usage.mu.Unlock()
	for _, d := range r.usage.d {
		if d.Subject.Kind != KindSystem || d.Subject.Credential != ScanCredential {
			t.Fatalf("recorded under %+v", d.Subject)
		}
	}
}
