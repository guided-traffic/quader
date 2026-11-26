package mainclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
)

// Purpose is the purpose label of the Bastion's outbound client in the shared outbound metrics.
const Purpose = "bastion_main"

const (
	// maxInterfaceBody bounds the body of an interface answer; the largest is a full feed page.
	maxInterfaceBody = 16 << 20
	// problemPeek is how much of a 400 or 401 problem body is read to find its code.
	problemPeek = 8 << 10
	// bodyAllowance is added to the connect and response-header timeouts for the time an interface answer
	// may take to arrive after its headers.
	bodyAllowance = 10 * time.Second
)

// HTTPClient is the [Client] over net/http (R-BAS-006, R-BAS-008, R-BAS-017, R-BAS-021, R-BAS-025, R-BAS-044).
type HTTPClient struct {
	o    Options
	base *url.URL
	// packages is the base of the forwards: bastion.main.packages_url, or base (Q-HA-74).
	packages  *url.URL
	origin    origin
	http      *http.Client
	now       func() time.Time
	opTimeout time.Duration
}

var _ Client = (*HTTPClient)(nil)

// New builds the client. A bastion.main.url that is not https, a base URL that is not an origin, a CA file that
// does not load and a missing token source are errors; the connection itself is not tried.
func New(o Options) (*HTTPClient, error) {
	if o.Metrics == nil || o.Tokens == nil {
		return nil, errors.New("mainclient: Metrics and Tokens are required")
	}
	base, err := url.Parse(o.URL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, fmt.Errorf("mainclient: bastion.main.url must be an https URL (R-BAS-044)")
	}
	org, err := newOrigin(o.BaseURL)
	if err != nil {
		return nil, err
	}
	packages := base
	if o.PackagesURL != "" {
		if packages, err = url.Parse(o.PackagesURL); err != nil || packages.Scheme != "https" || packages.Host == "" {
			return nil, fmt.Errorf("mainclient: bastion.main.packages_url must be an https URL (R-BAS-044)")
		}
	}
	c := &HTTPClient{o: o, base: base, packages: packages, origin: org, now: o.Now, opTimeout: o.ConnectTimeout + o.ResponseHeaderTimeout + bodyAllowance}
	if c.now == nil {
		c.now = time.Now
	}
	if c.http, err = c.factoryClient(); err != nil {
		return nil, err
	}
	if o.WrapTransport != nil {
		c.http.Transport = o.WrapTransport(c.http.Transport)
	}
	// A redirect is the main installation's answer, not something to follow: the Bastion token travels in a
	// header that a redirect would carry to another host.
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c, nil
}

// factoryClient builds the client of the shared outbound factory: TLS 1.2 or later, the system roots plus
// bastion.main.ca_file read again without a restart (R-BAS-044), the trace context of the Bastion's span. The
// egress address policy is empty: the destination is operator configuration, not input (H-446).
func (c *HTTPClient) factoryClient() (*http.Client, error) {
	cfg := config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true, MinVersion: "1.2"}}
	if c.o.CAFile != "" {
		cfg.TLS.CAFiles = []string{c.o.CAFile}
	}
	opts := []outbound.Option{outbound.WithVersion(c.o.Release)}
	if c.o.Tracer != nil {
		opts = append(opts, outbound.WithTracer(c.o.Tracer))
	}
	if c.o.Obs != nil {
		opts = append(opts, outbound.WithMetrics(c.o.Obs))
	}
	if c.o.Log != nil {
		opts = append(opts, outbound.WithLogger(c.o.Log))
	}
	if c.o.TrustCheckEvery > 0 {
		opts = append(opts, outbound.WithTrustCheckEvery(c.o.TrustCheckEvery))
	}
	if c.o.TLSMetrics != nil {
		opts = append(opts, outbound.WithTLSMetrics(c.o.TLSMetrics))
	}
	f := outbound.New(cfg, opts...)
	if err := f.Err(); err != nil {
		return nil, fmt.Errorf("mainclient: bastion.main.ca_file: %w", err)
	}
	return f.Client(Purpose,
		outbound.WithConnectTimeout(c.o.ConnectTimeout),
		outbound.WithResponseHeaderTimeout(c.o.ResponseHeaderTimeout),
		outbound.WithDirect(),
	), nil
}

