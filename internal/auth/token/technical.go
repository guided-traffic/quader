package token

import (
	"context"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Technical tokens (R-AUTH-045, Q-AUTH-415, Q-AUTH-416). A technical token is a named line of qdr_sat_ secrets of one service
// account, of the instance, a project or a repository. Its ID is the subject of a budget (usage-and-limits.md R-LIM-001): a
// rotation and a new secret keep it, so the token's name, rule and consumption go on. Every row of api_token of type sat names
// its technical token. Who may act on an account's technical tokens is decided by the caller of these methods; the creation
// rules of R-AUTH-027 (lifetimes, the derivation rule, the bounds of an Owner) are the ones of [Service.CreateTx].

// technicalName is the name of a technical token: the system that uses it.
var technicalName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// The states of a technical token: active while a secret is live, else expired or revoked by the end of its last secret.
const (
	StateActive  = "active"
	StateExpired = "expired"
	StateRevoked = "revoked"
)

// TechnicalToken is the record of R-AUTH-045.
type TechnicalToken struct {
	ID          string
	AccountID   string
	Name        string
	Description string
	CreatedAt   time.Time
	// Revision is the ETag of the record (R-API-012).
	Revision int64
	// CreatedBy is the reference of the principal that created it (a username or sa:<name>), "" when it is gone.
	CreatedBy string
	State     string
	// Secrets are the live secrets, newest first (one, two during a rotation's overlap), or the last one of a token that has
	// none.
	Secrets []Secret
}

// Secret is one qdr_sat_ secret of a technical token as the record shows it; never the value or its hash.
type Secret struct {
	ID          string
	Hint        string
	Scope       scope.Scope
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	LastUsedAt  *time.Time
	RotatedFrom string
	Live        bool
}

// TechnicalCreate is the creation of a technical token with its first secret: the name and description of the technical
// token, and the secret's request with the rules of R-AUTH-027 (Type and Description are set here; OwnerID is the account).
type TechnicalCreate struct {
	Request     CreateRequest
	Name        string
	Description string
}

func validTechnical(name, description string) error {
	var fields []errs.FieldError
	if !technicalName.MatchString(name) {
		fields = append(fields, errs.FieldError{
			Pointer: "/name", Code: "invalid_value",
			Detail: "the name of the system that uses the token, ^[a-z0-9][a-z0-9-]{0,62}$, for example nightly-mirror",
		})
	}
	if utf8.RuneCountInString(description) > 1024 {
		fields = append(fields, errs.FieldError{Pointer: "/description", Code: "invalid_value", Detail: "at most 1024 characters"})
	}
	if len(fields) == 0 {
		return nil
	}
	return errs.E(errs.Invalid, errs.CodeValidation, "The technical token is invalid.", errs.WithFields(fields...))
}

// secretRequest is the request of a secret of the technical token name: a service-account token whose row description is the
// technical token's name, which the token lists show beside the hint (R-UI-181).
func secretRequest(r CreateRequest, name string) CreateRequest {
	r.Type, r.Description = TypeSAT, name
	return r
}

// CreateTechnicalTx creates a technical token and its first secret in the caller's transaction (R-AUTH-045): the name is
// unique among the account's technical tokens (409 already_exists), the account holds fewer than
// auth.tokens.max_technical_tokens_per_account (409 technical_token_limit_reached) and fewer than
// max_active_per_principal.sat with a live secret (409 token_limit_reached). acct is the account, for the event.
func (s *Service) CreateTechnicalTx(ctx context.Context, tx meta.Tx, acct principal.ServiceAccount, r TechnicalCreate) (Created, error) {
	if err := validTechnical(r.Name, r.Description); err != nil {
		return Created{}, err
	}
	req := secretRequest(r.Request, r.Name)
	req.OwnerID = acct.ID
	sp, err := s.prepareCreate(ctx, req)
	if err != nil {
		return Created{}, err
	}
	sp.beforeInsert = func(ctx context.Context, tx meta.Tx, qs *q.Queries) (string, error) {
		n, err := qs.IdentityCountTechnicalTokens(ctx, acct.ID)
		if err != nil {
			return "", err
		}
		if int(n) >= s.o.Tokens.MaxTechnicalPerAccount {
			return "", errs.E(errs.Conflict, errs.CodeTechnicalTokenLimitReached,
				"The service account holds the maximum number of technical tokens; delete one first.")
		}
		id := principal.NewID()
		rows, err := qs.IdentityInsertTechnicalToken(ctx, q.IdentityInsertTechnicalTokenParams{
			ID: id, AccountID: acct.ID, Name: r.Name, Description: r.Description, CreatedBy: req.Caller.Actor.ID,
		})
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", errs.E(errs.Conflict, errs.CodeAlreadyExists, "The service account has a technical token with this name.")
		}
		return id, s.o.Recorder.RecordTx(ctx, tx, technicalEvent(audit.EventTechnicalTokenCreated, req.Caller, acct, id, r.Name, nil))
	}
	created, err := s.insertTx(ctx, tx, sp)
	if err != nil {
		return Created{}, principal.DBErr("token.create_technical", err)
	}
	created.Record.TechnicalTokenName = r.Name
	return created, nil
}

