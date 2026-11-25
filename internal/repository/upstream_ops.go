package repository

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The operations of the well-known repositories (R-REPO-073, R-API-083): the admin page's item, the state, the level of one and of
// all, the address and the account. They write through the update path of every repository as writes of the product, so the
// validation, the levels' rules, the secrets, the events and the repo_config change are those of any repository write; a write that
// changes nothing records nothing.

// UpstreamItem is a well-known repository as the admin page shows it (R-REPO-073). InUse is the API's: it reads the configuration of
// the replica that answers.
type UpstreamItem struct {
	Key         string
	Format      string
	Kind        wellknown.Kind
	DisplayName string
	// State is empty for a system repository.
	State      wellknown.State
	Visibility string
	// Fixed names what the product holds beyond the table of R-REPO-069: state and visibility of a system repository (the visibility
	// of upstream/clamav only while its clients are off), address and account of a local one.
	Fixed []string
	// Clients is the switch "ClamAV definition updates" of a repository whose declaration lets clients in (upstream/clamav,
	// R-REPO-075, Q-API-382); nil for every other repository.
	Clients *UpstreamClients
	Address UpstreamAddress
	Account UpstreamAccount
	// ListedBy is the number of virtual repositories whose member lists or pin rules name the repository, ListedByProjects the number
	// of their projects.
	ListedBy         int
	ListedByProjects int
	// ListedByNames is the qualified names of those virtual repositories, in byte order, at most 100; nil in the list, which answers
	// the numbers only (Q-API-363).
	ListedByNames []string
	ConfigVersion int64
	Warnings      []Warning
	// AccountRaisesLimit is whether an account would lift the upstream's limit: the preset its address matches says so and the
	// repository sends no credential (Q-UI-1160).
	AccountRaisesLimit bool
}

// UpstreamClients is whether an administrator switched the clients of a system repository on (R-REPO-075). The address the item
// shows is the API's: the protocol base on the base URL of the request.
type UpstreamClients struct {
	Enabled bool
}

// UpstreamAddress is the stored address, whether it differs from the product's, and the product's.
type UpstreamAddress struct {
	URL          string
	ExtraHosts   []ExtraHost
	UpstreamKeys []ArchiveKey // nil for a format without archive keys
	Overridden   bool
	Product      UpstreamProductAddress
}

// UpstreamProductAddress is the address the catalog of the running binary declares; for a retired repository, the stored one.
type UpstreamProductAddress struct {
	URL          string
	ExtraHosts   []ExtraHost
	UpstreamKeys []ArchiveKey
}

// ArchiveKey is one key of deb.upstreamKeys as the item lists it.
type ArchiveKey struct {
	Fingerprint string
	UserID      string
}

// UpstreamAccount is the account without its secret: the stored one, or the one the replica's configuration declares (R-REPO-077).
type UpstreamAccount struct {
	AuthMode string
	// Username is nil without an account and for a declared one: the username read from a file is never shown.
	Username *string
	// Password is {"set": true}, the reference as stored, or nil.
	Password          *SecretField
	RemovedByUpdateAt *time.Time
	// Source is AccountSourceAPI or AccountSourceConfiguration, from the configuration of the replica that answers.
	Source string
	// Configuration is the declaration while Source is configuration; nil otherwise.
	Configuration *UpstreamAccountConfiguration
	// LastDownloadAt and LastFailure come from the installation's last 100 ended downloads (R-OPS-051), on the answer about one
	// repository only; nil when none of them is the repository's.
	LastDownloadAt *time.Time
	LastFailure    *UpstreamAccountFailure
}

// The sources of an account (R-REPO-073 account.source).
const (
	AccountSourceAPI           = "api"
	AccountSourceConfiguration = "configuration"
)

// UpstreamAccountConfiguration is a declared account as the item shows it: the Secret's name, when quader migrate applied it, and the
// state of its files on the replica that answers. Nothing of the values.
type UpstreamAccountConfiguration struct {
	SecretName string
	Since      *time.Time
	Files      string
}

// UpstreamAccountFailure is the last failed download of the repository: when, and its code.
type UpstreamAccountFailure struct {
	At   time.Time
	Code string
}

