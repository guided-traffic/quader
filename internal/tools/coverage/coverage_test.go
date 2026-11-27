package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot finds the repository root from the package directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := findRepo()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func fixtureDocs(name string) string { return filepath.Join("testdata", name, "docs") }

func mustLoad(t *testing.T, docs string) *corpus {
	t.Helper()
	c, err := load(docs)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func fixtureModel(t *testing.T, name string, implemented ...string) *model {
	t.Helper()
	c := mustLoad(t, fixtureDocs(name))
	if len(c.Errors) > 0 {
		t.Fatalf("fixture %s: %v", name, c.Errors)
	}
	set := map[string]bool{}
	for _, id := range implemented {
		set[id] = true
	}
	return newModel(c, pageOrder(filepath.Join(fixtureDocs(name), "features"), c.Pages), set)
}

func TestRealDocsReproduceTheMeasuredNumbers(t *testing.T) {
	c := mustLoad(t, filepath.Join(repoRoot(t), "docs"))
	if len(c.Errors) > 0 {
		t.Fatalf("the real docs violate the rules:\n%s", strings.Join(c.Errors, "\n"))
	}
	m := newModel(c, pageOrder(filepath.Join(repoRoot(t), "docs", "features"), c.Pages), map[string]bool{})

	// Catalog priority -> entries, listed in a Covers: line; measured 2026-11-04, after the page
	// bastion.md raised its features to MVP, the projects of R-AUTHZ-026 (IAM-051, IAM-052, IAM-057,
	// UI-003, UI-050, UI-051) moved into the beta, and the mirror mode of phase 6b pulled the pull
	// replication entries API-082 to API-085 forward (Q-REPO-143), and after formats/oci.md (R-OCI-001 to R-OCI-035) listed the
	// entries of PKG-DOCKER-* and PKG-OCI-* that phase T-16 builds, and after formats/debian.md (R-DEB-…, phase T-17) listed
	// its P2 entries and the Later entry PKG-DEB-003, and after formats/ansible.md (R-ANS-001 to R-ANS-032, unit F0) listed the 13 P1 entries of PKG-ANSIBLE-*,
	// and after the visibility levels of R-AUTHZ-033 (T-18) covered the P2 entry IAM-062, and after signatures.md (R-SIG-001 to
	// R-SIG-043, stage 1 of the signatures on artifacts) listed the P1 entries BEYOND-015 and BEYOND-021 and the P2 entries BEYOND-016,
	// BEYOND-034 and PKG-NPM-024, and after its stage 2 (R-SIG-044 to R-SIG-060, Quader signs) listed the P2 entry PKG-NPM-037, and its
	// stage 3 (R-SIG-061 to R-SIG-078, the gate per repository) the P2 entry BEYOND-019. Several object-store connections (R-STOR-050 to
	// R-STOR-063, stage 1 of the stored content) list the P2 entries STOR-011, STOR-012 and STOR-015. The curation of a remote repository
	// (remote-curation.md, R-CUR-001 to R-CUR-023) lists the P1 entries SEC-007, SEC-032, SEC-035 and SEC-037 and the P2 entries SEC-029,
	// RMT-027 and ART-044. The chart view of formats/oci.md (R-OCI-042) lists the P1 entry PKG-OCI-018.
	want := map[string][2]int{
		prioMVP:   {395, 395},
		prioP1:    {514, 350},
		prioP2:    {884, 59},
		prioLater: {374, 2},
		prioOut:   {72, 0},
	}
	t.Run("E0.8 catalog counts per priority", func(t *testing.T) {
		for prio, w := range want {
			if got := m.countAll(m.Features, prio); got != w[0] {
				t.Errorf("%s entries: got %d, want %d", prio, got, w[0])
			}
			listed := m.countSpecified(m.Features, prio)
			if listed != w[1] {
				t.Errorf("%s listed in Covers: got %d, want %d", prio, listed, w[1])
			}
			if open := m.countOpen(m.Features, prio); open != w[0]-w[1] {
				t.Errorf("%s not listed: got %d, want %d", prio, open, w[0]-w[1])
			}
		}
	})
	t.Run("E0.8 catalog totals", func(t *testing.T) {
		if got := len(m.Features); got != 2239 {
			t.Errorf("catalog entries: got %d, want 2239", got)
		}
		if got := m.countSpecified(m.Features); got != 806 {
			t.Errorf("listed entries: got %d, want 806", got)
		}
		if got := m.countOpen(m.Features); got != 1433 {
			t.Errorf("not listed entries: got %d, want 1433", got)
		}
	})
	t.Run("E0.8 requirement counts", func(t *testing.T) {
		count := map[string]int{}
		for _, r := range m.Reqs {
			count[r.Priority]++
		}
		// 1160 since bastion.md's Every format (R-BAS-050 to R-BAS-060, eleven MVP requirements of 2026-11-22); 1168 since the
		// download links of storage.md (R-STOR-070 to R-STOR-074, R-LIM-040, R-UI-200, R-UI-201, and R-STOR-014 from P2 to P1). 1186 since the
		// several object-store connections of storage.md (R-STOR-050 to R-STOR-063, R-HA-072, R-BAK-030, R-UI-196, R-UI-197). 1205 since the
		// encryption of stored content (R-STOR-075 to R-STOR-091, R-BAK-031, R-BAS-061, and R-STOR-048 from P2 to P1). 1217 since the
		// handover per blob to a Bastion (R-BAS-070 to R-BAS-079, R-STOR-095, R-STOR-096, twelve P1 requirements). 1226 since the read
		// shapes per package at a Bastion (R-BAS-080 to R-BAS-088, nine P1 requirements). 1234 since the destruction of a retired
		// connection's storage keys (R-STOR-097 to R-STOR-103, R-BAK-032, eight P1 requirements). 1262 since the curation of a remote
		// repository (R-CUR-001 to R-CUR-023, R-NPM-049, R-NUGET-035, R-OCI-038, R-DEB-036, R-ANS-047, 28 P1 requirements). 1264 since
		// the scans after any fill (R-VULN-050, R-MAL-035, two P1 requirements). 1268 since the downloads from upstreams (R-OPS-049 to
		// R-OPS-052, four P1 requirements). 1273 since a screen says a server's code in its own language (R-UI-214, one P1 requirement).
		// 1274 since a page has one scrollbar (R-UI-215, one P1 requirement). 1276 since the accounts of the upstream registries from
		// existing Secrets (R-REPO-077, R-HA-073, two MVP requirements of the well-known repositories and the chart's secret wiring).
		// 1277 since an error says every fact of its answer (R-UI-216, one P1 requirement). 1278 since the downloads kept in sync
		// (R-CUR-027, one P1 requirement). 1280 since the switches of a package and a version and the change of the delivery mode
		// (R-CUR-024, R-CUR-025, two P1 requirements of W-28). 1281 since every load shows the spinner (R-UI-217, one P1 requirement).
		// 1282 since the range of versions in a package's allow (R-CUR-026, one P1 requirement of W-28).
		// 1285 since a version a remote cached is described as a local one (R-NPM-050, R-NPM-051, R-DEB-037, three P1 requirements of
		// W-30).
		// 1288 since a collection's documentation reads like the documentation site (R-ANS-048 to R-ANS-050, three P1 requirements of
		// W-30). 1289 since a package leads with the documentation its format carries (R-UI-218, one MVP requirement of W-30).
		// 1291 since a Debian package shows its relations, long description, changelog, copyright and file list (R-DEB-038, R-DEB-039,
		// two P1 requirements of W-30).
		// 1294 since an image's facts (R-OCI-039 to R-OCI-041, three P1 requirements of W-30, G7).
		// 1295 since a Helm chart's view (R-OCI-042, one P1 requirement, the owner's d4 of W-30).
		// 1296 since a role read through a remote reads like a role (R-ANS-056, one P1 requirement of W-30).
		// 1299 since the generator reads what the documentation site shows: fragments, a role's argument specs, the changelog
		// (R-ANS-051 to R-ANS-053, three P1 requirements of W-30).
		// 1302 since deprecation is a property of every format that has it (R-API-085, R-NUGET-036, R-ANS-060, three P1 requirements of
		// W-30).
		// 1305 since collections are found as on Galaxy, a name has a page, and the documentation is searched across collections
		// (R-ANS-057, R-ANS-058, R-UI-220, three P1 requirements of W-30, A3).
		// 1307 since a version's general properties read the same in every format and every table of versions says each version's
		// downloads and an image's digest (R-UI-221, R-UI-222, two MVP requirements of W-30, G3).
		// 1308 since a generic file's commands stand behind Download and a file without a type gets the text type of its name
		// (R-GENERIC-017, one P1 requirement of W-30, G9).
		// 1309 since an upstream's package shows its documentation before a version is stored (R-CUR-028, one P1 requirement of W-30,
		// G2).
		// 1310 since a collection archive is published on a repository's screen (R-ANS-061, one P1 requirement of W-30, A5).
		// 1311 since the import warns of a missing changelog, README, requires_ansible and licence (R-ANS-062, one P1 requirement of
		// W-30, A5).
		// 1312 since a version's page lists the packages that depend on its package (R-API-086, one P1 requirement of W-30, G5).
		// 1315 since NuGet's install forms by package type, its icon, licence file, frameworks, symbols, revision and release notes, and
		// what a build or an install of a package runs (R-NUGET-037 to R-NUGET-039, three P1 requirements of W-30, G6).
		// 1316 since a format declares what one download from an upstream is (R-OPS-053, one P1 requirement of W31).
		// 1317 since what one act of a client costs in transfers (R-LIM-041, one MVP requirement of W32).
		// 1319 since a download is audited per item and shown as one row (R-OPS-054, R-OPS-055, two P1 requirements of W33).
		// 1320 since a Bastion's cache stays below its size between two sweeps (R-BAS-089, one MVP requirement of W-34, E1).
		// 1321 since the bytes between a Bastion and the main installation are counted (R-BAS-090, one P1 requirement of W-34,
		// E5).
		// 1322 since a Bastion's own refusals tell a format's client the longest wait the format declares (R-BAS-091, one MVP
		// requirement of W-34, E2).
		// 1323 since the Bastion's built-in figures hold one normal command of every client (R-BAS-092, one MVP requirement of
		// W-34, E3).
		// 1324 since the built-in profiles hold the uses the page promises (R-LIM-042, one MVP requirement of W36).
		// 1339 since a Bastion's limits, its configuration from the main installation, the throughput between the two and a
		// Bastion that names nothing of the main installation are specified (R-BAS-093 to R-BAS-099, R-LIM-043 to R-LIM-046,
		// R-UI-240, R-UI-241, R-UI-243: fourteen MVP requirements; R-UI-242: one P1 requirement; W-34, E6).
		// 1340 since a visitor without a session reads the open findings of a public repository (R-UI-224, one P1 requirement
		// of W-35, P3).
		// 1343 since an installation serves its imprint and privacy notice from the chart's values and every screen links them
		// (R-UI-225, R-UI-226, R-BAS-100, three P1 requirements of W-35, P4).
		// 1344 since the audit log shows a push, a publish and a delete as one row (R-OPS-056, one P1 requirement of W33b).
		// 1345 since the reading parts of the package screens are views without a session and without acts (R-UI-223, one P1
		// requirement of W-35, P2).
		// 1348 since the catalogue's wire, the meaning of gui and docs and the grammar of its document paths are built
		// (R-BAS-101 to R-BAS-103, three P1 requirements of W-35, P5, with W-34's E7).
		// 1353 since the main installation builds the public catalogue (R-BAS-104 to R-BAS-108, five P1 requirements of W-35, P6).
		// 1355 since a forward's problems and a relayed registry token name nothing of the main installation (R-BAS-114, R-BAS-115,
		// two P1 requirements of W-35, P8, with W-34's E17).
		// 1356 since a Bastion registration can be declared in the main installation's configuration (R-BAS-127, one MVP
		// requirement).
		// 1361 since the Bastion mirrors the public catalogue and serves it on with its age (R-BAS-109 to R-BAS-113, five P1
		// requirements of W-35, P7).
		// 1364 since the Bastion serves its web interface's bundle, state and data from the catalogue (R-BAS-116 to R-BAS-118,
		// three P1 requirements of W-35, P9).
		// 1370 since the Bastion's bundle, its pages and its application are built (R-BAS-119, R-BAS-120, R-UI-227 to R-UI-230,
		// six P1 requirements of W-35, P9).
		// 1374 since search engines find a Bastion's public pages (R-BAS-121 to R-BAS-123, R-UI-231, four P1 requirements of
		// W-35, P10).
		// 1377 since the form of a time is the reader's and a load stands in the middle of what loads (R-UI-250 to R-UI-252,
		// three P1 requirements of UI-1126).
		// 1379 since every malware verdict names its scanner and rescans are ranked by scan pressure (R-MAL-036, R-MAL-037, two
		// P1 requirements of W-36, P1).
		// 1383 since a Bastion's web interface shows a public package's documentation while docs is on (R-BAS-124 to R-BAS-126,
		// R-UI-232, four P1 requirements of W-35, P11).
		// 1385 since the admin area reads the storage overview and the copies in figures (R-STOR-106, R-STOR-107, two P1
		// requirements of W-37, P1).
		// 1388 since Quader keeps the history of damaged copies, unreachable connections and absorbed failures (R-STOR-108 to
		// R-STOR-110, three P1 requirements of W-37, P2).
		// 1390 since every answer that lists or shows an artifact carries its malware state, and today's signatures can be read
		// (R-MAL-039, R-MAL-040, two P1 requirements of W-36, P3).
		// 1394 since the storage page shows connections, availability, encryption and the download links on one page, and the
		// dashboard the same summary (R-UI-262 to R-UI-264, R-UI-266, four P1 requirements of W-37, P3).
		// 1395 since the job malware-scan is the scan controller that calls each scanner replica (R-MAL-038, one P1 requirement
		// of W-36, P2).
		// 1396 since what the scan controller and the coverage show, and the rescan settings, are read and set through the API
		// (R-MAL-041, one P1 requirement of W-36, P5).
		// 1398 since every artifact shows its malware state in one word with its details on demand (R-UI-255, R-UI-256, two P1
		// requirements of W-36, P4).
		// 1400 since a Bastion's visitor sees grades per version, projects by name, the installation's time form and Set up
		// client (R-BAS-128, R-BAS-129, two P1 requirements of W-35, P9e).
		// 1402 since the storage page shows what went wrong in a period, integrity per connection and the findings (R-STOR-111,
		// R-UI-265, two P1 requirements of W-37, P4).
		// 1404 since a grade names when, by which scanner and against which database it was checked, and today's database is
		// one read (R-VULN-051, R-VULN-052, two P1 requirements of W-36, P7).
		// 1406 since the Bastion shows the malware state of a public artifact (R-MAL-042, R-UI-258, two P1 requirements of W-36,
		// P8).
		// 1407 since the scan controller and the coverage stand on their pages (R-UI-257, one P1 requirement of W-36, P6).
		// 1410 since the image scanning has its page in the admin area (R-VULN-053, W-38), the help of a setting or a column
		// stands behind one info sign (R-UI-267, W-39 P1) and a fixed value names the Helm configuration (R-UI-268, W-39 P3):
		// three P1 requirements.
		// 1412 since a Bastion's project view has its catalogue and the switch Hide repositories (R-BAS-130, R-BAS-131, two P1
		// requirements of W-40, P1).
		// 1413 since a crawler finds the project view (R-BAS-132, one P1 requirement of W-40, P2).
		// 1414 since a Bastion is disabled soft or hard over its connection (R-BAS-133, one MVP requirement of W-41, A1).
		// 1415 since what a Bastion may distribute is stored (R-BAS-134, one P1 requirement of W-41, B1).
		// 1416 since every list of things a client fetches offers its command (R-UI-269, one P1 requirement of W-40, P4b).
		// 1417 since a control the Helm configuration fixes is offered unavailable with its reason (R-UI-270, one P1 requirement
		// of W-41, C0).
		// 1418 since a text the product writes is shown in the reader's language (R-UI-272, one P1 requirement of W-40, P7).
		if len(m.Reqs) != 1418 || count[prioMVP] != 744 || count[prioP1] != 635 || count[prioP2] != 39 {
			t.Errorf("requirements: got %d (MVP %d, P1 %d, P2 %d), want 1418 (744, 635, 39)",
				len(m.Reqs), count[prioMVP], count[prioP1], count[prioP2])
		}
	})
	t.Run("E0.8 headings inside code fences are ignored", func(t *testing.T) {
		var sawRequirement, sawEntry bool
		for _, l := range c.Fenced {
			sawRequirement = sawRequirement || strings.Contains(l, "### R-NPM-012 ·")
			sawEntry = sawEntry || strings.Contains(l, "### RMT-004 ·")
		}
		if !sawRequirement || !sawEntry {
			t.Errorf("expected the format examples in the READMEs to be skipped, got %v", c.Fenced)
		}
	})
}

func TestCommittedOutputIsCurrent(t *testing.T) {
	root := repoRoot(t)
	t.Run("E0.8 regeneration changes no committed file", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", root, "-check"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
			t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
		}
	})
}

