package scanreport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		t.Error("a database from a scanner of the kind trivy: admitted")
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
	if v := ToolVersionOfClamd("ClamAV 1.5.5/27841/Fri Nov 20 06:00:00 2026\n"); v != "1.5.5" {
		t.Errorf("clamd's version = %q", v)
	}
	for _, line := range []string{"", "PONG", "ClamAV <script>/1/x", "clamav 1.5.5/1/x"} {
		if v := ToolVersionOfClamd(line); v != "" {
			t.Errorf("%q: %q", line, v)
		}
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
