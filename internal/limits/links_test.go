package limits

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// TestR_LIM_040_ALinkAndTheFigures: a link is charged its size at issuance, in enforce and in audit, also for a caller whose
// profile sets cachedBytesPerSecond, which keeps no download from a link (Q-LIM-99); the next flush adds the size to the hour
// window, so a used-up cachedBytesPerHour refuses the next download before any link (Q-LIM-98). That the serve point hands such a
// caller a link is TestR_STOR_070_TheConditionsOfALink's.
func TestR_LIM_040_ALinkAndTheFigures(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []Mode{ModeEnforce, ModeAudit} {
		t.Run("R-LIM-040 a caller with a speed limit is charged a link of 5 MB at issuance in "+string(mode), func(t *testing.T) {
			f := BuiltinProfiles()[ProfileStandard]
			f[CachedBytesPerHour] = 5 * mb
			if f[CachedBytesPerSecond] == 0 {
				t.Fatal("the profile Standard sets no speed limit")
			}
			r := newRig(t, mode, f)
			c := r.e.Begin(anonCaller("198.51.100.22"), qname.Repo{})
			if err := r.e.Admit(ctx, c); err != nil {
				t.Fatal(err)
			}
			c.ChargeLink(5 * mb)
			if cached, _ := c.Totals(); cached != 5*mb {
				t.Fatalf("the charge holds %d cached bytes", cached)
			}
			r.e.Finish(c)
			r.e.Flush(ctx)
			r.clk.add(time.Second)
			next := r.e.Begin(anonCaller("198.51.100.22"), qname.Repo{})
			defer r.e.Finish(next)
			err := r.e.Admit(ctx, next)
			switch mode {
			case ModeEnforce:
				if codeOf(err) != CodeDownloadLimitReached {
					t.Fatalf("the next download after a link that used up cachedBytesPerHour: %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("audit refused: %v", err)
				}
				if next.Outcome() != "would_limit:cachedBytesPerHour" {
					t.Fatalf("audit recorded %q", next.Outcome())
				}
			}
		})
	}
}
