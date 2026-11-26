package v1

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/guided-traffic/quader/internal/formats"
)

// The client setup snippets of R-API-030. A format contributes templates
// ([formats.SetupTemplate]); this file runs them with the data of the repository and
// the request, rewrites the credential by the mode, substitutes the package and
// quotes whatever it substitutes. The server never sees a token: the credential is
// the literal placeholder, a reference to an environment variable, or absent.

// Credential modes of the client setup (R-API-030).
const (
	credPlaceholder = "placeholder"
	credEnv         = "env"
	credNone        = "none"

	// tokenPlaceholder is what credentials=placeholder puts where the token goes;
	// the browser replaces it (R-UI-016).
	tokenPlaceholder = "{{token}}"
)

// setupInput is everything a snippet is made of.
type setupInput struct {
	// Templates are the format's templates; Client selects the ones of one client.
	Templates []formats.SetupTemplate
	Client    string
	// Credentials is credPlaceholder, credEnv or credNone.
	Credentials string
	// TokenEnvVar is the variable credentials=env refers to.
	TokenEnvVar string
	// Data is what the templates see. RequiresAuth is cleared for credNone, because a
	// snippet without a credential has nothing for an always-auth switch to send.
	Data formats.SetupData
	// Package and Version name what an install snippet installs; Package empty keeps
	// the placeholder. DisplayName is the package's display name, which an install
	// template with a Packages prefix writes in place of Package (R-ANS-056).
	Package, Version, DisplayName string
	// Scope is the scope of the scoped configure variant, without its "@"; empty for
	// none.
	Scope string
	// MayPublish says that the caller may deploy to the target; publish templates are
	// rendered only then.
	MayPublish bool
	// File is the file or the folder of a format whose items are paths that the install and publish snippets are for
	// (formats/generic.md R-GENERIC-017); nil keeps the placeholders.
	File *fileTarget
}

// fileTarget is the path a client setup names for a format whose items are paths (R-GENERIC-017): a file, whose install
// snippets download it under its name and check it against its SHA-256, or a folder, whose publish snippets upload into it.
type fileTarget struct {
	// Path is the canonical path (repository.CleanPath), "" for the root.
	Path string
	// Folder marks a folder: it has publish snippets only.
	Folder bool
	// SHA256 is the file's SHA-256 in lower-case hexadecimal; "" keeps the placeholder.
	SHA256 string
}

// snippet is one rendered snippet.
type snippet struct {
	ID       string
	Tab      string
	FileName string
	Language string
	Content  string
	// Marks are the parts of Content that are the reader's (R-API-030): the
	// reference to the token, and the placeholders and examples the reader replaces.
	Marks []snippetMark
}

// The kinds of a mark (R-API-030).
const (
	// markToken is the reference to the credential, or the placeholder the browser
	// fills: the place the token goes.
	markToken = "token"
	// markValue is a part the reader replaces with their own value: a placeholder
	// such as <package>, or an example such as myapp:1.0.
	markValue = "value"
)

// snippetMark is one part of a snippet that is the reader's, by its literal text: a
// screen marks every place the text stands in the snippet.
type snippetMark struct {
	Text string
	Kind string
}

// setupClients returns the clients the templates serve, sorted.
func setupClients(ts []formats.SetupTemplate) []string {
	var out []string
	for _, t := range ts {
		if !slices.Contains(out, t.Client) {
			out = append(out, t.Client)
		}
	}
	sort.Strings(out)
	return out
}

// snippetID derives the stable id of the API from a template: a template ID is
// "<client>.<name>.<section>" or "<client>.<section>", the snippet id is
// "<client>.<section>[.<name>]" (Q-API-160), with "-scoped" appended to the name of
// the scoped variant of a configure snippet.
func snippetID(t formats.SetupTemplate, scoped bool) string {
	parts := strings.Split(t.ID, ".")
	id := t.Client + "." + t.Section
	if len(parts) >= 3 {
		name := strings.Join(parts[1:len(parts)-1], ".")
		if scoped {
			name += "-scoped"
		}
		id += "." + name
	} else if scoped {
		id += ".scoped"
	}
	return id
}

