package v1

import (
	"context"
	"strings"

	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/store"
)

// The names of the limits and usage answers (R-LIM-032, R-LIM-034). The store keeps IDs; an answer collects the references it
// names first and reads their names in one transaction, so a list of many callers costs one read of names, not one per row.

// nameRefs collects the references of one answer.
type nameRefs struct{ r store.NameRefs }

// subject adds a subject of the usage store.
func (n *nameRefs) subject(kind limits.SubjectKind, ref string) {
	switch kind {
	case limits.KindUser:
		n.r.Users = append(n.r.Users, ref)
	case limits.KindTechnicalToken:
		n.r.Tokens = append(n.r.Tokens, ref)
	case limits.KindCI:
		n.r.CI = append(n.r.CI, ref)
	}
}

// rule adds the "who" of a rule.
func (n *nameRefs) rule(r limits.Rule) {
	switch r.WhoKind {
	case limits.WhoUser:
		n.r.Users = append(n.r.Users, r.WhoRef)
	case limits.WhoTechnicalToken:
		n.r.Tokens = append(n.r.Tokens, r.WhoRef)
	case limits.WhoCIProject:
		n.r.CI = append(n.r.CI, r.WhoRef)
	}
}

// names reads the collected names; without a store every map is empty and the answers name IDs.
func (s *Server) names(ctx context.Context, l LimitsServices, n nameRefs) (store.Names, error) {
	if l.Store == nil {
		return store.Names{}, nil
	}
	return l.Store.Names(ctx, n.r)
}

// ciNames is the issuer's name and the project path of a CI project's reference <issuer ID>:<project ID>; a project whose
// issuances are gone is named through the issuer alone.
func (s *Server) ciNames(ctx context.Context, l LimitsServices, n store.Names, ref string) (issuer, project string, path any) {
	issuer, project, _ = strings.Cut(ref, ":")
	if c, ok := n.CI[ref]; ok {
		if c.ProjectPath != "" {
			path = c.ProjectPath
		}
		return c.Issuer, project, path
	}
	if l.CI != nil {
		if name, ok, err := l.CI.IssuerName(ctx, issuer); err == nil && ok {
			issuer = name
		}
	}
	return issuer, project, nil
}

// subjectRep renders a subject with its names (R-LIM-034). A user or a technical token that no longer exists keeps its ID as
// its name and is marked deleted.
func (s *Server) subjectRep(ctx context.Context, l LimitsServices, n store.Names, kind limits.SubjectKind, ref string) map[string]any {
	out := map[string]any{"kind": kind.APIName(), "id": ref}
	switch kind {
	case limits.KindUser:
		out["name"], out["displayName"], out["deleted"] = ref, nil, true
		if u, ok := n.Users[ref]; ok {
			out["id"], out["name"], out["deleted"] = u.Name, u.Name, false
			if u.DisplayName != "" {
				out["displayName"] = u.DisplayName
			}
		}
	case limits.KindTechnicalToken:
		out["name"], out["account"], out["level"], out["deleted"] = ref, "", nil, true
		if t, ok := n.Tokens[ref]; ok {
			out["name"], out["account"], out["level"], out["deleted"] = t.Name, "sa:"+t.Account, t.Level(), false
		}
	case limits.KindCI:
		issuer, project, path := s.ciNames(ctx, l, n, ref)
		out["id"], out["issuer"], out["projectId"], out["projectPath"] = issuer+":"+project, issuer, project, path
	case limits.KindAnonymous:
		out["address"] = ref
	case limits.KindSystem:
		out["id"] = "system"
	}
	return out
}

// tokenSubject is a suggestion of a technical token (R-LIM-034).
func tokenSubject(t store.TokenName) map[string]any {
	return map[string]any{"kind": "technicalToken", "id": t.ID, "name": t.Name, "account": "sa:" + t.Account, "level": t.Level(), "deleted": false}
}

// ciSubject is a suggestion of a CI project.
func ciSubject(c store.CIProject) map[string]any {
	var path any
	if c.ProjectPath != "" {
		path = c.ProjectPath
	}
	return map[string]any{"kind": "ci", "id": c.Issuer + ":" + c.ProjectID, "issuer": c.Issuer, "projectId": c.ProjectID, "projectPath": path}
}
