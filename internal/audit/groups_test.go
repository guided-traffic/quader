package audit

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// R-OPS-056: the statements of the rows (audit.sql) and the functions and the index of migration 00144 name exactly the grouped
// actions of GroupedEvents, and the act of audit_row_key joins the actions of a push and of a delete as actActions does. A list that differs
// would leave an act's events out of the index the statements probe, or join actions the screen names apart. Unit tier, milliseconds,
// reads two files of the repository.
func TestR_OPS_056_TheStatementsNameTheGroupedActions(t *testing.T) {
	want := slices.Sorted(slices.Values(GroupedEvents()))
	list := regexp.MustCompile(`(?s)action (?:NOT )?IN \(([^)]*)\)`)
	names := func(in string) []string {
		var out []string
		for _, f := range strings.Split(in, ",") {
			out = append(out, strings.Trim(strings.TrimSpace(f), "'"))
		}
		slices.Sort(out)
		return out
	}
	sql, err := os.ReadFile("../meta/postgres/queries/audit.sql")
	if err != nil {
		t.Fatal(err)
	}
	rowsPart := string(sql[strings.Index(string(sql), "-- name: ListAuditRowsNewest"):])
	found := list.FindAllStringSubmatch(rowsPart, -1)
	if len(found) < 6 {
		t.Fatalf("the statements of the rows name %d lists of actions", len(found))
	}
	for _, m := range found {
		if got := names(m[1]); !slices.Equal(got, want) {
			t.Errorf("a statement of the rows names %v, GroupedEvents %v", got, want)
		}
	}
	mig, err := os.ReadFile("../meta/postgres/migrations/00144_audit_change_rows.sql")
	if err != nil {
		t.Fatal(err)
	}
	idx := string(mig[strings.Index(string(mig), "CREATE INDEX audit_event_row_idx"):])
	if m := list.FindStringSubmatch(idx); m == nil || !slices.Equal(names(m[1]), want) {
		t.Errorf("the index audit_event_row_idx names %v, GroupedEvents %v", m, want)
	}
	act := regexp.MustCompile(`WHEN action IN \(([^)]*)\) THEN '(\w+)'`)
	acts := act.FindAllStringSubmatch(string(mig), -1)
	if len(acts) != 2 {
		t.Fatalf("audit_row_key names %d acts of several actions, want 2", len(acts))
	}
	for _, m := range acts {
		if got, w := names(m[1]), slices.Sorted(slices.Values(actActions[m[2]])); !slices.Equal(got, w) {
			t.Errorf("audit_row_key's act %s is %v, actActions' %v", m[2], got, w)
		}
	}
}

