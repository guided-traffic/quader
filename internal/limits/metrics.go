package limits

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics are the families of R-LIM-030. They carry no subject (R-OPS-007): the figure, the mode, the outcome, the byte class
// and the subject kind only.
type Metrics struct {
	decisions *prometheus.CounterVec
	bytesC    *prometheus.CounterVec
	upstreamC *prometheus.CounterVec
	blocks    *prometheus.CounterVec
	mode      prometheus.Gauge
	// flushes counts the rounds of the charge buffer that ended (Q-LIM-112): what a test waits for instead of a time.
	flushes prometheus.Counter
}

// NewMetrics creates the families and registers them on reg; a nil reg registers nothing.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		decisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_limits_decisions_total",
			Help: "Decisions of the download limits by figure, mode and outcome (admitted, limited, would_limit, paced).",
		}, []string{"figure", "mode", "outcome"}),
		bytesC: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_limits_bytes_total",
			Help: "Bytes counted by the download limits, by class (cached, uncached) and subject kind.",
		}, []string{"class", "kind"}),
		upstreamC: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_limits_upstream_requests_total",
			Help: "Upstream requests counted at step 9a, by subject kind.",
		}, []string{"kind"}),
		blocks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_limits_blocks_started_total",
			Help: "Blocks set after refused requests, by subject kind.",
		}, []string{"kind"}),
		mode: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_limits_mode",
			Help: "1 while the download limits are in enforce, 0 in audit.",
		}),
		flushes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "quader_limits_flushes_total",
			Help: "Rounds of the replica's charge buffer that ended: the bytes and requests counted before a round began are in Valkey, or on the replica's share while Valkey cannot be used, once it ends.",
		}),
	}
	if reg != nil {
		for _, c := range []prometheus.Collector{m.decisions, m.bytesC, m.upstreamC, m.blocks, m.mode, m.flushes} {
			_ = reg.Register(c)
		}
	}
	return m
}

func (m *Metrics) decision(figure string, mode Mode, outcome string) {
	if m != nil {
		m.decisions.WithLabelValues(figure, string(mode), outcome).Inc()
	}
}

func (m *Metrics) bytes(kind SubjectKind, cached, uncached int64) {
	if m == nil {
		return
	}
	if cached > 0 {
		m.bytesC.WithLabelValues("cached", kind.APIName()).Add(float64(cached))
	}
	if uncached > 0 {
		m.bytesC.WithLabelValues("uncached", kind.APIName()).Add(float64(uncached))
	}
}

func (m *Metrics) upstream(kind SubjectKind) {
	if m != nil {
		m.upstreamC.WithLabelValues(kind.APIName()).Inc()
	}
}

func (m *Metrics) flushed() {
	if m != nil {
		m.flushes.Inc()
	}
}

func (m *Metrics) blockStarted(kind SubjectKind) {
	if m != nil {
		m.blocks.WithLabelValues(kind.APIName()).Inc()
	}
}

// SetMode sets quader_limits_mode.
func (m *Metrics) SetMode(mode Mode) {
	if m == nil {
		return
	}
	v := 0.0
	if mode == ModeEnforce {
		v = 1
	}
	m.mode.Set(v)
}
