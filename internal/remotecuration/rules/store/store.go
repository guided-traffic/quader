package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/remotecuration/rules"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// The store of the delivery rules (R-CUR-006): one rule per subject, written in the canonical forms of the format, each write in one
// transaction with the revision of the rule set raised, the change area repo_config recorded (every replica rebuilds its snapshot of
// the repository, R-ARCH-018), the rule's audit event and, for a write that narrows, repository.narrowed with the part rules.

// Notifier attaches a change of a change area to the writing transaction (R-ARCH-018 step 1); *cluster.Changes implements it.
type Notifier interface {
	Notify(tx meta.Tx, area string, version int, payload any) error
}

// ChangeHook runs in the transaction of every write of a repository's rules: the Debian format's virtual repositories that list the
// remote rebuild their merge (R-DEB-036). Nil does nothing.
type ChangeHook func(ctx context.Context, tx meta.Tx, t Target) error

// Options configure [NewStore].
type Options struct {
	Runner   meta.Runner
	Recorder audit.Recorder
	Notifier Notifier
	// Catalog folds names and versions; nil is catalog.Default.
	Catalog *catalog.Registry
	// Compile checks a section pattern with the one matcher of names (the repository model's CompileSection).
	Compile rules.Compiler
	// MaxRules is proxy.delivery.max_rules; zero is rules.DefaultMaxRules.
	MaxRules int
	// OnChange is optional.
	OnChange ChangeHook
}

// Store writes and lists the rules.
type Store struct {
	o Options
}

// NewStore returns the store.
func NewStore(o Options) (*Store, error) {
	if o.Runner == nil || o.Recorder == nil || o.Notifier == nil || o.Compile == nil {
		return nil, errors.New("rules: the store needs a runner, a recorder, a notifier and a pattern compiler")
	}
	o.Catalog = catalog.Or(o.Catalog)
	if o.MaxRules <= 0 {
		o.MaxRules = rules.DefaultMaxRules
	}
	return &Store{o: o}, nil
}

// Target is the remote repository a write or a list acts on.
type Target struct {
	ID     int64
	Name   qname.Repo
	Format string
}

// Caller is who writes, as the audit event names it.
type Caller struct {
	Actor  audit.Actor
	Source audit.Source
	// PrincipalID is the principal's ID (uuid text), "" for none (a break-glass administrator).
	PrincipalID string
}

// Input is a rule as a request writes it, before the format folds it (R-CUR-006).
type Input struct {
	Kind    rc.RuleKind
	Pattern string
	Name    string
	Version string
	Effect  rc.Effect
	Note    string
	// RequestID is the request entry whose allow writes the rule (R-CUR-015); "" for none.
	RequestID string
}

// Stored is a rule as the store holds it.
type Stored struct {
	ID string
	rules.Rule
	Note          string
	RequestID     string
	CreatedByID   string
	CreatedByName string
	CreatedAt     time.Time
}

// Written is the answer of a write: the rule, whether it was created (a new subject) or replaced, and the rule set's revision.
type Written struct {
	Stored
	Created  bool
	Revision int64
}