// UpstreamItems lists the well-known repositories: the presets in catalog order, the retired ones by key, then the system
// repositories (R-REPO-073). Three queries whatever the number of repositories: the rows, their facts and their listing.
func (s *Service) UpstreamItems(ctx context.Context) ([]UpstreamItem, error) {
	var out []UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.ListProjectRepositories(ctx, wellknown.ProjectKey)
		if err != nil {
			return err
		}
		facts, err := upstreamFactsByID(ctx, qs)
		if err != nil {
			return err
		}
		counts, err := listedByCounts(ctx, qs)
		if err != nil {
			return err
		}
		for _, row := range rows {
			it, err := s.upstreamItem(row, facts, counts)
			if err != nil {
				return err
			}
			out = append(out, it)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rank := upstreamRank(wellknown.Declared(s.cat))
	slices.SortStableFunc(out, func(a, b UpstreamItem) int { return rank(a.Key, a.Kind, b.Key, b.Kind) })
	return out, nil
}

// upstreamRank orders the well-known repositories: the presets in catalog order, a key the catalog no longer declares after them
// by key, the system repositories last.
func upstreamRank(decls []wellknown.Declaration) func(ak string, akind wellknown.Kind, bk string, bkind wellknown.Kind) int {
	rank := func(key string, kind wellknown.Kind) (int, int) {
		i := slices.IndexFunc(decls, func(d wellknown.Declaration) bool { return d.Key == key })
		system := 0
		if kind == wellknown.KindSystem {
			system = 1
		}
		if i < 0 {
			i = len(decls)
		}
		return system, i
	}
	return func(ak string, akind wellknown.Kind, bk string, bkind wellknown.Kind) int {
		as, ai := rank(ak, akind)
		bs, bi := rank(bk, bkind)
		switch {
		case as != bs:
			return as - bs
		case ai != bi:
			return ai - bi
		}
		return strings.Compare(ak, bk)
	}
}

// UpstreamItem returns one well-known repository; a key of no repository of the project upstream answers repository_not_found.
func (s *Service) UpstreamItem(ctx context.Context, key string) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.upstreamItemTx(ctx, tx, key, nil)
		return err
	})
	return out, err
}

// UpstreamSummary is a preset repository as a writer of a virtual repository sees it in the member picker (R-UI-189, Q-API-364):
// its state and its level, and nothing of its address, its account or who lists it.
type UpstreamSummary struct {
	Key         string
	DisplayName string
	State       wellknown.State
	Visibility  string
}

// UpstreamSummaries lists the preset repositories of a format that are not retired, in catalog order; a system repository is never
// a member (R-REPO-070) and is not listed. Two queries: the rows and their facts.
func (s *Service) UpstreamSummaries(ctx context.Context, format string) ([]UpstreamSummary, error) {
	decls := wellknown.Declared(s.cat)
	var out []UpstreamSummary
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.ListProjectRepositories(ctx, wellknown.ProjectKey)
		if err != nil {
			return err
		}
		facts, err := upstreamFactsByID(ctx, qs)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Format != format {
				continue
			}
			r, _ := toRepository(row, wellknown.ProjectKey, s.cat)
			withUpstream(&r, facts)
			if r.Upstream == nil || r.Upstream.Kind != wellknown.KindPreset || r.Upstream.Retired() {
				continue
			}
			sum := UpstreamSummary{Key: row.Key, DisplayName: row.Key, State: r.Upstream.State, Visibility: row.VisibilityArtifacts}
			if decl, ok := wellknown.Lookup(decls, row.Key); ok {
				sum.DisplayName = decl.DisplayName
			}
			out = append(out, sum)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rank := upstreamRank(decls)
	slices.SortStableFunc(out, func(a, b UpstreamSummary) int { return rank(a.Key, wellknown.KindPreset, b.Key, wellknown.KindPreset) })
	return out, nil
}

// upstreamItemTx is the item about one repository: the item of the list and the names of the virtual repositories that list it.
func (s *Service) upstreamItemTx(ctx context.Context, tx meta.Tx, key string, warnings []Warning) (UpstreamItem, error) {
	qs, err := queries(tx)
	if err != nil {
		return UpstreamItem{}, err
	}
	rows, err := qs.GetRepositoryByName(ctx, q.GetRepositoryByNameParams{Project: wellknown.ProjectKey, Key: key})
	if err != nil {
		return UpstreamItem{}, err
	}
	if len(rows) == 0 {
		return UpstreamItem{}, notFound(upstreamName(key))
	}
	facts, err := upstreamFactsOf(ctx, qs, rows[0].ID)
	if err != nil {
		return UpstreamItem{}, err
	}
	counts, err := listedByCounts(ctx, qs)
	if err != nil {
		return UpstreamItem{}, err
	}
	it, err := s.upstreamItem(rows[0], facts, counts)
	if err != nil {
		return UpstreamItem{}, err
	}
	if it.ListedByNames, err = listedByNames(ctx, qs, rows[0].ID); err != nil {
		return UpstreamItem{}, err
	}
	last, err := qs.UpstreamRepositoryLastDownloads(ctx, rows[0].ID)
	if err != nil {
		return UpstreamItem{}, err
	}
	if len(last) > 0 {
		if t := last[0].LastDownloadAt; t.Valid {
			at := t.Time.UTC()
			it.Account.LastDownloadAt = &at
		}
		if t := last[0].LastFailureAt; t.Valid {
			it.Account.LastFailure = &UpstreamAccountFailure{At: t.Time.UTC(), Code: last[0].LastFailureCode}
		}
	}
	it.Warnings = warnings
	return it, nil
}

