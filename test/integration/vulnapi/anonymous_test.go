//go:build integration

package vulnapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// anonymousReport is the version report as the anonymous caller reads it (R-VULN-020).
type anonymousReport struct {
	Repositories []struct {
		Repository  string  `json:"repository"`
		Evaluation  string  `json:"evaluation"`
		Blocked     bool    `json:"blocked"`
		MissingData *string `json:"missingData"`
		Findings    []struct {
			Advisory        string   `json:"advisory"`
			Status          string   `json:"status"`
			FixAvailable    bool     `json:"fixAvailable"`
			FixedVersions   []string `json:"fixedVersions"`
			DismissalReason *string  `json:"dismissalReason"`
			ResolvedReason  *string  `json:"resolvedReason"`
		} `json:"findings"`
		Grade struct {
			grade
			State      string  `json:"state"`
			StateSince *string `json:"stateSince"`
			StateCode  *string `json:"stateCode"`
		} `json:"grade"`
	} `json:"repositories"`
}

func decodeReport(t *testing.T, a testenv.Answer) anonymousReport {
	t.Helper()
	var r anonymousReport
	if a.Status != http.StatusOK || json.Unmarshal(a.Body, &r) != nil {
		t.Fatalf("version report: %d %s", a.Status, a.Body)
	}
	return r
}

