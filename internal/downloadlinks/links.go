package downloadlinks

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/items"
)

// The codes of the reasons a connection cannot serve links (R-STOR-074) or a handover (R-STOR-096), as the settings' answer names
// them.
const (
	ReasonEncrypted       = "encrypted"
	ReasonNotHTTPS        = "not_https"
	ReasonQuaderHost      = "quader_host"
	ReasonLinkCheckFailed = "link_check_failed"
)

// Reason is why a connection cannot serve links: a code and what it names (the host it collides with, what the store answered).
type Reason struct {
	Code, Detail string
}

// ConnectionState is what a replica decided of one connection from its own configuration, the settings' snapshot and its link
// check (R-STOR-074, R-STOR-096).
type ConnectionState struct {
	Name      string
	Encrypted bool
	// Endpoint is the connection's endpoint, PublicAddress the origin its links name: the setting, or the endpoint without one.
	Endpoint, PublicAddress string
	// Usable: the connection can serve a client's link now on this replica; Reasons say why not.
	Usable  bool
	Reasons []Reason
	// HandoverUsable: the connection can serve a handover to a Bastion (the same address rules and the link check's first part, on an
	// encrypted connection too); HandoverReasons say why not.
	HandoverUsable  bool
	HandoverReasons []Reason
	LinkCheck       blob.LinkCheck
}

// CacheControl is what a link asks of the store's answer: a shared cache keeps no bearer link's bytes (R-STOR-072, Q-STOR-287).
const CacheControl = "private, no-store"

// Options configure [New].
type Options struct {
	Connections *blob.Connections
	// Copies reads the copies of a blob for an item whose lookup did not read them (one statement; the item lookup reads them for
	// every download, Q-STOR-275). Nil streams such a download.
	Copies func(ctx context.Context, d blob.Digest) ([]blob.Copy, error)
	Rule   HostRule
	// Replica names the probe objects of the link check.
	Replica string
	Metrics *Metrics
	Log     *slog.Logger
}

// Links is the replica's side of the delivery by signed links: the settings' snapshot, each connection's state and the signing.
// It is safe for concurrent use.
type Links struct {
	o        Options
	settings atomic.Pointer[Settings]
	states   atomic.Pointer[map[string]ConnectionState]

	mu      sync.Mutex // serialises a recompute of the states and its log lines
	logged  map[string]string
	loaded  bool
	checkMu sync.Mutex // one link check run at a time
}

// New creates the replica's side with the defaults of a new installation until a snapshot is set.
func New(o Options) *Links {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	l := &Links{o: o, logged: map[string]string{}}
	d := Defaults()
	l.settings.Store(&d)
	l.recompute()
	return l
}

// Settings is the replica's snapshot of the row.
func (l *Links) Settings() Settings { return *l.settings.Load() }

// Metrics is the families the serve point counts with.
func (l *Links) Metrics() *Metrics { return l.o.Metrics }

// Rule is the replica's host rule, which a write's validation applies (R-STOR-074).
func (l *Links) Rule() HostRule { return l.o.Rule }

// ConnectionNames are the ids of the replica's connections, in list order.
func (l *Links) ConnectionNames() []string {
	var out []string
	if l.o.Connections != nil {
		for _, c := range l.o.Connections.List() {
			out = append(out, c.ID())
		}
	}
	return out
}

// SetSettings replaces the snapshot and decides again which connections can serve links. It reports whether the switch was turned
// on by this snapshot, when the caller runs the link checks again (R-STOR-074).
func (l *Links) SetSettings(s Settings) (switchedOn bool) {
	prev := l.settings.Swap(&s)
	l.mu.Lock()
	first := !l.loaded
	l.loaded = true
	l.mu.Unlock()
	l.recompute()
	return s.Enabled && (first || !prev.Enabled)
}

// States are the connections' states in list order.
func (l *Links) States() []ConnectionState {
	m := *l.states.Load()
	out := make([]ConnectionState, 0, len(m))
	for _, name := range l.ConnectionNames() {
		out = append(out, m[name])
	}
	return out
}

// State is one connection's state.
func (l *Links) State(conn string) (ConnectionState, bool) {
	s, ok := (*l.states.Load())[conn]
	return s, ok
}

// recompute decides every connection's state from the snapshot and its link check, sets the gauge and logs a connection that turned
// unable once per change (warn, with the reasons).
func (l *Links) recompute() {
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.Settings()
	m := map[string]ConnectionState{}
	if l.o.Connections != nil {
		for _, c := range l.o.Connections.List() {
			st := l.decide(c, set)
			m[c.ID()] = st
			l.o.Metrics.setUsable(c.ID(), st.Usable)
			// Logged while the switch is on, once its link check answered, once per change of the reasons.
			why := reasonsText(st.Reasons)
			if !set.Enabled || !st.LinkCheck.Ran {
				why = ""
			}
			if why != l.logged[c.ID()] {
				if why != "" {
					l.o.Log.Warn("a connection cannot serve signed links on this replica", "connection", c.ID(), "reasons", why)
				}
				l.logged[c.ID()] = why
			}
		}
	}
	l.states.Store(&m)
}

func reasonsText(rs []Reason) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		if r.Detail != "" {
			parts = append(parts, r.Code+" ("+r.Detail+")")
		} else {
			parts = append(parts, r.Code)
		}
	}
	return strings.Join(parts, ", ")
}

