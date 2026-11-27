package app

import (
	"context"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// bastionBaseURLs reads the base URLs a Bastion registration's deployments reported within the last 24 hours, and whether it
// distributes a repository, for client-setup with bastion=<name> (R-BAS-007, R-BAS-135) and the builder of its public catalogue.
type bastionBaseURLs struct{ db *postgres.DB }

var _ v1.BastionBaseURLs = bastionBaseURLs{}

// ReportedBaseURLs implements v1.BastionBaseURLs.
func (b bastionBaseURLs) ReportedBaseURLs(ctx context.Context, name string) ([]string, bool, error) {
	var (
		urls       []string
		registered bool
	)
	err := b.db.Read(ctx, func(d q.DBTX) error {
		urls, registered = nil, false
		qs := q.New(d)
		rows, err := qs.BastionGet(ctx, name)
		if err != nil || len(rows) == 0 {
			return err
		}
		registered = true
		urls, err = qs.BastionRecentBaseURLs(ctx, name)
		return err
	})
	return urls, registered, err
}

// Distributes implements v1.BastionBaseURLs: whether the registration distributes the repository (R-BAS-134).
func (b bastionBaseURLs) Distributes(ctx context.Context, name string, repositoryID int64) (bool, error) {
	var ok bool
	err := b.db.Read(ctx, func(d q.DBTX) error {
		var err error
		ok, err = q.New(d).BastionDistributesRepository(ctx, q.BastionDistributesRepositoryParams{Name: name, RepositoryID: repositoryID})
		return err
	})
	return ok, err
}
