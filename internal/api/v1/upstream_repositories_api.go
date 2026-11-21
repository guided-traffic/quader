package v1

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The operations of the well-known repositories of the built-in project upstream (R-API-083, R-REPO-073): the admin page's list
// and item, the state, the level of one and of all, the address and the account. They read with repositories:read-config and write
// with repositories:write-config (R-AUTHZ-041, Q-AUTHZ-303); the address and the account need a recent sign-in and If-Match, a raised
// level asks for a recent sign-in inside the write. The rules are the repository service's (internal/repository/upstream_ops.go).

// ScannerInUse reports whether the work a system repository serves reads through it, from the configuration of the replica that
// answers (R-REPO-070, R-REPO-075, Q-REPO-380): the item of the system repository shows it as inUse.
type ScannerInUse func() bool

// UpstreamServices are what the operations of the well-known repositories need beyond Options.Repositories.
type UpstreamServices struct {
	// ScannerTrivyInUse is the image scanning's answer for scanner-trivy; nil reads as not in use. The scanning's package of the
	// analyser's database supplies it (vulnerability-scanning.md R-VULN-044).
	ScannerTrivyInUse ScannerInUse
	// ScannerClamavInUse is the malware scanning's answer for scanner-clamav: the scanning is on and malware.database.repository
	// names it (malware-scanning.md R-MAL-011); nil reads as not in use.
	ScannerClamavInUse ScannerInUse
}

// inUse answers the use of a system repository by its key.
func (u UpstreamServices) inUse(key string) bool {
	var f ScannerInUse
	switch key {
	case wellknown.ScannerTrivyKey:
		f = u.ScannerTrivyInUse
	case wellknown.ScannerClamavKey:
		f = u.ScannerClamavInUse
	}
	return f != nil && f()
}

// upstreamServes reports whether a repository serves in the sense of Q-AUTHZ-305: a repository of the built-in project upstream that
// is switched on, not being deleted and no system repository; a repository of any other project always passes. A caller without
// repositories:read-config sees the project upstream (readsUpstream) and lists and is suggested its repositories (Q-AUTHZ-306) only
// through such a one, so a preset that is internal and off (Q-REPO-388) shows nobody anything until it is switched on.
func upstreamServes(r repository.Repository) bool {
	return !wellknown.IsProject(r.Name.Project) || (!r.Disabled && r.Lifecycle != repository.Deleting && !r.Upstream.System())
}

// upstreamCaller is the caller of a write: the actor, the administrator's choice of a restricted level (R-AUTHZ-037), and the
// recent-sign-in gate of a raised level (R-API-007).
func (s *Server) upstreamCaller(r *http.Request) (repository.Caller, error) {
	c := repositoryCaller(r)
	var err error
	if c.MayChooseRestricted, err = s.holds(r.Context(), string(authz.SecuritySettingsWrite)); err != nil {
		return repository.Caller{}, err
	}
	c.RaiseGate = func(context.Context) error { return s.requireRecentAuth(r) }
	return c, nil
}

// toUpstream renders an item; inUse is answered for a system repository from the replica's configuration.
func (s *Server) toUpstream(it repository.UpstreamItem) gen.UpstreamRepository {
	out := gen.UpstreamRepository{
		Key: it.Key, Name: wellknown.ProjectKey + "/" + it.Key, Format: it.Format, Kind: gen.UpstreamRepositoryKind(it.Kind),
		DisplayName: it.DisplayName, Visibility: gen.VisibilityLevel(it.Visibility), Fixed: make([]gen.UpstreamRepositoryFixed, 0, len(it.Fixed)),
		ListedBy: it.ListedBy, ListedByProjects: it.ListedByProjects, ConfigVersion: int(it.ConfigVersion),
		Address: gen.UpstreamRepositoryAddressInfo{
			Url: it.Address.URL, ExtraHosts: genHosts(it.Address.ExtraHosts), UpstreamKeys: genKeys(it.Address.UpstreamKeys),
			Overridden: it.Address.Overridden,
			Product: gen.UpstreamProductAddress{
				Url: it.Address.Product.URL, ExtraHosts: genHosts(it.Address.Product.ExtraHosts), UpstreamKeys: genKeys(it.Address.Product.UpstreamKeys),
			},
		},
		Account: gen.UpstreamRepositoryAccountInfo{
			AuthMode: gen.UpstreamRepositoryAccountInfoAuthMode(it.Account.AuthMode), Username: it.Account.Username,
			Password: genSecret(it.Account.Password), RemovedByUpdateAt: it.Account.RemovedByUpdateAt,
		},
	}
	for _, f := range it.Fixed {
		out.Fixed = append(out.Fixed, gen.UpstreamRepositoryFixed(f))
	}
	if it.ListedByNames != nil {
		names := it.ListedByNames
		out.ListedByNames = &names
	}
	if it.Kind == wellknown.KindSystem {
		inUse := s.o.Upstream.inUse(it.Key)
		out.InUse = &inUse
	} else {
		st := gen.UpstreamRepositoryState(it.State)
		out.State = &st
	}
	if len(it.Warnings) > 0 {
		ws := make([]gen.RepositoryWarning, 0, len(it.Warnings))
		for _, w := range it.Warnings {
			p := w.Pointer
			ws = append(ws, gen.RepositoryWarning{Code: w.Code, Pointer: &p, Detail: w.Detail})
		}
		out.Warnings = &ws
	}
	return out
}

