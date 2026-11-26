package ansible

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// What a remote keeps of the roles it reads (R-ANS-056, Q-ANS-110): the facts of the role documents of a lookup, written in the
// transaction that stores the lookup, and the README of the role's content document, written in the transaction that stores that
// document. The package of a role stays "role/<id>" (Q-ANS-36): the facts give it its display name, "<owner>.<name>", and its page, and
// decide nothing a pattern, a rule or a grant decides. Everything the upstream sends is untrusted: text is cut to the column's bound, a
// list to its count, and the README is HTML the host sanitizes on every serving (R-ANS-039).

// RoleContentDocument is the file name of the stored content document of a role, roles/<id>/content.json (variant "doc").
const RoleContentDocument = "content" + jsonSuffix

// RoleContentDocumentPath is the canonical path of the stored content document of a role, whose readme_html is its README.
func RoleContentDocumentPath(id string) string {
	return dirRoles + "/" + id + "/" + RoleContentDocument
}

// The bounds of a role's facts (the CHECKs of migration ansible_roles).
const (
	maxRoleDescription = 4096
	maxRoleGitHubWord  = 256
	maxRoleCommit      = 64
	maxRoleTags        = 64
	maxRoleTag         = 64
	maxRoleDeps        = 64
	maxRoleDep         = 129
	maxRoleFile        = 256
)

// upstreamRole is the part of a role document of the v1 API that a remote keeps (the lookup of the public Galaxy, recorded in
// the spec's table of client behaviour). The owner is "username"; a Galaxy that sends none names it in summary_fields.namespace.
type upstreamRole struct {
	ID            json.Number `json:"id"`
	Name          string      `json:"name"`
	Username      string      `json:"username"`
	Description   string      `json:"description"`
	GitHubUser    string      `json:"github_user"`
	GitHubRepo    string      `json:"github_repo"`
	GitHubBranch  string      `json:"github_branch"`
	Commit        string      `json:"commit"`
	Imported      string      `json:"imported"`
	DownloadCount *int64      `json:"download_count"`
	SummaryFields struct {
		Namespace struct {
			Name string `json:"name"`
		} `json:"namespace"`
		Tags         []json.RawMessage `json:"tags"`
		Dependencies []json.RawMessage `json:"dependencies"`
	} `json:"summary_fields"`
}

// parseRoleFacts reads the roles of a lookup document the upstream sent; a role without a usable id, owner or name is left out, as the
// rendering of the lookup leaves a role without an id out.
func parseRoleFacts(doc []byte) []rows.Role {
	var page struct {
		Results []json.RawMessage `json:"results"`
	}
	if len(doc) > MaxDocumentBytes || json.Unmarshal(doc, &page) != nil {
		return nil
	}
	var out []rows.Role
	for _, raw := range page.Results {
		if r, ok := roleFacts(raw); ok {
			out = append(out, r)
		}
	}
	return out
}

func roleFacts(raw json.RawMessage) (rows.Role, bool) {
	var u upstreamRole
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if dec.Decode(&u) != nil {
		return rows.Role{}, false
	}
	id, err := strconv.ParseInt(u.ID.String(), 10, 64)
	if err != nil || id < 1 || !digits(u.ID.String(), maxRoleIDDigits) {
		return rows.Role{}, false
	}
	owner := u.Username
	if owner == "" {
		owner = u.SummaryFields.Namespace.Name
	}
	if !roleWordOK(owner) || !roleWordOK(u.Name) {
		return rows.Role{}, false
	}
	r := rows.Role{
		ID: id, Owner: owner, Name: u.Name, Description: cutText(strings.TrimSpace(u.Description), maxRoleDescription),
		GitHubUser: bounded(u.GitHubUser, maxRoleGitHubWord), GitHubRepo: bounded(u.GitHubRepo, maxRoleGitHubWord),
		GitHubBranch: bounded(u.GitHubBranch, maxRoleGitHubWord), Commit: bounded(u.Commit, maxRoleCommit),
		Imported: parseTime(u.Imported), Downloads: -1,
		Tags:         roleWords(u.SummaryFields.Tags, maxRoleTags, maxRoleTag),
		Dependencies: roleWords(u.SummaryFields.Dependencies, maxRoleDeps, maxRoleDep),
	}
	if u.DownloadCount != nil && *u.DownloadCount >= 0 {
		r.Downloads = *u.DownloadCount
	}
	return r, true
}

