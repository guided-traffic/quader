package v1

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The README's table of the management API (section "Management API", the block "Every operation with its permission") is held to
// api/openapi.yaml (R-API-008): it claimed every operation and missed 154 of them on 2026-11-26. A row is written by hand, so it can
// say more than the document declares (filters, the answer's codes, the body); what the document does declare is checked here: each
// operation has exactly one row, under the family of its first tag, with its operationId; the permission column begins with the
// declaration as readmePermission words it; it carries " · recent sign-in" exactly when the operation declares
// x-quader-recent-auth, and "(`x-quader-owner-rights`)" exactly when it declares x-quader-owner-rights; and the families stand in
// the order of the document's tags.

// readmePermission words a declaration as the README's permission column begins.
func readmePermission(q Requirement) string {
	switch {
	case q.Public:
		return "public"
	case q.Bastion:
		return "a recognized Bastion (the Bastion interface)"
	case q.Authenticated && q.PublicSwitch != "":
		return "authenticated, public with `" + q.PublicSwitch + "`"
	case q.Authenticated:
		return "authenticated"
	case q.Permission != "" && q.ProjectRole != "":
		who := map[string]string{"owner": "Owner", "reader": "a member"}[q.ProjectRole]
		if q.Project == "" {
			who = map[string]string{"owner": "an Owner", "reader": "a member"}[q.ProjectRole]
			return "`" + q.Permission + "` or " + who + " of any project"
		}
		return "`" + q.Permission + "` or " + who + " of the project"
	case q.Permission != "":
		return "`" + q.Permission + "`"
	case q.Role != "":
		return "role `" + q.Role + "`"
	case q.Action != "":
		return "action `" + string(q.Action) + "` on `" + q.Project + "`/`" + q.Repository + "`"
	}
	return q.String()
}

type readmeOperation struct {
	key, id, family string
	permission      string
	recent, owner   bool
}

// documentOperations reads every operation of the document in its order, and the order of its tags.
func documentOperations(t *testing.T) ([]readmeOperation, []string) {
	t.Helper()
	var doc struct {
		Tags  []struct{ Name string } `yaml:"tags"`
		Paths yaml.Node               `yaml:"paths"`
	}
	if err := yaml.Unmarshal(specYAML, &doc); err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, tag := range doc.Tags {
		tags = append(tags, tag.Name)
	}
	var out []readmeOperation
	for i := 0; i+1 < len(doc.Paths.Content); i += 2 {
		path, item := doc.Paths.Content[i].Value, doc.Paths.Content[i+1]
		for j := 0; j+1 < len(item.Content); j += 2 {
			method := item.Content[j].Value
			if !slices.Contains(httpMethods(), method) {
				continue
			}
			var op map[string]any
			if err := item.Content[j+1].Decode(&op); err != nil {
				t.Fatal(err)
			}
			key := strings.ToUpper(method) + " " + path
			perm, _ := op["x-quader-permission"].(map[string]any)
			req, err := parseRequirement(perm)
			if err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			opTags, _ := op["tags"].([]any)
			if len(opTags) == 0 {
				t.Fatalf("%s has no tag", key)
			}
			_, owner := op["x-quader-owner-rights"]
			out = append(out, readmeOperation{
				key: key, id: fmt.Sprint(op["operationId"]), family: fmt.Sprint(opTags[0]),
				permission: readmePermission(req), recent: op["x-quader-recent-auth"] == true, owner: owner,
			})
		}
	}
	return out, tags
}

var (
	readmeFamilyRE = regexp.MustCompile(`^\*\*([a-z-]+)\*\*`)
	readmeRowRE    = regexp.MustCompile("^\\| `([A-Z]+ /[^`]*)` \\| `([^`]*)` \\| (.*) \\|$")
)

