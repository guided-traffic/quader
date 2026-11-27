//go:build integration

package bastionreg

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// declare runs `quader migrate up` in the test process with bastions.declared set to the entries, the step the command composes
// into its declared state (app.BastionsStep); the error is the one the command would exit with.
func (e *env) declare(entries map[string]config.DeclaredBastion) error {
	e.t.Helper()
	cfg := &config.Config{Database: e.cfg}
	cfg.Bastions = config.Bastions{MaxRegistrations: 16, Declared: entries}
	cfg.Auth.Tokens = tokenConfig()
	cfg.Auth.RevocationBound = revocationBound
	cfg.Cluster = config.Cluster{EpochPollInterval: time.Second, EpochMaxStaleness: 20 * time.Second, ChangeRetention: time.Hour}
	cfg.Health.WatchdogTimeout = 30 * time.Second
	cfg.Audit = config.Audit{AsyncBuffer: 100, RetentionDays: 365}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := postgres.Migrate(ctx, e.cfg, postgres.MigrateOptions{
		Instance: "it-migrate", Version: "0.1.0", Log: quiet, AuditRetentionDays: &cfg.Audit.RetentionDays,
		Migrate:  config.Migrate{AdvisoryLockWait: 20 * time.Second, DDLLockTimeout: 2 * time.Second, DDLLockRetries: 3},
		Declared: app.BastionsStep(cfg, "it-migrate", quiet),
	})
	return err
}

