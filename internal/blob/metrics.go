package blob

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/observability"
)

// Metrics are the Prometheus families of the blob store (R-STOR-004,
// R-STOR-024, R-STOR-033, R-STOR-055, R-STOR-060). Build it once per process with NewMetrics and hand
// it to every Spool, Guard and Collector; a nil *Metrics records nothing, which
// is what unit tests use. The object-store families carry the label connection, the id of a configured connection
// (at most eight, R-OPS-007).
type Metrics struct {
	rejections *prometheus.CounterVec
	spool      *SpoolMetrics

	s3Requests    *prometheus.CounterVec
	s3Duration    *prometheus.HistogramVec
	s3Bytes       *prometheus.CounterVec
	s3CircuitOpen *prometheus.GaugeVec

	gcDeleted   prometheus.Counter
	gcReclaimed prometheus.Counter
	gcKept      prometheus.Counter
	gcHeld      prometheus.Counter
	gcDeleting  prometheus.Gauge
	gcLastOK    *prometheus.GaugeVec // no series until a run succeeded
	gcReleased  *prometheus.CounterVec

	damaged            *prometheus.CounterVec
	copiesDamaged      *prometheus.CounterVec
	readsFallenOver    *prometheus.CounterVec
	sealedFailures     *prometheus.CounterVec
	writesFallenOver   *prometheus.CounterVec
	uploadsUnavailable prometheus.Counter
	readsUnavailable   *prometheus.CounterVec

	uploadSessions *prometheus.CounterVec

	// history receives what the reads and writes absorbed beside the counters (R-STOR-110): the replica's recorder of the history
	// (internal/blob/history), nil until SetHistory.
	history atomic.Pointer[HistorySink]
}

// HistorySink is what the history of the stored content counts per hour (storage.md R-STOR-110, Q-STOR-513): kind is one of the
// History… kinds, reason the reason of the matching metric, bytes the bytes concerned (0 when not known). It is called in the request
// path and must not block.
type HistorySink interface {
	Count(conn, kind, reason string, bytes int64)
}

// The kinds of storage_counts that the blob store counts (R-STOR-110).
const (
	HistoryReadFallOver         = "read_fallover"          // a read left a copy for the next (quader_storage_reads_fallen_over_total)
	HistoryReadUnavailable      = "read_unavailable"       // no copy could serve a read (quader_storage_reads_unavailable_total)
	HistoryWriteFallOver        = "write_fallover"         // the next connection took a write (quader_storage_writes_fallen_over_total)
	HistoryUploadRefused        = "upload_refused"         // fewer than min_copies copies were written (quader_storage_uploads_unavailable_total)
	HistoryCopyFailed           = "copy_failed"            // the reconciliation could not make a copy (quader_storage_reconcile_failures_total)
	HistoryEncryptedReadRefused = "encrypted_read_refused" // a check of the stored format refused a read (quader_storage_encrypted_read_failures_total)
)

// SetHistory makes the recorder of the history count what these metrics count (R-STOR-110). A nil *Metrics records nothing.
func (m *Metrics) SetHistory(h HistorySink) {
	if m != nil && h != nil {
		m.history.Store(&h)
	}
}

// count hands one event to the history's recorder, when there is one.
func (m *Metrics) count(conn, kind, reason string, bytes int64) {
	if m == nil {
		return
	}
	if h := m.history.Load(); h != nil {
		(*h).Count(conn, kind, reason, bytes)
	}
}

