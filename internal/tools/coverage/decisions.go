package main

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The index of decided questions: the part of docs/decisions.md between the marker pair of the block "decisions" holds one
// row per `### Q-<AREA>-<n> · <title>` block of the pages below docs (the catalog excepted), generated from the block. The
// rest of the page (the ADRs, the owner's decisions, the introductions) is written by hand and never touched.

const (
	decisionsBlock = "decisions"
	regenerate     = "go run ./internal/tools/coverage"
	diffShown      = 20 // -check names at most this many rows of the index one by one
)

var (
	choiceLabel   = regexp.MustCompile(`\*\*Choice(?: \([^)*]*\))?:\*\*|\*\*Choice\*\*:`)
	nextLabel     = regexp.MustCompile(`^ \*\*[A-Z][^*]{0,80}:\*\*`)
	adrHeading    = regexp.MustCompile(`^### (ADR-[0-9]+) · `)
	adrName       = regexp.MustCompile(`\bADR-([0-9]+)\b`)
	questionName  = regexp.MustCompile(`\b(Q-` + idPattern + `)\b`)
	optionLetters = regexp.MustCompile(`^(?:\(\w{1,4}\)(?:\s*(?:,|and|with|plus|\+|/)\s*)?)+$`)
	optionLetter  = regexp.MustCompile(`\((\w{1,4})\)`)
	statusOption  = regexp.MustCompile(`^options? ((?:\(\w{1,4}\)(?:\s*(?:,|and)\s*)?)+)(?:, as recommended)?$`)
	indexRowID    = regexp.MustCompile(`^\| \[(Q-` + idPattern + `)\]`)
	linkTarget    = regexp.MustCompile(`(\]\(<?)([^)\s>]+)`)
)

// question is a decided question's block: the heading without its hashes, the page below docs, the heading's line and the
// lines after it up to the next heading of level 1 to 3.
type question struct {
	ID, Heading, Page string
	Line              int
	Body              []string
}

func (q question) title() string {
	_, t, _ := strings.Cut(q.Heading, " · ")
	return t
}

func (q question) area() (string, int) {
	i := strings.LastIndex(q.ID, "-")
	n, _ := strconv.Atoi(q.ID[i+1:])
	return q.ID[:i], n
}

func (q question) where() string { return fmt.Sprintf("docs/%s:%d", q.Page, q.Line) }

// blockEnd returns the index of the first line among the visible ones that is a heading of level 1 to 3, or len(lines).
func blockEnd(lines []string, visible []int) int {
	for _, idx := range visible {
		if m := atxHeading.FindStringSubmatch(lines[idx]); m != nil && len(m[1]) <= 3 {
			return idx
		}
	}
	return len(lines)
}

// adrEntry is a hand-written entry of the decision log: its anchor and the questions its text names.
type adrEntry struct {
	Anchor string
	Names  map[string]bool
}

func markerPair(name string) (string, string) {
	return fmt.Sprintf("<!-- coverage-gen:begin %s -->", name), fmt.Sprintf("<!-- coverage-gen:end %s -->", name)
}

// renderDecisions replaces the generated part of docs/decisions.md. A row of the index that stands outside the marker pair is
// an error: the index is generated, and a hand-written row there would never be checked.
func renderDecisions(qs []question, text string) (string, []string) {
	begin, end := markerPair(decisionsBlock)
	if strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 || strings.Index(text, begin) > strings.Index(text, end) {
		return "", []string{fmt.Sprintf("docs/decisions.md: marker pair for block %q missing or malformed", decisionsBlock)}
	}
	var errs []string
	inside := false
	for i, l := range strings.Split(text, "\n") {
		switch {
		case l == begin:
			inside = true
		case l == end:
			inside = false
		case !inside && indexRowID.MatchString(l):
			errs = append(errs, fmt.Sprintf("docs/decisions.md:%d: a row of the index of decided questions stands outside the generated "+
				"part; the index is generated from the question's block: delete the row and run `%s`", i+1, regenerate))
		}
	}
	head, rest, _ := strings.Cut(text, begin)
	_, tail, _ := strings.Cut(rest, end)
	adrs := parseADRs(head + "\n" + tail)
	return head + begin + "\n" + strings.Join(renderIndex(qs, adrs), "\n") + "\n" + end + tail, errs
}

