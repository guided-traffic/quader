package job

import (
	"context"
	"fmt"

	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
)

// The job kinds and the queue of signatures on artifacts (R-SIG-011, R-SIG-012, R-SIG-017).
const (
	// KindVerify reads and checks the due signatures, after discovery, the orphan removal and the daily retry of failed (R-SIG-011).
	KindVerify = "signature-verify"
	// KindTrust recomputes the trust rows computed under an older anchor epoch, or missing (R-SIG-012).
	KindTrust = "signature-trust"
	// KindTrustRoot refreshes the public Sigstore root and npm's keys from the TUF repository (R-SIG-017).
	KindTrustRoot = "signature-trust-root"
	// QueueSignatures is the queue of the three (jobs.workers.signatures), so that neither the malware scanning nor the advisory sync
	// waits behind a backlog of signatures.
	QueueSignatures = "signatures"
	// The dedup keys: the kinds' names.
	DedupVerify    = KindVerify
	DedupTrust     = KindTrust
	DedupTrustRoot = KindTrustRoot
)

// VerifyKindSpec is the spec of KindVerify without its handler: queue signatures, coalesce (the work is read from the table, and a
// wake-up while a run works is kept for the next run).
func VerifyKindSpec() jobs.KindSpec {
	return jobs.KindSpec{Queue: QueueSignatures, DedupMode: jobs.Coalesce}
}

// TrustKindSpec is the spec of KindTrust without its handler: queue signatures, coalesce.
func TrustKindSpec() jobs.KindSpec {
	return jobs.KindSpec{Queue: QueueSignatures, DedupMode: jobs.Coalesce}
}

// TrustRootKindSpec is the spec of KindTrustRoot without its handler: queue signatures, exclusive (a scheduled task; a second run
// while one is queued or running adds nothing).
func TrustRootKindSpec() jobs.KindSpec {
	return jobs.KindSpec{Queue: QueueSignatures, DedupMode: jobs.Exclusive}
}

// WakeVerify enqueues KindVerify in tx: store.Options.Wake and the job's own continuation (Options.Enqueue).
func WakeVerify(ctx context.Context, tx meta.Tx) error {
	_, err := jobs.EnqueueTx(ctx, tx, VerifyKindSpec(), jobs.EnqueueSpec{Kind: KindVerify, DedupKey: DedupVerify})
	if err != nil {
		return fmt.Errorf("wake %s: %w", KindVerify, err)
	}
	return nil
}

// WakeTrust enqueues KindTrust in tx: store.Options.WakeTrust and the job's own continuation (Options.EnqueueTrust).
func WakeTrust(ctx context.Context, tx meta.Tx) error {
	_, err := jobs.EnqueueTx(ctx, tx, TrustKindSpec(), jobs.EnqueueSpec{Kind: KindTrust, DedupKey: DedupTrust})
	if err != nil {
		return fmt.Errorf("wake %s: %w", KindTrust, err)
	}
	return nil
}
