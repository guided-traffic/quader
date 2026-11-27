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

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The replica's snapshot of the Bastion registrations: what a request of a Bastion is decided on without a statement (R-ARCH-027).
// It holds each registration's switch of the handover per blob (R-BAS-071), which the serve point reads, the networks its Bastion
// connects from (R-BAS-017, Q-BAS-334), which the recognition reads, and its configuration (R-BAS-093, Q-BAS-320), which the
// handshake answers and the intake reads a rule's figures from, with its disable (R-BAS-133), which the recognition refuses a disabled
// Bastion's work by and whose change to hard cuts its forwards in flight. A change of a member announces its change area (bastion_handover,
// bastion_egress, bastion_configuration; a rule of a Bastion's place or a profile the area limits), every replica reads the rows
// again within the bound of R-ARCH-018 (2 s), and a lost change is read again after [SnapshotReload]. One statement reads a
// registration's revision with the rules of its place, so the configuration a handshake sends is of one moment.

// SnapshotReload is the snapshot's safety period: a lost change is read again after it.
const SnapshotReload = time.Minute

// SnapshotRow is one registration as the snapshot holds it.
type SnapshotRow struct {
	// ID is the registration's principal ID.
	ID       string
	Handover bool
	Egress   []netip.Prefix
	// Disabled is the way the registration is disabled (R-BAS-133), empty while it works.
	Disabled iface.DisableMode
	// Configuration is what the handshake sends (R-BAS-093); ConfigurationErr why the row's configuration could not be built, which
	// the handshake answers instead of a guess.
	Configuration    iface.Configuration
	ConfigurationErr error
}

// snapshotState is one loaded set of rows, replaced whole.
type snapshotState struct {
	handover      map[string]bool
	egress        map[string][]netip.Prefix
	disabled      map[string]iface.DisableMode
	configuration map[string]SnapshotRow
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
	hard       atomic.Pointer[func(bastionID string)]
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
			out = append(out, snapshotRowOf(row))
		}
		return out, err
	}, r, log)
}

// snapshotRowOf builds a registration's row of the snapshot, its configuration included.
func snapshotRowOf(row q.BastionSnapshotRow) SnapshotRow {
	out := SnapshotRow{ID: row.ID, Handover: row.Handover, Egress: egressOf(row.Egress), Disabled: iface.DisableMode(row.Disabled)}
	rules, err := placeRulesOf(row.Rules)
	if err == nil {
		l := limitsOf(limitsColumns{
			mode: row.LimitsMode, preset: row.LimitsPreset, flood: row.LimitsFlood, slots: row.LimitsSlots,
			throughput: row.LimitsThroughput, ceiling: row.LimitsCeiling,
		})
		out.Configuration, err = Configuration(row.Revision, iface.Switches{Handover: row.Handover, GUI: row.Gui, Docs: row.Docs, HideRepositories: row.HideRepositories}, l, rules)
		if err == nil && out.Disabled != "" {
			d := out.Disabled
			out.Configuration.Disabled = &d
		}
	}
	out.ConfigurationErr = err
	return out
}

