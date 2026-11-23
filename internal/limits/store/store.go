// Package store is the PostgreSQL store of the download limits (usage-and-limits.md R-LIM-010 to R-LIM-016, R-LIM-025,
// R-LIM-026): profiles, rules, ceilings, project maxima, settings and the record of blocks. Every write is audited in its
// transaction (R-LIM-030) and announces the change area limits (R-ARCH-018), so every replica rebuilds its index. No
// request reads it: [Store.Snapshot] is what the index is compiled from.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Notifier announces a change area in a transaction; *cluster.Changes implements it.
type Notifier interface {
	Notify(tx meta.Tx, area string, version int, payload any) error
}

// Options configure [New].
type Options struct {
	Runner  meta.Runner
	Changes Notifier
	Audit   audit.Recorder
	Log     *slog.Logger
}

// Store is the store.
type Store struct{ o Options }

// New creates the store.
func New(o Options) *Store {
	if o.Audit == nil {
		o.Audit = audit.Nop{}
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Store{o: o}
}

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errors.New("limits: not a PostgreSQL transaction")
	}
	return q.New(ptx), nil
}

// in runs fn in a transaction.
func (s *Store) in(ctx context.Context, fn func(qs *q.Queries, tx meta.Tx) error) error {
	return s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return fn(qs, tx)
	})
}

// changed announces the area and records the audit event in the write's transaction.
func (s *Store) changed(ctx context.Context, tx meta.Tx, what string, ev audit.Event) error {
	if s.o.Changes != nil {
		if err := s.o.Changes.Notify(tx, cluster.AreaLimits, 1, map[string]string{"what": what}); err != nil {
			return err
		}
	}
	if ev.Type == "" {
		return nil
	}
	if ev.Outcome == "" {
		ev.Outcome = audit.Success
	}
	return s.o.Audit.RecordTx(ctx, tx, ev)
}

