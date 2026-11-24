package v1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/requests"
	"github.com/guided-traffic/quader/internal/remotecuration/retention"
	"github.com/guided-traffic/quader/internal/repository"
)

// The requests for an allow and the discard of what was fetched for examination (remote-curation.md R-CUR-015, R-CUR-016; work
// package R2b of docs/developer/remote-curation.md): the list, an ask, an allow, a decline, a withdrawal and a discard. Who may do what
// is the table of R-CUR-005, a disjunction the declaration forms cannot express ("manage on the repository, or the system role admin";
// "read, and no right to set rules"), so every operation declares only that the caller is authenticated and decides here (Q-API-199).

// DeliveryRequestsServices are the services of the requests' operations; a nil member answers them with 503.
type DeliveryRequestsServices struct {
	Requests  *requests.Service
	Retention *retention.Service
}

// requestSetup is what every operation starts with: the services, the decisions, read on the repository (a hidden repository answers
// 404) and the remote repository from the snapshot; manager reports whether the caller may set its rules (manage or admin, R-CUR-005).
func (s *Server) requestSetup(ctx context.Context, key qname.Repo) (context.Context, *edge.Access, repository.Repository, bool, error) {
	if s.o.DeliveryRequests.Requests == nil || s.o.DeliveryRequests.Retention == nil {
		return ctx, nil, repository.Repository{}, false, unavailable("The requests for an allow")
	}
	ctx, acc, err := s.downloadAccess(ctx, key)
	if err != nil {
		return ctx, nil, repository.Repository{}, false, err
	}
	if err := downloadReader(ctx, acc, key); err != nil {
		return ctx, nil, repository.Repository{}, false, err
	}
	repo, _, err := s.deliveryTarget(ctx, key)
	if err != nil {
		return ctx, nil, repository.Repository{}, false, err
	}
	return ctx, acc, repo, downloadManager(ctx, acc, key), nil
}

func requestCaller(r *http.Request) requests.Caller {
	ctx := r.Context()
	return requests.Caller{Actor: edge.ActorFrom(httpserver.PrincipalFrom(ctx)), Source: auditSource(r), PrincipalID: callerID(ctx)}
}

func requestsManagerOnly(key qname.Repo) error {
	return errs.E(errs.Forbidden, errs.CodeForbidden, "Deciding the requests of "+strconv.Quote(key.String())+" needs manage on it.")
}

func deliveryRequestsSpec() ListSpec { return ListSpec{Filters: []string{"state", "origin", "mine"}} }

// ListDeliveryRequests implements GET …/delivery-requests (R-CUR-015 "The list", "Who sees them").
func (s *Server) ListDeliveryRequests(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.ListDeliveryRequestsParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	lq, err := ParseList(r, deliveryRequestsSpec(), s.o.API.Config)
	var ctx context.Context
	var acc *edge.Access
	var repo repository.Repository
	var manager bool
	if err == nil {
		ctx, acc, repo, manager, err = s.requestSetup(r.Context(), key)
	}
	f := requests.Filter{Limit: lq.FetchLimit()}
	if err == nil {
		f.State, f.Origin = lq.First("state"), lq.First("origin")
		switch lq.First("mine") {
		case "", "false":
		case "true":
			f.Mine = callerID(ctx)
		default:
			err = badParam("mine", "must be true or false")
		}
		if !manager {
			f.Mine = callerID(ctx) // a reader sees the entries it asks in (R-CUR-005)
			if f.Mine == "" {
				f.Mine = "00000000-0000-0000-0000-000000000000"
			}
		}
	}
	if err == nil && lq.After != nil {
		f.After, err = requestCursor(lq.After)
	}
	var views []requests.View
	if err == nil {
		views, err = s.o.DeliveryRequests.Requests.List(ctx, repo.NumericID(), f)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listDeliveryRequests"))
		return
	}
	page := NewPage(lq, views, func(v requests.View) []string {
		return []string{strconv.Itoa(openFlag(v.State)), v.LastAt.UTC().Format(time.RFC3339Nano), v.ID}
	})
	t := downloadTarget{ID: repo.NumericID(), Key: key, Format: repo.Format}
	WritePage(w, r, MapPage(page, func(v requests.View) map[string]any { return s.requestView(ctx, acc, t, manager, v) }))
}

func openFlag(st rc.RequestState) int {
	if st == rc.RequestOpen {
		return 0
	}
	return 1
}

// requestCursor reads the keyset position of the list: the last entry's open flag, last activity and ID.
func requestCursor(key []string) (*requests.Entry, error) {
	if len(key) != 3 {
		return nil, invalidCursor()
	}
	at, err := time.Parse(time.RFC3339Nano, key[1])
	if err != nil {
		return nil, invalidCursor()
	}
	st := rc.RequestDeclined
	if key[0] == "0" {
		st = rc.RequestOpen
	}
	return &requests.Entry{State: st, LastAt: at, ID: key[2]}, nil
}

