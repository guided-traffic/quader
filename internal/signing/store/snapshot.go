package store

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/signing"
)

// snapshotTTL is the safety period of the snapshot (R-ARCH-018): a change the change log missed is read again after it.
const snapshotTTL = time.Minute

// Snapshot is a replica's view of the identities (R-SIG-059): read from the database once and kept until the change area
// signing_identities moves or the safety period passes, so every replica answers the same keys, trusted root and chain within
// R-ARCH-018's bound. The switch's lock is the one coordination of signing (R-SIG-048); the snapshot decides nothing that needs it.
type Snapshot struct {
	st *Store
	c  *cache.Cache[struct{}, []signing.Identity]
}

// NewSnapshot registers the snapshot in the replica's cache registry; a nil registry reads the database on every call (a test).
func NewSnapshot(st *Store, r *cache.Registry) *Snapshot {
	s := &Snapshot{st: st}
	if r != nil {
		s.c = cache.Register(r, "signing_identities", cache.Options[struct{}, []signing.Identity]{
			MaxEntries: 1, SafetyTTL: snapshotTTL, Areas: []string{cluster.AreaSigningIdentities},
		})
	}
	return s
}

// Identities answers every identity, oldest first.
func (s *Snapshot) Identities(ctx context.Context) ([]signing.Identity, error) {
	if s.c == nil {
		return s.st.Identities(ctx)
	}
	return s.c.Load(ctx, struct{}{}, s.st.Identities)
}

// Current answers the current identity; ok is false when none is.
func (s *Snapshot) Current(ctx context.Context) (signing.Identity, bool, error) {
	ids, err := s.Identities(ctx)
	if err != nil {
		return signing.Identity{}, false, err
	}
	for _, i := range ids {
		if i.State == signing.StateCurrent {
			return i, true, nil
		}
	}
	return signing.Identity{}, false, nil
}