// renderSnippets runs the templates of one client. Install snippets are kept for
// every caller, configure snippets too, publish snippets only for a caller who may
// publish. With a scope the configure snippets that have a scoped variant come
// twice: the default registry and the scoped one (Q-API-235); a configure snippet
// that does not read the variant is rendered once.
func renderSnippets(in setupInput) ([]snippet, error) {
	data := in.Data
	if in.Credentials == credNone {
		data.RequiresAuth = false
	}
	var out []snippet
	claimed := claimingPrefix(in.Templates, in.Client, in.Package)
	for _, t := range in.Templates {
		if t.Client != in.Client {
			continue
		}
		if t.Section == "publish" && !in.MayPublish {
			continue
		}
		if t.Section == "install" && t.Packages != claimed {
			continue // an install template for other packages (formats.SetupTemplate.Packages)
		}
		variants := []string{""}
		if in.Scope != "" && hasScopedVariant(t) {
			variants = append(variants, in.Scope)
		}
		for _, scope := range variants {
			d := data
			d.Variant = scope
			content, err := runTemplate(t, d)
			if err != nil {
				return nil, fmt.Errorf("setup template %s: %w", t.ID, err)
			}
			content = applyCredentials(content, in)
			if in.File != nil {
				if t.Section == "install" && in.File.Folder {
					continue // a folder is uploaded into; nothing downloads it
				}
				content = substituteFile(content, t, *in.File)
			}
			if t.Section == "install" {
				name := in.Package
				if t.Packages != "" {
					// The name the client knows the package by; without one the template's example stands (a role whose owner and
					// name the remote has not read yet, R-ANS-056).
					name = in.DisplayName
				}
				content = substitutePackage(content, t, name, in.Version)
			}
			s := snippet{ID: snippetID(t, scope != ""), Tab: t.Section, FileName: t.File, Language: t.Lang, Content: content}
			s.Marks = snippetMarks(content, t, in)
			out = append(out, s)
		}
	}
	return out, nil
}

// claimingPrefix is the Packages prefix of the client's install templates that claims the package, "" when none does (or no package is
// named): the install templates whose Packages equals it are the ones rendered.
func claimingPrefix(templates []formats.SetupTemplate, client, pkg string) string {
	if pkg == "" {
		return ""
	}
	for _, t := range templates {
		if t.Client == client && t.Section == "install" && t.Packages != "" && strings.HasPrefix(pkg, t.Packages) {
			return t.Packages
		}
	}
	return ""
}

