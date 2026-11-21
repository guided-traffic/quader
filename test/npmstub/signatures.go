package npmstub

import (
	"net/http"
	"net/url"
	"strings"
)

// The endpoints of npm's signatures (signatures.md R-SIG-028): the registry's keys at /-/npm/v1/keys and a version's attestations at
// /-/npm/v1/attestations/<name>@<version>. A test sets what they answer; an endpoint without an answer is 404, as a registry without keys
// answers. Their requests are counted as the kinds keys and attestations.

// The kinds of the two endpoints.
const (
	KindKeys         = "keys"
	KindAttestations = "attestations"
)

const (
	keysPath         = "/-/npm/v1/keys"
	attestationsPath = "/-/npm/v1/attestations/"
)

// signatureRoute classifies the two endpoints; ok is false for any other path.
func signatureRoute(escapedPath string) (route, bool) {
	if escapedPath == keysPath {
		return route{kind: KindKeys}, true
	}
	rest, ok := strings.CutPrefix(escapedPath, attestationsPath)
	if !ok {
		return route{}, false
	}
	spec, err := url.PathUnescape(rest)
	if err != nil || spec == "" {
		return route{kind: KindOther}, true
	}
	return route{kind: KindAttestations, name: spec}, true
}

// SetKeys sets the keys document the stub answers; nil answers 404.
func (s *Server) SetKeys(doc []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = doc
}

// SetAttestations sets the attestations document of "<name>@<version>"; nil removes it.
func (s *Server) SetAttestations(spec string, doc []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attestations == nil {
		s.attestations = map[string][]byte{}
	}
	if doc == nil {
		delete(s.attestations, spec)
		return
	}
	s.attestations[spec] = doc
}

// Signatures counts the requests of the keys (an empty spec) or of a version's attestations.
func (c Counts) Signatures(kind, spec string) int { return c.count(kind, spec, "") }

func (s *Server) serveSignatures(w http.ResponseWriter, rt route) {
	s.mu.Lock()
	var doc []byte
	if rt.kind == KindKeys {
		doc = s.keys
	} else {
		doc = s.attestations[rt.name]
	}
	s.mu.Unlock()
	if doc == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
