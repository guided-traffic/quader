package repository

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// TestR_REPO_077_TheStateOfADeclaredAccountsFilesNamesTheCauseAndNoValue: the page's files state is readable, missing, empty or invalid,
// the cause an operator fixes first winning; the errors name the path and never the content. Unit tier, an in-memory reader.
func TestR_REPO_077_TheStateOfADeclaredAccountsFilesNamesTheCauseAndNoValue(t *testing.T) {
	files := map[string]string{}
	read := func(p string) ([]byte, error) {
		v, ok := files[p]
		if !ok {
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		}
		if v == "denied" {
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrPermission}
		}
		return []byte(v), nil
	}
	basic := ConfiguredAccount{AuthMode: AuthBasic, UsernameFile: "/s/username", PasswordFile: "/s/password"}
	bearer := ConfiguredAccount{AuthMode: AuthBearer, PasswordFile: "/s/token"}
	for _, tc := range []struct {
		name  string
		files map[string]string
		acc   ConfiguredAccount
		want  string
	}{
		{"both files hold a value", map[string]string{"/s/username": "u-9a\n", "/s/password": "p-9a\n"}, basic, AccountFilesReadable},
		{"bearer needs the token alone", map[string]string{"/s/token": "t-9a"}, bearer, AccountFilesReadable},
		{"no file", map[string]string{}, basic, AccountFilesMissing},
		{"a missing file wins over an empty one", map[string]string{"/s/username": "\n"}, basic, AccountFilesMissing},
		{"an empty password", map[string]string{"/s/username": "u-9a", "/s/password": "\r\n"}, basic, AccountFilesEmpty},
		{"a colon in the username", map[string]string{"/s/username": "u:9a", "/s/password": "p-9a"}, basic, AccountFilesInvalid},
		{"a colon in a password is a value", map[string]string{"/s/username": "u-9a", "/s/password": "p:9a"}, basic, AccountFilesReadable},
		{"a line break inside", map[string]string{"/s/username": "u-9a", "/s/password": "p\n9a"}, basic, AccountFilesInvalid},
		{"a file larger than 8 KiB", map[string]string{"/s/token": strings.Repeat("x", MaxSecretFileBytes)}, bearer, AccountFilesInvalid},
		{"a file that cannot be read", map[string]string{"/s/token": "denied"}, bearer, AccountFilesInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files = tc.files
			if got := tc.acc.FilesState(read); got != tc.want {
				t.Fatalf("state %q, want %q", got, tc.want)
			}
		})
	}
	files = map[string]string{"/s/username": "u:9a-secret"}
	_, err := AccountFileValue(read, "/s/username", true)
	if err == nil || !strings.Contains(err.Error(), "/s/username") || strings.Contains(err.Error(), "9a-secret") {
		t.Fatalf("err = %v", err)
	}
	if _, err := AccountFileValue(read, "/s/none", false); !errors.Is(err, errAccountFileMissing) {
		t.Fatalf("missing: %v", err)
	}
}

// TestR_REPO_077_AnAccountRaisesTheLimitOfAPresetThatSaysSoAndSendsNone: the limit's sentence offers an account only for the preset that
// says an account allows more (Docker Hub, Q-UI-1160) and only while the repository sends no credential; an address of no preset never.
func TestR_REPO_077_AnAccountRaisesTheLimitOfAPresetThatSaysSoAndSendsNone(t *testing.T) {
	r := catalog.NewRegistry()
	r.Register(catalog.Entry{Facts: catalog.Facts{
		ID: "ocilike", Classes: []Class{Remote}, Layout: "ocilike", ProtocolBasePath: "/v2/{project}/{key}/", PatternSubject: SubjectName,
		UpstreamPresets: []catalog.UpstreamPreset{
			{ID: "hub", DisplayName: "Hub", URL: "https://registry.hub.example/", Default: true, AccountRaisesLimit: true},
			{ID: "other", DisplayName: "Other", URL: "https://other.example/"},
		},
	}})
	for _, tc := range []struct {
		url   string
		sends bool
		want  bool
	}{
		{"https://registry.hub.example/", false, true},
		{"https://registry.hub.example/", true, false},
		{"https://other.example/", false, false},
		{"https://mirror.example/", false, false},
	} {
		if got := accountRaisesLimit(r, "ocilike", tc.url, tc.sends); got != tc.want {
			t.Errorf("%s sends %v: %v, want %v", tc.url, tc.sends, got, tc.want)
		}
	}
}
