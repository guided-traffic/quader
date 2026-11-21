package scanner

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics are the series of the role scanner (R-VULN-024): the analyses and the detections by result, the time of their steps, and the
// slots that hold a call. A nil *Metrics records nothing.
type Metrics struct {
	analyses   *prometheus.CounterVec
	detections *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	busy       prometheus.Gauge
}

// The steps of a call, the values of the label step: the pull and the analyser's run of an analysis, the analyser's run of a detection.
const (
	StepPull    = "pull"
	StepAnalyse = "analyse"
	StepDetect  = "detect"
)

// The results of a call that are no code of R-VULN-034.
const (
	// ResultAnalysed is a call that ended in an analysis.
	ResultAnalysed = "analysed"
	// ResultDetected is a detection that ended in a report.
	ResultDetected = "detected"
	// ResultCancelled is a call whose worker went away before the answer.
	ResultCancelled = "cancelled"
)

// NewMetrics registers the families on reg; a registry that holds them already hands them back.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		analyses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_scanner_analyses_total",
			Help: "Analyses of the scanner by result: analysed, or the code the call answered (R-VULN-034).",
		}, []string{"result"}),
		detections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_scanner_detections_total",
			Help: "Detections of the scanner by result: detected, or the code the call answered, analysis_outdated and database_loading among them (R-VULN-048).",
		}, []string{"result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "quader_scanner_analysis_duration_seconds",
			Help:    "Duration of the steps of a call: the pull through the scan listener and the analyser's run of an analysis, the analyser's run of a detection.",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 900},
		}, []string{"step"}),
		busy: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_scanner_slots_busy",
			Help: "Slots of the scanner that hold an analysis or a detection (vulnerabilities.images.scanner.slots).",
		}),
	}
	var err error
	if m.analyses, err = register(reg, m.analyses); err != nil {
		return nil, err
	}
	if m.detections, err = register(reg, m.detections); err != nil {
		return nil, err
	}
	if m.duration, err = register(reg, m.duration); err != nil {
		return nil, err
	}
	if m.busy, err = register(reg, m.busy); err != nil {
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

func (m *Metrics) result(r string) {
	if m != nil {
		m.analyses.WithLabelValues(r).Inc()
	}
}

func (m *Metrics) detection(r string) {
	if m != nil {
		m.detections.WithLabelValues(r).Inc()
	}
}

func (m *Metrics) step(step string, seconds float64) {
	if m != nil {
		m.duration.WithLabelValues(step).Observe(seconds)
	}
}

func (m *Metrics) slots(n int) {
	if m != nil {
		m.busy.Set(float64(n))
	}
}
