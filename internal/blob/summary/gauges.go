package summary

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/observability"
)

// The gauges of the storage summary (operations-and-observability.md R-OPS-008, storage.md R-STOR-056, R-STOR-060): every replica with
// the main pool reads the latest snapshot in the background (observability.DBGauges) and exports the same values, so a rule aggregates
// them with max. Before the first computation none of them has a series.
const (
	MetricSummaryAge   = "quader_storage_summary_age_seconds"
	MetricBlobsDamaged = "quader_storage_blobs_damaged"
	MetricCopies       = "quader_storage_copies"
	MetricBacklogBlobs = "quader_storage_connection_backlog_blobs"
	MetricBacklogBytes = "quader_storage_connection_backlog_bytes"
	MetricFillRatio    = "quader_storage_connection_fill_ratio"
)

// Gauges are the descriptors of the summary's gauges.
type Gauges struct {
	age, damaged, copies, backlogBlobs, backlogBytes, fill *prometheus.Desc
}

// NewGauges returns the descriptors.
func NewGauges() Gauges {
	conn := []string{"connection"}
	return Gauges{
		age:     prometheus.NewDesc(MetricSummaryAge, "Age in seconds of the latest storage summary (R-OPS-023).", nil, nil),
		damaged: prometheus.NewDesc(MetricBlobsDamaged, "Live blobs no connection holds whole, by content state, from the latest storage summary (R-STOR-028).", []string{"status"}, nil),
		copies: prometheus.NewDesc(MetricCopies,
			"Copies of live blobs by connection and state (present, missing, corrupt), from the latest storage summary (aggregate with max; R-STOR-060).",
			[]string{"connection", "state"}, nil),
		backlogBlobs: prometheus.NewDesc(MetricBacklogBlobs, "Live blobs without a present copy on the connection, from the latest storage summary (R-STOR-056).", conn, nil),
		backlogBytes: prometheus.NewDesc(MetricBacklogBytes, "Bytes of the live blobs without a present copy on the connection, from the latest storage summary.", conn, nil),
		fill:         prometheus.NewDesc(MetricFillRatio, "Present copies on the connection over the live blobs, from the latest storage summary (1 with no blob).", conn, nil),
	}
}

// Descs lists the descriptors.
func (g Gauges) Descs() []*prometheus.Desc {
	return []*prometheus.Desc{g.age, g.damaged, g.copies, g.backlogBlobs, g.backlogBytes, g.fill}
}

// Metrics are the gauges' values of one snapshot at the time now.
func (g Gauges) Metrics(s Summary, now time.Time) []prometheus.Metric {
	out := []prometheus.Metric{
		prometheus.MustNewConstMetric(g.age, prometheus.GaugeValue, max(now.Sub(s.ComputedAt).Seconds(), 0)),
		prometheus.MustNewConstMetric(g.damaged, prometheus.GaugeValue, float64(s.Damaged.Missing), "missing"),
		prometheus.MustNewConstMetric(g.damaged, prometheus.GaugeValue, float64(s.Damaged.Corrupt), "corrupt"),
	}
	for _, c := range s.Connections {
		out = append(out,
			prometheus.MustNewConstMetric(g.copies, prometheus.GaugeValue, float64(c.PresentCopies), c.ID, "present"),
			prometheus.MustNewConstMetric(g.copies, prometheus.GaugeValue, float64(c.MissingCopies), c.ID, "missing"),
			prometheus.MustNewConstMetric(g.copies, prometheus.GaugeValue, float64(c.CorruptCopies), c.ID, "corrupt"),
			prometheus.MustNewConstMetric(g.backlogBlobs, prometheus.GaugeValue, float64(c.BacklogBlobs), c.ID),
			prometheus.MustNewConstMetric(g.backlogBytes, prometheus.GaugeValue, float64(c.BacklogBytes), c.ID),
			prometheus.MustNewConstMetric(g.fill, prometheus.GaugeValue, c.FillRatio, c.ID))
	}
	return out
}

// GaugeSource is the source of the summary's gauges for observability.DBGauges: one read of the snapshot's row per evaluation.
func GaugeSource(db func() *postgres.DB) observability.GaugeSource {
	g := NewGauges()
	return observability.GaugeSource{
		Name: "storage-summary", Descs: g.Descs(),
		Collect: func(ctx context.Context) ([]prometheus.Metric, error) {
			d := db()
			if d == nil {
				return nil, nil
			}
			s, err := Latest(ctx, d)
			if errors.Is(err, ErrNone) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return g.Metrics(s, time.Now()), nil
		},
	}
}