// Canonical checks a rule as a request writes it and folds it by the format's rules (R-CUR-006): a name the format refuses answers
// 422 invalid_name, a version it cannot read 422 invalid_version, a pattern the matcher refuses 422 invalid_pattern, each at its
// pointer.
func (s *Store) Canonical(t Target, in Input) (rules.Rule, error) {
	var fields []errs.FieldError
	add := func(ptr, code, detail string) {
		fields = append(fields, errs.FieldError{Pointer: ptr, Code: code, Detail: detail})
	}
	r := rules.Rule{Kind: in.Kind, Effect: in.Effect}
	if in.Effect != rc.Allow && in.Effect != rc.Block {
		add("/effect", "invalid_value", `must be "allow" or "block"`)
	}
	if utf8.RuneCountInString(in.Note) > rules.MaxNoteLength {
		add("/note", "out_of_range", "at most 1000 characters")
	}
	switch in.Kind {
	case rc.KindSection:
		if in.Name != "" || in.Version != "" {
			add("/name", "not_applicable", "a section rule holds a pattern, no name or version")
		}
		p, ok := s.o.Catalog.NormalizeName(t.Format, in.Pattern)
		if !ok || in.Pattern == "" {
			add("/pattern", "invalid_pattern", "the pattern is no name pattern of the format")
			break
		}
		if _, err := s.o.Compile(p); err != nil {
			add("/pattern", "invalid_pattern", err.Error())
			break
		}
		r.Pattern = p
	case rc.KindPackage, rc.KindVersion:
		if in.Pattern != "" {
			add("/pattern", "not_applicable", "a package or version rule names a package, no pattern")
		}
		n, ok := s.o.Catalog.NormalizeName(t.Format, in.Name)
		if !ok || in.Name == "" || strings.ContainsAny(n, "*?[]{}") || len(n) > rules.MaxNameLength || hasControl(n) {
			add("/name", errs.CodeInvalidName, "the name is no package name of the format")
		} else {
			r.Name = n
		}
		if in.Kind == rc.KindPackage {
			if in.Version != "" {
				add("/version", "not_applicable", "a package rule holds every version; a version rule names one")
			}
			break
		}
		v, ok := s.o.Catalog.CanonicalVersion(t.Format, in.Version)
		if !ok || in.Version == "" || len(v) > rules.MaxVersionLength || hasControl(v) {
			add("/version", errs.CodeInvalidVersion, "the version is not one the format can read; a range is not a version")
		} else {
			r.Version = v
		}
	default:
		add("/kind", "invalid_value", `must be "section", "package" or "version"`)
	}
	if len(fields) == 0 {
		return r, nil
	}
	// One finding of its own code is answered by that code (R-CUR-006: invalid_name, invalid_version, invalid_pattern); several are
	// validation_failed, each at its pointer.
	code := errs.CodeValidation
	if len(fields) == 1 {
		switch fields[0].Code {
		case errs.CodeInvalidName, errs.CodeInvalidVersion, errs.CodeInvalidPattern:
			code = fields[0].Code
		}
	}
	return rules.Rule{}, errs.E(errs.Unprocessable, code, fields[0].Detail+".", errs.WithFields(fields...))
}

func hasControl(s string) bool {
	for _, c := range s {
		if unicode.IsControl(c) {
			return true
		}
	}
	return false
}

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errors.New("rules: the transaction is not one of internal/meta/postgres")
	}
	return q.New(ptx), nil
}

// Put creates the rule of a subject, or replaces the effect and note of the subject's rule (R-CUR-006 "One rule per subject"), in a
// transaction of its own.
func (s *Store) Put(ctx context.Context, t Target, c Caller, in Input) (Written, error) {
	var out Written
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.PutTx(ctx, tx, t, c, in)
		return err
	})
	return out, err
}

// PutTx is Put in the caller's transaction: the API's idempotent write and a request's allow (R-CUR-015) write the rule beside their
// own rows.
func (s *Store) PutTx(ctx context.Context, tx meta.Tx, t Target, c Caller, in Input) (Written, error) {
	r, err := s.Canonical(t, in)
	if err != nil {
		return Written{}, err
	}
	var out Written
	err = func() error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		// The revision's row lock orders the writes of one repository's rules: the limits below count under it.
		rev, err := qs.DeliveryRulesRaiseRevision(ctx, t.ID)
		if err != nil {
			return err
		}
		have, err := qs.DeliveryRuleBySubject(ctx, q.DeliveryRuleBySubjectParams{
			RepoID: t.ID, Kind: string(r.Kind), Pattern: r.Pattern, Name: r.Name, Version: r.Version,
		})
		if err != nil {
			return err
		}
		var previous rc.Effect
		id := ""
		if len(have) > 0 {
			id, previous = have[0].ID.String(), rc.Effect(have[0].Effect)
			if err := qs.DeliveryRuleReplace(ctx, q.DeliveryRuleReplaceParams{ID: id, Effect: string(r.Effect), Note: in.Note, RequestID: in.RequestID}); err != nil {
				return err
			}
		} else {
			if err := s.withinLimits(ctx, qs, t.ID, r.Kind); err != nil {
				return err
			}
			id = uuid.Must(uuid.NewV7()).String()
			if err := qs.DeliveryRuleInsert(ctx, q.DeliveryRuleInsertParams{
				ID: id, RepoID: t.ID, Kind: string(r.Kind), Pattern: r.Pattern, Name: r.Name, Version: r.Version, Effect: string(r.Effect),
				Note: in.Note, RequestID: in.RequestID, CreatedBy: c.PrincipalID,
			}); err != nil {
				return err
			}
		}
		event := audit.EventRemoteRuleCreated
		if previous != "" {
			event = audit.EventRemoteRuleUpdated
		}
		if err := s.record(ctx, tx, t, c, event, id, r, previous, in.Note, in.RequestID); err != nil {
			return err
		}
		// A block, or an allow turned into a block, narrows what the repository delivers (R-CUR-006 "Journal").
		if r.Effect == rc.Block && previous != rc.Block {
			n := &rules.Narrowing{BlocksAdded: []rules.Subject{rules.SubjectOfRule(r)}}
			if err := s.narrowed(ctx, tx, t, c, n); err != nil {
				return err
			}
		}
		if err := s.changed(ctx, tx, t); err != nil {
			return err
		}
		rows, err := qs.DeliveryRuleGet(ctx, q.DeliveryRuleGetParams{RepoID: t.ID, ID: id})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errors.New("rules: the rule written is not there")
		}
		out = Written{Stored: storedOf(rows[0]), Created: previous == "", Revision: rev}
		return nil
	}()
	return out, err
}

