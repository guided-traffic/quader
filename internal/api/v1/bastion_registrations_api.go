package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
)

// The Bastion registrations and their tokens (R-BAS-004, Q-BAS-24). The declarations are in the document:
// endpoints:read for the reads, endpoints:write for the writes, x-quader-if-match on the update, and the coarse authenticated
// form for the revocation of one token, because the handler admits endpoints:write or tokens:revoke, and for the update, because
// the handler requires limits:write for the member limits and endpoints:write for every other (Q-BAS-325). The rules are those of
// bastionmain.Registrations; the handlers decode, call it and encode.

// Capabilities a Bastion registration is managed with (Q-BAS-24, Q-BAS-325).
const (
	capabilityEndpointsWrite = "endpoints:write"
	capabilityTokensRevoke   = "tokens:revoke"
	capabilityLimitsWrite    = "limits:write"
)

// bastionsReady answers 503 and reports false when the replica has no registration service (a worker, or the
// HTTP shell of a test).
func (s *Server) bastionsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.o.Bastions == nil {
		httpserver.Problem(w, r, unavailable("The Bastion registrations"))
		return false
	}
	return true
}

func toBastion(b bastionmain.Registration) gen.Bastion {
	out := gen.Bastion{
		Id: uuidOf(b.ID), Name: b.Name, Description: b.Description, Enabled: b.Enabled,
		Switches: gen.BastionSwitches{Handover: b.Switches.Handover, Gui: b.Switches.GUI, Docs: b.Switches.Docs},
		Limits:   bastionLimitsRep(b.Limits, b.Preset), Status: make([]gen.BastionReplicaStatus, 0, len(b.Status)),
		Egress: bastionmain.EgressText(b.Egress), CreatedAt: utc(b.CreatedAt),
		Tokens: make([]gen.BastionTokenSummary, 0, len(b.Tokens)), Contacts: make([]gen.BastionContact, 0, len(b.Contacts)),
	}
	for _, st := range b.Status {
		var rep gen.BastionReplicaStatus
		viaJSON(st.Status, &rep)
		rep.Replica, rep.ReportedAt = st.Replica, utc(st.ReportedAt)
		out.Status = append(out.Status, rep)
	}
	if b.Declared != nil {
		out.Declared = &gen.BastionDeclaration{Since: utc(b.Declared.Since), SecretName: b.Declared.SecretName}
	}
	for _, t := range b.Tokens {
		sum := gen.BastionTokenSummary{
			Id: uuidOf(t.ID), Hint: t.Hint, Description: t.Description, CreatedAt: utc(t.CreatedAt), Declared: t.Declared,
		}
		if t.ExpiresAt != nil {
			sum.ExpiresAt = refTo(utc(*t.ExpiresAt))
		}
		if t.LastUsedAt != nil {
			sum.LastUsedAt = refTo(utc(*t.LastUsedAt))
		}
		out.Tokens = append(out.Tokens, sum)
	}
	for _, c := range b.Contacts {
		ct := gen.BastionContact{
			Replica: c.Replica, ContactAt: utc(c.ContactAt), ClientAddress: optString(c.ClientAddress), Release: c.Release,
			InterfaceVersion: c.InterfaceVersion, BaseUrl: c.BaseURL, CatalogueKept: c.CatalogueKept,
		}
		if c.EdgeLimit != nil {
			ct.EdgeLimit = refTo(c.EdgeLimit.Header())
		}
		out.Contacts = append(out.Contacts, ct)
	}
	return out
}

// bastionLimitsRep is a registration's limits as the API shows them (R-BAS-093): the members of the interface's shape, and the
// preset the registration reads.
func bastionLimitsRep(l bastionmain.Limits, preset iface.LimitsPreset) gen.BastionLimits {
	out := gen.BastionLimits{Mode: gen.BastionLimitsMode(l.Mode), Preset: gen.BastionLimitsPreset(preset)}
	viaJSON(l.Flood, &out.Flood)
	viaJSON(l.Slots, &out.Slots)
	viaJSON(l.Throughput, &out.Throughput)
	viaJSON(l.AnonymousCeiling, &out.AnonymousCeiling)
	if out.Flood.ExemptNetworks == nil {
		out.Flood.ExemptNetworks = []string{}
	}
	return out
}

// viaJSON copies a value of the interface's shape into the generated type of the same shape.
func viaJSON(from, to any) {
	b, err := json.Marshal(from)
	if err == nil {
		_ = json.Unmarshal(b, to)
	}
}

