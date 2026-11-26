package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// flood is the overflow attack of item 2 of W34-E4, scaled down: anonymous callers ask a Bastion for distinct artifacts (the
// files flood/f/<n>.bin of 4 MiB, in order and then from the start again, so each one is evicted before it is asked for again
// once their sum passes the cache) as fast as c connections allow, for a duration. With -spread n the requests come from n
// addresses (X-Forwarded-For through the Bastion's trusted proxy, this machine), as from a botnet; without, from one. Every second
// it writes one line: the answers it got, the bytes it read, and the growth of the Bastion's counters (requests to the main
// installation, bytes read from it, evictions, refusals, the cache's bytes).
func flood(args []string) error {
	fs := flag.NewFlagSet("flood", flag.ContinueOnError)
	addr := fs.String("addr", "", "the Bastion's listener, host:port")
	host := fs.String("host", "", "the Bastion's host:port as its base URL names it")
	ca := fs.String("ca", "", "CA file")
	metricsURL := fs.String("metrics", "", "the Bastion's metrics URL")
	project := fs.String("project", "e4", "project")
	files := fs.Int("files", 768, "distinct files")
	conc := fs.Int("c", 32, "connections")
	dur := fs.Duration("duration", 90*time.Second, "how long")
	spread := fs.Int("spread", 0, "distinct client addresses, 0 for the peer's own")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bc, err := hostClient(*addr, *host, *ca, "", 2*time.Minute)
	if err != nil {
		return err
	}
	mc, err := newClient(strings.TrimSuffix(*metricsURL, "/metrics"), *ca, "", 10*time.Second)
	if err != nil {
		return err
	}
	mc.hc.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true //nolint:forcetypeassert,gosec // the ops listener's certificate names the Bastion's host
	ctx, cancel := context.WithTimeout(context.Background(), *dur)
	defer cancel()
	var (
		next   atomic.Int64
		mu     sync.Mutex
		status = map[int]int{}
		bytes  int64
		wg     sync.WaitGroup
	)
	for w := range *conc {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				i := next.Add(1) - 1
				req, _ := http.NewRequestWithContext(ctx, "GET", bc.base+fmt.Sprintf("/files/%s/flood/f/%04d.bin", *project, int(i)%*files), nil)
				req.Host = bc.host
				if *spread > 0 {
					n := int(i) % *spread
					req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.%d.%d.%d", 1+w%200, n/250, n%250+1))
				}
				res, err := bc.hc.Do(req)
				if err != nil {
					if ctx.Err() == nil {
						mu.Lock()
						status[0]++
						mu.Unlock()
					}
					continue
				}
				nb, _ := io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
				mu.Lock()
				status[res.StatusCode]++
				bytes += nb
				mu.Unlock()
			}
		}()
	}
	enc := json.NewEncoder(os.Stdout)
	prev, _ := scrapeURL(mc)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	start := time.Now()
	var lastBytes int64
	lastStatus := map[int]int{}
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-tick.C:
		}
		cur, _ := scrapeURL(mc)
		mu.Lock()
		st := map[string]int{}
		for k, v := range status {
			if d := v - lastStatus[k]; d != 0 {
				st[fmt.Sprint(k)] = d
			}
			lastStatus[k] = v
		}
		got := bytes - lastBytes
		lastBytes = bytes
		mu.Unlock()
		delta := map[string]float64{}
		for k, v := range cur {
			if strings.HasPrefix(k, "quader_bastion_cache_bytes") || strings.HasPrefix(k, "quader_bastion_main_in_flight") {
				delta[k] = v
				continue
			}
			if d := v - prev[k]; d != 0 {
				delta[k] = d
			}
		}
		prev = cur
		_ = enc.Encode(map[string]any{"t": int(time.Since(start).Round(time.Second).Seconds()), "status": st, "bytesToAttacker": got, "bastion": delta})
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return enc.Encode(map[string]any{"summary": true, "requests": next.Load(), "status": status, "bytesToAttacker": bytes, "seconds": time.Since(start).Seconds()})
}

// scrapeURL reads the Bastion's counters the attack moves.
func scrapeURL(c *client) (map[string]float64, error) {
	a, err := c.do("GET", "/metrics", nil)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, line := range strings.Split(string(a.Body), "\n") {
		for _, p := range []string{
			"quader_bastion_main_requests_total", "quader_bastion_fill_bytes_total", "quader_bastion_bytes_served_total",
			"quader_bastion_cache_evictions_total", "quader_bastion_cache_bytes", "quader_bastion_refused_total", "quader_bastion_main_budget_exhausted_total",
			"quader_bastion_cache_no_room_total", "quader_bastion_main_in_flight", "quader_bastion_requests_total",
		} {
			if !strings.HasPrefix(line, p+"{") && !strings.HasPrefix(line, p+" ") {
				continue
			}
			i := strings.LastIndexByte(line, ' ')
			var v float64
			if _, err := fmt.Sscan(line[i+1:], &v); err != nil || strings.Contains(line, `kind="feed"`) || strings.Contains(line, `kind="handshake"`) {
				continue
			}
			out[strings.ReplaceAll(line[:i], `bastion="e4-bastion",`, "")] = v
		}
	}
	return out, nil
}
