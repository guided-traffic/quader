package job

import (
	"errors"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/store"
)

// Metrics are the worker's series of the signing (R-SIG-057): the end of the current and the announced identity's certificate, whether
// this worker holds the current identity's key, the operations by kind and result, and the work per kind and priority with the oldest
// due row. A nil *Metrics records nothing.
type Metrics struct {
	notAfter   *prometheus.GaugeVec
	loaded     prometheus.Gauge
	operations *prometheus.CounterVec
	backlog    *prometheus.GaugeVec
	oldest     *prometheus.GaugeVec
}

// NewMetrics registers the families on reg; a registry that holds them already hands them back.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		notAfter: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "quader_signing_identity_not_after_seconds",
			Help: "The Unix time of the end of the certificate of the current and of the announced signing identity (R-SIG-057); absent for a state without one.",
		}, []string{"state"}),
		loaded: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quader_signing_identity_loaded",
			Help: "1 when this worker holds the key of the current signing identity, else 0 (R-SIG-057).",
		}),
		operations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quader_signing_operations_total",
			Help: "Signing operations of the job signing by kind (npm, attestation) and result (signed, failed) (R-SIG-057).",
		}, []string{"kind", "result"}),
		backlog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "quader_signing_backlog",
			Help: "Work of the job signing that waits or is due, by kind and priority (R-SIG-049), after the last look.",
		}, []string{"kind", "priority"}),
		oldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "quader_signing_oldest_due_seconds",
			Help: "Age of the oldest due work of the job signing by kind, after the last look; 0 when nothing is due.",
		}, []string{"kind"}),
	}
	var err error
	if m.notAfter, err = register(reg, m.notAfter); err != nil {
		return nil, err
	}
	if m.loaded, err = register(reg, m.loaded); err != nil {
		return nil, err
	}
	if m.operations, err = register(reg, m.operations); err != nil {
		return nil, err
	}
	if m.backlog, err = register(reg, m.backlog); err != nil {
		return nil, err
	}
	if m.oldest, err = register(reg, m.oldest); err != nil {
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

func (m *Metrics) operation(kind, result string) {
	if m != nil {
		m.operations.WithLabelValues(kind, result).Inc()
	}
}

// identities sets the certificate ends of the current and the announced identity and whether this worker holds the current key.
func (m *Metrics) identities(ids []signing.Identity, loaded bool) {
	if m == nil {
		return
	}
	m.notAfter.Reset()
	for _, i := range ids {
		if (i.State == signing.StateCurrent || i.State == signing.StateAnnounced) && !i.NotAfter.IsZero() {
			m.notAfter.WithLabelValues(i.State).Set(float64(i.NotAfter.Unix()))
		}
	}
	if loaded {
		m.loaded.Set(1)
	} else {
		m.loaded.Set(0)
	}
}

// work sets the backlog per kind and priority and the oldest due row per kind; every kind of the table is set, 0 for none.
func (m *Metrics) work(rows []store.Backlog, now time.Time) {
	if m == nil {
		return
	}
	type key struct {
		kind string
		prio int
	}
	counts := map[key]int64{}
	oldest := map[string]float64{}
	for _, k := range []string{signing.WorkNPM, signing.WorkAttestation} {
		oldest[k] = 0
		for p := signing.PriorityPublish; p <= signing.PriorityDiscovery; p++ {
			counts[key{k, p}] = 0
		}
	}
	for _, r := range rows {
		if r.Failed {
			continue
		}
		counts[key{r.Kind, r.Priority}] += r.Rows
		if !r.OldestDue.IsZero() {
			if age := now.Sub(r.OldestDue).Seconds(); age > oldest[r.Kind] {
				oldest[r.Kind] = age
			}
		}
	}
	for k, n := range counts {
		m.backlog.WithLabelValues(k.kind, strconv.Itoa(k.prio)).Set(float64(n))
	}
	for k, age := range oldest {
		m.oldest.WithLabelValues(k).Set(age)
	}
}
