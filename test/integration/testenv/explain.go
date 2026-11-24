package testenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// QueryText returns the text of the sqlc statement name of internal/meta/postgres/queries/<file>, its arguments written in from args
// (argument name to an SQL literal: "'library/alpine'", "64", "'{}'"), for a test that reads the plan of a statement as the application
// runs it (Stack.RowsRead). An argument without a value fails the test.
func QueryText(tb testing.TB, file, name string, args map[string]string) string {
	tb.Helper()
	_, self, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "..", "internal", "meta", "postgres", "queries", file)) //nolint:gosec // G304: a query file of the repository a test names
	if err != nil {
		tb.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, "-- name: "+name+" ")
	if i < 0 {
		tb.Fatalf("%s holds no statement %s", file, name)
	}
	s = s[i:]
	if j := strings.Index(s[1:], "-- name: "); j >= 0 {
		s = s[:j+1]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), ";")
	arg := regexp.MustCompile(`sqlc\.arg\((\w+)\)`)
	return arg.ReplaceAllStringFunc(s, func(m string) string {
		k := arg.FindStringSubmatch(m)[1]
		v, ok := args[k]
		if !ok {
			tb.Fatalf("%s: no value for the argument %s", name, k)
		}
		return v
	})
}

// RowsRead runs a statement under EXPLAIN (ANALYZE, FORMAT JSON) and returns, per relation, the rows its scan nodes produced and those
// their filters removed, summed over their loops: what a statement read of a table, which a test bounds by the rows it asked for.
func (s *Stack) RowsRead(sql string) map[string]float64 {
	s.tb.Helper()
	var doc []struct {
		Plan json.RawMessage `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(s.Text("EXPLAIN (ANALYZE, FORMAT JSON) "+sql)), &doc); err != nil || len(doc) != 1 {
		s.tb.Fatalf("the plan of %s: %v", sql, err)
	}
	out := map[string]float64{}
	var walk func(raw json.RawMessage)
	walk = func(raw json.RawMessage) {
		var n struct {
			Relation  string            `json:"Relation Name"`
			Rows      float64           `json:"Actual Rows"`
			Loops     float64           `json:"Actual Loops"`
			Filtered  float64           `json:"Rows Removed by Filter"`
			Rechecked float64           `json:"Rows Removed by Index Recheck"`
			Plans     []json.RawMessage `json:"Plans"`
		}
		if json.Unmarshal(raw, &n) != nil {
			return
		}
		if n.Relation != "" {
			out[n.Relation] += (n.Rows + n.Filtered + n.Rechecked) * n.Loops
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(doc[0].Plan)
	return out
}

// SubPlanLoops runs a statement under EXPLAIN (ANALYZE, FORMAT JSON) and returns the most loops any of its subplans ran: a subplan per
// row of a table runs once per row, which a test bounds by the rows the statement returns.
func (s *Stack) SubPlanLoops(sql string) float64 {
	s.tb.Helper()
	var doc []struct {
		Plan json.RawMessage `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(s.Text("EXPLAIN (ANALYZE, FORMAT JSON) "+sql)), &doc); err != nil || len(doc) != 1 {
		s.tb.Fatalf("the plan of %s: %v", sql, err)
	}
	most := 0.0
	var walk func(raw json.RawMessage)
	walk = func(raw json.RawMessage) {
		var n struct {
			Parent string            `json:"Parent Relationship"`
			Loops  float64           `json:"Actual Loops"`
			Plans  []json.RawMessage `json:"Plans"`
		}
		if json.Unmarshal(raw, &n) != nil {
			return
		}
		if n.Parent == "SubPlan" {
			most = max(most, n.Loops)
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(doc[0].Plan)
	return most
}