// Close closes the idle connections.
func (c *HTTPClient) Close() { c.http.CloseIdleConnections() }

// origin is the base URL of the Bastion split into the values of the forwarded headers (R-BAS-017).
type origin struct {
	proto string
	// host is the host without the port, bracketed for IPv6; hostPort is host:port as the base URL spells it
	// (the port only when it is not the default of the scheme).
	host, hostPort string
	port           string
}

func newOrigin(raw string) (origin, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return origin{}, errors.New("mainclient: server.base_url must be an origin (scheme, host, optional port)")
	}
	o := origin{proto: u.Scheme, host: strings.ToLower(u.Hostname()), port: u.Port()}
	if strings.Contains(o.host, ":") {
		o.host = "[" + o.host + "]"
	}
	o.hostPort = o.host
	if o.port != "" {
		o.hostPort += ":" + o.port
	} else if u.Scheme == "https" {
		o.port = "443"
	} else {
		o.port = "80"
	}
	return o, nil
}

// apply writes the forwarded headers of R-BAS-017: exactly one entry in each family, replacing whatever was
// there. addr is the client address; an address that does not parse leaves the for entries out.
func (o origin) apply(h http.Header, addr string) {
	for _, k := range []string{iface.HeaderForwardedFor, iface.HeaderForwardedProto, iface.HeaderForwardedHost, iface.HeaderForwardedPort, iface.HeaderForwarded} {
		h.Del(k)
	}
	h.Set(iface.HeaderForwardedProto, o.proto)
	h.Set(iface.HeaderForwardedHost, o.host)
	h.Set(iface.HeaderForwardedPort, o.port)
	var el []string
	if a, ok := parseClient(addr); ok {
		h.Set(iface.HeaderForwardedFor, a.String())
		forVal := a.String()
		if a.Is6() {
			forVal = `"[` + a.String() + `]"`
		}
		el = append(el, "for="+forVal)
	}
	host := o.hostPort
	if strings.ContainsAny(host, ":[") {
		host = `"` + host + `"`
	}
	el = append(el, "proto="+o.proto, "host="+host)
	h.Set(iface.HeaderForwarded, strings.Join(el, ";"))
}

// parseClient reads the client address the engine determined, with or without a port or brackets.
func parseClient(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if a, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return a.Unmap().WithZone(""), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap().WithZone(""), true
	}
	return netip.Addr{}, false
}

// Handshake implements [Client].
func (c *HTTPClient) Handshake(ctx context.Context) (iface.Handshake, error) {
	var hs iface.Handshake
	contact := http.Header{}
	if c.o.EdgeLimit != nil {
		// The edge figure of the ingress in front of the replica, recorded with its contact (R-BAS-093, Q-BAS-354).
		contact.Set(iface.HeaderEdgeLimit, c.o.EdgeLimit.Header())
	}
	if c.o.CatalogueKept != nil {
		// Whether the replica's copy of the public catalogue survives a restart (R-BAS-111), for the registration's card.
		contact.Set(iface.HeaderCatalogueKept, strconv.FormatBool(*c.o.CatalogueKept))
	}
	if err := c.interfaceCall(ctx, ifaceCall{kind: KindHandshake, method: http.MethodGet, path: iface.PathHandshake, header: contact, out: &hs}); err != nil {
		return iface.Handshake{}, err
	}
	if hs.InterfaceVersion != iface.InterfaceVersion {
		return iface.Handshake{}, &MismatchError{Main: hs.InterfaceVersion}
	}
	return hs, nil
}

// Check implements [Client].
func (c *HTTPClient) Check(ctx context.Context, class Class, req iface.CheckRequest) (iface.CheckAnswer, error) {
	var ans iface.CheckAnswer
	err := c.interfaceCall(ctx, ifaceCall{kind: KindCheck, slot: true, class: class, method: http.MethodPost, path: iface.PathCheck, in: req, out: &ans})
	if err != nil {
		return iface.CheckAnswer{}, err
	}
	return ans, nil
}

