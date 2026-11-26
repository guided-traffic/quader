package ansible

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
)

// warningsOf are the messages of level WARNING of a task that are not the generator's.
func warningsOf(task map[string]any) []string {
	var out []string
	for _, m := range task["messages"].([]any) {
		msg := m.(map[string]any)
		if text, _ := msg["message"].(string); msg["level"] == "WARNING" && !strings.HasPrefix(text, "documentation: ") {
			out = append(out, text)
		}
	}
	return out
}

// TestR_ANS_062_TheImportWarnsOfAMissingChangelogReadmeRequiresAnsibleAndLicence: an archive without a changelog, a README, a
// requires_ansible or a licence is published, and its import task has a message of level WARNING for each, as Galaxy's importer warns;
// a changelog in docs/, a license_file and a meta/runtime.yml without the key are told apart, and an unreadable meta/runtime.yml keeps the
// reader's own warning alone. Unit (the format against its in-memory port), a few milliseconds, archives under a kilobyte.
func TestR_ANS_062_TheImportWarnsOfAMissingChangelogReadmeRequiresAnsibleAndLicence(t *testing.T) {
	cases := []struct {
		name string
		edit func(s *collectiontest.Spec)
		want []string
	}{
		{"a complete collection", func(s *collectiontest.Spec) { s.Files["changelogs/changelog.yaml"] = testChangelog }, nil},
		{"none of the four", func(s *collectiontest.Spec) {
			delete(s.Files, "README.md")
			delete(s.Files, "meta/runtime.yml")
			s.Info = map[string]any{"license": []string{}, "license_file": nil}
		}, []string{warnNoChangelog, warnNoReadme, warnNoRuntime, warnNoLicence}},
		{"a changelog in docs/ and a licence file", func(s *collectiontest.Spec) {
			s.Files["docs/CHANGELOG.md"] = "# Changelog\n"
			s.Files["LICENSE"] = "MIT\n"
			s.Info = map[string]any{"license": []string{}, "license_file": "LICENSE"}
		}, nil},
		{"a runtime without requires_ansible", func(s *collectiontest.Spec) {
			s.Files["CHANGELOG.rst"] = "Changelog\n"
			s.Files["meta/runtime.yml"] = "---\nplugin_routing: {}\n"
		}, []string{warnNoRequiresAnsible}},
		{"an unreadable runtime", func(s *collectiontest.Spec) {
			s.Files["CHANGELOG.md"] = "# Changelog\n"
			s.Files["meta/runtime.yml"] = "requires_ansible: [unclosed\n"
		}, nil},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHost(t)
			s := collectionSpec("1.0." + string(rune('0'+i)))
			c.edit(&s)
			r, err := publish(h, collectiontest.Build(s), true, "")
			if err != nil || r.Code != http.StatusAccepted {
				t.Fatalf("publish: %v %v", r, err)
			}
			task := taskOf(t, h, r)
			got := warningsOf(task)
			if c.name == "an unreadable runtime" {
				// The reader's warning of the ignored file, and none of the import's.
				if task["state"] != "completed" || len(got) != 1 || !strings.Contains(got[0], "requires_ansible is left empty") {
					t.Errorf("state %v, warnings %q", task["state"], got)
				}
				return
			}
			if task["state"] != "completed" || !reflect.DeepEqual(got, c.want) {
				t.Errorf("state %v, warnings %q, want %q", task["state"], got, c.want)
			}
		})
	}
}