// R-OPS-056: a row of several events names its act and its unit: a push of an image of tiny layers is one row of a push named by its
// tag, its size the sum of its files' and not of the version's event; an npm publish is a publish; files without a version are an
// upload; a delete of a package of three versions names three; the allowed writes have no size. Unit tier, milliseconds.
func TestR_OPS_056_AGroupNamesItsActAndItsUnit(t *testing.T) {
	t0 := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	n := 0
	member := func(action string, d map[string]any) q.ListAuditGroupMembersRow {
		n++
		raw, _ := json.Marshal(d)
		return q.ListAuditGroupMembersRow{
			ID: string(rune('a' + n)), OccurredAt: t0.Add(time.Duration(n) * time.Second), Action: action,
			Path: "", Details: raw, Replica: "quader-packages-a",
		}
	}
	head := func(m q.ListAuditGroupMembersRow) Entry {
		return Entry{ID: m.ID, OccurredAt: m.OccurredAt, Action: m.Action, Details: m.Details}
	}
	sha := strings.Repeat("ab", 32)

	push := []q.ListAuditGroupMembersRow{
		member(EventArtifactDeployed, map[string]any{"path": "acme/app/_blobs/sha256:" + sha, "sha256": sha, "size": 100, "package": "acme/app", "unit": "acme/app"}),
		member(EventArtifactDeployed, map[string]any{"path": "acme/app/_blobs/sha256:" + sha, "sha256": sha, "size": 200, "package": "acme/app", "unit": "acme/app"}),
		member(EventArtifactDeployed, map[string]any{"path": "acme/app/_tags/1.4.0", "sha256": sha, "size": 30, "package": "acme/app", "version": "1.4.0", "unit": "acme/app"}),
		member(EventPackagePublished, map[string]any{
			"package": "acme/app", "version": "1.4.0", "format": "oci", "unit": "acme/app",
			"files": []map[string]any{{"path": "acme/app", "sha256": sha, "size": 30}},
		}),
	}
	g := groupOf(head(push[0]), push)
	if g.Act != ActPush || g.Unit != "acme/app" || g.Package != "acme/app" || g.Version != "1.4.0" || g.Versions != 1 || g.Items != 4 ||
		!g.SizeKnown || g.Size != 330 || len(g.First) != 4 || g.First[3].SHA256 != sha || g.First[3].Action != EventPackagePublished {
		t.Errorf("a push: %+v", g)
	}

	publish := []q.ListAuditGroupMembersRow{
		member(EventArtifactDeployed, map[string]any{"path": "@acme/ui/-/ui-2.4.0.tgz", "sha256": sha, "size": 4821, "package": "@acme/ui", "version": "2.4.0", "unit": "@acme/ui@2.4.0"}),
		member(EventNPMPublish, map[string]any{"package": "@acme/ui", "version": "2.4.0", "integrity": "sha512-x", "tag": "latest", "unit": "@acme/ui@2.4.0"}),
	}
	if g := groupOf(head(publish[0]), publish); g.Act != ActPublish || g.Size != 4821 || g.Version != "2.4.0" {
		t.Errorf("an npm publish: %+v", g)
	}

	files := []q.ListAuditGroupMembersRow{
		member(EventArtifactDeployed, map[string]any{"path": "docs/a.txt", "sha256": sha, "size": 3, "unit": "docs/"}),
		member(EventArtifactDeployed, map[string]any{"path": "docs/b.txt", "sha256": sha, "size": 4, "unit": "docs/"}),
	}
	if g := groupOf(head(files[0]), files); g.Act != ActUpload || g.Unit != "docs/" || g.Package != "" || g.Size != 7 || g.Versions != 0 {
		t.Errorf("an upload of files: %+v", g)
	}

	var del []q.ListAuditGroupMembersRow
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		del = append(del,
			member(EventArtifactDeleted, map[string]any{"path": "left-pad/-/left-pad-" + v + ".tgz", "sha256": sha, "size": 10, "package": "left-pad", "version": v, "unit": "left-pad"}),
			member(EventNPMUnpublish, map[string]any{"package": "left-pad", "version": v, "integrity": "sha512-x", "unit": "left-pad"}))
	}
	if g := groupOf(head(del[0]), del); g.Act != ActDelete || g.Versions != 3 || g.Version != "1.0.0" || g.Size != 30 || g.Items != 6 {
		t.Errorf("a delete of a package of three versions: %+v", g)
	}

	manifest := []q.ListAuditGroupMembersRow{
		member(EventOCIManifestDeleted, map[string]any{"name": "acme/app", "digest": "sha256:" + sha, "deleted": 3, "unit": "acme/app"}),
	}
	if g := groupOf(head(manifest[0]), manifest); g.Act != ActDelete || g.Package != "acme/app" || g.SizeKnown || g.First[0].SHA256 != sha {
		t.Errorf("a manifest's delete names the image and the manifest's digest, and no size: %+v", g)
	}

	writes := []q.ListAuditGroupMembersRow{
		member("authz.write.allowed", map[string]any{"package": "acme/app", "path": "acme/app/_blobs/x", "unit": "acme/app"}),
		member("authz.write.allowed", map[string]any{"package": "acme/app", "path": "acme/app/_blobs/y", "unit": "acme/app"}),
	}
	if g := groupOf(head(writes[0]), writes); g.Act != ActWrites || g.SizeKnown || g.Items != 2 {
		t.Errorf("the allowed writes: %+v", g)
	}
	if ActOf("authz.read.allowed") != "" || ActOf("malware.download_blocked") != "" || ActOf("authz.denied") != "" {
		t.Error("an allowed read and a refusal are no part of a row of several events (Q-OPS-442)")
	}
}
