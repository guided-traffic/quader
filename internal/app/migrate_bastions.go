package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// maxBastionTokenFile bounds the file of a declared Bastion token: the token is 57 characters, the rest is the room of a line break
// an editor or a Secret tool adds. A larger file is not a token, and nothing more of it is read.
const maxBastionTokenFile = 1024

// BastionsStep is the application of bastions.declared as a step of its own (postgres.MigrateOptions.Declared), with the writers
// DeclaredState builds: what a test runs without the rest of the declared state (R-BAS-127).
func BastionsStep(cfg *config.Config, instance string, log *slog.Logger) func(context.Context, *postgres.MigrationSession) error {
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
		return applyDeclaredBastions(ctx, s, changes, recorder, cfg, log)
	}
}

// applyDeclaredBastions applies bastions.declared in one transaction of the migration session, with the actor system:configuration
// (R-BAS-127): it reads each entry's token file - the one place that reads it - and checks the value and the networks before any
// write, so a file that is missing or not a Bastion token stops the run with the entry named and writes nothing. It runs always, also
// with no entry: a registration the configuration no longer declares loses its mark and its declared token then. The registration
// and token services are the ones a replica uses, over the runner's one connection, so the change log tells every replica's token
// cache of a revoked token in the transaction.
func applyDeclaredBastions(ctx context.Context, s *postgres.MigrationSession, changes *cluster.Changes, recorder audit.Recorder, cfg *config.Config, log *slog.Logger) error {
	declared, err := declaredBastions(cfg.Bastions.Declared)
	if err != nil {
		return err
	}
	caches, err := cache.NewRegistry(nil, nil)
	if err != nil {
		return err
	}
	// The run validates no token, so the service's cache is never filled; it still needs a safety period to be registered, which a
	// configuration of a test that sets no token section does not give.
	tokenCfg := cfg.Auth.Tokens
	if tokenCfg.CacheTTL <= 0 {
		tokenCfg.CacheTTL = time.Minute
	}
	tokens := token.NewService(token.ServiceOptions{
		DB: s, Notifier: changes, Recorder: recorder, Cache: caches, Gate: principal.Gate{Bound: cfg.Auth.RevocationBound},
		Tokens: tokenCfg, Scope: TokenScopeOptions(cfg), Log: log,
	})
	regs := bastionmain.NewRegistrations(bastionmain.RegistrationOptions{
		DB: s, Notifier: changes, Recorder: recorder, Tokens: tokens, Max: cfg.Bastions.MaxRegistrations,
	})
	var res bastionmain.DeclaredResult
	if err := s.InTx(ctx, func(tx meta.Tx) error {
		var err error
		res, err = regs.ApplyDeclared(ctx, tx, principal.Caller{Actor: authz.ConfigurationActor()}, declared)
		return err
	}); err != nil {
		return fmt.Errorf("bastions.declared: %w", err)
	}
	log.Info("the Bastion registrations of the configuration applied", "declared", len(declared), "created", res.Created,
		"marked", res.Marked, "updated", res.Updated, "tokens_stored", res.TokensStored, "tokens_taken", res.TokensTaken,
		"tokens_replaced", res.TokensReplaced)
	for _, name := range res.Released {
		log.Warn("a Bastion registration is no longer declared in the configuration: its declared token is revoked, the registration stays; delete it on the Bastions page if it is no longer needed",
			"bastion", name)
	}
	return nil
}

// declaredBastions turns bastions.declared into the service's entries, in name order: the networks by bastionmain's rules and the
// token read from its file and checked; the error names the entry and never the value.
func declaredBastions(in map[string]config.DeclaredBastion) ([]bastionmain.Declared, error) {
	out := make([]bastionmain.Declared, 0, len(in))
	var problems []error
	for _, name := range slices.Sorted(maps.Keys(in)) {
		d := in[name]
		egress, err := bastionmain.ParseEgress("/egress", d.Egress)
		if err != nil {
			problems = append(problems, fmt.Errorf("bastions.declared.%s.egress: %w", name, err))
			continue
		}
		tok, err := readDeclaredBastionToken(d.TokenFile)
		if err != nil {
			problems = append(problems, fmt.Errorf("bastions.declared.%s.token_file: %w", name, err))
			continue
		}
		out = append(out, bastionmain.Declared{Name: name, Description: d.Description, Egress: egress, Token: tok})
	}
	return out, errors.Join(problems...)
}

// readDeclaredBastionToken reads a declared token's file as the Bastion reads its own (mainclient.ReadTokenFile): the value with the
// trailing line break of an editor or a Secret tool removed; any other white space fails the check. The error names the path and the
// token's hint at most, never the value.
func readDeclaredBastionToken(path string) (token.DeclaredBastionToken, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the path is bastions.declared.<name>.token_file, set by the chart
	if err != nil {
		return token.DeclaredBastionToken{}, fmt.Errorf("cannot read %s: %w", path, unwrapPathError(err))
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxBastionTokenFile+1))
	if err != nil {
		return token.DeclaredBastionToken{}, fmt.Errorf("cannot read %s: %w", path, unwrapPathError(err))
	}
	if len(b) > maxBastionTokenFile {
		return token.DeclaredBastionToken{}, fmt.Errorf("%s is larger than %d bytes: it does not hold a Bastion token", path, maxBastionTokenFile)
	}
	raw := strings.TrimRight(string(b), "\r\n")
	if raw == "" {
		return token.DeclaredBastionToken{}, fmt.Errorf("%s is empty: put a Bastion token in it (the Bastions page's Issue token, or quader admin bastion token generate)", path)
	}
	tok, err := token.ParseDeclaredBastion(raw)
	if err != nil {
		return token.DeclaredBastionToken{}, fmt.Errorf("%s: %w; make one with the Bastions page's Issue token or quader admin bastion token generate", path, err)
	}
	return tok, nil
}

// unwrapPathError is the cause of an *os.PathError without the path, which the caller names once.
func unwrapPathError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
