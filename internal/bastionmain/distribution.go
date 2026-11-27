package bastionmain

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// What a Bastion may distribute (R-BAS-134, Q-BAS-519): an administrator allows projects per Bastion, a new Bastion allows none, and a
// project's Owner chooses per repository the Bastions that distribute it among those that allow the project (the repository's member
// bastions, internal/repository/bastions.go). This file is the administrator's half: the allow list of a registration. Taking a project
// off the list deletes its repositories' choices for that Bastion in the same transaction (the trigger of 00159), so allowing it again
// distributes nothing until the Owners choose again. Nothing decides a request on the distribution yet: what a Bastion serves stays the
// main installation's decision per request as before.

// AllowedProject is a project a Bastion allows, as the API lists it: its key and display name, since when and by whom it is allowed,
// and the keys of its repositories that chose the Bastion.
type AllowedProject struct {
	Key          string
	DisplayName  string
	AllowedAt    time.Time
	AllowedBy    string
	Repositories []string
}

// ProjectPage is one page of a Bastion's allowed projects with the totals of the whole list.
type ProjectPage struct {
	Items []AllowedProject
	// HasMore says a page follows; Projects and Repositories count the allowed projects and the repositories distributed.
	HasMore                bool
	Projects, Repositories int64
}

// distributionTarget is a registration and a project an allow or a removal names, both locked FOR SHARE.
type distributionTarget struct {
	bastionID, bastion string
	projectID, project string
	projectDisplayName string
}

// lockTarget reads the registration and the project of an allow or a removal in tx: 404 for either that does not exist.
func lockTarget(ctx context.Context, qs *q.Queries, name, project string) (distributionTarget, error) {
	regs, err := qs.BastionDistributionShare(ctx, name)
	if err != nil {
		return distributionTarget{}, err
	}
	if len(regs) == 0 {
		return distributionTarget{}, notFound()
	}
	projects, err := qs.BastionDistributionProjectShare(ctx, project)
	if err != nil {
		return distributionTarget{}, err
	}
	if len(projects) == 0 {
		return distributionTarget{}, errs.E(errs.NotFound, errs.CodeNotFound, "The project "+project+" does not exist.")
	}
	return distributionTarget{
		bastionID: regs[0].ID, bastion: regs[0].Name,
		projectID: projects[0].ID, project: projects[0].Key, projectDisplayName: projects[0].DisplayName,
	}, nil
}

// AllowProject allows a project for the registration name (R-BAS-134): its Owners may then choose the Bastion for its repositories. A
// project allowed already changes nothing and records nothing (the PUT is idempotent); otherwise the row, the change area
// bastion_distribution and bastion.project.allowed, without a journal entry: a widening narrows nothing a restore could reopen.
func (r *Registrations) AllowProject(ctx context.Context, c principal.Caller, name, project string) error {
	err := r.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs := principal.Queries(tx)
		t, err := lockTarget(ctx, qs, name, project)
		if err != nil {
			return err
		}
		rows, err := qs.BastionProjectAllow(ctx, q.BastionProjectAllowParams{BastionID: t.bastionID, ProjectID: t.projectID, AllowedBy: actorRef(c.Actor)})
		if err != nil || len(rows) == 0 {
			return err
		}
		if err := r.notifyArea(tx, cluster.AreaBastionDistribution, t.bastionID); err != nil {
			return err
		}
		return r.o.Recorder.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventBastionProjectAllowed, Outcome: audit.Success, Actor: c.Actor, Source: c.Source, Project: t.project,
			Subject: audit.Subject{Kind: "bastion", ID: t.bastionID, Name: t.bastion},
			Details: mustJSON(map[string]any{"project": t.project, "projectId": t.projectID}),
		})
	})
	return principal.DBErr("bastion.project.allow", err)
}

