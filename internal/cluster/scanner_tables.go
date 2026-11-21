package cluster

import "github.com/guided-traffic/quader/internal/backup/tables"

// The rows of the scanner replicas are transient like the replica registry's: a restored installation starts without them, and
// every scanner reports itself again within one heartbeat (R-OPS-048, R-BAK-004).
func init() {
	tables.Register("scanner_replica", tables.Transient, tables.Options{})
}
