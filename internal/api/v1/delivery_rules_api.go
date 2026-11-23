package v1

import (
	"context"
	"net/http"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules/store"
	"github.com/guided-traffic/quader/internal/repository"
)

// The delivery rules of a remote repository (remote-curation.md R-CUR-005, R-CUR-006): list, create or replace, remove, and the check
// of a decision. Who may do what is a disjunction the declaration forms cannot express ("manage on the repository, or the system role
// admin", Q-API-199), so every operation declares only that the caller is authenticated and decides here, as the mirror's run does.

// DeliveryRulesServices are the services of the rules' operations; a nil member answers them with 503.
type DeliveryRulesServices struct {
	// Store writes and lists the rules.
	Store *store.Store
	// Reader is the replica's snapshot of the repositories: the check decides on the compiled rule set it holds.
	Reader repository.Reader
}

// deliveryTarget loads the remote repository an operation addresses from the snapshot: 400 not_applicable for a local or virtual one.
func (s *Server) deliveryTarget(ctx context.Context, key qname.Repo) (repository.Repository, store.Target, error) {
	rd := s.o.DeliveryRules.Reader
	if rd == nil {
		return repository.Repository{}, store.Target{}, unavailable("The delivery rules")
	}
	repo, err := rd.ByName(ctx, key)
	if err != nil {
		return repository.Repository{}, store.Target{}, err
	}
	if repo.Class != repository.Remote {
		return repository.Repository{}, store.Target{}, errs.E(errs.Invalid, errs.CodeNotApplicable,
			"Only a remote repository has delivery rules; "+strconv.Quote(key.String())+" is a "+string(repo.Class)+" repository.")
	}
	return repo, store.Target{ID: repo.NumericID(), Name: repo.Name, Format: repo.Format}, nil
}

// requireRuleReader decides who reads the rules with their notes (R-CUR-005): manage on the repository, repositories:read-config
// (an Owner of the project reads the configuration too), or the system role admin, whom the decision on manage admits. A repository
// hidden from the caller answers 404.
func (s *Server) requireRuleReader(ctx context.Context, key qname.Repo) error {
	ctx, acc, err := requestHolder(ctx, s.o.Repositories.Authz, key)
	if err != nil {
		return err
	}
	if acc == nil {
		return errNoDecisionPoint()
	}
	cfg, err := s.readsConfig(ctx, key.Project)
	if err != nil {
		return err
	}
	if cfg {
		return exposed(acc, key)
	}
	return hiddenOr(ctx, acc, key, acc.Decide(ctx, manageCoordinate(key)).Err())
}

// requireRuleWriter decides who writes a rule (R-CUR-005, Q-CUR-11): manage on the repository (the roles Maintainer and Owner) or the
// system role admin. repositories:write-config and repository-admin set the mode, not the rules.
func (s *Server) requireRuleWriter(ctx context.Context, key qname.Repo) error {
	ctx, acc, err := requestHolder(ctx, s.o.Repositories.Authz, key)
	if err != nil {
		return err
	}
	if acc == nil {
		return errNoDecisionPoint()
	}
	return hiddenOr(ctx, acc, key, acc.Decide(ctx, manageCoordinate(key)).Err())
}

// requireRepoReader decides the check (R-CUR-005): read on the repository.
func (s *Server) requireRepoReader(ctx context.Context, key qname.Repo) error {
	ctx, acc, err := requestHolder(ctx, s.o.Repositories.Authz, key)
	if err != nil {
		return err
	}
	if acc == nil {
		return errNoDecisionPoint()
	}
	return acc.Decide(ctx, authz.Coordinate{Repository: key, Kind: authz.KindMetadata, Actions: []authz.Action{authz.Read}}).Err()
}

// productRules is the 409 of a rule write on a system repository of the project upstream, whose rules are the product's (R-REPO-070).
func productRules(repo repository.Repository) error {
	if !repo.Upstream.System() {
		return nil
	}
	return managedByProductErr("The delivery rules of a system repository of the project upstream are the product's (R-REPO-070).")
}

