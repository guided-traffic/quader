// Command coverage generates the coverage pages of docs/coverage from the
// feature catalog (docs/features) and the specification (docs/spec), and the
// index of decided questions in docs/decisions.md from the questions' blocks,
// enforces the rules of those pages, learns the implementation status from
// test reports (Q-ROAD-4) and checks the relative links and anchors of the
// repository's Markdown. See docs/developer/coverage.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	exitOK    = 0
	exitData  = 1 // rule violation, bad input, broken link, a record or a badge on main
	exitStale = 2 // -check: a committed file differs from the regenerated one
	exitUsage = 64
)

// The evidence record and the coverage badge are written by the pipeline onto the branch recordBranch and never committed to
// main (Q-ARCH-811). Without -tests the generator renders main's pages without an implementation status and refuses a tree that
// holds either file.
const (
	recordBranch = "coverage"
	recordPage   = "https://github.com/guided-traffic/quader/blob/" + recordBranch + "/docs/coverage/"
	recordRel    = "docs/coverage/implemented.txt"
	badgeRel     = ".github/badges/coverage.json"
)

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

type options struct {
	Repo  string
	Check bool
	Links bool
	Tests []string
	// Carried are reports of earlier runs of legs that did not run in this one (Q-ROAD-11). They prove and veto like Tests; the
	// output names a requirement that only their failing test keeps out of the record.
	Carried []string
	// Previous is the record the pipeline published last (on the branch coverage); the output lists what this record adds and drops.
	Previous string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var tests, carried stringList
	opts := options{}
	fs.StringVar(&opts.Repo, "repo", "", "repository root (default: nearest parent directory holding go.mod)")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 2 when a generated file differs from what would be generated")
	fs.BoolVar(&opts.Links, "links", false, "check the relative links and anchors of every Markdown file instead of generating")
	fs.Var(&tests, "tests", "test report with requirement IDs in test names: `go test -json` stream or JUnit XML; "+
		"'-' reads stdin; repeatable, further file arguments count too; writes the evidence record from the reports (the pipeline's form)")
	fs.Var(&carried, "carried", "like -tests, for a report of an earlier run of a leg that did not run in this one; repeatable, needs -tests")
	fs.StringVar(&opts.Previous, "previous", "", "with -tests, the record published last: list the requirements this record adds and drops")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: coverage [-repo dir] [-check]\n"+
			"       coverage [-repo dir] -tests report... [-carried report]... [-previous implemented.txt] [report...]\n"+
			"       coverage [-repo dir] -links\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	opts.Tests = append(append([]string{}, tests...), fs.Args()...)
	opts.Carried = carried
	if opts.Repo == "" {
		root, err := findRepo()
		if err != nil {
			fmt.Fprintln(stderr, "coverage:", err)
			return exitUsage
		}
		opts.Repo = root
	}
	if opts.Links {
		if len(opts.Tests) > 0 || len(opts.Carried) > 0 || opts.Previous != "" || opts.Check {
			fmt.Fprintln(stderr, "coverage: -links does not combine with -check, -tests, -carried or -previous")
			return exitUsage
		}
		return runLinks(opts.Repo, stdout, stderr)
	}
	if opts.Check && len(opts.Tests) > 0 {
		fmt.Fprintln(stderr, "coverage: -check does not combine with -tests: main holds no record to compare with (Q-ARCH-811)")
		return exitUsage
	}
	if (len(opts.Carried) > 0 || opts.Previous != "") && len(opts.Tests) == 0 {
		fmt.Fprintln(stderr, "coverage: -carried and -previous need -tests")
		return exitUsage
	}
	return runGenerate(opts, stdin, stdout, stderr)
}

func findRepo() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod found above the working directory; pass -repo")
		}
		dir = parent
	}
}

func runLinks(repo string, stdout, stderr io.Writer) int {
	broken, checked, err := checkLinks(repo)
	if err != nil {
		fmt.Fprintln(stderr, "coverage:", err)
		return exitData
	}
	fmt.Fprintf(stdout, "relative links checked: %d, broken: %d\n", checked, len(broken))
	for _, b := range broken {
		fmt.Fprintln(stderr, "  "+b)
	}
	if len(broken) > 0 {
		return exitData
	}
	return exitOK
}

func fail(stderr io.Writer, errs []string) int {
	fmt.Fprintln(stderr, "coverage: error")
	for _, e := range errs {
		fmt.Fprintln(stderr, e)
	}
	return exitData
}

