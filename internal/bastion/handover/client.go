// Package handover is the Bastion's direct read of a blob the main installation handed over (R-BAS-074 to R-BAS-076): the
// link client, a client of its own and never the client of the main installation, and the readers of a plain and of an
// encrypted body. The engine decides when a handover is read and what happens with the bytes; this package fetches them
// from the object store, checks what can be checked before the first byte, verifies the bytes as they arrive, and holds the
// data key of an encrypted copy for the length of one read.
//
// What the link client sends is one GET of the link with User-Agent quader-bastion/<release> and nothing else of Quader's:
// no Authorization, no credential of a caller, no header of the interface, no forwarded header, no cookie and no trace
// context. It follows no redirect, and it refuses a link that is not https, that carries user information, or whose host
// is a host of the Bastion or of the main installation, so that nothing meant for Quader leaves on a path without a Bastion
// token. Its errors name neither the link nor the key.
package handover

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
)

// Purpose is the purpose label of the link client in the shared outbound metrics.
const Purpose = "bastion_handover"

// Options are what [New] needs; the server builds them from the configuration.
type Options struct {
	// Release is the release for the User-Agent.
	Release string
	// CAFile is bastion.main.ca_file (the chart's trustedCA): a bundle added to the system roots and read again without a
	// restart (Q-BAS-237).
	CAFile string
	// ConnectTimeout and ResponseHeaderTimeout are bastion.main.connect_timeout and response_header_timeout.
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	// RefusedHosts are the hosts of server.base_url, bastion.main.url and bastion.main.packages_url: a link on one of them is
	// refused, whatever its port (R-BAS-074).
	RefusedHosts []string
	// TrustCheckEvery is how often the CA file is looked at again at most; zero means the outbound factory's default.
	TrustCheckEvery time.Duration
	// WrapTransport wraps the transport; tests use it. Production leaves it nil.
	WrapTransport func(http.RoundTripper) http.RoundTripper
	// OnKeyDestroyed is called once for every data key the client destroyed; a test counts with it (R-BAS-076).
	OnKeyDestroyed func()
}

// Client is the link client of R-BAS-074.
type Client struct {
	o       Options
	http    *http.Client
	refused []string
	// keys counts the data keys held by reads that have not ended, for the tests of R-BAS-076.
	keys atomic.Int64
}

// New builds the link client. A CA file that does not load is an error; no connection is tried.
func New(o Options) (*Client, error) {
	cfg := config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true, MinVersion: "1.2"}}
	if o.CAFile != "" {
		cfg.TLS.CAFiles = []string{o.CAFile}
	}
	// No tracer and no upstream log: the store receives no trace context of Quader's, and a log record would carry the
	// link's path. No outbound metrics either: they are registered once per process, by the client of the main
	// installation, and the Bastion's own metrics count what a handover did (R-BAS-075). The address policy is empty: the
	// host comes from the main installation, as the main installation's own does from the operator (H-446).
	opts := []outbound.Option{outbound.WithVersion(o.Release)}
	if o.TrustCheckEvery > 0 {
		opts = append(opts, outbound.WithTrustCheckEvery(o.TrustCheckEvery))
	}
	f := outbound.New(cfg, opts...)
	if err := f.Err(); err != nil {
		return nil, fmt.Errorf("handover: bastion.main.ca_file: %w", err)
	}
	hc := f.Client(Purpose,
		outbound.WithConnectTimeout(o.ConnectTimeout),
		outbound.WithResponseHeaderTimeout(o.ResponseHeaderTimeout),
		outbound.WithDirect(),
	)
	if o.WrapTransport != nil {
		hc.Transport = o.WrapTransport(hc.Transport)
	}
	// A redirect is a failure before the first byte (R-BAS-075): the link names one object, and a redirect would lead
	// wherever the store or a party in between says.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	hc.Jar = nil
	c := &Client{o: o, http: hc}
	for _, h := range o.RefusedHosts {
		if k := hostKey(h); k != "" {
			c.refused = append(c.refused, k)
		}
	}
	return c, nil
}