// tokenFile writes a token file as the chart mounts it, with the line break a Secret tool adds, and returns its path.
func tokenFile(t *testing.T, dir, name, value string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestR_BAS_127_ADeclaredRegistrationSurvivesAFreshDatabase: on a fresh database quader migrate creates the declared registration
// with its description, its networks and the token of the file, marked; the Bastion holding that value is accepted by the authenticator
// of the handshake, with no expiry; a second run writes nothing; the card's API reads it declared with the Secret's name and its
// token declared without an expiry, and refuses a new description, new networks, a token issued beside the declared one (R-BAS-133) and
// the deletion (409 managed_by_config) while it takes the switches; a new value in the file replaces the token (the old one refused
// within the revocation bound), a value issued on the screen before the declaration is taken as the declared one, the declared token is
// invalidated on the API as the last resort and its revoked value stops the next run (R-BAS-133), and a run without the declaration
// releases the registration: unmarked, its declared token revoked, the registration and its issued tokens kept. About 3 s, bytes.
func TestR_BAS_127_ADeclaredRegistrationSurvivesAFreshDatabase(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	first, _ := token.Generate(token.TypeBST)
	entry := config.DeclaredBastion{
		Description: "Partner access", Egress: []string{"10.252.0.0/16"}, TokenFile: tokenFile(t, dir, "first", first),
		SecretName: "quader-bastion-token",
	}
	if err := e.declare(map[string]config.DeclaredBastion{"partners": entry}); err != nil {
		t.Fatalf("migrate on a fresh database: %v", err)
	}
	r, c := e.newCast("web-1", replicaOpts{declaredSecrets: map[string]string{"partners": "quader-bastion-token"}})
	ctx := context.Background()

	t.Run("R-BAS-127 the registration and its token exist after migrate on a fresh database", func(t *testing.T) {
		tok, err := r.auth.Authenticate(ctx, first)
		if err != nil {
			t.Fatalf("the declared token is refused: %v", err)
		}
		if tok.Name != "partners" || !tok.ExpiresAt.IsZero() {
			t.Errorf("authenticated %+v, want partners without an expiry", tok)
		}
		if n := e.number(`SELECT count(*) FROM audit_event WHERE action = 'bastion.created' AND actor_name = 'configuration' AND details->>'source' = 'configuration'`); n != 1 {
			t.Errorf("bastion.created by the configuration: %d", n)
		}
		if n := e.number(`SELECT count(*) FROM audit_event WHERE action = 'token.created' AND details->>'createdVia' = 'configuration'`); n != 1 {
			t.Errorf("token.created by the configuration: %d", n)
		}
		if n := e.number(`SELECT count(*) FROM audit_event WHERE details::text LIKE '%' || $1 || '%'`, first[8:]); n != 0 {
			t.Error("an audit event holds the token's value")
		}
	})

	t.Run("R-BAS-127 a second run writes nothing", func(t *testing.T) {
		before := e.number(`SELECT count(*) FROM audit_event`)
		rev := e.number(`SELECT revision FROM principal WHERE kind = 'bastion' AND name = 'partners'`)
		if err := e.declare(map[string]config.DeclaredBastion{"partners": entry}); err != nil {
			t.Fatal(err)
		}
		if after := e.number(`SELECT count(*) FROM audit_event`); after != before {
			t.Errorf("the second run recorded %d events", after-before)
		}
		if got := e.number(`SELECT revision FROM principal WHERE kind = 'bastion' AND name = 'partners'`); got != rev {
			t.Errorf("the revision moved from %d to %d", rev, got)
		}
	})

	t.Run("R-BAS-127 the card's API marks it declared and keeps the declared members", func(t *testing.T) {
		g := r.call("GET", bastions+"/partners", c.admin, nil)
		want(t, g, http.StatusOK, "")
		doc := g.json(t)
		d, _ := doc["declared"].(map[string]any)
		if d == nil || d["secretName"] != "quader-bastion-token" || d["since"] == nil || doc["description"] != "Partner access" ||
			fmt.Sprint(doc["egress"]) != "[10.252.0.0/16]" {
			t.Fatalf("registration %v", doc)
		}
		toks, _ := doc["tokens"].([]any)
		if len(toks) != 1 || toks[0].(map[string]any)["declared"] != true || toks[0].(map[string]any)["expiresAt"] != nil {
			t.Fatalf("tokens %v", toks)
		}
		tokenID := toks[0].(map[string]any)["id"].(string)
		etag := g.Header.Get("ETag")
		want(t, r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"description": "edited"}, "If-Match", etag), http.StatusConflict, "managed_by_config")
		want(t, r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"egress": []string{"203.0.113.0/28"}}, "If-Match", etag), http.StatusConflict, "managed_by_config")
		if tokenID == "" {
			t.Fatal("the declared token has no ID")
		}
		want(t, r.call("POST", bastions+"/partners/tokens", c.admin, map[string]any{}), http.StatusConflict, "managed_by_config")
		want(t, r.call("DELETE", bastions+"/partners", c.admin, nil), http.StatusConflict, "managed_by_config")
		same := r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"description": "Partner access", "switches": map[string]any{"gui": true}}, "If-Match", etag)
		want(t, same, http.StatusOK, "")
		if sw := same.json(t)["switches"].(map[string]any); sw["gui"] != true {
			t.Errorf("the switch an administrator sets is not taken: %v", sw)
		}
	})

	second, _ := token.Generate(token.TypeBST)
	t.Run("R-BAS-127 a new value in the file replaces the declared token", func(t *testing.T) {
		entry.TokenFile = tokenFile(t, dir, "second", second)
		if err := e.declare(map[string]config.DeclaredBastion{"partners": entry}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.auth.Authenticate(ctx, second); err != nil {
			t.Fatalf("the new value is refused: %v", err)
		}
		waitFor(t, "the old value refused", revocationBound+2*time.Second, func() bool {
			_, err := r.auth.Authenticate(ctx, first)
			return err != nil
		})
		if n := e.number(`SELECT count(*) FROM audit_event WHERE action = 'token.revoked' AND details->>'reason' = 'rotated'`); n != 1 {
			t.Errorf("token.revoked rotated: %d", n)
		}
		if err := e.declare(map[string]config.DeclaredBastion{"partners": {TokenFile: tokenFile(t, dir, "back", first), Egress: entry.Egress, Description: entry.Description}}); err == nil ||
			!strings.Contains(err.Error(), "bastions.declared.partners") || !strings.Contains(err.Error(), "revoked") || strings.Contains(err.Error(), first[8:]) {
			t.Errorf("the revoked value back in the file: %v", err)
		}
	})

	var kept token.Created
	var declaredValue string
	t.Run("R-BAS-127 a value issued on the screen before the declaration becomes the declared token", func(t *testing.T) {
		if _, err := r.regs.CreateToken(ctx, systemCaller, "partners", bastionmain.TokenRequest{Via: "admin-cli"}); err == nil ||
			!strings.Contains(err.Error(), "bastions.declared") {
			t.Fatalf("a token issued beside the declared one: %v, want 409 managed_by_config (R-BAS-133)", err)
		}
		if err := e.declare(nil); err != nil {
			t.Fatal(err)
		}
		var err error
		if kept, err = r.regs.CreateToken(ctx, systemCaller, "partners", bastionmain.TokenRequest{Via: "admin-cli"}); err != nil {
			t.Fatal(err)
		}
		issued, err := r.regs.CreateToken(ctx, systemCaller, "partners", bastionmain.TokenRequest{Via: "admin-cli"})
		if err != nil {
			t.Fatal(err)
		}
		entry.TokenFile, declaredValue = tokenFile(t, dir, "issued", issued.Secret), issued.Secret
		if err := e.declare(map[string]config.DeclaredBastion{"partners": entry}); err != nil {
			t.Fatal(err)
		}
		if got := e.text(`SELECT coalesce(declared_token::text, '') FROM bastion b JOIN principal p ON p.id = b.principal_id WHERE p.name = 'partners'`); got != issued.Record.ID {
			t.Errorf("the declared token is %q, want the issued %q", got, issued.Record.ID)
		}
		if n := e.number(`SELECT count(*) FROM api_token WHERE id = $1::uuid AND expires_at IS NULL AND revoked_at IS NULL AND created_via = 'configuration'`, issued.Record.ID); n != 1 {
			t.Error("the issued token taken as the declared one still expires, or was revoked")
		}
	})

	t.Run("R-BAS-133 the declared token is invalidated as the last resort, and its value stops the next run", func(t *testing.T) {
		id := e.text(`SELECT coalesce(declared_token::text, '') FROM bastion b JOIN principal p ON p.id = b.principal_id WHERE p.name = 'partners'`)
		want(t, r.call("DELETE", bastions+"/partners/tokens/"+id, c.admin, nil), http.StatusNoContent, "")
		waitFor(t, "the invalidated declared token refused", revocationBound+2*time.Second, func() bool {
			_, err := r.auth.Authenticate(ctx, declaredValue)
			return err != nil
		})
		if err := e.declare(map[string]config.DeclaredBastion{"partners": entry}); err == nil || !strings.Contains(err.Error(), "revoked") {
			t.Errorf("the invalidated value still in the file: %v", err)
		}
	})

	t.Run("R-BAS-127 a run without the declaration releases the registration and keeps it", func(t *testing.T) {
		if err := e.declare(nil); err != nil {
			t.Fatal(err)
		}
		if n := e.number(`SELECT count(*) FROM bastion b JOIN principal p ON p.id = b.principal_id WHERE p.name = 'partners' AND b.declared_at IS NULL AND b.declared_token IS NULL`); n != 1 {
			t.Fatal("the registration is gone or still marked")
		}
		// The release before the value issued on the screen was declared, and this one.
		if n := e.number(`SELECT count(*) FROM audit_event WHERE action = $1 AND details->>'reason' = 'configuration_removed'`, audit.EventBastionUpdated); n != 2 {
			t.Errorf("bastion.updated configuration_removed: %d", n)
		}
		if _, err := r.auth.Authenticate(ctx, kept.Secret); err != nil {
			t.Errorf("a token issued on the screen is refused after the release: %v", err)
		}
		want(t, r.call("DELETE", bastions+"/partners", c.admin, nil), http.StatusNoContent, "")
	})
}
