package jobs

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
)

// The kinds and tasks of the coordination layer itself (R-ARCH-016 table,
// R-OPS-019).
const (
	// KindClusterChangePrune deletes change-log rows (R-ARCH-018).
	KindClusterChangePrune = "cluster-change-prune"
	// KindReplicaRegistryPrune deletes the registry rows of lost replicas stale for 24 h and those of replicas that
	// signed off (R-OPS-003, Q-OPS-210).
	KindReplicaRegistryPrune = "replica-registry-prune"
)

// replicaPruneAfter is how long the row of a lost replica must be stale before
// the prune task deletes it (R-OPS-003). A row that went stale while draining
// goes at the first run after system.heartbeat_stale_after (Q-OPS-210).
const replicaPruneAfter = 24 * time.Hour

// RegisterCoordinationKinds registers cluster-change-prune and
// replica-registry-prune and their tasks, both every ten minutes
// (`*/10 * * * *`), exclusive, in the maintenance queue. The composition root
// calls it once, before Freeze, on every role: a web replica must know the
// kinds to enqueue them (the manual trigger of R-OPS-019).
func RegisterCoordinationKinds(r *Registry, c config.Cluster, s config.System) {
	r.RegisterKind(KindClusterChangePrune, KindSpec{
		Queue: "maintenance", DedupMode: Exclusive, MaxAttempts: 3,
		Handler: func(ctx context.Context, run *Run) error {
			n, err := cluster.PruneChanges(ctx, run.DB(), c.ChangeRetention)
			if err == nil {
				err = run.SetResult(map[string]int64{"deleted": n})
			}
			return err
		},
	})
	r.RegisterTask(KindClusterChangePrune, TaskSpec{Schedule: "*/10 * * * *"})

	r.RegisterKind(KindReplicaRegistryPrune, KindSpec{
		Queue: "maintenance", DedupMode: Exclusive, MaxAttempts: 3,
		Handler: func(ctx context.Context, run *Run) error {
			n, err := cluster.PruneReplicas(ctx, run.DB(), replicaPruneAfter, s.HeartbeatStaleAfter)
			if err == nil {
				err = run.SetResult(map[string]int64{"deleted": n})
			}
			return err
		},
	})
	r.RegisterTask(KindReplicaRegistryPrune, TaskSpec{Schedule: "*/10 * * * *"})
}