// requestView is an entry as the API answers it (R-CUR-015 "The list"): another asker's id, name and note only to those who may set
// rules; the decider's name to them and to the askers.
func (s *Server) requestView(ctx context.Context, acc *edge.Access, t downloadTarget, manager bool, v requests.View) map[string]any {
	me := callerID(ctx)
	askers := make([]map[string]any, 0, len(v.Askers))
	for _, a := range v.Askers {
		if manager || a.ID == me {
			askers = append(askers, map[string]any{"id": a.ID, "name": a.Name, "note": a.Note, "askedAt": a.AskedAt})
		} else {
			askers = append(askers, map[string]any{"id": nil, "name": nil, "note": nil, "askedAt": a.AskedAt})
		}
	}
	examined := make([]map[string]any, 0, len(v.Examined))
	for _, x := range v.Examined {
		examined = append(examined, map[string]any{"version": x.Version, "fetchedAt": x.FetchedAt, "files": x.Files})
	}
	var examination any
	if v.Examination != nil {
		examination = s.downloadView(ctx, acc, t, *v.Examination)
	}
	var decidedBy any
	if v.DecidedBy != nil && *v.DecidedBy != "" {
		decidedBy = s.starter(ctx, acc, t, *v.DecidedBy)
	}
	return map[string]any{
		"id": v.ID, "name": v.Name, "version": v.Version, "state": v.State, "askers": askers, "askerCount": v.Entry.Askers,
		"refusals": v.Refusals, "firstAt": v.FirstAt.UTC(), "lastAt": v.LastAt.UTC(), "examination": examination, "examined": examined,
		"examinationRefused": v.ExaminationRefused, "decidedBy": decidedBy, "decidedAt": nullTime(v.DecidedAt), "declineReason": v.DeclineReason,
	}
}

// requestBody reads the JSON body of a write into dst; a body that may be absent leaves dst as it is.
func (s *Server) requestBody(w http.ResponseWriter, r *http.Request, dst any, required bool) ([]byte, error) {
	body, err := readOptionalBody(w, r, s.bodyLimit())
	if err != nil {
		return nil, err
	}
	if body == nil {
		if required {
			return nil, errs.E(errs.Invalid, errs.CodeInvalid, "The request needs a JSON body.")
		}
		return nil, nil
	}
	if err := requireJSON(r, MediaJSON); err != nil {
		return nil, err
	}
	return body, StrictUnmarshal(body, dst)
}

// AskDeliveryRequest implements POST …/delivery-requests (R-CUR-015 "Asking").
func (s *Server) AskDeliveryRequest(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.AskDeliveryRequestParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, acc, repo, manager, err := s.requestSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The requests for an allow")
	}
	if err == nil && manager {
		err = errs.E(errs.Forbidden, errs.CodeForbidden,
			"You may set the delivery rules of "+strconv.Quote(key.String())+": allow the package yourself instead of asking for it.")
	}
	var in gen.DeliveryRequestInput
	var body []byte
	if err == nil {
		body, err = s.requestBody(w, r, &in, true)
	}
	c := requestCaller(r)
	t := requests.Target{Repo: repo}
	var sub requests.Subject
	if err == nil {
		sub, err = s.o.DeliveryRequests.Requests.Check(ctx, t, c, requests.AskInput{Name: in.Name, Version: strOf(in.Version), Note: strOf(in.Note)})
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.askDeliveryRequest"))
		return
	}
	dt := downloadTarget{ID: repo.NumericID(), Key: key, Format: repo.Format}
	charge := func(context.Context) ([]byte, error) { return s.downloadCharge(r, ctx, dt) }
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		asked, err := s.o.DeliveryRequests.Requests.AskTx(ctx, tx, t, c, sub, strOf(in.Note), charge)
		if err != nil {
			return Outcome{}, err
		}
		v, err := s.requestViewTx(ctx, tx, acc, dt, false, asked.Entry)
		if err != nil {
			return Outcome{}, err
		}
		status := http.StatusOK
		if asked.Created {
			status = http.StatusCreated
		}
		return Outcome{Status: status, Body: v}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// requestViewTx is the view of an entry read in the act's transaction (its examination row commits with it).
func (s *Server) requestViewTx(ctx context.Context, tx meta.Tx, acc *edge.Access, t downloadTarget, manager bool, e requests.Entry) (map[string]any, error) {
	v, err := s.o.DeliveryRequests.Requests.ViewTx(ctx, tx, t.ID, e.ID)
	if err != nil {
		return nil, err
	}
	return s.requestView(ctx, acc, t, manager, v), nil
}

// requestDecider is the setup of an allow, a decline and a discard: what writing a rule needs (R-CUR-005), and the product's rules of a
// system repository of the project upstream (409 managed_by_product).
func (s *Server) requestDecider(ctx context.Context, key qname.Repo) (context.Context, *edge.Access, repository.Repository, error) {
	ctx, acc, repo, manager, err := s.requestSetup(ctx, key)
	if err == nil && s.idem == nil {
		err = unavailable("The requests for an allow")
	}
	if err == nil && !manager {
		err = requestsManagerOnly(key)
	}
	if err == nil {
		err = productRules(repo)
	}
	return ctx, acc, repo, err
}

