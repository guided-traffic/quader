package v1

import (
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestRepositoryRepKeepsTheServerMembers: the representation of a repository carries the members the server adds beside the document,
// whose own MarshalJSON leaves a remote's include and exclude patterns out (remote-curation.md R-CUR-002) and would otherwise be promoted
// to the representation and drop them.
func TestRepositoryRepKeepsTheServerMembers(t *testing.T) {
	for _, class := range []repository.Class{repository.Remote, repository.Local} {
		st := repository.Stored{
			Repository: repository.Repository{Name: qname.Repo{Project: "web", Key: "r"}, Lifecycle: repository.Active},
			Document: repository.Document{Project: "web", Key: "r", Class: class, Format: "npm", Settings: repository.Settings{
				IncludePatterns: []string{}, ExcludePatterns: []string{},
			}},
			Warnings: []repository.Warning{{Code: "member_inactive", Detail: "x"}},
		}
		raw, err := json.Marshal(toRep(st, true, true))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v\n%s", class, err, raw)
		}
		for _, k := range []string{"project", "key", "lifecycle", "createdAt", "updatedAt", "lastActivityAt", "warnings"} {
			if _, ok := m[k]; !ok {
				t.Errorf("%s: no member %s in %s", class, k, raw)
			}
		}
		_, patterns := m["includePatterns"]
		if patterns != (class != repository.Remote) {
			t.Errorf("%s: includePatterns present %v in %s", class, patterns, raw)
		}
		if string(m["project"]) != `"web"` {
			t.Errorf("%s: project %s", class, m["project"])
		}
	}
}
