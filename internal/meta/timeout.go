package meta

import (
	"context"
	"time"
)

type statementTimeoutKey struct{}

// WithStatementTimeout bounds every statement of the transactions and the reads that run with the returned context: the runner
// opens each of them with SET LOCAL statement_timeout to d, and a statement that passes it ends with 503 database_timeout
// (architecture.md R-ARCH-019, Q-ARCH-594). The HTTP chain sets database.request_statement_timeout on every request; a handler
// whose requirement names a longer bound sets that bound on its own context. A context without it runs under the session's
// database.session.statement_timeout, which is what jobs and loops have. d of zero or less removes the bound.
func WithStatementTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, statementTimeoutKey{}, d)
}

// StatementTimeout returns the bound of the context and whether it has one.
func StatementTimeout(ctx context.Context) (time.Duration, bool) {
	d, ok := ctx.Value(statementTimeoutKey{}).(time.Duration)
	return d, ok && d > 0
}
