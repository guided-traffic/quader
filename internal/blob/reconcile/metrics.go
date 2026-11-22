package reconcile

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/observability"
)

// The names of the gauges every web and worker replica exports from PostgreSQL (R-STOR-056, R-STOR-060, ha-and-helm.md Q-HA-393):
// the alert QuaderStorageReplicationBacklog reads the age of the oldest queue row of a connection that is not filling, and
// QuaderStorageConnectionDetached a connection that holds copies and that no live replica configures.
const (
	MetricQueue         = "quader_storage_reconcile_queue"
	MetricQueueOldest   = "quader_storage_reconcile_queue_oldest_seconds"
	MetricFilling       = "quader_storage_connection_filling"
	MetricDetached      = "quader_storage_connection_detached"
	metricCopied        = "quader_storage_reconcile_copied_total"
	metricCopiedBytes   = "quader_storage_reconcile_copied_bytes_total"
	metricFailures      = "quader_storage_reconcile_failures_total"
	modeServer          = "server"
	modeStream          = "stream"
	helpQueue           = "Queue rows of the reconciliation by connection, from PostgreSQL (aggregate with max across replicas; R-STOR-056)."
	helpQueueOldest     = "Age in seconds of the oldest queue row of the reconciliation by connection, from PostgreSQL (aggregate with max)."
	helpFilling         = "1 while the connection is filling: configured without drain and no full pass has ended with every live blob on it (R-STOR-051)."
	helpDetached        = "1 while the connection is detached: it holds copies and no live replica has it in its configuration (R-STOR-051)."
	helpCopied          = "Copies the reconciliation made, by target connection and mode (server, stream)."
	helpCopiedBytes     = "Bytes of the copies the reconciliation made, by target connection and mode (server, stream)."
	helpFailures        = "Copies the reconciliation tried and could not make, by target connection and reason (R-STOR-056, Q-STOR-258)."
	connectionLabel     = "connection"
	failureLabelsReason = "reason"
)

// The reasons of quader_storage_reconcile_failures_total (Q-STOR-258).
const (
	// ReasonNoSource: no other connection that the worker reaches holds a present copy of the blob.
	ReasonNoSource = "no_source"
	// ReasonUnavailable: a transient error or an open circuit, at the target or at every source.
	ReasonUnavailable = "unavailable"
	// ReasonSourceDamaged: every source that answered was found missing or corrupt, and was marked so.
	ReasonSourceDamaged = "source_damaged"
	// ReasonRefused: the target answered a request with an error that retrying soon does not fix (a permission, a refused
	// request).
	ReasonRefused = "refused"
	// ReasonClaimed: the target's copy row is claimed for deletion by a drain that has not finished.
	ReasonClaimed = "claimed"
)

// Metrics are the counters of the reconciliation's runs. A nil *Metrics records nothing.
type Metrics struct {
	copied      *prometheus.CounterVec
	copiedBytes *prometheus.CounterVec
	failures    *prometheus.CounterVec
}

// NewMetrics registers the counters on m. It panics, like every registration in internal/observability, when called twice for
// one registry.
func NewMetrics(m *observability.Metrics) *Metrics {
	return &Metrics{
		copied:      m.NewCounterVec(metricCopied, helpCopied, connectionLabel, "mode"),
		copiedBytes: m.NewCounterVec(metricCopiedBytes, helpCopiedBytes, connectionLabel, "mode"),
		failures:    m.NewCounterVec(metricFailures, helpFailures, connectionLabel, failureLabelsReason),
	}
}

func (m *Metrics) copy(conn, mode string, bytes int64) {
	if m == nil {
		return
	}
	m.copied.WithLabelValues(conn, mode).Inc()
	if bytes > 0 {
		m.copiedBytes.WithLabelValues(conn, mode).Add(float64(bytes))
	}
}

func (m *Metrics) failure(conn, reason string) {
	if m != nil {
		m.failures.WithLabelValues(conn, reason).Inc()
	}
}

// Gauges are the descriptors of the gauges read from PostgreSQL; the composition root adds them as a source of
// observability.DBGauges, which evaluates them in the background (Q-OPS-18).
type Gauges struct{ queue, oldest, filling, detached *prometheus.Desc }

// NewGauges returns the descriptors.
func NewGauges() Gauges {
	l := []string{connectionLabel}
	return Gauges{
		queue:    prometheus.NewDesc(MetricQueue, helpQueue, l, nil),
		oldest:   prometheus.NewDesc(MetricQueueOldest, helpQueueOldest, l, nil),
		filling:  prometheus.NewDesc(MetricFilling, helpFilling, l, nil),
		detached: prometheus.NewDesc(MetricDetached, helpDetached, l, nil),
	}
}

// Descs lists the descriptors.
func (g Gauges) Descs() []*prometheus.Desc {
	return []*prometheus.Desc{g.queue, g.oldest, g.filling, g.detached}
}

// GaugeRow is one connection's values: its queue rows, the age of the oldest in seconds, whether it is filling and whether it is
// detached.
type GaugeRow struct {
	Connection    string
	Queued        int64
	OldestSeconds float64
	Filling       bool
	Detached      bool
}

// Metrics renders the rows: four series per connection that is not retired, so a connection with an empty queue shows 0.
func (g Gauges) Metrics(rows []GaugeRow) []prometheus.Metric {
	out := make([]prometheus.Metric, 0, 4*len(rows))
	for _, r := range rows {
		out = append(out,
			prometheus.MustNewConstMetric(g.queue, prometheus.GaugeValue, float64(r.Queued), r.Connection),
			prometheus.MustNewConstMetric(g.oldest, prometheus.GaugeValue, r.OldestSeconds, r.Connection),
			prometheus.MustNewConstMetric(g.filling, prometheus.GaugeValue, one(r.Filling), r.Connection),
			prometheus.MustNewConstMetric(g.detached, prometheus.GaugeValue, one(r.Detached), r.Connection))
	}
	return out
}

func one(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
