package v1

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/signing/bundle"
	"github.com/guided-traffic/quader/internal/signing/statement"
)

// The tab verify of the client setup (signatures.md R-SIG-055, R-SIG-056, Q-SIG-71): while a signing identity is current, npm's client
// gets npm.verify.signatures, and every client of a format whose artifacts Quader attests gets <client>.verify.attestation, the download of the trusted root and the one cosign
// command that verifies Quader's receipt attestation of an artifact offline (R-SIG-052). An image is verified among its referrers with
// cosign verify-attestation; every other artifact with its bundle, downloaded from the API, and cosign verify-blob-attestation. The
// core writes the snippet, not the format: it is the same for every client of a format, and no format knows the identity.

// The values of the verify snippet the reader replaces (R-API-030): the image and its digest, a file's path and its name.
const (
	verifyImageExample = "myapp"
	verifyDigestRef    = "<digest>"
	verifyPathRef      = "<path>"
	verifyFileRef      = "<file>"
)

// trustedRootFile is the name the snippet saves the trusted root under.
const trustedRootFile = "quader-trusted-root.json"

// verifyIdentity is the URI a verifier names as --certificate-identity: the current identity's first URI, else its first name
// (R-SIG-045); ok is false while no identity is current, signing is off, or its certificate has expired.
func (s *Server) verifyIdentity(ctx context.Context) (string, bool) {
	sv := s.o.Signing
	if sv.Snapshot == nil || !sv.Enabled {
		return "", false
	}
	ids, err := sv.Snapshot.Identities(ctx)
	if err != nil {
		return "", false
	}
	for _, i := range ids {
		if !i.SignsAt(time.Now()) {
			continue
		}
		for _, n := range i.Names {
			if strings.Contains(n, "://") {
				return n, true
			}
		}
		if len(i.Names) > 0 {
			return i.Names[0], true
		}
	}
	return "", false
}

// attests reports whether Quader attests artifacts of the format (formats.SignatureDeclaration.Attests).
func attests(f formats.Format) bool {
	sa := f.Signatures()
	return sa != nil && sa.Declaration().Attests != ""
}

// npmAuditClient is the client whose own command checks npm's registry signatures; pnpm and Yarn have none.
const npmAuditClient = "npm"

// registrySigned reports whether the format's clients check registry signatures of npm's scheme (formats.SignatureSchemeNPMRegistry):
// npm's, which Quader makes for every version of a local repository (signatures.md R-SIG-050).
func registrySigned(f formats.Format) bool {
	sa := f.Signatures()
	if sa == nil {
		return false
	}
	for _, k := range sa.Declaration().Kinds {
		if slices.Contains(k.Schemes, formats.SignatureSchemeNPMRegistry) {
			return true
		}
	}
	return false
}

// verifySnippets are the client's snippets of the tab verify (R-SIG-055): npm.verify.signatures, npm's own check of the registry
// signatures of what a project installed (Quader's of local packages beside the public registry's, Q-SIG-163), where the format's
// clients read them and the client is npm; and <client>.verify.attestation where Quader attests the format's artifacts.
func verifySnippets(f formats.Format, base, identity string, in setupInput) []snippet {
	var out []snippet
	if in.Client == npmAuditClient && registrySigned(f) {
		const content = "npm audit signatures\n"
		out = append(out, snippet{ID: in.Client + ".verify.signatures", Tab: "verify", Language: "shell", Content: content})
	}
	if attests(f) {
		out = append(out, verifySnippet(base, identity, in))
	}
	return out
}

// verifySnippet is the client's snippet of the tab verify: base is the configured base URL, identity the identity's URI, in the input of
// the client setup (the client, the credential mode, the package asked for, the repository).
func verifySnippet(base, identity string, in setupInput) snippet {
	base = strings.TrimSuffix(base, "/")
	trust := []string{
		"  --trusted-root " + trustedRootFile + " \\",
		"  --certificate-identity " + shellQuote(identity) + " \\",
		"  --certificate-oidc-issuer-regexp '.*' \\",
		"  --insecure-ignore-tlog --insecure-ignore-sct \\",
		"  --type " + shellQuote(statement.PredicateType) + " \\",
	}
	lines := []string{"curl -fsSo " + trustedRootFile + " " + shellQuote(base+"/api/v1/signing/trusted-root.json")}
	var values []string
	if in.Data.ImagePrefix != "" {
		image := verifyImageExample
		if in.Package != "" {
			image = in.Package
		}
		values = append(values, image, verifyDigestRef)
		lines = append(lines, "cosign verify-attestation \\")
		lines = append(lines, trust...)
		lines = append(lines, "  "+in.Data.ImagePrefix+"/"+image+"@sha256:"+verifyDigestRef)
	} else {
		values = append(values, verifyPathRef, verifyFileRef)
		route := base + "/api/v1/projects/" + url.PathEscape(in.Data.Project) + "/repositories/" + url.PathEscape(in.Data.Key) + "/attestation?path="
		// The token reaches curl as configuration on standard input, never as an argument (Q-GENERIC-9, Q-UI-1336): printf is a
		// builtin of the shell, so the process list shows no token.
		fetch := "curl -fsS -o " + verifyFileRef + bundle.FileSuffix + " '" + route + verifyPathRef + "'"
		if in.Credentials != credNone {
			fetch = `printf 'header = "Authorization: Bearer %s"\n' "` + formats.SetupTokenRef + `" | curl -fsS --config - -o ` +
				verifyFileRef + bundle.FileSuffix + " '" + route + verifyPathRef + "'"
		}
		lines = append(lines,
			fetch,
			"cosign verify-blob-attestation \\",
			"  --bundle "+verifyFileRef+bundle.FileSuffix+" \\",
		)
		lines = append(lines, trust...)
		lines = append(lines, "  "+verifyFileRef)
	}
	content := applyCredentials(strings.Join(lines, "\n")+"\n", in)
	t := formats.SetupTemplate{ID: in.Client + ".verify.attestation", Client: in.Client, Section: "verify", Lang: "shell", Examples: values}
	return snippet{
		ID: t.ID, Tab: "verify", Language: "shell", Content: content, Marks: snippetMarks(content, t, in),
	}
}