func TestRuleViolations(t *testing.T) {
	cases := []struct {
		name, rule, want string
	}{
		{"bad-catalog-priority", "E0.8 catalog entry with a priority outside the rubric fails", "WID-002 has no 'Priority: **<P>**' line"},
		{"missing-catalog-priority", "E0.8 catalog entry without a Priority line fails", "WID-002 has no 'Priority: **<P>**' line"},
		{"bad-req-priority", "E0.8 requirement with a priority outside the rubric fails", "R-WID-001 has no 'Priority: **<P>** · Covers: ...' line"},
		{"missing-covers", "E0.8 requirement without Covers fails", "R-WID-001 has no 'Priority: **<P>** · Covers: ...' line"},
		{"unknown-covers", "E0.8 Covers ID missing from the catalog fails", "R-WID-001 covers WID-099, which is not in the catalog"},
		{"covers-mixed", "E0.8 quader-only mixed with IDs fails", "R-WID-001 mixes 'quader-only' with catalog IDs"},
		{"covers-bad-token", "E0.8 Covers token that is no ID fails", `neither a catalog ID nor 'quader-only': "widget two"`},
		{"dup-catalog-id", "E0.8 duplicate catalog ID fails", "duplicate catalog ID WID-001"},
		{"dup-req-id", "E0.8 duplicate requirement ID fails", "duplicate requirement ID R-WID-001"},
		{"dup-question-id", "E0.8 duplicate Q- ID fails", "duplicate question ID Q-WID-1"},
		{"dup-question-id-across-pages", "E0.8 duplicate Q- ID on two pages fails", "duplicate question ID Q-WID-1: spec/other.md:3, spec/widgets.md"},
		{"dup-gap-id", "E0.8 duplicate H- number fails", "duplicate gap ID H-1"},
		{"unclosed-fence", "E0.8 unclosed code fence fails", "code fence opened here is never closed"},
		{"malformed-heading", "E0.8 malformed entry heading fails", "entry heading not in the form '### <ID> · <Title>'"},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			c := mustLoad(t, fixtureDocs(tc.name))
			joined := strings.Join(c.Errors, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("errors do not contain %q:\n%s", tc.want, joined)
			}
		})
	}
	t.Run("E0.8 a valid fixture has no errors", func(t *testing.T) {
		if c := mustLoad(t, fixtureDocs("ok")); len(c.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", c.Errors)
		}
	})
	t.Run("E0.8 headings inside fenced code blocks are ignored", func(t *testing.T) {
		c := mustLoad(t, fixtureDocs("fenced"))
		if len(c.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", c.Errors)
		}
		if len(c.Fenced) != 4 {
			t.Errorf("skipped headings: got %d (%v), want 4", len(c.Fenced), c.Fenced)
		}
		if len(c.Reqs) != 2 {
			t.Errorf("requirements: got %d, want 2", len(c.Reqs))
		}
	})
}

