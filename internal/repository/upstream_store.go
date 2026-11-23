package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The writes of the reconcile of `quader migrate` (R-REPO-067, Q-REPO-374): the repository service implements wellknown.Store over
// the migration session's transaction. Every write goes through the update and create paths of every repository, so the validation,
// the secrets, the levels, the audit events, the hooks and the repo_config change are the ones of any repository write.

// The reasons of the product's writes (R-REPO-067, R-REPO-068).
const (
	ReasonCatalog     = "catalog"
	ReasonSwitchedOn  = "switched_on"
	ReasonSwitchedOff = "switched_off"
	ReasonRetired     = "retired"
)

// CatalogActor is the actor of the reconcile's events: system:catalog (R-AUTHZ-041, R-REPO-067).
func CatalogActor() audit.Actor { return audit.Actor{Kind: "system", Name: "catalog"} }

// productCaller marks c as a write of the product with the reason of its events.
func productCaller(c Caller, reason string) Caller {
	c.product = productWrite{on: true, reason: reason}
	return c
}

// UpstreamStore returns the store the reconcile writes through, in tx, with the actor system:catalog.
func (s *Service) UpstreamStore(tx meta.Tx) wellknown.Store {
	return &upstreamStore{s: s, tx: tx, c: productCaller(Caller{Actor: CatalogActor()}, ReasonCatalog)}
}

type upstreamStore struct {
	s  *Service
	tx meta.Tx
	c  Caller
}

var _ wellknown.Store = (*upstreamStore)(nil)

func upstreamName(key string) qname.Repo { return qname.Repo{Project: wellknown.ProjectKey, Key: key} }

// Stored implements wellknown.Store.
func (u *upstreamStore) Stored(ctx context.Context) ([]wellknown.Stored, error) {
	qs, err := queries(u.tx)
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
	out := make([]wellknown.Stored, 0, len(rows))
	for _, row := range rows {
		d, err := toDocument(row, u.s.cat)
		if err != nil {
			return nil, fmt.Errorf("repository: stored document of upstream/%s: %w", row.Key, err)
		}
		f := facts[row.ID]
		st := wellknown.Stored{
			Key: row.Key, Format: catalog.ID(row.Format), Class: Class(row.Class), Kind: wellknown.Kind(f.Kind),
			Retired: f.RetiredAt.Valid, Overridden: f.AddressOverridden,
			Fixed: wellknown.Fixed{
				Private:     d.Visibility.Artifacts == LevelPrivate && d.Visibility.Wiki == LevelPrivate,
				ScanningOff: d.Vulnerabilities != nil && !d.Vulnerabilities.Enabled,
			},
		}
		if st.Fixed.Allow, err = productAllows(ctx, qs, row.ID, d); err != nil {
			return nil, err
		}
		// A local repository (scanner-clamav, R-REPO-075) has no remote section: no address, no account, no cache period.
		if rs, err := parseStoredRemote(d.Remote); d.Class == Remote && err == nil {
			st.HasAccount = rs.AuthMode != AuthNone || rs.Password != nil
			st.Address = storedAddress(rs, d)
			st.Fixed.UnusedRetentionHours = rs.Cache.UnusedRetentionHours
		}
		out = append(out, st)
	}
	return out, nil
}

// storedAddress is the address part of a stored remote repository (R-REPO-069).
func storedAddress(rs *RemoteSettings, d Document) wellknown.Address {
	a := wellknown.Address{URL: rs.URL, ExtraHosts: presetHosts(rs.ExtraHosts)}
	if m, ok := formatMemberOf(d.Format); ok {
		a.Settings = m.read(&d.Settings)
	}
	return a
}

func presetHosts(in []ExtraHost) []catalog.PresetHost {
	out := make([]catalog.PresetHost, 0, len(in))
	for _, h := range in {
		out = append(out, catalog.PresetHost{Pattern: h.Pattern, SendCredentials: h.SendCredentials})
	}
	return out
}

// Create implements wellknown.Store: the document a create of only key, class and format would store, with the declaration's
// address, its display name as the description, switched off for a preset (R-REPO-066), and the fixed members of a system
// repository (R-REPO-070). A local system repository (scanner-clamav, R-REPO-075, Q-REPO-411) has no address.
func (u *upstreamStore) Create(ctx context.Context, decl wellknown.Declaration) error {
	d := Document{Key: decl.Key, Class: Remote, Format: string(decl.Format)}
	d.Description = decl.DisplayName
	d.Disabled = decl.Kind == wellknown.KindPreset
	if decl.Local() {
		d.Class = Local
	} else if err := setAddress(&d, decl.Address(), true); err != nil {
		return err
	}
	if decl.Kind == wellknown.KindSystem {
		if err := u.s.setFixed(&d, decl.Fixed()); err != nil {
			return err
		}
	}
	d.normalize()
	c := u.c
	c.product.level = decl.Level
	st, err := u.s.createTx(ctx, u.tx, c, wellknown.ProjectKey, d, decl.Kind)
	if err != nil || decl.Kind != wellknown.KindSystem || decl.Local() {
		return described(err)
	}
	return u.s.writeProductRules(ctx, u.tx, st.Repository, decl.Fixed().Allow)
}

