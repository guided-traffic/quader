// Package edge holds the conversions every HTTP entry point of the edge layer
// shares: the request to the subject authorization decides on and to the actor of
// an audit event, and the holder of the index a request decides on. It imports
// httpserver, authz and audit and none of the entry points (internal/api/v1, the
// raw /files/ routes, the format dispatcher), which all import it; the
// architecture test keeps it that way.
package edge

import (
	"context"
	"net/http"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// ActorFrom converts the caller into the actor of an audit event: kind, principal
// ID, name, and the credential that proved it (token ID and method). Every
// handler and every Caller of a domain call builds its actor here, so an event
// names the credential whatever area records it (Q-OPS-49).
func ActorFrom(p httpserver.Principal) audit.Actor {
	return audit.Actor{
		Kind: string(p.Kind), ID: p.ID, Name: p.Name,
		TokenID: p.Credential.TokenID, AuthMethod: string(p.Credential.Method),
	}
}

// SubjectFrom converts a request into the subject authorization evaluates: the
// caller the authentication stage stored, and the endpoint whose listener
// accepted the request (R-EP-002), which selects the exposure ceiling of every
// decision on the subject (R-EP-013, Q-EP-20). It is the one conversion between
// the edge types and the domain type; the architecture test refuses a Subject
// built anywhere else, so a decision cannot lose its ceiling by a hand-made
// subject.
func SubjectFrom(r *http.Request) authz.Subject { return SubjectOf(r.Context()) }

// SubjectOf is [SubjectFrom] for code that holds the context of the request and
// not the request itself. The context is the one the endpoint's chain stored the
// caller and the endpoint in; outside a request it yields the anonymous subject
// without an endpoint, which a job or a command may not be.
func SubjectOf(ctx context.Context) authz.Subject {
	p := httpserver.PrincipalFrom(ctx)
	s := authz.Subject{
		Anonymous:  p.Anonymous,
		Kind:       principal.Kind(p.Kind),
		ID:         p.ID,
		Name:       p.Name,
		Mappings:   p.Mappings,
		CI:         p.CI,
		Scope:      p.Credential.Scope,
		TokenID:    p.Credential.TokenID,
		SessionID:  p.Credential.SessionID,
		AuthMethod: string(p.Credential.Method),
		MinEpoch:   p.MinEpoch,
		External:   p.External,
	}
	for _, g := range p.Groups {
		s.Groups = append(s.Groups, principal.GroupRef{ID: g.ID, Name: g.Name})
	}
	if p.Kind == httpserver.KindSystem && p.Credential.Registry != nil && len(p.Credential.Registry.Entries) == 1 {
		// The one repository a scan token names, which the fixed rule of system:vuln-scanner-trivy reads (R-OCI-037, Q-OCI-228).
		s.SystemRepo = p.Credential.Registry.Entries[0].Repo
	}
	if c := p.Credential.ClientCert; p.Kind == httpserver.KindCertificate && c != nil {
		s.CertSerial = c.Serial // the certificate subject's serial, which its audit events name (Q-CLAMAV-12)
	}
	if ep := httpserver.EndpointFrom(ctx); ep != nil {
		// The endpoint's cache-fill policy travels with its name, so the populate
		// check reads one source (R-EP-019, Q-AUTHZ-153).
		s.Endpoint, s.CacheFill = ep.Name(), ep.CacheFill()
	}
	return s
}