// runTemplate executes a template over the data with the quoting functions. A
// reference to a field the data does not have fails, so a template cannot reach for a
// credential (kit check 8).
func runTemplate(t formats.SetupTemplate, d formats.SetupData) (string, error) {
	tm, err := parseSetupTemplate(t)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := tm.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// parseSetupTemplate parses a template with the quoting functions.
func parseSetupTemplate(t formats.SetupTemplate) (*template.Template, error) {
	return template.New(t.ID).Option("missingkey=error").Funcs(template.FuncMap{
		"shell":        shellQuote,
		"powershell":   powershellQuote,
		"cmd":          cmdQuote,
		"xml":          html.EscapeString,
		"schemeless":   schemeless,
		"hostname":     hostname,
		"trimslash":    trimSlash,
		"registryhost": registryHost,
	}).Parse(t.Template)
}

// hasScopedVariant says whether a template is a configure snippet with a scoped
// variant: it reads SetupData.Variant (Q-API-235). A template that does not read it
// would render the same text twice, so the format declares a variant by using it and
// the core needs no second declaration.
func hasScopedVariant(t formats.SetupTemplate) bool {
	if t.Section != "configure" {
		return false
	}
	tm, err := parseSetupTemplate(t)
	if err != nil {
		return false
	}
	// The associated templates hold the bodies of {{define}} and {{block}}.
	return slices.ContainsFunc(tm.Templates(), func(d *template.Template) bool {
		return d.Tree != nil && readsVariant(d.Root)
	})
}

// readsVariant walks a parse tree for a reference to .Variant, $.Variant or $name.Variant.
func readsVariant(n parse.Node) bool {
	switch n := n.(type) {
	case nil:
		return false
	case *parse.ListNode:
		if n == nil {
			return false
		}
		return slices.ContainsFunc(n.Nodes, readsVariant)
	case *parse.ActionNode:
		return readsVariant(n.Pipe)
	case *parse.PipeNode:
		if n == nil {
			return false
		}
		return slices.ContainsFunc(n.Cmds, func(c *parse.CommandNode) bool { return readsVariant(c) })
	case *parse.CommandNode:
		return slices.ContainsFunc(n.Args, readsVariant)
	case *parse.FieldNode:
		return len(n.Ident) > 0 && n.Ident[0] == "Variant"
	case *parse.VariableNode:
		return len(n.Ident) > 1 && slices.Contains(n.Ident[1:], "Variant")
	case *parse.ChainNode:
		return readsVariant(n.Node) || slices.Contains(n.Field, "Variant")
	case *parse.IfNode:
		return readsBranch(&n.BranchNode)
	case *parse.RangeNode:
		return readsBranch(&n.BranchNode)
	case *parse.WithNode:
		return readsBranch(&n.BranchNode)
	case *parse.TemplateNode:
		return readsVariant(n.Pipe)
	}
	return false
}

func readsBranch(b *parse.BranchNode) bool {
	return readsVariant(b.Pipe) || readsVariant(b.List) || readsVariant(b.ElseList)
}

// clientHasScopedVariant says whether any configure template of the client has a
// scoped variant.
func clientHasScopedVariant(ts []formats.SetupTemplate, client string) bool {
	return slices.ContainsFunc(ts, func(t formats.SetupTemplate) bool { return t.Client == client && hasScopedVariant(t) })
}

// schemeless drops the scheme of an http or https URL and keeps the rest from the
// double slash on; anything else is returned as it is.
func schemeless(u string) string {
	for _, scheme := range []string{"https:", "http:"} {
		if rest, ok := strings.CutPrefix(u, scheme); ok && strings.HasPrefix(rest, "//") {
			return rest
		}
	}
	return u
}

// trimSlash drops one trailing slash of a URL: the form of a client that joins the base and a file name with a slash of its own
// (freshclam's PrivateMirror, formats/clamav.md R-CLAMAV-006).
func trimSlash(u string) string { return strings.TrimSuffix(u, "/") }

// registryHost is the host and port of an image reference's start ("<host>/<project>/<repo>"): what a client that signs in to a
// registry host and not to a path takes (`helm registry login`, formats/oci.md R-OCI-042).
func registryHost(prefix string) string {
	host, _, _ := strings.Cut(prefix, "/")
	return host
}

// hostname is the host name of a URL without its port, brackets or path: the key of a client that binds a credential to a host
// name alone (a ~/.netrc line, R-ANS-025, Q-ANS-48). A value that is not an http or https URL with a host is returned as it is.
func hostname(u string) string {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Hostname() == "" {
		return u
	}
	return p.Hostname()
}

// applyCredentials rewrites the token reference of a rendered snippet by the
// credential mode.
func applyCredentials(content string, in setupInput) string {
	switch in.Credentials {
	case credEnv:
		// Each spelling becomes the reference of the same variable in the syntax of
		// its shell or file (Q-API-187): the client expands it itself.
		return strings.NewReplacer(
			formats.SetupTokenRef, "${"+in.TokenEnvVar+"}",
			formats.SetupTokenRefWin, "%"+in.TokenEnvVar+"%",
			formats.SetupTokenRefPS, "$env:"+in.TokenEnvVar,
		).Replace(content)
	case credNone:
		lines := strings.SplitAfter(content, "\n")
		kept := lines[:0]
		for _, l := range lines {
			if !slices.ContainsFunc(formats.SetupTokenRefs(), func(ref string) bool { return strings.Contains(l, ref) }) {
				kept = append(kept, l)
			}
		}
		return strings.Join(kept, "")
	}
	return strings.NewReplacer(
		formats.SetupTokenRef, tokenPlaceholder, formats.SetupTokenRefWin, tokenPlaceholder, formats.SetupTokenRefPS, tokenPlaceholder,
	).Replace(content)
}

// substitutePackage puts the package of an install snippet in place of the
// placeholder, quoted for the language of the snippet, and attaches the version the
// way the template says (formats.SetupTemplate.VersionSpec, default "@<version>").
func substitutePackage(content string, t formats.SetupTemplate, pkg, version string) string {
	if pkg == "" {
		if t.PackageExample != "" {
			// The example the format's other snippets write, so one thing has one word on the page.
			return strings.ReplaceAll(content, formats.SetupPackageRef, t.PackageExample)
		}
		return content
	}
	quote := quoterFor(t.Lang)
	spec := quote(pkg)
	if version != "" {
		if t.VersionSpec == "" {
			spec = quote(pkg + "@" + version)
		} else {
			spec += strings.ReplaceAll(t.VersionSpec, formats.SetupVersionRef, quote(version))
		}
	}
	return strings.ReplaceAll(content, formats.SetupPackageRef, spec)
}

// substituteFile puts a file's or a folder's path in place of the placeholders of a template of a format whose items are paths
// (R-GENERIC-017). The address gets the path percent-encoded with nothing but unreserved characters (escapeFilePath), so it
// needs no quoting of its own inside the template's double quotes; a folder's address ends in the folder's path, a slash and the
// reader's <file>. The name a download is saved under is the path's last segment, quoted as one argument of the template's
// language, and "./" before a name that starts with "-", which a client would read as an option or as standard output. The
// SHA-256 is hexadecimal, which no shell reads.
func substituteFile(content string, t formats.SetupTemplate, f fileTarget) string {
	target := escapeFilePath(f.Path)
	if f.Folder {
		if target != "" {
			target += "/"
		}
		target += formats.SetupFileRef
	}
	pairs := []string{formats.SetupPathRef, target}
	if !f.Folder {
		pairs = append(pairs, formats.SetupFileNameRef, quoteArgument(t.Lang, savedName(f.Path)))
	}
	if f.SHA256 != "" {
		pairs = append(pairs, formats.SetupSHA256Ref, f.SHA256)
	}
	// One pass: the <file> a folder's address ends in is not replaced again.
	return strings.NewReplacer(pairs...).Replace(content)
}

// escapeFilePath percent-encodes every segment of a canonical path, every byte outside RFC 3986's unreserved characters
// (letters, digits, "-", ".", "_", "~") included, so that "$", "`", "!", a quote or a space of a name stays inert inside the
// double quotes of a shell's or PowerShell's string. The server decodes a request path once (R-ARCH-005), so this names the
// same file as the address the API answers.
func escapeFilePath(p string) string {
	if p == "" {
		return ""
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		var b strings.Builder
		for j := range len(s) {
			c := s[j]
			if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
				b.WriteByte(c)
				continue
			}
			fmt.Fprintf(&b, "%%%02X", c)
		}
		segs[i] = b.String()
	}
	return strings.Join(segs, "/")
}

