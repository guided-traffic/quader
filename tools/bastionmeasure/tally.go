package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// rec is one log=request record of quader (R-OPS-010): its end, its length and what the measurement reads of it.
type rec struct {
	Time          time.Time `json:"time"`
	Log           string    `json:"log"`
	Listener      string    `json:"listener"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	Status        int       `json:"status"`
	ResponseBytes int64     `json:"response_bytes"`
	DurationMS    float64   `json:"duration_ms"`
	UserAgent     string    `json:"user_agent"`
	PrincipalType string    `json:"principal_type"`
	Class         string    `json:"bastion_class"`
	Result        string    `json:"bastion_result"`
}

func (r rec) start() time.Time {
	return r.Time.Add(-time.Duration(r.DurationMS * float64(time.Millisecond)))
}

// readRecs reads the client listener's request records of a log whose start lies in [from, to).
func readRecs(file string, from, to time.Time, prefix string) ([]rec, error) {
	f, err := os.Open(file) //nolint:gosec // a log of the measurement, named by the caller
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []rec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"log":"request"`) {
			continue
		}
		var r rec
		if json.Unmarshal(line, &r) != nil || r.Listener != "client" {
			continue
		}
		if s := r.start(); s.Before(from) || !s.Before(to) || (prefix != "" && !strings.HasPrefix(r.Path, prefix) && !strings.HasPrefix(r.Path, "/api/v1/bastion/")) {
			continue
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// peak is the most records in progress at one moment.
func peak(rs []rec) int {
	type ev struct {
		t time.Time
		d int
	}
	evs := make([]ev, 0, 2*len(rs))
	for _, r := range rs {
		evs = append(evs, ev{r.start(), 1}, ev{r.Time, -1})
	}
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].t.Equal(evs[j].t) {
			return evs[i].d < evs[j].d
		}
		return evs[i].t.Before(evs[j].t)
	})
	cur, top := 0, 0
	for _, e := range evs {
		cur += e.d
		top = max(top, cur)
	}
	return top
}

// mainKind sorts a request of the Bastion at the main installation: the interface's own operations, a credential check, a
// registry token, a pass route (search, catalogue, whoami), a conditional request answered 304, or the forward of a miss.
func mainKind(r rec) string {
	p := r.Path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	switch {
	case p == "/api/v1/bastion/check":
		return "check"
	case strings.HasPrefix(p, "/api/v1/bastion/"):
		return "iface:" + strings.TrimPrefix(p, "/api/v1/bastion/")
	case strings.HasSuffix(p, "/_token") || strings.HasSuffix(p, "/v2/token"):
		return "token"
	case strings.Contains(p, "/-/v1/search") || strings.Contains(p, "/v3/search") || strings.Contains(p, "/_catalog") ||
		strings.Contains(p, "/-/whoami") || strings.Contains(p, "/search/"):
		return "pass"
	case r.Status == 304:
		return "conditional"
	}
	return "forward"
}

type summary struct {
	Label           string         `json:"label"`
	WallSeconds     float64        `json:"wallSeconds"`
	BastionRequests int            `json:"bastionRequests"`
	BastionStatus   map[string]int `json:"bastionStatus"`
	BastionResults  map[string]int `json:"bastionResults"`
	BytesToClient   int64          `json:"bytesToClient"`
	PeakAtBastion   int            `json:"peakAtBastion"`
	MissesFirstMin  int            `json:"missesFirstMinute"`
	MainRequests    int            `json:"mainRequests"`
	MainKinds       map[string]int `json:"mainKinds"`
	BytesFromMain   int64          `json:"bytesFromMain"`
	PeakAtMain      int            `json:"peakAtMain"`
	Metrics         map[string]int `json:"metricsDelta,omitempty"`
}

