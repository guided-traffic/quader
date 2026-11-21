package job

import (
	"context"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/key"
	"github.com/guided-traffic/quader/internal/signing/store"
)

// The job kind, its queue and its task (R-SIG-049).
const (
	// KindSigning switches an announced identity after its announce period, runs discovery, and runs the due work of every kind a
	// package registered, with the current identity's key.
	KindSigning = "signing"
	// QueueSigning is its queue (jobs.workers.signing): a publish's signature never waits behind the verification of signatures.
	QueueSigning = "signing"
	// DedupSigning is its dedup key: one job of the kind runs at a time in the installation, and the work that arrives during a run
	// waits as the one job behind it.
	DedupSigning = KindSigning
	// TaskSchedule is the default schedule of the task signing: discovery, the switch and the daily retry run at least this often.
	TaskSchedule = "*/10 * * * *"
)

// KindSpec is the spec of KindSigning without its handler: queue signing, coalesce.
func KindSpec() jobs.KindSpec { return jobs.KindSpec{Queue: QueueSigning, DedupMode: jobs.Coalesce} }

// Wake enqueues KindSigning in tx: the store's wake-up after an identity's change and after work was enqueued (store.Options.Wake).
func Wake(ctx context.Context, tx meta.Tx) error { return WakeAfter(ctx, tx, 0) }

// WakeAfter enqueues KindSigning in tx to run after delay; a waiting job of the key is moved earlier, never later (Q-ARCH-384).
func WakeAfter(ctx context.Context, tx meta.Tx, delay time.Duration) error {
	_, err := jobs.EnqueueTx(ctx, tx, KindSpec(), jobs.EnqueueSpec{Kind: KindSigning, DedupKey: DedupSigning, Delay: max(delay, 0)})
	if err != nil {
		return fmt.Errorf("wake %s: %w", KindSigning, err)
	}
	return nil
}

// Session is what an operation signs with: the current identity and its key, in a transaction that holds the identity's row (R-SIG-048).
type Session struct {
	Identity signing.Identity
	Key      key.Private
}

// Kind is a kind of work the job runs (R-SIG-049): npm's registry signature of a local version, the receipt attestation. The packages
// that sign register one each through Options.Kinds; the job runs the frame around it: the order by priority and time, the hold on the
// current identity, the attempts with their back-off, the daily retry, the metrics and the events.
type Kind struct {
	// Name is the kind of the row (signing.WorkNPM, signing.WorkAttestation).
	Name string
	// Sign does the operation of one row in tx, which holds the current identity's row: it writes the signature and whatever serves it.
	// The job deletes the row in the same transaction when Sign returns nil. An error rolls tx back and spends an attempt; its code is
	// the internal/errs code of the error, or "error".
	Sign func(ctx context.Context, tx meta.Tx, s Session, w store.Work) error
	// Discover writes the work of the kind that discovery finds among the items after the cursor, in item order, at most limit items,
	// with the current identity: what was stored before an identity existed, what a restore loaded, what a rotation or a retirement
	// left. It answers the last item id it looked at, after when there is none after it. Nil: the kind has no discovery.
	Discover func(ctx context.Context, current signing.Identity, after int64, limit int) (last int64, err error)
	// Served follows a change of the identities' states in what the kind's documents serve, once per run, whatever the identities'
	// states and signing.enabled (Q-SIG-160): npm's documents render the signatures of the listed identities, so a removal, a
	// retirement and the first current identity after none raise the revision of every package whose document changes (R-SIG-048).
	// ids are every identity. Nil: what the kind serves does not depend on the states.
	Served func(ctx context.Context, ids []signing.Identity) error
}
