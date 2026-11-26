package bastionmain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The Bastion registrations of the main installation (R-BAS-004): a principal of the kind bastion plus the
// row of the table bastion, the tokens of the type bst that belong to it, and the last contact of each
// replica. The service owns the rules of the page; the management API (internal/api/v1) and the admin
// commands (cmd/quader) call it.

// namePattern is the name rule of R-BAS-004 and of the check principal_bastion_name (migration 00032).
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,19}$`)

// maxDescription is the longest description of a registration or a token, in characters.
const maxDescription = 1024

// reservedName is the one name of the pattern that is no registration name: GET /api/v1/bastions/presets lists the presets
// (R-BAS-004, R-LIM-046), so a registration of that name could not be read.
const reservedName = "presets"

// ValidName reports whether a string is a registration name.
func ValidName(name string) bool { return namePattern.MatchString(name) && name != reservedName }

// Registration is a Bastion registration as the API shows it.
type Registration struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
	// Switches are the switches of its configuration (R-BAS-093): the handover per blob (R-BAS-071), the web interface and its
	// documentation (R-BAS-102), each off for a new registration.
	Switches iface.Switches
	// Limits are the limits it holds itself (R-BAS-093), Place the rules of its place (R-LIM-043), and Preset the preset it reads
	// ([EffectivePreset]).
	Limits Limits
	Place  []PlaceRule
	Preset iface.LimitsPreset
	// Status is the state each replica reported within the last hour, the newest first (R-LIM-045).
	Status []ReplicaState
	// Egress are the networks the Bastion connects from (R-BAS-017, Q-BAS-334): only a request from one of them may forward its
	// caller's address and host. Empty by default.
	Egress    []netip.Prefix
	CreatedAt time.Time
	// Revision is the ETag of the registration (principal.revision, R-API-012).
	Revision int64
	// Tokens are the unrevoked tokens, newest first. They hold neither a value nor a hash.
	Tokens []RegistrationToken
	// Contacts are the last contact of each replica, the most recent first (R-BAS-010).
	Contacts []Contact
	// Declared is set while the installation's configuration declares the registration (R-BAS-127): its description, its networks
	// and its declared token are the configuration's then, and the registration is not deleted on the API. Nil otherwise.
	Declared *Declaration
}

// Declaration is what a registration shows of its declaration (R-BAS-127): since when the configuration declares it, and the Secret
// the declared token comes from as the configuration of the replica that answers names it (empty outside the chart, and on a replica
// whose configuration does not declare it).
type Declaration struct {
	Since      time.Time
	SecretName string
}

// RegistrationToken is the public record of a Bastion token.
type RegistrationToken struct {
	ID          string
	Hint        string
	Description string
	CreatedAt   time.Time
	// ExpiresAt is the expiry; nil for the declared token, which does not expire (R-BAS-127).
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	// Expired says the expiry has passed although the token is not revoked.
	Expired bool
	// Declared says it is the token the registration's configuration declares (R-BAS-127): it ends with a change of its Secret or of
	// the declaration, not on the API.
	Declared bool
}

// Contact is the last handshake of one replica of a Bastion (R-BAS-010).
type Contact struct {
	Replica          string
	ContactAt        time.Time
	ClientAddress    string
	Release          string
	InterfaceVersion int
	BaseURL          string
	TokenID          string
	// EdgeLimit is the edge figure the replica reported (Quader-Bastion-Edge-Limit, R-BAS-093), nil for none; CatalogueKept whether
	// its catalogue survives a restart (Quader-Bastion-Catalogue-Kept, R-BAS-101), nil for none.
	EdgeLimit     *iface.EdgeLimit
	CatalogueKept *bool
}

// ReplicaState is the state a replica reported with its last batch (R-LIM-045): the report as it came, the time it came, and only
// the blocks that have not ended (Q-BAS-443).
type ReplicaState struct {
	Replica    string
	ReportedAt time.Time
	Status     iface.ReplicaStatus
}

// NewRegistration is the request of [Registrations.Create].
type NewRegistration struct {
	Name        string
	Description string
	// Egress are the networks the Bastion connects from, as the API received them; [ParseEgress] validates them.
	Egress []string
	// Source is "api" or "admin-cli", for the audit event.
	Source string
}

// Patch is the request of [Registrations.Update]; a nil member is unchanged.
type Patch struct {
	Description *string
	Enabled     *bool
	// Handover switches the handover per blob on or off (R-BAS-071); GUI and Docs the web interface and its documentation
	// (R-BAS-102).
	Handover *bool
	GUI      *bool
	Docs     *bool
	// Egress replaces the networks the Bastion connects from (R-BAS-017), as the API received them; nil leaves them.
	Egress *[]string
	// Preset applies a preset (R-LIM-046) before Limits: the five kind rules of the place, the flood figures and the mode.
	Preset *iface.LimitsPreset
	// Limits computes the registration's new limits from its current ones, inside the update's transaction (the API applies its
	// merge patch); nil leaves them.
	Limits func(Limits) (Limits, error)
	// ModeChange is asked before a change of the mode is written (R-LIM-044: recent authentication); an error refuses the update.
	ModeChange func() error
}

// RegistrationOptions configure [NewRegistrations].
type RegistrationOptions struct {
	DB       principal.DB
	Notifier principal.Notifier
	Recorder audit.Recorder
	// Tokens issues and revokes the Bastion tokens.
	Tokens *token.Service
	// Max is bastions.max_registrations.
	Max int
	// DeclaredSecrets names, by registration, the Secret of the token the replica's configuration declares (R-BAS-127,
	// bastions.declared.<name>.secret_name); the API shows it beside the mark. Nil for none.
	DeclaredSecrets map[string]string
}

// Registrations is the registration service.
type Registrations struct {
	o RegistrationOptions
}

// NewRegistrations creates the service.
func NewRegistrations(o RegistrationOptions) *Registrations {
	if o.Recorder == nil {
		o.Recorder = audit.Nop{}
	}
	return &Registrations{o: o}
}

func invalid(pointer, code, detail string) error {
	return errs.E(errs.Invalid, errs.CodeValidation, detail, errs.WithFields(errs.FieldError{Pointer: pointer, Code: code, Detail: detail}))
}

func checkDescription(pointer, d string) error {
	if utf8.RuneCountInString(d) > maxDescription {
		return invalid(pointer, "too_long", fmt.Sprintf("The description has at most %d characters.", maxDescription))
	}
	return nil
}

// Create registers a Bastion (R-BAS-004): a name by the rule of R-AUTHZ-002, at most bastions.max_registrations
// registrations, event bastion.created.
func (r *Registrations) Create(ctx context.Context, c principal.Caller, in NewRegistration) (Registration, error) {
	if !ValidName(in.Name) {
		return Registration{}, invalid("/name", "invalid_name", "The name must match ^[a-z][a-z0-9-]{0,19}$ and must not be "+reservedName+".")
	}
	if err := checkDescription("/description", in.Description); err != nil {
		return Registration{}, err
	}
	egress, err := ParseEgress("/egress", in.Egress)
	if err != nil {
		return Registration{}, err
	}
	if in.Source == "" {
		in.Source = "api"
	}
	var out Registration
	err = r.o.DB.InTx(ctx, func(tx meta.Tx) error {
		// The configuration lock of R-ARCH-017 serializes the creates of all replicas before the count.
		if err := cluster.LockConfigObject(ctx, tx, cluster.ConfigBastionRegistrations); err != nil {
			return err
		}
		qs := principal.Queries(tx)
		if _, err := r.insertTx(ctx, tx, c, in, egress); err != nil {
			return err
		}
		out, err = load(ctx, qs, in.Name)
		return err
	})
	if err != nil {
		return Registration{}, principal.DBErr("bastion.create", err)
	}
	return out, nil
}

// insertTx writes a new registration inside tx, which holds the configuration lock (R-ARCH-017): the count against
// bastions.max_registrations, the principal and the row, the five kind rules of the preset Internet, the change areas and
// bastion.created. It returns the principal ID.
func (r *Registrations) insertTx(ctx context.Context, tx meta.Tx, c principal.Caller, in NewRegistration, egress []netip.Prefix) (string, error) {
	qs := principal.Queries(tx)
	n, err := qs.BastionCount(ctx)
	if err != nil {
		return "", err
	}
	if int(n) >= r.o.Max {
		return "", errs.E(errs.Conflict, errs.CodeConflict, fmt.Sprintf("%d Bastion registrations exist, the most bastions.max_registrations allows; delete one first.", n))
	}
	id := principal.NewID()
	rows, err := qs.IdentityInsertPrincipal(ctx, q.IdentityInsertPrincipalParams{ID: id, Kind: string(principal.KindBastion), Name: in.Name, CreatedBy: c.Actor.ID})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", errs.E(errs.Conflict, errs.CodeAlreadyExists, "A Bastion registration with this name exists.")
	}
	if err := qs.BastionInsert(ctx, q.BastionInsertParams{ID: id, Description: in.Description, Egress: EgressText(egress)}); err != nil {
		return "", err
	}
	// The place's five kind rules of the preset Internet (R-LIM-046); the row's columns hold the rest of DefaultLimits.
	if err := insertKindRules(ctx, qs, id, c.Actor.ID); err != nil {
		return "", err
	}
	if err := r.notify(tx, id); err != nil {
		return "", err
	}
	if len(egress) > 0 {
		if err := r.notifyEgress(tx, id); err != nil {
			return "", err
		}
	}
	return id, r.o.Recorder.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventBastionCreated, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
		Subject: audit.Subject{Kind: "bastion", ID: id, Name: in.Name},
		Details: mustJSON(map[string]any{"source": in.Source, "description": in.Description, "egress": EgressText(egress)}),
	})
}

// load reads a registration with its tokens, contacts, the rules of its place and the states of its replicas; a 404 when there is
// none.
func load(ctx context.Context, qs *q.Queries, name string) (Registration, error) {
	rows, err := qs.BastionGet(ctx, name)
	if err != nil {
		return Registration{}, err
	}
	if len(rows) == 0 {
		return Registration{}, notFound()
	}
	g := rows[0]
	out := Registration{
		ID: g.ID, Name: g.Name, Description: g.Description, Enabled: g.Enabled,
		Switches: iface.Switches{Handover: g.Handover, GUI: g.Gui, Docs: g.Docs},
		Limits: limitsOf(limitsColumns{
			mode: g.LimitsMode, preset: g.LimitsPreset, flood: g.LimitsFlood, slots: g.LimitsSlots, throughput: g.LimitsThroughput,
			ceiling: g.LimitsCeiling,
		}),
		Egress: egressOf(g.Egress), CreatedAt: g.CreatedAt.Time, Revision: g.Revision, Declared: declarationOf(optTime(g.DeclaredAt.Time, g.DeclaredAt.Valid)),
	}
	toks, err := qs.BastionListTokens(ctx, q.BastionListTokensParams{ID: g.ID})
	if err != nil {
		return Registration{}, err
	}
	out.Tokens = make([]RegistrationToken, 0, len(toks))
	for _, t := range toks {
		out.Tokens = append(out.Tokens, tokenOf(t.ID, t.Hint, t.Description, t.CreatedAt.Time, optTime(t.ExpiresAt.Time, t.ExpiresAt.Valid), optTime(t.LastUsedAt.Time, t.LastUsedAt.Valid), t.Expired, t.Declared))
	}
	contacts, err := qs.BastionContactList(ctx, g.ID)
	if err != nil {
		return Registration{}, err
	}
	out.Contacts = make([]Contact, 0, len(contacts))
	for _, ct := range contacts {
		out.Contacts = append(out.Contacts, contactOf(ct.Replica, ct.ContactAt.Time, ct.ClientAddress, ct.Release, ct.InterfaceVersion,
			ct.BaseUrl, ct.TokenID, ct.EdgeLimit, ct.CatalogueKept))
	}
	if out.Place, err = placeRules(ctx, qs, g.ID); err != nil {
		return Registration{}, err
	}
	out.Preset = EffectivePreset(out.Limits, out.Place)
	states, err := qs.BastionStatusList(ctx, q.BastionStatusListParams{BastionID: g.ID, WithinSecs: statusRetention.Seconds()})
	if err != nil {
		return Registration{}, err
	}
	out.Status = make([]ReplicaState, 0, len(states))
	for _, st := range states {
		out.Status = append(out.Status, stateOf(st.Replica, st.ReportedAt.Time, st.Status))
	}
	return out, nil
}

// placeRules reads the rules of a registration's place.
func placeRules(ctx context.Context, qs *q.Queries, id string) ([]PlaceRule, error) {
	rows, err := qs.BastionPlaceRules(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]PlaceRule, 0, len(rows))
	for _, pr := range rows {
		d := placeRuleDoc{ID: pr.ID, WhoKind: pr.WhoKind, WhoRef: pr.WhoRef, Profile: pr.Profile, Builtin: pr.Builtin}
		_ = json.Unmarshal(pr.Overrides, &d.Overrides)
		_ = json.Unmarshal(pr.Figures, &d.Figures)
		out = append(out, d.rule())
	}
	return out, nil
}

// contactOf is a contact row as the registration shows it.
func contactOf(replica string, at time.Time, addr, release string, version int32, baseURL, tokenID, edge, kept string) Contact {
	c := Contact{
		Replica: replica, ContactAt: at, ClientAddress: addr, Release: release, InterfaceVersion: int(version), BaseURL: baseURL, TokenID: tokenID,
	}
	if e, err := iface.ParseEdgeLimit(edge); edge != "" && err == nil {
		c.EdgeLimit = &e
	}
	if b, err := strconv.ParseBool(kept); kept != "" && err == nil {
		c.CatalogueKept = &b
	}
	return c
}

func tokenOf(id, hint, desc string, created time.Time, expires, lastUsed *time.Time, expired, declared bool) RegistrationToken {
	return RegistrationToken{
		ID: id, Hint: hint, Description: desc, CreatedAt: created, ExpiresAt: expires, LastUsedAt: lastUsed, Expired: expired, Declared: declared,
	}
}

// optTime is a nullable column's time, nil when it is NULL.
func optTime(t time.Time, valid bool) *time.Time {
	if !valid {
		return nil
	}
	return &t
}

// declarationOf is the mark of a registration the configuration declares (R-BAS-127); nil when it is not set. The Secret's name is
// the replica's own configuration's ([Registrations.withSecrets]).
func declarationOf(at *time.Time) *Declaration {
	if at == nil {
		return nil
	}
	return &Declaration{Since: *at}
}

// withSecrets names the Secret of each declared registration as the replica's configuration does.
func (r *Registrations) withSecrets(regs ...*Registration) {
	for _, g := range regs {
		if g.Declared != nil {
			g.Declared.SecretName = r.o.DeclaredSecrets[g.Name]
		}
	}
}

// refuseDeclared refuses a patch that changes a member the configuration declares for the registration cur, when declared says it is
// declared (R-BAS-127); a patch that names them with their value changes nothing and passes. egress is the patch's list, parsed.
func refuseDeclared(declared bool, cur Registration, p Patch, egress []netip.Prefix) error {
	if !declared {
		return nil
	}
	if p.Description != nil && *p.Description != cur.Description {
		return managedByConfig("The description of " + cur.Name)
	}
	if p.Egress != nil && !slices.Equal(egress, cur.Egress) {
		return managedByConfig("The networks " + cur.Name + " connects from")
	}
	return nil
}

// managedByConfig is the 409 of a change the configuration decides (R-BAS-127).
func managedByConfig(what string) error {
	return errs.E(errs.Conflict, errs.CodeManagedByConfig,
		what+" is declared in the installation's configuration (bastions.declared); change it there and roll out.")
}

// notFound is the 404 of a registration that does not exist.
func notFound() error { return principal.NotFound("The Bastion registration") }

// Get returns a registration, or a 404.
func (r *Registrations) Get(ctx context.Context, name string) (Registration, error) {
	var out Registration
	err := r.o.DB.Read(ctx, func(db q.DBTX) error {
		var err error
		out, err = load(ctx, q.New(db), name)
		return err
	})
	r.withSecrets(&out)
	return out, principal.DBErr("bastion.get", err)
}

// List returns every registration ordered by name, with its tokens and contacts: there are at most
// bastions.max_registrations of them, so it is one read.
func (r *Registrations) List(ctx context.Context) ([]Registration, error) {
	var out []Registration
	err := r.o.DB.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		rows, err := qs.BastionList(ctx, q.BastionListParams{MaxRows: int32(max(r.o.Max, 1))}) //nolint:gosec // bounded by the configuration (at most 256)
		if err != nil {
			return err
		}
		toks, err := qs.BastionListTokensOfAll(ctx)
		if err != nil {
			return err
		}
		contacts, err := qs.BastionContactListAll(ctx)
		if err != nil {
			return err
		}
		kinds, err := qs.BastionPlaceRulesOfAll(ctx)
		if err != nil {
			return err
		}
		states, err := qs.BastionStatusListAll(ctx, statusRetention.Seconds())
		if err != nil {
			return err
		}
		out = make([]Registration, 0, len(rows))
		byID := map[string]int{}
		for _, g := range rows {
			byID[g.ID] = len(out)
			out = append(out, Registration{
				ID: g.ID, Name: g.Name, Description: g.Description, Enabled: g.Enabled,
				Switches: iface.Switches{Handover: g.Handover, GUI: g.Gui, Docs: g.Docs},
				Limits: limitsOf(limitsColumns{
					mode: g.LimitsMode, preset: g.LimitsPreset, flood: g.LimitsFlood, slots: g.LimitsSlots, throughput: g.LimitsThroughput,
					ceiling: g.LimitsCeiling,
				}),
				Egress: egressOf(g.Egress), CreatedAt: g.CreatedAt.Time, Revision: g.Revision, Tokens: []RegistrationToken{},
				Contacts: []Contact{}, Place: []PlaceRule{}, Status: []ReplicaState{}, Declared: declarationOf(optTime(g.DeclaredAt.Time, g.DeclaredAt.Valid)),
			})
		}
		for _, k := range kinds {
			if i, ok := byID[k.BastionID]; ok {
				pr := PlaceRule{WhoKind: limits.WhoEvery, WhoRef: k.WhoRef, Profile: k.Profile}
				_ = json.Unmarshal(k.Overrides, &pr.Overrides)
				out[i].Place = append(out[i].Place, pr)
			}
		}
		for _, st := range states {
			if i, ok := byID[st.BastionID]; ok {
				out[i].Status = append(out[i].Status, stateOf(st.Replica, st.ReportedAt.Time, st.Status))
			}
		}
		for _, t := range toks {
			if i, ok := byID[t.BastionID]; ok {
				out[i].Tokens = append(out[i].Tokens, tokenOf(t.ID, t.Hint, t.Description, t.CreatedAt.Time, optTime(t.ExpiresAt.Time, t.ExpiresAt.Valid), optTime(t.LastUsedAt.Time, t.LastUsedAt.Valid), t.Expired, t.Declared))
			}
		}
		for _, ct := range contacts {
			if i, ok := byID[ct.BastionID]; ok {
				out[i].Contacts = append(out[i].Contacts, contactOf(ct.Replica, ct.ContactAt.Time, ct.ClientAddress, ct.Release,
					ct.InterfaceVersion, ct.BaseUrl, ct.TokenID, ct.EdgeLimit, ct.CatalogueKept))
			}
		}
		for i := range out {
			// The list reads the kind rules of each place only, which the preset is read from; the place's rules are the registration's
			// own document's.
			out[i].Preset = EffectivePreset(out[i].Limits, out[i].Place)
			out[i].Place = nil
			r.withSecrets(&out[i])
		}
		return nil
	})
	return out, principal.DBErr("bastion.list", err)
}

// Update changes the description and enables or disables a registration (R-BAS-004). expected is the revision
// the caller loaded (If-Match) or errs.AnyRevision for the blind write. A disabled registration refuses its
// tokens like a disabled service account's (R-AUTH-020) within the revocation bound; enabling it restores them,
// because disabling revokes nothing. A new description records bastion.updated; a disable records
// bastion.disabled with its journal entry, so a restore to a point before it disables the registration again
// (R-HA-054, Q-BAS-145); an enable records bastion.enabled. The switch of the handover per blob records
// bastion.handover.enabled or the journaled bastion.handover.disabled (R-BAS-071). New networks the Bastion connects from record
// bastion.updated with the field egress and the lists before and after (R-BAS-017). A change of the configuration - a switch, a
// preset applied, the mode, the flood figures, the slots, the throughput or the anonymous ceiling - records one
// bastion.configuration.changed with the parts and their values before and after (R-BAS-093). A patch that changes nothing writes
// nothing and records nothing.
func (r *Registrations) Update(ctx context.Context, c principal.Caller, name string, p Patch, expected int64) (Registration, error) {
	if p.Description != nil {
		if err := checkDescription("/description", *p.Description); err != nil {
			return Registration{}, err
		}
	}
	if p.Preset != nil {
		if _, ok := Presets()[*p.Preset]; !ok {
			return Registration{}, invalid("/limits/preset", "enum", "The preset is internet, partnerNetwork or internal; custom is what a registration reads once its figures differ from its preset's.")
		}
	}
	var egress []netip.Prefix
	if p.Egress != nil {
		var err error
		if egress, err = ParseEgress("/egress", *p.Egress); err != nil {
			return Registration{}, err
		}
	}
	var out Registration
	err := r.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs := principal.Queries(tx)
		rows, err := qs.BastionLock(ctx, name)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return notFound()
		}
		lock := rows[0]
		if expected != errs.AnyRevision && lock.Revision != expected {
			return errs.Stale("The Bastion registration", lock.Revision)
		}
		cur, err := load(ctx, qs, name)
		if err != nil {
			return err
		}
		if err := refuseDeclared(lock.Declared, cur, p, egress); err != nil {
			return err
		}
		subject := audit.Subject{Kind: "bastion", ID: lock.ID, Name: name}
		if p.Description != nil && *p.Description != cur.Description {
			if err := qs.BastionUpdateDescription(ctx, q.BastionUpdateDescriptionParams{ID: lock.ID, Description: *p.Description}); err != nil {
				return err
			}
			if err := r.o.Recorder.RecordTx(ctx, tx, audit.Event{
				Type: audit.EventBastionUpdated, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
				Subject: subject, Details: mustJSON(map[string]any{"fields": []string{"description"}}),
			}); err != nil {
				return err
			}
		}
		if p.Enabled != nil && *p.Enabled != cur.Enabled {
			changed, err := qs.BastionSetEnabled(ctx, q.BastionSetEnabledParams{ID: lock.ID, Enabled: *p.Enabled})
			if err != nil {
				return err
			}
			if len(changed) > 0 {
				if err := principal.NotifyPrincipals(r.o.Notifier, tx, principal.ChangePrincipal, lock.ID); err != nil {
					return err
				}
				ev := audit.Event{
					Type: audit.EventBastionEnabled, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
					Subject: subject, Details: mustJSON(map[string]any{}),
				}
				if !*p.Enabled {
					ev.Type, ev.Journal = audit.EventBastionDisabled, registrationEntry(lock.ID, name)
				}
				if err := r.o.Recorder.RecordTx(ctx, tx, ev); err != nil {
					return err
				}
			}
		}
		var ch configChange
		if p.Handover != nil && *p.Handover != cur.Switches.Handover {
			if err := r.setHandover(ctx, tx, c, subject, *p.Handover); err != nil {
				return err
			}
			ch.compare("switches.handover", cur.Switches.Handover, *p.Handover)
		}
		if p.Egress != nil && !slices.Equal(egress, cur.Egress) {
			if err := r.setEgress(ctx, tx, c, subject, cur.Egress, egress); err != nil {
				return err
			}
		}
		if err := r.setConfiguration(ctx, tx, c, cur, p, &ch); err != nil {
			return err
		}
		if err := ch.record(ctx, tx, r.o.Recorder, c, subject); err != nil {
			return err
		}
		out, err = load(ctx, qs, name)
		return err
	})
	if err != nil {
		return Registration{}, principal.DBErr("bastion.update", err)
	}
	return out, nil
}

// setConfiguration writes the switches gui and docs and the limits of the registration cur inside the update's transaction (R-BAS-093):
// a preset first (its kind rules, flood figures and mode, R-LIM-046), then the limits the patch computes from the result, held to the
// rules a Bastion applies them by (ValidateLimits); a change of the mode is asked first (R-LIM-044). It announces the change area of
// the configuration, and the area limits when a preset rewrote a kind rule, and notes each part it changed in ch.
func (r *Registrations) setConfiguration(ctx context.Context, tx meta.Tx, c principal.Caller, cur Registration, p Patch, ch *configChange) error {
	if p.GUI == nil && p.Docs == nil && p.Preset == nil && p.Limits == nil {
		return nil
	}
	qs := principal.Queries(tx)
	sw := cur.Switches
	if p.GUI != nil {
		sw.GUI = *p.GUI
	}
	if p.Docs != nil {
		sw.Docs = *p.Docs
	}
	next := cur.Limits
	var rewritten []string
	if p.Preset != nil {
		pre := Presets()[*p.Preset]
		next.Preset, next.Flood, next.Mode = *p.Preset, pre.Flood, pre.Mode
		var refs, profiles []string
		for _, k := range kindOrder() {
			refs, profiles = append(refs, k), append(profiles, pre.Kinds[k])
		}
		rows, err := qs.BastionKindRulesApply(ctx, q.BastionKindRulesApplyParams{ID: cur.ID, WhoRefs: refs, Profiles: profiles, Actor: c.Actor.ID})
		if err != nil {
			return err
		}
		for _, row := range rows {
			rewritten = append(rewritten, row.WhoRef)
		}
	}
	if p.Limits != nil {
		var err error
		if next, err = p.Limits(next); err != nil {
			return err
		}
	}
	if err := ValidateLimits(next); err != nil {
		return invalid("/limits", "invalid", err.Error())
	}
	if next.Mode != cur.Limits.Mode && p.ModeChange != nil {
		if err := p.ModeChange(); err != nil {
			return err
		}
	}
	flood, slots, throughput, ceiling := columnsOf(next)
	written, err := qs.BastionSetConfiguration(ctx, q.BastionSetConfigurationParams{
		ID: cur.ID, Gui: sw.GUI, Docs: sw.Docs, LimitsMode: string(next.Mode), LimitsPreset: string(next.Preset), LimitsFlood: flood,
		LimitsSlots: slots, LimitsThroughput: throughput, LimitsCeiling: ceiling,
	})
	if err != nil {
		return err
	}
	ch.compare("switches.gui", cur.Switches.GUI, sw.GUI)
	ch.compare("switches.docs", cur.Switches.Docs, sw.Docs)
	ch.compare("mode", cur.Limits.Mode, next.Mode)
	ch.compare("preset", cur.Limits.Preset, next.Preset)
	ch.compare("flood", cur.Limits.Flood, next.Flood)
	ch.compare("slots", cur.Limits.Slots, next.Slots)
	ch.compare("throughput", cur.Limits.Throughput, next.Throughput)
	ch.compare("anonymousCeiling", cur.Limits.AnonymousCeiling, next.AnonymousCeiling)
	if len(rewritten) > 0 {
		ch.parts = append(ch.parts, "rules")
		ch.kinds = rewritten
		if err := r.notifyArea(tx, cluster.AreaLimits, cur.ID); err != nil {
			return err
		}
	}
	if len(written) > 0 {
		return r.notifyArea(tx, cluster.AreaBastionConfiguration, cur.ID)
	}
	return nil
}

// configChange collects the parts of a registration's configuration an update changed, for bastion.configuration.changed.
type configChange struct {
	parts         []string
	before, after map[string]any
	// kinds are the kind rules a preset rewrote.
	kinds []string
}

// compare notes the part when its value before and after differ.
func (ch *configChange) compare(part string, before, after any) {
	if string(mustJSON(before)) == string(mustJSON(after)) {
		return
	}
	if ch.before == nil {
		ch.before, ch.after = map[string]any{}, map[string]any{}
	}
	ch.parts = append(ch.parts, part)
	ch.before[part], ch.after[part] = before, after
}

// record writes bastion.configuration.changed when a part changed (R-BAS-093): the feed maps it to settings, so the Bastion
// handshakes again within one poll.
func (ch *configChange) record(ctx context.Context, tx meta.Tx, rec audit.Recorder, c principal.Caller, subject audit.Subject) error {
	if len(ch.parts) == 0 {
		return nil
	}
	d := map[string]any{"parts": ch.parts, "before": ch.before, "after": ch.after}
	if ch.before == nil {
		d["before"], d["after"] = map[string]any{}, map[string]any{}
	}
	if len(ch.kinds) > 0 {
		d["kindRules"] = ch.kinds
	}
	return rec.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventBastionConfigurationChanged, Outcome: audit.Success, Actor: c.Actor, Source: c.Source, Subject: subject,
		Details: mustJSON(d),
	})
}

// notifyArea announces a change area for the registration id.
func (r *Registrations) notifyArea(tx meta.Tx, area, id string) error {
	if r.o.Notifier == nil {
		return nil
	}
	return r.o.Notifier.Notify(tx, area, 1, map[string]string{"bastion": id})
}

// notify announces a registration created or deleted: its configuration and the rules of its place (R-BAS-093, R-LIM-043).
func (r *Registrations) notify(tx meta.Tx, id string) error {
	if err := r.notifyArea(tx, cluster.AreaBastionConfiguration, id); err != nil {
		return err
	}
	return r.notifyArea(tx, cluster.AreaLimits, id)
}

// insertKindRules writes the five kind rules of the preset Internet at a new registration's place (R-LIM-046).
func insertKindRules(ctx context.Context, qs *q.Queries, id, actor string) error {
	preset := Presets()[iface.PresetInternet]
	var refs, profiles, notes []string
	for _, k := range kindOrder() {
		refs, profiles, notes = append(refs, k), append(profiles, preset.Kinds[k]), append(notes, kindNote(k))
	}
	return qs.BastionKindRulesInsert(ctx, q.BastionKindRulesInsertParams{ID: id, WhoRefs: refs, Profiles: profiles, Notes: notes, Actor: actor})
}

// setHandover switches the handover per blob of the registration subject (R-BAS-071) inside the update's transaction: the
// member, the change area every replica's snapshot follows, and bastion.handover.enabled, or bastion.handover.disabled with the
// journal entry, so that a restore to a point before the switch-off keeps the handover off (R-HA-054). A member that already
// has the value writes nothing.
func (r *Registrations) setHandover(ctx context.Context, tx meta.Tx, c principal.Caller, subject audit.Subject, on bool) error {
	changed, err := principal.Queries(tx).BastionSetHandover(ctx, q.BastionSetHandoverParams{ID: subject.ID, Handover: on})
	if err != nil || len(changed) == 0 {
		return err
	}
	if r.o.Notifier != nil {
		if err := r.o.Notifier.Notify(tx, cluster.AreaBastionHandover, 1, map[string]string{"bastion": subject.ID}); err != nil {
			return err
		}
	}
	ev := audit.Event{
		Type: audit.EventBastionHandoverEnabled, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
		Subject: subject, Details: mustJSON(map[string]any{}),
	}
	if !on {
		ev.Type, ev.Journal = audit.EventBastionHandoverDisabled, handoverEntry(subject.ID, subject.Name)
	}
	return r.o.Recorder.RecordTx(ctx, tx, ev)
}

// setEgress replaces the networks the Bastion of the registration subject connects from (R-BAS-017) inside the update's transaction:
// the member, the change area every replica's snapshot follows, and bastion.updated with the lists before and after. A list that
// already is the one asked for writes nothing.
func (r *Registrations) setEgress(ctx context.Context, tx meta.Tx, c principal.Caller, subject audit.Subject, before, after []netip.Prefix) error {
	changed, err := principal.Queries(tx).BastionSetEgress(ctx, q.BastionSetEgressParams{ID: subject.ID, Egress: EgressText(after)})
	if err != nil || len(changed) == 0 {
		return err
	}
	if err := r.notifyEgress(tx, subject.ID); err != nil {
		return err
	}
	return r.o.Recorder.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventBastionUpdated, Outcome: audit.Success, Actor: c.Actor, Source: c.Source, Subject: subject,
		Details: mustJSON(map[string]any{"fields": []string{"egress"}, "egressBefore": EgressText(before), "egressAfter": EgressText(after)}),
	})
}

// notifyEgress announces the change area of the networks, so every replica's snapshot reads the registration again.
func (r *Registrations) notifyEgress(tx meta.Tx, id string) error {
	if r.o.Notifier == nil {
		return nil
	}
	return r.o.Notifier.Notify(tx, cluster.AreaBastionEgress, 1, map[string]string{"bastion": id})
}

// Delete deletes a registration: its tokens are revoked first (token.revoked with the journal entry of each,
// R-HA-054), then the registration and its contacts go; bastion.deleted is the last event, with the journal entry
// that disables the registration a restore brings back (Q-BAS-145).
func (r *Registrations) Delete(ctx context.Context, c principal.Caller, name string) error {
	err := r.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs := principal.Queries(tx)
		rows, err := qs.BastionLock(ctx, name)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return notFound()
		}
		if rows[0].Declared {
			return errs.E(errs.Conflict, errs.CodeManagedByConfig,
				name+" is declared in the installation's configuration (bastions.declared), which would create it again; remove the declaration and roll out first.")
		}
		id := rows[0].ID
		revoked, err := r.o.Tokens.RevokeAllOfBastionTx(ctx, tx, c, id, token.ReasonAdmin)
		if err != nil {
			return err
		}
		if _, err := qs.IdentityDeletePrincipal(ctx, id); err != nil {
			return err
		}
		if err := principal.NotifyPrincipals(r.o.Notifier, tx, principal.ChangePrincipal, id); err != nil {
			return err
		}
		if err := r.notify(tx, id); err != nil {
			return err
		}
		return r.o.Recorder.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventBastionDeleted, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
			Subject: audit.Subject{Kind: "bastion", ID: id, Name: name}, Details: mustJSON(map[string]any{"tokensRevoked": len(revoked)}),
			Journal: registrationEntry(id, name),
		})
	})
	return principal.DBErr("bastion.delete", err)
}

// registrationEntry is the journal entry of bastion.disabled and bastion.deleted: the registration's principal ID and,
// for display, its name. The replay disables the registration of that ID (R-HA-054, Q-BAS-145).
func registrationEntry(id, name string) json.RawMessage {
	return mustJSON(map[string]string{"principalId": id, "name": name})
}

// TokenRequest is the request of [Registrations.CreateTokenTx].
type TokenRequest struct {
	Description string
	// TTL is the lifetime; zero applies auth.tokens.bst.default_ttl.
	TTL time.Duration
	// Via is created_via: ui, api or admin-cli.
	Via string
}

// CreateTokenTx issues a Bastion token for a registration inside the caller's transaction (R-BAS-004,
// R-AUTH-022): the management API makes the idempotent create one transaction with its idempotency row. The
// secret is in the result only.
func (r *Registrations) CreateTokenTx(ctx context.Context, tx meta.Tx, c principal.Caller, name string, in TokenRequest) (token.Created, error) {
	if utf8.RuneCountInString(in.Description) > maxDescription {
		return token.Created{}, invalid("/description", "too_long", fmt.Sprintf("The description has at most %d characters.", maxDescription))
	}
	qs := principal.Queries(tx)
	rows, err := qs.BastionGet(ctx, name)
	if err != nil {
		return token.Created{}, principal.DBErr("bastion.token", err)
	}
	if len(rows) == 0 {
		return token.Created{}, notFound()
	}
	desc := in.Description
	if desc == "" {
		desc = "Bastion " + name
	}
	return r.o.Tokens.CreateBastionTx(ctx, tx, token.BastionRequest{
		Caller: c, BastionID: rows[0].ID, BastionName: name, Description: desc, TTL: in.TTL, Via: in.Via,
	})
}

// CreateToken is [Registrations.CreateTokenTx] in a transaction of its own (the admin command).
func (r *Registrations) CreateToken(ctx context.Context, c principal.Caller, name string, in TokenRequest) (token.Created, error) {
	var created token.Created
	err := r.o.DB.InTx(ctx, func(tx meta.Tx) error {
		var err error
		created, err = r.CreateTokenTx(ctx, tx, c, name, in)
		return err
	})
	if err != nil {
		return token.Created{}, principal.DBErr("bastion.token", err)
	}
	return created, nil
}

// RevokeToken revokes one token of a registration (R-BAS-004); a token that is revoked already is not an error.
func (r *Registrations) RevokeToken(ctx context.Context, c principal.Caller, name, id string) error {
	return r.o.Tokens.RevokeBastionToken(ctx, c, name, id)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // the values are maps of strings, numbers and booleans
	}
	return b
}