func TestRuleViolationsFailTheCommand(t *testing.T) {
	t.Run("E0.8 command exits 1 on a rule violation", func(t *testing.T) {
		repo := t.TempDir()
		copyTree(t, filepath.Join("testdata", "dup-req-id", "docs"), filepath.Join(repo, "docs"))
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", repo}, strings.NewReader(""), &stdout, &stderr); code != exitData {
			t.Fatalf("exit %d, want %d; stderr:\n%s", code, exitData, stderr.String())
		}
		if !strings.Contains(stderr.String(), "duplicate requirement ID R-WID-001") {
			t.Errorf("stderr does not name the duplicate:\n%s", stderr.String())
		}
	})
}

func TestFeatureStatus(t *testing.T) {
	t.Run("feature is implemented only when every covering requirement is", func(t *testing.T) {
		// R-WID-001 covers WID-001 and WID-002; R-WID-002 covers nothing.
		m := fixtureModel(t, "ok", "R-WID-001")
		status := map[string]string{}
		for _, f := range m.Features {
			status[f.ID] = m.status(f)
		}
		want := map[string]string{"WID-001": statusImplemented, "WID-002": statusImplemented, "WID-003": statusOpen}
		for id, w := range want {
			if status[id] != w {
				t.Errorf("%s: got %s, want %s", id, status[id], w)
			}
		}
	})
	t.Run("feature with an unimplemented covering requirement stays specified", func(t *testing.T) {
		c := mustLoad(t, fixtureDocs("ok"))
		c.Reqs = append(c.Reqs, requirement{ID: "R-WID-003", Priority: prioMVP, Covers: []string{"WID-001"}, Page: "widgets.md"})
		m := newModel(c, []string{"widgets.md"}, map[string]bool{"R-WID-001": true})
		for _, f := range m.Features {
			switch f.ID {
			case "WID-001":
				if got := m.status(f); got != statusSpecified {
					t.Errorf("WID-001: got %s, want specified (R-WID-003 is not implemented)", got)
				}
			case "WID-002":
				if got := m.status(f); got != statusImplemented {
					t.Errorf("WID-002: got %s, want implemented", got)
				}
			}
		}
	})
	t.Run("no evidence leaves everything specified or open", func(t *testing.T) {
		m := fixtureModel(t, "ok")
		if got := m.countImplemented(m.Features); got != 0 {
			t.Errorf("implemented features: got %d, want 0", got)
		}
		if got := renderChecklist(m); !strings.Contains(got, "Nothing is implemented.") {
			t.Errorf("checklist should say nothing is implemented")
		}
	})
}