// savedName is the local name a download is saved under: the last segment of the path, with "./" before it when it starts with
// "-", so that no client reads it as an option, and "-" alone does not mean standard output.
func savedName(p string) string {
	name := p[strings.LastIndex(p, "/")+1:]
	if strings.HasPrefix(name, "-") {
		return "./" + name
	}
	return name
}

// quoteArgument quotes a value as one argument of the language: a POSIX shell's single quotes where it needs them, and
// PowerShell's always, since a bare word there can be a number ("1kb" is 1024) or a splatted variable ("@name").
func quoteArgument(lang, v string) string {
	if lang == "powershell" {
		return powershellLiteral(v)
	}
	return quoterFor(lang)(v)
}

// snippetMarks names the parts of a rendered snippet that are the reader's, in the
// order they first stand in it: the reference to the token in the spelling the
// credential mode wrote, the placeholders of the core (the package, the version, and
// the project and the repository of the guide of a class) and the examples of the
// template. A part that does not stand in the snippet is not named.
func snippetMarks(content string, t formats.SetupTemplate, in setupInput) []snippetMark {
	var cands []snippetMark
	switch in.Credentials {
	case credEnv:
		for _, ref := range []string{"${" + in.TokenEnvVar + "}", "%" + in.TokenEnvVar + "%", "$env:" + in.TokenEnvVar} {
			cands = append(cands, snippetMark{Text: ref, Kind: markToken})
		}
	case credPlaceholder:
		cands = append(cands, snippetMark{Text: tokenPlaceholder, Kind: markToken})
	}
	values := []string{formats.SetupPackageRef, formats.SetupVersionRef, formats.GuideProjectPlaceholder, formats.GuideRepositoryPlaceholder}
	if t.Section == "install" && in.Package == "" && t.PackageExample != "" {
		values = append(values, t.PackageExample)
	}
	for _, v := range append(values, t.Examples...) {
		cands = append(cands, snippetMark{Text: v, Kind: markValue})
	}
	var out []snippetMark
	for _, c := range cands {
		if c.Text != "" && strings.Contains(content, c.Text) && !slices.ContainsFunc(out, func(m snippetMark) bool { return m.Text == c.Text }) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.Index(content, out[i].Text) < strings.Index(content, out[j].Text) })
	return out
}