func runGenerate(opts options, stdin io.Reader, stdout, stderr io.Writer) int {
	docs := filepath.Join(opts.Repo, "docs")
	c, err := load(docs)
	if err != nil {
		fmt.Fprintln(stderr, "coverage:", err)
		return exitData
	}
	if len(c.Errors) > 0 {
		return fail(stderr, c.Errors)
	}
	known := map[string]bool{}
	for _, r := range c.Reqs {
		known[r.ID] = true
	}

	implPath := filepath.Join(opts.Repo, filepath.FromSlash(recordRel))
	targets := map[string]string{}
	// set stays nil without -tests: main's pages are rendered without an implementation status (Q-ARCH-811).
	var set map[string]bool
	if len(opts.Tests) > 0 {
		ids, code := recordFromReports(opts, known, stdin, stdout, stderr)
		if code != exitOK {
			return code
		}
		targets[implPath] = renderImplemented(ids)
		set = map[string]bool{}
		for _, id := range ids {
			set[id] = true
		}
	} else if errs := heldByTheBranch(opts.Repo); len(errs) > 0 {
		return fail(stderr, errs)
	}

	order := pageOrder(filepath.Join(docs, "features"), c.Pages)
	m := newModel(c, order, set)
	if errs := m.checkAudiences(); len(errs) > 0 {
		return fail(stderr, errs)
	}
	anchors := map[string]bool{}
	var anchorErrs []string
	for _, p := range m.Pages {
		a := m.anchor(p)
		if anchors[a] || strings.Contains(a, "--") {
			anchorErrs = append(anchorErrs, "section anchor not unique or not host-independent: "+a)
		}
		anchors[a] = true
	}
	if len(anchorErrs) > 0 {
		return fail(stderr, anchorErrs)
	}

	checklist := renderChecklist(m)
	readmePath := filepath.Join(docs, "coverage", "README.md")
	readmeOld, err := os.ReadFile(readmePath) //nolint:gosec // G304: fixed path below the repository root given on the command line
	if err != nil {
		return fail(stderr, []string{"docs/coverage/README.md does not exist; it holds the prose around the generated blocks"})
	}
	readme, err := renderReadme(m, string(readmeOld))
	if err != nil {
		return fail(stderr, []string{err.Error()})
	}
	decisionsPath := filepath.Join(docs, "decisions.md")
	decisionsOld, err := os.ReadFile(decisionsPath) //nolint:gosec // G304: fixed path below the repository root given on the command line
	if err != nil {
		return fail(stderr, []string{"docs/decisions.md does not exist; it holds the prose around the generated index of decided questions"})
	}
	decisions, errs := renderDecisions(c.Questions, string(decisionsOld))
	if len(errs) > 0 {
		return fail(stderr, errs)
	}
	targets[filepath.Join(docs, "coverage", "checklist.md")] = checklist
	targets[readmePath] = readme
	targets[decisionsPath] = decisions

	printSummary(stdout, m, c)

	paths := make([]string, 0, len(targets))
	for p := range targets {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if opts.Check {
		return checkTargets(opts.Repo, paths, targets, func(p, old string) []string {
			if p != decisionsPath {
				return nil
			}
			return indexDiff(c.Questions, old, targets[p])
		}, stderr)
	}
	for _, p := range paths {
		//nolint:gosec // G306: generated documentation is committed and must be world-readable like the other docs
		if err := os.WriteFile(p, []byte(targets[p]), 0o644); err != nil {
			fmt.Fprintln(stderr, "coverage:", err)
			return exitData
		}
		rel, _ := filepath.Rel(opts.Repo, p)
		fmt.Fprintf(stdout, "wrote %s (%s bytes)\n", filepath.ToSlash(rel), num(len(targets[p])))
	}
	return exitOK
}

// checkTargets compares each generated file with the committed one and names the stale ones; explain adds the lines that say
// what differs in a file, where the generator can tell.
func checkTargets(repo string, paths []string, targets map[string]string, explain func(path, old string) []string, stderr io.Writer) int {
	stale := 0
	for _, p := range paths {
		old, err := os.ReadFile(p) //nolint:gosec // G304: path below the repository root given on the command line
		if err == nil && string(old) == targets[p] {
			continue
		}
		for _, l := range explain(p, string(old)) {
			fmt.Fprintln(stderr, l)
		}
		rel, _ := filepath.Rel(repo, p)
		fmt.Fprintf(stderr, "stale: %s (run the generator and commit the result)\n", filepath.ToSlash(rel))
		stale++
	}
	if stale > 0 {
		return exitStale
	}
	return exitOK
}

// recordFromReports is the pipeline's form (Q-ARCH-811): the record is what the reports prove, and nothing else. A test that names
// an ID the specification does not define fails it; with -previous it lists the requirements the record adds and the ones it
// drops, each with the reason, and fails on neither: no committed list is compared, because main holds none.
func recordFromReports(opts options, known map[string]bool, stdin io.Reader, stdout, stderr io.Writer) ([]string, int) {
	fromRun, fromEarlier := newEvidence(), newEvidence()
	for _, path := range opts.Tests {
		if err := fromRun.readFile(path, stdin); err != nil {
			return nil, fail(stderr, []string{err.Error()})
		}
	}
	for _, path := range opts.Carried {
		if err := fromEarlier.readFile(path, stdin); err != nil {
			return nil, fail(stderr, []string{err.Error()})
		}
	}
	ev := merged(fromRun, fromEarlier)
	ids, unknown := ev.implemented(known)
	if len(unknown) > 0 {
		return nil, fail(stderr, []string{"test names carry requirement IDs that no spec page defines: " + strings.Join(unknown, ", ")})
	}
	fmt.Fprintf(stdout, "tests: %d results read, %d requirements with passing evidence, %d with a failing test, %d held as not built\n",
		ev.Tests, len(ids), len(ev.Failed), len(ev.Unbuilt))
	if opts.Previous == "" {
		return ids, exitOK
	}
	data, err := os.ReadFile(opts.Previous) //nolint:gosec // G304: a record named on the tool's command line
	if err != nil {
		return nil, fail(stderr, []string{"-previous: " + err.Error()})
	}
	previous, err := parseImplemented(string(data))
	if err != nil {
		return nil, fail(stderr, []string{"-previous " + opts.Previous + ": " + err.Error()})
	}
	added, dropped := missingFrom(ids, previous), missingFrom(previous, ids)
	fmt.Fprintf(stdout, "newly proven: %d\n", len(added))
	for _, id := range added {
		fmt.Fprintf(stdout, "  %s\n", id)
	}
	fmt.Fprintf(stdout, "no longer proven: %d\n", len(dropped))
	for _, id := range dropped {
		fmt.Fprintf(stdout, "  %s: %s\n", id, whyNotProven(id, known, fromRun, fromEarlier))
	}
	return ids, exitOK
}

// heldByTheBranch names the record and the badge when the tree holds them: the pipeline writes both onto the branch coverage and
// main holds neither (Q-ARCH-811), so a change that adds or edits either is refused, whoever made it.
func heldByTheBranch(repo string) []string {
	var errs []string
	for _, rel := range []string{recordRel, badgeRel} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); err == nil {
			errs = append(errs, fmt.Sprintf("%s is in the tree: the pipeline writes the evidence record and the coverage badge onto the "+
				"branch %s, and main holds neither (Q-ARCH-811); remove the file", rel, recordBranch))
		}
	}
	return errs
}