// NewSecretTx issues a new secret for the technical token name of the account (R-AUTH-045, Q-AUTH-416): only while it has
// no live secret (409 technical_token_active; rotation renews a live one), under the creation rules of R-AUTH-027. The
// technical token keeps its ID, name, budget and history. 404 for a name the account has no technical token of.
func (s *Service) NewSecretTx(ctx context.Context, tx meta.Tx, acct principal.ServiceAccount, name string, r CreateRequest) (Created, error) {
	if !technicalName.MatchString(name) {
		return Created{}, principal.NotFound("The technical token")
	}
	req := secretRequest(r, name)
	req.OwnerID = acct.ID
	sp, err := s.prepareCreate(ctx, req)
	if err != nil {
		return Created{}, err
	}
	sp.beforeInsert = func(ctx context.Context, _ meta.Tx, qs *q.Queries) (string, error) {
		rows, err := qs.IdentityLockTechnicalToken(ctx, q.IdentityLockTechnicalTokenParams{AccountID: acct.ID, Name: name})
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", principal.NotFound("The technical token")
		}
		live, err := qs.IdentityTechnicalTokenLive(ctx, rows[0].ID)
		if err != nil {
			return "", err
		}
		if live {
			return "", errs.E(errs.Conflict, errs.CodeTechnicalTokenActive,
				"The technical token has a live secret; rotate it (POST /api/v1/tokens/{id}/rotate) to renew it.")
		}
		return rows[0].ID, nil
	}
	created, err := s.insertTx(ctx, tx, sp)
	if err != nil {
		return Created{}, principal.DBErr("token.new_secret", err)
	}
	created.Record.TechnicalTokenName = name
	return created, nil
}

// IssueTechnical is the secret of the command line (`quader admin token create`, R-AUTH-015): a new technical token of the name
// with its first secret, or a new secret of the account's technical token of that name when it has no live one. One
// transaction.
func (s *Service) IssueTechnical(ctx context.Context, acct principal.ServiceAccount, r TechnicalCreate) (Created, error) {
	var created Created
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs := principal.Queries(tx)
		existing, err := qs.IdentityTechnicalTokens(ctx, q.IdentityTechnicalTokensParams{AccountID: acct.ID, Name: r.Name})
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			created, err = s.NewSecretTx(ctx, tx, acct, r.Name, r.Request)
			return err
		}
		created, err = s.CreateTechnicalTx(ctx, tx, acct, r)
		return err
	})
	return created, principal.DBErr("token.issue_technical", err)
}

// ListTechnical lists the technical tokens of an account by name, each with its secrets.
func (s *Service) ListTechnical(ctx context.Context, accountID string) ([]TechnicalToken, error) {
	return s.readTechnical(ctx, accountID, "")
}

