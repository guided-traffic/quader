package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/ci"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/proxy/invalidation"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// DeclaredState is the step of `quader migrate` that applies the state the system
// configuration declares, after the schema steps and under the migration lock
// (postgres.MigrateOptions.Declared): the role bindings of authz.role_bindings
// (R-AUTHZ-010, Q-AUTHZ-170). The writers are the ones a replica uses, built over
// the runner's one connection: the change log writes the authz change in the
// transaction, so every running replica rebuilds its grant index from the poll
// (R-AUTHZ-028, no Valkey needed); the audit writer records the events with the
// actor system:configuration in the same transaction.
//
// instance is the Job's pod name, stored as the replica of the audit events.
func DeclaredState(cfg *config.Config, instance string, log *slog.Logger) func(context.Context, *postgres.MigrationSession) error {
	return func(ctx context.Context, s *postgres.MigrationSession) error {
		changes, err := cluster.NewChanges(cluster.ChangesOptions{
			DB: s.DBTX(), Config: cfg.Cluster, WatchdogTimeout: cfg.Health.WatchdogTimeout, Log: log,
		})
		if err != nil {
			return err
		}
		recorder, err := audit.New(audit.Options{DB: s, Replica: instance, Config: cfg.Audit, Log: log, AppLog: log})
		if err != nil {
			return err
		}
		groups := principal.NewManager(principal.ManagerOptions{
			DB: s, Notifier: changes, Recorder: recorder, BreakGlassName: cfg.Auth.BreakGlass.Username,
		})
		svc := authz.NewService(authz.ServiceOptions{DB: s, Changes: changes, Recorder: recorder})
		out, err := svc.ApplyDeclared(ctx, cfg.Authz.RoleBindings, groups)
		if err != nil {
			return err
		}
		log.Info("role bindings of the configuration applied", "declared", len(cfg.Authz.RoleBindings),
			"bound", out.Bound, "taken_over", out.Adopted, "unbound", out.Unbound, "groups_created", out.GroupsCreated)
		if len(out.Suppressed) > 0 {
			log.Warn("declared role bindings were not applied: a journal replay removed them after the restore point; bind them through the API to confirm",
				"bindings", out.Suppressed)
		}
		if err := applyDeclaredCIIssuers(ctx, s, changes, recorder, cfg, log); err != nil {
			return err
		}
		if err := applySignatureAnchors(ctx, s, changes, recorder, cfg, log); err != nil {
			return err
		}
		return ReconcileUpstream(ctx, s, changes, recorder, cfg, catalog.Default(), log)
	}
}

// UpstreamStep is the reconcile of the project upstream as a step of its own (postgres.MigrateOptions.Declared), with the writers
// DeclaredState builds: what a test runs with a catalog of its own (R-REPO-067).
func UpstreamStep(cfg *config.Config, cat *catalog.Registry, instance string, log *slog.Logger) func(context.Context, *postgres.MigrationSession) error {
	return func(ctx context.Context, s *postgres.MigrationSession) error {
		changes, err := cluster.NewChanges(cluster.ChangesOptions{
			DB: s.DBTX(), Config: cfg.Cluster, WatchdogTimeout: cfg.Health.WatchdogTimeout, Log: log,
		})
		if err != nil {
			return err
		}
		recorder, err := audit.New(audit.Options{DB: s, Replica: instance, Config: cfg.Audit, Log: log, AppLog: log})
		if err != nil {
			return err
		}
		return ReconcileUpstream(ctx, s, changes, recorder, cfg, cat, log)
	}
}

// ReconcileUpstream brings the well-known repositories of the project upstream in step with the catalog of the binary, in one
// transaction of the migration session (R-REPO-067, Q-REPO-374): missing ones are created, the product's address follows the catalog
// unless an administrator overrode it, a preset the catalog no longer declares is retired and a system repository it no longer
// declares removed (Q-REPO-490). The writers are the ones a replica uses, over the runner's one connection: the repository service
// with the actor system:catalog, the change log that notes repo_config, the invalidation of a remote whose upstream moved
// (R-REPO-011), and the job queue that takes a retired repository's purge and a removed one's deletion. It contacts no upstream. A catalog that declares a key the project holds as a repository of another format fails the run
// and writes nothing (step 6).
func ReconcileUpstream(ctx context.Context, s *postgres.MigrationSession, changes *cluster.Changes, recorder audit.Recorder,
	cfg *config.Config, cat *catalog.Registry, log *slog.Logger,
) error {
	reg := jobs.NewRegistry()
	// The purge of a retired preset and the deletion of a removed system repository (Q-REPO-490) run on a worker; the migration only
	// enqueues them.
	reg.RegisterKind(repository.UpstreamPurgeJobKind, repository.UpstreamPurgeJobSpec(func(context.Context, *jobs.Run) error {
		return errors.New("upstream-purge runs on a worker, not in quader migrate")
	}))
	reg.RegisterKind(repository.DeleteJobKind, repository.DeleteJobSpec(func(context.Context, *jobs.Run) error {
		return errors.New("repository-delete runs on a worker, not in quader migrate")
	}))
	svc, err := repository.NewService(repository.Options{
		Runner: s, Catalog: cat, Recorder: recorder, Notifier: changes, Log: log, PublicAvailable: func() bool { return false },
		Jobs: jobs.NewQueue(jobs.QueueOptions{DB: s.DBTX(), Registry: reg, Log: log}), DeleteBatch: cfg.Storage.Delete.BatchSize,
	})
	if err != nil {
		return err
	}
	inv, err := invalidation.NewService(invalidation.ServiceOptions{Runner: s, Notifier: changes, Recorder: recorder})
	if err != nil {
		return err
	}
	svc.Bind(repository.RemoteDeps{OnUpstreamChanged: func(ctx context.Context, tx meta.Tx, c repository.Caller, r repository.Repository) error {
		_, err := inv.InvalidateTx(ctx, tx, c, r, "", invalidation.ScopeAll)
		return err
	}})
	var res wellknown.Result
	if err := s.InTx(ctx, func(tx meta.Tx) error {
		var err error
		res, err = wellknown.Reconcile(ctx, svc.UpstreamStore(tx), wellknown.Declared(cat))
		return err
	}); err != nil {
		return fmt.Errorf("the project upstream: %w", err)
	}
	log.Info("the well-known repositories of the project upstream reconciled with the catalog",
		"created", res.Keys(wellknown.OpCreate), "written", res.Keys(wellknown.OpWrite), "retired", res.Keys(wellknown.OpRetire),
		"removed", res.Keys(wellknown.OpRemove), "declared_again", res.Keys(wellknown.OpUnretire), "override_cleared", res.Keys(wellknown.OpClearOverride))
	return applyUpstreamAccounts(ctx, s, svc, cfg, log)
}