func genHosts(in []repository.ExtraHost) []gen.RemoteExtraHost {
	out := make([]gen.RemoteExtraHost, 0, len(in))
	for _, h := range in {
		send := h.SendCredentials
		out = append(out, gen.RemoteExtraHost{Pattern: h.Pattern, SendCredentials: &send})
	}
	return out
}

func genKeys(in []repository.ArchiveKey) *[]gen.UpstreamArchiveKey {
	if in == nil {
		return nil
	}
	out := make([]gen.UpstreamArchiveKey, 0, len(in))
	for _, k := range in {
		out = append(out, gen.UpstreamArchiveKey{Fingerprint: k.Fingerprint, UserId: k.UserID})
	}
	return &out
}

// genSecret renders a stored secret field as the document does: {"set": true} or the reference.
func genSecret(f *repository.SecretField) *gen.SecretField {
	if f == nil {
		return nil
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return nil
	}
	var out gen.SecretField
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return &out
}

// writeUpstream answers an item with its ETag.
func (s *Server) writeUpstream(w http.ResponseWriter, r *http.Request, it repository.UpstreamItem) {
	SetETag(w, it.ConfigVersion, "")
	writeJSON(w, r, http.StatusOK, s.toUpstream(it))
}

// ListUpstreamRepositories implements GET /api/v1/upstream-repositories.
func (s *Server) ListUpstreamRepositories(w http.ResponseWriter, r *http.Request) {
	svc, err := s.repositoryService()
	var items []repository.UpstreamItem
	if err == nil {
		items, err = svc.UpstreamItems(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.UpstreamRepositoryList{Items: make([]gen.UpstreamRepository, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, s.toUpstream(it))
	}
	writeJSON(w, r, http.StatusOK, out)
}

// ListFormatUpstreamRepositories implements GET /api/v1/formats/{format}/upstream-repositories: the member picker's read for a
// writer of a virtual repository (Q-API-364). The declaration admits whoever holds repositories:write-config and the Owner of any
// project; the answer is the same for each of them.
func (s *Server) ListFormatUpstreamRepositories(w http.ResponseWriter, r *http.Request, format string) {
	if _, ok := formats.Lookup(format); !ok {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "No format has this name; GET /api/v1/formats lists the formats."))
		return
	}
	svc, err := s.repositoryService()
	var list []repository.UpstreamSummary
	if err == nil {
		list, err = svc.UpstreamSummaries(r.Context(), format)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.UpstreamRepositorySummaryList{Items: make([]gen.UpstreamRepositorySummary, 0, len(list))}
	for _, it := range list {
		out.Items = append(out.Items, gen.UpstreamRepositorySummary{
			Key: it.Key, Name: wellknown.ProjectKey + "/" + it.Key, DisplayName: it.DisplayName,
			State: gen.UpstreamRepositoryState(it.State), Visibility: gen.VisibilityLevel(it.Visibility),
		})
	}
	writeJSON(w, r, http.StatusOK, out)
}

// GetUpstreamRepository implements GET /api/v1/upstream-repositories/{key}.
func (s *Server) GetUpstreamRepository(w http.ResponseWriter, r *http.Request, key gen.UpstreamRepositoryKey) {
	svc, err := s.repositoryService()
	var it repository.UpstreamItem
	if err == nil {
		it, err = svc.UpstreamItem(r.Context(), key)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	if NotModified(w, r, ETag(it.ConfigVersion, "")) {
		return
	}
	s.writeUpstream(w, r, it)
}

// SetUpstreamRepositoryState implements PUT /api/v1/upstream-repositories/{key}/state.
func (s *Server) SetUpstreamRepositoryState(w http.ResponseWriter, r *http.Request, key gen.UpstreamRepositoryKey) {
	svc, err := s.repositoryService()
	var in gen.UpstreamRepositoryStateRequest
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	if err == nil && in.State != gen.UpstreamRepositoryStateRequestStateOn && in.State != gen.UpstreamRepositoryStateRequestStateOff {
		err = errs.E(errs.Invalid, errs.CodeValidation, "state is on or off.", errs.WithFields(errs.FieldError{Pointer: "/state", Code: "invalid_value", Detail: "on or off"}))
	}
	var it repository.UpstreamItem
	if err == nil {
		it, err = svc.SetUpstreamState(r.Context(), repositoryCaller(r), key, in.State == gen.UpstreamRepositoryStateRequestStateOn)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.writeUpstream(w, r, it)
}

// SetUpstreamRepositoryVisibility implements PUT /api/v1/upstream-repositories/{key}/visibility.
func (s *Server) SetUpstreamRepositoryVisibility(w http.ResponseWriter, r *http.Request, key gen.UpstreamRepositoryKey) {
	svc, err := s.repositoryService()
	var level string
	if err == nil {
		level, err = s.upstreamLevel(w, r)
	}
	var c repository.Caller
	if err == nil {
		c, err = s.upstreamCaller(r)
	}
	var it repository.UpstreamItem
	if err == nil {
		it, err = svc.SetUpstreamVisibility(r.Context(), c, key, level)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.writeUpstream(w, r, it)
}

// upstreamLevel reads the body {"level": …} of a level operation.
func (s *Server) upstreamLevel(w http.ResponseWriter, r *http.Request) (string, error) {
	var in gen.UpstreamRepositoryVisibilityRequest
	if err := DecodeJSON(w, r, &in, s.bodyLimit()); err != nil {
		return "", err
	}
	switch in.Level {
	case gen.VisibilityLevelPrivate, gen.VisibilityLevelInternal, gen.VisibilityLevelPublic:
		return string(in.Level), nil
	}
	return "", errs.E(errs.Invalid, errs.CodeValidation, "The level is one of private, internal and public.",
		errs.WithFields(errs.FieldError{Pointer: "/level", Code: "invalid_value", Detail: "private, internal or public"}))
}

// SwitchOnAllUpstreamRepositories implements POST /api/v1/upstream-repositories/switch-on-all.
func (s *Server) SwitchOnAllUpstreamRepositories(w http.ResponseWriter, r *http.Request, _ gen.SwitchOnAllUpstreamRepositoriesParams) {
	s.upstreamBulk(w, r, nil, func(ctx context.Context, tx meta.Tx, svc *repository.Service, c repository.Caller) ([]repository.UpstreamItem, string, error) {
		return svc.SwitchOnAllUpstreamTx(ctx, tx, c)
	})
}

// SetAllUpstreamRepositoriesVisibility implements POST /api/v1/upstream-repositories/set-visibility.
func (s *Server) SetAllUpstreamRepositoriesVisibility(w http.ResponseWriter, r *http.Request, _ gen.SetAllUpstreamRepositoriesVisibilityParams) {
	var level string
	body, err := ReadBody(w, r, MediaJSON, s.bodyLimit())
	if err == nil {
		var in gen.UpstreamRepositoryVisibilityRequest
		if err = StrictUnmarshal(body, &in); err == nil {
			switch in.Level {
			case gen.VisibilityLevelPrivate, gen.VisibilityLevelInternal, gen.VisibilityLevelPublic:
				level = string(in.Level)
			default:
				err = errs.E(errs.Invalid, errs.CodeValidation, "The level is one of private, internal and public.",
					errs.WithFields(errs.FieldError{Pointer: "/level", Code: "invalid_value", Detail: "private, internal or public"}))
			}
		}
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.upstreamBulk(w, r, body, func(ctx context.Context, tx meta.Tx, svc *repository.Service, c repository.Caller) ([]repository.UpstreamItem, string, error) {
		return svc.SetAllUpstreamVisibilityTx(ctx, tx, c, level)
	})
}

// upstreamBulk runs a bulk operation in the transaction of its Idempotency-Key (R-API-014, Q-API-361): all or nothing, answered with
// the repositories it changed and its bulk ID.
func (s *Server) upstreamBulk(w http.ResponseWriter, r *http.Request, body []byte,
	run func(ctx context.Context, tx meta.Tx, svc *repository.Service, c repository.Caller) ([]repository.UpstreamItem, string, error),
) {
	svc, err := s.repositoryService()
	if err == nil && s.idem == nil {
		err = unavailable("The idempotency store")
	}
	var c repository.Caller
	if err == nil {
		c, err = s.upstreamCaller(r)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		items, bulkID, err := run(ctx, tx, svc, c)
		if err != nil {
			return Outcome{}, err
		}
		out := map[string]any{"bulkId": bulkID}
		list := make([]gen.UpstreamRepository, 0, len(items))
		for _, it := range items {
			list = append(list, s.toUpstream(it))
		}
		out["items"] = list
		return Outcome{Status: http.StatusOK, Body: out}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// upstreamAddressBody is the body of setUpstreamRepositoryAddress; account is kept raw, so that an absent member and null differ.
type upstreamAddressBody struct {
	URL          *string                 `json:"url"`
	ExtraHosts   *[]repository.ExtraHost `json:"extraHosts"`
	UpstreamKeys *string                 `json:"upstreamKeys"`
	Account      json.RawMessage         `json:"account"`
}

// SetUpstreamRepositoryAddress implements PUT /api/v1/upstream-repositories/{key}/address.
func (s *Server) SetUpstreamRepositoryAddress(w http.ResponseWriter, r *http.Request, key gen.UpstreamRepositoryKey, _ gen.SetUpstreamRepositoryAddressParams) {
	svc, err := s.repositoryService()
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	var in upstreamAddressBody
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	input := repository.UpstreamAddressInput{URL: in.URL, ExtraHosts: in.ExtraHosts, UpstreamKeys: in.UpstreamKeys}
	if err == nil && in.Account != nil {
		input.AccountSet = true
		if string(in.Account) != "null" {
			var a repository.UpstreamAccountInput
			if err = StrictUnmarshal(in.Account, &a); err == nil {
				input.Account = &a
			}
		}
	}
	var it repository.UpstreamItem
	if err == nil {
		it, err = svc.SetUpstreamAddress(r.Context(), repositoryCaller(r), key, repositoryPrecondition(pre), input)
	}
	if err != nil {
		httpserver.Problem(w, r, repository.UpstreamPointers(err, false))
		return
	}
	s.writeUpstream(w, r, it)
}

// ResetUpstreamRepositoryAddress implements DELETE /api/v1/upstream-repositories/{key}/address.
func (s *Server) ResetUpstreamRepositoryAddress(w http.ResponseWriter, r *http.Request, key gen.UpstreamRepositoryKey, params gen.ResetUpstreamRepositoryAddressParams) {
	svc, err := s.repositoryService()
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	var it repository.UpstreamItem
	if err == nil {
		removeAccount := params.RemoveAccount != nil && *params.RemoveAccount
		it, err = svc.ResetUpstreamAddress(r.Context(), repositoryCaller(r), key, repositoryPrecondition(pre), removeAccount)
	}
	if err != nil {
		httpserver.Problem(w, r, repository.UpstreamPointers(err, false))
		return
	}
	s.writeUpstream(w, r, it)
}

// SetUpstreamRepositoryAccount implements PUT /api/v1/upstream-repositories/{key}/account.
func (s *Server) SetUpstreamRepositoryAccount(w http.ResponseWriter, r *http.Request, key gen.UpstreamRepositoryKey, _ gen.SetUpstreamRepositoryAccountParams) {
	svc, err := s.repositoryService()
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	var in repository.UpstreamAccountInput
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var it repository.UpstreamItem
	if err == nil {
		it, err = svc.SetUpstreamAccount(r.Context(), repositoryCaller(r), key, repositoryPrecondition(pre), in)
	}
	if err != nil {
		httpserver.Problem(w, r, repository.UpstreamPointers(err, true))
		return
	}
	s.writeUpstream(w, r, it)
}

// RemoveUpstreamRepositoryAccount implements DELETE /api/v1/upstream-repositories/{key}/account.
func (s *Server) RemoveUpstreamRepositoryAccount(w http.ResponseWriter, r *http.Request, key gen.UpstreamRepositoryKey, _ gen.RemoveUpstreamRepositoryAccountParams) {
	svc, err := s.repositoryService()
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	var it repository.UpstreamItem
	if err == nil {
		it, err = svc.RemoveUpstreamAccount(r.Context(), repositoryCaller(r), key, repositoryPrecondition(pre))
	}
	if err != nil {
		httpserver.Problem(w, r, repository.UpstreamPointers(err, true))
		return
	}
	s.writeUpstream(w, r, it)
}
