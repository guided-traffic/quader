//go:build integration

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// buildQuader compiles cmd/quader once per test into a temporary directory.
func buildQuader(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "quader")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/guided-traffic/quader/cmd/quader")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func runQuader(t *testing.T, bin string, env []string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = testenv.ChildEnv(env...)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint // exec returns it unwrapped
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, o.String(), e.String()
}

func TestMigrateCommand(t *testing.T) {
	st := getStack(t)
	bin := buildQuader(t)
	name, admin := st.newDatabase(t)
	cfg := st.config(t, name)
	cfgFile := filepath.Join(t.TempDir(), "quader.yaml")
	if err := os.WriteFile(cfgFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"QUADER_DATABASE_HOST=" + st.host, "QUADER_DATABASE_PORT=" + itoa(st.port), "QUADER_DATABASE_NAME=" + name,
		"QUADER_DATABASE_USERNAME_FILE=" + cfg.UsernameFile, "QUADER_DATABASE_PASSWORD_FILE=" + cfg.PasswordFile,
		"QUADER_DATABASE_SSL_MODE=verify-full", "QUADER_DATABASE_SSL_ROOT_CERT_FILE=" + testenv.CAFile(t),
		"QUADER_DATABASE_PREFLIGHT_ALLOW_SUPERUSER=true", "QUADER_POD_NAME=quader-migrate-abc",
		"QUADER_VALKEY_SENTINEL_ADDRESSES=[\"s1:26379\"]", "QUADER_VALKEY_SENTINEL_PRIMARY_NAME=quader",
		"QUADER_STORAGE_CONNECTIONS=[{\"bucket\":\"blobs\"}]",
		"QUADER_SECRETS_ENCRYPTION_KEY_FILES=[\"/keys/2026-10.key\"]", "QUADER_SECRETS_ACTIVE_KEY_ID=2026-10",
	}

	t.Run("R-HA-019 quader migrate up creates the schema, prints a JSON summary and exits 0", func(t *testing.T) {
		code, stdout, stderr := runQuader(t, bin, env, "migrate", "up", "--config", cfgFile)
		if code != 0 {
			t.Fatalf("exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
		}
		var res struct {
			Applied       []map[string]any `json:"applied"`
			SchemaApplied int              `json:"schemaApplied"`
			NothingToDo   bool             `json:"nothingToDo"`
		}
		if err := json.Unmarshal([]byte(stdout), &res); err != nil || len(res.Applied) == 0 || res.NothingToDo {
			t.Fatalf("summary %q: %v", stdout, err)
		}
		if !strings.Contains(stderr, `"msg":"applied step"`) {
			t.Fatalf("log lines missing: %s", stderr)
		}
	})

	t.Run("R-HA-019 a second run exits 0 with nothing to do", func(t *testing.T) {
		code, stdout, _ := runQuader(t, bin, env, "migrate", "up", "--config", cfgFile)
		if code != 0 || !strings.Contains(stdout, `"nothingToDo": true`) {
			t.Fatalf("exit %d stdout %s", code, stdout)
		}
	})

	t.Run("R-ARCH-026 quader migrate status lists applied steps", func(t *testing.T) {
		code, stdout, _ := runQuader(t, bin, env, "migrate", "status", "--config", cfgFile)
		if code != 0 || !strings.Contains(stdout, `"00001_foundation"`) || !strings.Contains(stdout, `"servable": true`) {
			t.Fatalf("exit %d stdout %s", code, stdout)
		}
	})

	t.Run("R-BAK-004 quader migrate up fails naming a table that no package of the binary registered, and passes again once it is gone", func(t *testing.T) {
		if _, err := admin.Exec(context.Background(), `CREATE TABLE quader.rogue_table (id bigint PRIMARY KEY)`); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := runQuader(t, bin, env, "migrate", "up", "--config", cfgFile)
		if code != 3 || !strings.Contains(stderr, "rogue_table") || !strings.Contains(stderr, "R-BAK-004") {
			t.Fatalf("exit %d stderr %s", code, stderr)
		}
		if _, err := admin.Exec(context.Background(), `DROP TABLE quader.rogue_table`); err != nil {
			t.Fatal(err)
		}
		if code, _, stderr := runQuader(t, bin, env, "migrate", "up", "--config", cfgFile); code != 0 {
			t.Fatalf("after the drop: exit %d stderr %s", code, stderr)
		}
	})

	t.Run("R-HA-019 a half-restored database makes quader migrate up exit 3", func(t *testing.T) {
		if _, err := admin.Exec(context.Background(), `INSERT INTO quader.system_state (key, value) VALUES ('restore_in_progress', '"20261028T020000Z-7f3a"')`); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := runQuader(t, bin, env, "migrate", "up", "--config", cfgFile)
		if code != 3 || !strings.Contains(stderr, "20261028T020000Z-7f3a") {
			t.Fatalf("exit %d stderr %s", code, stderr)
		}
	})
}