// applyUpstreamAccounts applies repositories.upstream_accounts after the reconcile, so a preset the release adds exists, in one
// transaction with the actor system:configuration (R-REPO-077): a declared key's stored account is removed and the declaration marked,
// a mark whose key is gone is cleared, and a key that names no remote repository of the project upstream fails the run before any pod
// rolls. The Job reads only the configuration: it never sees the files or their values.
func applyUpstreamAccounts(ctx context.Context, s *postgres.MigrationSession, svc *repository.Service, cfg *config.Config, log *slog.Logger) error {
	var res repository.UpstreamAccountsResult
	if err := s.InTx(ctx, func(tx meta.Tx) error {
		var err error
		res, err = svc.ApplyUpstreamAccounts(ctx, tx, repository.Caller{Actor: authz.ConfigurationActor()}, configuredAccounts(cfg))
		return err
	}); err != nil {
		return fmt.Errorf("the accounts of the project upstream: %w", err)
	}
	log.Info("the accounts of the project upstream applied from the configuration",
		"declared", len(cfg.Repositories.UpstreamAccounts), "applied", res.Declared, "removed", res.Removed)
	for _, key := range slices.Sorted(maps.Keys(res.Overridden)) {
		log.Warn("a declared account goes to the address an administrator set for the repository, not to Quader's",
			"repo", "upstream/"+key, "hosts", res.Overridden[key])
	}
	return nil
}

// configuredAccounts is repositories.upstream_accounts as the repository service and the upstream clients read it (R-REPO-077).
func configuredAccounts(cfg *config.Config) repository.ConfiguredAccounts {
	out := make(repository.ConfiguredAccounts, len(cfg.Repositories.UpstreamAccounts))
	for key, a := range cfg.Repositories.UpstreamAccounts {
		out[key] = repository.ConfiguredAccount{
			AuthMode: a.EffectiveAuthMode(), UsernameFile: a.UsernameFile, PasswordFile: a.PasswordFile, SecretName: a.SecretName,
		}
	}
	return out
}

// applyDeclaredCIIssuers applies ci.issuers in one transaction of the migration session (R-CI-001,
// Q-CI-16): an issuer is created or replaced by name with source configuration, and a configuration
// issuer that is no longer declared is disabled with removedFromConfiguration. No replica writes
// them, so replicas of two releases during a rolling update never overwrite each other.
//
// The issuer service is the one a replica uses, over the runner's one connection, so the API and the
// configuration agree on the defaults and the rules, and the change log writes the ci_mappings change in
// the transaction. It runs always, also with an empty list: the issuers that were removed from the
// configuration are disabled then, and that is the one case where nothing is declared and something
// must be written.
func applyDeclaredCIIssuers(ctx context.Context, s *postgres.MigrationSession, changes *cluster.Changes, recorder audit.Recorder, cfg *config.Config, log *slog.Logger) error {
	declared := ci.DeclaredFromConfigList(cfg.CI.Issuers)
	// The revoker of the replicas, over the runner's one connection: an issuer the configuration no longer
	// declares is disabled and its unexpired tokens are revoked in the same transaction, the change of the area
	// auth with them, so every running replica refuses them within auth.revocation_bound (R-CI-017).
	revoker, err := ci.NewRevoker(ci.RevokerOptions{DB: s, Changes: changes, Recorder: recorder})
	if err != nil {
		return fmt.Errorf("ci.issuers: %w", err)
	}
	issuers, err := ci.New(ci.Options{
		DB: s, Changes: changes, Recorder: recorder, Config: cfg.CI, OutboundConfig: cfg.Proxy.Outbound,
		EpochPollInterval: cfg.Cluster.EpochPollInterval, BaseURL: cfg.Server.BaseURL,
		// The mapping store is never asked: the migration disables an issuer only when the configuration no
		// longer declares it, which deletes no mapping; the revoker above revokes its tokens in that transaction.
		Revoker: revoker, Log: log,
	})
	if err != nil {
		return fmt.Errorf("ci.issuers: %w", err)
	}
	if err := s.InTx(ctx, func(tx meta.Tx) error { return issuers.ApplyDeclared(ctx, tx, declared) }); err != nil {
		return fmt.Errorf("ci.issuers: %w", err)
	}
	log.Info("CI issuers of the configuration applied", "declared", len(declared))
	return nil
}
