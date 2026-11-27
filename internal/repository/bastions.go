package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The repository's member bastions (bastion.md R-BAS-134, Q-REPO-491): the Bastions that distribute the repository beyond the main
// installation's network, chosen by the project's Owner (or a holder of repositories:write-config, the people who set its levels)
// among the Bastions that allow the project. They are rows of repository_bastion, not part of repositories.config; a row exists only
// while the Bastion allows the project (00159), so the member is the effective distribution. Public stays the main installation's
// level: a repository is distributed through a Bastion only while it is named here, whatever its level.

// BastionRef is an entry of the member bastions. A write names a Bastion by its name, as a string or as the object a read returned;
// a read gives the name, the base URLs of the Bastion's deployments (the API keeps those that are entries of
// server.additional_base_urls) and whether the registration is disabled, which a write ignores.
type BastionRef struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	Disabled  bool     `json:"disabled"`
}

// UnmarshalJSON reads a string or an object with the member name; addresses and disabled are read-only and ignored, any other member
// is refused like an unknown member of the document (R-REPO-008).
func (b *BastionRef) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		*b = BastionRef{}
		return json.Unmarshal(raw, &b.Name)
	}
	var obj struct {
		Name      string          `json:"name"`
		Addresses json.RawMessage `json:"addresses"`
		Disabled  json.RawMessage `json:"disabled"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&obj); err != nil {
		return fmt.Errorf("an entry of bastions is a Bastion's name or an object with the member name: %w", err)
	}
	*b = BastionRef{Name: obj.Name}
	return nil
}

// bastionNames are the names of the member, sorted and without duplicates: what a write stores and an update compares.
func bastionNames(refs []BastionRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Name)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// bastionsChanged reports whether an update changes the Bastions that distribute the repository.
func bastionsChanged(cur, nd *Document) bool {
	return !slices.Equal(bastionNames(cur.Bastions), bastionNames(nd.Bastions))
}

// allowedBastions resolves the names of the member against the Bastions that allow the project projectID: their principal IDs by name,
// or the 422 validation_failed with bastion_not_allowed at each entry that names a Bastion which does not allow the project (or does
// not exist: an Owner cannot tell the two apart, and neither may be chosen).
func allowedBastions(ctx context.Context, qs *q.Queries, projectID string, refs []BastionRef) (map[string]string, error) {
	names := bastionNames(refs)
	ids := make(map[string]string, len(names))
	if len(names) == 0 {
		return ids, nil
	}
	rows, err := qs.RepositoryBastionsAllowed(ctx, q.RepositoryBastionsAllowedParams{ProjectID: projectID, Names: names})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ids[r.Name] = r.ID
	}
	var fields []errs.FieldError
	for i, r := range refs {
		if _, ok := ids[r.Name]; !ok {
			fields = append(fields, errs.FieldError{
				Pointer: "/bastions/" + strconv.Itoa(i), Code: errs.CodeBastionNotAllowed,
				Detail: fmt.Sprintf("the Bastion %q does not allow this repository's project; an administrator allows the project for it first", r.Name),
			})
		}
	}
	if len(fields) > 0 {
		return nil, errs.E(errs.Invalid, errs.CodeValidation, "The repository names a Bastion that does not allow its project.", errs.WithFields(fields...))
	}
	return ids, nil
}

// createBastions resolves and stores the member of a create, in its transaction after the repository's row: the whole create is
// refused with the 422 of allowedBastions for a Bastion that does not allow the project.
func (s *Service) createBastions(ctx context.Context, tx meta.Tx, qs *q.Queries, c Caller, row q.Repository, project string, refs []BastionRef) error {
	if len(refs) == 0 {
		return nil
	}
	ids, err := allowedBastions(ctx, qs, row.ProjectID.String(), refs)
	if err != nil {
		return err
	}
	created, err := toRepository(row, project, s.cat)
	if err != nil {
		return err
	}
	return s.writeBastions(ctx, tx, c, created, nil, refs, ids)
}

// writeBastions stores the member of an update or a create inside its transaction: it takes off the Bastions cur names and nd does
// not, adds those nd names and cur does not, records repository.bastion.removed (journaled, {repositoryId, bastionId, name}) and
// repository.bastion.added for each, and announces the change area bastion_distribution once per Bastion. ids are nd's names resolved
// by allowedBastions; cur is nil for a create.
func (s *Service) writeBastions(ctx context.Context, tx meta.Tx, c Caller, r Repository, cur []BastionRef, nd []BastionRef, ids map[string]string) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	before, after := bastionNames(cur), bastionNames(nd)
	for _, name := range before {
		if slices.Contains(after, name) {
			continue
		}
		removed, err := qs.RepositoryBastionRemove(ctx, q.RepositoryBastionRemoveParams{RepositoryID: r.NumericID(), Name: name})
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			continue
		}
		if err := s.recordBastion(ctx, tx, c, r, audit.EventRepositoryBastionRemoved, name, removed[0], "owner"); err != nil {
			return err
		}
	}
	for _, name := range after {
		if slices.Contains(before, name) {
			continue
		}
		if err := qs.RepositoryBastionAdd(ctx, q.RepositoryBastionAddParams{RepositoryID: r.NumericID(), BastionID: ids[name]}); err != nil {
			return err
		}
		if err := s.recordBastion(ctx, tx, c, r, audit.EventRepositoryBastionAdded, name, ids[name], "owner"); err != nil {
			return err
		}
	}
	return nil
}

// recordBastion records repository.bastion.added or repository.bastion.removed of the repository r and announces the change area.
func (s *Service) recordBastion(ctx context.Context, tx meta.Tx, c Caller, r Repository, event, name, bastionID, cause string) error {
	ev := audit.Event{
		Type: event, Actor: c.Actor, Outcome: audit.Success, Source: c.Source,
		Subject: audit.Subject{Kind: "repository", ID: r.ID, Name: r.Name.String()}, Project: r.Name.Project,
		Details: mustMarshal(map[string]string{"bastion": name, "bastionId": bastionID, "cause": cause}),
	}
	if event == audit.EventRepositoryBastionRemoved {
		ev.Journal = ChoiceRemovedEntry(r.ID, bastionID, name)
	}
	if err := s.rec.RecordTx(ctx, tx, ev); err != nil {
		return err
	}
	return s.notif.Notify(tx, cluster.AreaBastionDistribution, 1, map[string]string{"bastion": bastionID})
}

// ChoiceRemovedEntry is the journal entry of repository.bastion.removed: the repository's ID and the Bastion's principal ID the replay
// deletes the row by, and the Bastion's name for display (Q-BAS-521). The move of a repository writes it too.
func ChoiceRemovedEntry(repositoryID, bastionID, name string) json.RawMessage {
	return mustMarshal(map[string]string{"repositoryId": repositoryID, "bastionId": bastionID, "name": name})
}

func mustMarshal(v map[string]string) json.RawMessage {
	raw, _ := json.Marshal(v) // a map of strings always marshals
	return raw
}

// bastionRefs groups rows of RepositoryBastionsOf or RepositoryBastionsAll by repository.
func bastionRefs[T any](rows []T, of func(T) (int64, BastionRef)) map[int64][]BastionRef {
	out := map[int64][]BastionRef{}
	for _, row := range rows {
		id, ref := of(row)
		if ref.Addresses == nil {
			ref.Addresses = []string{}
		}
		out[id] = append(out[id], ref)
	}
	return out
}

// loadBastions reads the member of the repositories ids.
func loadBastions(ctx context.Context, qs *q.Queries, ids ...int64) (map[int64][]BastionRef, error) {
	rows, err := qs.RepositoryBastionsOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	return bastionRefs(rows, func(r q.RepositoryBastionsOfRow) (int64, BastionRef) {
		return r.RepositoryID, BastionRef{Name: r.Name, Addresses: r.BaseUrls, Disabled: r.Disabled}
	}), nil
}

// loadAllBastions reads the member of every repository, for the list (R-REPO-012).
func loadAllBastions(ctx context.Context, run meta.Runner) (map[int64][]BastionRef, error) {
	var out map[int64][]BastionRef
	err := run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.RepositoryBastionsAll(ctx)
		if err != nil {
			return err
		}
		out = bastionRefs(rows, func(r q.RepositoryBastionsAllRow) (int64, BastionRef) {
			return r.RepositoryID, BastionRef{Name: r.Name, Addresses: r.BaseUrls, Disabled: r.Disabled}
		})
		return nil
	})
	return out, err
}

// orNone is the member as a read shows it: an empty list for none.
func orNone(refs []BastionRef) []BastionRef {
	if refs == nil {
		return []BastionRef{}
	}
	return refs
}