// ListBastions implements GET /api/v1/bastions.
func (s *Server) ListBastions(w http.ResponseWriter, r *http.Request) {
	if !s.bastionsReady(w, r) {
		return
	}
	rows, err := s.o.Bastions.List(r.Context())
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	items := make([]gen.Bastion, 0, len(rows))
	for _, b := range rows {
		items = append(items, toBastion(b))
	}
	writeJSON(w, r, http.StatusOK, gen.BastionList{Items: items})
}

// CreateBastion implements POST /api/v1/bastions.
func (s *Server) CreateBastion(w http.ResponseWriter, r *http.Request) {
	if !s.bastionsReady(w, r) {
		return
	}
	body, err := ReadBody(w, r, MediaJSON, s.bodyLimit())
	var in gen.NewBastion
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	nr := bastionmain.NewRegistration{Name: in.Name, Description: derefString(in.Description), Source: "api"}
	if in.Egress != nil {
		nr.Egress = *in.Egress
	}
	b, err := s.o.Bastions.Create(r.Context(), s.caller(r), nr)
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	w.Header().Set("Location", location(r, "/api/v1/bastions/"+b.Name))
	SetETag(w, b.Revision, "")
	writeJSON(w, r, http.StatusCreated, toBastion(b))
}

// GetBastion implements GET /api/v1/bastions/{name}.
func (s *Server) GetBastion(w http.ResponseWriter, r *http.Request, name string, _ gen.GetBastionParams) {
	if !s.bastionsReady(w, r) {
		return
	}
	b, err := s.o.Bastions.Get(r.Context(), name)
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	if NotModified(w, r, ETag(b.Revision, "")) {
		return
	}
	SetETag(w, b.Revision, "")
	writeJSON(w, r, http.StatusOK, toBastion(b))
}

// bastionPatchBody is a merge patch of a registration. The read-only members are accepted and ignored.
type bastionPatchBody struct {
	Description json.RawMessage `json:"description"`
	Enabled     json.RawMessage `json:"enabled"`
	Switches    json.RawMessage `json:"switches"`
	Limits      json.RawMessage `json:"limits"`
	Egress      json.RawMessage `json:"egress"`

	ID        json.RawMessage `json:"id"`
	Name      json.RawMessage `json:"name"`
	CreatedAt json.RawMessage `json:"createdAt"`
	Tokens    json.RawMessage `json:"tokens"`
	Contacts  json.RawMessage `json:"contacts"`
	Status    json.RawMessage `json:"status"`
	Declared  json.RawMessage `json:"declared"`
}

// bastionSwitchesPatch is the member switches of a merge patch.
type bastionSwitchesPatch struct {
	Handover json.RawMessage `json:"handover"`
	GUI      json.RawMessage `json:"gui"`
	Docs     json.RawMessage `json:"docs"`
}

// bastionLimitsDoc is a registration's limits as a merge patch applies to them (R-BAS-093): the members of the interface's shape.
type bastionLimitsDoc struct {
	Mode             iface.LimitsMode `json:"mode"`
	Flood            iface.Flood      `json:"flood"`
	Slots            iface.Slots      `json:"slots"`
	Throughput       iface.Throughput `json:"throughput"`
	AnonymousCeiling iface.Ceiling    `json:"anonymousCeiling"`
}

// bastionLimitsPatch reads the member limits of a merge patch: the preset to apply, and a function that merges the rest onto the
// registration's limits inside the update's transaction. absent is true when the patch has no member limits.
func bastionLimitsPatch(raw json.RawMessage) (preset *iface.LimitsPreset, merge func(bastionmain.Limits) (bastionmain.Limits, error), err error) {
	if raw == nil || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil, nil
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, nil, malformed("/limits", "type", "the value must be an object", err)
	}
	if p, ok := members["preset"]; ok {
		delete(members, "preset")
		var name iface.LimitsPreset
		if err := json.Unmarshal(p, &name); err != nil {
			return nil, nil, malformed("/limits/preset", "type", "the value must be a preset's name", err)
		}
		if name != iface.PresetCustom {
			preset = &name
		}
	}
	rest, _ := json.Marshal(members)
	merge = func(cur bastionmain.Limits) (bastionmain.Limits, error) {
		doc, err := ApplyMergePatch(bastionLimitsDoc{
			Mode: cur.Mode, Flood: cur.Flood, Slots: cur.Slots, Throughput: cur.Throughput, AnonymousCeiling: cur.AnonymousCeiling,
		}, rest)
		if err != nil {
			return cur, err
		}
		cur.Mode, cur.Flood, cur.Slots, cur.Throughput, cur.AnonymousCeiling = doc.Mode, doc.Flood, doc.Slots, doc.Throughput, doc.AnonymousCeiling
		return cur, nil
	}
	return preset, merge, nil
}

