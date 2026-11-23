package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decisionsFixture loads the fixture tree "decisions" and returns its questions and the text of its decisions.md.
func decisionsFixture(t *testing.T) ([]question, string) {
	t.Helper()
	c := mustLoad(t, fixtureDocs("decisions"))
	if len(c.Errors) > 0 {
		t.Fatalf("fixture decisions: %v", c.Errors)
	}
	data, err := os.ReadFile(filepath.Join(fixtureDocs("decisions"), "decisions.md"))
	if err != nil {
		t.Fatal(err)
	}
	return c.Questions, string(data)
}

func mustRenderDecisions(t *testing.T, qs []question, text string) string {
	t.Helper()
	out, errs := renderDecisions(qs, text)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	return out
}

func TestDecisionsIndex(t *testing.T) {
	qs, text := decisionsFixture(t)
	out := mustRenderDecisions(t, qs, text)
	rows := indexRows(out)

	t.Run("E0.8 one row per question block, none for a heading inside a fence", func(t *testing.T) {
		var ids []string
		for _, l := range strings.Split(out, "\n") {
			if m := indexRowID.FindStringSubmatch(l); m != nil {
				ids = append(ids, m[1])
			}
		}
		if got, want := strings.Join(ids, " "), "Q-GAD-1 Q-GAD-3 Q-WID-2 Q-WID-3 Q-WID-10"; got != want {
			t.Fatalf("rows: got %q, want %q (by area, then by number)", got, want)
		}
	})
	t.Run("E0.8 a row carries the link, the title, the status, the option a letter names and the ADR that names the question",
		func(t *testing.T) {
			want := "| [Q-WID-2](spec/widgets.md#q-wid-2--widgets-come-in-pairs-of-ab) | Widgets come in `pairs` of a\\|b | " +
				"Decided by the owner on 2026-11-20 | (b) Two widgets, `a\\|b` | [ADR-001](#adr-001--widgets-come-in-pairs) |"
			if rows["Q-WID-2"] != want {
				t.Errorf("row:\n got %s\nwant %s", rows["Q-WID-2"], want)
			}
		})
	t.Run("E0.8 the choice is the first sentence of the choice, with its links rebased to docs and the ADR it names", func(t *testing.T) {
		want := "| [Q-GAD-1](spec/formats/gadgets.md#q-gad-1--gadgets-reuse-widgets) | Gadgets reuse widgets | " +
			"Decided by the development agent | (a) — a gadget is built from the widgets of " +
			"[R-WID-001](spec/widgets.md#r-wid-001--make-widgets), as [ADR-002](#adr-002--gadgets-are-built-from-widgets) records. | " +
			"[ADR-002](#adr-002--gadgets-are-built-from-widgets) |"
		if rows["Q-GAD-1"] != want {
			t.Errorf("row:\n got %s\nwant %s", rows["Q-GAD-1"], want)
		}
	})
	t.Run("E0.8 an option the status line names, a superseded question and a block without a choice", func(t *testing.T) {
		if !strings.Contains(rows["Q-WID-3"], "| Decided by the owner on 2026-11-21 | (a) Blue | — |") {
			t.Errorf("Q-WID-3: %s", rows["Q-WID-3"])
		}
		if !strings.Contains(rows["Q-WID-10"],
			"| Superseded by [Q-WID-2](spec/widgets.md#q-wid-2--widgets-come-in-pairs-of-ab), which states the rule once | — | — |") {
			t.Errorf("Q-WID-10: %s", rows["Q-WID-10"])
		}
		if !strings.Contains(rows["Q-GAD-3"], "| Decided by the builder | — | — |") {
			t.Errorf("Q-GAD-3: %s", rows["Q-GAD-3"])
		}
	})
	t.Run("E0.8 an area names its pages and its count", func(t *testing.T) {
		if !strings.Contains(out, "### Q-GAD\n\n2 questions on [spec/formats/gadgets.md](spec/formats/gadgets.md).\n") {
			t.Errorf("no section for Q-GAD:\n%s", out)
		}
	})
	t.Run("E0.8 the hand-written parts stay and a second run gives the same bytes", func(t *testing.T) {
		head, _, _ := strings.Cut(text, "<!-- coverage-gen:begin decisions -->")
		_, tail, _ := strings.Cut(text, "<!-- coverage-gen:end decisions -->")
		if !strings.HasPrefix(out, head) || !strings.HasSuffix(out, tail) {
			t.Errorf("the text around the markers changed:\n%s", out)
		}
		if again := mustRenderDecisions(t, qs, out); again != out {
			t.Errorf("a second run changed the page:\n%s", again)
		}
	})
	t.Run("E0.8 a row outside the generated part and a missing marker are errors", func(t *testing.T) {
		stray := out + "\n" + rows["Q-WID-2"] + "\n"
		if _, errs := renderDecisions(qs, stray); len(errs) != 1 || !strings.Contains(errs[0], "stands outside the generated part") {
			t.Errorf("errors: %v", errs)
		}
		if _, errs := renderDecisions(qs, strings.Replace(text, "<!-- coverage-gen:end decisions -->", "", 1)); len(errs) != 1 ||
			!strings.Contains(errs[0], `marker pair for block "decisions"`) {
			t.Errorf("errors: %v", errs)
		}
	})
}