// described adds the field errors of a refused write to its message: `quader migrate` logs the error and exits, and a catalog
// whose declaration does not validate names the member there.
func described(err error) error {
	e, ok := errs.As(err)
	if !ok || len(e.Fields) == 0 {
		return err
	}
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Pointer+": "+f.Code+" ("+f.Detail+")")
	}
	return fmt.Errorf("%w: %s", err, strings.Join(parts, "; "))
}

// Write implements wellknown.Store: the product's address and the fixed members in one update (R-REPO-067 step 2), the account
// removed with it when the product moved its hosts (Q-REPO-377).
func (u *upstreamStore) Write(ctx context.Context, decl wellknown.Declaration, w wellknown.Write) error {
	c := u.c
	c.product.accountRemoved = w.RemoveAccount
	st, err := u.s.updateTx(ctx, u.tx, c, upstreamName(decl.Key), MatchAny(), func(cur Document) (Document, error) {
		nd := cur
		if w.Address {
			if err := setAddress(&nd, decl.Address(), false); err != nil {
				return Document{}, err
			}
		}
		if w.RemoveAccount {
			if err := setRemote(&nd, map[string]any{"authMode": AuthNone, "username": "", "password": nil}); err != nil {
				return Document{}, err
			}
		}
		if w.Fixed {
			if err := u.s.setFixed(&nd, decl.Fixed()); err != nil {
				return Document{}, err
			}
		}
		return nd, nil
	})
	if err == nil && w.Fixed && !decl.Local() {
		err = u.s.writeProductRules(ctx, u.tx, st.Repository, decl.Fixed().Allow)
	}
	if err != nil || !w.RemoveAccount {
		return err
	}
	qs, err := queries(u.tx)
	if err != nil {
		return err
	}
	return qs.SetUpstreamAccountRemoved(ctx, st.Repository.NumericID())
}

// SetOverridden implements wellknown.Store; the flag is no fact of the snapshot, so no change is noted.
func (u *upstreamStore) SetOverridden(ctx context.Context, key string, overridden bool) error {
	id, err := u.id(ctx, key)
	if err != nil {
		return err
	}
	qs, err := queries(u.tx)
	if err != nil {
		return err
	}
	return qs.SetUpstreamAddressOverridden(ctx, q.SetUpstreamAddressOverriddenParams{Overridden: overridden, RepositoryID: id})
}

// Retire implements wellknown.Store (R-REPO-067 step 5, Q-REPO-376): retired_at, the switch-off with repository.disabled and the
// reason retired (journaled), and the purge job. The rows stay as a tombstone.
func (u *upstreamStore) Retire(ctx context.Context, key string) error {
	name := upstreamName(key)
	qs, err := queries(u.tx)
	if err != nil {
		return err
	}
	rows, err := qs.LockRepositoryByName(ctx, q.LockRepositoryByNameParams{Project: wellknown.ProjectKey, Key: key})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return notFound(name)
	}
	id := rows[0].ID
	// retired_at first, so that the snapshot the change rebuilds reads the state retired.
	if err := qs.SetUpstreamRetired(ctx, id); err != nil {
		return err
	}
	c := productCaller(u.c, ReasonRetired)
	if r, _ := toRepository(rows[0], wellknown.ProjectKey, u.s.cat); r.Disabled {
		// Already off: the retirement still records repository.disabled, journaled, so a restore keeps it off (Q-AUTHZ-238).
		if err := u.s.recordDisabled(ctx, u.tx, c, r); err != nil {
			return err
		}
		if err := u.s.notify(u.tx, r); err != nil {
			return err
		}
	} else if _, err := u.s.updateTx(ctx, u.tx, c, name, MatchAny(), func(cur Document) (Document, error) {
		cur.Disabled = true
		return cur, nil
	}); err != nil {
		return err
	}
	if u.s.jobs == nil {
		return errors.New("repository: no job queue, so a retired repository cannot be purged")
	}
	_, err = u.s.jobs.Enqueue(ctx, u.tx, jobs.EnqueueSpec{
		Kind: UpstreamPurgeJobKind, DedupKey: UpstreamPurgeJobKind + ":" + strconv.FormatInt(id, 10),
		Args: UpstreamPurgeArgs{RepoID: id, Key: name.String()}, TargetKind: "repository", TargetKey: name.String(),
	})
	return err
}

