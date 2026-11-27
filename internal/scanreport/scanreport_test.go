package scanreport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/config"
)

func valid() Report {
	return Report{
		Name: "quader-malware-scanner-clamav-6d9c7b5f4-x2v4q", Node: "pool-a-7x2k", Version: "0.42.0", Revision: "4f1c2a9e0d7b",
		State: StateStarting, Reason: "database", ToolVersion: "1.5.5", Database: "main-62.daily-27841.bytecode-339",
		Slots: Slots{InUse: 1, Total: 2},
	}
}

// R-OPS-048, Q-OPS-223: nothing a scanner reports is trusted beyond its size and shape: bounded strings of a fixed character set, so no
// markup reaches a screen, a known state, slots within the configuration's bounds, and a database only from the kind that has one.
func TestR_OPS_048_AReportIsCheckedForItsSizeAndShape(t *testing.T) {
	if err := valid().Validate(config.ScannerKindClamav); err != nil {
		t.Fatalf("a valid report: %v", err)
	}
	trivy := valid()
	trivy.Database = ""
	if err := trivy.Validate(config.ScannerKindTrivy); err != nil {
		t.Fatalf("a valid report of trivy: %v", err)
	}
	long := strings.Repeat("a", 254)
	for name, change := range map[string]func(*Report){
		"markup in the name":        func(r *Report) { r.Name = "<img src=x onerror=alert(1)>" },
		"an empty name":             func(r *Report) { r.Name = "" },
		"a name of 254 characters":  func(r *Report) { r.Name = long },
		"a name with a space":       func(r *Report) { r.Name = "pod one" },
		"a node with a quote":       func(r *Report) { r.Node = `node"` },
		"a version of 65":           func(r *Report) { r.Version = strings.Repeat("1", 65) },
		"no version":                func(r *Report) { r.Version = "" },
		"an unknown state":          func(r *Report) { r.State = "fine" },
		"a reason with a hyphen":    func(r *Report) { r.Reason = "no-database" },
		"a reason as a sentence":    func(r *Report) { r.Reason = "no database loaded yet" },
		"a tool version with HTML":  func(r *Report) { r.ToolVersion = "1.5<b>" },
		"a database of 129":         func(r *Report) { r.Database = strings.Repeat("d", 129) },
		"no slot":                   func(r *Report) { r.Slots = Slots{InUse: 0, Total: 0} },
		"more slots in use":         func(r *Report) { r.Slots = Slots{InUse: 3, Total: 2} },
		"65 slots":                  func(r *Report) { r.Slots = Slots{InUse: 0, Total: 65} },
		"slots in use below nought": func(r *Report) { r.Slots = Slots{InUse: -1, Total: 2} },
	} {
		r := valid()
		change(&r)
		if err := r.Validate(config.ScannerKindClamav); err == nil {
			t.Errorf("%s: admitted", name)
		}
	}
	if err := valid().Validate(config.ScannerKindTrivy); err == nil {
		t.Error("a signature database's version from a scanner of the kind trivy: admitted")
	}
}

// R-OPS-048, Q-OPS-225: an image scanner reports the newest database its pod holds as the 64 hexadecimal digits of its layer's digest,
// with the build time of its metadata.json; anything else of the kind trivy, and a build time without a database, is refused.
func TestR_OPS_048_AnImageScannerReportsTheDatabaseItDetectsWith(t *testing.T) {
	built := time.Date(2026, 11, 21, 7, 2, 51, 0, time.UTC)
	trivy := valid()
	trivy.Reason, trivy.Database, trivy.DatabaseBuiltAt = "", strings.Repeat("ab", 32), &built
	if err := trivy.Validate(config.ScannerKindTrivy); err != nil {
		t.Fatalf("a valid report of an image scanner with its database: %v", err)
	}
	for name, change := range map[string]func(*Report){
		"a digest with its prefix":   func(r *Report) { r.Database = "sha256:" + strings.Repeat("ab", 32) },
		"63 digits":                  func(r *Report) { r.Database = strings.Repeat("a", 63) },
		"upper-case digits":          func(r *Report) { r.Database = strings.Repeat("AB", 32) },
		"a build time and no digest": func(r *Report) { r.Database = "" },
	} {
		r := trivy
		change(&r)
		if err := r.Validate(config.ScannerKindTrivy); err == nil {
			t.Errorf("%s: admitted", name)
		}
	}
	clamav := valid()
	clamav.DatabaseBuiltAt = &built
	if err := clamav.Validate(config.ScannerKindClamav); err == nil {
		t.Error("a build time from the kind clamav: admitted")
	}
}

