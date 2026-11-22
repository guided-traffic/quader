package downloadlinks

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/observability"
)

// The reasons a GET of a declaring route was streamed while the switch was on (R-STOR-073): the label reason of
// quader_storage_download_links_declined_total, a closed set (R-OPS-007).
const (
	DeclinedBastion    = "bastion"
	DeclinedOptOut     = "opt_out"
	DeclinedGate       = "gate"
	DeclinedRange      = "range"
	DeclinedSize       = "size"
	DeclinedConnection = "connection"
	DeclinedCredential = "credential"
	DeclinedSigning    = "signing"
)

// DeclinedReasons are the reasons in the order of the serve point's steps.
func DeclinedReasons() []string {
	return []string{
		DeclinedBastion, DeclinedOptOut, DeclinedGate, DeclinedRange, DeclinedSize, DeclinedConnection, DeclinedCredential, DeclinedSigning,
	}
}

// Metrics are the families of R-STOR-073. A nil *Metrics records nothing, which unit tests use.
type Metrics struct {
	issued   *prometheus.CounterVec
	bytes    *prometheus.CounterVec
	declined *prometheus.CounterVec
	usable   *prometheus.GaugeVec
}

// NewMetrics registers the families on m; like every registration it panics when called twice for one registry. The label format
// is bounded by the registered formats, reason by DeclinedReasons and connection by the configured connections (at most eight).
func NewMetrics(m *observability.Metrics) *Metrics {
	return &Metrics{
		issued: m.NewCounterVec("quader_storage_download_links_total",
			"Downloads handed to a signed link of the object store, counted when the link was issued (R-STOR-073).", "format"),
		bytes: m.NewCounterVec("quader_storage_download_link_bytes_total",
			"The sizes of the downloads handed to a signed link of the object store (R-STOR-073).", "format"),
		declined: m.NewCounterVec("quader_storage_download_links_declined_total",
			"GET requests of a route whose clients check a digest that were streamed while the delivery by signed links was on, by the first condition that did not hold (R-STOR-073).", "reason"),
		usable: m.NewGaugeVec("quader_storage_download_links_usable",
			"1 while the connection can serve signed links on this replica (R-STOR-074).", "connection"),
	}
}

// Issued counts a link issued for a download of a format and its size.
func (m *Metrics) Issued(format string, size int64) {
	if m == nil {
		return
	}
	m.issued.WithLabelValues(format).Inc()
	m.bytes.WithLabelValues(format).Add(float64(size))
}

// Declined counts a GET of a declaring route streamed for reason while the switch was on.
func (m *Metrics) Declined(reason string) {
	if m != nil {
		m.declined.WithLabelValues(reason).Inc()
	}
}

func (m *Metrics) setUsable(conn string, ok bool) {
	if m == nil {
		return
	}
	v := 0.0
	if ok {
		v = 1
	}
	m.usable.WithLabelValues(conn).Set(v)
}