// quoterFor returns the quoting of a language for a value placed in a command or an
// XML text (R-API-030).
func quoterFor(lang string) func(string) string {
	switch lang {
	case "shell":
		return shellQuote
	case "powershell":
		return powershellQuote
	case "cmd":
		return cmdQuote
	case "xml":
		return html.EscapeString
	}
	return func(s string) string { return s }
}

// powershellQuote quotes a value for a PowerShell command: a value of safe
// characters stays as it is, every other value is single-quoted - nothing is
// expanded inside - with an embedded single quote doubled. The typographic quotes
// PowerShell also reads as single quotes are doubled too.
func powershellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return powershellLiteral(s)
}

// powershellLiteral single-quotes a value for PowerShell whatever it holds: nothing is expanded inside, an embedded single quote
// is doubled, and so are the typographic quotes PowerShell also reads as single quotes.
func powershellLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '\u2018', '\u2019', '\u201a', '\u201b':
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// cmdQuote quotes a value for a cmd command: a value of safe characters stays as it
// is, every other value is double-quoted, an embedded double quote doubled and a
// percent sign doubled, so that no variable is expanded in a batch file. A line
// break cannot be quoted in cmd and is dropped.
func cmdQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	s = strings.NewReplacer("\r", "", "\n", "", `"`, `""`, "%", "%%").Replace(s)
	return `"` + s + `"`
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9@/._+:=][A-Za-z0-9@/._+:=-]*$`)

// shellQuote quotes a value for a POSIX shell command (R-API-030): a value of safe
// characters stays as it is, every other value is single-quoted with an embedded
// single quote written as '\”. Legacy npm names can hold ', (, ) , ! and *, which a
// shell interprets when a user pastes the command. A value that starts with "-" is
// quoted as well, so it never reads as a bare option in a snippet; that the client
// does not take it for one is the caller's to ensure (the package and the version
// of the client setup are refused, Q-NPM-50).
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var tokenShape = regexp.MustCompile(`qdr_(pat|sat|cit)_`)

// holdsTokenShape reports whether s looks like one of the tokens of R-AUTH-001. No
// answer of the client setup may; the same check runs over the input, so that a
// request cannot make the server echo a token it was handed, and over the output.
func holdsTokenShape(s string) bool { return tokenShape.MatchString(s) }
