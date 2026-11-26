package v1

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm"
	"github.com/guided-traffic/quader/internal/formats/nuget"
)

// TestR_SIG_055_TheVerifySnippetIsTheOneCosignCommandOfTheFormat: an image's snippet downloads the trusted root and runs cosign
// verify-attestation with the identity's URI, the flags of R-SIG-052 and the predicate type against the image by digest, marking the
// example image and the digest; a file's snippet downloads its bundle from the API, with the token's reference by the credential mode
// (none drops the header, not the command), and runs cosign verify-blob-attestation. Unit, microseconds, no server.
func TestR_SIG_055_TheVerifySnippetIsTheOneCosignCommandOfTheFormat(t *testing.T) {
	const base, identity = "https://quader.example.com/", "https://quader.example.com"
	image := verifySnippet(base, identity, setupInput{
		Client: "docker", Credentials: credPlaceholder, Data: formats.SetupData{ImagePrefix: "quader.example.com/web/docker", Project: "web", Key: "docker"},
	})
	for _, want := range []string{
		"curl -fsSo quader-trusted-root.json https://quader.example.com/api/v1/signing/trusted-root.json",
		"cosign verify-attestation", "--certificate-identity https://quader.example.com", "--certificate-oidc-issuer-regexp '.*'",
		"--insecure-ignore-tlog --insecure-ignore-sct", "receipt-attestation-v1", "quader.example.com/web/docker/myapp@sha256:<digest>",
	} {
		if !strings.Contains(image.Content, want) {
			t.Errorf("the image's snippet lacks %q:\n%s", want, image.Content)
		}
	}
	if image.ID != "docker.verify.attestation" || image.Tab != "verify" || len(image.Marks) != 2 || image.Marks[0].Text != "myapp" {
		t.Errorf("snippet %s %s marks %+v", image.ID, image.Tab, image.Marks)
	}
	file := verifySnippet(base, identity, setupInput{Client: "curl", Credentials: credPlaceholder, Data: formats.SetupData{Project: "web", Key: "files"}})
	for _, want := range []string{
		// The token reaches curl on standard input, never as an argument (Q-UI-1336).
		`printf 'header = "Authorization: Bearer %s"\n' "{{token}}" | curl -fsS --config - -o`, "/api/v1/projects/web/repositories/files/attestation?path=<path>",
		"cosign verify-blob-attestation", "--bundle <file>.quader.sigstore.json",
	} {
		if !strings.Contains(file.Content, want) {
			t.Errorf("the file's snippet lacks %q:\n%s", want, file.Content)
		}
	}
	if file.Marks[0].Kind != markToken {
		t.Errorf("marks %+v", file.Marks)
	}
	anon := verifySnippet(base, identity, setupInput{Client: "curl", Credentials: credNone, Data: formats.SetupData{Project: "web", Key: "files"}})
	if strings.Contains(anon.Content, "Authorization") || !strings.Contains(anon.Content, "curl -fsS -o <file>.quader.sigstore.json") {
		t.Errorf("credentials=none:\n%s", anon.Content)
	}
}

// TestR_SIG_055_NPMsClientGetsItsOwnSignatureCheckBesideTheAttestation: npm's client gets npm.verify.signatures (npm audit signatures,
// Q-SIG-163) before its attestation's snippet; pnpm and Yarn, which have no such command, only the attestation's; a format without npm's
// registry signatures none of it. Unit, microseconds, no server.
func TestR_SIG_055_NPMsClientGetsItsOwnSignatureCheckBesideTheAttestation(t *testing.T) {
	const base, identity = "https://quader.example.com/", "https://quader.example.com"
	in := setupInput{Client: "npm", Credentials: credPlaceholder, Data: formats.SetupData{Project: "web", Key: "npm-local"}}
	ids := func(ss []snippet) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return out
	}
	got := verifySnippets(&npm.Format{}, base, identity, in)
	if strings.Join(ids(got), " ") != "npm.verify.signatures npm.verify.attestation" || got[0].Content != "npm audit signatures\n" ||
		got[0].Tab != "verify" || got[0].Language != "shell" {
		t.Errorf("npm: %+v", got)
	}
	in.Client = "pnpm"
	if got := ids(verifySnippets(&npm.Format{}, base, identity, in)); strings.Join(got, " ") != "pnpm.verify.attestation" {
		t.Errorf("pnpm: %v", got)
	}
	in.Client = "npm"
	if got := ids(verifySnippets(&nuget.Format{}, base, identity, in)); strings.Join(got, " ") != "npm.verify.attestation" {
		t.Errorf("a format without npm's registry signatures: %v", got)
	}
}
