package limits

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"
)

// Q-LIM-66: a scan token's request is system work: the subject system with the credential vuln-scanner-trivy, recorded under the
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

// Q-LIM-80: the malware scanner's read with a blob token is system work with the credential malware-scanner-clamav, and limited by nothing;
// a system subject of another name keeps the image scanner's credential.
func TestQ_LIM_80_TheMalwareScannersReadIsRecordedUnderSystemWithItsOwnCredential(t *testing.T) {
	s, ok := SubjectOf(Caller{Kind: CallerSystem, System: MalwareScanCredential})
	if !ok || s.Kind != KindSystem || s.Credential != MalwareScanCredential || s.String() != "system" {
		t.Fatalf("subject %+v", s)
	}
	if s, _ := SubjectOf(Caller{Kind: CallerSystem, System: "vuln-scanner-trivy"}); s.Credential != ScanCredential {
		t.Fatalf("the image scanner: %+v", s)
	}
	r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
	c := r.e.Begin(Caller{Kind: CallerSystem, System: MalwareScanCredential}, qname.Repo{Project: "web", Key: "npm-local"})
	if st := r.e.Check(context.Background(), c, r.clk.now(), nil); st.Err != nil {
		t.Fatalf("checked: %v", st.Err)
	}
	c.AddCached(68)
	r.e.Finish(c)
	r.e.Flush(context.Background())
	r.usage.mu.Lock()
	defer r.usage.mu.Unlock()
	if len(r.usage.d) == 0 || r.usage.d[0].Subject.Credential != MalwareScanCredential {
		t.Fatalf("recorded %+v", r.usage.d)
	}
}