// Unretire implements wellknown.Store (R-REPO-067 step 4): the repository stays switched off; every replica reads its new state.
func (u *upstreamStore) Unretire(ctx context.Context, key string) error {
	qs, err := queries(u.tx)
	if err != nil {
		return err
	}
	rows, err := qs.LockRepositoryByName(ctx, q.LockRepositoryByNameParams{Project: wellknown.ProjectKey, Key: key})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return notFound(upstreamName(key))
	}
	if err := qs.ClearUpstreamRetired(ctx, rows[0].ID); err != nil {
		return err
	}
	r, _ := toRepository(rows[0], wellknown.ProjectKey, u.s.cat)
	return u.s.notify(u.tx, r)
}

func (u *upstreamStore) id(ctx context.Context, key string) (int64, error) {
	qs, err := queries(u.tx)
	if err != nil {
		return 0, err
	}
	rows, err := qs.GetRepositoryByName(ctx, q.GetRepositoryByNameParams{Project: wellknown.ProjectKey, Key: key})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, notFound(upstreamName(key))
	}
	return rows[0].ID, nil
}

// notify notes a change of the repository's facts in the change area repo_config, so every replica rebuilds its snapshot.
func (s *Service) notify(tx meta.Tx, r Repository) error {
	return s.notif.Notify(tx, cluster.AreaRepoConfig, 1, changePayload{ID: r.ID, Key: r.Name.String(), Op: "updated", Version: r.Version})
}

// setAddress writes an address into a document: remote.url and remote.extraHosts, and the members of the format's settings member
// the address carries. create writes the whole remote section afresh.
func setAddress(d *Document, a wellknown.Address, create bool) error {
	hosts := make([]ExtraHost, 0, len(a.ExtraHosts))
	for _, h := range a.ExtraHosts {
		hosts = append(hosts, ExtraHost{Pattern: h.Pattern, SendCredentials: h.SendCredentials})
	}
	if create {
		d.Remote = nil
	}
	if err := setRemote(d, map[string]any{"url": a.URL, "extraHosts": hosts}); err != nil {
		return err
	}
	return setFormatMembers(d, a.Settings)
}

// setFormatMembers writes the members of settings into the document's settings member of its format.
func setFormatMembers(d *Document, settings json.RawMessage) error {
	want := objectMembers(settings)
	if len(want) == 0 {
		return nil
	}
	m, ok := formatMemberOf(d.Format)
	if !ok {
		return fmt.Errorf("repository: the format %s has no settings member for the preset's settings", d.Format)
	}
	have := objectMembers(m.read(&d.Settings))
	if have == nil {
		have = map[string]json.RawMessage{}
	}
	for k, v := range want {
		have[k] = v
	}
	raw, err := json.Marshal(have)
	if err != nil {
		return err
	}
	m.write(&d.Settings, raw)
	return nil
}