// R-OPS-048, Q-OPS-224: the updater reports the set its directory holds with the daily's build time and no slots; a slot from it, a build
// time from another kind, and a build time without a set are refused.
func TestR_OPS_048_TheUpdaterReportsItsSetAndNoSlots(t *testing.T) {
	built := time.Date(2026, 11, 21, 8, 29, 0, 0, time.UTC)
	upd := valid()
	upd.Name, upd.Reason, upd.Slots, upd.DatabaseBuiltAt = "quader-malware-updater-freshclam-7c9d8-k2l4m", "seed", Slots{}, &built
	if err := upd.Validate(config.ScannerKindFreshclam); err != nil {
		t.Fatalf("a valid report of the updater: %v", err)
	}
	slots, noSet, clamav := upd, upd, valid()
	slots.Slots = Slots{InUse: 0, Total: 1}
	noSet.Database = ""
	clamav.DatabaseBuiltAt = &built
	for name, c := range map[string]struct {
		r    Report
		kind string
	}{
		"slots from the updater":      {slots, config.ScannerKindFreshclam},
		"a build time without a set":  {noSet, config.ScannerKindFreshclam},
		"a build time from clamav":    {clamav, config.ScannerKindClamav},
		"the updater's report as one": {upd, config.ScannerKindClamav},
	} {
		if err := c.r.Validate(c.kind); err == nil {
			t.Errorf("%s: admitted", name)
		}
	}
}

