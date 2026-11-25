package queue

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/remotecuration"
)

// The metrics of the download queue (R-CUR-019). The label repo is a repository's qualified name, capped as R-OPS-007 caps it.
const (
	// MetricDownloads counts the ends of queue runs (completed, bounded, refused, skipped, failed, cancelled) and the decisions on
	// probes (off, stored, active, queue_full, limited, busy, not_admitted, local, started, failed).
	MetricDownloads = "quader_remote_downloads_total"
	// MetricRows are the rows by state, set by the dispatch task every minute.
	MetricRows = "quader_remote_downloads"
	// MetricFailures counts failed attempts by class.
	MetricFailures = "quader_remote_download_failures_total"
	// MetricSyncRuns counts the looks at due syncs by outcome (started, overlapped, limited, bounded, disabled, paused; R-CUR-027).
	MetricSyncRuns = "quader_remote_download_sync_runs_total"
	// MetricSyncs are the syncs by state (active, paused), set by the dispatch task every minute.
	MetricSyncs = "quader_remote_download_syncs"
)

// Metrics are the families of the queue. A nil *Metrics counts nothing.
type Metrics struct {
	Downloads *prometheus.CounterVec
	Rows      *prometheus.GaugeVec
	Failures  *prometheus.CounterVec
	SyncRuns  *prometheus.CounterVec
	Syncs     *prometheus.GaugeVec
	// label caps a repository's name (observability.CappedLabels.Value); nil keeps it.
	label func(family, repo string) string
	// name is a repository's qualified name by its id, for the gauge; nil names it by its id.
	name func(id int64) string
}

// NewMetrics registers the families in reg; a nil registerer keeps them unregistered. label caps the repo label; name reads a
// repository's qualified name by its id.
func NewMetrics(reg prometheus.Registerer, label func(family, repo string) string, name func(id int64) string) (*Metrics, error) {
	m := &Metrics{
		Downloads: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricDownloads,
			Help: "Ends of the download queue's runs and the decisions on probes of remote repositories, by repository, origin and outcome (R-CUR-019).",
		}, []string{"repo", "origin", "outcome"}),
		Rows: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: MetricRows,
			Help: "Rows of the download queues by repository and state, read by the task remote-downloads-dispatch (R-CUR-019).",
		}, []string{"repo", "state"}),
		Failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricFailures,
			Help: "Failed attempts of the download queue's rows by repository and failure class (R-CUR-019).",
		}, []string{"repo", "class"}),
		SyncRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricSyncRuns,
			Help: "Looks of the task remote-downloads-dispatch and of a person's Run now at the syncs of remote repositories, by repository and outcome (R-CUR-019, R-CUR-027).",
		}, []string{"repo", "outcome"}),
		Syncs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: MetricSyncs,
			Help: "Syncs of the remote repositories by repository and state, active or paused, read by the task remote-downloads-dispatch (R-CUR-019, R-CUR-027).",
		}, []string{"repo", "state"}),
		label: label, name: name,
	}
	if reg != nil {
		for _, c := range []prometheus.Collector{m.Downloads, m.Rows, m.Failures, m.SyncRuns, m.Syncs} {
			if err := reg.Register(c); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

func (m *Metrics) repo(family, repo string) string {
	if m.label == nil {
		return repo
	}
	return m.label(family, repo)
}

// Decision counts the decision on a probe (R-CUR-013).
func (m *Metrics) Decision(repo, outcome string) {
	if m != nil {
		m.Downloads.WithLabelValues(m.repo(MetricDownloads, repo), string(remotecuration.OriginProbe), outcome).Inc()
	}
}

func (m *Metrics) ended(repo string, origin remotecuration.Origin, outcome string) {
	if m != nil {
		m.Downloads.WithLabelValues(m.repo(MetricDownloads, repo), string(origin), outcome).Inc()
	}
}

func (m *Metrics) failed(repo string, class remotecuration.FailureClass) {
	if m != nil {
		m.Failures.WithLabelValues(m.repo(MetricFailures, repo), string(class)).Inc()
	}
}

// gauge sets the rows by state; a repository that has no rows any more is dropped.
func (m *Metrics) gauge(rows []GaugeRow) { m.setGauge(m.Rows, MetricRows, rows) }

// syncGauge sets the syncs by state.
func (m *Metrics) syncGauge(rows []GaugeRow) { m.setGauge(m.Syncs, MetricSyncs, rows) }

func (m *Metrics) setGauge(g *prometheus.GaugeVec, family string, rows []GaugeRow) {
	g.Reset()
	sum := map[[2]string]float64{}
	for _, r := range rows {
		name := ""
		if m.name != nil {
			name = m.name(r.RepoID)
		}
		if name == "" {
			continue // a repository the snapshot does not hold any more
		}
		sum[[2]string{m.repo(family, name), r.State}] += float64(r.N)
	}
	for k, v := range sum {
		g.WithLabelValues(k[0], k[1]).Set(v)
	}
}

// syncRan counts a look at a sync.
func (m *Metrics) syncRan(repo string, outcome SyncOutcome) {
	if m != nil {
		m.SyncRuns.WithLabelValues(m.repo(MetricSyncRuns, repo), string(outcome)).Inc()
	}
}

// Forget deletes the series of a repository that left the snapshot (R-OPS-007).
func (m *Metrics) Forget(repo string) {
	if m == nil {
		return
	}
	l := prometheus.Labels{"repo": repo}
	m.Downloads.DeletePartialMatch(l)
	m.Rows.DeletePartialMatch(l)
	m.Failures.DeletePartialMatch(l)
	m.SyncRuns.DeletePartialMatch(l)
	m.Syncs.DeletePartialMatch(l)
}
