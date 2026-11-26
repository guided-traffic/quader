package bastionmain

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// The metric names of the main installation's side (R-BAS-008, INV-16). The bastion label is the registration's
// name, bounded by bastions.max_registrations. The Bastion's own metrics are internal/bastion/metrics.
const (
	MetricInterfaceRequests = "quader_bastion_interface_requests_total"
	MetricForwardedRequests = "quader_bastion_forwarded_requests_total"
	MetricForwardedBytes    = "quader_bastion_forwarded_bytes_total"
)

// The class label of quader_bastion_forwarded_bytes_total: on whose behalf the Bastion forwarded the request, the classes of the
// Bastion's budget (R-BAS-025, R-BAS-090). It is the caller's, not the marking's class of quader_bastion_forwarded_requests_total.
const (
	CallerAnonymous     = "anonymous"
	CallerAuthenticated = "authenticated"
)

// The outcome label of quader_bastion_interface_requests_total.
const (
	OutcomeOK        = "ok"
	OutcomeInvalid   = "invalid_token"
	OutcomeMismatch  = "interface_mismatch"
	OutcomeRefused   = "refused"
	OutcomeError     = "error"
	OutcomeNotServed = "not_implemented"
)

// Metrics are the counters of the main installation's side of the Bastion. Work package 2 increments
// Interface for every operation of the interface; work package 3 increments Forwarded for every recognized
// forwarded request with the class of the marking (iface.Class, or "probe" for a write probe), and the marking stage adds
// the body bytes of every recognized forward to ForwardedBytes (R-BAS-090).
type Metrics struct {
	// Interface counts the interface requests by registration, operation (iface.Operation*) and outcome.
	Interface *prometheus.CounterVec
	// Forwarded counts the forwarded requests by registration and class.
	Forwarded *prometheus.CounterVec
	// ForwardedBytes counts the body bytes written to forwarded requests by registration and the caller's class
	// (CallerAnonymous, CallerAuthenticated).
	ForwardedBytes *prometheus.CounterVec
}

// NewMetrics creates the metrics and registers them on reg; a nil reg creates them unregistered.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		Interface: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricInterfaceRequests,
			Help: "Requests of the Bastion interface by registration, operation (handshake, check, feed, downloads) and outcome (R-BAS-008).",
		}, []string{"bastion", "operation", "outcome"}),
		Forwarded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricForwardedRequests,
			Help: "Forwarded requests of Bastions by registration and class of the marking (R-BAS-008, R-BAS-013).",
		}, []string{"bastion", "class"}),
		ForwardedBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricForwardedBytes,
			Help: "Body bytes the main installation wrote to forwarded requests of Bastions, by registration and the caller's class (anonymous, authenticated) (R-BAS-090).",
		}, []string{"bastion", "class"}),
	}
	if reg == nil {
		return m, nil
	}
	for _, c := range []prometheus.Collector{m.Interface, m.Forwarded, m.ForwardedBytes} {
		if err := reg.Register(c); err != nil {
			var are prometheus.AlreadyRegisteredError
			if errors.As(err, &are) {
				return nil, errors.New("bastionmain: metrics are already registered on this registerer")
			}
			return nil, err
		}
	}
	return m, nil
}
