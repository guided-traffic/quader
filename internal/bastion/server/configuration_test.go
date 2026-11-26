package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
)

// TestR_BAS_093_ARestartDuringAnOutageAppliesTheStoredConfiguration: a replica stores the configuration it applied in its
// cache backend; restarted while the main installation cannot be reached it applies that one, the slots of the main
// installation's figures among it, before its first handshake (Q-BAS-327); a replica of an empty volume applies the built-in
// one. A cache volume of a few files, two starts.
func TestR_BAS_093_ARestartDuringAnOutageAppliesTheStoredConfiguration(t *testing.T) {
	main := maintest.New(t)
	main.SetConfiguration(func(c *iface.Configuration) {
		c.Revision = 31
		c.Limits.Slots = iface.Slots{MaxConcurrent: 12, AnonymousShare: 0.5, QueueTimeoutMillis: 700}
	})
	dir := t.TempDir()
	cfg := linkedConfig(main, dir, tokenFileFor(t, dir, main.NewToken()), "")
	first, logs := newLinked(t, cfg, nil)
	run(t, first, logs)
	eventually(t, 10*time.Second, "the main installation's configuration", func() bool {
		return strings.Contains(metricsBody(t, first), `quader_bastion_configuration_source{source="main"} 1`)
	})
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	main.SetDown(true)
	again, logs := newLinked(t, cfg, nil)
	run(t, again, logs)
	body := metricsBody(t, again)
	for _, want := range []string{
		`quader_bastion_configuration_source{source="stored"} 1`, "quader_bastion_configuration_revision 31",
		`quader_bastion_main_budget{class="authenticated"} 12`, `quader_bastion_main_budget{class="anonymous"} 6`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the restarted replica's metrics lack %q:\n%s", want, grepLines(body, "quader_bastion_configuration"))
		}
	}

	empty := t.TempDir()
	fresh, logs := newLinked(t, linkedConfig(main, empty, tokenFileFor(t, empty, main.NewToken()), ""), nil)
	run(t, fresh, logs)
	if body := metricsBody(t, fresh); !strings.Contains(body, `quader_bastion_configuration_source{source="builtin"} 1`) ||
		!strings.Contains(body, `quader_bastion_main_budget{class="anonymous"} 16`) {
		t.Errorf("a replica of an empty volume:\n%s", grepLines(body, "quader_bastion_configuration"))
	}
}