// decide is one connection's state (R-STOR-074, R-STOR-096): encrypted, the address not https, the link's host a host of Quader,
// the link check not passed.
func (l *Links) decide(c *blob.Connection, set Settings) ConnectionState {
	cfg := c.Config()
	st := ConnectionState{Name: c.ID(), Encrypted: c.Encrypted(), Endpoint: endpointOf(cfg), LinkCheck: c.LinkCheck()}
	st.PublicAddress = st.Endpoint
	if a, ok := set.PublicAddresses[c.ID()]; ok {
		st.PublicAddress = a
	}
	var address []Reason
	if u, err := url.Parse(st.PublicAddress); err != nil || u.Scheme != "https" || u.Host == "" {
		address = append(address, Reason{Code: ReasonNotHTTPS})
	} else if q, refused := l.o.Rule.Refuses(LinkHost(st.PublicAddress, c.Bucket(), cfg.ForcePathStyle)); refused {
		address = append(address, Reason{Code: ReasonQuaderHost, Detail: q})
	}
	handover := slices.Clone(address)
	if !st.LinkCheck.Ran || !st.LinkCheck.Works {
		handover = append(handover, Reason{Code: ReasonLinkCheckFailed, Detail: checkDetail(st.LinkCheck)})
	}
	if st.Encrypted {
		st.Reasons = append(st.Reasons, Reason{Code: ReasonEncrypted})
	}
	st.Reasons = append(st.Reasons, address...)
	if !st.LinkCheck.Passed() {
		st.Reasons = append(st.Reasons, Reason{Code: ReasonLinkCheckFailed, Detail: checkDetail(st.LinkCheck)})
	}
	st.Usable, st.HandoverUsable = len(st.Reasons) == 0, len(handover) == 0
	st.HandoverReasons = handover
	return st
}

func checkDetail(r blob.LinkCheck) string {
	if !r.Ran {
		return "the link check has not answered on this replica"
	}
	return r.Detail
}

// endpointOf is a connection's endpoint as a link names it without a public address: the configured one, or the regional endpoint
// of AWS that an empty endpoint resolves to (R-STOR-030).
func endpointOf(cfg config.StorageConnection) string {
	if cfg.Endpoint != "" {
		return config.NormalizeEndpoint(cfg.Endpoint)
	}
	return "https://s3." + cfg.Region + ".amazonaws.com"
}

// RunLinkChecks runs the link check on every connection (R-STOR-074): at the replica's start and when the switch is turned on, on an
// encrypted connection too (its probes are plain, R-STOR-096), and decides the states again. A connection whose store could not be
// asked keeps "not answered" and is asked again at the next run.
func (l *Links) RunLinkChecks(ctx context.Context) {
	l.checkMu.Lock()
	defer l.checkMu.Unlock()
	if l.o.Connections == nil {
		return
	}
	for _, c := range l.o.Connections.List() {
		if ctx.Err() != nil {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		r := blob.CheckLink(cctx, c.Objects(), c.Fetcher(), c.Prefix(), l.o.Replica)
		cancel()
		if !r.Ran && c.LinkCheck().Ran {
			continue // a store that could not be asked now keeps what it answered before
		}
		c.SetLinkCheck(r)
	}
	l.recompute()
}

// unanswered reports whether a connection's link check has not answered yet: the snapshot's loop asks it again.
func (l *Links) unanswered() bool {
	if l.o.Connections == nil {
		return false
	}
	for _, c := range l.o.Connections.List() {
		if !c.LinkCheck().Ran {
			return true
		}
	}
	return false
}

// Sign is steps 12 and 13 of R-STOR-070 for a download whose earlier conditions held: the first copy in the read order on a
// connection that can serve links (not encrypted, holding a copy not marked damaged, its circuit closed, its address and link check
// good, Q-STOR-291), and a link to that copy's object signed against its public address for GET with the item's content type, the
// disposition the serve point's own answer would carry (attachment with the file name, which the edge layer encodes) and private,
// no-store (R-STOR-072). declined names the reason of a download that streams instead: connection, credential or signing.
func (l *Links) Sign(ctx context.Context, it items.Item, disposition string) (link blob.Link, declined string) {
	copies := it.Copies
	if !it.CopiesRead {
		if l.o.Copies == nil {
			return blob.Link{}, DeclinedConnection
		}
		var err error
		if copies, err = l.o.Copies(ctx, it.Digest); err != nil {
			return blob.Link{}, DeclinedConnection
		}
	}
	if l.o.Connections == nil {
		return blob.Link{}, DeclinedConnection
	}
	set := l.Settings()
	for _, cp := range l.o.Connections.ServableOf(copies) {
		st, ok := l.State(cp.Connection)
		if !ok || !st.Usable {
			continue
		}
		c, _ := l.o.Connections.Get(cp.Connection)
		opts := blob.PresignOptions{Life: set.LinkLife(), ContentType: it.MediaTypeOrDefault(), ContentDisposition: disposition, CacheControl: CacheControl}
		if a, ok := set.PublicAddresses[c.ID()]; ok {
			opts.Address = a
		}
		link, err := c.Presign(ctx, cp.ObjectKey, opts)
		switch {
		case err == nil:
			return link, ""
		case errors.Is(err, blob.ErrCircuitOpen):
			return blob.Link{}, DeclinedConnection
		case errors.Is(err, blob.ErrCredentialExpiring):
			return blob.Link{}, DeclinedCredential
		default:
			l.o.Log.Warn("a signed link could not be signed; the download streams", "connection", c.ID(), "error", err)
			return blob.Link{}, DeclinedSigning
		}
	}
	return blob.Link{}, DeclinedConnection
}
