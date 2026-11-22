package app

import (
	"context"

	"github.com/guided-traffic/quader/internal/jobs"
)

// The reconciliation between the connections (storage.md R-STOR-056): its queue and pass, the server-side and the streamed
// copies, the fill, the drain, the retirement and the journal pass. Package C of the build plan of several connections
// (docs/developer/blob-store.md). Both hooks are called already and do nothing yet; until they do, the commit queues the gaps
// in blob_copy_queue and nothing copies them.

// registerStorageReconcile registers the kind and the task storage-reconcile.
func (a *App) registerStorageReconcile(_ *jobs.Registry) {}

// wireStorageReconcile builds what the reconciliation's handler needs at run time from the blob area, on every role.
func (a *App) wireStorageReconcile(_ context.Context, _ blobArea) error { return nil }