func details(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// figuresJSON is a set of figures as the store's queries read it: column name to value, null for unlimited.
func figuresJSON(f limits.Figures) string {
	m := map[string]*int64{}
	for i := range f {
		if f[i] > 0 {
			v := f[i]
			m[limits.Figure(i).Column()] = &v
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func overridesJSON(o limits.Overrides) string {
	b, _ := json.Marshal(o)
	return string(b)
}

// Snapshot reads everything the index is compiled from.
func (s *Store) Snapshot(ctx context.Context) (limits.Snapshot, error) {
	var snap limits.Snapshot
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		profiles, err := qs.LimitProfiles(ctx)
		if err != nil {
			return err
		}
		for _, p := range profiles {
			snap.Profiles = append(snap.Profiles, profileOf(p))
		}
		rules, err := qs.LimitAssignments(ctx)
		if err != nil {
			return err
		}
		for _, r := range rules {
			snap.Rules = append(snap.Rules, ruleOf(r))
		}
		ceilings, err := qs.LimitCeilings(ctx)
		if err != nil {
			return err
		}
		for _, c := range ceilings {
			snap.Ceilings = append(snap.Ceilings, limits.Ceiling{
				RepoID: c.RepositoryID, Class: limits.CeilingClass(c.Class), UncachedBytesPerHour: c.UncachedBytesPerHour.Int64,
				UpstreamRequestsPerHour: c.UpstreamRequestsPerHour.Int64, Version: c.Version, UpdatedAt: c.UpdatedAt.Time, UpdatedBy: c.UpdatedBy,
			})
		}
		maxima, err := qs.LimitProjectMaxima(ctx)
		if err != nil {
			return err
		}
		for _, m := range maxima {
			var o limits.Overrides
			_ = json.Unmarshal(m.Overrides, &o)
			snap.Maxima = append(snap.Maxima, limits.Maximum{
				ProjectID: m.ProjectID, ProjectKey: m.ProjectKey, Profile: m.Profile, Overrides: o, Version: m.Version,
				UpdatedAt: m.UpdatedAt.Time, UpdatedBy: m.UpdatedBy,
			})
		}
		st, err := qs.LimitSettings(ctx)
		if err != nil {
			return err
		}
		snap.Settings = limits.Settings{
			Mode: limits.Mode(st.Mode), BlockSeconds: int(st.BlockSeconds), Version: st.Version,
			UpdatedAt: st.UpdatedAt.Time, UpdatedBy: st.UpdatedBy,
		}
		return nil
	})
	return snap, err
}

func profileOf(p q.LimitProfilesRow) limits.Profile {
	f := limits.Figures{
		p.RequestsPerSecond.Int64, p.RequestsBurst.Int64, p.RequestsPerHour.Int64, p.CachedBytesPerSecond.Int64,
		p.CachedBytesPerHour.Int64, p.UncachedBytesPerSecond.Int64, p.UncachedBytesPerHour.Int64,
		p.UpstreamRequestsPerHour.Int64, p.ParallelDownloads.Int64, p.BytesPerDay.Int64, p.RefusedPerHour.Int64,
	}
	return limits.Profile{
		ID: p.ID, Key: p.Key, Name: p.Name, Description: p.Description, Builtin: p.Builtin, Figures: f, Version: p.Version,
		CreatedAt: p.CreatedAt.Time, CreatedBy: p.CreatedBy, UpdatedAt: p.UpdatedAt.Time, UpdatedBy: p.UpdatedBy,
	}
}

func ruleOf(r q.LimitAssignmentsRow) limits.Rule {
	var o limits.Overrides
	_ = json.Unmarshal(r.Overrides, &o)
	place := limits.InstancePlace()
	switch r.PlaceKind {
	case "project":
		place = limits.Place{Kind: limits.PlaceProject, ProjectID: r.ProjectID, ProjectKey: r.ProjectKey}
	case "repository":
		place = limits.Place{Kind: limits.PlaceRepository, RepoID: r.RepositoryID, ProjectID: r.RepoProjectID, ProjectKey: r.ProjectKey}
		place.Repo.Project, place.Repo.Key = r.ProjectKey, r.RepoKey
	}
	return limits.Rule{
		ID: r.ID, Place: place, WhoKind: limits.WhoKind(r.WhoKind), WhoRef: r.WhoRef, Profile: r.Profile, Overrides: o,
		Capped: r.Capped, Builtin: r.Builtin, Note: r.Note, Version: r.Version,
		CreatedAt: r.CreatedAt.Time, CreatedBy: r.CreatedBy, UpdatedAt: r.UpdatedAt.Time, UpdatedBy: r.UpdatedBy,
	}
}

// ProfileUsage counts the rules and project maxima per profile key (R-LIM-031 usedBy).
func (s *Store) ProfileUsage(ctx context.Context) (map[string][2]int64, error) {
	out := map[string][2]int64{}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.LimitProfileUsage(ctx)
		for _, r := range rows {
			out[r.Key] = [2]int64{r.Assignments, r.Maxima}
		}
		return err
	})
	return out, err
}

// CreateProfile stores an own profile (R-LIM-031).
func (s *Store) CreateProfile(ctx context.Context, actor audit.Actor, p limits.Profile) error {
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		if err := qs.InsertLimitProfile(ctx, q.InsertLimitProfileParams{
			ID: uuid.Must(uuid.NewV7()).String(), Key: p.Key, Name: p.Name, Description: p.Description,
			Figures: figuresJSON(p.Figures), Actor: actor.ID,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, "profile", audit.Event{
			Type: audit.EventLimitsProfileCreated, Actor: actor,
			Details: details(map[string]any{"key": p.Key, "changes": map[string]any{"name": p.Name, "figures": p.Figures}}),
		})
	})
	if postgres.IsState(err, pgUniqueViolation) {
		return errs.E(errs.Conflict, errs.CodeAlreadyExists, "A profile with this key or name exists already.")
	}
	return err
}

// UpdateProfile replaces a profile's name, description and figures when its version matches.
func (s *Store) UpdateProfile(ctx context.Context, actor audit.Actor, key string, version int64, p limits.Profile) error {
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.UpdateLimitProfile(ctx, q.UpdateLimitProfileParams{
			Key: key, Version: version, Name: p.Name, Description: p.Description, Figures: figuresJSON(p.Figures), Actor: actor.ID,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The profile", 0)
		}
		return s.changed(ctx, tx, "profile", audit.Event{
			Type: audit.EventLimitsProfileUpdated, Actor: actor,
			Details: details(map[string]any{"key": key, "changes": map[string]any{"name": p.Name, "figures": p.Figures}}),
		})
	})
	if postgres.IsState(err, pgUniqueViolation) {
		return errs.E(errs.Conflict, errs.CodeAlreadyExists, "A profile with this name exists already.")
	}
	return err
}