// readmeRows reads the rows of the README's block of every operation, each with the family it stands under, and the families in
// their order.
func readmeRows(t *testing.T) (map[string][]readmeOperation, []string) {
	t.Helper()
	raw, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "<summary>Every operation with its permission")
	if start < 0 {
		t.Fatal("the README has no block of every operation")
	}
	end := strings.Index(text[start:], "</details>")
	if end < 0 {
		t.Fatal("the block of every operation does not end")
	}
	rows := map[string][]readmeOperation{}
	var families []string
	family := ""
	for i, line := range strings.Split(text[start:start+end], "\n") {
		if m := readmeFamilyRE.FindStringSubmatch(line); m != nil {
			family = m[1]
			families = append(families, family)
			continue
		}
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		m := readmeRowRE.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("README block line %d is not a row of one operation (`METHOD /path` | `operationId` | permission): %.120s", i+1, line)
			continue
		}
		rows[m[1]] = append(rows[m[1]], readmeOperation{
			key: m[1], id: m[2], family: family, permission: m[3],
			recent: strings.Contains(m[3], " · recent sign-in"), owner: strings.Contains(m[3], "(`x-quader-owner-rights`)"),
		})
	}
	return rows, families
}

func TestR_API_008_TheReadmeListsEveryOperationWithItsPermission(t *testing.T) {
	ops, tags := documentOperations(t)
	rows, families := readmeRows(t)
	if len(ops) < 400 {
		t.Fatalf("the document has %d operations: the reading of api/openapi.yaml is broken", len(ops))
	}
	inDocument := map[string]bool{}
	var missing []string
	for _, op := range ops {
		inDocument[op.key] = true
		got := rows[op.key]
		switch {
		case len(got) == 0:
			missing = append(missing, fmt.Sprintf("| `%s` | `%s` | %s |  (under **%s**)", op.key, op.id, op.permission, op.family))
			continue
		case len(got) > 1:
			t.Errorf("%s has %d rows", op.key, len(got))
		}
		row := got[0]
		if row.id != op.id {
			t.Errorf("%s: the row names `%s`, the document `%s`", op.key, row.id, op.id)
		}
		if row.family != op.family {
			t.Errorf("%s stands under **%s**, its first tag is %s", op.key, row.family, op.family)
		}
		if !strings.HasPrefix(row.permission, op.permission) {
			t.Errorf("%s: the permission column begins %.60q, the declaration is %q", op.key, row.permission, op.permission)
		}
		if row.recent != op.recent {
			t.Errorf("%s: the row says recent sign-in %v, the document x-quader-recent-auth %v", op.key, row.recent, op.recent)
		}
		if row.owner != op.owner {
			t.Errorf("%s: the row names x-quader-owner-rights %v, the document %v", op.key, row.owner, op.owner)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d operations of api/openapi.yaml have no row in the README's table of the management API:\n%s", len(missing), strings.Join(missing, "\n"))
	}
	for key := range rows {
		if !inDocument[key] {
			t.Errorf("the README lists %s, which api/openapi.yaml does not declare", key)
		}
	}
	var order []string
	for _, tag := range tags {
		if slices.Contains(families, tag) {
			order = append(order, tag)
		}
	}
	if !slices.Equal(families, order) {
		t.Errorf("the README's families are %v, want them in the order of the document's tags: %v", families, order)
	}
}

// TestR_API_008_NoOtherReadmeTableListsAnOperation: a row of the management API stands only in the checked block. A second table
// beside it (the section "System API" had one) went without rows the document declares (GET /api/v1/system/storage-connections/{id},
// 2026-11-27) and said what the document does not (an anonymous read of the scanners), and no check saw it.
func TestR_API_008_NoOtherReadmeTableListsAnOperation(t *testing.T) {
	raw, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "<summary>Every operation with its permission")
	if start < 0 {
		t.Fatal("the README has no block of every operation")
	}
	end := strings.Index(text[start:], "</details>")
	if end < 0 {
		t.Fatal("the block of every operation does not end")
	}
	outside := text[:start] + text[start+end:]
	for _, m := range regexp.MustCompile("(?m)^\\| `[A-Z]+ /api/v1/[^`]*`").FindAllString(outside, -1) {
		t.Errorf("the README lists %s outside the block of every operation", m)
	}
}