// upstreamItem renders a row of the project upstream as the item, from its facts and the listing read for every row at once.
func (s *Service) upstreamItem(row q.Repository, facts map[int64]q.UpstreamRepository, counts map[int64]listedCount) (UpstreamItem, error) {
	d, err := toDocument(row, s.cat)
	if err != nil {
		return UpstreamItem{}, fmt.Errorf("repository: stored document of upstream/%s: %w", row.Key, err)
	}
	r, _ := toRepository(row, wellknown.ProjectKey, s.cat)
	withUpstream(&r, facts)
	f := facts[row.ID]
	it := UpstreamItem{
		Key: row.Key, Format: row.Format, Kind: wellknown.Kind(f.Kind), DisplayName: d.Description, Visibility: row.VisibilityArtifacts,
		Fixed: []string{}, ListedBy: counts[row.ID].virtuals, ListedByProjects: counts[row.ID].projects, ConfigVersion: row.ConfigVersion,
	}
	if r.Upstream != nil {
		it.State = r.Upstream.State
	}
	decl, declared := wellknown.Lookup(wellknown.Declared(s.cat), row.Key)
	if it.Kind == wellknown.KindSystem {
		it.Fixed = []string{"state", "visibility"}
		if declared && decl.Clients {
			it.Clients = &UpstreamClients{Enabled: f.ClientsEnabled}
			if f.ClientsEnabled {
				// While clients are on, the level is the administrator's (Q-REPO-415).
				it.Fixed = []string{"state"}
			}
		}
	}
	rs := &RemoteSettings{AuthMode: AuthNone}
	if d.Class == Remote {
		if rs, err = parseStoredRemote(d.Remote); err != nil {
			return UpstreamItem{}, fmt.Errorf("repository: stored remote section of upstream/%s: %w", row.Key, err)
		}
	} else {
		// A local system repository (clamav, R-REPO-075) has no address and no account to set (Q-REPO-412).
		it.Fixed = append(it.Fixed, "address", "account")
	}
	stored := storedAddress(rs, d)
	product := stored
	if declared {
		product = decl.Address()
		it.DisplayName = decl.DisplayName
	}
	deb := row.Format == debFormat
	it.Address = UpstreamAddress{
		URL: rs.URL, ExtraHosts: nonNilHosts(rs.ExtraHosts), UpstreamKeys: archiveKeys(deb, stored.Settings), Overridden: f.AddressOverridden,
		Product: UpstreamProductAddress{URL: product.URL, ExtraHosts: extraHosts(product.ExtraHosts), UpstreamKeys: archiveKeys(deb, product.Settings)},
	}
	it.Account = UpstreamAccount{AuthMode: rs.AuthMode, Password: rs.Password, Source: AccountSourceAPI}
	if rs.AuthMode != AuthNone || rs.Password != nil {
		u := rs.Username
		it.Account.Username = &u
	}
	if f.AccountRemovedAt.Valid {
		at := f.AccountRemovedAt.Time.UTC()
		it.Account.RemovedByUpdateAt = &at
	}
	if a, ok := s.accounts.For(wellknown.ProjectKey, row.Key); ok && d.Class == Remote {
		conf := &UpstreamAccountConfiguration{SecretName: a.SecretName, Files: a.FilesState(s.readFile)}
		if f.AccountConfiguredAt.Valid {
			at := f.AccountConfiguredAt.Time.UTC()
			conf.Since = &at
		}
		it.Account = UpstreamAccount{AuthMode: a.AuthMode, Source: AccountSourceConfiguration, Configuration: conf}
	}
	it.AccountRaisesLimit = d.Class == Remote && accountRaisesLimit(s.cat, row.Format, rs.URL, it.Account.AuthMode != AuthNone)
	return it, nil
}

func nonNilHosts(h []ExtraHost) []ExtraHost {
	if h == nil {
		return []ExtraHost{}
	}
	return h
}

// archiveKeys lists the keys of the deb.upstreamKeys a settings member holds: an empty list for an APT archive without keys, nil
// for another format.
func archiveKeys(deb bool, settings json.RawMessage) []ArchiveKey {
	if !deb {
		return nil
	}
	out := []ArchiveKey{}
	keys := debKeysOf(objectMembers(settings))
	if keys == "" {
		return out
	}
	certs, err := openpgp.ParseStoredCertificates([]byte(keys), openpgp.Limits{})
	if err != nil {
		return out
	}
	for _, c := range certs {
		out = append(out, ArchiveKey{Fingerprint: c.Fingerprint, UserID: c.UserID})
	}
	return out
}