// NewMetrics registers the families on m. It panics, like every registration
// in internal/observability, when called twice for one registry.
func NewMetrics(m *observability.Metrics) *Metrics {
	return &Metrics{
		rejections: m.NewCounterVec("quader_storage_upload_rejections_total", "Uploads refused before or while reading the body, by reason.", "reason"),
		spool:      NewSpoolMetrics(m),

		s3Requests:    m.NewCounterVec("quader_storage_s3_requests_total", "Object-store requests by connection, operation and outcome.", "connection", "operation", "outcome"),
		s3Duration:    m.NewHistogramVec("quader_storage_s3_request_duration_seconds", "Duration of object-store requests by connection and operation.", observability.HTTPBuckets(), "connection", "operation"),
		s3Bytes:       m.NewCounterVec("quader_storage_s3_bytes_total", "Bytes sent to and received from the object store, by connection and direction.", "connection", "direction"),
		s3CircuitOpen: m.NewGaugeVec("quader_storage_s3_circuit_open", "1 while the circuit breaker of the connection is open on this replica (R-STOR-060).", "connection"),

		gcDeleted:   m.NewCounterVec("quader_gc_deleted_blobs_total", "Blobs whose object garbage collection deleted.").WithLabelValues(),
		gcReclaimed: m.NewCounterVec("quader_gc_reclaimed_bytes_total", "Bytes garbage collection reclaimed.").WithLabelValues(),
		gcKept:      m.NewCounterVec("quader_gc_candidates_kept_total", "Candidates the per-blob check found referenced or gone.").WithLabelValues(),
		gcHeld:      m.NewCounterVec("quader_gc_candidates_held_total", "Candidates kept by a backup hold.").WithLabelValues(),
		gcDeleting:  m.NewGaugeVec("quader_gc_deleting_rows", "Blob rows in state deleting at the end of the last run.").WithLabelValues(),
		gcLastOK:    m.NewGaugeVec("quader_gc_last_success_timestamp_seconds", "End of the last garbage-collection run of this replica that was neither a dry run, nor paused, nor failed; absent until one has ended."),
		gcReleased:  m.NewCounterVec("quader_gc_copies_released_total", "Copies whose delete collection never sent, left to the orphan sweep of their connection, by connection (R-STOR-057).", "connection"),

		damaged:         m.NewCounterVec("quader_storage_blobs_damaged_total", "Blobs marked damaged by a detection, by new status.", "status"),
		copiesDamaged:   m.NewCounterVec("quader_storage_copies_damaged_total", "Copies of blobs marked damaged by a detection, by connection and new status (R-STOR-055).", "connection", "status"),
		readsFallenOver: m.NewCounterVec("quader_storage_reads_fallen_over_total", "Reads that left a copy for the next before their first byte, by connection and reason (R-STOR-055).", "connection", "reason"),
		sealedFailures:  m.NewCounterVec("quader_storage_encrypted_read_failures_total", "Reads of a stored object refused by a check of the stored format, by connection and the step's reason (R-STOR-082).", "connection", "reason"),
		writesFallenOver: m.NewCounterVec("quader_storage_writes_fallen_over_total",
			"Writes of an upload that failed on a connection and were taken by the next writable one, by the connection that failed and the failure's code (R-STOR-053, R-STOR-110).", "connection", "reason"),
		uploadsUnavailable: m.NewCounterVec("quader_storage_uploads_unavailable_total",
			"Uploads refused with 503 because fewer than storage.replication.min_copies copies could be written (R-STOR-053, R-STOR-110).").WithLabelValues(),
		readsUnavailable: m.NewCounterVec("quader_storage_reads_unavailable_total",
			"Reads that no copy could serve, answered 503 content_unavailable, by the connection of the last copy tried (R-STOR-055, R-STOR-110).", "connection"),

		uploadSessions: m.NewCounterVec("quader_storage_upload_sessions_total", "Multi-request upload sessions by how they ended: completed, aborted or expired (R-STOR-011).", "outcome"),
	}
}

// The outcomes of an upload session (UploadSession).
const (
	SessionCompleted = "completed"
	SessionAborted   = "aborted"
	SessionExpired   = "expired"
)

// UploadSessionCounter is the counter of one outcome, for a test that reads it.
func (m *Metrics) UploadSessionCounter(outcome string) prometheus.Counter {
	return m.uploadSessions.WithLabelValues(outcome)
}

// UploadSession counts n upload sessions that ended with the outcome (Q-STOR-173). A nil *Metrics records nothing.
func (m *Metrics) UploadSession(outcome string, n int) {
	if m != nil && n > 0 {
		m.uploadSessions.WithLabelValues(outcome).Add(float64(n))
	}
}

func (m *Metrics) reject(reason string) {
	if m != nil {
		m.rejections.WithLabelValues(reason).Inc()
	}
}

// SpoolMetrics are the two gauges of a spool's room (R-STOR-004, operations-and-observability.md Q-OPS-237): the budget held on it and
// the free space of its volume. [NewMetrics] builds them for the spool of the blob store; a process without a blob store builds them
// alone for its spool (the image scanner, vuln-scanner-trivy), so QuaderSpoolNearlyFull reads every spool by the same two names. A nil
// *SpoolMetrics records nothing.
type SpoolMetrics struct {
	used, free prometheus.Gauge
}

// NewSpoolMetrics registers quader_spool_used_bytes and quader_spool_free_bytes on m.
func NewSpoolMetrics(m *observability.Metrics) *SpoolMetrics {
	return &SpoolMetrics{
		used: m.NewGaugeVec("quader_spool_used_bytes", "Spool budget held on the replica's spool: the bytes running uploads, or the image scans of a scanner, have reserved on the spool volume.").WithLabelValues(),
		free: m.NewGaugeVec("quader_spool_free_bytes", "Free bytes of the spool volume, read at the start, at the last reservation and at the last release.").WithLabelValues(),
	}
}