func TestDecisionsCheck(t *testing.T) {
	qs, text := decisionsFixture(t)
	good := mustRenderDecisions(t, qs, text)
	missing := strings.Replace(good, indexRows(good)["Q-WID-3"]+"\n", "", 1)
	extra := strings.Replace(good, indexRows(good)["Q-WID-3"], indexRows(good)["Q-WID-3"]+"\n"+
		strings.Replace(indexRows(good)["Q-WID-3"], "Q-WID-3", "Q-WID-4", 1), 1)
	retitled := strings.Replace(good, "| Widgets are blue |", "| Widgets were blue |", 1)

	cases := []struct{ name, committed, want string }{
		{
			"E0.8 -check names a block without a row and where it stands", missing,
			"docs/decisions.md: Q-WID-3 (docs/spec/widgets.md:24) has no row in the index of decided questions",
		},
		{
			"E0.8 -check names a row without a block", extra,
			"docs/decisions.md: the index of decided questions has a row for Q-WID-4, which no page defines",
		},
		{
			"E0.8 -check names a row whose block changed", retitled,
			"docs/decisions.md: the row of Q-WID-3 no longer matches its block (docs/spec/widgets.md:24)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(indexDiff(qs, tc.committed, good), "\n")
			if !strings.Contains(got, tc.want) || !strings.Contains(got, "run `go run ./internal/tools/coverage`") {
				t.Errorf("got:\n%s\nwant a line %q and the command", got, tc.want)
			}
		})
	}
	t.Run("E0.8 -check passes on what the generator wrote", func(t *testing.T) {
		if d := indexDiff(qs, good, good); len(d) != 0 {
			t.Errorf("differences: %v", d)
		}
	})
}

func TestDecisionsIndexOfTheRealDocs(t *testing.T) {
	root := repoRoot(t)
	t.Run("E0.8 -check fails with the block's name when the committed index lacks its row", func(t *testing.T) {
		repo := t.TempDir()
		copyTree(t, filepath.Join(root, "docs"), filepath.Join(repo, "docs"))
		path := filepath.Join(repo, "docs", "decisions.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		row := indexRows(string(data))["Q-ARCH-1"]
		if row == "" {
			t.Fatal("the committed index has no row for Q-ARCH-1")
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(data), row+"\n", "", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", repo, "-check"}, strings.NewReader(""), &stdout, &stderr); code != exitStale {
			t.Fatalf("exit %d, want %d:\n%s", code, exitStale, stderr.String())
		}
		if !strings.Contains(stderr.String(), "docs/decisions.md: Q-ARCH-1 (docs/spec/architecture.md:") {
			t.Errorf("stderr does not name Q-ARCH-1:\n%s", stderr.String())
		}
		if code := run([]string{"-repo", repo}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
			t.Fatalf("regenerate: exit %d:\n%s", code, stderr.String())
		}
		if code := run([]string{"-repo", repo, "-check"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
			t.Fatalf("-check after the run: exit %d:\n%s", code, stderr.String())
		}
	})
}
