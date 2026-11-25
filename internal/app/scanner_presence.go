package app

import (
	"context"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/vuln/report"
)

// scannerPresence answers whether a scanner of a kind is connected now (Q-VULN-277), from what the System page's list of scanner
// replicas reads (R-OPS-048, Q-OPS-223), so that a list cell and the System page never disagree. A live, ready row of the kind is proof
// of one, whatever this replica's configuration says. Without one, the configuration decides how much the missing row says (presenceOf):
// no address of the kind here means none configured (the chart gives every role the same address); replicas that report themselves mean
// none ready; replicas that cannot report (no TLS, or no name to admit them by) leave it unknown, and a row's own code then names a
// scanner its last call could not reach. One statement over the small table scanner_replica, asked at most once per answer and only when
// an artifact waits for its scan.
type scannerPresence struct{ a *App }

// presence is the answer for a kind of scanner (trivy, clamav).
func (p scannerPresence) presence(ctx context.Context, kind gen.ScannerKindKind) (report.Presence, error) {
	mode := gen.ScannerKindReplicasNone
	for _, k := range scannerKinds(p.a.cfg) {
		if k.Kind == kind {
			mode = k.Replicas
		}
	}
	ready := false
	if db := p.a.database(); db != nil {
		var err error
		if ready, err = readyReplica(ctx, db, p.a.cfg.System, string(kind)); err != nil {
			return report.PresenceUnknown, err
		}
	}
	return presenceOf(ready, mode), nil
}

// presenceOf decides from whether a live row of the kind is ready and how the kind's replicas are known to this replica.
func presenceOf(ready bool, mode gen.ScannerKindReplicas) report.Presence {
	switch {
	case ready:
		return report.PresenceReady
	case mode == gen.ScannerKindReplicasUnverified:
		return report.PresenceUnknown
	}
	return report.PresenceNone
}

// readyReplica reads whether a live row of the kind is ready.
func readyReplica(ctx context.Context, db *postgres.DB, sys config.System, kind string) (bool, error) {
	var rows []cluster.ScannerReplica
	if err := db.Read(ctx, func(c q.DBTX) (err error) {
		rows, err = cluster.ListScannerReplicas(ctx, c, sys)
		return err
	}); err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.Kind == kind && !r.Lost && r.State == "ready" {
			return true, nil
		}
	}
	return false, nil
}

// ImageScanners is the answer for the image scanner (report.Scanners).
func (p scannerPresence) ImageScanners(ctx context.Context) (report.Presence, error) {
	return p.presence(ctx, gen.ScannerKindKindTrivy)
}

// MalwareScanners is the answer for the malware scanner.
func (p scannerPresence) MalwareScanners(ctx context.Context) (report.Presence, error) {
	return p.presence(ctx, gen.ScannerKindKindClamav)
}
