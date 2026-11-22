package scrub

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

// Args are the parameters of the task scrub: `POST /api/v1/system/tasks/scrub/run` with `{"connection": ""}` (R-STOR-027).
type Args struct {
	// Connection limits the run to one connection; empty reads every connection.
	Connection string `json:"connection"`
}

var connectionID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)

// Validate refuses a connection that is no connection id (R-STOR-050).
func (a Args) Validate() error {
	if a.Connection != "" && !connectionID.MatchString(a.Connection) {
		return fmt.Errorf("connection %q is no connection id (storage.md R-STOR-050)", a.Connection)
	}
	return nil
}

// JobRun is the part of the job framework's run handle a scrub run uses. *jobs.Run implements it.
type JobRun interface {
	// SetResult sets the result stored when the handler returns nil.
	SetResult(v any) error
	// Draining is closed when the replica begins its shutdown.
	Draining() <-chan struct{}
}

// RunJob is the body of the handler of the kind scrub: it reads the arguments, runs the scrub and stores the result. A run needs
// no checkpoint: the due copies are found again by their verification, so a run that a drain or a cancellation ended is
// continued by the next one. A connection this replica does not configure is ErrUnknownConnection.
func (s *Scrubber) RunJob(ctx context.Context, args json.RawMessage, run JobRun) error {
	var a Args
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &a); err != nil {
			return fmt.Errorf("scrub arguments: %w", err)
		}
	}
	if err := a.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnknownConnection, err)
	}
	res, err := s.Run(ctx, RunOptions{Connection: a.Connection, Draining: run.Draining()})
	if rerr := run.SetResult(res); rerr != nil && err == nil {
		err = rerr
	}
	return err
}