func readEvidence(t *testing.T, files ...string) *evidence {
	t.Helper()
	ev := newEvidence()
	for _, f := range files {
		if err := ev.readFile(filepath.Join("testdata", "evidence", f), strings.NewReader("")); err != nil {
			t.Fatal(err)
		}
	}
	return ev
}

func TestEvidence(t *testing.T) {
	known := map[string]bool{"R-WID-001": true, "R-WID-002": true, "R-WID-003": true}

	t.Run("passing test marks requirement implemented, failing test does not", func(t *testing.T) {
		ids, unknown := readEvidence(t, "pass-fail.json").implemented(known)
		if len(unknown) != 0 || strings.Join(ids, ",") != "R-WID-001" {
			t.Fatalf("got %v (unknown %v), want exactly R-WID-001", ids, unknown)
		}
	})
	t.Run("a failing test vetoes a passing one that names the same requirement", func(t *testing.T) {
		ids, _ := readEvidence(t, "veto.json").implemented(known)
		if strings.Join(ids, ",") != "R-WID-002" {
			t.Fatalf("got %v, want only R-WID-002 (R-WID-001 failed in package b, R-WID-003 was skipped)", ids)
		}
	})
	t.Run("JUnit reports are read like go test streams", func(t *testing.T) {
		ids, _ := readEvidence(t, "junit.xml").implemented(known)
		if strings.Join(ids, ",") != "R-WID-001" {
			t.Fatalf("got %v, want R-WID-001", ids)
		}
	})
	t.Run("reports combine and the veto holds across files", func(t *testing.T) {
		ids, _ := readEvidence(t, "pass-fail.json", "veto.json", "junit.xml").implemented(known)
		if len(ids) != 0 {
			t.Fatalf("got %v, want none: R-WID-001 fails in veto.json, R-WID-002 fails in pass-fail.json and junit.xml", ids)
		}
	})
	t.Run("an unknown ID in a test name is reported", func(t *testing.T) {
		_, unknown := readEvidence(t, "typo.json").implemented(known)
		if strings.Join(unknown, ",") != "R-WID-777" {
			t.Fatalf("unknown: got %v, want R-WID-777", unknown)
		}
	})
	t.Run("build chatter between JSON lines is skipped", func(t *testing.T) {
		ids, _ := readEvidence(t, "mixed-output.txt").implemented(known)
		if strings.Join(ids, ",") != "R-WID-001" {
			t.Fatalf("got %v, want R-WID-001", ids)
		}
	})
	t.Run("a file that is no report is an error", func(t *testing.T) {
		err := newEvidence().readFile(filepath.Join("testdata", "evidence", "not-a-report.json"), strings.NewReader(""))
		if err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("a test that holds a refusal marks the requirement unbuilt: it proves the refusal's requirement and keeps the marked one out", func(t *testing.T) {
		ev := newEvidence()
		stream := `{"Action":"pass","Package":"p","Test":"TestM/search_R-WID-001-unbuilt_R-WID-002"}` + "\n" +
			`{"Action":"pass","Package":"p","Test":"TestR_WID_003_unbuilt_DeprecateIsRefused"}` + "\n" +
			`{"Action":"pass","Package":"p","Test":"TestOther/R-WID-001_and_R-WID-003_pass_elsewhere"}` + "\n"
		if err := ev.readFile("-", strings.NewReader(stream)); err != nil {
			t.Fatal(err)
		}
		ids, unknown := ev.implemented(known)
		if len(unknown) != 0 || strings.Join(ids, ",") != "R-WID-002" {
			t.Fatalf("got %v (unknown %v), want only R-WID-002: R-WID-001 and R-WID-003 are held as not built", ids, unknown)
		}
		if !ev.Unbuilt["R-WID-001"] || !ev.Unbuilt["R-WID-003"] {
			t.Errorf("unbuilt: %v, want R-WID-001 and R-WID-003", ev.Unbuilt)
		}
		failing := newEvidence()
		if err := failing.readFile("-", strings.NewReader(`{"Action":"fail","Package":"p","Test":"TestM/R-WID-001-unbuilt"}`+"\n"+
			`{"Action":"pass","Package":"p","Test":"TestN/R-WID-001_built"}`+"\n")); err != nil {
			t.Fatal(err)
		}
		if ids, _ := failing.implemented(known); strings.Join(ids, ",") != "R-WID-001" {
			t.Errorf("a failing refusal test vetoed %v; it says nothing about the requirement it marks", ids)
		}
		typo := newEvidence()
		if err := typo.readFile("-", strings.NewReader(`{"Action":"pass","Package":"p","Test":"TestM/R-WID-777-unbuilt"}`+"\n")); err != nil {
			t.Fatal(err)
		}
		if _, unknown := typo.implemented(known); strings.Join(unknown, ",") != "R-WID-777" {
			t.Errorf("unknown: %v, want the marked R-WID-777", unknown)
		}
		for name, want := range map[string]string{
			"TestM/R-WID-001-unbuilt":                   "R-WID-001",
			"TestR_WID_001_unbuilt":                     "R-WID-001",
			"TestM/R-WID-001-unbuiltx":                  "",
			"TestM/PR-WID-001-unbuilt":                  "",
			"TestM/R-WID-001-unbuilt/R-WID-002-unbuilt": "R-WID-001,R-WID-002",
		} {
			if got := strings.Join(unbuiltIn(name), ","); got != want {
				t.Errorf("unbuiltIn(%q) = %q, want %q", name, got, want)
			}
			if ids := idsIn(name); len(ids) != 0 {
				t.Errorf("idsIn(%q) = %v; a marked ID is no evidence", name, ids)
			}
		}
	})
	t.Run("a package that started and did not end makes the report cut, one that ended in any way does not", func(t *testing.T) {
		cases := map[string]string{
			"two packages, one cut": `{"Action":"start","Package":"a"}` + "\n" + `{"Action":"start","Package":"b"}` + "\n" +
				`{"Action":"pass","Package":"a","Test":"TestA/R-WID-001_ok"}` + "\n" + `{"Action":"pass","Package":"a"}` + "\n",
			"a package that never ends": `{"Action":"start","Package":"b"}` + "\n",
		}
		for name, stream := range cases {
			err := newEvidence().readFile("-", strings.NewReader(stream))
			if err == nil || !strings.Contains(err.Error(), "the report is cut: b started and did not end") {
				t.Errorf("%s: %v, want the report named cut at package b", name, err)
			}
		}
		whole := `{"Action":"start","Package":"a"}` + "\n" + `{"Action":"start","Package":"b"}` + "\n" +
			`{"Action":"start","Package":"c"}` + "\n" + `{"Action":"pass","Package":"a"}` + "\n" +
			`{"Action":"fail","Package":"b"}` + "\n" + `{"Action":"skip","Package":"c"}` + "\n"
		if err := newEvidence().readFile("-", strings.NewReader(whole)); err != nil {
			t.Errorf("a report whose packages passed, failed and were skipped: %v", err)
		}
	})
	t.Run("standard input is read for -", func(t *testing.T) {
		ev := newEvidence()
		if err := ev.readFile("-", strings.NewReader(`{"Action":"pass","Package":"p","Test":"TestX/R-WID-002_ok"}`+"\n")); err != nil {
			t.Fatal(err)
		}
		if ids, _ := ev.implemented(known); strings.Join(ids, ",") != "R-WID-002" {
			t.Fatalf("got %v", ids)
		}
	})
}

func TestIDsInTestNames(t *testing.T) {
	cases := map[string][]string{
		"TestX/R-ARCH-002_forbidden_import_names_the_edge": {"R-ARCH-002"},
		"TestX/R-NPM-007":                    {"R-NPM-007"},
		"TestX/R-NPM-007/2_second_criterion": {"R-NPM-007"},
		"TestX/R-A-1_and_R-B-22_both":        {"R-A-1", "R-B-22"},
		"TestX/R-A-1_R-B-2":                  {"R-A-1", "R-B-2"},
		"R-ARCH-002 spaces in JUnit titles":  {"R-ARCH-002"},
		"TestPR-ARCH-002":                    nil,
		"TestX/R-ARCH-002-suffix":            nil,
		"TestX/r-arch-002":                   nil,
		"TestX/Q-ROAD-4_is_no_requirement":   nil,
		"TestX/E0.8_only":                    nil,

		// The underscore form of a Go function name (Q-ARCH-251).
		"TestR_API_053_NameOfTheTest":           {"R-API-053"},
		"TestR_EP_013_CeilingPerformance/shape": {"R-EP-013"},
		"BenchmarkR_EP_013_Decide":              {"R-EP-013"},
		"FuzzR_NPM_010":                         {"R-NPM-010"},
		"TestR_AUTH_027_E1_2_TokenEndToEnd":     {"R-AUTH-027"},
		"TestR_HA_052":                          {"R-HA-052"},
		"TestR_AUTH_001_and_R_AUTH_002_both":    {"R-AUTH-001", "R-AUTH-002"},
		"TestR_AUTH_001_R_AUTH_001":             {"R-AUTH-001"},
		"TestR_API_014_and_R-API-014_one_id":    {"R-API-014"},
		"TestFoo_R_API_053_bar":                 {"R-API-053"},
		"TestX/R_API_053_subtest":               {"R-API-053"},
		"TestX/Test_R_API_053":                  {"R-API-053"},
		"TestX/TestR_API_053":                   {"R-API-053"},
		"R_API_053 in a JUnit title":            {"R-API-053"},
		"TestPR_API_053":                        nil,
		"TestXR_API_053":                        nil,
		"TestX/PR_API_053":                      nil,
		"TestX-R_API_053":                       nil,
		"TestR_API_053x":                        nil,
		"TestR_API_053-suffix":                  nil,
		"TestR_API_":                            nil,
		"TestR_API":                             nil,
		"TestR_api_053":                         nil,
		"TestR__API_053":                        nil,
		"TestR_API_X_053":                       nil,
		"Testr_API_053":                         nil,
		"TestMyTest_R_API_053Z":                 nil,
		"TestX/Q_ROAD_4_is_no_requirement":      nil,
		"TestFooR_API_053":                      nil,
	}
	// The test names must not carry the sample names: the generator reads IDs
	// from the names of the tests it is run with.
	n := 0
	for name, want := range cases {
		n++
		t.Run(fmt.Sprintf("test name case %d", n), func(t *testing.T) {
			got := idsIn(name)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// copyTree copies a directory tree.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommandWithTestReports(t *testing.T) {
	root := repoRoot(t)
	setup := func(t *testing.T) string {
		repo := t.TempDir()
		copyTree(t, filepath.Join(root, "docs"), filepath.Join(repo, "docs"))
		return repo
	}
	stream := `{"Action":"pass","Package":"p","Test":"TestA/R-ARCH-002_passes"}` + "\n" +
		`{"Action":"fail","Package":"p","Test":"TestA/R-ARCH-003_fails"}` + "\n"
	write := func(t *testing.T, dir, content string) string {
		p := filepath.Join(dir, "report.json")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	recorded := func(t *testing.T, repo string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repo, "docs", "coverage", "implemented.txt"))
		if err != nil {
			t.Fatal(err)
		}
		ids, err := parseImplemented(string(data))
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(ids, ",")
	}

	// The pipeline's run: the record, and with -links-at (Q-ARCH-1040) the pages' links to the files the branch coverage does not
	// hold pointed at the record's commit, their links to each other relative.
	t.Run("E0.8 go test -json stream marks exactly the passing requirement implemented, the links at the record's commit", func(t *testing.T) {
		repo := setup(t)
		report := write(t, t.TempDir(), stream)
		const commit = "0123456789abcdef0123456789abcdef01234567"
		spec := repoURL + "blob/" + commit + "/docs/spec/architecture.md"
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", repo, "-tests", report, "-links-at", commit}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
			t.Fatalf("exit %d:\n%s", code, stderr.String())
		}
		if got := recorded(t, repo); got != "R-ARCH-002" {
			t.Fatalf("implemented.txt lists %v, want exactly R-ARCH-002", got)
		}
		checklist, _ := os.ReadFile(filepath.Join(repo, "docs", "coverage", "checklist.md"))
		if !strings.Contains(string(checklist), "[R-ARCH-002]("+spec+") (implemented)") {
			t.Errorf("checklist does not mark R-ARCH-002 as implemented with its link at the record's commit")
		}
		if strings.Contains(string(checklist), "[R-ARCH-003]("+spec+") (implemented)") {
			t.Errorf("checklist marks the failing R-ARCH-003 as implemented")
		}
		readme, _ := os.ReadFile(filepath.Join(repo, "docs", "coverage", "README.md"))
		if !strings.Contains(string(readme), "| Covered by an MVP requirement | Implemented |") {
			t.Errorf("the README of the record has no column Implemented")
		}
		if !strings.Contains(string(readme), "](checklist.md") {
			t.Error("the README's links to the checklist, which the branch holds, are not relative any more")
		}
		for name, page := range map[string][]byte{"checklist.md": checklist, "README.md": readme} {
			if strings.Contains(string(page), "](../") {
				t.Errorf("%s keeps a relative link out of docs/coverage, which leads nowhere on the branch", name)
			}
		}
	})
	t.Run("a test naming an unknown requirement fails the command", func(t *testing.T) {
		repo := setup(t)
		report := write(t, t.TempDir(), `{"Action":"pass","Package":"p","Test":"TestA/R-ARCH-999_typo"}`+"\n")
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", repo, "-tests", report}, strings.NewReader(""), &stdout, &stderr); code != exitData {
			t.Fatalf("exit %d:\n%s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "R-ARCH-999") {
			t.Errorf("stderr does not name R-ARCH-999:\n%s", stderr.String())
		}
	})
	t.Run("positional report arguments and standard input count as reports", func(t *testing.T) {
		repo := setup(t)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", repo, "-tests", "-"}, strings.NewReader(stream), &stdout, &stderr); code != exitOK {
			t.Fatalf("exit %d:\n%s", code, stderr.String())
		}
		data, _ := os.ReadFile(filepath.Join(repo, "docs", "coverage", "implemented.txt"))
		if !strings.Contains(string(data), "\nR-ARCH-002\n") || strings.Contains(string(data), "R-ARCH-003") {
			t.Errorf("implemented.txt:\n%s", data)
		}
	})
	t.Run("-previous lists what the record adds and drops, with the reason, and fails on neither", func(t *testing.T) {
		repo := setup(t)
		previous := filepath.Join(t.TempDir(), "implemented.txt")
		// R-ARCH-002 stays; R-ARCH-003 fails in this run; R-ARCH-004 fails in a carried report; R-ARCH-005 is named by no report;
		// R-NOPE-001 is no requirement any more; R-ARCH-006 is new.
		if err := os.WriteFile(previous, []byte("# the record of the branch\nR-ARCH-002\nR-ARCH-003\nR-ARCH-004\nR-ARCH-005\nR-NOPE-001\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run1 := write(t, t.TempDir(), `{"Action":"pass","Package":"p","Test":"TestA/R-ARCH-002_passes"}`+"\n"+
			`{"Action":"fail","Package":"p","Test":"TestA/R-ARCH-003_fails"}`+"\n"+
			`{"Action":"pass","Package":"p","Test":"TestA/R-ARCH-004_passes"}`+"\n"+
			`{"Action":"pass","Package":"p","Test":"TestA/R-ARCH-006_passes"}`+"\n")
		earlier := write(t, t.TempDir(), `{"Action":"fail","Package":"p","Test":"TestB/R-ARCH-004_fails"}`+"\n")
		var stdout, stderr bytes.Buffer
		code := run([]string{"-repo", repo, "-tests", run1, "-carried", earlier, "-previous", previous}, strings.NewReader(""), &stdout, &stderr)
		if code != exitOK {
			t.Fatalf("exit %d:\n%s", code, stderr.String())
		}
		for _, want := range []string{
			"newly proven: 1\n  R-ARCH-006\n",
			"no longer proven: 4\n",
			"  R-ARCH-003: a failing test of this run names it\n",
			"  R-ARCH-004: a failing test of a carried report names it\n",
			"  R-ARCH-005: no report names it in a passing test\n",
			"  R-NOPE-001: no requirement of the specification any more\n",
		} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
			}
		}
		if got := recorded(t, repo); got != "R-ARCH-002,R-ARCH-006" {
			t.Errorf("implemented.txt lists %v, want R-ARCH-002 and R-ARCH-006", got)
		}
	})
	t.Run("a cut report fails the command and writes nothing", func(t *testing.T) {
		repo := setup(t)
		cut := write(t, t.TempDir(), `{"Action":"start","Package":"p"}`+"\n"+
			`{"Action":"run","Package":"p","Test":"TestA/R-ARCH-002_passes"}`+"\n"+
			`{"Action":"pass","Package":"p","Test":"TestA/R-ARCH-002_passes"}`+"\n")
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", repo, "-tests", cut}, strings.NewReader(""), &stdout, &stderr); code != exitData {
			t.Fatalf("exit %d, want %d:\n%s", code, exitData, stderr.String())
		}
		if !strings.Contains(stderr.String(), "the report is cut: p started and did not end") {
			t.Errorf("stderr: %s", stderr.String())
		}
		if _, err := os.Stat(filepath.Join(repo, "docs", "coverage", "implemented.txt")); err == nil {
			t.Error("a record was written from a cut report")
		}
	})
	t.Run("the flags of the record need -tests, and -check does not combine with it", func(t *testing.T) {
		repo := setup(t)
		for _, args := range [][]string{
			{"-carried", "x.json"},
			{"-previous", "x.txt"},
			{"-carried", "x.json", "-links"},
			{"-check", "-tests", "x.json"},
			{"-tests", "x.json", "-links"},
			{"-claims", "-tests", "x.json"},
			{"-links-at", "0123456789abcdef"},
			{"-tests", "x.json", "-links-at", "HEAD"},
			{"-links", "-links-at", "0123456789abcdef"},
		} {
			if code := run(append([]string{"-repo", repo}, args...), strings.NewReader(""), io.Discard, io.Discard); code != exitUsage {
				t.Errorf("%v: exit %d, want %d", args, code, exitUsage)
			}
		}
	})
	t.Run("a tree that holds the record or the badge is refused by the generator and by -check", func(t *testing.T) {
		for _, rel := range []string{recordRel, badgeRel} {
			repo := setup(t)
			p := filepath.Join(repo, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("R-ARCH-002\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{}, {"-check"}} {
				var stderr bytes.Buffer
				if code := run(append([]string{"-repo", repo}, args...), strings.NewReader(""), io.Discard, &stderr); code != exitData {
					t.Errorf("%s %v: exit %d, want %d:\n%s", rel, args, code, exitData, stderr.String())
				}
				if !strings.Contains(stderr.String(), rel+" is in the tree") {
					t.Errorf("%s %v: stderr does not name the file:\n%s", rel, args, stderr.String())
				}
			}
		}
	})
	t.Run("without a record the pages show what the specification covers and point to the branch", func(t *testing.T) {
		repo := setup(t)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", repo}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
			t.Fatalf("exit %d:\n%s", code, stderr.String())
		}
		checklist, _ := os.ReadFile(filepath.Join(repo, "docs", "coverage", "checklist.md"))
		readme, _ := os.ReadFile(filepath.Join(repo, "docs", "coverage", "README.md"))
		if strings.Contains(string(checklist), "(implemented)") || strings.Contains(string(checklist), "Nothing is implemented") {
			t.Error("the checklist of main claims an implementation status")
		}
		if !strings.Contains(string(checklist), "("+recordPage+"checklist.md)") {
			t.Error("the checklist of main does not link the checklist of the record")
		}
		if strings.Contains(string(readme), "| Implemented |") || !strings.Contains(string(readme), "on the branch `coverage`") {
			t.Error("the README of main counts implemented entries or does not point to the record")
		}
		if _, err := os.Stat(filepath.Join(repo, "docs", "coverage", "implemented.txt")); err == nil {
			t.Error("the generator wrote a record without reports")
		}
	})
}