// TestR_VULN_019_AnAnonymousVisitorReadsTheOpenFindingsOfPublicRepositories: the owner's decision of 2026-11-26 (Q-VULN-280,
// Q-VULN-281): the main installation shows a visitor without a credential the grade and the open findings of what a public level
// admits, decided as the package reads beside them are, on a replica of the role api that serves the management API in the chart.
//
// The project it is public; npm-pub is a local repository with public artifacts, npm-local stays private, and npm-mixed is a virtual
// repository over both. pubpkg 1.0.0 is held by npm-pub and npm-local. In npm-pub it has a High finding with a fix (needs_triage), a
// Low (confirmed), a Medium a triager blocked, a Medium dismissed and a Critical resolved; in npm-local a Critical and the same High.
// privpkg 1.0.0 is held by npm-local only. Findings are rows of a few bytes; the test runs in about 3 s.
func TestR_VULN_019_AnAnonymousVisitorReadsTheOpenFindingsOfPublicRepositories(t *testing.T) {
	e := newEnvWith(t, func(st *testenv.Stack) {
		st.Repository("npm-pub", "local", "npm", `{"local":{"packageOwnership":"off"}}`)
		st.Repository("npm-mixed", "virtual", "npm", "")
		st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id) SELECT v.id, m.pos, m.id
			FROM repositories v, (SELECT id, CASE key WHEN 'npm-pub' THEN 0 ELSE 1 END AS pos FROM repositories WHERE key IN ('npm-pub', 'npm-local')) m
			WHERE v.key = 'npm-mixed'`)
		st.Exec("UPDATE project SET visibility = 'public'")
		st.Exec("UPDATE repositories SET visibility_artifacts = 'public' WHERE key IN ('npm-pub', 'npm-mixed')")
	})
	e.st.Exec(`INSERT INTO vuln_source_state (source, ecosystem, state, synced_at) VALUES ('osv', 'npm', 'complete', now())`)
	api := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false"}})
	for _, repo := range []string{"npm-pub", "npm-local"} {
		e.addVersion(repo, "pubpkg", "1.0.0")
	}
	e.addVersion("npm-local", "privpkg", "1.0.0")
	const (
		high, low, blocked, dismissed, resolved = "GHSA-aaaa-aaaa-0101", "GHSA-aaaa-aaaa-0102", "GHSA-aaaa-aaaa-0105", "GHSA-aaaa-aaaa-0103", "GHSA-aaaa-aaaa-0104"
		private, privOnly                       = "GHSA-aaaa-aaaa-0201", "GHSA-aaaa-aaaa-0301"
	)
	pub := func(f finding) { f.Repo, f.Package = "npm-pub", "pubpkg"; e.addFinding(f) }
	pub(finding{Advisory: high, Severity: "high", Fix: true})
	pub(finding{Advisory: low, Severity: "low", Status: "confirmed"})
	pub(finding{Advisory: blocked, Severity: "medium", Status: "blocked"})
	pub(finding{Advisory: dismissed, Severity: "medium", Status: "dismissed", DismissalReason: "risk_accepted"})
	pub(finding{Advisory: resolved, Severity: "critical", Status: "resolved"})
	e.addFinding(finding{Repo: "npm-local", Package: "pubpkg", Advisory: private, Severity: "critical"})
	e.addFinding(finding{Repo: "npm-local", Package: "pubpkg", Advisory: high, Severity: "high"})
	e.addFinding(finding{Repo: "npm-local", Package: "privpkg", Advisory: privOnly, Severity: "critical"})
	for _, id := range []string{high, low, blocked, dismissed, resolved, private, privOnly} {
		e.addAdvisory("npm", id, "An advisory "+id, "high", "CVE-2026-"+id[len(id)-4:])
	}
	pubID, privID := packageID("npm", "pubpkg"), packageID("npm", "privpkg")
	anon := func(path string) testenv.Answer { t.Helper(); return e.get(api, path, "") }
	noPrivate := func(t *testing.T, a testenv.Answer) {
		t.Helper()
		for _, word := range []string{"npm-local", "npm-mixed", private, privOnly, dismissed, resolved, "risk_accepted", "privpkg"} {
			if strings.Contains(string(a.Body), word) {
				t.Errorf("the anonymous answer names %s: %s", word, a.Body)
			}
		}
	}

	t.Run("R-VULN-020 the version report lists exactly the open findings its grade counts, and nothing of a private holder", func(t *testing.T) {
		a := anon("/api/v1/packages/" + pubID + "/versions/1.0.0/vulnerabilities")
		r := decodeReport(t, a)
		noPrivate(t, a)
		if len(r.Repositories) != 1 || r.Repositories[0].Repository != "it/npm-pub" {
			t.Fatalf("repositories: %s", a.Body)
		}
		rv := r.Repositories[0]
		var advisories []string
		fixable := int64(0)
		for _, f := range rv.Findings {
			advisories = append(advisories, f.Advisory)
			if !slices.Contains([]string{"needs_triage", "confirmed", "blocked"}, f.Status) || f.DismissalReason != nil || f.ResolvedReason != nil {
				t.Errorf("finding %+v", f)
			}
			if f.FixAvailable {
				fixable++
			}
		}
		slices.Sort(advisories)
		if want := []string{high, low, blocked}; !slices.Equal(advisories, want) {
			t.Errorf("findings %v, want %v", advisories, want)
		}
		g := rv.Grade
		open := g.Open.Critical + g.Open.High + g.Open.Medium + g.Open.Low + g.Open.None + g.Open.Unknown
		// Page and grade agree (B.4b): the rows are the findings the grade counts, the fixable count the rows with a fixed version.
		if open != int64(len(rv.Findings)) || g.Fixable != fixable || g.Grade != "C" || g.Open.High != 1 || g.Open.Medium != 1 || g.Open.Low != 1 {
			t.Errorf("grade %+v against %d rows, %d with a fix", g, len(rv.Findings), fixable)
		}
		if g.State != "checked" || g.StateCode != nil || g.StateSince != nil || rv.MissingData != nil {
			t.Errorf("state %q code %v since %v missing %v", g.State, g.StateCode, g.StateSince, rv.MissingData)
		}
	})

	t.Run("R-VULN-020 a signed-in reader sees what it saw before: every holder it reads, the dismissed finding beside the open ones", func(t *testing.T) {
		r := decodeReport(t, e.get(api, "/api/v1/packages/"+pubID+"/versions/1.0.0/vulnerabilities", e.tokAlice))
		if len(r.Repositories) != 2 {
			t.Fatalf("alice reads %d holders", len(r.Repositories))
		}
		for _, rv := range r.Repositories {
			if rv.Repository == "it/npm-pub" && len(rv.Findings) != 4 {
				t.Errorf("alice's npm-pub lists %d findings, want the three open ones and the dismissed one", len(rv.Findings))
			}
		}
	})

	t.Run("R-VULN-039 the package report and the list grade what the levels admit", func(t *testing.T) {
		a := anon("/api/v1/packages/" + pubID + "/vulnerabilities")
		noPrivate(t, a)
		var p struct {
			Versions []struct {
				Version string
				Grade   grade
			}
		}
		if a.Status != http.StatusOK || json.Unmarshal(a.Body, &p) != nil || len(p.Versions) != 1 || p.Versions[0].Grade.Grade != "C" ||
			p.Versions[0].Grade.Open.Critical != 0 || p.Versions[0].Grade.Repository != "it/npm-pub" {
			t.Fatalf("package report: %d %s", a.Status, a.Body)
		}
		list := anon("/api/v1/packages?format=npm&include=vulnerabilities")
		noPrivate(t, list)
		var page struct {
			Items []struct {
				Name               string
				VulnerabilityGrade *grade `json:"vulnerabilityGrade"`
			}
		}
		if list.Status != http.StatusOK || json.Unmarshal(list.Body, &page) != nil || len(page.Items) != 1 || page.Items[0].VulnerabilityGrade == nil ||
			page.Items[0].VulnerabilityGrade.Grade != "C" {
			t.Fatalf("list: %d %s", list.Status, list.Body)
		}
		// A virtual repository with a private member is no repository the anonymous caller reads (R-AUTHZ-009 rule 6): narrowed to it,
		// the grades are of nothing, as the list of its packages is empty.
		narrowed := anon("/api/v1/packages/" + pubID + "/vulnerabilities?repository=it/npm-mixed")
		noPrivate(t, narrowed)
		if narrowed.Status != http.StatusOK || strings.Contains(string(narrowed.Body), `"grade"`) {
			t.Errorf("narrowed to npm-mixed: %d %s", narrowed.Status, narrowed.Body)
		}
	})

	t.Run("R-VULN-020 the advisory page lists the open findings the levels admit, and is the 404 of a missing one otherwise", func(t *testing.T) {
		a := anon("/api/v1/vulnerabilities/advisories/npm/" + high)
		noPrivate(t, a)
		var d struct {
			Advisory struct{ ID string }
			Findings []struct{ Repository, Version string }
		}
		if a.Status != http.StatusOK || json.Unmarshal(a.Body, &d) != nil || len(d.Findings) != 1 || d.Findings[0].Repository != "it/npm-pub" {
			t.Fatalf("advisory %s: %d %s", high, a.Status, a.Body)
		}
		missing := anon("/api/v1/vulnerabilities/advisories/npm/GHSA-zzzz-zzzz-9999")
		if missing.Status != http.StatusNotFound {
			t.Fatalf("a missing advisory: %d %s", missing.Status, missing.Body)
		}
		for _, id := range []string{dismissed, resolved, private, privOnly} {
			hidden := anon("/api/v1/vulnerabilities/advisories/npm/" + id)
			if hidden.Status != http.StatusNotFound || hidden.Problem(t) != missing.Problem(t) {
				t.Errorf("advisory %s: %d %s, want the 404 of a missing one", id, hidden.Status, hidden.Body)
			}
		}
		if s := e.get(api, "/api/v1/vulnerabilities/advisories/npm/"+private, e.tokAlice); s.Status != http.StatusOK {
			t.Errorf("alice reads the advisory of npm-local: %d", s.Status)
		}
	})

	t.Run("R-AUTHZ-016 a version or package only a private repository holds answers as the package reads answer it", func(t *testing.T) {
		for _, tc := range []struct{ what, hidden, missing string }{
			{"version report", "/api/v1/packages/" + privID + "/versions/1.0.0/vulnerabilities", "/api/v1/packages/" + packageID("npm", "nopkg") + "/versions/1.0.0/vulnerabilities"},
			{"package report", "/api/v1/packages/" + privID + "/vulnerabilities", "/api/v1/packages/" + packageID("npm", "nopkg") + "/vulnerabilities"},
			{"version of a public package", "/api/v1/packages/" + pubID + "/versions/9.9.9/vulnerabilities", "/api/v1/packages/" + packageID("npm", "nopkg") + "/versions/9.9.9/vulnerabilities"},
			{"package read", "/api/v1/packages/" + privID + "/versions/1.0.0", "/api/v1/packages/" + packageID("npm", "nopkg") + "/versions/1.0.0"},
		} {
			hidden, missing := anon(tc.hidden), anon(tc.missing)
			if hidden.Status != http.StatusNotFound || missing.Status != http.StatusNotFound || hidden.Problem(t) != missing.Problem(t) {
				t.Errorf("%s: hidden %d %s, missing %d %s", tc.what, hidden.Status, hidden.Body, missing.Status, missing.Body)
			}
		}
		// The finding's detail and the report stay with who signs in.
		for _, path := range []string{"/api/v1/vulnerabilities/findings", "/api/v1/vulnerabilities/summary"} {
			if a := anon(path); a.Status != http.StatusUnauthorized {
				t.Errorf("%s: %d", path, a.Status)
			}
		}
	})
}

// TestR_EP_014_AnEndpointsExposureRulesBindTheAnonymousVulnerabilityReads: an endpoint that admits anonymous callers and exposes pubpkg
// of npm-pub from 2.0.0 only: through it a visitor without a credential reads the findings of 2.1.0 and not those of 1.0.0, by the
// version report, the package report, the list's grade and the advisory page, with the answers the package read gives the same version;
// an endpoint that admits no anonymous caller answers every one of the three 401, as the package read. About 4 s.
func TestR_EP_014_AnEndpointsExposureRulesBindTheAnonymousVulnerabilityReads(t *testing.T) {
	e := newEnvWith(t, func(st *testenv.Stack) {
		st.Repository("npm-pub", "local", "npm", `{"local":{"packageOwnership":"off"}}`)
		st.Exec("UPDATE project SET visibility = 'public'")
		st.Exec("UPDATE repositories SET visibility_artifacts = 'public' WHERE key = 'npm-pub'")
	})
	e.st.Exec(`INSERT INTO vuln_source_state (source, ecosystem, state, synced_at) VALUES ('osv', 'npm', 'complete', now())`)
	e.addVersion("npm-pub", "pubpkg", "1.0.0")
	e.addVersion("npm-pub", "pubpkg", "2.1.0")
	const old, cur = "GHSA-aaaa-aaaa-0401", "GHSA-aaaa-aaaa-0402"
	e.addFinding(finding{Repo: "npm-pub", Package: "pubpkg", Version: "1.0.0", Advisory: old, Severity: "critical"})
	e.addFinding(finding{Repo: "npm-pub", Package: "pubpkg", Version: "2.1.0", Advisory: cur, Severity: "low"})
	e.addAdvisory("npm", old, "Old", "critical", "CVE-2026-0401")
	e.addAdvisory("npm", cur, "Current", "low", "CVE-2026-0402")

	const host, closedHost = "packages.quader.test", "closed.quader.test"
	api := e.st.Start(t, testenv.ReplicaOptions{
		Name: "api-a", Role: "api", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false"},
		Endpoints: `{"customers":{"listen":"127.0.0.1:0","base_url":"https://` + host + `","surface":["api"],"auth":{"anonymous":true}},` +
			`"closed":{"listen":"127.0.0.1:0","base_url":"https://` + closedHost + `","surface":["api"]}}`,
	})
	rule := e.st.Do(api, "POST", "/api/v1/endpoints/customers/exposure-rules", e.tokRita,
		[]byte(`{"name":"pub","repository":"it/npm-pub","packages":{"include":["pubpkg"]},"versions":{"range":">=2.0.0"}}`),
		"Content-Type", "application/json")
	if rule.Status != http.StatusCreated {
		t.Fatalf("rule: %d %s", rule.Status, rule.Body)
	}
	epoch := rule.Header.Get("Quader-Authz-Epoch")
	through := func(path string) testenv.Answer {
		t.Helper()
		return e.st.DoAt(api.Addr("customers"), host, "GET", path, "", nil, "Quader-Authz-Min-Epoch", epoch)
	}
	id := packageID("npm", "pubpkg")
	// The package reads' answer for the same versions: the package list counts one version, 2.1.0, and the version page of 1.0.0 is the 404
	// of a missing one. (The pages of the package and of 2.1.0 are not compared: the rows of this suite have no files, and an endpoint with
	// rules shows a version by its files.)
	// The endpoint takes the new rule a moment after the epoch its creation answered (fail-closed meanwhile: an empty list); the reads
	// below start once it has.
	var versions testenv.Answer
	testenv.WaitFor(t, "the package list through the endpoint", 15*time.Second, func() bool {
		versions = through("/api/v1/packages?format=npm")
		return versions.Status == http.StatusOK && strings.Contains(string(versions.Body), `"latestVersion":"2.1.0"`) && strings.Contains(string(versions.Body), `"versionCount":1`)
	})
	if pkg := through("/api/v1/packages/" + id + "/versions/1.0.0"); pkg.Status != http.StatusNotFound {
		t.Errorf("the package read of the hidden version: %d", pkg.Status)
	}
	if a := through("/api/v1/packages/" + id + "/versions/1.0.0/vulnerabilities"); a.Status != http.StatusNotFound {
		t.Errorf("the version report of the hidden version: %d %s", a.Status, a.Body)
	}
	if a := through("/api/v1/packages/" + id + "/versions/2.1.0/vulnerabilities"); a.Status != http.StatusOK || strings.Contains(string(a.Body), old) {
		t.Errorf("the version report of the exposed version: %d %s", a.Status, a.Body)
	}
	grades := through("/api/v1/packages/" + id + "/vulnerabilities")
	if grades.Status != http.StatusOK || strings.Contains(string(grades.Body), `"1.0.0"`) || !strings.Contains(string(grades.Body), `"2.1.0"`) {
		t.Errorf("package report: %d %s", grades.Status, grades.Body)
	}
	if list := through("/api/v1/packages?format=npm&include=vulnerabilities"); list.Status != http.StatusOK || !strings.Contains(string(list.Body), `"grade":"A"`) {
		t.Errorf("the list grades the exposed latest version: %d %s", list.Status, list.Body)
	}
	if a := through("/api/v1/vulnerabilities/advisories/npm/" + old); a.Status != http.StatusNotFound {
		t.Errorf("the advisory of the hidden version: %d %s", a.Status, a.Body)
	}
	if a := through("/api/v1/vulnerabilities/advisories/npm/" + cur); a.Status != http.StatusOK {
		t.Errorf("the advisory of the exposed version: %d %s", a.Status, a.Body)
	}
	// Through the default endpoint both versions are public.
	if a := e.get(api, "/api/v1/packages/"+id+"/versions/1.0.0/vulnerabilities", ""); a.Status != http.StatusOK {
		t.Errorf("default endpoint: %d %s", a.Status, a.Body)
	}
	for _, path := range []string{
		"/api/v1/packages/" + id + "/versions/2.1.0", "/api/v1/packages/" + id + "/versions/2.1.0/vulnerabilities",
		"/api/v1/packages/" + id + "/vulnerabilities", "/api/v1/vulnerabilities/advisories/npm/" + cur,
	} {
		if a := e.st.DoAt(api.Addr("closed"), closedHost, "GET", path, "", nil); a.Status != http.StatusUnauthorized {
			t.Errorf("%s through an endpoint without anonymous callers: %d %s", path, a.Status, a.Body)
		}
	}
}
