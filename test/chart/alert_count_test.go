//go:build chart

package chart_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rulesAlertCounts counts the alerts of templates/_rules.tpl: all of them, and those outside every {{- if}} block, which render
// whatever switch is set.
func rulesAlertCounts(t *testing.T) (all, always int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(chartDir, "templates", "_rules.tpl"))
	if err != nil {
		t.Fatal(err)
	}
	depth := 0
	for _, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "{{- if ") || strings.HasPrefix(l, "{{ if "):
			depth++
		case (strings.HasPrefix(l, "{{- end") || strings.HasPrefix(l, "{{ end")) && depth > 0:
			depth--
		case strings.HasPrefix(l, "- alert: "):
			all++
			if depth == 0 {
				always++
			}
		}
	}
	return all, always
}

// TestR_HA_045_TheDocumentsCountTheAlerts holds the numbers of alerts that the README and the chart's developer page state to the
// rules the chart ships. It reads three files and renders nothing (milliseconds).
func TestR_HA_045_TheDocumentsCountTheAlerts(t *testing.T) {
	all, always := rulesAlertCounts(t)
	if all != len(alertSeverities()) {
		t.Fatalf("_rules.tpl holds %d alerts, alertSeverities %d", all, len(alertSeverities()))
	}
	cases := []struct {
		name, file string
		re         *regexp.Regexp
		want       []int
	}{
		{"the README's chart objects", "README.md", regexp.MustCompile(`with ` + "`metrics.prometheusRule.enabled`" + `[^;]*; (\d+) alerts: (\d+) always`), []int{all, always}},
		{"the README's values", "README.md", regexp.MustCompile(`# R-HA-045; (\d+) alerts, of which`), []int{all}},
		{"the chart page's objects", "docs/developer/helm-chart.md", regexp.MustCompile(`PrometheusRule with (\d+) alerts of .*?: (\d+) always`), []int{all, always}},
		{"the chart page's PrometheusRule", "docs/developer/helm-chart.md", regexp.MustCompile(`the rule holds the (\d+) alerts that render always`), []int{always}},
		{"the chart page's tests", "docs/developer/helm-chart.md", regexp.MustCompile(`the (\d+) alerts and their severities`), []int{all}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repoRoot, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			m := tc.re.FindAllStringSubmatch(string(raw), -1)
			if len(m) != 1 {
				t.Fatalf("%d matches in %s, want 1: the sentence that states the count moved; point this test at it", len(m), tc.file)
			}
			for i, w := range tc.want {
				if got, _ := strconv.Atoi(m[0][i+1]); got != w {
					t.Errorf("%s says %d, the chart ships %d (all %d, always %d)", tc.file, got, w, all, always)
				}
			}
		})
	}
}