func TestChecklistFormat(t *testing.T) {
	t.Run("E0.8 checklist lists every entry and requirement", func(t *testing.T) {
		m := fixtureModel(t, "ok")
		out := renderChecklist(m)
		for _, want := range []string{
			"| WID-001 | First widget | MVP | specified | [R-WID-001](../spec/widgets.md) |",
			"| WID-003 | Third widget | Out of scope | open | – |",
			"| R-WID-002 | Own design | P1 | [widgets.md](../spec/widgets.md) |",
			"## Widgets (WID)",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("checklist lacks %q:\n%s", want, out)
			}
		}
	})
	t.Run("checklist shows implementation status", func(t *testing.T) {
		out := renderChecklist(fixtureModel(t, "ok", "R-WID-001", "R-WID-002"))
		for _, want := range []string{
			"| WID-001 | First widget | MVP | implemented | [R-WID-001](../spec/widgets.md) (implemented) |",
			"| R-WID-002 (implemented) | Own design |",
			"2 of 2 spec requirements are implemented",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("checklist lacks %q:\n%s", want, out)
			}
		}
	})
}

func TestLinks(t *testing.T) {
	write := func(t *testing.T, root, rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	write(t, root, "a.md", "# A\n\n## Same\n\n## Same\n\n[ok](b.md) [ok](b.md#target-section) [ok](#same-1) [ok](sub/c.md#c-title)\n"+
		"[gone](missing.md) [bad anchor](b.md#nope) [bad self](#nope) [web](https://example.com/x.md)\n"+
		"`[code](missing.md)`\n\n```\n[fenced](missing.md)\n```\n\n[abs](/sub/c.md)\n")
	write(t, root, "b.md", "# B\n\n## Target `Section`\n\n<a id=\"explicit\"></a>\n")
	write(t, root, "sub/c.md", "# C Title\n\n[up](../b.md#explicit) [up](../a.md#same)\n")
	write(t, root, "node_modules/x/README.md", "[skipped](nowhere.md)\n")
	// .out holds what a build downloads or generates (the conformance suite's module cache, reports): not the repository's documents.
	write(t, root, ".out/oci-conformance/mod/x/README.md", "[skipped](nowhere.md)\n")
	// .claude holds other checkouts of the repository (worktrees) and .dev the local state of the development profiles: neither is
	// a document of this checkout, and a half-written worktree must not fail the check of this one.
	write(t, root, ".claude/worktrees/w/docs/x.md", "[skipped](nowhere.md)\n")
	write(t, root, ".dev/ui-dev/x.md", "[skipped](nowhere.md)\n")

	broken, checked, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("E0.8 link check finds exactly the broken links", func(t *testing.T) {
		want := []string{
			"a.md:8: b.md#nope (no such anchor in b.md)",
			"a.md:8: #nope (no such anchor in a.md)",
			"a.md:8: missing.md (target does not exist)",
		}
		got := strings.Join(broken, "\n")
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("missing report %q in:\n%s", w, got)
			}
		}
		if len(broken) != len(want) {
			t.Errorf("got %d broken links, want %d:\n%s", len(broken), len(want), got)
		}
		if checked != 10 {
			t.Errorf("checked %d links, want 10", checked)
		}
	})
	t.Run("E0.8 -links exit code", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-repo", root, "-links"}, strings.NewReader(""), &stdout, &stderr); code != exitData {
			t.Fatalf("exit %d, want %d", code, exitData)
		}
	})
}