// missingFrom returns the IDs of a that b does not hold, in the order of a.
func missingFrom(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, id := range b {
		in[id] = true
	}
	var out []string
	for _, id := range a {
		if !in[id] {
			out = append(out, id)
		}
	}
	return out
}

func printSummary(w io.Writer, m *model, c *corpus) {
	var parts []string
	for _, p := range priorities() {
		parts = append(parts, fmt.Sprintf("%s %d", p, m.countAll(m.Features, p)))
	}
	fmt.Fprintf(w, "catalog: %d pages, %d features (%s)\n", len(m.Pages), len(m.Features), strings.Join(parts, ", "))
	rp := map[string]int{}
	pages := map[string]bool{}
	refs := 0
	for _, r := range m.Reqs {
		rp[r.Priority]++
		pages[r.Page] = true
		refs += len(r.Covers)
	}
	var rparts []string
	for _, p := range priorities() {
		if rp[p] > 0 {
			rparts = append(rparts, fmt.Sprintf("%s %d", p, rp[p]))
		}
	}
	only := len(m.quaderOnlyReqs())
	fmt.Fprintf(w, "spec: %d pages, %d requirements (%s); %d cover catalog features, %d %s; %d Covers references\n",
		len(pages), len(m.Reqs), strings.Join(rparts, ", "), len(m.Reqs)-only, only, quaderOnly, refs)
	var cov []string
	for _, p := range priorities() {
		cov = append(cov, fmt.Sprintf("%s %d/%d", p, m.countSpecified(m.Features, p), m.countAll(m.Features, p)))
	}
	fmt.Fprintf(w, "coverage: %d specified, %d open; %s\n", m.countSpecified(m.Features), m.countOpen(m.Features), strings.Join(cov, "; "))
	if m.record {
		fmt.Fprintf(w, "implemented: %d of %d requirements, %d of %d features\n",
			m.implementedReqs(), len(m.Reqs), m.countImplemented(m.Features), len(m.Features))
	} else {
		fmt.Fprintf(w, "implemented: not rendered here; the pipeline's record is on the branch %s\n", recordBranch)
	}
	fmt.Fprintf(w, "headings skipped inside code fences: %d\n", len(c.Fenced))
	for _, l := range c.Fenced {
		fmt.Fprintf(w, "  %s\n", l)
	}
}
