package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// An account of a repository of the project upstream that the installation's configuration declares (R-REPO-077,
// repositories.upstream_accounts): its username and its password are files the replica reads at every use, through the re-read of a
// reference (R-REPO-033). The value lives in the files alone: never in PostgreSQL, a log, an API answer or an audit event. quader migrate
// applies the declaration (ApplyUpstreamAccounts): it removes an account an administrator had stored and keeps the time of the
// declaration in upstream_repository.account_configured_at; the replica's own configuration decides at use (Q-REPO-484).

// ConfiguredAccount is one entry of repositories.upstream_accounts.
type ConfiguredAccount struct {
	// AuthMode is basic or bearer.
	AuthMode string
	// UsernameFile is the file of the username; empty for bearer.
	UsernameFile string
	PasswordFile string
	// SecretName is the Kubernetes Secret the chart mounts the files from; empty outside the chart.
	SecretName string
}

// ConfiguredAccounts are the declared accounts by the repository's key in the project upstream.
type ConfiguredAccounts map[string]ConfiguredAccount

// For answers the declared account of a repository, by its qualified name: only a repository of the project upstream has one.
func (a ConfiguredAccounts) For(project, key string) (ConfiguredAccount, bool) {
	if !wellknown.IsProject(project) {
		return ConfiguredAccount{}, false
	}
	c, ok := a[key]
	return c, ok
}

// The states of a declared account's files on the replica that answers (R-REPO-073 account.configuration.files).
const (
	AccountFilesReadable = "readable"
	AccountFilesMissing  = "missing"
	AccountFilesEmpty    = "empty"
	AccountFilesInvalid  = "invalid"
)

// The failures of a declared account's file. The texts name the path and never the content.
var (
	errAccountFileMissing = errors.New("does not exist")
	errAccountFileEmpty   = errors.New("is empty")
)

// MaxSecretFileBytes bounds a secret file: the value limit of the document, 8 KiB, and a line break.
const MaxSecretFileBytes = 8<<10 + 2

// ReadSecretFile reads a file up to MaxSecretFileBytes: the reader of a secretRef's file and of a declared account's files.
func ReadSecretFile(name string) ([]byte, error) {
	f, err := os.Open(name) //nolint:gosec // G304: name is a path of the configuration or a validated reference below config_as_code.secrets_dir
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, MaxSecretFileBytes))
}

// AccountFileValue reads one file of a declared account with read and checks the value as a secretRef's value is checked (R-REPO-033):
// at most 8 KiB, one trailing line break dropped, not empty, no line break or NUL inside; a username holds no colon, which would end it
// in the Basic header. The error names the file's path and never its content.
func AccountFileValue(read func(string) ([]byte, error), path string, username bool) (observability.Secret, error) {
	b, err := read(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("the file %s %w", path, errAccountFileMissing)
	case err != nil:
		return "", fmt.Errorf("the file %s cannot be read: %w", path, err)
	case len(b) >= MaxSecretFileBytes:
		return "", fmt.Errorf("the file %s is larger than 8 KiB", path)
	}
	v := strings.TrimRight(string(b), "\r\n")
	switch {
	case v == "":
		return "", fmt.Errorf("the file %s %w", path, errAccountFileEmpty)
	case strings.ContainsAny(v, "\r\n\x00"):
		return "", fmt.Errorf("the file %s holds a line break or NUL", path)
	case username && strings.Contains(v, ":"):
		return "", fmt.Errorf("the file %s holds a colon, which a username of basic authentication cannot hold", path)
	}
	return observability.Secret(v), nil
}

// FilesState reads the account's files with read and answers their state; the values are dropped at once. missing wins over empty,
// and both over invalid, so the page names the cause an operator fixes first.
func (a ConfiguredAccount) FilesState(read func(string) ([]byte, error)) string {
	type file struct {
		path     string
		username bool
	}
	files := []file{{a.PasswordFile, false}}
	if a.UsernameFile != "" {
		files = append([]file{{a.UsernameFile, true}}, files...)
	}
	state := AccountFilesReadable
	rank := map[string]int{AccountFilesReadable: 0, AccountFilesInvalid: 1, AccountFilesEmpty: 2, AccountFilesMissing: 3}
	for _, f := range files {
		_, err := AccountFileValue(read, f.path, f.username)
		got := AccountFilesReadable
		switch {
		case errors.Is(err, errAccountFileMissing):
			got = AccountFilesMissing
		case errors.Is(err, errAccountFileEmpty):
			got = AccountFilesEmpty
		case err != nil:
			got = AccountFilesInvalid
		}
		if rank[got] > rank[state] {
			state = got
		}
	}
	return state
}

// The reasons of the writes of quader migrate that apply repositories.upstream_accounts (R-REPO-077).
const (
	ReasonConfiguration        = "configuration"
	ReasonConfigurationRemoved = "configuration_removed"
)

// UpstreamAccountsResult is what ApplyUpstreamAccounts changed, by key.
type UpstreamAccountsResult struct {
	Declared []string
	Removed  []string
	// Overridden names the declared keys whose stored address an administrator had overridden: the account goes to that address's
	// hosts, which the log names (Q-REPO-486).
	Overridden map[string][]string
}