// Feed implements [Client].
func (c *HTTPClient) Feed(ctx context.Context, after iface.Cursor, limit int) (iface.FeedAnswer, error) {
	q := url.Values{iface.QueryAfter: {after.String()}}
	if limit > 0 {
		q.Set(iface.QueryLimit, strconv.Itoa(limit))
	}
	var ans iface.FeedAnswer
	if err := c.interfaceCall(ctx, ifaceCall{kind: KindFeed, method: http.MethodGet, path: iface.PathFeed, query: q, out: &ans}); err != nil {
		return iface.FeedAnswer{}, err
	}
	return ans, nil
}

// Downloads implements [Client]. A batch takes a slot of the budget as an authenticated request (R-BAS-025).
func (c *HTTPClient) Downloads(ctx context.Context, req iface.DownloadsRequest) error {
	return c.interfaceCall(ctx, ifaceCall{kind: KindDownloads, slot: true, class: Authenticated, method: http.MethodPost, path: iface.PathDownloads, in: req})
}

// ifaceCall describes one request of the interface.
type ifaceCall struct {
	kind   Kind
	slot   bool
	class  Class
	method string
	path   string
	query  url.Values
	// header are further request headers of the call: the contact headers of the handshake.
	header http.Header
	in     any
	// out is the body to decode on a 200; nil means any 2xx is success and the body is ignored.
	out any
	// catalogue marks a read of the public catalogue (R-BAS-109): its body is paced in the throughput's catalogue class, and the
	// call is bounded by the caller's context alone, since a paced batch of megabytes may take longer than an answer of the
	// interface does. read, when set, reads a 200's body in place of out.
	catalogue bool
	read      func(*http.Response) error
}

func (c *HTTPClient) interfaceCall(ctx context.Context, call ifaceCall) error {
	if !call.catalogue {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opTimeout)
		defer cancel()
	}
	if call.slot {
		release, err := c.acquire(ctx, call.class, call.kind)
		if err != nil {
			return err
		}
		defer release()
	}
	var body io.Reader
	if call.in != nil {
		b, err := iface.Marshal(call.in)
		if err != nil {
			return fmt.Errorf("mainclient: encoding the %s request: %w", call.kind, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, call.method, c.base.String(), body)
	if err != nil {
		return fmt.Errorf("mainclient: building the %s request: %w", call.kind, err)
	}
	req.URL.Path, req.URL.RawQuery = call.path, call.query.Encode()
	c.commonHeaders(req.Header)
	tok := c.o.Tokens.Token()
	if tok.Value == "" {
		return errors.New("mainclient: no Bastion token in use")
	}
	req.Header.Set("Authorization", iface.BearerScheme+" "+tok.Value)
	req.Header.Set("Accept", iface.MediaType)
	if body != nil {
		req.Header.Set("Content-Type", iface.MediaType)
	}
	for k, v := range call.header {
		req.Header[k] = v
	}
	c.origin.apply(req.Header, "")
	resp, err := c.send(ctx, call.kind, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if call.catalogue {
		resp.Body = c.pacedCatalogue(ctx, resp.Body)
		err = c.readCatalogue(resp, call)
	} else {
		err = c.decode(resp, call)
	}
	// The connection a cancelled caller closes can draw an answer that raced the close (an empty 200 of a server
	// whose handler saw the close first); judged after the cancellation, it says nothing about the main installation.
	if cerr := ctx.Err(); err != nil && cerr != nil && !errors.Is(cerr, context.DeadlineExceeded) {
		return cerr
	}
	return err
}

// commonHeaders sets the headers of every request of the Bastion (R-BAS-008).
func (c *HTTPClient) commonHeaders(h http.Header) {
	h.Set("User-Agent", iface.UserAgentPrefix+c.o.Release)
	h.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
	h.Set(iface.HeaderReplica, c.o.Replica)
}

// acquire takes a slot of the budget and counts a request that found none.
func (c *HTTPClient) acquire(ctx context.Context, class Class, kind Kind) (func(), error) {
	if c.o.Budget == nil {
		return func() {}, nil
	}
	release, err := c.o.Budget.Acquire(ctx, class)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			c.o.Metrics.MainRequests.WithLabelValues(string(kind), metrics.OutcomeBusy).Inc()
		}
		return nil, err
	}
	return release, nil
}

