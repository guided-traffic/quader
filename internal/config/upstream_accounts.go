package config

import (
	"path"
	"regexp"
)

// The authentication of an account the configuration declares for a repository of the built-in project upstream (R-REPO-077): basic
// sends a username and a password or access token, bearer a token alone. none is not an account and is not accepted here.
const (
	UpstreamAccountBasic  = "basic"
	UpstreamAccountBearer = "bearer"
)

// maxUpstreamAccountKey bounds the key of an entry: the key of a repository of the project upstream (R-REPO-001).
const maxUpstreamAccountKey = 64

// upstreamAccountKeyPattern is the rule of a repository key of the project upstream, the presets' ids and the system repositories.
var upstreamAccountKeyPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// UpstreamAccount is one entry of repositories.upstream_accounts (R-REPO-077): an account of a repository of the project upstream whose
// username and password are files. The files are read at every use and never at load, so a Secret that does not exist yet stops no
// role: that repository's fetches answer 502 upstream_secret_unavailable until it does (Q-REPO-485). An element of a map is decoded as it
// is written, so a key left out is the zero value: an empty auth_mode is basic.
type UpstreamAccount struct {
	AuthMode     string `yaml:"auth_mode" json:"auth_mode,omitempty"`
	UsernameFile string `yaml:"username_file" json:"username_file,omitempty"`
	PasswordFile string `yaml:"password_file" json:"password_file"`
	// SecretName is the Kubernetes Secret the chart mounts the files from; the admin page names it. Empty outside the chart.
	SecretName string `yaml:"secret_name" json:"secret_name,omitempty"`
}

// EffectiveAuthMode is auth_mode with its default: basic.
func (a UpstreamAccount) EffectiveAuthMode() string {
	if a.AuthMode == "" {
		return UpstreamAccountBasic
	}
	return a.AuthMode
}

// Validate checks the cross-field rules of the section: the form of every declared upstream account (R-REPO-077). It never reads a
// file, and it cannot know the catalog: whether the key names a repository of the project upstream is checked by quader migrate, which
// fails with the key named.
func (r *Repositories) Validate(rep *Report) {
	for _, key := range sortedMapKeys(r.UpstreamAccounts) {
		r.UpstreamAccounts[key].validate(rep, key)
	}
}

func (a UpstreamAccount) validate(r *Report, key string) {
	at := "upstream_accounts." + key
	if len(key) > maxUpstreamAccountKey || !upstreamAccountKeyPattern.MatchString(key) {
		r.Add(at, "the key must match ^[a-z0-9]+(-[a-z0-9]+)*$ and have at most %d characters: it is the key of a repository of the project upstream (R-REPO-077)", maxUpstreamAccountKey)
	}
	switch a.EffectiveAuthMode() {
	case UpstreamAccountBasic:
		if a.UsernameFile == "" {
			r.Add(at+".username_file", "is required with auth_mode basic: the file that holds the account's username")
		}
	case UpstreamAccountBearer:
		if a.UsernameFile != "" {
			r.Add(at+".username_file", "must not be set with auth_mode bearer: a bearer token is sent without a username")
		}
	default:
		r.Add(at+".auth_mode", "%q is neither basic nor bearer (R-REPO-077)", a.AuthMode)
	}
	if a.PasswordFile == "" {
		r.Add(at+".password_file", "is required: the file that holds the password or the token")
	}
	for _, f := range [][2]string{{"username_file", a.UsernameFile}, {"password_file", a.PasswordFile}} {
		if f[1] != "" && (!path.IsAbs(f[1]) || path.Clean(f[1]) != f[1]) {
			r.Add(at+"."+f[0], "%q must be a clean absolute path", f[1])
		}
	}
}
