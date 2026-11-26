package bastionmain

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The replica's snapshot of the Bastion registrations: what a request of a Bastion is decided on without a statement (R-ARCH-027).
// It holds each registration's switch of the handover per blob (R-BAS-071), which the serve point and the handshake read, and the
// networks its Bastion connects from (R-BAS-017, Q-BAS-334), which the recognition reads. A change of either member announces its
// change area (bastion_handover, bastion_egress), every replica reads the rows again within the bound of R-ARCH-018 (2 s), and a
// lost change is read again after [SnapshotReload].

// SnapshotReload is the snapshot's safety period: a lost change is read again after it.
const SnapshotReload = time.Minute

// SnapshotRow is one registration as the snapshot holds it.
type SnapshotRow struct {
	// ID is the registration's principal ID.
	ID       string
	Handover bool
	Egress   []netip.Prefix
}

// snapshotState is one loaded set of rows, replaced whole.
type snapshotState struct {
	handover map[string]bool
	egress   map[string][]netip.Prefix
}

// RegistrationSnapshot is the replica's snapshot of the registrations. Before its first load, and while a load fails, it keeps what
// it had: no handover and no network for any registration before the first. It is safe for concurrent use; a nil snapshot answers
// as an empty one.
type RegistrationSnapshot struct {
	load func(ctx context.Context) ([]SnapshotRow, error)
	log  *slog.Logger

	state      atomic.Pointer[snapshotState]
	loaded     atomic.Bool
	switchedOn atomic.Pointer[func()]
	// trusted are server.trusted_proxies of the default endpoint: a registration's network that overlaps one is logged at the load
	// that brings it, because the forwarded headers from it count for every request, a Bastion's or not.
	trusted atomic.Pointer[[]netip.Prefix]

	kick   chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRegistrationSnapshot builds the snapshot over db and registers its cache on r, which a change of the areas bastion_handover
// and bastion_egress reloads; a nil registry reloads it at the safety period only.
func NewRegistrationSnapshot(db Reader, r *cache.Registry, log *slog.Logger) *RegistrationSnapshot {
	return newRegistrationSnapshot(func(ctx context.Context) ([]SnapshotRow, error) {
		var rows []q.BastionSnapshotRow
		err := db.Read(ctx, func(tx q.DBTX) error {
			var err error
			rows, err = q.New(tx).BastionSnapshot(ctx)
			return err
		})
		out := make([]SnapshotRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, SnapshotRow{ID: row.ID, Handover: row.Handover, Egress: egressOf(row.Egress)})
		}
		return out, err
	}, r, log)
}

// newRegistrationSnapshot is the snapshot over a load function (the tests give their own).
func newRegistrationSnapshot(load func(context.Context) ([]SnapshotRow, error), r *cache.Registry, log *slog.Logger) *RegistrationSnapshot {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &RegistrationSnapshot{load: load, log: log, kick: make(chan struct{}, 1)}
	s.state.Store(&snapshotState{handover: map[string]bool{}, egress: map[string][]netip.Prefix{}})
	if r != nil {
		cache.Register(r, "bastion_registrations", cache.Options[struct{}, struct{}]{
			MaxEntries: 1, SafetyTTL: SnapshotReload, Areas: []string{cluster.AreaBastionHandover, cluster.AreaBastionEgress},
			OnChange: func(*cache.Cache[struct{}, struct{}], string, int, json.RawMessage) { s.Reload() },
			OnFlush:  func(string) { s.Reload() },
		})
	}
	return s
}

// OnSwitchedOn sets what runs when a reload finds a registration's handover on that the snapshot before it had off: the replica
// runs its connections' link checks again (storage.md R-STOR-096). The first load calls nothing, since the link checks run at
// every start.
func (s *RegistrationSnapshot) OnSwitchedOn(f func()) { s.switchedOn.Store(&f) }

// WarnOverlap sets the networks of server.trusted_proxies a registration's network is compared with (Q-BAS-433): the networks the
// snapshot holds now at once, and each one a later load brings.
func (s *RegistrationSnapshot) WarnOverlap(trusted []netip.Prefix) {
	if s == nil {
		return
	}
	t := slices.Clone(trusted)
	s.trusted.Store(&t)
	cur := s.state.Load()
	s.warnOverlaps(cur, cur, true)
}

// On reports whether the handover of the registration with the principal ID bastionID is on in the replica's snapshot.
func (s *RegistrationSnapshot) On(bastionID string) bool {
	if s == nil {
		return false
	}
	return s.state.Load().handover[bastionID]
}

// Egress returns the networks the Bastion of the registration bastionID connects from, nil for none. The caller does not change
// the slice.
func (s *RegistrationSnapshot) Egress(bastionID string) []netip.Prefix {
	if s == nil {
		return nil
	}
	return s.state.Load().egress[bastionID]
}

// Reload asks the loop to read the rows now.
func (s *RegistrationSnapshot) Reload() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Name implements the composition root's Component.
func (s *RegistrationSnapshot) Name() string { return "bastion-registrations" }

// Start loads the rows once and runs the loop.
func (s *RegistrationSnapshot) Start(ctx context.Context) error {
	s.refresh(ctx)
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(SnapshotReload)
		defer t.Stop()
		for {
			select {
			case <-lctx.Done():
				return
			case <-t.C:
			case <-s.kick:
			}
			s.refresh(lctx)
		}
	}()
	return nil
}

// Stop ends the loop.
func (s *RegistrationSnapshot) Stop(context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	return nil
}

// refresh reads the rows and replaces the snapshot; a failed read keeps the last one.
func (s *RegistrationSnapshot) refresh(ctx context.Context) {
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := s.load(lctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("the Bastion registrations were not read; the last snapshot stays", "error", err)
		}
		return
	}
	s.set(rows)
}

// set replaces the snapshot with rows, reports a registration whose handover turned on after the first load, and logs a network
// that a load brought and that overlaps server.trusted_proxies.
func (s *RegistrationSnapshot) set(rows []SnapshotRow) {
	next := &snapshotState{handover: make(map[string]bool, len(rows)), egress: make(map[string][]netip.Prefix, len(rows))}
	for _, row := range rows {
		if row.Handover {
			next.handover[row.ID] = true
		}
		if len(row.Egress) > 0 {
			next.egress[row.ID] = row.Egress
		}
	}
	prev := s.state.Swap(next)
	first := !s.loaded.Swap(true)
	s.warnOverlaps(prev, next, first)
	if first || maps.Equal(prev.handover, next.handover) {
		return
	}
	for id := range next.handover {
		if !prev.handover[id] {
			if f := s.switchedOn.Load(); f != nil && *f != nil {
				(*f)()
			}
			return
		}
	}
}

// warnOverlaps logs each network of a registration that overlaps server.trusted_proxies, once when it first appears in the snapshot.
func (s *RegistrationSnapshot) warnOverlaps(prev, next *snapshotState, first bool) {
	t := s.trusted.Load()
	if t == nil || len(*t) == 0 {
		return
	}
	for id, nets := range next.egress {
		for _, n := range nets {
			if !first && slices.Contains(prev.egress[id], n) {
				continue
			}
			for _, p := range *t {
				if n.Overlaps(p) {
					s.log.Warn("a network a Bastion connects from is also in server.trusted_proxies: the forwarded headers from it count for every request, not only for that Bastion's",
						"bastion_id", id, "bastion_egress", n.String(), "trusted_proxy", p.String())
				}
			}
		}
	}
}
