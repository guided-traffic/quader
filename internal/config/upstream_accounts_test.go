package config

import (
	"strings"
	"testing"
)

// TestR_REPO_077_TheDeclaredUpstreamAccountsAreCheckedInTheirFormOnly: an entry is decoded with basic as the default authMode, from the
// file and from the environment; the key, the authMode, the files a mode needs and refuses, and clean absolute paths are refused with
// the key named. No file is read at load: an entry whose files do not exist loads.
func TestR_REPO_077_TheDeclaredUpstreamAccountsAreCheckedInTheirFormOnly(t *testing.T) {
	load := func(file string, env ...string) (*Loaded, error) {
		t.Helper()
		return Load(Options{File: "/c.yaml", ReadFile: fakeFS(map[string]string{"/c.yaml": file}), Environ: minimalEnv(env...), Command: CommandMigrate})
	}
	l, err := load("repositories:\n  upstream_accounts:\n    docker-hub:\n      username_file: /does/not/exist/username\n" +
		"      password_file: /does/not/exist/password\n      secret_name: quader-upstream-docker-hub\n")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Config.Repositories.UpstreamAccounts["docker-hub"]
	if a.EffectiveAuthMode() != UpstreamAccountBasic || a.UsernameFile != "/does/not/exist/username" || a.SecretName != "quader-upstream-docker-hub" {
		t.Errorf("decoded %+v", a)
	}
	l, err = load("", `QUADER_REPOSITORIES_UPSTREAM_ACCOUNTS={"npmjs":{"auth_mode":"bearer","password_file":"/run/npm/token"}}`)
	if err != nil {
		t.Fatalf("from the environment: %v", err)
	}
	if a := l.Config.Repositories.UpstreamAccounts["npmjs"]; a.EffectiveAuthMode() != UpstreamAccountBearer || a.PasswordFile != "/run/npm/token" {
		t.Errorf("from the environment: %+v", a)
	}
	if l, err = load(""); err != nil || len(l.Config.Repositories.UpstreamAccounts) != 0 {
		t.Errorf("default: %v %+v", err, l)
	}

	const files = "username_file: /u, password_file: /p"
	for _, tc := range []struct{ name, entry, want string }{
		{"a key with capitals", "Docker_Hub: {" + files + "}", "repositories.upstream_accounts.Docker_Hub the key must match"},
		{"a key of 65 characters", strings.Repeat("a", 65) + ": {" + files + "}", "at most 64 characters"},
		{"an unknown authMode", "docker-hub: {auth_mode: none, " + files + "}", `repositories.upstream_accounts.docker-hub.auth_mode "none" is neither basic nor bearer`},
		{"basic without a username file", "docker-hub: {password_file: /p}", "repositories.upstream_accounts.docker-hub.username_file is required with auth_mode basic"},
		{"bearer with a username file", "npmjs: {auth_mode: bearer, " + files + "}", "repositories.upstream_accounts.npmjs.username_file must not be set with auth_mode bearer"},
		{"no password file", "docker-hub: {username_file: /u}", "repositories.upstream_accounts.docker-hub.password_file is required"},
		{"a relative path", "docker-hub: {username_file: u, password_file: /p}", `repositories.upstream_accounts.docker-hub.username_file "u" must be a clean absolute path`},
		{"a path that is not clean", "docker-hub: {username_file: /u, password_file: /a/../p}", "repositories.upstream_accounts.docker-hub.password_file"},
		{"an unknown member", "docker-hub: {" + files + ", token_file: /t}", "token_file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load("repositories:\n  upstream_accounts:\n    " + tc.entry + "\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v, want %q", err, tc.want)
			}
		})
	}
}