// withinLimits refuses a rule beyond proxy.delivery.max_rules or a 101st section rule (422 too_many_rules).
func (s *Store) withinLimits(ctx context.Context, qs *q.Queries, repoID int64, kind rc.RuleKind) error {
	n, err := qs.DeliveryRuleCounts(ctx, repoID)
	if err != nil {
		return err
	}
	switch {
	case n.Rules >= int64(s.o.MaxRules):
		return errs.E(errs.Unprocessable, errs.CodeTooManyRules,
			"The repository holds "+strconv.Itoa(s.o.MaxRules)+" delivery rules, the most it may (proxy.delivery.max_rules).")
	case kind == rc.KindSection && n.Sections >= rules.MaxSections:
		return errs.E(errs.Unprocessable, errs.CodeTooManyRules, "The repository holds 100 section rules, the most it may.")
	}
	return nil
}

// Delete removes a rule in a transaction of its own; 404 not_found for a rule the repository does not hold.
func (s *Store) Delete(ctx context.Context, t Target, c Caller, id string) error {
	return s.o.Runner.InTx(ctx, func(tx meta.Tx) error { return s.DeleteTx(ctx, tx, t, c, id) })
}

// DeleteTx is Delete in the caller's transaction.
func (s *Store) DeleteTx(ctx context.Context, tx meta.Tx, t Target, c Caller, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return errs.E(errs.NotFound, errs.CodeNotFound, "The repository holds no rule of this ID.")
	}
	return func() error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if _, err := qs.DeliveryRulesRaiseRevision(ctx, t.ID); err != nil {
			return err
		}
		rows, err := qs.DeliveryRuleDelete(ctx, q.DeliveryRuleDeleteParams{RepoID: t.ID, ID: id})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errs.E(errs.NotFound, errs.CodeNotFound, "The repository holds no rule of this ID.")
		}
		row := rows[0]
		r := rules.Rule{Kind: rc.RuleKind(row.Kind), Pattern: row.Pattern.String, Name: row.Name.String, Version: row.Version.String, Effect: rc.Effect(row.Effect)}
		if err := s.record(ctx, tx, t, c, audit.EventRemoteRuleDeleted, id, r, r.Effect, row.Note, uuidText(row.RequestID.Valid, row.RequestID.String())); err != nil {
			return err
		}
		// Removing an allow narrows what the repository delivers (R-CUR-006 "Journal").
		if r.Effect == rc.Allow {
			if err := s.narrowed(ctx, tx, t, c, &rules.Narrowing{AllowsRemoved: []rules.Subject{rules.SubjectOfRule(r)}}); err != nil {
				return err
			}
		}
		return s.changed(ctx, tx, t)
	}()
}

func uuidText(valid bool, s string) string {
	if !valid {
		return ""
	}
	return s
}

// changed tells every replica that the repository's snapshot entry changed (R-CUR-006 "Propagation") and runs the change hook.
func (s *Store) changed(ctx context.Context, tx meta.Tx, t Target) error {
	payload := map[string]any{"id": strconv.FormatInt(t.ID, 10), "key": t.Name.String(), "op": "updated"}
	if err := s.o.Notifier.Notify(tx, cluster.AreaRepoConfig, 1, payload); err != nil {
		return err
	}
	if s.o.OnChange != nil {
		return s.o.OnChange(ctx, tx, t)
	}
	return nil
}

