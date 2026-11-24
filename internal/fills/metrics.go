package fills

import (
	"github.com/prometheus/client_golang/prometheus"
)

// The families of R-OPS-052.
const (
	familyEnded = "quader_remote_upstream_downloads_total"
	familyBytes = "quader_remote_upstream_download_bytes_total"
)

// metrics are the families of the downloads from upstreams (R-OPS-052): per replica, the label repo capped by R-OPS-007.
type metrics struct {
	running  prometheus.GaugeFunc
	total    *prometheus.CounterVec
	bytes    *prometheus.CounterVec
	failures prometheus.Counter
	label    func(family, name string) string
}

func newMetrics(reg prometheus.Registerer, label func(family, name string) string, running func() int) *metrics {
	if label == nil {
		label = func(_, name string) string { return name }
	}
	m := &metrics{
		running: prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "quader_remote_upstream_downloads_running",
			Help: "Downloads of artifacts from the upstreams of remote repositories that run on this replica now.",
		}, func() float64 { return float64(running()) }),
		total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: familyEnded,
			Help: "Downloads of artifacts from the upstreams of remote repositories that ended on this replica, by outcome.",
		}, []string{"repo", "outcome"}),
		bytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: familyBytes,
			Help: "Bytes read from the upstreams' artifact bodies, also those of downloads that failed.",
		}, []string{"repo"}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "quader_upstream_downloads_publish_failures_total",
			Help: "Publications of the running and finished downloads from upstreams that failed.",
		}),
		label: label,
	}
	if reg != nil {
		reg.MustRegister(m.running, m.total, m.bytes, m.failures)
	}
	return m
}

func (m *metrics) bytesOf(repo string) prometheus.Counter {
	return m.bytes.WithLabelValues(m.label(familyBytes, repo))
}

func (m *metrics) ended(repo, outcome string) {
	m.total.WithLabelValues(m.label(familyEnded, repo), outcome).Inc()
}

// Forget deletes the series of a repository that left the snapshot (R-OPS-007); the composition root registers it with the cap.
func (t *Tracker) Forget(name string) {
	l := prometheus.Labels{"repo": name}
	t.m.total.DeletePartialMatch(l)
	t.m.bytes.DeletePartialMatch(l)
}