// Close closes the idle connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

// KeysHeld is the number of data keys that reads of this client hold now: zero whenever no direct read of an encrypted
// copy runs (R-BAS-076).
func (c *Client) KeysHeld() int64 { return c.keys.Load() }

// Cause says why a direct read failed before its first byte, which decides the cooldown of R-BAS-075.
type Cause string

// The causes.
const (
	// CauseNetwork: a connect, name or TLS error, a timeout, or a body that broke off before its first byte.
	CauseNetwork Cause = "network"
	// CauseStore: the store answered 403 or 5xx, or with another length than the stored size.
	CauseStore Cause = "store"
	// CauseOther: everything that says nothing about the path to the store: a refused link, a redirect, another status, a
	// first segment that does not open under the handed key, a body that is not its digest, the caller's cancel.
	CauseOther Cause = "other"
)

// Error is the failure of a direct read before its first byte. Its text names neither the link nor the key.
type Error struct {
	Cause Cause
	// Digest is set when the bytes were read whole and are not the handed digest: the copy is not the blob (R-BAS-075).
	Digest bool
	msg    string
	err    error
}

// Error implements error.
func (e *Error) Error() string {
	if e.err != nil {
		return "handover: " + e.msg + ": " + e.err.Error()
	}
	return "handover: " + e.msg
}

// Unwrap returns the cause, free of the link.
func (e *Error) Unwrap() error { return e.err }

// Cooldown reports whether the failure says the store cannot be read from this replica (R-BAS-075, Q-BAS-236): the network
// or the store, not the value or the bytes.
func (e *Error) Cooldown() bool { return e.Cause == CauseNetwork || e.Cause == CauseStore }

func failure(cause Cause, msg string, err error) *Error {
	return &Error{Cause: cause, msg: msg, err: err}
}

// ErrDigest is the end of a read whose bytes are not the handed blob: the SHA-256 of a plain body, or on an encrypted copy
// the plaintext's SHA-256 after every segment authenticated (R-BAS-075). The engine fails the fill and marks the key.
var ErrDigest = errors.New("handover: the bytes are not the handed blob")

// Open starts the direct read of a handover the engine has checked (iface.Handover.Check): one GET of the whole object
// from byte 0 (R-BAS-074). It returns once the first bytes of the plaintext are verified and held: the status is 200, the
// length is the stored size, and on an encrypted copy the first segment authenticated under the handed key (on a blob of
// one segment, the whole object did). Every failure up to that point is an *Error, and the read is replaced by the forward
// of R-BAS-075. Open takes the handover's data key: it destroys h.DataKey before it returns, whatever happens, and the
// format's key it builds from it lives until [Read.Close] or until Open fails.
func (c *Client) Open(ctx context.Context, h iface.Handover) (*Read, error) {
	defer h.DataKey.Destroy()
	link, err := c.checkLink(h.Link)
	if err != nil {
		return nil, err
	}
	var key *heldKey
	if h.Encrypted {
		if key, err = c.takeKey(h); err != nil {
			return nil, err
		}
	} else if h.StoredSize != h.Size || h.DataKey.Present() {
		return nil, failure(CauseOther, "a plain copy's stored size is its size and it carries no key", nil)
	}
	rd, err := c.open(ctx, link, h, key)
	if err != nil {
		key.destroy()
		return nil, err
	}
	return rd, nil
}

// takeKey builds the format's data key from the handed one and checks S(size) against storedSize with the format's own
// function before anything is read (R-BAS-074).
func (c *Client) takeKey(h iface.Handover) (*heldKey, error) {
	if stored, err := blobcrypt.StoredSize(h.Size); err != nil || stored != h.StoredSize {
		return nil, failure(CauseOther, "an encrypted copy's stored size is not S(size)", nil)
	}
	raw := h.DataKey.Bytes()
	h.DataKey.Destroy()
	k, err := blobcrypt.KeyFromBytes(raw)
	clear(raw)
	if err != nil {
		return nil, failure(CauseOther, "the handed data key is not a key", nil)
	}
	c.keys.Add(1)
	return &heldKey{c: c, key: k}, nil
}

