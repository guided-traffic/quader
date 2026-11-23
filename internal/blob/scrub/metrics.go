package scrub

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/observability"
)

// The results of quader_storage_scrub_copies_total (R-STOR-027).
const (
	ResultOK      = "ok"
	ResultCorrupt = "corrupt"
	ResultMissing = "missing"
	ResultError   = "error"
	// ResultUnknownKey is a copy on an encrypted connection whose record names a storage key the replica does not hold: no evidence
	// about the object, so it is not marked and stays due (R-STOR-087, Q-STOR-311).
	ResultUnknownKey = "unknown_key"
)

// The outcomes of quader_storage_scrub_repairs_total (Q-STOR-265).
const (
	RepairDone     = "repaired"
	RepairFailed   = "failed"
	RepairNoSource = "no_source"
)

// Metrics are the scrub's counters (R-STOR-027, R-STOR-060). A nil *Metrics counts nothing.
type Metrics struct {
	copies  *prometheus.CounterVec
	bytes   *prometheus.CounterVec
	repairs *prometheus.CounterVec
}

// NewMetrics registers the scrub's families on m.
func NewMetrics(m *observability.Metrics) *Metrics {
	return &Metrics{
		copies: m.NewCounterVec("quader_storage_scrub_copies_total",
			"Copies the scrub read to their end, by connection and result: ok, corrupt, missing, error or unknown_key (R-STOR-027, R-STOR-087).", "connection", "result"),
		bytes: m.NewCounterVec("quader_storage_scrub_read_bytes_total",
			"Bytes the scrub read from a connection, its repairs' reads included (R-STOR-027).", "connection"),
		repairs: m.NewCounterVec("quader_storage_scrub_repairs_total",
			"Damaged copies the scrub tried to replace from another connection, by connection and outcome: repaired, failed or no_source (R-STOR-027).",
			"connection", "outcome"),
	}
}

func (m *Metrics) copy(conn, result string) {
	if m != nil {
		m.copies.WithLabelValues(conn, result).Inc()
	}
}

func (m *Metrics) read(conn string, n int64) {
	if m != nil && n > 0 {
		m.bytes.WithLabelValues(conn).Add(float64(n))
	}
}

func (m *Metrics) repair(conn, outcome string) {
	if m != nil {
		m.repairs.WithLabelValues(conn, outcome).Inc()
	}
}

// OverdueGrace is how long after its period a copy that was not read whole again counts as overdue (Q-STOR-266): one day, so
// that the copies an hourly run has not reached yet, and a run that a rollout or an outage of the workers skipped, do not count.
const OverdueGrace = 24 * time.Hour

// OverdueGauge is the source of quader_storage_scrub_overdue_copies{connection} (R-STOR-027, ha-and-helm.md R-HA-045
// QuaderStorageScrubBehind), read from PostgreSQL in the background on every replica that has the connections: per connection of
// this replica, the present copies whose last verification, or their write when never verified, lies more than the connection's
// scrub_period and OverdueGrace back.
func OverdueGauge(db *postgres.DB, conns *blob.Connections) observability.GaugeSource {
	desc := prometheus.NewDesc("quader_storage_scrub_overdue_copies",
		"Present copies not read whole within their connection's scrub_period and a day, by connection, from PostgreSQL (aggregate with max).",
		[]string{"connection"}, nil)
	return observability.GaugeSource{
		Name: "storage-scrub", Descs: []*prometheus.Desc{desc},
		Collect: func(ctx context.Context) ([]prometheus.Metric, error) {
			var out []prometheus.Metric
			err := db.Read(ctx, func(c q.DBTX) error {
				out = out[:0]
				for _, conn := range conns.List() {
					n, err := q.New(c).ScrubOverdue(ctx, q.ScrubOverdueParams{
						ConnectionID: conn.ID(), AgeSeconds: (conn.ScrubPeriod() + OverdueGrace).Seconds(),
					})
					if err != nil {
						return err
					}
					out = append(out, prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(n), conn.ID()))
				}
				return nil
			})
			return out, err
		},
	}
}
