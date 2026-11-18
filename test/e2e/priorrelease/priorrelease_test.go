package priorrelease

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const index = `apiVersion: v1
entries:
  quader:
  - name: quader
    version: 0.62.0
  - name: quader
    version: 0.61.0
  - name: quader
    version: 0.56.0
  quader-bastion:
  - name: quader-bastion
    version: 0.63.0
generated: "2026-11-18T21:15:00Z"
`

func TestListedReadsTheVersionsOfOneChart(t *testing.T) {
	got, err := Listed([]byte(index), "quader")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !got["0.62.0"] || !got["0.56.0"] || got["0.63.0"] {
		t.Errorf("Listed = %v, want 0.62.0, 0.61.0 and 0.56.0 and not the other chart's 0.63.0", got)
	}
	if _, err := Listed([]byte("<html>404</html>"), "quader"); err == nil {
		t.Error("a page that is no repository index was read as one")
	}
	if got, err := Listed([]byte("apiVersion: v1\nentries:\n  other: []\n"), "quader"); err != nil || len(got) != 0 {
		t.Errorf("an index without the chart: %v, %v; want no version and no error", got, err)
	}
}

func TestR_HA_020_PickTakesTheNewestPublishedReleaseBeforeTheCommit(t *testing.T) {
	listed := map[string]bool{"0.56.0": true, "0.61.0": true, "0.62.0": true}
	for name, tc := range map[string]struct {
		tags, atHead []string
		want         string
		passed       []string
	}{
		"the newest listed ancestor": {
			tags: []string{"v0.56.0", "v0.62.0", "v0.61.0", "v0.16.0"}, want: "v0.62.0",
		},
		"versions sort by number, not by text": {
			tags: []string{"v0.9.0", "v0.56.0", "v0.10.0"}, want: "v0.56.0",
			passed: nil,
		},
		"a release cut from the commit itself is not its own start": {
			tags: []string{"v0.62.0", "v0.61.0", "v0.63.0"}, atHead: []string{"v0.63.0"}, want: "v0.62.0",
			passed: []string{"v0.63.0 (points at the commit under test)"},
		},
		"a release whose chart is not published yet is passed over": {
			tags: []string{"v0.62.0", "v0.63.0"}, want: "v0.62.0",
			passed: []string{"v0.63.0 (the chart repository does not list it)"},
		},
		"the releases of the former name are not listed": {
			tags: []string{"v0.16.0", "v0.55.3"}, want: "",
			passed: []string{"v0.55.3 (the chart repository does not list it)", "v0.16.0 (the chart repository does not list it)"},
		},
		"tags that are no release are ignored": {
			tags: []string{"v0.61.0", "v0.62.0-rc.1", "nightly", "0.62.0"}, want: "v0.61.0",
		},
		"no tag at all": {want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, passed := Pick(tc.tags, tc.atHead, listed)
			if got != tc.want || !slices.Equal(passed, tc.passed) {
				t.Errorf("Pick = %q, passed %q; want %q, passed %q", got, passed, tc.want, tc.passed)
			}
		})
	}
}

func TestR_HA_021_BreaksNamesWhatTheUpgradeCannotCross(t *testing.T) {
	contract := func(path string) (bool, error) { return strings.Contains(path, "contract"), nil }
	diff := strings.Join([]string{
		"A\tinternal/meta/postgres/migrations/00079_add.sql",
		"A\tinternal/meta/postgres/migrations/00080_contract_drop.sql",
		"M\tinternal/meta/postgres/migrations/00012_edited.sql",
		"D\tinternal/meta/postgres/migrations/00013_gone.sql",
		"R100\tinternal/meta/postgres/migrations/00014_old.sql\tinternal/meta/postgres/migrations/00014_new.sql",
		"",
	}, "\n")
	got, err := Breaks(diff, contract)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"00080_contract_drop.sql is a contract step added since the release",
		"00012_edited.sql was changed after the release applied it",
		"00013_gone.sql was removed after the release applied it",
		"00014_old.sql was renamed to 00014_new.sql after the release applied it",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Breaks =\n%q\nwant\n%q", got, want)
	}

	if got, err := Breaks("A\tinternal/meta/postgres/migrations/00079_add.sql\n", contract); err != nil || len(got) != 0 {
		t.Errorf("an added expand step: %q, %v; want no break", got, err)
	}
	if got, err := Breaks("", contract); err != nil || len(got) != 0 {
		t.Errorf("no change: %q, %v", got, err)
	}
	if _, err := Breaks("X\tsomething.sql\n", contract); err == nil {
		t.Error("an unknown status was accepted")
	}
	if _, err := Breaks("not a name-status line\n", contract); err == nil {
		t.Error("a line without a tab was accepted")
	}
	boom := errors.New("unreadable")
	if _, err := Breaks("A\tx.sql\n", func(string) (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Errorf("a file that cannot be read: %v, want %v", err, boom)
	}
}
