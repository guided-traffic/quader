package main

import (
	"path"
	"regexp"
	"strings"
)

// The branch coverage holds the record's pages, implemented.txt and the badge, and no other file of the repository (Q-ARCH-1040):
// a relative link of a page to a specification page, a feature page or a developer page would lead nowhere there. With -links-at
// the generator writes such a link as the file on GitHub at the commit the record is made for, the tree the relative link meant;
// a link between the published files stays relative, so the pages lead to each other on the branch.
const repoURL = "https://github.com/guided-traffic/quader/"

// publishedFiles are the files of the record that the branch holds beside the badge.
var publishedFiles = map[string]bool{
	"docs/coverage/README.md":    true,
	"docs/coverage/checklist.md": true,
	recordRel:                    true,
}

// commitHash is what -links-at accepts: a commit's hash, abbreviated or whole, as `git rev-parse` and GITHUB_SHA give it.
var commitHash = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// linksAt returns page, a file of the directory dir (slash-separated, relative to the repository root), with every relative link to
// a file the branch does not hold pointed at that file on GitHub at commit. Links inside fenced code and inline code, links with a
// scheme and links to an anchor of the page itself stay as they are.
func linksAt(page, dir, commit string) string {
	lines := strings.Split(page, "\n")
	visible, _ := visibleLines(lines, nil)
	for _, idx := range visible {
		lines[idx] = linksAtLine(lines[idx], dir, commit)
	}
	return strings.Join(lines, "\n")
}

func linksAtLine(line, dir, commit string) string {
	if m := refDefinion.FindStringSubmatchIndex(line); m != nil {
		return line[:m[2]] + linkAt(line[m[2]:m[3]], dir, commit) + line[m[3]:]
	}
	var b strings.Builder
	last := 0
	for _, span := range inlineCode.FindAllStringIndex(line, -1) {
		b.WriteString(inlineLinksAt(line[last:span[0]], dir, commit))
		b.WriteString(line[span[0]:span[1]])
		last = span[1]
	}
	b.WriteString(inlineLinksAt(line[last:], dir, commit))
	return b.String()
}

func inlineLinksAt(text, dir, commit string) string {
	var b strings.Builder
	last := 0
	for _, m := range inlineLink.FindAllStringSubmatchIndex(text, -1) {
		b.WriteString(text[last:m[2]])
		b.WriteString(linkAt(text[m[2]:m[3]], dir, commit))
		last = m[3]
	}
	b.WriteString(text[last:])
	return b.String()
}

// linkAt is the target raw of a link of a page in dir as the branch needs it.
func linkAt(raw, dir, commit string) string {
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "//") || urlScheme.MatchString(raw) {
		return raw
	}
	pathPart, fragment, hasFragment := strings.Cut(raw, "#")
	target := strings.TrimPrefix(pathPart, "/")
	if !strings.HasPrefix(pathPart, "/") {
		target = path.Join(dir, pathPart)
	}
	target = path.Clean(target)
	if target == ".." || strings.HasPrefix(target, "../") || publishedFiles[target] {
		return raw
	}
	kind := "blob/"
	if strings.HasSuffix(pathPart, "/") || target == "." {
		kind = "tree/"
	}
	out := repoURL + kind + commit + "/"
	if target != "." {
		out += target
	}
	if hasFragment {
		out += "#" + fragment
	}
	return out
}
