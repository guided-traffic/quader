package app

import (
	"context"

	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/packages"
)

// AreaManagementHost is the Host the management operations a format decides write through, on the roles with the management API:
// the format host where the role builds one, else (the role api) a Host of its own (R-ARCH-001, Q-ARCH-301, Q-ARCH-1070).
const AreaManagementHost Area = "management-host"

// managementHostArea is the Host of the management operations.
type managementHostArea struct{ host formats.Host }

// wireManagementHost takes the format host where the role builds one (web, all), and builds the Host of the role api, its one host
// there: the files seam's serve check reads the stored marks
// of an item through it (R-ARCH-008 "Serve-time check", Q-ARCH-301), and an operation of the management API that a format decides
// writes through it (a deprecation, R-API-085). It has the item store, the package store, the decision point and the virtual engine
// of the role as its Resolver, so a format asks what the caller may read of a repository (formats.CallerVersions) as it does on a
// filling role (Q-ARCH-1070). It has no remote engine and no dispatcher: a call that fetches from an upstream or serves a protocol
// route answers with the internal error of a Host built without them, and no route of the role makes one.
func (a *App) wireManagementHost(_ context.Context, in infra, repo repositoryArea, az authzArea, fm formatsArea, fa filesArea, ve virtualEngineArea, fh formatHostArea) (managementHostArea, error) {
	if a.sub.Has(AreaFormatHost) {
		// web and all: the format host is the role's one host, bound to every format.
		if err := requireInputs(AreaManagementHost, given("format host", fh.host)); err != nil {
			return managementHostArea{}, err
		}
		return managementHostArea{host: fh.host}, nil
	}
	if err := requireInputs(AreaManagementHost, given("item store", fa.items), given("decision point", az.engine),
		given("repository reader", repo.reader), given("virtual engine", ve.engine)); err != nil {
		return managementHostArea{}, err
	}
	pkgs, err := packages.NewStore(in.db)
	if err != nil {
		return managementHostArea{}, err
	}
	resolver, err := formathost.NewResolver(ve.engine, a.readScoper(), nil)
	if err != nil {
		return managementHostArea{}, err
	}
	host, err := formathost.New(formathost.Options{
		Runner: in.db, Decider: az.engine, Items: fa.items, Packages: pkgs, Deps: fm.deps, Repos: repo.reader, Resolver: resolver,
		DownloadBuffer: int(a.cfg.Storage.Download.BufferSize), Log: a.log.With("component", "formats"),
	})
	if err != nil {
		return managementHostArea{}, err
	}
	a.host.set(host)
	return managementHostArea{host: host}, nil
}