func managedByProductErr(detail string) error {
	return errs.E(errs.Conflict, errs.CodeManagedByProduct, detail)
}

func ruleCaller(r *http.Request) store.Caller {
	ctx := r.Context()
	return store.Caller{Actor: edge.ActorFrom(httpserver.PrincipalFrom(ctx)), Source: auditSource(r), PrincipalID: callerID(ctx)}
}

func deliveryRulesSpec() ListSpec { return ListSpec{Filters: []string{"kind", "effect", "q"}} }

// ListDeliveryRules implements GET …/delivery-rules (R-CUR-006).
func (s *Server) ListDeliveryRules(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText string, _ gen.ListDeliveryRulesParams) {
	key := qname.Repo{Project: projectKey, Key: repoKeyText}
	ctx := r.Context()
	lq, err := ParseList(r, deliveryRulesSpec(), s.o.API.Config)
	if err == nil && s.o.DeliveryRules.Store == nil {
		err = unavailable("The delivery rules")
	}
	if err == nil {
		err = s.requireRuleReader(ctx, key)
	}
	var t store.Target
	if err == nil {
		_, t, err = s.deliveryTarget(ctx, key)
	}
	afterAt, afterID := "", ""
	if err == nil && lq.After != nil {
		if len(lq.After) != 2 {
			err = invalidCursor()
		} else {
			afterAt, afterID = lq.After[0], lq.After[1]
		}
	}
	var rows []store.Stored
	if err == nil {
		rows, err = s.o.DeliveryRules.Store.List(ctx, t.ID, store.ListFilter{Kind: lq.First("kind"), Effect: lq.First("effect"), Q: lq.First("q")},
			afterAt, afterID, lq.FetchLimit())
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listDeliveryRules"))
		return
	}
	page := NewPage(lq, rows, func(st store.Stored) []string {
		return []string{st.CreatedAt.Format("2006-01-02T15:04:05.999999Z07:00"), st.ID}
	})
	WritePage(w, r, MapPage(page, toDeliveryRule))
}

// PutDeliveryRule implements POST …/delivery-rules (R-CUR-006): 201 for a new subject, 200 for a replaced one.
func (s *Server) PutDeliveryRule(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText string, _ gen.PutDeliveryRuleParams) {
	key := qname.Repo{Project: projectKey, Key: repoKeyText}
	ctx := r.Context()
	var err error
	if s.o.DeliveryRules.Store == nil || s.idem == nil {
		err = unavailable("The delivery rules")
	}
	if err == nil {
		err = s.requireRuleWriter(ctx, key)
	}
	var body []byte
	var in gen.DeliveryRuleInput
	if err == nil {
		if err = requireJSON(r, MediaJSON); err == nil {
			body, err = readOptionalBody(w, r, s.bodyLimit())
		}
	}
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	var repo repository.Repository
	var t store.Target
	if err == nil {
		repo, t, err = s.deliveryTarget(ctx, key)
	}
	if err == nil {
		err = productRules(repo)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.putDeliveryRule"))
		return
	}
	input := store.Input{Kind: rc.RuleKind(in.Kind), Effect: rc.Effect(in.Effect), Pattern: strOf(in.Pattern), Name: strOf(in.Name), Version: strOf(in.Version), Note: strOf(in.Note)}
	c := ruleCaller(r)
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		wr, err := s.o.DeliveryRules.Store.PutTx(ctx, tx, t, c, input)
		if err != nil {
			return Outcome{}, err
		}
		status := http.StatusOK
		if wr.Created {
			status = http.StatusCreated
		}
		return Outcome{Status: status, Body: toDeliveryRuleWritten(wr)}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.putDeliveryRule"))
	}
}