// roleWords reads a list of strings (tags, dependencies): a member that is no string is read by its name member (a dependency the
// upstream sends as an object), anything else is left out; an empty or too long word too, and at most max words are kept.
func roleWords(list []json.RawMessage, maxWords, maxLen int) []string {
	out := []string{}
	for _, raw := range list {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			var o struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(raw, &o) != nil {
				continue
			}
			s = o.Name
		}
		s = strings.TrimSpace(s)
		if s == "" || len(s) > maxLen || !utf8.ValidString(s) || strings.ContainsFunc(s, isControl) {
			continue
		}
		out = append(out, s)
		if len(out) == maxWords {
			break
		}
	}
	return out
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// bounded is s when it is at most max bytes of valid text without control characters, "" otherwise.
func bounded(s string, max int) string {
	if len(s) > max || !utf8.ValidString(s) || strings.ContainsFunc(s, isControl) {
		return ""
	}
	return s
}

// cutText cuts s to at most max characters and drops what is no valid text.
func cutText(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// roleContent is the content document of a role (`v1/roles/<id>/content/`): the README's file name and the HTML the upstream rendered.
type roleContent struct {
	File, HTML string
}

// parseRoleContent reads a role's content document. A document that is no object is invalid; one without readme_html is a role without
// a README; one whose HTML is larger than rows.MaxRoleReadmeBytes is too large (R-ANS-056).
func parseRoleContent(doc []byte) (roleContent, error) {
	var c struct {
		Readme     *string `json:"readme"`
		ReadmeHTML *string `json:"readme_html"`
	}
	if len(doc) > MaxDocumentBytes || json.Unmarshal(doc, &c) != nil {
		return roleContent{}, invalidDocument("a role's content", "it is not a JSON object")
	}
	var out roleContent
	if c.Readme != nil {
		out.File = bounded(*c.Readme, maxRoleFile)
	}
	if c.ReadmeHTML != nil {
		if len(*c.ReadmeHTML) > rows.MaxRoleReadmeBytes {
			return roleContent{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamItemTooLarge,
				"The upstream's README of the role is larger than "+strconv.Itoa(rows.MaxRoleReadmeBytes)+" bytes.")
		}
		out.HTML = strings.ToValidUTF8(*c.ReadmeHTML, "�")
	}
	return out, nil
}

// onRoleLookup records the facts of the roles of a lookup in the transaction that stores it (R-ANS-056): the rows, the display name of a
// role's package that exists (a version of it was cached before its facts came), and the fetch of the README of such a role when it has
// none for the role's commit.
func (a remoteAdapter) onRoleLookup(ctx context.Context, tx formats.Tx, ev formats.FillEvent, data fillData) error {
	if len(data.roles) == 0 {
		return nil
	}
	st, err := storeOf(ev.Data)
	if err != nil {
		return err
	}
	if err := st.PutRoles(ctx, tx, ev.Repo.ID, data.roles); err != nil {
		return err
	}
	for _, r := range data.roles {
		id := strconv.FormatInt(r.ID, 10)
		if _, err := ev.Packages.SetDisplayName(ctx, ev.Repo, RolePackage(id), r.Owner+"."+r.Name); err != nil {
			return err
		}
		if err := queueRoleReadme(ctx, tx, st, ev.Repo.ID, r.ID, data.fetchDocs); err != nil {
			return err
		}
	}
	return nil
}

// queueRoleReadme queues the fetch of a role's README when the role has a page (its package exists), documentation is fetched, and the
// README was never asked for or was fetched for another commit; a queued fetch is not queued again.
func queueRoleReadme(ctx context.Context, tx formats.Tx, st Store, repoID, id int64, fetchDocs bool) error {
	if !fetchDocs {
		return nil
	}
	r, found, err := st.Role(ctx, tx, repoID, id)
	if err != nil || !found || !r.Packaged {
		return err
	}
	switch r.Readme.State {
	case rows.RoleReadmeQueued:
		return nil
	case rows.RoleReadmeNone:
	default:
		if r.Readme.Commit == r.Commit {
			return nil
		}
	}
	return st.EnqueueRoleReadme(ctx, tx, repoID, id)
}

// onRoleContent records the README of a role's content document in the transaction that stores it.
func onRoleContent(ctx context.Context, tx formats.Tx, ev formats.FillEvent, data fillData) error {
	if data.content == nil {
		return nil
	}
	st, err := storeOf(ev.Data)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(data.pkg, RolePrefix), 10, 64)
	if err != nil {
		return errs.E(errs.Internal, errs.CodeInternal, "The content document of a role names no role id.", errs.WithCause(err))
	}
	r := rows.RoleReadme{State: rows.RoleReadmeAbsent}
	if data.content.HTML != "" {
		r = rows.RoleReadme{State: rows.RoleReadmeAvailable, File: data.content.File, HTML: data.content.HTML}
	}
	if err := st.SetRoleReadme(ctx, tx, ev.Repo.ID, id, r); err != nil {
		return err
	}
	return ev.Packages.Notify(catalog.MetadataChangedArea, catalog.PackageChanged(ev.Repo.ID, data.pkg))
}

