package limits

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/qname"
)

// backgroundTotals sums what the usage store learnt of one subject: requests, background downloads, uncached bytes and upstream
// requests, and the limits met.
func (u *usageRecorder) backgroundTotals(subject string) (requests, bg, uncached, upstream int64, events map[string]int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	events = map[string]int64{}
	for _, d := range u.d {
		if d.Subject.String() != subject {
			continue
		}
		requests += d.Requests
		bg += d.Background
		uncached += d.Uncached
		upstream += d.Upstream
		for _, e := range d.Events {
			events[string(e.Mode)+":"+e.Figure] += e.Count
		}
	}
	return
}

// R-CUR-010, Q-CUR-17: a person's download ahead of one version is charged as a probe's background download is: one background
// download and one upstream request of the person's budget key, and no request of the management API. Unit tier, milliseconds.
func TestR_CUR_010_APersonsDownloadOfOneVersionIsChargedToThePerson(t *testing.T) {
	ctx := context.Background()
	repo := qname.Repo{Project: "platform", Key: "docker-hub"}
	r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
	caller := anonCaller("198.51.100.30")
	ticket, ok, err := r.e.StartDownload(ctx, caller, repo, 21)
	if err != nil || !ok || !ticket.Charged() || ticket.Ceiling != CeilingKey(21, CeilingAnonymous) {
		t.Fatalf("ticket %+v, ok %t, err %v", ticket, ok, err)
	}
	r.e.Flush(ctx)
	s, _ := SubjectOf(caller)
	requests, bg, _, upstream, _ := r.usage.backgroundTotals(s.String())
	if requests != 0 || bg != 1 || upstream != 1 {
		t.Fatalf("requests %d, background downloads %d, upstream requests %d; want 0, 1, 1", requests, bg, upstream)
	}
	var none *Engine
	if _, ok, err := none.StartDownload(ctx, caller, repo, 21); ok || err != nil {
		t.Errorf("an installation without the limits charged the download: %t, %v", ok, err)
	}
}

