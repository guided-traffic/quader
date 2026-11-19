package ciworkflows

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The curl stub stands in for the one call of the Actions API: it writes its arguments to STUB_LOG, and either fails as curl
// -f does (exit STUB_CURL_EXIT with a message) or writes the file STUB_DIR/answer to the path after -o.
const curlStub = `#!/usr/bin/env bash
echo "curl $*" >> "$STUB_LOG"
if [ -n "${STUB_CURL_EXIT:-}" ]; then
  echo "curl: (22) The requested URL returned error: 503" >&2
  exit "$STUB_CURL_EXIT"
fi
out=""
while [ $# -gt 0 ]; do
  case $1 in -o) out=$2; shift ;; esac
  shift
done
cp "$STUB_DIR/answer" "$out"
`

// selectRun is one run of select-leg.sh.
type selectRun struct {
	status  int
	output  string // standard output and error: the log of the step
	run     string // the value of run in GITHUB_OUTPUT
	summary string
	calls   []string // the curl calls
}

type selectCase struct {
	event    string // GITHUB_EVENT_NAME
	leg, job string
	sets     string
	nightly  string
	answer   string // the body of the Actions API's answer
	curlExit string // a failed call: curl's exit status
}

func runSelectLeg(t *testing.T, c selectCase) selectRun {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "curl"), []byte(curlStub), 0o755); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "answer"), []byte(c.answer), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(state, "output")
	summary := filepath.Join(state, "summary")
	log := filepath.Join(state, "calls.log")
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "actions", "select-leg", "select-leg.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if c.leg == "" {
		c.leg, c.job = "ha-suite", "e2e-npm"
	}
	if c.nightly == "" {
		c.nightly = "true"
	}
	cmd := exec.Command(bash, script)
	cmd.Env = []string{
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + state,
		"TMPDIR=" + state,
		"LEG=" + c.leg,
		"JOB=" + c.job,
		"SETS=" + c.sets,
		"NIGHTLY=" + c.nightly,
		"TOKEN=stub-token",
		"GITHUB_EVENT_NAME=" + c.event,
		"GITHUB_REPOSITORY=guided-traffic/quader",
		"GITHUB_REF_NAME=main",
		"GITHUB_WORKFLOW_REF=guided-traffic/quader/.github/workflows/performance.yml@refs/heads/main",
		"GITHUB_API_URL=https://api.github.com",
		"GITHUB_OUTPUT=" + output,
		"GITHUB_STEP_SUMMARY=" + summary,
		"STUB_DIR=" + state,
		"STUB_LOG=" + log,
		"STUB_CURL_EXIT=" + c.curlExit,
	}
	out, err := cmd.CombinedOutput()
	r := selectRun{output: string(out)}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		r.status = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(output); err == nil {
		if m := regexp.MustCompile(`(?m)^run=(.*)$`).FindAllStringSubmatch(string(raw), -1); len(m) == 1 {
			r.run = m[0][1]
		} else {
			t.Errorf("GITHUB_OUTPUT holds %d run= lines, want 1: %q", len(m), raw)
		}
	}
	if raw, err := os.ReadFile(summary); err == nil {
		r.summary = string(raw)
	}
	if raw, err := os.ReadFile(log); err == nil {
		r.calls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return r
}

// needPython skips a case that reads the API's answer where python3 is missing; select-leg.sh would run the leg there.
func needPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH")
	}
}

// apiRun is one entry of workflow_runs as the Actions API answers it, the fields the script reads.
type apiRun struct {
	RunNumber    int    `json:"run_number"`
	HTMLURL      string `json:"html_url"`
	Event        string `json:"event"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion,omitempty"`
	DisplayTitle string `json:"display_title"`
	UpdatedAt    string `json:"updated_at"`
}

// fullRun is a dispatch of all legs that ended ago with conclusion.
func fullRun(number int, ago time.Duration, conclusion string) apiRun {
	return apiRun{
		RunNumber:    number,
		HTMLURL:      fmt.Sprintf("https://github.com/guided-traffic/quader/actions/runs/%d", 44414813000+number),
		Event:        "workflow_dispatch",
		Status:       "completed",
		Conclusion:   conclusion,
		DisplayTitle: "HA and Performance (all legs)",
		UpdatedAt:    time.Now().UTC().Add(-ago).Format(time.RFC3339),
	}
}

func answer(t *testing.T, runs ...apiRun) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"total_count": len(runs), "workflow_runs": runs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantRun(t *testing.T, r selectRun, want string) {
	t.Helper()
	if r.status != 0 {
		t.Fatalf("select-leg.sh exited %d:\n%s", r.status, r.output)
	}
	if r.run != want {
		t.Errorf("run=%q, want %q\nlog:\n%s\nsummary:\n%s", r.run, want, r.output, r.summary)
	}
}

func wantNoCall(t *testing.T, r selectRun) {
	t.Helper()
	if len(r.calls) != 0 {
		t.Errorf("the Actions API was called: %q", r.calls)
	}
}

