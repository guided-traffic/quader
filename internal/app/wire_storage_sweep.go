package app

import (
	"context"

	"github.com/guided-traffic/quader/internal/jobs"
)

// The orphan sweep per connection with the adoption of an object of a live blob (storage.md R-STOR-026), the sessions on
// their connection (R-STOR-054) and collection per copy with the release (R-STOR-057): package B of the build plan of several
// connections (docs/developer/blob-store.md). Both hooks are called already and do nothing yet.

// registerStorageSweep registers the kind and the task orphan-sweep.
func (a *App) registerStorageSweep(_ *jobs.Registry) {}

// wireStorageSweep builds what the sweep's handler needs at run time from the blob area, on every role.
func (a *App) wireStorageSweep(_ context.Context, _ blobArea) error { return nil }
