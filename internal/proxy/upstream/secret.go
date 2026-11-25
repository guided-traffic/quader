package upstream

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/repository"
)

// RefreshInterval is how long a resolved secretRef is used before the file or the
// variable is read again, so that a changed file takes effect within it (R-REPO-033
// names 60 s as the bound; Q-REPO-89). A reference that failed is tried again
// after RetryInterval.
const (
	RefreshInterval = 10 * time.Second
	RetryInterval   = time.Second

	// maxSecretBytes bounds a file: the value limit of the document, 8 KiB, and a
	// line break.
	maxSecretBytes = repository.MaxSecretFileBytes
)

// errNotConfigured is why a file reference cannot be resolved without a directory.
var errNotConfigured = errors.New("config_as_code.secrets_dir is not set")

// resolver reads what a reference points at.
type resolver struct {
	dir      string
	lookup   func(string) (string, bool)
	readFile func(string) ([]byte, error)
}

// resolve reads a reference. The rules of R-OPS-029 are checked again here, so a
// row that was written around the API cannot route a quader file or variable to an
// upstream.
func (r resolver) resolve(ref repository.SecretRef) (observability.Secret, error) {
	if why := ref.Valid(); why != "" {
		return "", errors.New("the reference is invalid: " + why)
	}
	var val string
	switch {
	case ref.Env != "":
		v, ok := r.lookup(ref.Env)
		if !ok {
			return "", fmt.Errorf("the variable %s is not set", ref.Env)
		}
		val = v
	default:
		if r.dir == "" {
			return "", errNotConfigured
		}
		b, err := r.readFile(filepath.Join(r.dir, ref.File))
		if err != nil {
			return "", fmt.Errorf("the file %s cannot be read: %w", ref.File, err)
		}
		if len(b) >= maxSecretBytes {
			return "", fmt.Errorf("the file %s is larger than 8 KiB", ref.File)
		}
		val = string(b)
	}
	val = strings.TrimRight(val, "\r\n")
	switch {
	case val == "":
		return "", errors.New("the value is empty")
	case strings.ContainsAny(val, "\r\n\x00"):
		return "", errors.New("the value holds a line break or NUL")
	}
	return observability.Secret(val), nil
}

// secretValue is one secret of a client: fixed, for a sealed value or an inline
// one, or a value read from somewhere (a reference, a declared account's file)
// that is read again after RefreshInterval, and after RetryInterval once it failed.
type secretValue struct {
	read func() (observability.Secret, error) // nil for a fixed value
	now  func() time.Time

	mu  sync.Mutex
	val observability.Secret
	err error
	at  time.Time
}

func newFixedSecret(v observability.Secret) *secretValue { return &secretValue{val: v} }

func newRefSecret(ref repository.SecretRef, res resolver, now func() time.Time) *secretValue {
	return newReadSecret(func() (observability.Secret, error) { return res.resolve(ref) }, now)
}

// newFileSecret is a file of an account the configuration declares (R-REPO-077): an absolute path of the configuration, outside
// config_as_code.secrets_dir, which no secretRef can name.
func newFileSecret(read func(string) ([]byte, error), path string, username bool, now func() time.Time) *secretValue {
	return newReadSecret(func() (observability.Secret, error) { return repository.AccountFileValue(read, path, username) }, now)
}

func newReadSecret(read func() (observability.Secret, error), now func() time.Time) *secretValue {
	s := &secretValue{read: read, now: now}
	s.val, s.err = read()
	s.at = now()
	return s
}

// get returns the current value, reading it again when it is due.
func (s *secretValue) get() (observability.Secret, error) {
	if s.read == nil {
		return s.val, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	age := s.now().Sub(s.at)
	if (s.err == nil && age >= RefreshInterval) || (s.err != nil && age >= RetryInterval) {
		s.val, s.err = s.read()
		s.at = s.now()
	}
	return s.val, s.err
}

// credential is the authentication of a client: how the password goes into the
// Authorization header (R-REPO-033). It is the only place that builds the header;
// the hop policy decides who receives it.
type credential struct {
	mode     string
	username string
	// user is the username read from a file of a declared account (R-REPO-077); nil takes username.
	user   *secretValue
	secret *secretValue // nil when no password is configured
}

// check reports why the credential cannot be used now: the failure of
// upstream_secret_unavailable, without the cause in the client-facing text.
func (c *credential) check(repo string) error {
	if c == nil {
		return nil
	}
	for _, v := range []*secretValue{c.user, c.secret} {
		if v == nil {
			continue
		}
		if _, err := v.get(); err != nil {
			return secretUnavailable(repo, err)
		}
	}
	return nil
}

// headers returns the Authorization header of the current secret; none for
// authMode none, or while a secret cannot be resolved (check has failed the
// request before it gets here).
func (c *credential) headers() http.Header {
	if c == nil || c.mode == repository.AuthNone {
		return nil
	}
	var pw string
	if c.secret != nil {
		v, err := c.secret.get()
		if err != nil {
			return nil
		}
		pw = v.Reveal()
	}
	username := c.username
	if c.user != nil {
		v, err := c.user.get()
		if err != nil {
			return nil
		}
		username = v.Reveal()
	}
	h := http.Header{}
	switch c.mode {
	case repository.AuthBasic:
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+pw)))
	case repository.AuthBearer:
		h.Set("Authorization", "Bearer "+pw)
	}
	return h
}

// fingerprint is a digest of the credential in effect now: it changes when the secret behind a reference is rotated, which
// moves no configuration version. The zero value is no credential. A token that was obtained under one credential is not
// reused under another (R-REPO-033).
func (c *credential) fingerprint() [32]byte {
	h := c.headers()
	if len(h) == 0 {
		return [32]byte{}
	}
	return sha256.Sum256([]byte(h.Get("Authorization")))
}

// secretUnavailable is the 502 of a secret that cannot be unsealed or resolved
// (R-REPO-033). The cause is for the log; the client-facing text names the
// repository and nothing about the secret.
func secretUnavailable(repo string, cause error) *errs.Error {
	return errs.E(errs.UpstreamFailed, errs.CodeUpstreamSecretUnavailable,
		"The upstream credential of the repository "+repo+" cannot be unsealed or resolved on this replica.",
		errs.WithCause(cause))
}
