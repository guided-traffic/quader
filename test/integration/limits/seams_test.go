//go:build integration

package limits

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The seams between the technical tokens, the accounting and the API (work package 4 of docs/developer/usage-and-limits.md):
// a registry token of a technical token on the request path, the cleanup of a deleted technical token, the fallback without
// Valkey.

// usageOf is the requests of a subject in the five-minute rows.
func (e *env) usageOf(kind, ref string) int64 {
	return e.st.Number(`SELECT COALESCE(sum(requests), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = $1 AND subject_ref = $2`, kind, ref)
}

// tokenRow is the usage row of a technical token in the instance's overview, validated against the document by the stack.
func (e *env) tokenRow(t *testing.T, id string) map[string]any {
	t.Helper()
	a := e.st.Do(e.r, "GET", "/api/v1/usage?kind=technicalToken", e.admin, nil)
	if a.Status != 200 {
		t.Fatalf("usage: %d %s", a.Status, a.Body)
	}
	items, _ := a.JSON(t)["items"].([]any)
	for _, it := range items {
		row := it.(map[string]any)
		if subject := row["subject"].(map[string]any); subject["id"] == id {
			return row
		}
	}
	t.Fatalf("the technical token %s is not in the overview: %s", id, a.Body)
	return nil
}

// R-LIM-001: a registry token issued for a technical token's secret is charged to the technical token, on the request path: the
// token request of a docker-style exchange (Basic with the secret, R-LIM-003: charged after the token service authenticated it) and
// the registry request with the registry token, each once. Nothing is charged to the client's address.
func TestR_LIM_001_ARegistryTokenOfATechnicalTokenIsChargedToTheTechnicalToken(t *testing.T) {
	var sat, technical string
	e := newEnvWith(t, envOptions{valkey: true, seed: func(st *testenv.Stack) {
		st.Repository("images", "local", "oci", `{}`)
		sa := st.ServiceAccount("puller")
		st.Policy("images-read", []string{"read"}, []string{"images"}, sa)
		sat = st.Token(sa, scope.Scope{Inherit: true})
		technical = st.Text(`SELECT technical_token_id::text FROM api_token WHERE principal_id = $1::uuid`, sa.ID)
	}})
	q := url.Values{"service": {testenv.DefaultHost}, "scope": {"repository:it/images/team/app:pull"}}
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("puller:"+sat))
	a := e.st.Do(e.r, "GET", "/v2/_token?"+q.Encode(), "", nil, "Authorization", basic)
	if a.Status != 200 {
		t.Fatalf("token request: %d %s", a.Status, a.Body)
	}
	registry, _ := a.JSON(t)["token"].(string)
	if registry == "" {
		t.Fatalf("no registry token: %s", a.Body)
	}
	if a := e.st.Do(e.r, "GET", "/v2/it/images/team/app/tags/list", registry, nil); a.Status != 200 && a.Status != 404 {
		t.Fatalf("registry request: %d %s", a.Status, a.Body)
	}
	time.Sleep(2500 * time.Millisecond) // the charge buffer and the usage buffer flush
	if n := e.usageOf("technical_token", technical); n != 2 {
		t.Fatalf("the technical token was charged %d requests, want 2: the token request and the registry request", n)
	}
	if n := e.st.Number(`SELECT COALESCE(sum(requests), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'anonymous'`); n != 0 {
		rows := e.st.Text(`SELECT string_agg(subject_ref || ' ' || place || ' repo=' || COALESCE(repository_id::text, '-') || ' requests=' || requests || ' refused=' || refused, '; ') FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'anonymous'`)
		t.Fatalf("%d requests were charged to an address: %s", n, rows)
	}
	row := e.tokenRow(t, technical)
	if subject := row["subject"].(map[string]any); subject["account"] != "sa:puller" || subject["level"] != "instance" || subject["deleted"] != false {
		t.Fatalf("the overview names the token %v", subject)
	}
}