// parseADRs reads the entries `### ADR-<n> · <title>` of the hand-written part of the decision log.
func parseADRs(text string) map[string]*adrEntry {
	out := map[string]*adrEntry{}
	lines := strings.Split(text, "\n")
	visible, _ := visibleLines(lines, nil)
	var cur *adrEntry
	for _, idx := range visible {
		l := lines[idx]
		if m := adrHeading.FindStringSubmatch(l); m != nil {
			cur = &adrEntry{Anchor: headingSlug(strings.TrimPrefix(l, "### ")), Names: map[string]bool{}}
			out[m[1]] = cur
			continue
		}
		if h := atxHeading.FindStringSubmatch(l); h != nil && len(h[1]) <= 3 {
			cur = nil
		}
		if cur != nil {
			for _, m := range questionName.FindAllStringSubmatch(l, -1) {
				cur.Names[m[1]] = true
			}
		}
	}
	return out
}

func indexHeader() []string { return []string{"ID", "Question", "Status", "Choice", "ADR"} }

// renderIndex writes one section per area, ordered by area, with one row per question ordered by number.
func renderIndex(qs []question, adrs map[string]*adrEntry) []string {
	sorted := append([]question(nil), qs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ai, ni := sorted[i].area()
		aj, nj := sorted[j].area()
		if ai != aj {
			return ai < aj
		}
		return ni < nj
	})
	var out []string
	for i := 0; i < len(sorted); {
		area, _ := sorted[i].area()
		j := i
		pages := map[string]bool{}
		var rows [][]string
		for ; j < len(sorted); j++ {
			if a, _ := sorted[j].area(); a != area {
				break
			}
			pages[sorted[j].Page] = true
			rows = append(rows, indexCells(sorted[j], adrs))
		}
		var links []string
		for p := range pages {
			links = append(links, fmt.Sprintf("[%s](%s)", p, p))
		}
		sort.Strings(links)
		n := j - i
		noun := "questions"
		if n == 1 {
			noun = "question"
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, "### "+area, "", fmt.Sprintf("%d %s on %s.", n, noun, joinAnd(links)), "")
		out = append(out, maybeDetails(fmt.Sprintf("%s: %d %s", area, n, noun), table(indexHeader(), rows, nil), n)...)
		i = j
	}
	return out
}

func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func indexCells(q question, adrs map[string]*adrEntry) []string {
	return []string{
		fmt.Sprintf("[%s](%s#%s)", q.ID, q.Page, headingSlug(q.Heading)),
		escapePipes(rebaseLinks(q.title(), q.Page)),
		escapePipes(rebaseLinks(orDash(statusOf(q)), q.Page)),
		escapePipes(rebaseLinks(orDash(choiceOf(q)), q.Page)),
		adrCell(q, adrs),
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// paragraph joins the line at i and the lines that continue its paragraph: up to a blank line, a table row, a list item, a
// heading, an HTML line or a fence.
func paragraph(body []string, i int) string {
	parts := []string{strings.TrimSpace(body[i])}
	for _, l := range body[i+1:] {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") ||
			strings.HasPrefix(t, "#") || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			break
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

// firstSentence returns s up to the end of its first sentence, or up to the next bold label such as **Rationale:**; neither
// is looked for inside inline code, parentheses or brackets.
func firstSentence(s string) string {
	depth, code := 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '`':
			code = !code
		case code:
		case c == '(' || c == '[':
			depth++
		case (c == ')' || c == ']') && depth > 0:
			depth--
		case depth > 0:
		case c == ' ' && nextLabel.MatchString(s[i:]):
			return strings.TrimSpace(s[:i])
		case c == '.' && i+2 < len(s) && s[i+1] == ' ' && startsSentence(s[i+2]) && !abbreviated(s[:i]):
			return s[:i+1]
		}
	}
	return strings.TrimSpace(s)
}

func startsSentence(c byte) bool {
	return (c >= 'A' && c <= 'Z') || c == '*' || c == '`' || c == '[' || c == '('
}

func abbreviated(before string) bool {
	word := before[strings.LastIndexByte(before, ' ')+1:]
	switch strings.ToLower(word) {
	case "e.g", "i.e", "cf", "vs":
		return true
	}
	return false
}

// statusOf is the value of the block's status line: the text before its first dash or the end of its first sentence, without
// the bold marks.
func statusOf(q question) string {
	v, _ := statusParts(q)
	return v
}

// statusParts splits the status paragraph into its value and the rest. The value is the bold part with the words that
// continue it (" by [Q-…]", " into …") up to the first clause end, or, without a bold part, the first sentence up to a
// dash. The rest is what follows the first dash, or else the first sentence.
func statusParts(q question) (value, rest string) {
	for i, l := range q.Body {
		if !strings.HasPrefix(l, "Status:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(paragraph(q.Body, i), "Status:"))
		if before, after, ok := strings.Cut(v, " — "); ok {
			rest = after
			if !strings.HasPrefix(v, "**") || !strings.Contains(v[2:len(before)], "**") {
				v = before
			}
		}
		if bold, after, ok := strings.Cut(strings.TrimPrefix(v, "**"), "**"); ok && strings.HasPrefix(v, "**") {
			value = bold
			if !strings.HasSuffix(bold, ".") && continues(after) {
				value += firstClause(after)
			}
		} else {
			value = firstSentence(v)
		}
		if rest == "" {
			rest = v[min(len(firstSentence(v)), len(v)):]
		}
		value = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(value, "**", "")), ".:")
		return value, strings.TrimLeft(strings.TrimSpace(rest), "*—:,; ")
	}
	return "", ""
}

// continues tells whether the text after a bold status continues its phrase: a space and a lower-case word or a link.
func continues(after string) bool {
	return len(after) > 1 && after[0] == ' ' && ((after[1] >= 'a' && after[1] <= 'z') || after[1] == '[')
}

// firstClause returns s up to its first sentence end, dash, colon or semicolon outside inline code, parentheses and brackets.
func firstClause(s string) string {
	depth, code := 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '`':
			code = !code
		case code:
		case c == '(' || c == '[':
			depth++
		case (c == ')' || c == ']') && depth > 0:
			depth--
		case depth > 0:
		case c == ':' || c == ';' || (c == '.' && i+1 < len(s) && s[i+1] == ' ') || strings.HasPrefix(s[i:], " — "):
			return s[:i]
		}
	}
	return s
}

