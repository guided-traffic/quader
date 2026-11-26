package server

import (
	"context"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/guided-traffic/quader/internal/bastion/budget"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	bmetrics "github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/shared"
	"github.com/guided-traffic/quader/internal/bastion/throughput"
	"github.com/guided-traffic/quader/internal/bastion/usage"
)

// reporting is what a replica reports with every flush of its download buffer (R-LIM-045): the usage its budgets counted and its
// state. Each figure of the state is the replica's own, except where its comment says it is the Bastion's (R-BAS-096's table of
// the status, Q-BAS-474).
type reporting struct {
	replica string
	m       *bmetrics.Metrics
	usage   *usage.Buffer
	budgets *budget.Budgets
	pacer   *throughput.Pacer
	guard   *guard.Instance
	state   *shared.State
	// sharedCache is the bucket backend: the cache's figures are the whole Bastion's, which every replica reports alike (Q-BAS-480).
	sharedCache bool
	now         func() time.Time
}

// report drains the usage and builds the state.
func (r *reporting) report() (iface.UsageReport, iface.ReplicaStatus) {
	return r.usage.Drain(), r.status()
}

// status is the replica's state now (R-LIM-045, R-UI-240).
func (r *reporting) status() iface.ReplicaStatus {
	st := iface.ReplicaStatus{Replica: r.replica, At: r.now().UTC()}
	st.Cache.Shared = r.sharedCache
	m := r.m
	// The cache: what the replica's view of its backend holds and may hold, and what of it is protected (the Bastion's own figure
	// with the bucket backend, which every replica reports alike, the replica's own with a volume); hits, misses and evictions are
	// this replica's since its start.
	for _, a := range []string{bmetrics.AreaArtifacts, bmetrics.AreaDocuments} {
		st.Cache.Bytes += int64(gaugeValue(m.CacheBytes.WithLabelValues(a)))
		st.Cache.CapacityBytes += int64(gaugeValue(m.CacheCapacity.WithLabelValues(a)))
		st.Cache.ProtectedBytes += int64(gaugeValue(m.CacheProtected.WithLabelValues(a)))
	}
	for _, c := range []string{bmetrics.ClassArtifact, bmetrics.ClassShared, bmetrics.ClassPrivate, bmetrics.ClassAbsent, bmetrics.ClassPass} {
		st.Cache.Hits += int64(counterValue(m.Requests.WithLabelValues(c, bmetrics.ResultHit)) + counterValue(m.Requests.WithLabelValues(c, bmetrics.ResultChecked)))
		st.Cache.Misses += int64(counterValue(m.Requests.WithLabelValues(c, bmetrics.ResultMiss)))
	}
	for _, e := range []string{bmetrics.EvictSize, bmetrics.EvictStale, bmetrics.EvictMissing} {
		st.Cache.Evictions += int64(counterValue(m.CacheEvictions.WithLabelValues(e)))
	}
	if r.pacer != nil {
		st.Throughput = r.pacer.Status()
	}
	var blocks []iface.StatusBlock
	if r.budgets != nil {
		st.Limited = r.budgets.Limited()
		blocks = append(blocks, r.budgets.Blocks()...)
	}
	if r.guard != nil {
		blocks = append(blocks, r.guard.Blocks()...)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Until.After(blocks[j].Until) })
	st.Blocks = blocks[:min(len(blocks), iface.MaxStatusBlocks)]
	st.Blocked = int64(len(blocks))
	if r.state != nil && r.state.Blocks != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if n, err := r.state.Blocks.Size(ctx); err == nil {
			st.Blocked = max(st.Blocked, int64(n))
		}
		cancel()
	}
	return st
}

func gaugeValue(g prometheus.Gauge) float64 {
	var d dto.Metric
	if g.Write(&d) != nil || d.Gauge == nil {
		return 0
	}
	return d.Gauge.GetValue()
}

func counterValue(c prometheus.Counter) float64 {
	var d dto.Metric
	if c.Write(&d) != nil || d.Counter == nil {
		return 0
	}
	return d.Counter.GetValue()
}
