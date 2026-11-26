package downloads_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/downloads"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_LIM_045_EveryFlushSendsTheUsageAndTheStateAlsoWithoutDownloads: while downloads are not recorded every flush still sends a
// batch with the usage and the replica's state and no event, and a usage the main installation did not take is handed back. Rows
// of bytes, a flush of 20 ms.
func TestR_LIM_045_EveryFlushSendsTheUsageAndTheStateAlsoWithoutDownloads(t *testing.T) {
	var mu sync.Mutex
	var restored []iface.UsageReport
	calls := 0
	e := newEnv(t, func(c *config.BastionAudit, o *downloads.ReporterOptions) {
		c.BatchSize = 2
		o.Report = func() (iface.UsageReport, iface.ReplicaStatus) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return iface.UsageReport{BucketSeconds: 300, Rows: []iface.UsageRow{{Subject: "user:u1", Repository: "web/npm-all", Requests: int64(calls)}}},
				iface.ReplicaStatus{Replica: "replica-a", At: time.Now()}
		}
		o.Restore = func(u iface.UsageReport) { mu.Lock(); restored = append(restored, u); mu.Unlock() }
	})
	failOnce := true
	e.snd.fail = func(int) error {
		if failOnce {
			failOnce = false
			return errors.New("the main installation is away")
		}
		return nil
	}
	e.run(t)
	eventually(t, "two batches without downloads", func() bool { return len(e.snd.all()) >= 2 })
	for _, b := range e.snd.all()[:2] {
		if len(b.Events) != 0 || b.Status.Replica != "replica-a" || len(b.Usage.Rows) != 1 {
			t.Fatalf("a batch without downloads %+v", b)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(restored) == 0 || restored[0].Rows[0].Requests != 1 {
		t.Fatalf("the usage of the failed send was not handed back: %+v", restored)
	}
}

// TestR_LIM_045_OnlyTheFirstBatchOfAFlushCarriesTheReport: five events in batches of two send the report once.
func TestR_LIM_045_OnlyTheFirstBatchOfAFlushCarriesTheReport(t *testing.T) {
	reports := 0
	e := newEnv(t, func(c *config.BastionAudit, o *downloads.ReporterOptions) {
		c.BatchSize, c.FlushInterval = 2, time.Hour
		o.Report = func() (iface.UsageReport, iface.ReplicaStatus) {
			reports++
			return iface.UsageReport{}, iface.ReplicaStatus{Replica: "replica-a"}
		}
	})
	e.r.SetEnabled(true)
	for i := range 5 {
		e.r.Add(ev(i))
	}
	if err := e.r.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	all := e.snd.all()
	if len(all) != 3 || reports != 1 || all[0].Status.Replica != "replica-a" || all[1].Status.Replica != "" || all[2].Status.Replica != "" {
		t.Fatalf("%d batches, %d reports: %+v", len(all), reports, all)
	}
}