// DeleteDeliveryRule implements DELETE …/delivery-rules/{ruleId} (R-CUR-006).
func (s *Server) DeleteDeliveryRule(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText string, ruleID openapi_types.UUID) {
	key := qname.Repo{Project: projectKey, Key: repoKeyText}
	ctx := r.Context()
	var err error
	if s.o.DeliveryRules.Store == nil {
		err = unavailable("The delivery rules")
	}
	if err == nil {
		err = s.requireRuleWriter(ctx, key)
	}
	var repo repository.Repository
	var t store.Target
	if err == nil {
		repo, t, err = s.deliveryTarget(ctx, key)
	}
	if err == nil {
		err = productRules(repo)
	}
	if err == nil {
		err = s.o.DeliveryRules.Store.Delete(ctx, t, ruleCaller(r), ruleID.String())
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.deleteDeliveryRule"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CheckDeliveryRules implements POST …/delivery-rules/check (R-CUR-006): the decision of R-CUR-001 on this replica's snapshot.
func (s *Server) CheckDeliveryRules(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText string) {
	key := qname.Repo{Project: projectKey, Key: repoKeyText}
	ctx := r.Context()
	err := s.requireRepoReader(ctx, key)
	var body []byte
	var in gen.DeliveryCheckInput
	if err == nil {
		if err = requireJSON(r, MediaJSON); err == nil {
			body, err = readOptionalBody(w, r, s.bodyLimit())
		}
	}
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	var repo repository.Repository
	if err == nil {
		repo, _, err = s.deliveryTarget(ctx, key)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.checkDeliveryRules"))
		return
	}
	set := repo.Delivery()
	var d rc.Decision
	if v := strOf(in.Version); v != "" {
		d = set.Decide(in.Name, v)
	} else {
		d = set.Package(in.Name)
	}
	writeJSON(w, r, http.StatusOK, toDeliveryDecision(d, set.Mode()))
}

func toDeliveryRule(st store.Stored) gen.DeliveryRule {
	out := gen.DeliveryRule{
		Kind: gen.DeliveryRuleKind(st.Kind), Subject: st.Subject(), Effect: gen.DeliveryRuleEffect(st.Effect), Note: st.Note,
		Pattern: strPtr(st.Pattern), Name: strPtr(st.Name), Version: strPtr(st.Version), CreatedAt: st.CreatedAt,
	}
	out.Id = parseUUID(st.ID)
	if st.RequestID != "" {
		id := parseUUID(st.RequestID)
		out.RequestId = &id
	}
	if st.CreatedByID != "" {
		out.CreatedBy = &struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		}{Id: st.CreatedByID, Name: st.CreatedByName}
	}
	return out
}

func toDeliveryRuleWritten(wr store.Written) gen.DeliveryRuleWritten {
	r := toDeliveryRule(wr.Stored)
	return gen.DeliveryRuleWritten{
		Id: r.Id, Kind: gen.DeliveryRuleWrittenKind(r.Kind), Subject: r.Subject, Pattern: r.Pattern, Name: r.Name, Version: r.Version,
		Effect: gen.DeliveryRuleWrittenEffect(r.Effect), Note: r.Note, RequestId: r.RequestId, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
		Revision: wr.Revision,
	}
}

func toDeliveryDecision(d rc.Decision, mode rc.Mode) gen.DeliveryDecision {
	out := gen.DeliveryDecision{Delivered: d.Delivered}
	if !d.Delivered {
		reason := gen.DeliveryDecisionReason(d.Reason)
		out.Reason = &reason
	}
	if d.By.Kind != "" {
		kind, effect, subject := gen.DeliveryDecisionDecidedByKind(d.By.Kind), gen.DeliveryDecisionDecidedByEffect(d.By.Effect), d.By.Subject
		out.DecidedBy.Kind, out.DecidedBy.Effect, out.DecidedBy.Subject = &kind, &effect, &subject
		return out
	}
	m := gen.DeliveryDecisionDecidedByMode(mode)
	out.DecidedBy.Mode = &m
	return out
}
