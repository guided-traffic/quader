package bastionmain

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The switch of the handover per blob to a Bastion (R-BAS-071, Q-BAS-232): a member of the registration, off by default,
// kept in bastion.handover (00110). The serve point decides a forward from the replica's snapshot of the registrations whose
// handover is on, which the change area bastion_handover refreshes within the bound of R-ARCH-018 (2 s), so a download costs no
// statement for it (R-ARCH-027). Switching it off is journaled; the replay below switches it off again after a restore.

// HandoverReload is the snapshot's safety period: a lost change is read again after it.
const HandoverReload = time.Minute

// HandoverSwitches is the replica's snapshot of the registrations whose handover is on. Before its first load, and while a
// load fails, it keeps what it had: off for every registration before the first. It is safe for concurrent use.
type HandoverSwitches struct {
	load func(ctx context.Context) ([]string, error)
	log  *slog.Logger

	on         atomic.Pointer[map[string]bool]
	loaded     atomic.Bool
	switchedOn atomic.Pointer[func()]

	kick   chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewHandoverSwitches builds the snapshot over db and registers its cache on r, which a change of the area bastion_handover
// reloads; a nil registry reloads it at the safety period only.
func NewHandoverSwitches(db Reader, r *cache.Registry, log *slog.Logger) *HandoverSwitches {
	return newHandoverSwitches(func(ctx context.Context) ([]string, error) {
		var ids []string
		err := db.Read(ctx, func(tx q.DBTX) error {
			var err error
			ids, err = q.New(tx).BastionHandoverOn(ctx)
			return err
		})
		return ids, err
	}, r, log)
}

// newHandoverSwitches is the snapshot over a load function (the tests give their own).
func newHandoverSwitches(load func(context.Context) ([]string, error), r *cache.Registry, log *slog.Logger) *HandoverSwitches {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &HandoverSwitches{load: load, log: log, kick: make(chan struct{}, 1)}
	none := map[string]bool{}
	s.on.Store(&none)
	if r != nil {
		cache.Register(r, "bastion_handover", cache.Options[struct{}, struct{}]{
			MaxEntries: 1, SafetyTTL: HandoverReload, Areas: []string{cluster.AreaBastionHandover},
			OnChange: func(*cache.Cache[struct{}, struct{}], string, int, json.RawMessage) { s.Reload() },
			OnFlush:  func(string) { s.Reload() },
		})
	}
	return s
}

// OnSwitchedOn sets what runs when a reload finds a registration's handover on that the snapshot before it had off: the replica
// runs its connections' link checks again (storage.md R-STOR-096). The first load calls nothing, since the link checks run at
// every start.
func (s *HandoverSwitches) OnSwitchedOn(f func()) { s.switchedOn.Store(&f) }

// On reports whether the handover of the registration with the principal ID bastionID is on in the replica's snapshot.
func (s *HandoverSwitches) On(bastionID string) bool {
	if s == nil {
		return false
	}
	return (*s.on.Load())[bastionID]
}

// Reload asks the loop to read the rows now.
func (s *HandoverSwitches) Reload() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Name implements the composition root's Component.
func (s *HandoverSwitches) Name() string { return "bastion-handover" }

// Start loads the rows once and runs the loop.
func (s *HandoverSwitches) Start(ctx context.Context) error {
	s.refresh(ctx)
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(HandoverReload)
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
func (s *HandoverSwitches) Stop(context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	return nil
}

// refresh reads the rows and replaces the snapshot; a failed read keeps the last one.
func (s *HandoverSwitches) refresh(ctx context.Context) {
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ids, err := s.load(lctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("the switches of the Bastions' handover were not read; the last snapshot stays", "error", err)
		}
		return
	}
	s.set(ids)
}

// set replaces the snapshot with the registrations ids and reports a registration turned on after the first load.
func (s *HandoverSwitches) set(ids []string) {
	next := make(map[string]bool, len(ids))
	for _, id := range ids {
		next[id] = true
	}
	prev := s.on.Swap(&next)
	first := !s.loaded.Swap(true)
	if first || maps.Equal(*prev, next) {
		return
	}
	for id := range next {
		if !(*prev)[id] {
			if f := s.switchedOn.Load(); f != nil && *f != nil {
				(*f)()
			}
			return
		}
	}
}

// handoverEntry is the journal entry of bastion.handover.disabled: the registration's principal ID and, for display, its name.
func handoverEntry(id, name string) json.RawMessage { return registrationEntry(id, name) }

// The replay of a handover switched off (R-BAS-071, R-HA-054): a restore to t brings a registration's member back as it was at
// t, and the replay switches it off again when it was switched off after t, so a restore never hands a Bastion more than the
// administrator last allowed. It writes no audit event (Q-AUTHZ-232); every replica's snapshot follows the change area.
func init() {
	journal.Register(journal.Handler{Domain: "bastion", Events: []string{audit.EventBastionHandoverDisabled}, Apply: replayHandoverOff})
}

func replayHandoverOff(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e struct {
		PrincipalID string `json:"principalId"`
		Name        string `json:"name"`
	}
	if !o.Into(&e) || !journal.IsUUID(e.PrincipalID) {
		return o.Malformed("principalId is not an ID"), nil
	}
	qs := principal.Queries(tx)
	rows, err := qs.IdentityPrincipalByID(ctx, e.PrincipalID)
	if err != nil {
		return journal.Result{}, err
	}
	if len(rows) == 0 || rows[0].Kind != string(principal.KindBastion) {
		return o.Result(journal.SubjectUnknown, e.PrincipalID), nil
	}
	name := rows[0].Name
	changed, err := qs.BastionHandoverOff(ctx, e.PrincipalID)
	if err != nil {
		return journal.Result{}, err
	}
	if len(changed) == 0 {
		return o.Result(journal.AlreadyInEffect, name+".handover"), nil
	}
	if err := env.Notify(tx, cluster.AreaBastionHandover, 1, map[string]string{"bastion": e.PrincipalID}); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, name+".handover"), nil
}