// DeleteProfile deletes an own profile that no rule and no maximum names.
func (s *Store) DeleteProfile(ctx context.Context, actor audit.Actor, key string, version int64) error {
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.DeleteLimitProfile(ctx, q.DeleteLimitProfileParams{Key: key, Version: version})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The profile", 0)
		}
		return s.changed(ctx, tx, "profile", audit.Event{
			Type: audit.EventLimitsProfileDeleted, Actor: actor,
			Details: details(map[string]any{"key": key}),
		})
	})
	if postgres.IsState(err, pgForeignKeyViolation) {
		return errs.E(errs.Conflict, CodeProfileInUse, "A rule or a project's maximum names the profile.")
	}
	return err
}

// The codes of the store's refusals.
const (
	CodeProfileInUse      = "profile_in_use"
	CodeAssignmentExists  = "assignment_exists"
	CodeAssignmentBuiltin = "assignment_builtin"
)

// placeText is a place as the audit details name it.
func placeText(p limits.Place) string {
	switch p.Kind {
	case limits.PlaceProject:
		return "project:" + p.ProjectKey
	case limits.PlaceRepository:
		return "repository:" + p.Repo.String()
	}
	return "instance"
}

func ruleDetails(r limits.Rule) json.RawMessage {
	return details(map[string]any{
		"id": r.ID, "place": placeText(r.Place), "who": map[string]string{"kind": string(r.WhoKind), "ref": r.WhoRef},
		"profile": r.Profile, "overrides": r.Overrides, "capped": r.Capped,
	})
}

// CreateRule stores a rule (R-LIM-032); its ID is set.
func (s *Store) CreateRule(ctx context.Context, actor audit.Actor, r *limits.Rule) error {
	r.ID = uuid.Must(uuid.NewV7()).String()
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		if err := qs.InsertLimitAssignment(ctx, q.InsertLimitAssignmentParams{
			ID: r.ID, PlaceKind: r.Place.Kind.String(), ProjectID: projectIDOf(r.Place), RepositoryID: repoIDOf(r.Place),
			WhoKind: string(r.WhoKind), WhoRef: r.WhoRef, Profile: r.Profile, Overrides: overridesJSON(r.Overrides),
			Capped: r.Capped, Note: r.Note, Actor: actor.ID,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, "assignment", audit.Event{Type: audit.EventLimitsAssignmentCreated, Actor: actor, Details: ruleDetails(*r)})
	})
	if postgres.IsState(err, pgUniqueViolation) {
		return errs.E(errs.Conflict, CodeAssignmentExists, "This place has a rule for this caller already.")
	}
	return err
}

func projectIDOf(p limits.Place) string {
	if p.Kind == limits.PlaceProject {
		return p.ProjectID
	}
	return ""
}

func repoIDOf(p limits.Place) int64 {
	if p.Kind == limits.PlaceRepository {
		return p.RepoID
	}
	return 0
}

// UpdateRule replaces a rule's profile, overrides, cap and note when its version matches.
func (s *Store) UpdateRule(ctx context.Context, actor audit.Actor, r limits.Rule, version int64) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.UpdateLimitAssignment(ctx, q.UpdateLimitAssignmentParams{
			ID: r.ID, Version: version, Profile: r.Profile, Overrides: overridesJSON(r.Overrides), Capped: r.Capped, Note: r.Note, Actor: actor.ID,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The rule", 0)
		}
		return s.changed(ctx, tx, "assignment", audit.Event{Type: audit.EventLimitsAssignmentUpdated, Actor: actor, Details: ruleDetails(r)})
	})
}

// DeleteRule deletes a rule that is not built in when its version matches.
func (s *Store) DeleteRule(ctx context.Context, actor audit.Actor, r limits.Rule, version int64) error {
	if r.Builtin {
		return errs.E(errs.Conflict, CodeAssignmentBuiltin, "A built-in rule cannot be deleted; change its profile instead.")
	}
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.DeleteLimitAssignment(ctx, q.DeleteLimitAssignmentParams{ID: r.ID, Version: version})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The rule", 0)
		}
		return s.changed(ctx, tx, "assignment", audit.Event{Type: audit.EventLimitsAssignmentDeleted, Actor: actor, Details: ruleDetails(r)})
	})
}