// GetTechnical reads the technical token name of an account; 404 when it has none of the name.
func (s *Service) GetTechnical(ctx context.Context, accountID, name string) (TechnicalToken, error) {
	if !technicalName.MatchString(name) {
		return TechnicalToken{}, principal.NotFound("The technical token")
	}
	out, err := s.readTechnical(ctx, accountID, name)
	if err != nil {
		return TechnicalToken{}, err
	}
	if len(out) == 0 {
		return TechnicalToken{}, principal.NotFound("The technical token")
	}
	return out[0], nil
}

// GetTechnicalTx is [Service.GetTechnical] on the caller's transaction: the answer of a create or a new secret shows the
// technical token as the transaction wrote it.
func (s *Service) GetTechnicalTx(ctx context.Context, tx meta.Tx, accountID, name string) (TechnicalToken, error) {
	out, err := technicalOn(ctx, principal.Queries(tx), accountID, name)
	if err != nil {
		return TechnicalToken{}, principal.DBErr("token.technical", err)
	}
	if len(out) == 0 {
		return TechnicalToken{}, principal.NotFound("The technical token")
	}
	return out[0], nil
}

func (s *Service) readTechnical(ctx context.Context, accountID, name string) ([]TechnicalToken, error) {
	if !principal.IsID(accountID) {
		return nil, nil
	}
	var out []TechnicalToken
	err := s.o.DB.Read(ctx, func(db q.DBTX) error {
		var err error
		out, err = technicalOn(ctx, q.New(db), accountID, name)
		return err
	})
	return out, principal.DBErr("token.technical", err)
}

func technicalOn(ctx context.Context, qs *q.Queries, accountID, name string) ([]TechnicalToken, error) {
	rows, err := qs.IdentityTechnicalTokens(ctx, q.IdentityTechnicalTokensParams{AccountID: accountID, Name: name})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	only := ""
	if name != "" {
		only = rows[0].ID
	}
	secrets, err := qs.IdentityTechnicalTokenSecrets(ctx, q.IdentityTechnicalTokenSecretsParams{AccountID: accountID, TechnicalTokenID: only})
	if err != nil {
		return nil, err
	}
	byToken := map[string][]q.IdentityTechnicalTokenSecretsRow{}
	for _, sec := range secrets {
		byToken[sec.TechnicalTokenID] = append(byToken[sec.TechnicalTokenID], sec)
	}
	out := make([]TechnicalToken, 0, len(rows))
	for _, r := range rows {
		t := TechnicalToken{
			ID: r.ID, AccountID: r.AccountID, Name: r.Name, Description: r.Description, CreatedAt: r.CreatedAt.Time, Revision: r.Revision,
			CreatedBy: Owner{Kind: principal.KindFromRow(r.CreatedByKind, false), Name: r.CreatedByName}.Ref(),
		}
		if r.CreatedByName == "" {
			t.CreatedBy = ""
		}
		t.Secrets, t.State = secretsOf(byToken[r.ID])
		out = append(out, t)
	}
	return out, nil
}

// secretsOf picks what a record shows of a technical token's secret rows (newest first): the live ones, or the newest when
// none is live, and the state that follows: active, else revoked when the newest secret was revoked, else expired.
func secretsOf(rows []q.IdentityTechnicalTokenSecretsRow) ([]Secret, string) {
	var live []Secret
	for _, r := range rows {
		if r.Live {
			live = append(live, secretOf(r))
		}
	}
	switch {
	case len(live) > 0:
		return live, StateActive
	case len(rows) == 0:
		return []Secret{}, StateExpired
	}
	last := secretOf(rows[0])
	if last.RevokedAt != nil {
		return []Secret{last}, StateRevoked
	}
	return []Secret{last}, StateExpired
}

func secretOf(r q.IdentityTechnicalTokenSecretsRow) Secret {
	sc, err := scope.ParseStored(r.Scope)
	if err != nil {
		sc = scope.Scope{}
	}
	out := Secret{ID: r.ID, Hint: r.Hint, Scope: sc, CreatedAt: r.CreatedAt.Time, RotatedFrom: r.RotatedFrom, Live: r.Live}
	if r.ExpiresAt.Valid {
		t := r.ExpiresAt.Time
		out.ExpiresAt = &t
	}
	if r.RevokedAt.Valid {
		t := r.RevokedAt.Time
		out.RevokedAt = &t
	}
	if r.LastUsedAt.Valid {
		t := r.LastUsedAt.Time
		out.LastUsedAt = &t
	}
	return out
}