// RemoveProject takes a project off the registration's list (R-BAS-134): its repositories' choices for the Bastion go with it, in the
// same transaction, and allowing the project again brings none of them back. A project that is not on the list answers 404.
// bastion.project.removed names the repositories whose choice went and carries the journal entry {bastionId, projectId}, so a restore
// to a point before it deletes the row and the choices again (Q-BAS-521).
func (r *Registrations) RemoveProject(ctx context.Context, c principal.Caller, name, project string) error {
	err := r.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs := principal.Queries(tx)
		t, err := lockTarget(ctx, qs, name, project)
		if err != nil {
			return err
		}
		choices, err := qs.BastionProjectChoices(ctx, q.BastionProjectChoicesParams{BastionID: t.bastionID, ProjectID: t.projectID})
		if err != nil {
			return err
		}
		removed, err := qs.BastionProjectRemove(ctx, q.BastionProjectRemoveParams{BastionID: t.bastionID, ProjectID: t.projectID})
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			return errs.E(errs.NotFound, errs.CodeNotFound, "The Bastion "+t.bastion+" does not allow the project "+t.project+".")
		}
		if err := r.notifyArea(tx, cluster.AreaBastionDistribution, t.bastionID); err != nil {
			return err
		}
		keys := make([]string, 0, len(choices))
		for _, ch := range choices {
			keys = append(keys, ch.Key)
		}
		return r.o.Recorder.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventBastionProjectRemoved, Outcome: audit.Success, Actor: c.Actor, Source: c.Source, Project: t.project,
			Subject: audit.Subject{Kind: "bastion", ID: t.bastionID, Name: t.bastion},
			Details: mustJSON(map[string]any{"project": t.project, "projectId": t.projectID, "repositories": keys}),
			Journal: projectRemovedEntry(t.bastionID, t.projectID, t.bastion, t.project),
		})
	})
	return principal.DBErr("bastion.project.remove", err)
}

// Projects reads one page of the projects the registration name allows, by project key after the key after, those whose key or display
// name holds query (case-insensitively, "" for every one), at most limit; and the totals of the whole list. 404 for a registration that
// does not exist.
func (r *Registrations) Projects(ctx context.Context, name, after, query string, limit int) (ProjectPage, error) {
	var out ProjectPage
	err := r.o.DB.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		regs, err := qs.BastionGet(ctx, name)
		if err != nil {
			return err
		}
		if len(regs) == 0 {
			return notFound()
		}
		id := regs[0].ID
		rows, err := qs.BastionProjectList(ctx, q.BastionProjectListParams{BastionID: id, After: after, Query: strings.TrimSpace(query), MaxRows: int32(limit + 1)}) //nolint:gosec // a page size of the API (at most api.page_size_max)
		if err != nil {
			return err
		}
		if len(rows) > limit {
			rows, out.HasMore = rows[:limit], true
		}
		out.Items = make([]AllowedProject, 0, len(rows))
		for _, row := range rows {
			out.Items = append(out.Items, AllowedProject{
				Key: row.Key, DisplayName: row.DisplayName, AllowedAt: row.AllowedAt.Time, AllowedBy: row.AllowedBy, Repositories: row.Repositories,
			})
		}
		counts, err := qs.BastionProjectCount(ctx, id)
		out.Projects, out.Repositories = counts.Projects, counts.Repositories
		return err
	})
	return out, principal.DBErr("bastion.projects", err)
}

// OfferedBastion is a Bastion that allows a project, as its Owner chooses among them (R-BAS-134): its name, whether it is disabled,
// whether its web interface is on, and the base URLs its replicas reported within the last 24 hours, one per deployment.
type OfferedBastion struct {
	Name         string
	Disabled     bool
	WebInterface bool
	BaseURLs     []string
}

// Offered reads the Bastions that allow the project with the key project, by name; none for a project that does not exist.
func (r *Registrations) Offered(ctx context.Context, project string) ([]OfferedBastion, error) {
	var out []OfferedBastion
	err := r.o.DB.Read(ctx, func(db q.DBTX) error {
		rows, err := q.New(db).ProjectBastionsOffered(ctx, project)
		if err != nil {
			return err
		}
		out = make([]OfferedBastion, 0, len(rows))
		for _, row := range rows {
			out = append(out, OfferedBastion{Name: row.Name, Disabled: row.Disabled, WebInterface: row.Gui, BaseURLs: row.BaseUrls})
		}
		return nil
	})
	return out, principal.DBErr("bastion.offered", err)
}

// actorRef is what bastion_project.allowed_by keeps of the actor: its principal ID, or the name of an actor without one (admin-cli,
// break-glass).
func actorRef(a audit.Actor) string {
	if id := principal.IDOrEmpty(a.ID); id != "" {
		return id
	}
	if a.Name != "" {
		return a.Name
	}
	return a.Kind
}

// projectRemovedEntry is the journal entry of bastion.project.removed: the IDs the replay deletes the row by and, for display, the
// names.
func projectRemovedEntry(bastionID, projectID, bastion, project string) json.RawMessage {
	return mustJSON(map[string]string{"bastionId": bastionID, "projectId": projectID, "name": bastion, "project": project})
}