// upstreamWrite is one write of an operation: the repository by key, the product's caller with the operation's reason, and the
// change of its document. It answers the item after the write, in the write's transaction, and runs after the post-step of the
// upstream_repository row.
func (s *Service) upstreamWrite(ctx context.Context, tx meta.Tx, c Caller, key string, pre Precondition, reason string,
	build func(cur Document) (Document, error), after func(qs *q.Queries, id int64) error,
) (UpstreamItem, error) {
	pc := productCaller(c, reason)
	pc.product.bulkID, pc.product.clients = c.product.bulkID, c.product.clients
	st, err := s.updateTx(ctx, tx, pc, upstreamName(key), pre, build)
	if err != nil {
		return UpstreamItem{}, err
	}
	if after != nil {
		qs, err := queries(tx)
		if err != nil {
			return UpstreamItem{}, err
		}
		if err := after(qs, st.Repository.NumericID()); err != nil {
			return UpstreamItem{}, err
		}
	}
	return s.upstreamItemTx(ctx, tx, key, st.Warnings)
}

// upstreamFacts reads the kind and the state of a well-known repository by key; repository_not_found for a key of no repository of
// the project upstream.
func (s *Service) upstreamFacts(ctx context.Context, tx meta.Tx, key string) (*UpstreamFacts, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.GetRepositoryByName(ctx, q.GetRepositoryByNameParams{Project: wellknown.ProjectKey, Key: key})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, notFound(upstreamName(key))
	}
	facts, err := upstreamFactsOf(ctx, qs, rows[0].ID)
	if err != nil {
		return nil, err
	}
	r, _ := toRepository(rows[0], wellknown.ProjectKey, s.cat)
	withUpstream(&r, facts)
	if r.Upstream == nil {
		return nil, notFound(upstreamName(key))
	}
	return r.Upstream, nil
}

// SetUpstreamState switches a well-known repository on or off (R-REPO-068). A retired repository is not switched on (409
// upstream_retired); a system repository has no state (409 managed_by_product).
func (s *Service) SetUpstreamState(ctx context.Context, c Caller, key string, on bool) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.setUpstreamStateTx(ctx, tx, c, key, on, "")
		return err
	})
	return out, err
}

func (s *Service) setUpstreamStateTx(ctx context.Context, tx meta.Tx, c Caller, key string, on bool, level string) (UpstreamItem, error) {
	f, err := s.upstreamFacts(ctx, tx, key)
	if err != nil {
		return UpstreamItem{}, err
	}
	switch {
	case f.System():
		return UpstreamItem{}, managedByProduct("upstream/" + key + " has no state of its own: it is in use while the work it serves reads through it.")
	case on && f.Retired():
		return UpstreamItem{}, errs.E(errs.Conflict, errs.CodeUpstreamRetired, "Quader no longer offers upstream/"+key+", so it is not switched on.")
	}
	reason := ReasonSwitchedOff
	if on {
		reason = ReasonSwitchedOn
	}
	return s.upstreamWrite(ctx, tx, c, key, MatchAny(), reason, func(cur Document) (Document, error) {
		cur.Disabled = !on
		if level != "" {
			cur.Visibility = Visibility{Artifacts: level, Wiki: level}
		}
		return cur, nil
	}, func(qs *q.Queries, id int64) error {
		if !on {
			return nil
		}
		return qs.MarkUpstreamSwitchedOn(ctx, id)
	})
}

// SwitchOnAllUpstreamTx switches on every preset repository that is neither active nor retired and makes both its levels internal,
// in the caller's transaction (R-REPO-068, Q-REPO-384, Q-API-361). It answers the repositories it switched on and the bulk ID their
// events carry; a system repository is never touched.
func (s *Service) SwitchOnAllUpstreamTx(ctx context.Context, tx meta.Tx, c Caller) ([]UpstreamItem, string, error) {
	keys, err := s.upstreamPresets(ctx, tx, func(f *UpstreamFacts) bool {
		return f.State != wellknown.StateActive && f.State != wellknown.StateRetired
	})
	if err != nil {
		return nil, "", err
	}
	c.product.bulkID = newBulkID()
	out := make([]UpstreamItem, 0, len(keys))
	for _, k := range keys {
		it, err := s.setUpstreamStateTx(ctx, tx, c, k, true, LevelInternal)
		if err != nil {
			return nil, "", err
		}
		out = append(out, it)
	}
	return out, c.product.bulkID, nil
}