func tally(args []string) error {
	fs := flag.NewFlagSet("tally", flag.ContinueOnError)
	bastionLog := fs.String("bastion-log", "", "the Bastion's log")
	mainLog := fs.String("main-log", "", "the main installation's log")
	fromS := fs.String("from", "", "start of the window, RFC 3339")
	toS := fs.String("to", "", "end of the window, RFC 3339")
	label := fs.String("label", "", "the run's name")
	before := fs.String("metrics-before", "", "the Bastion's metrics before the run")
	after := fs.String("metrics-after", "", "the Bastion's metrics after the run")
	lat := fs.Bool("latency", false, "print the main installation's latency per check and per 304 instead")
	prefix := fs.String("path-prefix", "", "count only requests below this path (and the interface's own)")
	paths := fs.Bool("paths", false, "list each request of the window at the Bastion and at the main installation instead")
	if err := fs.Parse(args); err != nil {
		return err
	}
	from, err := time.Parse(time.RFC3339Nano, *fromS)
	if err != nil {
		return err
	}
	to, err := time.Parse(time.RFC3339Nano, *toS)
	if err != nil {
		return err
	}
	mrs, err := readRecs(*mainLog, from, to, *prefix)
	if err != nil {
		return err
	}
	var fromBastion []rec
	for _, r := range mrs {
		if r.PrincipalType == "bastion" || strings.HasPrefix(r.UserAgent, "quader-bastion/") {
			fromBastion = append(fromBastion, r)
		}
	}
	if *lat {
		return printLatency(*label, fromBastion)
	}
	brs, err := readRecs(*bastionLog, from, to, *prefix)
	if err != nil {
		return err
	}
	if *paths {
		for _, r := range brs {
			fmt.Printf("bastion %s %s %s %d %s/%s %d\n", r.start().Format("15:04:05.000"), r.Method, r.Path, r.Status, r.Class, r.Result, r.ResponseBytes)
		}
		for _, r := range fromBastion {
			if k := mainKind(r); k != "iface:feed" && k != "iface:handshake" {
				fmt.Printf("main    %s %s %s %d %s %d %.1fms\n", r.start().Format("15:04:05.000"), r.Method, r.Path, r.Status, k, r.ResponseBytes, r.DurationMS)
			}
		}
		return nil
	}
	s := summary{Label: *label, BastionStatus: map[string]int{}, BastionResults: map[string]int{}, MainKinds: map[string]int{}}
	var first, last time.Time
	for _, r := range brs {
		s.BastionRequests++
		s.BastionStatus[strconv.Itoa(r.Status)]++
		s.BastionResults[r.Class+"/"+r.Result]++
		s.BytesToClient += r.ResponseBytes
		if first.IsZero() || r.start().Before(first) {
			first = r.start()
		}
		if r.Time.After(last) {
			last = r.Time
		}
	}
	for _, r := range brs {
		if r.Result == "miss" && r.start().Sub(first) < time.Minute {
			s.MissesFirstMin++
		}
	}
	if !first.IsZero() {
		s.WallSeconds = math.Round(last.Sub(first).Seconds()*100) / 100
	}
	s.PeakAtBastion = peak(brs)
	var served []rec
	for _, r := range fromBastion {
		k := mainKind(r)
		if k == "iface:feed" || k == "iface:handshake" {
			continue
		}
		s.MainKinds[k]++
		if strings.HasPrefix(k, "iface:") {
			continue
		}
		s.MainRequests++
		s.BytesFromMain += r.ResponseBytes
		served = append(served, r)
	}
	s.PeakAtMain = peak(served)
	if *before != "" && *after != "" {
		if s.Metrics, err = metricsDelta(*before, *after); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(s)
	fmt.Println(string(b))
	return nil
}

func printLatency(label string, rs []rec) error {
	byKind := map[string][]float64{}
	for _, r := range rs {
		k := mainKind(r)
		if k == "check" || k == "conditional" || k == "forward" || k == "token" {
			byKind[k] = append(byKind[k], r.DurationMS)
		}
	}
	out := map[string]any{"label": label}
	for k, v := range byKind {
		sort.Float64s(v)
		out[k] = map[string]any{"n": len(v), "p50": pct(v, 0.50), "p95": pct(v, 0.95), "p99": pct(v, 0.99), "max": v[len(v)-1]}
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	return nil
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

// metricsDelta is the growth of the Bastion's counters of interest between two scrapes, by series.
func metricsDelta(beforeFile, afterFile string) (map[string]int, error) {
	b, err := scrape(beforeFile)
	if err != nil {
		return nil, err
	}
	a, err := scrape(afterFile)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for k, v := range a {
		if d := v - b[k]; d != 0 {
			out[k] = int(d)
		}
	}
	return out, nil
}

// counted reports whether a line of a scrape is a series of a counter the tally reads.
func counted(line string) bool {
	for _, p := range []string{
		"quader_bastion_main_requests_total", "quader_bastion_fill_bytes_total", "quader_bastion_bytes_served_total",
		"quader_bastion_refused_total", "quader_bastion_cache_evictions_total", "quader_bastion_main_budget_exhausted_total",
		"quader_bastion_cache_no_room_total",
	} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func scrape(file string) (map[string]float64, error) {
	raw, err := os.ReadFile(file) //nolint:gosec // a scrape of the measurement, named by the caller
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !counted(line) {
			continue
		}
		i := strings.LastIndexByte(line, ' ')
		if i < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[i+1:], 64)
		if err != nil {
			continue
		}
		key := strings.ReplaceAll(line[:i], `bastion="e4-bastion",`, "")
		if strings.Contains(key, `kind="feed"`) || strings.Contains(key, `kind="handshake"`) {
			continue
		}
		out[key] = v
	}
	return out, nil
}
