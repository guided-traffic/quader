package guard

import (
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
)

// builtIn returns the guard's options at the configuration's built-in defaults, read through the loader as a Bastion reads them.
func builtIn(t *testing.T) Options {
	t.Helper()
	const minimal = "server:\n  base_url: https://packages.example.com\nbastion:\n  main:\n    url: https://quader.example.com\n    token_file: /run/secrets/token/token\n"
	l, err := config.LoadBastion(config.Options{
		File: "/etc/quader/quader.yaml", Command: config.CommandServe,
		ReadFile: func(name string) ([]byte, error) {
			if name == "/etc/quader/quader.yaml" {
				return []byte(minimal), nil
			}
			return nil, os.ErrNotExist
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := l.Config.Bastion
	return Options{Limits: b.Limits, FailedAuth: b.FailedAuth, Blocking: b.Blocking}
}

// command is one normal command of a client as the measurement of usage-and-limits.md Q-LIM-59 found it: its requests, spread
// evenly over the time they took, each a miss on a cold Bastion.
type command struct {
	name     string
	requests int
	in       time.Duration
	cred     Credentialed
}

// TestR_BAS_092_TheDefaultsHoldOneNormalCommandOfEveryClient: at the built-in figures, one normal command of each client of the
// measurement passes a cold Bastion from one address without a refusal, its requests and its misses alike, and so do two
// installs at once from one address; an address that floods is still refused and blocked within seconds (R-BAS-035, R-BAS-037).
func TestR_BAS_092_TheDefaultsHoldOneNormalCommandOfEveryClient(t *testing.T) {
	for _, c := range []command{
		{"docker pull of an image of 40 layers, anonymous", 48, 280 * time.Millisecond, false},
		{"podman pull of an image of 40 layers, anonymous", 44, 480 * time.Millisecond, false},
		{"docker pull of an image of 40 layers", 48, 280 * time.Millisecond, true},
		{"npm ci of 475 packages", 475, 2500 * time.Millisecond, true},
		{"npm ci of 475 packages, anonymous", 475, 2500 * time.Millisecond, false},
		{"two npm ci of 475 packages at once, anonymous", 950, 2500 * time.Millisecond, false},
		{"ten pulls of 40 layers in a row, anonymous", 480, 3 * time.Second, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, 1, func(o *Options) {
				b := builtIn(t)
				o.Limits, o.FailedAuth, o.Blocking = b.Limits, b.FailedAuth, b.Blocking
			})
			step := c.in / time.Duration(c.requests)
			refused := 0
			for range c.requests {
				if !r.g.Request(ctx, "203.0.113.7", c.cred).Allowed || !r.g.Miss(ctx, "203.0.113.7", c.cred).Allowed {
					refused++
				}
				r.clk.advance(step)
			}
			if refused != 0 {
				t.Fatalf("%d of %d requests refused at the built-in figures %+v", refused, c.requests, r.g.o.Limits)
			}
		})
	}
	t.Run("an address that sends 1,000 anonymous requests a second is refused and blocked within ten seconds", func(t *testing.T) {
		r := newRig(t, 1, func(o *Options) {
			b := builtIn(t)
			o.Limits, o.FailedAuth, o.Blocking = b.Limits, b.FailedAuth, b.Blocking
		})
		admitted := 0
		for i := range 10_000 {
			if v := r.g.Blocked(ctx, "203.0.113.7"); !v.Allowed {
				if v.Reason != metrics.RefusedBlocked {
					t.Fatalf("refusal %+v", v)
				}
				t.Logf("blocked after %s, %d admitted", time.Duration(i)*time.Millisecond, admitted)
				return
			}
			if r.g.Request(ctx, "203.0.113.7", false).Allowed {
				admitted++
			}
			r.clk.advance(time.Millisecond)
		}
		t.Fatalf("not blocked within 10 s; %d admitted", admitted)
	})
}