// A dispatch decides by its sets alone, as before the check of the schedule, and never calls the API.
func TestSelectLegADispatchDecidesBySetsAndCallsNoAPI(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		c    selectCase
		run  string
		says string
	}{
		{"empty sets", selectCase{}, "true", ""},
		{"its set", selectCase{sets: "upgrade,ha-suite"}, "true", ""},
		{"its job key", selectCase{sets: "e2e-npm"}, "true", ""},
		{"another set", selectCase{sets: "upgrade"}, "false", "Leg ha-suite of job e2e-npm is not in the sets of this dispatch (upgrade): its steps are skipped."},
		{
			"a leg off the nightly, empty sets",
			selectCase{leg: "upgrade-release", job: "e2e-upgrade", nightly: "false"},
			"false",
			"Leg upgrade-release of job e2e-upgrade runs only on a dispatch that names it or e2e-upgrade: its steps are skipped.",
		},
		{"a leg off the nightly, named", selectCase{leg: "upgrade-release", job: "e2e-upgrade", nightly: "false", sets: "upgrade-release"}, "true", ""},
	} {
		c.c.event = "workflow_dispatch"
		// an answer that would skip the leg, had the script asked
		c.c.answer = answer(t, fullRun(400, time.Hour, "success"))
		r := runSelectLeg(t, c.c)
		wantRun(t, r, c.run)
		wantNoCall(t, r)
		if strings.TrimSpace(r.summary) != c.says {
			t.Errorf("%s: summary %q, want %q", c.name, r.summary, c.says)
		}
	}
}

func TestSelectLegRefusesSetsWithASpaceAndAnUnknownNightly(t *testing.T) {
	t.Parallel()
	for _, c := range []selectCase{
		{event: "workflow_dispatch", sets: "upgrade, ha-suite"},
		{event: "schedule", nightly: "yes"},
	} {
		r := runSelectLeg(t, c)
		if r.status != 1 || !strings.Contains(r.output, "::error::") {
			t.Errorf("%+v: exit %d, want 1 with an error:\n%s", c, r.status, r.output)
		}
	}
}

// A dispatch of all legs that ended 3 hours ago is the night's run: the scheduled leg is skipped and names it.
func TestSelectLegTheScheduleSkipsAfterAFullRunThreeHoursAgo(t *testing.T) {
	t.Parallel()
	needPython(t)
	night := fullRun(397, 3*time.Hour, "success")
	r := runSelectLeg(t, selectCase{event: "schedule", answer: answer(t,
		apiRun{
			RunNumber: 398, HTMLURL: "https://example.invalid/398", Event: "workflow_dispatch", Status: "completed", Conclusion: "success",
			DisplayTitle: "HA and Performance (sets: upgrade,vuln-long)", UpdatedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		},
		night,
		fullRun(390, 10*time.Hour, "failure"),
	)})
	wantRun(t, r, "false")
	for _, w := range []string{"Leg ha-suite of job e2e-npm is skipped", "defers to run #397 (" + night.HTMLURL + ")", "ended at " + night.UpdatedAt, "3.0 hours ago", "less than 12 hours"} {
		if !strings.Contains(r.summary, w) {
			t.Errorf("the summary lacks %q:\n%s", w, r.summary)
		}
		if !strings.Contains(r.output, w) {
			t.Errorf("the log lacks %q:\n%s", w, r.output)
		}
	}
	if len(r.calls) != 1 {
		t.Fatalf("%d calls of the Actions API, want 1: %q", len(r.calls), r.calls)
	}
	for _, w := range []string{
		"https://api.github.com/repos/guided-traffic/quader/actions/workflows/performance.yml/runs?branch=main&event=workflow_dispatch&status=completed&per_page=100",
		"Authorization: Bearer stub-token",
		"--max-time 30",
	} {
		if !strings.Contains(r.calls[0], w) {
			t.Errorf("the call lacks %q: %s", w, r.calls[0])
		}
	}
}

// Red legs do not matter: a full run that ended red is still the night's run.
func TestSelectLegTheScheduleSkipsAfterARedFullRun(t *testing.T) {
	t.Parallel()
	needPython(t)
	r := runSelectLeg(t, selectCase{event: "schedule", answer: answer(t, fullRun(397, 6*time.Hour, "failure"))})
	wantRun(t, r, "false")
}

