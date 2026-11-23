package job

import (
	"errors"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics are the worker's series of signatures on artifacts (R-SIG-039): the checks by scheme and result and their duration, the due
// rows by priority, the oldest due row, the invalid signatures with an attachment, the stale trust rows, the background fetch's
// requests and the age of the public Sigstore root in use. A nil *Metrics records nothing.
type Metrics struct {
	checks   *prometheus.CounterVec
	duration *prometheus.HistogramVec
	backlog  *prometheus.GaugeVec
	oldest   prometheus.Gauge
	invalid  prometheus.Gauge
	stale    prometheus.Gauge
	remote   *prometheus.CounterVec
	rootAge  prometheus.Gauge
}

// NewMetrics registers the families on reg; a registry that holds them already hands them back.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		checks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_signatures_checks_total",
			Help: "Checks of signatures by scheme and result: a state of R-SIG-003, or failed for a check that could not run.",
		}, []string{"scheme", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "quader_signatures_check_duration_seconds",
			Help:    "Duration of the read and the integrity check of one signature, by scheme.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"scheme"}),
		backlog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "quader_signatures_backlog",
			Help: "Signatures due for a check by priority: 0 upload, 1 fill, 2 subject arrived, 3 discovery and retry (R-SIG-011), after the job's last run.",
		}, []string{"priority"}),
		oldest: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_signatures_oldest_pending_seconds",
			Help: "Age of the oldest signature due for a check, after the job's last run.",
		}),
		invalid: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_signatures_invalid",
			Help: "Invalid signatures with an attachment, after the job's last run.",
		}),
		stale: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_signatures_trust_rows_stale",
			Help: "Trust rows computed under an older anchor epoch, after the job's last run (R-SIG-012).",
		}),
		remote: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_signatures_remote_requests_total",
			Help: "Requests of the background fetch of a remote repository's signatures (R-SIG-026), by format and result.",
		}, []string{"format", "result"}),
		rootAge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_signatures_trust_root_age_seconds",
			Help: "Age of the public Sigstore root in use: now minus its fetch, or minus the release snapshot's build (R-SIG-017).",
		}),
	}
	var err error
	if m.checks, err = register(reg, m.checks); err != nil {
		return nil, err
	}
	if m.duration, err = register(reg, m.duration); err != nil {
		return nil, err
	}
	if m.backlog, err = register(reg, m.backlog); err != nil {
		return nil, err
	}
	if m.oldest, err = register(reg, m.oldest); err != nil {
		return nil, err
	}
	if m.invalid, err = register(reg, m.invalid); err != nil {
		return nil, err
	}
	if m.stale, err = register(reg, m.stale); err != nil {
		return nil, err
	}
	if m.remote, err = register(reg, m.remote); err != nil {
		return nil, err
	}
	if m.rootAge, err = register(reg, m.rootAge); err != nil {
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

// RemoteRequest counts one request of the background fetch of a remote's signatures (R-SIG-026): the format of the remote and the
// result: "error" when the request returned an error (a transport failure, a rate-limit answer of the upstream), "ok" for every
// other answer, one that says the upstream has no such object included. A request the hourly budget of R-SIG-040 holds back is never
// sent and not counted, nor is a document read from the store (the type limited in internal/signatures/fetch).
func (m *Metrics) RemoteRequest(format, result string) {
	if m != nil {
		m.remote.WithLabelValues(format, result).Inc()
	}
}

func (m *Metrics) check(scheme, result string, d time.Duration) {
	if m != nil {
		m.checks.WithLabelValues(scheme, result).Inc()
		m.duration.WithLabelValues(scheme).Observe(d.Seconds())
	}
}

// gauges is what the gauges show after a run.
type gauges struct {
	due     [4]int64
	oldest  float64
	invalid int64
	stale   int64
	rootAge time.Duration
}

func (m *Metrics) set(g gauges) {
	if m == nil {
		return
	}
	for p, n := range g.due {
		m.backlog.WithLabelValues(strconv.Itoa(p)).Set(float64(n))
	}
	m.oldest.Set(g.oldest)
	m.invalid.Set(float64(g.invalid))
	m.stale.Set(float64(g.stale))
	m.rootAge.Set(g.rootAge.Seconds())
}
