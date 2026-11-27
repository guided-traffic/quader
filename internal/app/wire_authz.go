package app

import (
	"context"
	"net/http"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/project"
	"github.com/guided-traffic/quader/internal/visibility"
)

// withMinEpoch wraps the authentication stage: once a request authenticated, the
// header Quader-Authz-Min-Epoch is read into the principal (R-AUTHZ-032). The
// order matters: a request with an invalid credential got its 401 from the stage
// before the header is looked at, and an anonymous request ignores the header, so
// an anonymous caller cannot probe the epoch.
func withMinEpoch(next func(*http.Request) (httpserver.Principal, error)) func(*http.Request) (httpserver.Principal, error) {
	return func(r *http.Request) (httpserver.Principal, error) {
		p, err := next(r)
		if err != nil || p.Anonymous {
			return p, err
		}
		n, err := authz.ParseMinEpoch(r.Header.Get(authz.HeaderMinEpoch))
		if err != nil {
			return httpserver.Principal{}, err
		}
		p.MinEpoch = n
		return p, nil
	}
}

// wireAuthz is the authz area: the decision point (R-AUTHZ-028) and the policy
// service. The grant index is built from PostgreSQL before the client listener
// opens, and the engine follows the change areas authz and repo_config. Valkey
// is not an input; the engine reaches it only through the change log.
func (a *App) wireAuthz(ctx context.Context, in infra, repo repositoryArea, id identityArea, ex exposureArea) (authzArea, error) {
	if err := requireInputs(AreaAuthz, given("repository reader", repo.reader), given("principal store", id.principals),
		given("exposure ceiling", ex.ceiling)); err != nil {
		return authzArea{}, err
	}
	cfg := a.cfg
	eng, err := authz.New(authz.Options{
		Source:            authz.NewPostgresSource(in.db, cfg.Authz.Cache.MaxStaleness),
		Changes:           in.changes,
		Config:            cfg.Authz,
		Audit:             cfg.Audit,
		EpochPollInterval: cfg.Cluster.EpochPollInterval,
		RetryBudget:       cfg.Database.Retry.MaxElapsed,
		Repositories:      repo.reader,       // the lookup and catch-up of a repository the index lacks (R-API-052)
		Ceiling:           ex.ceiling,        // the exposure area (R-EP-013, wire_exposure.go)
		Formats:           catalog.Default(), // the name rule that folds the names and patterns of a format (R-AUTHZ-005)
		Recorder:          in.rec,
		Replica:           a.id.Instance,
		Log:               a.log.With("component", "authz"),
		Metrics:           a.tele.metrics.Registry(),
	})
	if err != nil {
		return authzArea{}, err
	}
	if err := a.startComponent(ctx, eng); err != nil {
		return authzArea{}, err
	}
	svc := authz.NewService(authz.ServiceOptions{
		DB: in.db, Changes: in.changes, Principals: id.principals, Repositories: repo.reader, Recorder: in.rec,
		Formats: catalog.Default(),
	})
	projects := project.New(project.Options{
		DB: in.db, Changes: in.changes, Principals: id.principals, Repositories: repo.reader, Policies: svc, Recorder: in.rec,
		PublicAvailable: a.publicAvailable,
	})
	levels := visibility.New(visibility.Options{DB: in.db, Recorder: in.rec, PublicAvailable: a.publicAvailable})
	return authzArea{engine: eng, service: svc, projects: projects, visibility: levels}, nil
}

// publicAvailable reports whether the default endpoint admits requests without a credential
// (endpoints.default.auth.anonymous, R-EP-010): the condition for setting a level to public (R-AUTHZ-033 rule 5).
func (a *App) publicAvailable() bool {
	for _, e := range a.cfg.Server.ResolveEndpoints() {
		if e.Default {
			return e.Anonymous
		}
	}
	return false
}

// publicContent reports whether an anonymous visitor reads anything through the endpoint of the request: the artifacts of
// some repository are `public` on the grant index and the endpoint admits requests without a credential (R-AUTH-037,
// R-AUTHZ-003). False on a replica without the authz area.
func (a *App) publicContent(ctx context.Context) bool {
	e := part[authzArea](a.parts).engine
	if e == nil {
		return false
	}
	if idx := e.Index(); idx == nil || !idx.PublicContent() {
		return false
	}
	ep := httpserver.EndpointFrom(ctx)
	return ep != nil && ep.AnonymousAllowed()
}

// anonymousReadsThrough reports whether the Bastion registration (its principal ID) distributes some repository whose artifacts
// are public on the grant index: the handshake's anonymousReads of that registration, before the default endpoint's own rule
// (R-BAS-010, Q-BAS-522). False on a replica without the authz area.
func (a *App) anonymousReadsThrough(bastionID string) bool {
	e := part[authzArea](a.parts).engine
	if e == nil {
		return false
	}
	idx := e.Index()
	return idx != nil && idx.PublicDistributed(bastionID)
}

// decider returns the decision point the request paths decide through, nil on a
// replica without the authz area (the shell profile). The nil is explicit
// because a nil *authz.Engine in the interface would not be nil.
func (a *App) decider() authz.Decider {
	e := part[authzArea](a.parts).engine
	if e == nil {
		return nil
	}
	return e
}

// readScoper is the decision point as the unaudited read scope of a listing (authz.ReadScoper);
// nil on a replica without the authz area.
func (a *App) readScoper() authz.ReadScoper {
	e := part[authzArea](a.parts).engine
	if e == nil {
		return nil
	}
	return e
}

// authzServices are the services the permission operations of the management API
// use; a member is nil on a replica without the authz area.
func (a *App) authzServices() v1.AuthzServices {
	az := part[authzArea](a.parts)
	return v1.AuthzServices{Policies: az.service, Engine: az.engine, Owners: part[ownershipArea](a.parts).service, Projects: az.projects, Visibility: az.visibility}
}