// The cases in which the scheduled leg runs, each with an answer the script reads.
func TestSelectLegTheScheduleRunsWithoutARecentFullRun(t *testing.T) {
	t.Parallel()
	needPython(t)
	partial := fullRun(398, time.Hour, "success")
	partial.DisplayTitle = "HA and Performance (sets: upgrade,vuln-long)"
	cancelled := fullRun(397, time.Hour, "cancelled")
	inProgress := fullRun(399, 0, "")
	inProgress.Status = "in_progress"
	deferred := fullRun(396, 2*time.Hour, "success")
	deferred.Event = "schedule"
	for name, runs := range map[string][]apiRun{
		"a full run ended 13 hours ago": {fullRun(390, 13*time.Hour, "success")},
		"only a partial run":            {partial},
		"a cancelled full run":          {cancelled},
		"a full run still in progress":  {inProgress},
		"a scheduled run of all legs":   {deferred},
		"no run at all":                 {},
		"a full run that did not start": {fullRun(395, time.Hour, "startup_failure")},
	} {
		r := runSelectLeg(t, selectCase{event: "schedule", answer: answer(t, runs...)})
		wantRun(t, r, "true")
		if len(r.calls) != 1 {
			t.Errorf("%s: %d calls of the Actions API, want 1", name, len(r.calls))
		}
		if !strings.Contains(r.output, "No full run of this workflow on main ended in the last 12 hours: leg ha-suite runs.") {
			t.Errorf("%s: the log does not say why the leg runs:\n%s", name, r.output)
		}
		if strings.Contains(r.output, "::warning::") {
			t.Errorf("%s: a warning for an answer that was read:\n%s", name, r.output)
		}
	}
}

// Fail open: a call that fails or an answer that cannot be read runs the leg, with a warning that says why.
func TestSelectLegTheScheduleRunsWhenTheCheckCannotDecide(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		c    selectCase
		says string
	}{
		"the API fails":         {selectCase{curlExit: "22"}, "the Actions API did not answer: curl: (22) The requested URL returned error: 503"},
		"an answer of no JSON":  {selectCase{answer: "<html>Unicorn</html>"}, "the answer of the Actions API could not be read"},
		"no workflow_runs":      {selectCase{answer: `{"message":"Not Found"}`}, "the answer of the Actions API could not be read: KeyError"},
		"a run without a field": {selectCase{answer: `{"workflow_runs":[{"event":"workflow_dispatch"}]}`}, "the answer of the Actions API could not be read: KeyError"},
		"a time that is no time": {
			selectCase{answer: `{"workflow_runs":[{"event":"workflow_dispatch","status":"completed","conclusion":"success",` +
				`"display_title":"HA and Performance (all legs)","updated_at":"yesterday","run_number":1,"html_url":"u"}]}`},
			"the answer of the Actions API could not be read: ValueError",
		},
	} {
		if name != "the API fails" {
			needPython(t)
		}
		c.c.event = "schedule"
		r := runSelectLeg(t, c.c)
		wantRun(t, r, "true")
		if !strings.Contains(r.output, "::warning::") || !strings.Contains(r.output, c.says) {
			t.Errorf("%s: the log lacks a warning with %q:\n%s", name, c.says, r.output)
		}
		if !strings.Contains(r.summary, "leg ha-suite of job e2e-npm runs") {
			t.Errorf("%s: the summary does not say the leg runs:\n%s", name, r.summary)
		}
	}
}

// A leg the schedule does not run anyway costs no call.
func TestSelectLegTheScheduleCallsNothingForALegOffTheNightly(t *testing.T) {
	t.Parallel()
	r := runSelectLeg(t, selectCase{event: "schedule", leg: "upgrade-release", job: "e2e-upgrade", nightly: "false"})
	wantRun(t, r, "false")
	wantNoCall(t, r)
}

// The script knows a full run by its run name: the workflow's run-name gives "HA and Performance (all legs)" to a run without
// sets, and every job of the workflow passes the token to select-leg and may read the Actions runs.
func TestThePerformanceWorkflowNamesItsFullRunsAndPassesTheToken(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "performance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	runName := regexp.MustCompile(`(?m)^run-name: (.*)$`).FindStringSubmatch(text)
	if runName == nil || !strings.Contains(runName[1], "'HA and Performance (all legs)'") || !strings.Contains(runName[1], "'HA and Performance (sets: {0})'") {
		t.Errorf("performance.yml has no run-name that names a full run 'HA and Performance (all legs)': %q", runName)
	}
	w := loadWorkflows(t)["performance.yml"]
	for name, j := range w.Jobs {
		var sel *step
		for i := range j.Steps {
			if j.Steps[i].Uses == "./.github/actions/select-leg" {
				sel = &j.Steps[i]
			}
		}
		if sel == nil {
			t.Errorf("job %s does not start with select-leg, so the schedule's check cannot skip it", name)
			continue
		}
		if sel.With["token"] != "${{ github.token }}" {
			t.Errorf("job %s passes no token to select-leg: %v", name, sel.With)
		}
		if j.Permissions != nil && j.Permissions["actions"] != "read" {
			t.Errorf("job %s sets its own permissions without actions: read: %v", name, j.Permissions)
		}
	}
	if !regexp.MustCompile(`(?m)^permissions:\n(  \w+: \w+\n)*  actions: read\n`).MatchString(text) {
		t.Error("performance.yml does not grant actions: read")
	}
}
