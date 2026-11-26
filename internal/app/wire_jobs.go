package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/jobs"
)

// Fallback cron schedules of the storage tasks (R-STOR-025). The configuration
// (maintenance.tasks) has the same defaults and wins; these apply only when the
// key is absent from it.
const (
	defaultGCSchedule     = "*/30 * * * *"
	defaultGCFullSchedule = "0 2 * * 0"
)

// jobRegistry builds the replica's job registry: the kinds that packages
// registered in jobs.Default at initialisation, the coordination kinds and the
// garbage-collection kinds. Every role builds it, because a web replica must know
// the kinds to enqueue them (R-ARCH-016). It is frozen before the replica serves.
// A registry per replica, not jobs.Default itself, keeps several replicas in one
// test process apart.
func (a *App) jobRegistry() (*jobs.Registry, error) {
	r := jobs.NewRegistry()
	for _, k := range jobs.Default.Kinds() {
		spec, _ := jobs.Default.Kind(k)
		r.RegisterKind(k, spec)
	}
	for name, t := range jobs.Default.Tasks() {
		r.RegisterTask(name, t)
	}
	jobs.RegisterCoordinationKinds(r, a.cfg.Cluster, a.cfg.System)
	jobs.RegisterProbeKind(r)
	a.registerGC(r)
	a.registerStorageSweep(r)
	a.registerStorageReconcile(r)
	a.registerStorageScrub(r)
	a.registerStorageSummary(r)
	a.registerStorageHistory(r)
	a.registerRepositoryJobs(r)
	a.registerIdentityJobs(r)
	a.registerCIJobs(r)
	a.registerVulnJobs(r)
	a.registerMalwareJobs(r)
	a.registerSignatureJobs(r)
	a.registerSigningJobs(r)
	a.registerSecretsReencrypt(r)
	a.registerAuditJobs(r)
	a.registerInvalidationJobs(r)
	a.registerFormatJobs(r)
	a.registerMirrorJobs(r)
	a.registerJournalJobs(r)
	a.registerBackupCheckJob(r)
	a.registerLimitsJobs(r)
	a.registerMalwareDatabaseJobs(r)
	a.registerExaminationJobs(r)
	a.registerBastionCatalogueJobs(r)
	if err := jobs.ValidateSchedules(a.cfg.Maintenance); err != nil {
		return nil, err
	}
	if err := checkTaskNames(r, a.cfg.Maintenance); err != nil {
		return nil, err
	}
	r.Freeze()
	return r, nil
}

// tasksNotBuilt are the tasks of R-OPS-019 that the specification names and whose schedules the configuration's default carries
// ahead of the code that registers them: none at present. A key of maintenance.tasks that names one is accepted and has no effect
// until the task is registered (Q-OPS-418); a name leaves this list in the change that registers its task. A function, not a package
// variable (R-ARCH-032); the names are written out, so that the list does not depend on a domain's package.
func tasksNotBuilt() []string {
	return nil
}

// checkTaskNames refuses a configuration that schedules a task this version does not have (R-OPS-019, Q-OPS-418): a key of
// maintenance.tasks that names no registered task, a misspelt name or one of another version, would otherwise be ignored, and the
// operator would believe a task switched off or rescheduled that runs on its default. Every role that builds the registry builds the
// same one, so every such replica refuses the same configuration at its start; the role scanner builds none (it runs no job and reads
// no task) and does not check the key.
func checkTaskNames(r *jobs.Registry, m config.Maintenance) error {
	registered := r.Tasks()
	var unknown []string
	for name := range m.Tasks {
		if _, ok := registered[name]; !ok && !slices.Contains(tasksNotBuilt(), name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	known := make([]string, 0, len(registered))
	for name := range registered {
		known = append(known, name)
	}
	sort.Strings(known)
	problems := make([]error, 0, len(unknown))
	for _, name := range unknown {
		problems = append(problems, fmt.Errorf("maintenance.tasks.%s: this version has no task %q; its tasks are %s", name, name, strings.Join(known, ", ")))
	}
	return errors.Join(problems...)
}

// registerGC registers the kinds gc and gc-full and their tasks (R-STOR-025):
// queue maintenance, exclusive, both tasks under the dedup key "gc", so at most
// one collection is queued or running whichever task started it. The handlers
// find the collector at run time (a.gc); only worker and all build one, and only
// they claim a job.
func (a *App) registerGC(r *jobs.Registry) {
	handler := func(mode blob.GCMode) jobs.Handler {
		return a.gc.handler("garbage collector", func(ctx context.Context, c *blob.Collector, run *jobs.Run) error {
			err := c.RunJob(ctx, mode, run.Args, run)
			if errors.Is(err, blob.ErrHandedOver) {
				// A run in its settle wait at the drain gives its job up: released, not failed (Q-STOR-217, Q-ARCH-501).
				return fmt.Errorf("%w: %w", jobs.ErrReleased, err)
			}
			return err
		})
	}
	for _, k := range []struct {
		kind     string
		mode     blob.GCMode
		fallback string
	}{{"gc", blob.GCIncremental, defaultGCSchedule}, {"gc-full", blob.GCFull, defaultGCFullSchedule}} {
		r.RegisterKind(k.kind, jobs.KindSpec{Queue: "maintenance", DedupMode: jobs.Exclusive, Handler: handler(k.mode)})
		spec := jobs.TaskSpec{Schedule: k.fallback, DedupKey: "gc", Mutating: true}
		if t, ok := a.cfg.Maintenance.Tasks[k.kind]; ok && t.Schedule != "" {
			spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
		}
		r.RegisterTask(k.kind, spec)
	}
}