// SetCeilings replaces both ceilings of a remote repository (R-LIM-015, R-LIM-032).
func (s *Store) SetCeilings(ctx context.Context, actor audit.Actor, repo string, repoID int64, before, after [2]limits.Ceiling) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		for i, class := range []limits.CeilingClass{limits.CeilingAnonymous, limits.CeilingSignedIn} {
			if err := qs.UpsertLimitCeiling(ctx, q.UpsertLimitCeilingParams{
				RepositoryID: repoID, Class: string(class), UncachedBytesPerHour: after[i].UncachedBytesPerHour,
				UpstreamRequestsPerHour: after[i].UpstreamRequestsPerHour, Actor: actor.ID,
			}); err != nil {
				return err
			}
		}
		return s.changed(ctx, tx, "ceiling", audit.Event{
			Type: audit.EventLimitsCeilingChanged, Actor: actor, Repository: repo,
			Details: details(map[string]any{"repository": repo, "before": ceilingsJSON(before), "after": ceilingsJSON(after)}),
		})
	})
}

func ceilingsJSON(c [2]limits.Ceiling) map[string]any {
	one := func(c limits.Ceiling) map[string]int64 {
		return map[string]int64{"uncachedBytesPerHour": c.UncachedBytesPerHour, "upstreamRequestsPerHour": c.UpstreamRequestsPerHour}
	}
	return map[string]any{"anonymous": one(c[0]), "signedIn": one(c[1])}
}

// SetMaximum sets or replaces a project's maximum (R-LIM-016); version 0 creates one.
func (s *Store) SetMaximum(ctx context.Context, actor audit.Actor, m limits.Maximum, version int64) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.UpsertLimitProjectMaximum(ctx, q.UpsertLimitProjectMaximumParams{
			ProjectID: m.ProjectID, Profile: m.Profile, Overrides: overridesJSON(m.Overrides), Actor: actor.ID, Version: version,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The project's maximum", 0)
		}
		return s.changed(ctx, tx, "maximum", audit.Event{
			Type: audit.EventLimitsProjectMaximumChanged, Actor: actor, Project: m.ProjectKey,
			Details: details(map[string]any{"project": m.ProjectKey, "after": map[string]any{"profile": m.Profile, "overrides": m.Overrides}}),
		})
	})
}

// DeleteMaximum removes a project's maximum.
func (s *Store) DeleteMaximum(ctx context.Context, actor audit.Actor, m limits.Maximum, version int64) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.DeleteLimitProjectMaximum(ctx, q.DeleteLimitProjectMaximumParams{ProjectID: m.ProjectID, Version: version})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The project's maximum", 0)
		}
		return s.changed(ctx, tx, "maximum", audit.Event{
			Type: audit.EventLimitsProjectMaximumChanged, Actor: actor, Project: m.ProjectKey,
			Details: details(map[string]any{"project": m.ProjectKey, "before": map[string]any{"profile": m.Profile}, "after": nil}),
		})
	})
}

// UpdateSettings changes the mode and the block time (R-LIM-026, R-LIM-033).
func (s *Store) UpdateSettings(ctx context.Context, actor audit.Actor, before, after limits.Settings, version int64) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.UpdateLimitSettings(ctx, q.UpdateLimitSettingsParams{
			Mode: string(after.Mode), BlockSeconds: int32(after.BlockSeconds), Version: version, Actor: actor.ID, //nolint:gosec // 60 to 86,400
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.Stale("The settings", 0)
		}
		if before.Mode != after.Mode {
			if err := s.changed(ctx, tx, "settings", audit.Event{
				Type: audit.EventLimitsModeChanged, Actor: actor,
				Details: details(map[string]any{"before": before.Mode, "after": after.Mode}),
			}); err != nil {
				return err
			}
		}
		if before.BlockSeconds != after.BlockSeconds {
			return s.changed(ctx, tx, "settings", audit.Event{
				Type: audit.EventLimitsSettingsChanged, Actor: actor,
				Details: details(map[string]any{
					"before": map[string]int{"blockSeconds": before.BlockSeconds},
					"after":  map[string]int{"blockSeconds": after.BlockSeconds},
				}),
			})
		}
		return s.changed(ctx, tx, "settings", audit.Event{})
	})
}

