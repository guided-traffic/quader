package control

import (
	"fmt"
	"strings"
)

// The relation fields of a binary package (R-DEB-038, Debian Policy 7.1): a comma-separated list of groups, each a list of alternatives
// separated by "|", each alternative a package name with an optional architecture qualifier (":any"), an optional version constraint in
// parentheses, an optional architecture restriction in brackets and optional build profile restrictions in angle brackets. Binary
// packages rarely carry the last two (dpkg-gencontrol reduces them away), but the grammar admits them, so they are read and kept.

// RelationFields are the relation fields of a binary package in the order apt show lists them, with the member name each has in the
// version's metadata.
var RelationFields = [...]struct{ Field, Key string }{
	{"Pre-Depends", "preDepends"},
	{"Depends", "depends"},
	{"Recommends", "recommends"},
	{"Suggests", "suggests"},
	{"Enhances", "enhances"},
	{"Breaks", "breaks"},
	{"Conflicts", "conflicts"},
	{"Replaces", "replaces"},
	{"Provides", "provides"},
}

// Relation is one alternative of a relation group.
type Relation struct {
	// Name is the package name.
	Name string `json:"name"`
	// Arch is the architecture qualifier after ":" ("any", "native" or an architecture), empty for none.
	Arch string `json:"arch,omitempty"`
	// Op and Version are the version constraint: Op one of "<<", "<=", "=", ">=", ">>"; both empty for none.
	Op      string `json:"op,omitempty"`
	Version string `json:"version,omitempty"`
	// Architectures is the architecture restriction in brackets, each entry as written ("amd64", "!i386").
	Architectures []string `json:"architectures,omitempty"`
	// Profiles are the build profile restrictions, one list per pair of angle brackets ("!nocheck").
	Profiles [][]string `json:"profiles,omitempty"`
}

// MaxRelations bounds the alternatives one field is read into: a field of a control paragraph is at most 1 MiB, and a list beyond this
// is no list a person reads (R-DEB-038).
const MaxRelations = 2000

// ParseRelations reads a relation field into its groups of alternatives. An empty field is no group. A field that does not follow the
// grammar is an error naming what is wrong, and the caller shows the field as text.
func ParseRelations(value string) ([][]Relation, error) {
	var out [][]Relation
	n := 0
	for _, group := range strings.Split(value, ",") {
		if strings.TrimSpace(group) == "" {
			if strings.TrimSpace(value) == "" {
				return nil, nil
			}
			continue // a trailing comma, which dpkg tolerates
		}
		var alts []Relation
		for _, alt := range strings.Split(group, "|") {
			r, err := parseRelation(alt)
			if err != nil {
				return nil, err
			}
			if n++; n > MaxRelations {
				return nil, fmt.Errorf("the field holds more than %d relations", MaxRelations)
			}
			alts = append(alts, r)
		}
		out = append(out, alts)
	}
	return out, nil
}