// R-LIM-028, R-LIM-038: a technical token's own rule shows in its record (R-AUTH-045's limit); when the token is deleted,
// limits-cleanup deletes its rule, and its usage stays until its retention, shown as a deleted token.
func TestR_LIM_028_ADeletedTechnicalTokensRuleGoesAndItsUsageStays(t *testing.T) {
	var sat, technical, name string
	e := newEnvWith(t, envOptions{valkey: true, role: "all", seed: func(st *testenv.Stack) {
		sa := st.ServiceAccount("mirror")
		st.Policy("mirror-read", []string{"read"}, []string{rFiles}, sa)
		sat = st.Token(sa, scope.Scope{Inherit: true})
		technical = st.Text(`SELECT technical_token_id::text FROM api_token WHERE principal_id = $1::uuid`, sa.ID)
		name = st.Text(`SELECT name FROM technical_token WHERE id = $1::uuid`, technical)
	}})
	for range 3 {
		if a := e.st.Do(e.r, "GET", e.filePath, sat, nil); a.Status != 200 {
			t.Fatalf("download: %d %s", a.Status, a.Body)
		}
	}
	rule := e.st.Do(e.r, "POST", "/api/v1/limits/assignments", e.admin, map[string]any{
		"who": map[string]any{"kind": "technicalToken", "technicalToken": technical}, "profile": "restricted",
		"overrides": map[string]any{"bytesPerDay": 5_000_000_000}, "note": "",
	}, "Idempotency-Key", "limit-of-the-mirror")
	if rule.Status != 201 {
		t.Fatalf("rule: %d %s", rule.Status, rule.Body)
	}
	t.Run("R-AUTH-045 the token's record names its rule", func(t *testing.T) {
		a := e.st.Do(e.r, "GET", "/api/v1/service-accounts/mirror/technical-tokens/"+name, e.admin, nil)
		limit, _ := a.JSON(t)["limit"].(map[string]any)
		if a.Status != 200 || limit["rule"] != rule.JSON(t)["id"] || limit["profile"] != "restricted" || limit["overrides"] != float64(1) {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
	})
	time.Sleep(2500 * time.Millisecond)
	if n := e.usageOf("technical_token", technical); n < 3 {
		t.Fatalf("%d requests of the token, want 3", n)
	}
	if a := e.st.Do(e.r, "DELETE", "/api/v1/service-accounts/mirror/technical-tokens/"+name, e.admin, nil); a.Status != 204 {
		t.Fatalf("delete: %d %s", a.Status, a.Body)
	}
	// Two hourly rows of two people, 31 days old, of one day and one repository: the run folds them (R-LIM-028).
	const oldHour = `date_trunc('day', now() - interval '31 days') + interval '10 hours'`
	for i, who := range []string{"u-fold-a", "u-fold-b"} {
		e.st.Exec(`INSERT INTO usage_bucket (granularity, bucket_start, subject_kind, subject_ref, place, repository_id, requests, cached_bytes)
			SELECT '1h', `+oldHour+` + make_interval(hours => $2::int), 'user', $1, 'instance', id, 3 + $2::int, 100 FROM repositories WHERE key = $3`,
			who, i, rFiles)
	}
	// The worker queues the daily task's next scheduled run when it starts (`25 3 * * *`), and a run is refused while one waits:
	// the scheduled one is taken out, so the test's own run is the one that cleans up.
	e.st.Exec(`DELETE FROM job WHERE kind = 'limits-cleanup' AND state = 'queued'`)
	deadline := time.Now().Add(30 * time.Second)
	run := e.st.Do(e.r, "POST", "/api/v1/system/tasks/limits-cleanup/run", e.admin, nil)
	if run.Status != 202 {
		t.Fatalf("limits-cleanup: %d %s", run.Status, run.Body)
	}
	loc := run.Header.Get("Location")
	job := loc[strings.LastIndexByte(loc, '/')+1:]
	for e.st.Number(`SELECT count(*) FROM job WHERE id::text = $1 AND state = 'succeeded'`, job) == 0 {
		if time.Now().After(deadline) {
			state := e.st.Text(`SELECT state || ' ' || COALESCE(error::text, '') FROM job WHERE id::text = $1`, job)
			t.Fatalf("limits-cleanup %s did not succeed within 30 s: %s", job, state)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if n := e.st.Number(`SELECT count(*) FROM limit_assignment WHERE who_kind = 'technical_token' AND who_ref = $1`, technical); n != 0 {
		t.Fatalf("the deleted token's rule is still there (%d)", n)
	}
	if n := e.usageOf("technical_token", technical); n < 3 {
		t.Fatalf("the deleted token's usage went with it: %d requests left", n)
	}
	if subject := e.tokenRow(t, technical)["subject"].(map[string]any); subject["deleted"] != true || subject["name"] != technical {
		t.Fatalf("the overview names the deleted token %v", subject)
	}
	t.Run("R-LIM-028 the hourly rows older than 30 days are one daily sum without a person, and they are gone", func(t *testing.T) {
		sum := e.st.Text(`SELECT count(*) || ' ' || COALESCE(sum(subjects), 0) || ' ' || COALESCE(sum(requests), 0) || ' ' || COALESCE(sum(cached_bytes), 0)
			FROM usage_daily_summary s JOIN repositories r ON r.id = s.repository_id
			WHERE r.key = $1 AND s.subject_kind = 'user' AND s.day = ((`+oldHour+`) AT TIME ZONE 'UTC')::date`, rFiles)
		if sum != "1 2 7 200" {
			t.Errorf("the daily sums of the day (rows, people, requests, cached bytes): %s, want 1 2 7 200", sum)
		}
		if n := e.st.Number(`SELECT count(*) FROM usage_bucket WHERE granularity = '1h' AND bucket_start < now() - interval '30 days'`); n != 0 {
			t.Errorf("%d hourly rows older than 30 days are left", n)
		}
	})
}

// R-LIM-019: with Valkey stopped, a replica decides on its own share and never refuses because Valkey failed: the hour's
// requests up to the limit pass, the next is refused by the figure, and the fallback is the metric's.
func TestR_LIM_019_WithValkeyStoppedTheReplicaDecidesOnItsShare(t *testing.T) {
	e := newEnvWith(t, envOptions{valkey: false})
	e.setStandard(t, map[string]any{"requestsPerHour": 4})
	e.setMode(t, "enforce")
	e.awaitIndex(t)
	for i := range 4 {
		if a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil); a.Status != 200 {
			t.Fatalf("request %d within the share: %d %s", i, a.Status, a.Body)
		}
	}
	a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
	if a.Status != 429 || a.Code(t) != "rate_limited" || a.JSON(t)["figure"] != "requestsPerHour" {
		t.Fatalf("the fifth request of the hour: %d %s", a.Status, a.Body)
	}
	if v := testenv.MetricValue(e.r.Metrics(t), `quader_ratelimit_fallback{limiter="limits"}`); v != 1 {
		t.Fatalf("quader_ratelimit_fallback{limiter=\"limits\"} = %v, want 1", v)
	}
}

// Q-API-354, Q-API-356: a project's first maximum is set by PUT without If-Match; a replace requires it; an If-Match where nothing
// exists fails; the read says "none" with 200.
func TestR_LIM_033_AProjectsFirstMaximumTakesNoIfMatch(t *testing.T) {
	e := newEnv(t)
	const path = "/api/v1/projects/it/limits/maximum"
	body := map[string]any{"profile": "technical", "overrides": map[string]any{"bytesPerDay": 2_000_000_000_000}}
	projects := func() []any {
		a := e.st.Do(e.r, "GET", "/api/v1/limits/projects", e.admin, nil)
		items, _ := a.JSON(t)["items"].([]any)
		return items
	}
	// R-UI-177: a project with repositories and neither a maximum nor a rule is no row of the projects' list (Q-API-351).
	if items := projects(); len(items) != 0 {
		t.Fatalf("projects without limits listed: %v", items)
	}
	// Q-API-356: no maximum is the ordinary state, read as 200 with `maximum` null and no ETag; a missing project stays 404.
	if a := e.st.Do(e.r, "GET", path, e.admin, nil); a.Status != 200 || a.JSON(t)["maximum"] != nil || a.Header.Get("ETag") != "" {
		t.Fatalf("a project without a maximum: %d %s (ETag %q)", a.Status, a.Body, a.Header.Get("ETag"))
	}
	if a := e.st.Do(e.r, "GET", "/api/v1/projects/no-such-project/limits/maximum", e.admin, nil); a.Status != 404 {
		t.Fatalf("the maximum of a project that does not exist: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.r, "PUT", path, e.admin, body, "If-Match", `"1"`); a.Status != 412 {
		t.Fatalf("an If-Match where no maximum exists: %d %s", a.Status, a.Body)
	}
	created := e.st.Do(e.r, "PUT", path, e.admin, body)
	if created.Status != 201 || created.Header.Get("ETag") != `"1"` {
		t.Fatalf("the first maximum: %d %s", created.Status, created.Body)
	}
	if a := e.st.Do(e.r, "PUT", path, e.admin, body); a.Status != 428 {
		t.Fatalf("a replace without If-Match: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.r, "PUT", path, e.admin, body, "If-Match", `"7"`); a.Status != 412 {
		t.Fatalf("a stale If-Match: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.r, "PUT", path, e.admin, body, "If-Match", `"1"`); a.Status != 200 {
		t.Fatalf("a replace: %d %s", a.Status, a.Body)
	}
	if items := projects(); len(items) != 1 || items[0].(map[string]any)["project"] != "it" || items[0].(map[string]any)["maximum"] == nil {
		t.Fatalf("the project with its maximum: %v", items)
	}
	read := e.st.Do(e.r, "GET", path, e.admin, nil)
	if m, _ := read.JSON(t)["maximum"].(map[string]any); read.Status != 200 || m["profile"] != "technical" || read.Header.Get("ETag") != `"2"` {
		t.Fatalf("the maximum read back: %d %s (ETag %q)", read.Status, read.Body, read.Header.Get("ETag"))
	}
}
