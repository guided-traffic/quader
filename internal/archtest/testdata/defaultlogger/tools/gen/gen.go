package gen

import "log/slog"

// A tool outside internal/ and cmd/ is not a replica's code.
func run() { slog.Info("outside the tree") }
