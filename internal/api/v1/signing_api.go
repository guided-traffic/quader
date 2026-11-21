package v1

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/signing"
	signingstore "github.com/guided-traffic/quader/internal/signing/store"
)

// The operations of Quader's signing identity (signatures.md R-SIG-045 to R-SIG-048, R-SIG-055): its state, the key request, the
// upload of the CA's certificate, the import of a key and its chain, the removal of the current identity, the retirement of a previous
// one, and the trusted root and the chain anyone may fetch. The store records every write's event in its transaction, announces the
// change area and wakes the job (internal/signing/store); this file decides the request and maps the answers. No answer carries key
// material: the store answers identities without their private part, and the passphrase of an import is held in a type that renders
// nothing until it is used once.

// SigningStore is what the operations of the signing identity need; *signingstore.Store implements it.
type SigningStore interface {
	Overview(ctx context.Context) (signingstore.Overview, error)
	CreateKey(ctx context.Context, actor audit.Actor, req signing.KeyRequest, managed bool) (signing.Identity, error)
	UploadCertificate(ctx context.Context, actor audit.Actor, chainPEM, caPEM []byte, managed bool) (signing.Identity, error)
	Import(ctx context.Context, actor audit.Actor, keyPEM, passphrase, chainPEM, caPEM []byte, managed bool) (signing.Identity, error)
	DiscardPending(ctx context.Context, actor audit.Actor) error
	Remove(ctx context.Context, actor audit.Actor, managed bool) error
	Retire(ctx context.Context, actor audit.Actor, id int64) (signing.Identity, error)
}

// SigningSnapshot is the replica's view of the identities the public reads answer from (R-SIG-059); *signingstore.Snapshot implements it.
type SigningSnapshot interface {
	Identities(ctx context.Context) ([]signing.Identity, error)
}

// SigningAttestations reads the bundle of Quader's receipt attestation of a stored file (R-SIG-054); *signingstore.Store implements it.
type SigningAttestations interface {
	Attestation(ctx context.Context, repoID int64, path string) (bundle []byte, found bool, err error)
}

// SigningServices are what the signing operations need; without Store they answer 503.
type SigningServices struct {
	Store    SigningStore
	Snapshot SigningSnapshot
	// Attestations answers the route of a file's attestation; nil answers it 503.
	Attestations SigningAttestations
	// Enabled is signing.enabled; Managed is true while the configuration names the identity's files (the Helm values name it,
	// R-SIG-046); AnnouncePeriod is signing.identity.announce_period.
	Enabled        bool
	Managed        bool
	AnnouncePeriod time.Duration
}

var (
	_ SigningStore        = (*signingstore.Store)(nil)
	_ SigningAttestations = (*signingstore.Store)(nil)
)

func (s *Server) signingServices() (SigningServices, error) {
	sv := s.o.Signing
	if sv.Store == nil || sv.Snapshot == nil {
		return sv, unavailable("The signing identity")
	}
	return sv, nil
}