// choiceOf is what the block chose: the first sentence of its **Choice:** paragraph, or, when the status line names the
// option, that sentence of the status line, or the option row marked "(chosen)". A sentence that names only option letters is
// replaced by those options' names from the block's options table.
func choiceOf(q question) string {
	for i, l := range q.Body {
		if !choiceLabel.MatchString(l) {
			continue
		}
		p := paragraph(q.Body, i)
		if s := firstSentence(strings.TrimSpace(p[choiceLabel.FindStringIndex(p)[1]:])); s != "" {
			return expandOptions(q.Body, s, s)
		}
		break
	}
	if _, rest := statusParts(q); strings.HasPrefix(rest, "option") {
		s := firstSentence(rest)
		if m := statusOption.FindStringSubmatch(strings.TrimRight(s, ".;: ")); m != nil {
			return expandOptions(q.Body, m[1], s)
		}
		return s
	}
	for _, l := range q.Body {
		if optionLetter.MatchString(l) && strings.HasPrefix(l, "| (") && strings.Contains(l, "(chosen)") {
			return optionName(l)
		}
	}
	return ""
}

// expandOptions returns the names of the options that letters names, from the block's options table, when letters names
// options only and the table has a row for each; otherwise it returns fallback.
func expandOptions(body []string, letters, fallback string) string {
	trimmed := strings.TrimRight(letters, ".:;, ")
	if !optionLetters.MatchString(trimmed) {
		return fallback
	}
	var names []string
	for _, m := range optionLetter.FindAllStringSubmatch(trimmed, -1) {
		row := ""
		for _, l := range body {
			if rest, ok := strings.CutPrefix(l, "| ("+m[1]+")"); ok && (strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "|")) {
				row = l
				break
			}
		}
		if row == "" {
			return fallback
		}
		names = append(names, optionName(row))
	}
	return strings.Join(names, "; ")
}

// optionName is the first cell of an options table's row, without the mark "(chosen)".
func optionName(row string) string {
	cells := splitCells(row)
	if len(cells) < 2 {
		return ""
	}
	name := strings.TrimSpace(cells[1])
	name = strings.ReplaceAll(name, " (chosen)", "")
	return strings.TrimSpace(strings.ReplaceAll(name, "(chosen)", ""))
}