// AllowDeliveryRequest implements POST …/delivery-requests/{requestId}/allow (R-CUR-015 "Allow").
func (s *Server) AllowDeliveryRequest(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, requestID openapi_types.UUID,
	_ gen.AllowDeliveryRequestParams,
) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	_, acc, repo, err := s.requestDecider(r.Context(), key)
	var in gen.DeliveryRequestAllowInput
	var body []byte
	if err == nil {
		body, err = s.requestBody(w, r, &in, false)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.allowDeliveryRequest"))
		return
	}
	input := requests.AllowInput{Note: strOf(in.Note)}
	input.AsPackage = in.As != nil && *in.As == gen.DeliveryRequestAllowInputAsPackage
	input.DownloadEveryVersion = in.DownloadEveryVersion != nil && *in.DownloadEveryVersion
	c := requestCaller(r)
	t := requests.Target{Repo: repo}
	dt := downloadTarget{ID: repo.NumericID(), Key: key, Format: repo.Format}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		got, err := s.o.DeliveryRequests.Requests.AllowTx(ctx, tx, t, c, requestID.String(), input)
		if err != nil {
			return Outcome{}, err
		}
		v, err := s.requestViewTx(ctx, tx, acc, dt, true, got.Entry)
		if err != nil {
			return Outcome{}, err
		}
		rows := make([]map[string]any, 0, len(got.Rows))
		for _, row := range got.Rows {
			rows = append(rows, s.downloadView(ctx, acc, dt, row))
		}
		var bulk any
		if got.Bulk != nil {
			bulk = s.bulkView(ctx, acc, dt, *got.Bulk, nil)
		}
		return Outcome{Status: http.StatusOK, Body: map[string]any{
			"request": v, "rule": toDeliveryRuleWritten(got.Rule), "downloads": rows, "bulk": bulk,
		}}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// DeclineDeliveryRequest implements POST …/delivery-requests/{requestId}/decline (R-CUR-015 "Decline").
func (s *Server) DeclineDeliveryRequest(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, requestID openapi_types.UUID,
	_ gen.DeclineDeliveryRequestParams,
) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	_, acc, repo, err := s.requestDecider(r.Context(), key)
	var in gen.DeliveryRequestDeclineInput
	var body []byte
	if err == nil {
		body, err = s.requestBody(w, r, &in, true)
	}
	if err == nil {
		err = requests.CheckReason(in.Reason)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.declineDeliveryRequest"))
		return
	}
	discard := in.Discard == nil || *in.Discard
	c := requestCaller(r)
	t := requests.Target{Repo: repo}
	dt := downloadTarget{ID: repo.NumericID(), Key: key, Format: repo.Format}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		e, gone, err := s.o.DeliveryRequests.Requests.DeclineTx(ctx, tx, t, c, requestID.String(), in.Reason, discard)
		if err != nil {
			return Outcome{}, err
		}
		v, err := s.requestViewTx(ctx, tx, acc, dt, true, e)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: http.StatusOK, Body: map[string]any{"request": v, "discarded": discardedView(gone)}}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

func discardedView(d retention.Discarded) map[string]any {
	versions := d.Versions
	if versions == nil {
		versions = []string{}
	}
	return map[string]any{"versions": versions, "files": d.Files}
}

// WithdrawDeliveryRequest implements POST …/delivery-requests/{requestId}/withdraw (R-CUR-015 "Withdraw").
func (s *Server) WithdrawDeliveryRequest(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, requestID openapi_types.UUID,
	_ gen.WithdrawDeliveryRequestParams,
) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	_, acc, repo, manager, err := s.requestSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The requests for an allow")
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.withdrawDeliveryRequest"))
		return
	}
	c := requestCaller(r)
	t := requests.Target{Repo: repo}
	dt := downloadTarget{ID: repo.NumericID(), Key: key, Format: repo.Format}
	err = s.idem.Do(w, r, nil, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		e, err := s.o.DeliveryRequests.Requests.WithdrawTx(ctx, tx, t, c, requestID.String())
		if err != nil {
			return Outcome{}, err
		}
		v, err := s.requestViewTx(ctx, tx, acc, dt, manager, e)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: http.StatusOK, Body: v}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// DiscardExaminations implements POST …/examinations/discard (R-CUR-016 "Discard").
func (s *Server) DiscardExaminations(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.DiscardExaminationsParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	_, _, repo, err := s.requestDecider(r.Context(), key)
	var in gen.ExaminationDiscardInput
	var body []byte
	if err == nil {
		body, err = s.requestBody(w, r, &in, true)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.discardExaminations"))
		return
	}
	c := retention.Caller{Actor: edge.ActorFrom(httpserver.PrincipalFrom(r.Context())), Source: auditSource(r)}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		gone, err := s.o.DeliveryRequests.Retention.DiscardTx(ctx, tx, repo, c, retention.Subject{Name: in.Name, Version: strOf(in.Version)},
			retention.CausePerson)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: http.StatusOK, Body: discardedView(gone)}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}