// UpdateTechnical changes the description of a technical token (R-AUTH-045); its name is immutable. expected is the revision
// the caller loaded (If-Match) or errs.AnyRevision (R-API-012): another one answers 412.
func (s *Service) UpdateTechnical(ctx context.Context, c principal.Caller, acct principal.ServiceAccount, name, description string, expected int64) (TechnicalToken, error) {
	if !technicalName.MatchString(name) {
		return TechnicalToken{}, principal.NotFound("The technical token")
	}
	if err := validTechnical(name, description); err != nil {
		return TechnicalToken{}, err
	}
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs := principal.Queries(tx)
		rows, err := qs.IdentityLockTechnicalToken(ctx, q.IdentityLockTechnicalTokenParams{AccountID: acct.ID, Name: name})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return principal.NotFound("The technical token")
		}
		if expected != errs.AnyRevision && rows[0].Revision != expected {
			return errs.Stale("The technical token", rows[0].Revision)
		}
		if rows[0].Description == description {
			return nil
		}
		if err := qs.IdentityUpdateTechnicalToken(ctx, q.IdentityUpdateTechnicalTokenParams{ID: rows[0].ID, Description: description}); err != nil {
			return err
		}
		return s.o.Recorder.RecordTx(ctx, tx, technicalEvent(audit.EventTechnicalTokenUpdated, c, acct, rows[0].ID, name,
			map[string]any{"fields": []string{"description"}}))
	})
	if err != nil {
		return TechnicalToken{}, principal.DBErr("token.update_technical", err)
	}
	return s.GetTechnical(ctx, acct.ID, name)
}

// DeleteTechnical revokes the unrevoked secrets of a technical token, each with its journaled token.revoked and the cascade of
// R-AUTH-029, and deletes the technical token with its secret rows (R-AUTH-045). Its rules go with limits-cleanup and its usage
// stays until its retention (usage-and-limits.md R-LIM-028). 404 for a name the account has no technical token of.
func (s *Service) DeleteTechnical(ctx context.Context, c principal.Caller, acct principal.ServiceAccount, name string) error {
	if !technicalName.MatchString(name) {
		return principal.NotFound("The technical token")
	}
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs := principal.Queries(tx)
		rows, err := qs.IdentityLockTechnicalToken(ctx, q.IdentityLockTechnicalTokenParams{AccountID: acct.ID, Name: name})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return principal.NotFound("The technical token")
		}
		id := rows[0].ID
		secrets, err := qs.IdentityLiveSecretsOf(ctx, id)
		if err != nil {
			return err
		}
		revoked := 0
		for _, sec := range secrets {
			ended, err := s.revokeTree(ctx, tx, qs, c, sec, ReasonAdmin, false)
			if err != nil {
				return err
			}
			revoked += len(ended)
		}
		if _, err := qs.IdentityDeleteTechnicalToken(ctx, id); err != nil {
			return err
		}
		return s.o.Recorder.RecordTx(ctx, tx, technicalEvent(audit.EventTechnicalTokenDeleted, c, acct, id, name,
			map[string]any{"secretsRevoked": revoked}))
	})
	return principal.DBErr("token.delete_technical", err)
}

// technicalEvent is a technicaltoken.* event: the account, the technical token's ID and name, and the account's place.
func technicalEvent(typ string, c principal.Caller, acct principal.ServiceAccount, id, name string, extra map[string]any) audit.Event {
	d := map[string]any{"account": acct.Reference(), "technicalToken": id, "name": name, "level": acct.Level()}
	if acct.Project != "" {
		d["project"] = acct.Project
	}
	if acct.Repository != "" {
		d["repository"] = acct.Repository
	}
	for k, v := range extra {
		d[k] = v
	}
	return audit.Event{
		Type: typ, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
		Subject: audit.Subject{Kind: "technical_token", ID: id, Name: acct.Reference() + "/" + name},
		Details: mustJSON(d),
	}
}
