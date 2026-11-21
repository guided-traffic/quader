package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The rows of the scanner replicas (R-OPS-048, Q-OPS-223): packages writes them from the reports a scanner sends to its scan listener,
// every role that serves the management API reads them, and replica-registry-prune deletes them by the registry's rule.

// ScannerRecord is what a report writes into a row, checked by the route before (scanreport.Report.Validate).
type ScannerRecord struct {
	Name, Node, Version, Revision, State, Reason, ToolVersion, Database string
	SlotsInUse, Slots                                                   int
}

// ScannerReplicas writes the rows on the scan listener of packages.
type ScannerReplicas struct {
	// DB runs the statements on the request pool.
	DB q.DBTX
	// System holds heartbeat_stale_after, which decides which rows count as live against the bound of a kind.
	System config.System
	// MaxRows is the bound of the live rows of one kind (scanreport.MaxRowsPerKind).
	MaxRows int
}

// Record writes a report; false when the row was refused (MaxRows live rows of the kind, or the UID of a row of another kind).
func (s ScannerReplicas) Record(ctx context.Context, kind, uid string, r ScannerRecord) (bool, error) {
	n, err := q.New(s.DB).RecordScannerReplica(ctx, q.RecordScannerReplicaParams{
		InstanceUid: uid, Kind: kind, Name: r.Name, Node: r.Node, Version: r.Version, Revision: r.Revision, State: r.State,
		Reason: r.Reason, ToolVersion: r.ToolVersion, DbVersion: r.Database,
		SlotsInUse: int32(r.SlotsInUse), SlotsTotal: int32(r.Slots), //nolint:gosec // bounded by the route's check: 0 to 64
		StaleSecs: s.System.HeartbeatStaleAfter.Seconds(), MaxRows: int32(s.MaxRows), //nolint:gosec // a small constant
	})
	if err != nil {
		return false, fmt.Errorf("recording the scanner replica %s: %w", uid, err)
	}
	return n == 1, nil
}

// Remove deletes the row of a replica that signed off; false when it had none of that kind.
func (s ScannerReplicas) Remove(ctx context.Context, kind, uid string) (bool, error) {
	n, err := q.New(s.DB).DeleteScannerReplica(ctx, q.DeleteScannerReplicaParams{InstanceUid: uid, Kind: kind})
	if err != nil {
		return false, fmt.Errorf("removing the scanner replica %s: %w", uid, err)
	}
	return n == 1, nil
}

// ScannerReplica is one row as GET /api/v1/system/scanners shows it.
type ScannerReplica struct {
	InstanceUID, Kind, Name, Node, Version, Revision, State, Reason string
	StateSince, StartedAt, HeartbeatAt                              time.Time
	Lost                                                            bool
	ToolVersion, DatabaseVersion                                    string
	// DatabaseBuiltAt is the build time of the signature database in use when the row names that version; zero otherwise.
	DatabaseBuiltAt   time.Time
	SlotsInUse, Slots int
}

// ListScannerReplicas reads the rows: a row that went stale while draining is not listed, every other stale one is lost (Q-OPS-210),
// by PostgreSQL's clock.
func ListScannerReplicas(ctx context.Context, db q.DBTX, sys config.System) ([]ScannerReplica, error) {
	rows, err := q.New(db).ListScannerReplicas(ctx, sys.HeartbeatStaleAfter.Seconds())
	if err != nil {
		return nil, fmt.Errorf("reading the scanner replicas: %w", err)
	}
	out := make([]ScannerReplica, 0, len(rows))
	for _, r := range rows {
		sr := ScannerReplica{
			InstanceUID: r.InstanceUid.String(), Kind: r.Kind, Name: r.Name, Node: r.Node.String, Version: r.Version,
			Revision: r.Revision, State: r.State, Reason: r.Reason.String, StateSince: r.StateSince.Time.UTC(),
			StartedAt: r.StartedAt.Time.UTC(), HeartbeatAt: r.HeartbeatAt.Time.UTC(), Lost: r.Lost, ToolVersion: r.ToolVersion.String,
			DatabaseVersion: r.DbVersion.String, SlotsInUse: int(r.SlotsInUse), Slots: int(r.SlotsTotal),
		}
		if r.DbBuiltAt.Valid {
			sr.DatabaseBuiltAt = r.DbBuiltAt.Time.UTC()
		}
		out = append(out, sr)
	}
	return out, nil
}

// PruneScannerReplicas is the scanner replicas' part of replica-registry-prune, by the registry's rule (PruneReplicas).
func PruneScannerReplicas(ctx context.Context, db q.DBTX, staleFor, signedOffAfter time.Duration) (int64, error) {
	return q.New(db).PruneScannerReplicas(ctx, q.PruneScannerReplicasParams{StaleSecs: staleFor.Seconds(), SignedOffSecs: signedOffAfter.Seconds()})
}