// send performs the request and counts it. A transport failure is an [UnreachableError]; the cancellation of
// the caller's own context is returned as it is, because it says nothing about the main installation.
func (c *HTTPClient) send(ctx context.Context, kind Kind, req *http.Request) (*http.Response, error) {
	start := c.now()
	resp, err := c.http.Do(req)
	c.o.Metrics.MainDuration.WithLabelValues(string(kind)).Observe(c.now().Sub(start).Seconds())
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL carries the caller's query
		}
		timeout := isTimeout(err)
		outcome := metrics.OutcomeError
		if timeout {
			outcome = metrics.OutcomeTimeout
		}
		c.o.Metrics.MainRequests.WithLabelValues(string(kind), outcome).Inc()
		if cerr := ctx.Err(); cerr != nil && !errors.Is(cerr, context.DeadlineExceeded) {
			return nil, cerr
		}
		return nil, &UnreachableError{Err: err, Timeout: timeout}
	}
	c.o.Metrics.MainRequests.WithLabelValues(string(kind), statusClass(resp.StatusCode)).Inc()
	return resp, nil
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

func statusClass(status int) string {
	switch {
	case status >= 500:
		return metrics.Outcome5xx
	case status >= 400:
		return metrics.Outcome4xx
	case status >= 300:
		return metrics.Outcome3xx
	}
	return metrics.Outcome2xx
}

// checkVersion compares the interface version an answer states with the Bastion's (R-BAS-009, R-BAS-028).
func checkVersion(h http.Header) error {
	v := h.Get(iface.HeaderInterface)
	if v == "" || v == strconv.Itoa(iface.InterfaceVersion) {
		return nil
	}
	n, _ := strconv.Atoi(v)
	return &MismatchError{Main: n}
}

// problem is the part of a problem body the client reads.
type problem struct {
	Code             string `json:"code"`
	InterfaceVersion int    `json:"interfaceVersion"`
}

// readProblem reads at most problemPeek bytes of a body and returns what it parsed and what it read.
func readProblem(body io.Reader) (problem, []byte) {
	buf, _ := io.ReadAll(io.LimitReader(body, problemPeek))
	var p problem
	_ = json.Unmarshal(buf, &p)
	return p, buf
}

// classify turns an answer that refuses the Bastion into its typed error; nil means it does not. own is true for
// the Bastion's own calls of the interface (handshake, feed, check, downloads) and false for a forward: the
// Bastion's token is refused only when the answer to a call it made itself says so. A forward carries a path the
// caller chose, which can name a route of the interface or of the management API, so a 401 bastion_token_invalid
// on it is no statement about the Bastion's token: it is an [AnswerError] that answers the caller 502
// bad_gateway (R-BAS-006, R-BAS-021, Q-BAS-121).
func (c *HTTPClient) classify(status int, p problem, own bool) error {
	switch {
	case status == http.StatusBadRequest && p.Code == iface.CodeInterfaceMismatch:
		return &MismatchError{Main: p.InterfaceVersion}
	case status == http.StatusUnauthorized && p.Code == iface.CodeTokenInvalid:
		if !own {
			return &AnswerError{Status: status, Code: p.Code}
		}
		c.o.Metrics.MainTokenRefused.Set(1)
		return ErrTokenRefused
	}
	return nil
}

// decode reads the answer of an interface operation.
func (c *HTTPClient) decode(resp *http.Response, call ifaceCall) error {
	if err := checkVersion(resp.Header); err != nil {
		return err
	}
	if resp.StatusCode/100 == 2 {
		c.o.Metrics.MainTokenRefused.Set(0)
		if call.out == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxInterfaceBody))
			return nil
		}
		if resp.StatusCode != http.StatusOK {
			return &AnswerError{Status: resp.StatusCode}
		}
		lr := &io.LimitedReader{R: resp.Body, N: maxInterfaceBody + 1}
		if err := iface.DecodeStrict(lr, call.out); err != nil {
			return &AnswerError{Status: resp.StatusCode, Err: err}
		}
		return nil
	}
	p, _ := readProblem(resp.Body)
	if err := c.classify(resp.StatusCode, p, true); err != nil {
		return err
	}
	if resp.StatusCode >= 500 {
		return &UnreachableError{Status: resp.StatusCode}
	}
	return &AnswerError{Status: resp.StatusCode, Code: p.Code}
}

