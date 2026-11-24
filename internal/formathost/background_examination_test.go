package formathost

import (
	"testing"

	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-CUR-016 (remote-curation.md, "What is examination"): a run of the download queue that stores a file of a version the rules do not
// deliver holds the version for examination with the files it stored or found; a delivered version, a run that stored nothing and a
// file of no version are none. Milliseconds, no data.
func TestR_CUR_016_ARunHoldsAnUndeliveredVersionForExamination(t *testing.T) {
	doc := `{"remote":{"url":"https://upstream.example.org/"}}`
	repo, err := repository.Build(7, 1, qr("names-remote"), repository.Remote, "names", []byte(doc), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo, err = repository.WithRules(repo, rc.OnlyAllowed, []rules.Rule{{Kind: rc.KindPackage, Name: "acme-a", Effect: rc.Allow}}, rules.Canon{})
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"acme-b/1.bin"}
	for _, tc := range []struct {
		what    string
		row     queue.Row
		stored  int
		examine bool
	}{
		{"a version the rules do not deliver, stored", queue.Row{Name: "acme-b", Version: "1"}, 1, true},
		{"a version the rules deliver", queue.Row{Name: "acme-a", Version: "1"}, 1, false},
		{"nothing stored: every file was there", queue.Row{Name: "acme-b", Version: "1"}, 0, false},
		{"a file of no version", queue.Row{Name: "acme-b", Version: ""}, 1, false},
	} {
		got := examinationOf(repo, tc.row, tc.stored, files)
		if (got != nil) != tc.examine {
			t.Errorf("%s: examination %+v, want %v", tc.what, got, tc.examine)
			continue
		}
		if got != nil && (got.Name != "acme-b" || got.Version != "1" || len(got.Files) != 1) {
			t.Errorf("%s: %+v", tc.what, got)
		}
	}
}