func TestR_OPS_048_DecodeIsStrictAndBounded(t *testing.T) {
	b, _ := json.Marshal(valid())
	if _, err := Decode(strings.NewReader(string(b))); err != nil {
		t.Fatalf("a report: %v", err)
	}
	for name, body := range map[string]string{
		"an unknown member":   `{"name":"a","version":"1","revision":"","state":"ready","slots":{"inUse":0,"total":1},"kind":"clamav"}`,
		"two values":          string(b) + string(b),
		"more than the bound": `{"name":"` + strings.Repeat("a", MaxBodyBytes) + `"}`,
		"no JSON":             "<html>",
	} {
		if _, err := Decode(strings.NewReader(body)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
	if !ValidUID("8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b") || ValidUID("8A0B5F8E-3C1D-4F2A-9B7E-1C2D3E4F5A6B") || ValidUID("x") || ValidUID("../x") {
		t.Error("ValidUID admits only a lower-case UUID")
	}
}

// R-OPS-048: the reporter puts the report at once and every interval, says draining at once after Drain, and signs off with DELETE.
func TestR_OPS_048_TheReporterReportsDrainsAndSignsOff(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != Path("8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b") {
			t.Errorf("path %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			var rep Report
			if err := json.Unmarshal(body, &rep); err != nil || rep.Validate(config.ScannerKindClamav) != nil {
				t.Errorf("the report %s: %v", body, err)
			}
			seen = append(seen, rep.State+":"+rep.Reason)
		case http.MethodDelete:
			seen = append(seen, "signed off")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	r := NewReporter(Options{
		URL: srv.URL + "/", HTTP: srv.Client(), Interval: time.Hour,
		Identity: Identity{UID: "8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b", Name: "pod-1", Version: "0.42.0", Revision: "abc"},
		Status: func(context.Context) Status {
			return Status{State: StateStarting, Reason: "database", ToolVersion: "1.5.5", InUse: 0, Total: 2}
		},
	})
	r.Start(context.Background())
	waitSeen(t, &mu, &seen, 1)
	r.Drain()
	waitSeen(t, &mu, &seen, 2)
	r.Stop(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(seen, ",") != "starting:database,draining:,signed off" {
		t.Fatalf("the requests: %v", seen)
	}
}

func waitSeen(t *testing.T, mu *sync.Mutex, seen *[]string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(*seen)
		mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fewer than %d requests", n)
}

// fakeClock is the sign-off's clock in a test: a pause passes at once and moves the time on by its length.
type fakeClock struct {
	mu     sync.Mutex
	at     time.Time
	pauses []time.Duration
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
	c.pauses = append(c.pauses, d)
	ch := make(chan time.Time, 1)
	ch <- c.at
	return ch
}

// R-OPS-048, Q-HA-495: a replica that ends in order is gone at once even when the scan listener is between two replicas of its own: the
// sign-off is tried again while the listener cannot take it (5xx), with a pause that doubles up to its cap, until it is taken; a refusal
// that another try cannot change (4xx) ends the tries at once; a listener that never takes it is tried for SignOffFor and no longer, and
// the failure is logged once (H-1995). The clock is the test's: no test waits for a pause.
func TestR_OPS_048_TheSignOffIsTriedAgainWithinItsBound(t *testing.T) {
	cases := []struct {
		name    string
		answers []int // the answers to the DELETEs in turn; the last one repeats
		tries   int
		pauses  []time.Duration
		warned  bool
	}{
		{"taken after the listener came back", []int{503, 502, 204}, 3, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}, false},
		{"a row the listener does not hold is gone", []int{404}, 1, nil, false},
		{"a refusal ends the tries", []int{401}, 1, nil, true},
		{"never taken: tried for its bound", []int{503}, 8, []time.Duration{
			250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second,
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			deletes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				mu.Lock()
				code := tc.answers[min(deletes, len(tc.answers)-1)]
				deletes++
				mu.Unlock()
				w.WriteHeader(code)
			}))
			defer srv.Close()
			clock := &fakeClock{at: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
			logs := &strings.Builder{}
			r := NewReporter(Options{
				URL: srv.URL, HTTP: srv.Client(), Interval: time.Hour, SignOffFor: SignOffWindow,
				Identity: Identity{UID: "8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b", Name: "pod-1", Version: "0.42.0", Revision: "abc"},
				Status:   func(context.Context) Status { return Status{State: StateReady, Total: 1} },
				Log:      slog.New(slog.NewTextHandler(logs, nil)),
				after:    clock.after, now: clock.now,
			})
			r.Stop(context.Background())
			mu.Lock()
			defer mu.Unlock()
			if deletes != tc.tries || !slices.Equal(clock.pauses, tc.pauses) {
				t.Fatalf("%d sign-offs with the pauses %v, want %d with %v", deletes, clock.pauses, tc.tries, tc.pauses)
			}
			want := 0
			if tc.warned {
				want = 1
			}
			if warned := strings.Count(logs.String(), "sign-off did not reach"); warned != want {
				t.Fatalf("logged %d times:\n%s", warned, logs.String())
			}
		})
	}
}

// R-OPS-048, Q-OPS-460, Q-OPS-461: a malware scanner names the calls its slots hold, each the digest of the call with the time its slot
// took it, at most one per slot; a report of 64 slots that each hold a call fits the bound of 16 KiB. Calls from another kind, more calls
// than slots, a malformed digest, a call without its time, a digest twice and a body above the bound are refused.
func TestR_OPS_048_AMalwareScannerReportsItsCallsWithinItsSlots(t *testing.T) {
	since := time.Date(2026, 11, 27, 9, 14, 5, 123456789, time.UTC)
	const hexDigits = "0123456789abcdef"
	call := func(i int) Call {
		return Call{Digest: "sha256:" + strings.Repeat("f", 62) + string(hexDigits[i/16]) + string(hexDigits[i%16]), Since: since}
	}
	r := valid()
	r.Calls = []Call{call(1)}
	if err := r.Validate(config.ScannerKindClamav); err != nil {
		t.Fatalf("a call: %v", err)
	}
	full := valid()
	full.Slots = Slots{InUse: 64, Total: 64}
	for i := range 64 {
		full.Calls = append(full.Calls, call(i))
	}
	b, _ := json.Marshal(full)
	if len(b) > MaxBodyBytes {
		t.Fatalf("64 calls are %d bytes, above the bound", len(b))
	}
	if got, err := Decode(strings.NewReader(string(b))); err != nil || got.Validate(config.ScannerKindClamav) != nil || len(got.Calls) != 64 {
		t.Fatalf("64 calls: %v", err)
	}
	trivy := valid()
	trivy.Database = ""
	for name, c := range map[string]struct {
		calls []Call
		kind  string
	}{
		"more calls than slots":       {[]Call{call(1), call(2), call(3)}, config.ScannerKindClamav},
		"a digest without its prefix": {[]Call{{Digest: strings.Repeat("a", 64), Since: since}}, config.ScannerKindClamav},
		"a digest in upper case":      {[]Call{{Digest: "sha256:" + strings.Repeat("A", 64), Since: since}}, config.ScannerKindClamav},
		"a call without its time":     {[]Call{{Digest: call(1).Digest}}, config.ScannerKindClamav},
		"a digest twice":              {[]Call{call(1), call(1)}, config.ScannerKindClamav},
		"calls from an image scanner": {[]Call{call(1)}, config.ScannerKindTrivy},
	} {
		rep := valid()
		if c.kind == config.ScannerKindTrivy {
			rep = trivy
		}
		rep.Calls = c.calls
		if err := rep.Validate(c.kind); err == nil {
			t.Errorf("%s: admitted", name)
		}
	}
	if _, err := Decode(strings.NewReader(`{"name":"` + strings.Repeat("a", 16<<10) + `"}`)); err == nil {
		t.Error("a body above 16 KiB: decoded")
	}
}
