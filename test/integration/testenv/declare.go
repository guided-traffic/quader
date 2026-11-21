package testenv

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// Declare runs `quader migrate up` in the test process with authz.role_bindings set
// to bindings (R-AUTHZ-010, Q-AUTHZ-170): the migration runner with nothing to
// apply, then the step that applies the declared role bindings, exactly as the
// command composes it (app.DeclaredState). Replicas that run pick the change up
// from the change log. The error is the one `quader migrate` would exit with.
func (s *Stack) Declare(bindings ...config.AuthzRoleBinding) error {
	s.tb.Helper()
	return DeclareRoleBindings(s.pg, bindings...)
}

// DeclareRoleBindings is [Stack.Declare] for a suite with an environment of its
// own: db is the configuration the installation's replicas open (the session host
// is the direct server also behind PgBouncer).
func DeclareRoleBindings(db config.Database, bindings ...config.AuthzRoleBinding) error {
	return declareWith(db, func(cfg *config.Config) { cfg.Authz.RoleBindings = bindings })
}

// DeclareWith runs `quader migrate up` as [Stack.Declare] does, with the declared state of the configuration that edit sets: the trust
// anchors of signatures.anchors, for example (signatures.md R-SIG-018).
func (s *Stack) DeclareWith(edit func(*config.Config)) error {
	s.tb.Helper()
	return declareWith(s.pg, edit)
}

func declareWith(db config.Database, edit func(*config.Config)) error {
	cfg := &config.Config{Database: db}
	edit(cfg)
	cfg.Cluster = config.Cluster{EpochPollInterval: time.Second, EpochMaxStaleness: 20 * time.Second, ChangeRetention: time.Hour}
	cfg.Health.WatchdogTimeout = 30 * time.Second
	cfg.Audit = config.Audit{AsyncBuffer: 100, RetentionDays: 365}
	cfg.Auth.BreakGlass.Username = "admin"
	// quader migrate applies ci.issuers on every run (R-CI-001, Q-CI-16), also with none declared; its issuer
	// service registers its caches with the SafetyTTL that config.Load fills from the defaults (R-ARCH-018).
	cfg.CI.Mappings.CacheSafetyTTL = 10 * time.Minute
	cfg.CI.SigningKeys.CacheSafetyTTL = 10 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := postgres.Migrate(ctx, db, postgres.MigrateOptions{
		Instance: "it-migrate", Version: "0.1.0", Log: Quiet(), AuditRetentionDays: &cfg.Audit.RetentionDays,
		Migrate:  config.Migrate{AdvisoryLockWait: 20 * time.Second, DDLLockTimeout: 2 * time.Second, DDLLockRetries: 3},
		Declared: app.DeclaredState(cfg, "it-migrate", Quiet()),
	})
	return err
}
