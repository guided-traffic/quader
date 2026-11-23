//go:build integration

package overload

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestBurstBesideEmptyCaches is the burst that Q-ARCH-240 shed (Q-ARCH-328): a
// replica with a 1 GiB container and the default caches, whose budgets add up
// to 648 MiB. It reserved them whole and admitted 77 downloads; the caches
// are empty here, so the replica admits what its memory holds, and 300 held
// downloads of 1 MiB (about 190 MiB) are all served. TestMemoryAdmission shows
// that a real exhaustion is still shed.
//
// The 300 downloads queue for the five connections of the harness's request pool: with one core under the background
// class at a host load of 10, 150 of 355 acquisitions waited 0.5 to 1 s. The harness bounds that wait by its retry
// bound of 3 s (testenv.Stack); a host four times as loaded reaches it, and a download past it is answered 503
// database_unavailable, which says nothing of the memory (the 503s of F-060 fit it; that run's codes were not kept). The
// replica waits as long as the product's default of database.retry.max_elapsed lets it, 30 s.
func TestBurstBesideEmptyCaches(t *testing.T) {
	in := newInstall(t)
	r := in.start(t, "burst", fmt.Sprintf("QUADER_RUNTIME_MEMORY_LIMIT_BYTES=%d", 1<<30), "QUADER_DATABASE_RETRY_MAX_ELAPSED=30s")
	big := in.deploy(t, r, "big.bin", 1<<20)
	const path = filesRoot + "big.bin"
	const n = 300

	t.Run("R-ARCH-005 Q-ARCH-328 the slots are not reduced by the caches' budgets while the caches are empty", func(t *testing.T) {
		// 0.8 × 0.85 × 1 GiB over 640 KiB is 1,114; the budgets of the default caches would leave 77.
		if slots := r.metric(t, "quader_http_memory_slots"); slots < 1000 {
			t.Fatalf("quader_http_memory_slots = %.0f, want about 1,114 (77 when the budgets were reserved)", slots)
		}
	})

	t.Run("R-ARCH-005 Q-ARCH-328 300 downloads held at once are all served: nothing is shed although the budgets of the caches would have left 77 slots", func(t *testing.T) {
		const shedSeries = `quader_http_requests_shed_total{endpoint="default",reason="memory_slots"}`
		base := r.completed(t)
		downloads := make([]*slow, n)
		for i := range downloads {
			downloads[i] = slowGet(t, r.client, path, in.token)
		}
		// A download the replica counts in flight is admitted or shed right after, by step 4, which waits for nothing (the
		// memory slots and the heap gate); one that completed was answered. Once every download is in one of the two, the
		// admission has nothing left to decide and the downloads are read: when they are read changes no admission.
		testenv.WaitFor(t, "every download reached the replica", 60*time.Second, func() bool {
			return r.completed(t)-base+r.metric(t, `quader_http_requests_in_flight{endpoint="default",listener="client"}`) >= n
		})
		results := make([]result, n)
		var wg sync.WaitGroup
		for i := range downloads {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = downloads[i].finish()
			}()
		}
		wg.Wait()
		for i, res := range results {
			if res.err != nil || res.status != http.StatusOK || res.sum != big || res.n != 1<<20 {
				t.Errorf("download %d: status %d, %d bytes, err %v, refusal %q", i, res.status, res.n, res.err, res.refusal)
			}
		}
		if shed := r.metrics(t, shedSeries)[shedSeries]; shed != 0 {
			t.Errorf("%.0f requests were shed for the memory", shed)
		}
		if t.Failed() {
			// Which admission shed or which wait ran out: every series of the shed counter and of the memory slots, and the
			// request pool's waits for a connection and the retries, read now.
			if code, body := get("http://"+r.ops+"/metrics", ""); code == http.StatusOK {
				var lines []string
				for _, l := range strings.Split(body, "\n") {
					if strings.HasPrefix(l, "quader_http_requests_shed_total") || strings.HasPrefix(l, "quader_http_memory_slots") ||
						strings.HasPrefix(l, "quader_db_pool_acquire_duration_seconds") || strings.HasPrefix(l, "quader_db_retries_total") {
						lines = append(lines, l)
					}
				}
				t.Logf("the replica's shed, memory and connection series:\n%s", strings.Join(lines, "\n"))
			}
		}
		if !r.alive() {
			t.Fatal("the replica exited")
		}
	})
}