func (m *SpoolMetrics) set(used int64, free uint64) {
	if m != nil {
		m.used.Set(float64(used))
		m.free.Set(float64(free))
	}
}

func (m *SpoolMetrics) setUsed(used int64) {
	if m != nil {
		m.used.Set(float64(used))
	}
}

// spoolMetrics is the gauges of the blob store's spool, nil for a nil *Metrics.
func (m *Metrics) spoolMetrics() *SpoolMetrics {
	if m == nil {
		return nil
	}
	return m.spool
}

func (m *Metrics) request(conn, op, outcome string, seconds float64) {
	if m != nil {
		m.s3Requests.WithLabelValues(conn, op, outcome).Inc()
		m.s3Duration.WithLabelValues(conn, op).Observe(seconds)
	}
}

func (m *Metrics) bytes(conn, direction string, n int64) {
	if m != nil && n > 0 {
		m.s3Bytes.WithLabelValues(conn, direction).Add(float64(n))
	}
}

func (m *Metrics) circuit(conn string, open bool) {
	if m == nil {
		return
	}
	v := 0.0
	if open {
		v = 1
	}
	m.s3CircuitOpen.WithLabelValues(conn).Set(v)
}

// The reasons of quader_storage_reads_fallen_over_total (R-STOR-055).
const (
	fallOverNotFound    = "not_found"   // the connection answered "no such object"
	fallOverUnavailable = "unavailable" // a transient error or an open circuit
	fallOverSize        = "size"        // a Content-Length other than the copy's stored size
	fallOverRecord      = "record"      // a check of the stored format refused the object before its first byte (R-STOR-082)
)

// encryptedReadFailure counts a read refused by a check of the stored format, by its reason (R-STOR-082), and in the history.
func (m *Metrics) encryptedReadFailure(conn, reason string) {
	if m != nil {
		m.sealedFailures.WithLabelValues(conn, reason).Inc()
		m.count(conn, HistoryEncryptedReadRefused, reason, 0)
	}
}

// writeFellOver counts a write that failed on conn with code and was taken by the next writable connection (R-STOR-053).
func (m *Metrics) writeFellOver(conn, code string) {
	if m != nil {
		m.writesFallenOver.WithLabelValues(conn, code).Inc()
		m.count(conn, HistoryWriteFallOver, code, 0)
	}
}

// uploadUnavailable counts an upload of bytes refused because fewer than min_copies copies were written; the history names the
// connection whose failure ended it and the failure's code (R-STOR-110).
func (m *Metrics) uploadUnavailable(conn, code string, bytes int64) {
	if m != nil {
		m.uploadsUnavailable.Inc()
		m.count(conn, HistoryUploadRefused, code, bytes)
	}
}

// readUnavailable counts a read of bytes that no copy could serve, on the connection of the last copy tried (R-STOR-110).
func (m *Metrics) readUnavailable(conn, code string, bytes int64) {
	if m != nil {
		m.readsUnavailable.WithLabelValues(conn).Inc()
		m.count(conn, HistoryReadUnavailable, code, bytes)
	}
}

// copyDamaged counts a copy marked missing or corrupt (R-STOR-055, R-STOR-027).
func (m *Metrics) copyDamaged(conn, status string) {
	if m != nil {
		m.copiesDamaged.WithLabelValues(conn, status).Inc()
	}
}

// fellOver counts a read that left a copy for the next (R-STOR-055), and in the history.
func (m *Metrics) fellOver(conn, reason string) {
	if m != nil {
		m.readsFallenOver.WithLabelValues(conn, reason).Inc()
		m.count(conn, HistoryReadFallOver, reason, 0)
	}
}

func (m *Metrics) gc(deleted, kept, held int, reclaimed int64) {
	if m == nil {
		return
	}
	m.gcDeleted.Add(float64(deleted))
	m.gcKept.Add(float64(kept))
	m.gcHeld.Add(float64(held))
	m.gcReclaimed.Add(float64(reclaimed))
}

// gcSucceeded records the end of a run that did its work (R-OPS-007).
func (m *Metrics) gcSucceeded(at time.Time) {
	if m != nil {
		m.gcLastOK.WithLabelValues().Set(float64(at.UnixNano()) / 1e9)
	}
}

// copiesReleased counts the copies of one connection that collection released (R-STOR-057).
func (m *Metrics) copiesReleased(conn string, n int) {
	if m != nil && n > 0 {
		m.gcReleased.WithLabelValues(conn).Add(float64(n))
	}
}

func (m *Metrics) setDeleting(n int) {
	if m != nil {
		m.gcDeleting.Set(float64(n))
	}
}

func (m *Metrics) damage(status string) {
	if m != nil {
		m.damaged.WithLabelValues(status).Inc()
	}
}
