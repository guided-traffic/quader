package app

import (
	"context"

	"github.com/guided-traffic/quader/internal/jobs"
)

// The scrub that reads every copy whole once per connection's period and lets the reconciliation repair what it finds
// (storage.md R-STOR-027). Package D of the build plan of several connections (docs/developer/blob-store.md). Both hooks are
// called already and do nothing yet.

// registerStorageScrub registers the kind and the task scrub.
func (a *App) registerStorageScrub(_ *jobs.Registry) {}

// wireStorageScrub builds what the scrub's handler needs at run time from the blob area, on every role.
func (a *App) wireStorageScrub(_ context.Context, _ blobArea) error { return nil }