// Do implements [Client].
func (c *HTTPClient) Do(ctx context.Context, f Forward) (*Response, error) {
	if f.Request == nil || f.Request.URL == nil {
		return nil, errors.New("mainclient: a forward needs a request")
	}
	kind := f.Kind
	if kind == "" {
		kind = KindForward
	}
	req, err := c.forwardRequest(ctx, f)
	if err != nil {
		return nil, err
	}
	since := c.now()
	release, err := c.acquire(ctx, f.Class, kind)
	if err != nil {
		return nil, err
	}
	adm, err := c.admit(ctx, f, kind, since)
	if err != nil {
		release()
		return nil, err
	}
	resp, err := c.send(ctx, kind, req)
	if err != nil {
		adm.Cancel()
		release()
		return nil, err
	}
	out, err := c.accept(resp, release, f.Offer.MaxSize >= 1)
	if err != nil {
		adm.Cancel()
		_ = resp.Body.Close()
		release()
		return nil, err
	}
	sb := out.Body.(*slotBody)
	paced := adm.Pace(sb.ReadCloser)
	sb.ReadCloser = paced
	if c.o.Throughput != nil {
		out.Pacing = paced
	}
	return out, nil
}

// admit admits a forward through the throughput after its slot (R-BAS-097): a miss waits for its first bytes at most the rest
// of the queue's time since it asked for its slot; the continuation of a fill runs already and is only paced.
func (c *HTTPClient) admit(ctx context.Context, f Forward, kind Kind, since time.Time) (Admission, error) {
	t := c.o.Throughput
	switch {
	case t == nil:
		return unpaced{}, nil
	case f.RangeFrom > 0:
		return t.Running(ctx, f.Class), nil
	}
	adm, err := t.Admit(ctx, f.Class, since)
	if err != nil {
		var te *ThroughputError
		if errors.As(err, &te) {
			c.o.Metrics.MainRequests.WithLabelValues(string(kind), metrics.OutcomeBusy).Inc()
		}
		return nil, err
	}
	return adm, nil
}

// unpaced is the admission of a client without a throughput.
type unpaced struct{}

func (unpaced) Pace(body io.ReadCloser) PacedBody { return unpacedBody{body} }
func (unpaced) Cancel()                           {}

type unpacedBody struct{ io.ReadCloser }

func (unpacedBody) Waiting() bool         { return false }
func (unpacedBody) Waited() time.Duration { return 0 }

// accept turns the main installation's answer to a forward into a [Response], or into the error it stands for.
// On an error the caller closes the body and releases the slot. offered says whether the forward carried the offer of
// R-BAS-072.
func (c *HTTPClient) accept(resp *http.Response, release func(), offered bool) (*Response, error) {
	if err := checkVersion(resp.Header); err != nil {
		return nil, err
	}
	m, marked, merr := iface.ParseMarking(resp.Header)
	if merr != nil {
		return nil, &AnswerError{Status: resp.StatusCode, Err: merr}
	}
	h, herr, err := takeHandover(resp, m, offered)
	if err != nil {
		return nil, err
	}
	body := resp.Body
	if (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized) &&
		strings.HasPrefix(resp.Header.Get("Content-Type"), iface.ProblemMediaType) {
		p, buf := readProblem(body)
		if err := c.classify(resp.StatusCode, p, false); err != nil {
			return nil, err
		}
		body = &prefixed{Reader: io.MultiReader(bytes.NewReader(buf), resp.Body), Closer: resp.Body}
	}
	if !marked && resp.StatusCode >= 500 {
		return nil, &UnreachableError{Status: resp.StatusCode}
	}
	if marked {
		c.o.Metrics.MainTokenRefused.Set(0)
	}
	iface.StripMarking(resp.Header)
	return &Response{
		Status: resp.StatusCode, Header: resp.Header, Body: &slotBody{ReadCloser: body, release: release}, Marking: m, Marked: marked,
		Handover: h, HandoverErr: herr,
	}, nil
}