// parseRelation reads one alternative.
func parseRelation(s string) (Relation, error) {
	s = strings.TrimSpace(s)
	var r Relation
	end := strings.IndexAny(s, " \t\n([<")
	if end < 0 {
		end = len(s)
	}
	name := s[:end]
	if q := strings.IndexByte(name, ':'); q >= 0 {
		name, r.Arch = name[:q], name[q+1:]
		if !archToken(r.Arch) {
			return Relation{}, fmt.Errorf("the architecture qualifier of %q is not valid", shortRel(s))
		}
	}
	if !packageName(name) {
		return Relation{}, fmt.Errorf("%q is not a package name", shortRel(name))
	}
	r.Name = name
	rest := strings.TrimSpace(s[end:])
	if strings.HasPrefix(rest, "(") {
		closeAt := strings.IndexByte(rest, ')')
		if closeAt < 0 {
			return Relation{}, fmt.Errorf("the version constraint of %s is not closed", name)
		}
		op, ver, err := constraint(rest[1:closeAt])
		if err != nil {
			return Relation{}, fmt.Errorf("the version constraint of %s: %w", name, err)
		}
		r.Op, r.Version = op, ver
		rest = strings.TrimSpace(rest[closeAt+1:])
	}
	if strings.HasPrefix(rest, "[") {
		closeAt := strings.IndexByte(rest, ']')
		if closeAt < 0 {
			return Relation{}, fmt.Errorf("the architecture restriction of %s is not closed", name)
		}
		r.Architectures = strings.Fields(rest[1:closeAt])
		for _, a := range r.Architectures {
			if !archToken(strings.TrimPrefix(a, "!")) {
				return Relation{}, fmt.Errorf("the architecture restriction of %s is not valid", name)
			}
		}
		rest = strings.TrimSpace(rest[closeAt+1:])
	}
	for strings.HasPrefix(rest, "<") {
		closeAt := strings.IndexByte(rest, '>')
		if closeAt < 0 {
			return Relation{}, fmt.Errorf("a build profile restriction of %s is not closed", name)
		}
		terms := strings.Fields(rest[1:closeAt])
		for _, t := range terms {
			if !profileToken(strings.TrimPrefix(t, "!")) {
				return Relation{}, fmt.Errorf("a build profile restriction of %s is not valid", name)
			}
		}
		r.Profiles = append(r.Profiles, terms)
		rest = strings.TrimSpace(rest[closeAt+1:])
	}
	if rest != "" {
		return Relation{}, fmt.Errorf("%q follows the relation on %s", shortRel(rest), name)
	}
	return r, nil
}

// constraint reads "op version"; the obsolete "<" and ">" read as "<=" and ">=", as dpkg reads them.
func constraint(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	var op string
	for _, o := range []string{"<<", "<=", ">=", ">>", "=", "<", ">"} {
		if strings.HasPrefix(s, o) {
			op = o
			break
		}
	}
	if op == "" {
		return "", "", fmt.Errorf("it has no operator")
	}
	ver := strings.TrimSpace(s[len(op):])
	switch op {
	case "<":
		op = "<="
	case ">":
		op = ">="
	}
	if ver == "" || strings.ContainsAny(ver, " \t\n()") {
		return "", "", fmt.Errorf("it has no version")
	}
	return op, ver, nil
}

// packageName is the grammar of R-DEB-002: lower-case letters, digits, '+', '-' and '.', starting with a letter or a digit, at least two.
func packageName(s string) bool {
	if len(s) < 2 || len(s) > 256 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case i > 0 && (c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}

// archToken admits an architecture, a wildcard ("linux-any") or a qualifier ("any", "native").
func archToken(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// profileToken admits a build profile name.
func profileToken(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' && c != '+' {
			return false
		}
	}
	return true
}

func shortRel(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// LongDescription reads the extended description of a Description field (Debian Policy 5.6.13) into its paragraphs: the lines after the
// synopsis, where a line "." is an empty line, a line that starts with a space (one more than the field's own) is shown verbatim and
// every other line is text that may be wrapped. A paragraph is a run of lines of one kind; an empty line ends it.
func LongDescription(description string) []DescriptionBlock {
	_, rest, found := strings.Cut(description, "\n")
	if !found {
		return nil
	}
	var out []DescriptionBlock
	var cur *DescriptionBlock
	for _, line := range strings.Split(rest, "\n") {
		if line == "." {
			cur = nil
			continue
		}
		verbatim := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if cur == nil || cur.Verbatim != verbatim {
			out = append(out, DescriptionBlock{Verbatim: verbatim})
			cur = &out[len(out)-1]
		}
		if cur.Text != "" {
			cur.Text += "\n"
		}
		cur.Text += line
	}
	return out
}

// DescriptionBlock is one paragraph of an extended description.
type DescriptionBlock struct {
	// Text is the paragraph's lines joined by "\n": a text paragraph's lines are wrapped by the screen, a verbatim one's are kept.
	Text string `json:"text"`
	// Verbatim says that the lines are shown as written (lists, tables, examples).
	Verbatim bool `json:"verbatim,omitempty"`
}
