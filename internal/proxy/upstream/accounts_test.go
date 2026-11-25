package upstream

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// syncBuffer is a log sink the clients write to from the request's goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// upstreamRepo is a repository of the built-in project upstream, where a declared account applies.
func upstreamRepo(id int64, key string, version int64) repository.Repository {
	return repository.Repository{
		ID: strconv.FormatInt(id, 10), Name: qname.Repo{Project: "upstream", Key: key}, Class: repository.Remote, Format: "npmlike", Version: version,
	}
}

// TestR_REPO_077_ADeclaredAccountIsReadFromItsFilesAtUse: the account of repositories.upstream_accounts is read from its two files and
// sent whatever the document stores; a rotated file takes effect after the refresh interval on the same client and moves the token
// cache's key; a missing, empty or malformed file fails the repository with upstream_secret_unavailable before anything is sent; the
// error and the log name the path and never the content. Unit tier, a fake clock, a stub upstream on loopback, bytes of data.
func TestR_REPO_077_ADeclaredAccountIsReadFromItsFilesAtUse(t *testing.T) {
	srv, got := stubRegistry(t)
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var clock atomic.Int64
	clock.Store(time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC).UnixNano())
	advance := func(d time.Duration) { clock.Add(int64(d)) }
	logs := &syncBuffer{}
	accounts := repository.ConfiguredAccounts{
		"docker-hub": {AuthMode: repository.AuthBasic, UsernameFile: filepath.Join(dir, "username"), PasswordFile: filepath.Join(dir, "password"), SecretName: "quader-upstream-docker-hub"},
		"npmjs":      {AuthMode: repository.AuthBearer, PasswordFile: filepath.Join(dir, "npm-token")},
	}
	f := newFixture(t, fixtureOpts{
		servers: []*httptest.Server{srv}, now: func() time.Time { return time.Unix(0, clock.Load()) },
		accounts: accounts, log: slog.New(slog.NewTextHandler(logs, nil)),
	})
	// The document stores no account: the apply of quader migrate removed it (R-REPO-077).
	s := settings(srv.URL+"/", repository.AuthNone)
	basic := func(user, pw string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pw))
	}

	write("username", "dummy-user-7f3a\n")
	write("password", "dummy-token-91c2\n")
	t.Run("the two files make the Basic header, and the client's settings carry the declared authMode", func(t *testing.T) {
		cl, err := f.clients.For(t.Context(), upstreamRepo(31, "docker-hub", 1), s)
		if err != nil {
			t.Fatal(err)
		}
		mustGet(t, cl, srv.URL+"/-/ping")
		if r := got.last(t); r.auth != basic("dummy-user-7f3a", "dummy-token-91c2") {
			t.Fatalf("Authorization = %q", r.auth)
		}
		if cl.Settings().AuthMode != repository.AuthBasic {
			t.Fatalf("authMode = %q", cl.Settings().AuthMode)
		}
	})

	t.Run("a rotated file takes effect after the refresh interval, on the same client, and moves the fingerprint", func(t *testing.T) {
		cl, err := f.clients.For(t.Context(), upstreamRepo(31, "docker-hub", 1), s)
		if err != nil {
			t.Fatal(err)
		}
		before := cl.CredentialFingerprint()
		write("password", "dummy-token-rotated\n")
		mustGet(t, cl, srv.URL+"/-/ping")
		if r := got.last(t); r.auth != basic("dummy-user-7f3a", "dummy-token-91c2") {
			t.Fatalf("the file was read again before the interval: %q", r.auth)
		}
		advance(RefreshInterval + time.Second)
		mustGet(t, cl, srv.URL+"/-/ping")
		if r := got.last(t); r.auth != basic("dummy-user-7f3a", "dummy-token-rotated") {
			t.Fatalf("Authorization = %q", r.auth)
		}
		if cl.CredentialFingerprint() == before {
			t.Fatal("the fingerprint did not move with the rotation: a token issued under the old token would be reused")
		}
		if again, _ := f.clients.For(t.Context(), upstreamRepo(31, "docker-hub", 1), s); again != cl {
			t.Fatal("a rotated file built a new client")
		}
	})

	t.Run("a bearer account sends the token alone", func(t *testing.T) {
		write("npm-token", "dummy-npm-5d1e\n")
		cl, err := f.clients.For(t.Context(), upstreamRepo(32, "npmjs", 1), s)
		if err != nil {
			t.Fatal(err)
		}
		mustGet(t, cl, srv.URL+"/-/ping")
		if r := got.last(t); r.auth != "Bearer dummy-npm-5d1e" {
			t.Fatalf("Authorization = %q", r.auth)
		}
	})

	t.Run("a repository of another project with the same key sends no account", func(t *testing.T) {
		cl, err := f.clients.For(t.Context(), repo(33, "docker-hub", 1), s)
		if err != nil {
			t.Fatal(err)
		}
		mustGet(t, cl, srv.URL+"/-/ping")
		if r := got.last(t); r.auth != "" {
			t.Fatalf("Authorization = %q", r.auth)
		}
	})

	for i, tc := range []struct {
		name, file, content string
		remove              bool
	}{
		{name: "a missing password file", file: "password", remove: true},
		{name: "an empty password file", file: "password", content: "\n"},
		{name: "a username with a colon", file: "username", content: "dummy:user-c0l0n\n"},
		{name: "a password with a NUL", file: "password", content: "dummy-nul\x00-token\n"},
	} {
		t.Run(tc.name+" fails the repository with upstream_secret_unavailable and sends nothing", func(t *testing.T) {
			write("username", "dummy-user-7f3a\n")
			write("password", "dummy-token-91c2\n")
			if tc.remove {
				if err := os.Remove(filepath.Join(dir, tc.file)); err != nil {
					t.Fatal(err)
				}
			} else {
				write(tc.file, tc.content)
			}
			before := len(got.all())
			_, err := f.clients.For(t.Context(), upstreamRepo(31, "docker-hub", int64(10+i)), s)
			e, ok := errs.As(err)
			if !ok || e.Code != errs.CodeUpstreamSecretUnavailable || e.Status() != 502 {
				t.Fatalf("err = %v", err)
			}
			if len(got.all()) != before {
				t.Fatal("a request went out without its credential")
			}
			if strings.Contains(e.Detail, dir) {
				t.Fatalf("the client-facing detail names the file: %s", e.Detail)
			}
			if !strings.Contains(logs.String(), filepath.Join(dir, tc.file)) {
				t.Fatalf("the log does not name the file's path: %s", logs.String())
			}
		})
	}
	for _, secret := range []string{"dummy-user", "dummy-token", "dummy-npm", "user-c0l0n", "dummy-nul"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("the log holds a value (%q): %s", secret, logs.String())
		}
	}
}