// upstreamPresets lists the keys of the preset repositories whose facts keep, in key order.
func (s *Service) upstreamPresets(ctx context.Context, tx meta.Tx, keep func(*UpstreamFacts) bool) ([]string, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.ListProjectRepositories(ctx, wellknown.ProjectKey)
	if err != nil {
		return nil, err
	}
	facts, err := upstreamFactsByID(ctx, qs)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, row := range rows {
		r, _ := toRepository(row, wellknown.ProjectKey, s.cat)
		withUpstream(&r, facts)
		if r.Upstream != nil && r.Upstream.Kind == wellknown.KindPreset && keep(r.Upstream) {
			out = append(out, row.Key)
		}
	}
	return out, nil
}

// SetUpstreamVisibility sets both levels of a well-known repository to one level (R-REPO-069); a system repository's level is fixed
// (409 managed_by_product). The rules of R-AUTHZ-038 are the update path's: the restricted levels, public_not_allowed, a raise asks
// c.RaiseGate.
func (s *Service) SetUpstreamVisibility(ctx context.Context, c Caller, key, level string) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.setUpstreamVisibilityTx(ctx, tx, c, key, level)
		return err
	})
	return out, err
}

func (s *Service) setUpstreamVisibilityTx(ctx context.Context, tx meta.Tx, c Caller, key, level string) (UpstreamItem, error) {
	f, err := s.upstreamFacts(ctx, tx, key)
	if err != nil {
		return UpstreamItem{}, err
	}
	if f.System() && !f.ClientsEnabled {
		if decl, ok := wellknown.Lookup(wellknown.Declared(s.cat), key); ok && decl.Clients {
			return UpstreamItem{}, managedByProduct("The level of upstream/" + key + " is private while its clients are switched off; switch them on with a level.")
		}
		return UpstreamItem{}, managedByProduct("The level of upstream/" + key + " is fixed: it is always private and only for Quader's own work.")
	}
	return s.upstreamWrite(ctx, tx, c, key, MatchAny(), "", func(cur Document) (Document, error) {
		cur.Visibility = Visibility{Artifacts: level, Wiki: level}
		return cur, nil
	}, nil)
}

// SetAllUpstreamVisibilityTx sets the level of every preset repository that is not retired, in the caller's transaction: one refusal
// changes none (Q-API-361). It answers the repositories whose level it changed.
func (s *Service) SetAllUpstreamVisibilityTx(ctx context.Context, tx meta.Tx, c Caller, level string) ([]UpstreamItem, string, error) {
	keys, err := s.upstreamPresets(ctx, tx, func(f *UpstreamFacts) bool { return !f.Retired() })
	if err != nil {
		return nil, "", err
	}
	c.product.bulkID = newBulkID()
	out := []UpstreamItem{}
	for _, k := range keys {
		before, err := s.upstreamItemTx(ctx, tx, k, nil)
		if err != nil {
			return nil, "", err
		}
		it, err := s.setUpstreamVisibilityTx(ctx, tx, c, k, level)
		if err != nil {
			return nil, "", err
		}
		if it.ConfigVersion != before.ConfigVersion {
			out = append(out, it)
		}
	}
	return out, c.product.bulkID, nil
}

// The reasons of the switch of a system repository's clients (R-REPO-075).
const (
	ReasonClientsOn  = "clients_on"
	ReasonClientsOff = "clients_off"
)

// WarningPublicNotAllowedLowered is the warning of a switch-on at public in an installation whose default endpoint admits no request
// without a credential: the clients are switched on at internal instead (R-REPO-075, R-REPO-073).
const WarningPublicNotAllowedLowered = "public_not_allowed_lowered"

// SetUpstreamClients switches the clients of a system repository on or off (R-REPO-075, Q-REPO-414, Q-API-382): only a repository
// whose declaration lets clients in (upstream/clamav) has the switch, every other answers 409 managed_by_product. Switching on opens the
// repository at level (public when empty), and at internal with the warning public_not_allowed_lowered where the installation does not
// allow public and the caller asked for public; it is a widening, so the caller's RaiseGate asks for a recent sign-in. Switching off
// sets both levels back to private, which records repository.narrowed. Each write raises config_version, records repository.updated
// with clients among its changes and the reason clients_on or clients_off, and notifies repo_config.
func (s *Service) SetUpstreamClients(ctx context.Context, c Caller, key string, enabled bool, level string) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		f, err := s.upstreamFacts(ctx, tx, key)
		if err != nil {
			return err
		}
		if decl, ok := wellknown.Lookup(wellknown.Declared(s.cat), key); !ok || !decl.Clients || !f.System() {
			return managedByProduct("upstream/" + key + " serves no client of its own: only the repository of ClamAV's definitions has this switch.")
		}
		if !enabled {
			out, err = s.writeClients(ctx, tx, c, key, false, LevelPrivate, nil)
			return err
		}
		if level == "" {
			level = LevelPublic
		}
		if c.RaiseGate != nil && !f.ClientsEnabled {
			if err := c.RaiseGate(ctx); err != nil {
				return err
			}
		}
		out, err = s.writeClients(ctx, tx, c, key, true, level, nil)
		if e, ok := errs.As(err); ok && e.Code == errs.CodePublicNotAllowed && level == LevelPublic {
			out, err = s.writeClients(ctx, tx, c, key, true, LevelInternal, []Warning{{
				Code: WarningPublicNotAllowedLowered, Pointer: "/level",
				Detail: "The default endpoint admits no request without a credential, so the ClamAV definitions are served at internal; ClamAV installations send a token.",
			}})
		}
		return err
	})
	return out, err
}