// takeHandover reads the handover of an answer before its marking is stripped (R-BAS-073). Only a forward that offered
// takes one: an answer of the class handover, or one that carries a handover's value, to a forward that offered none is a
// bad answer, never an empty 200 for a caller, and so is a value on an answer of another class. An answer of the class
// handover to a forward that offered returns its value, or in herr why it has none the engine may use: a missing value, a
// value the interface refuses, more than one, another status than 200. Such an answer is replaced by the forward without
// the offer (R-BAS-075); a refused value's key was destroyed by the decoding.
func takeHandover(resp *http.Response, m iface.Marking, offered bool) (h *iface.Handover, herr, err error) {
	carries := len(resp.Header.Values(iface.HeaderHandover)) > 0
	switch {
	case m.Class != iface.ClassHandover && !carries:
		return nil, nil, nil
	case !offered:
		return nil, nil, &AnswerError{Status: resp.StatusCode, Err: errors.New("a handover answers a forward that offered none")}
	case m.Class != iface.ClassHandover:
		return nil, nil, &AnswerError{Status: resp.StatusCode, Err: fmt.Errorf("a handover's value on an answer of the class %q", m.Class)}
	}
	v, ok, derr := iface.HandoverFrom(resp.Header)
	switch {
	case derr != nil:
		return nil, derr, nil
	case !ok:
		return nil, errors.New("bastion handover: an answer of the class handover carries no value"), nil
	case resp.StatusCode != http.StatusOK:
		v.DataKey.Destroy()
		return nil, fmt.Errorf("bastion handover: an answer of the class handover with status %d", resp.StatusCode), nil
	}
	return &v, nil, nil
}

type prefixed struct {
	io.Reader
	io.Closer
}

// slotBody releases the budget slot when the body ends or is closed (R-BAS-025).
type slotBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *slotBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.release)
	}
	return n, err
}

func (b *slotBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

// stripped are the request headers of a caller that never go to the main installation: the Bastion sets its
// own forwarded-for, interface and conditional headers, the caller's cookies are not its business, a range or a
// condition would turn the Bastion's fetch into a partial or empty one (R-BAS-021), and the caller's trace
// context is not continued (R-BAS-042).
func stripped(name string) bool {
	switch name {
	case "Cookie", "Cookie2", "Forwarded", "Range", "Host", "Content-Length", "Expect",
		"Traceparent", "Tracestate", "Baggage", "Authorization-Info",
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "Te", "Trailer",
		"Transfer-Encoding", "Upgrade", "User-Agent":
		return true
	}
	return strings.HasPrefix(name, "X-Forwarded-") || strings.HasPrefix(name, "If-") || strings.HasPrefix(name, "Quader-")
}

func (c *HTTPClient) forwardRequest(ctx context.Context, f Forward) (*http.Request, error) {
	src := f.Request
	var body io.Reader
	if f.Body != nil {
		body = f.Body
	}
	req, err := http.NewRequestWithContext(ctx, src.Method, c.packages.String(), body)
	if err != nil {
		return nil, fmt.Errorf("mainclient: building the forward: %w", err)
	}
	req.URL.Path, req.URL.RawPath, req.URL.RawQuery = src.URL.Path, src.URL.RawPath, src.URL.RawQuery
	if body != nil && src.ContentLength > 0 {
		req.ContentLength = src.ContentLength
	}
	connection := map[string]bool{}
	for _, v := range src.Header.Values("Connection") {
		for _, t := range strings.Split(v, ",") {
			connection[http.CanonicalHeaderKey(strings.TrimSpace(t))] = true
		}
	}
	for k, vs := range src.Header {
		if stripped(k) || connection[k] {
			continue
		}
		req.Header[k] = append([]string(nil), vs...)
	}
	c.commonHeaders(req.Header)
	tok := c.o.Tokens.Token()
	if tok.Value == "" {
		return nil, errors.New("mainclient: no Bastion token in use")
	}
	req.Header.Set(iface.HeaderAuthorization, iface.BearerScheme+" "+tok.Value)
	if f.IfNoneMatch != "" {
		req.Header.Set("If-None-Match", f.IfNoneMatch)
	}
	// The Bastion's own headers, after the copy of the caller's, which dropped every Quader-*, Range and If-* header.
	f.Offer.Set(req.Header)
	if f.ForHead && req.Method == http.MethodGet {
		req.Header.Set(iface.HeaderForHead, iface.ForHeadValue)
	}
	if f.RangeFrom > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(f.RangeFrom, 10)+"-")
		if f.IfRange != "" {
			req.Header.Set("If-Range", f.IfRange)
		}
	}
	c.origin.apply(req.Header, f.ClientAddress)
	return req, nil
}