// heldKey is the data key of one read; destroy runs once.
type heldKey struct {
	c    *Client
	key  blobcrypt.Key
	done atomic.Bool
}

func (k *heldKey) destroy() {
	if k == nil || !k.done.CompareAndSwap(false, true) {
		return
	}
	k.key.Destroy()
	k.c.keys.Add(-1)
	if k.c.o.OnKeyDestroyed != nil {
		k.c.o.OnKeyDestroyed()
	}
}

// checkLink parses a link and holds it to the link client's own rules, whatever the engine checked before: https, a host,
// no user information, and no host of the Bastion or of the main installation, whatever the port.
func (c *Client) checkLink(link string) (*url.URL, error) {
	u, err := url.Parse(link)
	switch {
	case err != nil:
		return nil, failure(CauseOther, "the link is not a URL", nil)
	case u.Scheme != "https" || u.Opaque != "" || u.Hostname() == "":
		return nil, failure(CauseOther, "the link is not an absolute https URL", nil)
	case u.User != nil:
		return nil, failure(CauseOther, "the link carries user information", nil)
	}
	host := hostKey(u.Hostname())
	for _, r := range c.refused {
		if r == host {
			return nil, failure(CauseOther, "the link leads to a host of the Bastion or of the main installation", nil)
		}
	}
	return u, nil
}

// open sends the GET and checks the answer up to the first verified bytes.
func (c *Client) open(ctx context.Context, link *url.URL, h iface.Handover, key *heldKey) (*Read, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, failure(CauseOther, "building the request", nil)
	}
	// The request is built from nothing but the link: no header of the caller's request, of the interface or of a
	// forward can be on it.
	req.Header = http.Header{"User-Agent": {iface.UserAgentPrefix + c.o.Release}}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	if err := checkAnswer(resp, h.StoredSize); err != nil {
		drain(resp)
		return nil, err
	}
	rd := newRead(resp.Body, h, key)
	if err := rd.first(); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return rd, nil
}

// checkAnswer is what holds before the first byte: status 200 and a Content-Length equal to the stored size.
func checkAnswer(resp *http.Response, stored int64) error {
	switch s := resp.StatusCode; {
	case s >= 300 && s < 400:
		return failure(CauseOther, fmt.Sprintf("the store redirected (status %d), which the link client does not follow", s), nil)
	case s == http.StatusForbidden || s >= 500:
		return failure(CauseStore, fmt.Sprintf("the store answered %d", s), nil)
	case s != http.StatusOK:
		return failure(CauseOther, fmt.Sprintf("the store answered %d", s), nil)
	case resp.ContentLength != stored:
		return failure(CauseStore, fmt.Sprintf("the store answered %d bytes, not the stored size %d", resp.ContentLength, stored), nil)
	}
	return nil
}

// drain closes the body of an answer that is not read; a short one is read to its end so the connection can be reused.
func drain(resp *http.Response) {
	buf := make([]byte, 4<<10)
	_, _ = resp.Body.Read(buf)
	_ = resp.Body.Close()
}

// transportFailure turns the error of a request that got no answer into an *Error without the link: a url.Error carries
// the whole URL, signature included.
func transportFailure(ctx context.Context, err error) *Error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure(CauseOther, "the read was cancelled", context.Canceled)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return failure(CauseNetwork, "the store did not answer in time", errors.New("timeout"))
	}
	return failure(CauseNetwork, "the store could not be reached", scrubbed(err))
}

// scrubbed keeps the text of a transport error and drops anything after a query or a fragment, should a lower layer ever
// write a URL into it.
func scrubbed(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if i := strings.IndexAny(msg, "?#"); i >= 0 {
		msg = msg[:i] + "…"
	}
	return errors.New(msg)
}

// hostKey is a host name as two hosts are compared: lower case, without brackets and without a trailing dot.
func hostKey(h string) string {
	if u, err := url.Parse(h); err == nil && u.Host != "" {
		h = u.Hostname()
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(h, "[]")), ".")
}
