package app

import (
	"context"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// bastionBaseURLs reads the base URL a Bastion registration reported at its most recent
// handshake, and whether it distributes a repository, for client-setup with bastion=<name> (R-BAS-007).
type bastionBaseURLs struct{ db *postgres.DB }

var _ v1.BastionBaseURLs = bastionBaseURLs{}

// ReportedBaseURL implements v1.BastionBaseURLs.
func (b bastionBaseURLs) ReportedBaseURL(ctx context.Context, name string) (string, bool, error) {
	var (
		base       string
		registered bool
	)
	err := b.db.Read(ctx, func(d q.DBTX) error {
		base, registered = "", false
		qs := q.New(d)
		rows, err := qs.BastionGet(ctx, name)
		if err != nil || len(rows) == 0 {
			return err
		}
		registered = true
		urls, err := qs.BastionLastBaseURL(ctx, name)
		if err != nil {
			return err
		}
		if len(urls) > 0 {
			base = urls[0]
		}
		return nil
	})
	return base, registered, err
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