// GetSigningIdentity implements GET /api/v1/signing/identity.
func (s *Server) GetSigningIdentity(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	var ov signingstore.Overview
	if err == nil {
		ov, err = sv.Store.Overview(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, signingOverviewRep(sv, ov))
}

func signingOverviewRep(sv SigningServices, ov signingstore.Overview) gen.SigningOverview {
	out := gen.SigningOverview{
		State: gen.SigningOverviewStateNone, Enabled: sv.Enabled, Managed: sv.Managed, AnnouncePeriodSeconds: int64(sv.AnnouncePeriod / time.Second),
		Previous: []gen.SigningIdentity{}, Backlog: signingBacklogRep(ov.Backlog),
	}
	for _, i := range ov.Identities {
		rep := signingIdentityRep(i, sv.AnnouncePeriod)
		switch i.State {
		case signing.StateCurrent:
			out.Current = &rep
			out.State = gen.SigningOverviewState(i.Source)
			if i.Source == signing.SourceConfiguration {
				out.Managed = true
			}
		case signing.StateAnnounced:
			out.Announced = &rep
		case signing.StatePending:
			out.Pending = &rep
		case signing.StatePrevious:
			out.Previous = append(out.Previous, rep)
		}
	}
	sort.SliceStable(out.Previous, func(a, b int) bool { return out.Previous[a].Id > out.Previous[b].Id })
	if ov.Refused != nil {
		out.Refused = &gen.SigningRefusal{Reason: ov.Refused.Reason, Fingerprint: optionalText(ov.Refused.Fingerprint), At: ov.Refused.At}
	}
	return out
}

func signingIdentityRep(i signing.Identity, announce time.Duration) gen.SigningIdentity {
	names := i.Names
	if names == nil {
		names = []string{}
	}
	rep := gen.SigningIdentity{
		Id: i.ID, Source: gen.SigningIdentitySource(i.Source), State: gen.SigningIdentityState(i.State), Subject: i.Subject, Issuer: i.Issuer,
		Names: names, NotBefore: optionalTime(i.NotBefore), NotAfter: optionalTime(i.NotAfter), Fingerprint: i.Fingerprint(),
		NpmKeyId: i.NPMKeyID, KeySha256: i.KeyFingerprint(), Csr: optionalText(i.CSR), CreatedAt: i.CreatedAt,
		CreatedBy: optionalText(i.CreatedBy), AnnouncedAt: optionalTime(i.AnnouncedAt), CurrentAt: optionalTime(i.CurrentAt),
		ExpiresAt: optionalTime(i.ExpiresAt), RetiredAt: optionalTime(i.RetiredAt),
	}
	if i.State == signing.StateAnnounced && !i.AnnouncedAt.IsZero() {
		rep.SwitchAt = optionalTime(i.AnnouncedAt.Add(announce))
	}
	return rep
}

func signingBacklogRep(rows []signingstore.Backlog) []gen.SigningBacklog {
	byKind := map[string]*gen.SigningBacklog{}
	var order []string
	for _, k := range []string{signing.WorkNPM, signing.WorkAttestation} {
		byKind[k] = &gen.SigningBacklog{Kind: gen.SigningBacklogKind(k)}
		order = append(order, k)
	}
	for _, r := range rows {
		b, ok := byKind[r.Kind]
		if !ok {
			continue
		}
		if r.Failed {
			b.Failed += r.Rows
			continue
		}
		b.Due += r.Rows
		if !r.OldestDue.IsZero() && (b.OldestDueAt == nil || r.OldestDue.Before(*b.OldestDueAt)) {
			t := r.OldestDue
			b.OldestDueAt = &t
		}
	}
	out := make([]gen.SigningBacklog, 0, len(order))
	for _, k := range order {
		out = append(out, *byKind[k])
	}
	return out
}

// CreateSigningKey implements POST /api/v1/signing/identity/key (R-SIG-047).
func (s *Server) CreateSigningKey(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	var body gen.SigningKeyRequest
	if err == nil {
		err = DecodeJSON(w, r, &body, s.bodyLimit())
	}
	var id signing.Identity
	if err == nil {
		id, err = sv.Store.CreateKey(r.Context(), signatureActor(r), keyRequest(body), sv.Managed)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.Header().Set("Location", location(r, "/api/v1/signing/identity"))
	writeJSON(w, r, http.StatusCreated, signingIdentityRep(id, sv.AnnouncePeriod))
}

func keyRequest(b gen.SigningKeyRequest) signing.KeyRequest {
	var out signing.KeyRequest
	if b.Subject != nil {
		v := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}
		out.Subject = signing.Subject{CN: v(b.Subject.CN), O: v(b.Subject.O), OU: v(b.Subject.OU), L: v(b.Subject.L), ST: v(b.Subject.ST), C: v(b.Subject.C)}
	}
	if b.Uris != nil {
		out.URIs = *b.Uris
	}
	if b.Emails != nil {
		out.Emails = *b.Emails
	}
	return out
}

// UploadSigningCertificate implements PUT /api/v1/signing/identity/certificate (R-SIG-047).
func (s *Server) UploadSigningCertificate(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	var body gen.SigningCertificateUpload
	if err == nil {
		err = DecodeJSON(w, r, &body, s.bodyLimit())
	}
	var id signing.Identity
	if err == nil {
		id, err = sv.Store.UploadCertificate(r.Context(), signatureActor(r), []byte(body.Chain), []byte(signingText(body.Ca)), sv.Managed)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, signingIdentityRep(id, sv.AnnouncePeriod))
}

// ImportSigningIdentity implements POST /api/v1/signing/identity/import (R-SIG-047). The key and the passphrase reach the store and
// nothing else; the passphrase is held as an observability.Secret until that call.
func (s *Server) ImportSigningIdentity(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	var body gen.SigningIdentityImport
	if err == nil {
		err = DecodeJSON(w, r, &body, s.bodyLimit())
	}
	var id signing.Identity
	if err == nil {
		pass := observability.Secret(signingText(body.Passphrase))
		keyPEM := observability.Secret(body.Key)
		id, err = sv.Store.Import(r.Context(), signatureActor(r), []byte(keyPEM.Reveal()), []byte(pass.Reveal()), []byte(body.Chain),
			[]byte(signingText(body.Ca)), sv.Managed)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.Header().Set("Location", location(r, "/api/v1/signing/identity"))
	writeJSON(w, r, http.StatusCreated, signingIdentityRep(id, sv.AnnouncePeriod))
}

// DiscardSigningKey implements DELETE /api/v1/signing/identity/pending (R-SIG-047).
func (s *Server) DiscardSigningKey(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	if err == nil {
		err = sv.Store.DiscardPending(r.Context(), signatureActor(r))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveSigningIdentity implements DELETE /api/v1/signing/identity (R-SIG-048).
func (s *Server) RemoveSigningIdentity(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	if err == nil {
		err = sv.Store.Remove(r.Context(), signatureActor(r), sv.Managed)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RetireSigningIdentity implements POST /api/v1/signing/identities/{id}/retire (R-SIG-048).
func (s *Server) RetireSigningIdentity(w http.ResponseWriter, r *http.Request, idText string) {
	sv, err := s.signingServices()
	var id int64
	if err == nil {
		id, err = signatureID(idText, func() error { return errs.E(errs.NotFound, errs.CodeNotFound, "No signing identity has this ID.") })
	}
	var out signing.Identity
	if err == nil {
		out, err = sv.Store.Retire(r.Context(), signatureActor(r), id)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, signingIdentityRep(out, sv.AnnouncePeriod))
}

// GetSigningTrustedRoot implements GET /api/v1/signing/trusted-root.json (R-SIG-052, Q-SIG-72): public, from the replica's snapshot.
func (s *Server) GetSigningTrustedRoot(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	var ids []signing.Identity
	if err == nil {
		ids, err = sv.Snapshot.Identities(r.Context())
	}
	var doc []byte
	ok := false
	if err == nil {
		doc, ok, err = signing.TrustedRoot(ids)
	}
	if err == nil && !ok {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "No signing identity is current or previous: there is nothing to trust.")
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writePublic(w, signing.TrustedRootMediaType, append(doc, '\n'))
}

// GetSigningChain implements GET /api/v1/signing/chain.pem (R-SIG-052, Q-SIG-72): public, from the replica's snapshot.
func (s *Server) GetSigningChain(w http.ResponseWriter, r *http.Request) {
	sv, err := s.signingServices()
	var ids []signing.Identity
	if err == nil {
		ids, err = sv.Snapshot.Identities(r.Context())
	}
	var chain []byte
	if err == nil {
		for _, i := range ids {
			if i.State == signing.StateCurrent {
				chain = signing.ChainPEM(i)
			}
		}
		if chain == nil {
			err = errs.E(errs.NotFound, errs.CodeNotFound, "No signing identity is current.")
		}
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writePublic(w, "application/pem-certificate-chain", chain)
}

// writePublic answers public material a cache may keep for a minute: the identities change rarely, and a switch is announced ten
// minutes ahead (R-SIG-048).
func writePublic(w http.ResponseWriter, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func signingText(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
