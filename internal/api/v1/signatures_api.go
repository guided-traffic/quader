package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/signatures"
)

// The management operations of signatures on artifacts (signatures.md R-SIG-034, with the permissions of R-SIG-021 and Q-SIG-39):
// the switch for project trust anchors, the anchors of the installation and of the projects, and the blocked signers. The reads of
// what an artifact carries are signatures_views.go, the security dashboard's list signatures_findings.go. The store records every
// write's audit event, raises the anchor epoch and wakes the trust job in the write's transaction (R-SIG-012, R-SIG-039); this file
// decides the request, maps the answers and the store's errors (internal/errs) to the contract.

// The members of a request and an answer that carry an anchor's material, by kind (R-SIG-015): the JSON of anchors.Material.
const (
	anchorMemberCA        = "ca"
	anchorMemberOpenPGP   = "openpgp"
	anchorMemberPublicKey = "publicKey"
	anchorMemberSigstore  = "sigstore"
)

// anchorMember answers the member of an anchor's kind, "" for a kind that is none of the four.
func anchorMember(kind string) string {
	switch kind {
	case "ca":
		return anchorMemberCA
	case "openpgp":
		return anchorMemberOpenPGP
	case "public-key":
		return anchorMemberPublicKey
	case "sigstore":
		return anchorMemberSigstore
	}
	return ""
}

// anchorMembers are the four members of material, in the order of R-SIG-015's table.
func anchorMembers() []string {
	return []string{anchorMemberCA, anchorMemberOpenPGP, anchorMemberPublicKey, anchorMemberSigstore}
}

func (s *Server) signatureServices() (SignatureServices, error) {
	sv := s.o.Signatures
	if sv.Store == nil {
		return sv, unavailable("The signatures")
	}
	return sv, nil
}

func signatureActor(r *http.Request) audit.Actor {
	p := httpserver.PrincipalFrom(r.Context())
	return audit.Actor{Kind: string(p.Kind), ID: principal.IDOrEmpty(p.ID), Name: p.Name}
}

// ---- the switch (R-SIG-019)