// ApplyUpstreamAccounts applies repositories.upstream_accounts in tx, with the actor of c (system:configuration): a declared key's
// stored account is removed and the declaration is marked; a marked repository whose key is no longer declared loses the mark. Each
// change raises config_version, invalidates what the repository cached as an account write does (R-REPO-069) and records
// repository.updated with the reason configuration or configuration_removed and account {source, authMode}; a second run with the same
// declaration writes nothing. A key that names no remote repository of the project upstream fails the run with the key named, before any
// write (R-REPO-077).
func (s *Service) ApplyUpstreamAccounts(ctx context.Context, tx meta.Tx, c Caller, declared ConfiguredAccounts) (UpstreamAccountsResult, error) {
	res := UpstreamAccountsResult{Overridden: map[string][]string{}}
	qs, err := queries(tx)
	if err != nil {
		return res, err
	}
	rows, err := qs.ListProjectRepositories(ctx, wellknown.ProjectKey)
	if err != nil {
		return res, err
	}
	facts, err := upstreamFactsByID(ctx, qs)
	if err != nil {
		return res, err
	}
	byKey := map[string]q.Repository{}
	for _, row := range rows {
		byKey[row.Key] = row
	}
	keys := make([]string, 0, len(declared))
	for k := range declared {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		row, ok := byKey[k]
		if !ok || Class(row.Class) != Remote {
			return res, fmt.Errorf("repositories.upstream_accounts.%s: the project upstream has no remote repository %q; the keys are those of "+
				"Admin area › Upstream registries, such as docker-hub (R-REPO-077)", k, k)
		}
	}
	for _, k := range keys {
		row := byKey[k]
		f := facts[row.ID]
		d, err := toDocument(row, s.cat)
		if err != nil {
			return res, err
		}
		rs, err := parseStoredRemote(d.Remote)
		if err != nil {
			return res, err
		}
		stored := rs.AuthMode != AuthNone || rs.Password != nil || rs.Username != ""
		if f.AccountConfiguredAt.Valid && !stored {
			continue
		}
		if err := s.writeConfiguredAccount(ctx, tx, c, k, ReasonConfiguration, declared[k].AuthMode); err != nil {
			return res, err
		}
		if err := qs.SetUpstreamAccountConfigured(ctx, row.ID); err != nil {
			return res, err
		}
		res.Declared = append(res.Declared, k)
		if f.AddressOverridden {
			res.Overridden[k] = wellknown.CredentialHosts(storedAddress(rs, d))
		}
	}
	for _, row := range rows {
		if _, still := declared[row.Key]; still || !facts[row.ID].AccountConfiguredAt.Valid {
			continue
		}
		if err := s.writeConfiguredAccount(ctx, tx, c, row.Key, ReasonConfigurationRemoved, AuthNone); err != nil {
			return res, err
		}
		if err := qs.ClearUpstreamAccountConfigured(ctx, row.ID); err != nil {
			return res, err
		}
		res.Removed = append(res.Removed, row.Key)
	}
	return res, nil
}

// writeConfiguredAccount is one write of the apply: the stored account goes (authMode none, no username, no secret), and the write is
// recorded and invalidates even where the document stays as it is.
func (s *Service) writeConfiguredAccount(ctx context.Context, tx meta.Tx, c Caller, key, reason, authMode string) error {
	pc := productCaller(c, reason)
	pc.product.account = &accountChange{Source: ReasonConfiguration, AuthMode: authMode}
	_, err := s.updateTx(ctx, tx, pc, upstreamName(key), MatchAny(), func(cur Document) (Document, error) {
		nd := cur
		return nd, setAccount(&nd, nil)
	})
	return described(err)
}

// accountChange is the account member of repository.updated for a write of quader migrate that applies a declared account: where the
// account comes from now and its authMode (none when the declaration is gone). The value is never in it.
type accountChange struct {
	Source   string `json:"source"`
	AuthMode string `json:"authMode"`
}

// configuredRefusal is the 409 managed_by_config of a write of the account, or of an address that moves the hosts receiving it, while
// the configuration declares the account (R-REPO-077, Q-REPO-486).
func configuredRefusal(key string, a ConfiguredAccount) error {
	where := "repositories.upstream_accounts." + key + " of the installation's configuration"
	if a.SecretName != "" {
		where = "the Helm values (secrets.upstreamAccounts." + key + ", Secret " + a.SecretName + ")"
	}
	return errs.E(errs.Conflict, errs.CodeManagedByConfig,
		"The account of upstream/"+key+" comes from "+where+": change or remove it there. An address that moves the hosts receiving it is refused for the same reason.")
}

// configuredAccount answers the declaration of a well-known repository: the replica's configuration, or the mark quader migrate left
// while this replica runs a configuration without it (a rolling update).
func (s *Service) configuredAccount(key string, f q.UpstreamRepository) (ConfiguredAccount, bool) {
	if a, ok := s.accounts.For(wellknown.ProjectKey, key); ok {
		return a, true
	}
	return ConfiguredAccount{}, f.AccountConfiguredAt.Valid
}

// ConfiguredAccountFor answers the declared account of a repository on this replica (the download list's accountRaisesLimit).
func (s *Service) ConfiguredAccountFor(project, key string) (ConfiguredAccount, bool) {
	return s.accounts.For(project, key)
}

// accountRaisesLimit reports whether an account would lift the repository's limit: the preset its address matches says so
// (catalog.UpstreamPreset.AccountRaisesLimit, Q-UI-1160) and the repository sends no credential.
func accountRaisesLimit(cat *catalog.Registry, format, url string, sends bool) bool {
	if sends {
		return false
	}
	e, ok := cat.Lookup(format)
	if !ok {
		return false
	}
	p, ok := e.PresetFor(url)
	return ok && p.AccountRaisesLimit
}

// AccountRaisesLimit is accountRaisesLimit with the service's catalog, for the API's limit view of a project's remote.
func (s *Service) AccountRaisesLimit(format, url string, sends bool) bool {
	return accountRaisesLimit(s.cat, format, url, sends)
}
