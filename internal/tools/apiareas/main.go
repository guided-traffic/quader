// Command apiareas names, for a change of the management API's answers, the integration areas whose tests name a changed member
// (Q-ARCH-703). It compares the schemas of api/openapi.yaml at a base revision with the working tree's, takes every property a schema
// gained, lost or changed, and lists the areas under test/integration whose test files name it as a quoted string (a JSON key the
// test reads or asserts absent). A landing runs those areas beside the ones of its subject:
//
//	go run ./internal/tools/apiareas -base main
//	go run ./internal/tools/apiareas -base 6b8eae484~1 -head 6b8eae484   # a change that landed
//
// It prints one line per changed member, "<Schema>.<member> (<how>): <areas>", and a last line "areas: <package paths>" for
// test/integration/run.sh. A member with a common name (id, name) is named by many areas; the line says how many, and the landing
// decides. What it cannot see: a format's own routes (their answers are no schema of the document), a test that builds the key from
// parts, and an error code (a value, not a member). It is a developer tool, not part of the product, and runs in about a second.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

func main() {
	base := flag.String("base", "main", "the git revision to compare against")
	head := flag.String("head", "", "a git revision to compare instead of the working tree")
	spec := flag.String("spec", "api/openapi.yaml", "the OpenAPI document, relative to the repository's root")
	tests := flag.String("tests", "test/integration", "the tree of the integration areas")
	flag.Parse()
	if err := run(*base, *head, *spec, *tests, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "apiareas:", err)
		os.Exit(1)
	}
}

func run(base, head, spec, tests string, out *os.File) error {
	old, err := show(base, spec)
	if err != nil {
		return err
	}
	cur, err := os.ReadFile(spec) //nolint:gosec // G304: a developer tool reads the path it is given
	if head != "" {
		cur, err = show(head, spec)
	}
	if err != nil {
		return err
	}
	changes, err := diff(old, cur)
	if err != nil {
		return err
	}
	idx, err := index(tests)
	if err != nil {
		return err
	}
	report(out, changes, idx, tests)
	return nil
}

// show is the file at path in the git revision rev.
func show(rev, path string) ([]byte, error) {
	b, err := exec.CommandContext(context.Background(), "git", "show", rev+":"+path).Output() //nolint:gosec // G204: a developer tool; the revision is the caller's
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %w", rev, path, err)
	}
	return b, nil
}

// change is a property of a schema that a revision added, removed or changed.
type change struct {
	Schema, Member, How string
}

// members returns every property of every schema of the document by "<Schema>.<member>", with its definition as canonical JSON.
func members(doc []byte) (map[string]string, error) {
	var d struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(doc, &d); err != nil {
		return nil, err
	}
	if len(d.Components.Schemas) == 0 {
		return nil, errors.New("the document has no schemas")
	}
	out := map[string]string{}
	for s, def := range d.Components.Schemas {
		for p, v := range def.Properties {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", s, p, err)
			}
			out[s+"."+p] = string(b)
		}
	}
	return out, nil
}

// diff lists the members old and cur differ in, sorted.
func diff(old, cur []byte) ([]change, error) {
	a, err := members(old)
	if err != nil {
		return nil, fmt.Errorf("the base's document: %w", err)
	}
	b, err := members(cur)
	if err != nil {
		return nil, fmt.Errorf("the working tree's document: %w", err)
	}
	var out []change
	add := func(key, how string) {
		s, m, _ := strings.Cut(key, ".")
		out = append(out, change{Schema: s, Member: m, How: how})
	}
	for k, v := range b {
		switch w, ok := a[k]; {
		case !ok:
			add(k, "added")
		case w != v:
			add(k, "changed")
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			add(k, "removed")
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Schema != out[j].Schema {
			return out[i].Schema < out[j].Schema
		}
		return out[i].Member < out[j].Member
	})
	return out, nil
}

// index reads every test file under tests and returns its contents by area (the first directory below tests).
func index(tests string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(tests, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		rel, err := filepath.Rel(tests, p)
		if err != nil {
			return err
		}
		area, _, ok := strings.Cut(filepath.ToSlash(rel), "/")
		if !ok {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: the walk's own paths
		if err != nil {
			return err
		}
		out[area] = append(append(out[area], b...), '\n')
		return nil
	})
	return out, err
}

// areasNaming lists the areas whose tests name member as a quoted string, sorted.
func areasNaming(idx map[string][]byte, member string) []string {
	quoted := []byte(`"` + member + `"`)
	var out []string
	for area, b := range idx {
		if bytes.Contains(b, quoted) {
			out = append(out, area)
		}
	}
	sort.Strings(out)
	return out
}

func report(w *os.File, changes []change, idx map[string][]byte, tests string) {
	if len(changes) == 0 {
		_, _ = fmt.Fprintln(w, "no member of a schema changed")
		return
	}
	all := map[string]bool{}
	for _, c := range changes {
		areas := areasNaming(idx, c.Member)
		for _, a := range areas {
			all[a] = true
		}
		names := strings.Join(areas, " ")
		if len(areas) == 0 {
			names = "no area names it"
		}
		_, _ = fmt.Fprintf(w, "%s.%s (%s, %d areas): %s\n", c.Schema, c.Member, c.How, len(areas), names)
	}
	var paths []string
	for a := range all {
		paths = append(paths, "./"+filepath.ToSlash(filepath.Join(tests, a))+"/")
	}
	sort.Strings(paths)
	_, _ = fmt.Fprintf(w, "areas: %s\n", strings.Join(paths, " "))
}