// writeClients writes the switch and the level of a system repository's clients in one update.
func (s *Service) writeClients(ctx context.Context, tx meta.Tx, c Caller, key string, enabled bool, level string, warnings []Warning) (UpstreamItem, error) {
	reason := ReasonClientsOff
	if enabled {
		reason = ReasonClientsOn
	}
	pc := c
	pc.product.clients = true
	it, err := s.upstreamWrite(ctx, tx, pc, key, MatchAny(), reason, func(cur Document) (Document, error) {
		cur.Visibility = Visibility{Artifacts: level, Wiki: level}
		return cur, nil
	}, func(qs *q.Queries, id int64) error {
		return qs.SetUpstreamClientsEnabled(ctx, q.SetUpstreamClientsEnabledParams{Enabled: enabled, RepositoryID: id})
	})
	if err != nil {
		return UpstreamItem{}, err
	}
	it.Warnings = append(it.Warnings, warnings...)
	return it, nil
}

// UpstreamAddressInput is the body of setUpstreamRepositoryAddress: an omitted member is the product's; Account replaces the stored
// account in the same write when AccountSet (null in the body is a nil Account).
type UpstreamAddressInput struct {
	URL          *string
	ExtraHosts   *[]ExtraHost
	UpstreamKeys *string
	AccountSet   bool
	Account      *UpstreamAccountInput
}

// UpstreamAccountInput is the body of setUpstreamRepositoryAccount: the members of R-REPO-033, the password as the document's secret
// field ({"value": …} or {"secretRef": …}).
type UpstreamAccountInput struct {
	AuthMode string          `json:"authMode"`
	Username *string         `json:"username"`
	Password json.RawMessage `json:"password"`
}

// SetUpstreamAddress overrides the address of a well-known repository (R-REPO-069). A stored account stays only while the hosts that
// receive it stay: otherwise the body must carry account (422 secret_value_required at /account/password), for every caller. The
// item reads overridden while the result differs from the product's address.
func (s *Service) SetUpstreamAddress(ctx context.Context, c Caller, key string, pre Precondition, in UpstreamAddressInput) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		decl, ok := wellknown.Lookup(wellknown.Declared(s.cat), key)
		if !ok {
			cur, err := s.upstreamItemTx(ctx, tx, key, nil)
			if err != nil {
				return err
			}
			decl = wellknown.Declaration{Key: key, Format: catalog.ID(cur.Format), URL: cur.Address.URL, ExtraHosts: presetHosts(cur.Address.ExtraHosts)}
		}
		want := decl.Address()
		if in.URL != nil {
			want.URL = *in.URL
		}
		if in.ExtraHosts != nil {
			want.ExtraHosts = presetHosts(*in.ExtraHosts)
		}
		if in.UpstreamKeys != nil {
			if string(decl.Format) != debFormat {
				return addressInvalid(errs.FieldError{
					Pointer: "/upstreamKeys", Code: errs.CodeNotApplicable,
					Detail: "archive keys belong to an APT archive; this repository is of the format " + string(decl.Format),
				})
			}
			raw, err := json.Marshal(map[string]string{"upstreamKeys": *in.UpstreamKeys})
			if err != nil {
				return err
			}
			want.Settings = raw
		}
		var err error
		out, err = s.writeAddress(ctx, tx, c, key, pre, decl, want, in.AccountSet, in.Account)
		return err
	})
	return out, err
}

// ResetUpstreamAddress writes the product's address and clears overridden (R-REPO-069). When the product's hosts that receive a
// credential differ from the override's while an account is stored, removeAccount must be set (422 secret_value_required otherwise).
func (s *Service) ResetUpstreamAddress(ctx context.Context, c Caller, key string, pre Precondition, removeAccount bool) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		decl, ok := wellknown.Lookup(wellknown.Declared(s.cat), key)
		if !ok {
			if _, err := s.upstreamFacts(ctx, tx, key); err != nil {
				return err
			}
			return errs.E(errs.Conflict, errs.CodeUpstreamRetired, "Quader no longer offers upstream/"+key+": it has no address of the product's to reset to.")
		}
		var err error
		out, err = s.writeAddress(ctx, tx, c, key, pre, decl, decl.Address(), removeAccount, nil)
		return err
	})
	return out, err
}

