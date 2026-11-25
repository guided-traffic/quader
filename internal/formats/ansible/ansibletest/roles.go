package ansibletest

import (
	"context"
	"slices"
	"strconv"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
)

// The facts of the roles a remote read (R-ANS-056), in memory. A role counts as packaged once MarkPackaged named it, which stands for the
// package row the host's writer keeps.

type roleKey struct{ repo, id int64 }

func (s *Store) roleInit() {
	if s.roles == nil {
		s.roles, s.packaged = map[roleKey]rows.Role{}, map[roleKey]bool{}
	}
}

// PutRoles keeps the facts and leaves a role's README and its state alone.
func (s *Store) PutRoles(_ context.Context, _ formats.Tx, repoID int64, roles []rows.Role) error {
	defer s.lock()()
	s.roleInit()
	for _, r := range roles {
		k := roleKey{repoID, r.ID}
		old, ok := s.roles[k]
		r.Tags, r.Dependencies = slices.Clone(r.Tags), slices.Clone(r.Dependencies)
		r.Readme = rows.RoleReadme{State: rows.RoleReadmeNone}
		if ok {
			r.Readme = old.Readme
		}
		s.roles[k] = r
	}
	return nil
}

// Role reads the facts without the README's HTML.
func (s *Store) Role(_ context.Context, _ formats.Tx, repoID, id int64) (rows.Role, bool, error) {
	defer s.lock()()
	s.roleInit()
	r, ok := s.roles[roleKey{repoID, id}]
	r.Readme.HTML = ""
	r.Packaged = s.packaged[roleKey{repoID, id}]
	return r, ok, nil
}

// SetRoleReadme records the README and its state; the commit is the role's.
func (s *Store) SetRoleReadme(_ context.Context, _ formats.Tx, repoID, id int64, rd rows.RoleReadme) error {
	defer s.lock()()
	s.roleInit()
	k := roleKey{repoID, id}
	r, ok := s.roles[k]
	if !ok {
		return nil
	}
	rd.Commit = r.Commit
	if rd.State == rows.RoleReadmeQueued {
		rd.Commit = r.Readme.Commit
	}
	r.Readme = rd
	s.roles[k] = r
	return nil
}

// EnqueueRoleReadme marks the README queued and records the job.
func (s *Store) EnqueueRoleReadme(_ context.Context, _ formats.Tx, repoID, id int64) error {
	defer s.lock()()
	s.roleInit()
	k := roleKey{repoID, id}
	if r, ok := s.roles[k]; ok {
		r.Readme.State = rows.RoleReadmeQueued
		s.roles[k] = r
	}
	s.RoleJobs = append(s.RoleJobs, roleKey{repoID, id}.String())
	return nil
}

// MarkPackaged says that the repository has the role's package.
func (s *Store) MarkPackaged(repoID, id int64) {
	defer s.lock()()
	s.roleInit()
	s.packaged[roleKey{repoID, id}] = true
}

// RoleFacts reads the facts of a role with its README's HTML, for a test.
func (s *Store) RoleFacts(repoID, id int64) (rows.Role, bool) {
	defer s.lock()()
	s.roleInit()
	r, ok := s.roles[roleKey{repoID, id}]
	return r, ok
}

// ViewRoles answers nothing: the package screens of a role are tested against PostgreSQL.
func (s *Store) ViewRoles(context.Context, []int64, bool) ([]rows.RoleView, error) { return nil, nil }

func (k roleKey) String() string {
	return strconv.FormatInt(k.repo, 10) + "/role/" + strconv.FormatInt(k.id, 10)
}