func TestR_LIM_037_ABackgroundDownloadIsChargedToTheProbe(t *testing.T) {
	ctx := context.Background()
	repo := qname.Repo{Project: "platform", Key: "docker-hub"}
	t.Run("the decision counts one upstream request and one background download of the probe's budget key", func(t *testing.T) {
		r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
		probe := r.e.Begin(anonCaller("198.51.100.20"), repo)
		ticket, ok, err := probe.StartBackground(ctx, 21)
		if err != nil || !ok || !ticket.Charged() || ticket.Key != probe.Key {
			t.Fatalf("ticket %+v, ok %t, err %v", ticket, ok, err)
		}
		if ticket.Ceiling != CeilingKey(21, CeilingAnonymous) {
			t.Fatalf("the ticket names the ceiling %q", ticket.Ceiling)
		}
		r.e.Finish(probe)
		r.e.Flush(ctx)
		requests, bg, _, upstream, _ := r.usage.backgroundTotals(probe.Subject.String())
		if requests != 1 || bg != 1 || upstream != 1 {
			t.Fatalf("requests %d, background downloads %d, upstream requests %d; want 1, 1, 1", requests, bg, upstream)
		}
		res, err := r.store.mem.Run(ctx, readUpstream(r, probe.Key))
		if err != nil || res[0].Cur != 1 {
			t.Fatalf("the budget key's upstream window holds %v (%v), want 1", res, err)
		}
	})

	t.Run("Q-LIM-54 the worker's charge counts uncached bytes and every exchange after the first, and no second background download", func(t *testing.T) {
		r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
		probe := r.e.Begin(anonCaller("198.51.100.21"), repo)
		ticket, _, err := probe.StartBackground(ctx, 21)
		if err != nil {
			t.Fatal(err)
		}
		r.e.Finish(probe)
		r.e.Flush(ctx)
		raw, err := json.Marshal(ticket)
		if err != nil {
			t.Fatal(err)
		}
		var back Ticket
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		bg := r.e.Resume(back, 21)
		if bg.Key != probe.Key || bg.Subject.String() != probe.Subject.String() || bg.Resolution.Place.Key() != probe.Resolution.Place.Key() {
			t.Fatalf("resumed %s at %s, want %s at %s", bg.Subject, bg.Key, probe.Subject, probe.Key)
		}
		bg.Exchange() // the first: counted by the decision
		bg.Exchange()
		bg.Exchange()
		bg.AddUncached(4096)
		bg.Done()
		r.e.Flush(ctx)
		requests, bgs, uncached, upstream, _ := r.usage.backgroundTotals(probe.Subject.String())
		if requests != 1 || bgs != 1 || uncached != 4096 || upstream != 3 {
			t.Fatalf("requests %d, background downloads %d, uncached %d, upstream %d; want 1, 1, 4096, 3", requests, bgs, uncached, upstream)
		}
		res, err := r.store.mem.Run(ctx, readUncached(r, CeilingKey(21, CeilingAnonymous)))
		if err != nil || res[0].Cur != 4096 {
			t.Fatalf("the ceiling's uncached window holds %v (%v), want 4096", res, err)
		}
		if a := r.e.Admit(ctx, bg); a != nil {
			t.Fatalf("a background download is admitted without a check: %v", a)
		}
	})

	t.Run("R-LIM-022 in enforce a used-up upstream budget starts nothing and counts no background download", func(t *testing.T) {
		f := BuiltinProfiles()[ProfileAnonymous]
		f[UpstreamRequestsPerHour] = 1
		r := newRig(t, ModeEnforce, f)
		first := r.e.Begin(anonCaller("198.51.100.22"), repo)
		if _, _, err := first.StartBackground(ctx, 5); err != nil {
			t.Fatal(err)
		}
		r.e.Finish(first)
		probe := r.e.Begin(anonCaller("198.51.100.22"), repo)
		_, ok, err := probe.StartBackground(ctx, 5)
		if !ok || codeOf(err) != CodeUpstreamLimitReached {
			t.Fatalf("ok %t, err %v; want the refusal of step 9a", ok, err)
		}
		r.e.Finish(probe)
		r.e.Flush(ctx)
		_, bg, _, _, events := r.usage.backgroundTotals(probe.Subject.String())
		if bg != 1 || events["enforce:upstreamRequestsPerHour"] != 1 {
			t.Fatalf("background downloads %d (want the first only), events %v", bg, events)
		}
	})

	t.Run("R-LIM-026 in audit it starts and is recorded as would_limit", func(t *testing.T) {
		f := BuiltinProfiles()[ProfileAnonymous]
		f[UpstreamRequestsPerHour] = 1
		r := newRig(t, ModeAudit, f)
		for range 2 {
			probe := r.e.Begin(anonCaller("198.51.100.23"), repo)
			ticket, _, err := probe.StartBackground(ctx, 5)
			if err != nil || !ticket.Charged() {
				t.Fatalf("audit refused a background download: %v", err)
			}
			r.e.Finish(probe)
		}
		r.e.Flush(ctx)
		_, bg, _, _, events := r.usage.backgroundTotals("anonymous:198.51.100.23")
		if bg != 2 || events["audit:upstreamRequestsPerHour"] != 1 {
			t.Fatalf("background downloads %d, events %v; want 2 and one would_limit", bg, events)
		}
	})

	t.Run("a request without a charge starts a download charged to nobody", func(t *testing.T) {
		var c *Charge
		ticket, ok, err := c.StartBackground(ctx, 5)
		if ok || err != nil || ticket.Charged() {
			t.Fatalf("ticket %+v ok %t err %v", ticket, ok, err)
		}
		r := newRig(t, ModeEnforce, BuiltinProfiles()[ProfileAnonymous])
		if r.e.Resume(Ticket{}, 5) != nil {
			t.Fatal("an empty ticket resumed a charge")
		}
	})
}

func readUpstream(r *rig, key string) *cluster.LimitBatch {
	var b cluster.LimitBatch
	idx, _ := cluster.WindowIndex(r.clk.now(), hour)
	b.Add(cluster.LimitOp{Kind: cluster.OpWindowRead, Figure: ctrUpstreamHour, Bucket: cluster.LimitBucket(key), Window: hour, Index: idx})
	return &b
}

func readUncached(r *rig, key string) *cluster.LimitBatch {
	var b cluster.LimitBatch
	idx, _ := cluster.WindowIndex(r.clk.now(), hour)
	b.Add(cluster.LimitOp{Kind: cluster.OpWindowRead, Figure: ctrUncachedHour, Bucket: cluster.LimitBucket(key), Window: hour, Index: idx})
	return &b
}