// writeAddress writes an address and, when accountSet, the account; overridden follows whether the result differs from the
// declaration's address.
func (s *Service) writeAddress(ctx context.Context, tx meta.Tx, c Caller, key string, pre Precondition, decl wellknown.Declaration,
	want wellknown.Address, accountSet bool, account *UpstreamAccountInput,
) (UpstreamItem, error) {
	declared, configured, err := s.declaredAccount(ctx, tx, key)
	if err != nil {
		return UpstreamItem{}, err
	}
	return s.upstreamWrite(ctx, tx, c, key, pre, "", func(cur Document) (Document, error) {
		if err := remoteOnly(cur); err != nil {
			return Document{}, err
		}
		rs, err := parseStoredRemote(cur.Remote)
		if err != nil {
			return Document{}, err
		}
		if accountSet && account != nil {
			if err := valueOnly(account); err != nil {
				return Document{}, err
			}
		}
		if configured && (accountSet || !slices.Equal(wellknown.CredentialHosts(storedAddress(rs, cur)), wellknown.CredentialHosts(want))) {
			return Document{}, configuredRefusal(key, declared)
		}
		hasAccount := rs.AuthMode != AuthNone || rs.Password != nil
		if hasAccount && !accountSet && !slices.Equal(wellknown.CredentialHosts(storedAddress(rs, cur)), wellknown.CredentialHosts(want)) {
			return Document{}, addressInvalid(errs.FieldError{
				Pointer: "/account/password", Code: errs.CodeSecretValueRequired,
				Detail: "the stored account goes to " + strings.Join(wellknown.CredentialHosts(storedAddress(rs, cur)), ", ") +
					" only: enter it again for the new address, or remove it (account null, or removeAccount=true on a reset)",
			})
		}
		nd := cur
		if err := setAddress(&nd, want, false); err != nil {
			return Document{}, err
		}
		if accountSet {
			if err := setAccount(&nd, account); err != nil {
				return Document{}, err
			}
		}
		return nd, nil
	}, func(qs *q.Queries, id int64) error {
		facts, err := qs.GetRepositoryByID(ctx, id)
		if err != nil || len(facts) == 0 {
			return err
		}
		d, err := toDocument(facts[0], s.cat)
		if err != nil {
			return err
		}
		rs, err := parseStoredRemote(d.Remote)
		if err != nil {
			return err
		}
		overridden := !wellknown.AddressEqual(decl.Address(), storedAddress(rs, d))
		if err := qs.SetUpstreamAddressOverridden(ctx, q.SetUpstreamAddressOverriddenParams{Overridden: overridden, RepositoryID: id}); err != nil {
			return err
		}
		if accountSet && account != nil {
			return qs.ClearUpstreamAccountRemoved(ctx, id)
		}
		return nil
	})
}

// SetUpstreamAccount stores an account for a well-known repository's upstream (R-REPO-033, R-REPO-069). The password is a value: a
// reference is refused, since an account from a file is the configuration's (R-REPO-077, Q-REPO-487); an account the configuration
// declares is refused with 409 managed_by_config.
func (s *Service) SetUpstreamAccount(ctx context.Context, c Caller, key string, pre Precondition, in UpstreamAccountInput) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		if err := s.refuseDeclared(ctx, tx, key); err != nil {
			return err
		}
		var err error
		out, err = s.upstreamWrite(ctx, tx, c, key, pre, "", func(cur Document) (Document, error) {
			if err := remoteOnly(cur); err != nil {
				return Document{}, err
			}
			if err := valueOnly(&in); err != nil {
				return Document{}, err
			}
			nd := cur
			return nd, setAccount(&nd, &in)
		}, func(qs *q.Queries, id int64) error { return qs.ClearUpstreamAccountRemoved(ctx, id) })
		return err
	})
	return out, err
}

// RemoveUpstreamAccount removes the stored account; an account the configuration declares is refused with 409 managed_by_config.
func (s *Service) RemoveUpstreamAccount(ctx context.Context, c Caller, key string, pre Precondition) (UpstreamItem, error) {
	var out UpstreamItem
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		if err := s.refuseDeclared(ctx, tx, key); err != nil {
			return err
		}
		var err error
		out, err = s.upstreamWrite(ctx, tx, c, key, pre, "", func(cur Document) (Document, error) {
			if err := remoteOnly(cur); err != nil {
				return Document{}, err
			}
			nd := cur
			return nd, setAccount(&nd, nil)
		}, nil)
		return err
	})
	return out, err
}

