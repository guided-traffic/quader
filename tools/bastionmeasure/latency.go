package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// latency sends what item 3 of W34-E4 times, one request at a time so that the tool does not queue behind itself: n credential
// checks straight to the main installation's interface, exactly as a Bastion sends them (the Bastion's token, the interface
// headers, the forwarded origin), half with the reader's token and half anonymous; and, through a Bastion replica whose
// artifact_revalidate_after is 1 s, n reads of the npm tarballs with the reader's token, each of which the replica turns into a
// conditional request answered 304. It prints the times it saw; the main installation's own times are its request records
// (tally -latency over the same window).
func latency(args []string) error {
	fs := flag.NewFlagSet("latency", flag.ContinueOnError)
	mainURL := fs.String("main", "", "the main installation's base URL")
	ca := fs.String("ca", "", "CA file")
	bstFile := fs.String("bastion-token-file", "", "the Bastion's token")
	credFile := fs.String("cred-file", "", "the reader's token")
	host := fs.String("host", "", "the Bastion's host:port as its base URL names it")
	via := fs.String("via", "", "address of the Bastion replica for the conditional requests, host:port; empty skips them")
	project := fs.String("project", "e4", "project")
	n := fs.Int("n", 300, "requests of each kind")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bst, err := readToken(*bstFile)
	if err != nil {
		return err
	}
	cred, err := readToken(*credFile)
	if err != nil {
		return err
	}
	mc, err := newClient(*mainURL, *ca, bst, 30*time.Second)
	if err != nil {
		return err
	}
	_, port, _ := strings.Cut(*host, ":")
	out := map[string]any{}
	var checks []float64
	for i := range *n {
		var c *string
		if i%2 == 0 {
			c = &cred
		}
		body := jsonBody(iface.CheckRequest{
			Credential: c, ClientAddress: fmt.Sprintf("198.51.100.%d", i%250+1),
			Repositories: []string{*project + "/npm-local"}, Packages: iface.CheckPackages{},
		})
		start := time.Now()
		a, err := mc.do("POST", iface.PathCheck, body, "Content-Type", iface.MediaType, "Accept", iface.MediaType,
			"User-Agent", iface.UserAgentPrefix+"e4", iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion), iface.HeaderReplica, "e4-latency",
			"X-Forwarded-Proto", "https", "X-Forwarded-Host", *host, "X-Forwarded-Port", port,
			"Forwarded", fmt.Sprintf(`proto=https;host="%s"`, *host))
		if err != nil {
			return err
		}
		if a.Status != http.StatusOK {
			return fmt.Errorf("check: %d %s", a.Status, a.Body)
		}
		checks = append(checks, float64(time.Since(start).Microseconds())/1000)
	}
	out["checkClientMs"] = stats(checks)
	if *via != "" {
		bc, err := hostClient(*via, *host, *ca, cred, 60*time.Second)
		if err != nil {
			return err
		}
		var reads []float64
		statuses := map[int]int{}
		for i := range *n {
			name := fmt.Sprintf("e4pkg-%03d", i%npmPackages+1)
			start := time.Now()
			a, err := bc.do("GET", "/api/npm/"+*project+"/npm-local/"+name+"/-/"+name+"-1.0.0.tgz", nil)
			if err != nil {
				return err
			}
			statuses[a.Status]++
			reads = append(reads, float64(time.Since(start).Microseconds())/1000)
		}
		out["readThroughBastionClientMs"] = stats(reads)
		out["readStatuses"] = statuses
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func stats(v []float64) map[string]float64 {
	if len(v) == 0 {
		return nil
	}
	sort.Float64s(v)
	r := func(x float64) float64 { return math.Round(x*100) / 100 }
	return map[string]float64{"n": float64(len(v)), "p50": r(pct(v, 0.5)), "p95": r(pct(v, 0.95)), "p99": r(pct(v, 0.99)), "max": r(v[len(v)-1])}
}

// hostClient is a client of a Bastion listening at addr (host:port) that names host, the host of the Bastion's base URL, in the
// request and in the TLS handshake: this machine does not resolve that name.
func hostClient(addr, host, caFile, token string, timeout time.Duration) (*client, error) {
	c, err := newClient("https://"+addr, caFile, token, timeout)
	if err != nil {
		return nil, err
	}
	hostOnly, _, _ := strings.Cut(host, ":")
	tr := c.hc.Transport.(*http.Transport) //nolint:forcetypeassert // newClient builds an *http.Transport
	tr.TLSClientConfig = &tls.Config{RootCAs: tr.TLSClientConfig.RootCAs, ServerName: hostOnly, MinVersion: tls.VersionTLS12}
	c.host = host
	return c, nil
}
