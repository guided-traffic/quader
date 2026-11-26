// Package settings holds the configuration a Bastion replica applies (R-BAS-093): the member configuration of the main
// installation's handshake, or, before the first handshake, the one the replica stored in its cache backend, else a built-in
// one that fails closed (Q-BAS-327). It is applied whole: [Holder.Apply] builds an [Applied] from it, which is never changed
// after, and swaps it in with one atomic store, so every reader sees the old or the new figures and never a mix. A request
// reads it once, at its start, and carries it on its context ([With], [Holder.For]); the guard, the anonymous ceiling and the
// engine read their figures from there.
//
// The figures the replica reads of it here are the flood protection (R-BAS-035 to R-BAS-037, Q-LIM-307), the slots (R-BAS-025)
// and the anonymous ceiling (R-BAS-094); the switches, the place's table and the throughput are kept whole for the packages
// that apply them (R-BAS-096, R-BAS-097, W-35).
package settings

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// Source is where the configuration a replica applies came from (quader_bastion_configuration_source).
type Source string

// The sources (R-BAS-093, Q-BAS-327).
const (
	// SourceMain is the configuration of the last handshake.
	SourceMain Source = "main"
	// SourceStored is the one the replica found in its cache backend at start, before its first handshake.
	SourceStored Source = "stored"
	// SourceBuiltin is [Builtin]: before the first handshake, without a stored configuration whose signature holds.
	SourceBuiltin Source = "builtin"
)

// Applied is one configuration as a replica applies it: the member of the wire and what the replica reads of it in the form
// it decides with. It is never changed after [FromConfiguration] built it.
type Applied struct {
	// Configuration is the member as it arrived, whole (R-BAS-093).
	Configuration iface.Configuration
	Source        Source
	Flood         Flood
	Slots         Slots
	Ceiling       Ceiling
}

// Flood is the flood protection per address (R-BAS-035 to R-BAS-037, Q-LIM-307) with its windows as durations and its exempt
// networks parsed.
type Flood struct {
	Anonymous, Authenticated Class
	// GUI is the class of the web interface's pages: requests only, a page view is never a miss (Q-LIM-307).
	GUI        Rate
	FailedAuth FailedAuth
	Blocking   Blocking
	// Exempt are the networks no flood figure, timeout or block applies to.
	Exempt []netip.Prefix
}

// Class is the request and miss figures of a class of callers per address.
type Class struct {
	Rate
	MissesPerMinute int
}

// Rate is a token bucket per address: a sustained rate and a burst.
type Rate struct {
	RequestsPerSecond int
	Burst             int
}

// FailedAuth is the failed-authentication timeout (R-BAS-036).
type FailedAuth struct {
	MaxFailures     int
	Window, Timeout time.Duration
}

// Blocking is the blocking of flooding addresses (R-BAS-037).
type Blocking struct {
	Threshold                     int
	Window, Duration, MaxDuration time.Duration
}

// Slots are the requests in flight to the main installation across the Bastion's replicas (R-BAS-025).
type Slots struct {
	MaxConcurrent  int
	AnonymousShare float64
	QueueTimeout   time.Duration
}

// Ceiling is the anonymous ceiling (R-BAS-094): requests to the main installation on behalf of all anonymous callers of the
// Bastion together. A figure of 0 is unlimited; Burst is set whenever PerSecond is (a rate without a burst holds one second
// of it).
type Ceiling struct {
	PerSecond, Burst, PerHour int64
}

// Unlimited reports whether the ceiling holds nothing.
func (c Ceiling) Unlimited() bool { return c.PerSecond <= 0 && c.PerHour <= 0 }

// FromConfiguration validates c as the link does (iface.Configuration.Validate) and builds what the replica applies.
func FromConfiguration(c iface.Configuration, src Source) (*Applied, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	f := c.Limits.Flood
	a := &Applied{
		Configuration: c, Source: src,
		Flood: Flood{
			Anonymous:     Class{Rate: Rate{f.Anonymous.RequestsPerSecond, f.Anonymous.Burst}, MissesPerMinute: f.Anonymous.MissesPerMinute},
			Authenticated: Class{Rate: Rate{f.Authenticated.RequestsPerSecond, f.Authenticated.Burst}, MissesPerMinute: f.Authenticated.MissesPerMinute},
			GUI:           Rate{f.GUI.RequestsPerSecond, f.GUI.Burst},
			FailedAuth: FailedAuth{
				MaxFailures: f.FailedAuth.MaxFailures, Window: seconds(f.FailedAuth.WindowSeconds), Timeout: seconds(f.FailedAuth.TimeoutSeconds),
			},
			Blocking: Blocking{
				Threshold: f.Blocking.Threshold, Window: seconds(f.Blocking.WindowSeconds), Duration: seconds(f.Blocking.DurationSeconds),
				MaxDuration: seconds(f.Blocking.MaxDurationSeconds),
			},
		},
		Slots: Slots{
			MaxConcurrent: c.Limits.Slots.MaxConcurrent, AnonymousShare: c.Limits.Slots.AnonymousShare,
			QueueTimeout: time.Duration(c.Limits.Slots.QueueTimeoutMillis) * time.Millisecond,
		},
		Ceiling: ceilingOf(c.Limits.AnonymousCeiling),
	}
	for _, n := range f.ExemptNetworks {
		p, err := netip.ParsePrefix(n)
		if err != nil {
			return nil, fmt.Errorf("bastion configuration: the exempt network %q: %w", n, err)
		}
		a.Flood.Exempt = append(a.Flood.Exempt, p)
	}
	return a, nil
}

func seconds(n int) time.Duration { return time.Duration(n) * time.Second }

func ceilingOf(c iface.Ceiling) Ceiling {
	var out Ceiling
	if c.RequestsPerSecond != nil {
		out.PerSecond = *c.RequestsPerSecond
		out.Burst = out.PerSecond
		if c.Burst != nil {
			out.Burst = *c.Burst
		}
	}
	if c.RequestsPerHour != nil {
		out.PerHour = *c.RequestsPerHour
	}
	return out
}

// Exempt reports whether the address key (an IPv4 address, or the /64 of an IPv6 one, guard.AddressKey) lies in an exempt
// network. An IPv6 key counts by the first address of its /64.
func (a *Applied) Exempt(key string) bool {
	if len(a.Flood.Exempt) == 0 {
		return false
	}
	var addr netip.Addr
	if p, err := netip.ParsePrefix(key); err == nil {
		addr = p.Addr()
	} else if addr, err = netip.ParseAddr(key); err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range a.Flood.Exempt {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

type ctxKey struct{}

// With carries the configuration a request read at its start, so that every decision of the request is made with the same
// figures, whatever a handshake swaps in meanwhile.
func With(ctx context.Context, a *Applied) context.Context {
	if a == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, a)
}

// From returns the configuration [With] put on ctx, or nil.
func From(ctx context.Context) *Applied {
	a, _ := ctx.Value(ctxKey{}).(*Applied)
	return a
}