// remoteOnly refuses the address and the account of a local system repository (clamav, R-REPO-075): it has neither, and
// the operations answer 409 managed_by_product.
func remoteOnly(cur Document) error {
	if cur.Class == Remote {
		return nil
	}
	return managedByProduct("upstream/" + cur.Key + " is a local repository of Quader's own work: it has no address and no account.")
}

// setAccount writes an account into the remote section; nil removes it.
func setAccount(d *Document, a *UpstreamAccountInput) error {
	if a == nil {
		return setRemote(d, map[string]any{"authMode": AuthNone, "username": "", "password": nil})
	}
	username := ""
	if a.Username != nil {
		username = *a.Username
	}
	var password any
	if len(a.Password) > 0 && string(a.Password) != "null" {
		password = a.Password
	}
	return setRemote(d, map[string]any{"authMode": a.AuthMode, "username": username, "password": password})
}

// declaredAccount answers whether the configuration declares the account of a well-known repository: the replica's own, or the mark
// of quader migrate (Q-REPO-484). A key of no repository of the project answers false; the write that follows answers its 404.
func (s *Service) declaredAccount(ctx context.Context, tx meta.Tx, key string) (ConfiguredAccount, bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return ConfiguredAccount{}, false, err
	}
	rows, err := qs.GetRepositoryByName(ctx, q.GetRepositoryByNameParams{Project: wellknown.ProjectKey, Key: key})
	if err != nil || len(rows) == 0 {
		return ConfiguredAccount{}, false, err
	}
	facts, err := qs.GetUpstreamRepositoryFacts(ctx, rows[0].ID)
	if err != nil || len(facts) == 0 {
		return ConfiguredAccount{}, false, err
	}
	a, ok := s.configuredAccount(key, facts[0])
	return a, ok, nil
}

// refuseDeclared is the 409 managed_by_config of the account's own operations while the configuration declares it.
func (s *Service) refuseDeclared(ctx context.Context, tx meta.Tx, key string) error {
	a, ok, err := s.declaredAccount(ctx, tx, key)
	if err != nil || !ok {
		return err
	}
	return configuredRefusal(key, a)
}

// valueOnly refuses a password given as a secretRef (Q-REPO-487): the account of a well-known repository is a value an administrator
// types, or the configuration's files (R-REPO-077). The pointer is the document's, which UpstreamPointers maps to the body's.
func valueOnly(in *UpstreamAccountInput) error {
	var probe struct {
		SecretRef json.RawMessage `json:"secretRef"`
	}
	// A member that is no object is the document's validation's to refuse; only a reference is refused here.
	_ = json.Unmarshal(in.Password, &probe)
	if len(probe.SecretRef) == 0 {
		return nil
	}
	return addressInvalid(errs.FieldError{
		Pointer: "/remote/password/secretRef", Code: errs.CodeSecretValueRequired,
		Detail: "the account of a well-known repository is given as a value; an account read from files is declared in " +
			"repositories.upstream_accounts (the Helm values' secrets.upstreamAccounts)",
	})
}

// addressInvalid is the 422 of a body of the address or the account operation.
func addressInvalid(f errs.FieldError) error {
	return errs.E(errs.Invalid, errs.CodeValidation, "1 field is invalid.", errs.WithFields(f))
}

// UpstreamPointers maps the pointers of a refused write of the address or the account from the repository document to the body of
// the operation (R-REPO-073): /remote/url to /url, /deb/upstreamKeys to /upstreamKeys, /remote/password to /account/password.
func UpstreamPointers(err error, account bool) error {
	e, ok := errs.As(err)
	if !ok || len(e.Fields) == 0 {
		return err
	}
	prefix := ""
	if !account {
		prefix = "/account"
	}
	out := *e
	out.Fields = make([]errs.FieldError, len(e.Fields))
	for i, f := range e.Fields {
		switch {
		case f.Pointer == "/remote/url" || strings.HasPrefix(f.Pointer, "/remote/extraHosts"):
			f.Pointer = strings.TrimPrefix(f.Pointer, "/remote")
		case f.Pointer == debKeysPointer:
			f.Pointer = "/upstreamKeys"
		case f.Pointer == "/remote/authMode" || f.Pointer == "/remote/username" || strings.HasPrefix(f.Pointer, "/remote/password"):
			f.Pointer = prefix + strings.TrimPrefix(f.Pointer, "/remote")
		}
		out.Fields[i] = f
	}
	return &out
}

func extraHosts(in []catalog.PresetHost) []ExtraHost {
	out := make([]ExtraHost, 0, len(in))
	for _, h := range in {
		out = append(out, ExtraHost{Pattern: h.Pattern, SendCredentials: h.SendCredentials})
	}
	return out
}

// newBulkID is the ID of a bulk operation (Q-API-361): a UUIDv4 in text.
func newBulkID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
