package catalogue

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// The metrics of the catalogue (R-CUR-019). The label repo is a repository's qualified name, capped as R-OPS-007 caps it.
const (
	// MetricRequests counts the catalogue's calls by repository, call and outcome.
	MetricRequests = "quader_remote_catalogue_requests_total"
	// MetricSeconds is the duration of a call that was not answered from the cache.
	MetricSeconds = "quader_remote_catalogue_seconds"
)

// Metrics are the families of the catalogue. A nil *Metrics counts nothing.
type Metrics struct {
	Requests *prometheus.CounterVec
	Seconds  *prometheus.HistogramVec
	label    func(family, repo string) string
}

// NewMetrics registers the families in reg (nil keeps them unregistered); label caps the repo label, nil keeps it.
func NewMetrics(reg prometheus.Registerer, label func(family, repo string) string) (*Metrics, error) {
	m := &Metrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricRequests,
			Help: "Calls of the upstream catalogue of remote repositories by repository, call and outcome (R-CUR-019).",
		}, []string{"repo", "call", "outcome"}),
		Seconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    MetricSeconds,
			Help:    "Duration of a call of the upstream catalogue that was not answered from its cache, by call (R-CUR-019).",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20},
		}, []string{"call"}),
		label: label,
	}
	if reg != nil {
		for _, c := range []prometheus.Collector{m.Requests, m.Seconds} {
			if err := reg.Register(c); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

// Forget drops the series of a repository whose label went (observability.CappedLabels.OnForget).
func (m *Metrics) Forget(repo string) {
	if m == nil {
		return
	}
	m.Requests.DeletePartialMatch(prometheus.Labels{"repo": repo})
}

func (m *Metrics) count(repo, call, outcome string) {
	if m == nil {
		return
	}
	if m.label != nil {
		repo = m.label(MetricRequests, repo)
	}
	m.Requests.WithLabelValues(repo, call, outcome).Inc()
}

func (m *Metrics) observe(call string, d time.Duration) {
	if m == nil {
		return
	}
	m.Seconds.WithLabelValues(call).Observe(d.Seconds())
}
