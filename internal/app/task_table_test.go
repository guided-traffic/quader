package app

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

// taskRow is one task of the table of R-OPS-019: its name, whether the row says "specified, not built", and the default schedule the
// row names for it ("" for manual only).
type taskRow struct {
	name     string
	notBuilt bool
	schedule string
	manual   bool
}

// r019Tasks reads the task table of R-OPS-019 from the specification: the backquoted names of the first column (a row may name two,
// `token-cleanup`, `session-cleanup`), the words "specified, not built" anywhere in the row, and the backquoted schedules of the third
// column, one per name in the order of the names, or "manual only".
func r019Tasks(t *testing.T) []taskRow {
	t.Helper()
	raw, err := os.ReadFile("../../docs/spec/operations-and-observability.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "### R-OPS-019 ")
	if start < 0 {
		t.Fatal("R-OPS-019 is not in docs/spec/operations-and-observability.md")
	}
	text = text[start:]
	tbl := strings.Index(text, "| Task | Semantics owned by | Default schedule |")
	if tbl < 0 {
		t.Fatal("the task table of R-OPS-019 moved; update the test")
	}
	tick := regexp.MustCompile("`([^`]+)`")
	var out []taskRow
	for _, line := range strings.Split(text[tbl:], "\n")[2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			t.Fatalf("a row of the task table without three columns: %s", line)
		}
		notBuilt := strings.Contains(line, "specified, not built")
		manual := strings.Contains(cells[3], "manual only")
		var schedules []string
		for _, m := range tick.FindAllStringSubmatch(cells[3], -1) {
			schedules = append(schedules, m[1])
		}
		names := tick.FindAllStringSubmatch(cells[1], -1)
		if !manual && len(schedules) != len(names) {
			t.Fatalf("the row %q names %d tasks and %d schedules", strings.TrimSpace(cells[1]), len(names), len(schedules))
		}
		for i, m := range names {
			r := taskRow{name: m[1], notBuilt: notBuilt, manual: manual}
			if !manual {
				r.schedule = schedules[i]
			}
			out = append(out, r)
		}
	}
	if len(out) < 30 {
		t.Fatalf("read only %d tasks from the table: %v", len(out), out)
	}
	return out
}

// defaultConfig is the configuration of a replica of the role all with every default.
func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	return loadCfg(t, config.RoleAll, baseEnv(t, nil)).Config
}

// TestR_OPS_019_TheTaskTableIsTheRegistry: the task table of R-OPS-019 and the tasks a replica registers are the same set (Q-OPS-418).
// A registered task has a row whose default schedule is the one the registry resolves with the default configuration ("manual only"
// for a manual-only task); a row of a task no replica registers says "specified, not built", and a row that says so of a registered
// task fails until the words go; where the configuration's default schedules a task the table marks, the row names that schedule.
// Every role registers the same tasks (jobRegistry), so the role all stands for each. Unit, about 10 ms, the specification page.
func TestR_OPS_019_TheTaskTableIsTheRegistry(t *testing.T) {
	cfg := defaultConfig(t)
	sub, err := SubsystemsFor(config.RoleAll)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := (&App{cfg: cfg, sub: sub}).jobRegistry()
	if err != nil {
		t.Fatal(err)
	}
	infos, err := reg.ResolveTasks(cfg.Maintenance)
	if err != nil {
		t.Fatal(err)
	}
	type resolved struct {
		schedule string
		manual   bool
	}
	registered := map[string]resolved{}
	for _, ti := range infos {
		registered[ti.Name] = resolved{schedule: ti.Schedule, manual: ti.ManualOnly}
	}
	rows := map[string]taskRow{}
	for _, r := range r019Tasks(t) {
		if _, dup := rows[r.name]; dup {
			t.Errorf("%s has two rows", r.name)
		}
		rows[r.name] = r
		got, ok := registered[r.name]
		switch {
		case !ok && !r.notBuilt:
			t.Errorf("%s is a row of R-OPS-019 that no replica registers: register it, or say \"specified, not built\" in its row", r.name)
		case ok && r.notBuilt:
			t.Errorf("%s is registered, and its row of R-OPS-019 says \"specified, not built\": strike the words, and drop it from tasksNotBuilt", r.name)
		case ok && r.manual != got.manual:
			t.Errorf("%s: the row says manual only %v, the registry %v", r.name, r.manual, got.manual)
		case ok && !r.manual && r.schedule != got.schedule:
			t.Errorf("%s: the row names the default schedule %q, the registry runs %q", r.name, r.schedule, got.schedule)
		case !ok:
			if c, has := cfg.Maintenance.Tasks[r.name]; has && c.Schedule != r.schedule {
				t.Errorf("%s: the row names %q, the configuration's default %q", r.name, r.schedule, c.Schedule)
			}
		}
	}
	var missing []string
	for name := range registered {
		if _, ok := rows[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s is registered and has no row in the table of R-OPS-019", name)
	}
	for _, name := range tasksNotBuilt() {
		if r, ok := rows[name]; !ok || !r.notBuilt {
			t.Errorf("tasksNotBuilt names %s, which the table of R-OPS-019 does not mark \"specified, not built\"", name)
		}
	}
}

// TestR_OPS_019_AKeyOfNoTaskStopsTheStart: a key of maintenance.tasks that names a task this version does not have is refused where the
// replica builds its job registry, with the key and the tasks there are (Q-OPS-418); the default configuration starts, and so does one
// that names the two tasks of the remote curation's queue before they are registered (R-CUR-020). Unit, milliseconds.
func TestR_OPS_019_AKeyOfNoTaskStopsTheStart(t *testing.T) {
	cfg := defaultConfig(t)
	sub, err := SubsystemsFor(config.RoleAll)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&App{cfg: cfg, sub: sub}).jobRegistry(); err != nil {
		t.Fatalf("the default configuration: %v", err)
	}
	ahead := *cfg
	ahead.Maintenance.Tasks = map[string]config.MaintenanceTask{}
	for _, name := range tasksNotBuilt() {
		ahead.Maintenance.Tasks[name] = config.MaintenanceTask{Enabled: true, Schedule: "* * * * *"}
	}
	if _, err := (&App{cfg: &ahead, sub: sub}).jobRegistry(); err != nil {
		t.Fatalf("the curation's tasks ahead of their registration: %v", err)
	}
	for _, name := range []string{"gc_full", "remote-cache-cleanup", "user-dormancy"} {
		bad := *cfg
		bad.Maintenance.Tasks = map[string]config.MaintenanceTask{"gc": {Enabled: true, Schedule: "*/30 * * * *"}, name: {Enabled: false}}
		_, err := (&App{cfg: &bad, sub: sub}).jobRegistry()
		if err == nil || !strings.Contains(err.Error(), "maintenance.tasks."+name+":") || !strings.Contains(err.Error(), "gc-full") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