// GetSignatureSettings implements GET /api/v1/signatures/settings.
func (s *Server) GetSignatureSettings(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signatureServices()
	var st signatures.Settings
	if err == nil {
		st, err = sv.Store.Settings(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	SetETag(w, st.Version, "")
	writeJSON(w, r, http.StatusOK, signatureSettingsRep(sv, st))
}

func signatureSettingsRep(sv SignatureServices, st signatures.Settings) gen.SignatureSettings {
	source := gen.SignatureTrustRootSourceRelease
	if st.TrustRoot.Source == signatures.TrustRootRefreshed {
		source = gen.SignatureTrustRootSourceRefreshed
	}
	return gen.SignatureSettings{
		Enabled: sv.Enabled, ProjectAnchors: st.ProjectAnchors, AnchorEpoch: st.AnchorEpoch, Version: st.Version,
		UpdatedAt: st.UpdatedAt, UpdatedBy: optionalText(st.UpdatedBy),
		TrustRoot: gen.SignatureTrustRoot{
			Source: source, FetchedAt: optionalTime(st.TrustRoot.FetchedAt), TufVersion: st.TrustRoot.TUFVersion,
			LastError: optionalText(st.TrustRoot.LastError), LastErrorAt: optionalTime(st.TrustRoot.LastErrorAt),
		},
		Backlog: gen.SignatureBacklog{Pending: st.Backlog.Pending, Stale: st.Backlog.Stale},
	}
}

// UpdateSignatureSettings implements PATCH /api/v1/signatures/settings (merge patch, R-API-013). The recent authentication of
// R-AUTHZ-037's rule is the shared step's (x-quader-recent-auth); If-Match is compared here against the row's version and handed
// to the store, which refuses a change that lost the race (errs.Stale).
func (s *Server) UpdateSignatureSettings(w http.ResponseWriter, r *http.Request, _ gen.UpdateSignatureSettingsParams) {
	ctx := r.Context()
	sv, err := s.signatureServices()
	var pre Precondition
	if err == nil {
		pre, err = requireIfMatch(r)
	}
	var patch []byte
	if err == nil {
		patch, err = ReadMergePatch(w, r, s.bodyLimit())
	}
	var cur signatures.Settings
	if err == nil {
		cur, err = sv.Store.Settings(ctx)
	}
	type settingsDoc struct {
		ProjectAnchors bool `json:"projectAnchors"`
	}
	next := settingsDoc{ProjectAnchors: cur.ProjectAnchors}
	if err == nil {
		next, err = ApplyMergePatch(next, patch)
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	var st signatures.Settings
	if err == nil {
		st, err = sv.Store.UpdateSettings(ctx, signatureActor(r), next.ProjectAnchors, cur.Version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	SetETag(w, st.Version, "")
	writeJSON(w, r, http.StatusOK, signatureSettingsRep(sv, st))
}

// ---- anchors (R-SIG-015 to R-SIG-019)

// anchorMaterial is the material of an anchor as the generated members carry it: the JSON of anchors.Material decodes into it.
type anchorMaterial struct {
	CA        *gen.SignatureAnchorCa        `json:"ca,omitempty"`
	OpenPGP   *gen.SignatureAnchorOpenpgp   `json:"openpgp,omitempty"`
	PublicKey *gen.SignatureAnchorPublicKey `json:"publicKey,omitempty"`
	Sigstore  *gen.SignatureAnchorSigstore  `json:"sigstore,omitempty"`
}

// signatureAnchorRep renders an anchor. withMaterial false leaves out the material, the note and the creator: the installation's
// anchors as a project's page lists them (R-SIG-037), which the project's members read without settings:read.
func signatureAnchorRep(a signatures.AnchorRecord, withMaterial bool) gen.SignatureAnchor {
	out := gen.SignatureAnchor{
		Id: a.ID, Scope: gen.SignatureAnchorScope(a.Scope), Project: optionalText(a.ProjectKey), Key: a.Key, Title: a.Title,
		Kind: gen.SignatureAnchorKind(a.Kind), Enabled: a.Enabled, Applied: a.Applied, Version: a.Version,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, Fingerprints: make([]gen.SignatureFingerprint, 0, len(a.Fingerprints)),
		Signatures: a.Signatures,
	}
	for _, f := range a.Fingerprints {
		out.Fingerprints = append(out.Fingerprints, gen.SignatureFingerprint{
			Kind: f.Kind, Value: f.Value, Subject: optionalText(f.Subject), NotAfter: optionalTime(f.NotAfter),
		})
	}
	if !withMaterial {
		return out
	}
	out.Note, out.CreatedBy = optionalText(a.Note), optionalText(a.CreatedBy)
	var m anchorMaterial
	// The store answers the material it validated on save; a document that does not decode is shown without it rather than failing
	// the list of every anchor.
	if len(a.Material) > 0 && json.Unmarshal(a.Material, &m) == nil {
		out.Ca, out.Openpgp, out.PublicKey, out.Sigstore = m.CA, m.OpenPGP, m.PublicKey, m.Sigstore
	}
	return out
}

// newAnchorBody is the body of a create: the material members are kept as they arrived, so the store's checks (anchors.Validate)
// see exactly what the client sent.
type newAnchorBody struct {
	Key       string          `json:"key"`
	Title     string          `json:"title"`
	Kind      string          `json:"kind"`
	CA        json.RawMessage `json:"ca"`
	OpenPGP   json.RawMessage `json:"openpgp"`
	PublicKey json.RawMessage `json:"publicKey"`
	Sigstore  json.RawMessage `json:"sigstore"`
	Note      *string         `json:"note"`
	Enabled   *bool           `json:"enabled"`
}

func (b newAnchorBody) members() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		anchorMemberCA: b.CA, anchorMemberOpenPGP: b.OpenPGP, anchorMemberPublicKey: b.PublicKey, anchorMemberSigstore: b.Sigstore,
	}
}

func anchorInvalid(pointer, code, detail string) error {
	return errs.E(errs.Invalid, errs.CodeValidation, detail, errs.WithFields(errs.FieldError{Pointer: pointer, Code: code, Detail: detail}))
}

// anchorInput checks the shape of a create (the key's grammar, the title, the kind and that exactly the kind's member is set,
// R-SIG-015) and builds the store's input; the material's own checks are the store's.
func anchorInput(b newAnchorBody, scope, projectID string) (signatures.AnchorInput, error) {
	in := signatures.AnchorInput{Scope: scope, ProjectID: projectID, Key: b.Key, Title: b.Title, Kind: b.Kind, Enabled: b.Enabled}
	if b.Note != nil {
		in.Note = *b.Note
	}
	if !validAnchorKey(b.Key) {
		return in, anchorInvalid("/key", "pattern", "key must be 1 to 64 lower-case letters and digits in groups joined by single hyphens.")
	}
	if n := utf8.RuneCountInString(b.Title); n < 1 || n > 128 || strings.TrimSpace(b.Title) == "" || !plainText(b.Title, false) {
		return in, anchorInvalid("/title", "length", "title must be 1 to 128 characters of text.")
	}
	if utf8.RuneCountInString(in.Note) > 1000 || !plainText(in.Note, true) {
		return in, anchorInvalid("/note", "length", "note must be at most 1000 characters of text.")
	}
	member := anchorMember(b.Kind)
	if member == "" {
		return in, anchorInvalid("/kind", "enum", "kind must be ca, openpgp, public-key or sigstore.")
	}
	// The kind's own member first, then the others in a fixed order, so that the same body always answers the same pointer.
	members := b.members()
	if raw := members[member]; len(raw) == 0 || string(raw) == "null" {
		return in, anchorInvalid("/"+member, "required", "An anchor of kind "+b.Kind+" needs its material in "+member+".")
	}
	for _, name := range anchorMembers() {
		if raw := members[name]; name != member && len(raw) > 0 && string(raw) != "null" {
			return in, anchorInvalid("/"+name, "invalid_value", "An anchor of kind "+b.Kind+" carries no "+name+".")
		}
	}
	mat, err := json.Marshal(map[string]json.RawMessage{member: members[member]})
	if err != nil {
		return in, errs.Wrap(err, "api.anchorInput")
	}
	in.Material = mat
	return in, nil
}

// validAnchorKey is the key's grammar of signature_anchors (R-SIG-015): ^[a-z0-9]+(-[a-z0-9]+)*$, at most 64 characters.
func validAnchorKey(k string) bool {
	if k == "" || len(k) > 64 || k[0] == '-' || k[len(k)-1] == '-' || strings.Contains(k, "--") {
		return false
	}
	for _, c := range k {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// plainText reports whether s is valid UTF-8 without control characters (a line break allowed where lines is true).
func plainText(s string, lines bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) && (!lines || c != '\n') {
			return false
		}
	}
	return true
}

// anchorPatchDoc is what a merge patch of an anchor applies to: the members it may change, with the material of the anchor's kind
// as the stored JSON, so that a patch of one member of the material merges into the rest (R-API-013).
type anchorPatchDoc struct {
	Title     string          `json:"title"`
	Note      *string         `json:"note"`
	Enabled   bool            `json:"enabled"`
	CA        json.RawMessage `json:"ca,omitempty"`
	OpenPGP   json.RawMessage `json:"openpgp,omitempty"`
	PublicKey json.RawMessage `json:"publicKey,omitempty"`
	Sigstore  json.RawMessage `json:"sigstore,omitempty"`
}

func (d anchorPatchDoc) member(name string) json.RawMessage {
	switch name {
	case anchorMemberCA:
		return d.CA
	case anchorMemberOpenPGP:
		return d.OpenPGP
	case anchorMemberPublicKey:
		return d.PublicKey
	}
	return d.Sigstore
}

// anchorPatch turns a merge patch of the anchor cur into the store's patch: only the members the patch names change; the material
// is the kind's member merged with the stored one. A member of another kind's material, and null for the material, are 422.
func anchorPatch(cur signatures.AnchorRecord, body []byte) (signatures.AnchorPatch, error) {
	var named map[string]json.RawMessage
	if err := json.Unmarshal(body, &named); err != nil {
		return signatures.AnchorPatch{}, malformed("", "type", "a merge patch must be a JSON object", err)
	}
	doc := anchorPatchDoc{Title: cur.Title, Note: optionalText(cur.Note), Enabled: cur.Enabled}
	var stored map[string]json.RawMessage
	_ = json.Unmarshal(cur.Material, &stored)
	member := anchorMember(cur.Kind)
	switch member {
	case anchorMemberCA:
		doc.CA = stored[member]
	case anchorMemberOpenPGP:
		doc.OpenPGP = stored[member]
	case anchorMemberPublicKey:
		doc.PublicKey = stored[member]
	case anchorMemberSigstore:
		doc.Sigstore = stored[member]
	}
	for _, name := range anchorMembers() {
		raw, has := named[name]
		switch {
		case !has:
		case name != member:
			return signatures.AnchorPatch{}, anchorInvalid("/"+name, "invalid_value", "An anchor of kind "+cur.Kind+" carries no "+name+".")
		case string(raw) == "null":
			return signatures.AnchorPatch{}, anchorInvalid("/"+name, "required", "The material of an anchor cannot be removed; remove the anchor instead.")
		}
	}
	next, err := ApplyMergePatch(doc, body)
	if err != nil {
		return signatures.AnchorPatch{}, err
	}
	var p signatures.AnchorPatch
	if _, has := named["title"]; has {
		if n := utf8.RuneCountInString(next.Title); n < 1 || n > 128 || strings.TrimSpace(next.Title) == "" || !plainText(next.Title, false) {
			return p, anchorInvalid("/title", "length", "title must be 1 to 128 characters of text.")
		}
		p.Title = &next.Title
	}
	if _, has := named["note"]; has {
		note := ""
		if next.Note != nil {
			note = *next.Note
		}
		if utf8.RuneCountInString(note) > 1000 || !plainText(note, true) {
			return p, anchorInvalid("/note", "length", "note must be at most 1000 characters of text.")
		}
		p.Note = &note
	}
	if _, has := named["enabled"]; has {
		p.Enabled = &next.Enabled
	}
	if _, has := named[member]; has && member != "" {
		if p.Material, err = json.Marshal(map[string]json.RawMessage{member: next.member(member)}); err != nil {
			return p, errs.Wrap(err, "api.anchorPatch")
		}
	}
	return p, nil
}

// signatureID reads the decimal ID of a path (an anchor's, a block's); a text that is none names nothing and answers 404.
func signatureID(text string, notFound func() error) (int64, error) {
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id < 1 {
		return 0, notFound()
	}
	return id, nil
}

func noAnchor() error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "No trust anchor has this ID.")
}

// anchorVersion is the version a write of an anchor names: the If-Match tag's revision, 0 (no check) for If-Match: *.
func anchorVersion(pre Precondition) int64 {
	if rev, ok := pre.Revision(""); ok {
		return rev
	}
	return 0
}

func anchorLocation(r *http.Request, a signatures.AnchorRecord) string {
	return location(r, "/api/v1/signatures/anchors/"+strconv.FormatInt(a.ID, 10))
}

func writeAnchor(w http.ResponseWriter, r *http.Request, status int, a signatures.AnchorRecord) {
	SetETag(w, a.Version, "")
	if status == http.StatusCreated {
		w.Header().Set("Location", anchorLocation(r, a))
	}
	writeJSON(w, r, status, signatureAnchorRep(a, true))
}

// ListSignatureAnchors implements GET /api/v1/signatures/anchors.
func (s *Server) ListSignatureAnchors(w http.ResponseWriter, r *http.Request, params gen.ListSignatureAnchorsParams) {
	sv, err := s.signatureServices()
	q := signatures.AnchorQuery{}
	if err == nil && params.Scope != nil {
		switch sc := string(*params.Scope); sc {
		case "shipped", "configuration", "installation", "project":
			q.Scope = sc
		default:
			err = badParam("scope", "must be shipped, configuration, installation or project")
		}
	}
	var list []signatures.AnchorRecord
	if err == nil {
		list, err = sv.Store.Anchors(r.Context(), q)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.SignatureAnchorList{Items: make([]gen.SignatureAnchor, 0, len(list))}
	for _, a := range list {
		out.Items = append(out.Items, signatureAnchorRep(a, true))
	}
	writeJSON(w, r, http.StatusOK, out)
}

// CreateSignatureAnchor implements POST /api/v1/signatures/anchors: an anchor of the scope installation.
func (s *Server) CreateSignatureAnchor(w http.ResponseWriter, r *http.Request) {
	s.createAnchor(w, r, "installation", "")
}

func (s *Server) createAnchor(w http.ResponseWriter, r *http.Request, scope, projectID string) {
	sv, err := s.signatureServices()
	var body newAnchorBody
	if err == nil {
		err = DecodeJSON(w, r, &body, s.bodyLimit())
	}
	var in signatures.AnchorInput
	if err == nil {
		in, err = anchorInput(body, scope, projectID)
	}
	var a signatures.AnchorRecord
	if err == nil {
		a, err = sv.Store.CreateAnchor(r.Context(), signatureActor(r), in)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeAnchor(w, r, http.StatusCreated, a)
}

// GetSignatureAnchor implements GET /api/v1/signatures/anchors/{id}.
func (s *Server) GetSignatureAnchor(w http.ResponseWriter, r *http.Request, idText gen.SignatureAnchorId) {
	sv, err := s.signatureServices()
	var id int64
	if err == nil {
		id, err = signatureID(idText, noAnchor)
	}
	var a signatures.AnchorRecord
	if err == nil {
		a, err = sv.Store.Anchor(r.Context(), id)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeAnchor(w, r, http.StatusOK, a)
}

// UpdateSignatureAnchor implements PATCH /api/v1/signatures/anchors/{id}: the store refuses what the anchor's scope does not allow
// (409 anchor_managed for a shipped anchor's other members and for an anchor of the Helm values, 403 project_anchors_disabled).
func (s *Server) UpdateSignatureAnchor(w http.ResponseWriter, r *http.Request, id gen.SignatureAnchorId, _ gen.UpdateSignatureAnchorParams) {
	s.updateAnchor(w, r, id, nil)
}

// updateAnchor applies a merge patch to the anchor id; within, when set, refuses an anchor it does not admit with 404 (a project's
// route reaches only that project's anchors).
func (s *Server) updateAnchor(w http.ResponseWriter, r *http.Request, idText string, within func(signatures.AnchorRecord) error) {
	ctx := r.Context()
	sv, err := s.signatureServices()
	var pre Precondition
	if err == nil {
		pre, err = requireIfMatch(r)
	}
	var body []byte
	if err == nil {
		body, err = ReadMergePatch(w, r, s.bodyLimit())
	}
	var id int64
	if err == nil {
		id, err = signatureID(idText, noAnchor)
	}
	var cur signatures.AnchorRecord
	if err == nil {
		cur, err = sv.Store.Anchor(ctx, id)
	}
	if err == nil && within != nil {
		err = within(cur)
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	var p signatures.AnchorPatch
	if err == nil {
		p, err = anchorPatch(cur, body)
	}
	var a signatures.AnchorRecord
	if err == nil {
		a, err = sv.Store.UpdateAnchor(ctx, signatureActor(r), id, p, anchorVersion(pre))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeAnchor(w, r, http.StatusOK, a)
}

// DeleteSignatureAnchor implements DELETE /api/v1/signatures/anchors/{id}: an administrator removes an anchor of the installation
// or of any project, whatever the switch says (R-SIG-019).
func (s *Server) DeleteSignatureAnchor(w http.ResponseWriter, r *http.Request, idText gen.SignatureAnchorId) {
	sv, err := s.signatureServices()
	var id int64
	if err == nil {
		id, err = signatureID(idText, noAnchor)
	}
	if err == nil {
		err = sv.Store.DeleteAnchor(r.Context(), signatureActor(r), id)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- a project's anchors (R-SIG-019, R-SIG-037)

// signatureProject resolves the project of a project route to its ID (uuid text), 404 for one that does not exist; the shared
// step already answered 404 for a project the caller cannot see.
func (s *Server) signatureProject(ctx context.Context, key string) (string, error) {
	svc, err := s.projects()
	if err != nil {
		return "", err
	}
	p, err := svc.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

func anchorNotFound() error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "The project has no trust anchor with this ID.")
}

// ofProject admits only an anchor of the scope project of the project projectID: every other anchor is absent from the route.
func ofProject(projectID string) func(signatures.AnchorRecord) error {
	return func(a signatures.AnchorRecord) error {
		if a.Scope != "project" || a.ProjectID != projectID {
			return anchorNotFound()
		}
		return nil
	}
}

// ListProjectSignatureAnchors implements GET /api/v1/projects/{projectKey}/signatures/anchors: the project's anchors, the switch,
// and the installation's anchors that apply in the project's scope without their material, note and creator (decision of the
// builder: one answer, because the project's members read the page and need not hold settings:read, R-SIG-021, R-SIG-037).
func (s *Server) ListProjectSignatureAnchors(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey) {
	ctx := r.Context()
	sv, err := s.signatureServices()
	var projectID string
	if err == nil {
		projectID, err = s.signatureProject(ctx, projectKey)
	}
	var st signatures.Settings
	if err == nil {
		st, err = sv.Store.Settings(ctx)
	}
	var own, inst []signatures.AnchorRecord
	if err == nil {
		own, err = sv.Store.Anchors(ctx, signatures.AnchorQuery{ProjectID: projectID})
	}
	if err == nil {
		inst, err = sv.Store.Anchors(ctx, signatures.AnchorQuery{})
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.ProjectSignatureAnchorList{
		ProjectAnchors: st.ProjectAnchors, Items: make([]gen.SignatureAnchor, 0, len(own)), Installation: make([]gen.SignatureAnchor, 0, len(inst)),
	}
	for _, a := range own {
		out.Items = append(out.Items, signatureAnchorRep(a, true))
	}
	for _, a := range inst {
		// Only what judges in the project's scope: an anchor switched off judges nowhere, and a timestamping anchor proves times but
		// knows no signer; the store marks both not applied where it says so.
		if a.Applied && a.Scope != "project" {
			out.Installation = append(out.Installation, signatureAnchorRep(a, false))
		}
	}
	writeJSON(w, r, http.StatusOK, out)
}

// CreateProjectSignatureAnchor implements POST /api/v1/projects/{projectKey}/signatures/anchors; the store answers 403
// project_anchors_disabled while the switch is off.
func (s *Server) CreateProjectSignatureAnchor(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey) {
	projectID, err := s.signatureProject(r.Context(), projectKey)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.createAnchor(w, r, "project", projectID)
}

// UpdateProjectSignatureAnchor implements PATCH /api/v1/projects/{projectKey}/signatures/anchors/{id}.
func (s *Server) UpdateProjectSignatureAnchor(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, id gen.SignatureAnchorId, _ gen.UpdateProjectSignatureAnchorParams) {
	projectID, err := s.signatureProject(r.Context(), projectKey)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.updateAnchor(w, r, id, ofProject(projectID))
}

// DeleteProjectSignatureAnchor implements DELETE /api/v1/projects/{projectKey}/signatures/anchors/{id}. The store removes a project
// anchor whatever the switch says, which is the administrator's right (R-SIG-019); an Owner who does not hold
// security-settings:write is refused here with 403 project_anchors_disabled while the switch is off, as every other write of a
// project anchor is (decision of the builder, R-SIG-019's "a write of a project anchor while the switch is off").
func (s *Server) DeleteProjectSignatureAnchor(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, idText gen.SignatureAnchorId) {
	ctx := r.Context()
	sv, err := s.signatureServices()
	var projectID string
	if err == nil {
		projectID, err = s.signatureProject(ctx, projectKey)
	}
	var id int64
	if err == nil {
		id, err = signatureID(idText, anchorNotFound)
	}
	var cur signatures.AnchorRecord
	if err == nil {
		cur, err = sv.Store.Anchor(ctx, id)
	}
	if err == nil {
		err = ofProject(projectID)(cur)
	}
	if err == nil {
		err = s.ownerWhileSwitchOn(ctx, sv)
	}
	if err == nil {
		err = sv.Store.DeleteAnchor(ctx, signatureActor(r), id)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownerWhileSwitchOn refuses a caller who reaches a project anchor's write as the project's Owner alone while the switch is off; a
// holder of security-settings:write passes.
func (s *Server) ownerWhileSwitchOn(ctx context.Context, sv SignatureServices) error {
	admin, err := s.holds(ctx, string(authz.SecuritySettingsWrite))
	if err != nil || admin {
		return err
	}
	st, err := sv.Store.Settings(ctx)
	if err != nil {
		return err
	}
	if !st.ProjectAnchors {
		return errs.E(errs.Forbidden, errs.CodeProjectAnchorsDisabled, "An administrator has not allowed project owners to maintain trust anchors for their project.")
	}
	return nil
}

// ---- blocks (R-SIG-020)

func signatureBlockRep(b signatures.BlockRecord) gen.SignatureBlock {
	return gen.SignatureBlock{
		Id: b.ID, Kind: gen.SignatureBlockKind(b.Kind), Value: b.Value, Issuer: optionalText(b.Issuer), Reason: b.Reason,
		CreatedAt: b.CreatedAt, CreatedBy: optionalText(b.CreatedBy), Signatures: b.Signatures,
	}
}

// ListSignatureBlocks implements GET /api/v1/signatures/blocks.
func (s *Server) ListSignatureBlocks(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signatureServices()
	var list []signatures.BlockRecord
	if err == nil {
		list, err = sv.Store.Blocks(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.SignatureBlockList{Items: make([]gen.SignatureBlock, 0, len(list))}
	for _, b := range list {
		out.Items = append(out.Items, signatureBlockRep(b))
	}
	writeJSON(w, r, http.StatusOK, out)
}

// blockInput checks a block (R-SIG-020): a kind of the four, a value of 1 to 1024 characters of text (a serial's is "<issuer DN>|<serial>"
// as the key IDs carry it), an issuer for an identity and for no other kind, and a reason of 1 to 1000 characters.
func blockInput(b gen.NewSignatureBlock) (signatures.BlockInput, error) {
	in := signatures.BlockInput{Kind: string(b.Kind), Value: b.Value, Reason: b.Reason}
	if b.Issuer != nil {
		in.Issuer = *b.Issuer
	}
	switch b.Kind {
	case gen.Fingerprint, gen.Serial, gen.Keyid, gen.Identity:
	default:
		return in, anchorInvalid("/kind", "enum", "kind must be fingerprint, serial, keyid or identity.")
	}
	if n := utf8.RuneCountInString(in.Value); n < 1 || n > 1024 || strings.TrimSpace(in.Value) == "" || !plainText(in.Value, false) {
		return in, anchorInvalid("/value", "length", "value must be 1 to 1024 characters of text.")
	}
	if utf8.RuneCountInString(in.Issuer) > 512 || !plainText(in.Issuer, false) {
		return in, anchorInvalid("/issuer", "length", "issuer must be at most 512 characters of text.")
	}
	if b.Kind == gen.Identity && strings.TrimSpace(in.Issuer) == "" {
		return in, anchorInvalid("/issuer", "required", "A block of an identity names its OIDC issuer.")
	}
	if b.Kind != gen.Identity && in.Issuer != "" {
		return in, anchorInvalid("/issuer", "invalid_value", "Only a block of an identity names an issuer.")
	}
	if !validReleaseReason(in.Reason) {
		return in, anchorInvalid("/reason", "length", "reason must be 1 to 1000 characters of text.")
	}
	return in, nil
}

// CreateSignatureBlock implements POST /api/v1/signatures/blocks.
func (s *Server) CreateSignatureBlock(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signatureServices()
	var body gen.NewSignatureBlock
	if err == nil {
		err = DecodeJSON(w, r, &body, s.bodyLimit())
	}
	var in signatures.BlockInput
	if err == nil {
		in, err = blockInput(body)
	}
	var b signatures.BlockRecord
	if err == nil {
		b, err = sv.Store.CreateBlock(r.Context(), signatureActor(r), in)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.Header().Set("Location", location(r, "/api/v1/signatures/blocks/"+strconv.FormatInt(b.ID, 10)))
	writeJSON(w, r, http.StatusCreated, signatureBlockRep(b))
}

// DeleteSignatureBlock implements DELETE /api/v1/signatures/blocks/{id}.
func (s *Server) DeleteSignatureBlock(w http.ResponseWriter, r *http.Request, idText string) {
	sv, err := s.signatureServices()
	var id int64
	if err == nil {
		id, err = signatureID(idText, func() error { return errs.E(errs.NotFound, errs.CodeNotFound, "No block has this ID.") })
	}
	if err == nil {
		err = sv.Store.DeleteBlock(r.Context(), signatureActor(r), id)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