// record writes the rule's event (R-CUR-019).
func (s *Store) record(ctx context.Context, tx meta.Tx, t Target, c Caller, event, id string, r rules.Rule, previous rc.Effect, note, requestID string) error {
	d := map[string]any{"ruleId": id, "kind": string(r.Kind), "subject": r.Subject(), "effect": string(r.Effect), "note": note}
	if previous != "" && event != audit.EventRemoteRuleCreated {
		d["previousEffect"] = string(previous)
	} else {
		d["previousEffect"] = nil
	}
	if event == audit.EventRemoteRuleDeleted {
		d["previousEffect"] = string(previous)
	}
	if requestID != "" {
		d["requestId"] = requestID
	} else {
		d["requestId"] = nil
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.o.Recorder.RecordTx(ctx, tx, audit.Event{
		Type: event, Actor: c.Actor, Outcome: audit.Success, Source: c.Source,
		Subject:    audit.Subject{Kind: "repository", ID: strconv.FormatInt(t.ID, 10), Name: t.Name.String()},
		Repository: t.Name.String(), Project: t.Name.Project, Details: raw,
	})
}

// narrowedEntry is the journal entry of repository.narrowed as the repository model's replay reads it (narrowedEntry of
// internal/repository): the repository and the part rules.
type narrowedEntry struct {
	RepositoryID string           `json:"repositoryId"`
	Key          string           `json:"key"`
	Rules        *rules.Narrowing `json:"rules"`
}

// narrowed records repository.narrowed with the part rules beside the rule's own event (R-CUR-006, R-REPO-061).
func (s *Store) narrowed(ctx context.Context, tx meta.Tx, t Target, c Caller, n *rules.Narrowing) error {
	entry, err := json.Marshal(narrowedEntry{RepositoryID: strconv.FormatInt(t.ID, 10), Key: t.Name.String(), Rules: n})
	if err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"class": "remote", "format": t.Format, "project": t.Name.Project, "parts": []string{"rules"}})
	if err != nil {
		return err
	}
	return s.o.Recorder.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventRepositoryNarrowed, Actor: c.Actor, Outcome: audit.Success, Source: c.Source,
		Subject:    audit.Subject{Kind: "repository", ID: strconv.FormatInt(t.ID, 10), Name: t.Name.String()},
		Repository: t.Name.String(), Project: t.Name.Project, Details: details, Journal: entry,
	})
}

// ListFilter narrows a list (R-CUR-006): the kind, the effect and a substring of the subject; each "" for any.
type ListFilter struct {
	Kind, Effect, Q string
}

// List reads one page of a repository's rules, newest first, after the keyset (afterAt, afterID) of the previous page ("" for the
// first page), at most limit rows.
func (s *Store) List(ctx context.Context, repoID int64, f ListFilter, afterAt, afterID string, limit int) ([]Stored, error) {
	var out []Stored
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.DeliveryRulesList(ctx, q.DeliveryRulesListParams{
			RepoID: repoID, Kind: f.Kind, Effect: f.Effect, Q: f.Q, AfterAt: afterAt, AfterID: afterID, RowLimit: int32(min(limit, 1<<20)), //nolint:gosec // bounded
		})
		if err != nil {
			return err
		}
		out = make([]Stored, len(rows))
		for i, r := range rows {
			out[i] = storedOf(q.DeliveryRuleGetRow(r))
		}
		return nil
	})
	return out, err
}

func storedOf(r q.DeliveryRuleGetRow) Stored {
	out := Stored{
		ID:   r.ID.String(),
		Rule: rules.Rule{Kind: rc.RuleKind(r.Kind), Pattern: r.Pattern.String, Name: r.Name.String, Version: r.Version.String, Effect: rc.Effect(r.Effect)},
		Note: r.Note, CreatedByName: r.CreatedByName, CreatedAt: r.CreatedAt.Time.UTC(),
	}
	if r.RequestID.Valid {
		out.RequestID = r.RequestID.String()
	}
	if r.CreatedBy.Valid {
		out.CreatedByID = r.CreatedBy.String()
	}
	return out
}