// setRemote writes members of the remote section; a nil value writes null.
func setRemote(d *Document, set map[string]any) error {
	m := objectMembers(d.Remote)
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	for k, v := range set {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		m[k] = raw
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	d.Remote = raw
	return nil
}

// setFixed writes the members the product fixes on a system repository (R-REPO-070, R-REPO-075): both levels private, the scanning off
// where the format has it, and for a remote one the cache period and the delivery mode onlyAllowed of its product rules (the rules
// themselves are rows, writeProductRules).
func (s *Service) setFixed(d *Document, f wellknown.Fixed) error {
	if f.Private {
		d.Visibility = Visibility{Artifacts: LevelPrivate, Wiki: LevelPrivate}
	}
	if info, ok := s.cat.Lookup(d.Format); ok && f.ScanningOff && (info.Ecosystem != "" || info.ImageEcosystem != "") {
		off := vuln.DefaultSettings()
		off.Enabled = false
		d.Vulnerabilities = &off
	}
	if d.Class != Remote {
		return nil
	}
	cache := objectMembers(objectMembers(d.Remote)["cache"])
	if cache == nil {
		cache = map[string]json.RawMessage{}
	}
	raw, err := json.Marshal(f.UnusedRetentionHours)
	if err != nil {
		return err
	}
	cache["unusedRetentionHours"] = raw
	section := map[string]any{"cache": cache}
	if len(f.Allow) > 0 {
		delivery := objectMembers(objectMembers(d.Remote)["delivery"])
		if delivery == nil {
			delivery = map[string]json.RawMessage{}
		}
		delivery["mode"] = json.RawMessage(`"` + string(rc.OnlyAllowed) + `"`)
		section["delivery"] = delivery
	}
	return setRemote(d, section)
}

// productAllows reads the product's delivery rules of a remote system repository as the reconcile compares them (R-REPO-070): the names
// of its package rules that allow, sorted, in mode onlyAllowed; a repository with any other rule, or in the other mode, reads as one
// without them, so the reconcile writes them again.
func productAllows(ctx context.Context, qs *q.Queries, id int64, d Document) ([]string, error) {
	rs, err := parseStoredRemote(d.Remote)
	if d.Class != Remote || err != nil || rs.Delivery.Mode != string(rc.OnlyAllowed) {
		return nil, nil //nolint:nilerr // a section that does not read holds no product rule
	}
	rows, err := qs.DeliveryRulesOfRepository(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.Kind != string(rc.KindPackage) || r.Effect != string(rc.Allow) {
			return nil, nil
		}
		out = append(out, r.Name.String)
	}
	slices.Sort(out)
	return out, nil
}

// writeProductRules makes the product's delivery rules of a system repository the only ones (R-REPO-070): every rule removed, a
// `package <name> allow` per name, the rule set's revision raised and the change area told, so every replica rebuilds the repository's
// rule set. A rule write through the API on such a repository answers 409 managed_by_product.
func (s *Service) writeProductRules(ctx context.Context, tx meta.Tx, r Repository, allow []string) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	id := r.NumericID()
	rows, err := qs.DeliveryRulesOfRepository(ctx, id)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := qs.DeliveryRuleDelete(ctx, q.DeliveryRuleDeleteParams{RepoID: id, ID: row.ID.String()}); err != nil {
			return err
		}
	}
	for _, name := range allow {
		n, ok := s.cat.NormalizeName(r.Format, name)
		if !ok {
			return fmt.Errorf("repository: the product's rule names %q, which is no name of %s", name, r.Format)
		}
		if err := qs.DeliveryRuleInsert(ctx, q.DeliveryRuleInsertParams{
			ID: uuid.Must(uuid.NewV7()).String(), RepoID: id, Kind: string(rc.KindPackage), Name: n, Effect: string(rc.Allow),
			Note: "The product's rule (R-REPO-070).",
		}); err != nil {
			return err
		}
	}
	if _, err := qs.DeliveryRulesRaiseRevision(ctx, id); err != nil {
		return err
	}
	return s.notif.Notify(tx, cluster.AreaRepoConfig, 1, changePayload{ID: r.ID, Key: r.Name.String(), Op: "updated", Version: r.Version})
}

// UpstreamPurgeJobKind is the job kind that removes the content of a retired well-known repository and keeps its rows
// (R-REPO-067 step 5, Q-REPO-376).
const UpstreamPurgeJobKind = "upstream-purge"

// UpstreamPurgeJobSpec is the registration of the kind, exclusive per repository.
func UpstreamPurgeJobSpec(h jobs.Handler) jobs.KindSpec {
	return jobs.KindSpec{Handler: h, DedupMode: jobs.Exclusive}
}

// UpstreamPurgeArgs are the arguments of a purge job.
type UpstreamPurgeArgs struct {
	RepoID int64  `json:"repoId"`
	Key    string `json:"key"`
}

// RunUpstreamPurgeJob is the handler of [UpstreamPurgeJobKind]: the items in batches as the deletion job removes them, then in one
// transaction what the row's removal would have taken with it, by upstream_repository_purge (last replaced by migration 00096). A
// repository the catalog declared again before the end keeps what is left.
func (s *Service) RunUpstreamPurgeJob(ctx context.Context, run *jobs.Run) error {
	var a UpstreamPurgeArgs
	if err := json.Unmarshal(run.Args, &a); err != nil {
		return jobs.Permanent(fmt.Errorf("upstream-purge: arguments: %w", err))
	}
	retired, err := s.retired(ctx, a.RepoID)
	if err != nil || !retired {
		return err
	}
	total, err := s.countItems(ctx, a.RepoID)
	if err != nil {
		return err
	}
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		n, err := s.deleteBatch(ctx, a.RepoID)
		if err != nil {
			return err
		}
		done += n
		if err := run.SaveProgress(ctx, done, max(total, done), "items"); err != nil {
			return err
		}
		if n < int64(s.batch) {
			break
		}
	}
	return s.run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		facts, err := qs.GetUpstreamRepositoryFacts(ctx, a.RepoID)
		if err != nil || len(facts) == 0 || !facts[0].RetiredAt.Valid {
			return err
		}
		return qs.PurgeUpstreamRepository(ctx, a.RepoID)
	})
}

func (s *Service) retired(ctx context.Context, id int64) (bool, error) {
	var out bool
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		facts, err := qs.GetUpstreamRepositoryFacts(ctx, id)
		out = err == nil && len(facts) == 1 && facts[0].RetiredAt.Valid
		return err
	})
	return out, err
}