// UpdateBastion implements PATCH /api/v1/bastions/{name}.
func (s *Server) UpdateBastion(w http.ResponseWriter, r *http.Request, name string, _ gen.UpdateBastionParams) {
	if !s.bastionsReady(w, r) {
		return
	}
	pre, err := ParseIfMatch(r)
	var raw []byte
	if err == nil {
		raw, err = ReadMergePatch(w, r, s.bodyLimit())
	}
	var in bastionPatchBody
	if err == nil {
		err = StrictUnmarshal(raw, &in)
	}
	var patch bastionmain.Patch
	if err == nil {
		patch.Description, err = patchString(in.Description, "/description")
	}
	if err == nil {
		patch.Enabled, err = patchEnabled(in.Enabled, "/enabled")
	}
	if err == nil {
		// The switches (R-BAS-071, R-BAS-102): true or false, a null switches nothing.
		err = patchSwitches(in.Switches, &patch)
	}
	if err == nil {
		// The networks the Bastion connects from (R-BAS-017): the whole list, [] empties it, a null changes nothing.
		patch.Egress, err = patchStrings(in.Egress, "/egress")
	}
	if err == nil {
		// The limits (R-BAS-093): a preset to apply and a merge patch of the rest.
		patch.Preset, patch.Limits, err = bastionLimitsPatch(in.Limits)
	}
	if err == nil {
		err = s.bastionPatchAllowed(r.Context(), in, patch)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	// A change of the mode needs recent authentication (R-LIM-044), asked only when the mode would change.
	patch.ModeChange = func() error { return s.requireRecentAuth(r) }
	b, err := s.o.Bastions.Update(r.Context(), s.caller(r), name, patch, expectedRevision(pre))
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	SetETag(w, b.Revision, "")
	writeJSON(w, r, http.StatusOK, toBastion(b))
}

// patchSwitches reads the member switches of a merge patch into the patch: absent and null are unchanged, as is every switch the
// member leaves out or sets to null.
func patchSwitches(raw json.RawMessage, patch *bastionmain.Patch) error {
	if raw == nil || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	var sw bastionSwitchesPatch
	if err := StrictUnmarshal(raw, &sw); err != nil {
		return err
	}
	var err error
	if patch.Handover, err = patchEnabled(sw.Handover, "/switches/handover"); err != nil {
		return err
	}
	if patch.GUI, err = patchEnabled(sw.GUI, "/switches/gui"); err != nil {
		return err
	}
	patch.Docs, err = patchEnabled(sw.Docs, "/switches/docs")
	return err
}

// bastionPatchAllowed holds a patch to the capabilities of Q-BAS-325: limits:write for the member limits, endpoints:write for every
// other member a patch changes. A patch of read-only members alone needs endpoints:write, as before.
func (s *Server) bastionPatchAllowed(ctx context.Context, in bastionPatchBody, p bastionmain.Patch) error {
	present := func(raw json.RawMessage) bool { return raw != nil && string(bytes.TrimSpace(raw)) != "null" }
	limitsOnly := present(in.Limits) && !present(in.Description) && !present(in.Enabled) && !present(in.Switches) && !present(in.Egress)
	if present(in.Limits) || p.Preset != nil {
		if ok, err := s.holds(ctx, capabilityLimitsWrite); err != nil || !ok {
			return firstErr(err, errs.E(errs.Forbidden, errs.CodeForbidden, "The permission limits:write is required to change a Bastion's limits."))
		}
	}
	if limitsOnly {
		return nil
	}
	if ok, err := s.holds(ctx, capabilityEndpointsWrite); err != nil || !ok {
		return firstErr(err, errs.E(errs.Forbidden, errs.CodeForbidden, "The permission endpoints:write is required."))
	}
	return nil
}

// patchEnabled reads a switch of a merge patch, enabled or a member of switches: absent and null are unchanged, because a null must never
// switch a registration off, nor its handover on or off; any other type is refused.
func patchEnabled(raw json.RawMessage, pointer string) (*bool, error) {
	// A JSON null decodes into a bool as false without an error, which would switch off what the patch did not change: it is
	// read as absent before the decode.
	if raw == nil || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, malformed(pointer, "type", "the value must be true or false", err)
	}
	return &v, nil
}