// Started records a block a flush set (R-LIM-025): asynchronously, never failing a request, and audited.
func (s *Store) Started(ctx context.Context, b limits.Block) {
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
			return qs.InsertLimitBlock(ctx, q.InsertLimitBlockParams{
				ID: b.ID, SubjectKind: string(b.Subject.Kind), SubjectRef: b.Subject.Ref, Place: b.Place.Key(),
				StartedAt: b.Started.UTC().Format(time.RFC3339Nano), Until: b.Until.UTC().Format(time.RFC3339Nano),
				RefusedCount: b.Refused, RefusedLimit: b.Limit,
			})
		})
		if err != nil {
			s.o.Log.Warn("a block of the download limits was not recorded", "error", err)
		}
		s.o.Audit.Record(ctx, audit.Event{
			Type: audit.EventLimitsBlockStarted, Actor: audit.Actor{Kind: "system"}, Outcome: audit.Success,
			Details: details(map[string]any{
				"block": b.ID, "subject": b.Subject.String(), "place": b.Place.Key(),
				"until": b.Until.UTC().Format(time.RFC3339), "refused": b.Refused, "limit": b.Limit,
			}),
		})
	}()
}

// BlockRow is a recorded block with its place as the store writes it.
type BlockRow struct {
	limits.Block
	PlaceKey string
}

func blockOf(id, kind, ref, place string, started, until time.Time, refused, limit int64, lifted *time.Time, by string) BlockRow {
	return BlockRow{Block: limits.Block{
		ID: id, Subject: limits.Subject{Kind: limits.SubjectKind(kind), Ref: ref}, Started: started, Until: until,
		Refused: refused, Limit: limit, LiftedAt: lifted, LiftedBy: by,
	}, PlaceKey: place}
}

// Blocks lists blocks, newest first: the active ones only when active is set; of the places named, every place when none
// is.
func (s *Store) Blocks(ctx context.Context, active bool, places []string, limit int) ([]BlockRow, error) {
	var out []BlockRow
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		place := ""
		if len(places) == 0 {
			places = []string{}
		} else {
			place = "-"
		}
		rows, err := qs.LimitBlocks(ctx, q.LimitBlocksParams{ActiveOnly: active, Place: place, Places: places, RowLimit: int32(limit)}) //nolint:gosec // at most 200
		for _, r := range rows {
			var lifted *time.Time
			if r.LiftedAt.Valid {
				t := r.LiftedAt.Time
				lifted = &t
			}
			out = append(out, blockOf(r.ID, r.SubjectKind, r.SubjectRef, r.Place, r.StartedAt.Time, r.Until.Time, r.RefusedCount, r.RefusedLimit, lifted, r.LiftedBy))
		}
		return err
	})
	return out, err
}

// isUUID reports whether s is a UUID in its text form.
func isUUID(s string) bool { return uuid.Validate(s) == nil }

// Block reads one block; ok is false when none has the ID.
func (s *Store) Block(ctx context.Context, id string) (BlockRow, bool, error) {
	if !isUUID(id) {
		return BlockRow{}, false, nil // no block has an ID that is no UUID
	}
	var out BlockRow
	found := false
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		r, err := qs.LimitBlock(ctx, id)
		if postgres.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var lifted *time.Time
		if r.LiftedAt.Valid {
			t := r.LiftedAt.Time
			lifted = &t
		}
		out, found = blockOf(r.ID, r.SubjectKind, r.SubjectRef, r.Place, r.StartedAt.Time, r.Until.Time, r.RefusedCount, r.RefusedLimit, lifted, r.LiftedBy), true
		return nil
	})
	return out, found, err
}

// LiftBlock records the lift of an active block; ok is false when it had ended or was lifted already.
func (s *Store) LiftBlock(ctx context.Context, actor audit.Actor, b BlockRow) (bool, error) {
	ok := false
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.LiftLimitBlock(ctx, q.LiftLimitBlockParams{ID: b.ID, Actor: actor.ID})
		if err != nil || n == 0 {
			return err
		}
		ok = true
		return s.o.Audit.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventLimitsBlockLifted, Actor: actor, Outcome: audit.Success,
			Details: details(map[string]any{"block": b.ID, "subject": b.Subject.String(), "place": b.PlaceKey}),
		})
	})
	return ok, err
}
