package gate

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics are the gate's series on the roles that serve downloads (R-SIG-075): the downloads a gate refused by reason (the reasons of
// R-SIG-068 and pending) and how long the downloads that waited for the checks of their signatures waited, by outcome (served, refused,
// pending). The alert QuaderSignatureGatePending reads the second. A nil *Metrics records nothing.
type Metrics struct {
	refused *prometheus.CounterVec
	wait    *prometheus.HistogramVec
}

// NewMetrics registers the families on reg; a registry that holds them already hands them back.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		refused: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_signatures_downloads_refused_total",
			Help: "Downloads a repository's signature gate refused, by reason: missing, untrusted, not_chosen, invalid, blocked, not_checked, pending (R-SIG-068).",
		}, []string{"reason"}),
		wait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "quader_signatures_download_wait_seconds",
			Help:    "How long a download waited for the checks of its signatures at a repository's gate, by outcome: served, refused, pending (R-SIG-067).",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 20, 25, 30, 60},
		}, []string{"outcome"}),
	}
	var err error
	if m.refused, err = register(reg, m.refused); err != nil {
		return nil, err
	}
	if m.wait, err = register(reg, m.wait); err != nil {
		return nil, err
	}
	return m, nil
}

func register[C prometheus.Collector](reg prometheus.Registerer, c C) (C, error) {
	if err := reg.Register(c); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(C); ok {
				return existing, nil
			}
		}
		return c, err
	}
	return c, nil
}

func (m *Metrics) refuse(reason string) {
	if m != nil {
		m.refused.WithLabelValues(reason).Inc()
	}
}

func (m *Metrics) waited(outcome string, d time.Duration) {
	if m != nil {
		m.wait.WithLabelValues(outcome).Observe(d.Seconds())
	}
}