// Q-ARCH-1040: a link of a published page to a file the branch coverage does not hold points at the file on GitHub at the record's
// commit; a link between the published files, an anchor of the page, a link with a scheme and a link in code stay as they are.
func TestLinksAtTheRecordsCommit(t *testing.T) {
	t.Parallel()
	const c = "abc1234"
	at := repoURL + "blob/" + c + "/"
	for in, want := range map[string]string{
		"[s](../spec/storage.md#r-stor-001--x)":        "[s](" + at + "docs/spec/storage.md#r-stor-001--x)",
		"[r](roadmap.md)":                              "[r](" + at + "docs/coverage/roadmap.md)",
		"[d](../spec/)":                                "[d](" + repoURL + "tree/" + c + "/docs/spec)",
		"[root](/README.md)":                           "[root](" + at + "README.md)",
		"[`code` text](../developer/coverage.md)":      "[`code` text](" + at + "docs/developer/coverage.md)",
		"[c](checklist.md#widgets-wid) [r](README.md)": "[c](checklist.md#widgets-wid) [r](README.md)",
		"[i](implemented.txt) [a](#same-page)":         "[i](implemented.txt) [a](#same-page)",
		"[w](https://example.com/x.md)":                "[w](https://example.com/x.md)",
		"`[x](../spec/a.md)` [y](../spec/b.md)":        "`[x](../spec/a.md)` [y](" + at + "docs/spec/b.md)",
		"[ref]: ../features/README.md":                 "[ref]: " + at + "docs/features/README.md",
		"```\n[f](../spec/a.md)\n```":                  "```\n[f](../spec/a.md)\n```",
	} {
		if got := linksAt(in, "docs/coverage", c); got != want {
			t.Errorf("linksAt(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