// newRegistrationSnapshot is the snapshot over a load function (the tests give their own).
func newRegistrationSnapshot(load func(context.Context) ([]SnapshotRow, error), r *cache.Registry, log *slog.Logger) *RegistrationSnapshot {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &RegistrationSnapshot{load: load, log: log, kick: make(chan struct{}, 1)}
	s.state.Store(&snapshotState{
		handover: map[string]bool{}, egress: map[string][]netip.Prefix{}, disabled: map[string]iface.DisableMode{},
		configuration: map[string]SnapshotRow{},
	})
	if r != nil {
		cache.Register(r, "bastion_registrations", cache.Options[struct{}, struct{}]{
			MaxEntries: 1, SafetyTTL: SnapshotReload,
			Areas:    []string{cluster.AreaBastionHandover, cluster.AreaBastionEgress, cluster.AreaBastionConfiguration, cluster.AreaLimits},
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

// OnHardDisabled sets what runs when a reload finds a registration disabled hard that the snapshot before it did not hold disabled hard
// (R-BAS-133): the replica cuts that Bastion's forwards in flight ([InFlight.Cancel]). The first load calls it too, for a registration
// it finds disabled hard: no forward is in flight before it, so it cuts nothing.
func (s *RegistrationSnapshot) OnHardDisabled(f func(bastionID string)) { s.hard.Store(&f) }

// Disabled returns the way the registration bastionID is disabled in the replica's snapshot (R-BAS-133), empty while it works and
// for a registration the snapshot does not hold.
func (s *RegistrationSnapshot) Disabled(bastionID string) iface.DisableMode {
	if s == nil {
		return ""
	}
	return s.state.Load().disabled[bastionID]
}

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

// Configuration returns the configuration of the registration bastionID the snapshot holds (R-BAS-093): ok is false for a
// registration the snapshot does not hold yet (created within the bound of R-ARCH-018), and err says why a held row's
// configuration could not be built.
func (s *RegistrationSnapshot) Configuration(bastionID string) (c iface.Configuration, ok bool, err error) {
	if s == nil {
		return iface.Configuration{}, false, nil
	}
	row, ok := s.state.Load().configuration[bastionID]
	return row.Configuration, ok, row.ConfigurationErr
}

// RuleFigures are the effective figures of a rule of the registration bastionID's place, for the utilisation of a reported usage row
// (R-LIM-045); false for a rule the snapshot does not hold.
func (s *RegistrationSnapshot) RuleFigures(bastionID, rule string) (limits.Figures, bool) {
	c, ok, err := s.Configuration(bastionID)
	if !ok || err != nil {
		return limits.Figures{}, false
	}
	f, ok := c.Limits.Place.Rules[rule]
	if !ok {
		return limits.Figures{}, false
	}
	v := func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	}
	return limits.Figures{
		v(f.RequestsPerSecond), v(f.RequestsBurst), v(f.RequestsPerHour), v(f.CachedBytesPerSecond), v(f.CachedBytesPerHour),
		v(f.UncachedBytesPerSecond), v(f.UncachedBytesPerHour), v(f.UpstreamRequestsPerHour), v(f.ParallelTransfers), v(f.BytesPerDay),
		v(f.RefusedPerHour),
	}, true
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

// set replaces the snapshot with rows, reports a registration whose handover turned on after the first load and one that turned
// disabled hard, and logs a network that a load brought and that overlaps server.trusted_proxies.
func (s *RegistrationSnapshot) set(rows []SnapshotRow) {
	next := &snapshotState{
		handover: make(map[string]bool, len(rows)), egress: make(map[string][]netip.Prefix, len(rows)),
		disabled: make(map[string]iface.DisableMode, len(rows)), configuration: make(map[string]SnapshotRow, len(rows)),
	}
	for _, row := range rows {
		next.configuration[row.ID] = row
		if row.Disabled != "" {
			next.disabled[row.ID] = row.Disabled
		}
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
	if f := s.hard.Load(); f != nil && *f != nil {
		for id, mode := range next.disabled {
			if mode == iface.DisableHard && prev.disabled[id] != iface.DisableHard {
				(*f)(id)
			}
		}
	}
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

// ReadConfiguration reads the configuration of the registration bastionID from the database (R-BAS-093): the handshake asks it for a
// registration its replica's snapshot does not hold yet. ok is false for a registration that does not exist.
func ReadConfiguration(ctx context.Context, db Reader, bastionID string) (c iface.Configuration, ok bool, err error) {
	var rows []q.BastionSnapshotRow
	if err := db.Read(ctx, func(tx q.DBTX) error {
		var err error
		rows, err = q.New(tx).BastionSnapshot(ctx)
		return err
	}); err != nil {
		return iface.Configuration{}, false, err
	}
	for _, row := range rows {
		if row.ID == bastionID {
			r := snapshotRowOf(row)
			return r.Configuration, true, r.ConfigurationErr
		}
	}
	return iface.Configuration{}, false, nil
}
