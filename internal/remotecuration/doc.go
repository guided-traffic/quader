// Package remotecuration is the domain of the curation of a remote repository (docs/spec/remote-curation.md, R-CUR-001 to R-CUR-023;
// the work packages are docs/developer/remote-curation.md): the delivery mode and rules, the download queue, the requests for an allow
// and what was fetched for examination.
//
// This package is the shared vocabulary and the ports, not the machinery. It imports nothing of the module, so that the repository
// model, the remote engine, the format host, the API and every package of the topic can import it without a cycle; its ports therefore
// speak in plain values (a repository's ID, names, versions), never in the repository or item types (the rule of internal/vuln).
//
// The machinery sits in sub-packages, each built by its own work package:
//
//	rules       the decision of R-CUR-001, the compiled rule set, the rules' store and the refusal's record (R2)
//	catalogue   the engine of the upstream catalogue: the lent upstream, budget, cache, bounds (R3)
//	queue       the download queue: store, order, dispatch, retention, the expansion of a bulk (R4)
//	requests    the requests for an allow and the examination a request starts (R2b)
//	retention   the discard of what was fetched for examination and the task remote-examination-cleanup (R2b)
//
// Until a sub-package lands, the stubs of contracts.go answer for it: every version is delivered ([DeliverAll]), nothing is queued
// ([NoQueue]) and nothing is requested ([NoRequests]), so that a package that shows a state can be built before the package that
// decides it.
package remotecuration

// The job, its queue and the two tasks of the topic (R-CUR-011, R-CUR-016, R-CUR-020). Their workers and schedules are the
// configuration's: jobs.workers.downloads, maintenance.tasks.remote-downloads-dispatch and maintenance.tasks.remote-examination-cleanup.
const (
	// KindDownload is one attempt of one row of a repository's download queue: queue QueueDownloads, MaxAttempts 1, exclusive per
	// row, 30 minutes (R-CUR-011). It replaces the job remote-background-download of R-REPO-065 (R-CUR-013).
	KindDownload = "remote-download"
	// QueueDownloads is the queue of the downloads ahead; its slots per claiming process are jobs.workers.downloads.
	QueueDownloads = "downloads"
	// TaskDispatch starts the rows whose next attempt came, finds rows whose job ended, reads the queue's gauge and removes finished
	// rows after their retention; every minute, on the queue maintenance (R-CUR-011, R-OPS-019).
	TaskDispatch = "remote-downloads-dispatch"
	// TaskExaminationCleanup removes what was fetched for examination and not delivered after the repository's
	// remote.delivery.examinedRetentionDays; daily (R-CUR-016, R-OPS-019).
	TaskExaminationCleanup = "remote-examination-cleanup"
)