// splitCells splits a table row at the pipes that are not escaped.
func splitCells(row string) []string {
	var cells []string
	start := 0
	for i := 0; i < len(row); i++ {
		if row[i] == '|' && (i == 0 || row[i-1] != '\\') {
			cells = append(cells, row[start:i])
			start = i + 1
		}
	}
	return append(cells, row[start:])
}

func escapePipes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '|' && (i == 0 || s[i-1] != '\\') {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// rebaseLinks rewrites the relative link targets of text, written on page (below docs), to resolve from docs/decisions.md.
// Inline code is left as it is.
func rebaseLinks(text, page string) string {
	parts := strings.Split(text, "`")
	for i := 0; i < len(parts); i += 2 {
		parts[i] = linkTarget.ReplaceAllStringFunc(parts[i], func(m string) string {
			sub := linkTarget.FindStringSubmatch(m)
			return sub[1] + rebaseTarget(sub[2], page)
		})
	}
	return strings.Join(parts, "`")
}

func rebaseTarget(target, page string) string {
	if urlScheme.MatchString(target) || strings.HasPrefix(target, "/") {
		return target
	}
	p, frag, hasFrag := strings.Cut(target, "#")
	resolved := page
	if p != "" {
		resolved = path.Clean(path.Join(path.Dir(page), p))
	}
	switch {
	case resolved == "decisions.md" && hasFrag:
		return "#" + frag
	case hasFrag:
		return resolved + "#" + frag
	}
	return resolved
}

// adrCell links the entries of the decision log that the block names or that name the question.
func adrCell(q question, adrs map[string]*adrEntry) string {
	nums := map[int]bool{}
	for _, l := range append([]string{q.Heading}, q.Body...) {
		for _, m := range adrName.FindAllStringSubmatch(l, -1) {
			n, _ := strconv.Atoi(m[1])
			nums[n] = true
		}
	}
	for id, e := range adrs {
		if e.Names[q.ID] {
			n, _ := strconv.Atoi(strings.TrimPrefix(id, "ADR-"))
			nums[n] = true
		}
	}
	if len(nums) == 0 {
		return "—"
	}
	sorted := make([]int, 0, len(nums))
	for n := range nums {
		sorted = append(sorted, n)
	}
	sort.Ints(sorted)
	var out []string
	for _, n := range sorted {
		id := fmt.Sprintf("ADR-%03d", n)
		if e, ok := adrs[id]; ok {
			out = append(out, fmt.Sprintf("[%s](#%s)", id, e.Anchor))
		} else {
			out = append(out, id)
		}
	}
	return strings.Join(out, ", ")
}

// indexDiff names the rows in which the committed index differs from the generated one: a block without a row, a row
// without a block, a row that no longer matches its block.
func indexDiff(qs []question, committed, generated string) []string {
	old, gen := indexRows(committed), indexRows(generated)
	at := map[string]question{}
	for _, q := range qs {
		at[q.ID] = q
	}
	var ids []string
	for id := range gen {
		ids = append(ids, id)
	}
	for id := range old {
		if _, ok := gen[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []string
	for _, id := range ids {
		o, inOld := old[id]
		g, inGen := gen[id]
		switch {
		case !inOld:
			out = append(out, fmt.Sprintf("docs/decisions.md: %s (%s) has no row in the index of decided questions", id, at[id].where()))
		case !inGen:
			out = append(out, fmt.Sprintf("docs/decisions.md: the index of decided questions has a row for %s, which no page defines", id))
		case o != g:
			out = append(out, fmt.Sprintf("docs/decisions.md: the row of %s no longer matches its block (%s): its title, status, "+
				"choice or ADRs changed", id, at[id].where()))
		}
	}
	if len(out) > diffShown {
		out = append(out[:diffShown], fmt.Sprintf("docs/decisions.md: and %d more rows", len(out)-diffShown))
	}
	if len(out) > 0 {
		out = append(out, fmt.Sprintf("docs/decisions.md: the index is generated: run `%s` and commit the result", regenerate))
	}
	return out
}

// indexRows returns the rows of the generated part of the decision log by question.
func indexRows(text string) map[string]string {
	begin, end := markerPair(decisionsBlock)
	_, rest, _ := strings.Cut(text, begin)
	region, _, _ := strings.Cut(rest, end)
	out := map[string]string{}
	for _, l := range strings.Split(region, "\n") {
		if m := indexRowID.FindStringSubmatch(l); m != nil {
			out[m[1]] = l
		}
	}
	return out
}