// patchStrings reads a list of strings of a merge patch: absent and null are unchanged, any other type is refused.
func patchStrings(raw json.RawMessage, pointer string) (*[]string, error) {
	if raw == nil || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var v []string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, malformed(pointer, "type", "the value must be a list of strings", err)
	}
	if v == nil {
		v = []string{}
	}
	return &v, nil
}

// DeleteBastion implements DELETE /api/v1/bastions/{name}.
func (s *Server) DeleteBastion(w http.ResponseWriter, r *http.Request, name string) {
	if !s.bastionsReady(w, r) {
		return
	}
	if err := s.o.Bastions.Delete(r.Context(), s.caller(r), name); err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}

// maxBastionTTLSeconds bounds ttlSeconds before it becomes a duration, so that the multiplication cannot
// overflow; the configured maximum is checked by the token service.
const maxBastionTTLSeconds = 1 << 31

// CreateBastionToken implements POST /api/v1/bastions/{name}/tokens (R-BAS-004, R-AUTH-022): the token is
// issued behind the Idempotency-Key and its value is in this answer only.
func (s *Server) CreateBastionToken(w http.ResponseWriter, r *http.Request, name string, _ gen.CreateBastionTokenParams) {
	if !s.bastionsReady(w, r) || !s.identityReady(w, r) {
		return
	}
	body, err := ReadBody(w, r, MediaJSON, s.bodyLimit())
	var in gen.NewBastionToken
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	var ttl time.Duration
	if err == nil && in.TtlSeconds != nil {
		if *in.TtlSeconds < 1 || *in.TtlSeconds > maxBastionTTLSeconds {
			err = errs.E(errs.Invalid, errs.CodeInvalidTTL, "ttlSeconds must be at least 60 and at most the maximum of auth.tokens.bst.max_ttl.")
		} else {
			ttl = time.Duration(*in.TtlSeconds) * time.Second
		}
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	c := s.caller(r)
	req := bastionmain.TokenRequest{Description: derefString(in.Description), TTL: ttl, Via: via(httpserver.PrincipalFrom(r.Context()))}
	err = s.idem.Do(w, r, body, IdempotentOp{
		Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
			created, err := s.o.Bastions.CreateTokenTx(ctx, tx, c, name, req)
			if err != nil {
				return Outcome{}, identityErr(err)
			}
			return Outcome{
				Status: http.StatusCreated, Body: toBastionTokenCreated(created), SecretID: created.Record.ID,
				Header: map[string]string{"Location": location(r, "/api/v1/bastions/"+name)},
			}, nil
		},
		SecretReplay: tokenIssued,
	})
	if err != nil {
		failIdentity(w, r, err)
	}
}

func toBastionTokenCreated(c token.Created) gen.BastionTokenCreated {
	rec := c.Record
	out := gen.BastionTokenCreated{Id: uuidOf(rec.ID), Token: c.Secret, Hint: rec.Hint, Description: rec.Description, CreatedAt: utc(rec.CreatedAt)}
	if rec.ExpiresAt != nil {
		out.ExpiresAt = utc(*rec.ExpiresAt)
	}
	return out
}

// RevokeBastionToken implements DELETE /api/v1/bastions/{name}/tokens/{id}: endpoints:write or tokens:revoke
// (Q-BAS-24). A token that is revoked already answers 204 as well.
func (s *Server) RevokeBastionToken(w http.ResponseWriter, r *http.Request, name string, id openapi_types.UUID) {
	if !s.bastionsReady(w, r) {
		return
	}
	ctx := r.Context()
	if err := s.requireAny(ctx, capabilityEndpointsWrite, capabilityTokensRevoke); err != nil {
		failIdentity(w, r, err)
		return
	}
	if err := s.o.Bastions.RevokeToken(ctx, s.caller(r), name, id.String()); err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}

// requireAny admits a caller that holds at least one of the capabilities; a failure to decide is returned as it
// is, a caller that holds none gets 403.
func (s *Server) requireAny(ctx context.Context, capabilities ...string) error {
	for _, c := range capabilities {
		ok, err := s.holds(ctx, c)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return errs.E(errs.Forbidden, errs.CodeForbidden, "The permission "+capabilities[0]+" or "+capabilities[len(capabilities)-1]+" is required.")
}
