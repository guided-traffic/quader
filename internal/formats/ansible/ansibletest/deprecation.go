package ansibletest

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
)

// The deprecations of collections (R-ANS-060), by repository and name; a missing entry is a collection that is not deprecated.

// CollectionDeprecation reads the deprecation of a collection.
func (s *Store) CollectionDeprecation(_ context.Context, _ formats.Tx, repoID int64, name string) (rows.CollectionDeprecation, bool, error) {
	defer s.lock()()
	d, ok := s.deprecations[key{repo: repoID, name: name}]
	return d, ok, nil
}

// SetCollectionDeprecation sets, changes or clears a collection's deprecation and logs the write's audit context.
func (s *Store) SetCollectionDeprecation(_ context.Context, tx formats.Tx, repoID int64, name string, d *rows.CollectionDeprecation, a rows.Audit) (bool, error) {
	defer s.lock()()
	k := key{repo: repoID, name: name}
	before, had := s.deprecations[k]
	switch {
	case d == nil && !had, d != nil && had && before.Message == d.Message:
		return false, nil
	case d == nil:
		delete(s.deprecations, k)
	default:
		if s.deprecations == nil {
			s.deprecations = map[key]rows.CollectionDeprecation{}
		}
		s.deprecations[k] = *d
	}
	s.Audits = append(s.Audits, a)
	if tx != nil {
		tx.OnRollback(func() {
			defer s.lock()()
			if had {
				s.deprecations[k] = before
			} else {
				delete(s.deprecations, k)
			}
		})
	}
	return true, nil
}

// SetUpstreamDeprecated records a remote's flag of a collection.
func (s *Store) SetUpstreamDeprecated(_ context.Context, _ formats.Tx, repoID int64, name string, deprecated bool) error {
	defer s.lock()()
	k := key{repo: repoID, name: name}
	if !deprecated {
		delete(s.deprecations, k)
		return nil
	}
	if s.deprecations == nil {
		s.deprecations = map[key]rows.CollectionDeprecation{}
	}
	if _, ok := s.deprecations[k]; !ok {
		s.deprecations[k] = rows.CollectionDeprecation{}
	}
	return nil
}

// ViewDeprecated finds nothing: the twin has no generic version ids (see ViewSummaries).
func (s *Store) ViewDeprecated(context.Context, []string, int) ([]int64, error) { return nil, nil }
