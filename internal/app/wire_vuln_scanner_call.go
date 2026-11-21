package app

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// The worker's side of the role scanner (R-VULN-041, R-VULN-048, R-OCI-037, ADR-212, ADR-213, ADR-229): the client of the two calls, over
// a connection per call with the worker's own pair as the client certificate, the internal CA as the only roots and the scanner's name
// required in its certificate (R-ARCH-038), and the signer of the scan token of each analysis, derived from the key ring the worker holds.
// A detection takes no token.

// scanCallSlack is added to the scanner's own bounds for the deadline of a call: pull_timeout + timeout + 1m for an analysis
// (R-VULN-041), timeout + 1m for a detection (R-VULN-048).
const scanCallSlack = time.Minute

// scannerCall builds the call of the scanner and the signer of the scan tokens.
func (a *App) scannerCall(ctx context.Context, in infra, log *slog.Logger) (*imagescan.Client, imagescan.ScanTokens, error) {
	cfg := a.cfg
	images := cfg.Vulnerabilities.Images
	if in.ring == nil {
		return nil, nil, errors.New("the scan tokens need the key ring")
	}
	signer, err := registrytoken.NewSigner(in.ring, cfg.OCI.TokenLifetime)
	if err != nil {
		return nil, nil, err
	}
	if signer, err = signer.ForScans(images.PullTimeout); err != nil {
		return nil, nil, err
	}
	serverName := images.Scanner.ServerName
	if serverName == "" {
		serverName = hostOf(images.Scanner.URL)
	}
	link, err := httpserver.NewLinkTLS(httpserver.LinkVulnScannerTrivy, cfg.Server, serverName, log, a.tlsOptions(httpserver.LinkVulnScannerTrivy)...)
	if err != nil {
		return nil, nil, err
	}
	o := imagescan.ClientOptions{
		URL: images.Scanner.URL, Deadline: images.PullTimeout + images.Timeout + scanCallSlack, DetectDeadline: images.Timeout + scanCallSlack,
	}
	if link != nil {
		o.TLS = func() *tls.Config {
			// A pair that does not allow client authentication is refused by the scanner; the reason is logged once per loaded pair.
			_ = link.Fit()
			return link.Config()
		}
		if err := a.startComponent(ctx, &linkComponent{name: "link-" + unitVulnScannerTrivy, link: link}); err != nil {
			return nil, nil, err
		}
		// The pair loaded with the link: a pair without client authentication is named at start (R-HA-063), not first at a call.
		_ = link.Fit()
	}
	client, err := imagescan.NewClient(o)
	if err != nil {
		return nil, nil, err
	}
	return client, signer, nil
}

// linkComponent runs the reloader of a link's material from the start of the replica to its stop.
type linkComponent struct {
	name   string
	link   *httpserver.LinkTLS
	cancel context.CancelFunc
	done   chan struct{}
}

func (c *linkComponent) Name() string { return c.name }

func (c *linkComponent) Start(ctx context.Context) error {
	rctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.cancel, c.done = cancel, make(chan struct{})
	go func() {
		defer close(c.done)
		c.link.Run(rctx)
	}()
	return nil
}

func (c *linkComponent) Stop(ctx context.Context) error {
	if c.cancel == nil {
		return nil
	}
	c.cancel()
	select {
	case <-c.done:
	case <-ctx.Done():
	}
	return nil
}