// githubWord is a GitHub account or repository name: what a repository address is built from.
var githubWord = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// roleRepositoryURL is the address of the role's GitHub repository, built from github_user and github_repo, never taken from the
// upstream as a URL; "" when either is no GitHub name.
func roleRepositoryURL(user, repo string) string {
	if !githubWord.MatchString(user) || !githubWord.MatchString(repo) || strings.Contains(user, "..") || strings.Contains(repo, "..") {
		return ""
	}
	return "https://github.com/" + user + "/" + repo
}

// describeRole is what the screens show of a version of a role (R-ANS-056): the description and the tags in the generic fields, the
// README as the upstream's HTML, and the facts of the role under the member "role", with the GitHub repository among the links of every
// format (repositoryUrl) and the package name rules and grants see.
func describeRole(v rows.RoleView) formats.ViewData {
	role := map[string]any{
		"id": v.ID, "owner": v.Owner, "name": v.Name, "packageName": v.Package, "readme": v.Readme.State,
	}
	if v.Readme.Reason != "" {
		role["readmeReason"] = v.Readme.Reason
	}
	if v.Downloads >= 0 {
		role["upstreamDownloads"] = v.Downloads
	}
	if v.GitHubBranch != "" {
		role["branch"] = v.GitHubBranch
	}
	if !v.Imported.IsZero() {
		role["imported"] = v.Imported
	}
	if len(v.Dependencies) > 0 {
		role["dependencies"] = v.Dependencies
	}
	md := map[string]any{"role": role}
	if u := roleRepositoryURL(v.GitHubUser, v.GitHubRepo); u != "" {
		md["repositoryUrl"] = u
	}
	return formats.ViewData{
		Description: v.Description, Keywords: v.Tags, HasReadme: v.Readme.State == rows.RoleReadmeAvailable, Metadata: md,
		Readme: v.Readme.HTML, ReadmeFile: v.Readme.File, ReadmeHTML: true,
	}
}

// isRole reports whether a package name is a role's (Q-ANS-36).
func isRole(name string) bool { return strings.HasPrefix(name, RolePrefix) }
